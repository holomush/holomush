---
name: Always grep for existing RPCs before accepting a "new RPC" proposal
description: HoloMUSH specs proposing a new auth/session/check RPC frequently overlap with one that already exists; always grep proto + handlers
type: feedback
---

When a HoloMUSH spec proposes a new RPC for "is the user signed in",
"check session", "fetch current player", or any read-only auth-state
probe, always grep `api/proto/holomush/{web,core}/v1/*.proto` and
`internal/grpc/auth_handlers.go` + `internal/web/auth_handlers.go`
before accepting the proposal as net-new.

**Why:** The 2026-04-25 multi-tab spec proposed `WhoAmI` without
mentioning `WebCheckSession` / `CheckPlayerSession` which already exist
and serve the same purpose. The spec's §1.3 ("the architecture already
supports the model we want") was undermined by missing this load-bearing
existing RPC. Without flagging the overlap, the plan would have
produced parallel surfaces with no deprecation/migration path.

**How to apply:** During the repo-reality pass, run:
- `rg -n "rpc " api/proto/holomush/web/v1/*.proto`
- `rg -n "rpc " api/proto/holomush/core/v1/*.proto`
- `rg -n "<proposed-method-name>|Check|WhoAmI|Validate" internal/grpc/auth_handlers.go internal/web/auth_handlers.go`

If anything overlaps, the spec MUST either (a) explicitly extend the
existing RPC, or (b) explicitly deprecate it with a migration note for
existing call sites. "Add a new RPC" without addressing the existing
one is a blocking finding.
