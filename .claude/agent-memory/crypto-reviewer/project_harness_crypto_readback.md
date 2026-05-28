---
name: harness-crypto-readback
description: integrationtest harness WithPluginCrypto wiring mirrors production history-reader + readback-decryptor crypto path; how to verify faithful mirror
metadata:
  type: project
---

The `internal/testsupport/integrationtest` harness gained a `WithPluginCrypto()` StartOption (crypto.go) that wires the full plugin-crypto round-trip: emit fence → encrypt publish → audit projection → host history reader read-back + read-back decryptor. Landed under bead holomush-y5inx.8.

**Why:** to exercise the SENSITIVE plugin-owned scene-event decrypt-on-readback path (authorized DEK participant decrypts; non-participant downgrades to metadata-only, no error) end-to-end in an integration test, matching production semantics.

**How to apply (when reviewing future changes to this harness's crypto wiring):**
- Production mirror is `cmd/holomush/sub_grpc.go`: `newHistoryReader` (~785-885) for the reader, the readback-decryptor block (~472-485), and the AuthGuard construction (~340-371). Diff the harness against these.
- Single-guard invariant: `buildHistoryCrypto` (crypto.go) builds `authguard.New(...)`→`NewSessionBridgeGuard` ONCE; both `readerCryptoOptions` (reader) and `configureReadback` (decryptor) must reuse that one `histCrypto.sessionGuard`. A second guard is a finding.
- INV-P7-9 selector identity: ONE `pc.selector` (`cryptowiring.KeySelector()`) must feed emit publisher, plugin consumer manager, AND history reader.
- Known ACCEPTABLE divergence: harness wires `WithHistoryAuth` only (no `WithHistoryAuthAndSourceResolver`/FallbackResolver), so INV-39 cold-tier fallback is inactive. Documented as hot-tier-only readback. Verified non-weakening: `WithHistoryAuth` and `WithHistoryAuthAndSourceResolver` wire identical hot-tier authGuard+DEK; only the cold source resolver differs (tier.go).
- Fail-closed proof: guard-deny → metadata-only frame + `NoPlaintextReasonAuthGuardDeny` + empty payload, no error (`history/dispatcher.go:310-314`). Bare reader (no crypto) returns `EVENTBUS_HISTORY_AUTH_GUARD_NIL` for sensitive reads — never zero-key decrypt (dispatcher.go:287-292).
- DEK seeding: `GetOrCreate` selects active row first, applies `initial` participants ONLY on first mint (manager.go:207-244). So `SeedSceneDEKParticipant` pre-minting with the session participant survives the emit-path `GetOrCreate(ctx,ctxID,nil)`. ctxID `{Type:"scene",ID:sceneID}` must match between seed and `contextIDFromSubject` (publisher.go:498-520).
