<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Event Payload Cryptography — Phase 5 Decomposition

## Status

Draft.

## Authors

Sean Brandt; brainstormed with Claude.

## Date

2026-05-07

## Context

This document decomposes `holomush-jxo8` (Phase 5 of `holomush-e49r`, "Rekey
CLI + AdminReadStream + OperatorAuthProvider") into independently-shippable
sub-epics, and locks the cross-cutting substrate decisions that ripple
through every sub-epic's eventual design spec and plan.

Phase 5 is the operator break-glass + destructive Rekey body of work in the
event-payload-cryptography series. Most behavioral semantics are already
pinned by the master spec (`docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`)
in §5.9 (`OperatorAuthProvider`), §6.3 (`Rekey`), and §7.5
(`AdminReadStream`). What was left open and is settled here:

- "Wizard" terminology vs. existing `RoleAdmin`.
- TOTP enforcement policy (hard-required vs. config-bypass).
- Wire protocol for the localhost UDS admin socket.
- Dual-control protocol mechanics (how the second operator approves).
- Solo-admin break-glass on cloud-hosted servers.
- Cryptographic integrity for site-policy changes.

### What this document is

A **decomposition design**. It sets the shape, ordering, and substrate
choices for the sub-epics that compose Phase 5. It does **not** fully spec
each sub-epic; those get their own design specs as they reach Tier-1
status in the build order below.

### What this document is not

- A re-statement of master-spec semantics. Where master-spec text remains
  authoritative, this document cites it by section rather than repeating
  it.
- A spec for any of Phases 6, 7, or 8 (VaultTransitProvider, plugin SDK
  helpers, or site-documentation deliverables).
- A behavioral spec for the dual-control protocol's exact prompt text,
  audit-event payload schema, or proto definitions. Those land in the
  sub-epic specs that introduce them.

## Cross-cutting decisions

Seven decisions, locked in this brainstorm. Each names the master-spec
sections that need amendment as part of sub-epic B's PR (see
[Master-spec amendments](#master-spec-amendments) below).

### Decision 1 — Decompose Phase 5 into six sub-epics

Phase 5 spans five distinct subsystems (TOTP, capability, UDS substrate,
operator auth + dual-control, Rekey lifecycle, AdminReadStream). Treating
them as a single design spec and plan would produce a planning loop too
long for productive review. Each sub-epic gets its own design spec, plan,
and bead chain.

**Why:** independently-shippable work units. Tier-1 sub-epics (A, B, C)
have no dependencies on each other and can be developed in parallel.
Tier-2 (D) gates on all of Tier-1. Tier-3 (E, F) can run in parallel
after D. See [Sub-epic decomposition](#sub-epic-decomposition).

### Decision 2 — `s/wizard/admin/` and add `crypto.operator` capability

The master spec uses "wizard" as a role name in the break-glass auth path
(§5.9 step 5, §6.3 1.4, §7.5). The existing role taxonomy in
`internal/access/role.go` defines `RolePlayer`, `RoleBuilder`, and
`RoleAdmin` only — no `RoleWizard`. MUSH-`Wizard` ≈ HoloMUSH-`RoleAdmin`
semantically.

This document amends the master spec to use `admin` throughout. The
existing `wizard_transfer` event-verb in `player_character_bindings`
(see master spec §4.3a) is a different concept (player-handoff action,
not a permission flag) and is unaffected.

In addition, a new `crypto.operator` capability gates break-glass on top
of `RoleAdmin`. The operator must hold both `RoleAdmin` AND
`crypto.operator` to invoke Rekey or AdminReadStream. This is a
defense-in-depth knob: not every admin can do break-glass, only admins
who are explicitly designated crypto operators.

**Terminology note.** "Capability" here is **not** a `command.Capability`
(the `{Action, Resource, Scope}` tuple defined in `internal/command/`
for command-pre-flight authorization). The `crypto.operator` capability
is a **player-attribute grant** — a string flag held on a player ID, not
a command-authorization tuple. Sub-epic B's design spec MAY introduce a
more specific term (e.g., `crypto-operator grant` / `operator
allow-list`) if the collision causes confusion at implementation time.

**Why:** simpler than introducing a new role tier (no migration, no role
taxonomy churn), but preserves the spec's intent that break-glass is a
narrower cohort than general administration. The capability is the
narrowing mechanism. The threat-model row "compromised in-game admin"
(§1, line 137) becomes "compromised in-game admin with `crypto.operator`."

**How granted (v1):** config-file allow-list. In-game grant UX deferred
to a P3 follow-up bead. See [Defaults](#defaults-captured).

### Decision 3 — TOTP hard-required for break-glass

Master spec §5.9 step 4 currently allows soft TOTP (warn-and-proceed if
not enrolled, gated by `require_totp` per-operation config). This
document tightens the contract: **TOTP MUST be enrolled to invoke any
break-glass operation.** No config knob bypasses this. If TOTP is not
enrolled, the CLI refuses with a clear error directing the operator to
the enrollment path.

**Why:** the master spec's defense-in-depth claim depends on TOTP
actually being verified. A soft-required field renders the audit
field's `totp_verified` interpretation muddy and makes the threat-model
row 137 weaker than advertised. HoloMUSH has no production deployments
yet, so there's no operational debt to accommodate; ship the strong
default.

**Implication:** TOTP enrollment is a hard predecessor in the bead
chain. Sub-epic A (TOTP) blocks sub-epic D (OperatorAuthProvider)
which blocks E (Rekey) and F (AdminReadStream). Without enrollment,
break-glass is unusable.

### Decision 4 — Admin socket uses ConnectRPC over UDS

Master spec text "RekeyService over a localhost UNIX domain socket" and
"localhost UNIX socket" (§6.3, §7.5) does not pin the wire protocol.
This document picks **ConnectRPC over UDS**.

**Why:** matches the codebase pattern. ConnectRPC is the codebase's
client-tool transport (`internal/web/handler.go` for browser-facing
RPCs); raw gRPC is the server-to-server / plugin-host transport
(`internal/control/`, `pkg/plugin/`). The admin socket is operator-CLI
to server, which fits the client-tool idiom.

**Operational benefits:**

- `curl --unix-socket /var/run/holomush.sock https://localhost/admin.v1.AdminService/Status`
  works for liveness debugging without installing a special client.
- The codebase already generates ConnectRPC bindings for every proto.
  Layout: `pkg/proto/holomush/<svc>/v1/<svc>v1connect/<svc>.connect.go`
  (sub-package per service) next to the gRPC binding's
  `pkg/proto/holomush/<svc>/v1/<svc>_grpc.pb.go`. Sub-epic C will follow
  this layout for `admin/v1/admin.proto`. No new build step.
- Server-streaming is first-class on ConnectRPC; satisfies
  `AdminReadStream`.

**SO_PEERCRED capture:** implemented as a small `http.Handler`
middleware that introspects the underlying `*net.UnixConn` from the
request and attaches the peer-cred record to the context for downstream
audit emission. UDS-only; never network-exposed.

### Decision 5 — Dual-control via server-issued approval token

The master spec says `--dual-control` requires "second operator's
signoff within 5 min" (§6.3, §7.5) but does not pin the mechanics.
Three workable shapes were considered (inline prompt, server token,
file-based drop). This document picks **server-issued approval token**:

1. Primary operator runs `holomush crypto rekey ... --dual-control`.
2. Server creates a pending-approval row in `admin_approvals`
   (`request_id`, `primary_player_id`, `op_kind`, `op_args_hash`,
   `expires_at = now + 5 min`).
3. CLI prints `request_id` to the primary's terminal; primary
   communicates it out-of-band to the second operator (Slack, etc.).
4. Second operator runs `holomush admin approve <request_id>`.
5. Second operator authenticates via `OperatorAuthProvider` (creds, TOTP,
   role, and capability all verified). MUST have a different `player_id`
   from primary. MUST hold `RoleAdmin` AND `crypto.operator`.
6. Server marks the row approved.
7. Primary's still-blocking CLI proceeds to Rekey / AdminReadStream
   execution.

**Why:** clean process isolation (two operators, two CLIs, two
authentications); the server is the source of truth for approval state;
`request_id` is the audit-trail anchor; expiry is server-enforced.

### Decision 6 — Two-layer dual-control: per-invocation flag + per-site config

Two distinct policy layers:

**Layer 1 — per-invocation flag (operator choice):**

- `holomush crypto rekey ...` (no flag) → single-control flow.
- `holomush crypto rekey ... --dual-control` → operator chose to force
  two-op flow.

**Layer 2 — per-site config (server enforcement):**

```yaml
crypto:
  dual_control_required:
    - rekey                # server REJECTS rekey without --dual-control
    - admin_read_stream    # same
```

If an op is listed in `dual_control_required`, the server returns
`DENY_DUAL_CONTROL_REQUIRED` for any invocation without dual-control,
regardless of operator-supplied flags. Server enforcement always wins;
operator can choose **stronger** but never **weaker** than site policy.

**Operating modes:**

| Site policy                         | Operator flag      | Result                                       |
| ----------------------------------- | ------------------ | -------------------------------------------- |
| `dual_control_required: []`         | (no flag)          | ✅ single-control: creds + TOTP               |
| `dual_control_required: []`         | `--dual-control`   | ✅ dual-control (operator chose stronger)     |
| `dual_control_required: [rekey]`    | (no flag)          | ❌ rejected: `DENY_DUAL_CONTROL_REQUIRED`     |
| `dual_control_required: [rekey]`    | `--dual-control`   | ✅ dual-control                               |

**Solo-admin defense story — layered by adversary class.** The master
spec defines two distinct auth-tier adversary classes whose defense
profiles differ:

- **Master-spec row 134 — "Curious operator with shell access"**
  (master spec line 134, verbatim): "Inside the trust boundary; has DB
  and JetStream access". This adversary already has shell on the host.
- **Master-spec row 137 — "Compromised in-game wizard"** (master spec
  line 137, verbatim): "Inside the auth tier but **without shell
  access**". This adversary has stolen credentials at the auth tier but
  cannot reach the host shell. This is also the adversary class the
  decomposition spec amends to "Compromised in-game admin with
  `crypto.operator` capability" (see [Master-spec amendments](#master-spec-amendments)).

Master spec §5.9 lines 1276-1279 and §7.5 lines 1698-1701 are explicit
that `SO_PEERCRED` and host access are **not** defense factors against
an adversary who already has shell access — i.e., row 134. That
guidance is correct and is preserved here. The decomposition spec
addresses both rows separately:

| Adversary class                                          | Defense profile in single-control mode                                      |
| -------------------------------------------------------- | --------------------------------------------------------------------------- |
| Master-spec row 137: compromised admin **without shell** | Localhost-UDS topology denies reach (topological lockout, not an authentication factor); once reach is achieved, **creds + TOTP** are the authentication defense |
| Master-spec row 134: curious operator **with shell**     | Localhost-UDS topology denies nothing (the adversary is already on the host); **creds + TOTP** are the only authentication defense — two factors |

For the row-137 adversary, the localhost-UDS architecture is a
**topological lockout**: an adversary without shell access on the
holomush host cannot reach the admin socket at all. This is a property
of the deployment topology, not an authentication factor of
`SO_PEERCRED`; master spec §7.5 line 1714 is correct that
`SO_PEERCRED` itself is operational data, not a gate against the
shell-access adversary. The lockout reduces row-137's reachable
authentication surface to creds + TOTP.

For the row-134 adversary, the topology defends nothing. Single-control
reduces to **two authentication factors: creds + TOTP**. This holds
only if those two factors compromise via orthogonal attack surfaces
(password manager / phishing vs. authenticator app on a separate
device). For solo-admin sites that accept this risk,
`dual_control_required: []` is the appropriate setting; the defense
rests on the orthogonality of those two factors.

For sites where the row-134 adversary's two-factor compromise is
plausible (insider risk, larger admin pool, regulated environments),
`dual_control_required: [rekey, admin_read_stream]` is the appropriate
setting; dual-control adds a third factor (a different operator's
creds + TOTP via the `admin_approvals` token) which master spec §5.9
line 1279 already identifies as the high-sensitivity defense.

**Honest summary.** Single-control mode is **two authentication
factors** (creds + TOTP) against the master-spec row-134 adversary
(curious operator with shell). Against the row-137 adversary
(no-shell), the localhost-UDS topology is an additional topological
lockout, but the authentication surface is still two factors once
reach is achieved. Operators choosing single-control accept that the
orthogonality of creds and TOTP is the load-bearing assumption against
the row-134 adversary. Multi-admin sites SHOULD opt into dual-control
to add the third operator-mediated factor.

**Site-policy upgrade path.** Going from solo-admin (`dual_control_required: []`)
to multi-admin requires editing config and restarting the server. The
restart emits a `crypto.policy_set` audit event recording the change
(see Decision 7). Going back to solo also requires config edit, restart,
and a logged audit event. There is no per-invocation override for site
policy; site policy can't be defeated by a single compromised admin's
CLI invocation.

**Why the asymmetry (config + restart, not hot-reload).** Site-policy
changes are rare, deliberate operations. Restart is a synchronization
point that makes "the policy actually in effect" unambiguous; the
emitted `policy_set` event records it; the chain (Decision 7) makes
silent flips detectable. Hot-reload introduces a race between policy
read and policy enforcement. Out-of-scope for v1.

### Decision 7 — Hash-chained `crypto.policy_set` audit events

Every site-policy change is recorded as a hash-chained audit event in
`events_audit`, the existing crypto-blind cold-tier audit projection.

**Mechanism:**

1. **At every server startup** (and at every future config reload, when
   that arrives), the server computes the effective policy snapshot and
   emits a `crypto.policy_set` audit event:
   - Subject: `audit.<game>.system.crypto_policy.<policy_name>`
     (e.g., `crypto_policy.dual_control_required`)
   - Type: `crypto.policy_set`
   - Payload: full effective policy snapshot, hash of the current
     payload, `prev_hash` referencing the previous `policy_set` event
     for this `policy_name`, server identity, server-start ULID,
     timestamp.
2. Each Rekey or AdminReadStream audit event embeds the active
   `policy_hash` at invocation time (i.e., the hash of the most-recent
   `policy_set` event for the relevant `policy_name`).

**Forensic chain.** If an attacker silently flips policy (edit config,
restart, do bad thing, edit back, restart), two `policy_set` events show
the round-trip; the per-invocation `policy_hash` shows which policy was
effective at the moment of action.

**Tamper-evidence properties:**

| Adversary action                                      | Detection mechanism                                                                                  |
| ----------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| Silent policy flip (round-trip)                       | Round-trip visible in `policy_set` events; per-invocation `policy_hash` pins effective policy        |
| Silent edit of `events_audit` row                     | Already detected by existing JetStream byte-equality / cold-tier integrity check                     |
| Silent drop of a `policy_set` event                   | Subsequent `policy_set` event's `prev_hash` won't match its actual predecessor                       |
| Replay with deleted history                           | `prev_hash` chain verification fails on first orphaned event                                         |

**What this does not defend against** (out of scope, master-spec §1
threat model):

- An adversary with PG superuser AND host shell who edits both
  `events_audit` AND the JetStream-side stream consistently. Root-on-host
  is the trust path.
- A new server install with a fresh database — by definition there's no
  prior chain to verify against. The first `policy_set` after
  `migrate up` has `prev_hash = null` (genesis).

**Reuses existing infrastructure.** No new key material. The hash chain
is anchored in `events_audit`'s existing byte-equality-with-JetStream
durability (master spec §4.7, §8.1).

**Sub-epic D ownership.** The on-wire schema (field types, SHA-256
canonicalization, genesis-row handling), the chain-verifier startup
ordering with respect to the other §10 bootstrap-time integrity checks
(KEK unlock, INV-32, INV-33, INV-37), and the chain-verify CLI command
are all sub-epic D's responsibility. The amended master-spec §4.6 and
§10 rows (see [Master-spec amendments](#master-spec-amendments)) are the
authoritative anchor for that work.

## Sub-epic decomposition

Six sub-epics under `holomush-jxo8`. Each will get its own design spec
when it reaches Tier-1 status in the build order.

| Sub-epic | Goal                                                                                                                                                                         | Depends on |
| -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- |
| **A**    | TOTP substrate: `pquerna/otp` library, `player_totp` table, recovery codes, server verification API, `holomush admin totp enroll`/`bootstrap-enroll`/`reset`/`recover` CLIs | none       |
| **B**    | `crypto.operator` capability: constant in `internal/access/`, config-file grant list, OperatorAuthProvider's check logic; lands master-spec amendments                       | none       |
| **C**    | UDS admin socket substrate: ConnectRPC server bound to UNIX socket, `SO_PEERCRED` middleware, lifecycle wiring in `cmd/holomush/`, `admin.v1` proto skeleton                 | none       |
| **D**    | `OperatorAuthProvider` + `InGameCredentialsProvider` + `admin_approvals` table + dual-control protocol + `crypto.policy_set` audit-event emission                            | A, B, C    |
| **E**    | `DEKManager.Rekey` 7-phase mechanics + `crypto_rekey_checkpoints` table + cluster invalidation reuse + INV-39 stale-DEK fallback + `holomush crypto rekey` CLI                | D          |
| **F**    | `AdminReadStream` server-streaming ConnectRPC endpoint + pre-data audit emit + `holomush admin read-stream` CLI                                                              | D          |

### Build order

```text
Tier 1 (parallel):  A  |  B  |  C
Tier 2:             D
Tier 3 (parallel):  E  |  F
```

**Tier 1 sub-epics MAY be brainstormed and planned in parallel.** They
share no implementation dependencies. Sub-epic B's first PR carries the
master-spec amendments (see below).

**D is the chokepoint.** It integrates A's TOTP verification, B's
capability check, and C's UDS plumbing into a unified
`OperatorAuthProvider`, plus introduces the `admin_approvals` table and
the dual-control protocol. D ships before E or F can begin.

**E and F are independent leaves.** Both consume D's
`OperatorAuthProvider` for authentication but otherwise share no code.
They can be brainstormed and planned in parallel after D ships.

### Existing children — reparenting

| Bead              | Title                                                            | Action                                                                  |
| ----------------- | ---------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `holomush-jxo8.1` | `@cluster status` and `@evict-member` admin commands             | Reparent under sub-epic C (UDS substrate); they're admin-socket RPCs    |
| `holomush-jxo8.2` | Composite index on `events_audit (dek_ref, dek_version)`         | Stays under sub-epic E; profiling decision is part of Rekey work        |
| `holomush-ojw1.5` | INV-39 stale-DEK fallback (Phase 5 / Rekey-prep)                 | Subsumed into sub-epic E; close `ojw1.5` when E lands                   |
| `holomush-ojw1.6` | Client-visible stale-DEK signaling (typed Event field)           | Stays as P3 follow-up; reparent under sub-epic E for ownership clarity  |

## Defaults captured

Decisions made during the brainstorm but not surfaced as separate
questions, because they're either obvious choices or trivially
reversible. Listed here so each sub-epic's design spec inherits them.

### Sub-epic A — TOTP substrate

- **Library:** `pquerna/otp` (RFC 6238 compliant, battle-tested in Go
  ecosystem).
- **Storage:** separate `player_totp` table — not columns on `players`.
  Lets us add `enrolled_at`, `last_verified_at`, and recovery-code
  metadata without churning the players schema.
- **Bootstrap-enroll is once-only.** `holomush admin totp bootstrap-enroll
  <player_id>` works without operator authentication (host-shell only)
  for the **first** admin to enroll. After the first successful
  enrollment, bootstrap is permanently closed for the lifetime of the
  database. Closure is recorded as a hash-chained
  `crypto.totp_bootstrap_completed` audit event (same chaining mechanism
  as Decision 7's `policy_set` events).
- **Recovery codes are v1.** Each TOTP enrollment generates 10
  single-use recovery codes. Codes are Argon2id-hashed in
  `player_totp_recovery_codes`. CLI prints them once at enrollment time;
  operator stores them out-of-band.
- **Lost-device recovery flow:**
  - **Solo admin:** uses a recovery code via
    `holomush admin totp recover <player_id> --code <code>`. Validates
    the hash, marks the code consumed, clears the TOTP secret. Admin then
    re-enrolls via `holomush admin totp enroll` (which from this state
    works against an unenrolled account). Re-enrollment generates a fresh
    set of 10 recovery codes.
  - **Multi-admin site:** alternative path is
    `holomush admin totp reset <player_id>` invoked by a different admin.
    Requires that admin's full break-glass auth (creds + TOTP + role +
    capability). Belt-and-suspenders alongside recovery codes.
- **All recovery codes lost AND solo admin** → must restore from DB
  backup or accept the loss. Honest and consistent with how production
  secret-management tools handle this.

### Sub-epic B — `crypto.operator` capability

- **Storage (v1):** config-file allow-list.

  ```yaml
  crypto:
    operators:
      - "01HZAVGE83MGFEXQQH5SP9NXKF"  # player_id of admin Alice
      - "01HZAVGE83MGFEXQQH5SP9NXKG"  # player_id of admin Bob
  ```

- **In-game grant UX deferred** to a P3 follow-up bead. Sub-epic B
  files the bead at landing time.
- **OperatorAuthProvider check sequence:** verify player creds → verify
  TOTP → verify `RoleAdmin` → verify `crypto.operator` capability. All
  four MUST pass.

### Sub-epic D — Dual-control + `admin_approvals` table

- **Approval-table cleanup:** 5-minute TTL enforced via
  `expires_at < now()` filter on read; expired rows MAY be left until a
  later sweep. Periodic background reaper deferred (low row volume; rows
  are tiny).
- **Approval-token format:** ULID (collision-safe, sortable, naturally
  human-typable in the second-op CLI).
- **`op_args_hash` binding:** the approval row binds to a hash of the
  primary's invocation arguments. The primary's CLI sends the same args
  when proceeding after approval; if they don't hash-match the stored
  `op_args_hash`, the server rejects with `DENY_APPROVAL_ARGS_MISMATCH`.
  Prevents a primary from getting approval for "Rekey scene X" and then
  invoking "Rekey scene Y" with the same approval row.

## Master-spec amendments

The following edits to
`docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` land
**alongside sub-epic B's first PR** (the natural carrier, since B
introduces the capability gate referenced by these edits):

| Section                  | Amendment                                                                                                                              |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------- |
| §1 row 137 (threat model)| `s/Compromised in-game wizard/Compromised in-game admin with crypto.operator capability/`                                              |
| §1 (threat-model layering)| Add note that single-control break-glass is two-factor (creds + TOTP) against the **row-134** adversary (curious operator with shell access); localhost-UDS topology denies reach for the **row-137** adversary (auth-tier compromised admin without shell access), reducing row-137's authentication surface to the same two factors once reach is achieved; dual-control is the third-factor defense per §5.9 line 1279 |
| §4.6                     | Add `crypto.policy_set` audit-event shape: subject `audit.<game>.system.crypto_policy.<policy_name>`, type `crypto.policy_set`, payload fields (`policy_hash bytes`, `prev_hash bytes nullable` (null only at genesis), `server_start_ulid string`, `policy_snapshot json`, `server_identity string`, `timestamp`); pin hash algorithm to **SHA-256 over RFC 8785 JCS-canonicalized JSON** of the payload excluding the `policy_hash` field. Sub-epic D MUST select an RFC-8785-compliant Go canonicalizer (e.g., `github.com/cyberphone/json-canonicalization`) and pin the version in `go.mod`; switching canonicalizer libraries or RFC interpretations is a chain-breaking change and MUST be treated as a master-spec amendment, not a sub-epic-internal refactor |
| §4.6                     | Add `policy_hash bytes` field to existing `audit.<game>.system.rekey.*` and `audit.<game>.system.operator_read.*` audit-event shapes; references the active `policy_set` event at invocation time |
| §5.9                     | `s/wizard/admin/` throughout; add capability-check step; rewrite step 4 for hard-required TOTP                                         |
| §5.9 (new subsection)    | Add §5.9.1 documenting `crypto.operator` capability storage (config-file v1) and grant mechanism                                       |
| §6.3 1.4                 | `s/wizard role/admin role + crypto.operator capability/`                                                                               |
| §6.3 (new subsection)    | Add §6.3.1 documenting the dual-control protocol (server-issued approval token, second-op CLI)                                         |
| §7.5                     | `s/wizard/admin/` throughout; reference §6.3.1 for dual-control mechanics                                                              |
| §10 (Bootstrap-time table)| Add row: `policy_set chain verification failure on startup` — Detection: `prev_hash` of latest `policy_set` for `policy_name` does not match the actual predecessor's `policy_hash` — Behavior: server refuses to start (consistent with INV-32/INV-33 fail-closed pattern) |
| §10 (Operator and policy errors table)| Add rows: `DENY_DUAL_CONTROL_REQUIRED` (server enforcement of site `dual_control_required` policy), `DENY_APPROVAL_ARGS_MISMATCH` (primary's invocation args don't match approved `op_args_hash`), `DENY_POLICY_HASH_UNKNOWN` (Rekey/AdminReadStream invocation references a `policy_hash` not present in the chain) |
| §11.1 Phase 5            | Add `crypto.policy_set` audit-event emission, `admin_approvals` and `player_totp` and `crypto_rekey_checkpoints` tables to scope list  |
| §12                      | Strike "Decide on TOTP enrollment for wizard accounts" (resolved by Decision 3); add forward-pointer to this decomposition spec        |

The edits do not change semantics outside the break-glass auth path;
all participant-facing crypto invariants (INV-1 through INV-50+) remain
authoritative. The §4.6 audit-shape additions and §10 failure-mode rows
are owned by sub-epic D (which introduces the `policy_set` emit code
path); sub-epic D's design spec MUST cite these amended sections rather
than re-defining the schema locally.

## Open questions

None at the close of this brainstorm. Sub-epic specs will surface
sub-epic-local open questions during their own brainstorms.

## Prerequisites and dependencies

- **Master spec:** `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`
  (§5.9, §6.3, §7.5; amendments listed above).
- **Phase 3 (`holomush-ojw1`):** EventSink encrypt, AuthGuard,
  decrypt-on-fanout, and downgrade fence. Closed; foundation for all
  decryption-audit emission.
- **Phase 4 (`holomush-fi0n`):** Add + Rotate lifecycle ops. Closed;
  `DEKManager.Add` and `.Rotate` are the substrate Rekey extends.
- **`internal/auth.Service.ValidateCredentials`:** existing Argon2id
  password verification (timing-safe). Reused by sub-epic A's
  `InGameCredentialsProvider` for the credentials leg.
- **`internal/access/role.go`:** existing `RolePlayer`/`RoleBuilder`/
  `RoleAdmin` constants. Sub-epic B adds `crypto.operator` capability
  alongside this taxonomy.
- **`pkg/proto/holomush/`:** existing dual ConnectRPC + raw-gRPC binding
  generation. Sub-epic C adds `admin/v1/admin.proto` and generates both
  bindings (raw gRPC unused at this time but no extra cost).

## References

- `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` —
  master spec.
- `docs/superpowers/specs/2026-04-30-event-payload-crypto-phase2-substrate-design.md` —
  Phase 2 substrate (KEKProvider, `crypto_keys`, DEKManager skeleton).
- `docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3a-grounding.md`
  through `2026-05-03-event-payload-crypto-phase3d-grounding.md` —
  Phase 3 grounding decisions (cited where relevant in sub-epic specs).
- `docs/superpowers/specs/2026-05-06-phase4-add-rotate-design.md` —
  Phase 4 design (Add + Rotate).
- `docs/superpowers/specs/2026-05-05-history-reader-crypto-options-design.md` —
  history-reader crypto-options design (referenced by sub-epic F).
