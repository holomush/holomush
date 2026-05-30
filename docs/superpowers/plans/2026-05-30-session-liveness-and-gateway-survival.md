<!--
SPDX-License-Identifier: Apache-2.0
Copyright 2026 HoloMUSH Contributors
-->

# Session Liveness & Gateway Survival Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `active`/`grid_present` derive from a gateway-refreshed connection lease that decays on its own, so a disconnected session (e.g. across a core redeploy) stops appearing in presence, and hold client transports across a core restart via durable-consumer reconnect.

**Architecture:** A per-connection lease (`session_connections.last_seen_at`) refreshed by the gateway while the client socket is open; a reaper sweep reaps lapsed connections and recomputes session liveness; the web/telnet gateways refresh the lease and survive core-stream breaks by re-`Subscribe`-ing the durable per-session JetStream consumer (server-side acked-seq resume) and deduping the un-acked overlap. Presence reads the now-honest `grid_present` flag.

**Tech Stack:** Go, PostgreSQL (pgx + `pgnanos` ns-epoch bigints), embedded NATS JetStream (durable consumers), ConnectRPC/gRPC (`corev1`/`webv1`), testify + Ginkgo (integration) + Playwright (E2E).

**Spec:** `docs/superpowers/specs/2026-05-30-session-liveness-and-gateway-survival-design.md` (invariants I-LIVE-1..5, I-PRES-1, I-SURV-1..5, I-SEC-1).

**Sequencing constraint (from the spec):** P1's lease *sweep* MUST NOT be enabled in production before P2's *refresher* exists, or live sessions get reaped. This plan orders refresh (Phase 2) before the sweep is wired into the running reaper (the sweep store/reaper code lands in Phase 1 but its `Run`-loop activation is the last step of Phase 2). Within one epic landing as a coordinated set this is just task ordering.

---

## File Structure

| File | Responsibility | Tasks |
| --- | --- | --- |
| `internal/store/migrations/000045_session_connection_last_seen.{up,down}.sql` | add `last_seen_at` + index | T1 |
| `internal/session/session.go` | `Store` interface: add `RefreshConnection`, `ListLapsedConnections`; `LapsedConnection` type | T2 |
| `internal/store/session_store.go` | implement the two methods; stamp `last_seen_at` in `AddConnection`; `grid_present` predicate on `ListActiveByLocation` | T2, T12 |
| `internal/session/mocks/mock_Store.go` | regenerated mock | T2 |
| `api/proto/holomush/core/v1/core.proto` | `RefreshConnection` RPC + messages | T3 |
| `api/proto/holomush/web/v1/web.proto` | `CONTROL_SIGNAL_RECONNECTING`/`RECONNECTED` enum values | T9 |
| `internal/grpc/refresh_connection.go` | `RefreshConnection` handler (ownership-validated) | T3 |
| `internal/grpc/server.go` | `recomputeSessionLiveness` extracted from `Disconnect` lifecycle; call on `AddConnection` | T4 |
| `internal/session/reaper.go` | lease sweep + boot-grace; `ReaperConfig` callbacks | T5 |
| `cmd/holomush/sub_grpc.go` | wire lease-sweep config + callbacks (engine in scope) | T6 |
| `internal/web/handler.go` | refresh on heartbeat (T7); survival reconnect loop (T10) | T7, T10 |
| `internal/telnet/gateway_handler.go` | refresh ticker (T8); survival reconnect (T11) | T8, T11 |
| `test/integration/presence/*_test.go`, `web/e2e/*.spec.ts`, `test/meta/*_test.go` | integration + E2E + invariant-bijection meta-test | T13–T16 |

> **Test-fixture convention:** integration tests construct an `*auth.PlayerSession`
> and `*session.Info` and seed them via `sessiontest.SeedPlayerSession(t, pool, ps)`
> + `store.Set(ctx, sess.ID, sess)`, following
> `test/integration/session/session_persistence_suite_test.go`. The
> `sessiontest.NewPlayerSession()` / `NewActiveSession(ps)` calls in the snippets
> below are **shorthand** for that construction — add them as local test helpers
> (they are not yet in `sessiontest`). `sessiontest.NewStoreWithPool(t)` returns
> `(store, pool)` in that order. Proto/codegen regeneration is `task proto`.
> Gateway test helpers in the web/telnet snippets (`runStreamEventsBriefly`,
> `captureClientFrames`, `newReconnectingCoreClient`, `newAuthedHandler`,
> `fake.refreshCalls()`, `fake.subscribeCalls()`, etc.) are likewise
> implementer-supplied scaffolding mirroring the fakes already in
> `internal/web/handler_test.go` / `internal/telnet/gateway_handler_test.go`.
>
> **Model intent (Rule 5):** default `model:sonnet` for every task (the sub-agent
> floor is sonnet — never haiku, per `.claude/rules/subagent-briefing.md`). Tasks
> **5** (lease sweep + reaper concurrency), **10** and **11** (gateway reconnect
> state machine) warrant `model:opus`. `plan-to-beads` applies these as `model:*`
> labels on the materialized beads.

---

## Phase 1 — Core connection lease

### Task 1: Migration — add `last_seen_at` lease column

**Files:**

- Create: `internal/store/migrations/000045_session_connection_last_seen.up.sql`
- Create: `internal/store/migrations/000045_session_connection_last_seen.down.sql`

- [ ] **Step 1: Write the up migration**

`000045_session_connection_last_seen.up.sql`:

```sql
-- Connection lease: last_seen_at is refreshed by the gateway while the client
-- socket is open (holomush-rsoe6). A connection whose last_seen_at is older
-- than the lease TTL is reaped, decoupling liveness from the stored status enum.
ALTER TABLE session_connections
  ADD COLUMN IF NOT EXISTS last_seen_at BIGINT NOT NULL DEFAULT 0;

-- Backfill existing rows: treat their connect time as their last-seen time.
UPDATE session_connections SET last_seen_at = connected_at WHERE last_seen_at = 0;

CREATE INDEX IF NOT EXISTS idx_session_connections_last_seen
  ON session_connections (last_seen_at);
```

- [ ] **Step 2: Write the down migration**

`000045_session_connection_last_seen.down.sql`:

```sql
DROP INDEX IF EXISTS idx_session_connections_last_seen;
ALTER TABLE session_connections DROP COLUMN IF EXISTS last_seen_at;
```

- [ ] **Step 3: Verify migration round-trips on a scratch DB**

Run: `task test:int -- -run TestMigrations ./internal/store/...`
Expected: PASS (migrations apply up then down cleanly; integration tests run against a fresh DB).

- [ ] **Step 4: Commit**

Commit per `references/vcs-preamble.md`: `feat(store): add session_connections.last_seen_at lease column (holomush-rsoe6)`

---

### Task 2: Store — `RefreshConnection`, `ListLapsedConnections`, stamp lease on `AddConnection`

**Files:**

- Modify: `internal/session/session.go` (Store interface ~343-357)
- Modify: `internal/store/session_store.go` (`AddConnection` ~557-560; add two methods after `RemoveConnection` ~577)
- Test: `test/integration/session/session_lease_test.go` (new; SharedPostgres → `//go:build integration`)
- Regenerate: `internal/session/mocks/mock_Store.go`

- [ ] **Step 1: Write the failing integration test**

`test/integration/session/session_lease_test.go`:

```go
//go:build integration

package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
)

func TestRefreshConnectionBumpsLastSeenAndListLapsedExcludesIt(t *testing.T) {
	ctx := context.Background()
	store, pool := sessiontest.NewStoreWithPool(t)

	ps := sessiontest.NewPlayerSession()
	sessiontest.SeedPlayerSession(t, pool, ps)
	sess := sessiontest.NewActiveSession(ps) // status=active, future ExpiresAt
	require.NoError(t, store.Set(ctx, sess.ID, sess))

	connID := ulid.Make()
	require.NoError(t, store.AddConnection(ctx, &session.Connection{
		ID: connID, SessionID: sess.ID, ClientType: "terminal",
		ConnectedAt: time.Now().Add(-time.Hour), // stale connect time
	}))

	// AddConnection stamps last_seen_at = connected_at (stale here), so the
	// connection is initially lapsed relative to a 45s TTL.
	lapsed, err := store.ListLapsedConnections(ctx, time.Now().Add(-45*time.Second))
	require.NoError(t, err)
	assert.Len(t, lapsed, 1, "stale-connect connection is lapsed before refresh")

	// Refresh bumps last_seen_at to now.
	require.NoError(t, store.RefreshConnection(ctx, connID))

	lapsed, err = store.ListLapsedConnections(ctx, time.Now().Add(-45*time.Second))
	require.NoError(t, err)
	assert.Empty(t, lapsed, "refreshed connection is no longer lapsed")
}

func TestRefreshConnectionReturnsNotFoundForMissingConnection(t *testing.T) {
	ctx := context.Background()
	store, _ := sessiontest.NewStoreWithPool(t)
	err := store.RefreshConnection(ctx, ulid.Make())
	require.Error(t, err)
	assert.Equal(t, "CONNECTION_NOT_FOUND", oopsCode(t, err))
}
```

(Helper `oopsCode` asserts the top-level oops code per `.claude/rules/grpc-errors.md`; reuse the package's existing helper or add `func oopsCode(t *testing.T, err error) string { t.Helper(); o, ok := oops.AsOops(err); require.True(t, ok); return o.Code() }`.)

- [ ] **Step 2: Run it — expect compile failure**

Run: `task test:int -- -run TestRefreshConnection ./test/integration/session/...`
Expected: FAIL — `store.ListLapsedConnections` / `store.RefreshConnection` undefined.

- [ ] **Step 3: Add interface methods + `LapsedConnection` type**

In `internal/session/session.go`, inside the `Store` interface (after `RemoveConnection`, ~347):

```go
	// RefreshConnection bumps a connection's lease (last_seen_at = now).
	// Returns CONNECTION_NOT_FOUND if no row matches connectionID.
	RefreshConnection(ctx context.Context, connectionID ulid.ULID) error

	// ListLapsedConnections returns connections whose lease is older than
	// olderThan — i.e. last_seen_at < olderThan. Used by the lease sweep.
	ListLapsedConnections(ctx context.Context, olderThan time.Time) ([]LapsedConnection, error)
```

And the type (near `Connection`):

```go
// LapsedConnection is the projection the lease sweep needs: enough to remove
// the row and recompute the owning session's derived liveness.
type LapsedConnection struct {
	ID         ulid.ULID
	SessionID  string
	ClientType string
}
```

- [ ] **Step 4: Implement in `session_store.go`**

Stamp the lease in `AddConnection` — extend the INSERT (line ~558-560):

```go
	_, err := s.pool.Exec(ctx,
		`INSERT INTO session_connections (id, session_id, client_type, streams, focus_key, connected_at, last_seen_at)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6, $6)`,
		conn.ID.String(), conn.SessionID, conn.ClientType, streams, focusKeyJSON, pgnanos.From(conn.ConnectedAt))
```

Add after `RemoveConnection` (~577):

```go
// RefreshConnection bumps a connection's lease to now.
func (s *PostgresSessionStore) RefreshConnection(ctx context.Context, connectionID ulid.ULID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE session_connections SET last_seen_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT WHERE id = $1`,
		connectionID.String())
	if err != nil {
		return oops.With("operation", "refresh connection").
			With("connection_id", connectionID.String()).Wrap(err)
	}
	if tag.RowsAffected() == 0 {
		return oops.Code("CONNECTION_NOT_FOUND").
			With("connection_id", connectionID.String()).
			Errorf("connection not found")
	}
	return nil
}

// ListLapsedConnections returns connections whose lease is older than olderThan.
func (s *PostgresSessionStore) ListLapsedConnections(ctx context.Context, olderThan time.Time) ([]session.LapsedConnection, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, session_id, client_type FROM session_connections WHERE last_seen_at < $1`,
		pgnanos.From(olderThan))
	if err != nil {
		return nil, oops.With("operation", "list lapsed connections").Wrap(err)
	}
	defer rows.Close()
	var out []session.LapsedConnection
	for rows.Next() {
		var idStr string
		var lc session.LapsedConnection
		if scanErr := rows.Scan(&idStr, &lc.SessionID, &lc.ClientType); scanErr != nil {
			return nil, oops.With("operation", "scan lapsed connection").Wrap(scanErr)
		}
		id, parseErr := ulid.Parse(idStr)
		if parseErr != nil {
			return nil, oops.With("operation", "parse lapsed connection id").With("id", idStr).Wrap(parseErr)
		}
		lc.ID = id
		out = append(out, lc)
	}
	return out, oops.Wrap(rows.Err())
}
```

- [ ] **Step 5: Regenerate the mock + run**

Run: `mockery` then `task test:int -- -run TestRefreshConnection ./test/integration/session/...`
Expected: PASS.

- [ ] **Step 6: Commit**

`feat(session): RefreshConnection + ListLapsedConnections lease store methods (holomush-rsoe6)`

---

### Task 3: `RefreshConnection` RPC (ownership-validated, enumeration-safe)

**Files:**

- Modify: `api/proto/holomush/core/v1/core.proto` (add RPC + messages near `Disconnect`)
- Create: `internal/grpc/refresh_connection.go`
- Test: `internal/grpc/refresh_connection_test.go`
- Regenerate protos (`task` proto-generate target)

- [ ] **Step 1: Add proto RPC + messages**

In `core.proto`, add to `service CoreService` (next to `Disconnect`):

```protobuf
  // RefreshConnection bumps a connection's liveness lease. Called periodically
  // by the gateway while the client socket is open (holomush-rsoe6). SERVED by
  // CoreServer.RefreshConnection; ownership-validated and enumeration-safe.
  rpc RefreshConnection(RefreshConnectionRequest) returns (RefreshConnectionResponse);
```

And the messages:

```protobuf
// RefreshConnectionRequest asks core to bump the lease for one connection.
message RefreshConnectionRequest {
  // meta carries request correlation data.
  RequestMeta meta = 1;
  // session_id names the game session owning the connection.
  string session_id = 2;
  // connection_id is the connection whose lease to refresh.
  string connection_id = 3;
  // player_session_token proves the caller owns session_id.
  string player_session_token = 4;
}

// RefreshConnectionResponse is empty on success; failures are gRPC status codes.
message RefreshConnectionResponse {
  // meta carries response correlation data.
  ResponseMeta meta = 1;
}
```

- [ ] **Step 2: Write the failing handler test**

`internal/grpc/refresh_connection_test.go`:

```go
package grpc

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	"github.com/holomush/holomush/internal/oops"
)

func TestRefreshConnectionRefreshesOwnedConnection(t *testing.T) {
	char := ulid.Make()
	loc := ulid.Make()
	sess := mkActiveAt("sess-1", char, loc)
	store := newTestSessionStore(t, map[string]*session.Info{"sess-1": sess})
	s := &CoreServer{
		sessionStore:      store,
		playerSessionRepo: newFakePlayerSessionRepo(ownedPlayerID),
	}
	_, err := s.RefreshConnection(context.Background(), &corev1.RefreshConnectionRequest{
		SessionId: "sess-1", ConnectionId: ulid.Make().String(),
		PlayerSessionToken: testPlayerSessionToken,
	})
	// Connection not present in the fake store → CONNECTION_NOT_FOUND surfaces
	// as a successful-ownership but missing-connection path (see Step 3).
	require.Error(t, err)
	assert.Equal(t, "CONNECTION_NOT_FOUND", oops.AsOops(err).Code())
}

func TestRefreshConnectionCollapsesOwnershipFailureToSessionNotFound(t *testing.T) {
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, nil),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	_, err := s.RefreshConnection(context.Background(), &corev1.RefreshConnectionRequest{
		SessionId: "missing", ConnectionId: ulid.Make().String(),
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.Error(t, err)
	assert.Equal(t, "SESSION_NOT_FOUND", oops.AsOops(err).Code())
}
```

- [ ] **Step 3: Implement the handler**

`internal/grpc/refresh_connection.go` (mirrors the ownership pattern at `list_focus_presence.go:44-66`):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/grpc/auth"
	"github.com/holomush/holomush/internal/oops"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// RefreshConnection bumps a connection's liveness lease (holomush-rsoe6).
// Ownership failures collapse to SESSION_NOT_FOUND (enumeration-safe, I-SEC-1).
func (s *CoreServer) RefreshConnection(ctx context.Context, req *corev1.RefreshConnectionRequest) (*corev1.RefreshConnectionResponse, error) {
	if req.GetSessionId() == "" || req.GetConnectionId() == "" {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("session_id and connection_id are required")
	}
	if _, err := auth.ValidateSessionOwnership(
		ctx, s.playerSessionRepo, s.sessionStore,
		req.GetPlayerSessionToken(), req.GetSessionId(),
	); err != nil {
		slog.DebugContext(ctx, "refresh connection ownership validation failed",
			"session_id", req.GetSessionId(), "error", err)
		return nil, oops.Code("SESSION_NOT_FOUND").
			With("session_id", req.GetSessionId()).Errorf("session not found")
	}
	connID, err := ulid.Parse(req.GetConnectionId())
	if err != nil {
		return nil, oops.Code("INVALID_ARGUMENT").With("connection_id", req.GetConnectionId()).
			Errorf("connection_id is not a valid ULID")
	}
	if refreshErr := s.sessionStore.RefreshConnection(ctx, connID); refreshErr != nil {
		return nil, refreshErr //nolint:wrapcheck // store returns canonical CONNECTION_NOT_FOUND oops code
	}
	return &corev1.RefreshConnectionResponse{Meta: responseMeta(req.GetMeta().GetRequestId())}, nil
}
```

- [ ] **Step 4: Regenerate protos + run**

Run: `task proto` (regenerate), then `task test -- -run TestRefreshConnection ./internal/grpc/`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

`feat(grpc): RefreshConnection RPC (ownership-validated lease refresh) (holomush-rsoe6)`

---

### Task 4: `recomputeSessionLiveness` — derive `active`/`grid_present` from connections

**Files:**

- Modify: `internal/grpc/server.go` (extract from the `Disconnect` lifecycle at ~1473-1545; call from `AddConnection` path ~854)
- Test: `internal/grpc/server_liveness_test.go`

- [ ] **Step 1: Write the failing test**

`internal/grpc/server_liveness_test.go`:

```go
package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
	sessionmocks "github.com/holomush/holomush/internal/session/mocks"
	"github.com/stretchr/testify/mock"
)

func TestRecomputeSessionLivenessDetachesWhenNoLiveConnections(t *testing.T) {
	store := sessionmocks.NewMockStore(t)
	store.EXPECT().CountConnections(mock.Anything, "sess-1").Return(0, nil).Once()
	store.EXPECT().UpdateStatus(mock.Anything, "sess-1", session.StatusDetached,
		mock.Anything, mock.Anything).Return(nil).Once()
	s := &CoreServer{sessionStore: store, sessionDefaults: SessionDefaults{TTL: 30 * time.Minute}}

	got, err := s.recomputeSessionLiveness(context.Background(), &session.Info{ID: "sess-1", GridPresent: false})
	require.NoError(t, err)
	assert.Equal(t, session.StatusDetached, got.status)
}

func TestRecomputeSessionLivenessClearsGridPresentWhenNoGridConnections(t *testing.T) {
	store := sessionmocks.NewMockStore(t)
	store.EXPECT().CountConnections(mock.Anything, "sess-1").Return(1, nil).Once() // comms_hub only
	store.EXPECT().CountConnectionsByType(mock.Anything, "sess-1", "terminal").Return(0, nil).Once()
	store.EXPECT().CountConnectionsByType(mock.Anything, "sess-1", "telnet").Return(0, nil).Once()
	store.EXPECT().UpdateGridPresent(mock.Anything, "sess-1", false).Return(nil).Once()
	s := &CoreServer{sessionStore: store}

	got, err := s.recomputeSessionLiveness(context.Background(), &session.Info{ID: "sess-1", GridPresent: true})
	require.NoError(t, err)
	assert.True(t, got.active)
	assert.False(t, got.gridPresent)
}
```

- [ ] **Step 2: Run — expect compile failure**

Run: `task test -- -run TestRecomputeSessionLiveness ./internal/grpc/`
Expected: FAIL — `recomputeSessionLiveness` undefined.

- [ ] **Step 3: Implement `recomputeSessionLiveness`**

In `server.go`, add (factoring the count/transition logic currently inline in `Disconnect` at 1473-1545):

```go
// livenessResult is the derived state after recomputing a session's connections.
type livenessResult struct {
	active      bool
	gridPresent bool
	status      session.Status
}

// recomputeSessionLiveness derives active/grid_present from the session's live
// connection set and applies the transitions (detach on zero connections; clear
// grid_present when no terminal/telnet connection remains). Shared by the
// Disconnect RPC, the AddConnection path, and the lease sweep (I-LIVE-3).
func (s *CoreServer) recomputeSessionLiveness(ctx context.Context, info *session.Info) (livenessResult, error) {
	total, err := s.sessionStore.CountConnections(ctx, info.ID)
	if err != nil {
		return livenessResult{}, oops.With("session_id", info.ID).Wrap(err)
	}
	res := livenessResult{active: total > 0, status: info.Status}
	if total == 0 {
		ttl := time.Duration(info.TTLSeconds) * time.Second
		if ttl <= 0 {
			ttl = s.sessionDefaults.TTL
		}
		now := time.Now()
		expiresAt := now.Add(ttl)
		if updErr := s.sessionStore.UpdateStatus(ctx, info.ID, session.StatusDetached, &now, &expiresAt); updErr != nil {
			return livenessResult{}, oops.With("session_id", info.ID).Wrap(updErr)
		}
		res.status = session.StatusDetached
		res.gridPresent = false
		return res, nil
	}
	term, err := s.sessionStore.CountConnectionsByType(ctx, info.ID, "terminal")
	if err != nil {
		return livenessResult{}, oops.With("session_id", info.ID).Wrap(err)
	}
	tel, err := s.sessionStore.CountConnectionsByType(ctx, info.ID, "telnet")
	if err != nil {
		return livenessResult{}, oops.With("session_id", info.ID).Wrap(err)
	}
	res.gridPresent = term+tel > 0
	if res.gridPresent != info.GridPresent {
		if updErr := s.sessionStore.UpdateGridPresent(ctx, info.ID, res.gridPresent); updErr != nil {
			return livenessResult{}, oops.With("session_id", info.ID).Wrap(updErr)
		}
	}
	return res, nil
}
```

- [ ] **Step 4: Call it from the `AddConnection` path**

In `server.go` after the `AddConnection` call (~854-866), recompute so a newly grid-attached session flips `grid_present` true:

```go
	if _, recErr := s.recomputeSessionLiveness(addCtx, info); recErr != nil {
		slog.WarnContext(addCtx, "recompute liveness after add connection failed",
			"session_id", info.ID, "error", recErr)
	}
```

- [ ] **Step 5: Run — expect PASS**

Run: `task test -- -run TestRecomputeSessionLiveness ./internal/grpc/`
Expected: PASS.

- [ ] **Step 6: Commit**

`refactor(grpc): extract recomputeSessionLiveness for derived active/grid_present (holomush-rsoe6)`

---

### Task 5: Lease sweep + boot-grace in the reaper

**Files:**

- Modify: `internal/session/reaper.go` (`ReaperConfig`, `Run`, new `reapLapsedConnections`)
- Test: `internal/session/reaper_lease_test.go`

- [ ] **Step 1: Write the failing test**

`internal/session/reaper_lease_test.go`:

```go
package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
)

func TestReaperSweepsLapsedConnectionAndDetachesSession(t *testing.T) {
	ctx := context.Background()
	store, pool := sessiontest.NewStoreWithPool(t)
	ps := sessiontest.NewPlayerSession()
	sessiontest.SeedPlayerSession(t, pool, ps)
	sess := sessiontest.NewActiveSession(ps)
	require.NoError(t, store.Set(ctx, sess.ID, sess))
	connID := ulid.Make()
	require.NoError(t, store.AddConnection(ctx, &session.Connection{
		ID: connID, SessionID: sess.ID, ClientType: "terminal",
		ConnectedAt: time.Now().Add(-time.Hour), // lapsed
	}))

	now := time.Now()
	var detached []string
	r := session.NewReaper(store, session.ReaperConfig{
		Interval:  50 * time.Millisecond,
		LeaseTTL:  45 * time.Second,
		BootGrace: 0, // disable boot-grace for this test
		Now:       func() time.Time { return now },
		OnSessionDetached: func(info *session.Info) { detached = append(detached, info.ID) },
	})
	rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	r.Run(rctx)

	count, err := store.CountConnections(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "lapsed connection was swept")
	assert.Contains(t, detached, sess.ID, "session detached after last connection swept")
}

func TestReaperSuppressesLeaseSweepDuringBootGrace(t *testing.T) {
	ctx := context.Background()
	store, pool := sessiontest.NewStoreWithPool(t)
	ps := sessiontest.NewPlayerSession()
	sessiontest.SeedPlayerSession(t, pool, ps)
	sess := sessiontest.NewActiveSession(ps)
	require.NoError(t, store.Set(ctx, sess.ID, sess))
	connID := ulid.Make()
	require.NoError(t, store.AddConnection(ctx, &session.Connection{
		ID: connID, SessionID: sess.ID, ClientType: "terminal",
		ConnectedAt: time.Now().Add(-time.Hour),
	}))

	start := time.Now()
	r := session.NewReaper(store, session.ReaperConfig{
		Interval:  50 * time.Millisecond,
		LeaseTTL:  45 * time.Second,
		BootGrace: time.Hour, // still inside grace window during the test
		Now:       func() time.Time { return start.Add(time.Minute) }, // < BootGrace
	})
	rctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	r.Run(rctx)

	count, err := store.CountConnections(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "boot-grace suppresses the lease sweep (I-LIVE-4)")
}
```

- [ ] **Step 2: Run — expect compile failure**

Run: `task test:int -- -run TestReaper.*Lapsed ./internal/session/...` (uses `sessiontest`, needs Docker)
Expected: FAIL — `ReaperConfig.LeaseTTL`/`BootGrace`/`Now`/`OnSessionDetached` undefined.

- [ ] **Step 3: Extend `ReaperConfig` + `Run` + add `reapLapsedConnections`**

In `reaper.go`:

```go
type ReaperConfig struct {
	Interval  time.Duration
	OnExpired func(info *Info)

	// LeaseTTL: a connection whose last_seen_at is older than now-LeaseTTL is
	// swept (holomush-rsoe6, I-LIVE-2). Zero disables the lease sweep.
	LeaseTTL time.Duration
	// BootGrace suppresses the lease sweep for this long after Run starts, so a
	// surviving gateway re-asserts its leases before any reaping (I-LIVE-4).
	BootGrace time.Duration
	// Now is injectable for tests; defaults to time.Now.
	Now func() time.Time
	// OnSessionDetached fires when the sweep detaches a session (last live
	// connection removed). Leave events stay deferred to OnExpired at TTL.
	OnSessionDetached func(info *Info)
	// OnGridPhaseOut fires when grid_present falls true→false but the session
	// stays active (a non-grid connection remains).
	OnGridPhaseOut func(info *Info)
}
```

In `NewReaper`, default the clock:

```go
	if config.Now == nil {
		config.Now = time.Now
	}
	r := &Reaper{store: store, config: config}
	r.bootAt = config.Now()
	return r
```

Add `bootAt time.Time` to the `Reaper` struct. Extend `Run`'s tick:

```go
		case <-ticker.C:
			r.reapExpired(ctx)
			if r.config.LeaseTTL > 0 && r.config.Now().Sub(r.bootAt) >= r.config.BootGrace {
				r.reapLapsedConnections(ctx)
			}
```

Add the sweep:

```go
func (r *Reaper) reapLapsedConnections(ctx context.Context) {
	cutoff := r.config.Now().Add(-r.config.LeaseTTL)
	lapsed, err := r.store.ListLapsedConnections(ctx, cutoff)
	if err != nil {
		slog.WarnContext(ctx, "reaper: list lapsed connections failed", "error", err)
		return
	}
	affected := make(map[string]struct{})
	for _, lc := range lapsed {
		if rmErr := r.store.RemoveConnection(ctx, lc.ID); rmErr != nil {
			slog.WarnContext(ctx, "reaper: remove lapsed connection failed",
				"connection_id", lc.ID.String(), "error", rmErr)
			continue
		}
		affected[lc.SessionID] = struct{}{}
	}
	for sessionID := range affected {
		r.recomputeAfterSweep(ctx, sessionID)
	}
}

func (r *Reaper) recomputeAfterSweep(ctx context.Context, sessionID string) {
	info, err := r.store.Get(ctx, sessionID)
	if err != nil {
		return // session already gone; nothing to recompute
	}
	if info.Status != StatusActive {
		return
	}
	total, err := r.store.CountConnections(ctx, sessionID)
	if err != nil {
		slog.WarnContext(ctx, "reaper: count connections failed", "session_id", sessionID, "error", err)
		return
	}
	if total == 0 {
		ttl := time.Duration(info.TTLSeconds) * time.Second
		now := r.config.Now()
		expiresAt := now.Add(ttl)
		if updErr := r.store.UpdateStatus(ctx, sessionID, StatusDetached, &now, &expiresAt); updErr != nil {
			slog.WarnContext(ctx, "reaper: detach after sweep failed", "session_id", sessionID, "error", updErr)
			return
		}
		if r.config.OnSessionDetached != nil {
			r.config.OnSessionDetached(info)
		}
		return
	}
	term, _ := r.store.CountConnectionsByType(ctx, sessionID, "terminal")
	tel, _ := r.store.CountConnectionsByType(ctx, sessionID, "telnet")
	gridPresent := term+tel > 0
	if !gridPresent && info.GridPresent {
		if updErr := r.store.UpdateGridPresent(ctx, sessionID, false); updErr != nil {
			slog.WarnContext(ctx, "reaper: clear grid_present after sweep failed", "session_id", sessionID, "error", updErr)
			return
		}
		if r.config.OnGridPhaseOut != nil {
			r.config.OnGridPhaseOut(info)
		}
	}
}
```

> Note: the reaper recomputes via store calls only (no engine dependency); event emission is delegated to the injected callbacks wired in `sub_grpc.go` (Task 6), preserving the package boundary.

- [ ] **Step 4: Run — expect PASS**

Run: `task test:int -- -run TestReaper.*Lapsed ./internal/session/...`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

`feat(session): lease sweep + boot-grace in reaper (holomush-rsoe6)`

---

### Task 6: Wire lease-sweep config + detach/phase-out callbacks

**Files:**

- Modify: `cmd/holomush/sub_grpc.go` (the `session.NewReaper(...)` block, step 10)
- Modify: `cmd/holomush/core.go` (config: `SessionLeaseTTL`, `SessionBootGrace`)
- Test: `cmd/holomush/sub_grpc_test.go` (extend reaper-wiring test)

- [ ] **Step 1: Add config fields + flags**

In `core.go`, alongside `SessionReaperInterval` (~75, ~159):

```go
	SessionLeaseTTL   string `koanf:"session_lease_ttl"`
	SessionBootGrace  string `koanf:"session_boot_grace"`
```

```go
	cmd.Flags().StringVar(&cfg.SessionLeaseTTL, "session-lease-ttl", "45s", "connection lease TTL")
	cmd.Flags().StringVar(&cfg.SessionBootGrace, "session-boot-grace", "60s", "lease-sweep suppression window after core start")
```

Extend `parseSessionConfig` (`core.go:~1115`, currently returns `(sessionTTL, reaperInterval time.Duration, err error)`) to parse and return the two new durations — new signature `(sessionTTL, reaperInterval, leaseTTL, bootGrace time.Duration, err error)` — defaulting `45s`/`60s` and erroring on malformed/zero exactly like the existing `SessionReaperInterval` checks. Update its single call site to assign the two new return values into the cfg struct (Step 2).

- [ ] **Step 2: Wire the callbacks into `NewReaper`**

In `sub_grpc.go` step 10, extend the `ReaperConfig`:

```go
	s.sessionReaper = session.NewReaper(sessionStore, session.ReaperConfig{
		Interval:  s.cfg.ReaperInterval,
		LeaseTTL:  s.cfg.LeaseTTL,
		BootGrace: s.cfg.BootGrace,
		OnExpired: func(info *session.Info) { /* unchanged: leave + session_ended + guest release */ },
		OnSessionDetached: func(info *session.Info) {
			slog.InfoContext(reaperCtx, "lease sweep detached session", "session_id", info.ID)
			// Leave is deferred to OnExpired at TTL (matches the cooperative detach path).
		},
		OnGridPhaseOut: func(info *session.Info) {
			char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}
			if dcErr := engine.HandleDisconnect(reaperCtx, char, "phased out"); dcErr != nil {
				slog.WarnContext(reaperCtx, "lease sweep grid phase-out leave failed",
					"session_id", info.ID, "error", dcErr)
			}
		},
	})
```

Add `LeaseTTL`/`BootGrace time.Duration` to the `grpcSubsystemConfig` struct (`sub_grpc.go:~62`, alongside `ReaperInterval` at `:78`) and populate them at the call site from `parseSessionConfig`'s new return values.

- [ ] **Step 3: Extend the wiring test**

In `sub_grpc_test.go`, assert `parseSessionConfig` rejects a malformed `SessionLeaseTTL`/`SessionBootGrace` and accepts valid durations (mirror `TestParseSessionConfigRejectsInvalidReaperInterval`). **Also update the 8 existing `parseSessionConfig` call sites in `cmd/holomush/core_test.go` (~lines 1047,1064,1080,1093,1106,1120,1135,1150)** — their `_, _, err :=` / `ttl, reaper, err :=` destructuring breaks when the signature grows to 5 returns; extend each to the new arity.

- [ ] **Step 4: Run**

Run: `task test -- -run TestParseSessionConfig ./cmd/holomush/` then `task build`
Expected: PASS; binary builds.

- [ ] **Step 5: Commit**

`feat(core): wire lease sweep TTL + boot-grace + detach/phase-out callbacks (holomush-rsoe6)`

---

## Phase 2 — Gateway lease refresh

### Task 7: Web — refresh lease on the heartbeat tick

**Files:**

- Modify: `internal/web/handler.go` (`StreamEvents` heartbeat case ~249-257)
- Test: `internal/web/handler_test.go`

- [ ] **Step 1: Write the failing test**

In `handler_test.go`, drive a fake `CoreServiceClient` whose `Subscribe` stays open, advance one heartbeat, and assert `RefreshConnection` was called with the stream's `connID`:

```go
func TestStreamEventsRefreshesLeaseOnHeartbeat(t *testing.T) {
	fake := &fakeCoreClient{ /* Subscribe returns a stream that blocks on Recv */ }
	h := newTestHandler(t, fake)
	// run StreamEvents with a 1ms heartbeat override; cancel after 2 ticks
	// (inject heartbeat interval via a handler field for the test)
	runStreamEventsBriefly(t, h)
	assert.GreaterOrEqual(t, fake.refreshCalls(), 1, "heartbeat refreshes the connection lease")
	assert.Equal(t, h.lastConnID(), fake.lastRefreshConnID())
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `task test -- -run TestStreamEventsRefreshesLease ./internal/web/`
Expected: FAIL — no `RefreshConnection` call.

- [ ] **Step 3: Implement — refresh in the heartbeat case**

In `handler.go`, after the successful heartbeat `stream.Send` (line ~257), add:

```go
				refreshCtx, refreshCancel := context.WithTimeout(ctx, rpcTimeout)
				if _, refreshErr := h.client.RefreshConnection(refreshCtx, &corev1.RefreshConnectionRequest{
					SessionId:          sessionID,
					ConnectionId:       connID.String(),
					PlayerSessionToken: token,
				}); refreshErr != nil {
					slog.DebugContext(ctx, "web: lease refresh failed (transient)", "session_id", sessionID, "error", refreshErr)
				}
				refreshCancel()
```

To make the heartbeat interval injectable for the test, add a `heartbeatInterval time.Duration` field on `Handler` defaulting to `15 * time.Second`, and use it at line 241.

- [ ] **Step 4: Run — expect PASS**

Run: `task test -- -run TestStreamEventsRefreshesLease ./internal/web/`
Expected: PASS.

- [ ] **Step 5: Commit**

`feat(web): refresh connection lease on StreamEvents heartbeat (holomush-rsoe6)`

---

### Task 8: Telnet — refresh lease on a ticker

**Files:**

- Modify: `internal/telnet/gateway_handler.go` (add `CoreClient.RefreshConnection`; add ticker in `Handle` loop ~177)
- Test: `internal/telnet/gateway_handler_test.go`

- [ ] **Step 1: Add `RefreshConnection` to the telnet `CoreClient` interface**

In `gateway_handler.go` (the `CoreClient` interface ~52-64):

```go
	RefreshConnection(ctx context.Context, req *corev1.RefreshConnectionRequest) (*corev1.RefreshConnectionResponse, error)
```

- [ ] **Step 2: Write the failing test**

`gateway_handler_test.go`: drive a handler into the authed/subscribed state, fire the refresh ticker once, assert the fake client saw a `RefreshConnection` for `h.connectionID`.

```go
func TestGatewayHandlerRefreshesLeaseWhileAuthed(t *testing.T) {
	fake := newFakeCoreClient(t)
	h := newAuthedHandler(t, fake) // sessionID + connectionID + token set, authed=true
	h.refreshOnce(context.Background()) // test hook calling the same path the ticker uses
	assert.Equal(t, h.connectionID, fake.lastRefreshConnID())
}
```

- [ ] **Step 3: Implement — refresh ticker in `Handle`**

Add a `refreshTicker := time.NewTicker(h.limits.LeaseRefreshInterval)` (add `LeaseRefreshInterval` to `Limits`, default 15s) and `defer refreshTicker.Stop()` before the `for` loop (~176), and a select case:

```go
		case <-refreshTicker.C:
			if h.authed && h.sessionID != "" {
				rCtx, rCancel := context.WithTimeout(childCtx, rpcTimeout)
				if _, err := h.client.RefreshConnection(rCtx, &corev1.RefreshConnectionRequest{
					SessionId:          h.sessionID,
					ConnectionId:       h.connectionID,
					PlayerSessionToken: h.playerSessionToken,
				}); err != nil {
					slog.DebugContext(childCtx, "telnet: lease refresh failed (transient)", "session_id", h.sessionID, "error", err)
				}
				rCancel()
			}
```

Factor the refresh body into `func (h *GatewayHandler) refreshOnce(ctx context.Context)` so the test and the ticker share one path.

- [ ] **Step 4: Run — expect PASS**

Run: `task test -- -run TestGatewayHandlerRefreshesLease ./internal/telnet/`
Expected: PASS.

- [ ] **Step 5: Activate the sweep (sequencing gate) + commit**

With both refreshers now landed, the lease sweep from Task 5/6 is safe to run. Confirm the default config (`LeaseTTL=45s`) is active. Commit: `feat(telnet): refresh connection lease on a ticker (holomush-rsoe6)`

---

## Phase 3 — Gateway survival

### Task 9: Add `RECONNECTING`/`RECONNECTED` web control signals

**Files:**

- Modify: `api/proto/holomush/web/v1/web.proto` (`ControlSignal` enum ~44-63)
- Regenerate protos + TS client (`web/src/lib/connect/...`)

- [ ] **Step 1: Add enum values**

```protobuf
  // CONTROL_SIGNAL_RECONNECTING: the gateway lost its core stream but is holding
  // the client and reconnecting; the UI shows a reconnecting indicator (holomush-rsoe6).
  CONTROL_SIGNAL_RECONNECTING = 4;
  // CONTROL_SIGNAL_RECONNECTED: the gateway re-established the core stream; the
  // client may clear the reconnecting indicator.
  CONTROL_SIGNAL_RECONNECTED = 5;
```

- [ ] **Step 2: Regenerate + verify it compiles**

Run: `task proto`, then `task build` and `cd web && bun run check` (TS types regenerated).
Expected: PASS.

- [ ] **Step 3: Commit**

`feat(proto): web ControlSignal RECONNECTING/RECONNECTED (holomush-rsoe6)`

---

### Task 10: Web — survive a core-stream break

**Files:**

- Modify: `internal/web/handler.go` (`StreamEvents` recv-error path ~263-280; track last event id)
- Test: `internal/web/handler_test.go`

- [ ] **Step 1: Write the failing test**

Drive `StreamEvents` with a fake core client whose first `Subscribe` stream errors `CodeUnavailable` after one event, then a second `Subscribe` succeeds and replays. Assert: (a) the client stream was NOT closed on the first error, (b) a `RECONNECTING` then `RECONNECTED` control frame reached the client, (c) the duplicate (redelivered) event was deduped.

```go
func TestStreamEventsReconnectsOnCoreStreamBreakWithoutClosingClient(t *testing.T) {
	fake := newReconnectingCoreClient(t) // stream #1: 1 event (id "E1") then Unavailable; stream #2: replays E1 then E2
	h := newTestHandler(t, fake)
	frames := captureClientFrames(t, h) // collects webv1 frames sent to the client
	assert.Contains(t, controlSignals(frames), webv1.ControlSignal_CONTROL_SIGNAL_RECONNECTING)
	assert.Contains(t, controlSignals(frames), webv1.ControlSignal_CONTROL_SIGNAL_RECONNECTED)
	assert.Equal(t, []string{"E1", "E2"}, eventIDs(frames), "redelivered E1 deduped; no client close")
}
```

- [ ] **Step 2: Run — expect FAIL** (`task test -- -run TestStreamEventsReconnects ./internal/web/`) — current code returns `CodeUnavailable` and ends the client stream.

- [ ] **Step 3: Implement — outer reconnect loop**

First define the break classification (package scope in `handler.go`):

```go
// breakCause classifies why a single Subscribe attempt ended.
type breakCause int

const (
	breakDone       breakCause = iota // ctx cancelled / clean end → return nil
	breakClientGone                   // client transport lost → return (defer Disconnect)
	breakCoreGone                     // core stream errored, client still alive → reconnect
)
```

Restructure `StreamEvents`: move the `Subscribe` + pump + forward into an inner `func (h *Handler) runSubscribeOnce(...) breakCause` and wrap it:

```go
	var lastForwardedID string
	seen := make(map[string]struct{})
	reconnectDeadline := time.Now().Add(h.reconnectCeiling) // ceiling ≤ session reattach TTL
	backoff := 100 * time.Millisecond
	for {
		cause := h.runSubscribeOnce(ctx, sessionID, token, connID, stream, &lastForwardedID, seen)
		switch cause {
		case breakClientGone:
			return nil // defer Disconnect fires
		case breakCoreGone:
			if time.Now().After(reconnectDeadline) {
				return connect.NewError(connect.CodeUnavailable, oops.Errorf("core unreachable past ceiling"))
			}
			_ = stream.Send(reconnectingControlFrame())
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			if backoff < 5*time.Second {
				backoff *= 2
			}
			// loop: re-Subscribe (durable consumer resumes server-side)
		case breakDone:
			return nil
		}
	}
```

`runSubscribeOnce` opens `h.client.Subscribe`, sends `RECONNECTED` after a successful (re)open (skip on the first open — gate on a `reconnected bool`), then runs the existing heartbeat/recv/forward select. In the event-forward path, capture the id and dedup:

```go
	case *corev1.SubscribeResponse_Event:
		id := frame.Event.GetId()
		if _, dup := seen[id]; dup {
			return // already forwarded (redelivery overlap)
		}
		seen[id] = struct{}{}
		lastForwardedID = id
		// ... existing translate + Send ...
```

Classify the break: client-gone when `stream.Send` fails or `ctx.Done()`; core-gone when `sub.Recv()` returns a non-EOF transport error. If `Subscribe` itself returns `SESSION_NOT_FOUND`, re-`SelectCharacter` (reattach) before the next `runSubscribeOnce`.

Add `reconnectCeiling time.Duration` to `Handler` (default from config; capped at the session reattach TTL).

- [ ] **Step 4: Run — expect PASS** (`task test -- -run TestStreamEventsReconnects ./internal/web/`).

- [ ] **Step 5: Commit**

`feat(web): survive core-stream break — hold client, reconnect, dedup resume (holomush-rsoe6)`

---

### Task 11: Telnet — survive a core-stream break

**Files:**

- Modify: `internal/telnet/gateway_handler.go` (the `eventRecv` closed path ~223-229)
- Test: `internal/telnet/gateway_handler_test.go`

- [ ] **Step 1: Write the failing test**

Drive the handler so its first subscription stream closes (core-gone) while the telnet conn stays open; assert it re-subscribes (calls `Subscribe` again) and emits a reconnecting status line rather than terminating.

```go
func TestGatewayHandlerReconnectsOnCoreStreamClose(t *testing.T) {
	fake := newReconnectingCoreClient(t)
	h := newAuthedHandler(t, fake)
	out := runHandleBriefly(t, h, fake)
	assert.GreaterOrEqual(t, fake.subscribeCalls(), 2, "re-subscribed after core stream closed")
	assert.Contains(t, out, "Reconnecting")
}
```

- [ ] **Step 2: Run — expect FAIL** — current code sends "Connection to server lost." and continues without re-subscribing.

- [ ] **Step 3: Implement — reconnect on `eventRecv` close**

Replace the `eventRecv` closed branch (~223-229). When the stream closes and the client is still connected and not quitting, re-establish the subscription (the durable consumer resumes), deduping by last event id:

```go
		case resp, ok := <-eventRecv:
			if !ok {
				eventRecv = nil
				if h.quitting || !h.authed || h.sessionID == "" {
					continue
				}
				h.send("[Reconnecting to server…]")
				if ch := h.resubscribe(childCtx); ch != nil { // re-Subscribe; reattach on SESSION_NOT_FOUND
					eventRecv = ch
					h.send("[Reconnected.]")
				} else {
					h.send("Connection to server lost.")
				}
				continue
			}
			switch frame := resp.GetFrame().(type) {
			case *corev1.SubscribeResponse_Event:
				if id := frame.Event.GetId(); id == h.lastEventID {
					continue // redelivery overlap
				} else {
					h.lastEventID = id
				}
				h.sendProtoEvent(frame.Event)
			// ... existing control-frame handling ...
			}
```

Add `lastEventID string` to the struct and a `resubscribe(ctx) <-chan *corev1.SubscribeResponse` helper (re-uses `subscribeAndEnter`'s Subscribe call with the existing `sessionID`/`connectionID`; on `SESSION_NOT_FOUND` calls `SelectCharacter` first). Bound retries by a deadline as in the web handler.

- [ ] **Step 4: Run — expect PASS** (`task test -- -run TestGatewayHandlerReconnects ./internal/telnet/`).

- [ ] **Step 5: Commit**

`feat(telnet): survive core-stream break — reconnect + dedup resume (holomush-rsoe6)`

---

## Phase 4 — Presence / `grid_present` split

### Task 12: Roster reads `grid_present`, not raw `active`

**Files:**

- Modify: `internal/store/session_store.go` (`ListActiveByLocation` predicate ~620)
- Test: `internal/grpc/list_focus_presence_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Add to `list_focus_presence_test.go`: a session at the location with `GridPresent=false` MUST NOT appear in the roster even though it is `active`.

```go
func TestListFocusPresenceExcludesActiveButNotGridPresent(t *testing.T) {
	char1, char2 := ulid.Make(), ulid.Make()
	loc := ulid.MustParse("01HYXLOCATION0000000000001")
	caller := mkActiveAt("sess-1", char1, loc)         // grid-present caller
	offgrid := mkActiveAt("sess-2", char2, loc)
	offgrid.GridPresent = false                         // active but not on the grid
	grant := policytest.NewGrantEngine()
	grant.Grant(access.CharacterSubject(char1.String()), "list_presence", access.LocationResource(loc.String()))
	s := &CoreServer{
		sessionStore:          newTestSessionStore(t, map[string]*session.Info{"sess-1": caller, "sess-2": offgrid}),
		playerSessionRepo:     newFakePlayerSessionRepo(ownedPlayerID),
		accessEngine:          grant,
		characterNameResolver: stubResolver(char1, char2),
	}
	resp, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId: "sess-1", PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.Len(t, resp.Entries, 1, "only grid-present characters appear in the roster (I-PRES-1)")
}
```

(Ensure `mkActiveAt` sets `GridPresent=true` by default; if not, set `caller.GridPresent = true`. The in-memory test store's `ListActiveByLocation` must apply the same `grid_present` filter as Postgres — update it too.)

- [ ] **Step 2: Run — expect FAIL** (`task test -- -run TestListFocusPresenceExcludesActiveButNotGridPresent ./internal/grpc/`).

- [ ] **Step 3: Implement — add the predicate**

In `session_store.go:620`, change the query:

```go
		`SELECT `+sessionSelectColumns+` FROM sessions WHERE location_id = $1 AND status = 'active' AND grid_present = true`,
```

Apply the same `grid_present` filter to the test session store's `ListActiveByLocation` (`internal/grpc/*_test.go` fake, or `sessiontest`). `location_follow.go:223` shares the method, so its presence list inherits the filter automatically.

- [ ] **Step 4: Run — expect PASS** + regression-check the existing presence tests still pass (`task test -- -run TestListFocusPresence ./internal/grpc/`).

- [ ] **Step 5: Commit**

`feat(store): presence roster filters grid_present (holomush-rsoe6)`

---

## Phase 5 — Cross-cutting tests & docs

### Task 13: Integration — end-to-end deadlock resolution

**Files:**

- Create: `test/integration/presence/lease_reaper_test.go` (`//go:build integration`)

- [ ] **Step 1: Write the spec** (Ginkgo): a guest session whose connection lease lapses → swept → detached → expired by the reaper → the guest player becomes eligible for `ListIdleGuests` (no active/detached session) and is collected. Use `integrationtest.Start(t)`, drive a connection, stop refreshing, advance the reaper.

```go
//go:build integration

var _ = Describe("Lease-driven liveness", func() {
	It("clears presence and unblocks the guest reaper when a lease lapses", func() {
		// Given a connected guest visible in presence …
		// When its lease lapses and the reaper sweeps …
		Eventually(presenceAt(loc)).Should(BeEmpty())
		// And the guest session is detached then expired …
		Eventually(guestSessionStatus(sessID)).Should(Equal("expired"))
	})
})
```

- [ ] **Step 2: Run** `task test:int -- ./test/integration/presence/...` → PASS.
- [ ] **Step 3: Commit** `test(integration): lease-driven presence + guest-reaper unblock (holomush-rsoe6)`

### Task 14: Integration — core restart does not detach a live session

**Files:** Create: `test/integration/presence/boot_grace_test.go`

- [ ] **Step 1:** Spec: a lease-refreshed session is NOT detached within the boot-grace window after a simulated reaper restart (construct a fresh `Reaper` with `bootAt=now`); assert no spurious `leave`.
- [ ] **Step 2:** `task test:int -- ./test/integration/presence/...` → PASS.
- [ ] **Step 3:** Commit `test(integration): boot-grace protects live sessions on restart (holomush-rsoe6)`

### Task 15: E2E — presence happy path + reconnect

**Files:** Create: `web/e2e/presence-liveness.spec.ts`

- [ ] **Step 1:** Specs (Playwright): (a) two browsers at one location each see the other in PRESENT; one closes → the other sees it drop (no ghost); (b) reload within reattach TTL keeps the character present exactly once; (c) kill transport → ghost clears within `L`+sweep; (d) restart core mid-session → RECONNECTING then resume with no dup/missing events.
- [ ] **Step 2:** `task test:e2e -- presence-liveness` → PASS.
- [ ] **Step 3:** Commit `test(e2e): presence liveness happy path + reconnect (holomush-rsoe6)`

### Task 16: Invariant bijection meta-test

**Files:** Create/extend: `test/meta/liveness_invariants_test.go`

- [ ] **Step 1:** Enumerate I-LIVE-1..5, I-PRES-1, I-SURV-1..5, I-SEC-1; map each to ≥1 named covering test (from Tasks 2–15); assert the bijection (every invariant has a test; every registered test names a real invariant). Pattern: mirror an existing meta-test (e.g. `test/meta/quarantine_registry_test.go`).
- [ ] **Step 2:** `task test -- ./test/meta/...` → PASS.
- [ ] **Step 3:** `task test:cover` → confirm >80% per touched package (≥90% for `internal/session`).
- [ ] **Step 4:** Commit `test(meta): invariant↔test bijection for liveness (holomush-rsoe6)`

---

## Final verification (before PR)

- [ ] `task pr-prep` green (fast lane).
- [ ] `task pr-prep:full` (touches int + E2E surface — Ginkgo presence suites + Playwright spec).
- [ ] Spec invariants I-LIVE-1..5 / I-PRES-1 / I-SURV-1..5 / I-SEC-1 each map to a passing test (Task 16).
- [ ] Update the session-lifecycle contributor doc + diagram (holomush-bgxg) with the connection-lease layer.
