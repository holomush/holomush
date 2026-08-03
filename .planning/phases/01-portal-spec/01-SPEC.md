# HoloMUSH v0.13 Web Portal — Portal SPEC

- **Status:** Complete — all sixteen sections authored across plans 01-01 through 01-06. §14's nine amendments are applied to their own artifacts and its one divergence is recorded; §13's eight invariants are registered `binding: pending`; every citation is swept and stamped in §16.
- **Date:** 2026-08-01
- **Milestone:** v0.13
- **Phase:** 1
- **Requirements:** PORTAL-01..PORTAL-10
- **Source research:** `.planning/research/SUMMARY.md`
- **Phase context:** `.planning/phases/01-portal-spec/01-CONTEXT.md`

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are
to be interpreted as described in RFC 2119.

---

## 1. Overview

This SPEC fixes the shape decisions the v0.13 web portal is built on, before any
of Phases 2–6 writes code against them. It is one document by deliberate choice:
a master-plus-sub-spec structure is the source of most master-versus-sibling
amendment drift recorded in `.claude/rules/references/design-review-learnings.md`,
and one document has no sibling to drift from.

**What this SPEC fixes.** The audience matrix and per-audience message shape; the
read-surface and name-capture inventories; the character lifecycle state
vocabulary; the name-normalization policy; the profile and media data model; the
profile visibility model; the new RPC surface; the admin information
architecture; the sorting/filtering verdict; and the verification-integrity rules
every v0.13 plan inherits.

**What this SPEC deliberately does not build.** v0.13 ships the visibility
**model plus its seeded defaults only**. There is no owner-facing per-field
visibility control, and no editing surface for the game-wide configuration —
see §8 for the normative statement of both. Media ships as schema and proto shape
with **zero upload behavior**. Role mutation is excluded from character
administration for this milestone (PORTAL-08). An omission is not an exclusion:
each of these is stated normatively where it belongs, not left to be inferred
from silence.

**Reading order for a downstream planner.** §7 and §8 are the profile contract —
a Phase-2 planner writes the tier-floor seed policies and the `profile.*` naming
convention from those two sections alone. §13 is the invariant register; every id
declared there exists in `docs/architecture/invariants.yaml`.

## 2. Audience Matrix and Message Shape

This section is normative. It fixes how many audiences exist, what message each
one receives, who builds those messages, and the single mechanism that binds the
whole arrangement.

### 2.1 There are exactly three audiences

| Audience | Who | What it may see |
| --- | --- | --- |
| `public` | Any reader who is not the character's owner and is not acting as an admin — including an anonymous web visitor and a guest session. Reachability and per-attribute floors are then evaluated per §8. | Identity and whatever the game's configured tier floors publish to that viewer. Never presence telemetry. |
| `owner` | The authenticated player who owns the character. | Everything `public` sees, plus the owner-only operational fields — session state, last location, last-played time — and the character's own lifecycle status. |
| `admin` | A caller holding the admin role, reached through the admin surface. | The rich administrative row: everything `owner` sees, plus the administrative fields §10 defines. |

Three is the whole vocabulary. Every read surface enumerated in §3 carries
**exactly one** of these three verdicts, and no surface may invent a fourth.

The audiences are **not** a ladder that a single message walks up by adding
fields. They are three separate shapes, for the reason §2.2 gives.

### 2.2 One proto message per audience

**The system MUST define a distinct proto message per audience:
`PublicCharacter`, `OwnCharacter`, and `AdminCharacter`.** A response destined
for one audience MUST carry that audience's message type and no other.

This is the decision that makes absence a **type-system property** rather than a
runtime discipline. A `PublicCharacter` has no field in which an owner-only value
could be placed, so a projection that tried to leak one does not compile. The
guarantee is therefore checked by the Go compiler on every build, by everyone, on
every branch — not by a reviewer noticing.

This mirrors the structural argument the scenes privacy boundary already makes in
this tree for its participant-gated versus public RPC pair
(`docs/superpowers/specs/2026-05-23-scenes-phase-6-logs-vote-privacy-design.md:262-267`,
§5.1 "Two-pair RPC architecture"): *"deliberately separate handlers"* whose proto
contract carries *"separate request/response messages with different field
sets"*, justified because the separation is what prevents a future refactor from
silently widening the private path. The same reasoning applies one layer down, to
the row-to-message projection.

**The rejected alternative, and why.** The obvious cheaper design is one
`Character` message whose privacy-bearing scalars are declared `optional` and
cleared server-side for viewers who may not see them. It is rejected. Its
guarantee is a **per-field discipline that nothing in the build enforces**: a
proto3 scalar that someone forgets to mark `optional` does not marshal as absent
when cleared — it marshals as `""`, which is **present**. The absence guarantee
then fails silently, for exactly the one field whose declaration was wrong, with
no compile error, no test failure, and no visible difference from a field the
character legitimately left blank. One forgotten keyword produces one silent
leak. The per-audience split has no equivalent failure mode because there is no
per-field keyword to forget.

This is the same rule `.claude/rules/abac-providers.md` mandates for ABAC
attribute providers — *omit, do not sentinel* — expressed here at the wire rather
than in the attribute bag. There it exists because an empty-string sentinel
satisfies `"" == ""` against any unresolved peer and creates a fail-open match in
a default-deny system. Here it exists because a present-empty field discloses that
a withheld value exists. Both are the same shape of bug; §8.9 states the read-time
half and this section states the message-shape half.

### 2.3 One projection function per (row → audience)

**Exactly three projection functions exist, one per audience:**

| Function | Produces | Reads |
| --- | --- | --- |
| `projectPublic` | `PublicCharacter` | the character row plus the viewer-filtered property slice (§8) |
| `projectOwner` | `OwnCharacter` | the character row plus the owner's full property slice |
| `projectAdmin` | `AdminCharacter` | the character row plus the administrative fields §10 defines |

**No handler MAY construct a character-shaped response message by struct
literal.** Every construction site goes through one of the three functions above.
A handler that needs a character-shaped message in a new response calls the
projection for its audience; it does not assemble the fields itself, and it does
not copy another handler's assembly.

The rule exists because the failure mode is not "a handler redacts wrongly" but
"a handler that constructs its own message never learns that redaction is a thing
that happens" — which is precisely how the list-and-search surfaces diverge from
the detail surface they were supposed to match (`.planning/research/PITFALLS.md`
Pitfall 2). Two RPCs serving the same audience share one projection function; see
§3's second membership rule.

### 2.4 `WebListAllCharacters` splits into two RPCs

Today one directory RPC serves every caller. It is replaced by two, one per
audience.

The message it hands back on the web side is
`holomush.core.v1.CharacterDirectoryEntry` (`api/proto/holomush/core/v1/core.proto:902-907`),
which is already identity-only — `character_id` and `name`. The leak is not in
that message; it is in the *roster* message that the surrounding auth and
character-management responses carry. `holomush.web.v1.CharacterSummary`
(`api/proto/holomush/web/v1/web.proto:496-513`, mirroring
`api/proto/holomush/core/v1/core.proto:688-710`) carries four presence-telemetry
fields beside the identity pair:

- `has_active_session` (`web.proto:503`)
- `session_status` (`web.proto:506`)
- `last_location` (`web.proto:509`)
- `last_played_at` (`web.proto:512`)

Those four are **owner-audience or admin-audience fields**. They MUST NOT appear
in any message served to the `public` audience.

The replacement surface is:

| New RPC | Audience | Response carries |
| --- | --- | --- |
| `WebListCharacterDirectory` | `public` | `repeated PublicCharacterSummary` — identity fields only; all four telemetry fields above are dropped. |
| `WebAdminListCharacters` | `admin` | `repeated AdminCharacter` — the rich row, reached through the admin surface and gated per §10. |

`WebListAllCharacters` is removed. It is not deprecated, not aliased, and not
kept as a thin forwarder — see §2.5.

`PublicCharacterSummary` is the list-shaped `public` projection; it is
`PublicCharacter` restricted to what a list row needs. Both are produced by
`projectPublic`; a list is not a second audience.

### 2.5 Breaking-change posture

**Existing message shapes MAY be replaced outright.** v0.13 ships **no
compatibility shim, no deprecation window, and no grandfathering.**

The system has no users other than its maintainer, which is what makes the
per-audience split of §2.2 and the RPC split of §2.4 cheap rather than a
migration programme. A removed RPC is removed; a reshaped message is reshaped;
every call site is updated in the same change.

The posture is stated here so that a downstream planner does not spend a task
budget preserving a contract nobody depends on. A "keep the old RPC forwarding to
the new one" task is **out of scope** and MUST NOT be planned.

### 2.6 The binding mechanism: a census compared by set equality

**The census over character-returning RPCs, compared by set equality, is the
SOLE mandated enforcement gate for this section.**

**What the census is.** A test that derives, from the generated service
descriptors, the set of every RPC whose response transitively contains a
character-shaped message, and compares that derived set against the checked-in
expected set — which is the §3 inventory. Inequality in either direction is RED.

**The comparison key.** The key is the **fully-qualified proto method name** in
`package.Service.Method` form — for example
`holomush.web.v1.WebService.WebListCharacters`. It is compared as an **exact
string**.

- It MUST NOT be a substring or prefix match. `…ListCharacters` is a substring of
  `…ListAllCharacters`, so a substring key silently collapses two distinct
  members into one and the census stops counting one of them.
- It MUST NOT be a Go handler identifier. Handler names are not stable against
  refactor, several are shared across services, and a Go name cannot be derived
  from the descriptor set the census reads.

**The comparison semantics.** The comparison is **set equality** —
**order-independent**, and explicitly **not** a list or sequence comparison, and
explicitly not a loop over the expected entries checking each one is present. The
§3 inventory's row order is presentational; reordering it MUST NOT change the
census verdict.

Set equality is what carries **both** halves of the guarantee at once:

| Half | What it catches |
| --- | --- |
| no more | An RPC exists in the tree that is not in §3 — a new character-returning endpoint that skipped the projection. |
| no fewer | An RPC is in §3 but no longer in the tree — an inventory row gone stale, which quietly shrinks what the census covers. |

A `for` loop over a hand-written list carries **neither**: it iterates the
expected set, so an unexpected member is never visited, and a stale expected
member fails for the wrong reason or is deleted to make the suite green. This is
the structural point `.planning/research/PITFALLS.md` Pitfall 2 makes about
per-endpoint suites — *"a per-endpoint test suite is structurally incapable of
detecting a missing member of its own set"* — and it is why the census, not a
per-endpoint suite, is the gate.

The comparison MUST be implemented as a set-equality assertion producing a
symmetric-difference diff on failure, so a RED census names which member is
extra and which is missing.

> **Notably absent:** there is **no** lint or meta-test banning character-shaped
> proto struct literals outside the projection package. Such a lint was
> considered and **deliberately not mandated**. The census already fails RED for
> every case that matters — a new character-returning RPC cannot ship without an
> inventory row — and a second gate over the same property costs a rule, a
> suppression vocabulary, and a maintenance surface for coverage the census
> already has. A future PR adding the lint is an **increment, not a correction**:
> it does not indicate this SPEC was wrong, and it MUST NOT be treated as a
> prerequisite for anything in v0.13. The reviewer for this SPEC **MUST** verify
> that no v0.13 PR silently adds a second mandated gate and then relaxes the
> census on the grounds that the lint covers it.

### 2.7 No visibility hints travel on the wire

**No response message MAY carry a per-field visibility hint, mask, flag map, or
hidden-field list telling the client which fields to hide.**

Enforcement is **absence from the marshaled bytes**. A field the viewer may not
see is not in the response at all. The client is not a participant in the
decision, is not told a decision was made, and cannot be made to cooperate
incorrectly.

This section deliberately names **no** placeholder field for such a map. A named
placeholder is an invitation; a downstream planner who finds one will populate
it. There is nothing to populate.

The reviewer for this SPEC **MUST** verify that no v0.13 PR adds a map, repeated
string, or bitmask field to any character-shaped response message whose purpose is
to describe the visibility of sibling fields. §13's per-field-absence invariant is
asserted over marshaled bytes precisely so that a hint field cannot satisfy it.

## 3. Read-Surface Inventory

This section is normative, and it is also **data**: the tables below are the
checked-in expected set the Phase-4 census (§2.6) compares against.
The comparison is **set equality** on the fully-qualified proto method name. A row
here is a census member; a member absent from here is a RED census.

### 3.1 Membership rules

**Rule 1 — membership is decided by response type, never by runtime
cardinality.** An RPC whose response transitively contains a character-shaped
message is a census member **even when it returns zero characters at runtime**. An
RPC that returns zero characters is **still a census member**: an empty list never
removes a member, a nil field never removes a member, and a surface that happens
to return nothing in the current fixture is still a member.

The rule matters because the alternative — deriving membership from observed
responses — makes the census's coverage depend on fixture richness, which is the
exact defect `.planning/research/PITFALLS.md` Pitfall 3 records for
snapshot-shaped tests. `holomush.web.v1.WebService.WebListMyScenes` is the worked
example: its `CharacterSceneInfo` documents that *"the roster fields
(participants, observers) are unset on this surface"*
(`api/proto/holomush/scene/v1/scene.proto:1013-1015`), so it returns no character
projection at runtime today. Membership is nonetheless decided by the response
type, and §3.4 records why that particular surface is governed by §5 rather than
by this census.

**Rule 2 — each RPC carries exactly one audience verdict, and RPCs sharing an
audience share one projection function.** The verdict names the audience of the
**character data carried in the response**, not the caller's relationship to the
RPC. A player reading their own roster is `owner`; the same player reading a
directory of everyone else's characters is `public`, because the character data
is not theirs. Two RPCs with the same verdict MUST call the same one of
`projectPublic` / `projectOwner` / `projectAdmin` (§2.3) rather than each
assembling its own message — a second assembly site is a second place the
audience contract can drift.

**Rule 3 — this table IS the expected set.** Adding a character-returning RPC
without adding a row here makes the census RED. **That is the intended behavior**,
not a friction to be worked around. The correct response to a RED census is to add
the row and give the surface an audience verdict and a projection; it is never to
relax the comparison, never to widen the predicate, and never to delete the
inventory row that went stale.

### 3.2 What counts as a character-shaped message

The census predicate is mechanical, so it must be stated mechanically.

**Type-reachable members.** A response transitively containing any of these
messages is a census member:

| Message | Defined at |
| --- | --- |
| `holomush.world.v1.CharacterInfo` | `api/proto/holomush/world/v1/world.proto:77-91` |
| `holomush.core.v1.CharacterSummary` | `api/proto/holomush/core/v1/core.proto:688-710` |
| `holomush.core.v1.CharacterDirectoryEntry` | `api/proto/holomush/core/v1/core.proto:902-907` |
| `holomush.core.v1.PresenceEntry` | `api/proto/holomush/core/v1/core.proto:428-441` |
| `holomush.web.v1.CharacterSummary` | `api/proto/holomush/web/v1/web.proto:496-513` |
| `holomush.web.v1.WebPresenceEntry` | `api/proto/holomush/web/v1/web.proto:960-968` |
| `holomush.plugin.host.v1.CharacterSummary` | `api/proto/holomush/plugin/host/v1/world.proto:123-128` |
| the v0.13 replacements: `PublicCharacter`, `PublicCharacterSummary`, `OwnCharacter`, `AdminCharacter` (§2.2, §2.4) | Phase 4 |

**Name-reachable members.** Some surfaces carry character identity as a bare
scalar or as rendered bytes, where no typed message exists for a predicate to
find. **A type-driven predicate alone would miss every one of them**, so they are
census members **enumerated by name** in §3.3, and the census MUST seed its
expected set with both categories. Stating this limit is the point: a census whose
predicate silently cannot reach a whole class of surface is a census that reports
green over a leak.

**How the census seeds this class, on both sides — and what that costs.** §2.6's
set-equality guarantee has two halves, and for the name-reachable class the derived
side cannot come from the descriptors: the paragraph above says so itself, no typed
message exists for the predicate to find. The census therefore seeds:

| Side | Source |
| --- | --- |
| expected | the §3.3 rows of **both** categories — type-reachable and name-reachable |
| derived | the descriptor walk for the type-reachable category, **unioned with the same explicitly-named name-reachable list** |

The consequence **MUST** be stated here rather than discovered under a red test:
for the name-reachable class the two sides share one source, so that class is
**self-certifying**. The census's "no more" half does not hold over it, and a new
bare-scalar identity field cannot make the census RED. The type-reachable class
keeps both halves intact, and it is the majority of the inventory.

A literal implementation that tried to derive the name-reachable side from the tree
would be RED on day one, and the natural repair is precisely the union above — so
the union is specified rather than left to be invented by whoever meets the red
test first, under pressure to make it green.

**Standing Phase-4 obligation.** Because no mechanical gate covers it, a new
**scalar-identity** field on a response message — a bare character name, a rendered
speaker string, an identifier whose display form is a name — **MUST** be
accompanied by a new §3.3 row in the same change. This is a review obligation, not
a test, and it is recorded here so a reviewer has something to cite.

**Deliberately outside the predicate.** `holomush.scene.v1.ParticipantInfo`
(`api/proto/holomush/scene/v1/scene.proto:325-338`),
`holomush.scene.v1.PublishedSceneEntry.speaker`
(`api/proto/holomush/scene/v1/scene.proto:820-827`), and
`holomush.scene.v1.CharacterSceneInfo`
(`api/proto/holomush/scene/v1/scene.proto:1012-1027`) are **not** character
projections. They are scene-membership and scene-content rows that **denormalize a
character display name** at their own layer. They are governed by §5's
name-capture inventory, not by this census, because the question they raise is
"was this name captured at emit time and is it therefore unreachable by a later
privacy change" — which is a different question with a different answer from "what
does this projection publish now".

The exception is the three public export surfaces: they denormalize names **and**
publish them to unauthenticated readers, so they are inventoried in **both** §3 and
§5. Being in one table only is the defect that cross-listing exists to prevent.

### 3.3 The inventory

Row order is presentational; the census comparison is order-independent (§2.6).

#### Host-owned character projections

| RPC | Proto location | Audience | Character-shaped message returned | Notes |
| --- | --- | --- | --- | --- |
| `holomush.world.v1.WorldService.GetCharacter` | `api/proto/holomush/world/v1/world.proto:30` | `public` | `CharacterInfo` (`world/v1/world.proto:157-160`) | ABAC-gated `read` on the character resource. Carries `player_id` (`world/v1/world.proto:81`) — an OOC player↔character linkage. Phase 4 MUST decide whether `PublicCharacter` retains it; it is not identity the `public` audience obviously needs. |
| `holomush.world.v1.WorldService.ListCharactersAtLocation` | `api/proto/holomush/world/v1/world.proto:38` | `public` | `repeated CharacterInfo` (`world/v1/world.proto:177-181`) | Returns an empty list, never `NOT_FOUND`, for an empty location — a rule-1 member with a routinely-empty result. |
| `holomush.plugin.host.v1.WorldQueryService.QueryCharacter` | `api/proto/holomush/plugin/host/v1/world.proto:28` | `public` | inline id/player_id/name/description/location_id (`plugin/host/v1/world.proto:96-108`) | The plugin-facing twin of `GetCharacter`. A plugin is neither owner nor admin, so it receives the `public` projection. Per `.claude/rules/plugin-runtime-symmetry.md` the Lua `holomush.query_character` host function and this RPC MUST land on the same projection. |
| `holomush.plugin.host.v1.WorldQueryService.QueryLocationCharacters` | `api/proto/holomush/plugin/host/v1/world.proto:33` | `public` | `repeated CharacterSummary` (`plugin/host/v1/world.proto:131-134`) | Already identity-only (`id`, `name`). |
| `holomush.core.v1.CoreService.AuthenticatePlayer` | `api/proto/holomush/core/v1/core.proto:74` | `owner` | `repeated CharacterSummary` (`core.proto:742`) | The login response carries the authenticating player's own roster with presence telemetry. Owner-audience, and correctly so — but it means `CharacterSummary` is load-bearing on the auth path and cannot simply be narrowed without reshaping this response too. |
| `holomush.core.v1.CoreService.SelectCharacter` | `api/proto/holomush/core/v1/core.proto:80` | `owner` | `character_name` scalar (`core.proto:781`) | Name-reachable: a bare display-name scalar, not a typed projection. |
| `holomush.core.v1.CoreService.CreatePlayer` | `api/proto/holomush/core/v1/core.proto:85` | `owner` | `repeated CharacterSummary` (`core.proto:817`) | Roster is empty at creation — a rule-1 member whose result is always empty today. |
| `holomush.core.v1.CoreService.CreateGuest` | `api/proto/holomush/core/v1/core.proto:90` | `owner` | `repeated CharacterSummary` (`core.proto:843`) | The provisioned starter character. |
| `holomush.core.v1.CoreService.CreateCharacter` | `api/proto/holomush/core/v1/core.proto:95` | `owner` | `character_name` scalar (`core.proto:872`) | Name-reachable. |
| `holomush.core.v1.CoreService.ListCharacters` | `api/proto/holomush/core/v1/core.proto:99` | `owner` | `repeated CharacterSummary` (`core.proto:887`) | The player's own roster, enriched with session status and last location. |
| `holomush.core.v1.CoreService.ListAllCharacters` | `api/proto/holomush/core/v1/core.proto:107` | `public` | `repeated CharacterDirectoryEntry` (`core.proto:912`) | The directory. Already identity-only; §3.5 records the doc-comment rule that made it so. |
| `holomush.core.v1.CoreService.CheckPlayerSession` | `api/proto/holomush/core/v1/core.proto:129` | `owner` | `repeated CharacterSummary` (`core.proto:980`) | The cookie-auth check returns the caller's own roster. |
| `holomush.core.v1.CoreService.ListFocusPresence` | `api/proto/holomush/core/v1/core.proto:169` | `public` | `repeated PresenceEntry` (`core.proto:471`) | Other characters present in the caller's focus context. `PresenceEntry` deliberately omits an arrival timestamp (`core.proto:439-440`) — a precedent for the same omit-don't-publish discipline §2.7 states. |
| `holomush.core.v1.CoreService.QueryStreamHistory` | `api/proto/holomush/core/v1/core.proto:154` | `public` | `repeated EventFrame` (`core.proto:1102`), carrying `actor_id` (`core.proto:279`) and a payload with a denormalized display name | Name-reachable. Historical frames; the names in them were captured at emit time — see §5. |
| `holomush.web.v1.WebService.WebAuthenticatePlayer` | `api/proto/holomush/web/v1/web.proto:157` | `owner` | `repeated CharacterSummary` (`web.proto:540`) | Web twin of `AuthenticatePlayer`. |
| `holomush.web.v1.WebService.WebSelectCharacter` | `api/proto/holomush/web/v1/web.proto:162` | `owner` | `character_name` scalar (`web.proto:580`) | Name-reachable. |
| `holomush.web.v1.WebService.WebCreatePlayer` | `api/proto/holomush/web/v1/web.proto:167` | `owner` | `repeated CharacterSummary` (`web.proto:607`) | |
| `holomush.web.v1.WebService.WebCreateGuest` | `api/proto/holomush/web/v1/web.proto:173` | `owner` | `repeated CharacterSummary` (`web.proto:631`) | |
| `holomush.web.v1.WebService.WebCreateCharacter` | `api/proto/holomush/web/v1/web.proto:177` | `owner` | `character_name` scalar (`web.proto:656`) | Name-reachable. |
| `holomush.web.v1.WebService.WebListCharacters` | `api/proto/holomush/web/v1/web.proto:182` | `owner` | `repeated CharacterSummary` (`web.proto:669`) | The web roster. |
| `holomush.web.v1.WebService.WebCheckSession` | `api/proto/holomush/web/v1/web.proto:207` | `owner` | `repeated CharacterSummary` (`web.proto:745`) | |
| `holomush.web.v1.WebService.WebQueryStreamHistory` | `api/proto/holomush/web/v1/web.proto:222` | `public` | `repeated GameEvent` (`web.proto:832-835`), whose `actor` field is *"the DISPLAY NAME of the acting character, extracted from the event payload"* (`web.proto:427-429`) | Name-reachable, and the clearest case that a type predicate alone is insufficient: the leaked value is a bare `string`. |
| `holomush.web.v1.WebService.WebListFocusPresence` | `api/proto/holomush/web/v1/web.proto:252` | `public` | `repeated WebPresenceEntry` (`web.proto:987`) | Web twin of `ListFocusPresence`. |

#### The `WebListAllCharacters` split (§2.4)

| RPC | Proto location | Audience | Character-shaped message returned | Notes |
| --- | --- | --- | --- | --- |
| `holomush.web.v1.WebService.WebListAllCharacters` | `api/proto/holomush/web/v1/web.proto:187` | `public` | `repeated holomush.core.v1.CharacterDirectoryEntry` (`web.proto:682`) | **Removed in v0.13.** Present in this table so the census's expected set describes the pre-split tree; Phase 4 deletes this row in the same change that deletes the RPC. |
| `holomush.web.v1.WebService.WebListCharacterDirectory` | Phase 4 | `public` | `repeated PublicCharacterSummary` | Replacement. Identity only; drops `has_active_session`, `session_status`, `last_location`, `last_played_at`. |
| `holomush.web.v1.WebService.WebAdminListCharacters` | Phase 4 | `admin` | `repeated AdminCharacter` | Replacement. The rich row, reached through the admin surface (§10). |

#### The three existing public export surfaces

These are **already live and already unauthenticated**. Each publishes a
denormalized character identity to any reader — a **name** by proto contract, a
character **id** in today's implementation, a mismatch §5.4 records and issue
**#4901** tracks — and each is the reason §5 exists.

**Which of the two the column holds does not affect census membership.** The
surface is character-returning either way, so every row below is a member on the
same grounds, and none of them may be dropped from the expected set on the
argument that it publishes ids rather than names today. §5.4 states why that
argument would be wrong even prospectively: the documented follow-up resolves
names into these surfaces, and the frozen rows have no update path.

Every one is inventoried in **both** this table and §5's name-capture table.

| RPC | Proto location | Audience | Character-shaped message returned | Notes |
| --- | --- | --- | --- | --- |
| `holomush.web.v1.WebService.WebExportScene` | `api/proto/holomush/web/v1/web.proto:329` | `public` | rendered `bytes content` (`web.proto:1136-1143`) containing per-line speaker labels | Proxies `holomush.sceneaccess.v1.SceneAccessService.ExportScene` (`api/proto/holomush/sceneaccess/v1/sceneaccess.proto:143`). Name-reachable through opaque bytes — no proto field names a character, so only an explicit enumeration reaches it. Publishes other characters' names to the requesting participant. |
| `holomush.web.v1.WebService.WebGetPublicSceneArchive` | `api/proto/holomush/web/v1/web.proto:345` | `public` | `repeated string participants_snapshot` (`web.proto:1195`) and `repeated PublishedSceneEntry content_entries` (`web.proto:1197`), each entry carrying `speaker` (`scene.proto:822`) | Proxies `SceneAccessService.GetPublicSceneArchive` (`sceneaccess.proto:164`). **Unauthenticated.** Publishes a frozen participant list to anonymous readers — character **ids** as implemented today, **names** by proto contract (§5.4, issue **#4901**). A later privacy change cannot reach this snapshot under either. |
| `holomush.web.v1.WebService.WebDownloadPublicSceneArchive` | `api/proto/holomush/web/v1/web.proto:351` | `public` | rendered `bytes content` (`web.proto:1216-1221`) | Proxies `SceneAccessService.DownloadPublicSceneArchive` (`sceneaccess.proto:171`). **Unauthenticated.** The download form of the row above; same names, rendered rather than structured, so likewise reachable only by explicit enumeration. |

`holomush.web.v1.WebService.WebListPublishedScenes` (`api/proto/holomush/web/v1/web.proto:339`,
proxying `sceneaccess.proto:157`) returns `repeated holomush.scene.v1.PublicSceneArchive`
(`web.proto:1176`), whose `participants_snapshot` (`scene.proto:1053`) carries the
same frozen participant column in bulk form — one entry per published scene,
under the identical §5.4 id-versus-name caveat. It is a **fourth** public export
surface, and it is
a census member with audience `public` on the same grounds as the three above. It
is named separately here because research enumerated three; the tree carries four,
and the fourth is the one that returns them in bulk.

### 3.4 The v0.13 surfaces §9 adds

§9 defines the new profile and administration RPCs. **Every one whose response
carries a character-shaped message is a census member**, and §9's own surface
table carries the audience verdict for each. The Phase-4 census's expected set is
the **union** of §3.3 and §9's character-returning rows, minus the rows §2.4
deletes.

This is stated rather than pre-listed because §9's RPC names are fixed by plan
01-04, and inventing them here would produce two tables that could disagree. What
is fixed here is the obligation: a new RPC in §9 that returns character data
without an audience verdict is an incomplete section, and the census will say so.

### 3.5 The privacy line the codebase already draws

The `ListAllCharacters` doc comment already states the rule this SPEC is
promoting (`api/proto/holomush/core/v1/core.proto:105-106`):

> *"Connection/online state is NOT included; that is a separately-permissioned
> attribute."*

That is a correct privacy line, drawn in the right place, and it is today enforced
by **nothing but two hand-written field lists** — `CharacterDirectoryEntry`
(`core.proto:902-907`), which honors it, and `CharacterSummary`
(`core.proto:688-710`, mirrored at `web.proto:496-513`), which carries exactly the
four connection-state fields the comment excludes. Nothing prevents a future field
from being added to the wrong one of the two. A comment is not a gate.

**This SPEC promotes that comment into the census-bound guarantee of §13.** The
rule survives unchanged; what changes is that it acquires an enforcement mechanism
that fails RED rather than a reader who has to notice.

## 4. Character Lifecycle

This section is normative. It fixes the storage shape, the state vocabulary, the
rule that makes the vocabulary safe, the three distinct operations, and what a
retired character looks like from every read surface.

### 4.1 The storage shape

**The lifecycle is a single text column on `characters`, NOT NULL, defaulting to
the active value, constrained by a `CHECK` over a closed vocabulary.**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `characters.status` | `TEXT` | Yes | The character's lifecycle state. Phase 2 transcribes this DDL fragment verbatim: `status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'retired', 'idle'))`. Exactly three values; see §4.2 for the vocabulary and §4.3 for the exhaustiveness rule that makes shipping all three safe. |

There is no status column on `characters` today
(`internal/store/migrations/000001_baseline.up.sql:67-76` — `id`, `player_id`,
`name`, `description`, `location_id`, `created_at`, and nothing else). Phase 2
adds this one.

**This follows the repo's existing enum-by-CHECK convention**, which has four
in-tree precedents in the baseline migration alone:

| Precedent | Location |
| --- | --- |
| `access_policies.effect` | `internal/store/migrations/000001_baseline.up.sql:259` |
| `access_policies.source` | `internal/store/migrations/000001_baseline.up.sql:261` |
| `access_audit_log.effect` | `internal/store/migrations/000001_baseline.up.sql:294` |
| `entity_properties.visibility` | `internal/store/migrations/000001_baseline.up.sql:358` |

**The rejected alternatives, and why.**

| Alternative | Why rejected |
| --- | --- |
| A paired `retired_at TIMESTAMPTZ` beside a boolean or beside the status | Two columns that can disagree. Nothing prevents `status='active'` with a non-null `retired_at`, and a reader consulting the wrong one of the two is a bug with no failing test. |
| Timestamp-only derivation — no status column, state inferred from `retired_at IS NULL` | No database backstop: the vocabulary is unconstrained, an impossible combination is representable, and **every reader must derive the state identically**. That is the same per-site discipline §2.2 rejects at the wire, one layer down. |

**No character can exist without a status.** The column is `NOT NULL` with a
`DEFAULT`, so an insert that names no status gets `'active'` and an insert that
names `NULL` fails. There is no "unknown", no empty string, and no absent-state
case for a reader to handle.

### 4.2 The vocabulary

**Exactly three values, shipped from day one:**

| Value | Means | Reachable in v0.13 |
| --- | --- | --- |
| `active` | In play. The default for every character, existing and new. | Yes — the default, and the target of un-retire. |
| `retired` | The owner has stepped the character out of active play. Reversible; see §4.4 and §4.5. | Yes — the `retire` operation. |
| `idle` | The character has been idled out by the system for inactivity. | **No.** v0.13 ships **no** transition into `idle`. See §4.3 — this is precisely why the exhaustiveness rule is not optional. |

**They are compared as exact lowercase string literals.** A comparison **MUST
NOT** case-fold, **MUST NOT** trim, and **MUST NOT** accept any other spelling.
`'Retired'`, `'RETIRED'` and `' retired'` are not the retired state; they are
values the `CHECK` rejects at write time, and any reader that would have accepted
them is comparing wrong.

### 4.3 The exhaustiveness rule — the other half of shipping `idle` early

Shipping a value nothing can reach is safe **only** when paired with the rule
below. Stated separately, the value alone is a latent fail-open.

**Rule 1 — every read of the lifecycle status MUST be an exhaustive `switch`
whose `default` arm denies.** Not `if status == "retired" { deny }`. Not
`status != "active"` in one query's `WHERE` and a different predicate in the
next. An exhaustive `switch` over the three values, with a `default` that denies
and does not fall through to permit.

**Rule 2 — Phase 2 MUST ship a test that constructs a character directly in
`idle`** — bypassing the (nonexistent) transition by writing the row — **and
asserts it is excluded from selection.** A value no test can reach is a value no
test covers; the direct construction is what makes the coverage non-vacuous
rather than trivially satisfied by the fact that no fixture can produce it.

**Why both halves.** An unreachable enum value plus a non-exhaustive check is
**structurally identical to the `members` privacy tier research CONFLICT 4
rejected**: a vocabulary token that no code path exercises, guarded by a
predicate that enumerates the values it knows about. It reads correct for as long
as the value stays unreachable, and it **fails open on the day the value becomes
reachable** — which is a later milestone's change, made by someone who has no
reason to audit every lifecycle read. The exhaustive `switch` fails closed
instead, and the direct-construction test proves it does so today rather than
promising it will.

This is the same move EXT-03/EXT-04 make for the deferred admin sections: ship
the registration, gate it, and prove the gate with a fixture that reaches the
otherwise-unreachable state.

### 4.4 Three distinct operations

**`retire`, `idle-out` and `purge` are three different operations.** Conflating
any two of them is unrecoverable, because they differ in reversibility, in what
they touch, and in who may invoke them.

| Operation | What it does | Reversible | Touches the name |
| --- | --- | --- | --- |
| `retire` | Sets `status` to `retired`. Owner-invoked. | Yes — un-retire sets `status` back to `active` (§4.5). | **No.** The name stays reserved. |
| `idle-out` | Sets `status` to `idle`. System-invoked on inactivity. **Not implemented in v0.13** — the value ships, the transition does not. | Yes, by the same shape as un-retire. | **No.** |
| `purge` | **Not a lifecycle state.** The existing irreversible character delete. The row is gone. | **No.** | The name is released, because the row that held it no longer exists. |

**`purge` is NOT a state, and the SPEC declares no `purged` value.** It is
`world.Service.DeleteCharacter` (`internal/world/service.go:745-777`), which is
already shipped, already ABAC-gated on the `delete` action, and already
irreversible. It deletes the character row and cascades its `entity_properties`
in one transaction and emits a `character_deleted` tombstone envelope in that
same transaction. Its blast radius beyond the character row is real and is not
hypothetical:

| Referencing column | On delete | Consequence |
| --- | --- | --- |
| `character_roles.character_id` | `ON DELETE CASCADE` (`internal/store/migrations/000001_baseline.up.sql:84`) | The character's roles are dropped silently. |
| `players.default_character_id` | `ON DELETE SET NULL` (`internal/store/migrations/000001_baseline.up.sql:80`) | The player's default character is silently nulled. |
| `locations.owner_id` | **no `ON DELETE` clause** (`internal/store/migrations/000001_baseline.up.sql:99`) — Postgres defaults to `NO ACTION` | The delete **errors at runtime** for any character that owns a location. |
| `objects.owner_id` | **no `ON DELETE` clause** (`internal/store/migrations/000001_baseline.up.sql:143`) | Same — the delete errors for any character that owns an object. |

**`purge` MUST NOT be wired to any player-facing affordance.** It is not the
implementation of a "delete my character" button, and an admin surface whose
button says "delete" **MUST NOT** call it without the SPEC-level decision that
this section deliberately does not make. Erasure-on-request is a separate feature
with separate requirements; v0.13 does not ship it.

**Retire MUST NOT release the name.** This is normative and it has two
independent reasons, either of which is sufficient:

1. **The name stays reserved for rostering.** Releasing it forecloses the
   deferred rostering work (backlog 999.6), which needs the historical binding to
   remain resolvable.
2. **Releasing it creates an impersonation vector.** Character display names are
   denormalized into surfaces no later write can reach — §5 inventories every
   one. A freed name claimed by a new character inherits the identity of every
   captured occurrence: every archived pose, every published scene, every
   rendered export. Neither retirement nor denormalized history is a bug alone;
   the two together are, and the cheap defense is to never release the name.

### 4.5 What a retired character looks like

A retired character is **out of active play, not hidden.** Three properties,
each normative:

1. **Roster-visible, with an un-retire affordance.** The owner's own roster
   (`owner` audience, §2.1) shows the retired character, labelled retired, with a
   control that returns it to `active`. Retirement is not a one-way door and the
   UI **MUST NOT** present it as one.
2. **Unselectable for play.** Character selection **MUST** exclude any character
   whose status is not `active`, by the exhaustive `switch` of §4.3 — so `idle`
   is excluded by the same code path that excludes `retired`, without a second
   predicate to keep in sync.
3. **Its public profile still resolves, and says retired.** A retired character's
   profile is reachable exactly as an active one is (§8's floors apply
   unchanged), and it carries a retired indication. It **MUST NOT** return the
   not-found-equivalent of §8.7.

**Why the profile stays up.** Scene archives publish the character's name to
anonymous readers regardless of its lifecycle state — `WebGetPublicSceneArchive`
and its three siblings (§3.3) serve frozen participant lists that retirement
cannot reach and §5 states will never be rewritten. A profile that 404s beside a
live archive naming the same character is an **inconsistency**, not privacy: the
name is already out, and the only thing the 404 removes is the reader's ability
to learn that the character is no longer in play. Retire means *left active
play*, and the profile says so.

The retired indication is a projection concern, not a new visibility tier. It is
carried by the character's own lifecycle status, which §2.1 already assigns to
the `owner` and `admin` audiences in full; what the `public` audience receives is
the fact of retirement, not the operational history behind it.

## 5. Name-Capture Surface Inventory

This section is normative, and like §3 it is also **data**: it is the checked-in
list of every place the system stores a character's display name instead of
referencing the character by id. §3 answers *"what does this projection publish
now"*; this section answers the different question §3.2 routes here — *"was this
name frozen at capture time, and is it therefore beyond the reach of any later
rename or privacy change"*.

The answer for the frozen half is **yes, and deliberately so**. The event bus is
append-only by design; rewriting it to chase a rename would be a far worse bug
than a stale name.

### 5.1 The four normative rules

**Rule 1 — the tie-break. Every site carries exactly ONE verdict.** A site is
`live` **only when an update path actually exists that a rename reaches**;
otherwise it is `historical`. A site that looks like both is `historical`. No
site is ever both, and no site is unclassified — an unlisted surface is an unmade
decision, not an implicitly-live one.

The test is mechanical, and it is about the **write** path, not the read path: if
renaming the character changes what this site subsequently serves without any
further write, it is `live`. If the value was written once and no rename touches
that write, it is `historical` — **even if the surface that serves it is
recomputed on every read**, because recomputation from frozen bytes yields the
frozen name.

**Rule 2 — two different equality relations, and they MUST NOT be conflated.**

| Verdict | Compared how |
| --- | --- |
| `historical` | As the **stored bytes**, exactly. A historical captured name is **NEVER re-normalized on read.** It was frozen at capture time under whatever policy was in force then, and re-normalizing it on read would silently rewrite the historical record in the reader's memory — producing a name the archive does not contain, differing between two readers running different code versions. |
| `live` | Under §6's normalized form. |

These are two relations over the same-looking strings. A single shared
"names are equal" helper used for both is a bug: it either re-normalizes history
or it leaves live comparison unnormalized, and there is no third outcome.

**Rule 3 — the concurrency verdict.** A rename running concurrently with a
name-capture write **may leave the captured name stale** — the capture may record
the pre-rename name after the rename has committed. **This outcome is accepted**
under the `historical` verdict: no compensating update runs, no reconciliation
job is scheduled, and the append-only log is **never** mass-updated.

This is a **deliberate scope decision, not an unhandled race.** The window is one
in-flight emit; the stale value it produces is indistinguishable from the stale
values every earlier capture already holds by design, which is exactly why no
special handling is warranted. Serializing renames against every in-flight emit
would put a lock on the emit path to buy consistency in a log whose whole
contract is that it is not consistent with live state.

**Rule 4 — the idempotency verdict for a same-name rename.** A rename whose
requested name normalizes to the character's **current stored display name,
byte-for-byte, is a no-op**: it emits no rename event, does not bump
`characters.version`, and returns success.

- The `expected_version` precondition is **still evaluated**. A stale
  `expected_version` on a same-name rename is still a conflict error; the no-op
  is about the write, not about the guard.
- A request whose normalized **uniqueness key** matches the current one but whose
  **display form differs** — a case or spacing variant — is a **real rename**: it
  writes the new display form, emits the event, and bumps the version. It does
  not collide with itself in the uniqueness index, because the row it would
  collide with is its own.

Rejection was the alternative and it is rejected: a client retrying after a lost
response would receive an error for a request that already succeeded, which turns
a harmless retry into a user-visible failure. Stating one outcome here is the
point — silence would become two implementations, one in Phase 3 and a different
one in Phase 6.

### 5.2 The inventory

Row order is presentational only. **No rule anywhere in this SPEC references a
row of this table by position**, and reordering the table changes nothing.

#### Historical — frozen at capture, unreachable by rename

| Capture site | Where the name is stored | Verdict | Reachable by rename | Consequence |
| --- | --- | --- | --- | --- |
| `actor_display_name` on the communication-content payload, stamped at emit time from the acting character's name | `api/proto/holomush/comm/v1/comm.proto:25-28`; stamped at `pkg/plugin/comm/builder.go:41`, `:48`, `:55` (each passing `a.Name`) | `historical` | no | The canonical name capture. Every say / pose / OOC line carries the name the character had when the line was emitted. |
| The same payload, durably projected into the **host** audit table | `internal/store/migrations/000052_events_audit_partition.up.sql:114` (`envelope BYTEA NOT NULL` — the name is inside the opaque envelope, not a column) | `historical` | no | Append-only. There is no column to update and no update path; a rename reaches none of it. |
| The same payload, durably projected into the **plugin-owned** scene log | `plugins/core-scenes/migrations/000004_create_scene_log.up.sql:23` (`payload BYTEA NOT NULL`) | `historical` | no | Same shape as the host table, in the plugin's own schema. Served publicly through the export surfaces below. |
| The legacy payload keys `character_name` and `sender_name`, still read by the web translator for un-migrated emitters | `internal/web/translate.go:25-26` (the `genericPayload` fields), consumed at `internal/web/translate.go:88-96` | `historical` | no | The same frozen value under two older key names. Migrating an emitter to the new key does **not** rewrite what earlier emits already stored. |
| `GameEvent.actor`, the rendered read-side of the three rows above | `api/proto/holomush/web/v1/web.proto:426-429` — documented as *"the DISPLAY NAME of the acting character, extracted from the event payload… For stable identity use `actor_id`, not this field"* | `historical` | no | Recomputed on every read, yet still `historical` by Rule 1: it is recomputed **from frozen bytes**. This is the row that makes Rule 1's write-path test necessary — a read-path test would classify it `live` and be wrong. |
| `published_scenes.participants_snapshot` — the frozen participant list of a published scene | `plugins/core-scenes/migrations/000008_scene_publication.up.sql:23` (`participants_snapshot JSONB`); written once at the PUBLISHED transition from `plugins/core-scenes/publish_snapshot.go:152` | `historical` | no | **Unauthenticated on read** through three of §3.3's four public export surfaces. See §5.4 — what this column stores today is not what its proto contract says. |
| `published_scenes.content_entries[].speaker` — the per-line speaker label of a published scene | `plugins/core-scenes/migrations/000008_scene_publication.up.sql:21` (`content_entries JSONB`); written at `plugins/core-scenes/publish_snapshot.go:375` and `plugins/core-scenes/commands.go:107` | `historical` | no | **Unauthenticated on read.** Same §5.4 caveat. |

#### Live — re-resolved on every read

| Capture site | Where the name is read from | Verdict | Reachable by rename | Consequence |
| --- | --- | --- | --- | --- |
| `characters.name` — the source of truth every live surface resolves against | `internal/store/migrations/000001_baseline.up.sql:71` | `live` | yes | The anchor row. A rename is a write here and nowhere else. |
| The whole §3 character-projection family — `CharacterInfo.name`, `CharacterSummary.name`, `CharacterDirectoryEntry.name`, `PresenceEntry`, and the v0.13 `PublicCharacter` / `OwnCharacter` / `AdminCharacter` replacements | §3.2's type-reachable table | `live` | yes | Projected from the character row per read. A rename is visible on the next read with no further work. |
| `SelectCharacterResponse.character_name` and its three siblings (§3.3's name-reachable scalar rows) | `internal/grpc/auth_handlers.go:352`, `:380`, `:413`, `:505` — each assigning from the freshly-read character row's `Name` | `live` | yes | A bare `string` on the wire, but resolved live, so a rename reaches it. |
| `ParticipantInfo.character_name` — the scene roster | `plugins/core-scenes/service.go:522-528`, `:534-538`, `:1504-1513` | `live` | yes | Resolved per read, best-effort. Today no name resolver is wired in the plugin, so it falls back to the character id — the proto documents that fallback (`api/proto/holomush/scene/v1/scene.proto:328-330`). Wiring a resolver later keeps it `live`; it does not move the verdict. |
| `PoseOrderEntry.character_name` — the pose queue | `plugins/core-scenes/poseorder.go:18-23`, populated at `:76` from the caller-supplied `names` map, which `plugins/core-scenes/service.go:2015` passes as `nil` today | `live` | yes | Same shape as the roster row: a per-read lookup with an id fallback. |

#### Explicitly checked and **not** a name-capture surface

Stated so that a later reader does not have to re-derive the negative:

| Candidate | Why it is not a member |
| --- | --- |
| `events_audit.rendering` (`internal/store/migrations/000052_events_audit_partition.up.sql:119`, `JSONB NOT NULL`) | Carries `RenderingMetadata` — `Category`, `Format`, `Label`, `DisplayTarget`, `SourcePlugin`, `SourcePluginVersion` (`internal/eventbus/types.go:127-134`). Verb-level presentation metadata; no character name and no actor field. |
| Every foreign key referencing `characters(id)` | They hold the **id**, which a rename does not touch. `players.default_character_id` (`:80`), `character_roles.character_id` (`:84`), `locations.owner_id` (`:99`), `objects.owner_id` (`:143`) — all in `internal/store/migrations/000001_baseline.up.sql`. Correct by construction; listed so the enumeration is visibly complete rather than visibly silent about them. |

### 5.3 Cross-listing: which capture site each public export surface serves

§3.2 requires that the public export surfaces appear in **both** §3 and §5 —
being in one table only is the defect the cross-listing exists to prevent. All
four are unauthenticated or near-unauthenticated read paths over rows this
section classifies `historical`.

| Public export surface (§3.3) | Serves which §5.2 capture site | Verdict inherited |
| --- | --- | --- |
| `holomush.web.v1.WebService.WebGetPublicSceneArchive` (`api/proto/holomush/web/v1/web.proto:345`) | `published_scenes.participants_snapshot` + `content_entries[].speaker` | `historical` |
| `holomush.web.v1.WebService.WebListPublishedScenes` (`api/proto/holomush/web/v1/web.proto:339`) | the same two columns, returned **in bulk** across every published scene | `historical` |
| `holomush.web.v1.WebService.WebDownloadPublicSceneArchive` (`api/proto/holomush/web/v1/web.proto:351`) | the same two columns, rendered to opaque `bytes` | `historical` |
| `holomush.web.v1.WebService.WebExportScene` (`api/proto/holomush/web/v1/web.proto:329`) | the **scene-log payload** rather than the publication columns — it renders live from `scene_log` rows (`plugins/core-scenes/export.go:103`, *"Read IC `scene_log` rows (live read, ORDER BY id ASC)"*), decoding each payload's actor field | `historical` |

The last row is the one worth reading twice. `WebExportScene` performs a **live
read** of the log — and the verdict is still `historical`, because Rule 1's test
is about the write path: a live read of frozen bytes returns frozen values.

### 5.4 The scene-archive rows say "name" and store an id

This is recorded rather than corrected, because it changes what a Phase-3 or
Phase-6 planner should expect to find.

**The proto contract says names.** `participants_snapshot` is documented as *"The
participant character names snapshotted at publish time"* at all three of its
declarations — `api/proto/holomush/scene/v1/scene.proto:873-874`, `:957-958`, and
`:1052-1053` — and `PublishedSceneEntry.speaker` as *"The speaking character's
display label for this line"* (`api/proto/holomush/scene/v1/scene.proto:821`).

**The implementation stores ids.** `ReadSceneMetaForSnapshot` populates the
participant list with `SELECT character_id FROM scene_participants`
(`plugins/core-scenes/publish_store.go:987-1002`), and its own type comment says
so outright: *"Name resolution is a follow-up; character IDs are the available
identity surface"* (`plugins/core-scenes/publish_store.go:956-960`). `speaker` is
assigned from `pl.ActorID` — the payload's actor id — at
`plugins/core-scenes/publish_snapshot.go:375` and
`plugins/core-scenes/commands.go:107`. The communication-content proto records
the same state from the other side: `actor_display_name` is *"Empty when name
resolution is deferred (scenes today)"* (`api/proto/holomush/comm/v1/comm.proto:25-27`).

**Three consequences, all normative.**

1. **The verdict does not change.** These rows are `historical` either way. What
   is frozen at the PUBLISHED transition stays frozen, whether the frozen bytes
   are an id or a name.
2. **The exposure today is smaller than the requirement assumes, and the exposure
   tomorrow is exactly as large.** PORTAL-03 and the source research both
   describe these surfaces as publishing names to anonymous readers. Today they
   publish ids. The moment the documented "follow-up" name resolution lands, they
   publish **frozen names with no update path** — which is why §4.4's rule that
   retire MUST NOT release the name is load-bearing prospectively and not only
   retrospectively.
3. **The follow-up MUST NOT backfill.** When name resolution lands, it applies to
   publications made from that point forward. It **MUST NOT** resolve names into
   already-written `participants_snapshot` or `content_entries` rows: that is the
   mass update Rule 3 forbids, wearing the costume of a data-quality improvement.

The proto-versus-handler disagreement itself is filed per
`.claude/rules/proto-doc-comments.md`, which requires an issue capturing the
mismatch and documenting the current behavior; this SPEC documents the current
behavior and does not change the schema.

### 5.5 The inventory adds no new denormalizations

**v0.13 adds no new name-capture surface.** No profile field, no media reference,
no admin row, and no notification payload copies a character display name. Where
a name is needed, it is resolved at read time through the projection functions of
§2.3.

**Any future surface that would store a display name instead of an id MUST add a
row to §5.2 first**, with a verdict, before the code that writes it lands. A
denormalization introduced without a row here is a surface that no rename, no
retirement, and no privacy change can reach, and that nothing in this document
says so — which is the precise failure this inventory exists to prevent.

## 6. Name Normalization Policy

This section is normative.

**Character-name normalization and player-username validation are TWO SEPARATE
POLICIES. They MUST NOT share an implementation, a validator, or a normalization
function.** Not a shared helper with a mode flag, not a common `normalizeName`
with a boolean, not one regex parameterized by caller.

They are separate because they answer different questions against different
threat models. A character name is an **in-world identity** that other characters
read and act on, so its adversary is a visually-identical impersonator and its
defense is Unicode-aware. A player username is an **out-of-character credential
handle** that nobody reads as identity, is already ASCII-only, and is already
backed by a real database constraint — so it needs no Unicode work and would only
acquire risk from having some. Merging them means one policy's relaxation
silently becomes the other's.

### 6.1 Character names

**Non-Latin scripts are permitted.** The platform is not English-only and this
section does not make it so. What is constrained is not the script but the
*confusability*.

#### 6.1.1 The normalization pipeline, in order

Order is part of the specification. Phase 2 implements these steps in exactly
this sequence:

| # | Step | What it does |
| --- | --- | --- |
| 1 | **NFKC normalization** | Unicode Normalization Form KC. Collapses compatibility variants — fullwidth `Ａ` (U+FF21) to `A`, the ligature `ﬁ` (U+FB01) to `fi`, superscripts and enclosed forms to their base — so two names that differ only in compatibility encoding become one string. |
| 2 | **Strip `Cf` format codepoints** | Remove every codepoint in Unicode general category `Cf` — zero-width joiner (U+200D), zero-width non-joiner (U+200C), the bidi overrides (U+202A–U+202E), the invisible-separator family. These render as nothing, so they are pure padding for producing two distinct strings that look identical. |
| 3 | **Whitespace canonicalization** | Trim leading and trailing whitespace; collapse each internal run to a single `U+0020`. This is the one step today's implementation already performs. |
| 4 | **Case folding for the uniqueness key** | Unicode full case folding, producing the stored normalized form §6.1.3 uses as the uniqueness key. **The display name is not case-folded** — the character keeps the capitalization the player chose. |

Steps 1–3 produce the **display name**. Step 4 additionally produces the
**uniqueness key**. Both are stored; see §6.1.3.

**A name whose normalized form is empty is REJECTED.** A submission consisting
entirely of whitespace, entirely of `Cf` codepoints, or of anything else the
pipeline removes normalizes to the empty string and **MUST** be rejected with a
validation error — never accepted, never stored, never silently defaulted.

#### 6.1.2 The confusable and mixed-script rule

Two mechanisms, because neither alone is sufficient.

**Mechanism A — the mixed-script restriction (catches cross-script splicing).**
Resolve the name's script set using Unicode script extensions (UTS #24), treating
codepoints of script `Common` and `Inherited` — spaces, punctuation, combining
marks — as script-neutral and excluding them from the set. Then:

| Resulting script set | Verdict |
| --- | --- |
| A single script | **Permitted** |
| Latin + any of Han, Hiragana, Katakana (Japanese) | **Permitted** |
| Latin + Han + Bopomofo (Chinese) | **Permitted** |
| Latin + Han + Hangul (Korean) | **Permitted** |
| **Latin + Cyrillic** | **Rejected** |
| **Latin + Greek** | **Rejected** |
| **Cyrillic + Greek** | **Rejected** |
| Any other combination of two or more scripts | **Rejected** |

This is UTS #39's **Moderately Restrictive** profile. It is named as a standard
profile rather than described freehand so that Phase 2 implements against a
specification rather than against a judgement call, and so a later reader can
check the implementation against the same document. The three explicitly-named
rejections are the confusable-rich pairs: Cyrillic `а` (U+0430) against Latin `a`
(U+0061), Greek `ο` (U+03BF) against Latin `o` (U+006F).

**Mechanism B — the skeleton check (catches whole-script confusables).**
Mechanism A alone passes a name written **entirely** in Cyrillic that renders
identically to an existing Latin name — it is single-script, so it is permitted.
The second mechanism closes that: compute the name's **confusable skeleton** per
UTS #39 §4 and **reject a create or rename whose skeleton equals the skeleton of
an existing character's name**.

**The skeleton is NOT the uniqueness key, and MUST NOT become one.** The
uniqueness key is the §6.1.1 normalized form; the skeleton is a separate stored
value backed by a **non-unique** index and checked by query. The reason is
version stability: the Unicode confusables table changes between Unicode
releases, so a skeleton computed today may differ from the same name's skeleton
after a library upgrade. A `UNIQUE` constraint whose meaning shifts under a
dependency bump is a migration hazard — existing rows can become retroactively
non-compliant with a constraint nobody edited. A query-time check degrades
gracefully under the same upgrade; a constraint does not.

**The Unicode version used to compute skeletons MUST be pinned and recorded**
alongside the stored skeleton, so a table upgrade is a deliberate, detectable
recomputation rather than a silent drift in what "confusable" means.

#### 6.1.3 The uniqueness key and the index that must land with rename

**The stored normalized name is the uniqueness key.** Phase 2 adds it as a column
on `characters` — the §6.1.1 pipeline's output through step 4 — with a **`UNIQUE`
index over it**. Uniqueness is a database constraint, not an application check.

**That index MUST land before or with `Rename`.** Not after. Today there is no
unique index on `characters.name` and no `LOWER(name)` index — the baseline
`characters` table (`internal/store/migrations/000001_baseline.up.sql:67-76`)
carries only `idx_characters_location` (`:76`). Uniqueness is enforced entirely by
a check-then-insert sequence with a window between the check and the write:

| Participant | Location | Role |
| --- | --- | --- |
| The existence query | `internal/bootstrap/setup/adapters.go:38-50` — `SELECT EXISTS(SELECT 1 FROM characters WHERE LOWER(name) = LOWER($1))` | The shared read half of every check-then-insert. Note it compares `LOWER(name)`, which is ASCII-oriented case folding over a column that permits any script. |
| Writer 1 — player character creation | `internal/auth/character_service.go:112-121` — `ExistsByName` then create | Races against itself under two concurrent creates. |
| Writer 2 — guest provisioning | `internal/auth/guest_service.go:227` — `ExistsByName` inside the retry-on-collision loop | Races against writer 1 and against itself. |
| Writer 3 — **`Rename`, not yet built** | Phase 3 | **Adding it triples the writers into a race that already has two.** |

Landing `Rename` without the index does not merely leave the existing race open;
it adds a third participant and a second write shape to it. That is why IDENT-09
sequences the index first, and why this SPEC restates the sequencing as normative
rather than leaving it to a requirement checkbox.

> **Correction to the requirement text.** `.planning/REQUIREMENTS.md` IDENT-09 and
> the source research both describe the race as spanning *two* sites, naming
> `internal/bootstrap/setup/adapters.go:38-50` and
> `internal/auth/character_service.go:112-121`. Those are the shared **query** and
> **one** writer. The tree carries a **second writer** —
> `internal/auth/guest_service.go:227` — which the enumeration above includes.
> §14 carries this as a queued amendment.

#### 6.1.4 The configurable block list

**Character names are additionally checked against a configurable block/disallow
list of regular expressions (IDENT-07).** The list is **evaluated server-side**,
against the **normalized** form, at **both create and rename**. A block list
enforced at create but not at rename is not a block list; it is a speed bump that
one extra RPC call walks around.

Client-side evaluation is not evaluation. The list **MUST NOT** be shipped to the
client as the enforcement point, whether or not it is also shipped for
convenience.

#### 6.1.5 What today's implementation does not do

Stated so Phase 2 knows it is **replacing** `NormalizeCharacterName`, not
extending it.

`NormalizeCharacterName` (`internal/world/validation.go:114-126`) performs
exactly two operations: `strings.Fields` (which trims and collapses whitespace)
and a per-word lowercase-then-title-case. It performs **no NFKC**, **no `Cf`
stripping**, **no case folding for a uniqueness key**, **no script analysis**, and
**no confusable detection**. It also conflates the display name with the
normalized form — the title-cased output is what gets stored and displayed, so a
player cannot choose their own capitalization today.

`ValidateCharacterName` (`internal/world/validation.go:69-105`) runs after it and
enforces UTF-8 validity, no leading/trailing or consecutive spaces, a rune-count
range, and `characterNameRegex` — `^[\p{L}]+( [\p{L}]+)*$`
(`internal/world/validation.go:60`), Unicode letters and single spaces only.

**One honest note about that regex:** because `Cf` codepoints are category `Cf`
and not `L`, the letters-and-spaces shape **already rejects** a name containing a
zero-width joiner today. So the tree is not currently open on that specific axis.
The pipeline still strips `Cf` in step 2 rather than relying on the rejection,
because the two are different guarantees: the regex refuses such a name, while
stripping guarantees that **no invisible codepoint survives into a stored display
name** even if the shape rule is later relaxed to permit apostrophes, hyphens, or
digits — a relaxation this SPEC neither makes nor forecloses. Defense that depends
on an unrelated rule staying strict is defense that expires without notice.

### 6.2 Player usernames

**The existing ASCII-only rule is unchanged.** v0.13 writes no new username
validation.

| Property | Value |
| --- | --- |
| The rule | `^[a-zA-Z][a-zA-Z0-9_]*$` — starts with a letter, then letters, digits and underscores (`internal/auth/player.go:31`, applied at `internal/auth/player.go:167`) |
| Length | 3 to 30 (`internal/auth/player.go:24-25`) |
| Uniqueness | A real database constraint — `username TEXT UNIQUE NOT NULL` (`internal/store/migrations/000001_baseline.up.sql:54`). No check-then-insert race exists here. |

**None of §6.1 applies to usernames. No NFKC, no `Cf` stripping, no case folding,
no script analysis, and no confusable folding.** The character set is ASCII, so
every one of those steps is either a no-op or a new failure mode with no threat to
answer.

**The v0.13 obligation is a regression guard, not new validation.** IDENT-08 is
discharged by a test that **pins the existing rule** — asserting that the
non-ASCII and leading-non-letter cases the regex rejects today are still rejected
— so that a future change to the character-name pipeline cannot quietly reach the
username path. That is the whole point of the pin: the guard exists to fail if
someone later "unifies" the two policies this section separates.

### 6.3 Pre-existing duplicates precede the index

**Duplicate normalized names may already exist in production data.** The
check-then-insert race described in §6.1.3 has been live for the whole life of the
`characters` table, and the existing check compares `LOWER(name)` rather than any
normalized form — so a pair differing only by NFKC-collapsible compatibility
characters was never even a candidate for rejection.

**Therefore the sequence is fixed:**

1. **Detect.** A read-only query enumerating every set of characters sharing a normalized name under the §6.1.1 pipeline.
2. **Resolve.** A one-shot job — separate from any migration — that resolves each collision.
3. **Then** add the `UNIQUE` index.

**The resolution MUST NOT run inside a migration.**
`.claude/rules/database-migrations.md` forbids long-running data backfills in
migrations, and this is one: it reads every character row, applies a Unicode
pipeline to each, and writes to the colliding ones. It is also, unlike a
migration, a step that can require a judgement call about which of two colliding
characters keeps its name — and a migration is the wrong place for anything a
human might need to decide.

Adding the index before the resolution is not an option: the index creation
simply fails on the first duplicate, at whatever moment the deployment reaches it.
Discovering the duplicates from a failed migration in a deployment window is the
same information the detection query gives for free, obtained at the worst
possible time.

## 7. Profile and Media Data Model

This section is normative. It fixes where profile data lives, what the complete
field set is, and how media is named.

### 7.1 The committed shape

**Every profile field and every media reference is an `entity_properties` row
under a `profile.` name prefix.** Intrinsic values stay columns on `characters`.

The split is:

| Lives as | What |
| --- | --- |
| A column on `characters` | The character's `name`; the in-world `description` (`internal/store/migrations/000001_baseline.up.sql:72`); the lifecycle `status` (added by Phase 2 per the §4 vocabulary); the optimistic-concurrency `version` (`internal/store/migrations/000049_world_version_guard.up.sql:20`, `version INTEGER NOT NULL DEFAULT 1`). |
| An `entity_properties` row | Every `profile.*` field in §7.2 and every media reference in §7.3. |

The rule separating them: a value the world model itself reads — to render a
`look`, to decide whether a character may be selected for play, to guard a
concurrent write — is a column. A value only the profile publishes is a row.

**This requires zero DDL for a twelfth field or an eleventh image.** That is what
makes the "no migration later" claim literally true rather than aspirational: the
`entity_properties` table already ships the whole mechanism
(`internal/store/migrations/000001_baseline.up.sql:350-371`) — `parent_type` /
`parent_id` / `name` / `value`, a per-row `visibility` CHECK over
`('public', 'private', 'restricted', 'system', 'admin')`, the `visible_to` /
`excluded_from` lists, and
`CONSTRAINT entity_properties_parent_name_unique UNIQUE(parent_type, parent_id, name)`
at `:364`. A new profile field is an `INSERT`. So is an eleventh image. Neither
is an `ALTER TABLE`, and neither is a migration.

Profile rows are addressed as `parent_type='character'`, `parent_id=<character
id>`, `name='profile.<field>'`.

### 7.2 The complete `profile.*` field set

**There are twelve `profile.*` text fields.** The count is stated here as a
number because Phase 5 selects its form approach from it — twelve fields is a
sectioned form, not a single-column stack, and that choice should not require
counting table rows by hand.

Every field's `value` is stored as `entity_properties.value` (`TEXT`, nullable).
The Type column below is the semantic shape a renderer should assume, not a
distinct storage type. No field is required — see §7.4.

| Property name | Type | Required | Description |
| --- | --- | --- | --- |
| `profile.pronouns` | short text, single line | No | The character's pronouns. Together with the character's name this is the minimum public identity, and the configuration cannot raise it above the profile's reachability floor (§8.8). |
| `profile.concept` | short text, single line | No | The one-line "what this character is" pitch. |
| `profile.species` | short text, single line | No | Species / race / kind, as the game's setting defines it. Free text — the platform ships no species vocabulary. |
| `profile.age` | short text, single line | No | Apparent or stated age. Free text, not an integer: settings routinely want "ageless", "early 30s", or a century. |
| `profile.faction` | short text, single line | No | Affiliation, house, crew, or allegiance. Free text; the platform ships no faction registry. |
| `profile.appearance` | long text, multi-paragraph | No | Extended appearance beyond the in-world description — detail a viewer would not get from a single `look`. |
| `profile.personality` | long text, multi-paragraph | No | Disposition and manner. |
| `profile.biography` | long text, multi-paragraph | No | History and background. |
| `profile.rumors` | long text, multi-paragraph | No | Rumors and RP hooks — the "reasons to approach this character" block (PROFILE-06). |
| `profile.currently` | short text, single line | No | The volatile "Currently" status line (PROFILE-07): what the character is up to right now. Expected to change often; carries no history. |
| `profile.rp_preferences` | long text, multi-paragraph | No | The **OOC** RP-preferences block (PROFILE-08): style, availability, content limits, walk-up-friendliness. Out-of-character text about the player's preferences, not about the character. |
| `profile.timezone` | short text, single line | No | Time zone, supporting the availability half of the OOC preferences (PROFILE-09). |

**Naming collision, called out deliberately.** `profile.rp_preferences` is **not**
`characters.preferences`. The latter is a shipped `JSONB` column
(`internal/store/migrations/000045_character_preferences.up.sql:5`) holding the
owner-partitioned **settings** scope — client and display settings. The OOC
RP-preferences block is published profile prose. Phase 5 **MUST NOT** write the
RP block into the settings column, and the property name carries the `rp_`
qualifier specifically so the two cannot be conflated by name alone.

### 7.3 Media naming

v0.13 ships **one primary image row and ten gallery rows**:

| Property name | Type | Required | Description |
| --- | --- | --- | --- |
| `profile.image.primary` | media reference | No | The single primary image. Exactly one per character, enforced by the database (below). |
| `profile.image.gallery.00` … `profile.image.gallery.09` | media reference | No | Ten gallery slots. The index is **two-digit zero-padded**, running `00` through `09` — never `0`, never `1`, never `10`. |

The zero-padded two-digit form is fixed, not a stylistic preference: property
names are compared as **exact bytes**. `profile.image.gallery.0` and
`profile.image.gallery.00` are two different rows that both coexist happily,
because the uniqueness constraint is over the byte string. There is no
normalization step anywhere in the read path that would collapse them, so the
format has to be fixed at specification time rather than discovered at insert
time.

**Exactly-one-primary is enforced by the database, not by application code.**
`CONSTRAINT entity_properties_parent_name_unique UNIQUE(parent_type, parent_id, name)`
(`internal/store/migrations/000001_baseline.up.sql:364`) makes a second
`profile.image.primary` row for the same character an insert error. No service
layer check is required, and none **SHOULD** be added — a hand-written check
beside a database constraint is a second source of truth that can disagree with
the first.

**v0.13 ships the schema and the proto shape only, with zero upload behavior.**
There is no uploader, no storage backend, and no media-serving path. The proto
carries `ProfileImage{media_id, alt_text, content_warning}` plus `primary_image`
and `repeated gallery [max_items = 10]` so that alt-text and content-warning have
somewhere to live before moderation exists (EXT-06). The model is proven in
v0.13 by inserting one primary and ten gallery rows through the real schema
(EXT-05) — demonstrating the no-migration-later claim rather than asserting it.

### 7.4 The in-world description is always public on the profile

The in-world description (`characters.description`, the `look` text) **is always
public on the profile, with no per-owner control.**

The reasoning is about what the existing gate actually means.
`seed:player-character-colocation` (`internal/access/policy/seed.go:51-54`)
permits a character to read a co-located character:
`when { resource.character.location == principal.character.location }`. That
gates **where a viewer has to be standing** — it does not gate **who may know**.
Treating it as a privacy boundary would retrofit a meaning it never carried. The
web surface removes the co-location *requirement*; it does not remove a privacy
control, because there was never a privacy control there to remove.

**The consequence is stated here so no phase can treat it as a formality:**
PROFILE-11's audit of existing character descriptions is now the **only** gate on
that text. The seed policy that permits off-location profile reads widens read
access to every existing description at once. That audit is a precondition of
shipping the policy, not a checkbox beside it.

The description's floor is configurable like any other governed attribute (§8.6)
— "always public on the profile" means it carries no *per-owner* control and no
paired per-row visibility property, not that a game cannot raise its floor.

### 7.5 The empty profile

A character with **zero** `profile.*` rows still resolves a profile. That profile
carries the character's name and pronouns, per the §8.8 minimum-identity floor.
A profile read **MUST NOT** return a not-found for a reachable character merely
because no profile rows exist.

A blank field **MUST be omitted from the response**, never emitted as an empty
value. This is the same absence-not-emptiness discipline §8.9 applies to withheld
fields, and it is deliberate that the two cases are indistinguishable to a
viewer: a field the character left blank and a field the viewer may not see
**MUST** look identical on the wire. If they differed, the response shape itself
would disclose which fields exist but are withheld.

A renderer therefore has exactly two states per field — present, or absent — and
**MUST NOT** infer anything from absence beyond "do not render this."

## 8. Profile Visibility Model

This section is normative. It governs what a web viewer may read from a
character's profile, and it is the sole authority on that question in v0.13.

### 8.1 No player or character agency

There is **no player or character agency over web profile visibility in v0.13.**
The game configuration is the sole visibility authority.

The system **MUST NOT** ship an owner-facing per-field public/private toggle, and
**MUST NOT** ship any other per-character or per-player control over what the
profile publishes. A player who wants something unpublished does not write it
into the profile.

This is stated as a prohibition rather than left to omission because a Phase-5
planner reading a list of profile fields would otherwise reasonably infer that
each one needs a visibility control beside it in the form. It does not. There is
no toggle to build.

> **Notably absent:** there is no `profile.*.visibility` owner-settable property,
> no `SetProfileFieldVisibility` RPC, and no visibility column in any v0.13
> profile form. The reviewer for this SPEC **MUST** verify that no future PR in
> this milestone adds one.

### 8.2 The viewer-tier ladder

Visibility is expressed as a **viewer-tier floor**. The ladder is exactly three
rungs, in this order:

```text
anonymous  <  guest  <  player
```

| Tier | Means |
| --- | --- |
| `anonymous` | No session at all — a logged-out web visitor, or a crawler. |
| `guest` | A guest session exists (the weakly-authenticated tier the platform already models). |
| `player` | An authenticated player session. |

The ladder is represented as a **string enum**, so adding a fourth rung later is
an append rather than a renumbering. v0.13 defines exactly these three; no other
token is a tier.

A viewer **clears** a floor when the viewer's own tier is at or above it. A floor
of `anonymous` is therefore readable by everyone; a floor of `player` is readable
only by an authenticated player.

### 8.2.1 Clearing a floor is set membership, never string ordering

"At or above it" is English. This subsection fixes its ABAC form, because the two
obvious translations differ and one of them fails open.

**The clearing test MUST be expressed as explicit set membership over the tier
token.** It **MUST NOT** be an ordinal string comparison — `>=`, `>`, `<`, `<=` on
the tier — and it **MUST NOT** be a numeric rank attribute derived from the enum.

Each floor names the exact set of tiers that clear it, and the policy carries that
set literally:

| Floor | The clearing test, verbatim |
| --- | --- |
| `anonymous` | `principal.viewer.tier in ["anonymous", "guest", "player"]` |
| `guest` | `principal.viewer.tier in ["guest", "player"]` |
| `player` | `principal.viewer.tier in ["player"]` |

**Why ordinal comparison is forbidden rather than merely discouraged.** The DSL's
only string ordering is byte order: `compareStrings`
(`internal/access/policy/dsl/evaluator.go:185-201`) implements `>=` as Go's
`l >= r` on the raw strings. The three v0.13 tokens sort in ladder order —
`anonymous` then `guest` then `player` — **by alphabetical accident, not by
design.** §8.2's own next move is the trap: adding a fourth rung later is an
append. Each of `spectator`, `unverified` and `visitor`
sorts lexicographically above `player`, so a `>=` test would hand a newly appended
rung the highest clearance in the system on the day the token is added, silently,
with no policy edit anywhere.

**Why not a numeric rank attribute.** A numeric rank reintroduces the ordering in a
second place: the enum and the rank table become two sources of truth for one
ladder, and a mis-assigned rank fails open in exactly the same direction as byte
order. Set membership has no second source to drift from.

**The cost is the feature.** Appending a fourth rung means editing N clearing sets
— one per governed row of §8.6 — and until someone does, the new token clears
**nothing**. That is fail-closed on append. It forces an explicit re-decision per
floor instead of a silent widening, which is precisely the property byte-order
comparison destroys.

**Phase 2 obligation.** Phase 2 **MUST** ship a test that introduces a synthetic
fourth tier token sorting lexicographically above `player` — `spectator` is the
worked example — and asserts a viewer at that tier does **not** clear a `player`
floor. The test **MUST** be demonstrated RED against an ordinal-comparison
implementation of the clearing test before the set-membership implementation
lands. A fourth-rung test never observed failing cannot distinguish the two
implementations, and it is the only assertion in the suite that can tell them
apart at all.

### 8.3 The floor is game-wide and per-attribute

The floor is **game-wide and keyed by attribute name**. One configuration governs
the whole game; **every character is governed identically**.

The system **MUST NOT** provide a per-character visibility override — neither
player-set nor admin-set. Two properties motivate this. First, a per-character
override costs a second lookup on every projection. Second, and decisively, an
override that outlives the reason it was created becomes invisible policy: it is
readable nowhere the game's stated posture is readable, and it silently
contradicts it.

The floor is **not** modelled per-character-pair, per-viewer-identity, or as
IC-knowledge visibility. Those model who *knows* what about whom, which is
knowledge modelling wearing a privacy hat. The tier floor models what the *game*
publishes to the *web*, which is a different question with a different answer.

### 8.4 The floor is an ABAC policy family

The floor lives as an **ABAC policy family extending the existing
`seed:property-*` family** (`internal/access/policy/seed.go:110-145` — the six
shipped property policies, each a `permit`/`forbid` over
`resource is property`), overridden by rows in `access_policies` carrying
`source='admin'` — a value the shipped CHECK vocabulary already models
(`internal/store/migrations/000001_baseline.up.sql:261`,
`CHECK (source IN ('seed', 'lock', 'admin', 'plugin'))`).

Therefore the configuration requires **zero new storage** and introduces zero new
concepts.

The visibility decision **MUST** be made by the default-deny ABAC engine. It
**MUST NOT** be moved into projection code, facade code, or a settings-store
value that projection code consults. A settings-store configuration consumed by a
projection would relocate an authorization decision out of the engine — the exact
pattern ADMIN-01 exists to prevent.

### 8.4.1 The viewer principal

§8.5 grounds the family's *resource* side. This subsection fixes its *subject*
side, which is not optional detail: the engine rejects a subject it cannot parse,
and an `anonymous` web viewer has no in-game identity to present.

**Subject form, per rung.** Every tier-floor evaluation presents a subject in the
`viewer:` namespace:

| Rung | Subject string |
| --- | --- |
| `anonymous` | `viewer:anonymous` |
| `guest` | `viewer:guest:<player-ulid>` |
| `player` | `viewer:player:<player-ulid>` |

All three parse. `SplitN(subject, ":", 2)` yields type `viewer` and a non-empty
id, which is what `validateRequest` (`internal/access/policy/engine.go:550-573`)
and `CanPerformAction` (`internal/access/policy/engine.go:418-425`) require —
either rejects a subject whose type or id is empty with `INVALID_ENTITY_REF`. The
anonymous rung has no player identifier and carries the literal token instead.

**The `viewer:` namespace is new, and its newness is the point.** The two subject
forms a Phase-2 implementer would otherwise reach for are both wrong, and both
fail **open**:

- **`character:<id>`** — the acting character. This activates
  `seed:admin-full-access` (`internal/access/policy/seed.go:104-109`,
  `permit(principal is character, action, resource)` with no resource guard at all)
  and `seed:player-character-colocation` (`internal/access/policy/seed.go:50-55`).
  Both are `principal is character`, so for those viewers the ladder is bypassed
  entirely and the profile answer comes from grid semantics.
- **`player:<id>`** — carries `player.grants` and the crypto-operator allow-list
  (`internal/access/policy/attribute/player.go`), an unrelated authorization axis,
  and has no representation for the anonymous rung at all.

Every shipped seed is `principal is character`, `principal is plugin` or
`principal is system` (`internal/access/policy/seed.go`). A `viewer:` subject
therefore matches **no** existing policy, which is what makes the tier-floor family
the only thing that can permit a profile read — and makes the default-deny floor
genuinely the floor.

**The attribute namespace and the provider that supplies it.** The tier arrives as
`principal.viewer.tier`. The resolver namespaces a provider's un-namespaced keys at
merge time under that provider's `Namespace()`
(`internal/access/policy/attribute/resolver.go:52-74`), so `tier` is supplied by a
new **`ViewerTierProvider`** whose `Namespace()` returns `"viewer"`, following the
shape of `PlayerAttributeProvider`
(`internal/access/policy/attribute/player.go:80-107`).

`ViewerTierProvider.ResolveSubject` parses the post-`viewer:` remainder and emits:

| Key | Type | Presence |
| --- | --- | --- |
| `tier` | string | **Always** — exactly one of `anonymous`, `guest`, `player`. |
| `player_id` | string | Only on the `guest` and `player` rungs. |
| `has_player_id` | bool | Always, true or false on every code path. |

`player_id` follows the omit-don't-sentinel rule
(`.claude/rules/abac-providers.md`, ADR holomush-ti1b): on the anonymous rung the
key is **absent**, never `""`. An empty-string sentinel satisfies `"" == ""`
against any other unresolved peer attribute and creates a fail-open match in a
default-deny system. The `has_player_id` witness is emitted on every path, so a
policy that needs to distinguish absent from present has something to test.

`ResolveResource` returns `(nil, nil)` — a viewer is a Subject, never a Resource.
All three keys **MUST** be declared in `ViewerTierProvider.Schema()`: the resolver
drops and counts (`abac_rejected_provider_attributes_total`) any provider attribute
whose key is not in the namespace schema, so an undeclared `tier` is silently
absent rather than loudly wrong.

**The tier is server-derived.** The token **MUST** be derived from the server-side
session state the gateway already authenticated. It **MUST NOT** be read from a
client-supplied header, query parameter, cookie value or request field. The subject
string is built at the facade, in the same trust position as `access.PlayerSubject`
(`internal/access/prefix.go:82-94`) occupies today.

**Phase 2 obligations.** Phase 2 **MUST**:

1. **Register `ViewerTierProvider` in `BuildABACStack`**
   (`internal/access/setup/setup.go:108-262`), alongside the other
   `resolver.RegisterProvider` calls. An unregistered namespace does not error:
   `principal.viewer.tier` is simply absent from the bag, every condition
   referencing it evaluates false, and the whole family silently default-denies in
   production while a unit test that stubs the bag stays green. The corpus sweep
   `warnOnMissingSeedCoverage` (holomush-xxel) WARNs at construction for a
   namespace referenced by a seed but not registered; Phase 2 **MUST** confirm that
   WARN does not fire for `viewer`.
2. **Add `SubjectViewer = "viewer:"`** to the subject-prefix constants **and to
   `knownPrefixes`** (`internal/access/prefix.go:12-61`), extending the
   known-prefix table test. `access.ParseEntityRef`
   (`internal/access/prefix.go:196-220`) returns `INVALID_ENTITY_REF` for an
   unlisted prefix.
3. **Ship a `ViewerSubject` constructor** beside `PlayerSubject` and
   `CharacterSubject` (`internal/access/prefix.go:63-94`), panicking on an empty
   identifier, so no call site builds the subject string by concatenation.

### 8.4.2 Profile reachability is its own resource

The *profile reachability* row of §8.6 is neither an `entity_properties` row nor an
attribute name. §7.1 puts `name` and `description` in `characters` columns, and
reachability governs whether the profile resolves at all rather than what one field
publishes. The name-keyed family of §8.5 has no key that can match it, so
reachability carries its own resource, its own action and its own policy.

| Element | Value |
| --- | --- |
| Resource | `profile:<character_id>` — a distinct resource type, **not** `character:<id>` |
| Action | `read` |
| Principal | the `viewer:` subject of §8.4.1 |
| Seeded policy | `seed:profile-reachable` |

The seeded v0.13 policy, expressing §8.6's `anonymous` reachability floor:

```text
permit(principal is viewer, action in ["read"], resource is profile)
  when { principal.viewer.tier in ["anonymous", "guest", "player"] };
```

Raising the reachability floor is an edit to that clearing set and nothing else.
The policy reads no resource attributes, so it needs no `profile`-namespace
AttributeProvider: `resource is profile` is a target match on the parsed resource
type (`parseEntityType`, `internal/access/policy/engine.go:542-548`).

**Why not `character:<id>` with action `read`.** That pair is already permitted for
character subjects by `seed:admin-full-access` and
`seed:player-character-colocation` (`internal/access/policy/seed.go:50-55`,
`:104-109`). More fundamentally it means "may read the character entity", which is
a different question from "does this character's profile resolve on the web".
Reusing it would let a future change to co-location or admin semantics move the
reachability floor without anyone editing the visibility configuration.

**Reachability is evaluated independently.** It is its own `Evaluate` call. Its
result **MUST NOT** be derived from any per-field result, and no per-field result
**MUST** be derived from it beyond the ordering fixed below. Deriving reachability
from "did any field clear its floor" pins it permanently at `anonymous` under the
seeded defaults of §8.6 — `name` sits at `anonymous`, so something always clears —
the §8.7 not-found-equivalent can then never fire, and **INV-PRIVACY-9 binds to a
gate that cannot deny**. That is the false-green the registry's binding ratchet
exists to catch, arrived at by construction rather than by a bad annotation.

**Order.** Reachability is evaluated **first**. A DENY returns the §8.7
not-found-equivalent, and no per-field evaluation runs.

**Phase 2 obligation.** Add `ResourceProfile = "profile:"` to the resource-prefix
constants and to `knownPrefixes` (`internal/access/prefix.go:21-61`), with a
`ProfileResource` constructor beside the others.

### 8.5 The floor is evaluated at read time

The floor **MUST** be evaluated **at read time against the attribute name**. It
**MUST NOT** be stamped onto `entity_properties.visibility` per row from the
configuration.

`profile.*` rows carry a uniform per-row `visibility` value; the tier-floor
policy family evaluates the pair *(attribute name, viewer tier)* on each read. A
configuration change therefore takes effect on the **next read, with no data
migration and no backfill**.

The rejected alternative — stamping each row's `visibility` from the
configuration — would require a one-shot backfill across every profile row of
every character on every configuration change. Migrations forbid in-migration
backfills, so that alternative buys a maintenance job in exchange for nothing.
Read-time evaluation is what makes "entirely configurable" actually cheap.

**Phase 2 obligation.** Phase 2 **MUST** confirm the property name is reachable
as an ABAC resource attribute before seeding the family. It is reachable today:
`PropertyProvider.ResolveResource` emits `attrs["name"] = prop.Name`
(`internal/access/policy/attribute/property.go:80-86`), alongside `parent_type`,
`parent_id` and `visibility`. Phase 2 confirms this still holds at the version it
builds against rather than inheriting the claim.

### 8.5.1 The tier floor composes conjunctively with the row-keyed decision

**A profile attribute is published only when BOTH decisions permit: the viewer
clears the attribute's tier floor, AND the underlying `entity_properties` row's own
`visibility` / `visible_to` / `excluded_from` permits the read. Either one denying
denies the read.** The composition is **conjunctive**, and it is the caller that
ANDs the two — it is **not** the engine's own permit combining, and the tier-floor
family **MUST NOT** be implemented as one more permit inside the same evaluation as
the shipped `seed:property-*` family.

Stating this is load-bearing because the engine combines permits **disjunctively**:
`combineDecisions` (`internal/access/policy/engine.go:591-611`) returns the first
satisfied forbid, otherwise the first satisfied permit — so any satisfied permit
permits. A tier-floor permit is keyed on the attribute **name** and never inspects
the row. Dropped into the same evaluation it would be **additive to** the row-keyed
family rather than **conjunctive with** it: a `profile.` row carrying
`visibility='private'` or `'admin'` would be published to every viewer that clears
that name's floor, because the tier permit is satisfied and nothing forbids. Only
`restricted` plus `excluded_from` would survive, and only because it is the
family's one `forbid` (`internal/access/policy/seed.go:140-145`).

The exposure that shape creates is concrete rather than theoretical.
`seed:property-owner-write` (`internal/access/policy/seed.go:128-133`) lets a
character owner write **any** property on their own character, and §7.1 makes a new
profile field an `INSERT` — an open namespace an owner or a plugin can write into
at will. An additive tier-floor permit publishes those rows to the open web.

Two evaluations, ANDed by the caller. Not one evaluation with two permits in it.

#### 8.5.1.1 Phase-2 obligation: term B's request shape, and why it MUST NOT be removed

This section fixes the **composition** but deliberately does not fix the second
evaluation's **request shape**, which Phase 2 MUST settle before seeding.

The hazard is specific. All six shipped property policies are
`principal is character` (`internal/access/policy/seed.go:110-145`). Term B issued
with §8.4.1's `viewer:` subject therefore matches **zero** policies and resolves
`EffectDefaultDeny` (`internal/access/policy/engine.go:591-611`), making `A AND B`
permanently false: **no profile attribute publishes to anyone.**

That is fail-closed, so it leaks nothing. The danger is the *repair*. Confronted
with a profile that renders empty for every viewer, the cheapest-looking fix is to
drop term B — which restores precisely the additive-permit exposure this section
exists to close, and does so quietly, because the symptom it relieves looks like a
bug and the hole it reopens has no symptom at all.

**Removing term B is a normative violation of this section, not a tuning decision.**
Phase 2 MUST instead give term B a shape that can match: either viewer-flavored
row-keyed policies alongside the existing character-flavored ones, or an explicit
rule that term B evaluates against a co-located character subject where one exists
and **DENIES** where one does not. Whichever is chosen, the conjunction stands.

Identified by `abac-reviewer` at the Phase 1 hand-off gate (2026-08-01) as the one
residual after the four blocking §8 findings were closed.

### 8.6 The configured postures

The table below is the whole configuration surface. Its rows are the governed
attribute names; the three posture columns are worked examples of the same table
expressing three different games; the final column is what v0.13 seeds.

| Governed attribute | Scrape-friendly game | Guest-floor game | Players-only game | **Seeded v0.13 default** |
| --- | --- | --- | --- | --- |
| *profile reachability* | `anonymous` | `guest` | `player` | **`anonymous`** |
| name (`characters.name`) | `anonymous` | `guest` | `player` | **`anonymous`** |
| `profile.pronouns` | `anonymous` | `guest` | `player` | **`anonymous`** |
| in-world description (`characters.description`) | `anonymous` | `guest` | `player` | **`anonymous`** |
| `profile.rumors` | `anonymous` | `guest` | `player` | **`guest`** |
| `profile.currently` | `anonymous` | `guest` | `player` | **`guest`** |
| `profile.rp_preferences` | `anonymous` | `guest` | `player` | **`guest`** |
| `profile.timezone` | `anonymous` | `guest` | `player` | **`guest`** |
| `profile.concept` | `anonymous` | `guest` | `player` | **`guest`** |
| `profile.species` | `anonymous` | `guest` | `player` | **`guest`** |
| `profile.age` | `anonymous` | `guest` | `player` | **`guest`** |
| `profile.faction` | `anonymous` | `guest` | `player` | **`guest`** |
| `profile.appearance` | `anonymous` | `guest` | `player` | **`guest`** |
| `profile.personality` | `anonymous` | `guest` | `player` | **`guest`** |
| `profile.biography` | `anonymous` | `guest` | `player` | **`guest`** |
| `profile.image.primary` | `anonymous` | `guest` | `player` | **`guest`** |
| `profile.image.gallery.00` … `profile.image.gallery.09`, each name a row | `anonymous` | `guest` | `player` | **`guest`** |

The media line is eleven rows, not a pattern: §7.3 fixes the eleven names as exact
bytes, so the set is closed and enumerable. `profile.image.gallery.10` is **not** a
member — it is an unenumerated name, and by the rule below it is denied.

Read the postures as columns, not as three configurations: it is **one** table,
and a game picks a floor per row. The three columns exist to show that the same
mechanism expresses a game that wants a character fully scrapable by anonymous
visitors, a game that wants guests as the floor for most things, and a game that
wants authenticated players as the floor for everything.

**Totality rule.** Every governed attribute **MUST** carry an explicit floor, and
the rows above are an **exact enumeration of attribute names**, matched as whole
strings. The tier-floor family **MUST NOT** contain a glob, prefix, wildcard or
catch-all pattern over `profile.*`. An attribute name appearing in no row above
**is denied, not defaulted** — there is no residual floor for it to fall back to.

**This supersedes the earlier formulation, and the direction of the change is the
correction.** The rule previously read: any `profile.*` attribute not individually
assigned a floor would default to `guest`, never to `anonymous`. That was written
for a closed field list, where `guest` is a tightening. Over the namespace §7.1
actually ships it is not. §7.1 makes a new profile field an `INSERT`, so the
namespace is open and an owner or a plugin can write into it
(`seed:property-owner-write`, `internal/access/policy/seed.go:128-133`); and the
floor is reached by a **name-keyed permit**, so a residual `guest` default is a
permit for a name nobody has ever considered. That is *more permissive than the
engine's own default-deny* — the wrong direction — and it means a row inserted
outside this table is published to every guest on the public web the moment it
exists.

The corrected rule keeps the original intent exactly (adding a profile field
**MUST NOT** silently publish it) and fixes the mechanism that was supposed to
carry it. An unset floor still **MUST NOT** be read as "allow"; it is now read as
nothing at all, which the engine already resolves as deny.

Adding a field to §7.2 therefore means adding a row here in the same change. That
is friction by design, and it is the same shape as §3's census rule 3: the correct
response is to add the row, never to widen the match.

*Note on the profile-reachability row.* Reachability is a facet **above** the
fields: it governs whether the profile resolves at all, and it is evaluated
before any per-field floor. It is not an attribute name and not a property row —
§8.4.2 gives it its own resource, action and seeded policy, and fixes that it is
evaluated independently of every per-field result. §8.7 and §8.8 constrain it.

*Note on the two `characters`-column rows.* Name and the in-world description are
columns, not `entity_properties` rows (§7.1), so they carry a tier floor but no
row-keyed peer decision — §8.5.1's conjunction has only one term for them.

### 8.7 Unreachable profiles are opaque

A profile the viewer cannot reach **MUST** return a **not-found-equivalent whose
wire shape is indistinguishable from that of a nonexistent character**.

It **MUST NOT** return a "this profile is private" signal, a distinct error code,
a distinguishable status, or a response whose size or timing separates the two
cases in an obvious way. A distinct "private" response discloses that the
character exists, which is the fact the floor was set to withhold.

### 8.8 Name and pronouns are a hard floor

**If a viewer can reach the profile at all, that viewer sees `name` and
`pronouns`.**

The configuration **MAY** set the profile's own reachability floor as high as it
likes — including `player`, making the profile invisible to the entire public
web. It **MUST NOT** raise `name` or `pronouns` above that reachability floor.

This is a constraint on the configuration, not a configurable attribute. It
guarantees that every profile a viewer can reach is non-empty, and it guarantees
the initial-letter avatar placeholder always has a letter to render.

**What enforces this, and what does not — recorded, not redesigned around.** v0.13
ships no mechanism that enforces this constraint against a configuration that
violates it deliberately. The engine is deny-overrides
(`combineDecisions`, `internal/access/policy/engine.go:591-611`), so an
admin-authored `forbid` row carrying `source='admin'` beats the seeded permit: an
operator writing one against `name` or `pronouns` puts them out of reach of a
viewer who reaches the profile, and nothing in the system objects. That failure is
**closed** — the viewer sees less, never more — so it is not a disclosure hole and
it is not a defect to fix here. It is recorded because INV-PRIVACY-10 is phrased as
a system guarantee while in fact resting on **operator discipline**, and a reader
who assumes a mechanism enforces it would be wrong about the shape of the
protection. Nothing is exposed in v0.13: §8.12 ships no editing surface, so the
only way to author such a row is a direct write to `access_policies`.

### 8.9 Enforcement is by absence, not by emptiness

An attribute whose floor the viewer does not clear **MUST be absent from the
marshaled response**. It **MUST NOT** be present-and-empty.

This is the same discipline `.claude/rules/abac-providers.md` mandates for ABAC
attribute providers — *omit, do not sentinel* — expressed one layer out, at the
wire. The rule exists there because an empty-string sentinel satisfies `"" == ""`
against any other unresolved peer and creates a fail-open match in a default-deny
system. It applies here for the same reason plus a plainer one: a present-empty
field is indistinguishable from a field the character left blank, so the response
itself discloses that a withheld value exists.

Enforcement **MUST NOT** be client-side hiding.

### 8.10 Infrastructure failure resolves DENY

An infrastructure failure in the tier-floor lookup — the policy store is
unreachable, the attribute provider errors, the engine cannot evaluate —
**MUST resolve DENY**. It **MUST NOT** resolve permit, and it **MUST NOT** be
silently swallowed into "this viewer sees nothing".

The shipped precedent is `world.Service.ListPropertiesByParent`
(`internal/world/service.go:1144-1171`), whose filter loop distinguishes three
outcomes: a nil error appends the property, `ErrPermissionDenied` filters it
silently, and `ErrAccessEvaluationFailed` **aborts the whole call**. The third
branch is the one that matters here — masking an infra failure as "no visible
properties" produces a profile that renders as legitimately sparse when in fact
nothing was evaluated. The tier floor **MUST NOT** become the one place in this
system where a lookup failure reads as permissive.

### 8.11 Recorded divergence from strict grid-parity

The divergence below is intentional. It is listed with its rationale, and the
reviewer is expected to evaluate it as a deliberate choice, not a defect.

| Source text | v0.13 SPEC | Rationale |
| --- | --- | --- |
| Stated principle: *"anything readable in the same location 'on grid' is visible to other logged-in users on the web."* Strict grid-parity places the in-world description at the `player` floor, because reading it on grid requires a logged-in character standing in the same location. | The seeded default places the in-world description at **`anonymous`** — more open than strict grid-parity. | Grid-parity is the **floor the principle guarantees, not a ceiling on what a game may publish.** An open default is what makes a shareable profile URL worth having: a profile that renders blank to every logged-out visitor is not a profile page, it is a login wall. A game wanting strict grid-parity raises the in-world description to `player` in configuration — one row of the §8.6 table, no code change. |

Recording this matters because the divergence is otherwise indistinguishable from
an oversight, and the correct response to an oversight (tighten it) is the
opposite of the correct response to this (leave it, and document the knob).

### 8.12 What ships in v0.13

v0.13 ships the **model plus the seeded defaults only.** Phase 1 specifies the
model and the defaults; Phase 2 lands the tier-floor policy family alongside the
ABAC vocabulary it already owns.

> **Notably absent:** there is **no editing surface** for the visibility
> configuration in v0.13 — no admin form, no RPC, no command. Changing the floor
> in v0.13 means changing the seeded policy set. The editor arrives when the
> `config` admin section — already registered, role-gated, and returning
> `NOT_IMPLEMENTED` *after* the gate per EXT-01/EXT-02 — gets its handler body,
> which is a body replacement rather than new wiring. The reviewer for this SPEC
> **MUST** verify no v0.13 PR adds an editing surface ahead of it.

## 9. RPC Surface

This section is normative. It fixes the whole new RPC surface v0.13 adds, the
concurrency contract every mutation carries, the update-mask rules, the error
surface, and the media proto shape that ships now and empty.

A Phase-4 planner writes every new request and response message from this section
without choosing a field type.

### 9.1 Where the surface lives, and what it is not

**Every new surface is a typed RPC on the character facade. None of them is a
command.**

The facade is `holomush.characteraccess.v1.CharacterAccessService`, built to the
shape `holomush.sceneaccess.v1.SceneAccessService` already ships
(`api/proto/holomush/sceneaccess/v1/sceneaccess.proto`), with `Web*`-prefixed
proxies on `holomush.web.v1.WebService`. The proxies translate protocol; the
facade holds the decisions.

`.claude/rules/gateway-boundary.md` §"Structural writes use typed RPCs, not the
command path" is the governing rule, and it is unambiguous: a GUI button or form
is a **machine-initiated structural write** and MUST reach a typed RPC. The
command path — `HandleCommand` / `sendCommand` — is reserved for **human
conversational verbs typed into a terminal** (`pose`, `say`, `join`). Creating,
renaming, retiring, or editing a character from a web form is structural. None of
it may be string-built into a command.

The rule is restated here rather than cited-and-left because the anti-pattern is
cheap and available: `sendCommand` already exists, already works, and already
reaches the core. Routing a form submission through it would send a
machine-initiated mutation through the human text-command parser — the exact
shape ADR `holomush-v4qmu` records as rejected. **If a required operation has no
typed RPC in this section, the correct response is to add the RPC, not to reach
for the command path.**

Two further boundaries hold throughout:

- **The gateway computes nothing.** `internal/web/` translates protocol and holds
  gRPC clients. Every audience decision, every tier-floor evaluation, and every
  authorization decision is core-side.
- **Projection, not assembly.** Every character-shaped response message on this
  surface is built by one of §2.3's three projection functions. No handler in
  this section constructs one by struct literal.

### 9.2 The read surface

| RPC | Caller | Audience | Gate | Description |
| --- | --- | --- | --- | --- |
| `CharacterAccessService.ListCharacterDirectory` | any web reader | `public` | tier floor (§8.4) on the directory resource | The public character directory. Returns `repeated PublicCharacterSummary` — identity only. Replaces `WebListAllCharacters` (§2.4). |
| `CharacterAccessService.GetCharacterProfile` | any web reader | `public` | profile reachability floor, then per-attribute floors (§8.6) | The public profile read. Returns `PublicCharacter` plus the viewer-filtered `profile.*` slice. Below the reachability floor it returns the §8.7 not-found-equivalent. |
| `CharacterAccessService.ListMyCharacters` | authenticated player | `owner` | session resolution + ownership | The owner's own roster, including retired characters with their lifecycle status (§4.5 property 1). |
| `CharacterAccessService.GetMyCharacter` | authenticated player | `owner` | session resolution + ownership | One owned character in full, for the edit surfaces. Returns `OwnCharacter`. |
| `CharacterAccessService.AdminListCharacters` | admin | `admin` | ABAC on `admin_section:characters` (§10.4) | The rich administrative list. Returns `repeated AdminCharacter`. |
| `CharacterAccessService.AdminSearchCharacters` | admin | `admin` | ABAC on `admin_section:characters` | Search over the administrative list. Bounded by §11's field list. |
| `CharacterAccessService.AdminGetCharacter` | admin | `admin` | ABAC on `admin_section:characters` | One character's administrative detail. Returns `AdminCharacter`. |

Each row's web proxy carries the same audience, the same gate, and the same
response message under a `Web`-prefixed name —
`WebListCharacterDirectory`, `WebGetCharacterProfile`, `WebListMyCharacters`,
`WebGetMyCharacter`, `WebAdminListCharacters`, `WebAdminSearchCharacters`,
`WebAdminGetCharacter`. The proxy pair is a **census pair**: §3's inventory
already lists core-side and web-side twins as separate members
(`CoreService.ListCharacters` beside `WebService.WebListCharacters`), and these
follow that precedent. Both halves of every pair are census members under §3.4.

**The profile URL is keyed on the character id, never on the name.** A stable
profile URL (PROFILE-01) cannot be keyed on a mutable value: §6.1 makes the name
renameable, and §4.4 makes it releasable by the purge path — so a name-keyed URL
would break on rename and, worse, would silently resolve to a **different
character** after a purge frees the name and a new character claims it. The id is
stable for the row's whole life and is released only when the row is.

> **Notably absent:** there is **no** name-keyed profile lookup RPC, and no
> `GetCharacterProfileByName`. A future rostering feature (backlog 999.6) that
> wants name resolution MUST answer the reclaim question above before adding one.
> The reviewer for this SPEC **MUST** verify no v0.13 PR adds a name-keyed
> character lookup to this surface.

### 9.3 The mutation surface

Every row below is a **mutation**. Every mutation that targets an **existing
character row** carries `expected_version` on its request per §9.4.
`CreateCharacter` is the one exclusion — the table marks it — because a create
has no prior row and therefore no prior version to be guarded against. What
guards a create instead is §6.1.3's `UNIQUE` index on the stored normalized name,
the same index the `RenameCharacter` row below collides against. §9.4.2 states
the exclusion normatively.

| RPC | Caller | Audience of the response | Gate | Description |
| --- | --- | --- | --- | --- |
| `CharacterAccessService.CreateCharacter` | authenticated player | `owner` | session resolution | **No `expected_version` — it creates the row a version guard would protect (§9.4.2).** Structured identity card (IDENT-01): name, pronouns, concept, species, age, faction. **Reshaped, not new** — see below. |
| `CharacterAccessService.UpdateCharacterProfile` | owner | `owner` | ABAC `write` on `character:<id>` | Partial update of the `profile.*` prose fields (§7.2), driven by an update mask (§9.5). Server-enforced length caps (IDENT-02). |
| `CharacterAccessService.UpdateCharacterDescription` | owner | `owner` | ABAC `write` on `character:<id>` | The in-world `look` text — the intrinsic `characters.description` column, **not** a `profile.*` row (IDENT-02a). Reaches the shipped `world.Service.UpdateCharacterDescription` (`internal/world/service.go:799-836`), never a parallel write path. |
| `CharacterAccessService.RenameCharacter` | owner | `owner` | ABAC `write` on `character:<id>` | Rename (IDENT-03). Runs §6.1's pipeline, the mixed-script and skeleton checks, and the block list; collides against the §6.1.3 unique index. |
| `CharacterAccessService.RetireCharacter` | owner | `owner` | ABAC `write` on `character:<id>` | Soft retire (IDENT-04). Sets `status` to `retired`. Does **not** release the name (§4.4). |
| `CharacterAccessService.UnretireCharacter` | owner | `owner` | ABAC `write` on `character:<id>` | Returns `status` to `active` (§4.5 property 1). |
| `CharacterAccessService.AdminUpdateCharacter` | admin | `admin` | ABAC on `admin_section:characters` | Administrative character edit, driven by the §10.6 field-mask allowlist. |
| `CharacterAccessService.AdminRetireCharacter` | admin | `admin` | ABAC on `admin_section:characters` | Admin disable. Moves the character through the **same** lifecycle states as owner-initiated retire (ADMIN-05). |
| `CharacterAccessService.AdminUnretireCharacter` | admin | `admin` | ABAC on `admin_section:characters` | The reverse. |

Each row's web proxy carries the same name under a `Web` prefix.

**`CreateCharacter` is a reshape, not an addition.** `WebCreateCharacter`
(`api/proto/holomush/web/v1/web.proto:177`) exists today and returns a bare
`character_name` scalar (`web.proto:656`). v0.13 replaces its request with the
identity card and its response with `OwnCharacter`. Per §2.5 the old shape is
replaced outright. It keeps its §3.3 census membership through the reshape — a
reshaped RPC is the same member, and deleting its inventory row would make the
census RED for the wrong reason.

**Retire and un-retire are two RPCs, not one `SetCharacterStatus(status)`.** A
single status-setting RPC would put the lifecycle vocabulary **on the wire**,
which makes `idle` a client-supplied value. §4.2 ships `idle` with **no
transition into it** in v0.13; a wire-settable status field is exactly such a
transition, added by accident, reachable by `curl`, and bypassing §4.3's
exhaustiveness rule because the write side never consults it. Two intent-named
RPCs keep the vocabulary server-side, give each operation its own audit meaning,
and leave `idle` unreachable — which is the precondition §4.3's
direct-construction test is written against.

**Every mutation emits through the transactional outbox in the same transaction
as the state change.** The state change and its one semantic envelope commit or
roll back together; a committed state change without its envelope is impossible.
This is the shipped `INV-WORLD-1` guarantee, and it is reached by routing the
write through the world write executor's same-transaction outbox seam — the shape
`UpdateCharacterDescription` already uses (`internal/world/service.go:820-828`).
A new mutation that writes state outside that seam silently regresses v0.12's
MODEL-03/04 guarantees, which is why this is stated as a MUST here rather than
inherited by assumption.

### 9.4 The concurrency contract

**Every §9.3 mutation that targets an existing character row carries
`expected_version` on its request message.** `CreateCharacter` is excluded;
§9.4.2 states the exclusion and its reason.

#### 9.4.1 Carriage: a scalar field on each request message

`expected_version` is an **`int32` scalar field on each guarded mutation request
message.** It is **NOT** a shared embedded precondition message, and no
`Precondition`/`WriteOptions` wrapper is introduced.

| Property | Value | Grounding |
| --- | --- | --- |
| Storage column | `characters.version` | `internal/store/migrations/000049_world_version_guard.up.sql:20` — `ALTER TABLE characters ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;` |
| Column type | Postgres `INTEGER` (32-bit signed) | same line |
| Domain type | `Version int` on `world.Character` | `internal/world/character.go:29` |
| Proto type | `int32 expected_version` | transcribed from the column |

Phase 4 transcribes `int32`; it does not choose a width. The scalar carriage
matches the shape the migration already models — one integer per row, read back
into the CAS predicate — and matching it means the request field, the struct
field, and the column are the same value with no wrapper to unwrap in between.

The field is **not** declared `optional`, and does not need to be. §2.2 rejects
proto3 `optional` scalars as an absence mechanism because an unset scalar
marshals as its zero value and is indistinguishable from an explicit zero. Here
that indistinguishability is **harmless**, because §9.4.2 sends both cases down
the same branch: both are rejected. This is the one place in the SPEC where the
proto3 zero-value property costs nothing, and it costs nothing precisely because
zero is not a legal input.

#### 9.4.2 Absent or zero is rejected, and never means "skip the guard"

**A guarded mutation request whose `expected_version` is absent or zero MUST be
rejected** with `CHARACTER_VERSION_REQUIRED` (§9.6) at the RPC boundary, before
the request reaches the domain layer. It **MUST NOT** be treated as an
instruction to write without the guard, and it **MUST NOT** be silently defaulted
to the row's current version.

**A *guarded mutation* is one that targets a character row that already exists**
— every §9.3 row except `CreateCharacter`. Creation is **outside this rule's
scope**, and `CreateCharacter` **MUST NOT** carry `expected_version` at all: the
field is absent from its request message, so there is nothing for this rule to
reject and no ignored field for a client to read meaning into.

The exclusion is not a relaxation of the guard. `expected_version` is an
optimistic-concurrency predicate **against a row that already exists** — it
answers "has this row changed since I read it". A create has no prior row, no
prior version, and nothing it could be stale against; the row comes into being at
`version = 1` from the `DEFAULT` in migration `000049`. Applying the rule to
creation would reject **every legal create request**, because there is no value a
well-behaved client could send: zero is not a legal input (§9.4.1), and any
non-zero value would be a fabrication about a row that does not exist yet.

**What guards a create instead.** Two concurrent creates do not race over a
version — they race over a **name**, and §6.1.3's `UNIQUE` index on the stored
normalized name is what decides them, surfacing as `CHARACTER_NAME_TAKEN` (§9.6).
That is the same index the `RenameCharacter` row collides against. Creation's
concurrency safety is therefore already specified, in the section that specifies
it for rename; it is not missing, and Phase 4 **MUST NOT** invent a
version-shaped substitute for it.

**This rule is load-bearing against a live affordance, not hypothetical.** The
shipped repository layer treats a zero version as an *unversioned* write:

- `CharacterRepository.Update` appends the CAS predicate **only when
  `char.Version > 0`** (`internal/world/postgres/character_repo.go:82-85`); at
  zero the `UPDATE` matches on id alone.
- `CharacterRepository.Delete` documents `expectedVersion == 0` as "an
  unversioned delete (existence-checked only)"
  (`internal/world/postgres/character_repo.go:120`) and enforces the expectation
  only when `expectedVersion > 0` (`:134`).

That affordance exists for callers that had not yet threaded a read version
through, and it is correct at the layer it lives in. It is **not** correct at a
web mutation boundary: a request that omits the field would not fail there — it
would succeed, unguarded, and the lost update it permits would be invisible. The
new RPCs therefore reject at their own entry point rather than relying on a
downstream layer that is documented to accept zero.

#### 9.4.3 Exactly one of two concurrent mutations succeeds

**Of two concurrent mutations carrying the same `expected_version`, exactly one
succeeds.** The other is rejected with the typed concurrent-edit signal:

- oops code **`WORLD_CONCURRENT_EDIT`** — `internal/world/errors.go:26`,
  `const CodeConcurrentEdit = "WORLD_CONCURRENT_EDIT"`.
- wrapping the sentinel `world.ErrConcurrentEdit`
  (`internal/world/errors.go:22`), which is *"deliberately distinct from
  ErrNotFound so a lost update is never mistaken for a missing row"*
  (`internal/world/errors.go:20-21`).

The loser is **not** auto-retried and **not** silently reapplied. Surfacing the
conflict is the behavior; a retry loop at the facade would reintroduce
last-write-wins one layer up, which is the guard this milestone must not regress.

**Phase 4 introduces the wire mapping for this code.** No facade maps it today —
`world.Service` propagates it unchanged by design
(`internal/world/errors.go:19-20`: *"The world.Service boundary propagates it
unchanged (D-02: no UX mapping in this phase)"*). §9.6 fixes the mapping as
`Aborted`. This is stated as an addition rather than an inherited behavior so
Phase 4 does not go looking for a mapping that is not there.

**The existing two-replica resilience harness is pointed at these RPCs** rather
than reimplemented as fresh concurrency tests. The concurrency property is the
same property v0.12 already proves; what changes is the entry point.

### 9.5 The update-mask contract

The edit RPCs — `UpdateCharacterProfile` and `AdminUpdateCharacter` — take a
`google.protobuf.FieldMask update_mask`, copying the shape the shipped scene
update already uses.

**The precedent, verbatim.** `SceneAccessServer.UpdateScene`
(`internal/grpc/sceneaccess_service.go:861-902`) validates the mask against a
closed allowlist declared as a map keyed by the exact path string
(`updateSceneMaskablePaths`, `internal/grpc/sceneaccess_service.go:846-853`),
rejects any path outside it with `InvalidArgument`
(`internal/grpc/sceneaccess_service.go:870-874`), and short-circuits an empty
mask as a no-op success **after** the ownership check
(`internal/grpc/sceneaccess_service.go:878-880`). The rationale in its own
comment is the one this SPEC adopts: reject an unlisted path *"rather than let a
direct gRPC caller drive an unadvertised mutation through the mask"*
(`internal/grpc/sceneaccess_service.go:843-845`).

Four rules, all normative:

1. **Allowlist, never a whole-message write.** The set of acceptable paths is
   checked in, closed, and compared against. A handler that writes whatever
   fields the request carries writes fields the UI never exposes.
2. **Paths are matched as exact strings.** No prefix matching, no wildcard, no
   glob, no dotted-subtree expansion. `profile` MUST NOT reach
   `profile.rp_preferences`; `role` MUST NOT be reachable from anything. An
   exact-string map lookup is the mechanism, as in the precedent above.
3. **Mask evaluation is order-independent.** The mask is a **set** of paths. The
   handler's verdict — which fields are written, whether the request is accepted
   — MUST NOT depend on the order the paths appear in. Reordering a mask's
   entries MUST produce an identical outcome, and duplicate entries MUST be
   idempotent rather than applying twice.
4. **An empty mask is a no-op success, not update-everything.** A request with no
   paths changes nothing and returns success. It MUST NOT be interpreted as
   "apply every field in the message", which would turn an under-populated client
   request into a silent full-row overwrite.

The gate ordering matters and is normative: **authorization is asserted before
the mask is applied, and the empty-mask short-circuit happens after
authorization.** The precedent does exactly this (`:862-880`) — ownership is
resolved first, so a no-op request cannot be used by an unauthorized caller to
drive downstream validation or store work, and cannot be used as an existence
oracle.

### 9.6 The error surface

All codes are `oops.Code(...)` values, following the repo convention.

| Code | Wire status | When |
| --- | --- | --- |
| `WORLD_CONCURRENT_EDIT` | `Aborted` | A mutation's `expected_version` does not match the row's current version (§9.4.3). Already stamped by the repos (`internal/world/postgres/character_repo.go:135`); Phase 4 adds the wire mapping. |
| `CHARACTER_VERSION_REQUIRED` | `InvalidArgument` | A **guarded** mutation request — one targeting an existing character row — carries an absent or zero `expected_version` (§9.4.2). Not reachable from `CreateCharacter`, which carries no such field. **New in v0.13.** |
| `CHARACTER_NOT_FOUND` | `NotFound` | The character id names no row. Already stamped (`internal/world/postgres/character_repo.go:97`, `:129`). |
| `CHARACTER_NOT_OWNED` | `PermissionDenied` | An `owner`-audience mutation names a character the calling player does not own. **New in v0.13.** |
| `CHARACTER_MASK_PATH_UNSUPPORTED` | `InvalidArgument` | An `update_mask` path falls outside the allowlist (§9.5 rule 2). **New in v0.13**; mirrors the precedent at `internal/grpc/sceneaccess_service.go:872`. |
| `CHARACTER_NAME_INVALID` | `InvalidArgument` | A create or rename fails §6.1's pipeline, the mixed-script rule, the skeleton check, or the block list — including a name that normalizes to empty. **New in v0.13.** |
| `CHARACTER_NAME_TAKEN` | `AlreadyExists` | A create or rename collides with the §6.1.3 unique index on the stored normalized name. **New in v0.13.** |
| `CHARACTER_PROFILE_NOT_FOUND` | `NotFound` | The profile is below its reachability floor, **or** the character id names no row. One uniform code for both — see below. **New in v0.13.** |

**`CHARACTER_PROFILE_NOT_FOUND` is deliberately one code for two causes.** §8.7
requires an unreachable profile to be indistinguishable from a nonexistent
character. Two codes, or two wire messages, or two response sizes, would separate
the cases and disclose that the character exists — which is the fact the floor
was set to withhold. The uniform code is the mechanism, not a convenience.

**Wire opacity.** Per `.claude/rules/grpc-errors.md`: the wire-level
`Status.message` is **generic**, and the structured reason is an **internal
attribute only**, logged with `errutil.LogErrorContext(ctx, msg, err, ...)` so
the oops code and context map survive as structured fields rather than being
flattened into a string. A handler MUST NOT interpolate an inner error into the
returned status message.

#### 9.6.1 The mandated assertion, and the one that does not work

**An opacity or authorization contract on this surface MUST be asserted over the
wire, not over the oops chain.**

| Assert | With |
| --- | --- |
| The mapped status | `status.Code(err)` equals the row's Wire-status value. |
| The generic message | `status.Convert(err).Message()` equals the generic literal the handler returns. |
| No leak | The internal code string does **not** appear anywhere in that message. |

For `CHARACTER_PROFILE_NOT_FOUND` specifically, the assertion is the
**differential** one §8.7 implies: drive an unreachable profile and a nonexistent
character id through the same RPC and assert the two responses are identical
across status, message and body. A one-sided "the unreachable profile returns
NotFound" assertion is satisfied by an implementation that returns NotFound with
a distinguishable message, which is the leak.

**Why not an oops-code assertion.** Neither
`errutil.AssertErrorCode` (`pkg/errutil/testing.go:15-20`) nor the
`oops.AsOops(err)` + `.Code()` form asserts the **outermost** code. Under the
pinned `github.com/samber/oops v1.22.0` (`go.mod:32`), `OopsError.Code()` is
documented in the dependency as *"returns the error code from the deepest error
in the chain"* and is implemented as a recursive `getDeepestErrorCode` walk. Both
spellings therefore resolve the same value, and both pass on a double-wrap: given
`oops.Code("INTERNAL").Wrap(oops.Code("CHARACTER_PROFILE_NOT_FOUND")…)` they
return `CHARACTER_PROFILE_NOT_FOUND` while the wire carries `INTERNAL`. Verified
empirically against the pinned version on 2026-08-01.

This contradicts `.claude/rules/grpc-errors.md` §"Wire opacity needs TOP-LEVEL
code assertions", which presents `oops.AsOops(err).Code()` as the
non-chain-walking alternative to `errutil.AssertErrorCode`; the two are
behaviorally identical here, and the single-expression spelling is not even a
compilable call, because `oops.AsOops` returns `(OopsError, bool)`
(`pkg/errutil/testing.go:17`, `internal/session/reaper.go:167`). The mismatch is
tracked as issue **#4902**; this SPEC documents current behavior and does not
change the rule. §14 carries the corresponding ROADMAP amendment.

**`errutil.AssertErrorCode` remains correct** for asserting *which* internal code
a handler produced — it resolves the specific, deepest code, which is usually the
one a test means. It is simply not evidence about what the wire carried, and MUST
NOT be cited as such.

### 9.7 The media proto shape ships now, and empty

The proto carries the media shape from v0.13 so that alt-text and content
warnings have somewhere to live before moderation exists (EXT-06):

| Element | Shape |
| --- | --- |
| `ProfileImage` | `{ media_id, alt_text, content_warning }` — three fields, all strings. |
| `primary_image` | A single `ProfileImage` on the character-profile response. Backed by the `profile.image.primary` row (§7.3), which the database constrains to exactly one per character. |
| `gallery` | `repeated ProfileImage gallery [(buf.validate.field).repeated.max_items = 10]`. **Ten is the maximum item count**, matching the ten `profile.image.gallery.00`…`.09` rows §7.3 fixes. |

**v0.13 ships zero upload behavior.** There is no uploader, no storage backend,
no media-serving path, and no `media_id` minting. `media_id` is an opaque string
whose format v0.13 does not fix, because nothing in v0.13 produces one. The model
is proven instead by inserting one primary and ten gallery rows through the real
schema and reading them back (EXT-05) — which demonstrates the no-migration-later
claim of §7.1 rather than asserting it.

> **Notably absent:** there is **no** upload RPC, no signed-URL endpoint, no
> multipart handler, and no image-processing dependency in v0.13. The reviewer
> for this SPEC **MUST** verify no v0.13 PR adds one — the shape ships early
> specifically so that the *schema* need not change when upload arrives, not as a
> signal that upload is in scope.

## 10. Admin Information Architecture

This section is normative. It fixes the admin section registry, the descriptor
that makes a registered section incapable of being wired without authorization,
where the authorization decision is made, whether the gate is evaluated per
acting character or per player, what the character-edit surface may reach, and
what is excluded outright.

A Phase-6 planner writes the registry and its descriptors from this section
without choosing a gating model.

### 10.1 The registry is exactly seven sections

The table below **is** the registry. Its first cell is the section id and nothing
else, so the id set is extractable from the rows rather than inferred from prose
— the same set-equality discipline §2.6 mandates for the RPC census, applied to
this registry census.

| Section id | Status | Authorization descriptor | Handler disposition |
| --- | --- | --- | --- |
| `characters` | available | `{read, admin_section:characters}` | Serves §9's admin RPCs. |
| `stats` | planned | `{read, admin_section:stats}` | Returns `NOT_IMPLEMENTED` **after** the gate. |
| `players` | planned | `{read, admin_section:players}` | Returns `NOT_IMPLEMENTED` **after** the gate. |
| `moderation` | planned | `{read, admin_section:moderation}` | Returns `NOT_IMPLEMENTED` **after** the gate. |
| `audit` | planned | `{read, admin_section:audit}` | Returns `NOT_IMPLEMENTED` **after** the gate. |
| `config` | planned | `{read, admin_section:config}` | Returns `NOT_IMPLEMENTED` **after** the gate. The visibility-configuration editor of §8.12 is this section's first tenant. |
| `plugins` | planned | `{read, admin_section:plugins}` | Returns `NOT_IMPLEMENTED` **after** the gate. |

**Seven ids, one available and six planned. There is no eighth**, and a v0.13 PR
adding one adds a row here in the same change.

**The authoritative registry is core-side.** The nav the web draws is *derived*
from it. A registry that lives only in `web/src/` is a client-side artifact that
cannot gate a server-side decision, which is the specific hazard
`.planning/research/PITFALLS.md` Pitfall 7 names.

**Mirror the shape, not the location.** `web/src/lib/nav/sections.ts:35-47`
already implements the pattern: an ordered `as const satisfies readonly
WorkspaceSection[]` literal whose derived `SectionId` union
(`web/src/lib/nav/sections.ts:47`) is the *"exhaustive key type for any
per-section map"*, so — in that file's own words — *"a section without an icon
then fails to compile rather than crashing the rail at runtime"*. That is the
mechanism §10.2 needs, one field over. **No library is added.**

### 10.2 The authorization descriptor is mandatory

**A registry entry REQUIRES an authorization descriptor. The descriptor has no
default, and no zero value means allow. A section registered without one fails at
compile time or at boot.**

All three clauses are load-bearing and none is redundant:

- **Required** — the descriptor is a field of the entry, not a lookup in a
  parallel table that can be missing a key.
- **No default** — there is no "if unset, use `admin`". A default is a value
  somebody stops thinking about.
- **No zero value meaning allow** — an empty action, an empty resource, or a
  zero-valued descriptor struct **MUST** be rejected, never read as permissive.
  This is the same fail-open shape §8.6's totality rule forbids for visibility
  floors and §4.3 forbids for lifecycle reads; it is forbidden here for the same
  reason.

**Failure is at compile time or at boot, not at request time.** The typed literal
carries the compile-time half: a `satisfies` constraint over a descriptor type
with no optional fields makes an entry missing its descriptor a type error. The
boot-time half validates the registry once at start and refuses to start on a
zero-valued descriptor. A request-time check would mean the misconfiguration is
discovered by the first unauthorized caller, which is the wrong party to learn it
from.

**A meta-test asserts set equality between the registry and the descriptor set**
(EXT-04). Because the descriptor is a required field of the entry, that equality
is trivially satisfiable — so the mandated test is the stronger, non-vacuous form
Pitfall 7 specifies: **enumerate the registry, and for every registered section
assert an unprivileged caller receives a typed denial from that section's
endpoint.** Today that is seven assertions, six of them against sections with no
content — which is precisely the point. The test is non-vacuous from day one, and
a new section that skips the gate turns it RED before the section has anything to
review.

### 10.3 The six planned sections refuse after the gate

**Order is the specification.** A planned section evaluates its gate **first**,
and returns `NOT_IMPLEMENTED` only to a caller the gate permitted.

Two consequences, both normative:

1. **A non-admin hitting a planned section is DENIED, not told it is
   unimplemented.** The refusal reveals nothing about which sections exist or
   what is being built.
2. **Wiring a section later replaces a handler body.** The gate is already there,
   already called, already covered by §10.2's denial test. Nobody has to remember
   to add a check, which is the thing nobody remembers.

This is the same move §4.3 makes for the unreachable `idle` value: ship the
structure, prove it with a fixture that reaches the otherwise-unreachable state,
and do not rely on the state staying unreachable.

Reserved capacity carries an implicit and false promise that the hard part is
done. Shipping the six gated-and-refusing is what makes the promise true instead.

### 10.4 The authorization shape

**The decision is an ABAC evaluation on an admin-section resource, made by one
shared helper called first at every entry point.**

| Element | Value |
| --- | --- |
| Resource family | `admin_section:<id>`, joining the shipped resource-prefix family at `internal/access/prefix.go:23-33`. Phase 2 adds the prefix and `AdminSectionResource()`. |
| Action | `read` to reach a section; `write` for a mutation within it. Both on the section resource. |
| Policy | `seed:admin-section-access`, scoped by resource **type** rather than by enumerated id, so it covers all seven sections and every future section at zero additional policy cost (EXT-07). |
| Denial codes | `DENY_ADMIN_SECTION` when the ABAC decision denies; `DENY_ADMIN_SECTION_UNREGISTERED` when the section id is not in the registry. |

Three prohibitions, each naming a real alternative:

- **Never a bare role lookup.** `PlayerHasRole` is a storage query, not an
  authorization decision. Calling it directly puts the decision outside the
  default-deny engine, where a *missing* policy would read as permissive instead
  of denying.
- **Never a route-guard or gateway decision.** A SvelteKit `+layout.ts` redirect
  and an `internal/web/` check are both UX, not the boundary. Beyond being
  bypassable by any caller who skips the route, an authorization decision in
  `internal/web/` is business logic in the process
  `.claude/rules/gateway-boundary.md` designates as protocol translation only —
  and that process is designed to be horizontally scaled and replaceable, which
  makes it the least trustworthy place to hold a decision. **The route guard
  MUST carry a comment saying it is UX and not the control**, so the next reader
  does not mistake it for one.
- **Never "the facade is the only caller, so the facade checking is enough."**
  The RPCs are reachable directly.

**Every admin RPC re-asserts its own gate, through the same helper, as its first
statement.** The redundancy is the point: keeping the call sites in lockstep is
what prevents one of them from silently losing a check. The tree already carries
this exact pattern with its rationale — `AssertOperatorAdmin`
(`internal/admin/auth/operator_admin.go:37-64`) is called first at every operator
entry point, and the comment at its call site records that *"Both gates are
re-asserted at every admin RPC entry point per INV-CRYPTO-83; the shared helper
keeps the three sites in lockstep"* (`internal/admin/auth/ingame.go:117-118`).
The web admin surface transposes that shape onto a different auth model; it does
not invent a new one.

**Denial tests carry a paired positive control.** A test asserting a non-admin is
denied proves nothing unless the *same fixture* succeeds once granted the role —
otherwise `err != nil` cannot distinguish "denied for lack of the admin role"
from "denied for lack of a session". The denial assertion itself follows §9.6.1:
assert the wire, not the oops chain.

### 10.5 The gate is evaluated per player, not per acting character

Research assigned this question to Phase 1 explicitly, and it cannot be deferred:
the session role field's shape follows from the answer, and reshaping that field
after Phase 4 or 6 writes it is a wire-compat change to every caller.

**Verdict: the admin gate is evaluated PER PLAYER.**

**This is what the tree already does, at every site the check exists.** Roles are
*stored* per character — `character_roles` is keyed `(character_id, role)`
(`internal/store/migrations/000001_baseline.up.sql:83-87`) — but the only shipped
lookup reads them per player:

- `PostgresRoleStore.PlayerHasRole` says so in its own comment: *"PlayerHasRole
  returns true iff any character of playerID has role"*
  (`internal/store/role_store.go:83`), and its query joins `character_roles` to
  `characters` on `WHERE c.player_id = $1`
  (`internal/store/role_store.go:86-93`).
- The shipped operator path depends on that semantics and documents it: *"Steps
  4-5: capability allow-list + RoleAdmin (any character)"*
  (`internal/admin/auth/ingame.go:116`), calling `AssertOperatorAdmin` with a
  **player** id (`internal/admin/auth/ingame.go:119`), which in turn calls
  `roleStore.PlayerHasRole(ctx, playerID, access.RoleAdmin)`
  (`internal/admin/auth/operator_admin.go:53`).

Specifying a per-character gate for the web would put two different answers to
"is this caller an admin" over one table — the operator socket saying yes and the
web saying no for the same human at the same moment. That is a second source of
truth about a trust boundary, and the cheaper of the two ways to remove it is to
match what exists.

**The character-scoped storage with a player-scoped read is not an accident to be
tidied away here.** It is a known asymmetry, tracked as issue **#4899**, and it
is precisely why §10.8 excludes role mutation: a character-scoped write into
`character_roles` has **player-scoped blast radius**.

#### 10.5.1 What follows from the verdict

1. **The session role field is player-scoped and singular.**
   `WebCheckSessionResponse` (`api/proto/holomush/web/v1/web.proto:733-746`)
   carries `player_name`, `player_id`, `is_guest` and the character roster today
   — **no role field at all**. Phase 4 or 6 adds `repeated string roles`
   carrying the roles the **player** holds. It is **NOT** a per-character map and
   **MUST NOT** be shaped as one. The field is computed once per session check,
   not once per character.

2. **Switching alts does not change admin reach, and the UI MUST NOT imply it
   does.** The alt switcher changes the acting character; the player is
   unchanged, so the roles are unchanged. Admin navigation that appeared and
   disappeared as the user switched alts would be describing a boundary that does
   not exist — worse than a wrong label, because a user who saw it vanish would
   reasonably conclude they had dropped a privilege they still hold.

3. **The roles field changes only what is drawn.** It is a nav-hiding input and
   **never** the authorization boundary (ADMIN-08). Drawing a link the viewer may
   not use still results in a denial at the RPC — the link is a wrong affordance,
   not an escalation. The converse is also true and worth stating: hiding a link
   the viewer *may* use is a UX defect, not a security control, and MUST NOT be
   relied on as one.

4. **Admin navigation is filtered from the registry contract**, not from template
   `{#if}` blocks (ADMIN-07). One derivation, consumed by every surface that
   draws a section, so a section can never be visible in one surface and hidden
   in another — the same single-gate discipline
   `web/src/lib/nav/sections.ts:63-67`'s `visibleSections` already applies to the
   workspace rail.

### 10.6 Character administration and the field-mask allowlist

The admin character-edit surface is `AdminUpdateCharacter` (§9.3), driven by an
`update_mask` under §9.5's four rules — allowlist, exact-string path matching,
order-independent evaluation, empty mask is a no-op.

**The rule that generates the allowlist:** a path is eligible only if writing it
has **no side condition beyond a length cap**. Anything carrying a normalization
pipeline, a uniqueness constraint, or a state machine gets its own intent-named
RPC instead, because a generic mask write would reach the column while bypassing
the rule that governs it.

Applying that rule, the allowlist is exactly:

```text
description
profile.pronouns
profile.concept
profile.species
profile.age
profile.faction
profile.appearance
profile.personality
profile.biography
profile.rumors
profile.currently
profile.rp_preferences
profile.timezone
```

Thirteen paths: the in-world description plus the twelve `profile.*` fields of
§7.2. Three exclusions follow from the rule and are stated so they read as
decisions:

- **`name` is excluded.** A rename runs §6.1's normalization pipeline, the
  mixed-script and skeleton checks, the block list, and collides against §6.1.3's
  unique index. A mask write would reach the column and run none of them.
- **`status` is excluded.** §9.3 keeps the lifecycle vocabulary off the wire so
  `idle` stays unreachable; a maskable `status` path would put it back on. Admin
  disable goes through `AdminRetireCharacter`, which moves the character through
  the **same** lifecycle states as owner-initiated retire (ADMIN-05).
- **`version` is excluded.** It is the concurrency guard, carried as
  `expected_version` on the request (§9.4), never as an editable field.

**No path may reach a role.** No `role`, `roles`, `character_roles`, `grant`,
`permission`, or `capability` path is in the allowlist, and §9.5 rule 2's
exact-string matching means no prefix or wildcard can expand into one. Because
the real risk is a *future* field rather than a present one, the durable
verification is schema-level: **a meta-test that fails if the admin character
message ever gains a field whose name matches `role|grant|permission|capability`,
paired with an allowlist test asserting set equality against the checked-in list
above.**

**The escalation test needs a positive control.** A test that calls
`AdminUpdateCharacter` with a `roles` field the message does not have proves
nothing — the request never carried the payload, the assertion "role unchanged"
is satisfied by the field being silently dropped, and the test passes whether or
not the property holds. The mandated shape is: first demonstrate the write path
works at all on a field it *is* allowed to change, then attempt the escalation on
the same call.

**The irreversible delete is reachable from no player-facing affordance.**
`world.Service.DeleteCharacter` (`internal/world/service.go:745-777`) is not the
implementation of an admin "delete" button (§4.4). Admin disable is retire.

### 10.7 Audit emission

**Every admin mutation emits its audit envelope in the same transaction as the
state change**, through the transactional outbox seam §9.3 mandates for every
mutation. The state change and its envelope commit or roll back together; an
admin mutation that committed without its audit envelope is impossible.

The envelope **MUST** carry:

- **The before-values.** An audit record saying "admin X updated character Y"
  answers nothing. The before-values are the whole point, and they are available
  only at write time — the row is already overwritten by the time anything else
  could look.
- **The acting player id, not only the acting character.** §10.5 makes the
  authority player-scoped, so an audit trail keyed on the character records the
  wrong subject: it names which alt was in the chair, not who exercised the
  authority. Record both; the player id is the required one.
- **The section and action** that were evaluated, so the record ties back to the
  §10.4 decision that permitted it.

**Audit emission ships in v0.13 even though the audit *viewer* is deferred.** The
`audit` section returns `NOT_IMPLEMENTED` after its gate (§10.1) — but if
emission waited for it, the viewer would ship later with no history behind it.
Emission first is what makes the deferred viewer worth building.

The durable audit row lands in the existing `events_audit` table, reached through
the shipped audit pipeline rather than a bespoke table — retention, DLQ capture,
and replay come free. Note precisely what "in-transaction" does and does not
mean here: **the envelope is transactional; the `events_audit` row is projected
from it.** §14 carries the amendment, because two artifacts state this
differently.

### 10.8 Notably absent

> **Notably absent — four exclusions, each stated because an omission is not an
> exclusion.** Silence is read as "nobody thought about it", which is an
> invitation; a stated exclusion is a decision a later PR has to argue with.
>
> - **Role mutation is NOT part of character administration in this milestone
>   (PORTAL-08).** *Reason:* the read is player-wide (§10.5), so granting a role
>   to any one character — a throwaway alt included — grants it to the **player**
>   everywhere the check is player-scoped, including the operator path. A
>   character-scoped write with player-scoped blast radius is an escalation
>   vector, and the character-admin UI is exactly where a role field would
>   naturally be edited. The underlying per-player-versus-per-character question
>   is open as issue **#4899**; role management belongs in the deferred `players`
>   section, with its own design, after that is decided.
> - **Admin impersonation is NOT admitted.** *Reason:* it attributes actions to
>   the wrong actor, which launders the audit trail §10.7 exists to produce —
>   every record it generates names a subject that did not act.
> - **A hardcoded break-glass admin identifier is NOT admitted.** *Reason:* it is
>   an authorization decision made outside the default-deny engine, in a place no
>   policy query can see and no policy change can revoke.
> - **A raw SQL console or database-console surface is NOT admitted.** *Reason:*
>   it bypasses the ABAC gate, the field-mask allowlist, the lifecycle state
>   machine, and the audit emission simultaneously — every control this section
>   specifies, at once.
>
> The reviewer for this SPEC **MUST** verify that no v0.13 PR adds any of the
> four. Each would be individually reasonable-looking as a "small admin
> convenience", and each removes a control that the rest of this section spends
> its length establishing.

## 11. Sorting and Filtering Verdict

This section is normative. PORTAL-09 asks whether any v0.13 surface sorts or
filters on a profile field. It is answered here as a verdict, because silence on
the question would be indistinguishable from nobody having asked it.

### 11.1 The verdict

**No. No v0.13 surface sorts, filters, groups, or counts on a profile field.**

Stated as a prohibition: a v0.13 surface **MUST NOT** place a `profile.*`
property row — or any value derived from one — in an `ORDER BY`, a `WHERE`, a
`GROUP BY`, a `COUNT`, a facet tally, or a pagination total.

### 11.2 The three reasons, in order

**First — property rows are not cheaply sortable or filterable.** §7.1 puts every
profile field in `entity_properties` as a name/value row, which is what buys the
no-migration-later guarantee. The cost of that shape is that ordering or
filtering on a field means joining or pivoting the property table per attribute.
This is the acknowledged residual of the research conflict that chose property
rows over JSONB: the shape won decisively on every other axis and lost on this
one. Paying an index and a pivot for a capability nothing in v0.13 needs is the
wrong trade.

**Second — read-time tier evaluation makes it incoherent as well as expensive.**
§8.5 evaluates the viewer-tier floor **at read time** against the attribute name,
which is what makes a configuration change take effect on the next read with no
backfill. The consequence for ordering is structural: **the visible set differs
per viewer.** There is no single ordering of characters by `profile.currently`,
because for an anonymous visitor most of those values do not exist and for an
authenticated player they do. A sort would have to be computed per viewer over a
per-viewer set, or computed once over the unfiltered set and then filtered — and
the second is the leak. Sorting is not merely expensive under this model; it has
no viewer-independent meaning to compute.

Note what the alternative would have cost. A sortable profile field would have to
be **stamped** — materialized into a column or an indexed row with its visibility
resolved at write time — which is exactly the per-row stamping §8.5 rejects,
reintroducing the backfill-on-every-configuration-change that read-time
evaluation exists to avoid.

**Third — aggregate operations over privacy-bearing fields are themselves a side
channel.** This is the decisive reason, and it would hold even if the first two
did not. Redaction is applied to the response; sorting, filtering, and counting
happen in SQL, before the redactor runs, over the unredacted value. The redactor
cannot see that it has already leaked:

- An **ordering** over a withheld field discloses the relative values of that
  field for every row in the list.
- A **filter** discloses the predicate's value for every row returned — and, by
  absence, for every row not returned.
- A **facet count** over a privacy-partitioned set lets a reader binary-search a
  single character's withheld value by differencing counts across queries.
- A **pagination total** on a filtered privacy-partitioned list is itself the
  leak, even when every returned page is correctly filtered.

Each of these defeats §8.9's per-field omission without ever placing the withheld
value in a response. §8.9 guarantees the field is absent from the marshaled
bytes; none of the four channels above puts it there.

### 11.3 The one surface that MAY sort and filter, and exactly on what

**The admin character list MAY sort and filter — on intrinsic columns only,
never on a profile property row.** The permitted set is enumerated here so Phase 6
does not have to infer it:

| Field | Sort | Filter | Why it is safe |
| --- | --- | --- | --- |
| `characters.name` | Yes | Yes | Identity. Public at every tier §8.8 permits a profile to be reached at. Filtering matches against the stored normalized name of §6.1.3, not the display name. |
| `characters.created_at` | Yes | Yes | Intrinsic row metadata, carrying no profile content. |
| `characters.status` | Yes | Yes | The lifecycle column of §4.1. Administration needs to separate active from retired; the value is `admin`-audience already. |
| `characters.player_id` | No | Yes | The OOC player↔character linkage, already visible to the `admin` audience. Equality filter only — grouping a player's alts — never an ordering. |

That is the whole list. **Every other column and every `profile.*` row is
excluded**, including the in-world `characters.description`: it is an intrinsic
column, but it is prose whose ordering is meaningless and whose filtering is a
content search, which is a different feature with a different design.
`characters.location_id` is likewise excluded — administration does not need it,
and it is the worked example the facet-count attack above is built on.

`AdminSearchCharacters` (§9.2) searches **names**, not profile prose. A prose
search over profile fields would be a filter over privacy-bearing rows wearing a
different name, and §11.1's prohibition reaches it.

The admin list is the permitted surface precisely because its audience already
sees every field it could order by (§2.1) — there is no withheld value for an
ordering to disclose. That property is what makes it safe, so it is the property
that must still hold if the list ever grows a new sort key.

### 11.4 What this leaves open

The deferred searchable character directory is unaffected, and deliberately so.
Its indexing need stays **additive and non-blocking** exactly because nothing in
v0.13 depends on it: no v0.13 surface sorts or filters on a profile field, so no
v0.13 schema, policy, or projection is shaped around making one cheap. A later
milestone adds indexing without undoing anything decided here.

**Any future directory MUST answer this question again before it ships**, against
the model as it stands then. This verdict is not a permanent property of the
system — it is a decision for v0.13, made cheap by the fact that v0.13 promises
no sorting. The moment a surface wants to order or filter on a profile field, all
three reasons in §11.2 come back, and the third one does not weaken with time.

> **Notably absent:** there is **no** sort dropdown, no facet panel, no
> `total_count` on any filtered privacy-partitioned list, and no
> `?has_<field>=true`-style query parameter on any v0.13 character surface. The
> reviewer for this SPEC **MUST** verify no v0.13 PR adds one — a sort control
> whose options are drawn from the §7.2 field list is the specific warning sign,
> because that list is the privacy-bearing set.

## 12. Verification Integrity

This section is normative, and it is the one section of this SPEC written to be
**read somewhere else**. Its six rules are copied verbatim into every v0.13
`PLAN.md` from Phase 2 onward (§12.2), so each rule is written to stand alone: a
reader of a Phase-6 plan who never opens this document must still be able to act
on the copied text.

**Why the section exists.** v0.12's audit catalogued **seventeen** instances of
*"a verification that cannot fail"* — and it catalogued them with these same
review gates already in place. Research then found that the natural test for
nearly every privacy and authorization property in this milestone **passes while
the property is false** (`.planning/research/PITFALLS.md`, the *"Inverted test
question"* paragraph carried by all fourteen pitfalls). Three of them, in one
line each:

- A private-field test asserting `resp.Bio == ""` passes because **the fixture's
  bio was empty to begin with** — it never fails, including against a handler
  with the redaction deleted (`PITFALLS.md:89-100`).
- A per-endpoint leak suite **cannot detect the endpoint nobody wrote a test
  for** — it is structurally incapable of finding a missing member of its own set
  (`PITFALLS.md`, Pitfall 2; `.planning/research/SUMMARY.md:153-154`).
- A denial test passes on a subject that **would have been denied anyway** — for
  a missing token rather than a missing role, a distinction `err != nil` cannot
  draw (`PITFALLS.md:392-400`).

None of these is a sloppy test. Each is the *obvious* test, written first, by
someone who understood the property. That is what makes the rules below binding
rather than advisory.

### 12.1 The six rules

The rules are numbered **1 through 6**, in the order `.planning/REQUIREMENTS.md`
PORTAL-10 (`:51-78`) fixes them. **The numbering is part of the contract** — see
§12.2. Each rule carries a **non-vacuity clause** naming what a fake satisfaction
of that rule looks like, because every one of these rules can itself be
satisfied vacuously.

---

1. **Census with set equality.**

Every property that must hold across a *set* of surfaces MUST be verified by a
**census** that derives the set from the tree and compares it against a
checked-in expected set by **set equality** — order-independent, exact-string
keys, symmetric-difference diff on failure. Inequality in **either** direction is
RED. A per-endpoint test suite MUST NOT be substituted: it iterates the expected
set, so an unexpected member is never visited. The v0.13 instances are the
character-returning RPC census (§2.6, §3) and the admin section-registry ↔
authorization-descriptor census (EXT-04, §10.2).

> **Non-vacuity.** A census over an **empty or hand-written set** satisfies this
> rule while proving nothing. The derived side MUST come from the tree —
> generated service descriptors, the registry's own entries — never from a second
> hand-maintained list, which merely compares a list to its own copy. A phase
> whose census set is empty at the moment the test is written MUST say so and say
> why, rather than shipping a green comparison of two empty sets.

2. **Paired positive control on every denial test.**

Every test asserting that a subject is **denied** MUST be paired, on the **same
fixture**, with a positive control proving that subject would otherwise have been
**permitted** — typically the same call after granting the one attribute under
test. Without the pair, the denial test cannot distinguish *"denied for lack of
the admin role"* from *"denied for lack of a session"*, and it stays green when
the gate is deleted. The negative test MUST also target the **privileged**
endpoint: a denial test aimed at an endpoint that has nothing to deny is the
purest form of a verification that cannot fail (`PITFALLS.md:338-344`). The same
pairing applies to negative *content* assertions — assert the fixture is
non-degenerate first (`PITFALLS.md:233-240`).

> **Non-vacuity.** A phase that has **no denial tests at all** MUST state that
> explicitly in its plan rather than omitting this rule. An omitted rule is
> indistinguishable from a rule nobody got to; a stated absence is a claim
> someone made and a reviewer can challenge. Rule 2 MUST NOT be satisfied by the
> absence of the tests it governs.

3. **Assertions against marshaled response bytes.**

Every assertion that a field is **absent** for a viewer MUST be made against the
**marshaled response bytes**, not against a populated Go struct and not against
rendered UI. Absence is this milestone's entire enforcement mechanism (§2.7,
§8.9): a field cleared in a projection helper the handler never calls, or hidden
by the client, MUST NOT be able to pass. Seed a distinctive sentinel value and
assert the sentinel does not appear anywhere in the serialized response, rather
than asserting a named field equals its zero value.

> **Non-vacuity.** An assertion against a **populated Go struct** — or against
> the DOM in a Playwright test — satisfies the sentence but not the rule. So does
> asserting `field == ""` on a fixture that never set the field: that is rule 1's
> empty-set failure wearing rule 3's clothes, and it is the exact shape of
> `PITFALLS.md:89-100`.

4. **Gates demonstrated RED against the pre-fix state.**

Every new gate — test, lint, census, meta-test, CI check — MUST be **observed
failing** against the state that precedes its fix, and that observation MUST be
recorded in the plan's SUMMARY. **A gate never seen failing is
indistinguishable from a gate that cannot fail.** The named v0.13 instance is the
name-uniqueness gate: it MUST be demonstrated RED against **today's unindexed
schema**, before the unique index lands (IDENT-09, §6.3).

> **Non-vacuity.** A gate whose RED state was **assumed rather than observed**
> fails this rule, and so does a gate demonstrated red against a *hypothetical*
> pre-fix state constructed for the demonstration rather than against the tree as
> it actually stood. "The test would have failed before" is not the observation;
> the recorded non-zero exit is.

5. **Wire-level assertion of every opacity and authorization contract.**

An opacity or authorization contract MUST be asserted **over the wire**, per
§9.6.1: the mapped status via `status.Code(err)`, the generic message via
`status.Convert(err).Message()`, and the internal code string **absent** from
that message. Where the contract is *indistinguishability* — an unreachable
profile from a nonexistent character (§8.7) — the assertion MUST be
**differential**: drive both cases through the same RPC and assert the two
responses are identical across status, message and body.

**An oops-code assertion is not evidence about what the wire carried.** Under the
pinned `github.com/samber/oops v1.22.0` (`go.mod:32`), `OopsError.Code()` is
documented in the dependency as *"returns the error code from the deepest error
in the chain"* and is implemented as a recursive `getDeepestErrorCode` walk;
`errutil.AssertErrorCode` (`pkg/errutil/testing.go:15-20`) is a thin wrapper over
`oops.AsOops` plus that same `.Code()`. The two spellings are therefore
**behaviorally identical**, and **both** return the inner code on a double-wrap —
`oops.Code("INTERNAL").Wrap(oops.Code("DENY_NOT_ADMIN_ROLE")…)` yields
`DENY_NOT_ADMIN_ROLE` while the wire carries `INTERNAL`. `errutil.AssertErrorCode`
remains the correct and endorsed tool for asserting **which internal code** a
handler produced; it simply MUST NOT be cited as proof of what a caller saw.

> **Restated.** PORTAL-10's original rule 5 read *"Top-level oops code
> assertions via `oops.AsOops(err).Code()`; `errutil.AssertErrorCode` chain-walks
> and passes on double-wrap."* Both halves are false against the pinned
> dependency, and a test written to that prescription **asserts the inner code
> and passes while the wire leaks** — a verification that cannot fail, living
> inside the rule written to end them. This SPEC restates rule 5 around the wire
> and changes no rule file; the mismatch with
> `.claude/rules/grpc-errors.md` §*"Wire opacity needs TOP-LEVEL code
> assertions"* is tracked as issue **#4902**. §14 carries the amendment.

> **Non-vacuity.** An assertion that **passes on a double-wrapped error** fails
> this rule no matter which helper spells it. So does a **one-sided** assertion
> on a differential contract: *"the unreachable profile returns NotFound"* is
> satisfied by an implementation that returns NotFound with a distinguishable
> message, which is the leak.

6. **Invariant-scope discipline.**

Every guarantee this milestone pins MUST be allocated into an **existing**
registry scope (`ACCESS`, `PRIVACY`, `WORLD` — §13) or declare a boundary. Ad-hoc
families (`INV-PROFILE-*`, `INV-ADMIN-*`, `INV-PORTAL-*`) MUST NOT be minted;
that is precisely the debt `.claude/rules/invariants.md` exists to prevent. An
entry whose asserting test does not exist yet ships `binding: pending` with **no**
`asserted_by`. Because this SPEC lives under `.planning/`, its entries are
invisible to the orphan check — which walks only `docs/superpowers/specs/`
(`test/meta/invariant_registry_test.go:341`) — and MUST be **hand-registered** in
`docs/architecture/invariants.yaml`.

> **Non-vacuity.** A **fabricated `// Verifies:` annotation** on a test that
> merely touches the code, rather than asserting the guarantee, satisfies the
> ratchet and defeats it — the documented false-green the binding ratchet exists
> to catch. A `Skip`-only placeholder carrying the annotation is the same
> failure. When no test genuinely asserts the invariant, that is a real coverage
> gap: leave it `pending` and file it.

---

### 12.2 The binding mechanism

**The block above is copied verbatim into an acceptance-criteria block in every
v0.13 `PLAN.md` from Phase 2 onward, and the copying phase specializes each rule
to its own subject matter.** `gsd-plan-checker` verifies both — that the block is
present, and that it is specialized rather than pasted unchanged. This is D-17
(`.planning/phases/01-portal-spec/01-CONTEXT.md:163-169`).

Three properties make the copy load-bearing rather than ceremonial:

| Property | Requirement |
| --- | --- |
| Specialized, not pasted | Each rule names the concrete v0.13 artifact it governs **in that phase** — which census, which denial test, which gate. A rule copied without a named subject is a rule nobody owns. |
| Census scope named per phase | Each copied block **names the census scope its own phase owns**, so two phases do not both claim the same census set. Phase 4 owns the character-returning-RPC census (§2.6); Phase 6 owns the admin section-registry census (EXT-04). Neither restates the other's, and neither may cite the other's as discharging its own rule 1. |
| Numbering preserved | The rules stay numbered **1 through 6** in **every** copy. A cross-artifact reference — a SUMMARY saying *"rule 4 demonstrated RED at commit X"*, a review comment saying *"rule 2 unsatisfied"* — is only unambiguous if the numbering is stable. A phase MUST NOT renumber, reorder, or drop a rule; a rule that does not apply is stated as not applying (see rule 2's non-vacuity clause) rather than removed. |

**No meta-test asserts on planning-document markdown.** The enforcement is the
copied block plus plan-checker review, and nothing else. Both alternatives were
considered and rejected:

| Rejected | Reason |
| --- | --- |
| A `test/meta/` check over the plan markdown | Asserting on planning documents is unusual here — the nearest relative, `internal/access/spec_amendments_test.go`, substring-asserts a `docs/superpowers/specs/` file, not a plan. Worse, **rule 4 wants gates demonstrated RED**, and this one is hard to see fail meaningfully: a substring check over prose goes green the moment the substring appears, whether or not the rule was applied. It would be a gate that cannot fail, guarding the rules against gates that cannot fail. |
| Prose only — state the rules and trust the review | v0.12 catalogued seventeen verifications that could not fail **with these same review gates already in place**. Prose is what was already in place. |

The honest limit: a copied block is enforced by a reviewer reading it. This SPEC
records that limit rather than dressing it up, which is why every rule carries a
non-vacuity clause — the clause is what gives that reviewer something specific to
check.

### 12.3 Test plan by tier

Tier names are `.claude/rules/testing.md`'s exactly. Where a rule spans tiers,
the row names the tier at which the rule's **binding** check lives.

| Rule | Tier | Runner | Concrete v0.13 instance |
| --- | --- | --- | --- |
| 1 — census | `unit` | `task test` | The character-returning-RPC census derives from generated service descriptors and needs no dependencies (§2.6); the section-registry ↔ descriptor census reads the registry in-process (EXT-04). Both belong beside the other set-equality meta-tests. |
| 2 — paired positive control | `full-stack integration` | `task test:int` | The admin-gate denial tests (ADMIN-02) need the real `CoreServer` and a real ABAC engine, because the property is *which* gate denied. Pure policy-evaluation pairs (`seed:admin-section-access`, Phase 2) also run at `unit` against the engine directly. |
| 3 — marshaled bytes | `unit` for projections, `full-stack integration` for reads | `task test`, `task test:int` | A projection function's output marshals in-process; the end-to-end guarantee that no v0.13 read path reintroduces a field needs the real handler chain. **`E2E` does not satisfy this rule** — a Playwright assertion reads the DOM, which is the client, which §2.7 removes from the decision. |
| 4 — gate RED pre-fix | tier of the gate itself | — | The name-uniqueness gate is `full-stack integration` (real Postgres, `//go:build integration`) and MUST be run against the pre-index schema. For a lint or a census the tier is `unit`; the rule is about the *observation*, not the tier. |
| 5 — wire-level assertion | `full-stack integration` | `task test:int` | `status.Code` / `status.Convert(...).Message()` over the real handler chain, plus the §8.7 differential pair. An in-process gRPC call is sufficient **because §9.6.1 requires the handler to return a mapped status**; an oops error returned bare would surface as `codes.Unknown`, which is itself a failure of this rule. |
| 6 — invariant-scope discipline | `unit` | `task test` | `test/meta/invariant_registry_test.go` — drift, provenance, binding-presence and the `Skip`-only-placeholder guard all run under `task test` with no build tag. The `pending`-versus-`bound` judgement itself is a review judgement, not a test. |
| — | `E2E` | `task test:e2e` | Named here only to fix its boundary: `E2E` verifies that the surface a logged-out visitor loads renders (PROFILE-01) and that the UI states the not-retroactive fact (PROFILE-12). It MUST NOT be cited as satisfying rules 1, 3 or 5. |

> **Notably absent:** there is **no** meta-test over `.planning/` markdown, **no**
> lint asserting the six-rule block's presence, and **no** CI check counting
> rules. The reviewer for this SPEC **MUST** verify no v0.13 PR adds one and then
> relaxes the plan-checker review on the grounds that the check covers it — a
> substring check over prose is the weaker gate, not the stronger one.

## 13. Invariants

v0.13 allocates its invariants into the **existing `ACCESS`, `PRIVACY` and
`WORLD` scopes. No new scope and no new boundary is declared.** Minting a fresh
`INV-PORTAL` family is exactly the debt `.claude/rules/invariants.md` exists to
prevent: pre-2026-05 specs each minted their own un-indexed family and a
migration had to dig them all out.

The guarantees below are split by what each one **is**. The disclosure
guarantees — what the response reveals about existence and about minimum identity
— are `PRIVACY`. The evaluation guarantees — the read-time authorization
decision, the wire shape that decision enforces, and the completeness of the
surface set that decision must cover — are `ACCESS`, which is where
`INV-PRIVACY`'s own boundary forwards ABAC policy evaluation by name.

- **INV-ACCESS-10 (read-time tier-floor evaluation):** the viewer-tier floor for
  a profile attribute is evaluated at read time by the default-deny ABAC engine
  against the attribute name and the viewer's tier, never stamped per row; and an
  infrastructure failure in that evaluation resolves DENY, never permit. Bound by
  a test that drives a profile read against a failing access engine and asserts
  the read aborts rather than returning a sparse profile — the
  `ErrAccessEvaluationFailed` branch shape at
  `internal/world/service.go:1144-1171`. **Binding lands in Phase 4**, with the
  policy family itself seeded in Phase 2.
- **INV-ACCESS-11 (per-field absence):** an attribute whose floor the viewer does
  not clear is absent from the marshaled response, never present-and-empty. Bound
  by a test asserting over the **marshaled bytes** — not the Go struct — that the
  withheld field's key does not appear. **Binding lands in Phase 4.**
- **INV-PRIVACY-9 (profile-reachability opacity):** a profile below its
  reachability floor returns a not-found-equivalent whose wire shape is identical
  to the response for a character id that does not exist. Bound by a test
  asserting the two responses are indistinguishable across status, error code and
  body. **Binding lands in Phase 4.**
- **INV-PRIVACY-10 (minimum-identity floor):** if a viewer can reach a profile at
  all, that profile carries name and pronouns; the configuration may set the
  profile's reachability floor arbitrarily high but cannot raise name or pronouns
  above it. Bound by a test that sets the reachability floor at each rung of the
  ladder and asserts every reachable response carries both fields. **Binding
  lands in Phase 4.**
- **INV-ACCESS-12 (character read-surface census):** the set of RPCs whose
  response transitively contains a character-shaped message equals the set
  enumerated in §3 — no more and no fewer. Bound by the Phase-4 census test,
  which derives the set from the generated service descriptors plus §3.2's
  explicitly-named non-type-reachable surfaces, and compares it against the §3
  inventory by **set equality** on the fully-qualified proto method name in
  `package.Service.Method` form — order-independent, never a substring match,
  never a Go handler identifier. **Binding lands in Phase 4.**

- **INV-WORLD-5 (exhaustive lifecycle reads):** every read of
  `characters.status` is an exhaustive `switch` over the closed vocabulary whose
  `default` arm denies, and a character in the third, otherwise-unreachable
  `idle` state is excluded from character selection. Bound by a test that
  **constructs a character directly in `idle`** — bypassing the transition v0.13
  does not ship — and asserts the exclusion, so the unreachable value is covered
  non-vacuously rather than by the absence of a fixture that can reach it.
  **Binding lands in Phase 2**, with the migration that adds the column.
- **INV-WORLD-6 (retire preserves the name reservation):** retiring a character
  leaves its row and its name reservation intact; the irreversible character
  delete (`internal/world/service.go:745-777`) is the only path by which a
  character name becomes claimable again. Bound by a test that retires a
  character and asserts a second character cannot take its name, paired with the
  positive half — that the delete path does release it — so the test pins the
  boundary rather than one side of it. **Binding lands in Phase 2.**
- **INV-WORLD-7 (guarded character mutation):** every new character mutation RPC
  **that targets an existing character row** carries `expected_version` on its
  request, rejects an absent or zero value rather than writing unguarded, and
  rejects a stale value with the typed `WORLD_CONCURRENT_EDIT` signal so exactly
  one of two concurrent mutations sharing a version succeeds. `CreateCharacter`
  is **excluded from the version guard** — it creates the row the guard would
  protect, so it carries no `expected_version` and is guarded instead by
  §6.1.3's unique index on the stored normalized name (§9.4.2). The
  same-transaction obligation is **not** scoped that way: **every** new character
  mutation RPC, creation included, commits its state change together with its one
  outbox envelope in the same transaction. Bound by a test that drives two
  concurrent mutations at the same version and asserts exactly one commits and
  the other carries the typed code at the **top level**, paired with a
  zero-version request asserting rejection — the paired half is what proves the
  guard is not satisfied vacuously by a caller that simply never omits the field.
  **Binding lands in Phase 4**, with the domain commands landing in Phase 3.

**All eight ship `binding: pending`.** Their asserting tests do not exist until
Phase 2 or Phase 4, and a `// Verifies:` annotation on a test that does not
genuinely assert the guarantee is a false-green — the documented failure this
registry's binding ratchet exists to catch. A `pending` entry carries no
`asserted_by`.

## 14. Amendments and Divergences

This SPEC supersedes text in four live planning artifacts and records one
deliberate divergence from a principle its own maintainer stated. **Each entry is
listed with its rationale, and the reviewer is expected to evaluate it as
intentional, not a defect.**

**Recording an amendment is not applying it.** A downstream planner reads
`.planning/ROADMAP.md` and `.planning/REQUIREMENTS.md` directly; a criterion that
still says an owner can toggle field visibility will be planned regardless of
what a table here says about it. `.claude/rules/references/design-review-learnings.md`
names exactly this — an amendment row whose superseded string is still live in a
sibling artifact — as a recurring review failure. Every row below has therefore
been **applied to its own artifact**, and each application is proven by a
recorded search for the superseded string rather than asserted. The exception is
row 8's rule-file half, which is deliberately not applied; the row says why.

The first column quotes the superseded text **verbatim** alongside its artifact
and location. The quoting is not decoration: it is what forces the search that
catches a string still live somewhere nobody inventoried.

> **On the quotations.** The source artifacts are hard-wrapped, so several quotes
> below span a line break in their source and are reproduced here with the break
> normalized to a space. The searches that prove each amendment landed therefore
> target a substring lying on a **single** source line — a multi-line quote
> searched line-wise returns zero matches whether or not the text survives, which
> would be a proof that cannot fail. Each row's single-line search target and its
> result are recorded in `01-05-SUMMARY.md`.

| Artifact and superseded text | v0.13 SPEC | Rationale |
| --- | --- | --- |
| **1.** `.planning/ROADMAP.md` — Phase 4 success criterion 3, opening clause: *"An owner can set any profile field to `public` or `private` except `name` and `pronouns`, which the server refuses to make private"* | The **game-configured, per-attribute viewer-tier floor** of §8.3 — visibility is configuration, never an owner control (§8.1) — with `name` and `pronouns` re-seated as the hard floor of §8.8 that the configuration cannot raise above the profile's own reachability floor. **The exhaustive-`switch`-with-`default: deny` clause survives verbatim** and is retained in the restated criterion. | D-09: v0.13 allows **no player or character agency** over profile visibility. A criterion granting an owner a control that will not exist would be built as one — a settings surface, a mutation RPC, a per-row `visibility` write path — none of which this SPEC specifies, and each of which would then need unwinding. The half of the criterion that is about *evaluating* a tier value is correct as written and is the half that survives: §8.6's postures are exactly the exhaustive switch the clause demands, and an unrecognized tier still denies. |
| **2.** `.planning/ROADMAP.md` — Phase 5 success criterion 4, opening clause: *"An owner flips a field between public and private and the change is what a logged-out visitor sees on the next load"* | A **configuration** change taking effect on the next read, per §8.5 — the floor is evaluated at read time against the live configuration and is never stamped onto a row, so no backfill exists and "next load" is exactly right about the timing. | Same root as row 1: the toggle does not exist. The criterion's *observable* claim — that a visibility change is visible to a logged-out visitor on the next load without any republish step — is a real and testable property of the read-time model, and restating it around the configuration keeps it. What changes is who performs the change, not when it takes effect. |
| **3.** `.planning/REQUIREMENTS.md` — PROFILE-12: *"The visibility toggle and the retirement flow **state in the UI** that privacy is not retroactive over already-published history."* | The not-retroactive statement is re-seated onto the two surfaces that will exist: the **retirement flow**, and the **surface where a player authors profile fields**. §5's name-capture inventory is what makes the statement true — display names are denormalized into immutable event payloads and `scene_log`, and there is no update path. | The retirement half stands unchanged and is **not** weakened. The "visibility toggle" half has no toggle to attach its statement to under D-09. Deleting the clause would lose the requirement's actual point, which is that a player MUST be told before authoring content that publishing is one-way; the profile-authoring surface is where that warning belongs once the toggle is gone. |
| **4.** `.planning/research/SUMMARY.md` — CONFLICT 4 resolution: *"ship `public` and `private` in the v0.13 UI"* | The **tier vocabulary** decision survives intact — two tiers with real evaluators, `restricted` present in the `entity_properties` CHECK but not surfaced, exhaustive `switch` with `default: deny`. Its **owner-facing UI expression** does not: there is no v0.13 surface on which a player selects a tier. | This is a **dated research record** (2026-07-31). It is annotated with a superseded-by note placed immediately after the affected clause and is **never rewritten** — rewriting a dated artifact destroys the ability to reconstruct why a decision was made, and the reasoning here (PITFALLS' fail-open unimplemented-tier argument beating FEATURES' three-tier proposal) is reasoning this SPEC still relies on. Only the clause's UI expression is superseded. |
| **5.** `docs/architecture/invariants.yaml` — the `INV-PRIVACY` scope record, `boundary:` first sentence: *"Privacy-relevant gating on history reads."* | *"Privacy-relevant gating on reads."* The `"Does NOT include: ABAC policy evaluation (→ INV-ACCESS), subscribe authorization (→ INV-EVENTBUS)"` clause is **preserved verbatim and MUST NOT be widened**. The `description:` enumeration was extended in the same edit so the record describes the entry family it now owns. | The scope is named PRIVACY, not PRIVACY-OF-HISTORY; it was narrowed to history reads only because stream-history work happened to mint it. §13 files two disclosure guarantees there, and a boundary statement that excluded them would have made the registry's own ownership record false — the failure `.claude/rules/invariants.md` calls out as "never leave the registry describing a guarantee the code no longer makes". The preserved exclusion clause is what routes this SPEC's tier-floor *evaluation* to `ACCESS`, and it is why splitting four guarantees across two scopes is coherent rather than a fudge. **Landed by plan 01-01**; the generated companion is already regenerated. |
| **6.** `.planning/REQUIREMENTS.md` PORTAL-02 and `.planning/ROADMAP.md` Phase 1 success criterion 1 — both: *"the three existing public export surfaces"* | **Four.** §3's inventory enumerates them. The fourth is `WebListPublishedScenes` (`api/proto/holomush/web/v1/web.proto:339`), which returns `repeated PublicSceneArchive` whose `participants_snapshot` (`api/proto/holomush/scene/v1/scene.proto:1053`) is a frozen participant projection served unauthenticated — **in bulk**, one entry per published scene rather than one per request. | Not cosmetic. A Phase-4 census built to the requirement's letter would enumerate three and miss the highest-volume unauthenticated export surface of the set — precisely the missing-census-member failure §12 rule 1 exists to prevent, and precisely the shape research warned about when it said a per-endpoint suite cannot detect the endpoint nobody wrote a test for. Found by plan 01-02's tree enumeration and verified against the tree at the wave boundary, not relayed. **Note:** the surface carries participant **ids** today, not names — `ReadSceneMetaForSnapshot` selects `character_id` (`plugins/core-scenes/publish_store.go:988`) while the proto doc comment claims names; that mismatch is tracked as issue **#4901** and does not affect this row, because the surface is character-returning either way. |
| **7.** `.planning/REQUIREMENTS.md` IDENT-09 and `.planning/research/SUMMARY.md` "Must carry forward" item 3 — both locate the check-then-insert race *"across `internal/bootstrap/setup/adapters.go:38-50` and `internal/auth/character_service.go:112-121`"*, and item 3 argues *"Adding `Rename` doubles the writers into that race."* | Those two sites are the shared existence **query** and **one** writer. There is a **second** writer: `internal/auth/guest_service.go:227`, which calls the same `ExistsByName` inside the guest-name retry-on-collision loop. The race has **one query and two writers** today, and three once `Rename` lands — not two becoming four. §6.1.3 carries the corrected enumeration. | Not cosmetic. A Phase-2 planner sizing the duplicate-detection and index work from the requirement's letter would audit one creation path and miss the guest one — the path that provisions characters **automatically and at volume**, and therefore the one most likely to have produced the pre-existing duplicates the one-shot job must resolve before the unique index can be created. Found by plan 01-03's writer enumeration. The research summary is a dated record and is annotated in place, not rewritten. |
| **8.** **PORTAL-10 rule 5** — `.planning/REQUIREMENTS.md`: *"**Top-level oops code assertions** via `oops.AsOops(err).Code()`; `errutil.AssertErrorCode` chain-walks and passes on double-wrap."* Restated at `.planning/ROADMAP.md` in the PORTAL-10 preamble, Phase 1 criterion 5 and Phase 6 criterion 1; sourced from `.planning/research/SUMMARY.md` and `.planning/research/PITFALLS.md`; originating in `.claude/rules/grpc-errors.md` §*"Wire opacity needs TOP-LEVEL code assertions"*. | **Both halves are false against the pinned dependency**, and rule 5 is restated around the **wire** per §9.6.1 and §12.1: `status.Code(err)`, a generic `status.Convert(err).Message()` with the internal code string absent from it, and a differential two-viewer assertion where the contract is indistinguishability. `errutil.AssertErrorCode` stays **endorsed** for asserting *which* internal code a handler produced. | Under `github.com/samber/oops v1.22.0` (`go.mod:32`), `OopsError.Code()` is documented in the dependency as *"returns the error code from the deepest error in the chain"* and is implemented as a recursive `getDeepestErrorCode` walk; `errutil.AssertErrorCode` (`pkg/errutil/testing.go:15-20`) is a thin wrapper over `oops.AsOops` plus that same `.Code()`. The two spellings are behaviorally identical and **both** pass on a double-wrap. Additionally `oops.AsOops` returns `(OopsError, bool)`, so the single-expression spelling does not compile. **This is the most load-bearing row in this table:** rule 5 is one of the six §12 copies verbatim into every v0.13 plan, so shipping it as written would seed an assertion that cannot fail on the leak it exists to catch into all five remaining phases — the exact *"verification that cannot fail"* PORTAL-10 was written to end. Verified empirically against the pinned version, 2026-08-01. Tracked as issue **#4902**. **The `.claude/rules/grpc-errors.md` half is deliberately NOT applied here:** §9.6.1 already committed to documenting current behavior without changing the rule file, a repo rule file is outside a SPEC-authoring phase's blast radius, and #4902 owns that edit. The planning artifacts, which downstream planners read as directives, are amended. |
| **9.** `.planning/REQUIREMENTS.md` ADMIN-06 — *"in-transaction"* against *"the existing `events_audit`"* — and `.planning/ROADMAP.md` Phase 6 success criterion 3: *"Every admin mutation writes an `events_audit` row **in the same transaction**"* | The durability boundary is the **outbox envelope**, per §10.7: an admin mutation emits its audit envelope in the same transaction as the state change, and the `events_audit` row is **projected** from that envelope. | **No transactional write path into `events_audit` exists, and one would be the wrong thing to build.** The table is written by the asynchronous JetStream audit projection — `projection.persist` (`internal/eventbus/audit/projection.go:319-331`) calling `writeAuditRow`, whose `INSERT INTO events_audit` is at `internal/eventbus/audit/projection.go:434` — plus the retention-partition mover (`internal/eventbus/audit/retention_partitions.go:546`). A Phase-6 planner building to the criterion's letter would find no path and would either invent a bespoke direct insert — bypassing the codec / `dek_ref` / dedup contract `writeAuditRow` maintains, and creating a second writer to a partitioned table — or quietly weaken the criterion to whatever got built. The guarantee ADMIN-06 actually wants, that an admin mutation cannot commit without its audit record being durably queued, is fully delivered by the envelope boundary (`INV-WORLD-1`, at-least-once projection with DLQ capture). Only the sentence naming the wrong table changes. |
| **10.** *Divergence, not an amendment.* The maintainer's stated grid-parity principle: *"Anything that is readable in the same location 'on grid' is visible to other logged in users on the web."* Strict grid-parity puts the in-world `description` at a **player** floor. | The seeded default places `description` at **anonymous** — more open than the principle requires (§8.6, §8.12). | Surfaced and confirmed at context-gathering (D-14). **Grid-parity is the floor the principle guarantees, not a ceiling on what a game may publish**, and an open default is what makes a shareable profile URL worth having. A game wanting strict grid-parity raises `description` to `player` in configuration — one row of the posture table, no code change. This is recorded here so it reads as a **choice rather than an oversight**, which is the whole reason D-14 asked for it in writing. §8.11 carries the same divergence in its own section; this row exists so a reviewer scanning only §14 still finds it. |

### 14.1 What is NOT amended

The blast radius stops here. Three requirements a reader might expect to be
casualties of D-09's no-agency decision survive **unamended**, and a planner
should treat their text as current:

- **PROFILE-03** — *"Each profile field carries server-enforced visibility of
  `public` or `private`, with sane defaults. Enforcement is by omission from the
  response, never client-side hiding."* It never names **who** sets the
  visibility. Server-enforced, sane defaults, and enforcement-by-omission are
  precisely what §8 specifies; the requirement was always about the mechanism.
- **PROFILE-04** — the profile-reachability facet. Re-seated, not rewritten: the
  facet becomes the profile-level tier floor (§8.7), and the not-found-equivalent
  it demands is exactly what `CHARACTER_PROFILE_NOT_FOUND` delivers (§9.6).
- **PROFILE-05** — *"Name and pronouns cannot be set private."* Re-seated as a
  constraint on the **configuration** rather than on an owner (D-13, §8.8). The
  guarantee it states — every reachable profile carries both fields — is
  strictly stronger under the configured model than under an owner-controlled
  one, because no per-character setting can override it.

Nothing else in `.planning/REQUIREMENTS.md`, `.planning/ROADMAP.md` or the
research corpus is superseded by this SPEC. In particular the **v2 requirements**
and the **Out of Scope** table are carried forward unchanged, the latter into §15.

## 15. Out of Scope

Explicitly excluded, each with its reason. This is a numbered section rather than
a footnote because **an omission is not an exclusion** — a reader who cannot find
a feature here has no way to tell whether it was considered and rejected or
simply forgotten, and the difference is what stops it being smuggled back in
during planning.

### 15.1 Carried forward from the milestone requirements

Every entry from `.planning/REQUIREMENTS.md`'s Out-of-Scope table, with its
reason, unchanged:

| Excluded | Reason |
| --- | --- |
| Freeform HTML/CSS in profiles | Samy-worm class; CSS alone still exfiltrates under CSP. |
| Over-granular privacy matrices | Nextcloud's own docs concede their 3×4 cross-product confuses users. |
| Per-character-pair visibility | IC-knowledge modelling wearing a privacy hat; not a privacy control. |
| Relationship-web graphs | Consent, staleness, and N² privacy-filtered reads. |
| Raw DB/SQL console in the admin panel | Bypasses every ABAC gate and all audit emission. |
| Hardcoded break-glass admin identifiers | Unauditable standing privilege. |
| Admin impersonation | Launders the audit trail — actions attribute to the wrong actor. |
| Hard-delete on retire | Irreversible; FK references from `character_roles` (CASCADE), `scene_participants`, `player_character_bindings`, and `locations.owner_id`/`objects.owner_id` (no `ON DELETE` — errors at runtime). §4.4 keeps retire, idle-out and purge three distinct operations for this reason. |
| Dashboard-first MVP with stub sections | A dashboard of empty cards is the rot pattern EXT-02 exists to prevent; §10.3 requires the six planned sections to refuse **after** the gate instead. |
| Nav nesting deeper than 2 levels | Both surveyed admin-IA libraries explicitly reject it. |
| Role mutation in character administration | PORTAL-08 — see §15.3, which states this one emphatically because it is the one most likely to be mistaken for an oversight. |
| World/building editing surfaces | Still SPEC-less; unchanged from the PROJECT.md Out-of-Scope entry. |
| `@testing-library/svelte`, `vitest-browser-svelte` | The repo already has a working `mount`/`unmount` component-test project (17 files); the latter is a whole-suite migration, filed separately. |
| Any query-cache layer in the web client | Creates a second source of truth against the live `StreamEvents` push feed. |
| Proto `reserved` ranges as an extensibility claim | Hygiene only — see §15.4, which states what actually carries the extensibility constraint. |

### 15.2 Deferred by this phase

Four exclusions this SPEC adds, each with the seam it leaves:

| Deferred | Seam left, and why it is deferred |
| --- | --- |
| **An admin editing surface for the visibility-floor configuration** | v0.13 ships the model plus seeded defaults only (D-15, §8.12). The editor arrives when the `config` admin section — already registered, role-gated and returning `NOT_IMPLEMENTED` after the gate per EXT-01/EXT-02 — gets its handler body. That makes this a **body replacement, not new wiring**, and gives the deferred section its first concrete tenant. Rejected: a minimal editor in Phase 6, which the roadmap already flags as the highest-risk phase. |
| **The full `docs/superpowers/` retirement sweep** | Phase 1 does the **pointer update only** (D-19). The sweep touches ~20 files, several of them live gates — the orphan-check walk root (`test/meta/invariant_registry_test.go:341`), `scripts/docs-paths-regex.sh` and `scripts/lint-docs-paths-sync.sh` (which decide whether a PR takes the docs fast lane), `scripts/adr-doctor.sh`, `gorules/plugin.go`, `Taskfile.yaml` — plus relocation of 140 spec files. It gets its own issue precisely because it touches the gates where a silent fail-open would live. |
| **A struct-literal lint** banning character-shaped proto literals outside the projection package | Considered and **consciously not mandated** (D-04, §2.6). The census already fails RED for every case that matters. A future PR adding the lint is an **increment, not a correction**: it does not indicate this SPEC was wrong, and it MUST NOT be treated as a prerequisite for anything in v0.13, nor as grounds for relaxing the census. |
| **A fourth viewer-tier rung**, and the representation of the visibility configuration for the future editor | Raised as possible discussion topics and not pursued. The tier ladder is a **string enum** (§8.2), so a fourth rung is an append — provided §8.6's exhaustive `switch` with a denying `default` is honored, which is what keeps an unimplemented rung from failing **open**. |

### 15.3 Role mutation — stated emphatically

**Role mutation is not part of character administration in this milestone. An
omission is not an exclusion.**

This is PORTAL-08, and it is stated in this form because it is the exclusion most
likely to be read as an oversight and quietly added. Two facts make it a security
decision rather than a scoping one:

- `PlayerHasRole` (`internal/store/role_store.go:83-93`) is **player-wide**: it
  returns true iff *any* character of the player holds the role. A role granted
  to a throwaway alt therefore confers it **everywhere**. That is deliberate and
  documented in the operator/break-glass path
  (`internal/admin/auth/ingame.go:116` — *"RoleAdmin (any character)"*), not a
  defect; v0.13 is simply the first work that would load those semantics onto a
  new surface.
- The decision on player-wide versus per-character role semantics is tracked as
  issue **#4899** and **MUST land before any admin surface exposes role
  mutation**, because `WebCheckSessionResponse` needs a role field shaped to
  match and adding it later is a wire-compat change to every caller.

The mechanism enforcing the exclusion is ADMIN-04's **field-mask allowlist that
excludes roles** (§10.6) — an allowlist, so a role path is excluded by not being
enumerated rather than by a deny-list someone must remember to extend.

### 15.4 Proto field-number reservation is hygiene, not extensibility

**A `reserved` range in a `.proto` file MUST NOT be presented as discharging this
milestone's extensibility constraint, and MUST NOT be cited in any REQ-ID,
success criterion or plan as if it had.**

All three researchers who considered it agreed it is a **documentation
convention** only: reserve numbers, with a comment naming the deferral issue, and
nothing more. Two reasons it cannot carry more weight:

- Reserved capacity carries an implicit false promise that *"the hard part is
  done"* — the reserved-capacity-that-rots pattern. The hard part is the
  authorization descriptor, the policy family, and the projection; a number is
  not any of them.
- Under this SPEC's **absence-means-hidden** semantics (§2.7, §8.9), a reserved
  non-`optional` scalar that later becomes real **serializes an empty value from
  day one**. That is a fail-open placeholder in precisely the position where
  absence is the enforcement mechanism.

What actually carries the extensibility constraint, and where each is specified:

| Carrier | Where |
| --- | --- |
| The admin **section registry** with a mandatory authorization descriptor — no default, no zero value meaning allow — and the registry↔descriptor set-equality census | §10.1, §10.2; EXT-01, EXT-03, EXT-04 |
| The **ABAC namespace** — `admin_section:` as a resource type, with `seed:admin-section-access` covering every future section at zero additional policy cost | §10.4; EXT-07 |
| The **property-row media model** — proven by inserting one primary and ten gallery rows through the real schema, demonstrating the no-migration-later claim rather than asserting it | §7.3, §9.7; EXT-05, EXT-06 |

## 16. Grounding Trace

Every `path:line` citation in this document was extracted mechanically from the
document itself and opened against the tree at commit **`5817edab`** on branch
`gsd/v0.13-web-portal-identity-admin-foundations`, on **2026-08-01**. The sweep
covered every citation in §§1–15: **189 distinct path-bearing citations**,
appearing **220 times**, plus **20** written as a bare `` `:N` `` continuing the
path named immediately before them — **56 files** in all. **Six** were
corrected, **none** was removed, and the remaining 183 resolved unchanged, as did
all 20 bare continuations. The per-citation dispositions are recorded in
`01-06-SUMMARY.md`. This section restates a subset of those citations and
introduces no new one.

**These citations are point-in-time and are expected to drift.** This SPEC is a
design record, not a file whose citations a CI gate keeps green: no check
re-resolves them, and the tree will move underneath them. A reader who finds a
citation that no longer lands where this document says it does should re-derive
the construct by name rather than assume the claim is wrong — the line moved, the
argument did not. Where a claim depends on a construct's *content* rather than
its location, the content is quoted inline in the section that makes the claim,
so the quote survives the line number.

The list below is grouped by what each citation grounds. It is flat by design: a
reviewer can walk it and re-verify the grounding without reading the sections
that consume it. Line lists sharing one bullet name one construct shape repeated
across those lines.

### 16.1 Access control, ABAC and profile visibility (§7.4, §8, §10.4)

- `internal/access/policy/seed.go:51-54` — the `seed:player-character-colocation`
  policy whose `resource.character.location == principal.character.location`
  clause §7.4 argues is a location gate, not a privacy gate.
- `internal/access/policy/seed.go:110-145` — the six shipped `seed:property-*`
  policies the §8.4 tier-floor family extends.
- `internal/access/policy/attribute/property.go:80-86` — `PropertyProvider`'s
  attribute bag, emitting `name` alongside `parent_type` / `parent_id` /
  `visibility`; the reachability §8.5's Phase-2 obligation confirms.
- `internal/access/prefix.go:23-33` — the shipped resource-prefix family
  `admin_section:` joins (§10.4).
- `internal/store/migrations/000001_baseline.up.sql:261` — `CHECK (source IN
  ('seed', 'lock', 'admin', 'plugin'))`, the vocabulary that already models the
  `source='admin'` override rows §8.4 uses.
- `internal/store/migrations/000001_baseline.up.sql:350-371` — the whole
  `entity_properties` table: the per-row `visibility` CHECK (`:358`) and
  `entity_properties_parent_name_unique` (`:364`).
- `internal/world/service.go:1144-1171` — `ListPropertiesByParent`'s three-way
  filter loop; the `ErrAccessEvaluationFailed` arm that aborts is the shipped
  precedent for §8.10's fail-closed rule and binds INV-ACCESS-10.

### 16.2 World model, lifecycle and concurrency (§4, §9.4)

- `internal/store/migrations/000001_baseline.up.sql:67-76` — the `characters`
  table as it stands: seven columns, no `status`, and only
  `idx_characters_location` (`:76`).
- `internal/store/migrations/000001_baseline.up.sql:259`, `:294`, `:358` — the
  three further enum-by-`CHECK` precedents §4.1 follows.
- `internal/store/migrations/000001_baseline.up.sql:80`, `:84`, `:99`, `:143` —
  the four foreign keys into `characters(id)`: `ON DELETE SET NULL`, `ON DELETE
  CASCADE`, and two with no `ON DELETE` clause at all (§4.4's purge blast radius).
- `internal/store/migrations/000001_baseline.up.sql:83-87` — `character_roles`
  keyed `(character_id, role)`: roles stored per character (§10.5).
- `internal/store/migrations/000049_world_version_guard.up.sql:20` —
  `version INTEGER NOT NULL DEFAULT 1`, the column §9.4.1 transcribes `int32` from.
- `internal/world/character.go:29` — `Version int` on the domain type.
- `internal/world/service.go:745-777` — `DeleteCharacter`: the ABAC `delete` gate,
  the property cascade, and the tombstone envelope in one transaction (§4.4).
- `internal/world/service.go:799-836` — `UpdateCharacterDescription`, the shipped
  same-transaction outbox seam §9.3 mandates, with the CAS-guard comment and the
  `ErrConcurrentEdit` mapping at `:820-828`.
- `internal/world/errors.go:19-20`, `:20-21`, `:22`, `:26` — the propagate-unchanged
  comment, the deliberately-distinct-from-`ErrNotFound` rationale, the
  `ErrConcurrentEdit` sentinel, and `CodeConcurrentEdit = "WORLD_CONCURRENT_EDIT"`.
- `internal/world/postgres/character_repo.go:82-85` — the CAS predicate appended
  **only** when `char.Version > 0`; the live zero-means-unguarded affordance
  §9.4.2 rejects at the RPC boundary.
- `internal/world/postgres/character_repo.go:120`, `:134` — the same affordance on
  `Delete`, documented and enforced only above zero.
- `internal/world/postgres/character_repo.go:97`, `:129`, `:135` — the
  `CHARACTER_NOT_FOUND` and `WORLD_CONCURRENT_EDIT` stamps §9.6 inherits.

### 16.3 The read-surface inventory (§3)

- `api/proto/holomush/core/v1/core.proto:74`, `:80`, `:85`, `:90`, `:95`, `:99`,
  `:107`, `:129`, `:154`, `:169` — the ten `CoreService` rpc declarations
  inventoried in §3.3, one `rpc <Name>(...) returns (...);` per line.
- `api/proto/holomush/web/v1/web.proto:157`, `:162`, `:167`, `:173`, `:177`,
  `:182`, `:187`, `:207`, `:222`, `:252`, `:329`, `:339`, `:345`, `:351` — the
  fourteen `WebService` rpc declarations, same shape.
- `api/proto/holomush/world/v1/world.proto:30`, `:38` and
  `api/proto/holomush/plugin/host/v1/world.proto:28`, `:33` — the world-service
  and plugin-host-service rpc declarations.
- `api/proto/holomush/core/v1/core.proto:688-710` and `web.proto:496-513` — the
  two `CharacterSummary` messages, each carrying the four presence-telemetry
  fields §2.4 removes from the `public` audience (`web.proto:503`, `:506`,
  `:509`, `:512`).
- `api/proto/holomush/core/v1/core.proto:902-907` — `CharacterDirectoryEntry`,
  already identity-only.
- `api/proto/holomush/core/v1/core.proto:428-441` — `PresenceEntry`, whose
  `:439-440` comment records the deliberately-omitted arrival timestamp §2.7 cites
  as an in-tree omit-don't-publish precedent.
- `api/proto/holomush/core/v1/core.proto:105-106` — the `ListAllCharacters` doc
  comment drawing the privacy line §3.5 promotes into INV-ACCESS-12.
- `api/proto/holomush/world/v1/world.proto:77-91` — `CharacterInfo`, including the
  `player_id` field at `world/v1/world.proto:81`.
- `api/proto/holomush/plugin/host/v1/world.proto:96-108`, `:123-128` — the
  plugin-facing inline character response and `CharacterSummary`.
- `api/proto/holomush/web/v1/web.proto:960-968` — `WebPresenceEntry`.
- `core.proto:279`, `:471`, `:742`, `:781`, `:817`, `:843`, `:872`, `:887`,
  `:912`, `:980`, `:1102` and `web.proto:540`, `:580`, `:607`, `:631`, `:656`,
  `:669`, `:682`, `:745`, `:832-835`, `:987` — the response fields each
  inventory row names, one field declaration per line.
- `api/proto/holomush/web/v1/web.proto:426-429` — `GameEvent.actor`, the bare
  `string` display name that makes a type-driven census predicate insufficient
  (§3.2's name-reachable category).
- `web.proto:1136-1143`, `:1176`, `:1195`, `:1197`, `:1216-1221` and
  `api/proto/holomush/sceneaccess/v1/sceneaccess.proto:143`, `sceneaccess.proto:157`,
  `:164`, `:171` — the four public export surfaces' response messages and the
  facade RPCs they proxy.
- `api/proto/holomush/scene/v1/scene.proto:325-338`, `:820-827`, `:1012-1027` —
  `ParticipantInfo`, `PublishedSceneEntry` and `CharacterSceneInfo`: the three
  messages §3.2 places deliberately outside the census predicate, with
  `:1013-1015` carrying the roster-fields-unset comment §3.1 uses as its
  worked rule-1 example.
- `api/proto/holomush/web/v1/web.proto:733-746` — `WebCheckSessionResponse` as it
  stands, with no role field, which §10.5.1 adds one to.
- `docs/superpowers/specs/2026-05-23-scenes-phase-6-logs-vote-privacy-design.md:262-267`
  — the deliberately-separate-handlers argument §2.2 extends one layer down.

### 16.4 Name capture and normalization (§5, §6)

- `api/proto/holomush/comm/v1/comm.proto:25-28` — `actor_display_name`, the
  canonical capture; `:25-27` is the comment recording that it is empty while
  scene name resolution is deferred.
- `pkg/plugin/comm/builder.go:41`, `:48`, `:55` — the three builders stamping
  `a.Name` into that field at emit time.
- `internal/store/migrations/000052_events_audit_partition.up.sql:114` — the
  opaque `envelope BYTEA` the host audit table freezes the payload into; `:119` is
  the `rendering JSONB` column §5.2 checks and excludes.
- `internal/eventbus/types.go:127-134` — `RenderingMetadata`'s six fields, none a
  character name, grounding that exclusion.
- `plugins/core-scenes/migrations/000004_create_scene_log.up.sql:23` — the
  plugin-owned `payload BYTEA`.
- `plugins/core-scenes/migrations/000008_scene_publication.up.sql:21`, `:23` —
  `content_entries JSONB` and `participants_snapshot JSONB`.
- `plugins/core-scenes/publish_snapshot.go:152` — the single write of
  `ParticipantsSnapshot` at the PUBLISHED transition; `:375` and
  `plugins/core-scenes/commands.go:107` assign `speaker` from `pl.ActorID`.
- `plugins/core-scenes/publish_store.go:956-960` — the type comment stating name
  resolution is a follow-up; `:987-1002` and `:988` are the
  `SELECT character_id FROM scene_participants` query that makes it so (§5.4,
  issue #4901).
- `api/proto/holomush/scene/v1/scene.proto:873-874`, `:957-958`, `:1052-1053` —
  the three `participants_snapshot` declarations whose doc comments claim names,
  and `:821` / `scene.proto:822` for `speaker`; the disagreement §5.4 records.
- `internal/web/translate.go:25-26`, `:88-96` — the legacy `character_name` /
  `sender_name` keys and the fallback chain that still reads them.
- `plugins/core-scenes/export.go:103` — the live-read-of-frozen-bytes comment that
  makes §5.1 rule 1 a write-path test rather than a read-path one.
- `plugins/core-scenes/service.go:522-528`, `:534-538`, `:1504-1513` and
  `api/proto/holomush/scene/v1/scene.proto:328-330` — the roster's per-read
  best-effort name lookup with its documented id fallback.
- `plugins/core-scenes/poseorder.go:18-23`, `:76` and
  `plugins/core-scenes/service.go:2015` — the pose queue's same shape, with `nil`
  passed for the names map today.
- `internal/grpc/auth_handlers.go:352`, `:380`, `:413`, `:505` — the four
  `CharacterName` assignments from a freshly-read row, which is why §5.2 classes
  those scalars `live`.
- `internal/store/migrations/000001_baseline.up.sql:71` — `characters.name`, the
  anchor row a rename writes.
- `internal/world/validation.go:114-126` — `NormalizeCharacterName`: whitespace
  collapse plus per-word title case, and nothing else; `:69-105` is
  `ValidateCharacterName`; `:60` is `characterNameRegex`, the letters-and-spaces
  shape §6.1.5's honest note turns on.
- `internal/bootstrap/setup/adapters.go:38-50` — the shared `ExistsByName` query
  comparing `LOWER(name)`; `internal/auth/character_service.go:112-121` and
  `internal/auth/guest_service.go:227` are the **two** writers racing on it
  (§6.1.3, §14 row 7).
- `internal/auth/player.go:24-25`, `:31`, `:167` — the username length bounds, the
  ASCII regex, and its application; `internal/store/migrations/000001_baseline.up.sql:54`
  is the real `UNIQUE` constraint §6.2 contrasts with the character-name race.

### 16.5 Profile storage and media (§7)

- `internal/store/migrations/000001_baseline.up.sql:72` — `characters.description`,
  the in-world `look` text §7.4 governs.
- `internal/store/migrations/000001_baseline.up.sql:350-371`, `:364` — the
  `entity_properties` mechanism and the uniqueness constraint that makes
  exactly-one-primary a database guarantee (§7.3), with no DDL for a twelfth
  field or an eleventh image (§7.1).
- `internal/store/migrations/000045_character_preferences.up.sql:5` —
  `characters.preferences JSONB`, the settings column §7.2 forbids the OOC
  RP-preferences block from being written into.

### 16.6 Admin surface (§10)

- `internal/store/role_store.go:83`, `:83-93`, `:86-93` — `PlayerHasRole`'s
  own comment (*"true iff any character of playerID has role"*) and the query
  joining on `c.player_id`: the shipped per-player read §10.5 matches.
- `internal/admin/auth/ingame.go:116`, `:117-118`, `:119` — the
  capability-plus-role step comment, the re-assert-at-every-entry-point rationale
  §10.4 transposes, and the call passing a **player** id.
- `internal/admin/auth/operator_admin.go:37-64`, `:53` — `AssertOperatorAdmin` in
  full and its `PlayerHasRole` call.
- `web/src/lib/nav/sections.ts:35-47`, `:47`, `:63-67` — the ordered
  `as const satisfies` registry with its compile-time rationale, the derived
  `SectionId` union, and `visibleSections`; §10.1 mirrors the shape, not the
  location.
- `internal/eventbus/audit/projection.go:319-331`, `:434` and
  `internal/eventbus/audit/retention_partitions.go:546` — the asynchronous
  projection and partition mover that are the **only** writers into
  `events_audit`, which is why §10.7 and §14 row 9 put the durability boundary at
  the outbox envelope.

### 16.7 The update mask, the error surface and verification (§9.5, §9.6, §12)

- `internal/grpc/sceneaccess_service.go:843-845` — the rejection rationale §9.5
  adopts verbatim; `:846-853` is the closed allowlist map; `:861-902` is
  `UpdateScene` entire; `:870-874` and `:872` are the exact-string check and its
  `InvalidArgument`; `:878-880` and `:862-880` are the empty-mask short-circuit
  and the ownership-first ordering §9.5 makes normative.
- `go.mod:32` — `github.com/samber/oops v1.22.0`, the pinned version §9.6.1 and
  §12.1 rule 5 were verified against.
- `pkg/errutil/testing.go:15-20` — `AssertErrorCode`, a thin wrapper over
  `oops.AsOops` plus `.Code()`; `:17` and `internal/session/reaper.go:167` show
  `oops.AsOops` returning `(OopsError, bool)`, which is why the single-expression
  spelling does not compile (§14 row 8, issue #4902).
- `.planning/REQUIREMENTS.md` PORTAL-10 (`:51-78`) — the source ordering the six
  §12.1 rules preserve.
- `.planning/research/PITFALLS.md:89-100`, `:233-240`, `:338-344`, `:392-400` —
  the four inverted-test-question passages §12 quotes: the empty-fixture private
  field, the non-degenerate-fixture pairing, the denial test aimed at an endpoint
  with nothing to deny, and the `err != nil` that cannot say which gate fired.
- `.planning/research/SUMMARY.md:153-154` — the census-with-set-equality
  recommendation §2.6 and §12.1 rule 1 implement.
- `.planning/phases/01-portal-spec/01-CONTEXT.md:163-169` — D-17, the decision
  that binds §12 by verbatim copy into every downstream plan.
- `test/meta/invariant_registry_test.go:341` — the orphan check's hard-coded walk
  root, `docs/superpowers/specs`, which is why §12.1 rule 6 and §13 require this
  SPEC's registry entries to be hand-registered.

### 16.8 What this trace does not cover

Three classes of claim in this document are **not** grounded by a `path:line`
citation, deliberately:

- **Forward references to Phase-2-through-6 artifacts.** `PublicCharacter`,
  `projectPublic`, `CharacterAccessService`, `admin_section:`,
  `AdminSectionResource()` and the tier-floor policy family do not exist yet.
  §3.3 and §9 mark them `Phase 4` or `Phase 2` in place of a line number, which
  is the honest form: a citation would have to be invented.
- **Rule-file references without a line.** `.claude/rules/abac-providers.md`,
  `gateway-boundary.md`, `grpc-errors.md`, `plugin-runtime-symmetry.md`,
  `database-migrations.md`, `proto-doc-comments.md`, `testing.md` and
  `invariants.md` are cited by file and section heading rather than by line,
  because rule files are reorganized often and a heading survives what a line
  number does not.
- **External standards.** UTS #24, UTS #39 and its Moderately Restrictive profile
  are named as specifications so Phase 2 implements against the document rather
  than against this one's paraphrase (§6.1.2).
