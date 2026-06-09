<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Sensitive-Event Crypto Activation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make KEK presence the single activation gate for sensitive-event crypto, prove every character has a binding at creation, and require a (frictionlessly auto-provisioned) KEK to boot.

**Architecture:** Three pillars on `internal/grpc` (gate), `internal/grpc`/`internal/auth` (binding-guarantee tests), and `cmd/holomush` + `internal/eventbus/crypto/kek` (KEK boot). Pillar 3 reverses the prior "KEK-less degraded posture," so it also updates the integration harness, E2E compose, and docs.

**Tech Stack:** Go, gRPC (`CoreServer`), `koanf` config, `cobra` CLI, ChaCha20-Poly1305/Argon2id KEK file (`internal/eventbus/crypto/kek`), testify + Ginkgo (integration).

**Spec:** `docs/superpowers/specs/2026-06-09-sensitive-event-crypto-activation-design.md`

---

## File Structure

| File | Responsibility | Pillar |
| --- | --- | --- |
| `internal/grpc/auth_handlers.go` | Rename `WithCryptoEnabled`→`WithCryptoActive`; field `cryptoEnabled`→`cryptoActive` | 1 |
| `internal/grpc/server.go` | Subscribe identity-build gate (`:995`) | 1 |
| `internal/grpc/query_stream_history.go` | History identity-build gate (`:306`) | 1 |
| `cmd/holomush/sub_grpc.go` | Wire `WithCryptoActive(cfg.RekeyManager != nil)` into the `CoreServer` option list | 1 |
| `internal/grpc/auth_handlers_test.go` | Binding minted on normal create path | 2 |
| `internal/auth/guest_service_test.go` | Binding minted on guest path | 2 |
| `internal/eventbus/crypto/kek/source_file_test.go` | Confirm absent-keyfile is `errors.Is(os.ErrNotExist)`-detectable (test-only; no `source_file.go` change) | 3 |
| `cmd/holomush/kek_provision.go` (new) | `resolvePassphrase` (env/file-ref/prompt) + `ensureKeyfile` (auto-gen) + `provisionBootKEKProvider` (boot path); `cmd_admin_totp_deps.go` left unchanged | 3 |
| `cmd/holomush/core.go` | KEK error becomes fatal (`:745-749`) | 3 |
| `cmd/holomush/root.go` | `--auto-gen-kek` flag | 3 |
| `internal/testsupport/integrationtest/*.go` | Provision a test KEK so suites boot | 3 (blast) |
| `compose.e2e.yaml`, `docs/...` | Test passphrase env; operator docs | 3 (blast) |
| `docs/architecture/invariants.yaml` | Register the 3 new invariants | 5 |
| `test/integration/crypto/*_test.go` | Decrypt-to-participant; no metadata-only-to-participant | 5 |

---

## Phase 1: Single gate — KEK presence

### Task 1: Rename `cryptoEnabled`→`cryptoActive` and wire it from KEK presence

**Files:**

- Modify: `internal/grpc/auth_handlers.go` (field doc `:178`, option `:150-160`)
- Modify: `internal/grpc/server.go:995`
- Modify: `internal/grpc/query_stream_history.go:306`
- Modify: `cmd/holomush/sub_grpc.go` (the `coreServerOpts := []holoGRPC.CoreServerOption{…}` slice, ~`:495`, where `WithBindingRepository` is appended)
- Modify: `cmd/holomush/sub_grpc_test.go`
- Modify: `internal/grpc/subscribe_server_test.go` (struct literals `cryptoEnabled: true` at `:353,:386`)
- Modify: `internal/grpc/query_stream_history_test.go` (struct literals `cryptoEnabled: true` at `:1011,:1052`)

- [ ] **Step 1: Write the failing test** (option wiring)

In `cmd/holomush/sub_grpc_test.go`, add:

```go
// Verifies: the activation gate is wired from KEK presence, not a standalone flag.
func TestCoreServerCryptoActiveTracksKEKPresence(t *testing.T) {
	require.True(t, cryptoActiveFor(grpcSubsystemConfig{RekeyManager: &stubDEKManager{}}),
		"RekeyManager set ⇒ crypto active")
	require.False(t, cryptoActiveFor(grpcSubsystemConfig{}),
		"no RekeyManager ⇒ crypto inactive")
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestCoreServerCryptoActiveTracksKEKPresence ./cmd/holomush/`
Expected: FAIL — `undefined: cryptoActiveFor`.

- [ ] **Step 3: Add the helper + wire the option**

In `cmd/holomush/sub_grpc.go`, add the helper and use it where `CoreServer` options are assembled (the same option slice that already appends `holoGRPC.WithBindingRepository(bindingRepo)` at `:418`):

```go
// cryptoActiveFor reports whether sensitive-event crypto is active for this
// subsystem config: true iff a KEK (RekeyManager) is wired. This is the single
// activation gate (replaces the vestigial cryptoEnabled flag).
func cryptoActiveFor(cfg grpcSubsystemConfig) bool { return cfg.RekeyManager != nil }
```

Then in the option list: `opts = append(opts, holoGRPC.WithCryptoActive(cryptoActiveFor(s.cfg)))`.

- [ ] **Step 4: Rename the field + option in `internal/grpc` (incl. struct-literal test sites)**

In `internal/grpc/auth_handlers.go`: rename `WithCryptoEnabled`→`WithCryptoActive`, update its doc to "gates binding-resolution on KEK presence; wired from `RekeyManager != nil`". In `server.go:178` rename the field `cryptoEnabled`→`cryptoActive`. Update both gate sites:

```go
// server.go:995  and  query_stream_history.go:306 (identical)
if s.bindings != nil && s.cryptoActive {
```

The field is package-private, so the rename also breaks the test files that set it directly in `CoreServer{…}` struct literals. Update those too (the field is in `package grpc`, same as the tests):

Run: `rg -n 'cryptoEnabled' internal/grpc/` to find every site, then change `cryptoEnabled: true` → `cryptoActive: true` in `internal/grpc/subscribe_server_test.go:353,386` and `internal/grpc/query_stream_history_test.go:1011,1052` (and any other hit). A clean compile of `./internal/grpc/` is the check.

- [ ] **Step 5: Run the wiring test + the package tests**

Run: `task test -- ./cmd/holomush/ ./internal/grpc/`
Expected: PASS (the existing `TestSubscriberOptions*` and gate tests compile against the renamed symbol).

- [ ] **Step 6: Commit**

`jj commit -m "feat(crypto): gate Subscribe/history identity on KEK presence (cryptoActive) (holomush-5rh.8.29.12)"`

### Task 2: Extract the identity-build gate into a tested helper (DRY + lockstep)

The `if s.bindings != nil && s.cryptoActive { Current → NewCharacterIdentity → ToSessionIdentity }`
block is duplicated verbatim at `server.go:995` (Subscribe) and `query_stream_history.go:306`
(QueryStreamHistory). Extract it so the gate has one source of truth (both sites stay in
lockstep) and is unit-testable in isolation.

**Files:**

- Create: `internal/grpc/session_identity.go`
- Modify: `internal/grpc/server.go:995`, `internal/grpc/query_stream_history.go:306`
- Test: `internal/grpc/session_identity_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestBuildCharacterIdentity(t *testing.T) {
	pid, cid := idgen.New().String(), idgen.New().String()
	t.Run("crypto inactive ⇒ zero identity, no lookup", func(t *testing.T) {
		s := &CoreServer{bindings: &fakeBindingRepo{}, cryptoActive: false}
		id, err := s.buildCharacterIdentity(context.Background(), pid, cid)
		require.NoError(t, err)
		require.Equal(t, eventbus.SessionIdentity{}, id)
	})
	t.Run("nil bindings ⇒ zero identity", func(t *testing.T) {
		s := &CoreServer{bindings: nil, cryptoActive: true}
		id, err := s.buildCharacterIdentity(context.Background(), pid, cid)
		require.NoError(t, err)
		require.Equal(t, eventbus.SessionIdentity{}, id)
	})
	t.Run("active + binding ⇒ character identity", func(t *testing.T) {
		s := &CoreServer{bindings: &fakeBindingRepo{current: "bind-1"}, cryptoActive: true}
		id, err := s.buildCharacterIdentity(context.Background(), pid, cid)
		require.NoError(t, err)
		require.NotEqual(t, eventbus.SessionIdentity{}, id) // a real character identity
	})
	t.Run("Current error ⇒ wrapped BINDING_LOOKUP_FAILED", func(t *testing.T) {
		s := &CoreServer{bindings: &fakeBindingRepo{err: errors.New("boom")}, cryptoActive: true}
		_, err := s.buildCharacterIdentity(context.Background(), pid, cid)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "BINDING_LOOKUP_FAILED")
	})
}

type fakeBindingRepo struct {
	current string
	err     error
}

func (f *fakeBindingRepo) Create(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (f *fakeBindingRepo) Current(context.Context, string) (string, error) {
	return f.current, f.err
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestBuildCharacterIdentity ./internal/grpc/`
Expected: FAIL — `undefined: (*CoreServer).buildCharacterIdentity`.

- [ ] **Step 3: Implement the helper + replace both call sites**

```go
// internal/grpc/session_identity.go
// buildCharacterIdentity returns the typed session identity for the character
// when crypto is active and a binding repo is wired; otherwise the zero
// (passthrough) identity. Single source of truth for the Subscribe and
// QueryStreamHistory identity gate (formerly duplicated at server.go:995 /
// query_stream_history.go:306).
func (s *CoreServer) buildCharacterIdentity(ctx context.Context, playerID, characterID string) (eventbus.SessionIdentity, error) {
	if s.bindings == nil || !s.cryptoActive {
		return eventbus.SessionIdentity{}, nil
	}
	bindingID, err := s.bindings.Current(ctx, characterID)
	if err != nil {
		return eventbus.SessionIdentity{}, oops.Code("BINDING_LOOKUP_FAILED").
			With("character_id", characterID).Wrap(err)
	}
	identity, err := authguard.NewCharacterIdentity(playerID, characterID, bindingID)
	if err != nil {
		return eventbus.SessionIdentity{}, oops.Code("IDENTITY_INVALID").Wrap(err)
	}
	return authguard.ToSessionIdentity(identity), nil
}
```

Replace the inline block at `server.go:995` with the call, preserving the Subscribe-scoped outer code so existing assertions hold:

```go
sessionIdentity, identityErr := s.buildCharacterIdentity(ctx, info.PlayerID.String(), info.CharacterID.String())
if identityErr != nil {
	return oops.Code("SUBSCRIBE_BINDING_LOOKUP_FAILED").Wrap(identityErr)
}
```

and the analogous `HISTORY_BINDING_LOOKUP_FAILED` wrap at `query_stream_history.go:306`. Then run `rg -n 'SUBSCRIBE_BINDING_LOOKUP_FAILED|HISTORY_BINDING_LOOKUP_FAILED|SUBSCRIBE_IDENTITY_INVALID|HISTORY_IDENTITY_INVALID' internal/grpc/` and reconcile any test asserting the old inner codes (the two outer codes above are preserved; the inner `BINDING_LOOKUP_FAILED`/`IDENTITY_INVALID` are new and chained beneath them).

- [ ] **Step 4: Run to verify it passes**

Run: `task test -- ./internal/grpc/`
Expected: PASS.

- [ ] **Step 5: Commit**

`jj commit -m "refactor(crypto): extract tested buildCharacterIdentity gate for Subscribe + history (holomush-5rh.8.29.12)"`

---

## Phase 2: Prove the creation-time binding guarantee

### Task 3: Tests that every creation path mints a binding

**Files:**

- Test: `internal/grpc/auth_handlers_test.go` (normal path — alongside `TestCreateCharacter_Success:654`)
- Test: `internal/auth/guest_service_test.go` (guest path)

- [ ] **Step 1: Write the normal-path test**

```go
// Verifies: createCharacterAtomic mints a current binding so bindings.Current resolves.
func TestCreateCharacterMintsBindingResolvableByCurrent(t *testing.T) {
	// Given a CoreServer wired with a real (or fake) transactor + BindingRepo,
	// When CreateCharacter succeeds,
	// Then bindings.Current(ctx, newCharID) returns a non-empty binding_id, no error.
	// Use the same fake BindingRepo the existing auth_handlers_test.go uses; assert
	// Create was called with reason "initial_bind" and Current then resolves.
}
```

- [ ] **Step 2: Write the guest-path test**

```go
// Verifies: guest creation mints a binding ("initial_bind_guest") in the same tx.
func TestCreateGuestMintsBinding(t *testing.T) {
	// Given NewGuestService(..., bindingRepo) (bindingRepo is a REQUIRED ctor arg),
	// When CreateGuest succeeds,
	// Then the binding repo recorded a Create with reason "initial_bind_guest"
	// and Current resolves for the guest character.
}
```

- [ ] **Step 3: Run to verify they pass (guarantee already holds) or fail (gap found)**

Run: `task test -- -run 'TestCreateCharacterMintsBinding|TestCreateGuestMintsBinding' ./internal/grpc/ ./internal/auth/`
Expected: PASS — the guarantee exists; these tests pin it against regression. If either FAILS, a creation path is missing binding-mint — fix the creation path (not the test).

- [ ] **Step 4: Add a defensive-assertion comment at the gate sites**

In `server.go:995` / `query_stream_history.go:306`, update the comment to: "binding is minted at creation (auth_handlers.go:519); `Current` failing here is a misconfiguration assertion, never expected once KEK is mandatory." (Comment-only; keeps the loud hard-fail.)

- [ ] **Step 5: Commit**

`jj commit -m "test(crypto): pin creation-time binding guarantee (normal + guest) (holomush-5rh.8.29.12)"`

---

## Phase 3: KEK required to boot + frictionless provisioning

### Task 4: Confirm an absent keyfile is detectable (test-only — no production change)

The existing `FileSource.Load` (`source_file.go:77`) already wraps the `os.ReadFile` error in
`oops.Code("KEK_FILE_LOAD_FAILED").Wrap(err)`, and `oops.Wrap` preserves the `errors.Is`
chain — so `errors.Is(loadErr, os.ErrNotExist)` already succeeds for an absent file **without
any code change**. This task only adds a regression test pinning that property (which
`ensureKeyfile` in Task 6 relies on). The existing `TestFileSource_Load_FailsOnMissingFile`
(`source_file_test.go:70`, asserts the `KEK_FILE_LOAD_FAILED` code) stays valid and unchanged —
both properties hold on the same error.

**Files:**

- Test: `internal/eventbus/crypto/kek/source_file_test.go` (add one test; **do not** modify `source_file.go`)

- [ ] **Step 1: Write the test**

```go
func TestFileSource_Load_AbsentFileIsDistinguishable(t *testing.T) {
	src, err := kek.NewFileSource("/nonexistent/master.key.enc", staticPassphraseFunc("x"))
	require.NoError(t, err)
	_, loadErr := src.Load(context.Background())
	require.Error(t, loadErr)
	require.True(t, errors.Is(loadErr, os.ErrNotExist),
		"absent keyfile MUST be distinguishable via errors.Is(os.ErrNotExist) through the oops wrap")
}
```

- [ ] **Step 2: Run to verify it passes**

Run: `task test -- -run 'TestFileSource_Load' ./internal/eventbus/crypto/kek/`
Expected: PASS — both the new test and the existing `KEK_FILE_LOAD_FAILED` test. If the new
test FAILS, `oops.Wrap` does not chain `errors.Is` for this case; only then add an
`errors.Is(err, os.ErrNotExist)` branch in `Load` that returns a `fmt.Errorf("...: %w", err)`
form **in addition to** the existing code (do not remove `KEK_FILE_LOAD_FAILED`). Add `"errors"` / `"os"` imports to the test if missing.

- [ ] **Step 3: Commit**

`jj commit -m "test(crypto): pin absent-KEK-file detectable via os.ErrNotExist (holomush-5rh.8.29.12)"`

### Task 5: Passphrase resolution — env / file-ref / prompt

**Files:**

- Create: `cmd/holomush/kek_provision.go`
- Test: `cmd/holomush/kek_provision_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestResolvePassphrase(t *testing.T) {
	t.Run("env var wins", func(t *testing.T) {
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE", "from-env")
		got, err := resolvePassphrase(passphraseSources{interactive: false})
		require.NoError(t, err)
		require.Equal(t, "from-env", string(got))
	})
	t.Run("file ref read and trimmed", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "pass")
		require.NoError(t, os.WriteFile(f, []byte("from-file\n"), 0o600))
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE", "")
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE_FILE", f)
		got, err := resolvePassphrase(passphraseSources{interactive: false})
		require.NoError(t, err)
		require.Equal(t, "from-file", string(got)) // trailing newline trimmed
	})
	t.Run("none and non-interactive errors", func(t *testing.T) {
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE", "")
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE_FILE", "")
		_, err := resolvePassphrase(passphraseSources{interactive: false})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "KEK_PASSPHRASE_UNAVAILABLE")
	})
	t.Run("missing file ref errors (not silently empty)", func(t *testing.T) {
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE", "")
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE_FILE", "/nonexistent/pass")
		_, err := resolvePassphrase(passphraseSources{interactive: false})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "KEK_PASSPHRASE_FILE_READ_FAILED")
	})
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestResolvePassphrase ./cmd/holomush/`
Expected: FAIL — `undefined: resolvePassphrase`.

- [ ] **Step 3: Implement `resolvePassphrase`**

`kek_provision.go` is in the same package (`package main`) as `cmd_admin_totp_deps.go`, which
already declares `envKEKPassphrase = "HOLOMUSH_KEK_PASSPHRASE"` (`:34`) and `envKEKFile` (`:32`).
**Reuse** those — declare ONLY the new `envKEKPassphraseFile` (declaring `envKEKPassphrase`
again is a duplicate-const compile error).

```go
const envKEKPassphraseFile = "HOLOMUSH_KEK_PASSPHRASE_FILE" // new; envKEKPassphrase already exists

type passphraseSources struct{ interactive bool }

// resolvePassphrase returns the KEK unlock passphrase from (first hit wins):
// env HOLOMUSH_KEK_PASSPHRASE, env HOLOMUSH_KEK_PASSPHRASE_FILE (file contents,
// trailing whitespace trimmed), or an interactive prompt when a TTY is attached.
// It NEVER logs the passphrase and NEVER auto-generates one.
func resolvePassphrase(src passphraseSources) ([]byte, error) {
	if p := os.Getenv(envKEKPassphrase); p != "" {
		return []byte(p), nil
	}
	if f := os.Getenv(envKEKPassphraseFile); f != "" {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, oops.Code("KEK_PASSPHRASE_FILE_READ_FAILED").With("path", f).Wrap(err)
		}
		return bytes.TrimRight(raw, "\r\n \t"), nil
	}
	if src.interactive {
		return promptPassphrase() // term.ReadPassword on stdin fd; no echo
	}
	return nil, oops.Code("KEK_PASSPHRASE_UNAVAILABLE").
		Errorf("no KEK passphrase: set %s, %s, or run interactively", envKEKPassphrase, envKEKPassphraseFile)
}
```

Add `promptPassphrase()` using `golang.org/x/term` `ReadPassword(int(os.Stdin.Fd()))` (already an indirect dep via SSH/term usage — verify with `task` build; if absent, fall back to a bufio line read guarded by `term.IsTerminal`).

- [ ] **Step 4: Run to verify it passes**

Run: `task test -- -run TestResolvePassphrase ./cmd/holomush/`
Expected: PASS.

- [ ] **Step 5: Commit**

`jj commit -m "feat(crypto): KEK passphrase via env / file-ref / prompt (holomush-5rh.8.29.12)"`

### Task 6: Auto-generate the keyfile when absent (never regenerate)

**Files:**

- Modify: `cmd/holomush/kek_provision.go`
- Test: `cmd/holomush/kek_provision_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestEnsureKeyfile(t *testing.T) {
	t.Run("absent + autoGen mints and persists, reused on second call", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "master.key.enc")
		pf := func(context.Context) ([]byte, error) { return []byte("pw"), nil }
		require.NoError(t, ensureKeyfile(context.Background(), path, pf, true))
		info1, err := os.Stat(path); require.NoError(t, err)
		require.NoError(t, ensureKeyfile(context.Background(), path, pf, true)) // idempotent
		info2, _ := os.Stat(path)
		require.Equal(t, info1.ModTime(), info2.ModTime(), "MUST NOT regenerate an existing keyfile")
	})
	t.Run("absent + NOT autoGen refuses", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "master.key.enc")
		err := ensureKeyfile(context.Background(), path, func(context.Context) ([]byte, error) { return []byte("pw"), nil }, false)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "KEK_FILE_NOT_FOUND")
	})
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestEnsureKeyfile ./cmd/holomush/`
Expected: FAIL — `undefined: ensureKeyfile`.

- [ ] **Step 3: Implement `ensureKeyfile`**

```go
// ensureKeyfile guarantees a sealed keyfile exists at path. If present: no-op.
// If absent and autoGen: mint a fresh master KEK, seal it with the passphrase,
// persist. If absent and !autoGen: return KEK_FILE_NOT_FOUND (refuse to boot).
// It MUST NOT overwrite an existing keyfile.
func ensureKeyfile(ctx context.Context, path string, pf kek.PassphraseFunc, autoGen bool) error {
	src, err := kek.NewFileSource(path, pf)
	if err != nil {
		return oops.Wrap(err)
	}
	if _, loadErr := src.Load(ctx); loadErr == nil {
		return nil // present (load succeeded) → reuse, never regenerate
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return oops.Wrap(loadErr) // corrupt / wrong-passphrase / other → surface
	}
	if !autoGen {
		return oops.Code("KEK_FILE_NOT_FOUND").With("path", path).
			Errorf("no KEK file at %s; pass --auto-gen-kek for first start", path)
	}
	master := make([]byte, kek.KEKByteLength)
	defer clear(master)
	if _, err := io.ReadFull(rand.Reader, master); err != nil { // crypto/rand
		return oops.Code("KEK_GENERATE_FAILED").Wrap(err)
	}
	return oops.Wrap(src.Persist(ctx, master))
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `task test -- -run TestEnsureKeyfile ./cmd/holomush/`
Expected: PASS.

- [ ] **Step 5: Commit**

`jj commit -m "feat(crypto): auto-generate KEK file on first boot, never regenerate (holomush-5rh.8.29.12)"`

### Task 7: Require KEK at boot — wire provisioner + make the error fatal + `--auto-gen-kek`

**Files:**

- Create: `cmd/holomush/kek_provision.go` (add `provisionBootKEKProvider` next to `resolvePassphrase`/`ensureKeyfile` from Tasks 5-6)
- Modify: `cmd/holomush/core.go:745-749` (call the boot provisioner; fatal instead of warn-degrade)
- Modify: `cmd/holomush/root.go` (`--auto-gen-kek` flag → thread to `core.go`)
- **Do NOT modify** `cmd/holomush/cmd_admin_totp_deps.go::buildKEKProviderFromConfig` — it is the admin-TOTP CLI path and MUST keep requiring an existing keyfile + env passphrase (an admin command operating on a running server must never auto-gen a fresh KEK). The boot path gets its own provisioner; the two only share the `kek.NewFileSource`/`NewLocalAEADProvider` tail.

- [ ] **Step 1: Add `provisionBootKEKProvider` (new boot-path function; admin path untouched)**

In `cmd/holomush/kek_provision.go`, add a NEW function (do not touch `buildKEKProviderFromConfig`):

```go
// provisionBootKEKProvider builds the KEK provider for SERVER BOOT: it resolves
// the passphrase (env / file-ref / prompt), auto-generates the sealed keyfile when
// absent and autoGen is set, then loads it. Distinct from buildKEKProviderFromConfig
// (the admin-CLI path, which requires a pre-existing keyfile and never auto-gens).
func provisionBootKEKProvider(ctx context.Context, pool *pgxpool.Pool, autoGen bool) (kek.Provider, error) {
	keyFile := os.Getenv(envKEKFile)
	if keyFile == "" {
		return nil, oops.Code("BOOT_KEK_FILE_MISSING").With("env_var", envKEKFile).
			Errorf("%s is required", envKEKFile)
	}
	pass, err := resolvePassphrase(passphraseSources{interactive: term.IsTerminal(int(os.Stdin.Fd()))})
	if err != nil {
		return nil, err // KEK_PASSPHRASE_UNAVAILABLE / KEK_PASSPHRASE_FILE_READ_FAILED
	}
	pf := func(context.Context) ([]byte, error) { return pass, nil } // capture; don't re-resolve
	if err := ensureKeyfile(ctx, keyFile, pf, autoGen); err != nil {
		return nil, err // KEK_FILE_NOT_FOUND when absent && !autoGen
	}
	source, err := kek.NewFileSource(keyFile, pf)
	if err != nil {
		return nil, oops.Code("BOOT_KEK_FILE_SOURCE_FAILED").Wrap(err)
	}
	provider, err := kek.NewLocalAEADProvider(ctx, source, pool) // calls Load internally; file now exists
	if err != nil {
		return nil, oops.Code("BOOT_KEK_PROVIDER_FAILED").Wrap(err)
	}
	return provider, nil
}
```

Resolving the passphrase up-front (before `ensureKeyfile`) makes refuse-to-boot fire before any keyfile I/O. The `pf` closure captures the resolved bytes — it must NOT call `resolvePassphrase` itself (that would swallow the error and re-resolve per call).

- [ ] **Step 2: Make the boot error fatal (leave the downstream guards)**

In `core.go:745-749`, replace the warn-and-degrade (`slog.WarnContext(...); kekProvider = nil`) with a hard return that calls the new boot provisioner:

```go
kekProvider, kekErr := provisionBootKEKProvider(ctx, dbSub.Pool(), autoGenKEK)
if kekErr != nil {
	return oops.Code("BOOT_KEK_REQUIRED").
		Errorf("a KEK is required to start: %w (set %s + a passphrase source, or pass --auto-gen-kek)", kekErr, envKEKFile)
}
```

Do **NOT** touch the downstream code. `kekProvider` is now guaranteed non-nil, so the existing `if kekProvider != nil` gate at `core.go:753` (guarding the TOTP/admin block, lines 753-799) and the `kekProvider != nil` log attribute at `:872` simply always take the present branch — harmless, left as-is. (`:851` `KEKProvider: kekProvider` is a struct field, not a branch.) Removing those guards is an optional, out-of-scope simplification; this task only makes the *absence* fatal. `autoGenKEK` is the value read from the cobra flag (Step 3), threaded into `runCoreWithDeps` the same way other CLI options reach `core.go`.

- [ ] **Step 3: Add the `--auto-gen-kek` flag**

Use the same vehicle as the other boolean run flags (`skip-seed-migrations`, `reset-setting`): add an `AutoGenKEK` field to `coreConfig`, register it on the run command, and thread it into the provisioner:

```go
// coreConfig struct field
AutoGenKEK bool `koanf:"auto_gen_kek"`

// run-command flag registration
cmd.Flags().BoolVar(&cfg.AutoGenKEK, "auto-gen-kek", false,
    "generate a KEK file if absent on first boot (passphrase still required)")

// inside runCoreWithDeps
kekProvider, kekErr := provisionBootKEKProvider(ctx, dbSub.Pool(), cfg.AutoGenKEK)
```

Do NOT use a bare `PersistentFlags().Bool(...)` — it does not bind into the koanf-loaded `coreConfig`, so the value would never reach the provisioner. No package global.

- [ ] **Step 4: Write the boot-matrix test**

```go
func TestProvisionBootKEKProviderBootMatrix(t *testing.T) {
	t.Run("no passphrase, non-interactive ⇒ KEK_PASSPHRASE_UNAVAILABLE", func(t *testing.T) {
		t.Setenv("HOLOMUSH_KEK_FILE", filepath.Join(t.TempDir(), "m.key.enc"))
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE", "")
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE_FILE", "")
		_, err := provisionBootKEKProvider(context.Background(), nil, true)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "KEK_PASSPHRASE_UNAVAILABLE")
	})
	t.Run("passphrase set, keyfile absent, autoGen off ⇒ KEK_FILE_NOT_FOUND", func(t *testing.T) {
		t.Setenv("HOLOMUSH_KEK_FILE", filepath.Join(t.TempDir(), "m.key.enc"))
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE", "pw")
		_, err := provisionBootKEKProvider(context.Background(), nil, false)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "KEK_FILE_NOT_FOUND")
	})
	t.Run("no KEK file path ⇒ BOOT_KEK_FILE_MISSING", func(t *testing.T) {
		t.Setenv("HOLOMUSH_KEK_FILE", "")
		t.Setenv("HOLOMUSH_KEK_PASSPHRASE", "pw")
		_, err := provisionBootKEKProvider(context.Background(), nil, true)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "BOOT_KEK_FILE_MISSING")
	})
}
```

All three cases return **before** `kek.NewLocalAEADProvider` (which needs a real pool), so `pool=nil` is safe here — they fire at the file-path / passphrase / keyfile checks. The happy path (auto-gen → provider) needs a DB pool and is covered by the integration harness (Tasks 8/10), not this unit test.

- [ ] **Step 5: Run + build**

Run: `task test -- -run 'TestProvisionBootKEKProviderBootMatrix' ./cmd/holomush/` then `task build`
Expected: PASS; build green.

- [ ] **Step 6: Commit**

`jj commit -m "feat(crypto): require a KEK to boot; auto-gen via --auto-gen-kek (holomush-5rh.8.29.12)"`

---

## Phase 4: Blast radius — harness, E2E, docs

### Task 8: Provision a KEK across the test/dev surface

**Files:**

- Modify: `internal/testsupport/integrationtest/*.go` (the Start/boot helper)
- Modify: `compose.e2e.yaml`, dev compose / `Taskfile` `dev` target
- Modify: `site/src/content/docs/operating/*` (KEK provisioning + passphrase sources + `--auto-gen-kek`)

- [ ] **Step 1: Make the integration harness boot with a KEK**

In the harness boot, set `HOLOMUSH_KEK_FILE` to a temp path, `HOLOMUSH_KEK_PASSPHRASE` to a fixed test value, and enable auto-gen. Confirm `integrationtest.Start(t, ...)` still returns a running stack.

Run: `task test:int -- ./test/integration/crypto/...`
Expected: PASS (suites boot with crypto active).

- [ ] **Step 2: E2E + dev compose**

Confirm `compose.e2e.yaml` sets the KEK env (the 29.10 WIP already does for crypto e2e — generalise it to the standard service). Add the dev passphrase env + `--auto-gen-kek` to the `task dev` invocation.

Run: `task test:e2e` (smoke) — Expected: stack boots.

- [ ] **Step 3: Docs**

Update the operating guide: KEK is now required; the three passphrase sources; `--auto-gen-kek` first-start; the no-regenerate guarantee.

Run: `task lint:markdown` — Expected: PASS.

- [ ] **Step 4: Commit**

`jj commit -m "chore(crypto): provision KEK across harness/compose/dev; doc KEK requirement (holomush-5rh.8.29.12)"`

---

## Phase 5: Invariants + integration proof

### Task 9: Register the three invariants

**Files:**

- Modify: `docs/architecture/invariants.yaml` (CRYPTO scope — allocate next free Ns; consult existing first)
- Generate: `go run ./cmd/inv-render`

- [ ] **Step 1: Add entries** (allocate the next free `INV-CRYPTO-N`s; consult existing CRYPTO entries first). Map each to its genuinely-asserting test and add `// Verifies: INV-CRYPTO-N` above that test:
  - **Single-gate** (crypto active ⟺ KEK present) → `TestBuildCharacterIdentity` (Task 2) + `TestCoreServerCryptoActiveTracksKEKPresence` (Task 1).
  - **Boot-refusal** (no passphrase ⇒ no boot) → `TestProvisionBootKEKProviderBootMatrix` (Task 7).
  - **Creation-time binding guarantee** → `TestCreateCharacterMintsBindingResolvableByCurrent` + `TestCreateGuestMintsBinding` (Task 3).

  All three target concrete, non-placeholder tests, so `binding: bound` is honest (no `Skip`/empty body — `TestBoundInvariantsAreGenuinelyAsserted` will confirm).

- [ ] **Step 2: Regenerate + verify**

Run: `go run ./cmd/inv-render` then `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestBoundInvariantsAreGenuinelyAsserted|TestProvenanceGuard' ./test/meta/`
Expected: PASS.

- [ ] **Step 3: Commit**

`jj commit -m "docs(invariants): register INV-CRYPTO single-gate, boot-refusal, binding-guarantee (holomush-5rh.8.29.12)"`

### Task 10: Integration — decrypt-to-participant, no metadata-only-to-participant

**Files:**

- Modify/Test: `test/integration/crypto/*_test.go` (KEK-provisioned harness from Task 8)

- [ ] **Step 1: Write the load-bearing integration test**

```go
//go:build integration
// Verifies: with KEK present, a focused scene participant + a comms recipient
// receive decrypted plaintext (MetadataOnly()==false); a non-participant gets
// metadata-only. Proves the publish/subscribe asymmetry is gone.
```

Drive a scene pose and a comms whisper through the real stack; assert `delivery.MetadataOnly()` is false for the participant/recipient and the payload equals plaintext, true for a non-participant.

- [ ] **Step 2: Run**

Run: `task test:int -- ./test/integration/crypto/...`
Expected: PASS.

- [ ] **Step 3: Commit**

`jj commit -m "test(crypto): integration proof of decrypt-to-participant under live crypto (holomush-5rh.8.29.12)"`

---

## Post-Implementation Checklist

- [ ] `task pr-prep` green (fast lane); `task pr-prep:full` if int/e2e surface touched.
- [ ] crypto-reviewer (FIRST) → code-reviewer before push.
- [ ] Tracked as a downstream bead dependency (NOT a gate of this PR): the 29.10 E2E
  (`uvqsnqzn`, workspace `crypto-golive-impl`) rebases onto this work and goes green *after*
  this lands. Wire `bd dep add holomush-5rh.8.29.10 <this-epic>` so the ordering is explicit;
  do not block this PR's `pr-prep` on the other workspace.
- [ ] Spec §3.1's KEK-less-degraded clause amended (or superseded) to reflect the mandatory-KEK posture.
- [ ] `bd close` each child; `bd dolt push`.
<!-- adr-capture: sha256=ac8d72c7716b3e4e; session=cli; ts=2026-06-09T17:57:32Z; adrs=holomush-gkw77,holomush-kddop -->
