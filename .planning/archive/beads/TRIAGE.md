# Beads migration triage — 2026-07-09

Disposition of every non-closed bead at export time (508 records), from the
beads → GitHub Issues + GSD migration. Method: **code-grounded audit** (parallel
bead-auditor agents verifying each bead's primary claim against current code) for
all bugs, P1s, and in_progress beads (140); **metadata triage** (recency, parent-epic
state, label heuristics, judgment pass) for the rest (368).

| Route | Count | Meaning |
| --- | --- | --- |
| GitHub issue | 179 | Valid, discrete, actionable — filed as a GH issue (see table) |
| Backlog | 95 | Strategic/epic-shaped — consolidated into `.planning/ROADMAP.md` 999.x backlog entries |
| Verified done | 26 | Code-grounded evidence the work already landed |
| Stale | 11 | Premise no longer true / target superseded |
| Duplicate | 2 | Duplicate of another bead |
| Archive only | 195 | Deferred/aged-out/low-priority — recoverable from the export |

## Filed as GitHub issues

| Issue | Bead | P | Title |
| --- | --- | --- | --- |
| #4596 | `holomush-0agf` | P1 | E5.7.1 Telnet connect observability gap — refreshCharacterList errors swallowed |
| #4597 | `holomush-0sc.11` | P1 | E2E tests for channel commands |
| #4598 | `holomush-1nl7` | P1 | TestProjectionDrainsPublishedMessageToAuditTable flake — AwaitDrained cold-start race |
| #4599 | `holomush-2lfdl` | P1 | TestPostgresUpdateSessionConnection_LockAcquisitionOrder_NoDeadlock hangs ~10min on gfo6-drain branch |
| #4600 | `holomush-2vfy` | P1 | XSS: ansi_up output used with @html in AnsiRenderer.svelte — input not pre-escaped |
| #4601 | `holomush-3lt5w` | P1 | ListPublishedScenes, GetPublicSceneArchive, DownloadPublicSceneArchive skip BeginServiceDispatch |
| #4602 | `holomush-3y6l` | P1 | engine.Evaluate audit-log failures downgraded to Warn with no signal to callers |
| #4603 | `holomush-4m7x` | P1 | ABAC integration test fixtures missing engine wiring |
| #4604 | `holomush-71zq.12` | P1 | Design issue: SIGKILL of holder PID does not release lock if descendants survive |
| #4605 | `holomush-7b9n` | P1 | F-E12 chain-verification E2E flake: operator_read_completed audit row times out under load (10s Eventually too tight) |
| #4606 | `holomush-brlb` | P1 | No IP-based rate limiting on authentication endpoints |
| #4607 | `holomush-dqd1` | P1 | iwzt amended-spec privacy tests: ReattachWithinTTL + TTLExpiryFreshFloor |
| #4608 | `holomush-eii4` | P1 | Observability server: failed Shutdown restores running=true with HTTP server in undefined state |
| #4609 | `holomush-jsna` | P1 | Plugin manager + hostfunc error-path test gaps (lifecycle + access) |
| #4610 | `holomush-llds` | P1 | test: design proper D11 canonical lock-order test (current test is shape-insufficient) |
| #4611 | `holomush-o32z` | P1 | gRPC control server: running flag set in NewGRPCServer constructor before Start() |
| #4612 | `holomush-pz68` | P1 | Multi-surface quit confirmation UX |
| #4613 | `holomush-q55b` | P1 | Flaky integration test: TestProjectionResumesAfterRestart races on consumer-info eventual consistency |
| #4614 | `holomush-r20h` | P1 | Wire active session-consumer GC on quit (DeleteConsumer subscriber) |
| #4615 | `holomush-tnxo` | P1 | GRPCServer concurrent lifecycle race tests (Start + Stop) |
| #4616 | `holomush-ub4o` | P1 | ABAC Phase 7.7 production bug cluster (resolver re-entrance, circuit breaker, schema validation) |
| #4617 | `holomush-vakk` | P1 | Move plugin-domain payload structs out of internal/core/event.go |
| #4618 | `holomush-w4mf4` | P1 | web: authed terminal route not responsive on mobile — TopBar header overflows, disconnect-screen text clips, terminal pane collapses to a narrow strip |
| #4619 | `holomush-wm0fi.1` | P1 | ConnectionFocusCache: generation-guarded focus cache with FIFO size cap |
| #4620 | `holomush-wm0fi.2` | P1 | Cache-first FocusReader + focus_cache hit/miss metrics |
| #4621 | `holomush-wm0fi.3` | P1 | Caching session.Store decorator evicting at the write chokepoint |
| #4622 | `holomush-wm0fi.4` | P1 | Wire the focus cache decorator into the core stack (prod + harness) |
| #4623 | `holomush-wm0fi.5` | P1 | Integration test: focus cache invalidates on a committed focus change |
| #4624 | `holomush-wm0fi.6` | P1 | Bind INV-SCENE-69 in the invariant registry |
| #4625 | `holomush-zao2` | P1 | Create docs/reference/testing-guide.md — testify vs ginkgo patterns + examples |
| #4626 | `holomush-zjru` | P1 | ABAC engine: SESSION_INVALID PolicyID should use deny: prefix, not infra: |
| #4627 | `holomush-zxij` | P1 | ABAC Phase 7.7 integration & unit test gaps |
| #4628 | `holomush-0f5w` | P2 | Recalibrate benchmark baselines (BenchmarkAttributeResolution, BenchmarkSinglePolicyEvaluation) |
| #4629 | `holomush-0sc.13` | P2 | core-channels: target-character ABAC binding is best-effort (defense-in-depth gap, WR-01) |
| #4630 | `holomush-0sc.18` | P2 | core-channels: ban/kick does not immediately evict live delivery (T-01-20, moderation gap) |
| #4631 | `holomush-0z62` | P2 | cleanup: backfill unit tests for Phase 5 Lua + gRPC client paths (codecov debt) |
| #4632 | `holomush-16rt5` | P2 | Lua ambient session-stream hostfuncs bypass the stream-subscribe fence |
| #4633 | `holomush-16y9p` | P2 | Audit non-Internal gRPC error leaks in core-scenes (FailedPrecondition %v on oops) |
| #4634 | `holomush-1rcjo` | P2 | Add parse-only mermaid lint to the docs lane (catch broken diagrams pre-merge) |
| #4635 | `holomush-201s9` | P2 | render-adr emits YAML frontmatter title that fails repo markdown lint |
| #4636 | `holomush-23q2w` | P2 | Bring service-kind capability injection under the resolver grant set (consistency with brokered RequiredCapabilities) |
| #4637 | `holomush-2p1bx` | P2 | CI: cache e2e web toolchain (pnpm) + verify Namespace profile cache toggles |
| #4638 | `holomush-3d9o` | P2 | Scenes: in-band signal when reconnect drops focus due to revoked membership |
| #4639 | `holomush-44n9` | P2 | Policy attribute validation at compile time |
| #4640 | `holomush-4fad` | P2 | Plugin manifest alias format validation |
| #4641 | `holomush-4gchp` | P2 | control.proto Shutdown: graceful field logged but not actioned |
| #4642 | `holomush-5nyq` | P2 | T30: Integration tests for full ABAC flow (Phase 7.7) |
| #4643 | `holomush-5rh.32` | P2 | scene composer ignores leading pose/say/OOC sigils — sends raw text, leaking ':' / '"' into content |
| #4644 | `holomush-61jl` | P2 | Drop HTTP/1.1: gateway requires HTTP/2 for all connections |
| #4645 | `holomush-62gy` | P2 | Review polymorphic command capability declarations |
| #4646 | `holomush-6m9rr` | P2 | Flaky E2E recurrence: scenes.spec.ts 'second tab does not disturb terminal stream' — CI-budget branch may not apply |
| #4647 | `holomush-6ofl` | P2 | Startup coverage for cmd/holomush/{core,sub_grpc}.go |
| #4648 | `holomush-6uvc` | P2 | stream_registry honors explicit ReplayMode instead of silently downgrading |
| #4649 | `holomush-7kl40` | P2 | cryptoActive can be false despite provisioned KEK when rekey wiring deps incomplete |
| #4650 | `holomush-7z87` | P2 | Phase 3b INV-22/25 history decrypt integration tests |
| #4651 | `holomush-8igzp` | P2 | SceneComposer stuck disabled when setSceneFocus rejects — surface focus-set failure + retry affordance |
| #4652 | `holomush-8im0k` | P2 | Plugin does not react to character deletion — orphan participant + vote-roster rows accumulate |
| #4653 | `holomush-9kx6` | P2 | Centralize E2E test helper for register + create character + enter game |
| #4654 | `holomush-a4wj` | P2 | plugin_consumer.go has 0% unit coverage — exercised via E2E only |
| #4655 | `holomush-arxu` | P2 | Update site docs: terminal UI, glossary, architecture invariants |
| #4656 | `holomush-bd32` | P2 | InactiveThreshold not validated against SessionTTL: events can be lost mid-disconnect |
| #4657 | `holomush-bpir8.2` | P2 | PoseCard.svelte contentWarning branch not exercised by any test |
| #4658 | `holomush-bpir8.3` | P2 | CommunicationLine.svelte channel-prefix branches for say and pose have no test coverage |
| #4659 | `holomush-cv1qh` | P2 | Acquire a project-tuned Svelte + shadcn UI dev skill (encode web/CLAUDE.md design-system conventions) |
| #4660 | `holomush-dj95.10` | P2 | Document permitted Lua/binary asymmetries in plugin-runtime-symmetry.md |
| #4661 | `holomush-e77ti.1` | P2 | TestPluginServerAdapterInitInjectsBothEventSinkAndFocusClient asserts non-nil injection only — no end-to-end sink exercised |
| #4662 | `holomush-ec22.10` | P2 | Code quality: standardize on oops at boundaries — three packages still use bare fmt.Errorf |
| #4663 | `holomush-ec22.11` | P2 | Code quality: hostfunc context-fallback duplicated 13+ sites — apply existing helpers |
| #4664 | `holomush-ec22.12` | P2 | Code quality: magic values + silent cursor-encode failures |
| #4665 | `holomush-ec22.13` | P2 | Tests: replace ~16 time.Sleep async-sync sites with deterministic patterns |
| #4666 | `holomush-ec22.14` | P2 | Tests: ~20 string-match error assertions should use error codes (errutil.AssertErrorCode / errors.Is) |
| #4667 | `holomush-ec22.18` | P2 | Docs: site/docs/contributing/architecture.md describes pre-cutover model (stale) |
| #4668 | `holomush-ec22.19` | P2 | Docs/code: terminology sweep — room still in canonical command help and player-facing strings |
| #4669 | `holomush-ec22.20` | P2 | Docs: per-package doc.go for high-traffic packages + plugin developer onboarding docs |
| #4670 | `holomush-ec22.24` | P2 | Build: repo hygiene — dual renovate.json, tracked lock file, stray issues.jsonl |
| #4671 | `holomush-ec22.25` | P2 | Build/CI: add drift detection — mocks, proto codegen, buf breaking-change |
| #4672 | `holomush-ec22.26` | P2 | Build/CI: pinning discipline outliers — corepack pnpm, arduino/setup-task, compose images |
| #4673 | `holomush-ec22.27` | P2 | DX: task dev requires full Docker rebuild — add backend-only iteration loop |
| #4674 | `holomush-ec22.6` | P2 | Architecture cleanup: dead engine handlers + EventStoreAdapter + manager/server god-objects |
| #4675 | `holomush-ec22.7` | P2 | Plugin runtime hardening: proxy goroutine cleanup, Lua ctx watchdog, mTLS-by-default |
| #4676 | `holomush-ec22.8` | P2 | Auth audit trail completeness: thread UA/IP through web+telnet to AuthenticatePlayer; add per-IP caps |
| #4677 | `holomush-ep3a0` | P2 | Pin buf binary version consistently across local + CI to prevent masked proto-format Lint failures |
| #4678 | `holomush-fu0r` | P2 | Attribute resolver panic-recovery log uses Error not ErrorContext (distributed-trace gap) |
| #4679 | `holomush-fux3` | P2 | Scenes: scene_idle_nudge background trigger implementation |
| #4680 | `holomush-gik1` | P2 | Reserved plugin names — block 'system' / 'world-service' from manifest validation |
| #4681 | `holomush-gkhn` | P2 | Add event and session Prometheus metrics |
| #4682 | `holomush-hdnx` | P2 | iwzt: I-PRIV-6 floor-preservation arm — assert staff sees only post-LocationArrivedAt events |
| #4683 | `holomush-hf64u` | P2 | Integration: hoist per-It full-stack Start to BeforeAll (privacy/presence/scenes) |
| #4684 | `holomush-hfvc` | P2 | Disconnect path deletes guest sessions immediately, preventing page-reload reattach |
| #4685 | `holomush-iwkgn` | P2 | seam-guard.test.ts describe name overclaims --mush-* positive assertion for PoseCard and CommunicationRenderer |
| #4686 | `holomush-jb1ec` | P2 | Meta-test walker descends into .claude/worktrees, causing spurious INV-FS-4 failures |
| #4687 | `holomush-k2hbr` | P2 | Extend INV-ROPS-3 colon-stream scan to web/e2e Playwright specs (*.ts) |
| #4688 | `holomush-knnrj` | P2 | Wire scene creation + core-scenes command path into the integrationtest harness |
| #4689 | `holomush-kriap` | P2 | rumdl tooling drift: brew installs 0.2.4 but CI pins v0.1.62 — local fast lane can't catch CI docs-lint failures |
| #4690 | `holomush-ksf12` | P2 | describe me / describe here broken: no host-side 'character' entity mutator |
| #4691 | `holomush-l6std` | P2 | plugin.proto: PluginHostService Log/KVGet/KVSet/KVDelete/AddSessionStream/RemoveSessionStream RPCs are declared but unserved |
| #4692 | `holomush-mcqs9` | P2 | Remove inner-error leaks from non-Internal gRPC status returns in core-scenes |
| #4693 | `holomush-mlko` | P2 | Review pgx pool and PostgreSQL connection settings for plugin architecture |
| #4694 | `holomush-nmvf` | P2 | KV hostfunc: log decision reason on denial (currently silently dropped) |
| #4695 | `holomush-ogmy0` | P2 | crypto reminder path-fallback misses manifest-only crypto.emits edits |
| #4696 | `holomush-okkj6` | P2 | Consistency: scene subcommand handlers should tokenize args like the gate (Fields[0]+arity) |
| #4697 | `holomush-p7lc` | P2 | Build ABAC performance benchmark suite |
| #4698 | `holomush-pqzv` | P2 | TestMigrator_ConcurrentUp flaky on docker port-map timeout |
| #4699 | `holomush-psr9` | P2 | Gateway-side request validation for ConnectRPC endpoints |
| #4700 | `holomush-q825` | P2 | Wire KEK provider into production core.go (mirror admin CLI's file-source pattern) |
| #4701 | `holomush-qg1v5` | P2 | Integration test: real core-communication page/whisper/pemit emit Sensitive=true & encrypt with crypto enabled |
| #4702 | `holomush-qik8d` | P2 | Migrate test/integration/crypto/readback_test.go to Ginkgo/Gomega |
| #4703 | `holomush-qyrbn` | P2 | ADR beads diverge from render-adr output — /adr update/supersede/render would corrupt legacy & retroactively-captured ADRs |
| #4704 | `holomush-rooy` | P2 | Plugin-installed policies bypass seed-coverage validator (holomush-xxel scope gap) |
| #4705 | `holomush-s7zi` | P2 | Add integration tests for transactional cascade deletion |
| #4706 | `holomush-sh6x` | P2 | Remove unused Functions.RegisterWithEmitCapture API + tests (post jg9b.3 capture-pass narrowing) |
| #4707 | `holomush-tmfi` | P2 | Plugin developer docs for commands[].aliases field |
| #4708 | `holomush-tn3cv` | P2 | core-channels: banned/kicked member keeps receiving live channel events until reconnect (no stream eviction on moderation) |
| #4709 | `holomush-tu3s` | P2 | Audit logger: differentiate ModeMinimal from ModeDenialsOnly (identical implementations) |
| #4710 | `holomush-vfje` | P2 | PluginProvider.SetRegistry race — use atomic.Pointer for plugin registry |
| #4711 | `holomush-vr6yo` | P2 | proto comment: MUST NOT declare AttributeResolverService in provides — contradicted by test-abac-widget/plugin.yaml |
| #4712 | `holomush-w6bue` | P2 | E2E: scene extend command-path via integrationtest (Ginkgo/Gomega) |
| #4713 | `holomush-w7u5` | P2 | Unify plugin alias pgxpool with shared database pool |
| #4714 | `holomush-wfjxh` | P2 | Add real-store test for workspaceStore.isFocusReady flip-after-await ordering (T7 race fix) |
| #4715 | `holomush-wfum` | P2 | E5.7.2 Silent hash upgrade failure — Argon2idHasher.NeedsUpgrade() never called in auth flow |
| #4716 | `holomush-wrwqu` | P2 | warm-light theme: accent/accent-foreground pairing fails WCAG AA (2.70:1) app-wide |
| #4717 | `holomush-y3zs3` | P2 | Register scene actor-binding cross-check as a named invariant + bind across all 9 SceneService handlers |
| #4718 | `holomush-zmh3` | P2 | chore(lint): investigate gocritic+ruleguard loading failure on plugins/ packages |
| #4719 | `holomush-009tw` | P3 | Consolidate seed-disable-migration test Describes + harness CreateScene visibility option |
| #4720 | `holomush-047sd` | P3 | Integration harness: refresh command AliasCache from alias_seeder |
| #4721 | `holomush-0sc.14` | P3 | core-channels: InviteToChannel records invitee (not inviter) as ops-journal actor (WR-02) |
| #4722 | `holomush-0sc.16` | P3 | core-channels: minor polish (prune graceful shutdown, rate-limiter GC, js_seq, subject facet strictness) |
| #4723 | `holomush-2bxn` | P3 | DSL ContainsCondition allows zero path segments before contains() method |
| #4724 | `holomush-2py0c` | P3 | Bind INV-PLUGIN-43 cycle sub-clause with a boot-level cycle test |
| #4725 | `holomush-431pp` | P3 | CI: cache build/plugins artifacts across jobs |
| #4726 | `holomush-44gqn` | P3 | INV-TS lint guards fail-open when rg is missing/errors |
| #4727 | `holomush-4g0gt` | P3 | Web unary RPC handlers log benign client cancellation (context.Canceled) at ERROR; streaming path already downgrades |
| #4728 | `holomush-5rh.28` | P3 | Scenes web theme: light mode lacks the pop/brightness of dark mode |
| #4729 | `holomush-71xyn` | P3 | Web-BFF/typed-RPC channel create: trusted owner-player-id for per-player rate limiting |
| #4730 | `holomush-72ti5` | P3 | Meta-test: every in-tree sensitivity:always Lua plugin must claim sensitive=true at emit sites |
| #4731 | `holomush-79f4g` | P3 | Polish: web communication seam non-blocking review nits (PR #4524) |
| #4732 | `holomush-7fskx` | P3 | Scene emit/no_space live-render convergence (web + telnet) |
| #4733 | `holomush-ay0vp` | P3 | E9.5 landing review follow-ups: stale client.ts comment + is_guest seed note |
| #4734 | `holomush-b8r7x` | P3 | Add a regression guard that the 'releases' sidebar topic stays registered in astro.config.mjs |
| #4735 | `holomush-ci23e` | P3 | Add wholesystem census negative arm: under-declaring fixture binary plugin must fail load (INV-PLUGIN-54) |
| #4736 | `holomush-djhor` | P3 | Stale Status headers on implemented specs mislead readers/analyzers — refresh with completion pointers |
| #4737 | `holomush-dokzt` | P3 | policy CreateBatch emits no oops code on constraint failure |
| #4738 | `holomush-dt60a` | P3 | Plugin dependency model polish: typed UnsatisfiedDep.Reason, RequiresDisplay zero-Kind format, VERSION optional consistency, fail-fast CHANGELOG/upgrade-guide |
| #4739 | `holomush-e0lbe` | P3 | commit-lint PR-title check doesn't re-run on title edit (missing 'edited' trigger) |
| #4740 | `holomush-e77ti.2` | P3 | host_capability_servers_test.go: broker registration test checks 9 services but omits WorldService, PropertyService, SessionService which are in proto but not registered |
| #4741 | `holomush-ec22.28` | P3 | CI: nightly soak has no failure notification — failures invisible |
| #4742 | `holomush-fb5i0` | P3 | Harden luabridge.pushBridgeError against fmt-wrapped status errors (latent opacity leak) |
| #4743 | `holomush-fdu2b` | P3 | Register terminal scene-command guest-deny as a named invariant |
| #4744 | `holomush-flgpj` | P3 | Delete orphaned capability *Fn helpers + cap modules from hostfunc after atomic cutover (holomush-eykuh.4.9) |
| #4745 | `holomush-fycpp` | P3 | Binary forged-dispatch-rejection: whole-system e2e once a scope-eligible binary capability exists |
| #4746 | `holomush-hhysa` | P3 | Reject cross-gameID qualified stream subjects at read/subscribe entry (defense-in-depth) |
| #4747 | `holomush-hqzc3` | P3 | jj amend force-push to a PR branch does not re-trigger path-filtered ci.yaml (close+reopen to re-fire) |
| #4748 | `holomush-jq34t` | P3 | page/whisper/pemit: sender echo is direct command output, recipient is an event — unify so both flow through the event stream |
| #4749 | `holomush-jtl99` | P3 | Integration-tier PG fault-injection coverage for dek Store/CheckpointRepo + core-scenes DB-error branches |
| #4750 | `holomush-k5gk0` | P3 | Validate FocusRedirect.FocusKind against known kinds in CollectFocusRedirects (defense-in-depth) |
| #4751 | `holomush-kcy5c` | P3 | Bump @bufbuild/protoc-gen-es + bufbuild/es codegen plugin to 2.12.1, regenerate web stubs |
| #4752 | `holomush-lfy04` | P3 | Route stub service-method @param/@return refs through the collision-aware classNamer |
| #4753 | `holomush-logk1` | P3 | Flaky: session_store_integration_test.go context-deadline (10s ctx too tight for testcontainer under -race/parallel CI load) |
| #4754 | `holomush-lvirs` | P3 | Scenes web: unify SceneBoardRow status dot onto sceneStateDotClass helper |
| #4755 | `holomush-mnm2a` | P3 | render-adr YAML frontmatter output breaks task lint (license-eye header lands above frontmatter; repo ADRs use H1 style) |
| #4756 | `holomush-obo44` | P3 | Forcible session disconnect: gateway-observed eviction backing for SessionAdmin.DisconnectSession |
| #4757 | `holomush-oik33` | P3 | Scene mute/notify defense-in-depth: tighten MuteScene + ListCharacterScenes to require vouched metadata |
| #4758 | `holomush-p0wjp` | P3 | Guard fetchCommandList against stale-session clobber in CommandInput |
| #4759 | `holomush-p29j` | P3 | session_connections.client_type is unsanitized user input stored in DB |
| #4760 | `holomush-qd80a` | P3 | web: move restoreSession from +layout.svelte onMount to root +layout.ts load() |
| #4761 | `holomush-rhnd` | P3 | Graceful degradation can silently mask ABAC policy failures |
| #4762 | `holomush-seuz` | P3 | RenderingPublisher.Publish rewraps inner error, masking EVENTBUS_PUBLISH_EXPIRED |
| #4763 | `holomush-tkctt` | P3 | integrationtest.Session.SendCommand omits connection_id — focus-dependent specs must use SendCommandOnConnection |
| #4764 | `holomush-tlilq` | P3 | Flaky E2E: session-security.spec.ts '11th login evicts the oldest session' (timing) |
| #4765 | `holomush-uvkks` | P3 | world/grpc_server.go: 4 InvalidArgument returns %v a ULID parse error |
| #4766 | `holomush-v6sz8` | P3 | Focus-redirect polish: dedup storeFocusReader, typed FocusRedirectTable key, blank-verb test + error-code assertions |
| #4767 | `holomush-w7t5` | P3 | Server-side player preferences |
| #4768 | `holomush-xtkj2` | P3 | Add unit test: multi-*Aware provider with partial capability declaration (INV-PLUGIN-54) |
| #4769 | `holomush-y684z` | P3 | Extract shared actorBinding helper for the ~10 duplicated SceneService guard blocks |
| #4770 | `holomush-ymkf` | P3 | UpdateGridPresent SQL parameter order mismatch — maintenance hazard |
| #4771 | `holomush-zb2zv` | P3 | Document the rumdl tooling-dir exclusions (.planning/.claude/.beads/.serena) in docs/CLAUDE.md |
| #4772 | `holomush-zpjx7` | P3 | Brokered focus proto uses bytes ULID fields — awkward for Lua callers (no luabridge ULID-bytes helper) |
| #4773 | `holomush-zrt97` | P3 | Register INV-ACCESS for omit-optional-attrs and bind scene resolver location tests |
| #4774 | `holomush-0sc.17` | P4 | core-channels: admin create rate-limit bypass keys on exact matched-policy id (availability-only) |

## Consolidated into the GSD backlog (`ROADMAP.md` §Backlog)

### Admin Web UI & Config

- `holomush-g4pb` *(epic)* P3 — Admin Commands via Web UI & Config
- `holomush-7nub` P1 — Admin panel — /admin route with operator tools
- `holomush-g4pb.1` P3 — Design: admin web UI & config system

### Architecture decomposition program

- `holomush-1bft` *(epic)* P1 — Repo audit follow-up: layering & boundaries (2026-05-13)
- `holomush-dj95` *(epic)* P1 — Repo audit follow-up: architecture (2026-05-13)
- `holomush-wm0fi` *(epic)* P1 — [epic] Focus-redirect hot-path cache
- `holomush-yvdm` *(epic)* P1 — Repo audit follow-up: design alignment (2026-05-13)
- `holomush-dj95.5` P1 — Decompose CoreServer (24 fields) along auth / subscribe-history / command-pipeline wings
- `holomush-dj95.6` P1 — Split plugin/manager.go into Loader / Registry / Lifecycle; dedupe trustAllowlist
- `holomush-dj95.7` P1 — Migrate bootstrap orchestration from runCoreWithDeps to lifecycle.Orchestrator
- `holomush-ec22.2` P1 — Architecture: collapse parallel core.Event / eventbus.Event model post-JetStream cutover
- `holomush-ec22.5` P1 — Architecture: gateway invariant violated — web/telnet/gateway import internal/core

### Channels — remaining scope

- `holomush-0sc` *(epic)* P2 — Epic 10: Channels
- `holomush-0sc.2` P2 — [E10] Implementation Plan: Channels
- `holomush-0sc.3` P2 — [E10.1] Channel Schema - Name, Type, Membership
- `holomush-0sc.4` P2 — [E10.2] Channel Commands - join/leave/list/say
- `holomush-0sc.5` P2 — [E10.3] Channel Types - Public, Private, Admin
- `holomush-0sc.6` P2 — [E10.4] Moderation - Mute, Ban, Ops
- `holomush-0sc.7` P2 — [E10.5] Channel History - Logging, Replay on Join
- `holomush-0sc.8` P3 — Channel message search (full-text)

### Character Rostering & Transfer

- `holomush-gloh` *(epic)* P2 — Epic: Character Rostering & Transfer

### Code health & test-quality program

- `holomush-89o9` *(epic)* P2 — Repo audit follow-up: codebase humanization / de-slop (2026-05-13)
- `holomush-ec22` *(epic)* P1 — Codebase review findings (2026-04-25)
- `holomush-0yo6` P1 — Increase test coverage on Phase 1.5 infrastructure packages
- `holomush-ec22.15` P3 — Tests: ~15 ACE-framework naming violations in older test files
- `holomush-ec22.16` P3 — Tests: weak/skeleton tests + zero-assertion compile-canaries + skip-with-unreachable-setup
- `holomush-ec22.22` P3 — Docs: archive ~30 stale plans + consolidate phase-7.x ABAC plan files
- `holomush-ec22.9` P3 — Security polish: cookie/TLS coupling, dummy hash entropy, write timeout, addlicense pin
- `holomush-izk0` P1 — Comprehensive session-lifecycle Ginkgo test matrix

### Discord Integration

- `holomush-aqq` *(epic)* P3 — Epic 12: Discord Integration
- `holomush-aqq.1` P3 — [E12] Design: Discord Integration Architecture
- `holomush-aqq.2` P3 — [E12] Implementation Plan: Discord Integration
- `holomush-aqq.3` P1 — [E12.1] Discord Bot - Go Plugin Implementation
- `holomush-aqq.4` P1 — [E12.2] Channel Bridge - Discord ↔ Game Channel Sync
- `holomush-aqq.5` P1 — [E12.3] OAuth Linking - Discord Account to Player Account
- `holomush-aqq.6` P2 — [E12.4] Notifications - Mentions and DMs Forwarded
- `holomush-aqq.7` P2 — [E12.5] Status Sync - Online/Offline Presence

### Documentation program

- `holomush-k7qy` *(epic)* P1 — Comprehensive system design documentation: consolidate + complete
- `holomush-rm9g` *(epic)* P1 — Comprehensive features/usage/admin/operator/player documentation under site/docs/
- `holomush-3lm4` P2 — Unified help system: in-game help command + website command reference
- `holomush-bgxg` P1 — Session lifecycle diagram + site reference doc

### Existing Phase 3 (Platform Hardening)

- `holomush-s5ts` *(epic)* P3 — External NATS deploy: design + render deny rules + lifecycle pivot off embedded-only

### Feature wishlist

- `holomush-0hiu` P2 — Content storage — interface-backed blob storage for uploads
- `holomush-8bv4` P2 — Claude Code skill for authoring HoloMUSH plugins
- `holomush-nmr8` P2 — Custom theme support — operator-defined color schemes
- `holomush-vrzu` P2 — Rich text support for game messages (markdown + emoji)

### Forums

- `holomush-djj` *(epic)* P3 — Epic 11: Forums
- `holomush-djj.1` P3 — [E11] Design: Forums Architecture
- `holomush-djj.2` P3 — [E11] Implementation Plan: Forums
- `holomush-djj.3` P1 — [E11.1] Forum Schema - Boards, threads, posts
- `holomush-djj.4` P1 — [E11.2] Web UI - Browsing, posting
- `holomush-djj.5` P1 — [E11.3] Moderation Tools - Edit, delete, lock, move
- `holomush-djj.6` P2 — [E11.4] Notifications - New replies
- `holomush-djj.7` P2 — [E11.5] In-game Integration - forum recent command
- `holomush-twoqp` P2 — [E9.6] Forum Integration - Scene Requests, Scheduling

### Invariant registry backfill program

- `holomush-hz0v4` *(epic)* P2 — Central invariant doc registry: catalog every INV-* invariant in one place
- `holomush-s6wp` *(epic)* P3 — Centralize invariant capture, storage, and cross-reference across specs
- `holomush-huuvr` P3 — Migrate INV-DOCS scope (docs-IA / docs-quality invariants)
- `holomush-hz0v4.11` P3 — Backfill // Verifies: INV-CRYPTO-N bindings for pending crypto invariants
- `holomush-hz0v4.16` P3 — Backfill INV-SCENE verification bindings (60 pending)
- `holomush-hz0v4.17` P3 — Backfill INV-PLUGIN verification bindings (39 pending)
- `holomush-hz0v4.18` P3 — Backfill INV-EVENTBUS verification bindings (28 pending)
- `holomush-hz0v4.19` P3 — Backfill non-crypto long-tail verification bindings (STORE/TELEMETRY/ACCESS/SESSION/COMMAND/CLUSTER/PRESENCE, 34 pending)
- `holomush-hz0v4.20` P3 — Re-bind INV-PRIVACY-6 (floor-preservation arm) + INV-PRIVACY-7 (custom history_scope) when test gaps close
- `holomush-hz0v4.21` P3 — Audit cleanup: reclassify/remove registry entries that fail the invariant bar
- `holomush-yyn59` P3 — Migrate INV-BRANDING scope (site brand-token invariants)

### Inventory & Object Manipulation

- `holomush-ni99` *(epic)* P3 — Inventory & Object Manipulation
- `holomush-ni99.1` P3 — Design: inventory & object interaction model

### Observability & vendor-neutral telemetry

- `holomush-ionvr` *(epic)* P2 — obs: vendor-neutral error/telemetry/metrics abstraction at every seam
- `holomush-yxfbi` *(epic)* P3 — Signal-hygiene: expected/benign conditions should not masquerade as ERROR/WARN-with-stacktrace (design seed)
- `holomush-lhx5w` P2 — Design: error-event seam — slog.Any+LogValuer idiom + handler-level CaptureException (evolves holomush-1wbzn)

### Ops & deployment resilience

- `holomush-aub5` *(epic)* P4 — Remote KMS substrate: VaultTransitProvider + provider-migrate CLI + KEK rotation CLI
- `holomush-2iyrf` P2 — Design: gateway-survival deploy strategy (don't restart gateway unless it actually changed)
- `holomush-3pvs` P2 — Add Tailscale for remote admin access (optional SSH replacement)
- `holomush-6doi` P2 — Disaster recovery: recreate droplet from backup
- `holomush-n6z3` P2 — Background database sync to S3/GCS/Azure Blob Storage
- `holomush-q426` P2 — Backup and restore guide for production deployments

### Platform & security design seeds

- `holomush-1334` P1 — ABAC Phase 7.7 missing features (fair-share timeout, debug endpoint)
- `holomush-207m` P1 — Plugin host functions lack authorization (streams, focus, session)
- `holomush-8l7d` P2 — Communication event type extensibility for channels, forums, and plugin-defined verbs
- `holomush-9cn8r` P3 — Decide whether plugins with stream.history may read private scene .ic stream metadata
- `holomush-ddwz` P2 — Formalize feature-flag/gate system across the host
- `holomush-ecbg` P2 — eventbus audit drift detector: codec / row tamper checker
- `holomush-l4kx` P2 — holomush audit-backfill CLI subcommand
- `holomush-ql7ef` P2 — Design: full-system load/perf testing harness + SLOs
- `holomush-qqigi` P2 — Decide whether production should fail-closed when KEK provider is unavailable (currently silent fail-open)

### Scenes & RP — remaining scope

- `holomush-5rh` *(epic)* P2 — Epic 9: Scenes & RP

### Web Client Portal completion

- `holomush-qve` *(epic)* P2 — Epic 8: Web Client
- `holomush-qve.10` P2 — [E8.8] Portal: Admin - Server stats, player management
- `holomush-qve.15` P2 — [E8] Character creation + management UI
- `holomush-qve.17` P2 — Web surface for 1:1 direct messages (page/whisper)
- `holomush-qve.7` P2 — [E8.5] Offline support - Command queue, event cache, reconnect sync
- `holomush-qve.8` P2 — [E8.6] Portal: Wiki - Help pages, lore, documentation
- `holomush-qve.9` P2 — [E8.7] Portal: Characters - Public profiles, character sheets

### iOS Client (stretch)

- `holomush-5g6` *(epic)* P4 — Epic 13: iOS Client (Stretch)

## Verified already done

Code-grounded evidence cited per bead.

| Bead | Title | Evidence |
| --- | --- | --- |
| `holomush-0n2l` | Make migrations idempotent for concurrent execution | internal/store/migrate_integration_test.go:233-280 TestMigrator_ConcurrentUp asserts concurrent Up() succeeds via golang-migrate's advisory lock; test exists an |
| `holomush-0sc.10` | Guest auto-join to seeded channels on session creation | plugins/core-channels/main.go:96-143 QuerySessionStreams unions ListDefaultChannels for every session at establishment; guest auto-join implemented via default- |
| `holomush-0sc.12` | Channel plugin rework on new plugin ABAC architecture | plugins/core-channels/ fully shipped (service.go, store.go, main.go, commands.go, audit.go); blocking deps 275o/6l7l/jg9b.3 all CLOSED. |
| `holomush-0sc.9` | Channel session subscription wiring | main.go QuerySessionStreams (establish) + service.go:462 unsubscribeLive (leave) implement subscribe/unsubscribe; gag/ungag superseded by mute design (member st |
| `holomush-2uogn` | Gateway OTLP relay endpoint for browser OpenTelemetry (same-origin, mirrors /api/sentry-relay) | internal/web/otlp_relay.go (148 lines, NewOTLPRelayHandler) implemented and wired at server.go:106-112 on /api/otlp/v1/traces; otlp_relay_test.go has 9 Test fun |
| `holomush-5rh.19` | Scenes Phase 10: Notifications + telnet edge cases + polish | shipped as GSD Phase 2 Scenes Lineage Completion (ROADMAP.md:53, completed 2026-07-09) |
| `holomush-685f` | Fix resolver/provider entity ID format mismatch (blocks ABAC integration tests) | internal/access/policy/attribute/resolver.go:365-375 passes full unstripped entityRef to providers; helpers.go:50-61 parseEntityResource expects 'type:id'. No p |
| `holomush-7uyus` | cog-release.bats setup() depends on developer git signing config — fails non-interactive pr-prep | scripts/tests/cog-release.bats:20-21 sets `git config commit.gpgsign false`/`tag.gpgsign false` with hermetic comment; fixed in commit f0812b4e0 (#4542, 2026-06 |
| `holomush-8daku` | cog-release.bats setup git commit fails under global commit.gpgsign=true (op-ssh-sign/1Password unreachable in non-interactive runs) | Same underlying bug as holomush-7uyus; fixed by same commit f0812b4e0 (2026-06-29) adding commit.gpgsign false to cog-release.bats setup(), postdates this bead' |
| `holomush-9s4wv` | bootstrap_seed_secrets.py: multi-line paste corrupts SSH key via terminal bracketed-paste escapes | Commit 84a595bce (PR #4211) merged; scripts/bootstrap_seed_secrets.py:150-180 has _ANSI_CSI regex stripping bracketed-paste escapes exactly as the fix plan desc |
| `holomush-bf5fu` | pnpm check: input-group-addon.svelte type error (Property 'focus' does not exist on type 'Element') | web/.../input-group-addon.svelte:47 fixed in commit 8bbdffb8e (2026-07-01): added querySelector<HTMLElement> generic; postdates bead's 2026-06-28 filing. |
| `holomush-bpy6p` | fix(compose): dev otel-collector unreachable from gateway under dev:obs — add collector to frontend network | compose.yaml:91-101 otel-collector networks now list both `backend` and `frontend`, comment verbatim matches bead's fix ('Mirrors compose.prod.yaml'). |
| `holomush-buph` | Plugin architecture documentation and test coverage audit | All cited godoc gaps filled (registry.go:12, registered_service.go:17, grpc_proxy.go:18, schema_provisioner.go:25, pkg/plugin/service.go:39, inprocess_conn.go:1 |
| `holomush-cicf0` | docs-only PRs skip adr-doctor (lint:adr) — ADR-template violations reach main unguarded | Taskfile.yaml pr-prep:docs now runs `task: lint:adr`; ci-docs-skip.yaml comment confirms real lint lane. Fixed by commit 5aaeadcb8 (PR #4283, holomush-3zrvh, 20 |
| `holomush-ec22.4` | Architecture: domain core imports transport types (pkg/proto/holomush/web/v1) | internal/core/registry.go:9,20 now imports corev1 (pkg/proto/holomush/core/v1), not web/v1; commit 'feat(proto): add corev1.RenderingMetadata and EventChannel'  |
| `holomush-f7kc` | [E5.8.1] Align auth spec with implementation — schema/expiry/migration-number drift | docs/specs/2026-01-25-auth-identity-design.md now shows character_id nullable, token_hash (not token_signature), last_seen_at, 24hr expiry, and migration 000009 |
| `holomush-kgxh` | NewPlayerToken returns error that can never fail — misleading API (verify if signature still exists) | internal/auth/player_token.go does not exist; no NewPlayerToken function anywhere in internal/auth/*.go (only player_session.go). Bead's own acceptance criteria |
| `holomush-l2l1` | New arrivals see entire prior history of public locations they join | internal/grpc/scope_floor.go:34-38 streamScopeFloor enforces server-side floor at info.LocationArrivedAt for location streams (holomush-iwzt privacy epic), inde |
| `holomush-ndbz0` | GetScene/EndScene leak inner error into gRPC status (grpc-errors.md) | plugins/core-scenes/service.go:470 (GetScene) and :778 (EndScene) already use opaque status.Error(codes.Internal,"internal error"), no %v leak |
| `holomush-nv83` | ABAC resolver: principal.id and resource.id injection (discovered during scenes Phase 1) | resolver.go:197-215 id-injection fix in place; resolver_test.go has 20+ tests covering it; stable in prod 3 months, no regression reported |
| `holomush-p305` | ABAC engine: process-level degradedCount couples independent Engine instances | engine.go:29,66-68,128,414: degraded is per-instance atomic.Bool; IsDegraded/Evaluate unaffected by other engines. Only the Prometheus gauge is process-wide, by |
| `holomush-qve.5.8` | [E8.3] Terminal UI: render command responses in scrollback | command_response is now a first-class stream event (core/event.go:52, builtins.go:67 DisplayTarget=TERMINAL); terminal +page.svelte routeEvent->appendLine rende |
| `holomush-tt77` | mergeCallerHeaders reserved-key check is case-sensitive; nats-msg-id bypasses guard | internal/eventbus/publisher.go:365-366 mergeCallerHeaders already canonicalizes via textproto.CanonicalMIMEHeaderKey before reserved-key check; comment :354-359 |
| `holomush-up7h` | [E5.8.2] Document HMAC token design decision (implementation diverged from spec) | docs/adr/holomush-ydti-use-opaque-session-tokens-instead-signed-jwts.md fully documents HMAC-vs-opaque decision; committed 2026-02-02, predates bead (filed 2026 |
| `holomush-xrdjw` | Enable sloglint (context: scope) + migrate bare slog/logger calls to *Context variants | .golangci.yaml:39,160-171 sloglint enabled w/ context:scope. Remaining ~113 bare slog.X( calls are confined to cmd/holomush main/init/bootstrap (exempt carve-ou |
| `holomush-yjed` | Flaky test: TestCoordinatorRequestInvalidationReturnsRateLimitedWhenProbeAndPillRefuses | Commits 899fc44ff (holomush-kz7tb, 2026-06-19) + 7baa11577 (holomush-o7k0p, 2026-06-21) de-flake this exact test; extensive fix comments now in coordinator_send |

## Stale (premise no longer true)

| Bead | Title | Evidence |
| --- | --- | --- |
| `holomush-0h8m` | Fix FindByCharacterName not-found semantics: MemStore returns SESSION_NOT_FOUND error, Postgres returns nil,nil — align to nil,nil and add interface doc | MemStore type no longer exists (rg 'MemStore' --type go -> 0 hits); only PostgresSessionStore remains in internal/store/session_store.go. Premise gone. |
| `holomush-eawl` | QueryStreamHistory has same off-by-precision bug as Subscribe (iwzt.15); fixing it surfaces +page.svelte snapshot-apply-after-backfill UI race | Nanosecond-timestamps epic (gfo6) removed ALL Truncate(time.Microsecond) truncation end-to-end; query_stream_history.go:273 and server.go:1306 have no truncate  |
| `holomush-ec22.23` | Build/CI: silently broken gates — ebnf-sync hook path + missing EventIDMustBeMonotonic ruleguard rule | lefthook fully retired (git-tracked lefthook.yaml gone); EBNF check now task generate:ebnf:check using correct site/public/reference paths. EventIDMustBeMonoton |
| `holomush-h8xj` | task gowork produces go.work Go rejects as 'module appears multiple times' | jj retired for native git worktrees (commit c80eaf742); no go.work file exists, no 'gowork' task in Taskfile.yaml. GOWORK=off is now permanent for an unrelated  |
| `holomush-iw804` | Missing KEK env vars logs full oops stacktrace at WARN every dev boot; message lacks setup-doc reference | cmd/holomush/core.go:744-754: KEK is now mandatory at boot (fail-closed BOOT_KEK_REQUIRED, 'no degraded KEK-less mode') per PR #4411 (KEK-mandatory boot). The f |
| `holomush-jh4l` | Fix admin capability entity ref format in plugin YAMLs | No admin.boot/admin.wall/admin.alias dotted capability refs exist anywhere; plugin.yaml schema uniformly uses {action,resource,scope} triples (plugins/core-comm |
| `holomush-jo0a` | Flaky test: auth deletes expired sessions in bulk (clock-skew race on 1ms TTL) | test/integration/auth/player_session_test.go no longer exists (dir has auth_suite_test.go, core_client_shim_test.go, multi_tab_test.go). DeleteExpired now teste |
| `holomush-tvvp` | gRPC control server: Stop() doesn't close listener — leak on early-stop race | grpc-go v1.82.0 server.go:888-896: Serve() self-closes lis whether called before or after Stop/GracefulStop. No actual leak; bead premise is factually wrong. |
| `holomush-uvu9` | MockSessionAccess.Sessions public field — verify and fix potential data race in parallel tests | internal/command/handlers/testutil/ dir does not exist (handlers/ has no testutil subdir). Only mockSessionAccess in stdlib_session_test.go, a different unexpor |
| `holomush-uyrd` | Sidebar collapsed icons: add tooltips and hover highlight | web/src/lib/components/sidebar/Sidebar.svelte:49-52 — sidebar redesigned to binary hide/show (width:0), no collapsed icon-strip w/ location-pin/door/people icon |
| `holomush-x1tnm` | [BACKLOG] Command events without character_id should error | plugins/core-communication/main.lua: 'character_name = character_name or "Someone"' pattern is gone; current code uses ctx.character_id or "" (different, smalle |

## Duplicates

| Bead | Title | Evidence |
| --- | --- | --- |
| `holomush-0sc.15` | Lua add_session_stream hostfunc bypasses AuthorizePluginStreamContribution fence | Identical finding to holomush-16rt5: internal/plugin/hostfunc/stdlib_streams.go AddStream bypasses AuthorizePluginStreamContribution; both filed 2026-07-09 from |
| `holomush-gdr6w` | test/meta INV-SCENE-41 (TestFocusAdapterPairAssembledOnlyInHelper) scans sibling .claude/worktrees checkouts | Same root cause as jb1ec: test/meta/focus_delta_gate_test.go:15,18 nonTestGoFilesContaining does bare filepath.WalkDir with no .claude/worktrees exclusion. jb1e |

## Archive only

Not migrated: explicitly deferred, P4 backlog-tier, children of closed epics,
or stale audit/review findings whose code moved on. Every record remains
recoverable from `2026-07-09-beads-export-full.jsonl.gz`.

| Bead | P | Title | Reason |
| --- | --- | --- | --- |
| `holomush-wagqb` | P1 | Token/cost/latency optimization — Claude Code dev workflow (Phase 1+2) | Bead's own note: PR #4569 merged 2026-07-03, epic 7/8 (87%) done; only .5 (operator-side ~/.claude d |
| `holomush-095g.7` | P2 | Plugin-as-caller identity for PluginHostService.QueryStreamHistory against plugin-owned subjects | child of closed epic holomush-095g, untouched since 2026-05-16 |
| `holomush-1qp5` | P2 | Implement look command (core navigation) | stale audit/review finding (last touch 2026-05-16) |
| `holomush-32v8` | P2 | Missing TestGuestAuthenticator_ReturnsIsGuest unit test specified in plan | stale audit/review finding (last touch 2026-05-16) |
| `holomush-6pmp` | P2 | Implement move command (core navigation — go through exits) | stale audit/review finding (last touch 2026-05-16) |
| `holomush-72sj` | P2 | Create core-channels plugin — use AddSessionStream(LIVE_ONLY) + QueryStreamHistory | stale audit/review finding (last touch 2026-05-16) |
| `holomush-7gdh` | P2 | Phase 2 follow-up: keyring-backed KEKSource (go-keyring) | stale audit/review finding (last touch 2026-05-16) |
| `holomush-7zot` | P2 | CI static-analysis guard to prevent AccessControl.Check reintroduction | stale audit/review finding (last touch 2026-05-15) |
| `holomush-8hff` | P2 | Strategic theme: social-spaces — substrate-and-uses cluster (scenes → channels → forums → discord) | decision record (ADR-captured) |
| `holomush-8j8q` | P2 | Command State Management for Multi-Turn Interactions | decision record (ADR-captured) |
| `holomush-9mxr.21.3` | P2 | INV-P5-7 pin is a happy-path test; atomicity observer-isolation is not verified | child of closed epic holomush-9mxr.21, untouched since 2026-05-24 |
| `holomush-afs9` | P2 | arrive event log lacks session_id correlation field + uses raw slog instead of errutil.LogError | stale audit/review finding (last touch 2026-05-16) |
| `holomush-b5cn` | P2 | Implement boot command (admin: boot player/guest with reason) | stale audit/review finding (last touch 2026-05-16) |
| `holomush-b8n5` | P2 | ABAC Phase 7.7 cleanup + design followups | stale audit/review finding (last touch 2026-05-15) |
| `holomush-cb4x` | P2 | Scenes: scene log + scene export commands + export renderers | stale audit/review finding (last touch 2026-05-16) |
| `holomush-cbkx` | P2 | Merge NewX + NewXWithLogger constructor pairs via functional options | stale audit/review finding (last touch 2026-05-16) |
| `holomush-dj95.11` | P2 | Add AuthGuard audit metrics counters (drain_failed, marshal_failed) | stale audit/review finding (last touch 2026-05-15) |
| `holomush-dj95.12` | P2 | Lift focus.Coordinator from internal/grpc/focus/ to internal/focus/ | stale audit/review finding (last touch 2026-05-30) |
| `holomush-dj95.2` | P2 | Delete decodeAuthorizeAndDispatch + its callsites (depends on dj95.1) | stale audit/review finding (last touch 2026-05-30) |
| `holomush-dj95.8` | P2 | Rename PostgresEventStore → SystemInfoStore + switch InitGameID to idgen.New() | stale audit/review finding (last touch 2026-05-15) |
| `holomush-dj95.9` | P2 | Reclassify internal/admin/policy/ under auditchain or eventbus/crypto/policy | stale audit/review finding (last touch 2026-05-15) |
| `holomush-eo5z0` | P2 | Add unit coverage for grpc Client.UpdateScene wrapper | explicitly deferred |
| `holomush-h9jr` | P2 | Create test/integration/access/location_equivalence_test.go — Phase 7.7 migration validation | stale audit/review finding (last touch 2026-05-16) |
| `holomush-imiq` | P2 | Wire EVENTS_AUDIT_DLQ for audit projection MaxDeliver exhaustion | stale audit/review finding (last touch 2026-05-16) |
| `holomush-kday` | P2 | Focus substrate architecture documentation — concept doc + plugin API guides | stale audit/review finding (last touch 2026-05-30) |
| `holomush-ln1ab` | P2 | Polish: type-design + comment suggestions from PR #4266 review | explicitly deferred |
| `holomush-m8n6` | P2 | T28.5 migration-equivalence gate: file ADR waiver or write seed-policy integration tests | stale audit/review finding (last touch 2026-05-15) |
| `holomush-mjy3` | P2 | Reconsider object_examine sensitivity: 'never' will silently reject future sensitive emits (security implication) | stale audit/review finding (last touch 2026-05-16) |
| `holomush-p1tq2.12` | P2 | Input struct free-string fields allow subject/action/resource misassignment at construction | explicitly deferred |
| `holomush-p1tq2.13` | P2 | Decision struct permits allowed=true with denial-flavoured Reason (no coherence invariant) | explicitly deferred |
| `holomush-p1tq2.15` | P2 | EngineProvider.AttributeResolver() leaks concrete *attribute.Resolver through a public interface | explicitly deferred |
| `holomush-p1tq2.17` | P2 | EvaluateDecision and pluginauthz.Decision have no way to distinguish 'denied by engine' from 'no engine consulted' | explicitly deferred |
| `holomush-p1tq2.20` | P2 | handleResume doc-comment says 'Owner-only in Phase 2' but resume policy already admits any participant | explicitly deferred |
| `holomush-p1tq2.22` | P2 | splitResourceRef doc-comment says 'single colon' but rejects multi-colon IDs silently | explicitly deferred |
| `holomush-p1tq2.23` | P2 | host_service.go Evaluate: engine-nil and host-nil paths return error without log | explicitly deferred |
| `holomush-pojo` | P2 | Implement who command (list online characters) | stale audit/review finding (last touch 2026-05-16) |
| `holomush-q0n0` | P2 | Add Reason field to DisconnectRequest — leave events currently hardcoded to 'quit' | stale audit/review finding (last touch 2026-05-16) |
| `holomush-rs0j` | P2 | Phase 7.6 ABAC + KV polish (test gaps, dead code, design comments) | stale audit/review finding (last touch 2026-05-15) |
| `holomush-sdzd` | P2 | Auth: Add test for password reset token_hash collision (UNIQUE constraint violation) | stale audit/review finding (last touch 2026-05-16) |
| `holomush-setx` | P2 | HandleCommand returns generic 'session not found' for all store errors — masks DB failures | stale audit/review finding (last touch 2026-05-16) |
| `holomush-sz0h3` | P2 | Principle: the web is a superset of telnet — every gameplay subsystem has a web control expression | decision record (ADR-captured) |
| `holomush-t2udp` | P2 | Meta-test outOfScope check is a tautology — tests author-controlled list, not actual file content | stale audit/review finding (last touch 2026-05-25) |
| `holomush-vitqu` | P2 | TestReadbackWithoutReadbackFlagDenied inlines 100+ lines of setup duplicating buildReadbackEnv | stale audit/review finding (last touch 2026-05-25) |
| `holomush-wagqb.5` | P2 | Document bd prime operator-side de-dup (T1a, RD4) | explicitly deferred |
| `holomush-x5d7` | P2 | Test ErrSessionEnded teardown failure paths (HandleDisconnect/sessionStore.Delete errors) | stale audit/review finding (last touch 2026-05-16) |
| `holomush-xlty` | P2 | AccessPolicyEngine call-site observability gaps (metric labels + span attributes) | stale audit/review finding (last touch 2026-05-15) |
| `holomush-xpn2` | P2 | Phase 2 follow-up: systemd-credential KEKSource | stale audit/review finding (last touch 2026-05-16) |
| `holomush-xq7iq` | P2 | Add per-item max_len to tags repeated field in UpdateScene/WebUpdateScene protos | explicitly deferred |
| `holomush-ysuxw` | P2 | Publish-vote web: optional polish deferred from PR #4564 review | explicitly deferred |
| `holomush-zkmfc` | P2 | Scope and apply INV-P8-N rename + ship Phase 8 invariant meta-test | explicitly deferred |
| `holomush-zluv` | P2 | Add strict audit mode to EngineConfig for compliance deployments | explicitly deferred |
| `holomush-0s2h` | P3 | Centralize localStorage access via safeStorage helper | P3 orphan task, last touch 2026-04-26 |
| `holomush-11u0` | P3 | @cluster status and @evict-member admin commands | P3 orphan task, last touch 2026-05-15 |
| `holomush-12irb` | P3 | Add gitignore for web/.claude/agent-memory so reviewer reports don't get committed | P3 orphan task, last touch 2026-05-30 |
| `holomush-12j7` | P3 | Plumb user_agent and ip_address through auth RPCs to ListPlayerSessions | P3 orphan task, last touch 2026-04-26 |
| `holomush-17xh` | P3 | Integration test: alias commands through plugin dispatch | P3 orphan task, last touch 2026-05-01 |
| `holomush-186q` | P3 | CI hygiene: bead-existence smoke check for dek.Manager stubAllowSet | stale audit/review finding (last touch 2026-05-16) |
| `holomush-19uc` | P3 | Playwright E2E test: session TTL expiration behavior | stale audit/review finding (last touch 2026-05-16) |
| `holomush-1xhx` | P3 | echo-bot: design re-emit semantics for cross-plugin event types (crypto.emits interaction) | stale audit/review finding (last touch 2026-05-16) |
| `holomush-3skv5` | P3 | docs(guide): rewrite building.md as a goal->steps->verify how-to once in-game building commands ship | P3 orphan task, last touch 2026-05-29 |
| `holomush-3soe` | P3 | CoreClient vs GRPCClient dual-interface drift | P3 orphan task, last touch 2026-04-26 |
| `holomush-3ucc` | P3 | Host-side guard: enforce MetadataOnly+NoPlaintextReason coherence at EventFrame conversion | P3 orphan task, last touch 2026-05-13 |
| `holomush-4dm1` | P3 | localStorage scrollback persistence | P3 orphan task, last touch 2026-04-26 |
| `holomush-4ibz` | P3 | Add integration test for resolver fail-closed behavior end-to-end | explicitly deferred |
| `holomush-4icb` | P3 | Pill message signing (deferred from ojw1.3.18, Decision 7) | stale audit/review finding (last touch 2026-05-16) |
| `holomush-50hl` | P3 | Struct parameter for SetSystemAlias to prevent positional confusion | P3 orphan task, last touch 2026-04-26 |
| `holomush-5e0s` | P3 | Type-check entityPrefix in world.checkAccess — prevent silent unmapped-prefix bugs | stale audit/review finding (last touch 2026-05-16) |
| `holomush-5o1d` | P3 | Deferred review findings from PR #88 (ABAC Phase 7.6 migration) | explicitly deferred |
| `holomush-5yl5u` | P3 | Audience-based IA restructure of site contributing/ docs (maintainer vs contributor) | P3 orphan task, last touch 2026-05-29 |
| `holomush-61en` | P3 | Re-enable Benchmark Regression Check workflow with cost/scope fixes | P3 orphan task, last touch 2026-05-19 |
| `holomush-61sa` | P3 | Subscribe + replay subscribe tests use time.Sleep — flaky under slow CI | stale audit/review finding (last touch 2026-05-16) |
| `holomush-6n1j3` | P3 | Pin protoc-gen-doc version in Taskfile tools install (currently @latest) | P3 orphan task, last touch 2026-05-29 |
| `holomush-6nds` | P3 | JS storage rebuild from PG audit: restore tool | P3 orphan task, last touch 2026-04-26 |
| `holomush-6ra22` | P3 | Surface build version in UI + command palette + in-game command | P3 orphan task, last touch 2026-05-29 |
| `holomush-71zq.9` | P3 | Optimize bats collision-test wall time (kill propagation through holder process tree) | child of closed epic holomush-71zq, untouched since 2026-05-01 |
| `holomush-742pn` | P3 | Bring coreEventToProto under a reflection parity guard (5th host→plugin event marshal site) | P3 orphan task, last touch 2026-05-30 |
| `holomush-7kyy` | P3 | Add errutil.AssertErrorCode tests for config error code propagation | stale audit/review finding (last touch 2026-05-16) |
| `holomush-7lp86` | P3 | Lua decrypt_own_audit_rows flattens decrypted-to-empty plaintext to nil | P3 orphan task, last touch 2026-05-26 |
| `holomush-7m4j` | P3 | RevokeOtherPlayerSessions: best-effort + per-ID failure reporting | P3 orphan task, last touch 2026-04-26 |
| `holomush-7nty` | P3 | Test gap: subscriber.go decodeAndAuthorize NoPlaintextReason stamps have no coverage | P3 orphan task, last touch 2026-05-13 |
| `holomush-7retn` | P3 | license:check (addlicense) scans local scripts/.venv — add .venv to -ignore | P3 orphan task, last touch 2026-05-24 |
| `holomush-7ssv` | P3 | Expand TypeScript unit test coverage for web client | P3 orphan task, last touch 2026-04-26 |
| `holomush-82u8` | P3 | Assess pgxpool leakage across internal/world/postgres repos; centralize if warranted | P3 orphan task, last touch 2026-05-09 |
| `holomush-88bcy` | P3 | Test/annotation: I-LIVE-5 single-source-of-liveness has only positive-path coverage | P3 orphan task, last touch 2026-05-31 |
| `holomush-8l20j` | P3 | Go comment on decodePolicyHashOrEmpty inaccurately cites 'genesis' empty case | P3 orphan task, last touch 2026-05-28 |
| `holomush-97d54` | P3 | core-scenes: explicit id-ordering assertion in ReadSceneLogForSnapshot test | P3 orphan task, last touch 2026-05-29 |
| `holomush-9cfv` | P3 | [E7] CI lint rule for char: prefix elimination | P3 orphan task, last touch 2026-04-26 |
| `holomush-9twm` | P3 | Extract detectMode to pure function for unit testing | P3 orphan task, last touch 2026-04-26 |
| `holomush-anoe` | P3 | cleanup: convert internal/store testing-style integration tests to Ginkgo | P3 orphan task, last touch 2026-05-23 |
| `holomush-awb3` | P3 | Apply ADR ti1b omission pattern to remaining optional attrs (owner_id, held_by, contained_in, shadows_id, property value/owner) | P3 orphan task, last touch 2026-05-22 |
| `holomush-b4tj` | P3 | chore(web): make SvelteKit dist regeneration deterministic or gitignore it | P3 orphan task, last touch 2026-04-26 |
| `holomush-bbcz` | P3 | Client-visible stale-DEK signaling via typed Event field | P3 orphan task, last touch 2026-05-15 |
| `holomush-bmpkb` | P3 | Update claude-code-hooks-design.md: file utilities are nudges, not blocks | P3 orphan task, last touch 2026-05-30 |
| `holomush-c8egn` | P3 | Web gateway: bound the per-stream 'seen' event-dedup set (slow leak) | P3 orphan task, last touch 2026-05-31 |
| `holomush-cfpd` | P3 | Service registry lacks protected service name blocklist | P3 orphan task, last touch 2026-05-30 |
| `holomush-dfmv` | P3 | Reaper tests are time-dependent (2-second WithTimeout) — flaky under slow CI | stale audit/review finding (last touch 2026-05-16) |
| `holomush-dgeyp` | P3 | Capture attach_moment_ms inside Subscriber.OpenSession at actual consumer-attach commit (iu8j hygiene) | P3 orphan task, last touch 2026-05-24 |
| `holomush-dixj` | P3 | Add ErrAccessEvaluationFailed tests for remaining service methods | explicitly deferred |
| `holomush-dq99` | P3 | Production KeySelector: replace identityProductionKeySelector placeholder with KEK-backed impl | stale audit/review finding (last touch 2026-05-16) |
| `holomush-dqc8e` | P3 | Polish: telnet ceiling wiring + 2 doc-comment precision fixes (rsoe6 review) | P3 orphan task, last touch 2026-05-31 |
| `holomush-dzz5` | P3 | Four web/handler.go stub RPC methods repeat identical CodeUnimplemented error (verify still applies) | stale audit/review finding (last touch 2026-05-16) |
| `holomush-e6kvc` | P3 | hostfunc-audit-table.md drifts from RegisteredFunctionsForAudit() — auto-sync or remove | P3 orphan task, last touch 2026-05-28 |
| `holomush-eifx` | P3 | Control plane shares CA with data plane | P3 orphan task, last touch 2026-04-26 |
| `holomush-ewxr` | P3 | Add composite index on player_sessions(player_id, expires_at) | P3 orphan task, last touch 2026-04-26 |
| `holomush-fbct9` | P3 | Test gap: telnet I-SURV-4 ceiling-exceed exit (transport-symmetry vs web) | P3 orphan task, last touch 2026-05-31 |
| `holomush-fl17b` | P3 | Test gap: I-LIVE-3 grid_present=false with a live non-grid (comms_hub) connection | P3 orphan task, last touch 2026-05-31 |
| `holomush-fnld3` | P3 | Wire publish/withdraw_publish per-action ABAC at the command leaf (or document inert policies) | P3 orphan task, last touch 2026-05-26 |
| `holomush-fvxlv` | P3 | docs(reference/events): make the generated events.md index orienting (generator change) | P3 orphan task, last touch 2026-05-29 |
| `holomush-g9vf` | P3 | Add MaxJustificationLength cap to RekeyRequest.Justification + admin handler validation | P3 orphan task, last touch 2026-05-13 |
| `holomush-gh6hv` | P3 | SDK DecryptOwnAuditRows helper panics on nil PluginHostServiceClient | P3 orphan task, last touch 2026-05-26 |
| `holomush-ghm` | P3 | Add comprehensive Prometheus metrics across system | P3 orphan task, last touch 2026-05-01 |
| `holomush-gmfk` | P3 | AccessPolicyEngine call-site ergonomics: AccessRequest type encap + shared eval helper | stale audit/review finding (last touch 2026-05-16) |
| `holomush-h33u` | P3 | Rail.svelte: expose class prop for shadcn-convention parity | P3 orphan task, last touch 2026-04-26 |
| `holomush-hfb3` | P3 | Optimize filter-at-delivery: cache session floor in per-Subscribe goroutine | P3 orphan task, last touch 2026-05-19 |
| `holomush-hkftf` | P3 | E2E: presence ghost-clear (kill-transport) + core-restart reconnect (rsoe6 T15 c/d follow-up) | P3 orphan task, last touch 2026-05-31 |
| `holomush-hvcs` | P3 | Add integration test for S1 system-subject injection via external ingress | explicitly deferred |
| `holomush-i767` | P3 | ⌘R reverse-i-search overlay for command history | P3 orphan task, last touch 2026-04-26 |
| `holomush-ih96` | P3 | ABAC: distinguish critical vs non-critical attribute providers for fail-closed behavior | explicitly deferred |
| `holomush-ir5q` | P3 | Optimize plugin build: dynamic Taskfile tasks, caching, parallel builds | explicitly deferred |
| `holomush-jaqp` | P3 | Verifier INV-E27 per-entry subject-derived scope cross-check (defensive) | P3 orphan task, last touch 2026-05-13 |
| `holomush-jqrk` | P3 | Investigate moving guest authentication into a plugin | P3 orphan task, last touch 2026-05-01 |
| `holomush-k0r5o` | P3 | docs(reference/grpc-api): add orientation + api-guide link to generated grpc-api.md (generator/SP0-SP4) | P3 orphan task, last touch 2026-05-29 |
| `holomush-k30c` | P3 | In-game admin command to grant/revoke crypto.operator capability | P3 orphan task, last touch 2026-05-15 |
| `holomush-k72a` | P3 | A11y: ARIA-live announcement for backfill loading | P3 orphan task, last touch 2026-04-26 |
| `holomush-kb1f` | P3 | Backfill scroll-up pagination (older history via before_id) | P3 orphan task, last touch 2026-04-26 |
| `holomush-kl9w` | P3 | Plugin manifest hot-reload callback infrastructure | P3 orphan task, last touch 2026-05-15 |
| `holomush-ktxch` | P3 | Ginkgo AfterEach nil-env teardown can mask setup failure (scene_history_readback_test) | P3 orphan task, last touch 2026-05-26 |
| `holomush-l3an` | P3 | Add ListActive error injection to handler mocks — cover who/wall/boot error paths | stale audit/review finding (last touch 2026-05-16) |
| `holomush-l60y` | P3 | Refactor QueryStreamHistory tests into table-driven cases | P3 orphan task, last touch 2026-04-26 |
| `holomush-lw0o` | P3 | Memoize getBufferSize() in terminalStore.ts | P3 orphan task, last touch 2026-04-26 |
| `holomush-lyw1` | P3 | reorder Subscribe nil-subscriber guard vs ownership validation | P3 orphan task, last touch 2026-05-01 |
| `holomush-lzow` | P3 | Implement scope-aware CanPerformAction evaluation | P3 orphan task, last touch 2026-04-26 |
| `holomush-m88d` | P3 | Cache-resident plaintext DEK material: bound lifetime in dek.Cache LRU | P3 orphan task, last touch 2026-05-15 |
| `holomush-ma2n` | P3 | Add role column to characters table | P3 orphan task, last touch 2026-04-26 |
| `holomush-n9pd` | P3 | Disconnect makes two CountConnectionsByType round-trips — combine into single query | stale audit/review finding (last touch 2026-05-16) |
| `holomush-nkac` | P3 | Delete dead HandleSay/HandlePose methods in internal/core/engine.go | stale audit/review finding (last touch 2026-05-16) |
| `holomush-nko7` | P3 | multi-protocol fan-out e2e: telnet + web see same pose | P3 orphan task, last touch 2026-04-26 |
| `holomush-omy8` | P3 | Add SetGuestMetadata store method for safe guest-fields backfill on reattach | P3 orphan task, last touch 2026-05-17 |
| `holomush-otc3v` | P3 | obs(sentry): wire CaptureException -> grouped Sentry Issues + broaden error capture | P3 orphan task, last touch 2026-05-28 |
| `holomush-p0wc` | P3 | Subscribe: return nil for clean context.Canceled instead of SUBSCRIPTION_CANCELLED error | stale audit/review finding (last touch 2026-05-16) |
| `holomush-pfo7q` | P3 | core-scenes: trim over-explained ULID-ordering comment in ReadSceneLogForSnapshot test | P3 orphan task, last touch 2026-05-29 |
| `holomush-ph1l` | P3 | admin_authenticate_e2e_test.go: refactor T25+T26+T27 to Ginkgo Ordered containers | P3 orphan task, last touch 2026-05-13 |
| `holomush-qhitt` | P3 | Add favicon.ico to web client (currently 404s) | P3 orphan task, last touch 2026-05-23 |
| `holomush-qp0a` | P3 | crypto: background GC sweep for orphan DEK rows from aborted Rekey checkpoints | P3 orphan task, last touch 2026-05-13 |
| `holomush-qybt` | P3 | Task test:int should use glob-based discovery | P3 orphan task, last touch 2026-04-26 |
| `holomush-r2tv` | P3 | Make disconnect connection-remove + count atomic | P3 orphan task, last touch 2026-04-26 |
| `holomush-rcr2` | P3 | Consider scenes.updated_at column for sort/display in Phase 8/9 | P3 orphan task, last touch 2026-04-26 |
| `holomush-rr3p8` | P3 | Composer command chip: tier-3 player-alias resolution (deferred from holomush-2zjio) | P3 orphan task, last touch 2026-05-30 |
| `holomush-s54y` | P3 | Plugin Phase 4 cleanup: auto-generate hostfunc bindings from proto + dynamic reload | stale audit/review finding (last touch 2026-05-16) |
| `holomush-sjd6` | P3 | Add pprof HTTP endpoints to server for runtime profiling | P3 orphan task, last touch 2026-04-26 |
| `holomush-ton17` | P3 | docs(operating/crypto): write master-key bootstrap runbook in crypto-setup once Phase 8 lands | P3 orphan task, last touch 2026-05-28 |
| `holomush-u3o9` | P3 | Control plane RPC: AdminResetPassword | P3 orphan task, last touch 2026-04-26 |
| `holomush-uhiz` | P3 | Server-side population of optional sidebar fields (mood, lastMode, isIdle) | P3 orphan task, last touch 2026-04-26 |
| `holomush-wtmn` | P3 | ABACStack.Close drops sqlDB.Close error when AuditLogger.Close already failed | stale audit/review finding (last touch 2026-05-16) |
| `holomush-wyr3` | P3 | cleanup: convert auto_focus_on_join_test.go TestAutoFocus* suite to table-driven | P3 orphan task, last touch 2026-05-29 |
| `holomush-wzhr` | P3 | Catch-up replay protocol via last_invalidation_seq (deferred from ojw1.3.19, Decision 6) | stale audit/review finding (last touch 2026-05-16) |
| `holomush-x8v4i` | P3 | docs(extending): add grounded client-connection walkthrough to api-guide.md | P3 orphan task, last touch 2026-05-28 |
| `holomush-xe62` | P3 | events_audit composite index on (dek_ref, dek_version) — profile-then-decide | P3 orphan task, last touch 2026-05-13 |
| `holomush-xqub` | P3 | Backlog: Full ExitProvider and SceneProvider attributes (Phase 7.7+ expansion) | P3 orphan task, last touch 2026-05-16 |
| `holomush-y9au` | P3 | Cover XDG-default-path non-ErrNotExist stat error path in config_test.go | stale audit/review finding (last touch 2026-05-16) |
| `holomush-yfis` | P3 | OTel tracing for backfill | P3 orphan task, last touch 2026-04-26 |
| `holomush-z1ys` | P3 | Orphaned internal/plugin integration tests | P3 orphan task, last touch 2026-04-26 |
| `holomush-z3y9` | P3 | Building & objects plugin integration tests | stale audit/review finding (last touch 2026-05-16) |
| `holomush-z51u` | P3 | INV-E meta-test enforcing INV-E[N] ↔ test-name binding (Phase 3c pattern) | P3 orphan task, last touch 2026-05-13 |
| `holomush-1k13` | P4 | MOTD on session start with appropriate commands | P4 backlog-tier |
| `holomush-255` | P4 | Gateway <-> Core ping/pong health checking | P4 backlog-tier |
| `holomush-2e57` | P4 | Web StreamEvents sets ClientType='terminal' — should it be 'comms_hub'? Design clarification needed | P4 backlog-tier |
| `holomush-3xfs` | P4 | Audit zero CharacterID handling in Subscribe | P4 backlog-tier |
| `holomush-4cet` | P4 | chore(dev): fix gopls workspace path confusion in jj worktree symlinks | P4 backlog-tier |
| `holomush-5g6.1` | P4 | [E13] Design: iOS Client Architecture | P4 backlog-tier |
| `holomush-5g6.2` | P4 | [E13] Implementation Plan: iOS Client | P4 backlog-tier |
| `holomush-5g6.3` | P4 | [E13] Phase 13.1: SwiftUI Scaffold - App Structure | P4 backlog-tier |
| `holomush-5g6.4` | P4 | [E13] Phase 13.2: WebSocket Transport - Connect to Server | P4 backlog-tier |
| `holomush-5g6.5` | P4 | [E13] Phase 13.3: Terminal UI - Input and Scrollback | P4 backlog-tier |
| `holomush-5g6.6` | P4 | [E13] Phase 13.4: Push Notifications - Event Alerts | P4 backlog-tier |
| `holomush-5g6.7` | P4 | [E13] Phase 13.5: App Store Submission - Published | P4 backlog-tier |
| `holomush-7t20` | P4 | Add concurrent dispatch engine failure test for metric consistency | explicitly deferred |
| `holomush-8lhd` | P4 | holomushlint: forbid errors.Is(err, oopsSentinel) — tautological under samber/oops | P4 backlog-tier |
| `holomush-9w8r` | P4 | Cleanup: drop unused oops.Code('SESSION_ENDED') wrap in quit.go and dispatcher.go | P4 backlog-tier |
| `holomush-ay5l` | P4 | Fix broken relative links in markdown docs (MD057) | P4 backlog-tier |
| `holomush-box9` | P4 | Document Lua state limitations for multi-turn commands | P4 backlog-tier |
| `holomush-ce8a` | P4 | Pre-production: Migration 30 bootstrap_metadata backfill (trigger-deferred) | P4 backlog-tier |
| `holomush-cyi` | P4 | Control gRPC authentication with special client cert or admin privileges | P4 backlog-tier |
| `holomush-jeej` | P4 | Cluster operations documentation — write site/docs/operating/cluster.md | P4 backlog-tier |
| `holomush-k2hq` | P4 | Follow-up: separate unrelated subscriber.go and codes.go changes from ABAC PR | explicitly deferred |
| `holomush-k5qb` | P4 | [E7] Metrics query and alerting documentation | P4 backlog-tier |
| `holomush-mctg` | P4 | RekeyAbort audit payload: carry operator-supplied reason in Justification | P4 backlog-tier |
| `holomush-out` | P4 | Plugin self-describing manifests for dynamic loading | P4 backlog-tier |
| `holomush-p4un` | P4 | [E7] Performance benchmark for AccessPolicyEngine vs AccessControl | P4 backlog-tier |
| `holomush-qalj` | P4 | Force-destroy slog severity: Warn → Error for monitoring escalation | P4 backlog-tier |
| `holomush-ujbs7` | P4 | Add forced-tie regression lock for SceneStore ListParticipants / pose-order determinism | P4 backlog-tier |
| `holomush-ujuv` | P4 | crypto rekey: --purge-hot flag (master spec §6.3 Phase 4.4) | P4 backlog-tier |
| `holomush-wdoo` | P4 | [Needs human review] Investigate orphan wr9.127 with blank title | P4 backlog-tier |
| `holomush-x4n1r` | P4 | Scenes Phase 7: Templates | P4 backlog-tier |
| `holomush-zzrd` | P4 | Remove transitional connection_id != '' gate in Subscribe handler | P4 backlog-tier |
