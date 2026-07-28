---
phase: 09-test-quality-code-health-sweep
plan: 08
subsystem: testing
tags: [tls, x509, certificates, coverage, oops, lifecycle, negative-path-testing]

requires:
  - phase: 09-01
    provides: the repaired coverage measurement chain and the distinction between the codecov metric and the local go-tool-cover metric
provides:
  - Thirteen behavioural negative-path test functions across internal/tls, each verified falsifiable by mutation
  - Coverage of every reachable error branch in certificate generation, save, load and validation
  - Coverage of the TLS lifecycle subsystem's fail-to-start behaviour and its per-stage error diagnosis
  - A documented classification of the seventeen remaining uncovered blocks as unreachable-without-injection
affects: [09-18 test naming ratchet, 09-19 coverage floor gate, QUAL-02 verification]

tech-stack:
  added: []
  patterns:
    - "Mutation-based falsifiability check: every added test was proven to fail against a deliberately broken production branch before being accepted"
    - "Precondition-asserting skip: a permission-dependent subtest probes that chmod actually denies before running, so it cannot pass vacuously as root"

key-files:
  created: []
  modified:
    - internal/tls/certs_test.go
    - internal/tls/subsystem_test.go

key-decisions:
  - "The plan's 76.2% baseline is a codecov LINE ratio; its 80% gate is a go-tool-cover STATEMENT ratio. They are different instruments and the package already read 83.9% on the gate's instrument before any test was written, so the plan's Task 2 gate could not fail. Replaced with a falsifiable strict-increase gate on a single instrument."
  - "SaveCertificates cannot distinguish which artifact failed via the oops `operation` key — oops merges context innermost-first, so the outer label is shadowed. Tests assert the `path` key, which does survive, plus fail-fast file-presence assertions."
  - "Seventeen uncovered blocks are crypto/rand, x509 create/parse, pem.Encode and file-Close failures on already-validated inputs. Left uncovered rather than contorting production code to reach them; the floor is 80%, not 100%."

patterns-established:
  - "Negative control before acceptance: mutate the production branch, confirm the test fails, restore, confirm the test passes"
  - "Assert the failure reason via errutil.AssertErrorCode / AssertErrorContext, never a message substring"

requirements-completed: [QUAL-02]

coverage:
  - id: D1
    description: "Every reachable error branch in certificate generation, save and validation asserts its specific failure reason"
    requirement: "QUAL-02"
    verification:
      - kind: unit
        ref: "task test -- ./internal/tls/ (86 tests, coverage 91.7% of statements)"
        status: pass
    human_judgment: false
  - id: D2
    description: "The TLS lifecycle subsystem fails to start rather than starting with an unvalidated config, and reports which certificate stage failed"
    requirement: "QUAL-02"
    verification:
      - kind: unit
        ref: "internal/tls/subsystem_test.go#TestTLSSubsystemPrepareReportsSetupFailureAndLeavesTheSubsystemUnstarted"
        status: pass
      - kind: unit
        ref: "internal/tls/subsystem_test.go#TestEnsureCertsReportsWhichGenerationStageFailed"
        status: pass
    human_judgment: false
  - id: D3
    description: "The internal/tls codecov figure clears the 80% named floor"
    requirement: "QUAL-02"
    verification: []
    human_judgment: true
    rationale: "codecov's figure is produced by the CI upload pipeline, not reproducible locally. The local proxy for that metric moved 80.98% -> 90.23%, but only the landed codecov report can confirm the floor is cleared."

status: complete
---

# Phase 09 Plan 08: TLS Package Coverage Floor Summary

Raised `internal/tls` from 83.9% to 91.7% statement coverage with thirteen negative-path tests,
each proven falsifiable by mutating the production branch it covers — and found that the plan's
own coverage gate could not fail.

## Inherited Work — What It Was and What I Did

A previous executor died mid-stream on an API error, leaving 263 uncommitted, never-compiled,
never-run lines in `internal/tls/certs_test.go`: ten test functions and no `subsystem_test.go`
work at all.

**Verification performed:** I backed the file up (SHA-256 confirmed), restored the HEAD version,
measured the true baseline, then restored the inherited work and ran it.

**It failed. Eight failures across two of the ten functions.**

`TestSaveCertificatesReportsWhichArtifactFailedToWrite` and its client counterpart asserted that
`SaveCertificates` reports `operation: "save CA certificate"` (and three siblings). The production
code does set that label — but `oops` merges chain context **innermost-first**, so the inner
`saveCert`/`saveKey` label `"create cert file"` shadows it. The tests asserted a behaviour the
code does not have.

I rewrote both. They now assert the `path` context key — which does survive the wrap and is what
an operator actually needs — plus the cert-vs-key `operation` label that is genuinely reported,
plus a stronger claim the originals did not make: **fail-fast semantics**. Each subtest asserts
that the artifacts preceding the blocked one are on disk (proving the sequence really reached the
branch under test) and that the artifacts following it were never written. That matters beyond
tidiness: `EnsureCerts` treats the presence of *any* of `core.crt`/`core.key`/`root-ca.crt` as
"certificates already exist", so a partially-written certs directory boots into a broken trust
posture rather than regenerating.

The other eight inherited functions passed and survived mutation testing. They were kept, and the
two rewritten ones were renamed to describe what they now prove.

## The Plan's Falsified Premise

The plan states the package sits at **76.2%** and gates Task 2 on
`go tool cover -func … >= 80`. Measured at HEAD before writing a line of test code:

| Instrument | Metric | Baseline at HEAD |
|---|---|---|
| `go tool cover -func`, package-scoped profile | statement ratio | **83.9%** |
| local line-ratio proxy over the same profile | line ratio | **80.98%** |
| codecov API (per `09-RESEARCH.md:214`) | line ratio, `.codecov.yml` ignores applied, unit+integration merged | **76.2%** |

The 76.2% figure comes from **codecov**; the plan's gate measures **`go tool cover`**. On the
gate's own instrument the package was already above 80% before any work — **the Task 2 gate could
not fail.** This is the Phase-9 defect class, in the gate the plan wrote to guard against it.

**Deviation applied.** I ran the plan's gate as written (it passes, exit 0) but did not treat it
as evidence. The gate I actually held the work to is a strict increase on a single instrument:

```
before=83.9 after=91.7 strictly_greater=YES   # exit 0
```

That gate fails if the added tests cover nothing. Per-test falsifiability was established
separately by mutation, below.

**What I cannot verify:** codecov's figure is produced by the CI upload pipeline. The local
line-ratio proxy — the closer analogue of codecov's metric — moved **80.98% → 90.23%**, a
+9.25-point gain on top of a baseline that already exceeded 80 on that proxy. Whether the landed
codecov report clears the named floor depends on the upload, which plan 09-01 found is not
guaranteed by a green CI job. D3 in the coverage block is flagged `human_judgment: true` for this
reason. There is also **no per-package codecov floor** — codecov measures patch and project only;
the `internal/tls ≥80%` floor is a project convention enforced by 09-19's gate, not by codecov.

## Task 1 — Classification of the Uncovered Mass

Thirty-five uncovered blocks (37 statements) at baseline, classified before any test was written:

| Production branch | Classification | Reason |
|---|---|---|
| `GenerateCA` malformed SAN URI (`certs.go:63`) | **genuine** | A game ID with a control character cannot form the identity SAN |
| `GenerateServerCert` / `GenerateClientCert` sign failure (`123`, `278`) | **genuine** | A `root-ca.crt`/`root-ca.key` pair from different generations — a real deployment mistake |
| `SaveCertificates` / `SaveClientCert` MkdirAll failure (`139`, `293`) | **genuine** | Certs path under a regular file |
| `SaveCertificates` per-artifact write failures (`147`, `155`, `158`) | **genuine** | Obstructed artifact path; also proves fail-fast |
| `SaveClientCert` per-artifact write failures (`299`, `302`) | **genuine** | Same |
| `saveKey` marshal failure (`230`) and open failure (`235`) | **genuine** | Unmarshallable curve; ordering is observable |
| `ValidateHostname` Common Name fallback (`455`) | **genuine** | Accepting side of the deprecated fallback was never asserted |
| `ValidateExtKeyUsage` match (`470`) | **genuine** | Only the rejecting side was asserted — a check that accepted everything would have passed the whole suite |
| `TLSSubsystem.Prepare` ensurer failure (`subsystem.go:78`) | **genuine** | The plan's named key link; injectable via the existing `CertEnsurer` seam |
| `EnsureCerts` dir/CA/client-save stages (`137`, `142`, `162`) | **genuine** | Each a distinct operator-visible boot failure |
| `crypto/rand` and serial failures (`52`, `57`, `99`, `104`, `256`, `261`) | **unreachable** | Requires a failing `rand.Reader` |
| `x509.CreateCertificate` / `ParseCertificate` on valid templates (`82`, `87`, `128`, `283`) | **unreachable** | Inputs are validated one line earlier |
| `pem.Encode` and `f.Close` failures (`214`, `220`, `239`, `245`) | **unreachable** | Requires a failing writer on an already-open file |
| `EnsureCerts` server-cert / client-cert generation (`147`, `157`) | **unreachable** | CA is freshly generated and valid |
| `EnsureCerts` post-save load (`169`) | **unreachable** | Requires a save that succeeds and a load that fails |

Seventeen blocks remain uncovered, all from the unreachable set. Left uncovered rather than
contorting production code to reach them.

## Task 2 — Tests Added

Exact names, recorded so plan 09-18's naming ratchet does not rename them. No name's final
underscore-delimited segment is a single CamelCase token (none contain underscores).

**`internal/tls/certs_test.go`**

1. `TestGenerateCARejectsGameIDThatCannotFormASANURI`
2. `TestGenerateServerCertFailsWhenCAKeyDoesNotMatchCACertificate`
3. `TestGenerateClientCertFailsWhenCAKeyDoesNotMatchCACertificate`
4. `TestSaveCertificatesFailsWhenCertsDirectoryCannotBeCreated`
5. `TestSaveCertificatesFailsFastAndNamesTheUnwritableArtifactPath` *(rewritten from the inherited, failing `…ReportsWhichArtifactFailedToWrite`)*
6. `TestSaveClientCertFailsFastAndNamesTheUnwritableArtifactPath` *(rewritten, same)*
7. `TestSaveClientCertFailsWhenCertsDirectoryCannotBeCreated`
8. `TestSaveKeyRejectsUnsupportedCurveWithoutCreatingAFile`
9. `TestValidateHostnameFallsBackToCommonNameOnlyForAnExactMatch`
10. `TestValidateExtKeyUsageAcceptsCertificateCarryingTheRequiredUsage`

**`internal/tls/subsystem_test.go`**

11. `TestTLSSubsystemPrepareReportsSetupFailureAndLeavesTheSubsystemUnstarted`
12. `TestTLSSubsystemPrepareSurfacesTheCertificateStageThatFailed`
13. `TestEnsureCertsReportsWhichGenerationStageFailed`

## Falsifiability — Mutation Results

Every load-bearing claim was checked by breaking the production branch and confirming the test
fails. Production was restored and `git diff` confirmed clean after each.

| # | Mutation | Test that must fail | Result |
|---|---|---|---|
| NC1 | `SaveCertificates` swallows a failed CA-cert write (best-effort instead of fail-fast) | `…FailsFastAndNamesTheUnwritableArtifactPath` | fails ✓ |
| NC2 | `ValidateExtKeyUsage` never matches | `…AcceptsCertificateCarryingTheRequiredUsage` | fails ✓ |
| NC3 | `ValidateHostname` drops the Common Name fallback | `…FallsBackToCommonNameOnlyForAnExactMatch` | fails ✓ |
| NC4 | `GenerateCA` ignores the `url.Parse` error | `…RejectsGameIDThatCannotFormASANURI` | fails ✓ |
| NC5 | `saveKey` opens the file before marshalling | `…RejectsUnsupportedCurveWithoutCreatingAFile` | fails ✓ |
| NC6 | `Prepare` stores a config despite the ensurer failing | `…LeavesTheSubsystemUnstarted` | fails ✓ |
| NC7 | `EnsureCerts` collapses all stage codes to one | `…ReportsWhichGenerationStageFailed` | fails ✓ |
| NC8 | `Prepare` returns the bare cause without wrapping | `…ReportsSetupFailureAndLeavesTheSubsystemUnstarted` | fails ✓ |

The read-only-parent subtest additionally asserts its own precondition (probing that `chmod 0500`
genuinely denies creation) and skips rather than passing vacuously under root. A verbose run
confirmed all three subtests RUN and PASS — none skipped.

## Verification

| Check | Result |
|---|---|
| `task test -- ./internal/tls/` | exit 0 — 86 tests, **91.7%** of statements (was 64 tests, 83.9%) |
| Plan's Task 1 gate (baseline profile probe) | exit 0 |
| Plan's Task 2 gate (`>= 80`) | exit 0 — but could not fail; see deviation |
| Strict-increase gate (replacement) | exit 0 — 83.9 → 91.7 |
| `task test` (repo-wide) | exit 0 — 10394 tests, 4 skipped |
| `task lint` | exit 0 |
| Assertion count delta | +51 across the two files, against a required minimum of 26 (2 × 13) |
| New `strings.Contains(err` assertions | 0 before, 0 after |
| Certificate/key fixture files committed | none — `git status --porcelain internal/tls/` showed only Go sources |

## Deviations from Plan

**1. [Rule 3 — Blocking] The plan's Task 2 gate could not fail.**
- **Found during:** Task 1 baseline measurement.
- **Issue:** The plan's 76.2% baseline is a codecov line ratio; its `>= 80` gate reads `go tool cover` statement ratio, which was already 83.9% at HEAD.
- **Fix:** Ran the plan's gate as written for the record, and added a falsifiable strict-increase gate plus per-test mutation controls as the real acceptance evidence. Documented that the codecov floor cannot be confirmed locally.
- **Commit:** documentation-only; no code change.

**2. [Rule 1 — Bug] Two inherited tests asserted a behaviour the code does not have.**
- **Found during:** first compile-and-run of the inherited work.
- **Issue:** `oops` context merges innermost-first, so `SaveCertificates`' `operation` label is shadowed by `saveCert`/`saveKey`'s. Eight subtest failures.
- **Fix:** Rewrote both to assert the surviving `path` key and the genuinely-reported `operation` label, and strengthened them with fail-fast file-presence assertions.
- **Files modified:** `internal/tls/certs_test.go`
- **Commit:** `cb9ce4974`

**3. [Rule 3 — Blocking] gosec G302 on the cleanup chmod.**
- **Fix:** Line-scoped `//nolint:gosec // G302: Need 0700 to clean up directory`, matching the existing precedent at `subsystem_test.go:217`. No config widened.
- **Commit:** `295585e6e`

## Discovered, Not Fixed (Out of Scope)

`TestEnsureCerts_DirectoryCreationFailure` (`subsystem_test.go:191`, pre-existing) **does not test
what its name says.** It builds `badDir = <regular-file>/nested/certs`; `fileExists` on
`<badDir>/core.crt` returns `ENOTDIR`, and `fileExists` treats any non-`IsNotExist` error as
"exists" — so `EnsureCerts` takes the *load-existing* branch, never reaching `xdg.EnsureDir`. The
test passes for the wrong reason, which is why `subsystem.go:137` was still uncovered despite it.
My `TestEnsureCertsReportsWhichGenerationStageFailed` covers the real branch. The misleading test
is not failing, so per the scope boundary I left it alone — it is a candidate for QUAL-03's
test-quality sweep.

## Self-Check: PASSED

- `internal/tls/certs_test.go` — FOUND (modified, committed `cb9ce4974`)
- `internal/tls/subsystem_test.go` — FOUND (modified, committed `295585e6e`)
- `.planning/phases/09-test-quality-code-health-sweep/09-08-SUMMARY.md` — FOUND
- Commit `cb9ce4974` — FOUND
- Commit `295585e6e` — FOUND
