# Phase 7: Event-Model & Bootstrap Decomposition - Pattern Map

**Mapped:** 2026-07-15
**Worktree:** `/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7` @ `be030a368` (branch `gsd/phase-07-event-model-bootstrap-decomposition`)
**Files analyzed:** 9 new/modified artifact groups
**Analogs found:** 8 strong / 1 partial / 0 none

> **Scope note.** RESEARCH.md already carries verified excerpts for the *problem*
> surfaces (the Seq bug chain, the eventbus→core edge, the broadcast duplicate,
> the `auditRow` seam). This file does NOT duplicate them. It answers only:
> **what existing in-tree code should each NEW artifact copy its shape from?**

---

## File Classification

| New/Modified artifact | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|
| `internal/presence/` (new pkg; `presence.Emitter` ← `core.Engine`) | service | event-driven (emit) | `internal/plugin/hostcap/system_broadcaster.go` | **exact** (marshal→build→emit over an injected sink) |
| `internal/presence/` package/wiring shape | service pkg | — | `internal/world/setup/subsystem.go:18-70` (provider-config ctor) | role-match |
| Event-type vocabulary leaf (D-05) | vocabulary/const | none (pure) | `internal/telnet/gamenotice/` + `internal/naming/` | **exact** |
| Consumer-defined iface in `internal/auth` (FINDING-1) | port decl | — | `internal/world/setup/subsystem.go:21-31` (`PoolProvider`/`EngineProvider`) | **exact** |
| Broadcast port + one builder (D-02) | port + builder | request→emit | `hostcap.SessionAdmin` + `hostcap.NewSystemBroadcaster` | **exact** (it IS the surviving builder) |
| `lifecycle.Subsystem` Prepare/Activate (D-11) | interface | lifecycle | `internal/lifecycle/subsystem.go:44-58` (18 impls, enumerated below) | modify-in-place |
| Handles-not-live-values ctors (D-09) | subsystem ctors | lifecycle | `internal/world/setup/subsystem.go:31-63` + `internal/access/setup/subsystem.go:27-67` | **exact** — the pattern already exists; the 5 eager starts are the deviants |
| TLS subsystem (D-14 phantom) | subsystem | file I/O | `internal/world/setup/subsystem.go` (`DependsOn(Database)` shape) | role-match |
| D-07 concurrent-publisher regression test | integration test | streaming | `test/integration/eventbus_e2e/cursor_concurrent_test.go` | **exact — same bug class, one layer down** |
| Gateway closure test (Wave 0) | unit test (AST/`go list`) | — | `cmd/holomush/gateway_imports_test.go` | **exact** |
| Topo start-order pin test (D-14) | unit test | — | `cmd/holomush/core_subsystems_test.go:17-52` | **exact** |

---

## Pattern Assignments

### 1. `internal/presence` — `presence.Emitter` (D-03/D-04)

**Analog A — the emit body shape:** `internal/plugin/hostcap/system_broadcaster.go`

This is the *closest* in-tree twin to `core.Engine`: a struct holding **one**
injected sink, whose methods marshal a payload → build an event → append, and
which returns the **interface**, not the struct, from its constructor.

```go
// Source: internal/plugin/hostcap/system_broadcaster.go:35-62 [VERIFIED live]
type systemBroadcaster struct {
	appender core.EventAppender
}

// NewSystemBroadcaster builds a SessionAdmin broadcast backing over appender.
// Returned as the SessionAdmin interface so callers cannot reach the struct.
func NewSystemBroadcaster(appender core.EventAppender) SessionAdmin {
	return &systemBroadcaster{appender: appender}
}

func (b *systemBroadcaster) BroadcastSystemMessage(ctx context.Context, message string) error {
	//nolint:errcheck // json.Marshal cannot fail for map[string]string
	payload, _ := json.Marshal(map[string]string{"message": message})
	event := core.NewEvent(
		core.SystemBroadcastSubject,
		core.EventTypeSystem,
		core.Actor{Kind: core.ActorSystem, ID: core.ActorSystemID},
		payload,
	)
	if err := b.appender.Append(ctx, event); err != nil {
		return oops.Code("SYSTEM_BROADCAST_FAILED").Wrap(err)
	}
	return nil
}
```

**Deltas the planner must apply:** the sink becomes `eventbus.Publisher`
(`Publish(ctx, eventbus.Event) error`, per `.claude/rules/event-interfaces.md`),
not `core.EventAppender` (deleted by D-01); the subject becomes a qualified dot
subject; error codes follow the existing `core.Engine` bodies.

**Analog B — what MOVES (the source of truth for the port surface):**

`core.Engine` is confirmed to be exactly **1 field + 3 methods**, matching
CONTEXT.md D-03:

| Symbol | Site | Notes |
|---|---|---|
| `Engine struct{ store EventAppender }` | `internal/core/engine.go:33-35` | one field |
| `NewEngine(store) *Engine` | `internal/core/engine.go:44-49` | **panics on nil**, incl. typed-nil via `isNilEventAppender` (`engine.go:54-62`) — *this reflect-based typed-nil guard is non-obvious logic; carry it across or delete it deliberately* |
| `HandleConnect(ctx, char)` | `internal/core/engine.go:65-85` | → `Arrived`/`EmitArrive` |
| `HandleDisconnect(ctx, char, reason)` | `internal/core/engine.go:88-108` | → `Departed`/`EmitLeave` |
| `EndSession(ctx, char, sessionID, cause, reason)` | `internal/core/engine_end_session.go:33-79` | **RESEARCH open-Q #2**: unaddressed by D-04's rename. |
| `ArrivePayload` / `LeavePayload` / `CommandResponsePayload` | `internal/core/engine.go:17-30` | ⚠️ `CommandResponsePayload` is used by `internal/grpc`'s `emitCommandResponse`, **not** by Engine — it is NOT presence and must not travel to `internal/presence`. It is vocabulary-adjacent → candidate for the D-05 leaf. |

⚠️ **`EndSession` carries load-bearing, non-obvious logic that a mechanical move
will silently drop.** It is not a payload-marshal twin of the other two:

```go
// Source: internal/core/engine_end_session.go:33-45,68-70 [VERIFIED live]
func (e *Engine) EndSession(
	_ context.Context,   // ← caller ctx DELIBERATELY ignored
	char CharacterRef, sessionID string, cause string, reason string,
) error {
	// NOTE: The caller's ctx is intentionally NOT consulted here. A pre-
	// cancelled ctx (client hangup just before EndSession ran) MUST NOT skip
	// the audit-critical session_ended append. The append below uses a fresh
	// background ctx bounded by sessionTerminalCommitTimeout, which is the
	// only deadline that gates this write.
	...
	appendCtx, cancel := context.WithTimeout(context.Background(), sessionTerminalCommitTimeout)
	defer cancel()
```
Also its actor selection is cause-dependent (`engine_end_session.go:56-59`:
`quit` → `ActorCharacter`, else `ActorSystem`). **Both must survive verbatim.**
`sessionTerminalCommitTimeout = 5 * time.Second` (`engine_end_session.go:18`)
travels with it.

**Analog C — the package/wiring shape (constructor takes config, not live values):**
see §"Handles, not live values" below — `internal/world/setup` is the model.

---

### 2. Event-type vocabulary leaf (D-05)

**Two live analogs. Both are true leaves (verified `go list -deps` → zero internal deps).**

**Analog A (preferred shape) — `internal/telnet/gamenotice/`.** A 2-file
package (`gamenotice.go` + `gamenotice_test.go`), pure string transforms, no
imports, colocated near its consumer. This is the repo's canonical "tiny
vocabulary leaf" and — notably — it already lives *under* `internal/telnet/`,
proving the gateway may own a leaf.

**Analog B — `internal/naming/`.** ⚠️ **Confirms D-16's open question:**
`go list -deps ./internal/naming` returns exactly one package (itself) —
`internal/naming` **IS a true leaf**. Per D-15's leaf-only principle,
`naming.Theme` (`internal/telnet/guest_auth.go:21,28`) **may stay**.

**Full leaf inventory** (`go list -deps` → zero internal/pkg deps other than
self), for the planner's destination choice:

```
internal/access/policy/types      internal/idgen         internal/observability
internal/eventbus/audit/auditheader  internal/invregistry  internal/pgnanos
internal/eventbus/codec           internal/lifecycle     internal/session      ← already a leaf
internal/eventbus/crypto/kek      internal/logging       internal/telnet/gamenotice
internal/eventbus/cursor/cursorv1 internal/naming        internal/tls
internal/eventbus/natsconn        internal/gatewaymetrics internal/xdg
internal/eventbus/telemetry
```

⚠️ **This list corroborates RESEARCH FINDING-2 and adds one fact CONTEXT.md
does not state:** `internal/session` is *already* a leaf, yet
`session.DefaultLeaseRefreshInterval` (`internal/session/reaper.go:25`,
`= 15 * time.Second`) must still relocate to satisfy D-15's letter. The
constant's **value must not change** (ASVS V3, per RESEARCH).

**Leaf-package doc-comment + SPDX shape to copy** — every leaf carries the SPDX
pair then a package doc; `gamenotice_test.go` shows the table-driven test shape
these leaves use.

---

### 3. Consumer-defined interface (Pattern 1 / FINDING-1 / D-02)

**RESEARCH cites `hostcap.SessionAdmin` as the precedent. There is a stronger,
denser one the research does not name: the `*Provider` family in `*/setup/`.**
It is the repo's *systematic* application of the pattern — 20+ sites — and it
is simultaneously the D-09 analog.

**Canonical excerpt — declaration site (the consumer declares it):**

```go
// Source: internal/world/setup/subsystem.go:19-31 [VERIFIED live]
// PoolProvider provides a database connection pool. Implemented by the
// database subsystem without requiring a direct import.
type PoolProvider interface {
	Pool() *pgxpool.Pool
}

// EngineProvider provides an ABAC policy engine. Implemented by the
// ABAC subsystem without requiring a direct import.
type EngineProvider interface {
	Engine() types.AccessPolicyEngine
}
```

Note the doc-comment convention — each states *"Implemented by the X subsystem
**without requiring a direct import**"*. That sentence IS the pattern's
rationale, and the planner should reproduce it on the new `auth` interface.

**Full site inventory** (`rg -n 'Provider interface' internal/`), for the planner
to pick the nearest neighbour:

| Site | Interfaces |
|---|---|
| `internal/world/setup/subsystem.go:21,27` | `PoolProvider`, `EngineProvider` |
| `internal/world/setup/relay_subsystem.go:34` | `EventBusProvider` |
| `internal/access/setup/subsystem.go:29` | `PoolProvider` |
| `internal/auth/setup/subsystem.go:22` | `PoolProvider` |
| `internal/session/setup/subsystem.go:22` | `PoolProvider` |
| `internal/bootstrap/setup/subsystem.go:34,39,44,50,56,62` | 6 providers — the **densest** example |
| `internal/plugin/setup/subsystem.go:51,65,75,80,86` | `EngineProvider`, `PolicyInstallerProvider`, `WorldServiceProvider`, `SessionProvider`, `AdminDepsProvider` |
| `internal/eventbus/audit/subsystem.go:187,193` | `JSProvider`, `PoolProvider` |
| `internal/grpc/auth_handlers.go:54,65,70` | `AuthServiceProvider`, … |
| `internal/plugin/hostcap/capabilities.go:58` | `SessionAdmin` (RESEARCH's cited precedent) |

**Apply to FINDING-1** — `internal/auth` declares (in `auth_service.go`, next to
its other narrow ports `PlayerRepository`/`PasswordHasher`):

```go
// TODAY [VERIFIED live]:
//   internal/auth/auth_service.go:28   engine       *core.Engine
//   internal/auth/auth_service.go:39   func WithGameSessionFanout(engine *core.Engine, gameSessions gamesession.Store) ServiceOption
//   internal/auth/auth_service.go:76   func (s *Service) ConfigureGameSessionFanout(engine *core.Engine, gameSessions gamesession.Store)
```
The **exact** method set `auth` calls on the engine (verified — both live in the
eviction fanout, `auth_service.go:102` and `:110`):

```go
	if dcErr := s.engine.HandleDisconnect(ctx, char, "evicted"); dcErr != nil { ... }
	if endErr := s.engine.EndSession(ctx, char, child.ID,
		core.SessionEndedCauseEvicted,
		"Session evicted — you logged in elsewhere."); endErr != nil { ... }
```
⇒ the consumer-defined interface is **exactly 2 methods** (`HandleDisconnect`,
`EndSession` — under their new names). `HandleConnect` is NOT called by `auth`.

⚠️ **The interface's params must also be cycle-free.** `auth` passes
`core.CharacterRef` (`auth_service.go:97-101`). `CharacterRef` stays in
`internal/core` and `core` remains a leaf, so this is fine — but if the planner
moves `CharacterRef`, re-check.

**Test-fake shape for the new interface** — `internal/plugin/setup/system_broadcaster_test.go:20-22`
is the in-package hand-rolled fake idiom used for exactly this kind of narrow port:

```go
// Source: internal/plugin/setup/system_broadcaster_test.go:18-22 [VERIFIED live]
// noopAppender is a core.EventAppender that discards events — enough to assert
// the SessionAdmin backing is wired (the backing's behavior is covered by the
// hostcap broadcaster tests).
type noopAppender struct{}

func (noopAppender) Append(context.Context, core.Event) error { return nil }
```

---

### 4. The ONE surviving broadcast builder (D-02)

**There are exactly two builder-ish sites, and the analog IS the survivor.**

| Site | Role | Disposition |
|---|---|---|
| `internal/plugin/hostcap/system_broadcaster.go:35-67` | the **real builder** (marshals `{"message":…}`, stamps system actor, appends) | **KEEP** → becomes the one builder (re-typed to `eventbus.Publisher`). Its `NewSystemBroadcaster(appender) SessionAdmin` ctor shape is already correct. |
| `internal/plugin/setup/subsystem.go:494-499` (`ConfigureSystemBroadcaster`) | **not a duplicate builder** — a *late-binding wiring hook* that calls `hostcap.NewSystemBroadcaster` | **KEEP as wiring**, re-type its param |
| `internal/command/types.go:622-641` (`Services.BroadcastSystemMessage`) | the **duplicate** payload construction | **DELETE the body**; `Services` keeps only a consumer-defined 1-method port |

```go
// Source: internal/plugin/setup/subsystem.go:494-499 [VERIFIED live]
func (s *PluginSubsystem) ConfigureSystemBroadcaster(appender core.EventAppender) {
	if s.luaHost == nil || appender == nil {
		return
	}
	s.luaHost.SetSessionAdmin(hostcap.NewSystemBroadcaster(appender))
}
```

⚠️ **Runtime-symmetry note the planner must carry** — the doc comment at
`subsystem.go:490-492` states: *"The binary host needs no equivalent:
SessionAdminService is Lua-only (not in `hostcap.BinaryDefaultSet`)."* This is a
**pre-existing, documented** Lua-only surface. Re-typing the port MUST NOT
change that; do not "fix parity" here (see `.claude/rules/plugin-runtime-symmetry.md`
§"Permitted asymmetry").

⚠️ Its comment also names the **late-binding rationale** that D-09 interacts
with: *"It MUST be called from the gRPC subsystem's Start once the event
appender exists: the appender is built after the plugin subsystem starts, so a
construction-time option cannot reach it."* Under D-09's handles-not-values
rule this hook may become a plain `DependsOn` edge — the planner should decide
explicitly rather than inherit it.

---

### 5. `lifecycle.Subsystem` Prepare/Activate (D-11) — TRUE blast radius

**The interface being modified:**

```go
// Source: internal/lifecycle/subsystem.go:42-58 [VERIFIED live]
type Subsystem interface {
	// ID returns the typed identifier for this subsystem.
	ID() SubsystemID
	// DependsOn returns the subsystems that must be started before this one.
	DependsOn() []SubsystemID
	// Start initializes the subsystem. It MUST be idempotent.
	// A non-nil error is fatal — the server will not start.
	Start(ctx context.Context) error
	// Stop shuts down the subsystem. It MUST be idempotent and
	// MUST NOT block indefinitely.
	Stop(ctx context.Context) error
}
```

**Every implementation, enumerated (`rg -n 'DependsOn\(\) \[\]lifecycle\.SubsystemID'`):**

| # | Impl | Site | Kind |
|---|---|---|---|
| 1 | `store.DatabaseSubsystem` | `internal/store/subsystem.go:43` | prod |
| 2 | `eventbus.Subsystem` | `internal/eventbus/subsystem.go:77` | prod |
| 3 | `setup.ABACSubsystem` | `internal/access/setup/subsystem.go:65` | prod |
| 4 | `setup.AuthSubsystem` | `internal/auth/setup/subsystem.go:59` | prod |
| 5 | `setup.WorldSubsystem` | `internal/world/setup/subsystem.go:58` | prod |
| 6 | `setup.OutboxRelaySubsystem` | `internal/world/setup/relay_subsystem.go:74` | prod |
| 7 | `setup.SessionSubsystem` | `internal/session/setup/subsystem.go:47` | prod |
| 8 | `setup.BootstrapSubsystem` | `internal/bootstrap/setup/subsystem.go:103` | prod |
| 9 | `setup.PluginSubsystem` | `internal/plugin/setup/subsystem.go:154` | prod |
| 10 | `audit.Subsystem` | `internal/eventbus/audit/subsystem.go:255` | prod |
| 11 | `chain.VerifierSubsystem` | `internal/eventbus/audit/chain/verifier_subsystem.go:67` | prod |
| 12 | `dek.CheckpointSweepSubsystem` | `internal/eventbus/crypto/dek/sweep.go:76` | prod |
| 13 | `cluster.registry` | `internal/cluster/registry.go:174` | prod (unexported) |
| 14 | `socket.AdminSocketSubsystem` | `internal/admin/socket/subsystem.go:64` | prod |
| 15 | `policy.CryptoPolicySubsystem` | `internal/admin/policy/subsystem.go:40` | prod |
| 16 | `grpcSubsystem` | `cmd/holomush/sub_grpc.go:170` | prod (`package main`) |
| 17 | `stubSubsystem` | `cmd/holomush/core_subsystems_test.go:22` | **test** |
| 18 | `stubSubsystem` | `internal/lifecycle/orchestrator_test.go:29` | **test** |
| 19 | `stubRegistry` | `internal/eventbus/crypto/invalidation/coordinator_error_test.go:35` | **test** |

⇒ **16 production + 3 test impls.** CONTEXT.md D-11's "~18 subsystems" is close;
the exact figure is **16 prod / 19 total**. Plus the **new TLS subsystem** (D-14)
→ 17 prod.

**Shape to follow per impl** — `WorldSubsystem` is the cleanest (config struct +
ID + DependsOn + Start that only *then* touches live resources):

```go
// Source: internal/world/setup/subsystem.go:53-63 [VERIFIED live]
// NewWorldSubsystem creates a WorldSubsystem using the provided WorldSubsystemConfig.
// It does not allocate or start any runtime resources; call Start to initialize the service and transactor.
func NewWorldSubsystem(cfg WorldSubsystemConfig) *WorldSubsystem {
	return &WorldSubsystem{cfg: cfg}
}

// ID returns SubsystemWorld.
func (s *WorldSubsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemWorld }

// DependsOn returns [SubsystemDatabase, SubsystemABAC].
func (s *WorldSubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{lifecycle.SubsystemDatabase, lifecycle.SubsystemABAC}
}
```

**The natural Prepare/Activate seam is already visible in `WorldSubsystem.Start`:**

```go
// Source: internal/world/setup/subsystem.go:64-70 [VERIFIED live]
// Start creates all world repositories, transactor, and WorldService.
// codecov:ignore — tested by integration and E2E tests
func (s *WorldSubsystem) Start(ctx context.Context) error {
	pool := s.cfg.DB.Pool()          // ← ACQUIRE (Prepare)
	engine := s.cfg.ABAC.Engine()    // ← ACQUIRE (Prepare)
	transactor := worldpostgres.NewTransactor(pool)
	s.service = world.NewService(world.ServiceConfig{ ... })   // ← wire
```
Note `codecov:ignore — tested by integration and E2E tests` is the established
comment on subsystem `Start` bodies; reproduce it on new/split methods.

**D-13.2 evidence (the idempotency contract's origin):** the analog *documents*
CONTEXT.md's claim verbatim —

```go
// Source: internal/access/setup/subsystem.go:69-78 [VERIFIED live]
// Start builds the ABAC stack, registers health, and starts the poller.
// Start is idempotent: if the subsystem is already started, it returns nil
// immediately. This allows the ABAC subsystem to be pre-started in core
// boot when admin handler construction needs Resolver() before the
// orchestrator drives StartAll. Mirrors store.DatabaseSubsystem.Start.
// codecov:ignore — tested by integration and E2E tests
func (s *ABACSubsystem) Start(ctx context.Context) error {
	if s.stack != nil {
		return nil // already started — guard against double-start (would launch a duplicate poller goroutine)
	}
```
⚠️ **But the parenthetical is a second, independent reason** RESEARCH open-Q #5
says it could not find: *"would launch a duplicate poller goroutine."* The guard
is **not purely** a pre-start artifact — at least for ABAC it also protects a
goroutine. D-13.2 must be decided per-subsystem, not globally retired.

**`StopAll` — the LOW-7 target (D-14):**

```go
// Source: internal/lifecycle/orchestrator.go:79-96 [VERIFIED live]
// StopAll stops subsystems in reverse start order.
func (o *Orchestrator) StopAll(ctx context.Context) {
	for i := len(o.startOrder) - 1; i >= 0; i-- {
		id := o.startOrder[i]
		sub := o.subsystems[id]
		slog.InfoContext(ctx, "stopping subsystem", "subsystem", id.String())
		if err := sub.Stop(ctx); err != nil {
			slog.ErrorContext(ctx, "subsystem stop error", "subsystem", id.String(), "error", err)
		}
	}
}
```
Note `StopAll` returns **nothing** and never checks `ctx.Done()` — LOW-7's fix
touches this signature *and* is what `StartAll`'s rollback path calls
(`orchestrator.go:60`), so a signature change ripples there too.

---

### 6. Handles, not live values (D-09)

**No new pattern needed — the correct shape is already the majority shape.**
`WorldSubsystem`/`ABACSubsystem` take a **config struct of provider interfaces**
and resolve at `Start`. The 5 eager starts are deviations *from the repo's own
existing pattern*, not a gap in it.

```go
// Source: internal/world/setup/subsystem.go:33-45 [VERIFIED live]
// WorldSubsystemConfig configures the world subsystem.
type WorldSubsystemConfig struct {
	DB   PoolProvider
	ABAC EngineProvider
	...
}
```
```go
// Source: internal/access/setup/subsystem.go:52-59 [VERIFIED live]
// NewABACSubsystem creates an ABAC subsystem. No live resources are allocated.
// If cfg.AuditMode is empty, it defaults to audit.ModeDenialsOnly.
func NewABACSubsystem(cfg ABACSubsystemConfig) *ABACSubsystem {
	if cfg.AuditMode == "" {
		cfg.AuditMode = audit.ModeDenialsOnly
	}
	return &ABACSubsystem{cfg: cfg}
}
```
*"No live resources are allocated"* / *"It does not allocate or start any runtime
resources"* — **this doc sentence is the D-09 acceptance criterion in prose.**
Every constructor the planner rewrites should be able to carry it truthfully.

**Apply to `cluster.NewSubsystem`** (which today "requires a non-nil `*nats.Conn`
**at construction time**", `core.go:455`): the analog says take a
`ConnProvider interface { Conn() *nats.Conn }` in a config struct and call it in
`Start`, with `DependsOn(SubsystemEventBus)`. `internal/world/setup/relay_subsystem.go:34`
(`EventBusProvider`) is the nearest existing instance of exactly this.

**Accessor panic-guard idiom to preserve** (the tell D-09 targets):
```go
// Source: internal/plugin/setup/subsystem.go:502-507 [VERIFIED live]
// CommandRegistry returns the command Registry. Panics if called before Start().
func (s *PluginSubsystem) CommandRegistry() *command.Registry {
	if s.cmdRegistry == nil {
		panic("plugin/setup: CommandRegistry() called before Start()")
	}
	return s.cmdRegistry
}
```
Under D-09 these guards stay (they *are* the provider contract); what dies is
the **caller-side pre-`Start()`** that exists to dodge them.

---

### 7. TLS subsystem (D-14 phantom `SubsystemTLS`)

**No dedicated analog — but none is needed:** `internal/tls` is already a **true
leaf** (verified). The subsystem wrapper follows `WorldSubsystem` (§5) with
`DependsOn() []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}` (gameID).
The ID already exists at `internal/lifecycle/subsystem.go:18` → **no const-block
edit, no `task generate`** (corroborates RESEARCH FINDING-9).

---

## Test Analogs

### T1. D-07 concurrent-publisher pagination regression → **exact analog exists**

`test/integration/eventbus_e2e/cursor_concurrent_test.go` is the **same bug
class, one layer down**: it was written for `holomush-suos`, when the *internal*
eventbus cursor was ULID-keyed. D-07 is the identical defect at the
*plugin `ReplayTail`* boundary. **This test should be cloned, not designed.**

```go
// Source: test/integration/eventbus_e2e/cursor_concurrent_test.go:22-53 [VERIFIED live]
// Cursor concurrent pagination specs reproduce the exact scenario the
// holomush-suos bead was filed to fix: two publishers writing to the same
// subject at high concurrency, where ULID lex order deliberately disagrees
// with JetStream stream-sequence order.
//
// Pre-suos (ULID-keyed pagination): the internal cursor advanced by ULID lex
// position rather than JS stream sequence. ... so the cursor could
// skip or revisit events between internal page loads — causing drops or
// duplicates in the result set.
//
// Post-suos (seq-keyed pagination): the internal cursor (AfterSeq/BeforeSeq)
// is derived from the last event's JS Seq. ...
var _ = Describe("Cursor pagination under concurrent publishers with drifted ULIDs", func() {
	const (
		publishersCount = 2
		eventsPerPub    = 50
		totalEvents     = publishersCount * eventsPerPub
		// pageSize is intentionally small relative to totalEvents: this forces
		// the crossoverStream to make multiple internal hot-tier page loads,
		// exercising the seq-keyed cursor advancement on each boundary.
		pageSize = 20
	)
```

**The reusable ULID-drift generator (copy verbatim, retarget the reader):**
```go
// Source: test/integration/eventbus_e2e/cursor_concurrent_test.go:58-91 [VERIFIED live]
	publishConcurrent := func(ctx context.Context, pub eventbus.Publisher, subject eventbus.Subject, eventType eventbus.Type) {
		var wg sync.WaitGroup
		errCh := make(chan error, publishersCount*eventsPerPub)
		for p := 0; p < publishersCount; p++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < eventsPerPub; i++ {
					id, err := ulid.New(ulid.Timestamp(time.Now()), crand.Reader)
					if err != nil { errCh <- err; return }
					ev := eventbus.Event{
						ID: id, Subject: subject, Type: eventType,
						Timestamp: time.Now().UTC(),
						Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
						Payload:   []byte("p"),
					}
					if pubErr := pub.Publish(ctx, ev); pubErr != nil { errCh <- pubErr; return }
				}
			}()
		}
		wg.Wait()
		close(errCh)
		for err := range errCh { Expect(err).NotTo(HaveOccurred(), "publisher goroutine returned an error") }
	}
```
Key mechanics: `crand.Reader` + fresh `ulid.New` per event (**not**
`core.NewULID()` — monotonic ULIDs would defeat the drift); distinct subject per
spec to avoid cross-talk (`:101-103`); `pageSize` ≪ `total` to force multiple
page loads; 60s ctx via `DeferCleanup(cancel)`.

**Suite-entry + `suiteT` idiom (required if new helpers take `*testing.T`):**
```go
// Source: test/integration/eventbus_e2e/cursor_concurrent_suite_test.go:15-29 [VERIFIED live]
// suiteT captures the testing.T from the Ginkgo bootstrap so spec bodies can
// invoke local helpers (freshPool, drainStream, currentStreamLastSeq,
// buildReader) which take *testing.T. Mirrors the world_suite_test.go pattern.
var suiteT *testing.T

func TestEventbusE2E(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "EventbusE2E Suite")
}
```
⚠️ **The package already has a Ginkgo entry point (`TestEventbusE2E`) — do NOT
add a second `RunSpecs`.** Existing helpers to reuse: `freshBus()`,
`freshPool()`, `currentStreamLastSeq(suiteT, bus)`, `drainStream`, `buildReader`.
Header: `//go:build integration` + `package eventbus_e2e_test` + the two
`//nolint:revive // ginkgo convention` dot-imports.

### T2. Gateway import gate (D-17) + the new closure test

**Analog:** `cmd/holomush/gateway_imports_test.go` — the file being amended.

```go
// Source: cmd/holomush/gateway_imports_test.go:101-109 [VERIFIED live]
var forbidden = []string{
	"github.com/holomush/holomush/internal/world",
	"github.com/holomush/holomush/internal/access",
	"github.com/holomush/holomush/internal/store",
	"github.com/holomush/holomush/internal/plugin",
	"github.com/holomush/holomush/internal/eventbus",
	"github.com/holomush/holomush/internal/auth/service", // ← PHANTOM: package does not exist. See warning below.
	"github.com/holomush/holomush/internal/command",
}
```

> ⚠️ **This excerpt is the CURRENT (broken) state — do NOT copy the
> `internal/auth/service` line forward.** Added 2026-07-15 after a cross-AI review
> caught it; verified live: **`internal/auth/service` does not exist** — the
> package files sit directly in `internal/auth`, and `go list ./internal/auth/...`
> yields no `service` subpackage. A forbidden entry naming a package that cannot
> exist never matches: it is a dead rule protecting nothing. **07-04 Task 2
> replaces it with `"github.com/holomush/holomush/internal/auth"`** and Task 3
> fixes the identical phantom in `docs/architecture/invariants.yaml:2345`'s
> INV-EVENTBUS-1 summary. This excerpt is retained verbatim because it is what is
> on disk today (that is what makes it a useful analog for *editing* the file) —
> but the corrected list is the target.
```go
// Source: cmd/holomush/gateway_imports_test.go:111-138 [VERIFIED live]
// TestGatewayImportsAreOnlyProtocolTranslation is INV-EVENTBUS-1. Gateway-side
// files MUST NOT import domain packages. Core-process files are excluded
// via coreOnlyFiles.
func TestGatewayImportsAreOnlyProtocolTranslation(t *testing.T) {
	pkgs, err := packages.Load(
		&packages.Config{
			Mode: packages.NeedName | packages.NeedFiles |
				packages.NeedSyntax | packages.NeedImports |
				packages.NeedTypes,
			Tests: true,
		},
		"github.com/holomush/holomush/cmd/holomush",
		"github.com/holomush/holomush/internal/web/...",
		"github.com/holomush/holomush/internal/telnet/...",
	)
	require.NoError(t, err)
	require.Empty(t, packages.PrintErrors(pkgs))
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			goFile := pkg.Fset.Position(file.Pos()).Filename
			checkFile(t, pkg.PkgPath, goFile, file)
		}
	}
}
```
Confirms **RESEARCH FINDING-3**: `checkFile` iterates `file.Imports` —
**direct imports only** (`:148`). The new **closure** test is genuinely new
shape; the nearest analog for its oracle is `packages.Load` with
`packages.NeedDeps | packages.NeedImports` and a recursive walk of
`pkg.Imports`, or shelling `go list -deps`. Header shape to copy:
`//go:build !integration` + `package main` (**not** `main_test`).

⚠️ File header also carries `Tests: true` with a load-bearing comment
(`:120-123`) — preserve it; without it gateway-side `_test.go` files bypass the
gate.

### T3. Topological start-order pin (D-14 MEDIUM-11) + the count cascade

**Analog:** `cmd/holomush/core_subsystems_test.go`.

```go
// Source: cmd/holomush/core_subsystems_test.go:16-24 [VERIFIED live]
// stubSubsystem is a minimal lifecycle.Subsystem for testing the
// productionSubsystems helper. Only ID() is read by the test.
type stubSubsystem struct {
	id lifecycle.SubsystemID
}

func (s stubSubsystem) ID() lifecycle.SubsystemID          { return s.id }
func (s stubSubsystem) DependsOn() []lifecycle.SubsystemID { return nil }
func (s stubSubsystem) Start(_ context.Context) error      { return nil }
func (s stubSubsystem) Stop(_ context.Context) error       { return nil }
```
⚠️ **`stubSubsystem.DependsOn()` returns `nil`** — a topo-order pin test CANNOT
reuse this stub as-is; it must carry real deps (the
`internal/lifecycle/orchestrator_test.go:29` stub does: `return s.deps` — **that**
is the right analog for the pin test).

**The count cascade is concrete and hard-coded:**
```go
// Source: cmd/holomush/core_subsystems_test.go:26-52 [VERIFIED live]
// allStubs returns the full 16-element stub list in production order.
// ...
// Index 14 (SubsystemRekeyCheckpointSweep) was added in sub-epic E Task 6.
// Index 15 (SubsystemOutboxRelay) was added in Phase 5 05-07 (MODEL-04 relay).
func allStubs() [16]stubSubsystem { ... }
```
Adding TLS ⇒ `[16]` → `[17]` **plus** every call site that spreads
`s[0]…s[15]` positionally (e.g. `:57-61`). This is exactly the documented
cascade in CONTEXT.md §Specific Ideas.

⚠️ **Drift vs CONTEXT.md:** D-14 (LOW-8) says `productionSubsystems` takes
**15** positional params. Live source is **16**:
```go
// Source: cmd/holomush/core.go:1462-1472 [VERIFIED live]
func productionSubsystems(
	dbSub, abacSub, authSub, worldSub,
	sessionSub, pluginSub, bootstrapSub,
	cryptoChainVerifierSub,
	eventBusSub, clusterSub, auditSub,
	cryptoPolicySub,
	grpcSub,
	adminSub,
	rekeyCheckpointSweepSub,
	outboxRelaySub lifecycle.Subsystem,
) []lifecycle.Subsystem {
```
RESEARCH says 16 and is correct. Use **16 → 17 (with TLS)**.

---

## Shared Patterns

### SPDX + package doc (every new file)
```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package lifecycle provides subsystem lifecycle management, health tracking,
// and readiness gating for the core server.
package lifecycle
```
*(Source: `internal/lifecycle/subsystem.go:1-6`.)* Applied by `task fmt` via
`license-eye` — **commit the mutation**.

### Error handling — `oops.Code(...)` at boundaries
`oops.Code("SYSTEM_BROADCAST_FAILED").Wrap(err)` (`hostcap/system_broadcaster.go:59`);
`oops.With("operation", "marshal_arrive_payload").Wrap(err)` (`core/engine.go:68`);
`oops.Code("ABAC_SETUP_FAILED").Wrap(err)` (`access/setup/subsystem.go:106`).
**Apply to:** every new `presence` / broadcast / subsystem method. Preserve
existing codes verbatim across the move (`SESSION_ENDED_APPEND_FAILED`,
`engine_end_session.go:72`) — tests may assert them via `errutil.AssertErrorCode`.

### Structured logging — context-carrying variants
`slog.InfoContext(ctx, "starting subsystem", "subsystem", id.String())`
(`orchestrator.go:57`) — the shape for all new lifecycle logging.
`.claude/rules/logging.md` / `sloglint context: scope` is mechanically enforced.

### Constructor doc sentence as a contract
*"No live resources are allocated"* (`access/setup/subsystem.go:52`) /
*"It does not allocate or start any runtime resources; call Start to initialize…"*
(`world/setup/subsystem.go:53-54`). **Apply to:** every D-09-rewritten constructor.

### `codecov:ignore` on subsystem `Start`
`// codecov:ignore — tested by integration and E2E tests`
(`world/setup/subsystem.go:65`, `access/setup/subsystem.go:74`).
**Apply to:** new `Start`/`Prepare`/`Activate` bodies.

---

## No Analog Found / Partial

| Artifact | Why |
|---|---|
| **Gateway transitive-closure test** | **Partial.** `gateway_imports_test.go` supplies the `packages.Load` + failure-report skeleton and the `coreOnlyFiles`/`forbidden` scoping, but **no in-tree test computes a transitive closure**. The recursive-walk (or `go list -deps` shell-out) oracle is genuinely new. Nearest neighbour is `test/meta/` (which does AST/meta assertions but not build-graph closure). |
| **`Prepare`/`Activate` two-phase lifecycle + rollback (D-13.1)** | **No in-tree analog.** `Orchestrator` has exactly one phase; `StopAll` (`orchestrator.go:79-96`) walks only `startOrder`. "Activate fails after N prepared" has no precedent anywhere in the repo. This is genuine design work — consistent with CONTEXT.md D-13 assigning it to the planner. |
| **D-08 "`hostv1.Event` gains no seq field" guard test** | **No analog.** No in-tree test asserts the *absence* of a proto field. Nearest is `test/meta/`'s registry/census meta-tests (assert-a-set-is-exact) — a proto-descriptor field-name census would follow that spirit but is new construction. |

---

## Contradictions With CONTEXT.md / RESEARCH.md Surfaced By This Mapping

1. **`productionSubsystems` has 16 params, not 15** (CONTEXT.md D-14/LOW-8 says 15).
   `cmd/holomush/core.go:1462-1472`. RESEARCH's "16" is correct. With TLS → 17.
2. **`Start`-idempotency is NOT purely a pre-start artifact** (CONTEXT.md D-13.2;
   RESEARCH open-Q #5 found no second reason). `internal/access/setup/subsystem.go:77`
   documents one: *"would launch a duplicate poller goroutine."* D-13.2 must be
   decided **per-subsystem**.
3. **`internal/naming` IS a true leaf** — confirmed by `go list -deps`
   (D-16 asked the planner to confirm). `naming.Theme` may stay in `internal/telnet`.
4. **`internal/session` is already a leaf too**, yet D-15 forbids it wholesale —
   corroborates RESEARCH FINDING-2 (the decision stands; the *rationale* must not
   be restated as "reaches the DB").
5. **`core.CommandResponsePayload` lives in `engine.go:17-19` but is not Engine's** —
   a naïve "move engine.go → presence" drags a `internal/grpc` payload type into
   `internal/presence`. It is vocabulary → D-05 leaf candidate.
6. **`core.NewEngine`'s typed-nil `reflect` guard** (`engine.go:44-62`) is real
   logic, contradicting D-03's "Engine has **no logic to protect**". Carry or drop
   it deliberately.
7. **D-02's builder is Lua-only by documented design** (`plugin/setup/subsystem.go:490-492`)
   — a permitted asymmetry. Do not "fix" it while re-typing the port.

## Metadata

**Analog search scope:** `internal/...`, `cmd/holomush/`, `test/integration/`
**Oracles used:** `go list -deps` (leaf census, build graph), `rg` (symbol sites), direct `Read`
**Pattern extraction date:** 2026-07-15 @ `be030a368`
