---
phase: 7
slug: event-model-bootstrap-decomposition
status: verified
# threats_open = count of OPEN threats at or above workflow.security_block_on severity (the blocking gate)
threats_open: 0
asvs_level: 1
created: 2026-07-18
---

# Phase 7 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Register origin: `register_authored_at_plan_time: true` (all eleven PLAN.md files carry a `<threat_model>` block). ASVS L1 · block_on: high. Verified at L1 grep-depth against the executed implementation (short-circuit rule: `threats_open: 0 AND register_authored_at_plan_time: true AND asvs_level == 1`). Corroborated by the already-completed gates: 12 rounds of cross-AI plan review (07-REVIEWS.md), a 3-iteration post-execution code-review-fix cycle that independently found and fixed 3 real concurrency races (07-REVIEW.md / 07-REVIEW-FIX.md — CR-01, CR-02, WR-01), gsd-verifier passed, crypto-reviewer READY, abac-reviewer READY, `task test:int` green.

---

## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| gateway (telnet/web) → core gRPC (07-01) | Untrusted client traffic is translated here; the client wrapper classifies server errors |
| gateway process → DB (07-01) | MUST NOT exist — gateway is protocol-translation only (gateway-boundary invariant) |
| publisher → JetStream / `events_audit` (07-02) | Event-type strings are the durable wire discriminator |
| gateway → user terminal (07-02) | Payload structs drive what a player is shown |
| player terminal → telnet command parser (07-03) | Untrusted input crosses here; `ParseCommand` is the grammar |
| gateway → session lease (07-03) | The refresh interval gates whether a live session is reaped |
| gateway process → domain/DB (07-04) | The closure boundary this plan enforces mechanically (permanent gate) |
| invariant registry → CI (07-04) | A fabricated binding makes CI assert nothing |
| session lifecycle → audit log (07-05) | `session_ended` is an audit-critical, non-skippable record |
| host emit → JetStream subject (07-05) | The subject determines who can subscribe (ABAC on the stream resource) |
| Lua plugin → SessionAdmin host capability (07-06) | A plugin-triggered broadcast reaches every active session |
| host broadcast → all sessions (07-06) | The system actor stamp is what marks a message as trustworthy host output |
| plugin → host emit, `event_emitter.go::Emit` (07-07) | Manifest gates (`actor_kinds_claimable`, `emits`, `crypto.emits`) fire here for BOTH runtimes |
| `eventbus.Event` → `history.PluginDowngradeFence` (07-07) | The unexported `auditRow`/`AuditRowOf` seam enforcing INV-CRYPTO-42/50 |
| host → plugin history response (07-07) | `hostv1.Event`'s 8 fields; the cursor is opaque |
| plugin → host history RPC (07-08) | An untrusted plugin supplies the cursor token it received |
| host cursor codec (07-08) | `internal/eventbus/cursor` — opaque to plugins by construction |
| process boot → serving (07-09) | Fail-closed posture: a failed subsystem MUST abort the boot, not degrade it |
| TLS CA generation → gameID (07-09) | The gameID is embedded in the CA certificate on first boot |
| boot gate → audit chain (07-10) | INV-CRYPTO-102: boot MUST refuse when a registered chain is broken |
| gRPC serving → audit projection (07-10) | Events served before the projection is up may not be durably audited |
| process → orchestrator shutdown (07-10) | An unbounded shutdown means the process never self-exits |
| subsystem acquisition → subsystem serving (07-11) | The Prepare/Activate barrier this plan makes structural |
| boot gate (audit chain) → any serving (07-11) | INV-CRYPTO-102 must hold before anything serves |
| failed boot → partial state (07-11) | A half-started server is a fail-open posture |

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-07-01 | Elevation of Privilege | `internal/telnet` transitive closure reaching `internal/store` | high | mitigate | Client extracted so `internal/grpc`'s 41-pkg domain closure leaves telnet's build graph; `go list -deps` acceptance criterion; permanentized by 07-04's closure gate | closed |
| T-07-02 | Information Disclosure | `TranslateSubscribeErr` error classification | medium | mitigate | Moved verbatim; `SESSION_NOT_FOUND` enumeration-safety (I-SEC-1) pinned by grep on exact oops code literal | closed |
| T-07-03 | Spoofing | `ClientConfig` TLS/credentials selection | low | accept | Moved verbatim; `crypto/tls` + credentials selection byte-identical, no new attack surface | closed |
| T-07-04 | Tampering | npm/pip/cargo installs (07-01) | high | N/A | Zero external packages installed; `git diff go.mod` confirms no new dependency | closed |
| T-07-05 | Tampering | Event-type wire strings during the move | high | mitigate | `eventvocab_test.go` pins every string literal; grep criterion proves no old reference survives; `task test:int` re-asserts against the real audit projection | closed |
| T-07-06 | Tampering | `ValidatePayload`/`MaxPayloadSize` | high | mitigate | Moved verbatim; boundary test asserts exact 64 KiB bound (accept at limit, reject at +1) | closed |
| T-07-07 | Information Disclosure | `internal/command` gaining an `internal/eventbus` reach | medium | mitigate | `eventvocab` is a dependency-free leaf; `go list -deps ./internal/command` asserted to still exclude `internal/eventbus` | closed |
| T-07-08 | Repudiation | Payload JSON tag drift | medium | mitigate | Structs copied verbatim; round-trip test asserts literal tag names | closed |
| T-07-09 | Tampering | npm/pip/cargo installs (07-02) | high | N/A | Zero external packages installed | closed |
| T-07-10 | Tampering | `ParseCommand` grammar | high | mitigate | Moved verbatim; lowercase + inner-spacing pin test added; existing `command_test.go` cases move intact | closed |
| T-07-11 | Denial of Service | `DefaultRefreshInterval` value drift | high | mitigate | Pinned `== 15 * time.Second`; asserts exactly one definition exists repo-wide | closed |
| T-07-12 | Spoofing | ULID monotonicity/uniqueness | high | mitigate | `crypto/rand` retained; single shared `ulid.Monotonic` entropy site asserted repo-wide; monotonicity tests moved intact | closed |
| T-07-13 | Elevation of Privilege | Gateway retaining a non-leaf import | medium | mitigate | `go list -deps` asserts gateway no longer contains the source package; permanentized by 07-04 | closed |
| T-07-14 | Tampering | npm/pip/cargo installs (07-03) | high | N/A | Zero external packages installed | closed |
| T-07-15 | Elevation of Privilege | gateway→DB closure (07-04) | high | mitigate | Zero-closure gate is the permanent enforcement mechanism; positive control confirms `internal/grpc`'s 41-pkg closure is correctly detected | closed |
| T-07-16 | Repudiation | Fabricated invariant binding | high | mitigate | `TestBoundInvariantsAreGenuinelyAsserted` required green; no `// Verifies:` on a non-asserting test | closed |
| T-07-17 | Tampering | `coreOnlyFiles` allowlist growth | medium | mitigate | Map-doesn't-grow assertion in the closure gate test | closed |
| T-07-18 | Repudiation | INV-GW→EVENTBUS invariant reversion | medium | mitigate | Regex-fixture byte-unchanged assertion | closed |
| T-07-19 | Tampering | npm/pip/cargo installs (07-04) | high | N/A | Zero external packages installed | closed |
| T-07-20 | Repudiation | `EmitSessionEnded` context discipline | high | mitigate | Moved verbatim; migrated test suite intact | closed |
| T-07-21 | Spoofing | Cause-dependent actor selection | high | mitigate | Grep criterion + migrated tests confirm actor-selection logic unchanged | closed |
| T-07-22 | Information Disclosure | Subject construction drift | high | mitigate | `task test:int` re-assertion against real audit projection required | closed |
| T-07-23 | Denial of Service | Nil Publisher deferred failure | medium | mitigate | Dedicated panic/nil-guard test | closed |
| T-07-24 | Elevation of Privilege | auth↔eventbus import cycle | high | mitigate | `go list -deps ./internal/auth` asserted to exclude both sides of the cycle | closed |
| T-07-25 | Tampering | npm/pip/cargo installs (07-05) | high | N/A | Zero external packages installed | closed |
| T-07-26 | Spoofing | System-actor stamping | high | mitigate | Pinned by test; re-asserted on the Lua emit path | closed |
| T-07-27 | Elevation of Privilege | Binary plugins gaining SessionAdmin | high | mitigate | Out of scope per plugin-runtime-symmetry rule (permitted transport asymmetry); documented | closed |
| T-07-28 | Tampering | Broadcast subject drift | medium | mitigate | `SystemBroadcastSubject` literal pinned | closed |
| T-07-29 | Information Disclosure | `internal/command`→eventbus reach (07-06) | medium | mitigate | `go list -deps` check | closed |
| T-07-30 | Tampering | npm/pip/cargo installs (07-06) | high | N/A | Zero external packages installed | closed |
| T-07-31 | Spoofing | Actor-kind forgery | high | mitigate | crypto e2e/metadata_only integration suites required green | closed |
| T-07-32 | Elevation of Privilege | Plugin crypto downgrade | high | mitigate | `PluginDowngradeFence` tests required green | closed |
| T-07-33 | Information Disclosure | Sequence-cursor leak to plugins | high | mitigate | No raw `seq` field in `stream.proto`; cursor stays opaque by construction | closed |
| T-07-34 | Elevation of Privilege | Lua/binary emit-gate asymmetry | high | mitigate | Common gate stays at `event_emitter.go::Emit`; pluginparity suite unchanged | closed |
| T-07-35 | Repudiation | Wire/audit byte drift | high | mitigate | `AppSchemaVersion` held at 1; byte-equality suites required green | closed |
| T-07-36 | Repudiation | Stale rule documentation | medium | mitigate | CLAUDE.md + `.claude/rules/` amended in the same change | closed |
| T-07-37 | Tampering | npm/pip/cargo installs (07-07) | high | N/A | Zero external packages installed | closed |
| T-07-38 | Information Disclosure | Plugin history sequence-cursor leak | high | mitigate | `hostv1.Event` 8-field census pinned | closed |
| T-07-39 | Repudiation | Plugin audit skip bug | high | mitigate | Spec A/B tests are themselves the fix; required green | closed |
| T-07-40 | Elevation of Privilege | ABAC bypass via threaded parameter | high | mitigate | `streamauth_test.go` deny-path assertions unchanged | closed |
| T-07-41 | Elevation of Privilege | Lua/binary history-RPC asymmetry | high | mitigate | Lockstep signature change across both runtimes; structural typing | closed |
| T-07-42 | Denial of Service | Legacy `Seq==0` mishandling | medium | mitigate | Tail-read policy test | closed |
| T-07-43 | Tampering | npm/pip/cargo installs (07-08) | high | N/A | Zero external packages installed | closed |
| T-07-44 | Denial of Service | Boot panic → crash-loop under KEK-wired config | high | mitigate | Mandatory KEK-wired full-boot E2E gate at each commit boundary in 07-09 | closed |
| T-07-45 | Tampering | TLS CA generation binds wrong gameID | high | mitigate | `TLSSubsystem.DependsOn` returns `[SubsystemDatabase]` — verified live in current tree | closed |
| T-07-46 | Elevation of Privilege | Serving with unresolved ABAC/hasher dependency | high | mitigate | Accessor panic guards ("called before Prepare") preserved through the Start-split | closed |
| T-07-47 | Tampering | 16-arg positional constructor mis-ordering | medium | mitigate | Converted to named-field struct construction | closed |
| T-07-48 | Spoofing | Second ordering authority competing with topoSort | medium | mitigate | No parallel latch; `topoSort` remains sole ordering authority (D-10) | closed |
| T-07-49 | Tampering | npm/pip/cargo installs (07-09) | high | N/A | Zero external packages installed | closed |
| T-07-50 | Repudiation | gRPC serving before audit projection is up | high | mitigate | `grpcSubsystem.DependsOn()` includes `SubsystemAuditProjection` — verified live in current tree | closed |
| T-07-51 | Tampering | Domain surfaces touched pre-chain-proof | medium | mitigate | Same `DependsOn` edge covers this path; residuals documented in-plan | closed |
| T-07-52 | Denial of Service | Unbounded `StopAll` | high | mitigate | Context-deadline test on shutdown | closed |
| T-07-53 | Repudiation | Silent abandoned-subsystem list on shutdown | medium | mitigate | `StopAll` logs skipped subsystem IDs at error level | closed |
| T-07-54 | Spoofing | Stale ordering-guarantee comments | medium | mitigate | Stale comments deleted, replaced by real `DependsOn` edges | closed |
| T-07-55 | Tampering | npm/pip/cargo installs (07-10) | high | N/A | Zero external packages installed | closed |
| T-07-56 | Repudiation | Surface serves before an undeclared dependency is up | high | mitigate | `orchestrator.go` `StartAll` runs the full `preparedOrder` sweep before any Activate — verified live | closed |
| T-07-57 | Tampering | Chain verifier gate runs after serving starts | high | mitigate | `VerifierSubsystem.Prepare` runs the chain walk (not Activate) — verified live | closed |
| T-07-58 | Denial of Service | Leaked resources after mid-Activate failure | high | mitigate | `StopAll` walks `preparedOrder` (superset of `activatedOrder`) in reverse on rollback — verified live | closed |
| T-07-59 | Denial of Service | Duplicate poller launched under rollback/retry | medium | mitigate | `access/setup/subsystem.go` idempotency guard present — verified live | closed |
| T-07-60 | Denial of Service | Rollback itself hangs | high | mitigate | Deadline handling retained through the new Prepare/Activate/rollback paths | closed |
| T-07-61 | Tampering | npm/pip/cargo installs (07-11) | high | N/A | Zero external packages installed | closed |
| T-07-62 | Elevation of Privilege | Future subsystem escapes the two-sweep barrier via Prepare | high | mitigate | Documented contract + 17-row Activate audit in SUMMARY.md; review-enforced residual | closed |
| T-07-63 | Repudiation | Lua-runtime plugin left on a broken cursor (history) | high | mitigate | `stdlib_focus.go` has no hardcoded `Seq: 0` at either encode site — verified live | closed |
| T-07-64 | Spoofing | Unqualified subject reaching `Publish` | high | mitigate | Exact-literal qualified-Subject criterion | closed |
| T-07-65 | Denial of Service | Unbootable dependency cycle in production subsystem graph | high | mitigate | `cmd/holomush/core_topo_order_test.go` exercises the real production graph through `topoSort` — verified live | closed |
| T-07-66a | Repudiation | Page-advance uses the wrong anchor event (07-08) | high | mitigate | `hostcap/servers.go:900-906` + `stdlib_focus.go:452-462` both index the oldest `[0]` element as backward anchor | closed |
| T-07-66b | Elevation of Privilege | gRPC/admin.sock bind before chain-verification walk completes (07-09) | medium | mitigate | `grpcSubsystem.DependsOn()` includes `SubsystemCryptoChainVerifier` — verified live | closed |
| T-07-67 | Denial of Service | Disabled-mode admin socket nil-panic | high | mitigate | `s.server == nil` guard present in `Activate` — verified live | closed |
| T-07-68 | Denial of Service | gRPC session reapers never launch | high | mitigate | Reaper construct-in-Prepare / launch-in-Activate split present in `sub_grpc.go` — verified live | closed |
| T-07-69 | Elevation of Privilege | Plugin subsystem partial-Prepare resource leak | high | mitigate | `cleanupOnError` closes both `binaryHost` and `luaHost` on every pre-manager error path — verified live | closed |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above workflow.security_block_on (high) count toward threats_open*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

**Note on IDs:** the source plans independently reused `T-07-66` for two distinct threats across different plans (07-08's page-advance anchor and 07-09's grpc/admin.sock bind-before-verification) — a cross-round numbering collision in the plan authoring process, not a duplicate threat. Disambiguated here as `T-07-66a` (07-08) / `T-07-66b` (07-09). A separate `T-07-44` collision was already resolved in-plan by the authors via renumbering to `T-07-63`/`T-07-64`, both preserved as distinct entries above alongside `T-07-44` itself (a third, unrelated threat that happens to share the same numeric range).

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| R-07-01 | T-07-03 | `ClientConfig` TLS/credential selection logic moved verbatim with no behavior change; low severity, no new attack surface introduced by a pure code-relocation. | Sean | 2026-07-18 |
| R-07-02 | T-07-27 | Binary plugins do not gain the SessionAdmin host capability in this phase — this is the permitted Lua/binary transport asymmetry documented in `.claude/rules/plugin-runtime-symmetry.md` (same policy chokepoint, different transport), not a privilege gradient. | Sean | 2026-07-18 |

*Accepted risks do not resurface in future audit runs.*

---

## Operator Follow-ups (ship-time, non-blocking)

These are non-blocking hygiene notes from crypto-reviewer and abac-reviewer, not open threats — each is either cosmetic or a latent/defense-in-depth suggestion with no demonstrated attack path today.

| Ref | Action | Tracked as |
|-----|--------|------------|
| — | `internal/eventbus/crypto/dek/sweep.go:32-33,84-87` doc comments say "resolved once at the top of Start" — stale after the Prepare/Activate split; cosmetic only. | crypto-reviewer NIT |
| — | `CheckpointSweepSubsystem` has no mutex guarding `s.done`/`s.cancel` against a hypothetical concurrent double-`Stop` (parity with `audit.Subsystem`'s defensive mutex); latent, not reachable under the orchestrator's serialized two-sweep contract. | crypto-reviewer non-blocking |
| — | `internal/access/setup/subsystem.go` `Activate` has no explicit `s.stack == nil` guard (relies on orchestrator ordering); would fail-closed via panic, not fail-open, if ever invoked out of order — not reachable today. | abac-reviewer LOW |
| — | Duplicate `PlayerRepository` construction in `crypto_operator_validation.go` and `subsystem.go` Prepare — trivial allocation, no security impact. | abac-reviewer LOW |
| — | `subsystem.go` discards the crypto-operator validator's error via `//nolint:errcheck` — accurate today (validator always returns nil per its documented lax+warn contract); becomes load-bearing once sub-epic D adds a fail-closed error path — already tracked by the existing in-code comment. | abac-reviewer LOW |

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-07-18 | 70 | 70 | 0 | gsd-secure-phase (L1, register_authored_at_plan_time) + crypto-reviewer (READY) + abac-reviewer (READY) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-07-18
