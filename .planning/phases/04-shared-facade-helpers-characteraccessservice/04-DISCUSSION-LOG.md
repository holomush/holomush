# Phase 4: Shared Facade Helpers & `CharacterAccessService` - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-08-10
**Phase:** 4-Shared Facade Helpers & `CharacterAccessService`
**Areas discussed:** Multi-alt owner reads (D-27), Surface slice + census universe, The D-29 permit shape (criterion 6), Enforcing "by construction", plus three loose ends (sketch findings, length caps, tier resolution)

**Areas offered and all four selected.** Most of Phase 4 is locked by `01-SPEC.md`
§7/§8/§9 and by Phase 2's shipped ABAC vocabulary, so the offered areas were
deliberately narrow: only what those sections left open, got wrong, or deferred.

---

## Multi-alt owner reads (Phase 2 D-27's deferred half)

**Grounding surfaced before the question:** `entity_properties.owner` is a scalar
`TEXT` column (`000001_baseline.sql:360`), so `property.go:212-215`'s ALL rule
reduces to *"the owning character is that player's only character"* —
`seed:viewer-property-private-read` is unsatisfiable for any 2+-alt player.

| Option | Description | Selected |
|--------|-------------|----------|
| Keep ALL, split the audiences | No policy change; `viewer:` stays public-facing, owners read private fields via owner-audience RPCs which never build a `viewer:` principal | ✓ |
| Relax `owner_player_id` to the row's player | Fixes self-visibility uniformly, but is a real widening — reveals alt-linkage the grid-side character-keyed policy withholds | |
| Union on both permit-side peers | D-27's explicitly declined alternative; largest blast radius; reopens a settled Phase 2 decision | |

**User's choice:** Keep ALL, split the audiences → **D-69**
**Notes:** Adjudicated on the grid-vs-web asymmetry: `seed:property-private-read`
is `owner == principal.character.id`, so character D genuinely cannot read C's
private row even under one human. Any owner-side web relaxation therefore
discloses alt-linkage — this is the widening D-27 declined, not a category error
to be corrected.

| Option | Description | Selected |
|--------|-------------|----------|
| `GetCharacterProfile` identical for every viewer | One projection, one path; the owner verifies what they publish by seeing what a stranger sees | ✓ |
| Owner-elevated — fold in private rows | Convenient for a preview flow, but adds a branch inside the exact function criterion 2 asserts over, and varies response shape by viewer identity | |

**User's choice:** Identical for every viewer → **D-70**

| Option | Description | Selected |
|--------|-------------|----------|
| Registry invariant + binding test | Hand-registered `invariants.yaml` entry, bound by a 2-alt fixture asserting the private row is absent | ✓ |
| Test + doc comment only | Cheaper, no registry churn — but the registry is where a future phase actually looks | |
| You decide | Judge against `.claude/rules/invariants.md`'s in-scope table | |

**User's choice:** Registry invariant + binding test → **D-71**

---

## Surface slice + census universe

**Two tensions surfaced before the question:** (1) §2.6's census derives from
**generated service descriptors**, so a declared-but-unimplemented RPC is a
census member the moment the proto compiles. (2) Criterion 1 cannot literally
cover "every character-returning RPC" — `ListCharacterDirectory` and
`GetCharacterProfile` serve anonymous viewers, and `resolveAndGate` rejects
guests.

| Option | Description | Selected |
|--------|-------------|----------|
| Only what Phase 4 implements | 6 facade RPCs + `Web*` proxies; Phases 5/6 add their own rows alongside implementations; every census member has a live handler | ✓ |
| Whole §9 surface, unimplemented stubs | Wire contract visible up front, but 10 stubs become census members with audience verdicts they cannot honor, forcing a skip list | |
| Whole surface, two-census split | Honest, but ships two censuses with differing universes — §2.6 warns against a second gate becoming grounds to relax the first | |

**User's choice:** Only what Phase 4 implements → **D-72**

| Option | Description | Selected |
|--------|-------------|----------|
| Owner-audience RPCs, both halves | The set where "non-guest player who owns this character" is the contract; public reads covered by criteria 2 and 3 | ✓ |
| All character-returning RPCs, per-audience gate | Broader coverage in one test, but the per-audience mapping becomes a second thing to keep honest | |
| You decide | Whichever keeps the RED failure sharpest | |

**User's choice:** Owner-audience RPCs, both halves → **D-73**

| Option | Description | Selected |
|--------|-------------|----------|
| Amend §9.3, don't declare it | Strike the `RenameCharacter` row with D-44's rationale; also fix §9.4.2's dependent prose | ✓ |
| Leave §9.3, add a deferred marker | Preserves the design verbatim, but leaves a normative section describing an RPC no phase will build | |

**User's choice:** Amend §9.3 → **D-74**

---

## The D-29 permit shape (criterion 6)

**Grounding surfaced before the question:** Phase 2 shipped only half of
PROFILE-11 — `seed:profile-public-read` is `resource is property`, but the
in-world description is a **column**, reached via `resource is character`, which
no widening has touched.

**Interruption:** the user paused to ask what `D-29` was before answering. It was
explained (Phase 2 `02-CONTEXT.md:321-338` — the deferred
`permit(principal is character, action in ["read"], resource is character)`,
deferred because `characterToProto` returns `PlayerId` and `LocationId`), then
the same three questions were re-asked unchanged.

| Option | Description | Selected |
|--------|-------------|----------|
| New narrow action + own projection | A `read_description`-shaped action, `GetCharacter` and its colocation clause untouched; the omission is structural | ✓ |
| Keep `read`, narrow `CharacterInfo` itself | Cleanest conceptually, but `location_id` is load-bearing for movement/presence — a breaking grid-wide change | |
| Keep `read`, add a when-clause guard | The DSL cannot constrain what the caller will project — describes the handler, not the request | |

**User's choice:** New narrow action + own projection → **D-75**

| Option | Description | Selected |
|--------|-------------|----------|
| Both `character` and `viewer` twins | Character permit closes D-29's literal deferral; viewer twin is what lets the web profile reach the description at all | ✓ |
| Viewer twin only | Closes the web half, leaves the criterion's stated grid half open | |
| Character permit only | Closes the grid deferral, strands PROFILE-10a behind a policy gap | |

**User's choice:** Both twins → **D-76**

### The audit question — reframed mid-discussion

Originally offered as three grades of ceremony for authoring a **new** audit
(committed re-runnable query / query + blocking checkpoint / you decide).

**User's response, verbatim in substance:** *"we need to stop over-engineering
checks. if there is true value in 1, and it's not simply going to end up being
duplicative cruft that ABAC and/or E2E tests should cover in the future, then 1"*

Investigating that challenge found the audit **already exists**:
`.planning/phases/02-abac-schema-vocabulary/02-AUDIT-profile-public-read.sql`
result sets (4) `:149-160` and (5) `:163-174` read `characters.description`
directly. Phase 2's run returned a legitimate zero on a 3-character sandbox, and
GitHub #4937 (open, `awaiting-precursor`) already asks for a re-run against a
**populated** corpus — which does not exist. The question was reframed:

| Option | Description | Selected |
|--------|-------------|----------|
| Re-run + record, non-blocking | Re-run sets 4/5 and append a dated Phase-4 result | |
| Skip the re-run, cite Phase 2's result | The corpus has not changed; re-running reproduces the same zero | ✓ |
| Re-run and block on a non-zero finding | Near-certainly dead branch on a 3-character corpus | |

**User's choice:** Skip the re-run, cite Phase 2's result → **D-77**
**Notes:** *"And I want to make sure that we capture this pattern/duplicate and
what led us here as an anti-pattern. We need strong guidance in keeping things as
simple and idiomatic as we can, lets not invent novel things just because —
especially when there are already in tree solutions, tests, or OSS tooling that
will do the job."*

### Anti-pattern capture (follow-on)

| Option | Description | Selected |
|--------|-------------|----------|
| Store it as a blessed engram rule | Joins the repo rule scope; surfaces in every session's rules index | ✓ |
| Memory only, no rule | Memory helps only once something prompts a search | |
| Rule, but reword first | | |

**User's choice:** Store as a rule → stored as `7zy1161fh1`; narrative memory
`r65waekn3h`.

| Option | Description | Selected |
|--------|-------------|----------|
| `.claude/rules/references/plan-review-learnings.md` | Existing read-on-demand home for this class of finding; zero new files | ✓ |
| A GitHub issue | Visible outside this toolchain, but guidance would sit open indefinitely | |
| Nothing beyond engram | | |

**User's choice:** `plan-review-learnings.md` → new head section added.

---

## Enforcing "by construction"

**Grounding surfaced before the question:** two in-tree mechanisms already do
what criteria 1 and 5 ask —
`world_envelope_census_test.go:136`'s `bodyReferencesSelector(fn.Body, recvName, "mutator")`,
and `sceneaccess_service.go:28-31`'s narrow dependency interface.

| Option | Description | Selected |
|--------|-------------|----------|
| Embedded `playerGate` struct in `internal/grpc` | Method promotion keeps ~20 existing call sites byte-identical; `s.<name>` is exactly what `bodyReferencesSelector` censuses | ✓ |
| Free functions taking explicit deps | Most explicit, but rewrites every call site and forces a novel AST predicate | |
| New `internal/grpc/playergate` package | Strongest boundary; largest move for a boundary neither census nor compiler needs | |

**User's choice:** Embedded struct → **D-78**

| Option | Description | Selected |
|--------|-------------|----------|
| Narrow interface — make it not compile | `ListByParent` outside the facade's type set; zero rules, zero suppression vocabulary | ✓ |
| `gorules` ruleguard analyzer | Catches it anywhere, but adds a second gate over a compiler-enforceable property | |
| `test/meta` AST test | Fails the test rather than the build; strictly weaker for more machinery | |

**User's choice:** Narrow interface → **D-79**

| Option | Description | Selected |
|--------|-------------|----------|
| Distinctive sentinel + byte scan | Only form that distinguishes absent from present-and-empty for proto3 scalars | ✓ |
| Proto reflection over the unmarshaled message | Better failure messages, but cannot make the distinction under test | |
| You decide | | |

**User's choice:** Sentinel + byte scan → **D-80**

---

## Loose ends

Offered as multiSelect against "leave to my discretion"; the user selected **all
three** to settle explicitly.

| Option | Description | Selected |
|--------|-------------|----------|
| Record rulings, build nothing | Rename withdrawn; A2/A3 accepted as design, built in Phase 6 | ✓ |
| Record rulings AND amend 01-SPEC now | Fold A2/A3 into §9.2/§11.3 in the same amendment pass | |
| Defer all three to Phase 6 | Requires editing a ROADMAP phase entry, for which no sanctioned writer exists | |

**User's choice:** Record rulings, build nothing → **D-81**

| Option | Description | Selected |
|--------|-------------|----------|
| Reuse world's constants, enforce in the facade | 100 / 4000 from `validation.go:20-21`; no new numbers minted | ✓ |
| Same values via `buf.validate` on the proto | Declarative, but a proto-level rejection carries no oops code through §9.6 | |
| New profile-specific constants | Two new numbers where two existing ones fit; they will drift | |

**User's choice:** Reuse world's constants → **D-82**
**Notes:** Investigation established IDENT-02a needs no new cap at all —
`world.ValidateDescription` already runs on the `UpdateCharacterDescription`
path, so the 4000-char, UTF-8 and control-char rules are inherited.

| Option | Description | Selected |
|--------|-------------|----------|
| In the facade, at viewer-principal construction | The only layer holding the session; keeps the gateway computing nothing (§9.1) | ✓ |
| In `attribute/viewer.go` alongside the provider | Would need session state plumbed below the boundary, inverting §9.1's direction | |
| You decide | | |

**User's choice:** In the facade → **D-83**

---

## Claude's Discretion

- Naming of the new ABAC action, the two seed policy ids, and the `playerGate`
  struct/field names.
- The new registry invariant's scope (`INV-PRIVACY` vs `INV-ACCESS`) and id.
- Test-file placement, tier, and naming.
- Whether the narrow world interface is one interface or two (read vs mutate).

No area was answered with a bare "you decide" — every "You decide" option offered
was declined in favor of an explicit choice.

## Deferred Ideas

- Populated-corpus re-run of the exposure audit (GitHub #4937, left open).
- Relaxing `owner_player_id` to the row's player — D-69's rejected option,
  reconsider only on a product requirement for owner self-view on the public page.
- Admin RPCs, A2's sort key, A3's username search — decided here, built in Phase 6.
- `CreateCharacter` (IDENT-01) and profile rendering (PROFILE-01/10a) — Phase 5.
- Rename + the approval dimension — backlog 999.20, linked to 999.6.
- A lint banning character-shaped proto struct literals outside the projection
  package — §2.6 deliberately did not mandate it.
- Registering `viewer:` / `admin_section:` in `knownPrefixes` — hygiene, carried
  from Phase 2.
