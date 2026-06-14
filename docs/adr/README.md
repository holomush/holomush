<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Architecture Decision Records (ADRs)

This directory contains Architecture Decision Records (ADRs) documenting
significant design decisions made during HoloMUSH development. Each ADR
captures the context, options considered, decision made, and consequences
of architectural choices.

ADRs are immutable once accepted. If a decision is reversed, a new ADR
supersedes the old one; the bd decision record gains a `--type supersedes`
edge and the file's `**Status:**` reflects the supersession.

## Index

| Title | Date | Status | bd decision |
|-------|------|--------|-------------|
| [Atomic (big-bang) plugin-capability cutover over phased allowlist rollout](holomush-40ssh-atomic-big-bang-plugin-capability-cutover-over-phased-allowl.md) | 2026-06-14 | Accepted | `holomush-40ssh` |
| [Resolver Grants set as the single least-privilege gate authority](holomush-vpg8l-resolver-grants-set-as-single-least-privilege-gate-authority.md) | 2026-06-14 | Accepted | `holomush-vpg8l` |
| [o262d DAG-resolution failures stay always-fatal behind a named policy-function seam](holomush-ptf7b-o262d-dag-resolution-failures-stay-always-fatal-behind-named.md) | 2026-06-14 | Accepted | `holomush-ptf7b` |
| [Retire plugin capability injection only; keep ambient stdlib ungated](holomush-05f3v-retire-plugin-capability-injection-only-keep-ambient-stdlib.md) | 2026-06-14 | Accepted | `holomush-05f3v` |
| [Gate binary plugin capability injection on manifest declaration](holomush-m4ac3-gate-binary-plugin-capability-injection-manifest-declaration.md) | 2026-06-13 | Accepted | `holomush-m4ac3` |
| [Make binary plugin Init unconditional to close the degenerate-manifest escape](holomush-toh7a-make-binary-plugin-init-unconditional-close-degenerate-manif.md) | 2026-06-13 | Accepted | `holomush-toh7a` |
| [Two single-clause capability-enforcement invariants (INV-PLUGIN-54 / 55)](holomush-1psri-two-single-clause-capability-enforcement-invariants-inv-plug.md) | 2026-06-13 | Accepted | `holomush-1psri` |
| [Wholesystem census as the capability-declaration integration guard](holomush-wlyzs-wholesystem-census-as-capability-declaration-integration-gua.md) | 2026-06-13 | Accepted | `holomush-wlyzs` |
| [Defer Lua capability-declaration enforcement to eykuh.4 (binary-only now)](holomush-nk46j-defer-lua-capability-declaration-enforcement-eykuh-4-binary.md) | 2026-06-13 | Accepted | `holomush-nk46j` |
| [Stamp dispatch context host-side; never accept a wire subject](holomush-wvrtc-stamp-dispatch-context-host-side-never-accept-wire-subject.md) | 2026-06-12 | Accepted | `holomush-wvrtc` |
| [Capability access is a default-deny ABAC decision, not a set check](holomush-syhc2-capability-access-is-default-deny-abac-decision-not-set-chec.md) | 2026-06-12 | Accepted | `holomush-syhc2` |
| [access: and scope: are valid only on capability entries, not service entries](holomush-u1sdq-access-and-scope-are-valid-only-capability-entries-not-servi.md) | 2026-06-12 | Accepted | `holomush-u1sdq` |
| [Scope enforcement uses typed co-located extractors, not reflective broker introspection](holomush-afyzh-scope-enforcement-uses-typed-co-located-extractors-not-refle.md) | 2026-06-12 | Accepted | `holomush-afyzh` |
| [Extract HostCapabilities port; relocate host.v1 servers to runtime-neutral package](holomush-l5bqb-extract-hostcapabilities-port-relocate-host-v1-servers-runti.md) | 2026-06-12 | Accepted | `holomush-l5bqb` |
| [Split Lua bridge by proto ownership: codegen host caps, descriptor-driven plugin services](holomush-ws2mi-split-lua-bridge-by-proto-ownership-codegen-host-caps-descri.md) | 2026-06-12 | Accepted | `holomush-ws2mi` |
| [Per-plugin bufconn server for host-established Lua plugin identity](holomush-elqw4-per-plugin-bufconn-server-host-established-lua-plugin-identi.md) | 2026-06-12 | Accepted | `holomush-elqw4` |
| [Domain-primary carving axis for host capability services](holomush-nbscl-domain-primary-carving-axis-host-capability-services.md) | 2026-06-12 | Accepted | `holomush-nbscl` |
| [Dedicated holomush.plugin.host.v1 namespace; WorldService kept as coexisting domain service](holomush-e9go5-dedicated-holomush-plugin-host-v1-namespace-worldservice-kep.md) | 2026-06-12 | Accepted | `holomush-e9go5` |
| [Ambient runtime substrate below the capability model; retire the Log RPC](holomush-cryy2-ambient-runtime-substrate-below-capability-model-retire-log.md) | 2026-06-12 | Accepted | `holomush-cryy2` |
| [Clean cutover: delete PluginHostService and rewire the binary SDK in sub-spec 2](holomush-2fb90-clean-cutover-delete-pluginhostservice-and-rewire-binary-sdk.md) | 2026-06-12 | Accepted | `holomush-2fb90` |
| [Typed-entry requires model for plugin manifests (capability/service kind tags)](holomush-mg4x6-typed-entry-requires-model-plugin-manifests-capability-servi.md) | 2026-06-11 | Accepted | `holomush-mg4x6` |
| [Fail-fast fail-closed plugin-loader policy with structured ResolveResult](holomush-dfkca-fail-fast-fail-closed-plugin-loader-policy-structured-resolv.md) | 2026-06-11 | Accepted | `holomush-dfkca` |
| [Lua plugin capability/service transport via in-process gRPC over the host broker](holomush-gtkzy-lua-plugin-capability-service-transport-via-process-grpc-ove.md) | 2026-06-11 | Accepted | `holomush-gtkzy` |
| [KEK presence is the single activation gate for sensitive-event crypto](holomush-gkw77-kek-presence-is-single-activation-gate-sensitive-event-crypt.md) | 2026-06-09 | Accepted | `holomush-gkw77` |
| [Require a provisioned KEK to boot; auto-generate keyfile, never the passphrase](holomush-kddop-require-provisioned-kek-boot-auto-generate-keyfile-never-pas.md) | 2026-06-09 | Accepted | `holomush-kddop` |
| [DEK seed failure is FATAL and refuses scene focus](holomush-mihtk-dek-seed-failure-is-fatal-and-refuses-scene-focus.md) | 2026-06-09 | Accepted | `holomush-mihtk` |
| [Seed comms recipients host-side at the publisher genesis boundary](holomush-olpdd-seed-comms-recipients-host-side-at-publisher-genesis-boundar.md) | 2026-06-09 | Accepted | `holomush-olpdd` |
| [Scene DEK genesis happens lazily on first SetSceneFocus](holomush-r90tl-scene-dek-genesis-happens-lazily-first-setscenefocus.md) | 2026-06-09 | Accepted | `holomush-r90tl` |
| [Use player-scoped workspace over terminal focus-pivot for scenes](holomush-wf4zj-use-player-scoped-workspace-over-terminal-focus-pivot-scenes.md) | 2026-06-07 | Accepted | `holomush-wf4zj` |
| [Observer auto-join preserves INV-SCENE-60 participant-list boundary](holomush-zukuh-observer-auto-join-preserves-inv-scene-60-participant-list-b.md) | 2026-06-07 | Accepted | `holomush-zukuh` |
| [Scene log reads ride QueryStreamHistory; no plugin-side ReadSceneLog](holomush-pc3bg-scene-log-reads-ride-querystreamhistory-no-plugin-side-reads.md) | 2026-06-07 | Accepted | `holomush-pc3bg` |
| [All web scene paths use BFF RPCs; hybrid public-proxy rejected](holomush-b0365-all-web-scene-paths-use-bff-rpcs-hybrid-public-proxy-rejecte.md) | 2026-06-07 | Accepted | `holomush-b0365` |
| [scene_activity uses in-consumer control-frame downgrade, not a new stream](holomush-0qnnr-scene-activity-uses-consumer-control-frame-downgrade-not-new.md) | 2026-06-07 | Accepted | `holomush-0qnnr` |
| [Canonical wire event-type is plugin-qualified &lt;plugin&gt;:&lt;verb&gt;](holomush-yl3mf-canonical-wire-event-type-is-plugin-qualified-plugin-verb.md) | 2026-06-07 | Accepted | `holomush-yl3mf` |
| [Three-vocabulary event-type model: bare crypto/registered, qualified wire/verbs, one bridge](holomush-8aure-three-vocabulary-event-type-model-bare-crypto-registered-qua.md) | 2026-06-07 | Accepted | `holomush-8aure` |
| [Qualify core-scenes event types end-to-end (no backward-compat transformation layer)](holomush-1gwns-qualify-core-scenes-event-types-end-end-no-backward-compat-t.md) | 2026-06-07 | Accepted | `holomush-1gwns` |
| [INV-<SCOPE>-<N> canonical invariant naming convention](holomush-6wcf2-adr-inv-scope-n-canonical-invariant-naming-convention.md) | 2026-05-31 | Accepted | `holomush-6wcf2` |
| [Paired YAML+markdown invariant registry with drift meta-test](holomush-4v2dq-adr-paired-yaml-markdown-invariant-registry-drift-meta-test.md) | 2026-05-31 | Accepted | `holomush-4v2dq` |
| [Derive world-read subject host-side; no subject field on wire](holomush-nthq6-derive-world-read-subject-host-side-no-subject-field-wire.md) | 2026-05-30 | Accepted | `holomush-nthq6` |
| [Remove forgeable injectable WorldService; PluginHostService Query is the sole binary world-read path](holomush-c6oo8-remove-forgeable-injectable-worldservice-pluginhostservice-q.md) | 2026-05-30 | Accepted | `holomush-c6oo8` |
| [Isolate xk6 extensions in a separate Go module](holomush-2344h-isolate-xk6-extensions-separate-go-module.md) | 2026-05-28 | Accepted | `holomush-2344h` |
| [Use k6 + Custom xk6 Binary for Full-System Load Testing](holomush-evggu-use-k6-custom-xk6-binary-full-system-load-testing.md) | 2026-05-28 | Accepted | `holomush-evggu` |
| [Use topic-tab navigation over a single grouped sidebar](holomush-q924m-use-topic-tab-navigation-over-a-single-grouped-sidebar.md) | 2026-05-28 | Accepted | `holomush-q924m` |
| [Organize docs audience-first with Diátaxis modes within](holomush-md3k4-organize-docs-audience-first-di-taxis-modes-within.md) | 2026-05-28 | Accepted | `holomush-md3k4` |
| [Use autogenerate sidebar over explicit entries](holomush-38kmt-use-autogenerate-sidebar-over-explicit-entries.md) | 2026-05-28 | Accepted | `holomush-38kmt` |
| [Adopt Astro Starlight as the docs site platform](holomush-145ko-adopt-astro-starlight-as-docs-site-platform.md) | 2026-05-27 | Accepted | `holomush-145ko` |
| [Render Mermaid diagrams client-side via Starlight plugin](holomush-xneg2-render-mermaid-diagrams-client-side-via-starlight-plugin.md) | 2026-05-27 | Accepted | `holomush-xneg2` |
| [Use bun as the docs-site package manager](holomush-qf2oo-use-bun-as-docs-site-package-manager.md) | 2026-05-27 | Accepted | `holomush-qf2oo` |
| [integration/E2E are CI-authoritative-and-required, local-optional](holomush-5k6au-integration-e2e-are-ci-authoritative-and-required-local-opti.md) | 2026-05-26 | Accepted | `holomush-5k6au` |
| [quarantine flaky specs via governed registry, not deletion](holomush-5eqiv-quarantine-flaky-specs-via-governed-registry-not-deletion.md) | 2026-05-26 | Accepted | `holomush-5eqiv` |
| [Separate authorization (WHO) from business-state validity (WHEN) in scene policies](holomush-sqpnv-authz-who-vs-business-state-when-scene-policies.md) | 2026-05-25 | Accepted | `holomush-sqpnv` |
| [Add host Evaluate RPC for per-action plugin authorization](holomush-dttdj-host-evaluate-rpc-per-action-plugin-authz.md) | 2026-05-25 | Accepted | `holomush-dttdj` |
| [Derive Evaluate subject host-side; no subject field on wire](holomush-qeypl-host-derived-evaluate-subject.md) | 2026-05-25 | Accepted | `holomush-qeypl` |
| [Scope plugin Evaluate entitlement to owned resource types](holomush-61rdl-evaluate-entitlement-owned-resource-types.md) | 2026-05-25 | Accepted | `holomush-61rdl` |
| [Make plugin authorization gates structural via gated subcommand dispatcher](holomush-9l9pu-structural-gated-subcommand-dispatcher.md) | 2026-05-25 | Accepted | `holomush-9l9pu` |
| [Drop in-memory session.Store fake; test against real Postgres](holomush-bozv-drop-session-memstore-test-against-postgres.md) | 2026-05-23 | Accepted | `holomush-bozv` |
| [Denormalize Pose-Order Metadata Against scene_log Source of Truth](holomush-r4th-denormalize-pose-order-metadata.md) | 2026-05-19 | Accepted | `holomush-r4th` |
| [Migrate Scene Subjects to NATS Dot-Style Atomically With Plugin Emit Code](holomush-s9nu-scene-subject-atomic-migration.md) | 2026-05-19 | Accepted | `holomush-s9nu` |
| [Classify All Scene Content Events as sensitivity:always, Including OOC](holomush-sb3n-scene-content-sensitivity-always.md) | 2026-05-19 | Accepted | `holomush-sb3n` |
| [Generalize Plugin-Code Participant Gate from Scene-Log to All Participant-Only Scene RPCs](holomush-nt2d-participant-gate-pattern-generalized.md) | 2026-05-19 | Accepted (supersedes `holomush-c8a9`) | `holomush-nt2d` |
| [Extend sceneAuditLogStore With Operation-Specific InsertScenePose for Transactional Atomicity](holomush-1ang-audit-interface-operation-specific-tx.md) | 2026-05-19 | Accepted | `holomush-1ang` |
| [Snapshot RPC as Source of Truth for Current-State Presence](holomush-da2q-snapshot-rpc-source-of-truth-presence.md) | 2026-05-19 | Accepted | `holomush-da2q` |
| [Current-State Presence Snapshot Exempt from I-PRIV-1 Temporal Floor](holomush-o46k-presence-snapshot-exempt-from-priv-floor.md) | 2026-05-19 | Accepted | `holomush-o46k` |
| [Introduce list_presence ABAC Action with Default-Deny and Same-Location Seed](holomush-lp65-list-presence-abac-action.md) | 2026-05-19 | Accepted | `holomush-lp65` |
| [Plugin Manifests Opt-In to history_scope (vs Spec's Exempt-List Framing)](holomush-jhl5-plugin-history-scope-opt-in.md) | 2026-05-17 | Accepted | `holomush-jhl5` |
| [Hard-Gate (Current-Location-Only) for Location-Stream History Reads](holomush-wxty-hard-gate-location-stream-history.md) | 2026-05-17 | Accepted | `holomush-wxty` |
| [Per-Session Attach Intervals on SessionInfo for Multi-Session Continuity](holomush-rc8b-per-session-attach-intervals.md) | 2026-05-17 | Accepted | `holomush-rc8b` |
| [Session-Store Sync Hook on Character Move](holomush-kmac-session-store-sync-hook-character-move.md) | 2026-05-17 | Accepted | `holomush-kmac` |
| [In-Process Filter-at-Delivery as Load-Bearing Privacy Gate; NATS-as-Source-of-Truth for Consumer Config](holomush-ghpx-filter-at-delivery-and-nats-source-of-truth.md) | 2026-05-17 | Accepted | `holomush-ghpx` |
| [Use Init-RPC Protocol Extension to Communicate Code-Registered Emit Types](holomush-vie9-init-rpc-emit-type-communication.md) | 2026-05-17 | Accepted | `holomush-vie9` |
| [Scope Lua Load Capture Pass to crypto.emits-Declaring Plugins Only](holomush-7h0c-lua-load-pass-optin-scope.md) | 2026-05-17 | Accepted | `holomush-7h0c` |
| [Split Plugin SDK into eventkit and groupkit by Scope](holomush-p7w0-split-plugin-sdk-eventkit-groupkit.md) | 2026-05-16 | Accepted | `holomush-p7w0` |
| [Require N=2 Consumer Validation Before SDK Primitive Extraction](holomush-lrt3-n2-consumer-validation-sdk-extraction.md) | 2026-05-16 | Accepted | `holomush-lrt3` |
| [Strict Plugin-Boundary: Plugins Must Not Modify internal/](holomush-z1e7-strict-plugin-boundary.md) | 2026-05-16 | Accepted | `holomush-z1e7` |
| [Startup-Time Set-Equality Validation of crypto.emits Declarations](holomush-3vsb-manifest-emit-type-startup-validation.md) | 2026-05-16 | Accepted | `holomush-3vsb` |
| [Enforce Scene Privacy at Plugin Code, Not ABAC Engine](holomush-c8a9-scene-privacy-plugin-code-enforcement.md) | 2026-05-16 | Superseded by `holomush-nt2d` | `holomush-c8a9` |
| [Use suiteT Capture Pattern Instead of GinkgoT() for testing.TB](holomush-1f1w-suitet-capture-pattern-ginkgo-testing-tb.md) | 2026-05-16 | Accepted | `holomush-1f1w` |
| [Remap INV-Pinned Test\* Functions to Ginkgo Suite Entries on Migration](holomush-iv7l-remap-inv-pinned-tests-ginkgo-suite-entries.md) | 2026-05-16 | Accepted | `holomush-iv7l` |
| [AdminReadStream Bypasses HistoryReader/Dispatcher](holomush-8f2x-adminreadstream-bypasses-historyreaderdispatcher.md) | 2026-05-12 | Accepted | `holomush-8f2x` |
| [Custom Go-Native ABAC Engine](holomush-kokk-custom-go-native-abac-engine.md) | 2026-02-05 | Accepted | `holomush-kokk` |
| [Cedar-Aligned Fail-Safe Type Semantics](holomush-iv43-cedar-aligned-fail-safe-type-semantics.md) | 2026-02-05 | Accepted | `holomush-iv43` |
| [Deny-Overrides Without Priority Ordering](holomush-501i-deny-overrides-without-priority-ordering.md) | 2026-02-05 | Accepted | `holomush-501i` |
| [Eager Attribute Resolution with Per-Request Caching](holomush-fvn5-eager-attribute-resolution-per-request-caching.md) | 2026-02-05 | Accepted | `holomush-fvn5` |
| [Properties as First-Class World Model Entities](holomush-xx3e-properties-as-first-class-world-model-entities.md) | 2026-02-05 | Accepted | `holomush-xx3e` |
| [Direct StaticAccessControl Replacement](holomush-7kvy-direct-staticaccesscontrol-replacement.md) | 2026-02-05 | Accepted | `holomush-7kvy` |
| [Three-Layer Player Access Control](holomush-0tq6-three-layer-player-access-control.md) | 2026-02-05 | Accepted | `holomush-0tq6` |
| [PostgreSQL LISTEN/NOTIFY for Policy Cache Invalidation](holomush-5z2y-postgresql-listennotify-policy-cache-invalidation.md) | 2026-02-05 | Accepted | `holomush-5z2y` |
| [Use Opaque Session Tokens Instead of Signed JWTs](holomush-ydti-use-opaque-session-tokens-instead-signed-jwts.md) | 2026-02-02 | Accepted | `holomush-ydti` |
| [Argon2id Password Hashing](holomush-4x7x-argon2id-password-hashing.md) | 2026-02-02 | Accepted | `holomush-4x7x` |
| [Player-Character Authentication Model](holomush-ex40-player-character-authentication-model.md) | 2026-02-02 | Accepted | `holomush-ex40` |
| [Timing-Attack Resistant Authentication](holomush-8qbl-timing-attack-resistant-authentication.md) | 2026-02-02 | Accepted | `holomush-8qbl` |
| [Command State Management for Multi-Turn Interactions](holomush-8j8q-command-state-management-multi-turn-interactions.md) | 2026-02-02 | Proposed (Deferred to post-v1) | `holomush-8j8q` |
| [Unified Command Registry](holomush-5nu7-unified-command-registry.md) | 2026-02-02 | Accepted | `holomush-5nu7` |
| [Command Security Model](holomush-vi5e-command-security-model.md) | 2026-02-02 | Superseded by `holomush-7kvy` | `holomush-vi5e` |
| [Command Conflict Resolution](holomush-ogb4-command-conflict-resolution.md) | 2026-02-02 | Accepted | `holomush-ogb4` |

<!-- BEGIN MIGRATION MAP -->

## Migration map (2026-05-14)

The legacy `NNNN-<slug>.md` numbering was retired in favor of
bd-decision IDs. Stubs at the old paths preserve external references.

| Legacy | bd decision | Current file |
|--------|-------------|--------------|
| ADR 0001 | `holomush-ydti` | [holomush-ydti-use-opaque-session-tokens-instead-signed-jwts.md](holomush-ydti-use-opaque-session-tokens-instead-signed-jwts.md) |
| ADR 0002 | `holomush-4x7x` | [holomush-4x7x-argon2id-password-hashing.md](holomush-4x7x-argon2id-password-hashing.md) |
| ADR 0003 | `holomush-ex40` | [holomush-ex40-player-character-authentication-model.md](holomush-ex40-player-character-authentication-model.md) |
| ADR 0004 | `holomush-8qbl` | [holomush-8qbl-timing-attack-resistant-authentication.md](holomush-8qbl-timing-attack-resistant-authentication.md) |
| ADR 0005 | `holomush-8j8q` | [holomush-8j8q-command-state-management-multi-turn-interactions.md](holomush-8j8q-command-state-management-multi-turn-interactions.md) |
| ADR 0006 | `holomush-5nu7` | [holomush-5nu7-unified-command-registry.md](holomush-5nu7-unified-command-registry.md) |
| ADR 0007 | `holomush-vi5e` | [holomush-vi5e-command-security-model.md](holomush-vi5e-command-security-model.md) |
| ADR 0008 | `holomush-ogb4` | [holomush-ogb4-command-conflict-resolution.md](holomush-ogb4-command-conflict-resolution.md) |
| ADR 0009 | `holomush-kokk` | [holomush-kokk-custom-go-native-abac-engine.md](holomush-kokk-custom-go-native-abac-engine.md) |
| ADR 0010 | `holomush-iv43` | [holomush-iv43-cedar-aligned-fail-safe-type-semantics.md](holomush-iv43-cedar-aligned-fail-safe-type-semantics.md) |
| ADR 0011 | `holomush-501i` | [holomush-501i-deny-overrides-without-priority-ordering.md](holomush-501i-deny-overrides-without-priority-ordering.md) |
| ADR 0012 | `holomush-fvn5` | [holomush-fvn5-eager-attribute-resolution-per-request-caching.md](holomush-fvn5-eager-attribute-resolution-per-request-caching.md) |
| ADR 0013 | `holomush-xx3e` | [holomush-xx3e-properties-as-first-class-world-model-entities.md](holomush-xx3e-properties-as-first-class-world-model-entities.md) |
| ADR 0014 | `holomush-7kvy` | [holomush-7kvy-direct-staticaccesscontrol-replacement.md](holomush-7kvy-direct-staticaccesscontrol-replacement.md) |
| ADR 0015 | `holomush-0tq6` | [holomush-0tq6-three-layer-player-access-control.md](holomush-0tq6-three-layer-player-access-control.md) |
| ADR 0016 | `holomush-5z2y` | [holomush-5z2y-postgresql-listennotify-policy-cache-invalidation.md](holomush-5z2y-postgresql-listennotify-policy-cache-invalidation.md) |
| ADR 0017 | `holomush-8f2x` | [holomush-8f2x-adminreadstream-bypasses-historyreaderdispatcher.md](holomush-8f2x-adminreadstream-bypasses-historyreaderdispatcher.md) |

<!-- END MIGRATION MAP -->

## Format

See `docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md`
§"ADR format (unified)" for the canonical template. All ADRs use one
format: Context, Decision, Rationale, Alternatives Considered,
Consequences, References.

## Template

New ADRs are written by the `/capture-adrs` skill, which renders from
the spec's format definition. To write one manually, follow the same
shape and use `bd create -t decision --validate` to file the record.

## Writing guidelines

| Guideline                 | Description                                                                                              |
| ------------------------- | -------------------------------------------------------------------------------------------------------- |
| **Immutability**          | ADRs are permanent records — do not edit accepted ADRs to change decisions                               |
| **Supersession**          | To reverse a decision, create a new ADR and mark the old one as "Superseded by `<bd-id>`"                |
| **RFC2119 keywords**      | Use MUST/SHOULD/MAY in consequences when describing implementation requirements                          |
| **Comprehensive options** | Document ALL options considered, not just the chosen one                                                 |
| **Trade-off clarity**     | Consequences should honestly capture both benefits and costs                                             |
| **Future-proof**          | Assume readers in 5 years won't have context — explain everything                                        |

## References

- [Michael Nygard's ADR template](https://github.com/joelparkerhenderson/architecture-decision-record)
- [ADR Tools GitHub](https://github.com/npryce/adr-tools)
- [RFC 2119: Key words for RFCs](https://www.ietf.org/rfc/rfc2119.txt)
