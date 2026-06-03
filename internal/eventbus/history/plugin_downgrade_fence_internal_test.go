// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/pkg/errutil"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// fenceRowOf builds an AuditRow for the fenceCheckRow unit tests.
// dekRef may be nil (absent dek_ref) or non-nil.
func fenceRowOf(eventType, codec string, dekRef *uint64) *pluginauditpb.AuditRow {
	row := &pluginauditpb.AuditRow{
		Id:      []byte("0123456789ABCDEF"),
		Subject: "events.test.scene.01ABC.ic",
		Type:    eventType,
		Codec:   codec,
		Payload: []byte("payload"),
	}
	if dekRef != nil {
		row.DekRef = dekRef
	}
	return row
}

// fenceLookupStub is the in-package CryptoKeysLookup fake for the
// fenceCheckRow unit tests. exists controls the (bool, nil) answer; a
// non-nil err exercises the infrastructure-failure (stream-fatal) path.
type fenceLookupStub struct {
	exists bool
	err    error
}

func (f fenceLookupStub) Exists(_ context.Context, _ uint64) (bool, error) {
	return f.exists, f.err
}

// TestFenceCheckRowVerdictsMatchSpec verifies that fenceCheckRow returns the
// correct fenceVerdict for each spec-mandated branch (INV-CRYPTO-42 downgrade,
// INV-CRYPTO-50 DEK existence) without going through the full fencedStream path.
func TestFenceCheckRowVerdictsMatchSpec(t *testing.T) {
	t.Parallel()
	always := map[string]struct{}{"scene_pose": {}}
	dek := uint64(7)

	tests := []struct {
		name   string
		row    *pluginauditpb.AuditRow
		lookup CryptoKeysLookup
		want   fenceVerdict
	}{
		{
			name:   "identity codec on always-sensitive type is a downgrade refusal",
			row:    fenceRowOf("scene_pose", "identity", nil),
			lookup: fenceLookupStub{exists: true},
			want:   fenceRefuseDowngrade,
		},
		{
			name:   "identity codec on non-sensitive type is clean",
			row:    fenceRowOf("scene_say", "identity", nil),
			lookup: fenceLookupStub{exists: true},
			want:   fenceClean,
		},
		{
			name:   "non-identity codec with existing dek is clean",
			row:    fenceRowOf("scene_pose", "xchacha20poly1305-v1", &dek),
			lookup: fenceLookupStub{exists: true},
			want:   fenceClean,
		},
		{
			name:   "non-identity codec with absent dek_ref is DEK-missing refusal",
			row:    fenceRowOf("scene_pose", "xchacha20poly1305-v1", nil),
			lookup: fenceLookupStub{exists: true},
			want:   fenceRefuseDEKMissing,
		},
		{
			name:   "non-identity codec with lookup-miss dek is DEK-missing refusal",
			row:    fenceRowOf("scene_pose", "xchacha20poly1305-v1", &dek),
			lookup: fenceLookupStub{exists: false},
			want:   fenceRefuseDEKMissing,
		},
		{
			name:   "non-identity codec with nil lookup fails closed to Internal",
			row:    fenceRowOf("scene_pose", "xchacha20poly1305-v1", &dek),
			lookup: nil,
			want:   fenceRefuseInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := fenceCheckRow(context.Background(), tt.row, always, tt.lookup)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestFenceCheckRowForwardsLookupError verifies that an infrastructure
// failure from the crypto_keys lookup surfaces as a wrapped
// AUDIT_ROW_DEK_LOOKUP_FAILED error (stream-fatal), distinguishing
// "can't even check" from the per-row "DEK doesn't exist" refusal.
func TestFenceCheckRowForwardsLookupError(t *testing.T) {
	t.Parallel()
	always := map[string]struct{}{"scene_pose": {}}
	dek := uint64(7)

	_, err := fenceCheckRow(
		context.Background(),
		fenceRowOf("scene_pose", "xchacha20poly1305-v1", &dek),
		always,
		fenceLookupStub{err: errors.New("connection refused")},
	)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_ROW_DEK_LOOKUP_FAILED")
}

// --- INV-CRYPTO-32: clean-row decrypt for routed participants ---

// fenceStubRouter / fenceStubStream are in-package fakes (the external
// fakeRouter/fakeFenceStream live in package history_test and cannot drive
// the decryptPluginRow primitive, which is unexported).
type fenceStubRouter struct {
	stream eventbus.HistoryStream
}

func (r *fenceStubRouter) QueryHistory(_ context.Context, _ string, _ eventbus.HistoryQuery) (eventbus.HistoryStream, error) {
	return r.stream, nil
}

type fenceStubStream struct {
	events []eventbus.Event
	idx    int
}

func (s *fenceStubStream) Next(_ context.Context) (eventbus.Event, error) {
	if s.idx >= len(s.events) {
		return eventbus.Event{}, io.EOF
	}
	ev := s.events[s.idx]
	s.idx++
	return ev, nil
}

func (s *fenceStubStream) Close() error { return nil }

// readbackDenyGuard refuses every Check — stands in for a non-member
// character whose checkCharacter DEK-membership branch denies.
type readbackDenyGuard struct{}

func (readbackDenyGuard) Check(_ context.Context, _ eventbus.SessionCheckRequest) (eventbus.SessionDecision, error) {
	return eventbus.SessionDecision{Permit: false}, nil
}

// characterPrincipal returns a CHARACTER SessionIdentity for the routed
// participant read path (ReadBack=false → checkCharacter).
func characterPrincipal(characterID, bindingID string) eventbus.SessionIdentity {
	return eventbus.SessionIdentity{
		Kind:        eventbus.IdentityKindCharacter,
		CharacterID: characterID,
		BindingID:   bindingID,
	}
}

// fenceWithDeps builds a fence wrapping a single-event stub stream, wired with
// the supplied readback crypto deps (guard controls member vs non-member).
func fenceWithDeps(t *testing.T, deps readbackTestDeps, guard eventbus.SessionAuthGuard, row *pluginauditpb.AuditRow) *PluginDowngradeFence {
	t.Helper()
	stream := &fenceStubStream{events: []eventbus.Event{stampedFenceEvent(row)}}
	return NewPluginDowngradeFence(
		&fenceStubRouter{stream: stream},
		WithAlwaysSensitiveTypes(deps.alwaysSensitive),
		WithCryptoKeysLookup(deps.cryptoKeys),
		WithViolationEmitter(&fenceNoopEmitter{}),
		WithFenceReadbackCrypto(guard, deps.dek, deps.audit),
	)
}

// stampedFenceEvent builds an Event carrying row via the AuditRow stamp seam,
// mirroring what the production router does so AuditRowOf(ev) recovers row.
func stampedFenceEvent(row *pluginauditpb.AuditRow) eventbus.Event {
	ev := eventbus.Event{
		Subject: eventbus.Subject(row.GetSubject()),
		Type:    eventbus.Type(row.GetType()),
		Payload: row.GetPayload(),
	}
	eventbus.StampAuditRow(&ev, row)
	return ev
}

// fenceNoopEmitter is a do-nothing ViolationEmitter for tests that never
// exercise the downgrade path.
type fenceNoopEmitter struct{}

func (fenceNoopEmitter) EmitViolation(_ context.Context, _ string, _ *pluginauditpb.AuditRow, _, _ string) error {
	return nil
}

// TestFenceDecryptsCleanRowForMember asserts a member character reading a
// clean (encrypted, valid-DEK) plugin-owned row over the routed path receives
// DECRYPTED plaintext — the INV-CRYPTO-32 contract change (was ciphertext
// passthrough pre-T8). The participant path is ReadBack=false (checkCharacter).
func TestFenceDecryptsCleanRowForMember(t *testing.T) {
	t.Parallel()
	deps := newReadbackDeps(t) // permit guard == member
	row := encryptedRow(t, deps, []byte("Alice poses."))

	fence := fenceWithDeps(t, deps, deps.guard, row)
	out, err := fence.QueryHistory(context.Background(), "core-scenes", eventbus.HistoryQuery{
		Subject:  "events.test.scene.01ABC.pose",
		Identity: characterPrincipal("char-1", "bind-1"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = out.Close() })

	ev, err := out.Next(context.Background())
	require.NoError(t, err)
	assert.False(t, ev.MetadataOnly, "member MUST receive decrypted plaintext, not metadata-only")
	assert.Equal(t, []byte("Alice poses."), ev.Payload, "clean row decrypts to plaintext for member")
}

// TestFenceRefusesCleanRowForNonMember asserts a non-member character reading a
// clean plugin-owned row receives a metadata-only refusal (AuthGuardDeny) with
// NO plaintext — the security crux of INV-CRYPTO-32. checkCharacter denies, so the
// row is refused after the layer-(1) fence but before any plaintext is surfaced.
func TestFenceRefusesCleanRowForNonMember(t *testing.T) {
	t.Parallel()
	deps := newReadbackDeps(t)
	row := encryptedRow(t, deps, []byte("Alice poses."))

	fence := fenceWithDeps(t, deps, readbackDenyGuard{}, row)
	out, err := fence.QueryHistory(context.Background(), "core-scenes", eventbus.HistoryQuery{
		Subject:  "events.test.scene.01ABC.pose",
		Identity: characterPrincipal("intruder", "bind-x"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = out.Close() })

	ev, err := out.Next(context.Background())
	require.NoError(t, err, "non-member refusal is per-row metadata-only, not stream-fatal")
	assert.True(t, ev.MetadataOnly, "non-member MUST get metadata-only")
	assert.Equal(t, eventbus.NoPlaintextReasonAuthGuardDeny, ev.NoPlaintextReason,
		"non-member denial surfaces AuthGuardDeny (checkCharacter denied)")
	assert.Nil(t, ev.Payload, "non-member MUST NOT receive plaintext (INV-CRYPTO-32)")
}
