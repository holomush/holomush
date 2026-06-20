// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.opentelemetry.io/otel/trace"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

// ctxCapturingHandler is a slog.Handler that records the context.Context passed
// to the most recent Handle call. It lets a test prove that a log call used a
// *Context slog variant (which forwards the caller's ctx) rather than a bare
// variant (which logs with context.Background(), silently dropping whatever ctx
// the caller holds) — the distinction that decides whether a log line carries
// trace_id/span_id (.claude/rules/logging.md).
type ctxCapturingHandler struct {
	mu      sync.Mutex
	lastCtx context.Context
}

func (h *ctxCapturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *ctxCapturingHandler) Handle(ctx context.Context, _ slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastCtx = ctx
	return nil
}

func (h *ctxCapturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *ctxCapturingHandler) WithGroup(string) slog.Handler      { return h }

func (h *ctxCapturingHandler) capturedSpanContext() trace.SpanContext {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lastCtx == nil {
		// Handle was never called (guard path not reached). Return a zero
		// SpanContext so the assertion fails readably rather than relying on a
		// nil ctx flowing into trace.SpanContextFromContext.
		return trace.SpanContext{}
	}
	return trace.SpanContextFromContext(h.lastCtx)
}

// installCapturingDefaultLogger swaps slog's default logger for one backed by a
// ctxCapturingHandler and restores the original on cleanup. The guard log calls
// under test use the package-level slog.* functions, which route through
// slog.Default().
func installCapturingDefaultLogger(t *testing.T) *ctxCapturingHandler {
	t.Helper()
	h := &ctxCapturingHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

// tracedTestContext returns a context carrying a valid, sampled SpanContext so a
// test can assert that the span survives the log call (i.e. the ctx was
// threaded through rather than dropped).
func tracedTestContext() (context.Context, trace.TraceID) {
	traceID := trace.TraceID{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		TraceFlags: trace.FlagsSampled,
	})
	return trace.ContextWithSpanContext(context.Background(), sc), traceID
}

func TestJoinFocusNotInitializedGuardLogCarriesTraceContext(t *testing.T) {
	h := installCapturingDefaultLogger(t)

	// focus ops nil → getFocusOps returns nil → the "focus ops not initialized"
	// guard fires before the function derives its own ctx.
	L := newFocusTestState(t, nil, nil)
	ctx, wantTraceID := tracedTestContext()
	L.SetContext(ctx)

	require.NoError(t, L.DoString(`holomush.join_focus("sess-1", "scene", "ignored")`))

	sc := h.capturedSpanContext()
	require.True(t, sc.IsValid(),
		"guard log must carry the Lua state's trace context (use slog.WarnContext with a derived ctx)")
	assert.Equal(t, wantTraceID, sc.TraceID(), "log ctx must carry the same trace as the Lua state")
}

func TestJoinFocusGuardLogUsesBackgroundWhenLuaStateHasNoTraceContext(t *testing.T) {
	h := installCapturingDefaultLogger(t)

	// No L.SetContext → luaContext falls back to context.Background(), which
	// carries no span. This pins the negative half of the contract: the guard
	// log degrades to an un-correlated line rather than fabricating a span.
	L := newFocusTestState(t, nil, nil)

	require.NoError(t, L.DoString(`holomush.join_focus("sess-1", "scene", "ignored")`))

	sc := h.capturedSpanContext()
	assert.False(t, sc.IsValid(),
		"with no ctx on the Lua state, the guard log must carry no trace context (Background fallback)")
}

func TestAddSessionStreamNotInitializedGuardLogCarriesTraceContext(t *testing.T) {
	h := installCapturingDefaultLogger(t)

	// No stream registry → getStreamRegistry returns nil → the "stream registry
	// not initialized" guard fires before the function derives its own ctx.
	L := lua.NewState()
	t.Cleanup(L.Close)
	hf := hostfunc.New(nil)
	hf.Register(L, "test-plugin")

	ctx, wantTraceID := tracedTestContext()
	L.SetContext(ctx)

	require.NoError(t, L.DoString(`holomush.add_session_stream("sess-1", "channel:abc")`))

	sc := h.capturedSpanContext()
	require.True(t, sc.IsValid(),
		"guard log must carry the Lua state's trace context (use slog.WarnContext with a derived ctx)")
	assert.Equal(t, wantTraceID, sc.TraceID(), "log ctx must carry the same trace as the Lua state")
}

func TestRemoveSessionStreamNotInitializedGuardLogCarriesTraceContext(t *testing.T) {
	h := installCapturingDefaultLogger(t)

	// No stream registry → getStreamRegistry returns nil → the "stream registry
	// not initialized" guard fires before the function derives its own ctx.
	L := lua.NewState()
	t.Cleanup(L.Close)
	hf := hostfunc.New(nil)
	hf.Register(L, "test-plugin")

	ctx, wantTraceID := tracedTestContext()
	L.SetContext(ctx)

	require.NoError(t, L.DoString(`holomush.remove_session_stream("sess-1", "channel:abc")`))

	sc := h.capturedSpanContext()
	require.True(t, sc.IsValid(),
		"guard log must carry the Lua state's trace context (use slog.WarnContext with a derived ctx)")
	assert.Equal(t, wantTraceID, sc.TraceID(), "log ctx must carry the same trace as the Lua state")
}

func TestKVGetStoreUnavailableGuardLogCarriesTraceContext(t *testing.T) {
	h := installCapturingDefaultLogger(t)

	L := lua.NewState()
	t.Cleanup(L.Close)

	// nil kv store, engine allows → ABAC passes and execution reaches the
	// nil-store guard, which fires before the function derives its own ctx.
	hf := hostfunc.New(nil, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	ctx, wantTraceID := tracedTestContext()
	L.SetContext(ctx)

	require.NoError(t, L.DoString(`val, err = holomush.kv_get("key")`))

	sc := h.capturedSpanContext()
	require.True(t, sc.IsValid(),
		"guard log must carry the Lua state's trace context (use slog.ErrorContext with a derived ctx)")
	assert.Equal(t, wantTraceID, sc.TraceID(), "log ctx must carry the same trace as the Lua state")
}
