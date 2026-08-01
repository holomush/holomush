# Pitfalls Research

**Domain:** Adding identity surfaces (character CRUD, public profiles with per-field privacy) and an admin portal to an existing, mature, default-deny-ABAC game platform
**Researched:** 2026-07-31
**Confidence:** HIGH for claims grounded in this repo (every such claim cites a file read during this research); MEDIUM for the extensibility-rot failure modes (§4), which are pattern-level rather than code-grounded.

---

## How to read this file

Every pitfall below carries a **Inverted test question** — the v0.12 lens, stated in
`.planning/MILESTONES.md:24`:

> *"is this check's passing state reachable without the property holding?"*

v0.12 catalogued ~17 instances of a verification that cannot fail, and the milestone audit
found the same shape one level up (a promised coverage gate that did not exist; a required
`Vuln` check that could never pass). A companion failure was also recorded: **a correct
implementation of a wrong spec passes every test derived from that spec** — six review
checkpoints approved a CODEOWNERS gate that was exempt at `docs/CODEOWNERS` because the
spec named only two of GitHub's three honored locations.

Privacy and authorization are exactly the domains where that failure is cheapest to make
and most expensive to discover. So for each privacy/authz pitfall this file states:

1. The test a team would naturally write to assert the property, and
2. **how that test passes while the property is false.**

Treat #2 as the acceptance criterion, not #1. A phase's DoD should read *"the negative
test was demonstrated RED against the pre-fix state"*, not *"a test exists."*

**Grounding note on why this milestone is high-risk specifically:** there are **zero
`RoleAdmin` references anywhere under `internal/web/`** (verified by grep during this
research). Every existing admin authorization in the codebase lives on the Unix-socket
operator path (`internal/admin/auth/`) or in `internal/access/`. The `/admin` web route
is a **brand-new trust boundary**, not an extension of an existing one — there is no
in-repo precedent for the web gateway making an admin decision, and therefore no existing
test suite that would notice if the new one is wrong.

---

## Critical Pitfalls

### Pitfall 1: The API returns the whole object and the client hides fields

**What goes wrong:**
`GetCharacterProfile` returns a fully-populated `CharacterProfile` message and the Svelte
component renders only the fields whose `visibility` flag says public. The private fields
travel over the wire in plaintext and are visible in the browser's network tab, in the
ConnectRPC response body, and to any non-browser client that speaks the same RPC.

**Why it happens:**
Per-field privacy is naturally modeled as *metadata about a field*, so the temptation is
to ship the field **and** its visibility flag and let the renderer decide. It is also the
path of least resistance when one message type serves both the owner-edit view and the
public view — one RPC, one message, one handler, and a `visibility` map the UI consumes.

**How to avoid:**
Make the field's absence the enforcement mechanism, not a sibling boolean. Two options,
both acceptable; pick one in the SPEC and make it an invariant:

- **Separate messages.** `OwnerCharacterProfile` (all fields) and `PublicCharacterProfile`
  (only ever-public fields plus `optional` per-field entries). The public message
  physically cannot carry a private field, so the leak is a compile error.
- **One message, `optional` scalars, server-side clearing.** Every privacy-bearing field
  is `optional` so "hidden" is `nil` on the wire, not `""`. **This is the same shape this
  repo already mandates for ABAC attributes** — `.claude/rules/abac-providers.md`: providers
  MUST omit an optional key, never emit an empty-string sentinel, because
  `"" == ""` evaluates true and creates a fail-open match. A hidden profile field emitted
  as `""` is the identical bug at the presentation layer: it is indistinguishable from
  "the owner set it to empty," and any downstream equality/search on it fail-opens.

Additionally: **redact at the innermost layer that knows the viewer**, i.e. the core
handler, not the BFF and never the SvelteKit load function. The gateway boundary rule
(`.claude/rules/gateway-boundary.md`) already forbids the gateway from computing —
"The gateway proxies; it does not compute." Redaction is computation.

**Warning signs:**
- A single proto message used by both the owner-edit RPC and the public-profile RPC.
- A `map<string, bool> field_visibility` or `repeated string hidden_fields` on a response
  message. This is nearly always the client-side-hiding smell — the server is telling the
  client what to hide instead of not sending it.
- A Svelte `{#if profile.showBio}` guard around a field.
- Any privacy string field that is non-`optional` in proto3.

**Phase to address:** Phase 1 (portal SPEC) must fix the message shape. Phase for profiles
implements it. This cannot be retrofitted cheaply — the message shape is the fix.

**Inverted test question — how could the test pass while the property is false?**
The natural test is `TestGetPublicProfileHidesPrivateBio`, asserting
`resp.Bio == ""` for a viewer who is not the owner. It passes while the property is false in
**four** distinct ways:

1. **The fixture's bio was empty to begin with.** If the test seeds a character without
   setting a bio, `resp.Bio == ""` is true regardless of any redaction code. The test never
   fails, ever, including against a handler with the redaction deleted. *Fix: seed a
   distinctive sentinel (`"SECRET-BIO-DO-NOT-LEAK"`) and assert on the whole serialized
   response, not the field.*
2. **The assertion is on the Go struct field, not the wire.** Redaction that happens in a
   `func (p *Profile) Redact()` called by the *test helper* but not by the handler passes.
   *Fix: assert against marshaled proto bytes —
   `require.NotContains(t, string(mustMarshal(resp)), "SECRET-BIO-DO-NOT-LEAK")`. This also
   catches the field leaking through a nested message, a `details` blob, or a debug field
   nobody thought about.*
3. **The test only covers the field it names.** It proves `bio` is redacted and says nothing
   about the six other fields, or about the seventh added next quarter. *Fix: drive the
   assertion from the privacy-field registry — a table test that enumerates every
   privacy-bearing field and fails when a field exists in the schema but not in the table
   (a bijection meta-test, the shape v0.12 used for the 48-row session matrix).*
4. **It tests the detail endpoint only.** See Pitfall 2.

---

### Pitfall 2: List/search/directory endpoints leak what the detail endpoint protects

**What goes wrong:**
`GetCharacterProfile` correctly redacts. `ListCharacters`, the admin character search, the
scene-participant sidebar, and the presence list each build their own summary message from
the same row and none of them call the redactor. The private field ships in the list.

**Why it happens:**
Redaction gets implemented in the handler that the feature ticket named. List endpoints are
usually older, are often not in the same phase, and construct a *different* message type
(`CharacterSummary`, not `CharacterProfile`) — so the redaction function's type signature
does not even fit, and nobody notices it wasn't called.

**This system is already primed for this specific bug.** `CharacterSummary`
(`api/proto/holomush/core/v1/core.proto:688-708`, mirrored at
`api/proto/holomush/web/v1/web.proto:496-513`) already carries `has_active_session`,
`session_status`, `last_location`, and `last_played_at`. Meanwhile
`ListAllCharacters`'s own proto doc comment (`core.proto:100-106`) states:

> *"Connection/online state is NOT included; that is a separately-permissioned attribute."*

So the codebase **already draws a privacy line between the roster view and the directory
view**, in a comment, enforced by nothing but the hand-written field list of two adjacent
message types. Adding profile fields to either message without re-deriving that line is the
default outcome.

**How to avoid:**
- One **projection function per (row → audience)** pair, and a rule that no handler may
  construct a character-shaped response message by struct literal. Every construction site
  goes through `projectPublic(row, viewer)` / `projectOwner(row)` / `projectAdmin(row)`.
  Then a lint/meta-test greps for `CharacterSummary{` / `PublicProfile{` literals outside
  the projection package — that gate is mechanically checkable, unlike "remember to redact."
- Enumerate the **full read-surface inventory** in the SPEC before writing any of it. For
  this system, at minimum: `GetCharacter` (world.proto:30), `ListCharacters`,
  `ListAllCharacters` (the directory), `ListCharactersAtLocation` (world.proto:38),
  `ListFocusPresence`, admin character list/search (new), `QueryStreamHistory`,
  `WebExportScene` (web.proto:329), `WebGetPublicSceneArchive` (web.proto:345),
  `WebDownloadPublicSceneArchive` (web.proto:351). The last three are **public, unauthenticated
  export surfaces that already exist** — see Pitfall 3.
- Promote the `ListAllCharacters` doc-comment rule into a bound invariant with a test, since
  the new profile work is exactly what would violate it.

**Warning signs:**
- Any handler that builds a character-shaped proto message with a struct literal.
- `git diff --stat` for the profile phase touches `profile_handler.go` and nothing else.
- A new field added to `CharacterSummary` "for the picker" that also happens to be
  privacy-bearing.

**Phase to address:** Phase 1 SPEC produces the read-surface inventory (it is a deliverable,
not a note). Profile phase implements projections. Admin phase adds its own audience.

**Inverted test question:**
The natural test is a per-endpoint `TestListCharactersOmitsPrivateFields`. It passes while
the property is false because **it can only cover the endpoints someone remembered to write
it for** — and the failure mode *is* forgetting an endpoint. A per-endpoint test suite is
structurally incapable of detecting a missing member of its own set.

The verification that *can* fail: a **census test** that enumerates every RPC in the
generated service descriptors whose response transitively contains a character-shaped
message, and fails when that set is not equal to a checked-in expected set. New leaky
endpoint → set inequality → RED, without anyone remembering. This is the same shape as the
`test/meta/quarantine_registry_test.go` bijection and the whole-system plugin census the
repo already runs. Set equality carries "no more" **and** "no fewer"; a `for` loop over a
hand-written list carries neither.

---

### Pitfall 3: Privacy enforced on read but not on export, history, or already-published archives

**What goes wrong:**
A player marks a profile field private, or retires a character. The field/name remains
readable through a path that never consults the privacy setting: a scene export, an audit
query, a published scene archive rendered before the privacy change, or a search index.

**Why it happens:**
Privacy is a property of the *live row*; exports and event history are **snapshots taken
earlier**, by different code, often in a different process, sometimes already public.
Nobody re-derives the snapshot when the source's privacy changes because there is no
mechanism to.

**This system has the sharpest possible version of this problem, and it is already shipped.**
Character display names are **denormalized into immutable event payloads at emit time**:
`plugins/core-scenes/commands_emit_test.go:162-174` asserts a scene pose payload
`includes actor_display_name when the dispatcher provides one` / `omits actor_display_name
when the dispatcher provides none`, and `plugins/core-scenes/poseorder.go:20` carries
`CharacterName` on the pose-order entry. Those payloads land in JetStream and are projected
into the plugin-owned `scene_log`. The event bus is append-only by design (PROJECT.md Key
Decision 3: "Actions produce immutable ordered events"). **There is no update path.** And
`WebGetPublicSceneArchive` / `WebDownloadPublicSceneArchive` (web.proto:345, 351) serve those
snapshots publicly.

**How to avoid:**
Decide the model explicitly in the SPEC and write it down as a user-facing promise, because
the honest answer here is *"history is not retroactively private"*:

- **Draw the line where the snapshot was taken.** Privacy and retirement apply to the
  **live profile surface and future events**; already-published archives are historical
  record. State this in the UI at the moment of the privacy toggle and at retirement —
  a checkbox whose tooltip says "hidden from your profile" and whose actual scope is
  "hidden from your profile but not from the 40 published scenes you're in" is a
  user-trust bug even when the code is correct.
- **Do not add new denormalized copies.** Profile fields MUST NOT be copied into event
  payloads, notification payloads, or the admin list cache. Every new denormalization
  is a new surface that privacy changes cannot reach. If a display name is needed, resolve
  it at read time through the projection function (Pitfall 2).
- **Enumerate the existing snapshot surfaces** (the export/archive RPCs above,
  `events_audit`, `scene_log`, DLQ payloads) in the SPEC with an explicit
  in-scope/out-of-scope verdict per surface. An unlisted surface is an unmade decision.

**Warning signs:**
- Any new `character_name` / `display_name` / profile field appearing in a proto **payload**
  message rather than a **response** message.
- A profile field written into `preferences` JSONB (`characters.preferences`, migration
  `000045_character_preferences.up.sql:5`) that is then copied anywhere.
- Product language like "make private" without a scope qualifier.

**Phase to address:** Phase 1 SPEC (the scope decision + surface inventory). Profile phase
enforces "no new denormalization."

**Inverted test question:**
`TestPrivateFieldNotInSceneExport` passes while the property is false whenever the export
fixture's scene predates the field's existence, or contains no poses by the character under
test. Both are the default when a test author builds a minimal fixture. *Fix: the export
test must be built from a fixture that demonstrably contains the character's content —
assert the export contains the character's **public** name first (proving the fixture is
non-degenerate), then assert it does not contain the private value. A negative assertion
without a paired positive assertion on the same fixture proves nothing about the fixture.*

This is exactly the v0.12 `test -s` failure: the check passed on a metadata-only coverage
profile because "non-empty" was reachable without "contains coverage data."

---

### Pitfall 4: Ordering, counting, and filtering side channels around private fields

**What goes wrong:**
The field is redacted but a derived signal is not. Concretely, in this domain:
- The character directory offers "sort by last played" while `last_played_at` is private.
  The order of the public list reveals the hidden values.
- A filtered search (`?has_bio=true`, `?location=Tavern`) returns a *subset*; membership in
  the subset discloses the predicate's value for every returned row, and — via absence —
  for every row not returned.
- A facet count ("12 characters in the Tavern") over a set where location visibility is
  per-character lets an attacker binary-search a single character's hidden location by
  differencing counts across queries.
- Presence: a private character shows as absent from `ListFocusPresence` but its scene
  participation count still increments.

**Why it happens:**
Redaction is applied to the *response payload* as the last step. Sorting, filtering, and
counting happen in **SQL**, before the redactor ever runs, on the unredacted column. The
redactor cannot see that it has already leaked.

**How to avoid:**
- **Make the query planner privacy-aware, not just the serializer.** Any `ORDER BY`,
  `WHERE`, or `COUNT` over a privacy-bearing column MUST carry the viewer's predicate in the
  same statement. In practice: no privacy-bearing column may appear in an `ORDER BY` or
  `WHERE` of a public-audience query at all. Sorting the public directory is restricted to
  ever-public columns (name, created_at).
- **No aggregate counts over privacy-partitioned sets** on public surfaces. Show a list, not
  a count, or show a count computed only over the rows the viewer can already see.
- Treat this as a design constraint on the SPEC's search/sort feature list, not an
  implementation detail. It is much cheaper to not promise "sort by last active" than to
  build it safely.

**Warning signs:**
- A sort dropdown whose options are drawn from the same field list as the privacy toggles.
- Any `COUNT(*)` in a public-audience query.
- A search endpoint whose `WHERE` clause is built from user-supplied field names.
- Pagination totals (`total_count`) on a filtered privacy-partitioned list — the total is
  itself the leak, even when the page contents are correctly filtered.

**Phase to address:** Phase 1 SPEC (constrain the sort/search feature set). Profile and admin
phases implement.

**Inverted test question:**
`TestSearchDoesNotReturnPrivateCharacters` asserts the *rows* are absent — and passes while
the *order*, the *total_count*, and the *absence pattern* all still leak. The row-level
assertion is orthogonal to the channel that leaks.

The verification that can fail: a **differential test** — two viewers, identical query,
assert the responses are byte-identical except for fields the model says may differ. Any
side channel (order, count, pagination total) shows up as a diff. This is much stronger than
"the private row is not in the list," and unlike the per-field test it has no way to be
vacuously true.

---

### Pitfall 5: Privacy checked on the owner path but not the admin path, or vice versa

**What goes wrong:**
Two symmetric failures:
- **Admin bypass leaks to non-admins.** The admin character-detail RPC deliberately returns
  everything (legitimate). A non-admin reaches it because the check is `if req.IsAdminView`
  or because the web BFF has an `admin=true` query param it forwards.
- **Owner path over-redacts, or under-redacts.** The owner's own edit screen is fed by the
  *public* projection, so the owner cannot see their own hidden values; the "fix" is to add
  an `include_private` flag, which the next caller sets unconditionally.

**Why it happens:**
Three audiences (public / owner / admin) crossed with N fields is a matrix, and it gets
implemented as a chain of `if` statements in one handler. Chains of `if` in an authorization
path are where a `||` gets typed instead of a `&&`.

**How to avoid:**
- **The audience is derived server-side from the authenticated subject, never from a request
  field.** No `include_private`, no `as_admin`, no `view=admin`. If the request can express
  the audience, the request can lie.
- **Separate RPCs per audience** where the field sets differ materially. `GetMyCharacter`
  (owner, from the session's player), `GetPublicProfile` (any viewer), `AdminGetCharacter`
  (RoleAdmin). Three RPCs, three ABAC actions, three tests. This is the same shape as this
  repo's existing `ListCharacters` (own) vs `ListAllCharacters` (directory) split.
- Run each audience through the **default-deny ABAC engine** with a distinct
  action/resource, per PROJECT.md Key Decision 2. Do not implement a fourth, parallel,
  hand-rolled authorization mechanism inside the profile handler.

**Warning signs:**
- Any boolean on a request message that widens the response.
- A single handler with three response-shaping branches.
- The admin RPC and the public RPC returning the same message type.

**Phase to address:** Phase 1 SPEC (the audience matrix is a SPEC artifact). Profile + admin
phases implement.

**Inverted test question:**
`TestAdminCanSeePrivateFields` and `TestNonAdminCannotSeePrivateFields` both pass while the
property is false when the non-admin test calls the **public** RPC (which never returned the
field anyway) rather than the **admin** RPC. The negative test must target the *privileged*
endpoint with an *unprivileged* subject. Ask of every negative authz test: *which endpoint
is it calling?* — a denial test aimed at an endpoint that has nothing to deny is the purest
form of a verification that cannot fail.

---

### Pitfall 6: Authorization enforced in the nav/route and not at every RPC

**What goes wrong:**
`/admin/+layout.ts` checks the role and redirects. The nav renders conditionally. The
underlying `AdminListCharacters` / `AdminUpdateCharacter` RPCs check nothing, because "you
can only get here from the admin page." Anyone with a session token can `curl` the endpoint.

**Why it happens:**
The route guard is the visible, demoable artifact — it is what a stakeholder sees. It is
written first, it works, and the RPC-level check feels redundant. In a BFF architecture the
feeling is amplified: the BFF *is* the only caller, so "the BFF checks it" reads as
sufficient.

**In this repo it would also be a boundary violation.** `.claude/rules/gateway-boundary.md`:
the gateway does "Protocol translation" and "MUST NOT" do "Business logic or data
aggregation." An authorization decision made in `internal/web/` or in a SvelteKit
`+layout.ts` is business logic living in the wrong process — and the gateway is explicitly
designed to be horizontally scaled and replaceable, so it is the *least* trustworthy place
to put a decision.

**How to avoid:**
- **Every admin RPC re-asserts the gate at its own entry point.** This repo already has the
  canonical pattern, with a written rationale: `internal/admin/auth/operator_admin.go`'s
  `AssertOperatorAdmin` — *"Three sites use this exact pair: Authenticate, Approve, and
  ResetTOTP. Sharing the helper here prevents the three sites from drifting (e.g., one of
  them silently removing a check)."* It re-asserts both gates at every entry point per
  INV-CRYPTO-83. Copy this shape exactly for the web admin surface: one shared
  `AssertWebAdmin(ctx, ...)` helper, called first in every admin handler, returning typed
  `DENY_*` oops codes.
- **Route guard is UX, not security.** Say so in a comment at the route guard, so the next
  reader does not treat it as the control.
- Register admin actions with the ABAC engine (`engine.Evaluate(subject, action, resource)`)
  rather than a bare `if hasRole` — default-deny is the project's locked decision, and it
  means a *missing* policy denies rather than allows.

**Warning signs:**
- An admin handler whose first statement is not an authorization assertion.
- The string `RoleAdmin` appearing in `web/src/` or `internal/web/` (today: **zero
  occurrences under `internal/web/`** — that number should stay meaningful).
- A PR that adds an admin RPC and touches no test file named for denial.

**Phase to address:** Admin shell phase — and it must be the *first* thing built in that
phase, before any section, so every subsequent section inherits it.

**Inverted test question:**
`TestAdminPageRedirectsNonAdmin` (a Playwright test) passes while every admin RPC is wide
open — it tests the redirect, which is not the property. Even at the RPC level,
`TestAdminListCharactersDeniedForNonAdmin` passes while the property is false if the test's
non-admin subject would have been denied *anyway* for an unrelated reason (no session, no
character selected, missing token). The assertion `err != nil` cannot distinguish
"denied for lack of admin role" from "denied for lack of a token."

*Fix, and this repo already does it:* assert the **specific oops code**
(`errutil.AssertErrorCode(t, err, "DENY_NOT_ADMIN_ROLE")`), with a paired positive test
proving the same subject **succeeds** once granted the role. The positive control is what
proves the negative test's subject was otherwise valid. A denial test without a paired
grant test on the same fixture is unfalsifiable.

⚠️ Note the repo's own recorded caveat: `errutil.AssertErrorCode` chain-walks via
`errors.Is` and will pass on a double-wrapped error. For an opacity/authorization contract,
assert the **top-level** code via `oops.AsOops(err).Code()` per `.claude/rules/grpc-errors.md`.

> **SUPERSEDED — see `.planning/phases/01-portal-spec/01-SPEC.md` §9.6.1, §12.1 rule 5, and §14
> row 8.** The recorded caveat this repeats is itself wrong. Under the pinned
> `github.com/samber/oops v1.22.0`, `OopsError.Code()` returns the code from the **deepest** error
> in the chain (a recursive `getDeepestErrorCode` walk), and `errutil.AssertErrorCode`
> (`pkg/errutil/testing.go:15-20`) is a thin wrapper over `oops.AsOops` plus that same `.Code()` —
> the two spellings are behaviorally identical and **both** pass on a double-wrap. Assert **over
> the wire** instead: `status.Code(err)` plus a generic `status.Convert(err).Message()` carrying no
> internal code string. The paired-positive-control point above (which is this pitfall's actual
> subject) stands unchanged. Tracked as issue **#4902**. Annotated 2026-08-01 by plan 01-05.

---

### Pitfall 7: The reserved nav section that gets wired later without its authorization

**What goes wrong:**
This milestone ships `/admin` with a nav containing declared, empty room for stats, player
management, moderation, audit viewer, config, and plugin management (PROJECT.md's Current
Milestone). Six months later someone implements the audit viewer. The nav entry already
exists, so the PR is "just wire the section" — the route is added, the RPC is added, and
because the *nav* was already gated by the shell's `RoleAdmin` check, nobody adds a check to
the new RPC. The section is reachable by URL and by `curl`.

**Why it happens:**
This is the specific hazard created by this milestone's defining constraint. **Reserved room
carries an implicit and false promise: "the hard part is done."** A future implementer sees
an existing gated shell and reasonably infers that the gating is structural. It isn't — it
gated a nav item that pointed at nothing.

**How to avoid — this is the highest-leverage single decision in the milestone:**
Make the reserved slots **structurally incapable of being wired without authorization**:

- The section registry entry is not a label + href. It is a record that **requires** an
  authorization descriptor: `{id, label, href, requiredAction, requiredResource}` with no
  default and no zero value that means "allow." A section registered without an
  `requiredAction` fails to compile or fails at boot (fail-closed at load — the pattern the
  plugin manifest system already uses).
- The reserved sections ship **registered and denied**, not absent. Each of the six ships
  with its authorization descriptor populated and a handler that returns
  `NOT_IMPLEMENTED` *after* passing the gate. Then wiring a section means replacing a
  handler body — the gate is already there and already tested.
- A meta-test asserts **set equality** between the section registry and the set of
  authorization descriptors. Adding a section without a descriptor is RED.

**Warning signs:**
- A nav section defined as a plain string or a two-field struct.
- An admin route file with a TODO and no authorization call.
- The section registry living in `web/src/` (client-side) rather than being served from core.

**Phase to address:** Admin shell phase. This is the requirement that makes the "declared
empty room" REQ-ID meaningful rather than decorative.

**Inverted test question:**
`TestAdminNavOnlyShowsSectionsUserCanAccess` passes trivially today because **there are no
implemented sections** — the assertion is over an empty or one-element set. It will continue
to pass as sections are added, because it tests the *nav rendering*, not the endpoints.

The verification that can fail: enumerate the registry, and **for every registered section
assert an unprivileged caller receives a denial from its endpoint**. Today that is six
`NOT_IMPLEMENTED`-after-gate assertions — which is exactly the point: the test is
non-vacuous from day one, and a new section that skips the gate turns it RED before the
section has any content to review.

---

### Pitfall 8: Privilege escalation through the character-admin edit surface

**What goes wrong:**
The admin character-edit RPC accepts a `Character` message and writes the fields it
contains. Roles live in `character_roles` (`internal/store/migrations/000001_baseline.up.sql:83-88`),
keyed by `character_id`. If the edit surface ever accepts a role list — or if a future
"edit character" reuses a generic update path — an admin can grant `admin` to any character,
and more subtly:

**The escalation is player-wide and one-way.** `PostgresRoleStore.PlayerHasRole`
(`internal/store/role_store.go:83-103`) is documented *"returns true iff **any** character of
playerID has role"* and joins `character_roles` to `characters` on `player_id`. So granting
`admin` to a player's *throwaway alt* grants that **player** admin everywhere the check is
player-scoped — including the operator path in `internal/admin/auth/ingame.go:116`
(*"capability allow-list + RoleAdmin (any character)"*). A character-scoped write has
player-scoped blast radius, and the character-admin UI is precisely where a role field would
naturally be edited.

Related: an admin editing their **own** roles, or their own character, is unrestricted by
default.

**How to avoid:**
- **Role mutation is not part of character administration in this milestone.** Make that an
  explicit SPEC exclusion, not an omission. The character-admin edit RPC's field mask MUST
  NOT include roles. If role management is wanted, it belongs in the (deferred) player
  management section with its own design.
- **Field-mask allowlist, never a whole-message write.** `AdminUpdateCharacter` accepts an
  explicit `update_mask` and rejects unknown paths. A handler that does
  `repo.Update(ctx, req.Character)` writes whatever the client sends, including fields the
  UI never exposes.
- **Self-edit is a distinct, more-restricted action.** At minimum, an admin acting on their
  own player's characters produces a distinguishable audit entry; ideally role-affecting
  self-action is denied outright (the standard four-eyes shape — and note this repo already
  has an `Approve` step in the operator admin path, so the precedent exists).
- Reuse `AssertOperatorAdmin`'s two-gate composition idea: for genuinely dangerous
  operations, `RoleAdmin` alone should not suffice.

**Warning signs:**
- `role`, `roles`, or `character_roles` appearing anywhere in the admin character proto.
- An update RPC with no `update_mask`.
- Any admin write path where the acting subject is not compared to the target's owner.

**Phase to address:** Admin character-administration phase. The SPEC must state the field
mask explicitly.

**Inverted test question:**
`TestAdminCannotEscalateOwnRole` passes while the property is false if the test attempts
escalation through a path the implementation doesn't have (e.g. calls `AdminUpdateCharacter`
with a `roles` field that proto-drops silently because it isn't in the message). The test
asserts "role unchanged," gets "role unchanged," and proves nothing — the request never
carried the payload.

*Fix:* the test must first demonstrate the **write path works at all** on a field it is
allowed to change (positive control), then attempt the escalation on the same call. And
because the real risk is a *future* field, the durable verification is a schema-level
assertion: a meta-test that fails if the admin character message ever gains a field whose
name matches `role|grant|permission|capability`, and a field-mask allowlist test asserting
**set equality** with a checked-in list.

---

### Pitfall 9: No audit trail on admin mutations — or one that cannot be trusted

**What goes wrong:**
Admin edits a character's name/description, disables an account. Nothing durable records
who, what, before-value, when. Three months later a dispute has no answer. Or worse: an
audit record exists but is written **after** the mutation commits, on a best-effort path, so
a failure silently loses the record while the mutation stands.

**Why it happens:**
Audit is a cross-cutting concern with no visible user story, so it is scheduled last and
implemented as a `slog.Info` line. And the "emit an event after commit" shape is the natural
one — which is precisely the shape v0.12 spent a phase deleting.

**How to avoid — and this system makes the right answer cheap:**
v0.12's Phase 5 built a **transactional outbox**: intent written in the same transaction as
the state change, drained by a leased relay publishing in `feed_position` order with
`Nats-Msg-Id` dedup, and *"the post-commit emit path was deleted, not deprecated"*
(MILESTONES.md:15). Admin mutations MUST ride that outbox. An admin mutation whose audit
record is emitted post-commit reintroduces the exact dual-write window the previous
milestone closed — and it would be reintroduced in the one place where losing the record is
worst.

- Audit record content: acting **player** ID (not just character — see Pitfall 8's
  player-wide role semantics), target character ID, action, **before and after values** of
  every changed field, timestamp, and the request's correlation/trace ID.
- Use the existing `events_audit` host-owned audit projection rather than a bespoke table,
  so retention, DLQ (`EVENTS_AUDIT_DLQ`, never-drop per INV-EVENTBUS-29/30), and the replay
  CLI all apply for free.
- **Before-values are the whole point.** An audit row saying "admin X updated character Y"
  answers nothing.

**Warning signs:**
- An admin mutation handler containing `slog.InfoContext(...)` and no event emit.
- An emit that happens after the repo call returns rather than inside its transaction.
- An audit payload with an `after` but no `before`.

**Phase to address:** Admin character-administration phase, as a gate on the *first* admin
mutation — not as a follow-up.

**Inverted test question:**
`TestAdminEditEmitsAuditEvent` passes while the property is false when it asserts only that
*an* event was emitted. It does not assert the event survived, that it is durable, that it
contains before-values, or that it is written in the same transaction. The dual-write bug is
invisible to it by construction: the emit *does* happen on the happy path.

The verification that can fail: **inject a failure between commit and emit** and assert the
audit record is still recoverable (which the outbox guarantees and a post-commit emit does
not). If the test cannot be written because there is no injection seam, that is itself the
finding. v0.12 built a two-replica resilience harness for exactly this class of question
(`test/integration/resilience/`) — reuse it rather than asserting the happy path.

---

### Pitfall 10: Name uniqueness is application-level, unindexed, and normalization-naive

**What goes wrong (three distinct bugs sharing one root):**

Grounded in the current code:

```
// internal/bootstrap/setup/adapters.go:38-50
"SELECT EXISTS(SELECT 1 FROM characters WHERE LOWER(name) = LOWER($1))"
```

and in `internal/auth/character_service.go:112-121`, a **check-then-insert**:
`ExistsByName` → `if exists { return CHARACTER_NAME_TAKEN }` → create. Meanwhile
`internal/store/migrations/000001_baseline.up.sql:68-76` defines
`name TEXT NOT NULL` with **no unique constraint** and only `idx_characters_location` —
there is no unique index and no index on `LOWER(name)` at all.

1. **TOCTOU race.** Two concurrent creations of the same name both pass `ExistsByName` and
   both insert. Nothing in the database stops it. Today this is reachable via
   `CreateCharacter`; **v0.13 makes it far more reachable** by adding `Rename` (a second
   writer to the same uniqueness domain) and a web form that users will double-submit.
2. **Sequential scan.** `WHERE LOWER(name) = LOWER($1)` cannot use any existing index. It is
   an unauthenticated-adjacent, user-triggerable full scan of `characters` on every creation
   and every rename keystroke if the UI adds live availability checking.
3. **Normalization is not confusable-safe.** `world.NormalizeCharacterName`
   (`internal/world/validation.go:114-126`) does `strings.Fields`, `strings.ToLower`, then
   uppercases the first rune of each word. That is it. There is **no Unicode NFKC
   normalization, no zero-width/format-character stripping, and no confusable folding.**
   So `Аdmin` (Cyrillic А, U+0410) and `Admin` are different names; `Ad` + `U+200B`
   (zero-width space) + `min` is a third. (Written escaped on purpose — a literal
   zero-width character in a planning doc trips injection scanners and is invisible
   to reviewers, which is itself the point.)
   Under a public-profile feature, that is a ready-made impersonation vector — and it is far
   more damaging with public profiles than it was with a telnet-only roster.

**How to avoid:**
- **A partial unique index on the normalized form is the fix, and it is cheap.**
  `CREATE UNIQUE INDEX CONCURRENTLY ... ON characters (normalized_name)` — a stored,
  generated, or trigger-free application-written column (the repo forbids DB triggers and
  functions per `.claude/rules/database-migrations.md`, so compute it in Go and store it).
  Then the uniqueness check becomes "attempt the insert, handle `23505`," which has no race.
  Keep `ExistsByName` for the friendly pre-check UX, but **the constraint is the control.**
- Extend normalization: NFKC, strip `Cf` (format) characters, reject mixed-script names or
  fold confusables. Decide the policy in the SPEC — this is a product decision (does the game
  permit non-Latin names?) with a security consequence.
- **Migration must handle existing duplicates.** If any already exist (the race has been
  live), `CREATE UNIQUE INDEX` fails. The migration needs a detection query first, and the
  repo's migration rules forbid long-running backfills inside migrations — so a one-shot job
  plus a migration is the shape.

**Warning signs:**
- A live "is this name available?" endpoint (guaranteed to make the seq scan hot).
- `Rename` implemented by copying `createWithMaxAndBind`'s check-then-write shape.
- Any test that asserts uniqueness by calling create twice **sequentially**.

**Phase to address:** Phase 1 SPEC decides the normalization policy; the character-management
phase ships the index + rename. The index MUST land before or with `Rename`, not after.

**Inverted test question:**
`TestCannotCreateDuplicateName` passes today, and will pass after `Rename` ships, while the
property is false — because it creates two characters **sequentially**, which the
application-level check does catch. The property that is false is *concurrent* uniqueness,
and a sequential test is structurally blind to it.

The verification that can fail: two goroutines racing the same name against a real Postgres,
asserting exactly one succeeds and the other gets `CHARACTER_NAME_TAKEN` (or `23505`), run
with `-race` and repeated. Demonstrate it RED against the current unindexed schema **before**
adding the index — per the repo's own rule, a gate never observed matching is
indistinguishable from one that cannot match.

---

### Pitfall 11: Rename breaks references the system holds by name, not by ID

**What goes wrong:**
`Rename` updates `characters.name`. Everything that captured the *name* keeps the old one;
everything that captured the *ID* is fine. The system currently does both, and they are not
obviously distinguishable in the code.

Grounded instances of name-capture found in this repo:
- **Event payloads.** `actor_display_name` is written into scene pose payloads at emit time
  (`plugins/core-scenes/commands_emit_test.go:162-174`). Events are immutable
  (PROJECT.md Key Decision 3). These never update.
- **Plugin-owned audit rows.** `scene_log` entries carry `CharacterName`
  (`plugins/core-scenes/poseorder.go:20`, `service.go:1509`, `2022`). Append-only.
- **Published scene archives**, served publicly (`web.proto:345`, `351`), rendered from the
  above.
- Anything the telnet client has already printed to a scrollback.

Meanwhile `character_roles`, `player_character_bindings`, `sessions`, `locations.owner_id`,
and `objects.*` all reference **`characters(id)`** and are unaffected — the FK list is
`internal/store/migrations/000001_baseline.up.sql:80, 84, 99, 140, 143, 160` and
`000015_create_player_character_bindings.up.sql:14`.

**Why it happens:**
Denormalizing a display name into an event is the *right* call for an append-only log (the
log must be readable without a join into mutable state). It only becomes a bug when a rename
feature is added later and nobody re-derives which copies exist.

**How to avoid:**
- **Enumerate the name-capture surfaces in the SPEC** and give each an explicit verdict:
  historical (stays stale, by design) vs. live (must re-resolve). Then write it into the UI:
  renaming warns "past scenes and published logs keep your previous name."
- **A rename produces an event.** A `character.renamed` event with `{id, old_name, new_name}`
  on the outbox gives every consumer — present and future — a hook, and gives the audit log
  the before-value.
- **Consider a name-history table** (`character_name_history`) so old names resolve to the
  current character. This is what makes archives navigable and is the substrate for the
  impersonation defense in Pitfall 12.
- **Do not "fix" this by mass-updating event payloads.** Rewriting an append-only audit log
  is a far worse bug than a stale name, and would break the JetStream/`events_audit` model
  the platform is built on.

**Warning signs:**
- A rename implementation that greps for other tables to update.
- A test asserting that a scene log shows the *new* name for an *old* pose (this asserts the
  wrong behavior — it would require mutating the audit log).
- Product copy promising "your name updates everywhere."

**Phase to address:** Character-management phase (rename). The surface enumeration is a
Phase 1 SPEC deliverable.

**Inverted test question:**
`TestRenameUpdatesCharacterName` passes while every reference is stale — it asserts the
column changed, which is the easy half. The property under test is really *"the set of
surfaces showing a stale name equals the set the SPEC declared historical."* That is testable
as a table: for each declared-historical surface, assert the **old** name still appears
(pinning the decision); for each declared-live surface, assert the **new** name appears. A
test that only checks the live surfaces cannot detect a surface that was never classified.

⚠️ Beware the inverse of this — v0.12's catalogue includes *"a bats case that pinned a
security hole as intended behavior."* Pinning "old name still appears in archive" is correct
**only if the SPEC decided that**. Pin the decision, and make the test cite the SPEC section,
so a future reader can tell a deliberate pin from a bug frozen into a test.

---

### Pitfall 12: Retire/disable semantics leak, and name release enables impersonation

**What goes wrong:**
"Retire" is added with no schema support (there is **no status, `retired_at`, or `deleted_at`
column on `characters` today** — verified across all `ALTER TABLE characters` migrations:
`000045` added `preferences`, `000049` added `version`, and that is the extent of it). So it
gets implemented as one of:

- **A hard `DELETE`.** This **fails at runtime** for any character that owns a location or an
  object: `locations.owner_id TEXT REFERENCES characters(id)` (baseline:99) and
  `objects.owner_id` (baseline:143) carry **no `ON DELETE` clause**, so Postgres defaults to
  `NO ACTION` and the delete errors. Worse, for characters that *can* be deleted, the
  cascade at `character_roles.character_id ... ON DELETE CASCADE` (baseline:84) **silently
  drops their roles**, and `players.default_character_id ... ON DELETE SET NULL`
  (baseline:80) silently nulls the player's default — with no audit record of either.
- **A boolean `is_retired`** that exactly one read path consults. The retired character then
  appears in `ListAllCharacters` (the directory), in `ListCharactersAtLocation`, in
  presence, in search, and in scene-participant lists — every one of which is a separate
  query with its own `WHERE`.
- **Name release.** If retirement frees the name, a new character can claim it and inherit
  the identity of every historical pose, archive, and log entry that captured the name
  (Pitfall 11). This is the impersonation vector, and it is created *by* the combination of
  retirement + denormalized historical names, neither of which is a bug alone.

**How to avoid:**
- **Retire is a state, not a delete.** Add an explicit lifecycle column and treat "hard
  delete" as an operator-only, separately-designed operation (probably out of scope for
  v0.13). Do this even if the admin UI's button says "delete" — GDPR-style erasure is a
  different feature with different requirements.
- **Names are never released** by retirement — or if they are, only after a cooling-off
  period **and** only with a name-history table (Pitfall 11) so the old binding remains
  resolvable and the UI can disambiguate. Default to "never released"; it is the safe
  default and is trivially relaxable later.
- **The retired predicate goes into the projection/query layer** (Pitfall 2), so it is
  applied by construction at every read surface rather than remembered per-query.
- **Audit the cascade before shipping any delete.** The FK inventory above is six rows; read
  them and decide each one, rather than discovering `NO ACTION` in production.

**Warning signs:**
- An admin "Delete" button with no corresponding `deleted_at`/`status` column in the diff.
- A `WHERE ... AND NOT retired` appearing in some queries and not others.
- Retirement implemented without touching the name-uniqueness path (Pitfall 10) at all —
  that means the decision about name release was never made.

**Phase to address:** Character-management phase (player-initiated retire) and admin phase
(admin disable). Both need the same lifecycle column, so the SPEC must define it once.

**Inverted test question:**
`TestRetiredCharacterNotInDirectory` passes while the character is visible in five other
places, for the same structural reason as Pitfall 2 — a per-endpoint test cannot detect a
missed endpoint.

Two verifications that *can* fail:
1. **A census** over every character-returning RPC (reuse the Pitfall 2 census), asserting a
   retired fixture appears in **none** of them except those explicitly allowlisted.
2. **A DB-level assertion** that every production query touching `characters` either carries
   the lifecycle predicate or is on an allowlist — checkable with `ast-grep`/lint over the
   repo store layer, which unlike a runtime test cannot be vacuously satisfied by a fixture.

---

### Pitfall 13: The new mutation RPCs bypass the optimistic-concurrency guard v0.12 just built

**What goes wrong:**
`Rename`, `SetDescription`, `Retire`, and the admin edit all write `characters` with a plain
`UPDATE ... WHERE id = $1`. v0.12's MODEL-03 added
`characters.version INTEGER NOT NULL DEFAULT 1`
(`internal/store/migrations/000049_world_version_guard.up.sql:20`) precisely so world writes
use version-predicated CAS (`... WHERE id = $1 AND version = $2`) with a typed
`WORLD_CONCURRENT_EDIT` signal, closing last-write-wins (M12, #4798). New write paths that
skip the predicate **silently reintroduce the bug the previous milestone spent a phase
empirically reproducing and closing** — and they reintroduce it on the surface where
concurrent edits are most likely (a web form and an admin panel editing the same row).

Same for the outbox: MODEL-04 requires the emit intent be written **in the same transaction**
as the state change; the post-commit emit path was deleted, not deprecated (MILESTONES.md:15).

**Why it happens:**
The new RPCs are written by someone reading `CreateCharacter` as the template — and creation
has no version to check. The CAS pattern lives in the *update* paths of the four world repos,
which a character-identity feature has no reason to read.

**How to avoid:**
- Every new `characters` write goes through the existing world repo update path, carrying
  `expected_version` on the request message and surfacing `WORLD_CONCURRENT_EDIT` to the UI
  as a "someone else changed this" retry.
- Every new mutation emits through the outbox in the same transaction.
- Add the new RPCs to whatever test binds INV-WORLD-1..4 rather than writing fresh
  concurrency tests — the invariants already exist and are bound.

**Warning signs:**
- A request message for a character mutation with no `expected_version` / `version` field.
- A store method on `characters` whose `UPDATE` has a one-clause `WHERE`.
- An emit call placed after `tx.Commit()`.

**Phase to address:** Character-management phase (the first new mutation RPC sets the
pattern for all of them) — and Phase 1 SPEC must put `expected_version` on the request
messages, because adding it later is a wire-compat change to every caller.

**Inverted test question:**
`TestRenameCharacter` passes with a plain `UPDATE`. Concurrency bugs are invisible to
single-threaded tests by definition. The verification that can fail is the one v0.12 already
built: the two-replica resilience harness that **deterministically reproduced** M12
last-write-wins. Point it at the new mutation RPCs. If the new RPCs are correct it stays
green; if they use a one-clause `WHERE` it goes red — which is the definition of a test worth
having.

---

### Pitfall 14: Reserved capacity rots (the extensibility question, answered concretely)

The milestone's defining constraint is "leave room for the deferred pieces." Reserved
capacity does not rot randomly — it rots in four specific, nameable ways.

#### 14a. Reserved-but-unused proto fields

**How it rots:** A reserved field number with a placeholder name (`reserved 7;` or
`string future_avatar_url = 7;` unused) carries **no semantics**. When the deferred feature
arrives, its actual requirements do not match the guess — the avatar needs an ID + a
content hash + dimensions, not a URL string. The reserved field is then either (a) repurposed
with different semantics, silently breaking any consumer that read the old meaning, or
(b) abandoned, leaving a permanently-dead field number and the *new* fields appended at 12,
so the schema now has a lie in it.

**Sharper failure specific to privacy work:** a reserved field that is **not** `optional`
serializes as its zero value. A reserved `string bio_extended = 7;` ships `""` to every
client from day one. Under a privacy model where "absent" means "hidden" (Pitfall 1, and the
repo's own omit-don't-sentinel rule), a reserved non-optional field is a **fail-open
placeholder** — it is present-and-empty, which is exactly the shape
`.claude/rules/abac-providers.md` bans.

**What makes it survive instead:** Do not reserve *fields*. Reserve **field numbers only**
(`reserved 7 to 12;` with a comment naming the intended feature and linking the deferral
issue), which costs nothing, carries no semantics, and cannot serialize. Reserve a *shape*
only when the shape is already validated by a real consumer — see 14c.

#### 14b. The empty nav section registry

**How it rots:** Three ways, in increasing severity.
1. **The registry has no consumer, so its contract is untested.** Whatever fields it has are
   whatever the first (only) section needed. The second section needs something different
   and the registry is rewritten — the reservation bought nothing.
2. **The registry lives client-side.** A nav registry in `web/src/lib/adminNav.ts` is a
   list of labels. It cannot carry an authorization descriptor that the server enforces, so
   the reserved slots are decorative. This is the default outcome and it directly enables
   Pitfall 7.
3. **The empty slots are exempt from every gate.** Meta-tests, census tests, and lint rules
   are written against "sections that exist," and an empty slot isn't one — so the slot
   accretes no protection and the future implementer inherits none.

**What makes it survive instead:** Ship the reserved sections **registered, gated, and
returning `NOT_IMPLEMENTED` after the gate** (Pitfall 7). Then: the registry has six
consumers on day one so its contract is exercised; the authorization descriptor is
mandatory and server-side; and every gate written for the character section automatically
covers the other six because they are members of the same set. **Reserved capacity survives
in proportion to how much of the real machinery it is forced to run through on day one.**
An empty slot that runs through nothing is a comment.

#### 14c. A media schema with no consumer

**How it rots:** The 1-primary + 10-gallery schema ships with no upload path
(explicitly deferred to 999.16). With no writer and no reader, nothing constrains it, so:
- Nobody discovers that the ordering semantics are undefined until someone builds reordering.
- Nobody discovers there is no place for alt-text / content-warning / moderation-state until
  moderation arrives (and moderation is *also* a deferred admin section — two deferrals that
  will collide).
- The "no later migration" promise is tested by nothing, so it is a hope. A migration will be
  needed and the REQ-ID will have been satisfied on paper.

**What makes it survive instead:** Give it **one real consumer now.** The cheapest honest
option: make the *existing* character description-or-name rendering path read through the
media schema's accessor, even if it always returns zero images. That single call site turns
"does the schema work" from a guess into a compiled, tested fact, and it means the deferred
upload path plugs into a socket that is already load-bearing.

And **write the constraint down as a testable claim**: the REQ-ID says "accommodates 1 primary
+ 10 gallery without a later migration." Turn that into a migration-shaped test — a fixture
that inserts 1 primary + 10 gallery rows through the schema as designed, today, with a stub
storage reference. If that insert works, the claim is verified. If it can't even be written
because the schema has no insert path, the claim is unverifiable and the REQ-ID is decorative.

⚠️ **This is the "correct implementation of a wrong spec" hazard in its purest form.** Every
test derived from the media SPEC will pass, because the SPEC is the only thing describing a
consumer that does not exist. The only defense is an external reality check: does a real
insert of the full 1+10 shape succeed?

#### 14d. A privacy tier that only one field uses

**How it rots:** A three-tier model (`public` / `friends` / `private`) shipped when only
`bio` is privacy-bearing means the `friends` tier has no membership source (there is no
friends/relationship model in this codebase). It is a value in an enum with no evaluator.
When the second field arrives, the tier is either (a) still unimplemented and quietly treated
as `private` — a silent semantic change for anyone who selected it, or (b) implemented then,
against a membership model designed for a different purpose.

**Sharpest version:** an unimplemented tier that **fails open**. If the evaluator is
`switch tier { case Private: return false; default: return true }`, then `friends` — and any
future tier value, and any corrupted value — returns *visible*. This is structurally the
same bug as the empty-string sentinel: an unhandled value that type-checks and evaluates
permissively. The repo's `HasPlayerGrant` (`internal/access/grants.go:75-83`) shows the
correct instinct — an absent bag key and a failed type assertion both return `(false, nil)`,
i.e. deny — but note it also shows the hazard: **a type mismatch is indistinguishable from
"no grants."** A privacy evaluator with the same shape would deny silently on a schema bug,
which is safe; one written with the polarity inverted (`isPrivate`) would **allow** silently,
which is not.

**What makes it survive instead:**
- **Only ship tiers that have a working evaluator today.** Two tiers (`public` / `private`)
  with a real evaluator beats three with two real ones. Adding a tier later is an enum
  append — cheap. Removing a tier users have selected is a data migration and a broken
  promise — expensive. **Asymmetric cost: under-reserve tiers, over-reserve field numbers.**
- **Default-deny on unknown tier values, exhaustively.** `switch` with an explicit
  `default: return notVisible` and a linter/`exhaustive` check so a new enum value fails the
  build rather than falling through.
- **Apply the tier to ≥2 fields at ship time.** One field cannot distinguish "the tier model
  works" from "this field's `if` works."

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Client-side field hiding (return all, render some) | Ships the profile UI in a day; one RPC | Silent data leak to every non-browser client; unfixable without a wire-breaking change | **Never** |
| One `CharacterProfile` message for owner + public + admin | No message proliferation; simpler client types | Every future field must be classified in a handler `if`, and the classification is invisible in the schema | Only if `optional` + server-side clearing + a per-field census test all ship together |
| App-level name uniqueness (status quo) with no DB index | No migration; already works | TOCTOU duplicates, seq scan on every create/rename; impersonation surface under public profiles | Acceptable **only** until `Rename` ships. Rename is the trigger that makes it untenable |
| Nav sections as `{label, href}` strings | Nav ships in an hour | Reserved slots carry no authorization; future sections wire up unguarded (Pitfall 7) | Never for `/admin`. Fine for public nav |
| Route-guard-only admin authorization | Demoable immediately; feels complete | Every admin RPC is open to any session; gateway-boundary violation | Never — but shipping the route guard *first* is fine if the RPC gate lands in the same phase |
| Reserved proto fields with placeholder names | "Room is reserved" checks the REQ-ID box | Non-`optional` reserved fields serialize zero values (fail-open under absence-means-hidden); semantics guessed wrong get repurposed or abandoned | Reserve **numbers** (`reserved 7 to 12;`), never named fields |
| Hard `DELETE` for admin "delete character" | One line | FK `NO ACTION` errors on location/object owners; silent `CASCADE` role loss; unrecoverable | Never in v0.13. Retire = state transition |
| Audit as a `slog` line | Zero design work | No durable record, not queryable, lost on log rotation, no before-values | Never for admin mutations |
| Post-commit audit emit | Simpler than threading a tx | Reintroduces the exact dual-write window v0.12 deleted | Never — the outbox already exists |
| Three privacy tiers, one implemented | "Extensible" | Unimplemented tier silently degrades or fails open; users select a tier that does nothing | Never. Ship 2 real tiers |

---

## Integration Gotchas

Mistakes specific to connecting these new features to **this** existing system.

| Existing subsystem | Common mistake | Correct approach |
|---|---|---|
| **ABAC engine** (`internal/access`, default-deny) | Hand-rolling a `if hasRole(admin)` check inside the profile/admin handler, parallel to the engine | `engine.Evaluate(subject, action, resource)` with a new action per audience. Default-deny means a *missing* policy denies — a parallel check has no such safety |
| **ABAC attribute providers** (`.claude/rules/abac-providers.md`) | A new `profile`/`character` attribute provider emitting `attrs["visibility"] = ""` for unset | Omit the key; emit a `has_visibility` witness. `""` type-checks and creates a fail-open match against any other unresolved peer |
| **`character_roles` / `PlayerHasRole`** (`internal/store/role_store.go:83-103`) | Assuming a role granted to one character is scoped to that character | It is **player-wide**: "true iff **any** character of playerID has role." An admin grant on an alt is an admin grant on the player |
| **`AssertOperatorAdmin`** (`internal/admin/auth/operator_admin.go`) | Writing a new, differently-shaped admin gate for the web path | Copy the pattern: one shared helper, called at **every** entry point, typed `DENY_*` codes, with the "prevents the three sites from drifting" rationale carried into the new helper's doc comment |
| **Gateway boundary** (`.claude/rules/gateway-boundary.md`) | Redacting, aggregating, or authorizing in `internal/web/` or a SvelteKit `load` | The gateway proxies. Redaction and authorization are computation and belong in core |
| **Typed-RPC rule** (ADR `holomush-v4qmu`) | Implementing rename/retire/admin-edit via `sendCommand`/`HandleCommand` | Structural writes get typed RPCs. Only conversational verbs use the command path |
| **World version guard** (migration `000049`, MODEL-03) | New character-mutation RPCs with `UPDATE ... WHERE id = $1` | Version-predicated CAS + `expected_version` on the request; surface `WORLD_CONCURRENT_EDIT` |
| **Transactional outbox** (MODEL-04) | Emitting the profile-changed / admin-audit event after commit | Intent written in the same transaction; the post-commit path was deleted, not deprecated |
| **`events_audit` + DLQ** (INV-EVENTBUS-29/30) | A bespoke `admin_audit` table | Ride the host audit projection: retention, never-drop DLQ, and the `holomush audit dlq` replay CLI come free |
| **Invariant registry** (`.claude/rules/invariants.md`) | Inventing `INV-PROFILE-*` / `INV-ADMIN-*` ad hoc in the SPEC without registering | Allocate ids in an existing scope (`ACCESS`, `PRIVACY`) or declare a new boundary in `invariants.yaml`. Ship `binding: pending` rather than fabricating a `// Verifies:` |
| **Plugin runtime symmetry** | Adding a profile-read host capability for Lua but not binary (or vice versa) | Any host-side gate applies to both; asymmetry only when both reach the same policy chokepoint via different transport |
| **`core-scenes` name resolution** | Adding profile display-name lookups as a new denormalization into scene payloads | Resolve at read time through the projection layer; do not add a copy that privacy changes cannot reach |

---

## Performance Traps

| Trap | Symptoms | Prevention | When it breaks |
|---|---|---|---|
| `LOWER(name)` uniqueness check with no index (`adapters.go:38-50`) | Slow character creation; CPU spike on the DB during a signup burst | Unique index on a stored normalized-name column; the index serves both correctness and speed | Noticeable in the low thousands of characters; severe if the UI adds live name-availability checking (one seq scan per keystroke) |
| Admin character list with no pagination, mirroring `ListAllCharacters`' documented "fetch-all, no pagination" (`core.proto:100`) | Admin page slow, then times out | Paginate the admin list from day one — but **not** with a `total_count` over a privacy-partitioned set (Pitfall 4) | Whenever the game exceeds a few thousand characters; the directory has the same issue today |
| N+1 on profile enrichment (presence, location name, scene count per row) | List latency scales linearly with page size | Batch-resolve in one query, or omit enrichment from list views entirely — which is also the privacy-safe answer (Pitfall 2) | ~50-row pages |
| Per-field privacy evaluated per field per row | Quadratic-ish work on list endpoints | Resolve the viewer's relationship to the owner **once per row**, then apply a precomputed mask | Large list pages; worse if the tier model gains a membership lookup |
| Media schema queried per profile even when empty | Extra round trip on every profile view for a feature with no data | Left-join or lazy-load; the zero-image case must cost nothing | Immediately, since it is the 100% case for this milestone |

---

## Security Mistakes

Domain-specific, beyond generic web security.

| Mistake | Risk | Prevention |
|---|---|---|
| Homoglyph/zero-width character names (normalization is `Fields`+`ToLower`+title-case only, `validation.go:114-126`) | Impersonation of admins or notable players on a now-public profile surface | NFKC + strip `Cf` + confusable folding or script restriction; decide policy in the SPEC |
| Name released on retirement | New character inherits every historical pose/archive that captured the name | Never release, or release only with a name-history table + cooling-off |
| Character enumeration via profile URLs / name-availability endpoint | Full roster harvest; correlation of alts to a player | Note `RequestPasswordReset` already implements enumeration-prevention deliberately (`core.proto:110-112`) — mirror that instinct: rate-limit and make responses uniform |
| Alt-linking disclosure | Profile or admin surface reveals that two characters share a `player_id` — a privacy harm players do not expect | `player_id` MUST NOT appear in any public or non-admin projection. Add it to the census assertion (Pitfall 2) |
| Role grant on an alt = player-wide admin (`role_store.go:83-103`) | Escalation via the least-scrutinized character | Exclude roles from the character-admin field mask entirely this milestone |
| Admin editing their own player's characters | Unreviewable self-service escalation | Distinguishable audit entry at minimum; deny role-affecting self-action |
| Presence/online-state joined into public profiles | Reveals real-world activity patterns; `core.proto:100-106` already calls online state "a separately-permissioned attribute" | Keep the existing boundary; bind it as an invariant so the profile work cannot cross it silently |
| Admin RPCs reachable without the shell | Full character administration by any authenticated session | `AssertWebAdmin` first statement in every admin handler; census test over the registry |
| Retired character still in presence/search | "Deleted" identity remains discoverable; user believes it is gone | Lifecycle predicate in the projection layer + census test |
| Reserved non-`optional` privacy fields serializing `""` | Present-and-empty is fail-open under absence-means-hidden semantics | Reserve numbers, not fields; all privacy-bearing fields `optional` |

---

## UX Pitfalls

| Pitfall | User impact | Better approach |
|---|---|---|
| A privacy toggle whose real scope is narrower than its label | User believes past scenes are now private; they are not (Pitfall 3) | State the scope at the toggle: "hidden from your profile — published scenes keep what was recorded" |
| Rename with no warning about historical logs | User renames to escape a name and finds the old one all over the archives | Warn at rename; offer the name-history explanation |
| "Delete character" that actually retires | User expects erasure; gets a hidden row | Call it "Retire." If erasure is genuinely wanted, that is a separate, designed feature |
| Admin nav showing six sections that do nothing | Every admin's first impression is a broken product; support load | Render reserved sections visibly disabled with "coming in a future release," or hide them from the nav while keeping them registered server-side |
| Concurrent-edit failure surfaced as a generic error | Admin and player silently clobber each other, or see "internal error" | Surface `WORLD_CONCURRENT_EDIT` as "someone else changed this — reload" |
| Name-taken error only after a long form submit | Repeated form loss | Inline availability check — but rate-limited and index-backed (Performance Traps) |

---

## "Looks Done But Isn't" Checklist

- [ ] **Per-field privacy:** often missing enforcement on list/search/export — verify via a
      census over *every* character-returning RPC, not per-endpoint tests
- [ ] **Per-field privacy:** often missing on the *newest* field — verify the field↔privacy
      table is a **bijection** meta-test against the schema, so a new field fails RED
- [ ] **Per-field privacy:** often leaks via order/count/pagination total — verify with a
      two-viewer differential test asserting byte-identical responses modulo declared diffs
- [ ] **Admin authorization:** often present only at the route — verify by calling each admin
      RPC directly with a non-admin session and asserting the **specific** `DENY_*` code
      (~~top-level, via `oops.AsOops`~~ — **superseded**: assert over the wire via
      `status.Code(err)` and a generic `status.Convert(err).Message()`; `oops.AsOops(err).Code()`
      resolves the *deepest* chain code and passes on a double-wrap. See `01-SPEC.md` §12.1 rule 5
      and §14 row 8; issue **#4902**), with a paired positive control proving the subject is
      otherwise valid
- [ ] **Reserved admin sections:** often ungated — verify all six reserved sections are
      registered with a mandatory authorization descriptor and each returns a denial to an
      unprivileged caller **today**
- [ ] **Admin mutations:** often missing before-values — verify the audit payload carries
      `before` and `after`, and that it is written in the mutation's transaction (test by
      injecting a post-commit failure)
- [ ] **Name uniqueness:** often only sequentially tested — verify with a concurrent race
      test against real Postgres, demonstrated RED against the current unindexed schema
- [ ] **Rename:** often "done" when the column changes — verify the declared-historical vs.
      declared-live surface table, each row citing its SPEC section
- [ ] **Retire:** often only hides from one query — verify via census + a lint/`ast-grep`
      assertion that every `characters` query carries the lifecycle predicate or is allowlisted
- [ ] **Retire:** often ships without deciding name release — verify the decision is written
      down and tested
- [ ] **New mutation RPCs:** often skip the version guard — verify `expected_version` is on
      every request message and the resilience harness is pointed at the new RPCs
- [ ] **Media schema:** often unverifiable — verify by actually inserting 1 primary + 10
      gallery rows through the schema today
- [ ] **Privacy tiers:** often one is unimplemented — verify every enum value has a real
      evaluator and unknown values default-deny exhaustively
- [ ] **Every new gate:** verify it was **observed RED** against the pre-fix state. A gate
      never seen failing is indistinguishable from one that cannot fail

---

## Recovery Strategies

| Pitfall | Recovery cost | Recovery steps |
|---|---|---|
| Private field leaked in a response | **HIGH** — data already disclosed | Fix the projection; audit access logs for the window; assume disclosed and notify. Wire-shape fix may be breaking |
| List endpoint leaks what detail protects | MEDIUM | Add the projection + census test. Same disclosure assumption as above for the exposure window |
| Admin RPC unauthenticated in production | **HIGH** | Gate immediately; audit `events_audit` for calls in the window; rotate anything a mutation could have touched |
| Reserved section wired without a gate | MEDIUM | Gate it; audit; then retrofit the mandatory-descriptor registry so it cannot recur |
| Duplicate names created by the TOCTOU race | MEDIUM | Detect, rename duplicates (with player contact), then add the unique index. Cannot add the index until duplicates are gone |
| Rename left stale names in archives | **LOW if declared, HIGH if not** | If the SPEC declared it historical: a doc/UI fix. If not: the archives are append-only, so the only real remedies are a name-history resolver or accepting it |
| Character hard-deleted, cascade dropped roles | **HIGH** — data gone, no audit | Restore from backup. Prevention (lifecycle column) is far cheaper than any recovery here |
| Non-version-guarded mutation clobbered an edit | MEDIUM | The lost write is unrecoverable without an audit before-value — which is why Pitfall 9's before-values matter for Pitfall 13's recovery |
| Reserved proto field repurposed with new semantics | MEDIUM | Deprecate the field, `reserved` its number, add a new one. Any consumer reading the old meaning breaks |
| Privacy tier shipped unimplemented, users selected it | MEDIUM | Implement it, or migrate selections to the strictest tier and notify. Never silently reinterpret |

---

## Pitfall-to-Phase Mapping

Phase names are indicative; the mapping is by content, and Phase 1 is the portal SPEC phase
PROJECT.md already names as opening the milestone.

| # | Pitfall | Prevention phase | Verification that can actually fail |
|---|---|---|---|
| 1 | Client-side field hiding | **Phase 1 SPEC** (message shape) → profiles | Marshal the response; `NotContains` a distinctive sentinel seeded into the private field |
| 2 | List/search leaks | **Phase 1 SPEC** (read-surface inventory) → profiles + admin | Census with **set equality** over every character-returning RPC |
| 3 | Export/history/archive leaks | **Phase 1 SPEC** (scope decision) → profiles | Paired positive+negative assertion on a proven-non-degenerate export fixture |
| 4 | Order/count/filter side channels | **Phase 1 SPEC** (constrain sort/search) → profiles | Two-viewer differential test asserting byte-identical responses |
| 5 | Owner vs admin path asymmetry | **Phase 1 SPEC** (audience matrix) → profiles + admin | Negative test aimed at the **privileged** endpoint with an **unprivileged** subject |
| 6 | Authz in nav/route only | **Admin shell — first deliverable** | Direct RPC call asserting the top-level `DENY_*` code, with a paired grant-succeeds control |
| 7 | Reserved section wired ungated later | **Admin shell** (mandatory authz descriptor) | All six reserved sections registered + denied to an unprivileged caller today |
| 8 | Escalation via character-admin edit | **Admin character administration** | Field-mask **set equality** + a schema meta-test banning role-ish field names |
| 9 | Missing/untrustworthy admin audit | **Admin character administration** (gate on first mutation) | Inject a post-commit failure; assert the record survives |
| 10 | Name uniqueness / normalization | **Phase 1 SPEC** (normalization policy) → character management | Concurrent race against real Postgres, demonstrated RED pre-index |
| 11 | Rename breaks name-captured refs | **Phase 1 SPEC** (surface enumeration) → character management | Declared-historical vs declared-live table, each row citing its SPEC section |
| 12 | Retire leaks / name reuse | **Phase 1 SPEC** (lifecycle column) → character mgmt + admin | Census + lint that every `characters` query carries the predicate or is allowlisted |
| 13 | Bypassing the v0.12 version guard + outbox | **Character management** (first mutation RPC) | Point the existing two-replica resilience harness at the new RPCs |
| 14a | Reserved proto fields rot | **Phase 1 SPEC** | Reserve numbers only; lint that every privacy-bearing field is `optional` |
| 14b | Empty nav registry rots | **Admin shell** | Registry contract exercised by six real consumers on day one |
| 14c | Media schema with no consumer rots | **Profiles** | Actually insert 1 primary + 10 gallery rows through the schema today |
| 14d | Privacy tier with one user rots | **Phase 1 SPEC** (tier count) → profiles | Every enum value has an evaluator; exhaustive `switch` with default-deny; tier applied to ≥2 fields |

---

## Sources

**Primary — files read in this repository (all claims about current behavior cite these):**

- `.planning/PROJECT.md` — milestone scope, locked decisions, constraints
- `.planning/MILESTONES.md:24` — the "verification that cannot fail" theme; `:15` — outbox/version-guard outcomes
- `.claude/rules/abac-providers.md` — omit-don't-sentinel; missing-attr-is-false semantics
- `.claude/rules/gateway-boundary.md` — proxy-not-compute; typed RPCs for structural writes
- `internal/store/migrations/000001_baseline.up.sql:68-88, 99, 140-160` — `characters` DDL (no unique index on name), `character_roles`, FK inventory with `ON DELETE` semantics
- `internal/store/migrations/000049_world_version_guard.up.sql:20` — `characters.version`
- `internal/store/migrations/000045_character_preferences.up.sql:5`, `000051_player_reaping.up.sql` — the only other `characters`/lifecycle-adjacent migrations
- `internal/bootstrap/setup/adapters.go:38-50` — `ExistsByName` SQL
- `internal/auth/character_service.go:103-134` — check-then-insert creation pipeline
- `internal/world/validation.go:114-126` — `NormalizeCharacterName`
- `internal/store/role_store.go:83-103` — `PlayerHasRole` ("any character of playerID")
- `internal/access/role.go`, `internal/access/grants.go:44-83` — roles; `HasPlayerGrant` absent-key/type-assert semantics
- `internal/admin/auth/operator_admin.go`, `internal/admin/auth/ingame.go:116-121` — `AssertOperatorAdmin`, the re-assert-at-every-entry-point pattern (INV-CRYPTO-83)
- `api/proto/holomush/core/v1/core.proto:100-112, 688-708` — `ListAllCharacters` doc (online state separately permissioned), `CharacterSummary`
- `api/proto/holomush/web/v1/web.proto:329, 345, 351, 496-513` — scene export/archive RPCs, web `CharacterSummary`
- `plugins/core-scenes/commands_emit_test.go:153-190`, `poseorder.go:20`, `service.go:1509/2022` — `actor_display_name` / `CharacterName` denormalization into events and `scene_log`
- Grep result: **zero `RoleAdmin` references under `internal/web/`** — the web admin trust boundary is net-new

**Secondary — repo conventions applied as prevention:**

- `.claude/rules/grpc-errors.md` — top-level oops code assertion for opacity contracts
- `.claude/rules/invariants.md` — registry allocation; no fabricated bindings
- `.claude/rules/database-migrations.md` — no triggers/functions; no in-migration backfills
- `.claude/rules/references/plan-review-learnings.md`, `design-review-learnings.md` — the
  "demonstrate RED against the pre-fix state" and set-equality-over-loop reflexes

---
*Pitfalls research for: identity + admin surfaces on an existing default-deny-ABAC game platform*
*Researched: 2026-07-31*
