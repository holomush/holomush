---
title: "Host function audit table"
---

Per-function audit of every host function registered on Lua states,
confirming each either completes in O(1) time or respects its context
parameter for potentially-blocking work. For the invariant this table
enforces and the meta-test that backstops it, see
[Host function context audit](/contributing/explanation/hostfunc-context-audit/).
Maintain this table when adding or changing a host function — see
[Add a host function](/contributing/how-to/add-a-host-function/).

| Host function(s)                                                                                                                | Classification     | How the invariant is met                                                                                                                                          |
| ------------------------------------------------------------------------------------------------------------------------------- | ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `holomush.log`                                                                                                                  | O(1)               | Appends a log record via slog with no blocking I/O.                                                                                                               |
| `holomush.new_request_id`                                                                                                       | O(1)               | Generates a ULID using `crypto/rand`, which does not block under normal operation.                                                                                |
| `holomush.kv_get`, `holomush.kv_set`, `holomush.kv_delete`                                                                      | context-respecting | Each derives `context.WithTimeout(L.Context(), defaultPluginQueryTimeout)` with a 5-second backend cap; when the CPU deadline fires, `L.Context()` is already cancelled, so the call returns promptly. |
| `holomush.query_location`, `holomush.query_character`, `holomush.query_location_characters`, `holomush.query_object`            | context-respecting | Delegate to `world.Service` methods that accept a context parameter and return promptly on cancellation.                                                          |
| `holomush.create_location`, `holomush.create_exit`, `holomush.create_object`, `holomush.find_location`, `holomush.set_property`, `holomush.get_property` | context-respecting | Delegate to context-aware service methods.                                                                                                                        |
| `holomush.list_commands`, `holomush.get_command_help`                                                                           | O(1)               | Read from an in-memory registry with no blocking calls.                                                                                                            |
| `holomush.add_session_stream`, `holomush.remove_session_stream`, `holomush.join_focus`, `holomush.leave_focus`, `holomush.present_focus`, `holomush.query_stream_history` | context-respecting | Delegate to services that accept context parameters.                                                                                                              |
