<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Phase 5 Sub-Epic D — OperatorAuthProvider + Dual-Control + `crypto.policy_set` Chain — Design

## Status

Draft — pending `design-reviewer` READY verdict before `superpowers:writing-plans`.

## Authors

- Sean Brandt
- Claude (Opus 4.7 1M)

## Date

2026-05-09

## Context

Sub-epic D is the integration layer of Phase 5 of the event-payload-cryptography
roadmap. The three Tier-1 substrates have all merged (`holomush-jxo8.3` TOTP,
`holomush-jxo8.5` `crypto.operator` capability, `holomush-jxo8.4` UDS admin
socket). D unifies them into a server-side `OperatorAuthProvider`, ships the
`admin_approvals` table that backs dual-control, and lands the hash-chained
`crypto.policy_set` audit-event substrate plus its fail-closed startup
verifier. D also becomes the production audit emitter for four of sub-epic A's
five reserved `crypto.totp_*` events (per A's spec §"Audit events emitted" /
"Emission ownership and the host-shell-CLI gap" — A's `Service` is PG-only and
does not emit; emission is the calling layer's responsibility).

This spec inherits all decisions from the Phase 5 decomposition spec
(`docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-decomposition.md`),
particularly Decision 5 (dual-control via server-issued approval token),
Decision 6 (two-layer dual-control: per-invocation flag + per-site config),
and Decision 7 (hash-chained `crypto.policy_set` audit events). Where this
spec amends the master crypto spec
(`docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`), the
amendments are listed in §10 below.

### Project context

D unblocks sub-epic E (Rekey lifecycle, `holomush-jxo8.7`) and sub-epic F
(`AdminReadStream` server + CLI, `holomush-jxo8.8`). Both depend on D's
`OperatorAuthProvider`, session store, and `admin_approvals` repo. D itself
also ships the `holomush admin totp reset` CLI deferred from sub-epic A and
the `holomush admin approve` second-op CLI required by Decision 5.

D has zero external API consumers. There is no backward-compat requirement;
no schema-shape preserved for older clients; no deprecation window for the
provisional `OperatorAuthProvider` shape master spec §5.9 sketched.

### Out of scope (non-goals)

- **Rekey lifecycle and CLI** — sub-epic E (`holomush-jxo8.7`).
  D ships only the auth + approval substrate Rekey consumes.
- **`AdminReadStream` server and CLI** — sub-epic F (`holomush-jxo8.8`).
  D ships only the auth + approval substrate the stream consumes.
- **Hot-reload of `crypto.dual_control_required`** — deferred per
  Decision 6 of the decomposition spec. v1 requires server restart to take
  effect; the restart emits a new `policy_set` chain event.
- **Plan-level chain integrity for non-`policy_set` audit events** — only
  `events.<game>.system.crypto_policy.<policy_name>` is hash-chained in this
  spec. Other audit subjects retain their existing
  `events_audit`-byte-equality-with-JetStream durability story.
- **`crypto.totp_bootstrap_completed` emitter** — no Phase-5 caller (sub-epic
  A's host-shell CLI is the only bootstrap path; D's `OperatorAuthProvider`
  consumes only `Service.Verify`). Subject is reserved for future server-side
  bootstrap flows.
- **`crypto.totp_enrolled` emitter from D** — D's decorator wrapping
  `totp.Service` covers `Enroll` / `BootstrapEnroll`, but no Phase-5 server
  code path calls those methods through the decorator. Future enrollment-via-
  server flows will wire through it and emit transparently.
- **`@cluster status` and `@evict-member` admin commands** — `holomush-jxo8.1`,
  P3 follow-up.
- **In-game grant UX for `crypto.operator`** — P3 follow-up filed at sub-epic B
  landing.

## Section 1 — Threat model

D inherits the master spec §1 threat model and the decomposition spec
Decision 6 layered defense story (master-spec row 134 "curious operator with
shell access" vs row 137 "compromised in-game admin with `crypto.operator`
capability without shell"). D's contribution to the defense profile:

| Adversary action | D's defense |
|---|---|
| Row-134 (shell access) replays a Phase-3 captured TOTP code | Sub-epic A's `last_used_step` rejects it. Authenticate fails. (TOTP is per-step single-use; D does not weaken this.) |
| Row-134 steals an issued session token from process memory | Bounded by 10-min TTL. Localhost-UDS topology means the token is never on the wire outside the host. |
| Row-134 forges a session token | Tokens are server-issued ULIDs in an in-memory map; forgery requires guessing a valid ULID (122 bits of entropy) within 10 min. Not a real attack vector. |
| Row-134 silently flips `dual_control_required` config on disk | Restart emits a new `policy_set` chain event; round-trip flips visible in `events_audit`; per-invocation `policy_hash` pins the policy that was effective at action time. |
| Row-134 tampers with `events_audit` chain | Existing JetStream byte-equality / cold-tier integrity check (master spec §4.7, §8.1) detects raw row tampering. The hash chain detects logical tampering: silent drop of a `policy_set` event or replay with deleted history. |
| Row-137 (auth-tier compromise without shell) bypasses topology | Localhost-UDS denies reach (topological lockout). Once reach is achieved, defense is creds + TOTP via D's check sequence. |
| Row-137 bypasses dual-control on a `dual_control_required` op | Server-side enforcement of site policy — D's Authenticate / E's RekeyHandler reject the single-control invocation with `DENY_DUAL_CONTROL_REQUIRED` regardless of operator-supplied flags. |

What D does NOT defend against (out of master-spec threat model):

- An adversary with PG superuser AND host shell who edits both
  `events_audit` and JetStream consistently. Root-on-host is the trust path.
- A new install with a fresh database — no prior chain to verify against.
  The first `policy_set` event after `migrate up` has `prev_hash IS NULL`
  (genesis).

## Section 2 — Invariants

D introduces 19 numbered invariants. Each MUST be backed by at least one
named test (unit, integration, or e2e). The Section 8 test strategy lists
the test names.

### Authentication invariants

| ID | Invariant | Test |
|---|---|---|
| INV-D1 | `OperatorAuthProvider.Authenticate`'s 6-step check sequence is order-fixed: ValidateCredentials → IsEnrolled → Verify → HasPlayerGrant → PlayerHasRole(RoleAdmin) → PeerCred capture. Later steps MUST NOT be reached on earlier failure. (Avoids leaking which step failed beyond the typed DENY code.) | `TestAuthenticateStepOrderFixedOnFailure` |
| INV-D2 | A session token MUST have a 10-minute TTL from issuance; `SessionStore.Get` MUST reject any token whose deadline has passed and emit `DENY_SESSION_EXPIRED`. | `TestSessionStoreRejectsExpiredToken` |
| INV-D3 | The session map is per-process in-memory; restart loses all sessions by design. Operators re-authenticate. (Documented operational property — not a security claim.) | `TestSessionStoreEmptiedOnConstruction` |
| INV-D4 | `PeerCred` captured by sub-epic C's middleware is recorded in `OperatorIdentity.OSUser` for audit only. It MUST NOT be consulted in any DENY decision. | `TestAuthenticateIgnoresPeerCredForGating` |
| INV-D15 | Authenticate MUST reject any request whose verified `player_id` is not in the `crypto.operators` allow-list with `DENY_NOT_OPERATOR`. | `TestAuthenticateRejectsNonOperator` |
| INV-D16 | `ResetTOTP` and `Approve` RPC handlers MUST re-assert the capability check AND the role check on the resolving session's identity (defense in depth against grant or role revocation mid-session). | `TestResetTOTPRequiresCapabilityOnHandler`, `TestApproveRequiresCapabilityOnHandler`, `TestResetTOTPRequiresAdminRoleOnHandler` |
| INV-D19 | Authenticate MUST reject any request whose verified `player_id` does not have at least one character with the `admin` role (`DENY_NOT_ADMIN_ROLE`). The `crypto.operator` capability is *narrowing* on top of `RoleAdmin` (master spec §5.9.1, sub-epic B `internal/access/grants.go:14-18` — "MUST be combined with `RoleAdmin`"). Decomposition spec Decision 5 line 177 makes the conjunction explicit. | `TestAuthenticateRejectsPlayerWithoutAdminRole` |

### Dual-control invariants

| ID | Invariant | Test |
|---|---|---|
| INV-D5 | `admin_approvals.expires_at = created_at + 5 min`. Read paths MUST filter `expires_at < now()` so expired rows are invisible. (No background reaper in v1; rows are tiny.) | `TestApprovalRepoReadFiltersExpired` |
| INV-D6 | Second-op `MarkApproved` MUST reject any request where the second-op `player_id` equals the row's `primary_player_id` (`DENY_DUAL_CONTROL_SELF`). | `TestMarkApprovedRejectsSelfApproval` |
| INV-D7 | The Approve RPC handler MUST verify the second-op's identity holds `crypto.operator` (already enforced at Authenticate, but re-checked here as defense in depth — see INV-D16). | covered by INV-D16 test |
| INV-D8 | `op_args_hash` MUST be computed as `SHA-256(proto.MarshalOptions{Deterministic: true}.Marshal(args))`. Any code path that produces or verifies the hash MUST agree on this exact algorithm. Cross-binary stability is load-bearing on the protobuf-go version pin (INV-D18). | `TestOpArgsHashAlgorithmStableAgainstGolden` |
| INV-D9 | When the primary proceeds with the operation post-approval, the server MUST recompute `op_args_hash` from the proceeding-call's args; mismatch → `DENY_APPROVAL_ARGS_MISMATCH`. (Defended via E/F's handlers; D ships the algorithm.) | `TestOpArgsHashMismatchRejected` |

### Chain integrity invariants

| ID | Invariant | Test |
|---|---|---|
| INV-D10 | The `crypto.policy_set` chain genesis condition is `prev_hash IS NULL`. The first event for a `policy_name` in a fresh `events_audit` MUST have `prev_hash = null`; subsequent events MUST have `prev_hash` equal to their predecessor's `policy_hash`. | `TestPolicySetGenesisHasNullPrevHash`, `TestPolicySetSubsequentReferencesPredecessor` |
| INV-D11 | The startup chain verifier MUST be fail-closed: any mismatch (broken predecessor, wrong `policy_hash`, missing `prev_hash` when non-null was expected) → server refuses to start. (Matches INV-32/33/37 fail-closed pattern in master spec §10.) | `TestVerifierRefusesStartOnBrokenChain` |
| INV-D12 | `policy_hash = SHA-256(JCS_canonicalize(payload_without_policy_hash_field))`. The `policy_hash` field itself is excluded from canonicalization input. | `TestPolicyHashComputedOverPayloadMinusHashField` |
| INV-D13 | The JCS canonicalizer is pinned in `go.mod` to a specific pseudo-version (v1: `github.com/cyberphone/json-canonicalization v0.0.0-20241213102144-19d51d7fe467`). A meta-test asserts the pin so a silent dependency bump cannot land. Switching libraries or RFC interpretations is a chain-breaking change and MUST be a master-spec amendment per the §4.6 amendment row. | `TestJCSCanonicalizationLockedToVendoredImpl` |

### Audit emission invariants

| ID | Invariant | Test |
|---|---|---|
| INV-D14 | The `AuditingService` decorator emits exactly once per observed state transition (locked, recovery-consumed, cleared). Publish failure does NOT roll back the inner `Service` PG state — the operation succeeded; emission is informational. Failure is logged via `slog.Warn` with structured fields. (Distinct from F's INV-42 hard-required pre-emit gate for `AdminReadStream`.) | `TestAuditingServiceEmitsOnceOnTransition`, `TestAuditingServiceLogsAndContinuesOnPublishError` |
| INV-D17 | `CryptoPolicySubsystem.EmitCurrentSnapshot` MUST fail-closed on Publish error (return error from `Subsystem.Start`; server refuses to start). Distinct from INV-D14 because chain integrity is a security claim, not informational audit — a dropped `policy_set` event would silently break chain continuity on the next boot. | `TestCryptoPolicySubsystemFailsStartOnPublishError` |
| INV-D18 | The `google.golang.org/protobuf` module MUST be pinned in `go.mod` to a specific version. A meta-test asserts the pin so a silent dependency bump cannot land. Reasoning: `proto.MarshalOptions{Deterministic: true}` is documented stable within a binary version but NOT guaranteed stable across protobuf-go releases. Cross-process `op_args_hash` agreement (INV-D8/D9) and the meaning of `proto.MarshalDeterministic` are chain-of-custody load-bearing on this pin in the same way INV-D13 locks JCS. | `TestProtoDeterministicMarshalLockedToVendoredProtobuf` |
| INV-D20 | The verifier MUST distinguish "first boot, no chain yet" from "chain existed and was truncated" via a persistent chain-init signal stored in `bootstrap_metadata` (key = `crypto.policy_chain_initialized.<policy_name>`, value = `true`). After every successful genesis Publish the emitter writes the signal (idempotent); on subsequent boots `VerifyChain` reads it and returns `POLICY_CHAIN_TRUNCATED` when the audit row-set is empty but the signal is present. Closes the audit-integrity gap where deleting every chain row for a subject would otherwise let the verifier succeed and the next boot emit a fresh genesis. | `TestVerifyChainAcceptsEmptyOnFirstBoot`, `TestVerifyChainRejectsTruncatedChain`, `TestEmitCurrentSnapshotMarksChainInitialized` |

## Section 3 — Architecture overview

D contributes four packages under `internal/admin/` and zero packages
elsewhere. The package boundary between D and sub-epics A/B/C is preserved.

### Components

```text
internal/admin/
├── auth/              # Operator authentication
│   ├── provider.go        # OperatorAuthProvider interface
│   ├── ingame.go          # InGameCredentialsProvider impl (default)
│   ├── session.go         # In-memory session store, ULID tokens, 10-min TTL
│   └── handler.go         # Authenticate RPC handler (registers on C's mux)
│
├── approval/          # Dual-control approval rows
│   ├── repo.go            # admin_approvals Postgres repo
│   ├── types.go           # Approval, OpKind, etc.
│   └── handler.go         # Approve RPC handler
│
├── policy/            # crypto.policy_set hash chain
│   ├── verifier.go        # Startup chain integrity check (Bootstrap)
│   ├── emitter.go         # CryptoPolicySubsystem emit-on-startup
│   ├── chain.go           # JCS canonicalize + SHA-256 + payload schema
│   └── subsystem.go       # lifecycle.Subsystem impl
│
└── totp_audit/        # Decorator wrapping totp.Service
    └── auditing.go        # AuditingService — emits crypto.totp_*
```

### Wire surface — three RPCs added to `admin.v1.AdminService`

D extends the proto definition at `api/proto/holomush/admin/v1/admin.proto`
with three RPCs. C registered the service mux at
`internal/admin/socket/server.go::buildMux`; D's handlers are registered there
alongside the existing `Status` handler.

| RPC | Caller | Server-side flow |
|---|---|---|
| `Authenticate(AuthenticateRequest{username, password, totp_code}) -> AuthenticateResponse{session_token, expires_at, identity_summary}` | every CLI sub-command | Run `OperatorAuthProvider.Authenticate` 6-step sequence (see §4); on success issue a fresh ULID session token, store identity in `SessionStore` with 10-min TTL, return token + `expires_at`. PeerCred captured from ctx (sub-epic C middleware) into `OperatorIdentity.OSUser` for audit only. |
| `Approve(ApproveRequest{session_token, request_id}) -> ApproveResponse{}` | second-op CLI (`holomush admin approve <request_id>`) | Resolve `session_token` via `SessionStore.Get`; re-validate BOTH `crypto.operator` capability AND `RoleAdmin` per INV-D16 (defense-in-depth against grant or role revocation mid-session); look up `admin_approvals` row by `request_id` filtered by `expires_at >= now()` and `approved_at IS NULL`; reject if second-op `player_id == primary_player_id` (`DENY_DUAL_CONTROL_SELF`); otherwise `UPDATE admin_approvals SET approved_at = now(), approved_by_player_id = $session.PlayerID WHERE request_id = $1`. |
| `ResetTOTP(ResetTOTPRequest{session_token, target_player_id}) -> ResetTOTPResponse{cleared}` | admin reset CLI (`holomush admin totp reset <player>`) | Resolve session; re-validate BOTH `crypto.operator` capability AND `RoleAdmin` per INV-D16 (defense-in-depth against grant or role revocation mid-session); call `AuditingService.ClearTOTP(target_player_id, ClearReasonAdminReset)`; decorator emits `crypto.totp_cleared` post-success. |

E's `Rekey` and F's `AdminReadStream` are added later on the same mux; both
consume D's `SessionStore.Get(session_token)` to resolve identity and D's
`approval.Repo.{Open,WaitForApproval}` Go API for dual-control.

### Trust boundaries

```text
CLI process (operator's terminal)
  │
  │  prompts user for username / password / totp_code
  │  (TLS-irrelevant; localhost UDS only)
  │
  ▼
holomush server process (admin socket subsystem)
  │
  │  PeerCred middleware (sub-epic C) → ctx
  │  Authenticate handler → OperatorAuthProvider 6-step
  │    │
  │    ├─→ auth.Service.ValidateCredentials  (existing — argon2id timing-safe)
  │    ├─→ totp.Service.IsEnrolled            (sub-epic A)
  │    ├─→ AuditingService.Verify              (sub-epic A + D's decorator;
  │    │                                       emits crypto.totp_locked on
  │    │                                       LockoutTransition)
  │    ├─→ access.HasPlayerGrant              (sub-epic B; crypto.operator)
  │    ├─→ roleStore.PlayerHasRole            (RoleAdmin per INV-D16)
  │    └─→ peercred capture                   (sub-epic C)
  │
  │  on success: SessionStore.Issue → ULID token
  │
  ▼
session token (in-memory, 10 min TTL)
  │
  ▼
subsequent RPCs (Approve / ResetTOTP / Rekey / AdminReadStream)
  resolve via SessionStore.Get
```

### Default-deny posture

Every RPC handler MUST start with session resolution; an unknown or
expired token is rejected before any handler logic runs. The capability
check is repeated at each handler (defense in depth) so that a session
issued before a configuration change cannot exploit lingering authority.

## Section 4 — `OperatorAuthProvider` check sequence

The interface and default implementation:

```go
package adminauth

// AuthRequest is the credential bundle the CLI collected via prompts and
// sends in the Authenticate RPC payload.
type AuthRequest struct {
    Username string
    Password string
    TOTPCode string
    PeerCred socket.PeerCred  // captured by middleware; for audit
}

// OperatorIdentity is the audit record shape per master spec §4.6.
type OperatorIdentity struct {
    PlayerID           string  // ULID
    OSUser             string  // "uid=1001 (sean)" — audit, not enforced
    TOTPVerified       bool    // always true on successful Authenticate
    AuthProviderName   string  // "ingame-creds-totp"
    ProviderSpecificID string  // empty for in-game provider
}

type OperatorAuthProvider interface {
    Name() string
    Authenticate(ctx context.Context, req AuthRequest) (OperatorIdentity, error)
}
```

The 6-step check sequence (`InGameCredentialsProvider`):

1. `auth.Service.ValidateCredentials(ctx, req.Username, req.Password)` →
   `*Player` or typed error. Reuses the existing argon2id timing-safe path.
   Failure → `DENY_INVALID_CREDENTIALS`.
2. `totp.Service.IsEnrolled(ctx, player.ID)` → bool. Miss → `DENY_NOT_ENROLLED`.
3. `auditingTotp.Verify(ctx, player.ID, req.TOTPCode)` → `VerifyResult`.
   Outcome != `OutcomeOK` → `DENY_BAD_TOTP`. (Decorator emits
   `crypto.totp_locked` if `LockoutTransition: true`; the audit emission is a
   side effect, not a control-flow factor.)
4. `access.HasPlayerGrant(ctx, resolver, player.ID,
   access.CapabilityCryptoOperator)` → bool. Miss → `DENY_NOT_OPERATOR`.
5. `roleStore.PlayerHasRole(ctx, player.ID, access.RoleAdmin)` → bool.
   Miss → `DENY_NOT_ADMIN_ROLE`. (Per master spec §5.9 step 6 and
   decomposition spec Decision 5 line 177: capability is *narrowing* on
   top of `RoleAdmin`; the conjunction is the dual-key property. See §5
   "Role helper" below for the SQL.)
6. PeerCred capture from `ctx` → `OperatorIdentity.OSUser`. Audit only;
   never gates.

On success, build `OperatorIdentity{PlayerID: player.ID, OSUser: …,
TOTPVerified: true, AuthProviderName: "ingame-creds-totp"}`, mint a
ULID session token via `SessionStore.Issue(identity)`, return.

### Role helper — `RoleStore.PlayerHasRole`

Roles in HoloMUSH attach to **characters**, not players (`character_roles`
table; `RoleStore.GetRoles(characterID)` is character-keyed). To check
"this player has the `admin` role" we ask "does any of this player's
characters have it?" — semantics consistent with the bootstrap admin
seed (`internal/bootstrap/admin.go:95` creates one player + one admin
character).

D extends the `RoleStore` interface (`internal/store/role_store.go`)
with one method:

```go
// PlayerHasRole returns true iff at least one character belonging to
// playerID has the given role assigned. Used by D's OperatorAuthProvider
// to gate operator authentication.
PlayerHasRole(ctx context.Context, playerID, role string) (bool, error)
```

Postgres implementation (single SQL, indexed):

```sql
SELECT 1
  FROM character_roles cr
  JOIN characters c ON cr.character_id = c.id
 WHERE c.player_id = $1
   AND cr.role     = $2
 LIMIT 1
```

`character_roles.character_id` and `characters.id` are TEXT-typed ULIDs
in the existing schema; `characters.player_id` is the player FK. The
existing `(character_id, role)` index on `character_roles` plus
`characters` PK index keep the join cheap. One round-trip per Authenticate;
Authenticate is rare.

**Implementor note:** every existing implementor of `store.RoleStore`
(production `PostgresRoleStore`, plus in-tree fakes such as
`internal/bootstrap/admin_test.go::fakeRoleStore`) MUST gain a
`PlayerHasRole` method when this interface extension lands. Compile-time
enforcement makes the missed-update case loud rather than silent.

### `SessionStore`

```go
type SessionStore interface {
    Issue(identity OperatorIdentity) (token string, expiresAt time.Time, err error)
    Get(token string) (OperatorIdentity, error)  // DENY_SESSION_INVALID / DENY_SESSION_EXPIRED
    Revoke(token string) error                   // for future logout flows
}
```

In-memory implementation (`internal/admin/auth/session.go`):

- `map[string]sessionEntry` guarded by `sync.RWMutex`.
- `sessionEntry{Identity OperatorIdentity, ExpiresAt time.Time}`.
- TTL = 10 minutes from issuance. Starting value chosen to span
  multiple RPCs in a typical operator flow (Authenticate → OpenApproval →
  WaitForApproval → Rekey/AdminReadStream proceed). Sub-epic E will revise
  if Rekey worst-case (cold-tier re-encrypt + Phase-5 N-of-N cache
  invalidate) is observed to exceed it; the recovery path on session
  expiry is "operator re-runs Authenticate to mint a fresh token and
  resumes from checkpoint". No master-spec line pins this number.
- Cleanup-on-Get: every `Get` checks `ExpiresAt`; expired entries are
  deleted in-line and `DENY_SESSION_EXPIRED` returned.
- Optional periodic GC goroutine (every 1 min, deletes all expired) for
  servers with very low Get rate; not load-bearing.
- ULID tokens (`oklog/ulid` already imported) — sortable, 122-bit entropy,
  same alphabet as approval `request_id` (audit-trail consistency).

### Authentication failure DENY codes

D introduces / consumes the following DENY codes (typed `oops.Code` values):

| Code | Origin | Cause |
|---|---|---|
| `DENY_INVALID_CREDENTIALS` | step 1 | argon2id mismatch or missing user |
| `DENY_NOT_ENROLLED` | step 2 | no `player_totp` row |
| `DENY_BAD_TOTP` | step 3 | TOTP `Verify` outcome ≠ `OutcomeOK` |
| `DENY_LOCKED` | step 3 | TOTP `Verify` outcome = `OutcomeLocked` (sub-classification of `DENY_BAD_TOTP`; surfaced separately for operator UX) |
| `DENY_NOT_OPERATOR` | step 4 | `crypto.operator` grant absent |
| `DENY_NOT_ADMIN_ROLE` | step 5 | no character of this player has the `admin` role |
| `DENY_SESSION_INVALID` | session | token unknown |
| `DENY_SESSION_EXPIRED` | session | token expired |
| `DENY_DUAL_CONTROL_SELF` | Approve | second-op `player_id == primary_player_id` |
| `DENY_DUAL_CONTROL_REQUIRED` | E/F handler | site policy lists op + operator did not pass `--dual-control` |
| `DENY_APPROVAL_ARGS_MISMATCH` | E/F handler | proceeding args' hash ≠ stored `op_args_hash` |
| `DENY_APPROVAL_EXPIRED` | E/F handler / Approve | `expires_at < now()` |
| `DENY_APPROVAL_ALREADY_APPROVED` | E/F handler / Approve | `approved_at IS NOT NULL` |
| `DENY_POLICY_HASH_UNKNOWN` | E/F handler | invocation references a `policy_hash` not present in chain (master spec amendment, B's row 484) |

## Section 5 — `admin_approvals` + dual-control protocol

### Schema

Migration `000020_create_admin_approvals.{up,down}.sql`:

```sql
-- 000020_create_admin_approvals.up.sql
CREATE TABLE IF NOT EXISTS admin_approvals (
    request_id              BYTEA PRIMARY KEY,         -- 16-byte ULID
    primary_player_id       TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    op_kind                 TEXT NOT NULL,             -- "rekey" | "admin_read_stream"
    op_args_hash            BYTEA NOT NULL,            -- 32-byte SHA-256
    expires_at              TIMESTAMPTZ NOT NULL,
    approved_at             TIMESTAMPTZ NULL,
    approved_by_player_id   TEXT NULL REFERENCES players(id) ON DELETE CASCADE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Index supports the unapproved-not-expired lookup hot path.
CREATE INDEX IF NOT EXISTS idx_admin_approvals_pending
    ON admin_approvals (expires_at)
    WHERE approved_at IS NULL;
```

Player IDs use `TEXT REFERENCES players(id) ON DELETE CASCADE` to match the rest of the schema (000001 baseline, 000015 player_character_bindings, 000019 player_totp). The partial index is keyed on `expires_at` to support the §6 Approve hot path filter `expires_at >= now() AND approved_at IS NULL`.

```sql
-- 000020_create_admin_approvals.down.sql
DROP INDEX IF EXISTS idx_admin_approvals_pending;
DROP TABLE IF EXISTS admin_approvals;
```

### Repo Go API (consumed by E/F)

```go
package approval

type Repo interface {
    Open(ctx context.Context, req OpenRequest) (RequestID, error)
    Get(ctx context.Context, requestID RequestID) (Approval, error)
    MarkApproved(ctx context.Context, requestID RequestID, secondOpPlayerID string) error
    WaitForApproval(ctx context.Context, requestID RequestID, deadline time.Time) (Approval, error)
}

type OpenRequest struct {
    PrimaryPlayerID string  // ULID
    OpKind          string  // "rekey" | "admin_read_stream"
    OpArgsHash      []byte  // 32-byte SHA-256
}

type Approval struct {
    RequestID            RequestID
    PrimaryPlayerID      string
    OpKind               string
    OpArgsHash           []byte
    ExpiresAt            time.Time
    ApprovedAt           *time.Time  // nil = pending
    ApprovedByPlayerID   string      // "" = pending
}
```

`Open` generates a fresh ULID `request_id`, inserts the row, returns the ID.
The primary's CLI prints the ID to stderr for the operator to communicate
out-of-band.

`MarkApproved` enforces:

- The row exists, is unexpired, and is unapproved (`expires_at >= now()`,
  `approved_at IS NULL`).
- `secondOpPlayerID != Approval.PrimaryPlayerID` (INV-D6 →
  `DENY_DUAL_CONTROL_SELF`).
- Atomic single-statement update with `WHERE` predicates so concurrent
  approves cannot race.

`WaitForApproval` is the primary's blocking call after `Open`. v1 polls
every 500 ms until the row's `approved_at` is non-null OR `deadline` has
passed. (PostgreSQL `LISTEN`/`NOTIFY` would be cleaner but adds a coupling
that the UDS-localhost topology does not justify; v1 stays with polling.)

### `op_args_hash` algorithm

```go
func ComputeOpArgsHash(args proto.Message) ([]byte, error) {
    raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(args)
    if err != nil {
        return nil, oops.Code("OP_ARGS_HASH_MARSHAL_FAILED").Wrap(err)
    }
    sum := sha256.Sum256(raw)
    return sum[:], nil
}
```

Both the primary's CLI (computing the hash for `Open`) and the server-side
proceeding handler (recomputing for verification) use the same proto
deterministic-marshal byte sequence. Mismatch → `DENY_APPROVAL_ARGS_MISMATCH`.

JCS is **not** used here. JCS is reserved for `crypto.policy_set` chain
hashing where the payload is JSON-shaped server-side (§6). Args are
proto-shaped on the wire and stay in proto land.

### Second-op CLI flow

`holomush admin approve <request_id>`:

1. CLI prompts user for username / password / TOTP code.
2. CLI opens an independent ConnectRPC connection to the admin UDS socket.
3. CLI calls `Authenticate(creds, totp)` → receives a session token.
4. CLI calls `Approve(session_token, request_id)`.
5. Server-side handler:
   - Resolves session via `SessionStore.Get`.
   - Re-asserts capability (INV-D16).
   - Calls `Repo.MarkApproved(request_id, session.Identity.PlayerID)`.
   - On `DENY_DUAL_CONTROL_SELF` (INV-D6) returns the typed error.
6. CLI prints "approved" or the typed DENY message to stderr.

### Two-layer enforcement (Decision 6)

| Site policy | Operator flag | Result |
|---|---|---|
| `dual_control_required: []` | (no flag) | single-control: 5-step Authenticate suffices |
| `dual_control_required: []` | `--dual-control` | dual-control (operator chose stronger) |
| `dual_control_required: [rekey]` | (no flag) | server returns `DENY_DUAL_CONTROL_REQUIRED` |
| `dual_control_required: [rekey]` | `--dual-control` | dual-control |

Server enforcement is in E's RekeyHandler / F's AdminReadStreamHandler at
the moment of operation invocation. D ships the Decoder for the config:
`internal/config/config.go` already has a `CryptoConfig` struct (sub-epic B);
D extends it with `DualControlRequired []string` (validated lax+warn at
startup — see §9).

## Section 6 — `crypto.policy_set` hash chain

### Subject and event-type

- Subject: `events.<game>.system.crypto_policy.<policy_name>`
  (e.g., `events.holomush.system.crypto_policy.dual_control_required`)
- Event type: `crypto.policy_set`

(Sub-epic A's `events.>` choice — events.<game>.system.crypto_totp.* — is
preserved; the EVENTS JetStream stream binds only `events.>`. The master
spec's aspirational `audit.<game>.system.crypto_policy.<policy_name>`
form is amended to the `events.>` form per §10.)

### Payload schema

```go
package policy

// PolicySetPayload is the body of a crypto.policy_set audit event.
type PolicySetPayload struct {
    PolicyName        string         `json:"policy_name"`
    PolicySnapshot    map[string]any `json:"policy_snapshot"`     // current effective config
    PolicyHash        []byte         `json:"policy_hash"`         // base64 in JSON; SHA-256 over canonicalized self-minus-policy_hash
    PrevHash          []byte         `json:"prev_hash"`           // null at genesis; otherwise the predecessor's policy_hash
    ServerStartULID   string         `json:"server_start_ulid"`   // ULID minted at server boot
    ServerIdentity    string         `json:"server_identity"`     // hostname or configured server-id
    Timestamp         time.Time      `json:"timestamp"`
}
```

In v1, `PolicyName` is one of: `dual_control_required` (the only chain
defined here). Future policy names follow the same chain pattern, each with
its own independent chain.

### Hash algorithm

```go
func computePolicyHash(payload *PolicySetPayload) ([]byte, error) {
    // Build a copy with policy_hash zeroed to exclude it from canonicalization.
    canon := *payload
    canon.PolicyHash = nil
    raw, err := json.Marshal(&canon)
    if err != nil {
        return nil, oops.Code("POLICY_HASH_JSON_MARSHAL_FAILED").Wrap(err)
    }
    canonical, err := jsoncanonicalizer.Transform(raw)
    if err != nil {
        return nil, oops.Code("POLICY_HASH_JCS_FAILED").Wrap(err)
    }
    sum := sha256.Sum256(canonical)
    return sum[:], nil
}
```

Library: `github.com/cyberphone/json-canonicalization` pinned at
`v0.0.0-20241213102144-19d51d7fe467`. A meta-test
(`TestJCSCanonicalizationLockedToVendoredImpl` in
`internal/admin/policy/`) reads `go.sum` / `go.mod` and asserts the import
path + version pin. Any future bump MUST be a deliberate master-spec
amendment because changing the canonicalizer (or the underlying RFC 8785
interpretation) is a chain-breaking change.

### Genesis (INV-D10)

A fresh database has no `events_audit` rows for the chain. The first
`policy_set` event for a `policy_name` is written with `prev_hash IS NULL`.
The verifier MUST recognize this as the chain root: walking backward and
arriving at a null `prev_hash` is OK; walking backward and arriving at a
non-null `prev_hash` whose predecessor cannot be found is FAIL.

### Verifier (Bootstrap subsystem) — data shape

The chain payload (`PolicySetPayload`) lives **inside the marshaled `Event`
proto envelope**, which `events_audit.envelope` stores as `BYTEA`
(migration `000017_events_audit_envelope_rename.up.sql` renamed the column
from `payload` to `envelope` to clarify this — the column has always
contained `proto.Marshal(*Event)` bytes; the chain payload is the inner
`Event.Payload` field, JSON-encoded). v1 stores `policy_hash` and
`prev_hash` only inside the JSON payload; no structured columns are added
to `events_audit`. The decode-at-verify-time cost is N JSON parses per
boot (chain length is small in practice — one event per server-config
change), and avoiding a column-shape coupling keeps `events_audit`
agnostic to D's evolution.

The verifier loads rows by exact subject and `js_seq` ordering (the
JetStream sequence, which is the durable monotonic ordering on
`events_audit` per migration 000011's index):

```sql
SELECT envelope, js_seq
  FROM events_audit
 WHERE subject = $1                  -- e.g., events.holomush.system.crypto_policy.dual_control_required
 ORDER BY js_seq ASC
```

For each row: `proto.Unmarshal(envelope, &eventbusv1.Event{})` →
`json.Unmarshal(event.Payload, &PolicySetPayload{})`. The chain walk is
identical in shape to the original sketch, but now grounded in the real
columns:

**Codec constraint:** the chain subject (`events.<game>.system.crypto_policy.<policy_name>`)
MUST be bound to `IdentityCodec` in any production `KeySelector`
deployment. The verifier's decode path bypasses the codec layer (it does
`proto.Unmarshal` then `json.Unmarshal` directly on `Event.Payload`),
which is correct for identity-codec pass-through (the default at
`internal/eventbus/publisher.go:482-488` — `identityKeySelector`) but
fails if the subject is bound to `xchacha20poly1305-v1` or any other
non-identity codec. Encrypting the chain payload is unsupported in v1
because the verifier runs in Bootstrap before the crypto provider /
DEK manager are wired. (Sub-epic A's `events.<game>.system.crypto_totp.*`
events follow the same constraint — the audit projection writes them
through the identity path.) D's `KeySelector` setup MUST NOT register a
non-identity codec for the chain subject; if a future deployment needs
encrypted policy events, that is a master-spec amendment, not a
sub-epic-internal refactor.

```go
type chainEntry struct {
    Seq     int64
    Payload PolicySetPayload  // decoded from envelope.Event.Payload (JSON)
}

func VerifyChain(ctx context.Context, pool *pgxpool.Pool, subject string, policyName string) error {
    entries, err := loadPolicySetChainEntries(ctx, pool, subject) // ORDER BY js_seq ASC
    if err != nil {
        return oops.Code("POLICY_CHAIN_LOAD_FAILED").
            With("subject", subject).Wrap(err)
    }
    if len(entries) == 0 {
        return nil // fresh DB; CryptoPolicySubsystem will write the genesis row
    }
    if entries[0].Payload.PrevHash != nil {
        return oops.Code("POLICY_CHAIN_BROKEN_GENESIS").
            With("subject", subject).
            With("js_seq", entries[0].Seq).
            Errorf("first %s event has non-null prev_hash", policyName)
    }
    for i := 1; i < len(entries); i++ {
        expectedPrev := entries[i-1].Payload.PolicyHash
        if !bytes.Equal(entries[i].Payload.PrevHash, expectedPrev) {
            return oops.Code("POLICY_CHAIN_BROKEN_LINK").
                With("policy_name", policyName).
                With("js_seq", entries[i].Seq).
                Errorf("prev_hash does not match predecessor's policy_hash")
        }
        // Recompute and verify each event's own policy_hash to catch
        // tampering of the payload bytes themselves.
        recomputed, _ := computePolicyHash(&entries[i].Payload)
        if !bytes.Equal(entries[i].Payload.PolicyHash, recomputed) {
            return oops.Code("POLICY_CHAIN_HASH_MISMATCH").
                With("policy_name", policyName).
                With("js_seq", entries[i].Seq).
                Errorf("policy_hash does not match canonicalized payload")
        }
    }
    return nil
}
```

`loadPolicySetChainEntries` does the SQL above, the
`proto.Unmarshal(envelope) → json.Unmarshal(event.Payload, &PolicySetPayload)`
two-step decode, and returns the `chainEntry` slice ordered by `js_seq`.

The verifier runs inside the Bootstrap subsystem
(`internal/bootstrap/setup/subsystem.go::Start`) alongside INV-32/33/37
and the orphan check. Failure aborts startup with a typed error.

### Verifier consistency wrt audit projection async-write

The verifier reads `events_audit` BEFORE the audit projection runs on
this boot (Bootstrap subsystem starts before EventBus / AuditProjection
in `productionSubsystems()`). The visible `events_audit` content reflects
**what the previous server lifetime's projection successfully wrote** —
plus whatever the just-this-boot's projection-resume writes after the
verifier has already returned.

This has one footnote: if the prior boot crashed with a `policy_set`
event acked by JetStream but not yet projected to `events_audit`, the
verifier on this boot misses that tail event. After AuditProjection
starts on this boot, it'll re-deliver the JS message and write the
projection row. The next-boot verifier will see it. The current boot's
`CryptoPolicySubsystem.Start` (which depends on AuditProjection and
runs after the projection drains its initial backlog) reads the
now-projected tail and extends the chain correctly with `prev_hash`
matching that tail's `policy_hash`.

What this means for adversaries:

- **Graceful shutdown:** projection drains before exit. No verifier gap.
- **Crash:** chain may have an unprojected tail event for one boot
  cycle. The next-boot verifier sees the recovered tail. An adversary
  with shell who tampers with the in-flight JS message between boots
  can break the chain — but tampering with JS is master-spec §1
  trust-path territory (root-on-host bypasses audit by design).
- **Cluster/replica:** v1 is single-replica; deferred for clustered
  topologies (see §11 out of scope).

INV-D11 ("startup verifier MUST be fail-closed") is preserved across
all paths within the master-spec threat model.

### Emitter (`CryptoPolicySubsystem`)

After AuditProjection is up, a new lifecycle subsystem reads the latest
chain event for each known policy_name, computes its `policy_hash`, builds
the new event with `prev_hash = thatHash` (or null if no prior event),
and publishes via `eventbus.Publisher`.

```go
package policy

type CryptoPolicySubsystem struct {
    cfg     CryptoPolicySubsystemConfig
    started bool
}

type CryptoPolicySubsystemConfig struct {
    GameID          string
    ServerStartULID string
    ServerIdentity  string
    Publisher       eventbus.Publisher
    Pool            *pgxpool.Pool
    EffectiveConfig CryptoEffectiveConfig  // current dual_control_required, etc.
    Clock           Clock
}

func (s *CryptoPolicySubsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemCryptoPolicy }
func (s *CryptoPolicySubsystem) DependsOn() []lifecycle.SubsystemID {
    return []lifecycle.SubsystemID{lifecycle.SubsystemAuditProjection}
}
func (s *CryptoPolicySubsystem) Start(ctx context.Context) error {
    return EmitCurrentSnapshot(ctx, s.cfg)
}
func (s *CryptoPolicySubsystem) Stop(ctx context.Context) error { return nil }
```

A new `lifecycle.SubsystemCryptoPolicy` constant is added to
`internal/lifecycle/subsystem.go`. `productionSubsystems()` in
`cmd/holomush/core.go` is extended with the new subsystem after AuditProjection
and before gRPC.

`EmitCurrentSnapshot` is idempotent on no-change: if the latest existing event's
`PolicySnapshot` byte-equals (after JCS canonicalize) the current effective
config, no new event is emitted. (Avoids growing the chain on every restart
when nothing changed. Genuine restarts that change effective policy DO emit.)

**Publish-failure handling (INV-D17): fail-closed.** If `eventbus.Publisher.Publish`
returns an error during `EmitCurrentSnapshot`, the subsystem returns the error
from `Subsystem.Start` and the lifecycle orchestrator refuses to start the
server. This is **distinct from INV-D14's log-and-continue policy for TOTP
lifecycle events** because chain integrity is a security claim: a silently-
dropped `policy_set` event would break chain continuity on the next boot,
making forensic reconstruction impossible. The publish call MUST block on
JetStream ack before returning success.

Per-invocation `policy_hash` for E/F's audit events: when E's RekeyHandler /
F's AdminReadStreamHandler emit their own audit event, they include
`policy_hash = currentSnapshotHash` for `dual_control_required`. This
field's verification (INV-`DENY_POLICY_HASH_UNKNOWN`) lives in E/F handlers;
D ships the lookup helper `policy.LatestHashForName(ctx, pool, policyName) ([]byte, error)`.

## Section 7 — Audit emission decorator

`internal/admin/totp_audit/auditing.go`:

```go
package totpaudit

import (
    "context"
    "log/slog"

    "github.com/oklog/ulid/v2"

    "github.com/holomush/holomush/internal/eventbus"
    "github.com/holomush/holomush/internal/totp"
)

// AuditingService wraps totp.Service to emit lifecycle audit events on
// every observed state transition. Sub-epic A's host-shell CLIs continue
// using the raw totp.Service (R5 Option Y — no eventbus access). All
// server-side callers SHOULD wire through the decorator.
type AuditingService struct {
    inner   totp.Service
    pub     eventbus.Publisher
    gameID  string
    clock   totp.Clock
    logger  *slog.Logger
}

func NewAuditingService(inner totp.Service, pub eventbus.Publisher,
    gameID string, clock totp.Clock, logger *slog.Logger) *AuditingService {
    return &AuditingService{inner: inner, pub: pub, gameID: gameID, clock: clock, logger: logger}
}

func (a *AuditingService) Verify(ctx context.Context, pid ulid.ULID, code string) (totp.VerifyResult, error) {
    res, err := a.inner.Verify(ctx, pid, code)
    if err != nil {
        return res, err
    }
    if res.LockoutTransition {
        a.emitLocked(ctx, pid, res.LockedUntil)
    }
    return res, nil
}

func (a *AuditingService) ConsumeRecoveryCode(ctx context.Context, pid ulid.ULID, code string) (totp.ConsumeRecoveryResult, error) {
    res, err := a.inner.ConsumeRecoveryCode(ctx, pid, code)
    if err != nil {
        return res, err
    }
    a.emitRecoveryConsumed(ctx, pid, res.RecoveryCodeID)
    return res, nil
}

func (a *AuditingService) ClearTOTP(ctx context.Context, pid ulid.ULID, by totp.ClearReason) (totp.ClearResult, error) {
    res, err := a.inner.ClearTOTP(ctx, pid, by)
    if err != nil {
        return res, err
    }
    a.emitCleared(ctx, pid, by, res.AuditClearedAt)
    return res, nil
}

func (a *AuditingService) RecoverAndClear(ctx context.Context, pid ulid.ULID, code string) (totp.RecoverAndClearResult, error) {
    res, err := a.inner.RecoverAndClear(ctx, pid, code)
    if err != nil {
        return res, err
    }
    a.emitRecoveryConsumed(ctx, pid, res.RecoveryCodeID)
    a.emitCleared(ctx, pid, totp.ClearReasonRecoveryCode, res.AuditClearedAt)
    return res, nil
}

// (Enroll, BootstrapEnroll, IsEnrolled, etc. — wrapped same way; emit on success.)
```

### Emit failure handling (INV-D14)

If `pub.Publish` returns an error inside one of the `emit*` helpers:

- `slog.Warn` with structured fields: `event_type`, `player_id`, `subject`,
  `publish_error`. Includes a short rationale so operators reading logs know
  this is a known fallback path.
- The inner `Service` operation already succeeded (PG state is committed).
  Do **not** roll back — informational audit is not gating.
- Emit a metric: `admin_audit_publish_failures_total{event_type=...}`.

This is distinct from F's `AdminReadStream` which has INV-42's
hard-required pre-emit gate (no plaintext leaves the server until the audit
event lands). TOTP-lifecycle events are informational; loss of one event is
a forensic gap but not a security breach.

**`RecoverAndClear` partial-emit window.** `RecoverAndClear` performs
recovery-code consumption AND TOTP enrollment clear in a single PG
transaction (sub-epic A INV-A6 + INV-A7). The decorator emits two events
sequentially after the inner call returns success. If the first emit
(`crypto.totp_recovery_code_consumed`) succeeds but the second
(`crypto.totp_cleared`) fails, the audit log shows "recovery code
consumed" without the matching "cleared" event, while PG state shows
both have happened. This is a known forensic-drift window covered by
INV-D14's log-and-continue contract. Operators reading `events_audit`
SHOULD treat the inner Service / PG state (`player_totp` row absence +
`player_totp_recovery_codes` row consumption) as the source of truth;
the audit log is informational. The `slog.Warn` log line for the failed
emit names the event type, player_id, and publish error; an operator
investigating a "missing cleared event" can grep server logs to find
the failed emit.

## Section 8 — Test strategy

D ships tests at five layers. Every invariant in §2 has at least one named
test mapping to it.

### Unit tests (fast, mocked deps; `task test`)

`internal/admin/auth/`:

- `TestAuthenticateStepOrderFixedOnFailure` — ValidateCredentials fails →
  IsEnrolled is never called (assert via mockery). Same for steps 2, 3, 4, 5.
  (INV-D1)
- `TestAuthenticateRejectsNonOperator` — ValidateCredentials + TOTP succeed
  but `HasPlayerGrant` returns false → DENY_NOT_OPERATOR; identity not
  issued; no session created. (INV-D15)
- `TestAuthenticateRejectsPlayerWithoutAdminRole` — steps 1-4 succeed
  but `RoleStore.PlayerHasRole(playerID, RoleAdmin)` returns false →
  DENY_NOT_ADMIN_ROLE; identity not issued. (INV-D19)
- `TestAuthenticateIgnoresPeerCredForGating` — same input twice with
  different PeerCred values returns identical (success or failure) outcomes;
  PeerCred surfaces in `OperatorIdentity.OSUser` only. (INV-D4)
- `TestResetTOTPRequiresAdminRoleOnHandler` — session issued for player
  with admin role; revoke role mid-session via direct `RoleStore.RemoveRole`
  on the operator's admin character; new ResetTOTP call → DENY_NOT_ADMIN_ROLE.
  (INV-D16 + INV-D19 defense-in-depth)
- `TestSessionStoreEmptiedOnConstruction` — `NewSessionStore()` → `Get(...)`
  returns DENY_SESSION_INVALID. (INV-D3)
- `TestSessionStoreRejectsExpiredToken` — Issue, advance fake clock past
  TTL, Get → DENY_SESSION_EXPIRED. (INV-D2)
- `TestSessionStoreConcurrentIssueAndGet` — fan out 100 Issue + 100 Get
  goroutines; race detector clean. (concurrency invariant)
- `TestResetTOTPRequiresCapabilityOnHandler` — session issued for player A
  with `crypto.operator`, then external mutation removes A from
  `crypto.operators` set; new ResetTOTP call → DENY_NOT_OPERATOR. (INV-D16)

`internal/admin/approval/`:

- `TestMarkApprovedRejectsSelfApproval` — Open with primary X; MarkApproved
  with secondOp X → DENY_DUAL_CONTROL_SELF. (INV-D6)
- `TestApprovalRepoReadFiltersExpired` — insert expired row; Get returns
  not-found-equivalent; insert non-expired; Get returns row. (INV-D5)
- `TestOpArgsHashAlgorithmStableAgainstGolden` — table test with
  representative `proto.Message` values; assert SHA-256 hex equals a
  fixed expected value (golden file). Cross-binary stability is locked
  by INV-D18's protobuf-go version pin. (INV-D8)
- `TestOpArgsHashMismatchRejected` — Open with hashA; proceed with hashB →
  DENY_APPROVAL_ARGS_MISMATCH. (INV-D9; combined with E/F handlers.)

`internal/admin/policy/`:

- `TestPolicySetGenesisHasNullPrevHash` — empty events_audit; emit current
  snapshot; assert resulting payload's `prev_hash` is nil. (INV-D10)
- `TestPolicySetSubsequentReferencesPredecessor` — emit twice with policy
  changes; assert second event's `prev_hash` == first's `policy_hash`.
  (INV-D10)
- `TestPolicyHashComputedOverPayloadMinusHashField` — handcrafted
  payload; recompute `policy_hash`; assert the hash matches a golden value
  produced from a JCS-canonicalized version with `policy_hash` zeroed.
  (INV-D12)
- `TestVerifierRefusesStartOnBrokenChain` — three inserted events; corrupt
  middle event's `prev_hash`; VerifyChain returns
  `POLICY_CHAIN_BROKEN_LINK`. (INV-D11)
- `TestVerifierDecodesEnvelopeAndJSONPayload` — synthesize a valid
  `events_audit` row whose `envelope` column contains a marshaled `Event`
  with JSON `PolicySetPayload` in `Event.Payload`; verifier loads, decodes,
  and walks. (Grounds INV-D10/D11/D12 against the actual data shape.)
- `TestCryptoPolicySubsystemFailsStartOnPublishError` — wire a Publisher
  stub that returns an error; assert `Subsystem.Start` returns the wrapped
  error; assert no row was written to `events_audit` (no inflight). (INV-D17)
- `TestJCSCanonicalizationLockedToVendoredImpl` — meta-test that reads
  `go.mod` / `go.sum` and asserts the `cyberphone/json-canonicalization`
  pseudo-version is exactly the pinned one. (INV-D13)
- `TestProtoDeterministicMarshalLockedToVendoredProtobuf` — meta-test that
  reads `go.mod` / `go.sum` and asserts `google.golang.org/protobuf` is
  pinned to a specific version (chosen at implementation time from the
  current `go.mod`; the test locks it). Documents in a `go.mod` comment
  that the pin is load-bearing on `op_args_hash` cross-process equality
  (INV-D8/D9). (INV-D18)

`internal/admin/totp_audit/`:

- `TestAuditingServiceEmitsOnceOnTransition` — Verify returns
  `LockoutTransition: true`; mock Publisher receives exactly one
  `crypto.totp_locked` Publish call with the right subject. (INV-D14)
- `TestAuditingServiceLogsAndContinuesOnPublishError` — Publisher returns
  error; AuditingService still returns inner's result (no PG rollback).
  Assert slog records the warning. (INV-D14)
- `TestAuditingServiceWrapsAllStateTransitionMethods` — table test for
  Verify/ConsumeRecoveryCode/ClearTOTP/RecoverAndClear/Enroll/BootstrapEnroll;
  each emits exactly the expected event(s) on success.

### Boundary tests (real interfaces; mocks only at the leaves)

- `TestAuthenticateRPCEndToEndOverMux` — register the Authenticate handler
  on a real `http.ServeMux` matching production wiring; HTTP-POST the
  request payload over an in-process listener; assert response body
  contains a session token and `expires_at`.
- `TestAuthenticateRPCRejectsExpiredSessionTokenInDownstreamRPC` — issue
  a token via Authenticate; advance clock; call Approve over the same
  mux; assert DENY_SESSION_EXPIRED returned in the response body.
- `TestPeerCredCapturedInAuditEnvelope` — drive Authenticate via the
  PeerCred-middleware-wrapped mux; assert the issued session's
  `OperatorIdentity.OSUser` contains the PeerCred uid. (INV-D4 + boundary)

### Integration tests (real Postgres; `task test:int` build tag)

- `TestAdminApprovalsMigrationUpDownRoundtrip` — apply migration 000020,
  insert + select a row, run down migration, assert table dropped.
- `TestApprovalRepoOpenGetMarkApprovedConcurrent` — fan out 50 Open calls
  - 50 MarkApproved calls; assert no row gets approved twice; PG
  `WHERE` predicates serialize correctly.
- `TestVerifierOnRealChain` — synthesize 3 valid events; VerifyChain
  succeeds; corrupt `prev_hash` of event 2; VerifyChain fails with
  `POLICY_CHAIN_BROKEN_LINK`.

### End-to-end tests (full server stack via Ginkgo; `test/integration/`)

- `e2e_admin_authenticate_lifecycle_test.go`:
  - **Happy path:** boot full server with `crypto.operators=[playerA]`;
    sub-epic A's `bootstrap-enroll` CLI seeds TOTP for playerA out-of-band;
    admin CLI calls Authenticate → receives session token; Get on the
    session store returns the right identity.
  - **TOTP lockout flow:** Authenticate × 5 with bad TOTP; assert 5th call
    triggers `events_audit` row for `crypto.totp_locked`; 6th call returns
    DENY_LOCKED.
  - **Reset cleared flow:** Authenticate with playerA (operator); call
    ResetTOTP for playerB (a non-operator with TOTP enrolled); assert
    `events_audit` contains `crypto.totp_cleared` with
    `cleared_by=admin_reset`.
- `e2e_admin_dual_control_test.go`:
  - **Happy path:** primary opens approval (via a stub Rekey-shape
    operation that exercises the Repo); second-op CLI calls Approve;
    primary's blocking call resolves (WaitForApproval returns the
    approved row); proceed-with-args succeeds.
  - **DENY paths:** self-approval → DENY_DUAL_CONTROL_SELF; second-op
    without `crypto.operator` → DENY_NOT_OPERATOR; expired approval →
    DENY_APPROVAL_EXPIRED; mismatched proceed args → DENY_APPROVAL_ARGS_MISMATCH.
- `e2e_admin_policy_chain_test.go`:
  - **Genesis on first boot:** boot full server on a fresh DB; assert
    `events_audit` has 1 `crypto.policy_set` row for `dual_control_required`
    with `prev_hash IS NULL`.
  - **Chain-extend on second boot:** stop server, change config, boot
    again; assert second `policy_set` event has `prev_hash` ==
    first event's `policy_hash`.
  - **Fail-closed on tamper:** as in the previous test, tamper with first
    event's `payload` bytes in PG, attempt third boot; assert server
    refuses to start with `POLICY_CHAIN_BROKEN_LINK` /
    `POLICY_CHAIN_HASH_MISMATCH`.

### Meta-tests

- `TestSpecAmendmentsLandedSubEpicD` (extends `internal/access/spec_amendments_test.go`)
  — substring-match each amendment row in §10 against the actual
  master-spec file content. Lock the amendments so a careless edit cannot
  silently revert them.
- `TestJCSCanonicalizationLockedToVendoredImpl` (above) — locks the
  canonicalizer pseudo-version.

## Section 9 — Validation (startup hooks)

D extends the existing lax+warn startup-validation pattern from sub-epic B
(`crypto.operators` allow-list validation in `cmd/holomush/crypto_operator_validation.go`).

### `crypto.dual_control_required` validation

```go
func validateDualControlRequired(
    ops []string,
    knownOpKinds map[string]struct{},
    logger *slog.Logger,
) []string {
    valid := make([]string, 0, len(ops))
    for _, op := range ops {
        if _, ok := knownOpKinds[op]; !ok {
            logger.Warn("crypto.dual_control_required references unknown op_kind; ignoring",
                "op_kind", op,
                "known_ops", knownOpKinds)
            continue
        }
        valid = append(valid, op)
    }
    return valid
}
```

`knownOpKinds` is hard-coded to `{"rekey", "admin_read_stream"}` in v1.

Behavior:

- Unknown op_kind → `slog.Warn` and exclude from enforcement. Server starts
  successfully.
- Empty / nil → no enforcement (single-control allowed for any op).
- All entries valid → enforcement engaged.

This mirrors B's lax+warn philosophy: validation is observability, not
gating. An operator's typo in the YAML doesn't bring down the server.

### Chain integrity validation

Already covered by §6's verifier — fail-closed at startup (INV-D11).
Distinct from lax+warn because chain integrity is a security claim, not a
config-typo concern.

### Session-store sanity

At construction, the session store is empty. There is no startup
validation of session content because the session map is per-process
in-memory by design (INV-D3). A sanity test
(`TestSessionStoreEmptiedOnConstruction`) covers this contract.

## Section 10 — Master-spec amendments

The following edits to `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`
land alongside D's first PR. A meta-test
(`TestSpecAmendmentsLandedSubEpicD`) substring-asserts each amendment.

| Section | Amendment |
|---|---|
| §5.9 interface | Replace the `Authenticate(ctx, prompt PromptFunc)` and `RequireDualControl(ctx, primary, prompt PromptFunc)` shape with the server-side shape: `Authenticate(ctx, AuthRequest) (OperatorIdentity, error)`. The CLI-side `PromptFunc` was architecturally aspirational — a callback cannot run server-side across the UDS boundary. Dual-control orchestration moves from a provider method to an operation-handler concern (server creates `admin_approvals` row, primary's CLI blocks, second-op CLI calls Approve). |
| §5.9 default impl | Reorder default-implementation steps so the role check (step 5 in this amended sequence) follows the `crypto.operator` capability check (step 4) and precedes the PeerCred capture. Master spec original §5.9 left the order ambiguous between role and capability; D's 6-step sequence is the canonical wiring. (Conjunction property — `RoleAdmin AND crypto.operator` per decomposition spec line 89/177 — is preserved unchanged.) |
| §5.9 step 6 → step 5 wiring | Reify the role check as `RoleStore.PlayerHasRole(playerID, RoleAdmin)`. Roles attach to characters in HoloMUSH; "player has role" is "any of player's characters has role". Sub-epic B's `internal/access/grants.go:14-18` doc comment ("MUST be combined with RoleAdmin") is preserved verbatim. |
| §6.3.1 | Pin `op_args_hash = SHA-256(proto.MarshalOptions{Deterministic: true}.Marshal(args))`. JCS is reserved for `crypto.policy_set` chain hashing only. Cross-binary stability requires pinning `google.golang.org/protobuf` in `go.mod` (see INV-D18). |
| §4.6 (chain subject) | Pin the `policy_set` chain subject to `events.<game>.system.crypto_policy.<policy_name>`, reconciling §4.6's mixed `audit.>` / `events.>` usage. The chosen form matches sub-epic A's `events.<game>.system.crypto_totp.*` precedent and the EVENTS JetStream stream's `events.>` filter. |
| §4.6 (chain payload storage) | The chain payload (`PolicySetPayload`) is encoded as JSON inside the `Event.Payload` field of the `core.v1.Event` envelope. `events_audit.envelope` (post-000017 rename) stores the marshaled envelope. The verifier decodes envelope → JSON payload at read time; no structured columns added to `events_audit` in v1. |
| §10 (DENY codes) | Add: `DENY_NOT_ADMIN_ROLE`, `DENY_SESSION_INVALID`, `DENY_SESSION_EXPIRED`, `DENY_DUAL_CONTROL_SELF`, `DENY_APPROVAL_EXPIRED`, `DENY_APPROVAL_ALREADY_APPROVED`. |

D's master-spec amendments do not change any participant-facing crypto
invariant (INV-1 through INV-50+). They reshape the operator authentication
boundary and the chain-data storage shape to match what's architecturally
enforceable, while **preserving B's RoleAdmin AND crypto.operator
conjunction** (decomposition spec line 89/177; `internal/access/grants.go:14-18`
doc comment; B's `TestSpecAmendmentsLanded` substrings).

## Section 11 — Out of scope (recap)

Already enumerated in §Out of scope above. Summary:

- E (Rekey lifecycle) and F (AdminReadStream).
- Hot-reload of `crypto.dual_control_required`.
- `crypto.totp_bootstrap_completed` and `crypto.totp_enrolled` emitters
  (no Phase-5 server-side caller).
- `@cluster status` / `@evict-member`.
- In-game grant UX for `crypto.operator`.

## Open questions

None at the close of this brainstorm. Sub-issues:

1. ~~OperatorAuthProvider topology~~ — server-side, separate Authenticate
   RPC + session token.
2. ~~Session TTL~~ — 10-min, in-memory ULID-keyed map.
3. ~~Player↔role resolution~~ — keep the role check (per
   `design-reviewer` Finding 1); D ships `RoleStore.PlayerHasRole(playerID,
   role)` helper (single SQL JOIN: `character_roles` × `characters` on
   `player_id`). The B-shipped `RoleAdmin AND crypto.operator` conjunction
   is preserved.
4. ~~Second-op CLI transport~~ — ConnectRPC over the same UDS socket as the
   primary; sub-command of the same binary.
5. ~~op_args_hash algorithm~~ — proto deterministic marshal + SHA-256;
   `google.golang.org/protobuf` version pinned in `go.mod` per INV-D18.
6. ~~admin_approvals TTL enforcement~~ — read-time `expires_at < now()`
   filter; defer reaper.
7. ~~Chain verify + emit ordering~~ — verify in Bootstrap; emit in new
   `CryptoPolicySubsystem` after AuditProjection.
8. ~~JCS canonicalizer~~ — `cyberphone/json-canonicalization` pinned at
   `v0.0.0-20241213102144-19d51d7fe467` with meta-test lock.
9. ~~Audit emission integration~~ — `AuditingService` decorator wrapping
   `totp.Service`.

## References

- `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` —
  master spec.
- `docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-decomposition.md` —
  Phase 5 decomposition (Decision 5/6/7; sub-epic D row).
- `docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-totp-substrate-design.md` —
  sub-epic A spec (R5 Option Y; D's audit-emission ownership inheritance).
- `docs/superpowers/specs/2026-05-08-event-payload-crypto-phase5-sub-epic-b-design.md` —
  sub-epic B spec (`crypto.operator` capability + 14 master-spec amendments).
- `docs/superpowers/specs/2026-05-09-phase5-sub-epic-c-admin-socket-design.md` —
  sub-epic C spec (UDS substrate D registers RPCs on).
- RFC 8785 — JSON Canonicalization Scheme.
- `internal/totp/audit.go` — A's reserved subject builders + payload structs
  D's decorator emits via.
- `internal/admin/socket/server.go` — C's `buildMux` extension point.
