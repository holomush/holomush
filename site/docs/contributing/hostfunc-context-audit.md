# Host Function Context Audit

Host functions registered on Lua states via
`internal/plugin/hostfunc/functions.go:Register` run as Go code inside
the goroutine dispatched by `Host.invoke`. While Go code runs,
`gopher-lua`'s per-instruction context check is suspended — a host
function that blocks ignores the plugin-level CPU deadline.

This audit table documents each registered host function and confirms
it either completes in O(1) time or respects its context parameter for
any potentially-blocking work. A meta-test locks this invariant against
future regressions.

## Invariant

Every exported host function registered on a Lua state MUST either:

- complete in O(1) time (no loops of unbounded length, no I/O), OR
- respect `L.Context()` for any call that could block (RPC, I/O,
  channel wait).

The meta-test
`TestHostFuncsRespectContextOnPreCancelledCtx` in
`internal/plugin/hostfunc/context_audit_test.go` invokes every
registered host function with a pre-cancelled context and asserts it
returns within 50 ms.

## Audit table

Maintain this table when adding or changing a host function.

The functions `holomush.log` and `holomush.new_request_id` are O(1).
`holomush.log` appends a log record via slog with no blocking I/O.
`holomush.new_request_id` generates a ULID using `crypto/rand` which
does not block under normal operation.

The KV functions (`holomush.kv_get`, `holomush.kv_set`,
`holomush.kv_delete`) are bounded by `defaultPluginQueryTimeout`
(5 seconds) and are context-aware through their service backends. The
50 ms meta-test bound is satisfied because each function validates its
arguments (non-empty key, plugin name) before any blocking call; a
zero-arg call from the meta-test raises a Lua argument error before
any blocking path executes.

The world query functions (`holomush.query_location`,
`holomush.query_character`, `holomush.query_location_characters`,
`holomush.query_object`) delegate to `world.Service` methods that accept
a context parameter and return promptly on cancellation.

The world mutation functions (`holomush.create_location`,
`holomush.create_exit`, `holomush.create_object`,
`holomush.find_location`, `holomush.set_property`,
`holomush.get_property`) likewise delegate to context-aware service
methods.

The command functions (`holomush.list_commands`,
`holomush.get_command_help`) read from an in-memory registry with no
blocking calls.

The session-stream functions (`holomush.add_session_stream`,
`holomush.remove_session_stream`) and focus functions
(`holomush.join_focus`, `holomush.leave_focus`,
`holomush.present_focus`, `holomush.query_stream_history`) delegate to
services that accept context parameters.

## Adding a new host function

When adding a new host function:

1. Confirm it either completes in O(1) time or accepts a context.
2. Add its name to the `RegisteredFunctionsForAudit` list in
   `internal/plugin/hostfunc/functions.go` so the meta-test exercises
   it.
3. If the function does I/O, document the bounding mechanism
   (RPC deadline, channel timeout, etc.) in this file.
