- **Per-family coverage meta-test MUST be DELETED on registry migration — not
  optional (hz0v4.14.9 NOT-READY).** When a scope migrates I-<OLD>-N →
  INV-<SCOPE>-N, the legacy `test/meta/i_<old>_coverage_test.go` greps for
  `// Verifies: I-<OLD>-N` annotations (e.g. `iPrivVerifiesRE =
  //\s*Verifies:\s*I-PRIV-(\d+)`) and requires each 1..N to have ≥1 binding. The
  migration renames every `// Verifies: I-<OLD>-N` to `// Verifies: INV-<SCOPE>-N`,
  so the stale test finds ZERO and fails ALL N invariants. `.14.5` (PRESENCE)
  correctly DELETED `i_pres_coverage_test.go`; `.14.9` (PRIVACY) MISSED the
  deletion → `TestEveryIPRIVInvariantHasAtLeastOneTestBinding` failed I-PRIV-1..8
  (`DONE 15 tests, 1 failure`). The file's own doc-comment even says "PRIVACY
  follows when it migrates." Coverage is absorbed by registry-driven
  `TestEveryRegistryInvariantHasBinding` (passes — found all 8). Review check: after
  confirming `rg 'Verifies:\s*I-<OLD>-[0-9]' --glob '*_test.go'` → ZERO, the
  matching `i_<old>_coverage_test.go` MUST be absent from the tree; if present,
  it's a guaranteed FAIL. Also note: `r.Token` in registry refs is UNUSED by
  `checkProvenance` (line 520 greps `e.ID` canonical, not `r.Token`), so refs
  recording legacy `token: "I-PRIV-1"` is cosmetic-only, harmless. Bare `go vet
  -tags=integration` surfaces `lostcancel` warnings at `context.WithTimeout` sites
  that already carry `//nolint:govet` — NOT findings (bare vet ignores golangci
  nolint; `task lint` is the real gate). Encountered: hz0v4.14.9 (2026-06-02) — NOT READY.

- **Bare-INV-N (CLUSTER) migration is the same shape as I-<OLD>-N but the
  per-family test lives in `test/meta/inv_binding_test.go`, not a separate
  `i_<old>_coverage_test.go` (hz0v4.14.11 READY).** Phase-3c invariants
  (INV-53..60 + INV-28/29) were tracked by
  `TestEveryPhase3cInvariantHasAtLeastOneTestBinding` (scanned `// Verifies:
  INV-<digits>`). On migration this test + its 3 locals
  (`phase3cInvariants`/`invLintEnforced`/`verifiesRE`) MUST be removed, but the
  shared `findRepoRoot`/`skipDirs` helpers in the SAME file MUST be KEPT (used by
  10+ meta files — `liveness_invariants_test.go`, `proto_doc_comments_test.go`,
  `invariant_registry_test.go`, etc.). Verify file still compiles via `task test
  -- ./test/meta/`. The lone surviving `TestEveryPhase3c...` mention is fine if
  it's in the rewritten doc-comment explaining the retirement (not a symbol ref).
  Review pattern for bare-INV-N: (1) `rg '\bINV-(28|29|53..60)\b' -g '!docs/**'
  -g '!*.md' -g '!test/meta/**'` → exit 1 (NO matches); (2) closed-world
  {file,token} rewrite preserves co-located non-set bare tokens — INV-27 in
  participants_cache.go must read "INV-CLUSTER-9 + INV-27", and CRYPTO tokens
  (INV-9,12,13,17-21,25,26,30,32,33,34,37,39,49) elsewhere untouched; (3) DENSE
  renumber: non-contiguous legacy maps ascending-by-position to 1..N — verify each
  entry's `legacy:` AND every `refs[].token` equals the legacy token (awk
  block-scan), no dup legacy; (4) gorules is a SEPARATE module — its analyzer
  user-facing diagnostic message + Doc + testdata `// want` directives rename in
  lockstep (INV-58→INV-CLUSTER-8); run `task test:gorules` after `go clean
  -testcache` (else cached `ok` masks the testdata edit); (5) `task
  lint:invariants` (= `inv-render -check`) must exit 0 for invariants.md sync.
  Origin SPEC is NOT migrated (spec line still says INV-58) — by design; legacy
  column in invariants.md/yaml records the link. Encountered: hz0v4.14.11
  (2026-06-02) — READY.

- **Multi-family scope (EVENTBUS=GW+ROPS+P7-split) + DENSE letter-suffix renumber
  (GW-3a) (hz0v4.14.12 READY).** GW 1..16 dense over {1,2,3,3a,4..11,13..16};
  P7 SPLIT (audit-half P7-3/4/10→25/26/27, rest stay bare for later scopes). `awk`
  ref/legacy scans FALSE-mismatch on `3a`/`refs:[]`/`,}` — hand-read. Per-family
  coverage test was `TestAllGatewayRegistryInvariantsHaveTests` in
  `internal/gateway_invariants/meta_test.go`; generic
  `TestInvariantTokenBoundariesRejectFalsePositives` KEPT (its INV-GW-* fixtures
  intentional). Global partition: other scopes may list a foreign-owned file in
  THEIR shared_files. Trailing-comma `,}` noise recurs — Low non-blocking.

- **Largest multi-family (SCENE=P4+P5+P6+FS+Y5INX+SH+phase-8 bare → 59 ids, 86
  files) (hz0v4.14.13 READY).** P4/P5 coverage meta-tests
  (`inv_p4/p5_coverage_meta_test.go`) are `testName`-EXISTENCE checks (go/parser
  Test* names; `{inv,testName}` table) — robust to rename if `testName:` strings +
  renamed funcs move in lockstep; DISTINCT from fragile `// Verifies:` scanners
  (delete those, per .14.9). Confirm which kind before delete-vs-migrate-in-place.
  Manual `\b`-unreachable underscore renames (`TestINV_P4_4→_SCENE_4`): grep old →
  ZERO + chase string refs + cross-file cites. spec-only `refs:[]` count = declared
  spec-only set exactly. Same `,}` noise — Low non-blocking.

- **Scope with DEFERRED bare-INV-N + foreign tokens uses file-path owned_paths
  (NOT /** globs) to keep the residual walk clean (hz0v4.14.14 PLUGIN READY).**
  When a scope's directory holds un-migrated bare INV-N (deferred to a later pass)
  or foreign-family tokens, owning the `dir/**` glob would make the residual walk
  trip on those left-behind tokens. Fix: list only the specific migrated files in
  `owned_paths`; put every mixed/foreign-carrying file in `shared_files` (shared ≠
  residual-walked). PLUGIN: `pluginauthz/**`, `wholesystem/**`, `plugin/**` REMOVED
  from scaffold globs → only `config.go`, `identity_registry.go`, `plugin_repo.go`,
  2 eventbus grep tests, 3 WS harness files owned. Review checks that HELD: (1)
  per-file token scan of every owned file → ONLY INV-<SCOPE>-N (no bare/foreign
  leftover) ⇒ residual walk clean; (2) every mixed file in shared_files actually
  carries a foreign/deferred token (lua/host.go=INV-6/7/8/S5, manager.go=EVENTBUS-11/
  M1/S5, subsystem.go=INV-1/4/SCENE-38, harness.go=PRIVACY/ACCESS-4/P7-9/bare-1,
  plugins.go=ACCESS-4) — justifies shared-not-owned; (3) STORE-owned
  no_delete_grep_test.go is in PLUGIN shared (not owned); (4) deferred bare INV-1..5/8/11
  LEFT in pluginauthz/hostfunc untouched. P7-split sanity: PLUGIN block has NO P7 entry,
  no P7 token rewritten (pre-existing EVENTBUS P7-3/4/10 refs untouched). W9ML source
  spec defines 1..9 (redesign §2.1 undercounted to 1..8) — code reality authoritative;
  spec-only refs:[] = exactly W9ML-1..6 (6). Same `token: "...",}` trailing-comma noise
  recurred AGAIN (3rd time: SCENE INV-7 ref) — Low non-blocking; renderer-inert.
  Encountered: hz0v4.14.14 (2026-06-03) — READY.

- **Final/hardest CRYPTO leg (52 ids: reader-opts 1..6→1..5 + master-crypto bare
  9..49 + RB-1..12 + P7-crypto-half) is the same shape; all checks held (hz0v4.14.15
  READY).** Key residual-walk subtlety confirmed at source: `bareInvRE` in
  `test/meta/invariant_registry_test.go:488` is `\bINV-\d+\b` — matches ONLY bare
  NUMERIC `INV-9`. It does NOT match `INV-P7-1` / `INV-RB-1` (the `-P7-`/`-RB-`
  segment breaks `\d+` right after `INV-`). So a surviving descriptive prose
  reference like `// the legacy INV-P7-1..16 set, migrated to...` in an OWNED file
  (phase7_boundary_meta_test.go:25) is INERT to the residual walk — NOT a finding.
  Residual walk also `continue`s on shared files (line 554). The
  `phase7_boundary_meta_test.go` is a `testName`-EXISTENCE meta-test (collects Go
  Test* func names via go/parser; `tc.inv` is a t.Run label only, robust to rename)
  — distinct from `// Verifies:`-grep scanners that MUST be deleted (.14.9). Its
  `cases` table `inv` strings migrate in lockstep. Shared_files MAY be unowned by
  any scope: `TestOwnedPathsPartition` (line 113) only forbids the SAME glob in two
  scopes' owned_paths; an unowned shared file (crypto_manifest.go carrying only
  INV-CRYPTO-27; plugin_role_permissions_test.go only INV-CRYPTO-48) is permissible —
  `checkProvenance` line 524 accepts shared-OR-owned. Dense renumber maps
  non-contiguous legacy ascending-by-position (reader INV-5 was never a real inv →
  1,2,3,4,6→1,2,3,4,5; master INV-17→CRYPTO-9). Provenance check greps canonical
  `e.ID` (line 520), NOT `r.Token` (legacy FROM-anchor) — so 209 refs showing
  "legacy TOKEN-ABSENT" is EXPECTED/correct; re-scan for canonical id instead.
  grpc-api.md table-row "non-comment" diffs are doc prose; verify each ± pair
  differs ONLY in the token. No `token:",}` trailing-comma noise this time.
  Encountered: hz0v4.14.15 (2026-06-03) — READY.

- **Final bare-INV-N PER-SITE pass (tri-overloaded namespace: same bare number
  → DIFFERENT outcomes per file) — all checks held (hz0v4.14.22 READY).** The
  whole point: bare INV-1 → INV-PLUGIN-22 ONLY in plugin-evaluate files
  (pluginauthz/evaluate.go, hostfunc/evaluate.go, hostfunc/stdlib_settings.go,
  goplugin/evaluate_invariants_test.go), while command-visibility INV-1 (commands.go,
  functions.go, setup/subsystem.go) LEFT bare; settings INV-6 (host_service.go) +
  fence INV-6/7 (lua/host.go) LEFT. Verify per-file, not per-number. Key checks:
  (1) `rg '\bINV-[0-9]+\b'` over EVERY owned file → ZERO (residual walk = `bareInvRE
  \bINV-\d+\b` at invariant_registry_test.go:488); foreign bare tokens survive ONLY
  in shared_files (residual `continue`s on shared, line ~550). (2) checkProvenance
  (line 492) greps CANONICAL `e.ID` at each ref site, NOT `r.Token` — so refs
  recording legacy `token: "INV-15"/"INV-A16"/"INV-4"` is the standing FROM-anchor
  convention, NOT a finding; the canonical token must ALSO be physically present
  (it is). (3) Cross-scope ownership is legit: access/setup/subsystem.go +
  setup_warn_integ_test.go are PLUGIN-owned (INV-4 audit-wiring is plugin-evaluate,
  not ABAC) — confirm each appears ONCE in owned_paths and carries ONLY the migrated
  token; no scope owns `internal/access/**` glob so TestOwnedPathsPartition is safe.
  (4) seed.go/seed_test.go genuinely multi-scope (PRIVACY I-PRIV-6 + now ACCESS) →
  ACCESS-shared not owned; seed_smoke_test.go/spec_amendments_test.go ACCESS-owned.
  (5) Bare `A16` prose (no INV- prefix) MUST stay — `\bA16\b` rewrite would corrupt
  `INV-A16`; `INV-A16` → INV-ACCESS-8 cleanly. (6) Zero `.go` executable-line edits;
  every .go diff line is a comment or test-assertion string-literal token swap.
  (7) This diff CLEANED a pre-existing `,}` (INV-RA-6 line 1657) but ADDED a new one
  (INV-A16 line 1682) — 5th recurrence, Low non-blocking, renderer-inert. PLUGIN dense
  1..28, ACCESS dense 1..8. Encountered: hz0v4.14.22 (2026-06-03) — READY.

- **First NOT-READY in the .14.x classification series: registry guards are
  BLIND to unregistered files, so a green meta-test run does NOT prove a family
  migration is complete (hz0v4.14.23 INV-F NOT READY).** Two defects survived a
  fully-green run (lint:invariants, provenance, partition, binding, 617 unit tests,
  int-compile): (A) **51 residual bare `INV-F*` token refs** (INV-F1/2/3/6/9/10/11/
  12/14/15/17) in THREE cmd/holomush files NOT in the diff: admin_read_stream_e2e_test.go
  (45), readstream_wiring_test.go (4), admin_authenticate_e2e_test.go (2). These are
  the SAME family the bead migrates, but the files are neither owned_paths nor
  shared_files nor in any registry `refs[]`, so the residual walk + provenance guard
  never look at them → green despite incomplete migration. 4 of those are STALE
  cross-file func-name cites (`TestINV_F2/F6/F3/F17_...` at lines 1124/1159/1245/2137)
  pointing at funcs THIS diff RENAMED to TestINV_CRYPTO_54/56/55/67 — now dangling.
  (B) **Range-rewrite corruption**: phase7_boundary_meta_test.go:25 prose
  `INV-P7-1..16` → `INV-CRYPTO-38..16` (tool rewrote left side of a `..N` range,
  left suffix). `INV-P7-1` is a P7 token (handled in .14.15), OUT OF SCOPE for the
  F pass — tool should not have touched it. Substring `INV-CRYPTO-38` keeps
  provenance green (real anchor is the table row at line 54), so CI is blind.
  Redesign spec (2026-06-01-...-redesign.md:42) DOCUMENTS this exact class:
  "INV-CRYPTO-1..5 — nonsensical as crypto. Every CI gate passed." Same bug the
  crypto-reviewer caught on F-compound-refs (INV-CRYPTO-56/F7), recurred on a
  `..N` range. **Review pattern for family migrations: do NOT trust green guards
  as proof of completeness. ALWAYS run `rg -c '\bINV-<OLD>[0-9]' --glob '!docs/**'`
  over the WHOLE tree (not just diffed files) — residual hits in unregistered files
  = incomplete migration. Also `rg 'INV-<SCOPE>-[0-9]+\.\.[0-9]+'` for range
  corruption, and `rg 'TestINV_<OLD>[0-9]'` for dangling renamed-func cites.**
  Mechanical search-replace correct part: 35 diffed files all comment/string/test-func
  swaps, dense 53..67, no executable edits, generated artifacts in sync. The DEFECT
  is what was MISSED, not what was changed. Encountered: hz0v4.14.23 (2026-06-03) — NOT READY.

- **INV-S\* substrate-contract per-token SPLIT across THREE scopes + master-crypto
  fence (hz0v4.14.24 READY).** S3→PLUGIN-31, S5→PLUGIN-32, S4→EVENTBUS-28,
  S9→SCENE-60; master fence INV-6→PLUGIN-29, INV-7→PLUGIN-30. Checks that held:
  (1) tri+-overload: 8 fence files (sensitivity_fence{,_test}.go, event_emitter{,_crypto_test}.go,
  lua/host.go, pkg/plugin/event.go, integrationtest/crypto.go, plugin.proto) → ZERO bare
  INV-6/7; foreign INV-6/7 LEFT in web frontend (themeStore/commandListStore/composerChip),
  CI-tooling (tooling_no_mandatory_int_test.go), reader-opts (cmd/holomush/sub_grpc_test.go),
  settings (goplugin/host_service.go:610), AND phase-8 board-content SCENE-58/59 legacy
  (a DIFFERENT INV-6/7 namespace — its legacy column STAYS INV-6/7). (2) S1/S2/S6/S7/S8/S10
  have ZERO code/manifest refs → split is complete; only S3/S4/S5/S9 were ever code-bound.
  Remaining INV-S* hits are ONLY docs/roadmap.md + site/docs + ADRs (prose, not annotations) +
  invariants.yaml legacy/refs.token (FROM-anchor). (3) service.go + store.go genuinely
  multi-scope → carry BOTH INV-EVENTBUS-28 (S4) AND INV-SCENE-60 (S9): EVENTBUS-shared +
  SCENE-owned, listed in both. (4) checked cmd/ + api/proto/ (prior-pass blind spots, see
  .14.23) → clean. Dense: PLUGIN 1..32, EVENTBUS 1..28, SCENE 1..60, no dup/gap. Trailing-comma
  ,} noise recurred (6th time): added 2 (EVENTBUS-28/SCENE-60 last refs), cleaned 2 — Low
  non-blocking, renderer-inert. Encountered: hz0v4.14.24 (2026-06-04) — READY.
