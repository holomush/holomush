<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Plugin Read-Back Decryption Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give plugins a host-mediated path to decrypt their own `sensitivity:always` events on read-back (snapshot direct entry) and complete the routed fence so scene participants can read decrypted history.

**Architecture:** A reusable host-side `decryptPluginRow` (per-row: fence check → `AuditRowToEvent` → `decodeAuthorizeAndDispatch`) is consumed by two entries — a new `PluginHostService.DecryptOwnAuditRows` RPC (snapshot, plugin-self-decrypt principal, gated by host-side `OwnerMap` ownership + a new `readback` manifest capability; **no ABAC `decrypt` grant** — that plumbing is unbuilt, deferred per spec §7.5) and the existing `PluginDowngradeFence` clean-row path (participant, DEK-participant-set principal). The host always holds DEKs; the plugin only ever receives plaintext. Authorization is routed by a new `ReadBack` discriminator on the AuthGuard `CheckRequest` so `decodeAuthorizeAndDispatch` is reused wholesale.

**Tech Stack:** Go, `gopher-lua` (hostfunc), `hashicorp/go-plugin` + Connect/gRPC (`buf generate`), PostgreSQL, `crypto/rand`. Tests: `task test` (unit, Docker for session/`sessiontest`), Ginkgo/Gomega for E2E (`//go:build integration`, `task test:int`).

**Spec:** [`docs/superpowers/specs/2026-05-25-plugin-readback-decrypt-design.md`](../specs/2026-05-25-plugin-readback-decrypt-design.md) (design-reviewer READY, round 3). Invariants INV-RB-1..12 referenced per task.

**Out of scope (downstream beads):** the C7 snapshot *pipeline* that calls `DecryptOwnAuditRows` (`holomush-5rh.20.26`); scene-log review/export (`holomush-cb4x`); focus-substrate backfill completeness (`oy6e`).

---

## File Structure

| File | New/Modify | Responsibility |
| --- | --- | --- |
| `internal/plugin/crypto_manifest.go` | Modify | `CryptoEmit.Readback bool` field |
| `internal/plugin/crypto_validator.go` | Modify | reject `readback:true` on non-emitted / `never` types |
| `internal/plugin/manager.go` | Modify | `Manager.PluginCanReadBack(plugin, eventType) bool` |
| `internal/eventbus/authguard/authguard.go` | Modify | extend `ManifestLookup` interface; `ReadBack` on `CheckRequest`; `PermitPluginReadbackGrant`/`DenyReadbackManifestMissing` codes |
| `internal/eventbus/authguard/adapter_manifest.go` | Modify | `manifestAdapter.PluginCanReadBack` delegates to `Manager` |
| `internal/eventbus/authguard/guard.go` | Modify | `Guard.checkPluginReadback`; route `Plugin+ReadBack` in `Check` |
| `internal/eventbus/bus.go` | Modify | `ReadBack bool` on `SessionCheckRequest` |
| `internal/eventbus/authguard/adapter_session.go` | Modify | map `ReadBack` across the eventbus↔authguard bridge |
| `internal/eventbus/history/plugin_downgrade_fence.go` | Modify | extract `fenceCheckRow`; clean-row path calls `decryptPluginRow` |
| `internal/eventbus/history/dispatcher.go` | Modify | thread `readBack` into `decodeAuthorizeAndDispatch` |
| `internal/eventbus/history/readback.go` | Create | `decryptPluginRow` + `RowResult` |
| `api/proto/holomush/plugin/v1/audit.proto` | Modify | `DecryptOwnAuditRows` RPC + request/response + `RowResult` message |
| `internal/plugin/goplugin/host_service.go` | Modify | `DecryptOwnAuditRows` host handler (OwnerMap gate + chunked decrypt) |
| `pkg/plugin/audit.go` | Modify | Go SDK `DecryptOwnAuditRows` helper |
| `internal/plugin/hostfunc/*` + `internal/plugin/lua/*` | Modify | Lua hostfunc wrapper (Go+Lua parity) |
| `plugins/core-scenes/plugin.yaml` | Modify | `readback: true` on `scene_pose`/`scene_say`/`scene_emit` |
| `schemas/plugin.schema.json` | Modify (generated) | regen for `readback` field |
| `site/docs/extending/plugin-crypto-readback.md` | Create | author-facing capability docs |
| `test/integration/crypto/readback_test.go` | Create | real-stack integration + meta-test |
| `test/integration/privacy/scene_history_readback_test.go` | Create | participant E2E |

Dependency order: **T1 → T2** (manifest); **T1 → T3** (authguard); **T4** (fence extract, independent); **T3 + T4 → T5** (primitive); **T5 → T6 → T7** (RPC, SDK/Lua); **T5 + T4 → T8** (fence completion); **all → T9**; **T10** (docs) parallel.

---

## Task 1: Manifest `readback` capability

**Files:**

- Modify: `internal/plugin/crypto_manifest.go:33`
- Modify: `internal/plugin/crypto_validator.go`
- Modify: `internal/plugin/manager.go:1504` (add sibling method)
- Modify: `internal/eventbus/authguard/authguard.go` (`ManifestLookup`)
- Modify: `internal/eventbus/authguard/adapter_manifest.go`
- Test: `internal/plugin/crypto_manifest_test.go`, `internal/plugin/crypto_validator_test.go`, `internal/plugin/manager_test.go`, `internal/eventbus/authguard/adapter_manifest_test.go`

- [ ] **Step 1: Write the failing test for the field + lookup**

In `internal/plugin/manager_test.go`:

```go
func TestPluginCanReadBack(t *testing.T) {
	t.Parallel()
	m := newTestManagerWithManifest(t, &Manifest{
		Name: "core-scenes",
		Crypto: &CryptoSection{Emits: []CryptoEmit{
			{EventType: "scene_pose", Sensitivity: SensitivityAlways, Readback: true},
			{EventType: "scene_join_ic", Sensitivity: SensitivityNever},
		}},
	})
	assert.True(t, m.PluginCanReadBack("core-scenes", "scene_pose"))
	assert.False(t, m.PluginCanReadBack("core-scenes", "scene_join_ic"), "readback not set")
	assert.False(t, m.PluginCanReadBack("core-scenes", "unknown"), "type not emitted")
	assert.False(t, m.PluginCanReadBack("other", "scene_pose"), "wrong plugin")
}
```

(`newTestManagerWithManifest` is the existing helper in `internal/plugin/crypto_manifest_lookup_test.go`, used by `TestManagerPluginRequestsDecryptionMatchesQualifiedRef`; reuse it.)

- [ ] **Step 2: Run it to verify it fails**

Run: `task test -- -run TestPluginCanReadBack ./internal/plugin/`
Expected: FAIL — `Readback` undefined and `PluginCanReadBack` undefined.

- [ ] **Step 3: Add the field**

`internal/plugin/crypto_manifest.go` — extend `CryptoEmit`:

```go
type CryptoEmit struct {
	EventType   string      `yaml:"event_type" json:"event_type"`
	Sensitivity Sensitivity `yaml:"sensitivity" json:"sensitivity"`
	Description string      `yaml:"description,omitempty" json:"description,omitempty"`
	// Readback declares that this plugin may read back and decrypt its own
	// historical events of this type via the host (the plugin never holds a
	// DEK). Default false (default-deny). MUST NOT be true for a
	// SensitivityNever type. See plugin-readback-decrypt-design INV-RB-2.
	Readback bool `yaml:"readback,omitempty" json:"readback,omitempty"`
}
```

- [ ] **Step 4: Add `Manager.PluginCanReadBack`**

`internal/plugin/manager.go` — mirror `PluginRequestsDecryption` (`:1504`) but scan `Emits`:

```go
// PluginCanReadBack returns true iff pluginName's manifest declares
// crypto.emits[].readback=true for eventType. Read-back authorization
// gate g2 (plugin-readback-decrypt-design §4). Distinct from
// PluginRequestsDecryption, which reads crypto.consumes.
func (m *Manager) PluginCanReadBack(pluginName, eventType string) bool {
	manifest := m.lookupManifest(pluginName)
	if manifest == nil || manifest.Crypto == nil {
		return false
	}
	for _, e := range manifest.Crypto.Emits {
		if e.EventType == eventType {
			return e.Readback
		}
	}
	return false
}
```

- [ ] **Step 5: Extend the `ManifestLookup` interface + adapter**

`internal/eventbus/authguard/authguard.go` — add to the existing interface:

```go
type ManifestLookup interface {
	PluginRequestsDecryption(pluginName, eventType string) bool
	PluginCanReadBack(pluginName, eventType string) bool
}
```

`internal/eventbus/authguard/adapter_manifest.go` — add:

```go
func (a *manifestAdapter) PluginCanReadBack(pluginName, eventType string) bool {
	if a == nil || a.mgr == nil {
		return false
	}
	return a.mgr.PluginCanReadBack(pluginName, eventType)
}
```

Update every other `ManifestLookup` implementor to satisfy the wider interface: `fakeManifest` (`internal/eventbus/authguard/guard_test.go`), the lookups in `test/integration/crypto/metadata_only_test.go` and `test/integration/crypto/plugin_decrypt_test.go`. Each gains a method returning `false` by default (or a configurable field mirroring its existing `PluginRequestsDecryption` fake).

- [ ] **Step 6: Run the lookup tests**

Run: `task test -- -run 'TestPluginCanReadBack|TestPluginRequestsDecryption' ./internal/plugin/ ./internal/eventbus/authguard/`
Expected: PASS.

- [ ] **Step 7: Write the failing validator test**

`internal/plugin/crypto_validator_test.go`:

```go
func TestCryptoValidatorRejectsReadbackOnNeverType(t *testing.T) {
	t.Parallel()
	err := ValidateCrypto(&Manifest{
		Name: "core-scenes",
		Crypto: &CryptoSection{Emits: []CryptoEmit{
			{EventType: "scene_join_ic", Sensitivity: SensitivityNever, Readback: true},
		}},
	})
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_READBACK_ON_NEVER")
}

func TestCryptoValidatorAllowsReadbackOnAlwaysType(t *testing.T) {
	t.Parallel()
	err := ValidateCrypto(&Manifest{
		Name: "core-scenes",
		Crypto: &CryptoSection{Emits: []CryptoEmit{
			{EventType: "scene_pose", Sensitivity: SensitivityAlways, Readback: true},
		}},
	})
	require.NoError(t, err)
}
```

(The entrypoint is `ValidateCrypto(m *Manifest)` at `crypto_validator.go:17`; the new code `PLUGIN_CRYPTO_READBACK_ON_NEVER` follows the existing `PLUGIN_CRYPTO_*` naming, e.g. `PLUGIN_CRYPTO_INVALID_SENSITIVITY`.)

- [ ] **Step 8: Run to verify it fails, then add the rule**

Run: `task test -- -run TestCryptoValidatorRejectsReadbackOnNeverType ./internal/plugin/` → FAIL.

In `crypto_validator.go`, in the per-`CryptoEmit` loop add:

```go
if e.Readback && e.Sensitivity == SensitivityNever {
	return oops.Code("PLUGIN_CRYPTO_READBACK_ON_NEVER").
		With("event_type", e.EventType).
		Errorf("readback:true is invalid on a sensitivity:never type")
}
```

- [ ] **Step 9: Run the validator + lint, then commit**

Run: `task test -- ./internal/plugin/ ./internal/eventbus/authguard/` then `task lint:go`
Expected: PASS, no lint findings.
Commit using VCS-appropriate commands per `references/vcs-preamble.md`: `feat(plugin): add crypto.emits[].readback manifest capability (holomush-m7pxs INV-RB-2)`.

---

## Task 2: core-scenes declares `readback`

**Files:**

- Modify: `plugins/core-scenes/plugin.yaml:40-55`
- Modify (generated): `schemas/plugin.schema.json`
- Test: `plugins/core-scenes/plugin_manifest_test.go` (or the existing manifest-parse test)

- [ ] **Step 1: Write the failing test**

In the core-scenes manifest test (mirror an existing `crypto.emits` assertion):

```go
func TestCoreScenesManifestDeclaresReadback(t *testing.T) {
	t.Parallel()
	m := loadCoreScenesManifest(t) // existing helper
	for _, et := range []string{"scene_pose", "scene_say", "scene_emit"} {
		e := findEmit(t, m, et)
		assert.True(t, e.Readback, "%s MUST declare readback:true for snapshot decrypt", et)
		assert.Equal(t, plugin.SensitivityAlways, e.Sensitivity)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestCoreScenesManifestDeclaresReadback ./plugins/core-scenes/` → FAIL.

- [ ] **Step 3: Add `readback: true` to the three content emits**

`plugins/core-scenes/plugin.yaml` — for `scene_pose`, `scene_say`, `scene_emit` add `readback: true`:

```yaml
    - event_type: scene_pose
      sensitivity: always
      readback: true
      description: "IC pose by a scene participant; visible to all participants in the scene's IC stream."
```

(Leave `scene_ooc` without `readback` — OOC is never archived into the published log, so the snapshot does not read it.)

- [ ] **Step 4: Regenerate the schema**

Run: `task generate` to update `schemas/plugin.schema.json` for the new `readback` field. Confirm the field appears in the generated schema (`rg readback schemas/plugin.schema.json`).

- [ ] **Step 5: Run tests + lint, then commit**

Run: `task test -- ./plugins/core-scenes/` → PASS.
Commit: `feat(core-scenes): declare crypto readback on scene content events (holomush-m7pxs)`.

---

## Task 3: AuthGuard read-back branch

**Files:**

- Modify: `internal/eventbus/authguard/authguard.go` (`CheckRequest`, decision codes)
- Modify: `internal/eventbus/authguard/guard.go:119` (add `checkPluginReadback`; route in `Check`)
- Modify: `internal/eventbus/bus.go` (`SessionCheckRequest.ReadBack`)
- Modify: `internal/eventbus/authguard/adapter_session.go` (bridge mapping)
- Test: `internal/eventbus/authguard/guard_test.go`

- [ ] **Step 1: Write the failing test**

`internal/eventbus/authguard/guard_test.go`:

```go
func TestCheckPluginReadbackPermitsWithManifestFlag(t *testing.T) {
	t.Parallel()
	// 2-gate model: permit rests on the manifest readback flag (g2);
	// OwnerMap (g1) is enforced upstream at the primitive. NO ABAC gate —
	// denyABAC proves readback does not depend on an ABAC grant (spec §7.5).
	g := NewGuard(noParts{}, fakeManifest{readback: true}, denyABAC{}, noBP{})
	dec, err := g.Check(context.Background(), CheckRequest{
		Identity:  Identity{Kind: IdentityKindPlugin, PluginName: "core-scenes"},
		EventType: "scene_pose",
		ReadBack:  true,
		KeyID:     7, KeyVersion: 1,
	})
	require.NoError(t, err)
	assert.True(t, dec.Permit, "permit on manifest flag alone — no ABAC dependency")
	assert.Equal(t, PermitPluginReadbackGrant, dec.Code)
}

func TestCheckPluginReadbackDeniedWithoutManifestFlag(t *testing.T) {
	t.Parallel()
	g := NewGuard(noParts{}, fakeManifest{readback: false}, denyABAC{}, noBP{})
	dec, err := g.Check(context.Background(), CheckRequest{
		Identity: Identity{Kind: IdentityKindPlugin, PluginName: "core-scenes"},
		EventType: "scene_pose", ReadBack: true, KeyID: 7, KeyVersion: 1,
	})
	require.NoError(t, err)
	assert.False(t, dec.Permit)
	assert.Equal(t, DenyReadbackManifestMissing, dec.Code)
}

func TestCheckPluginReadbackFalseUsesLiveDeliveryGate(t *testing.T) {
	t.Parallel()
	// ReadBack=false MUST route to the existing checkPlugin (requests_decryption).
	g := NewGuard(noParts{}, fakeManifest{readback: true, requests: false}, allowABAC{}, noBP{})
	dec, _ := g.Check(context.Background(), CheckRequest{
		Identity: Identity{Kind: IdentityKindPlugin, PluginName: "core-scenes"},
		EventType: "scene_pose", ReadBack: false, KeyID: 7, KeyVersion: 1,
	})
	assert.False(t, dec.Permit, "live-delivery gate denies without requests_decryption")
	assert.Equal(t, DenyManifestDeclarationMissing, dec.Code)
}
```

Extend `fakeManifest` with a `readback bool` field and a `PluginCanReadBack` method returning it.

- [ ] **Step 2: Run to verify failure**

Run: `task test -- -run TestCheckPluginReadback ./internal/eventbus/authguard/` → FAIL (`ReadBack`, `PermitPluginReadbackGrant`, `DenyReadbackManifestMissing` undefined).

- [ ] **Step 3: Add the discriminator + decision codes**

`internal/eventbus/authguard/authguard.go`:

```go
type CheckRequest struct {
	Identity   Identity
	KeyID      codec.KeyID
	KeyVersion uint32
	EventType  string
	EventID    ulid.ULID
	// ReadBack selects the read-back authorization path (manifest
	// crypto.emits[].readback) over the live-delivery path
	// (crypto.consumes.requests_decryption). Only meaningful for
	// IdentityKindPlugin. See plugin-readback-decrypt-design §4.
	ReadBack bool
}
```

Add to the `DecisionCode` block: `PermitPluginReadbackGrant` and `DenyReadbackManifestMissing`.

- [ ] **Step 4: Add `checkPluginReadback` + route in `Check`**

`internal/eventbus/authguard/guard.go` — new method mirroring `checkPlugin` but using `PluginCanReadBack`:

```go
// checkPluginReadback — read-back path: backpressure pre-check → readback
// manifest declaration → permit. INV-RB-2 gate g2 (manifest); gate g1
// (OwnerMap subject ownership) is enforced upstream at the primitive entry.
// NO ABAC gate — gate 3 was dropped (that plumbing is unbuilt; spec §7.5).
// ctx is unused (kept for signature parity with checkPlugin / future ABAC).
func (g *Guard) checkPluginReadback(_ context.Context, req CheckRequest) (Decision, error) {
	if g.bp.ShouldThrottle(req.Identity.PluginName) {
		return Decision{Permit: false, Code: DenyAuditBackpressure, Reason: "audit-emit queue throttled"}, nil
	}
	if !g.manifest.PluginCanReadBack(req.Identity.PluginName, req.EventType) {
		return Decision{Permit: false, Code: DenyReadbackManifestMissing, Reason: "manifest does not declare crypto.emits[].readback"}, nil
	}
	return Decision{Permit: true, Code: PermitPluginReadbackGrant}, nil
}
```

In `Guard.Check`, where it dispatches on `Identity.Kind`, route the plugin case:

```go
case IdentityKindPlugin:
	if req.ReadBack {
		return g.checkPluginReadback(ctx, req)
	}
	return g.checkPlugin(ctx, req)
```

- [ ] **Step 5: Thread `ReadBack` across the eventbus↔authguard bridge**

`internal/eventbus/bus.go` — add `ReadBack bool` to `SessionCheckRequest`. `internal/eventbus/authguard/adapter_session.go` — in the `SessionCheckRequest → authguard.CheckRequest` mapping, copy `ReadBack: req.ReadBack`.

- [ ] **Step 6: Run + lint, then commit**

Run: `task test -- ./internal/eventbus/authguard/ ./internal/eventbus/` then `task lint:go` → PASS.
Commit: `feat(authguard): read-back authz branch (checkPluginReadback, ReadBack discriminator) (holomush-m7pxs INV-RB-2)`.

---

## Task 4: Extract the shared per-row fence check

**Files:**

- Modify: `internal/eventbus/history/plugin_downgrade_fence.go:160-233`
- Test: `internal/eventbus/history/plugin_downgrade_fence_test.go`

- [ ] **Step 1: Write the failing test for the extracted function**

`internal/eventbus/history/plugin_downgrade_fence_test.go`:

```go
func TestFenceCheckRow(t *testing.T) {
	t.Parallel()
	always := map[string]struct{}{"scene_pose": {}}
	lookup := fakeCryptoKeysLookup{exists: true}

	// identity codec on always-sensitive → downgrade refusal.
	r, err := fenceCheckRow(context.Background(), rowOf("scene_pose", "identity", nil), always, lookup)
	require.NoError(t, err)
	assert.Equal(t, fenceRefuseDowngrade, r)

	// non-identity, dek present → clean.
	dek := uint64(7)
	r, err = fenceCheckRow(context.Background(), rowOf("scene_pose", "xchacha20poly1305-v1", &dek), always, lookup)
	require.NoError(t, err)
	assert.Equal(t, fenceClean, r)

	// non-identity, dek absent → DEK-missing refusal.
	r, err = fenceCheckRow(context.Background(), rowOf("scene_pose", "xchacha20poly1305-v1", nil), always, lookup)
	require.NoError(t, err)
	assert.Equal(t, fenceRefuseDEKMissing, r)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `task test -- -run TestFenceCheckRow ./internal/eventbus/history/` → FAIL.

- [ ] **Step 3: Extract `fenceCheckRow`**

`internal/eventbus/history/plugin_downgrade_fence.go` — introduce a result enum and a pure function, then call it from `fencedStream.Next`:

```go
type fenceVerdict int

const (
	fenceClean fenceVerdict = iota
	fenceRefuseDowngrade   // INV-P7-7
	fenceRefuseDEKMissing  // INV-P7-15
	fenceRefuseInternal
)

// fenceCheckRow applies INV-P7-7 (downgrade) + INV-P7-15 (DEK existence)
// to one plugin audit row. Pure except for the cryptoKeys existence
// lookup. Shared by fencedStream.Next (routed reads) and the snapshot
// direct entry (DecryptOwnAuditRows) so INV-RB-5 holds on both paths.
func fenceCheckRow(ctx context.Context, row *pluginauditpb.AuditRow, alwaysSensitive map[string]struct{}, lookup CryptoKeysLookup) (fenceVerdict, error) {
	if row.GetCodec() == "identity" {
		if _, sensitive := alwaysSensitive[row.GetType()]; sensitive {
			return fenceRefuseDowngrade, nil
		}
		return fenceClean, nil
	}
	if row.DekRef == nil {
		return fenceRefuseDEKMissing, nil
	}
	if lookup == nil {
		return fenceRefuseInternal, nil
	}
	exists, err := lookup.Exists(ctx, *row.DekRef)
	if err != nil {
		return fenceClean, oops.Code("AUDIT_ROW_DEK_LOOKUP_FAILED").With("dek_ref", *row.DekRef).Wrap(err)
	}
	if !exists {
		return fenceRefuseDEKMissing, nil
	}
	return fenceClean, nil
}
```

Rewrite `fencedStream.Next`'s INV-P7-7/INV-P7-15 block to call `fenceCheckRow` and map verdicts to the existing `refuseEvent(...)` reasons + `emitViolationBounded` on downgrade. Behavior MUST be identical to today (this step is a pure refactor — the clean-row path still `return ev, nil` until Task 9).

- [ ] **Step 4: Run the full fence suite**

Run: `task test -- ./internal/eventbus/history/` → PASS (all pre-existing fence tests green; refactor is behavior-preserving).

- [ ] **Step 5: Commit**

Commit: `refactor(history): extract fenceCheckRow for reuse by read-back paths (holomush-m7pxs INV-RB-5)`.

---

## Task 5: The `decryptPluginRow` primitive

**Files:**

- Create: `internal/eventbus/history/readback.go`
- Modify: `internal/eventbus/history/dispatcher.go:252` (thread `readBack`)
- Test: `internal/eventbus/history/readback_test.go`

- [ ] **Step 1: Write the failing test**

`internal/eventbus/history/readback_test.go`:

```go
func TestDecryptPluginRowPlaintextOnCleanRow(t *testing.T) {
	t.Parallel()
	deps := newReadbackDeps(t) // wires fakeGuard(permit), fakeDEK, fakeAuditEmitter, always-set, exists-lookup
	row := encryptedRow(t, deps, "scene_pose", []byte("Alice poses."))
	res := decryptPluginRow(context.Background(), pluginPrincipal("core-scenes", "inst-1"), row, deps)
	require.True(t, res.OK())
	assert.Equal(t, []byte("Alice poses."), res.Plaintext)
	assert.Len(t, deps.audit.records, 1, "INV-19 audit emitted (INV-RB-3)")
}

func TestDecryptPluginRowRefusesDowngrade(t *testing.T) {
	t.Parallel()
	deps := newReadbackDeps(t)
	row := rowOf("scene_pose", "identity", nil) // identity on always-sensitive
	res := decryptPluginRow(context.Background(), pluginPrincipal("core-scenes", "inst-1"), row, deps)
	require.False(t, res.OK())
	assert.Equal(t, eventbus.NoPlaintextReasonDowngradeRefused, res.Reason)
	assert.Empty(t, deps.audit.records, "no decrypt → no audit")
}

func TestDecryptPluginRowFailClosedWithoutAuditEmitter(t *testing.T) {
	t.Parallel()
	deps := newReadbackDeps(t)
	deps.audit = nil // INV-RB-3 fail-closed
	row := encryptedRow(t, deps, "scene_pose", []byte("x"))
	res := decryptPluginRow(context.Background(), pluginPrincipal("core-scenes", "inst-1"), row, deps)
	assert.False(t, res.OK())
}
```

- [ ] **Step 2: Run to verify failure**

Run: `task test -- -run TestDecryptPluginRow ./internal/eventbus/history/` → FAIL.

- [ ] **Step 3: Thread `readBack` into the decode path**

`internal/eventbus/history/dispatcher.go` — add a `readBack bool` parameter to `decodeAuthorizeAndDispatch` and set it on the built request:

```go
req := eventbus.SessionCheckRequest{
	Identity:   identity,
	KeyID:      keyID,
	KeyVersion: keyVersion,
	EventType:  envelope.GetType(),
	EventID:    eventID,
	ReadBack:   readBack,
}
```

Update existing callers (hot/cold tier) to pass `false`.

- [ ] **Step 4: Write the primitive + `RowResult`**

`internal/eventbus/history/readback.go`:

```go
// RowResult is the per-row outcome of read-back decryption: either
// Plaintext (OK) or a typed refusal Reason. INV-RB-12.
type RowResult struct {
	Plaintext []byte
	Reason    eventbus.NoPlaintextReason // zero == none (OK)
	Err       error                      // non-nil for terminal errors (e.g. DEK lookup)
}

func (r RowResult) OK() bool { return r.Err == nil && r.Reason == eventbus.NoPlaintextReasonUnspecified }

// readbackDeps bundles the host-held machinery the primitive needs.
type readbackDeps struct {
	alwaysSensitive map[string]struct{}
	cryptoKeys      CryptoKeysLookup
	guard           eventbus.SessionAuthGuard
	dek             eventbus.SessionDEKManager
	audit           eventbus.SessionAuditEmitter
}

// decryptPluginRow runs the shared read-back per-row pipeline: fence
// (INV-P7-7/P7-15, INV-RB-5) → AuditRowToEvent → decodeAuthorizeAndDispatch
// (AAD INV-E20 + DEK resolve + codec.Decode + INV-19 audit, INV-RB-1/3/4).
// The host always holds the DEK; the principal (plugin self-decrypt or
// participant character) is authorized inside decodeAuthorizeAndDispatch.
func decryptPluginRow(ctx context.Context, identity eventbus.SessionIdentity, row *pluginauditpb.AuditRow, d readbackDeps) RowResult {
	verdict, err := fenceCheckRow(ctx, row, d.alwaysSensitive, d.cryptoKeys)
	if err != nil {
		return RowResult{Err: err}
	}
	switch verdict {
	case fenceRefuseDowngrade:
		return RowResult{Reason: eventbus.NoPlaintextReasonDowngradeRefused}
	case fenceRefuseDEKMissing:
		return RowResult{Reason: eventbus.NoPlaintextReasonDEKMissing}
	case fenceRefuseInternal:
		return RowResult{Reason: eventbus.NoPlaintextReasonInternal}
	}

	envelope := AuditRowToEvent(row)
	// AuditRowToEvent omits Payload (it is AAD-only, INV-TS-5). Restore the
	// ciphertext for c.Decode — aad.Build excludes Payload, so AAD is
	// unaffected; this matches how the hot/cold tiers feed a full envelope.
	envelope.Payload = row.GetPayload()
	codecName := codec.Name(row.GetCodec())
	keyID := codec.KeyID(row.GetDekRef())
	readBack := identity.Kind == eventbus.IdentityKindPlugin
	ev, metaOnly, err := decodeAuthorizeAndDispatch(
		ctx, envelope, codecName, keyID, row.GetDekVersion(),
		identity, d.guard, d.dek, d.audit, readBack,
	)
	if err != nil {
		return RowResult{Err: err}
	}
	if metaOnly {
		return RowResult{Reason: ev.NoPlaintextReason}
	}
	return RowResult{Plaintext: ev.Payload}
}
```

(`NoPlaintextReasonUnspecified` is the zero value at `eventbus/types.go:34` — do NOT add a new reason constant; `TestNoPlaintextReasonEnumParity` pins the count at 8. Confirm `row.GetDekRef()` / `row.GetDekVersion()` accessors on the generated `pluginauditpb.AuditRow`.)

- [ ] **Step 5: Run + lint, commit**

Run: `task test -- ./internal/eventbus/history/` then `task lint:go` → PASS.
Commit: `feat(history): decryptPluginRow read-back primitive (holomush-m7pxs INV-RB-1/3/4/5/12)`.

---

## Task 6: `DecryptOwnAuditRows` RPC + host handler

**Files:**

- Modify: `api/proto/holomush/plugin/v1/audit.proto`
- Modify: `internal/plugin/goplugin/host_service.go:495` (pattern)
- Test: `internal/plugin/goplugin/host_service_test.go`

- [ ] **Step 1: Add the proto + regenerate**

`api/proto/holomush/plugin/v1/audit.proto` — add to `PluginAuditService` (or `PluginHostService` if the plugin→host audit calls live there; match where `AuditEvent`/`QueryHistory` are defined for the host-implemented side):

```protobuf
// DecryptOwnAuditRows decrypts a batch of the calling plugin's OWN audit
// rows host-side. The plugin never holds a DEK. Per-row result envelope
// (INV-RB-12). Batch capped at 500 server-side. Authorization: OwnerMap
// subject ownership (g1) + crypto.emits[].readback manifest flag (g2) (INV-RB-2).
rpc DecryptOwnAuditRows(DecryptOwnAuditRowsRequest) returns (DecryptOwnAuditRowsResponse);
```

```protobuf
message DecryptOwnAuditRowsRequest {
  repeated AuditRow rows = 1;
}
message DecryptOwnAuditRowsResponse {
  repeated RowResult results = 1; // 1:1 with request rows, same order (INV-RB-12)
}
message RowResult {
  bytes id = 1;            // echoes AuditRow.id for correlation
  bytes plaintext = 2;     // set iff decrypted
  string no_plaintext_reason = 3; // set iff refused (e.g. "downgrade_refused", "dek_missing")
}
```

Run: `task generate` (buf). Confirm generated Go in `pkg/proto/holomush/plugin/v1/`.

- [ ] **Step 2: Write the failing handler test**

`internal/plugin/goplugin/host_service_test.go`:

```go
func TestDecryptOwnAuditRowsRejectsForeignSubject(t *testing.T) {
	t.Parallel()
	s := newHostServiceForPlugin(t, "core-scenes", withOwnerMap(ownerOf("events.main.scene.X.ic", "core-scenes")))
	resp, err := s.DecryptOwnAuditRows(ctx, &pluginv1.DecryptOwnAuditRowsRequest{
		Rows: []*pluginv1.AuditRow{rowWithSubject("events.main.comm.whisper", "scene_pose")},
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "not_owner", resp.Results[0].NoPlaintextReason, "INV-RB-2 g1: not the owner")
}

func TestDecryptOwnAuditRowsCapsBatchAt500(t *testing.T) {
	t.Parallel()
	s := newHostServiceForPlugin(t, "core-scenes", withOwnerMap(...))
	_, err := s.DecryptOwnAuditRows(ctx, &pluginv1.DecryptOwnAuditRowsRequest{Rows: make([]*pluginv1.AuditRow, 501)})
	errutil.AssertErrorCode(t, err, "DECRYPT_BATCH_TOO_LARGE")
}
```

- [ ] **Step 3: Run to verify failure**

Run: `task test -- -run TestDecryptOwnAuditRows ./internal/plugin/goplugin/` → FAIL.

- [ ] **Step 4: Implement the handler**

`internal/plugin/goplugin/host_service.go` — mirror `QueryStreamHistory`'s structure, but note this handler **rejects** an over-cap batch (vs `QueryStreamHistory`, which silently *clamps* `count` to `maxQueryStreamHistoryCount` at `:512`). Use a dedicated `const maxDecryptBatch = 500` rather than reusing the clamp constant, to keep the reject-vs-clamp distinction explicit:

```go
const maxDecryptBatch = 500

func (s *pluginHostServiceServer) DecryptOwnAuditRows(ctx context.Context, req *pluginv1.DecryptOwnAuditRowsRequest) (*pluginv1.DecryptOwnAuditRowsResponse, error) {
	if len(req.GetRows()) > maxDecryptBatch {
		return nil, oops.Code("DECRYPT_BATCH_TOO_LARGE").
			With("plugin", s.pluginName).With("count", len(req.GetRows())).
			Errorf("batch exceeds cap %d", maxDecryptBatch)
	}
	dec := s.host.ReadbackDecryptor() // host wiring exposes the primitive's deps + OwnerMap
	results := make([]*pluginv1.RowResult, 0, len(req.GetRows()))
	for _, row := range req.GetRows() {
		results = append(results, dec.DecryptOwnRow(ctx, s.pluginName, s.instanceID, row))
	}
	return &pluginv1.DecryptOwnAuditRowsResponse{Results: results}, nil
}
```

Add a host-side `ReadbackDecryptor` (a thin type in `internal/eventbus/history` or `internal/plugin`) whose `DecryptOwnRow` does the **OwnerMap gate 1** then calls `decryptPluginRow` with a plugin `SessionIdentity`, mapping `RowResult` → proto `RowResult`. Owner mismatch → `no_plaintext_reason="not_owner"` (no `decryptPluginRow` call).

- [ ] **Step 5: Run + lint, commit**

Run: `task test -- ./internal/plugin/goplugin/ ./internal/eventbus/history/` then `task lint:go` → PASS.
Commit: `feat(plugin): DecryptOwnAuditRows host RPC (OwnerMap gate + chunked decrypt) (holomush-m7pxs INV-RB-2/6/12)`.

---

## Task 7: Go SDK + Lua hostfunc parity

**Files:**

- Modify: `pkg/plugin/audit.go`
- Modify: `internal/plugin/hostfunc/*` + `internal/plugin/lua/*` (mirror an existing host RPC)
- Test: `pkg/plugin/audit_test.go`, `internal/plugin/hostfunc/*_test.go`

- [ ] **Step 1: Write failing Go SDK test**

`pkg/plugin/audit_test.go` — assert the SDK method calls the client and returns per-row plaintext/refusal:

```go
func TestDecryptOwnAuditRowsSDK(t *testing.T) {
	t.Parallel()
	c := &fakeHostClient{decrypt: func(rows []*pluginv1.AuditRow) []*pluginv1.RowResult {
		return []*pluginv1.RowResult{{Id: rows[0].Id, Plaintext: []byte("hi")}}
	}}
	got, err := DecryptOwnAuditRows(ctx, c, []*pluginv1.AuditRow{{Id: idBytes}})
	require.NoError(t, err)
	assert.Equal(t, []byte("hi"), got[0].Plaintext)
}
```

- [ ] **Step 2: Run → fail; implement the SDK helper**

Add `DecryptOwnAuditRows` to `pkg/plugin/audit.go` wrapping the generated client method (mirror the existing `LoadForQuery`/`QueryStreamHistory` SDK shape).

- [ ] **Step 3: Write the failing Lua hostfunc test, then implement**

Mirror the existing `query_stream_history` Lua wrapper: register `holomush.host:decrypt_own_audit_rows(rows)` in the Lua VM, marshalling rows in and `{plaintext|reason}` out. Assert via the hostfunc test harness that a Lua call reaches the host client.

Per the host-RPC Go+Lua parity invariant, this MUST ship in the same change as the Go SDK method.

- [ ] **Step 4: Run + commit**

Run: `task test -- ./pkg/plugin/ ./internal/plugin/hostfunc/ ./internal/plugin/lua/` → PASS.
Commit: `feat(plugin): DecryptOwnAuditRows Go SDK + Lua hostfunc (parity) (holomush-m7pxs)`.

---

## Task 8: Complete the fence — routed reads decrypt

**Files:**

- Modify: `internal/eventbus/history/plugin_downgrade_fence.go` (`fencedStream.Next` clean-row path; thread caller identity)
- Modify: `internal/eventbus/history/tier.go:420` (pass `q.Caller`/`q.Identity` into the fence)
- Test: `internal/eventbus/history/plugin_downgrade_fence_test.go` (migrate ciphertext→plaintext assertions)

- [ ] **Step 1: Update the fence test from passthrough to plaintext**

Migrate the existing "clean row passes through as ciphertext" assertions to expect **plaintext** for an authorized member, and a refusal for a non-member:

```go
func TestFenceDecryptsCleanRowForMember(t *testing.T) {
	t.Parallel()
	f := newFenceWithReadback(t, memberCharacter("char-1")) // wires decryptPluginRow deps + character identity
	ev, err := fencedNext(t, f, encryptedRow(t, "scene_pose", []byte("Alice poses.")))
	require.NoError(t, err)
	assert.False(t, ev.MetadataOnly)
	assert.Equal(t, []byte("Alice poses."), ev.Payload)
}

func TestFenceRefusesCleanRowForNonMember(t *testing.T) {
	t.Parallel()
	f := newFenceWithReadback(t, nonMemberCharacter("char-2"))
	ev, err := fencedNext(t, f, encryptedRow(t, "scene_pose", []byte("secret")))
	require.NoError(t, err)
	assert.True(t, ev.MetadataOnly, "non-member MUST NOT get plaintext (INV-RB-7)")
}
```

- [ ] **Step 2: Run to verify failure**

Run: `task test -- -run 'TestFenceDecrypts|TestFenceRefusesCleanRow' ./internal/eventbus/history/` → FAIL.

- [ ] **Step 3: Wire the clean-row path to `decryptPluginRow`**

In `fencedStream.Next`, replace the final `// Clean row — pass through` `return ev, nil` with a call to `decryptPluginRow` using a **character** `SessionIdentity` derived from the query caller. The fence gains the readback deps + the caller identity at construction. Map the `RowResult`: plaintext → set `ev.Payload`; refusal → `refuseEvent(ev, res.Reason)`; `Err` → return the error.

`internal/eventbus/history/tier.go:420` — thread the caller into the fenced router so the fence can build the identity:

```go
return r.fencedRouter().QueryHistory(ctx, owner.PluginName, q) // q.Caller / q.Identity already carry the principal (bus.go:117)
```

(If the `PluginHistoryRouter` interface does not currently forward identity to the fence, extend the fence constructor/`QueryHistory` to accept it from `q`. Convert `q.Caller` (Actor) → `authguard.Identity` via the existing `ToSessionIdentity`/Actor mapping; `ReadBack=false` so `Check` routes to `checkCharacter` DEK-membership.)

- [ ] **Step 4: Run the full history + privacy unit suites**

Run: `task test -- ./internal/eventbus/history/ ./internal/grpc/` → PASS. Confirm no other fence test regressed.

- [ ] **Step 5: Commit**

Commit: `feat(history): fence decrypts clean rows for authorized routed readers (holomush-m7pxs INV-RB-7)`.

---

## Task 9: Integration + E2E (real stack)

**Files:**

- Create: `test/integration/crypto/readback_test.go` (real-stack integration + INV-RB meta-test)
- Create: `test/integration/privacy/scene_history_readback_test.go` (participant E2E, Ginkgo)

- [ ] **Step 1: Real-stack integration — authorized vs unauthorized**

`test/integration/crypto/readback_test.go` (`//go:build integration`) using the real codec + DEK manager + fence + primitive (NOT `fakeHistoryReader` — closes the fake-bus coverage gap). Snapshot direct-entry path: authorized → plaintext (INV-RB-1/2/3/4/12), unauthorized → refused (INV-RB-2):

```go
func TestReadbackAuthorizedReturnsPlaintext(t *testing.T) {
	h := integrationtest.New(t) // real Postgres + NATS + CoreServer
	// emit an encrypted scene_pose as core-scenes; read it back via DecryptOwnAuditRows.
	row := h.EmitSceneIC(t, "scene_pose", "Alice poses.")
	res := h.DecryptOwnAuditRows(t, "core-scenes", []*pluginv1.AuditRow{row})
	require.Equal(t, []byte("Alice poses."), res[0].Plaintext)
	h.AssertPluginDecryptAudit(t, "core-scenes", 1) // INV-RB-3
}

func TestReadbackForeignSubjectRefused(t *testing.T) { /* core-comm row via core-scenes → not_owner */ }
func TestReadbackWithoutReadbackFlagDenied(t *testing.T) { /* plugin w/o readback manifest → deny */ }
```

- [ ] **Step 2: Meta-test — every INV-RB-* referenced**

Enumerate the INV-RB-* that **this plan implements** — **1, 2, 3, 4, 5, 7, 9, 11, 12** plus the direct-entry side of **6** — and assert each is named in ≥1 test (`rg`-style registry test mirroring the master crypto spec's invariant-coverage discipline). **INV-RB-8** (snapshot atomicity), **INV-RB-10** (`SNAPSHOT_DECRYPT_FAILED`), and the *consumer* side of **INV-RB-6** (the snapshot reads via the direct entry, not `QueryHistory`) are asserted by the C7 snapshot-pipeline bead (`holomush-5rh.20.26`), which consumes `DecryptOwnAuditRows` — out of scope here (see plan header). Run: `task test:int -- ./test/integration/crypto/` → PASS.

- [ ] **Step 3: Participant E2E — scene-IC scrollback**

`test/integration/privacy/scene_history_readback_test.go` (Ginkgo): a scene member reconnects, backfills the scene-IC stream via `WebQueryStreamHistory`, and receives **decrypted** poses; a non-member receives refused/metadata-only (INV-RB-7). Use the `integrationtest` harness with `WithPolicyEngine` seeded to permit.

- [ ] **Step 4: Run E2E, commit**

Run: `task test:int -- ./test/integration/crypto/ ./test/integration/privacy/` → PASS.
Commit: `test(crypto): real-stack read-back integration + participant E2E (holomush-m7pxs INV-RB-3/7/11/12)`.

---

## Task 10: Documentation (PR-blocking)

**Files:**

- Create: `site/docs/extending/plugin-crypto-readback.md`
- Modify: `site/docs/contributing/` (a sentence in the relevant crypto/eventbus contributor page)

- [ ] **Step 1: Write the author-facing capability doc**

`site/docs/extending/plugin-crypto-readback.md`: what `crypto.emits[].readback` means, that decrypt is host-mediated (plugin never holds a DEK), the three authz gates (OwnerMap ownership / `readback` flag / ABAC `decrypt`), the INV-19 audit, and that the host caps batches at 500. Link the design spec.

- [ ] **Step 2: Note the fence-contract change for contributors**

Add a short note to `site/docs/contributing/event-emit-pipeline.md` (the closest existing crypto/eventbus contributor page; if it lacks a read-back section, add one): routed plugin-owned reads now decrypt clean rows for authorized readers (was ciphertext-passthrough); `fenceCheckRow` is shared with the snapshot read-back path.

- [ ] **Step 3: Build docs + commit**

Run: `task docs:build` → success (no broken links).
Commit: `docs(extending): plugin read-back decrypt capability (holomush-m7pxs)`.

---

## Verification (whole plan)

- [ ] `task lint` — zero findings
- [ ] `task test` — all unit packages green, ≥80% per-package coverage on touched packages (`task test:cover`)
- [ ] `task test:int` — integration + E2E green (Docker)
- [ ] `task pr-prep` — full lane green before any push
<!-- adr-capture: sha256=35b67f992e0afa6a; ts=2026-05-25T16:15:06Z; adrs=holomush-g3d4l,holomush-edqh1,holomush-c3kyv,holomush-wfh42 -->
