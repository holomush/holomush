<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Test Suite Review Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Review and uplift ~210 unit test files for naming consistency (ACE sentence-style), quality (positive/negative coverage, focused assertions), and deduplication.

**Architecture:** Bottom-up by package, smallest to largest. Each phase produces a single PR with two commit types: mechanical renames (rubber-stamp review) and quality changes (real review). CLAUDE.md is updated first to establish conventions before any test changes land.

**Tech Stack:** Go testing, testify (assert/require/mock), gotestdox (CI validation)

**Spec:** `docs/superpowers/specs/2026-04-04-test-suite-review-design.md`

---

## Per-File Audit Protocol

Every test file in every phase follows this protocol. Tasks below reference it
as "run the audit protocol."

### Step A: Rename Test Functions

Apply the naming convention from the spec:

- **No subtests:** Function name MUST be a sentence following ACE (Action,
  Condition, Expectation) in PascalCase.
  - Before: `TestConfigDir_EnvVar`
  - After: `TestConfigDirUsesXDGEnvVarWhenSet`
- **With subtests:** Parent name is `TestType_Method` or `TestNounVerb`. Subtest
  name strings carry the ACE sentence in lowercase English.
  - Before: `t.Run("success", ...)`
  - After: `t.Run("returns location for valid ID", ...)`
- Underscores SHOULD NOT appear in top-level names except the `TestType_Method`
  pattern for receiver-method tests with subtests.

### Step B: Check Positive/Negative Balance

- Every exported production function MUST have at least one happy-path test and
  one error/edge-case test.
- Table-driven tests MUST include both valid and invalid cases.
- If a function only has positive tests, add a negative case.

### Step C: Remove Weak Tests

- Zero-assertion tests (`_, _ = Foo()` "don't panic" pattern) MUST either gain
  meaningful assertions or be removed.
- Duplicate tests covering the same behavior at the same layer MUST be
  consolidated.

### Step D: Verify Focus

- Each test/subtest tests exactly one behavior.
- If a test name would need "and," split it.

### Step E: Verify Tests Pass

```bash
task test -- ./the/package/...
```

If the package has integration tests:

```bash
task test:int -- ./test/integration/the-domain/...
```

If the package touches grpc, web, telnet, or cmd:

```bash
task test:e2e
```

---

## Task 1: Update CLAUDE.md Testing Section

**Files:**

- Modify: `CLAUDE.md:275-410` (Testing section)

This task adds the naming convention and quality standards to CLAUDE.md so all
subsequent work (and future development) follows the rules.

See the spec for full content of each section:
`docs/superpowers/specs/2026-04-04-test-suite-review-design.md`

- [ ] **Step 1:** Add "Test Naming" section (after "Test Files", before "Table-Driven Tests") —
  ACE framework, good/bad examples table, subtest example, requirements table.

- [ ] **Step 2:** Update "Table-Driven Tests" example — sentence-style subtest names,
  testify assertions instead of `t.Errorf`.

- [ ] **Step 3:** Add "Test Quality" section (after "Assertions", before "Mocking with Mockery") —
  five requirements covering both-path testing, assertion quality, focus, error codes.

- [ ] **Step 4:** Run `task fmt` to verify formatting.

- [ ] **Step 5:** Commit with jj: `docs: add test naming and quality standards to CLAUDE.md`

---

## Task 2: Phase 1 — Infrastructure Packages (Rename Commit)

**Files:**

- Modify: `internal/xdg/xdg_test.go` (198 lines, 19 tests, no subtests)
- Modify: `internal/tls/certs_test.go` (1050 lines, 38 tests, 1 with subtests)
- Modify: `internal/naming/naming_test.go` (74 lines, 3 tests, all with subtests)
- Modify: `internal/observability/server_test.go` (529 lines, 15 tests, no subtests)
- Modify: `internal/idgen/id_test.go` (37 lines, 3 tests, no subtests)
- Modify: `internal/logging/handler_test.go` (132 lines, 7 tests, 1 with subtests)
- Modify: `internal/telemetry/provider_test.go` (42 lines, 2 tests, no subtests)

This task is the **rename-only** commit. No behavior changes, no new tests.

- [ ] **Step 1: Rename xdg_test.go functions**

| Old Name | New Name |
|----------|----------|
| `TestConfigDir_EnvVar` | `TestConfigDirUsesXDGEnvVarWhenSet` |
| `TestConfigDir_Default` | `TestConfigDirFallsBackToHomeDotConfigWhenEnvUnset` |
| `TestDataDir_EnvVar` | `TestDataDirUsesXDGEnvVarWhenSet` |
| `TestDataDir_Default` | `TestDataDirFallsBackToHomeDotLocalShareWhenEnvUnset` |
| `TestStateDir_EnvVar` | `TestStateDirUsesXDGEnvVarWhenSet` |
| `TestStateDir_Default` | `TestStateDirFallsBackToHomeDotLocalStateWhenEnvUnset` |
| `TestRuntimeDir_EnvVar` | `TestRuntimeDirUsesXDGEnvVarWhenSet` |
| `TestRuntimeDir_Fallback` | `TestRuntimeDirFallsBackToStateDirRunWhenEnvUnset` |
| `TestCertsDir` | `TestCertsDirReturnsCertsSubdirOfConfigDir` |
| `TestEnsureDir` | `TestEnsureDirCreatesNestedDirectories` |
| `TestEnsureDir_Permissions` | `TestEnsureDirSetsPermissionsTo0700` |
| `TestEnsureDir_Idempotent` | `TestEnsureDirSucceedsWhenCalledTwice` |
| `TestEnsureDir_Error` | `TestEnsureDirFailsWhenParentIsAFile` |
| `TestHomeDir_Fallback` | `TestHomeDirFallsBackToOsUserHomeDirWhenHOMEUnset` |
| `TestConfigDir_HomeDirError` | `TestConfigDirHandlesHomeDirErrorWhenBothEnvVarsUnset` |
| `TestDataDir_HomeDirError` | `TestDataDirHandlesHomeDirErrorWhenBothEnvVarsUnset` |
| `TestStateDir_HomeDirError` | `TestStateDirHandlesHomeDirErrorWhenBothEnvVarsUnset` |
| `TestRuntimeDir_StateDirError` | `TestRuntimeDirHandlesStateDirErrorWhenAllEnvVarsUnset` |
| `TestCertsDir_ConfigDirError` | `TestCertsDirHandlesConfigDirErrorWhenBothEnvVarsUnset` |

- [ ] **Step 2: Rename tls/certs_test.go functions**

| Old Name | New Name |
|----------|----------|
| `TestGenerateCA` | `TestGenerateCAReturnsValidSelfSignedCertificate` |
| `TestGenerateServerCert` | `TestGenerateServerCertReturnsValidCertSignedByCA` |
| `TestSaveAndLoadCertificates` | `TestSaveAndLoadCertificatesRoundTripsCorrectly` |
| `TestLoadCA_MissingFiles` | `TestLoadCAFailsWhenFilesAreMissing` |
| `TestSaveCertificates_OnlyCA` | `TestSaveCertificatesWritesOnlyCAWhenServerCertIsNil` |
| `TestGameIDExtraction` | `TestGameIDExtractionParsesURIFromCertificate` |
| `TestGenerateCA_URIFormat` | `TestGenerateCAIncludesGameIDAsURISAN` |
| `TestGenerateClientCert` | `TestGenerateClientCertReturnsValidCertSignedByCA` |
| `TestSaveClientCert` | `TestSaveClientCertWritesPEMFilesToDisk` |
| `TestLoadServerTLS` | `TestLoadServerTLSReturnsConfigWithMutualAuth` |
| `TestLoadClientTLS` | `TestLoadClientTLSReturnsConfigWithClientCert` |
| `TestLoadServerTLS_MissingFiles` | `TestLoadServerTLSFailsWhenFilesAreMissing` |
| `TestLoadClientTLS_MissingFiles` | `TestLoadClientTLSFailsWhenFilesAreMissing` |
| `TestCertificateNearExpiration` | `TestCheckCertificateExpirationReturnsTrueWhenNearExpiry` |
| `TestCertificateExpired` | `TestCheckCertificateExpirationReturnsTrueWhenExpired` |
| `TestCertificateValid` | `TestCheckCertificateExpirationReturnsFalseWhenValid` |
| `TestCertificateRotation` | `TestCertificateRotationGeneratesNewCertsWhenExpired` |
| `TestSelfSignedCertWithoutCA` | `TestSelfSignedCertFailsVerificationWithoutCA` |
| `TestWrongHostnameInCertificate` | `TestWrongHostnameInCertificateFailsValidation` |
| `TestMismatchedKeyAndCertPair` | `TestMismatchedKeyAndCertPairFailsToLoad` |
| `TestValidateCertificateChain_ValidChain` | `TestValidateCertificateChainSucceedsForValidChain` |
| `TestLoadCertificate_InvalidPEM` | `TestLoadCertificateFailsForInvalidPEM` |
| `TestClientCertForServerAuth` | `TestClientCertFailsWhenUsedForServerAuth` |
| `TestServerCertForClientAuth` | `TestServerCertFailsWhenUsedForClientAuth` |
| `TestLoadCA_InvalidCertPEM` | `TestLoadCAFailsForInvalidCertPEM` |
| `TestLoadCA_InvalidKeyPEM` | `TestLoadCAFailsForInvalidKeyPEM` |
| `TestLoadCA_InvalidCertificateData` | `TestLoadCAFailsForInvalidCertificateData` |
| `TestLoadCA_InvalidKeyData` | `TestLoadCAFailsForInvalidKeyData` |
| `TestLoadServerTLS_InvalidCAPEM` | `TestLoadServerTLSFailsForInvalidCAPEM` |
| `TestLoadClientTLS_InvalidCAPEM` | `TestLoadClientTLSFailsForInvalidCAPEM` |
| `TestLoadServerTLS_MissingCAFile` | `TestLoadServerTLSFailsWhenCAFileIsMissing` |
| `TestLoadClientTLS_MissingCAFile` | `TestLoadClientTLSFailsWhenCAFileIsMissing` |
| `TestValidateCertificateChain_NilCert` | `TestValidateCertificateChainFailsForNilCert` |
| `TestValidateCertificateChain_NilCA` | `TestValidateCertificateChainFailsForNilCA` |
| `TestValidateHostname_NilCert` | `TestValidateHostnameFailsForNilCert` |
| `TestValidateHostname_IPAddress` | `TestValidateHostnameSucceedsForIPAddress` |
| `TestValidateExtKeyUsage_NilCert` | `TestValidateExtKeyUsageFailsForNilCert` |
| `TestCheckCertificateExpiration_NotYetValid` | `TestCheckCertificateExpirationReturnsTrueWhenNotYetValid` |

- [ ] **Step 3: Rename naming/naming_test.go functions**

The naming package tests already use subtests, so only the parent names need
review:

| Old Name | New Name | Notes |
|----------|----------|-------|
| `TestTheme_Name` | `TestTheme_Name` | Keep — `Type_Method` with subtests |
| `TestTheme_Generate` | `TestTheme_Generate` | Keep — `Type_Method` with subtests |
| `TestTheme_ImplementsInterface` | `TestTheme_ImplementsInterface` | Keep — `Type_Method` with subtests |

Review subtest names within each function. Update any that are vague (e.g.,
`"success"`) to sentence style.

- [ ] **Step 4: Rename observability/server_test.go functions**

| Old Name | New Name |
|----------|----------|
| `TestServer_Metrics` | `TestServerMetricsEndpointReturnsPrometheusOutput` |
| `TestServer_LivenessReturns200` | `TestServerLivenessReturns200` |
| `TestServer_ReadinessWhenReady` | `TestServerReadinessReturns200WhenReady` |
| `TestServer_ReadinessWhenNotReady` | `TestServerReadinessReturns503WhenNotReady` |
| `TestServer_ReadinessWithNilChecker` | `TestServerReadinessReturns200WhenCheckerIsNil` |
| `TestServer_DoubleStartFails` | `TestServerStartFailsWhenAlreadyRunning` |
| `TestServer_StopIdempotent` | `TestServerStopSucceedsWhenCalledTwice` |
| `TestServer_ErrorChannelReportsServeErrors` | `TestServerErrorChannelReportsServeErrors` |
| `TestServer_ErrorChannelClosesOnNormalShutdown` | `TestServerErrorChannelClosesOnNormalShutdown` |
| `TestServer_ConcurrentStopCalls` | `TestServerStopIsSafeConcurrently` |
| `TestServer_StopContextTimeout` | `TestServerStopReturnsErrorWhenContextExpires` |
| `TestServer_StopContextTimeoutRestoresState` | `TestServerStopRestoresStateWhenContextExpires` |
| `TestServer_MetricsIncrement` | `TestServerMetricsIncrementUpdatesCounters` |
| `TestRecordEngineFailure` | `TestRecordEngineFailureIncrementsCounter` |
| `TestRecordCircuitBreakerTrip` | `TestRecordCircuitBreakerTripIncrementsCounter` |

- [ ] **Step 5: Rename idgen/id_test.go functions**

| Old Name | New Name |
|----------|----------|
| `TestNew_ReturnsValidULID` | `TestNewReturnsValidULID` |
| `TestNew_ReturnsUniqueValues` | `TestNewReturnsUniqueValuesAcrossCalls` |
| `TestNew_HasNonDecreasingTimestamps` | `TestNewReturnsNonDecreasingTimestamps` |

- [ ] **Step 6: Rename logging/handler_test.go functions**

| Old Name | New Name |
|----------|----------|
| `TestSetup_JSONFormat` | `TestSetupReturnsJSONHandlerWhenFormatIsJSON` |
| `TestSetup_TextFormat` | `TestSetupReturnsTextHandlerWhenFormatIsText` |
| `TestHandler_TraceContext` | `TestHandlerIncludesTraceFieldsWhenSpanContextPresent` |
| `TestHandler_NoTraceContext` | `TestHandlerOmitsTraceFieldsWhenSpanContextAbsent` |
| `TestSetup_DefaultFormat` | `TestSetupDefaultsToJSONFormat` |
| `TestSetDefault` | `TestSetDefaultSetsGlobalLogger` |
| `TestSetup_LevelFiltering` | `TestSetup_LevelFiltering` |

Review subtest names in `TestSetup_LevelFiltering` for sentence style.

- [ ] **Step 7: Rename telemetry/provider_test.go functions**

| Old Name | New Name |
|----------|----------|
| `TestInit_NoEndpoint` | `TestInitReturnsNoopProviderWhenEndpointIsEmpty` |
| `TestInit_WithEndpoint` | `TestInitReturnsOTLPProviderWhenEndpointIsSet` |

- [ ] **Step 8: Run tests for all Phase 1 packages**

```bash
task test -- ./internal/xdg/ ./internal/tls/ ./internal/naming/ \
  ./internal/observability/ ./internal/idgen/ ./internal/logging/ \
  ./internal/telemetry/
```

Expected: all tests pass (renames are behavior-preserving).

- [ ] **Step 9: Commit rename-only changes**

```bash
jj --no-pager describe -m "test(infra): rename test functions to sentence style

Apply ACE naming convention to infrastructure packages: xdg, tls,
naming, observability, idgen, logging, telemetry. Mechanical renames
only — no behavior changes.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Phase 1 — Infrastructure Packages (Quality Commit)

**Files:** Same 7 test files as Task 2.

This task addresses quality issues found during the inventory.

- [ ] **Step 1: Fix zero-assertion tests in xdg_test.go**

Five tests in xdg_test.go call functions with no assertions:

- `TestConfigDirHandlesHomeDirErrorWhenBothEnvVarsUnset`
- `TestDataDirHandlesHomeDirErrorWhenBothEnvVarsUnset`
- `TestStateDirHandlesHomeDirErrorWhenBothEnvVarsUnset`
- `TestRuntimeDirHandlesStateDirErrorWhenAllEnvVarsUnset`
- `TestCertsDirHandlesConfigDirErrorWhenBothEnvVarsUnset`

For each, add assertions that the function returns an error (not just doesn't
panic):

```go
func TestConfigDirReturnsErrorWhenBothEnvVarsUnset(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	// On macOS, os.UserHomeDir() may still work via cgo.
	// We verify the function doesn't panic and returns a valid result.
	dir, err := ConfigDir()
	if err != nil {
		assert.Empty(t, dir, "ConfigDir() returned non-empty string with error")
	} else {
		assert.NotEmpty(t, dir, "ConfigDir() returned empty string without error")
	}
}
```

Apply this pattern to all five. Also rename them (the earlier rename used
"Handles" which is vague — replace with "ReturnsErrorOrValidPath"):

- `TestConfigDirReturnsErrorOrValidPathWhenBothEnvVarsUnset`
- `TestDataDirReturnsErrorOrValidPathWhenBothEnvVarsUnset`
- `TestStateDirReturnsErrorOrValidPathWhenBothEnvVarsUnset`
- `TestRuntimeDirReturnsErrorOrValidPathWhenAllEnvVarsUnset`
- `TestCertsDirReturnsErrorOrValidPathWhenBothEnvVarsUnset`

- [ ] **Step 2: Audit tls/certs_test.go for quality gaps**

Read the full file. Check:

- Are there tests for loading certificates from valid PEM but expired certs?
- Are validation functions tested with boundary conditions (cert expiring in
  exactly 30 days)?
- Do the subtest names in `TestWrongHostnameInCertificate` follow sentence
  style?

Fix any issues found. This step requires reading the file during execution.

- [ ] **Step 3: Audit observability/server_test.go for quality gaps**

Read the full file. Check:

- `TestServerMetricsEndpointReturnsPrometheusOutput` — does it assert on
  specific metric names or just HTTP 200?
- Is there a test for starting the server with an invalid address?
- Are `TestRecordEngineFailure` and `TestRecordCircuitBreakerTrip` testing
  the same pattern? If so, consolidate into a table-driven test.

Fix any issues found.

- [ ] **Step 4: Audit remaining Phase 1 files**

For idgen, logging, telemetry, naming — read each file and apply steps B-D
of the audit protocol. These are small files (37-132 lines) so issues will be
minor.

- [ ] **Step 5: Run all Phase 1 tests**

```bash
task test -- ./internal/xdg/ ./internal/tls/ ./internal/naming/ \
  ./internal/observability/ ./internal/idgen/ ./internal/logging/ \
  ./internal/telemetry/
```

- [ ] **Step 6: Commit quality changes**

```bash
jj --no-pager describe -m "test(infra): fix quality gaps in infrastructure test suites

Replace zero-assertion tests with meaningful checks, consolidate
duplicate test patterns, and add missing negative test cases.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 7: Run pr-prep**

```bash
task pr-prep
```

Must pass with zero failures before opening PR.

- [ ] **Step 8: Open PR for Phase 1**

Push to remote and open PR targeting main. PR title:
`test(infra): sentence-style naming and quality uplift for infrastructure packages`

---

## Task 4: Phase 2 — Core, Store, Config, Control (Rename Commit)

**Files:**

- Modify: `internal/core/character_test.go` (20 lines, 1 test)
- Modify: `internal/core/command_test.go` (36 lines, 1 test with subtests)
- Modify: `internal/core/engine_test.go` (229 lines, 8 tests)
- Modify: `internal/core/event_test.go` (99 lines, 4 tests, 2 with subtests)
- Modify: `internal/core/registry_test.go` (136 lines, 7 tests, 1 with subtests)
- Modify: `internal/core/store_memory_test.go` (238 lines, 9 tests)
- Modify: `internal/core/ulid_test.go` (34 lines, 3 tests)
- Modify: `internal/store/alias_test.go` (579 lines, 10 tests, mostly subtests)
- Modify: `internal/store/migrate_embed_test.go` (53 lines, 1 test)
- Modify: `internal/store/migrate_test.go` (~742 lines, 30+ tests)
- Modify: `internal/store/player_session_store_test.go` (487 lines, 8+ tests)
- Modify: `internal/store/postgres_test.go` (~939 lines, 16+ tests)
- Modify: `internal/store/session_store_test.go` (~1145 lines, 18+ tests)
- Modify: `internal/config/config_test.go` (213 lines, 12 tests)
- Modify: `internal/control/grpc_server_test.go` (1033 lines, 31+ tests)

Do NOT modify integration test files (`*_integration_test.go`, Ginkgo suites)
— those are out of scope per the spec.

- [ ] **Step 1: Read each test file and build rename table**

For each file, read the test function names and apply the naming rules. Build
a rename table like Task 2. Key patterns to watch for:

- `internal/core/engine_test.go`: Functions like `TestEngine_HandleSay` and
  `TestEngine_HandleSay_AppendsToStore` — these don't use subtests, so rename
  to sentences: `TestEngineHandleSayPublishesEventToSubscribers`,
  `TestEngineHandleSayAppendsEventToStore`, etc.
- `internal/core/store_memory_test.go`: Functions like
  `TestMemoryEventStore_Replay_AfterIDNotFound` — rename to
  `TestMemoryEventStoreReplayReturnsEmptyWhenAfterIDNotFound`.
- `internal/config/config_test.go`: Functions like
  `TestLoad_FromYAMLFile` — rename to `TestLoadParsesYAMLConfigFile`.
- `internal/store/` files with subtests: Keep `TestType_Method` parents,
  review subtest names for sentence style.

- [ ] **Step 2: Apply renames**

Edit each file, changing only function names and subtest name strings.

- [ ] **Step 3: Run tests**

```bash
task test -- ./internal/core/ ./internal/store/ ./internal/config/ \
  ./internal/control/
```

- [ ] **Step 4: Commit rename-only changes**

```bash
jj --no-pager describe -m "test(core): rename test functions to sentence style

Apply ACE naming convention to core, store, config, and control
packages. Mechanical renames only.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Phase 2 — Core, Store, Config, Control (Quality Commit)

**Files:** Same as Task 4.

- [ ] **Step 1: Audit core/engine_test.go**

Check: Are there tests for engine handling events when the store returns an
error on subscribe? Is concurrent access tested? Fix gaps.

- [ ] **Step 2: Audit store/ test files**

Key questions:

- Do `postgres_test.go` and `session_store_test.go` duplicate validation logic
  that belongs in the service layer?
- Are mock row/scan errors consistently tested across all store test files?
- Do the alias tests cover the full CRUD cycle including edge cases?

- [ ] **Step 3: Audit config/config_test.go**

Check: Is there a test for loading config when the file has invalid
permissions? Is there a test for environment variable overrides (not just CLI
flags)?

- [ ] **Step 4: Audit control/grpc_server_test.go**

Check: With 31+ tests in one file, are any redundant? Are concurrent operation
tests focused on distinct behaviors?

- [ ] **Step 5: Run tests including integration**

```bash
task test -- ./internal/core/ ./internal/store/ ./internal/config/ \
  ./internal/control/
task test:int
```

- [ ] **Step 6: Commit quality changes**

- [ ] **Step 7: Run pr-prep and open PR**

```bash
task pr-prep
```

---

## Task 6: Phase 3 — Auth Packages

**Files:**

- Modify: `internal/auth/hasher_test.go`
- Modify: `internal/auth/ratelimit_test.go`
- Modify: `internal/auth/character_service_test.go`
- Modify: `internal/auth/reset_test.go`
- Modify: All other `internal/auth/*_test.go` files (~13 total)
- Modify: `internal/auth/postgres/player_repo_test.go`
- Modify: `internal/auth/postgres/reset_repo_test.go`
- Modify: `internal/auth/postgres/postgres_test.go`

- [ ] **Step 1: Read all auth test files, build rename tables**

The hasher tests (`TestHashPassword`, `TestVerifyPassword`) already use good
subtest names — these parents are fine. Focus on files without subtests.

- [ ] **Step 2: Apply renames (rename-only commit)**
- [ ] **Step 3: Audit quality — positive/negative balance**

Key areas:

- `character_service_test.go`: Does it test character creation failure due to
  name collision? Due to player not found?
- `reset_test.go`: Are token expiry and token reuse tested?
- `ratelimit_test.go`: Is the bypass-for-admin path tested?

- [ ] **Step 4: Fix quality gaps (quality commit)**
- [ ] **Step 5: Run tests**

```bash
task test -- ./internal/auth/...
```

- [ ] **Step 6: Run pr-prep and open PR**

---

## Task 7: Phase 4 — Command Packages

**Files:**

- Modify: `internal/command/alias_test.go`
- Modify: `internal/command/metrics_test.go`
- Modify: `internal/command/parser_test.go`
- Modify: `internal/command/ratelimit_test.go`
- Modify: `internal/command/resolve_test.go`
- Modify: `internal/command/validation_test.go`
- Modify: `internal/command/middleware_test.go`
- Modify: All other `internal/command/*_test.go` files (~14 total)
- Modify: `internal/command/handlers/*_test.go` (5 files)

- [ ] **Step 1: Read all command test files, build rename tables**
- [ ] **Step 2: Apply renames (rename-only commit)**
- [ ] **Step 3: Audit quality**

Key areas:

- `parser_test.go`: Does it share test cases with `validation_test.go`? If so,
  deduplicate.
- `middleware_test.go`: Are rate limit bypass scenarios tested?
- Handler tests: Does each handler test both successful execution and
  authorization failure?

- [ ] **Step 4: Fix quality gaps (quality commit)**
- [ ] **Step 5: Run tests**

```bash
task test -- ./internal/command/...
```

- [ ] **Step 6: Run pr-prep and open PR**

---

## Task 8: Phase 5 — Plugin Packages

**Files:**

- Modify: All `internal/plugin/*_test.go` (~16 files)
- Modify: `internal/plugin/goplugin/*_test.go` (2 files)
- Modify: `internal/plugin/hostfunc/*_test.go` (11 files)
- Modify: `internal/plugin/lua/*_test.go` (3 files)

- [ ] **Step 1: Read all plugin test files, build rename tables**
- [ ] **Step 2: Apply renames (rename-only commit)**
- [ ] **Step 3: Audit quality**

Key areas:

- `hostfunc/` has 11 test files — check for duplication between
  `functions_internal_test.go` and `stdlib_internal_test.go`.
- `lua/state_test.go` vs `lua/state_internal_test.go` — do they overlap?
- Are plugin loading failure modes tested (invalid manifest, missing entry
  point)?

- [ ] **Step 4: Fix quality gaps (quality commit)**
- [ ] **Step 5: Run tests**

```bash
task test -- ./internal/plugin/...
```

- [ ] **Step 6: Run pr-prep and open PR**

Split into 2 PRs if the diff exceeds 1000 lines.

---

## Task 9: Phase 6 — World Packages

**Files:**

- Modify: All `internal/world/*_test.go` (~14 files)
- Modify: All `internal/world/postgres/*_test.go` (~11 files)

- [ ] **Step 1: Read all world test files, build rename tables**
- [ ] **Step 2: Apply renames (rename-only commit)**
- [ ] **Step 3: Audit quality and dedup (Category 2)**

This is where unit-vs-integration deduplication is most likely. Key questions:

- Does `internal/world/location_test.go` test validation that also appears in
  `internal/world/postgres/location_repo_test.go`?
- Does `internal/world/scene_test.go` duplicate `postgres/scene_repo_test.go`?

For each overlap: keep the test where the validation code lives, remove the
other.

- [ ] **Step 4: Fix quality gaps and remove duplicates (quality commit)**
- [ ] **Step 5: Run tests including integration**

```bash
task test -- ./internal/world/...
task test:int -- ./test/integration/world/...
```

- [ ] **Step 6: Run pr-prep and open PR**

---

## Task 10: Phase 7 — Access/Policy Packages

**Files:**

- Modify: `internal/access/*_test.go` (5 files)
- Modify: `internal/access/setup/*_test.go` (1 file)
- Modify: `internal/access/policy/*_test.go` (~12 files)
- Modify: `internal/access/policy/attribute/*_test.go` (14 files)
- Modify: `internal/access/policy/audit/*_test.go` (3 files)
- Modify: `internal/access/policy/dsl/*_test.go` (8 files)
- Modify: `internal/access/policy/policytest/*_test.go` (1 file)
- Modify: `internal/access/policy/store/*_test.go` (2 files)
- Modify: `internal/access/policy/types/*_test.go` (1 file)

This is the largest phase (47 files). Split into 2-3 PRs.

- [ ] **Step 1: PR 1 — access/ and access/policy/ (non-attribute)**

Read and rename all test files in `access/`, `access/setup/`,
`access/policy/` (top-level), `access/policy/audit/`, `access/policy/dsl/`,
`access/policy/policytest/`, `access/policy/store/`, `access/policy/types/`.

- [ ] **Step 2: Audit quality for PR 1 files**

Key areas:

- `dsl/parser_test.go` vs `dsl/validate_test.go` — deduplicate shared cases.
- `dsl/ast_spike_test.go` — is this still needed or was it a one-off
  experiment? If experimental, remove.
- `policy/engine_contract_test.go` — does this overlap with
  `policy/compiler_test.go`?

- [ ] **Step 3: Commit, run tests, run pr-prep, open PR 1**

```bash
task test -- ./internal/access/...
```

- [ ] **Step 4: PR 2 — attribute/ provider tests (dedup: Category 1)**

Create shared provider contract test helper:

```go
// File: internal/access/policy/attribute/contract_test.go

func assertProviderContract(t *testing.T, provider AttributeProvider) {
	t.Helper()

	t.Run("namespace is non-empty", func(t *testing.T) {
		assert.NotEmpty(t, provider.Namespace())
	})

	t.Run("schema returns non-nil definitions", func(t *testing.T) {
		schema := provider.Schema()
		assert.NotNil(t, schema)
	})

	t.Run("resolve subject returns nil for non-matching ID", func(t *testing.T) {
		attrs, err := provider.ResolveSubject(context.Background(), "unrelated:id")
		require.NoError(t, err)
		assert.Nil(t, attrs)
	})

	t.Run("resolve resource returns nil for non-matching ID", func(t *testing.T) {
		attrs, err := provider.ResolveResource(context.Background(), "unrelated:id")
		require.NoError(t, err)
		assert.Nil(t, attrs)
	})
}
```

Then update each provider test file:

1. Remove the boilerplate `Namespace`, `ResolveSubject` (nil return), and
   non-matching resource tests.
2. Add `assertProviderContract(t, NewXxxProvider())` call.
3. Keep only provider-specific test cases.

- [ ] **Step 5: Rename attribute test functions to sentence style**
- [ ] **Step 6: Commit, run tests, run pr-prep, open PR 2**

---

## Task 11: Phase 8 — Cross-Cutting Deduplication

**Files:** Varies based on findings from earlier phases.

- [ ] **Step 1: Review findings from Phases 2-7**

During earlier phases, issues were flagged but deferred to this pass. Collect
the list of:

- Unit tests that duplicate integration test coverage
- Tests that belong in a different package
- Shared helpers that should be extracted

- [ ] **Step 2: Address each finding**

For each:

- If a unit test duplicates an integration test, remove the unit test (the
  integration test is more authoritative).
- If a helper is duplicated across packages, extract to a shared location.

- [ ] **Step 3: Run full test suite**

```bash
task test
task test:int
task test:e2e
```

- [ ] **Step 4: Run pr-prep and open PR**

---

## Task 12: Phase 9 — gotestdox CI Check

**Files:**

- Modify: `.github/workflows/ci.yml` (or equivalent CI config)
- Modify: `Taskfile.yml` (add gotestdox task)

- [ ] **Step 1: Add gotestdox to project tools**

```bash
go install github.com/bitfield/gotestdox/cmd/gotestdox@latest
```

Add to Taskfile:

```yaml
  test:dox:
    desc: Validate test names read as sentences
    cmds:
      - go test -v ./internal/... ./pkg/... ./cmd/... 2>&1 | gotestdox
```

- [ ] **Step 2: Add CI step**

Add a step to the CI workflow that runs `gotestdox` and fails if any test name
doesn't convert to a readable sentence. The exact CI config depends on the
current workflow structure — read `.github/workflows/` to determine where to
add.

- [ ] **Step 3: Run locally to verify**

```bash
task test:dox
```

- [ ] **Step 4: Run pr-prep and open PR**

---

## Task 13: Update cmd/holomush Test Files

**Files:**

- Modify: `cmd/holomush/auth_adapters_test.go` (65 lines, 5 tests)
- Modify: `cmd/holomush/automigrate_test.go` (~428 lines)
- Modify: `cmd/holomush/cert_poll_test.go` (208 lines, 8 tests)
- Modify: `cmd/holomush/core_test.go` (~1031 lines)
- Modify: `cmd/holomush/deps_test.go` (~1302 lines)
- Modify: `cmd/holomush/gateway_test.go` (~845 lines)
- Modify: `cmd/holomush/main_test.go` (241 lines, 14 tests)
- Modify: `cmd/holomush/migrate_test.go` (~742 lines)
- Modify: `cmd/holomush/status_test.go` (~569 lines)

Do NOT modify `automigrate_integration_test.go` (integration test, out of
scope).

This task can run in parallel with Phase 2 or be folded into Phase 2's PR
since cmd/holomush tests depend on core/store packages.

- [ ] **Step 1: Read all cmd test files, build rename tables**

Key patterns:

- `TestAuthCharRepoAdapter_ImplementsInterface` — rename to
  `TestAuthCharRepoAdapterImplementsCharacterRepository`
- `TestGatewayCommand_Flags` — rename to `TestGatewayCommandRegistersExpectedFlags`
- `TestWaitForTLSCerts_ImmediateSuccess` — rename to
  `TestWaitForTLSCertsSucceedsWhenCertsExist`

- [ ] **Step 2: Apply renames (rename-only commit)**
- [ ] **Step 3: Audit quality**

Key areas:

- `deps_test.go` at 1302 lines is the largest test file. Check for:
  - Tests that could be consolidated into table-driven subtests
  - Duplicate setup patterns that should be helpers
- `core_test.go` and `gateway_test.go` — similar structure, check for overlap

- [ ] **Step 4: Fix quality gaps (quality commit)**
- [ ] **Step 5: Run tests including E2E**

```bash
task test -- ./cmd/holomush/...
task test:e2e
```

- [ ] **Step 6: Run pr-prep and open PR**

---

## Task 14: Phase 1 — Additional Packages

**Files:**

- Modify: `internal/property/*_test.go` (3 files)
- Modify: `internal/content/*_test.go` (4 files)
- Modify: `internal/session/*_test.go` (3 files)
- Modify: `internal/telnet/*_test.go` (2 files)
- Modify: `internal/web/*_test.go` (8 files)
- Modify: `internal/grpc/*_test.go` (7 files)
- Modify: `internal/bootstrap/*_test.go` (8 files)
- Modify: `pkg/errutil/testing_test.go`
- Modify: `pkg/holo/*_test.go` (4 files)
- Modify: `pkg/plugin/event_test.go`
- Modify: `pkg/proto/holomush/plugin/v1/*_test.go` (2 files)

These packages weren't explicitly assigned to a phase in the spec. Group them
logically:

- **PR A:** property, content, session (10 files — domain support packages)
- **PR B:** telnet, web, grpc (17 files — protocol layer)
- **PR C:** bootstrap, pkg/ (15 files — foundation)

For each PR, follow the standard protocol: rename commit, quality commit,
pr-prep, open PR.

- [ ] **Step 1: PR A — property, content, session**
- [ ] **Step 2: PR B — telnet, web, grpc (run test:e2e after)**
- [ ] **Step 3: PR C — bootstrap, pkg/**

Key quality issue in bootstrap: `admin_bootstrap_test.go` vs `admin_test.go`
overlap (Category 3 dedup). Consolidate during this step.
