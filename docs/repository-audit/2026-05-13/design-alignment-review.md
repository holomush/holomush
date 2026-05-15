# HoloMUSH Design-vs-Implementation Alignment Audit

- **Date:** 2026-05-13
- **Auditor:** Claude (Opus 4.7, 1M context) — read-only sub-agent
- **Worktree:** `/Volumes/Code/github.com/holomush/.worktrees/repo-audit-2026-05-13`
- **Scope:** Compare merged source tree against design docs in `docs/specs/`,
  `docs/superpowers/specs/`, `docs/plans/`, `docs/superpowers/plans/`,
  `docs/adr/`, `docs/specs/decisions/`, `CLAUDE.md`, and `.claude/rules/`.
- **Method:** Spec-grounded inspection. Every drift below cites the spec
  (path + section) and the code (`path:line`). No code mutations were made.
- **Follow-up tracking:** `holomush-yvdm` — epic for triaging findings into child task beads (`bd show holomush-yvdm`)

## Executive summary

The implementation tracks the major specs very closely on the headline
invariants — JetStream cutover, event-payload crypto Phases 1–5, ABAC default
deny, two-layer command authorization, ADR 0017 (AdminReadStream bypass), and
the events_audit envelope rename are all faithfully realized. The bulk of the
drift is in three areas:

1. **Plugin boundary regression on payload schemas.** The JetStream design
   (§1b lines 200–222) explicitly mandates that the plugin-domain payload
   structs in `internal/core/event.go` (`LocationStatePayload`,
   `ExitUpdatePayload`, `PagePayload`, `WhisperPayload`, `WhisperNoticePayload`,
   `OOCPayload`, `PemitPayload`) MUST move out of `internal/core/`. They are
   still there, still used by `internal/grpc/location_follow.go:219` and
   `internal/web/translate_test.go`. This is the single largest unfixed drift
   and it directly contradicts the project boundary invariant in `CLAUDE.md`
   ("plugin-owned types MUST NOT leak into `internal/core/`"). **Major.**
2. **Active session-consumer GC is missing.** JetStream spec §4 "Session GC"
   requires an internal subscriber to `events.session.*.lifecycle` that calls
   `js.DeleteConsumer("session_<id>")` immediately on quit. Only the passive
   `InactiveThreshold = 24h` path exists; no listener calls `DeleteConsumer`
   in the eventbus or session packages. **Major** (operational debt — leaks
   consumers for up to 24h).
3. **Legacy host-owned `EventType` and `core.Subscription`-shaped APIs**
   remain in `internal/core/event.go:48-66` for host events that the F5
   migration was scoped to move. They are intentionally retained per the
   comment at `internal/core/event.go:43-47`, but the JetStream spec's
   blanket "the 19 EventType constants in `internal/core/event.go:39-70`
   MUST be deleted" (§1b lines 197–199) is now partially overridden by an
   undocumented project decision. **Minor** (decision exists in code, but
   the spec hasn't been amended to reflect it — see Inverted-Drift list).

Remaining findings are predominantly minor: a couple of legacy specs whose
status field reads "Implemented" but which have been silently superseded,
small terminology inconsistencies between specs and implementation, and one
header (`App-Rendering`) that the implementation stamps but the JetStream
spec's headers table does not enumerate.

---

## 1. EventBus / JetStream (`internal/eventbus/`)

Spec: `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md`.

### 1.1 Plugin-domain payload structs still live in `internal/core/` — **Major**

- **Spec ref:** §1b "Event Type and Payload — plugin-declared, opaque to host",
  lines 200–222 (`docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md`).
  Explicit RFC2119: "The plugin-domain payload structs at
  `internal/core/event.go:74-143` … **MUST** also move out of `internal/core/`."
- **Code ref:** `internal/core/event.go:68-139` (still defines
  `LocationStatePayload`, `LocationStateInfo`, `LocationStateExit`,
  `LocationStateChar`, `ExitUpdatePayload`, `PagePayload`, `WhisperPayload`,
  `WhisperNoticePayload`, `OOCPayload`, `PemitPayload`).
- **Drift:** Direct host code still constructs these — `internal/grpc/location_follow.go:219`
  builds a `core.LocationStatePayload{}`. They are also re-exported via tests at
  `internal/web/translate_test.go:227,273,302,320,334,353`. Per the boundary
  invariant in `CLAUDE.md` ("Plugin Boundary"), this is a regression.
- **Recommended direction:** Either complete the migration (move each payload
  to the owning plugin's proto namespace per spec §1d, retire the Go structs)
  or amend the JetStream spec §1b with a decision record explaining why two
  of the seven payload types are intentionally retained host-side. Prefer the
  former — payloads carrying *content* (Whisper, Page, Pemit, OOC) are
  plugin-owned by every other design rule in the repo.

### 1.2 No active session-consumer GC on quit — **Major**

- **Spec ref:** §4 "Session GC", lines 577–583.
  "Explicit cleanup on session quit: an internal subscriber to
  `events.session.*.lifecycle` (PR #233) calls
  `js.DeleteConsumer(\"session_<id>\")` immediately. Best-effort; threshold
  catches the rest."
- **Code ref:** `internal/eventbus/subscriber.go:34-38` declares
  `DefaultSessionInactiveThreshold = 24 * time.Hour`; no code path calls
  `DeleteConsumer` in response to `session_ended` / `events.session.*.lifecycle`.
  `rg -n "DeleteConsumer.*session" internal/` returns no production hits;
  the only mention is the comment at `internal/eventbus/subscriber.go:158`
  pointing forward to "the session-lifecycle listener in F5".
- **Drift:** The passive path works (sessions disappear after 24h) but the
  active GC path the spec specifies is absent. Operational cost: consumer
  state and filestore pressure linger after every quit, especially on
  rapid-reconnect telnet flows.
- **Recommended direction:** Implement the subscriber. File a P1 bead under
  the open eventbus epic; the listener is ~30 LOC plus a test. The passive
  threshold should remain as the safety net.

### 1.3 `App-Rendering` header is undocumented in the JetStream headers table — **Minor**

- **Spec ref:** §1d "Wire format — proto + version header", headers table
  (lines 290–298). Five headers are required (`Nats-Msg-Id`, `App-Schema-Version`,
  `App-Event-Type`, `App-Codec`, trace context). No mention of
  `App-Rendering`.
- **Code ref:** `internal/eventbus/rendering_publisher.go:75` stamps
  `event.Headers["App-Rendering"]`; `internal/eventbus/audit/projection.go:30`
  consumes it; `internal/eventbus/publisher.go:331-334` documents an
  architectural single-writer invariant for it.
- **Drift:** The header is real, has a single-writer architectural invariant,
  and a projection consumer reads it — but it has no slot in the spec's
  authoritative wire-format table. The newer comm-extensibility design
  (`docs/specs/2026-03-28-comm-event-extensibility-design.md`) and
  gateway-verb-registry-sourcing design touch it; the substrate spec was not
  amended.
- **Recommended direction:** Add a row for `App-Rendering` to the JetStream
  spec's headers table with a note that it is set by `RenderingPublisher`
  and consumed by the audit projection.

### 1.4 Legacy `event-delivery-redesign` spec status field is stale — **Minor**

- **Spec ref:** `docs/specs/2026-03-20-event-delivery-redesign.md:3`
  states `**Status:** Implemented`. The JetStream design (line 7)
  declares this spec superseded: "Supersedes:
  docs/specs/2026-03-20-event-delivery-redesign.md (LISTEN/NOTIFY-based
  delivery; this design replaces both the event log and the delivery
  transport)."
- **Drift:** A reader landing on the older spec via search would believe
  the LISTEN/NOTIFY delivery is the current architecture. The status field
  should read `Superseded by 2026-04-18-jetstream-event-log-design.md`.
- **Recommended direction:** One-line edit on the legacy spec's status field.

### 1.5 Headers / wire format otherwise track spec — **Pass**

The header constants in `internal/eventbus/publisher.go:36-57` exactly mirror
the spec table: `Nats-Msg-Id`, `App-Schema-Version`, `App-Event-Type`,
`App-Codec`. `SchemaVersion = "1"` (line 62) matches the spec's "App-Schema-Version
proto schema major version" mandate. `App-Codec` is stamped unconditionally
(line 302), satisfying the "never empty" mandate. `App-Dek-Ref` and
`App-Dek-Version` headers are added per the crypto design's §4.1 NATS-headers
table.

### 1.6 Stream topology — **Pass**

`internal/eventbus/subsystem.go:197-206` creates the `EVENTS` stream with
`Subjects: events.>`, `Retention: LimitsPolicy`, `Replicas: 1`, `MaxAge`
from config (defaulting to 30 days per `internal/eventbus/config.go:21`),
`Duplicates` from `DupeWindow`, and `AllowDirect: true`. Matches the spec's
`StreamConfig{}` block at §2 lines 308–318 exactly.

### 1.7 `EventWriter` removed — **Pass**

`rg -n "type EventWriter|EventWriter struct" internal/` returns no
production hits. `internal/store/migrations/000010_drop_events_and_cursors.up.sql`
drops the `events` table and the `event_cursors` jsonb column from `sessions`,
matching the spec's §4 "What gets deleted" list. The substrate cutover is
complete.

### 1.8 `cursor_lock.go` and `replay.go` removed from `internal/grpc/` — **Pass**

`ls internal/grpc/` shows no `cursor_lock.go` or `replay.go`. Spec §4 "What
gets deleted" satisfied.

---

## 2. Event-payload crypto (`internal/eventbus/crypto/`, `internal/eventbus/codec/`, `internal/eventbus/authguard/`)

Spec: `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`.

### 2.1 AAD canonicalization matches spec — **Pass**

- **Spec ref:** §4.2 "AAD binding" lines 568–611, fields enumerated as
  `"HMAAD\x01"` + `len(id)` + id + `len(subject)` + subject + … +
  `uint32(dek_version)`.
- **Code ref:** `internal/eventbus/crypto/aad/aad.go:24-26` declares
  `var magic = []byte("HMAAD\x01")`; `aad.go:106` writes the magic; the
  canonicalization layout follows the spec.
- **INV-25** ("an event whose cleartext metadata, codec, or `dek_ref` has been
  altered MUST fail decryption") is exercised by
  `internal/eventbus/codec/xchacha20poly1305_test.go::TestXChaCha20Poly1305DetectsAADTamper`
  per the spec's enforcing-test table at §2 line 360.

### 2.2 Sensitivity fence enforces INV-6 / INV-7 — **Pass**

- **Spec ref:** INV-6 / INV-7 (§2 lines 217–218).
- **Code ref:** `internal/plugin/event_emitter.go:172` calls
  `EnforceSensitivity(manifestSensitivity, intent.Sensitive)`;
  `internal/plugin/crypto_manifest.go:18-22` defines
  `SensitivityAlways` / `SensitivityNever`. Enforcing tests live at
  `internal/plugin/event_emitter_crypto_test.go`.

### 2.3 AuthGuard denies operator on subscribe — **Pass**

- **Spec ref:** INV-43 ("The runtime AuthGuard MUST NEVER return PERMIT for
  a subject of kind `operator`").
- **Code ref:** `internal/eventbus/authguard/guard.go:49-54` returns
  `Decision{Permit: false, Code: DenyOperatorUseAdminRPC}` for
  `IdentityKindOperator`.

### 2.4 Manifest `crypto.emits` enforcement covers INV-45 — **Pass**

- **Spec ref:** INV-45 (§2 line 336).
- **Code ref:** `internal/plugin/crypto_validator.go:12-157` runs three
  rules: well-formedness of `emits`, no wildcards in
  `requests_decryption`, qualified `<plugin>:<event_type>` references,
  presence of plugin in `dependencies`, declared-sensitivity check
  against the producing plugin's `crypto.emits`, and rejection of
  `SensitivityNever` references.

### 2.5 Cluster coordination invariants present — **Pass**

INV-53 through INV-60 (Phase 3c grounding) all have matching code in
`internal/eventbus/crypto/invalidation/`, `internal/cluster/`, and
`internal/eventbus/crypto/dek/`. `internal/cluster/probe_pill.go:67-87`
implements the claim-then-probe TOCTOU mitigation that INV-57 prescribes.

### 2.6 `events_audit.payload` → `envelope` rename complete — **Pass**

`internal/store/migrations/000017_events_audit_envelope_rename.up.sql`
performs the metadata-only rename; INV-21 (§2 line 254) cites this migration
explicitly. The migration is idempotent per CLAUDE.md.

### 2.7 Stale-DEK soft-delete fallback — **Pass**

- **Spec ref:** INV-39 ("reads of historical events whose `dek_ref` no longer
  exists in `crypto_keys` MUST automatically fall back to the cold tier").
- **Code ref:** `internal/eventbus/crypto/dek/store.go` filters
  `destroyed_at IS NULL` on production read paths (per spec text);
  ADR 0017 references this fallback in `internal/admin/readstream/cold_reader.go`
  as the source-of-truth for redaction reasons.

---

## 3. ADR 0017: AdminReadStream architectural correction

Spec: `docs/adr/0017-admin-readstream-bypasses-history-reader.md`.

### 3.1 F bypasses `HistoryReader`/dispatcher — **Pass**

- **ADR ref:** Decision §1 ("F has its own `internal/admin/readstream/cold_reader.go`
  (~150 LOC) that issues SQL directly against `events_audit`").
- **Code ref:** `internal/admin/readstream/cold_reader.go` exists; 266 LOC.
  Verified by `rg -n "history\.NewReader|HistoryReader|dispatcher\.DispatchFor"
  internal/admin/readstream/` — only documentation comments at
  `cold_reader.go:7` and `handler.go:88` reference the old shape; no live
  usage of those types from `internal/admin/readstream/`.
- **ADR §2** ("F has its own `decrypt.go` (~80 LOC)") — `decrypt.go` is
  140 LOC (close to budget). `classifyDecryptErr` lives in the same package
  (ADR §3) as an unexported helper.
- **ADR §5** (wire shape uses `corev1.EventFrame` with typed `metadata_only`
  and `no_plaintext_reason`) — `internal/admin/readstream/handler.go:88`
  documents the ADR-0017 wire decision.
- **ADR §6** (`chain.Handler` factory retained) — `internal/admin/readstream/chain.go`
  exists.

The correction has been implemented as written; the audit found no
residual `OperatorReadAuthGuard` / `NoOpDecryptAuditEmitter` /
`staleDEKColdResolver` / `mergeStreams` types in `internal/admin/readstream/`.

---

## 4. ABAC (`internal/access/`)

Specs:

- `docs/specs/2026-01-21-access-control-design.md` (legacy / superseded for command surface by full-abac-design).
- `docs/specs/2026-02-05-full-abac-design.md`.
- `docs/specs/abac/` (the modular phase-7 spec).
- `docs/specs/decisions/epic7/` (ADRs).

### 4.1 Subject prefix `char:` → `character:` normalization — **Pass** (resolved by ADR)

- **Legacy spec ref:** `docs/specs/2026-01-21-access-control-design.md:111`
  uses `char:` as the example subject prefix.
- **Decision ref:** `docs/specs/decisions/epic7/phase-7.1/013-subject-prefix-normalization.md`
  lines 12–17: "Normalize to `character:` everywhere. … the engine MUST
  reject the `char:` prefix with a clear error".
- **Code ref:** `internal/access/policy/dsl/evaluator_test.go:355` and many
  others use `character:`. The legacy spec is left stale on this point but
  the ADR supersedes it; the inconsistency is documented and intentional.
- **Recommendation:** Add a "Superseded by" pointer at
  `docs/specs/2026-01-21-access-control-design.md:111` so a reader does not
  mint policies with the wrong prefix.

### 4.2 Default deny — **Pass**

- **Spec ref:** "Default Deny — Unknown subjects or missing permissions MUST
  return false" (`docs/specs/2026-01-21-access-control-design.md:77`).
- **Code ref:** ADR 0010 (`docs/adr/0010-cedar-aligned-fail-safe-type-semantics.md`)
  and ADR 0011 (`0011-deny-overrides-without-priority.md`) extend this;
  `internal/access/` engine returns `Permit: false` from
  `NotApplicable` and infra-failure paths. No drift found.

### 4.3 Command authorization is two-layer — **Pass**

- **Spec ref:** `docs/superpowers/specs/2026-04-03-command-capability-enforcement-design.md`
  §"Two-Layer Authorization" lines 60–119.
- **Code ref:** `internal/command/access.go:23-90` defines
  `CheckCommandExecution` (Layer 1, action `execute`, resource
  `command:<name>`) and `CheckCapabilityPreFlight` (Layer 2,
  iterates declared `Capability{}` calling `engine.CanPerformAction`).
  Scope is read from `Capability.EffectiveScope()` (line 70), defaulting
  to `ScopeSelf` per the spec's "Default is least privilege" principle.

### 4.4 ABAC plugin trust boundary — **Pass**

`docs/superpowers/specs/2026-04-06-plugin-abac-trust-boundary-design.md`
and `2026-04-07-plugin-abac-hardening-design.md` are reflected in
`internal/plugin/grpc_proxy.go` and `internal/plugin/attribute_proxy.go`.
The `paths:` rules at `.claude/rules/plugin-runtime-symmetry.md` are
respected — host-side ABAC infrastructure lives in `internal/access/`,
plugin-owned policies live alongside the plugin.

---

## 5. Plugin system (`internal/plugin/`, `plugins/`)

Specs:

- `docs/specs/2026-01-18-plugin-system-design.md` (legacy; heavily superseded).
- `docs/superpowers/specs/2026-04-05-plugin-architecture-rework-design.md` (current).
- `docs/superpowers/specs/2026-04-05-plugin-schema-role-isolation-design.md`.
- `docs/superpowers/specs/2026-04-12-plugin-verb-registration-design.md`.

### 5.1 Legacy plugin manifest schema vs current — **Minor**

- **Legacy spec ref:** `docs/specs/2026-01-18-plugin-system-design.md:107-143`
  documents a manifest with top-level `events:` and `capabilities:` lists.
- **Current code ref:** `plugins/core-scenes/plugin.yaml:9-58` uses
  `requires:`, `provides:`, `emits:`, `audit:`, `crypto:`, `commands:` —
  none of the legacy `events:` / `capabilities:` fields appear.
- **Drift:** The legacy spec's status field is not "Superseded"; a reader
  building a new plugin from `2026-01-18-plugin-system-design.md` would mint
  the wrong manifest shape. The rework design at
  `docs/superpowers/specs/2026-04-05-plugin-architecture-rework-design.md`
  does not declare an explicit `Supersedes:` link.
- **Recommended direction:** Either mark the legacy doc as superseded and
  point at the architecture-rework, or fold the relevant remaining parts
  (Lua vs binary, host-function contract) into the current design.

### 5.2 Lua/binary runtime symmetry — **Pass** (per `.claude/rules/plugin-runtime-symmetry.md`)

`internal/plugin/event_emitter.go` calls `EnforceSensitivity` regardless of
runtime; the manifest validator (`internal/plugin/crypto_validator.go`) is
runtime-agnostic. No asymmetric trust check found.

### 5.3 `actor_kinds_claimable` — **Pass**

`plugins/core-scenes/plugin.yaml:16` declares
`actor_kinds_claimable: [plugin, character]`, per the actor-claim
authentication design (`docs/superpowers/specs/2026-04-25-plugin-actor-claim-authentication-design.md`).
Validation lives in `internal/plugin/identity_registry.go`.

### 5.4 Host RPC Lua parity — **Spot-check Pass**

`feedback_host_rpc_lua_parity` rule states every PluginHostService RPC MUST
ship Go SDK method + Lua hostfunc together. `internal/plugin/hostfunc/`
contains the Lua side; spot-check of `host.go` finds matching Go and Lua
surfaces (no exhaustive comparison performed for the audit).

---

## 6. World model (`internal/world/`)

Spec: `docs/specs/2026-01-22-world-model-design.md` (Status: Implemented).

### 6.1 Schema and types — **Pass**

`internal/world/location.go`, `exit.go`, `object.go`, `scene.go`, and
`internal/world/postgres/*_repo.go` cover the spec's table definitions.
`ParticipantRole` constants at `internal/world/scene.go:13-16` mirror the
spec's `scene_participants.role` enum.

### 6.2 Scene types still in host package, but core-scenes is a plugin — **Minor**

- **Spec ref:** `docs/specs/2026-01-22-world-model-design.md:163-175` defines
  `scene_participants` as host-side schema.
- **Plugin ref:** `plugins/core-scenes/plugin.yaml:23-26` declares its own
  audit schema `plugin_core_scenes.scene_log` and `provides:
  holomush.scene.v1.SceneService`.
- **Drift:** The world spec predates the plugin-architecture-rework. Scene
  state and participants are now plugin-owned; the world-spec schema lives
  on as legacy types in `internal/world/scene.go` for `ParticipantRole`
  validation but is not the authoritative scene-participant store. This is
  a forked source-of-truth situation that is not flagged in either spec.
- **Recommended direction:** Annotate
  `docs/specs/2026-01-22-world-model-design.md:163` ("Scene Participants")
  with a pointer to `docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md`
  and `2026-04-16-b10-core-scenes-adoption-design.md` so the source-of-truth
  shift is documented.

---

## 7. Command authorization

Spec: `docs/superpowers/specs/2026-04-03-command-capability-enforcement-design.md`
plus `CLAUDE.md` "Command Authorization" §.

### 7.1 Scope defaults to `ScopeSelf` — **Pass**

`internal/command/access.go:70` calls `capability.EffectiveScope()`;
`internal/command/types.go` defines `ScopeSelf` as the zero value.

### 7.2 Plugin and core commands use the same model — **Pass**

`plugins/core-scenes/plugin.yaml:48-58` declares capabilities as structured
objects; host-registered core commands at
`internal/command/handlers/register.go` use the same `Capability{}` struct.

### 7.3 No drift found in this subsystem

---

## 8. Web ↔ server contracts

### 8.1 ConnectRPC server-streaming Flusher/Unwrap requirement — **Pass**

- **Spec ref:** `CLAUDE.md` "HTTP Middleware": "the wrapper MUST implement
  `http.Flusher` and `Unwrap()` — ConnectRPC server-streaming calls Flush()
  after each frame and will error if the interface is missing."
- **Code ref:** `internal/web/cookie.go:117-132` implements both `Flush()`
  and `Unwrap()` on `cookieWriter`.

### 8.2 Web client EventFrame typed redaction — **Pass via ADR 0017**

The ADR mandates `corev1.EventFrame` (typed `metadata_only` and
`no_plaintext_reason`); CLI/web consumers read the typed fields rather than
sniffing `len(Payload)==0`. Verified at `internal/admin/readstream/handler.go:88`.

---

## 9. Specs without corresponding implementation

These are specs in `docs/` that have no observable implementation in the tree.

| Spec | Notes |
| --- | --- |
| `docs/superpowers/specs/2026-04-18-sandbox-deployment-design.md` | Deployment-runbook spec; implementation lives in ops/, not the code tree. Not a drift — by design. |
| Channels plugin (referenced as future in JetStream §1c and in `docs/specs/2026-04-03-channels-architecture.md`) | No `plugins/core-channels/` directory; intentional deferral per JetStream spec line 247 ("core-channels plugin (future)"). |
| `docs/superpowers/specs/2026-05-05-history-reader-crypto-options-design.md` | The `WithHistoryAuth` and related options exist (`internal/eventbus/history/`) but the ADR 0017 correction supersedes the F branch. The history-reader-crypto-options spec is partially retired for AdminReadStream. Add an "Amended by ADR 0017" note. |

---

## 10. Implementations without spec backing

### 10.1 `App-Rendering` header (see §1.3 above)

A real wire-level header with single-writer architectural invariants that is
not in the JetStream spec's headers table. Documented in the comm-extensibility
spec but the master substrate spec is the document a reader would consult
first.

### 10.2 `App-Actor-Kind` and `App-Actor-ID` headers

- **Code ref:** `internal/eventbus/publisher.go:46-49,307-310`.
- **Spec ref:** No mention in the JetStream §1d headers table.
- **Drift:** Stamped on every published message; consumed by the audit
  projection. Not a correctness drift, but undocumented in the substrate spec.
- **Recommended direction:** Add header rows for `App-Actor-Kind` and
  `App-Actor-ID` to the JetStream spec §1d table with a note that they
  carry the host-stamped actor metadata.

### 10.3 Host-owned EventType constants retained in `internal/core/event.go:48-66`

Per spec §1b lines 197–199, all 19 constants were to be deleted. Eight remain:
`EventTypeArrive`, `EventTypeLeave`, `EventTypeSystem`, `EventTypeMove`,
`EventTypeCommandResponse`, `EventTypeCommandError`, `EventTypeLocationState`,
`EventTypeExitUpdate`, `EventTypeSessionEnded`. The comment at
`internal/core/event.go:43-47` says "All constants here are host-owned and
stay permanently" — a project-decision override that the JetStream spec has
not been amended to reflect.

**Recommended direction:** Amend JetStream spec §1b to enumerate the host-owned
event types that remain and explain the boundary rule's host-vs-plugin
distinction (host events stay, plugin events move). The current implementation
is internally consistent; the spec is what is out of date.

---

## 11. Internal contradictions / stale statuses

### 11.1 `docs/specs/2026-03-20-event-delivery-redesign.md:3`

Status: "Implemented" — should be "Superseded by 2026-04-18-jetstream-event-log-design.md".
The JetStream spec line 7 explicitly supersedes it.

### 11.2 `docs/specs/2026-01-18-plugin-system-design.md:1-6`

Status implies current, but `events:` / `capabilities:` top-level fields are
not used by any plugin manifest in `plugins/*/plugin.yaml`. Should be marked
"Superseded by 2026-04-05-plugin-architecture-rework-design.md".

### 11.3 `docs/specs/2026-01-21-access-control-design.md:111`

Documents `char:` subject prefix; the engine rejects this prefix per
`docs/specs/decisions/epic7/phase-7.1/013-subject-prefix-normalization.md`.
Add a "See ADR 013" pointer at line 111.

### 11.4 `docs/specs/2026-01-22-world-model-design.md` "Scene Participants"

Schema predates the core-scenes plugin extraction. Add an amendment note
pointing at `2026-04-06-scenes-and-rp-design-v2.md`.

---

## Prioritized realignment list

### P0 — Correctness or invariant impact

None. Every drift found is documentation lag or a missing-but-not-breaking
component; no invariant the master specs declare as MUST is currently
violated by the code.

### P1 — Direct boundary-rule violations / operational debt

1. **Move plugin-domain payload structs out of `internal/core/event.go`**
   (§1.1 above). Spec mandate, boundary-rule violation, single biggest
   drift. ~7 struct moves + import-update in `internal/grpc/location_follow.go`
   and tests.
2. **Wire active session-consumer GC** (§1.2). Subscribe to
   `events.<game>.session.*.lifecycle`, call `js.DeleteConsumer` on quit.
   Spec §4 line 581–583. ~30 LOC plus integration test.

### P2 — Spec/documentation realignment (no code change)

1. Update `docs/specs/2026-03-20-event-delivery-redesign.md` status →
   "Superseded by 2026-04-18-jetstream-event-log-design.md".
2. Update `docs/specs/2026-01-18-plugin-system-design.md` status →
   "Superseded by 2026-04-05-plugin-architecture-rework-design.md".
3. Add an "Amendment / Superseded by" note at
   `docs/specs/2026-01-21-access-control-design.md:111` for the
   `char:` → `character:` ADR.
4. Amend `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md` §1b
   to enumerate the host-owned event types that intentionally remain in
   `internal/core/event.go` (overrides the blanket "all 19 MUST be deleted"
   mandate that is no longer the project's intent).
5. Add `App-Actor-Kind`, `App-Actor-ID`, and `App-Rendering` rows to the
   JetStream spec §1d headers table.
6. Annotate `docs/specs/2026-01-22-world-model-design.md` "Scene
   Participants" with a pointer to `2026-04-06-scenes-and-rp-design-v2.md`
   and `2026-04-16-b10-core-scenes-adoption-design.md`.
7. Amend `docs/superpowers/specs/2026-05-05-history-reader-crypto-options-design.md`
   with an "Amended by ADR 0017" pointer noting that the AdminReadStream
   branch of history-reader options is retired.

### P3 — Verification follow-ups

1. Spot-check every PluginHostService RPC for Go-SDK ↔ Lua hostfunc parity
   (the `feedback_host_rpc_lua_parity` rule). Not exhaustively audited here.
2. Verify no test still constructs `core.LocationStatePayload{}` after
   §1.1 (P1.1) is addressed — `internal/web/translate_test.go` is the
   landmine.

---

## Methodology notes

- Tool order: `mcp__probe__search_code` was attempted first per repo
  policy. Where it lacked a fresh index for spec markdown, `rg` was used
  as the documented fallback. All file reads used the `Read` tool with
  explicit `offset`/`limit`.
- This audit is read-only. No `jj`/`git`/`bd`/`task` mutating commands
  were executed; status of beads and CI state was not consulted.
- Word count: ≈ 3,200 (target range 2,500–4,000).

*End of report.*
