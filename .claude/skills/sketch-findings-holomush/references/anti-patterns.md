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

### 3d. The schema guard must be schema-level, not allowlist-level

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

## Origin

Synthesized from sketches: **001, 002, 003, 004** (plus the wrap-up's own verification pass)
