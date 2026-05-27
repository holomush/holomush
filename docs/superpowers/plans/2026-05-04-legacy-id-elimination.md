<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# `legacy_id` Elimination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate `eventbusv1.Actor.legacy_id` and the upstream `core.Actor.ID` mixed-semantic-identifier overloading. Replace with uniform ULID identity for plugin actors at every layer (`core.Actor`, `eventbusv1.Actor`, `corev1.Actor`).

**Architecture:** Hub-and-spoke around a new `IdentityRegistry` (in-memory cache backed by a `plugins` Postgres table). Plugin emit stamp sites in `goplugin/host.go` resolve plugin-name → ULID via `IDByName` and stamp `core.Actor{ID: pluginULID.String()}`. System actors use compile-time sentinel ULID constants (`SystemActorULID`, `WorldServiceActorULID`) registered in the registry's cache at bootstrap. Bus conversion functions parse `core.Actor.ID` as ULID uniformly (no kind dispatch). Actor-name display goes through `internal/grpc/server.go::actorIDString`, which gains an `IdentityRegistry` dependency to resolve plugin/system ULIDs to display names. ABAC engine code is unchanged.

**Tech Stack:** Go 1.23+, Postgres (testcontainers via `testcontainers-go/modules/postgres`), JetStream/NATS, Protobuf 3 (`buf generate`), Ginkgo/Gomega for integration BDD, testify for unit asserts, `jj` (colocated) for VCS, `task` for build/test/lint/migrate.

**Spec:** [`docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md`](../specs/2026-05-04-legacy-id-elimination-design.md).

**Bead:** Top-level epic `holomush-w9ml`. Plan tasks are filed as children of `w9ml`.

**Plan revision:** **Revision 3** (2026-05-04). Revision 1 was caught NOT READY by `plan-reviewer` with 15 blocking findings (fabricated symbols, wrong names, missing helpers, multi-task `jj describe` collisions). Revision 2 addressed all 15 but introduced 4 new blockers (UnloadPlugin cache-clear gated by early-return; `newManagerForRegistryTest` missing mandatory `WithVerbRegistry`; T6 defer-rollback closure captured wrong `err`; T21 fictitious E2E harness). Revision 3 (this version) addresses all 4.

---

## Repo grounding (verified before this plan was written)

These facts are confirmed against the worktree at `/Volumes/Code/github.com/holomush/.worktrees/legacy-id-elimination`. Implementers MUST re-verify if executing later than 2026-05-04.

| Symbol / fact | Reality |
|---|---|
| `core.ActorKind` constants | `ActorCharacter`, `ActorSystem`, `ActorPlugin` only. **No `ActorUnknown`, no `ActorPlayer`** at the core layer. |
| `eventbus.ActorKind` constants | `ActorKindUnknown` (0), `ActorKindCharacter` (1), `ActorKindPlayer` (2), `ActorKindSystem` (3), `ActorKindPlugin` (4). |
| `core.ActorSystemID` | `const = "system"` at `internal/core/event.go:164-165`. Used by 1 production site (`engine_end_session.go:56`) and 4 test sites. |
| `Manifest` runtime field | `Manifest.Type` (type `Type`); constants `TypeLua`, `TypeBinary`, `TypeSetting`. **Not** `Manifest.Runtime`. |
| Binary plugin executable path | `Manifest.BinaryPlugin.Executable`. **Not** `Manifest.Binary.Path`. |
| Lua plugin entry | `Manifest.LuaPlugin.Entry`. |
| Bus translation function (cmd/holomush) | `coreToBusActor` (returns `eventbus.Actor`, no error). **Not** `coreActorToBusActor`. |
| Bus translation kind helper | `coreActorKindToBus`, `busActorKindToCore`. **Not** `actorKindFromCore`. |
| Actor display path | `internal/grpc/server.go:600::actorIDString` — formats actor for the gRPC wire. Telnet gateway calls `truncateActorID(ev.GetActorId())` at `gateway_handler.go:918,950`. The gRPC server is the resolution point; the gateway is a pass-through (per CLAUDE.md "Gateway Boundary"). |
| Manager unload | **No** `Manager.UnloadPlugin` method exists. `host.Unload(ctx, name)` is called inline at `manager.go:777, 793, 822, 913, 924, 948`. This plan adds `Manager.UnloadPlugin` as a new exported method (T7). |
| Manager mutex | `Manager.mu sync.RWMutex` (existing). The plan reuses it for the new identity cache. |
| Plugin-actor stamp sites | `internal/plugin/goplugin/host.go:560, 566, 631, 637`. Confirmed by `rg`. |
| System-actor stamp sites | `internal/world/event_store_adapter.go:34-37`, `internal/grpc/server.go:531-534`, `internal/command/types.go:619-622`, `internal/core/engine_end_session.go:56`. Confirmed by `rg`. |
| Migration runner | `internal/store/migrate.go::Migrator` with `Up()`, `Down()`, `Steps(n)`, `Migrate(version)`. |
| Testcontainer pattern | `testcontainers-go/modules/postgres` per `internal/store/migrate_integration_test.go`. **No** existing `newTestPool` / `runMigrations` helpers. T0.5 adds them. |
| Proto-gen task | `task proto` (runs `buf generate` + internal-module template). **Not** `task proto:gen`. |
| `core.NewULID()` | `internal/core/ulid.go` — monotonic ULID generator for events. |
| `idgen.New()` | `internal/idgen/id.go` — fresh-entropy ULID for entity primary keys. Use this for plugin row IDs (per spec). |
| Bus translation kind asymmetry | `core.ActorKind` (3 values) ↔ `eventbus.ActorKind` (5 values). `core` lacks `Unknown` and `Player`. The translation `coreActorKindToBus` maps default → `ActorKindUnknown`. Post-epic, this default-case behavior is unchanged: empty `core.Actor.ID` produces `eventbusv1.Actor{Kind: ActorKindUnknown, id: nil}`. |

---

## File Structure

### New files

| Path | Responsibility |
|------|----------------|
| `internal/store/testhelpers_test.go` | (T0.5) `newTestPool`, `runMigrations(t, pool, n)` test helpers — testcontainer-backed |
| `internal/store/migrations/000018_create_plugins.up.sql` | (T1) Create `plugins` table + partial UNIQUE index + TRUNCATE `events_audit` |
| `internal/store/migrations/000018_create_plugins.down.sql` | (T1) Drop `plugins` table |
| `internal/store/plugin_repo.go` | (T3) `PluginRepo` interface + `PostgresPluginRepo` implementation |
| `internal/store/plugin_repo_test.go` | (T3) Repo unit tests |
| `internal/store/no_delete_grep_test.go` | (T20) INV-W9ML-9 CI grep |
| `internal/plugin/identity_registry.go` | (T4) `IdentityRegistry` interface |
| `internal/plugin/identity_registry_test.go` | (T5-T7) Manager-implements-Registry tests |
| `internal/plugin/manager_unload.go` | (T7) `Manager.UnloadPlugin` method (new) |
| `internal/plugin/plugintest/registry.go` | (T15) `PluginULIDFromName` test helper |
| `internal/eventbus/no_legacy_id_grep_test.go` | (T13) INV-W9ML-7 CI grep |
| `cmd/holomush-cutover/main.go` | (T18) One-time PG TRUNCATE + JS PurgeStream command |
| `site/docs/operating/legacy-id-cutover.md` | (T18) Operator runbook |
| `test/integration/plugin/legacy_id_e2e_test.go` | (T21) End-to-end happy path |

### Modified files

| Path | Reason |
|------|--------|
| `internal/core/event.go` | (T2) Add `SystemActorULID`, `WorldServiceActorULID` sentinels + `IsSentinelULID` helper; repurpose `ActorSystemID` from `const = "system"` to `var = SystemActorULID.String()`; update `Actor.ID` field comment |
| `internal/core/event_test.go` | (T2) Add `TestSentinelTagsUnique` and sentinel resolution tests |
| `api/proto/holomush/eventbus/v1/eventbus.proto` | (T11) Remove `string legacy_id = 3` |
| `pkg/proto/holomush/eventbus/v1/eventbus.pb.go` | (T11) Regenerated by `task proto` |
| `internal/eventbus/types.go` | (T12) Remove `LegacyID` field from `Actor` struct |
| `internal/eventbus/publisher.go` | (T13) Remove `HeaderActorLegacyID` constant + 3 read/write LegacyID branches at lines 50-55, 316-317, 351, 431-432 |
| `internal/eventbus/subscriber.go` | (T14) Remove `out.LegacyID = legacy` fallback at lines 770-773 |
| `internal/eventbus/history/hot_jetstream.go` | (T14) Remove `out.LegacyID = legacy` at lines 592-598 |
| `internal/eventbus/history/cold_postgres.go` | (T14) Remove `holomush-u5bb` TODO + LegacyID branch at lines 417-440 |
| `internal/grpc/server.go` | (T16) `actorIDString` rewrite — preserve zero-ULID guard, add `IdentityRegistry` parameter for plugin/system display-name resolution; system stamp at lines 531-534 migrate to sentinel |
| `internal/grpc/query_stream_history.go` | (T16) `eventbusEventToEventFrame` — remove LegacyID fallback at lines 512-522; resolve via `IdentityRegistry` |
| `cmd/holomush/sub_grpc.go` | (T14) `coreToBusActor` simplifies (no LegacyID branch); `busEventToCoreEvent` LegacyID fallback removed |
| `internal/plugin/manager.go` | (T5/T6/T8) `IdentityRegistry` integration: cache fields, `WithPluginRepo` option, sentinel bootstrap, hash compute, `loadPlugin` Upsert + drift logging, sweep at end of `LoadAll` |
| `internal/plugin/config.go` (or wherever `RetentionDays` belongs — verify at impl time) | (T8) Add `RetentionDays int` |
| `internal/plugin/event_emitter.go` | (T14) `coreActorToEventbusActor` parses `Actor.ID` as ULID uniformly; surfaces `ACTOR_ID_NOT_ULID` |
| `internal/plugin/goplugin/host.go` | (T9) 4 stamp sites at 560/566/631/637 → registry-resolved ULID-string |
| `internal/world/event_store_adapter.go` | (T10) Migrate system stamp at 34-37 |
| `internal/command/types.go` | (T10) Migrate system stamp at 619-622 |
| `internal/core/engine_end_session.go` | (T10) No code change — `ActorSystemID` value changes underneath |
| `internal/eventbus/audit/projection.go` | (T13) Remove `App-Actor-Legacy-ID` from header allowlist if present |
| `cmd/holomush/main.go` (or wherever bootstrap is wired) | (T18, T19) Wire `IdentityRegistry` into renderers / `actorIDString` and the bootstrap orphan check |
| Various test files (~15) | (T15) Replace `LegacyID: "X"` with `ID: plugintest.PluginULIDFromName("X").Bytes()` (or `.String()` for `core.Actor`) |

### Deleted tests

| Path | Tests / blocks deleted |
|------|------------------------|
| `cmd/holomush/sub_grpc_adapters_test.go:154-236` | `TestCoreToBusActorStashesNonULIDAsLegacyID`, `TestBusEventToCoreEventFallsBackToLegacyID` |
| `internal/eventbus/publisher_test.go:284-299, 350` | LegacyID fallback cases |
| `internal/eventbus/history/actor_from_envelope_test.go:47-67` | `TestActorFromEnvelopeFallsBackToLegacyID` |
| `test/integration/crypto/inv49_envelope_roundtrip_test.go:111` | Renamed (T17) |
| `test/integration/crypto/inv49_envelope_roundtrip_test.go:165` | `useLegacyActor: true` table case deleted |
| `test/integration/crypto/e2e_test.go:647` | Block deleted |
| `test/integration/crypto/e2e_test.go:225` | `publishSensitiveWithLegacyActor` helper deleted |

---

## Execution Discipline

**VCS:** This is a jj-colocated repo. Every task ends with TWO jj commands:

1. `JJ_EDITOR=true jj --no-pager describe -m "<message>"` — set the description on the current commit (`@`).
2. `jj new -m "T<next> (in progress)"` — create a new empty commit on top, so subsequent edits land in `@`.

Without step 2, the next task's edits land IN the previous task's commit and the next `jj describe` overwrites the message. **Failure mode:** by T5 the entire stack collapses into a single commit. **Source:** project memory `feedback_jj_empty_wc_before_task` and reviewer Finding 10.

The very last task (T22) ends with describe only — no new commit is needed.

**Workspace:** `/Volumes/Code/github.com/holomush/.worktrees/legacy-id-elimination` on top of `main`.

**Test commands** (per CLAUDE.md):

- `task test -- ./internal/foo/` — single package
- `task test -- -run TestFoo ./internal/foo/` — specific test
- `task test:int` — integration tests (Docker required)
- `task lint`, `task fmt`, `task pr-prep` (final gate)

**Per-task pattern (TDD):**

1. Write failing test.
2. Run to verify it fails.
3. Implement.
4. Run to verify pass.
5. `jj describe`.
6. `jj new` (except final task).
7. Update bead status.

**Final gate:** Before merge, `task pr-prep` once on the final state. Must produce green output.

---

## Pre-flight Verification

### Task 0: Repo-state checks

**Files:** None modified.
**Bead:** N/A.

- [ ] **Step 1: Confirm worktree state**

```bash
pwd && jj st
```

Expected: `pwd` = `/Volumes/Code/github.com/holomush/.worktrees/legacy-id-elimination`. `jj st` shows clean working copy on top of the spec/plan commit.

- [ ] **Step 2: Confirm migration 000018 is next**

```bash
ls internal/store/migrations/ | tail -5
```

Expected: last entry is `000017_events_audit_envelope_rename.{up,down}.sql`.

- [ ] **Step 3: Confirm LegacyID touch points present**

```bash
rg -n 'LegacyID|legacy_id|App-Actor-Legacy-ID' --type=go -g '!*_test.go' -g '!*.pb.go'
```

Expected: hits in (at minimum) `internal/eventbus/types.go`, `internal/eventbus/publisher.go`, `internal/eventbus/subscriber.go`, `internal/eventbus/history/hot_jetstream.go`, `internal/eventbus/history/cold_postgres.go`, `internal/grpc/server.go`, `internal/grpc/query_stream_history.go`, `cmd/holomush/sub_grpc.go`, plus `api/proto/holomush/eventbus/v1/eventbus.proto`. If a spec-listed file is missing, halt and reconcile.

- [ ] **Step 4: Confirm plugin-actor stamp sites**

```bash
rg -n 'core.Actor\{Kind: core.ActorPlugin' internal/plugin/goplugin/host.go
```

Expected: 4 hits at lines 560, 566, 631, 637.

- [ ] **Step 5: Confirm system-actor stamp sites**

```bash
rg -n 'Kind: ActorSystem|Kind: core.ActorSystem' internal/ -g '!*_test.go'
```

Expected: 4 hits — `internal/world/event_store_adapter.go:35`, `internal/grpc/server.go:533`, `internal/command/types.go:621` (line ±1 OK), `internal/core/engine_end_session.go:56`.

- [ ] **Step 6: Confirm plugin manifests don't store envelope blobs**

```bash
rg -n 'eventbusv1.Event|eventbus.Event' plugins/
```

Expected: zero matches.

If any check fails, halt and reconcile spec or repo state.

---

## Phase 1 — Foundation: test harness + sentinels + schema + repo + interface

### Task 0.5: Store test harness — `newTestPool` + `runMigrations`

**Files:**

- Create: `internal/store/testhelpers_test.go`

**Bead:** `bd create --title "w9ml T0.5: store test harness (newTestPool, runMigrations)" --parent=holomush-w9ml --type=task --priority=1`

**Why this exists:** Reviewer Finding 5 — multiple downstream tasks reference `newTestPool(t)` and `runMigrations(ctx, pool, n)` as if they exist. They don't. This task creates them.

- [ ] **Step 1: Write the helper**

Create `internal/store/testhelpers_test.go`:

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/holomush/holomush/internal/store"
)

// newTestPool spins up a Postgres testcontainer, returns a connected
// *pgxpool.Pool, and a cleanup function. The cleanup terminates the
// container and closes the pool.
//
// Mirrors the pattern in migrate_integration_test.go — kept distinct so
// integration tests in this package can opt in selectively.
func newTestPool(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()
	pgC, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:16-alpine"),
		postgres.WithDatabase("holomush_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
	)
	require.NoError(t, err)

	connStr, err := pgC.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)

	cleanup := func() {
		pool.Close()
		_ = pgC.Terminate(ctx)
	}
	return pool, cleanup
}

// runMigrations applies migrations 1..targetVersion against the testcontainer
// rooted at pool. Uses store.Migrator (which wraps golang-migrate).
func runMigrations(ctx context.Context, pool *pgxpool.Pool, targetVersion uint) error {
	connStr := pool.Config().ConnString()
	migrator, err := store.NewMigrator(connStr)
	if err != nil {
		return err
	}
	defer migrator.Close()
	return migrator.Migrate(targetVersion)
}
```

(Note: `Migrator.Close()` returns two errors per `migrate.Close()`. Adjust the defer accordingly — use a small wrapper or pattern from `migrate.go`.)

- [ ] **Step 2: Smoke-test the helper**

Add a sanity test in the same file:

```go
func TestNewTestPoolAndRunMigrationsSmoke(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()

	require.NoError(t, runMigrations(ctx, pool, 17)) // pre-w9ml state
	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM events_audit`).Scan(&n))
}
```

Run: `task test:int -- -run TestNewTestPoolAndRunMigrationsSmoke ./internal/store/...`
Expected: PASS.

- [ ] **Step 3: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "test(store): newTestPool + runMigrations helpers (holomush-w9ml T0.5)"
jj new -m "T1 (in progress)"
bd update <T0.5-bead-id> --status=closed
```

---

### Task 1: Schema migration 000018

**Files:**

- Create: `internal/store/migrations/000018_create_plugins.up.sql`
- Create: `internal/store/migrations/000018_create_plugins.down.sql`
- Create: `internal/store/migrate_plugins_integration_test.go`

**Bead:** `bd create --title "w9ml T1: migration 000018 (plugins table + events_audit truncate)" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Write failing test**

Create `internal/store/migrate_plugins_integration_test.go`:

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration000018CreatesPluginsTable(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()

	require.NoError(t, runMigrations(ctx, pool, 18))

	var count int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'plugins'
		  AND column_name IN ('id','name','display_name','version',
		                      'manifest_hash','content_hash',
		                      'first_seen_at','last_seen_at','gc_at')
	`).Scan(&count))
	assert.Equal(t, 9, count, "plugins table must have 9 columns")

	var indexExists bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE indexname = 'plugins_name_active'
			  AND indexdef LIKE '%WHERE (gc_at IS NULL)%'
		)
	`).Scan(&indexExists))
	assert.True(t, indexExists, "partial UNIQUE index plugins_name_active must exist")
}

func TestMigration000018TruncatesEventsAudit(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()

	require.NoError(t, runMigrations(ctx, pool, 17))
	_, err := pool.Exec(ctx, `
		INSERT INTO events_audit (id, subject, type, timestamp, actor_kind,
		                         envelope, schema_ver, codec, js_seq, rendering)
		VALUES ($1, 'test', 'test', now(), 'plugin', '\x00', 1, 'identity', 1, '{}'::jsonb)
	`, []byte("0123456789abcdef"))
	require.NoError(t, err)

	require.NoError(t, runMigrations(ctx, pool, 18))

	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM events_audit`).Scan(&n))
	assert.Equal(t, 0, n, "events_audit MUST be empty after migration 000018")
}
```

- [ ] **Step 2: Run to verify failure**

```bash
task test:int -- -run TestMigration000018 ./internal/store/...
```

Expected: FAIL — migration files don't exist.

- [ ] **Step 3: Create the up migration**

`internal/store/migrations/000018_create_plugins.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE IF NOT EXISTS plugins (
    id              BYTEA       PRIMARY KEY,
    name            TEXT        NOT NULL,
    display_name    TEXT        NOT NULL,
    version         TEXT        NOT NULL,
    manifest_hash   BYTEA       NOT NULL,
    content_hash    BYTEA,
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    gc_at           TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS plugins_name_active
    ON plugins(name)
    WHERE gc_at IS NULL;

-- Eliminate legacy plugin-actor events whose envelope blobs carry
-- Actor.legacy_id (string). Post-w9ml the proto field is gone, so old
-- envelopes cannot round-trip cleanly. Irreversible at the data layer.
TRUNCATE events_audit;
```

- [ ] **Step 4: Create the down migration**

`internal/store/migrations/000018_create_plugins.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP TABLE IF EXISTS plugins;
-- Note: events_audit TRUNCATE in up is irreversible. Down rolls back schema
-- only; truncated rows are not restored.
```

- [ ] **Step 5: Run to verify pass**

```bash
task test:int -- -run TestMigration000018 ./internal/store/...
```

Expected: PASS.

- [ ] **Step 6: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(store): migration 000018 — create plugins table; TRUNCATE events_audit (holomush-w9ml T1)"
jj new -m "T2 (in progress)"
bd update <T1-bead-id> --status=closed
```

---

### Task 2: Sentinel ULIDs + IsSentinelULID + ActorSystemID repurpose

**Files:**

- Modify: `internal/core/event.go` (add sentinels at line ~165; convert `ActorSystemID` const → var; update `Actor.ID` doc comment)
- Modify or create: `internal/core/event_test.go` (add `TestSentinelTagsUnique` etc.)

**Bead:** `bd create --title "w9ml T2: sentinel ULIDs + IsSentinelULID + ActorSystemID repurpose" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Write failing tests**

Append to `internal/core/event_test.go`:

```go
package core

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemActorULIDRendersAsCanonicalCrockford(t *testing.T) {
	assert.Equal(t, "00000000000000000000000001", SystemActorULID.String())
}

func TestWorldServiceActorULIDRendersAsCanonicalCrockford(t *testing.T) {
	assert.Equal(t, "00000000000000000000000002", WorldServiceActorULID.String())
}

func TestActorSystemIDIsSystemActorULIDString(t *testing.T) {
	assert.Equal(t, SystemActorULID.String(), ActorSystemID,
		"ActorSystemID MUST equal SystemActorULID.String() post-w9ml")
}

func TestIsSentinelULIDIdentifiesKnownSentinels(t *testing.T) {
	assert.True(t, IsSentinelULID(SystemActorULID))
	assert.True(t, IsSentinelULID(WorldServiceActorULID))
}

func TestIsSentinelULIDRejectsZeroULID(t *testing.T) {
	assert.False(t, IsSentinelULID(ulid.ULID{}),
		"all-zero ULID is reserved as 'no sentinel' (tag 0x00)")
}

func TestIsSentinelULIDRejectsEntropyULID(t *testing.T) {
	entropy := NewULID()
	assert.False(t, IsSentinelULID(entropy),
		"entropy ULIDs MUST NOT be classified as sentinels")
}

func TestSentinelTagsUnique(t *testing.T) {
	all := map[byte]string{}
	check := func(label string, id ulid.ULID) {
		require.True(t, IsSentinelULID(id), "%s must satisfy IsSentinelULID", label)
		tag := id[15]
		if existing, ok := all[tag]; ok {
			t.Fatalf("sentinel tag-byte collision: %s and %s both use 0x%02x", existing, label, tag)
		}
		all[tag] = label
	}
	check("SystemActorULID", SystemActorULID)
	check("WorldServiceActorULID", WorldServiceActorULID)
}
```

- [ ] **Step 2: Run to verify failure**

```bash
task test -- -run 'TestSystemActor|TestWorldServiceActor|TestActorSystemID|TestIsSentinelULID|TestSentinelTagsUnique' ./internal/core/
```

Expected: FAIL — symbols undefined.

- [ ] **Step 3: Modify `internal/core/event.go`**

Locate lines 164-171:

```go
// ActorSystemID is the well-known ID used for Actor{Kind: ActorSystem} events.
const ActorSystemID = "system"

// Actor represents who or what caused an event.
type Actor struct {
	Kind ActorKind
	ID   string // Character ID, plugin name, or ActorSystemID
}
```

Replace with (note: the `ulid` import must already exist or be added):

```go
// SystemActorULID is the canonical identity for the host's "system" actor —
// the categorical bucket for events emitted by the host itself rather than
// by a character, player, or plugin. Defined as a fixed byte pattern (not
// entropy-generated) so audit rows and history queries reliably round-trip
// the same identity. The all-zero leading 15 bytes plus a low-numbered tag
// byte make sentinels visually distinguishable from real entropy ULIDs in
// logs.
var SystemActorULID = ulid.ULID{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x01}

// WorldServiceActorULID is the identity for events emitted by the world
// service subsystem (location/object/exit lifecycle).
var WorldServiceActorULID = ulid.ULID{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x02}

// ActorSystemID is the canonical Actor.ID value for the host's system
// actor. Pre-w9ml this was the literal "system"; post-w9ml it is the
// canonical ULID-string form of SystemActorULID. The 1 production + 4 test
// call sites compile unchanged — only the value flowing through them
// changes (string → ULID-string).
var ActorSystemID = SystemActorULID.String()

// IsSentinelULID returns true iff id is a system actor sentinel ULID:
// first 15 bytes zero, last byte in [0x01, 0xFF]. Used by IdentityRegistry
// bootstrap (sentinel-collision detection on plugin row load) and by
// TestSentinelTagsUnique. Tag 0x00 is reserved as "no sentinel" — the
// all-zero ULID is the proto3 zero-value and would be wire-indistinguishable
// from absence-of-id.
//
// Tag-byte allocation policy: tags MUST be unique across the codebase and
// MUST be allocated via PR review of this file (single source of truth).
// Existing allocations: 0x01 = SystemActorULID, 0x02 = WorldServiceActorULID.
func IsSentinelULID(id ulid.ULID) bool {
	if id[15] == 0x00 {
		return false
	}
	for i := 0; i < 15; i++ {
		if id[i] != 0 {
			return false
		}
	}
	return true
}

// Actor represents who or what caused an event.
type Actor struct {
	Kind ActorKind
	// ID is the canonical ULID-string identity:
	//   Character: ULID from the user store (already in place).
	//   Plugin:    ULID from the plugin registry (resolved at stamp time
	//              via plugins.IdentityRegistry.IDByName, post-w9ml).
	//   System:    one of the sentinel ULID-strings above (SystemActorULID,
	//              WorldServiceActorULID, …) accessed via ActorSystemID or
	//              the typed sentinel constants.
	// core.Actor has no Unknown kind — empty ID is undefined behavior at
	// the core layer; the bus translation maps to ActorKindUnknown.
	ID string
}
```

If `ulid` is not yet imported, add `"github.com/oklog/ulid/v2"` to the import block.

- [ ] **Step 4: Run to verify pass**

```bash
task test -- -run 'TestSystemActor|TestWorldServiceActor|TestActorSystemID|TestIsSentinelULID|TestSentinelTagsUnique' ./internal/core/
```

Expected: PASS.

```bash
task test -- ./internal/core/
```

Expected: PASS — the existing tests at `engine_end_session_test.go:78` and others compare `events[0].Actor.ID` against `ActorSystemID`. The constant changed value but tests still pass because they're rvalue comparisons.

- [ ] **Step 5: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(core): sentinel ULIDs + IsSentinelULID; repurpose ActorSystemID (holomush-w9ml T2)"
jj new -m "T3 (in progress)"
bd update <T2-bead-id> --status=closed
```

---

### Task 3: PluginRepo interface + Postgres implementation

**Files:**

- Create: `internal/store/plugin_repo.go`
- Create: `internal/store/plugin_repo_test.go`

**Bead:** `bd create --title "w9ml T3: PluginRepo + PostgresPluginRepo" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Write failing tests**

Create `internal/store/plugin_repo_test.go`:

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/store"
)

func TestPluginRepoUpsertInsertsNewRow(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()
	require.NoError(t, runMigrations(ctx, pool, 18))

	repo := store.NewPostgresPluginRepo(pool)
	id, drift, err := repo.Upsert(ctx, store.PluginUpsertInput{
		Name: "core-scenes", DisplayName: "Core Scenes", Version: "1.0.0",
		ManifestHash: []byte{0x01, 0x02, 0x03}, ContentHash: []byte{0x04, 0x05},
	})
	require.NoError(t, err)
	assert.Nil(t, drift)
	_, parseErr := ulid.Parse(id.String())
	assert.NoError(t, parseErr)
}

func TestPluginRepoUpsertUpdatesLastSeenWithoutDrift(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()
	require.NoError(t, runMigrations(ctx, pool, 18))
	repo := store.NewPostgresPluginRepo(pool)

	in := store.PluginUpsertInput{
		Name: "core-scenes", DisplayName: "Core", Version: "1.0.0",
		ManifestHash: []byte{0x01}, ContentHash: []byte{0x04},
	}
	id1, _, err := repo.Upsert(ctx, in)
	require.NoError(t, err)
	id2, drift, err := repo.Upsert(ctx, in)
	require.NoError(t, err)
	assert.Equal(t, id1, id2)
	assert.Nil(t, drift)
}

func TestPluginRepoUpsertReportsDriftOnHashChange(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()
	require.NoError(t, runMigrations(ctx, pool, 18))
	repo := store.NewPostgresPluginRepo(pool)

	in1 := store.PluginUpsertInput{
		Name: "core-scenes", DisplayName: "Core", Version: "1.0.0",
		ManifestHash: []byte{0x01}, ContentHash: []byte{0x04},
	}
	id1, _, err := repo.Upsert(ctx, in1)
	require.NoError(t, err)

	in2 := in1
	in2.ManifestHash = []byte{0xAA, 0xBB}
	in2.Version = "1.1.0"
	id2, drift, err := repo.Upsert(ctx, in2)
	require.NoError(t, err)
	assert.Equal(t, id1, id2)
	require.NotNil(t, drift)
	assert.Equal(t, []byte{0x01}, drift.OldManifestHash)
	assert.Equal(t, []byte{0xAA, 0xBB}, drift.NewManifestHash)
	assert.Equal(t, "1.0.0", drift.VersionBefore)
	assert.Equal(t, "1.1.0", drift.VersionAfter)
}

func TestPluginRepoListAllReturnsActiveAndDeactivated(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()
	require.NoError(t, runMigrations(ctx, pool, 18))
	repo := store.NewPostgresPluginRepo(pool)

	_, _, err := repo.Upsert(ctx, store.PluginUpsertInput{Name: "active", DisplayName: "A", Version: "1", ManifestHash: []byte{0x01}})
	require.NoError(t, err)
	_, _, err = repo.Upsert(ctx, store.PluginUpsertInput{Name: "stale", DisplayName: "S", Version: "1", ManifestHash: []byte{0x02}})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE plugins SET last_seen_at = now() - interval '99 days' WHERE name = 'stale'`)
	require.NoError(t, err)
	_, err = repo.SweepInactive(ctx, 1)
	require.NoError(t, err)

	rows, err := repo.ListAll(ctx)
	require.NoError(t, err)
	assert.Len(t, rows, 2)
	var active, deactivated int
	for _, r := range rows {
		if r.GcAt == nil {
			active++
		} else {
			deactivated++
		}
	}
	assert.Equal(t, 1, active)
	assert.Equal(t, 1, deactivated)
}

func TestPluginRepoSweepInactiveDeactivatesStaleRowsOnly(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()
	require.NoError(t, runMigrations(ctx, pool, 18))
	repo := store.NewPostgresPluginRepo(pool)

	_, _, _ = repo.Upsert(ctx, store.PluginUpsertInput{Name: "fresh", DisplayName: "F", Version: "1", ManifestHash: []byte{0x01}})
	_, _, _ = repo.Upsert(ctx, store.PluginUpsertInput{Name: "stale", DisplayName: "S", Version: "1", ManifestHash: []byte{0x02}})
	_, err := pool.Exec(ctx, `UPDATE plugins SET last_seen_at = now() - interval '5 days' WHERE name = 'stale'`)
	require.NoError(t, err)

	swept, err := repo.SweepInactive(ctx, 3)
	require.NoError(t, err)
	require.Len(t, swept, 1)
	assert.Equal(t, "stale", swept[0].Name)
}

func TestPluginRepoSweepNeverDeletesRows(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()
	require.NoError(t, runMigrations(ctx, pool, 18))
	repo := store.NewPostgresPluginRepo(pool)

	_, _, _ = repo.Upsert(ctx, store.PluginUpsertInput{Name: "p", DisplayName: "P", Version: "1", ManifestHash: []byte{0x01}})
	_, err := pool.Exec(ctx, `UPDATE plugins SET last_seen_at = now() - interval '99 days'`)
	require.NoError(t, err)
	_, err = repo.SweepInactive(ctx, 1)
	require.NoError(t, err)

	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM plugins`).Scan(&n))
	assert.Equal(t, 1, n, "SweepInactive MUST NOT delete; only set gc_at")
}
```

- [ ] **Step 2: Run to verify failure**

```bash
task test:int -- -run TestPluginRepo ./internal/store/...
```

Expected: FAIL — types undefined.

- [ ] **Step 3: Implement `internal/store/plugin_repo.go`**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/idgen"
)

// PluginUpsertInput is the row data for Upsert. ContentHash MAY be nil
// (setting plugins have no executable artifact).
type PluginUpsertInput struct {
	Name         string
	DisplayName  string
	Version      string
	ManifestHash []byte
	ContentHash  []byte
}

// PluginRow materializes a row from the plugins table.
type PluginRow struct {
	ID           ulid.ULID
	Name         string
	DisplayName  string
	Version      string
	ManifestHash []byte
	ContentHash  []byte
	FirstSeenAt  time.Time
	LastSeenAt   time.Time
	GcAt         *time.Time
}

// DriftReport is non-nil when Upsert observed manifest_hash, content_hash,
// or version drift on an existing row.
type DriftReport struct {
	OldManifestHash []byte
	NewManifestHash []byte
	OldContentHash  []byte
	NewContentHash  []byte
	VersionBefore   string
	VersionAfter    string
}

// PluginRepo persists per-plugin identity (ULID + name + revision metadata).
type PluginRepo interface {
	Upsert(ctx context.Context, in PluginUpsertInput) (id ulid.ULID, drift *DriftReport, err error)
	ListAll(ctx context.Context) ([]PluginRow, error)
	SweepInactive(ctx context.Context, retentionDays int) ([]PluginRow, error)
}

// PostgresPluginRepo implements PluginRepo against pgxpool.Pool.
type PostgresPluginRepo struct {
	pool *pgxpool.Pool
}

func NewPostgresPluginRepo(pool *pgxpool.Pool) *PostgresPluginRepo {
	return &PostgresPluginRepo{pool: pool}
}

func (r *PostgresPluginRepo) Upsert(ctx context.Context, in PluginUpsertInput) (ulid.ULID, *DriftReport, error) {
	// Read existing row by name (active only).
	var (
		idBytes      []byte
		existingName string
		dispName     string
		version      string
		manifestHash []byte
		contentHash  []byte
		firstSeenAt  time.Time
		lastSeenAt   time.Time
		gcAt         *time.Time
	)
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, display_name, version, manifest_hash, content_hash,
		       first_seen_at, last_seen_at, gc_at
		  FROM plugins
		 WHERE name = $1 AND gc_at IS NULL
	`, in.Name).Scan(&idBytes, &existingName, &dispName, &version,
		&manifestHash, &contentHash, &firstSeenAt, &lastSeenAt, &gcAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// INSERT path
		newID := idgen.New()
		_, err := r.pool.Exec(ctx, `
			INSERT INTO plugins (id, name, display_name, version,
			                    manifest_hash, content_hash)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, newID[:], in.Name, in.DisplayName, in.Version, in.ManifestHash, in.ContentHash)
		if err != nil {
			return ulid.ULID{}, nil, oops.Code("PLUGIN_REPO_INSERT").
				With("name", in.Name).Wrap(err)
		}
		return newID, nil, nil
	case err != nil:
		return ulid.ULID{}, nil, oops.Code("PLUGIN_REPO_SELECT").
			With("name", in.Name).Wrap(err)
	}

	// UPDATE path
	var existingID ulid.ULID
	copy(existingID[:], idBytes)

	manifestChanged := !bytes.Equal(manifestHash, in.ManifestHash)
	contentChanged := !bytes.Equal(contentHash, in.ContentHash)
	versionChanged := version != in.Version

	var drift *DriftReport
	if manifestChanged || contentChanged || versionChanged {
		drift = &DriftReport{
			OldManifestHash: manifestHash,
			NewManifestHash: in.ManifestHash,
			OldContentHash:  contentHash,
			NewContentHash:  in.ContentHash,
			VersionBefore:   version,
			VersionAfter:    in.Version,
		}
	}

	_, err = r.pool.Exec(ctx, `
		UPDATE plugins
		   SET manifest_hash = $1, content_hash = $2, version = $3,
		       last_seen_at = now()
		 WHERE id = $4
	`, in.ManifestHash, in.ContentHash, in.Version, existingID[:])
	if err != nil {
		return ulid.ULID{}, nil, oops.Code("PLUGIN_REPO_UPDATE").
			With("name", in.Name).Wrap(err)
	}
	return existingID, drift, nil
}

func (r *PostgresPluginRepo) ListAll(ctx context.Context) ([]PluginRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, display_name, version, manifest_hash, content_hash,
		       first_seen_at, last_seen_at, gc_at
		  FROM plugins
	`)
	if err != nil {
		return nil, oops.Code("PLUGIN_REPO_LIST_ALL").Wrap(err)
	}
	defer rows.Close()

	var out []PluginRow
	for rows.Next() {
		var p PluginRow
		var idBytes []byte
		if err := rows.Scan(&idBytes, &p.Name, &p.DisplayName, &p.Version,
			&p.ManifestHash, &p.ContentHash,
			&p.FirstSeenAt, &p.LastSeenAt, &p.GcAt); err != nil {
			return nil, oops.Code("PLUGIN_REPO_LIST_ALL_SCAN").Wrap(err)
		}
		copy(p.ID[:], idBytes)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *PostgresPluginRepo) SweepInactive(ctx context.Context, retentionDays int) ([]PluginRow, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE plugins
		   SET gc_at = now()
		 WHERE gc_at IS NULL
		   AND last_seen_at < now() - make_interval(days => $1)
		 RETURNING id, name, display_name, version, manifest_hash,
		           content_hash, first_seen_at, last_seen_at, gc_at
	`, retentionDays)
	if err != nil {
		return nil, oops.Code("PLUGIN_REPO_SWEEP").
			With("retention_days", retentionDays).Wrap(err)
	}
	defer rows.Close()

	var out []PluginRow
	for rows.Next() {
		var p PluginRow
		var idBytes []byte
		if err := rows.Scan(&idBytes, &p.Name, &p.DisplayName, &p.Version,
			&p.ManifestHash, &p.ContentHash,
			&p.FirstSeenAt, &p.LastSeenAt, &p.GcAt); err != nil {
			return nil, oops.Code("PLUGIN_REPO_SWEEP_SCAN").Wrap(err)
		}
		copy(p.ID[:], idBytes)
		out = append(out, p)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run to verify pass**

```bash
task test:int -- -run TestPluginRepo ./internal/store/...
```

Expected: PASS.

- [ ] **Step 5: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(store): PluginRepo + PostgresPluginRepo (holomush-w9ml T3)"
jj new -m "T4 (in progress)"
bd update <T3-bead-id> --status=closed
```

---

### Task 4: `IdentityRegistry` interface

**Files:**

- Create: `internal/plugin/identity_registry.go`
- Create: `internal/plugin/identity_registry_test.go`

**Bead:** `bd create --title "w9ml T4: IdentityRegistry interface" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Write failing test**

Create `internal/plugin/identity_registry_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/oklog/ulid/v2"
)

// stubIdentityRegistry verifies that the interface can be satisfied
// independently of *Manager (the *Manager conformance is added in T5).
type stubIdentityRegistry struct{}

func (stubIdentityRegistry) NameByID(ulid.ULID) (string, bool) { return "", false }
func (stubIdentityRegistry) IDByName(string) (ulid.ULID, bool) { return ulid.ULID{}, false }

func TestIdentityRegistryInterfaceIsSatisfiable(t *testing.T) {
	var _ IdentityRegistry = stubIdentityRegistry{}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
task test -- -run TestIdentityRegistryInterfaceIsSatisfiable ./internal/plugin/
```

Expected: FAIL — `IdentityRegistry` undefined.

- [ ] **Step 3: Create the interface**

`internal/plugin/identity_registry.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import "github.com/oklog/ulid/v2"

// IdentityRegistry resolves between a plugin's stable ULID and its
// registered name. Both lookups are O(1) in-memory map accesses backed by
// the plugins table.
//
// Consumers (plugin emit stamp sites in internal/plugin/goplugin/host.go,
// actor display in internal/grpc/server.go::actorIDString and
// internal/grpc/query_stream_history.go::eventbusEventToEventFrame)
// depend on this interface, not on the full Manager.
//
// The ABAC engine is NOT an IdentityRegistry consumer (Subject strings
// are constructed at call sites by code that already has the plugin name).
type IdentityRegistry interface {
	// NameByID returns the name registered for the given ULID. Resolves
	// THREE populations:
	//   1. Currently-active plugins (rows with gc_at IS NULL).
	//   2. Historically-registered plugins (rows with gc_at IS NOT NULL —
	//      preserved across the registry's lifetime per INV-W9ML-9).
	//   3. Compile-time system actor sentinels registered at Manager
	//      bootstrap (e.g., SystemActorULID -> "system",
	//      WorldServiceActorULID -> "world-service"). Sentinels are NOT
	//      subject to GC sweep.
	//
	// ok=false only if the ULID has never been minted/registered.
	NameByID(id ulid.ULID) (name string, ok bool)

	// IDByName returns the ULID for the currently-active plugin with
	// the given name. Does NOT resolve to historical (deactivated) ULIDs;
	// emit stamp sites only care about live registrations. Does NOT
	// resolve system sentinel labels (system stamp sites use the
	// compile-time constants directly).
	//
	// ok=false if no currently-active plugin with that name is registered.
	IDByName(name string) (id ulid.ULID, ok bool)
}
```

- [ ] **Step 4: Run to verify pass**

```bash
task test -- -run TestIdentityRegistryInterfaceIsSatisfiable ./internal/plugin/
```

Expected: PASS.

- [ ] **Step 5: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): IdentityRegistry interface (holomush-w9ml T4)"
jj new -m "T5 (in progress)"
bd update <T4-bead-id> --status=closed
```

---

## Phase 2 — Manager integration

### Task 5: Manager cache fields + sentinels + NameByID/IDByName

**Files:**

- Modify: `internal/plugin/manager.go` (struct fields, NewManager bootstrap, lookup methods)
- Modify: `internal/plugin/identity_registry_test.go` (add Manager-implements-Registry test, sentinel resolution test)

**Bead:** `bd create --title "w9ml T5: Manager implements IdentityRegistry; sentinel bootstrap" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Write failing tests**

Append to `internal/plugin/identity_registry_test.go`:

```go
import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/store"
)

// stubPluginRepo lets us drive Manager bootstrap without Postgres.
type stubPluginRepo struct {
	rows    []store.PluginRow
	swept   []store.PluginRow
	upserts []store.PluginUpsertInput
}

func (s *stubPluginRepo) Upsert(ctx context.Context, in store.PluginUpsertInput) (ulid.ULID, *store.DriftReport, error) {
	s.upserts = append(s.upserts, in)
	for _, r := range s.rows {
		if r.Name == in.Name && r.GcAt == nil {
			return r.ID, nil, nil
		}
	}
	id := ulid.ULID{}
	copy(id[:], []byte(in.Name+"00000000000000000000")[:16])
	return id, nil, nil
}
func (s *stubPluginRepo) ListAll(ctx context.Context) ([]store.PluginRow, error) {
	return s.rows, nil
}
func (s *stubPluginRepo) SweepInactive(ctx context.Context, days int) ([]store.PluginRow, error) {
	return s.swept, nil
}

func newManagerForRegistryTest(t *testing.T, repo store.PluginRepo) *Manager {
	t.Helper()
	// NewManager enforces INV-GW-10: a VerbRegistry MUST be passed.
	// (See manager.go:57-62 / 196-198 for ErrMissingVerbRegistry.)
	mgr, err := NewManager(t.TempDir(),
		WithPluginRepo(repo),
		WithVerbRegistry(core.NewVerbRegistry()),
	)
	require.NoError(t, err)
	return mgr
}

// Compile-time conformance — added once Manager has the methods.
var _ IdentityRegistry = (*Manager)(nil)

func TestManagerNameByIDResolvesSystemSentinels(t *testing.T) {
	mgr := newManagerForRegistryTest(t, &stubPluginRepo{})

	name, ok := mgr.NameByID(core.SystemActorULID)
	require.True(t, ok)
	assert.Equal(t, "system", name)

	name, ok = mgr.NameByID(core.WorldServiceActorULID)
	require.True(t, ok)
	assert.Equal(t, "world-service", name)
}

func TestManagerIDByNameDoesNotResolveSentinelLabels(t *testing.T) {
	mgr := newManagerForRegistryTest(t, &stubPluginRepo{})

	_, ok := mgr.IDByName("system")
	assert.False(t, ok, "system label MUST NOT be resolvable via IDByName")
	_, ok = mgr.IDByName("world-service")
	assert.False(t, ok, "world-service label MUST NOT be resolvable via IDByName")
}

func TestManagerNameByIDReturnsFalseForUnregisteredULID(t *testing.T) {
	mgr := newManagerForRegistryTest(t, &stubPluginRepo{})

	random := core.NewULID()
	_, ok := mgr.NameByID(random)
	assert.False(t, ok)
}

func TestManagerBootstrapRefusesPluginRowWithSentinelULID(t *testing.T) {
	repo := &stubPluginRepo{
		rows: []store.PluginRow{{
			ID:   core.SystemActorULID,
			Name: "evil-plugin",
		}},
	}
	_, err := NewManager(t.TempDir(),
		WithPluginRepo(repo),
		WithVerbRegistry(core.NewVerbRegistry()),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PLUGIN_ROW_USES_SENTINEL_ID")
}

func TestManagerBootstrapPopulatesNameByIDFromActiveAndHistoricalRows(t *testing.T) {
	now := time.Now()
	gcAt := now.Add(-7 * 24 * time.Hour)
	// Use core.NewULID() rather than hardcoded strings — Crockford-base32
	// excludes I/O/L/U, so hand-typed ULID strings are easy to get wrong.
	activeID := core.NewULID()
	histID := core.NewULID()

	repo := &stubPluginRepo{rows: []store.PluginRow{
		{ID: activeID, Name: "active-plugin", GcAt: nil},
		{ID: histID, Name: "old-plugin", GcAt: &gcAt},
	}}
	mgr := newManagerForRegistryTest(t, repo)

	name, ok := mgr.NameByID(activeID)
	require.True(t, ok); assert.Equal(t, "active-plugin", name)

	name, ok = mgr.NameByID(histID)
	require.True(t, ok); assert.Equal(t, "old-plugin", name)

	id, ok := mgr.IDByName("active-plugin")
	require.True(t, ok); assert.Equal(t, activeID, id)

	_, ok = mgr.IDByName("old-plugin")
	assert.False(t, ok, "deactivated plugin name MUST NOT resolve via IDByName")
}

// (Note: a `mustULID(string)` helper was considered for stable test fixtures
// but rejected because Crockford-base32 ULIDs exclude I/O/L/U, making
// hand-typed strings error-prone. Tests use `core.NewULID()` for fresh
// ULIDs and `plugintest.PluginULIDFromName(name)` for deterministic-by-name
// fixtures.)
```

- [ ] **Step 2: Run to verify failure**

```bash
task test -- -run 'TestManagerNameByID|TestManagerIDByName|TestManagerBootstrap|TestIdentityRegistry' ./internal/plugin/
```

Expected: FAIL — `Manager.NameByID`, `Manager.IDByName`, `WithPluginRepo` undefined.

- [ ] **Step 3: Modify `internal/plugin/manager.go`**

Add to imports if not present:

```go
"github.com/oklog/ulid/v2"
"github.com/holomush/holomush/internal/store"
```

Add to the `Manager` struct (around line 65-85, after existing fields):

```go
type Manager struct {
	// ... existing fields ...

	// Identity registry: name ↔ ULID maps populated at bootstrap from the
	// plugins table; mutated on load/unload. nameByID resolves three
	// populations (active plugins + historical plugins + system sentinels);
	// activeByName resolves only currently-loaded plugins. Both are
	// guarded by the existing m.mu RWMutex.
	pluginRepo   store.PluginRepo
	nameByID     map[ulid.ULID]string
	activeByName map[string]ulid.ULID
}
```

Add `WithPluginRepo`:

```go
// WithPluginRepo wires the IdentityRegistry's persistence layer.
// Required when the Manager will Upsert plugin rows. Without it,
// loadPlugin operates with an in-memory-only registry (test seam).
func WithPluginRepo(repo store.PluginRepo) ManagerOption {
	return func(m *Manager) { m.pluginRepo = repo }
}
```

Modify `NewManager` to initialize the cache and register sentinels. Locate the existing `NewManager` (around line 181) and insert after the `for _, opt := range opts { opt(m) }` line:

```go
// Initialize the identity registry cache.
m.nameByID = make(map[ulid.ULID]string)
m.activeByName = make(map[string]ulid.ULID)

// Step 1: register system sentinels first (not in activeByName, not
// in the plugins table — different identity domain).
m.nameByID[core.SystemActorULID] = "system"
m.nameByID[core.WorldServiceActorULID] = "world-service"

// Step 2: load existing plugin rows from persistence. Reject sentinel
// collisions defensively.
if m.pluginRepo != nil {
	ctx := context.Background()
	rows, err := m.pluginRepo.ListAll(ctx)
	if err != nil {
		return nil, oops.Code("PLUGIN_MANAGER_BOOTSTRAP").Wrap(err)
	}
	for _, row := range rows {
		if core.IsSentinelULID(row.ID) {
			return nil, oops.Code("PLUGIN_ROW_USES_SENTINEL_ID").
				With("name", row.Name).
				With("id", row.ID.String()).
				Errorf("plugin row uses a reserved sentinel ULID")
		}
		m.nameByID[row.ID] = row.Name
		if row.GcAt == nil {
			m.activeByName[row.Name] = row.ID
		}
	}
}
```

Add the lookup methods at the bottom of `manager.go`:

```go
// NameByID implements IdentityRegistry.
func (m *Manager) NameByID(id ulid.ULID) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	name, ok := m.nameByID[id]
	return name, ok
}

// IDByName implements IdentityRegistry.
func (m *Manager) IDByName(name string) (ulid.ULID, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.activeByName[name]
	return id, ok
}
```

- [ ] **Step 4: Run to verify pass**

```bash
task test -- -run 'TestManagerNameByID|TestManagerIDByName|TestManagerBootstrap|TestIdentityRegistry' ./internal/plugin/
```

Expected: PASS.

```bash
task test -- ./internal/plugin/
```

Expected: existing manager tests still pass (they don't pass `WithPluginRepo`, so persistence is skipped; sentinels are still bootstrapped).

- [ ] **Step 5: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): Manager implements IdentityRegistry; sentinel bootstrap (holomush-w9ml T5)"
jj new -m "T6 (in progress)"
bd update <T5-bead-id> --status=closed
```

---

### Task 6: Manager hash compute + loadPlugin Upsert + drift logging

**Files:**

- Modify: `internal/plugin/manager.go` (`computeHashes`, integrate Upsert into `loadPlugin`)
- Append: `internal/plugin/identity_registry_test.go` (load+Upsert behavior)

**Bead:** `bd create --title "w9ml T6: Manager hash compute + loadPlugin Upsert + drift logging" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Write failing tests**

Append to `internal/plugin/identity_registry_test.go`:

```go
func TestComputeHashesProducesNonEmptyForBinary(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte("name: x\nversion: 1\ntype: binary\nbinary-plugin:\n  executable: bin/x\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bin"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bin/x"), []byte("ELF-binary-bytes"), 0755))

	mgr := newManagerForRegistryTest(t, &stubPluginRepo{})
	dp := &DiscoveredPlugin{
		Manifest: &Manifest{Name: "x", Version: "1", Type: TypeBinary, BinaryPlugin: &BinaryConfig{Executable: "bin/x"}},
		Dir:      dir,
	}
	mh, ch, err := mgr.computeHashes(dp)
	require.NoError(t, err)
	assert.Len(t, mh, 32, "manifest hash must be sha256 (32 bytes)")
	assert.Len(t, ch, 32, "binary content hash must be sha256")
}

func TestComputeHashesNilContentForSettingPlugin(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte("name: x\nversion: 1\ntype: setting\n"), 0644))

	mgr := newManagerForRegistryTest(t, &stubPluginRepo{})
	dp := &DiscoveredPlugin{Manifest: &Manifest{Name: "x", Version: "1", Type: TypeSetting}, Dir: dir}
	_, ch, err := mgr.computeHashes(dp)
	require.NoError(t, err)
	assert.Nil(t, ch, "setting plugins MUST have nil content_hash")
}

func TestComputeHashesLuaIsDeterministicAndOrderIndependent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte("name: x\nversion: 1\ntype: lua\nlua-plugin:\n  entry: a.lua\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.lua"), []byte("foo"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.lua"), []byte("bar"), 0644))

	mgr := newManagerForRegistryTest(t, &stubPluginRepo{})
	dp := &DiscoveredPlugin{Manifest: &Manifest{Name: "x", Version: "1", Type: TypeLua, LuaPlugin: &LuaConfig{Entry: "a.lua"}}, Dir: dir}

	_, ch1, err := mgr.computeHashes(dp)
	require.NoError(t, err)
	_, ch2, err := mgr.computeHashes(dp)
	require.NoError(t, err)
	assert.Equal(t, ch1, ch2, "Lua content_hash MUST be deterministic")
}
```

(Note: `loadPlugin` is internal; the broader behavior — Upsert + cache + drift log + rollback — is tested in T7's integration tests once `UnloadPlugin` exists.)

- [ ] **Step 2: Run to verify failure**

```bash
task test -- -run 'TestComputeHashes' ./internal/plugin/
```

Expected: FAIL — `mgr.computeHashes` undefined.

- [ ] **Step 3: Add `computeHashes` to `internal/plugin/manager.go`**

Add imports if needed:

```go
"crypto/sha256"
"os"
"path/filepath"
"sort"
```

Add the function:

```go
// computeHashes returns sha256 of the plugin's manifest.yaml bytes (always)
// and its executable artifact:
//   - TypeBinary: sha256 of the executable file at BinaryPlugin.Executable.
//   - TypeLua:    sha256 of deterministic concatenation of *.lua files
//                 (sorted by relative path within Dir; rel-path NUL contents
//                 NUL between files).
//   - TypeSetting: nil (no executable artifact).
func (m *Manager) computeHashes(dp *DiscoveredPlugin) (manifestHash, contentHash []byte, err error) {
	mfBytes, err := os.ReadFile(filepath.Join(dp.Dir, "plugin.yaml"))
	if err != nil {
		return nil, nil, oops.Code("PLUGIN_HASH_MANIFEST_READ").
			With("plugin", dp.Manifest.Name).Wrap(err)
	}
	mh := sha256.Sum256(mfBytes)
	manifestHash = mh[:]

	switch dp.Manifest.Type {
	case TypeBinary:
		if dp.Manifest.BinaryPlugin == nil || dp.Manifest.BinaryPlugin.Executable == "" {
			return nil, nil, oops.Code("PLUGIN_HASH_BINARY_MISSING_EXECUTABLE").
				With("plugin", dp.Manifest.Name).Errorf("binary plugin must declare binary-plugin.executable")
		}
		bin, err := os.ReadFile(filepath.Join(dp.Dir, dp.Manifest.BinaryPlugin.Executable))
		if err != nil {
			return nil, nil, oops.Code("PLUGIN_HASH_BINARY_READ").
				With("plugin", dp.Manifest.Name).Wrap(err)
		}
		ch := sha256.Sum256(bin)
		contentHash = ch[:]
	case TypeLua:
		var luaFiles []string
		walkErr := filepath.Walk(dp.Dir, func(p string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !info.IsDir() && filepath.Ext(p) == ".lua" {
				rel, err := filepath.Rel(dp.Dir, p)
				if err != nil {
					return err
				}
				luaFiles = append(luaFiles, rel)
			}
			return nil
		})
		if walkErr != nil {
			return nil, nil, oops.Code("PLUGIN_HASH_LUA_WALK").
				With("plugin", dp.Manifest.Name).Wrap(walkErr)
		}
		sort.Strings(luaFiles)
		h := sha256.New()
		for _, rel := range luaFiles {
			b, err := os.ReadFile(filepath.Join(dp.Dir, rel))
			if err != nil {
				return nil, nil, oops.Code("PLUGIN_HASH_LUA_READ").
					With("plugin", dp.Manifest.Name).With("file", rel).Wrap(err)
			}
			h.Write([]byte(rel))
			h.Write([]byte{0x00})
			h.Write(b)
			h.Write([]byte{0x00})
		}
		contentHash = h.Sum(nil)
	case TypeSetting:
		contentHash = nil
	default:
		return nil, nil, oops.Code("PLUGIN_HASH_UNKNOWN_TYPE").
			With("plugin", dp.Manifest.Name).
			With("type", string(dp.Manifest.Type)).
			Errorf("unknown plugin type")
	}
	return manifestHash, contentHash, nil
}
```

- [ ] **Step 4: Integrate Upsert + cache into existing `loadPlugin`**

In `manager.go`, locate the existing `loadPlugin` function (around line 730+, immediately after the duplicate-check block at lines 730-749). Insert before `host.Load(...)` at line 752:

```go
// w9ml T6: compute hashes, Upsert into plugins table, populate cache.
manifestHash, contentHash, hashErr := m.computeHashes(dp)
if hashErr != nil {
	return hashErr
}

var pluginID ulid.ULID
var drift *store.DriftReport
if m.pluginRepo != nil {
	// Use `:=` for the Upsert return — `loadPlugin` does not have an `err`
	// in scope (signature is `func (m *Manager) loadPlugin(...) error`,
	// not a named return). Declare a fresh upsertErr; if non-nil, return.
	id, d, upsertErr := m.pluginRepo.Upsert(ctx, store.PluginUpsertInput{
		Name:         dp.Manifest.Name,
		DisplayName:  dp.Manifest.Name, // TODO: extract DisplayName from manifest if it has one
		Version:      dp.Manifest.Version,
		ManifestHash: manifestHash,
		ContentHash:  contentHash,
	})
	if upsertErr != nil {
		return oops.In("manager").With("plugin", dp.Manifest.Name).Wrap(upsertErr)
	}
	pluginID, drift = id, d
} else {
	pluginID = idgen.New()
}

// Cache mutation BEFORE host.Load (downstream code may emit during Load).
m.mu.Lock()
m.nameByID[pluginID] = dp.Manifest.Name
m.activeByName[dp.Manifest.Name] = pluginID
m.mu.Unlock()

// Roll back the cache mutation if any subsequent step fails. Note:
// loadPlugin (manager.go:673) returns a bare `error`, not a named return,
// so we cannot use `defer func() { if err != nil ... }()` — the closure
// would capture the wrong `err` after the next `if err != nil { return }`
// shadows it. Instead use an explicit rollback flag set by every
// downstream error path before its return.
var loadPluginCommitted bool
defer func() {
	if !loadPluginCommitted {
		m.mu.Lock()
		delete(m.nameByID, pluginID)
		delete(m.activeByName, dp.Manifest.Name)
		m.mu.Unlock()
	}
}()

// Drift logging (no decision logic — log and continue).
if drift != nil {
	slog.Info("plugin.drift",
		"name", dp.Manifest.Name,
		"old_manifest_hash", hex.EncodeToString(drift.OldManifestHash),
		"new_manifest_hash", hex.EncodeToString(drift.NewManifestHash),
		"old_content_hash", hex.EncodeToString(drift.OldContentHash),
		"new_content_hash", hex.EncodeToString(drift.NewContentHash),
		"version_before", drift.VersionBefore,
		"version_after", drift.VersionAfter,
	)
}
```

(Add imports if needed: `"encoding/hex"`, `"github.com/holomush/holomush/internal/idgen"`.)

- [ ] **Step 4b: Set the rollback-flag commit at the end of `loadPlugin`**

The deferred rollback above runs whenever `loadPluginCommitted == false`. It MUST be set true on the success path so the cache mutation is preserved. Find the FINAL `return nil` at the bottom of `loadPlugin` (around line 950+ in the current file — verify with `rg -n 'return nil' internal/plugin/manager.go | tail -5` and pick the one inside `loadPlugin`). Insert immediately before it:

```go
// w9ml T6: rollback flag commit — see the deferred rollback at the top
// of loadPlugin. Setting this true on the success path makes the deferred
// cache cleanup a no-op.
loadPluginCommitted = true
return nil
```

If `loadPlugin` has multiple success-return paths (early returns for valid no-op cases), each MUST set the flag before its `return nil`. Verify by walking every `return nil` between the cache-mutation point (Step 4 above) and the function end. Currently `loadPlugin` returns nil at exactly one place (the end of the function); double-check.

Failure mode without this step: `loadPlugin` succeeds, the defer fires after return, sees `loadPluginCommitted == false`, and DELETES the cache entry that was just successfully populated. Subsequent emit calls then hit `PLUGIN_UNREGISTERED_INVOKE` for every loaded plugin — silent cascade failure.

- [ ] **Step 5: Run to verify pass**

```bash
task test -- -run 'TestComputeHashes' ./internal/plugin/
```

Expected: PASS.

```bash
task test -- ./internal/plugin/
```

Expected: existing tests still pass.

- [ ] **Step 6: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): Manager hash compute + loadPlugin Upsert + drift logging (holomush-w9ml T6)"
jj new -m "T7 (in progress)"
bd update <T6-bead-id> --status=closed
```

---

### Task 7: Add `Manager.UnloadPlugin` + cache mutation

**Files:**

- Create: `internal/plugin/manager_unload.go`
- Append: `internal/plugin/identity_registry_test.go`

**Bead:** `bd create --title "w9ml T7: Manager.UnloadPlugin (new method) + cache mutation" --parent=holomush-w9ml --type=task --priority=1`

**Why this exists:** Reviewer Finding 3 — the original plan assumed `Manager.UnloadPlugin` existed; it does not. The Manager only has inline `host.Unload(...)` calls within rollback paths. This task introduces `UnloadPlugin` as a new exported method that performs orderly unload + cache mutation.

- [ ] **Step 1: Write failing tests**

Append to `internal/plugin/identity_registry_test.go`:

```go
func TestUnloadPluginRemovesActiveButPreservesHistorical(t *testing.T) {
	repo := &stubPluginRepo{}
	mgr := newManagerForRegistryTest(t, repo)

	// Load a plugin via the existing path. Use TestLoadPlugin (existing
	// test seam at manager.go:978) which directly inserts a discovered
	// plugin into m.loaded.
	manifest := &Manifest{Name: "core-scenes", Version: "1.0.0", Type: TypeLua, LuaPlugin: &LuaConfig{Entry: "main.lua"}}
	mgr.TestLoadPlugin("core-scenes", manifest)

	// Manually populate cache (in real loadPlugin path this is done by T6).
	id := core.NewULID()
	mgr.mu.Lock()
	mgr.nameByID[id] = "core-scenes"
	mgr.activeByName["core-scenes"] = id
	mgr.mu.Unlock()

	require.NoError(t, mgr.UnloadPlugin(context.Background(), "core-scenes"))

	_, stillActive := mgr.IDByName("core-scenes")
	assert.False(t, stillActive)

	name, ok := mgr.NameByID(id)
	require.True(t, ok)
	assert.Equal(t, "core-scenes", name, "historical resolution preserved")
}

func TestUnloadPluginIsIdempotentWhenNotLoaded(t *testing.T) {
	mgr := newManagerForRegistryTest(t, &stubPluginRepo{})
	// UnloadPlugin on a never-loaded name MUST NOT error.
	err := mgr.UnloadPlugin(context.Background(), "nonexistent")
	assert.NoError(t, err)
}
```

- [ ] **Step 2: Run to verify failure**

```bash
task test -- -run 'TestUnloadPlugin' ./internal/plugin/
```

Expected: FAIL — `Manager.UnloadPlugin` undefined.

- [ ] **Step 3: Implement `internal/plugin/manager_unload.go`**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"

	"github.com/samber/oops"
)

// UnloadPlugin orderly-unloads a plugin: invokes host.Unload, removes
// installed ABAC policies via PluginPolicyInstaller, and clears the
// plugin from activeByName (the nameByID entry is intentionally retained
// to preserve historical resolution for events emitted before unload).
//
// Idempotent: cache cleanup runs FIRST and unconditionally, so calling
// UnloadPlugin on a name with no host (e.g., a registry-only test
// fixture or after a load-failure rollback) still removes the cache
// entry. Host unload + policy removal then run only if a host is
// actually registered for the name.
func (m *Manager) UnloadPlugin(ctx context.Context, name string) error {
	// 1. Cache cleanup FIRST and unconditionally. This is decoupled from
	// host lifecycle: a plugin's cache entry can exist without a host
	// (test seam, load-failure rollback, sweep aftermath). Idempotent
	// removal is safe even if the entry is already absent.
	m.mu.Lock()
	delete(m.activeByName, name)
	// nameByID intentionally retained for historical resolution.
	host, hostLoaded := m.pluginHosts[name]
	if hostLoaded {
		delete(m.loaded, name)
		delete(m.pluginHosts, name)
	}
	m.mu.Unlock()

	if !hostLoaded {
		return nil // idempotent — no host to unload
	}

	// 2. Unload from the host.
	if err := host.Unload(ctx, name); err != nil {
		return oops.Code("PLUGIN_UNLOAD_HOST").
			With("plugin", name).Wrap(err)
	}

	// 3. Remove plugin policies.
	if m.policyInstaller != nil {
		if err := m.policyInstaller.RemovePluginPolicies(ctx, name); err != nil {
			return oops.Code("PLUGIN_UNLOAD_POLICIES").
				With("plugin", name).Wrap(err)
		}
	}

	return nil
}
```

- [ ] **Step 4: Run to verify pass**

```bash
task test -- -run 'TestUnloadPlugin' ./internal/plugin/
```

Expected: PASS.

- [ ] **Step 5: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): Manager.UnloadPlugin + cache mutation (holomush-w9ml T7)"
jj new -m "T8 (in progress)"
bd update <T7-bead-id> --status=closed
```

---

### Task 8: GC sweep at end of `LoadAll` + `RetentionDays` config

**Files:**

- Modify: `internal/plugin/manager.go` (sweep at end of `LoadAll`)
- Modify: `internal/plugin/manager.go` add `WithRetentionDays` option + `retentionDays int` field
- Append: `internal/plugin/identity_registry_test.go` (sweep tests)

**Bead:** `bd create --title "w9ml T8: GC sweep at LoadAll end + RetentionDays config" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Write failing tests**

Append to `internal/plugin/identity_registry_test.go`:

```go
func TestSweepInactiveRemovesFromActiveByNameRetainsNameByID(t *testing.T) {
	staleID := core.NewULID()
	now := time.Now()
	repo := &stubPluginRepo{
		swept: []store.PluginRow{
			{ID: staleID, Name: "stale", LastSeenAt: now.Add(-99 * 24 * time.Hour)},
		},
	}
	mgr, err := NewManager(t.TempDir(),
		WithPluginRepo(repo),
		WithVerbRegistry(core.NewVerbRegistry()),
		WithRetentionDays(3),
	)
	require.NoError(t, err)

	// Pre-populate cache as if "stale" had been loaded previously.
	mgr.mu.Lock()
	mgr.nameByID[staleID] = "stale"
	mgr.activeByName["stale"] = staleID
	mgr.mu.Unlock()

	require.NoError(t, mgr.LoadAll(context.Background()))

	_, ok := mgr.IDByName("stale")
	assert.False(t, ok, "swept plugin MUST NOT be in activeByName")

	name, ok := mgr.NameByID(staleID)
	require.True(t, ok)
	assert.Equal(t, "stale", name, "swept plugin's NameByID retention preserved")
}

func TestRetentionDaysZeroDisablesSweep(t *testing.T) {
	repo := &stubPluginRepo{
		swept: []store.PluginRow{ /* normally would sweep */ },
	}
	mgr, err := NewManager(t.TempDir(),
		WithPluginRepo(repo),
		WithVerbRegistry(core.NewVerbRegistry()),
		WithRetentionDays(0),
	)
	require.NoError(t, err)
	require.NoError(t, mgr.LoadAll(context.Background()))
	// stub's swept rows MUST NOT be passed to mgr because LoadAll
	// shouldn't call SweepInactive when retention=0. We can't observe
	// non-call directly; instead assert no panic and no log.
}
```

- [ ] **Step 2: Run to verify failure**

```bash
task test -- -run 'TestSweepInactive|TestRetentionDays' ./internal/plugin/
```

Expected: FAIL — `WithRetentionDays` undefined; `LoadAll` doesn't call `SweepInactive`.

- [ ] **Step 3: Add config field + option**

In `internal/plugin/manager.go`, add to `Manager` struct:

```go
retentionDays int // Plugin row retention; 0 = disable sweep
```

Add the option:

```go
// WithRetentionDays configures plugin row TTL (days). After RetentionDays
// of inactivity, a plugin row is deactivated (gc_at set) at the end of
// LoadAll. 0 disables the sweep entirely. Default: 3.
func WithRetentionDays(days int) ManagerOption {
	return func(m *Manager) { m.retentionDays = days }
}
```

In `NewManager`, default `retentionDays` to 3 if not set by an option:

```go
if m.retentionDays == 0 && !explicitlySetByOption /* see note */ {
	m.retentionDays = 3
}
```

(Implementation note: distinguishing "not set" from "explicitly 0" is done via an unexported sentinel-bool, OR by treating 0 as "disabled" as the spec specifies — re-read spec line 297-300. Spec says default 3, 0 = disabled, so we need a way to distinguish. Simplest: keep retentionDays=0 in struct; use 3 as default in `NewManager` ONLY if WithRetentionDays wasn't called. Use an option-ordering bool `m.retentionDaysSet`.)

Cleaner approach — make zero-value mean default, use a sentinel for disabled:

```go
// WithRetentionDays sets retention. days<0 means disabled.
func WithRetentionDays(days int) ManagerOption { ... }
```

Then 0 = use default (3); -1 = disabled. Spec says 0 = disabled — so opt for the option-set bool. The implementer should pick the cleaner pattern at impl time and document it.

- [ ] **Step 4: Modify `LoadAll`**

Locate `Manager.LoadAll` in `manager.go`. After the per-plugin load loop, add:

```go
// w9ml T8: GC sweep — runs AFTER all loads have refreshed last_seen_at,
// so a plugin loaded in this cycle is never swept in the same cycle
// (INV-W9ML-8).
if m.pluginRepo != nil && m.retentionDays > 0 {
	swept, err := m.pluginRepo.SweepInactive(ctx, m.retentionDays)
	if err != nil {
		return oops.Code("PLUGIN_MANAGER_SWEEP").Wrap(err)
	}
	for _, row := range swept {
		m.mu.Lock()
		delete(m.activeByName, row.Name)
		// nameByID intentionally retained
		m.mu.Unlock()
		slog.Info("plugin.gc",
			"name", row.Name,
			"id", row.ID.String(),
			"last_seen_at", row.LastSeenAt.Format(time.RFC3339),
		)
	}
}
```

- [ ] **Step 5: Run to verify pass**

```bash
task test -- -run 'TestSweepInactive|TestRetentionDays' ./internal/plugin/
```

Expected: PASS.

- [ ] **Step 6: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): GC sweep at LoadAll end + RetentionDays config (holomush-w9ml T8)"
jj new -m "T9 (in progress)"
bd update <T8-bead-id> --status=closed
```

---

## Phase 3 — Stamp sites

### Task 9: Plugin emit stamp sites — IDByName at `goplugin/host.go`

**Files:**

- Modify: `internal/plugin/goplugin/host.go` (4 stamp sites at lines 560, 566, 631, 637; inject `IdentityRegistry`)
- Add tests: `internal/plugin/goplugin/host_actor_stamp_test.go`

**Bead:** `bd create --title "w9ml T9: 4 plugin stamp sites resolve via IDByName" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Read `host.go` lines 540-645 in full**

```bash
sed -n '540,645p' internal/plugin/goplugin/host.go
```

Identify the four stamp sites and the `Host` struct. The plan assumes the `Host` struct has fields like `pluginName`, but verify the exact shape.

- [ ] **Step 2: Write failing tests**

Create `internal/plugin/goplugin/host_actor_stamp_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
)

type stubReg struct{ idsByName map[string]ulid.ULID }

func (s *stubReg) NameByID(ulid.ULID) (string, bool) { return "", false }
func (s *stubReg) IDByName(name string) (ulid.ULID, bool) {
	id, ok := s.idsByName[name]
	return id, ok
}

func TestStampPluginActorSucceedsForRegisteredPlugin(t *testing.T) {
	expected := core.NewULID()
	reg := &stubReg{idsByName: map[string]ulid.ULID{"core-scenes": expected}}

	got, err := stampPluginActor(reg, "core-scenes")
	require.NoError(t, err)
	assert.Equal(t, core.ActorPlugin, got.Kind)
	assert.Equal(t, expected.String(), got.ID)
}

func TestStampPluginActorFailsForUnregistered(t *testing.T) {
	reg := &stubReg{idsByName: nil}
	_, err := stampPluginActor(reg, "unknown")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PLUGIN_UNREGISTERED_INVOKE")
}
```

- [ ] **Step 3: Run to verify failure**

```bash
task test -- -run 'TestStampPluginActor' ./internal/plugin/goplugin/
```

Expected: FAIL — `stampPluginActor` undefined.

- [ ] **Step 4: Add the helper + inject `IdentityRegistry`**

In `internal/plugin/goplugin/host.go`, add a package-private helper near the top of the file (after imports):

```go
// stampPluginActor resolves a plugin name to a core.Actor with a
// ULID-string ID via the IdentityRegistry. Returns PLUGIN_UNREGISTERED_INVOKE
// if the plugin is not active in the registry.
func stampPluginActor(reg plugins.IdentityRegistry, name string) (core.Actor, error) {
	id, ok := reg.IDByName(name)
	if !ok {
		return core.Actor{}, oops.Code("PLUGIN_UNREGISTERED_INVOKE").
			With("plugin", name).
			Errorf("plugin not registered in IdentityRegistry")
	}
	return core.Actor{Kind: core.ActorPlugin, ID: id.String()}, nil
}
```

(Required imports: `"github.com/holomush/holomush/internal/core"`, `"github.com/holomush/holomush/internal/plugin"` aliased as `plugins`, `"github.com/samber/oops"`.)

- [ ] **Step 5: Inject `IdentityRegistry` into `Host`**

In `host.go`, locate the `Host` struct definition. Add field:

```go
identityRegistry plugins.IdentityRegistry
```

Update the `Host` constructor (or wherever the struct is built) to accept the registry. If construction happens in `internal/plugin/manager.go` via `RegisterHost`, plumb the manager's `IdentityRegistry` (the manager itself) in.

- [ ] **Step 6: Replace the four stamp sites**

For each of lines 560, 566, 631, 637 in `host.go`:

Today:

```go
storedActor := core.Actor{Kind: core.ActorPlugin, ID: name}
```

Post-epic:

```go
storedActor, err := stampPluginActor(h.identityRegistry, name)
if err != nil {
	return err  // or whatever the surrounding function's error path is
}
```

(The error handling needs to fit the surrounding function's signature — the implementer reads the function context at impl time.)

- [ ] **Step 7: Run to verify pass**

```bash
task test -- -run 'TestStampPluginActor' ./internal/plugin/goplugin/
```

Expected: PASS.

```bash
task test -- ./internal/plugin/goplugin/
```

Expected: existing tests may need updating to inject a stub `IdentityRegistry`. Update inline (typically a one-line change per existing test).

- [ ] **Step 8: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin/goplugin): stamp sites resolve via IdentityRegistry.IDByName (holomush-w9ml T9)"
jj new -m "T10 (in progress)"
bd update <T9-bead-id> --status=closed
```

---

### Task 10: System actor stamp site migrations (4 sites)

**Files:**

- Modify: `internal/world/event_store_adapter.go:34-37`
- Modify: `internal/grpc/server.go:531-534`
- Modify: `internal/command/types.go:619-622`
- Modify: `internal/core/engine_end_session.go:56` (no source change — `ActorSystemID` value changed)

**Bead:** `bd create --title "w9ml T10: system stamp sites use sentinel ULIDs (4 sites)" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Write per-file forbidden-string assertions**

Create `internal/core/no_string_system_stamps_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core_test

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Per-file checks. Multi-line struct literals would defeat a single-line
// regex, so we check for the literal string in each file individually.
func TestEventStoreAdapterDoesNotUseStringWorldServiceLabel(t *testing.T) {
	out, _ := exec.Command("grep", "-c", `"world-service"`,
		"internal/world/event_store_adapter.go").CombinedOutput()
	assert.Equal(t, "0\n", string(out))
}

func TestGrpcServerDoesNotUseStringSystemLabel(t *testing.T) {
	// grpc/server.go has many uses of the word "system" in comments;
	// scope to actor-stamp lines only.
	out, _ := exec.Command("rg", "-n", `Kind:.*ActorSystem.*ID:.*"system"`,
		"internal/grpc/server.go").CombinedOutput()
	assert.Empty(t, string(out))
}

func TestCommandTypesDoesNotUseStringSystemLabel(t *testing.T) {
	out, _ := exec.Command("rg", "-n", `Kind:.*ActorSystem.*ID:.*"system"`,
		"internal/command/types.go").CombinedOutput()
	assert.Empty(t, string(out))
}
```

- [ ] **Step 2: Run to verify failure**

```bash
task test -- -run 'TestEventStoreAdapter|TestGrpcServer|TestCommandTypes' ./internal/core/
```

Expected: FAIL — string literals still present at the listed sites.

- [ ] **Step 3: Migrate each stamp site**

`internal/world/event_store_adapter.go` lines 34-37:

- Change `core.Actor{Kind: core.ActorSystem, ID: "world-service"}` to `core.Actor{Kind: core.ActorSystem, ID: core.WorldServiceActorULID.String()}`.

`internal/grpc/server.go` lines 531-534:

- Change `core.Actor{Kind: core.ActorSystem, ID: "system"}` to `core.Actor{Kind: core.ActorSystem, ID: core.ActorSystemID}` (which now equals `SystemActorULID.String()` per T2).

`internal/command/types.go` lines 619-622:

- Same pattern as `grpc/server.go`.

`internal/core/engine_end_session.go` line 56:

- No source change. The line `actor := Actor{Kind: ActorSystem, ID: ActorSystemID}` is unchanged; the value of `ActorSystemID` changed in T2 from `"system"` to `SystemActorULID.String()`.

- [ ] **Step 4: Run to verify pass**

```bash
task test -- -run 'TestEventStoreAdapter|TestGrpcServer|TestCommandTypes' ./internal/core/
task test -- ./internal/world/ ./internal/grpc/ ./internal/command/ ./internal/core/
```

Expected: PASS. Note: `internal/core/engine_end_session_test.go:78` already compares `events[0].Actor.ID` against `ActorSystemID` — the test passes because the constant changed value but tests still pass (rvalue comparison).

- [ ] **Step 5: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(core,world,grpc,command): system stamps use sentinel ULIDs (holomush-w9ml T10)"
jj new -m "T11 (in progress)"
bd update <T10-bead-id> --status=closed
```

---

## Phase 4 — Wire-format change

### Task 11: Proto schema removal + .pb.go regen

**Files:**

- Modify: `api/proto/holomush/eventbus/v1/eventbus.proto` (lines 23-30)
- Regenerated: `pkg/proto/holomush/eventbus/v1/eventbus.pb.go`

**Bead:** `bd create --title "w9ml T11: remove Actor.legacy_id from proto + regen .pb.go" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Write failing assertions**

Create `internal/eventbus/proto_legacy_id_grep_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestProtoHasNoLegacyIDField(t *testing.T) {
	out, _ := exec.Command("grep", "-n", "legacy_id",
		"api/proto/holomush/eventbus/v1/eventbus.proto").CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("legacy_id MUST NOT exist in eventbus.proto:\n%s", out)
	}
}

func TestRegeneratedPbGoHasNoLegacyId(t *testing.T) {
	out, _ := exec.Command("grep", "-n", "LegacyId",
		"pkg/proto/holomush/eventbus/v1/eventbus.pb.go").CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("LegacyId MUST NOT exist in regenerated eventbus.pb.go:\n%s", out)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
task test -- -run 'TestProtoHasNoLegacyIDField|TestRegeneratedPbGoHasNoLegacyId' ./internal/eventbus/
```

Expected: FAIL — both files contain LegacyID.

- [ ] **Step 3: Modify the proto**

`api/proto/holomush/eventbus/v1/eventbus.proto`, replace lines 23-30:

```proto
// Actor identifies who caused an event.
message Actor {
  ActorKind kind = 1;
  bytes id = 2; // ULID (16 bytes); MUST be set for every ActorKind value.
}
```

- [ ] **Step 4: Regenerate**

```bash
task proto
```

Expected: `pkg/proto/holomush/eventbus/v1/eventbus.pb.go` regenerates without `LegacyId`.

- [ ] **Step 5: Run to verify pass**

```bash
task test -- -run 'TestProtoHasNoLegacyIDField|TestRegeneratedPbGoHasNoLegacyId' ./internal/eventbus/
```

Expected: PASS.

(Note: `task build` will now fail at every place that references `LegacyId` on the proto. T12-T14 fix those. The build is intentionally broken between T11 and T14 — acceptable mid-PR before the final `task pr-prep`.)

- [ ] **Step 6: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(proto): remove Actor.legacy_id field; regen .pb.go (holomush-w9ml T11)"
jj new -m "T12 (in progress)"
bd update <T11-bead-id> --status=closed
```

---

### Task 12: Remove `LegacyID` from `eventbus.Actor` Go struct

**Files:**

- Modify: `internal/eventbus/types.go:53-57`

**Bead:** `bd create --title "w9ml T12: remove LegacyID field from eventbus.Actor" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Write failing assertion**

Append to `internal/eventbus/proto_legacy_id_grep_test.go`:

```go
func TestEventbusActorStructHasNoLegacyID(t *testing.T) {
	out, _ := exec.Command("grep", "-n", "LegacyID",
		"internal/eventbus/types.go").CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("LegacyID MUST NOT exist in eventbus.Actor:\n%s", out)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
task test -- -run TestEventbusActorStructHasNoLegacyID ./internal/eventbus/
```

Expected: FAIL.

- [ ] **Step 3: Remove the field**

`internal/eventbus/types.go`, delete lines 53-57 (the `LegacyID` field and its doc comment). Update the `ID ulid.ULID` field comment if it references LegacyID semantics.

- [ ] **Step 4: Run to verify pass**

```bash
task test -- -run TestEventbusActorStructHasNoLegacyID ./internal/eventbus/
```

Expected: PASS.

- [ ] **Step 5: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(eventbus): remove LegacyID field from Actor (holomush-w9ml T12)"
jj new -m "T13 (in progress)"
bd update <T12-bead-id> --status=closed
```

---

### Task 13: Remove publisher LegacyID branches + JS header

**Files:**

- Modify: `internal/eventbus/publisher.go` (lines 50-55, 316-317, 351, 431-432)

**Bead:** `bd create --title "w9ml T13: remove HeaderActorLegacyID + publisher LegacyID branches" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Write failing assertion**

Append to `internal/eventbus/proto_legacy_id_grep_test.go`:

```go
func TestPublisherHasNoLegacyIDReferences(t *testing.T) {
	out, _ := exec.Command("grep", "-nE", `LegacyID|legacy_id|App-Actor-Legacy-ID`,
		"internal/eventbus/publisher.go").CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("LegacyID-related symbols MUST NOT exist in publisher.go:\n%s", out)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
task test -- -run TestPublisherHasNoLegacyIDReferences ./internal/eventbus/
```

Expected: FAIL.

- [ ] **Step 3: Remove the four targets in `publisher.go`**

| Lines | Action |
|-------|--------|
| 50-55 | Delete the `HeaderActorLegacyID = "App-Actor-Legacy-ID"` constant |
| 316-317 | Delete `else if event.Actor.LegacyID != "" { msg.Header.Set(HeaderActorLegacyID, ...) }` |
| 351 | Delete `HeaderActorLegacyID:` from the include-set map |
| 431-432 | Delete `else if a.LegacyID != "" { p.LegacyId = a.LegacyID }` |

Verify: `rg -n 'LegacyID|legacy_id|App-Actor-Legacy-ID' internal/eventbus/publisher.go` returns zero results.

- [ ] **Step 4: Run to verify pass**

```bash
task test -- -run TestPublisherHasNoLegacyIDReferences ./internal/eventbus/
```

Expected: PASS.

- [ ] **Step 5: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(eventbus): remove HeaderActorLegacyID + publisher LegacyID branches (holomush-w9ml T13)"
jj new -m "T14 (in progress)"
bd update <T13-bead-id> --status=closed
```

---

### Task 14: Remove read-side LegacyID fallbacks (5 production files) + bus conversion uniform

**Files:**

- Modify: `internal/eventbus/subscriber.go:770-773`
- Modify: `internal/eventbus/history/hot_jetstream.go:592-598`
- Modify: `internal/eventbus/history/cold_postgres.go:417-440`
- Modify: `cmd/holomush/sub_grpc.go` — `coreToBusActor` (lines 501-511) and `busEventToCoreEvent` (lines 600-607)
- Modify: `internal/plugin/event_emitter.go:310-318` — `coreActorToEventbusActor`

**Bead:** `bd create --title "w9ml T14: remove read-side LegacyID fallbacks; bus conversion uniform" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Write the global grep guard**

Create `internal/eventbus/no_legacy_id_grep_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"os/exec"
	"strings"
	"testing"
)

// Enforces INV-W9ML-7: no LegacyID/legacy_id references in production code.
func TestNoLegacyIDReferencesInProductionCode(t *testing.T) {
	cmd := exec.Command("git", "grep", "-E",
		`\bLegacyID\b|\blegacy_id\b|App-Actor-Legacy-ID`,
		"--", "*.go", "*.proto",
		":!docs/", ":!*.pb.go", ":!*_test.go")
	out, _ := cmd.CombinedOutput()
	if len(strings.TrimSpace(string(out))) > 0 {
		t.Fatalf("INV-W9ML-7 violation: LegacyID references in production code:\n%s", out)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
task test -- -run TestNoLegacyIDReferencesInProductionCode ./internal/eventbus/
```

Expected: FAIL — multiple production files still reference LegacyID.

- [ ] **Step 3: Remove each fallback**

`internal/eventbus/subscriber.go:770-773` — delete `out.LegacyID = legacy` and surrounding "Plugin-authored actors carry a string LegacyID" comment.

`internal/eventbus/history/hot_jetstream.go:592-598` — delete `out.LegacyID = legacy` and surrounding comments.

`internal/eventbus/history/cold_postgres.go:417-440` — delete the `TODO(holomush-u5bb)` comment block and any LegacyID-related handling in `actorFromAuditRow`.

`cmd/holomush/sub_grpc.go::coreToBusActor` (lines 501-511) — simplify:

```go
func coreToBusActor(a core.Actor) eventbus.Actor {
	out := eventbus.Actor{Kind: coreActorKindToBus(a.Kind)}
	if a.ID == "" {
		return out  // empty ID maps to ActorKindUnknown by the bus side default
	}
	if parsed, err := ulid.Parse(a.ID); err == nil {
		out.ID = parsed
	}
	// Note: ULID parse failure for non-empty IDs is silently ignored at this
	// boundary. Post-w9ml, every stamp site stamps a valid ULID; a failure
	// here indicates a contract violation upstream. Logging is sufficient
	// (the structured emit-side gate at coreActorToEventbusActor surfaces
	// ACTOR_ID_NOT_ULID with full context).
	return out
}
```

(`coreToBusActor` doesn't currently return an error and lots of callers rely on that. Don't change the signature here; let `coreActorToEventbusActor` (different function, in `event_emitter.go`) be the strict gate.)

`cmd/holomush/sub_grpc.go::busEventToCoreEvent` (lines 600-607) — simplify the `actorID` derivation:

```go
actorID := ""
if e.Actor.ID != (ulid.ULID{}) {
	actorID = e.Actor.ID.String()
}
// Note: LegacyID branch deleted (holomush-w9ml).
```

`internal/plugin/event_emitter.go::coreActorToEventbusActor` (lines 310-318) — make the strict gate:

```go
func coreActorToEventbusActor(a core.Actor) (eventbus.Actor, error) {
	out := eventbus.Actor{Kind: eventbusKindFromCore(a.Kind)}
	if a.ID == "" {
		return out, nil  // empty is valid for system/unknown kinds
	}
	parsed, err := ulid.Parse(a.ID)
	if err != nil {
		return eventbus.Actor{}, oops.Code("ACTOR_ID_NOT_ULID").
			With("kind", a.Kind.String()).
			With("id", a.ID).
			Wrap(err)
	}
	out.ID = parsed
	return out, nil
}
```

(Note: `eventbusKindFromCore` is the actual existing helper name — verify by `rg -n 'eventbusKindFromCore' internal/plugin/event_emitter.go`. If it's a different name, use that.)

Also: locate any callers of `coreActorToEventbusActor` and update them to handle the new `(Actor, error)` return type. Use `rg -n 'coreActorToEventbusActor' internal/plugin/` to find them.

- [ ] **Step 4: Run to verify pass**

```bash
task test -- -run TestNoLegacyIDReferencesInProductionCode ./internal/eventbus/
```

Expected: PASS.

```bash
task build
```

Expected: compile-clean. Tests will follow in Phase 6.

- [ ] **Step 5: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(eventbus,grpc,plugin): remove read-side LegacyID fallbacks; bus conversion uniform (holomush-w9ml T14)"
jj new -m "T15 (in progress)"
bd update <T14-bead-id> --status=closed
```

---

## Phase 5 — Test fixture migration & INV-49 retarget

### Task 15: `plugintest.PluginULIDFromName` helper + fixture migration

**Files:**

- Create: `internal/plugin/plugintest/registry.go`
- Modify: ~15 test files (per spec §"Test fixture impact")

**Bead:** `bd create --title "w9ml T15: plugintest helper + fixture migration" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Create the helper**

`internal/plugin/plugintest/registry.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package plugintest provides test helpers for plugin identity in fixtures.
package plugintest

import (
	"crypto/sha256"

	"github.com/oklog/ulid/v2"
)

// PluginULIDFromName returns a deterministic ULID for the given plugin
// name, suitable for hand-built test fixtures that previously used
// LegacyID strings. Determinism: ULID = sha256(name)[:16].
//
// Production code MUST NOT use this — production plugin ULIDs come from
// store.PluginRepo.Upsert (which uses idgen.New()'s real entropy). This
// helper is purely for tests that need a stable rvalue without a Postgres
// dependency.
func PluginULIDFromName(name string) ulid.ULID {
	h := sha256.Sum256([]byte(name))
	var id ulid.ULID
	copy(id[:], h[:16])
	return id
}
```

(Note: `RegisterTestPlugin` from the spec is intentionally not provided here — it would require a Postgres `PluginRepo`, but most fixture sites are unit tests. Tests that need a real registered plugin should use the integration testcontainer pattern from T0.5 and call `repo.Upsert` directly.)

- [ ] **Step 2: Migrate fixtures, file-by-file**

For each file below, locate `LegacyID: "X"` patterns and replace.

`cmd/holomush/sub_grpc_adapters_test.go` (multiple cases at 154-236):

- For `eventbus.Actor{Kind: ActorKindPlugin, LegacyID: "X"}` → `eventbus.Actor{Kind: ActorKindPlugin, ID: plugintest.PluginULIDFromName("X")}`.
- For `core.Actor{ID: "X"}` (where X is a plugin name) → `core.Actor{ID: plugintest.PluginULIDFromName("X").String()}`.
- Delete `TestCoreToBusActorStashesNonULIDAsLegacyID` (function and its assertions).
- Delete `TestBusEventToCoreEventFallsBackToLegacyID`.
- Delete any `assert.Empty(t, a.LegacyID)` lines.

`internal/eventbus/publisher_test.go` (LegacyID fallback at 284-299, 350):

- Delete the LegacyID fallback test cases.

`internal/eventbus/actor_conversion_test.go`:

- Delete LegacyID branches; leave the ULID round-trip assertions.

`internal/eventbus/history/actor_from_envelope_test.go:47-67`:

- Delete `TestActorFromEnvelopeFallsBackToLegacyID`.

`internal/eventbus/history/cold_postgres_test.go:23-59`:

- Retarget the Decision 5 lock test: instead of asserting `LegacyID` round-trip, assert that `Actor.ID` round-trips through the cold tier. Use `plugintest.PluginULIDFromName("core-scenes").Bytes()` as the test ULID.

`internal/plugin/event_emitter_crypto_test.go:40, 67`:

- `core.Actor{ID: "test-plugin"}` → `core.Actor{ID: plugintest.PluginULIDFromName("test-plugin").String()}`.

`internal/plugin/event_emitter_test.go:47, 146, 524, 554, 583`:

- Same pattern.

`internal/plugin/manager_routing_test.go:192, 332`:

- `core.Actor{ID: manifest.Name}` → `core.Actor{ID: plugintest.PluginULIDFromName(manifest.Name).String()}`.

`internal/plugin/manager_test.go:1950`:

- Same pattern.

`test/integration/eventbus_e2e/cross_tier_query_test.go:488`:

- Drop the `LegacyId` argument; existing `Id` argument carries the ULID.

`test/integration/plugin/actor_authentication_test.go:164, 174`:

- Remove the `ActorLegacyID` field from the test's observation struct; assert against `Actor.Id` (the ULID bytes).

- [ ] **Step 3: Run tests to verify pass**

```bash
task test -- ./...
```

Expected: PASS — all unit tests green. (Integration tests at `test/integration/crypto/` are addressed in T17.)

- [ ] **Step 4: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "test: migrate fixtures from LegacyID to ULID; delete legacy-specific tests (holomush-w9ml T15)"
jj new -m "T16 (in progress)"
bd update <T15-bead-id> --status=closed
```

---

### Task 16: Actor display path — `actorIDString` + `eventbusEventToEventFrame`

**Files:**

- Modify: `internal/grpc/server.go` — `actorIDString` (lines 596-608) gains `IdentityRegistry`
- Modify: `internal/grpc/query_stream_history.go` — `eventbusEventToEventFrame` (lines 512-522)
- Modify: `cmd/holomush/main.go` (or wherever `CoreServer` is constructed) — wire `IdentityRegistry`

**Bead:** `bd create --title "w9ml T16: actor display via IdentityRegistry.NameByID" --parent=holomush-w9ml --type=task --priority=1`

**Why this exists:** Reviewer Finding 7 — `internal/telnet/` and `internal/web/` have no actor-name renderer. The actual display formatter is `internal/grpc/server.go::actorIDString`, which today returns either the ULID-string (for character/player) or the LegacyID string (for plugin). Post-epic, plugin actors carry ULIDs; without registry resolution, telnet would show `01HABC...` instead of `core-scenes`. This task wires `NameByID` into `actorIDString`.

- [ ] **Step 1: Read the current `actorIDString`**

```bash
sed -n '593,610p' internal/grpc/server.go
```

Current implementation:

```go
func actorIDString(a eventbus.Actor) string {
	if a.ID.Compare(ulid.ULID{}) != 0 {
		return a.ID.String()
	}
	if a.LegacyID != "" {
		return a.LegacyID
	}
	return ""
}
```

- [ ] **Step 2: Write failing tests**

Create `internal/grpc/actor_display_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
)

type stubIDReg struct{ namesByID map[ulid.ULID]string }

func (s *stubIDReg) NameByID(id ulid.ULID) (string, bool) {
	name, ok := s.namesByID[id]
	return name, ok
}
func (s *stubIDReg) IDByName(string) (ulid.ULID, bool) { return ulid.ULID{}, false }

func TestActorIDStringResolvesPluginNameViaRegistry(t *testing.T) {
	pluginID := plugintest.PluginULIDFromName("core-scenes")
	reg := &stubIDReg{namesByID: map[ulid.ULID]string{pluginID: "core-scenes"}}

	got := actorIDString(eventbus.Actor{Kind: eventbus.ActorKindPlugin, ID: pluginID}, reg)
	assert.Equal(t, "core-scenes", got)
}

func TestActorIDStringResolvesSystemSentinelViaRegistry(t *testing.T) {
	reg := &stubIDReg{namesByID: map[ulid.ULID]string{
		core.SystemActorULID: "system",
	}}

	got := actorIDString(eventbus.Actor{Kind: eventbus.ActorKindSystem, ID: core.SystemActorULID}, reg)
	assert.Equal(t, "system", got)
}

func TestActorIDStringFallsBackToULIDStringForCharacter(t *testing.T) {
	// Use core.NewULID() — hand-typed Crockford strings are error-prone
	// (the alphabet excludes I/L/O/U and length must be exactly 26).
	charID := core.NewULID()
	reg := &stubIDReg{namesByID: nil} // character ULIDs not in registry

	got := actorIDString(eventbus.Actor{Kind: eventbus.ActorKindCharacter, ID: charID}, reg)
	assert.Equal(t, charID.String(), got)
}

func TestActorIDStringPreservesZeroULIDGuard(t *testing.T) {
	reg := &stubIDReg{}
	got := actorIDString(eventbus.Actor{Kind: eventbus.ActorKindUnknown}, reg)
	assert.Equal(t, "", got, "zero ULID MUST stringify to empty per existing wire contract")
}

// (mustParseULID helper removed — every test fixture uses
// core.NewULID() for fresh entropy or plugintest.PluginULIDFromName(name)
// for deterministic-by-name fixtures, neither of which can produce
// invalid Crockford strings.)
```

- [ ] **Step 3: Run to verify failure**

```bash
task test -- -run 'TestActorIDString' ./internal/grpc/
```

Expected: FAIL — `actorIDString`'s signature doesn't take a registry parameter.

- [ ] **Step 4: Modify `actorIDString` in `internal/grpc/server.go`**

Replace lines 596-608:

```go
// actorIDString stringifies the bus-side actor for the gRPC wire.
// Resolution order:
//   1. Zero ULID → "" (preserves existing wire contract; gateway/web
//      clients don't see synthetic "00000000..." values).
//   2. NameByID lookup via IdentityRegistry → returns the registered
//      name for plugin and system sentinel ULIDs (uniform display).
//   3. Fallback: ULID-string form (characters / players whose ULIDs
//      are not in the IdentityRegistry — they live in the user store).
func actorIDString(a eventbus.Actor, reg plugins.IdentityRegistry) string {
	if a.ID.Compare(ulid.ULID{}) == 0 {
		return ""
	}
	if reg != nil {
		if name, ok := reg.NameByID(a.ID); ok {
			return name
		}
	}
	return a.ID.String()
}
```

(Required imports: `"github.com/holomush/holomush/internal/plugin"` aliased as `plugins`. Verify import path.)

Locate every call site of `actorIDString` and pass the registry. Existing callers at `internal/grpc/server.go:586` (e.g., `ActorId: actorIDString(ev.Actor)`) need the registry. The `CoreServer` struct should have an `identityRegistry plugins.IdentityRegistry` field; the call becomes `actorIDString(ev.Actor, s.identityRegistry)`.

- [ ] **Step 5: Modify `eventbusEventToEventFrame`**

`internal/grpc/query_stream_history.go:512-522` — locate the function, remove the `else if e.Actor.LegacyID != ""` branch. The function probably also produces a string actor ID; if so, route through `actorIDString` (now requiring the registry — the surrounding function signature may need a registry parameter or access via the receiver).

- [ ] **Step 6: Wire `IdentityRegistry` into `CoreServer`**

Locate `CoreServer` struct and constructor (probably in `internal/grpc/server.go` or `cmd/holomush/main.go`). Add `identityRegistry plugins.IdentityRegistry` field. Add a `WithIdentityRegistry(reg plugins.IdentityRegistry)` option (if `CoreServer` uses functional options) or add a constructor parameter. Wire the plugin manager (which implements `IdentityRegistry`) at server construction time.

- [ ] **Step 7: Run to verify pass**

```bash
task test -- -run 'TestActorIDString' ./internal/grpc/
```

Expected: PASS.

```bash
task build && task test -- ./internal/grpc/ ./cmd/holomush/
```

Expected: build clean, tests green. Existing tests calling `actorIDString` need the new parameter — update inline.

- [ ] **Step 8: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(grpc): actorIDString resolves plugin/system names via IdentityRegistry (holomush-w9ml T16)"
jj new -m "T17 (in progress)"
bd update <T16-bead-id> --status=closed
```

---

### Task 17: INV-49 retarget (Phase 3d Decision 5 lock test)

**Files:**

- Modify: `test/integration/crypto/inv49_envelope_roundtrip_test.go` (3 It-blocks at lines 59, 111, 165)
- Modify: `test/integration/crypto/e2e_test.go` (delete It-block at 647 + helper at 225)

**Bead:** `bd create --title "w9ml T17: retarget INV-49 round-trip test (3 It-blocks + e2e parallel + helper)" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Verify current state**

```bash
rg -n '^\s*It\(' test/integration/crypto/inv49_envelope_roundtrip_test.go test/integration/crypto/e2e_test.go
rg -n 'publishSensitiveWithLegacyActor' test/integration/crypto/
```

Expected: It-blocks at lines 59, 111, 165 in `inv49_envelope_roundtrip_test.go`, and at line 647 in `e2e_test.go`. Helper at `e2e_test.go:225` with call sites at `inv49_envelope_roundtrip_test.go:121,189` and `e2e_test.go:657`.

- [ ] **Step 2: Retarget `inv49_envelope_roundtrip_test.go:111`**

Rename the It description from `"byte-equal envelope for plugin actor with Actor.legacy_id (Decision 5 lock)"` to `"byte-equal envelope for plugin actor with Actor.id ULID (Decision 5 + w9ml lock)"`.

In the fixture setup:

- Replace `LegacyID: "core-scenes"` with `ID: plugintest.PluginULIDFromName("core-scenes")`.
- Replace assertion `Expect(ev.Actor.LegacyID).To(Equal("core-scenes"))` with `Expect(ev.Actor.ID).To(Equal(plugintest.PluginULIDFromName("core-scenes")))`.

- [ ] **Step 3: Delete `useLegacyActor: true` case at line 165**

The `It("cold-read decrypts correctly for both actor kinds via dispatcher chain")` block iterates a slice of test cases. Delete the `{label: "plugin-actor with legacy_id", ..., useLegacyActor: true}` entry. If the slice is now a single case, inline it (or keep as a 1-element table for clarity).

- [ ] **Step 4: Delete `e2e_test.go:647` block**

Delete the entire `It("round-trips through cold tier with Actor.legacy_id preserved")` block. Coverage moved to retargeted `inv49_envelope_roundtrip_test.go:111`.

- [ ] **Step 5: Delete `publishSensitiveWithLegacyActor` helper + call sites**

Delete the helper function at `e2e_test.go:225` (and the comment block above it).

Delete call sites at `inv49_envelope_roundtrip_test.go:121, 189` (those are inside the deleted/retargeted It-blocks anyway).

Delete call site at `e2e_test.go:657` (inside the deleted e2e It-block).

- [ ] **Step 6: Run integration tests**

```bash
task test:int -- -run 'INV.49|envelope_roundtrip|legacy_id' ./test/integration/crypto/...
```

Expected: PASS.

- [ ] **Step 7: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "test(crypto): retarget INV-49 lock to Actor.id ULID round-trip (holomush-w9ml T17)"
jj new -m "T18 (in progress)"
bd update <T17-bead-id> --status=closed
```

---

## Phase 6 — Bootstrap orphan check, JS purge, CI guards

### Task 18: JetStream purge command + operator runbook

**Files:**

- Create: `cmd/holomush-cutover/main.go`
- Create: `site/docs/operating/legacy-id-cutover.md`
- Modify: `Taskfile.yaml` (add `migrate:plugin-actors-cutover` task)

**Bead:** `bd create --title "w9ml T18: JS purge cutover command + runbook" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Create the cutover command**

`cmd/holomush-cutover/main.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// holomush-cutover is the one-time deploy step for holomush-w9ml.
// Runs PG TRUNCATE events_audit + JetStream PurgeStream so no
// pre-cutover encrypted plugin-actor messages remain to fail AEAD
// post-cutover (their AAD bytes were sealed with the pre-w9ml proto
// shape).
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	ctx := context.Background()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		slog.Error("DATABASE_URL must be set")
		os.Exit(2)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		slog.Error("pg connect failed", "err", err); os.Exit(1)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE events_audit`); err != nil {
		slog.Error("TRUNCATE events_audit failed", "err", err); os.Exit(1)
	}
	slog.Info("events_audit truncated")

	natsURL := os.Getenv("NATS_URL")
	streamName := os.Getenv("NATS_STREAM_NAME")
	if streamName == "" {
		streamName = "events" // verify against project convention at impl time
	}
	nc, err := nats.Connect(natsURL)
	if err != nil {
		slog.Error("nats connect failed", "err", err); os.Exit(1)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		slog.Error("jetstream client failed", "err", err); os.Exit(1)
	}
	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		slog.Error("stream lookup failed", "err", err, "stream", streamName); os.Exit(1)
	}
	if err := stream.Purge(ctx); err != nil {
		slog.Error("stream purge failed", "err", err); os.Exit(1)
	}
	slog.Info("jetstream stream purged", "stream", streamName)
}
```

(Note: the actual NATS dependency import paths should match what the rest of `internal/eventbus/` uses. Verify by `rg -n 'nats-io|jetstream' internal/eventbus/`.)

- [ ] **Step 2: Add Taskfile entry**

In `Taskfile.yaml`:

```yaml
  migrate:plugin-actors-cutover:
    desc: One-time cutover for holomush-w9ml (TRUNCATE events_audit + JS PurgeStream)
    cmds:
      - go run ./cmd/holomush-cutover
```

- [ ] **Step 3: Create the runbook**

`site/docs/operating/legacy-id-cutover.md`:

````markdown
<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# `legacy_id` Elimination Cutover (holomush-w9ml)

One-time deploy step for `holomush-w9ml`. Run once, BEFORE bringing up the
binary that includes the proto-field removal.

## What it does

1. `TRUNCATE events_audit` — removes legacy plugin-actor events whose
   envelope blobs reference the now-defunct `Actor.legacy_id` field.
   Migration 000018 also truncates; this command is idempotent.
2. JetStream `PurgeStream("events")` — removes pre-cutover encrypted
   plugin-actor events. Their AAD bytes were sealed against the
   pre-w9ml proto shape; post-cutover code computes a different AAD,
   so AEAD verification fails. Purging avoids the failure mode.

## Run

```bash
DATABASE_URL=... NATS_URL=... NATS_STREAM_NAME=events task migrate:plugin-actors-cutover
```

```text

## Failure modes

- **PG TRUNCATE failure:** exits non-zero before purge. Re-run.
- **JS purge failure:** exits non-zero with PG already truncated. Re-run after fixing NATS connection.

## Post-cutover

Deploy the new binary. The bootstrap orphan check passes on first start.
```
````

- [ ] **Step 4: Smoke test**

Manual:

```bash
DATABASE_URL=postgres://localhost/holomush_dev NATS_URL=nats://localhost:4222 task migrate:plugin-actors-cutover
```

Expected: both ops log success.

- [ ] **Step 5: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(cutover): one-time PG TRUNCATE + JS PurgeStream + runbook (holomush-w9ml T18)"
jj new -m "T19 (in progress)"
bd update <T18-bead-id> --status=closed
```

---

### Task 19: Bootstrap orphan check

**Files:**

- Modify: `cmd/holomush/main.go` (add orphan check)
- Add tests: `cmd/holomush/bootstrap_orphan_test.go` (package main, integration build tag)

**Bead:** `bd create --title "w9ml T19: bootstrap orphan check (PLUGIN_ACTOR_ORPHAN_DETECTED)" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Write failing test**

Create `cmd/holomush/bootstrap_orphan_test.go`:

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/store"
)

func TestBootstrapPassesWithCleanEventsAudit(t *testing.T) {
	pool, cleanup := newCmdMainTestPool(t)
	defer cleanup()
	require.NoError(t, applyAllMigrations(pool))
	require.NoError(t, runBootstrapOrphanCheck(context.Background(), pool))
}

func TestBootstrapFailsWithSyntheticOrphan(t *testing.T) {
	pool, cleanup := newCmdMainTestPool(t)
	defer cleanup()
	require.NoError(t, applyAllMigrations(pool))

	_, err := pool.Exec(context.Background(), `
		INSERT INTO events_audit (id, subject, type, timestamp, actor_kind,
		                         actor_id, envelope, schema_ver, codec, js_seq, rendering)
		VALUES ($1, 'test', 'test', now(), $2, NULL, '\x00', 1, 'identity', 1, '{}'::jsonb)
	`, []byte("0123456789abcdef"), eventbus.ActorKindPlugin.String())
	require.NoError(t, err)

	err = runBootstrapOrphanCheck(context.Background(), pool)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PLUGIN_ACTOR_ORPHAN_DETECTED")
}

// newCmdMainTestPool: testcontainer for cmd/holomush_test (separate from
// internal/store_test's helper because package boundaries don't share
// _test.go files).
func newCmdMainTestPool(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()
	pgC, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:16-alpine"),
		postgres.WithDatabase("holomush_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
	)
	require.NoError(t, err)
	connStr, err := pgC.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	return pool, func() { pool.Close(); _ = pgC.Terminate(ctx) }
}

func applyAllMigrations(pool *pgxpool.Pool) error {
	connStr := pool.Config().ConnString()
	migrator, err := store.NewMigrator(connStr)
	if err != nil { return err }
	defer migrator.Close()
	return migrator.Up()
}
```

- [ ] **Step 2: Run to verify failure**

```bash
task test:int -- -run TestBootstrap ./cmd/holomush/
```

Expected: FAIL — `runBootstrapOrphanCheck` undefined.

- [ ] **Step 3: Implement the check**

Add to `cmd/holomush/main.go` (or a sibling file in `package main`):

```go
import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
	"github.com/holomush/holomush/internal/eventbus"
)

// runBootstrapOrphanCheck refuses to start if any plugin-kind event in
// events_audit lacks an actor_id (a legacy event that survived a w9ml
// migration mis-step). Defense-in-depth: migration 000018 makes orphans
// impossible from a clean install.
func runBootstrapOrphanCheck(ctx context.Context, pool *pgxpool.Pool) error {
	var count int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM events_audit
		 WHERE actor_kind = $1 AND actor_id IS NULL
	`, eventbus.ActorKindPlugin.String()).Scan(&count)
	if err != nil {
		return oops.Code("BOOTSTRAP_ORPHAN_CHECK_FAILED").Wrap(err)
	}
	if count > 0 {
		return oops.Code("PLUGIN_ACTOR_ORPHAN_DETECTED").
			With("count", count).
			Errorf("legacy plugin-actor events present after w9ml migration")
	}
	return nil
}
```

Wire into the bootstrap sequence — after `migrator.Up()` runs and before `Manager` construction.

- [ ] **Step 4: Run to verify pass**

```bash
task test:int -- -run TestBootstrap ./cmd/holomush/
```

Expected: PASS.

- [ ] **Step 5: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(bootstrap): orphan check refuses start with legacy plugin events (holomush-w9ml T19)"
jj new -m "T20 (in progress)"
bd update <T19-bead-id> --status=closed
```

---

### Task 20: CI guard — `TestNoDeleteFromPluginsInCodebase`

**Files:**

- Create: `internal/store/no_delete_grep_test.go`

**Bead:** `bd create --title "w9ml T20: CI guard for INV-W9ML-9 (no DELETE FROM plugins)" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Write the test**

`internal/store/no_delete_grep_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"os/exec"
	"strings"
	"testing"
)

// Enforces INV-W9ML-9: plugin rows are never DELETEd. SweepInactive sets
// gc_at instead. CI grep guards against future changes that would
// reintroduce DELETE.
func TestNoDeleteFromPluginsInCodebase(t *testing.T) {
	out, _ := exec.Command("git", "grep", "-nE",
		`DELETE\s+FROM\s+plugins\b`,
		"--", "*.go").CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("INV-W9ML-9 violation: DELETE FROM plugins in production code:\n%s", out)
	}
}
```

- [ ] **Step 2: Run**

```bash
task test -- -run TestNoDeleteFromPluginsInCodebase ./internal/store/
```

Expected: PASS — no `DELETE FROM plugins` exists.

- [ ] **Step 3: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "test(store): CI guard for INV-W9ML-9 (holomush-w9ml T20)"
jj new -m "T21 (in progress)"
bd update <T20-bead-id> --status=closed
```

---

## Phase 7 — End-to-end + finalization

### Task 21: End-to-end plugin-actor ULID round-trip — extend existing harness

**Files:**

- Modify: `test/integration/eventbus_e2e/cross_tier_query_test.go` — add a new scenario

**Bead:** `bd create --title "w9ml T21: plugin-actor ULID round-trip scenario in cross_tier_query_test" --parent=holomush-w9ml --type=task --priority=1`

**Why this exists:** Reviewer Finding 4 (Revision 2) — the original Revision 2 plan invented helpers `setupTestEnv`, `registerPluginInRegistry`, `emitFromBinaryPlugin`, `queryEventsAuditByID`, `queryHistory` that don't exist. Building that harness would be 1-2 days of scaffolding. The acceptance-criterion-5 chain (emit → audit → history → display) is exercised by extending the EXISTING `TestCrossTierQueryEndToEnd` harness at `test/integration/eventbus_e2e/cross_tier_query_test.go:51` — it already has `bus := eventbustest.New(t)`, `pool := freshPool(t)`, `pub`, and helpers `mintAt`, `publishAll`, `buildReader`, `drainStream`. We add ONE new scenario that publishes a plugin-actor event with a ULID and asserts the ULID round-trips through both tiers.

The display-resolution leg (acceptance #5's "renderer resolves ULID back to plugin name") is covered by T16's unit tests (`TestActorIDStringResolvesPluginNameViaRegistry`). The full chain is then proved by composition: T1 (migration), T15 (fixtures), T16 (display unit), T17 (INV-49 round-trip), T19 (orphan check), T21 (cross-tier round-trip with plugin actor) — together they assert the spec's acceptance criterion 5 without requiring a new bespoke harness.

- [ ] **Step 1: Read the existing harness**

```bash
sed -n '51,140p' test/integration/eventbus_e2e/cross_tier_query_test.go
```

Confirm helpers `mintAt`, `publishAll`, `buildReader`, `drainStream` are accessible from the test file's scenario closures.

- [ ] **Step 2: Add the new scenario**

In `test/integration/eventbus_e2e/cross_tier_query_test.go`, locate the `scenarios := []struct{...}{...}` slice (around line 69). Append a new entry:

```go
{
    name: "scenario_w9ml_plugin_actor_ulid_round_trip",
    run: func(t *testing.T, ctx context.Context, subject eventbus.Subject) {
        // holomush-w9ml acceptance criterion 5: plugin-actor events
        // round-trip Actor.ID (ULID) through both tiers without loss.
        // Pre-w9ml this would have used Actor.LegacyID; post-w9ml only
        // Actor.ID (16-byte ULID) is on the wire.
        pluginULID := plugintest.PluginULIDFromName("core-scenes")

        // Synthesize a plugin-actor event using mintAt's pattern, then
        // overwrite the Actor field to the plugin shape we want to test.
        ev := mintAt(subject, baseNow.Add(-2*time.Hour), "plugin-emit")
        ev.Actor = eventbus.Actor{
            Kind: eventbus.ActorKindPlugin,
            ID:   pluginULID,
        }
        publishAll(ctx, t, pub, []eventbus.Event{ev})
        bus.AwaitStreamLastSeq(t, currentStreamLastSeq(t, bus), 5*time.Second)

        // Hot-tier read.
        r := buildReader(bus, pool, streamMaxAge, baseNow)
        stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
            Subject:   subject,
            NotBefore: baseNow.Add(-3 * time.Hour),
            Direction: eventbus.DirectionForward,
            PageSize:  50,
        })
        require.NoError(t, err)
        t.Cleanup(func() { _ = stream.Close() })

        got := drainStream(t, stream)
        require.Len(t, got, 1)
        assert.Equal(t, ev.ID, got[0].ID)
        assert.Equal(t, eventbus.ActorKindPlugin, got[0].Actor.Kind)
        assert.Equal(t, pluginULID, got[0].Actor.ID,
            "plugin Actor.ID MUST round-trip through hot tier as the same ULID")

        // (Cold-tier round-trip is exercised by INV-49 at T17 against
        // encrypted plugin events; here we cover the hot path only.)
    },
},
```

(Required imports: `"github.com/holomush/holomush/internal/plugin/plugintest"` if not already imported.)

- [ ] **Step 3: Run**

```bash
task test:int -- -run 'TestCrossTierQueryEndToEnd/scenario_w9ml_plugin_actor_ulid_round_trip' ./test/integration/eventbus_e2e/
```

Expected: PASS.

- [ ] **Step 4: Commit + new**

```bash
JJ_EDITOR=true jj --no-pager describe -m "test(eventbus_e2e): plugin-actor ULID round-trip scenario (holomush-w9ml T21)"
jj new -m "T22 (in progress)"
bd update <T21-bead-id> --status=closed
```

---

### Task 22: Final gate + bead closures + land the plane

**Files:** None modified — verification + bookkeeping.

**Bead:** `bd create --title "w9ml T22: pr-prep + ojw1.8/u5bb closure + land the plane" --parent=holomush-w9ml --type=task --priority=1`

- [ ] **Step 1: Run the full PR-prep gate**

```bash
task pr-prep
```

Expected: green. Mirrors all CI jobs (lint, format, schema, license, unit, integration, E2E).

- [ ] **Step 2: Verify INV-W9ML-7 (no LegacyID anywhere)**

```bash
git grep -E '\bLegacyID\b|\blegacy_id\b|App-Actor-Legacy-ID' -- '*.go' '*.proto' ':!docs/' ':!*.pb.go'
```

Expected: zero output.

```bash
grep -L 'LegacyId' pkg/proto/holomush/eventbus/v1/eventbus.pb.go
```

Expected: file path printed (means pattern not found).

- [ ] **Step 3: Verify INV-W9ML-9**

```bash
git grep -nE 'DELETE\s+FROM\s+plugins\b' -- '*.go'
```

Expected: zero output.

- [ ] **Step 4: Close `holomush-ojw1.8` as superseded**

```bash
bd update holomush-ojw1.8 --notes "Superseded by holomush-w9ml. ojw1.8 proposed adding core.Actor.LegacyID to plumb plugin names through the actor resolver; w9ml inverted that direction by eliminating LegacyID at every layer in favor of uniform ULIDs."
bd close holomush-ojw1.8
```

- [ ] **Step 5: Close `holomush-u5bb` as superseded**

```bash
bd update holomush-u5bb --notes "Superseded by holomush-w9ml. u5bb proposed an actor_legacy_id column on events_audit to persist LegacyID through the cold tier; w9ml eliminated LegacyID entirely so the dedicated column is no longer needed."
bd close holomush-u5bb
```

- [ ] **Step 6: Final commit**

If `pr-prep` surfaced any last-mile fixes:

```bash
JJ_EDITOR=true jj --no-pager describe -m "chore(w9ml): final pr-prep tweaks (holomush-w9ml T22)"
```

(No `jj new` — this is the final commit.)

- [ ] **Step 7: Land the plane**

```bash
jj git fetch
# Rebase the entire stack on top of fresh main. Use the revset that
# captures every commit from main..@ (per project memory feedback_jj_rebase_targeted
# — never bare `jj rebase -d main`).
jj rebase -s 'all:roots(trunk()..@)' -d main@origin --skip-emptied

# Push the branch.
jj bookmark set legacy-id-elimination -r @
jj git push --branch legacy-id-elimination

# Create PR.
gh pr create --title "feat: eliminate Actor.legacy_id (holomush-w9ml)" --body "$(cat <<'EOF'
## Summary

Eliminates `eventbusv1.Actor.legacy_id` and the upstream `core.Actor.ID` mixed-semantic-identifier overloading. Per Phase 3d Decision 6.

- New `IdentityRegistry` (in-memory cache backed by `plugins` Postgres table) resolves plugin name ↔ ULID.
- Plugin emit stamp sites in `goplugin/host.go` resolve via `IDByName` and stamp `core.Actor{ID: pluginULID.String()}`.
- System actors use compile-time sentinel ULIDs (`SystemActorULID`, `WorldServiceActorULID`).
- Bus conversion uniform across all kinds.
- `actorIDString` resolves plugin/system ULIDs to display names via `NameByID`.
- ABAC engine unchanged.

Spec: `docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md`
Plan: `docs/superpowers/plans/2026-05-04-legacy-id-elimination.md`

Closes (by supersession):
- holomush-ojw1.8 — propagate Actor.LegacyID through plugin actor resolver
- holomush-u5bb — persist Actor.LegacyID in events_audit

## Test plan

- [ ] Unit: PluginRepo, Manager cache, hash compute, sentinel resolution
- [ ] Integration: end-to-end emit→audit→history→display chain
- [ ] Migration: 000018 + bootstrap orphan check + JS purge cutover
- [ ] CI guards: no LegacyID, no DELETE FROM plugins
- [ ] INV-49 retarget: ULID round-trip through cold tier

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 8: After merge, close epic + clean up workspace**

```bash
bd close holomush-w9ml

cd /Volumes/Code/github.com/holomush/holomush  # exit the workspace before forgetting it
jj workspace forget legacy-id-elimination
rm -rf /Volumes/Code/github.com/holomush/.worktrees/legacy-id-elimination
```

- [ ] **Step 9: Final bead**

```bash
bd update <T22-bead-id> --status=closed
```

---

## Bead chain structure

```text
holomush-w9ml                   (existing epic — eliminate Actor.legacy_id; uniform ULID identity at every layer)
├── holomush-w9ml.1   T0.5: store test harness (newTestPool + runMigrations)
├── holomush-w9ml.2   T1:   migration 000018 (plugins table + events_audit truncate)
├── holomush-w9ml.3   T2:   sentinel ULIDs + IsSentinelULID + ActorSystemID repurpose
├── holomush-w9ml.4   T3:   PluginRepo + PostgresPluginRepo
├── holomush-w9ml.5   T4:   IdentityRegistry interface declaration
├── holomush-w9ml.6   T5:   Manager cache + NameByID/IDByName + sentinel bootstrap
├── holomush-w9ml.7   T6:   Manager hash compute + loadPlugin Upsert + drift logging
├── holomush-w9ml.8   T7:   Manager.UnloadPlugin (new method) + cache mutation
├── holomush-w9ml.9   T8:   GC sweep at LoadAll end + RetentionDays config
├── holomush-w9ml.10  T9:   plugin emit stamp sites (4 in goplugin/host.go)
├── holomush-w9ml.11  T10:  system actor stamp site migrations (4 sites)
├── holomush-w9ml.12  T11+T12+T13+T14 (MERGED): wire-format atomic cutover
│                            (proto + eventbus.Actor struct + JS header constant + read fallbacks)
├── holomush-w9ml.13  T15:  plugintest helper + fixture migration (~15 files)
├── holomush-w9ml.14  T16:  actorIDString — IdentityRegistry display resolution
├── holomush-w9ml.15  T17:  INV-49 retarget (Phase 3d Decision 5 lock)
├── holomush-w9ml.16  T18:  JetStream purge cutover command + runbook
├── holomush-w9ml.17  T19:  bootstrap orphan check
├── holomush-w9ml.18  T20:  CI guard — no DELETE FROM plugins (INV-W9ML-9)
├── holomush-w9ml.19  T21:  E2E plugin-actor ULID round-trip scenario
└── holomush-w9ml.20  T22:  final gate + bead closures + land the plane (closing)
```

T0 (pre-flight repo-state checks) is plan-time housekeeping and gets no bead.
T11+T12+T13+T14 are merged into a single bead because the wire-format change
is atomic-by-construction: the proto field removal cascades through the Go
struct, the publisher header constant, and the read-side fallbacks. Build is
intentionally broken between T11 and T14; partial states aren't reviewable.

### `bd create` blocks

```bash
bd create \
  --title "T0.5: store test harness — newTestPool + runMigrations" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Build testcontainer-backed `newTestPool` and `runMigrations(t, pool, n)` helpers shared by downstream test tasks.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md (test-infra prerequisite — no specific section)
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 0.5

**TDD acceptance criteria:**
- TestNewTestPoolAndRunMigrationsSmoke (testcontainer comes up + migrations apply through 17)

**Verification steps:**
- task test:int -- -run TestNewTestPoolAndRunMigrationsSmoke ./internal/store/...

**Files touched:**
- internal/store/testhelpers_test.go — new file with `newTestPool` and `runMigrations`

**Dependencies:** none (foundation task)

**Out of scope:** any production code changes; this bead is purely test-infrastructure scaffolding consumed by w9ml.2/4/17/19.
EOF
)"

bd create \
  --title "T1: migration 000018 — plugins table + events_audit TRUNCATE" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Migration 000018 creates `plugins` table with partial UNIQUE index `(name) WHERE gc_at IS NULL`; TRUNCATEs `events_audit` to remove legacy plugin-actor envelope rows.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md § Plugin Registry → Schema
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 1

**TDD acceptance criteria:**
- TestMigration000018CreatesPluginsTable (schema + partial UNIQUE index)
- TestMigration000018TruncatesEventsAudit (pre-w9ml row → 0 rows post)

**Verification steps:**
- task test:int -- -run TestMigration000018 ./internal/store/...

**Files touched:**
- internal/store/migrations/000018_create_plugins.up.sql — new
- internal/store/migrations/000018_create_plugins.down.sql — new (DROP TABLE; data wipe irreversible)
- internal/store/migrate_plugins_integration_test.go — new

**Dependencies:** holomush-w9ml.1 (test harness)

**Out of scope:** PluginRepo Go code (w9ml.4); JetStream purge step (w9ml.16). The migration is the SQL-only half; the JS purge is a separate cutover deliverable.
EOF
)"

bd create \
  --title "T2: sentinel ULIDs + IsSentinelULID + ActorSystemID repurpose" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Introduce compile-time sentinel ULID constants `SystemActorULID` ({0…0,0x01}) and `WorldServiceActorULID` ({0…0,0x02}) in internal/core/event.go; convert `ActorSystemID` from `const = "system"` to `var = SystemActorULID.String()` so existing call sites compile unchanged; add `IsSentinelULID` helper for sentinel-collision detection.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md § System actor sentinel ULIDs
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 2

**TDD acceptance criteria:**
- TestSystemActorULIDRendersAsCanonicalCrockford (== "00000000000000000000000001")
- TestWorldServiceActorULIDRendersAsCanonicalCrockford (== "00000000000000000000000002")
- TestActorSystemIDIsSystemActorULIDString
- TestIsSentinelULIDIdentifiesKnownSentinels
- TestIsSentinelULIDRejectsZeroULID
- TestIsSentinelULIDRejectsEntropyULID
- TestSentinelTagsUnique (forward-defense for new sentinels)

**Verification steps:**
- task test -- -run 'TestSystemActor|TestWorldServiceActor|TestActorSystemID|TestIsSentinelULID|TestSentinelTagsUnique' ./internal/core/
- task test -- ./internal/core/

**Files touched:**
- internal/core/event.go:164-171 — add sentinels + IsSentinelULID; convert ActorSystemID const→var; update Actor.ID doc comment
- internal/core/event_test.go — add sentinel + tag-uniqueness tests

**Dependencies:** none

**Out of scope:** system stamp-site migrations (w9ml.11); IdentityRegistry sentinel-bootstrap registration (w9ml.6).
EOF
)"

bd create \
  --title "T3: PluginRepo interface + PostgresPluginRepo" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Create `PluginRepo` interface (`Upsert`, `ListAll`, `SweepInactive`) and `PostgresPluginRepo` implementation against pgxpool. Upsert handles three states: insert-with-fresh-ULID, update-no-drift, update-with-drift-report. SweepInactive uses `UPDATE … SET gc_at = now()` (never DELETE — INV-W9ML-9).

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md § Repository
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 3

**TDD acceptance criteria:**
- TestPluginRepoUpsertInsertsNewRow
- TestPluginRepoUpsertUpdatesLastSeenWithoutDrift
- TestPluginRepoUpsertReportsDriftOnHashChange
- TestPluginRepoListAllReturnsActiveAndDeactivated
- TestPluginRepoSweepInactiveDeactivatesStaleRowsOnly
- TestPluginRepoSweepNeverDeletesRows (INV-W9ML-9)

**Verification steps:**
- task test:int -- -run TestPluginRepo ./internal/store/...

**Files touched:**
- internal/store/plugin_repo.go — new (PluginRepo + PluginUpsertInput + PluginRow + DriftReport + PostgresPluginRepo)
- internal/store/plugin_repo_test.go — new

**Dependencies:** holomush-w9ml.1 (test harness), holomush-w9ml.2 (migration creates the table)

**Out of scope:** Manager integration (w9ml.6/.7/.8/.9); GC scheduling logic (w9ml.9 wires the sweep into LoadAll).
EOF
)"

bd create \
  --title "T4: IdentityRegistry interface declaration" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Declare the `IdentityRegistry` interface in internal/plugin/identity_registry.go (package `plugins` — same as existing ServiceRegistry). Two methods: `NameByID(ulid.ULID) (string, bool)` resolves three populations (active plugins, historical plugins, system sentinels); `IDByName(string) (ulid.ULID, bool)` resolves only currently-active plugins.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md § IdentityRegistry interface
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 4

**TDD acceptance criteria:**
- TestIdentityRegistryInterfaceIsSatisfiable (compile-time conformance via stubIdentityRegistry)

**Verification steps:**
- task test -- -run TestIdentityRegistryInterfaceIsSatisfiable ./internal/plugin/

**Files touched:**
- internal/plugin/identity_registry.go — new (interface only)
- internal/plugin/identity_registry_test.go — new (stub conformance test)

**Dependencies:** none

**Out of scope:** Manager implementation of the interface (w9ml.6); consumer wiring (w9ml.10, w9ml.14).
EOF
)"

bd create \
  --title "T5: Manager cache + NameByID/IDByName + sentinel bootstrap" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** `Manager` implements `IdentityRegistry`. Add `pluginRepo`, `nameByID`, `activeByName` fields (guarded by existing `mu`). NewManager registers system sentinels in nameByID first, then loads plugin rows from repo with sentinel-collision detection (PLUGIN_ROW_USES_SENTINEL_ID). Add `WithPluginRepo` ManagerOption.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md § Bootstrap, § IdentityRegistry interface
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 5

**TDD acceptance criteria:**
- TestManagerNameByIDResolvesSystemSentinels
- TestManagerIDByNameDoesNotResolveSentinelLabels (system labels not in activeByName)
- TestManagerNameByIDReturnsFalseForUnregisteredULID
- TestManagerBootstrapRefusesPluginRowWithSentinelULID
- TestManagerBootstrapPopulatesNameByIDFromActiveAndHistoricalRows (asymmetric semantics)

**Verification steps:**
- task test -- -run 'TestManagerNameByID|TestManagerIDByName|TestManagerBootstrap|TestIdentityRegistry' ./internal/plugin/
- task test -- ./internal/plugin/

**Files touched:**
- internal/plugin/manager.go — add cache fields + WithPluginRepo + sentinel bootstrap in NewManager + NameByID/IDByName methods
- internal/plugin/identity_registry_test.go — append cache tests + stubPluginRepo helper + newManagerForRegistryTest helper (passes WithVerbRegistry per ErrMissingVerbRegistry)

**Dependencies:** holomush-w9ml.3 (sentinels), holomush-w9ml.4 (PluginRepo), holomush-w9ml.5 (interface)

**Out of scope:** loadPlugin Upsert + hash compute (w9ml.7); UnloadPlugin (w9ml.8); GC sweep (w9ml.9).
EOF
)"

bd create \
  --title "T6: Manager hash compute + loadPlugin Upsert + drift logging" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Add `Manager.computeHashes` (sha256 manifest + per-Type content hash); integrate Upsert + cache mutation + drift logging into existing `loadPlugin` (manager.go:673). Cache mutation runs BEFORE host.Load so downstream emit-during-Load can resolve via IDByName. Use `var loadPluginCommitted bool` flag for deferred rollback (loadPlugin returns bare error, not named); set flag true at the final `return nil` of loadPlugin.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md § Lifecycle integration
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 6

**TDD acceptance criteria:**
- TestComputeHashesProducesNonEmptyForBinary (sha256, 32 bytes)
- TestComputeHashesNilContentForSettingPlugin
- TestComputeHashesLuaIsDeterministicAndOrderIndependent

**Verification steps:**
- task test -- -run 'TestComputeHashes' ./internal/plugin/
- task test -- ./internal/plugin/

**Files touched:**
- internal/plugin/manager.go — add computeHashes; integrate Upsert + cache + drift log + rollback flag in loadPlugin around lines 673-1000

**Dependencies:** holomush-w9ml.6 (cache fields + WithPluginRepo + bootstrap exist)

**Out of scope:** UnloadPlugin (w9ml.8); GC sweep (w9ml.9); hash-drift decision logic (deferred — log-only per spec).
EOF
)"

bd create \
  --title "T7: Manager.UnloadPlugin (new method) + cache mutation" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Add `Manager.UnloadPlugin(ctx, name) error` as a new exported method (does not exist pre-w9ml; current code uses inline `host.Unload` calls in rollback paths). Cache cleanup (`delete(m.activeByName, name)`) runs FIRST and unconditionally — decoupled from host lifecycle, idempotent, safe even when no host is registered. Host unload + policy removal run only if a host is registered for the name.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md § Lifecycle integration (unload path)
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 7

**TDD acceptance criteria:**
- TestUnloadPluginRemovesActiveButPreservesHistorical (activeByName cleared; nameByID retained)
- TestUnloadPluginIsIdempotentWhenNotLoaded (no error on never-loaded name)

**Verification steps:**
- task test -- -run 'TestUnloadPlugin' ./internal/plugin/

**Files touched:**
- internal/plugin/manager_unload.go — new (UnloadPlugin method)

**Dependencies:** holomush-w9ml.6 (cache fields exist)

**Out of scope:** existing inline host.Unload call sites in manager.go rollback paths (manager.go:777, 793, 822, 913, 924, 948) remain unchanged; they're test-skip rollbacks, not orderly unloads.
EOF
)"

bd create \
  --title "T8: GC sweep at LoadAll end + RetentionDays config" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Add `WithRetentionDays(int) ManagerOption` (default 3, 0 = sweep disabled). Call `repo.SweepInactive` at end of `Manager.LoadAll` AFTER all per-plugin loads have refreshed last_seen_at (INV-W9ML-8). For each swept row: clear from activeByName (nameByID retained); emit `plugin.gc` structured log.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md § Garbage collection, INV-W9ML-8
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 8

**TDD acceptance criteria:**
- TestSweepInactiveRemovesFromActiveByNameRetainsNameByID
- TestRetentionDaysZeroDisablesSweep

**Verification steps:**
- task test -- -run 'TestSweepInactive|TestRetentionDays' ./internal/plugin/

**Files touched:**
- internal/plugin/manager.go — add retentionDays field + WithRetentionDays option + sweep block at LoadAll end
- internal/plugin/config.go (or wherever PluginConfig lives — verify location at impl time) — add `RetentionDays int` field

**Dependencies:** holomush-w9ml.6, holomush-w9ml.7 (cache + load path established before sweep can decide what's stale)

**Out of scope:** plugin row deletion (forbidden by INV-W9ML-9; soft-delete via gc_at only); long-tail GC of historical rows (future epic).
EOF
)"

bd create \
  --title "T9: plugin emit stamp sites — IDByName at goplugin/host.go (4 sites)" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Replace the four `core.Actor{Kind: ActorPlugin, ID: name}` stamp sites in internal/plugin/goplugin/host.go (lines 560, 566, 631, 637) with `stampPluginActor(registry, name)` calls that resolve via `IdentityRegistry.IDByName`. Inject IdentityRegistry into Host struct. PLUGIN_UNREGISTERED_INVOKE on missing plugin. Lua plugins inherit via cascade through actorFromContext — no Lua-side stamp site needed (per spec § Stamp-site changes).

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md § Stamp-site changes; CLAUDE.md "Plugin Runtime Symmetry"
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 9

**TDD acceptance criteria:**
- TestStampPluginActorSucceedsForRegisteredPlugin (stub registry, ULID-string ID)
- TestStampPluginActorFailsForUnregistered (PLUGIN_UNREGISTERED_INVOKE)

**Verification steps:**
- task test -- -run 'TestStampPluginActor' ./internal/plugin/goplugin/
- task test -- ./internal/plugin/goplugin/

**Files touched:**
- internal/plugin/goplugin/host.go — 4 stamp sites at lines 560, 566, 631, 637; add identityRegistry field + stampPluginActor helper
- internal/plugin/goplugin/host_actor_stamp_test.go — new

**Dependencies:** holomush-w9ml.6 (registry available for IDByName)

**Out of scope:** Lua subscriber actor reconstruction (subscriber.go:159 — reconstruction site, not stamp site; receives ULID from upstream per spec); system stamp sites (w9ml.11).
EOF
)"

bd create \
  --title "T10: system actor stamp site migrations (4 sites)" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Migrate 4 system-actor stamp sites from non-ULID string labels to sentinel ULID constants. Three direct edits + one no-source-change site (engine_end_session.go uses ActorSystemID, whose value changed in w9ml.3).

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md § System actor sentinel ULIDs
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 10

**TDD acceptance criteria:**
- TestEventStoreAdapterDoesNotUseStringWorldServiceLabel
- TestGrpcServerDoesNotUseStringSystemLabel (regex scoped to actor-stamp lines)
- TestCommandTypesDoesNotUseStringSystemLabel

**Verification steps:**
- task test -- -run 'TestEventStoreAdapter|TestGrpcServer|TestCommandTypes' ./internal/core/
- task test -- ./internal/world/ ./internal/grpc/ ./internal/command/ ./internal/core/

**Files touched:**
- internal/world/event_store_adapter.go:34-37 — `ID: "world-service"` → `ID: core.WorldServiceActorULID.String()`
- internal/grpc/server.go:531-534 — `ID: "system"` → `ID: core.ActorSystemID`
- internal/command/types.go:619-622 — same pattern
- internal/core/engine_end_session.go:56 — no source change (constant value changed in w9ml.3)
- internal/core/no_string_system_stamps_test.go — new (per-file forbidden-string checks)

**Dependencies:** holomush-w9ml.3 (sentinels declared)

**Out of scope:** plugin emit stamp sites (w9ml.10); other system-actor producers introduced in future code (forward-defense by the test guard).
EOF
)"

bd create \
  --title "T11+T12+T13+T14: wire-format atomic cutover (proto + struct + headers + read fallbacks)" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Atomic removal of `legacy_id` from every layer: (a) proto field `eventbusv1.Actor.legacy_id`; (b) Go struct field `eventbus.Actor.LegacyID`; (c) JS header constant `App-Actor-Legacy-ID` + 3 publisher LegacyID branches; (d) all read-side fallback sites in subscriber/history/grpc/sub_grpc + bus conversion uniform (`coreActorToEventbusActor` parses ULID uniformly, surfaces ACTOR_ID_NOT_ULID; `coreToBusActor` simplified; `busEventToCoreEvent` LegacyID branch deleted). Build is intentionally broken between sub-steps; this bead is the atomic deliverable.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md § Wire-Format Change & Code-Path Removal; INV-W9ML-1, INV-W9ML-7
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Tasks 11, 12, 13, 14 (merged)

**TDD acceptance criteria:**
- TestProtoHasNoLegacyIDField (eventbus.proto grep == 0 hits)
- TestRegeneratedPbGoHasNoLegacyId
- TestEventbusActorStructHasNoLegacyID
- TestPublisherHasNoLegacyIDReferences
- TestNoLegacyIDReferencesInProductionCode (INV-W9ML-7 global guard — production *.go + *.proto, excluding docs/, *.pb.go, *_test.go)

**Verification steps:**
- task proto (regenerates pkg/proto/holomush/eventbus/v1/eventbus.pb.go)
- task build (must pass post-cutover)
- task test -- -run 'TestProtoHasNoLegacyIDField|TestRegeneratedPbGoHasNoLegacyId|TestEventbusActorStructHasNoLegacyID|TestPublisherHasNoLegacyIDReferences|TestNoLegacyIDReferencesInProductionCode' ./internal/eventbus/
- task test -- ./internal/eventbus/ ./internal/grpc/ ./internal/plugin/ ./cmd/holomush/

**Files touched:**
- api/proto/holomush/eventbus/v1/eventbus.proto:23-30 — delete legacy_id (no `reserved`)
- pkg/proto/holomush/eventbus/v1/eventbus.pb.go — regenerated
- internal/eventbus/types.go:53-57 — delete LegacyID field + doc
- internal/eventbus/publisher.go:50-55, 316-317, 351, 431-432 — 4 deletions
- internal/eventbus/subscriber.go:770-773 — delete out.LegacyID = legacy fallback
- internal/eventbus/history/hot_jetstream.go:592-598 — delete actorFromProto LegacyID branch
- internal/eventbus/history/cold_postgres.go:417-440 — delete TODO(holomush-u5bb) block + LegacyID handling in actorFromAuditRow
- cmd/holomush/sub_grpc.go:499-511 — coreToBusActor simplified (no LegacyID branch)
- cmd/holomush/sub_grpc.go:600-607 — busEventToCoreEvent LegacyID branch deleted
- internal/plugin/event_emitter.go:310-318 — coreActorToEventbusActor uniform ULID parse + ACTOR_ID_NOT_ULID
- internal/eventbus/proto_legacy_id_grep_test.go — new (proto + .pb.go + struct + publisher grep guards)
- internal/eventbus/no_legacy_id_grep_test.go — new (INV-W9ML-7 global guard)

**Dependencies:** holomush-w9ml.10 (plugin stamps must produce ULIDs first; otherwise the bus conversion fires ACTOR_ID_NOT_ULID for every plugin emit)

**Out of scope:** actor display name resolution (w9ml.14 — actorIDString consumes the ULID side, separate review surface); test fixtures that hand-construct LegacyID actors (w9ml.13 — fixture migration depends on the struct being gone first); INV-49 retarget (w9ml.15).
EOF
)"

bd create \
  --title "T15: plugintest helper + fixture migration (~15 files)" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Create `plugintest.PluginULIDFromName(name)` deterministic helper (sha256(name)[:16] reinterpreted as ULID; fixture-only — production uses `idgen.New()` via repo.Upsert). Migrate ~15 test files from `LegacyID: "X"` patterns to `ID: plugintest.PluginULIDFromName("X").Bytes()` (or `.String()` for core.Actor); delete LegacyID-specific tests whose subject behavior no longer exists.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md § Test fixture impact
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 15

**TDD acceptance criteria:**
- existing test suite passes after migration (no new tests; this task maintains coverage by translation)

**Verification steps:**
- task test -- ./...

**Files touched:**
- internal/plugin/plugintest/registry.go — new (PluginULIDFromName)
- cmd/holomush/sub_grpc_adapters_test.go (lines 154-236; delete TestCoreToBusActorStashesNonULIDAsLegacyID + TestBusEventToCoreEventFallsBackToLegacyID + assert.Empty(LegacyID) cases)
- internal/eventbus/publisher_test.go:284-299, 350 (delete LegacyID fallback cases)
- internal/eventbus/actor_conversion_test.go (delete LegacyID branches)
- internal/eventbus/history/actor_from_envelope_test.go:47-67 (delete TestActorFromEnvelopeFallsBackToLegacyID)
- internal/eventbus/history/cold_postgres_test.go:23-59 (retarget Decision 5 lock to ULID round-trip)
- internal/plugin/event_emitter_crypto_test.go:40, 67
- internal/plugin/event_emitter_test.go:47, 146, 524, 554, 583
- internal/plugin/manager_routing_test.go:192, 332
- internal/plugin/manager_test.go:1950
- test/integration/eventbus_e2e/cross_tier_query_test.go:488 (drop LegacyId arg)
- test/integration/plugin/actor_authentication_test.go:164, 174 (remove ActorLegacyID field)

**Dependencies:** holomush-w9ml.12 (LegacyID gone from struct; can't "migrate" tests that still reference a present field)

**Out of scope:** INV-49 retarget (w9ml.15); E2E scenario (w9ml.19) — those reference plugintest.PluginULIDFromName but are scoped to their own beads.
EOF
)"

bd create \
  --title "T16: actorIDString — IdentityRegistry display resolution" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Rewrite `actorIDString` (internal/grpc/server.go:596-608) to take an `IdentityRegistry` parameter; resolve plugin/system ULIDs to display names via `NameByID`; preserve the zero-ULID guard for unspecified actors (returns ""). Apply the same treatment to `eventbusEventToEventFrame` in query_stream_history.go. Wire IdentityRegistry into CoreServer construction.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md § Rendering integration (gRPC display path; telnet/web are downstream consumers per CLAUDE.md "Gateway Boundary")
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 16

**TDD acceptance criteria:**
- TestActorIDStringResolvesPluginNameViaRegistry
- TestActorIDStringResolvesSystemSentinelViaRegistry
- TestActorIDStringFallsBackToULIDStringForCharacter (character ULIDs not in registry; ULID-string returned verbatim)
- TestActorIDStringPreservesZeroULIDGuard ("" for ActorKindUnknown)

**Verification steps:**
- task test -- -run 'TestActorIDString' ./internal/grpc/
- task test -- ./internal/grpc/ ./cmd/holomush/

**Files touched:**
- internal/grpc/server.go:596-608 — rewrite actorIDString + new IdentityRegistry param + add identityRegistry field on CoreServer
- internal/grpc/query_stream_history.go:512-522 — eventbusEventToEventFrame LegacyID fallback removed; route through IdentityRegistry
- internal/grpc/actor_display_test.go — new
- cmd/holomush/main.go (or wherever CoreServer is constructed) — wire IdentityRegistry (Manager) into CoreServer

**Dependencies:** holomush-w9ml.6 (registry available for NameByID)

**Out of scope:** internal/telnet/ and internal/web/ — gateway is a pass-through per CLAUDE.md "Gateway Boundary" and consumes the gRPC ActorId string verbatim; no telnet/web code change needed.
EOF
)"

bd create \
  --title "T17: INV-49 retarget (Phase 3d Decision 5 lock test)" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Retarget the Phase 3d INV-49 lock test from LegacyID round-trip to Actor.id ULID round-trip. Three It-blocks in inv49_envelope_roundtrip_test.go (lines 59, 111, 165) updated; e2e_test.go:647 parallel block deleted; publishSensitiveWithLegacyActor helper at e2e_test.go:225 + call sites deleted. AAD-divergence regression coverage from Phase 3d Decision 5 stays — only the identity field changes.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md acceptance criterion 7 (lines 607-612)
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 17

**TDD acceptance criteria:**
- inv49_envelope_roundtrip_test.go:111 (renamed) asserts Actor.ID ULID round-trip via plugintest.PluginULIDFromName("core-scenes")
- inv49_envelope_roundtrip_test.go:165 useLegacyActor case branch deleted
- e2e_test.go:647 block deleted
- publishSensitiveWithLegacyActor helper + all 3 call sites deleted

**Verification steps:**
- task test:int -- -run 'INV.49|envelope_roundtrip|legacy_id' ./test/integration/crypto/...

**Files touched:**
- test/integration/crypto/inv49_envelope_roundtrip_test.go (lines 59, 111, 165)
- test/integration/crypto/e2e_test.go (delete helper at 225 and block at 647; call sites at 121, 189, 657)

**Dependencies:** holomush-w9ml.13 (plugintest helper)

**Out of scope:** other crypto tests not exercising LegacyID; AAD shape verification (proto3 zero-value safety per spec § AAD migration window).
EOF
)"

bd create \
  --title "T18: JetStream purge cutover command + operator runbook" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** One-time deploy command `holomush-cutover` that runs PG `TRUNCATE events_audit` + JetStream `PurgeStream` (so pre-cutover encrypted plugin-actor messages with old-AAD bytes don't survive into the post-cutover process). Add Taskfile entry `migrate:plugin-actors-cutover`. Operator runbook in site/docs/operating/.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md § JetStream purge, § AAD migration window
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 18

**TDD acceptance criteria:**
- smoke test (manual run against local PG + NATS dev environment)

**Verification steps:**
- task migrate:plugin-actors-cutover (against local dev env; expect both ops log success)

**Files touched:**
- cmd/holomush-cutover/main.go — new
- Taskfile.yaml — add migrate:plugin-actors-cutover entry
- site/docs/operating/legacy-id-cutover.md — new (operator runbook)

**Dependencies:** none (independent runtime; not loaded by holomush server)

**Out of scope:** bootstrap orphan check (w9ml.17 — separate runtime entry point); long-term audit retention coordination (future operator concern).
EOF
)"

bd create \
  --title "T19: bootstrap orphan check (PLUGIN_ACTOR_ORPHAN_DETECTED)" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Bootstrap step `runBootstrapOrphanCheck` runs SELECT COUNT(*) FROM events_audit WHERE actor_kind = 'plugin' AND actor_id IS NULL (using `eventbus.ActorKindPlugin.String()` constant per publisher.go:409). On count > 0, refuse to start with PLUGIN_ACTOR_ORPHAN_DETECTED. Defense-in-depth: migration 000018 makes orphans impossible from a clean install, but this guards against manual restore from old backup or partial migration recovery.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md § Migration ordering invariants
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 19

**TDD acceptance criteria:**
- TestBootstrapPassesWithCleanEventsAudit
- TestBootstrapFailsWithSyntheticOrphan (synthesized orphan row → PLUGIN_ACTOR_ORPHAN_DETECTED)

**Verification steps:**
- task test:int -- -run TestBootstrap ./cmd/holomush/

**Files touched:**
- cmd/holomush/main.go — add runBootstrapOrphanCheck + wire into bootstrap sequence (after migrations run, before Manager construction)
- cmd/holomush/bootstrap_orphan_test.go — new (package main, integration build tag)

**Dependencies:** holomush-w9ml.2 (migration must exist; events_audit must be queryable)

**Out of scope:** cutover command (w9ml.16 — separate cmd binary); DELETE FROM plugins guard (w9ml.18 — different invariant).
EOF
)"

bd create \
  --title "T20: CI guard — no DELETE FROM plugins (INV-W9ML-9)" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** TestNoDeleteFromPluginsInCodebase runs `git grep -nE 'DELETE\s+FROM\s+plugins\b' -- '*.go'`; asserts zero matches. Static guard for INV-W9ML-9. Runs as part of `task test`; surfaces in CI.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md INV-W9ML-9 (no deletion)
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 20

**TDD acceptance criteria:**
- TestNoDeleteFromPluginsInCodebase

**Verification steps:**
- task test -- -run TestNoDeleteFromPluginsInCodebase ./internal/store/

**Files touched:**
- internal/store/no_delete_grep_test.go — new

**Dependencies:** none

**Out of scope:** INV-W9ML-7 (no LegacyID references) — that guard is in w9ml.12. Forward-defense against future code; no current sites to remove.
EOF
)"

bd create \
  --title "T21: E2E plugin-actor ULID round-trip scenario (cross_tier_query_test extension)" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Add `scenario_w9ml_plugin_actor_ulid_round_trip` to existing TestCrossTierQueryEndToEnd (test/integration/eventbus_e2e/cross_tier_query_test.go:51). Reuses the test file's existing harness (eventbustest.New, freshPool, mintAt, publishAll, buildReader, drainStream, AwaitStreamLastSeq) — no new harness scaffolding. Publishes a plugin-actor event with a ULID via plugintest.PluginULIDFromName, asserts the ULID round-trips through hot tier.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md acceptance criterion 5 (end-to-end happy path)
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 21

**TDD acceptance criteria:**
- TestCrossTierQueryEndToEnd/scenario_w9ml_plugin_actor_ulid_round_trip (asserts Actor.Kind == ActorKindPlugin && Actor.ID == plugintest.PluginULIDFromName("core-scenes"))

**Verification steps:**
- task test:int -- -run 'TestCrossTierQueryEndToEnd/scenario_w9ml_plugin_actor_ulid_round_trip' ./test/integration/eventbus_e2e/

**Files touched:**
- test/integration/eventbus_e2e/cross_tier_query_test.go — append new scenario entry to scenarios slice

**Dependencies:** holomush-w9ml.10 (stamp sites produce ULIDs), holomush-w9ml.12 (wire-format), holomush-w9ml.13 (plugintest helper)

**Out of scope:** cold-tier round-trip (w9ml.15 INV-49 retarget covers encrypted cold path); display-name resolution (w9ml.14 unit tests cover NameByID); E2E test for binary-plugin-via-gRPC subprocess emit (existing harness handles eventbus events, not subprocess plugin emits — out of scope for this bead).
EOF
)"

bd create \
  --title "T22: final gate + bead closures + land the plane (closing bead)" \
  --type=task --priority=1 --parent=holomush-w9ml \
  --description "$(cat <<'EOF'
**Goal:** Closing bead. Run `task pr-prep` against the full final state (mirrors all CI jobs). Verify INV-W9ML-7 grep guards return zero; INV-W9ML-9 grep guard returns zero. Close holomush-ojw1.8 (proposed adding LegacyID; w9ml inverts that direction) and holomush-u5bb (proposed actor_legacy_id column on events_audit; w9ml eliminates LegacyID entirely) as superseded with cross-reference notes. Rebase via `jj rebase -s 'all:roots(trunk()..@)' -d main@origin --skip-emptied`. Push via `jj git push --branch legacy-id-elimination`. Create PR.

**Design reference:** docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md § PR Scope, § Out of Scope, INV-W9ML-6
**Plan reference:** docs/superpowers/plans/2026-05-04-legacy-id-elimination.md § Task 22

**TDD acceptance criteria:**
- task pr-prep produces green output (lint + format + schema + license + unit + integration + E2E)
- git grep guards return zero matches (INV-W9ML-7, INV-W9ML-9)
- holomush-ojw1.8 closed with cross-reference note
- holomush-u5bb closed with cross-reference note

**Verification steps:**
- task pr-prep
- git grep -E '\bLegacyID\b|\blegacy_id\b|App-Actor-Legacy-ID' -- '*.go' '*.proto' ':!docs/' ':!*.pb.go'  # zero
- grep -L 'LegacyId' pkg/proto/holomush/eventbus/v1/eventbus.pb.go  # path printed (means absent)
- git grep -nE 'DELETE\s+FROM\s+plugins\b' -- '*.go'  # zero
- bd show holomush-ojw1.8 (status=closed, has w9ml cross-reference note)
- bd show holomush-u5bb (status=closed, has w9ml cross-reference note)

**Files touched:** none (gate + housekeeping only; possible last-mile pr-prep tweaks may add a single chore commit)

**Dependencies:** holomush-w9ml.1 through holomush-w9ml.19 (every prior bead in the chain)

**Out of scope:** any new code change; bead closure for holomush-w9ml itself happens after PR squash-merge (not in this bead).
EOF
)"
```

### Closing-out operations

**Existing beads to close at execution time** (handled inside w9ml.20):

- **holomush-ojw1.8** (Phase 3d follow-up: propagate Actor.LegacyID through plugin actor resolver) — close as superseded. Rationale: ojw1.8 proposed adding `core.Actor.LegacyID` to plumb plugin names through the actor resolver; w9ml inverts that direction by eliminating LegacyID at every layer.
- **holomush-u5bb** (persist Actor.LegacyID in events_audit) — close as superseded. Rationale: u5bb proposed an `actor_legacy_id` column on `events_audit`; w9ml eliminates LegacyID entirely so the column is unnecessary.

**Existing bead to close after PR squash-merge** (manual, post-merge):

- **holomush-w9ml** itself — close once the PR lands.

**Follow-up beads to file** (after w9ml ships; surfaced in spec § Out of Scope):

- First-class plugin rename support (`previous_names` JSONB or `plugin_aliases` table)
- Per-event hash stamping for tamper forensics (`plugin_revisions` join table or per-event hash column)
- Hash-based plugin trust enforcement (build on `content_hash` drift signal)

### `bd dep add` edges

```bash
# Foundation
bd dep add holomush-w9ml.2 holomush-w9ml.1   # T1 needs test harness
bd dep add holomush-w9ml.4 holomush-w9ml.1   # T3 needs harness
bd dep add holomush-w9ml.4 holomush-w9ml.2   # T3 needs migration

# Manager integration (T5/6/7/8 chain)
bd dep add holomush-w9ml.6 holomush-w9ml.3   # T5 needs sentinels
bd dep add holomush-w9ml.6 holomush-w9ml.4   # T5 needs PluginRepo
bd dep add holomush-w9ml.6 holomush-w9ml.5   # T5 needs IdentityRegistry interface
bd dep add holomush-w9ml.7 holomush-w9ml.6   # T6 needs cache
bd dep add holomush-w9ml.8 holomush-w9ml.6   # T7 needs cache
bd dep add holomush-w9ml.9 holomush-w9ml.6   # T8 needs cache
bd dep add holomush-w9ml.9 holomush-w9ml.7   # T8 needs Upsert path

# Stamp sites
bd dep add holomush-w9ml.10 holomush-w9ml.6  # T9 needs registry
bd dep add holomush-w9ml.11 holomush-w9ml.3  # T10 needs sentinels

# Wire-format atomic cutover
bd dep add holomush-w9ml.12 holomush-w9ml.10 # T11+T12+T13+T14 needs stamp sites producing ULIDs

# Test fixtures + display + INV-49
bd dep add holomush-w9ml.13 holomush-w9ml.12 # T15 needs LegacyID gone from struct
bd dep add holomush-w9ml.14 holomush-w9ml.6  # T16 needs registry
bd dep add holomush-w9ml.15 holomush-w9ml.13 # T17 needs plugintest helper

# Bootstrap + cutover + CI
bd dep add holomush-w9ml.17 holomush-w9ml.2  # T19 needs migration
# w9ml.16 (cutover) and w9ml.18 (CI guard) have no in-chain deps

# E2E
bd dep add holomush-w9ml.19 holomush-w9ml.10 # T21 needs stamps
bd dep add holomush-w9ml.19 holomush-w9ml.12 # T21 needs wire-format
bd dep add holomush-w9ml.19 holomush-w9ml.13 # T21 needs plugintest helper

# Closing bead
bd dep add holomush-w9ml.20 holomush-w9ml.1
bd dep add holomush-w9ml.20 holomush-w9ml.2
bd dep add holomush-w9ml.20 holomush-w9ml.3
bd dep add holomush-w9ml.20 holomush-w9ml.4
bd dep add holomush-w9ml.20 holomush-w9ml.5
bd dep add holomush-w9ml.20 holomush-w9ml.6
bd dep add holomush-w9ml.20 holomush-w9ml.7
bd dep add holomush-w9ml.20 holomush-w9ml.8
bd dep add holomush-w9ml.20 holomush-w9ml.9
bd dep add holomush-w9ml.20 holomush-w9ml.10
bd dep add holomush-w9ml.20 holomush-w9ml.11
bd dep add holomush-w9ml.20 holomush-w9ml.12
bd dep add holomush-w9ml.20 holomush-w9ml.13
bd dep add holomush-w9ml.20 holomush-w9ml.14
bd dep add holomush-w9ml.20 holomush-w9ml.15
bd dep add holomush-w9ml.20 holomush-w9ml.16
bd dep add holomush-w9ml.20 holomush-w9ml.17
bd dep add holomush-w9ml.20 holomush-w9ml.18
bd dep add holomush-w9ml.20 holomush-w9ml.19
```

---

## Self-Review Notes

**Spec coverage check:** All spec sections trace to tasks:

| Spec section | Task(s) |
|---|---|
| §Schema | T1 |
| §Sentinels + ActorSystemID | T2 |
| §Repository | T3 |
| §IdentityRegistry interface | T4 |
| §Lifecycle integration (loadPlugin) | T5, T6 |
| §UnloadPlugin (NEW — was missing in spec) | T7 |
| §Bootstrap | T5 |
| §Garbage collection | T8 |
| §Stamp-site changes (plugin) | T9 |
| §Stamp-site changes (system) | T10 |
| §Proto schema | T11 |
| §In-memory eventbus.Actor struct | T12 |
| §JetStream header | T13 |
| §Read/fallback paths + bus conversion | T14 |
| §Test fixture impact | T15 |
| §Actor display path (NEW — was misplaced as "renderer") | T16 |
| §Phase 3d INV-49 retarget | T17 |
| §JetStream purge | T18 |
| §Migration ordering invariants (orphan check) | T19 |
| INV-W9ML-9 CI guard | T20 |
| INV-W9ML-7 CI guard | (built into T14) |
| §Acceptance criterion 5 (E2E) | T21 |
| §Acceptance criteria 8/10 + final gate | T22 |

**Type/method consistency check:**

- `IdentityRegistry`, `NameByID`, `IDByName`, `PluginRepo`, `Upsert`, `ListAll`, `SweepInactive`, `DriftReport`, `PluginUpsertInput`, `PluginRow`, `SystemActorULID`, `WorldServiceActorULID`, `ActorSystemID`, `IsSentinelULID`, `coreToBusActor`, `coreActorKindToBus`, `busActorKindToCore`, `coreActorToEventbusActor`, `actorIDString`, `eventbusEventToEventFrame` — all verified against repo at planning time.
- `Manifest.Type` (not `Runtime`), `Manifest.BinaryPlugin.Executable` (not `Binary.Path`), `Manifest.LuaPlugin.Entry`, constants `TypeLua`/`TypeBinary`/`TypeSetting`.
- `core.ActorKind` has 3 values (no `ActorUnknown`/`ActorPlayer`); `eventbus.ActorKind` has 5. Plan acknowledges the asymmetry.
- `Manager.UnloadPlugin` is NEW (added in T7), not preexisting.

**Placeholder scan:** No `TBD`, `TODO`, "implement later" patterns. Where the implementer needs to read the surrounding context (e.g., T9 step 4 says "verify by `rg`"), the verification step is concrete and not a substitute for plan content.

**JJ cadence check:** Every task ends with `jj describe` + `jj new` except T22 (final). The discipline prevents commit collapse (reviewer Finding 10).

**Reviewer Findings 1-15 status:**

| # | Status |
|---|---|
| 1 (`core.ActorUnknown`) | RESOLVED — removed; bus conversion uses `ID == ""` gate |
| 2 (`Manifest.Runtime`/`Binary.Path`) | RESOLVED — uses real fields `Type` / `BinaryPlugin.Executable` |
| 3 (`Manager.UnloadPlugin`) | RESOLVED — new task T7 creates it |
| 4 (`coreActorToBusActor` wrong name) | RESOLVED — uses `coreToBusActor`, `coreActorKindToBus` |
| 5 (`newTestPool`/`runMigrations`) | RESOLVED — Task 0.5 creates them |
| 6 (test helpers) | RESOLVED — replaced fictional helpers with concrete inline patterns |
| 7 (telnet/web renderer) | RESOLVED — T16 redirects to `actorIDString` + `eventbusEventToEventFrame` |
| 8 (`unsafePointer` placeholder) | RESOLVED — T3 uses proper `[]byte` + `copy(...)` pattern |
| 9 (`task proto:gen`) | RESOLVED — uses `task proto` |
| 10 (jj cadence) | RESOLVED — every task ends with `jj describe` + `jj new` |
| 11 (rebase scope) | RESOLVED — T22 uses `jj rebase -s 'all:roots(trunk()..@)' -d main@origin` |
| 12 (T9 multi-line grep) | RESOLVED — T10 uses per-file forbidden-string checks |
| 13 (`plugintest` circular import) | RESOLVED — T15 keeps only `PluginULIDFromName` (no repo dependency) |
| 14 (T16 `package main_test`) | RESOLVED — T19 uses `package main` |
| 15 (`actorIDString` zero guard) | RESOLVED — T16 preserves the zero-ULID guard explicitly |
