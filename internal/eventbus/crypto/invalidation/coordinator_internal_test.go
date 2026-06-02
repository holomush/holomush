// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invalidation

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/holomush/holomush/internal/cluster/clustertest"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// newTestCoordinator builds a *coordinator (concrete) wired to a
// real NATS connection so handleInvalidate's protocol assumptions
// (msg.Reply / Conn.Publish) work end-to-end. Logger is captured
// into a buffer so tests can assert on warn-log breadcrumbs. Metrics
// is intentionally left nil so tests exercise the nil-Metrics branches.
func newTestCoordinator(t *testing.T, logBuf *bytes.Buffer) *coordinator {
	t.Helper()
	h := clustertest.New(t, "test-game", 1)
	logger := slog.New(slog.NewJSONHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cache := dek.NewCache(dek.CacheConfig{})
	partCache := dek.NewParticipantsCache(dek.CacheConfig{})
	deps := Deps{
		Conn:      h.Embedded.Conn,
		Registry:  h.Members[0].Registry,
		DEKCache:  cache,
		PartCache: partCache,
		Logger:    logger,
	}
	cfg := Config{
		ClusterID:         "test-game",
		InvalidateTimeout: 100 * time.Millisecond,
	}.Defaults()
	return &coordinator{cfg: cfg, deps: deps}
}

func TestHandleInvalidateDropsCrossClusterMessage(t *testing.T) {
	logBuf := &bytes.Buffer{}
	c := newTestCoordinator(t, logBuf)

	// Subscribe to a real reply subject so we can assert that
	// handleInvalidate's INV-CLUSTER-4 cross-cluster drop path did NOT publish
	// an ack. (Setting msg.Reply = "" wouldn't catch a bug where the
	// drop path forgot the early-return: the empty-Reply branch's
	// "msg.Reply empty" warn-log would mask the regression.)
	const replySubj = "_INBOX.cross_cluster_drop_test"
	ackCh := make(chan *nats.Msg, 1)
	sub, err := c.deps.Conn.Subscribe(replySubj, func(m *nats.Msg) {
		select {
		case ackCh <- m:
		default:
		}
	})
	if err != nil {
		t.Fatalf("subscribe reply listener: %v", err)
	}
	t.Cleanup(func() {
		if uerr := sub.Unsubscribe(); uerr != nil {
			t.Errorf("sub.Unsubscribe: %v", uerr)
		}
	})

	// Build a payload with a mismatched cluster_id; cluster_id-mismatch
	// must be dropped without ack (INV-CLUSTER-4).
	payload := Payload{
		Seq:                 1,
		CoordinatorMemberID: "01HSENDERAAAAAAAAAAAAAAA",
		ClusterID:           "other-game", // mismatch
		ContextType:         "scene",
		ContextID:           "01HSCENE",
		Action:              ActionRekey,
		IssuedAt:            time.Now(),
	}
	body, err := MarshalPayload(payload)
	if err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}
	msg := &nats.Msg{Data: body, Reply: replySubj}

	// Should not panic, should not ack, and MUST log the drop.
	c.handleInvalidate(msg)

	if !strings.Contains(logBuf.String(), "cross-cluster message dropped") {
		t.Errorf("expected 'cross-cluster message dropped' warning in log; got: %s", logBuf.String())
	}

	// INV-CLUSTER-4 requires the message be dropped WITHOUT ack. Wait briefly
	// for any wrongly-published reply to land; assert nothing arrives.
	select {
	case stray := <-ackCh:
		t.Errorf("INV-CLUSTER-4 violation: handleInvalidate sent ack on cross-cluster drop path; got reply with data %q", string(stray.Data))
	case <-time.After(100 * time.Millisecond):
		// Expected: silence on the reply subject.
	}
}

func TestHandleInvalidateDropsUnknownAction(t *testing.T) {
	logBuf := &bytes.Buffer{}
	c := newTestCoordinator(t, logBuf)

	payload := Payload{
		Seq:                 2,
		CoordinatorMemberID: "01HSENDERAAAAAAAAAAAAAAA",
		ClusterID:           "test-game",
		ContextType:         "scene",
		ContextID:           "01HSCENE",
		Action:              Action("bogus-unknown"),
		IssuedAt:            time.Now(),
	}
	body, err := MarshalPayload(payload)
	if err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}
	msg := &nats.Msg{Data: body}

	c.handleInvalidate(msg) // must not panic

	if !strings.Contains(logBuf.String(), "unknown action") {
		t.Errorf("expected 'unknown action' warning in log; got: %s", logBuf.String())
	}
}

func TestHandleInvalidateDropsParseError(t *testing.T) {
	logBuf := &bytes.Buffer{}
	c := newTestCoordinator(t, logBuf)

	msg := &nats.Msg{Data: []byte("not-json-at-all")}

	c.handleInvalidate(msg) // must not panic

	if !strings.Contains(logBuf.String(), "parse failed") {
		t.Errorf("expected 'parse failed' warning in log; got: %s", logBuf.String())
	}
}

func TestRecordSuccessIsNoOpWhenMetricsNil(t *testing.T) {
	logBuf := &bytes.Buffer{}
	c := newTestCoordinator(t, logBuf)

	// Metrics is nil; recordSuccess MUST early-return without panic.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("recordSuccess panicked with nil Metrics: %v", r)
		}
	}()
	c.recordSuccess(ActionRekey, "success", time.Now())
}

func TestHandleInvalidateLogsWhenReplyEmptyForKnownAction(t *testing.T) {
	// Known action with empty msg.Reply: action processing runs
	// (eviction succeeds), reply marshals fine, and the empty-Reply
	// branch logs "msg.Reply empty; cannot ack" without panicking.
	logBuf := &bytes.Buffer{}
	c := newTestCoordinator(t, logBuf)

	ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE"}
	c.deps.DEKCache.Put(dek.CacheKey{KeyID: 1, Version: 1}, ctxID,
		dek.NewMaterial(make([]byte, dek.DEKByteLength)))

	payload := Payload{
		Seq:                 3,
		CoordinatorMemberID: c.deps.Registry.Self(),
		ClusterID:           "test-game",
		ContextType:         ctxID.Type,
		ContextID:           ctxID.ID,
		Action:              ActionRekey,
		IssuedAt:            time.Now(),
		Version:             1,
		SuccessorVersion:    2,
	}
	body, err := MarshalPayload(payload)
	if err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}
	msg := &nats.Msg{Data: body, Reply: ""} // explicit empty Reply

	c.handleInvalidate(msg) // must not panic

	if !strings.Contains(logBuf.String(), "msg.Reply empty") {
		t.Errorf("expected 'msg.Reply empty' warning in log; got: %s", logBuf.String())
	}
	// Eviction-side effect: confirm ActionRekey path actually ran.
	if _, ok := c.deps.DEKCache.Get(dek.CacheKey{KeyID: 1, Version: 1}); ok {
		t.Errorf("DEK cache entry still present; ActionRekey eviction did not run")
	}
}

func TestConfigDefaultsAppliesInvalidateTimeoutWhenNonPositive(t *testing.T) {
	got := Config{InvalidateTimeout: 0}.Defaults()
	if got.InvalidateTimeout != 5*time.Second {
		t.Errorf("Defaults() with zero timeout = %v; want 5s default", got.InvalidateTimeout)
	}

	gotNeg := Config{InvalidateTimeout: -1 * time.Second}.Defaults()
	if gotNeg.InvalidateTimeout != 5*time.Second {
		t.Errorf("Defaults() with negative timeout = %v; want 5s default", gotNeg.InvalidateTimeout)
	}
}

func TestConfigDefaultsPreservesPositiveInvalidateTimeout(t *testing.T) {
	want := 7 * time.Second
	got := Config{InvalidateTimeout: want}.Defaults()
	if got.InvalidateTimeout != want {
		t.Errorf("Defaults() with positive timeout = %v; want preserved %v", got.InvalidateTimeout, want)
	}
}

func TestTimeoutForActionKEKRotationReturnsThirtySeconds(t *testing.T) {
	c := &coordinator{cfg: Config{InvalidateTimeout: 5 * time.Second}}
	// KEK rotation has its own 30s budget per INV-CLUSTER-1; the configured
	// InvalidateTimeout (default 5s) MUST NOT apply.
	if got := c.timeoutFor(ActionKEKRotation); got != 30*time.Second {
		t.Errorf("timeoutFor(KEKRotation) = %v; want 30s (INV-CLUSTER-1 budget)", got)
	}
}

func TestTimeoutForOtherActionsReturnsConfiguredInvalidateTimeout(t *testing.T) {
	c := &coordinator{cfg: Config{InvalidateTimeout: 7 * time.Second}}
	for _, a := range []Action{ActionRotate, ActionRekey, ActionParticipantsChanged} {
		if got := c.timeoutFor(a); got != 7*time.Second {
			t.Errorf("timeoutFor(%v) = %v; want configured InvalidateTimeout 7s", a, got)
		}
	}
}
