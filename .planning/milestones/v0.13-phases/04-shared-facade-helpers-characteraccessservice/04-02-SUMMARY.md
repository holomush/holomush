---
phase: 04-shared-facade-helpers-characteraccessservice
plan: 02
subsystem: internal/grpc
tags: [authorization, refactor, abac, scene-access, character-identity]
status: complete

requires:
  - "internal/auth: PlayerSessionRepository, PlayerRepository, CharacterRepository"
  - "internal/grpc/auth_handlers.go: resolvePlayerSessionWithRepo"
provides:
  - "internal/grpc: playerGate (embeddable shared gate)"
  - "internal/grpc: newPlayerGate(playerSessionRepo, playerRepo, charRepo, guestDenialMessage)"
  - "internal/grpc: (*playerGate).resolveAndGate — the only guest gate (INV-SCENE-64)"
  - "internal/grpc: (*playerGate).ownedCharacter — the only server-side ownership resolution (INV-SCENE-63)"
  - "internal/grpc: sceneGuestDenialMessage constant"
affects:
  - "internal/grpc/sceneaccess_service.go (SceneAccessServer now embeds playerGate)"
  - "plan 04-05 (second facade embeds the same gate)"
  - "plan 04-08 (routing census over surfaces reaching the gate)"

tech-stack:
  added: []
  patterns:
    - "embedded-struct method promotion to keep ~45 call sites byte-identical across an extraction"
    - "per-facade wire message as a constructor parameter with a safe non-empty default"

key-files:
  created:
    - internal/grpc/player_gate.go
    - internal/grpc/player_gate_test.go
  modified:
    - internal/grpc/sceneaccess_service.go
    - internal/grpc/sceneaccess_service_test.go

decisions:
  - "Receiver name stayed `s` (only the receiver TYPE changed) per the task action's verbatim-move mandate, rather than the `g` spelling one acceptance-criterion regex assumed."
  - "Test functions are named Test PlayerGate<Method>… so the plan's own `-run 'PlayerGate'` verify command actually selects them instead of passing vacuously on zero matches."
  - "The guest-denial default lives on `newPlayerGate`, not on the struct zero value, so a miswired facade cannot deny with an empty reason."

metrics:
  duration: 11min
  tasks: 2
  commits: 2
  files: 4
  completed: 2026-08-11

actuals:
  tokens: 5035
  tasks: 2
  commits: 2
---

# Phase 04 Plan 02: Shared `playerGate` Extraction Summary

The guest gate (INV-SCENE-64) and server-side character-ownership resolution
(INV-SCENE-63) now have exactly one definition each, on an embeddable
`playerGate` struct, with the guest-denial wire message parameterized per facade
and all 45 existing `SceneAccessServer` call sites carried unchanged by method
promotion.

## What was built

**Task 1 — the extraction** (`9436fa6d8`)

`internal/grpc/player_gate.go` declares `playerGate` with the three repository
fields moved off `SceneAccessServer` (`playerSessionRepo`, `playerRepo`,
`charRepo`) plus `guestDenialMessage`. Both methods moved verbatim — only the
receiver type changed from `*SceneAccessServer` to `*playerGate`. Every status
code, every message, both `slog.ErrorContext` calls, and the `//nolint:wrapcheck`
directives are unchanged. The one behavioral edit is the guest denial, which now
reads `status.Error(codes.PermissionDenied, s.guestDenialMessage)` instead of the
inline literal.

`newPlayerGate` defaults an empty message to `sceneGuestDenialMessage`
(`"guests cannot access scenes"`), so a miswired facade cannot produce an empty
denial reason. `SceneAccessServer` embeds `playerGate` as a bare field and
`NewSceneAccessServer` builds it internally, keeping its exact positional
signature — all three construction sites (`cmd/holomush/sub_grpc.go`,
`internal/testsupport/integrationtest/harness.go`,
`internal/testsupport/integrationtest/session.go`) compile untouched.
`beginDispatch` stayed on `*SceneAccessServer` as instructed.

**Task 2 — the pins** (`6faff615f`)

`internal/grpc/player_gate_test.go`, 7 test functions / 10 tests, asserted
directly against `*playerGate` with wire-level `status.Code` +
`status.Convert(err).Message()` (no `oops.AsOops` anywhere in the file):

| Behavior | Test |
|---|---|
| guest → `PermissionDenied` + configured message | `TestPlayerGateResolveAndGateDeniesAGuestSessionWithPermissionDeniedAndTheConfiguredMessage` |
| message is per-gate, not hard-coded (3 subtests incl. empty→default) | `TestPlayerGateResolveAndGateGuestDenialMessageIsPerGateRatherThanHardCoded` |
| non-guest → session, nil (positive control) | `TestPlayerGateResolveAndGateReturnsTheResolvedSessionForANonGuestPlayer` |
| unwired session repo → `Unimplemented` | `TestPlayerGateResolveAndGateReturnsUnimplementedWhenTheSessionRepositoryIsNotConfigured` |
| two-way opacity equality (malformed id ≡ non-owned id) | `TestPlayerGateOwnedCharacterReturnsAnIdenticalNotFoundForAnUnparseableIDAndForANonOwnedCharacter` |
| repo failure → `Internal`, message ≠ NotFound message | `TestPlayerGateOwnedCharacterReturnsInternalWithAMessageDistinctFromNotFoundWhenTheCharacterRepositoryFails` |
| owned id → character, nil (positive control) | `TestPlayerGateOwnedCharacterReturnsTheCharacterWhenThePlayerOwnsIt` |

The opacity assertion is deliberately **two-way**, per the plan's HIGH-1
correction: the infrastructure outcome is pinned separately at `codes.Internal`
with a message asserted **not** equal to the NotFound message, so a future change
that masks an outage as NotFound fails here rather than silently breaking the
caller at `sceneaccess_service.go` that branches on `codes.Internal`.

## Verification

| Gate | Result |
|---|---|
| `task test -- ./internal/grpc/ ./internal/web/` **at Task 1's commit, before any Task 2 assertion** | **green (919 tests)** — this is the carve-out's own condition; the pre-existing suite covering all 45 call sites and the 5 web guest-denial assertions passed on the moved code alone, so the move was verbatim |
| `task test -- ./internal/grpc/ ./internal/web/` after Task 2 | green (929 tests) |
| `task test -- -run 'PlayerGate' ./internal/grpc/` | green, 10 tests |
| `task test -- -run 'TestProvenanceGuard' ./test/meta/` | green |
| `task build` | green |
| `task lint` | green (exit 0) |
| `task test:int` | green (exit 0; 11882 tests, 7 pre-existing skips/quarantines) |

Source-state criteria:

- `func (s *playerGate) (ownedCharacter\|resolveAndGate)` in `player_gate.go` → **2 matches**
- `func (s *SceneAccessServer) (ownedCharacter\|resolveAndGate)` in `internal/grpc/` → **0**
- `s.resolveAndGate(` in `sceneaccess_service.go` → **24**; `s.ownedCharacter(` → **21** (unchanged from HEAD)
- `(s.gate|s.playerGate).(resolveAndGate|ownedCharacter)(` → **0** — every call reaches the helpers through the bare `s.` receiver
- `playerSessionRepo|playerRepo|charRepo` in `sceneaccess_service.go` → only inside `NewSceneAccessServer`
- `INV-SCENE-63|INV-SCENE-64` in `player_gate.go` → **4 matches**; none left on the moved methods in `sceneaccess_service.go`
- `rg -o 'guests cannot access scenes' internal/web/ | wc -l` → **5** (unchanged from HEAD)

### Observed RED for the parameterization

Required by Task 2's last criterion. `newPlayerGate`'s empty-message default was
temporarily changed from `sceneGuestDenialMessage` to `"guests are not allowed"`.
Observed failure:

```text
=== FAIL: internal/grpc TestPlayerGateResolveAndGateGuestDenialMessageIsPerGateRatherThanHardCoded/an_empty_message_falls_back_to_the_scene_literal
    player_gate_test.go:90:
        Error: Not equal:
          expected: "guests cannot access scenes"
          actual  : "guests are not allowed"
DONE 10 tests, 2 failures
```

The mutation was reverted (`git diff` on `player_gate.go` empty against the
committed state) and the suite re-run green before the Task 2 commit. The
parameterization is live, not decorative.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `newTestSceneAccessServer` could not compile after the field move**

- **Found during:** Task 1, first `task test` run
- **Issue:** `internal/grpc/sceneaccess_service_test.go:175` builds
  `&SceneAccessServer{playerSessionRepo: …, playerRepo: …, charRepo: …}` as a
  struct literal. Those three fields moved onto the embedded `playerGate`, so
  the package failed to build with three `unknown field` errors.
- **Fix:** switched the helper to
  `playerGate: newPlayerGate(sessionRepo, playerRepo, charRepo, sceneGuestDenialMessage)`.
  This routes the test constructor through the same constructor production uses,
  so the guest-denial literal the grpc-side tests assert is preserved exactly.
- **Files modified:** `internal/grpc/sceneaccess_service_test.go` (not in the
  plan's `files_modified` list — a fourth file was unavoidable)
- **Commit:** `9436fa6d8`

### Plan-text resolutions (no code impact beyond naming)

**2. Acceptance-criterion regex vs. the action's verbatim-move mandate.** Task 1's
first criterion greps `func \(g \*playerGate\)`, but the action says to change
"only the receiver type from `*SceneAccessServer` to `*playerGate`" and "do not
rename locals". Those conflict. The action text was followed — the receiver is
`s`, as it was on `*SceneAccessServer`. The criterion's actual property (exactly
two definitions, both on `*playerGate`) holds and was verified with the receiver
name relaxed.

**3. `IDENT-02` deliberately NOT marked complete.** The plan frontmatter lists
`requirements: [IDENT-02]`, and `gsd-tools query requirements.mark-complete
IDENT-02` did flip the checkbox — but IDENT-02 is *"a player can edit their
character's prose fields … with server-enforced length caps"*, and this plan
ships no mutation surface and no length cap. It contributes the gate those
surfaces will route through; it does not deliver the capability. The flip was
reverted (`git checkout -- .planning/REQUIREMENTS.md`) so the requirement is
marked by the plan that actually ships it (04-06 / 04-07). The tool's own output
corroborated the mismatch: `table_unmatched: ["IDENT-02"]` with
`write_set_complete: false` — the traceability row did not match, so the write
was half-applied anyway.

**4. Test names prefixed with `PlayerGate`.** Task 2's verify command is
`task test -- -run 'PlayerGate' ./internal/grpc/`. Names derived from the
behavior bullets (`TestResolveAndGate…`, `TestOwnedCharacter…`) match zero tests
under that filter, so the command would have passed vacuously. Every test
function is named `TestPlayerGate<Method>…`, which keeps the ACE sentence shape
and makes the plan's own gate load-bearing.

## Notes for downstream plans

- **04-05** embeds `playerGate` the same way `SceneAccessServer` does and passes
  its own `guestDenialMessage` to `newPlayerGate`; both methods promote with no
  call-site ceremony.
- **04-08's** census predicate (a `go/ast` check for a named selector on the
  method's own receiver) applies unchanged to both facades — no call site reaches
  the gate through a named field.
- `CharacterAccessServer` (04-01) keeps its own viewer-rung resolution and was
  deliberately **not** routed through this gate: a public profile read must yield
  a guest rung, whereas this gate denies one. That asymmetry is by design.

## Known Stubs

None. No placeholder values, no TODO/FIXME, no unwired data paths introduced.

## Self-Check: PASSED

- `internal/grpc/player_gate.go` — FOUND
- `internal/grpc/player_gate_test.go` — FOUND
- `.planning/phases/04-shared-facade-helpers-characteraccessservice/04-02-SUMMARY.md` — FOUND
- commit `9436fa6d8` — FOUND
- commit `6faff615f` — FOUND
