<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Phase 2 — Main-loop citation spot-checks & adjudications

Reviewer (me) independently re-derives load-bearing Blocker/High claims from source before they feed the report/issues. Each entry: claim → what I checked → verdict.

## D4 two-pass disagreement — ADJUDICATED (finding UPHELD as High)

The D4 security-auditor produced **two divergent reports** (parent agent `d4-perimeter.md` + its internal worker `04-perimeter-platform-security.md`). They disagree on the top finding:

- Worker: **HIGH — gateway ConnectRPC handler has no `WithReadMaxBytes` → unbounded body buffering → OOM.**
- Parent: **0 High**; does not surface the OOM item; top finding is secure-cookies-default-false (Medium).

**My independent adjudication → the OOM finding is REAL and High:**

- `internal/web/server.go:68-76` constructs the public ConnectRPC mux with `connect.WithInterceptors(...)` only — **no `connect.WithReadMaxBytes`**. Verified this session.
- `connectrpc.com/connect@v1.20.0/protocol_connect.go:1119`: `if u.readMaxBytes > 0 && int64(u.readMaxBytes) < math.MaxInt64 { reader = io.LimitReader(...) }` — the LimitReader is applied **only when readMaxBytes > 0**; the size check at `:1132` is likewise gated on `> 0`. Unset (0) ⇒ the entire request body is read into memory with no cap. Verified in the vendored module source this session (`go env GOMODCACHE`).
- Contrast: core gRPC caps inbound at 4 MiB — `internal/grpc/server.go:56` `MaxRecvMsgSize = 4*1024*1024`, applied at `:1850/:1868`. So the gap is an asymmetry, not a deliberate uniform choice.
- Partial mitigation: `internal/web/server.go:137` sets `ReadHeaderTimeout: 10*time.Second` (slowloris-on-headers covered), but **no `ReadTimeout`/`WriteTimeout`** — a large or slow *body* is not bounded.
- **Severity call:** the gateway is the unauthenticated public web entrypoint (:8080). A single POST can drive memory arbitrarily high. Not a Blocker (no data loss / auth bypass), but High for a system that targets reliability. One-line fix: `connect.WithReadMaxBytes(4<<20)` + an `http.Server.ReadTimeout`.
- **Merge action for the report:** keep the worker's OOM finding (High) + the parent's richer strengths (13) and extra Lows (argon2 t=1 vs RFC 9106; username-enum timing; socket bind→chmod race). Both agree on secure-cookies-default-false (Medium, untracked), Lua watchdog (#4675), per-IP rate-limit gap (#4606). **Lesson for the report's methodology section:** the two-pass divergence is itself evidence the single-pass reviewer can miss a real High — the adjudication process caught it.

## D7-H1 — events_audit unbounded growth — UPHELD (High)

- **Claim:** `events_audit` (durable event-log / history fallback) has no retention/partition/archival; sibling ABAC audit table does.
- **Checked:** `internal/audit/retention.go` defines a full `RetentionWorker` (RunOnce/Start/Stop/HealthCheck) driven by a `PartitionManager` — but it serves the ABAC `access_audit_log`, not `events_audit`. Migrations touching `events_audit` (000009 create, 000011/000014/000017) create indexes (`subject_id`, `subject_ts`, `subject_pat`, `subject_js_seq`) but contain **no `PARTITION`, no retention/`DELETE`/detach**. No prod code references `events_audit` retention. Verified this session.
- **Verdict:** UPHELD. The machinery to fix it already exists (extend the RetentionWorker/PartitionManager to events_audit). High for long-lived deployments; not urgent at fresh-install scale.

## D8b-H1 — no player movement command — UPHELD (High), one residual to close in verification wave

- **Claim:** no registered command/RPC lets a player traverse an exit; `world.Service.MoveCharacter` has no production caller.
- **Checked this session:** `handleExitClick → sendCommand(direction)` (`web/.../terminal/+page.svelte:664`); full plugin command inventory (no go/move/walk/look/who); `probe MoveCharacter` → no non-test production caller outside `internal/world`; live: exit-click + typed-direction both no-op, `look`/`who` → "Unknown command."
- **Residual:** confirm no built-in dispatcher handler/alias in `internal/command/handlers` reaches `MoveCharacter` (the skeptic wave will hammer this). Provisionally UPHELD.

## D9c-H1 — nats-server v2.14.2 vulnerable — UPHELD (High)

- **Checked:** `go.mod:22` `github.com/nats-io/nats-server/v2 v2.14.2` (confirmed). The Connz/monitor endpoint (subject of GHSA-q59r-vq66-pxc2, fixed v2.14.3) is opt-in via `event_bus.monitor_port` (`internal/eventbus/config.go` `MonitorPort koanf:"monitor_port"`), and `site/src/content/docs/operating/how-to/operations.md` instructs operators to enable it for `nats stream info`/Prometheus — so the vulnerable path is reachable-by-runbook. govulncheck misses it (no Go-vulndb entry yet). Verdict: UPHELD; low urgency (Renovate PR already open), but a real network-facing-dep gap and a real govulncheck blind spot.

## D9a-H1 — >80% per-package coverage MUST is not CI-enforced — UPHELD (High)

- **Checked:** active branch-protection ruleset `protect-main` required checks = Build, Lint, Test, CodeRabbit, Integration Test, E2E Test — **no Codecov/coverage check** (verified via `gh api repos/holomush/holomush/rulesets`). `codecov.yml` has no per-package target breakout. So the `CLAUDE.md:187`/`testing.md:25` ">80% per-package" MUST has no enforcement teeth. The agent's live measurement (cmd/holomush 53.4%, #4782 merged at 54.6% patch) I did not independently re-measure, but the enforcement-gap — the load-bearing claim — is confirmed. Verdict: UPHELD. Severity note: this is an honesty/process gap, not a runtime defect; High is defensible because it's a documented MUST that main violates, but it competes with runtime-High findings for priority.

## D1-H1 — "event sourcing" is CRUD + post-commit events — partially self-verified (skeptic dispatched)

- **Checked:** `internal/world/service.go` `MoveObject` (`:571` DB write, `:587` emit — DB-then-emit, non-atomic, `move_succeeded:true` on emit failure). Consistent with the D1 agent's claim that state is direct-write CRUD and events are post-commit notifications, not the source of truth. Full multi-doc "event sourcing" claim list to be confirmed by the skeptic wave against the 4 cited docs.
