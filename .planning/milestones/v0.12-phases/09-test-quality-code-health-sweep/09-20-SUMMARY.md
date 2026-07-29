---
phase: 09-test-quality-code-health-sweep
plan: 20
subsystem: test-harness
tags: [QUAL-04, integration-harness, session-lifecycle, seams]
status: complete
requires:
  - 09-07 (EmitDirectEventAt seam; depguard pinning of integrationtest)
provides:
  - "AuthedPlayer.OpenTelnetSession — a real telnet session opener (was a t.Fatalf stub)"
  - "Server.DetachAndExpireSession — a session row the production reaper actually selects"
  - "WithBuiltinCommands — compiled-in command handlers on the harness's default registry"
  - "Server.cmdRegistry — the effective command registry, inspectable without plugins"
  - "Administrator-boot row disposition for the session-lifecycle matrix"
affects:
  - 09-12
  - 09-13
  - 09-14
  - 09-15
tech-stack:
  added: []
  patterns:
    - "Every seam is proven by a negative control: break the production behaviour and confirm the spec fails, attributed by name"
    - "Assert production-observable state (a DB column, a deleted row), never the test's own input or the absence of an error"
key-files:
  created:
    - internal/testsupport/integrationtest/builtin_commands_test.go
  modified:
    - internal/testsupport/integrationtest/session.go
    - internal/testsupport/integrationtest/harness.go
    - test/integration/privacy/privacy_test.go
    - .planning/phases/09-test-quality-code-health-sweep/deferred-items.md
decisions:
  - "Telnet differentiation is threaded through attach's SubscribeRequest only, not SelectCharacter — the connection row is where production observes the difference"
  - "WithInTreePlugins wins over WithBuiltinCommands: register before adoption, so setting both cannot double-register or panic"
  - "The administrator-boot row is dispositioned not-implementable-from-harness-defaults, with resetpassword --kick named as the path that DOES exist"
metrics:
  duration: 38m
  tasks: 3
  files: 5
  completed: 2026-07-26
---

# Phase 09 Plan 20: QUAL-04 Harness Seams Summary

Three harness seams that the session-lifecycle matrix plans assume — telnet transport
identity, a reaper-selectable expired session, and a dispatchable compiled-in command —
each built to drive the real production path and each proven by a negative control that
makes its spec fail when the seam is removed.

## What each seam does, and does NOT, cover

**Read this section before writing 09-12/13/14/15.** Each seam reaches a specific
production path; none of them reaches more than that.

### Seam 1 — telnet client type

```go
func (p *AuthedPlayer) OpenTelnetSession(ctx context.Context) *Session   // session.go:942
func (p *AuthedPlayer) OpenWebSession(ctx context.Context) *Session      // session.go:920
func (p *AuthedPlayer) openSession(ctx context.Context, clientType string) *Session // session.go:953
```

Both openers delegate to one `openSession` body; they differ only in the client type.
`Session` gained an unexported `clientType` field which `attach` sends on the
`SubscribeRequest`, falling back to `"terminal"` when unset — so `ConnectAuthedWithRoles`
and `ConnectGuest` are byte-identical on the wire to before.

- **Covers:** the client type the production Subscribe handler stamps on the
  `session_connections` row (`internal/grpc/subscribe_handler.go:358-364`), validated
  against the production allowlist by the session store
  (`internal/store/session_store.go:519-528`). This is what grid-presence roster queries
  filter on (`session_store.go:637`, `:752`), so it decides who is visible to whom.
- **Does NOT cover:** any telnet *protocol* behaviour. There is no telnet gateway in the
  loop — `internal/telnet` is not exercised. The session is opened through the same
  `SelectCharacter` RPC as a web session, and `SelectCharacterRequest.ClientType` is left
  unset for both openers (behaviourally identical: `auth_handlers.go:388` only
  special-cases `comms_hub`). The difference is the connection row and nothing else.
- **How to assert:** read `client_type` from `session_connections` keyed by session id.
  A spec phrased against the argument passed to the opener proves nothing. The helper
  `connectionClientTypes(ctx, ts, sessionID)` in the privacy suite does this.

### Seam 2 — a session row the reaper actually selects

```go
func (s *Server) DetachAndExpireSession(ctx context.Context, sessionID string, expiresAt time.Time) // harness.go:1080
```

- **Covers:** the exact state `ListExpired`'s predicate matches —
  `status = 'detached' AND expires_at < now` (`internal/store/session_store.go:445-452`).
  `expiresAt` is a caller-supplied parameter, so no sleep is needed to make the row
  eligible. `detached_at` is preserved when the row already reached detached status
  through the production Disconnect RPC.
- **Does NOT cover:** the lease sweep (`reapLapsedConnections`). That path is driven by
  backdating `session_connections.last_seen_at` and needs `LeaseTTL` + `BootGrace` on the
  reaper config; the precedent is `test/integration/presence/lease_reaper_test.go:55-135`.
- **The trap this replaces:** `Server.ExpireSession` (unchanged, still has callers) sets
  `status = 'expired'`, which `ListExpired` can never match. Its doc comment now says so
  explicitly and points here. **Never seed a reaper assertion through `ExpireSession`.**

### Seam 3 — dispatchable compiled-in commands

```go
func WithBuiltinCommands() StartOption   // harness.go:277
```

Registers `handlers.RegisterAll` — exactly `quit` and `shutdown` — onto the default
registry, without requiring built binary plugin artifacts.

**Interaction rule with `WithInTreePlugins`: the plugin option WINS.** Registration
happens on the default registry *before* the plugin subsystem's registry is adopted
wholesale (`cmdRegistry = pluginSub.CommandRegistry()`). The adopted registry already
carries the same compiled-in handlers, because the plugin subsystem calls `RegisterAll`
on its own registry. Setting both options is therefore safe in either order and cannot
double-register or panic; the registrations made by the narrow option are simply
discarded. Proven by `TestWithBuiltinCommandsComposesWithInTreePlugins`, which runs both
orderings.

`Server` also gained an unexported `cmdRegistry` field holding the *effective* registry on
every path, because the existing `CommandRegistry()` accessor panics without plugins.

- **Does NOT cover:** admin commands. `RegisterAdmin` is deliberately not called — see the
  administrator-boot disposition below.

**Critical for anyone writing a command spec:** an unknown command is a *user-facing*
error, so `HandleCommand` emits a `command_response` and still answers `Success=true`
(`internal/grpc/command_handler.go:291-302`). `SendCommand` therefore returns **no error**
for an unregistered command. A spec asserting only "SendCommand succeeded" passes against
the empty registry and proves nothing. Assert a production effect instead — the quit spec
asserts the session row is deleted (`command_handler.go:267-289`).

## Termination transitions: the production entry point for each

| Transition | Production entry point | Drivable from harness defaults? |
|---|---|---|
| **quit** | compiled-in `quit` command via `Session.SendCommand`; `QuitHandler` returns `ErrSessionEnded`, the server emits leave + `session_ended` (cause quit), deletes the row, fires disconnect hooks (`internal/grpc/command_handler.go:267-289`) | Yes, with `WithBuiltinCommands()` |
| **logout** | not a command — the `Logout` RPC, already wrapped by `Session.Logout` (`session.go`), which tears down the transport then calls `coreServer.Logout` | Yes, no option needed |
| **administrator boot** | `resetpassword --kick` (`internal/command/handlers/resetpassword.go:197-218`) | **No** — see below |

## Administrator-boot row disposition

**Disposition: `not-implementable-from-harness-defaults`.**

Recorded in the form the plan pinned:

> not-implementable from harness defaults without admin wiring — see
> `resetpassword --kick` (`internal/command/handlers/resetpassword.go:197-218`), which is
> a real, capability-gated administrator session-boot path but which bypasses the
> `RecordBootedSession` / `session_ended` semantics the matrix row asserts.

Both halves are load-bearing. **An administrator session-boot capability EXISTS in this
tree.** Any statement that none exists is false.

### The capability-spelling search, run and recorded

The capability is spelled `kick`, not `boot`. `rg -n 'kick' internal/command/handlers/`
returns matches, including:

```
internal/command/handlers/resetpassword.go:27:	resetPasswordUsage       = "resetpassword <username> [password] [--kick]"
internal/command/handlers/resetpassword.go:35:	capSessionKick = command.Capability{Action: "admin", Resource: "session", Scope: command.ScopeGlobal}
```

It is capability-gated (`capSessionKick` checked at `resetpassword.go:124`), registered
through `RegisterAdmin` (`internal/command/handlers/register.go`), and already unit-tested
(`resetpassword_test.go:163`, "reset with --kick terminates game sessions").

### Gap 1 — semantic (remedy: production behaviour change)

`--kick` calls `exec.Services().Session().DeleteByCharacter` once per character
(`resetpassword.go:209`). That is a raw `DELETE ... RETURNING`
(`internal/store/session_store.go:813-827`) which emits nothing. So `--kick` delivers the
row-deleted half of the matrix row's assertion and **not** the `session_ended` half: no
`STREAM_CLOSED` reaches subscribers and no disconnect hooks fire.

The correct semantics live only on the plugin-originated path — `dispatchToPlugin` records
each booted session (`internal/command/dispatcher.go:543`) and the server then emits leave,
emits `session_ended` with cause `kicked`, deletes the row, and fires hooks
(`internal/grpc/command_handler.go:307-355`). Note additionally that **no in-tree plugin
currently registers a `boot` command** (`rg -n 'boot' plugins/*/plugin.yaml` finds only an
unrelated comment), so those semantics are presently unreachable from any shipped command.

**Filed as issue #4862.** It concerns the `session_ended` semantic gap — *not* a missing
entry point. Existing issues were searched first; the closest is **#576** ("Admin boot via
`DeleteByCharacter` skips leave event and disconnect hooks"), which is CLOSED and covered
the now-deleted `internal/command/handlers/boot.go`. The `resetpassword --kick` caller was
never migrated with it, so the same defect survives in a second command. No open issue
covered it.

### Gap 2 — harness wiring (remedy: harness wiring, separate from Gap 1)

Independently of semantics, the path is unreachable from harness defaults.
`RegisterAdmin` panics on any nil dependency (`register.go:14-23`) and requires five the
harness does not wire: `PlayerRepo`, `Hasher`, `PlayerSessions`, `ResetRepo`, `CharLister`.
`WithBuiltinCommands` therefore calls `RegisterAll` only.

These two gaps have different remedies and must not be conflated: closing Gap 2 alone
would make the command *reachable* while still delivering the wrong semantics, so a spec
written against it would go green on a transition the system does not actually perform.

**Do not** write an approximating spec for this row, and do not repurpose logout or a
direct row delete as a stand-in — either produces a green cell for semantics the system
does not deliver.

## Negative controls performed

All three were run and observed; each produced **exactly one** failure, attributed by name
to the spec's own assertion.

| Seam | Break applied | Observed failure |
|---|---|---|
| Telnet | `OpenTelnetSession` passes `clientTypeTerminal` | `Expected <["terminal"]> to consist of <["telnet"]>` — the DB read, not the argument, decides |
| Reaper | seed via the old `ExpireSession` instead of the new seam | `Timed out after 8.001s ... Expected an error to have occurred. Got: <nil>` — the row was never reaped |
| Commands | drop `WithBuiltinCommands()` from `Start` | `quit MUST have reached QuitHandler ... Expected an error to have occurred. Got: <nil>` — **and `SendCommand` itself returned no error**, confirming a no-error assertion would have been vacuous |

Each break was reverted immediately and the suite re-run green.

A fourth, permanent negative control ships as a test:
`TestDefaultStartLeavesTheCommandRegistryEmpty` asserts `quit` is absent by default, so the
positive registration assertion cannot silently become vacuous later.

## Deviations from Plan

**1. [Rule 3 — blocking] Task 1's `persisted.LocationArrivedAt` guard could never pass.**

- **Found during:** Task 1 acceptance checks.
- **Issue:** the criterion required `rg -c 'persisted.LocationArrivedAt' …/session.go` to
  return 1 as proof the opener body was not duplicated. It returns 2 — and returned 2
  *before* this plan (`git show 02f2a950d:…` confirms). The second occurrence is
  `RefreshFromPersisted` (`session.go:375`), an unrelated pre-existing method. The guard as
  written fails on a correct implementation, i.e. it is unfalsifiable in the wrong
  direction.
- **Fix:** replaced with a falsifiable equivalent — the count must not *increase* (2 → 2),
  and exactly one occurrence must sit inside an opener body (line 980, shared by both
  openers). The plan's actual intent, no duplication of the persisted-row sourcing logic,
  is satisfied and verified.

**2. [Rule 2 — missing critical functionality] Added `Server.cmdRegistry`.**

- **Found during:** Task 3, writing the both-options guard.
- **Issue:** the acceptance criterion requires proving a suite setting both options starts
  without panicking *and* that `quit` is dispatchable — but the existing
  `Server.CommandRegistry()` accessor panics unless `WithInTreePlugins` was passed, so
  there was no way to inspect the effective registry on the default path.
- **Fix:** added an unexported `cmdRegistry` field to `Server`, populated on every path.
  Additive; no existing accessor changed.

**3. Scope note — two unrelated flakes observed, not fixed.**

Two full-lane `task test:int` runs each failed one audit-projection `Eventually`
assertion, in different specs (`internal/eventbus/audit` DLQ metric;
`cmd/holomush` `admin_read_stream_e2e_test.go:889`). Both passed in isolation, neither
package imports `internal/testsupport/integrationtest` (verified: `rg -q` exits 1), and a
third full-lane run over the same tree passed clean (exit 0, 10836 tests). Logged to
`deferred-items.md`; out of scope per the scope boundary, and not quarantined since neither
reproduces.

## Verification

| Gate | Result |
|---|---|
| `task test:int` (full lane) | exit 0 — 10836 tests, 7 skipped |
| `task test` | exit 0 — 10411 tests, 4 skipped |
| `task lint` | exit 0 |
| `task fmt` | exit 0; mutations committed |
| `task test:int -- ./test/integration/privacy/...` | exit 0 |
| New specs actually registered | confirmed via `-ginkgo.json-report`: 3 QUAL-04 specs, all `passed` (spec count 18 → 21) |
| New harness tests actually ran | confirmed 5 tests, **0 skipped**, so the plugin-gated both-options test was not silently skipped |

Spec counts were read from the Ginkgo JSON report rather than parsed from
`gotestsum --format pkgname` output, which prints no spec counts and so cannot detect a
spec silently vanishing (09-11 finding).

## Self-Check: PASSED

- `internal/testsupport/integrationtest/builtin_commands_test.go` — FOUND
- `internal/testsupport/integrationtest/session.go` — FOUND
- `internal/testsupport/integrationtest/harness.go` — FOUND
- `test/integration/privacy/privacy_test.go` — FOUND
- Commit `988bd4c4b` — FOUND
- Commit `56b3fedb9` — FOUND
- Commit `b61ee1838` — FOUND
- Issue #4862 — created

## Known Stubs

None. `OpenTelnetSession`'s `t.Fatalf` stub was the one stub in scope and is now a real
implementation. No new stub, placeholder, or skipped test was introduced.

## Note on QUAL-04 status

QUAL-04 was **deliberately left `Pending`** in `REQUIREMENTS.md`. This plan delivers the
seams only; the requirement's actual subject — "a session-lifecycle test matrix covers the
connect / reconnect / multi-character / idle-timeout paths" — is delivered by 09-12, 09-13,
09-14 and 09-15, all still incomplete, which carry the same requirement id in their
frontmatter. The per-plan `requirements.mark-complete` call flipped it to Complete; that
was reverted, because a requirement marked done while four of its five plans are unwritten
is precisely the overstated-assurance artifact this phase exists to eliminate. The last of
09-12..09-15 to land should mark it.
