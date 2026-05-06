# History Reader Crypto Options — Design Spec

**Date:** 2026-05-05
**Bead:** `holomush-ojw1.7`
**Parent epic:** `holomush-ojw1` Phase 3: EventSink encrypt + AuthGuard + decrypt-on-fanout + downgrade fence

## Problem

`history.NewReader` cannot forward crypto dependencies (AuthGuard, DEKManager, DecryptAuditEmitter) to its default hot and cold tiers. The per-tier option types exist (`HotTierOption` at `hot_jetstream.go:28`, `ColdTierOption` at `cold_postgres.go:43`) and `WithCryptoCold` already forwards cold-tier options, but:

- No `WithCryptoHot` equivalent exists — the hot tier is built bare at `tier.go:247`
- Production at `sub_grpc.go:newHistoryReader` passes no crypto options
- The E2E test (`e2e_test.go`) reinvents the cold dispatch chain with a 145-line `e2eColdTier` shim rather than using the real `newPostgresColdTier` with options

## Goals

- History Reader MUST support forwarding crypto options to the default hot tier, achieving parity with the existing `WithCryptoCold` cold-tier path
- A convenience option MUST exist to wire both tiers with the same AuthGuard, DEKManager, and DecryptAuditEmitter in a single call
- Production MUST have a call-site API (`newHistoryReader`) that accepts (or skips) crypto dependencies
- E2E tests MUST use the real production cold tier wired via options rather than a duplicated 145-line shim
- Crypto component construction in production is explicitly OUT OF SCOPE — this task delivers the plumbing; a separate task provides the water

## Design

### 1. Reader struct changes (`tier.go`)

Add `hotOpts` field mirroring existing `coldOpts`:

```go
hotOpts []HotTierOption  // forwarded to newJetStreamHotTier; mirror of coldOpts
```

Wire it in `NewReader` at line 247:

```go
// Before:
r.hot = newJetStreamHotTier(r.js, r.selector, r.now)
// After:
r.hot = newJetStreamHotTier(r.js, r.selector, r.now, r.hotOpts...)
```

### 2. New Option functions (`tier.go`)

Three additions near `WithCryptoCold` (line 159):

```go
// WithCryptoHot forwards HotTierOption values to the default JetStream hot
// tier when NewReader builds it. Mirrors WithCryptoCold for hot/cold parity.
// No-op when the caller supplies WithHotTier. Multiple calls accumulate.
func WithCryptoHot(opts ...HotTierOption) Option {
    return func(r *Reader) { r.hotOpts = append(r.hotOpts, opts...) }
}

// WithHistoryAuth wires AuthGuard + DEKManager + DecryptAuditEmitter into
// BOTH hot and cold tiers. This is the common case — production and tests
// always configure tiers symmetrically.
func WithHistoryAuth(
    g eventbus.SessionAuthGuard,
    m eventbus.SessionDEKManager,
    em eventbus.SessionAuditEmitter,
) Option {
    return func(r *Reader) {
        r.hotOpts = append(r.hotOpts,
            WithHistoryAuthGuard(g),
            WithHistoryDEKManager(m),
            WithHistoryDecryptAuditEmitter(em),
        )
        r.coldOpts = append(r.coldOpts,
            WithColdHistoryAuthGuard(g),
            WithColdHistoryDEKManager(m),
            WithColdHistoryDecryptAuditEmitter(em),
        )
    }
}
```

### 3. Production wiring (`sub_grpc.go`)

Change `newHistoryReader` signature to accept optional auth parameters:

```go
func newHistoryReader(
    js jetstream.JetStream,
    pool *pgxpool.Pool,
    cfg eventbus.Config,
    owners *audit.OwnerMap,
    router history.PluginHistoryRouter,
    guard eventbus.SessionAuthGuard,     // NEW — nil = passthrough
    dekMgr eventbus.SessionDEKManager,   // NEW — nil = passthrough
    auditEm eventbus.SessionAuditEmitter, // NEW — nil = passthrough
) eventbus.HistoryReader {
```

At the call site in `Start()` (line 297), pass nil for all three:

```go
historyReader := newHistoryReader(js, pool, s.cfg.EventBus.Config(), owners, router, nil, nil, nil)
```

When non-nil, `newHistoryReader` appends `WithHistoryAuth(guard, dekMgr, auditEm)` to opts. Behavior unchanged when nil — `decodeAuthorizeAndDispatch` returns `EVENTBUS_HISTORY_AUTH_GUARD_NIL` for sensitive events (same as today).

### 4. E2E test cleanup (`e2e_test.go`)

Replace `buildColdReader` to use `WithHistoryAuth` instead of the `e2eColdTier` shim:

```go
func buildColdReader(env *e2eEnv) *history.Reader {
    farFuture := time.Now().UTC().Add(100 * 365 * 24 * time.Hour)
    return history.NewReader(env.bus.JS, env.pool,
        eventbus.Config{}.Defaults().StreamMaxAge,
        func() time.Time { return farFuture },
        history.WithHistoryAuth(env.guard, env.dekMgr, env.auditEm),
    )
}
```

Delete the following (no longer needed):

- `e2eColdTier` type (lines 290-295)
- `e2eColdTier.Read` (lines 297-333)
- `dispatchColdRow` (lines 338-399)
- `eventFromEnvelope` (lines 402-423)
- `protoActorKindToEventbus` (lines 425-438)

The test assertions (lines 560-640) are unchanged — they call `reader.QueryHistory` and verify the same fields.

## What this does NOT do

- Does NOT construct `dek.Manager` / `authguard.Guard` / `guardaudit.Emitter` in production. Those require a production KEK source (env var, file, or Vault) and are deferred to a separate task.
- Does NOT change the hot-tier subscriber path (`JetStreamSubscriber`) — that is already wired with auth options.
- Does NOT add hot-tier auth options to any existing production code path. The pipes are built; water flows when crypto bootstrap lands.

## Invariants

| ID | Invariant | Verification |
|----|-----------|-------------|
| INV-1 | `WithHistoryAuth(g, m, em)` produces the same `coldOpts` as `WithCryptoCold(WithColdHistoryAuthGuard(g), WithColdHistoryDEKManager(m), WithColdHistoryDecryptAuditEmitter(em))` | Unit test — construct two Readers, deep-equal their `coldOpts` |
| INV-2 | `WithHistoryAuth(g, m, em)` produces the same `hotOpts` as `WithCryptoHot(WithHistoryAuthGuard(g), WithHistoryDEKManager(m), WithHistoryDecryptAuditEmitter(em))` | Unit test — construct two Readers, deep-equal their `hotOpts` |
| INV-3 | `NewReader` forwards `hotOpts` to `newJetStreamHotTier` when building the default hot tier | Unit test — inject mock HotTierOption that sets a sentinel, verify sentinel propagated |
| INV-4 | `WithCryptoHot` is a no-op when `WithHotTier` is also supplied | Unit test — supply both, verify `hotOpts` not forwarded to custom tier |
| INV-5 | E2E cold-tier tests produce identical results with `WithHistoryAuth` as with the `e2eColdTier` shim | Integration — existing Ginkgo tests pass unchanged |
| INV-6 | `newHistoryReader(nil, nil, nil)` preserves existing nil-auth behavior | Unit test — verify `WithHistoryAuth` not appended when all three params are nil |

INV-1 through INV-4 require access to unexported `Reader` fields (`hotOpts`, `coldOpts`). These tests MUST live in `package history` (internal), not `package history_test`. The `internal/eventbus/history/` directory already has 10 internal test files; add to one or create a new one.

## Files touched

| File | Change |
|------|--------|
| `internal/eventbus/history/tier.go` | Add `hotOpts` field, `WithCryptoHot`, `WithHistoryAuth`, wire `hotOpts` in `NewReader` |
| `internal/eventbus/history/tier_test.go` | Tests for INV-1 through INV-4, INV-6 |
| `cmd/holomush/sub_grpc.go` | Add auth params to `newHistoryReader` signature, pass nil at call site |
| `cmd/holomush/sub_grpc_test.go` | Test for nil-passthrough behavior (INV-6) |
| `test/integration/crypto/e2e_test.go` | Replace `e2eColdTier` shim with `WithHistoryAuth`, delete dead code |
