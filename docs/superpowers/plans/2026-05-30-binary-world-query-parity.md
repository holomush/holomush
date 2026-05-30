<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Binary World-Query Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give binary plugins host-stamped `QueryLocation/QueryCharacter/QueryLocationCharacters/QueryObject` RPCs on `PluginHostService`, unify both plugin runtimes on a host-derived on-behalf-of (OBO) read subject, and delete the forgeable injectable `WorldService` gRPC path.

**Architecture:** Extend the accepted ADR holomush-qeypl pattern (host-derived `Evaluate` subject, no subject on the wire) to world reads. The acting subject is recovered host-side from the dispatch token (binary) or the VM-context actor (Lua), mapped through the single shared `pluginauthz.ActorSubject`, and passed to the existing `world.Service` ABAC chokepoint. The `WorldService` gRPC server existed only for plugin injection and is removed; `world.Service` (the in-process Go type) stays.

**Tech Stack:** Go, buf/protobuf (`task proto:generate`), gopher-lua, hashicorp/go-plugin, testify + Ginkgo (`//go:build integration`), `task` runner.

**Spec:** `docs/superpowers/specs/2026-05-30-binary-world-query-parity-design.md`

---

## File structure

| File | Responsibility | Action |
|------|----------------|--------|
| `api/proto/holomush/plugin/v1/plugin.proto` | 4 RPCs + request/response messages (no subject field) | Modify |
| `pkg/proto/holomush/plugin/v1/*.pb.go` | Generated bindings | Regenerate |
| `internal/plugin/goplugin/host.go` | `WorldReader` dep + `WithWorldService` option | Modify |
| `internal/plugin/goplugin/host_service.go` | 4 Query handlers + `readSubject` helper + proto mappers | Modify |
| `internal/plugin/goplugin/world_query_test.go` | Handler unit tests | Create |
| `internal/plugin/goplugin/world_query_invariants_test.go` | INV-1/INV-3/INV-4 meta-tests | Create |
| `internal/plugin/setup/subsystem.go` | Wire `WithWorldService`; remove registry injection | Modify |
| `internal/plugin/setup/world_conn.go` | The injection conn builder | Delete |
| `internal/world/grpc_server.go` + `grpc_server_test.go` | Dead WorldService gRPC server (forgery surface) | Delete (pending proto check) |
| `internal/plugin/hostfunc/helpers.go` | OBO read-subject helper | Modify |
| `internal/plugin/hostfunc/world.go` | 4 query fns → OBO subject | Modify |
| `internal/plugin/hostfunc/world_test.go` | Lua OBO tests | Modify |
| `pkg/plugin/world_client.go` + `world_client_test.go` | `WorldQuerier` facade + `WorldQuerierAware` | Create |
| `plugins/core-scenes/plugin.yaml` | Drop unused `requires` | Modify |
| `test/integration/plugin/binary_plugin_test.go` | Re-vehicle WorldService-coupled tests | Modify |
| `site/src/content/docs/extending/tutorials/binary-plugins.md` | Replace WorldService `requires` example | Modify |
| `site/src/content/docs/reference/grpc-api.md` | Regenerated reference | Regenerate |

---

## Phase 1: Proto surface

### Task 1: Add the four Query RPCs to PluginHostService

**Files:**

- Modify: `api/proto/holomush/plugin/v1/plugin.proto`
- Regenerate: `pkg/proto/holomush/plugin/v1/`

- [ ] **Step 1: Add the RPCs to `service PluginHostService`**

After the `Evaluate` RPC (`plugin.proto:190`), add:

```protobuf
  // QueryLocation returns one location's identity snapshot. The host resolves
  // it through world.Service.GetLocation under the acting subject derived from
  // the dispatch token (the invoking character for command/event dispatch, the
  // plugin itself for plugin-initiated reads). No subject is accepted on the
  // wire; ABAC is enforced at the world-service layer.
  rpc QueryLocation(PluginHostServiceQueryLocationRequest) returns (PluginHostServiceQueryLocationResponse);

  // QueryCharacter returns one character's identity snapshot via
  // world.Service.GetCharacter under the host-derived acting subject. location_id
  // is empty when the character is not in the world. No subject on the wire.
  rpc QueryCharacter(PluginHostServiceQueryCharacterRequest) returns (PluginHostServiceQueryCharacterResponse);

  // QueryLocationCharacters returns the {id, name} roster of characters at a
  // location via world.Service.GetCharactersByLocation under the host-derived
  // acting subject. The roster is empty when none are present. No subject on the wire.
  rpc QueryLocationCharacters(PluginHostServiceQueryLocationCharactersRequest) returns (PluginHostServiceQueryLocationCharactersResponse);

  // QueryObject returns one object's identity snapshot via world.Service.GetObject
  // under the host-derived acting subject. location_id is empty when the object
  // is not placed in the world. No subject on the wire.
  rpc QueryObject(PluginHostServiceQueryObjectRequest) returns (PluginHostServiceQueryObjectResponse);
```

- [ ] **Step 2: Add the request/response messages**

At the end of `plugin.proto` (before the closing of the file's message block — match surrounding style):

```protobuf
// PluginHostServiceQueryLocationRequest names the location to snapshot. It
// carries no subject: the host derives the acting subject from the dispatch
// token (INV-1).
message PluginHostServiceQueryLocationRequest {
  // location_id is the ULID of the location to read.
  string location_id = 1;
}

// PluginHostServiceQueryLocationResponse returns the requested location's
// identity fields.
message PluginHostServiceQueryLocationResponse {
  // id is the location ULID.
  string id = 1;
  // name is the location's display name.
  string name = 2;
  // description is the location's prose description.
  string description = 3;
}

// PluginHostServiceQueryCharacterRequest names the character to snapshot;
// carries no subject (INV-1).
message PluginHostServiceQueryCharacterRequest {
  // character_id is the ULID of the character to read.
  string character_id = 1;
}

// PluginHostServiceQueryCharacterResponse returns the requested character's
// identity fields. location_id is empty when the character is not in the world.
message PluginHostServiceQueryCharacterResponse {
  // id is the character ULID.
  string id = 1;
  // player_id is the owning player's ULID.
  string player_id = 2;
  // name is the character's display name.
  string name = 3;
  // description is the character's prose description.
  string description = 4;
  // location_id is the character's current location ULID, empty if none.
  string location_id = 5;
}

// PluginHostServiceQueryLocationCharactersRequest names the location whose
// occupant roster to return; carries no subject (INV-1).
message PluginHostServiceQueryLocationCharactersRequest {
  // location_id is the ULID of the location whose roster to read.
  string location_id = 1;
}

// PluginHostServiceCharacterRef is one entry in a character roster: id + name
// only. Use QueryCharacter to fetch full detail.
message PluginHostServiceCharacterRef {
  // id is the character ULID.
  string id = 1;
  // name is the character's display name.
  string name = 2;
}

// PluginHostServiceQueryLocationCharactersResponse returns the location's
// occupant roster; empty when the location holds no characters.
message PluginHostServiceQueryLocationCharactersResponse {
  // characters is the {id, name} roster at the location.
  repeated PluginHostServiceCharacterRef characters = 1;
}

// PluginHostServiceQueryObjectRequest names the object to snapshot; carries no
// subject (INV-1).
message PluginHostServiceQueryObjectRequest {
  // object_id is the ULID of the object to read.
  string object_id = 1;
}

// PluginHostServiceQueryObjectResponse returns the requested object's identity
// fields. location_id is empty when the object is not placed.
message PluginHostServiceQueryObjectResponse {
  // id is the object ULID.
  string id = 1;
  // name is the object's display name.
  string name = 2;
  // description is the object's prose description.
  string description = 3;
  // location_id is the object's current location ULID, empty if none.
  string location_id = 4;
}
```

- [ ] **Step 3: Regenerate bindings**

Run: `task proto:generate`
Expected: `pkg/proto/holomush/plugin/v1/plugin.pb.go` + `plugin_grpc.pb.go` updated with the new types; no error.

- [ ] **Step 4: Verify proto lint (doc-comment + name-echo gates)**

Run: `task lint:proto`
Expected: PASS (every new element has a non-name-echo doc comment).

- [ ] **Step 5: Verify build**

Run: `task build`
Expected: PASS (generated server interface now declares the 4 methods; `pluginHostServiceServer` will fail to satisfy it until Task 5 — if `task build` fails here on the unimplemented methods, that is expected and resolved in Task 5; commit the proto first).

- [ ] **Step 6: Commit**

`jj describe -m "feat(plugin): add world-query RPCs to PluginHostService proto (holomush-q42fh)"` then `jj new`.

### Task 2: INV-1 meta-test — no subject field on Query requests

**Files:**

- Create: `internal/plugin/goplugin/world_query_invariants_test.go`

- [ ] **Step 1: Write the failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/reflect/protoreflect"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// TestINV1WorldQueryRequestsHaveNoSubjectField asserts none of the four
// world-query request messages carry a subject field — the acting subject is
// host-derived from the dispatch token, never plugin-supplied (INV-1).
func TestINV1WorldQueryRequestsHaveNoSubjectField(t *testing.T) {
	descs := []protoreflect.MessageDescriptor{
		(&pluginv1.PluginHostServiceQueryLocationRequest{}).ProtoReflect().Descriptor(),
		(&pluginv1.PluginHostServiceQueryCharacterRequest{}).ProtoReflect().Descriptor(),
		(&pluginv1.PluginHostServiceQueryLocationCharactersRequest{}).ProtoReflect().Descriptor(),
		(&pluginv1.PluginHostServiceQueryObjectRequest{}).ProtoReflect().Descriptor(),
	}
	for _, d := range descs {
		fields := d.Fields()
		for i := range fields.Len() {
			assert.NotEqual(t, "subject", string(fields.Get(i).Name()),
				"%s MUST NOT carry a subject field (INV-1)", d.Name())
		}
		assert.Equal(t, 1, fields.Len(),
			"%s MUST carry exactly one field (the entity id) (INV-1)", d.Name())
	}
}
```

- [ ] **Step 2: Run to verify it passes** (the proto from Task 1 already satisfies it)

Run: `task test -- -run TestINV1WorldQuery ./internal/plugin/goplugin/`
Expected: PASS. (This is a lock, not a red-first test — it guards against a future subject-field addition.)

- [ ] **Step 3: Commit**

`jj describe -m "test(plugin): INV-1 lock — world-query requests carry no subject (holomush-q42fh)"` then `jj new`.

---

## Phase 2: Binary handlers

### Task 3: Thread a world reader into the plugin Host

**Files:**

- Modify: `internal/plugin/goplugin/host.go`
- Modify: `internal/plugin/setup/subsystem.go`

- [ ] **Step 1: Define the `WorldReader` interface and `worldQuerier` field**

In `host.go`, near the other option funcs (`WithCommandQuerier` at `:201`), add:

```go
// WorldReader is the read-only slice of world.Service the plugin host needs to
// serve world-query RPCs. *world.Service satisfies it.
type WorldReader interface {
	GetLocation(ctx context.Context, subjectID string, id ulid.ULID) (*world.Location, error)
	GetCharacter(ctx context.Context, subjectID string, id ulid.ULID) (*world.Character, error)
	GetCharactersByLocation(ctx context.Context, subjectID string, locationID ulid.ULID, opts world.ListOptions) ([]*world.Character, error)
	GetObject(ctx context.Context, subjectID string, id ulid.ULID) (*world.Object, error)
}

// WithWorldService injects the world reader used by the world-query host RPCs.
func WithWorldService(w WorldReader) HostOption {
	return func(h *Host) { h.worldQuerier = w }
}
```

Add the field to `type Host struct` (alongside `commandQuerier` at `:221`):

```go
	worldQuerier      WorldReader
```

(Add the imports `"github.com/holomush/holomush/internal/world"` and `"github.com/oklog/ulid/v2"` to `host.go` if not already present.)

- [ ] **Step 2: Wire it in subsystem.go**

In `internal/plugin/setup/subsystem.go`, where the goplugin host is constructed (the block with `goplugin.WithServiceRegistry(s.registry)` at `:274`), add the option:

```go
		goplugin.WithWorldService(s.cfg.World.Service()),
```

- [ ] **Step 3: Verify build**

Run: `task build`
Expected: PASS.

- [ ] **Step 4: Commit**

`jj describe -m "feat(plugin): inject WorldReader into plugin Host (holomush-q42fh)"` then `jj new`.

### Task 4: `readSubject` helper — host-derived OBO subject from the dispatch token

**Files:**

- Modify: `internal/plugin/goplugin/host_service.go`
- Create: `internal/plugin/goplugin/world_query_test.go`

- [ ] **Step 1: Write the failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/errutil"
)

// ctxWithToken issues a dispatch token for the given actor and returns an
// incoming-metadata context carrying it (mirrors how DeliverCommand wires the
// plugin call).
func ctxWithToken(t *testing.T, h *Host, plugin string, actor core.Actor) context.Context {
	t.Helper()
	tok, err := h.tokenStore.Issue(plugin, actor)
	require.NoError(t, err)
	md := metadata.New(map[string]string{"x-holomush-emit-token": tok})
	return metadata.NewIncomingContext(context.Background(), md)
}

func TestReadSubjectDerivesCharacterSubjectFromToken(t *testing.T) {
	h := newTestHostWithTokenStore(t) // helper defined in Step 3
	s := &pluginHostServiceServer{host: h, pluginName: "p"}
	charID := core.NewULID().String()
	ctx := ctxWithToken(t, h, "p", core.Actor{Kind: core.ActorCharacter, ID: charID})

	subject, err := s.readSubject(ctx)

	require.NoError(t, err)
	assert.Equal(t, "character:"+charID, subject)
}

func TestReadSubjectFailsClosedWithoutToken(t *testing.T) {
	h := newTestHostWithTokenStore(t)
	s := &pluginHostServiceServer{host: h, pluginName: "p"}

	_, err := s.readSubject(context.Background())

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_MISSING")
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestReadSubject ./internal/plugin/goplugin/`
Expected: FAIL — `s.readSubject` undefined and `newTestHostWithTokenStore` undefined.

- [ ] **Step 3: Implement `readSubject` and the test host helper**

In `host_service.go` add (reusing the exact token-recovery shape from `Evaluate` at `:543-570`):

```go
// readSubject recovers the host-vouched ABAC subject for a read-only host RPC
// from the dispatch token, mirroring Evaluate's token→actor recovery. The
// subject is NEVER taken from the request (INV-1/INV-2). Fails closed.
func (s *pluginHostServiceServer) readSubject(ctx context.Context) (string, error) {
	if s.host == nil {
		return "", oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	s.host.mu.RLock()
	tokenStore := s.host.tokenStore
	s.host.mu.RUnlock()

	md, _ := metadata.FromIncomingContext(ctx)
	tokens := md.Get("x-holomush-emit-token")
	if len(tokens) == 0 || tokens[0] == "" {
		return "", oops.Code("EMIT_TOKEN_MISSING").
			With("plugin", s.pluginName).
			Errorf("plugin read without a host-issued dispatch token")
	}
	if tokenStore == nil {
		return "", oops.Code("EMIT_TOKEN_STORE_UNCONFIGURED").
			With("plugin", s.pluginName).
			Errorf("plugin token store is not configured")
	}
	storedActor, ok := tokenStore.Lookup(s.pluginName, tokens[0])
	if !ok {
		return "", oops.Code("EMIT_TOKEN_REJECTED").
			With("plugin", s.pluginName).
			Errorf("dispatch token is not valid for this plugin")
	}
	subject := pluginauthz.ActorSubject(storedActor)
	if subject == "" {
		return "", oops.Code("WORLD_QUERY_NO_SUBJECT").
			With("plugin", s.pluginName).
			Errorf("no acting subject for world read")
	}
	return subject, nil
}
```

In `world_query_test.go` add the host helper (construct a Host with a real token store; mirror the existing test-host construction in `host_service_test.go`):

```go
func newTestHostWithTokenStore(t *testing.T) *Host {
	t.Helper()
	h := NewHost() // token store is initialized by NewHost; confirm against host.go
	t.Cleanup(func() { _ = h.Close() })
	return h
}
```

(If `NewHost()` does not initialize `tokenStore`, follow the construction used by the existing `TestEmitEvent*`/`TestEvaluate*` tests in `host_service_test.go` — match that exact setup.)

- [ ] **Step 4: Run to verify it passes**

Run: `task test -- -run TestReadSubject ./internal/plugin/goplugin/`
Expected: PASS.

- [ ] **Step 5: Commit**

`jj describe -m "feat(plugin): readSubject helper derives OBO subject from dispatch token (holomush-q42fh)"` then `jj new`.

### Task 5: Implement the four Query handlers + proto mappers

**Files:**

- Modify: `internal/plugin/goplugin/host_service.go`
- Modify: `internal/plugin/goplugin/world_query_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `world_query_test.go`:

```go
func TestQueryLocationReturnsLocationWhenAuthorized(t *testing.T) {
	h, eng, repo := newTestHostWithWorld(t) // helper in Step 3
	locID := core.NewULID()
	charID := core.NewULID().String()
	eng.Grant("character:"+charID, "read", "location:"+locID.String())
	repo.EXPECT().Get(mock.Anything, locID).Return(&world.Location{ID: locID, Name: "Square", Description: "A square."}, nil)
	s := &pluginHostServiceServer{host: h, pluginName: "p"}
	ctx := ctxWithToken(t, h, "p", core.Actor{Kind: core.ActorCharacter, ID: charID})

	resp, err := s.QueryLocation(ctx, &pluginv1.PluginHostServiceQueryLocationRequest{LocationId: locID.String()})

	require.NoError(t, err)
	assert.Equal(t, locID.String(), resp.GetId())
	assert.Equal(t, "Square", resp.GetName())
	assert.Equal(t, "A square.", resp.GetDescription())
}

func TestQueryLocationDeniedForUnauthorizedSubject(t *testing.T) {
	h, _, _ := newTestHostWithWorld(t) // no grant
	locID := core.NewULID()
	s := &pluginHostServiceServer{host: h, pluginName: "p"}
	ctx := ctxWithToken(t, h, "p", core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()})

	_, err := s.QueryLocation(ctx, &pluginv1.PluginHostServiceQueryLocationRequest{LocationId: locID.String()})

	require.Error(t, err)
}

func TestQueryLocationInvalidIDRejected(t *testing.T) {
	h, _, _ := newTestHostWithWorld(t)
	s := &pluginHostServiceServer{host: h, pluginName: "p"}
	ctx := ctxWithToken(t, h, "p", core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()})

	_, err := s.QueryLocation(ctx, &pluginv1.PluginHostServiceQueryLocationRequest{LocationId: "not-a-ulid"})

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_ARGUMENT")
}
```

(Add equivalent happy-path + denial tests for `QueryCharacter`, `QueryLocationCharacters` — including an empty-roster case — and `QueryObject`, following the same shape. For `QueryCharacter`, assert the nullable `location_id` is empty when `Character.LocationID == nil`.)

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestQuery ./internal/plugin/goplugin/`
Expected: FAIL — handlers + `newTestHostWithWorld` undefined.

- [ ] **Step 3: Implement the handlers + mappers**

In `host_service.go`:

```go
// worldQuerier returns the host's world reader under the read lock.
func (s *pluginHostServiceServer) worldQuerier() WorldReader {
	s.host.mu.RLock()
	defer s.host.mu.RUnlock()
	return s.host.worldQuerier
}

func (s *pluginHostServiceServer) QueryLocation(ctx context.Context, req *pluginv1.PluginHostServiceQueryLocationRequest) (*pluginv1.PluginHostServiceQueryLocationResponse, error) {
	subject, err := s.readSubject(ctx)
	if err != nil {
		return nil, err
	}
	wq := s.worldQuerier()
	if wq == nil {
		return nil, oops.Code("WORLD_READER_UNCONFIGURED").With("plugin", s.pluginName).Errorf("world reader not configured")
	}
	id, err := ulid.Parse(req.GetLocationId())
	if err != nil {
		return nil, oops.Code("INVALID_ARGUMENT").With("plugin", s.pluginName).Errorf("invalid location_id")
	}
	loc, err := wq.GetLocation(ctx, subject, id)
	if err != nil {
		return nil, mapWorldQueryError(s.pluginName, err)
	}
	return &pluginv1.PluginHostServiceQueryLocationResponse{
		Id: loc.ID.String(), Name: loc.Name, Description: loc.Description,
	}, nil
}

func (s *pluginHostServiceServer) QueryCharacter(ctx context.Context, req *pluginv1.PluginHostServiceQueryCharacterRequest) (*pluginv1.PluginHostServiceQueryCharacterResponse, error) {
	subject, err := s.readSubject(ctx)
	if err != nil {
		return nil, err
	}
	wq := s.worldQuerier()
	if wq == nil {
		return nil, oops.Code("WORLD_READER_UNCONFIGURED").With("plugin", s.pluginName).Errorf("world reader not configured")
	}
	id, err := ulid.Parse(req.GetCharacterId())
	if err != nil {
		return nil, oops.Code("INVALID_ARGUMENT").With("plugin", s.pluginName).Errorf("invalid character_id")
	}
	c, err := wq.GetCharacter(ctx, subject, id)
	if err != nil {
		return nil, mapWorldQueryError(s.pluginName, err)
	}
	resp := &pluginv1.PluginHostServiceQueryCharacterResponse{
		Id: c.ID.String(), PlayerId: c.PlayerID.String(), Name: c.Name, Description: c.Description,
	}
	if c.LocationID != nil {
		resp.LocationId = c.LocationID.String()
	}
	return resp, nil
}

func (s *pluginHostServiceServer) QueryLocationCharacters(ctx context.Context, req *pluginv1.PluginHostServiceQueryLocationCharactersRequest) (*pluginv1.PluginHostServiceQueryLocationCharactersResponse, error) {
	subject, err := s.readSubject(ctx)
	if err != nil {
		return nil, err
	}
	wq := s.worldQuerier()
	if wq == nil {
		return nil, oops.Code("WORLD_READER_UNCONFIGURED").With("plugin", s.pluginName).Errorf("world reader not configured")
	}
	id, err := ulid.Parse(req.GetLocationId())
	if err != nil {
		return nil, oops.Code("INVALID_ARGUMENT").With("plugin", s.pluginName).Errorf("invalid location_id")
	}
	chars, err := wq.GetCharactersByLocation(ctx, subject, id, world.ListOptions{})
	if err != nil {
		return nil, mapWorldQueryError(s.pluginName, err)
	}
	refs := make([]*pluginv1.PluginHostServiceCharacterRef, len(chars))
	for i, c := range chars {
		refs[i] = &pluginv1.PluginHostServiceCharacterRef{Id: c.ID.String(), Name: c.Name}
	}
	return &pluginv1.PluginHostServiceQueryLocationCharactersResponse{Characters: refs}, nil
}

func (s *pluginHostServiceServer) QueryObject(ctx context.Context, req *pluginv1.PluginHostServiceQueryObjectRequest) (*pluginv1.PluginHostServiceQueryObjectResponse, error) {
	subject, err := s.readSubject(ctx)
	if err != nil {
		return nil, err
	}
	wq := s.worldQuerier()
	if wq == nil {
		return nil, oops.Code("WORLD_READER_UNCONFIGURED").With("plugin", s.pluginName).Errorf("world reader not configured")
	}
	id, err := ulid.Parse(req.GetObjectId())
	if err != nil {
		return nil, oops.Code("INVALID_ARGUMENT").With("plugin", s.pluginName).Errorf("invalid object_id")
	}
	o, err := wq.GetObject(ctx, subject, id)
	if err != nil {
		return nil, mapWorldQueryError(s.pluginName, err)
	}
	resp := &pluginv1.PluginHostServiceQueryObjectResponse{
		Id: o.ID.String(), Name: o.Name, Description: o.Description,
	}
	// NB: world.Object exposes locationID via the LocationID() accessor
	// (object.go:135), unlike Location/Character which use public fields.
	if lid := o.LocationID(); lid != nil {
		resp.LocationId = lid.String()
	}
	return resp, nil
}

// mapWorldQueryError sanitizes a world.Service error for the plugin wire
// boundary: it logs nothing here (callers log) and wraps with a generic code,
// never leaking internal detail (.claude/rules/grpc-errors.md).
func mapWorldQueryError(plugin string, err error) error {
	return oops.Code("WORLD_QUERY_FAILED").With("plugin", plugin).Wrap(err)
}
```

Add the `newTestHostWithWorld` helper to `world_query_test.go`, constructing a `Host` with `WithWorldService(world.NewService(...))` backed by a `policytest` engine and `worldtest` mock repos (mirror `internal/world/service_test.go` setup). Confirm `world.Object` has a `LocationID *ulid.ULID` field via `mcp__probe__extract_code world.Object`; if the field name differs, adjust the mapper.

- [ ] **Step 4: Run to verify it passes**

Run: `task test -- -run TestQuery ./internal/plugin/goplugin/`
Expected: PASS.

- [ ] **Step 5: Verify the server now satisfies the generated interface**

Run: `task build`
Expected: PASS (`pluginHostServiceServer` implements all 4 methods).

- [ ] **Step 6: Commit**

`jj describe -m "feat(plugin): serve world-query RPCs with host-derived OBO subject (holomush-q42fh)"` then `jj new`.

---

## Phase 3: Lua OBO refactor

### Task 6: Switch the Lua query fns to a host-derived OBO subject

**Files:**

- Modify: `internal/plugin/hostfunc/helpers.go`
- Modify: `internal/plugin/hostfunc/world.go`
- Modify: `internal/plugin/hostfunc/world_test.go`

- [ ] **Step 1: Write the failing test**

Append to `world_test.go` (mirror `evaluate_test.go`'s context-actor setup):

```go
func TestQueryLocationUsesActingCharacterSubject(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID()
	locID := core.NewULID()
	eng := policytest.NewGrantEngine()
	eng.Grant("character:"+charID.String(), "read", "location:"+locID.String())
	repo := worldtest.NewMockLocationRepository(t)
	repo.EXPECT().Get(mock.Anything, locID).Return(&world.Location{ID: locID, Name: "Square"}, nil)
	svc := world.NewService(world.ServiceConfig{LocationRepo: repo, Engine: eng})
	hf := hostfunc.New(nil, hostfunc.WithWorldService(svc))
	L.SetContext(core.WithActor(context.Background(), core.Actor{Kind: core.ActorCharacter, ID: charID.String()}))
	hf.Register(L, "lua-plug")

	require.NoError(t, L.DoString(`loc = holomush.query_location("`+locID.String()+`")`))
	// Assert the read succeeded under character:<id>, not plugin:lua-plug.
	tbl := L.GetGlobal("loc").(*lua.LTable)
	assert.Equal(t, "Square", tbl.RawGetString("name").String())
}

func TestQueryLocationFailsClosedWithoutActor(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	svc := world.NewService(world.ServiceConfig{
		LocationRepo: worldtest.NewMockLocationRepository(t),
		Engine: policytest.NewGrantEngine(),
	})
	hf := hostfunc.New(nil, hostfunc.WithWorldService(svc))
	L.SetContext(context.Background()) // no actor
	hf.Register(L, "lua-plug")

	require.NoError(t, L.DoString(`loc, err = holomush.query_location("`+core.NewULID().String()+`")`))
	assert.Equal(t, lua.LNil, L.GetGlobal("loc"))
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"))
}
```

(If the existing `world_test.go` asserts `plugin:<name>` subject for queries, those assertions encode the OLD behavior and MUST be updated to the OBO subject — this is the latent-gap fix, not a regression.)

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestQueryLocation ./internal/plugin/hostfunc/`
Expected: FAIL — `query_location` still reads under `plugin:lua-plug`, so the `character:<id>` grant doesn't authorize it (permission denied) and `loc` is nil.

- [ ] **Step 3: Add an OBO read-subject helper**

In `helpers.go`, add alongside `withQueryContext` (`:84`):

```go
// withReadSubject derives the host-stamped acting subject from the VM context
// actor (OBO: the invoking character during dispatch; the plugin itself for
// plugin-initiated reads) and invokes fn with it. Fails closed (pushes a Lua
// error and returns) when no actor is on the context — the subject is NEVER
// the hard-coded plugin identity (INV-2). Mirrors evaluateFn's derivation.
func (f *Functions) withReadSubject(
	L *lua.LState,
	funcName string,
	fn func(ctx context.Context, subjectID string) int,
) int {
	parentCtx := L.Context()
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, defaultPluginQueryTimeout)
	defer cancel()

	actor, ok := core.ActorFromContext(ctx)
	if !ok {
		return pushError(L, funcName+": no acting subject for world read")
	}
	subject := pluginauthz.ActorSubject(actor)
	if subject == "" {
		return pushError(L, funcName+": no acting subject for world read")
	}
	return fn(ctx, subject)
}
```

The error helper is `pushError(L *lua.LState, errMsg string) int` (`helpers.go:23`) —
the only error-push idiom in the package; there is no `pushQueryError`. Add
imports `core` (`internal/core`) and `pluginauthz` (`internal/plugin/pluginauthz`)
if absent. `defaultPluginQueryTimeout` already exists (used by `withQueryContext`).

- [ ] **Step 4: Rewrite the four query fns to use it**

This is a **minimal diff per fn**, not a rewrite: swap the `withQueryContext`
(adapter, `plugin:<name>`) wrapper for `withReadSubject` (OBO subject), and
change the data-source call from `adapter.GetX(ctx, id)` to
`f.worldMutator.GetX(ctx, subject, id)`. **Keep each fn's existing Lua-table
marshalling block verbatim** — only the wrapper and the call line change.
`queryLocationFn` becomes:

```go
func (f *Functions) queryLocationFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		id, perr := ulid.Parse(L.CheckString(1))
		if perr != nil {
			return pushError(L, "query_location: invalid location id")
		}
		return f.withReadSubject(L, "query_location", func(ctx context.Context, subject string) int {
			loc, err := f.worldMutator.GetLocation(ctx, subject, id)
			if err != nil {
				return pushError(L, "query_location: "+err.Error())
			}
			// <-- keep the EXISTING loc→Lua-table marshalling that currently
			//     follows `adapter.GetLocation(ctx, id)` in this fn, verbatim.
			return /* existing marshalled push */ 0
		})
	}
}
```

Apply the same minimal diff to `queryCharacterFn`, `queryLocationCharactersFn`,
`queryObjectFn`: change their inner call to `f.worldMutator.GetCharacter(ctx,
subject, id)` / `GetCharactersByLocation(ctx, subject, id, opts)` /
`GetObject(ctx, subject, id)` respectively, wrap in `f.withReadSubject(L,
"<fn-name>", …)`, and keep each fn's current marshalling block unchanged. Remove
the `withQueryContext` calls from these four fns. The `pluginName` parameter
stays in each fn's signature (registration-fixed) even if now unused in the OBO
path — unused function parameters are legal in Go. Leave `withMutatorContext`
and the write fns untouched (mutations are out of scope and remain
`plugin:<name>`).

- [ ] **Step 5: Remove now-dead read code from the adapter**

**`withQueryContext` MUST NOT be deleted** — it has non-query callers
`findLocationFn` (`world_write.go:207`) and `getPropertyFn` (`world_write.go:366`).
Only stop the four query fns from using it. For `WorldQuerierAdapter`: if its read
methods (`GetLocation`/`GetCharacter`/`GetCharactersByLocation`/`GetObject` in
`adapter.go`) have no remaining callers after Step 4, delete those read methods;
keep the adapter type itself if `withMutatorContext`/write paths still construct
it. Confirm caller sets with `mcp__probe__search_code "WorldQuerierAdapter"` and
run `task lint` to surface any unused-symbol errors.

- [ ] **Step 6: Run to verify it passes**

Run: `task test -- -run TestQuery ./internal/plugin/hostfunc/`
Expected: PASS.

- [ ] **Step 7: Commit**

`jj describe -m "fix(plugin): Lua world queries read under acting subject, not plugin identity (holomush-q42fh)"` then `jj new`.

---

## Phase 4: SDK facade

### Task 7: `WorldQuerier` plugin-facing facade

**Files:**

- Create: `pkg/plugin/world_client.go`
- Create: `pkg/plugin/world_client_test.go`

- [ ] **Step 1: Write the failing test**

Mirror the real `evaluate_client_test.go` pattern exactly — a server type
embedding `UnimplementedPluginHostServiceServer`, started via the existing
`startPluginHostServiceTestServer(t, srv)` helper, with the token captured from
incoming metadata (the package-private `emitTokenHeader` const,
`event_sink.go:23`). There is no client stub type in this package.

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

type worldTestServer struct {
	pluginv1.UnimplementedPluginHostServiceServer
	gotToken string
}

func (s *worldTestServer) QueryLocation(ctx context.Context, req *pluginv1.PluginHostServiceQueryLocationRequest) (*pluginv1.PluginHostServiceQueryLocationResponse, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if tokens := md.Get(emitTokenHeader); len(tokens) > 0 {
			s.gotToken = tokens[0]
		}
	}
	return &pluginv1.PluginHostServiceQueryLocationResponse{Id: req.GetLocationId(), Name: "Square", Description: "A square."}, nil
}

func TestWorldQuerierGetLocationForwardsTokenAndMaps(t *testing.T) {
	srv := &worldTestServer{}
	conn := startPluginHostServiceTestServer(t, srv)
	c := &hostWorldClient{client: pluginv1.NewPluginHostServiceClient(conn)}
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs(emitTokenHeader, "dispatch-token-abc"))

	loc, err := c.GetLocation(ctx, "01LOC")

	require.NoError(t, err)
	assert.Equal(t, "01LOC", loc.ID)
	assert.Equal(t, "Square", loc.Name)
	assert.Equal(t, "dispatch-token-abc", srv.gotToken) // token ferried incoming→outgoing
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestWorldQuerier ./pkg/plugin/`
Expected: FAIL — `hostWorldClient` / `WorldQuerier` undefined.

- [ ] **Step 3: Implement the facade**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"

	"github.com/samber/oops"
	"google.golang.org/grpc/metadata"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// Location/Character/Object/CharacterRef are the plugin-facing world snapshots
// returned by WorldQuerier. They are decoupled from the wire types.
type Location struct{ ID, Name, Description string }
type Character struct{ ID, PlayerID, Name, Description, LocationID string }
type Object struct{ ID, Name, Description, LocationID string }
type CharacterRef struct{ ID, Name string }

// WorldQuerier is the read-only world surface exposed to binary plugins. The
// host derives the acting subject from the dispatch context; the plugin passes
// only the entity id (never a subject).
type WorldQuerier interface {
	GetLocation(ctx context.Context, locationID string) (Location, error)
	GetCharacter(ctx context.Context, characterID string) (Character, error)
	GetLocationCharacters(ctx context.Context, locationID string) ([]CharacterRef, error)
	GetObject(ctx context.Context, objectID string) (Object, error)
}

// WorldQuerierAware is implemented by service providers to receive a
// WorldQuerier during Init, parallel to HostEvaluatorAware.
type WorldQuerierAware interface{ SetWorldQuerier(WorldQuerier) }

type hostWorldClient struct{ client pluginv1.PluginHostServiceClient }

func (c *hostWorldClient) GetLocation(ctx context.Context, locationID string) (Location, error) {
	if c.client == nil {
		return Location{}, oops.New("host world client is not configured")
	}
	resp, err := c.client.QueryLocation(ferryToken(ctx), &pluginv1.PluginHostServiceQueryLocationRequest{LocationId: locationID})
	if err != nil {
		return Location{}, oops.With("location_id", locationID).Wrap(err)
	}
	return Location{ID: resp.GetId(), Name: resp.GetName(), Description: resp.GetDescription()}, nil
}

// (GetCharacter, GetLocationCharacters, GetObject follow the same shape:
// ferryToken(ctx), call the matching RPC, map the response struct.)
```

Add a `ferryToken(ctx)` helper that copies the incoming `x-holomush-emit-token` onto the outgoing context (extract the exact logic from `evaluate_client.go:64-75` into a shared helper and have both call it — DRY).

- [ ] **Step 4: Run to verify it passes**

Run: `task test -- -run TestWorldQuerier ./pkg/plugin/`
Expected: PASS.

- [ ] **Step 5: Wire `WorldQuerierAware` injection**

Find where `HostEvaluatorAware` / `FocusClientAware` are injected during plugin Init (`mcp__probe__search_code "SetHostEvaluator"`), and inject `SetWorldQuerier(&hostWorldClient{client: ...})` at the same site. Add a test asserting a provider implementing `WorldQuerierAware` receives a non-nil querier.

- [ ] **Step 6: Commit**

`jj describe -m "feat(plugin-sdk): WorldQuerier facade for binary plugins (holomush-q42fh)"` then `jj new`.

---

## Phase 5: Remove the forgery surface

### Task 8: Delete the injectable WorldService and its dead gRPC server

**Files:**

- Modify: `internal/plugin/setup/subsystem.go`
- Delete: `internal/plugin/setup/world_conn.go`
- Delete: `internal/world/grpc_server.go`, `internal/world/grpc_server_test.go` (pending proto check)
- Modify: `plugins/core-scenes/plugin.yaml`

- [ ] **Step 1: Check for any other consumer of the WorldService gRPC/Connect surface**

Run: `mcp__probe__search_code "NewWorldServiceHandler"` and `rg -rn 'worldv1connect|RegisterWorldServiceServer|WorldServiceClient' --type go`
Expected: the only references are `world_conn.go`, `grpc_server.go`, and their tests. Record the result. If a real consumer exists (e.g., a mounted Connect handler), STOP and keep the proto + server, deleting only the registry injection; otherwise proceed to delete the server too.

- [ ] **Step 2: Remove the registry injection in subsystem.go**

Delete, in `subsystem.go`: the `worldConn` struct field (`:134`); the
`newWorldInProcessConn` construction + error handling (`:214-218`); the
deferred/teardown `s.worldConn.Close()` handling (`:234-235`); the
`s.registry.Register(plugins.RegisteredService{Name: "holomush.world.v1.WorldService", Conn: worldConn})`
block (`:238-244`); and the final `s.worldConn.Close()` in the subsystem's
`Close`/shutdown (`:432-434`). Keep `hostfunc.WithWorldService(s.cfg.World.Service())`
(`:188`) and the new `goplugin.WithWorldService(...)` (Task 3) — both read
`s.cfg.World.Service()` directly and are independent of the registry injection.

- [ ] **Step 3: Delete the dead files**

```text
rm internal/plugin/setup/world_conn.go
rm internal/world/grpc_server.go internal/world/grpc_server_test.go
```

(Only delete `grpc_server.go`/`_test.go` if Step 1 found no other consumer.)

- [ ] **Step 4: Drop the unused requires from core-scenes**

In `plugins/core-scenes/plugin.yaml`, remove the line `- holomush.world.v1.WorldService` under `requires:`. If `requires:` becomes empty, set it to `requires: []` or remove the key per the schema (`task lint` validates `schemas/plugin.schema.json`).

- [ ] **Step 5: Verify build + lint**

Run: `task build && task lint`
Expected: PASS. No unused-symbol or dangling-import errors.

- [ ] **Step 6: Commit**

`jj describe -m "refactor(plugin): remove forgeable injectable WorldService gRPC path (holomush-q42fh)"` then `jj new`.

### Task 9: Re-vehicle the WorldService-coupled integration tests

**Files:**

- Modify: `test/integration/plugin/binary_plugin_test.go`

- [ ] **Step 1: Remove the WorldService fixtures**

Delete the five WorldService registry registrations (around `:188, 275, 396, 552, 845`) and the manifest assertion at `:131` (`Requires` contains `holomush.world.v1.WorldService`). These existed only to satisfy core-scenes' now-removed `requires`.

- [ ] **Step 2: Re-vehicle the "fails to load" test**

Rewrite the test at `:330-357` so the unmet-requires DAG path is still covered: load a binary whose in-test manifest declares `requires: [holomush.nonexistent.v1.FakeService]` (use the existing core-scenes binary with an in-test manifest override, or a `testdata/` fixture binary). Assert the load error contains `holomush.nonexistent.v1.FakeService`. The registry-resolve check fires after the go-plugin handshake, so a launchable binary is required.

- [ ] **Step 3: Run the integration suite for this file**

Run: `task test:int -- ./test/integration/plugin/...` (Ginkgo suites filter via `--focus`/`--label-filter`, not `go test -run`; run the whole package here)
Expected: PASS.

- [ ] **Step 4: Commit**

`jj describe -m "test(plugin): re-vehicle WorldService-coupled binary-plugin integration tests (holomush-q42fh)"` then `jj new`.

### Task 10: INV-3 meta-test — WorldService not registry-resolvable

**Files:**

- Modify: `internal/plugin/goplugin/world_query_invariants_test.go`

- [ ] **Step 1: Write the test**

Build the plugin subsystem the way an existing setup test does (`mcp__probe__search_code "NewServiceRegistry"` / find the subsystem `Start` test) and assert the registry has no `holomush.world.v1.WorldService` entry:

```go
func TestINV3WorldServiceNotPluginResolvable(t *testing.T) {
	reg := buildTestPluginRegistry(t) // mirror the existing subsystem-start test setup
	// ServiceRegistry.Resolve returns (*RegisteredService, error) (registry.go:39);
	// an unregistered name returns a non-nil error.
	_, err := reg.Resolve("holomush.world.v1.WorldService")
	assert.Error(t, err, "WorldService MUST NOT be resolvable from the plugin registry (INV-3)")
}
```

- [ ] **Step 2: Run to verify it passes**

Run: `task test -- -run TestINV3 ./internal/plugin/goplugin/`
Expected: PASS.

- [ ] **Step 3: Commit**

`jj describe -m "test(plugin): INV-3 lock — WorldService not plugin-resolvable (holomush-q42fh)"` then `jj new`.

---

## Phase 6: Parity invariants + integration

### Task 11: INV-4 parity meta-test (4 Lua query fns ↔ 4 RPCs)

**Files:**

- Modify: `internal/plugin/goplugin/world_query_invariants_test.go`

- [ ] **Step 1: Write the test**

```go
func TestINV4WorldQuerySurfaceParity(t *testing.T) {
	luaQueries := []string{
		"holomush.query_location",
		"holomush.query_character",
		"holomush.query_location_characters",
		"holomush.query_object",
	}
	sd := pluginv1.File_holomush_plugin_v1_plugin_proto.Services().ByName("PluginHostService")
	rpcs := map[string]bool{}
	for i := range sd.Methods().Len() {
		if n := string(sd.Methods().Get(i).Name()); strings.HasPrefix(n, "Query") {
			rpcs[n] = true
		}
	}
	assert.Len(t, rpcs, len(luaQueries),
		"each Lua query_* fn MUST have a matching PluginHostService Query RPC (INV-4)")
	for _, want := range []string{"QueryLocation", "QueryCharacter", "QueryLocationCharacters", "QueryObject"} {
		assert.True(t, rpcs[want], "missing RPC %s (INV-4)", want)
	}
}
```

(Confirm the generated proto file var name via `mcp__probe__grep "File_holomush_plugin_v1_plugin_proto"`.)

- [ ] **Step 2: Run to verify it passes**

Run: `task test -- -run TestINV4 ./internal/plugin/goplugin/`
Expected: PASS.

- [ ] **Step 3: Commit**

`jj describe -m "test(plugin): INV-4 lock — Lua/RPC world-query surface parity (holomush-q42fh)"` then `jj new`.

### Task 12: Integration — cross-runtime OBO scoping + confused-deputy regression

**Files:**

- Create: `test/integration/plugin/world_query_obo_test.go` (or extend an existing suite)

- [ ] **Step 1: Write the integration specs (Ginkgo, `//go:build integration`)**

Using the `integrationtest` harness (`WithInTreePlugins()` where a real plugin is needed, `WithPolicyEngine(policytest.DenyAllEngine())` for denial paths), specify:

```go
//go:build integration

var _ = Describe("Plugin world-query OBO", func() {
	It("scopes a binary plugin's world read to the acting character (INV-5)", func() {
		// character A (authorized for location L) triggers a command handled by a
		// binary plugin that calls QueryLocation(L); expect success returning L.
	})
	It("denies a read the acting character is not authorized for (confused-deputy guard)", func() {
		// character A NOT authorized for character B's record; A triggers the plugin
		// to QueryCharacter(B); expect PermissionDenied — proves OBO, not plugin-broad.
	})
	It("yields identical results via the Lua runtime for the same actor (INV-5)", func() {
		// same scenario through a Lua plugin; assert equal outcome.
	})
	It("uses plugin identity for a plugin-initiated read (no acting character)", func() {
		// plugin-initiated read resolves to plugin:<name>.
	})
})
```

Fill each `It` with concrete harness calls (seed location/character via the harness helpers; drive the command path; assert the response or the gRPC code).

- [ ] **Step 2: Run**

Run: `task test:int -- ./test/integration/plugin/...`
Expected: PASS.

- [ ] **Step 3: Commit**

`jj describe -m "test(plugin): integration — OBO world-read scoping + confused-deputy guard (holomush-q42fh)"` then `jj new`.

### Task 13: INV-2 structural meta-test + coverage gate

**Files:**

- Modify: `internal/plugin/goplugin/world_query_invariants_test.go`

- [ ] **Step 1: Write the INV-2 structural assertion**

INV-2 (both surfaces derive the subject via `pluginauthz.ActorSubject`, never request-sourced or hard-coded) is enforced behaviorally by Task 5/6 tests plus a structural guard. Add a guard that the binary handlers and Lua query fns do not reference `req.GetSubjectId`-style accessors and do not call `access.PluginSubject` at the query sites:

```go
func TestINV2NoRequestSubjectOnQueryHandlers(t *testing.T) {
	// The Query*Request messages have no subject field (INV-1 covers this); this
	// asserts the SDK request constructors expose no subject setter either.
	// Reflect over the request structs: assert no exported field name contains "Subject".
	for _, msg := range []any{
		&pluginv1.PluginHostServiceQueryLocationRequest{},
		&pluginv1.PluginHostServiceQueryCharacterRequest{},
		&pluginv1.PluginHostServiceQueryLocationCharactersRequest{},
		&pluginv1.PluginHostServiceQueryObjectRequest{},
	} {
		ty := reflect.TypeOf(msg).Elem()
		for i := range ty.NumField() {
			assert.NotContains(t, ty.Field(i).Name, "Subject",
				"%s exposes a Subject field (INV-2)", ty.Name())
		}
	}
}
```

- [ ] **Step 2: Run to verify it passes**

Run: `task test -- -run TestINV2 ./internal/plugin/goplugin/`
Expected: PASS.

- [ ] **Step 3: Verify per-package coverage**

Run: `task test:cover -- ./internal/plugin/... ./pkg/plugin/... ./internal/world/...`
Expected: each touched package > 80%. Add tests for any uncovered handler branch (nil reader, world error mapping) until the gate passes.

- [ ] **Step 4: Commit**

`jj describe -m "test(plugin): INV-2 structural guard + coverage for world-query surface (holomush-q42fh)"` then `jj new`.

---

## Phase 7: Docs + final gates

### Task 14: Update docs and run the full pre-push gate

**Files:**

- Modify: `site/src/content/docs/extending/tutorials/binary-plugins.md`
- Regenerate: `site/src/content/docs/reference/grpc-api.md`
- Verify: `site/src/content/docs/extending/tutorials/lua-plugins.md`

- [ ] **Step 1: Replace the WorldService `requires` example**

In `binary-plugins.md` (the `requires: [holomush.world.v1.WorldService]` example, ~line 47), replace it with the new pattern: binary plugins reach world reads via the injected `WorldQuerier` host facade (implement `WorldQuerierAware`), not a `requires` entry. Add a short snippet showing `SetWorldQuerier` + a `GetLocation` call. Note the read is host-scoped to the acting character (OBO).

- [ ] **Step 2: Regenerate the gRPC reference**

Run: `task docs:build` (or the proto→md generator the repo uses; confirm via `rg -n 'grpc-api' Taskfile.yml`).
Expected: `grpc-api.md` reflects the removed WorldService server (if deleted) and the new PluginHostService RPCs. Verify the `lua-plugins.md` `world_ext.*` section still reads correctly (the `query_*` globals remain, now OBO).

- [ ] **Step 3: Markdown + docs lint**

Run: `task fmt && task lint:docs-symmetry`
Expected: PASS (escape `|` in any tables; mermaid for any diagrams).

- [ ] **Step 4: Full pre-push gate**

Run: `task pr-prep:full` (this change touches integration surface — Ginkgo suites — so the full lane is appropriate).
Expected: `✓ All PR checks passed.` (exit 0). If it fails, read the named failing check and fix; do not retry on a real failure.

- [ ] **Step 5: Commit**

`jj describe -m "docs(plugin): document binary world-query facade; regen gRPC reference (holomush-q42fh)"` then `jj new`.

---

## Spec coverage check

| Spec element | Task(s) |
|---|---|
| 4 RPCs, no subject field (Component 1) | 1 |
| Binary handlers, OBO subject (Component 2) | 3, 4, 5 |
| Lua OBO refactor (Component 3) | 6 |
| SDK facade (Component 4) | 7 |
| Remove forgery surface incl. dead gRPC server (Component 5) | 8 |
| Integration-test rework (Component 5) | 9 |
| Docs (Component 5) | 14 |
| INV-1 | 2 |
| INV-2 | 13 |
| INV-3 | 10 |
| INV-4 | 11 |
| INV-5, INV-6 | 12 (+ 5/6 unit) |
| Coverage / TDD / verification commands | every task; gate in 13, 14 |
| A1/A2 ADRs | captured by `capture-adrs` after plan READY |
