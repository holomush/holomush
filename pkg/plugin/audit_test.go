// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk_test

import (
	"context"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// fakeJSMsg is the minimal jetstream.Msg surface StoreFromMessage reads:
// Headers() and Data(). All other methods panic — they are unreached on
// the parse path under test.
type fakeJSMsg struct {
	headers nats.Header
	data    []byte
}

func (f *fakeJSMsg) Headers() nats.Header { return f.headers }
func (f *fakeJSMsg) Data() []byte         { return f.data }
func (f *fakeJSMsg) Subject() string      { panic("not used in StoreFromMessage tests") }

func (f *fakeJSMsg) Reply() string { panic("not used in StoreFromMessage tests") }

func (f *fakeJSMsg) Ack() error { panic("not used in StoreFromMessage tests") }

func (f *fakeJSMsg) DoubleAck(_ context.Context) error { panic("not used in StoreFromMessage tests") }

func (f *fakeJSMsg) Nak() error { panic("not used in StoreFromMessage tests") }

func (f *fakeJSMsg) NakWithDelay(_ time.Duration) error { panic("not used in StoreFromMessage tests") }

func (f *fakeJSMsg) InProgress() error { panic("not used in StoreFromMessage tests") }

func (f *fakeJSMsg) Term() error { panic("not used in StoreFromMessage tests") }

func (f *fakeJSMsg) TermWithReason(_ string) error { panic("not used in StoreFromMessage tests") }

func (f *fakeJSMsg) Metadata() (*jetstream.MsgMetadata, error) {
	panic("not used in StoreFromMessage tests")
}

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
		"AuditRow field count drifted from proto; INV-EVENTBUS-26 broken")
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
			if tc.row.Actor == nil {
				assert.Nil(t, proto.GetActor(), "nil Actor MUST round-trip as nil (not zero-value)")
			} else {
				require.NotNil(t, proto.GetActor())
				assert.Equal(t, tc.row.Actor.GetKind(), proto.GetActor().GetKind())
				assert.Equal(t, tc.row.Actor.GetId(), proto.GetActor().GetId())
			}
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

// validStoreHeaders builds the App-* header set that auditheader.Parse
// requires for a non-error parse. Tests delete or override individual
// entries to exercise specific failure paths.
func validStoreHeaders() nats.Header {
	h := nats.Header{}
	h.Set("App-Codec", "identity")
	h.Set("App-Schema-Version", "1")
	return h
}

// marshalEnvelope is a test helper that proto-marshals an Event with the
// supplied id. Length of id MUST be 16 except for the explicit-bad-id
// test that verifies AUDIT_PLUGIN_BAD_EVENT_ID.
func marshalEnvelope(t *testing.T, id []byte) []byte {
	t.Helper()
	ev := &eventbusv1.Event{
		Id:        id,
		Subject:   "events.test.scene.01ABC.ic",
		Type:      "test-plugin:secret",
		Timestamp: timestamppb.New(time.Unix(1700000000, 0).UTC()),
		Payload:   []byte("envelope-payload-bytes"),
		Actor: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   []byte("char-id-16-bytes"),
		},
	}
	bs, err := proto.Marshal(ev)
	require.NoError(t, err)
	return bs
}

func TestStoreFromMessage_IdentityCodecHappyPath(t *testing.T) {
	t.Parallel()
	rowID := ulid.Make().Bytes()
	msg := &fakeJSMsg{headers: validStoreHeaders(), data: marshalEnvelope(t, rowID[:])}

	row, err := pluginsdk.StoreFromMessage(msg)
	require.NoError(t, err)
	assert.Equal(t, "identity", row.Codec)
	assert.Equal(t, int32(1), row.SchemaVer)
	assert.Equal(t, "events.test.scene.01ABC.ic", row.Subject)
	assert.Equal(t, "test-plugin:secret", row.Type)
	assert.Equal(t, []byte("envelope-payload-bytes"), row.Payload,
		"INV-P7-1: payload bytes from envelope must round-trip verbatim")
	assert.Equal(t, rowID[:], row.EventID[:])
	assert.Nil(t, row.DEKRef, "identity codec MUST have nil DEKRef")
	assert.Nil(t, row.DEKVersion, "identity codec MUST have nil DEKVersion")
}

func TestStoreFromMessage_EncryptedCodecWithDEK(t *testing.T) {
	t.Parallel()
	h := validStoreHeaders()
	h.Set("App-Codec", "xchacha20poly1305-v1")
	h.Set("App-Dek-Ref", "42")
	h.Set("App-Dek-Version", "7")
	rowID := ulid.Make().Bytes()
	msg := &fakeJSMsg{headers: h, data: marshalEnvelope(t, rowID[:])}

	row, err := pluginsdk.StoreFromMessage(msg)
	require.NoError(t, err)
	assert.Equal(t, "xchacha20poly1305-v1", row.Codec)
	require.NotNil(t, row.DEKRef)
	assert.Equal(t, uint64(42), *row.DEKRef)
	require.NotNil(t, row.DEKVersion)
	assert.Equal(t, uint32(7), *row.DEKVersion)
}

func TestStoreFromMessage_RejectsMissingHeader(t *testing.T) {
	t.Parallel()
	h := validStoreHeaders()
	h.Del("App-Codec") // parser-level required field
	msg := &fakeJSMsg{headers: h, data: marshalEnvelope(t, ulid.Make().Bytes())}

	_, err := pluginsdk.StoreFromMessage(msg)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_MISSING_HEADER")
}

func TestStoreFromMessage_RejectsBadSchemaVersion(t *testing.T) {
	t.Parallel()
	h := validStoreHeaders()
	h.Set("App-Schema-Version", "not-a-number")
	msg := &fakeJSMsg{headers: h, data: marshalEnvelope(t, ulid.Make().Bytes())}

	_, err := pluginsdk.StoreFromMessage(msg)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_BAD_SCHEMA_VERSION")
}

func TestStoreFromMessage_RejectsNegativeDEKRef(t *testing.T) {
	t.Parallel()
	h := validStoreHeaders()
	h.Set("App-Codec", "xchacha20poly1305-v1")
	h.Set("App-Dek-Ref", "-1")
	msg := &fakeJSMsg{headers: h, data: marshalEnvelope(t, ulid.Make().Bytes())}

	_, err := pluginsdk.StoreFromMessage(msg)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_DEK_REF_PARSE_FAILED")
}

func TestStoreFromMessage_RejectsUnmarshalableEnvelope(t *testing.T) {
	t.Parallel()
	msg := &fakeJSMsg{
		headers: validStoreHeaders(),
		data:    []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
	}

	_, err := pluginsdk.StoreFromMessage(msg)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_ENVELOPE_UNMARSHAL_FAILED")
}

func TestStoreFromMessage_RejectsBadEventID(t *testing.T) {
	t.Parallel()
	// 8-byte id (not 16) — INV: ULID is 16 bytes.
	msg := &fakeJSMsg{
		headers: validStoreHeaders(),
		data:    marshalEnvelope(t, []byte("8-bytes!")),
	}

	_, err := pluginsdk.StoreFromMessage(msg)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_BAD_EVENT_ID")
}

func TestStoreFromMessage_NilTimestampTolerated(t *testing.T) {
	t.Parallel()
	// Manually construct an envelope with no timestamp.
	ev := &eventbusv1.Event{
		Id:      ulid.Make().Bytes(),
		Subject: "events.test.scene.01ABC.ic",
		Type:    "test-plugin:secret",
		Payload: []byte("p"),
	}
	data, err := proto.Marshal(ev)
	require.NoError(t, err)
	msg := &fakeJSMsg{headers: validStoreHeaders(), data: data}

	row, err := pluginsdk.StoreFromMessage(msg)
	require.NoError(t, err)
	assert.True(t, row.Timestamp.IsZero(), "missing envelope ts MUST yield zero time, not panic")
}

// -------------------------------------------------------------------------
// DecryptOwnAuditRows SDK tests
// -------------------------------------------------------------------------

// decryptTestServer is a minimal PluginHostServiceServer that records
// DecryptOwnAuditRows calls and returns a configurable per-row result.
type decryptTestServer struct {
	pluginv1.UnimplementedPluginHostServiceServer
	fn func(req *pluginv1.DecryptOwnAuditRowsRequest) (*pluginv1.DecryptOwnAuditRowsResponse, error)
}

func (s *decryptTestServer) DecryptOwnAuditRows(_ context.Context, req *pluginv1.DecryptOwnAuditRowsRequest) (*pluginv1.DecryptOwnAuditRowsResponse, error) {
	if s.fn != nil {
		return s.fn(req)
	}
	// Echo id back with no plaintext by default.
	results := make([]*pluginv1.RowResult, 0, len(req.GetRows()))
	for _, r := range req.GetRows() {
		results = append(results, &pluginv1.RowResult{Id: r.GetId()})
	}
	return &pluginv1.DecryptOwnAuditRowsResponse{Results: results}, nil
}

// startDecryptTestServer spins up a bufconn gRPC server and returns a client conn.
func startDecryptTestServer(t *testing.T, srv pluginv1.PluginHostServiceServer) *grpc.ClientConn {
	t.Helper()
	const bufSize = 1 << 20 // 1 MiB
	listener := bufconn.Listen(bufSize)
	server := grpc.NewServer() //nolint:noctx // bufconn test server — no production traffic
	pluginv1.RegisterPluginHostServiceServer(server, srv)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient(
		"passthrough:///decrypt-test",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()), //nolint:noctx // bufconn test client
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestDecryptOwnAuditRowsSDKReturnsPerRowPlaintext(t *testing.T) {
	t.Parallel()
	rowID := ulid.Make().Bytes()
	srv := &decryptTestServer{
		fn: func(req *pluginv1.DecryptOwnAuditRowsRequest) (*pluginv1.DecryptOwnAuditRowsResponse, error) {
			return &pluginv1.DecryptOwnAuditRowsResponse{
				Results: []*pluginv1.RowResult{
					{Id: req.GetRows()[0].GetId(), Outcome: &pluginv1.RowResult_Plaintext{Plaintext: []byte("hello")}},
				},
			}, nil
		},
	}
	conn := startDecryptTestServer(t, srv)
	client := pluginv1.NewPluginHostServiceClient(conn)

	rows := []*pluginv1.AuditRow{{Id: rowID, Subject: "events.main.scene.01ABC.ic"}}
	got, err := pluginsdk.DecryptOwnAuditRows(context.Background(), client, rows)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, rowID, got[0].GetId())
	assert.Equal(t, []byte("hello"), got[0].GetPlaintext())
}

func TestDecryptOwnAuditRowsSDKReturnsRefusalReason(t *testing.T) {
	t.Parallel()
	rowID := ulid.Make().Bytes()
	srv := &decryptTestServer{
		fn: func(req *pluginv1.DecryptOwnAuditRowsRequest) (*pluginv1.DecryptOwnAuditRowsResponse, error) {
			return &pluginv1.DecryptOwnAuditRowsResponse{
				Results: []*pluginv1.RowResult{
					{Id: req.GetRows()[0].GetId(), Outcome: &pluginv1.RowResult_NoPlaintextReason{NoPlaintextReason: "not_owner"}},
				},
			}, nil
		},
	}
	conn := startDecryptTestServer(t, srv)
	client := pluginv1.NewPluginHostServiceClient(conn)

	rows := []*pluginv1.AuditRow{{Id: rowID, Subject: "events.main.channel.01ABC.msg"}}
	got, err := pluginsdk.DecryptOwnAuditRows(context.Background(), client, rows)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "not_owner", got[0].GetNoPlaintextReason())
	assert.Nil(t, got[0].GetPlaintext())
}

func TestDecryptOwnAuditRowsSDKPropagatesHostError(t *testing.T) {
	t.Parallel()
	srv := &decryptTestServer{
		fn: func(_ *pluginv1.DecryptOwnAuditRowsRequest) (*pluginv1.DecryptOwnAuditRowsResponse, error) {
			return nil, status.Error(codes.ResourceExhausted, "DECRYPT_BATCH_TOO_LARGE: cap 500")
		},
	}
	conn := startDecryptTestServer(t, srv)
	client := pluginv1.NewPluginHostServiceClient(conn)

	rows := []*pluginv1.AuditRow{{Id: ulid.Make().Bytes()}}
	_, err := pluginsdk.DecryptOwnAuditRows(context.Background(), client, rows)
	require.Error(t, err)
}
