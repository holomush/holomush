<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Bootstrap: Aliases & Admin Seeding Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Seed standard MUSH command aliases and create an admin account on first boot, with multi-role character support wired into the ABAC stack.

**Architecture:** Two idempotent bootstrap functions run during core startup: one seeds system aliases (`"`, `:`, `;`), the other creates an admin player+character when the players table is empty. A new `character_roles` join table and `RoleStore` interface provide multi-role support, wired into the existing ABAC policy engine via an updated `RoleResolver`.

**Tech Stack:** Go, PostgreSQL, Argon2id (existing), ABAC policy DSL (existing `in` support), SvelteKit (render update)

**Spec:** `docs/specs/2026-03-28-bootstrap-aliases-admin-design.md`

---

## Chunk 1: Foundation — Naming Package, Role Constants, Role Store

### Task 1: Extract naming package from telnet

Move `NameTheme` interface and name lists from `internal/telnet/guest_auth.go` to a shared `internal/naming/` package.

**Files:**

- Create: `internal/naming/theme.go`
- Create: `internal/naming/gemstone.go`
- Create: `internal/naming/star.go`
- Create: `internal/naming/star_test.go`
- Modify: `internal/telnet/guest_auth.go` (remove name lists, import from naming)
- Modify: `internal/telnet/guest_auth_test.go` (update imports if needed)

- [ ] **Step 1: Create `internal/naming/theme.go`**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package naming provides themed name generators for characters.
package naming

// Theme generates themed names.
type Theme interface {
	Name() string
	Generate() (firstName, secondName string)
}
```

- [ ] **Step 2: Create `internal/naming/gemstone.go`**

Move `gemstones`, `elements` slices and `GemstoneElementTheme` from `internal/telnet/guest_auth.go:24-54`. Keep the same logic, update the interface name from `NameTheme` to `Theme`.

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package naming

import "math/rand/v2"

var gemstones = []string{
	"Amber", "Amethyst", "Beryl", "Coral", "Diamond",
	"Emerald", "Garnet", "Jade", "Jasper", "Lapis",
	"Moonstone", "Obsidian", "Onyx", "Opal", "Pearl",
	"Quartz", "Ruby", "Sapphire", "Topaz", "Turquoise",
}

var elements = []string{
	"Argon", "Boron", "Carbon", "Cobalt", "Copper",
	"Gold", "Helium", "Iodine", "Iron", "Krypton",
	"Neon", "Nickel", "Osmium", "Radium", "Radon",
	"Silver", "Titanium", "Xenon", "Zinc", "Zircon",
}

// GemstoneElementTheme generates names like "Amber_Argon".
type GemstoneElementTheme struct{}

// NewGemstoneElementTheme creates a new GemstoneElementTheme.
func NewGemstoneElementTheme() *GemstoneElementTheme {
	return &GemstoneElementTheme{}
}

// Name returns the theme identifier.
func (t *GemstoneElementTheme) Name() string {
	return "gemstone_element"
}

// Generate returns a random (gemstone, element) pair.
func (t *GemstoneElementTheme) Generate() (firstName, secondName string) {
	return gemstones[rand.IntN(len(gemstones))], elements[rand.IntN(len(elements))] //nolint:gosec // non-security name generation
}
```

- [ ] **Step 3: Create `internal/naming/star.go`**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package naming

import "math/rand/v2"

var stars = []string{
	"Sirius", "Vega", "Rigel", "Altair", "Deneb",
	"Polaris", "Antares", "Betelgeuse", "Capella", "Arcturus",
	"Spica", "Regulus", "Procyon", "Aldebaran", "Fomalhaut",
	"Canopus", "Achernar", "Bellatrix", "Castor", "Pollux",
}

// StarTheme generates single star names for admin/staff characters.
type StarTheme struct{}

// NewStarTheme creates a new StarTheme.
func NewStarTheme() *StarTheme {
	return &StarTheme{}
}

// Name returns the theme identifier.
func (t *StarTheme) Name() string {
	return "star"
}

// Generate returns a random star name as firstName, with empty secondName.
func (t *StarTheme) Generate() (firstName, secondName string) {
	return stars[rand.IntN(len(stars))], "" //nolint:gosec // non-security name generation
}
```

- [ ] **Step 4: Write star theme test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package naming

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStarTheme_Name(t *testing.T) {
	theme := NewStarTheme()
	assert.Equal(t, "star", theme.Name())
}

func TestStarTheme_Generate(t *testing.T) {
	theme := NewStarTheme()
	first, second := theme.Generate()
	assert.NotEmpty(t, first, "star name should not be empty")
	assert.Empty(t, second, "second name should be empty for star theme")
}

func TestGemstoneElementTheme_Generate(t *testing.T) {
	theme := NewGemstoneElementTheme()
	first, second := theme.Generate()
	assert.NotEmpty(t, first)
	assert.NotEmpty(t, second)
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/naming/... -v`
Expected: PASS

- [ ] **Step 6: Update `internal/telnet/guest_auth.go`**

Remove the `NameTheme` interface, `gemstones` slice, `elements` slice, and `GemstoneElementTheme` type (lines 18-54). Replace with import from `internal/naming/`. Update `GuestAuthenticator` to use `naming.Theme` instead of `NameTheme`.

Key changes:

- `NameTheme` → `naming.Theme` in the `GuestAuthenticator` struct field and `NewGuestAuthenticator` parameter
- `NewGemstoneElementTheme()` call in `cmd/holomush/core.go:444` → `naming.NewGemstoneElementTheme()`
- Update any test files that reference `NameTheme` or `GemstoneElementTheme`

- [ ] **Step 7: Run full test suite for telnet**

Run: `go test ./internal/telnet/... -v`
Expected: PASS

- [ ] **Step 8: Commit**

`jj desc -m "refactor: extract naming package from telnet guest auth"`

---

### Task 2: System role constants

**Files:**

- Create: `internal/access/role.go`
- Create: `internal/access/role_test.go`

- [ ] **Step 1: Create `internal/access/role.go`**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

const (
	// RolePlayer is the default role for all characters.
	RolePlayer = "player"
	// RoleBuilder grants world-building permissions.
	RoleBuilder = "builder"
	// RoleAdmin grants full access to everything.
	RoleAdmin = "admin"
)

// SystemRoles returns all system roles in privilege order (lowest first).
func SystemRoles() []string {
	return []string{RolePlayer, RoleBuilder, RoleAdmin}
}
```

- [ ] **Step 2: Write test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSystemRoles(t *testing.T) {
	roles := SystemRoles()
	assert.Equal(t, []string{"player", "builder", "admin"}, roles)
}

func TestRoleConstants(t *testing.T) {
	assert.Equal(t, "player", RolePlayer)
	assert.Equal(t, "builder", RoleBuilder)
	assert.Equal(t, "admin", RoleAdmin)
}
```

- [ ] **Step 3: Run test**

Run: `go test ./internal/access/ -run TestSystemRoles -v`
Expected: PASS

- [ ] **Step 4: Replace magic strings**

Search for `"admin"`, `"builder"`, `"player"` in Go files under `internal/access/policy/attribute/` and replace with `access.RolePlayer`, `access.RoleBuilder`, `access.RoleAdmin` where they refer to role values (not policy DSL text).

Key files:

- `internal/access/policy/attribute/character.go:105` — `role := "player"` → `role := access.RolePlayer`
- `internal/access/policy/attribute/character_test.go` — mock role values
- `test/integration/access/access_suite_test.go` — static role resolver values

- [ ] **Step 5: Run affected tests**

Run: `go test ./internal/access/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

`jj desc -m "feat(access): add system role constants and replace magic strings"`

---

### Task 3: character\_roles migration and RoleStore

**Files:**

- Create: `internal/store/migrations/000020_character_roles.up.sql`
- Create: `internal/store/migrations/000020_character_roles.down.sql`
- Create: `internal/store/role_store.go`
- Create: `internal/store/role_store_test.go`

- [ ] **Step 1: Check next migration number**

Run: `ls internal/store/migrations/*.up.sql | tail -1`
Use the next sequential number (likely 000020).

- [ ] **Step 2: Create up migration**

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE character_roles (
    character_id TEXT NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    PRIMARY KEY (character_id, role)
);
```

- [ ] **Step 3: Create down migration**

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP TABLE IF EXISTS character_roles;
```

- [ ] **Step 4: Write RoleStore interface and Postgres implementation**

Create `internal/store/role_store.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"

	"github.com/samber/oops"
)

// RoleStore provides CRUD operations for character role assignments.
type RoleStore interface {
	GetRoles(ctx context.Context, characterID string) ([]string, error)
	AddRole(ctx context.Context, characterID string, role string) error
	RemoveRole(ctx context.Context, characterID string, role string) error
}

// PostgresRoleStore implements RoleStore using PostgreSQL.
type PostgresRoleStore struct {
	pool poolIface
}

// NewPostgresRoleStore creates a new PostgreSQL role store.
func NewPostgresRoleStore(pool poolIface) *PostgresRoleStore {
	return &PostgresRoleStore{pool: pool}
}

// GetRoles returns all roles assigned to a character.
func (s *PostgresRoleStore) GetRoles(ctx context.Context, characterID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT role FROM character_roles WHERE character_id = $1`, characterID)
	if err != nil {
		return nil, oops.With("character_id", characterID).Wrap(err)
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, oops.With("operation", "scan role row").Wrap(err)
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

// AddRole assigns a role to a character. Idempotent — no error if already assigned.
func (s *PostgresRoleStore) AddRole(ctx context.Context, characterID string, role string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO character_roles (character_id, role) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		characterID, role)
	if err != nil {
		return oops.With("character_id", characterID).With("role", role).Wrap(err)
	}
	return nil
}

// RemoveRole removes a role from a character.
func (s *PostgresRoleStore) RemoveRole(ctx context.Context, characterID string, role string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM character_roles WHERE character_id = $1 AND role = $2`,
		characterID, role)
	if err != nil {
		return oops.With("character_id", characterID).With("role", role).Wrap(err)
	}
	return nil
}
```

- [ ] **Step 5: Write RoleStore integration test**

Create `internal/store/role_store_test.go` with `//go:build integration` tag. Test GetRoles (empty), AddRole, AddRole idempotent, RemoveRole, cascade on character delete. Follow the pattern in `internal/store/alias_test.go` for testcontainers setup.

- [ ] **Step 6: Run integration test**

Run: `go test -tags=integration ./internal/store/ -run TestRoleStore -v`
Expected: PASS

- [ ] **Step 7: Commit**

`jj desc -m "feat(store): character_roles migration and PostgresRoleStore"`

---

### Task 4: Update RoleResolver to multi-role

**Files:**

- Modify: `internal/access/policy/attribute/character.go:17-30` (RoleResolver interface)
- Modify: `internal/access/policy/attribute/character.go:104-112` (resolve logic)
- Modify: `internal/access/policy/attribute/character.go:139-152` (Schema)
- Modify: `internal/access/policy/attribute/character_test.go` (mock + tests)
- Modify: `test/integration/access/access_suite_test.go:99-103` (staticRoleResolver)

- [ ] **Step 1: Update RoleResolver interface**

In `internal/access/policy/attribute/character.go`, change:

```go
// Before (lines 25-30):
type RoleResolver interface {
	GetRole(subject string) string
}

// After:
type RoleResolver interface {
	GetRoles(ctx context.Context, subject string) []string
}
```

Update the doc comment (lines 17-24) to describe multi-role behavior.

- [ ] **Step 2: Update resolve() to use multi-role**

In `character.go`, replace lines 104-120:

```go
// Resolve roles from role resolver
var roles []string
if p.roleResolver != nil {
	subjectID := access.CharacterSubject(char.ID.String())
	roles = p.roleResolver.GetRoles(subjectID)
}
if len(roles) == 0 {
	roles = []string{access.RolePlayer}
}

attrs := map[string]any{
	"id":          char.ID.String(),
	"player_id":   char.PlayerID.String(),
	"name":        char.Name,
	"description": char.Description,
	"roles":       roles,
}
```

- [ ] **Step 3: Update Schema()**

In `character.go`, change the schema (line 146):

```go
// Before: "role": types.AttrTypeString,
// After:  "roles": types.AttrTypeStringList,
```

- [ ] **Step 4: Update test mocks**

In `character_test.go`, change `mockRoleResolver`:

```go
type mockRoleResolver struct {
	roles map[string][]string
}

func (m *mockRoleResolver) GetRoles(ctx context.Context, subject string) []string {
	return m.roles[subject]
}
```

Update all test cases that set role expectations to use `[]string` values and check `"roles"` attribute instead of `"role"`.

- [ ] **Step 5: Update integration test mock**

In `test/integration/access/access_suite_test.go`, change `staticRoleResolver`:

```go
type staticRoleResolver struct {
	roles map[string][]string
}

func (s *staticRoleResolver) GetRoles(ctx context.Context, subject string) []string {
	return s.roles[subject]
}
```

(Note: method should be `GetRoles` to match the new interface.)

- [ ] **Step 6: Run tests**

Run: `go test ./internal/access/... -v`
Expected: PASS (attribute tests)

Run: `go test -tags=integration ./test/integration/access/... -v`
Expected: May fail until seed policies are updated (Task 5)

- [ ] **Step 7: Commit**

`jj desc -m "feat(access): update RoleResolver to multi-role []string"`

---

### Task 5: Update seed policies for multi-role

**Files:**

- Modify: `internal/access/policy/seed.go` (6 policies)
- Modify: `internal/access/policy/seed.go:20` (doc comment)

- [ ] **Step 1: Update Pattern A policies (equality → in)**

Change 2 policies:

`seed:admin-full-access` (line 96):

```text
// Before: principal.character.role == "admin"
// After:  "admin" in principal.character.roles
```

`seed:property-admin-read` (line 114):

```text
// Before: resource.property.visibility == "admin" && principal.character.role == "admin"
// After:  resource.property.visibility == "admin" && "admin" in principal.character.roles
```

Increment `SeedVersion` to 3 on both.

- [ ] **Step 2: Update Pattern B policies (scalar-in-list → in)**

Change 4 policies:

`seed:builder-location-write` (line 78):

```text
// Before: principal.character.role in ["builder", "admin"]
// After:  "builder" in principal.character.roles
```

`seed:builder-object-write` (line 84): same pattern
`seed:builder-commands` (line 90): same pattern (keep the `resource.command.name in [...]` part)
`seed:builder-exit-write` (line 149): same pattern

Increment `SeedVersion` to 3 on all four.

- [ ] **Step 3: Update doc comment**

Line 20: change `principal.character.role` → `principal.character.roles`

- [ ] **Step 4: Run seed policy tests**

Run: `go test ./internal/access/policy/... -v`
Expected: PASS

- [ ] **Step 5: Run integration tests**

Run: `go test -tags=integration ./test/integration/access/... -v`
Expected: PASS (now that both resolver and policies are updated)

- [ ] **Step 6: Commit**

`jj desc -m "feat(access): update seed policies for multi-role character.roles"`

---

### Task 6: Wire RoleStore into ABAC stack

**Files:**

- Modify: `internal/access/setup/setup.go:53-58` (ABACConfig)
- Modify: `internal/access/setup/setup.go:86-91` (CharacterProvider creation)
- Create: `internal/store/role_resolver.go` (adapter from RoleStore → RoleResolver)
- Modify: `cmd/holomush/core.go:275-279` (pass RoleStore to ABACConfig)

- [ ] **Step 1: Create RoleResolver adapter**

Create `internal/store/role_resolver.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"log/slog"
	"strings"

	"github.com/holomush/holomush/internal/access/policy/attribute"
)

// Ensure PostgresRoleResolver satisfies attribute.RoleResolver at compile time.
var _ attribute.RoleResolver = (*PostgresRoleResolver)(nil)

// PostgresRoleResolver adapts PostgresRoleStore to the attribute.RoleResolver interface.
// It uses a background context for role lookups since the RoleResolver interface
// does not accept a context parameter.
type PostgresRoleResolver struct {
	store RoleStore
}

// NewPostgresRoleResolver creates a new resolver backed by the given store.
func NewPostgresRoleResolver(store RoleStore) *PostgresRoleResolver {
	return &PostgresRoleResolver{store: store}
}

// GetRoles returns the roles for the given subject (format: "character:ULID").
// Strips the "character:" prefix before querying the store, since the
// character_roles table stores bare character IDs.
func (r *PostgresRoleResolver) GetRoles(ctx context.Context, subject string) []string {
	// Strip entity type prefix — subject arrives as "character:01ABC..."
	// but the store expects bare character IDs.
	charID := strings.TrimPrefix(subject, "character:")

	// RoleResolver interface has no context; use background.
	roles, err := r.store.GetRoles(context.Background(), charID)
	if err != nil {
		slog.Error("role resolution failed", "subject", subject, "error", err)
		return nil
	}
	return roles
}
```

- [ ] **Step 2: Update ABACConfig**

In `internal/access/setup/setup.go`, add `RoleStore` to `ABACConfig`:

```go
type ABACConfig struct {
	Pool          *pgxpool.Pool
	CharacterRepo world.CharacterRepository
	AuditMode     audit.Mode
	RoleStore     store.RoleStore // optional; nil = all characters default to player
}
```

This requires importing the store package. Add it.

- [ ] **Step 3: Update BuildABACStack to wire resolver**

In `setup.go`, replace line 87:

```go
// Before:
charProvider := attribute.NewCharacterProvider(cfg.CharacterRepo, nil)

// After:
var roleResolver attribute.RoleResolver
if cfg.RoleStore != nil {
	roleResolver = store.NewPostgresRoleResolver(cfg.RoleStore)
}
charProvider := attribute.NewCharacterProvider(cfg.CharacterRepo, roleResolver)
```

- [ ] **Step 4: Update core.go to pass RoleStore**

In `cmd/holomush/core.go`, after `abacPool` is available (around line 273), create the role store and pass it:

```go
roleStore := store.NewPostgresRoleStore(abacPool)

abacStack, abacErr := abacsetup.BuildABACStack(ctx, abacsetup.ABACConfig{
	Pool:          abacPool,
	CharacterRepo: worldpostgres.NewCharacterRepository(abacPool),
	AuditMode:     audit.ModeDenialsOnly,
	RoleStore:     roleStore,
})
```

Hoist `roleStore` declaration so it's available later for admin bootstrap.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/access/... -v && go test ./cmd/holomush/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

`jj desc -m "feat(access): wire PostgresRoleStore into ABAC stack"`

---

## Chunk 2: Bootstrap Functions

### Task 7: PlayerRepository.Count

**Files:**

- Modify: `internal/auth/player.go:129-145` (PlayerRepository interface)
- Modify: `internal/auth/postgres/player_repo.go` (Postgres impl)
- Modify: `internal/auth/mocks/mock_PlayerRepository.go` (regenerate)

- [ ] **Step 1: Add Count to PlayerRepository interface**

In `internal/auth/player.go`, add after `GetByEmail` (around line 141):

```go
// Count returns the total number of players.
Count(ctx context.Context) (int, error)
```

- [ ] **Step 2: Implement in Postgres**

In `internal/auth/postgres/player_repo.go`, add:

```go
// Count returns the total number of players.
func (r *PlayerRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM players`).Scan(&count)
	if err != nil {
		return 0, oops.With("operation", "count players").Wrap(err)
	}
	return count, nil
}
```

- [ ] **Step 3: Regenerate mocks**

Run: `mockery`
Verify `internal/auth/mocks/mock_PlayerRepository.go` has `Count` method.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/auth/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

`jj desc -m "feat(auth): add Count method to PlayerRepository"`

---

### Task 8: Alias seeding bootstrap

**Files:**

- Create: `internal/bootstrap/aliases.go`
- Create: `internal/bootstrap/aliases_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/bootstrap/aliases_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
)

func TestSeedSystemAliases_SeedsAllThree(t *testing.T) {
	repo := &fakeAliasRepo{aliases: make(map[string]string)}
	cache := command.NewAliasCache()

	err := SeedSystemAliases(context.Background(), repo, cache)
	require.NoError(t, err)

	assert.Equal(t, "say", repo.aliases[`"`])
	assert.Equal(t, "pose", repo.aliases[":"])
	assert.Equal(t, "pose", repo.aliases[";"])
}

func TestSeedSystemAliases_SkipsExisting(t *testing.T) {
	repo := &fakeAliasRepo{aliases: map[string]string{`"`: "say"}}
	cache := command.NewAliasCache()

	err := SeedSystemAliases(context.Background(), repo, cache)
	require.NoError(t, err)

	// " already existed; only : and ; should be seeded
	assert.Equal(t, 2, repo.setCalls, "should only call SetSystemAlias for missing aliases")
}

// fakeAliasRepo is a minimal in-memory alias repo for unit tests.
type fakeAliasRepo struct {
	aliases  map[string]string
	setCalls int
}

func (f *fakeAliasRepo) GetSystemAliases(_ context.Context) (map[string]string, error) {
	return f.aliases, nil
}

func (f *fakeAliasRepo) SetSystemAlias(_ context.Context, alias, cmd, _ string) error {
	f.aliases[alias] = cmd
	f.setCalls++
	return nil
}
```

Note: `fakeAliasRepo` only needs the two methods `SeedSystemAliases` actually calls. It does not need to satisfy the full `store.AliasRepository` interface. Define a local interface in `aliases.go` for just these methods.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bootstrap/... -v`
Expected: FAIL (package doesn't exist yet)

- [ ] **Step 3: Implement SeedSystemAliases**

Create `internal/bootstrap/aliases.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package bootstrap provides first-boot initialization for HoloMUSH.
package bootstrap

import (
	"context"
	"log/slog"

	"github.com/holomush/holomush/internal/command"
)

// AliasSeeder is the subset of store.AliasRepository needed for seeding.
type AliasSeeder interface {
	GetSystemAliases(ctx context.Context) (map[string]string, error)
	SetSystemAlias(ctx context.Context, alias, command, createdBy string) error
}

// standardAliases defines the MUSH aliases seeded on every startup.
var standardAliases = []struct {
	alias   string
	command string
}{
	{`"`, "say"},
	{":", "pose"},
	{";", "pose"},
}

// SeedSystemAliases ensures standard MUSH command aliases exist in the database
// and alias cache. Idempotent — skips aliases that already exist.
func SeedSystemAliases(ctx context.Context, repo AliasSeeder, cache *command.AliasCache) error {
	existing, err := repo.GetSystemAliases(ctx)
	if err != nil {
		return err
	}

	var seeded []string
	for _, a := range standardAliases {
		if _, exists := existing[a.alias]; exists {
			continue
		}
		if err := repo.SetSystemAlias(ctx, a.alias, a.command, "system:bootstrap"); err != nil {
			return err
		}
		seeded = append(seeded, a.alias)
	}

	if len(seeded) > 0 {
		slog.Info("seeded system aliases", "aliases", seeded)
	}

	// Always reload all system aliases into cache (handles both fresh seed
	// and subsequent boots where aliases already exist in the database).
	all, reloadErr := repo.GetSystemAliases(ctx)
	if reloadErr != nil {
		return reloadErr
	}
	cache.LoadSystemAliases(all)

	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/bootstrap/... -v`
Expected: PASS

- [ ] **Step 5: Add test for idempotency with pre-populated cache**

Add a test that calls `SeedSystemAliases` twice and verifies no double-seeding and no errors.

- [ ] **Step 6: Commit**

`jj desc -m "feat(bootstrap): seed standard MUSH command aliases at startup"`

---

### Task 9: Admin bootstrap

**Files:**

- Create: `internal/bootstrap/admin.go`
- Create: `internal/bootstrap/admin_test.go`

- [ ] **Step 1: Write failing test for admin creation on empty DB**

Create `internal/bootstrap/admin_test.go` with a test using fakes for `PlayerRepository.Count`, `PlayerRepository.Create`, `CharacterService.Create`, `RoleStore.AddRole`, and `auth.PasswordHasher`.

Test: when `Count()` returns 0, admin player and character are created, admin role is assigned.

- [ ] **Step 2: Write failing test for skip on non-empty DB**

Test: when `Count()` returns 1, nothing is created.

- [ ] **Step 3: Write failing test for env var overrides**

Test: when env vars `HOLOMUSH_ADMIN_USERNAME`, `HOLOMUSH_ADMIN_PASSWORD`, `HOLOMUSH_ADMIN_CHARACTER` are set, those values are used instead of defaults.

- [ ] **Step 4: Implement SeedAdmin**

Create `internal/bootstrap/admin.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"os"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/naming"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/world"
)

// CharacterCreator is the subset of auth.CharacterService needed for admin bootstrap.
type CharacterCreator interface {
	Create(ctx context.Context, playerID ulid.ULID, name string) (*world.Character, error)
}

// SeedAdminDeps holds the dependencies for admin bootstrapping.
type SeedAdminDeps struct {
	PlayerRepo   auth.PlayerRepository
	CharService  CharacterCreator
	RoleStore    store.RoleStore
	Hasher       auth.PasswordHasher
	NameTheme    naming.Theme
}

// SeedAdmin creates an admin player and character on first boot.
// Skips if any players already exist. Reads overrides from env vars:
//   - HOLOMUSH_ADMIN_USERNAME (default: "admin")
//   - HOLOMUSH_ADMIN_PASSWORD (default: random 24-char)
//   - HOLOMUSH_ADMIN_CHARACTER (default: random star name)
func SeedAdmin(ctx context.Context, deps SeedAdminDeps) error {
	count, err := deps.PlayerRepo.Count(ctx)
	if err != nil {
		return oops.Code("ADMIN_BOOTSTRAP_FAILED").Wrap(err)
	}
	if count > 0 {
		slog.Debug("admin bootstrap skipped: players already exist", "count", count)
		return nil
	}

	username := envOrDefault("HOLOMUSH_ADMIN_USERNAME", "admin")
	password, generated := envOrGenerate("HOLOMUSH_ADMIN_PASSWORD")
	charName := os.Getenv("HOLOMUSH_ADMIN_CHARACTER")
	if charName == "" {
		charName, _ = deps.NameTheme.Generate()
	}

	hash, err := deps.Hasher.Hash(password)
	if err != nil {
		return oops.Code("ADMIN_BOOTSTRAP_FAILED").With("operation", "hash password").Wrap(err)
	}

	player, err := auth.NewPlayer(username, nil, hash)
	if err != nil {
		return oops.Code("ADMIN_BOOTSTRAP_FAILED").With("operation", "create player").Wrap(err)
	}
	if err := deps.PlayerRepo.Create(ctx, player); err != nil {
		return oops.Code("ADMIN_BOOTSTRAP_FAILED").With("operation", "persist player").Wrap(err)
	}

	char, err := deps.CharService.Create(ctx, player.ID, charName)
	if err != nil {
		return oops.Code("ADMIN_BOOTSTRAP_FAILED").With("operation", "create character").Wrap(err)
	}

	if err := deps.RoleStore.AddRole(ctx, char.ID.String(), access.RoleAdmin); err != nil {
		return oops.Code("ADMIN_BOOTSTRAP_FAILED").With("operation", "assign admin role").Wrap(err)
	}

	if generated {
		slog.Info("admin account created",
			"username", username,
			"character", charName,
			"password", password,
		)
	} else {
		slog.Info("admin account created",
			"username", username,
			"character", charName,
		)
	}

	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrGenerate(key string) (value string, generated bool) {
	if v := os.Getenv(key); v != "" {
		return v, false
	}
	b := make([]byte, 18) // 18 bytes → 24 base64 chars
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.URLEncoding.EncodeToString(b), true
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/bootstrap/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

`jj desc -m "feat(bootstrap): admin player and character seeding on first boot"`

---

## Chunk 3: Pose NoSpace, expandMUSHPrefix Removal, Core Wiring

### Task 10: Pose NoSpace payload flag

**Files:**

- Modify: `internal/core/engine.go:20-24` (PosePayload)
- Modify: `internal/command/handlers/pose.go` (check InvokedAs)
- Modify: `internal/command/handlers/pose_test.go` (new test case)
- Modify: `internal/telnet/gateway_handler.go:627` (render NoSpace)
- Modify: `web/src/lib/components/terminal/EventRenderer.svelte:22-24` (render NoSpace)

- [ ] **Step 1: Add NoSpace field to PosePayload**

In `internal/core/engine.go:21-24`:

```go
type PosePayload struct {
	CharacterName string `json:"character_name"`
	Action        string `json:"action"`
	NoSpace       bool   `json:"no_space,omitempty"`
}
```

- [ ] **Step 2: Update pose handler to set NoSpace**

In `internal/command/handlers/pose.go:22-25`:

```go
payload, err := json.Marshal(core.PosePayload{
	CharacterName: exec.CharacterName(),
	Action:        exec.Args,
	NoSpace:       exec.InvokedAs == ";",
})
```

- [ ] **Step 3: Write pose test for no-space**

In `internal/command/handlers/pose_test.go`, add a test case where `InvokedAs` is `";"` and verify the event payload has `NoSpace: true`.

- [ ] **Step 4: Update telnet gateway rendering**

In `internal/telnet/gateway_handler.go:627`:

```go
// Before:
h.send(fmt.Sprintf("%s %s", p.CharacterName, p.Action))

// After:
if p.NoSpace {
	h.send(fmt.Sprintf("%s%s", p.CharacterName, p.Action))
} else {
	h.send(fmt.Sprintf("%s %s", p.CharacterName, p.Action))
}
```

- [ ] **Step 5: Update web translate layer**

The web gateway has its own `posePayload` struct in `internal/web/translate.go:24-27`. Add `NoSpace`:

```go
type posePayload struct {
	CharacterName string `json:"character_name"`
	Action        string `json:"action"`
	NoSpace       bool   `json:"no_space,omitempty"`
}
```

In the `"pose"` case of `translateEvent()` (lines 101-113), pass `no_space` via the `Metadata` field on `GameEvent`:

```go
case "pose":
	var p posePayload
	if err := json.Unmarshal(ev.GetPayload(), &p); err != nil {
		slog.Error("web: failed to unmarshal pose payload", "error", err)
		return nil
	}
	ge := &webv1.GameEvent{
		Type:          "pose",
		CharacterName: p.CharacterName,
		Text:          p.Action,
		Timestamp:     ts,
		Channel:       ch,
	}
	if p.NoSpace {
		meta, _ := structpb.NewStruct(map[string]interface{}{"no_space": true})
		ge.Metadata = meta
	}
	return ge
```

- [ ] **Step 6: Update web client EventRenderer**

In `web/src/lib/components/terminal/EventRenderer.svelte`, update the pose block (lines 22-24). The `metadata` field carries `no_space`:

```svelte
{:else if event.type === 'pose'}
  <span class="actor">{event.characterName}</span>{#if !event.metadata?.no_space}{' '}{/if}<span class="action">{@html linkUrls(event.text)}</span>
```

Check that the `event` type in the component's `Props` interface includes `metadata?: Record<string, unknown>` (it should already via the existing type definition).

- [ ] **Step 7: Run tests**

Run: `go test ./internal/command/handlers/... -run TestPose -v`
Expected: PASS

- [ ] **Step 8: Commit**

`jj desc -m "feat(pose): add NoSpace flag for semicolon-invoked pose"`

---

### Task 11: Remove expandMUSHPrefix

**Files:**

- Modify: `internal/grpc/server.go:398-401` (remove call)
- Modify: `internal/grpc/server.go:489-507` (remove function)
- Modify: `internal/grpc/server_test.go` (remove any tests for expandMUSHPrefix)

- [ ] **Step 1: Remove the function call**

In `internal/grpc/server.go`, remove lines 399-401:

```go
// Remove these lines:
// Expand MUSH single-character prefixes before dispatch.
// These will become proper system aliases when the alias cache is wired.
input = expandMUSHPrefix(input)
```

- [ ] **Step 2: Remove the function definition**

Delete `expandMUSHPrefix` (lines 489-507).

- [ ] **Step 3: Remove tests if any**

Search for `expandMUSHPrefix` in test files and remove.

Run: `grep -rn expandMUSHPrefix internal/grpc/`

- [ ] **Step 4: Run tests**

Run: `go test ./internal/grpc/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

`jj desc -m "refactor: remove expandMUSHPrefix, replaced by alias cache"`

---

### Task 12: Wire bootstrap into core.go

**Files:**

- Modify: `cmd/holomush/core.go` (add bootstrap calls at the right startup points)

- [ ] **Step 1: Add admin bootstrap call**

After auth services are wired (around line 340, after `characterService` is created), add:

```go
// Admin bootstrap — create admin account on first boot
adminBootErr := bootstrap.SeedAdmin(ctx, bootstrap.SeedAdminDeps{
	PlayerRepo:  authPlayerRepo,
	CharService: characterService,
	RoleStore:   roleStore,
	Hasher:      authHasher,
	NameTheme:   naming.NewStarTheme(),
})
if adminBootErr != nil {
	return oops.Code("ADMIN_BOOTSTRAP_FAILED").Wrap(adminBootErr)
}
```

- [ ] **Step 2: Add alias seeding call**

After alias cache is created and system aliases are loaded (around line 462), add:

```go
// Seed standard MUSH aliases (idempotent)
if seedErr := bootstrap.SeedSystemAliases(ctx, aliasRepo, aliasCache); seedErr != nil {
	return oops.Code("ALIAS_SEED_FAILED").Wrap(seedErr)
}
```

This replaces the existing `GetSystemAliases` + `LoadSystemAliases` block (lines 456-462), since `SeedSystemAliases` handles loading into the cache.

- [ ] **Step 3: Add imports**

Add imports for `internal/bootstrap` and `internal/naming`.

- [ ] **Step 4: Run tests**

Run: `task test`
Expected: PASS

- [ ] **Step 5: Commit**

`jj desc -m "feat: wire alias seeding and admin bootstrap into core startup"`

---

### Task 13: InvokedAs documentation

**Files:**

- Modify: `internal/command/types.go:240-244` (enhance doc comment)

- [ ] **Step 1: Enhance InvokedAs doc comment**

Replace the existing comment at lines 240-244:

```go
// InvokedAs is the original command name as typed by the user, before alias
// resolution. Handlers can use this to alter behavior based on which alias
// invoked them. For example, the pose handler checks InvokedAs == ";" to
// distinguish no-space pose (;'s eyes glow → "Alaric's eyes glow") from
// standard pose (: waves → "Alaric waves"). This pattern allows a single
// registered command to serve multiple alias variants without separate
// command registrations.
InvokedAs string
```

- [ ] **Step 2: Add developer documentation**

Create or update a section in `site/docs/developers/` about the `InvokedAs` convention. Cover:

- What `InvokedAs` is (the original alias before resolution)
- How to use it in command handlers
- The pose/semipose canonical example
- When to use this pattern vs registering separate commands

- [ ] **Step 3: Commit**

`jj desc -m "docs: enhance InvokedAs convention documentation"`

---

## Chunk 4: Quality Gates

### Task 14: Full test suite and lint

- [ ] **Step 1: Run unit tests**

Run: `task test`
Expected: PASS

- [ ] **Step 2: Run linter**

Run: `task lint`
Expected: PASS (fix any issues)

- [ ] **Step 3: Run integration tests**

Run: `task test:int`
Expected: PASS

- [ ] **Step 4: Run E2E tests**

Run: `task test:e2e`
Expected: PASS

- [ ] **Step 5: Verify alias resolution E2E**

Manually verify (or add E2E test) that connecting via telnet and typing `"hello` produces a say event, `:waves` produces a pose event with space, and `;'s eyes` produces a pose event without space.

- [ ] **Step 6: Squash jj commits and create branch for PR**

```bash
# Review all commits on this branch
jj log --limit 15

# Create a new change, then squash all work into it
# Use jj squash to fold changes, or create a bookmark on the
# final change. Exact workflow depends on how many changes exist.
jj bookmark create bootstrap-aliases-admin -r @
jj git push --bookmark bootstrap-aliases-admin
```

- [ ] **Step 7: Close beads**

```bash
bd close holomush-a3a7.3 holomush-qcn6 --reason="Implemented in bootstrap-aliases-admin PR"
```
