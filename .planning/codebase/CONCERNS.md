# Codebase Concerns

**Analysis Date:** 2026-07-08

## Overview

HoloMUSH shows unusually mature process discipline for its scale (invariant registry, quarantine bijection meta-test, gRPC error-leak rules, plugin runtime-symmetry invariant). Most "concerns" below are **documented and actively managed** debt rather than hidden rot. Genuine open risk is concentrated in three places: (1) the large `binding: pending` invariant surface (a coverage gap, not a known bug), (2) the still-unimplemented external/clustered NATS deployment mode, and (3) the sheer size of a handful of core files that raises review/maintenance cost.

## Tech Debt

**In-code TODO/FIXME markers — small and current, not a debt pile:**

- Total real `// TODO` / `// FIXME` comments in `internal/`, `plugins/`, `cmd/` (excluding test-name false positives like `TestQueryStreamHistory*`): ~9 in production code, plus 2 in `web/e2e/terminal.spec.ts`. This is a small, actively-triaged set for a monorepo this size — most reference a bead ID or phase marker rather than being orphaned.
- Notable ones:
  - `plugins/core-scenes/commands.go:890` — mixed-render branch (focused + skipped both non-empty) not yet handled; scoped to Phase 6 §7.4.
  - `cmd/holomush/gateway.go:258` — telnet handler is still a placeholder pending gRPC-based rewrite (`TODO(grpc-telnet)`).
  - `internal/auth/reset_service.go:213` — session invalidation failure is currently non-fatal; TODO to consider making it mandatory. Worth revisiting given security sensitivity of auth reset flows.
  - `internal/grpc/auth_handlers.go:253` — `connID` not threaded through `SelectCharacterResponse` proto (field doesn't exist yet).
  - `internal/eventbus/authguard/audit/emitter.go:216,246` — two `TODO(metrics)` for missing failure-counter metrics on the audit drain/marshal paths. Since this is the crypto-adjacent audit emitter, a silent failure here currently has no counter to alert on — an observability gap on a security-relevant path.
  - `internal/eventbus/audit/subsystem.go:59` — `TODO (Phase B): wire a DLQ` for messages that exhaust `MaxDeliver`. Until this lands, audit messages that repeatedly fail delivery have no dead-letter capture path — check `internal/eventbus/audit/subsystem.go` before relying on audit completeness guarantees under sustained delivery failure.
  - `internal/cluster/pill.go:75` — open span flushing not yet bounded by `pillFlushTimeout` (`TODO(T11+)`).
  - `web/e2e/terminal.spec.ts:241,563` — two E2E assertions disabled pending F7 schema changes (EventCursors JSONB removal); tracked against `holomush-1tvn.13`/`.14`.

**`nolint` directive volume:**

- 852 `//nolint` occurrences across `internal/`, `plugins/`, `cmd/`. Per `.claude/rules/grpc-errors.md` and root `CLAUDE.md`, these are required to be **line-scoped with a comment**, not config-wide suppressions — spot checks (`internal/web/handler.go:381,418,460,484`, `//nolint:wrapcheck // gRPC status errors pass through as-is`) confirm the convention is followed rather than blanket-ignored. This volume reflects a codebase with intentional escape hatches at gRPC/status boundaries (`wrapcheck` false-positives on intentional opaque pass-through), not undisciplined suppression — but it is still a large surface; any PR touching a file with a `nolint` cluster should re-verify the directive still applies to the exact line, since line-scoped directives silently stop protecting code if lines shift without moving the comment.

**Largest files (maintenance/review cost, not necessarily bugs):**

| File | Lines |
|------|-------|
| `internal/grpc/scenemocks/mock_SceneServiceClient.go` | 2041 (generated mock) |
| `plugins/core-scenes/service.go` | 1913 |
| `plugins/core-scenes/store.go` | 1846 |
| `internal/plugin/manager.go` | 1838 |
| `internal/session/mocks/mock_Store.go` | 1826 (generated mock) |
| `internal/grpc/server.go` | 1807 |
| `plugins/core-scenes/commands.go` | 1739 |
| `internal/plugin/goplugin/host.go` | 1573 |
| `cmd/holomush/core.go` | 1461 |
| `internal/testsupport/integrationtest/harness.go` | 1439 |
| `internal/store/session_store.go` | 1338 |
| `internal/telnet/gateway_handler.go` | 1269 |
| `internal/plugin/hostcap/servers.go` | 1242 |
| `internal/world/service.go` | 1104 |

Excluding generated mocks, `plugins/core-scenes/*` (service.go, store.go, commands.go) and `internal/grpc/server.go` / `internal/plugin/manager.go` / `internal/plugin/goplugin/host.go` are the genuine hand-maintained hotspots — these are the files where a change has the widest blast radius and the highest review burden. `core-scenes` in particular is the single largest, most complex in-tree plugin (participants, IC/OOC rendering, focus redirects, crypto-emits, audit) and any change there should budget for wide test-suite impact.

## Known Bugs

No open, reproducible bugs were surfaced by this static sweep (no `BUG(` markers found in the codebase, and known-flaky tests are all classified as infra flakiness, not logic bugs — see Test Coverage Gaps below). This does not mean zero bugs exist — it means none are self-documented in code comments; check `bd list --status=open -t bug` for the live tracked set, which is out of scope for a static codebase scan.

## Security Considerations

**Crypto subsystem (`internal/eventbus/crypto/`) — high sensitivity, heavily invariant-gated:**

- This is by far the most invariant-dense area of the codebase: of 334 total registered invariants, 103 are `INV-CRYPTO-*`, and the overwhelming majority of those are still `binding: pending` (no test yet proves them — see Test Coverage Gaps). Read `docs/architecture/invariants.yaml` for the exact list before touching `internal/eventbus/crypto/`, `internal/eventbus/codec/`, `internal/eventbus/history/cold_postgres.go`, `internal/plugin/event_emitter.go::Emit`, or `internal/eventbus/audit/projection.go` — the `crypto-reviewer` agent gate (`.claude/rules` / root CLAUDE.md pre-push table) exists specifically because this surface has previously needed a dedicated reviewer distinct from the general code-reviewer.
- `crypto.emits` manifest declarations (plugin.yaml) are the single enforcement point for "this event type must be encrypted" — a plugin author who forgets to declare a sensitive event type in `crypto.emits` produces a silent-plaintext bug, not a build failure, unless the emit-time fence (INV-6/INV-7, per invariants.yaml Phase 3d note) catches it. This is a real "one manifest line short of a data leak" shape, mitigated by the fence but worth flagging to anyone adding new plugin event types.
- `dek.Material` handling has an explicit invariant (`binding: pending`) that it MUST NOT be passed to `io.Writer`, JSON/gob/proto marshalers, `slog`/`log`, or `fmt.Sprint/Print/Errorf` — this is exactly the kind of guarantee that's easy to violate accidentally in a debug log line and currently has no test binding it.

**ABAC default-deny integrity (`internal/access/`):**

- The `AttributeProvider` optional-attribute-omission convention (`.claude/rules/abac-providers.md`) is the load-bearing mechanism protecting fail-closed semantics — a provider that emits an empty-string sentinel instead of omitting the key creates a fail-**open** match (documented incident class: `holomush-9gtl`). Any new `AttributeProvider` added under `internal/access/policy/attribute/` is a place where this specific mistake is easy to reintroduce; there is no compiler-level protection against it, only convention + code review.
- `INV-ACCESS-*` has 8 pending bindings — a modest number relative to CRYPTO/SCENE, suggesting ABAC test coverage is comparatively further along, but still not fully proven.
- The runtime `AuthGuard` invariant "MUST NEVER return PERMIT for a subject of kind operator" (crypto-adjacent, `binding: pending`) is a fail-safe guarantee with no test yet — a regression here would let an operator subject silently read plaintext it shouldn't.

**gRPC error-leak discipline (`.claude/rules/grpc-errors.md`):**

- Spot-checked `internal/grpc/query_stream_history.go:432-436` — these `status.Errorf(codes.X, "%v", err)` calls wrap known sentinel errors (`eventbus.ErrCursorInvalid/ErrCursorStale/ErrCursorLag`), which are safe, client-relevant validation errors, not internal error leakage. This is compliant with the rule (leak concern is specifically about `codes.Internal` wrapping arbitrary internal `err` values). No violations of the "never leak inner errors past trust boundaries" rule were found in this sweep — the codebase appears to follow its own documented convention here.

**Plugin runtime trust symmetry (`internal/plugin/`, `pkg/plugin/`, `plugins/`):**

- The `plugin-runtime-symmetry.md` rule documents a real, previously-exploitable class of bug (asymmetric trust between Lua and binary plugin runtimes) and pins the enforcement chokepoint at `internal/plugin/event_emitter.go::Emit`. Any new host-side trust check on plugins that does NOT route through this shared path (or an equivalent shared chokepoint) reintroduces the privilege-gradient risk this rule exists to prevent. Watch for new `PluginHostService` RPCs added to only one runtime's SDK (Go or Lua hostfunc) — the rule explicitly calls this out as the most common way parity breaks.

## Performance Bottlenecks

No specific performance bottleneck was surfaced by static analysis in this sweep. The load-testing harness (referenced in `invariants.yaml` INV-TELEMETRY-adjacent pending entries — "Load harness drives the web tier over the Connect protocol", "SLO thresholds gate against `.benchmarks/load-baseline.json`") indicates the project has infrastructure for catching regressions but the harness itself is explicitly **not wired into `task pr-prep`** (fast lane) per a pending invariant — meaning a performance regression will not be caught before merge unless someone runs the load suite manually or in a separate CI job.

## Fragile Areas

**Invariant-dense, low-binding scopes are the highest-fragility surface:**

| Scope | Pending | Bound (approx, registry-wide) |
|-------|---------|-------------------------------|
| INV-CRYPTO | 103 | — |
| INV-SCENE | 60 | — |
| INV-PLUGIN | 29 | — |
| INV-EVENTBUS | 21 | — |
| INV-TELEMETRY | 9 | — |
| INV-STORE | 9 | — |
| INV-ACCESS | 8 | — |
| INV-SESSION | 4 | — |
| INV-PRIVACY | 4 | — |
| INV-COMMAND | 3 | — |
| INV-CLUSTER | 3 | — |
| INV-PRESENCE | 2 | — |

(334 total registered invariants; 259 `binding: pending`, 88 `binding: bound` — the totals overlap because some ids carry legacy aliases, so read this as directional, not exact-sum.) CRYPTO and SCENE dominate the pending list by a wide margin, meaning the two most architecturally complex subsystems (payload encryption and the `core-scenes` plugin) currently have the largest ratio of "guarantee documented, not yet proven by a test." This is the single most systemic concern in the codebase: a regression in either subsystem that violates a pending invariant would not be caught by CI today.

**Quarantined tests — all classified as infra flakiness, not logic bugs:**
`test/quarantine.yaml` currently lists 4 entries, all under active beads:

- `TestProjectionResumesAfterRestart` (`holomush-q55b`) — consumer-info eventual-consistency race on JetStream restart.
- `TestProjectionDrainsPublishedMessageToAuditTable` (`holomush-1nl7`) — `AwaitDrained` cold-start race.
- `ConcurrentUp allows at least one concurrent Up to succeed` (`holomush-pqzv`) — testcontainers Docker port-map timeout under load.
- `admin_read_stream F-E12 chain verification (surgical step skip)` (`holomush-7b9n`) — `operator_read_completed` audit row times out under load.

All four are timing/infra races (JetStream eventual consistency, testcontainer startup, load-induced timeouts) rather than logic defects, consistent with the project's stated quarantine policy ("quarantine is for flakiness with an open bead; fix known-cause failures, don't quarantine them"). This is a small, well-governed quarantine list for a project of this size — not a red flag on its own, but the eventual-consistency races (`q55b`, `1nl7`) both touch the audit/projection path that also has pending CRYPTO invariants, compounding the "audit completeness under restart/load" uncertainty.

## Scaling Limits

**External/clustered NATS is explicitly unimplemented** (tracked epic `holomush-s5ts`):

- Confirmed via `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md` and `2026-05-03-event-payload-crypto-phase3d-grounding.md`: HoloMUSH currently runs **only** embedded in-process NATS JetStream (`MemoryStorage`); "externally clustered later" is a stated future direction with a clean seam already designed (per-cluster subject prefixing, `internal.<cluster_id>.cache_invalidate.*`, N-of-N replica ack protocol for KEK/DEK rotation — see `INV-28`/`INV-29`/`INV-54`/`INV-59` in the crypto design spec) but **not built**. This means: (1) HoloMUSH today cannot horizontally scale the event bus across multiple server processes/nodes; (2) the crypto rotation/invalidation protocol described for cluster mode is speculative/未-verified against a real multi-node deployment. Anyone planning a multi-node HoloMUSH deployment should treat this as a hard current ceiling, not a config flag.
- `test/quarantine.yaml`'s `ConcurrentUp` entry (testcontainer port-map timeout "under load") hints that even single-node integration testing is somewhat resource-sensitive in CI, a secondary signal that scale headroom in the test infra itself is thin.

## Dependencies at Risk

No dependency-specific risk (deprecated package, abandoned upstream, license conflict) was surfaced by this codebase-only sweep — a proper audit would need `go list -m -u all` / `govulncheck` output, which is outside static-file analysis scope. Recommend running `codebase-cleanup:deps-audit` skill for that dimension specifically if needed.

## Missing Critical Features

- **External/clustered NATS deployment** (`holomush-s5ts`) — see Scaling Limits above. Blocks any horizontally-scaled or highly-available server-process deployment topology.
- **Audit dead-letter queue** (`internal/eventbus/audit/subsystem.go:59`, `TODO (Phase B)`) — messages that exhaust `MaxDeliver` on the audit projection path currently have no capture mechanism; a sustained downstream (Postgres) outage could silently drop audit records past the retry ceiling rather than parking them for later replay.
- **Telnet handler is still a placeholder** (`cmd/holomush/gateway.go:258`) pending the gRPC-based rewrite — the dual-protocol (telnet + web) promise in the architecture overview is not yet at parity for this specific handler path.

## Test Coverage Gaps

**The single largest, most systemic gap: `binding: pending` invariants (259 of 334).**

- What's not tested: any invariant marked `binding: pending` in `docs/architecture/invariants.yaml` has **no test currently proving it** — per `.claude/rules/invariants.md`, this is a tolerated ratchet state (decision `holomush-hz0v4.10`), not a blocker, but it is a real, enumerable coverage gap. The heaviest concentrations are:
  - **Crypto** (`INV-CRYPTO-*`, 103 pending) — DEK/KEK rotation correctness, envelope byte-equality across the emit→audit→cold-read path, plugin read-back decrypt gating, operator-read audit-before-data ordering, and downgrade-fence refusal semantics are all still unproven by test. This is the single highest-priority backlog area given the security sensitivity of the subsystem.
  - **Scene** (`INV-SCENE-*`, 60 pending) — the `core-scenes` plugin's ABAC/capability/audit invariants are unproven at the same rate as crypto, compounding the file-size fragility noted above (`plugins/core-scenes/service.go`/`store.go`/`commands.go`).
  - **Plugin** (`INV-PLUGIN-*`, 29 pending) and **EventBus** (`INV-EVENTBUS-*`, 21 pending) — manifest-gate and emit/subscribe ordering guarantees.
  - Files: `docs/architecture/invariants.yaml` (source of truth, 4774 lines); regenerate the human-readable view with `go run ./cmd/inv-render` → `docs/architecture/invariants.md`.
- Priority: **High** for INV-CRYPTO and INV-SCENE given both the security/data-integrity stakes and the sheer volume; **Medium** for the rest.
- This gap is explicitly **not** hidden — the registry surfaces it by design, and per-scope backfill is tracked under epic `holomush-hz0v4`. The concern here is the raw size of the gap today, not a lack of process to close it.

**Audit-metrics observability gap:** `internal/eventbus/authguard/audit/emitter.go:216,246` — no counters yet exist for `authguard_audit_drain_failed_total` / `authguard_audit_marshal_failed_total`. A silent failure on this path (audit drain or marshal failure) currently has no alertable signal.

---

*Concerns audit: 2026-07-08*
