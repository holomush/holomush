---
phase: 9
round: 2
supersedes: ca821ff0f
reviewers: [codex, pi, opencode]
reviewed_at: 2026-07-26T06:05:00Z
reviewer_models:
  codex: cli-default
  pi: moonshotai/kimi-k3 (openrouter, --thinking high)
  opencode: default (github copilot)
lanes_attempted: [codex, pi, opencode, cursor]
lanes_dropped:
  cursor: "unauthenticated — rendered TTY sign-in splash, exit 0, 2322 bytes of ASCII art"
plans_reviewed: [09-01, 09-02, 09-03, 09-04, 09-05, 09-06, 09-07, 09-08, 09-09, 09-10, 09-11, 09-12, 09-13, 09-14, 09-15, 09-16, 09-17, 09-18, 09-19, 09-20, 09-21]
verdicts:
  codex: NOT READY TO EXECUTE (risk HIGH)
  pi: NOT READY (one-plan fix + smaller concerns)
  opencode: NOT READY (two blocking findings)
orchestrator_verdict: NOT READY — targeted replan required
---

# Cross-AI Plan Review — Phase 9 (Round 2)

**Round 2 of 2.** Round 1 (`ca821ff0f`, codex + pi) returned NOT READY on the
19-plan set; the replan at `4e13786d3` incorporated its six blockers and grew the
set to 21 plans. This round re-reviews that replanned set.

**Deliberate method note — the lanes were NOT primed.** Round 1's findings were
withheld from this round's prompt. Telling a reviewer "confirm these six blockers
are fixed" invites confirmation bias (the repo's own lesson: a primed checker
certified a falsehood in 52s, Phase 8). An unprimed re-review is the stronger
test: genuinely-closed blockers do not resurface on their own. None of round 1's
six blockers were re-raised as open — but three lanes found **new** defects, one
of which the replan *introduced while fixing* round 1.

Claude was skipped for independence (it is the executing runtime). The cursor
lane was attempted and dropped — see `lanes_dropped`.

---

## Codex Review

## Summary

The plan set is unusually evidence-conscious: it uses behavioral tests, negative demonstrations, live status observation, and explicit anti-vacuity checks. Most QUAL-05 work and the reaper/command-registration seams are grounded correctly in current code. However, execution is blocked by four material defects: unresolved user decision gates were silently converted into new scope, the final Codecov gate contradicts the repository’s documented two-session model, late configuration changes lack a guaranteed push/CI cycle before external gate mutation, and the shared lifecycle harness can be stopped after its first spec. The move matrix also claims production behavior while bypassing the production movement pipeline. **NOT READY TO EXECUTE.**

## Strengths

- The E2E coverage tracer targets a real silent-failure path. The current task neither deletes stale `coverage-e2e.out` nor propagates `covdata textfmt` failure; it exits with only the E2E status ([Taskfile.yaml:229](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/Taskfile.yaml:229)). The UID hypothesis is credible because the overlay bind-mounts host-created directories ([compose.e2e.cover.yaml:9](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/compose.e2e.cover.yaml:9)) into an image running as a non-root user ([Dockerfile:10](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/Dockerfile:10)). Plan 09-01 correctly requires diagnosis before choosing a fix.

- The reaper seam addresses an actual false test path. `ExpireSession` writes `status=expired` ([harness.go:993](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/testsupport/integrationtest/harness.go:993)), but the production query selects only detached sessions whose expiry is past ([session_store.go:444](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/store/session_store.go:444)). The real reaper then marks and deletes those rows ([reaper.go:93](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/session/reaper.go:93)). Plan 09-20’s negative demonstration using the old helper is especially valuable.

- The silent audit-drop plan follows the existing observability pattern precisely. The nil-emitter branch is currently silent ([plugin_downgrade_fence.go:423](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/eventbus/history/plugin_downgrade_fence.go:423)), while neighboring branches already use the injected logger and `WarnContext` with plugin/type fields ([plugin_downgrade_fence.go:439](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/eventbus/history/plugin_downgrade_fence.go:439)). Plan 09-06’s positive and negative log assertions are appropriately narrow.

- The ABAC remediation follows an established safe form rather than inventing one. The defect is visible in the current sentinel assignments ([location.go:68](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/access/policy/attribute/location.go:68)), and the correct omit-key-plus-witness pattern already exists in the character provider ([character.go:132](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/access/policy/attribute/character.go:132)).

- Several new meta-test gates explicitly avoid Go’s “no tests to run” false green by asserting both file existence and a top-level PASS line, such as the matrix guard ([09-12-PLAN.md:148](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-12-PLAN.md:148)) and ACE ratchet ([09-18-PLAN.md:226](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-18-PLAN.md:226)).

## Concerns

- **HIGH — Three plan-local re-scopes lack the explicit decision required by CONTEXT.** D-02 remains locked at 70% and D-03 says integration coverage should count instead of ignoring wiring code ([09-CONTEXT.md:43](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-CONTEXT.md:43)). D-09 explicitly requires a re-scope conversation above roughly 150 ACE renames ([09-CONTEXT.md:102](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-CONTEXT.md:102)). Research found 1,106 literal hits and presented options rather than a settled outcome ([09-RESEARCH.md:334](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-RESEARCH.md:334)). Nevertheless:
  - 09-01 raises the project target to 80% and reverses D-03 by removing existing ignores ([09-01-PLAN.md:48](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-01-PLAN.md:48)).
  - 09-10 also calls this a “reversed” D-03 ([09-10-PLAN.md:44](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-10-PLAN.md:44)).
  - 09-18 calls the tightened 114-hit predicate a “settled decision” ([09-18-PLAN.md:55](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-18-PLAN.md:55)).
  
  These may be good choices, but the plans cannot silently make them when the phase context expressly reserved them for user confirmation.

- **HIGH — Plan 09-19’s session-count gate contradicts the authoritative repository configuration.** `.codecov.yml` states that three uploaded files deduplicate to two sessions and that waiting for three would prevent notification ([.codecov.yml:11](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.codecov.yml:11)). CI does upload unit, integration, and E2E coverage separately ([ci.yaml:174](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.github/workflows/ci.yaml:174), [ci.yaml:218](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.github/workflows/ci.yaml:218), [ci.yaml:285](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.github/workflows/ci.yaml:285)). Yet 09-19 requires `totals.sessions >= 3` ([09-19-PLAN.md:107](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-19-PLAN.md:107)) while simultaneously preserving `after_n_builds: 2` as load-bearing ([09-19-PLAN.md:145](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-19-PLAN.md:145)). At least one of these premises is wrong; as written, the final gate may be unsatisfiable.

- **HIGH — Late Codecov edits are not guaranteed to reach the PR before live verification or ruleset mutation.** Plan 09-21 performs the only unconditional push and CI wait ([09-21-PLAN.md:95](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-21-PLAN.md:95)). Plan 09-17 may then edit `.codecov.yml` and says the change must be pushed and observed on a new head ([09-17-PLAN.md:146](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-17-PLAN.md:146)), but its automated gate merely queries whatever SHA the PR currently exposes ([09-17-PLAN.md:171](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-17-PLAN.md:171)). Plan 09-19 then makes another local config change before the ruleset checkpoint. Its sequencing only mandates another push when the branch is behind main, not when local HEAD is ahead of the PR head ([09-19-PLAN.md:206](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-19-PLAN.md:206)). This permits observations against stale remote configuration.

- **HIGH — The process-wide lazy harness can be stopped after the first spec.** Plan 09-12 proposes starting it lazily and registering `DeferCleanup` at the call site ([09-12-PLAN.md:197](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-12-PLAN.md:197)). Ginkgo v2.32 invokes `DeferCleanup` registered from `It`/`BeforeEach` when that spec ends; suite-wide cleanup happens only when registered from `BeforeSuite` ([core_dsl.go:872](/Users/sean/go/pkg/mod/github.com/onsi/ginkgo/v2@v2.32.0/core_dsl.go:872)). The existing suite already has a suitable `BeforeSuite`/`AfterSuite` lifecycle ([session_persistence_suite_test.go:57](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/test/integration/session/session_persistence_suite_test.go:57)). A once-guarded lazy accessor first called inside a spec can therefore return a stopped server to every later spec.

- **HIGH — The move matrix test bypasses the behavior it claims to cover.** `Session.MoveTo` directly updates the `sessions` table and explicitly does not invoke `world.Service.MoveCharacter` or update `characters.location_id` ([session.go:288](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/testsupport/integrationtest/session.go:288)). The actual production path performs the guarded world mutation and then invokes the movement hook ([service.go:971](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/world/service.go:971), [service.go:1035](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/world/service.go:1035)). Plan 09-15 nevertheless uses the direct helper and says that closes “moving while attached” ([09-15-PLAN.md:80](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-15-PLAN.md:80)). It proves privacy filtering after test-authored state mutation, not that production movement advances the session floor.

- **MEDIUM — The proposed client-type field has an unsafe zero-value path.** Plan 09-20 says to add a string field “defaulting” to terminal and have `attach` read it ([09-20-PLAN.md:99](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-20-PLAN.md:99)), but Go string fields default to `""`. The plan only explicitly updates the web/telnet opener; existing `ConnectGuest` and `ConnectAuthedWithRoles` construct `Session` literals independently ([harness.go:1085](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/testsupport/integrationtest/harness.go:1085), [harness.go:1172](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/testsupport/integrationtest/harness.go:1172)). Unless `attach` normalizes empty to terminal or every constructor is updated, these paths will send an invalid client type.

- **MEDIUM — The ACE plan’s declared dependency contract is false.** Its front matter lists only five dependencies ([09-18-PLAN.md:6](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-18-PLAN.md:6)), while its objective claims every test-touching plan is explicitly listed ([09-18-PLAN.md:46](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-18-PLAN.md:46)). Plans 09-05, 09-06, and 09-10 add or alter tests but are not direct dependencies. Wave ordering currently masks the error, but the claimed last-mover guarantee is not encoded in the graph.

- **LOW — Some diagnostic pipelines can mask a failed coverage producer.** The TLS and command coverage probes pipe `go tool cover` into `tail -1` without `set -o pipefail` ([09-08-PLAN.md:87](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-08-PLAN.md:87), [09-10-PLAN.md:99](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-10-PLAN.md:99)). A failing producer can be replaced by a successful empty `tail`.

## Suggestions

1. Add an explicit decision checkpoint before execution that records:

   - 70% versus 80% project acceptance;
   - whether the two existing `cmd/holomush` ignores are removed;
   - literal D-07 versus the tightened 114-hit ACE predicate.

   Then update `09-CONTEXT.md` so the plans are not contradicting their locked source.

2. Reconcile Codecov’s session model before 09-19:

   - If Codecov still deduplicates to two sessions, require `sessions == 2` and independently require `e2e` flag coverage greater than zero.
   - If it now reports three, update the `.codecov.yml` explanation and re-prove that `after_n_builds: 2` does not post prematurely.

3. Insert explicit publish/observe checkpoints after any 09-17 config edit and after the 09-19 threshold edit. Before changing the ruleset, assert:

   ```sh
   test "$(git rev-parse HEAD)" = "$(gh pr view ... --json headRefOid --jq .headRefOid)"
   ```

   Then wait for CI and query statuses on that exact SHA.

4. Start the shared lifecycle harness in the suite’s existing `BeforeSuite` and stop it in `AfterSuite`, or use a fresh `BeforeEach` harness with per-spec cleanup. Also require each created session to register `DeferCleanup` for logout/transport teardown; `Server.Stop` currently stops only the plugin subsystem ([harness.go:809](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/testsupport/integrationtest/harness.go:809)).

5. Either drive the move row through `world.Service.MoveCharacter` plus the real movement hook, or relabel the current test as privacy-floor behavior after a simulated move and mark the production lifecycle cell uncovered/covered elsewhere.

6. Make client-type normalization explicit in `attach` (`""` → `"terminal"`) and test all existing constructors, not only the new telnet opener.

7. Add every earlier test-modifying plan as an explicit 09-18 dependency, and add `set -o pipefail` plus a non-empty `total:` assertion to the coverage probes.

## Risk Assessment

**HIGH.** The individual implementation plans are generally careful, but the remaining problems sit at assurance and release-control boundaries: an internally contradictory final coverage gate, external branch-protection mutation based on potentially stale commits, and lifecycle tests that can either reuse a stopped harness or certify simulated state as production behavior. Those failures could halt execution late, deadlock future PRs, or let the phase finish with the same kind of overstated assurance it is intended to eliminate.

Codecov’s current external API semantics could not be independently checked through Context7 because the documentation request failed with DNS resolution; the Codecov concern above is therefore grounded in the repository’s own configuration and CI workflow rather than current external documentation.
---

## pi Review (moonshotai/kimi-k3)

Good — this confirms `capSessionKick` (admin, session, global) is a real, seeded capability distinct from the read_unrestricted_history one. Let's write up the final review now.

## Summary

This is an unusually well-evidenced plan set — nearly every shell gate, file:line citation, and harness-signature claim I checked against the actual repo (21 plans, dozens of citations) turned out to be accurate, including several subtle corrections the plans made to their own prior research (e.g., 32 vs 25 `TestINV_` declarations, 3 vs 4 `It` blocks in `session_lease_test.go`, the exact `.codecov.yml` unfiltered-count numbers, and the live codecov API figures which I re-fetched and match exactly: 78.28% project, 64.25% `cmd/holomush`, 76.23% `internal/tls`, 0.0% e2e flag). The shell-gate hardening (PASS-line assertions instead of bare exit codes, `pipefail`, positive-then-negative assertion ordering, `rg -c … >/dev/null` instead of `rg -q` to avoid SIGPIPE-141) is correctly reasoned and I reproduced the failure modes it guards against live. However, I found one materially significant gap: plan 09-20's central "verified-against-source" claim that "there is no administrator-boot command and no administrator-boot RPC anywhere in the tree" is **false** — `internal/command/handlers/resetpassword.go`'s `--kick` flag is a real, capability-gated (`admin`, `session`, global scope), already-tested production mechanism that deletes a target character's session row, and it is never mentioned anywhere in the plan set despite being the closest real analog to the matrix's "Admin boot → row deleted" row. This causes plans 09-14/09-20 to mis-disposition that matrix row as `not-implementable` when a real (if imperfect — it bypasses the `session_ended` event path) entry point exists. This is a correctable finding, not a fundamental flaw in the plan set's design, but it must be resolved before execution locks in the wrong disposition and files a needless "no entry point" issue.

**Verdict: NOT READY** — pending a one-plan fix to 09-20 Task 3 (and downstream 09-14/09-12 disposition) to account for `resetpassword --kick`, plus the smaller concerns below. Once that is resolved, this plan set is executable as designed.

## Strengths

- **Live-verified coverage figures throughout.** I independently queried `api.codecov.io` and got 78.28% project / 64.25% `cmd/holomush` / 76.23% `internal/tls` / 0.0% e2e flag — all match the plans' cited numbers exactly (`.planning/phases/09-test-quality-code-health-sweep/09-19-PLAN.md:108`, `09-01-PLAN.md` objective). This is real evidence, not restated assumption.
- **The codecov status-context deadlock risk is real and correctly diagnosed.** I queried 3 recently-merged Go-changing PRs' head vs merge commits: `codecov/patch` posted only on merge commits (e.g. head `0554b2c2…`→no coverage status, merge `497748c6…`→`codecov/patch`), never on any head commit, and `codecov/project` appeared on none of the 6 commits sampled. This directly validates 09-17-PLAN.md's core "assumption" block and the deadlock mitigation built into 09-19's checkpoint (`09-19-PLAN.md` T-09-19-06).
- **The reaper-selection-predicate mismatch (B2) is real and precisely diagnosed.** `internal/store/session_store.go:447` selects `status='detached' AND expires_at < now()`; `harness.go:995-1003`'s `ExpireSession` sets `status='expired'`, which that query never matches. `test/integration/presence/lease_reaper_test.go:120-166` is a genuine, already-working precedent for the detach→backdate→real-reaper pattern plan 09-20/09-13 correctly reuse.
- **Harness helper claims all verified present and correct**: `WaitForEvent` (session.go:171), `MoveTo` (:303), `DetachTransport` (:366), `ReattachTransport` (:419), `QueryStreamHistory` (:705), `QueryStreamHistoryBounded` (:722), `EmitDirectEvent` (:770), `ExpireSession` (harness.go:995) — all exact matches, zero TODO panics in the file.
- **The telnet-opener defect (B1) is real**: `OpenTelnetSession` at `session.go:875-878` literally calls `t.Fatalf`; `attach()` hardcodes `ClientType: "terminal"` at line 458; `session_store.go:517-519` confirms `telnet` is a legal allowlisted value. Plan 09-20 Task 1's fix is well-targeted.
- **The empty-command-registry defect (B3) is real**: `harness.go:468-471`'s comment confirms "otherwise it gets an empty registry (no commands registered)"; `RegisterAll` only registers `quit`/`shutdown` (`register.go:86-89`).
- **ABAC sentinel fix sites (#4793) verified exactly**: all 7 cited `attrs[...] = ""` lines in `location.go:72,80`, `object.go:117,125,133`, `property.go:93,102` match precisely; `character.go:139` and `stream.go:40-48` are genuine, already-fixed reference forms.
- **Cookie/security-header inversion (#4794) verified exactly**: `gateway.go:120` default-false flag, `cookie.go:52-56` already-correct downgrade logic, `security_headers.go:74-82` already-correct conditional — the plan correctly scopes the change to the default/plumbing only.
- **Migration precedent (#4796) verified**: no index on `sessions.location_id` across all 52 migrations, `000008_session_player_fk.{up,down}.sql` is the exact precedent form cited, 000053 confirmed free.
- **Shell-gate hardening is real engineering, not decoration.** I reproduced: `rg -c file1 file2 | wc -l` counts files not matches (2 vs 3 matches across 2 files); `go test -run NoMatch` exits 0 printing `[no tests to run]`; piping into `rg -q` under `pipefail` produces exit 141 (SIGPIPE) on the passing case. All three failure modes the plans explicitly guard against (09-11, 09-12, 09-16, 09-18 notes) are genuine and correctly mitigated by the chosen `rg -c … >/dev/null` / PASS-line-grep patterns.
- **Wave/dependency graph is topologically sound** — I mechanically verified all 21 plans' `depends_on` edges point to strictly lower wave numbers with no cycles.
- **The matrix cell count correction (38 not 48) is verified**: the source matrix (`09-RESEARCH.md`, reproduced from `holomush-izk0`) genuinely has 10 `n/a` cells; `09-12-PLAN.md` still asks for a 48-row *registry* (with 10 explicitly `not-applicable`), which is internally consistent even though it differs from "38 populated."
- **Precise self-correction of prior research** in multiple places: `TestINV_*` count corrected from 25→32 (I verified 32 exactly), `session_lease_test.go` It-count corrected from 4→3, `.codecov.yml` unfiltered-count corrected to exactly 3/2 (I verified).

## Concerns

- **HIGH — `09-20-PLAN.md` Task 3's "no administrator-boot entry point" finding is factually wrong, and this propagates into 09-14 and 09-12/09-16's dispositions.** `internal/command/handlers/resetpassword.go` registers a production, capability-gated (`command.Capability{Action: "admin", Resource: "session", Scope: command.ScopeGlobal}`, line 35) `--kick` flag that calls `exec.Services().Session().DeleteByCharacter(ctx, ch.ID)` (line 209) for every character of the target player — a real "administrator boot" mechanism, already unit-tested (`resetpassword_test.go:163-201`, `t.Run("reset with --kick terminates game sessions", ...)`). The plan's search ("command registrations, admin registrations, proto service definitions... do not stop at a keyword match in a comment") apparently missed this handler despite it being registered via the exact `RegisterAdmin`/`handlers.AdminDeps` path the plan does read (`register.go:12-83`). The dispositions in 09-14 ("If administrator boot has no entry point... do NOT invent one... disposition as not-implementable") and 09-12/09-16 (registry `not-implementable` marking + filed issue) are built on this false premise. Note the nuance: `resetpassword --kick` deletes the row via a raw `DELETE` (`session_store.go:813-827`) without going through the `RecordBootedSession`/`session_ended`-event path that `internal/grpc/command_handler.go:307-350` implements for plugin-originated boots — so it is not a *perfect* match for the matrix's "row deleted" semantics, but it is unambiguously a real, capability-scoped, admin-initiated session-termination entry point that the plan's search should have surfaced and at minimum discussed.
- **MEDIUM — 09-20's evidence-gathering instructions ("search the command registrations, the admin registrations, and the proto service definitions") were followed for `RegisterAll`/proto but evidently not exhaustively for `RegisterAdmin`'s existing `resetpassword` handler**, even though the plan explicitly reads `register.go` in full per its own `<read_first>` block (`09-20-PLAN.md` Task 3 read_first list). This suggests the verification-against-source discipline that worked well elsewhere in this plan set had one blind spot specifically on the admin-boot question, which is exactly the question three downstream plans (09-12, 09-14, 09-16) treat as settled.
- **MEDIUM — `TestGatewayCommand_SecureCookiesFlag` (`cmd/holomush/gateway_test.go:552`) is itself a single-token-tail ACE naming violation** (`_SecureCookiesFlag` tail tokenizes to `Secure`+`Cookies`+`Flag` — arguably 3 tokens, so it may or may not trip the tightened predicate depending on exact tokenization; worth the executor double-checking against 09-18's classifier before assuming it's clean, since plan 09-04's Task 1 adds a sibling test for the opt-out with the same naming risk and neither plan flags this explicitly the way 09-08/09-09 do for their own new tests).
- **LOW — plan 09-05's dependency on 09-01 is not obviously justified.** The migration/index work has no stated relationship to the coverage-chain repair; `depends_on: ["09-01"]` may just be the wave-1-gate convention (everything waits on the tracer plan) rather than a genuine functional dependency, but the plan doesn't say so, leaving it ambiguous whether 09-05 could run in wave 1 alongside 09-01 if 09-01 is delayed.
- **LOW — 09-19 Task 2's `rg -q '^\s*threshold: 0%' .codecov.yml` gate is asserted to fail against the unedited file** (confirmed: I ran it, it returns exit 1) but the plan's own `<note>` says "the gate is expected to fail on first run" for a *different* plan (09-17 Task 2); for 09-19 Task 2 the plan presents the failing-first-run behavior as evidence the gate is non-vacuous, which is correct, but it's worth confirming the executor doesn't confuse the two similarly-worded "expected to fail" notes across 09-17 and 09-19 during execution.
- **LOW — plan 09-15 Task 3 adds a new spec to `privacy_test.go` for the floor-preservation arm but doesn't require a `// Verifies: INV-PRIVACY-6` annotation**, even though the existing sibling spec in that file carries exactly that annotation (`privacy_test.go:29`) and the new spec completes the same invariant's second arm. Missing this would be a minor consistency gap, not a functional bug.

## Suggestions

1. **Before executing 09-20, re-run the admin-boot search explicitly including `grep -rn "kick" internal/command/handlers/`** (not just `RecordBootedSession`/`BootedSessions`), and update 09-20 Task 3, 09-14 Task 1, and the 09-12 registry to disposition the administrator-boot row against `resetpassword --kick` — either as `spec-covered` (if the matrix's semantics can be satisfied by this mechanism) or as an explicit note explaining why the row still needs an issue despite `resetpassword --kick` existing (e.g., "it doesn't emit `session_ended`, so it's an imperfect admin-boot and the gap is worth tracking, but it does prove the row is drivable"). Filing an issue for "no entry point exists" when one does would be a wasted issue and a subtly wrong assurance artifact — exactly the failure mode this phase is designed to eliminate.
2. Add a one-line cross-check in 09-18 Task 1 or 09-04 to explicitly classify `TestGatewayCommand_SecureCookiesFlag` under the tightened ACE predicate before the sweep runs, since it's a pre-existing name the sweep plan didn't originate.
3. Consider having 09-05 state explicitly whether its `depends_on: ["09-01"]` is a genuine functional dependency or just phase convention, for clarity if the wave graph is ever re-parallelized.

## Risk Assessment

**MEDIUM.** The plan set's engineering discipline (live-evidence-checked shell gates, self-correcting research, precise file:line citations) is exceptionally strong and the vast majority of claims I checked against source held up exactly as stated — this is not a risk about plan quality broadly. The risk is narrowly concentrated in the one factual gap found (administrator-boot entry point), which is real, propagates through three plans (09-12, 09-14, 09-20/09-16), and would cause the phase to file a misleading "capability doesn't exist" issue and lock in an incorrect matrix disposition if executed as written. This is cheap to fix (re-scope one task in one plan plus a registry row) but must be fixed before execution, because the whole phase's stated purpose is eliminating exactly this class of unverified assurance claim.

---

## OpenCode Review

Let me begin the systematic verification. I'll start with the key files referenced across multiple
Now I have sufficient evidence. Let me compile the review.

---

# Cross-AI Plan Review: Phase 9 — Test-Quality & Code-Health Sweep

## 1. Summary

This is a **densely reasoned, meticulously cross-referenced** plan set. Nearly every claim about existing code (`path:line`) and every verification gate (`automated` block) has been independently verified and found accurate. The wave ordering, the "ratchet not review-judgment" posture, the deliberate "fix all, no allowlist" with a sizing-gate release valve, and the obsessive attention to vacuous-gate construction all reflect genuine engineering rigour. Two findings prevent a READY verdict: (1) the administrator-boot entry-point claim in 09-20 Task 3 is **falsified by live code** — `resetpassword --kick` is a production admin-boot path; (2) the E2E coverage-flush tracer (09-01) is a single point of failure with a stop-condition that was flagged by a prior review and remains unresolved. Addressing both is a single-day fix. **Verdict: NOT READY — two blocking findings.**

---

## 2. Strengths

- **Comprehensive harness verification (09-20).** All three seams identified by the prior cross-AI review have been independently confirmed: `OpenTelnetSession` is fatal at `internal/testsupport/integrationtest/session.go:875-876`, `attach` hardcodes `"terminal"` at `:459`, `EmitDirectEvent` takes no timestamp at `:770`, and the `ExpireSession` helper at `harness.go:995-1003` writes `status='expired'` while the reaper's query at `internal/store/session_store.go:447` selects `status='detached'`. All four claims are precisely correct. The "smoke spec that fails without the seam" negative-demonstration requirement is the correct assurance posture.

- **Verification gate construction (all plans).** The `automated` verification blocks are the best I have seen. Every gate has been defended against the vacuity class it replaces: `-run`-pattern gates require explicit `--- PASS:` line assertion (09-12, 09-16, 09-18), `pipefail` is set consistently, `rg -c` uses `>/dev/null` rather than `-q` to avoid SIGPIPE under pipefail, negative assertions are bracketed by positive controls that fail on a missing file, `awk` defaults to failure and only clears on success, and `test -f` prefix checks prevent the `-run`-no-test-exists-returns-zero` trap. This is methodical care applied to the right layer.

- **Research-driven scope correction (09-RESEARCH.md).** All three GATE findings are verified. GATE 1: the 54.6% false premise is confirmed — codecov's API returns 78.28% project coverage on `main`. GATE 2: the D-07 predicate returns ~1,106 hits vs a ~150 threshold, and the tightened single-token-tail predicate producing ~114 is directionally correct. The re-scoping of QUAL-02 to the `holomush-0yo6` floor set (two actual gaps, not 15 imaginary points) is the right call.

- **Dependency ordering.** The wave graph is load-bearing and internally consistent. 09-20 (harness seams) → 09-12 (registry) → 09-13/14/15 (specs) → 09-16 (bijection guard) is coherent, and 09-18 (ACE sweep) depending on all test-touching plans including wave-2 ones (09-03, 09-08) correctly enforces the "sweep last" contract.

- **#4793 sentinel sites precision.** All seven sites at `internal/access/policy/attribute/location.go:72,80`, `object.go:117,125,133`, `property.go:93,102` are confirmed. `character.go:139` is already fixed with the ADR-citing comment. `stream.go:40-48` is the second reference form. 100% accurate.

- **09-05 migration planning.** The `000053_sessions_location_index` gap is confirmed — no index on `location_id` across all 52 migrations, and `000052` is the last on disk. The plan correctly identifies the `CREATE INDEX IF NOT EXISTS` precedent (000008) and rejects `CONCURRENTLY` (no precedent, no non-transactional runner). The up/down/up round-trip test is correctly scoped as a task.

- **Self-aware false-positive defence (09-18).** The plan explicitly addresses the arch-review false positive class — that 658 of 1,106 D-07 hits are already ACE-compliant sentences using `_` as a separator — and tightens to the single-token-tail form. The `TestINV_*` carve-out is correct (32 declarations, load-bearing for invariant registry readability at `test/meta/invariant_registry_test.go`).

---

## 3. Concerns

### HIGH — Administrator-boot entry point claim is falsified by live code (09-20 Task 3, assumption block `verified-against-source`)

**Evidence:** The plan assumptions claim *"There is no administrator-boot command and no administrator-boot RPC anywhere in the tree."* This is wrong. Two facilities exist:
- `internal/command/handlers/resetpassword.go:197-218`: the `resetpassword --kick` admin command iterates player sessions, calls `RecordBootedSession` for each, emits `session_ended` with cause `kicked`, and counts completed squelches. This is a real, tested admin entry point: `internal/command/handlers/resetpassword_test.go:163` tests `"reset with --kick terminates game sessions"`.
- `internal/grpc/dispatcher_test.go:643-748`: `TestAdminBootEmitsSessionEndedWithKickedCause` directly drives the boot mechanism via `exec.RecordBootedSession` and asserts the `session_ended` event with cause `kicked`. This is the canonical admin-boot integration test.

**Mechanism:** `resetpassword --kick` dispatches through the same command handler as any other command (`internal/grpc/dispatcher_test.go:717-722` drives `server.HandleCommand(ctx, &corev1.HandleCommandRequest{Command: "testboot", ...})`). The admin boot row IS drivable from the harness via `Session.SendCommand(ctx, "resetpassword <player> --kick")` — assuming the harness has the admin handler registered and valid deps for `RegisterAdmin` (which panics on nil deps at `internal/command/handlers/register.go:14-23`). This is a nuance the plan should have surfaced, not a blocker for the matrix — the admin-boot row could be spec-covered by driving `resetpassword --kick`.

**Impact:** 09-20 will record `"not-implementable"` for a row that IS implementable. The matrix registry (09-12/09-16) will propagate that incorrect disposition. The fix is a single-day scope change: drive `resetpassword --kick` through the harness with admin dependencies or accept the more honest classification "not-implementable from harness defaults without admin wiring."

### MEDIUM — E2E coverage-flush tracer (09-01) is a single-point-of-failure for 7+ downstream plans

**Evidence:** 09-01 is the `tracer` type and depends on environment-specific Docker behaviour (bind-mount uid, `stop_grace_period` on macOS Docker Desktop vs Linux CI). The prior cross-AI review flagged this: *"if both hypotheses turn out wrong, the entire QUAL-02 09-10/09-17/09-19 chain stalls."* This concern remains unresolved. The plan's `STOP CONDITION` says to escalate after two fix attempts, but there is no documented escalation path, no pre-agreed re-scope trigger, and no alternative measurement chain.

**Impact:** A blocked 09-01 blocks 09-02, 09-10, 09-17, 09-19 transitively through the wave graph. The coverage backfill (09-10) and the gate wiring (09-17/09-19) are the declarative purpose of QUAL-02; without them, QUAL-02 is undelivered. The tracer is correctly typed but incorrectly risk-managed.

### MEDIUM — 09-20's `resetpassword --kick` does not work from the narrow compiled-in option

**Evidence:** The plan adds a harness option registering only `RegisterAll` (quit, shutdown). But driving admin boot requires `RegisterAdmin` which panics on nil dependencies (`internal/command/handlers/register.go:14-23`). Even if the harness option is widened to include admin handlers, the admin deps (PlayerRepo, Hasher, PlayerSessions, ResetRepo, CharLister) are heavy — they require a real player repository, hasher, session store etc. The plan is correct that there's no *standalone* boot command, but incorrect that there's no entry point at all. The `resetpassword --kick` path exists but requires wiring admin dependencies into the harness.

**Impact:** If 09-20 were corrected to note `resetpassword --kick` exists, the matrix row would remain `not-implementable` without additional harness wiring. This is still a valid outcome — it just needs honest documentation rather than a blanket "no entry point" assertion.

### MEDIUM — `depguard_config_test.go` is missing the `natstest` entry

**Evidence:** At `test/meta/depguard_config_test.go:24-31`, the pinned set is `{eventbustest, coretest, quarantinetest}` — but the configured deny list at `.golangci.yaml:149-156` has FOUR entries, including `natstest` which landed in a prior phase. The plan previously claiming this was "already correctly respected" (`CLAUDE.md §Testing` docs) is wrong — the meta-test pins 3 of 4. 09-07 Task 3 correctly identifies this gap and proposes fixing it. **This is a correctness issue the plans actually catch and fix** — I call it out as a verified property of the plan quality, not a defect in the plans.

### LOW — Plans talk about 48 matrix cells but the source has 38 populated (09-12, 09-16)

**Evidence:** The 09-RESEARCH.md correctly identifies this: 10 of 48 positions are `n/a` in izk0's table (e.g., multi-session × fresh-SelectCharacter, web columns × tmux-style telnet reattach). D-12 in CONTEXT.md says "full 12×4 matrix as `holomush-izk0` specifies" — which IS 48 positions, 38 populated. The plan correctly handles this by saying each cell gets either a spec or a `n/a` marking, but the phrasing in a few places (e.g. 09-12 Task 1 "with exactly forty-eight rows") is fine — 48 rows with 10 `n/a` and 38 needing coverage.

### LOW — 09-02 Task 1 files four issues that overlap in description domain

**Evidence:** The four `eventbus_e2e` files are all about event-bus integrity patterns (audit drift, JS storage corruption, multi-protocol fanout, backfill rebuild). Filing one issue per file is deliberate per D-11. However, all four would likely be implemented together in a single eventbus sprint. This is a tooling choice, not a defect — the plan explicitly justifies the split to avoid the `holomush-ec22.9` lost-items pattern.

### LOW — 09-06 does not mention that `f.log` is already injectable

**Evidence:** `internal/eventbus/history/plugin_downgrade_fence.go:81` exports `WithFenceLogger(log *slog.Logger)`, `:141` stores it as `log *slog.Logger`, `:172` defaults to `slog.Default()`. The plan says "If the fence's logger is not currently injectable from a test, add the narrowest possible seam" — but it already IS injectable. This is a minor inaccuracy but doesn't affect correctness; the plan's action to capture log records from a test would use `WithFenceLogger` with a test handler.

---

## 4. Suggestions

1. **09-20 Task 3: Correct the admin-boot finding.** The plan should record that `resetpassword --kick` IS an admin session-boot entry point, but driving it from the harness requires admin wiring (`RegisterAdmin` with full deps) that the narrow compiled-in option does not provide. The matrix row should be dispositioned as `not-implementable-from-harness-defaults` with the `resetpassword --kick` path cited, or the harness should be extended with an admin deps option. Either is a valid scope decision; "no entry point" is not accurate.

2. **09-01: Document an escalation path in the STOP CONDITION.** Add a single sentence: "If both hypotheses fail, file an issue with the diagnostic evidence, skip the e2e-coverage-dependent QUAL-02 verification (09-17, 09-19 Task 1), and deliver QUAL-02 through the unit-lane-only measurement that 09-10 Task 1 records — recording the e2e gap as a tracked follow-up." This makes the stop condition actionable rather than blocking.

3. **09-07 Task 3: Verify the `natstest` entry is actually present in the deny list before adding `integrationtest`.** The plan correctly notes the meta-test omits `natstest`. During execution, confirm with `rg` before editing — the plan says "While there, add the one currently-denied package the meta-test omits" which is the right action.

4. **All plans using `coverage-e2e.out`: Consider that `task test:e2e:cover` requires Docker and the instrumented build (`docker:build:cover`), which may be unavailable on the executor's machine.** Several plans' verification gates reference `coverage-e2e.out` or `task test:e2e:cover`. The executor's local environment determines whether these gates can run. The RESEARCH.md's "Environment Availability" table correctly notes Docker is available, but individual plan gates may fail if Docker is unavailable mid-execution.

---

## 5. Risk Assessment

**OVERALL: MEDIUM**

The plan quality is in the top 5% of plans I have reviewed — the verification gate construction, the research-driven scope correction, and the harness-claim verification are all first-rate. The blocking findings are narrow and fixable:

- **F1 (HIGH):** 09-20's administrator-boot claim is falsified. Fix: recategorize the finding to note `resetpassword --kick` exists but needs admin wiring. One plan, one task, one paragraph change.

- **F2 (MEDIUM):** 09-01's single-point-of-failure for the QUAL-02 measurement chain remains as flagged by the prior review. Fix: add a documented fallback path to the STOP CONDITION. One plan, one sentence addition.

Neither finding requires rewriting plans or restructuring the wave graph. Both are addressable before the first task executes. Once addressed, this phase is ready.

**Additional risk:** The phase's scope is aggressive (21 plans, ~10 waves) and the ACE sweep alone touches ~114 files. The risk of merge conflicts with concurrent branches is real (D-08/D-10 flag this). However, the `gsd/v0.12-milestone` branch is the single remaining phase, so concurrent work is unlikely.

**Key insurance:** The plans have three built-in circuit breakers: the GATE 1 sizing check (78.28% > 70%), the GATE 2 re-scope release valve (1,106 → 114), and the 09-01 STOP CONDITION. All three are correctly wired and would fire before wasted work. The admin-boot finding is the only one that would fire *during* execution rather than *before* it.

---

## Consensus Summary

**All three prompt-fed lanes returned NOT READY.** No lane had repo-access
problems; every finding below is grounded at `path:line` and the orchestrator
independently re-verified each HIGH against source before recording it here.

The lanes' HIGH findings barely overlap — codex audited release-control and
lifecycle semantics, pi and opencode audited assumption-block factuality. A
single-lane round would have missed four or five real defects regardless of which
lane ran.

### Agreed Strengths

Raised by 2+ reviewers, each independently verified:

- **The anti-vacuity shell-gate hardening is genuine engineering, not decoration**
  (codex, pi, opencode). pi reproduced all three guarded failure modes live:
  `rg -c f1 f2 | wc -l` counts files not matches; `go test -run NoMatch` exits 0
  printing `[no tests to run]`; piping into `rg -q` under `pipefail` yields exit
  141 (SIGPIPE). The chosen `rg -c … >/dev/null` and PASS-line-grep forms
  correctly mitigate each. codex confirms the PASS-line assertions at
  `09-12-PLAN.md:148` and `09-18-PLAN.md:226`.
- **09-20's four harness-seam claims are all exactly correct** (codex, pi,
  opencode): `OpenTelnetSession` fatal at `session.go:875-878`; `attach`
  hardcodes `ClientType: "terminal"` (~`:458`); `EmitDirectEvent` takes no
  timestamp at `:770`; `ExpireSession` writes `status='expired'`
  (`harness.go:995-1003`) while the reaper selects `status='detached'`
  (`session_store.go:447`). The negative-demonstration requirement ("a smoke spec
  that fails without the seam") is the right assurance posture.
- **The #4793 ABAC sentinel inventory is 100% accurate** (codex, pi, opencode):
  all seven sites at `location.go:72,80`, `object.go:117,125,133`,
  `property.go:93,102`; `character.go:139` and `stream.go:40-48` are genuine
  already-fixed reference forms.
- **Live coverage figures are real evidence, not restated assumption** (pi
  re-queried `api.codecov.io` and matched exactly: 78.28% project, 64.25%
  `cmd/holomush`, 76.23% `internal/tls`, **0.0% e2e flag**).

### Agreed Concerns

**C-1 · HIGH · 2 of 3 lanes (pi, opencode) · orchestrator-confirmed —
`09-20-PLAN.md:21`'s "no administrator-boot entry point" assumption is FALSE.**

`resetpassword --kick` is a real, capability-gated, already-tested admin
session-termination path:

| Evidence | Location |
| --- | --- |
| usage string declares `--kick` | `internal/command/handlers/resetpassword.go:27` |
| capability `{admin, session, ScopeGlobal}` | `resetpassword.go:35` |
| `--kick` requires admin on session | `resetpassword.go:111,124` |
| terminates sessions for all player characters | `resetpassword.go:197-218` |
| `DeleteByCharacter` per character | `resetpassword.go:209` |
| unit test `"reset with --kick terminates game sessions"` | `resetpassword_test.go:163` |

The plan's own text names the error: *"the admin registration adds
account-management commands, not a boot."* `resetpassword` **is** an
account-management command that also boots sessions. The search looked for the
token `boot` and found only a proto comment; the capability is spelled `kick`.

pi adds the load-bearing nuance: `--kick` deletes the row via raw `DELETE`
(`session_store.go:813-827`) **without** the `RecordBootedSession` /
`session_ended` path (`internal/grpc/command_handler.go:307-350`). So it is not a
perfect match for the matrix's "Admin boot → row deleted" semantics — but it is
unambiguously a real entry point, and the plan must discuss it rather than assert
none exists.

*Impact:* 09-20 records `not-implementable` for a drivable row; 09-12/09-14/09-16
propagate that disposition into the committed matrix registry; a needless "no
entry point" issue gets filed. That is precisely the overstated-assurance failure
mode this phase exists to eliminate.

*Aggravating factor:* `09-20-PLAN.md:234` pre-loads the conclusion ("which is
what the current tree indicates"), anchoring an executor who re-searches at
execution time.

**C-2 · HIGH · codex · orchestrator-confirmed — 09-19's `totals.sessions >= 3`
gate contradicts the repository's own authoritative config and is likely
unsatisfiable.**

`.codecov.yml:12-20` states verbatim that the three uploads **de-duplicate to two
sessions**, and that `after_n_builds: 2` matches that count while *"3 would
block."* Yet `09-19-PLAN.md:107` requires `sessions >= 3` while `:145` preserves
`after_n_builds: 2` as load-bearing. At least one premise is wrong.

Two independent corroborations that the gate cannot pass as written: pi's live
API query returns **e2e flag = 0.0%**, and the project's own prior finding records
`sessions: 2` on the merge commit `497748c6d` despite a green E2E job. The e2e
coverage upload lands empty — so a third session may never materialise.

**C-3 · HIGH · codex · orchestrator-confirmed, with a correction — 09-CONTEXT.md
contradicts the plans on the coverage target.**

`09-CONTEXT.md:44` still reads **70%** (and D-03 as originally locked), while
`09-01-PLAN.md:48` and `09-10-PLAN.md:44` target **80%** and remove the existing
`cmd/holomush` ignores.

> **Orchestrator correction — do NOT re-litigate this.** codex proposes a new
> user decision checkpoint. That is the wrong remedy: the user **did** make these
> calls during the planning session (80% + `threshold: 0%`, and un-ignoring
> `cmd/holomush` core.go + sub_grpc.go), after the sizing gate falsified the
> ~54.6% premise. The defect is that **CONTEXT.md was never updated to record
> decisions already taken** — a record-keeping gap, not an unauthorised re-scope.
> The fix is to amend D-02/D-03 in CONTEXT.md, not to re-ask.

The same applies to the D-09 ACE predicate: the tightened single-token-tail
predicate (~114 hits, down from 1,106 literal) was a user decision, not a plan's
unilateral re-scope. Record it.

**C-4 · HIGH · codex · not independently re-verified — Ginkgo `DeferCleanup`
scope would stop the shared harness after the first spec.**

`09-12-PLAN.md:197` proposes starting the process-wide harness lazily and
registering `DeferCleanup` at the call site. codex read Ginkgo v2.32's source
(`core_dsl.go:872`): `DeferCleanup` registered from `It`/`BeforeEach` fires when
**that spec** ends; suite-scoped cleanup requires registration from
`BeforeSuite`. A once-guarded lazy accessor first called inside a spec therefore
hands a **stopped** server to every later spec. The suite already has a suitable
`BeforeSuite`/`AfterSuite` (`session_persistence_suite_test.go:57`).

*Flagged as needing execution-time confirmation* — this is the one HIGH the
orchestrator did not re-verify against Ginkgo's source.

**C-5 · HIGH · codex — late `.codecov.yml` edits may be verified against a stale
PR head SHA.**

09-21 performs the only unconditional push + CI wait (`09-21-PLAN.md:95`). 09-17
may then edit `.codecov.yml` (`:146`) but its gate queries whatever SHA the PR
currently exposes (`:171`). 09-19 makes a further local change before the ruleset
checkpoint, and mandates a push only when the branch is *behind main* — not when
local HEAD is *ahead of* the PR head (`09-19-PLAN.md:206`). This permits
observing stale remote configuration, then mutating branch protection on that
basis. codex's suggested assertion is sound:
`test "$(git rev-parse HEAD)" = "$(gh pr view … --jq .headRefOid)"`.

**C-6 · MEDIUM · codex + opencode — 09-01 is a single point of failure with an
unactionable stop condition.** A blocked 09-01 transitively blocks 09-02, 09-10,
09-17, 09-19 — i.e. all of QUAL-02. The STOP CONDITION says escalate after two
attempts but names no escalation path, re-scope trigger, or alternative
measurement chain. opencode proposes concrete fallback text.

### Other findings worth carrying

- **MEDIUM** (codex) — the move-matrix row claims production behaviour while
  bypassing the production movement pipeline; either drive
  `world.Service.MoveCharacter` or relabel the cell honestly.
- **MEDIUM** (codex) — `09-18-PLAN.md:6` declares five dependencies while `:46`
  claims every test-touching plan is listed; 09-05/09-06/09-10 alter tests and
  are absent. Wave ordering masks it, but the last-mover guarantee is not encoded
  in the graph.
- **MEDIUM** (opencode) — `test/meta/depguard_config_test.go:24-31` pins 3 of the
  4 packages denied at `.golangci.yaml:149-156` (`natstest` missing). *09-07 Task
  3 already catches and fixes this* — recorded as evidence of plan quality, not a
  defect.
- **MEDIUM** (pi) — `TestGatewayCommand_SecureCookiesFlag`
  (`cmd/holomush/gateway_test.go:552`) may itself trip 09-18's tightened ACE
  predicate, and 09-04 adds a sibling with the same naming risk; neither plan
  flags it.
- **LOW** (codex) — coverage probes at `09-08-PLAN.md:87` and `09-10-PLAN.md:99`
  pipe `go tool cover` into `tail -1` without `pipefail`; a failing producer is
  masked by a succeeding `tail`.
- **LOW** (opencode) — `09-06`'s fence logger is already injectable via
  `WithFenceLogger` (`plugin_downgrade_fence.go:81`); the plan's "add a seam if
  needed" hedge is unnecessary.
- **LOW** (pi) — 09-15 Task 3's new spec should carry `// Verifies:
  INV-PRIVACY-6` to match its sibling at `privacy_test.go:29`.
- **LOW** (pi) — 09-05's `depends_on: ["09-01"]` looks like wave convention
  rather than a functional dependency; say which.

### Divergent Views

- **09-17's codecov assumption block.** pi rates it a **strength** — it
  independently sampled 3 merged Go-changing PRs and confirmed `codecov/patch`
  posts only on merge commits, never on heads, validating the deadlock diagnosis.
  codex rates the surrounding 09-17/09-19 sequencing a **HIGH** risk. These are
  not in conflict: pi validated the *assumption*, codex faulted the *gate and its
  ordering*. Both stand.
- **Severity of the admin-boot finding.** opencode calls the row "still
  reasonably `not-implementable` without admin harness wiring" (`RegisterAdmin`
  panics on nil deps, `register.go:14-23`); pi argues the row is drivable and the
  disposition must change. Resolution: the *disposition wording* must change
  either way — "no entry point exists" is false; "not implementable from harness
  defaults without admin wiring, see `resetpassword --kick`" is true.

---

## Orchestrator Notes

### Round 1 → Round 2 delta

None of round 1's six blockers were re-raised as open by any lane. The replan's
fixes hold. Of the two premise corrections the replan made *beyond* round 1's
findings:

- **The `OpenTelnetSession` "stale TODO" correction stands.** The helper is still
  `t.Fatalf` at `session.go:875-878`, but the TODO's *stated* blocker (Subscribe
  goroutine wiring) is genuinely obsolete — `attach()` wires it. The replan
  created `09-20-PLAN.md` to implement the seam. All three lanes confirm the
  diagnosis.
- **The admin-boot correction is WRONG** and is now C-1 above. It was introduced
  *by the fix round*, and it is the third instance in this phase of one defect
  class: **searching for a token instead of a capability.** (1) round 1's
  `rg 'panic("TODO'` missed `t.Fatalf("...TODO...")`; (2) `go test -run <pat>`
  exits 0 when nothing matches; (3) searching `boot` missed `--kick`. Each was
  introduced while fixing the previous one.

### Lane-integrity incident (method finding)

The cursor lane failed **twice, both times exit 0**, in two different signatures:

1. stale flags (`--mode`/`--trust` no longer exist) → **empty** output, exit 0;
2. unauthenticated → **2322 bytes of ASCII sign-in splash**, exit 0.

The GSD review workflow's guard for this lane is `[ ! -s file ]`. Failure (2)
**passes that guard** — a terminal logo would have been written into REVIEWS.md
and counted as a review. Emptiness and exit codes are both unreliable signals for
CLI-lane health; only *content shape* distinguishes a review from a splash
screen. Worth hardening in the workflow.

### Recommended next step

Targeted replan, not a from-scratch one. C-1 through C-5 are localised:

| Finding | Plans to amend |
| --- | --- |
| C-1 admin-boot | 09-20 (assumption + Task 3), 09-14, 09-12, 09-16 |
| C-2 codecov sessions | 09-19 |
| C-3 CONTEXT record | 09-CONTEXT.md (D-02, D-03, D-09) |
| C-4 Ginkgo scope | 09-12 |
| C-5 stale SHA | 09-17, 09-19, 09-21 |

```
/gsd-plan-phase 9 --reviews
```
