<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Phase 5 Sub-epic B Implementation Plan — `crypto.operator` capability

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the `crypto.operator` capability — a player-attribute grant exposed via ABAC, plus the typed Go facade D consumes, plus the top-level `crypto:` YAML config block, plus 14 master-spec amendments and a stub operator doc.

**Architecture:** Introduce `player:` as a Subject namespace in ABAC via a new `PlayerAttributeProvider` exposing `player.id` and `player.grants`. The capability is a flat narrowing grant (string `"crypto.operator"`) on top of `RoleAdmin`; v1 storage is a config-file allow-list. Sub-epic D consumes via the typed facade `access.HasPlayerGrant`; no ABAC seed policy in B (seam documented for future migration).

**Tech Stack:** Go, koanf (config), pgxpool/pgx (PG), slog (logging), oops (errors), reflect (no-mutation meta-test). No new dependencies.

**Spec:** [`docs/superpowers/specs/2026-05-08-event-payload-crypto-phase5-sub-epic-b-design.md`](../specs/2026-05-08-event-payload-crypto-phase5-sub-epic-b-design.md).

---

## File Structure

**New (created by this plan):**

- `internal/access/grants.go` — `CapabilityCryptoOperator` constant + `PlayerSubject` helper + `HasPlayerGrant` facade.
- `internal/access/grants_test.go` — facade and constant unit tests.
- `internal/access/policy/attribute/player.go` — `PlayerAttributeProvider`.
- `internal/access/policy/attribute/player_test.go` — provider unit + contract tests.
- `cmd/holomush/crypto_operator_validation_test.go` — startup-validation integration test (build-tag `//go:build integration`).
- `internal/access/spec_amendments_test.go` — meta-test for §"Master-spec amendments inventory".
- `scripts/check_bead_jxo8_5.sh` — bead-description fingerprint check.
- `site/docs/operating/crypto-setup.md` — operator doc stub (master spec §9.2 marks this as Phase-8 scope; B creates a minimal stub).

**Modified:**

- `internal/access/prefix.go` — add `SubjectPlayer = "player:"` to the const block; add `SubjectPlayer` to `knownPrefixes`.
- `internal/access/prefix_test.go` — extended.
- `internal/access/role.go` (read-only reference; no edits expected).
- `internal/config/config.go` — add `CryptoConfig` struct + `DefaultCryptoConfig()`.
- `internal/config/config_test.go` — extended.
- `internal/access/setup/setup.go` — register `PlayerAttributeProvider` with the resolver.
- `internal/access/setup/setup_test.go` — namespace-registration test.
- `cmd/holomush/core.go` — load `CryptoConfig`, run cross-check, build provider, register.
- `internal/auth/postgres/player_repo.go` — add `ExistingIDs(ctx, ids []string) ([]string, error)` bulk-existence helper.
- `internal/auth/postgres/player_repo_test.go` — extended.
- `Taskfile.yaml` — wire `scripts/check_bead_jxo8_5.sh` into `task pr-prep`.
- `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` — 14 master-spec amendments.
- `docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-decomposition.md` — drift-fix verification (already applied during spec brainstorm; this plan asserts via meta-test).

---

## Tasks

### Task 1: Add `SubjectPlayer` constant + `knownPrefixes` extension

**Files:**

- Modify: `internal/access/prefix.go`
- Modify: `internal/access/prefix_test.go`

- [ ] **Step 1: Write failing test for `SubjectPlayer` constant + `ParseEntityRef` acceptance**

In `internal/access/prefix_test.go` (extending the existing file; create one if absent), add:

```go
func TestSubjectPlayerConstant(t *testing.T) {
    assert.Equal(t, "player:", SubjectPlayer)
}

func TestParseEntityRefAcceptsPlayerNamespace(t *testing.T) {
    typeName, id, err := ParseEntityRef("player:01HZAVGE83MGFEXQQH5SP9NXKF")
    assert.NoError(t, err)
    assert.Equal(t, "player", typeName)
    assert.Equal(t, "01HZAVGE83MGFEXQQH5SP9NXKF", id)
}

func TestParseEntityRefRejectsEmptyPlayerID(t *testing.T) {
    _, _, err := ParseEntityRef("player:")
    require.Error(t, err)
    assert.Contains(t, err.Error(), "empty ID in entity reference")
}
```

(Imports: `"testing"`, `"github.com/stretchr/testify/assert"`, `"github.com/stretchr/testify/require"`. Tests live in `package access`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run "TestSubjectPlayerConstant|TestParseEntityRefAcceptsPlayerNamespace|TestParseEntityRefRejectsEmptyPlayerID" ./internal/access/`

Expected: FAIL — `SubjectPlayer` undefined; `ParseEntityRef("player:01HZAVGE...")` returns `unknown entity reference prefix: player:01HZAVGE...`.

- [ ] **Step 3: Add `SubjectPlayer` constant and append to `knownPrefixes`**

In `internal/access/prefix.go`, edit the Subject prefix const block (lines 13-18):

```go
// Subject prefix constants identify the type of entity making a request.
const (
    SubjectCharacter = "character:"
    SubjectPlugin    = "plugin:"
    SubjectSystem    = "system"
    SubjectSession   = "session:"
    SubjectPlayer    = "player:"
)
```

Then edit the `knownPrefixes` slice (lines 42-56) to add `SubjectPlayer`:

```go
var knownPrefixes = []string{
    SubjectCharacter,
    SubjectPlugin,
    SubjectSession,
    SubjectPlayer,
    ResourceCharacter,
    ResourceLocation,
    ResourceObject,
    ResourceCommand,
    ResourceProperty,
    ResourceStream,
    ResourceExit,
    ResourceScene,
    ResourceKV,
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run "TestSubjectPlayerConstant|TestParseEntityRefAcceptsPlayerNamespace|TestParseEntityRefRejectsEmptyPlayerID" ./internal/access/`

Expected: PASS for all three.

- [ ] **Step 5: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`. Suggested message:

```
feat(access): add SubjectPlayer namespace constant (holomush-jxo8.5.1)

Adds player: as a Subject prefix alongside character:, plugin:, system,
session:. Extends knownPrefixes so ParseEntityRef accepts "player:<ulid>".
Foundation for sub-epic B's PlayerAttributeProvider.
```

---

### Task 2: Add `CapabilityCryptoOperator` constant + `PlayerSubject` helper

**Files:**

- Create: `internal/access/grants.go`
- Create: `internal/access/grants_test.go`

- [ ] **Step 1: Write failing tests for the constant and helper**

In `internal/access/grants_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

import (
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestCapabilityCryptoOperatorIsCryptoOperator(t *testing.T) {
    assert.Equal(t, "crypto.operator", CapabilityCryptoOperator)
}

func TestPlayerSubject(t *testing.T) {
    assert.Equal(t, "player:01HZAVGE83MGFEXQQH5SP9NXKF",
        PlayerSubject("01HZAVGE83MGFEXQQH5SP9NXKF"))
}

func TestPlayerSubjectPanicsOnEmpty(t *testing.T) {
    assert.PanicsWithValue(t,
        "access.PlayerSubject: empty playerID would bypass access control",
        func() { _ = PlayerSubject("") },
    )
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run "TestCapability|TestPlayerSubject" ./internal/access/`

Expected: FAIL — `CapabilityCryptoOperator` undefined; `PlayerSubject` undefined.

- [ ] **Step 3: Implement constant, helper, and SubjectResolver interface**

`internal/access/policy/attribute/resolver.go` defines `Resolver` as a
**concrete struct**, not an interface (verified at line 29). Per Go
"accept interfaces" convention, the consuming package (`internal/access`)
defines a narrow interface naming exactly the methods it needs from the
resolver. `*attribute.Resolver` satisfies it implicitly; test fakes
implement it explicitly.

In `internal/access/grants.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

import (
    "context"

    "github.com/samber/oops"

    "github.com/holomush/holomush/internal/access/policy/types"
)

// CapabilityCryptoOperator is the grant string that gates break-glass
// crypto operations (Rekey, AdminReadStream). Held by players in the
// crypto.operators config allow-list. MUST be combined with RoleAdmin
// to authorize break-glass operations.
const CapabilityCryptoOperator = "crypto.operator"

// SubjectResolver is the narrow interface HasPlayerGrant requires from
// the ABAC attribute resolver. *attribute.Resolver satisfies this
// implicitly. Defined in the consuming package so test fakes can
// substitute without importing the full resolver internals.
type SubjectResolver interface {
    ResolveSubjectAttributes(ctx context.Context, subjectID string, action string) (*types.AttributeBags, error)
}

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

// HasPlayerGrant returns true iff the given player holds the named
// grant. The grant set is resolved through the SubjectResolver, which
// MUST be configured with a PlayerAttributeProvider (see
// internal/access/policy/attribute/player.go) for this to return
// non-empty results.
//
// Returns (false, nil) when the player has no matching grant.
// Returns (false, error) on resolver errors or invalid input. Empty
// playerID and empty grant are rejected at the API boundary with typed
// errors, without invoking the resolver.
func HasPlayerGrant(ctx context.Context, resolver SubjectResolver,
    playerID string, grant string) (bool, error) {
    if playerID == "" {
        return false, oops.
            Code("PLAYER_ID_EMPTY").
            With("grant", grant).
            Errorf("playerID must be non-empty")
    }
    if grant == "" {
        return false, oops.
            Code("GRANT_EMPTY").
            With("player_id", playerID).
            Errorf("grant must be non-empty")
    }

    bags, err := resolver.ResolveSubjectAttributes(ctx, PlayerSubject(playerID), "")
    if err != nil {
        return false, oops.
            With("player_id", playerID).
            With("grant", grant).
            Wrap(err)
    }

    raw, ok := bags.Subject["player.grants"]
    if !ok {
        return false, nil
    }
    grants, ok := raw.([]string)
    if !ok {
        return false, nil
    }
    for _, g := range grants {
        if g == grant {
            return true, nil
        }
    }
    return false, nil
}
```

The import path for `types.AttributeBags` is
`github.com/holomush/holomush/internal/access/policy/types`. Verify
`*types.AttributeBags` shape by reading `types.go` if surprised.

- [ ] **Step 4: Run constant + PlayerSubject tests to verify they pass**

Run: `task test -- -run "TestCapability|TestPlayerSubject" ./internal/access/`

Expected: PASS for `TestCapabilityCryptoOperatorIsCryptoOperator`, `TestPlayerSubject`, `TestPlayerSubjectPanicsOnEmpty`. (HasPlayerGrant tests come in Task 7.)

- [ ] **Step 5: Commit**

Suggested message:

```
feat(access): CapabilityCryptoOperator + PlayerSubject helper (holomush-jxo8.5.2)

Adds the crypto.operator grant constant and the canonical subject-ID
formatter. Plus the HasPlayerGrant facade scaffolding (tests for the
facade arrive once PlayerAttributeProvider is in place).
```

---

### Task 3: Players repo bulk-existence helper

**Files:**

- Modify: `internal/auth/postgres/player_repo.go`
- Modify: `internal/auth/postgres/player_repo_test.go`

- [ ] **Step 1: Write failing test for `ExistingIDs`**

The existing `internal/auth/postgres/player_repo_test.go` is `package
postgres_test`, build-tag `//go:build integration`, uses a module-level
`testPool` and inline `&auth.Player{...}` literals (no `setupPlayerRepoTest`
or `newTestPlayer` helpers). Match that pattern. Append to the file:

```go
func TestPlayerRepository_ExistingIDs(t *testing.T) {
    ctx := context.Background()
    repo := postgres.NewPlayerRepository(testPool)

    p1 := &auth.Player{
        ID:           ulid.Make(),
        Username:     "existing_ids_user_1",
        PasswordHash: "hash",
        CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
        UpdatedAt:    time.Now().UTC().Truncate(time.Microsecond),
    }
    p2 := &auth.Player{
        ID:           ulid.Make(),
        Username:     "existing_ids_user_2",
        PasswordHash: "hash",
        CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
        UpdatedAt:    time.Now().UTC().Truncate(time.Microsecond),
    }
    require.NoError(t, repo.Create(ctx, p1))
    require.NoError(t, repo.Create(ctx, p2))
    t.Cleanup(func() {
        _, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = ANY($1::text[])`,
            []string{p1.ID.String(), p2.ID.String()})
    })

    nonexistent := ulid.Make().String()

    found, err := repo.ExistingIDs(ctx, []string{
        p1.ID.String(),
        nonexistent,
        p2.ID.String(),
    })
    require.NoError(t, err)
    assert.ElementsMatch(t,
        []string{p1.ID.String(), p2.ID.String()},
        found,
        "should return only the IDs that exist in the players table")
}

func TestPlayerRepository_ExistingIDs_EmptyInput(t *testing.T) {
    ctx := context.Background()
    repo := postgres.NewPlayerRepository(testPool)

    found, err := repo.ExistingIDs(ctx, nil)
    require.NoError(t, err)
    assert.Empty(t, found, "nil input should return empty slice without querying")

    found, err = repo.ExistingIDs(ctx, []string{})
    require.NoError(t, err)
    assert.Empty(t, found, "empty input should return empty slice without querying")
}
```

(Imports already present in the file: `context`, `testing`, `time`,
`ulid/v2`, `require`, `assert`, `auth`, `postgres`. No new imports needed.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test:int -- -run "TestPlayerRepository_ExistingIDs" ./internal/auth/postgres/`

Expected: FAIL — `repo.ExistingIDs` is undefined.

- [ ] **Step 3: Implement `ExistingIDs`**

In `internal/auth/postgres/player_repo.go`, append:

```go
// ExistingIDs returns the subset of the input ID strings that exist in
// the players table. Used by the crypto.operators startup cross-check
// (sub-epic B) to identify configured operator IDs that don't correspond
// to any player. Read-only; no schema mutation.
//
// Returns an empty slice for nil or empty input without issuing a query.
// Returns the IDs in arbitrary order (caller must not depend on input order).
func (r *PlayerRepository) ExistingIDs(ctx context.Context, ids []string) ([]string, error) {
    if len(ids) == 0 {
        return []string{}, nil
    }

    rows, err := r.pool.Query(ctx,
        `SELECT id FROM players WHERE id = ANY($1::text[])`,
        ids,
    )
    if err != nil {
        return nil, oops.
            Code("PLAYER_REPO_EXISTING_IDS_FAILED").
            With("input_count", len(ids)).
            Wrap(err)
    }
    defer rows.Close()

    found := make([]string, 0, len(ids))
    for rows.Next() {
        var id string
        if err := rows.Scan(&id); err != nil {
            return nil, oops.Code("PLAYER_REPO_EXISTING_IDS_SCAN_FAILED").Wrap(err)
        }
        found = append(found, id)
    }
    if err := rows.Err(); err != nil {
        return nil, oops.Code("PLAYER_REPO_EXISTING_IDS_ROWS_FAILED").Wrap(err)
    }
    return found, nil
}
```

(Imports: ensure `"context"` and `"github.com/samber/oops"` are already in the file's import block; they should be.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test:int -- -run "TestPlayerRepository_ExistingIDs" ./internal/auth/postgres/`

Expected: PASS for both.

- [ ] **Step 5: Commit**

Suggested message:

```
feat(auth): PlayerRepository.ExistingIDs bulk-existence helper (holomush-jxo8.5.3)

Read-only bulk lookup for the crypto.operators startup cross-check.
Returns the subset of input IDs present in the players table. Empty
input returns empty slice without querying.
```

---

### Task 4: `CryptoConfig` struct + parsing

**Files:**

- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write failing tests for `CryptoConfig` parsing**

In `internal/config/config_test.go`, append:

```go
func TestDefaultCryptoConfigIsEmpty(t *testing.T) {
    cfg := DefaultCryptoConfig()
    assert.Empty(t, cfg.Operators)
}

func TestLoadParsesCryptoOperators(t *testing.T) {
    dir := t.TempDir()
    cfgFile := filepath.Join(dir, "config.yaml")
    err := os.WriteFile(cfgFile, []byte(`crypto:
  operators:
    - "01HZAVGE83MGFEXQQH5SP9NXKF"
    - "01HZAVGE83MGFEXQQH5SP9NXKG"
`), 0o600)
    require.NoError(t, err)

    cfg := DefaultCryptoConfig()
    cmd := newTestCmd(nil)  // existing helper
    require.NoError(t, Load(cfgFile, cmd, &cfg, "crypto"))

    assert.Equal(t, []string{
        "01HZAVGE83MGFEXQQH5SP9NXKF",
        "01HZAVGE83MGFEXQQH5SP9NXKG",
    }, cfg.Operators)
}

func TestLoadCryptoMissingSectionIsEmpty(t *testing.T) {
    dir := t.TempDir()
    cfgFile := filepath.Join(dir, "config.yaml")
    err := os.WriteFile(cfgFile, []byte(`other:
  setting: value
`), 0o600)
    require.NoError(t, err)

    cfg := DefaultCryptoConfig()
    cmd := newTestCmd(nil)
    require.NoError(t, Load(cfgFile, cmd, &cfg, "crypto"))
    assert.Empty(t, cfg.Operators)
}

func TestLoadCryptoOperatorsEmptyListIsEmpty(t *testing.T) {
    dir := t.TempDir()
    cfgFile := filepath.Join(dir, "config.yaml")
    err := os.WriteFile(cfgFile, []byte(`crypto:
  operators: []
`), 0o600)
    require.NoError(t, err)

    cfg := DefaultCryptoConfig()
    cmd := newTestCmd(nil)
    require.NoError(t, Load(cfgFile, cmd, &cfg, "crypto"))
    assert.Empty(t, cfg.Operators)
}

func TestLoadCryptoOperatorsMalformedFails(t *testing.T) {
    dir := t.TempDir()
    cfgFile := filepath.Join(dir, "config.yaml")
    err := os.WriteFile(cfgFile, []byte(`crypto:
  operators: "not a list"
`), 0o600)
    require.NoError(t, err)

    cfg := DefaultCryptoConfig()
    cmd := newTestCmd(nil)
    err = Load(cfgFile, cmd, &cfg, "crypto")
    require.Error(t, err)
    // Match the existing CONFIG_UNMARSHAL_FAILED code via errutil if
    // available; otherwise assert via err.Error() substring.
    assert.Contains(t, err.Error(), "CONFIG_UNMARSHAL_FAILED")
}
```

If `newTestCmd(nil)` doesn't accept nil today, pass an empty `*testConfig{}` per existing pattern. Inspect `config_test.go` line 23-34 to confirm signature.

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run "TestDefaultCryptoConfig|TestLoadParsesCryptoOperators|TestLoadCryptoMissingSection|TestLoadCryptoOperatorsEmptyList|TestLoadCryptoOperatorsMalformed" ./internal/config/`

Expected: FAIL — `CryptoConfig`, `DefaultCryptoConfig` undefined.

- [ ] **Step 3: Implement `CryptoConfig`**

In `internal/config/config.go`, add (after `AuthConfig` block, around line 56):

```go
// CryptoConfig holds crypto-related server configuration loaded from
// the top-level "crypto" YAML section. Sub-epic B introduces this block
// with operators as its first tenant; future sub-epics (e.g., D's
// dual_control_required) extend the same block.
type CryptoConfig struct {
    // Operators is the allow-list of player IDs (ULIDs) that hold the
    // crypto.operator capability — the narrowing grant required (in
    // addition to RoleAdmin) for break-glass operations.
    //
    // Lax+warn validation: at startup the server cross-checks each ID
    // against the players table and emits a structured warning per
    // unknown ID. The configured list is used as-is regardless;
    // unknown IDs become inert grants (no one can authenticate as a
    // nonexistent player).
    //
    // Empty / missing → no operators → break-glass impossible.
    // Reload requires server restart in v1.
    Operators []string `koanf:"operators"`
}

// DefaultCryptoConfig returns an empty CryptoConfig — no operators,
// break-glass disabled. Operators MUST explicitly populate the list.
func DefaultCryptoConfig() CryptoConfig {
    return CryptoConfig{
        Operators: []string{},
    }
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run "TestDefaultCryptoConfig|TestLoadParsesCryptoOperators|TestLoadCryptoMissingSection|TestLoadCryptoOperatorsEmptyList|TestLoadCryptoOperatorsMalformed" ./internal/config/`

Expected: PASS for all five.

- [ ] **Step 5: Commit**

Suggested message:

```
feat(config): CryptoConfig with operators allow-list (holomush-jxo8.5.4)

Introduces the top-level crypto: YAML block. First tenant is
crypto.operators (allow-list of player IDs that hold the crypto.operator
capability). Future sub-epics extend the block with dual_control_required
etc.
```

---

### Task 5: `PlayerAttributeProvider` scaffolding (Namespace, Schema, ResolveResource, constructor)

**Files:**

- Create: `internal/access/policy/attribute/player.go`
- Create: `internal/access/policy/attribute/player_test.go`

- [ ] **Step 1: Write failing tests for scaffolding**

In `internal/access/policy/attribute/player_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"

    "github.com/holomush/holomush/internal/access/policy/types"
)

func TestPlayerProviderNamespace(t *testing.T) {
    p := NewPlayerAttributeProvider(nil)
    assert.Equal(t, "player", p.Namespace())
}

func TestPlayerProviderSchema(t *testing.T) {
    p := NewPlayerAttributeProvider(nil)
    schema := p.Schema()
    require.NotNil(t, schema)
    assert.Equal(t, types.AttrTypeString, schema.Attributes["id"])
    assert.Equal(t, types.AttrTypeStringList, schema.Attributes["grants"])
    assert.Len(t, schema.Attributes, 2, "v1 schema exposes only id and grants")
}

func TestPlayerProviderResolveResourceAlwaysNil(t *testing.T) {
    p := NewPlayerAttributeProvider(nil)
    attrs, err := p.ResolveResource(context.Background(), "player:01HZAVGE83MGFEXQQH5SP9NXKF")
    assert.NoError(t, err)
    assert.Nil(t, attrs, "players are subjects, never resources in this design")

    attrs, err = p.ResolveResource(context.Background(), "character:01HZAVGE83MGFEXQQH5SP9NXKG")
    assert.NoError(t, err)
    assert.Nil(t, attrs)
}

func TestPlayerProviderConstructorDeduplicates(t *testing.T) {
    p := NewPlayerAttributeProvider([]string{"01A", "01A", "01B"})
    // Operator-set inspection via test-only accessor; if the provider
    // does not expose one, exercise via ResolveSubject in Task 6 instead.
    assert.Equal(t, 2, p.operatorCount())
}

func TestPlayerProviderConstructorEmptyInput(t *testing.T) {
    p := NewPlayerAttributeProvider(nil)
    assert.Equal(t, 0, p.operatorCount())

    p = NewPlayerAttributeProvider([]string{})
    assert.Equal(t, 0, p.operatorCount())
}
```

(Imports: add `"github.com/stretchr/testify/require"` if not present.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run "TestPlayerProvider" ./internal/access/policy/attribute/`

Expected: FAIL — `PlayerAttributeProvider`, `NewPlayerAttributeProvider`, `operatorCount` undefined.

- [ ] **Step 3: Implement scaffolding**

In `internal/access/policy/attribute/player.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
    "context"

    "github.com/holomush/holomush/internal/access/policy/types"
)

// PlayerAttributeProvider exposes player-level attributes for ABAC
// subject resolution. v1 schema: player.id, player.grants. The grant
// set is captured at construction time from the operator allow-list
// (typically loaded from crypto.operators YAML config) and is read-only
// thereafter — no reload in v1, by design (see sub-epic B spec INV-B6).
type PlayerAttributeProvider struct {
    operators map[string]struct{} // playerID present iff holds crypto.operator grant
}

// NewPlayerAttributeProvider constructs a provider with the given
// operator allow-list. Deduplicates the input. Empty / nil input is
// valid and produces a provider for which every player has no grants.
func NewPlayerAttributeProvider(operatorPlayerIDs []string) *PlayerAttributeProvider {
    set := make(map[string]struct{}, len(operatorPlayerIDs))
    for _, id := range operatorPlayerIDs {
        if id == "" {
            continue
        }
        set[id] = struct{}{}
    }
    return &PlayerAttributeProvider{operators: set}
}

// Namespace returns the attribute namespace this provider handles.
func (p *PlayerAttributeProvider) Namespace() string {
    return "player"
}

// ResolveResource always returns (nil, nil) — players are Subjects in
// this design, not Resources. No resource attributes exposed.
func (p *PlayerAttributeProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
    return nil, nil
}

// Schema returns the v1 namespace schema: id + grants.
func (p *PlayerAttributeProvider) Schema() *types.NamespaceSchema {
    return &types.NamespaceSchema{
        Attributes: map[string]types.AttrType{
            "id":     types.AttrTypeString,
            "grants": types.AttrTypeStringList,
        },
    }
}

// operatorCount is a test-only accessor for assertions on the operator
// set's size. Unexported on purpose — production code MUST NOT depend
// on this method.
func (p *PlayerAttributeProvider) operatorCount() int {
    return len(p.operators)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run "TestPlayerProvider" ./internal/access/policy/attribute/`

Expected: PASS for the five Task-5 tests. (`ResolveSubject` not yet implemented; that's Task 6.)

- [ ] **Step 5: Commit**

Suggested message:

```
feat(access): PlayerAttributeProvider scaffolding (holomush-jxo8.5.5)

Implements Namespace(), Schema(), ResolveResource(), and the constructor
with dedup. ResolveSubject is a stub (returns nil, nil) — actual subject
resolution lands in the next commit.
```

---

### Task 6: `PlayerAttributeProvider.ResolveSubject` — happy + error paths

**Files:**

- Modify: `internal/access/policy/attribute/player.go`
- Modify: `internal/access/policy/attribute/player_test.go`

- [ ] **Step 1: Write failing tests for ResolveSubject**

In `internal/access/policy/attribute/player_test.go`, append:

```go
const (
    testOperatorULID    = "01HZAVGE83MGFEXQQH5SP9NXKF"
    testNonOperatorULID = "01HZAVGE83MGFEXQQH5SP9NXKG"
)

func TestPlayerProviderResolveSubjectOperator(t *testing.T) {
    p := NewPlayerAttributeProvider([]string{testOperatorULID})
    attrs, err := p.ResolveSubject(context.Background(), "player:"+testOperatorULID)
    require.NoError(t, err)
    require.NotNil(t, attrs)

    assert.Equal(t, testOperatorULID, attrs["id"])
    assert.Equal(t, []string{"crypto.operator"}, attrs["grants"])
}

func TestPlayerProviderResolveSubjectNonOperator(t *testing.T) {
    p := NewPlayerAttributeProvider([]string{testOperatorULID})
    attrs, err := p.ResolveSubject(context.Background(), "player:"+testNonOperatorULID)
    require.NoError(t, err)
    require.NotNil(t, attrs)

    assert.Equal(t, testNonOperatorULID, attrs["id"])
    grants, ok := attrs["grants"].([]string)
    require.True(t, ok, "grants must be []string")
    assert.NotNil(t, grants, "grants slice must be non-nil even when empty")
    assert.Empty(t, grants)
}

func TestPlayerProviderResolveSubjectNonPlayerNamespace(t *testing.T) {
    p := NewPlayerAttributeProvider([]string{testOperatorULID})

    cases := []string{
        "character:" + testOperatorULID,
        "location:01HZAVGE83MGFEXQQH5SP9NXKH",
        "plugin:my-plugin",
    }
    for _, sid := range cases {
        t.Run(sid, func(t *testing.T) {
            attrs, err := p.ResolveSubject(context.Background(), sid)
            assert.NoError(t, err)
            assert.Nil(t, attrs, "non-player subject must return nil bag")
        })
    }
}

func TestPlayerProviderResolveSubjectMalformedSubject(t *testing.T) {
    p := NewPlayerAttributeProvider(nil)

    cases := []struct {
        name        string
        subjectID   string
        wantCode    string
        wantSubstr  string
    }{
        {
            name:       "empty post-colon",
            subjectID:  "player:",
            wantCode:   "INVALID_ENTITY_ID",
            wantSubstr: "invalid subject ID format",
        },
        {
            name:       "no colon",
            subjectID:  "playerULID",
            wantCode:   "INVALID_ENTITY_ID",
            wantSubstr: "invalid subject ID format",
        },
        {
            name:       "non-ULID under player namespace",
            subjectID:  "player:not-a-ulid",
            wantCode:   "INVALID_PLAYER_ID",
            wantSubstr: "invalid player ULID",
        },
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            attrs, err := p.ResolveSubject(context.Background(), tc.subjectID)
            assert.Nil(t, attrs)
            require.Error(t, err)
            // Match by code via existing errutil if used elsewhere; otherwise
            // assert via err.Error() substring (oops codes appear in the
            // error string).
            assert.Contains(t, err.Error(), tc.wantCode)
            assert.Contains(t, err.Error(), tc.wantSubstr)
        })
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run "TestPlayerProviderResolveSubject" ./internal/access/policy/attribute/`

Expected: FAIL — `ResolveSubject` doesn't yet implement the parsing branches; current stub returns `(nil, nil)` for everything.

- [ ] **Step 3: Implement ResolveSubject**

In `internal/access/policy/attribute/player.go`, replace the `ResolveSubject` stub with:

```go
import (
    "context"
    "strings"

    "github.com/oklog/ulid/v2"
    "github.com/samber/oops"

    "github.com/holomush/holomush/internal/access"
    "github.com/holomush/holomush/internal/access/policy/types"
)

// ... (existing code) ...

// ResolveSubject resolves player attributes for a "player:<ulid>" subject.
// Returns (nil, nil) for any other namespace (provider declines, resolver
// moves on). Returns un-namespaced attribute keys ("id", "grants") per
// the AttributeProvider contract; the resolver namespaces them at merge
// time (resolver.go:436-452).
//
// Error codes:
//   - INVALID_ENTITY_ID: malformed subject shape (no colon, empty post-colon).
//   - INVALID_PLAYER_ID: non-ULID identifier under "player:" namespace.
func (p *PlayerAttributeProvider) ResolveSubject(_ context.Context, subjectID string) (map[string]any, error) {
    parts := strings.SplitN(subjectID, ":", 2)
    if len(parts) != 2 {
        return nil, oops.
            Code("INVALID_ENTITY_ID").
            With("entity_id", subjectID).
            Errorf("invalid subject ID format: expected 'type:id'")
    }

    entityType, idStr := parts[0], parts[1]
    if entityType != "player" {
        return nil, nil
    }
    if idStr == "" {
        return nil, oops.
            Code("INVALID_ENTITY_ID").
            With("entity_id", subjectID).
            Errorf("invalid subject ID format: empty ID under 'player:' namespace")
    }

    if _, err := ulid.Parse(idStr); err != nil {
        return nil, oops.
            Code("INVALID_PLAYER_ID").
            With("entity_id", subjectID).
            With("id_part", idStr).
            Wrapf(err, "invalid player ULID")
    }

    grants := []string{}
    if _, ok := p.operators[idStr]; ok {
        grants = []string{access.CapabilityCryptoOperator}
    }

    return map[string]any{
        "id":     idStr,
        "grants": grants,
    }, nil
}
```

(Note: importing `internal/access` from `internal/access/policy/attribute` is already established by `character.go:10`, so no new cycle.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run "TestPlayerProviderResolveSubject" ./internal/access/policy/attribute/`

Expected: PASS for all four tests (operator, non-operator, non-player namespace, malformed).

- [ ] **Step 5: Commit**

Suggested message:

```
feat(access): PlayerAttributeProvider.ResolveSubject (holomush-jxo8.5.6)

Resolves player.id and player.grants for "player:<ulid>" subjects.
Returns ["crypto.operator"] for IDs in the operator set; empty
non-nil slice otherwise. Returns nil for non-player namespaces (provider
declines). Validates ULID format with INVALID_PLAYER_ID; rejects malformed
shape with INVALID_ENTITY_ID.
```

---

### Task 7: `HasPlayerGrant` facade tests

**Files:**

- Modify: `internal/access/grants_test.go`

- [ ] **Step 1: Write failing tests**

In `internal/access/grants_test.go`, merge the new imports (`context`,
`errors`, `internal/access/policy/types`) into the existing import
block declared in Task 2 (single import block per file convention),
then append:

```go
// fakeResolver implements the SubjectResolver interface (single method)
// for HasPlayerGrant tests. *attribute.Resolver also satisfies
// SubjectResolver implicitly; tests that need full ABAC resolution
// (Task 9) construct a real one.
type fakeResolver struct {
    grants []string
    err    error
}

func (f *fakeResolver) ResolveSubjectAttributes(_ context.Context, _ string, _ string) (*types.AttributeBags, error) {
    if f.err != nil {
        return nil, f.err
    }
    bags := &types.AttributeBags{
        Subject: map[string]any{},
    }
    if f.grants != nil {
        bags.Subject["player.grants"] = f.grants
    }
    return bags, nil
}

// Compile-time assertion that fakeResolver satisfies SubjectResolver.
var _ SubjectResolver = (*fakeResolver)(nil)

func TestHasPlayerGrant_OperatorPermits(t *testing.T) {
    res := &fakeResolver{grants: []string{"crypto.operator"}}
    ok, err := HasPlayerGrant(context.Background(), res, "01HZAVGE83MGFEXQQH5SP9NXKF", CapabilityCryptoOperator)
    require.NoError(t, err)
    assert.True(t, ok)
}

func TestHasPlayerGrant_NonOperatorDenies(t *testing.T) {
    res := &fakeResolver{grants: []string{}}
    ok, err := HasPlayerGrant(context.Background(), res, "01HZAVGE83MGFEXQQH5SP9NXKF", CapabilityCryptoOperator)
    require.NoError(t, err)
    assert.False(t, ok)
}

func TestHasPlayerGrant_DifferentGrantNotMatched(t *testing.T) {
    res := &fakeResolver{grants: []string{"other.grant"}}
    ok, err := HasPlayerGrant(context.Background(), res, "01HZAVGE83MGFEXQQH5SP9NXKF", CapabilityCryptoOperator)
    require.NoError(t, err)
    assert.False(t, ok)
}

func TestHasPlayerGrant_GenericOverGrantName(t *testing.T) {
    res := &fakeResolver{grants: []string{"some.future.grant"}}
    ok, err := HasPlayerGrant(context.Background(), res, "01HZAVGE83MGFEXQQH5SP9NXKF", "some.future.grant")
    require.NoError(t, err)
    assert.True(t, ok, "facade must match any grant string by exact equality, not just CapabilityCryptoOperator")
}

func TestHasPlayerGrant_PropagatesResolverError(t *testing.T) {
    boom := errors.New("resolver boom")
    res := &fakeResolver{err: boom}
    ok, err := HasPlayerGrant(context.Background(), res, "01HZAVGE83MGFEXQQH5SP9NXKF", CapabilityCryptoOperator)
    require.Error(t, err)
    assert.False(t, ok)
    assert.ErrorIs(t, err, boom, "resolver error must propagate")
}

func TestHasPlayerGrant_RejectsEmptyPlayerID(t *testing.T) {
    res := &fakeResolver{grants: []string{"crypto.operator"}}
    ok, err := HasPlayerGrant(context.Background(), res, "", CapabilityCryptoOperator)
    require.Error(t, err)
    assert.False(t, ok)
    assert.Contains(t, err.Error(), "PLAYER_ID_EMPTY")
}

func TestHasPlayerGrant_RejectsEmptyGrant(t *testing.T) {
    res := &fakeResolver{grants: []string{"crypto.operator"}}
    ok, err := HasPlayerGrant(context.Background(), res, "01HZAVGE83MGFEXQQH5SP9NXKF", "")
    require.Error(t, err)
    assert.False(t, ok)
    assert.Contains(t, err.Error(), "GRANT_EMPTY")
}
```

(Add `"errors"` to the import block. Verify the actual `attribute.Resolver` / `*types.AttributeBags` signature — if the interface differs, adjust `fakeResolver` to match. Read `internal/access/policy/attribute/resolver.go` to confirm.)

- [ ] **Step 2: Run tests to verify they fail or pass selectively**

Run: `task test -- -run "TestHasPlayerGrant" ./internal/access/`

Expected: PASS for all 7 tests **if** the resolver interface signature matches the fake. If the test file compiles with errors due to interface mismatch, FIX `fakeResolver` to match the actual `attribute.Resolver` interface contract before proceeding.

- [ ] **Step 3: No production code change needed**

`HasPlayerGrant` was implemented in Task 2; tests now exercise it. If any test fails on logic (not interface), debug the facade implementation.

- [ ] **Step 4: Run all access-package tests to confirm green**

Run: `task test -- ./internal/access/...`

Expected: PASS.

- [ ] **Step 5: Commit**

Suggested message:

```
test(access): HasPlayerGrant facade unit tests (holomush-jxo8.5.7)

Covers operator-permit, non-operator-deny, exact-match semantics,
generic-over-grant-name, resolver-error propagation, empty-playerID and
empty-grant API-boundary rejection. INV-B3, INV-B4, INV-B8 satisfied.
```

---

### Task 8: PlayerAttributeProvider — concurrent reads + contract test + no-mutation meta-test

**Files:**

- Modify: `internal/access/policy/attribute/player_test.go`

- [ ] **Step 1: Write failing tests for concurrency, contract, and no-mutation**

Append to `internal/access/policy/attribute/player_test.go`:

```go
import (
    "reflect"
    // ... existing imports ...
)

func TestPlayerProviderContract(t *testing.T) {
    p := NewPlayerAttributeProvider(nil)
    assertProviderContract(t, p)  // existing helper from contract_test.go
}

func TestPlayerProviderConcurrentResolves(t *testing.T) {
    p := NewPlayerAttributeProvider([]string{
        testOperatorULID,
        "01HZAVGE83MGFEXQQH5SP9NXKH",
        "01HZAVGE83MGFEXQQH5SP9NXKJ",
    })

    cases := []struct {
        subject     string
        wantInList  bool
    }{
        {"player:" + testOperatorULID, true},
        {"player:01HZAVGE83MGFEXQQH5SP9NXKH", true},
        {"player:01HZAVGE83MGFEXQQH5SP9NXKJ", true},
        {"player:" + testNonOperatorULID, false},
    }
    for _, tc := range cases {
        tc := tc
        t.Run(tc.subject, func(t *testing.T) {
            t.Parallel()
            attrs, err := p.ResolveSubject(context.Background(), tc.subject)
            require.NoError(t, err)
            grants := attrs["grants"].([]string)
            if tc.wantInList {
                assert.Contains(t, grants, "crypto.operator")
            } else {
                assert.NotContains(t, grants, "crypto.operator")
            }
        })
    }
}

func TestPlayerProviderNoMutationAPI(t *testing.T) {
    typ := reflect.TypeOf(&PlayerAttributeProvider{})

    // Only the constructor (NewPlayerAttributeProvider) writes to the
    // operators field; no method on *PlayerAttributeProvider may mutate
    // it. Enumerate exported methods and assert none have a mutating
    // shape (no Set/Add/Remove/Reload/Clear-style names; non-pointer
    // receivers via type-name inspection would already prevent mutation).
    bannedPrefixes := []string{"Set", "Add", "Remove", "Reload", "Clear", "Update", "Insert", "Delete"}
    for i := 0; i < typ.NumMethod(); i++ {
        m := typ.Method(i)
        for _, banned := range bannedPrefixes {
            assert.False(t,
                strings.HasPrefix(m.Name, banned),
                "PlayerAttributeProvider must not expose mutator %q (INV-B6 — operator set is read-only post-construction)",
                m.Name,
            )
        }
    }
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `task test -race -- -run "TestPlayerProvider(Contract|Concurrent|NoMutation)" ./internal/access/policy/attribute/`

Expected: PASS. The race detector (`-race`) catches any latent unsynchronized writes to `p.operators` (there should be none).

- [ ] **Step 3: No production code change** — these tests confirm existing behavior.

- [ ] **Step 4: Run full provider test suite under race detector**

Run: `task test -race -- ./internal/access/policy/attribute/`

Expected: PASS, no data races.

- [ ] **Step 5: Commit**

Suggested message:

```
test(access): PlayerAttributeProvider contract + concurrency + no-mutation (holomush-jxo8.5.8)

Reuses existing assertProviderContract battery, adds parallel ResolveSubject
table-driven test (catches latent mutation under -race), and a reflection-
based meta-test enforcing INV-B6 (no exported mutator methods).
```

---

### Task 9: Resolver registration in `BuildABACStack`

**Files:**

- Modify: `internal/access/setup/setup.go`
- Create: `internal/access/setup/setup_player_test.go` (build-tag `//go:build integration`)

The actual setup entry point is `BuildABACStack(ctx context.Context, cfg
ABACConfig) (*ABACStack, error)` at `internal/access/setup/setup.go:71`.
`ABACConfig` is `{Pool, CharacterRepo, RoleStore, AuditMode}`. We add
`CryptoOperators []string` and register `PlayerAttributeProvider`
between the existing CharacterProvider and CommandProvider registrations.

- [ ] **Step 1: Write failing test for namespace registration**

In `internal/access/setup/setup_player_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package setup_test

import (
    "context"
    "testing"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/access"
    "github.com/holomush/holomush/internal/access/setup"
    "github.com/holomush/holomush/test/testutil"
)

func freshABACStack(t *testing.T, operators []string) (*setup.ABACStack, func()) {
    t.Helper()
    env := testutil.SharedPostgres(t)
    connStr := testutil.FreshDatabase(t, env)

    ctx := context.Background()
    pool, err := pgxpool.New(ctx, connStr)
    require.NoError(t, err)

    stack, err := setup.BuildABACStack(ctx, setup.ABACConfig{
        Pool:            pool,
        CharacterRepo:   nil, // optional per setup.go:96
        RoleStore:       nil,
        CryptoOperators: operators,
    })
    require.NoError(t, err)
    return stack, func() {
        _ = stack.Close()
        pool.Close()
    }
}

func TestPlayerProviderRegisteredWithResolver(t *testing.T) {
    operatorID := "01HZAVGE83MGFEXQQH5SP9NXKF"
    stack, cleanup := freshABACStack(t, []string{operatorID})
    defer cleanup()

    bags, err := stack.Resolver.ResolveSubjectAttributes(context.Background(),
        access.PlayerSubject(operatorID), "")
    require.NoError(t, err)
    assert.Equal(t, []string{"crypto.operator"}, bags.Subject["player.grants"])
    assert.Equal(t, operatorID, bags.Subject["player.id"])
}

func TestPlayerProviderEmptyOperators(t *testing.T) {
    stack, cleanup := freshABACStack(t, nil)
    defer cleanup()

    nonOpID := "01HZAVGE83MGFEXQQH5SP9NXKG"
    bags, err := stack.Resolver.ResolveSubjectAttributes(context.Background(),
        access.PlayerSubject(nonOpID), "")
    require.NoError(t, err)
    grants, ok := bags.Subject["player.grants"].([]string)
    require.True(t, ok)
    assert.Empty(t, grants)
}

func TestPlayerProviderNamespaceNonColliding(t *testing.T) {
    // Build ABAC twice with no operators; if PlayerAttributeProvider's
    // namespace collides with any existing namespace, the second
    // RegisterProvider call would error with "already registered".
    s1, c1 := freshABACStack(t, nil)
    defer c1()
    require.NotNil(t, s1.Resolver)

    s2, c2 := freshABACStack(t, []string{"01HZAVGE83MGFEXQQH5SP9NXKF"})
    defer c2()
    require.NotNil(t, s2.Resolver)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test:int -- -run "TestPlayerProvider(RegisteredWithResolver|EmptyOperators|NamespaceNonColliding)" ./internal/access/setup/`

Expected: FAIL — `ABACConfig` has no `CryptoOperators` field; the
`PlayerAttributeProvider` is not registered.

- [ ] **Step 3: Add `CryptoOperators` to `ABACConfig` and register the provider**

In `internal/access/setup/setup.go`, modify the `ABACConfig` struct
(currently lines 58-64):

```go
// ABACConfig holds configuration for building the ABAC stack.
type ABACConfig struct {
    Pool          *pgxpool.Pool
    CharacterRepo world.CharacterRepository
    RoleStore     store.RoleStore
    AuditMode     audit.Mode
    // CryptoOperators is the list of player IDs (ULIDs) holding the
    // crypto.operator capability. Passed to PlayerAttributeProvider
    // at construction. Empty / nil → no operators (break-glass disabled).
    CryptoOperators []string
}
```

Then in `BuildABACStack`, after the CharacterProvider block (currently
lines 95-105) and before the CommandProvider block (currently line 108),
add:

```go
    // 8a. Player provider (subject namespace alongside character;
    //     resolves player.id and player.grants for "player:<ulid>"
    //     subjects). Sub-epic B.
    playerProvider := attribute.NewPlayerAttributeProvider(cfg.CryptoOperators)
    if err := resolver.RegisterProvider(playerProvider); err != nil {
        return nil, eb.Wrapf(err, "register player provider")
    }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test:int -- -run "TestPlayerProvider(RegisteredWithResolver|EmptyOperators|NamespaceNonColliding)" ./internal/access/setup/`

Expected: PASS.

- [ ] **Step 5: Commit**

Suggested message:

```
feat(access): wire PlayerAttributeProvider into setup (holomush-jxo8.5.9)

Registers PlayerAttributeProvider with the AttributeResolver alongside
CharacterProvider et al. Setup deps gain a CryptoOperators []string field
that callers populate from CryptoConfig.Operators.
```

---

### Task 10: Server startup — load `CryptoConfig`, run cross-check, build provider

**Files:**

- Modify: `cmd/holomush/core.go`
- Create: `cmd/holomush/crypto_operator_validation_test.go` (build-tag `//go:build integration`)

- [ ] **Step 1: Write failing integration tests**

`test/testutil/postgres.go` exposes `SharedPostgres(t)` and
`FreshDatabase(t, env)`. `FreshDatabase` returns a migrated database
by cloning a pre-migrated template (`ensureTemplate` at lines 262-309
of `test/testutil/postgres.go`), so no separate migration call is
needed. There is no `SetupPostgres` or `SeedPlayer` helper — seed
players inline via SQL.

In `cmd/holomush/crypto_operator_validation_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
    "bytes"
    "context"
    "log/slog"
    "testing"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/test/testutil"
)

// freshPool returns a *pgxpool.Pool bound to a fresh test database
// from the shared Postgres container. testutil.FreshDatabase already
// templates a pre-migrated schema (see test/testutil/postgres.go's
// ensureTemplate, lines 262-309), so no separate migration call is
// needed. Caller's t.Cleanup closes the pool.
func freshPool(t *testing.T) *pgxpool.Pool {
    t.Helper()
    env := testutil.SharedPostgres(t)
    connStr := testutil.FreshDatabase(t, env)

    ctx := context.Background()
    pool, err := pgxpool.New(ctx, connStr)
    require.NoError(t, err)
    t.Cleanup(pool.Close)
    return pool
}

// seedPlayer inserts a minimal player row and returns the ULID.
func seedPlayer(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
    t.Helper()
    id := ulid.Make().String()
    _, err := pool.Exec(ctx,
        `INSERT INTO players (id, username, password_hash, created_at, updated_at)
         VALUES ($1, $2, $3, $4, $4)`,
        id, "test_player_"+id[:8], "hash", time.Now().UTC().Truncate(time.Microsecond))
    require.NoError(t, err)
    return id
}

func TestCryptoOperatorValidation_AllKnown(t *testing.T) {
    ctx := context.Background()
    pool := freshPool(t)
    p1 := seedPlayer(t, ctx, pool)
    p2 := seedPlayer(t, ctx, pool)

    var logBuf bytes.Buffer
    logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

    set, err := validateCryptoOperators(ctx, pool, []string{p1, p2}, logger)
    require.NoError(t, err)
    assert.Len(t, set, 2)
    assert.NotContains(t, logBuf.String(), "crypto.operator references unknown player")
}

func TestCryptoOperatorValidation_SomeUnknown(t *testing.T) {
    ctx := context.Background()
    pool := freshPool(t)
    p1 := seedPlayer(t, ctx, pool)
    unknown := ulid.Make().String()

    var logBuf bytes.Buffer
    logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

    set, err := validateCryptoOperators(ctx, pool, []string{p1, unknown}, logger)
    require.NoError(t, err)
    assert.Len(t, set, 2, "set keeps full configured list (lax+warn semantics)")
    assert.Contains(t, set, p1)
    assert.Contains(t, set, unknown)

    assert.Contains(t, logBuf.String(), "crypto.operator references unknown player")
    assert.Contains(t, logBuf.String(), unknown)
}

func TestCryptoOperatorValidation_AllUnknown(t *testing.T) {
    ctx := context.Background()
    pool := freshPool(t)
    u1 := ulid.Make().String()
    u2 := ulid.Make().String()

    var logBuf bytes.Buffer
    logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

    set, err := validateCryptoOperators(ctx, pool, []string{u1, u2}, logger)
    require.NoError(t, err, "lax+warn must NOT fail-closed even when all IDs unknown (INV-B5)")
    assert.Len(t, set, 2)
    output := logBuf.String()
    assert.Contains(t, output, u1)
    assert.Contains(t, output, u2)
}

func TestCryptoOperatorValidation_EmptyConfig(t *testing.T) {
    ctx := context.Background()
    pool := freshPool(t)

    var logBuf bytes.Buffer
    logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

    set, err := validateCryptoOperators(ctx, pool, nil, logger)
    require.NoError(t, err)
    assert.Empty(t, set)
    assert.Empty(t, logBuf.String(), "empty config must not query DB and must not warn")
}

func TestCryptoOperatorValidation_DuplicatesInConfig(t *testing.T) {
    ctx := context.Background()
    pool := freshPool(t)
    p1 := seedPlayer(t, ctx, pool)

    var logBuf bytes.Buffer
    logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

    set, err := validateCryptoOperators(ctx, pool, []string{p1, p1}, logger)
    require.NoError(t, err)
    assert.Len(t, set, 1, "duplicates must dedupe")
    assert.Empty(t, logBuf.String())
}

func TestCryptoOperatorValidation_QueryFails(t *testing.T) {
    ctx := context.Background()
    pool := freshPool(t)
    pool.Close() // force a query failure (closed pool)

    var logBuf bytes.Buffer
    logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

    operatorID := ulid.Make().String()
    set, err := validateCryptoOperators(context.Background(), pool, []string{operatorID}, logger)
    require.NoError(t, err, "query failure must NOT gate startup")
    assert.Contains(t, set, operatorID)
    assert.Contains(t, logBuf.String(), "crypto.operator validation skipped")
}
```

(Drop the `store` import — no longer used after removing the redundant
migration call. The original spec called for `store.NewMigrator` per a
defensive belt-and-suspenders pattern, but `test/testutil/postgres.go`
already provides migrated databases out of `FreshDatabase`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test:int -- -run "TestCryptoOperatorValidation" ./cmd/holomush/`

Expected: FAIL — `validateCryptoOperators` undefined.

- [ ] **Step 3: Implement `validateCryptoOperators`**

In `cmd/holomush/core.go` (or a sibling file like `cmd/holomush/crypto_operator_validation.go` if you prefer separating concerns), add:

```go
import (
    "context"
    "log/slog"

    "github.com/jackc/pgx/v5/pgxpool"

    "github.com/holomush/holomush/internal/auth/postgres"
)

// validateCryptoOperators cross-checks the configured operator list
// against the players table and emits structured warnings for IDs that
// don't correspond to any player. Returns the configured list as a set
// (deduplicated) regardless of cross-check outcome — validation is
// observability, not gating, per sub-epic B's INV-B5 / INV-B7.
//
// Empty / nil configured list → empty set, no DB query, no warnings.
// Query failure → warning, proceed with full configured set.
func validateCryptoOperators(ctx context.Context, pool *pgxpool.Pool, configured []string, logger *slog.Logger) (map[string]struct{}, error) {
    set := make(map[string]struct{}, len(configured))
    for _, id := range configured {
        if id == "" {
            continue
        }
        set[id] = struct{}{}
    }
    if len(set) == 0 {
        return set, nil
    }

    deduped := make([]string, 0, len(set))
    for id := range set {
        deduped = append(deduped, id)
    }

    repo := postgres.NewPlayerRepository(pool)
    found, err := repo.ExistingIDs(ctx, deduped)
    if err != nil {
        logger.Warn("crypto.operator validation skipped",
            "err", err,
            "configured_count", len(deduped))
        return set, nil
    }

    foundSet := make(map[string]struct{}, len(found))
    for _, id := range found {
        foundSet[id] = struct{}{}
    }
    for _, id := range deduped {
        if _, ok := foundSet[id]; !ok {
            logger.Warn("crypto.operator references unknown player", "player_id", id)
        }
    }
    return set, nil
}
```

Then thread the call through the existing core boot sequence: load `CryptoConfig` via `config.Load(configPath, cmd, &cryptoCfg, "crypto")`, call `validateCryptoOperators(ctx, pool, cryptoCfg.Operators, logger)`, pass the resulting set into `SetupDeps.CryptoOperators` (or whatever channel reaches the resolver setup from Task 9). Inspect the existing `runCore` / `runCoreWithDeps` shape to determine the exact insertion point.

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test:int -- -run "TestCryptoOperatorValidation" ./cmd/holomush/`

Expected: PASS for all six tests.

- [ ] **Step 5: Commit**

Suggested message:

```
feat(crypto): wire crypto.operators startup validation (holomush-jxo8.5.10)

Loads CryptoConfig.Operators at server start, cross-checks against the
players table, emits structured warnings per unknown ID. Lax+warn:
configured set is used as-is regardless of cross-check outcome; query
failure does not gate startup. INV-B5, INV-B7 satisfied.
```

---

### Task 11: Master-spec amendments (14 edits)

**Files:**

- Modify: `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`

This task applies the 14 amendments enumerated in the design spec's
[Master-spec amendments inventory](../specs/2026-05-08-event-payload-crypto-phase5-sub-epic-b-design.md#master-spec-amendments-inventory).
The meta-test (Task 12) enforces presence; this task supplies the edits.

**Application order: bottom-up.** Apply edits from highest line number
to lowest so insertions of new content (A2 paragraph, A3 / A6 / A8
subsections, A10 / A11 table rows) don't shift the line numbers of
not-yet-applied edits below them. Concrete order:

1. A13 (§11.3 line 2185)
2. A12 (§11.1 Phase 5 scope)
3. A11 (§10 operator-and-policy-errors table)
4. A10 (§10 bootstrap-time table)
5. A9 (§7.5)
6. A8 (new §6.3.1 subsection insert)
7. A7 (§6.3 1.4)
8. A6 (new §5.9.1 subsection insert)
9. A5 (§5.9 in-line edits)
10. A4 (§4.6 add policy_hash field to existing blocks)
11. A3 (§4.6 add new crypto.policy_set block)
12. A14 (§4.6 line 833)
13. A2 (§1 paragraph insert after threat-model table)
14. A1 (§1 row 137)

The numbered amendment IDs (A1–A14) below remain the canonical
identifiers used by the meta-test fingerprints — order of application
is a tactical choice; order in the table is documentation.

**Reading note:** Steps 1–14 below correspond to amendments A1–A14 in
the inventory (and thus in the spec). Apply them in the **bottom-up
order listed above** — start with A13 (highest line number, fewest
downstream insertions), end with A1 (lowest line number). Don't
confuse the inventory ordering (A1 first) with the application order
(A13 first).

- [ ] **Step 1: Apply A1 (§1 row 137 threat-model rewording)**

Edit line 137 of the master spec from:

```
| Compromised in-game wizard | Trigger destructive operations (Rekey, AdminReadStream) | Inside the auth tier but without shell access |
```

to:

```
| Compromised in-game admin with crypto.operator capability | Trigger destructive operations (Rekey, AdminReadStream) | Inside the auth tier but without shell access |
```

- [ ] **Step 2: Apply A2 (§1 threat-model layering note)**

Insert a new paragraph after the threat-model table (around line 145; check current line numbers since A1 shifts nothing). Text:

> **Threat-model layering for break-glass authentication.** Single-control
> break-glass authentication uses two factors (admin credentials + TOTP)
> against the row-134 adversary (curious operator with shell access);
> the localhost-UDS topology denies network reach for the row-137
> adversary (auth-tier compromised admin without shell access),
> reducing row-137's authentication surface to the same two factors
> once reach is achieved. Dual-control is the third-factor defense per
> §5.9 line 1279 (a different operator's credentials + TOTP, mediated
> by the `admin_approvals` token).

- [ ] **Step 3: Apply A3 (§4.6 add `crypto.policy_set` audit-event shape)**

In §4.6, after the existing `audit.<game>.system.rekey.*` block, insert:

```
**`audit.<game>.system.crypto_policy.<policy_name>`** — emitted on every
crypto-policy change (server startup writes the current effective policy;
future reload paths emit on each change):

```yaml
metadata:
  actor:        {kind: system, server_identity: <server-id>}
  event_type:   "crypto.policy_set"
  timestamp:    <now>
payload (cleartext):
  policy_hash:        <bytes>           # SHA-256 over RFC 8785 JCS-canonicalized payload (excluding policy_hash)
  prev_hash:          <bytes nullable>  # null only at genesis (first policy_set for this policy_name)
  server_start_ulid:  <ULID>
  policy_snapshot:    <json>            # the full effective policy
  server_identity:    <string>
```

Hash algorithm pinned to **SHA-256 over RFC 8785 JCS-canonicalized JSON**
of the payload **excluding the `policy_hash` field**. Sub-epic D MUST
select an RFC-8785-compliant Go canonicalizer (e.g.,
`github.com/cyberphone/json-canonicalization`) and pin the version in
`go.mod`. Switching canonicalizer libraries or RFC interpretations is a
chain-breaking change and MUST be treated as a master-spec amendment, not
a sub-epic-internal refactor.
```

- [ ] **Step 4: Apply A4 (§4.6 `policy_hash` field on rekey/operator_read events)**

In §4.6, add a `policy_hash: <bytes>` field to the existing
`audit.<game>.system.rekey.*` payload block and the
`audit.<game>.system.operator_read.*` payload block (referencing the
active `policy_set` event at invocation time).

- [ ] **Step 5: Apply A5 (§5.9 `s/wizard/admin/` + step-4 hard-required TOTP + new step-5 capability check)**

In §5.9 lines 1264–1325:

- Replace `wizard credentials` → `admin credentials` (line 1264, 1278).
- Replace `the wizard role` → `the admin role` (line 1291, 1317).
- Rewrite step 4 from:
  > "If TOTP not enrolled: log a warning to the audit event payload (`totp_verified: false`); proceed only if config allows `require_totp: false` for the operation."
  to:
  > "Verify TOTP enrolled. **Hard-required:** if not enrolled, refuse with `DENY_NOT_ENROLLED` and direct the operator to the enrollment path. No config knob bypasses this. Verify TOTP code; on mismatch, refuse with `DENY_BAD_TOTP`."
- Insert a new step 5 (renumber existing step 5 to 6):
  > "5. Verify player holds the `crypto.operator` capability via `access.HasPlayerGrant(ctx, resolver, playerID, access.CapabilityCryptoOperator)`. On miss, refuse with `DENY_NOT_OPERATOR`. (Capability-storage mechanism: see §5.9.1.)"
- Renumbered step 6 reads: "Verify player holds the `admin` role."

- [ ] **Step 6: Apply A6 (new §5.9.1 subsection)**

After §5.9, add:

```
#### 5.9.1 `crypto.operator` capability — storage and grant mechanism

The `crypto.operator` capability is a player-attribute grant — a flat
narrowing flag held on a player ID, MUST be combined with `RoleAdmin` to
authorize break-glass operations. It is **not** a `command.Capability`
tuple (the `{Action, Resource, Scope}` shape used for command pre-flight
authorization).

**Storage in v1: config-file allow-list.**

```yaml
crypto:
  operators:
    - "01HZAVGE83MGFEXQQH5SP9NXKF"  # admin Alice
    - "01HZAVGE83MGFEXQQH5SP9NXKG"  # admin Bob
```

**Runtime exposure:** A `PlayerAttributeProvider`
(`internal/access/policy/attribute/player.go`) introduces `player:` as a
Subject namespace in ABAC. Schema: `player.id: AttrTypeString`,
`player.grants: AttrTypeStringList`. For player IDs in the configured
allow-list, the provider exposes
`bags.Subject["player.grants"] = ["crypto.operator"]`; otherwise the
list is empty (non-nil).

**Consumer surface:** `access.HasPlayerGrant(ctx, resolver, playerID,
access.CapabilityCryptoOperator)` — typed Go facade implemented in
`internal/access/grants.go`. The OperatorAuthProvider (sub-epic D)
invokes this facade as step 5 of its check sequence.

**Validation:** Lax+warn at startup. The server cross-checks each
configured player ID against the players table; unknown IDs trigger
structured warnings (`slog.Warn("crypto.operator references unknown
player", "player_id", <ulid>)`). The configured list is used as-is —
validation is observability, not gating. Query failure during validation
emits a `"crypto.operator validation skipped"` warning and proceeds with
the full configured set.

**Reload:** Restart-only in v1. Hot reload is a documented future seam.

**In-game grant UX:** Deferred to a P3 follow-up bead. Operators edit
the YAML config and restart the server in v1.
```

- [ ] **Step 7: Apply A7 (§6.3 1.4)**

Edit line 1433 from:

```
  1.4  RekeyService verifies wizard role + TOTP factor
```

to:

```
  1.4  RekeyService verifies admin role + crypto.operator capability + TOTP factor
```

- [ ] **Step 8: Apply A8 (new §6.3.1 subsection — dual-control protocol)**

After §6.3, add:

```
#### 6.3.1 Dual-control protocol

Server-issued approval-token mechanism (sub-epic D ships the implementation):

1. Primary operator runs `holomush crypto rekey ... --dual-control`.
2. Server creates a pending row in `admin_approvals`:
   - `request_id` (ULID, primary key)
   - `primary_player_id`
   - `op_kind` (e.g., `"rekey"`, `"admin_read_stream"`)
   - `op_args_hash` (SHA-256 over canonicalized invocation args)
   - `expires_at = now() + 5 min`
3. Server prints `request_id` to primary's terminal; primary
   communicates it out-of-band to the second operator.
4. Second operator runs `holomush admin approve <request_id>`.
5. Second operator authenticates via `OperatorAuthProvider`
   (admin credentials + TOTP + `RoleAdmin` + `crypto.operator`). MUST
   have a different `player_id` from primary AND MUST hold both
   `RoleAdmin` AND `crypto.operator`.
6. Server marks the row approved.
7. Primary's still-blocking CLI proceeds to Rekey / AdminReadStream
   execution.

Approval-token format: ULID. TTL: 5 minutes; expired rows MAY be left
until a periodic sweep (rows are tiny). `op_args_hash` binds the
approval to the primary's invocation args; mismatch on proceed → server
rejects with `DENY_APPROVAL_ARGS_MISMATCH`.
```

- [ ] **Step 9: Apply A9 (§7.5 `s/wizard/admin/` + xref to §6.3.1)**

In §7.5 lines 1682–1722:

- Replace `wizard credentials` → `admin credentials` (line 1703).
- Replace `the wizard role` → `the admin role + crypto.operator capability` (line 1707).
- Replace `wizard-creds + TOTP + dual-control` → `admin-creds + TOTP + dual-control` (line 1714).
- Add a paragraph at the end of §7.5 cross-referencing §6.3.1 for dual-control mechanics.

- [ ] **Step 10: Apply A10 (§10 Bootstrap-time table — policy_set chain verification row)**

In §10's Bootstrap-time table, add a row:

| Failure | Detection | Behavior |
| --- | --- | --- |
| `policy_set` chain verification failure on startup | `prev_hash` of latest `policy_set` for `policy_name` does not match the actual predecessor's `policy_hash` | Server refuses to start (consistent with INV-32 / INV-33 / INV-37 fail-closed pattern) |

- [ ] **Step 11: Apply A11 (§10 Operator-and-policy-errors table — three new rows)**

In §10's operator-and-policy-errors table, add three rows:

| Code | Reason |
| --- | --- |
| `DENY_DUAL_CONTROL_REQUIRED` | Server enforces site `dual_control_required` policy; rejects single-control invocations of the listed operations |
| `DENY_APPROVAL_ARGS_MISMATCH` | Primary's invocation args do not match the stored `op_args_hash` from the approval row |
| `DENY_POLICY_HASH_UNKNOWN` | Rekey / AdminReadStream invocation references a `policy_hash` not present in the `policy_set` chain |

- [ ] **Step 12: Apply A12 (§11.1 Phase 5 scope list extension)**

In §11.1's Phase 5 row scope list, append:

- `crypto.policy_set` audit-event emission (sub-epic D)
- `admin_approvals` table (sub-epic D)
- `player_totp` table (sub-epic A; merged)
- `crypto_rekey_checkpoints` table (sub-epic E)

- [ ] **Step 13: Apply A13 (§11.3 step 5 rewrite)**

In §11.3, edit step 5 line 2185 from:

```
5. Decide on TOTP enrollment for wizard accounts. Required for `Rekey` and
   `AdminReadStream` once enabled in config.
```

to:

```
5. Verify TOTP enrolled for admin accounts who hold the `crypto.operator` capability.
   Required for `Rekey` and `AdminReadStream`.
```

- [ ] **Step 14: Apply A14 (§4.6 line 833 actor metadata)**

Edit line 833 from:

```
  actor:        {kind: operator, os_user: <uid>, player_id: <wizard player_id>}
```

to:

```
  actor:        {kind: operator, os_user: <uid>, player_id: <admin player_id>}
```

- [ ] **Step 15: Run `task lint` and verify markdown fmt**

Run: `task lint`

If markdown linting flags any of the inserted blocks (e.g., table column alignment), apply `task fmt` to fix.

- [ ] **Step 16: Commit**

Suggested message:

```
docs(crypto): master-spec amendments for sub-epic B (holomush-jxo8.5.11)

Lands the 14 amendments per the sub-epic B design spec's amendments
inventory: s/wizard/admin/ throughout, hard-required TOTP semantic flip,
new §5.9.1 (crypto.operator capability mechanism), new §6.3.1 (dual-control
protocol), §4.6 audit-shape additions for crypto.policy_set + policy_hash,
§10 fail-closed and DENY-code rows, §11.1 scope additions, §11.3 step 5
rewrite, §4.6 line 833 actor-metadata correction.

Two of the amendments (A13, A14) also correct drift in the decomposition
spec's amendments table (already applied during brainstorm — verified by
spec_amendments_test.go meta-test in next commit).
```

---

### Task 12: Spec-amendments meta-test

**Files:**

- Create: `internal/access/spec_amendments_test.go`

- [ ] **Step 1: Write the meta-test**

In `internal/access/spec_amendments_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

import (
    "os"
    "path/filepath"
    "runtime"
    "strings"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// TestSpecAmendmentsLanded enforces INV-B-AMEND: every master-spec
// amendment listed in the sub-epic B design spec's "Master-spec
// amendments inventory" must leave a detectable fingerprint in the
// master spec text. Catches "code without amendments" and
// "amendments-with-drifted-text" failure modes.
func TestSpecAmendmentsLanded(t *testing.T) {
    masterSpec := readSpec(t,
        "docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md")

    // Each fingerprint is a distinctive substring that MUST appear
    // post-amendment. Keyed by amendment ID for diagnostics.
    fingerprints := map[string]string{
        "A1":  "Compromised in-game admin with crypto.operator capability",
        "A2":  "Single-control break-glass authentication uses two factors",
        "A3":  "audit.<game>.system.crypto_policy",
        "A4":  "policy_hash:        <bytes>",
        "A5":  "DENY_NOT_ENROLLED",
        "A6":  "5.9.1 `crypto.operator` capability",
        "A7":  "admin role + crypto.operator capability + TOTP factor",
        "A8":  "6.3.1 Dual-control protocol",
        "A9":  "admin-creds + TOTP + dual-control",
        "A10": "policy_set chain verification failure on startup",
        "A11": "DENY_DUAL_CONTROL_REQUIRED",
        "A12": "admin_approvals` table (sub-epic D)",
        "A13": "admin accounts who hold the `crypto.operator` capability",
        "A14": "<admin player_id>",
    }

    // Forbidden substrings: pre-amendment text that MUST NOT remain.
    forbiddenAfterAmendment := map[string]string{
        "A1-stale":  "Compromised in-game wizard ",  // trailing space avoids matching new text
        "A13-stale": "Decide on TOTP enrollment for wizard accounts",
        "A14-stale": "<wizard player_id>",
    }

    for id, fp := range fingerprints {
        assert.Contains(t, masterSpec, fp,
            "INV-B-AMEND: amendment %s fingerprint missing from master spec", id)
    }
    for id, fp := range forbiddenAfterAmendment {
        assert.NotContains(t, masterSpec, fp,
            "INV-B-AMEND: pre-amendment text %s still present in master spec", id)
    }
}

// TestDecompositionSpecDriftFixesLanded enforces that B's PR also
// carries the drift-fix amendments to the decomposition spec table
// (§11.3 step 5 row + §4.6 line 833 row).
func TestDecompositionSpecDriftFixesLanded(t *testing.T) {
    decomp := readSpec(t,
        "docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-decomposition.md")

    assert.Contains(t, decomp, "§11.3 step 5 (line 2185)",
        "decomposition spec must point A13 at §11.3, not §12")
    assert.Contains(t, decomp, "§4.6 line 833",
        "decomposition spec must include the §4.6 line 833 amendment row")
    assert.NotContains(t, decomp,
        "Strike \"Decide on TOTP enrollment for wizard accounts\"",
        "decomposition spec must not retain the misattributed §12 strike text")
}

// readSpec resolves the path relative to repo root by walking up from
// the current test file location. Required because Go tests run with
// CWD set to the package directory, not the repo root.
func readSpec(t *testing.T, relPath string) string {
    t.Helper()
    _, thisFile, _, ok := runtime.Caller(0)
    require.True(t, ok, "could not determine caller location")
    repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
    fullPath := filepath.Join(repoRoot, relPath)
    bytes, err := os.ReadFile(fullPath)
    require.NoError(t, err, "could not read spec at %s", fullPath)
    return string(bytes)
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `task test -- -run "TestSpecAmendmentsLanded|TestDecompositionSpecDriftFixesLanded" ./internal/access/`

Expected: PASS — Task 11's amendments are in place; decomposition spec drift fixes were applied during the spec brainstorm.

If any fingerprint is missing, that's a Task-11 bug. Fix Task-11's amendment and re-run.

- [ ] **Step 3: No production code change.**

- [ ] **Step 4: Run full access-package test suite**

Run: `task test -- ./internal/access/`

Expected: PASS.

- [ ] **Step 5: Commit**

Suggested message:

```
test(crypto): TestSpecAmendmentsLanded meta-test (holomush-jxo8.5.12)

Enforces INV-B-AMEND: all 14 master-spec amendments + the two
decomposition-spec drift fixes leave detectable fingerprints. Catches
amendments-without-code and code-without-amendments at CI time.
```

---

### Task 13: Bead-description check script + `task pr-prep` wiring

**Files:**

- Create: `scripts/check_bead_jxo8_5.sh`
- Modify: `Taskfile.yaml` (or wherever `task pr-prep` is defined)

- [ ] **Step 1: Write the check script**

`bd show <id> --json` returns a JSON **array** with a single object
(verified empirically). The `.dependents[]` array carries
`{id, title, ..., dependency_type}` rows; `dependency_type: "blocks"`
captures the blocking edge. There is no `bd dep relate` subcommand and
no `--kind=related` flag (verified via `bd dep --help` and
`bd dep add --help`). The existing graph already has
`jxo8.5 blocks jxo8.6` from epic creation, so the gate verifies
description-only — no edge mutation needed.

In `scripts/check_bead_jxo8_5.sh`:

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# INV-B-BEAD: ensure jxo8.5's bead description matches sub-epic B's
# narrowed scope before this PR is pushed.

set -euo pipefail

BEAD_ID="holomush-jxo8.5"
DESCRIPTION_FINGERPRINT="capability + storage + facade + amendments"
STALE_FRAGMENT="OperatorAuthProvider check sequence (creds → TOTP → RoleAdmin → crypto.operator)"

if ! command -v bd >/dev/null 2>&1; then
    echo "bd CLI not found; skipping bead-description check (best-effort)"
    exit 0
fi
if ! command -v jq >/dev/null 2>&1; then
    echo "ERROR: jq required for bead-description check" >&2
    exit 1
fi

# bd show returns a JSON array; the bead is at index 0.
DESC=$(bd show "$BEAD_ID" --json 2>/dev/null | jq -r '.[0].description // ""')
if [ -z "$DESC" ]; then
    echo "ERROR: could not read description for $BEAD_ID" >&2
    exit 1
fi

if ! grep -qF "$DESCRIPTION_FINGERPRINT" <<<"$DESC"; then
    echo "ERROR: $BEAD_ID description must contain fingerprint:" >&2
    echo "  '$DESCRIPTION_FINGERPRINT'" >&2
    echo "Update via: bd update $BEAD_ID --description=...  (see Task 14)" >&2
    exit 1
fi

if grep -qF "$STALE_FRAGMENT" <<<"$DESC"; then
    echo "ERROR: $BEAD_ID description still contains stale text:" >&2
    echo "  '$STALE_FRAGMENT'" >&2
    echo "Sub-epic B does not own this; that's sub-epic D's scope." >&2
    exit 1
fi

# Sanity: jxo8.5 already blocks jxo8.6 by epic-creation time. Verify the
# edge survives, in case someone removes it.
if ! bd show "$BEAD_ID" --json 2>/dev/null | \
        jq -e '.[0].dependents[]? | select(.id == "holomush-jxo8.6" and .dependency_type == "blocks")' \
        >/dev/null; then
    echo "ERROR: $BEAD_ID must block holomush-jxo8.6 (sub-epic D consumes B's facade)" >&2
    exit 1
fi

echo "OK: $BEAD_ID description and blocks-edge match sub-epic B's narrowed scope"
```

Make executable: `chmod +x scripts/check_bead_jxo8_5.sh`.

- [ ] **Step 2: Wire into `task pr-prep:run`**

The actual `pr-prep` task in `Taskfile.yaml` is a thin per-machine lock
wrapper (line 522) that delegates the real work to `pr-prep:run` (line
559). Insert the bead-check **early** in `pr-prep:run`'s `cmds` —
before the long-running test phases — so a stale bead fails fast. The
existing block runs (in order): preamble, bats, schema check, license,
plugin build, lint, fmt:check, unit tests, integration tests. Insert
the bead check after the schema check (line 583) and before the license
check (line 584):

```yaml
      - echo "▸ Verifying jxo8.5 bead description (sub-epic B)..."
      - bash scripts/check_bead_jxo8_5.sh
      - echo "▸ Checking license headers..."
      - task: license:check
```

(File extension is `.yaml`, not `.yml` — verified.)

- [ ] **Step 3: Test the script manually**

Run: `bash scripts/check_bead_jxo8_5.sh`

Expected output: a non-zero exit because the bead description hasn't been updated yet (Task 14 does that). This confirms the gate works.

- [ ] **Step 4: Defer green verification to Task 14**

The script's pass/fail outcome depends on Task 14's bead update. Don't expect green here.

- [ ] **Step 5: Commit**

Suggested message:

```
chore(crypto): bead-description gate for sub-epic B (holomush-jxo8.5.13)

scripts/check_bead_jxo8_5.sh enforces INV-B-BEAD: jxo8.5's description
contains the narrow-scope fingerprint and a RELATED edge to jxo8.6.
Wired into task pr-prep so PR pushes fail-closed if the bead is stale.
```

---

### Task 14: Update bead `holomush-jxo8.5` description

**Files:**

- (External: bd issue tracker)

- [ ] **Step 1: Read current description**

Run: `bd show holomush-jxo8.5`

Note the current description (which lists the broader OperatorAuthProvider check-sequence scope).

- [ ] **Step 2: Update description**

`bd update` accepts `--body-file -` to read the description from stdin
(verified via `bd update --help`). Run:

```bash
bd update holomush-jxo8.5 --body-file - <<'EOF'
**Goal:** Ship the crypto.operator capability — a player-attribute grant exposed via ABAC, plus the typed Go facade D consumes, plus the top-level crypto: YAML config block, plus 14 master-spec amendments and a stub operator doc. **Scope: capability + storage + facade + amendments.** Sub-epic D (jxo8.6) wires the OperatorAuthProvider auth sequence; B does not.

**Design reference:** docs/superpowers/specs/2026-05-08-event-payload-crypto-phase5-sub-epic-b-design.md

**Plan reference:** docs/superpowers/plans/2026-05-08-event-payload-crypto-phase5-sub-epic-b.md

**TDD acceptance criteria:** PlayerAttributeProvider exposes player.id + player.grants; access.HasPlayerGrant facade; crypto.operators YAML config with lax+warn validation; 14 master-spec amendments land via TestSpecAmendmentsLanded meta-test; bead-description fingerprint check via scripts/check_bead_jxo8_5.sh.

**Verification steps:** task lint; task test; task test:int; task pr-prep.

**Files touched:** internal/access/grants.go (new); internal/access/policy/attribute/player.go (new); internal/access/prefix.go (modified — SubjectPlayer constant); internal/config/config.go (CryptoConfig); internal/auth/postgres/player_repo.go (ExistingIDs helper); internal/access/setup/setup.go (provider registration); cmd/holomush/core.go (validation wiring); docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md (14 amendments).

**Dependencies:** None (Tier 1, parallel-able with A and C). Blocks sub-epic D (jxo8.6) which consumes the capability via access.HasPlayerGrant.

**Out of scope:** OperatorAuthProvider check sequence (D); admin_approvals + dual-control (D); crypto.policy_set audit emission (D); ABAC seed policy that uses the grant (documented future seam); UDS substrate (C); TOTP (A, merged); in-game grant UX (P3 follow-up bead, filed at landing).
EOF
```

- [ ] **Step 3: Verify the blocks-edge to `holomush-jxo8.6` already exists**

Run: `bd show holomush-jxo8.5 --json | jq '.[0].dependents[] | select(.id == "holomush-jxo8.6")'`

Expected output: a JSON object with `dependency_type: "blocks"`. This
edge was created at epic-creation time; the gate script just verifies
it survives. If for any reason it's missing, restore via:

```bash
bd dep add holomush-jxo8.6 holomush-jxo8.5
# This makes jxo8.6 depend on (be blocked by) jxo8.5, equivalent to
# "jxo8.5 blocks jxo8.6".
```

(Per `bd dep add --help`: `bd dep add <blocked> <blocker>`.)

- [ ] **Step 4: Verify the gate passes**

Run: `bash scripts/check_bead_jxo8_5.sh`

Expected: `OK: holomush-jxo8.5 description and blocks-edge match sub-epic B's narrowed scope`.

- [ ] **Step 5: Commit (no code change; bead update is recorded by `bd dolt push` separately)**

This task has no code commit. Run `bd dolt push` near end of the work session per CLAUDE.md "Landing the Plane" / `bd prime`.

---

### Task 15: Site-doc stub — `site/docs/operating/crypto-setup.md`

**Files:**

- Create: `site/docs/operating/crypto-setup.md`

- [ ] **Step 1: Write a failing presence check (optional — meta-discipline)**

The doc-presence is implicitly enforced by reviewers reading the PR; an explicit test is overkill for a markdown file. Skip the test scaffold; jump straight to writing.

- [ ] **Step 2: Create the doc**

In `site/docs/operating/crypto-setup.md`:

```markdown
<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Crypto setup

This page is the operator entry point for HoloMUSH's event-payload
cryptography setup. It is currently a stub focused on the operator
allow-list (Phase 5 sub-epic B). Phase 8 expands this page with the full
master-key bootstrap runbook.

## Operator allow-list (`crypto.operator` capability)

Break-glass crypto operations (`Rekey`, `AdminReadStream`) require an
operator to hold both the `RoleAdmin` role AND the `crypto.operator`
capability. The capability is the narrowing grant that limits break-glass
to a specific cohort of admins.

### YAML configuration

The operator allow-list lives in the top-level `crypto:` block of the
HoloMUSH server config:

```yaml
crypto:
  operators:
    - "01HZAVGE83MGFEXQQH5SP9NXKF"  # admin Alice
    - "01HZAVGE83MGFEXQQH5SP9NXKG"  # admin Bob
```

Each entry MUST be a player ULID. Comments after `#` are recommended
for human readability.

### Finding a player's ULID

Query the players table directly:

```sql
SELECT id FROM players WHERE username = 'alice';
```

Or via the `holomush admin player show <username>` command if available.

### Validation behavior at startup

The server cross-checks each configured player ID against the players
table once at startup:

- Unknown IDs trigger a structured warning:
  `crypto.operator references unknown player`. The configured list is
  used as-is regardless — validation is observability, not gating.
- Query failures (PG transient errors) produce a
  `crypto.operator validation skipped` warning and the server proceeds
  with the full configured set.
- Empty / missing `crypto.operators` → no operators → break-glass is
  effectively disabled.

This deliberately fail-open posture means typos in the config produce
warnings, not startup failures. Operators can recover by editing the
config and restarting.

### Reload

Restart-only in v1. To grant or revoke `crypto.operator` for a player,
edit the YAML file and restart the server. Hot reload is a future
enhancement; see the sub-epic B design spec for the documented seam.

### In-game grant UX

Deferred to a future P3 follow-up bead. For now, all changes go through
the YAML config file.

## See also

- [Master spec — Section 5.9.1](../../../docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md): canonical definition of the `crypto.operator` capability.
- [Sub-epic B design spec](../../../docs/superpowers/specs/2026-05-08-event-payload-crypto-phase5-sub-epic-b-design.md): full design of the capability mechanism.
```

- [ ] **Step 3: Verify the file lints**

Run: `task docs:build` (or the local docs preview task) and confirm no broken-link warnings or YAML frontmatter errors. If the docs system uses `mkdocs.yml` or zensical config, check whether `crypto-setup.md` needs to be added to the sidebar / nav.

- [ ] **Step 4: Inspect rendered output**

Run: `task docs:serve` and visit the local URL for the operating section. Confirm the page renders and links resolve.

- [ ] **Step 5: Commit**

Suggested message:

```
docs(operating): crypto-setup.md stub (holomush-jxo8.5.15)

Minimal operator-facing doc for the crypto.operators YAML knob shipped
in sub-epic B. Master spec §9.2 marks crypto-setup.md as Phase-8 work;
this stub lands the config knob doc together with the code knob, and
Phase 8 expands the file with the full master-key bootstrap runbook.
```

---

### Task 16: Final verification — full PR-prep gate

**Files:**

- (None — verification only.)

- [ ] **Step 1: Run full lint gate**

Run: `task lint`

Expected: PASS, zero warnings.

- [ ] **Step 2: Run full unit-test gate**

Run: `task test`

Expected: PASS, no failures.

- [ ] **Step 3: Run full integration-test gate**

Run: `task test:int`

Expected: PASS, no failures.

- [ ] **Step 4: Run `task pr-prep` (mirrors CI)**

Run: `task pr-prep`

Expected: PASS for every sub-task. The bead-description gate (Task 13's
`scripts/check_bead_jxo8_5.sh`) runs as part of this — it MUST pass.
If any other CI mirror task fails, fix before proceeding to Task 17.

- [ ] **Step 5: Verify code search returns no leftover `cmd_crypto_operator_validation_test`**

Run: `rg "cmd_crypto_operator_validation_test"` (should return zero
matches; the file is `crypto_operator_validation_test.go` per NB6).

Verify no `<wizard player_id>` remains in the master spec:
`rg "<wizard player_id>" docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`
(should return zero matches).

---

### Task 17: Pre-push reviews

**Files:**

- (None — adversarial review only.)

- [ ] **Step 1: Run `/review-crypto`**

Per CLAUDE.md, changes to `internal/access/` and master-spec
amendments touch the crypto domain. Invoke
`Agent` with `subagent_type: crypto-reviewer` (or run `/review-crypto`).
Expected verdict: READY.

If NOT READY, address findings inline and re-run.

- [ ] **Step 2: Run `/review-abac`**

Changes to `internal/access/` touch the ABAC surface. Invoke
`Agent` with `subagent_type: abac-reviewer` (or run `/review-abac`).
Expected verdict: READY.

If NOT READY, address findings inline and re-run.

- [ ] **Step 3: Run `/review-code`**

Per CLAUDE.md "Pre-Push Review Gates", `/review-code` runs after the
domain-specific reviewers. Invoke `Agent` with `subagent_type: code-reviewer`
(or run `/review-code`). Expected verdict: READY.

If NOT READY, address findings inline and re-run.

- [ ] **Step 4: Push the branch**

Per CLAUDE.md "Landing the Plane":

1. `jj git fetch`.
2. Targeted rebase: `jj rebase -r <change-id> -d main@origin` (NEVER bare `-d main`).
3. Set bookmark: `jj bookmark set phase5-sub-epic-b -r @`.
4. Push: `jj git push --branch phase5-sub-epic-b`.
5. Verify: `jj st`.

- [ ] **Step 5: Open PR**

Run: `gh pr create --title "feat(crypto): Phase 5 sub-epic B — crypto.operator capability (holomush-jxo8.5)" --body "$(cat <<'EOF'
## Summary

- New ABAC `player:` Subject namespace with `PlayerAttributeProvider` exposing `player.id` + `player.grants`.
- `access.HasPlayerGrant` facade for sub-epic D's `OperatorAuthProvider`.
- Top-level `crypto:` YAML config block; `crypto.operators` allow-list with lax+warn validation.
- 14 master-spec amendments + 2 decomposition-spec drift fixes (enforced by `TestSpecAmendmentsLanded`).
- Operator-doc stub at `site/docs/operating/crypto-setup.md` (Phase 8 expands).
- Bead-description gate via `scripts/check_bead_jxo8_5.sh` in `task pr-prep`.

## Out of scope

- `OperatorAuthProvider` check sequence (sub-epic D).
- Dual-control + `admin_approvals` table (D).
- `crypto.policy_set` audit emission (D).
- In-game grant UX (P3 follow-up bead).

## Test plan

- [x] `task lint`
- [x] `task test`
- [x] `task test:int`
- [x] `task pr-prep`
- [x] `/review-crypto` — READY
- [x] `/review-abac` — READY
- [x] `/review-code` — READY

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"`

Then run `/autofix <PR#>` to address any CodeRabbit feedback per project convention.

---

## Self-review

After writing the plan, this section captures the inline review pass:

**Spec coverage check:**

- INV-B1 / INV-B2 → Task 5+6 (ResolveSubject happy + non-operator paths).
- INV-B3 → Task 7 (`TestHasPlayerGrant_OperatorPermits` + `TestHasPlayerGrant_DifferentGrantNotMatched`).
- INV-B4 → Task 7 (`TestHasPlayerGrant_GenericOverGrantName`).
- INV-B5 → Task 10 (`TestCryptoOperatorValidation_AllUnknown`).
- INV-B6 → Task 8 (`TestPlayerProviderNoMutationAPI`).
- INV-B7 → Task 10 (`TestCryptoOperatorValidation_EmptyConfig`) + Task 6.
- INV-B8 → Task 7 (`TestHasPlayerGrant_RejectsEmptyPlayerID` + `TestHasPlayerGrant_RejectsEmptyGrant`).
- INV-B9 → Task 5 (`TestPlayerProviderResolveResourceAlwaysNil`).
- INV-B10 → Task 9 (`TestPlayerProviderNamespaceNonColliding`).
- INV-B-AMEND → Task 12.
- INV-B-BEAD → Tasks 13+14.

All 12 invariants have a covering task. ✓

**Spec deliverables coverage:**

- Deliverable 1 (`PlayerAttributeProvider`): Tasks 5+6+8.
- Deliverable 2 (constant + facade): Tasks 2+7.
- Deliverable 3 (`CryptoConfig` + validation): Tasks 4+10.
- Deliverable 4 (site-doc): Task 15.
- Master-spec amendments: Task 11.
- Foundational subject prefix: Task 1.
- Repo bulk-existence helper: Task 3.
- Resolver registration: Task 9.
- Meta-tests + gate: Tasks 12+13+14.
- Verification + reviews + push: Tasks 16+17.

All deliverables covered. ✓

**Type / signature consistency:**

- `PlayerAttributeProvider` constructor: `NewPlayerAttributeProvider([]string) *PlayerAttributeProvider` — used consistently in Tasks 5, 6, 8, 9.
- `HasPlayerGrant(ctx context.Context, resolver attribute.Resolver, playerID, grant string) (bool, error)` — Task 2 defines, Task 7 tests, Task 11 spec amendment references.
- `CapabilityCryptoOperator = "crypto.operator"` — Task 2 defines, Task 7 + spec amendments use.
- `CryptoConfig.Operators []string` — Task 4 defines, Task 10 consumes.
- `validateCryptoOperators(ctx, pool, configured, logger) (map[string]struct{}, error)` — Task 10 defines, no other caller.
- `PlayerRepository.ExistingIDs(ctx, ids []string) ([]string, error)` — Task 3 defines, Task 10 consumes.

All types consistent. ✓

**Placeholder scan:**

- No "TBD" / "TODO" tokens.
- A few caveats like "if X helper exists, otherwise inline" — these are deliberate calibrations to existing-code uncertainty, not dropped requirements. Each is paired with a specific fallback action.

No placeholders. ✓

---

## Out of scope (re-stated for plan readers)

This plan does NOT cover:

- **`OperatorAuthProvider`** — sub-epic D (`holomush-jxo8.6`) consumes B's facade.
- **`admin_approvals` + dual-control** — D.
- **`crypto.policy_set` audit emission + chain verification** — D.
- **`Rekey` lifecycle, CLI, or `crypto_rekey_checkpoints` table** — sub-epic E.
- **`AdminReadStream` server / CLI** — sub-epic F.
- **UDS / ConnectRPC plumbing** — sub-epic C.
- **TOTP enrollment, verification, audit** — sub-epic A (merged in PR #3535).
- **In-game `holomush admin grant crypto.operator <player>` command** — P3 follow-up bead, filed at sub-epic B PR landing.
- **Hot reload of `crypto.operators`** — documented future seam in B's design spec; not built.
- **Policy-driven gate (ABAC seed referencing `principal.grants`)** — documented future seam; not built. Migration path requires DSL grammar work or synthetic resource invention; left to a follow-up brainstorm.

---

## Bead chain structure

```text
holomush-jxo8.5                  (existing epic — sub-epic B: crypto.operator capability)
├── holomush-jxo8.5.1            (NEW — Foundations: prefix + capability + facade + repo helper + config)
├── holomush-jxo8.5.2            (NEW — PlayerAttributeProvider implementation)
│   • Depends on jxo8.5.1
├── holomush-jxo8.5.3            (NEW — Setup wiring + lax+warn startup validation)
│   • Depends on jxo8.5.1, jxo8.5.2
└── holomush-jxo8.5.4            (NEW — Master-spec amendments + meta-tests + operator-doc stub)
    • Independent of code beads (doc/governance)
```

### Per-bead `bd create` blocks

```bash
bd create \
  --title "Phase 5 sub-epic B: foundations — prefix + capability + facade + repo + config" \
  --type task --priority 2 --parent holomush-jxo8.5 \
  --body-file - <<'EOF'
**Goal:** Land sub-epic B's foundational primitives — `SubjectPlayer` Subject prefix, `CapabilityCryptoOperator` constant, `PlayerSubject` helper, `SubjectResolver` interface, `HasPlayerGrant` facade, `PlayerRepository.ExistingIDs` bulk-existence helper, and `CryptoConfig` YAML block.

**Design reference:** docs/superpowers/specs/2026-05-08-event-payload-crypto-phase5-sub-epic-b-design.md §"Deliverable 2: Capability constant + helpers" + §"Deliverable 3: crypto: YAML config block"

**Plan reference:** docs/superpowers/plans/2026-05-08-event-payload-crypto-phase5-sub-epic-b.md §Tasks 1, 2, 3, 4, 7

**TDD acceptance criteria:**
- TestSubjectPlayerConstant
- TestParseEntityRefAcceptsPlayerNamespace
- TestParseEntityRefRejectsEmptyPlayerID
- TestCapabilityCryptoOperatorIsCryptoOperator
- TestPlayerSubject + TestPlayerSubjectPanicsOnEmpty
- TestPlayerRepository_ExistingIDs + TestPlayerRepository_ExistingIDs_EmptyInput
- TestDefaultCryptoConfigIsEmpty + TestLoadParsesCryptoOperators + TestLoadCryptoMissingSectionIsEmpty + TestLoadCryptoOperatorsEmptyListIsEmpty + TestLoadCryptoOperatorsMalformedFails
- TestHasPlayerGrant_OperatorPermits + _NonOperatorDenies + _DifferentGrantNotMatched (INV-B3)
- TestHasPlayerGrant_GenericOverGrantName (INV-B4)
- TestHasPlayerGrant_PropagatesResolverError
- TestHasPlayerGrant_RejectsEmptyPlayerID + TestHasPlayerGrant_RejectsEmptyGrant (INV-B8)

**Verification steps:**
- task lint
- task test -- ./internal/access/ ./internal/config/
- task test:int -- ./internal/auth/postgres/

**Files touched:**
- internal/access/prefix.go — add SubjectPlayer + knownPrefixes
- internal/access/prefix_test.go — extended
- internal/access/grants.go (new) — CapabilityCryptoOperator, SubjectResolver, PlayerSubject, HasPlayerGrant
- internal/access/grants_test.go (new) — facade unit tests
- internal/config/config.go — CryptoConfig + DefaultCryptoConfig
- internal/config/config_test.go — extended
- internal/auth/postgres/player_repo.go — ExistingIDs helper
- internal/auth/postgres/player_repo_test.go — extended

**Dependencies:** None (foundational).

**Out of scope:** PlayerAttributeProvider (jxo8.5.2). Resolver registration / startup validation (jxo8.5.3). Master-spec amendments / site doc (jxo8.5.4).
EOF
```

```bash
bd create \
  --title "Phase 5 sub-epic B: PlayerAttributeProvider implementation" \
  --type task --priority 2 --parent holomush-jxo8.5 \
  --body-file - <<'EOF'
**Goal:** Implement `PlayerAttributeProvider` — introduces the `player:` Subject namespace in ABAC with schema `{player.id: AttrTypeString, player.grants: AttrTypeStringList}`. Operator set is captured at construction and read-only thereafter.

**Design reference:** docs/superpowers/specs/2026-05-08-event-payload-crypto-phase5-sub-epic-b-design.md §"Deliverable 1: PlayerAttributeProvider"

**Plan reference:** docs/superpowers/plans/2026-05-08-event-payload-crypto-phase5-sub-epic-b.md §Tasks 5, 6, 8

**TDD acceptance criteria:**
- TestPlayerProviderNamespace + TestPlayerProviderSchema
- TestPlayerProviderResolveResourceAlwaysNil (INV-B9)
- TestPlayerProviderConstructorDeduplicates + TestPlayerProviderConstructorEmptyInput
- TestPlayerProviderResolveSubjectOperator (INV-B1)
- TestPlayerProviderResolveSubjectNonOperator (INV-B2, INV-B7)
- TestPlayerProviderResolveSubjectNonPlayerNamespace
- TestPlayerProviderResolveSubjectMalformedSubject — table-driven, codes INVALID_ENTITY_ID + INVALID_PLAYER_ID
- TestPlayerProviderContract — reuses `assertProviderContract`
- TestPlayerProviderConcurrentResolves — t.Parallel under `-race`
- TestPlayerProviderNoMutationAPI (INV-B6) — reflection-based meta-test

**Verification steps:**
- task lint
- task test -race -- ./internal/access/policy/attribute/

**Files touched:**
- internal/access/policy/attribute/player.go (new)
- internal/access/policy/attribute/player_test.go (new)

**Dependencies:** jxo8.5.1 (uses `access.CapabilityCryptoOperator` constant).

**Out of scope:** Setup wiring (jxo8.5.3). ABAC seed policy referencing `principal.grants` — documented future seam, not built.
EOF
```

```bash
bd create \
  --title "Phase 5 sub-epic B: setup wiring + lax+warn startup validation" \
  --type task --priority 2 --parent holomush-jxo8.5 \
  --body-file - <<'EOF'
**Goal:** Register `PlayerAttributeProvider` in `BuildABACStack` (extending `ABACConfig` with `CryptoOperators []string`); wire `validateCryptoOperators` into server startup; emit lax+warn diagnostics for unknown operator IDs and on PG transient failures.

**Design reference:** docs/superpowers/specs/2026-05-08-event-payload-crypto-phase5-sub-epic-b-design.md §"Construction-time flow" + §"Validation behavior at startup"

**Plan reference:** docs/superpowers/plans/2026-05-08-event-payload-crypto-phase5-sub-epic-b.md §Tasks 9, 10

**TDD acceptance criteria:**
- TestPlayerProviderRegisteredWithResolver
- TestPlayerProviderEmptyOperators
- TestPlayerProviderNamespaceNonColliding (INV-B10)
- TestCryptoOperatorValidation_AllKnown
- TestCryptoOperatorValidation_SomeUnknown
- TestCryptoOperatorValidation_AllUnknown (INV-B5)
- TestCryptoOperatorValidation_EmptyConfig (INV-B7)
- TestCryptoOperatorValidation_DuplicatesInConfig
- TestCryptoOperatorValidation_QueryFails

**Verification steps:**
- task lint
- task test:int -- ./internal/access/setup/ ./cmd/holomush/

**Files touched:**
- internal/access/setup/setup.go — `ABACConfig.CryptoOperators` field; PlayerAttributeProvider registered in `BuildABACStack` after CharacterProvider
- internal/access/setup/setup_player_test.go (new, build-tag integration)
- cmd/holomush/core.go — load CryptoConfig, call validateCryptoOperators, thread operator set into ABACConfig
- cmd/holomush/crypto_operator_validation_test.go (new, build-tag integration)

**Dependencies:** jxo8.5.1 (uses `ExistingIDs`, `CryptoConfig`), jxo8.5.2 (registers PlayerAttributeProvider).

**Out of scope:** Hot-reload of operator list — documented future seam, not built. Master-spec amendments / site doc (jxo8.5.4).
EOF
```

```bash
bd create \
  --title "Phase 5 sub-epic B: master-spec amendments + meta-tests + operator-doc stub" \
  --type task --priority 2 --parent holomush-jxo8.5 \
  --body-file - <<'EOF'
**Goal:** Land 14 master-spec amendments (applied bottom-up to avoid line drift); install `TestSpecAmendmentsLanded` meta-test enforcing INV-B-AMEND; install `scripts/check_bead_jxo8_5.sh` bead-description gate wired into `task pr-prep:run`; update `holomush-jxo8.5`'s description to match B's narrow scope (INV-B-BEAD); create `site/docs/operating/crypto-setup.md` operator-doc stub.

**Design reference:** docs/superpowers/specs/2026-05-08-event-payload-crypto-phase5-sub-epic-b-design.md §"Master-spec amendments inventory" + §"Deliverable 4: Site-doc edit" + §"Invariants" rows INV-B-AMEND and INV-B-BEAD

**Plan reference:** docs/superpowers/plans/2026-05-08-event-payload-crypto-phase5-sub-epic-b.md §Tasks 11, 12, 13, 14, 15

**TDD acceptance criteria:**
- TestSpecAmendmentsLanded (INV-B-AMEND) — verifies 14 fingerprints present + 3 forbidden stale fragments absent
- TestDecompositionSpecDriftFixesLanded — verifies decomposition spec carries the §11.3 + §4.6 line 833 drift fixes
- scripts/check_bead_jxo8_5.sh exits 0 against the updated jxo8.5 description (INV-B-BEAD)

**Verification steps:**
- task lint
- task test -- ./internal/access/
- bash scripts/check_bead_jxo8_5.sh
- task docs:build (markdown render of crypto-setup.md)

**Files touched:**
- docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md — 14 amendments (bottom-up: A13, A12, A11, A10, A9, A8, A7, A6, A5, A4, A3, A14, A2, A1)
- docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-decomposition.md — drift-fix verification (already applied during spec brainstorm; meta-test asserts)
- internal/access/spec_amendments_test.go (new) — meta-test
- scripts/check_bead_jxo8_5.sh (new) — bead-description gate
- Taskfile.yaml — gate wired into pr-prep:run after schema-check, before license-check
- site/docs/operating/crypto-setup.md (new) — operator-doc stub
- (External: `bd update holomush-jxo8.5 --body-file -` updates description per Task 14)

**Dependencies:** None (doc/governance; can run parallel to jxo8.5.1/.2/.3).

**Out of scope:** Phase 8's master-spec §9.2 status flip from NEW → EXTEND for crypto-setup.md (deferred to the Phase 8 epic). In-game grant UX for crypto.operator (P3 follow-up bead, filed at landing).
EOF
```

### Closing-out operations

- **Update `holomush-jxo8.5` description** (executed as part of `jxo8.5.4`'s Task 14): narrow scope text per the design spec's "Bead-description note". Verified by INV-B-BEAD gate. The existing `blocks` edge to `holomush-jxo8.6` is preserved (verified by the gate too).
- No supersessions.

### `bd dep add` edges

```bash
bd dep add holomush-jxo8.5.2 holomush-jxo8.5.1   # provider depends on foundations
bd dep add holomush-jxo8.5.3 holomush-jxo8.5.1   # wiring depends on foundations
bd dep add holomush-jxo8.5.3 holomush-jxo8.5.2   # wiring depends on provider
# jxo8.5.4 has no code dependencies — runs parallel
```

## Follow-up beads (filed at landing)

- **In-game grant UX for `crypto.operator`** — P3, parent `holomush-jxo8.5`. Specifics: server-side admin command (`holomush admin grant crypto.operator <player>` and `revoke`) plus the underlying repo or config-mutation mechanism. Out-of-scope notes: requires deciding where the grant state lives (DB table vs config-file mutation vs hybrid).
- **Master spec §9.2 status flip for `crypto-setup.md`** — Phase 8 owns. Flag in the Phase 8 epic as a small bookkeeping amendment when Phase 8 is brainstormed.
