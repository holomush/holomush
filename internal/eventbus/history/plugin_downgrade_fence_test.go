// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/history"
	"github.com/holomush/holomush/pkg/errutil"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// --- Test fakes ---

// fakeRouter returns a pre-staged HistoryStream from QueryHistory.
type fakeRouter struct {
	stream eventbus.HistoryStream
	err    error
}

func (r *fakeRouter) QueryHistory(_ context.Context, _ string, _ eventbus.HistoryQuery) (eventbus.HistoryStream, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.stream, nil
}

// fakeStream returns a pre-staged sequence of events. Each call to
// Next pops one off; exhaustion returns io.EOF.
type fakeFenceStream struct {
	events []eventbus.Event
	idx    int
	closed bool
}

func (s *fakeFenceStream) Next(_ context.Context) (eventbus.Event, error) {
	if s.idx >= len(s.events) {
		return eventbus.Event{}, io.EOF
	}
	ev := s.events[s.idx]
	s.idx++
	return ev, nil
}

func (s *fakeFenceStream) Close() error {
	s.closed = true
	return nil
}

// stubLookupAlwaysFound implements CryptoKeysLookup with Exists=true
// for any dek_ref. Used for tests that focus on INV-CRYPTO-42 paths.
type stubLookupAlwaysFound struct {
	calls int
	mu    sync.Mutex
}

func (s *stubLookupAlwaysFound) Exists(_ context.Context, _ uint64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return true, nil
}

// stubLookupNotFound returns Exists=false for INV-CRYPTO-50 unknown-dek tests.
type stubLookupNotFound struct{}

func (stubLookupNotFound) Exists(_ context.Context, _ uint64) (bool, error) {
	return false, nil
}

// stubLookupErr returns an infrastructure error for the stream-fatal path.
type stubLookupErr struct {
	err error
}

func (s stubLookupErr) Exists(_ context.Context, _ uint64) (bool, error) {
	return false, s.err
}

// recordingEmitter captures EmitViolation calls so tests can assert
// the audit signal fired with the expected refusal code.
type recordingEmitter struct {
	mu    sync.Mutex
	calls []recordedViolation
	err   error
}

type recordedViolation struct {
	plugin      string
	rowType     string
	expected    string
	refusalCode string
}

func (r *recordingEmitter) EmitViolation(_ context.Context, plugin string, row *pluginauditpb.AuditRow, expected, refusalCodee string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedViolation{
		plugin:      plugin,
		rowType:     row.GetType(),
		expected:    expected,
		refusalCode: refusalCodee,
	})
	return r.err
}

func (r *recordingEmitter) snapshot() []recordedViolation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedViolation, len(r.calls))
	copy(out, r.calls)
	return out
}

// --- Helpers ---

// stampedEvent builds an Event whose AuditRowOf returns row by going
// through the audit-package router stamp seam. Calls eventbus.StampAuditRow
// directly since these tests bypass the actual router conversion.
func stampedEvent(row *pluginauditpb.AuditRow) eventbus.Event {
	ev := eventbus.Event{
		Subject: eventbus.Subject(row.GetSubject()),
		Type:    eventbus.Type(row.GetType()),
		Payload: row.GetPayload(),
	}
	eventbus.StampAuditRow(&ev, row)
	return ev
}

func sensitiveSet(types ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(types))
	for _, t := range types {
		out[t] = struct{}{}
	}
	return out
}

// --- Tests ---

// TestFenceRefusesIdentityForAlwaysSensitiveType — INV-CRYPTO-42. Plugin
// returns codec=identity + cleartext for an always-sensitive type;
// the fence MUST surface metadata_only=true with refusal reason
// DowngradeRefused, AND emit a plugin_integrity_violation audit.
func TestFenceRefusesIdentityForAlwaysSensitiveType(t *testing.T) {
	t.Parallel()

	row := &pluginauditpb.AuditRow{
		Id:      []byte("0123456789ABCDEF"),
		Subject: "events.test.scene.01ABC.ic",
		Type:    "test-plugin:secret",
		Codec:   "identity",
		Payload: []byte("cleartext-leak"),
	}
	stream := &fakeFenceStream{events: []eventbus.Event{stampedEvent(row)}}
	rec := &recordingEmitter{}
	fence := history.NewPluginDowngradeFence(
		&fakeRouter{stream: stream},
		history.WithAlwaysSensitiveTypes(sensitiveSet("test-plugin:secret")),
		history.WithCryptoKeysLookup(&stubLookupAlwaysFound{}),
		history.WithViolationEmitter(rec),
	)

	out, err := fence.QueryHistory(context.Background(), "test-plugin", eventbus.HistoryQuery{
		Subject: "events.test.scene.01ABC.ic",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = out.Close() })

	ev, err := out.Next(context.Background())
	require.NoError(t, err, "INV-CRYPTO-42 refusal MUST be per-row, not stream-fatal")
	assert.True(t, ev.MetadataOnly, "refused row MUST stamp metadata_only=true")
	assert.Equal(t, eventbus.NoPlaintextReasonDowngradeRefused, ev.NoPlaintextReason)
	assert.Nil(t, ev.Payload, "refused row MUST drop payload")

	calls := rec.snapshot()
	require.Len(t, calls, 1, "INV-CRYPTO-42 refusal MUST emit exactly one violation")
	assert.Equal(t, "test-plugin", calls[0].plugin)
	assert.Equal(t, "test-plugin:secret", calls[0].rowType)
	assert.Equal(t, "AUDIT_ROW_DOWNGRADE_DETECTED", calls[0].refusalCode)
}

// TestFenceContinuesStreamAfterRefusal — INV-CRYPTO-42. The original v1
// plan returned a stream-fatal error here, which would let any
// malicious plugin DoS legitimate rows by putting a downgrade event
// first. The corrected design: refused row is per-row metadata_only,
// stream continues to subsequent rows.
func TestFenceContinuesStreamAfterRefusal(t *testing.T) {
	t.Parallel()

	bad := &pluginauditpb.AuditRow{
		Id:      []byte("BAD0000000000000"),
		Subject: "events.test.scene.01ABC.ic",
		Type:    "test-plugin:secret",
		Codec:   "identity",
		Payload: []byte("cleartext-leak"),
	}
	good := &pluginauditpb.AuditRow{
		Id:      []byte("GOOD000000000000"),
		Subject: "events.test.scene.01ABC.ic",
		Type:    "test-plugin:plain",
		Codec:   "identity",
		Payload: []byte("legit-plaintext"),
	}
	stream := &fakeFenceStream{events: []eventbus.Event{stampedEvent(bad), stampedEvent(good)}}
	fence := history.NewPluginDowngradeFence(
		&fakeRouter{stream: stream},
		history.WithAlwaysSensitiveTypes(sensitiveSet("test-plugin:secret")),
		history.WithCryptoKeysLookup(&stubLookupAlwaysFound{}),
		history.WithViolationEmitter(&recordingEmitter{}),
	)

	out, err := fence.QueryHistory(context.Background(), "test-plugin", eventbus.HistoryQuery{
		Subject: "events.test.scene.01ABC.ic",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = out.Close() })

	row0, err := out.Next(context.Background())
	require.NoError(t, err)
	require.True(t, row0.MetadataOnly, "row 0 MUST be refused")

	row1, err := out.Next(context.Background())
	require.NoError(t, err, "row 1 MUST be reachable; v1's stream-fatal-error bug would have stopped here")
	assert.False(t, row1.MetadataOnly, "row 1 MUST be unrefused")
	assert.Equal(t, []byte("legit-plaintext"), row1.Payload)
}

// TestFenceAllowsIdentityForNonSensitiveType — INV-CRYPTO-42 negative.
// Identity codec for a NON-sensitive type passes through unchanged.
func TestFenceAllowsIdentityForNonSensitiveType(t *testing.T) {
	t.Parallel()

	row := &pluginauditpb.AuditRow{
		Id:      []byte("0123456789ABCDEF"),
		Subject: "events.test.scene.01ABC.ic",
		Type:    "test-plugin:plain",
		Codec:   "identity",
		Payload: []byte("legit-plaintext"),
	}
	stream := &fakeFenceStream{events: []eventbus.Event{stampedEvent(row)}}
	rec := &recordingEmitter{}
	fence := history.NewPluginDowngradeFence(
		&fakeRouter{stream: stream},
		history.WithAlwaysSensitiveTypes(sensitiveSet("test-plugin:secret")),
		history.WithCryptoKeysLookup(&stubLookupAlwaysFound{}),
		history.WithViolationEmitter(rec),
	)

	out, err := fence.QueryHistory(context.Background(), "test-plugin", eventbus.HistoryQuery{
		Subject: "events.test.scene.01ABC.ic",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = out.Close() })

	ev, err := out.Next(context.Background())
	require.NoError(t, err)
	assert.False(t, ev.MetadataOnly, "non-sensitive type MUST pass through")
	assert.Equal(t, []byte("legit-plaintext"), ev.Payload)
	assert.Empty(t, rec.snapshot(), "no violation should fire for non-sensitive identity")
}

// TestFenceRefusesUnknownDekRef — INV-CRYPTO-50. Plugin returns a non-
// identity codec with a dek_ref the crypto_keys lookup says doesn't
// exist; the fence MUST surface metadata_only=true. NO violation
// emit (indistinguishable from legitimate Rekey-destroyed case).
func TestFenceRefusesUnknownDekRef(t *testing.T) {
	t.Parallel()

	dr := uint64(9999999)
	dv := uint32(1)
	row := &pluginauditpb.AuditRow{
		Id:         []byte("0123456789ABCDEF"),
		Subject:    "events.test.scene.01ABC.ic",
		Type:       "test-plugin:secret",
		Codec:      "xchacha20poly1305-v1",
		Payload:    []byte("ciphertext"),
		DekRef:     &dr,
		DekVersion: &dv,
	}
	stream := &fakeFenceStream{events: []eventbus.Event{stampedEvent(row)}}
	rec := &recordingEmitter{}
	fence := history.NewPluginDowngradeFence(
		&fakeRouter{stream: stream},
		history.WithAlwaysSensitiveTypes(sensitiveSet("test-plugin:secret")),
		history.WithCryptoKeysLookup(stubLookupNotFound{}),
		history.WithViolationEmitter(rec),
	)

	out, err := fence.QueryHistory(context.Background(), "test-plugin", eventbus.HistoryQuery{
		Subject: "events.test.scene.01ABC.ic",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = out.Close() })

	ev, err := out.Next(context.Background())
	require.NoError(t, err, "INV-CRYPTO-50 unknown DEK MUST be per-row refusal, not stream-fatal")
	assert.True(t, ev.MetadataOnly)
	assert.Equal(t, eventbus.NoPlaintextReasonDEKMissing, ev.NoPlaintextReason,
		"INV-CRYPTO-50 unknown-DEK MUST report DEKMissing (legitimate Rekey-destroyed lookalike), NOT DowngradeRefused")
	assert.Empty(t, rec.snapshot(), "INV-CRYPTO-50 path MUST NOT emit violation audit")
}

// TestFenceRefusesAbsentDekRefForNonIdentityCodec — INV-CRYPTO-50. Plugin
// returns a non-identity codec with dek_ref absent (nil); the fence
// MUST surface metadata_only=true.
func TestFenceRefusesAbsentDekRefForNonIdentityCodec(t *testing.T) {
	t.Parallel()

	row := &pluginauditpb.AuditRow{
		Id:      []byte("0123456789ABCDEF"),
		Subject: "events.test.scene.01ABC.ic",
		Type:    "test-plugin:secret",
		Codec:   "xchacha20poly1305-v1",
		Payload: []byte("ciphertext"),
		// DekRef intentionally nil
	}
	stream := &fakeFenceStream{events: []eventbus.Event{stampedEvent(row)}}
	fence := history.NewPluginDowngradeFence(
		&fakeRouter{stream: stream},
		history.WithAlwaysSensitiveTypes(sensitiveSet("test-plugin:secret")),
		history.WithCryptoKeysLookup(&stubLookupAlwaysFound{}),
		history.WithViolationEmitter(&recordingEmitter{}),
	)

	out, err := fence.QueryHistory(context.Background(), "test-plugin", eventbus.HistoryQuery{
		Subject: "events.test.scene.01ABC.ic",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = out.Close() })

	ev, err := out.Next(context.Background())
	require.NoError(t, err)
	assert.True(t, ev.MetadataOnly)
	assert.Equal(t, eventbus.NoPlaintextReasonDEKMissing, ev.NoPlaintextReason,
		"INV-CRYPTO-50 absent-dek_ref MUST report DEKMissing (Rekey-destroyed lookalike), NOT DowngradeRefused")
}

// TestFenceRefusalClearsEmbeddedAuditRowPayload — master spec INV-CRYPTO-15
// (refused row payload empty). Pre-1r0v.9, refuseEvent nilled the
// outer Event.Payload but left the embedded *pluginauditpb.AuditRow
// intact — the value-copied refused.auditRow still pointed at the
// original row, which retained the plugin-supplied cleartext bytes.
//
// A future feature that surfaces auditRow metadata (e.g. operator-read
// classifier extending its inspection to plugin rows) would silently
// re-leak the cleartext. eventbus.Event.Refused now strips both the
// outer Payload AND the embedded auditRow.Payload; this test gates
// that contract end-to-end through the fence.
func TestFenceRefusalClearsEmbeddedAuditRowPayload(t *testing.T) {
	t.Parallel()

	row := &pluginauditpb.AuditRow{
		Id:      []byte("0123456789ABCDEF"),
		Subject: "events.test.scene.01ABC.ic",
		Type:    "test-plugin:secret",
		Codec:   "identity",
		Payload: []byte("malicious-cleartext-leak"),
	}
	stream := &fakeFenceStream{events: []eventbus.Event{stampedEvent(row)}}
	fence := history.NewPluginDowngradeFence(
		&fakeRouter{stream: stream},
		history.WithAlwaysSensitiveTypes(sensitiveSet("test-plugin:secret")),
		history.WithCryptoKeysLookup(&stubLookupAlwaysFound{}),
		history.WithViolationEmitter(&recordingEmitter{}),
	)

	out, err := fence.QueryHistory(context.Background(), "test-plugin", eventbus.HistoryQuery{
		Subject: "events.test.scene.01ABC.ic",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = out.Close() })

	ev, err := out.Next(context.Background())
	require.NoError(t, err)
	require.True(t, ev.MetadataOnly, "test precondition: row MUST have been refused")
	require.Nil(t, ev.Payload, "outer Event.Payload MUST be nil on refusal")

	embedded := eventbus.AuditRowOf(ev)
	require.NotNil(t, embedded, "embedded auditRow MUST survive refusal (diagnostic metadata preserved)")
	assert.Empty(t, embedded.GetPayload(),
		"INV-CRYPTO-15: refused row MUST NOT carry cleartext anywhere — embedded auditRow.Payload MUST be empty")
	// Diagnostic metadata (codec) is intentionally preserved so an
	// operator-read classifier can still see WHY the row was refused.
	assert.Equal(t, "identity", embedded.GetCodec(),
		"diagnostic metadata (codec) MUST survive refusal — only Payload is stripped")
}

// TestFenceFailsClosedWithNilCryptoKeysLookup — INV-CRYPTO-50 fail-closed
// guard. Production wiring (E.3) always supplies a non-nil lookup, but
// a future change to the default ("nil → refuse" to "nil → pass") would
// silently weaken the fence. A non-identity codec + dek_ref-present row
// fed through a fence built WITHOUT WithCryptoKeysLookup MUST be
// refused with NoPlaintextReasonInternal (the configuration-failure
// reason — distinct from DEKMissing, which is the legitimate
// Rekey-destroyed case).
func TestFenceFailsClosedWithNilCryptoKeysLookup(t *testing.T) {
	t.Parallel()

	dr := uint64(42)
	dv := uint32(1)
	row := &pluginauditpb.AuditRow{
		Id:         []byte("0123456789ABCDEF"),
		Subject:    "events.test.scene.01ABC.ic",
		Type:       "test-plugin:secret",
		Codec:      "xchacha20poly1305-v1",
		Payload:    []byte("ciphertext"),
		DekRef:     &dr,
		DekVersion: &dv,
	}
	stream := &fakeFenceStream{events: []eventbus.Event{stampedEvent(row)}}
	fence := history.NewPluginDowngradeFence(
		&fakeRouter{stream: stream},
		history.WithAlwaysSensitiveTypes(sensitiveSet("test-plugin:secret")),
		// WithCryptoKeysLookup intentionally omitted — exercises
		// the fail-closed branch at plugin_downgrade_fence.go.
		history.WithViolationEmitter(&recordingEmitter{}),
	)

	out, err := fence.QueryHistory(context.Background(), "test-plugin", eventbus.HistoryQuery{
		Subject: "events.test.scene.01ABC.ic",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = out.Close() })

	ev, err := out.Next(context.Background())
	require.NoError(t, err, "nil-lookup fail-closed MUST be per-row refusal, not stream-fatal")
	assert.True(t, ev.MetadataOnly)
	assert.Equal(t, eventbus.NoPlaintextReasonInternal, ev.NoPlaintextReason,
		"nil cryptoKeysLookup is a configuration failure — MUST report Internal, NOT DEKMissing or DowngradeRefused")
}

// TestFenceForwardsCryptoKeysLookupError — INV-CRYPTO-50 error path. A
// non-nil error from the crypto_keys lookup is infrastructure failure,
// stream-fatal — distinguishes "DEK doesn't exist" (per-row refusal)
// from "we can't even check" (stop the stream).
func TestFenceForwardsCryptoKeysLookupError(t *testing.T) {
	t.Parallel()

	dr := uint64(42)
	dv := uint32(1)
	row := &pluginauditpb.AuditRow{
		Id:         []byte("0123456789ABCDEF"),
		Subject:    "events.test.scene.01ABC.ic",
		Type:       "test-plugin:secret",
		Codec:      "xchacha20poly1305-v1",
		Payload:    []byte("ciphertext"),
		DekRef:     &dr,
		DekVersion: &dv,
	}
	stream := &fakeFenceStream{events: []eventbus.Event{stampedEvent(row)}}
	fence := history.NewPluginDowngradeFence(
		&fakeRouter{stream: stream},
		history.WithAlwaysSensitiveTypes(sensitiveSet("test-plugin:secret")),
		history.WithCryptoKeysLookup(stubLookupErr{err: errors.New("connection refused")}),
		history.WithViolationEmitter(&recordingEmitter{}),
	)

	out, err := fence.QueryHistory(context.Background(), "test-plugin", eventbus.HistoryQuery{
		Subject: "events.test.scene.01ABC.ic",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = out.Close() })

	_, err = out.Next(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_ROW_DEK_LOOKUP_FAILED")
}

// TestFenceSetBuiltOnceAtBoot — INV-CRYPTO-44. The always-sensitive set is
// captured at construction time; subsequent caller-side mutation MUST
// NOT shift the fence's behavior. We verify by mutating the source
// map after construction and asserting the fence's view is unchanged.
func TestFenceSetBuiltOnceAtBoot(t *testing.T) {
	t.Parallel()

	src := sensitiveSet("test-plugin:secret")
	fence := history.NewPluginDowngradeFence(
		&fakeRouter{stream: &fakeFenceStream{}},
		history.WithAlwaysSensitiveTypes(src),
	)

	// Mutate the source after construction — fence MUST be insulated.
	src["test-plugin:added-after-boot"] = struct{}{}
	delete(src, "test-plugin:secret")

	got := fence.AlwaysSensitiveTypesForTest()
	require.Len(t, got, 1, "fence's set MUST be the boot-time snapshot")
	_, ok := got["test-plugin:secret"]
	assert.True(t, ok, "fence's set MUST contain the original boot-time value")
	_, leak := got["test-plugin:added-after-boot"]
	assert.False(t, leak, "fence's set MUST NOT reflect post-boot mutation (INV-CRYPTO-44)")
}

// TestFenceForwardsInnerRouterError verifies a router-level error
// (not a stream-level Next error) is forwarded verbatim.
func TestFenceForwardsInnerRouterError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("plugin client missing")
	fence := history.NewPluginDowngradeFence(
		&fakeRouter{err: wantErr},
		history.WithAlwaysSensitiveTypes(sensitiveSet()),
	)
	_, err := fence.QueryHistory(context.Background(), "test-plugin", eventbus.HistoryQuery{
		Subject: "events.test.scene.01ABC.ic",
	})
	require.ErrorIs(t, err, wantErr)
}

// TestFenceForwardsEOF verifies io.EOF passes through unchanged so
// the wrapping does not break stream termination semantics.
func TestFenceForwardsEOF(t *testing.T) {
	t.Parallel()
	fence := history.NewPluginDowngradeFence(
		&fakeRouter{stream: &fakeFenceStream{}},
		history.WithAlwaysSensitiveTypes(sensitiveSet()),
	)
	out, err := fence.QueryHistory(context.Background(), "test-plugin", eventbus.HistoryQuery{
		Subject: "events.test.scene.01ABC.ic",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = out.Close() })
	_, err = out.Next(context.Background())
	assert.ErrorIs(t, err, io.EOF)
}
