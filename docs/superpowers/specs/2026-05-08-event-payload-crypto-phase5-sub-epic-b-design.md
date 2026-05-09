<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Event Payload Cryptography — Phase 5 Sub-epic B Design (`crypto.operator` capability)

## Status

Draft.

## Authors

Sean Brandt; brainstormed with Claude.

## Date

2026-05-08

## Context

Sub-epic B of `holomush-jxo8` (Phase 5 of `holomush-e49r`, "Rekey CLI +
AdminReadStream + OperatorAuthProvider"). Decomposed in
[`docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-decomposition.md`](2026-05-07-event-payload-crypto-phase5-decomposition.md);
this document is sub-epic B's design spec, planned in parallel with sub-epics
A (TOTP — merged in PR #3535) and C (UDS substrate — pending).

Sub-epic B introduces the `crypto.operator` capability — a player-attribute
grant that, in combination with `RoleAdmin`, gates break-glass operations
(`Rekey`, `AdminReadStream`). The decomposition spec's Decision 2 settled
that:

- The grant is **player-attribute-shaped** (per-player, flat, MUST hold
  alongside `RoleAdmin`), not a `command.Capability` (`{Action, Resource,
  Scope}` tuple).
- Storage in v1 is a config-file allow-list. In-game grant UX is deferred
  to a P3 follow-up bead.
- The grant is the narrowing mechanism on top of `RoleAdmin`; the
  threat-model row "compromised in-game admin" amends to "compromised
  in-game admin **with `crypto.operator` capability**."

Sub-epic D (`OperatorAuthProvider` + `InGameCredentialsProvider` +
`admin_approvals` + dual-control + `crypto.policy_set` audit emission)
consumes B's deliverables as step 4 of its check sequence (creds → TOTP →
`RoleAdmin` → `crypto.operator`). B does NOT ship that wiring; D does.

### What this document is

A complete design for sub-epic B's four code artifacts and the 13
master-spec amendments that land in B's first PR.

### What this document is not

- A spec for sub-epic D's `OperatorAuthProvider`. D will reference §5.9.1
  (added by B) but will not require B to ship the auth-sequence wiring.
- A re-statement of master-spec semantics. Where master-spec text is
  authoritative, this document cites it by section.
- A spec for the in-game grant UX (P3 follow-up bead, filed at landing
  time).

## Scope

Sub-epic B ships four code artifacts and one set of doc edits:

1. **`PlayerAttributeProvider`** — a new `internal/access/policy/attribute/`
   provider that introduces `player:` as a Subject namespace in ABAC, with
   schema `{player.id: AttrTypeString, player.grants: AttrTypeStringList}`.
2. **Capability constant + helpers** — `access.CapabilityCryptoOperator =
   "crypto.operator"`, `access.PlayerSubject(playerID) string`, and the
   typed Go facade `access.HasPlayerGrant(ctx, resolver, playerID, grant)
   (bool, error)` consumed by D.
3. **Top-level `crypto:` YAML config block** — first tenant
   `crypto.operators: [<player_id>...]`. Lax+warn startup validation:
   cross-check player IDs against the players table once at startup, emit
   one structured warning per unknown ID, do NOT fail-closed.
4. **Site-doc edit** — `site/docs/operating/crypto-setup.md` extended with
   a section documenting the `crypto.operators` config field, how to find
   a player's player_id, and the lax-warn behavior. PR-blocking, not a
   follow-up.

Plus the 14 master-spec amendments (B's PR also corrects two drift
issues in the decomposition-spec table); see
[Master-spec amendments inventory](#master-spec-amendments-inventory).

### Out of scope

- `OperatorAuthProvider` interface, `InGameCredentialsProvider`
  implementation, or any auth-sequence wiring (sub-epic D).
- `admin_approvals` table or dual-control flow (D).
- `crypto.policy_set` audit-event emission, hash-chain verification, or
  bootstrap-time chain check (D).
- A seeded ABAC policy that *uses* the grant — only attribute exposure
  and the typed facade. The seam for a future policy-driven gate is
  documented as a future seam in the Architecture section but not built.
- UDS / ConnectRPC plumbing (sub-epic C).
- TOTP enrollment, verification, recovery codes, or audit emission
  (sub-epic A; merged).
- In-game admin command to grant `crypto.operator` (P3 follow-up bead,
  filed at landing time of B's PR).

### Bead-description note

`holomush-jxo8.5`'s description currently lists the full check sequence
(`creds → TOTP → RoleAdmin → crypto.operator`) under TDD acceptance.
The decomposition-spec table and this document narrow B's scope to
**capability + storage + facade + amendments**; D wires the check
sequence. B's PR MUST update `jxo8.5`'s bead description to match this
narrower scope and add a `RELATED` link to `jxo8.6` (sub-epic D) for the
sequence wiring.

## Architecture

### File layout

**New:**

- `internal/access/grants.go` — `CapabilityCryptoOperator` constant,
  `PlayerSubject()` helper, `HasPlayerGrant()` facade.
- `internal/access/grants_test.go` — unit tests for the above.
- `internal/access/policy/attribute/player.go` —
  `PlayerAttributeProvider` implementing the existing `AttributeProvider`
  interface.
- `internal/access/policy/attribute/player_test.go` — unit + contract
  tests.
- `cmd/holomush/crypto_operator_validation_test.go` — startup-
  validation integration test (build-tag `//go:build integration`).
- `site/docs/operating/crypto-setup.md` — created as a new operator-
  facing doc for crypto setup. Master spec §9.2 marks this file as
  Phase-8 work; B creates a minimal stub now (operator allow-list
  section only, with frontmatter and brief intro) so the config knob is
  documented at the same time it ships. Phase 8 expands the file with
  the full operator runbook.

**Modified:**

- `internal/access/prefix.go` — add `SubjectPlayer = "player:"`
  constant alongside the existing `SubjectCharacter` / `SubjectPlugin` /
  `SubjectSystem` / `SubjectSession`; add `SubjectPlayer` to the
  `knownPrefixes` slice so `access.ParseEntityRef("player:<ulid>")`
  validates.
- `internal/access/prefix_test.go` — extended with `player:` prefix
  parsing tests mirroring the existing helpers.
- `internal/config/config.go` — add `CryptoConfig{Operators []string
  \`koanf:"operators"\`}` and `DefaultCryptoConfig() CryptoConfig`.
- `internal/config/config_test.go` — extended with `CryptoConfig`
  parsing tests.
- `internal/access/setup/setup.go` — register
  `PlayerAttributeProvider` with the resolver alongside `CharacterProvider`
  et al.
- `internal/access/setup/setup_test.go` — extended with namespace-
  registration test.
- `cmd/holomush/core.go` (or current resolver-wiring location) — load
  `CryptoConfig`, run lax-validation cross-check against the players
  repo, build `PlayerAttributeProvider` with the operator set, register
  it.
- `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` —
  the 14 master-spec amendments (see
  [Master-spec amendments inventory](#master-spec-amendments-inventory)).
- `docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-decomposition.md` —
  one-line drift fix in the decomposition spec's "Master-spec amendments"
  table to align its row count and section anchors with B's amendments
  inventory (see [Drift framing](#drift-framing)).

### Construction-time flow

1. `config.Load(..., &cryptoCfg, "crypto")` populates `cryptoCfg.Operators
   []string`. Empty / missing → empty slice (not nil-vs-empty distinction;
   `[]string{}` semantically).
2. Server queries the players repo once: `SELECT id FROM players WHERE id
   = ANY($1)` against `cryptoCfg.Operators`. For each ULID present in
   config but missing from the result set, emit a structured
   `slog.Warn("crypto.operator references unknown player",
   "player_id", <ulid>)`.
3. **The configured set is used as-is, regardless of cross-check
   outcome.** Validation is observability-only, not gating.
4. Build the operator set as `map[string]struct{}` from
   `cryptoCfg.Operators` (deduplicated). Pass to
   `attribute.NewPlayerAttributeProvider(operatorSet)`.
5. Provider holds a read-only snapshot. No mutex needed in v1 (no
   reload).
6. Register provider with the `attribute.Resolver` alongside
   `CharacterProvider` and the resource providers.

### Runtime call path

D's `OperatorAuthProvider.Authenticate` (sub-epic D, future) invokes
`access.HasPlayerGrant(ctx, resolver, playerID,
access.CapabilityCryptoOperator)` as step 4 of its check sequence.
Internally:

1. `resolver.ResolveSubjectAttributes(ctx, "player:"+playerID, "")`
   dispatches to `PlayerAttributeProvider.ResolveSubject`.
2. Provider returns `{id: playerID, grants: ["crypto.operator"]}` if
   `playerID` ∈ operator set, or `{id: playerID, grants: []}` otherwise.
3. Facade scans the `grants` slice; returns `(true, nil)` on match,
   `(false, nil)` on miss.
4. Errors propagate verbatim from the resolver.

### Future seams (documented, not built)

**Reload seam.** If hot reload of the operator list becomes a
requirement, the `map[string]struct{}` field becomes an `OperatorSource`
interface (`Has(playerID string) bool`) backed by a watcher. v1 captures
the snapshot at construction time; reload requires server restart.

**Policy-driven gate seam.** A future PR MAY seed an ABAC policy
referencing `principal.grants` and swap D's call from `HasPlayerGrant` to
`engine.Evaluate`. The `PlayerAttributeProvider` does not change in that
migration. See [Decision: facade vs policy-driven gate](#decision-facade-vs-policy-driven-gate-now)
below for the v1 trade-off.

## Decision: facade vs policy-driven gate (now)

The decomposition spec (Decision 2) settled that the capability is a
player-attribute grant exposed via ABAC. This document settles a finer
question: how is the gate *enforced* once the attribute is exposed.

Two shapes considered:

- **Option 1 (chosen for v1):** typed Go facade
  `access.HasPlayerGrant`. D's `OperatorAuthProvider` calls the facade
  directly, consistent with its other three typed checks (creds, TOTP,
  RoleAdmin). No ABAC policy seeded.
- **Option 2 (deferred):** seeded ABAC policy `permit(principal is
  player, action == X, resource is Y) when { "crypto.operator" in
  principal.grants };`. D calls `engine.Evaluate`.

**Why Option 1 today.** The gate is `"crypto.operator" in
principal.grants` — a flat list lookup. A policy-driven gate requires
inventing a synthetic resource namespace (e.g., `admin_socket`) and
action verb (e.g., `invoke_break_glass`) solely to satisfy ABAC's target
arity; those nouns have no other consumer and no `ResourceProvider`
backing instance attributes. Within `OperatorAuthProvider`, the four
check steps (creds, TOTP, RoleAdmin, grant) are each typed Go calls
returning typed deny codes; routing only step 4 through ABAC creates an
inconsistency at the auth-provider boundary. ABAC's binary `Permitted /
Denied` decision flattens the per-step deny code unless reconstructed
from `Decision.Reason()`.

**Why Option 2 is worth keeping reachable.** Architectural consistency:
"all auth gates flow through ABAC" is a coherent strategic direction. A
policy-driven gate gains `forbid` overrides (cluster-wide revocation
without code change), DSL-expressible composition, and uniform audit
shape with other ABAC decisions. If/when ABAC is extended to model
identity/grant predicates uniformly, B's PlayerAttributeProvider is the
natural attribute layer; a follow-up PR adds the seed policy and swaps
D's call site.

**Migration cost (honest accounting).** The attribute layer (B) does not
change. The migration cost is one of:

- **DSL grammar extension** to permit policies without a `resource is X`
  clause. The existing seed policies all bind to a real resource
  (location, scene, stream, etc.); whether the parser accepts a
  resource-less rule is unverified. This option avoids inventing fake
  nouns but requires touching the DSL grammar and tooling.
- **Synthetic resource invention** — introduce a virtual `admin_socket`
  (or `crypto_break_glass`) resource type and `invoke_break_glass` action
  verb solely to satisfy ABAC's target arity. Cheaper to implement
  (no grammar work) but adds vocabulary with no other consumer and no
  `ResourceProvider` backing instance attributes (the resource is a
  phantom).

Either path is a follow-up PR; neither is "free" but both are
small-scoped. Picking between them requires verifying the DSL grammar's
current behavior (which writing-plans phase MAY do as part of a
follow-up bead, not as part of B).

**Spec note** for D's design: D's OperatorAuthProvider implementation
SHOULD use the `HasPlayerGrant` facade in v1. Migration to a
policy-driven gate is a documented seam; if pursued, it lands as its own
brainstorm + design + plan, not bundled into D.

## Deliverable 1: `PlayerAttributeProvider`

**Package:** `internal/access/policy/attribute/`.
**File:** `player.go`.

### Public API

```go
// PlayerAttributeProvider exposes player-level attributes for ABAC subject
// resolution. v1 schema: player.id, player.grants. The grant set is
// captured at construction time from the operator allow-list and is
// read-only thereafter.
type PlayerAttributeProvider struct {
    operators map[string]struct{} // player ID → present iff crypto.operator
}

// NewPlayerAttributeProvider constructs a provider with the given
// operator allow-list. Deduplicates the input. Empty slice is valid and
// produces a provider for which every player has no grants.
func NewPlayerAttributeProvider(operatorPlayerIDs []string) *PlayerAttributeProvider
```

### `AttributeProvider` interface satisfaction

The provider implements the existing four-method `attribute.AttributeProvider`
interface (per `internal/access/policy/attribute/provider.go:55-73`):

- `Namespace() string`:
  - Returns the literal `"player"`. The resolver dispatches subjects
    matching `player:` to this provider via the `Namespace()` registration.
- `ResolveSubject(ctx, subjectID string) (map[string]any, error)`:
  - For `subjectID` matching `player:<ulid>`: returns
    `{"id": ulid, "grants": grantsForID(ulid)}` — **un-namespaced keys**
    matching the schema. The resolver namespaces them at merge time
    (see [Provider return shape](#provider-return-shape) below).
  - `grants` is `["crypto.operator"]` if ulid ∈ operators, else
    `[]string{}` (non-nil empty slice for predictability under
    reflect-based contract assertions; the ABAC `in` operator handles
    nil and empty-non-nil identically per
    `internal/access/policy/dsl/evaluator.go:348-371`, so this is
    style, not a correctness requirement).
  - For `subjectID` not in the `player:` namespace: returns `(nil, nil)`
    (per provider contract — declines non-matching namespace; resolver
    moves to the next provider). Matches `CharacterProvider`'s shape at
    `internal/access/policy/attribute/character.go:80-82`.
  - For malformed `subjectID`:
    - **No colon** (`"playerULID"`): in production unreachable —
      `internal/access/policy/attribute/resolver.go:285-293`
      (`validateEntityRef`) pre-validates and rejects before dispatching
      to providers. Provider-level test asserts the error code
      `INVALID_ENTITY_ID` for defense-in-depth.
    - **Empty ID** (`"player:"`): error with code `INVALID_ENTITY_ID`
      and substring `"invalid subject ID format"`. Mirrors
      `CharacterProvider:70-75`.
    - **Non-ULID under `player:`** (`"player:not-a-ulid"`): error with
      code `INVALID_PLAYER_ID` and substring `"invalid player ULID"`.
      Mirrors `CharacterProvider:85-92`'s `INVALID_CHARACTER_ID` shape
      with the namespace-appropriate code.
- `ResolveResource(ctx, resourceID string) (map[string]any, error)`:
  - Always returns `(nil, nil)`. Players are subjects in this design;
    they are not addressable as resources. No resource attributes
    exposed.
- `Schema() *types.NamespaceSchema`:
  - Namespace: `"player"`. Attributes:
    `{"id": AttrTypeString, "grants": AttrTypeStringList}` —
    un-namespaced attribute keys (the `Namespace()` return value is the
    namespace; the schema's `Attributes` map keys are the bare attribute
    names).

### Provider return shape

The provider/resolver contract (per
`internal/access/policy/attribute/resolver.go:436-452`,
`mergeAttributes`) is:

- **Provider returns un-namespaced keys.** `ResolveSubject` returns
  `{"id": ..., "grants": ...}`, NOT `{"player.id": ..., "player.grants":
  ...}`. The keys MUST match the names declared in `Schema().Attributes`.
- **Resolver namespaces keys at merge time.** The resolver writes them
  into `bags.Subject` as `<namespace>.<key>` — i.e., `player.id` and
  `player.grants` (`resolver.go:449`:
  `bagKey := fmt.Sprintf("%s.%s", namespace, key)`).
- **Resolver also injects a top-level `id` directly** from the part
  after `:` in the subject string (`resolver.go:182-184`,
  `:262-264`), so `bags.Subject["id"]` is also populated for any
  subject. This is independent of provider-returned keys.
- **Schema-mismatch silently drops keys.** If the provider returns a
  key not declared in `Schema().Attributes`, `mergeAttributes`'s S6
  check (`resolver.go:439`) drops it and increments
  `abac_rejected_provider_attributes_total{namespace=player,key=<key>}`.
  Provider tests MUST assert that the schema and the return shape
  match exactly.

**Test impact:**

- Tests asserting on the provider's direct return shape use
  un-namespaced keys: `attrs["grants"]`.
- Tests asserting on the resolved attribute bag use namespaced keys:
  `bags.Subject["player.grants"]`.

The facade `HasPlayerGrant` reads from the bag, so its internal lookup
is `bags.Subject["player.grants"]`.

### Concurrency

The `operators` field is set once at construction and never mutated.
`ResolveSubject` calls are race-free without locking. Verified by a
`t.Parallel()` table-driven test.

## Deliverable 2: Capability constant + helpers

**Package:** `internal/access/`.
**File:** `grants.go`.

### Public API

```go
// CapabilityCryptoOperator is the grant string that gates break-glass
// crypto operations (Rekey, AdminReadStream). Held by players in the
// crypto.operators config allow-list. Must be combined with RoleAdmin
// to authorize break-glass.
const CapabilityCryptoOperator = "crypto.operator"

// PlayerSubject returns the ABAC subject ID for a player. Players are
// a Subject-namespace identity alongside characters; this helper formats
// the canonical "player:<ulid>" form expected by PlayerAttributeProvider.
//
// Panics on empty playerID, mirroring the safety guard in
// CharacterSubject / PluginSubject (internal/access/prefix.go:60-163):
// empty subject strings would silently bypass access control if returned
// as "player:" (the bare prefix), so the helper hard-fails instead.
//
// Uses the SubjectPlayer constant added to internal/access/prefix.go.
func PlayerSubject(playerID string) string

// HasPlayerGrant returns true iff the given player holds the named
// grant. The grant set is resolved through the AttributeResolver — the
// resolver MUST be configured with a PlayerAttributeProvider for this
// to return non-empty results.
//
// Returns (false, nil) when the player has no matching grant.
// Returns (false, error) on resolver errors or invalid input. The
// returned error wraps the resolver error with a typed code for empty
// playerID at the API boundary.
func HasPlayerGrant(ctx context.Context, resolver attribute.Resolver,
    playerID string, grant string) (bool, error)
```

### Behavior

- Empty `playerID` → returns `(false, oops.Code("PLAYER_ID_EMPTY")...)`
  without calling the resolver. Defensive at the API boundary.
- Empty `grant` → returns `(false, oops.Code("GRANT_EMPTY")...)`
  without calling the resolver. Empty grant is a programming bug (a
  caller dropped the grant arg) and matches the codebase's strong
  fail-loudly-on-empty-identity-string convention used by
  `internal/access/prefix.go` helpers (`PluginSubject`,
  `CharacterSubject`, etc., which `panic` on empty).
- Resolver error → returns `(false, wrappedErr)`; `wrappedErr` carries
  the resolver's error chain plus the playerID and grant context.
- Match logic: exact string equality on each entry of
  `bags.Subject["player.grants"]`. No prefix / glob / case-insensitive
  matching. Callers SHOULD use the `CapabilityCryptoOperator` constant
  rather than the literal string.

## Deliverable 3: `crypto:` YAML config block

**Package:** `internal/config/`.
**File:** `config.go` (extended).

### Schema

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
func DefaultCryptoConfig() CryptoConfig
```

### YAML shape

```yaml
crypto:
  operators:
    - "01HZAVGE83MGFEXQQH5SP9NXKF"  # admin Alice (Phase 5 break-glass)
    - "01HZAVGE83MGFEXQQH5SP9NXKG"  # admin Bob   (Phase 5 break-glass)
```

The block is loaded via the existing `config.Load(configPath, cmd, &cfg,
"crypto")` pattern (per `internal/config/config.go::Load`).

### Validation behavior at startup

1. Parse `crypto.operators` into `[]string` (ULIDs).
2. Bulk query: `SELECT id FROM players WHERE id = ANY($1::text[])`.
3. **If the cross-check query errors** (PG transient failure, network,
   etc.): emit `slog.Warn("crypto.operator validation skipped",
   "err", err)` and proceed with the full configured set. Validation
   is observability; query failure does not gate startup.
4. **If the query succeeds:** for each ULID in config but missing from
   the result set, emit `slog.Warn("crypto.operator references unknown
   player", "player_id", <ulid>)`. One warning per unknown ID.
5. Build operator set from the **full configured list** (not the
   cross-check intersection) — validation is observability, not gating.
6. Pass operator set to `NewPlayerAttributeProvider(...)`.

### Edge cases

- Missing `crypto:` section → `DefaultCryptoConfig()` → empty operators
  → no DB query issued (skip the `WHERE id = ANY($1)` if list is empty).
- Empty list → same as missing; no DB query.
- Duplicates in config → silently deduplicated by the
  `map[string]struct{}` constructor; no warning.
- Malformed ULID strings in config → currently passed through as-is;
  the bulk PG query treats them as opaque strings. They will trigger an
  unknown-ID warning if not in the players table, which catches
  typos via the same observability path. (No separate ULID-format
  validation in v1.)
- Config-file unreachable / unparseable → existing `config.Load` error
  paths apply (`CONFIG_PARSE_FAILED`, `CONFIG_UNMARSHAL_FAILED`); B does
  not change those.

## Deliverable 4: Site-doc edit

**File:** `site/docs/operating/crypto-setup.md` (existing).

Add a new top-level section, **"Operator allow-list (`crypto.operator`
capability)"**, covering:

- What the capability is and why it exists (narrowing grant on top of
  `RoleAdmin` for break-glass operations).
- The YAML config shape (the example in [YAML shape](#yaml-shape)
  above).
- How to find a player's player_id (existing CLI / DB query — link to
  the relevant operator runbook section if one exists, otherwise
  document the SQL path).
- The lax+warn validation behavior — typos in the config produce
  startup warnings, not hard failures; the config is the source of
  truth.
- Restart-only reload in v1.
- Forward-pointer: in-game grant UX is deferred to a P3 follow-up bead.

PR-blocking, not a follow-up. Operators reading `crypto-setup.md` after
B lands MUST be able to configure operators correctly without consulting
source code.

## Master-spec amendments inventory

The following 14 edits to
[`docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`](2026-04-25-event-payload-crypto-design.md)
land in B's first PR. Row count is 14 (was 13 in the decomposition spec
table; A14 was missed there for §4.6 line 833 and A13's section anchor
was misattributed; B's PR amends the decomposition table for these two
drift fixes per [Drift framing](#drift-framing)).

| #   | Section                       | Edit                                                                                                                                 | Type                       | Authoritative source                          |
| --- | ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ | -------------------------- | --------------------------------------------- |
| A1  | §1 row 137                    | `s/Compromised in-game wizard/Compromised in-game admin with crypto.operator capability/`                                            | mechanical + 1-clause refinement | Decomposition Decision 2                |
| A2  | §1 (threat-model layering)    | New paragraph: single-control = creds + TOTP against row-134 (operator with shell); UDS topology denies reach for row-137 (auth-tier no-shell), reducing row-137 to same two factors once reach achieved; dual-control adds third factor per §5.9 line 1279 | substantive (~150 words) | Decomposition Decision 6 "Honest summary"   |
| A3  | §4.6                          | Add `crypto.policy_set` audit-event shape: subject `audit.<game>.system.crypto_policy.<policy_name>`, type `crypto.policy_set`, payload (`policy_hash bytes`, `prev_hash bytes nullable` (null at genesis), `server_start_ulid string`, `policy_snapshot json`, `server_identity string`, `timestamp`); hash = SHA-256 over RFC 8785 JCS-canonicalized JSON of payload excluding `policy_hash`; canonicalizer library version pinned in `go.mod` and switching it is a master-spec amendment, not a refactor | substantive, schema-defining | Decomposition table cell (D-content, B-carried) |
| A4  | §4.6                          | Add `policy_hash bytes` field to existing `audit.<game>.system.rekey.*` and `audit.<game>.system.operator_read.*` event shapes        | substantive               | Decomposition table cell (D-content, B-carried) |
| A5  | §5.9                          | `s/wizard/admin/` throughout; rewrite step 4 from soft TOTP (warn-and-proceed gated by `require_totp`) to **hard-required TOTP** (refuse with `DENY_NOT_ENROLLED`, no config bypass); insert new step 5 "verify `crypto.operator` capability" (current step 5 becomes step 6) | substantive (Decision 3 enforcement) | Decomposition Decision 3                |
| A6  | §5.9.1 (new)                  | Add subsection **"`crypto.operator` capability — storage and grant mechanism"** documenting capability constant, top-level `crypto:` YAML block, `PlayerAttributeProvider` exposure of `player.grants`, `access.HasPlayerGrant` facade, lax+warn validation, restart-only reload, deferred in-game grant UX | B's primary substantive edit | This sub-epic's design                  |
| A7  | §6.3 1.4                      | `s/wizard role/admin role + crypto.operator capability/`                                                                              | mechanical                | Decomposition Decision 2                       |
| A8  | §6.3.1 (new)                  | Add subsection documenting dual-control protocol: server-issued approval token, `admin_approvals` row schema (`request_id`, `primary_player_id`, `op_kind`, `op_args_hash`, `expires_at = now+5min`), 5-min TTL, second-op CLI (`holomush admin approve <request_id>`), second op MUST have different `player_id` from primary AND hold both `RoleAdmin` AND `crypto.operator` | substantive, schema-defining | Decomposition Decision 5 (D-content, B-carried) |
| A9  | §7.5                          | `s/wizard/admin/` throughout; cross-reference §6.3.1 for dual-control mechanics                                                       | mechanical + xref          | Decomposition Decision 2                       |
| A10 | §10 Bootstrap-time            | Add row: `policy_set chain verification failure on startup` — Detection: `prev_hash` of latest `policy_set` for `policy_name` ≠ actual predecessor's `policy_hash`; Behavior: server refuses to start (consistent with INV-32/33/37 fail-closed pattern) | substantive (table row)   | Decomposition Decision 7 (D-content, B-carried) |
| A11 | §10 Operator-and-policy-errors | Add three rows: `DENY_DUAL_CONTROL_REQUIRED` (server enforces site `dual_control_required`); `DENY_APPROVAL_ARGS_MISMATCH` (primary's args ≠ stored `op_args_hash`); `DENY_POLICY_HASH_UNKNOWN` (invocation references a `policy_hash` not in the chain) | substantive (table rows)  | Decomposition Decisions 5+7 (D-content, B-carried) |
| A12 | §11.1 Phase 5 scope           | Add to scope list: `crypto.policy_set` audit-event emission, `admin_approvals`, `player_totp`, `crypto_rekey_checkpoints` tables      | additive list update       | Decomposition sub-epic decomposition           |
| A13 | §11.3 step 5 (line 2185)      | Rewrite from "Decide on TOTP enrollment for wizard accounts. Required for `Rekey` and `AdminReadStream` once enabled in config." to "Verify TOTP enrolled for admin accounts who hold the `crypto.operator` capability. Required for `Rekey` and `AdminReadStream`." Combines Decision 2's `s/wizard/admin/` with Decision 3's hard-required-TOTP semantic. | substantive — combined `s/wizard/admin/` + Decision 3 enforcement | Decomposition Decisions 2+3 (decomposition table misattributed this to §12; corrected here) |
| A14 | §4.6 line 833                 | `s/<wizard player_id>/<admin player_id>/` in the rekey audit-event example actor metadata (`actor: {kind: operator, os_user: <uid>, player_id: <admin player_id>}`)                                                                                                                                                                                       | mechanical                | Decomposition Decision 2 (missed by decomposition table; added here)                        |

### Drift framing

The decomposition spec is the contract. B writes amendments faithfully
against it. The *only* edits that introduce semantic change beyond
surface replacement are:

- **A1** narrows the threat-model row to "with `crypto.operator`
  capability" — deliberate per Decision 2.
- **A5 step 4** and **A13** flip soft-TOTP to hard-TOTP — deliberate per
  Decision 3.
- **A2 + A6 + A8** are new content; A6 is B's canonical contribution; A2
  and A8 paraphrase Decisions 6 and 5 respectively.
- **A3 + A4 + A10 + A11** are D-authored content carried in B's PR per
  the decomposition-spec footer; the table cells already specify
  schema-level detail, and D's eventual design references these
  amended sections.

**Drift fixes to the decomposition spec itself.** While writing this
spec, two errors were found in the decomposition spec's "Master-spec
amendments" table that B's PR also corrects:

1. **A13's section anchor was wrong.** The decomposition table says
   "§12 ... Strike 'Decide on TOTP enrollment for wizard accounts'."
   The substring lives at master spec §11.3 step 5 line 2185, NOT in
   §12 (which is the 12-row Open Questions table; the substring does
   not appear there). B's PR rewrites A13's section anchor in the
   decomposition spec to point at §11.3 step 5 and aligns the edit
   wording with the corrected target.
2. **A14 was missing.** Master spec §4.6 line 833 contains
   `<wizard player_id>` in the rekey audit-event example. Decision 2's
   `s/wizard/admin/` should reach this line; the decomposition table
   omitted it. B's PR adds A14 to the decomposition spec's amendment
   table.

If D's design discovers that any of A3 / A4 / A8 / A10 / A11 needs
refinement, the refinement is itself a master-spec amendment landing in
D's PR with a corresponding update to the decomposition spec to keep
them aligned.

**Phase 8 deferral.** Master spec §9.2's status table (lines 1968–1972)
lists `site/docs/operating/crypto-setup.md` with `Status: NEW` and a
description focused on master-key bootstrap. After B lands, the file
exists with the operator-allow-list section. B does NOT amend §9.2's
status row in the master spec — that flip from `NEW` to `EXTEND` (and
the description broadening to mention "operator allow-list") is Phase
8's responsibility, since Phase 8 is the home of the full operator
runbook expansion. B's stub is functionally correct; the table-row
status drift is the right kind of bookkeeping for Phase 8 to land
together with its own §9.2 work.

## Invariants

| ID | RFC2119 statement | Test name | Test location |
| --- | --- | --- | --- |
| INV-B1 | `PlayerAttributeProvider.ResolveSubject` MUST return `grants: ["crypto.operator"]` for any player ID present in the configured operator set, given a well-formed `player:<ulid>` subject. | `TestPlayerProvider_ResolveSubject_Operator` | `internal/access/policy/attribute/player_test.go` |
| INV-B2 | `PlayerAttributeProvider.ResolveSubject` MUST return `grants: []` (empty, non-nil) for any player ID NOT present in the configured operator set, given a well-formed `player:<ulid>` subject. | `TestPlayerProvider_ResolveSubject_NonOperator` | `internal/access/policy/attribute/player_test.go` |
| INV-B3 | `access.HasPlayerGrant` MUST return `(true, nil)` iff the resolver's `player.grants` list contains the queried grant string by exact match. | `TestHasPlayerGrant_OperatorPermits` and `TestHasPlayerGrant_DifferentGrantNotMatched` | `internal/access/grants_test.go` |
| INV-B4 | `access.HasPlayerGrant` MUST be string-keyed-generic over grant name (no single-purpose `crypto.operator` hard-coding); the same code path MUST handle hypothetical future grants. | `TestHasPlayerGrant_GenericOverGrantName` | `internal/access/grants_test.go` |
| INV-B5 | Server startup MUST NOT fail-closed on unknown player IDs in `crypto.operators`; it MUST emit one structured `slog.Warn` per unknown ID and proceed. | `TestCryptoOperatorValidation_AllUnknown` | `cmd/holomush/crypto_operator_validation_test.go` (integration) |
| INV-B6 | The `crypto.operators` configuration MUST NOT be reloaded at runtime in v1; the operator set is captured at provider construction time. | `TestPlayerProvider_NoMutationAPI` — uses reflection on the `PlayerAttributeProvider` type to assert no exported mutator methods exist and the constructor is the only path that writes to the operator set. | `internal/access/policy/attribute/player_test.go` |
| INV-B7 | An empty or missing `crypto.operators` configuration MUST result in `grants: []` for every player; no player holds any grant. | `TestCryptoOperatorValidation_EmptyConfig` and `TestPlayerProvider_ResolveSubject_NonOperator` | `cmd/holomush/crypto_operator_validation_test.go`, `internal/access/policy/attribute/player_test.go` |
| INV-B8 | `access.HasPlayerGrant` MUST reject empty `playerID` AND empty `grant` inputs at the API boundary with typed errors (`PLAYER_ID_EMPTY` / `GRANT_EMPTY` respectively) without invoking the resolver. | `TestHasPlayerGrant_RejectsEmptyPlayerID` and `TestHasPlayerGrant_RejectsEmptyGrant` | `internal/access/grants_test.go` |
| INV-B9 | `PlayerAttributeProvider` MUST NOT resolve resources; `ResolveResource` MUST return `(nil, nil)` for any input. Players are subjects in this design. | `TestPlayerProvider_ResolveResource_AlwaysNil` | `internal/access/policy/attribute/player_test.go` |
| INV-B10 | The `player:` Subject namespace MUST coexist with existing namespaces (`character:`, resource namespaces) without registration conflict. | `TestPlayerProviderNamespaceNonColliding` | `internal/access/setup/setup_test.go` |
| INV-B-AMEND | All 14 master-spec amendments listed in [Master-spec amendments inventory](#master-spec-amendments-inventory) MUST land in the same PR as B's first implementation commit. | `TestSpecAmendmentsLanded` — meta-test that opens the master spec and asserts each amendment's distinctive fingerprint substring is present (e.g., `"crypto.operator capability"` in §1 row 137; `"hard-required TOTP"` in §5.9 step 4; the new §5.9.1 / §6.3.1 subsection headings; `"admin accounts who hold the `crypto.operator` capability"` in §11.3 step 5; etc.). | One of: (a) `internal/access/spec_amendments_test.go` (Go test, runs under `task test`), or (b) `scripts/check_spec_amendments.sh` invoked from a `task lint` hook (shell-level CI gate). Writing-plans phase picks one and pins it. |
| INV-B-BEAD | B's PR MUST update bead `holomush-jxo8.5`'s description to reflect the narrow scope (capability + storage + facade + amendments) and MUST add a `RELATED` link to `holomush-jxo8.6` (sub-epic D) for the OperatorAuthProvider check-sequence wiring. | `TestBeadJxo8_5DescriptionUpdated` — runs `bd show holomush-jxo8.5 --json` (or equivalent), asserts the description contains the fingerprint substring `"capability + storage + facade + amendments"` AND does NOT contain the stale fragment `"OperatorAuthProvider check sequence (creds → TOTP → RoleAdmin → crypto.operator)"`. Asserts `RELATED` edges include `holomush-jxo8.6`. | `scripts/check_bead_jxo8_5.sh` invoked from `task pr-prep` (the gate that gates all PR pushes per CLAUDE.md). Writing-plans phase pins the script and the gate hook. |

### Meta-test enforcement

INV-B-AMEND requires a meta-test that prevents an unintended split of
amendments and code. The Phase 2 substrate spec uses an analogous
pattern: `internal/eventbus/crypto/dek/api_test.go::stubAllowSet` (per
`docs/superpowers/specs/2026-04-30-event-payload-crypto-phase2-substrate-design.md`
§Decision 1) is a static map enforced by a CI test that checks
amendments are present.

B's plan MUST select a concrete enforcement mechanism: either

- a Go test that opens `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`
  and asserts each amendment's distinctive substring is present (e.g.,
  `"crypto.operator capability"` in §1 row 137; `"hard-required TOTP"`
  in §5.9 step 4; etc.), OR
- a shell-level CI gate equivalent.

Picking one is left to the writing-plans phase; the invariant binds
the spec to *some* mechanism that detects amendments-without-code or
code-without-amendments.

## Test plan

Per CLAUDE.md TDD conventions: write each test file first, then the
implementation.

### Unit tests

**`internal/access/grants_test.go`** (new):

- `TestPlayerSubject` — formats `"player:" + ulid` correctly.
- `TestCapabilityCryptoOperatorIsCryptoOperator` — constant equals
  `"crypto.operator"` (locks the string surface).
- `TestHasPlayerGrant_OperatorPermits` — resolver yields
  `grants: ["crypto.operator"]` → `(true, nil)`. (INV-B3)
- `TestHasPlayerGrant_NonOperatorDenies` — resolver yields `grants: []`
  → `(false, nil)`.
- `TestHasPlayerGrant_DifferentGrantNotMatched` — resolver yields
  `grants: ["other.grant"]`, query for `"crypto.operator"` → `(false,
  nil)`. (INV-B3 negative side)
- `TestHasPlayerGrant_GenericOverGrantName` — same code path matches
  `"some.future.grant"` when the resolver returns it. (INV-B4)
- `TestHasPlayerGrant_PropagatesResolverError` — resolver returns
  wrapped error → facade returns `(false, err)` with chain intact.
- `TestHasPlayerGrant_RejectsEmptyPlayerID` — `""` returns
  `(false, oops.Code("PLAYER_ID_EMPTY")...)` without invoking the
  resolver. (INV-B8)
- `TestHasPlayerGrant_RejectsEmptyGrant` — empty grant string returns
  `(false, oops.Code("GRANT_EMPTY")...)` without invoking the resolver.

**`internal/access/policy/attribute/player_test.go`** (new):

- `TestPlayerProviderContract` — runs `assertProviderContract(t,
  NewPlayerAttributeProvider([]string{}))` from the existing
  `contract_test.go`.
- `TestPlayerProvider_ResolveSubject_Operator` — operator ID in set
  → `{id, grants: ["crypto.operator"]}`. (INV-B1)
- `TestPlayerProvider_ResolveSubject_NonOperator` — non-operator ID
  → `{id, grants: []}` (empty slice, not nil). (INV-B2, INV-B7)
- `TestPlayerProvider_ResolveSubject_NonPlayerNamespace` —
  `"character:..."`, `"location:..."` → `(nil, nil)` (provider declines
  non-matching namespaces).
- `TestPlayerProvider_ResolveSubject_MalformedSubject` — table-driven:
  - `"player:"` (empty post-colon) → error code `INVALID_ENTITY_ID`,
    substring `"invalid subject ID format"`.
  - `"playerULID"` (no colon) → error code `INVALID_ENTITY_ID`
    (production-unreachable; resolver pre-validates per
    `resolver.go:285-293`, but the provider asserts the code for
    defense-in-depth).
  - `"player:not-a-ulid"` → error code `INVALID_PLAYER_ID`, substring
    `"invalid player ULID"`.
- `TestPlayerProvider_Schema` — exposes exactly `{player.id:
  AttrTypeString, player.grants: AttrTypeStringList}`.
- `TestPlayerProvider_ResolveResource_AlwaysNil` — resource resolution
  returns `(nil, nil)` for any input. (INV-B9)
- `TestPlayerProvider_ConstructorDeduplicatesOperators` —
  `NewPlayerAttributeProvider([]string{"01A", "01A", "01B"})` produces
  a 2-entry set.
- `TestPlayerProvider_ConcurrentResolves` — `t.Parallel()` table-driven;
  verifies the read-only set is race-safe under `go test -race`.
- `TestPlayerProvider_NoMutationAPI` — meta-test using reflection to
  scan the `PlayerAttributeProvider` type for any exported mutator
  methods or any non-constructor methods that mutate `operators`.
  (INV-B6)

**`internal/access/prefix_test.go`** (extended):

- `TestSubjectPlayerConstant` — `SubjectPlayer == "player:"`.
- `TestKnownPrefixesIncludesPlayer` — `knownPrefixes` slice contains
  `SubjectPlayer`.
- `TestParseEntityRefAcceptsPlayerNamespace` —
  `access.ParseEntityRef("player:01ABC...")` returns the player
  namespace + ID without error.
- `TestPlayerSubjectPanicsOnEmpty` — `PlayerSubject("")` panics, mirroring
  `CharacterSubject("")` / `PluginSubject("")` etc.

**`internal/config/config_test.go`** (extended):

- `TestLoadParsesCryptoOperators` — `crypto: { operators: [<ulid1>,
  <ulid2>] }` → `CryptoConfig.Operators` has both.
- `TestLoadCryptoMissingSectionIsEmpty` — no `crypto:` key → empty
  list, no error.
- `TestLoadCryptoOperatorsEmptyListIsEmpty` — `crypto: { operators:
  [] }` → empty list.
- `TestDefaultCryptoConfigIsEmpty` — `DefaultCryptoConfig().Operators`
  is empty.
- `TestLoadCryptoOperatorsMalformedFails` — `crypto: { operators:
  "not a list" }` → `CONFIG_UNMARSHAL_FAILED`.

### Integration tests

**`internal/access/setup/setup_test.go`** (extended):

- `TestPlayerProviderRegisteredWithResolver` — wire the full setup,
  call `resolver.ResolveSubjectAttributes(ctx, "player:<operator_id>",
  "")`, verify `bags.Subject["player.grants"]` is `["crypto.operator"]`.
- `TestPlayerProviderNamespaceNonColliding` — registering
  `PlayerAttributeProvider` alongside existing providers does not error
  and all namespaces resolve correctly. (INV-B10)

**Startup-validation integration test** (build-tag `//go:build integration`,
co-located with the wiring code, e.g.
`cmd/holomush/crypto_operator_validation_test.go`):

- `TestCryptoOperatorValidation_AllKnown` — seed players table with 2
  IDs; config lists same 2 → 0 warnings; operator set has both.
- `TestCryptoOperatorValidation_SomeUnknown` — seed 2 IDs; config lists
  3 (2 known + 1 unknown) → 1 structured warning emitted (assert via
  captured `slog.Handler`) with `player_id` matching the unknown ID;
  operator set has all 3.
- `TestCryptoOperatorValidation_AllUnknown` — seed 0 IDs; config lists
  2 → 2 warnings; operator set has both; **server still completes
  startup**. (INV-B5)
- `TestCryptoOperatorValidation_EmptyConfig` — `crypto: { operators:
  [] }` → no DB query issued (assert via repo mock or query log);
  operator set empty; no warnings. (INV-B7)
- `TestCryptoOperatorValidation_DuplicatesInConfig` — config has
  `[01A, 01A, 01B]`; players table has all → operator set has 2
  entries; no warnings (deduplication is silent).
- `TestCryptoOperatorValidation_QueryFails` — inject a transient PG
  failure into the bulk-existence query; assert `slog.Warn`
  `"crypto.operator validation skipped"` is emitted with the wrapped
  error; assert server completes startup; assert operator set contains
  the full configured list (validation skipped, not gated).

### Out-of-scope tests (D / E / F own them)

- `OperatorAuthProvider` step-4 integration with the facade.
- Break-glass denial when player has admin role but no
  `crypto.operator` grant.
- E2E: configure operator, restart, invoke break-glass, verify success.
- Hot-reload of operator list — deferred (no v1 implementation to test).

### Coverage target

> 80% per package per CLAUDE.md. Achievable trivially — every line of
`grants.go`, `player.go`, and the `CryptoConfig` extension is exercised
by the unit suite.

### Verification gates

Order: `task lint` → `task test` → `task test:int` → `task pr-prep`.

Pre-push reviews:

1. `/review-crypto` — touches the crypto surface (master-spec
   amendments, `crypto.*` capability constant, `crypto:` config block).
2. `/review-abac` — touches `internal/access/`.
3. `/review-code` — runs after the domain-specific reviewers per
   CLAUDE.md "Pre-Push Review Gates" matrix.

## Dependencies

- **Master spec:** `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`
  (§1, §4.6, §5.9, §6.3, §7.5, §10, §11.1, §12; amended in this PR per
  the inventory above).
- **Decomposition spec:**
  [`docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-decomposition.md`](2026-05-07-event-payload-crypto-phase5-decomposition.md)
  (Decisions 2, 3, 5, 6, 7).
- **Sub-epic A (TOTP substrate; merged in PR #3535,
  `holomush-jxo8.3`):** B does not depend on A's code, but the master-
  spec amendments coordinate with A's spec on the §5.9 step-4 hard-
  required-TOTP rewording (A's spec already enforces hard TOTP for the
  enroll/verify path; A5 carries that into the master spec for the
  break-glass path).
- **Existing ABAC infrastructure
  (`internal/access/policy/attribute/`):** `AttributeProvider`
  interface, `Resolver` interface, `assertProviderContract` test
  battery. B reuses without modifying.
- **Existing config infrastructure (`internal/config/`):**
  `config.Load(...)` pattern, koanf-backed YAML parsing. B extends
  with the new `CryptoConfig` struct.
- **Existing players repository:** read-only bulk-existence query
  needed for the lax-validation cross-check. B's writing-plans phase
  MUST inspect the existing players repo for a suitable
  bulk-existence method; if none exists, B's plan adds one minimal
  read-only helper (e.g., `players.Repo.ExistingIDs(ctx, ids []string)
  ([]string, error)` returning the subset of `ids` that exist, backed
  by `SELECT id FROM players WHERE id = ANY($1::text[])`). The helper
  is the only repo modification B makes; no schema migration.

## Open questions

None at the close of brainstorm. Implementation-time discoveries land
as updates to this spec.

## References

- `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` — master spec (amended by B).
- `docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-decomposition.md` — decomposition spec (B's parent).
- `docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-totp-substrate-design.md` — sub-epic A (TOTP substrate; merged).
- `docs/superpowers/specs/2026-04-30-event-payload-crypto-phase2-substrate-design.md` — Phase 2 substrate (Decision 1: meta-test enforcement pattern, reused for INV-B-AMEND).
- `internal/access/role.go` — existing role taxonomy.
- `internal/access/policy/attribute/character.go` — existing AttributeProvider pattern (reference).
- `internal/config/config.go::Load` — existing config-loading pattern.
- `site/docs/operating/crypto-setup.md` — operator runbook (extended by B).
