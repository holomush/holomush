<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# History Reader Crypto Options Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add hot-tier crypto option forwarding to `history.NewReader` (parity with existing `WithCryptoCold`), a convenience `WithHistoryAuth` bundle for both tiers, production wiring at `sub_grpc.go`, and elimination of the E2E `e2eColdTier` shim.

**Architecture:** Three additions to `tier.go` (`hotOpts` field, `WithCryptoHot`, `WithHistoryAuth`) complete the Reader-level option surface. `newHistoryReader` in `sub_grpc.go` gains three optional auth params (nil = passthrough, same behavior as today). The E2E test's 145-line `e2eColdTier` shim is replaced by calling the real `newPostgresColdTier` via `WithHistoryAuth`.

**Tech Stack:** Go, testify, Ginkgo/Gomega (integration)

**Spec:** [docs/superpowers/specs/2026-05-05-history-reader-crypto-options-design.md](../specs/2026-05-05-history-reader-crypto-options-design.md)

---

## Task 1: Add `hotOpts` field, `WithCryptoHot`, `WithHistoryAuth`, and wire `NewReader`

**Files:**

- Modify: `internal/eventbus/history/tier.go`

- [ ] **Step 1: Add `hotOpts` field to `Reader` struct**

Insert after the `coldOpts` field (after line 212):

```go
// hotOpts accumulates HotTierOption values supplied via
// WithCryptoHot(...). They are forwarded to newJetStreamHotTier
// when NewReader builds the default hot tier. Ignored when the
// caller injects a test fake via WithHotTier (that path owns its
// own option wiring).
hotOpts []HotTierOption
```

- [ ] **Step 2: Add `WithCryptoHot` function**

Insert after `WithCryptoCold` (after line 161):

```go
// WithCryptoHot forwards HotTierOption values to the default
// JetStream hot tier when NewReader builds it. Mirrors WithCryptoCold
// for hot/cold parity:
//
//	history.NewReader(js, pool, max, now,
//	    history.WithCryptoHot(
//	        history.WithHistoryAuthGuard(g),
//	        history.WithHistoryDEKManager(m),
//	        history.WithHistoryDecryptAuditEmitter(em),
//	    ),
//	)
//
// No-op when the caller supplies WithHotTier (the test-fake path) —
// the injected HotTier owns its own option wiring. Multiple
// WithCryptoHot calls accumulate (last-writer-wins semantics inherited
// from the underlying HotTierOption setters).
func WithCryptoHot(opts ...HotTierOption) Option {
	return func(r *Reader) { r.hotOpts = append(r.hotOpts, opts...) }
}
```

- [ ] **Step 3: Add `WithHistoryAuth` function**

Insert after `WithCryptoHot` (after the new function from Step 2):

```go
// WithHistoryAuth wires AuthGuard + DEKManager + DecryptAuditEmitter
// into BOTH hot and cold tiers. This is the common case — production
// and tests always configure tiers symmetrically. Equivalent to
// calling WithCryptoHot and WithCryptoCold with the matching
// per-tier option constructors.
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

- [ ] **Step 4: Wire `hotOpts` in `NewReader`**

At line 247, change:

```go
if r.hot == nil && r.js != nil {
	r.hot = newJetStreamHotTier(r.js, r.selector, r.now)
}
```

To:

```go
if r.hot == nil && r.js != nil {
	r.hot = newJetStreamHotTier(r.js, r.selector, r.now, r.hotOpts...)
}
```

- [ ] **Step 5: Verify compilation**

```bash
cd internal/eventbus/history && go build ./...
```

Expected: zero errors.

- [ ] **Step 6: Commit**

```bash
jj commit -m "feat(history): add WithCryptoHot, WithHistoryAuth, hotOpts forwarding to NewReader"
```

---

### Task 2: Unit tests for option forwarding invariants

**Files:**

- Create: `internal/eventbus/history/tier_crypto_options_test.go`

Tests live in `package history` (internal) to access unexported `Reader` fields (`hotOpts`, `coldOpts`). All mocks are minimal stubs compiled inline; no new mockery-generated types.

- [ ] **Step 1: Create the test file with stub types and INV-1**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
)

// --- stub types (compile-time interface satisfaction) ---

type stubAuthGuard struct{}

var _ eventbus.SessionAuthGuard = (*stubAuthGuard)(nil)

func (*stubAuthGuard) Check(_ context.Context, _ eventbus.SessionCheckRequest) (eventbus.SessionDecision, error) {
	return eventbus.SessionDecision{Permit: true}, nil
}

type stubDEKManager struct{}

var _ eventbus.SessionDEKManager = (*stubDEKManager)(nil)

func (*stubDEKManager) Resolve(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
	return codec.Key{}, nil
}

type stubAuditEmitter struct{}

var _ eventbus.SessionAuditEmitter = (*stubAuditEmitter)(nil)

func (*stubAuditEmitter) EmitPluginDecrypt(_ context.Context, _ eventbus.PluginDecryptRecord) error {
	return nil
}

// TestWithHistoryAuthProducesSameColdOptsAsCryptoCold asserts INV-1:
// WithHistoryAuth(g, m, em) populates coldOpts identically to calling
// WithCryptoCold with the matching per-tier constructors.
func TestWithHistoryAuthProducesSameColdOptsAsCryptoCold(t *testing.T) {
	g := &stubAuthGuard{}
	m := &stubDEKManager{}
	em := &stubAuditEmitter{}

	bundleR := &Reader{}
	WithHistoryAuth(g, m, em)(bundleR)

	explicitR := &Reader{}
	WithCryptoCold(
		WithColdHistoryAuthGuard(g),
		WithColdHistoryDEKManager(m),
		WithColdHistoryDecryptAuditEmitter(em),
	)(explicitR)

	require.Len(t, bundleR.coldOpts, 3, "WithHistoryAuth produces 3 coldOpts")
	require.Len(t, explicitR.coldOpts, 3, "WithCryptoCold produces 3 coldOpts")

	// Apply both option sets to fresh tiers and compare results.
	bundleCT := &postgresColdTier{}
	for _, o := range bundleR.coldOpts {
		o(bundleCT)
	}
	explicitCT := &postgresColdTier{}
	for _, o := range explicitR.coldOpts {
		o(explicitCT)
	}

	assert.Equal(t, explicitCT.authGuard, bundleCT.authGuard, "authGuard must match")
	assert.Equal(t, explicitCT.dekManager, bundleCT.dekManager, "dekManager must match")
	assert.Equal(t, explicitCT.auditEmitter, bundleCT.auditEmitter, "auditEmitter must match")
}
```

- [ ] **Step 2: Add INV-2 test (hotOpts symmetry)**

Append to the same file:

```go
// TestWithHistoryAuthProducesSameHotOptsAsCryptoHot asserts INV-2:
// WithHistoryAuth(g, m, em) populates hotOpts identically to calling
// WithCryptoHot with the matching per-tier constructors.
func TestWithHistoryAuthProducesSameHotOptsAsCryptoHot(t *testing.T) {
	g := &stubAuthGuard{}
	m := &stubDEKManager{}
	em := &stubAuditEmitter{}

	bundleR := &Reader{}
	WithHistoryAuth(g, m, em)(bundleR)

	explicitR := &Reader{}
	WithCryptoHot(
		WithHistoryAuthGuard(g),
		WithHistoryDEKManager(m),
		WithHistoryDecryptAuditEmitter(em),
	)(explicitR)

	require.Len(t, bundleR.hotOpts, 3, "WithHistoryAuth produces 3 hotOpts")
	require.Len(t, explicitR.hotOpts, 3, "WithCryptoHot produces 3 hotOpts")

	bundleHT := &jetStreamHotTier{}
	for _, o := range bundleR.hotOpts {
		o(bundleHT)
	}
	explicitHT := &jetStreamHotTier{}
	for _, o := range explicitR.hotOpts {
		o(explicitHT)
	}

	assert.Equal(t, explicitHT.authGuard, bundleHT.authGuard, "authGuard must match")
	assert.Equal(t, explicitHT.dekManager, bundleHT.dekManager, "dekManager must match")
	assert.Equal(t, explicitHT.auditEmitter, bundleHT.auditEmitter, "auditEmitter must match")
}
```

- [ ] **Step 3: Add INV-3 test (NewReader forwards hotOpts to hot tier)**

Append to the same file. Uses `eventbustest.New(t)` for an embedded JetStream with MemoryStorage:

```go
// TestNewReaderForwardsHotOptsToHotTier asserts INV-3: when NewReader builds
// the default hot tier, HotTierOption values accumulated via WithCryptoHot are
// forwarded to newJetStreamHotTier. A sentinel option sets a detectable field
// on the hot tier; after NewReader returns, the sentinel must be visible.
func TestNewReaderForwardsHotOptsToHotTier(t *testing.T) {
	embedded := eventbustest.New(t)

	g := &stubAuthGuard{}
	m := &stubDEKManager{}
	em := &stubAuditEmitter{}

	reader := NewReader(
		embedded.JS,
		nil,               // no pool — cold tier not needed
		24*time.Hour,      // streamMaxAge (arbitrary)
		time.Now,
		WithCryptoHot(
			WithHistoryAuthGuard(g),
			WithHistoryDEKManager(m),
			WithHistoryDecryptAuditEmitter(em),
		),
	)

	ht, ok := reader.hot.(*jetStreamHotTier)
	require.True(t, ok, "default hot tier must be *jetStreamHotTier")
	assert.Equal(t, g, ht.authGuard, "authGuard forwarded to hot tier")
	assert.Equal(t, m, ht.dekManager, "dekManager forwarded to hot tier")
	assert.Equal(t, em, ht.auditEmitter, "auditEmitter forwarded to hot tier")
}
```

Add the required imports to the test file:

```go
import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
)
```

The `time` import is needed for `24*time.Hour` in NewReader's streamMaxAge argument and the `time.Now` clock function.

- [ ] **Step 4: Add INV-4 test (WithHotTier bypasses WithCryptoHot)**

Append to the same file:

```go
// TestWithCryptoHotIgnoredWhenCustomHotTier asserts INV-4:
// WithCryptoHot options are not forwarded to a custom tier supplied
// via WithHotTier. The custom tier retains its original fields.
func TestWithCryptoHotIgnoredWhenCustomHotTier(t *testing.T) {
	customTier := &jetStreamHotTier{}

	r := &Reader{}
	WithCryptoHot(WithHistoryAuthGuard(&stubAuthGuard{}))(r) // crypto option
	WithHotTier(customTier)(r)                                // custom tier, wins

	// cryptoGuard stores the option, but it was never applied —
	// the custom tier (set by WithHotTier) has nil authGuard.
	assert.Len(t, r.hotOpts, 1, "hotOpts are accumulated regardless")
	assert.Same(t, customTier, r.hot, "custom tier is installed")
	assert.Nil(t, customTier.authGuard, "custom tier authGuard unchanged — crypto not forwarded")
}
```

- [ ] **Step 5: Run unit tests**

```bash
task test -- ./internal/eventbus/history/ -run 'TestWithHistoryAuth|TestWithCryptoHot|TestNewReaderForwards'
```

Expected: 4 tests PASS.

- [ ] **Step 6: Run full history package tests to verify no regressions**

```bash
task test -- ./internal/eventbus/history/
```

Expected: all tests PASS.

- [ ] **Step 7: Commit**

```bash
jj commit -m "test(history): INV-1/2/3/4 crypto option forwarding unit tests"
```

---

### Task 3: Production wiring in `sub_grpc.go`

**Files:**

- Modify: `cmd/holomush/sub_grpc.go`

- [ ] **Step 1: Change `newHistoryReader` signature**

At line 661, change:

```go
func newHistoryReader(
	js jetstream.JetStream,
	pool *pgxpool.Pool,
	cfg eventbus.Config,
	owners *audit.OwnerMap,
	router history.PluginHistoryRouter,
) eventbus.HistoryReader {
```

To:

```go
func newHistoryReader(
	js jetstream.JetStream,
	pool *pgxpool.Pool,
	cfg eventbus.Config,
	owners *audit.OwnerMap,
	router history.PluginHistoryRouter,
	guard eventbus.SessionAuthGuard,     // nil = passthrough (current behavior)
	dekMgr eventbus.SessionDEKManager,   // nil = passthrough (current behavior)
	auditEm eventbus.SessionAuditEmitter, // nil = passthrough (current behavior)
) eventbus.HistoryReader {
```

- [ ] **Step 2: Append `WithHistoryAuth` when non-nil**

Inside `newHistoryReader`, after the `if router != nil` block (after line 673), add:

```go
	if guard != nil && dekMgr != nil && auditEm != nil {
		opts = append(opts, history.WithHistoryAuth(guard, dekMgr, auditEm))
	}
```

Full function body after changes:

```go
	opts := []history.Option{}
	if owners != nil {
		opts = append(opts, history.WithOwners(owners))
	}
	if router != nil {
		opts = append(opts, history.WithPluginRouter(router))
	}
	if guard != nil && dekMgr != nil && auditEm != nil {
		opts = append(opts, history.WithHistoryAuth(guard, dekMgr, auditEm))
	}
	return history.NewReader(js, pool, cfg.StreamMaxAge, time.Now, opts...)
```

- [ ] **Step 3: Update call site to pass nil**

At line 297, change:

```go
historyReader := newHistoryReader(js, pool, s.cfg.EventBus.Config(), owners, router)
```

To:

```go
historyReader := newHistoryReader(js, pool, s.cfg.EventBus.Config(), owners, router, nil, nil, nil)
```

- [ ] **Step 4: Verify compilation**

```bash
cd cmd/holomush && go build ./...
```

Expected: zero errors.

- [ ] **Step 5: Run cmd-level tests**

```bash
task test -- ./cmd/holomush/
```

Expected: all tests PASS (existing tests cover the nil-passthrough path).

- [ ] **Step 6: Commit**

```bash
jj commit -m "feat(cmd): plumb crypto auth options through newHistoryReader"
```

---

### Task 4: Production wiring test for INV-6

**Files:**

- Modify: `cmd/holomush/sub_grpc_test.go`

- [ ] **Step 1: Check if sub_grpc_test.go exists**

```bash
ls cmd/holomush/sub_grpc_test.go
```

If the file exists, append to it. If not, create it.

- [ ] **Step 2: Add INV-6 test**

```go
// TestNewHistoryReaderNilPreservesNilAuth asserts INV-6: calling
// newHistoryReader with nil guard/dekMgr/auditEm must preserve
// the existing nil-auth behavior (no WithHistoryAuth appended).
func TestNewHistoryReaderNilPreservesNilAuth(t *testing.T) {
	// newHistoryReader(nil, nil, cfg, nil, nil, nil, nil, nil)
	// must not panic and must return a valid Reader.
	cfg := eventbus.Config{}.Defaults()
	reader := newHistoryReader(nil, nil, cfg, nil, nil, nil, nil, nil)
	assert.NotNil(t, reader, "nil auth must still return a valid HistoryReader")
}
```

The existing imports in `sub_grpc_test.go` already include `testing`, `testify/assert`, and `eventbus` — no new imports needed.

- [ ] **Step 3: Run the test**

```bash
task test -- ./cmd/holomush/ -run TestNewHistoryReaderNil
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj commit -m "test(cmd): INV-6 nil-auth passthrough for newHistoryReader"
```

---

### Task 5: Replace E2E `e2eColdTier` shim with `WithHistoryAuth`

**Files:**

- Modify: `test/integration/crypto/e2e_test.go`

- [ ] **Step 1: Delete `e2eColdTier` type and `Read` method**

Delete lines 285-333 (the `e2eColdTier` struct and its `Read` method):

```go
// Delete: type e2eColdTier struct { ... }
// Delete: func (c *e2eColdTier) Read(...) { ... }
```

- [ ] **Step 2: Delete `dispatchColdRow`**

Delete lines 335-399 (the `dispatchColdRow` function):

```go
// Delete: func dispatchColdRow(...) { ... }
```

- [ ] **Step 3: Delete `eventFromEnvelope`**

Delete lines 401-423 (the `eventFromEnvelope` function):

```go
// Delete: func eventFromEnvelope(...) { ... }
```

- [ ] **Step 4: Delete `protoActorKindToEventbus`**

Delete lines 425-438 (the `protoActorKindToEventbus` function):

```go
// Delete: func protoActorKindToEventbus(...) { ... }
```

- [ ] **Step 5: Replace `buildColdReader`**

At the current location of `buildColdReader` (shifts after deletions above), replace:

```go
func buildColdReader(env *e2eEnv) *history.Reader {
	coldTier := &e2eColdTier{
		pool:    env.pool,
		guard:   env.guard,
		dekMgr:  env.dekMgr,
		auditEm: env.auditEm,
	}
	farFuture := time.Now().UTC().Add(100 * 365 * 24 * time.Hour)
	return history.NewReader(env.bus.JS, env.pool,
		eventbus.Config{}.Defaults().StreamMaxAge,
		func() time.Time { return farFuture },
		history.WithColdTier(coldTier),
	)
}
```

With:

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

- [ ] **Step 6: Remove unused imports**

After deleting `dispatchColdRow` (which used `proto.Unmarshal`), `eventFromEnvelope` (which used `protoactorKindToEventbus`), and `e2eColdTier` (which used `sql.NullInt64`), the following imports become unused and MUST be removed:

- `database/sql` (only used by `e2eColdTier.Read`'s `sql.NullInt64` scan)
- `google.golang.org/protobuf/proto` (only used by `dispatchColdRow`'s `proto.Unmarshal`)
- `eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"` (only used by `dispatchColdRow`, `eventFromEnvelope`, `protoActorKindToEventbus`)

Import removal is verified by `task lint` (Step 8) and `task test:int` (Step 7) — both compile with `-tags=integration`.

- [ ] **Step 7: Run integration tests to verify INV-5**

```bash
task test:int
```

Expected: all integration tests PASS, including the 3 cold-tier BDD scenarios in `e2e_test.go`:

- "delivers plaintext to the participant via cold tier"
- "delivers metadata-only to a non-participant via cold tier"
- "round-trips through cold tier with Actor.legacy_id preserved"

- [ ] **Step 8: Run `task lint`**

```bash
task lint
```

Expected: zero lint errors.

- [ ] **Step 9: Commit**

```bash
jj commit -m "test(crypto): replace e2eColdTier shim with WithHistoryAuth (INV-5)"
```

---

## Post-Implementation: Verification

- [ ] Run `task test` — all unit tests pass
- [ ] Run `task test:int` — all integration tests pass (INV-5)
- [ ] Run `task lint` — zero lint errors
- [ ] Run `task fmt` — no diff
- [ ] Verify `WithHistoryAuth(nil, nil, nil)` does nothing (INV-6)
- [ ] Verify `buildColdReader` uses real `newPostgresColdTier` not shim
- [ ] Verify `e2eColdTier`, `dispatchColdRow`, `eventFromEnvelope`, `protoActorKindToEventbus` are deleted

## Invariant Coverage

| Invariant | Task | Test |
|-----------|------|------|
| INV-1: `WithHistoryAuth` coldOpts = `WithCryptoCold(...)` | Task 2 Step 1 | `TestWithHistoryAuthProducesSameColdOptsAsCryptoCold` |
| INV-2: `WithHistoryAuth` hotOpts = `WithCryptoHot(...)` | Task 2 Step 2 | `TestWithHistoryAuthProducesSameHotOptsAsCryptoHot` |
| INV-3: `NewReader` forwards `hotOpts` | Task 2 Step 3 | `TestNewReaderForwardsHotOptsToHotTier` (embedded NATS + sentinel) |
| INV-4: `WithCryptoHot` no-op with `WithHotTier` | Task 2 Step 4 | `TestWithCryptoHotIgnoredWhenCustomHotTier` |
| INV-5: E2E cold-tier tests unchanged | Task 5 Step 7 | Existing Ginkgo scenarios |
| INV-6: `newHistoryReader(nil, nil, nil)` preserves nil-auth | Task 4 Step 2 | `TestNewHistoryReaderNilPreservesNilAuth` |

---

## Bead chain structure

```text
holomush-ojw1                   (existing epic — Phase 3: EventSink encrypt + AuthGuard + decrypt-on-fanout + downgrade fence)
└── holomush-ojw1.7             (existing task — plumb cold-tier auth options through history.NewReader)
    ├── holomush-ojw1.7.1       Reader options + unit tests (INV-1/2/3/4)
    ├── holomush-ojw1.7.2       Production wiring + INV-6 test
    └── holomush-ojw1.7.3       Replace e2eColdTier shim with WithHistoryAuth (INV-5)
```

### holomush-ojw1.7.1

```bash
bd create \
  --title "Reader options + unit tests (INV-1/2/3/4)" \
  --type task \
  --priority 2 \
  --parent holomush-ojw1.7 \
  --description "$(cat <<'EOF'
**Goal:** Add hotOpts field, WithCryptoHot, WithHistoryAuth to history.Reader, wire hotOpts in NewReader, and write unit tests for invariants INV-1 through INV-4.

**Design reference:** docs/superpowers/specs/2026-05-05-history-reader-crypto-options-design.md §1 (Reader struct), §2 (Option functions), Invariants table

**Plan reference:** docs/superpowers/plans/2026-05-06-history-reader-crypto-options.md Task 1 + Task 2

**TDD acceptance criteria:**
- TestWithHistoryAuthProducesSameColdOptsAsCryptoCold (INV-1)
- TestWithHistoryAuthProducesSameHotOptsAsCryptoHot (INV-2)
- TestNewReaderForwardsHotOptsToHotTier (INV-3) — uses eventbustest.New(t) for embedded NATS
- TestWithCryptoHotIgnoredWhenCustomHotTier (INV-4)

**Verification steps:**
- task test -- ./internal/eventbus/history/ -run 'TestWithHistoryAuth|TestWithCryptoHot|TestNewReaderForwards'
- task test -- ./internal/eventbus/history/ (full package, verify no regressions)

**Files touched:**
- internal/eventbus/history/tier.go:191-247 — add hotOpts field, WithCryptoHot, WithHistoryAuth, wire NewReader
- internal/eventbus/history/tier_crypto_options_test.go — new file, 4 tests in package history (internal)

**Dependencies:** none (can start immediately)

**Out of scope:** Crypto component construction (dek.Manager, authguard.Guard, guardaudit.Emitter); hot-tier subscriber path changes
EOF
)"
```

### holomush-ojw1.7.2

```bash
bd create \
  --title "Production wiring + INV-6 test" \
  --type task \
  --priority 2 \
  --parent holomush-ojw1.7 \
  --description "$(cat <<'EOF'
**Goal:** Add optional auth params to newHistoryReader in sub_grpc.go, pass nil at the call site, and add INV-6 unit test for nil-auth passthrough.

**Design reference:** docs/superpowers/specs/2026-05-05-history-reader-crypto-options-design.md §3 (Production wiring)

**Plan reference:** docs/superpowers/plans/2026-05-06-history-reader-crypto-options.md Task 3 + Task 4

**TDD acceptance criteria:**
- TestNewHistoryReaderNilPreservesNilAuth (INV-6) — nil guard/dekMgr/auditEm must return valid HistoryReader without panic

**Verification steps:**
- cd cmd/holomush && go build ./... (compile check)
- task test -- ./cmd/holomush/ (existing tests + INV-6)
- task test -- ./cmd/holomush/ -run TestNewHistoryReaderNil

**Files touched:**
- cmd/holomush/sub_grpc.go:661-676 — add 3 auth params to newHistoryReader, append WithHistoryAuth when non-nil
- cmd/holomush/sub_grpc.go:297 — pass nil, nil, nil at call site
- cmd/holomush/sub_grpc_test.go — INV-6 nil-passthrough test

**Dependencies:** holomush-ojw1.7.1 (needs WithHistoryAuth to exist before wiring)

**Out of scope:** Crypto component construction in production; non-nil auth wiring (separate future task)
EOF
)"
```

### holomush-ojw1.7.3

```bash
bd create \
  --title "Replace e2eColdTier shim with WithHistoryAuth (INV-5)" \
  --type task \
  --priority 2 \
  --parent holomush-ojw1.7 \
  --description "$(cat <<'EOF'
**Goal:** Delete the 145-line e2eColdTier shim (e2eColdTier type, dispatchColdRow, eventFromEnvelope, protoActorKindToEventbus), replace buildColdReader to use WithHistoryAuth with the real newPostgresColdTier, and verify E2E cold-tier tests produce identical results.

**Design reference:** docs/superpowers/specs/2026-05-05-history-reader-crypto-options-design.md §4 (E2E test cleanup)

**Plan reference:** docs/superpowers/plans/2026-05-06-history-reader-crypto-options.md Task 5

**TDD acceptance criteria:**
- INV-5: Existing Ginkgo BDD scenarios pass unchanged:
  - "delivers plaintext to the participant via cold tier"
  - "delivers metadata-only to a non-participant via cold tier"
  - "round-trips through cold tier with Actor.legacy_id preserved"

**Verification steps:**
- task test:int (all integration tests, including 3 cold-tier scenarios)
- task lint (catches unused imports: database/sql, proto, eventbusv1)
- Verify e2eColdTier, dispatchColdRow, eventFromEnvelope, protoActorKindToEventbus are deleted

**Files touched:**
- test/integration/crypto/e2e_test.go:285-458 — delete e2eColdTier type+Read, dispatchColdRow, eventFromEnvelope, protoActorKindToEventbus; replace buildColdReader

**Dependencies:** holomush-ojw1.7.2 (needs production wiring complete before shim removal)

**Out of scope:** Production crypto construction; new E2E scenarios beyond existing cold-tier coverage
EOF
)"
```

### `bd dep add` edges

```bash
bd dep add holomush-ojw1.7.2 holomush-ojw1.7.1   # 7.2 depends on WithHistoryAuth from 7.1
bd dep add holomush-ojw1.7.3 holomush-ojw1.7.2   # 7.3 depends on production wiring from 7.2
```
