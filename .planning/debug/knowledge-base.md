# GSD Debug Knowledge Base

Resolved debug sessions. Used by `gsd-debugger` to surface known-pattern hypotheses at the start of new investigations.

---

## e2e-scene-pose-regression — binary plugins hardcode game_id="main", silently diverging from the host's real per-database game id after gameIDProvider was wired
- **Date:** 2026-07-18
- **Error patterns:** scene log, pose, publish-vote, timeout, expect(locator).toBeVisible() failed, JetStream subject mismatch, events.main, ServiceConfig, game_id, eventbus.Qualify, no error logged, event never arrives, RPC succeeds but no delivery
- **Root cause:** Two binary plugins (`plugins/core-scenes`, `plugins/core-channels`) hardcode `gameID = "main"` at Init because `pluginv1.ServiceConfig` carried no `game_id` field. Before commit 255c46fa6, the EventBus subsystem's own `GameID` was also never wired to the DB-resolved id (stayed at the literal `"main"` default), so publish/subscribe subjects accidentally agreed. 255c46fa6 correctly wired EventBus to resolve the real per-database game id, which broke the accidental agreement — plugins kept publishing on `events.main.scene.<id>...` while the host correctly subscribed on `events.<real-game-id>.scene.<id>...`. `eventbus.Qualify()` treats any subject already starting with `"events."` as pre-qualified and passes it through verbatim, so the mismatch was permanent and silent (JetStream never routes non-matching-subject messages; neither side logs an error).
- **Fix:** Added `game_id` field to `pluginv1.ServiceConfig` proto; `internal/plugin/goplugin/host.go`'s Init-request construction now populates `Config.GameId: h.gameID` (the same gameIDProvider-resolved value already used for mTLS cert SANs); added `pluginsdk.ResolveGameID(config)` helper (falls back to `"main"` when unset, for test harnesses); `plugins/core-scenes/main.go` and `plugins/core-channels/main.go` replaced their hardcoded `gameID = "main"` with `pluginsdk.ResolveGameID(config)`.
- **Files changed:** api/proto/holomush/plugin/v1/plugin.proto, pkg/proto/holomush/plugin/v1/plugin.pb.go, web/src/lib/connect/holomush/plugin/v1/plugin_pb.ts, pkg/plugin/config.go, pkg/plugin/config_test.go, internal/plugin/goplugin/host.go, plugins/core-scenes/main.go, plugins/core-channels/main.go
---
