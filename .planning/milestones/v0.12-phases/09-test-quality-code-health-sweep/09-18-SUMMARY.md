---
phase: 09-test-quality-code-health-sweep
plan: 18
subsystem: test-quality
tags: [ace-naming, test-hygiene, meta-test, ratchet, QUAL-03]
status: complete
requires:
  - "09-03, 09-05, 09-06, 09-08, 09-09, 09-10, 09-11, 09-16 (every plan in this phase that touches a test file)"
provides:
  - "test/meta/ace_naming_registry_test.go — the ACE naming + placeholder-label ratchet"
  - "138 renamed test functions; 13 rewritten subtest/table-case labels"
affects:
  - "71 test files across cmd/, internal/, pkg/, plugins/, test/meta/"
tech-stack:
  added: []
  patterns:
    - "go/ast declaration walk in test/meta, reusing findRepoRoot + skipDirs from meta_helpers_test.go"
    - "slice-literal discriminator to separate table-driven cases from domain struct fixtures"
    - "non-vacuous-corpus assertions (>500 files, >1000 decls, >500 labels) so an empty walk cannot pass as clean"
key-files:
  created:
    - test/meta/ace_naming_registry_test.go
  modified:
    - "70 *_test.go files (see the mapping below)"
decisions:
  - "Tightened predicate = underscore-form AND no subtests AND single-token CamelCase tail; reproduced 09-RESEARCH exactly (1572/466/1106)"
  - "Exactly two carve-outs, neither widened: TestINV_ prefix (32 decls) and the two pinned TestPrivacy_ names"
  - "Table-case label detection restricted to slice-literal elements — 45 hits unrestricted vs 13 real"
  - "Ratchet is a meta-test, not a lint plugin; no invariant-registry entry, no // Verifies: annotation"
metrics:
  duration: ~150min
  completed: 2026-07-26
  tasks: 3
  files: 71
  renames: 138
  labels_fixed: 13
---

# Phase 09 Plan 18: ACE Test-Naming Sweep & Ratchet Summary

Renamed 138 test functions whose names ended in a bare topic rather than an
expectation, rewrote 13 placeholder subtest labels, and locked both with a
self-excluding `go/ast` meta-test ratchet proven to fail on each violation class.

## Task 1 — the hit set (analysis only, no repo files changed)

The predicate was implemented as a throwaway `go/ast` walker: top-level
`func TestX(t *testing.T)` declarations, split on `_`, flagging a name whose
final segment tokenises to a single CamelCase word **and** whose body declares
no `X.Run(...)` subtests (the sanctioned `TestType_Method` exception).

**Calibration reproduced 09-RESEARCH to the unit**, which is the strongest
evidence the predicate implemented is the settled one rather than a lookalike:

| Measure | 09-RESEARCH | This run | |
|---|---:|---:|---|
| underscore-form total | 1572 | 1572 | ✓ |
| …with subtests (sanctioned exception) | 466 | 466 | ✓ |
| …without subtests (literal D-07) | 1106 | 1106 | ✓ |
| …of which single-token tail | 114 | 116 | +2 (churn) |
| `TestINV_` declarations | 25 (stale) | **32** | plan predicted 32 |

### Mandated classification checks

| Name | Tail tokenisation | Verdict |
|---|---|---|
| `TestGatewayCommand_SecureCookiesFlag` | `SecureCookiesFlag` → `Secure`/`Cookies`/`Flag` = **3** | **NOT a hit** |
| `TestGatewayCommandSecureCookiesExplicitFalseDisablesTheSecureAttributeSet` (09-04) | no underscore | NOT a hit |
| `TestGatewayConfigLoadDefaultsSecureCookiesOnWhenNeitherFlagNorConfigKeyIsSet` (09-04) | no underscore | NOT a hit |
| `TestGatewayConfigLoadHonoursSecureCookiesFalseFromTheConfigFile` (09-04) | no underscore | NOT a hit |

`TestGatewayCommand_SecureCookiesFlag` classifying **not a hit** is the decisive
check: under the literal D-07 form it would be one (it declares no subtests), and
the count would have been ~1106 rather than ~116.

**Architecture-review cleared list:** all five files (`emit_intent_parity_test.go`,
`scopecheck_test.go`, `hostcap/session_test.go`, `hostcap/world_test.go`,
`auth/auth_service_test.go`) absent from the hit set. ✓

**Phase-9-added names:** the 36 test functions this branch adds versus
`origin/main` were intersected with the hit set — empty. No earlier plan in this
phase violated its own naming criterion.

### Carve-outs — exactly two, neither widened

1. **`TestINV_` prefix — 32 declarations.** Their underscores encode a registry
   identifier that ~100 files reference via `// Verifies:`.
   **Honest note: this carve-out is currently inert.** No `TestINV_` name reaches
   the predicate. Two (`TestINV_P4_Coverage_Meta`, `TestINV_P5_Coverage_Meta`) do
   have single-token tails (`Meta`) but declare subtests at lines 170/171 and are
   excluded by the sanctioned exception first. An initial `rg -A25` window
   reported them as subtest-free — the same too-small-window false positive 09-09
   recorded; the AST is authoritative.
2. **Two pinned `TestPrivacy_*` names.** Realised as Ginkgo `Describe` containers
   (`test/integration/session/lifecycle_privacy_floor_test.go:621,724`), so the
   declaration walk never reaches them. Kept in the ratchet so converting either
   to a plain Go test cannot silently make it a violation. Both verified intact.

## Task 2 — the sweep

**138 renames** (116 from Task 1 + 22 from the Task 3 deviation below) and
**13 labels**. Every replacement was written by reading what the test asserts.

### Counted proof no verification was removed

| Measure | Before | After |
|---|---:|---:|
| Test function declarations (repo-wide, AST) | 6328 | **6328** |
| `t.Skip(` lines | 22 | **22** |
| Ginkgo `Skip(` lines | 25 | **25** |
| Assertions across the 55 touched files | 3154 | **3154** |
| `TestINV_` declarations | 32 | **32** |
| Task-2 diff | — | **165 insertions / 165 deletions** |
| `internal/web` test funcs / assertions | 244 / 1029 | **244 / 1029** |

### Executable-reference scope

Searched across `*.go`, `*.sh`, `*.yaml`, `*.yml`, `Taskfile*`, `*.bats`,
excluding `.planning/` (which deliberately records every old name so the rename
stays auditable). **Exactly one executable reference existed:**
`internal/test/invariants/inv_p5_coverage_meta_test.go:140` names
`TestReconnect_VsConcurrentLeave_Serializes` in a string literal and `t.Fatalf`s
when the named test is absent — updated with the declaration.

Post-sweep the check is clean, verified **with word boundaries** and a positive
control (`TestStyledText_AppendText`, a genuinely surviving name, is still found —
so the empty result is meaningful, not a broken query). A first pass without `-w`
appeared to show survivors; all were substrings of longer, different tests
(`TestStyledText_Append_NoSharedState` contains `TestStyledText_Append`). The
renamer used `\bOLD\b` and was correct throughout.

### The six identical labels

`internal/store/player_session_store_test.go` carried `"happy path"` six times
across six different operations; each became a different sentence:

| Line | Operation | New label |
|---|---|---|
| 66 | `Create` | `inserts the session row` |
| 137 | `GetByTokenHash` | `returns the session matching the token hash` |
| 278 | `Delete` | `deletes the session row by id` |
| 333 | `DeleteByPlayer` | `deletes every session belonging to the player` |
| 449 | `GetByID` | `returns the session matching the id` |
| 768 | `RefreshTTL` | `extends expires_at for the session` |

Remaining seven: `automigrate_test.go` `"success"` → `runs Up then closes the
migrator`; `cmd_admin_totp_run_test.go` ×2 `"happy path"` → `prepares, prints,
then commits the {bootstrap ,}enrollment…`; `shutdown_test.go` `"negative"` →
`negative number of seconds`; `world_write_format_test.go` `"empty"` → `no
allowed types`; `manifest_test.go` `"simple"` → `lowercase letters only`;
`character_test.go` `"empty"` → `empty name`.

## Task 3 — the ratchet

`test/meta/ace_naming_registry_test.go`, two guards, green from the commit that
introduced it (the sweep had already emptied both hit sets, so no red window).

Both failure modes demonstrated and reverted, each attributable **by name**:

| Seeded violation | Result |
|---|---|
| `TestStatus_String` restored in `session_test.go` | exit 201; message named `internal/session/session_test.go:32`, the test, and the tail `"String"`. **Label guard stayed green.** |
| `"success"` label restored in `automigrate_test.go` | exit 201; message named `cmd/holomush/automigrate_test.go:318` and the label. **Name guard stayed green.** |

Design choices recorded: meta-test rather than a lint plugin (the lint config
excludes `_test.go` from several linters, so an analyzer whose whole domain is
test files would need those exclusions audited, not assumed); reuses
`findRepoRoot`/`skipDirs` rather than redeclaring them; **no** invariant-registry
entry and **no** `// Verifies:` annotation, matching the two ratchets beside it.

Both guards assert a non-vacuous corpus (>500 files, >1000 declarations, >500
labels) so a walk that silently matched nothing cannot pass as clean — the
phase's recurring "verification that cannot fail" defect class.

## Deviations from Plan

### 1. [Rule 1 — Bug] The Task 1 predicate silently skipped `internal/web`, hiding 22 violations

**Found during:** Task 3, on the ratchet's first run.
**Issue:** the Task 1 predicate — and 09-RESEARCH's, which produced the ~114
figure — put `"web"` in its skip set intending the top-level SvelteKit directory.
`filepath.WalkDir` matches on the directory **basename**, so `internal/web/` was
skipped too, concealing 22 genuine single-token-tail names
(`TestTranslateEvent_Say`, `TestCORS_Preflight`, `TestHandler_SendCommand_Success`, …).
**Why it surfaced:** the ratchet reuses the repo's shared `skipDirs`, which
correctly contains no `"web"` entry. Its first run over the swept tree failed with
all 22 named — the ratchet catching its own author's blind spot.
**Fix:** renamed all 22 (never exempted — the plan forbids widening the carve-out).
The renamer carried the identical basename bug; its zero-hit guard exited 1 with
`NO OCCURRENCE APPLIED` rather than no-opping silently, which is how the second
application was caught before it did nothing.
**Corrected total:** 138, inside the plan's factor-of-two tolerance on ~114.
**Commit:** `2b206917e`

### 2. [Rule 2 — Correctness] Table-case detection tightened structurally

The label predicate as first written matched any `name:`/`desc:` field and
returned **45** hits. 32 were domain fixtures, not labels —
`world.Location{Name: "Test"}`, `CommandEntryConfig{Name: "test"}`,
`Property{Name: "test"}`. Restricting to `name:` fields on elements of a **slice**
literal (the table-driven shape) took it to exactly the **13** sites 09-RESEARCH
enumerated, file-for-file and line-for-line. This is the phase's defect class (j)
— a needle matching non-target text — caught before it drove any edit.

### 3. [Rule 2 — Accuracy] Ratchet doc comment marked as historical

The ratchet's own prose quotes three pre-sweep names to illustrate the
anti-pattern; all three no longer exist. Marked explicitly so neither a reader nor
a grep-based audit reads them as live citations — the doc-comment-needle trap this
phase hit four times. **Commit:** `2d917a817`

### Not done, deliberately

Historical planning/design documents under `docs/superpowers/{plans,specs}/`
contain ~30 verbatim occurrences of old names inside quoted code blocks and
point-in-time records. These are **not executable references** and rewriting them
would falsify the historical record of what past plans proposed. One live design
spec (`2026-05-21-…-focus-model-…-design.md:543`) cites
`TestReconnect_VsConcurrentLeave_Serializes` as INV-P5-12 evidence; that invariant
is `binding: pending` with no `asserted_by`, so no registry binding is orphaned.
Registry bindings name **files**, not test names — verified: no
`invariants.yaml` entry references any renamed test, and the one `// Verifies:`
annotation inside a renamed function's file (`INV-CRYPTO-17`,
`local_aead_test.go:88`) sits directly above its test and moved with it.

## Blast-radius checks (all clear)

- **Session-matrix registry (09-16):** the rename sweep touched no file under
  `test/` — this plan's own new ratchet, `test/meta/ace_naming_registry_test.go`,
  is the sole addition there; all five guards green.
- **Invariant bindings:** 32 `TestINV_` declarations unchanged; no registry entry
  names a renamed test.
- **Quarantine registry:** untouched; bijection guard green.
- **Pinned names:** both `TestPrivacy_*` Ginkgo containers intact.
- **`task fmt`:** run after every code change; mutated nothing beyond the sweep,
  and leaked no `#magic___^_^___line` sentinel (issue #4864).

## Verification

| Gate | Result |
|---|---|
| `task test` | exit 0 — **10418** tests, 4 skipped (was 10416; +2 = the two ratchet guards) |
| `task test:int` | exit 0 — **10843** tests, 7 skipped (was 10841; +2, same) |
| `task lint` | exit 0 |
| `task test -- ./test/meta/` | exit 0 — **109** tests (was 107; +2, same) |
| ACE ratchet scoped run | exit 0, 2 `--- PASS: TestACENaming*` lines |
| Predicate re-run post-sweep | **0** name hits, **0** label hits |

The `test:int` baseline in the executor briefing (10837) predates 09-16, which
recorded adding exactly +4 guards (`task test` 10412→10416, `test/meta` 103→107);
those run in both lanes, accounting for 10837→10841 before this plan. Every count
delta attributable.

## Self-Check: PASSED

- `test/meta/ace_naming_registry_test.go` — FOUND
- Commits `0234a892c`, `2b206917e`, `c5940b6ea`, `2d917a817` — all FOUND
- Working tree clean

## Full rename mapping (138)

Old → new. Task 1 set (116):

| Old | New |
|---|---|
| TestWaitForTLSCerts_Timeout | TestWaitForTLSCertsReturnsErrorWhenCertsNeverAppearBeforeTheDeadline |
| TestParseContextFlag_Empty | TestParseContextFlagRejectsAnEmptyValue |
| TestExitCodeForError_Unknown | TestExitCodeForErrorMapsAnUnrecognisedCodeToSoftwareFailure |
| TestExitCodeForError_Nil | TestExitCodeForErrorReturnsZeroForANilError |
| TestExitCodeForTerminatedBy_Unspecified | TestExitCodeForTerminatedByMapsUnspecifiedToSoftwareFailure |
| TestMapToExitCodeErr_TEMPFAIL | TestMapToExitCodeErrMapsPhase5TimeoutToTempFail |
| TestMapToExitCodeErr_CANTCREAT | TestMapToExitCodeErrMapsRekeyConflictCodesToCantCreat |
| TestMapToExitCodeErr_SOFTWARE | TestMapToExitCodeErrMapsPhase7AuditFailureToSoftware |
| TestMapToExitCodeErr_NOPERM | TestMapToExitCodeErrMapsDenyCodesToNoPerm |
| TestMapToExitCodeErr_Unknown | TestMapToExitCodeErrPassesAnUnrecognisedCodeThroughUnwrapped |
| TestCmd_CryptoRekey_Resume_Registered | TestCryptoRekeyRegistersAResumeSubcommandCarryingForceDestroyAndConfirmFlags |
| TestCmd_CryptoRekey_Abort | TestRunRekeyAbortPrintsTheAbortTimestampOnSuccess |
| TestCmd_CryptoRekey_Status | TestRunRekeyStatusPrintsTheCheckpointPhaseReturnedByTheServer |
| TestCmd_CryptoRekey_List | TestRunRekeyListPrintsAHeaderAndOneRowPerInFlightRequest |
| TestCmd_CryptoRekey_List_Empty | TestRunRekeyListPrintsOnlyTheHeaderWhenNoRequestsAreInFlight |
| TestCoreCommand_Flags | TestCoreCommandHelpListsEveryExpectedFlag |
| TestCoreCommand_Properties | TestCoreCommandDeclaresItsUseShortAndLongDescriptions |
| TestCoreCommand_Help | TestRootCommandCoreHelpContainsEveryExpectedSection |
| TestSignalStop_Cleanup | TestSignalStopStopsDeliveringOSSignalsToTheChannel |
| TestGatewayCommand_Flags | TestGatewayCommandHelpListsEveryExpectedFlag |
| TestGatewayCommand_Properties | TestGatewayCommandDeclaresItsUseShortAndLongDescriptions |
| TestGatewayCommand_Help | TestRootCommandGatewayHelpContainsEveryExpectedSection |
| TestGatewayConfig_Defaults | TestGatewayDefaultAddressConstantsHoldTheirDocumentedValues |
| TestMigrateCommand_Help | TestRootCommandMigrateHelpMentionsTheUpAndDownSubcommands |
| TestMigrateCommand_Properties | TestMigrateCommandDeclaresItsUseShortAndLongDescriptions |
| TestMigrateStatusLogic_Clean | TestRunMigrateStatusLogicReportsOKWhenTheSchemaIsNotDirty |
| TestMigrateStatusLogic_Dirty | TestRunMigrateStatusLogicReportsDirtyAndTheForceRemedyWhenTheSchemaIsDirty |
| TestStatus_Properties | TestStatusCommandDeclaresItsUseShortAndLongDescriptions |
| TestStatus_Help | TestRootCommandStatusHelpContainsEveryExpectedSection |
| TestStatus_Flags | TestStatusCommandHelpListsEveryExpectedFlag |
| TestStatusConfig_Defaults | TestStatusCommandFlagsDefaultToJSONOffAndTheControlAddresses |
| TestAttributeBags_Initialization | TestNewAttributeBagsReturnsFourNonNilEmptyBags |
| TestAdminReadStreamConnectHandler_Delegates | TestAdminReadStreamConnectHandlerForwardsTheFinishedFrameFromTheUnderlyingRPCHandler |
| TestRekeyHandler_Streams_Progress | TestRekeyHandlerStreamsACompletedEventCarryingTheRunCounters |
| TestRekeyResumeHandler_Streams_Completed | TestRekeyResumeHandlerStreamsACompletedEventMarkedResumedAndForceDestroyed |
| TestRekeyHandler_Abort_SingleControl_Allowed | TestRekeyAbortAcceptsSingleControlEvenWhereRekeyMandatesDualControl |
| TestRekeyHandler_Abort_Terminal | TestRekeyAbortSurfacesTheTerminalCheckpointCodeFromTheRunner |
| TestAdminBootstrapper_Priority | TestAdminBootstrapperReportsTheContentBootstrapPriority |
| TestMigrationBootstrapper_Priority | TestMigrationBootstrapperReportsTheSchemaBootstrapPriority |
| TestPolicyBootstrapper_Priority | TestPolicyBootstrapperReportsThePolicyBootstrapPriority |
| TestSettingBootstrapper_Priority | TestSettingBootstrapperReportsTheWorldBootstrapPriority |
| TestRateLimiter_Concurrency | TestRateLimiterAllowsConcurrentCallsForOneSessionWithoutRacing |
| TestLoggingConfig_Defaults | TestDefaultLoggingConfigEnablesAllSinksAndFloorsSentryAtWarn |
| TestFileStore_Delete | TestFileStoreDeleteRemovesTheItemSoGetReturnsNil |
| TestPostgresStore_Put_Upsert | TestPostgresStorePutOverwritesBodyAndMetadataForAnExistingKey |
| TestPostgresStore_List_Pagination | TestPostgresStoreListPagesThroughItemsAndClearsTheCursorOnTheLastPage |
| TestPostgresStore_Delete | TestPostgresStoreDeleteRemovesTheItemSoGetReturnsNil |
| TestRoutingStore_Put_Fallback | TestRoutingStorePutRoutesAnUnmappedContentTypeToTheFallbackStore |
| TestRoutingStore_List_Pagination | TestRoutingStoreListPagesThroughItemsAndClearsTheCursorOnTheLastPage |
| TestVerifier_EmptyChain_NotInitialized_OK | TestVerifyScopeAcceptsAnEmptyChainAsGenesisEligibleOnFirstBoot |
| TestParseAuditHeaders_Identity | TestParseAuditHeadersLeavesDEKFieldsNilForTheIdentityCodec |
| TestParseAuditHeaders_Encrypted | TestParseAuditHeadersPopulatesDEKRefAndVersionForAnEncryptedCodec |
| TestRekeyHandlerFor_Canonicalize | TestRekeyHandlerCanonicalizeProducesIdenticalBytesForReorderedJSONKeys |
| TestCache_PutGet_Roundtrip | TestCacheGetReturnsTheMaterialStoredUnderTheSameKey |
| TestOrchestrator_Phase1_ConcurrentSameContext_Rejected | TestRunPhase1FreshRejectsASecondRekeyForAContextAlreadyInProgress |
| TestExtractMissingMembers_Missing | TestExtractMissingMembersReturnsNilWhenTheErrorCarriesNoMemberList |
| TestCheckpoint_Phase5MissingMembers_Decodes | TestCheckpointPhase5MissingMembersDecodesJSONAndReportsADecodeFailureCode |
| TestIsUniqueViolation_Detects23505 | TestIsUniqueViolationRecognisesPostgresErrorCode23505 |
| TestNoneProvider_RotateKEK_Refuses | TestNoneProviderRotateKEKRefusesWithTheRotateRefusedCode |
| TestLocalAEADProvider_WrapUnwrap_Roundtrip | TestLocalAEADProviderUnwrapRecoversTheDEKWrappedByTheSameProvider |
| TestLocalAEADProvider_Unwrap_TamperedWrappedBytes_Fails | TestLocalAEADProviderUnwrapRejectsTamperedCiphertextWithATagMismatch |
| TestLocalAEADProvider_Unwrap_WithUnknownKEKKeyID_Fails | TestLocalAEADProviderUnwrapRejectsAnUnknownKEKKeyID |
| TestLocalAEADProvider_HealthCheck_Succeeds | TestLocalAEADProviderHealthCheckSucceedsForAConfiguredProvider |
| TestNoneProvider_HealthCheck_Succeeds | TestNoneProviderHealthCheckSucceedsBecauseRefusingCryptoIsItsDesign |
| TestEnvSource_Persist_Refused | TestEnvSourcePersistRefusesBecauseTheSourceIsReadOnly |
| TestFallback_Case7_TransientError_Propagates | TestFallbackResolverPropagatesATransientHotDEKErrorWithoutMaskingItAsMetadataOnly |
| TestFallback_Case8_ColdReaderError_Wrapped | TestFallbackResolverWrapsAColdReaderLookupFailureAsColdLookupFailed |
| TestFallback_Case9_ColdTransientError_Propagates | TestFallbackResolverPropagatesATransientColdDEKErrorAsDEKResolveTransient |
| TestContentServiceServer_ListContent_Pagination | TestListContentReturnsTheRequestedPageSizeAndACursorForTheNextPage |
| TestDispatcher_HandleCommand_Say | TestHandleCommandSayEmitsASayEventOnTheSpeakersLocationStream |
| TestDispatcher_HandleCommand_Pose | TestHandleCommandPoseEmitsAPoseEvent |
| TestDispatcher_HandleCommand_Quit | TestHandleCommandQuitDeletesTheSessionAndEmitsALeaveEvent |
| TestReconnect_VsConcurrentLeave_Serializes | TestRestoreConnectionFocusSerializesAgainstAConcurrentSceneLeave |
| TestClient_Disconnect | TestClientDisconnectReturnsSuccessFromTheCoreService |
| TestClient_Subscribe | TestClientSubscribeReceivesEventFramesCarryingAnID |
| TestLevelGate_WithAttrs_Propagates | TestLevelGateWithAttrsPropagatesAttributesToTheBaseHandler |
| TestLevelGate_WithGroup_Propagates | TestLevelGateWithGroupPropagatesTheGroupNameToTheBaseHandler |
| TestEntityMutatorRegistry_Register_Duplicate | TestEntityMutatorRegistryRegisterRejectsASecondMutatorForTheSameEntityType |
| TestEntityMutatorRegistry_RegisteredTypes_Sorted | TestEntityMutatorRegistryRegisteredTypesReturnsTypesInSortedOrder |
| TestStatus_String | TestStatusStringReturnsTheLowercaseNameForEachSessionStatus |
| TestSentryLogsTarget_Invalid | TestSentryLogsTargetReturnsErrorForAMalformedDSN |
| TestFormatEvent_State_Suppressed | TestFormatEventSuppressesLocationStateEventsForTelnet |
| TestFormatEvent_System | TestFormatEventRendersASystemEventsMessageText |
| TestRefreshCharacterList_Success | TestRefreshCharacterListShowsTheNewlyAddedCharacterWhenQuitReturnsToCharacterSelect |
| TestRefreshCharacterList_Error | TestRefreshCharacterListReportsAFailureMessageWhenListCharactersErrors |
| TestGemstoneElementTheme_Name | TestGemstoneElementThemeReportsItsRegisteredThemeName |
| TestGemstoneElementTheme_Generate | TestGemstoneElementThemeGenerateReturnsTwoDistinctNonEmptyWords |
| TestGuestAuthenticator_GenerateName_Unique | TestGuestAuthenticatorGenerateNameReturnsNoDuplicatesAcrossRepeatedCalls |
| TestObject_Containment | TestObjectContainmentReportsOnlyTheLocationForAnObjectPlacedInALocation |
| TestWorldService_DeleteLocation_Integration | TestDeleteLocationCascadesToItsEntityProperties |
| TestWorldService_DeleteObject_Integration | TestDeleteObjectCascadesToItsEntityProperties |
| TestWorldService_DeleteCharacter_Integration | TestDeleteCharacterCascadesToItsEntityProperties |
| TestObjectRepository_ListAtLocation_Empty | TestListAtLocationReturnsANonNilEmptySliceForALocationWithNoObjects |
| TestPropertyRepository_ListByParent_Empty | TestListByParentReturnsNoResultsForAParentWithNoProperties |
| TestPropertyRepository_Update | TestPropertyRepositoryUpdatePersistsValueVisibilityAndFlags |
| TestPropertyRepository_Delete | TestPropertyRepositoryDeleteRemovesTheRowSoGetReturnsNotFound |
| TestOrphanConfig_Defaults | TestDefaultOrphanConfigUsesADayLongGracePeriodAndIntervalWithA100Threshold |
| TestOrphanDetector_Construction | TestNewOrphanDetectorReturnsANonNilDetector |
| TestFmt_Parse_Combined | TestFmtParseRendersBoldColourAndNewlineCodesInOneString |
| TestFmt_Parse_Empty | TestFmtParseRendersAnEmptyStringForEmptyInput |
| TestCodeToANSI_Coverage | TestCodeToANSIContainsEveryDocumentedFormatCode |
| TestEmitter_Global | TestEmitterGlobalQueuesOneSystemEventOnTheGlobalStream |
| TestFmt_Separator | TestFmtSeparatorRendersANonEmptyHorizontalRule |
| TestStyledText_Append | TestStyledTextAppendPreservesBothOperandsInTheRenderedOutput |
| TestPropertyRegistry_Resolve_Ambiguous | TestPropertyRegistryResolveReportsEveryMatchForAnAmbiguousPrefix |
| TestProperty_Fields | TestPropertyExposesItsConfiguredFieldsAndAppliesToList |
| TestPropertyType_Constants | TestPropertyTypeConstantsHoldTheirDocumentedStringValues |
| TestPropertyType_String | TestPropertyTypeStringReturnsTheUnderlyingStringForEachType |
| TestPropertyRegistry_MustRegister_Panics | TestPropertyRegistryMustRegisterPanicsOnADuplicateName |
| TestPropertyRegistry_MustRegister_Success | TestPropertyRegistryMustRegisterStoresAResolvablePropertyForAFreshName |
| TestPluginServerAdapter_HandleEvent_Success | TestPluginServerAdapterHandleEventReturnsTheHandlersEmitEvents |
| TestCommandRequest_Fields | TestCommandRequestCarriesEveryCallerSuppliedFieldVerbatim |
| TestCommandResponse_Fields | TestCommandResponseCarriesEventsOutputBootedSessionsAndEndSession |
| TestHandleJoin_AutoFocus_Terminal | TestSceneJoinAutoFocusesTerminalConnectionsAndReportsItInTheOutput |
| TestRenderPoseOrder_Empty | TestRenderPoseOrderReportsNoParticipantsForAnEmptyScene |
| TestGetPoseOrder_StoreError_Internal | TestGetPoseOrderMapsAStoreFailureToInternalRatherThanPermissionDenied |

Deviation-1 set — `internal/web` (22):

| Old | New |
|---|---|
| TestPlayerTokenFromHeader_Present | TestPlayerTokenFromHeaderReturnsTheTokenWhenTheInjectHeaderIsSet |
| TestPlayerTokenFromHeader_Missing | TestPlayerTokenFromHeaderReturnsUnauthenticatedWhenTheHeaderIsAbsent |
| TestPlayerTokenFromHeader_Empty | TestPlayerTokenFromHeaderReturnsUnauthenticatedWhenTheHeaderIsEmpty |
| TestWebSelectCharacter_Reattached | TestWebSelectCharacterForwardsTheReattachedFlagFromCore |
| TestSetSessionCookie_Insecure | TestSetSessionCookieOmitsTheSecureAttributeWhenSecureIsFalse |
| TestGetSessionToken_Missing | TestGetSessionTokenReturnsEmptyWhenTheRequestCarriesNoCookie |
| TestCORS_Preflight | TestCORSMiddlewareAnswersPreflightWithNoContentAndTheAllowedHeaders |
| TestCORS_NoOrigins_Passthrough | TestCORSMiddlewareSetsNoAllowOriginWhenNoOriginsAreConfigured |
| TestHandler_SendCommand_Success | TestSendCommandReturnsSuccessWhenCoreAcceptsTheCommand |
| TestHandler_Disconnect_Success | TestDisconnectReturnsAResponseWhenCoreAcceptsTheDisconnect |
| TestHandler_GetCommandHistory_Success | TestGetCommandHistoryReturnsTheCommandsRecordedForTheSession |
| TestFileServer_Override | TestFileServerServesIndexFromTheOverrideDirectory |
| TestFileServer_MissingAsset_Returns404 | TestFileServerReturns404ForAnAssetThatDoesNotExist |
| TestTranslateEvent_Say | TestTranslateEventRendersSayWithSpeakerActorIDAndSaysLabel |
| TestTranslateEvent_Pose | TestTranslateEventRendersPoseAsACommunicationActionCarryingTheActor |
| TestTranslateEvent_Arrive | TestTranslateEventRendersArriveAsAMovementNotificationOnBothChannels |
| TestTranslateEvent_Leave | TestTranslateEventRendersLeaveAsAMovementEventOnBothChannels |
| TestTranslateEvent_System | TestTranslateEventRendersSystemAsATerminalOnlyNotification |
| TestTranslateEvent_Move | TestTranslateEventRendersMoveWithItsMessageOnBothChannels |
| TestTranslateEvent_OOC | TestTranslateEventRendersOOCAsAnActionCarryingTheStyleMetadata |
| TestTranslateEvent_Pemit | TestTranslateEventRendersPemitAsATerminalOnlyNarrative |
| TestTranslateEvent_Page | TestTranslateEventRendersPageAsSpeechCarryingThePagesLabel |

## Known Stubs

None. No stub, placeholder, or unwired component was introduced.
