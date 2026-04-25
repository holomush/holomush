# code-reviewer agent memory

This file accumulates HoloMUSH-specific anti-patterns, subtle invariants, and
recurring blind spots discovered during adversarial code review. Entries are
added by the agent itself after completing a review.

Keep under 200 lines. Curate — don't hoard.

## Anti-patterns

- **Stale-base diff illusion**: When reviewing a stack pre-push, always check
  `jj log -r 'main@origin'` head against the branch's fork point. A bare
  `jj diff main@origin..@` will conflate the branch's actual changes with
  upstream-only changes that landed since the branch was forked, producing
  a noisy diff that may suggest the PR touches unrelated files. Use
  `jj diff -r 'fork_point(main@origin | @)..@'` to see the true PR scope.
  Always confirm the branch is current before claiming a "small PR" — the
  rebase gap itself is often a blocking finding ("rebase before push").

- **Wrapped-status `FromError` rewrites Status.Message**: grpc-go's
  `status.FromError` walks the error chain via `errors.As`. When err is
  `oops.Wrap(status.Error(...))`, FromError reaches the inner status BUT
  rewrites its Message to `err.Error()` (the outer chain's stringification).
  This means `st.Message()` and `st.Err().Error()` are NOT necessarily the
  plugin's original message; both can include outer wrapper text. If
  client-visible message purity matters, the inner status must be unwrapped
  explicitly (e.g., via `oops.Unwrap` chain walk) before re-emitting.
  Documented as out-of-scope at `internal/grpc/query_stream_history.go:295`.

- **Comment-only proto reservations.** `// field N is reserved (was X)` without
  an actual `reserved N;` declaration does NOT prevent reuse — proto3 permits
  re-use of any field number that isn't in a real `reserved` block. Search
  pattern: `rg "field [0-9]+ is reserved|reserved [0-9]+;" api/proto/`.
  Project convention uses real `reserved N;` (see
  `api/proto/holomush/core/v1/core.proto:109`,
  `api/proto/holomush/web/v1/web.proto:94`). Pre-existing wart introduced by
  PR #179 (cookie cutover, commit f5473248e) in
  `WebAuthenticatePlayerResponse` and `WebCreatePlayerResponse`.

- **Stale TS regen across stacked proto commits.** When a stack of commits
  edits proto files, `task proto` may not be run by every commit's author,
  leaving the per-language generated bindings out of sync with each other.
  When reviewing a stacked-PR proto change, check whether earlier commits in
  the stack regenerated all generated artifacts (Go, TS, etc.) or only some.

## Invariants worth remembering

- **Top-level oops Code() is the wire-visible code**: client-side error
  classification reads only the OUTERMOST oops node's code via
  `oops.AsOops(err).Code()`. `errutil.AssertErrorCode` walks the chain and
  passes if the code appears anywhere — DO NOT use it for opacity-invariant
  pin tests. Use `oops.AsOops(err).Code()` directly to assert the
  client-visible code (see
  `internal/grpc/query_stream_history_test.go:944` for the canonical pattern).

- **Plugin-status preservation chain**: for `mapHistoryError` to translate
  plugin gRPC codes correctly, every layer between the plugin and the
  handler MUST preserve the gRPC status. The chain is:
  `PluginAuditService.QueryHistory` (plugin) → `pluginHistoryStream.Next`
  (`internal/eventbus/audit/plugin_router.go:158-176`) → `HistoryReader`
  → handler. Each preservation site uses the pattern:
  `if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown { return err }`
  with `//nolint:wrapcheck` justifying the deliberate non-wrap. Adding an
  `oops.Wrap` anywhere in this chain would shadow the code from
  `mapHistoryError`'s `status.FromError` lookup.

- **Proto field-number lifecycle**: deletion → MUST add `reserved N;` AND
  `reserved "field_name";` in the same commit. Comment-only reservation is
  not enforced by `protoc` and not enforced by the project's lint chain.

- **Generated artifacts inventory** for proto changes:
  - `pkg/proto/holomush/<svc>/v1/<svc>.pb.go` (Go)
  - `web/src/lib/connect/holomush/<svc>/v1/<svc>_pb.ts` (TS bindings)
  - Run `task proto` (or `task web:generate`) to regenerate.

- **Diff-scope verification**: for proto-only tasks, `jj diff -r @ --name-only`
  should show only `.proto`, `.pb.go`, and `_pb.ts` files. Anything else is
  scope creep.
