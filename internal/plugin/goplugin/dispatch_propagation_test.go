// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

// Binary-runtime dispatch-context propagation (holomush-ndtq1), the counterpart
// to the Lua-only holomush-eykuh.4.13. Unlike the Lua bufconn (in-process; the
// host controls both ends, so dispatch rides host-stamped metadata), a binary
// plugin is out-of-process and controls its own outgoing gRPC metadata. The
// host-vouched DispatchContext is therefore bound to the unforgeable host-issued
// emit token at delivery and recovered server-side from the *validated* token
// (tokenDispatchInterceptor → Host.LookupDispatch), never from plugin-supplied
// metadata. These tests prove: a token-bound dispatch clears the scope fence;
// absence fails closed; and raw forged x-holomush-* metadata is ignored
// (plugin-runtime-symmetry, INV-PLUGIN-51 / INV-PLUGIN-52).
//
// The path is latent in production: BinaryDefaultSet omits WorldMutationService
// (the binary Host has no world surface), so these tests register the scoped
// capability against a custom base with a no-op mutator.

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/world"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// dispatchTestLocID is the acting character's resolved location, used both as the
// dispatch attribute and the CreateExit source location so the own-location fence
// permits the call on the happy path. dispatchTestDestID is the exit destination.
// Both MUST be valid ULIDs — CreateExit parses from_id/to_id and the scope fence
// compares the raw from_id against the dispatch location.
const (
	dispatchTestLocID  = "01KV4GA88FGR0W8Y30ZK1K9XM5"
	dispatchTestDestID = "01KV4GA88FBGE9HRP0GSMSG7BA"
	dispatchTestPlugin = "builder-bot"
)

// noopWorldMutator is a world.Mutator whose write methods succeed without
// touching any store, so a scoped CreateExit that clears the scope fence reaches
// a real handler and returns success. Read methods return zero values.
type noopWorldMutator struct{}

var _ world.Mutator = (*noopWorldMutator)(nil)

func (*noopWorldMutator) GetLocation(_ context.Context, _ string, _ ulid.ULID) (*world.Location, error) {
	return nil, nil
}

func (*noopWorldMutator) GetCharacter(_ context.Context, _ string, _ ulid.ULID) (*world.Character, error) {
	return nil, nil
}

func (*noopWorldMutator) GetCharactersByLocation(_ context.Context, _ string, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	return nil, nil
}

func (*noopWorldMutator) GetObject(_ context.Context, _ string, _ ulid.ULID) (*world.Object, error) {
	return nil, nil
}

func (*noopWorldMutator) CreateLocation(_ context.Context, _ string, _ *world.Location) error {
	return nil
}

func (*noopWorldMutator) CreateExit(_ context.Context, _ string, _ *world.Exit) error { return nil }

func (*noopWorldMutator) CreateObject(_ context.Context, _ string, _ *world.Object) error { return nil }

func (*noopWorldMutator) UpdateLocation(_ context.Context, _ string, _ *world.Location) error {
	return nil
}

func (*noopWorldMutator) UpdateObject(_ context.Context, _ string, _ *world.Object) error { return nil }

func (*noopWorldMutator) FindLocationByName(_ context.Context, _, _ string) (*world.Location, error) {
	return nil, nil
}

// ownLocationDispatchEngine models the own-location seed policy: it permits a
// plugin write to "location:<id>" only when the host-overlaid action attribute
// "dispatch_location" equals <id>. The resolved dispatch location MUST reach the
// server-side scope interceptor (via the validated token) for the call to be
// allowed — exactly the property under test.
type ownLocationDispatchEngine struct{}

var _ types.AccessPolicyEngine = ownLocationDispatchEngine{}

func (ownLocationDispatchEngine) Evaluate(_ context.Context, req types.AccessRequest) (types.Decision, error) {
	resType, resID, ok := splitDispatchTypeID(req.Resource)
	if !ok || resType != "location" || req.Action != "write" {
		return types.NewDecision(types.EffectDefaultDeny, "no policy", ""), nil
	}
	dispatchLoc, _ := req.Attributes["dispatch_location"].(string)
	if dispatchLoc != "" && dispatchLoc == resID {
		return types.NewDecision(types.EffectAllow, "own-location", "test:own-location"), nil
	}
	return types.NewDecision(types.EffectDefaultDeny, "not own-location", ""), nil
}

func (ownLocationDispatchEngine) CanPerformAction(_ context.Context, _, _, _, _ string) (bool, error) {
	return true, nil
}

func splitDispatchTypeID(ref string) (typ, id string, ok bool) {
	for i := 0; i < len(ref); i++ {
		if ref[i] == ':' {
			if i == 0 || i == len(ref)-1 {
				return "", "", false
			}
			return ref[:i], ref[i+1:], true
		}
	}
	return "", "", false
}

// worldMutatorBase is a hostcap.HostCapabilities that embeds the real binary Host
// (for its other surface methods, all nil — including the real token store and
// LookupDispatch) but overrides WorldMutator so the scope-eligible
// WorldMutationService has a no-op backing handler; the binary Host hardcodes
// WorldMutator() == nil. This is the only seam the latent scoped path needs.
type worldMutatorBase struct {
	*Host
	mutator world.Mutator
}

func (b worldMutatorBase) WorldMutator() hostcap.WorldMutator { return b.mutator }

// newBinaryWorldMutationConn stands up the binary host-capability server via the
// production chain builder (newHostCapabilityServer — token-dispatch interceptor
// then capability/scope interceptor) with a WorldMutation-inclusive set, fronted
// by an in-process bufconn. It returns the conn and the backing *Host so tests can
// mint dispatch-bound emit tokens via the same store the interceptor reads.
//
// LuaDefaultSet is used only because it is the registered set that INCLUDES the
// scope-eligible WorldMutationService (BinaryDefaultSet omits it); the interceptor
// chain under test is the binary production chain regardless of set.
func newBinaryWorldMutationConn(t *testing.T) (*plugins.InProcessConn, *Host) {
	t.Helper()
	h := NewHost()
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	base := worldMutatorBase{Host: h, mutator: &noopWorldMutator{}}
	deps := hostcap.InterceptorDeps{
		Engine:     ownLocationDispatchEngine{},
		PluginName: dispatchTestPlugin,
		DeclaredAccess: func(_, capToken string) (string, bool) {
			if capToken == "world.mutation" {
				return "write", true
			}
			return "", false
		},
	}
	srv := newHostCapabilityServer(base, deps, hostcap.LuaDefaultSet, nil)
	conn, err := plugins.NewInProcessConn(srv)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn, h
}

// issueDispatchToken mints a host emit token bound to dc and returns a client ctx
// carrying it as outgoing x-holomush-emit-token metadata — what a plugin's SDK
// ferries back on a plugin→host call.
func issueDispatchToken(t *testing.T, h *Host, dc pluginauthz.DispatchContext) context.Context {
	t.Helper()
	actor := core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()}
	token, err := h.tokenStore.IssueWithDispatch(dispatchTestPlugin, actor, "", dc)
	require.NoError(t, err)
	return metadata.AppendToOutgoingContext(context.Background(), "x-holomush-emit-token", token)
}

// TestBinaryScopedCapabilityCallSucceedsWhenDispatchBoundToToken drives a
// scope:local world.mutation CreateExit through the binary host-capability chain
// with a host-vouched dispatch bound to the caller's emit token. The own-location
// fence clears only if the token's "location" attribute reaches the server-side
// scope interceptor — proving tokenDispatchInterceptor recovers it from the
// validated token.
//
// Verifies: INV-PLUGIN-51
func TestBinaryScopedCapabilityCallSucceedsWhenDispatchBoundToToken(t *testing.T) {
	conn, h := newBinaryWorldMutationConn(t)
	client := hostv1.NewWorldMutationServiceClient(conn)

	ctx := issueDispatchToken(t, h, pluginauthz.DispatchContext{
		Subject:    "character:01TEST",
		Attributes: map[string]string{"location": dispatchTestLocID},
	})
	resp, err := client.CreateExit(ctx, &hostv1.CreateExitRequest{
		FromId: dispatchTestLocID,
		ToId:   dispatchTestDestID,
		Name:   "north",
	})
	require.NoError(t, err,
		"scoped CreateExit must succeed once the token-bound dispatch location reaches the scope "+
			"interceptor; SCOPE_NO_DISPATCH here means tokenDispatchInterceptor did not recover it")
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.GetId())
}

// TestBinaryScopedCapabilityCallFailsClosedWithoutToken proves the fence denies
// when the caller presents no token (so no dispatch can be recovered): the scoped
// CreateExit must fail closed (SCOPE_NO_DISPATCH).
//
// Verifies: INV-PLUGIN-51
func TestBinaryScopedCapabilityCallFailsClosedWithoutToken(t *testing.T) {
	conn, _ := newBinaryWorldMutationConn(t)
	client := hostv1.NewWorldMutationServiceClient(conn)

	_, err := client.CreateExit(context.Background(), &hostv1.CreateExitRequest{
		FromId: dispatchTestLocID,
		ToId:   dispatchTestDestID,
		Name:   "north",
	})
	require.Error(t, err, "scoped call with no token-bound dispatch must fail closed")
	assert.Contains(t, err.Error(), "scoped capability call without dispatch context")
}

// TestBinaryForgedDispatchMetadataIsIgnored is the core anti-forgery regression:
// an untrusted out-of-process plugin can put ANY value in its outgoing gRPC
// metadata. It forges an x-holomush-dispatch envelope naming the exit's own source
// location (which would clear the own-location fence if the server trusted it),
// while holding only a token bound to NO character dispatch. The server MUST ignore
// the forged metadata and recover dispatch solely from the validated token, so the
// call still fails closed (SCOPE_NO_DISPATCH). If tokenDispatchInterceptor ever
// read plugin metadata, this would fail-open.
//
// Verifies: INV-PLUGIN-51
func TestBinaryForgedDispatchMetadataIsIgnored(t *testing.T) {
	conn, h := newBinaryWorldMutationConn(t)
	client := hostv1.NewWorldMutationServiceClient(conn)

	// Token bound to zero dispatch (e.g. a self-token / non-character delivery).
	ctx := issueDispatchToken(t, h, pluginauthz.DispatchContext{})
	// Plugin forges the reserved dispatch key directly on its outgoing call.
	forged := `{"subject":"character:01TEST","attributes":{"location":"` + dispatchTestLocID + `"}}`
	ctx = metadata.AppendToOutgoingContext(ctx, "x-holomush-dispatch", forged)

	_, err := client.CreateExit(ctx, &hostv1.CreateExitRequest{
		FromId: dispatchTestLocID,
		ToId:   dispatchTestDestID,
		Name:   "north",
	})
	require.Error(t, err,
		"forged x-holomush-dispatch metadata must NOT authorize a scoped call: the server recovers "+
			"dispatch only from the host-issued token, never from plugin-supplied metadata")
	assert.Contains(t, err.Error(), "scoped capability call without dispatch context")
}

// dispatchCapturingClient wraps the shared mock plugin client and, during a
// host→plugin delivery, recovers the dispatch bound to the emit token the host
// attached — proving the delivery side binds the host-vouched dispatch to the
// token (while the token is still valid, before the deferred Revoke).
type dispatchCapturingClient struct {
	*mockGRPCPluginClient
	store    *emitTokenStore
	dispatch pluginauthz.DispatchContext
	ok       bool
}

func (c *dispatchCapturingClient) capture(ctx context.Context) {
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		if toks := md.Get("x-holomush-emit-token"); len(toks) == 1 {
			c.dispatch, c.ok = c.store.LookupDispatch(dispatchTestPlugin, toks[0])
		}
	}
}

func (c *dispatchCapturingClient) HandleEvent(ctx context.Context, req *pluginv1.HandleEventRequest, opts ...grpc.CallOption) (*pluginv1.HandleEventResponse, error) {
	c.capture(ctx)
	return c.mockGRPCPluginClient.HandleEvent(ctx, req, opts...)
}

func (c *dispatchCapturingClient) HandleCommand(ctx context.Context, req *pluginv1.HandleCommandRequest, opts ...grpc.CallOption) (*pluginv1.HandleCommandResponse, error) {
	c.capture(ctx)
	return c.mockGRPCPluginClient.HandleCommand(ctx, req, opts...)
}

// TestDeliveryBindsHostVouchedDispatchToEmitToken proves the host SIDE: each
// delivery (DeliverEvent / DeliverCommand) to a binary plugin binds the
// host-vouched dispatch (subject + resolved location) to the emit token it issues,
// recoverable via LookupDispatch — NOT marshalled into forgeable metadata.
//
// Verifies: INV-PLUGIN-51
func TestDeliveryBindsHostVouchedDispatchToEmitToken(t *testing.T) {
	const charID = "01HCHAR0000000000000000000"
	cases := []struct {
		name    string
		deliver func(h *Host, ctx context.Context) error
	}{
		{"DeliverEvent", func(h *Host, ctx context.Context) error {
			_, err := h.DeliverEvent(ctx, dispatchTestPlugin, pluginsdk.Event{Type: "say"})
			return err
		}},
		{"DeliverCommand", func(h *Host, ctx context.Context) error {
			_, err := h.DeliverCommand(ctx, dispatchTestPlugin, pluginsdk.CommandRequest{})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg, _ := stubRegistryFor(dispatchTestPlugin)
			h := NewHostWithFactory(&mockClientFactory{client: &mockPluginClient{protocol: &mockClientProtocol{}}},
				WithIdentityRegistry(reg),
				WithDispatchAttributeResolver(fakeAttrResolver{attrs: map[string]any{"location": dispatchTestLocID}}))
			defer func() { _ = h.Close(context.Background()) }()
			capturing := &dispatchCapturingClient{mockGRPCPluginClient: &mockGRPCPluginClient{}, store: h.tokenStore}
			h.plugins[dispatchTestPlugin] = &loadedPlugin{manifest: &plugins.Manifest{Name: dispatchTestPlugin}, plugin: capturing}

			ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorCharacter, ID: charID})
			require.NoError(t, tc.deliver(h, ctx))

			require.True(t, capturing.ok,
				"delivery must bind a host-vouched dispatch to the emit token (recoverable via LookupDispatch)")
			assert.Equal(t, "character:"+charID, capturing.dispatch.Subject)
			assert.Equal(t, dispatchTestLocID, capturing.dispatch.Attributes["location"],
				"the scope-bearing location attribute (what the own-location fence evaluates) must be bound to the token")
		})
	}
}
