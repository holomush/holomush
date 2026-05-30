<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Scenes Phase 8 — Scene Board + Content Warnings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the scene board + a game-overridable content-warning model, on an **owner-partitioned** settings substrate that exposes typed, structurally-isolated per-plugin settings to plugins without the host learning any plugin's vocabulary.

**Architecture:** Substrate-first. Phase 1 adds a list type and the owner-partitioning model to `internal/settings` (host stays domain-ignorant via an opaque `Plugins` passthrough). Phase 2 exposes it over a `PluginHostService` RPC whose owner is **bound from the authenticated plugin name** (`pluginHostServiceServer.pluginName`), with Go+Lua parity. Phases 3–5 are core-scenes consumers: the CW taxonomy/vocabulary (read-time default fallback), the `ListScenes` board query (server-side filtering, union CW block), and the `scenes` command.

**Tech Stack:** Go, gRPC/buf, PostgreSQL (host `internal/store/migrations`), gopher-lua hostfuncs (`internal/plugin/lua`), hashicorp/go-plugin host service (`internal/plugin/goplugin`), testify + Ginkgo (`internal/testsupport/integrationtest`).

**Spec:** `docs/superpowers/specs/2026-05-29-scenes-phase-8-board-content-warnings-design.md`
**Design bead:** `holomush-iokti`

**Model labels (for plan-to-beads, Rule 5):** Phases 1–2 substrate tasks → `model:opus` (interface design, boundary-sensitive, ABAC). Phases 3–5 → `model:sonnet` (pattern-mirroring against grounded examples).

---

## Conventions for every task

- Tests: `task test -- ./<package>/`; integration `task test:int`. Never bare `go test`.
- Commit: `jj commit -m "type(scope): desc (holomush-iokti)"`.
- SPDX headers on new `.go`/`.sql` (via `task fmt`); `slog.*Context(ctx,…)`; `oops` for errors.
- After each task: `task lint && task test -- ./<touched-package>/`.

---

## Phase 1: Settings substrate — list type + owner partitioning

### Task 1: `StringSliceN` read accessor across all backings

**Files:**

- Modify: `internal/settings/settings.go` (add to `Settings` interface)
- Modify: `internal/settings/player.go:76` (`jsonMapSettings`), `:168` (`emptySettings`)
- Modify: `internal/settings/game.go:56` (`postgresGameSettings`)
- Modify: `internal/settings/chain.go:23` (`Chain`)
- Test: `internal/settings/player_test.go`, `game_test.go`, `chain_test.go`

- [ ] **Step 1: Write failing tests** (one per backing)

```go
// player_test.go
func TestJSONMapSettingsStringSliceNReturnsNativeArray(t *testing.T) {
	s := settings.NewJSONMapSettingsForTest(map[string]json.RawMessage{
		"core.cw_block": json.RawMessage(`["violence","death"]`)}, true) // (data, validateNamespace)
	got, ok := s.StringSliceN(context.Background(), "core.cw_block")
	require.True(t, ok)
	assert.Equal(t, []string{"violence", "death"}, got)
}
func TestJSONMapSettingsStringSliceNScalarReturnsFalse(t *testing.T) {
	s := settings.NewJSONMapSettingsForTest(map[string]json.RawMessage{
		"core.cw_block": json.RawMessage(`"violence"`)}, true)
	_, ok := s.StringSliceN(context.Background(), "core.cw_block")
	assert.False(t, ok)
}
// game_test.go
func TestGameSettingsStringSliceNDecodesJSONArrayString(t *testing.T) {
	store := newMockSystemInfoStore()
	require.NoError(t, store.SetSystemInfo(context.Background(), "core.x", `["a","b"]`))
	got, ok := settings.NewGameSettings(store).StringSliceN(context.Background(), "core.x")
	require.True(t, ok)
	assert.Equal(t, []string{"a", "b"}, got)
}
```

(Use `core.*` keys — `core` is already in `RegisteredNamespaces`, so host-partition validation passes.)

- [ ] **Step 2: Run, verify fail.** `task test -- ./internal/settings/` → `StringSliceN undefined`.

- [ ] **Step 3: Add to interface + backings**

`settings.go` — add to `Settings`:

```go
	// StringSliceN returns a string-list setting; unset or non-list → (nil,false).
	StringSliceN(ctx context.Context, key string) ([]string, bool)
```

`player.go` `jsonMapSettings` (note `validateNamespace` gate — see Task 2; for Task 1 keep the existing unconditional `ValidateNamespace`, Task 2 adds the flag):

```go
func (j *jsonMapSettings) StringSliceN(ctx context.Context, key string) ([]string, bool) {
	if j.validateNamespace { // field added in Task 2; until then call ValidateNamespace directly
		if err := ValidateNamespace(key); err != nil {
			slog.DebugContext(ctx, "settings read: invalid namespace", "key", key, "error", err)
			return nil, false
		}
	}
	raw, ok := j.data[key]
	if !ok {
		return nil, false
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false // not a JSON array — never coerce a scalar
	}
	return out, true
}
```

> NB: Tasks 1 and 2 touch `jsonMapSettings` together. If implementing strictly serially, in Task 1 write `StringSliceN` with the existing unconditional `ValidateNamespace(key)` call, then Task 2 introduces the `validateNamespace` field and threads it through all five accessors. Prefer implementing Task 1 + Task 2 as one branch.

`game.go` `postgresGameSettings`:

```go
func (g *postgresGameSettings) StringSliceN(ctx context.Context, key string) ([]string, bool) {
	s, ok := g.StringN(ctx, key)
	if !ok { return nil, false }
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		slog.DebugContext(ctx, "game settings slice decode failed", "key", key, "error", err)
		return nil, false
	}
	return out, true
}
```

`player.go` `emptySettings`: `func (e *emptySettings) StringSliceN(context.Context, string) ([]string, bool) { return nil, false }`

`chain.go` `Chain`:

```go
func (c *Chain) StringSliceN(ctx context.Context, key string) ([]string, bool) {
	for _, s := range c.scopes {
		if v, ok := s.StringSliceN(ctx, key); ok { return v, true }
	}
	return nil, false
}
```

Add `slices`/`StringSliceN` to the `stubSettings` test double in `chain_test.go`.

- [ ] **Step 4: Run, verify pass.** `task test -- ./internal/settings/`

- [ ] **Step 5: Commit.** `jj commit -m "feat(settings): StringSliceN list accessor across backings (holomush-iokti)"`

---

### Task 2: Owner-partitioning — `Scoped`/`Writable` + `Owner()`/`Host()` + validation flag

**Files:**

- Modify: `internal/settings/settings.go` (add `Scoped`, `Writable`; change store `For` returns)
- Modify: `internal/settings/player.go` (`jsonMapSettings` gains `validateNamespace bool`; `PlayerSettings.For` returns `Scoped`; add `Owner`/`Host`)
- Create: `internal/settings/scoped.go` (the `scopedView` implementing `Scoped`)
- Test: `internal/settings/scoped_test.go`

**Design:** `Settings` (read) unchanged. A `scopedView` wraps a player's/character's/game's preference data and implements `Scoped`. `.Owner(name)` returns a `Writable` over the `plugins[name]` sub-map with `validateNamespace=false`. `.Host()` returns a `Writable` over the host keyspace with `validateNamespace=true`. For player/character scope, `.Host()` is a **deferred no-op** view (reads unset; writes error) — host player/character settings remain the typed `auth.PlayerPreferences` struct's job; no consumer needs the generic host partition at those scopes (spec §3.1). For game scope, `.Host()` is the real `system_info` keyspace.

- [ ] **Step 1: Write failing isolation tests**

```go
// scoped_test.go
func TestOwnerPartitionsAreIsolated(t *testing.T) {
	sc := settings.NewScopedForTest(map[string]json.RawMessage{}) // empty player prefs
	require.NoError(t, sc.Owner("core-scenes").SetStringSlice(ctx, "content.cw_block", []string{"violence"}))
	// other owner sees nothing:
	_, ok := sc.Owner("core-channels").StringSliceN(ctx, "content.cw_block")
	assert.False(t, ok)
	// same owner round-trips, WITHOUT a RegisteredNamespaces entry for "content":
	got, ok := sc.Owner("core-scenes").StringSliceN(ctx, "content.cw_block")
	require.True(t, ok)
	assert.Equal(t, []string{"violence"}, got)
}
```

- [ ] **Step 2: Run, verify fail.** `task test -- ./internal/settings/`

- [ ] **Step 3: Implement**

In `settings.go`:

```go
type Scoped interface {
	Settings                    // bare reads → host partition
	Owner(name string) Writable
	Host() Writable
}
type Writable interface {
	Settings
	SetString(ctx context.Context, key, value string) error
	SetStringSlice(ctx context.Context, key string, values []string) error
}
// store factories now return Scoped:
type PlayerSettingsStore    interface { For(ctx context.Context, playerID    ulid.ULID) Scoped }
type CharacterSettingsStore interface { For(ctx context.Context, characterID ulid.ULID) Scoped }
type GameSettings           interface { Scoped }
```

Add `validateNamespace bool` to `jsonMapSettings`; gate every accessor's `ValidateNamespace` call on it. Provide `NewJSONMapSettingsForTest(data, validateNamespace)`.

`scoped.go` — a `scopedView` holding the loaded prefs + a commit func (read-modify-write back through the owning repo, Task 3). `.Owner(name)` returns a `Writable` whose `jsonMapSettings.data` is `prefs.Plugins[name]` (created if absent) and `validateNamespace=false`; writes mutate that sub-map and call commit. `.Host()` for player/character returns a `noopWritable` (reads `(zero,false)`, writes `oops.Errorf("host player/character settings are typed; use auth.PlayerPreferences")`). `Chain.Owner(name)` returns a new `Chain` whose scopes are each `.Owner(name)`.

- [ ] **Step 4: Run, verify pass.** `task test -- ./internal/settings/`

- [ ] **Step 5: Commit.** `jj commit -m "feat(settings): owner-partitioned Scoped/Writable with structural isolation (holomush-iokti)"`

---

### Task 3: `SetStringSlice` writer + opaque `Plugins` passthrough (one writer, no clobber)

**Files:**

- Modify: `internal/auth/player.go` (add `Plugins map[string]json.RawMessage` to `PlayerPreferences`)
- Create: `internal/settings/player_store.go` (real `PlayerSettings` backed by `auth.PlayerRepository`, reading/writing `Preferences.Plugins`)
- Modify: bootstrap wiring (`cmd/holomush/sub_grpc.go` near `NewGameSettings:433`) to construct `NewPlayerSettingsStore`
- Test: `internal/auth/player_test.go`, `internal/settings/player_store_integration_test.go`

- [ ] **Step 1: Write failing clobber-resistance test**

```go
// player_test.go — typed marshal must round-trip the opaque Plugins bag.
func TestPlayerPreferencesPluginsBagRoundTrips(t *testing.T) {
	p := auth.PlayerPreferences{MaxCharacters: 3,
		Plugins: map[string]json.RawMessage{"core-scenes": json.RawMessage(`{"content.cw_block":["violence"]}`)}}
	b, err := json.Marshal(p); require.NoError(t, err)
	var got auth.PlayerPreferences
	require.NoError(t, json.Unmarshal(b, &got))
	assert.JSONEq(t, `{"content.cw_block":["violence"]}`, string(got.Plugins["core-scenes"]))
}
```

- [ ] **Step 2: Run, verify fail** (`Plugins` field undefined).

- [ ] **Step 3: Implement**

`auth/player.go` — add to `PlayerPreferences`:

```go
	// Plugins is an OPAQUE per-plugin settings passthrough (owner partition).
	// The host never interprets its contents (INV-10). Keyed by plugin name.
	Plugins map[string]json.RawMessage `json:"plugins,omitempty"`
```

`player_store.go` — `PlayerSettings.For(ctx, playerID) Scoped` loads `repo.GetByID` → builds a `scopedView` over `player.Preferences.Plugins`, with a commit func that does `repo.GetByID` → set `Preferences.Plugins[owner][key]=raw` → `repo.Update` (the **single** existing writer; whole-struct marshal now includes `Plugins`, so no clobber). `SetStringSlice` marshals `[]string`→`json.RawMessage` and stores it natively (so `StringSliceN`'s `json.Unmarshal(raw,&out)` works).

Wire `NewPlayerSettingsStore(playerRepo)` in bootstrap and inject into the host (Task 7) + focus coordinator.

- [ ] **Step 4: Run, verify pass.** `task test -- ./internal/auth/` + `task test:int -- ./internal/settings/`

- [ ] **Step 5: Commit.** `jj commit -m "feat(settings): player owner-store via opaque Plugins passthrough (holomush-iokti)"`

---

### Task 4: Game-scope owner partitioning (`plugin/<name>/<key>` prefix)

**Files:**

- Modify: `internal/settings/game.go` (`postgresGameSettings` implements `Scoped`: `Owner`/`Host`)
- Modify: `internal/settings/namespaces.go` (reserve `plugin` as a forbidden host top-level namespace)
- Test: `internal/settings/game_test.go`

- [ ] **Step 1: Failing test** — `game.Owner("core-scenes").SetStringSlice(ctx,"content.cw_taxonomy",…)` stores under `plugin/core-scenes/content.cw_taxonomy` and is readable back via `Owner("core-scenes")`, invisible to `Host()`.

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement** — `postgresGameSettings.Owner(name)` returns a `Writable` that prefixes every key with `"plugin/"+name+"/"` before hitting `SystemInfoStore`, with `validateNamespace=false`. `Host()` returns the bare `postgresGameSettings` (validated). Add a guard so host keys beginning `plugin/` are rejected by `ValidateNamespace` (reserve the segment). The prefix is built from the host-bound owner only (Task 7), so isolation (INV-11) holds.

- [ ] **Step 4: Run, verify pass.** `task test -- ./internal/settings/`

- [ ] **Step 5: Commit.** `jj commit -m "feat(settings): game-scope owner partitioning via reserved plugin/ prefix (holomush-iokti)"`

---

### Task 5: `characters.preferences` migration + real `CharacterSettingsStore`

**Files:**

- Create: `internal/store/migrations/000045_character_preferences.up.sql` / `.down.sql` (re-verify the number — current max is `000044`)
- Modify: `internal/settings/character.go` (replace null store with a real owner-partitioned one; keep `NewNullCharacterSettingsStore` for tests)
- Create: `internal/settings/character_store.go`
- Modify: bootstrap wiring to construct the real store
- Test: `internal/settings/character_store_integration_test.go`

- [ ] **Step 1: Migration**

`000045_character_preferences.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 8: per-character preferences (owner-partitioned settings scope).
ALTER TABLE characters ADD COLUMN IF NOT EXISTS preferences JSONB NOT NULL DEFAULT '{}';
```

`.down.sql`: `ALTER TABLE characters DROP COLUMN IF EXISTS preferences;`

- [ ] **Step 2: Failing test** — character-scope `.Owner("core-scenes")` round-trip against a Postgres testcontainer.

- [ ] **Step 3: Run, verify fail.** `task test:int -- ./internal/settings/`

- [ ] **Step 4: Implement** — `CharacterSettings` mirroring `PlayerSettings` (Task 3): a `CharacterPrefs` reader over the `characters` row, `For` returns a `scopedView` over `preferences.plugins`, commit via a `characters.preferences` read-modify-write. (A typed `CharacterPreferences{Plugins map[string]json.RawMessage}` struct or a raw JSONB map — match the player approach.)

- [ ] **Step 5: Run, verify pass + roll migration up/down on a scratch DB.**

- [ ] **Step 6: Commit.** `jj commit -m "feat(settings): real character-scope owner store + characters.preferences (holomush-iokti)"`

---

## Phase 2: Plugin settings access — host RPC + parity + authz

### Task 6: Proto — `GetSetting`/`SetSetting` (single-scope)

**Files:** Modify `api/proto/holomush/plugin/v1/plugin.proto`; regenerate.

- [ ] **Step 1: Add RPCs + messages** (every element doc-commented per `.claude/rules/proto-doc-comments.md`). After the focus RPCs (~:181):

```protobuf
  // GetSetting reads a single-scope setting in the calling plugin's owner
  // partition (owner bound host-side from the authenticated plugin). Phase 8.
  rpc GetSetting(PluginHostServiceGetSettingRequest) returns (PluginHostServiceGetSettingResponse);
  // SetSetting writes a single-scope setting in the calling plugin's partition;
  // GAME scope requires operator authz.
  rpc SetSetting(PluginHostServiceSetSettingRequest) returns (PluginHostServiceSetSettingResponse);
```

```protobuf
// SettingScope selects the scope a setting Get/Set targets. No chained mode:
// callers compose scopes themselves (e.g. CW-block union).
enum SettingScope {
  // Unspecified — rejected (fail closed).
  SETTING_SCOPE_UNSPECIFIED = 0;
  // Server-wide (holomush_system_info).
  SETTING_SCOPE_GAME = 1;
  // Per-player (players.preferences).
  SETTING_SCOPE_PLAYER = 2;
  // Per-character (characters.preferences).
  SETTING_SCOPE_CHARACTER = 3;
}
// PluginHostServiceGetSettingRequest reads one key. Owner is NOT on the wire —
// it is bound host-side from the authenticated plugin (INV-11).
message PluginHostServiceGetSettingRequest {
  // Scope to read.
  SettingScope scope = 1;
  // Principal ULID: player ID (PLAYER), character ID (CHARACTER), empty (GAME).
  string principal_id = 2;
  // Plugin-owned dot-key (e.g. "content.cw_block").
  string key = 3 [(buf.validate.field).string.min_len = 1];
}
// PluginHostServiceGetSettingResponse returns a typed list-or-string value.
message PluginHostServiceGetSettingResponse {
  // Whether the key resolved.
  bool found = 1;
  // String-list value (Phase 8 uses list-valued settings).
  repeated string string_list = 2;
  // Scalar string value (for non-list keys).
  string string_value = 3;
}
// PluginHostServiceSetSettingRequest writes one key in the caller's partition.
message PluginHostServiceSetSettingRequest {
  // Target scope.
  SettingScope scope = 1;
  // Principal ULID (empty for GAME).
  string principal_id = 2;
  // Plugin-owned dot-key.
  string key = 3 [(buf.validate.field).string.min_len = 1];
  // String-list value to store.
  repeated string string_list = 4;
}
// PluginHostServiceSetSettingResponse is the empty ack.
message PluginHostServiceSetSettingResponse {}
```

- [ ] **Step 2: Regenerate + lint.** `task lint:proto && task build` — clean.

- [ ] **Step 3: Commit.** `jj commit -m "feat(plugin): GetSetting/SetSetting single-scope RPCs (holomush-iokti)"`

---

### Task 7: Host handler — owner bound from `pluginName` + ABAC authz

**Files:**

- Modify: `internal/plugin/goplugin/host_service.go` (add methods on `pluginHostServiceServer`, ~after `Evaluate:521`)
- Modify: `internal/plugin/goplugin/host.go:195` (`Host` struct gains settings stores) + add `WithPlayerSettings`/`WithCharacterSettings`/`WithGameSettings` options (mirror `WithEngine:169`/`WithFocusCoordinator:140`) + `SetXxx` late-binders (mirror `SetFocusCoordinator:322`)
- Modify: bootstrap (`internal/plugin/setup/` + `cmd/holomush`) to inject the stores
- Test: `internal/plugin/goplugin/host_service_test.go`

- [ ] **Step 1: Failing tests — owner binding + authz denial**

```go
func TestGetSettingBindsOwnerToCallingPlugin(t *testing.T) {
	h := newTestHostWithEngine(t, "core-scenes", manifest, allowEngine())
	seedPlayerPluginSetting(t, h, "p1", "core-scenes", "content.cw_block", []string{"violence"})
	// Construct the server struct directly (mirror host_service_test.go:1235) —
	// newPluginHostServiceServer returns a *grpc.Server factory, not the server.
	s := &pluginHostServiceServer{host: h, pluginName: "core-scenes"}
	resp, err := s.GetSetting(ctx, &pluginv1.PluginHostServiceGetSettingRequest{
		Scope: pluginv1.SettingScope_SETTING_SCOPE_PLAYER, PrincipalId: "p1", Key: "content.cw_block"})
	require.NoError(t, err); require.True(t, resp.GetFound())
	assert.Equal(t, []string{"violence"}, resp.GetStringList())
}
func TestSetSettingGameScopeDeniedForNonOperator(t *testing.T) {
	s := &pluginHostServiceServer{host: newTestHostWithEngine(t,"core-scenes",manifest,denyEngine()), pluginName: "core-scenes"}
	_, err := s.SetSetting(ctx, &pluginv1.PluginHostServiceSetSettingRequest{
		Scope: pluginv1.SettingScope_SETTING_SCOPE_GAME, Key:"content.cw_taxonomy", StringList:[]string{"x"}})
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}
```

- [ ] **Step 2: Run, verify fail.** `task test -- ./internal/plugin/goplugin/`

- [ ] **Step 3: Implement** — mirror `Evaluate:521` (snapshot deps under lock). Resolve `base Scoped` from `scope` (`game` / `players.For(principalID)` / `characters.For(principalID)`); **bind owner via `base.Owner(s.pluginName)`** — `s.pluginName` is stamped at construction (`host_service.go:28`), never from the request. Authz through `s.host.engine`: PLAYER/CHARACTER require `principal_id == acting subject` (own settings); GAME writes require an operator `write`/`setting` decision; deny → `codes.PermissionDenied`. Then `StringSliceN` / `SetStringSlice`. Never leak inner errors (`grpc-errors.md`): log + `status.Error(codes.Internal,"internal error")`.

- [ ] **Step 4: Run, verify pass.** `task test -- ./internal/plugin/goplugin/`

- [ ] **Step 5: Commit.** `jj commit -m "feat(plugin): host GetSetting/SetSetting — owner-bound + ABAC authz (holomush-iokti)"`

---

### Task 8: Go SDK client method

**Files:** Create `pkg/plugin/settings_client.go` (mirror `pkg/plugin/focus_client.go:296`); add `GetSetting`/`SetSetting` to the host-client interface plugins consume; test `settings_client_test.go`.

- [ ] **Step 1–2: Failing test** for `GetSetting(ctx, scope, principalID, key) ([]string, bool, error)` marshaling the request and mapping the response (mirror `focus_client_test.go`). Run → fail.
- [ ] **Step 3: Implement** mirroring `pluginHostFocusClient.SetConnectionFocus:296` — build `pluginv1.PluginHostServiceGetSettingRequest`, call `c.client.GetSetting`, map `found`/`string_list`. (No owner param — the host binds it.)
- [ ] **Step 4–5:** Run pass; `jj commit -m "feat(plugin): SDK settings client (holomush-iokti)"`

---

### Task 9: Lua hostfunc parity (INV-8)

**Files:** Create `internal/plugin/lua/settings_ops_adapter.go` (mirror `focus_ops_adapter.go`); modify `internal/plugin/lua/host.go` (add `SetSettingsOps`, mirror `SetFocusCoordinator:121`) + the `hostfunc.Functions` surface to register `host.get_setting`/`host.set_setting`; test a Lua read.

- [ ] **Step 1–2: Failing test** — a Lua script calling `host.get_setting("player", principal, "content.cw_block")` returns the list. Run → fail.
- [ ] **Step 3: Implement** the adapter (delegating to the same stores the host handler uses, owner-bound to the Lua plugin's name) + register the Lua functions mirroring the focus ops. **INV-8 gate:** ship in the same change-set as Task 8.
- [ ] **Step 4–5:** Run pass (`task test -- ./internal/plugin/lua/` + `task test:int`); `jj commit -m "feat(plugin): Lua get_setting/set_setting hostfunc parity (holomush-iokti)"`

---

## Phase 3: Content-warning model (owned by core-scenes)

### Task 10: `DefaultCWTaxonomy` + read-time fallback + Create/Update validation

**Files:**

- Create: `plugins/core-scenes/content_warnings.go` (`DefaultCWTaxonomy` const + `effectiveTaxonomy(ctx) []string`)
- Modify: `plugins/core-scenes/service.go` (`CreateScene`, `UpdateScene` validation)
- Test: `plugins/core-scenes/service_test.go`

- [ ] **Step 1: Failing tests**

```go
func TestEffectiveTaxonomyFallsBackToDefaultWhenGameUnset(t *testing.T) {
	p := newScenePluginWithSettings(t, /* game content.cw_taxonomy unset */)
	assert.Subset(t, p.effectiveTaxonomy(ctx), []string{"violence", "death"})
}
func TestCreateSceneRejectsUnknownContentWarning(t *testing.T) {
	svc := newSceneServiceWithTaxonomy(t, []string{"violence"})
	_, err := svc.CreateScene(ctx, &scenev1.CreateSceneRequest{
		CharacterId: char, Title: "X", ContentWarnings: []string{"not-a-cat"}})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}
```

- [ ] **Step 2: Run, verify fail.** `task test -- ./plugins/core-scenes/`

- [ ] **Step 3: Implement**

```go
// content_warnings.go
var DefaultCWTaxonomy = []string{"violence", "sexual-content", "death",
	"substance-use", "self-harm", "body-horror", "abuse"}

// effectiveTaxonomy reads game-scope content.cw_taxonomy via the settings
// client; falls back to DefaultCWTaxonomy when unset (INV-5).
func (p *scenePlugin) effectiveTaxonomy(ctx context.Context) []string {
	if tax, ok, err := p.settings.GetSetting(ctx, GAME, "", "content.cw_taxonomy"); err == nil && ok && len(tax) > 0 {
		return tax
	}
	return DefaultCWTaxonomy
}
```

In `CreateScene`/`UpdateScene`, reject any `content_warnings` value not in `effectiveTaxonomy(ctx)` with `status.Error(codes.InvalidArgument, "unknown content warning")`. Inject the settings client like `p.evaluator`.

- [ ] **Step 4: Run, verify pass.** `task test -- ./plugins/core-scenes/`

- [ ] **Step 5: Commit.** `jj commit -m "feat(scenes): DefaultCWTaxonomy fallback + content_warnings validation (holomush-iokti)"`

---

## Phase 4: Scene board query

### Task 11: `ListScenesRequest` proto fields

**Files:** Modify `api/proto/holomush/scene/v1/scene.proto:245`.

- [ ] **Step 1: Add fields** (doc-commented):

```protobuf
  // Per-query CW exclude filter (hide), layered on the caller's resolved block;
  // applied server-side so pagination stays correct.
  repeated string exclude_content_warnings = 4;
  // Requesting character ULID — resolves character-scope CW block.
  string character_id = 5 [(buf.validate.field).string.min_len = 1];
  // Requesting player ULID — resolves player-scope CW block.
  string player_id = 6 [(buf.validate.field).string.min_len = 1];
```

- [ ] **Step 2: Regenerate + lint.** `task lint:proto && task build`
- [ ] **Step 3: Commit.** `jj commit -m "feat(scenes): ListScenesRequest CW-exclude + identity fields (holomush-iokti)"`

---

### Task 12: `ListScenes` store query + service handler (visibility, pagination, tag filter)

**Files:** Modify `plugins/core-scenes/store.go` (add `ListBoard` after `ListScenesForCharacter:1281`), `plugins/core-scenes/service.go` (add `ListScenes`, mirror `GetScene:322`); test `store_integration_test.go`, `service_test.go`.

- [ ] **Step 1: Failing store test (INV-1 + pagination + tag)**

```go
//go:build integration
func TestListBoardReturnsOnlyOpenActivePaginated(t *testing.T) {
	st := newSceneStore(t)
	seedScene(t, st, "open", "active", []string{"plot"}, nil)
	seedScene(t, st, "private", "active", nil, nil)  // excluded
	seedScene(t, st, "open", "ended", nil, nil)       // excluded
	rows, err := st.ListBoard(ctx, BoardQuery{Limit: 50, Tags: []string{"plot"}})
	require.NoError(t, err); require.Len(t, rows, 1)
	assert.Equal(t, "open", rows[0].Visibility)
}
```

- [ ] **Step 2: Run, verify fail.** `task test:int -- ./plugins/core-scenes/`

- [ ] **Step 3: Implement `ListBoard`** mirroring `ListScenesForCharacter` (span, `oops.Code`, row scan). SQL:

```sql
SELECT id, title, description, location_id, owner_id, state, pose_order_mode,
       content_warnings, tags, visibility, created_at
FROM scenes
WHERE visibility = 'open' AND state IN ('active','paused')
  AND ($1::text[] IS NULL OR tags @> $1)
ORDER BY created_at DESC, id ASC
LIMIT $2 OFFSET $3
```

`BoardQuery{Limit, Offset int; Tags []string}` (cap Limit ≤200, default when 0). `ListScenes` maps request→`BoardQuery`, calls `ListBoard`, maps rows→`[]*scenev1.SceneInfo` via existing `rowToProto`. CW filtering deferred to Task 13.

- [ ] **Step 4: Run, verify pass.** `task test:int -- ./plugins/core-scenes/`
- [ ] **Step 5: Commit.** `jj commit -m "feat(scenes): ListScenes board query (holomush-iokti)"`

---

### Task 13: CW filtering — union block across scopes + per-query exclude (INV-3, INV-6)

**Files:** Modify `plugins/core-scenes/service.go` (`ListScenes`), `plugins/core-scenes/store.go` (`ListBoard` excludes a CW set); test `service_test.go`.

- [ ] **Step 1: Failing union test**

```go
func TestListScenesExcludesUnionOfBlockedCWs(t *testing.T) {
	// player blocks {death}; a scene tagged death is excluded; CW labels still present.
	svc := newSceneServiceWithBlocks(t, playerBlock("p1", []string{"death"}))
	seedScene(t, svc, "open","active", nil, []string{"death"})   // excluded
	seedScene(t, svc, "open","active", nil, []string{"romance"}) // kept
	resp, err := svc.ListScenes(ctx, &scenev1.ListScenesRequest{Limit:50, PlayerId:"p1", CharacterId:"c1"})
	require.NoError(t, err); require.Len(t, resp.GetScenes(), 1)
}
```

- [ ] **Step 2: Run, verify fail.** `task test -- ./plugins/core-scenes/`

- [ ] **Step 3: Implement union + filter** — excluded set = per-query `exclude_content_warnings` ∪ resolved block. Resolve the block as the **union** of three single-scope `GetSetting` calls (GAME `""`, PLAYER `req.player_id`, CHARACTER `req.character_id`) for `content.cw_block` (INV-6 — single-scope reads, owner-bound to core-scenes host-side). Pass the union to `ListBoard`, which adds `AND NOT (content_warnings && $4)` (array overlap). Never strip `content_warnings` from the projection (INV-2).

- [ ] **Step 4: Run, verify pass.** `task test -- ./plugins/core-scenes/` + `task test:int`
- [ ] **Step 5: Commit.** `jj commit -m "feat(scenes): board CW filtering — union block + per-query exclude (holomush-iokti)"`

---

## Phase 5: `scenes` board command

### Task 14: `scenes` board command handler

**Files:** Modify `plugins/core-scenes/commands.go` (board command; the existing per-character listing stays `scene list`, `handleSceneList:1171`), `plugins/core-scenes/plugin.yaml` (register `scenes` + capabilities); test `commands_test.go`.

- [ ] **Step 1: Failing tests**

```go
func TestScenesBoardRendersOpenScenesWithCWLabels(t *testing.T) {
	p := newScenePluginWithBoard(t /* one scene cw=violence */)
	resp, err := p.handleScenesBoard(ctx, cmdReq("c1","p1",""))
	require.NoError(t, err); assert.Contains(t, resp.Text, "violence")
}
func TestScenesBoardParsesHideArg(t *testing.T) {
	p := newScenePluginWithBoard(t /* cw=violence + cw=romance */)
	resp, _ := p.handleScenesBoard(ctx, cmdReq("c1","p1","hide:violence"))
	assert.NotContains(t, resp.Text, "violence")
}
```

- [ ] **Step 2: Run, verify fail.** `task test -- ./plugins/core-scenes/`

- [ ] **Step 3: Implement** — parse `hide:<cw>`/`tag:<t>` into `ListScenesRequest{ExcludeContentWarnings, Tags, PlayerId:req.PlayerID, CharacterId:req.CharacterID, Limit}`, call `p.service.ListScenes`, render rows (title, id, owner, participant count, tags, CW labels, `[paused]`) via `strings.Builder` like `handleInfo:567`. Register `scenes` in `plugin.yaml` with read capabilities; route it (top-level command, distinct from `scene list`).

- [ ] **Step 4: Run, verify pass.** `task test -- ./plugins/core-scenes/` + `task test:int`
- [ ] **Step 5: Commit.** `jj commit -m "feat(scenes): scenes board command (holomush-iokti)"`

---

## Post-implementation checklist

- [ ] `task pr-prep` green; `task pr-prep:full` (this plan adds integration tests — Tasks 3, 5, 7, 12, 13).
- [ ] All 12 invariants (INV-1…INV-12) have a backing test; meta-test asserts catalog completeness.
- [ ] Migration `000045_character_preferences` rolls up AND down on a scratch DB.
- [ ] `task lint:proto` clean (new RPCs/messages/enum fully documented; name-echo passes).
- [ ] **INV-8 parity** verified — Go SDK + Lua both `get_setting`/`set_setting`.
- [ ] **INV-11 isolation** test — a plugin cannot reach another owner's partition (owner is server-stamped, not request-supplied).
- [ ] **INV-12** — adding `content.cw_block` required NO `RegisteredNamespaces`/`internal/` edit (only the one-time `Plugins` field + owner machinery).
- [ ] `abac-reviewer` (settings-write authz, Task 7) + `code-reviewer` before push.
- [ ] Site docs: `site/src/content/docs/extending/` host settings RPC; player-guide scene board entry.
- [ ] Supersede `holomush-5rh.17` when these beads close.

<!-- adr-capture: sha256=ca96f4bfe7c8b892; session=iokti; ts=2026-05-30T09:22:25Z; adrs=holomush-74ib4,holomush-uvbyt,holomush-0blcz -->
