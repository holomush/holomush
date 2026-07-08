<!--
SPDX-License-Identifier: Apache-2.0
Copyright 2026 HoloMUSH Contributors
-->

# Focus-Redirect Hot-Path Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the per-dispatch Postgres `GetConnection` read on the
`pose`/`say`/`ooc`/`emit` focus-redirect hot path by serving connection focus from
an in-memory, self-invalidating cache.

**Architecture:** A `ConnectionFocusCache` (map + global generation counter +
FIFO size cap) shared by a `cachingSessionStore` decorator (evicts on every
focus-affecting store write) and a cache-first `cachingFocusReader`. Correctness
hinges on one method — `UpdateSessionConnection`, the sole path that changes a
live connection's focus; create/remove evictions are best-effort memory reclaim.
A generation guard closes the cache-aside repopulation race against the plain
(non-locking) `GetConnection` SELECT. Single-core in-process assumption (documented
invariant); fail-closed focus semantics (INV-SCENE-67) preserved.

**Tech Stack:** Go, `container/list` (FIFO), `sync.Mutex`, prometheus counters,
testify (unit), Ginkgo/Gomega (integration), `oklog/ulid/v2`.

**Spec:** `docs/superpowers/specs/2026-07-07-focus-hotpath-cache-design.md`

**Design decisions locked here (spec open questions):**

- **Placement (spec Q1):** all three types live in `internal/command`. The
  decorator wraps the `session.Store` *interface* (`internal/session`), which
  `internal/command` already imports (`focus_reader.go:13`); the `FocusReader`
  seam already lives there. No import cycle (`internal/grpc` + `cmd/holomush`
  import `internal/command`; `internal/command` imports only `internal/session`).
- **Invariant scope (spec Q2):** `INV-SCENE-69` — sibling of `INV-SCENE-67`
  (`invariants.yaml:3339`), which already governs the same `internal/command`
  focus-read path.
- **Generation guard (spec Q's guard choice):** single global counter (simpler,
  strictly more conservative than per-key epoch).
- **Lock (spec Q3):** `sync.Mutex` first; a benchmark task confirms the hot path is
  not contention-bound (a Postgres read is ~1000× a mutex-guarded map read). An
  `RWMutex`/sharding follow-up bead is filed only if the benchmark shows contention.
- **`Delete`/`DeleteByCharacter` (spec Q3):** `Delete` (session-keyed) enumerates
  via `ListConnectionsBySession` before delegating and evicts each;
  `DeleteByCharacter` (char-keyed, rare admin path) relies on the LRU/FIFO cap
  backstop — a deleted character's connections never dispatch, so it is bounded
  memory, not stale-serve.

---

## File Structure

| File | Responsibility |
| ---- | -------------- |
| `internal/command/connection_focus_cache.go` (create) | The cache: `Peek`/`beginLoad`/`commitLoad`/`Evict`, global-gen guard, FIFO cap. |
| `internal/command/connection_focus_cache_test.go` (create) | Unit tests incl. the generation-guard race and cap eviction. |
| `internal/command/focus_reader.go` (modify) | Add `cachingFocusReader` + `NewCachingFocusReader`; keep `storeFocusReader`. |
| `internal/command/caching_focus_reader_test.go` (create) | Internal (`package command`) unit tests for the cache-first reader incl. fail-closed. |
| `internal/command/caching_session_store.go` (create) | `session.Store` decorator that evicts on focus-affecting writes. |
| `internal/command/caching_session_store_test.go` (create) | Unit tests: evict-after-success, delete enumeration, delegation. |
| `internal/observability/server.go` (modify) | `focus_cache` hit/miss counters + `RecordFocusCacheHit/Miss`. |
| `cmd/holomush/sub_grpc.go` (modify) | Production wiring: wrap store, cache-first reader. |
| `internal/testsupport/integrationtest/harness.go` (modify) | Integration-harness wiring (same shape). |
| `test/integration/scenes/focus_cache_test.go` (create) | End-to-end eviction across all three class-A paths. |
| `docs/architecture/invariants.yaml` (modify) | `INV-SCENE-69` entry, `binding: bound`, `asserted_by`. |

---

## Task 1: `ConnectionFocusCache`

**Files:**

- Create: `internal/command/connection_focus_cache.go`
- Test: `internal/command/connection_focus_cache_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/session"
)

func TestConnectionFocusCacheServesStoredKindOnHit(t *testing.T) {
	c := NewConnectionFocusCache(4)
	id := ulid.Make()
	g := c.beginLoad()
	c.commitLoad(id, session.FocusKind("scene"), g)

	kind, ok := c.Peek(id)
	assert.True(t, ok)
	assert.Equal(t, session.FocusKind("scene"), kind)
}

func TestConnectionFocusCacheMissesWhenAbsent(t *testing.T) {
	c := NewConnectionFocusCache(4)
	_, ok := c.Peek(ulid.Make())
	assert.False(t, ok)
}

func TestConnectionFocusCacheEvictDropsEntry(t *testing.T) {
	c := NewConnectionFocusCache(4)
	id := ulid.Make()
	c.commitLoad(id, session.FocusKind("scene"), c.beginLoad())
	c.Evict(id)

	_, ok := c.Peek(id)
	assert.False(t, ok, "evicted entry must miss")
}

// Verifies the generation guard closes the cache-aside repopulation race:
// a load whose window spans an Evict must NOT populate.
func TestConnectionFocusCacheCommitDroppedWhenEvictRacesLoad(t *testing.T) {
	c := NewConnectionFocusCache(4)
	id := ulid.Make()

	g := c.beginLoad()   // reader captures generation before its DB read
	c.Evict(id)          // a concurrent focus write commits + evicts during the read
	c.commitLoad(id, session.FocusKind("scene"), g) // reader tries to populate stale value

	_, ok := c.Peek(id)
	assert.False(t, ok, "populate spanning an Evict must be dropped, leaving no stale entry")
}

func TestConnectionFocusCacheEvictsOldestWhenOverCap(t *testing.T) {
	c := NewConnectionFocusCache(2)
	a, b, d := ulid.Make(), ulid.Make(), ulid.Make()
	c.commitLoad(a, "a", c.beginLoad())
	c.commitLoad(b, "b", c.beginLoad())
	c.commitLoad(d, "d", c.beginLoad()) // over cap → evicts oldest (a)

	_, okA := c.Peek(a)
	_, okB := c.Peek(b)
	_, okD := c.Peek(d)
	assert.False(t, okA, "oldest entry evicted under cap pressure")
	assert.True(t, okB)
	assert.True(t, okD)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestConnectionFocusCache ./internal/command/`
Expected: FAIL — `undefined: NewConnectionFocusCache`.
(Dispatch this via the `local-check` agent: kind=test, args=`-run TestConnectionFocusCache ./internal/command/`.)

- [ ] **Step 3: Write the implementation**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"container/list"
	"sync"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/session"
)

// DefaultFocusCacheCap bounds cached connection-focus entries. It is a memory
// backstop against leaked entries (spec G4), not a correctness mechanism — the
// working set is naturally one entry per active connection.
const DefaultFocusCacheCap = 65536

// ConnectionFocusCache caches the derived focus kind per connection for the
// dispatch hot path. A single global generation counter guards the cache-aside
// repopulation race: a load whose window spans any Evict is dropped. Safe for
// concurrent use.
type ConnectionFocusCache struct {
	mu      sync.Mutex
	entries map[ulid.ULID]session.FocusKind
	order   *list.List                    // FIFO of ulid.ULID; front = oldest
	elems   map[ulid.ULID]*list.Element   // key → its node in order
	cap     int
	gen     uint64 // bumped on every Evict
}

// NewConnectionFocusCache creates a cache bounded to capacity entries (<=0 uses
// DefaultFocusCacheCap).
func NewConnectionFocusCache(capacity int) *ConnectionFocusCache {
	if capacity <= 0 {
		capacity = DefaultFocusCacheCap
	}
	return &ConnectionFocusCache{
		entries: make(map[ulid.ULID]session.FocusKind),
		order:   list.New(),
		elems:   make(map[ulid.ULID]*list.Element),
		cap:     capacity,
	}
}

// Peek returns the cached focus kind and whether it was a hit.
func (c *ConnectionFocusCache) Peek(connID ulid.ULID) (session.FocusKind, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k, ok := c.entries[connID]
	return k, ok
}

// beginLoad captures the current generation, called before the store read.
func (c *ConnectionFocusCache) beginLoad() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

// commitLoad stores kind for connID only if no Evict happened since beginLoad
// returned gen.
func (c *ConnectionFocusCache) commitLoad(connID ulid.ULID, kind session.FocusKind, gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != gen {
		return // an eviction happened during the load window; drop the (possibly stale) value
	}
	if _, ok := c.entries[connID]; !ok {
		c.elems[connID] = c.order.PushBack(connID)
	}
	c.entries[connID] = kind
	for c.order.Len() > c.cap {
		oldest := c.order.Front()
		oid := oldest.Value.(ulid.ULID)
		c.order.Remove(oldest)
		delete(c.entries, oid)
		delete(c.elems, oid)
	}
}

// Evict removes connID's cached focus and bumps the generation so any in-flight
// load is dropped at commitLoad.
func (c *ConnectionFocusCache) Evict(connID ulid.ULID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	if e, ok := c.elems[connID]; ok {
		c.order.Remove(e)
		delete(c.elems, connID)
		delete(c.entries, connID)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run TestConnectionFocusCache ./internal/command/` (via `local-check`, kind=test).
Expected: PASS (all 5).

- [ ] **Step 5: Commit**

Commit per `references/vcs-preamble.md` (jj): `jj commit -m "feat(command): ConnectionFocusCache with generation-guarded populate (holomush-wm0fi)"`.

---

## Task 2: `cachingFocusReader` + observability counters

**Files:**

- Modify: `internal/observability/server.go` (add counters + Record funcs)
- Modify: `internal/command/focus_reader.go` (add cache-first reader)
- Test: `internal/command/caching_focus_reader_test.go` (create, `package command`)

- [ ] **Step 1: Add the observability counters**

In `internal/observability/server.go`, alongside the existing `engineFailures`
CounterVec + `RecordEngineFailure` (`:47`), add:

```go
var focusCacheLookups = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "holomush_focus_cache_lookups_total",
		Help: "Focus-redirect connection-focus cache lookups by result (hit|miss).",
	},
	[]string{"result"},
)

// RecordFocusCacheHit counts a focus-cache hit on the dispatch hot path.
func RecordFocusCacheHit() { focusCacheLookups.WithLabelValues("hit").Inc() }

// RecordFocusCacheMiss counts a focus-cache miss (falls back to a store read).
func RecordFocusCacheMiss() { focusCacheLookups.WithLabelValues("miss").Inc() }
```

Register it wherever `engineFailures` is registered (find the
`prometheus.MustRegister(...)` / registry block in the same file and add
`focusCacheLookups`). Match the existing registration pattern exactly.

- [ ] **Step 2: Write the failing reader tests**

Create `internal/command/caching_focus_reader_test.go` as an **internal** test
(`package command`) so it can reach the cache's unexported `beginLoad`/`commitLoad`.
Do NOT append to `internal/command/focus_reader_test.go` — that file is
`package command_test` (external) and cannot reference unexported methods. (Go
allows internal `package command` and external `package command_test` test files
to coexist in the same directory.)

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
)

type stubConnGetter struct {
	conn *session.Connection
	err  error
	calls int
}

func (s *stubConnGetter) GetConnection(_ context.Context, _ ulid.ULID) (*session.Connection, error) {
	s.calls++
	return s.conn, s.err
}

func TestCachingFocusReaderServesFromCacheWithoutStoreOnHit(t *testing.T) {
	cache := NewConnectionFocusCache(4)
	id := ulid.Make()
	cache.commitLoad(id, session.FocusKind("scene"), cache.beginLoad())
	getter := &stubConnGetter{}
	r := NewCachingFocusReader(getter, cache)

	kind, err := r.ConnectionFocusKind(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, session.FocusKind("scene"), kind)
	assert.Equal(t, 0, getter.calls, "cache hit must not touch the store")
}

func TestCachingFocusReaderPopulatesFromStoreOnMiss(t *testing.T) {
	cache := NewConnectionFocusCache(4)
	id := ulid.Make()
	getter := &stubConnGetter{conn: &session.Connection{ID: id, FocusKey: &session.FocusKey{Kind: "scene"}}}
	r := NewCachingFocusReader(getter, cache)

	kind, err := r.ConnectionFocusKind(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, session.FocusKind("scene"), kind)
	got, ok := cache.Peek(id)
	assert.True(t, ok, "miss must populate the cache")
	assert.Equal(t, session.FocusKind("scene"), got)
}

func TestCachingFocusReaderCachesGridForVanishedConnection(t *testing.T) {
	cache := NewConnectionFocusCache(4)
	id := ulid.Make()
	getter := &stubConnGetter{err: oops.Code("CONNECTION_NOT_FOUND").Errorf("gone")}
	r := NewCachingFocusReader(getter, cache)

	kind, err := r.ConnectionFocusKind(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, session.FocusKind(""), kind, "vanished connection resolves to grid")
	_, ok := cache.Peek(id)
	assert.True(t, ok, "CONNECTION_NOT_FOUND is a resolved no-focus state, cacheable as grid")
}

// Verifies fail-closed (INV-SCENE-67): a genuine read error is NOT cached and
// propagates, so the dispatcher still aborts.
func TestCachingFocusReaderPropagatesAndDoesNotCacheGenuineError(t *testing.T) {
	cache := NewConnectionFocusCache(4)
	id := ulid.Make()
	getter := &stubConnGetter{err: oops.Code("FOCUS_STORE_UNAVAILABLE").Errorf("db down")}
	r := NewCachingFocusReader(getter, cache)

	_, err := r.ConnectionFocusKind(context.Background(), id)
	require.Error(t, err)
	_, ok := cache.Peek(id)
	assert.False(t, ok, "genuine read errors must never be cached (fail-closed preserved)")
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `task test -- -run TestCachingFocusReader ./internal/command/` (via `local-check`).
Expected: FAIL — `undefined: NewCachingFocusReader`.

- [ ] **Step 4: Add the cache-first reader**

Append to `internal/command/focus_reader.go` (it already imports `context`,
`errors`, `ulid`, `oops`, `session`; add `observability`):

```go
// cachingFocusReader serves focus kinds from a ConnectionFocusCache, falling
// back to the underlying getter on a miss. It preserves fail-closed semantics
// (INV-SCENE-67): genuine read errors are never cached and propagate. The
// CONNECTION_NOT_FOUND → grid mapping matches storeFocusReader exactly.
type cachingFocusReader struct {
	getter connectionGetter
	cache  *ConnectionFocusCache
}

// NewCachingFocusReader adapts a connection getter + cache into a cache-first
// FocusReader.
func NewCachingFocusReader(getter connectionGetter, cache *ConnectionFocusCache) FocusReader {
	return &cachingFocusReader{getter: getter, cache: cache}
}

func (r *cachingFocusReader) ConnectionFocusKind(
	ctx context.Context, connectionID ulid.ULID,
) (session.FocusKind, error) {
	if kind, ok := r.cache.Peek(connectionID); ok {
		observability.RecordFocusCacheHit()
		return kind, nil
	}
	observability.RecordFocusCacheMiss()

	gen := r.cache.beginLoad()
	conn, err := r.getter.GetConnection(ctx, connectionID)
	if err != nil {
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "CONNECTION_NOT_FOUND" {
			r.cache.commitLoad(connectionID, "", gen) // resolved no-focus (grid)
			return "", nil
		}
		return "", err //nolint:wrapcheck // store errors are already oops-coded; fail-closed, uncached
	}
	kind := session.FocusKind("")
	if conn.FocusKey != nil {
		kind = conn.FocusKey.Kind
	}
	r.cache.commitLoad(connectionID, kind, gen)
	return kind, nil
}
```

Add `"github.com/holomush/holomush/internal/observability"` to the import block.

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test -- -run TestCachingFocusReader ./internal/command/` (via `local-check`).
Expected: PASS (all 4).

- [ ] **Step 6: Commit**

`jj commit -m "feat(command): cache-first FocusReader + focus_cache metrics (holomush-wm0fi)"`

---

## Task 3: `cachingSessionStore` decorator

**Files:**

- Create: `internal/command/caching_session_store.go`
- Test: `internal/command/caching_session_store_test.go`

- [ ] **Step 1: Write the failing tests**

Uses the generated `session/mocks.MockStore` (satisfies `session.Store`).

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/session/mocks"
)

// Verifies: INV-SCENE-69 — a committed focus change on a live connection
// (UpdateSessionConnection) invalidates the cache, so the next read observes the
// new value rather than a stale cached one.
func TestCachingSessionStoreEvictsAfterSuccessfulUpdateSessionConnection(t *testing.T) {
	inner := mocks.NewMockStore(t)
	cache := NewConnectionFocusCache(4)
	id := ulid.Make()
	cache.commitLoad(id, session.FocusKind("scene"), cache.beginLoad()) // pretend it was cached

	inner.EXPECT().
		UpdateSessionConnection(mock.Anything, "sess-1", id, mock.Anything).
		Return(nil)

	store := NewCachingSessionStore(inner, cache)
	err := store.UpdateSessionConnection(context.Background(), "sess-1", id,
		session.NewSessionConnectionMutator(func(i session.Info, c session.Connection) (session.Info, session.Connection, error) {
			return i, c, nil
		}))
	require.NoError(t, err)

	_, ok := cache.Peek(id)
	assert.False(t, ok, "successful UpdateSessionConnection must evict the connection's cache entry")
}

func TestCachingSessionStoreDoesNotEvictWhenUpdateFails(t *testing.T) {
	inner := mocks.NewMockStore(t)
	cache := NewConnectionFocusCache(4)
	id := ulid.Make()
	cache.commitLoad(id, session.FocusKind("scene"), cache.beginLoad())

	inner.EXPECT().
		UpdateSessionConnection(mock.Anything, "sess-1", id, mock.Anything).
		Return(oops.Code("WRITE_FAILED").Errorf("boom"))

	store := NewCachingSessionStore(inner, cache)
	err := store.UpdateSessionConnection(context.Background(), "sess-1", id,
		session.NewSessionConnectionMutator(func(i session.Info, c session.Connection) (session.Info, session.Connection, error) {
			return i, c, nil
		}))
	require.Error(t, err)

	_, ok := cache.Peek(id)
	assert.True(t, ok, "a rolled-back write must leave the cache untouched")
}

func TestCachingSessionStoreEvictsOnRemoveConnection(t *testing.T) {
	inner := mocks.NewMockStore(t)
	cache := NewConnectionFocusCache(4)
	id := ulid.Make()
	cache.commitLoad(id, session.FocusKind("scene"), cache.beginLoad())
	inner.EXPECT().RemoveConnection(mock.Anything, id).Return(nil)

	store := NewCachingSessionStore(inner, cache)
	require.NoError(t, store.RemoveConnection(context.Background(), id))
	_, ok := cache.Peek(id)
	assert.False(t, ok)
}

func TestCachingSessionStoreDeleteEnumeratesAndEvictsSessionConnections(t *testing.T) {
	inner := mocks.NewMockStore(t)
	cache := NewConnectionFocusCache(4)
	c1, c2 := ulid.Make(), ulid.Make()
	cache.commitLoad(c1, "scene", cache.beginLoad())
	cache.commitLoad(c2, "scene", cache.beginLoad())

	inner.EXPECT().ListConnectionsBySession(mock.Anything, "sess-1").
		Return([]*session.Connection{{ID: c1}, {ID: c2}}, nil)
	inner.EXPECT().Delete(mock.Anything, "sess-1").Return(nil)

	store := NewCachingSessionStore(inner, cache)
	require.NoError(t, store.Delete(context.Background(), "sess-1"))
	_, ok1 := cache.Peek(c1)
	_, ok2 := cache.Peek(c2)
	assert.False(t, ok1)
	assert.False(t, ok2)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestCachingSessionStore ./internal/command/` (via `local-check`).
Expected: FAIL — `undefined: NewCachingSessionStore`.

- [ ] **Step 3: Write the decorator**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/session"
)

// cachingSessionStore decorates a session.Store to keep a ConnectionFocusCache
// consistent with the persisted Connection.FocusKey. Correctness (INV-SCENE-69)
// hinges on UpdateSessionConnection — the sole path that changes a live
// connection's focus. Create/remove evictions are best-effort memory reclaim: a
// removed connection never dispatches, so a missed eviction is bounded memory
// (backstopped by the cache size cap), not a stale-serve.
//
// The embedded session.Store promotes every method not overridden below.
type cachingSessionStore struct {
	session.Store
	cache *ConnectionFocusCache
}

// NewCachingSessionStore wraps inner so focus reads can be cached and every
// focus-affecting write invalidates the cache.
func NewCachingSessionStore(inner session.Store, cache *ConnectionFocusCache) session.Store {
	return &cachingSessionStore{Store: inner, cache: cache}
}

// UpdateSessionConnection evicts AFTER a successful delegate — the sole
// correctness-critical invalidation (INV-SCENE-69). A rolled-back write leaves
// the cache untouched.
func (s *cachingSessionStore) UpdateSessionConnection(
	ctx context.Context, sessionID string, connectionID ulid.ULID, m session.SessionConnectionMutator,
) error {
	if err := s.Store.UpdateSessionConnection(ctx, sessionID, connectionID, m); err != nil {
		return err //nolint:wrapcheck // store errors are already oops-coded
	}
	s.cache.Evict(connectionID)
	return nil
}

// AddConnection evicts so an initial focus on a (re)created connection is not
// shadowed by any prior entry for the same ULID.
func (s *cachingSessionStore) AddConnection(ctx context.Context, conn *session.Connection) error {
	if err := s.Store.AddConnection(ctx, conn); err != nil {
		return err //nolint:wrapcheck // store errors are already oops-coded
	}
	s.cache.Evict(conn.ID)
	return nil
}

// RemoveConnection: best-effort reclaim (removed connection never dispatches).
func (s *cachingSessionStore) RemoveConnection(ctx context.Context, connectionID ulid.ULID) error {
	err := s.Store.RemoveConnection(ctx, connectionID)
	s.cache.Evict(connectionID)
	return err //nolint:wrapcheck // store errors are already oops-coded
}

// RemoveConnectionAndCount: best-effort reclaim.
func (s *cachingSessionStore) RemoveConnectionAndCount(
	ctx context.Context, sessionID string, connectionID ulid.ULID,
) (session.ConnectionCounts, bool, error) {
	counts, removed, err := s.Store.RemoveConnectionAndCount(ctx, sessionID, connectionID)
	s.cache.Evict(connectionID)
	return counts, removed, err //nolint:wrapcheck // store errors are already oops-coded
}

// Delete removes a whole session; session_connections rows cascade
// (migrations/000001_baseline.up.sql:228). Enumerate + evict before delegating.
// Best-effort: on a list error we fall through to the cap backstop.
//
// DeleteByCharacter is intentionally NOT overridden: it is keyed by characterID
// (would need a FindByCharacter → ListConnectionsBySession hop) and is a rare
// admin path; its cascade-removed connections never dispatch, so the FIFO cap
// reclaims their entries (bounded memory, not stale-serve).
func (s *cachingSessionStore) Delete(ctx context.Context, id string) error {
	if conns, listErr := s.Store.ListConnectionsBySession(ctx, id); listErr == nil {
		for _, c := range conns {
			s.cache.Evict(c.ID)
		}
	}
	return s.Store.Delete(ctx, id) //nolint:wrapcheck // store errors are already oops-coded
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run TestCachingSessionStore ./internal/command/` (via `local-check`).
Expected: PASS (all 4).

- [ ] **Step 5: Commit**

`jj commit -m "feat(command): caching session.Store decorator, evict at write chokepoint (holomush-wm0fi)"`

---

## Task 4: Wire the cache into the core stack (production + harness)

**Files:**

- Modify: `cmd/holomush/sub_grpc.go:230,371`
- Modify: `internal/testsupport/integrationtest/harness.go:314,451`

- [ ] **Step 1: Production wiring — wrap the store at its source**

In `cmd/holomush/sub_grpc.go`, immediately after `sessionStore := s.cfg.Sessions.Store()`
(`:230`), insert:

```go
	// Focus hot-path cache (holomush-wm0fi). Wrap the store here so EVERY
	// downstream consumer (command services, focus coordinator, CoreServer,
	// reaper) writes through the same decorator — invalidation is complete by
	// construction. The dispatcher's FocusReader (below) reads through the shared
	// cache.
	focusCache := command.NewConnectionFocusCache(command.DefaultFocusCacheCap)
	sessionStore = command.NewCachingSessionStore(sessionStore, focusCache)
```

Then change the dispatcher's focus reader (`:371`) from:

```go
		command.WithFocusReader(command.NewStoreFocusReader(sessionStore)),
```

to:

```go
		command.WithFocusReader(command.NewCachingFocusReader(sessionStore, focusCache)),
```

- [ ] **Step 2: Integration-harness wiring**

In `internal/testsupport/integrationtest/harness.go`, immediately after
`sessionStoreInst := store.NewPostgresSessionStore(pool)` (`:314`), insert:

```go
	focusCache := command.NewConnectionFocusCache(command.DefaultFocusCacheCap)
	sessionStoreInst = command.NewCachingSessionStore(sessionStoreInst, focusCache)
```

Then change the harness focus reader (`:451`) from
`command.WithFocusReader(command.NewStoreFocusReader(sessionStoreInst))` to
`command.WithFocusReader(command.NewCachingFocusReader(sessionStoreInst, focusCache))`.

Confirm `sessionStoreInst`'s declared type is `session.Store` (the interface) so
the reassignment type-checks; if it is the concrete `*store.PostgresSessionStore`,
change the declaration to `var sessionStoreInst session.Store = store.NewPostgresSessionStore(pool)`.

- [ ] **Step 3: Verify the stack compiles and unit tests pass**

Run: `task build` then `task test -- ./internal/command/ ./cmd/holomush/...` (via `local-check`, kind=build then kind=test).
Expected: build PASS; command tests PASS.

- [ ] **Step 4: Commit**

`jj commit -m "feat(wiring): route core stack through focus cache decorator (holomush-wm0fi)"`

---

## Task 5: Integration test — end-to-end eviction on a committed focus change

**Files:**

- Create: `test/integration/scenes/focus_cache_test.go`

Grounding note: the harness API below is copied verbatim from the sibling
`test/integration/scenes/focus_routed_input_test.go` (the grid-vs-scene pose
routing suite) — `integrationtest.Start(... WithInTreePlugins/WithPluginCrypto/WithFocusDelivery)`,
`ts.ConnectAuthed`, `ts.NewLocation`, `owner.CreateScene`, `owner.SendCommand`,
`owner.SendCommandOnConnection`, `owner.WaitForEvent`, `owner.LocationID`. `suiteT`
is the package-level suite `*testing.T`. Do not invent new harness surface.

Scope note: this single spec exercises the `AutoFocusOnJoin` class-A path
end-to-end. All three class-A paths (`SetConnectionFocus`, `AutoFocusOnJoin`,
`RestoreConnectionFocus`) funnel through the same `UpdateSessionConnection`
chokepoint, and Task 3's unit test proves that chokepoint evicts regardless of
caller — so per-path integration duplication is unnecessary (see spec § Testing).

- [ ] **Step 1: Write the failing spec (Ginkgo)**

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package scenes_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// Verifies: INV-SCENE-69 — a committed focus change on a live connection (here
// AutoFocusOnJoin, which writes Connection.FocusKey via Store.UpdateSessionConnection,
// the sole live-focus chokepoint every class-A coordinator path shares) evicts
// the hot-path focus cache, so the NEXT ambient verb observes the new focus, not
// a stale cached one. Task 3's unit test proves the chokepoint evicts for ALL
// callers; this proves the end-to-end no-stale-serve property through the real
// CoreServer.
var _ = Describe("holomush-wm0fi: focus cache invalidates on a committed focus change", func() {
	var (
		ts    *integrationtest.Server
		ctx   context.Context
		owner *integrationtest.Session
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
		DeferCleanup(cancel)
		ts = integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
			integrationtest.WithFocusDelivery(),
		)
		owner = ts.ConnectAuthed(ctx, "Owner")
	})

	AfterEach(func() {
		if owner != nil {
			owner.Logout(ctx)
		}
		ts.Stop()
	})

	It("routes a post-join pose to the scene IC stream, proving the cached grid focus was evicted", func() {
		loc := ts.NewLocation(ctx)
		sceneID := owner.CreateScene(ctx, loc)
		Expect(sceneID).NotTo(BeZero(), "CreateScene must return a non-zero bare ULID")

		// STEP 1 — prime the cache with GRID focus. Before joining, owner's
		// connection has a nil FocusKey; this pose misses the cache, reads grid
		// from the store, and POPULATES the cache with grid for owner's conn.
		// Without eviction, this stale grid entry would misroute the post-join
		// pose (STEP 3) back to the location stream — which is exactly what this
		// spec would catch as a WaitForEvent timeout on the scene IC assertion.
		Expect(owner.SendCommandOnConnection(ctx, "pose waves-on-grid")).To(Succeed())
		gridFrame := owner.WaitForEvent(ctx, "core-communication:pose")
		Expect(gridFrame).NotTo(BeNil())
		Expect(gridFrame.GetStream()).To(ContainSubstring("location."+owner.LocationID.String()),
			"pre-join pose must route to the location stream (primes the cache with grid focus)")

		// STEP 2 — commit a focus change. `scene join` drives AutoFocusOnJoin,
		// which writes FocusKey=scene via Store.UpdateSessionConnection (evicting
		// the cached grid focus) and wires the live scene IC subscription.
		Expect(owner.SendCommand(ctx, "scene join "+sceneID.String())).To(Succeed())

		// STEP 3 — the next ambient pose MUST observe the new scene focus, not the
		// stale cached grid focus. A broken eviction keeps routing to the location
		// stream, so no core-scenes:scene_pose is ever delivered and WaitForEvent
		// fails.
		Expect(owner.SendCommandOnConnection(ctx, "pose waves-in-scene")).To(Succeed())
		sceneFrame := owner.WaitForEvent(ctx, "core-scenes:scene_pose")
		Expect(sceneFrame).NotTo(BeNil(),
			"post-join pose MUST redirect to the scene once the focus change evicted the cached grid focus")
		Expect(sceneFrame.GetStream()).To(ContainSubstring("scene."+sceneID.String()+".ic"),
			"the delivered event's stream MUST be the scene IC subject, proving the cache served the new focus")
	})
})
```

- [ ] **Step 2: Build the binary plugins and run the integration suite**

Run: `task test:int -- ./test/integration/scenes/...` (via `local-check`, kind=int).
Expected: PASS (harness auto-builds binary plugins).

- [ ] **Step 3: Commit**

`jj commit -m "test(scenes): integration coverage for focus-cache invalidation (holomush-wm0fi)"`

---

## Task 6: Bind INV-SCENE-69 in the registry

**Files:**

- Modify: `docs/architecture/invariants.yaml`
- (`docs/architecture/invariants.md` is regenerated, never hand-edited)

- [ ] **Step 1: Flip the invariant entry from pending to bound**

The `INV-SCENE-69` entry already exists in `docs/architecture/invariants.yaml`
as `binding: pending` (registered when the spec landed, so the spec-orphan check
passed). Task 6 flips it to `bound` and adds `asserted_by`. Locate the existing
entry (under the `INV-SCENE` scope, after `INV-SCENE-68`) and change
`binding: pending` to `binding: bound`, then add the `asserted_by` block:

```yaml
  - id: INV-SCENE-69
    scope: INV-SCENE
    origin_spec: "docs/superpowers/specs/2026-07-07-focus-hotpath-cache-design.md"
    legacy: []
    summary: >-
      # (leave the existing summary text unchanged)
    binding: bound
    asserted_by:
      - "internal/command/caching_session_store_test.go"
      - "internal/command/connection_focus_cache_test.go"
      - "test/integration/scenes/focus_cache_test.go"
```

(If the schema requires `legacy:` or `boundary:` keys on SCENE entries, copy them
verbatim from the `INV-SCENE-67` entry — do not invent fields.)

- [ ] **Step 2: Confirm the `// Verifies:` annotations exist**

The annotations were added in Tasks 3, 1, and 5:

- `TestCachingSessionStoreEvictsAfterSuccessfulUpdateSessionConnection` (Task 3) — the eviction clause.
- `TestConnectionFocusCacheCommitDroppedWhenEvictRacesLoad` (Task 1) — the generation-guard clause. Add `// Verifies: INV-SCENE-69` immediately above it now.
- The integration `Describe`/`It` (Task 5) — the end-to-end property.

- [ ] **Step 3: Regenerate and verify**

Run: `go run ./cmd/inv-render` (regenerates `invariants.md`), then
`task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` (via `local-check`).
Expected: PASS — INV-SCENE-69 bound, generated diff clean.

- [ ] **Step 4: Commit**

`jj commit -m "docs(invariants): bind INV-SCENE-69 focus-cache no-stale-serve (holomush-wm0fi)"`

---

## Post-Implementation Checklist

- [ ] `task fmt` (SPDX headers, table reflow) — commit any mutations.
- [ ] `task pr-prep` green (fast lane); the diff touches integration surface, so
      run `HOLOMUSH_PR_PREP_FORCE_FULL=1 task pr-prep` (full lane) — ABAC/focus
      integration blast radius per prior focus-work incidents.
- [ ] `code-reviewer` (+ `abac-reviewer` is NOT required — no `internal/access/`
      changes; confirm the diff stays out of `internal/access/`).
- [ ] Benchmark the hot path under concurrent dispatch (spec Q3); if the single
      `sync.Mutex` shows contention, file a follow-up bead for `RWMutex`/sharding.
      Otherwise note "no contention observed" and close the question.
- [ ] Confirm the `focus_cache_hit` rate approaches 1 on ambient verbs (spec
      Observability) — validates the regression is closed.
