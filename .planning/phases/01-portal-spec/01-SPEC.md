# HoloMUSH v0.13 Web Portal — Portal SPEC

- **Status:** Draft — sections 1, 7, 8 and the section-13 opening authored (plan 01-01); remaining sections filled by plans 01-02 through 01-06
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
| `holomush.world.v1.WorldService.GetCharacter` | `api/proto/holomush/world/v1/world.proto:30` | `public` | `CharacterInfo` (`world.proto:157-160`) | ABAC-gated `read` on the character resource. Carries `player_id` (`world.proto:81`) — an OOC player↔character linkage. Phase 4 MUST decide whether `PublicCharacter` retains it; it is not identity the `public` audience obviously needs. |
| `holomush.world.v1.WorldService.ListCharactersAtLocation` | `api/proto/holomush/world/v1/world.proto:38` | `public` | `repeated CharacterInfo` (`world.proto:177-181`) | Returns an empty list, never `NOT_FOUND`, for an empty location — a rule-1 member with a routinely-empty result. |
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
| `holomush.web.v1.WebService.WebQueryStreamHistory` | `api/proto/holomush/web/v1/web.proto:222` | `public` | `repeated GameEvent` (`web.proto:832-834`), whose `actor` field is *"the DISPLAY NAME of the acting character, extracted from the event payload"* (`web.proto:427-429`) | Name-reachable, and the clearest case that a type predicate alone is insufficient: the leaked value is a bare `string`. |
| `holomush.web.v1.WebService.WebListFocusPresence` | `api/proto/holomush/web/v1/web.proto:252` | `public` | `repeated WebPresenceEntry` (`web.proto:987`) | Web twin of `ListFocusPresence`. |

#### The `WebListAllCharacters` split (§2.4)

| RPC | Proto location | Audience | Character-shaped message returned | Notes |
| --- | --- | --- | --- | --- |
| `holomush.web.v1.WebService.WebListAllCharacters` | `api/proto/holomush/web/v1/web.proto:187` | `public` | `repeated holomush.core.v1.CharacterDirectoryEntry` (`web.proto:682`) | **Removed in v0.13.** Present in this table so the census's expected set describes the pre-split tree; Phase 4 deletes this row in the same change that deletes the RPC. |
| `holomush.web.v1.WebService.WebListCharacterDirectory` | Phase 4 | `public` | `repeated PublicCharacterSummary` | Replacement. Identity only; drops `has_active_session`, `session_status`, `last_location`, `last_played_at`. |
| `holomush.web.v1.WebService.WebAdminListCharacters` | Phase 4 | `admin` | `repeated AdminCharacter` | Replacement. The rich row, reached through the admin surface (§10). |

#### The three existing public export surfaces

These are **already live and already unauthenticated**. Each publishes
denormalized character names to any reader, and each is the reason §5 exists.
Every one is inventoried in **both** this table and §5's name-capture table.

| RPC | Proto location | Audience | Character-shaped message returned | Notes |
| --- | --- | --- | --- | --- |
| `holomush.web.v1.WebService.WebExportScene` | `api/proto/holomush/web/v1/web.proto:329` | `public` | rendered `bytes content` (`web.proto:1136-1143`) containing per-line speaker labels | Proxies `holomush.sceneaccess.v1.SceneAccessService.ExportScene` (`api/proto/holomush/sceneaccess/v1/sceneaccess.proto:143`). Name-reachable through opaque bytes — no proto field names a character, so only an explicit enumeration reaches it. Publishes other characters' names to the requesting participant. |
| `holomush.web.v1.WebService.WebGetPublicSceneArchive` | `api/proto/holomush/web/v1/web.proto:345` | `public` | `repeated string participants_snapshot` (`web.proto:1195`) and `repeated PublishedSceneEntry content_entries` (`web.proto:1197`), each entry carrying `speaker` (`scene.proto:822`) | Proxies `SceneAccessService.GetPublicSceneArchive` (`sceneaccess.proto:164`). **Unauthenticated.** Publishes a frozen list of participant character names to anonymous readers. A later privacy change cannot reach this snapshot. |
| `holomush.web.v1.WebService.WebDownloadPublicSceneArchive` | `api/proto/holomush/web/v1/web.proto:351` | `public` | rendered `bytes content` (`web.proto:1216-1221`) | Proxies `SceneAccessService.DownloadPublicSceneArchive` (`sceneaccess.proto:171`). **Unauthenticated.** The download form of the row above; same names, rendered rather than structured, so likewise reachable only by explicit enumeration. |

`holomush.web.v1.WebService.WebListPublishedScenes` (`api/proto/holomush/web/v1/web.proto:339`,
proxying `sceneaccess.proto:157`) returns `repeated holomush.scene.v1.PublicSceneArchive`
(`web.proto:1176`), whose `participants_snapshot` (`scene.proto:1053`) carries the
same frozen names in list form. It is a **fourth** public export surface, and it is
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

> *Placeholder — authored by plan 01-03.*

## 6. Name Normalization Policy

> *Placeholder — authored by plan 01-03.*

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

### 8.6 The configured postures

The table below is the whole configuration surface. Its rows are the governed
attribute names; the three posture columns are worked examples of the same table
expressing three different games; the final column is what v0.13 seeds.

| Governed attribute | Scrape-friendly game | Guest-floor game | Players-only game | **Seeded v0.13 default** |
| --- | --- | --- | --- | --- |
| *profile reachability* | `anonymous` | `guest` | `player` | **`anonymous`** |
| `name` | `anonymous` | `guest` | `player` | **`anonymous`** |
| `pronouns` | `anonymous` | `guest` | `player` | **`anonymous`** |
| in-world description (`characters.description`) | `anonymous` | `guest` | `player` | **`anonymous`** |
| `profile.rumors` | `anonymous` | `guest` | `player` | **`guest`** |
| `profile.currently` | `anonymous` | `guest` | `player` | **`guest`** |
| `profile.preferences` | `anonymous` | `guest` | `player` | **`guest`** |
| `profile.timezone` | `anonymous` | `guest` | `player` | **`guest`** |
| every other `profile.*` field (§7) | `anonymous` | `guest` | `player` | **`guest`** |

Read the postures as columns, not as three configurations: it is **one** table,
and a game picks a floor per row. The three columns exist to show that the same
mechanism expresses a game that wants a character fully scrapable by anonymous
visitors, a game that wants guests as the floor for most things, and a game that
wants authenticated players as the floor for everything.

**Totality rule.** Every governed attribute **MUST** carry an explicit floor. Any
`profile.*` attribute not individually assigned one **MUST** default to `guest`,
never to `anonymous`. An unset floor **MUST NOT** be read as "allow" — a
zero-value-means-allow default is the fail-open shape this milestone forbids
elsewhere, and it would mean that adding a profile field silently publishes it.

*Note on the profile-reachability row.* Reachability is a facet **above** the
fields: it governs whether the profile resolves at all, and it is evaluated
before any per-field floor. §8.7 and §8.8 constrain it.

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

> *Placeholder — authored by plan 01-04.*

## 10. Admin Information Architecture

> *Placeholder — authored by plan 01-04.*

## 11. Sorting and Filtering Verdict

> *Placeholder — authored by plan 01-04.*

## 12. Verification Integrity

> *Placeholder — authored by plan 01-05.*

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

**All seven ship `binding: pending`.** Their asserting tests do not exist until
Phase 2 or Phase 4, and a `// Verifies:` annotation on a test that does not
genuinely assert the guarantee is a false-green — the documented failure this
registry's binding ratchet exists to catch. A `pending` entry carries no
`asserted_by`.

## 14. Amendments and Divergences

> *Placeholder — authored by plan 01-05.*

> **Queued for plan 01-05 — SIX amendments, not four.** `01-CONTEXT.md:197-202`
> drafts four rows (ROADMAP Phase 4 criterion 3, ROADMAP Phase 5 criterion 4,
> REQUIREMENTS PROFILE-12, research SUMMARY CONFLICT 4). Plan 01-01 added a
> **fifth** and plan 01-02's tree enumeration forced a **sixth**. 01-05 MUST
> carry both:
>
> | Artifact | Amendment |
> | --- | --- |
> | `docs/architecture/invariants.yaml` — the `INV-PRIVACY` scope record | The `boundary:` first sentence read *"Privacy-relevant gating on history reads."* and now reads *"Privacy-relevant gating on reads."* The scope is named PRIVACY; it was narrowed to history reads only because stream-history work minted it. The `"Does NOT include: ABAC policy evaluation (→ INV-ACCESS), subscribe authorization (→ INV-EVENTBUS)"` clause is **preserved verbatim and MUST NOT be widened** — that clause is what routes this SPEC's tier-floor evaluation to `ACCESS` (§13), and it is why splitting the four guarantees across two scopes is coherent rather than a fudge. The `description:` enumeration was extended in the same edit so the scope record describes the entry family it now owns. Landed by plan 01-01. |
> | `.planning/REQUIREMENTS.md:31` (PORTAL-02) and `.planning/ROADMAP.md:132` (Phase 1 success criterion 1) | Both read *"including the **three** existing public export surfaces"*. The tree has **four**. Plan 01-02's enumeration found `WebListPublishedScenes` (`api/proto/holomush/web/v1/web.proto:339`) returning `repeated PublicSceneArchive`, whose `participants_snapshot` (`api/proto/holomush/scene/v1/scene.proto:1053`) carries the same frozen participant names as `WebGetPublicSceneArchive` — **in bulk**. Amend both to *"four"*. This is not cosmetic: a Phase-4 census built to the requirement's letter would enumerate three and miss the highest-volume unauthenticated name-export surface of the set, which is precisely the missing-census-member failure PORTAL-10 rule 1 exists to prevent. Verified against the tree by the orchestrator at the wave-2 boundary, not relayed. |

## 15. Out of Scope

> *Placeholder — authored by plan 01-05.*

## 16. Grounding Trace

> *Placeholder — authored by plan 01-06.*
