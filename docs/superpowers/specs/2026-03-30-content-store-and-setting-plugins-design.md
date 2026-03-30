<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Content Store and Setting Plugins Design

## Overview

HoloMUSH needs a data-driven landing page and a way for operators to customize
every aspect of their game's presentation. This spec introduces two connected
systems: a general-purpose **ContentStore** for managing game content, and a
**setting plugin type** that bootstraps a world (content, locations, theme) on
first boot.

The default setting ships a "Crossroads" world — a nexus where universes
collide — giving new operators a playable game out of the box. Operators can
swap settings, edit content with any text editor, or eventually manage
everything through an admin UI.

## Goals

- Replace the stub landing page with real, data-driven content
- Provide a ContentStore interface that any part of the system can use
- Make game content operator-customizable without code changes
- Ship a compelling default world that demonstrates the platform
- Ensure a clean migration path to a full CMS admin UI

## Non-Goals

- Admin UI for content editing (post-v0.1)
- Full CMS with drag-and-drop section management (post-v0.1)
- User-generated content through the ContentStore (players use in-game building commands)
- Asset pipeline or image optimization

---

## ContentStore Interface

### Design

A general-purpose content store that decouples content producers (setting
bootstrapper, admin API) from consumers (landing page, MOTD, help system).

```go
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

// ContentStore is the interface all backends implement.
type ContentStore interface {
    Get(ctx context.Context, key string) (*ContentItem, error)
    List(ctx context.Context, prefix string, opts ListOptions) (*ListResult, error)
    Put(ctx context.Context, item *ContentItem) error
    Delete(ctx context.Context, key string) error
}
```

### Key Conventions

Keys use dot-separated hierarchical namespaces:

| Pattern | Purpose |
| --- | --- |
| `landing.hero` | Landing page hero section |
| `landing.pitch` | Setting description |
| `landing.features.*` | Feature cards (ordered by metadata) |
| `landing.connect` | Connection information |
| `theme.custom.<name>` | Full custom theme definition (JSON) |
| `theme.overrides.<base>` | Sparse overlay on a base theme (JSON) |
| `theme.default` | Default theme name for first visit (text) |
| `motd.welcome` | Message of the day |
| `help.*` | Custom help content |

### Routing Store

A `RoutingContentStore` delegates to backend stores based on content type.
This allows text content in Postgres and binary assets in file/object storage.

```go
// RoutingContentStore dispatches to backends by content type.
type RoutingContentStore struct {
    routes   map[string]ContentStore // content type → backend
    fallback ContentStore            // default backend
}
```

**v0.1 backends:**

- `PostgresContentStore` — text, markdown, JSON content. Stored in a
  `content_items` table.
- `FileContentStore` — images and binary assets. Serves from a data directory
  under XDG paths.

**Default routing:**

| Content Type | Backend |
| --- | --- |
| `text/markdown` | Postgres |
| `application/json` | Postgres |
| `text/plain` | Postgres |
| `image/*` | File |
| `application/octet-stream` | File |

Post-v0.1, operators MAY swap the file backend for S3/GCS/Azure Blob via
configuration.

**`List` semantics:** `RoutingContentStore.List` MUST query ALL backends and
merge results, sorted by key. This is necessary because a `List("landing.")`
call may match markdown items in Postgres and image-ref items in file storage.

The routing store MUST support glob patterns for content type matching (e.g.,
`image/*` matches `image/png`, `image/jpeg`). Exact matches take precedence
over glob matches.

**Missing route:** When `Get` or `Put` is called for a content type with no
matching route, the `fallback` backend handles the request. If `fallback` is
nil, the store MUST return an error.

### Database Schema

```sql
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

The `text_pattern_ops` index enables efficient prefix queries for
`List("landing.")`. The GIN index on `search_vector` supports full-text
search (post-v0.1 admin UI).

`PostgresContentStore.Put` MUST populate `search_vector` using
`to_tsvector('english', ...)` when the content type starts with `text/` or
equals `application/json`. For binary content types (`image/*`,
`application/octet-stream`), `search_vector` MUST be set to NULL. No database
triggers — all logic lives in the Go store implementation.

### ServiceProxy Extension

Plugins MAY read content through new read-only ServiceProxy methods:

```go
// --- Content (read-only) ---

// GetContent retrieves a content item by key.
GetContent(ctx context.Context, key string) (*ContentItem, error)

// ListContent returns content items matching a key prefix with pagination.
ListContent(ctx context.Context, prefix string, opts ListOptions) (*ListResult, error)
```

These methods MUST be added to the ServiceProxy interface, implemented in
ServiceProxyImpl, and reflected in the parity test table.

**Write access is NOT exposed through ServiceProxy.** The `SettingBootstrapper`
takes a direct `ContentStore` dependency (see below). This prevents arbitrary
plugins from overwriting landing page content or theme settings. A future
admin API will provide authorized write access.

---

## Setting Plugin Type

### Overview

A new plugin type `setting` that bootstraps a game world on first boot.
Setting plugins provide content, seed locations, and theme customizations.

### Manifest Schema

```yaml
name: setting-crossroads
version: 1.0.0
type: setting

setting:
  display_name: "The Crossroads"
  description: "A nexus where worlds collide"
  content_dir: content/    # Markdown files with frontmatter
  world_dir: world/        # Seed locations/exits (YAML)
  theme: theme.json        # Theme color/font overrides
```

**Manifest validation changes:**

A new `TypeSetting` constant MUST be added to the plugin type enum in
`internal/plugin/manifest.go` alongside `TypeCore`, `TypeLua`, and `TypeBinary`.

The `Manifest.Validate()` method MUST be updated:

- Accept `type: setting` as valid
- Require a `setting:` stanza when `type` is `setting`
- Reject `commands:`, `lua-plugin:`, and `binary-plugin:` stanzas when
  `type` is `setting` (setting plugins MUST NOT declare commands)
- The JSON schema tag on `Manifest.Type` MUST be updated to include `setting`

A new `SettingConfig` struct MUST be added to the manifest:

```go
type SettingConfig struct {
    DisplayName string `yaml:"display_name" json:"display_name"`
    Description string `yaml:"description" json:"description"`
    ContentDir  string `yaml:"content_dir" json:"content_dir"`
    WorldDir    string `yaml:"world_dir" json:"world_dir"`
    Theme       string `yaml:"theme" json:"theme"`
}
```

### Content Format

Content files are **markdown with YAML frontmatter**. Subdirectories provide
organization; the `key` in frontmatter is authoritative for storage.

```text
plugins/setting-crossroads/
  plugin.yaml
  content/
    landing/
      hero.md
      pitch.md
      features/
        01-storytelling.md
        02-any-character.md
        03-web-and-telnet.md
        04-build-your-corner.md
      connect.md
    motd/
      welcome.md
  world/
    locations.yaml
    exits.yaml
  theme.json
```

**Example content file (`landing/hero.md`):**

```markdown
---
key: landing.hero
content_type: text/markdown
title: "The Crossroads"
tagline: "Where worlds collide"
---
```

**Example feature card (`landing/features/01-storytelling.md`):**

```markdown
---
key: landing.features.storytelling
content_type: text/markdown
title: "Collaborative Storytelling"
icon: quill
order: 1
---

Create scenes, invite participants, and write stories together in real
time. Set privacy levels and build narratives that matter.
```

### BootstrapPlugin Interface

Setting plugins are **bootstrap-only** — they run once during startup, not at
runtime. Rather than implementing the `Host` interface (which is for runtime
dispatch), bootstrap-only plugins implement a dedicated interface:

```go
// BootstrapPlugin is implemented by plugins that run once during server
// startup. They seed data, create initial state, or perform one-time
// setup. They are NOT runtime plugins — no commands, events, or lifecycle.
type BootstrapPlugin interface {
    // Priority determines execution order. Lower values run first.
    // Use the BootstrapPriority constants.
    Priority() int

    Bootstrap(ctx context.Context, manifest *Manifest, pluginDir string) error
}

// Bootstrap priority levels. Plugins at the same priority run in
// discovery order (lexicographic by plugin name).
const (
    BootstrapPrioritySchema  = 100  // DB migrations, schema changes
    BootstrapPriorityPolicy  = 200  // ABAC policies, access control seeds
    BootstrapPriorityWorld   = 300  // Locations, exits, world state (setting plugins)
    BootstrapPriorityContent = 400  // Content store items, themes
    BootstrapPriorityAlias   = 500  // Command aliases (needs registry populated)
)
```

The plugin manager discovers bootstrap plugins by manifest type and calls
`Bootstrap` during startup. They are NOT registered in the runtime dispatch
table.

**v0.1 implementations:**

| Bootstrapper | Priority | Replaces |
| --- | --- | --- |
| `MigrationBootstrapper` | 100 (Schema) | Auto-migration in `core.go`, migration step in deleted `seed` command |
| `PolicyBootstrapper` | 200 (Policy) | Inline `policy.Bootstrap()` call in `core.go` (~line 262) |
| `SettingBootstrapper` | 300 (World) | Deleted `seed` command's location creation + new content/theme seeding |
| `AdminBootstrapper` | 400 (Content) | Inline `bootstrap.SeedAdmin()` call in `core.go` (~line 365) |
| `AliasBootstrapper` | 500 (Alias) | Inline `bootstrap.SeedSystemAliases()` call in `core.go` (~line 523) |

Each adapter wraps the existing bootstrap function (no logic changes) and
exposes it through the `BootstrapPlugin` interface. The three inline
bootstrap blocks in `core.go` are replaced by a single
`bootstrapRunner.RunAll(ctx)` call.

**Adapter pattern:** Each bootstrapper is a thin struct in
`internal/bootstrap/` that holds the dependencies the existing function
needs and delegates to it from `Bootstrap()`. The existing functions
(`SeedSystemAliases`, `SeedAdmin`, `policy.Bootstrap`) MUST NOT be modified
— only wrapped.

```go
// Example: AliasBootstrapper wraps SeedSystemAliases.
type AliasBootstrapper struct {
    repo  AliasSeeder
    cache *command.AliasCache
}

func (b *AliasBootstrapper) Priority() int { return BootstrapPriorityAlias }

func (b *AliasBootstrapper) Bootstrap(ctx context.Context, _ *plugins.Manifest, _ string) error {
    return SeedSystemAliases(ctx, b.repo, b.cache)
}
```

**Bootstrap runner:** A new `BootstrapRunner` in `internal/plugin/` collects
all `BootstrapPlugin` implementations, sorts by `Priority()`, and runs them
sequentially. Any error is fatal — the server MUST NOT start with a failed
bootstrap step.

```go
type BootstrapRunner struct {
    plugins []BootstrapPlugin
    logger  *slog.Logger
}

func (r *BootstrapRunner) Register(p BootstrapPlugin)
func (r *BootstrapRunner) RunAll(ctx context.Context) error
```

### SettingBootstrapper

The first `BootstrapPlugin` implementation. Processes a setting plugin manifest
and seeds content, world data, and theme overrides on first boot.

```go
// SettingBootstrapper processes a setting plugin manifest and seeds
// content, world data, and theme overrides on first boot.
//
// Lives in internal/bootstrap alongside SeedSystemAliases and SeedAdmin.
type SettingBootstrapper struct {
    contentStore ContentStore
    worldService *world.Service  // concrete type — bootstrap needs CreateLocation/CreateExit
    logger       *slog.Logger
}
```

Compile-time check: `var _ plugins.BootstrapPlugin = (*SettingBootstrapper)(nil)`

`SettingBootstrapper.Priority()` returns `BootstrapPriorityWorld` (300). It
seeds both world state and content in a single pass since the setting plugin
owns both. Content seeding at priority 300 (rather than 400) is acceptable
because setting content has no dependencies on other bootstrap plugins.

The `SettingBootstrapper` takes direct dependencies on `ContentStore` and
`WorldService`. It does NOT go through `ServiceProxy` because:

- It needs write access to ContentStore (which ServiceProxy MUST NOT expose)
- It runs during startup before the plugin dispatch loop
- It is not a runtime plugin — no commands, events, or lifecycle

**Bootstrap behavior:**

1. Walk `content_dir`, parse each `.md` file (frontmatter + body)
2. For each file, call `ContentStore.Put` with the extracted key, content type,
   body, and metadata
3. Parse `world_dir/locations.yaml` and create seed locations via WorldService
4. Parse `world_dir/exits.yaml` and create exits between locations
5. Apply `theme.json` overrides to the content store as `theme.*` keys

**Idempotency:** The SettingBootstrapper MUST skip content items and locations
that already exist. It MUST NOT overwrite operator customizations. This follows
the same pattern as alias seeding — check before create.

**Constraint:** Only ONE setting plugin MUST be active at a time. The active
setting is recorded in the `bootstrap_metadata` table on first boot.

### Removal of Seed Command

The existing `cmd/holomush/seed.go` creates a single hardcoded "The Nexus"
location and runs database migrations. Both responsibilities move into the
bootstrap plugin system:

- **Database migrations** become a `MigrationBootstrapper` at
  `BootstrapPrioritySchema` (100). This replaces both the `seed` command's
  migration step and the auto-migration in `core.go` startup.
- **World seeding** moves to the `SettingBootstrapper` at
  `BootstrapPriorityWorld` (300).

`cmd/holomush/seed.go` MUST be deleted. The `seed` subcommand is removed from
the CLI. All bootstrap logic runs automatically during `core` startup via
the ordered `BootstrapPlugin` system.

The well-known starting location ULID (`01HZN3XS000000000000000000`) SHOULD
be preserved by the crossroads setting plugin for backward compatibility with
existing deployments.

### Bootstrap Metadata Table

```sql
CREATE TABLE IF NOT EXISTS bootstrap_metadata (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE bootstrap_metadata IS 'Tracks one-time bootstrap state (active setting, schema version, etc.)';
```

The active setting is stored as:

| Key | Value | Purpose |
| --- | --- | --- |
| `active_setting` | `crossroads` | Which setting plugin was bootstrapped |
| `setting_version` | `1.0.0` | Version of the setting that was applied |

### CLI Integration

The `--setting` and `--reset-setting` flags MUST be added to the `core`
subcommand in `cmd/holomush/core.go`, consistent with existing CLI flags
like `--session-ttl` and `--grpc-addr`.

```text
--setting=crossroads    (default)
--setting=skeleton      (bare minimum)
--reset-setting         (force re-bootstrap from setting plugin)
```

**Behavior:**

- On first boot (no `active_setting` in `bootstrap_metadata`), the
  `--setting` flag determines which setting plugin to bootstrap. The value
  is stored in `bootstrap_metadata`.
- On subsequent boots, the stored `active_setting` is used. The `--setting`
  flag is ignored with a log warning if it differs from the stored value.
- `--reset-setting` clears the stored value and re-runs bootstrap with the
  current `--setting` flag. This MUST NOT delete operator-customized content
  — it only re-seeds missing items.

---

## Gateway API

### New RPCs

Add to the existing `WebService` in the gateway proto:

```protobuf
// GetContent retrieves a single content item by key.
rpc GetContent(GetContentRequest) returns (GetContentResponse);

// ListContent returns all content items matching a key prefix.
rpc ListContent(ListContentRequest) returns (ListContentResponse);
```

These are public (no authentication required) for landing page content. The
gateway proxies to a new core server RPC that reads from ContentStore.

**Request/Response messages:**

```protobuf
message GetContentRequest {
  string key = 1;
}

message GetContentResponse {
  ContentItem item = 1;
}

message ListContentRequest {
  string prefix = 1;
  int32 limit = 2;          // 0 = no limit (returns all matches)
  string cursor = 3;        // empty = start from beginning
}

message ListContentResponse {
  repeated ContentItem items = 1;
  string next_cursor = 2;   // empty = no more results
}

message ContentItem {
  string key = 1;
  string content_type = 2;
  bytes body = 3;
  map<string, string> metadata = 4;
}
```

The `updated_at` field is intentionally omitted from the public proto message.
It is an internal implementation detail. A future admin API proto SHOULD
include it for optimistic locking and cache invalidation.

### Core Server RPC

A new `ContentService` on the core gRPC server:

```protobuf
service ContentService {
  rpc GetContent(GetContentRequest) returns (GetContentResponse);
  rpc ListContent(ListContentRequest) returns (ListContentResponse);
}
```

The gateway's `WebService` proxies `GetContent`/`ListContent` to this service.

---

## Landing Page (Web Client)

### Data Flow

1. `+page.ts` (SvelteKit load function) calls `ListContent("landing.")` via
   ConnectRPC
2. Groups items by key prefix into sections
3. Passes grouped data to `+page.svelte` as props
4. Component renders each section from the content data

### Layout

| Section | Content Key | Rendering |
| --- | --- | --- |
| Hero | `landing.hero` | Title + tagline from metadata, CTAs are structural |
| Pitch | `landing.pitch` | Rendered markdown body |
| Features | `landing.features.*` | Grid of cards, ordered by `order` metadata |
| Connect | `landing.connect` | Rendered markdown with connection details |

**CTAs (Login / Register / Guest)** are part of the component structure, not
content. They link to `/login`, `/register`, and trigger guest auth
respectively. These remain in the Svelte component.

### Markdown Rendering

Use a lightweight markdown renderer (`marked` or equivalent) for content
bodies. The renderer MUST sanitize output to prevent XSS from admin-authored
content. Use DOMPurify on rendered HTML.

### Theme Integration

The existing theme store (`themeStore.ts`) provides CSS variables from bundled
JSON files (`default-dark.json`, `default-light.json`).

The theme system supports three layers:

1. **Bundled themes** — `default-dark` and `default-light`, always available.
2. **Custom themes** — Full theme definitions stored in ContentStore as
   `theme.custom.<name>` (`application/json`). Same structure as
   `default-dark.json` — all color keys MUST be present. Operators and setting
   plugins can add entirely new themes.
3. **Overrides** — Sparse overlays stored as `theme.overrides.<base>`
   (`application/json`). Merged onto the named base theme (bundled or custom).
   Only the keys being changed need to be present.

**Content keys:**

| Key | Purpose |
| --- | --- |
| `theme.custom.<name>` | Full custom theme (all color keys) |
| `theme.overrides.<base>` | Sparse overlay on a base theme |
| `theme.default` | Which theme to use on first visit (text, e.g. `default-dark`) |

**Setting plugin `theme.json` format:**

```json
{
  "default": "default-dark",
  "overrides": {
    "default-dark": {
      "say.speaker": "#ff9800",
      "background": "#1a1a2e"
    }
  },
  "custom": {
    "crossroads-dark": {
      "say.speaker": "#b39ddb",
      "say.speech": "#e0e0e0",
      "...all 25 keys..."
    }
  }
}
```

The `SettingBootstrapper` writes each entry to the ContentStore:
- `theme.default` → `"default-dark"` (text)
- `theme.overrides.default-dark` → the override object (JSON)
- `theme.custom.crossroads-dark` → the full theme (JSON)

**Theme resolution (client):**

1. Player selects a theme (stored in `localStorage`)
2. Look up base theme: bundled OR `theme.custom.<name>` from ContentStore
3. Look up `theme.overrides.<base>` from ContentStore
4. Merge: `{ ...baseTheme, ...overrides }`
5. Apply as CSS variables

**Loading:** The root layout's `load` function (`+layout.ts`) MUST call
`ListContent("theme.")` during initialization to fetch all custom themes and
overrides in one request. These are passed to the theme store, which merges
them with bundled themes to build the available theme list.

If the `ListContent` call fails or returns no results, the theme store MUST
fall back to bundled themes without error.

---

## Default Setting: The Crossroads

### Premise

The doors opened without warning — rifts between realities tearing through
the fabric of a thousand worlds. Now the Crossroads stands at the center of
it all: a city of impossible architecture where displaced travelers, exiled
gods, and stranded explorers forge new lives in the spaces between what was
and what might be.

### Landing Page Content

**Hero:**

- Title: "The Crossroads"
- Tagline: "Where worlds collide"

**Pitch:**

> The doors opened without warning — rifts between realities tearing through
> the fabric of a thousand worlds. Now the Crossroads stands at the center of
> it all: a city of impossible architecture where displaced travelers, exiled
> gods, and stranded explorers forge new lives in the spaces between what was
> and what might be.

**Feature Cards:**

1. **Collaborative Storytelling** — Create scenes, invite participants, and
   write stories together in real time. Set privacy levels and build
   narratives that matter.
2. **Any Character, Any World** — Your backstory is your own. Step through a
   door from anywhere. Sci-fi soldier, wandering spirit, displaced noble —
   the Crossroads welcomes all.
3. **Web & Telnet** — Play from your browser or connect with any MU\* client.
   Same world, same conversations, your choice of interface.
4. **Build Your Corner** — Claim a space, describe it, connect it to the grid.
   The Crossroads grows with you.

**Connect Info:**

- Web: auto-detected from current URL
- Telnet: configured by operator (`--telnet-addr`)

### Seed World

The crossroads setting seeds three locations. The Nexus SHOULD use the
well-known ULID from the existing `seed.go` (`01HZN3XS000000000000000000`)
for backward compatibility with `--guest-start-location` defaults.

| Location | Description |
| --- | --- |
| The Nexus | Central hub. A vast circular plaza beneath an impossible sky where fragments of other worlds drift like clouds. Doorways line the perimeter — some stable, some flickering. |
| The Threshold | Arrival point for new characters. A shimmering archway where new arrivals step through, disoriented and blinking. |
| The Doors Market | A sprawling market hall lined with freestanding doorways. Each opens onto a different world's bazaar — step through one for clockwork trinkets from a steam-powered empire, another for spell components from a realm of living magic, a third for salvaged tech from a post-collapse orbital. Vendors haggle across thresholds in a dozen languages. |

**Exits:**

- The Threshold → The Nexus ("plaza", "nexus")
- The Nexus → The Doors Market ("market", "doors market")
- The Nexus → The Threshold ("threshold", "arrival")
- The Doors Market → The Nexus ("plaza", "nexus")

### Skeleton Setting

The `setting-skeleton` provides the absolute minimum:

- One location: "The Void" — "An empty expanse. Everything starts somewhere."
- No landing page content beyond title: "New Game" / tagline: "A HoloMUSH World"
- Default theme (no overrides)

---

## Testing

### Unit Tests

- `PostgresContentStore`: CRUD operations, prefix listing, upsert behavior
- `FileContentStore`: read/write binary assets, path traversal prevention
- `RoutingContentStore`: correct delegation by content type, fallback behavior,
  cross-backend `List` merge
- `SettingBootstrapper`: content file parsing (frontmatter extraction),
  idempotent bootstrap, world seed creation
- Markdown frontmatter parser: valid files, missing frontmatter, malformed YAML

### Integration Tests

- Setting plugin bootstrap with real Postgres (testcontainers):
  `ListContent("landing.")` returns expected items after crossroads bootstrap
- World seeds create locations and exits that are queryable via WorldService
- Content overrides: operator-edited content survives re-bootstrap (idempotency)

### E2E Tests (Playwright)

**Landing page content verification:**

- Hero section displays title ("The Crossroads") and tagline ("Where worlds collide") from content store
- Pitch section renders markdown body text (verify key phrases present)
- All four feature cards are present with correct titles, in correct order
- Each feature card body text is rendered (not empty)
- Connect section displays connection information

**Navigation and links:**

- Login CTA navigates to `/login`
- Register CTA navigates to `/register`
- Guest CTA triggers guest auth and navigates to `/terminal`
- Every `<a>` element on the landing page MUST have a valid `href` that does not 404
- No broken links (crawl all hrefs, verify non-404 response)

**Theme:**

- Landing page respects active theme (dark mode by default)
- Theme overrides from content store are applied (verify CSS variable values)
- Theme toggle (if present) switches between available themes

**Content-driven rendering:**

- If content store returns empty results, landing page renders gracefully (no crash, shows fallback or empty state)
- Content changes are reflected on page reload (modify content via API or direct DB, reload, verify)

---

## Migration Path to CMS (Post-v0.1)

The design ensures a clean evolution to a full admin UI:

1. **ContentStore interface is stable.** The admin UI reads/writes through
   the same `Get`/`List`/`Put`/`Delete` methods. No schema migration needed.
2. **Markdown content is admin-UI native.** A syntax-highlighted editor with
   live preview renders and saves markdown directly. No format translation.
3. **Section management** becomes a matter of creating/deleting content items
   with appropriate keys. The landing page component already renders whatever
   `ListContent("landing.")` returns.
4. **Image uploads** go through `FileContentStore` (or S3 backend). The
   `RoutingContentStore` handles delegation transparently.
5. **No operator migration.** Content already lives in Postgres. The admin UI
   is a new frontend for the same data.
