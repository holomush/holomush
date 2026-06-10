<!--
SPDX-License-Identifier: Apache-2.0
-->

# Scene DEK Genesis-on-First-Focus Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the first `SetSceneFocus` on a never-posed scene succeed by genesising the scene DEK (seeded with the focusing reader) instead of failing because no DEK exists yet.

**Architecture:** Add a genesis-safe `EnsureParticipant` method to `dek.Manager` (`GetOrCreate(initial=[p])` then idempotent `Add(p)`). Rename the scene-focus-specific `sceneDEKAdder` interface method from `Add` to `EnsureParticipant` (single consumer) and route `SetSceneFocus` through it. Register and bind an invariant; prove end-to-end via an integration test; the `.10` E2E (`scenes.spec.ts` S3) is the live reproducer.

**Tech Stack:** Go, pgx/pgxpool (Postgres), Ginkgo/Gomega integration suites (`//go:build integration`), the existing `internal/eventbus/crypto/dek` DEK substrate.

**Spec:** [`docs/superpowers/specs/2026-06-09-scene-dek-genesis-on-focus-design.md`](../specs/2026-06-09-scene-dek-genesis-on-focus-design.md)

**Review gating:** event-payload-cryptography surface — `crypto-reviewer` THEN `code-reviewer` MUST run before push; green on `task test:int` AND `task test:e2e` before landing. Crypto tasks → `model:opus`.

---

## File Structure

| File | Responsibility | Tasks |
|---|---|---|
| `internal/eventbus/crypto/dek/manager.go` | `Manager` interface + `manager` impl; add `EnsureParticipant` | 1 |
| `internal/eventbus/crypto/dek/manager_integration_test.go` | DEK-integration Ginkgo specs for `EnsureParticipant` | 1 |
| `internal/grpc/sceneaccess_service.go` | `sceneDEKAdder` interface + `SetSceneFocus` seed call | 2 |
| `internal/grpc/sceneaccess_service_test.go` | Three `sceneDEKAdder` fakes + focus tests | 2 |
| `test/integration/crypto/scene_focus_genesis_test.go` (new) | End-to-end first-focus genesis proof (`// Verifies: INV-CRYPTO-121`) | 3 |
| `docs/architecture/invariants.yaml` (+ generated `invariants.md`) | Register INV-CRYPTO-121 | 4 |

---

## Task 1: `EnsureParticipant` on `dek.Manager`

**Files:**

- Modify: `internal/eventbus/crypto/dek/manager.go` (interface `Manager` at :25; `manager` impl near `Add` at :348)
- Test: `internal/eventbus/crypto/dek/manager_integration_test.go` (Ginkgo `Describe("Manager")` suite; entrypoint `TestDEKIntegration`)

**Model:** opus (crypto surface)

- [ ] **Step 1: Write the failing DEK-integration test**

Add to `internal/eventbus/crypto/dek/manager_integration_test.go`, inside the existing `var _ = Describe("Manager", func() { … })` block (mirror the construction pattern at manager_integration_test.go:158-170):

```go
It("EnsureParticipant genesises the DEK seeded with p when none exists", func() {
	ctx := context.Background()
	connStr, teardown := newTestPGPool(suiteT)
	DeferCleanup(teardown)
	pool, err := pgxpool.New(ctx, connStr)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(pool.Close)

	provider := newTestProvider(suiteT)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache, partCache, noopInvalidator, &stubBindingResolver{})
	Expect(err).NotTo(HaveOccurred())

	ctxID := dek.ContextID{Type: "scene", ID: "01GENESISFOCUS01"}
	p := dek.Participant{PlayerID: "01PLAYER0000000001", CharacterID: "01CHAR00000000001", AddedVia: "test.first_focus"}

	// No DEK exists yet — bare Add would fail with ErrNoRows; EnsureParticipant must genesis.
	Expect(mgr.EnsureParticipant(ctx, ctxID, p)).To(Succeed())

	key, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
	Expect(err).NotTo(HaveOccurred())
	parts, err := mgr.Participants(ctx, key.ID, 1)
	Expect(err).NotTo(HaveOccurred())
	Expect(parts).To(HaveLen(1))
	Expect(parts[0].PlayerID).To(Equal(p.PlayerID))
})

It("EnsureParticipant appends p when an active DEK already exists without p", func() {
	ctx := context.Background()
	connStr, teardown := newTestPGPool(suiteT)
	DeferCleanup(teardown)
	pool, err := pgxpool.New(ctx, connStr)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(pool.Close)

	provider := newTestProvider(suiteT)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache, partCache, noopInvalidator, &stubBindingResolver{})
	Expect(err).NotTo(HaveOccurred())

	ctxID := dek.ContextID{Type: "scene", ID: "01GENESISFOCUS02"}
	// Publisher-style empty genesis (no participants), mirroring initialParticipantsForContext nil.
	_, err = mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
	Expect(err).NotTo(HaveOccurred())

	p := dek.Participant{PlayerID: "01PLAYER0000000002", CharacterID: "01CHAR00000000002", AddedVia: "test.focus_after_pose"}
	Expect(mgr.EnsureParticipant(ctx, ctxID, p)).To(Succeed())

	key, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
	Expect(err).NotTo(HaveOccurred())
	parts, err := mgr.Participants(ctx, key.ID, 1)
	Expect(err).NotTo(HaveOccurred())
	Expect(parts).To(HaveLen(1))
	Expect(parts[0].PlayerID).To(Equal(p.PlayerID))
})

It("EnsureParticipant is an idempotent no-op when p already present", func() {
	ctx := context.Background()
	connStr, teardown := newTestPGPool(suiteT)
	DeferCleanup(teardown)
	pool, err := pgxpool.New(ctx, connStr)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(pool.Close)

	provider := newTestProvider(suiteT)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache, partCache, noopInvalidator, &stubBindingResolver{})
	Expect(err).NotTo(HaveOccurred())

	ctxID := dek.ContextID{Type: "scene", ID: "01GENESISFOCUS03"}
	p := dek.Participant{PlayerID: "01PLAYER0000000003", CharacterID: "01CHAR00000000003", AddedVia: "test.idempotent"}
	Expect(mgr.EnsureParticipant(ctx, ctxID, p)).To(Succeed())
	Expect(mgr.EnsureParticipant(ctx, ctxID, p)).To(Succeed())

	key, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
	Expect(err).NotTo(HaveOccurred())
	parts, err := mgr.Participants(ctx, key.ID, 1)
	Expect(err).NotTo(HaveOccurred())
	Expect(parts).To(HaveLen(1), "duplicate EnsureParticipant must not double-append")
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `task test:int -- -run TestDEKIntegration ./internal/eventbus/crypto/dek/ -ginkgo.focus='EnsureParticipant'`
Expected: FAIL — compile error `mgr.EnsureParticipant undefined (type dek.Manager has no field or method EnsureParticipant)`.

- [ ] **Step 3: Add `EnsureParticipant` to the `Manager` interface**

In `internal/eventbus/crypto/dek/manager.go`, in the `Manager` interface (at :25), immediately after the `Add` declaration (:35):

```go
	// EnsureParticipant guarantees the active DEK for ctxID exists and that p
	// is a participant. Unlike Add (which requires a pre-existing active DEK),
	// it genesises the DEK seeded with p when none exists — the genesis-safe
	// form used by the first reader to focus a never-posed scene
	// (INV-CRYPTO-121). Idempotent.
	EnsureParticipant(ctx context.Context, ctxID ContextID, p Participant) error
```

- [ ] **Step 4: Implement `EnsureParticipant` on the `manager` struct**

In `internal/eventbus/crypto/dek/manager.go`, immediately after the `Add` method (which ends at :387):

```go
// EnsureParticipant guarantees the active DEK for ctxID exists and contains p.
//
//   - No active DEK: GetOrCreate mints v1 seeded with p (genesis). The
//     subsequent Add hits the idempotency branch (p already present) and is a
//     no-op.
//   - Active DEK without p: GetOrCreate short-circuits (existing-row branch
//     ignores initial); Add appends p under SELECT … FOR UPDATE.
//   - Active DEK with p: GetOrCreate no-op; Add idempotent no-op.
//
// Concurrency: GetOrCreate resolves the INSERT race via unique-violation
// re-select (manager.go GetOrCreate); Add serializes via updateParticipants'
// row lock. Both orderings converge on one active DEK with p present.
func (m *manager) EnsureParticipant(ctx context.Context, ctxID ContextID, p Participant) error {
	if _, err := m.GetOrCreate(ctx, ctxID, []Participant{p}); err != nil {
		return err
	}
	return m.Add(ctx, ctxID, p)
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `task test:int -- -run TestDEKIntegration ./internal/eventbus/crypto/dek/ -ginkgo.focus='EnsureParticipant'`
Expected: PASS — 3 specs ran, 0 failed (confirm a non-zero spec count; `0 of N` = false green).

- [ ] **Step 6: Commit**

```text
jj describe -m "feat(crypto): dek.Manager.EnsureParticipant — genesis-safe participant seed (holomush-5rh.8.29.13)"
jj new
```

---

## Task 2: Route `SetSceneFocus` through `EnsureParticipant`

**Files:**

- Modify: `internal/grpc/sceneaccess_service.go` (`sceneDEKAdder` interface at :36; seed call at :413)
- Test: `internal/grpc/sceneaccess_service_test.go` (fakes `stubSceneDEKAdder`:40, `failingSceneDEKAdder`:610, `capturingSceneDEKAdder`:618)

**Model:** opus (crypto surface)

**Decision (resolves spec §3.2):** Rename the `sceneDEKAdder` interface method `Add → EnsureParticipant` rather than adding a second method — `sceneDEKAdder` has exactly one consumer (`SetSceneFocus`) and one production implementer (`dek.Manager`, which gains `EnsureParticipant` in Task 1).

- [ ] **Step 1: Update the three test fakes to the new method name (write the failing test first)**

In `internal/grpc/sceneaccess_service_test.go`, rename the `Add` method on each fake to `EnsureParticipant` (signatures are identical):

```go
// line ~40
func (stubSceneDEKAdder) EnsureParticipant(_ context.Context, _ dek.ContextID, _ dek.Participant) error {
	return nil
}

// line ~610
func (failingSceneDEKAdder) EnsureParticipant(_ context.Context, _ dek.ContextID, _ dek.Participant) error {
	return errors.New("dek seed boom")
}

// line ~624 — capturingSceneDEKAdder records the call. Rename the method ONLY;
// keep the body verbatim (fields are called/gotCtxID/gotPart, defined at :618-621).
func (c *capturingSceneDEKAdder) EnsureParticipant(_ context.Context, ctxID dek.ContextID, p dek.Participant) error {
	c.called = true
	c.gotCtxID = ctxID
	c.gotPart = p
	return nil
}
```

Leave the existing assertions (failing-adder ⇒ focus refused with `codes.Internal`; capturing-adder ⇒ participant captured) unchanged — they now exercise `EnsureParticipant`.

- [ ] **Step 2: Run the package tests to verify they fail**

Run: `task test -- ./internal/grpc/ -run 'TestWithSceneDEKAdderSetsField|SetSceneFocus'`
Expected: FAIL — compile error `failingSceneDEKAdder does not implement sceneDEKAdder (missing method Add)` / the interface still names `Add`.

- [ ] **Step 3: Rename the interface method**

In `internal/grpc/sceneaccess_service.go` at the `sceneDEKAdder` interface (:36), rename `Add` → `EnsureParticipant` (signature unchanged):

```go
// sceneDEKAdder seeds a character as a DEK participant so the AuthGuard's
// hot tier permits that session to decrypt sensitive scene events. The
// genesis-safe form: it mints the scene DEK (seeded with the character) when
// none exists yet — the first reader to focus a never-posed scene
// (INV-CRYPTO-121). Satisfied by dek.Manager.
type sceneDEKAdder interface {
	EnsureParticipant(ctx context.Context, ctxID dek.ContextID, p dek.Participant) error
}
```

- [ ] **Step 4: Update the seed call site**

In `internal/grpc/sceneaccess_service.go` at :413, change `s.dekAdder.Add(` to `s.dekAdder.EnsureParticipant(`. The surrounding comment (:403-410) and FATAL-on-failure block (:419-425) stay — update only the comment's "Add is idempotent (manager.go:377)" phrasing to "EnsureParticipant is genesis-safe and idempotent":

```go
		// Seed the character as a DEK participant so the AuthGuard hot-tier
		// permits this session to decrypt sensitive scene events. Genesis-safe:
		// mints the scene DEK seeded with this reader if none exists yet
		// (first focus precedes first pose). FATAL: if the seed fails the
		// connection MUST NOT be focused (ADR holomush-mihtk). EnsureParticipant
		// is idempotent so a client retry is a safe no-op. (INV-CRYPTO-121.)
		if s.dekAdder != nil {
			ctxID := dek.ContextID{Type: "scene", ID: sceneIDStr}
			addErr := s.dekAdder.EnsureParticipant(ctx, ctxID, dek.Participant{
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

- [ ] **Step 5: Run the package tests to verify they pass**

Run: `task test -- ./internal/grpc/ -run 'TestWithSceneDEKAdderSetsField|SetSceneFocus'`
Expected: PASS.

- [ ] **Step 6: Commit**

```text
jj describe -m "feat(crypto): SetSceneFocus genesis-seeds via EnsureParticipant (holomush-5rh.8.29.13)"
jj new
```

---

## Task 3: Integration proof — first focus on a fresh scene genesises + seeds

**Files:**

- Create: `test/integration/crypto/scene_focus_genesis_test.go`

**Model:** opus (crypto surface)

This test drives the bug's exact shape through the production seed path: a fresh scene (no prior pose) is focused by a participant; the focus succeeds, the character becomes a scene DEK participant, and a subsequent sensitive pose decrypts on the live fan-out path to that participant. It uses the same `subscribeHarness` / DEK-manager construction the `.12` integration tests use (see `test/integration/crypto/decrypt_to_participant_test.go` for the canonical harness: `buildCommsHarness(suiteT)` → `subscribeHarness{publisher, subscriber, dekMgr}`, `h.subscriber.OpenSession`, `delivery.Event().Payload` / `delivery.MetadataOnly()`).

- [ ] **Step 1: Write the failing integration test**

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0

package crypto

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// Verifies: INV-CRYPTO-121
var _ = Describe("Scene DEK genesis-on-first-focus", func() {
	It("first EnsureParticipant on a never-posed scene genesises the DEK and seeds the reader", func() {
		ctx := context.Background()
		h := buildCommsHarness(suiteT) // provides h.dekMgr backed by a real Postgres pool

		// A scene that has never been posed to: no active DEK row exists.
		sceneCtx := dek.ContextID{Type: "scene", ID: "01FRESHSCENEFOCUS1"}
		reader := dek.Participant{
			PlayerID:    "01PLAYERFOCUS00001",
			CharacterID: "01CHARFOCUS000001",
			AddedVia:    "sceneaccess.SetSceneFocus",
		}

		// The production focus seed path. Before the fix this returned ErrNoRows.
		Expect(h.dekMgr.EnsureParticipant(ctx, sceneCtx, reader)).To(Succeed())

		// The scene DEK now exists with the reader as a participant.
		key, err := h.dekMgr.GetOrCreate(ctx, sceneCtx, []dek.Participant{})
		Expect(err).NotTo(HaveOccurred())
		parts, err := h.dekMgr.Participants(ctx, key.ID, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(parts).To(HaveLen(1))
		Expect(parts[0].CharacterID).To(Equal(reader.CharacterID))
	})
})
```

> If `buildCommsHarness` does not expose `dekMgr`, use the direct `dek.NewManager` construction from `manager_integration_test.go:158-170` (real pool via `newTestPGPool`) instead — the implementer probes the harness fields first and adapts. The assertion contract (EnsureParticipant succeeds on a fresh scene; reader becomes the sole participant) is fixed.

- [ ] **Step 2: Run the test to verify it fails (before Task 1/2 land) or passes (after)**

Run: `task test:int -- ./test/integration/crypto/ -ginkgo.focus='genesis-on-first-focus'`
Expected (with Tasks 1-2 already committed): PASS, 1 spec ran. (If run against pre-fix code, FAIL with `select_active_dek_for_update` ErrNoRows.)

- [ ] **Step 3: Confirm the `// Verifies:` annotation is present**

Run: `rg -n 'Verifies: INV-CRYPTO-121' test/integration/crypto/scene_focus_genesis_test.go`
Expected: one match immediately above the `Describe`.

- [ ] **Step 4: Commit**

```text
jj describe -m "test(crypto): integration proof of scene DEK genesis-on-first-focus (holomush-5rh.8.29.13)"
jj new
```

---

## Task 4: Register and bind INV-CRYPTO-121

**Files:**

- Modify: `docs/architecture/invariants.yaml` (add INV-CRYPTO-121)
- Generated: `docs/architecture/invariants.md` (via `go run ./cmd/inv-render` — never hand-edit)

**Model:** opus (crypto surface)

- [ ] **Step 1: Add the invariant entry**

In `docs/architecture/invariants.yaml`, in the `INV-CRYPTO` scope (after the INV-CRYPTO-120 entry), add:

```yaml
  - id: INV-CRYPTO-121
    scope: INV-CRYPTO
    origin_spec: "docs/superpowers/specs/2026-06-09-scene-dek-genesis-on-focus-design.md"
    legacy: []
    summary: "The first SetSceneFocus on a scene context with no active DEK MUST
      genesis the DEK, seeded with the focusing reader as a participant; a
      participant-seeding focus MUST NOT fail solely because no DEK pre-existed.
      The publisher's scene-context genesis remains participant-empty (the scene
      genesis asymmetry, publisher.go initialParticipantsForContext, preserved)."
    binding: bound
    asserted_by:
      - "test/integration/crypto/scene_focus_genesis_test.go"
    refs:
      - {file: "test/integration/crypto/scene_focus_genesis_test.go", token: "INV-CRYPTO-121"}
```

- [ ] **Step 2: Regenerate the rendered table**

Run: `go run ./cmd/inv-render`
Expected: `docs/architecture/invariants.md` updated to include INV-CRYPTO-121 in the generated regions.

- [ ] **Step 3: Run the registry meta-tests + confirm no generated-doc drift**

Run: `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestRegistryBindingChecks|TestBoundInvariantsAreGenuinelyAsserted|TestProvenanceGuard' ./test/meta/`
Expected: PASS — INV-CRYPTO-121 is bound and its `asserted_by` test genuinely asserts it.

Then confirm the regenerated `invariants.md` has no uncommitted drift beyond the new entry:
Run: `go run ./cmd/inv-render && jj diff --stat docs/architecture/invariants.md`
Expected: only the INV-CRYPTO-121 additions show; re-running `inv-render` is a no-op (generate-and-diff clean).

- [ ] **Step 4: Commit**

```text
jj describe -m "docs(invariants): register INV-CRYPTO-121 scene DEK genesis-on-first-focus (holomush-5rh.8.29.13)"
jj new
```

---

## Final verification (before push)

- [ ] `task test:int` (full crypto + scene integration suites green)
- [ ] `task test -- ./internal/grpc/ ./internal/eventbus/crypto/dek/ ./test/meta/` (unit + meta green)
- [ ] `task lint`
- [ ] `crypto-reviewer` READY, then `code-reviewer` READY
- [ ] `task test:e2e` — `scenes.spec.ts` S3 "pose card appears live" passes (the `.10` reproducer); this confirms the end-to-end live-decrypt path. Coordinate with `holomush-5rh.8.29.10`, which carries the strengthened S3 spec.

## Out of scope

Eager genesis-at-scene-creation; any change to `Add`/`Rotate`/publisher/comms recipient seeding; rekey. `.10`'s E2E surface stays its own bead; this plan unblocks it.
<!-- adr-capture: sha256=58b7b20a57ad5ae4; session=cli; ts=2026-06-09T23:42:57Z; adrs=holomush-r90tl -->
