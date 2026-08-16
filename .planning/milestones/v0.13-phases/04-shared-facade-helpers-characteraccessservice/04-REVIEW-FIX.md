---
phase: 04-shared-facade-helpers-characteraccessservice
fixed_at: 2026-08-12T00:35:00Z
review_path: .planning/phases/04-shared-facade-helpers-characteraccessservice/04-REVIEW.md
iteration: 3
findings_in_scope: 1
fixed: 1
skipped: 0
status: all_fixed
cumulative:
  iterations: 3
  findings_in_scope: 14
  fixed: 14
  skipped: 0
  fix_commits: 15
---

# Phase 4: Code Review Fix Report (iteration 3 — final)

**Fixed at:** 2026-08-12
**Source review:** `.planning/phases/04-shared-facade-helpers-characteraccessservice/04-REVIEW.md`
**Iteration:** 3 of a 3-pass cap — the loop is now closed

**This pass:**
- Findings in scope: 1 (1 warning — `fix_scope: all`)
- Fixed: 1
- Skipped: 0

**Cumulative across all three passes:**
- Findings in scope: 14 (2 critical-tier, 9 warning, 3 info)
- Fixed: 14
- Skipped: 0
- Fix commits: 15 (one finding, iteration-1 CR-01, needed a lint follow-up)

The iteration-3 review re-derived each of pass 2's five fixes **from the code
and from runnable probes**, not from the prior fix report, and found all five
holding with no regression. It surfaced exactly one outstanding defect, which
predated both fix passes. That defect is fixed here and the loop terminates
clean: nothing is deferred, nothing is skipped.

## Fixed this pass

### WR-01: the web proxy file documented four shipped RPCs as still answering `Unimplemented`

**Files modified:** `internal/web/character_handlers.go`
**Commit:** `59909c0a3`
**Class:** documentation defect (comment-only; no behavior change)

**What was wrong.** The block comment above the four owner-audience proxies
read:

> THEY ANSWER Unimplemented TODAY, AND THAT IS THE INTENDED STATE. Their facade
> handlers land in plans 04-05 and 04-06; until then
> UnimplementedCharacterAccessServiceServer answers Unimplemented and these
> proxies pass it through unchanged.

True when the file landed in `92e484214` (plan 04-04). Falsified by plans 04-05
and 04-06, which shipped all four handlers.

**Verified before writing the replacement**, so the corrected comment cannot
itself go stale against the gate one directory over:

| Proxy (`internal/web/character_handlers.go`) | Paired facade handler | Live at |
| --- | --- | --- |
| `WebListMyCharacters` | `CharacterAccessServer.ListMyCharacters` | `internal/grpc/characteraccess_owner.go:68` |
| `WebGetMyCharacter` | `CharacterAccessServer.GetMyCharacter` | `internal/grpc/characteraccess_owner.go:109` |
| `WebUpdateCharacterProfile` | `CharacterAccessServer.UpdateCharacterProfile` | `internal/grpc/characteraccess_write.go:257` |
| `WebUpdateCharacterDescription` | `CharacterAccessServer.UpdateCharacterDescription` | `internal/grpc/characteraccess_write.go:463` |

That set is exactly `characterWebProxyRPCs()` at
`test/meta/characteraccess_routing_census_test.go:272-279`, which
`TestCharacterAccessRoutingCensusWebProxies` compares by **set equality** — so
the stale comment was contradicting a checked-in gate.

**Why it was worth fixing rather than ignoring.** These proxies genuinely do
still return `CodeUnimplemented` — but from the `h.characterAccess == nil`
guard (`:70`, `:97`, `:127`, `:169`), i.e. an **unwired client in
`cmd/holomush`**, not an unbuilt facade. The comment therefore named a
plausible-but-wrong cause for a real wire outcome, sitting on the surface an
operator reaches first when a profile edit fails.

**Applied fix.** Replaced the stale paragraph with the two things that are true
at HEAD: that all four facade handlers are live (naming them, and naming the
census helper that pins the set), and that the only `Unimplemented` these
proxies still emit is their own nil-client guard, which means a gateway wiring
fault. The stale "they exist so the compile-time
`webv1connect.WebServiceHandler` assertion doesn't break the build" rationale
went with it — that was the placeholder reason; they now exist because they
proxy shipped RPCs. The first paragraph (shape claim: nil-client guard, header
token, bounded context, field-by-field forward, log-then-pass-through, computes
nothing) is still accurate and was kept verbatim.

**Explicitly NOT changed:** the `h.characterAccess == nil` guard's behavior. It
is correct, and the finding was never about it.

## Skipped

None — this pass, or any pass.

## Verification (this pass)

All gates run **inside the isolated review-fix worktree**
(`.worktrees/rf-04-20500-…`, branch `gsd-reviewfix/04-20500`), not in the main
checkout. The worktree was seeded with the prebuilt `bin/custom-gcl` (gitignored,
so absent from a fresh worktree) to let the real lint lane run rather than a
substitute.

| Gate | Result |
| --- | --- |
| `task fmt:check` | **exit 0** — no gofumpt/dprint/rumdl drift, so `task fmt` would mutate nothing |
| `task test -- ./internal/web/` | **exit 0** — 365 tests |
| `task test -- -run 'TestCharacterAccessRoutingCensus' ./test/meta/` | **exit 0** — 8 tests, the gate the comment now cites |
| `task lint` | **exit 0** (full lane, including `lint:invariants`) |

Judged by **exit code** in every case. `FAIL:`-shaped strings in `task lint`
stdout are `echo` bodies inside the guard scripts, not findings.

`git diff --stat` in the worktree showed exactly one file changed
(`internal/web/character_handlers.go`, +11/-6) before the commit, and
`git add` named that single path — no `git add -A`, nothing from `.gsd/`, the
`04-REVIEW*.iter*.md` backups, or the modified `04-REVIEW.md`.

The known pre-existing flake (`internal/command`
`TestRateLimiter_Allow/tokens_do_not_exceed_burst_capacity`, GitHub #4955) did
not appear; that package was outside the scoped runs.

**Worktree teardown note.** The repo's `git-merge-guard` hook permits
`--ff-only` merges from `origin/main` only, so the fix commit was carried onto
`v013-phase-03` by `git cherry-pick` instead of a fast-forward. The cherry-pick
parent was the branch tip, and the resulting tree hash is **identical** to the
fix branch's tree. The temp worktree, the temp branch, and the recovery sentinel
were all removed afterward; `git worktree list` is back to two entries and the
only uncommitted paths are the pre-existing ones this agent was told not to
touch.

## Cumulative record — all three passes

### Iteration 1 — 8 findings, 8 fixed, 9 commits

| Finding | Commit(s) | Subject |
| --- | --- | --- |
| CR-01 | `66f85d3a3`, `e4659c280` | map `codes.Aborted` at the gateway so concurrent-edit conflicts are not HTTP 500 (+ govet `shadow` lint follow-up) |
| WR-01 | `21cfe7653` | re-arm the non-vacuity control guarding INV-PRIVACY-9 |
| WR-02 | `1d23fc3d8` | close the owner profile read against the governed name set |
| WR-03 | `c219cff95` | stop reporting a post-COMMIT re-read failure as an ownership refusal |
| WR-04 | `d0c3531e1` | attribute the shared gate's logs to the calling surface, via `errutil` |
| WR-05 | `18762ae80` | log each visibility failure once, at the subsystem that produced it |
| WR-06 | `1321d85c1` | name only what the write changed in the `character_profile_update` envelope |
| WR-07 | `bc5316506` | do not bump the aggregate version for an all-no-op profile write |

### Iteration 2 — 5 findings, 5 fixed, 5 commits

| Finding | Commit | Subject |
| --- | --- | --- |
| WR-01 | `f49f8ed44` | make the errutil-vs-bare-slog guard actually discriminate |
| WR-02 | `56876f339` | omit an identical-value resubmit from `changed_attributes` |
| IN-01 | `d3573f8e3` | lift `newCorpusEngine` into one shared helper carrying the refusal |
| IN-02 | `736d15317` | stop a log-absence assertion colliding with the sentinel's own text |
| IN-03 | `9793f7c74` | document why the two profile-write no-ops answer a stale caller differently |

Iteration 2's WR-02 is notable as a **self-correction**: it fixed a residual the
iteration-1 fixer had declined, after re-checking and finding its own stated
blocker was wrong.

### Iteration 3 — 1 finding, 1 fixed, 1 commit

| Finding | Commit | Subject |
| --- | --- | --- |
| WR-01 | `59909c0a3` | correct the stale `Unimplemented` comment on the owner-audience web proxies |

### Shape of the loop

Findings per pass went **8 → 5 → 1**, and the single iteration-3 finding was a
documentation defect that predated both fix passes rather than anything either
pass introduced. The iteration-3 review re-ran the load-bearing empirical claims
(a standalone `slog.TextHandler` + `samber/oops@v1.22.0` probe for the errutil
guard; mockery `.Once()` expectations for the write path) instead of trusting
the fix reports, and cleared all of them. No fix was ever rolled back; no
finding was ever skipped.

### Follow-up from iteration 2 — now closed

Iteration 2 flagged that `.claude/rules/grpc-errors.md:38` taught the same false
oops/slog mechanism its WR-01 had corrected in a test comment, and would keep
re-seeding the error. That landed separately as `fe955fdb2`
(`docs(rules): correct the false oops/slog mechanism in grpc-errors`), the
commit immediately preceding this pass. Nothing remains open from it.

## Outstanding

Nothing. All 14 findings across the three passes are fixed and committed. No
deferrals, no skips, no known-broken state left behind.

---

_Fixed: 2026-08-12_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 3 (final pass of the capped auto-loop)_
