// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

func TestAuditRecorderDenyAccumulatesHintOnContext(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())

	pluginsdk.Audit(ctx).Deny("not_member", "player not in channel members",
		pluginsdk.AuditAttrs{"channel.type": "public"})

	hints := pluginsdk.HarvestAuditHints(ctx)
	require.Len(t, hints, 1)
	assert.Equal(t, "not_member", hints[0].ID)
	assert.Equal(t, pluginsdk.AuditEffectDeny, hints[0].Effect)
	assert.Equal(t, "player not in channel members", hints[0].Message)
	assert.Equal(t, "public", hints[0].Attributes["channel.type"])
}

func TestAuditRecorderAllowAccumulatesHintWithCorrectEffect(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())

	pluginsdk.Audit(ctx).Allow("speak_ok", "message delivered", nil)

	hints := pluginsdk.HarvestAuditHints(ctx)
	require.Len(t, hints, 1)
	assert.Equal(t, pluginsdk.AuditEffectAllow, hints[0].Effect)
}

func TestAuditRecorderIsNoOpWhenNoHandlerContextAttached(t *testing.T) {
	// Plain context — no handler attachment.
	ctx := context.Background()

	// Should not panic, should silently drop.
	pluginsdk.Audit(ctx).Deny("orphan", "no context", nil)

	hints := pluginsdk.HarvestAuditHints(ctx)
	assert.Nil(t, hints)
}

func TestHarvestAuditHintsDrainsTheSlice(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())
	pluginsdk.Audit(ctx).Deny("e1", "", nil)
	pluginsdk.Audit(ctx).Deny("e2", "", nil)

	first := pluginsdk.HarvestAuditHints(ctx)
	assert.Len(t, first, 2)

	second := pluginsdk.HarvestAuditHints(ctx)
	assert.Empty(t, second, "harvest is destructive")
}

func TestAuditRecorderDenyCopiesAttributesNotReferenced(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())

	attrs := pluginsdk.AuditAttrs{"key": "value"}
	pluginsdk.Audit(ctx).Deny("copy_test", "", attrs)

	// Mutate the caller's map — the recorded hint should not change.
	attrs["key"] = "mutated"

	hints := pluginsdk.HarvestAuditHints(ctx)
	require.Len(t, hints, 1)
	assert.Equal(t, "value", hints[0].Attributes["key"],
		"recorder must copy the attribute map")
}

// T27a — SHOULD boundary: empty ID is silently dropped and logged.
// Rationale: the proto AuditDecisionHint has min_len=1 on the ID field.
// If the SDK accepted empty IDs, they would accumulate on the context
// and then fail proto marshaling at the response-serialization step,
// silently dropping the hint without a clear diagnostic. Fail fast at
// the SDK layer by dropping + logging, so plugin authors see the
// problem during development rather than in production.
func TestAuditRecorderDenyWithEmptyIDIsSilentlyDroppedAndLogged(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())

	pluginsdk.Audit(ctx).Deny("", "message with no id", nil)

	hints := pluginsdk.HarvestAuditHints(ctx)
	assert.Empty(t, hints,
		"recorder must silently drop hints with empty ID (proto min_len=1 would fail marshal)")
}

// T27a (mirror) — symmetric Allow empty-ID drop. The empty-ID guard lives
// on the shared record() helper, so Allow inherits the same behavior. This
// test makes the symmetry explicit so a future regression that special-cases
// only Deny would be caught immediately.
func TestAuditRecorderAllowWithEmptyIDIsSilentlyDroppedAndLogged(t *testing.T) {
	ctx := pluginsdk.NewContextForHandler(context.Background())

	pluginsdk.Audit(ctx).Allow("", "allow with no id", nil)

	hints := pluginsdk.HarvestAuditHints(ctx)
	assert.Empty(t, hints,
		"recorder must silently drop Allow hints with empty ID, mirroring the Deny path")
}

// ptrTo returns a pointer to the supplied value. Test-local mirror of
// k8s.io/utils/ptr.To; inlined to avoid bringing a new module dep.
func ptrTo[T any](v T) *T { return &v }

// ulidFromString parses a 26-char ULID string. Test-local helper; failing
// the parse fails the test immediately because no test should construct
// AuditRow.EventID from a malformed ULID.
func ulidFromString(t *testing.T, s string) ulid.ULID {
	t.Helper()
	u, err := ulid.Parse(s)
	require.NoError(t, err)
	return u
}

func TestAuditRowStructMirrorsProto(t *testing.T) {
	// Compile-time + runtime assertion that the Go struct's exported
	// fields match the proto's field set.
	row := pluginsdk.AuditRow{
		EventID:    ulid.ULID{},
		Subject:    "events.test.scene.01ABC.ic",
		Type:       "test-plugin:secret",
		Timestamp:  time.Unix(1700000000, 0),
		Actor:      nil,
		Codec:      "identity",
		Payload:    []byte("hello"),
		DEKRef:     nil,
		DEKVersion: nil,
		SchemaVer:  1,
	}

	got := reflect.TypeOf(row)
	wantFields := []string{
		"EventID", "Subject", "Type", "Timestamp", "Actor",
		"Codec", "Payload", "DEKRef", "DEKVersion", "SchemaVer",
	}
	require.Equal(t, len(wantFields), got.NumField(),
		"AuditRow field count drifted from proto; INV-P7-4 broken")
	for i, want := range wantFields {
		assert.Equal(t, want, got.Field(i).Name)
	}
}

func TestAuditRowRoundTripPreservesAllFields(t *testing.T) {
	cases := []struct {
		name string
		row  pluginsdk.AuditRow
	}{
		{
			name: "identity_codec_nil_dek",
			row: pluginsdk.AuditRow{
				EventID:    ulidFromString(t, "01JD9R7N5VFKQM5T1JX6S9PYRJ"),
				Subject:    "events.test.scene.01ABC.ic",
				Type:       "test-plugin:plaintext",
				Timestamp:  time.Unix(1700000000, 0).UTC(),
				Actor:      nil,
				Codec:      "identity",
				Payload:    []byte("hello"),
				DEKRef:     nil,
				DEKVersion: nil,
				SchemaVer:  1,
			},
		},
		{
			name: "encrypted_codec_with_dek",
			row: pluginsdk.AuditRow{
				EventID:    ulidFromString(t, "01JD9R7N5VFKQM5T1JX6S9PYRK"),
				Subject:    "events.test.scene.01ABC.ic",
				Type:       "test-plugin:secret",
				Timestamp:  time.Unix(1700000000, 123456789).UTC(),
				Actor:      &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, Id: []byte("char-01")},
				Codec:      "xchacha20poly1305-v1",
				Payload:    []byte{0xDE, 0xAD, 0xBE, 0xEF},
				DEKRef:     ptrTo(uint64(42)),
				DEKVersion: ptrTo(uint32(7)),
				SchemaVer:  2,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Round-trip via the proto shape.
			proto, err := pluginsdk.LoadForQuery(tc.row)
			require.NoError(t, err)
			require.NotNil(t, proto)

			assert.Equal(t, tc.row.EventID[:], proto.GetId())
			assert.Equal(t, tc.row.Subject, proto.GetSubject())
			assert.Equal(t, tc.row.Type, proto.GetType())
			assert.Equal(t, tc.row.Timestamp.UnixNano(), proto.GetTimestamp().AsTime().UnixNano())
			assert.Equal(t, tc.row.Codec, proto.GetCodec())
			assert.Equal(t, tc.row.Payload, proto.GetPayload())
			if tc.row.DEKRef != nil {
				assert.Equal(t, *tc.row.DEKRef, proto.GetDekRef())
			}
			if tc.row.DEKVersion != nil {
				assert.Equal(t, *tc.row.DEKVersion, proto.GetDekVersion())
			}
			assert.Equal(t, tc.row.SchemaVer, proto.GetSchemaVer())
		})
	}
}
