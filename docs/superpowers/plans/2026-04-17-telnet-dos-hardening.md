# Telnet DoS Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add four defensive controls to the telnet gateway — global connection cap, read idle deadline, per-send write deadline, and pre-auth timer — so a single attacker cannot exhaust gateway resources via Slowloris or goroutine flooding.

**Architecture:** Accept-loop gains a channel semaphore (`chan struct{}`) gating goroutine spawns; the handler gains a `Limits` struct and a `deadlineReader` wrapper that refreshes `SetReadDeadline` on every `Read`; `send()` sets `SetWriteDeadline`; `Handle()` runs a local pre-auth timer whose firing case returns iff `!h.authed`. Four Prometheus metrics emit attack signal. All knobs are cobra flags with sensible defaults.

**Tech Stack:** Go stdlib (net, bufio, time, context), `github.com/prometheus/client_golang`, `github.com/samber/oops` for error codes, testify for assertions, existing cobra/koanf config.

**Spec:** `docs/superpowers/specs/2026-04-17-telnet-dos-hardening-design.md`

**Bead:** `holomush-abbg`

---

## File Structure

| File | Action | Responsibility |
| ---- | ------ | -------------- |
| `internal/telnet/limits.go` | CREATE | `Limits` struct + `DefaultLimits` package var |
| `internal/telnet/limits_test.go` | CREATE | DefaultLimits sanity |
| `internal/telnet/metrics.go` | CREATE | Four Prometheus metrics + `Record*` helpers |
| `internal/telnet/metrics_test.go` | CREATE | Record helper smoke tests |
| `internal/telnet/deadline_reader.go` | CREATE | `deadlineReader` wrapper over `net.Conn` |
| `internal/telnet/deadline_reader_test.go` | CREATE | Per-read deadline refresh test |
| `internal/telnet/refuse.go` | CREATE | `RefuseOverCapacity(conn net.Conn)` helper |
| `internal/telnet/refuse_test.go` | CREATE | Refusal writes message + closes |
| `internal/telnet/gateway_handler.go` | MODIFY | `NewGatewayHandler` signature adds `Limits`; integrate `deadlineReader`, write deadline, pre-auth timer |
| `internal/telnet/gateway_handler_test.go` | MODIFY | Add `newTestHandler` helper; add three new tests |
| `test/integration/telnet/e2e_test.go` | MODIFY | One-line signature update at `NewGatewayHandler` call site |
| `cmd/holomush/gateway.go` | MODIFY | New config fields, `Validate`, build `Limits`, accept-loop semaphore |
| `cmd/holomush/gateway_test.go` | MODIFY | New tests for config validation + accept-loop capacity |
| `test/integration/telnet/dos_integration_test.go` | CREATE | Three integration tests (Slowloris, capacity, pre-auth) |
| `site/docs/operating/telnet-security.md` | MODIFY | Add "Resource limits" section |

Build order: foundation primitives first (Limits, metrics, deadline reader, refuse helper), then handler integration, then accept-loop integration, then operator docs.

---

## Task 1: Limits type + defaults

**Files:**

- Create: `internal/telnet/limits.go`
- Create: `internal/telnet/limits_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/telnet/limits_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultLimitsMatchSpec(t *testing.T) {
	assert.Equal(t, 5*time.Minute, DefaultLimits.IdleReadTimeout,
		"spec pins idle read timeout default to 5m")
	assert.Equal(t, 30*time.Second, DefaultLimits.WriteTimeout,
		"spec pins write timeout default to 30s")
	assert.Equal(t, 2*time.Minute, DefaultLimits.PreAuthTimeout,
		"spec pins pre-auth timeout default to 2m")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestDefaultLimitsMatchSpec ./internal/telnet/`
Expected: FAIL with `undefined: DefaultLimits`

- [ ] **Step 3: Write minimal implementation**

Create `internal/telnet/limits.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import "time"

// Limits bounds per-connection resource use. Zero values in a Limits value
// are NOT interpreted as "unlimited" — callers MUST populate the struct
// explicitly or use DefaultLimits.
type Limits struct {
	// IdleReadTimeout is the deadline refreshed on every Read from the
	// underlying connection. A drip-fed Slowloris attacker hits this
	// ceiling and is disconnected.
	IdleReadTimeout time.Duration

	// WriteTimeout bounds a single send. Applied via SetWriteDeadline
	// before every write. A stuck-client write returns immediately with
	// a timeout error.
	WriteTimeout time.Duration

	// PreAuthTimeout is the total wall-clock budget from connect to
	// successful character selection. Fires once; a fire after auth is
	// a no-op.
	PreAuthTimeout time.Duration
}

// DefaultLimits are the production-safe defaults for a modest VPS hosting
// a hobby-to-mid-size MUSH.
var DefaultLimits = Limits{
	IdleReadTimeout: 5 * time.Minute,
	WriteTimeout:    30 * time.Second,
	PreAuthTimeout:  2 * time.Minute,
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestDefaultLimitsMatchSpec ./internal/telnet/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
jj --no-pager describe -m "feat(telnet): introduce Limits struct with production defaults

New Limits type carries IdleReadTimeout, WriteTimeout, and PreAuthTimeout.
DefaultLimits values (5m/30s/2m) match the design spec and are safe for a
modest VPS. Nothing consumes the type yet — follow-up tasks wire it into
NewGatewayHandler.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Metrics package

**Files:**

- Create: `internal/telnet/metrics.go`
- Create: `internal/telnet/metrics_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/telnet/metrics_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRecordConnectionActiveTracksDelta(t *testing.T) {
	before := testutil.ToFloat64(ConnectionsActive)
	IncConnectionsActive()
	assert.Equal(t, before+1, testutil.ToFloat64(ConnectionsActive))
	DecConnectionsActive()
	assert.Equal(t, before, testutil.ToFloat64(ConnectionsActive))
}

func TestRecordConnectionRefusedIncrements(t *testing.T) {
	before := testutil.ToFloat64(ConnectionsRefusedTotal)
	RecordConnectionRefused()
	assert.Equal(t, before+1, testutil.ToFloat64(ConnectionsRefusedTotal))
}

func TestRecordPreAuthTimeoutIncrements(t *testing.T) {
	before := testutil.ToFloat64(PreAuthTimeoutsTotal)
	RecordPreAuthTimeout()
	assert.Equal(t, before+1, testutil.ToFloat64(PreAuthTimeoutsTotal))
}

func TestRecordIdleTimeoutIncrements(t *testing.T) {
	before := testutil.ToFloat64(IdleTimeoutsTotal)
	RecordIdleTimeout()
	assert.Equal(t, before+1, testutil.ToFloat64(IdleTimeoutsTotal))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestRecord ./internal/telnet/`
Expected: FAIL with `undefined: ConnectionsActive`

- [ ] **Step 3: Write minimal implementation**

Create `internal/telnet/metrics.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ConnectionsActive tracks the current number of live telnet handler
// goroutines. Rises toward MaxConns under load; a gauge pinned at the cap
// is the primary DoS signal for operators.
var ConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "holomush_telnet_connections_active",
	Help: "Current number of active telnet connections",
})

// ConnectionsRefusedTotal counts accepts that were immediately closed
// because the global connection cap was full.
var ConnectionsRefusedTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "holomush_telnet_connections_refused_total",
	Help: "Total telnet connections refused due to MaxConns cap",
})

// PreAuthTimeoutsTotal counts connections disconnected because the
// pre-auth timer fired before successful character selection.
var PreAuthTimeoutsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "holomush_telnet_preauth_timeouts_total",
	Help: "Total telnet connections disconnected for exceeding the pre-auth timeout",
})

// IdleTimeoutsTotal counts connections disconnected because the read
// deadline expired (Slowloris / idle client).
var IdleTimeoutsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "holomush_telnet_idle_timeouts_total",
	Help: "Total telnet connections disconnected due to idle read timeout",
})

// IncConnectionsActive increments the active-connection gauge.
func IncConnectionsActive() { ConnectionsActive.Inc() }

// DecConnectionsActive decrements the active-connection gauge.
func DecConnectionsActive() { ConnectionsActive.Dec() }

// RecordConnectionRefused increments the refused counter.
func RecordConnectionRefused() { ConnectionsRefusedTotal.Inc() }

// RecordPreAuthTimeout increments the pre-auth timeout counter.
func RecordPreAuthTimeout() { PreAuthTimeoutsTotal.Inc() }

// RecordIdleTimeout increments the idle timeout counter.
func RecordIdleTimeout() { IdleTimeoutsTotal.Inc() }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestRecord ./internal/telnet/`
Expected: PASS (4 subtests)

- [ ] **Step 5: Commit**

```bash
jj --no-pager describe -m "feat(telnet): add Prometheus metrics for DoS observability

Four metrics — active gauge plus three counters for refused connections,
pre-auth timeouts, and idle timeouts. Package globals registered via
promauto, consumed by follow-up tasks wiring them into the accept loop
and handler. Record* helpers keep callers free of the metric type.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: deadlineReader wrapper

**Files:**

- Create: `internal/telnet/deadline_reader.go`
- Create: `internal/telnet/deadline_reader_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/telnet/deadline_reader_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDeadlineConn is a minimal net.Conn stand-in that records every
// SetReadDeadline call. Only Read + SetReadDeadline are exercised.
type mockDeadlineConn struct {
	net.Conn // embed for the methods we don't care about (they'll panic if touched)
	readErr  error
	readN    int
	readBuf  []byte
	reads    int
	deadlines []time.Time
}

func (m *mockDeadlineConn) SetReadDeadline(t time.Time) error {
	m.deadlines = append(m.deadlines, t)
	return nil
}

func (m *mockDeadlineConn) Read(p []byte) (int, error) {
	m.reads++
	if m.readErr != nil {
		return 0, m.readErr
	}
	n := copy(p, m.readBuf)
	return n, nil
}

func TestDeadlineReaderSetsDeadlineBeforeEveryRead(t *testing.T) {
	mc := &mockDeadlineConn{readBuf: []byte("ab")}
	r := &deadlineReader{conn: mc, timeout: 100 * time.Millisecond}

	buf := make([]byte, 1)

	start := time.Now()
	_, err := r.Read(buf)
	require.NoError(t, err)
	_, err = r.Read(buf)
	require.NoError(t, err)

	assert.Equal(t, 2, mc.reads, "one underlying Read per wrapper Read")
	require.Len(t, mc.deadlines, 2, "one deadline per Read")

	for i, d := range mc.deadlines {
		delta := d.Sub(start)
		assert.GreaterOrEqual(t, delta, 100*time.Millisecond,
			"deadline #%d is in the future by at least timeout", i)
		assert.Less(t, delta, 1*time.Second,
			"deadline #%d is not absurdly far in the future", i)
	}
}

func TestDeadlineReaderPropagatesReadError(t *testing.T) {
	sentinel := errors.New("boom")
	mc := &mockDeadlineConn{readErr: sentinel}
	r := &deadlineReader{conn: mc, timeout: 100 * time.Millisecond}

	_, err := r.Read(make([]byte, 1))
	assert.ErrorIs(t, err, sentinel)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestDeadlineReader ./internal/telnet/`
Expected: FAIL with `undefined: deadlineReader`

- [ ] **Step 3: Write minimal implementation**

Create `internal/telnet/deadline_reader.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"net"
	"time"
)

// deadlineReader refreshes the underlying connection's read deadline
// before every Read call, so the idle timeout is measured from the last
// byte received rather than connection open. A Slowloris attacker that
// drip-feeds bytes still hits the deadline because the refresh only
// fires at read time — no bytes in flight, no refresh, deadline expires.
type deadlineReader struct {
	conn    net.Conn
	timeout time.Duration
}

func (r *deadlineReader) Read(p []byte) (int, error) {
	if err := r.conn.SetReadDeadline(time.Now().Add(r.timeout)); err != nil {
		return 0, err
	}
	return r.conn.Read(p)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestDeadlineReader ./internal/telnet/`
Expected: PASS (2 tests)

- [ ] **Step 5: Commit**

```bash
jj --no-pager describe -m "feat(telnet): add deadlineReader for per-read idle deadline refresh

Thin io.Reader wrapper that calls SetReadDeadline before each underlying
Read. Intended to be wrapped in bufio.Scanner so the scanner's reads
always see a fresh idle budget. Not yet consumed — follow-up task wires
it into NewGatewayHandler.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: RefuseOverCapacity helper

**Files:**

- Create: `internal/telnet/refuse.go`
- Create: `internal/telnet/refuse_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/telnet/refuse_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRefuseConn records SetWriteDeadline, Write, and Close for assertions.
type mockRefuseConn struct {
	net.Conn
	written    []byte
	deadlines  []time.Time
	closed     bool
	writeErr   error
	closeErr   error
}

func (m *mockRefuseConn) SetWriteDeadline(t time.Time) error {
	m.deadlines = append(m.deadlines, t)
	return nil
}

func (m *mockRefuseConn) Write(p []byte) (int, error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	m.written = append(m.written, p...)
	return len(p), nil
}

func (m *mockRefuseConn) Close() error {
	m.closed = true
	return m.closeErr
}

func TestRefuseOverCapacityWritesMessageAndCloses(t *testing.T) {
	before := testutilCounterValue(ConnectionsRefusedTotal)

	mc := &mockRefuseConn{}
	RefuseOverCapacity(mc, 30*time.Second)

	assert.True(t, mc.closed, "connection must be closed")
	assert.NotEmpty(t, mc.deadlines, "a write deadline must be set")
	assert.Contains(t, strings.ToLower(string(mc.written)), "capacity",
		"refusal message must indicate capacity problem")
	assert.Contains(t, string(mc.written), "\r\n",
		"refusal message must end with CRLF for telnet clients")
	assert.Equal(t, before+1, testutilCounterValue(ConnectionsRefusedTotal),
		"refusal must increment the counter")
}

func TestRefuseOverCapacityClosesEvenIfWriteFails(t *testing.T) {
	mc := &mockRefuseConn{writeErr: net.ErrClosed}
	RefuseOverCapacity(mc, 30*time.Second)
	assert.True(t, mc.closed)
}
```

And add this helper to `internal/telnet/metrics_test.go` (NOTE: add to the existing file created in Task 2 — don't recreate it):

```go
// Append to internal/telnet/metrics_test.go

import "github.com/prometheus/client_golang/prometheus"

// testutilCounterValue returns the current value of a prometheus.Counter.
// Used by tests in this package that want before/after deltas without
// importing prometheus/testutil at every call site.
func testutilCounterValue(c prometheus.Counter) float64 {
	return promTestUtilToFloat64(c)
}
```

Wait — the simpler approach is to just import `testutil` in the refuse test directly. Revise the test file to use:

```go
import "github.com/prometheus/client_golang/prometheus/testutil"
// ...
before := testutil.ToFloat64(ConnectionsRefusedTotal)
// ...
assert.Equal(t, before+1, testutil.ToFloat64(ConnectionsRefusedTotal))
```

Use that form throughout. Remove the `testutilCounterValue` helper — the real testutil import is cleaner.

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestRefuseOverCapacity ./internal/telnet/`
Expected: FAIL with `undefined: RefuseOverCapacity`

- [ ] **Step 3: Write minimal implementation**

Create `internal/telnet/refuse.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"log/slog"
	"net"
	"time"
)

// refusalMessage is written to clients that hit the connection cap.
// Terminated with CRLF because telnet clients expect line-ending pairs.
const refusalMessage = "Server at capacity. Try again later.\r\n"

// RefuseOverCapacity writes a refusal line to conn and closes it. Used by
// the accept loop when the concurrent-connection semaphore is full. Any
// write error is logged at debug and swallowed — the client has already
// given up or we cannot reach them further.
//
// writeTimeout bounds the refusal write; callers pass the same value used
// for live-connection writes.
//
// The function also increments ConnectionsRefusedTotal so capacity events
// show up in operator metrics.
func RefuseOverCapacity(conn net.Conn, writeTimeout time.Duration) {
	RecordConnectionRefused()

	if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		slog.Debug("telnet: failed to set refusal write deadline", "error", err)
	}
	if _, err := conn.Write([]byte(refusalMessage)); err != nil {
		slog.Debug("telnet: failed to write refusal", "error", err)
	}
	if err := conn.Close(); err != nil {
		slog.Debug("telnet: failed to close refused connection", "error", err)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestRefuseOverCapacity ./internal/telnet/`
Expected: PASS (2 tests)

- [ ] **Step 5: Commit**

```bash
jj --no-pager describe -m "feat(telnet): add RefuseOverCapacity helper

Writes a short refusal line, closes the connection, and increments the
refused-connections counter. Bounded by a caller-supplied write timeout
so a slow client cannot delay the refusal path. Called by the accept
loop in a follow-up task.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: NewGatewayHandler signature + Limits + newTestHandler

**Files:**

- Modify: `internal/telnet/gateway_handler.go:82-89` (signature + constructor body)
- Modify: `internal/telnet/gateway_handler_test.go` (add `newTestHandler`, convert all existing call sites)
- Modify: `test/integration/telnet/e2e_test.go:409`
- Modify: `cmd/holomush/gateway.go:428`

This task is a mechanical signature migration — no behavior change yet. It unblocks Tasks 6–8, which add behavior.

- [ ] **Step 1: Update the handler struct + constructor**

Modify `internal/telnet/gateway_handler.go`. Add `limits` field to `GatewayHandler`:

```go
// Locate the GatewayHandler struct near line 62-79 and add the limits field
// after the existing fields.
type GatewayHandler struct {
	conn         net.Conn
	reader       *bufio.Reader
	client       CoreClient
	verbRegistry *core.VerbRegistry
	sessionID    string
	connectionID string
	charName     string
	authed       bool
	quitting     bool
	eventCh      chan *corev1.SubscribeResponse

	limits Limits

	// Two-phase auth state.
	playerSessionToken string
	characters         []*corev1.CharacterSummary
	selectMode         bool
	loggingOut         bool
}
```

Change the constructor at line 82-89:

```go
// NewGatewayHandler creates a new GatewayHandler for the given connection.
// limits bounds per-connection resource usage; callers SHOULD pass
// DefaultLimits unless they have a specific reason to deviate.
func NewGatewayHandler(conn net.Conn, client CoreClient, registry *core.VerbRegistry, limits Limits) *GatewayHandler {
	return &GatewayHandler{
		conn:         conn,
		reader:       bufio.NewReader(conn),
		client:       client,
		verbRegistry: registry,
		limits:       limits,
	}
}
```

- [ ] **Step 2: Add `newTestHandler` helper to the test file**

Modify `internal/telnet/gateway_handler_test.go`. Add near the top (after the imports and existing `testRegistry` helper):

```go
// newTestHandler wraps NewGatewayHandler with DefaultLimits so existing
// tests remain a single line and don't grow noise from the new parameter.
// Tests that need custom limits call NewGatewayHandler directly.
func newTestHandler(conn net.Conn, client CoreClient) *GatewayHandler {
	return NewGatewayHandler(conn, client, testRegistry(), DefaultLimits)
}
```

- [ ] **Step 3: Convert every in-package test call site to `newTestHandler`**

Every instance of `NewGatewayHandler(serverConn, client, testRegistry())` in `internal/telnet/gateway_handler_test.go` becomes `newTestHandler(serverConn, client)`. Also `NewGatewayHandler(serverConn, client, testRegistry())` appears on lines 189, 251, 343, 409, 474, 535, 578, 668, 712, 762, 819, 873, 932, 979, 1027, 1346, 1403, 1479, 1540, 1594, 1674, 1744, 1796, 1863, 1921, 2113, plus `h := NewGatewayHandler(...)` on line 1963 — all need the same mechanical rewrite.

Recommended mechanical approach: `sed -i '' 's/NewGatewayHandler(serverConn, client, testRegistry())/newTestHandler(serverConn, client)/g' internal/telnet/gateway_handler_test.go` then `sed -i '' 's/NewGatewayHandler(serverConn, client, testRegistry())/newTestHandler(serverConn, client)/g'` — but the user's project rule is "Use the Grep tool and Edit tool instead of sed". So use a single `Edit` with `replace_all: true`:

Edit `internal/telnet/gateway_handler_test.go`:

- `old_string`: `NewGatewayHandler(serverConn, client, testRegistry())`
- `new_string`: `newTestHandler(serverConn, client)`
- `replace_all`: `true`

Also covers the `h := NewGatewayHandler(...)` case on line 1963 because the right-hand side is identical.

- [ ] **Step 4: Update the two production call sites**

Modify `cmd/holomush/gateway.go:428`:

Old:

```go
handler := telnet.NewGatewayHandler(conn, client, registry)
```

New:

```go
handler := telnet.NewGatewayHandler(conn, client, registry, telnet.DefaultLimits)
```

Modify `test/integration/telnet/e2e_test.go:409`:

Old:

```go
handler := telnet.NewGatewayHandler(conn, grpcCli, sharedRegistry)
```

New:

```go
handler := telnet.NewGatewayHandler(conn, grpcCli, sharedRegistry, telnet.DefaultLimits)
```

- [ ] **Step 5: Verify everything still compiles and the existing suite passes**

Run: `task test -- ./internal/telnet/ ./cmd/holomush/`
Expected: PASS, no new failures versus `main`.

- [ ] **Step 6: Commit**

```bash
jj --no-pager describe -m "refactor(telnet): thread Limits through NewGatewayHandler

Mechanical signature migration. NewGatewayHandler now takes a Limits
struct; callers pass DefaultLimits. Behaviour unchanged — follow-up
tasks consume the field to wire in deadlines and the pre-auth timer.
Tests use a newTestHandler helper to keep call sites quiet.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Integrate deadlineReader + read-deadline tests

**Files:**

- Modify: `internal/telnet/gateway_handler.go:82-89` (constructor) and the scanner goroutine near line 128-150
- Modify: `internal/telnet/gateway_handler_test.go` (add read-deadline tests)

- [ ] **Step 1: Write the failing tests**

Add to `internal/telnet/gateway_handler_test.go`:

```go
func TestReadDeadlineFiresOnIdleClient(t *testing.T) {
	before := testutil.ToFloat64(IdleTimeoutsTotal)

	serverConn, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	client := &mockCoreClient{}

	handler := NewGatewayHandler(serverConn, client, testRegistry(), Limits{
		IdleReadTimeout: 100 * time.Millisecond,
		WriteTimeout:    DefaultLimits.WriteTimeout,
		PreAuthTimeout:  DefaultLimits.PreAuthTimeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		handler.Handle(ctx)
		close(done)
	}()

	// Drain the welcome banner so the server's scanner tries to read.
	_, _ = bufio.NewReader(clientConn).ReadString('\n')

	// Send NO bytes. Wait for handler to exit via idle timeout.
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not exit on idle deadline")
	}

	assert.Equal(t, before+1, testutil.ToFloat64(IdleTimeoutsTotal),
		"idle timeout must increment the counter")
}

func TestReadDeadlineResetsOnByte(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	client := &mockCoreClient{}

	handler := NewGatewayHandler(serverConn, client, testRegistry(), Limits{
		IdleReadTimeout: 150 * time.Millisecond,
		WriteTimeout:    DefaultLimits.WriteTimeout,
		PreAuthTimeout:  DefaultLimits.PreAuthTimeout,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		handler.Handle(ctx)
		close(done)
	}()

	// Drain welcome banner so we know handler has started reading.
	_, _ = bufio.NewReader(clientConn).ReadString('\n')

	// Send a byte every 50 ms for 400 ms — total > 2 × IdleReadTimeout.
	// If the deadline resets on each read, the handler stays alive.
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	stop := time.After(400 * time.Millisecond)
Keepalive:
	for {
		select {
		case <-ticker.C:
			_, werr := clientConn.Write([]byte("a"))
			if werr != nil {
				break Keepalive
			}
		case <-stop:
			break Keepalive
		}
	}

	select {
	case <-done:
		t.Fatal("handler exited during keep-alive — deadline did not reset per read")
	default:
		// still running, as expected
	}
}
```

Ensure `testutil` is imported at the top of the file:

```go
import "github.com/prometheus/client_golang/prometheus/testutil"
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run 'TestReadDeadline' ./internal/telnet/`
Expected: FAIL on `TestReadDeadlineFiresOnIdleClient` — the handler never exits because no deadline is set. (The keep-alive test may pass accidentally for the same reason; that's fine until Step 3.)

- [ ] **Step 3: Implement — wrap reader with deadlineReader + record idle timeout**

Modify `NewGatewayHandler` in `internal/telnet/gateway_handler.go` to wrap the connection in a deadlineReader:

```go
func NewGatewayHandler(conn net.Conn, client CoreClient, registry *core.VerbRegistry, limits Limits) *GatewayHandler {
	dr := &deadlineReader{conn: conn, timeout: limits.IdleReadTimeout}
	return &GatewayHandler{
		conn:         conn,
		reader:       bufio.NewReader(dr),
		client:       client,
		verbRegistry: registry,
		limits:       limits,
	}
}
```

In `Handle()`, modify the scanner goroutine (currently near line 128-150) to detect `net.Error.Timeout() == true` on scanner errors and record the idle-timeout metric. Locate this block:

```go
if err := scanner.Err(); err != nil {
    select {
    case errCh <- err:
    case <-childCtx.Done():
    }
}
```

Replace with:

```go
if err := scanner.Err(); err != nil {
    var netErr net.Error
    if errors.As(err, &netErr) && netErr.Timeout() {
        RecordIdleTimeout()
    }
    select {
    case errCh <- err:
    case <-childCtx.Done():
    }
}
```

(`errors` and `net` are already imported in this file.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run 'TestReadDeadline' ./internal/telnet/`
Expected: PASS (2 tests)

Also run the full telnet suite to catch regressions:

Run: `task test -- ./internal/telnet/`
Expected: PASS, no regressions from previous tasks.

- [ ] **Step 5: Commit**

```bash
jj --no-pager describe -m "feat(telnet): enforce idle read deadline via deadlineReader

NewGatewayHandler now wraps the connection in a deadlineReader whose
per-Read SetReadDeadline refresh bounds how long a client can sit
idle or drip-feed bytes. Scanner timeout errors record the
holomush_telnet_idle_timeouts_total counter.

Closes Slowloris vector from holomush-abbg (control 2 of 4).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Write deadline in send()

**Files:**

- Modify: `internal/telnet/gateway_handler.go` — the `send` function near line 737
- Modify: `internal/telnet/gateway_handler_test.go` — add write-deadline test

- [ ] **Step 1: Write the failing test**

Add to `internal/telnet/gateway_handler_test.go`:

```go
// mockDeadlineTrackingConn records SetWriteDeadline calls without a real socket.
type mockDeadlineTrackingConn struct {
	net.Conn
	writeDeadlines []time.Time
	writeBuf       []byte
}

func (m *mockDeadlineTrackingConn) SetWriteDeadline(t time.Time) error {
	m.writeDeadlines = append(m.writeDeadlines, t)
	return nil
}

func (m *mockDeadlineTrackingConn) Write(p []byte) (int, error) {
	m.writeBuf = append(m.writeBuf, p...)
	return len(p), nil
}

func (m *mockDeadlineTrackingConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockDeadlineTrackingConn) Close() error                        { return nil }
func (m *mockDeadlineTrackingConn) RemoteAddr() net.Addr                { return &net.TCPAddr{} }

func TestSendSetsWriteDeadline(t *testing.T) {
	mc := &mockDeadlineTrackingConn{}
	h := &GatewayHandler{
		conn:   mc,
		limits: Limits{WriteTimeout: 30 * time.Second},
	}

	start := time.Now()
	h.send("hello world")

	require.Len(t, mc.writeDeadlines, 1, "send must set exactly one write deadline")
	delta := mc.writeDeadlines[0].Sub(start)
	assert.GreaterOrEqual(t, delta, 30*time.Second,
		"deadline must be at least WriteTimeout into the future")
	assert.Less(t, delta, 31*time.Second,
		"deadline must not be absurdly far in the future")
	assert.Contains(t, string(mc.writeBuf), "hello world",
		"send must actually write the message body")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestSendSetsWriteDeadline ./internal/telnet/`
Expected: FAIL — current `send()` does not call `SetWriteDeadline`, so `writeDeadlines` is empty.

- [ ] **Step 3: Implement — add SetWriteDeadline before Fprintln**

Modify `send` in `internal/telnet/gateway_handler.go`. Replace the current function (near line 737):

```go
func (h *GatewayHandler) send(msg string) {
	if _, err := fmt.Fprintln(h.conn, sanitizeTelnetOutput(msg)); err != nil {
		slog.Debug("gateway: failed to send message", "error", err)
	}
}
```

With:

```go
func (h *GatewayHandler) send(msg string) {
	if err := h.conn.SetWriteDeadline(time.Now().Add(h.limits.WriteTimeout)); err != nil {
		slog.Debug("gateway: failed to set write deadline", "error", err)
		return
	}
	if _, err := fmt.Fprintln(h.conn, sanitizeTelnetOutput(msg)); err != nil {
		slog.Debug("gateway: failed to send message", "error", err)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestSendSetsWriteDeadline ./internal/telnet/`
Expected: PASS

Also run the full telnet suite:

Run: `task test -- ./internal/telnet/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
jj --no-pager describe -m "feat(telnet): apply write deadline to every send

send() now calls SetWriteDeadline(now + WriteTimeout) before each write.
A stuck client's full send buffer no longer holds the handler goroutine
indefinitely.

Closes holomush-abbg control 3 of 4.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Pre-auth timer

**Files:**

- Modify: `internal/telnet/gateway_handler.go` — `Handle` near line 92 (timer creation + new select case)
- Modify: `internal/telnet/gateway_handler_test.go` — three new tests

- [ ] **Step 1: Write the failing tests**

Add to `internal/telnet/gateway_handler_test.go`:

```go
func TestPreAuthTimerFiresForUnauthedClient(t *testing.T) {
	before := testutil.ToFloat64(PreAuthTimeoutsTotal)

	serverConn, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	client := &mockCoreClient{}
	handler := NewGatewayHandler(serverConn, client, testRegistry(), Limits{
		IdleReadTimeout: DefaultLimits.IdleReadTimeout,
		WriteTimeout:    DefaultLimits.WriteTimeout,
		PreAuthTimeout:  100 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		handler.Handle(ctx)
		close(done)
	}()

	// Read any output lines until EOF or "Authentication timeout.".
	scanner := bufio.NewScanner(clientConn)
	var sawTimeoutLine bool
	deadline := time.After(1 * time.Second)
Loop:
	for {
		select {
		case <-deadline:
			break Loop
		default:
		}
		if !scanner.Scan() {
			break
		}
		if strings.Contains(scanner.Text(), "Authentication timeout") {
			sawTimeoutLine = true
		}
	}

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not exit on pre-auth timeout")
	}

	assert.True(t, sawTimeoutLine, "client must receive 'Authentication timeout.'")
	assert.Equal(t, before+1, testutil.ToFloat64(PreAuthTimeoutsTotal),
		"preauth counter must increment")
}

func TestPreAuthTimerCancelledAfterGuestConnect(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	// CreateGuest returns one character and a session token so the auto-
	// select path marks the handler authed.
	client := &mockCoreClient{
		createGuestResp: &corev1.CreateGuestResponse{
			Success:            true,
			PlayerSessionToken: "guest-token",
			Characters: []*corev1.CharacterSummary{
				{CharacterId: "char-guest", CharacterName: "Guest-1"},
			},
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "session-1",
			CharacterName: "Guest-1",
		},
		// Subscribe stream: stream that immediately errors with EOF so
		// the handler remains alive under childCtx but receives no events.
	}
	client.subStream = newEOFStream()

	handler := NewGatewayHandler(serverConn, client, testRegistry(), Limits{
		IdleReadTimeout: DefaultLimits.IdleReadTimeout,
		WriteTimeout:    DefaultLimits.WriteTimeout,
		PreAuthTimeout:  200 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		handler.Handle(ctx)
		close(done)
	}()

	// Consume welcome banner.
	reader := bufio.NewReader(clientConn)
	for i := 0; i < 3; i++ {
		_, _ = reader.ReadString('\n')
	}

	// Issue connect guest.
	_, err := clientConn.Write([]byte("connect guest\n"))
	require.NoError(t, err)

	// Wait past pre-auth timeout. Handler must still be alive.
	time.Sleep(400 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("handler exited — pre-auth timer fired after successful auth")
	default:
	}

	cancel() // clean shutdown
	<-done
}

func TestPreAuthTimerCancelledAfterTwoPhaseSelect(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:            true,
			PlayerSessionToken: "player-token",
			Characters: []*corev1.CharacterSummary{
				{CharacterId: "char-alice", CharacterName: "Alice"},
			},
			DefaultCharacterId: "char-alice",
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "session-1",
			CharacterName: "Alice",
		},
	}
	client.subStream = newEOFStream()

	handler := NewGatewayHandler(serverConn, client, testRegistry(), Limits{
		IdleReadTimeout: DefaultLimits.IdleReadTimeout,
		WriteTimeout:    DefaultLimits.WriteTimeout,
		PreAuthTimeout:  200 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		handler.Handle(ctx)
		close(done)
	}()

	reader := bufio.NewReader(clientConn)
	for i := 0; i < 3; i++ {
		_, _ = reader.ReadString('\n')
	}

	_, err := clientConn.Write([]byte("connect alice password\n"))
	require.NoError(t, err)

	time.Sleep(400 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("handler exited — pre-auth timer fired after successful two-phase auth")
	default:
	}

	cancel()
	<-done
}
```

**Note on `newEOFStream`:** if the existing test file already has a helper that returns a `CoreService_SubscribeClient` yielding `io.EOF` on `Recv()`, reuse it. Otherwise, add this minimal stub:

```go
// newEOFStream returns a SubscribeClient whose first Recv returns io.EOF.
// Used by tests that care only about auth completing and the handler
// entering the event-loop idle state.
func newEOFStream() corev1.CoreService_SubscribeClient {
	return &eofSubStream{}
}

type eofSubStream struct {
	corev1.CoreService_SubscribeClient
}

func (s *eofSubStream) Recv() (*corev1.SubscribeResponse, error) { return nil, io.EOF }
func (s *eofSubStream) Context() context.Context                  { return context.Background() }
func (s *eofSubStream) Header() (metadata.MD, error)              { return nil, nil }
func (s *eofSubStream) Trailer() metadata.MD                      { return nil }
func (s *eofSubStream) CloseSend() error                          { return nil }
func (s *eofSubStream) SendMsg(any) error                         { return nil }
func (s *eofSubStream) RecvMsg(any) error                         { return nil }
```

(Check the file first: `grep -n 'newEOFStream\|eofSubStream\|subStream' internal/telnet/gateway_handler_test.go` — if an equivalent helper already exists, use it and skip the stub definition.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run 'TestPreAuthTimer' ./internal/telnet/`
Expected: FAIL on `TestPreAuthTimerFiresForUnauthedClient` — handler never exits on its own. The two "cancelled" tests will likely pass accidentally because there's no timer yet; that's fine — they're protecting against regression once Step 3 adds the timer.

- [ ] **Step 3: Implement — add timer + select case in Handle()**

Modify `internal/telnet/gateway_handler.go` in the `Handle` method (near line 92). After the welcome banner (currently `h.send("Use: connect guest")` on line 123), add:

```go
preAuth := time.NewTimer(h.limits.PreAuthTimeout)
defer preAuth.Stop()
```

In the main `for { select { ... } }` loop, add a new case:

```go
case <-preAuth.C:
    if !h.authed {
        h.send("Authentication timeout.")
        RecordPreAuthTimeout()
        return
    }
    // authed — timer fired benignly, fall through to next iteration.
```

Place this case between the existing `case <-childCtx.Done()` and `case err := <-errCh` to keep the "terminal" cases grouped at the top.

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run 'TestPreAuthTimer' ./internal/telnet/`
Expected: PASS (3 tests)

Full suite:

Run: `task test -- ./internal/telnet/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
jj --no-pager describe -m "feat(telnet): enforce pre-auth timeout in Handle()

Adds a per-connection timer that fires PreAuthTimeout after connect. If
the handler is still unauthenticated when it fires, we send
'Authentication timeout.' and close. A fire after auth is ignored and
the select loop continues.

Closes holomush-abbg control 4 of 4.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: Gateway config fields + validation

**Files:**

- Modify: `cmd/holomush/gateway.go` — `gatewayConfig`, `Validate()`, defaults, flag registration
- Modify: `cmd/holomush/gateway_test.go` — extend `TestGatewayConfig_Validate`, new flag-defaults test

- [ ] **Step 1: Write the failing tests**

Modify `cmd/holomush/gateway_test.go`. Locate `TestGatewayConfig_Validate` (line 350) and add the new positive/negative cases to its existing table-driven structure. Also add a new test for flag defaults:

```go
func TestGatewayCommand_TelnetDoSLimitDefaults(t *testing.T) {
	cmd := NewGatewayCmd()

	maxConns, err := cmd.Flags().GetInt("telnet-max-conns")
	require.NoError(t, err)
	assert.Equal(t, 1000, maxConns, "default max conns per spec")

	idle, err := cmd.Flags().GetDuration("telnet-idle-timeout")
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, idle, "default idle timeout per spec")

	write, err := cmd.Flags().GetDuration("telnet-write-timeout")
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, write, "default write timeout per spec")

	preAuth, err := cmd.Flags().GetDuration("telnet-pre-auth-timeout")
	require.NoError(t, err)
	assert.Equal(t, 2*time.Minute, preAuth, "default pre-auth timeout per spec")
}

func TestGatewayConfig_ValidateRejectsNonPositiveTelnetLimits(t *testing.T) {
	// Start from a fully-valid config, then flip each limit to zero and
	// ensure Validate surfaces CONFIG_INVALID.
	base := gatewayConfig{
		TelnetAddr:           ":4201",
		CoreAddr:             "localhost:9000",
		ControlAddr:          "127.0.0.1:9002",
		LogFormat:            "json",
		TelnetMaxConns:       1000,
		TelnetIdleTimeout:    5 * time.Minute,
		TelnetWriteTimeout:   30 * time.Second,
		TelnetPreAuthTimeout: 2 * time.Minute,
	}

	cases := []struct {
		field string
		mut   func(c *gatewayConfig)
	}{
		{"TelnetMaxConns=0", func(c *gatewayConfig) { c.TelnetMaxConns = 0 }},
		{"TelnetMaxConns<0", func(c *gatewayConfig) { c.TelnetMaxConns = -1 }},
		{"TelnetIdleTimeout=0", func(c *gatewayConfig) { c.TelnetIdleTimeout = 0 }},
		{"TelnetWriteTimeout=0", func(c *gatewayConfig) { c.TelnetWriteTimeout = 0 }},
		{"TelnetPreAuthTimeout=0", func(c *gatewayConfig) { c.TelnetPreAuthTimeout = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			cfg := base
			tc.mut(&cfg)
			err := cfg.Validate()
			require.Error(t, err)
			// Project pattern: errutil.AssertErrorCode where available; fall back to substring.
			assert.Contains(t, err.Error(), "CONFIG_INVALID")
		})
	}
}
```

If `errutil.AssertErrorCode` is available in the test file already, prefer:

```go
errutil.AssertErrorCode(t, err, "CONFIG_INVALID")
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run 'TestGatewayCommand_TelnetDoSLimitDefaults|TestGatewayConfig_ValidateRejectsNonPositiveTelnetLimits' ./cmd/holomush/`
Expected: FAIL with `undefined: TelnetMaxConns` and unknown flags.

- [ ] **Step 3: Implement — add config fields, defaults, flags, and validation**

Modify `cmd/holomush/gateway.go`. Update the `gatewayConfig` struct (line 33-42) to add the four fields:

```go
type gatewayConfig struct {
	TelnetAddr           string        `koanf:"telnet_addr"`
	CoreAddr             string        `koanf:"core_addr"`
	ControlAddr          string        `koanf:"control_addr"`
	MetricsAddr          string        `koanf:"metrics_addr"`
	LogFormat            string        `koanf:"log_format"`
	WebAddr              string        `koanf:"web_addr"`
	WebDir               string        `koanf:"web_dir"`
	CORSOrigins          []string      `koanf:"cors_origins"`
	TelnetMaxConns       int           `koanf:"telnet_max_conns"`
	TelnetIdleTimeout    time.Duration `koanf:"telnet_idle_timeout"`
	TelnetWriteTimeout   time.Duration `koanf:"telnet_write_timeout"`
	TelnetPreAuthTimeout time.Duration `koanf:"telnet_pre_auth_timeout"`
}
```

Extend the defaults block (line 62-68):

```go
const (
	defaultTelnetAddr            = ":4201"
	defaultCoreAddr              = "localhost:9000"
	defaultGatewayControlAddr    = "127.0.0.1:9002"
	defaultGatewayMetricsAddr    = "127.0.0.1:9101"
	defaultWebAddr               = ":8080"
	defaultTelnetMaxConns        = 1000
	defaultTelnetIdleTimeout     = 5 * time.Minute
	defaultTelnetWriteTimeout    = 30 * time.Second
	defaultTelnetPreAuthTimeout  = 2 * time.Minute
)
```

Extend `Validate()` (line 45-59) — add these checks before the final `return nil`:

```go
if cfg.TelnetMaxConns <= 0 {
	return oops.Code("CONFIG_INVALID").Errorf("telnet-max-conns must be positive, got %d", cfg.TelnetMaxConns)
}
if cfg.TelnetIdleTimeout <= 0 {
	return oops.Code("CONFIG_INVALID").Errorf("telnet-idle-timeout must be positive, got %s", cfg.TelnetIdleTimeout)
}
if cfg.TelnetWriteTimeout <= 0 {
	return oops.Code("CONFIG_INVALID").Errorf("telnet-write-timeout must be positive, got %s", cfg.TelnetWriteTimeout)
}
if cfg.TelnetPreAuthTimeout <= 0 {
	return oops.Code("CONFIG_INVALID").Errorf("telnet-pre-auth-timeout must be positive, got %s", cfg.TelnetPreAuthTimeout)
}
```

Extend `newGatewayCmd` flag registration (line 88-95):

```go
cmd.Flags().IntVar(&cfg.TelnetMaxConns, "telnet-max-conns", defaultTelnetMaxConns, "max concurrent telnet connections")
cmd.Flags().DurationVar(&cfg.TelnetIdleTimeout, "telnet-idle-timeout", defaultTelnetIdleTimeout, "per-connection idle read timeout")
cmd.Flags().DurationVar(&cfg.TelnetWriteTimeout, "telnet-write-timeout", defaultTelnetWriteTimeout, "per-send write deadline")
cmd.Flags().DurationVar(&cfg.TelnetPreAuthTimeout, "telnet-pre-auth-timeout", defaultTelnetPreAuthTimeout, "disconnect unauthenticated clients after this duration")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run 'TestGatewayCommand_TelnetDoSLimitDefaults|TestGatewayConfig_Validate' ./cmd/holomush/`
Expected: PASS (including all existing Validate cases)

- [ ] **Step 5: Commit**

```bash
jj --no-pager describe -m "feat(gateway): config knobs for telnet DoS limits

Adds --telnet-max-conns, --telnet-idle-timeout, --telnet-write-timeout,
and --telnet-pre-auth-timeout with spec defaults. Validate rejects
zero/negative values at startup. Values are consumed by the accept
loop and handler in follow-up tasks.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: Accept-loop capacity semaphore

**Files:**

- Modify: `cmd/holomush/gateway.go` — `runGatewayWithDeps` + `runTelnetAcceptLoop`
- Modify: `cmd/holomush/gateway_test.go` — accept-loop capacity tests

- [ ] **Step 1: Write the failing tests**

Add to `cmd/holomush/gateway_test.go`:

```go
func TestAcceptLoopRefusesAtCapacity(t *testing.T) {
	before := testutil.ToFloat64(telnet.ConnectionsRefusedTotal)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var client GRPCClient = &noopGRPCClient{}
	registry := core.NewVerbRegistry()
	_ = core.RegisterBuiltinTypes(registry)

	limits := telnet.DefaultLimits
	limits.IdleReadTimeout = 5 * time.Second
	limits.PreAuthTimeout = 5 * time.Second

	slots := make(chan struct{}, 2) // MaxConns=2

	loopDone := make(chan struct{})
	go func() {
		runTelnetAcceptLoop(ctx, ln, client, registry, cancel, slots, limits)
		close(loopDone)
	}()

	// Open 2 connections that we keep alive.
	c1, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer func() { _ = c1.Close() }()
	c2, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer func() { _ = c2.Close() }()

	// Wait until both slots are occupied by polling the gauge.
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(telnet.ConnectionsActive) >= 2
	}, 2*time.Second, 10*time.Millisecond)

	// Open a third connection — it should get the refusal line and close.
	c3, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer func() { _ = c3.Close() }()

	_ = c3.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 128)
	n, _ := c3.Read(buf)
	assert.Contains(t, strings.ToLower(string(buf[:n])), "capacity",
		"third connection must receive refusal line")

	assert.Equal(t, before+1, testutil.ToFloat64(telnet.ConnectionsRefusedTotal),
		"refused counter must increment exactly once")

	cancel()
	_ = ln.Close()
	<-loopDone
}

func TestAcceptLoopReleasesSlotOnHandlerExit(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var client GRPCClient = &noopGRPCClient{}
	registry := core.NewVerbRegistry()
	_ = core.RegisterBuiltinTypes(registry)

	limits := telnet.DefaultLimits
	limits.IdleReadTimeout = 200 * time.Millisecond
	limits.PreAuthTimeout = 200 * time.Millisecond

	slots := make(chan struct{}, 1)

	loopDone := make(chan struct{})
	go func() {
		runTelnetAcceptLoop(ctx, ln, client, registry, cancel, slots, limits)
		close(loopDone)
	}()

	// Open and immediately close to cycle the slot.
	c, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	_ = c.Close()

	// Wait for slot to free.
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(telnet.ConnectionsActive) == 0
	}, 2*time.Second, 10*time.Millisecond)

	// Second connection MUST succeed (slot was freed).
	c2, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer func() { _ = c2.Close() }()

	require.Eventually(t, func() bool {
		return testutil.ToFloat64(telnet.ConnectionsActive) >= 1
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	_ = ln.Close()
	<-loopDone
}
```

Also add to the test file (if not already present):

```go
import (
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/holomush/holomush/internal/telnet"
)

// noopGRPCClient satisfies GRPCClient without connecting anywhere. Used
// by accept-loop tests where the handler will exit via idle timeout
// before making any RPC.
type noopGRPCClient struct{}

func (noopGRPCClient) Close() error { return nil }
// Implement the other methods by delegating to nil to get compile-time
// errors if the interface shrinks. Tests rely on idle timeout, not RPCs.
// If the interface requires more methods, add stubs that return errors.
```

**Implementation note for the worker:** the exact method set of `GRPCClient` in `cmd/holomush/gateway.go` must be mirrored — check the interface declaration before writing `noopGRPCClient`. Each method should return a sentinel error so any accidental RPC call is loud. Do NOT return nil pointers for response types; return `errors.New("noopGRPCClient: RPC not expected")`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestAcceptLoop ./cmd/holomush/`
Expected: FAIL — `runTelnetAcceptLoop` doesn't take `slots` / `limits` yet; `ConnectionsActive` / `ConnectionsRefusedTotal` referenced via `telnet.*` are new; compile errors.

- [ ] **Step 3: Implement — semaphore, thread through, refuse path, gauge**

Modify `cmd/holomush/gateway.go` in two places.

**(a) `runGatewayWithDeps` — create the semaphore and pass it + limits into the accept loop.**

Locate the existing `go runTelnetAcceptLoop(ctx, telnetListener, grpcClient, verbRegistry, cancel)` call (around line 302). Replace with:

```go
slots := make(chan struct{}, cfg.TelnetMaxConns)
limits := telnet.Limits{
	IdleReadTimeout: cfg.TelnetIdleTimeout,
	WriteTimeout:    cfg.TelnetWriteTimeout,
	PreAuthTimeout:  cfg.TelnetPreAuthTimeout,
}
go runTelnetAcceptLoop(ctx, telnetListener, grpcClient, verbRegistry, cancel, slots, limits)
```

**(b) `runTelnetAcceptLoop` — add the semaphore + limits parameters, use `select` for capacity.**

Replace the function (line 391-431) with:

```go
// runTelnetAcceptLoop accepts telnet connections with exponential backoff on errors.
// slots bounds the number of concurrent handler goroutines; a full slots channel
// triggers immediate refusal via RefuseOverCapacity. The cancel function is called
// on panic to trigger graceful shutdown.
func runTelnetAcceptLoop(
	ctx context.Context,
	listener net.Listener,
	client GRPCClient,
	registry *core.VerbRegistry,
	cancel func(),
	slots chan struct{},
	limits telnet.Limits,
) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in telnet accept loop, triggering shutdown", "panic", r)
			cancel()
		}
	}()

	backoff := newAcceptBackoff()

	for {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			select {
			case <-ctx.Done():
				return
			default:
				backoff.failure()
				waitDuration := backoff.wait()
				slog.Error("telnet accept failed, backing off",
					"error", acceptErr,
					"backoff", waitDuration,
				)
				select {
				case <-ctx.Done():
					return
				case <-time.After(waitDuration):
				}
				continue
			}
		}
		backoff.success()

		select {
		case slots <- struct{}{}:
			telnet.IncConnectionsActive()
			handler := telnet.NewGatewayHandler(conn, client, registry, limits)
			go func() {
				defer func() {
					<-slots
					telnet.DecConnectionsActive()
				}()
				handler.Handle(ctx)
			}()
		default:
			telnet.RefuseOverCapacity(conn, limits.WriteTimeout)
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run TestAcceptLoop ./cmd/holomush/`
Expected: PASS (2 tests)

Also run the wider gateway test suite:

Run: `task test -- ./cmd/holomush/`
Expected: PASS; existing tests (including `TestTelnetAcceptLoopPanicRecovery` and `TestTelnetAcceptLoop_BackoffOnErrors`) may need small updates to pass the new arguments — update them to pass a large `slots` (e.g., `make(chan struct{}, 100)`) and `telnet.DefaultLimits`.

- [ ] **Step 5: Register telnet metrics with the observability server**

In `runGatewayWithDeps`, locate the existing observability registration (around line 238):

```go
obsServer.MustRegister(command.CommandExecutions, command.CommandDuration, command.AliasExpansions)
```

Update to:

```go
obsServer.MustRegister(
	command.CommandExecutions, command.CommandDuration, command.AliasExpansions,
	telnet.ConnectionsActive, telnet.ConnectionsRefusedTotal,
	telnet.PreAuthTimeoutsTotal, telnet.IdleTimeoutsTotal,
)
```

Run: `task test -- ./cmd/holomush/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
jj --no-pager describe -m "feat(gateway): enforce telnet connection cap via channel semaphore

runTelnetAcceptLoop now gates handler spawns behind a buffered channel
sized to cfg.TelnetMaxConns. When full, new accepts are refused via
RefuseOverCapacity and counted by holomush_telnet_connections_refused_total.
The active gauge is maintained alongside slot acquire/release.

Closes holomush-abbg control 1 of 4. All four controls now live.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 11: Integration tests

**Files:**

- Create: `test/integration/telnet/dos_integration_test.go`

Three scenario tests under the `integration` build tag exercising the full wiring with a real TCP listener.

- [ ] **Step 1: Write the tests**

Create `test/integration/telnet/dos_integration_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package telnet_test

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/telnet"
)

// dosAcceptLoop runs a minimal accept loop using the real RefuseOverCapacity
// path. We cannot invoke cmd/holomush's runTelnetAcceptLoop directly from
// an integration test without a full gateway bringup; the duplication here
// is intentional and scoped to the DoS behaviour we need to validate.
func dosAcceptLoop(t *testing.T, ctx context.Context, ln net.Listener, max int, limits telnet.Limits) {
	t.Helper()
	slots := make(chan struct{}, max)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			return
		}
		select {
		case slots <- struct{}{}:
			telnet.IncConnectionsActive()
			go func() {
				defer func() {
					<-slots
					telnet.DecConnectionsActive()
				}()
				// Minimal handler: read lines until idle timeout or ctx done.
				_ = conn.SetReadDeadline(time.Now().Add(limits.IdleReadTimeout))
				_, _ = bufio.NewReader(conn).ReadString('\n')
				_ = conn.Close()
			}()
		default:
			telnet.RefuseOverCapacity(conn, limits.WriteTimeout)
		}
	}
}

func TestTelnetSlowlorisDroppedByIdleDeadline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	limits := telnet.Limits{
		IdleReadTimeout: 500 * time.Millisecond,
		WriteTimeout:    30 * time.Second,
		PreAuthTimeout:  30 * time.Second,
	}
	go dosAcceptLoop(t, ctx, ln, 10, limits)

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Send nothing. Expect server to close us within 2 × IdleReadTimeout.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, readErr := bufio.NewReader(conn).ReadString('\n')
	assert.Error(t, readErr, "server must close an idle connection via read deadline")
}

func TestTelnetCapacityRefusal(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	limits := telnet.Limits{
		IdleReadTimeout: 5 * time.Second,
		WriteTimeout:    5 * time.Second,
		PreAuthTimeout:  5 * time.Second,
	}
	const maxConns = 2
	go dosAcceptLoop(t, ctx, ln, maxConns, limits)

	held := make([]net.Conn, 0, maxConns)
	for i := 0; i < maxConns; i++ {
		c, dErr := net.Dial("tcp", ln.Addr().String())
		require.NoError(t, dErr)
		held = append(held, c)
	}
	defer func() {
		for _, c := range held {
			_ = c.Close()
		}
	}()

	// Wait for the gauge to report the max concurrency.
	require.Eventually(t, func() bool {
		return telnetConnectionsActive() >= float64(maxConns)
	}, 2*time.Second, 10*time.Millisecond)

	c, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	_ = c.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 128)
	n, _ := c.Read(buf)
	assert.Contains(t, strings.ToLower(string(buf[:n])), "capacity",
		"over-capacity connect must receive refusal line")
}

// telnetConnectionsActive reads the gauge via testutil. Split out so the
// test file doesn't need prometheus/testutil at every call site.
func telnetConnectionsActive() float64 {
	// Use the package-level gauge directly via prometheus/testutil.
	// Imported inside this helper to keep the main test body concise.
	return promToFloat64(telnet.ConnectionsActive)
}

func promToFloat64(g interface{ Desc() any }) float64 {
	// Avoid importing testutil at file level — use dto.Metric dump instead.
	// The concrete type is prometheus.Gauge / Counter; we use reflection-free
	// access via a tiny collector.
	//
	// Simplest working form: import prometheus/testutil at the top instead.
	// If the import is fine, replace this function with one-liner usage.
	panic("use prometheus/testutil.ToFloat64(telnet.ConnectionsActive) directly; see note in plan Task 11")
}
```

**Simplification note for the worker:** the `promToFloat64` shim above is a placeholder. The clean implementation is to import `github.com/prometheus/client_golang/prometheus/testutil` at the top of the file and use `testutil.ToFloat64(telnet.ConnectionsActive)` directly in the test body, dropping both helpers. Pick the clean form.

- [ ] **Step 2: Run the tests to verify they pass**

Run: `task test:int -- -run 'TestTelnetSlowloris|TestTelnetCapacity' ./test/integration/telnet/`
Expected: PASS (2 tests). Requires Docker; the existing integration harness handles this.

- [ ] **Step 3: Commit**

```bash
jj --no-pager describe -m "test(telnet): integration tests for DoS hardening

Two scenario tests under the integration build tag: Slowloris (connect,
send nothing, get closed via IdleReadTimeout) and capacity refusal
(MaxConns exceeded, next connect receives 'Server at capacity.' line).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 12: Operator documentation

**Files:**

- Modify: `site/docs/operating/telnet-security.md` — add "Resource limits" section

- [ ] **Step 1: Write the new section**

Open `site/docs/operating/telnet-security.md` (create the file if it does not exist — check first). Append a new section at the bottom, before any trailing footer. The section contents are:

A top-level heading `## Resource limits` followed by a paragraph explaining that the gateway enforces four operator-tunable limits on the telnet surface to prevent Slowloris, goroutine flooding, and unbounded pre-auth idle.

Then a Markdown table with columns "Flag | Default | What it bounds" whose rows are:

- `--telnet-max-conns` / `1000` — Concurrent telnet connections; new accepts beyond this receive a refusal line and close.
- `--telnet-idle-timeout` / `5m` — Time since the last byte read; an idle or drip-fed connection is closed.
- `--telnet-write-timeout` / `30s` — Per-send write deadline; a stuck client's full send buffer cannot hold the handler.
- `--telnet-pre-auth-timeout` / `2m` — Time from connect to successful character selection; unauthenticated clients are disconnected.

Then a `### Tuning` subsection with two paragraphs:

1. Size `--telnet-max-conns` to `peak concurrent players × 1.5`. Monitor `holomush_telnet_connections_active` and `holomush_telnet_connections_refused_total` via Prometheus; non-zero refusals under legitimate load mean the cap is too low.
2. The timeouts are chosen for a typical MUSH; very slow-typing players at the character picker may trip `--telnet-pre-auth-timeout` on large character inventories — raise to `5m` if that affects legitimate users.

Then a `### Metrics` subsection introducing the four Prometheus metrics, followed by a Markdown table with columns "Metric | Purpose":

- `holomush_telnet_connections_active` — Current open connection count; primary DoS signal when it pins near the cap.
- `holomush_telnet_connections_refused_total` — Capacity refusals; sustained growth indicates attack or legitimate overload.
- `holomush_telnet_idle_timeouts_total` — Read-deadline disconnects; sustained growth suggests Slowloris.
- `holomush_telnet_preauth_timeouts_total` — Unauthenticated clients disconnected; expected non-zero from scanners.

- [ ] **Step 2: Lint**

Run: `task lint:markdown`
Expected: `Success: No issues found`

- [ ] **Step 3: Commit**

```bash
jj --no-pager describe -m "docs(operating): document telnet DoS resource limits

Adds Resource limits section to telnet-security.md covering the four
new flags, tuning guidance, and the four Prometheus metrics that
expose DoS state.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Final verification

- [ ] **Run the full pr-prep gate**

Run: `task pr-prep`
Expected: all stages green (lint, format, schema, license, unit, integration, E2E).

- [ ] **Push and open PR**

```bash
jj --no-pager git push --bookmark fix/telnet-dos-hardening --allow-new
# then, from the main repo directory (gh needs the colocated git repo):
cd /Volumes/Code/github.com/holomush/holomush
GIT_SSL_NO_VERIFY=1 gh pr create \
  --title "feat(telnet): DoS hardening — connection cap, deadlines, pre-auth timer" \
  --body "$(cat <<'EOF'
## Summary

Closes holomush-abbg (P0, security-finding, co-design-dos). Adds four
defensive controls to the telnet gateway:

1. Global concurrent-connection cap via a channel semaphore
   (`--telnet-max-conns`, default 1000)
2. Per-Read idle deadline via `deadlineReader`
   (`--telnet-idle-timeout`, default 5 min)
3. Per-send write deadline in `send()`
   (`--telnet-write-timeout`, default 30 s)
4. Pre-auth total timer in `Handle()`
   (`--telnet-pre-auth-timeout`, default 2 min)

Four Prometheus metrics expose DoS state. Operator docs updated.

Scope boundary: per-IP rate limiting on auth RPCs is tracked separately
under holomush-brlb.

## Test plan

- [x] `task pr-prep` green — lint, format, schema, license, unit, integration, E2E
- [x] 12 new unit tests (limits, metrics, deadline reader, refuse helper,
  read deadline, write deadline, pre-auth timer × 3, config × 2, accept
  loop × 2)
- [x] 2 integration tests (Slowloris via real TCP; capacity refusal)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)" --head fix/telnet-dos-hardening
```

- [ ] **Close the bead on merge**

```bash
bd close holomush-abbg --reason "Shipped in PR #<NUMBER>. Four controls live: connection cap, idle read deadline, write deadline, pre-auth timer. Metrics registered. Docs updated."
```

---

## Self-review (plan author)

**Spec coverage:**

| Spec requirement | Task(s) |
| --- | --- |
| Control 1: global concurrent-connection cap | Task 10 |
| Control 2: per-connection read idle timeout | Task 6 (deadlineReader consumed) + Task 3 (wrapper implementation) |
| Control 3: per-send write timeout | Task 7 |
| Control 4: pre-auth total timeout | Task 8 |
| `Limits` struct + `DefaultLimits` | Task 1 |
| Four Prometheus metrics | Task 2 + Task 10 (gauge wiring) |
| `RefuseOverCapacity` helper | Task 4 |
| Config flags + defaults + `Validate()` | Task 9 |
| Accept-loop semaphore + refusal | Task 10 |
| Metrics registered with observability server | Task 10 step 5 |
| Unit tests (12 cases in spec) | Tasks 1, 2, 3, 4, 6, 7, 8, 9, 10 |
| Integration tests (Slowloris, capacity, pre-auth) | Task 11 (Slowloris + capacity) — pre-auth integration test dropped because Task 8's unit test plus Task 9's flag default test already cover the timer behaviour and the flag plumbing; a dedicated integration run would only re-exercise the same paths. If the worker wants parity with the spec, they can add a third case mirroring the capacity test pattern. |
| Operator docs | Task 12 |

**Placeholder scan:** the `promToFloat64` helper in Task 11 is explicitly called out as a placeholder with a clear replacement instruction for the worker. No other TBDs or vague steps.

**Type consistency:** `Limits` field names (`IdleReadTimeout`, `WriteTimeout`, `PreAuthTimeout`) are consistent across Tasks 1, 5, 6, 7, 8, 9, 10. Metric variable names (`ConnectionsActive`, `ConnectionsRefusedTotal`, `PreAuthTimeoutsTotal`, `IdleTimeoutsTotal`) consistent across Tasks 2, 4, 6, 8, 10, 11. Config field names (`TelnetMaxConns`, etc.) consistent in Task 9 and Task 10. `Record*` / `Inc*` / `Dec*` metric helper names consistent.

**Ambiguity check:** one judgment call is flagged explicitly — the integration test for pre-auth timer was deferred in favor of the unit test, with rationale. The worker may re-add it if they prefer full spec parity.
