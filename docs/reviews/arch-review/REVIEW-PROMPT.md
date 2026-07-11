# Reusable Prompt — HoloMUSH L7 Architecture & Quality Review

Paste the block below into a fresh session (ideally in a dedicated worktree — `task workspace:new -- arch-review-<date>`) to re-run the full multi-agent review. It reproduces the 2026-07-11 review's method, roster, fairness contract, and hard-won gotchas. Replace `<DATE>` with today's (`date +%Y-%m-%d`).

The first review's output — the gold-standard shape to match — lives at `docs/reviews/arch-review/2026-07-11/` (REPORT.md, findings/, verification/, issue-plan.md, STATUS.md). Read it first if you want a worked example.

---

## THE PROMPT

> You are an L7 engineer conducting a full architecture & quality review of HoloMUSH. Be fair, impartial, and critical without being harsh. **Calibration: this is a hobbyist/community-scale platform, NOT a five-nines system — but it explicitly targets reliability, correctness, and usability. Judge findings against *those* goals.** Credit strengths explicitly; a fair review says what's done well. Your work will be reviewed adversarially and you must be able to defend every finding. Trust nothing without verification or a cited source.
>
> **Workspace & output:** `docs/reviews/arch-review/<DATE>/`. Subdirs: `findings/` (per-dimension), `verification/` (adversarial checks), `evidence/` (raw tool output + `ui/` screenshots). Deliverables: `00-review-plan.md`, `01-system-map.md` (briefing pack), `REPORT.md`, `issue-plan.md`, `STATUS.md` (task ledger — your recovery point across context loss; keep it current). **Present the plan and wait for approval before dispatching agents. File GitHub issues ONLY after a second explicit approval gate.**
>
> **Method (5 phases):**
>
> 1. **Ground truth (main loop):** read `site/src/content/docs/contributing/explanation/architecture.md`, `docs/architecture/invariants.yaml`, the EventBus + roadmap design docs; build a system map with `codegraph_explore`/`probe`; snapshot open issues for dedup (`gh issue list -R holomush/holomush --state open --limit 400 --json number,title,labels > evidence/open-issues.json`); stand up the app (`task dev:obs`, background — web :8080, telnet :4201; needs Docker); write `01-system-map.md` as a shared briefing pack (container diagram, load-bearing flows verified from source, size inventory, where-things-live table, the severity rubric + citation contract + dedup + findings-file format below). Give this pack to every agent.
> 2. **Evidence fan-out (11 read-only agents, parallel, each writes its own `findings/dN-*.md`):**
>    - D1 Architecture — `comprehensive-review:comprehensive-review-architect-review` (opus)
>    - D2 ABAC — repo `abac-reviewer` (opus, repo-pinned)
>    - D3 Event crypto — repo `crypto-reviewer` (opus, repo-pinned)
>    - D4 Perimeter security — `comprehensive-review:comprehensive-review-security-auditor` (opus)
>    - D5 Performance — `observability-monitoring:observability-monitoring-performance-engineer` (opus)
>    - D6 Reliability/observability — `systems-programming:golang-pro` (sonnet)
>    - D7 Data layer — `observability-monitoring:observability-monitoring-database-optimizer` (sonnet)
>    - D8a UI static — `gsd-ui-auditor` (sonnet)
>    - D9a Testing/CI — `codebase-cleanup:codebase-cleanup-test-automator` (sonnet)
>    - D9b Docs accuracy — `general-purpose` (sonnet), method = verify-against-code
>    - D9c Dependencies — `general-purpose` (sonnet), run real tools (govulncheck, pnpm/bun audit), save raw output to `evidence/deps/`
>    - **D8b UI live (main loop, NOT an agent — needs the running stack):** drive the app with `agent-browser` (open :8080 → `snapshot -i` → `click`/`fill`/`press Enter` → screenshot to `evidence/ui/`). Exercise: guest login, terminal verbs (say/pose/ooc/help/look/who), movement (exit click + typed), command palette, character switcher. Extract transcript via `agent-browser eval "document.body.innerText"`.
> 3. **Adversarial verification:** for EVERY Blocker/High finding, spawn an independent skeptic (`general-purpose` sonnet) told to **REFUTE it, defaulting to REFUTED if uncertain**, re-deriving from source; write `verification/skeptic-*.md`. Personally spot-check the tool-output findings (deps versions, CI rulesets) and record in `verification/spot-checks.md`. Apply any self-corrections honestly in place.
> 4. **Cross-model second opinion:** bounce the finding set + severity calls off `codex:codex-rescue` framed as "be a skeptic of the REVIEW, not the codebase — where am I unfair, overstating, or blind?" Record verbatim + your adjudication in `verification/codex-opinion.md`.
> 5. **Synthesis + issue plan:** `REPORT.md` with mermaid C4 (context+container) + PlantUML sequence diagrams for the two load-bearing flows (event publish→fan-out→audit; command dispatch→ABAC→execute), a **dual/multi-rubric severity presentation** (separate operational-runtime from product-readiness from architecture-integrity from assurance-governance — do NOT flatten everything to "High"), a strengths section, an already-tracked ledger (dedup vs `evidence/open-issues.json`), and a limitations/methodology appendix (the defense package). Then `issue-plan.md` mapping findings → epics/tasks with labels/priorities/acceptance, deduped. **Approval gate before `gh issue create`.** Then land the branch (commit, `task pr-prep`, push, PR).
>
> **Every agent gets these ground rules:** read-only wrt code (write only under the workspace); citation-or-it-didn't-happen (`path:line` verified this session, or URL with date); search ladder probe/codegraph → `rg` (never bare grep) → ast-grep; judge commands by exit code not stdout strings; cross-reference `docs/adr/` + `.claude/rules/` + `docs/architecture/invariants.yaml` before calling something a defect (a `binding: pending` invariant is a known coverage gap, not a discovery; recorded ADR decisions aren't findings). Findings-file format: per-finding = Severity + falsifiable Claim + Evidence (`path:line`) + Impact + Recommendation + Dedup. Return ≤300-word summaries; write full findings to disk.
>
> **Severity rubric:** Blocker (breaks a core promise: data loss, auth bypass, plaintext leak, order corruption) / High / Medium / Low / Info+Strength. But at synthesis, sort Highs by *rubric* (operational teeth vs product-readiness vs architecture-integrity vs assurance-governance) — a coverage-policy gap and an unauthenticated OOM are both "High" and should not read as equally urgent.

---

## GOTCHAS learned 2026-07-11 (save the next run hours)

- **Terminal eats "move"/"Move" (and some other substrings) in Bash `rg`/`grep`/`git log` STDOUT** — a pty rendering artifact ("MoveCharacter"→gone, "removes"→"res", "events_audit"→"ln"). Bash search output for movement/removal symbols is UNRELIABLE. Use `mcp__probe__search_code` / `codegraph_explore` / `Read` (all render correctly) for any symbol containing those substrings. Confirmed this run.
- **Repo-pinned reviewers (`abac-reviewer`, `crypto-reviewer`) and some plugin reviewers spawn an internal worker** → you get **two completion notifications** per agent, sometimes writing **two findings files** (e.g. D4 wrote both `d4-perimeter.md` and `04-perimeter-platform-security.md`). The two passes can **disagree** — treat a disagreement as a signal to adjudicate yourself (this run, one D4 pass flagged a real OOM the other missed). Reconcile both files at synthesis.
- **`agent-browser` flags:** screenshot is `--full` not `--full-page`, arg order `screenshot [selector] [path]`; there is no `text` subcommand — use `eval "document.body.innerText"` or `read`. Ensure the output dir exists (a recreated dir mid-run silently breaks a screenshot path).
- **Movement is the canonical live-finding trap:** the world engine has `MoveCharacter` but (as of this baseline) NO command calls it; the exit button does `sendCommand(direction)` into a dead registry lookup. If re-running, verify whether that's been fixed before re-filing.
- **Event-sourcing reframe (F1):** don't stop at "docs are wrong." The world model is CRUD and was *never* event-sourced (no rebuild path ever existed; the removed F7 "replay" was client-catch-up). The finding is an architecture-decision investigation (build it vs formally adopt CRUD + ADR), and it's the root cause of the dual-write + last-write-wins bugs. See `2026-07-11/verification/f1-eventsourcing-why.md`.
- **Biggest methodology blind spot (codex, unaddressed):** dimension-scoped agents miss *emergent* cross-subsystem behavior. The unanswered question every run should try to answer: *"two players act concurrently during a NATS broker flap while one replica restarts — what breaks?"* Consider adding a dedicated resilience/chaos pass as a 12th workstream.
- **CI reality:** `main` is gated by branch-protection ruleset `protect-main` (Build/Lint/Test/CodeRabbit/Integration/E2E) — NO coverage gate, despite a ">80% per-package" doc MUST. CI runs the underlying `task` targets directly, NOT `task pr-prep`.
- **Dedup is essential:** ~180+ open issues; many findings are already tracked. Always `jq`/`rg` `evidence/open-issues.json` before proposing an issue.
- **Task tracking:** mirror phases into the harness TaskCreate/TaskUpdate for live visibility, but keep `STATUS.md` as the durable recovery point (harness state doesn't survive a session; a committed file does).
- **Memory:** recall engram at start (spine + `ws:*` overlay); store the review outcome to the workspace overlay at the end.

## Cadence suggestion

Re-run after each milestone or ~quarterly. A re-run is cheap to scope down: the fan-out roster and gotchas are fixed; only the baseline commit and the dedup snapshot change. For a lighter touch, run just the phase most relevant to recent work (e.g. security after an auth change) using that dimension's agent + the skeptic pass.
