<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Phase 5 Sub-Epic C — UDS Admin-Socket Substrate Design

## Status

Draft.

## Authors

Sean Brandt; brainstormed with Claude.

## Date

2026-05-09

## Context

This document specifies the UDS admin-socket substrate (sub-epic C of
`holomush-jxo8`, Phase 5 of `holomush-e49r`). Sub-epic C ships the
transport layer that sub-epics D, E, and F register their RPCs against.
It ships zero operational endpoints — only a liveness probe (`Status`
RPC) and the plumbing sub-epics D/E/F need.

Parent decomposition: `docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-decomposition.md`,
Decision 4 and sub-epic C row.

Master spec: `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`
§6.3 and §7.5 (transport requirements).

## Goals

- ConnectRPC server bound exclusively to a UNIX domain socket — never
  TCP, never network-exposed.
- `SO_PEERCRED` middleware that captures peer identity (uid, gid, pid)
  into the request context for audit enrichment by downstream handlers.
- Lifecycle wiring in `cmd/holomush/` via the existing `Subsystem`
  interface.
- `admin.v1` proto skeleton with a single `Status` RPC.
- Platform support: full peer-cred capture on Linux and Darwin; graceful
  no-op on other platforms.

## Non-Goals

- Per-RPC handlers for Rekey (sub-epic E) or AdminReadStream (sub-epic F).
- `OperatorAuthProvider` authentication check sequence (sub-epic D).
- `@cluster status` / `@evict-member` admin commands (`holomush-jxo8.1`,
  P3 follow-up; remains an open child of sub-epic C after this substrate
  lands).
- Any TCP-exposed admin endpoint.
- Authentication or authorization on the socket — `SO_PEERCRED` capture
  is audit enrichment only, not a defense factor.
- Hot-reload of socket configuration — restart-only.

## Design

### Socket path

The socket lives in the XDG runtime directory:

```text
{xdg.RuntimeDir()}/admin.sock
```

Which resolves to:

- Linux: `$XDG_RUNTIME_DIR/holomush/admin.sock` (or
  `~/.local/state/holomush/run/admin.sock` when `$XDG_RUNTIME_DIR` is
  unset)
- macOS dev: `~/.local/state/holomush/run/admin.sock`
- Container: set `XDG_RUNTIME_DIR` explicitly for a predictable path

`xdg.RuntimeDir()` already exists; no new helper needed. The directory
is created with mode `0700` via `xdg.EnsureDir()` before bind. The
`0700` directory is the primary access control gate: only the owning
user can enter it. After `net.Listen` returns, `os.Chmod(0o600)` is
called on the socket file as a supplementary restriction.
`os.Chmod` is used rather than `umask` to avoid process-wide state
mutation; the `0700` parent directory closes the brief window between
socket creation and the `chmod` call.

Operators running HoloMUSH as a systemd system service should set
`Environment=XDG_RUNTIME_DIR=%t/holomush` in the unit file, or accept
the `~/.local/state/holomush/run` fallback.

A companion lock file at `{xdg.RuntimeDir()}/admin.lock` provides
stale-socket detection (see [Stale-socket recovery](#stale-socket-recovery)).

### Stale-socket recovery

The substrate uses `flock(2)` — not a PID-file `kill` check — as the
liveness primitive. The reason: `kill(pid, 0)` returns `nil` for any
live process, not just holomush. After a hard crash (SIGKILL, power
loss), the OS reuses PIDs freely; the PID written to a `.pid` file may
belong to an unrelated process within seconds of a restart. `flock`
avoids this: the kernel drops all flocks held by a process when it dies,
regardless of cause, and the lock is tied to the open file descriptor,
not to a PID number.

**`admin.lock` lifecycle:**

On `Start()`:

1. Open (or create) `admin.lock` for read/write.
2. Attempt a non-blocking exclusive `flock(fd, LOCK_EX|LOCK_NB)`.
3. If `flock` succeeds → no live holomush holds the lock; proceed:
   a. Remove stale `admin.sock` if present (log a warning if it was).
   b. Bind `net.Listen("unix", socketPath)`.
   c. Hold the `admin.lock` fd open for the server's lifetime.
4. If `flock` fails with `EWOULDBLOCK` → another instance is live →
   return `ErrAdminSocketAlreadyHeld`.

On `Stop()`:

1. `httpServer.Shutdown(ctx)`.
2. Remove `admin.sock`.
3. Close the `admin.lock` fd (flock released atomically by the kernel).

`admin.lock` is NOT removed on `Stop()`. Removing it would introduce a
window where a concurrent fast-restart's `open + flock` lands on a
soon-to-be-unlinked inode, defeating the liveness guarantee. The file
persists as a permanent fixture in `xdg.RuntimeDir()`. Its presence is
harmless when no server is running; the flock, not the file, is the
authoritative liveness signal.

The `admin.lock` fd MUST be kept open until `Stop()`. Closing it early
(e.g., via deferred cleanup before `Stop`) drops the flock prematurely.

**Remaining race:** two concurrent `Start()` calls can both acquire the
flock before either reaches `net.Listen`. The second `net.Listen` will
fail with `EADDRINUSE` — a loud, correct failure. The main gRPC port
bind provides a second independent guard against accidental double-start.

### Server shape

`internal/admin/socket/server.go` borrows the `Server` struct shape and
`Start`/`Stop` method signatures from `internal/web/server.go`,
substituting:

- `net.Listen("unix", socketPath)` for `net.Listen("tcp", addr)`
- `PeerCredMiddleware` as the sole outer middleware

ConnectRPC's unary, server-streaming, and client-streaming protocols all
work over HTTP/1.1 (only bidirectional streaming requires HTTP/2).
Sub-epic F's AdminReadStream is server-streaming; HTTP/2 is not required
and is not configured here.

Handler chain (outermost to innermost):

```text
PeerCredMiddleware → mux (ConnectRPC handlers)
```

Security-headers, CORS, and cookie middleware from `internal/web/` are
NOT applied — this socket is not browser-facing.

The `ConnContext` field on `http.Server` is set to extract the
`*net.UnixConn` from the `net.Conn` and store it on the request context
before handlers run. This is how `PeerCredMiddleware` obtains the raw
file descriptor for peer-credential syscalls.

### SO_PEERCRED middleware

```go
// PeerCred holds the peer process identity captured from the UDS connection.
type PeerCred struct {
    UID uint32
    GID uint32
    PID int32
}
```

Three platform-conditional files share one function signature:

```go
// readPeerCred reads peer credentials from a UNIX domain socket connection.
func readPeerCred(conn *net.UnixConn) (PeerCred, error)
```

| File | Build constraint | Implementation |
| ---- | ---------------- | -------------- |
| `peercred_linux.go` | `//go:build linux` | `unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)` — single syscall returning pid+uid+gid |
| `peercred_darwin.go` | `//go:build darwin` | uid+gid: `unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)` (returns `*unix.Xucred`; `Uid` field + `Ngroups/Groups[0]`); pid: `syscall.GetsockoptInt(fd, int(unix.SOL_LOCAL), int(unix.LOCAL_PEERPID))` (stdlib `syscall`, constants from `golang.org/x/sys/unix`) — two calls |
| `peercred_other.go` | `//go:build !linux && !darwin` | Returns zero `PeerCred` and a descriptive error; logs a one-time `slog.Warn` at server startup |

`golang.org/x/sys` is already in the module graph (`v0.43.0 // indirect`).
Adding a direct import will cause `go mod tidy` to promote it to a
direct dependency line — a one-line `go.mod` change, no new download.

The middleware stores `PeerCred` in the request context under a
package-private key. Downstream handlers retrieve it via:

```go
cred, ok := socket.PeerCredFromContext(ctx)
```

If peer-cred was unavailable (`!linux && !darwin`), `ok` is `false`.
Handlers MUST treat a missing cred as absent rather than panicking.
Sub-epic D's audit enrichment path will log a warning when `ok` is
`false`.

### admin.v1 proto

`api/proto/holomush/admin/v1/admin.proto`:

```protobuf
syntax = "proto3";

package holomush.admin.v1;

option go_package = "github.com/holomush/holomush/pkg/proto/holomush/admin/v1;adminv1";

service AdminService {
  rpc Status(StatusRequest) returns (StatusResponse);
}

message StatusRequest {}

message StatusResponse {
  string version = 1;
  bool healthy   = 2;
}
```

`version` is the server binary version string (the package-level
`version` var in `cmd/holomush/`, set via `-X` ldflag), threaded into
`AdminSocketSubsystemConfig.Version` at construction time.

`healthy` reports `true` iff the admin-socket HTTP server is accepting
requests (i.e., `Start()` completed without error). It does NOT
aggregate upstream-subsystem health — that is the role of the
`ReadinessRegistry`. `Status` is an admin-socket liveness probe, not a
full-system health check.

Generated output layout (matching existing convention):

```text
pkg/proto/holomush/admin/v1/
  admin.pb.go              ← protoc-gen-go (messages/types)
  admin_grpc.pb.go         ← protoc-gen-go-grpc (gRPC binding)
  adminv1connect/
    admin.connect.go       ← protoc-gen-connect-go (ConnectRPC binding)
```

The raw gRPC binding is generated but unused at sub-epic C time —
consistent with how `web.v1` is handled.

### Lifecycle wiring

**`internal/lifecycle/subsystem.go`** — add:

```go
SubsystemAdminSocket // admin_socket
```

After `SubsystemCluster`. Regenerate `subsystemid_string.go` via
`go generate ./internal/lifecycle/...`.

**`internal/admin/socket/subsystem.go`** — new file:

```go
type AdminSocketSubsystemConfig struct {
    SocketPath string // derived from xdg.RuntimeDir() + "/admin.sock"
    LockPath   string // derived from xdg.RuntimeDir() + "/admin.lock"
    Version    string // binary version string for StatusResponse (package-level version var in cmd/holomush/)
}

type AdminSocketSubsystem struct { ... }

func NewAdminSocketSubsystem(cfg AdminSocketSubsystemConfig) *AdminSocketSubsystem

func (s *AdminSocketSubsystem) ID() lifecycle.SubsystemID {
    return lifecycle.SubsystemAdminSocket
}

func (s *AdminSocketSubsystem) DependsOn() []lifecycle.SubsystemID {
    return nil // substrate has no subsystem dependencies
}
```

**`cmd/holomush/core.go`** — resolve socket paths at step 4 (alongside
`certsDir`, before subsystem construction), construct
`AdminSocketSubsystem`, and add it to the `productionSubsystems(...)`
call at step 8.

```go
// Step 4 — alongside certsDir derivation (non-fatal):
// XDG failures log a warning and leave paths empty; AdminSocketSubsystem.Start
// is a no-op when SocketPath == "", so orch.StartAll still succeeds.
var adminSocketPath, adminLockPath string
if runtimeDir, err := xdg.RuntimeDir(); err != nil {
    slog.Warn("admin socket disabled: cannot determine XDG runtime dir")
} else if err := xdg.EnsureDir(runtimeDir); err != nil {
    slog.Warn("admin socket disabled: cannot create XDG runtime dir")
} else {
    adminSocketPath = filepath.Join(runtimeDir, "admin.sock")
    adminLockPath   = filepath.Join(runtimeDir, "admin.lock")
}

// Step 7 — subsystem construction:
adminSub := socket.NewAdminSocketSubsystem(socket.AdminSocketSubsystemConfig{
    SocketPath: adminSocketPath,
    LockPath:   adminLockPath,
    Version:    version, // package-level ldflag var
})

// Step 8 — orchestrator:
for _, sub := range productionSubsystems(
    dbSub, abacSub, ..., clusterSub, auditSub, grpcSub, adminSub,
) { ... }
```

### Acceptance criteria

| ID | Criterion |
| -- | --------- |
| AC-C1 | `AdminSocketSubsystem.Start()` creates `admin.sock` and `admin.lock` in `xdg.RuntimeDir()` when the runtime dir is available; when `SocketPath == ""` (XDG unavailable), `Start()` is a no-op and returns nil |
| AC-C2 | Socket file has mode `0600` after bind |
| AC-C3 | `Status` RPC returns the configured `Version` string and `healthy: true` when the server is serving |
| AC-C4 | `curl --unix-socket {path} http://localhost/holomush.admin.v1.AdminService/Status` returns HTTP 200 with valid `StatusResponse` |
| AC-C5 | `PeerCredFromContext` returns a populated `PeerCred` on Linux (test gated `runtime.GOOS == "linux"`, `t.Skipf` otherwise); returns `ok=false` on `!linux && !darwin` without panic |
| AC-C6 | On Darwin, `PeerCred` contains non-zero `UID`, `GID`, and `PID` values (test gated `runtime.GOOS == "darwin"`, `t.Skipf` otherwise); CI matrix MUST cover both Linux and Darwin |
| AC-C7 | If `admin.lock` is held by a live process, `Start()` returns `ErrAdminSocketAlreadyHeld` and does not rebind |
| AC-C8 | If a stale `admin.lock` exists (no live holder), the flock is acquired, stale `admin.sock` is removed, and the server starts cleanly |
| AC-C9 | `Stop()` removes `admin.sock`; `admin.lock` persists as a permanent fixture |
| AC-C10 | A unit test asserts that the `productionSubsystems(...)` slice in `cmd/holomush/core_subsystems_test.go` contains a subsystem with `ID() == lifecycle.SubsystemAdminSocket` (extending the existing `TestProductionSubsystemsIncludesCluster` pattern) |
| AC-C11 | A white-box unit test (in package `socket`) asserts `s.listener.(*net.UnixListener)` succeeds after `Start()`, confirming the listener is UDS-only; no exported `Listener()` accessor required |

### Files touched

| Path | Action |
| ---- | ------ |
| `api/proto/holomush/admin/v1/admin.proto` | NEW |
| `pkg/proto/holomush/admin/v1/admin.pb.go` | NEW (generated) |
| `pkg/proto/holomush/admin/v1/admin_grpc.pb.go` | NEW (generated) |
| `pkg/proto/holomush/admin/v1/adminv1connect/admin.connect.go` | NEW (generated) |
| `internal/lifecycle/subsystem.go` | Add `SubsystemAdminSocket` constant |
| `internal/lifecycle/subsystemid_string.go` | Regenerate via `go generate` |
| `internal/admin/socket/server.go` | NEW — UDS ConnectRPC server |
| `internal/admin/socket/server_test.go` | NEW |
| `internal/admin/socket/peercred.go` | NEW — `PeerCred` type, context helpers, middleware |
| `internal/admin/socket/peercred_test.go` | NEW |
| `internal/admin/socket/peercred_linux.go` | NEW — Linux `SO_PEERCRED` impl |
| `internal/admin/socket/peercred_linux_test.go` | NEW |
| `internal/admin/socket/peercred_darwin.go` | NEW — Darwin `getsockopt(SOL_LOCAL, LOCAL_PEERCRED)` + `LOCAL_PEERPID` impl |
| `internal/admin/socket/peercred_darwin_test.go` | NEW |
| `internal/admin/socket/peercred_other.go` | NEW — no-op fallback |
| `internal/admin/socket/subsystem.go` | NEW — `AdminSocketSubsystem` |
| `internal/admin/socket/subsystem_test.go` | NEW |
| `internal/admin/socket/status_handler.go` | NEW — `Status` RPC implementation |
| `internal/admin/socket/status_handler_test.go` | NEW |
| `cmd/holomush/core.go` | Wire `AdminSocketSubsystem` into orchestrator |
| `go.mod` | `golang.org/x/sys` promoted from indirect to direct by `go mod tidy` |

## Security considerations

- **Topological lockout**: `net.Listen("unix", socketPath)` is the only
  listen call in the substrate. TCP is structurally impossible — there
  is no address string, no TCP socket, no port. AC-C11 makes this
  assertion testable.
- **Filesystem permission gate**: mode `0600` on the socket file +
  mode `0700` on the XDG runtime directory means only the server process
  owner can reach the socket. The `0700` directory is the primary gate;
  `os.Chmod(0o600)` after `net.Listen` adds the supplementary restriction.
  `os.Chmod` is preferred over `umask` to avoid mutating process-wide state.
- **SO_PEERCRED is audit-only**: it does not gate any operation.
  Authentication lives in sub-epic D.
- **flock + bind race**: the flock acquisition and `net.Listen` are not
  a single atomic operation. Two concurrent `Start()` calls can both
  acquire the flock before either reaches `net.Listen`; the second
  `net.Listen` fails with `EADDRINUSE` — a loud, correct failure. The
  main gRPC port bind provides an independent second guard.

## Open questions

None at spec time. Sub-epic D's design spec will address how
`PeerCredFromContext` output is incorporated into audit event metadata.
