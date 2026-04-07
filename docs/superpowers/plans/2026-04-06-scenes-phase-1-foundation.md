# Scenes Phase 1: Foundation Slice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish the `core-scenes` binary plugin with `CreateScene` and `GetScene` RPCs, ABAC `AttributeResolverService` for the `scene` resource type, command handlers for `scene create` and `scene info`, and a Ginkgo integration test that proves the entire stack works end-to-end.

**Architecture:** Binary plugin (`plugins/core-scenes/`) following the pattern established by `plugins/test-abac-widget/`. Plugin owns its Postgres schema (`plugin_core_scenes`), implements `holomush.scene.v1.SceneService` and `holomush.plugin.v1.AttributeResolverService`, declares Cedar policies in its manifest, and is gated by the host's two-layer ABAC.

**Tech Stack:** Go 1.24+, pgx/v5, hashicorp go-plugin, OpenTelemetry tracing (global tracer), slog structured logging, Ginkgo/Gomega, testcontainers-go.

**Spec reference:** [`docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md`](../specs/2026-04-06-scenes-and-rp-design-v2.md)

**Bead reference:** `holomush-5rh.10`

**Workspace:** `scene-rewrite` jj workspace at `/Users/sean/Code/github.com/holomush/.worktrees/scene-rewrite`

**Reference for old implementation:** `scene-system` jj bookmark. Read files via `jj file show -r scene-system <path>`.

**O11y scope for Phase 1 (per spec section 10):** structured slog logging at all boundaries; OTel tracing via the global tracer (no-op if not configured at process level). Prometheus metrics deferred — see spec section 11 architectural gap.

---

## File Structure

| File | Status | Responsibility |
|------|--------|----------------|
| `internal/plugin/manifest.go` | Modify | Remove `"scene"` from `ProtectedResourceTypes` |
| `internal/plugin/manifest_test.go` | Modify | Add ACE test verifying scene is no longer protected |
| `plugins/core-scenes/plugin.yaml` | Replace | v2 manifest: resource_types, AttributeResolverService provider, per-resource policy |
| `plugins/core-scenes/migrations/000001_scenes.up.sql` | Replace | Phase 1 minimum: `scenes` table only |
| `plugins/core-scenes/migrations/000001_scenes.down.sql` | Delete | Plugin migration runner ignores `.down.sql` |
| `plugins/core-scenes/types.go` | Create | `Scene` struct, state/visibility enums, validation helpers |
| `plugins/core-scenes/store.go` | Replace | `SceneStore` with `Create` + `Get` only (Phase 1 scope) |
| `plugins/core-scenes/store_test.go` | Replace | Unit tests for `SceneStore` business logic (using a mock pool) |
| `plugins/core-scenes/store_integration_test.go` | Replace | Testcontainers-backed store tests |
| `plugins/core-scenes/service.go` | Replace | `SceneServiceImpl` with `CreateScene` + `GetScene` only |
| `plugins/core-scenes/service_test.go` | Replace | Unit tests for service layer using a fake store |
| `plugins/core-scenes/resolver.go` | Create | `AttributeResolverServiceServer` implementation |
| `plugins/core-scenes/resolver_test.go` | Create | Unit tests for resolver |
| `plugins/core-scenes/commands.go` | Create | `HandleCommand` dispatcher + `scene create` and `scene info` handlers |
| `plugins/core-scenes/commands_test.go` | Create | Unit tests for command handlers |
| `plugins/core-scenes/main.go` | Replace | Plugin entry point: `Init`, `RegisterServices`, `RegisterAttributeResolver` |
| `plugins/core-scenes/observability.go` | Create | Tracer + logger helpers used across the plugin |
| `test/integration/plugin/core_scenes_test.go` | Create | Ginkgo integration test mirroring `abac_widget_test.go` |

---

## Task 0: Read existing plugins/core-scenes and delete obsolete files

The existing `plugins/core-scenes/*` files on `main` are PR #192's stub. Per the brainstorm decision, they are throwaway — useful only if a specific file happens to match what we'd write anyway. Tasks below replace them. This task removes the files we know we don't need.

**Files:**

- Delete: `plugins/core-scenes/migrations/000001_scenes.down.sql`

- [ ] **Step 1: Delete the down migration file**

```bash
rm plugins/core-scenes/migrations/000001_scenes.down.sql
```

- [ ] **Step 2: Verify deletion**

Run: `ls plugins/core-scenes/migrations/`
Expected: only `000001_scenes.up.sql` shown (will be replaced in Task 3).

- [ ] **Step 3: Commit**

Run:

```bash
jj --no-pager describe -m "chore(scenes): remove unused down migration

Plugin migration runner only applies .up.sql files; .down.sql is ignored.
Removing the file to avoid suggesting rollback support that doesn't exist."
jj --no-pager new -m "(working: scenes phase 1 sub-task #1)"
```

---

## Task 1: Remove `scene` from `ProtectedResourceTypes`

The `scene` type was protected when it lived in the server core. Now that the plugin owns it, the protection is wrong — plugins must be able to declare `resource_types: [scene]`.

**Files:**

- Modify: `internal/plugin/manifest.go:48-52`
- Modify: `internal/plugin/manifest_test.go` (add test)

- [ ] **Step 1: Write the failing test**

Open `internal/plugin/manifest_test.go`. Find an existing test that exercises `ProtectedResourceTypes` (e.g., `TestParseManifestResourceTypesAndTrust` from PR #195's manifest test suite). Add a new top-level test:

```go
func TestSceneResourceTypeIsNotProtected(t *testing.T) {
    // Why: scenes are owned by the core-scenes plugin (Epic 9 v2), not the
    // server core. The plugin must be able to declare resource_types: [scene]
    // without trust escalation. See spec
    // docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md section 5.1.
    if plugins.ProtectedResourceTypes["scene"] {
        t.Fatal("scene MUST NOT be in ProtectedResourceTypes — owned by core-scenes plugin")
    }
}
```

If the test file uses package `plugins_test` with named imports, adjust the reference accordingly. Confirm the package name by looking at the existing tests in `manifest_test.go`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test -- -run TestSceneResourceTypeIsNotProtected ./internal/plugin/`
Expected: FAIL with message "scene MUST NOT be in ProtectedResourceTypes" (because `"scene": true,` still exists at line 50).

- [ ] **Step 3: Remove `"scene"` from the protected list**

Open `internal/plugin/manifest.go`. Find lines 48-52:

```go
var ProtectedResourceTypes = map[string]bool{
    "character": true, "location": true, "exit": true, "object": true,
    "stream": true, "property": true, "scene": true, "command": true,
    "system": true, "server": true, "player": true,
}
```

Replace with (remove `"scene": true,`):

```go
var ProtectedResourceTypes = map[string]bool{
    "character": true, "location": true, "exit": true, "object": true,
    "stream": true, "property": true, "command": true,
    "system": true, "server": true, "player": true,
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `task test -- -run TestSceneResourceTypeIsNotProtected ./internal/plugin/`
Expected: PASS.

- [ ] **Step 5: Run the broader manifest test suite to verify no regressions**

Run: `task test -- ./internal/plugin/`
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
jj --no-pager describe -m "fix(plugin): remove 'scene' from ProtectedResourceTypes

The scene resource type was protected when scenes lived in the server
core. With the v2 plugin architecture, scenes are owned by the core-scenes
binary plugin and the plugin MUST be able to declare resource_types: [scene]
without trust escalation.

Adds TestSceneResourceTypeIsNotProtected to lock the change in.

Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md section 5.1
Bead: holomush-5rh.10"
jj --no-pager new -m "(working: scenes phase 1 — plugin manifest)"
```

---

## Task 2: Replace `plugins/core-scenes/plugin.yaml` with v2 manifest

The existing manifest (PR #192) is missing `resource_types`, the AttributeResolverService provider, and the per-resource read-own-scene policy.

**Files:**

- Replace: `plugins/core-scenes/plugin.yaml`

- [ ] **Step 1: Write the new manifest**

Replace the entire contents of `plugins/core-scenes/plugin.yaml` with:

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

name: core-scenes
version: 1.0.0
type: binary
resource_types: [scene]

requires:
  - holomush.world.v1.WorldService

provides:
  - holomush.scene.v1.SceneService
  - holomush.plugin.v1.AttributeResolverService

storage: postgres

binary-plugin:
  executable: core-scenes

commands:
  - name: scene
    capabilities:
      - action: write
        resource: scene
        scope: local
    help: "Manage RP scenes"
    usage: "scene <subcommand> [args]"
  - name: scenes
    help: "Browse open scenes"
    usage: "scenes [--tags tag1,tag2]"

policies:
  - name: execute-scene-commands
    dsl: >-
      permit(principal is character, action in ["execute"], resource is command)
      when { resource.command.name in ["scene", "scenes"] };
  - name: read-own-scene
    dsl: >-
      permit(principal is character, action in ["read"], resource is scene)
      when { resource.scene.owner == principal.id };
```

- [ ] **Step 2: Verify the manifest parses**

Run: `task test -- -run TestParseManifest ./internal/plugin/`
Expected: PASS. (The manifest parser tests don't validate this specific file, but they exercise the parser; if the file has a YAML syntax error, parsing-related tests will fail.)

- [ ] **Step 3: Validate the YAML against the schema**

Run: `task pr-prep -- schema` if a schema validation step exists; otherwise:

```bash
task lint
```

Expected: No errors related to plugin.yaml.

- [ ] **Step 4: Commit**

```bash
jj --no-pager describe -m "feat(scenes): declare AttributeResolverService and per-resource policy

Update plugins/core-scenes/plugin.yaml to v2:
- declare resource_types: [scene]
- provide holomush.plugin.v1.AttributeResolverService
- add per-resource read-own-scene policy that exercises the resolver

Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md sections 5.3, 9.1
Bead: holomush-5rh.10"
jj --no-pager new -m "(working: scenes phase 1 — migration)"
```

---

## Task 3: Trim `000001_scenes.up.sql` to Phase 1 minimum (`scenes` table only)

The existing migration creates four tables. Phase 1 only needs `scenes`. The other tables come back in later phases.

**Files:**

- Replace: `plugins/core-scenes/migrations/000001_scenes.up.sql`

- [ ] **Step 1: Replace the migration content**

Replace the entire contents of `plugins/core-scenes/migrations/000001_scenes.up.sql` with:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 1 schema: scenes table only.
-- Subsequent migrations add scene_participants (Phase 3),
-- scene_logs (Phase 6), and scene_templates (Phase 7).

CREATE TABLE IF NOT EXISTS scenes (
    id               TEXT        PRIMARY KEY,
    title            TEXT        NOT NULL,
    description      TEXT        NOT NULL DEFAULT '',
    location_id      TEXT,
    owner_id         TEXT        NOT NULL,
    state            TEXT        NOT NULL DEFAULT 'active',
    pose_order       TEXT        NOT NULL DEFAULT 'free',
    visibility       TEXT        NOT NULL DEFAULT 'open',
    idle_timeout_secs INTEGER,
    template_id      TEXT,
    content_warnings TEXT[]      NOT NULL DEFAULT '{}',
    tags             TEXT[]      NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at         TIMESTAMPTZ,
    archived_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_scenes_state ON scenes(state);
CREATE INDEX IF NOT EXISTS idx_scenes_owner ON scenes(owner_id);
CREATE INDEX IF NOT EXISTS idx_scenes_location ON scenes(location_id) WHERE location_id IS NOT NULL;
```

Note: `idx_scenes_owner` is added to support the Phase 1 ABAC policy `read-own-scene` which will frequently filter by owner. `idx_scenes_state` and `idx_scenes_location` support later phase queries.

- [ ] **Step 2: Verify no compile errors anywhere yet**

Run: `task lint`
Expected: No errors related to the plugin migration. There may be Go build errors in `plugins/core-scenes/store.go` if it references columns that no longer exist (it shouldn't — store.go also references the `scenes` table fields). If there are unrelated build errors at this point, that's fine — they'll be fixed in later tasks.

- [ ] **Step 3: Commit**

```bash
jj --no-pager describe -m "feat(scenes): Phase 1 schema — scenes table only

Trim plugins/core-scenes/migrations/000001_scenes.up.sql to the minimum
schema needed for Phase 1: a single scenes table with indexes for state,
owner_id (supports the Phase 1 read-own-scene ABAC policy), and location_id.

scene_participants, scene_logs, and scene_templates are added in Phases 3, 6, and 7 via subsequent migrations.

Bead: holomush-5rh.10"
jj --no-pager new -m "(working: scenes phase 1 — types)"
```

---

## Task 4: Create `plugins/core-scenes/types.go` with Phase 1 domain types

Phase 1 needs the `Scene` struct and the enums it uses. Other types (templates, logs, participants) come in later phases.

**Files:**

- Create: `plugins/core-scenes/types.go`
- Create: `plugins/core-scenes/types_test.go`

- [ ] **Step 1: Write the failing tests**

Create `plugins/core-scenes/types_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import "testing"

func TestSceneStateIsValidReturnsTrueForKnownStates(t *testing.T) {
    cases := []SceneState{
        SceneStateActive,
        SceneStatePaused,
        SceneStateEnded,
        SceneStateArchived,
    }
    for _, s := range cases {
        if !s.IsValid() {
            t.Errorf("SceneState(%q).IsValid() = false, want true", s)
        }
    }
}

func TestSceneStateIsValidReturnsFalseForUnknownState(t *testing.T) {
    if SceneState("bogus").IsValid() {
        t.Error("SceneState(\"bogus\").IsValid() = true, want false")
    }
}

func TestSceneVisibilityIsValidReturnsTrueForKnownVisibilities(t *testing.T) {
    cases := []SceneVisibility{SceneVisibilityOpen, SceneVisibilityPrivate}
    for _, v := range cases {
        if !v.IsValid() {
            t.Errorf("SceneVisibility(%q).IsValid() = false, want true", v)
        }
    }
}

func TestSceneVisibilityIsValidReturnsFalseForUnknownVisibility(t *testing.T) {
    if SceneVisibility("bogus").IsValid() {
        t.Error("SceneVisibility(\"bogus\").IsValid() = true, want false")
    }
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `task test -- -run TestSceneState ./plugins/core-scenes/`
Expected: FAIL with build error: "undefined: SceneState".

- [ ] **Step 3: Implement the types**

Create `plugins/core-scenes/types.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

// SceneState represents the lifecycle state of a scene.
//
// Per spec section 1.2, the only valid transitions are:
//   active  -> paused | ended
//   paused  -> active | ended
//   ended   -> archived
// A scene MUST NOT transition backward.
type SceneState string

// Scene state constants.
const (
    SceneStateActive   SceneState = "active"
    SceneStatePaused   SceneState = "paused"
    SceneStateEnded    SceneState = "ended"
    SceneStateArchived SceneState = "archived"
)

// IsValid reports whether s is a recognized scene state.
func (s SceneState) IsValid() bool {
    switch s {
    case SceneStateActive, SceneStatePaused, SceneStateEnded, SceneStateArchived:
        return true
    }
    return false
}

// SceneVisibility controls who can discover and join a scene.
//
// Open scenes appear on the scene board and accept any join.
// Private scenes do not appear on the board and require an invitation.
type SceneVisibility string

// Scene visibility constants.
const (
    SceneVisibilityOpen    SceneVisibility = "open"
    SceneVisibilityPrivate SceneVisibility = "private"
)

// IsValid reports whether v is a recognized scene visibility.
func (v SceneVisibility) IsValid() bool {
    switch v {
    case SceneVisibilityOpen, SceneVisibilityPrivate:
        return true
    }
    return false
}

// PoseOrderMode controls how the plugin computes pose order from the IC stream.
// Phase 1 only persists the value; pose order computation lands in Phase 4.
type PoseOrderMode string

// Pose order constants.
const (
    PoseOrderModeFree   PoseOrderMode = "free"
    PoseOrderModeStrict PoseOrderMode = "strict"
    PoseOrderMode3PR    PoseOrderMode = "3pr"
    PoseOrderMode5PR    PoseOrderMode = "5pr"
)

// IsValid reports whether m is a recognized pose order mode.
func (m PoseOrderMode) IsValid() bool {
    switch m {
    case PoseOrderModeFree, PoseOrderModeStrict, PoseOrderMode3PR, PoseOrderMode5PR:
        return true
    }
    return false
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `task test -- -run TestSceneState ./plugins/core-scenes/`
Run: `task test -- -run TestSceneVisibility ./plugins/core-scenes/`
Expected: All four tests PASS.

- [ ] **Step 5: Commit**

```bash
jj --no-pager describe -m "feat(scenes): add Phase 1 domain types

types.go defines SceneState, SceneVisibility, and PoseOrderMode enums with
IsValid helpers for validation. The full Scene struct lives in store.go's
SceneRow until Phase 2/3 split it out.

Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md section 1
Bead: holomush-5rh.10"
jj --no-pager new -m "(working: scenes phase 1 — observability)"
```

---

## Task 5: Create `plugins/core-scenes/observability.go` (tracer + logger helpers)

Plugin-side observability uses OTel global tracer (no-op when not configured) and slog. Per spec section 10, this is the foundation that Phase 1 service/store/resolver code builds on.

**Files:**

- Create: `plugins/core-scenes/observability.go`

- [ ] **Step 1: Create the observability helpers**

Create `plugins/core-scenes/observability.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "context"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"
    "go.opentelemetry.io/otel/trace"
)

// tracerName is the OTel instrumentation name for the core-scenes plugin.
// All spans are created on the tracer obtained from the global TracerProvider
// using this name. If no provider is configured at process startup, the
// global no-op tracer is returned and span operations become no-ops.
const tracerName = "github.com/holomush/holomush/plugins/core-scenes"

// startSpan starts a span on the core-scenes tracer with the given name and
// attributes. The returned context carries the span; the caller MUST defer
// span.End().
//
// Per spec section 10.1, all gRPC service entries, resolver calls, store
// operations, and lifecycle transitions emit spans. This helper is the only
// path to a span in the plugin so the instrumentation name and attribute
// conventions stay consistent.
func startSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
    tracer := otel.GetTracerProvider().Tracer(tracerName)
    return tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

// recordError marks the span as errored with the given message and sets its
// status to codes.Error. Use at the call site instead of inlining repeated
// span.RecordError + span.SetStatus pairs.
func recordError(span trace.Span, err error) {
    if err == nil {
        return
    }
    span.RecordError(err)
    span.SetStatus(codes.Error, err.Error())
}
```

- [ ] **Step 2: Verify it compiles**

Run: `task lint`
Expected: No errors in `plugins/core-scenes/observability.go`. (The OTel API imports may need to be added to `go.mod` if not already present — they should be, as PR #195 uses them.)

- [ ] **Step 3: Commit**

```bash
jj --no-pager describe -m "feat(scenes): add OTel tracing helpers

observability.go provides startSpan and recordError helpers used by the
service, resolver, store, and command layers. Uses the global TracerProvider
so when no exporter is configured (e.g. in unit tests), span operations are
no-ops.

Per spec section 10.2, Prometheus metrics for the plugin are deferred until
the binary plugin metrics infrastructure exists (see spec section 11).

Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md section 10
Bead: holomush-5rh.10"
jj --no-pager new -m "(working: scenes phase 1 — store)"
```

---

## Task 6: Replace `plugins/core-scenes/store.go` with Phase 1 store

The existing store.go has more methods than Phase 1 needs and references `ParticipantRow`. Trim it to `Create` + `Get` only with `SceneRow` only.

**Files:**

- Replace: `plugins/core-scenes/store.go`

- [ ] **Step 1: Write the new store**

Replace the entire contents of `plugins/core-scenes/store.go` with:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "context"
    "embed"
    "errors"
    "io/fs"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/samber/oops"
    "go.opentelemetry.io/otel/attribute"

    "github.com/holomush/holomush/pkg/plugin/storage"
)

//go:embed migrations/*.up.sql
var migrationsFS embed.FS

// SceneRow is the persistence-layer representation of a scene. The shape
// matches the scenes table column-for-column.
//
// Pointer types (LocationID, IdleTimeoutSecs, TemplateID, EndedAt, ArchivedAt)
// represent nullable columns. ContentWarnings and Tags are non-null TEXT[]
// columns; an empty slice corresponds to '{}' in the database.
type SceneRow struct {
    ID              string
    Title           string
    Description     string
    LocationID      *string
    OwnerID         string
    State           string
    PoseOrder       string
    Visibility      string
    IdleTimeoutSecs *int
    TemplateID      *string
    ContentWarnings []string
    Tags            []string
    CreatedAt       time.Time
    EndedAt         *time.Time
    ArchivedAt      *time.Time
}

// SceneStore provides PostgreSQL persistence for scenes.
//
// Phase 1 implements only Create and Get. Subsequent phases extend the store
// with state transitions (Phase 2), participant operations (Phase 3),
// publish-vote and log archival (Phase 6), and templates (Phase 7).
type SceneStore struct {
    pool *pgxpool.Pool
}

// NewSceneStore opens a connection pool and runs the embedded migrations.
//
// The connection string is the one provided by the host's SchemaProvisioner
// in ServiceConfig.ConnectionString — it has search_path=plugin_core_scenes
// pre-configured, so all queries automatically target the plugin's schema.
func NewSceneStore(ctx context.Context, connString string) (*SceneStore, error) {
    pool, err := storage.Connect(ctx, connString)
    if err != nil {
        return nil, oops.Code("SCENE_STORE_CONNECT_FAILED").Wrap(err)
    }

    sub, err := fs.Sub(migrationsFS, "migrations")
    if err != nil {
        pool.Close()
        return nil, oops.Code("SCENE_STORE_INIT_FAILED").Wrap(err)
    }
    if err := storage.RunMigrationsFS(ctx, pool, sub); err != nil {
        pool.Close()
        return nil, oops.Code("SCENE_STORE_MIGRATIONS_FAILED").Wrap(err)
    }

    return &SceneStore{pool: pool}, nil
}

// Close releases the underlying connection pool. Safe to call from a defer
// in main(); idempotent if pool is already nil-ish (pgxpool guards internally).
func (s *SceneStore) Close() {
    if s.pool != nil {
        s.pool.Close()
    }
}

// Create inserts a new scene row. The caller MUST populate ID, Title,
// OwnerID, State, PoseOrder, and Visibility; defaults from the schema apply
// for unset nullable fields.
func (s *SceneStore) Create(ctx context.Context, row *SceneRow) error {
    ctx, span := startSpan(ctx, "scene.store.create",
        attribute.String("scene_id", row.ID),
    )
    defer span.End()

    _, err := s.pool.Exec(ctx, `
        INSERT INTO scenes (
            id, title, description, location_id, owner_id, state, pose_order,
            visibility, idle_timeout_secs, template_id, content_warnings, tags
        ) VALUES (
            $1, $2, $3, $4, $5, $6, $7,
            $8, $9, $10, $11, $12
        )`,
        row.ID, row.Title, row.Description, row.LocationID, row.OwnerID,
        row.State, row.PoseOrder, row.Visibility, row.IdleTimeoutSecs,
        row.TemplateID, row.ContentWarnings, row.Tags,
    )
    if err != nil {
        recordError(span, err)
        return oops.Code("SCENE_CREATE_FAILED").With("scene_id", row.ID).Wrap(err)
    }
    return nil
}

// Get loads a single scene by ID. Returns a SCENE_NOT_FOUND error code if
// the row does not exist.
func (s *SceneStore) Get(ctx context.Context, id string) (*SceneRow, error) {
    ctx, span := startSpan(ctx, "scene.store.get",
        attribute.String("scene_id", id),
    )
    defer span.End()

    row := &SceneRow{}
    err := s.pool.QueryRow(ctx, `
        SELECT id, title, description, location_id, owner_id, state, pose_order,
               visibility, idle_timeout_secs, template_id, content_warnings, tags,
               created_at, ended_at, archived_at
        FROM scenes
        WHERE id = $1`,
        id,
    ).Scan(
        &row.ID, &row.Title, &row.Description, &row.LocationID, &row.OwnerID,
        &row.State, &row.PoseOrder, &row.Visibility, &row.IdleTimeoutSecs,
        &row.TemplateID, &row.ContentWarnings, &row.Tags,
        &row.CreatedAt, &row.EndedAt, &row.ArchivedAt,
    )
    if err != nil {
        recordError(span, err)
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Wrap(err)
        }
        return nil, oops.Code("SCENE_GET_FAILED").With("scene_id", id).Wrap(err)
    }
    return row, nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `task lint`
Expected: No errors in `plugins/core-scenes/store.go`. There will likely still be errors elsewhere in the package because `service.go` references methods that no longer exist — those are addressed in later tasks.

- [ ] **Step 3: Commit**

```bash
jj --no-pager describe -m "feat(scenes): Phase 1 SceneStore (Create + Get only)

Trim store.go to the methods needed for Phase 1: Create and Get on the
scenes table. ParticipantRow and participant methods are removed; they
return in Phase 3 via a new migration and additional store methods.

Both methods are instrumented with OTel spans via the observability
helpers from Task 5. Errors are wrapped with oops.Code() so the gRPC
service layer can map them to appropriate status codes.

Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md sections 1.1, 9.2, 10.1
Bead: holomush-5rh.10"
jj --no-pager new -m "(working: scenes phase 1 — store integration test)"
```

---

## Task 7: Replace `plugins/core-scenes/store_integration_test.go` with Phase 1 tests

The existing integration test references methods that no longer exist. Replace with a focused test of `Create` and `Get`.

**Files:**

- Replace: `plugins/core-scenes/store_integration_test.go`
- Replace: `plugins/core-scenes/store_test.go` (delete — replaced by integration test for now; pure unit tests can come back later if value emerges)

- [ ] **Step 1: Delete the old unit test file**

```bash
rm plugins/core-scenes/store_test.go
```

The old `store_test.go` tested methods that are deleted from the store. Phase 1's store coverage is via the integration test using a real Postgres container (the project pattern for store tests anyway).

- [ ] **Step 2: Write the integration test**

Replace the entire contents of `plugins/core-scenes/store_integration_test.go` with:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/samber/oops"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/pkg/errutil"
    "github.com/holomush/holomush/test/testutil"
)

// newTestStore starts a Postgres testcontainer, opens a SceneStore against
// it, and returns the store with a cleanup function. Uses the project's
// testutil.StartPostgres helper to match other plugin integration tests.
func newTestStore(t *testing.T) (*SceneStore, func()) {
    t.Helper()

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    pgInstance, err := testutil.StartPostgres(ctx)
    require.NoError(t, err, "failed to start postgres testcontainer")

    store, err := NewSceneStore(ctx, pgInstance.ConnString)
    require.NoError(t, err, "failed to open scene store")

    cleanup := func() {
        store.Close()
        _ = pgInstance.Terminate(ctx)
        cancel()
    }
    return store, cleanup
}

func TestSceneStoreCreatePersistsAllSceneFields(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    locationID := "loc-01"
    row := &SceneRow{
        ID:              "scene-01HXYZ",
        Title:           "A Decades-Crossed Meeting",
        Description:     "Off-grid private meeting",
        LocationID:      &locationID,
        OwnerID:         "char-alice",
        State:           string(SceneStateActive),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{},
        Tags:            []string{"plot", "social"},
    }

    err := store.Create(ctx, row)
    require.NoError(t, err)

    got, err := store.Get(ctx, row.ID)
    require.NoError(t, err)
    assert.Equal(t, row.ID, got.ID)
    assert.Equal(t, row.Title, got.Title)
    assert.Equal(t, row.Description, got.Description)
    require.NotNil(t, got.LocationID)
    assert.Equal(t, locationID, *got.LocationID)
    assert.Equal(t, row.OwnerID, got.OwnerID)
    assert.Equal(t, row.State, got.State)
    assert.Equal(t, row.PoseOrder, got.PoseOrder)
    assert.Equal(t, row.Visibility, got.Visibility)
    assert.ElementsMatch(t, row.Tags, got.Tags)
    assert.NotZero(t, got.CreatedAt)
}

func TestSceneStoreGetReturnsNotFoundForMissingScene(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()

    _, err := store.Get(ctx, "scene-does-not-exist")
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")

    var oopsErr oops.OopsError
    if errors.As(err, &oopsErr) {
        assert.Equal(t, "scene-does-not-exist", oopsErr.Context()["scene_id"])
    }
}

func TestSceneStoreCreateRejectsDuplicateID(t *testing.T) {
    store, cleanup := newTestStore(t)
    defer cleanup()

    ctx := context.Background()
    row := &SceneRow{
        ID:              "scene-dup",
        Title:           "Original",
        OwnerID:         "char-bob",
        State:           string(SceneStateActive),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{},
        Tags:            []string{},
    }

    err := store.Create(ctx, row)
    require.NoError(t, err)

    err = store.Create(ctx, row)
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "SCENE_CREATE_FAILED")
}
```

- [ ] **Step 3: Run the integration tests**

Run: `task test:int -- ./plugins/core-scenes/`
Expected: All three tests PASS. Integration tests use Docker via testcontainers and may take 30-60s on first run.

- [ ] **Step 4: Commit**

```bash
jj --no-pager describe -m "test(scenes): Phase 1 SceneStore integration tests

Replace store tests with three integration tests against a real Postgres
testcontainer:
- create persists all fields and get returns them
- get returns SCENE_NOT_FOUND for missing scenes
- create rejects duplicate ID with SCENE_CREATE_FAILED

The unit-level store_test.go is deleted; Phase 1 store coverage is via
the integration test, which is the project convention for store tests.

Bead: holomush-5rh.10"
jj --no-pager new -m "(working: scenes phase 1 — service)"
```

---

## Task 8: Replace `plugins/core-scenes/service.go` with Phase 1 SceneServiceImpl

The existing service.go has all 9 RPCs from the v1 design. Phase 1 only needs `CreateScene` and `GetScene`; later phases add the rest.

**Files:**

- Replace: `plugins/core-scenes/service.go`

- [ ] **Step 1: Write the new service**

Replace the entire contents of `plugins/core-scenes/service.go` with:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "context"
    "crypto/rand"
    "errors"
    "log/slog"
    "strings"
    "time"

    "github.com/oklog/ulid/v2"
    "github.com/samber/oops"
    "go.opentelemetry.io/otel/attribute"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "google.golang.org/protobuf/types/known/timestamppb"

    scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// sceneStorer is the persistence interface required by SceneServiceImpl.
// Defined here so the service layer is not coupled to the concrete
// SceneStore type — tests can substitute a fake implementation.
//
// Phase 1 only needs Create and Get. The interface grows phase by phase.
type sceneStorer interface {
    Create(ctx context.Context, row *SceneRow) error
    Get(ctx context.Context, id string) (*SceneRow, error)
}

// SceneServiceImpl implements scenev1.SceneServiceServer for Phase 1.
//
// The store field is wired by main()'s Init via direct field assignment
// after NewSceneStore returns. The pre-allocated zero-value SceneServiceImpl
// is registered with the gRPC server in RegisterServices, before Init is
// called, so the field assignment in Init wires the store after RegisterServices.
type SceneServiceImpl struct {
    scenev1.UnimplementedSceneServiceServer
    store sceneStorer
}

// NewSceneServiceImpl returns a service backed by the given store.
// Used by tests; main() constructs the service directly with a nil store
// and assigns it after Init.
func NewSceneServiceImpl(store sceneStorer) *SceneServiceImpl {
    return &SceneServiceImpl{store: store}
}

// CreateScene generates a new scene ID, persists the scene, and returns it.
// The caller (host) is responsible for ensuring ABAC has authorised the
// command-execute action; per-resource ABAC for the new scene happens at
// the read path.
func (s *SceneServiceImpl) CreateScene(ctx context.Context, req *scenev1.CreateSceneRequest) (*scenev1.CreateSceneResponse, error) {
    ctx, span := startSpan(ctx, "scene.service.create_scene",
        attribute.String("subject_id", req.GetCharacterId()),
    )
    defer span.End()

    if req.GetCharacterId() == "" {
        recordError(span, errors.New("character_id is required"))
        return nil, status.Errorf(codes.InvalidArgument, "character_id is required")
    }
    title := strings.TrimSpace(req.GetTitle())
    if title == "" {
        recordError(span, errors.New("title is required"))
        return nil, status.Errorf(codes.InvalidArgument, "title is required")
    }

    id, err := newSceneID()
    if err != nil {
        recordError(span, err)
        return nil, status.Errorf(codes.Internal, "failed to generate scene id: %v", err)
    }
    span.SetAttributes(attribute.String("scene_id", id))

    row := &SceneRow{
        ID:              id,
        Title:           title,
        Description:     req.GetDescription(),
        OwnerID:         req.GetCharacterId(),
        State:           string(SceneStateActive),
        PoseOrder:       string(PoseOrderModeFree),
        Visibility:      string(SceneVisibilityOpen),
        ContentWarnings: []string{},
        Tags:            []string{},
    }
    if loc := req.GetLocationId(); loc != "" {
        row.LocationID = &loc
    }

    if err := s.store.Create(ctx, row); err != nil {
        recordError(span, err)
        slog.WarnContext(ctx, "scene.service.create_scene store error",
            "subject_id", req.GetCharacterId(),
            "scene_id", id,
            "error", err,
        )
        return nil, status.Errorf(codes.Internal, "failed to create scene: %v", err)
    }

    slog.InfoContext(ctx, "scene.service.create_scene ok",
        "subject_id", req.GetCharacterId(),
        "scene_id", id,
        "title", title,
    )

    return &scenev1.CreateSceneResponse{
        Scene: rowToProto(row, time.Now().UTC()),
    }, nil
}

// GetScene loads a scene by ID and returns it. The host's ABAC engine has
// already evaluated the read-own-scene policy before this RPC is invoked,
// so the service does not perform an additional ownership check.
func (s *SceneServiceImpl) GetScene(ctx context.Context, req *scenev1.GetSceneRequest) (*scenev1.GetSceneResponse, error) {
    ctx, span := startSpan(ctx, "scene.service.get_scene",
        attribute.String("scene_id", req.GetSceneId()),
    )
    defer span.End()

    if req.GetSceneId() == "" {
        recordError(span, errors.New("scene_id is required"))
        return nil, status.Errorf(codes.InvalidArgument, "scene_id is required")
    }

    row, err := s.store.Get(ctx, req.GetSceneId())
    if err != nil {
        recordError(span, err)
        var oe oops.OopsError
        if errors.As(err, &oe) && oe.Code() == "SCENE_NOT_FOUND" {
            return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetSceneId())
        }
        slog.WarnContext(ctx, "scene.service.get_scene store error",
            "scene_id", req.GetSceneId(),
            "error", err,
        )
        return nil, status.Errorf(codes.Internal, "failed to get scene: %v", err)
    }

    slog.InfoContext(ctx, "scene.service.get_scene ok",
        "scene_id", row.ID,
    )

    return &scenev1.GetSceneResponse{
        Scene: rowToProto(row, row.CreatedAt),
    }, nil
}

// newSceneID generates a ULID using crypto/rand for entropy. Per project
// convention, math/rand is forbidden everywhere — see CLAUDE.md.
func newSceneID() (string, error) {
    ms := ulid.Timestamp(time.Now())
    id, err := ulid.New(ms, rand.Reader)
    if err != nil {
        return "", oops.Code("SCENE_ID_GEN_FAILED").Wrap(err)
    }
    return "scene-" + id.String(), nil
}

// rowToProto converts a SceneRow to the proto representation.
//
// createdAt is passed in to allow CreateScene (which has not re-fetched
// from the database) to use the host's wall clock; GetScene passes the
// row's actual CreatedAt.
func rowToProto(row *SceneRow, createdAt time.Time) *scenev1.SceneInfo {
    info := &scenev1.SceneInfo{
        Id:          row.ID,
        Title:       row.Title,
        Description: row.Description,
        OwnerId:     row.OwnerID,
        State:       row.State,
        Visibility:  row.Visibility,
        CreatedAt:   timestamppb.New(createdAt),
    }
    if row.LocationID != nil {
        info.LocationId = *row.LocationID
    }
    return info
}
```

Note: The existing `pkg/proto/holomush/scene/v1/scene.pb.go` was generated from the v1 proto and includes `SceneInfo`, `CreateSceneRequest`, `CreateSceneResponse`, `GetSceneRequest`, `GetSceneResponse`. If the field names in the proto differ from what `rowToProto` uses, this code will fail to compile — fix by adjusting the field references to match the actual proto.

- [ ] **Step 2: Verify it compiles**

Run: `task lint`
Expected: `plugins/core-scenes/service.go` builds. There may still be errors in `service_test.go` (which references the old methods) — those are addressed in Task 9.

If there are field mismatches against the proto (e.g., `Visibility` doesn't exist on `SceneInfo`), check the proto file at `api/proto/holomush/scene/v1/scene.proto` and adjust the Go field names.

- [ ] **Step 3: Commit**

```bash
jj --no-pager describe -m "feat(scenes): Phase 1 SceneServiceImpl (CreateScene + GetScene)

Replace service.go with the Phase 1 minimum: only CreateScene and GetScene
RPCs. The other RPCs (List, Update, End, Pause, Resume, Join, Leave, etc.)
return in their respective phases.

Both RPCs:
- emit a span via the observability helpers
- log structured slog entries on entry/exit
- map oops error codes to gRPC status codes (SCENE_NOT_FOUND -> NotFound)
- generate scene IDs using crypto/rand via oklog/ulid

The sceneStorer interface lets unit tests substitute a fake store without
the testcontainers overhead.

Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md sections 7.1, 7.2, 10.1, 10.3
Bead: holomush-5rh.10"
jj --no-pager new -m "(working: scenes phase 1 — service tests)"
```

---

## Task 9: Replace `plugins/core-scenes/service_test.go` with Phase 1 unit tests

The existing service_test.go tests methods that no longer exist. Replace with a fake-store-based unit test of `CreateScene` and `GetScene`.

**Files:**

- Replace: `plugins/core-scenes/service_test.go`

- [ ] **Step 1: Write the new tests**

Replace the entire contents of `plugins/core-scenes/service_test.go` with:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "context"
    "errors"
    "strings"
    "testing"

    "github.com/samber/oops"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"

    scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// fakeStore is an in-memory sceneStorer used by service unit tests. It
// supports configurable error injection so tests can exercise the error
// branches of the service layer.
type fakeStore struct {
    scenes    map[string]*SceneRow
    createErr error
    getErr    error
}

func newFakeStore() *fakeStore {
    return &fakeStore{scenes: make(map[string]*SceneRow)}
}

func (f *fakeStore) Create(_ context.Context, row *SceneRow) error {
    if f.createErr != nil {
        return f.createErr
    }
    if _, exists := f.scenes[row.ID]; exists {
        return oops.Code("SCENE_CREATE_FAILED").With("scene_id", row.ID).Errorf("duplicate")
    }
    cp := *row
    f.scenes[row.ID] = &cp
    return nil
}

func (f *fakeStore) Get(_ context.Context, id string) (*SceneRow, error) {
    if f.getErr != nil {
        return nil, f.getErr
    }
    row, ok := f.scenes[id]
    if !ok {
        return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Errorf("not found")
    }
    return row, nil
}

func TestSceneServiceCreateScenePersistsTitleAndOwnerWhenRequestIsValid(t *testing.T) {
    store := newFakeStore()
    svc := NewSceneServiceImpl(store)

    resp, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
        CharacterId: "char-alice",
        Title:       "  Tea at the Manor  ",
    })
    require.NoError(t, err)
    require.NotNil(t, resp.GetScene())
    assert.True(t, strings.HasPrefix(resp.GetScene().GetId(), "scene-"))
    assert.Equal(t, "Tea at the Manor", resp.GetScene().GetTitle(), "title should be trimmed")
    assert.Equal(t, "char-alice", resp.GetScene().GetOwnerId())
    assert.Equal(t, string(SceneStateActive), resp.GetScene().GetState())
    assert.Equal(t, string(SceneVisibilityOpen), resp.GetScene().GetVisibility())
}

func TestSceneServiceCreateSceneRejectsEmptyCharacterID(t *testing.T) {
    svc := NewSceneServiceImpl(newFakeStore())

    _, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
        CharacterId: "",
        Title:       "Anything",
    })
    require.Error(t, err)
    st, ok := status.FromError(err)
    require.True(t, ok)
    assert.Equal(t, codes.InvalidArgument, st.Code())
    assert.Contains(t, st.Message(), "character_id")
}

func TestSceneServiceCreateSceneRejectsBlankTitle(t *testing.T) {
    svc := NewSceneServiceImpl(newFakeStore())

    _, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
        CharacterId: "char-alice",
        Title:       "   ",
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    assert.Equal(t, codes.InvalidArgument, st.Code())
    assert.Contains(t, st.Message(), "title")
}

func TestSceneServiceCreateSceneReturnsInternalWhenStoreFails(t *testing.T) {
    store := newFakeStore()
    store.createErr = oops.Code("SCENE_CREATE_FAILED").Errorf("boom")
    svc := NewSceneServiceImpl(store)

    _, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
        CharacterId: "char-alice",
        Title:       "Tea",
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    assert.Equal(t, codes.Internal, st.Code())
}

func TestSceneServiceGetSceneReturnsSceneWhenItExists(t *testing.T) {
    store := newFakeStore()
    store.scenes["scene-known"] = &SceneRow{
        ID:         "scene-known",
        Title:      "Existing",
        OwnerID:    "char-alice",
        State:      string(SceneStateActive),
        Visibility: string(SceneVisibilityOpen),
    }
    svc := NewSceneServiceImpl(store)

    resp, err := svc.GetScene(context.Background(), &scenev1.GetSceneRequest{SceneId: "scene-known"})
    require.NoError(t, err)
    assert.Equal(t, "scene-known", resp.GetScene().GetId())
    assert.Equal(t, "Existing", resp.GetScene().GetTitle())
}

func TestSceneServiceGetSceneReturnsNotFoundWhenSceneIsMissing(t *testing.T) {
    svc := NewSceneServiceImpl(newFakeStore())

    _, err := svc.GetScene(context.Background(), &scenev1.GetSceneRequest{SceneId: "scene-missing"})
    require.Error(t, err)
    st, ok := status.FromError(err)
    require.True(t, ok)
    assert.Equal(t, codes.NotFound, st.Code())
}

func TestSceneServiceGetSceneReturnsInternalForUnknownStoreError(t *testing.T) {
    store := newFakeStore()
    store.getErr = errors.New("connection refused")
    svc := NewSceneServiceImpl(store)

    _, err := svc.GetScene(context.Background(), &scenev1.GetSceneRequest{SceneId: "scene-x"})
    require.Error(t, err)
    st, _ := status.FromError(err)
    assert.Equal(t, codes.Internal, st.Code())
}
```

- [ ] **Step 2: Run the tests**

Run: `task test -- ./plugins/core-scenes/`
Expected: All tests in service_test.go and types_test.go PASS.

If proto field names differ (e.g., `SceneId` vs `Id`), adjust both the test code and `service.go` to match.

- [ ] **Step 3: Commit**

```bash
jj --no-pager describe -m "test(scenes): Phase 1 SceneServiceImpl unit tests

Replace service_test.go with seven unit tests against a fake sceneStorer:
- create persists trimmed title and owner
- create rejects empty character_id (codes.InvalidArgument)
- create rejects blank title (codes.InvalidArgument)
- create returns Internal when store fails
- get returns scene when present
- get returns NotFound when missing
- get returns Internal for unknown store errors

The fakeStore is in the test file (not exported); it's used only here.

Bead: holomush-5rh.10"
jj --no-pager new -m "(working: scenes phase 1 — resolver)"
```

---

## Task 10: Create `plugins/core-scenes/resolver.go` (AttributeResolverService)

The plugin's `AttributeResolverService` resolves scene attributes for the host's ABAC policy engine. Phase 1 needs `GetSchema` (called once at load) and `ResolveResource` returning the scene's owner.

**Files:**

- Create: `plugins/core-scenes/resolver.go`
- Create: `plugins/core-scenes/resolver_test.go`

- [ ] **Step 1: Write the failing test**

Create `plugins/core-scenes/resolver_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "context"
    "strings"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"

    pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

func TestSceneResolverGetSchemaReturnsSceneAttributes(t *testing.T) {
    r := NewSceneResolver(newFakeStore())

    resp, err := r.GetSchema(context.Background(), &pluginv1.GetSchemaRequest{})
    require.NoError(t, err)
    require.NotNil(t, resp.GetResourceTypes())
    sceneSchema, ok := resp.GetResourceTypes()["scene"]
    require.True(t, ok, "schema must include 'scene' resource type")
    assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING, sceneSchema.GetAttributes()["owner"])
    assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING, sceneSchema.GetAttributes()["state"])
    assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING, sceneSchema.GetAttributes()["visibility"])
}

func TestSceneResolverResolveResourceReturnsSceneAttributes(t *testing.T) {
    store := newFakeStore()
    store.scenes["scene-01"] = &SceneRow{
        ID:         "scene-01",
        OwnerID:    "char-alice",
        State:      string(SceneStateActive),
        Visibility: string(SceneVisibilityOpen),
    }
    r := NewSceneResolver(store)

    resp, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
        ResourceType: "scene",
        ResourceId:   "scene-01",
    })
    require.NoError(t, err)
    attrs := resp.GetAttributes()
    require.NotNil(t, attrs["owner"])
    assert.Equal(t, "char-alice", attrs["owner"].GetStringValue())
    require.NotNil(t, attrs["state"])
    assert.Equal(t, "active", attrs["state"].GetStringValue())
    require.NotNil(t, attrs["visibility"])
    assert.Equal(t, "open", attrs["visibility"].GetStringValue())
}

func TestSceneResolverResolveResourceRejectsForeignResourceType(t *testing.T) {
    r := NewSceneResolver(newFakeStore())

    _, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
        ResourceType: "widget",
        ResourceId:   "widget-1",
    })
    require.Error(t, err)
    st, ok := status.FromError(err)
    require.True(t, ok)
    assert.Equal(t, codes.InvalidArgument, st.Code())
    assert.True(t, strings.Contains(st.Message(), "scene"), "error should mention 'scene'")
}

func TestSceneResolverResolveResourceReturnsNotFoundForMissingScene(t *testing.T) {
    r := NewSceneResolver(newFakeStore())

    _, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
        ResourceType: "scene",
        ResourceId:   "scene-missing",
    })
    require.Error(t, err)
    st, ok := status.FromError(err)
    require.True(t, ok)
    assert.Equal(t, codes.NotFound, st.Code())
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `task test -- -run TestSceneResolver ./plugins/core-scenes/`
Expected: FAIL with build error: `undefined: NewSceneResolver`.

- [ ] **Step 3: Implement the resolver**

Create `plugins/core-scenes/resolver.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "context"
    "errors"

    "github.com/samber/oops"
    "go.opentelemetry.io/otel/attribute"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"

    pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// resourceTypeScene is the single resource type the scene plugin owns.
// Declared as a constant so the resolver and the manifest stay in sync.
const resourceTypeScene = "scene"

// SceneResolver implements pluginv1.AttributeResolverServiceServer for the
// scene plugin. It exposes the schema of attributes the plugin can resolve
// (GetSchema) and resolves attributes for individual scene resources
// (ResolveResource) when called by the host's ABAC engine during policy
// evaluation.
//
// Per the spec section 5.5 hard-privacy boundary, this resolver MUST NOT
// expose log content, vote tallies, or any other content that lives behind
// the privacy boundary. Phase 1 only exposes owner/state/visibility/location
// for use in the read-own-scene policy.
type SceneResolver struct {
    pluginv1.UnimplementedAttributeResolverServiceServer
    store sceneStorer
}

// NewSceneResolver returns a resolver backed by the given store.
func NewSceneResolver(store sceneStorer) *SceneResolver {
    return &SceneResolver{store: store}
}

// GetSchema returns the attribute schema the plugin can resolve. Called
// once by the host's plugin manager after host.Load returns.
func (r *SceneResolver) GetSchema(ctx context.Context, _ *pluginv1.GetSchemaRequest) (*pluginv1.GetSchemaResponse, error) {
    _, span := startSpan(ctx, "scene.resolver.get_schema")
    defer span.End()

    return &pluginv1.GetSchemaResponse{
        ResourceTypes: map[string]*pluginv1.ResourceTypeSchema{
            resourceTypeScene: {
                Attributes: map[string]pluginv1.AttributeType{
                    "id":         pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
                    "owner":      pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
                    "state":      pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
                    "visibility": pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
                    "location":   pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
                },
            },
        },
    }, nil
}

// ResolveResource returns the attributes for a specific scene resource.
// Called by the host's ABAC engine when evaluating a policy that references
// a scene attribute (e.g., the read-own-scene policy that checks
// resource.scene.owner).
//
// Resource type other than "scene" is rejected with InvalidArgument so
// host-side misrouting bugs surface immediately.
func (r *SceneResolver) ResolveResource(ctx context.Context, req *pluginv1.ResolveResourceRequest) (*pluginv1.ResolveResourceResponse, error) {
    ctx, span := startSpan(ctx, "scene.resolver.resolve_resource",
        attribute.String("resource_type", req.GetResourceType()),
        attribute.String("resource_id", req.GetResourceId()),
    )
    defer span.End()

    if req.GetResourceType() != resourceTypeScene {
        err := status.Errorf(codes.InvalidArgument,
            "core-scenes only resolves resource type %q, got %q",
            resourceTypeScene, req.GetResourceType())
        recordError(span, err)
        return nil, err
    }

    row, err := r.store.Get(ctx, req.GetResourceId())
    if err != nil {
        recordError(span, err)
        var oe oops.OopsError
        if errors.As(err, &oe) && oe.Code() == "SCENE_NOT_FOUND" {
            return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetResourceId())
        }
        return nil, status.Errorf(codes.Internal, "failed to resolve scene: %v", err)
    }

    location := ""
    if row.LocationID != nil {
        location = *row.LocationID
    }

    return &pluginv1.ResolveResourceResponse{
        Attributes: map[string]*pluginv1.AttributeValue{
            "id":         {Kind: &pluginv1.AttributeValue_StringValue{StringValue: row.ID}},
            "owner":      {Kind: &pluginv1.AttributeValue_StringValue{StringValue: row.OwnerID}},
            "state":      {Kind: &pluginv1.AttributeValue_StringValue{StringValue: row.State}},
            "visibility": {Kind: &pluginv1.AttributeValue_StringValue{StringValue: row.Visibility}},
            "location":   {Kind: &pluginv1.AttributeValue_StringValue{StringValue: location}},
        },
    }, nil
}
```

- [ ] **Step 4: Run the resolver tests and confirm they pass**

Run: `task test -- -run TestSceneResolver ./plugins/core-scenes/`
Expected: All four tests PASS.

- [ ] **Step 5: Commit**

```bash
jj --no-pager describe -m "feat(scenes): AttributeResolverService for scene resource type

Add resolver.go implementing pluginv1.AttributeResolverServiceServer:
- GetSchema returns the scene attribute schema (id, owner, state, visibility, location)
- ResolveResource returns attribute values from the store
- Foreign resource types rejected with InvalidArgument (catches host misrouting)
- Missing scenes return NotFound

This is the linchpin of Phase 1 — the per-resource read-own-scene policy
in plugin.yaml references resource.scene.owner, which the host resolves
by calling this method.

Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md sections 5.2, 5.5
Bead: holomush-5rh.10"
jj --no-pager new -m "(working: scenes phase 1 — commands)"
```

---

## Task 11: Create `plugins/core-scenes/commands.go` for `scene create` and `scene info`

The plugin needs to handle the `scene` command. Phase 1 dispatches the `create` and `info` subcommands to the SceneServiceImpl.

**Files:**

- Create: `plugins/core-scenes/commands.go`
- Create: `plugins/core-scenes/commands_test.go`

- [ ] **Step 1: Write the failing test**

Create `plugins/core-scenes/commands_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "context"
    "strings"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

func newTestPlugin() *scenePlugin {
    store := newFakeStore()
    svc := NewSceneServiceImpl(store)
    return &scenePlugin{
        store:   nil, // not used by command handlers
        service: svc,
    }
}

func TestHandleCommandReturnsUsageWhenSubcommandIsMissing(t *testing.T) {
    p := newTestPlugin()

    resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "",
        CharacterID: "char-alice",
    })
    require.NoError(t, err)
    assert.Equal(t, pluginsdk.CommandError, resp.Status)
    assert.Contains(t, resp.Output, "Usage")
}

func TestHandleCommandRejectsUnknownSubcommand(t *testing.T) {
    p := newTestPlugin()

    resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "frobnicate",
        CharacterID: "char-alice",
    })
    require.NoError(t, err)
    assert.Equal(t, pluginsdk.CommandError, resp.Status)
    assert.Contains(t, resp.Output, "Unknown")
}

func TestHandleCommandCreateInvokesSceneServiceCreateScene(t *testing.T) {
    p := newTestPlugin()

    resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "create A New Scene",
        CharacterID: "char-alice",
    })
    require.NoError(t, err)
    assert.Equal(t, pluginsdk.CommandOK, resp.Status)
    assert.Contains(t, resp.Output, "Scene created")
    // The created scene id should be discoverable from the output.
    assert.True(t, strings.Contains(resp.Output, "scene-"), "output should include the scene id")
}

func TestHandleCommandInfoShowsCreatedScene(t *testing.T) {
    p := newTestPlugin()

    // Create a scene first.
    createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "create The Manor",
        CharacterID: "char-alice",
    })
    require.NoError(t, err)
    require.Equal(t, pluginsdk.CommandOK, createResp.Status)

    // Extract the scene ID from the create output. Output format is:
    //   "Scene created: <id>"
    parts := strings.Split(createResp.Output, "Scene created:")
    require.Len(t, parts, 2)
    sceneID := strings.TrimSpace(parts[1])

    // Info on that scene.
    infoResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "info " + sceneID,
        CharacterID: "char-alice",
    })
    require.NoError(t, err)
    assert.Equal(t, pluginsdk.CommandOK, infoResp.Status)
    assert.Contains(t, infoResp.Output, "The Manor")
    assert.Contains(t, infoResp.Output, "char-alice")
}

func TestHandleCommandInfoReturnsErrorWhenSceneIDIsMissing(t *testing.T) {
    p := newTestPlugin()

    resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
        Command:     "scene",
        Args:        "info",
        CharacterID: "char-alice",
    })
    require.NoError(t, err)
    assert.Equal(t, pluginsdk.CommandError, resp.Status)
    assert.Contains(t, resp.Output, "scene id")
}
```

This test depends on `pluginsdk.CommandRequest` having a `CharacterID` field. Verify by checking `pkg/plugin/service.go` for the field name; if it's `Subject`, `Caller`, or similar, adjust the field references.

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `task test -- -run TestHandleCommand ./plugins/core-scenes/`
Expected: FAIL — `HandleCommand` currently returns the placeholder message from `main.go`.

- [ ] **Step 3: Implement the command dispatcher and handlers**

Create `plugins/core-scenes/commands.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "context"
    "fmt"
    "strings"

    "go.opentelemetry.io/otel/attribute"

    pluginsdk "github.com/holomush/holomush/pkg/plugin"
    scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// dispatchCommand handles the "scene" top-level command. Phase 1 supports
// only the "create" and "info" subcommands; the rest return "not yet
// implemented" so future phases can plug in their handlers without
// changing this dispatcher.
//
// Subcommand parsing is intentionally simple: the first whitespace-separated
// token is the subcommand, the rest is its argument string. The "scenes"
// command (no subcommand, browses the board) lands in Phase 8 and is
// handled separately.
func (p *scenePlugin) dispatchCommand(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
    ctx, span := startSpan(ctx, "scene.command.dispatch",
        attribute.String("subject_id", req.CharacterID),
    )
    defer span.End()

    sub, rest := splitSubcommand(req.Args)
    span.SetAttributes(attribute.String("subcommand", sub))

    if sub == "" {
        return pluginsdk.Errorf("Usage: scene <subcommand> [args]\nKnown subcommands: create, info"), nil
    }

    switch sub {
    case "create":
        return p.handleCreate(ctx, req, rest)
    case "info":
        return p.handleInfo(ctx, req, rest)
    default:
        return pluginsdk.Errorf("Unknown scene subcommand %q. Known subcommands: create, info.", sub), nil
    }
}

// handleCreate creates a new scene with the given title. The title is the
// rest of the command line (allowing whitespace) so "scene create The Manor"
// works without quoting.
func (p *scenePlugin) handleCreate(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
    title := strings.TrimSpace(args)
    if title == "" {
        return pluginsdk.Errorf("Usage: scene create <title>"), nil
    }

    resp, err := p.service.CreateScene(ctx, &scenev1.CreateSceneRequest{
        CharacterId: req.CharacterID,
        Title:       title,
    })
    if err != nil {
        return pluginsdk.Errorf("Failed to create scene: %v", err), nil
    }

    return &pluginsdk.CommandResponse{
        Status: pluginsdk.CommandOK,
        Output: fmt.Sprintf("Scene created: %s", resp.GetScene().GetId()),
    }, nil
}

// handleInfo shows scene metadata for the given scene ID. Per the read-own-scene
// policy, the host's ABAC engine has already verified the caller is the owner
// before this code runs (when invoked via the dispatcher's full ABAC pipeline);
// in unit tests, ABAC is bypassed and the service is called directly.
func (p *scenePlugin) handleInfo(ctx context.Context, _ pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
    sceneID := strings.TrimSpace(args)
    if sceneID == "" {
        return pluginsdk.Errorf("Usage: scene info <scene id>"), nil
    }

    resp, err := p.service.GetScene(ctx, &scenev1.GetSceneRequest{SceneId: sceneID})
    if err != nil {
        return pluginsdk.Errorf("Failed to get scene: %v", err), nil
    }

    info := resp.GetScene()
    var b strings.Builder
    fmt.Fprintf(&b, "Scene: %s (%s)\n", info.GetTitle(), info.GetId())
    fmt.Fprintf(&b, "Owner: %s\n", info.GetOwnerId())
    fmt.Fprintf(&b, "State: %s\n", info.GetState())
    fmt.Fprintf(&b, "Visibility: %s\n", info.GetVisibility())
    if info.GetDescription() != "" {
        fmt.Fprintf(&b, "Description: %s\n", info.GetDescription())
    }
    if info.GetLocationId() != "" {
        fmt.Fprintf(&b, "Location: %s\n", info.GetLocationId())
    }

    return &pluginsdk.CommandResponse{
        Status: pluginsdk.CommandOK,
        Output: b.String(),
    }, nil
}

// splitSubcommand splits args into the first whitespace-delimited token and
// the remainder. Used by dispatchCommand to extract the subcommand name.
func splitSubcommand(args string) (sub, rest string) {
    args = strings.TrimSpace(args)
    if args == "" {
        return "", ""
    }
    idx := strings.IndexAny(args, " \t")
    if idx < 0 {
        return args, ""
    }
    return args[:idx], strings.TrimSpace(args[idx+1:])
}
```

- [ ] **Step 4: Update `HandleCommand` in `main.go` to call the dispatcher**

Open `plugins/core-scenes/main.go`. Find the `HandleCommand` method (lines ~36-41) and replace it with:

```go
// HandleCommand routes scene commands to the appropriate subcommand handler.
// The dispatcher lives in commands.go to keep main.go focused on plugin
// lifecycle.
func (p *scenePlugin) HandleCommand(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
    if req.Command != "scene" {
        return pluginsdk.Errorf("core-scenes does not handle command %q", req.Command), nil
    }
    return p.dispatchCommand(ctx, req)
}
```

- [ ] **Step 5: Run the command tests and confirm they pass**

Run: `task test -- -run TestHandleCommand ./plugins/core-scenes/`
Expected: All five tests PASS.

- [ ] **Step 6: Commit**

```bash
jj --no-pager describe -m "feat(scenes): scene create and scene info command handlers

Add commands.go with the scene subcommand dispatcher and handlers for
create and info. main.go's HandleCommand delegates to dispatchCommand;
unknown subcommands return an error response with the list of known
subcommands.

Phase 1 surface:
- scene create <title> -> creates and returns the new scene id
- scene info <id>      -> shows scene metadata

Other subcommands (end/pause/resume/join/leave/etc.) come in their phases.

Bead: holomush-5rh.10"
jj --no-pager new -m "(working: scenes phase 1 — main.go)"
```

---

## Task 12: Update `plugins/core-scenes/main.go` to wire the resolver

The current main.go registers `SceneServiceServer` but does not implement `RegisterAttributeResolver`. Phase 1 needs both.

**Files:**

- Modify: `plugins/core-scenes/main.go`

- [ ] **Step 1: Replace main.go with the updated entry point**

Replace the entire contents of `plugins/core-scenes/main.go` with:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "context"
    "log/slog"

    "github.com/samber/oops"
    "google.golang.org/grpc"

    pluginsdk "github.com/holomush/holomush/pkg/plugin"
    pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
    scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// scenePlugin is the binary plugin entry struct. It implements:
//   - pluginsdk.Handler          (HandleEvent, HandleCommand)
//   - pluginsdk.ServiceProvider  (RegisterServices, Init)
//   - pluginsdk.AttributeResolverProvider (RegisterAttributeResolver)
//
// The service and resolver fields are pre-allocated in main() so the gRPC
// server registration in RegisterServices/RegisterAttributeResolver (which
// runs before Init) has valid receivers. Init wires the store into both
// after NewSceneStore returns.
type scenePlugin struct {
    store    *SceneStore
    service  *SceneServiceImpl
    resolver *SceneResolver
}

// HandleEvent is a no-op for Phase 1. The scene plugin does not subscribe
// to event streams until Phase 4 (event streams + pose order).
func (p *scenePlugin) HandleEvent(_ context.Context, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
    return nil, nil
}

// HandleCommand routes scene commands to the appropriate subcommand handler.
// The dispatcher lives in commands.go to keep main.go focused on plugin
// lifecycle.
func (p *scenePlugin) HandleCommand(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
    if req.Command != "scene" {
        return pluginsdk.Errorf("core-scenes does not handle command %q", req.Command), nil
    }
    return p.dispatchCommand(ctx, req)
}

// RegisterServices registers the SceneServiceServer on the go-plugin gRPC
// transport so the host can proxy scene RPCs to this plugin.
func (p *scenePlugin) RegisterServices(registrar grpc.ServiceRegistrar) {
    scenev1.RegisterSceneServiceServer(registrar, p.service)
}

// RegisterAttributeResolver registers the SceneResolver on the go-plugin
// gRPC transport so the host's ABAC engine can resolve scene attributes
// during policy evaluation.
func (p *scenePlugin) RegisterAttributeResolver(registrar grpc.ServiceRegistrar) {
    pluginv1.RegisterAttributeResolverServiceServer(registrar, p.resolver)
}

// Init is called by the host after the gRPC connection is established and
// the Postgres schema/role have been provisioned. It opens the connection
// pool, runs the embedded migrations, and wires the resulting store into
// both the service and the resolver.
//
// The connection string from ServiceConfig has search_path=plugin_core_scenes
// pre-set, so all queries automatically target the plugin's schema.
func (p *scenePlugin) Init(ctx context.Context, config *pluginv1.ServiceConfig) error {
    connStr := config.GetConnectionString()
    if connStr == "" {
        return oops.Code("SCENE_INIT_FAILED").Errorf("connection_string is required")
    }

    store, err := NewSceneStore(ctx, connStr)
    if err != nil {
        return oops.Code("SCENE_INIT_FAILED").Wrap(err)
    }

    p.store = store
    p.service.store = store
    p.resolver.store = store

    slog.InfoContext(ctx, "core-scenes plugin initialised",
        "storage", "postgres",
    )
    return nil
}

func main() {
    plugin := &scenePlugin{
        service:  &SceneServiceImpl{},
        resolver: &SceneResolver{},
    }

    pluginsdk.ServeWithServices(
        &pluginsdk.ServeConfig{Handler: plugin},
        plugin,
    )
}
```

- [ ] **Step 2: Run the full plugin test suite**

Run: `task test -- ./plugins/core-scenes/`
Expected: All unit tests in the package PASS.

- [ ] **Step 3: Build the plugin binary to verify the entry point compiles**

Run: `task build`
Expected: Build succeeds. The plugin binary should be produced (location depends on `scripts/build-plugins.sh`).

- [ ] **Step 4: Commit**

```bash
jj --no-pager describe -m "feat(scenes): wire AttributeResolver into plugin entry point

Update main.go:
- scenePlugin gains a resolver field
- RegisterAttributeResolver registers SceneResolver with the host's gRPC
  registrar so the host's ABAC engine can call ResolveResource during
  policy evaluation
- Init wires the store into both service and resolver after migrations run

This completes the plugin's static structure. The integration test in
Task 13 verifies the entire stack runs end-to-end.

Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md section 9.4
Bead: holomush-5rh.10"
jj --no-pager new -m "(working: scenes phase 1 — integration test)"
```

---

## Task 13: Create `test/integration/plugin/core_scenes_test.go` (Ginkgo end-to-end test)

The integration test loads the compiled plugin binary, sets up the host's ABAC engine, and verifies the end-to-end flow: owner can create + read; non-owner is denied by the per-resource policy.

**Files:**

- Create: `test/integration/plugin/core_scenes_test.go`

- [ ] **Step 1: Read the existing reference test**

Open `test/integration/plugin/abac_widget_test.go` and read it carefully. The new test follows the same structure: BeforeEach sets up Postgres + plugin host + ABAC engine; It blocks evaluate policies. Look specifically at how the policy engine, schema registry, attribute resolver, and audit logger are wired — the new test reuses the same helpers.

- [ ] **Step 2: Write the new integration test**

Create `test/integration/plugin/core_scenes_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    "runtime"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    plugins "github.com/holomush/holomush/internal/plugin"
    "github.com/holomush/holomush/internal/plugin/goplugin"
    policytypes "github.com/holomush/holomush/internal/access/policy"
    "github.com/holomush/holomush/test/testutil"
)

// coreScenesBinaryPath returns the path to the compiled core-scenes plugin
// binary, matching the layout produced by scripts/build-plugins.sh.
//
// Returns ("", false) if the binary is not present, in which case the
// integration test SKIPS rather than failing — the plugin must be built
// before running this test (task build does this).
func coreScenesBinaryPath() (string, bool) {
    cwd, err := os.Getwd()
    if err != nil {
        return "", false
    }
    // Walk up to repo root from test/integration/plugin/
    repoRoot := filepath.Clean(filepath.Join(cwd, "..", "..", ".."))
    pluginDir := filepath.Join(repoRoot, "plugins", "core-scenes")
    binary := filepath.Join(pluginDir,
        fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH),
        "core-scenes")
    if _, err := os.Stat(binary); err != nil {
        return "", false
    }
    return pluginDir, true
}

func loadCoreScenesManifest() *plugins.Manifest {
    cwd, _ := os.Getwd()
    repoRoot := filepath.Clean(filepath.Join(cwd, "..", "..", ".."))
    manifestPath := filepath.Join(repoRoot, "plugins", "core-scenes", "plugin.yaml")
    m, err := plugins.LoadManifestFile(manifestPath)
    Expect(err).NotTo(HaveOccurred())
    return m
}

var _ = Describe("core-scenes plugin (Phase 1)", func() {
    var (
        ctx       context.Context
        cancel    context.CancelFunc
        host      *goplugin.Host
        engine    policytypes.AccessPolicyEngine
        cleanup   []func()
    )

    BeforeEach(func() {
        pluginDir, ok := coreScenesBinaryPath()
        if !ok {
            Skip("core-scenes binary not built; run `task build` first")
        }

        ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)
        cleanup = nil

        // Start Postgres testcontainer.
        pg, err := testutil.StartPostgres(ctx)
        Expect(err).NotTo(HaveOccurred())
        cleanup = append(cleanup, func() { _ = pg.Terminate(ctx) })

        // Provision plugin schema/role.
        provisioner := plugins.NewSchemaProvisioner(pg.ConnString)
        Expect(provisioner.Init(ctx)).To(Succeed())
        cleanup = append(cleanup, func() { provisioner.Close() })

        // Build the binary plugin host.
        registry := plugins.NewServiceRegistry()
        host = goplugin.NewHost(
            goplugin.WithSchemaProvisioner(provisioner),
            goplugin.WithServiceRegistry(registry),
        )
        cleanup = append(cleanup, func() { _ = host.Close(ctx) })

        // Load the core-scenes plugin.
        manifest := loadCoreScenesManifest()
        Expect(host.Load(ctx, manifest, pluginDir)).To(Succeed())

        // Build the ABAC engine using the same pattern as abac_widget_test.go.
        // The exact wiring (SchemaRegistry, AttributeResolver, PluginAttributeProvider,
        // PolicyCache, etc.) MUST mirror the reference test verbatim — copy
        // the BeforeEach body from test/integration/plugin/abac_widget_test.go
        // and substitute "core-scenes" for "test-abac-widget" where the plugin
        // name is referenced.
        engine = setupSceneABACEngine(ctx, host, manifest)
    })

    AfterEach(func() {
        for i := len(cleanup) - 1; i >= 0; i-- {
            cleanup[i]()
        }
        cancel()
    })

    Describe("ABAC: command execution", func() {
        It("permits a character to execute the scene command", func() {
            req, err := policytypes.NewAccessRequest("character:char-alice", "execute", "command:scene")
            Expect(err).NotTo(HaveOccurred())

            decision, err := engine.Evaluate(ctx, req)
            Expect(err).NotTo(HaveOccurred())
            Expect(decision.IsAllowed()).To(BeTrue(),
                "expected execute-scene-commands policy to permit; got %v", decision.Effect())
        })
    })

    Describe("ABAC: per-resource read-own-scene", func() {
        var sceneID string

        BeforeEach(func() {
            // Create a scene as char-alice via the gRPC service. We invoke
            // CreateScene through the plugin's exposed gRPC client (host
            // proxies it). The exact wiring is via host.SceneServiceClient(name)
            // or equivalent — see how abac_widget_test.go acquires its plugin
            // service client.
            client := getSceneServiceClient(host, "core-scenes")
            resp, err := client.CreateScene(ctx, &scenev1.CreateSceneRequest{
                CharacterId: "char-alice",
                Title:       "Tea at the Manor",
            })
            Expect(err).NotTo(HaveOccurred())
            sceneID = resp.GetScene().GetId()
        })

        It("permits the owner to read their own scene", func() {
            req, err := policytypes.NewAccessRequest("character:char-alice", "read", "scene:"+sceneID)
            Expect(err).NotTo(HaveOccurred())

            decision, err := engine.Evaluate(ctx, req)
            Expect(err).NotTo(HaveOccurred())
            Expect(decision.IsAllowed()).To(BeTrue(),
                "owner should be permitted to read their own scene")
        })

        It("denies a non-owner attempting to read the scene", func() {
            req, err := policytypes.NewAccessRequest("character:char-bob", "read", "scene:"+sceneID)
            Expect(err).NotTo(HaveOccurred())

            decision, err := engine.Evaluate(ctx, req)
            Expect(err).NotTo(HaveOccurred())
            Expect(decision.IsAllowed()).To(BeFalse(),
                "non-owner must be denied by default (no policy permits)")
        })
    })
})
```

**Important:** The functions `setupSceneABACEngine` and `getSceneServiceClient` are placeholders for code that mirrors the reference test in `abac_widget_test.go`. When implementing this task, **read `abac_widget_test.go` carefully** and either (a) copy the BeforeEach body inline (preferred for clarity) or (b) extract the engine setup into a shared helper in this test file. Same for the plugin service client acquisition.

Do NOT introduce a new test helper package; keep all helpers in this test file.

- [ ] **Step 3: Build the plugin and run the integration test**

Run:

```bash
task build
task test:int -- -run TestCoreScenesPhase1 ./test/integration/plugin/
```

If the test runner uses Ginkgo's package-level test entry point (`go test ./test/integration/plugin/...`), use:

```bash
task test:int -- ./test/integration/plugin/
```

Expected: All Describe/It blocks pass. The first test run takes 1-2 minutes due to testcontainer startup.

If `setupSceneABACEngine` is undefined (because the placeholder helpers weren't replaced), the build fails — replace them with the actual wiring from `abac_widget_test.go`.

- [ ] **Step 4: Commit**

```bash
jj --no-pager describe -m "test(scenes): Phase 1 integration test (Ginkgo + testcontainers)

End-to-end test that loads the compiled core-scenes plugin, provisions
its schema, sets up the host's ABAC engine, and verifies:
- the execute-scene-commands policy permits character to run 'scene'
- the read-own-scene policy permits the owner to read their scene
- the read-own-scene policy denies a non-owner

This is the linchpin test for Phase 1. It exercises the full stack:
plugin process startup, schema provisioning, gRPC service registration,
attribute resolver registration, manifest policy installation, and the
host calling back into the plugin's resolver during policy evaluation.

Mirrors the structure of test/integration/plugin/abac_widget_test.go.

Spec: docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md section 9.4
Bead: holomush-5rh.10"
jj --no-pager new -m "(working: scenes phase 1 — verification)"
```

---

## Task 14: Final verification — `task pr-prep`

**Files:** None (verification only)

- [ ] **Step 1: Run the full pre-PR verification**

Run: `task pr-prep`
Expected: All checks pass — lint, format, schema validation, license headers, unit tests, integration tests, E2E tests.

- [ ] **Step 2: Fix any issues**

If `task pr-prep` reports issues, fix them and re-run. Common likely issues:

- Missing license header on a new file → run `task license:add`
- Lint warnings on new code → fix in the relevant file
- Test flakes → investigate; do not just retry

Each fix is its own commit (do not bundle into the verification commit).

- [ ] **Step 3: Verify the bead can be closed**

Read back the Phase 1 acceptance criteria from bead `holomush-5rh.10`:

```bash
bd show holomush-5rh.10
```

Verify each checklist item is satisfied:

- [ ] "scene" removed from ProtectedResourceTypes; test added (Task 1)
- [ ] core-scenes plugin builds via task build (verified in Task 12 step 3 + Task 14 step 1)
- [ ] Plugin loads on server startup (implicitly by integration test in Task 13)
- [ ] "scene create <title>" creates a scene; the owner can read it (Task 13)
- [ ] "scene info" shows scene details for owner (Task 11 unit test + Task 13)
- [ ] Non-owner is denied read by ABAC (Task 13)
- [ ] Ginkgo integration test verifies the full path (Task 13)
- [ ] All boundaries emit spans, metrics, and structured logs — adjusted: spans + structured logs only; metrics deferred per spec section 11
- [ ] task pr-prep passes (Task 14 step 1)

- [ ] **Step 4: Close the Phase 1 bead**

```bash
bd close holomush-5rh.10 --reason "Phase 1 foundation slice complete. core-scenes plugin scaffolded with CreateScene/GetScene RPCs, AttributeResolverService, command handlers for 'scene create' and 'scene info', and end-to-end Ginkgo integration test verifying owner-can-read / non-owner-denied via per-resource ABAC. Prometheus metrics deferred per spec section 11. Implementation plan: docs/superpowers/plans/2026-04-06-scenes-phase-1-foundation.md"
```

- [ ] **Step 5: Final commit**

If any fix-up commits were made in Step 2, ensure all are described and the working copy is clean:

```bash
jj --no-pager st
```

Expected: "The working copy has no changes."

---

## Self-Review Notes

After writing this plan, I checked it against the spec and noticed:

1. **Spec section 10.1 lists six span categories.** Phase 1 implements four of them (`scene.service.<rpc>`, `scene.resolver.resolve_resource`, `scene.resolver.get_schema`, `scene.store.<op>`). The other two (`scene.lifecycle.transition`, `scene.command.<sub>`) are added in this plan: `scene.command.dispatch` is in commands.go, `scene.lifecycle.transition` belongs to Phase 2 (state transitions). This is consistent.

2. **Spec section 10.4 lists "Scene created" as a Phase 1 business event.** It MUST emit a span, log entry, and metric. Span ✓ (in CreateScene). Log entry ✓ (slog.InfoContext after store.Create). Metric ✗ (deferred — see spec gap). When metrics infrastructure exists, add a `scene_created_total` counter increment to CreateScene.

3. **The plan does not exercise the host-side command dispatch path.** The integration test (Task 13) calls the plugin's gRPC service directly to set up state, then evaluates ABAC policies. It does not exercise `scene create` going through the actual command dispatcher. Phase 1's acceptance criterion "scene create <title> creates a scene" is satisfied by the unit tests in commands_test.go (Task 11). End-to-end command dispatch through the host is exercised by every later phase, so I've left this gap deliberate rather than building a host-command-dispatcher harness specifically for Phase 1.

4. **No placeholders remain in this plan.** Each task has complete code. The integration test (Task 13) has two placeholder function names (`setupSceneABACEngine`, `getSceneServiceClient`) that I deliberately left as references to the existing pattern in `abac_widget_test.go` rather than reproducing 100+ lines of host wiring inline — the implementer reads the reference test once and copies the pattern. This is the lowest-friction path; reproducing the wiring inline would lock the plan to the current host setup, which is more brittle.

---

**Plan complete.** Saved to `docs/superpowers/plans/2026-04-06-scenes-phase-1-foundation.md`.

**Two execution options:**

**1. Subagent-Driven (recommended)** — Dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**
