package wasm_test

import (
	"context"
	"sync"
	"testing"

	"github.com/holomush/holomush/internal/wasm"
	"go.opentelemetry.io/otel/trace/noop"
)

// Shared test infrastructure for wasm package tests.
//
// The echo plugin is large (~10.8MB) and takes ~1.5s to compile. By sharing
// a pre-loaded host across tests that only read from it, we avoid recompiling
// the plugin for each test.
//
// IMPORTANT: Extism plugins are NOT thread-safe for concurrent calls. The WASM
// memory gets corrupted when the same plugin instance handles multiple concurrent
// calls. Therefore:
//   - Tests using t.Parallel() CANNOT share a host
//   - Only sequential tests (no t.Parallel()) can share a host
//
// This reduces test time from ~35s to ~17s by avoiding redundant plugin compilation.

var (
	sharedEchoHost     *wasm.ExtismHost
	sharedEchoHostOnce sync.Once
	sharedEchoHostErr  error
)

// getSharedEchoHost returns a pre-loaded ExtismHost with the echo plugin.
// The host is shared across all tests in the package.
//
// IMPORTANT: Tests using this host MUST:
//   - NOT call t.Parallel() (concurrent DeliverEvent causes memory corruption)
//   - NOT close the host
//   - NOT load additional plugins
//   - NOT modify the host state in any way
//
// For parallel tests or tests that modify host state, use newIsolatedHost instead.
func getSharedEchoHost(t testing.TB) *wasm.ExtismHost {
	t.Helper()
	sharedEchoHostOnce.Do(func() {
		tracer := noop.NewTracerProvider().Tracer("test")
		sharedEchoHost = wasm.NewExtismHost(tracer)
		sharedEchoHostErr = sharedEchoHost.LoadPlugin(context.Background(), "echo", echoWASM)
	})
	if sharedEchoHostErr != nil {
		t.Fatalf("failed to load shared echo plugin: %v", sharedEchoHostErr)
	}
	return sharedEchoHost
}

// newIsolatedHost creates a fresh ExtismHost for tests that need isolation.
// The caller is responsible for closing it with defer host.Close(ctx).
//
// Use this when your test:
//   - Calls t.Parallel() (required - concurrent DeliverEvent is not safe)
//   - Needs to close the host
//   - Loads plugins other than echo
//   - Tests host lifecycle behavior
//   - Tests behavior with closed hosts
func newIsolatedHost(t testing.TB) *wasm.ExtismHost {
	t.Helper()
	tracer := noop.NewTracerProvider().Tracer("test")
	return wasm.NewExtismHost(tracer)
}
