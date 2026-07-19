---
phase: 07-event-model-bootstrap-decomposition
reviewed: 2026-07-18T00:00:00Z
depth: standard
files_reviewed: 6
files_reviewed_list:
  - api/proto/holomush/plugin/v1/plugin.proto
  - internal/plugin/goplugin/host.go
  - pkg/plugin/config.go
  - pkg/plugin/config_test.go
  - plugins/core-channels/main.go
  - plugins/core-scenes/main.go
findings:
  critical: 0
  warning: 1
  info: 2
  total: 3
status: issues_found
---

# Phase 07: Code Review Report — Post-Ship Fix (game_id threading)

**Reviewed:** 2026-07-18T00:00:00Z
**Depth:** standard
**Files Reviewed:** 6
**Status:** issues_found (no blockers; one test-coverage warning)

## Summary

This is a small, well-scoped fix: a new `game_id` field on `pluginv1.ServiceConfig`
(proto + generated Go/TS), threaded from `goplugin.Host.Load` (reading the
already-resolved `h.gameID`) into `InitRequest.Config.GameId`, consumed by a new
`pluginsdk.ResolveGameID` helper, and substituted for the two hardcoded `"main"`
literals in `plugins/core-scenes/main.go` and `plugins/core-channels/main.go`.

I traced the full dependency chain rather than trusting the diff at face value:

- Confirmed production wiring actually supplies a real (non-`"main"`-hardcoded)
  `gameID` into `goplugin.WithGameID(gameID)` at
  `internal/plugin/setup/subsystem.go:355`, sourced once from `s.cfg.GameID()`
  at line 192-195 and fed to both the Lua (`hostfunc.WithGameID`) and binary
  hosts — so the new `GameId: h.gameID` in `host.go:1021` is reading a real
  value in production, not a config default.
- Confirmed `h.gameID` is read directly (not via the `RLock`-guarded `GameID()`
  accessor) inside `Load()`, which is safe because `Load()` already holds
  `h.mu.Lock()` for its full body (`host.go:797-798`) — no double-lock, no
  torn read.
- Confirmed `ResolveGameID`'s nil/empty-string fallback to `"main"` is
  defensively correct for both `nil` `*ServiceConfig` and a zero-value one
  (test-harness construction), and is the *only* production call site of
  `pluginv1.ServiceConfig{...}` in the whole repo
  (`internal/plugin/goplugin/host.go:1012`) — so there is no second, unfixed
  hardcoded-`"main"` construction site left behind.
- Confirmed the regenerated `pkg/proto/holomush/plugin/v1/plugin.pb.go` and
  `web/src/lib/connect/holomush/plugin/v1/plugin_pb.ts` are consistent with the
  `.proto` change (field 5, `string`, correctly reflected in both the raw
  descriptor bytes and the generated struct/getter), satisfying the
  generated-code-must-ship-with-schema-change rule.
- Confirmed the new proto field doc comment is grounded (explains the actual
  `gameIDProvider` resolution path and the silent-divergence failure mode) and
  is not a name-echo, satisfying `.claude/rules/proto-doc-comments.md`.

No bugs, security issues, or logic errors found in the diff itself. The one
substantive gap is a missing regression test at the layer that actually wires
the new field through `Load()` (see WR-01) — the exact class of gap that
produced the original silent-failure bug this change fixes.

## Warnings

### WR-01: `Host.Load` threading `GameId` into `InitRequest.Config` has no direct test

**File:** `internal/plugin/goplugin/host.go:1011-1023`
**Issue:** The new `GameId: h.gameID` line in the `InitRequest.Config` literal
has no test asserting the host actually threads it through. `pkg/plugin/config_test.go`
thoroughly tests `ResolveGameID`'s own fallback logic in isolation, but nothing
in `internal/plugin/goplugin/host_test.go` constructs a `Host` via
`WithGameID(...)`/`WithCA(...)`, calls `Load`, and asserts
`grpcClient.initReq.Config.GetGameId()` equals the configured game id.

This matters because the codebase already has an established pattern for
exactly this shape of assertion — `mockGRPCPluginClient` captures `initReq` in
`host_test.go`, and there are multiple existing tests following it for the
sibling field `DeclaredCapabilities` (`TestLoadPassesDeclaredCapabilitiesToInit`
at `host_test.go:2166`, plus the grant-scoped variant at `host_test.go:2214`
and `host_grants_internal_test.go`). `GameId` is the newer sibling field added
in this exact diff and has zero hits for `GameId`/`gameID`/`GameID` anywhere in
`host_test.go` (verified via `rg`).

The original bug this fix addresses ("neither side errors" — a silent subject
mismatch) was invisible precisely because nothing asserted the host→plugin
value transfer end-to-end. Landing the fix without a test at that same seam
reintroduces the same blind spot for any future regression (e.g., someone
reordering the `Config{}` literal fields, or a future refactor of `Load`
dropping the assignment) — `go vet`/`task test` would stay green while the
field silently stops being threaded.

**Fix:** Add a test mirroring `TestLoadPassesDeclaredCapabilitiesToInit`:

```go
func TestLoadPassesGameIDToInit(t *testing.T) {
	factory := newMockClientFactory()
	h := NewHostWithFactory(factory, WithGameID("01KXVJHWPYXZ9NGPBC3V0C9WD0"))
	// ... load a manifest as the existing DeclaredCapabilities test does ...
	require.NoError(t, h.Load(ctx, manifest, dir))

	grpcClient := factory.lastClient() // or however the existing test recovers it
	require.NotNil(t, grpcClient.initReq, "expected InitRequest to be set")
	assert.Equal(t, "01KXVJHWPYXZ9NGPBC3V0C9WD0",
		grpcClient.initReq.Config.GetGameId(),
		"InitRequest.Config.GameId must carry the host's resolved game id")
}
```

Also worth a companion assertion (or a subtest) that a `Host` constructed
*without* `WithGameID`/`WithCA` sends an empty `GameId` (documenting the
test-harness fallback contract that `ResolveGameID` relies on).

## Info

### IN-01: Duplicated multi-line rationale comment across two plugin `main.go` files

**File:** `plugins/core-scenes/main.go:250-254`, `plugins/core-channels/main.go:294-298`
**Issue:** Both `Init` methods carry a near-identical 5-line comment explaining
why `pluginsdk.ResolveGameID(config)` replaced the hardcoded `"main"` literal.
This is consistent with the existing pattern in this codebase (both plugins
already duplicated the *previous* hardcoded-`"main"` comment before this fix),
so it's not a regression, but the duplication compounds with every future
touch to this call site.
**Fix:** Non-blocking. If a third binary plugin adopts the same pattern,
consider consolidating the rationale into the `pluginsdk.ResolveGameID` doc
comment (which already carries the full rationale in `pkg/plugin/config.go`)
and shrinking each call site to a one-line pointer comment.

### IN-02: No Lua-side equivalent exists for plugins that might build fully-qualified subjects directly

**File:** N/A (absence noted while cross-checking `.claude/rules/plugin-runtime-symmetry.md`)
**Issue:** This fix is binary-plugin-only because today's Lua host
(`internal/plugin/lua/host.go`, `internal/plugin/hostfunc`) has no in-tree Lua
plugin that constructs a fully-qualified `"events.<game_id>...."` subject
directly the way `core-scenes`/`core-channels` do for their own audit-subject
reads (`dotStyleSceneSubject`/`dotStyleChannelSubject`). `hostfunc.WithGameID`
already exists but serves a different purpose (qualifying domain-relative
stream refs for the `QueryStreamHistory` ABAC check, per
`internal/plugin/goplugin/host.go:140-148`'s doc comment on `WithGameID`).
This is a **permitted asymmetry** under the plugin-runtime-symmetry rule (no
current Lua caller hits the same trust/policy surface), not a violation — but
flagging it since the failure mode is the exact "silent, no-error-either-side"
class this whole fix exists to close. If a future Lua plugin needs to build a
direct qualified subject (rather than emitting a domain-relative ref for the
host to qualify), there is currently no `pluginsdk`-equivalent helper exposed
to Lua for resolving the host's game id from that construction site.
**Fix:** No action required now. Worth a one-line note in
`.claude/rules/plugin-runtime-symmetry.md`'s "permitted asymmetry" catalogue
(or the `holomush-dj95.10` tracking item) the next time someone touches that
file, so a future Lua plugin author doesn't rediscover this the hard way.

---

_Reviewed: 2026-07-18T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
