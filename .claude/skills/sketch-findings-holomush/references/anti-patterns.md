# Anti-patterns — what the sketches got wrong, and how

Cross-cutting. Every entry here is something a sketch **actually produced** and then had to
correct, or something the SPEC explicitly warns will be produced by reflex. This is the
highest-value file in the skill: these are the mistakes Phase 6 is most likely to repeat,
because each one is the *default instinct*.

---

## 1. Fabricating a field that does not exist

**What happened.** Sketch 001's first draft shipped a `Last seen` column in the character
table. There is no such column. `characters` is `id, player_id, name, description,
location_id, created_at, version, preferences` (`000001_baseline.up.sql:68-75`, plus
`000049` adding `version` and `000045` adding `preferences`); Phase 2 adds `status`.

**Why it happened.** "Last seen" is the default instinct for any admin list. It is what an
admin table *looks like*, so it got drawn without checking the schema.

**Why the obvious derivations also fail.** Both candidate sources answer *"online now"*, not
*"last active"*: `sessions` rows are **reaped**; `session_connections.last_seen_at` is a
**gateway lease** reaped by the lease sweep (`internal/session/reaper.go:29`).

**Corrected to** `version` (`Ver`), which does exist and is load-bearing (§9.4).

> **Rule:** before drawing a column, field, or badge, confirm the backing column exists in a
> migration. Sketch 002 flags this as *"the single most likely column for Phase 6 to invent
> by reflex."*

---

## 2. Writing speculative scope into the UI

**What happened.** Sketch 003's variant C ("Scope preview") says what will live in each
planned section. Only **`config`** has documented scope anywhere in the SPEC (§10.1 + §14).
`stats`, `players`, `moderation`, `audit`, `plugins` have **none** — a scope panel would be
**invented for five of six sections**.

`moderation` gets the closest thing to a hint (§7 notes profile warnings need *"somewhere to
live before moderation exists (EXT-06)"*) — but that is a statement about a different
feature's dependency, not a scope.

**Why this is worse than it looks.** Plausible-sounding UI copy **is read back as a
requirement** by the next planner. Inventing "Moderation: review reports, issue warnings,
manage bans" creates a spec out of nothing.

> **Rule:** never write speculative scope into the UI. If a scope panel is wanted, make it
> **conditional** — render only where documented scope exists, fall back to the minimal
> state elsewhere.

The sketch renders this honestly (real content for `config`, an explicit "no documented
scope" panel for the other five) so the variant **argues against itself** rather than
needing a paragraph to explain why it lost. That is a good pattern for sketching a variant
you suspect will fail.

---

## 3. Verification that cannot fail

This is a **recurring family** in this codebase, not a one-off.

### 3a. The escalation test with no positive control

> §10.6 — *"A test that calls `AdminUpdateCharacter` with a `roles` field the message does
> not have proves nothing — the request never carried the payload, the assertion 'role
> unchanged' is satisfied by the field being silently dropped, and the test passes whether
> or not the property holds."*

**Mandated shape:** first demonstrate the write path works on a field it *is* allowed to
change, **then** attempt the escalation on the same call.

### 3b. `oops.AsOops(err).Code()` returns the **deepest** code, not the top-level one

Verified at `oops@v1.22.0/error.go:230-235` (`getDeepestErrorCode`). `.claude/rules/
grpc-errors.md` states the opposite and is **wrong** (issue
[#4902](https://github.com/holomush/holomush/issues/4902)). Under
`oops.Code("INTERNAL").Wrap(oops.Code("STREAM_ACCESS_DENIED")…)` it yields
`STREAM_ACCESS_DENIED` while the wire carries `INTERNAL` — so an opacity test written to
that rule **asserts the opposite of the property and passes while the wire leaks.**

> **Rule:** assert opacity **at the wire**, not via `Code()`.

### 3c. A false-green search during this very wrap-up

Verifying the theme claim below, three consecutive commands printed a clean
`mismatches=0` while **every comparison had errored**:

```bash
# BROKEN — `--` ends flag parsing, so -P became a file path
rg -o -- '--color-[a-z0-9-]+(?=:)' -P web/src/app.css

# BROKEN — a pattern starting with `--` is parsed as a flag
rg -oN "--color-primary:[^;]*" web/src/app.css   # rg: unrecognized flag

# CORRECT
rg -oN -e "--color-primary:[^;]*" web/src/app.css
```

Both failures produced **empty input to the comparison loop**, and an empty loop reports
zero mismatches. Per `.claude/rules/grepping.md`: decide pass/fail by **exit code**, and
demonstrate a gate goes **RED** against a known-bad state before trusting its green.

> **Rule:** a check never observed failing is indistinguishable from one that cannot fail.

### 3d. The fingerprint check that passes by construction — **and its antidote**

Sketch 010 compares four "nothing here" paths by fingerprinting each rendered panel. **In a
correct implementation the renderer does not branch on which kind of miss occurred, so the
panels are identical by construction and the check passes trivially.**

So the sketch **ships the failure**: a `☣ inject leak` toggle makes one path render a
bespoke "Access denied" page. Fingerprints diverge, the match border drops, the note flips
to a disclosure warning. **Verified both directions — identical inputs → green, one panel
changed → red.**

> **This is the pattern to copy, not just the warning.** Any opacity/indistinguishability
> test must ship a demonstrated red state alongside its green one.

### 3e. The schema guard must be schema-level, not allowlist-level

Because the real risk is a **future** field, §10.6 mandates **two** tests, not one:

1. A **meta-test** failing if the admin character message ever gains a field matching
   `role|grant|permission|capability`.
2. An **allowlist test** asserting set equality against the checked-in 13-path list.

The set-equality test pins today's list; the schema test catches tomorrow's field before
anyone thinks to add it to the mask.

---

## 4. Inheriting a premise nobody checked

**What happened.** Sketch 004 was handed off (by sketches 002 and 003, and by earlier notes)
as covering *"the irreversible delete"*. **There is no delete in the admin portal.** §9.3's
census has `AdminUpdateCharacter` / `AdminRetireCharacter` / `AdminUnretireCharacter` and
**no `AdminDeleteCharacter`**; §4.4 and §10.6 both forbid wiring
`world.Service.DeleteCharacter` to an admin button.

The correct framing: **the destructive action is Retire, which is reversible.**

> **Rule:** a premise arriving through a hand-off chain is not evidence. Check it against the
> census/SPEC before designing on top of it — sketch 004 had to correct its own stated
> design question.

---

## 5. Claiming a file is a verbatim mirror when it is not

**What happened.** Both `sources/themes/default.css`'s own header comment and the sketch
MANIFEST assert it *"mirrors `web/src/app.css` verbatim"*. It does not.

It carries **34 of 39 `--color-*` tokens at byte-identical values** (all shared values
agree — the colors *are* trustworthy), but it restructures `@theme` into plain `:root` and
**drops** `@layer base`, the density tokens, and the `prefers-reduced-motion` keyframes.
Five tokens are absent (all unused by the sketches).

**Why it matters.** "Verbatim" invites treating the file as an app.css substitute. It is
not: the sketches inherit **no density tokens and no reduced-motion gating**, both of which
production must apply.

> **Rule:** "verbatim" / "mirrors" / "identical to" is a *checkable* claim. Check it, or
> weaken the wording. See `foundations.md` for the accurate characterization.

---

## 6. Hiding a thing and calling it enforcement

**What happened (nearly).** `/admin` is invisible without permission and deep links render
not-found. It would be easy to read that as the access control.

> §10.4 — a route guard is *"bypassable by any caller who skips the route."*

SC5's rule — *"drawing a link the viewer may not use still results in a denial at the RPC"*
— has an inverse that matters just as much: **not drawing the link does not remove the need
for that denial.**

And the consequence runs the wrong way: because the nav is hidden and deep links bounce,
**the only callers still reaching the denial path are those deliberately bypassing the UI**
— i.e. hiding the entrance *concentrates* the SPEC-defect-D1 disclosure onto exactly the
population it leaks to, rather than mitigating it.

> **Rule:** UX invisibility is never the boundary. The ABAC gate is.

---

## 7. Distinguishable denial codes as an enumeration oracle

`DENY_ADMIN_SECTION` vs `DENY_ADMIN_SECTION_UNREGISTERED` lets an unauthorized caller
distinguish a real section id from a fake one — the exact disclosure §10.3 forbids. **The
UI renders both identically; the leak is on the wire**, which is where nobody looks during a
UI review. Tracked as [#4904](https://github.com/holomush/holomush/issues/4904); full
analysis in `gating-and-absence.md`.

> **Rule:** when two refusals must be indistinguishable, the property lives on the wire —
> status, error code, **and** body. `INV-PRIVACY-9` is the in-repo pattern to copy.

---

## 8. Collapsing a column without checking what it took with it

Merging the admin nav into the rail (sketch 001) produced **two** defects at once: two
active bars in one column at two hierarchy levels, and the silent loss of the identity block
and `⌘K` affordance to `width: 0`.

> **Rule:** on any collapse, check for (a) a duplicated "you are here" marker and (b)
> orphaned identity/affordance blocks. A merged column needs an explicit hierarchy device
> that two columns gave for free — and that device must be **scoped to the collapsed
> breakpoint**, or it damages the full-width layout.

---

## 9. Reaching for the conventional admin-table shape

SPEC §11.3 names *"a sort control whose options are drawn from the §7.2 field list"* as
**the specific warning sign**, because that list is the privacy-bearing set. Sort-by-anything
and facet-everything are prohibited by construction.

Likewise: **no multi-select, no bulk bar** — §9's admin RPCs are all singular, so a bulk
surface implies either N sequential calls with partial-failure UX or a batch RPC the census
does not contain.

And `total_count` is safe on the admin list **only** because it is not privacy-partitioned.
It would **not** be safe on the public directory.

> **Rule:** the shape of "a normal admin tool" is the thing under constraint here. Check
> §11.3 before adding an affordance, not after.

---

## 10. Fabricated **copy** — and how far it travels before anyone checks

**What happened.** Sketch 008's create card read *"Names are permanent once taken."* Sketch
009's confirm step read *"I understand this name is permanent. It cannot be changed in this
release."*

**Both were false.** v0.13 ships player rename — **IDENT-03**,
`CharacterAccessService.RenameCharacter`, owner-scoped, ABAC `write` on `character:<id>`,
SPEC §9.4.2 line 1805, **Phase 3** — and Phase 3's own ROADMAP goal line names it
explicitly. What *is* true is that a name is **reserved** once taken, and stays reserved
after retire (§4.4, §4.5).

**Why this is worse than the fabricated `last seen` column (§1).** Three reasons:

1. **It propagated.** Two independent sketches asserted it. A fabricated *field* fails
   loudly at build time; fabricated *copy* renders perfectly.
2. **It would have inverted a decision.** 009's winner (A — submit & report, which lets the
   server silently rewrite the name and reports afterwards) is only acceptable **because
   rename exists**. Had "permanent" been true, A would have been the *wrong* choice.
3. **It was caught by luck of process, not by review.** 008 wrote down "sketch 009 tests
   this claim"; 009 went and checked. Nothing structural would have caught it.

> **Rule:** UI copy that asserts a *property of the system* ("permanent", "cannot be
> undone", "always", "never") is a **claim**, and claims get grounded to a SPEC section or a
> `path:line` like any other. If it also carries a **dependency** — decision X is only safe
> because claim Y holds — write the dependency down where the decision lives.

---

## 11. Conflating the platform brand with the game world

**What happened.** Sketch 010's first draft shipped a **"Back to HoloMUSH"** button.

Wrong twice over:

1. **HoloMUSH is the platform, not the game.** `.claude/rules/branding.md` **INV-6**: the
   brand is *"the software/platform only — never the game world / default setting."* A
   player is in *a game that runs on* HoloMUSH.
2. **The game's name is not the platform's to assume.** It exists as
   `SettingConfig.DisplayName` (**required** on setting plugins,
   `internal/plugin/manifest.go:211`, enforced at `:494`) — **but it reaches no web
   surface.** No RPC carries it. Tracked as
   [#4905](https://github.com/holomush/holomush/issues/4905).

**Corrected to `Home`** — viewer-agnostic, no new surface, conflates nothing. The
`>holomush_` wordmark stays in the top bar; that is platform chrome, which INV-6 permits.

> **Rule:** never hardcode `HoloMUSH` in player-facing copy. If a surface needs the *game's*
> identity, that is a plumbing gap to raise, not a string to invent.

### ⚠ This entry demonstrated §1 while being written

Sketch 010 asserted, and the first packaging of this skill repeated, that *"the only
`HoloMUSH` strings under `web/src/` are SPDX copyright headers."* **False** — there are
three real render sites (`TopBar.svelte:66`, which is fine; `routes/+page.svelte:34`, a
fallback; `terminal/+page.svelte:689`, a bare `<h1>`). See `foundations.md` for the table.

The claim was never run as a search that could have failed — it is §1 (fabricating a fact
about the tree) and §3 (verification that cannot fail) at once, committed **inside the file
that documents both**. It survived a sketch review, a wrap-up curation pass, and a commit.

> **Rule:** a claim of the form *"the only X in the tree is Y"* is an **exhaustiveness**
> claim. Run it, read every hit, and classify each one — do not report the shape of the
> result you expected.

---

## 12. The conditional disclosure notice — where "the obvious improvement" is the bug

**What happened.** Sketch 007's variant B added *"Signed-in players see more on some
profiles."* to signed-out profile views.

That notice is **legal only because it is unconditional** — same text, every profile, every
signed-out visitor, regardless of whether anything was withheld.

**The natural later "improvement" — show it only when something *was* withheld — is a
which-characters-have-populated-profiles oracle.** It is the more thoughtful-looking change,
and it is precisely the leak §7.5/§8.9 exist to prevent.

**B was rejected**, which closes the channel *by construction*: v0.13 never ships the notice,
so nobody can later "improve" it.

> **Rule:** when a UI element is safe only because it is **unconditional**, that
> unconditionality is a **requirement**, not an implementation detail — state it where the
> component lives. And prefer not shipping the element at all when the conditional version
> is the obvious next step.

Same family: counts, lock icons, greyed sections, progress indicators, "N more below"
affordances. **Anything whose presence or value varies with how much was withheld.**

---

## 13. Building "coming soon" slots for a feature that ships no data

**What happened.** Sketch 007 rendered a Gallery section. §7.3 ships the media model as
*"the schema and the proto shape only, with **zero upload behavior**. There is no uploader,
no storage backend, and no media-serving path."*

So in a real v0.13 deployment **no media rows exist**, and by §8.9's absence rule the Gallery
**never renders for anyone**. The sketch showed it only because its fixture asserts rows
exist, to prove the model has a rendering.

**Do not build empty dashed slots as a "coming soon" affordance.** That is §2's
speculative-scope mistake one layer down: it invents a promise the SPEC does not make.

> **Rule:** build the renderer so the data *would* render if present, and ship a page that
> shows **nothing** when it is not. An empty placeholder is a claim about the roadmap.

---

## 14. Promising something across a two-moment gap

**What happened.** Sketch 009's variant B checks name availability as you type and shows a
green tick.

**An availability check and an `INSERT` are two different moments.** Even with §6.1.3's
`UNIQUE` index doing the real enforcement, "available ✓" can be followed by a `23505` on
submit because someone took the name in between.

This is **not** the legacy TOCTOU — the index closes the *correctness* hole. It is a **UI
honesty** problem the index **cannot** close: the check is stale the moment it returns. B's
tick means *"probably"*.

> **Rule:** if the UI asserts a fact that a later write re-checks, the losing path is **the
> variant's defining requirement**, not an edge case to discover in implementation. Or
> choose a shape that never makes the promise — which is what A and C do.

---

## 15. Two vocabularies sharing one word on one surface

**What happened.** The roster **already ships** a status badge, and it is **session** state
(`hasActiveSession → 'Active'`, else `'Offline'`, `+page.svelte:132-136`). v0.13 adds
`characters.status` — **lifecycle** state (`active | retired | idle`).

They share the word **"status"** and the token **`active`**, while meaning entirely different
things. A retired card renders **"Retired · Offline"**, where `Offline` is *meaningless* on
a character that cannot be played at all — it is a second status competing with the first,
distinguishable only by knowing which vocabulary each token belongs to.

**Fix: a non-`active` lifecycle MUST suppress the session badge entirely.** Session state is
only meaningful for a character that could be connected.

> **Rule:** this is a `.claude/rules/terminology.md`-class problem — check whether a new
> domain word **already means something else on the same screen**. When it does, the new
> vocabulary avoids the collided token in its user-facing labels (which is why 008's
> sections are `Playable` / `Not playable`, not `Active` / `Inactive`).

---

## 16. The polite "Access denied" page — and the nested error boundary

**Two shapes, one failure.** Both destroy the not-found indistinguishability that three
independent surfaces (003 `/admin`, 007 unreachable profiles, and the rejected 006-C) rest
on.

**16a — the bespoke refusal page.** *"Access denied — you don't have permission to view this
area."* It is polite, it is helpful, it is what every other web app does — **and it is the
single most natural thing an implementer would write.** It hands a prober the fact that
`/admin/*` exists and they are merely excluded. This is exactly what 010's `☣ inject leak`
toggle renders, to prove the check can go red (§3d).

**16b — the second `+error.svelte`.** SvelteKit resolves the **nearest** boundary. Once
boundaries are nestable, someone adds `routes/admin/+error.svelte` and **the property dies
silently with no test failing** — a one-file PR that looks harmless. This is why 010's
variant C (in-shell not-found) was rejected despite being the nicer experience.

> **Rule:** one root `+error.svelte`, and a meta-test asserting **exactly one** exists under
> `web/src/routes/`. Never a "forbidden" page, never a redirect, never an echo of the
> requested path. And the renderer takes **no reason parameter** — if it has nothing, a
> future change cannot surface it.

**Calibration, so this is not over-applied:** indistinguishability is **per-viewer, not
global**. An admin hitting `/admin/moderation` *should* see it resolve — that is the gate
permitting them. Requiring global sameness would forbid ever showing an admin their own
screen.

---

## 17. Validating a surface in isolation and calling the decision settled

Two instances, one root cause.

**17a — the surface with no frame.** Sketch 004 was built **entirely standalone**: zero
`shell`, zero `rail`, zero `adminnav`, no container query, outermost wrapper a bare
`.stage`. **The edit Sheet had never rendered inside the frame it will live in** — and the
untested collision was specific: at 768–1023px the content column is at its **narrowest**
exactly where a 380px Sheet is **widest relative to it**. Sketch 005 exists solely because
of that.

**17b — the band decided once and never promoted.** The `<768px` treatment was designed on
**one** surface (001's table). 002 half-inherited it (zeroed only `.rail`, no `.mobilebar`);
003 had **none**; 004 had no shell at all. Sketch 006 exists solely because of that.

> **Rule:** a decision made on one surface is not a decision for the set. Before treating a
> cross-cutting choice as settled, **enumerate the surfaces it should apply to and check each
> one** — the drift is invisible per-sketch and obvious in a table.

The corollary is a cheap-sequencing win worth copying: 005 picked the **simplest** geometry
and *deliberately left the phone band wrong*, handing it to 006 rather than settling it by
assumption. Paying only where it broke cost **one CSS block**; adopting the three-treatment
design up front would have cost three.

---

## Origin

Synthesized from sketches: **001–010** (plus the wrap-up's own verification pass).
Entries 1–9 come from round 1 (001–004); 10–17 from round 2 (005–010).
