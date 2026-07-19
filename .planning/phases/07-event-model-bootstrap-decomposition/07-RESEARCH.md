# Phase 7: Event-Model & Bootstrap Decomposition - Research

**Researched:** 2026-07-15
**Domain:** Go cross-package type refactor (event model collapse), process lifecycle/bootstrap, import-boundary enforcement
**Confidence:** HIGH (all load-bearing claims verified against live source in this worktree at `be030a368`)

## Summary

This is a **behavior-preserving refactor phase** with one deliberate behavior change (D-07's
pagination fix). It has no external dependencies, no new libraries, and no research-from-the-web
component — the entire risk surface is *this codebase's own import graph and type topology*.
Accordingly this research is a **grounding audit**, not a technology survey.

The audit **confirmed** the great majority of CONTEXT.md's 18 decisions against live source, and
confirmed all five documented traps verbatim (D-18's stale INV-GW-1 recommendation, the
eventbus→core import edge, the `auditRow` package-private seam, the ARCH-04↔ARCH-05 vocabulary
collision, and the Seq never-serialize rule). It also surfaced **nine drift findings**, two of
which are structural and change what the plan must do:

1. **D-04 as written does not compile.** `internal/auth` is a `core.Engine` consumer CONTEXT.md
   never lists, and `internal/eventbus`'s transitive closure provably contains `internal/auth`.
   The proposed `internal/presence` package (which imports eventbus) would therefore create
   `auth → presence → eventbus → … → auth` — an import cycle. **D-04's explicit "no cycle"
   claim is false.**
2. **D-15/D-16's stated rationale for forbidding `internal/core` and `internal/session` is
   factually wrong.** Both are *already* dependency-free leaves (`go list -deps` → zero internal
   packages). Only `internal/grpc` (41 internal packages, reaching world/access/command/store)
   matches the "transitively reaches DB/domain" rationale. The **decision** stands (it is locked
   and defensible on drift-prevention grounds); the **justification** must not be repeated as
   written or the plan ships a fabricated claim.

**Primary recommendation:** Resolve the D-04 cycle with the repo's own **consumer-defined
interface** pattern — the one D-02 already blesses. `internal/auth` already receives the engine
through an injection seam (`WithGameSessionFanout(engine *core.Engine, …)`, `auth_service.go:39`);
changing that parameter to a locally-declared one-method interface satisfied by `*presence.Emitter`
breaks the cycle with zero new packages and zero new concepts. Sequence ARCH-05's
`TranslateSubscribeErr` extraction **first** — it is the single change carrying ~85% of ARCH-05's
real value and it is independent of everything else.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Event wire type + audit row seam | `internal/eventbus` | — | Owns the unexported `auditRow` + `AuditRowOf` package-private accessor feeding INV-CRYPTO-42/50; type cannot leave (D-01) |
| Event-type vocabulary | new dependency-free leaf | — | Must be importable by BOTH eventbus and gateway; resolves the ARCH-04↔ARCH-05 collision (D-05) |
| Presence emission (arrive/leave/end-session) | new `internal/presence` | — | Imports eventbus; consumed via **interfaces**, not direct import, by `internal/auth` (cycle, see FINDING-1) |
| Broadcast intent | one builder + consumer-defined interfaces | `internal/command`, `hostcap` | `command` must NOT import eventbus (D-01/D-02) |
| Subsystem start/stop ordering | `lifecycle.Orchestrator` (`topoSort`) | — | Sole ordering authority; no second gate (D-10) |
| Protocol translation only | `internal/web`, `internal/telnet` | — | Leaf-only imports (D-15) |
| Host-internal seq cursor | `internal/eventbus/cursor` + hostcap | — | Never crosses the plugin proto boundary (D-08) |

## User Constraints (from CONTEXT.md)

### Locked Decisions

All 18 decisions D-01..D-18 in
`.planning/phases/07-event-model-bootstrap-decomposition/07-CONTEXT.md` are locked and are
**not** re-litigated by this research. Summarized for the planner:

- **D-01** `eventbus.Event` wins in place; `core.Event`/`core.NewEvent`/`core.EventAppender`
  deleted. `internal/command` must NOT import `internal/eventbus`.
- **D-02** One broadcast builder; consumer-defined interfaces; no shared port package.
  `Services.Events()` goes away.
- **D-03** `core.Engine` **moves** out of `internal/core` (not a port) — the eventbus→core edge
  makes staying an import cycle.
- **D-04** New `internal/presence`; type renamed `presence.Emitter`; methods renamed.
- **D-05** Event-type vocabulary → dependency-free leaf importable by eventbus AND gateway.
- **D-06** Three duplicate actor bridges collapse with `core.Actor`.
- **D-07** Take the host-internal Seq fix; ships with a concurrent-publisher skip/repeat
  regression test. This IS a behavior change.
- **D-08** Do NOT expose `Seq` to plugins.
- **D-09** Full two-phase: zero eager starts; constructors take handles/providers.
- **D-10** No countdown latch; `topoSort`+`DependsOn` is the ordering authority; reuse
  `ReadinessRegistry` if a gate is wanted.
- **D-11** Split `Subsystem.Start` into Prepare/Activate.
- **D-12** Two waves (A: handles/eager-start removal; B: Prepare/Activate), each independently
  green and reviewable.
- **D-13** Planner MUST settle: (1) two-phase rollback semantics; (2) the
  `Start`-MUST-be-idempotent contract's fate.
- **D-14** Ride-alongs in scope: MEDIUM-11, LOW-7, LOW-8, phantom `SubsystemTLS`.
- **D-15** Leaf-only principle; `internal/core`, `internal/session`, `internal/grpc` forbidden
  wholesale; no per-symbol allow-list.
- **D-16** The violation inventory (verified below).
- **D-17** Extend the existing AST test; amend + bind `INV-EVENTBUS-1`.
- **D-18** ⚠️ Do NOT follow LOW-6's "rename to INV-GW-1" — it reverses a completed migration.

### Claude's Discretion

- Final verb set for `presence.Emitter`'s renamed methods.
- `ReplayTail`'s new signature shape (param vs cursor struct).
- Destination package for extracted `TranslateSubscribeErr` and the gateway leaves.
- Package placement/naming of the event-type vocabulary leaf (D-05) and broadcast builder (D-02).
- Internal wave decomposition within Wave A / Wave B.
- Whether MEDIUM-11 lands as a real `DependsOn` edge or comment-deletion + topo-order pin test.
- PR/delivery shape.

### Deferred Ideas (OUT OF SCOPE)

- `cmd/holomush`'s `coreOnlyFiles` allowlist (~30 entries).
- Exposing `Seq` to plugins.
- MEDIUM-4's full bidirectional-coupling unwind (Phase 8).
- `ReadinessRegistry.AllReady` vacuous-truth fail-open.
- Doc drift in `.planning/PROJECT.md` + `.planning/codebase/ARCHITECTURE.md` (file a `gh issue`).
- Fuller `internal/core` decomposition (Phase 8/999.9).

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| ARCH-03 | Process bootstrap migrated onto `lifecycle.Orchestrator`, unifying start/stop ordering | §Bootstrap census verifies all 5 eager starts, the phantom `SubsystemTLS`, the 16-param `productionSubsystems`, the MEDIUM-11 comment location, `grpcSubsystem.DependsOn`'s missing AuditProjection edge, and the count-test cascade |
| ARCH-04 | Parallel `core.Event`/`eventbus.Event` models collapsed to one representation | §Event-model census (complete consumer inventory), FINDING-1 (the cycle that blocks D-04), FINDING-4 (D-07's undercounted signature blast radius), the verified `auditRow` seam and eventbus→core edge |
| ARCH-05 | Gateway holds only protocol-translation dependencies | §Gateway boundary — full live violation inventory, quantified closure sizes (telnet 47 → target ~6), FINDING-2 (the false rationale), FINDING-3 (direct-vs-transitive gate gap), verified zero blast radius in `cmd/holomush` |

## Project Constraints (from CLAUDE.md)

Directives the plan MUST comply with (each is enforced by tooling or review):

| Directive | Consequence for this phase |
|-----------|---------------------------|
| **MUST** use `task` for build/test/lint/fmt — never raw `go test`/`golangci-lint` | All verification steps use `task ...` |
| **MUST** run `task test:int` on refactors — `task test` does NOT compile `//go:build integration` files | **Non-negotiable here**; this is a cross-package type refactor, the exact shape that breaks integration silently |
| **MUST** write tests before implementation (TDD) | D-07's regression test is written RED first |
| **MUST** run `task lint` before committing; **MUST** run `task fmt` (it mutates files — commit them) | Editing aligned Go `const`/`var` blocks (e.g. the `SubsystemID` block) can pass build+tests yet fail `task fmt:check` in CI |
| **MUST NOT** disable lint/format rules without confirmation; line-scoped `//nolint:<rule>` only | Never widen `.golangci.yaml` |
| **MUST** use context-carrying slog variants when a `ctx` is in scope (`sloglint context: scope`) | Bootstrap code (`cmd/holomush/core.go`) is the classic bare-`slog` offender; the `main`/init carve-out is narrow. Note `core.go:1453` already uses bare `slog.Warn` in a no-ctx helper (legitimate) |
| **MUST NOT** fabricate an invariant binding (`// Verifies:`) — the INV-RB-3 false-green bug | D-17's `INV-EVENTBUS-1` binding must be genuine (see §Validation Architecture) |
| **MUST** use `core.NewEvent()`, never `core.Event{}` literals | ⚠️ ARCH-04 **deletes** `core.NewEvent` — this CLAUDE.md rule and `.claude/rules/event-conventions.md` must be **amended in the same change** or the docs describe a deleted symbol |
| **MUST NOT** commit directly to `main`; squash-merge via PR | Phase branch `gsd/phase-07-...` already in use |
| New `SubsystemID` constants go at END of const block, then `task generate` | ⚠️ Not needed for TLS — see FINDING-9 |
| **MUST** treat binary and Lua plugins identically (runtime symmetry) | D-02's broadcast port + D-07's `ReplayTail` both touch the Lua/binary split — see FINDING-4 |
| **MUST NOT** import `coretest`/`eventbustest`/`natstest` in production code (depguard) | `coretest.MemoryEventStore` retires with `core.EventAppender` |

## Runtime State Inventory

This is a **refactor/rename phase**, so this section is mandatory. Each category answered
explicitly.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| **Stored data** | **None.** ARCH-04 changes in-memory Go types only. The wire format is unchanged: `eventbus.Event` is already the published type (`internal/world/outbox/wire.go:154` `EnvelopeToEvent`), `events_audit` rows are written from `msg.Data()`, and `AppSchemaVersion = 1` (`internal/world/outbox/taxonomy.go:19`) is **adopted, not bumped** (D-domain: "adopts these schemas rather than re-inventing them"). Verified: no `core.Event` is ever serialized — `busEventToCoreEvent` (`cmd/holomush/sub_grpc.go:976`) is a **read-path** translation only. | None — code edit only |
| **Live service config** | **None.** No n8n/Datadog/Cloudflare-style external config carries these Go symbol names. | None |
| **OS-registered state** | **None.** No task-scheduler/systemd registration embeds `core.Event`/`core.Engine`. | None |
| **Secrets/env vars** | **None.** No env var or SOPS key names any renamed symbol. ⚠️ But see §Specific: D-09's boot-panic regression only reproduces **with a KEK configured** (`cryptoActiveFor(cfg) = cfg.RekeyManager != nil`, `cmd/holomush/sub_grpc.go:184`) — the test env must wire a KEK or the regression is untested. | None (but test env must set KEK) |
| **Build artifacts** | **`internal/lifecycle/subsystemid_string.go`** is generated from the `SubsystemID` const block (`//go:generate stringer -type=SubsystemID -linecomment`, `subsystem.go:10`). Also `schemas/plugin.schema.json` regenerates via `go generate` on manifest changes. | Run `task generate` **only if** the `SubsystemID` const block changes — see FINDING-9: TLS needs **no** new constant |

## Standard Stack

**No new dependencies.** This phase adds zero libraries. Every tool it needs is already in the
tree and verified present:

### Core (existing, verified in-tree)
| Package | Purpose | Why Standard |
|---------|---------|--------------|
| `internal/lifecycle` | `Orchestrator` (`topoSort`, `StartAll`, `StopAll`), `Subsystem`, `ReadinessRegistry` | ARCH-03's target; already does the hard part [VERIFIED: `internal/lifecycle/subsystem.go`, read in full] |
| `golang.org/x/tools/go/packages` | AST import gate | Already used by `cmd/holomush/gateway_imports_test.go:15` [VERIFIED] |
| `github.com/oklog/ulid/v2` | Event identity | `core.NewULID()` — load-bearing for monotonicity [VERIFIED: `internal/core/ulid.go`] |
| `github.com/samber/oops` | Structured errors | Repo convention |
| `stringer` | `subsystemid_string.go` | `//go:generate` directive at `internal/lifecycle/subsystem.go:10` [VERIFIED] |
| `cmd/inv-render` | Regenerates `docs/architecture/invariants.md` from YAML | D-17's binding flip [VERIFIED: `.claude/rules/invariants.md`] |

### Verification tooling (the phase's own instrument)
`go list -deps` is the **authoritative** transitive-closure oracle and is the tool this research
used to find FINDING-1/2/3. The planner should use it in tests, not `rg` — grep sees *text*,
`go list` sees the *build graph*. This distinction is exactly what the existing AST test misses
(FINDING-3).

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Extending the AST test | `depguard` | **Rejected by D-17** — two gates to sync; depguard cannot express the file-level `coreOnlyFiles` split |
| Consumer-defined interface for `auth` | Moving `auth`'s presence calls to `cmd/holomush` | Larger blast radius; the injection seam already exists (FINDING-1) |
| New neutral package for whole `Event` | — | **Rejected by D-01** — breaks the `auditRow` package-private seam |

## Package Legitimacy Audit

**Not applicable — this phase installs zero external packages.** No registry lookups were
performed because no new dependency is proposed. Every package named in this research already
exists in `go.mod` or in-tree. If the planner introduces a new external dependency (it should
not need to), the Package Legitimacy Gate must be run before that plan ships.

## Architecture Patterns

### System Architecture Diagram — the import topology that governs this phase

```
                    ┌──────────────────────────────────────────┐
                    │        THE CYCLE CONSTRAINT (D-03)       │
                    └──────────────────────────────────────────┘

   internal/eventbus ──────► internal/core        [VERIFIED types.go:13,
        │                    (VerbRegistry,        rendering_publisher.go:12]
        │                     NewULID)
        │                                    ⇒ core can NEVER name eventbus.Event
        │
        ├──► eventbus/crypto/dek ──► … ──► internal/store ──► internal/auth
        │                                                          │
        │    [VERIFIED: go list -deps ./internal/eventbus          │
        │     contains internal/auth AND internal/store]           │
        │                                                          │
        │                                                          ▼
        │                                          internal/auth holds
        │                                          engine *core.Engine
        │                                          [auth_service.go:28]
        ▼
   ┌─────────────────────────────────────────────────────────────────────┐
   │  ⚠ FINDING-1: proposed internal/presence imports eventbus.          │
   │  If internal/auth imports presence:                                 │
   │     auth → presence → eventbus → … → store → auth   = CYCLE         │
   │  D-04's "no cycle (eventbus imports neither)" omits internal/auth.  │
   └─────────────────────────────────────────────────────────────────────┘


              ┌────────────────── ARCH-05 DATA FLOW ──────────────────┐

   telnet conn ──► internal/telnet ──► grpcclient.TranslateSubscribeErr
                        │                        │
                        │                        ▼
                        │              internal/grpc  (41 internal pkgs)
                        │                        │
                        │                        ├──► internal/world
                        │                        ├──► internal/access
                        │                        ├──► internal/command
                        │                        └──► internal/store  ◄── THE DB
                        │
                        └─► CURRENT CLOSURE: 47 internal packages
                            [VERIFIED: go list -deps ./internal/telnet]

   web conn ──► internal/web ──► core, session, gatewaymetrics, telemetry
                        └─► CURRENT CLOSURE: 7 internal packages, reaches
                            NO world/access/command/store/grpc  ◄── already clean
```

**Reading:** ARCH-05's entire *substantive* win is removing one import edge from `internal/telnet`.
`internal/web` is already compliant with D-15's **spirit**; it violates only D-15's **letter**
(it imports `core` + `session`, which are themselves leaves — FINDING-2).

### Pattern 1: Consumer-defined interface (the repo's cycle-breaker)
**What:** The consumer declares a narrow one-method interface; the producer is injected.
**When to use:** Whenever importing the producer package would create a cycle or drag a closure.
**Precedent in-tree:** `hostcap.SessionAdmin` (`internal/plugin/hostcap/capabilities.go:58-60`) —
declares `BroadcastSystemMessage(ctx, message) error` without importing the implementer. D-02
explicitly blesses this pattern ("Consumer-defined interfaces — no shared port package").

```go
// Source: internal/plugin/hostcap/capabilities.go:58-63 [VERIFIED, read live]
type SessionAdmin interface {
	// BroadcastSystemMessage sends a system message to all active sessions.
	BroadcastSystemMessage(ctx context.Context, message string) error
	// DisconnectSession forcibly disconnects a session with a reason.
	DisconnectSession(ctx context.Context, sessionID, reason string) error
}
```

**Apply this to FINDING-1:** `internal/auth` declares its own interface rather than importing
`internal/presence`. The injection seam already exists:

```go
// Source: internal/auth/auth_service.go:39 [VERIFIED live] — TODAY:
func WithGameSessionFanout(engine *core.Engine, gameSessions gamesession.Store) ServiceOption
// Source: internal/auth/auth_service.go:115 [VERIFIED live] — TODAY:
func (s *Service) ConfigureGameSessionFanout(engine *core.Engine, gameSessions gamesession.Store)
// Source: internal/auth/auth_service.go:28 [VERIFIED live] — TODAY:
	engine       *core.Engine
```
Changing `*core.Engine` → a locally-declared `PresenceEmitter` interface (covering the three
methods `auth` actually calls: `HandleDisconnect` at `:235`, `EndSession` at `:243`) breaks the
cycle with **no new package and no new concept**.

### Pattern 2: Intent at the chokepoint, wire type in one place
**What:** Name the *intent* at the boundary; construct the wire type at exactly one site.
**Precedent:** `internal/world/outbox/wire.go:154` `EnvelopeToEvent(env wmodel.Envelope)
(eventbus.Event, error)` — the worked example for D-02's broadcast builder. [VERIFIED live]

### Pattern 3: Structural enforcement over discipline
**What:** Make the illegal state unrepresentable rather than relying on per-edge discipline.
**Why it matters here:** D-11's Prepare/Activate split is this pattern applied to lifecycle. The
motivating evidence is verified: `grpcSubsystem.DependsOn()` returns
`[Bootstrap, Sessions, Auth, EventBus]` — **AuditProjection is absent**
(`cmd/holomush/sub_grpc.go:170-178`) [VERIFIED live].

### Anti-Patterns to Avoid
- **Grepping for imports instead of using `go list -deps`.** Text search sees direct import
  lines; it cannot see transitive reach. FINDING-2 and FINDING-3 both exist because the
  distinction was missed.
- **Justifying D-15 with "core/session reach the DB."** Provably false (FINDING-2). Use the
  drift-prevention rationale instead.
- **A second ordering authority** (countdown latch) alongside `topoSort` — D-10; this is
  MEDIUM-11's exact failure mode.
- **Fabricating the `INV-EVENTBUS-1` binding** — the documented INV-RB-3 false-green bug.
- **Assuming `task test` covers this refactor.** It does not compile integration files.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Subsystem start ordering | A latch / phase counter / slice order | `lifecycle.Orchestrator`'s `topoSort` + `DependsOn` | D-10; a second authority competing with topoSort IS MEDIUM-11 |
| Readiness gating | A new gate | `lifecycle.ReadinessRegistry` (`AllReady`/`WaitReady`/`HealthReporter`) | D-10; already used at core.go step 9 |
| Import-boundary enforcement | A bespoke parser | `golang.org/x/tools/go/packages` (already wired) + `go list -deps` for closure | Existing test already loads web/telnet correctly |
| Transitive closure computation | Recursive grep | `go list -deps` | The build graph is the truth; grep is not |
| Cursor encode/decode | A new token format | `internal/eventbus/cursor` — `HostCursor{Seq, ID}` already has the field | D-07 *fills in* a field, adds nothing [VERIFIED: `cursor.go:54-57`] |
| Actor bridging | A 4th copy | Collapse the 3 that exist (D-06) | Two are self-admitted hand-copies |
| Invariant doc table | Hand-editing `invariants.md` | `go run ./cmd/inv-render` | Generated region; meta-test diffs it |

**Key insight:** ARCH-03 is **not** "build an orchestrator" — the orchestrator exists and is
tested. It is "stop doing bootstrap work outside the one that exists." Every fix D-09 proposes is
expressible as an existing `DependsOn` edge.

## Common Pitfalls

### Pitfall 1: The `internal/auth` import cycle (FINDING-1)
**What goes wrong:** `internal/presence` is created per D-04, `internal/auth` is updated to use it,
and the build fails with an import cycle.
**Why it happens:** CONTEXT.md's D-04 cycle analysis lists only `grpc` + `cmd/holomush` as
consumers and asserts "eventbus imports neither." Both halves are incomplete: `internal/auth` is
also a consumer, and eventbus's closure **does** contain `internal/auth`.
**How to avoid:** Consumer-defined interface in `internal/auth` (Pattern 1). Never import
`internal/presence` from `internal/auth`.
**Warning signs:** `import cycle not allowed` at `task build`. Detect *before* writing code:
`go list -deps ./internal/eventbus | grep holomush/internal/auth` → non-empty means the cycle is
live.

### Pitfall 2: `task test` green, `task test:int` red
**What goes wrong:** A cross-package type refactor compiles and unit-tests clean, then integration
breaks — because `task test` does **not** compile `//go:build integration` files.
**Why it happens:** Documented repo landmine. `internal/testsupport/integrationtest/harness.go`
hand-mirrors production wiring in at least four places, each of which must move in lockstep:
`noopEventAppender:1313`, `busEventAppenderAdapter:1328`, `harnessCoreToBusActor:1364`,
`focusHistoryReaderAdapter:1522` [all VERIFIED live].
**How to avoid:** `task test:int` on **every** commit that touches a shared type, not just at the
end. Per D-12, each wave must land green.
**Warning signs:** Any edit to `core.Event`, `core.Actor`, `EventAppender`, `HistoryReader`.

### Pitfall 3: The `ReplayTail` signature is declared **twice** (FINDING-4)
**What goes wrong:** D-07's seq param is added to `plugins.HistoryReader`, and
`internal/plugin/lua/hostcap_adapter.go` stops compiling.
**Why it happens:** Two interfaces declare the identical method, and a Lua adapter relies on
**structural typing** to satisfy both from one concrete value:
```go
// Source: internal/plugin/lua/hostcap_adapter.go:225-234 [VERIFIED live]
// HistoryReader returns the history reader from the Functions backing (nil when unset).
// hostfunc.HistoryReader and plugins.HistoryReader have the same ReplayTail signature;
// the concrete type satisfies both.
func (a *luaHostCapAdapter) HistoryReader() plugins.HistoryReader {
	hr := a.f.GetHistoryReader()
	if hr == nil {
		return nil
	}
	return hr
}
```
**How to avoid:** Change **both** interfaces in lockstep — `internal/plugin/host.go:136` and
`internal/plugin/hostfunc/stdlib_focus.go:50`. This is a **plugin-runtime-symmetry** surface
(Lua reaches history via hostfunc; binary via hostcap) — asymmetry here is a rule violation.
**Warning signs:** `cannot use hr (variable of type hostfunc.HistoryReader) as plugins.HistoryReader`.

### Pitfall 4: Amending the `forbidden` list silently re-scopes `cmd/holomush`
**What goes wrong:** The `forbidden` list is **shared** between `cmd/holomush`,
`internal/web/...`, and `internal/telnet/...` (`gateway_imports_test.go:115-128`). Adding three
packages could break `cmd/holomush` files not covered by `coreOnlyFiles`.
**How to avoid:** **Already verified safe.** I enumerated all 38 `cmd/holomush` files *not* in
`coreOnlyFiles` and checked each for `internal/{core,session,grpc}` imports: **zero hits**. The
amendment's blast radius in `cmd/holomush` is nil.
**Warning signs:** None expected — but re-run the check if `coreOnlyFiles` changes.

### Pitfall 5: The AST gate cannot enforce D-15's actual principle (FINDING-3)
**What goes wrong:** The plan adds `internal/grpc` to `forbidden`, the test goes green, and
everyone believes the transitive principle is enforced. It is not.
**Why it happens:** `checkFile` iterates `file.Imports` — **direct imports only**
(`gateway_imports_test.go:148`). D-15's rule ("MUST NOT import any package that *transitively*
reaches DB/domain/bus") is a closure property the AST test structurally cannot express. A future
gateway file could import an innocuous-looking leaf that later grows a `world` dep, and the gate
stays green.
**How to avoid:** Add a **closure assertion** alongside the direct-import gate (see §Validation
Architecture). This is also what makes `INV-EVENTBUS-1`'s binding genuine rather than nominal.

### Pitfall 6: `rg -r` silently eats your pattern
**What goes wrong:** `rg -rn 'DefaultLeaseRefreshInterval'` prints `session.n` — `-r` is
`--replace` and consumed the `n`.
**Why it happens:** Documented repo trap; I reproduced it live during this research.
**How to avoid:** `rg -n`. Never `-rn`.

## Code Examples

### Verified: the eventbus→core edge (why `core.Engine` must move — D-03)
```go
// Source: internal/eventbus/types.go:5-14 [VERIFIED live]
import (
	"fmt"
	"regexp"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/core"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)
```
Also `internal/eventbus/rendering_publisher.go:12`. ⇒ `core` naming `eventbus.Event` is
`core → eventbus → core`. Confirms D-03 and the briefing's trap #2.

### Verified: the Seq never-serialize rule (D-08)
```go
// Source: internal/eventbus/types.go:141-148 [VERIFIED live]
type Event struct {
	ID        ulid.ULID
	Seq       uint64 // JetStream stream sequence; populated by both tier readers and by the subscriber. Host-internal — never serialized in any public proto envelope.
	Subject   Subject
	Type      Type
	Timestamp time.Time
	Actor     Actor
	Payload   []byte // codec.Encode output (ciphertext if encryption is on)
```

### Verified: D-07's bug chain, hop by hop
```go
// HOP 1 — QueryHistory returns eventbus.Event WITH Seq (above).

// HOP 2 — busEventToCoreEvent DESTROYS Seq (core.Event has no such field).
// Source: cmd/holomush/sub_grpc.go:976-992 [VERIFIED live]
func busEventToCoreEvent(e eventbus.Event, stream string) core.Event {
	actorID := ""
	if e.Actor.ID != (ulid.ULID{}) {
		actorID = e.Actor.ID.String()
	}
	return core.Event{
		ID:        e.ID,
		Stream:    stream,
		Type:      core.EventType(e.Type),
		Timestamp: e.Timestamp,
		Actor: core.Actor{
			Kind: busActorKindToCore(e.Actor.Kind),
			ID:   actorID,
		},
		Payload: e.Payload,
	}                    // ← no Seq. Information destroyed here.
}

// HOP 3 — encodeHostEventCursor is FORCED to hardcode Seq: 0, and says so.
// Source: internal/plugin/hostcap/servers.go:1279-1296 [VERIFIED live; func at :1285]
// encodeHostEventCursor encodes an event ULID into an opaque host cursor
// token for the plugin → host boundary. Seq is not available here (the
// plugins.HistoryReader.ReplayTail interface returns core.Event without Seq),
// so Seq=0 is used. The cold tier handles Seq=0 as "ID-only" fallback.
func encodeHostEventCursor(id ulid.ULID) []byte {
	b, err := cursor.Encode(cursor.Cursor{
		Version: cursor.CurrentVersion,
		Epoch:   cursor.CurrentEpoch(),
		Owner:   cursor.Owner{Kind: cursor.OwnerHost},
		Host:    &cursor.HostCursor{Seq: 0, ID: id},   // ← always zero
	})
	...
}

// HOP 4 — ReplayTail passes neither AfterSeq nor BeforeSeq.
// Source: cmd/holomush/sub_grpc.go:916-935 [VERIFIED live]
	q := eventbus.HistoryQuery{
		Subject:   sub,
		Direction: eventbus.DirectionBackward,
		PageSize:  count,
		NotBefore: notBefore,
	}
	if !beforeID.IsZero() {
		q.BeforeID = beforeID          // ← ULID cursor only. No seq.
	}

// HOP 5 — the system states ULID order != stream order.
// Source: internal/eventbus/history/hot_jetstream.go:424-431 [VERIFIED live]
// Ordering is owned by JetStream per-stream sequence, not ULID lex order —
// concurrent publishers produce events whose ULIDs do NOT match stream
// sequence.
```
**Conclusion: the bug is real and every hop is confirmed.** The target field already exists:
```go
// Source: internal/eventbus/cursor/cursor.go:54-57 [VERIFIED live]
type HostCursor struct {
	Seq uint64
	ID  ulid.ULID
}
```

### Verified: D-02's live duplicate payload shape
```go
// Source: internal/command/types.go:622-641 [VERIFIED live]
func (s *Services) BroadcastSystemMessage(ctx context.Context, stream, message string) {
	if s.events == nil {
		slog.DebugContext(ctx, "broadcastSystemMessage: event store not configured")
		return
	}
	//nolint:errcheck // json.Marshal cannot fail for map[string]string
	payload, _ := json.Marshal(map[string]string{
		"message": message,
	})
	event := core.NewEvent(stream, core.EventTypeSystem, core.Actor{
		Kind: core.ActorSystem,
		ID:   core.ActorSystemID,
	}, payload)
	if err := s.events.Append(ctx, event); err != nil { ... }
}
```
The `{"message": ...}` shape is mirrored by `hostcap/system_broadcaster.go` — the drift contract
D-02 removes.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `INV-GW-1..16` gateway invariant family | `INV-EVENTBUS-1..16` | `holomush-hz0v4.14.12` | ⚠️ **D-18** — LOW-6's "rename to INV-GW-1" is STALE; following it reverses the migration [VERIFIED: `internal/gateway_invariants/meta_test.go:17`] |
| `EventStore.Append` + LISTEN/NOTIFY | JetStream EventBus (`Publisher`/`Subscriber`/`HistoryReader`) | F1–F7 cutover | `core.EventAppender` is a vestige of the old model — hence ARCH-04 |
| Event sourcing / state-from-replay | **CRUD-canonical** (MODEL-01 Option B) | Phase 4 ADR | The collapsed Event is an audit/notification wire type, NOT a state-rebuild source |
| Colon-style subjects (`scene:01ABC`) | Dot-delimited `events.<game_id>.<domain>.<id>` | `holomush-rops` | `subjectxlate` deleted; do not reintroduce |
| Ad-hoc invariant families | `docs/architecture/invariants.yaml` registry + binding ratchet | `holomush-hz0v4` | D-17 must follow the ratchet, not invent |

**Deprecated/outdated:**
- `core.Event`/`core.NewEvent`/`core.EventAppender`/`core.Engine` — deleted by this phase.
- `coretest.MemoryEventStore` — retires with `core.EventAppender`.
- ⚠️ **Root `CLAUDE.md` + `.claude/rules/event-conventions.md` mandate `core.NewEvent()`** — both
  describe a symbol this phase deletes. **Must be amended in the same change.**
- ⚠️ `.planning/PROJECT.md` "Key Decisions" #3 and `.planning/codebase/ARCHITECTURE.md` still
  assert event-sourcing — reversed by MODEL-01 (deferred; file a `gh issue`).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | everything | ✓ | go.mod-pinned | — |
| `task` (go-task) | all build/test/lint | ✓ | `Taskfile.yaml` present | — |
| Docker | `task test:int` (Postgres testcontainers), `task test:e2e` | ✓ (assumed per repo norm) | — | **None — blocking.** `task test:int` is mandatory here |
| `stringer` | `subsystemid_string.go` | ✓ via `task generate` | — | — |
| `go list` / `golang.org/x/tools/go/packages` | closure + AST gates | ✓ | in `go.mod` | — |
| Embedded NATS (`eventbustest`) | D-07 regression test | ✓ in-tree | — | Real NATS container (`natstest`) only if external-mode-specific |
| KEK config | D-09 boot-panic regression | ⚠️ test-env dependent | — | **None** — without a KEK the regression is untested (see §Specific) |

**Missing dependencies with no fallback:** none identified (Docker assumed present per repo norm;
confirm before planning `task test:int` steps).

## Validation Architecture

Nyquist validation is **enabled** (`.planning/config.json` → `workflow.nyquist_validation: true`).

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `testify` (unit); Ginkgo/Gomega (integration, `//go:build integration`); Playwright (E2E) |
| Config file | `Taskfile.yaml` (task defs); `.golangci.yaml` (lint, v2) |
| Quick run command | `task test -- ./internal/<pkg>/` |
| Full suite command | `task test` then **`task test:int`** (mandatory — `task test` does NOT compile integration files) |

### What "no behavior change" MEANS — as an observable, testable property

This is a refactor phase; the whole risk is **silent behavior drift**. "No behavior change" is
too vague to test. Per requirement, here is the operationalization:

| Req | "No behavior change" = this observable property | How it is observed |
|-----|--------------------------------------------------|--------------------|
| **ARCH-04** | **The bytes on the wire and in `events_audit` are identical before/after.** `eventbus.Event` is already the published type; `core.Event` is never serialized (`busEventToCoreEvent` is read-path only). So: for a fixed input, the published proto envelope + `events_audit` row are byte-identical. | Existing crypto/audit integration suites already assert byte-equality (INV-21 lineage: `audit/projection.go` writes `msg.Data()`). Run `task test:int`. Plus: `AppSchemaVersion` stays `1` — assert it is NOT bumped. |
| **ARCH-04** | **Emit gates still fire identically.** The actor-kind/emits/crypto.emits manifest gates at `event_emitter.go::Emit` behave the same for Lua AND binary after the 3 actor bridges collapse to 1. | `test/integration/pluginparity/`, `test/integration/crypto/` (which explicitly exercise `coreActorToEventbusActor`'s non-ULID rejection at `e2e_test.go:208`, `emit_test.go:113`, `metadata_only_test.go:237,337`) |
| **ARCH-03** | **The boot succeeds with a KEK configured, and the topological start order is unchanged** (except where D-14 deliberately changes it). Shutdown completes within the deadline. | A topo-order pin test (see New Tests) + `task test:int` full-stack boot via `integrationtest.Start(t)` |
| **ARCH-03** | **`StopAll` terminates.** Today `defer orch.StopAll(context.Background())` (`core.go:1105`) has no deadline — LOW-7. After: it honors a 5s ctx. | A test asserting `StopAll` returns under deadline with a deliberately-hanging subsystem |
| **ARCH-05** | **`internal/telnet`'s transitive internal-package closure shrinks from 47 to ~6 and contains no `world`/`access`/`command`/`store`.** This is the objective, non-behavioral success metric. | `go list -deps` assertion (see New Tests) |
| **ARCH-05** | **Telnet/web runtime behavior is byte-identical** — same error classification (`SESSION_NOT_FOUND` vs `RPC_FAILED`), same connection IDs, same arrive/leave rendering. | Existing `internal/telnet/gateway_handler_test.go` (uses `core.EventTypeCommandResponse` at :376,:442 — will need the vocabulary-leaf rename); `task test:e2e` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| ARCH-04 | Event wire bytes unchanged after collapse | integration | `task test:int` | ✅ `test/integration/crypto/`, `internal/eventbus/audit/` |
| ARCH-04 | Actor-bridge collapse preserves non-ULID rejection | integration | `task test:int` | ✅ `test/integration/crypto/emit_test.go:113` |
| ARCH-04 | Lua/binary emit parity preserved | integration | `task test:int` | ✅ `test/integration/pluginparity/` |
| ARCH-04 | Broadcast payload `{"message":...}` shape preserved from one builder | unit+integration | `task test -- ./internal/command/ ./internal/plugin/hostcap/` | ✅ `hostcap/system_broadcaster_test.go`, `plugin/setup/system_broadcaster_test.go`, `test/integration/pluginparity/session_admin_broadcast_test.go` |
| ARCH-04 | **No import cycle** (FINDING-1) | build | `task build` + `task test:int` | ✅ compiler is the oracle |
| **D-07** | **Plugin history pages neither skip nor repeat under concurrent publishers** | integration | `task test:int` | ❌ **Wave 0 — NEW** |
| D-08 | `hostv1.Event` still has no seq field; cursor stays opaque | unit (meta) | `task test -- ./internal/plugin/hostcap/` | ❌ **NEW** (guard) |
| ARCH-03 | Boot succeeds with KEK wired; no pre-starts | integration | `task test:int` | ⚠️ partial — must confirm KEK wired |
| ARCH-03 | Topological start order pinned | unit | `task test -- ./cmd/holomush/` | ❌ **Wave 0 — NEW** (D-14 MEDIUM-11) |
| ARCH-03 | `StopAll` honors deadline (LOW-7) | unit | `task test -- ./internal/lifecycle/ ./cmd/holomush/` | ❌ **NEW** |
| ARCH-03 | Prepare/Activate rollback semantics (D-11/D-13) | unit | `task test -- ./internal/lifecycle/` | ❌ **Wave B — NEW** |
| ARCH-03 | `productionSubsystems` set/count | unit | `task test -- ./cmd/holomush/` | ✅ 7 tests in `core_subsystems_test.go` — will need updating |
| ARCH-05 | Gateway direct imports exclude core/session/grpc | unit (AST) | `task test -- ./cmd/holomush/` | ✅ `gateway_imports_test.go` — amend `forbidden` |
| ARCH-05 | **Gateway transitive closure excludes domain** (FINDING-3) | unit | `task test -- ./cmd/holomush/` | ❌ **NEW — this is what makes INV-EVENTBUS-1's binding genuine** |

### Sampling Rate
- **Per task commit:** `task test -- ./<touched-pkg>/` **and** `task lint`
- **Per shared-type edit (every ARCH-04 commit):** **`task test:int`** — non-negotiable; `task test`
  does not compile integration files
- **Per wave merge (D-12):** `task test` + `task test:int` + `task build` green; wave
  independently reviewable
- **Phase gate:** `task pr-prep` green (fast lane) before push; `Integration Test` + `E2E Test`
  are required CI checks

### Wave 0 Gaps

- [ ] **D-07 concurrent-publisher pagination regression** — integration, `//go:build integration`.
      MUST reproduce the real failure: concurrent publishers producing ULIDs that do NOT match
      stream sequence, then assert pages neither skip nor repeat. A quiet-stream page walk
      **passes today and proves nothing** (`hot_jetstream.go:424-431` names the exact condition).
      Suggested home: `test/integration/eventbus_e2e/` or alongside the existing history tier
      tests. Use `eventbustest` embedded NATS (correct tier — this is not external-mode-specific).
- [ ] **Gateway transitive-closure assertion** — unit, `cmd/holomush/gateway_imports_test.go`.
      Asserts `go list -deps ./internal/telnet` and `./internal/web` contain no
      `internal/{world,access,command,store,grpc,plugin,eventbus,auth/service}`. This closes
      FINDING-3's gap and is the **genuine** binding for INV-EVENTBUS-1.
- [ ] **Topological start-order pin** — unit, `cmd/holomush/core_subsystems_test.go`. Pins the
      actual `topoSort` sequence so MEDIUM-11's comment-vs-graph divergence cannot recur.
- [ ] **`StopAll` deadline test** — unit, `internal/lifecycle/`.
- [ ] **Prepare/Activate rollback test** — unit, `internal/lifecycle/` (Wave B; shape depends on
      D-13.1).
- [ ] Framework install: **none needed** — all frameworks present.

### How INV-EVENTBUS-1 gets a GENUINE binding

Per `.claude/rules/invariants.md` and the guard `TestBoundInvariantsAreGenuinelyAsserted`
(`test/meta/invariant_registry_test.go:1128`), a binding is genuine only when the annotated test
**actually asserts** the invariant. Never fabricate (the documented INV-RB-3 false-green bug).

**Current state [VERIFIED live, `docs/architecture/invariants.yaml:2340-2348`]:**
```yaml
  - id: INV-EVENTBUS-1
    scope: INV-EVENTBUS
    origin_spec: "docs/superpowers/specs/2026-04-26-gateway-verb-registry-sourcing.md"
    legacy: ["INV-GW-1@docs/superpowers/specs/2026-04-26-gateway-verb-registry-sourcing.md"]
    summary: "The gateway process MUST NOT import internal/world, internal/access, internal/store, internal/plugin, internal/eventbus,
      internal/auth/service, or internal/command."
    binding: pending
    refs:
      - {file: "cmd/holomush/gateway_imports_test.go", token: "INV-GW-1"}
```

**Required steps:**
1. **Amend `summary`** to add `internal/core`, `internal/session`, `internal/grpc` (D-17). Keep it
   in sync with the `forbidden` list — the summary drifts the moment the list changes.
2. **Fix the stale `refs` token** — FINDING-8: the entry claims token `INV-GW-1` in
   `cmd/holomush/gateway_imports_test.go`, but that file contains **no** `INV-GW-1` token; it
   carries `INV-EVENTBUS-1` (lines 21, 111, 122) [VERIFIED live].
3. **Annotate** `// Verifies: INV-EVENTBUS-1` immediately above
   `TestGatewayImportsAreOnlyProtocolTranslation` (`gateway_imports_test.go:114`). This test
   genuinely asserts the invariant — it uses `require.NoError` + `t.Errorf`, which
   `TestBoundInvariantsAreGenuinelyAsserted`'s classifier scores as `"asserts"` (not a
   Skip-only placeholder). **The binding is legitimate, not fabricated.**
4. ⚠️ **Consider also annotating the NEW closure test.** The existing AST test only proves the
   *direct-import* half of the summary. If the summary is read as a transitive claim (D-15's
   principle), the AST test alone is a **partial binding** — the exact hazard
   `.claude/rules/invariants.md` warns needs human review (as INV-PRIVACY-6 did), because the
   guard cannot detect partial bindings. Recommend `asserted_by` listing **both** tests.
5. Set `binding: bound` and add `asserted_by:`. Per the rule, `pending` entries MUST NOT carry
   `asserted_by` — so both change together.
6. Run `go run ./cmd/inv-render` (regenerates `docs/architecture/invariants.md`; never hand-edit
   the generated regions).
7. Verify: `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/`

## Security Domain

`security_enforcement` is not disabled, so this section is included. This is an internal refactor
with **no new attack surface**, but two existing controls sit directly in the blast radius.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | indirect | `internal/auth` is touched by ARCH-04 (FINDING-1). No auth *logic* changes — only the `*core.Engine` parameter type. Any change to auth's session-fanout behavior is out of scope and would be a regression. |
| V3 Session Management | indirect | `session.DefaultLeaseRefreshInterval` relocates (D-16). **Value must not change** — it is a live session-lease timing constant. |
| V4 Access Control | yes | Plugin emit gates (`actor_kinds_claimable`, `emits`, `crypto.emits`) fire at `event_emitter.go::Emit` for both runtimes. The 3→1 actor-bridge collapse (D-06) must preserve identical gate behavior — including `coreActorToEventbusActor`'s **non-ULID rejection**, which `test/integration/crypto/` explicitly depends on. |
| V5 Input Validation | yes | `core.ParseCommand` moves to a leaf (D-16). Grammar must be byte-identical. |
| V6 Cryptography | yes | **Do not hand-roll.** `eventbus.Event`'s unexported `auditRow` + `AuditRowOf` feed `history.PluginDowngradeFence` for **INV-CRYPTO-42/50** — this package-private seam is precisely why D-01 rejects a neutral-leaf Event. Breaking it silently breaks a crypto downgrade fence. |

### Known Threat Patterns for this change

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Actor-kind forgery via a mis-collapsed actor bridge | Spoofing | Preserve `coreActorToEventbusActor`'s ULID validation + manifest gate at the common path; `test/integration/crypto/` asserts it |
| Plugin crypto **downgrade** via broken `AuditRowOf` seam | Elevation of Privilege | Keep `Event` in `internal/eventbus` (D-01); `PluginDowngradeFence` tests must stay green |
| Plugin reads events outside its authorized set via seq cursor | Information Disclosure | D-08 — do NOT expose `Seq`; cursor stays opaque; `hostv1.Event` gains no seq field |
| **Serving before the audit projection is up** (audit gap) | Repudiation | D-11/D-14 — `grpcSubsystem.DependsOn()` **excludes AuditProjection** [VERIFIED live, `sub_grpc.go:170-178`]; add the edge. Events served before the projection is up may not be durably audited |
| Boot panic → fail-open or crash-loop under production KEK | DoS | D-09; regression MUST run with a KEK wired |
| Gateway compromise reaching the DB | Elevation of Privilege | ARCH-05 — telnet currently has `internal/store` in its closure [VERIFIED: 47-pkg closure] |

⚠️ **Crypto-reviewer gate:** per CLAUDE.md, changes touching `internal/eventbus/codec/`,
`internal/eventbus/history/dispatcher.go`, `internal/plugin/event_emitter.go::Emit`, or
`internal/eventbus/audit/projection.go` **MUST** run `crypto-reviewer` before push. D-06's actor
bridge (`event_emitter.go:306`) and D-07's history path both land in that surface. Plan for it.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Docker is available for `task test:int` / `task test:e2e` | Environment Availability | Integration tests cannot run → the mandatory regression net is unavailable → phase cannot be verified. Confirm before planning. |
| A2 | `internal/telnet`'s post-fix closure lands at ~6 internal packages | Validation Architecture | The exact target number is an estimate (current direct imports minus `grpc`, plus telemetry's 2 transitive deps). The **assertion should be "excludes world/access/command/store"**, not a magic number — a brittle count invites churn. |
| A3 | `TestBoundInvariantsAreGenuinelyAsserted`'s classifier scores the gateway AST test as `"asserts"` | Validation Architecture | Inferred from the classifier's own table (`invariant_registry_test.go:1208-1217`: `require.NoError` → `"asserts"`) applied to the test's body. Not executed. If wrong, the binding flip fails CI — caught immediately, low risk. |
| A4 | The intermediate hop from `internal/eventbus/crypto/dek` to `internal/store` | Architecture diagram | **The cycle proof does NOT depend on this.** The proof is closure membership (`go list -deps ./internal/eventbus` contains `internal/auth`), which I verified directly. The exact intermediate chain is illustrative only and I did not fully trace it — stated honestly rather than fabricated. |
| A5 | No stored data / OS state / secrets reference the renamed symbols | Runtime State Inventory | Based on the wire type already being `eventbus.Event` and `core.Event` never being serialized (verified via `busEventToCoreEvent` being read-path-only). If a serialized `core.Event` exists somewhere unsearched, a data migration would be needed. Low risk — the F1–F7 cutover already moved the wire to eventbus. |

## Open Questions

1. **How does `internal/auth` reach presence without a cycle? (FINDING-1 — BLOCKING)**
   - What we know: the cycle is proven; `auth` already has an injection seam
     (`WithGameSessionFanout`/`ConfigureGameSessionFanout`); the repo's consumer-defined-interface
     pattern (D-02) solves it cleanly.
   - What's unclear: whether the planner prefers the interface in `internal/auth` or restructuring
     so `cmd/holomush` owns the fanout wiring.
   - **Recommendation:** consumer-defined interface in `internal/auth`. Zero new packages, matches
     D-02's blessed pattern, smallest diff. **The planner MUST settle this — it is not an
     implementer detail.**

2. **What happens to `Engine.EndSession` under the `presence.Emitter` rename? (D-04)**
   - What we know: `Engine` has **three** methods — `HandleConnect:65`, `HandleDisconnect:88`
     (`engine.go`), and `EndSession:33` (`engine_end_session.go`). D-04 names renames for only the
     first two.
   - What's unclear: `EndSession` is session-lifecycle, not obviously "presence." It has 6 live
     call sites across `auth`, `grpc`, `cmd/holomush`.
   - **Recommendation:** keep all three on `presence.Emitter` (they share the store + emit shape);
     name it deliberately rather than by omission.

3. **Does `internal/web` need to change at all for ARCH-05? (FINDING-2)**
   - What we know: web's closure is 7 packages and reaches no domain. It imports `core` +
     `session`, both already leaves.
   - What's unclear: D-15 forbids them **wholesale**, so web must change to satisfy the *letter*
     even though it already satisfies the *spirit*.
   - **Recommendation:** honor D-15 (it is locked) but sequence web **after** telnet — telnet
     carries the real risk and the real win.

4. **D-13.1 — two-phase rollback semantics.** Unresolved by design; the planner must settle.
   `StopAll` today only stops subsystems in `startOrder` (`orchestrator.go:81-96`). "Activate
   fails after N prepared" is a genuinely new question.

5. **D-13.2 — is `Start` MUST-be-idempotent vestigial?** [VERIFIED live,
   `internal/lifecycle/subsystem.go:51`: "Start initializes the subsystem. It MUST be
   idempotent."] CONTEXT.md's claim that it exists *only* to support the pre-start hack is
   plausible and consistent with `StartAll` re-invoking `Start` on already-started subsystems, but
   I did **not** find an independent second reason for it. Decide deliberately.

6. **MEDIUM-11: real edge or comment-deletion + pin test?** (Claude's discretion per CONTEXT.md.)
   Note the actual boot intent must be established first — the comment asserts an order the graph
   contradicts, so one of them is wrong about intent.

## Sources

### Primary (HIGH confidence — live source in this worktree @ `be030a368`)
- `internal/lifecycle/subsystem.go` — read in full
- `cmd/holomush/gateway_imports_test.go` — read in full
- `internal/gateway_invariants/meta_test.go` — read in full
- `internal/eventbus/types.go:5-14,138-150`; `internal/eventbus/cursor/cursor.go:54-66`
- `internal/eventbus/history/hot_jetstream.go:420-432`
- `cmd/holomush/sub_grpc.go:170-178,282-295,726-752,821-865,910-992`
- `cmd/holomush/core.go:256-360,462,494-500,797-800,977,1100-1110,1450-1490`
- `internal/plugin/hostcap/servers.go:1279-1300`; `capabilities.go:52-64`
- `internal/plugin/host.go:128-142`; `hostfunc/stdlib_focus.go:44-56`; `lua/hostcap_adapter.go:215-245`
- `internal/core/engine.go:28-50,65,88`; `engine_end_session.go:33`
- `internal/command/types.go:523,543,561,618-645`
- `internal/auth/auth_service.go:28,39,115,235,243`
- `internal/world/outbox/taxonomy.go:12-19`; `wire.go:149-160`
- `docs/architecture/invariants.yaml:478-492,2340-2348`
- `go list -deps` / `go list -f {{.Imports}}` — authoritative build-graph oracle (FINDING-1/2/3)
- `.planning/phases/07-.../07-CONTEXT.md`, `.planning/REQUIREMENTS.md`, `.planning/config.json`

### Secondary (MEDIUM confidence)
- `.claude/rules/`: `invariants.md`, `gateway-boundary.md`, `event-conventions.md`,
  `event-interfaces.md`, `testing.md`, `plugin-runtime-symmetry.md`, `logging.md`
- `.claude/rules/references/plan-review-learnings.md`, `design-review-learnings.md` — the
  fabrication catalogue this research is designed to prevent
- Root `CLAUDE.md`

### Tertiary (LOW confidence — NOT used for any claim)
- None. No web search was performed; no external documentation was consulted. This phase has no
  external-technology surface, and all search providers are disabled in `.planning/config.json`.

## Metadata

**Confidence breakdown:**
- Standard stack: **HIGH** — no new dependencies; every tool verified present in-tree
- Architecture / import topology: **HIGH** — established via `go list -deps` (the build graph
  itself), not inference. FINDING-1/2/3 are reproducible in one command each.
- Pitfalls: **HIGH** — each is a verified live citation, several reproduced during research
  (including the `rg -r` trap)
- Bootstrap census: **HIGH** — all 5 eager starts, phantom TLS, and the param count read live
- D-13 questions: **MEDIUM** — deliberately unresolved by CONTEXT.md; flagged, not answered

**Research date:** 2026-07-15
**Valid until:** ~2026-08-14 (30 days) — but **invalidated immediately** by any merge to `main`
that touches `internal/{core,eventbus,auth,grpc,telnet,web}` or `cmd/holomush`. Line citations
drift; re-verify the load-bearing ones (FINDING-1's cycle above all) at plan time with the
one-line commands given in each finding.
