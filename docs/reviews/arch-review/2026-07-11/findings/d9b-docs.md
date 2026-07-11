<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# D9b — Documentation (accuracy, completeness, usability) — Findings

**Agent:** general-purpose/claude-sonnet-5 · **Date:** 2026-07-11 · **Scope examined:** `site/src/content/docs/` (all 5 audiences, ~87 pages, spot-checked against source), `docs/roadmap.md`, `docs/adr/` (sampled citations), `docs/architecture/invariants.{yaml,md}` (regeneration check), `.claude/rules/plugin-manifest.md`, reference-doc generation (`scripts/gen-event-docs.sh`, `task docs:proto`, `task docs:linkcheck`), internal link integrity (static + live crawl).

## Summary

The docs corpus is a genuine strength of this project: five audience-organized trees, a real auto-generation pipeline for event/gRPC references, an invariant registry that round-trips clean, and several very recently written operator runbooks (`external-nats-deployment.md`, `sentry.md`) that I verified line-by-line against source and found **fully accurate** — every config key, CLI subcommand, error code, and Prometheus metric name checked out. Voice and IA discipline (audience dirs, absolute-path-only internal links, no drift into `room` terminology) hold up well.

The weak spot is `contributing/explanation/architecture.md`, the single highest-traffic contributor doc — it self-contradicts on the event-ordering model and describes two implemented subsystems (Channels, the web transport) using stale/wrong language. This is already tracked as generally stale (#4667); my findings add the specific, falsifiable instances. I also found one genuine internal-docs defect (`plugin-manifest.md`'s required-fields table is provably wrong) and one process gap (`docs/roadmap.md` has an orphaned `theme:*` label, which the project's own CLAUDE.md rule forbids).

**Counts:** 0 Blocker, 0 High, 5 Medium, 3 Low, 1 dedicated Strengths section.

## Findings

### MEDIUM-1 architecture.md self-contradicts on event ordering (ULID vs. JetStream sequence)

- **Severity:** Medium
- **Claim:** The "Core Components → Event System" concept table asserts events have "ULID ordering," directly contradicted three paragraphs later by the same document's own "Event Ordering Model" section, and by the actual mechanism in code.
- **Evidence:** `site/src/content/docs/contributing/explanation/architecture.md:71` (`| **Events** | Immutable records with ULID ordering |`) vs. `architecture.md:84-100` ("HoloMUSH uses **per-stream ordering**... Events across different streams have no ordering guarantee") vs. code: `internal/eventbus/integration_test.go:33` ("this via JS's per-stream monotonically-increasing sequence: all events...") and the project-wide rule (`CLAUDE.md` ULID Generation table): "Ordering is JetStream's per-stream `uint64` seq — **not** ULID lex order." The same false claim repeats in the "Technology Stack" table: `architecture.md:295` (`| **Events** | Custom (ULID ordered) | Append-only, replayable |`).
- **Impact:** A contributor skimming the concept table (the first thing on the page after the diagram) walks away with the wrong mental model of the system's core ordering guarantee — exactly the kind of "docs that lie" this review is calibrated to catch, on the project's most-referenced architecture doc.
- **Recommendation:** Change both table rows to "JetStream per-stream sequence ordering; ULID is identity/dedup only" — one clause, already stated correctly elsewhere in the same file.
- **Dedup:** already-tracked:#4667 (general "describes pre-cutover model (stale)"); this is a specific, previously-unenumerated instance.

### MEDIUM-2 architecture.md: Channels marked "(future)" — subsystem has shipped

- **Severity:** Medium
- **Claim:** The Stream Types table lists the `channel.<name>` stream's content as "Channel messages (future)," but the Channels subsystem is a fully implemented, in-tree binary plugin.
- **Evidence:** `site/src/content/docs/contributing/explanation/architecture.md:113` — the stream table row for the `channel.<name>` domain is labelled "Channel messages (future)", but `plugins/core-channels/plugin.yaml` is a real shipped manifest (`emits: [channel]`, `history_scope: custom`) and `plugins/core-channels/main.go:95` qualifies each subject to `events.<game>.channel.<id>`. Per the review's system map, Channels shipped as Phase 1 of PR #4595, merged before this baseline.
- **Impact:** A contributor reading the architecture doc to understand what streams exist is told a real, load-bearing subsystem doesn't exist yet.
- **Recommendation:** Drop "(future)"; the row is otherwise accurate (subject shape matches).
- **Dedup:** already-tracked:#4667.

### MEDIUM-3 architecture.md + operating/index.mdx: wrong web transport technology ("WebSocket" / gorilla/websocket)

- **Severity:** Medium
- **Claim:** Both docs describe the web client's transport as "WebSocket," and architecture.md names `gorilla/websocket` as the concrete dependency. Neither is true — the web transport is ConnectRPC (HTTP-based streaming), and there is no WebSocket code anywhere in the repo.
- **Evidence:** `architecture.md:297` (`| **WebSocket** | gorilla/websocket | Mature, widely used |`) and the mermaid diagram at `architecture.md:26` (`WS[WebSocket Adapter]`); `site/src/content/docs/operating/index.mdx` ("**WebSocket** (port 8080) — Modern web client with PWA support"). Verified: `grep -n "gorilla/websocket" go.mod go.sum` → zero hits; `grep -rn "websocket|WebSocket" internal/ cmd/ web/src/` (excluding tests) → zero hits anywhere in the codebase. `go.mod:8` shows the real dependency: `connectrpc.com/connect v1.20.0`. Other docs get this right — `site/src/content/docs/operating/reference/configuration.md:415` correctly says "Serves the ConnectRPC API."
- **Impact:** Misleads both contributors (hunting for a nonexistent `gorilla/websocket` dependency) and operators (who may over-provision reverse-proxy config for `Upgrade`/`Connection` WebSocket headers that ConnectRPC doesn't need — though I confirmed the deployment docs themselves don't propagate this into a broken nginx example).
- **Recommendation:** Replace "WebSocket" → "ConnectRPC (HTTP)" in both the tech-stack table and operating/index.mdx's connection-methods list; update the mermaid diagram label.
- **Dedup:** already-tracked:#4667 (architecture.md instance only — operating/index.mdx is a separate file/issue, not previously flagged).

### MEDIUM-4 docs/roadmap.md: orphaned `theme:plugin-trust` label

- **Severity:** Medium
- **Claim:** The GitHub label `theme:plugin-trust` is applied to open issues, but `docs/roadmap.md` has zero narrative section for it — violating the project's own CLAUDE.md rule ("**MUST NOT** orphan labels: If a `theme:*` label exists with no narrative section in `docs/roadmap.md`, either add the section or drop the label").
- **Evidence:** `jq -r '.[].labels[].name' evidence/open-issues.json | grep '^theme:' | sort -u` → `theme:plugin-capability-architecture`, `theme:plugin-trust`, `theme:social-spaces`, `theme:web-portals`. Issues #4768 and #4735 both carry `theme:plugin-trust`. `grep -n "plugin-trust" docs/roadmap.md` → no matches (exit 1). `docs/roadmap.md`'s three active-theme sections (`## Active themes`, lines 36-237) are only social-spaces, plugin-capability-architecture, web-portals; plugin-trust appears in neither "Active," "Completed," nor "Future themes."
- **Impact:** A reader trying to understand the "why" behind the two plugin-trust-labeled issues (INV-PLUGIN-54 coverage gaps) has no roadmap entry to consult — the exact double-entry drift the CLAUDE.md rule exists to prevent.
- **Recommendation:** Either add a narrative section (even a short "future theme, not yet promoted" stub matching the "Future themes (sketches)" pattern already used for other pre-promotion clusters) or drop the label from the two issues.
- **Dedup:** none found (searched open-issues.json for theme/roadmap-titled issues; no match).

### MEDIUM-5 `.claude/rules/plugin-manifest.md`: "Required top-level fields" table is wrong

- **Severity:** Medium
- **Claim:** The rule's "Required top-level fields" table lists `resource_types`, `requires`, and `provides` alongside `name`/`version`/`type` as required — but only `name`, `version`, and `type` are actually required by the manifest schema, and multiple real in-tree plugins ship without any of the other three.
- **Evidence:** `.claude/rules/plugin-manifest.md` "Required top-level fields" table includes `resource_types`, `requires`, `provides`. Code ground truth: `internal/plugin/manifest.go:89-91` — only `Name`, `Version`, `Type` carry `jsonschema:"required"`; `ResourceTypes` (line 148), `Requires` (line 117), `Provides` (line 118) are all `yaml:"...,omitempty"` with no required tag. Empirical proof: `plugins/echo-bot/plugin.yaml` is a real, loaded, in-tree Lua plugin with **no** `resource_types`, `requires`, or `provides` fields at all.
- **Impact:** This rule file is consulted by both AI agents and human contributors as ground truth when authoring or reviewing plugin manifests (per its own path-trigger in the project's `.claude/rules/`). A contributor trusting the "required" label would add unnecessary fields or, worse, conclude a manifest lacking them is broken when it is in fact valid and already shipping.
- **Recommendation:** Move `resource_types`/`requires`/`provides` to the "Conditionally required" or a new "Optional" table (they're conditionally required only when the plugin owns ABAC resource types / declares service dependencies / provides services, respectively) — mirroring how `storage` is already correctly placed as "Conditionally required."
- **Dedup:** none found.

### LOW-1 pr-prep.md: fast-lane step list is incomplete; "9-step" mislabel

- **Severity:** Low
- **Claim:** The "Fast lane" step enumeration omits a real check step, and a later section miscounts the number of steps in `pr-prep:run`.
- **Evidence:** `site/src/content/docs/contributing/how-to/pr-prep.md:20-29` lists 8 fast-lane steps (bats, schema check, license, plugin builds, lint, fmt, unit tests, build). The real inner body, `Taskfile.yaml:860-898` (`pr-prep:fast:run`), runs 9 steps — the doc's list omits the "Verifying Lua host-cap bindings are current" check (`Taskfile.yaml:875-885`, comparing `internal/plugin/luabridge/bindings_gen.go` + `pkg/plugin/luastubs/holomush.lua` before/after `go generate`). Separately, `pr-prep.md:148` calls `pr-prep:run` "the actual 9-step CI mirror," but `Taskfile.yaml:983-1034` shows `pr-prep:run` (the full-lane inner body, distinct from `pr-prep:fast:run`) runs 11 checks: bats, schema, Lua-bindings, EBNF+railroad (`generate:ebnf:check`), license, plugin-build, lint, fmt, unit test, integration test, E2E test.
- **Impact:** Low — the gate itself is unaffected and the CLI's own error messages are self-explanatory when these checks fail, so this is a documentation completeness gap rather than a functional one.
- **Recommendation:** Add the Lua-bindings-check line to the fast-lane list; fix "9-step" → "11-step" (or drop the specific count).
- **Dedup:** none found.

### MEDIUM-6 `docs:linkcheck` provides negligible regression protection

- **Severity:** Medium
- **Claim:** The internal-link checker task (a) is not wired into any lint/CI/pr-prep gate, and (b) even run manually, its invocation only crawls to depth 1 (~20-25 links from the homepage + top-level section indexes), not the full ~87-page site — so a broken deep cross-page link (e.g., one of architecture.md's "Further Reading" links) would not be caught by it either way.
- **Evidence:** `grep -n "docs:linkcheck" Taskfile.yaml` → only the task's own definition (`Taskfile.yaml:1216-1219`); zero references in `.github/workflows/*.yaml`, `task lint`, `task pr-prep`, or `task pr-prep:docs` (`Taskfile.yaml:1036-1048`, the docs-only fast lane, has no linkcheck step). Ran it live this session: `task docs:linkcheck` → `bunx linkinator dist/index.html --recurse --silent ...` → `✓ Successfully scanned 20 links in 0.038 seconds` — confirmed by a manual rerun (`bunx linkinator dist/index.html --recurse`) that shows it visits `dist/index.html` plus the four top-level section index files (`dist/guide/`, `dist/extending/`, `dist/operating/`, `dist/contributing/`) and stops — it never recurses into any of the ~80 leaf pages (e.g. `dist/contributing/how-to/pr-prep/` is never visited), because local-file-mode link resolution doesn't parse those folder-index pages for further outbound links.
- **Impact:** A future PR that renames or removes a page (as architecture.md's own "Further Reading" section links to 5 other pages) has no automated backstop, despite one existing and appearing to work (exit 0, "successfully scanned").
- **Recommendation:** Either wire `docs:linkcheck` into `pr-prep:docs` (cheap — it already depends on `docs:build`), or point it at a served URL (`bunx serve dist &`) instead of the local file path so `--recurse` actually traverses the full site.
- **Dedup:** none found. Note: I independently re-verified internal link **integrity** by statically matching every `](/...)` link across all 87 content files against real content slugs — **zero broken links found today** (see Strengths). This finding is about the safety net's coverage, not a current defect.

### LOW-2 `reference/grpc-api.md` has no staleness gate

- **Severity:** Low
- **Claim:** The committed, "auto-generated" gRPC API reference has no CI or lint check verifying it's current with `api/proto/**`, unlike the sibling generated-doc mechanisms (`docs/architecture/invariants.md` has `go run ./cmd/inv-render --check`; `reference/events/*.md` is gitignored and rebuilt every `docs:build`).
- **Evidence:** `grep -n "docs:proto" Taskfile.yaml .github/workflows/*.yaml` → only the task definition itself (`Taskfile.yaml:1223`), never invoked elsewhere. I confirmed the file is currently accurate (`git log -1 --format=%cd -- site/src/content/docs/reference/grpc-api.md` → 2026-07-09, same day as the most recent `api/proto/holomush/channel/v1/channel.proto` commit, and it does document the Channel service), so this is a process-fragility finding, not an active inaccuracy.
- **Impact:** Low today; the drift risk grows every proto change that a contributor forgets to pair with `task docs:proto`.
- **Recommendation:** Add a `--check`-style diff gate (regenerate to a temp path, diff, fail on mismatch) to `pr-prep:docs` or `lint:proto`, mirroring `cmd/inv-render`'s pattern.
- **Dedup:** none found.

## Strengths

- **`operating/how-to/external-nats-deployment.md`** (same-day-merged Phase 3 runbook) is exhaustively accurate — I verified every config key (`event_bus.{mode,url,credentials,tls.{ca,cert,key},provision,dlq.{max_age,max_bytes}}` against `internal/eventbus/config.go:41-106`), every CLI subcommand (`holomush audit dlq {list,show,replay}` against `cmd/holomush/cmd_audit.go:45-109`), the Prometheus metric name (`holomush_audit_dlq_messages_total` against `internal/eventbus/audit/lag_metric.go:46-51`), the error code (`EVENTBUS_ACCOUNT_OVERSCOPED` against `internal/eventbus/scopecheck.go:46-48`), and the `deploy/nats/verify-scoping.sh` env-var contract. Zero discrepancies found.
- **`operating/how-to/sentry.md`** is similarly precise against `internal/web/sentry_relay.go`, `internal/web/otlp_relay.go`, and the server-side env vars in `internal/telemetry/sentry.go` — every endpoint path, size limit, and gate condition checked out.
- **`extending/reference/plugin-api.md`** and **`extending/tutorials/plugin-guide.mdx`**: every documented Lua host function (`kv_get/set/delete`, `query_location/character/location_characters/object`, ABAC resource-pattern table) verified against `internal/plugin/hostfunc/functions.go` and `internal/plugin/hostfunc/world.go`; manifest field examples match `internal/plugin/manifest.go`'s schema tags exactly.
- **`contributing/how-to/sessions.md`**, **`database-migrations.md`**, and the `pr-prep.md` concurrency/lock-recovery sections: every CLI command and Taskfile behavior I spot-checked was accurate (`task workspace:new`, `holomush migrate {up,down,status,version,force}`, the flock/lsof recovery recipe).
- **`docs/architecture/invariants.md` is in sync with `invariants.yaml`** — `go run ./cmd/inv-render --check` exits 0.
- **Zero broken internal links found** via independent static verification: every `](/...)`-style link across all 87 `site/src/content/docs/**/*.{md,mdx}` files resolves to a real content slug (custom Python check, this session). The docs also consistently use absolute-path links exclusively — no relative-link style drift found.
- **`AGENTS.md` genuinely is a relative symlink to `CLAUDE.md`** (`ls -la` confirms `lrwxr-xr-x ... AGENTS.md -> CLAUDE.md`), matching the CLAUDE.md header's claim.
- **The `crypto.emits` event-reference generation (`scripts/gen-event-docs.sh`)** is real, idempotent, and correctly designed: output is gitignored and regenerated at `task docs:build`/`docs:serve` time from the live `plugins/*/plugin.yaml`, so it cannot drift the way a committed generated doc can — confirmed live (`task docs:linkcheck` regenerated 3 plugin event pages from `core-communication`/`core-objects`/`core-scenes` correctly on this run).
- **197 ADRs** in `docs/adr/`; every ADR ID cited in the `.claude/rules/*.md` files I sampled (`holomush-v4qmu`, `holomush-iv43`, `holomush-ti1b`, `holomush-qf2oo`, `holomush-cr3gq`, `holomush-sb3n`) resolved to a real file.
- Audience organization (`guide`/`operating`/`extending`/`contributing`/`reference`) is consistently applied and each has a clear "start here" entry point (e.g. `operating/index.mdx` → "Start here → Installing HoloMUSH"; `extending/tutorials/getting-started.md` → prerequisites link to the operator install guide). No end-to-end "how do I actually run a game" gap found — it's covered, just split across the operating install guide + the plugin getting-started tutorial as intended by the audience split.

## Not examined

- Live sandbox operational runbooks (`sandbox-operations.md`, `sandbox-restore.md`) — cannot verify against production `game.holomush.dev` without operational access; the embedded `ssh`/`docker compose` commands were read but not executed.
- `guide/` (player-facing) docs beyond a terminology (`room` vs `location`) spot-check — clean on that axis, but not otherwise deep-verified.
- Full `reference/grpc-api.md` content-by-content diff against all 27 protos — spot-checked only the Channel service's presence/recency.
- `operating/how-to/crypto/{crypto-setup,crypto-runbook,crypto-monitoring}.md`, `ca-rotation.md`, `telnet-security.md`, `authentication-recovery.md`, `operating/explanation/{authentication,plugin-security}.md` — not verified this session; time-boxed out in favor of breadth across audiences plus the explicitly-named targets (architecture.md, the Phase 3 NATS runbook, plugin manifest docs, pr-prep/sessions/migrations/quarantine).
- `docs/superpowers/specs/` and `docs/superpowers/plans/` corpus (very large) — not audited for staleness beyond the invariants.yaml cross-check; `docs/architecture/invariants.md`'s `binding: pending` ratio was not tallied (per the briefing pack, pending bindings are known/tolerated, not a discovery).
- `web/CLAUDE.md` — referenced by multiple docs as the SvelteKit-pattern source of truth but not independently audited.
- `extending/tutorials/{binary-plugins,lua-plugins}.md` beyond the shared manifest-schema spot-check already covered via `plugin-guide.mdx`.
- `quarantine.md`, `integration-tests.md` contributor how-tos — not independently verified against `test/quarantine.yaml` / the harness this session (time-boxed out).
