<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Harness Plugin-Crypto Round-Trip Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the full plugin-crypto round-trip (emit fence → publish encrypt → audit projection → read-back decrypt) into the `integrationtest` harness behind an opt-in `WithPluginCrypto()`, extracting prod's manifest-derivation helpers into a shared package so the harness reproduces prod crypto routing faithfully.

**Architecture:** Phase 1 extracts four derivation helpers from `cmd/holomush` (package `main`, unimportable) into a new `internal/plugin/cryptowiring` package consumed by both prod and the harness — the derivations take narrow interfaces so they unit-test with fakes. Phase 2 adds `WithPluginCrypto()` and mirrors prod's `sub_grpc.go:343-484` crypto wiring against the embedded bus. Phase 3 proves the round-trip with a Ginkgo integration suite.

**Tech Stack:** Go, gopher-lua/go-plugin host, NATS JetStream (embedded via `eventbustest`), Postgres testcontainer, `dek`/`kek`/`codec`/`audit`/`authguard` crypto packages, Ginkgo/Gomega.

**Spec:** `docs/superpowers/specs/2026-05-27-harness-plugin-crypto-roundtrip-design.md` (design-reviewer READY). Invariants INV-5IA-1..6 referenced per task.

---

## File Structure

| Path | Responsibility | Phase |
|------|----------------|-------|
| `internal/plugin/cryptowiring/cryptowiring.go` (Create) | `KeySelector`, `AlwaysSensitiveSet`, `CryptoKeysLookup`, `OwnerMapFromManager` + the narrow `ManifestSource` interface | 1 |
| `internal/plugin/cryptowiring/cryptowiring_test.go` (Create) | Unit tests (fakes) for the derivations + nil-guards | 1 |
| `internal/plugin/cryptowiring/cryptowiring_integration_test.go` (Create) | `//go:build integration` — `CryptoKeysLookup.Exists` against Postgres | 1 |
| `cmd/holomush/phase7_fence_wiring.go` (Modify) | Remove moved helpers; repoint to `cryptowiring` | 1 |
| `cmd/holomush/sub_grpc.go` (Modify) | Repoint `historyOwnersFromPlugins` / `buildAlwaysSensitiveSet` / `newCryptoKeysLookup` / `buildKeySelector` call sites | 1 |
| `cmd/holomush/core.go` (Modify) | Repoint `buildKeySelector` call site only (audit-side owner derivation untouched) | 1 |
| `internal/testsupport/integrationtest/crypto.go` (Create, `//go:build integration`) | `WithPluginCrypto()` + the 4-link wiring constructor | 2 |
| `internal/testsupport/integrationtest/harness.go` (Modify) | Thread `withPluginCrypto` through `startConfig` + `Start` | 2 |
| `internal/testsupport/integrationtest/plugins.go` (Modify) | Accept crypto deps in `startPlugins`; call `ConfigureEventEmitter` / `ConfigureReadbackDecryptor` | 2 |
| `test/integration/plugincrypto/plugincrypto_suite_test.go` (Create) | Ginkgo suite bootstrap | 3 |
| `test/integration/plugincrypto/roundtrip_test.go` (Create) | Positive + negative round-trip specs (INV-5IA-4/5/6) | 3 |

---

## Phase 1: Extract shared `cryptowiring` package

### Task 1: Create `cryptowiring` package + move `KeySelector`

**Files:**

- Create: `internal/plugin/cryptowiring/cryptowiring.go`
- Create: `internal/plugin/cryptowiring/cryptowiring_test.go`
- Modify: `cmd/holomush/phase7_fence_wiring.go` (remove `buildKeySelector` + `identityProductionKeySelector`)
- Modify: `cmd/holomush/core.go:523` and `cmd/holomush/sub_grpc.go` (repoint `buildKeySelector()` → `cryptowiring.KeySelector()`)

- [ ] **Step 1: Write the failing test**

`internal/plugin/cryptowiring/cryptowiring_test.go`:

```go
package cryptowiring_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/plugin/cryptowiring"
)

func TestKeySelectorReturnsIdentityCodecForEncrypt(t *testing.T) {
	sel := cryptowiring.KeySelector()
	name, label, err := sel.SelectForEncrypt(context.Background(), "events.g1.scene.x.ic")
	assert.NoError(t, err)
	assert.Equal(t, codec.NameIdentity, name)
	assert.Equal(t, codec.KeyLabel(""), label)
}

func TestKeySelectorReturnsNoKeyForDecrypt(t *testing.T) {
	sel := cryptowiring.KeySelector()
	key, err := sel.SelectForDecrypt(context.Background(), codec.NameIdentity, codec.KeyID(0))
	assert.NoError(t, err)
	assert.Equal(t, codec.NoKey, key)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./internal/plugin/cryptowiring/`
Expected: FAIL — package `cryptowiring` does not exist.

- [ ] **Step 3: Create the package, move `buildKeySelector` + `identityProductionKeySelector` verbatim**

`internal/plugin/cryptowiring/cryptowiring.go` (move the two declarations from `cmd/holomush/phase7_fence_wiring.go:36-55` verbatim; export `KeySelector`):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package cryptowiring holds the plugin-manifest-derived crypto/audit wiring
// shared by production boot (cmd/holomush) and the integration harness
// (internal/testsupport/integrationtest). Extracting these derivations keeps
// the harness faithful to prod's exact ownership/sensitivity routing.
package cryptowiring

import (
	"context"

	"github.com/holomush/holomush/internal/eventbus/codec"
)

// KeySelector returns the single identity codec.KeySelector instance threaded
// into audit.PluginConsumerManager (WithKeySelector) and history.NewReader
// (WithCodecSelector). INV-P7-9 requires SAME pointer-identity in both places.
func KeySelector() codec.KeySelector { return &identityKeySelector{} }

type identityKeySelector struct{}

func (identityKeySelector) SelectForEncrypt(_ context.Context, _ string) (codec.Name, codec.KeyLabel, error) {
	return codec.NameIdentity, "", nil
}

func (identityKeySelector) SelectForDecrypt(_ context.Context, _ codec.Name, _ codec.KeyID) (codec.Key, error) {
	return codec.NoKey, nil
}
```

- [ ] **Step 4: Repoint prod call sites + delete the moved declarations**

In `cmd/holomush/phase7_fence_wiring.go` delete `buildKeySelector` (lines 36-38) and `identityProductionKeySelector` (lines 40-55). In `cmd/holomush/core.go:523` change `pluginCodecKeySelector := buildKeySelector()` → `pluginCodecKeySelector := cryptowiring.KeySelector()` and add the import `"github.com/holomush/holomush/internal/plugin/cryptowiring"`. Grep for any other `buildKeySelector()` caller: `rg -n "buildKeySelector\(" cmd/holomush/` and repoint each.

- [ ] **Step 5: Run tests + build**

Run: `task test -- ./internal/plugin/cryptowiring/` → Expected: PASS
Run: `task build` → Expected: success (prod repointed cleanly)

- [ ] **Step 6: Commit**

`jj describe`/`jj commit` per `references/vcs-preamble.md`: `refactor(crypto): extract codec KeySelector to internal/plugin/cryptowiring (holomush-5iaov)`

---

### Task 2: Move `AlwaysSensitiveSet` (interface-based, unit-tested)

**Files:**

- Modify: `internal/plugin/cryptowiring/cryptowiring.go` (add `ManifestSource` + `AlwaysSensitiveSet`)
- Modify: `internal/plugin/cryptowiring/cryptowiring_test.go`
- Modify: `cmd/holomush/phase7_fence_wiring.go` (remove `buildAlwaysSensitiveSet` + `startsWith`)
- Modify: `cmd/holomush/sub_grpc.go:377` (repoint)

- [ ] **Step 1: Write the failing test (with a fake manifest source)**

Append to `cryptowiring_test.go`:

```go
type fakeLoadedPlugin struct {
	name        string
	alwaysTypes []string // event types declared sensitivity:always
}

type fakeManifestSource struct{ plugins []fakeLoadedPlugin }

func (f fakeManifestSource) ListPlugins() []string {
	out := make([]string, len(f.plugins))
	for i, p := range f.plugins {
		out[i] = p.name
	}
	return out
}

func (f fakeManifestSource) AlwaysSensitiveEmitTypes(pluginName string) []string {
	for _, p := range f.plugins {
		if p.name == pluginName {
			return p.alwaysTypes
		}
	}
	return nil
}

func TestAlwaysSensitiveSetQualifiesUnqualifiedTypes(t *testing.T) {
	src := fakeManifestSource{plugins: []fakeLoadedPlugin{
		{name: "core-scenes", alwaysTypes: []string{"scene_pose", "core-scenes:scene_say"}},
	}}
	got := cryptowiring.AlwaysSensitiveSet(src)
	assert.Equal(t, map[string]struct{}{
		"core-scenes:scene_pose": {},
		"core-scenes:scene_say":  {},
	}, got)
}

func TestAlwaysSensitiveSetEmptyForNilSource(t *testing.T) {
	assert.Empty(t, cryptowiring.AlwaysSensitiveSet(nil))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./internal/plugin/cryptowiring/`
Expected: FAIL — `cryptowiring.AlwaysSensitiveSet` and `ManifestSource` undefined.

- [ ] **Step 3: Add the interface + function**

> Design note: the current `buildAlwaysSensitiveSet` (`phase7_fence_wiring.go:66-89`) reaches into `mgr.ListPlugins()` + `mgr.GetLoadedPlugin(name).Manifest.Crypto.Emits`. To unit-test without a loaded `*plugin.Manager`, the extracted form takes a narrow `ManifestSource` interface that flattens "always-sensitive emit types per plugin" — `*plugin.Manager` satisfies it via a tiny adapter (Step 4). This keeps the derivation logic (prefix-qualification) unit-testable while prod passes the real manager.

Add to `cryptowiring.go` (note `strings.HasPrefix` replaces the cmd-local `startsWith`):

```go
import "strings" // add to import block

// ManifestSource is the narrow read surface the derivations need from a loaded
// plugin set. *plugin.Manager satisfies the richer original API; the prod call
// sites adapt it (see managerSource in cmd/holomush). Defined as an interface
// so cryptowiring unit tests use fakes instead of a fully-loaded Manager.
type ManifestSource interface {
	ListPlugins() []string
	// AlwaysSensitiveEmitTypes returns the crypto.emits[] event types declared
	// sensitivity:always for pluginName (qualified or unqualified).
	AlwaysSensitiveEmitTypes(pluginName string) []string
}

// AlwaysSensitiveSet produces the qualified `<plugin>:<event_type>` set the
// PluginDowngradeFence uses for INV-P7-7. Returns a non-nil empty map when src
// is nil. Mirrors buildAlwaysSensitiveSet (phase7_fence_wiring.go:66-89).
func AlwaysSensitiveSet(src ManifestSource) map[string]struct{} {
	out := map[string]struct{}{}
	if src == nil {
		return out
	}
	for _, name := range src.ListPlugins() {
		prefix := name + ":"
		for _, et := range src.AlwaysSensitiveEmitTypes(name) {
			key := et
			if !strings.HasPrefix(key, prefix) {
				key = prefix + key
			}
			out[key] = struct{}{}
		}
	}
	return out
}
```

- [ ] **Step 4: Add the `*plugin.Manager` adapter + repoint prod**

In `cmd/holomush` add a `managerSource` adapter (new small file `cmd/holomush/cryptowiring_adapter.go` or appended near the call site) implementing `cryptowiring.ManifestSource` over `*plugins.Manager` — `ListPlugins()` delegates; `AlwaysSensitiveEmitTypes(name)` reproduces the `GetLoadedPlugin(name).Manifest.Crypto.Emits` walk filtering `SensitivityAlways`:

```go
type managerSource struct{ mgr *plugins.Manager }

func (s managerSource) ListPlugins() []string { return s.mgr.ListPlugins() }

func (s managerSource) AlwaysSensitiveEmitTypes(name string) []string {
	dp, ok := s.mgr.GetLoadedPlugin(name)
	if !ok || dp.Manifest == nil || dp.Manifest.Crypto == nil {
		return nil
	}
	var out []string
	for _, emit := range dp.Manifest.Crypto.Emits {
		if emit.Sensitivity == plugins.SensitivityAlways {
			out = append(out, emit.EventType)
		}
	}
	return out
}
```

In `sub_grpc.go:377` change `alwaysSensitive := buildAlwaysSensitiveSet(pluginManager)` → `alwaysSensitive := cryptowiring.AlwaysSensitiveSet(managerSource{mgr: pluginManager})`. Delete `buildAlwaysSensitiveSet` + `startsWith` from `phase7_fence_wiring.go`.

- [ ] **Step 5: Run tests + build**

Run: `task test -- ./internal/plugin/cryptowiring/` → PASS
Run: `task build` → success

- [ ] **Step 6: Commit**

`refactor(crypto): extract AlwaysSensitiveSet to cryptowiring (holomush-5iaov)`

---

### Task 3: Move `CryptoKeysLookup`

**Files:**

- Modify: `internal/plugin/cryptowiring/cryptowiring.go` (add `CryptoKeysLookup`)
- Modify: `internal/plugin/cryptowiring/cryptowiring_test.go` (nil-guard unit test)
- Create: `internal/plugin/cryptowiring/cryptowiring_integration_test.go` (Exists-query, `//go:build integration`)
- Modify: `cmd/holomush/phase7_fence_wiring.go` (remove `newCryptoKeysLookup` + `cryptoKeysLookup`)
- Modify: `cmd/holomush/sub_grpc.go:378` (repoint)

- [ ] **Step 1: Write the failing unit test (nil-pool guard)**

Append to `cryptowiring_test.go`:

```go
func TestCryptoKeysLookupNilPoolReturnsError(t *testing.T) {
	lookup := cryptowiring.CryptoKeysLookup(nil)
	_, err := lookup.Exists(context.Background(), 42)
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./internal/plugin/cryptowiring/`
Expected: FAIL — `cryptowiring.CryptoKeysLookup` undefined.

- [ ] **Step 3: Move `newCryptoKeysLookup` + `cryptoKeysLookup` verbatim**

Move from `phase7_fence_wiring.go:101-129` into `cryptowiring.go`, exporting the constructor as `CryptoKeysLookup`; body (the `crypto_keys WHERE id=$1 AND destroyed_at IS NULL` query + `pgx.ErrNoRows`→false + nil-pool guard) is unchanged. Add imports `pgx`, `pgxpool`, `oops`, `history`.

```go
// CryptoKeysLookup wraps the pool with the Exists query satisfying
// history.CryptoKeysLookup. Filters destroyed_at IS NULL so destroyed DEKs read
// as Exists=false (INV-P7-15). Moved verbatim from newCryptoKeysLookup.
func CryptoKeysLookup(pool *pgxpool.Pool) history.CryptoKeysLookup {
	return &cryptoKeysLookup{pool: pool}
}
// ... cryptoKeysLookup struct + Exists method moved verbatim ...
```

- [ ] **Step 4: Write the Exists-query integration test**

`internal/plugin/cryptowiring/cryptowiring_integration_test.go`:

```go
//go:build integration

package cryptowiring_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/plugin/cryptowiring"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

func TestCryptoKeysLookupExistsReportsAbsentDEKAsFalse(t *testing.T) {
	ctx := context.Background()
	connStr := testutil.FreshDatabase(t, testutil.SharedPostgres(t))
	es, err := store.NewPostgresEventStore(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(es.Close)

	lookup := cryptowiring.CryptoKeysLookup(es.Pool())
	exists, err := lookup.Exists(ctx, 999999)
	require.NoError(t, err)
	assert.False(t, exists, "absent DEK id must read as Exists=false, not error")
}
```

- [ ] **Step 5: Repoint prod + run**

In `sub_grpc.go:378` change `cryptoKeysLookupForFence := newCryptoKeysLookup(pool)` → `cryptoKeysLookupForFence := cryptowiring.CryptoKeysLookup(pool)`. Delete the moved declarations from `phase7_fence_wiring.go`.

Run: `task test -- ./internal/plugin/cryptowiring/` → PASS (unit)
Run: `task test:int -- ./internal/plugin/cryptowiring/` → PASS (Exists query)
Run: `task build` → success

- [ ] **Step 6: Commit**

`refactor(crypto): extract CryptoKeysLookup to cryptowiring (holomush-5iaov)`

---

### Task 4: Add `OwnerMapFromManager` (read-side derivation; D2 narrowed — no collapse)

**Files:**

- Modify: `internal/plugin/cryptowiring/cryptowiring.go` (add `OwnerMapFromManager` + extend `ManifestSource`)
- Modify: `internal/plugin/cryptowiring/cryptowiring_test.go`
- Modify: `cmd/holomush/sub_grpc.go:336,892-927` (repoint history owners to the shared fn; delete `historyOwnersFromPlugins`)
- **DO NOT TOUCH** `cmd/holomush/core.go:574-590` — the audit-side derivation's `pcm.Add`-success gate is load-bearing (spec §1.2 / D2).

- [ ] **Step 1: Write the failing test (fake) + read-side parity intent**

Extend the `fakeManifestSource` with audit-subject + client-registration surface, then:

```go
func (f fakeManifestSource) AuditSubjects() []cryptowiring.AuditSubjectDecl {
	var out []cryptowiring.AuditSubjectDecl
	for _, p := range f.plugins {
		for _, s := range p.auditSubjects {
			out = append(out, cryptowiring.AuditSubjectDecl{PluginName: p.name, Subject: s})
		}
	}
	return out
}
func (f fakeManifestSource) HasAuditClient(name string) bool {
	for _, p := range f.plugins {
		if p.name == name {
			return p.hasClient
		}
	}
	return false
}

func TestOwnerMapFromManagerOmitsPluginsWithoutRegisteredClient(t *testing.T) {
	src := fakeManifestSource{plugins: []fakeLoadedPlugin{
		{name: "core-scenes", auditSubjects: []string{"scene:*"}, hasClient: true},
		{name: "ghost", auditSubjects: []string{"ghost:*"}, hasClient: false}, // no client → omitted
	}}
	om := cryptowiring.OwnerMapFromManager(src)
	require.NotNil(t, om)
	assert.Equal(t, "core-scenes", om.Resolve("scene:abc").PluginName)
	assert.Empty(t, om.Resolve("ghost:abc").PluginName, "ghost has no client → not owned (host fallback)")
}

func TestOwnerMapFromManagerNilWhenNoOwners(t *testing.T) {
	assert.Nil(t, cryptowiring.OwnerMapFromManager(fakeManifestSource{}))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./internal/plugin/cryptowiring/`
Expected: FAIL — `OwnerMapFromManager` / `AuditSubjectDecl` undefined.

- [ ] **Step 3: Add `OwnerMapFromManager` (port `historyOwnersFromPlugins` logic onto the interface)**

```go
import "github.com/holomush/holomush/internal/eventbus/audit"
import "log/slog"

// AuditSubjectDecl mirrors the (PluginName, Subject) pair the manager exposes
// via AuditSubjects(); redeclared here so ManifestSource stays decoupled.
type AuditSubjectDecl struct {
	PluginName string
	Subject    string
}

// Extend ManifestSource with the audit surface (Step 1 of Task 4):
//   AuditSubjects() []AuditSubjectDecl
//   HasAuditClient(pluginName string) bool

// OwnerMapFromManager builds the read-side audit.OwnerMap: a subject is owned
// iff its plugin has a registered PluginAuditService client. Returns nil when no
// plugin qualifies (reader treats nil as "host owns everything"). This is the
// READ-side derivation (ports historyOwnersFromPlugins, sub_grpc.go:892-927). It
// is intentionally NOT shared with core.go's audit-side derivation, which adds a
// load-bearing pcm.Add-success gate (spec §1.2).
func OwnerMapFromManager(src ManifestSource) *audit.OwnerMap {
	if src == nil {
		return nil
	}
	decls := src.AuditSubjects()
	owners := make([]audit.SubjectOwner, 0, len(decls))
	for _, d := range decls {
		if !src.HasAuditClient(d.PluginName) {
			continue
		}
		owners = append(owners, audit.SubjectOwner{PluginName: d.PluginName, Pattern: d.Subject})
	}
	if len(owners) == 0 {
		return nil
	}
	m, err := audit.NewOwnerMap(owners)
	if err != nil {
		slog.Error("cryptowiring: OwnerMap construction failed; plugin-owned subjects route via host fallback", "error", err)
		return nil
	}
	return m
}
```

- [ ] **Step 4: Extend `managerSource` adapter + repoint sub_grpc.go**

Add to `managerSource` (cmd/holomush): `AuditSubjects()` maps `s.mgr.AuditSubjects()` → `[]cryptowiring.AuditSubjectDecl`; `HasAuditClient(name)` returns `_, ok := s.mgr.PluginAuditClient(name); ok`. In `sub_grpc.go:336` change `owners := historyOwnersFromPlugins(pluginManager)` → `owners := cryptowiring.OwnerMapFromManager(managerSource{mgr: pluginManager})`. Delete `historyOwnersFromPlugins` (sub_grpc.go:892-927).

- [ ] **Step 5: Read-side parity check + run**

Confirm no behavior change on the prod read path: `rg -n "historyOwnersFromPlugins" cmd/holomush/` returns no hits (fully repointed). The fake-based tests assert the same client-registration filter the deleted function used.

Run: `task test -- ./internal/plugin/cryptowiring/` → PASS
Run: `task test:int` (full, to catch history-reader integration breakage from the repoint) → PASS
Run: `task build` → success

- [ ] **Step 6: Commit**

`refactor(crypto): extract read-side OwnerMapFromManager to cryptowiring (holomush-5iaov)`

---

## Phase 2: Harness `WithPluginCrypto` wiring

### Task 5: Add `WithPluginCrypto()` option + panic-without-plugins guard (INV-5IA-1/2)

**Files:**

- Create: `internal/testsupport/integrationtest/crypto.go` (`//go:build integration`)
- Modify: `internal/testsupport/integrationtest/harness.go` (`startConfig` field + Start guard)
- Test: `internal/testsupport/integrationtest/crypto_test.go` (`//go:build integration`)

- [ ] **Step 1: Write the failing test (INV-5IA-2 panic)**

`internal/testsupport/integrationtest/crypto_test.go`:

```go
//go:build integration

package integrationtest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// INV-5IA-2: WithPluginCrypto without WithInTreePlugins must panic.
func TestWithPluginCryptoWithoutPluginsPanics(t *testing.T) {
	assert.PanicsWithValue(t,
		"integrationtest: WithPluginCrypto() requires WithInTreePlugins()",
		func() { Start(t, WithPluginCrypto()) })
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test:int -- ./internal/testsupport/integrationtest/`
Expected: FAIL — `WithPluginCrypto` undefined.

- [ ] **Step 3: Add the option + startConfig field + guard**

In `harness.go` `startConfig` (line ~145) add `withPluginCrypto bool`. In `crypto.go`:

```go
//go:build integration

package integrationtest

// WithPluginCrypto wires the full plugin-crypto round-trip (emit fence → publish
// encrypt → audit projection → read-back) into the harness. REQUIRES
// WithInTreePlugins (the emitter, per-plugin consumer, and read-back decryptor
// all need the loaded Manager). Assumes crypto-CORRECT plugins: WithCryptoEnabled
// is global to the shared emitter, so a loaded plugin that emits
// sensitivity:always content without claiming Sensitive=true would reject (spec
// §6.2). Drive only crypto-correct plugins (e.g. core-scenes) under this option.
func WithPluginCrypto() StartOption {
	return func(c *startConfig) { c.withPluginCrypto = true }
}
```

In `harness.go` `Start`, after options are resolved (line ~243) add the guard:

```go
if cfg.withPluginCrypto && !cfg.withPlugins {
	panic("integrationtest: WithPluginCrypto() requires WithInTreePlugins()")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test:int -- -run TestWithPluginCryptoWithoutPluginsPanics ./internal/testsupport/integrationtest/`
Expected: PASS

- [ ] **Step 5: Commit**

`feat(test): add WithPluginCrypto opt-in + plugins-required guard (holomush-5iaov)`

---

### Task 6: Write the round-trip Ginkgo suite (RED — drives the wiring)

> TDD for integration substrate: author the target round-trip suite now. It will FAIL (links unwired) until Tasks 7-8 land. The suite defines the harness API the wiring must satisfy: `Server.EmitPluginEvent`, `Server.QueryPluginAuditRows`, `Server.ReadBackOwnRows`.

**Files:**

- Create: `test/integration/plugincrypto/plugincrypto_suite_test.go`
- Create: `test/integration/plugincrypto/roundtrip_test.go`

- [ ] **Step 1: Suite bootstrap**

`plugincrypto_suite_test.go`:

```go
//go:build integration

package plugincrypto_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPluginCrypto(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Plugin Crypto Round-Trip Suite")
}
```

- [ ] **Step 2: Write the positive round-trip spec (INV-5IA-4/6) — expected RED**

`roundtrip_test.go` (positive path; helper signatures defined here are implemented in Tasks 7-8):

```go
//go:build integration

package plugincrypto_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

var _ = Describe("plugin crypto round-trip", func() {
	var ts *integrationtest.Server
	ctx := context.Background()

	BeforeEach(func() {
		ts = integrationtest.Start(GinkgoT(), integrationtest.WithInTreePlugins(), integrationtest.WithPluginCrypto())
		DeferCleanup(ts.Stop)
	})

	It("encrypts sensitivity:always content on the wire and recovers plaintext via read-back", func() {
		// 1. Emit a sensitivity:always core-scenes IC event (claims Sensitive=true).
		emitted := ts.EmitPluginEvent(ctx, "core-scenes", "scene_pose", `{"text":"a secret pose"}`, true)

		// INV-5IA-4: encrypted on the wire — non-identity codec + a DEK row.
		Eventually(func() codec.Name { return ts.WireCodecFor(ctx, emitted.SubjectStr) }).
			ShouldNot(Equal(codec.NameIdentity))
		Expect(ts.DEKRowCount(ctx)).To(BeNumerically(">", 0))

		// 2. Event projected to the plugin audit table (scene_log) as an encrypted row.
		var rows []integrationtest.PluginAuditRow
		Eventually(func() int {
			rows = ts.QueryPluginAuditRows(ctx, "core-scenes", emitted.SubjectStr)
			return len(rows)
		}).Should(BeNumerically(">", 0))

		// 3. Read-back via host decryptor → plaintext recovered (INV-5IA-6).
		results := ts.ReadBackOwnRows(ctx, "core-scenes", rows)
		Expect(results).To(HaveLen(len(rows)))
		Expect(results[0].Plaintext).To(ContainSubstring("a secret pose"))
		// INV-5IA-6: read-back audit fired.
		Expect(ts.ReadBackAuditCount(ctx)).To(BeNumerically(">", 0))
	})
})
```

- [ ] **Step 3: Run to confirm RED (compiles against missing helpers)**

Run: `task test:int -- ./test/integration/plugincrypto/`
Expected: FAIL/compile-error — `ts.EmitPluginEvent` etc. undefined (these are implemented in Tasks 7-8). This RED state is the target the wiring must turn green.

- [ ] **Step 4: Commit**

`test(plugincrypto): add round-trip suite (red until wiring lands) (holomush-5iaov)`

---

### Task 7: Harness crypto substrate — KEK + DEK manager + crypto publisher (links 1-2)

**Files:**

- Modify: `internal/testsupport/integrationtest/crypto.go`
- Modify: `internal/testsupport/integrationtest/harness.go` (construct substrate when `withPluginCrypto`; pass crypto publisher into the emitter wiring)
- Modify: `internal/testsupport/integrationtest/plugins.go` (`startPlugins` accepts the crypto publisher + gameID; calls `ConfigureEventEmitter`)
- Modify: `harness.go` Server struct (store `dekMgr`, `cryptoSelector`, helper fields)

- [ ] **Step 1: Add the substrate constructor (mirror `holomushtest.newKEKProvider` + `dek.NewManager`)**

In `crypto.go`:

```go
import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/plugin/cryptowiring"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/stretchr/testify/require"
)

type pluginCrypto struct {
	dekMgr   dek.Manager
	selector codec.KeySelector
	publisher eventbus.Publisher // crypto-enabled (DEK + identity selector), rendering-wrapped
}

// newPluginCrypto builds the test crypto substrate: ephemeral KEK → DEK manager
// (pool-backed Store, so DEKs persist to crypto_keys) → a crypto-enabled
// publisher over the embedded bus. Mirrors holomushtest.newKEKProvider
// (server.go:492-505) + dek.NewManager (server.go:402-409).
func newPluginCrypto(t *testing.T, bus *eventbustest.Embedded, pool *pgxpool.Pool, verbReg *core.VerbRegistry) *pluginCrypto {
	t.Helper()
	ctx := context.Background()

	kekBytes := make([]byte, kek.KEKByteLength)
	_, err := rand.Read(kekBytes)
	require.NoError(t, err)
	envName := "HOLOMUSH_TEST_KEK_PLUGINCRYPTO"
	t.Setenv(envName, hex.EncodeToString(kekBytes))
	provider, err := kek.NewLocalAEADProviderForUnitTest(ctx, kek.NewEnvSource(envName, false))
	require.NoError(t, err, "newPluginCrypto: KEK provider")

	cacheCfg := dek.CacheConfig{Capacity: 64, TTL: time.Minute}
	dekMgr, err := dek.NewManager(
		provider, dek.NewStore(pool),
		dek.NewCache(cacheCfg), dek.NewParticipantsCache(cacheCfg),
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil }, // noopInv
		worldpg.NewBindingRepository(pool),
	)
	require.NoError(t, err, "newPluginCrypto: dek.NewManager")

	sel := cryptowiring.KeySelector()
	raw := bus.Bus.Publisher(eventbus.WithDEKManager(dekMgr), eventbus.WithCodecSelector(sel))
	return &pluginCrypto{
		dekMgr:    dekMgr,
		selector:  sel,
		publisher: eventbus.NewRenderingPublisher(raw, verbReg),
	}
}
```

- [ ] **Step 2: Thread the crypto publisher into `startPlugins` → `ConfigureEventEmitter` (link 1)**

In `plugins.go`, add `cryptoPublisher eventbus.Publisher` and `gameID string` to `pluginDeps`. After `ps.Start(ctx)` in `startPlugins` (line ~274), when `cryptoPublisher != nil`:

```go
if d.cryptoPublisher != nil {
	ps.Manager().ConfigureEventEmitter(
		d.cryptoPublisher,
		// WithGameID takes a GameIDProvider (func() string), NOT a string
		// (event_emitter.go:63-64) — wrap the captured gameID in a closure.
		plugins.WithGameID(func() string { return d.gameID }),
		plugins.WithCryptoEnabled(true), // link 1: scene_pose etc. → event.Sensitive=true
	)
}
```

In `harness.go` `Start`, when `cfg.withPluginCrypto`, construct `pc := newPluginCrypto(t, bus, pool, verbRegistry)` BEFORE `startPlugins` and pass `cryptoPublisher: pc.publisher, gameID: bus.Bus.GameID()` into `pluginDeps` (`pluginDeps.gameID` stays `string`; the closure above adapts it to `GameIDProvider`). Store `pc` on the `Server` for Tasks 8 + helpers (`Server.t *testing.T` already exists at `harness.go:101` for the `require.NoError(s.t, …)` calls in Task 8).

- [ ] **Step 3: Add `EmitPluginEvent` + wire-codec/DEK helpers used by the suite**

In `crypto.go`, add `Server` methods (panic via `requirePluginCrypto` if the option was absent):

```go
type EmittedEvent struct{ SubjectStr string }

// EmitPluginEvent drives a real plugin emit through the Manager's
// EmitPluginEvent boundary (the same path core-scenes commands use), returning
// the NATS subject for wire assertions.
func (s *Server) EmitPluginEvent(ctx context.Context, plugin, eventType, payloadJSON string, sensitive bool) EmittedEvent {
	s.requirePluginCrypto("EmitPluginEvent")
	// ... call s.pluginSub.Manager().EmitPluginEvent(ctx, plugin, pluginsdk.EmitEvent{
	//        Subject: plugin+":"+<entity>, Type: eventType, Payload: payloadJSON, Sensitive: sensitive})
	// EmitPluginEvent returns ONLY error (manager.go:408) — the helper DERIVES the
	// translated NATS subject from its inputs (subjectxlate.Legacy(plugin+":"+entity, gameID))
	// and returns it in EmittedEvent.SubjectStr for the wire assertions.
}

// WireCodecFor reads the JetStream message header codec for the subject.
func (s *Server) WireCodecFor(ctx context.Context, subject string) codec.Name { /* read via bus.JS */ }

// DEKRowCount counts rows in crypto_keys.
func (s *Server) DEKRowCount(ctx context.Context) int { /* SELECT count(*) FROM crypto_keys */ }
```

> Plan note: `EmitPluginEvent`'s exact `pluginsdk.EmitEvent` shape + `Manager.EmitPluginEvent` signature are grounded at `manager.go:424` / `manager_test.go:2001` — the implementer confirms via `mcp__probe__extract_code EmitPluginEvent` before filling the body.

- [ ] **Step 4: Run — partial green (emit + wire-encryption assertions)**

Run: `task test:int -- ./internal/testsupport/integrationtest/` → PASS (panic guard + any substrate unit assertions)
Run: `task build` and `task test:int -- ./test/integration/plugincrypto/` → still RED on read-back helpers (Task 8), but the emit + `WireCodecFor` ShouldNot(identity) + `DEKRowCount > 0` assertions now pass.

- [ ] **Step 5: Commit**

`feat(test): wire harness plugin-crypto emit+encrypt substrate (holomush-5iaov)`

---

### Task 8: Audit projection (link 3) + read-back decryptor (link 4) → suite GREEN

**Files:**

- Modify: `internal/testsupport/integrationtest/crypto.go`
- Modify: `internal/testsupport/integrationtest/harness.go` / `plugins.go` (call `ConfigureReadbackDecryptor`; start the PCM)

- [ ] **Step 1: Wire the PluginConsumerManager (link 3)**

In `crypto.go`, add (mirrors `core.go:556-591`, including a local `pluginAuditClientAdapter` mirroring `core.go:1159-1173`):

```go
func startPluginConsumers(t *testing.T, ctx context.Context, bus *eventbustest.Embedded, mgr *plugins.Manager, sel codec.KeySelector) *audit.PluginConsumerManager {
	t.Helper()
	pcm := audit.NewPluginConsumerManager(bus.JS, audit.WithKeySelector(sel))
	byPlugin := map[string][]string{}
	for _, d := range mgr.AuditSubjects() {
		byPlugin[d.PluginName] = append(byPlugin[d.PluginName], d.Subject)
	}
	for name, subjects := range byPlugin {
		client, ok := mgr.PluginAuditClient(name)
		if !ok {
			continue
		}
		require.NoError(t, pcm.Add(ctx, audit.PluginConsumerConfig{
			PluginName: name, Subjects: subjects, Client: pluginAuditClientAdapter{client: client},
		}), "startPluginConsumers: pcm.Add %s", name)
	}
	return pcm
}
```

Define `pluginAuditClientAdapter` with the single `AuditEvent(ctx, *pluginv1.AuditEventRequest) (*pluginv1.AuditEventResponse, error)` method delegating to the proto client (copy shape from `core.go:1164-1173`).

- [ ] **Step 2: Wire the read-back decryptor (link 4) — real guard + audit emitter**

Add to `crypto.go` (mirrors `sub_grpc.go:347-366,477-484`):

```go
func (s *Server) configureReadback(ctx context.Context, pc *pluginCrypto) {
	mgr := s.pluginSub.Manager()
	auditEm, err := authguardaudit.NewQueuedEmitter(pc.publisher, authguardaudit.WithGameID(s.bus.Bus.GameID()))
	require.NoError(s.t, err)
	sessionBridgeEm, err := authguardaudit.NewSessionBridgeEmitter(auditEm)
	require.NoError(s.t, err)

	guard, err := authguard.New(
		authguard.NewDEKParticipantLookup(pc.dekMgr),
		authguard.NewPluginManifestLookup(mgr),
		s.accessEngine,        // ABACEngine — never invoked on the plugin-readback path
		auditEm,               // BackpressureChecker — fresh QueuedEmitter ⇒ no throttle (spec §6.1)
	)
	require.NoError(s.t, err)

	owners := cryptowiring.OwnerMapFromManager(managerSourceForHarness{mgr: mgr})
	mgr.ConfigureReadbackDecryptor(history.NewReadbackDecryptor(
		owners,
		cryptowiring.AlwaysSensitiveSet(managerSourceForHarness{mgr: mgr}),
		cryptowiring.CryptoKeysLookup(s.pool),
		authguard.NewSessionBridgeGuard(guard),
		pc.dekMgr,
		sessionBridgeEm,
	))
	s.readbackAuditEm = auditEm // for ReadBackAuditCount
}
```

> `managerSourceForHarness` is the harness's copy of the `cmd/holomush` `managerSource` adapter (Task 2/4). Both are thin; sharing them is optional (a follow-up could move the adapter into `cryptowiring`, but that pulls `*plugin.Manager` into the package — deferred). Call `startPluginConsumers` + `configureReadback` from `Start` when `cfg.withPluginCrypto`, after `startPlugins`.

- [ ] **Step 3: Add `QueryPluginAuditRows` / `ReadBackOwnRows` / `ReadBackAuditCount` helpers**

```go
type PluginAuditRow struct{ /* mirrors pluginv1.AuditRow fields needed */ }
type ReadBackResult struct {
	Plaintext string
	Denied    bool // true when the host decryptor refused the row (g1/g2 deny)
}

func (s *Server) QueryPluginAuditRows(ctx context.Context, plugin, subject string) []PluginAuditRow { /* SELECT from plugin_core_scenes.scene_log */ }
func (s *Server) ReadBackOwnRows(ctx context.Context, plugin string, rows []PluginAuditRow) []ReadBackResult { /* call mgr.<ReadbackDecryptor>.DecryptOwnRow per row, mapping plaintext */ }
func (s *Server) ReadBackAuditCount(ctx context.Context) int { /* count read-back audit emissions via readbackAuditEm or events_audit */ }
```

> Implementer grounds the exact `scene_log` columns via `rg -n "scene_log" plugins/core-scenes/` and the `DecryptOwnRow` call shape via `mcp__probe__extract_code DecryptOwnRow` (readback.go:245).

- [ ] **Step 4: Run — suite GREEN (INV-5IA-4/6)**

Run: `task test:int -- ./test/integration/plugincrypto/`
Expected: PASS — positive round-trip recovers plaintext + audit fired.

- [ ] **Step 5: Commit**

`feat(test): wire harness plugin audit projection + read-back; round-trip green (holomush-5iaov)`

---

## Phase 3: Negative path + invariants + coverage

### Task 9: Negative read-back (INV-5IA-5), shared-source assertion (INV-5IA-3), coverage

**Files:**

- Modify: `test/integration/plugincrypto/roundtrip_test.go` (negative spec)
- Modify: `internal/testsupport/integrationtest/crypto.go` (optional `WithReadbackThrottled` seam if needed for negative)
- Modify: `internal/plugin/cryptowiring/cryptowiring_test.go` (INV-5IA-3 pointer-identity)

- [ ] **Step 1: Write the negative spec (readback:false → denied, no plaintext)**

Append to `roundtrip_test.go`:

```go
It("denies read-back for an event type whose manifest declares readback:false, without leaking plaintext", func() {
	// scene_publish_started is sensitivity:never (publish_events.go:61) — emit a
	// readback:false-class event and assert read-back refuses it.
	emitted := ts.EmitPluginEvent(ctx, "core-scenes", "scene_publish_started", `{"scene":"x"}`, false)
	var rows []integrationtest.PluginAuditRow
	Eventually(func() int { rows = ts.QueryPluginAuditRows(ctx, "core-scenes", emitted.SubjectStr); return len(rows) }).
		Should(BeNumerically(">", 0))
	results := ts.ReadBackOwnRows(ctx, "core-scenes", rows)
	Expect(results[0].Plaintext).To(BeEmpty(), "readback-denied row must not yield plaintext")
	Expect(results[0].Denied).To(BeTrue())
	// INV-5IA-4 negative half: a sensitivity:never event stays identity-coded.
	Expect(ts.WireCodecFor(ctx, emitted.SubjectStr)).To(Equal(codec.NameIdentity))
})
```

> Grounding note: confirm a `readback:false` (or `sensitivity:never`) event type the implementer can drive through `EmitPluginEvent`; `plugin.yaml:81,84-117` lists `sensitivity:never` notice types. The assertion targets `DenyReadbackManifestMissing` / a non-OK `RowResult` (guard.go:166-167). `ReadBackResult.Denied` is defined in Task 8.

- [ ] **Step 2: Run to verify it passes**

Run: `task test:int -- ./test/integration/plugincrypto/`
Expected: PASS (both positive and negative).

- [ ] **Step 3: Write the INV-5IA-3 pointer-identity unit test**

In `cryptowiring_test.go`, assert `KeySelector()` returns a usable identity selector and (harness side) that the SAME selector instance feeds the PCM and the publisher. The harness assertion (in `crypto_test.go`):

```go
// INV-5IA-1: WithInTreePlugins ALONE must NOT wire plugin crypto. A crypto-only
// helper called without WithPluginCrypto panics via requirePluginCrypto, proving
// the substrate is unwired (the census suite already runs WithInTreePlugins-only).
func TestWithInTreePluginsAloneDoesNotWireCrypto(t *testing.T) {
	ts := Start(t, WithInTreePlugins())
	defer ts.Stop()
	assert.Panics(t, func() {
		ts.EmitPluginEvent(context.Background(), "core-scenes", "scene_pose", `{"text":"x"}`, true)
	})
}

// INV-5IA-3: the codec selector is shared (pointer-identity) across links 2-4.
func TestPluginCryptoSharesSelectorInstance(t *testing.T) {
	ts := Start(t, WithInTreePlugins(), WithPluginCrypto())
	defer ts.Stop()
	assert.Same(t, ts.cryptoSelectorForTest(), ts.cryptoSelectorForTest()) // single instance reused
}
```

> Implementer exposes a test-only accessor `cryptoSelectorForTest()` returning `s.pluginCrypto.selector`; the real pointer-identity guarantee comes from constructing `sel` once in `newPluginCrypto` and passing the same value to both `bus.Bus.Publisher(WithCodecSelector(sel))` and `audit.NewPluginConsumerManager(WithKeySelector(sel))`.

- [ ] **Step 4: Coverage verification**

Run unit coverage on the new prod package:

```text
task test:cover -- ./internal/plugin/cryptowiring/
```

Expected: ≥80% (derivations + KeySelector + nil-guards). Note the `CryptoKeysLookup.Exists` query lines are integration-covered (Task 3 integration test) — confirm with:

```text
go test -tags=integration -coverprofile=/tmp/cw.out ./internal/plugin/cryptowiring/ && go tool cover -func=/tmp/cw.out | tail -1
```

Harness-wiring coverage (integration-only) evidence:

```text
go test -tags=integration -coverpkg=./internal/testsupport/integrationtest/... ./test/integration/plugincrypto/...
```

- [ ] **Step 5: Full gate + commit**

Run: `task test:int` (full integration — catches cross-suite breakage)
Run: `task lint:go` and `task fmt`
Commit: `test(plugincrypto): negative read-back + INV-5IA-3 selector identity + coverage (holomush-5iaov)`

---

## Out of scope (follow-up beads)

- Stale "no sensitive field" comments at `event_emitter.go:162-168` and `:67-82` (spec §1.3).
- Scene-specific publish driving (`CreateScene`, publish windows/scheduler) — `holomush-shcyu`.
- Host-subject audit projection (`audit.NewSubsystem`), cluster invalidation — explicitly excluded (spec §2).
<!-- adr-capture: sha256=a235c0ac04b4dcd2; session=cli; ts=2026-05-28T00:48:19Z; adrs=holomush-35ej2 -->
