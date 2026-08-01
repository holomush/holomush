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

> *Placeholder — authored by plan 01-02.*

## 3. Read-Surface Inventory

> *Placeholder — authored by plan 01-02.*

## 4. Character Lifecycle

> *Placeholder — authored by plan 01-03.*

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

The four guarantees below are split by what each one **is**. The two disclosure
guarantees — what the response reveals about existence and about minimum identity
— are `PRIVACY`. The two evaluation guarantees — the read-time authorization
decision and the wire shape that decision enforces — are `ACCESS`, which is where
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

**All four ship `binding: pending`.** Their asserting tests do not exist until
Phase 4, and a `// Verifies:` annotation on a test that does not genuinely assert
the guarantee is a false-green — the documented failure this registry's binding
ratchet exists to catch. A `pending` entry carries no `asserted_by`.

## 14. Amendments and Divergences

> *Placeholder — authored by plan 01-05.*

> **Queued for plan 01-05 — FIVE amendments, not four.** `01-CONTEXT.md:197-202`
> drafts four rows (ROADMAP Phase 4 criterion 3, ROADMAP Phase 5 criterion 4,
> REQUIREMENTS PROFILE-12, research SUMMARY CONFLICT 4). Plan 01-01 added a
> **fifth**, which 01-05 MUST carry:
>
> | Artifact | Amendment |
> | --- | --- |
> | `docs/architecture/invariants.yaml` — the `INV-PRIVACY` scope record | The `boundary:` first sentence read *"Privacy-relevant gating on history reads."* and now reads *"Privacy-relevant gating on reads."* The scope is named PRIVACY; it was narrowed to history reads only because stream-history work minted it. The `"Does NOT include: ABAC policy evaluation (→ INV-ACCESS), subscribe authorization (→ INV-EVENTBUS)"` clause is **preserved verbatim and MUST NOT be widened** — that clause is what routes this SPEC's tier-floor evaluation to `ACCESS` (§13), and it is why splitting the four guarantees across two scopes is coherent rather than a fudge. The `description:` enumeration was extended in the same edit so the scope record describes the entry family it now owns. Landed by plan 01-01. |

## 15. Out of Scope

> *Placeholder — authored by plan 01-05.*

## 16. Grounding Trace

> *Placeholder — authored by plan 01-06.*
