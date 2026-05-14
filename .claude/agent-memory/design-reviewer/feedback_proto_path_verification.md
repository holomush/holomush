---
name: proto-path-verification
description: Spec proto paths require verbatim filesystem verification — authors often abbreviate or guess
metadata:
  type: feedback
---

# Proto-path claims need verbatim filesystem verification

Specs that cite proto paths like `api/plugin/v1/plugin.proto` are often citing the LOGICAL path (`<package>/<file>`) not the on-disk path. In HoloMUSH the convention is `api/proto/holomush/<package>/<file>.proto`. Reverse direction (`pkg/proto/holomush/<package>/<file>.pb.go`) is generated.

**Why:** plan-writers transcribe spec path literally; missing file → either silent fork or task failure.

**How to apply:** every spec that names a `.proto` path → run `Glob api/proto/**/*.proto` AND check the `source:` line in any `*_grpc.pb.go` or `*.pb.go` file in the same generated tree.

Seen 2026-05-13 in event-payload-crypto-phase7-plugin-sdk-design v2: spec cited `api/plugin/v1/plugin.proto` at §3.2 + §4.2 throughout; actual on-disk path is `api/proto/holomush/plugin/v1/audit.proto`.

Related: [[feedback_clean_break_enumerate_callers]]
