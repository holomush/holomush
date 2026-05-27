<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Content Store, Setting Plugins, and Bootstrap Consolidation Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a content store, setting plugin system, data-driven landing page, and consolidate all bootstrap logic under a priority-ordered BootstrapPlugin interface.

**Architecture:** ContentStore interface with Postgres + file backends behind a routing store. BootstrapPlugin interface with priority ordering replaces scattered bootstrap calls in core.go. Setting plugins bootstrap world/content/theme on first boot. Gateway exposes content via RPC. SvelteKit landing page renders from ContentStore.

**Tech Stack:** Go (ContentStore, BootstrapPlugin, SettingBootstrapper), Protobuf (ContentService RPC), PostgreSQL (content\_items + bootstrap\_metadata tables), SvelteKit (landing page), marked + DOMPurify (markdown rendering)

**Spec:** `docs/superpowers/specs/2026-03-30-content-store-and-setting-plugins-design.md`

---

## Phase Overview

| Phase | Description | Produces |
| ----- | ----------- | -------- |
| 1 | ContentStore interface + Postgres backend | Testable content CRUD with prefix queries and search vector |
| 2 | FileContentStore + RoutingContentStore | Multi-backend content routing by media type |
| 3 | BootstrapPlugin interface + BootstrapRunner | Priority-ordered bootstrap system, replaces inline bootstrap in core.go |
| 4 | Setting plugin manifest + SettingBootstrapper | Setting plugins discovered, content/world/theme seeded on first boot |
| 5 | Gateway ContentService + ServiceProxy extension | Content accessible via RPC and from plugins |
| 6 | Landing page + theme integration | Data-driven SvelteKit landing page rendering from ContentStore |
| 7 | Crossroads + skeleton settings | Default game content, seed world, theme |

Phases 1-2 are sequential (content store foundation). Phase 3 is independent of 1-2 (bootstrap system) and **MAY run in parallel** with Phases 1-2. Phases 4-5 depend on 1-3. Phase 6 depends on 5. Phase 7 depends on 4+6.

---

## Chunk 1: ContentStore Interface + PostgresContentStore

### Task 1: Database migration for content\_items and bootstrap\_metadata

**Files:**

- Create: `internal/store/migrations/000022_content_items.up.sql`
- Create: `internal/store/migrations/000022_content_items.down.sql`
- Create: `internal/store/migrations/000023_bootstrap_metadata.up.sql`
- Create: `internal/store/migrations/000023_bootstrap_metadata.down.sql`

**Context:** Two new tables. `content_items` stores managed content with full-text search support. `bootstrap_metadata` tracks one-time bootstrap state.

- [ ] **Step 1: Create content\_items migration**

```sql
-- 000022_content_items.up.sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE IF NOT EXISTS content_items (
    key            TEXT PRIMARY KEY,
    content_type   TEXT NOT NULL DEFAULT 'text/markdown',
    body           BYTEA NOT NULL,
    metadata       JSONB NOT NULL DEFAULT '{}',
    search_vector  TSVECTOR,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_content_items_prefix
    ON content_items (key text_pattern_ops);

CREATE INDEX IF NOT EXISTS idx_content_items_search
    ON content_items USING GIN (search_vector);

COMMENT ON TABLE content_items IS 'General-purpose content store for managed game content';
```

```sql
-- 000022_content_items.down.sql
DROP TABLE IF EXISTS content_items;
```

- [ ] **Step 2: Create bootstrap\_metadata migration**

```sql
-- 000023_bootstrap_metadata.up.sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE IF NOT EXISTS bootstrap_metadata (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE bootstrap_metadata IS 'Tracks one-time bootstrap state (active setting, schema version, etc.)';
```

```sql
-- 000023_bootstrap_metadata.down.sql
DROP TABLE IF EXISTS bootstrap_metadata;
```

- [ ] **Step 3: Verify migrations apply cleanly**

Run: `task test:int` (integration tests run migrations via testcontainers)
Expected: PASS — existing tests still pass with new migrations

- [ ] **Step 4: Commit**

`feat(store): add content_items and bootstrap_metadata migrations`

---

### Task 2: ContentStore interface and types

**Files:**

- Create: `internal/content/types.go`
- Create: `internal/content/store.go`

**Context:** The ContentStore interface and ContentItem type live in a new `internal/content` package. This package owns the content abstraction — implementations (Postgres, file) import it.

- [ ] **Step 1: Create types.go with ContentItem and ListOptions**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package content provides a general-purpose content store interface.
package content

import "time"

// ContentItem represents a single piece of managed content.
type ContentItem struct {
    Key         string            // hierarchical key, e.g. "landing.hero"
    ContentType string            // IANA media type: "text/markdown", "application/json", etc.
    Body        []byte            // the content
    Metadata    map[string]string // arbitrary k/v (title, icon, order, alt text)
    UpdatedAt   time.Time         // last modification timestamp
}

// ListOptions controls pagination for List queries.
type ListOptions struct {
    Limit  int    // 0 = no limit
    Cursor string // empty = start from beginning
}

// ListResult contains paginated results.
type ListResult struct {
    Items      []*ContentItem
    NextCursor string // empty = no more results
}
```

- [ ] **Step 2: Create store.go with ContentStore interface**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package content

import "context"

// ContentStore is the interface all content backends implement.
type ContentStore interface {
    // Get retrieves a content item by key. Returns nil, nil if not found.
    Get(ctx context.Context, key string) (*ContentItem, error)

    // List returns content items matching a key prefix with optional pagination.
    List(ctx context.Context, prefix string, opts ListOptions) (*ListResult, error)

    // Put creates or updates a content item.
    Put(ctx context.Context, item *ContentItem) error

    // Delete removes a content item by key.
    Delete(ctx context.Context, key string) error
}
```

- [ ] **Step 3: Verify compilation**

Run: `task build`
Expected: PASS

- [ ] **Step 4: Commit**

`feat(content): add ContentStore interface and types`

---

### Task 3: PostgresContentStore implementation

**Files:**

- Create: `internal/content/postgres_store.go`
- Create: `internal/content/postgres_store_test.go`

**Context:** Implements ContentStore against the `content_items` table. Populates `search_vector` for text content types. Uses `pgx` pool (same pattern as other Postgres stores).

- [ ] **Step 1: Write failing tests for PostgresContentStore**

Table-driven tests covering:

- Put + Get round-trip (text/markdown)
- Put + Get round-trip (application/json)
- Get returns nil for non-existent key
- Put upserts (update existing item)
- List with prefix returns matching items
- List with empty prefix returns all items
- List with pagination (limit + cursor)
- List returns empty result for no matches
- Delete removes item
- Delete non-existent key is a no-op
- Put populates search\_vector for text/markdown content
- Put sets search\_vector to NULL for image/\* content
- Put sets updated\_at automatically

Use testcontainers for Postgres (follow pattern from `internal/store/alias_test.go` or `test/integration/` tests).

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — PostgresContentStore not implemented

- [ ] **Step 3: Implement PostgresContentStore**

Key implementation details:

- Constructor: `NewPostgresContentStore(pool *pgxpool.Pool) *PostgresContentStore`
- `Put`: Use `INSERT ... ON CONFLICT (key) DO UPDATE`. Set `search_vector = to_tsvector('english', convert_from($body, 'UTF8'))` when content\_type starts with `text/` or equals `application/json`. Set `search_vector = NULL` otherwise.
- `Get`: `SELECT key, content_type, body, metadata, updated_at FROM content_items WHERE key = $1`
- `List`: `SELECT ... WHERE key LIKE $prefix || '%' ORDER BY key`. When cursor is non-empty: add `AND key > $cursor`. When limit > 0: add `LIMIT $limit + 1` (fetch one extra to determine next\_cursor).
- `Delete`: `DELETE FROM content_items WHERE key = $1`

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test`
Expected: PASS

- [ ] **Step 5: Commit**

`feat(content): implement PostgresContentStore with search vector`

---

### Task 4: FileContentStore implementation

**Files:**

- Create: `internal/content/file_store.go`
- Create: `internal/content/file_store_test.go`

**Context:** Stores binary content on the local filesystem. Root directory is configurable (XDG data dir). Keys map to file paths (dots become directory separators).

- [ ] **Step 1: Write failing tests**

Table-driven tests:

- Put + Get round-trip for binary content
- Key-to-path mapping (dots → path separators)
- Get returns nil for non-existent key
- Delete removes file
- List returns items matching prefix
- Path traversal prevention (reject keys with `..`)
- Metadata stored as sidecar `.meta.json` file

- [ ] **Step 2: Run tests to verify they fail**

- [ ] **Step 3: Implement FileContentStore**

Key details:

- Constructor: `NewFileContentStore(rootDir string) *FileContentStore`
- Key `theme.logo` maps to `<rootDir>/theme/logo` (file) + `<rootDir>/theme/logo.meta.json` (metadata)
- `Put`: Write body to file, metadata to sidecar JSON
- `Get`: Read file + sidecar, compose ContentItem
- `List`: Walk directory, match prefix
- Path traversal: Reject keys containing `..` or starting with `/`

- [ ] **Step 4: Run tests to verify they pass**

- [ ] **Step 5: Commit**

`feat(content): implement FileContentStore for binary assets`

---

### Task 5: RoutingContentStore

**Files:**

- Create: `internal/content/routing_store.go`
- Create: `internal/content/routing_store_test.go`

**Context:** Delegates to backend stores by content type. Supports glob patterns for media type matching (e.g., `image/*`).

- [ ] **Step 1: Write failing tests**

- Routes `text/markdown` to Postgres mock
- Routes `image/png` to file mock (via `image/*` glob)
- Falls back to default store for unknown types
- `List` queries all backends and merges results sorted by key
- Returns error when no route and no fallback

- [ ] **Step 2: Run tests to verify they fail**

- [ ] **Step 3: Implement RoutingContentStore**

```go
type RoutingContentStore struct {
    routes   map[string]ContentStore // content type or glob → backend
    fallback ContentStore
}

func NewRoutingContentStore(fallback ContentStore, routes map[string]ContentStore) *RoutingContentStore
```

- `resolveStore(contentType)`: Check exact match first, then glob patterns, then fallback.
- `List`: Query all unique backends, merge results by key, apply pagination.

- [ ] **Step 4: Run tests to verify they pass**

- [ ] **Step 5: Commit**

`feat(content): implement RoutingContentStore with glob matching`

---

## Chunk 2: BootstrapPlugin Interface + Runner

### Task 6: BootstrapPlugin interface and BootstrapRunner

**Files:**

- Create: `internal/plugin/bootstrap.go`
- Create: `internal/plugin/bootstrap_test.go`

**Context:** Priority-ordered bootstrap plugin system. The runner collects plugins, sorts by priority, and runs them sequentially. Any failure is fatal.

- [ ] **Step 1: Write failing tests**

- Runner executes plugins in priority order
- Lower priority values run first
- Same priority runs in registration order
- Error from any plugin stops execution and returns error
- Empty runner succeeds

- [ ] **Step 2: Run tests to verify they fail**

- [ ] **Step 3: Implement BootstrapPlugin and BootstrapRunner**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
    "context"
    "log/slog"
    "sort"
)

// BootstrapPlugin is implemented by plugins that run once during server startup.
type BootstrapPlugin interface {
    Priority() int
    Bootstrap(ctx context.Context, manifest *Manifest, pluginDir string) error
}

// Bootstrap priority levels.
const (
    BootstrapPrioritySchema  = 100
    BootstrapPriorityPolicy  = 200
    BootstrapPriorityWorld   = 300
    BootstrapPriorityContent = 400
    BootstrapPriorityAlias   = 500
)

// BootstrapRunner collects and runs bootstrap plugins in priority order.
type BootstrapRunner struct {
    bootstrappers []BootstrapPlugin
    logger  *slog.Logger
}

func NewBootstrapRunner(logger *slog.Logger) *BootstrapRunner
func (r *BootstrapRunner) Register(p BootstrapPlugin)
func (r *BootstrapRunner) RunAll(ctx context.Context) error
```

`RunAll` sorts by `Priority()`, iterates, calls `Bootstrap(ctx, nil, "")` for non-manifest plugins. Returns first error.

- [ ] **Step 4: Run tests to verify they pass**

- [ ] **Step 5: Commit**

`feat(plugin): add BootstrapPlugin interface and BootstrapRunner`

---

### Task 7: MigrationBootstrapper

**Files:**

- Create: `internal/bootstrap/migration.go`
- Create: `internal/bootstrap/migration_test.go`

**Context:** Wraps the existing auto-migration logic. Priority 100 (Schema). Replaces the inline `runAutoMigration()` call in core.go.

- [ ] **Step 1: Write failing tests**

- Calls migrator Up() when enabled
- Skips migration when disabled
- Returns error on migration failure
- Priority() returns BootstrapPrioritySchema (100)

- [ ] **Step 2: Run tests to verify they fail**

- [ ] **Step 3: Implement MigrationBootstrapper**

```go
type MigrationBootstrapper struct {
    databaseURL    string
    migratorFactory func(string) (AutoMigrator, error)
    enabled        bool
}

func (b *MigrationBootstrapper) Priority() int { return plugins.BootstrapPrioritySchema }
func (b *MigrationBootstrapper) Bootstrap(ctx context.Context, _ *plugins.Manifest, _ string) error
```

Delegates to existing `runAutoMigration` pattern (create migrator, call Up, close).

- [ ] **Step 4: Run tests to verify they pass**

- [ ] **Step 5: Commit**

`feat(bootstrap): add MigrationBootstrapper`

---

### Task 8: PolicyBootstrapper adapter

**Files:**

- Create: `internal/bootstrap/policy.go`
- Create: `internal/bootstrap/policy_test.go`

**Context:** Wraps the existing `deps.PolicyBootstrapper` function. Priority 200.

- [ ] **Step 1: Write failing test**
- [ ] **Step 2: Implement PolicyBootstrapper**

```go
type PolicyBootstrapper struct {
    bootstrapFn        func(ctx context.Context, skipSeedMigrations bool) error
    skipSeedMigrations bool
}

func (b *PolicyBootstrapper) Priority() int { return plugins.BootstrapPriorityPolicy }
func (b *PolicyBootstrapper) Bootstrap(ctx context.Context, _ *plugins.Manifest, _ string) error {
    return b.bootstrapFn(ctx, b.skipSeedMigrations)
}
```

- [ ] **Step 3: Run tests, verify pass**
- [ ] **Step 4: Commit**

`feat(bootstrap): add PolicyBootstrapper adapter`

---

### Task 9: AdminBootstrapper adapter

**Files:**

- Create: `internal/bootstrap/admin_bootstrap.go`
- Create: `internal/bootstrap/admin_bootstrap_test.go`

**Context:** Wraps `SeedAdmin`. Priority 400 (Content — admin needs DB and policies ready).

- [ ] **Step 1: Write failing test**
- [ ] **Step 2: Implement AdminBootstrapper**

```go
type AdminBootstrapper struct {
    deps SeedAdminDeps
}

func (b *AdminBootstrapper) Priority() int { return plugins.BootstrapPriorityContent }
func (b *AdminBootstrapper) Bootstrap(ctx context.Context, _ *plugins.Manifest, _ string) error {
    return SeedAdmin(ctx, b.deps)
}
```

- [ ] **Step 3: Run tests, verify pass**
- [ ] **Step 4: Commit**

`feat(bootstrap): add AdminBootstrapper adapter`

---

### Task 10: AliasBootstrapper adapter

**Files:**

- Create: `internal/bootstrap/alias_bootstrap.go`
- Create: `internal/bootstrap/alias_bootstrap_test.go`

**Context:** Wraps `SeedSystemAliases`. Priority 500 (needs command registry populated).

- [ ] **Step 1: Write failing test**
- [ ] **Step 2: Implement AliasBootstrapper**

```go
type AliasBootstrapper struct {
    repo  AliasSeeder
    cache *command.AliasCache
}

func (b *AliasBootstrapper) Priority() int { return plugins.BootstrapPriorityAlias }
func (b *AliasBootstrapper) Bootstrap(ctx context.Context, _ *plugins.Manifest, _ string) error {
    return SeedSystemAliases(ctx, b.repo, b.cache)
}
```

- [ ] **Step 3: Run tests, verify pass**
- [ ] **Step 4: Commit**

`feat(bootstrap): add AliasBootstrapper adapter`

---

### Task 11: Wire BootstrapRunner into core.go, delete seed.go

**Files:**

- Modify: `cmd/holomush/core.go` (replace inline bootstrap calls with runner)
- Delete: `cmd/holomush/seed.go`
- Delete: `cmd/holomush/seed_test.go`
- Delete: `cmd/holomush/validate_seeds.go`
- Delete: `cmd/holomush/validate_seeds_test.go`
- Modify: `cmd/holomush/root.go` (remove seed subcommand registration)

**Context:** Replace the three scattered bootstrap blocks (migration, policy, admin, alias) with a single `bootstrapRunner.RunAll(ctx)`. Delete the standalone seed command.

- [ ] **Step 1: Create BootstrapRunner in core.go startup, register all bootstrappers**

In the startup sequence after DB connection, replace:

```go
// Old: inline migration
runAutoMigration(...)
// Old: inline policy bootstrap
deps.PolicyBootstrapper(ctx, cfg.SkipSeedMigrations)
// Old: inline admin seed
bootstrap.SeedAdmin(ctx, deps)
// Old: inline alias seed
bootstrap.SeedSystemAliases(ctx, aliasRepo, aliasCache)
```

With:

```go
runner := plugins.NewBootstrapRunner(slog.Default())
runner.Register(&bootstrap.MigrationBootstrapper{...})
runner.Register(&bootstrap.PolicyBootstrapper{...})
runner.Register(&bootstrap.AdminBootstrapper{...})
runner.Register(&bootstrap.AliasBootstrapper{...})
if err := runner.RunAll(ctx); err != nil {
    return oops.Code("BOOTSTRAP_FAILED").Wrap(err)
}
```

- [ ] **Step 2: Delete seed.go and seed\_test.go**
- [ ] **Step 3: Remove seed subcommand from root.go**
- [ ] **Step 4: Run task test && task lint**
- [ ] **Step 5: Run task test:int** (integration tests must still pass)
- [ ] **Step 6: Commit**

`refactor(core): consolidate bootstrap into BootstrapRunner, delete seed command`

---

## Chunk 3: Setting Plugin Type + SettingBootstrapper

### Task 12: Manifest changes for setting type

**Files:**

- Modify: `internal/plugin/manifest.go`
- Modify: `internal/plugin/manifest_test.go`

**Context:** Add `TypeSetting` to the type enum and `SettingConfig` struct. Update validation.

- [ ] **Step 1: Write failing tests**

- Valid setting manifest parses and validates
- Setting manifest without `setting:` stanza fails validation
- Setting manifest with `commands:` fails validation
- Setting manifest with `lua-plugin:` fails validation
- Setting type enum value is `"setting"`

- [ ] **Step 2: Run tests to verify they fail**

- [ ] **Step 3: Add TypeSetting, SettingConfig, and validation**

```go
const TypeSetting Type = "setting"

type SettingConfig struct {
    DisplayName string `yaml:"display_name" json:"display_name"`
    Description string `yaml:"description" json:"description"`
    ContentDir  string `yaml:"content_dir" json:"content_dir"`
    WorldDir    string `yaml:"world_dir" json:"world_dir"`
    Theme       string `yaml:"theme" json:"theme"`
}
```

Update `Validate()`:

- Accept `TypeSetting` in the type switch
- Require `Setting != nil` when type is setting
- Reject `Commands`, `LuaPlugin`, `BinaryPlugin` when type is setting
- Update `jsonschema` struct tag on `Manifest.Type` field to `jsonschema:"required,enum=core,enum=lua,enum=binary,enum=setting"`
- Add `Setting *SettingConfig` field to `Manifest` struct with `yaml:"setting,omitempty" json:"setting,omitempty"`

- [ ] **Step 4: Run tests to verify they pass**
- [ ] **Step 5: Run task generate:schema** (update JSON schema)
- [ ] **Step 6: Commit**

`feat(plugin): add setting plugin type to manifest`

---

### Task 13: Markdown frontmatter parser

**Files:**

- Create: `internal/content/frontmatter.go`
- Create: `internal/content/frontmatter_test.go`

**Context:** Parses markdown files with YAML frontmatter into ContentItem. Used by SettingBootstrapper to read setting plugin content directories.

- [ ] **Step 1: Write failing tests**

- Parse valid file with frontmatter + body
- Parse file with no body (frontmatter only)
- Parse file with no frontmatter (treated as plain markdown, key from filename)
- Malformed YAML frontmatter returns error
- Extracts key, content\_type, and arbitrary metadata from frontmatter
- Body is everything after the closing `---`

- [ ] **Step 2: Run tests to verify they fail**

- [ ] **Step 3: Implement ParseContentFile**

```go
func ParseContentFile(path string) (*ContentItem, error)
```

Split on `---` delimiters. Parse YAML frontmatter into map. Extract `key` and `content_type` (default `text/markdown`). Remaining map entries become Metadata. Body is bytes after second `---`.

- [ ] **Step 4: Run tests to verify they pass**
- [ ] **Step 5: Commit**

`feat(content): add markdown frontmatter parser`

---

### Task 14: BootstrapMetadataStore

**Files:**

- Create: `internal/bootstrap/metadata_store.go`
- Create: `internal/bootstrap/metadata_store_test.go`

**Context:** Simple key-value store for bootstrap state, backed by the `bootstrap_metadata` table.

- [ ] **Step 1: Write failing tests**

- Get returns value for existing key
- Get returns empty string and false for missing key
- Set creates new key
- Set upserts existing key
- Delete removes key

- [ ] **Step 2: Implement PostgresBootstrapMetadataStore**

```go
type BootstrapMetadataStore interface {
    Get(ctx context.Context, key string) (string, bool, error)
    Set(ctx context.Context, key, value string) error
    Delete(ctx context.Context, key string) error
}
```

- [ ] **Step 3: Run tests, verify pass**
- [ ] **Step 4: Commit**

`feat(bootstrap): add BootstrapMetadataStore`

---

### Task 15: SettingBootstrapper

**Files:**

- Create: `internal/bootstrap/setting.go`
- Create: `internal/bootstrap/setting_test.go`

**Context:** Processes a setting plugin manifest. Walks content directory, parses markdown files, writes to ContentStore. Creates seed locations and exits. Applies theme overrides. Idempotent — skips existing content. Depends on Task 14 (BootstrapMetadataStore).

- [ ] **Step 1: Write failing tests**

- Bootstraps content from markdown files into ContentStore
- Skips content that already exists (idempotency)
- Creates seed locations from YAML
- Creates seed exits from YAML
- Applies theme overrides as content items
- Records active setting in bootstrap\_metadata
- Skips bootstrap if setting already recorded (subsequent boots)
- `--reset-setting` clears metadata and re-runs
- Priority() returns BootstrapPriorityWorld (300)

Use mock ContentStore, mock WorldService, and mock BootstrapMetadataStore.

- [ ] **Step 2: Run tests to verify they fail**

- [ ] **Step 3: Implement SettingBootstrapper**

```go
// Compile-time check.
var _ plugins.BootstrapPlugin = (*SettingBootstrapper)(nil)

type SettingBootstrapper struct {
    contentStore  content.ContentStore
    worldService  *world.Service
    metadataStore BootstrapMetadataStore
    settingName   string // from --setting flag
    resetSetting  bool   // from --reset-setting flag
    logger        *slog.Logger
}

func (b *SettingBootstrapper) Priority() int { return plugins.BootstrapPriorityWorld }
func (b *SettingBootstrapper) Bootstrap(ctx context.Context, manifest *plugins.Manifest, pluginDir string) error
```

Bootstrap flow:

1. Check `bootstrap_metadata` for `active_setting`. If present and not resetting, skip.
2. Walk `manifest.Setting.ContentDir` in pluginDir, parse each `.md` file
3. For each, check if key exists in ContentStore. Skip if exists (idempotency).
4. Parse `manifest.Setting.WorldDir/locations.yaml`, create locations via WorldService
5. Parse `manifest.Setting.WorldDir/exits.yaml`, create exits via WorldService
6. Parse `manifest.Setting.Theme`, write theme items to ContentStore
7. Record `active_setting` and `setting_version` in bootstrap\_metadata

- [ ] **Step 4: Run tests to verify they pass**
- [ ] **Step 5: Commit**

`feat(bootstrap): implement SettingBootstrapper`

---

### Task 16: Add --setting and --reset-setting flags to core command

**Files:**

- Modify: `cmd/holomush/core.go`

**Context:** New CLI flags on the core subcommand. Wire SettingBootstrapper into the BootstrapRunner.

- [ ] **Step 1: Add flags to NewCoreCmd**

```go
cmd.Flags().StringVar(&cfg.Setting, "setting", "crossroads", "setting plugin to bootstrap on first boot")
cmd.Flags().BoolVar(&cfg.ResetSetting, "reset-setting", false, "force re-bootstrap from setting plugin")
```

- [ ] **Step 2: Wire SettingBootstrapper into runner**

After the plugin manager discovers plugins, find the active setting plugin manifest and register `SettingBootstrapper` in the runner.

- [ ] **Step 3: Run task test && task lint**
- [ ] **Step 4: Commit**

`feat(core): add --setting and --reset-setting flags`

---

## Chunk 4: Gateway ContentService + ServiceProxy

### Task 17: ContentService proto definition

**Files:**

- Create: `api/proto/holomush/content/v1/content.proto`
- Modify: `api/proto/holomush/web/v1/web.proto` (add content RPCs to WebService)

**Context:** Content service proto with Get/List RPCs. Added to WebService for gateway access (public, no auth required).

- [ ] **Step 1: Create content.proto**

```protobuf
syntax = "proto3";
package holomush.content.v1;
option go_package = "github.com/holomush/holomush/pkg/proto/holomush/content/v1;contentv1";

service ContentService {
    rpc GetContent(GetContentRequest) returns (GetContentResponse);
    rpc ListContent(ListContentRequest) returns (ListContentResponse);
}

message GetContentRequest { string key = 1; }
message GetContentResponse { ContentItem item = 1; }
message ListContentRequest {
    string prefix = 1;
    int32 limit = 2;
    string cursor = 3;
}
message ListContentResponse {
    repeated ContentItem items = 1;
    string next_cursor = 2;
}
message ContentItem {
    string key = 1;
    string content_type = 2;
    bytes body = 3;
    map<string, string> metadata = 4;
}
```

- [ ] **Step 2: Add GetContent/ListContent to WebService in web.proto**
- [ ] **Step 3: Update buf.yaml to include new proto directory** (check `api/proto/buf.yaml` for module config)
- [ ] **Step 4: Run task proto to regenerate**
- [ ] **Step 5: Run buf lint to verify**
- [ ] **Step 6: Commit**

`feat(proto): add ContentService and content RPCs to WebService`

---

### Task 18: Core ContentService implementation

**Files:**

- Create: `internal/grpc/content_service.go`
- Create: `internal/grpc/content_service_test.go`

**Context:** gRPC server implementation that reads from ContentStore.

- [ ] **Step 1: Write failing tests**
- [ ] **Step 2: Implement ContentService**
- [ ] **Step 3: Wire into core gRPC server**
- [ ] **Step 4: Run tests, verify pass**
- [ ] **Step 5: Commit**

`feat(grpc): implement ContentService`

---

### Task 19: Gateway WebService content proxying

**Files:**

- Modify: `internal/web/handler.go`

**Context:** The gateway's WebService proxies GetContent/ListContent to the core's ContentService.

- [ ] **Step 1: Add content client to gateway handler**
- [ ] **Step 2: Implement GetContent/ListContent proxy methods**
- [ ] **Step 3: Write tests**
- [ ] **Step 4: Commit**

`feat(web): proxy content RPCs through gateway WebService`

---

### Task 20: ServiceProxy content methods

**Files:**

- Modify: `internal/plugin/service_proxy.go`
- Modify: `internal/plugin/service_proxy_impl.go`
- Modify: `internal/plugin/parity_test.go`

**Context:** Add read-only GetContent/ListContent to ServiceProxy. Update parity test table.

- [ ] **Step 1: Add methods to ServiceProxy interface**
- [ ] **Step 2: Implement in ServiceProxyImpl**
- [ ] **Step 3: Update parity test table**
- [ ] **Step 4: Run task test && task lint**
- [ ] **Step 5: Commit**

`feat(plugin): add GetContent/ListContent to ServiceProxy`

---

## Chunk 5: Landing Page + Theme Integration

### Task 21: Add marked + DOMPurify dependencies

**Files:**

- Modify: `web/package.json`

- [ ] **Step 1: Install dependencies**

Run: `cd web && pnpm add marked dompurify && pnpm add -D @types/dompurify`

- [ ] **Step 2: Commit**

`build(web): add marked and DOMPurify for content rendering`

---

### Task 22: Theme store overrides from ContentStore

**Files:**

- Modify: `web/src/lib/stores/themeStore.ts`
- Modify: `web/src/routes/+layout.ts`
- Create: `web/src/lib/stores/contentStore.ts`

**Context:** Fetch theme overrides and custom themes from ContentStore via RPC. Three-layer theme resolution: bundled → custom → overrides.

- [ ] **Step 1: Create contentStore.ts helper**

Thin wrapper around the ConnectRPC content client. Provides `getContent(key)` and `listContent(prefix)`.

- [ ] **Step 2: Add applyOverrides to themeStore**

New function that merges custom themes and overrides onto bundled themes.

- [ ] **Step 3: Read existing +layout.ts and +layout.svelte to understand current load pattern**

The existing `+layout.ts` has `ssr = false` and `prerender = false`. The existing `+layout.svelte` calls `restoreSession()` on mount. Theme fetching MUST integrate with this existing setup.

- [ ] **Step 4: Fetch themes in +layout.ts load function**

Add a `load` export that calls `listContent("theme.")`, pass to theme store. Ensure SSR=false context is respected.

- [ ] **Step 5: Verify existing theme toggle still works**
- [ ] **Step 6: Commit**

`feat(web): load theme overrides from ContentStore`

---

### Task 23: Data-driven landing page

**Files:**

- Modify: `web/src/routes/+page.svelte`
- Create: `web/src/routes/+page.ts`
- Create: `web/src/lib/components/MarkdownContent.svelte`
- Create: `web/src/lib/components/FeatureCard.svelte`

**Context:** Replace the stub landing page with a data-driven component that renders from ContentStore.

- [ ] **Step 1: Create +page.ts load function**

Calls `listContent("landing.")`, groups by section, passes as props.

- [ ] **Step 2: Create MarkdownContent component**

Renders markdown body with `marked`, sanitizes with DOMPurify.

- [ ] **Step 3: Create FeatureCard component**

Renders a single feature card (title, icon, body markdown).

- [ ] **Step 4: Rewrite +page.svelte**

Hero section (title + tagline from metadata), pitch (markdown body), feature grid (sorted by order metadata), connect info (markdown body), CTAs (Login/Register/Guest — structural, not content).

- [ ] **Step 5: Verify with manual test** (start dev server, check rendering)
- [ ] **Step 6: Commit**

`feat(web): data-driven landing page from ContentStore`

---

## Chunk 6: Setting Content + E2E Tests

### Task 24: Crossroads setting plugin

**Files:**

- Create: `plugins/setting-crossroads/plugin.yaml`
- Create: `plugins/setting-crossroads/content/landing/hero.md`
- Create: `plugins/setting-crossroads/content/landing/pitch.md`
- Create: `plugins/setting-crossroads/content/landing/features/01-storytelling.md`
- Create: `plugins/setting-crossroads/content/landing/features/02-any-character.md`
- Create: `plugins/setting-crossroads/content/landing/features/03-web-and-telnet.md`
- Create: `plugins/setting-crossroads/content/landing/features/04-build-your-corner.md`
- Create: `plugins/setting-crossroads/content/landing/connect.md`
- Create: `plugins/setting-crossroads/content/motd/welcome.md`
- Create: `plugins/setting-crossroads/world/locations.yaml`
- Create: `plugins/setting-crossroads/world/exits.yaml`
- Create: `plugins/setting-crossroads/theme.json`

**Context:** The default "Crossroads" setting. All content from the spec. World seeds three locations with exits.

- [ ] **Step 1: Create plugin.yaml manifest**
- [ ] **Step 2: Create all content markdown files** (per spec)
- [ ] **Step 3: Create world/locations.yaml** with The Nexus (well-known ULID), The Threshold, The Doors Market
- [ ] **Step 4: Create world/exits.yaml** with four exits per spec
- [ ] **Step 5: Create theme.json** with crossroads dark overrides
- [ ] **Step 6: Verify manifest validates**

Run: `task generate:schema && task lint`

- [ ] **Step 7: Commit**

`feat(setting): add crossroads default setting plugin`

---

### Task 25: Skeleton setting plugin

**Files:**

- Create: `plugins/setting-skeleton/plugin.yaml`
- Create: `plugins/setting-skeleton/content/landing/hero.md`
- Create: `plugins/setting-skeleton/world/locations.yaml`

**Context:** Minimal setting — one location, bare landing page.

- [ ] **Step 1: Create all files** per spec
- [ ] **Step 2: Commit**

`feat(setting): add skeleton setting plugin`

---

### Task 26: Integration tests (Ginkgo + testcontainers)

**Files:**

- Create: `test/integration/content/content_suite_test.go`
- Create: `test/integration/content/content_integration_test.go`

**Context:** Integration tests per spec: setting plugin bootstrap with real Postgres, world seeds queryable, idempotency verified. Uses Ginkgo/Gomega + `//go:build integration` tag (same pattern as `test/integration/world/`).

- [ ] **Step 1: Write Ginkgo suite setup** (testcontainers Postgres, run migrations)

- [ ] **Step 2: Test: setting bootstrap seeds content**

`ListContent("landing.")` returns expected items after crossroads bootstrap.

- [ ] **Step 3: Test: world seeds create locations and exits**

Query locations via WorldService after bootstrap. Verify The Nexus, The Threshold, The Doors Market exist with expected exits.

- [ ] **Step 4: Test: operator-edited content survives re-bootstrap**

Put a custom `landing.hero` content item, re-run bootstrap, verify custom content preserved (idempotency).

- [ ] **Step 5: Run task test:int**
- [ ] **Step 6: Commit**

`test(integration): add content store and setting bootstrap tests`

---

### Task 27: E2E Playwright tests

**Files:**

- Modify: `web/e2e/landing.spec.ts` (already exists — audit existing tests, then expand)
- Modify: existing terminal E2E tests if world seed changes affect them

**Context:** Comprehensive landing page verification per spec.

- [ ] **Step 1: Write landing page content tests**

- Hero displays "The Crossroads" title and "Where worlds collide" tagline
- Pitch section contains key phrases from content
- All four feature cards present with correct titles and order
- Feature card bodies are non-empty
- Connect section displays connection info

- [ ] **Step 2: Write navigation tests**

- Login CTA navigates to `/login`
- Register CTA navigates to `/register`
- Guest CTA authenticates and navigates to `/terminal`
- All `<a>` elements have valid hrefs (no 404s)

- [ ] **Step 3: Write theme tests**

- Dark theme applied by default
- Theme overrides from content store applied (check CSS variable values)

- [ ] **Step 4: Write graceful degradation test**

- Empty content store renders fallback (no crash)

- [ ] **Step 5: Run task test:e2e**
- [ ] **Step 6: Commit**

`test(e2e): comprehensive landing page verification`

---

## Post-Implementation Checklist

- [ ] All unit tests pass: `task test`
- [ ] All linters pass: `task lint`
- [ ] Integration tests pass: `task test:int`
- [ ] E2E tests pass: `task test:e2e`
- [ ] `seed` subcommand removed, `holomush seed` errors gracefully
- [ ] `holomush core --setting=crossroads` boots with content seeded
- [ ] `holomush core --setting=skeleton` boots with minimal content
- [ ] Landing page renders all sections from content store
- [ ] Theme overrides applied correctly
- [ ] Operator can edit content files and re-bootstrap
- [ ] Parity test table updated with GetContent/ListContent
- [ ] License headers on all new files
