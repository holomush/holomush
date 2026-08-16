# Phase 2: ABAC & Schema Vocabulary - Pattern Map

**Mapped:** 2026-08-03
**Files analyzed:** 21 planned files (create or modify)
**Analogs found:** 16 / 21 (5 are new shapes)

Every `path:line` below was opened in this worktree this session. Where RESEARCH.md
already verified a citation, this document does not re-quote it — it points at the
research section and adds only what the planner needs to *imitate* (the excerpt, the
surrounding file conventions, the sibling test).

**Standing caveats for every excerpt below:**
- Core migrations are **single-file goose** (`NNNNNN_name.sql`, `-- +goose Up` / `-- +goose Down`). The `.up.sql`/`.down.sql` pair form is dead in `internal/store/migrations/`. Every `*.up.sql` citation in `01-SPEC.md` / `02-CONTEXT.md` is stale (RESEARCH P-1).
- Next free migration version is **`000054`** (`000053_sessions_location_index.sql` is the highest).
- SPDX header on every new `.go` / `.sql` file; `task fmt` adds it — commit the result.

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `internal/access/policy/seed.go` (append ~10 policies) | config/policy data | request-response (ABAC eval) | `seed:directory-list-characters` `seed.go:482-487`; `seed:property-*` `seed.go:110-146` | **exact** |
| `internal/access/prefix.go` (+3 prefixes, +3 ctors) | utility/vocabulary | transform | `PlayerSubject` `prefix.go:83-93`; `CharacterResource` `prefix.go:95-107` | **exact** |
| `internal/access/prefix_test.go` (+3 known-prefix rows) | test | — | `prefix_test.go:560-605` table | **exact** |
| `internal/access/policy/attribute/viewer.go` (new) | AttributeProvider | request-response | `internal/access/policy/attribute/property.go:30-220` | **exact** |
| `internal/access/setup/setup.go` (register viewer provider) | wiring/config | — | `setup.go:240-264` numbered `RegisterProvider` block | **exact** |
| `internal/access/policy/seed_smoke_test.go` (+`viewerProvider` double, tier/admin-section assertions) | test | — | `createSeedEngine` + `characterProvider` `seed_smoke_test.go:18-52` | **exact** |
| `test/integration/access/<profile_public_read>_test.go` | test (integration) | — | `test/integration/access/seed_policies_test.go:1-60` | **exact** |
| `internal/store/migrations/000054_*.sql` (A: columns + non-unique idx + settings seed) | migration | batch DDL | `000045_character_preferences.sql` (col), `000053_sessions_location_index.sql` (idx), `000007_seed_scene_defaults.sql` (seed) | **exact** |
| `internal/store/migrations/000055_*.go` (B: Go backfill + collision detect) | migration | batch/transform | `internal/store/migrate_gointerleave_integration_test.go:160-210` + `internal/store/migrations/doc.go` | **exact** (fixture form; see caveat) |
| `internal/store/migrations/000056_*.sql` (C: SET NOT NULL then UNIQUE) | migration | DDL | `migrate_gointerleave_integration_test.go:70-88` (verbatim rationale) | **exact** |
| `internal/<blocklist-pkg>/cache.go` + `poller.go` (new) | service (DB-backed config) | poll / event-driven | `internal/access/policy/cache.go:17-120`, `poller.go:80-140` | **exact** |
| `internal/<blocklist-pkg>/*_test.go` | test | — | `internal/access/policy/cache_test.go:166-188` (reload-failure no-partial-update) | **exact** |
| `internal/world/validation.go` (replace `NormalizeCharacterName`) | domain utility | transform | itself, `validation.go:107-126` (the code being replaced) | **role-match** |
| `internal/auth/character_service.go` (gate at create) | service | request-response | `createWithMaxAndBind` `character_service.go:103-122` | **exact** |
| `internal/auth/guest_service.go` (gate at guest create) | service | request-response | `acquireUniqueName` `guest_service.go:218-239` | **exact** |
| `internal/admin/<section-registry>.go` + shared authz helper (new) | registry + guard | request-response | `AssertOperatorAdmin` `internal/admin/auth/operator_admin.go:37-64` | **role-match** |
| Boot validator for the section registry (D-09) / block list (D-15) | config validation | boot | `policy.Bootstrap` `internal/access/policy/bootstrap.go:27-56` | **role-match** |
| `cmd/internal/gen-confusables/main.go` + `//go:generate` + Taskfile entry | codegen | file-I/O | `internal/plugin/gen-schema` via `generate:schema` `Taskfile.yaml:596-606` | **exact** |
| `internal/<confusables-pkg>/skeleton.go` + generated table | domain leaf pkg | transform | **NO ANALOG — new shape** | — |
| Mixed-script check (§6.1.2 Mechanism A) | domain leaf pkg | transform | **NO ANALOG — new shape** | — |
| `cmd/holomush/cmd_<name-resolve>.go` (D-22 CLI) | CLI command | request-response | `cmd/holomush/cmd_crypto_rekey.go:1-60` (cobra + oops + typed exit codes) | **role-match** |
| Rename gate (criterion 1's "rename" half) | — | — | **NO ANALOG — no rename path exists** (see below) | — |
| Concurrent-claim uniqueness integration test | test (integration) | — | `test/integration/access/concurrent_engine_test.go:1-60` (shape only) + `plugins/core-scenes/publish_store.go:159-166` (23505) | **partial** |
| `docs/architecture/invariants.yaml` (+INV-PRIVACY-11) | registry data | — | `INV-PRIVACY-9/-10` entries, `invariants.yaml:2156-2170` | **exact** |

---

## Pattern Assignments

### 1. `internal/access/policy/seed.go` — new seed policies (D-01, D-03, D-06/EXT-07, PROFILE-11)

**Analog:** the same file. Seed policies are **Go struct literals**, not migrations. Adding one is an append to the `SeedPolicies()` slice; `policy.Bootstrap` installs it at boot (RESEARCH P2.1 for the install/upgrade/collision semantics).

**Struct + slice shape** (`internal/access/policy/seed.go:6-12,36-38`):

```go
// SeedPolicy defines a system-installed default policy.
type SeedPolicy struct {
	Name        string
	Description string
	DSLText     string
	SeedVersion int
}

func SeedPolicies() []SeedPolicy {
	return []SeedPolicy{
```

**Closest structural twin for `seed:admin-section-access`** — a **target-only** policy over an underscore-bearing resource type, with a leading rationale comment block. Copy this shape including the comment (`seed.go:475-487`):

```go
		// --- Character directory (INV-ACCESS-9) ---
		//
		// Any authenticated character (registered or guest) may list the server-wide
		// character directory (id + name only). Connection/online state requires a
		// separate, more-restrictive permission (not part of this seed). The resource
		// is the singleton "character_directory:all" — target-only match, no `when`
		// clause needed. Evaluated by CoreService.ListAllCharacters.
		{
			Name:        "seed:directory-list-characters",
			Description: "Any authenticated character (incl. guest) may list the character directory (names only)",
			DSLText:     `permit(principal is character, action in ["list_character_directory"], resource is character_directory);`,
			SeedVersion: 1,
		},
```

> NOTE the actual policy name is **`seed:directory-list-characters`** — RESEARCH P2.1 and Code Examples §C call it `seed:character-directory`. Use the real name when grepping.

**The read-side family D-01 twins** (`seed.go:110-146`) — the two the phase must not get wrong:

```go
		{
			Name:        "seed:property-public-read",
			Description: "Public properties readable by co-located characters",
			DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.property.visibility == "public" && principal.character.location == resource.property.parent_location };`,
			SeedVersion: 2,
		},
		...
		{
			Name:        "seed:property-owner-write",   // ← D-01: NO viewer twin
			Description: "Property owners can write and delete their properties",
			DSLText:     `permit(principal is character, action in ["write", "delete"], resource is property) when { resource.property.owner == principal.character.id };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:property-restricted-excluded",  // ← the family's only forbid
			DSLText:     `forbid(principal is character, action in ["read"], resource is property) when { resource.property.visibility == "restricted" && resource has property.excluded_from && principal.character.id in resource.property.excluded_from };`,
			SeedVersion: 2,
		},
```

**The policy criterion 4 widens** (`seed.go:50-56`) — the colocation clause is the whole restriction:

```go
		{
			Name:        "seed:player-character-colocation",
			Description: "Characters can read co-located characters",
			DSLText:     `permit(principal is character, action in ["read"], resource is character) when { resource.character.location == principal.character.location };`,
			SeedVersion: 2,
		},
```

**Editing an existing policy vs adding one:** an edit to `DSLText` **requires a `SeedVersion` bump** or `upgradeSeedPolicy` never runs (`bootstrap.go:91-93`); an admin-customized row (`Source != "seed"`) is skipped with a WARN (`bootstrap.go:80-88`). A brand-new policy needs no version dance.

---

### 2. `internal/access/prefix.go` — `admin_section:`, `profile:`, `viewer:`

**Analog:** `internal/access/prefix.go:83-107`. Constant → `knownPrefixes` entry → panic-on-empty constructor, in that order. Copy the doc-comment rationale verbatim in substance — it is the reason the panic exists:

```go
// PlayerSubject returns the canonical ABAC subject ID for a player
// ("player:<ulid>"). Players are a Subject-namespace identity alongside
// characters; PlayerAttributeProvider resolves this namespace.
//
// Panics on empty playerID, mirroring the safety guard in the other
// helpers in this package — empty subject strings would silently bypass
// access control if returned as the bare prefix.
func PlayerSubject(playerID string) string {
	if playerID == "" {
		panic("access.PlayerSubject: empty playerID would bypass access control")
	}
	return SubjectPlayer + playerID
}
```

Resource-side twin, same file (`prefix.go:95-107`), `CharacterResource` — note it documents *why* the subject and resource prefixes share a string.

**Test rows** go in the known-prefix table at `internal/access/prefix_test.go:560-605`, which proves membership indirectly via `ParseEntityRef`:

```go
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// ParseEntityRef uses the internal knownPrefixes slice to validate
			// prefixes. We test that each prefix constant can be parsed correctly,
			// which proves it's in knownPrefixes.
			typeName, id, err := access.ParseEntityRef(tt.constant + "test-id")
			require.NoError(t, err, "prefix should be recognized: %s", tt.desc)
```

---

### 3. `internal/access/policy/attribute/viewer.go` — `ViewerTierProvider` (new)

**Analog:** `internal/access/policy/attribute/property.go` — the canonical omit-don't-sentinel provider. Four things to copy:

**(a) Interface surface** (`property.go:37-45`): `Namespace() string`, `ResolveSubject`, `ResolveResource`, `Schema()`. A provider that is subject-only returns `nil, nil` from the resource half:

```go
// Namespace returns "property".
func (p *PropertyProvider) Namespace() string {
	return "property"
}

// ResolveSubject returns nil, nil — properties are not subjects.
func (p *PropertyProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}
```

`ViewerTierProvider` inverts this: `ResolveSubject` does the work, `ResolveResource` returns `nil, nil`.

**(b) Omit-don't-sentinel with a `has_X` witness on BOTH branches** (`property.go:104-118`) — this is `.claude/rules/abac-providers.md` in code, and `player_id` on the `anonymous` rung is the exact case:

```go
	// Handle optional Owner.
	//
	// Per ADR holomush-ti1b (motivating bug holomush-9gtl): when
	// has_owner=false the `owner` key MUST be OMITTED — NOT emitted as an
	// empty-string sentinel. This one is directly load-bearing:
	// seed:property-private-read and seed:property-owner-write both gate on
	// `resource.property.owner == principal.character.id` (see
	// internal/access/policy/seed.go), so an ownerless property MUST NOT be
	// comparable at all. The has_owner witness stays on both branches.
	if prop.Owner != nil {
		attrs["owner"] = *prop.Owner
		attrs["has_owner"] = true
	} else {
		attrs["has_owner"] = false
	}
```

**(c) `Schema()` must declare every key or the resolver drops it** (`property.go:196-220`):

```go
func (p *PropertyProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"id":                  types.AttrTypeString,
			"owner":               types.AttrTypeString,
			"has_owner":           types.AttrTypeBool,
			"visible_to":          types.AttrTypeStringList,
		},
	}
}
```

⇒ `viewer` schema needs `tier` (String), `player_id` (String), `has_player_id` (Bool).

**(d) Error wrapping at the repo boundary** (`property.go:71-77`): `oops.Code("<THING>_FETCH_FAILED").With(k,v).Wrapf(err, "...")`.

**Registration** — `internal/access/setup/setup.go:240-264`, a numbered comment block. Append in the same style:

```go
	// 9a. Stream provider (resolves resource.stream.{name,location} for
	// seed:player-stream-emit and seed:player-location-stream-read policies).
	streamProvider := attribute.NewStreamProvider()
	if err := resolver.RegisterProvider(streamProvider); err != nil {
		return nil, eb.Wrapf(err, "register stream provider")
	}
```

Skipping this step is RESEARCH P-7: the family silently default-denies while unit tests stay green.

---

### 4. ABAC behavior tests

**Unit tier — `internal/access/policy/seed_smoke_test.go:18-52`.** Load **all** seeds against the real engine; use the mock-provider constructor style, never `policytest` (RESEARCH P-6):

```go
// createSeedEngine builds an Engine loaded with ALL seed policies and the
// given attribute providers. This exercises the full evaluation pipeline:
// target matching → attribute resolution → condition evaluation → deny-overrides.
func createSeedEngine(t *testing.T, providers []attribute.AttributeProvider) *Engine {
	t.Helper()

	seeds := SeedPolicies()
	dslTexts := make([]string, len(seeds))
	for i, s := range seeds {
		dslTexts[i] = s.DSLText
	}

	return createTestEngineWithPolicies(t, dslTexts, providers)
}

func characterProvider(subjectAttrs, resourceAttrs map[string]any) *mockAttributeProvider {
	return &mockAttributeProvider{
		namespace:   "character",
		subjectMap:  subjectAttrs,
		resourceMap: resourceAttrs,
		schema: &types.NamespaceSchema{
			Attributes: map[string]types.AttrType{
				"id":    types.AttrTypeString,
				"roles": types.AttrTypeStringList,
			},
		},
	}
}
```

Write `viewerProvider(...)` beside `characterProvider` in the same file.

**Integration tier — `test/integration/access/seed_policies_test.go:1-56`.** Ginkgo, `//go:build integration`, `package access_test`, shared `env.pool`, and a `BeforeEach` whose delete ORDER is documented per-table (RESEARCH P-11):

```go
//go:build integration

package access_test

var _ = Describe("Seed Policy Behavior", func() {
	BeforeEach(func() {
		_, err := env.pool.Exec(ctx, "DELETE FROM player_character_bindings")
		Expect(err).NotTo(HaveOccurred())
		// entity_properties references characters, locations, and objects via
		// parent_id (enforced at the application layer). Delete before
		// characters/locations to avoid orphaned rows from FK cascade surprises.
		_, err = env.pool.Exec(ctx, "DELETE FROM entity_properties")
		Expect(err).NotTo(HaveOccurred())
		_, err = env.pool.Exec(ctx, "DELETE FROM characters")
```

---

### 5. Migration A — `000054_*.sql` (columns + non-unique index + settings seed)

Three sub-shapes, all in-tree, all single-file goose.

**Add a column** — `internal/store/migrations/000045_character_preferences.sql`, the whole file:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up

-- Phase 8: per-character preferences (owner-partitioned settings scope).
ALTER TABLE characters ADD COLUMN IF NOT EXISTS preferences JSONB NOT NULL DEFAULT '{}';

-- +goose Down

ALTER TABLE characters DROP COLUMN IF EXISTS preferences;
```

⇒ `last_active_at BIGINT NOT NULL DEFAULT 0` (D-25) and `status TEXT NOT NULL DEFAULT 'active' CHECK (status IN (...))` follow this exactly. Enum-by-CHECK precedent is `entity_properties.visibility` (`000001_baseline.sql:354-377`).

**Add an index** — `000053_sessions_location_index.sql:1-32`, notable for a long rationale comment INCLUDING why the non-blocking form was not used (goose wraps each migration in a transaction):

```sql
-- +goose Up

-- 000053 — index sessions.location_id (issue #4796).
-- ...
-- Plain index build. PostgreSQL's non-blocking index-build form cannot run
-- inside a transaction, and the migration runner wraps each migration in one;
-- ... adopting it here would mean adding non-transactional runner support.

CREATE INDEX IF NOT EXISTS idx_sessions_location_id
  ON sessions(location_id);

-- +goose Down

-- Reverse 000053: drop the presence-query index on sessions.location_id.

DROP INDEX IF EXISTS idx_sessions_location_id;
```

**Seed a settings row (D-14)** — `000007_seed_scene_defaults.sql`, the whole file. Note the Down is value-guarded so an operator override is not deleted:

```sql
-- +goose Up

-- Seed the default scene focus replay tail count in holomush_system_info.
-- ON CONFLICT DO NOTHING preserves operator overrides on re-application.

INSERT INTO holomush_system_info (key, value)
VALUES ('scenes.focus.replay_tail_default', '3')
ON CONFLICT (key) DO NOTHING;

-- +goose Down

-- Remove the seeded scene default. Only deletes the row if the value is
-- still the default '3'; operator-customized values are preserved.

DELETE FROM holomush_system_info
WHERE key = 'scenes.focus.replay_tail_default'
  AND value = '3';
```

⇒ D-14's block-list value must be a **JSON array string** to round-trip through `settings.StringSliceN` (see §7).

---

### 6. Migration B (Go) and C (constrain) — D-21 / D-22

**Analog:** `internal/store/migrate_gointerleave_integration_test.go`. It is a **fixture**, not a production migration, but it builds D-21's exact A→B→C chain and documents C's ordering in the same words D-21 uses (`:70-88`):

```go
// goInterleaveConstrainSQL is the fixture's migration 3: the D-21 (C) step.
//
// SET NOT NULL before CREATE UNIQUE INDEX is load-bearing, not stylistic.
// Postgres treats NULLs as distinct for uniqueness, so a unique index over an
// unbackfilled nullable column succeeds and enforces nothing — a green deploy
// with the guarantee silently absent. ...
const goInterleaveConstrainSQL = `-- +goose Up
ALTER TABLE gointerleave_names ALTER COLUMN normalized_name SET NOT NULL;
CREATE UNIQUE INDEX gointerleave_names_normalized_key
    ON gointerleave_names (normalized_name);

-- +goose Down
DROP INDEX gointerleave_names_normalized_key;
ALTER TABLE gointerleave_names ALTER COLUMN normalized_name DROP NOT NULL;
`
```

Go step (`:160-196`) — transactional, with an irreversible-down that **errors** rather than silently succeeding:

```go
	&goose.GoFunc{
		Mode: goose.TransactionEnabled,
		RunTx: func(ctx context.Context, tx *sql.Tx) error { /* backfill */ },
	},
	&goose.GoFunc{
		Mode: goose.TransactionEnabled,
		RunTx: func(_ context.Context, _ *sql.Tx) error {
			return oops.Code(goInterleaveIrreversibleCode).
				Errorf("migration %d normalized names in place; the pre-normalization values are not recoverable", goInterleaveGoVersion)
		},
	},
```

**Two divergences the planner MUST carry into the real migration:**
1. The fixture calls `goose.NewGoMigration(...)` directly and passes `goose.WithDisableGlobalRegistry(true)` (`:203-208`, with its own warning that production MUST NOT). The **real** `000055_*.go` registers via `goose.AddMigrationContext` inside an `init()`; the **filename is the version declaration**, and a missing `init()` makes the migration silently vanish (`internal/store/migrations/doc.go`; RESEARCH P-10).
2. The blank import that runs those `init()`s lives at `internal/store/migrations_register.go`, guarded by `internal/store/migrations_register_test.go`.

**Read `internal/store/migrations/doc.go` in full before writing `000055`** — it is the normative rules file for Go migrations.

---

### 7. Block list — settings read/seed + `Cache`/`Poller` mirror (D-14/D-15/D-16)

**Settings read/write** — `internal/settings/game.go:120-158`. The read side is lenient (`(nil,false)` + DebugContext) and the write side namespace-validates:

```go
func (g *postgresGameSettings) StringSliceN(ctx context.Context, key string) ([]string, bool) {
	s, ok := g.StringN(ctx, key)
	if !ok {
		return nil, false
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		slog.DebugContext(ctx, "game settings string-slice parse failed",
			"key", key, "raw", s, "error", err)
		return nil, false
	}
	return out, true
}

func (g *postgresGameSettings) SetStringSlice(ctx context.Context, key string, values []string) error {
	if err := ValidateNamespace(key); err != nil {
		return oops.With("key", key).Wrap(err)
	}
	encoded, err := json.Marshal(values)
	...
	return g.store.SetSystemInfo(ctx, key, string(encoded))
}
```

Namespace allowlist is `internal/settings/namespaces.go:15-20`; `core` is already admitted — **no change needed** (D-14).

**Compiled-snapshot cache** — `internal/access/policy/cache.go:17-88`. Copy the three-part shape: an immutable `Snapshot`, a `readBarrier` for reload-in-progress, and functional options:

```go
// Snapshot is an immutable, read-only view of compiled policies.
// It is safe for concurrent reads without locking.
type Snapshot struct {
	Policies  []CachedPolicy
	CreatedAt time.Time
}

// readBarrier is a one-shot broadcast result for the read barrier.
// Readers wait on done; err is written before close(done) so Go's
// memory model guarantees visibility without additional synchronization.
type readBarrier struct {
	done chan struct{}
	err  error
}

type Cache struct {
	store    store.PolicyStore
	compiler *Compiler
	mu       sync.RWMutex
	snapshot *Snapshot
	barrierMu sync.Mutex
	barrier   *readBarrier
	dirty     bool // true if Invalidate called during active reload
}
```

**Poller** — `internal/access/policy/poller.go:82-140`. Constructor rejects nil collaborators, defaults `Interval` to 10s, `Run` does an **immediate first poll**, and the first poll only marks `initialized` AFTER a successful reload:

```go
func NewPoller(cfg PollerConfig) (*Poller, error) {
	if cfg.Querier == nil { return nil, fmt.Errorf("policy poller: Querier is required") }
	if cfg.Reloader == nil { ... }
	if cfg.Tracker == nil { ... }
	if cfg.Interval <= 0 { cfg.Interval = 10 * time.Second }
	return &Poller{cfg: cfg}, nil
}

func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()
	p.poll(ctx)   // Immediate first poll
	for { select { case <-ctx.Done(): return; case <-ticker.C: p.poll(ctx) } }
}

	// First poll: establish baseline and reload.
	// IMPORTANT: only mark initialized AFTER reload succeeds to ensure retry on failure.
	if !p.initialized {
		if reloadErr := p.cfg.Reloader.Reload(ctx); reloadErr != nil {
			pollerErrorsTotal.Inc()
			errutil.LogErrorContext(ctx, "policy poller: initial reload failed", reloadErr)
			p.cfg.Tracker.RecordFailure("initial reload failed: " + reloadErr.Error())
			return
		}
```

The two-signal `VersionQuerier` (`poller.go:19-24`, returns `(time.Time, int64, error)`) is the shape RESEARCH P-5 says to reuse as `(updated_at, hash(value))` — a bare `updated_at` poll does not detect a direct-SQL edit.

**Reload-failure-leaves-last-valid test** — `internal/access/policy/cache_test.go:166-188`:

```go
func TestCacheReloadFailsOnCompilationError(t *testing.T) {
	...
	err := cache.Reload(context.Background())
	assert.Error(t, err, "reload should fail when a policy cannot compile")

	// Snapshot should still be empty (no partial update).
	snap, err := cache.Snapshot(context.Background())
	require.NoError(t, err)
	assert.Empty(t, snap.Policies)
}
```

⇒ D-16's "a bad pattern makes Reload fail, leaving the last valid list in force" is this test with a non-empty prior snapshot.

---

### 8. Name pipeline and its call sites (IDENT-06, criterion 1)

**The function being replaced** — `internal/world/validation.go:107-126`. §6.1.5 says *replace*, not extend; its per-word title-casing is what conflates display name with normalized form:

```go
// NormalizeCharacterName converts a character name to Initial Caps format.
// Example: "alaric" -> "Alaric", "jOhN sMiTh" -> "John Smith", "josé" -> "José"
func NormalizeCharacterName(name string) string {
	words := strings.Fields(name)
	for i, word := range words {
		if word != "" {
			runes := []rune(strings.ToLower(word))
			runes[0] = unicode.ToUpper(runes[0])
			words[i] = string(runes)
		}
	}
	return strings.Join(words, " ")
}
```

Its `strings.Fields` + `Join` half is the reusable whitespace-canonicalization step (§6.1.1 step 3). The `ToLower`/`ToUpper` half goes.

**Validation error style** — `validation.go:69-105` returns `&ValidationError{Field: "name", Message: "..."}`, not `oops`. New pipeline rejections in this file should match; the **confusable rejection message MUST NOT name the colliding character**.

**Create path 1 (player)** — `internal/auth/character_service.go:103-122`, the normalize → validate → exists → create sequence the new gate slots into:

```go
func (s *CharacterService) createWithMaxAndBind(ctx context.Context, playerID ulid.ULID, name string, maxCharacters int, bindReason string) (*world.Character, error) {
	// Normalize the name (trims whitespace, collapses spaces, Initial Caps)
	normalizedName := world.NormalizeCharacterName(name)

	// Validate the normalized name
	if err := world.ValidateCharacterName(normalizedName); err != nil {
		return nil, oops.Code("CHARACTER_INVALID_NAME").With("name", name).Wrap(err)
	}

	// Check name uniqueness (case-insensitive, using normalized name)
	exists, err := s.charRepo.ExistsByName(ctx, normalizedName)
	...
	if exists {
		return nil, oops.Code("CHARACTER_NAME_TAKEN").
			With("name", normalizedName).
			Errorf("character name %q is already taken", normalizedName)
	}
```

**Create path 2 (guest)** — `internal/auth/guest_service.go:218-239`. This path **never calls the pipeline** (RESEARCH P-3); a gate installed only in path 1 leaves it open:

```go
	for range maxGuestNameRetries {
		name, err := s.namer.GenerateName()
		...
		// Character names are stored with spaces; check using the display form.
		charName := strings.ReplaceAll(name, "_", " ")
		exists, err := s.chars.ExistsByName(ctx, charName)
```

**Rename: NO ANALOG — no rename path exists.** `rg -n 'Rename' --type go -g '!*_test.go' internal/world internal/auth` returns **zero** matches. `RenameCharacter` is Phase 3's. Phase 2's obligation is the *primitive* (the `UNIQUE` index and the pipeline) that Phase 3's rename gates on — a plan that says "gate the rename path" has nothing to edit. The planner should say so explicitly rather than invent a call site.

**Four `ExistsByName` sites, not two** (RESEARCH P2.4): `internal/bootstrap/setup/adapters.go:39-50` (the shared `LOWER(name)` query), the two writers above, and `internal/testsupport/integrationtest/harness.go:1549`.

**23505 handling for the real gate** — `plugins/core-scenes/publish_store.go:159-166`:

```go
// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505) raised by the named index/constraint.
func isUniqueViolation(err error, constraintName string) bool {
	...
	return pgErr.Code == "23505" && pgErr.ConstraintName == constraintName
}
```

Also `internal/eventbus/crypto/dek.IsUniqueViolation` (`dek/manager.go:289`, tested at `dek/store_test.go:16-21`) for a name-agnostic variant.

---

### 9. Admin section registry + shared gate (D-06/D-08/D-09, EXT-07)

**Analog for the shared helper** — `internal/admin/auth/operator_admin.go:37-64`. Layered checks, each returning a distinct `oops.Code("DENY_*")`, with the code taxonomy documented above the function:

```go
func AssertOperatorAdmin(
	ctx context.Context,
	resolver access.SubjectResolver,
	roleStore PlayerRoleHasher,
	playerID string,
) error {
	hasCap, err := access.HasPlayerGrant(ctx, resolver, playerID, access.CapabilityCryptoOperator)
	if err != nil {
		return oops.Code("INGAME_GRANT_LOOKUP_FAILED").
			With("player_id", playerID).Wrap(err)
	}
	if !hasCap {
		return oops.Code("DENY_NOT_OPERATOR").
			With("player_id", playerID).
			Errorf("crypto.operator capability absent")
	}
	...
	return nil
}
```

⇒ D-06's gate-then-distinguish is this shape with the ABAC `Evaluate` call **first** and the registry lookup **second**, so a denied caller only ever sees `DENY_ADMIN_SECTION`. The `player_id`-flavored subject here is also the live evidence for RESEARCH Open Question 2 (what principal `seed:admin-section-access` takes).

**Registry itself: NO ANALOG — new shape.** `rg -n 'admin_section|ViewerTier' --type go` is empty (RESEARCH P2.3). The web-side `web/src/lib/nav/sections.ts:35-47` is the **shape** to mirror, not a Go analog.

**Boot validator (D-09, and D-15's block-list compile)** — `internal/access/policy/bootstrap.go:27-56`. The house posture is: validate at boot, wrap with `oops` naming the offending entry, return the error, abort startup:

```go
// Bootstrap seeds the policy store with default policies and creates initial
// audit log partitions. Failures are fatal — any error MUST cause the server
// to abort startup (ADR #92).
func Bootstrap(...) error {
	...
	seeds := SeedPolicies()
	for _, seed := range seeds {
		if err := bootstrapSeed(ctx, policyStore, compiler, logger, seed, opts); err != nil {
			return oops.
				With("seed", seed.Name).
				Errorf("bootstrap: fatal seed policy error: %w", err)
		}
	}
	logger.InfoContext(ctx, "bootstrap complete", "seeds_total", len(seeds))
	return nil
}
```

`internal/bootstrap/setting.go:95-120` is the sibling with an `oops.Code("SETTING_BOOTSTRAP_FAILED")` taxonomy and a "already done, skipping" early return.

---

### 10. Confusables codegen (P0 recommendation)

**Analog:** the `generate:schema` pipeline. Three coordinated pieces.

**(a) `//go:generate` directive on a package file** — `internal/plugin/manifest.go:6`:
```go
//go:generate go run github.com/holomush/holomush/internal/plugin/gen-schema
```
Sibling forms: `internal/eventbus/crypto/dek/checkpoint_fsm.go:6` (`cmd/internal/fsmdiagram`), `internal/access/policy/dsl/ast.go:8`, `internal/plugin/luabridge/doc.go:27`.

**(b) Generator location.** Two in-tree conventions: a **package-local** `gen-*/` subdir (`internal/plugin/gen-schema`, `internal/plugin/luabridge/gen`) or **`cmd/internal/<tool>`** (`cmd/internal/fsmdiagram` — the only occupant today). Either is house style; the package-local form is the more common.

**(c) Taskfile entry with `sources:`/`generates:` for drift detection** — `Taskfile.yaml:596-606`:
```yaml
  generate:schema:
    desc: Generate plugin JSON Schema from Go types
    cmds:
      - go generate ./internal/plugin/
    sources:
      - internal/plugin/gen-schema/main.go
      - internal/plugin/manifest.go
      - internal/plugin/schema.go
    generates:
      - schemas/plugin.schema.json
```
`generate:luabridge` (`:607-622`) is the better model for a *data-dependent* generator — its comment explains why the upstream descriptors are listed as `sources:` even though they are not Go inputs. The confusables generator's `sources:` should likewise include the pinned-version declaration so a version bump forces regeneration.

**Skeleton algorithm + generated table itself: NO ANALOG — new shape.** No Unicode-security code exists in the tree. RESEARCH Code Examples §B carries the ~20-line algorithm; there is nothing in-repo to imitate.

**Mixed-script check: NO ANALOG — new shape.** Nothing in-tree uses `unicode.Scripts`.

---

### 11. `cmd/holomush/cmd_<name-resolve>.go` — the D-22 duplicate-resolution CLI

**Analog:** `cmd/holomush/cmd_crypto_rekey.go:1-60` — `package main`, cobra, `oops`, an injectable client factory for tests, and typed errors carrying sysexits exit codes:

```go
package main

import ( "bufio"; "context"; ...; "github.com/samber/oops"; "github.com/spf13/cobra"; ... )

// adminClientFactory returns an AdminServiceClient.  In production the
// implementation dials the admin UDS socket.  In tests it returns a
// pre-built httptest client.
type adminClientFactory func() (adminv1connect.AdminServiceClient, error)

// exitCodeError wraps an error and annotates it with a sysexits.h exit code.
type exitCodeError struct {
	exitCode int
	...
}
```

Sibling commands to skim for the plain (non-streaming) shape: `cmd_admin_totp_reset.go`, `cmd_plugin_validate.go`.

**Partial match caveat:** every existing `cmd_*.go` reaches the server over the admin UDS/Connect surface. D-22's command must run the §6.1.1 pipeline and block list, which are in-process libraries — so the *cobra scaffolding* transfers but the *transport* does not. There is **no in-tree one-shot data-fixing CLI that talks straight to Postgres**; state that gap rather than implying one exists.

---

### 12. `docs/architecture/invariants.yaml` — INV-PRIVACY-11 (D-07)

**Analog:** `invariants.yaml:2156-2170`. A pending entry is exactly five keys and **MUST NOT** carry `asserted_by`:

```yaml
  - id: INV-PRIVACY-9
    scope: INV-PRIVACY
    origin_spec: ".planning/phases/01-portal-spec/01-SPEC.md"
    summary: "A character profile below its configured reachability floor returns a not-found-equivalent whose wire shape
      is identical to the response for a character id that does not exist — no distinct 'this profile is private' signal,
      error code or status, which would disclose that the character exists."
    binding: pending
```

A `bound` entry adds `asserted_by:` as a file list (`invariants.yaml:5060-5065`, INV-WORLD-4). Phase 2 flips exactly three to `bound`: **INV-WORLD-5, INV-WORLD-6, INV-PRIVACY-11** — each needs a `// Verifies: INV-<SCOPE>-N` annotation plus `go run ./cmd/inv-render`. The section-comment convention (a `# --` block above a family explaining its provenance) should be extended, not replaced.

---

## Shared Patterns

### Error construction
**Source:** `internal/auth/character_service.go:108-122`, `internal/access/policy/attribute/property.go:71-77`
**Apply to:** every new service, provider, migration, and CLI file.
`oops.Code("SCREAMING_SNAKE").With(k, v).Wrap(err)` at boundaries; `.Errorf(...)` when originating. `internal/world/validation.go` is the exception — it uses `&ValidationError{Field, Message}` and new validation rejections there should match its neighbors.

### Structured logging
**Source:** `internal/access/policy/poller.go:120-126`, `internal/settings/game.go:125-126`
**Apply to:** all new Go files.
`slog.DebugContext(ctx, ...)` / `errutil.LogErrorContext(ctx, msg, err)` — never the bare variants when a `ctx` is in scope (`sloglint context: scope` enforces it).

### Migration file header
**Source:** every file in `internal/store/migrations/`
**Apply to:** `000054`, `000056`.
```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up
...
-- +goose Down
...
```
Idempotent (`IF NOT EXISTS` / `IF EXISTS`); Down reverses in reverse order; timestamp-class columns are `BIGINT` epoch-ns (INV-STORE-1, `task lint:no-timestamptz`).

### Fail-closed boot validation
**Source:** `internal/access/policy/bootstrap.go:27-56`; `internal/bootstrap/setting.go:95-120`
**Apply to:** D-09 (registry descriptor validator) and D-15 (block-list compile).
Validate the whole set at boot, wrap with the offending entry's identity, return the error, let startup abort. Do not log-and-continue.

### Optional attribute emission
**Source:** `internal/access/policy/attribute/property.go:104-118`
**Apply to:** `ViewerTierProvider` only (the phase's one new provider).
Omit the key; emit the `has_X` witness on **both** branches; declare both in `Schema()`.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `internal/<confusables-pkg>/skeleton.go` + generated confusables table | domain leaf pkg | transform | No Unicode-security code exists anywhere in the tree. The *codegen pipeline* has analogs (§10); the algorithm and data do not. Use RESEARCH Code Examples §B. |
| Mixed-script check (§6.1.2 Mechanism A) | domain leaf pkg | transform | Nothing in-tree touches `unicode.Scripts`. `characterNameRegex` (`internal/world/validation.go:60`) is the only script-adjacent code and it is a `\p{L}` class, not a script-set computation. |
| Admin section registry (seven ids + authorization descriptor) | registry | — | `rg -n 'admin_section\|ViewerTier' --type go` returns zero. `web/src/lib/nav/sections.ts:35-47` is a TypeScript shape reference, not a Go analog. The *gate* has an analog (§9); the *registry* does not. |
| Configurable regex block list | config | — | RESEARCH and CONTEXT both confirm: every regex in the tree is a compile-time `MustCompile` allowlist. The *storage* (settings) and the *refresh* (`Cache`/`Poller`) have analogs; the "operator-supplied regexes compiled at boot" concept does not. |
| Rename gate | — | — | **No rename path exists** (`rg -n 'Rename' --type go -g '!*_test.go' internal/world internal/auth` → empty). `RenameCharacter` is Phase 3's. Phase 2 ships the primitive it will gate on. |
| One-shot Postgres data-fixing CLI (D-22 transport half) | CLI | batch | Every `cmd/holomush/cmd_*.go` talks to the server over admin UDS/Connect. The cobra scaffolding transfers; the direct-DB shape is new. |

---

## Metadata

**Analog search scope:** `internal/access/`, `internal/world/`, `internal/auth/`, `internal/settings/`, `internal/store/`, `internal/store/migrations/`, `internal/admin/`, `internal/bootstrap/`, `cmd/holomush/`, `cmd/internal/`, `test/integration/access/`, `docs/architecture/`, `Taskfile.yaml`, `plugins/core-scenes/`
**Files opened this session:** 24
**Pattern extraction date:** 2026-08-03
**Companion documents:** `02-CONTEXT.md` (26 locked decisions), `02-RESEARCH.md` (P0 Unicode mechanism, P1 audit query, P2 repo grounding, 12 pitfalls). This document does not restate them.
