---
phase: 07-event-model-bootstrap-decomposition
plan: 08
subsystem: infra
tags: [go, eventbus, plugin, grpc, cursor, pagination, arch-04, d-07, d-08]

# Dependency graph
requires:
  - phase: 07-07
    provides: "eventbus.Event as the tree's single Event representation; plugins.HistoryReader.ReplayTail and hostfunc.HistoryReader.ReplayTail both return []eventbus.Event (carrying Seq), unblocking this plan's cursor fix"
provides:
  - "D-07 fixed: plugin history pagination is seq-keyed end-to-end. ReplayTail (both plugins.HistoryReader and hostfunc.HistoryReader interfaces) gains beforeSeq uint64; encodeHostEventCursor (hostcap) and both Lua cursor-encode sites (hostfunc) carry each event's real eventbus.Event.Seq instead of a hardcoded 0."
  - "busHistoryReaderAdapter.ReplayTail (cmd/holomush) and focusHistoryReaderAdapter.ReplayTail (integrationtest harness) both set HistoryQuery.BeforeSeq only when beforeSeq != 0 — beforeSeq==0 means 'no cursor, read the tail' (the settled legacy-cursor policy; no ID-only fallback exists on either tier)."
  - "D-08 preserved: hostv1.Event stays at exactly 8 fields, no plugin-facing seq — pinned by a census meta-test (TestHostV1EventFieldCensusExcludesSequence) asserting field-set equality, not just absence."
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Deterministic ULID-vs-seq-inversion test construction: precompute N ULIDs via ulid.New(ulid.Timestamp(time.Now()), crand.Reader) (not core.NewULID, whose monotonic clamp fights reordering), sort, publish sequentially in descending lex order, then assert a premise self-check (forward scan: seq strictly increasing, ID strictly decreasing) before trusting the pagination assertions — replaces a probabilistic concurrent-publisher construction that could silently false-green."
    - "Two-page fake-reader round-trip test: drive a real RPC/hostfunc handler for page 1, feed its NextCursor verbatim into page 2's request, and assert the fake's recorded page-2 call arguments equal page 1's oldest-event (index 0) real (Seq, ID) pair — catches a next_cursor-only regression that a single-page encode test cannot."

key-files:
  created:
    - cmd/holomush/plugin_replaytail_pagination_integration_test.go
    - internal/plugin/hostcap/hostv1_no_seq_test.go
  modified:
    - internal/plugin/host.go
    - internal/plugin/hostfunc/stdlib_focus.go
    - internal/plugin/hostfunc/streamauth.go
    - internal/plugin/hostcap/servers.go
    - cmd/holomush/sub_grpc.go
    - internal/testsupport/integrationtest/harness.go
    - internal/plugin/hostfunc/streamauth_test.go
    - internal/plugin/hostfunc/stdlib_focus_test.go
    - internal/plugin/hostcap/streamhistory_test.go
    - internal/plugin/goplugin/host_service_test.go
    - cmd/holomush/sub_grpc_adapters_test.go

key-decisions:
  - "ReplayTail's new param is positioned as beforeSeq uint64 immediately before beforeID ulid.ULID in both interfaces (plugins.HistoryReader, hostfunc.HistoryReader), per the plan's <settlements> — not a cursor struct, since the existing parameter list already carries the cursor pieces positionally."
  - "next_cursor / per-event encode sites keep their existing index-[0] anchor (the oldest event of the ascending page) unchanged — only the hardcoded Seq:0 inside each cursor.HostCursor literal was wrong. Verified this is unchanged at both hostcap/servers.go:904's protoEvents[0].GetCursor() and stdlib_focus.go's events[0] next_cursor literal."
  - "beforeSeq==0 means 'no cursor — read the tail', matching the pre-existing behavior of both tiers (hot: buildConfig's if q.BeforeSeq > 0 cursor path; cold: hasCursor := cursorSeq > 0). No ID→seq resolution or reject-as-stale path was added — both were explicitly rejected in the plan's <legacy_zero_seq_policy>."
  - "Reworded two negation comments ('not synthesize an ID-only fallback' / 'there is no ID-only fallback') to 'a fallback keyed on ID alone' after self-verifying Task 2's own acceptance-criteria grep for the retracted framing — the substring match can't distinguish an assertion from a negation, so the original wording tripped the plan's own regression guard."

patterns-established:
  - "When a plan's acceptance criteria include a substring-match grep asserting some retracted phrase does NOT reappear, phrase code comments that describe the negation without using the literal retracted substring — otherwise the negation itself trips the guard."

requirements-completed: [ARCH-04]

coverage:
  - id: D1
    description: "Plugin history pagination via busHistoryReaderAdapter neither skips nor repeats on a quiet stream (D-07's primary, deterministic RED gate) or when ULID lex order deterministically disagrees with stream sequence"
    requirement: "ARCH-04"
    verification:
      - kind: integration
        ref: "cmd/holomush/plugin_replaytail_pagination_integration_test.go#TestReplayTailPaginationAdvancesAcrossPagesOnQuietStream"
        status: pass
      - kind: integration
        ref: "cmd/holomush/plugin_replaytail_pagination_integration_test.go#TestReplayTailPaginationAdvancesWhenULIDOrderDisagreesWithSeqOrder"
        status: pass
    human_judgment: false
  - id: D2
    description: "Both plugins.HistoryReader.ReplayTail and hostfunc.HistoryReader.ReplayTail carry a beforeSeq param in lockstep; the hostcap and Lua cursor encoders both thread the real Seq at every encode site (per-event AND next_cursor, independently); beforeSeq==0 produces a tail read with no ID-only fallback"
    requirement: "ARCH-04"
    verification:
      - kind: unit
        ref: "cmd/holomush/sub_grpc_adapters_test.go#TestBusHistoryReaderReplayTailPassesBeforeSeqOnQuery"
        status: pass
      - kind: unit
        ref: "cmd/holomush/sub_grpc_adapters_test.go#TestBusHistoryReaderReplayTailZeroBeforeSeqLeavesQueryBeforeSeqUnsetTailRead"
        status: pass
      - kind: unit
        ref: "internal/plugin/hostcap/streamhistory_test.go#TestStreamHistoryQueryStreamHistoryTwoPageRoundTripAnchorsOnRealSeq"
        status: pass
      - kind: unit
        ref: "internal/plugin/hostfunc/stdlib_focus_test.go#TestQueryStreamHistoryMultipageWalkThreadsRealSeqIntoNextCursor"
        status: pass
      - kind: unit
        ref: "internal/plugin/hostfunc/stdlib_focus_test.go#TestQueryStreamHistoryCursorRoundTripsAsBase64"
        status: pass
      - kind: unit
        ref: "internal/plugin/goplugin/host_service_test.go#TestQueryStreamHistoryDecodesOpaqueBeforeIDCursor"
        status: pass
      - kind: other
        ref: "rg -c 'Seq: 0' internal/plugin/hostcap/servers.go internal/plugin/hostfunc/stdlib_focus.go -> 0 matches in either file"
        status: pass
    human_judgment: false
  - id: D3
    description: "D-08 preserved: hostv1.Event stays at exactly 8 fields, no plugin-facing seq; the wire proto is unchanged"
    requirement: "ARCH-04"
    verification:
      - kind: unit
        ref: "internal/plugin/hostcap/hostv1_no_seq_test.go#TestHostV1EventFieldCensusExcludesSequence"
        status: pass
      - kind: other
        ref: "rg -c 'seq' api/proto/holomush/plugin/host/v1/stream.proto -i -> 0"
        status: pass
    human_judgment: false
  - id: D4
    description: "No regressions across the whole repo: task build, task test (whole repo), task test:int (whole repo), task lint all green"
    requirement: "ARCH-04"
    verification:
      - kind: other
        ref: "task build (exit 0); task test -> 10251 tests, 4 pre-existing skips, 0 failures; task test:int -> 10673 tests, 7 pre-existing skips, 0 failures; task lint (exit 0)"
        status: pass
    human_judgment: false

duration: ~35min
completed: 2026-07-18
status: complete
---

# Phase 07 Plan 08: D-07 Plugin History Pagination Seq Fix Summary

**Fixed a live correctness bug where plugin history pagination silently re-requested the newest page forever on a quiet stream (no concurrency needed) because the cursor never carried JetStream's stream sequence — both runtimes now thread the real `Seq` through an opaque cursor, with a census guard keeping it off the plugin wire.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-07-17T21:24Z (approx, following 07-07 completion)
- **Completed:** 2026-07-18T01:58Z
- **Tasks:** 3 completed
- **Files modified:** 13 (2 created, 11 modified)

## RED Gate Evidence (Task 1, before Task 2's fix)

`task test:int -- -run 'TestReplayTailPagination' ./cmd/holomush/...` FAILED as predicted, with the **repeat** failure mode (not a skip), confirming `<red_framing_correction>`'s diagnosis that a lone `BeforeID` paginates nothing — every page read from the tail:

```
=== FAIL: cmd/holomush TestReplayTailPaginationAdvancesAcrossPagesOnQuietStream (0.01s)
    Error: Should be false
    Messages: event 01KXSDWE0W2RGYDDS1369M6X4N repeated across pages — cursor is not advancing (D-07)

=== FAIL: cmd/holomush TestReplayTailPaginationAdvancesWhenULIDOrderDisagreesWithSeqOrder (5.00s)
    Error: Should be false
    Messages: event 01KXSDWE109W0CAYPP1ZNXZKV9 repeated across pages — cursor is not advancing by seq (D-07)
```

## GREEN Evidence (post Task 2 fix) — page-advance anchor proof

Per the round-6 cross-AI requirement, the observed **Spec A** page 1 and page 2 ID sets (20 events each, quiet-stream walk, `pageSize=20`) show zero intersection, confirming the fix anchors on the correct (oldest, index-0) event and advances by real `Seq`:

```
page 1 ids: [01KXSF4KNT0EYKAPYPNNGBK06M 01KXSF4KNT0EYKAPYPNPEX9RJR 01KXSF4KNT0EYKAPYPNSBA8H53
             01KXSF4KNT0EYKAPYPNW3849DR 01KXSF4KNT0EYKAPYPNZHQ3NCA 01KXSF4KNT0EYKAPYPP1DCEVKX
             01KXSF4KNT0EYKAPYPP2RR6DSF 01KXSF4KNT0EYKAPYPP4TPFRYP 01KXSF4KNT0EYKAPYPP711PJ3Y
             01KXSF4KNT0EYKAPYPPAA2H3FQ 01KXSF4KNT0EYKAPYPPAH39RV7 01KXSF4KNT0EYKAPYPPE3Y8DYD
             01KXSF4KNT0EYKAPYPPHPAX7FW 01KXSF4KNT0EYKAPYPPJRT8PAN 01KXSF4KNT0EYKAPYPPMQBTJ9G
             01KXSF4KNT0EYKAPYPPMXJG3W3 01KXSF4KNT0EYKAPYPPP7HCPKS 01KXSF4KNT0EYKAPYPPSKT24DG
             01KXSF4KNT0EYKAPYPPW5MKPW5 01KXSF4KNT0EYKAPYPPXRY5ZQR]
page 2 ids: [01KXSF4KNT0EYKAPYPM165BPED 01KXSF4KNT0EYKAPYPM1YB1YZY 01KXSF4KNT0EYKAPYPM5P8QVXC
             01KXSF4KNT0EYKAPYPM7906G02 01KXSF4KNT0EYKAPYPMAFHTT53 01KXSF4KNT0EYKAPYPMCV92XZZ
             01KXSF4KNT0EYKAPYPME4NAF92 01KXSF4KNT0EYKAPYPMGQ9PZT0 01KXSF4KNT0EYKAPYPMJ3DQTEY
             01KXSF4KNT0EYKAPYPMM1P1XC5 01KXSF4KNT0EYKAPYPMQ4FEV4J 01KXSF4KNT0EYKAPYPMTV9XBER
             01KXSF4KNT0EYKAPYPMYSMZTDH 01KXSF4KNT0EYKAPYPN2MZRY6X 01KXSF4KNT0EYKAPYPN3MX59CW
             01KXSF4KNT0EYKAPYPN6DZMCCD 01KXSF4KNT0EYKAPYPN8PSZEFB 01KXSF4KNT0EYKAPYPNB2XMHW8
             01KXSF4KNT0EYKAPYPNEDP4GZX 01KXSF4KNT0EYKAPYPNJ29PGDY]
```
page 1 ∩ page 2 = ∅ (verified by inspection; also structurally guaranteed by the test's own `seen` map assertion, which would have failed on the first repeated ID). The anchor that produced page 2 was **page 1's oldest event, index 0** — captured this evidence via temporary `t.Logf` instrumentation, then reverted (working tree confirmed byte-identical to the committed test file afterward).

## Accomplishments

- `plugins.HistoryReader.ReplayTail` and `hostfunc.HistoryReader.ReplayTail` both gained `beforeSeq uint64` in lockstep (positioned immediately before `beforeID`), across all 4 production implementations (`busHistoryReaderAdapter`, `focusHistoryReaderAdapter`, `authorizingHistoryReader`, plus the `lua/hostcap_adapter.go` structural-typing coupling — verified via `task build`) and 4 test fakes.
- `hostcap/servers.go`'s `encodeHostEventCursor` now takes `(seq uint64, id ulid.ULID)`; the per-event encode call site passes each event's own real `e.Seq`; the false "Seq is not available here" doc comment is replaced with the true post-ARCH-04 rationale; `next_cursor`'s existing `protoEvents[0]` (oldest-event) anchor is **unchanged** — only its hardcoded `Seq: 0` was wrong.
- `hostfunc/stdlib_focus.go`'s **two independent** cursor-encode sites (per-event at the old `:437-441`, and the separate `next_cursor` literal at the old `:459-463`) and its decode site all now thread the real Seq — the Lua runtime had the identical bug independently of the binary runtime (plugin-runtime-symmetry).
- `cmd/holomush/sub_grpc.go`'s `busHistoryReaderAdapter.ReplayTail` and the integration-test harness's `focusHistoryReaderAdapter.ReplayTail` both set `HistoryQuery.BeforeSeq` only when `beforeSeq != 0`, alongside the existing `BeforeID` tripwire — matching the settled `beforeSeq==0` → "read the tail" policy with no ID-only fallback.
- Added a hostcap two-page `QueryStreamHistory` round-trip test and a Lua-path multipage walk test, both of which feed page 1's real `NextCursor`/`next_cursor` into a page-2 request and assert the fake reader's recorded page-2 arguments equal page 1's oldest event's real `(Seq, ID)` — this is the only coverage that would catch a `next_cursor`-only partial fix (the most plausible regression per the cross-AI review).
- Added `internal/plugin/hostcap/hostv1_no_seq_test.go`'s `TestHostV1EventFieldCensusExcludesSequence`, a census guard asserting `hostv1.Event`'s proto field set is exactly the 8 documented names, with a belt-and-braces "no field name contains seq/sequence" check. Demonstrated RED by temporarily adding a 9th name and reverting.

## Task Commits

1. **Task 1: RED — multipage repeat at the plugin ReplayTail boundary** - `b92ca1fa5` (test)
2. **Task 2: GREEN — thread the real Seq through the cursor and into the query** - `c36cb1ea8` (feat)
3. **Task 3: D-08 guard — assert hostv1.Event never grows a seq field** - `629103a04` (test)
4. **Fixup: reword negation comments tripping Task 2's own retraction grep** - `ee987f6e6` (fix)

_Note: an unplanned 4th commit was needed — see Deviations below._

## Files Created/Modified

- `cmd/holomush/plugin_replaytail_pagination_integration_test.go` — new; Spec A (quiet-stream multipage walk, the RED gate) and Spec B (deterministic ULID-vs-seq inversion, post-green defence-in-depth), both driving the real `busHistoryReaderAdapter` against an embedded-JetStream bus
- `internal/plugin/hostcap/hostv1_no_seq_test.go` — new; D-08 census guard
- `internal/plugin/host.go`, `internal/plugin/hostfunc/stdlib_focus.go` — `HistoryReader.ReplayTail` interface decls gain `beforeSeq uint64`
- `internal/plugin/hostfunc/streamauth.go` — `authorizingHistoryReader.ReplayTail` forwards the new param unchanged; ABAC deny path untouched
- `internal/plugin/hostcap/servers.go` — `encodeHostEventCursor` signature change; per-event encode passes real `Seq`; decode side extracts `Seq`; false doc comment replaced
- `cmd/holomush/sub_grpc.go` — `busHistoryReaderAdapter.ReplayTail` sets `HistoryQuery.BeforeSeq` conditionally
- `internal/testsupport/integrationtest/harness.go` — `focusHistoryReaderAdapter.ReplayTail` mirrors the same fix
- Test fakes updated to the new 6-arg signature: `internal/plugin/hostfunc/streamauth_test.go`, `internal/plugin/hostfunc/stdlib_focus_test.go`, `internal/plugin/hostcap/streamhistory_test.go`, `internal/plugin/goplugin/host_service_test.go`, `cmd/holomush/sub_grpc_adapters_test.go` — each also gained new assertions covering `beforeSeq` threading and/or the multipage anchor

## Decisions Made

See frontmatter `key-decisions`. Notably: the `next_cursor` / per-event cursor `[0]`-index anchors were **not** changed — the plan's `<page_advance_anchor>` correction was already reflected correctly in the current codebase (both sites already indexed the oldest event); only the hardcoded `Seq: 0` inside each was wrong, and that is exactly what this plan fixed.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Fixed 4 `unconvert` lint findings on new test code**
- **Found during:** Task 2 (`task lint` after adding `beforeSeq` assertions)
- **Issue:** `assert.Equal(t, uint64(constName), ...)` where `constName` was already declared `const x = uint64(N)` — an unnecessary type conversion.
- **Fix:** Removed the redundant `uint64(...)` wrapper at each of the 4 call sites.
- **Files modified:** `cmd/holomush/sub_grpc_adapters_test.go`, `internal/plugin/goplugin/host_service_test.go`, `internal/plugin/hostfunc/stdlib_focus_test.go` (2 sites).
- **Verification:** `task lint` exits 0.
- **Committed in:** `c36cb1ea8` (folded into the Task 2 commit, before it was made).

**2. [Rule 1 - Bug] Self-tripped acceptance-criteria grep on retracted framing**
- **Found during:** Task 2 self-verification (running the plan's own listed acceptance-criteria greps after committing)
- **Issue:** Task 2's acceptance criteria include `rg -ci 'id.?only fallback|fall.?back to .?BeforeID' internal/plugin/ cmd/holomush/` returning 0 matches, to guard against reinstating the retracted "ID-only fallback" framing. Two of my own doc/test comments correctly described the *absence* of such a fallback ("not synthesize an ID-only fallback", "there is no ID-only fallback") but the substring-match grep cannot distinguish an assertion from a negation, so it matched anyway.
- **Fix:** Reworded both to "a fallback keyed on ID alone" — identical meaning, doesn't contain the literal retracted phrase.
- **Files modified:** `cmd/holomush/sub_grpc_adapters_test.go`, `internal/plugin/hostfunc/stdlib_focus.go`.
- **Verification:** `rg -ci '...'` now returns 0 matches (exit 1, no output); `task build`, `task test -- ./cmd/holomush/... ./internal/plugin/...`, `task fmt` all still green.
- **Committed in:** `ee987f6e6` (separate small follow-up commit, since Task 2's commit was already made when this was discovered).

---

**Total deviations:** 2 auto-fixed (1 Rule 3 blocking lint fix folded into Task 2's commit, 1 Rule 1 bug fix as a small follow-up commit)
**Impact on plan:** Both are hygiene/self-consistency fixes with zero behavior change. No scope creep.

## Issues Encountered

- **Doc-drift not fixed (intentionally deferred, per the plan's own conditional):** `internal/eventbus/history/tier.go:717-718`'s comment — *"The ID cursor (AfterID / BeforeID) is preserved so matchesQuery can still filter at the boundary"* — is stale (`matchesQuery` does no `BeforeID`/`AfterID` filtering; only `AfterSeq`/`BeforeSeq` are checked). The plan explicitly said "If Task 2 touches this file, correct the comment; otherwise note it in the SUMMARY." Task 2's `<files>` list does not include `tier.go`, so per the plan's own instruction this was left untouched and is noted here instead. Not blocking; a future doc-cleanup pass can fix it.
- **`gsd-tools` state/roadmap update commands unavailable in this environment** (no `gsd-core/bin/gsd-tools.cjs` resolvable path and no `gsd-tools` CLI on PATH) — STATE.md, ROADMAP.md, and REQUIREMENTS.md were updated directly via Edit rather than through the `gsd_run query state.*` / `roadmap.update-plan-progress` / `requirements.mark-complete` verbs. Content and semantics match what those verbs would have produced (advance to Plan 9, progress bar recalculated, decisions appended, ARCH-04 requirement marked complete for this plan's contribution).

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- D-07 (the live pagination correctness bug) is fixed and regression-tested at 3 independent layers: the plugin-facing integration boundary (`busHistoryReaderAdapter`), the hostcap (binary-runtime) unit layer, and the hostfunc (Lua-runtime) unit layer.
- D-08 (no plugin-facing seq) is preserved and now has a durable census guard (`TestHostV1EventFieldCensusExcludesSequence`) that will fail loudly if a future change adds any new field to `hostv1.Event`, forcing an explicit justification.
- **crypto-reviewer gate reminder:** per CLAUDE.md's Pre-Push Review Gates table, this plan's changes touch the history path (`internal/plugin/hostcap/servers.go`, `internal/plugin/hostfunc/stdlib_focus.go`, `cmd/holomush/sub_grpc.go`) — `crypto-reviewer` (`/holomush-dev:review-crypto`) MUST run before this branch is pushed / a PR is opened. Not run as part of this execution (that gate belongs to the pre-push/ship step, not per-plan execution).
- No blockers for the remaining phase-07 plans (09, 10, 11).

---
*Phase: 07-event-model-bootstrap-decomposition*
*Completed: 2026-07-18*

## Self-Check: PASSED
