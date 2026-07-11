---
quick_id: 260711-hg1
title: "GH-4785: cap gateway ConnectRPC request-body size (unauthenticated OOM)"
issue: 4785
status: complete
date: 2026-07-11
commit: 0e3806ebf
---

# Quick Task 260711-hg1 — Summary

## What changed

`internal/web/server.go` — the public gateway's ConnectRPC handler now caps
inbound request bodies:

1. New package const `maxRequestBytes = 4 * 1024 * 1024` (4 MiB), matching
   `internal/grpc.MaxRecvMsgSize`.
2. `connect.WithReadMaxBytes(maxRequestBytes)` added to the
   `webv1connect.NewWebServiceHandler` options.
3. `http.Server.ReadTimeout = 30s` added alongside the existing
   `ReadHeaderTimeout`.

`internal/web/server_test.go` — new regression test
`TestServerRejectsOversizedRequestBody`.

## Why

connect-go v1.20.0 only applies its body `LimitReader` when `readMaxBytes > 0`;
unset, it buffers the whole request body into memory, so an unauthenticated POST
with a large body could OOM the gateway. Core gRPC already capped inbound at
4 MiB — the public surface did not.

## Verification

- **TDD RED:** before the fix the 5 MiB body was fully read and unmarshaled,
  returning `invalid_argument` / HTTP 400 — proving the buffering vector.
- **TDD GREEN:** after the fix the same body returns `resource_exhausted` /
  HTTP 429 before buffering.
- `task test -- ./internal/web/` → 360 tests pass.
- `task pr-prep` (fast lane) → green (exit 0, "✓ Fast PR checks passed").

## Streaming-safety (verified, not assumed)

`ReadTimeout` does not truncate the long-lived server-streaming `StreamEvents`
RPC. Under HTTP/2 (`x/net/http2` server.go:1987-1990) the connection-wide read
deadline is disarmed after headers and a per-stream timer is armed whose handler
(`onReadTimeout`, server.go:1846-1852) closes only the request **body reader**
(`st.body.CloseWithError`), never the response writer. Server-streaming reads its
single request message before writing responses, so the body is at EOF when the
timer fires and the response stream is unaffected.

## Follow-ups

None. Per-route caps and request-rate limiting are out of scope (separate
surfaces).
