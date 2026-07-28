---
phase: 09-test-quality-code-health-sweep
reviewed: 2026-07-26T00:00:00Z
depth: deep
files_reviewed: 115
findings:
  critical: 1
  warning: 6
  info: 7
  total: 14
status: issues_found
---

# Phase 9: Code Review Report

**Reviewed:** 2026-07-26
**Depth:** deep (cross-file: import graph, meta-test ↔ registry ↔ marker bijection, doc ↔ code contracts)
**Scope:** `main..HEAD` git diff (115 source files), NOT the 41 files the SUMMARY artifacts declare.
**Status:** issues_found

## Summary

The eight production files are clean. I found no security defect, no logic error, and no
convention violation in the gateway default inversion, the three ABAC providers, the downgrade
fence, `alias.go`, or the two harness seams. The ABAC sentinel removal is correct and — because
`location_test.go` / `property_test.go` compare the whole attribute bag with `assert.Equal` —
the omission is genuinely pinned, not merely un-asserted.

The rename-sweep claim (plan 09-18) **holds**. I verified it independently rather than accepting
it: 68 of the touched test files have byte-identical add/delete counts, and every removed line
across that set is a `func Test…` declaration, a comment, or a table-case `name:` label — zero
assertions were lost. The 32 removed lines matching `assert.|require.|Expect(` across the whole
test diff are all accounted for (24 are unreachable code below a `Skip(...)` in the four
`eventbus_e2e` stubs; the rest are deliberate strengthenings). I also swept all 140 renamed test
identifiers for dangling references across the tree: all live references were updated except one
(WR-04); the rest are historical plan docs.

The one Critical finding is an instance of the *exact* anti-pattern this phase set out to
eliminate — `NotTo(BeZero())` on a bigint-nanos timestamp column that round-trips as the UNIX
epoch — committed six lines below a comment that documents the trap, and repeated five times.

**Categories explicitly clean:**
- **Security** — no findings. No injection, no path traversal, no hardcoded credential, no
  unsafe deserialization, no authz bypass in the diff. The `--secure-cookies` inversion moves
  the default in the fail-safe direction and the config-file opt-out is preserved and tested.
- **Production correctness** — no findings in the 8 production `.go` files.
- **Migration 000053** — content is correct: idempotent up, paired reversing down, no
  triggers/functions, plain (non-`CONCURRENTLY`) build correctly justified against the
  transaction-wrapping runner.
- **Terminology / logging / event-construction / oops conventions** — no violations found. The
  new fence log correctly uses `WarnContext` with snake_case keys and a lowercase static
  message; `EmitDirectEventAt` correctly uses `eventbus.NewEvent`.

---

## Critical

### CR-01: `NotTo(BeZero())` cannot fail on `LocationArrivedAt` — the privacy-floor existence assertion is vacuous, 5×

**File:** `test/integration/session/lifecycle_attach_test.go:108`, `:135`, `:149`, `:329`;
`test/integration/session/lifecycle_ttl_test.go:194`

**Issue:** `sessions.location_arrived_at` is a `NOT NULL BIGINT` of UNIX nanoseconds
(`internal/store/migrations/000041_auth_timestamps_to_bigint`), scanned through
`pgnanos.Time.Scan` → `time.Unix(0, v).UTC()` (`internal/pgnanos/pgnanos.go:31-32`). An unset
column (`0`) therefore materialises as **1970-01-01T00:00:00Z**, not Go's zero `time.Time`
(year 1). Gomega's `BeZero()` is a `reflect.DeepEqual` against the type's zero value, so
`Expect(info.LocationArrivedAt).NotTo(BeZero())` **passes on a completely unset floor**.

This is the same defect the phase catalogued and claims to have fixed. The file's own comment at
`lifecycle_attach_test.go:99-102` spells the trap out verbatim — *"the column round-trips as the
UNIX EPOCH when unset, not as Go's zero time, so `NotTo(BeZero())` would pass on an unset floor
and prove nothing"* — and the correct form is used for the sibling column on the very next
assertion (`GuestCharacterCreatedAt` at `:103` via `BeTemporally("~", info.CreatedAt, …)`, and
the negative direction at `:131` via `.Unix()).To(BeZero())`). Only the `LocationArrivedAt`
assertions regressed to the vacuous form.

**Failure scenario (the exact production change that leaves it green):** delete the
`location_arrived_at = $N` term from `PostgresSessionStore.Create`'s INSERT
(`internal/store/session_store.go:320-336`), so a freshly created session's floor stays `0`.
`streamScopeFloor` (`internal/grpc/scope_floor.go:34-38`) then filters every location history
query against 1970 — i.e. **no floor at all**, and every new session reads the full history of
its location including everything that happened before the character arrived (INV-PRIVACY-1
defeated, a privacy leak). All five assertions above still pass. This is the false-green class
the repo has been bitten by twice (INV-RB-3, INV-PRIVACY-7).

Compounding it: the reattach specs assert floor *preservation* with
`BeTemporally("==", originalArrival)` (`lifecycle_attach_test.go:225,263,294,348,379,400`),
which is also satisfied when both sides are the epoch. Those specs depend on CR-01's
preconditions for their meaning.

**Fix:** use the form the file already established for the sibling column.

```go
// lifecycle_attach_test.go:108 (and :135, :149, :329; lifecycle_ttl_test.go:194)
Expect(info.LocationArrivedAt.Unix()).NotTo(BeZero(),
    "INV-PRIVACY-1: a fresh session MUST carry a location arrival timestamp; the column "+
        "round-trips as the UNIX epoch when unset, so BeZero() must be applied to Unix()")
// stronger still — pins a REAL instant rather than merely a non-epoch one:
Expect(info.LocationArrivedAt).To(BeTemporally("~", info.CreatedAt, time.Minute),
    "INV-PRIVACY-1: the arrival floor MUST be set to a real instant near session creation")
```

Prefer the `BeTemporally("~", info.CreatedAt, …)` form at `:108/:135/:149` (it also catches a
floor set to a wrong-but-non-epoch value) and the `.Unix()` form at the two precondition sites.

---

## Warnings

### WR-01: The migration idempotency spec asserts idempotency of SQL it wrote itself, not of the migration

**File:** `internal/store/migrations_sessions_location_index_integration_test.go:138-164`
(specifically `:155-163`)

**Issue:** The spec's stated purpose is *"the up migration uses IF NOT EXISTS, so re-running it
against a database that already carries the index must succeed"*. It then hand-writes the
statement as a Go string literal —
``db.Exec("CREATE INDEX IF NOT EXISTS " + sessionsLocationIndexName + " ON sessions(location_id)")``
— and asserts *that* succeeds. The migration file is never re-executed. The first spec in the
file never runs the up migration twice either (it goes fresh→52→53→52→head, so the index is
always absent when `CREATE` runs).

**Failure scenario:** delete `IF NOT EXISTS` from
`internal/store/migrations/000053_sessions_location_index.up.sql:21`. Both specs in this file
still pass, `task test:int` stays green, and the repo's stated migration rule ("idempotent — use
`IF NOT EXISTS` so reruns are safe", `.claude/rules/database-migrations.md`) is silently broken.

**Fix:** execute the embedded migration source, not a re-typed copy.

```go
up, err := migrations.FS.ReadFile("000053_sessions_location_index.up.sql")
Expect(err).NotTo(HaveOccurred())
_, err = db.Exec(string(up))
Expect(err).NotTo(HaveOccurred(), "the up migration's own SQL must be idempotent")
down, err := migrations.FS.ReadFile("000053_sessions_location_index.down.sql")
Expect(err).NotTo(HaveOccurred())
_, err = db.Exec(string(down))
Expect(err).NotTo(HaveOccurred())
_, err = db.Exec(string(down))
Expect(err).NotTo(HaveOccurred(), "the down migration's own SQL must be idempotent")
```

(If the embed FS is unexported, add a small `//go:build integration` accessor rather than
re-typing the SQL — re-typing is what makes the current test unfalsifiable.)

### WR-02: `depguard_config_test.go` documents an exact set-match and implements a subset-containment check

**File:** `test/meta/depguard_config_test.go:24-41`

**Issue:** The new comment asserts *"The pinned set MUST match the CONFIGURED deny set exactly,
not a subset of it. A pin that covers only some entries lets the uncovered ones be deleted
silently — the same config-diverges-from-reality failure this test exists to catch."* The loop
below still only iterates the hard-coded slice and calls `require.Contains(t, cfg, "- pkg: "+pkg)`.
The reverse direction — every `- pkg:` entry under the `no-test-only-constructs-in-production`
deny list appears in the pinned slice — is not checked.

**Failure scenario:** a future change adds a sixth deny entry (say
`internal/testsupport/foobartest`) to `.golangci.yaml` without touching this test. It is
unpinned, so a later change deletes it and this test passes — reproducing exactly the failure
the comment says the test exists to catch. The narrowing to `"- pkg: "` (a genuine improvement)
does not address this.

**Fix:** parse the deny list and compare sets.

```go
var cfgDoc struct {
    Linters struct {
        Settings struct {
            Depguard struct {
                Rules map[string]struct {
                    Deny []struct{ Pkg string `yaml:"pkg"` } `yaml:"deny"`
                } `yaml:"rules"`
            } `yaml:"depguard"`
        } `yaml:"settings"`
    } `yaml:"linters"`
}
require.NoError(t, yaml.Unmarshal(data, &cfgDoc))
rule, ok := cfgDoc.Linters.Settings.Depguard.Rules["no-test-only-constructs-in-production"]
require.True(t, ok, "the deny rule MUST exist")
got := make([]string, 0, len(rule.Deny))
for _, d := range rule.Deny { got = append(got, d.Pkg) }
require.ElementsMatch(t, pinned, got,
    "the pinned set and the configured deny set MUST be identical — a new deny entry MUST be "+
        "pinned here in the same change, or it can be deleted silently later")
```

### WR-03: The session-matrix placement guard accepts a Ginkgo *pending* spec as coverage

**File:** `test/meta/session_matrix_registry_test.go:543`

**Issue:** The placement guard is
`if next, ok := nextNonCommentLine(lines, i+1); !ok || !strings.Contains(next, "It(")`. The
substring `"It("` is also contained in `XIt(` and `PIt(` — Ginkgo's *pending* spec constructors,
whose bodies **never execute** — and in `FIt(` (focused, which suppresses every other spec in
the suite). This is structurally the same near-miss the file's own regex comment cites as the
lesson of 09-11 (`Skip(` matching inside `NotSkip(`), applied one line later.

**Failure scenario:** a contributor whose lifecycle spec is failing changes
`It("resumes the one existing game session…")` to `XIt(...)`. The spec no longer runs. The
placement guard still passes (the line contains `It(`), the bijection still passes (the marker is
still well-formed and still names a `spec` row), the disposition counts are unchanged — and
`test/session-matrix.yaml` continues to advertise `telnet-tmux-reattach.telnet` as spec-covered
by a spec that executes nothing. There are no `XIt`/`PIt`/`FIt` in the tree today, so this is
latent rather than live.

**Fix:** anchor on the `It(` token rather than a bare substring, and reject the pending/focused
forms explicitly.

```go
// sessionMatrixSpecOpenRE matches a line that OPENS a running Ginkgo spec.
// XIt/PIt are pending (never execute) and FIt suppresses the rest of the
// suite; each contains the literal "It(", so a substring test accepts them.
var sessionMatrixSpecOpenRE = regexp.MustCompile(`(^|[^A-Za-z])It\(`)
var sessionMatrixPendingSpecRE = regexp.MustCompile(`\b[XPF]It\(`)
...
next, ok := nextNonCommentLine(lines, i+1)
if !ok || !sessionMatrixSpecOpenRE.MatchString(next) || sessionMatrixPendingSpecRE.MatchString(next) {
    scan.Misplaced = append(scan.Misplaced, ...)
}
```

### WR-04: A renamed test broke the lockstep the meta-test's own doc comment mandates

**File:** `internal/test/invariants/inv_p5_coverage_meta_test.go:137-141`

**Issue:** The ACE sweep renamed `TestReconnect_VsConcurrentLeave_Serializes` →
`TestRestoreConnectionFocusSerializesAgainstAConcurrentSceneLeave` and updated the `cases` slice
here. But the meta-test's doc comment (`:16-22`) states: *"Fix by updating the cases slice AND
the spec's §10 invariant table in lockstep — the two MUST agree at all times."* The spec §10 table
was not updated; the old identifier still appears in
`docs/superpowers/specs/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md`
and in the matching plan under `docs/superpowers/plans/`.

**Failure scenario:** this one is already realised — INV-SCENE-25's spec-level traceability now
points at an identifier that does not exist in the tree. A reader auditing INV-SCENE-25 greps the
spec's named test, finds nothing, and concludes the invariant is unbound. Nothing fails; the
divergence just accumulates. (I swept all 140 renamed identifiers; this is the only live
reference the sweep missed. All other stale hits are historical `docs/**/plans/` records, which
are correctly frozen.)

**Fix:** update the §10 row in the design spec (and the sibling plan, or leave the plan frozen and
note the rename in the spec) to name
`TestRestoreConnectionFocusSerializesAgainstAConcurrentSceneLeave`. Consider adding the reverse
check to the meta-test — assert the spec file contains each `testName` — so the lockstep is
mechanical rather than documentary.

### WR-05: The `--secure-cookies` default inversion is documented in one place; the install guide still shows the bare invocation

**File:** `site/src/content/docs/operating/how-to/deploy/installation.md:232` and `:319`

**Issue:** `cmd/holomush/gateway.go:92,130` inverts a security-relevant default that changes
runtime behaviour for existing operators. `site/src/content/docs/operating/reference/configuration.md`
got a thorough `:::caution` covering both directions. `installation.md` — the page an operator
actually follows to stand a server up — still shows `holomush gateway` bare in the two-terminal
walkthrough (`:232`) and in the systemd unit `ExecStart` (`:319`), with no mention of the flag and
no cross-reference to the caution.

**Failure scenario:** an operator following `installation.md` deploys the systemd unit on a LAN
host serving plain HTTP at `http://holomush.internal:8080`. Browsers grant the secure-context
exemption only to `localhost`/loopback, so the `Secure` session cookie is silently dropped: users
authenticate, get redirected, and are logged out again, **with no error anywhere** — exactly the
failure mode `configuration.md`'s caution describes. The local two-terminal case at `:232` is safe
(localhost is a secure context), so this is the systemd/LAN path specifically.

**Fix:** add a one-line cross-reference beside the systemd `ExecStart` (and in the Terminal 2
block) pointing at the `--secure-cookies` section, e.g.
`ExecStart=/usr/local/bin/holomush gateway   # add --secure-cookies=false if serving plain HTTP on a non-localhost host`,
plus a link to `configuration.md#--secure-cookies`.

### WR-06: Migration spec comment describes a step-back/step-forward the code does not perform

**File:** `internal/store/migrations_sessions_location_index_integration_test.go:152-154`

**Issue:** *"Step back one and forward one with the index already dropped and recreated, then
execute the up migration's statement a second time directly…"* — the spec does no stepping at
all. It goes straight from `migrator.Up()` to two raw `db.Exec` calls. The comment describes a
prior draft.

**Failure scenario:** a maintainer reads the comment, believes the round-trip is exercised twice
in this spec, and deletes the (genuinely necessary) round-trip coverage from the first spec as
redundant. Doc drift that misdirects a future edit, rather than a runtime defect.

**Fix:** delete or rewrite the comment to describe what the code does. Fixing WR-01 supersedes
most of it anyway.

---

## Info

### IN-01: Pre-existing broken assertion idioms in a file this phase touched

**File:** `internal/tls/subsystem_test.go:292-294`, `:300`, `:322-327`, `:343-345`, `:351`,
`:369-375`

These lines are **pre-existing** (unchanged by this branch; the file gained only the three new
tests at `:108`, `:137`, `:165`). Flagging because they are the phase's own subject matter in a
file it edited, and are cheap to fix while it is open:

1. `if os.Getenv("GOOS") == "windows" { t.Skip(...) }` (`:300`, `:351`) — `GOOS` is a build
   constant, not an environment variable. `os.Getenv("GOOS")` returns `""` unconditionally, so the
   skip **never fires**. Use `runtime.GOOS == "windows"`.
2. `assert.True(t, assert.Condition(t, func() bool { return assert.Contains(t, s, "a") || assert.Contains(t, s, "b") }), ...)`
   — `assert.Contains` *reports a failure on `t`* when it does not match, so in an `||` chain the
   first non-matching alternative already fails the test even when the second matches. The
   "either/or" intent is not what the code does; today it passes only because the first
   alternative happens to match. Replace with a plain
   `assert.True(t, strings.Contains(s, "a") || strings.Contains(s, "b"), "…, got: %v", err)`, or
   better, `errutil.AssertErrorCode` — which the three new tests in the same file already use
   correctly.

### IN-02: Dead branch in a table-driven test

**File:** `internal/tls/subsystem_test.go:380-424` (pre-existing) — every case sets
`expectError: true`, so the `else { assert.NoError(...) }` arm at `:419-421` is unreachable.
Either add a no-error case (e.g. all six files present and valid) or drop the field.

### IN-03: Incorrect package doc comment on a test file

**File:** `internal/tls/certs_test.go:4-5` — `// Package tls provides TLS certificate generation
and loading for HoloMUSH.` sits directly above `package tlscerts`. The package is named
`tlscerts`, and a package doc comment belongs on the non-test file. Delete it.

### IN-04: Removing two files from `.codecov.yml` `ignore` couples the project status to the E2E upload

**File:** `.codecov.yml:79-88` — `cmd/holomush/core.go` and `cmd/holomush/sub_grpc.go` are no
longer ignored. The rationale (the E2E lane now flushes counters, per the `stop_grace_period`
fix) is sound and the three Taskfile guards make a silent regression loud locally. But on any run
where the E2E coverage upload does not land (a lane that is skipped, a fork PR without secrets,
an infra failure that CI treats as non-blocking), those two files now report 0% and drag project
coverage well past the 1-point `threshold`. codecov `project` is not a required protect-main
check today, so this is a noise risk rather than a merge blocker — worth watching on the first
few post-merge runs.

### IN-05: `session_matrix_registry_test.go` claims an automated check that lives only in the plan

**File:** `test/meta/session_matrix_registry_test.go:56-60` — *"That absence is checked by
grepping this file for the annotation's literal form."* No test in the tree performs that grep;
it is a plan-level verification step. The file's avoidance of the literal is still correct and
worth keeping — just reword to *"verified by the introducing plan"* so a reader does not assume a
standing guard.

### IN-06: `covered-elsewhere` pointer resolution is a whole-file substring match

**File:** `test/meta/session_matrix_registry_test.go:469-474` — `container` and `name` are checked
with `require.Contains(t, body, …)` against the entire file. A citation whose text survives only
inside a comment resolves successfully. For the 32 `spec` rows this is backstopped by the marker
bijection; for the 2 `covered-elsewhere` rows it is the only check. Low impact at n=2; if it
grows, match against the `Describe(`/`It(` string literal specifically.

### IN-07: Redundant assertion pair

**File:** `cmd/holomush/core_test.go` (`TestApplyLogSinkFlagsRoutesEachSinkOverrideToItsOwnField`)
— `assert.NotEqual(t, "error", lc.Stderr.Level)` immediately follows
`assert.Equal(t, "debug", lc.Stderr.Level)`, which already pins the value; same for the OTel pair.
Harmless, but the "cross-wiring guard" framing suggests it adds coverage it does not.

---

_Reviewed: 2026-07-26_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
