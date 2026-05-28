---
title: "Audit-Chain Primitive"
---

The `auditchain` primitive (`internal/eventbus/audit/chain/`) provides
a generalized per-scope tamper-evident sequence for host-owned system
audit events. It was introduced in Phase 5 sub-epic E to generalize
the `policy_set` chain from sub-epic D into a reusable primitive.

**Audience:** Developers adding new host-side system audit event chains.
Plugin-emitted audit events (ABAC allow/deny) use a separate path; see
[Emitting Audit Events from Plugins](/extending/how-to/audit-events/).

## What the primitive provides

- **Per-scope hash chain:** Each chain registration is scoped by a
  domain-specific key (e.g., `scene:01ABC` for a per-context rekey chain).
  Each event in a scope carries a `prev_hash` linking it to its predecessor,
  and a `self_hash` over its own payload.
- **Boot-time verifier:** `auditchain.VerifierSubsystem` walks every
  registered chain at server boot. If any chain has a break (hash mismatch
  or gap), the server refuses to start with `AUDIT_CHAIN_BROKEN`.
- **Emitter helper:** `auditchain.Emitter.ComputePrevHashFor` fetches the
  current chain head for a scope and returns the `prev_hash` and
  `prev_event_id` to embed in the next event.

## Chain registration

Register a chain at wiring time in `cmd/holomush/core.go` by calling the
chain's owning package's registration function (e.g., `dek.RegisterRekey(v)`).

### The `Chain` struct

```go
package chain // internal/eventbus/audit/chain

// Chain describes a single hash-chained audit-event family.
// Pure metadata — no behavior is carried on the struct.
// Behavior (canonicalize / scope extraction) lives as standalone
// functions in the owning chain's package, registered via Handler.
type Chain struct {
    SubjectPrefix     string // e.g. "events.<game>.system.rekey"
    SelfHashField     string // dot-path of self_hash in payload
    PrevHashField     string // dot-path of prev_hash in payload
    ScopePayloadField string // dot-path identifying this chain's scope
}
```

### The `Handler` struct

The `Handler` bundles per-chain behavior and is passed to
`VerifierSubsystem.Register` at wiring time:

```go
type Handler struct {
    Chain            Chain
    SubjectFor       func(scope string) string
    ScopeFromSubject func(subject string) (string, error)
    ScopeFromPayload func(payload []byte) (string, error)
    Canonicalize     func(payload []byte) ([]byte, error)
    PrevHashOf       func(payload []byte) ([]byte, error)
    SelfHashOf       func(payload []byte) ([]byte, error)
}
```

## Contracts you must satisfy

### Subject prefix (INV-E26)

Every registered chain's `SubjectFor(scope)` MUST return a string starting
with `events.<game>.` — for example:

```text
events.<game>.system.rekey.<context_type>.<context_id>
```

Chain-bearing audit events must live under the `events.>` JetStream
SubjectFilter so they reach `events_audit` (where `auditchain.Repo` reads
from). The `audit.*` prefix is reserved for future non-chain forensic
emits and must not be used for registered chains.

A meta-test (`TestAuditChainRegistry_AllChainsUseEventsPrefix`) enforces
this for every chain in the registry.

### Scope cross-check (INV-E27)

You must register both `ScopeFromSubject` and `ScopeFromPayload`. The
verifier independently derives the chain scope from the event's subject and
from its payload, then asserts they agree. A mismatch is rejected with
`AUDIT_CHAIN_SCOPE_MISMATCH`.

Both functions must return the same canonical scope string for a given event.
For the rekey chain, the scope is `"<context_type>:<context_id>"`, derived
from the subject as the last two components and from the payload as
`context.type + ":" + context.id`.

A meta-test (`TestAuditChainRegistry_AllChainsRegisterScopeFromPayload`)
enforces that both functions are non-nil for every registered chain.

### Self-hash composition (INV-E28)

The self-hash is computed as:

```text
SHA-256(Canonicalize(zero(payload, SelfHashFieldName)))
```

This composition is pinned at the primitive level via
`auditchain.RecomputeSelfHash`. You must not diverge from it:

- The hash function is SHA-256 — always.
- The composition order is: zero the self-hash field → canonicalize → SHA-256.
- Your `Canonicalize` function handles domain-specific normalization
  (e.g., empty byte slices to `null` for D's policy_set chain). Plain JCS
  over the JSON payload is sufficient for most chains.

```go
// auditchain.RecomputeSelfHash is the single authoritative recompute.
// Call this in your verifier tests to confirm your chain's self_hash
// is reproducible.
func RecomputeSelfHash(payload map[string]any, selfHashField string) ([]byte, error)
```

### JCS canonicalization pin

All chains MUST use the vendored
`github.com/cyberphone/json-canonicalization` `jsoncanonicalizer.Transform`
function pinned in `go.mod`. This is enforced by
`TestJCSCanonicalizationLockedToVendoredImpl`.

## Genesis vs linked entries

- **Genesis entry:** The first event in a chain scope has `prev_hash: null`
  (or the zero value). The verifier accepts a null `prev_hash` only for the
  first entry for a given scope.
- **Linked entry:** Every subsequent event must carry the `prev_hash` equal
  to the `self_hash` of its predecessor. Call
  `auditchain.Emitter.ComputePrevHashFor(ctx, handler, scope)` at emit time
  to fetch the current head.

## VerifierSubsystem lifecycle

1. All chains are registered with `VerifierSubsystem` at wiring time
   (before `Start` is called).
2. At `Start`, `VerifierSubsystem` calls `Verifier.VerifyAll` for every
   registered chain, walking every discovered scope.
3. If any scope has a broken chain, `Start` returns `AUDIT_CHAIN_BROKEN`
   and the server refuses to boot.
4. After boot, no further verification runs automatically; chain integrity
   is maintained by the per-event `prev_hash` linkage.

## Worked example: `dek.RekeyChain`

The rekey chain (`internal/eventbus/crypto/dek/`) is the canonical example.
Its registration shape:

```go
var RekeyChain = auditchain.Chain{
    SubjectPrefix:     "events.<game>.system.rekey",
    SelfHashField:     "rekey_chain.self_hash",
    PrevHashField:     "rekey_chain.prev_hash",
    ScopePayloadField: "context",
}

var RekeyHandler = auditchain.Handler{
    Chain:            RekeyChain,
    SubjectFor:       func(scope string) string {
        ct, cid := splitContextScope(scope)
        return fmt.Sprintf("events.%s.system.rekey.%s.%s", currentGameID, ct, cid)
    },
    ScopeFromSubject:  parseRekeyScopeFromSubject,
    ScopeFromPayload:  parseRekeyScopeFromPayload,
    Canonicalize:      CanonicalizeRekeyPayload,  // plain JCS; no empty-form normalization
    PrevHashOf:        extractRekeyPrevHash,
    SelfHashOf:        extractRekeySelfHash,
}
```

Registration at wiring time (`cmd/holomush/core.go`):

```go
verifierSubsystem := auditchain.NewVerifierSubsystem(auditchainRepo, log)
policy.RegisterPolicySet(verifierSubsystem)   // D's policy_set chain
dek.RegisterRekey(verifierSubsystem)          // E's rekey chain
```

## Adding a new chain

1. Define a `Chain` metadata struct and a `Handler` bundle in your owning
   package (e.g., `internal/eventbus/crypto/kek/`).
2. Implement `SubjectFor`, `ScopeFromSubject`, `ScopeFromPayload`,
   `Canonicalize`, `PrevHashOf`, `SelfHashOf` as standalone functions in
   your package.
3. Add a `Register<YourChain>(v *auditchain.VerifierSubsystem)` function
   that calls `v.Register(handler)`.
4. Call `Register<YourChain>(verifierSubsystem)` in `core.go`'s wiring block.
5. Add a unit test that asserts:
   - `SubjectFor(someScope)` starts with `events.<game>.`
   - `ScopeFromSubject(SubjectFor(scope)) == scope`
   - `ScopeFromPayload(examplePayload) == scope`
   - `RecomputeSelfHash` over a fixture matches the stored `self_hash`
6. Add an integration test that verifies the chain survives a round-trip
   through `events_audit` and `VerifierSubsystem.Start`.

## Error codes

| Code | Meaning |
|------|---------|
| `AUDIT_CHAIN_BROKEN` | Boot-time verifier detected a hash-chain break in a registered chain |
| `AUDIT_CHAIN_SCOPE_MISMATCH` | A row's subject-derived scope differs from its payload-derived scope (INV-E27) |

## See also

- [Crypto Runbook](/operating/how-to/crypto/crypto-runbook/) — operator procedure for the rekey chain
- [Crypto Monitoring](/operating/how-to/crypto/crypto-monitoring/) — alert rules for chain-integrity failures
- [Sub-epic E design spec §3.6–§3.8](https://github.com/holomush/holomush/blob/main/docs/superpowers/specs/2026-05-10-event-payload-crypto-phase5-sub-epic-e-design.md) — full primitive design and R6 amendment
- [Master spec §4.6](https://github.com/holomush/holomush/blob/main/docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md) — audit event shapes
