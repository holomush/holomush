  Encountered: holomush-hz0v4.14.5 (2026-06-01) — READY.

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

- **Multi-family scope migration (EVENTBUS = GW+ROPS+P7-split) is the same shape
  as single-family but with three legacy specs feeding one scope, and includes a
  DENSE-with-letter-suffix renumber (GW-3a) (hz0v4.14.12 READY).** Verify per
  family: GW 1..16 DENSE over spec set {1,2,3,3a,4,5,6,7,8,9,10,11,13,14,15,16}
  (16 ids; GW-3a→EB-4 shifts 4..11 by one; GW-12 forward-declared, NOT migrated);
  ROPS 1..8 → 16+N (offset by GW count); P7 SPLIT — only audit-half P7-3/4/10 →
  25/26/27, P7-1,2,5-9,11-16 STAY as bare INV-P7-N for later CRYPTO/PLUGIN scopes.
  Checks that held: (1) `awk` ref/legacy match scans give FALSE mismatches on
  `3a` (splits at `INV-GW-3`), `refs: []`, and `,}` — hand-read the diff, don't
  trust awk; (2) the per-family coverage test for GW was
  `TestAllGatewayRegistryInvariantsHaveTests` living INSIDE
  `internal/gateway_invariants/meta_test.go` (NOT a separate
  `i_<old>_coverage_test.go`) — retired correctly, generic
  `TestInvariantTokenBoundariesRejectFalsePositives` KEPT with documented
  INV-GW-* regex fixtures (residual GW tokens there are intentional, not
  unmigrated annotations); (3) global partition: 76 owned paths across all
  scopes, 0 dup, 0 glob/concrete overlap — other scopes (PRIVACY/PRESENCE) may
  list an EVENTBUS-owned file in THEIR shared_files (correct: shared = rides on
  another scope's owned file); (4) generated pb.go/_pb.ts + proto sources must be
  comment-only — filter `^[+-]` minus `(//|*|/*)` → empty. WATCH FOR: stray
  trailing-comma edits to a NON-migrated scope's flow-mapping (saw `INV-60",}`
  added to INV-CLUSTER's last ref) — valid YAML, renderer-inert, but out-of-scope
  noise in a closed-world diff → Low non-blocking. Encountered: hz0v4.14.12
  (2026-06-02) — READY.

- **Largest multi-family scope migration to date (SCENE = P4+P5+P6+FS(+FW)+Y5INX+SH
  + phase-8 bare 2/5/6/7 → 59 ids across 6 origin specs, 86 files) follows the
  established shape; all checks held (hz0v4.14.13 READY).** Reusable patterns: (1)
  the P4/P5 in-place coverage meta-tests (`inv_p4/p5_coverage_meta_test.go`) are
  `testName`-EXISTENCE checks (go/parser collects Test* func names; table has
  `{inv, testName}` rows) — robust to rename so long as the `testName:` strings AND
  any manually-renamed funcs move in lockstep. Distinct from the FRAGILE
  `// Verifies:`-annotation scanners (those MUST be deleted on migration, per .14.9).
  Confirm which kind before deciding delete-vs-migrate-in-place. (2) The retired
  `scenes_phase6_invariants_test.go` SCANNED for `INV-P6-N` substrings (broke on
  rename); verify the deleted file defined ONLY locals (`phase6Invariants`,
  `invP6CitationRE`, `TestPhase6...`) and merely USED shared helpers — `rg` the
  deleted symbols across `test/meta/` → zero dangling. (3) Manual `\b`-unreachable
  underscore-form renames: `TestINV_P4_4/5→_SCENE_4/5`,
  `TestStreamScopeFloor_..._INV_P4_9→_SCENE_9` — grep old names → ZERO, and chase
  their string refs (coverage table) + cross-file doc-comment cite
  (`late_joiner_temporal_floor_test.go`). (4) Closed-world with co-located foreign
  families: `INV-S*` substrate (~48 sites, NOT a scope yet) + `INV-S9`/`INV-S5`
  must survive UNTOUCHED next to rewritten scene tokens (e.g. `INV-S9 / INV-SCENE-32`);
  crypto's bare `INV-2/5/6/7` elsewhere untouched (scene rewrite is path-scoped).
  (5) Same renderer-inert `token: "...",}` trailing-comma noise on an out-of-scope
  line (here INV-P7-10 in EVENTBUS) recurs — Low non-blocking. spec-only refs:[]
  count must equal exactly the declared spec-only set (FS-2/6/7 + SH-1..5 = 8).
  Encountered: hz0v4.14.13 (2026-06-02) — READY.

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
