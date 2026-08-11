---
phase: 04-shared-facade-helpers-characteraccessservice
reviewed: 2026-08-11T23:05:00Z
depth: deep
iteration: 3
files_reviewed: 25
files_reviewed_list:
  - internal/grpc/characteraccess_directory.go
  - internal/grpc/characteraccess_directory_test.go
  - internal/grpc/characteraccess_owner.go
  - internal/grpc/characteraccess_owner_test.go
  - internal/grpc/characteraccess_profile_test.go
  - internal/grpc/characteraccess_projection.go
  - internal/grpc/characteraccess_service.go
  - internal/grpc/characteraccess_viewer_test.go
  - internal/grpc/characteraccess_write.go
  - internal/grpc/characteraccess_write_test.go
  - internal/grpc/player_gate.go
  - internal/grpc/player_gate_test.go
  - internal/web/status_interceptor.go
  - internal/web/status_interceptor_test.go
  - internal/web/character_handlers.go
  - internal/world/service.go
  - internal/world/service_profile_test.go
  - internal/world/mutator.go
  - internal/world/mutator_profile_test.go
  - internal/world/payloads.go
  - internal/access/policy/seed.go
  - test/integration/access/character_profile_read_test.go
  - test/integration/access/character_write_test.go
  - test/integration/access/character_directory_test.go
  - test/meta/characteraccess_routing_census_test.go
findings:
  critical: 0
  warning: 1
  info: 0
  total: 1
status: issues_found
---

# Phase 4: Code Review Report (iteration 3 — final)

**Reviewed:** 2026-08-11
**Depth:** deep
**Files Reviewed:** 25 (the narrowed post-fix scope)
**Status:** issues_found (0 critical, 1 warning)

## Summary

Scope was the two fix passes' output and its immediate seams. Each of pass 2's five
fixes was re-derived **from the code and, where the claim was empirical, from a
runnable probe** — not from `04-REVIEW-FIX.iter3.md`. All five hold. None
introduced a regression I could construct a case against. One defect remains
outstanding, and it predates both fix passes: a doc comment in the web proxy file
that states the opposite of what the code now does.

### 1. Did pass 2's five fixes hold?

| Fix | Verdict | Evidence |
| --- | --- | --- |
| `f49f8ed44` errutil-vs-bare-slog guard | **Holds, and genuinely discriminates** | I did not take the fix report's RED proof on trust. I ran both log shapes through a standalone `slog.TextHandler` + `samber/oops@v1.22.0` probe. Under the bare `slog.ErrorContext(ctx, msg, "error", err)` form the output is `…error.message="" error.err="database unreachable" error.code=CHARACTER_LIST_FAILED…`: `Contains(logged, "code=CHARACTER_LIST_FAILED")` is **true** (it is a substring of `error.code=…`), and `Contains(logged, "error.code=")` is **true**. So the paired `assert.NotContains(t, logged, "error.code=")` at `player_gate_test.go:346-348` is the single load-bearing clause and it fails under the bug, exactly as the comment at `:336-343` now says. The production side is genuinely `errutil.LogErrorContext` at both sites (`player_gate.go:94`, `:119`), and `pkg/errutil/log.go:38-46` emits `error` as a **string** plus a flat `code` — so `errutil` can never itself produce an `error.` group and trip the NotContains. |
| `736d15317` did not re-disarm it | **Confirmed** | Different file, different assertion. The directory spec (`characteraccess_directory_test.go:622-623`) forbids the FULL prefixed `"character access: profile visibility evaluation failed"`. The infra-failure branch logs `msg="character access: directory gate infrastructure failure" error="profile visibility evaluation failed"` (`characteraccess_directory.go:202-205`) — the two never concatenate into the forbidden string, so the added `infraFailureGate` subtest does not collide. The double-log **counter** (`:624`, `strings.Count(logged, "the policy store is unreachable") == 1`) is the clause that actually discriminates for both branches: `erroringGate` puts that text in the oops error, `infraFailureGate` puts it in `decision.Reason()` which `evaluateGate` re-attaches via `.With("reason", …)`, so a restored second log in `mapProfileError` pushes the count to 2 under **either** fixture. |
| `56876f339` identical-value resubmit | **Holds; write path provably untouched** | `service.go:1157` appends the row to `updates` **unconditionally**; only the `changed` append at `:1162` is gated. The empty-partition early return at `:1228` tests `len(creates)==0 && len(updates)==0 && len(deletes)==0` and never consults `changed`, so an all-identical resubmit still writes, still runs the CAS (`char.Version = expectedVersion` at `:1244`), still bumps. The `current.Value == nil` arm is correct: a NULL-valued row against a non-empty submitted value is a real change, and `value == ""` is already claimed by the first branch. Both new subtests `EXPECT(...).Update(...).Once()` on a mockery mock (`service_profile_test.go:404`, `:427`), so the write semantics are pinned by a failing expectation rather than by prose. `BuildCharacterProfileUpdatePayload` (`payloads.go:455`) builds `make([]string, 0)`, so an empty `changed` marshals to `[]`, not `null`; `taxonomy.go:213` declares the field as unconstrained `json`; and `rg` finds **no** production consumer of `changed_attributes` that an empty list could break. |
| `d3573f8e3` shared `newCorpusEngine` | **Holds; no spec weakened** | The diff is a pure lift: the directory file's closure was byte-identical to the profile-read one minus the refusal, and it was deleted, not merged-down. The surviving package-level helper (`character_profile_read_test.go:112-132`) carries **both** guards — `len(excluded)+len(appended) > 0` at `:122` and `corpus.removed == len(excluded)` at `:128` — and all five call sites now reach it (`character_profile_read_test.go:315,344,486,519`; `character_directory_test.go:239,264,285`). The refusal therefore fires for a neither-excludes-nor-appends corpus at every one of them, including the three that previously bypassed it. `env` is package-level so no signature changed. `task test:int` was not re-run here; the fix report records 99/99. |
| `9793f7c74` no-op documentation | **Matches behavior** | The facade's empty-mask path (`characteraccess_write.go:288-312`) requires only `expected_version > 0` (`requireGuardedVersion`) and returns success with the freshly-read character regardless of staleness — exactly what its new comment at `:294-306` claims. The domain's empty-partition path (`service.go:1228-1236`) refuses `char.Version != expectedVersion` with `CodeConcurrentEdit` — exactly what its new comment at `:1219-1227` claims. Each side cross-references the other by file, and both warn against collapsing the asymmetry. |

### 2. Did any fix introduce a regression?

None found. Scoped `task test -- ./internal/grpc/... ./internal/world/... ./test/meta/... ./internal/web/... ./internal/access/...` is **exit 0**, 3882 tests, 1 pre-existing skip (`TestGenerateEBNF`, `DUMP_EBNF` opt-in).

Two things I checked specifically for regression and cleared:

- **The identical-value gate cannot starve the envelope.** `changed` empty while a partition is non-empty is representable end to end (`[]`, unconstrained schema, no consumer), and the two no-op shapes stay distinct: empty-partition ships **no** envelope and no version bump; all-identical ships **one** envelope naming nothing, with the bump.
- **`isDomainValueRejection` (`characteraccess_write.go:414-417`) is not caught by the `oops.Code()` deepest-code trap.** `ValidateDescription` returns a bare `*ValidationError` with no oops code (`validation.go:96-110`), so `oops.Code(CodeCharacterInvalid).Wrap(setErr)` (`service.go:938`) leaves `CHARACTER_INVALID` as the deepest — and the classification is correct.

## Warnings

### WR-01: the web proxy file documents four shipped RPCs as still answering `Unimplemented`

**File:** `internal/web/character_handlers.go:53-62`

**Issue:** The block comment introducing the four owner-audience proxies states:

```go
// THEY ANSWER Unimplemented TODAY, AND THAT IS THE INTENDED STATE. Their facade
// handlers land in plans 04-05 and 04-06; until then
// UnimplementedCharacterAccessServiceServer answers Unimplemented and these
// proxies pass it through unchanged.
```

That was true when the file landed in `92e484214` (plan 04-04). It is false at HEAD:
all four facade handlers shipped in this phase and are live —
`ListMyCharacters` (`characteraccess_owner.go:68`), `GetMyCharacter`
(`characteraccess_owner.go:109`), `UpdateCharacterProfile`
(`characteraccess_write.go:257`), `UpdateCharacterDescription`
(`characteraccess_write.go:463`). The generated `Unimplemented…` embed no longer
serves any of them.

This is not cosmetic drift. The comment names a **specific wrong cause** for a
specific wire outcome, and it sits on the surface an operator reaches first when a
profile edit fails. A reader debugging a real `CodeUnimplemented` — which these
proxies genuinely still return, from the `h.characterAccess == nil` guard at `:70`,
`:97`, `:127`, `:169` — is told by the comment above the guard that the code is
expected and the facade is unbuilt, pointing away from the actual cause (an
unwired client). The routing census (`test/meta/characteraccess_routing_census_test.go:582`)
pins these four proxies as owner-audience members precisely because they are live,
so the comment also contradicts a checked-in gate one directory over.

It also carries a second stale claim in the same breath — that the proxies "exist
now because `webv1connect.WebServiceHandler` is asserted at compile time … so
declaring the RPCs without the methods would break the build." That was the
placeholder rationale; their reason for existing now is that they proxy shipped
RPCs.

**Fix:** replace the block with what is true at HEAD, keeping the shape claim that
is still load-bearing:

```go
// The four owner-audience proxies below copy WebGetCharacterProfile's shape
// exactly: nil-client guard, token from the header, bounded context,
// field-by-field forward, log-then-pass-through on error. They compute nothing.
//
// All four facade handlers are LIVE (plans 04-05, 04-06):
// CharacterAccessServer.ListMyCharacters / GetMyCharacter /
// UpdateCharacterProfile / UpdateCharacterDescription. The only Unimplemented
// these proxies produce is their own nil-client guard, which is a wiring fault
// in cmd/holomush, NOT an unbuilt facade.
```

---

_Reviewed: 2026-08-11_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep — iteration 3 (final)_
