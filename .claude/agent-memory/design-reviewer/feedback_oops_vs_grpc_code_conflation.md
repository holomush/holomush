---
name: oops code vs gRPC status code conflation in HoloMUSH specs
description: HoloMUSH specs sometimes describe oops codes as if they were gRPC status codes — flag the wording, but do not block when the design behaves identically to existing pattern
type: feedback
---

# oops code vs gRPC status code conflation in HoloMUSH specs

In HoloMUSH, error codes like `STREAM_ACCESS_DENIED`, `SCENE_AUDIT_ACCESS_DENIED`, `AUDIT_PLUGIN_HISTORY_RPC_FAILED` are **oops codes** (constructed via `oops.Code(...).Errorf(...)`), NOT gRPC `codes.Code` values. The codebase asserts them via `errutil.AssertErrorCode(t, err, "STREAM_ACCESS_DENIED")` which inspects the oops chain in-process.

When a spec says "the wire-level gRPC code is `STREAM_ACCESS_DENIED`" it is using "gRPC code" colloquially to mean "what survives in the error chain through an in-process gRPC test." Over a real network, gRPC would marshal a non-status oops error as `codes.Unknown` and the oops chain would be lost.

**Why:** This is a pre-existing convention in `internal/grpc/query_stream_history.go:170,178,203` and `internal/grpc/query_stream_history_test.go:220` — specs inherit it rather than introduce it. The implementer following such a spec will write code that behaves identically to the existing pattern, and intra-process tests will pass.

**How to apply:** When reviewing specs that mix oops codes with gRPC status code language:

- Flag the wording as a non-blocking finding (medium severity)
- Verify the test the spec proposes is implementable against the existing `errutil.AssertErrorCode` pattern
- Do NOT block — the design works; the wording is the bug
- Suggest concrete spec text fix: e.g., "the error chain carries oops code X" rather than "the wire-level gRPC code is X"
- Real gRPC status codes (`codes.PermissionDenied`, `codes.InvalidArgument`) used in plugin-boundary contexts are correct as written — only flag conflations that affect host-boundary error chains
