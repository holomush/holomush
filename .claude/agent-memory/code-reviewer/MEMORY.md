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
