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
