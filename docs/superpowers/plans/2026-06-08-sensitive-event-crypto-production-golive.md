<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Production Sensitive-Event Crypto Go-Live — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the production live Subscribe/Publish path for sensitive-event encryption/decryption (the master-spec "Phase 3d flag flip") and add the first production DEK-participant seeders, so scene RP *and* private messaging (page/whisper/pemit) deliver end-to-end under crypto.

**Architecture:** Three production parts plus registry+e2e. (1) `cmd/holomush/sub_grpc.go` passes the DEK manager to the live Publisher and the AuthGuard/DEK/audit triad to the live Subscriber, gated on `RekeyManager != nil`. (2) `SetSceneFocus` seeds the focusing character as a DEK participant on `{scene,id}` — fatal on failure. (3) The publisher seeds the subject-derived recipient as the initial DEK participant for `character.<id>` contexts at genesis, with `dek.Manager.GetOrCreate` resolving an empty `BindingID` on its create branch only.

**Tech Stack:** Go, NATS JetStream (embedded), PostgreSQL (`crypto_keys`), `dek.Manager`, `authguard`, gRPC, Ginkgo/Gomega integration tests, Playwright E2E.

**Spec:** `docs/superpowers/specs/2026-06-08-sensitive-event-crypto-production-golive-design.md`

**Reviewer gates (before push):** `crypto-reviewer` (FIRST) → `abac-reviewer` (the `SetSceneFocus` gate) → `code-reviewer`. Both `task test:int` AND `task test:e2e` MUST be green.

**Commands:** `task test -- ./<pkg>` (unit), `task test:int` (integration, needs Docker), `task test:e2e` (Playwright), `task lint`, `task fmt`, `go run ./cmd/inv-render` (regenerate invariants doc).

**Model guidance (Rule 5 — crypto-domain, fail-closed invariants):** Tasks 1, 2, 6, 7, 8, 9 touch the event-payload-crypto surface (publisher/subscriber wiring, DEK genesis seeding, binding resolution, invariant registry) → `model:opus`. Tasks 3, 4, 5 (scene-access seam + fatal seed + production wiring) → `model:opus` (subtle fail-closed ordering). Task 10 (KEK CLI + compose + Playwright) → `model:sonnet`.

---

## Phase 1: Global flag-flip (production wiring)

### Task 1: Wire the DEK manager into the live Publisher

**Files:**

- Modify: `cmd/holomush/sub_grpc.go:203` (the `rawPublisher := s.cfg.EventBus.Publisher()` call) + add a `publisherOptionsFor` helper
- Test: `cmd/holomush/sub_grpc_test.go`

The live publisher is constructed with no options, so `JetStreamPublisher.dekMgr` is nil and `Publish` fail-rejects every `Sensitive=true` event (`internal/eventbus/publisher.go:226` `EVENTBUS_SENSITIVE_EVENT_NO_DEK_MANAGER`). `s.cfg.RekeyManager` (a `dek.Manager`, `sub_grpc.go:98`) is the production DEK manager and is in scope at line 203.

- [ ] **Step 1: Write the failing test**

Add to `cmd/holomush/sub_grpc_test.go` (INV-CRYPTO-117 clause 1 — asserts the option-derivation helper that Step 3 extracts so it is unit-testable without a full `Start`):

```go
// Verifies: INV-CRYPTO-117
func TestPublisherOptionsIncludeDEKManagerWhenRekeySet(t *testing.T) {
	cfg := grpcSubsystemConfig{RekeyManager: &stubDEKManager{}}
	require.Len(t, publisherOptionsFor(cfg), 1, "RekeyManager set ⇒ exactly the WithDEKManager option")
}

// Verifies: INV-CRYPTO-117
func TestPublisherOptionsEmptyWhenRekeyNil(t *testing.T) {
	require.Empty(t, publisherOptionsFor(grpcSubsystemConfig{RekeyManager: nil}),
		"no KEK ⇒ plaintext-only publisher, no DEK option")
}
```

Add a minimal stub near the top of the test file if absent (it only needs to satisfy `dek.Manager`; methods are never called by `publisherOptionsFor`):

```go
type stubDEKManager struct{ dek.Manager } // embeds the interface; nil methods unused here
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test -- -run 'TestPublisherOptions' ./cmd/holomush/`
Expected: FAIL — `undefined: publisherOptionsFor`.

- [ ] **Step 3: Write the minimal implementation**

In `cmd/holomush/sub_grpc.go`, add the helper near `Start`:

```go
// publisherOptionsFor returns the PublishOptions for the live publisher.
// When a DEK manager (RekeyManager) is configured, the publisher is DEK-aware
// so Sensitive=true events take the encrypt branch (publisher.go:208); when
// nil, the publisher stays plaintext-only and the subsystem still starts
// (degraded-but-safe for KEK-less deployments).
func publisherOptionsFor(cfg grpcSubsystemConfig) []eventbus.PublishOption {
	if cfg.RekeyManager == nil {
		return nil
	}
	return []eventbus.PublishOption{eventbus.WithDEKManager(cfg.RekeyManager)}
}
```

Then change `sub_grpc.go:203`:

```go
	rawPublisher := s.cfg.EventBus.Publisher(publisherOptionsFor(s.cfg)...)
```

(`eventbus` is already imported in `sub_grpc.go`.)

- [ ] **Step 4: Run the test to verify it passes**

Run: `task test -- -run 'TestPublisherOptions' ./cmd/holomush/`
Expected: PASS.

- [ ] **Step 5: Commit**

```text
jj commit -m "feat(crypto): wire DEK manager into live publisher (holomush-5rh.8.29)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Wire the AuthGuard/DEK/audit triad into the live Subscriber

**Files:**

- Modify: `cmd/holomush/sub_grpc.go` (delete the early `subscriber` build at lines 324-328; re-create it after the auth-guard assembly block at lines 343-374) + add a `subscriberOptionsFor` helper
- Test: `cmd/holomush/sub_grpc_test.go`

The live subscriber is built at line 324 with no options, so the decode path falls into the `guard == nil` branch (`internal/eventbus/subscriber.go:514`) and never decrypts sensitive events. The triad (`historyAuthGuard`, `historyDEKMgr`, `historyAuditEm`) is assembled at lines 343-374 — *after* 324 — so the subscriber must be relocated to reuse those instances.

- [ ] **Step 1: Write the failing test**

Add to `cmd/holomush/sub_grpc_test.go`:

```go
// Verifies: INV-CRYPTO-117
func TestSubscriberOptionsIncludeAuthGuardWhenGuardPresent(t *testing.T) {
	opts := subscriberOptionsFor(stubSessionAuthGuard{}, &stubDEKManager{}, stubSessionAuditEmitter{})
	require.Len(t, opts, 3, "guard present ⇒ AuthGuard + DEKManager + DecryptAuditEmitter")
}

// Verifies: INV-CRYPTO-117
func TestSubscriberOptionsEmptyWhenGuardNil(t *testing.T) {
	require.Empty(t, subscriberOptionsFor(nil, nil, nil),
		"no guard ⇒ bare subscriber preserves guard==nil passthrough")
}
```

Add minimal stubs near the top of the test file (methods never called by `subscriberOptionsFor`; reuse `stubDEKManager` from Task 1 — `dek.Manager` satisfies `eventbus.SessionDEKManager` per `sub_grpc.go:98`):

```go
type stubSessionAuthGuard struct{ eventbus.SessionAuthGuard }
type stubSessionAuditEmitter struct{ eventbus.SessionAuditEmitter }
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test -- -run 'TestSubscriberOptions' ./cmd/holomush/`
Expected: FAIL — `undefined: subscriberOptionsFor`.

- [ ] **Step 3: Write the minimal implementation**

In `cmd/holomush/sub_grpc.go`, add the helper near `publisherOptionsFor`:

```go
// subscriberOptionsFor returns the SubscribeOptions for the live subscriber.
// When the AuthGuard is present (KEK configured), the subscriber decodes and
// authorizes sensitive events (subscriber.go:505 decodeAndAuthorize),
// delivering plaintext to DEK participants and metadata-only to others. When
// the guard is nil, the bare subscriber preserves the pre-flag-flip
// passthrough (subscriber.go:514).
func subscriberOptionsFor(
	guard eventbus.SessionAuthGuard,
	dekMgr eventbus.SessionDEKManager,
	auditEm eventbus.SessionAuditEmitter,
) []eventbus.SubscribeOption {
	if guard == nil {
		return nil
	}
	opts := []eventbus.SubscribeOption{
		eventbus.WithSubscriberAuthGuard(guard),
		eventbus.WithSubscriberDEKManager(dekMgr),
	}
	if auditEm != nil {
		opts = append(opts, eventbus.WithSubscriberDecryptAuditEmitter(auditEm))
	}
	return opts
}
```

**Delete** the early subscriber construction (lines 324-328):

```go
	subscriber := s.cfg.EventBus.Subscriber()
	if subscriber == nil {
		return oops.Code("GRPC_EVENTBUS_SUBSCRIBER_NIL").
			Errorf("EventBus subscriber is nil; subsystem not started")
	}
```

and re-create it **after** the auth-guard assembly block (immediately after line 374, before the Phase-7 fence block at line 376):

```go
	// Phase 3d flag flip (holomush-5rh.8.29): the live subscriber is built
	// AFTER the AuthGuard/DEK/audit triad is resolved so decode-on-fan-out can
	// decrypt sensitive events for DEK participants. When KEK is absent
	// (historyAuthGuard==nil) the bare subscriber preserves passthrough.
	subscriber := s.cfg.EventBus.Subscriber(
		subscriberOptionsFor(historyAuthGuard, historyDEKMgr, historyAuditEm)...)
	if subscriber == nil {
		return oops.Code("GRPC_EVENTBUS_SUBSCRIBER_NIL").
			Errorf("EventBus subscriber is nil; subsystem not started")
	}
```

- [ ] **Step 4: Run the tests**

Run: `task test -- -run 'TestSubscriberOptions' ./cmd/holomush/` → Expected: PASS.
Run: `task test -- ./cmd/holomush/` → Expected: PASS (the relocated `subscriber` variable is consumed later in `Start` exactly as before).

- [ ] **Step 5: Commit**

```text
jj commit -m "feat(crypto): wire AuthGuard into live subscriber — Phase 3d flag flip (holomush-5rh.8.29)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Phase 2: Scene reader-seeding (fatal)

### Task 3: Add the DEK-adder seam to SceneAccessServer

**Files:**

- Modify: `internal/grpc/sceneaccess_service.go` (add interface, field, setter; add `dek` import)
- Test: `internal/grpc/sceneaccess_service_test.go`

A narrow interface + optional setter (NOT a positional constructor arg) keeps the existing 7-arg `NewSceneAccessServer` and all its call sites — including the integration harness — unchanged.

- [ ] **Step 1: Write the failing test**

Add to `internal/grpc/sceneaccess_service_test.go`:

```go
func TestWithSceneDEKAdderSetsField(t *testing.T) {
	s := &SceneAccessServer{}
	s.WithSceneDEKAdder(stubSceneDEKAdder{})
	require.NotNil(t, s.dekAdder, "WithSceneDEKAdder must set the dekAdder field")
}

type stubSceneDEKAdder struct{}

func (stubSceneDEKAdder) Add(_ context.Context, _ dek.ContextID, _ dek.Participant) error { return nil }
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test -- -run 'TestWithSceneDEKAdder' ./internal/grpc/`
Expected: FAIL — `s.dekAdder undefined` / `s.WithSceneDEKAdder undefined`.

- [ ] **Step 3: Write the minimal implementation**

In `internal/grpc/sceneaccess_service.go`, add the import:

```go
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
```

Add the interface above the `SceneAccessServer` struct:

```go
// sceneDEKAdder seeds a character as a DEK participant so the AuthGuard's
// hot-tier checkCharacter branch permits this session to decrypt sensitive
// scene events (e.g. scene_pose). Satisfied by dek.Manager (its Add method).
type sceneDEKAdder interface {
	Add(ctx context.Context, ctxID dek.ContextID, p dek.Participant) error
}
```

Add the field to the struct (after `pluginManager`):

```go
	// dekAdder is optional. When non-nil, SetSceneFocus seeds the focusing
	// character as a DEK participant after the participation gate passes, so
	// the AuthGuard permits decryption of sensitive scene events. nil disables
	// seeding (KEK-less deployments / tests).
	dekAdder sceneDEKAdder
```

Add the setter after `NewSceneAccessServer`:

```go
// WithSceneDEKAdder attaches a DEK participant adder. Call after construction;
// when set, SetSceneFocus seeds the focusing character as a DEK participant
// (fatal on failure).
func (s *SceneAccessServer) WithSceneDEKAdder(a sceneDEKAdder) {
	s.dekAdder = a
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `task test -- -run 'TestWithSceneDEKAdder' ./internal/grpc/`
Expected: PASS.

- [ ] **Step 5: Commit**

```text
jj commit -m "feat(scene-access): add optional DEK-adder seam to SceneAccessServer (holomush-5rh.8.29)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: SetSceneFocus seeds the participant (fatal on failure)

**Files:**

- Modify: `internal/grpc/sceneaccess_service.go` (`SetSceneFocus`, inside the `if sceneIDStr != ""` block, after the `JoinFocus` block and before the block's closing `}`; add `time` import)
- Test: `internal/grpc/sceneaccess_service_test.go`

Ordering MUST be: participation gate → `JoinFocus` → `Add` (fatal) → `SetConnectionFocus`. A fatal `Add` after a successful `JoinFocus` leaves a dangling `FocusMembership` (benign; self-heals on retry — `Add` is idempotent, `manager.go:357`).

- [ ] **Step 1: Write the failing test**

Add to `internal/grpc/sceneaccess_service_test.go` a unit test driving the seed path. Reuse the file's existing `SetSceneFocus` test scaffolding (it already builds a `SceneAccessServer` with fakes for `sessionStore`, `coordinator`, `sceneClient`, `charRepo`, `playerSessionRepo` and a happy-path participant + `JoinFocus`). Assert `SetSceneFocus` returns `codes.Internal` and does NOT call `SetConnectionFocus` when the adder fails:

```go
type failingSceneDEKAdder struct{}

func (failingSceneDEKAdder) Add(_ context.Context, _ dek.ContextID, _ dek.Participant) error {
	return oops.Code("DEK_ADD_FAILED").Errorf("seed failed")
}

// Verifies: INV-CRYPTO-116
func TestSetSceneFocusReturnsInternalWhenDEKSeedFails(t *testing.T) {
	s, fakeCoord := newSetSceneFocusServerWithParticipant(t) // existing/extended helper in this file
	s.WithSceneDEKAdder(failingSceneDEKAdder{})

	_, err := s.SetSceneFocus(context.Background(), validSetSceneFocusRequest(t)) // existing/extended helper
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
	require.False(t, fakeCoord.setConnectionFocusCalled,
		"a fatal DEK seed MUST refuse focus — SetConnectionFocus must not run")
}
```

If `newSetSceneFocusServerWithParticipant` / `validSetSceneFocusRequest` / `fakeCoord.setConnectionFocusCalled` do not already exist, add them mirroring the existing participation-gate test setup in this file (extend the existing fake coordinator with a `setConnectionFocusCalled bool` flag set in its `SetConnectionFocus`).

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test -- -run 'TestSetSceneFocusReturnsInternalWhenDEKSeedFails' ./internal/grpc/`
Expected: FAIL — `SetConnectionFocus` still runs (seed block absent), so the flag is true.

- [ ] **Step 3: Write the minimal implementation**

In `internal/grpc/sceneaccess_service.go`, insert inside the `if sceneIDStr := req.GetSceneId(); sceneIDStr != "" {` block, immediately after the `JoinFocus` error-handling block (after the `// FOCUS_ALREADY_MEMBER — membership already present; proceed to SetConnectionFocus.` comment) and before the block's closing brace:

```go
		// Seed the character as a DEK participant so the AuthGuard hot-tier
		// permits this session to decrypt sensitive scene events (scene_pose,
		// scene_say, scene_emit, scene_ooc). FATAL: if the seed fails the
		// connection MUST NOT be focused — a focused connection that cannot
		// decrypt would receive blank (metadata-only) poses. Refusing focus
		// surfaces the error so the client retries; Add is idempotent
		// (manager.go:357) so retry is a safe no-op. (Invariant: a connection
		// is focused on a scene only if its character can decrypt that scene.)
		if s.dekAdder != nil {
			ctxID := dek.ContextID{Type: "scene", ID: sceneIDStr}
			addErr := s.dekAdder.Add(ctx, ctxID, dek.Participant{
				PlayerID:    ps.PlayerID.String(),
				CharacterID: gameSession.CharacterID.String(),
				JoinedAt:    time.Now().UTC(),
				AddedVia:    "sceneaccess.SetSceneFocus",
			})
			if addErr != nil {
				slog.ErrorContext(ctx, "scene access: SetSceneFocus DEK seed failed",
					"scene_id", sceneIDStr,
					"character_id", gameSession.CharacterID.String(),
					"error", addErr)
				return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
			}
		}
```

Add the `time` import to `internal/grpc/sceneaccess_service.go` (it is NOT currently imported there):

```go
	"time"
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `task test -- -run 'TestSetSceneFocus' ./internal/grpc/`
Expected: PASS (the new fatal-seed test passes; pre-existing `SetSceneFocus` tests still pass — they construct the server without `WithSceneDEKAdder`, so `s.dekAdder` is nil and the block is skipped).

- [ ] **Step 5: Commit**

```text
jj commit -m "feat(scene-access): SetSceneFocus seeds DEK participant, fatal on failure (holomush-5rh.8.29)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Wire the scene DEK-adder in production

**Files:**

- Modify: `cmd/holomush/sub_grpc.go:521` (the `NewSceneAccessServer(...)` call site)
- Test: build + package tests (the setter wiring is one line; behavior is unit-proven in Task 4, e2e-proven in Task 10)

- [ ] **Step 1: Write the implementation**

In `cmd/holomush/sub_grpc.go`, change the scene-access construction (lines 521-529) to capture the server, attach the adder when `RekeyManager` is set, then assign:

```go
		saSrv := holoGRPC.NewSceneAccessServer(
			authPlayerSessionRepo,
			authPlayerRepo,
			authCharRepo,
			sessionStore,
			focusCoord,
			scenev1.NewSceneServiceClient(sceneSvc.Conn),
			pluginManager,
		)
		// Seed DEK participants on SetSceneFocus when the KEK/DEK stack is
		// present so the AuthGuard permits decryption of sensitive scene
		// events for the focusing session (holomush-5rh.8.29).
		if s.cfg.RekeyManager != nil {
			saSrv.WithSceneDEKAdder(s.cfg.RekeyManager)
		}
		sceneAccessSrv = saSrv
```

- [ ] **Step 2: Build + test**

Run: `task build && task test -- ./cmd/holomush/`
Expected: PASS.

- [ ] **Step 3: Commit**

```text
jj commit -m "feat(crypto): wire scene DEK-adder into production gRPC subsystem (holomush-5rh.8.29)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Phase 3: Comms recipient-seeding (genesis)

### Task 6: GetOrCreate resolves an empty BindingID on the create branch

**Files:**

- Modify: `internal/eventbus/crypto/dek/manager.go` (the `GetOrCreate` create branch — after the `selectActive` miss, before `in := row{...}` at ~line 236; use `resolved` at lines 243 and 267)
- Test: `internal/eventbus/crypto/dek/manager_integration_test.go`

`GetOrCreate` stores `initial` as-is (`manager.go:243`). A publisher-derived initial participant (`{CharacterID}` only, empty `BindingID`) would be rejected by the AuthGuard (it requires `BindingID != ""`). Resolve empty `BindingID`s on the **create branch only** — pre-bound participants (every existing caller; e.g. `manager_integration_test.go:257`, harness `SeedSceneDEKParticipant`) pass through unchanged. `m.bindings` is guaranteed non-nil by the constructor (`manager.go:172`). This resolves the spec §3.4 fork (Option A): binding I/O happens only at genesis, never per message.

- [ ] **Step 1: Write the failing test**

Add an `It` to the `Describe("Manager", …)` suite in `internal/eventbus/crypto/dek/manager_integration_test.go`. Each `It` builds its own `ctx`/`pool`/`mgr` locally (there is no shared suite `mgr`); follow the canonical construction at `manager_integration_test.go:158-170`. Use a `&stubBindingResolver{bindingID: …}` (defined in `manager_test.go:19` — `Current` returns the configured `bindingID`) so the test needs NO FK-parent seeding — it directly proves that `GetOrCreate` resolves an empty `BindingID` through the resolver:

```go
	It("resolves an empty BindingID on genesis via the BindingResolver", func() {
		ctx := context.Background()
		connStr, teardown := newTestPGPool(suiteT)
		DeferCleanup(teardown)
		pool, err := pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)

		provider := newTestProvider(suiteT)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		const wantBinding = "01RESOLVEDBINDING0000000"
		mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache, partCache,
			noopInvalidator, &stubBindingResolver{bindingID: wantBinding})
		Expect(err).NotTo(HaveOccurred())

		ctxID := dek.ContextID{Type: "character", ID: "01HRECIPIENTCHAR000000000"}
		// Participant supplied with an EMPTY BindingID — GetOrCreate must resolve it.
		key, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{{CharacterID: "01HRECIPIENTCHAR000000000"}})
		Expect(err).NotTo(HaveOccurred())

		parts, err := mgr.Participants(ctx, key.ID, key.Version)
		Expect(err).NotTo(HaveOccurred())
		Expect(parts).To(HaveLen(1))
		Expect(parts[0].BindingID).To(Equal(wantBinding),
			"genesis must resolve empty BindingID via BindingResolver.Current")
	})
```

(All helpers — `newTestPGPool`, `newTestProvider`, `noopInvalidator`, `stubBindingResolver` — already exist in the suite; `pgxpool` and `time` are already imported. No new imports needed.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test:int -- -run 'TestDEKManager' ./internal/eventbus/crypto/dek/`
Expected: FAIL — `parts[0].BindingID` is empty (stored as-is).

- [ ] **Step 3: Write the minimal implementation**

In `internal/eventbus/crypto/dek/manager.go`, in `GetOrCreate`, after the `selectActive` miss confirms minting and before `in := row{...}` (≈ line 236):

```go
	// Resolve any participant supplied without a BindingID (e.g. the publisher's
	// genesis-seed for a character.<id> context). Pre-bound participants pass
	// through unchanged. m.bindings is guaranteed non-nil (NewManager:172).
	// Mirrors the resolution Add performs (manager.go:344). Genesis-only: the
	// active-row short-circuit above means this never runs once the DEK exists.
	resolved := initial
	if len(initial) > 0 {
		resolved = make([]Participant, len(initial))
		for i, p := range initial {
			if p.BindingID == "" {
				bindingID, bErr := m.bindings.Current(ctx, p.CharacterID)
				if bErr != nil {
					return codec.Key{}, oops.Code("DEK_BINDING_RESOLVE_FAILED").
						With("character_id", p.CharacterID).Wrap(bErr)
				}
				p.BindingID = bindingID
			}
			resolved[i] = p
		}
	}
```

Replace the two `initial` references with `resolved`:

- `manager.go:243` → `Participants: resolved,`
- `manager.go:267` (the `m.partCache.Put` argument) → `resolved,`

- [ ] **Step 4: Run the tests**

Run: `task test:int -- -run 'TestDEKManager' ./internal/eventbus/crypto/dek/`
Expected: PASS — including the existing `GetOrCreate` round-trip test at `manager_integration_test.go:257` (pre-bound participants skip resolution; BindingIDs unchanged).

- [ ] **Step 5: Commit**

```text
jj commit -m "feat(crypto): GetOrCreate resolves empty BindingID at genesis (holomush-5rh.8.29)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Publisher seeds the subject-derived recipient for character contexts

**Files:**

- Modify: `internal/eventbus/publisher.go:215` (the `GetOrCreate(ctx, ctxID, nil)` call) + add an `initialParticipantsForContext` helper
- Test: `internal/eventbus/publisher_test.go`

For a sensitive event to `character.<id>`, the recipient owns that personal stream and must be a DEK participant. Derive the initial set from the context type at genesis. Scenes (`Type=="scene"`) keep `nil` (readers seeded at `SetSceneFocus`).

- [ ] **Step 1: Write the failing test**

Add to `internal/eventbus/publisher_test.go`:

```go
func TestInitialParticipantsForContextCharacterSeedsRecipient(t *testing.T) {
	got := initialParticipantsForContext(dek.ContextID{Type: "character", ID: "01HRECIPIENT0000000000000000"})
	require.Len(t, got, 1)
	require.Equal(t, "01HRECIPIENT0000000000000000", got[0].CharacterID)
	require.Empty(t, got[0].BindingID, "binding resolved downstream in GetOrCreate, not here")
}

func TestInitialParticipantsForContextSceneIsNil(t *testing.T) {
	require.Nil(t, initialParticipantsForContext(dek.ContextID{Type: "scene", ID: "01HSCENE"}))
}

func TestInitialParticipantsForContextOtherIsNil(t *testing.T) {
	require.Nil(t, initialParticipantsForContext(dek.ContextID{Type: "location", ID: "01HLOC"}))
}
```

(`publisher_test.go` is `package eventbus`; `dek` is already used in the package. Confirm the test file imports `dek`.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test -- -run 'TestInitialParticipantsForContext' ./internal/eventbus/`
Expected: FAIL — `undefined: initialParticipantsForContext`.

- [ ] **Step 3: Write the minimal implementation**

In `internal/eventbus/publisher.go`, add the helper near `contextIDFromSubject` (the package already imports `time`, line 15):

```go
// initialParticipantsForContext returns the DEK participant set to seed at
// genesis for a context, derived from the subject. A character.<id> context is
// a personal stream whose owner (the recipient of a private message —
// page/whisper/pemit) must be able to decrypt, so seed that character. The
// BindingID is left empty; GetOrCreate resolves it via the BindingResolver on
// its create branch (manager.go). Scene and other contexts seed nothing here
// (scene readers are seeded at SetSceneFocus).
func initialParticipantsForContext(ctxID dek.ContextID) []dek.Participant {
	if ctxID.Type != "character" {
		return nil
	}
	return []dek.Participant{{
		CharacterID: ctxID.ID,
		JoinedAt:    time.Now().UTC(),
		AddedVia:    "publisher.genesis",
	}}
}
```

Change the genesis call at `publisher.go:215`:

```go
		k, dekErr := p.dekMgr.GetOrCreate(ctx, ctxID, initialParticipantsForContext(ctxID))
```

- [ ] **Step 4: Run the tests**

Run: `task test -- -run 'TestInitialParticipantsForContext' ./internal/eventbus/` → Expected: PASS.
Run: `task test -- ./internal/eventbus/` → Expected: PASS.

- [ ] **Step 5: Commit**

```text
jj commit -m "feat(crypto): publisher seeds recipient as DEK participant for character contexts (holomush-5rh.8.29)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Integration — genesis auto-seed asymmetry (comms vs scene)

**Files:**

- Create: `test/integration/crypto/genesis_seed_test.go`
- Reference: `test/integration/crypto/metadata_only_test.go` (the `buildSubscribeHarness` model — same package `crypto_test`; `subscribeHarness{publisher, subscriber, dekMgr}`; the `dekBindingStub{bindingID: "bind-metadata"}` resolver; `h.subscriber.OpenSession`; `delivery.MetadataOnly()` / `delivery.Event().Payload`)

This is the new integration coverage: it proves the genesis-seed asymmetry end-to-end. A `character.<id>` context auto-seeds its recipient at genesis (Task 7 + Task 6) → the recipient decrypts a page **with no pre-seed**; a `scene.<id>` context seeds no one at genesis → an unseeded subscriber gets metadata-only. Binds master INV-8 (**INV-CRYPTO-116**). (Subscriber decrypt mechanics for pre-seeded participants are already covered by `metadata_only_test.go`; this file proves the *seeding*, not the decode.)

- [ ] **Step 1: Write the failing test**

Create `test/integration/crypto/genesis_seed_test.go` (`//go:build integration`, `package crypto_test`). Follow the exact shape of `metadata_only_test.go` (manifest + `plugins.NewPluginEventEmitter(h.publisher, manifestLookup, actorResolver)` + `h.subscriber.OpenSession` + `stream.Next`). Reuse the already-registered verb `test-plugin:whisper` (only its `Type` matters to the RenderingPublisher; the `Subject` selects the context).

```go
//go:build integration

package crypto_test

// Verifies: INV-CRYPTO-116
var _ = Describe("Genesis DEK seeding asymmetry (character auto-seeds, scene does not)", func() {
	It("a page recipient decrypts with NO pre-seed; a third party gets metadata-only", func() {
		ctx := context.Background()
		h := buildSubscribeHarness(suiteT)

		recipientCharID := "01HRECIPIENTCHARACTER0000"
		const (
			recipientPlayerID = "01HRECIPIENTPLAYER000000"
			thirdPlayerID     = "01HTHIRDPLAYER0000000000"
			thirdCharacterID  = "01HTHIRDCHARACTER0000000"
		)

		// Emit a sensitive page to character.<recipient> with NO pre-seed. The
		// publisher's genesis (Task 7) derives initial=[recipient]; GetOrCreate
		// (Task 6) resolves the recipient's binding via the harness stub
		// ("bind-metadata"), minting the DEK with the recipient as participant.
		manifest := &plugins.Manifest{
			Name: "test-plugin", Emits: []string{"character"}, ActorKindsClaimable: []string{"plugin"},
			Crypto: &plugins.CryptoSection{Emits: []plugins.CryptoEmit{
				{EventType: "test-plugin:whisper", Sensitivity: plugins.SensitivityMay}}},
		}
		manifestLookup := func(name string) *plugins.Manifest {
			if name == "test-plugin" { return manifest }
			return nil
		}
		actorID := plugintest.PluginULIDFromName("test-plugin").String()
		actorResolver := func(_ context.Context, _ string) (core.Actor, error) {
			return core.Actor{Kind: core.ActorPlugin, ID: actorID}, nil
		}
		emitter := plugins.NewPluginEventEmitter(h.publisher, manifestLookup, actorResolver)

		const plaintext = `{"text":"a private page for the recipient only"}`
		Expect(emitter.Emit(ctx, "test-plugin", pluginsdk.EmitIntent{
			Subject: "character." + recipientCharID, Type: pluginsdk.EventType("test-plugin:whisper"),
			Payload: plaintext, Sensitive: true})).NotTo(HaveOccurred())

		// Recipient identity uses the stub's resolved binding ("bind-metadata").
		recipientID := eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter,
			PlayerID: recipientPlayerID, CharacterID: recipientCharID, BindingID: "bind-metadata"}
		recStream, err := h.subscriber.OpenSession(ctx, "recipient-"+recipientCharID, recipientID,
			[]eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = recStream.Close() })

		rcv, cancel := context.WithTimeout(ctx, testutil.DefaultWait)
		defer cancel()
		d, err := recStream.Next(rcv)
		Expect(err).NotTo(HaveOccurred())
		Expect(d.Ack()).NotTo(HaveOccurred())
		Expect(d.MetadataOnly()).To(BeFalse(), "genesis auto-seeded the recipient")
		Expect(d.Event().Payload).To(Equal([]byte(plaintext)), "INV-CRYPTO-116: recipient decrypts page")

		// A third party (different binding) on the same stream gets metadata-only.
		thirdID := eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter,
			PlayerID: thirdPlayerID, CharacterID: thirdCharacterID, BindingID: "bind-OTHER"}
		thirdStream, err := h.subscriber.OpenSession(ctx, "third-"+recipientCharID, thirdID,
			[]eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = thirdStream.Close() })
		td, err := thirdStream.Next(rcv)
		Expect(err).NotTo(HaveOccurred())
		Expect(td.Ack()).NotTo(HaveOccurred())
		Expect(td.MetadataOnly()).To(BeTrue(), "non-recipient on the personal stream gets metadata-only")
		Expect(td.Event().Payload).To(BeEmpty())
	})

	It("a scene context seeds no one at genesis (subscriber gets metadata-only with no pre-seed)", func() {
		ctx := context.Background()
		h := buildSubscribeHarness(suiteT)
		sceneID := "01HXXXGENESISSCENE000001"

		manifest := &plugins.Manifest{
			Name: "test-plugin", Emits: []string{"scene"}, ActorKindsClaimable: []string{"plugin"},
			Crypto: &plugins.CryptoSection{Emits: []plugins.CryptoEmit{
				{EventType: "test-plugin:whisper", Sensitivity: plugins.SensitivityMay}}},
		}
		manifestLookup := func(name string) *plugins.Manifest { if name == "test-plugin" { return manifest }; return nil }
		actorID := plugintest.PluginULIDFromName("test-plugin").String()
		actorResolver := func(_ context.Context, _ string) (core.Actor, error) {
			return core.Actor{Kind: core.ActorPlugin, ID: actorID}, nil
		}
		emitter := plugins.NewPluginEventEmitter(h.publisher, manifestLookup, actorResolver)

		Expect(emitter.Emit(ctx, "test-plugin", pluginsdk.EmitIntent{
			Subject: "scene." + sceneID, Type: pluginsdk.EventType("test-plugin:whisper"),
			Payload: `{"text":"unseeded scene pose"}`, Sensitive: true})).NotTo(HaveOccurred())

		anyID := eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter,
			PlayerID: "01HANYPLAYER000000000000", CharacterID: "01HANYCHARACTER000000000", BindingID: "bind-metadata"}
		stream, err := h.subscriber.OpenSession(ctx, "any-"+sceneID, anyID, []eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })
		rcv, cancel := context.WithTimeout(ctx, testutil.DefaultWait)
		defer cancel()
		d, err := stream.Next(rcv)
		Expect(err).NotTo(HaveOccurred())
		Expect(d.Ack()).NotTo(HaveOccurred())
		Expect(d.MetadataOnly()).To(BeTrue(), "scene genesis seeds no one; readers seeded only at SetSceneFocus")
	})
})
```

- [ ] **Step 2: Run the test to verify it fails (pre-Task-6/7) then passes (post)**

Run: `task test:int -- ./test/integration/crypto/`
Expected: WITHOUT Tasks 6+7 the recipient receives metadata-only (genesis seeds no one) → the first `It` FAILS; WITH Tasks 6+7 applied the recipient decrypts → PASS. The scene `It` passes throughout (scenes never auto-seed).

- [ ] **Step 3: Commit**

```text
jj commit -m "test(crypto): genesis DEK-seed asymmetry — character auto-seeds, scene does not (holomush-5rh.8.29)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Phase 4: Invariant registry + E2E

### Task 9: Register/bind invariants

**Files:**

- Modify: `docs/architecture/invariants.yaml`
- Generate: `docs/architecture/invariants.md` (via `go run ./cmd/inv-render` — never hand-edit)
- Verify: `test/meta/invariant_registry_test.go`

- [ ] **Step 1: Add/update registry entries**

In `docs/architecture/invariants.yaml`, under the `INV-CRYPTO` scope:

1. Add **INV-CRYPTO-116** (registers master INV-8, currently an orphan):

```yaml
  - id: INV-CRYPTO-116
    scope: INV-CRYPTO
    origin_spec: "docs/superpowers/specs/2026-06-08-sensitive-event-crypto-production-golive-design.md"
    legacy: ["INV-8@docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md"]
    summary: "A subject in a DEK's participant set MUST receive plaintext via live fan-out when policy permits."
    asserted_by:
      - "test/integration/crypto/genesis_seed_test.go"
    binding: bound
    refs:
      - {file: "test/integration/crypto/genesis_seed_test.go", token: "INV-CRYPTO-116"}
```

2. Add **INV-CRYPTO-117** (the production-wiring guarantee — two clauses, asserted independently per design-review finding 2):

```yaml
  - id: INV-CRYPTO-117
    scope: INV-CRYPTO
    origin_spec: "docs/superpowers/specs/2026-06-08-sensitive-event-crypto-production-golive-design.md"
    summary: "When RekeyManager is non-nil the production gRPC subsystem MUST build the live Publisher DEK-aware and the
      live Subscriber AuthGuard-wired; when nil, both MUST preserve plaintext/passthrough and the subsystem MUST start."
    asserted_by:
      - "cmd/holomush/sub_grpc_test.go"
    binding: bound
    refs:
      - {file: "cmd/holomush/sub_grpc_test.go", token: "INV-CRYPTO-117"}
```

3. Update **INV-CRYPTO-6** (master INV-9). It is currently `binding: pending`, but `test/integration/crypto/metadata_only_test.go:268` already asserts it (`delivery.Event().Payload` `BeEmpty()`, "INV-CRYPTO-6: non-participant payload must be empty bytes"). Flip it to `bound`, citing the existing test:

```yaml
    asserted_by:
      - "test/integration/crypto/metadata_only_test.go"
    binding: bound
```

Keep the existing `refs`. Add a `// Verifies: INV-CRYPTO-6` annotation immediately above the asserting `It` in `metadata_only_test.go` if one is not already present (the file already references the token in comments at line 268; add the canonical `// Verifies:` form so `TestBoundInvariantsAreGenuinelyAsserted` recognizes it).

- [ ] **Step 2: Confirm `// Verifies:` annotations**

The test bodies in Tasks 1, 2, 4, 8 already carry `// Verifies: INV-CRYPTO-117` / `INV-CRYPTO-116`. Add `// Verifies: INV-CRYPTO-6` above the non-participant `It` in `metadata_only_test.go` (Step 1.3).

- [ ] **Step 3: Regenerate, format, verify (order matters — `task fmt` before render per the `lint:yaml` gotcha)**

```text
task fmt
go run ./cmd/inv-render
task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/
```

Expected: meta tests PASS; `jj diff --git` shows `invariants.md` regenerated (generated regions only).

- [ ] **Step 4: Run the integration suite to confirm bindings are real**

Run: `task test:int -- ./test/integration/crypto/`
Expected: PASS.

- [ ] **Step 5: Commit**

```text
jj commit -m "docs(invariants): register INV-CRYPTO-116/117, bind INV-CRYPTO-6 (holomush-5rh.8.29)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: E2E — KEK provisioning + live PoseCard (the holomush-5rh.8.27 surface)

**Files:**

- Create: `cmd/holomush/cmd_kek.go`, `cmd/holomush/cmd_kek_init.go` (the `holomush kek init <path>` subcommand)
- Modify: `cmd/holomush/root.go` (register `NewKEKCmd()`), `cmd/holomush/gateway_imports_test.go` (allowlist the two core-only files), `compose.e2e.yaml` (kek-init service + KEK env vars), `web/e2e/scenes.spec.ts` (strengthen S3)
- Reference: the crypto-reviewer-vetted WIP commit `uplzoyqk` (`jj show --git uplzoyqk` — the kek-init pieces are SOUND and in-scope; re-derive, do not blind-copy)

This is the `.8.27` surface that rides this PR: provision a test KEK in the e2e compose stack so sensitive scene events work end-to-end, then strengthen scenario S3 to assert the live PoseCard (now decryptable because Tasks 1-7 wired the live path + scene seeding).

- [ ] **Step 1: Add the `holomush kek init` subcommand**

Create `cmd/holomush/cmd_kek.go` and `cmd/holomush/cmd_kek_init.go` per the WIP (`jj show --git uplzoyqk`): a `kek` parent command with an `init <path>` subcommand that reads `HOLOMUSH_KEK_PASSPHRASE`, generates 32 random bytes via `crypto/rand`, and persists via `kek.NewFileSource(path, …).Persist(...)`. Register `cmd.AddCommand(NewKEKCmd())` in `cmd/holomush/root.go`. Allowlist the two new core-only files in `cmd/holomush/gateway_imports_test.go` (they import `internal/eventbus/crypto/kek`).

- [ ] **Step 2: Verify the command builds and the gateway-imports test passes**

Run: `task build && task test -- -run 'TestGateway' ./cmd/holomush/`
Expected: PASS.

- [ ] **Step 3: Wire the KEK into the e2e compose stack**

In `compose.e2e.yaml`, add a `kek-init` init service that runs `holomush kek init <kekfile>` with `HOLOMUSH_KEK_PASSPHRASE` set, writes the KEK file to a shared volume, and add `HOLOMUSH_KEK_FILE` + `HOLOMUSH_KEK_PASSPHRASE` to the core service env with `depends_on: { kek-init: { condition: service_completed_successfully } }` (per the WIP).

- [ ] **Step 4: Strengthen scenes.spec.ts S3 (write the failing assertion first)**

In `web/e2e/scenes.spec.ts`, change scenario S3 to assert the live PoseCard appears in the scene log after a pose submission (not just the UI submit). This is the failing test: against a stack without the KEK + live-crypto wiring it would not render the decrypted pose. MUST NOT be quarantined.

- [ ] **Step 5: Run E2E**

Run: `task test:e2e`
Expected: PASS — the pose is emitted (encrypted), decrypted on the live path (Tasks 1-2), the poser is a DEK participant via SetSceneFocus seeding (Tasks 3-5), and the PoseCard renders.

- [ ] **Step 6: Commit**

```text
jj commit -m "test(e2e): provision KEK + assert live PoseCard for scene pose (holomush-5rh.8.27, holomush-5rh.8.29)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Final verification (before review gates)

- [ ] `task pr-prep` (fast lane) green.
- [ ] `task test:int` green (Docker).
- [ ] `task test:e2e` green (the `.8.27` surface).
- [ ] `crypto-reviewer` (FIRST) → `abac-reviewer` (the `SetSceneFocus` participation gate) → `code-reviewer`, all READY.
- [ ] Close `holomush-5rh.8.29`; unblock and complete `holomush-5rh.8.27` (S3 strengthened).

## Spec ↔ task coverage

| Spec section | Task(s) |
| --- | --- |
| §3.1 Global flag-flip (Publisher) | Task 1 |
| §3.1 Global flag-flip (Subscriber) | Task 2 |
| §3.2 Scene reader-seeding (fatal) | Tasks 3, 4, 5 |
| §3.3 Comms recipient-seeding (genesis) | Tasks 6, 7 |
| §3.4 Binding-resolution fork (resolved: Option A — resolve in GetOrCreate create branch) | Task 6 |
| §4 Two seeding triggers (asymmetry) | Task 8 |
| §5 Invariants (register 116/117, bind 6) | Task 9 |
| §7 Testing (int) | Task 8 |
| §7 Testing (e2e, .8.27 surface) | Task 10 |
| §8 Out of scope (comms sender-echo) | n/a — holomush-jq34t |

<!-- adr-capture: sha256=af4b6090c9209f11; session=cli; ts=2026-06-09T01:04:40Z; adrs=holomush-mihtk,holomush-olpdd -->
