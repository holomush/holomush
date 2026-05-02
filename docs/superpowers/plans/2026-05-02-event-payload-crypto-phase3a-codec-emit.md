<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Event Payload Crypto — Phase 3a — Codec + Emit + Sensitivity Fence

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land emit-side encryption: an `xchacha20poly1305-v1` codec, a per-emit `Sensitive` flag with a host-side fence, and a sensitivity-aware publish path that produces ciphertext on the bus + matching `events_audit` row + `App-Dek-Ref/Version` headers when a plugin manifest declares `sensitivity: may|always`. Decryption-on-fanout is **out of scope** (Phase 3b).

**Architecture:** Three substrate edits (codec interface gains `aad []byte`, `codec.Key` gains `Version uint32`, `dek.Material.AsCodecKey` gains `version` param) plus a sensitivity-aware emit/publish pipeline. The emitter resolves manifest sensitivity, runs the host-side fence, and sets a host-internal `event.Sensitive bool` flag. The publisher branches on that flag: false → existing identity path; true → DEK acquire + AAD build + xchacha encrypt + crypto headers. End-to-end behavior is gated by a `Crypto.Enabled` config flag that defaults to `false`.

**Tech Stack:** Go 1.22+, `golang.org/x/crypto/chacha20poly1305`, the existing eventbus substrate (`internal/eventbus/codec`, `internal/eventbus/crypto/{aad,dek,kek}`, `internal/eventbus/publisher.go`, `internal/eventbus/audit/projection.go`), the Phase 1 manifest grammar (`internal/plugin/crypto_manifest.go`).

**Grounding:** [`docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3a-grounding.md`](../specs/2026-05-02-event-payload-crypto-phase3a-grounding.md) — resolves four substrate seams; mandatory pre-read before this plan.

---

## File structure

| File | Status | Responsibility |
| --- | --- | --- |
| `internal/eventbus/codec/codec.go` | MODIFY | `Codec.Encode/Decode` gain `aad []byte`; `Key` gains `Version uint32`; `IdentityCodec` impls update |
| `internal/eventbus/codec/codec_test.go` | MODIFY | 4 callsite signature updates |
| `internal/eventbus/codec/registry.go`, `registry_test.go` | MODIFY (test) | `stubCodec` gets new signature |
| `internal/eventbus/codec/xchacha20poly1305.go` | CREATE | Codec impl satisfying new interface |
| `internal/eventbus/codec/xchacha20poly1305_test.go` | CREATE | Round-trip + AAD-binding + tamper tests |
| `internal/eventbus/crypto/dek/material.go` | MODIFY | `AsCodecKey(id, version)` companion edit |
| `internal/eventbus/crypto/dek/material_test.go` | MODIFY | Test signature update |
| `internal/eventbus/crypto/dek/manager.go` | MODIFY | 3 `AsCodecKey` callsites pass version |
| `internal/eventbus/publisher.go` | MODIFY | `HeaderDekRef`/`HeaderDekVersion` consts; `Encode` call gets `aad`; sensitivity-aware crypto branch |
| `internal/eventbus/publisher_test.go` | MODIFY | `errCodec` signature; header constant test |
| `internal/eventbus/subscriber.go` | MODIFY | `Decode` call gets `aad=nil` |
| `internal/eventbus/history/hot_jetstream.go` | MODIFY | `Decode` call gets `aad=nil` |
| `internal/eventbus/audit/plugin_consumer.go` | MODIFY | `Decode` call gets `aad=nil` |
| `internal/eventbus/types.go` | MODIFY | Add host-internal `Sensitive bool` field on `Event` |
| `internal/eventbus/audit/projection.go` | MODIFY | Read `App-Dek-Ref/Version` headers; extend inline `INSERT INTO events_audit` to include `dek_ref, dek_version` |
| `internal/eventbus/audit/projection_test.go` | MODIFY | Integration test asserts SELECT roundtrip of dek_ref/version columns |
| `pkg/plugin/event.go` | MODIFY | Add `Sensitive bool` to `EmitIntent` |
| `internal/plugin/crypto_manifest.go` | MODIFY | Add `LookupEmitSensitivity(manifest, eventType)` helper |
| `internal/plugin/crypto_manifest_test.go` | MODIFY | Helper test |
| `internal/plugin/sensitivity_fence.go` | CREATE | `EnforceSensitivity(manifest, claimed)` |
| `internal/plugin/sensitivity_fence_test.go` | CREATE | Truth-table test |
| `internal/plugin/event_emitter.go` | MODIFY | Wire fence; set `event.Sensitive` |
| `internal/plugin/event_emitter_crypto_test.go` | CREATE | Crypto-path emitter tests |
| `internal/eventbus/config.go` | MODIFY | Add `Crypto.Enabled` flag |
| `internal/bootstrap/wire_crypto.go` | CREATE | Conditional emitter+publisher wiring |
| `test/integration/crypto/emit_test.go` | CREATE | E2E sensitive emit integration test |

Migrations 000013 (`crypto_keys`) and 000014 (`events_audit_dek_columns`) already exist from Phase 2 — no schema work in this phase.

---

## Tasks

### Task 0: Verify clean working copy and current main

Each task commits separately. If the worktree starts with uncommitted changes or is not based on the latest main, the first commit picks up unrelated work.

- [ ] **Step 1: Verify clean working copy.**

Run: `jj --no-pager st`

Expected: `The working copy has no changes.`

If not clean: stop and ask the operator. Do NOT proceed.

- [ ] **Step 2: Verify base on current main.**

Run: `jj --no-pager log -r 'main..@' --no-graph | head -3`

Expected: empty (the worktree is at `main`) OR a list of commits that the operator has explicitly authorized as in-flight Phase 3a work.

If unexpected commits: stop and clarify before continuing.

---

### Task 1: Substrate edits — codec interface, `codec.Key.Version`, `AsCodecKey` companion

This is one mechanical commit that lands all the substrate type changes the rest of the plan depends on. Every existing caller of `Codec.Encode`/`Codec.Decode` gains `aad []byte` (passing `nil` for now); every existing callsite of `dek.Material.AsCodecKey` gains a `version` argument.

**Files:**

- Modify: `internal/eventbus/codec/codec.go`
- Modify: `internal/eventbus/codec/codec_test.go`
- Modify: `internal/eventbus/codec/registry_test.go`
- Modify: `internal/eventbus/publisher.go`
- Modify: `internal/eventbus/publisher_test.go`
- Modify: `internal/eventbus/subscriber.go`
- Modify: `internal/eventbus/history/hot_jetstream.go`
- Modify: `internal/eventbus/audit/plugin_consumer.go`
- Modify: `internal/eventbus/crypto/dek/material.go`
- Modify: `internal/eventbus/crypto/dek/material_test.go`
- Modify: `internal/eventbus/crypto/dek/manager.go`

- [ ] **Step 1: Add `Version` to `codec.Key`.**

In `internal/eventbus/codec/codec.go`, change the `Key` struct:

```go
// Key carries DEK identity and material to a Codec. Identity is
// (ID, Version) — the (KeyID, version) pair used at every other
// substrate boundary. Bytes is the AEAD key material; for IdentityCodec
// callers the zero value (ID=0, Version=0, Bytes=nil) is correct.
type Key struct {
	ID      KeyID
	Version uint32
	Bytes   []byte
}
```

- [ ] **Step 2: Change `Codec.Encode/Decode` interface to accept `aad []byte`.**

Same file:

```go
type Codec interface {
	Name() Name
	// Encode produces the wire bytes for plaintext under key. aad is
	// passed to AEAD codecs as Additional Authenticated Data; IdentityCodec
	// ignores it. Phase 3a's emit path supplies aad via aad.Build(...).
	Encode(ctx context.Context, plaintext []byte, key Key, aad []byte) ([]byte, error)
	// Decode validates and reverses Encode. aad MUST equal the value used
	// at Encode time for AEAD codecs; mismatch surfaces as a generic
	// codec-specific error (no oracle).
	Decode(ctx context.Context, ciphertext []byte, key Key, aad []byte) ([]byte, error)
}
```

- [ ] **Step 3: Update `IdentityCodec.Encode/Decode` signatures.**

Same file, replace the existing methods:

```go
func (IdentityCodec) Encode(_ context.Context, plaintext []byte, _ Key, _ []byte) ([]byte, error) {
	return plaintext, nil
}

func (IdentityCodec) Decode(_ context.Context, ciphertext []byte, _ Key, _ []byte) ([]byte, error) {
	return ciphertext, nil
}
```

- [ ] **Step 4: Update test stubs.**

In `internal/eventbus/codec/registry_test.go:63-72`, the existing `stubCodec` is:

```go
type stubCodec struct{}

func (stubCodec) Name() codec.Name { return codec.Name("test-only-stub") }
func (stubCodec) Encode(_ context.Context, p []byte, _ codec.Key) ([]byte, error) {
	return p, nil
}

func (stubCodec) Decode(_ context.Context, p []byte, _ codec.Key) ([]byte, error) {
	return p, nil
}
```

Keep the struct + Name unchanged. ONLY append `, _ []byte` to each method's parameter list:

```go
func (stubCodec) Encode(_ context.Context, p []byte, _ codec.Key, _ []byte) ([]byte, error) {
	return p, nil
}

func (stubCodec) Decode(_ context.Context, p []byte, _ codec.Key, _ []byte) ([]byte, error) {
	return p, nil
}
```

In `internal/eventbus/publisher_test.go`, around line 59-68 the existing `errCodec` is:

```go
type errCodec struct{ name codec.Name }

func (e errCodec) Name() codec.Name { return e.name }
func (errCodec) Encode(_ context.Context, _ []byte, _ codec.Key) ([]byte, error) {
	return nil, errors.New("encode boom")
}
func (errCodec) Decode(_ context.Context, _ []byte, _ codec.Key) ([]byte, error) {
	return nil, errors.New("decode boom")
}
```

Keep the struct + `Name()` unchanged. ONLY append `, _ []byte` to each method's parameter list:

```go
func (errCodec) Encode(_ context.Context, _ []byte, _ codec.Key, _ []byte) ([]byte, error) {
	return nil, errors.New("encode boom")
}
func (errCodec) Decode(_ context.Context, _ []byte, _ codec.Key, _ []byte) ([]byte, error) {
	return nil, errors.New("decode boom")
}
```

- [ ] **Step 5: Update production callers to pass `aad=nil`.**

Each callsite below has the EXACT current form on the left and the new form on the right. Use Edit's `old_string`/`new_string` literally.

`internal/eventbus/publisher.go:200`:

```go
// Was:
encoded, err := c.Encode(ctx, plainBytes, key)
// Becomes:
encoded, err := c.Encode(ctx, plainBytes, key, nil)
```

`internal/eventbus/subscriber.go:412`:

```go
// Was:
plain, err := c.Decode(ctx, msg.Data(), key)
// Becomes:
plain, err := c.Decode(ctx, msg.Data(), key, nil)
```

`internal/eventbus/history/hot_jetstream.go:379`:

```go
// Was:
plain, err := c.Decode(ctx, msg.Data(), key)
// Becomes:
plain, err := c.Decode(ctx, msg.Data(), key, nil)
```

`internal/eventbus/audit/plugin_consumer.go:354` (note: this site uses `context.Background()` not `ctx`):

```go
// Was:
plain, err := c.Decode(context.Background(), data, codec.Key{})
// Becomes:
plain, err := c.Decode(context.Background(), data, codec.Key{}, nil)
```

- [ ] **Step 6: Update test callers in `codec_test.go`.**

Run: `rg -n "\.Encode\(|\.Decode\(" internal/eventbus/codec/codec_test.go`

Expected: 4 callsites around lines 19, 22, 29, 31, each using `codec.NoKey` (the project sentinel for keyless codec ops, declared at `codec.go:46`). Each gets a trailing `, nil)` argument:

```go
// Was:
out, err := c.Encode(context.Background(), []byte("hi"), codec.NoKey)
back, err := c.Decode(context.Background(), out, codec.NoKey)
// Becomes:
out, err := c.Encode(context.Background(), []byte("hi"), codec.NoKey, nil)
back, err := c.Decode(context.Background(), out, codec.NoKey, nil)
```

- [ ] **Step 7: Update `dek.Material.AsCodecKey` companion signature.**

In `internal/eventbus/crypto/dek/material.go`, around line 43:

```go
// AsCodecKey returns a codec.Key that copies the unwrapped DEK bytes
// alongside the supplied (id, version) DEK identity. The copy ensures
// callers cannot mutate Material's internal buffer through Key.Bytes.
func (m *Material) AsCodecKey(id codec.KeyID, version uint32) codec.Key {
	out := make([]byte, len(m.bytes))
	copy(out, m.bytes)
	return codec.Key{ID: id, Version: version, Bytes: out}
}
```

- [ ] **Step 8: Update the three `manager.go` callers of `AsCodecKey`.**

In `internal/eventbus/crypto/dek/manager.go`:

- Line ~139 (mint path): `return material.AsCodecKey(keyID, 1), nil` (DEK was just minted at v1).
- Line ~148 (cache-hit path): `return material.AsCodecKey(keyID, version), nil` (the cache lookup used `version`).
- Line ~201 (DB-unwrap path): `return material.AsCodecKey(codec.KeyID(r.ID), r.Version), nil` (use the row's version).

Each line currently passes only `keyID`; add the second argument as shown.

- [ ] **Step 9: Update `material_test.go`.**

In `internal/eventbus/crypto/dek/material_test.go`, find every `m.AsCodecKey(<id>)` and change to `m.AsCodecKey(<id>, 1)` (any positive version is fine; tests don't depend on the specific value).

- [ ] **Step 10: Build the entire repo.**

Run: `task lint:go`

Expected: 0 issues. If anything fails, it's an unenumerated callsite — `rg "AsCodecKey|\.Encode\(.*Key|\.Decode\(.*Key" --type go` and patch.

- [ ] **Step 11: Run unit tests.**

Run: `task test`

Expected: green.

- [ ] **Step 12: Commit.**

`jj describe -m "feat(eventbus): substrate edits — Codec aad param + Key.Version + AsCodecKey companion (holomush-ojw1.1.2)"`, then `jj new`.

---

### Task 2: Implement `xchacha20poly1305-v1` codec

**Files:**

- Create: `internal/eventbus/codec/xchacha20poly1305.go`
- Create: `internal/eventbus/codec/xchacha20poly1305_test.go`
- Modify: `internal/eventbus/codec/codec.go` (uncomment `NameXChaCha20v1`)
- Modify: `internal/eventbus/codec/registry.go` (register the new codec)

- [ ] **Step 1: Write the failing tests.**

Create `internal/eventbus/codec/xchacha20poly1305_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package codec_test

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/chacha20poly1305"

	"github.com/holomush/holomush/internal/eventbus/codec"
)

func newXChachaKey(t *testing.T) codec.Key {
	t.Helper()
	km := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(km)
	require.NoError(t, err)
	return codec.Key{ID: 1, Version: 1, Bytes: km}
}

func TestXChaCha20Poly1305RoundTripsPlaintext(t *testing.T) {
	c := codec.NewXChaCha20Poly1305v1()
	key := newXChachaKey(t)
	plaintext := []byte("hello, secret world")
	aad := []byte("test-aad")

	ct, err := c.Encode(context.Background(), plaintext, key, aad)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ct, "ciphertext must differ from plaintext")

	got, err := c.Decode(context.Background(), ct, key, aad)
	require.NoError(t, err)
	assert.Equal(t, plaintext, got)
}

func TestXChaCha20Poly1305DetectsCiphertextTamper(t *testing.T) {
	c := codec.NewXChaCha20Poly1305v1()
	key := newXChachaKey(t)
	aad := []byte("test-aad")

	ct, err := c.Encode(context.Background(), []byte("hello"), key, aad)
	require.NoError(t, err)
	ct[len(ct)-1] ^= 0x01 // flip a tag byte

	_, err = c.Decode(context.Background(), ct, key, aad)
	require.Error(t, err, "tampered ciphertext must not decrypt")
}

func TestXChaCha20Poly1305DetectsAADTamper(t *testing.T) {
	c := codec.NewXChaCha20Poly1305v1()
	key := newXChachaKey(t)

	ct, err := c.Encode(context.Background(), []byte("hello"), key, []byte("aad-A"))
	require.NoError(t, err)

	_, err = c.Decode(context.Background(), ct, key, []byte("aad-B"))
	require.Error(t, err, "AAD mismatch must fail decryption")
}

func TestXChaCha20Poly1305AcceptsNilAAD(t *testing.T) {
	c := codec.NewXChaCha20Poly1305v1()
	key := newXChachaKey(t)

	ct, err := c.Encode(context.Background(), []byte("hello"), key, nil)
	require.NoError(t, err)

	got, err := c.Decode(context.Background(), ct, key, nil)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), got)
}

func TestXChaCha20Poly1305NameIsXChaCha20Poly1305v1(t *testing.T) {
	c := codec.NewXChaCha20Poly1305v1()
	assert.Equal(t, codec.NameXChaCha20v1, c.Name())
}

func TestXChaCha20Poly1305RejectsWrongLengthKey(t *testing.T) {
	c := codec.NewXChaCha20Poly1305v1()
	badKey := codec.Key{ID: 1, Version: 1, Bytes: []byte("too-short")}
	_, err := c.Encode(context.Background(), []byte("hello"), badKey, nil)
	require.Error(t, err)
}

func TestXChaCha20Poly1305DecodeRejectsShortCiphertext(t *testing.T) {
	c := codec.NewXChaCha20Poly1305v1()
	key := newXChachaKey(t)
	_, err := c.Decode(context.Background(), []byte("short"), key, nil)
	require.Error(t, err, "ciphertext shorter than nonce+tag must error")
}

func TestXChaCha20Poly1305RegisteredInRegistry(t *testing.T) {
	c, err := codec.Resolve(codec.NameXChaCha20v1)
	require.NoError(t, err)
	assert.Equal(t, codec.NameXChaCha20v1, c.Name())
}
```

- [ ] **Step 2: Run to confirm FAIL.**

Run: `task test -- -run TestXChaCha20Poly1305 ./internal/eventbus/codec/`

Expected: FAIL — `undefined: codec.NewXChaCha20Poly1305v1` and `codec.NameXChaCha20v1`.

- [ ] **Step 3: Add the codec name constant.**

In `internal/eventbus/codec/codec.go`, find the existing `const ( … )` block defining `NameIdentity` (around line 21) and add:

```go
const (
	NameIdentity    Name = "identity"
	NameXChaCha20v1 Name = "xchacha20poly1305-v1"
)
```

(Replace the previously-commented-out line.)

- [ ] **Step 4: Implement the codec.**

Create `internal/eventbus/codec/xchacha20poly1305.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package codec — xchacha20poly1305-v1 implementation.
//
// Wire layout: nonce || ciphertext || tag
//   nonce      : 24 bytes (XChaCha20-Poly1305 NonceSizeX)
//   ciphertext : len(plaintext) bytes
//   tag        : 16 bytes (Poly1305 tag, appended by Seal)
//
// AAD is supplied via the codec interface's `aad []byte` parameter
// (Phase 3a substrate edit). Phase 3a's emit path calls
// internal/eventbus/crypto/aad.Build to construct AAD.
package codec

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// XChaCha20Poly1305v1 implements Codec for sensitive payloads.
type XChaCha20Poly1305v1 struct{}

// NewXChaCha20Poly1305v1 returns a stateless codec instance.
func NewXChaCha20Poly1305v1() *XChaCha20Poly1305v1 { return &XChaCha20Poly1305v1{} }

// Name returns NameXChaCha20v1.
func (*XChaCha20Poly1305v1) Name() Name { return NameXChaCha20v1 }

// Encode produces nonce || ciphertext || tag using key.Bytes as the
// AEAD key and aad as additional authenticated data. Errors on
// wrong-length keys or RNG failure.
func (*XChaCha20Poly1305v1) Encode(_ context.Context, plaintext []byte, key Key, aad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key.Bytes)
	if err != nil {
		return nil, fmt.Errorf("xchacha20poly1305-v1: new aead: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("xchacha20poly1305-v1: rng: %w", err)
	}
	out := make([]byte, 0, len(nonce)+len(plaintext)+aead.Overhead())
	out = append(out, nonce...)
	out = aead.Seal(out, nonce, plaintext, aad)
	return out, nil
}

// Decode validates and decrypts. AAD MUST equal the value supplied at
// Encode; any mismatch surfaces as a generic error (no oracle).
func (*XChaCha20Poly1305v1) Decode(_ context.Context, ciphertext []byte, key Key, aad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key.Bytes)
	if err != nil {
		return nil, fmt.Errorf("xchacha20poly1305-v1: new aead: %w", err)
	}
	if len(ciphertext) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("xchacha20poly1305-v1: ciphertext too short")
	}
	nonce, sealed := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	pt, err := aead.Open(nil, nonce, sealed, aad)
	if err != nil {
		return nil, fmt.Errorf("xchacha20poly1305-v1: open: %w", err)
	}
	return pt, nil
}
```

- [ ] **Step 5: Register the codec in the registry.**

`internal/eventbus/codec/registry.go:14-19` is a package-level map literal — no `init()`, no exported `Register` (only `RegisterForTest` for tests). Extend the literal:

```go
// Was (registry.go:14-19):
var (
	regMu    sync.RWMutex
	registry = map[Name]Codec{
		NameIdentity: IdentityCodec{},
	}
)

// Becomes:
var (
	regMu    sync.RWMutex
	registry = map[Name]Codec{
		NameIdentity:    IdentityCodec{},
		NameXChaCha20v1: &XChaCha20Poly1305v1{},
	}
)
```

The `&` is required because `XChaCha20Poly1305v1`'s methods are pointer-receiver per Task 2 step 4. Verify by checking `Resolve(NameXChaCha20v1)` in the new test in step 1 returns the codec.

- [ ] **Step 5b: Update the registry meta-test `declaredNames`.**

`internal/eventbus/codec/registry_test.go:19-22` declares `declaredNames` — a list every const in `codec.go` MUST appear in. The meta-test `TestRegistryHasNoExtraEntriesNotDeclared` (line 34) cross-checks `declaredNames` against `registry`. Adding `NameXChaCha20v1` to the registry without adding it here makes the meta-test fail.

```go
// Was (registry_test.go:19-22):
var declaredNames = []codec.Name{
	codec.NameIdentity,
	// Add new constants here when introduced.
}

// Becomes:
var declaredNames = []codec.Name{
	codec.NameIdentity,
	codec.NameXChaCha20v1,
	// Add new constants here when introduced.
}
```

- [ ] **Step 6: Run all codec tests.**

Run: `task test -- ./internal/eventbus/codec/`

Expected: PASS for all 8 new tests + any pre-existing tests still green.

- [ ] **Step 7: Lint.**

Run: `task lint:go`

Expected: 0 issues.

- [ ] **Step 8: Commit.**

`jj describe -m "feat(codec): implement xchacha20poly1305-v1 AEAD codec (holomush-ojw1.1.2)"`, then `jj new`.

---

### Task 3: Add `App-Dek-Ref`/`App-Dek-Version` headers + audit projection columns

**Files:**

- Modify: `internal/eventbus/publisher.go` (add header constants + add to reserved set)
- Modify: `internal/eventbus/publisher_test.go` (constants test)
- Modify: `internal/eventbus/audit/projection.go` (read headers in persist; extend inline INSERT)
- Modify: `internal/eventbus/audit/projection_test.go` (integration test for dek columns)

The `events_audit.dek_ref` and `dek_version` columns already exist (migration 000014 from Phase 2). The audit projection's persist path is an inline raw SQL INSERT at `internal/eventbus/audit/projection.go:241-258` — no row struct exists. This task adds two header reads + extends the column list and `$N` placeholders.

- [ ] **Step 1: Header constants test.**

Append to `internal/eventbus/publisher_test.go`:

```go
func TestHeaderConstantsIncludeDekRefAndDekVersion(t *testing.T) {
	if eventbus.HeaderDekRef != "App-Dek-Ref" {
		t.Fatalf("HeaderDekRef = %q, want %q", eventbus.HeaderDekRef, "App-Dek-Ref")
	}
	if eventbus.HeaderDekVersion != "App-Dek-Version" {
		t.Fatalf("HeaderDekVersion = %q, want %q", eventbus.HeaderDekVersion, "App-Dek-Version")
	}
}
```

(The `eventbus` package import alias may be different — match what the file already uses.)

Run: `task test -- -run TestHeaderConstantsIncludeDekRef ./internal/eventbus/`. Expected: FAIL.

- [ ] **Step 2: Add the constants.**

In `internal/eventbus/publisher.go`, inside the existing `const ( … )` block that defines `HeaderCodec`:

```go
// HeaderDekRef carries the crypto_keys.id (decimal string) for events
// encrypted with a non-identity codec. Empty for codec=identity. Maps
// 1:1 to events_audit.dek_ref (BIGINT) via the audit projection.
HeaderDekRef = "App-Dek-Ref"

// HeaderDekVersion carries the per-context DEK version (decimal string).
// Empty for codec=identity. Maps to events_audit.dek_version (INTEGER).
HeaderDekVersion = "App-Dek-Version"
```

- [ ] **Step 3: Verify constants test passes.**

Run: `task test -- -run TestHeaderConstantsIncludeDekRef ./internal/eventbus/`. Expected: PASS.

- [ ] **Step 4: Add the headers to the system-reserved set.**

Find the `systemReservedHeaders` (or equivalent) map in `publisher.go` (look near where `HeaderCodec` is registered as reserved — around line 247-253). Add:

```go
HeaderDekRef:     {},
HeaderDekVersion: {},
```

This prevents callers from stamping these headers via `event.Headers` (per the existing reserved-keys rule); they're publisher-owned.

- [ ] **Step 5: Read the existing INSERT path.**

Read `internal/eventbus/audit/projection.go:170-262` to see the persist function. Two relevant pieces:

- Line 174 reads `App-Codec` via `h.Get(headerCodec)` (a local `headerCodec = "App-Codec"` const at line 25).
- Lines 241-258 are the inline `INSERT INTO events_audit (...)` with 11 columns and `$1..$11` placeholders.

This task adds two more header reads above the INSERT, and extends the column list + placeholders.

- [ ] **Step 6: Add the two header parses + extend the INSERT.**

In `internal/eventbus/audit/projection.go`, immediately after the existing `codec := h.Get(headerCodec)` block (around line 174-176, after the `AUDIT_MISSING_HEADER` error path), insert:

```go
// Phase 3a: parse optional App-Dek-Ref and App-Dek-Version headers.
// Both are absent for codec=identity rows; nil pointers below write
// SQL NULL via pgx nullable handling.
var dekRef *int64
if v := h.Get(eventbus.HeaderDekRef); v != "" {
	parsed, parseErr := strconv.ParseInt(v, 10, 64)
	if parseErr != nil {
		return oops.Code("AUDIT_DEK_REF_PARSE_FAILED").
			With("header", eventbus.HeaderDekRef).
			With("value", v).
			Wrap(parseErr)
	}
	dekRef = &parsed
}
var dekVer *int32
if v := h.Get(eventbus.HeaderDekVersion); v != "" {
	parsed, parseErr := strconv.ParseInt(v, 10, 32)
	if parseErr != nil {
		return oops.Code("AUDIT_DEK_VERSION_PARSE_FAILED").
			With("header", eventbus.HeaderDekVersion).
			With("value", v).
			Wrap(parseErr)
	}
	v32 := int32(parsed)
	dekVer = &v32
}
```

(Add `"strconv"` to the imports if not already present; `eventbus` is already imported per the existing `eventbus.HeaderXXX` references above the function.)

Then change the INSERT block at lines 241-258. Current form:

```go
_, err = p.pool.Exec(ctx, `
    INSERT INTO events_audit (
        id, subject, type, timestamp, actor_kind, actor_id,
        payload, schema_ver, codec, js_seq, rendering
    ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
    ON CONFLICT (id) DO NOTHING`,
    idBytes,
    msg.Subject(),
    eventType,
    meta.Timestamp,
    actorKind,
    actorID,
    msg.Data(),
    ver,
    codec,
    meta.Sequence.Stream,
    renderingJSON,
)
```

New form:

```go
_, err = p.pool.Exec(ctx, `
    INSERT INTO events_audit (
        id, subject, type, timestamp, actor_kind, actor_id,
        payload, schema_ver, codec, js_seq, rendering,
        dek_ref, dek_version
    ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
    ON CONFLICT (id) DO NOTHING`,
    idBytes,
    msg.Subject(),
    eventType,
    meta.Timestamp,
    actorKind,
    actorID,
    msg.Data(),
    ver,
    codec,
    meta.Sequence.Stream,
    renderingJSON,
    dekRef,
    dekVer,
)
```

`pgx` writes SQL NULL when the `*int64` / `*int32` pointer is nil.

- [ ] **Step 7: Add an integration test asserting the column round-trip.**

Append to `internal/eventbus/audit/projection_test.go` (this file is `//go:build integration`-tagged — verify by reading the build directive on line 1; the new test belongs here, alongside the other integration tests):

```go
func TestPersistWritesDekColumnsFromHeaders(t *testing.T) {
	t.Parallel()
	pg := testutil.StartPostgres(t)
	pool := testutil.NewPGPool(t, pg.ConnString())
	defer pool.Close()
	require.NoError(t, store.NewMigrator(pg.ConnString()).Up())

	proj := audit.NewProjection(pool /* match existing projection_test fixtures */)

	msgID := core.NewULID()
	msg := newPersistMsgFixture(t, persistMsgOpts{
		ID:    msgID,
		Codec: "xchacha20poly1305-v1",
		Headers: map[string]string{
			eventbus.HeaderDekRef:     "42",
			eventbus.HeaderDekVersion: "3",
		},
		Payload: []byte("ciphertext-bytes"),
	})

	require.NoError(t, proj.Persist(context.Background(), msg))

	var dekRef sql.NullInt64
	var dekVer sql.NullInt32
	err := pool.QueryRow(context.Background(),
		`SELECT dek_ref, dek_version FROM events_audit WHERE id = $1`,
		msgID.Bytes()).Scan(&dekRef, &dekVer)
	require.NoError(t, err)
	require.True(t, dekRef.Valid)
	assert.Equal(t, int64(42), dekRef.Int64)
	require.True(t, dekVer.Valid)
	assert.Equal(t, int32(3), dekVer.Int32)
}

func TestPersistWritesNullDekColumnsForIdentityCodec(t *testing.T) {
	t.Parallel()
	pg := testutil.StartPostgres(t)
	pool := testutil.NewPGPool(t, pg.ConnString())
	defer pool.Close()
	require.NoError(t, store.NewMigrator(pg.ConnString()).Up())

	proj := audit.NewProjection(pool)

	msgID := core.NewULID()
	msg := newPersistMsgFixture(t, persistMsgOpts{
		ID:      msgID,
		Codec:   "identity",
		Payload: []byte("plaintext"),
	})

	require.NoError(t, proj.Persist(context.Background(), msg))

	var dekRef sql.NullInt64
	var dekVer sql.NullInt32
	err := pool.QueryRow(context.Background(),
		`SELECT dek_ref, dek_version FROM events_audit WHERE id = $1`,
		msgID.Bytes()).Scan(&dekRef, &dekVer)
	require.NoError(t, err)
	assert.False(t, dekRef.Valid, "identity codec must not populate dek_ref")
	assert.False(t, dekVer.Valid, "identity codec must not populate dek_version")
}
```

The fixture builder `newPersistMsgFixture` and its options struct should follow the patterns already in `projection_test.go`. Inspect the file via `rg "func.*persistMsg|persistTest|projectionTest" internal/eventbus/audit/projection_test.go` and reuse whatever fixture builds a `*nats.Msg`-equivalent with headers. If no equivalent exists, write the minimal version (~20 lines) that builds a `jetstream.Msg` with the supplied headers — the fixture's responsibility is just "produce a message the projection can persist."

- [ ] **Step 8: Run integration tests.**

Run: `task test:int -- -run "TestPersistWritesDekColumns|TestPersistWritesNullDekColumns" ./internal/eventbus/audit/...`

Expected: PASS for both new tests.

- [ ] **Step 9: Run the full audit suite for regressions.**

Run: `task test:int -- ./internal/eventbus/audit/...`

Expected: green.

- [ ] **Step 10: Commit.**

`jj describe -m "feat(audit): App-Dek-Ref/Version headers + events_audit columns wired (holomush-ojw1.1.7)"`, then `jj new`.

---

### Task 4: Add `Sensitive bool` to `EmitIntent` + `LookupEmitSensitivity` helper

**Files:**

- Modify: `pkg/plugin/event.go` (add `Sensitive` field)
- Modify: `internal/plugin/crypto_manifest.go` (add helper)
- Modify: `internal/plugin/crypto_manifest_test.go` (helper test)

- [ ] **Step 1: Test for `LookupEmitSensitivity`.**

Append to `internal/plugin/crypto_manifest_test.go`:

```go
func TestLookupEmitSensitivityReturnsDeclaredValueForListedEventType(t *testing.T) {
	m := &plugins.Manifest{
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "scene.whisper", Sensitivity: plugins.SensitivityAlways},
				{EventType: "scene.pose", Sensitivity: plugins.SensitivityNever},
			},
		},
	}
	got := plugins.LookupEmitSensitivity(m, "scene.whisper")
	assert.Equal(t, plugins.SensitivityAlways, got)
}

func TestLookupEmitSensitivityDefaultsToNeverForUnlistedEventType(t *testing.T) {
	m := &plugins.Manifest{
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "scene.whisper", Sensitivity: plugins.SensitivityAlways},
			},
		},
	}
	got := plugins.LookupEmitSensitivity(m, "scene.pose")
	assert.Equal(t, plugins.SensitivityNever, got)
}

func TestLookupEmitSensitivityHandlesNilManifest(t *testing.T) {
	got := plugins.LookupEmitSensitivity(nil, "anything")
	assert.Equal(t, plugins.SensitivityNever, got)
}

func TestLookupEmitSensitivityHandlesEmptyEmits(t *testing.T) {
	m := &plugins.Manifest{Crypto: &plugins.CryptoSection{}}
	got := plugins.LookupEmitSensitivity(m, "anything")
	assert.Equal(t, plugins.SensitivityNever, got)
}

func TestLookupEmitSensitivityHandlesNilCryptoBlock(t *testing.T) {
	m := &plugins.Manifest{Crypto: nil} // crypto: block omitted from YAML
	got := plugins.LookupEmitSensitivity(m, "anything")
	assert.Equal(t, plugins.SensitivityNever, got)
}
```

(The test file imports `plugins "github.com/holomush/holomush/internal/plugin"` per existing convention — match the existing import alias in this file.)

- [ ] **Step 2: Run to confirm FAIL.**

Run: `task test -- -run TestLookupEmitSensitivity ./internal/plugin/`. Expected: FAIL — `undefined: plugins.LookupEmitSensitivity`.

- [ ] **Step 3: Implement the helper.**

Append to `internal/plugin/crypto_manifest.go`:

```go
// LookupEmitSensitivity returns the manifest-declared Sensitivity for
// (manifest, eventType). Returns SensitivityNever when:
//   - manifest is nil
//   - manifest.Crypto is nil (the crypto: block is optional in YAML;
//     plugins that don't use crypto leave it absent)
//   - manifest.Crypto.Emits is empty
//   - eventType is not listed in manifest.Crypto.Emits
//
// The caller is responsible for any plugin-name lookup; this helper
// operates on an already-resolved *Manifest.
func LookupEmitSensitivity(manifest *Manifest, eventType string) Sensitivity {
	if manifest == nil || manifest.Crypto == nil {
		return SensitivityNever
	}
	for _, emit := range manifest.Crypto.Emits {
		if emit.EventType == eventType {
			return emit.Sensitivity
		}
	}
	return SensitivityNever
}
```

- [ ] **Step 4: Verify pass.**

Run: `task test -- -run TestLookupEmitSensitivity ./internal/plugin/`. Expected: PASS.

- [ ] **Step 5: Add `Sensitive bool` to `EmitIntent`.**

In `pkg/plugin/event.go` around line 111:

```go
// EmitIntent is a host-side request to emit an event on behalf of a plugin.
// ... (existing godoc) ...
type EmitIntent struct {
	Subject string
	Type    EventType
	Payload string // JSON string

	// Sensitive declares per-event sensitivity at emit time.
	//
	// Phase 3a runtime semantics (host-side fence):
	//   - manifest sensitivity=never:  Sensitive=true rejected (INV-6).
	//   - manifest sensitivity=may:    field decides (false → plaintext, true → encrypted).
	//   - manifest sensitivity=always: Sensitive=false rejected (INV-7).
	//
	// Default false. Plugins that do not emit sensitive events leave
	// this zero.
	Sensitive bool
}
```

- [ ] **Step 6: Build.**

Run: `task lint:go`

Expected: 0 issues. (Adding a field is additive; no callers need to be updated.)

- [ ] **Step 7: Run unit tests.**

Run: `task test -- ./pkg/plugin/ ./internal/plugin/`. Expected: green.

- [ ] **Step 8: Commit.**

`jj describe -m "feat(plugin): Sensitive bool on EmitIntent + LookupEmitSensitivity helper (holomush-ojw1.1.3)"`, then `jj new`.

---

### Task 5: Sensitivity fence — `EnforceSensitivity`

**Files:**

- Create: `internal/plugin/sensitivity_fence.go`
- Create: `internal/plugin/sensitivity_fence_test.go`

The fence is the host-side ground-truth check: takes manifest sensitivity + plugin's claim, returns effective sensitivity OR an error.

- [ ] **Step 1: Write the truth-table test.**

Create `internal/plugin/sensitivity_fence_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestEnforceSensitivity(t *testing.T) {
	tests := []struct {
		name     string
		manifest plugins.Sensitivity
		claimed  bool
		want     plugins.Sensitivity
		wantErr  string
	}{
		{"never + claim=false → never", plugins.SensitivityNever, false, plugins.SensitivityNever, ""},
		{"never + claim=true → INV-6 reject", plugins.SensitivityNever, true, "", "EVENT_SENSITIVITY_NOT_DECLARED"},
		{"may + claim=false → never (plaintext)", plugins.SensitivityMay, false, plugins.SensitivityNever, ""},
		{"may + claim=true → always (encrypt)", plugins.SensitivityMay, true, plugins.SensitivityAlways, ""},
		{"always + claim=false → INV-7 reject", plugins.SensitivityAlways, false, "", "EVENT_SENSITIVITY_REQUIRED"},
		{"always + claim=true → always", plugins.SensitivityAlways, true, plugins.SensitivityAlways, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := plugins.EnforceSensitivity(tt.manifest, tt.claimed)
			if tt.wantErr != "" {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEnforceSensitivityRejectsUnknownManifestValue(t *testing.T) {
	_, err := plugins.EnforceSensitivity(plugins.Sensitivity("garbage"), false)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SENSITIVITY_INVALID")
}
```

- [ ] **Step 2: Run to confirm FAIL.**

Run: `task test -- -run TestEnforceSensitivity ./internal/plugin/`. Expected: FAIL.

- [ ] **Step 3: Implement.**

Create `internal/plugin/sensitivity_fence.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import "github.com/samber/oops"

// EnforceSensitivity is the host-side ground-truth check that closes
// INV-6 (over-claim reject) and INV-7 (under-claim reject) at emit
// time. Given the manifest-declared sensitivity for an event type
// and the plugin's per-event Sensitive flag, returns the effective
// sensitivity the host MUST use, or a typed error when the
// combination is forbidden.
//
// Truth table:
//
//   manifest=never  + claim=false → effective=never
//   manifest=never  + claim=true  → REJECT (INV-6, EVENT_SENSITIVITY_NOT_DECLARED)
//   manifest=may    + claim=false → effective=never (plaintext)
//   manifest=may    + claim=true  → effective=always (encrypt)
//   manifest=always + claim=false → REJECT (INV-7, EVENT_SENSITIVITY_REQUIRED)
//   manifest=always + claim=true  → effective=always
func EnforceSensitivity(manifest Sensitivity, claimed bool) (Sensitivity, error) {
	switch manifest {
	case SensitivityNever:
		if claimed {
			return "", oops.Code("EVENT_SENSITIVITY_NOT_DECLARED").
				With("manifest", string(manifest)).
				Errorf("plugin claimed Sensitive=true on an event the manifest declares plaintext (INV-6)")
		}
		return SensitivityNever, nil
	case SensitivityMay:
		if claimed {
			return SensitivityAlways, nil
		}
		return SensitivityNever, nil
	case SensitivityAlways:
		if !claimed {
			return "", oops.Code("EVENT_SENSITIVITY_REQUIRED").
				With("manifest", string(manifest)).
				Errorf("plugin claimed Sensitive=false on an event the manifest declares always sensitive (INV-7)")
		}
		return SensitivityAlways, nil
	}
	return "", oops.Code("SENSITIVITY_INVALID").
		With("manifest", string(manifest)).
		Errorf("manifest sensitivity is not a known value")
}
```

- [ ] **Step 4: Verify pass.**

Run: `task test -- -run TestEnforceSensitivity ./internal/plugin/`. Expected: PASS.

- [ ] **Step 5: Lint.**

Run: `task lint:go`. Expected: 0 issues.

- [ ] **Step 6: Commit.**

`jj describe -m "feat(plugin): emit-time sensitivity fence (INV-6 + INV-7) (holomush-ojw1.1.4)"`, then `jj new`.

---

### Task 6: Wire fence into emitter; set `event.Sensitive`

**Files:**

- Modify: `internal/eventbus/types.go` (add `Sensitive` field to `Event`)
- Modify: `internal/plugin/event_emitter.go`
- Create: `internal/plugin/event_emitter_crypto_test.go`

The emitter's job: look up manifest sensitivity, run the fence with `intent.Sensitive`, set `event.Sensitive` accordingly. Encryption itself stays in the publisher (Task 7).

- [ ] **Step 1: Add the host-internal `Sensitive` field to `eventbus.Event`.**

In `internal/eventbus/types.go` around line 92:

```go
type Event struct {
	ID        ulid.ULID
	Seq       uint64 // ... existing comment ...
	Subject   Subject
	Type      Type
	Timestamp time.Time
	Actor     Actor
	Payload   []byte // codec.Encode output (ciphertext if encryption is on)

	// Sensitive is a host-internal flag set by the emitter when manifest
	// sensitivity + plugin claim resolve to an encrypted publish. The
	// publisher reads this to choose between the existing identity path
	// and the Phase 3a sensitivity-aware crypto path. NEVER serialized
	// to the wire; never persisted; cold-tier reads return Sensitive=false
	// (the row's codec column is the source of truth on read).
	Sensitive bool

	Rendering *RenderingMetadata
	Headers   map[string]string
}
```

- [ ] **Step 2: Wire the fence into `PluginEventEmitter.Emit`.**

The emitter currently builds `eventbus.Event` at line 174 of `internal/plugin/event_emitter.go`. Add the sensitivity resolution between the manifest gate (line 137) and the `eventbus.Event` construction.

In `internal/plugin/event_emitter.go`, modify `Emit` to add (after line 137, the `EMIT_ACTOR_KIND_NOT_CLAIMABLE` block, before `if e.publisher == nil`):

```go
// Phase 3a: resolve manifest sensitivity + run the host-side fence.
// Result is stamped on event.Sensitive; the publisher acts on it.
manifestSensitivity := LookupEmitSensitivity(manifest, string(intent.Type))
effective, err := EnforceSensitivity(manifestSensitivity, intent.Sensitive)
if err != nil {
	return oops.With("plugin", pluginName).
		With("subject", subjectRaw).
		With("event_type", string(intent.Type)).
		Wrap(err)
}
sensitive := effective == SensitivityAlways
```

Then at the `eventbus.Event{...}` literal (around line 174), add `Sensitive: sensitive`:

```go
event := eventbus.Event{
	ID:        core.NewULID(),
	Subject:   sub,
	Type:      typ,
	Timestamp: time.Now().UTC(),
	Actor:     coreActorToEventbusActor(actor),
	Payload:   payload,
	Sensitive: sensitive,
}
```

- [ ] **Step 3: Write emitter tests.**

Create `internal/plugin/event_emitter_crypto_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// recordingPublisher captures the eventbus.Event passed to Publish so
// tests can assert on its host-internal fields (Sensitive, etc.).
type recordingPublisher struct{ events []eventbus.Event }

func (r *recordingPublisher) Publish(_ context.Context, e eventbus.Event) error {
	r.events = append(r.events, e)
	return nil
}

func newCryptoTestEmitter(t *testing.T, pub eventbus.Publisher, manifest *plugins.Manifest) *plugins.PluginEventEmitter {
	t.Helper()
	lookup := func(name string) *plugins.Manifest {
		if name == "test-plugin" {
			return manifest
		}
		return nil
	}
	// ActorResolver returns core.Actor — Actor.ID is a string per
	// internal/core/event.go:170. ActorPlugin lives in package core
	// (internal/core/event.go:148).
	resolve := func(_ context.Context, _ string) (core.Actor, error) {
		return core.Actor{Kind: core.ActorPlugin, ID: "test-plugin"}, nil
	}
	return plugins.NewPluginEventEmitter(pub, lookup, resolve)
}

func TestEmitterStampsSensitiveTrueForManifestMayPlusClaimTrue(t *testing.T) {
	pub := &recordingPublisher{}
	manifest := newSensitiveTestManifest("test-plugin", "scene", []plugins.CryptoEmit{
		{EventType: "test-plugin:whisper", Sensitivity: plugins.SensitivityMay},
	})
	emitter := newCryptoTestEmitter(t, pub, manifest)

	intent := pluginsdk.EmitIntent{
		Subject:   "scene:01HXXXTESTSCENE000000000",
		Type:      pluginsdk.EventType("test-plugin:whisper"),
		Payload:   `{"text":"hi"}`,
		Sensitive: true,
	}
	require.NoError(t, emitter.Emit(context.Background(), "test-plugin", intent))

	require.Len(t, pub.events, 1)
	assert.True(t, pub.events[0].Sensitive, "manifest=may + claim=true must set event.Sensitive")
}

func TestEmitterStampsSensitiveFalseForManifestMayPlusClaimFalse(t *testing.T) {
	pub := &recordingPublisher{}
	manifest := newSensitiveTestManifest("test-plugin", "scene", []plugins.CryptoEmit{
		{EventType: "test-plugin:whisper", Sensitivity: plugins.SensitivityMay},
	})
	emitter := newCryptoTestEmitter(t, pub, manifest)

	intent := pluginsdk.EmitIntent{
		Subject:   "scene:01HXXXTESTSCENE000000000",
		Type:      pluginsdk.EventType("test-plugin:whisper"),
		Payload:   `{"text":"hi"}`,
		Sensitive: false,
	}
	require.NoError(t, emitter.Emit(context.Background(), "test-plugin", intent))

	require.Len(t, pub.events, 1)
	assert.False(t, pub.events[0].Sensitive)
}

func TestEmitterRejectsClaimTrueOnManifestNeverEvent(t *testing.T) {
	pub := &recordingPublisher{}
	manifest := newSensitiveTestManifest("test-plugin", "scene", []plugins.CryptoEmit{
		{EventType: "test-plugin:pose", Sensitivity: plugins.SensitivityNever},
	})
	emitter := newCryptoTestEmitter(t, pub, manifest)

	intent := pluginsdk.EmitIntent{
		Subject:   "scene:01HXXXTESTSCENE000000000",
		Type:      pluginsdk.EventType("test-plugin:pose"),
		Payload:   `{}`,
		Sensitive: true, // INV-6 over-claim
	}
	err := emitter.Emit(context.Background(), "test-plugin", intent)
	require.Error(t, err)
	assert.Empty(t, pub.events, "rejected emit must not publish")
}

func TestEmitterRejectsClaimFalseOnManifestAlwaysEvent(t *testing.T) {
	pub := &recordingPublisher{}
	manifest := newSensitiveTestManifest("test-plugin", "scene", []plugins.CryptoEmit{
		{EventType: "test-plugin:secret", Sensitivity: plugins.SensitivityAlways},
	})
	emitter := newCryptoTestEmitter(t, pub, manifest)

	intent := pluginsdk.EmitIntent{
		Subject:   "scene:01HXXXTESTSCENE000000000",
		Type:      pluginsdk.EventType("test-plugin:secret"),
		Payload:   `{}`,
		Sensitive: false, // INV-7 under-claim
	}
	err := emitter.Emit(context.Background(), "test-plugin", intent)
	require.Error(t, err)
	assert.Empty(t, pub.events)
}

// newSensitiveTestManifest constructs a minimal valid Manifest with a
// crypto.emits block. ActorKindsClaimable is []string per manifest.go:84
// (validated/normalized to lowercase strings — "plugin", "character",
// etc.). Crypto is *CryptoSection per manifest.go:107.
func newSensitiveTestManifest(name, namespace string, emits []plugins.CryptoEmit) *plugins.Manifest {
	return &plugins.Manifest{
		Name:                name,
		Emits:               []string{namespace},
		ActorKindsClaimable: []string{"plugin"},
		Crypto:              &plugins.CryptoSection{Emits: emits},
	}
}
```

If `newSensitiveTestManifest` already exists in this package's test helpers, drop the local definition.

- [ ] **Step 4: Run.**

Run: `task test -- -run TestEmitter ./internal/plugin/`. Expected: PASS for all four new tests.

- [ ] **Step 5: Run all `internal/plugin/` tests to catch regressions.**

Run: `task test -- ./internal/plugin/`. Expected: green.

- [ ] **Step 6: Commit.**

`jj describe -m "feat(plugin): wire emit-time sensitivity fence into PluginEventEmitter (holomush-ojw1.1.5)"`, then `jj new`.

---

### Task 7: Sensitivity-aware crypto path in publisher

The publisher branches on `event.Sensitive`. False → existing identity path (zero behavioral change). True → new path: get DEK from `dek.Manager`, build AAD, encode with `xchacha20poly1305-v1`, stamp `App-Codec`/`App-Dek-Ref`/`App-Dek-Version` headers.

**Files:**

- Modify: `internal/eventbus/publisher.go`
- Modify: `internal/eventbus/publisher_test.go`

- [ ] **Step 1: Add `dek.Manager` dependency to `JetStreamPublisher`.**

In `internal/eventbus/publisher.go`, find the `JetStreamPublisher` struct and add the field + a constructor option:

```go
type JetStreamPublisher struct {
	js       *jetstream.JetStream  // existing
	selector codec.KeySelector     // existing
	keys     codec.KeyProvider     // existing
	// ... existing fields ...

	// dekMgr provides DEKs for sensitive events (event.Sensitive=true).
	// nil → publisher takes the legacy identity path for every event.
	// Wired via WithDEKManager option; bootstrap supplies it when
	// CryptoConfig.Enabled is true.
	dekMgr DEKManager
}

// DEKManager is the publisher-facing subset of dek.Manager — Phase 3a
// uses GetOrCreate on the emit path; decrypt-on-fanout (Phase 3b) will
// use Resolve.
type DEKManager interface {
	GetOrCreate(ctx context.Context, ctxID dek.ContextID, initial []dek.Participant) (codec.Key, error)
}

// WithDEKManager wires a DEK manager. When non-nil, sensitive events
// (event.Sensitive=true) take the crypto branch in Publish; nil keeps
// behavior identical to pre-Phase-3a builds.
func WithDEKManager(m DEKManager) PublishOption {
	return func(p *JetStreamPublisher) { p.dekMgr = m }
}
```

`PublishOption` is defined at `internal/eventbus/publisher.go:67` as `type PublishOption func(*JetStreamPublisher)`. Existing options (`WithCodecSelector` at line 72, `WithKeyProvider` at line 78, `WithSafetyMargin` at line 84) follow the `func WithX(x X) PublishOption { return func(p *JetStreamPublisher) { p.x = x } }` pattern. Match it.

The import for `dek` is `github.com/holomush/holomush/internal/eventbus/crypto/dek`.

- [ ] **Step 2: Add the sensitivity-aware branch to `Publish`.**

The patch lands between `proto.Marshal(envelope)` (line 171) and the existing `c.Encode(...)` call. Specifically, replace lines 176-205 of `internal/eventbus/publisher.go` (from `codecName, keyLabel, err := p.selector.SelectForEncrypt(...)` through the existing `encoded, err := c.Encode(ctx, plainBytes, key, nil)` call that Task 1 just landed) with the sensitivity-branched version below. The variable `envelope` (the `*eventbusv1.Event` constructed at line 162) and `plainBytes` (its proto-marshaled form, line 171) are both in scope at the patch point and used by the new code.

```go
var (
	codecName codec.Name
	key       codec.Key
	dekRef    string
	dekVer    string
)
if event.Sensitive && p.dekMgr != nil {
	ctxID, ctxErr := contextIDFromSubject(event.Subject)
	if ctxErr != nil {
		return oops.Code("EVENTBUS_DEK_CONTEXT_ID_FAILED").
			With("subject", string(event.Subject)).
			Wrap(ctxErr)
	}
	k, dekErr := p.dekMgr.GetOrCreate(ctx, ctxID, nil)
	if dekErr != nil {
		return oops.Code("EVENTBUS_DEK_GETORCREATE_FAILED").
			With("subject", string(event.Subject)).
			Wrap(dekErr)
	}
	codecName = codec.NameXChaCha20v1
	key = k
	dekRef = strconv.FormatUint(uint64(k.ID), 10)
	dekVer = strconv.FormatUint(uint64(k.Version), 10)
} else {
	if event.Sensitive && p.dekMgr == nil {
		return oops.Code("EVENTBUS_SENSITIVE_EVENT_NO_DEK_MANAGER").
			With("subject", string(event.Subject)).
			Errorf("event.Sensitive=true but publisher has no DEK manager wired")
	}
	cn, keyLabel, err := p.selector.SelectForEncrypt(ctx, string(event.Subject))
	if err != nil {
		return oops.Code("EVENTBUS_CODEC_SELECT_FAILED").Wrap(err)
	}
	codecName = cn
	if codecName != codec.NameIdentity {
		if p.keys == nil {
			return oops.Code("EVENTBUS_KEY_PROVIDER_MISSING").
				With("codec", string(codecName)).
				Errorf("non-identity codec requires a KeyProvider")
		}
		k, keyErr := p.keys.Active(ctx, keyLabel)
		if keyErr != nil {
			return oops.Code("EVENTBUS_KEY_FETCH_FAILED").
				With("codec", string(codecName)).
				With("key_label", string(keyLabel)).
				Wrap(keyErr)
		}
		key = k
	}
}

c, err := codec.Resolve(codecName)
if err != nil {
	return oops.Code("EVENTBUS_CODEC_UNKNOWN").With("codec", string(codecName)).Wrap(err)
}

// aad.Build verified signature (internal/eventbus/crypto/aad/aad.go:62):
//   func Build(event *eventbusv1.Event, codecName string,
//              dekRef uint64, dekVersion uint32) ([]byte, error)
// envelope is the *eventbusv1.Event constructed at line 162 of this
// file; codecName is codec.Name (cast to string); key.ID is codec.KeyID
// (cast to uint64); key.Version is uint32 (Phase 3a addition).
var aadBytes []byte
if event.Sensitive {
	ab, aErr := aad.Build(envelope, string(codecName), uint64(key.ID), key.Version)
	if aErr != nil {
		return oops.Code("EVENTBUS_AAD_BUILD_FAILED").Wrap(aErr)
	}
	aadBytes = ab
}

encoded, err := c.Encode(ctx, plainBytes, key, aadBytes)
if err != nil {
	return oops.Code("EVENTBUS_CODEC_ENCODE_FAILED").
		With("codec", string(codecName)).
		Wrap(err)
}
```

(The `aad` package import is `github.com/holomush/holomush/internal/eventbus/crypto/aad`.)

- [ ] **Step 3: Stamp the new headers.**

In the same function, where existing `msg.Header.Set(HeaderCodec, ...)` lives (around line 215), append:

```go
if dekRef != "" {
	msg.Header.Set(HeaderDekRef, dekRef)
	msg.Header.Set(HeaderDekVersion, dekVer)
}
```

- [ ] **Step 4: Add the `contextIDFromSubject` helper.**

Same file (or a new utility file in the same package — `internal/eventbus/context_id.go` is appropriate):

```go
// contextIDFromSubject derives a dek.ContextID from a NATS-native
// subject like "events.<game>.scene.<id>.<facet>". Phase 3a supports
// the scene namespace; other namespaces will be added as their plugins
// gain crypto.emits declarations.
func contextIDFromSubject(subject Subject) (dek.ContextID, error) {
	s := string(subject)
	if !strings.HasPrefix(s, "events.") {
		return dek.ContextID{}, oops.New("subject is not in events.<game>.<namespace>.<id>... form")
	}
	parts := strings.SplitN(s, ".", 5)
	if len(parts) < 4 {
		return dek.ContextID{}, oops.With("subject", s).
			Errorf("subject must have at least events.<game>.<namespace>.<id>")
	}
	namespace := parts[2]
	id := parts[3]
	if id == "" {
		return dek.ContextID{}, oops.With("subject", s).
			New("subject context id token must not be empty")
	}
	return dek.ContextID{Type: namespace, ID: id}, nil
}
```

- [ ] **Step 5: Write publisher tests for the crypto branch.**

Append to `internal/eventbus/publisher_test.go`:

```go
func TestPublisherWithoutDEKManagerRejectsSensitiveEvent(t *testing.T) {
	pub := newTestJetStreamPublisher(t /* no WithDEKManager */)
	event := newTestEvent(t)
	event.Sensitive = true

	err := pub.Publish(context.Background(), event)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_SENSITIVE_EVENT_NO_DEK_MANAGER")
}

func TestPublisherWithDEKManagerStampsCryptoHeadersOnSensitiveEvent(t *testing.T) {
	dek := newStubDEKManagerWithKey(t, codec.Key{ID: 42, Version: 3, Bytes: testKey32Bytes(t)})
	pub := newTestJetStreamPublisher(t, eventbus.WithDEKManager(dek))
	event := newTestEvent(t)
	event.Sensitive = true
	event.Subject = "events.main.scene.01HXXXSCENEID000000000"

	require.NoError(t, pub.Publish(context.Background(), event))

	msg := waitForOnePublishedMessage(t, pub)
	assert.Equal(t, "xchacha20poly1305-v1", msg.Header.Get(eventbus.HeaderCodec))
	assert.Equal(t, "42", msg.Header.Get(eventbus.HeaderDekRef))
	assert.Equal(t, "3", msg.Header.Get(eventbus.HeaderDekVersion))
}
```

The helpers `newTestJetStreamPublisher`, `newTestEvent`, `waitForOnePublishedMessage`, `newStubDEKManagerWithKey`, `testKey32Bytes` should follow the patterns already in this test file. If `newTestJetStreamPublisher` doesn't accept options today, extend it variadic.

`newStubDEKManagerWithKey` is a small new test fake — implement once at the top of this file:

```go
type stubDEKManager struct{ key codec.Key }

func (s stubDEKManager) GetOrCreate(_ context.Context, _ dek.ContextID, _ []dek.Participant) (codec.Key, error) {
	return s.key, nil
}

func newStubDEKManagerWithKey(_ *testing.T, k codec.Key) eventbus.DEKManager {
	return stubDEKManager{key: k}
}

func testKey32Bytes(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}
```

- [ ] **Step 6: Run.**

Run: `task test -- -run "TestPublisherWithoutDEK|TestPublisherWithDEKManagerStamps" ./internal/eventbus/`. Expected: PASS.

- [ ] **Step 7: Run full eventbus test suite for regressions.**

Run: `task test -- ./internal/eventbus/...`. Expected: green.

- [ ] **Step 8: Commit.**

`jj describe -m "feat(eventbus): sensitivity-aware crypto path in JetStreamPublisher (holomush-ojw1.1.6)"`, then `jj new`.

---

### Task 8: Bootstrap wiring + `Crypto.Enabled` config flag

**Files:**

- Modify: `internal/eventbus/config.go` (add config struct)
- Create: `internal/bootstrap/wire_crypto.go`
- Create: `internal/bootstrap/wire_crypto_test.go`

- [ ] **Step 1: Test the flag default.**

`internal/eventbus/config.go:61` declares `func (c Config) Defaults() Config` — a method, not a free function. Use it (or the zero value) below.

In the test file alongside `config.go` (`rg -l "Test.*Defaults\|Test.*Config" internal/eventbus/` to find the right one — likely `config_test.go`), append:

```go
func TestCryptoEnabledDefaultsToFalse(t *testing.T) {
	cfg := eventbus.Config{}.Defaults()
	assert.False(t, cfg.Crypto.Enabled, "Phase 3a ships dark — flag must default to off")
}
```

- [ ] **Step 2: Add the field.**

In `internal/eventbus/config.go`, add the field to the existing `Config` struct (around line 31-56) and define `CryptoConfig`:

```go
type Config struct {
	// ... existing fields ...
	Crypto CryptoConfig `koanf:"crypto"`
}

// CryptoConfig gates the Phase 3a sensitivity-aware crypto path.
// Default Enabled=false → the publisher behaves identically to
// pre-Phase-3a builds. See spec §11.1 phase 3.
type CryptoConfig struct {
	Enabled bool `koanf:"enabled"`
}
```

`Config.Defaults()` at line 61 needs no edit — `Crypto.Enabled` should default to false (zero value), and `Defaults()`'s pattern is "set to default-if-zero-value", which for a bool we want to skip (flag stays false unless explicitly set).

- [ ] **Step 3: Wiring smoke test.**

Create `internal/bootstrap/wire_crypto_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/bootstrap"
	"github.com/holomush/holomush/internal/eventbus"
)

func TestPluginEmitterDepsBuildSucceedsWithCryptoDisabled(t *testing.T) {
	cfg := eventbus.Config{} // Crypto.Enabled defaults false
	deps := bootstrap.PluginEmitterDeps{
		Publisher:  &noopPublisher{},
		Manifests:  func(string) *bootstrap.Manifest { return nil },
		Resolver:   func(context.Context, string) (bootstrap.Actor, error) { return bootstrap.Actor{}, nil },
	}
	emitter, err := bootstrap.BuildPluginEmitter(context.Background(), cfg, deps)
	require.NoError(t, err)
	assert.NotNil(t, emitter)
}

type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, eventbus.Event) error { return nil }
```

- [ ] **Step 4: Implement.**

Create `internal/bootstrap/wire_crypto.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package bootstrap — Phase 3a crypto wiring (holomush-ojw1.1).
//
// BuildPluginEmitter constructs a PluginEventEmitter; when
// cfg.Crypto.Enabled is true, the caller MUST also wire a DEK manager
// into the publisher via eventbus.WithDEKManager (this happens during
// JetStreamPublisher construction, not here — this helper produces
// the emitter half only).
package bootstrap

import (
	"context"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	plugins "github.com/holomush/holomush/internal/plugin"
)

// Manifest is re-exported for callers that don't want to import the
// internal/plugin package directly.
type Manifest = plugins.Manifest

// Actor is re-exported similarly. Note: the canonical Actor type is
// core.Actor, NOT plugins.Actor (which doesn't exist). We alias core
// here for callers' convenience.
type Actor = core.Actor

// PluginEmitterDeps bundles the constructor inputs.
type PluginEmitterDeps struct {
	Publisher eventbus.Publisher
	Manifests plugins.ManifestLookup
	Resolver  plugins.ActorResolver
}

// BuildPluginEmitter constructs a PluginEventEmitter from cfg + deps.
// The crypto enable flag affects publisher wiring (DEK manager,
// sensitivity branch), NOT the emitter's structure — the emitter
// always runs the fence; the fence's effective output depends on the
// manifest only. The flag matters in the publisher.
func BuildPluginEmitter(_ context.Context, _ eventbus.Config, deps PluginEmitterDeps) (*plugins.PluginEventEmitter, error) {
	return plugins.NewPluginEventEmitter(deps.Publisher, deps.Manifests, deps.Resolver), nil
}
```

- [ ] **Step 5: Run.**

Run: `task test -- ./internal/bootstrap/ ./internal/eventbus/`. Expected: PASS.

- [ ] **Step 6: Commit.**

`jj describe -m "feat(bootstrap): Crypto.Enabled config + emitter builder (holomush-ojw1.1.8)"`, then `jj new`.

---

### Task 9: Integration test — sensitive emit produces ciphertext + matching audit row

E2E test. Real PostgreSQL container (via `test/testutil`), embedded NATS (via `internal/eventbus/eventbustest` for unit-style integration; or full bus for full E2E). Asserts:

1. Bus message payload is ciphertext (not equal to JSON plaintext).
2. `App-Codec` = `xchacha20poly1305-v1`; `App-Dek-Ref`, `App-Dek-Version` populated.
3. `events_audit` row mirrors all of the above byte-for-byte (INV-21).
4. Round-trip via `aad.Build` + `codec.NewXChaCha20Poly1305v1().Decode` recovers original plaintext (INV-25).

**Files:**

- Create: `test/integration/crypto/emit_test.go`

- [ ] **Step 1: Write the integration test.**

Create `test/integration/crypto/emit_test.go`:

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package crypto_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/holomush/holomush/test/testutil"
)

// TestSensitiveEmitProducesCiphertextOnBusAndInAudit lands the
// end-to-end Phase 3a behavior: a manifest=may + Sensitive=true emit
// produces ciphertext on the bus and a byte-equal events_audit row
// (INV-21). AAD-bind tamper detection (INV-25) is unit-tested in
// internal/eventbus/codec/xchacha20poly1305_test.go; full decrypt
// round-trip is Phase 3b's job (subscribe path).
func TestSensitiveEmitProducesCiphertextOnBusAndInAudit(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPostgres(t)
	bus := testutil.StartEmbeddedJetStream(t)
	pool := testutil.NewPGPool(t, pg.ConnString())
	defer pool.Close()

	// KEK source: env-backed test KEK (32 random bytes hex-encoded).
	kekHex := testutil.RandomKEKHex(t)
	t.Setenv("HOLOMUSH_TEST_KEK", kekHex)
	kekSource := kek.NewEnvSource("HOLOMUSH_TEST_KEK", false)

	provider, err := kek.NewLocalAEADProvider(ctx, kekSource, pool)
	require.NoError(t, err)

	// Verified signatures (cited path:line):
	//   dek.NewStore(pool *pgxpool.Pool) *Store           // store.go:77
	//   dek.NewCache(cfg CacheConfig) *Cache              // cache.go:74
	//   dek.NewManager(provider, store, cache) (Manager, error) // manager.go:49
	//   CacheConfig{Capacity int; TTL time.Duration}      // cache.go:26
	dekStore := dek.NewStore(pool)
	dekCache := dek.NewCache(dek.CacheConfig{Capacity: 64})
	dekMgr, err := dek.NewManager(provider, dekStore, dekCache)
	require.NoError(t, err)

	manifest := &plugins.Manifest{
		Name:                "test-plugin",
		Emits:               []string{"scene"},
		ActorKindsClaimable: []string{"plugin"},
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "test-plugin:whisper", Sensitivity: plugins.SensitivityMay},
			},
		},
	}
	manifestLookup := func(name string) *plugins.Manifest {
		if name == "test-plugin" {
			return manifest
		}
		return nil
	}
	actorResolver := func(_ context.Context, _ string) (core.Actor, error) {
		return core.Actor{Kind: core.ActorPlugin, ID: "test-plugin"}, nil
	}

	// Verified signature (publisher.go:121):
	//   func NewJetStreamPublisher(js jetstream.JetStream, cfg Config, opts ...PublishOption) *JetStreamPublisher
	// No ctx parameter; second arg is Config; single return value.
	// bus.JS is a public field on eventbustest.Embedded (embedded.go:50);
	// not a method.
	publisher := eventbus.NewJetStreamPublisher(
		bus.JS,
		eventbus.Config{}.Defaults(),
		eventbus.WithDEKManager(dekMgr),
	)

	emitter := plugins.NewPluginEventEmitter(publisher, manifestLookup, actorResolver)

	const plaintext = `{"text":"hello, secret world"}`
	intent := pluginsdk.EmitIntent{
		Subject:   "scene:01HXXXTESTSCENE000000000",
		Type:      pluginsdk.EventType("test-plugin:whisper"),
		Payload:   plaintext,
		Sensitive: true,
	}
	require.NoError(t, emitter.Emit(ctx, "test-plugin", intent))

	// 1. Bus assertion. jetstream.Msg uses methods (Data, Headers, Subject).
	msg := testutil.WaitForOneJetStreamMsg(t, bus, "events.>", testutil.DefaultWait)
	headers := msg.Headers()
	assert.Equal(t, "xchacha20poly1305-v1", headers.Get(eventbus.HeaderCodec))
	dekRefHdr := headers.Get(eventbus.HeaderDekRef)
	dekVerHdr := headers.Get(eventbus.HeaderDekVersion)
	require.NotEmpty(t, dekRefHdr)
	require.NotEmpty(t, dekVerHdr)
	assert.NotEqual(t, []byte(plaintext), msg.Data(), "payload must be ciphertext")

	// 2. Audit row mirrors bus (INV-21).
	// Nats-Msg-Id stamps the event ULID; the audit projection decodes it
	// back to bytes for the BYTEA id column. Use the same decoder.
	natsMsgID := headers.Get("Nats-Msg-Id")
	require.NotEmpty(t, natsMsgID)
	idBytes := testutil.MustParseULID(t, natsMsgID).Bytes()
	row := testutil.QueryEventsAuditByID(t, pool, idBytes)
	assert.Equal(t, "xchacha20poly1305-v1", row.Codec)
	require.NotNil(t, row.DekRef)
	gotRef, _ := strconv.ParseInt(dekRefHdr, 10, 64)
	assert.Equal(t, gotRef, *row.DekRef)
	require.NotNil(t, row.DekVersion)
	gotVer, _ := strconv.ParseInt(dekVerHdr, 10, 32)
	assert.Equal(t, int32(gotVer), *row.DekVersion)
	assert.Equal(t, msg.Data(), row.Payload, "INV-21: bus and audit payload bytes must be byte-equal")

	// AAD-bind verification (INV-25 round-trip) is unit-tested at
	// internal/eventbus/codec/xchacha20poly1305_test.go::TestXChaCha20Poly1305DetectsAADTamper.
	// Decrypt-on-fanout E2E (full plaintext recovery via the subscriber
	// path) is Phase 3b.
}
```

**Testutil inventory check.** Run `ls test/testutil/` and `rg "func StartPostgres|func NewPGPool" test/testutil/`. Expected: only `postgres.go` and `postgres_integration_test.go` exist; `StartPostgres` and `NewPGPool` exist there. Every other helper below is **new** in this task.

Land all new helpers in a single `test/testutil/crypto.go` file as part of Step 1. Concrete implementations:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package testutil

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// EmbeddedBus is the test-side handle to an embedded NATS+JetStream
// instance from internal/eventbus/eventbustest.
type EmbeddedBus = eventbustest.Embedded

func StartEmbeddedJetStream(t *testing.T) *EmbeddedBus {
	t.Helper()
	return eventbustest.New(t)
}

// RandomKEKHex returns kek.KEKByteLength random bytes hex-encoded —
// the form expected by kek.NewEnvSource for test deployments.
func RandomKEKHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, kek.KEKByteLength)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return hex.EncodeToString(b)
}

func MustParseULID(t *testing.T, s string) ulid.ULID {
	t.Helper()
	u, err := ulid.Parse(s)
	require.NoError(t, err)
	return u
}

const DefaultWait = 5 * time.Second

// WaitForOneJetStreamMsg pull-subscribes to subject and returns the
// first message received within timeout. Uses bus.JS (the public field
// on eventbustest.Embedded — embedded.go:50, NOT a method) and the
// project-wide stream name eventbus.StreamName ("EVENTS",
// subsystem.go:24).
func WaitForOneJetStreamMsg(t *testing.T, bus *EmbeddedBus, subject string, timeout time.Duration) jetstream.Msg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cons, err := bus.JS.OrderedConsumer(ctx, eventbus.StreamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{subject},
	})
	require.NoError(t, err)

	msgs, err := cons.Fetch(1, jetstream.FetchMaxWait(timeout))
	require.NoError(t, err)
	for msg := range msgs.Messages() {
		return msg
	}
	require.Fail(t, "no JetStream message received within timeout", "subject=%s", subject)
	return nil
}

// EventsAuditRow is the test-side projection of a single events_audit
// row, exposing the columns this test cares about.
type EventsAuditRow struct {
	Codec      string
	Payload    []byte
	DekRef     *int64
	DekVersion *int32
}

func QueryEventsAuditByID(t *testing.T, pool *pgxpool.Pool, idBytes []byte) EventsAuditRow {
	t.Helper()
	var row EventsAuditRow
	var dekRef sql.NullInt64
	var dekVer sql.NullInt32
	err := pool.QueryRow(context.Background(),
		`SELECT codec, payload, dek_ref, dek_version FROM events_audit WHERE id = $1`,
		idBytes,
	).Scan(&row.Codec, &row.Payload, &dekRef, &dekVer)
	require.NoError(t, err)
	if dekRef.Valid {
		v := dekRef.Int64
		row.DekRef = &v
	}
	if dekVer.Valid {
		v := dekVer.Int32
		row.DekVersion = &v
	}
	return row
}

// MustUnmarshalEventbusEnvelope proto-unmarshals decrypted plaintext
// bytes back into the eventbus envelope. For sensitive events,
// codec.Decode returns these proto bytes; pass the result here to get
// the inner *eventbusv1.Event with original Payload, ID, etc.
func MustUnmarshalEventbusEnvelope(t *testing.T, decryptedBytes []byte) *eventbusv1.Event {
	t.Helper()
	envelope := &eventbusv1.Event{}
	require.NoError(t, proto.Unmarshal(decryptedBytes, envelope))
	return envelope
}

// ExtractInnerJSONPayload returns the plugin's original JSON payload
// from an unmarshaled envelope.
func ExtractInnerJSONPayload(_ *testing.T, envelope *eventbusv1.Event) string {
	return string(envelope.GetPayload())
}
```

- [ ] **Step 2: Run the integration test.**

Run: `task test:int -- -run TestSensitiveEmitProducesCiphertextOnBusAndInAudit ./test/integration/crypto/...`

Expected: PASS. If a helper is missing, add it as a small commit on top before continuing.

- [ ] **Step 3: Lint.**

Run: `task lint:go`. Expected: 0 issues.

- [ ] **Step 4: Commit.**

`jj describe -m "test(crypto): integration test — sensitive emit ciphertext + audit (holomush-ojw1.1.9)"`, then `jj new`.

---

### Task 10: Wire spec invariants to enforcing tests

Master spec §2 (invariants) currently lists each invariant with a "Test type" column but no concrete test name. For the four invariants Phase 3a closes (INV-6, INV-7, INV-25, INV-49) and the one we depend on (INV-21), add a "Enforcing test" line per spec convention.

**Files:**

- Modify: `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`

- [ ] **Step 1: Add enforcing-test pointers.**

Edit the spec around the relevant rows in §2 (search for `INV-6`, `INV-7`, `INV-21`, `INV-25`, `INV-49` lines around line 200-330). Each row gets the existing description left intact; below it (or in a new column if the table is widened uniformly), add a single-line annotation:

```text
INV-6  → Test: internal/plugin/sensitivity_fence_test.go::TestEnforceSensitivity (table case "never + claim=true → INV-6 reject")
INV-7  → Test: internal/plugin/sensitivity_fence_test.go::TestEnforceSensitivity (table case "always + claim=false → INV-7 reject")
INV-21 → Test: test/integration/crypto/emit_test.go::TestSensitiveEmitProducesCiphertextOnBusAndInAudit (assertion on byte-equality)
INV-25 → Test: internal/eventbus/codec/xchacha20poly1305_test.go::TestXChaCha20Poly1305DetectsAADTamper
INV-49 → Test: internal/eventbus/audit/projection_test.go::TestPersistWritesDekColumnsFromHeaders
```

Format the additions to match whatever format the spec already uses for traceability (footnotes, paragraph below table, or new column — pick one and apply uniformly to these five rows).

- [ ] **Step 2: Run lint.**

Run: `task lint:markdown`. Expected: clean.

- [ ] **Step 3: Run fmt.**

Run: `task fmt`. Expected: clean.

- [ ] **Step 4: Commit.**

`jj describe -m "docs(crypto): wire Phase 3a invariants to enforcing tests (holomush-ojw1.1.10)"`, then `jj new`.

---

## Acceptance gates

Before opening a PR for Phase 3a:

- [ ] All ten tasks committed in order.
- [ ] `task lint:go` → 0 issues.
- [ ] `task test` → green.
- [ ] `task test:int -- ./test/integration/crypto/...` → green.
- [ ] Cold-cache `task pr-prep` → green end-to-end.
- [ ] `code-reviewer` adversarial sub-agent → READY (per project policy).
- [ ] PR description references spec sections this lands (§5.1, §11.1, §4.6, §4.7) and the four resolved decisions in the grounding doc.

## What this DOES NOT yet provide

Stated explicitly so reviewers and operators don't expect it:

- **No subscriber-side decryption.** A Subscribe call against a sensitive event delivers ciphertext to the wire with no `metadata_only` flag. **Phase 3b** adds AuthGuard + decrypt-on-fanout + `metadata_only`.
- **No DEK cache invalidation across replicas.** Phase 3a uses whatever in-memory cache the Phase 2 `dek.Cache` ships with. Multi-replica invalidation is **Phase 3c**.
- **No cold-tier crypto on `QueryStreamHistory`.** Cold-tier reads of sensitive events return ciphertext to the caller. **Phase 3d**.
- **No NATS account-level deny rules.** Operators with bus access can subscribe to sensitive subjects (and read ciphertext). **Phase 3d**.

`Crypto.Enabled` stays `false` in production until Phase 3d. Until then, any sensitive emit produced under `Crypto.Enabled=true` lands as ciphertext that no subscriber can decrypt — useful for staging soak only.
