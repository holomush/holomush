<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Plugin history authz — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enforce scene-membership authz at the plugin boundary in `PluginAuditService.QueryHistory`, plumbing caller identity from the host session through `eventbus.HistoryQuery` and the gRPC contract, with end-to-end opacity for the client.

**Architecture:** Defense-in-depth. The outer I-17 gate at `CoreServer.QueryStreamHistory` stays unchanged. The plugin gets a new independent membership check against its own `scene_participants` table, fed by a new `caller` field on `QueryHistoryRequest` populated by the host. Errors propagate as gRPC `status.Error` so the router can preserve codes, and `mapHistoryError` collapses plugin-side `PermissionDenied` into the same `STREAM_ACCESS_DENIED` oops code the outer gate uses.

**Tech Stack:** Go 1.22+, hashicorp/go-plugin (subprocess), protobuf (`api/proto/holomush/plugin/v1/audit.proto`), gRPC, samber/oops (structured errors), pgx (PostgreSQL), Ginkgo/Gomega (integration), testify (unit), gotestsum.

**Spec:** [docs/superpowers/specs/2026-04-23-plugin-history-authz-design.md](../specs/2026-04-23-plugin-history-authz-design.md)

**Bead:** `holomush-095g` (P1 bug)

---

## File map

| File | Action | What changes |
| --- | --- | --- |
| `api/proto/holomush/plugin/v1/audit.proto` | modify | Append `holomush.eventbus.v1.Actor caller = 8;` to `QueryHistoryRequest` |
| `pkg/proto/holomush/plugin/v1/audit.pb.go` | regenerate | Auto-generated from above |
| `internal/eventbus/publisher.go` | modify | Rename `actorToProto` → `ActorToProto`, `actorKindToProto` → `ActorKindToProto` (export). Update in-package call sites. |
| `internal/eventbus/bus.go` | modify | Add `Caller Actor` field to `HistoryQuery` struct |
| `internal/eventbus/audit/plugin_router.go` | modify | Two edit sites: (1) populate `req.Caller = ActorToProto(q.Caller)` in `QueryHistory`; (2) gRPC-status pass-through on RPC error and stream Recv error |
| `internal/eventbus/audit/plugin_router_test.go` | extend | New tests for caller forwarding and gRPC status preservation |
| `internal/grpc/query_stream_history.go` | modify | Add `caller eventbus.Actor` parameter to `fetchHistoryFramesFromBus`; populate from `info.CharacterID`; extend `mapHistoryError` with gRPC-status dispatch |
| `internal/grpc/query_stream_history_test.go` | extend | New tests for caller threading + `mapHistoryError` status dispatch |
| `plugins/core-scenes/store.go` | modify | Add `IsMember(ctx, sceneID, characterID) (bool, error)` method on `SceneStore` |
| `plugins/core-scenes/store_integration_test.go` | extend | Integration test for `IsMember` |
| `plugins/core-scenes/audit.go` | modify | Replace permissive `QueryHistory` body with auth-first flow; remove TODO at line 211; rewrite docstring |
| `plugins/core-scenes/audit_test.go` | create | Unit tests for auth dispatch, subject parsing, early rejection, mid-pagination revocation |
| `test/integration/eventbus_e2e/plugin_audit_isolation_test.go` | extend | 6 integration cases per spec §6.5 |

---

## Pre-flight

These are setup steps the implementer MUST complete before starting Task 1.

- [ ] **Confirm workspace and branch state**

```bash
cd /Users/sean/Code/github.com/holomush/.worktrees/095g
jj st
jj log -r '@-' --no-graph -T 'commit_id ++ " " ++ description.first_line()'
```

Expected: working copy is on top of `e069b5a679b7945b85aa106db6d26f4c70153ce2 feat(eventbus): history pagination on JetStream stream sequence with opaque cursors (holomush-suos) (#254)`. The previously-committed spec `docs/superpowers/specs/2026-04-23-plugin-history-authz-design.md` is in the working copy as the only changed file.

- [ ] **Confirm `bd` access from this workspace**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd show holomush-095g
```

Expected: shows the bead with status `in_progress` (already claimed earlier this session).

- [ ] **Create bead sub-tasks under holomush-095g**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd create \
  --title="Wire foundations: proto + eventbus exports + HistoryQuery.Caller" \
  --description="Spec §4.1, §4.2 (proto), §7 steps 1-3. Adds caller=8 to QueryHistoryRequest, exports ActorToProto/ActorKindToProto, adds HistoryQuery.Caller field." \
  --type=task --priority=1 --parent holomush-095g

BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd create \
  --title="Router gRPC status pass-through + caller forwarding" \
  --description="Spec §5.5, §7 steps 6-7. Two edit sites in plugin_router.go: outer QueryHistory and pluginHistoryStream.Next. Forward q.Caller; preserve plugin gRPC status codes." \
  --type=task --priority=1 --parent holomush-095g

BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd create \
  --title="Host handler caller plumbing + mapHistoryError dispatch" \
  --description="Spec §4.3, §5.5, §7 steps 5,10. fetchHistoryFramesFromBus gains caller param; mapHistoryError gets gRPC status dispatch step before existing errors.Is chain." \
  --type=task --priority=1 --parent holomush-095g

BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd create \
  --title="Plugin SceneStore.IsMember + SceneAuditServer enforcement" \
  --description="Spec §5.1-5.4, §7 steps 8-9,12. New IsMember helper; auth-first ordering in QueryHistory; subject parsing; remove TODO; update docstring." \
  --type=task --priority=1 --parent holomush-095g

BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd create \
  --title="Integration tests + verification + follow-up bead" \
  --description="Spec §6.5 (6 integration cases) and §7 step 13 (file plugin-as-caller follow-up bead). Run task pr-prep." \
  --type=task --priority=1 --parent holomush-095g
```

Record the returned bead IDs; they're referenced from each Task's "Step 1: Claim the bead" below as `holomush-XXXX`.

---

## Phase 1: Wire foundations

### Task 1: Add `caller` field to `QueryHistoryRequest` proto

**Files:**

- Modify: `api/proto/holomush/plugin/v1/audit.proto`
- Regenerate: `pkg/proto/holomush/plugin/v1/audit.pb.go`

- [ ] **Step 1: Claim the wire-foundations bead**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd update <wire-foundations-id> --status=in_progress
```

- [ ] **Step 2: Edit `api/proto/holomush/plugin/v1/audit.proto`**

Append the new field to `QueryHistoryRequest`. Do NOT modify fields 1–7 or their existing comments.

```proto
message QueryHistoryRequest {
  string subject = 1;
  bytes after = 2; // ULID; empty = from start
  bytes before = 3; // ULID; empty = unbounded
  int32 page_size = 4; // host caps at 200
  int32 direction = 5; // 1=forward, 2=backward
  google.protobuf.Timestamp not_before = 6;
  google.protobuf.Timestamp not_after = 7;

  // caller identifies the principal on whose behalf the host is reading.
  // Plugins implementing PluginAuditService MUST enforce domain-specific
  // authz (e.g., membership) against this identity before returning rows.
  // An absent caller, a zero identity, or an unsupported Actor.Kind MUST
  // be rejected with gRPC PERMISSION_DENIED. The host populates this
  // field from the authenticated session record; clients never supply it.
  holomush.eventbus.v1.Actor caller = 8;
}
```

- [ ] **Step 3: Regenerate proto**

```bash
task proto
```

Expected: `pkg/proto/holomush/plugin/v1/audit.pb.go` is updated; no other files change. `task proto` runs `buf generate`.

- [ ] **Step 4: Verify the generated Go has `Caller` field**

```bash
rg "Caller \*eventbusv1.Actor" pkg/proto/holomush/plugin/v1/audit.pb.go
```

Expected: at least one match showing `Caller *eventbusv1.Actor` on the `QueryHistoryRequest` Go struct.

- [ ] **Step 5: Verify build**

```bash
task build
```

Expected: success. Existing call sites that construct `QueryHistoryRequest` (`internal/eventbus/audit/plugin_router.go:70-74`) compile fine because `Caller` is just a new optional field.

- [ ] **Step 6: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin-proto): add caller field to PluginAuditService.QueryHistory request

Per spec docs/superpowers/specs/2026-04-23-plugin-history-authz-design.md §4.1.
Additive proto change; no plugin behavior change yet.

Refs holomush-095g."
jj new
```

`jj new` starts a fresh empty change so subsequent task commits don't clobber this one.

---

### Task 2: Export `ActorToProto` and `ActorKindToProto`

**Files:**

- Modify: `internal/eventbus/publisher.go:274,284` (rename + export)

- [ ] **Step 1: Edit `internal/eventbus/publisher.go`**

Find the existing `actorToProto` function around line 274 and rename to `ActorToProto`. Find `actorKindToProto` around line 284 and rename to `ActorKindToProto`. Update the body of `ActorToProto` to call the renamed `ActorKindToProto`.

```go
// ActorToProto converts an in-process Actor to the proto representation.
// Exported so audit-router and other cross-package callers can reuse the
// single source of truth for Actor mapping.
func ActorToProto(a Actor) *eventbusv1.Actor {
	p := &eventbusv1.Actor{Kind: ActorKindToProto(a.Kind)}
	if a.ID != (ulid.ULID{}) {
		p.Id = a.ID.Bytes()
	} else if a.LegacyID != "" {
		p.LegacyId = a.LegacyID
	}
	return p
}

// ActorKindToProto maps the in-process ActorKind enum to the proto enum.
// ActorKindUnknown (zero) maps to ACTOR_KIND_UNSPECIFIED — there is no
// proto ACTOR_KIND_UNKNOWN.
func ActorKindToProto(k ActorKind) eventbusv1.ActorKind {
	switch k {
	case ActorKindCharacter:
		return eventbusv1.ActorKind_ACTOR_KIND_CHARACTER
	case ActorKindPlayer:
		return eventbusv1.ActorKind_ACTOR_KIND_PLAYER
	case ActorKindSystem:
		return eventbusv1.ActorKind_ACTOR_KIND_SYSTEM
	case ActorKindPlugin:
		return eventbusv1.ActorKind_ACTOR_KIND_PLUGIN
	default:
		return eventbusv1.ActorKind_ACTOR_KIND_UNSPECIFIED
	}
}
```

- [ ] **Step 2: Update in-package call sites**

```bash
rg -n "actorToProto|actorKindToProto" internal/eventbus/
```

For each match (in `internal/eventbus/publisher.go` and any other files in the same package that call these helpers), update the call to the new exported names. Use `sed` only if you've reviewed every match first; safer to use the Edit tool per file.

- [ ] **Step 3: Verify build**

```bash
task build
```

Expected: success across all packages. The rename was contained to one package.

- [ ] **Step 4: Verify existing tests still pass**

```bash
task test -- ./internal/eventbus/...
```

Expected: all green.

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "refactor(eventbus): export ActorToProto / ActorKindToProto

Cross-package callers (audit-router) need the single-source proto mapping.
No behavior change.

Refs holomush-095g."
jj new
```

---

### Task 3: Add `Caller` field to `eventbus.HistoryQuery`

**Files:**

- Modify: `internal/eventbus/bus.go` (location of `HistoryQuery` struct)

- [ ] **Step 1: Locate the struct**

```bash
rg -n "^type HistoryQuery struct" internal/eventbus/
```

Expected: a single match in `internal/eventbus/bus.go` (or wherever the struct is defined).

- [ ] **Step 2: Add `Caller` field**

Add a new field at the end of the struct definition. The exact ordering doesn't affect wire compat (this is a Go struct, not proto), but appending preserves git diff readability.

```go
type HistoryQuery struct {
	Subject   Subject
	Direction Direction
	PageSize  int
	NotBefore time.Time
	NotAfter  time.Time
	AfterSeq  uint64
	AfterID   ulid.ULID
	BeforeSeq uint64
	BeforeID  ulid.ULID

	// Caller identifies the principal on whose behalf the read is happening.
	// Populated by the host's gRPC handler from the authenticated session
	// record. Public-tier readers (hot JetStream, cold Postgres) ignore this
	// field; plugin-owned subject routes (PluginHistoryRouter) MUST forward
	// it to the plugin's PluginAuditService.QueryHistory for membership
	// enforcement. See spec §4.2.
	Caller Actor
}
```

- [ ] **Step 3: Verify build**

```bash
task build
```

Expected: success. Adding a field doesn't break existing struct-literal constructions because they all use field-name syntax (verified by `rg "HistoryQuery{" --type go -A 1`).

- [ ] **Step 4: Verify existing tests still pass**

```bash
task test -- ./internal/eventbus/... ./internal/grpc/...
```

Expected: all green; the new field is unread by any existing path.

- [ ] **Step 5: Close the wire-foundations bead and commit**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd close <wire-foundations-id>
JJ_EDITOR=true jj --no-pager describe -m "feat(eventbus): add HistoryQuery.Caller field

Required by plugin-router boundary for forwarding caller identity to
PluginAuditService.QueryHistory. Public-tier readers ignore the field.

Refs holomush-095g."
jj new
```

---

## Phase 2: Router gRPC status preservation

### Task 4: Router caller forwarding + outer-call gRPC status pass-through

**Files:**

- Modify: `internal/eventbus/audit/plugin_router.go:70-101`
- Test: `internal/eventbus/audit/plugin_router_test.go` (extend)

- [ ] **Step 1: Claim the router bead**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd update <router-bead-id> --status=in_progress
```

- [ ] **Step 2: Write failing test for caller forwarding**

Add to `internal/eventbus/audit/plugin_router_test.go` (anywhere after the existing `stubProvider`/`fakeStream` types — those are already defined at lines 60–82):

```go
func TestPluginHistoryRouterForwardsCallerVerbatim(t *testing.T) {
	t.Parallel()

	id := core.NewULID()
	idBytes := id.Bytes()
	callerID := core.NewULID()

	fs := &fakeStream{
		resps: []*pluginv1.QueryHistoryResponse{
			{Event: &eventbusv1.Event{
				Id:        idBytes[:],
				Subject:   "events.main.scene.01ABC.ic",
				Timestamp: timestamppb.New(time.Unix(1, 0)),
			}},
		},
	}
	fc := &fakeHistoryClient{stream: fs}
	router := audit.NewPluginHistoryRouter(stubProvider{name: "core-scenes", client: fc})

	_, err := router.QueryHistory(context.Background(), "core-scenes", eventbus.HistoryQuery{
		Subject:   "events.main.scene.01ABC.ic",
		PageSize:  50,
		Direction: eventbus.DirectionForward,
		Caller: eventbus.Actor{
			Kind: eventbus.ActorKindCharacter,
			ID:   callerID,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, fc.lastReq)

	gotCaller := fc.lastReq.GetCaller()
	require.NotNil(t, gotCaller, "Caller MUST be set on the proto request")
	assert.Equal(t, eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, gotCaller.GetKind())
	assert.Equal(t, callerID.Bytes()[:], gotCaller.GetId(), "Id bytes MUST match the caller ULID")
}
```

- [ ] **Step 3: Run the test and verify it fails**

```bash
task test -- -run TestPluginHistoryRouterForwardsCallerVerbatim ./internal/eventbus/audit/
```

Expected: FAIL because `gotCaller` is nil — the router does not yet populate the field.

- [ ] **Step 4: Edit `internal/eventbus/audit/plugin_router.go` lines 70–74**

Find the `req := &pluginv1.QueryHistoryRequest{...}` construction and add the `Caller` field. The import already includes `"github.com/holomush/holomush/internal/eventbus"`; if the file does not already alias it, ensure the call uses the package-qualified `eventbus.ActorToProto`.

```go
req := &pluginv1.QueryHistoryRequest{
	Subject:   string(q.Subject),
	PageSize:  int32(pageSize),
	Direction: directionProto(q.Direction),
	Caller:    eventbus.ActorToProto(q.Caller),
}
```

- [ ] **Step 5: Re-run the caller-forwarding test**

```bash
task test -- -run TestPluginHistoryRouterForwardsCallerVerbatim ./internal/eventbus/audit/
```

Expected: PASS.

- [ ] **Step 6: Write failing test for gRPC status pass-through on outer RPC error**

Add another test to the same file. We need a fake client whose `QueryHistory` RPC immediately returns a `status.Error(codes.PermissionDenied, ...)` rather than opening a stream.

```go
// fakeStatusErrorClient returns a precise status error from QueryHistory.
type fakeStatusErrorClient struct {
	pluginv1.PluginAuditServiceClient
	err error
}

func (c *fakeStatusErrorClient) QueryHistory(_ context.Context, _ *pluginv1.QueryHistoryRequest, _ ...grpc.CallOption) (pluginv1.PluginAuditService_QueryHistoryClient, error) {
	return nil, c.err
}

func TestPluginHistoryRouterPreservesGRPCStatusOnQueryHistoryError(t *testing.T) {
	t.Parallel()

	wantErr := status.Error(codes.PermissionDenied, "scene audit access denied")
	router := audit.NewPluginHistoryRouter(stubProvider{
		name:   "core-scenes",
		client: &fakeStatusErrorClient{err: wantErr},
	})

	_, err := router.QueryHistory(context.Background(), "core-scenes", eventbus.HistoryQuery{
		Subject:  "events.main.scene.01ABC.ic",
		PageSize: 50,
		Caller: eventbus.Actor{
			Kind: eventbus.ActorKindCharacter,
			ID:   core.NewULID(),
		},
	})
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok, "router MUST return a recognisable status error, not an oops-wrapped opaque error")
	assert.Equal(t, codes.PermissionDenied, st.Code(), "router MUST preserve the plugin's gRPC status code")
}

func TestPluginHistoryRouterWrapsNonStatusErrorOnQueryHistoryError(t *testing.T) {
	t.Parallel()

	router := audit.NewPluginHistoryRouter(stubProvider{
		name:   "core-scenes",
		client: &fakeStatusErrorClient{err: errors.New("transport failure")},
	})

	_, err := router.QueryHistory(context.Background(), "core-scenes", eventbus.HistoryQuery{
		Subject:  "events.main.scene.01ABC.ic",
		PageSize: 50,
	})
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "non-status errors MUST be oops-wrapped for diagnostic context")
	assert.Equal(t, "AUDIT_PLUGIN_HISTORY_RPC_FAILED", oopsErr.Code())
}
```

Add new imports as needed: `"errors"`, `"google.golang.org/grpc"`, `"google.golang.org/grpc/codes"`, `"google.golang.org/grpc/status"`, `"github.com/samber/oops"`. The other identifiers (`audit`, `eventbus`, `pluginv1`, `eventbusv1`, `core`) are already imported.

- [ ] **Step 7: Run the new tests and verify they fail**

```bash
task test -- -run "TestPluginHistoryRouterPreservesGRPCStatusOnQueryHistoryError|TestPluginHistoryRouterWrapsNonStatusErrorOnQueryHistoryError" ./internal/eventbus/audit/
```

Expected: both FAIL — the existing code wraps every error with `oops.Code("AUDIT_PLUGIN_HISTORY_RPC_FAILED")`, so the status-preservation case fails, and the non-status case happens to pass for a different reason (incidental). Confirm both states.

- [ ] **Step 8: Edit `internal/eventbus/audit/plugin_router.go` lines 95–102**

Replace the existing error-wrap block:

```go
stream, err := client.QueryHistory(rpcCtx, req)
if err != nil {
	cancel()
	return nil, oops.Code("AUDIT_PLUGIN_HISTORY_RPC_FAILED").
		With("plugin", pluginName).
		With("subject", string(q.Subject)).
		Wrap(err)
}
```

with:

```go
stream, err := client.QueryHistory(rpcCtx, req)
if err != nil {
	cancel()
	// Preserve gRPC status codes from the plugin verbatim — the host's
	// mapHistoryError relies on status.FromError for opacity translation.
	// Only wrap non-status errors with our diagnostic oops code.
	if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
		return nil, err
	}
	return nil, oops.Code("AUDIT_PLUGIN_HISTORY_RPC_FAILED").
		With("plugin", pluginName).
		With("subject", string(q.Subject)).
		Wrap(err)
}
```

Add imports if missing: `"google.golang.org/grpc/codes"`, `"google.golang.org/grpc/status"`.

- [ ] **Step 9: Re-run the new tests**

```bash
task test -- -run "TestPluginHistoryRouterPreservesGRPCStatusOnQueryHistoryError|TestPluginHistoryRouterWrapsNonStatusErrorOnQueryHistoryError|TestPluginHistoryRouterForwardsCallerVerbatim" ./internal/eventbus/audit/
```

Expected: all three PASS.

- [ ] **Step 10: Run the full router test file to catch regressions**

```bash
task test -- ./internal/eventbus/audit/
```

Expected: all green.

- [ ] **Step 11: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(audit): forward caller and preserve gRPC status in PluginHistoryRouter

- QueryHistory populates QueryHistoryRequest.Caller from q.Caller via
  the exported eventbus.ActorToProto helper.
- gRPC status codes from the plugin (e.g., PermissionDenied) round-trip
  unchanged; only non-status errors get the AUDIT_PLUGIN_HISTORY_RPC_FAILED
  diagnostic wrap.

Per spec §4.2, §5.5. Refs holomush-095g."
jj new
```

---

### Task 5: `pluginHistoryStream.Next` gRPC status pass-through

**Files:**

- Modify: `internal/eventbus/audit/plugin_router.go:148-157` (`pluginHistoryStream.Next`)
- Test: `internal/eventbus/audit/plugin_router_test.go` (extend)

- [ ] **Step 1: Write failing test for stream-level gRPC status pass-through**

Add to `plugin_router_test.go`. We extend `fakeStream` so it can return a mid-stream `status.Error` from `RecvMsg` / `Recv`.

```go
// fakeStreamWithRecvErr returns the given err from Recv.
type fakeStreamWithRecvErr struct {
	fakeStream
	err error
}

func (s *fakeStreamWithRecvErr) Recv() (*pluginv1.QueryHistoryResponse, error) {
	return nil, s.err
}

// fakeRecvErrClient hands out a stream whose Recv returns the error.
type fakeRecvErrClient struct {
	pluginv1.PluginAuditServiceClient
	streamErr error
}

func (c *fakeRecvErrClient) QueryHistory(_ context.Context, _ *pluginv1.QueryHistoryRequest, _ ...grpc.CallOption) (pluginv1.PluginAuditService_QueryHistoryClient, error) {
	return &fakeStreamWithRecvErr{err: c.streamErr}, nil
}

func TestPluginHistoryStreamPreservesGRPCStatusOnRecvError(t *testing.T) {
	t.Parallel()

	wantErr := status.Error(codes.PermissionDenied, "scene audit access denied")
	router := audit.NewPluginHistoryRouter(stubProvider{
		name:   "core-scenes",
		client: &fakeRecvErrClient{streamErr: wantErr},
	})

	stream, err := router.QueryHistory(context.Background(), "core-scenes", eventbus.HistoryQuery{
		Subject:  "events.main.scene.01ABC.ic",
		PageSize: 50,
		Caller: eventbus.Actor{
			Kind: eventbus.ActorKindCharacter,
			ID:   core.NewULID(),
		},
	})
	require.NoError(t, err)

	_, err = stream.Next(context.Background())
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok, "stream Next MUST return a recognisable status error from the plugin")
	assert.Equal(t, codes.PermissionDenied, st.Code())
}
```

- [ ] **Step 2: Run test, verify failure**

```bash
task test -- -run TestPluginHistoryStreamPreservesGRPCStatusOnRecvError ./internal/eventbus/audit/
```

Expected: FAIL — `Next` currently wraps every Recv error with `oops.Code("AUDIT_PLUGIN_HISTORY_RECV_FAILED")`.

- [ ] **Step 3: Edit `internal/eventbus/audit/plugin_router.go` lines 148–157**

Replace:

```go
resp, err := s.rpc.Recv()
close(doneCh)
if err != nil {
	if errors.Is(err, io.EOF) {
		return eventbus.Event{}, io.EOF
	}
	return eventbus.Event{}, oops.Code("AUDIT_PLUGIN_HISTORY_RECV_FAILED").
		With("plugin", s.pluginName).
		With("subject", s.subject).
		Wrap(err)
}
```

with:

```go
resp, err := s.rpc.Recv()
close(doneCh)
if err != nil {
	if errors.Is(err, io.EOF) {
		return eventbus.Event{}, io.EOF
	}
	// Same pattern as the outer QueryHistory: preserve gRPC status codes
	// so mapHistoryError can do opacity translation; wrap non-status only.
	if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
		return eventbus.Event{}, err
	}
	return eventbus.Event{}, oops.Code("AUDIT_PLUGIN_HISTORY_RECV_FAILED").
		With("plugin", s.pluginName).
		With("subject", s.subject).
		Wrap(err)
}
```

- [ ] **Step 4: Re-run the test**

```bash
task test -- -run TestPluginHistoryStreamPreservesGRPCStatusOnRecvError ./internal/eventbus/audit/
```

Expected: PASS.

- [ ] **Step 5: Run the full audit package tests**

```bash
task test -- ./internal/eventbus/audit/
```

Expected: all green.

- [ ] **Step 6: Close the router bead and commit**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd close <router-bead-id>
JJ_EDITOR=true jj --no-pager describe -m "feat(audit): preserve gRPC status on plugin history stream Recv

Mirror the outer QueryHistory pattern: status.FromError-recognised codes
round-trip; transport / non-status errors get oops-wrapped.

Per spec §5.5. Refs holomush-095g."
jj new
```

---

## Phase 3: Host handler integration

### Task 6: Thread caller through `fetchHistoryFramesFromBus`

**Files:**

- Modify: `internal/grpc/query_stream_history.go:219` (call site) and `:279-308` (function signature + body)
- Test: `internal/grpc/query_stream_history_test.go` (extend)

- [ ] **Step 1: Claim the host-handler bead**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd update <host-handler-bead-id> --status=in_progress
```

- [ ] **Step 2: Write failing test for caller threading**

The existing `internal/grpc/query_stream_history_test.go` already has the scaffolding we need: `fakeHistoryReader` (line 39) records `gotQ eventbus.HistoryQuery`, and `newQueryStreamHistoryServer(t, reader, sess)` (line 82) is the harness builder. Add a new test that uses both:

```go
func TestQueryStreamHistoryThreadsCallerFromSession(t *testing.T) {
	t.Parallel()

	charID := ulid.MustParse("01HYXCHAR0000000000000000C")
	sessionInfo := &session.Info{
		CharacterID: charID,
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	sess := newTestSessionStore(t, map[string]*session.Info{"sess-1": sessionInfo})

	reader := &fakeHistoryReader{}
	server := newQueryStreamHistoryServer(t, reader, sess)

	// Public stream so the I-17 gate doesn't fire — focuses the test on
	// caller threading. Use ABAC-allow-all (already wired by the harness).
	_, err := server.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "sess-1",
		Stream:    "location:01HYXLOC00000000000000000",
		Count:     10,
	})
	require.NoError(t, err)

	assert.Equal(t, eventbus.ActorKindCharacter, reader.gotQ.Caller.Kind,
		"handler MUST set Caller.Kind = ActorKindCharacter")
	assert.Equal(t, charID, reader.gotQ.Caller.ID,
		"handler MUST set Caller.ID from info.CharacterID")
}
```

The exact construction of `*session.Info` and the signature of `newTestSessionStore` are already established by the existing tests in the file (e.g., `TestQueryStreamHistoryRejectsExpiredSession` shows the pattern). If your `session.Info` literal doesn't compile, copy from the nearest existing test in the same file — the type may have additional required fields by the time you implement.

- [ ] **Step 3: Run the test, verify it fails**

```bash
task test -- -run TestQueryStreamHistoryThreadsCallerFromSession ./internal/grpc/
```

Expected: FAIL because `reader.gotQuery.Caller` is the zero `Actor{}`.

- [ ] **Step 4: Update `fetchHistoryFramesFromBus` signature**

Edit `internal/grpc/query_stream_history.go` at the function definition (around line 279). Add `caller eventbus.Actor` as the new last parameter, and set it on the constructed `HistoryQuery`.

```go
func fetchHistoryFramesFromBus(
	ctx context.Context,
	reader eventbus.HistoryReader,
	gameID, legacyStream string,
	count int,
	notBefore time.Time,
	beforeSeq uint64,
	beforeID ulid.ULID,
	caller eventbus.Actor,
) ([]*corev1.EventFrame, error) {
	natsSubject, err := subjectxlate.Legacy(legacyStream, gameID)
	if err != nil {
		return nil, oops.With("stream", legacyStream).Wrap(err)
	}
	sub, err := eventbus.NewSubject(natsSubject)
	if err != nil {
		return nil, oops.With("stream", legacyStream).Wrap(err)
	}

	q := eventbus.HistoryQuery{
		Subject:   sub,
		Direction: eventbus.DirectionBackward,
		PageSize:  count + 1,
		NotBefore: notBefore,
		Caller:    caller,
	}
	if beforeSeq != 0 {
		q.BeforeSeq = beforeSeq
		q.BeforeID = beforeID
	} else if !beforeID.IsZero() {
		q.BeforeID = beforeID
	}
	// ... rest of existing function body unchanged ...
```

- [ ] **Step 5: Update the call site at line 219**

In the `QueryStreamHistory` handler, after Step 5 (authorization), construct the caller and pass it through:

```go
caller := eventbus.Actor{
	Kind: eventbus.ActorKindCharacter,
	ID:   info.CharacterID,
}
frames, fetchErr := fetchHistoryFramesFromBus(
	ctx, s.historyReader, s.currentGameID(), req.Stream, count,
	notBefore, beforeSeq, beforeID, caller,
)
```

- [ ] **Step 6: Re-run the test**

```bash
task test -- -run TestQueryStreamHistoryThreadsCallerFromSession ./internal/grpc/
```

Expected: PASS.

- [ ] **Step 7: Run the full handler tests**

```bash
task test -- ./internal/grpc/
```

Expected: all green. The signature change to `fetchHistoryFramesFromBus` is contained within the file; no external callers exist (confirm via `rg "fetchHistoryFramesFromBus"`).

- [ ] **Step 8: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(grpc): thread caller from session into HistoryQuery

QueryStreamHistory derives Actor{Kind: Character, ID: info.CharacterID}
and passes it to fetchHistoryFramesFromBus, which sets q.Caller before
calling reader.QueryHistory.

Per spec §4.3. Refs holomush-095g."
jj new
```

---

### Task 7: Extend `mapHistoryError` with gRPC status dispatch

**Files:**

- Modify: `internal/grpc/query_stream_history.go:259-270`
- Test: `internal/grpc/query_stream_history_test.go` (extend)

- [ ] **Step 1: Write failing tests for the new dispatch step**

Add to the same test file:

```go
func TestMapHistoryErrorTranslatesPermissionDeniedToOpaqueOopsCode(t *testing.T) {
	t.Parallel()

	pluginErr := status.Error(codes.PermissionDenied, "scene audit access denied")
	got := mapHistoryError(pluginErr)
	require.Error(t, got)

	oopsErr, ok := oops.AsOops(got)
	require.True(t, ok, "translated error MUST be oops-wrapped")
	assert.Equal(t, "STREAM_ACCESS_DENIED", oopsErr.Code(),
		"PermissionDenied from the plugin MUST collapse into the same opaque oops code the outer I-17 gate uses")
}

func TestMapHistoryErrorPassesThroughInvalidArgument(t *testing.T) {
	t.Parallel()

	pluginErr := status.Error(codes.InvalidArgument, "subject malformed")
	got := mapHistoryError(pluginErr)
	require.Error(t, got)

	st, ok := status.FromError(got)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestMapHistoryErrorRetainsCursorInvalidDispatchForNonStatusErrors(t *testing.T) {
	t.Parallel()

	got := mapHistoryError(eventbus.ErrCursorInvalid)
	require.Error(t, got)
	st, ok := status.FromError(got)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code(),
		"existing cursor-error dispatch MUST still apply when no gRPC status is present")
}
```

- [ ] **Step 2: Run, verify failures**

```bash
task test -- -run "TestMapHistoryError" ./internal/grpc/
```

Expected: the first test FAILS (no oops code on returned err), the second FAILS (no dispatch), the third PASSES already (existing behavior).

- [ ] **Step 3: Edit `internal/grpc/query_stream_history.go` lines 259–270**

Prepend a status-code dispatch step BEFORE the existing `errors.Is` chain:

```go
func mapHistoryError(err error) error {
	// gRPC status pass-through with opacity translation. The plugin emits
	// status.Error directly; the router preserves the code; we run this
	// dispatch FIRST so the existing cursor-error chain doesn't shadow it.
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.PermissionDenied:
			// Opaque: collapse plugin-boundary denial into the same oops
			// code the outer I-17 gate uses. Client cannot distinguish
			// "outer wall caught" from "plugin wall caught."
			return oops.Code("STREAM_ACCESS_DENIED").
				Errorf("not authorized to read stream")
		case codes.InvalidArgument:
			return status.Errorf(codes.InvalidArgument, "%v", err)
		}
		// Other status codes (Internal, Unavailable, …) pass through.
	}
	switch {
	case errors.Is(err, eventbus.ErrCursorInvalid):
		return status.Errorf(codes.InvalidArgument, "%v", err)
	case errors.Is(err, eventbus.ErrCursorStale):
		return status.Errorf(codes.FailedPrecondition, "%v", err)
	case errors.Is(err, eventbus.ErrCursorLag):
		return status.Errorf(codes.Unavailable, "%v", err)
	default:
		return err // let oops wrap through
	}
}
```

- [ ] **Step 4: Re-run the tests**

```bash
task test -- -run "TestMapHistoryError" ./internal/grpc/
```

Expected: all PASS.

- [ ] **Step 5: Run the full handler test package**

```bash
task test -- ./internal/grpc/
```

Expected: all green.

- [ ] **Step 6: Close the host-handler bead and commit**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd close <host-handler-bead-id>
JJ_EDITOR=true jj --no-pager describe -m "feat(grpc): translate plugin PermissionDenied to opaque STREAM_ACCESS_DENIED

mapHistoryError now runs a gRPC-status dispatch step before the existing
errors.Is chain. PermissionDenied collapses into the same oops code the
outer I-17 gate uses; InvalidArgument passes through; other codes fall
through to existing behavior.

Per spec §5.5. Refs holomush-095g."
jj new
```

---

## Phase 4: Plugin enforcement

### Task 8: `SceneStore.IsMember` helper

**Files:**

- Modify: `plugins/core-scenes/store.go` (add new method)
- Test: `plugins/core-scenes/store_integration_test.go` (extend)

- [ ] **Step 1: Claim the plugin bead**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd update <plugin-bead-id> --status=in_progress
```

- [ ] **Step 2: Write failing integration test**

Add to `plugins/core-scenes/store_integration_test.go`. The file already uses testcontainers; locate the existing setup helper (likely `setupSceneStore(t)` or similar) and reuse it. New test:

```go
func TestSceneStoreIsMemberReturnsTrueForOwner(t *testing.T) {
	store, cleanup := setupSceneStore(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	sceneID := "01SCENE000000000000000000"
	ownerID := "01CHAR0OWNER0000000000000"

	_, err := store.CreateScene(ctx, sceneID, ownerID, "Test scene", false)
	require.NoError(t, err)

	got, err := store.IsMember(ctx, sceneID, ownerID)
	require.NoError(t, err)
	assert.True(t, got, "owner MUST be reported as member")
}

func TestSceneStoreIsMemberReturnsTrueForMember(t *testing.T) {
	store, cleanup := setupSceneStore(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	sceneID := "01SCENE000000000000000000"
	ownerID := "01CHAR0OWNER0000000000000"
	memberID := "01CHAR0MEMBER000000000000"

	_, err := store.CreateScene(ctx, sceneID, ownerID, "Test scene", false)
	require.NoError(t, err)
	// Reuse the existing JoinScene path or whatever the file uses to add members.
	_, err = store.JoinScene(ctx, sceneID, memberID)
	require.NoError(t, err)

	got, err := store.IsMember(ctx, sceneID, memberID)
	require.NoError(t, err)
	assert.True(t, got)
}

func TestSceneStoreIsMemberReturnsFalseForInvited(t *testing.T) {
	store, cleanup := setupSceneStore(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	sceneID := "01SCENE000000000000000000"
	ownerID := "01CHAR0OWNER0000000000000"
	invitedID := "01CHAR0INVITED00000000000"

	_, err := store.CreateScene(ctx, sceneID, ownerID, "Private scene", true)
	require.NoError(t, err)
	err = store.InviteParticipant(ctx, sceneID, ownerID, invitedID)
	require.NoError(t, err)

	got, err := store.IsMember(ctx, sceneID, invitedID)
	require.NoError(t, err)
	assert.False(t, got, "invited-only rows MUST return false — invitation grants join, not read")
}

func TestSceneStoreIsMemberReturnsFalseForNonParticipant(t *testing.T) {
	store, cleanup := setupSceneStore(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	sceneID := "01SCENE000000000000000000"
	ownerID := "01CHAR0OWNER0000000000000"
	otherID := "01CHAR0OTHER000000000000"

	_, err := store.CreateScene(ctx, sceneID, ownerID, "Test scene", false)
	require.NoError(t, err)

	got, err := store.IsMember(ctx, sceneID, otherID)
	require.NoError(t, err)
	assert.False(t, got)
}

func TestSceneStoreIsMemberReturnsFalseForMissingScene(t *testing.T) {
	store, cleanup := setupSceneStore(t)
	t.Cleanup(cleanup)

	got, err := store.IsMember(context.Background(),
		"01SCENE000000000000000000", "01CHAR000000000000000000")
	require.NoError(t, err, "missing scene MUST be nil error per spec §5.4 (info-hiding)")
	assert.False(t, got)
}
```

If the existing file uses different helper names (`SeedScene`, `AddParticipant`, etc.), adapt the calls. The behavior assertions don't change.

- [ ] **Step 3: Run the integration tests, verify they fail**

```bash
task test:int -- -run "TestSceneStoreIsMember" ./plugins/core-scenes/
```

The plugin store integration tests live at `./plugins/core-scenes/` (verified against the canonical package list in `Taskfile.yaml:test:int`, which enumerates `./plugins/core-scenes/` among the integration targets). Expected: FAIL with "store.IsMember undefined."

- [ ] **Step 4: Add `IsMember` to `plugins/core-scenes/store.go`**

Append after `GetWithMembership` (around line 326):

```go
// IsMember reports whether characterID has an owner or member row in
// sceneID. Invited-only rows return false — invitation grants join
// rights, not read rights (see spec §5.4 for the deliberate role-policy
// tightening).
//
// Missing scene and missing row both return (false, nil) by design: the
// audit-read boundary MUST NOT distinguish "scene doesn't exist" from
// "you're not a member" because that would leak scene existence to
// non-members. Internal logs MAY tag the cases distinctly via slog
// attributes; the function's return type does not.
func (s *SceneStore) IsMember(ctx context.Context, sceneID, characterID string) (bool, error) {
	const q = `
		SELECT 1
		FROM scene_participants
		WHERE scene_id = $1
		  AND character_id = $2
		  AND role IN ('owner', 'member')
		LIMIT 1
	`
	var one int
	err := s.pool.QueryRow(ctx, q, sceneID, characterID).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, oops.Code("SCENE_STORE_IS_MEMBER_FAILED").
			With("scene_id", sceneID).
			With("character_id", characterID).
			Wrap(err)
	}
	return true, nil
}
```

If `s.pool` is named differently (e.g., `s.db`), match the file's existing convention. Confirm by reading lines 1–80 of `store.go`. Imports needed: `errors`, `github.com/jackc/pgx/v5`. Add them only if not already present.

- [ ] **Step 5: Re-run the integration tests**

```bash
task test:int -- -run "TestSceneStoreIsMember" ./plugins/core-scenes/
```

Expected: all five tests PASS.

- [ ] **Step 6: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(core-scenes): add SceneStore.IsMember helper

Narrow indexed lookup on scene_participants for the audit-read boundary.
Invited-only rows return false; missing scene returns (false, nil) for
information-hiding (spec §5.4).

Refs holomush-095g."
jj new
```

---

### Task 9: Plugin `SceneAuditServer.QueryHistory` enforcement

**Files:**

- Modify: `plugins/core-scenes/audit.go:202-269` (replace `QueryHistory` body and docstring)
- Test: `plugins/core-scenes/audit_test.go` (create)

- [ ] **Step 1: Create the test scaffolding**

Create `plugins/core-scenes/audit_test.go`. The plugin compiles as `package main`; tests can live in the same package. We need a fake `pluginv1.PluginAuditService_QueryHistoryServer` to capture sends, and a fake store that records whether `queryLog` was called.

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// fakeAuditServerStream satisfies pluginv1.PluginAuditService_QueryHistoryServer
// and records every Send.
type fakeAuditServerStream struct {
	grpc.ServerStream
	ctx       context.Context
	sends     []*pluginv1.QueryHistoryResponse
	sendErr   error
}

func (s *fakeAuditServerStream) Context() context.Context  { return s.ctx }
func (s *fakeAuditServerStream) SendHeader(metadata.MD) error { return nil }
func (s *fakeAuditServerStream) SetHeader(metadata.MD) error  { return nil }
func (s *fakeAuditServerStream) SetTrailer(metadata.MD)       {}
func (s *fakeAuditServerStream) Send(resp *pluginv1.QueryHistoryResponse) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.sends = append(s.sends, resp)
	return nil
}

// fakeAuditStore records calls made by SceneAuditServer during a test.
// It satisfies BOTH sceneAuditLogStore and sceneMembershipLookup so a
// single instance can serve as the test double for both store fields.
type fakeAuditStore struct {
	queryLogCalled bool
	rows           []logRow
	queryLogErr    error
	isMemberMap    map[string]bool // key: sceneID|characterID
}

func (s *fakeAuditStore) IsMember(_ context.Context, sceneID, characterID string) (bool, error) {
	if s.isMemberMap == nil {
		return false, nil
	}
	return s.isMemberMap[sceneID+"|"+characterID], nil
}

// queryLog matches *SceneAuditStore.queryLog verbatim (verified against
// plugins/core-scenes/audit.go before the rewrite — uses *timestamppb.Timestamp,
// not time.Time, for notBefore/notAfter).
func (s *fakeAuditStore) queryLog(
	_ context.Context,
	_ string,
	_, _ []byte,
	_, _ *timestamppb.Timestamp,
	_ bool,
	_ int,
) ([]logRow, error) {
	s.queryLogCalled = true
	if s.queryLogErr != nil {
		return nil, s.queryLogErr
	}
	return s.rows, nil
}

func ulidStringBytes(t *testing.T, s string) []byte {
	t.Helper()
	id, err := ulid.Parse(s)
	require.NoError(t, err)
	b := id.Bytes()
	return b[:]
}
```

`SceneAuditServer.store` is currently typed as a concrete `*SceneAuditStore`. To make the server testable without a real PG pool AND to give it access to `SceneStore.IsMember` (which lives on the *parent* `*SceneStore`, not the audit store), restructure the server:

1. Open `plugins/core-scenes/audit.go`. Find the existing `SceneAuditStore.queryLog` method signature — copy it verbatim into the interface below. Do NOT rewrite the parameter types; the existing signature passes `*timestamppb.Timestamp` directly (verified against current call site at `audit.go:244-245`). The interface MUST match the existing method exactly so `*SceneAuditStore` satisfies it without code change.

2. Add two interfaces and update the `SceneAuditServer` struct:

```go
// sceneAuditLogStore is the log-storage surface SceneAuditServer needs.
// Signature matches *SceneAuditStore.queryLog verbatim (verified at
// plugins/core-scenes/audit.go before the rewrite).
type sceneAuditLogStore interface {
	queryLog(
		ctx context.Context,
		subject string,
		after, before []byte,
		notBefore, notAfter *timestamppb.Timestamp,
		reverse bool,
		pageSize int,
	) ([]logRow, error)
}

// sceneMembershipLookup is the membership-check surface SceneAuditServer
// needs. *SceneStore (Task 8) satisfies this.
type sceneMembershipLookup interface {
	IsMember(ctx context.Context, sceneID, characterID string) (bool, error)
}

type SceneAuditServer struct {
	pluginv1.UnimplementedPluginAuditServiceServer
	store        sceneAuditLogStore    // queryLog only
	memberLookup sceneMembershipLookup // IsMember only
}
```

3. Update wiring in `main.go:108`:

```go
p.auditSrv.store = NewSceneAuditStore(store.Pool())
p.auditSrv.memberLookup = store // *SceneStore satisfies sceneMembershipLookup
```

Verify `task build` passes after these structural changes before writing the new tests in Step 2.

- [ ] **Step 2: Write failing auth-dispatch tests**

Append to `plugins/core-scenes/audit_test.go`:

```go
func TestQueryHistoryRejectsNilCaller(t *testing.T) {
	srv := &SceneAuditServer{
		store:        &fakeAuditStore{},
		memberLookup: &fakeAuditStore{},
	}
	stream := &fakeAuditServerStream{ctx: context.Background()}

	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: "events.main.scene.01ABC0000000000000000000.ic",
		Caller:  nil, // explicit
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "auth denial MUST emit gRPC status")
	assert.Equal(t, codes.PermissionDenied, st.Code())
}

func TestQueryHistoryRejectsCharacterCallerWithZeroID(t *testing.T) {
	srv := &SceneAuditServer{
		store:        &fakeAuditStore{},
		memberLookup: &fakeAuditStore{},
	}
	stream := &fakeAuditServerStream{ctx: context.Background()}

	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: "events.main.scene.01ABC0000000000000000000.ic",
		Caller: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   nil, // zero
		},
	}, stream)

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.PermissionDenied, st.Code())
}

func TestQueryHistoryRejectsNonCharacterKinds(t *testing.T) {
	cases := []eventbusv1.ActorKind{
		eventbusv1.ActorKind_ACTOR_KIND_UNSPECIFIED,
		eventbusv1.ActorKind_ACTOR_KIND_PLAYER,
		eventbusv1.ActorKind_ACTOR_KIND_SYSTEM,
		eventbusv1.ActorKind_ACTOR_KIND_PLUGIN,
	}
	for _, k := range cases {
		t.Run(k.String(), func(t *testing.T) {
			srv := &SceneAuditServer{
				store:        &fakeAuditStore{},
				memberLookup: &fakeAuditStore{},
			}
			stream := &fakeAuditServerStream{ctx: context.Background()}

			err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
				Subject: "events.main.scene.01ABC0000000000000000000.ic",
				Caller: &eventbusv1.Actor{
					Kind: k,
					Id:   ulidStringBytes(t, "01CHAR000000000000000000"),
				},
			}, stream)
			require.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.PermissionDenied, st.Code())
		})
	}
}

func TestQueryHistoryAllowsMemberAndReturnsRows(t *testing.T) {
	sceneID := "01ABC0000000000000000000"
	charIDStr := "01CHAR000000000000000000"
	charBytes := ulidStringBytes(t, charIDStr)

	logStore := &fakeAuditStore{
		rows: []logRow{
			{id: ulidStringBytes(t, "01EVENT00000000000000000"),
				subject:   "events.main.scene." + sceneID + ".ic",
				eventType: "scene.pose.posted",
				timestamp: time.Unix(100, 0),
			},
		},
	}
	memberStore := &fakeAuditStore{
		isMemberMap: map[string]bool{sceneID + "|" + charIDStr: true},
	}

	srv := &SceneAuditServer{store: logStore, memberLookup: memberStore}
	stream := &fakeAuditServerStream{ctx: context.Background()}

	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: "events.main.scene." + sceneID + ".ic",
		Caller: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   charBytes,
		},
	}, stream)

	require.NoError(t, err)
	require.Len(t, stream.sends, 1)
	assert.Equal(t, "scene.pose.posted", stream.sends[0].GetEvent().GetType())
}

func TestQueryHistoryDeniesNonMemberWithoutHittingLogStore(t *testing.T) {
	sceneID := "01ABC0000000000000000000"
	charIDStr := "01CHAR000000000000000000"
	charBytes := ulidStringBytes(t, charIDStr)

	logStore := &fakeAuditStore{} // no rows; we assert it isn't called
	memberStore := &fakeAuditStore{isMemberMap: map[string]bool{}}

	srv := &SceneAuditServer{store: logStore, memberLookup: memberStore}
	stream := &fakeAuditServerStream{ctx: context.Background()}

	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: "events.main.scene." + sceneID + ".ic",
		Caller: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   charBytes,
		},
	}, stream)

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.PermissionDenied, st.Code())
	assert.False(t, logStore.queryLogCalled,
		"log store MUST NOT be queried when auth denies — auth is step 1, before any DB work")
}

func TestQueryHistoryRejectsMalformedSubject(t *testing.T) {
	cases := []string{
		"",                                // empty — existing SCENE_AUDIT_SUBJECT_REQUIRED
		"events.main.location.01XYZ.ic",   // not scene
		"not.events.prefix.scene.01.ic",   // not events.*
		"events.main.scene.*.ic",          // wildcard in sceneID
		"events.main.scene.>",             // wildcard
		"events.main.scene",               // too few tokens
	}
	for _, subj := range cases {
		t.Run(subj, func(t *testing.T) {
			srv := &SceneAuditServer{
				store:        &fakeAuditStore{},
				memberLookup: &fakeAuditStore{isMemberMap: map[string]bool{}},
			}
			stream := &fakeAuditServerStream{ctx: context.Background()}

			err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
				Subject: subj,
				Caller: &eventbusv1.Actor{
					Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
					Id:   ulidStringBytes(t, "01CHAR000000000000000000"),
				},
			}, stream)
			require.Error(t, err)
			st, _ := status.FromError(err)
			// Empty subject keeps the existing SCENE_AUDIT_SUBJECT_REQUIRED
			// path (also INVALID_ARGUMENT). All others go through the new
			// SCENE_AUDIT_SUBJECT_INVALID path.
			assert.Equal(t, codes.InvalidArgument, st.Code())
		})
	}
}
```

- [ ] **Step 3: Run tests, verify they fail**

```bash
task test -- ./plugins/core-scenes/
```

Expected: all the new tests FAIL — current `QueryHistory` has no caller validation, no membership check, and no subject parsing beyond the empty-string guard.

- [ ] **Step 4: Replace `QueryHistory` in `plugins/core-scenes/audit.go`**

Replace lines 202–269 with the new implementation. Drop the TODO; rewrite the docstring to describe the enforcement contract.

```go
// QueryHistory streams scene_log rows matching the request after enforcing
// scene membership at the plugin boundary. Authorisation is step 1 and runs
// BEFORE cursor decoding or any DB query — the early-rejection ordering
// avoids timing oracles and is pinned by audit_test.go's
// TestQueryHistoryDeniesNonMemberWithoutHittingLogStore.
//
// The caller (req.Caller) is forwarded verbatim from the host's
// CoreServer.QueryStreamHistory handler (which derives it from the
// authenticated session). Plugins MUST NOT trust client-supplied identity;
// see spec §3.2 for the trust model.
//
// Membership policy: only owner and member roles see rows. Invited rows
// return PERMISSION_DENIED — invitation grants join rights, not passive
// read rights (spec §5.4). Non-CHARACTER caller kinds are rejected;
// admin / system / cross-plugin reads are deferred to a future RPC.
//
// Errors:
//   - codes.PermissionDenied — caller missing, kind unsupported, or non-member
//   - codes.InvalidArgument  — subject empty or malformed
//   - codes.Internal         — store / DB error
func (s *SceneAuditServer) QueryHistory(req *pluginv1.QueryHistoryRequest, stream pluginv1.PluginAuditService_QueryHistoryServer) error {
	if req == nil || req.GetSubject() == "" {
		return status.Error(codes.InvalidArgument, "subject required")
	}

	// Auth — step 1, before any other work.
	caller := req.GetCaller()
	if caller == nil {
		slog.InfoContext(stream.Context(), "scene audit denied — caller missing",
			"subject", req.GetSubject(), "code", "SCENE_AUDIT_AUTH_REQUIRED")
		return status.Error(codes.PermissionDenied, "caller required")
	}
	if caller.GetKind() != eventbusv1.ActorKind_ACTOR_KIND_CHARACTER {
		slog.InfoContext(stream.Context(), "scene audit denied — non-character caller",
			"subject", req.GetSubject(), "kind", caller.GetKind().String(),
			"code", "SCENE_AUDIT_AUTH_REQUIRED")
		return status.Error(codes.PermissionDenied, "unsupported caller kind")
	}
	callerIDBytes := caller.GetId()
	if len(callerIDBytes) != 16 {
		slog.InfoContext(stream.Context(), "scene audit denied — caller id wrong length",
			"subject", req.GetSubject(), "code", "SCENE_AUDIT_AUTH_REQUIRED")
		return status.Error(codes.PermissionDenied, "caller id required")
	}
	var callerULID ulid.ULID
	copy(callerULID[:], callerIDBytes)
	if callerULID == (ulid.ULID{}) {
		slog.InfoContext(stream.Context(), "scene audit denied — caller id zero",
			"subject", req.GetSubject(), "code", "SCENE_AUDIT_AUTH_REQUIRED")
		return status.Error(codes.PermissionDenied, "caller id required")
	}
	callerCharID := callerULID.String()

	// Subject parse.
	sceneID, err := parseSceneSubject(req.GetSubject())
	if err != nil {
		slog.InfoContext(stream.Context(), "scene audit denied — subject malformed",
			"subject", req.GetSubject(), "code", "SCENE_AUDIT_SUBJECT_INVALID")
		return status.Error(codes.InvalidArgument, err.Error())
	}

	// Membership check.
	ok, err := s.memberLookup.IsMember(stream.Context(), sceneID, callerCharID)
	if err != nil {
		return status.Errorf(codes.Internal, "membership lookup failed: %v", err)
	}
	if !ok {
		slog.InfoContext(stream.Context(), "scene audit denied — non-member",
			"subject", req.GetSubject(), "scene_id", sceneID,
			"character_id", callerCharID, "code", "SCENE_AUDIT_ACCESS_DENIED")
		return status.Error(codes.PermissionDenied, "not a participant")
	}

	// From here, the existing pagination + streaming logic runs unchanged.
	ctx := stream.Context()
	pageSize := int(req.GetPageSize())
	if pageSize <= 0 {
		pageSize = auditDefaultPageSize
	}
	if pageSize > auditMaxPageSize {
		pageSize = auditMaxPageSize
	}

	dir := req.GetDirection()
	if dir == 0 {
		dir = directionForward
	}

	var (
		afterCursor  []byte
		beforeCursor []byte
	)
	if v := req.GetAfter(); len(v) > 0 {
		afterCursor = v
	}
	if v := req.GetBefore(); len(v) > 0 {
		beforeCursor = v
	}

	// IMPORTANT: this call expression MUST match the existing pre-edit
	// code at audit.go:244-245 verbatim. The current code passes
	// req.GetNotBefore() / req.GetNotAfter() (proto Timestamps) directly
	// — do NOT wrap with .AsTime() unless the existing queryLog signature
	// has changed. Copy the call from the pre-edit version.
	rows, err := s.store.queryLog(ctx, req.GetSubject(), afterCursor, beforeCursor,
		req.GetNotBefore(), req.GetNotAfter(), dir == directionBackward, pageSize)
	if err != nil {
		return err
	}

	for i := range rows {
		row := &rows[i]
		resp := &pluginv1.QueryHistoryResponse{
			Event: &eventbusv1.Event{
				Id:        row.id,
				Subject:   row.subject,
				Type:      row.eventType,
				Timestamp: timestamppb.New(row.timestamp),
				Actor:     actorProtoFromRow(row.actorKind, row.actorID),
				Payload:   row.payload,
			},
		}
		if err := stream.Send(resp); err != nil {
			return status.Errorf(codes.Internal, "send failed for subject %s: %v",
				req.GetSubject(), err)
		}
	}
	return nil
}

// parseSceneSubject extracts sceneID from a JetStream-native scene subject.
// Expected: events.<gameID>.scene.<sceneID>.<channel>[.<...>]. Rejects
// wildcard tokens and malformed shapes. See spec §5.3.
func parseSceneSubject(subject string) (string, error) {
	parts := strings.Split(subject, ".")
	if len(parts) < 5 {
		return "", oops.Code("SCENE_AUDIT_SUBJECT_INVALID").
			With("subject", subject).
			Errorf("subject does not match events.<game>.scene.<id>.<channel>")
	}
	if parts[0] != "events" || parts[2] != "scene" {
		return "", oops.Code("SCENE_AUDIT_SUBJECT_INVALID").
			With("subject", subject).
			Errorf("subject not owned by core-scenes")
	}
	for _, p := range parts {
		if strings.ContainsAny(p, "*>") {
			return "", oops.Code("SCENE_AUDIT_SUBJECT_INVALID").
				With("subject", subject).
				Errorf("wildcard subjects not permitted for QueryHistory")
		}
	}
	return parts[3], nil
}
```

Imports needed at top of `audit.go` (verified against the current file: `slog` is NOT yet imported; the others are also new):

- `"log/slog"`
- `"strings"`
- `"google.golang.org/grpc/codes"`
- `"google.golang.org/grpc/status"`

Already imported and unchanged: `"context"`, `"time"`, `"github.com/oklog/ulid/v2"`, `"github.com/samber/oops"`, `"google.golang.org/protobuf/types/known/timestamppb"`, the proto packages.

- [ ] **Step 5: Run unit tests, verify they pass**

```bash
task test -- ./plugins/core-scenes/
```

Expected: all green, including the new tests.

- [ ] **Step 6: Run plugin integration tests**

```bash
task test:int -- ./test/integration/plugin/...
```

Expected: existing plugin tests still pass. The wire change is additive.

- [ ] **Step 7: Add the early-rejection regression test explicitly**

Confirm `TestQueryHistoryDeniesNonMemberWithoutHittingLogStore` (Step 2) is in the file; this is the load-bearing pin against future ordering regressions per spec §6.1.

- [ ] **Step 8: Add the membership-change-mid-pagination test**

Append to `audit_test.go`:

```go
func TestQueryHistoryReChecksMembershipAcrossPaginations(t *testing.T) {
	sceneID := "01ABC0000000000000000000"
	charIDStr := "01CHAR000000000000000000"
	charBytes := ulidStringBytes(t, charIDStr)

	logStore := &fakeAuditStore{
		rows: []logRow{{
			id:        ulidStringBytes(t, "01EVENT00000000000000000"),
			subject:   "events.main.scene." + sceneID + ".ic",
			eventType: "scene.pose.posted",
			timestamp: time.Unix(100, 0),
		}},
	}
	memberStore := &fakeAuditStore{
		isMemberMap: map[string]bool{sceneID + "|" + charIDStr: true},
	}

	srv := &SceneAuditServer{store: logStore, memberLookup: memberStore}

	// Page 1 — member, rows returned.
	stream1 := &fakeAuditServerStream{ctx: context.Background()}
	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: "events.main.scene." + sceneID + ".ic",
		Caller: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   charBytes,
		},
	}, stream1)
	require.NoError(t, err)
	require.Len(t, stream1.sends, 1)

	// Simulate kick.
	delete(memberStore.isMemberMap, sceneID+"|"+charIDStr)

	// Page 2 — no longer member, denied.
	stream2 := &fakeAuditServerStream{ctx: context.Background()}
	err = srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: "events.main.scene." + sceneID + ".ic",
		Caller: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   charBytes,
		},
	}, stream2)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.PermissionDenied, st.Code())
	assert.Empty(t, stream2.sends)
}
```

- [ ] **Step 9: Re-run the test**

```bash
task test -- -run TestQueryHistoryReChecksMembershipAcrossPaginations ./plugins/core-scenes/
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(core-scenes): enforce scene membership at PluginAuditService.QueryHistory

Auth runs as step 1: caller validation (kind=CHARACTER, non-zero ID),
subject parse, membership check via SceneStore.IsMember. Non-members
(including invited-only) get PERMISSION_DENIED before any log query
runs. Removes the TODO at audit.go:211.

Per spec §5.1-5.5. Refs holomush-095g."
jj new
```

---

### Task 10: Run full unit suite for plugin packages

- [ ] **Step 1: Run plugin unit tests**

```bash
task test -- ./plugins/...
```

Expected: all green.

- [ ] **Step 2: Close the plugin bead**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd close <plugin-bead-id>
```

(No commit — Tasks 8 and 9 already shipped the code.)

---

## Phase 5: Integration & follow-up

### Task 11: Spec §6.5 integration cases — distributed across the right test files

**Files (one per spec case):**

- Modify: `internal/grpc/query_stream_history_test.go` (case 2 — handler-level opacity test)
- Modify: `cmd/holomush/sub_grpc_adapters_test.go` (case 6 — `busHistoryReaderAdapter` fail-closed)
- (Cases 1, 3, 4, 5 are already covered — see Step 2 below)

**Test-style note:** the existing files in this repo use plain Go `testing.T`, NOT Ginkgo/Gomega. Verified via `rg "^func Test" test/integration/eventbus_e2e/plugin_audit_isolation_test.go` (single-Test-function file, no `Describe`/`It`). Match the existing style.

- [ ] **Step 1: Claim the integration bead**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd update <integration-bead-id> --status=in_progress
```

- [ ] **Step 2: Map spec §6.5 cases to actual test locations (no work — just verify coverage)**

The spec lists 6 cases. Four of them are already covered by tests written in earlier tasks; only two need new tests:

| §6.5 case | Where it's tested | Status |
| --- | --- | --- |
| 1 — outer I-17 catches non-participant | `TestQueryStreamHistoryEnforcesMembershipGateForPrivateStream` (existing, `internal/grpc/query_stream_history_test.go`) | Existing test still applies; verify it still passes after Task 6/7 changes. |
| 2 — plugin wall catches when outer passes | NEW: `TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode` (this task, Step 3) | NEW |
| 3 — router PermissionDenied for non-participant Caller | `TestPluginHistoryRouterPreservesGRPCStatusOnQueryHistoryError` (Task 4) + `TestPluginHistoryStreamPreservesGRPCStatusOnRecvError` (Task 5) | Already covered by router unit tests. |
| 4 — router PermissionDenied for zero Caller | `TestQueryHistoryRejectsNilCaller` and friends (Task 9 plugin unit tests) prove the plugin-side denial; the router-side wiring is exercised by Task 4. | Already covered. |
| 5 — mid-read revocation | `TestQueryHistoryReChecksMembershipAcrossPaginations` (Task 9, plugin unit) | Already covered at unit level — same enforcement code path; integration adds no signal. |
| 6 — plugin-host RPC fail-closed | NEW: `TestBusHistoryReaderAdapterFailsClosedOnPluginOwnedSubjects` (this task, Step 4) | NEW |

If any of the existing-test references in the table fail after the earlier tasks land, fix the failure before proceeding. Don't forge ahead.

- [ ] **Step 3: Write case 2 — plugin-wall opacity test**

Add to `internal/grpc/query_stream_history_test.go`. The setup builds a session WITH a focus membership (so I-17 passes) and a `fakeHistoryReader` whose `err` is the plugin's `status.Error(codes.PermissionDenied, ...)`. Assertion: handler's returned error has oops code `STREAM_ACCESS_DENIED` — the SAME code the outer I-17 denial uses, proving opacity.

```go
func TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode(t *testing.T) {
	t.Parallel()

	stream, focus := sceneFocusMembership(t)
	future := time.Now().Add(time.Hour)
	sess := newTestSessionStore(t, map[string]*session.Info{
		"s1": {
			ID:               "s1",
			CharacterID:      ulid.MustParse("01HYXCHAR0000000000000000C"),
			ExpiresAt:        &future,
			FocusMemberships: []session.FocusMembership{focus},
		},
	})

	// fakeHistoryReader returns a precise plugin-style status error.
	reader := &fakeHistoryReader{
		err: status.Error(codes.PermissionDenied, "scene audit access denied"),
	}
	s := newQueryStreamHistoryServer(t, reader, sess)

	_, err := s.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "s1",
		Stream:    stream,
		Count:     10,
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "STREAM_ACCESS_DENIED",
		"plugin-boundary PermissionDenied MUST translate to the same opaque oops code that the outer I-17 gate uses")
}
```

Imports needed (verify which are missing): `time`, `github.com/oklog/ulid/v2`, `google.golang.org/grpc/codes`, `google.golang.org/grpc/status`. The other identifiers (`session`, `sceneFocusMembership`, `newTestSessionStore`, `newQueryStreamHistoryServer`, `errutil`, `corev1`, `fakeHistoryReader`) are already in scope from the existing file.

If `errutil.AssertErrorCode` doesn't accept a third "message" argument in your version, drop it — the existing tests at lines 220, 291, 310, 329 use the two-argument form.

- [ ] **Step 4: Run case 2 and verify**

```bash
task test -- -run TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode ./internal/grpc/
```

Expected: PASS (Task 7 already implemented the `mapHistoryError` dispatch).

If this fails because the `fakeHistoryReader.err` flows through `fetchHistoryFramesFromBus`'s additional wrapping (it calls `mapHistoryError(oops.Code("INTERNAL").With(...).Wrap(fetchErr))` at line 221), inspect: the outer `oops.Code("INTERNAL").Wrap` will mask the inner status. Verify the call site survives a status pass-through. If not, refine Task 7's `mapHistoryError` to look at the wrapped inner error too — `oops.AsOops(err).Cause()` or `errors.Unwrap` walk. Pin this with the test before declaring it green.

- [ ] **Step 5: Write case 6 — `busHistoryReaderAdapter` fail-closed**

Add to `cmd/holomush/sub_grpc_adapters_test.go`. The adapter wraps a `eventbus.HistoryReader`; for plugin-owned subjects the Reader routes to `PluginHistoryRouter`. We can inject a fake Reader that simulates the routed-to-plugin response.

```go
// fakeReader returns a precise error from QueryHistory, regardless of q.
type fakeReader struct{ err error }

func (f *fakeReader) QueryHistory(_ context.Context, _ eventbus.HistoryQuery) (eventbus.HistoryStream, error) {
	return nil, f.err
}

func TestBusHistoryReaderAdapterFailsClosedOnPluginOwnedSubjects(t *testing.T) {
	t.Parallel()

	// Simulate what happens when the adapter routes a plugin-owned subject
	// through the router with zero q.Caller: plugin returns PermissionDenied,
	// router preserves the status, adapter receives it.
	reader := &fakeReader{
		err: status.Error(codes.PermissionDenied, "caller required"),
	}
	adapter := &busHistoryReaderAdapter{
		reader: reader,
		gameID: func() string { return "main" },
	}

	_, err := adapter.ReplayTail(context.Background(),
		"scene:01HYXSCENE00000000000000CC:ic", 10, time.Time{}, ulid.ULID{})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err),
		"adapter MUST surface the plugin's PermissionDenied for plugin-owned subjects until the plugin-as-caller follow-up lands")
}
```

The adapter currently wraps every error with `oops.With("stream", ...).Wrap(err)` (sub_grpc.go:521,538,550). That `oops.Wrap` may bury the gRPC status — `status.Code(err)` walks the unwrap chain via `status.FromError`, but if the oops wrap doesn't expose the inner via `Unwrap()` then `status.Code` returns `Unknown`.

If the test fails for that reason, fix: change the three `oops.Wrap` calls in `busHistoryReaderAdapter.ReplayTail` to use the same `status.FromError` pass-through pattern Task 4 introduced for the router. Document this fix as a small extension to Task 5 / 6 that the case 6 test pins.

- [ ] **Step 6: Run case 6**

```bash
task test -- -run TestBusHistoryReaderAdapterFailsClosedOnPluginOwnedSubjects ./cmd/holomush/
```

Expected: PASS. If it fails because of the `oops.Wrap` shadowing, see the fix note in Step 5.

- [ ] **Step 7: Run the full unit suite**

```bash
task test
```

Expected: all green. The existing `TestQueryStreamHistoryEnforcesMembershipGateForPrivateStream` (case 1 from §6.5) still passes, asserting we didn't regress the outer I-17 path.

- [ ] **Step 8: Run integration suite**

```bash
task test:int
```

Expected: all green. The existing `plugin_audit_isolation_test.go` still passes — its scope (audit projection routing, not QueryHistory authz) is orthogonal to our changes, but verify nothing broke.

- [ ] **Step 9: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "test: cover spec §6.5 cases 2 and 6 against existing scaffolding

Case 2 (plugin-wall opacity) goes in query_stream_history_test.go using
the existing fakeHistoryReader to inject a status.Error(PermissionDenied);
asserts errutil.AssertErrorCode(STREAM_ACCESS_DENIED), proving §5.5's
translation chain.

Case 6 (busHistoryReaderAdapter fail-closed for plugin-owned subjects)
goes in sub_grpc_adapters_test.go using a fakeReader that simulates the
plugin's PermissionDenied; pins the §4.4 deferred behavior.

Cases 1, 3, 4, 5 are already covered by prior task tests (existing
TestQueryStreamHistoryEnforcesMembershipGateForPrivateStream, the
router unit tests in Tasks 4-5, and the plugin unit tests in Task 9).

Per spec §6.5. Refs holomush-095g."
jj new
```

---

### Task 12: File the follow-up bead for plugin-as-caller identity

**Files:** none (beads only).

- [ ] **Step 1: File the bead**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd create \
  --title="Plugin-as-caller identity for PluginHostService.QueryStreamHistory against plugin-owned subjects" \
  --description="Spec docs/superpowers/specs/2026-04-23-plugin-history-authz-design.md §4.4 deferred this. Today busHistoryReaderAdapter.ReplayTail (cmd/holomush/sub_grpc.go:511) passes zero q.Caller, so plugin-owned subjects routed through PluginHistoryRouter return PERMISSION_DENIED to plugin callers. No in-tree caller currently breaks. Before any plugin starts reading plugin-owned subjects via this path, design: (a) what Actor.Kind a plugin caller carries, (b) what capability grants gate cross-plugin reads, (c) whether plugins can read their own subjects without the gate, (d) how a plugin asserts which character (if any) it reads on behalf of." \
  --type=task --priority=2 --parent holomush-095g
```

Record the returned bead ID; reference it in the PR description so reviewers can verify §7 step 13 is satisfied.

- [ ] **Step 2: Update the parent bead with a link**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd update <followup-bead-id> --notes="Filed per spec §7 step 13 / §8 second bullet. Blocks any future caller of holomush.query_stream_history against plugin-owned subjects."
```

---

## Phase 6: Verification & landing

### Task 13: Full CI mirror via `task pr-prep`

- [ ] **Step 1: Run `task pr-prep`**

```bash
task pr-prep
```

Expected: ALL CI jobs green — schema, license, lint (Go + proto + markdown), unit, integration, E2E. Per project memory `feedback_pr_prep_must_run`, this MUST be the full run; never substitute partial checks. Per `feedback_pr_prep_gate`, this MUST pass before any push.

If anything fails, fix at the root and re-run the full `task pr-prep`. Do NOT skip jobs.

- [ ] **Step 2: Squash all task commits into one PR commit**

```bash
jj log -r 'main@origin..@-' --no-graph -T 'change_id ++ " " ++ description.first_line()'
# review the chain; identify the change-ids to squash.
JJ_EDITOR=true jj --no-pager squash --from <oldest-change-id> --into <newest-change-id> -m "feat(plugin-audit): enforce scene-membership authz at PluginAuditService.QueryHistory

Closes the TODO at plugins/core-scenes/audit.go:211 by adding defense-in-depth
membership enforcement at the plugin boundary, complementing the existing
outer I-17 gate at internal/grpc/query_stream_history.go.

- Adds Actor caller=8 field to QueryHistoryRequest proto (additive).
- Plumbs caller from session through eventbus.HistoryQuery, the router,
  and into the plugin RPC.
- Plugin runs auth-first (caller validation → subject parse → IsMember
  membership check) before any cursor decode or log query, pinned by a
  TestQueryHistoryDeniesNonMemberWithoutHittingLogStore test.
- Plugin emits gRPC status.Error; router preserves status codes; host's
  mapHistoryError translates PermissionDenied to STREAM_ACCESS_DENIED so
  the client cannot distinguish outer-wall from plugin-wall denials.
- 6 integration cases prove the opacity invariant end-to-end and pin the
  fail-closed behavior at the plugin-host RPC boundary (§4.4).
- Follow-up bead <followup-bead-id> filed for the deferred plugin-as-caller
  identity design (spec §4.4 / §7 step 13).

Spec: docs/superpowers/specs/2026-04-23-plugin-history-authz-design.md
Closes holomush-095g.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 3: Set the bookmark and push**

```bash
jj bookmark set holomush-095g -r @-
jj git fetch
# Per project memory feedback_jj_rebase_targeted: NEVER bare jj rebase -d main.
jj rebase -r <our-change-id> -d main@origin
jj git push --branch holomush-095g
```

Expected: push succeeds; remote `holomush-095g` branch exists with the squashed commit.

- [ ] **Step 4: Open the PR**

```bash
gh pr create --title "feat(plugin-audit): enforce scene-membership authz at PluginAuditService.QueryHistory" \
  --body "$(cat <<'EOF'
## Summary
- Adds `Actor caller = 8` to `PluginAuditService.QueryHistory` proto and plumbs the field from the host session through `eventbus.HistoryQuery` → `PluginHistoryRouter` → plugin.
- Plugin enforces scene-membership authz (auth-first ordering, owner/member only, deliberate exclusion of `invited`) before any DB work.
- gRPC status codes from the plugin (`PermissionDenied`, `InvalidArgument`) round-trip through the router; `mapHistoryError` collapses plugin-boundary `PermissionDenied` into the same opaque `STREAM_ACCESS_DENIED` oops code the outer I-17 gate uses.
- Opacity invariant: client cannot distinguish outer-wall denials from plugin-wall denials (load-bearing test in §6.5 case 2).

## Spec
docs/superpowers/specs/2026-04-23-plugin-history-authz-design.md

## Test plan
- [x] `task test` (unit)
- [x] `task test:int` (integration, including 6 new authz cases)
- [x] `task pr-prep` (full CI mirror)

## Follow-up
- New bead: <followup-bead-id> — plugin-as-caller identity for `PluginHostService.QueryStreamHistory` against plugin-owned subjects (spec §4.4).

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 5: Close the integration bead and the parent bead**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd close <integration-bead-id>
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd update holomush-095g --status=in_review
```

Hold off on closing `holomush-095g` itself until the PR merges to `main`.

- [ ] **Step 6: Post the PR URL**

Print the PR URL the implementer will see from `gh pr create`. Done.

---

## Self-review notes for the implementer

If at any point you find that:

- A function name in this plan doesn't match the repo's current state — verify with `rg` before editing. Codebases drift.
- A test relies on a helper that doesn't exist — write the smallest helper that satisfies the test, named per the existing file's conventions.
- A `task` target named here doesn't exist — check `Taskfile.yaml` and adjust. Never substitute raw `go`/`golangci-lint`/`buf` commands per project rule.
- A commit fails a hook — fix the underlying issue and create a NEW commit. Do NOT use `--no-verify` per project rule.

If you hit a blocker that requires a design decision not in the spec, stop and surface the question rather than inventing a solution.
