<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Sensitive-Event Crypto Activation — Design

- **Bead:** holomush-5rh.8.29.12
- **Status:** design (brainstorming → design-reviewer gate)
- **Predecessor:** [2026-06-08 sensitive-event crypto production go-live](2026-06-08-sensitive-event-crypto-production-golive-design.md) (PR #4409, merged) wired the DEK/AuthGuard/audit machinery onto the live publisher and subscriber, **gated on KEK presence**, and shipped it **dormant**.

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, **MAY** are per
RFC 2119 / RFC 8174 (root `CLAUDE.md`).

---

## 1. Problem

PR #4409 landed the crypto wiring but left it inert, and uncovered two correctness
gaps plus one deployment-posture decision:

1. **Gate asymmetry.** The live **publisher** encrypts on `event.Sensitive && p.dekMgr != nil`
   (`internal/eventbus/publisher.go:209`, the encrypting case of the codec switch) — gated
   purely on KEK presence
   (`RekeyManager != nil`). The live **subscriber** builds the authenticated
   `sessionIdentity` only when `s.bindings != nil && s.cryptoEnabled`
   (`internal/grpc/server.go:995`, and the identical guard at
   `internal/grpc/query_stream_history.go:306`), where `cryptoEnabled` is a vestigial flag
   (`server.go:178`) **set true nowhere in production**. Consequence: a deployment
   with a KEK present but `cryptoEnabled` false would **encrypt on publish** while the
   subscriber passes a zero `IdentityKindUnknown` identity to an active AuthGuard →
   `DenyUnknownIdentityKind` → **metadata-only delivery to every reader, including
   participants**. The two gates MUST be one.

2. **Binding-row precondition.** When the subscriber identity *is* built, it calls
   `bindings.Current(charID)` and hard-fails `SUBSCRIBE_BINDING_LOOKUP_FAILED` for a
   character with no current binding row. The binding is minted atomically at character /
   guest creation (`auth_handlers.go:519`), so this holds in a fully-wired deployment; the
   residual risk is the no-binding fallback path (`auth_handlers.go:520`, taken when the
   transactor/binding repo is unwired). Pillar 2 makes that guarantee **explicit and tested**
   rather than incidental.

3. **KEK is never required.** The merged spec (§3.1) made a KEK-less deployment a
   *supported degraded posture that MUST NOT fail to start*. That means sensitive
   features (scenes `scene_pose/say/emit/ooc`, comms `page/whisper/pemit`) can run
   **in the clear** with no signal. There is no way to *require* a KEK, and the manual
   `kek init` provisioning step is friction that makes "just run without crypto" the
   path of least resistance.

## 2. Goals / Non-goals

**Goals**

- Make **KEK presence the single activation gate** for sensitive-event crypto across
  publish and subscribe; eliminate the asymmetry by construction.
- Confirm — via tests, not new behaviour — that every character resolves a current binding
  through the existing creation-time minting (no read-path create, no backfill); keep
  `bindings.Current`'s hard-fail as a defensive assertion.
- Make a provisioned KEK **mandatory to boot**, while keeping provisioning **frictionless**
  (auto-generate the keyfile; never the passphrase).
- Keep the KEK **operator-supplied** (the software never ships or invents key-unlock
  material).

**Non-goals**

- Provisioning a KEK *for* any operator/deployment (impossible by design — the operator
  supplies the passphrase).
- Key rotation / rekey UX (already shipped; unchanged here).
- External/clustered NATS (out of scope everywhere — holomush-s5ts).
- Multi-KEK / per-tenant KEK hierarchies.

## 3. Design

Three separable pillars. They SHOULD be materialized as child beads of
holomush-5rh.8.29.12 (see §7), sequenced 1 → 2 → 3.

### 3.1 Pillar 1 — Single gate: KEK presence

The identity-build gate MUST key off **KEK presence (`RekeyManager != nil`)** — the same
signal that wires the publisher's DEK manager and the subscriber's AuthGuard
(the `if s.cfg.RekeyManager != nil` guard, `cmd/holomush/sub_grpc.go:350`) — instead of the
vestigial `cryptoEnabled` flag, which is
never set true in production (`WithCryptoEnabled`, `internal/grpc/auth_handlers.go:156`, is
uncalled on the prod path).

**Mechanism (corrected after design-review round 1).** `s.bindings` is **not** a
KEK-presence signal: `WithBindingRepository` is wired **unconditionally**
(`cmd/holomush/sub_grpc.go:418`), because guest-session construction needs the binding
repository regardless of crypto (`auth.NewGuestService(..., bindingRepo)`,
`sub_grpc.go:274-281`). So the gate MUST NOT collapse to
`if s.bindings != nil` — that would activate binding lookup on KEK-less boots and hard-fail
`SUBSCRIBE_BINDING_LOOKUP_FAILED` (the very failure mode §1 #2 names). Instead, the server's
activation flag (today `cryptoEnabled`, `server.go:178`; SHOULD be renamed `cryptoActive`
for honesty) MUST be **wired from `RekeyManager != nil` at construction** in
`cmd/holomush/sub_grpc.go`. The guard stays `if s.bindings != nil && s.cryptoActive` and MUST
be applied at **both** identity-build sites, which today diverge only because the flag is
never set:

- `internal/grpc/server.go:995` (Subscribe), and
- `internal/grpc/query_stream_history.go:306` (QueryStreamHistory) — the identical guard,
  which MUST be changed in lockstep or history reads keep the asymmetry.

This is "wire the gate correctly," **not** "delete the gate": deleting it and running binding
lookup unconditionally breaks KEK-less boots (and, pre-Pillar-3, every deployment). Once
Pillar 3 makes a KEK mandatory, `cryptoActive` is always true at runtime, so the flag becomes
a defensive invariant rather than a behavioural switch; a later cleanup MAY then remove it
(cf. the plugin-side `WithCryptoEnabled` deletion in `5992153` / #4399, which dropped a
likewise-vestigial gate once the sensitivity fence was unconditional). Removal is explicitly
out of scope here — Pillar 1 only converges the gate onto the one signal.

**Result:** with a KEK present, the publisher encrypts AND the subscriber builds a real
character identity (Subscribe **and** history) AND the AuthGuard permits the participant →
decrypt-to-participant. The publish/subscribe asymmetry is gone because both derive from the
single KEK-presence signal.

### 3.2 Pillar 2 — Prove the creation-time binding guarantee

A character's binding is **already minted at creation time**, not lazily on the read path:
`createCharacterAtomic` (`internal/grpc/auth_handlers.go:519`) creates the character and its
binding (`bindings.Create(playerID, charID, "initial_bind")`) in one transaction whenever
`transactor != nil && bindings != nil`; guest creation does the same with
`"initial_bind_guest"` (`internal/auth/guest_service.go`). Both the transactor and the
binding repository are wired **unconditionally** (`cmd/holomush/sub_grpc.go:418`), so in any
real deployment every character — normal or guest — has a current binding the moment it
exists. `dek.Manager.GetOrCreate` and the Subscribe identity-build both only **resolve** that
binding via `bindings.Current`; neither creates one.

The only way `bindings.Current` returns `BINDING_NOT_FOUND` is a character created via the
no-binding fallback (`auth_handlers.go:520-524`, taken when `transactor`/`bindings` is nil).
Once Pillar 3 makes a KEK mandatory and bindings are unconditionally wired, that fallback is
unreachable in production, and there are no pre-existing users to have legacy binding-less
characters (master spec §2491).

Therefore Pillar 2 is **verification, not new behaviour**:

- Tests MUST prove the binding is minted on **every** character-creation path — the normal
  `createCharacterAtomic` transactional path AND the guest path — and that a created
  character's `bindings.Current` resolves.
- The `bindings.Current` hard-fails — `SUBSCRIBE_BINDING_LOOKUP_FAILED` (Subscribe) and
  `HISTORY_BINDING_LOOKUP_FAILED` (history), plus `DEK_BINDING_RESOLVE_FAILED` on the
  publisher's participant-`Add` path (`manager.go`, a distinct call site) — MUST be **kept as
  defensive assertions**: they should never fire once KEK is mandatory, and a fired error is
  the correct loud signal of a misconfiguration rather than a silent metadata-only degradation.
- No lazy create-on-read, no backfill job, no migration (all would be dead code given the
  creation-time guarantee and the empty user base — YAGNI).

### 3.3 Pillar 3 — KEK required + frictionless provisioning

**Requirement.** A server MUST refuse to boot unless it can obtain an unlock passphrase
(see sources below). This **removes the KEK-less degraded posture** of the predecessor
spec §3.1 — crypto is always active. (§4 covers the blast radius.)

**Frictionless provisioning.** On boot, given a passphrase:

- If the configured keyfile **exists**, load it (current behaviour).
- If the keyfile is **absent**, the server MUST **auto-generate** a master KEK, seal it
  with the passphrase (`kek.FileSource.Persist`, Argon2id per master spec §5.3), persist
  it to the configured path, and reuse it on subsequent boots. It MUST NOT regenerate when
  a keyfile is present (regeneration would orphan every prior DEK and make historical
  events undecryptable).
- Headless first-start uses a `--auto-gen-kek` flag (or equivalent config) so the
  auto-generate path runs without a TTY. Interactive first-start MAY prompt
  ("no keyfile found at <path>; generate one now? [y/N]").

**Passphrase sources** (the operator-supplied secret; never auto-generated, never stored
beside the keyfile — this preserves the at-rest separation that makes the sealed keyfile
safe on disk/in backups). The `PassphraseFunc` seam (`kek/source_file.go:39`) already
abstracts this. Resolution order, first hit wins:

1. **Env var** — `HOLOMUSH_KEK_PASSPHRASE` (existing).
2. **File ref** — `HOLOMUSH_KEK_PASSPHRASE_FILE` pointing at a file whose contents are the
   passphrase (Docker/systemd/k8s secret-mount idiom). The file MUST be read at boot; its
   contents MUST NOT be logged.
3. **Interactive prompt** — only when a TTY is attached and neither env nor file-ref is set.

If none of the three yields a passphrase, the server MUST exit non-zero with a clear
diagnostic (it MUST NOT boot, and MUST NOT auto-generate a passphrase).

**What is auto-generated vs. operator-supplied:**

| Artifact | Source | Stored where |
| --- | --- | --- |
| Master KEK (keyfile) | Auto-generated if absent | Configured keyfile path (sealed) |
| Unlock passphrase | **Operator only** (env / file-ref / prompt) | **Never persisted by the server** |

## 4. Blast radius (explicit)

Making KEK mandatory reverses the predecessor spec's "KEK-less MUST NOT fail to start" and
touches every boot path:

- **Predecessor spec** (§3.1) MUST be amended (or superseded in the relevant clause) to
  record that the KEK-less degraded posture is retired.
- **Integration harness** `internal/testsupport/integrationtest` and any
  `sessiontest`/boot helpers MUST provision a KEK (auto-gen keyfile + a fixed test
  passphrase env) so suites still start.
- **E2E**: `compose.e2e.yaml` already provisions a test KEK in the 29.10 WIP; the change
  generalises that to the standard harness.
- **Local dev / `task dev`** and any docker-compose dev stack MUST set a dev passphrase
  (and rely on `--auto-gen-kek`).
- **Docs**: operating guide (KEK provisioning, the new passphrase sources, the `--auto-gen-kek`
  flag) MUST be updated.

This surface is the cost of the guarantee; the auto-generate path is what keeps it from
being painful.

## 5. Invariants

Register in `docs/architecture/invariants.yaml` (CRYPTO scope; consult existing entries,
do not renumber):

- **Single-gate guarantee** — sensitive-event crypto activates iff a KEK is present; there
  is no second flag that can desynchronise publish from subscribe. (Asserted by a
  gate truth-table test + an integration test proving no metadata-only-to-participant when
  KEK present.)
- **Boot-refusal guarantee** — a server with no obtainable passphrase MUST NOT boot.
  (Asserted by a boot-matrix unit test.)
- **Creation-time binding guarantee** — every character-creation path (normal
  `createCharacterAtomic` and guest) mints a current binding, so `bindings.Current` resolves
  for any character that exists in a fully-wired deployment. (Asserted by Pillar 2's
  creation-path tests.)

Each MUST ship `binding: bound` with a genuinely-asserting test, or `binding: pending` with
a filed coverage bead — never a fabricated binding (`.claude/rules/invariants.md`).

## 6. Testing

- **Unit.** Gate truth table (KEK present/absent × bindings wired) → identity built or not.
  Boot-refusal matrix: {keyfile present/absent} × {passphrase env/file-ref/none} ×
  {`--auto-gen-kek` on/off} → boot / auto-gen / refuse. Creation-time binding minting on
  both the normal and guest paths (then `bindings.Current` resolves). Passphrase file-ref
  reader (trailing-newline trim, missing file, unreadable, never logged).
- **Integration** (`test/integration/crypto`, now KEK-provisioned). The load-bearing
  assertion: with KEK present, a focused scene participant and a comms recipient receive
  **decrypted plaintext** (`MetadataOnly() == false`), and a non-participant receives
  metadata-only — proving the asymmetry is gone.
- **E2E.** The 29.10 WIP (KEK provisioning + live PoseCard, change `uvqsnqzn` in workspace
  `crypto-golive-impl`) is the acceptance E2E; it unblocks once Pillars 1–3 land.

## 7. Decomposition

holomush-5rh.8.29.12 SHOULD become a small epic with one child per pillar:

1. **Gate alignment** — wire the activation flag (`cryptoActive`) from `RekeyManager != nil`
   and apply the guard at both `server.go:995` (Subscribe) and `query_stream_history.go:306`
   (QueryStreamHistory); KEK-presence becomes the sole gate. (Flag removal is out of scope.)
2. **Binding-guarantee verification** — tests proving creation-time binding minting on the
   normal and guest paths; keep `bindings.Current`'s hard-fail as a defensive assertion (no
   new create-on-read, no backfill).
3. **KEK-required boot + auto-gen + passphrase sources** — incl. the harness/compose/docs
   blast-radius updates.

Dependency: 3 depends on 1+2 (mandatory KEK is only correct once the read path is correct).
The 29.10 E2E depends on all three.

## 8. Risks & sequencing

- **Reversing a merged decision.** §4's blast radius is the main risk; mitigated by the
  auto-generate path and a single coordinated change to the boot helpers.
- **Regeneration footgun.** Auto-generate MUST be strictly absent-keyfile-only; a guard
  test MUST prove a present keyfile is never overwritten.
- **Passphrase leakage.** The file-ref and prompt paths MUST NOT log passphrase contents;
  covered by a test and a code-review focus.
- **Review gates.** crypto-reviewer (FIRST) → code-reviewer before push, per the predecessor
  spec's discipline.
<!-- adr-capture: sha256=677d496f4e3a0749; session=cli; ts=2026-06-09T17:37:36Z; adrs=holomush-gkw77,holomush-kddop -->
