// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

// Binary-runtime counterpart to internal/plugin/lua/dispatch_propagation_test.go
// (holomush-eykuh.4.13 was Lua-only). It proves the binary host-capability server
// chain (newHostCapabilityServer) reconstructs the host-vouched dispatch context
// from incoming metadata before the scope interceptor, so a binary plugin's
// plugin→host scoped capability call resolves its own-location fence identically
// to a Lua plugin's (plugin-runtime-symmetry, INV-PLUGIN-51 / INV-PLUGIN-52).
//
// The path is latent in production: BinaryDefaultSet omits WorldMutationService
// (the binary Host has no world surface), so these tests register the scoped
// capability against a custom base with a no-op mutator. They lock the
// dispatch-stamp wiring in place BEFORE a scope-eligible capability gains a
// binary consumer — without it the call would fail closed with SCOPE_NO_DISPATCH
// (holomush-ndtq1).

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/dispatchwire"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/world"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// dispatchTestLocID is the acting character's resolved location, used both as the
// dispatch attribute and the CreateExit source location so the own-location fence
// permits the call on the happy path. dispatchTestDestID is the exit destination.
// Both MUST be valid ULIDs — CreateExit parses from_id/to_id and the scope fence
// compares the raw from_id against the dispatch location.
const (
	dispatchTestLocID  = "01KV4GA88FGR0W8Y30ZK1K9XM5"
	dispatchTestDestID = "01KV4GA88FBGE9HRP0GSMSG7BA"
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
// "dispatch_location" equals <id>. The resolved dispatch location MUST cross the
// wire (as metadata) for the call to be allowed — exactly the property under test.
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
// (for its 17 other surface methods, all nil) but overrides WorldMutator so the
// scope-eligible WorldMutationService has a no-op backing handler — the binary
// Host hardcodes WorldMutator() == nil. This is the only seam the latent scoped
// path needs to be exercisable.
type worldMutatorBase struct {
	*Host
	mutator world.Mutator
}

func (b worldMutatorBase) WorldMutator() hostcap.WorldMutator { return b.mutator }

// newBinaryWorldMutationConn stands up the binary host-capability server via the
// production chain builder (newHostCapabilityServer) with the WorldMutation-
// inclusive set, fronted by an in-process bufconn. LuaDefaultSet is used only
// because it is the registered set that INCLUDES the scope-eligible
// WorldMutationService; the interceptor chain (dispatch-stamp → capability) is
// the binary production chain regardless of set.
func newBinaryWorldMutationConn(t *testing.T, pluginName string) *plugins.InProcessConn {
	t.Helper()
	h := NewHost()
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	base := worldMutatorBase{Host: h, mutator: &noopWorldMutator{}}
	deps := hostcap.InterceptorDeps{
		Engine:     ownLocationDispatchEngine{},
		PluginName: pluginName,
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
	return conn
}

// TestBinaryScopedCapabilityCallSucceedsWhenDispatchFerried drives a
// scope:local world.mutation CreateExit through the binary host-capability server
// chain with a host-vouched dispatch envelope in the outgoing metadata (what the
// host delivery marshal + SDK ferry produce). The own-location fence can only be
// satisfied if the dispatch "location" attribute reaches the server-side scope
// interceptor — proving the dispatch-stamp interceptor reconstructs it. Without
// the fix this fails closed with SCOPE_NO_DISPATCH.
//
// Verifies: INV-PLUGIN-51
func TestBinaryScopedCapabilityCallSucceedsWhenDispatchFerried(t *testing.T) {
	conn := newBinaryWorldMutationConn(t, "builder-bot")
	client := hostv1.NewWorldMutationServiceClient(conn)

	// AttachOutgoing marshals the host-vouched dispatch into outgoing metadata,
	// exactly as the host delivery side does and the SDK ferries onto plugin→host
	// calls; the bufconn delivers it as the server's incoming metadata.
	ctx := dispatchwire.AttachOutgoing(context.Background(), pluginauthz.DispatchContext{
		Subject:    "character:01TEST",
		Attributes: map[string]string{"location": dispatchTestLocID},
	})
	resp, err := client.CreateExit(ctx, &hostv1.CreateExitRequest{
		FromId: dispatchTestLocID,
		ToId:   dispatchTestDestID,
		Name:   "north",
	})
	require.NoError(t, err,
		"scoped CreateExit must succeed once the host-vouched dispatch location crosses the "+
			"binary boundary; SCOPE_NO_DISPATCH here means the dispatch-stamp interceptor is not wired")
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.GetId())
}

// TestBinaryScopedCapabilityCallFailsClosedWithoutDispatch proves the fence still
// denies when NO dispatch envelope is on the wire: the same scoped CreateExit
// must fail closed (SCOPE_NO_DISPATCH). Guards against the propagation wiring
// opening a fail-open hole — absence of dispatch must keep denying.
//
// Verifies: INV-PLUGIN-51
func TestBinaryScopedCapabilityCallFailsClosedWithoutDispatch(t *testing.T) {
	conn := newBinaryWorldMutationConn(t, "builder-bot")
	client := hostv1.NewWorldMutationServiceClient(conn)

	_, err := client.CreateExit(context.Background(), &hostv1.CreateExitRequest{
		FromId: dispatchTestLocID,
		ToId:   dispatchTestDestID,
		Name:   "north",
	})
	require.Error(t, err, "scoped call with no dispatch must fail closed")
	assert.Contains(t, err.Error(), "scoped capability call without dispatch context")
}

// TestDeliveryMarshalsHostVouchedDispatchOntoOutgoingMetadata proves the host
// SIDE of the binary propagation: each delivery (DeliverEvent / DeliverCommand)
// to a binary plugin marshals the host-vouched dispatch envelope onto the
// outgoing call metadata, so the plugin receives it and the SDK can ferry it back
// on plugin→host scoped calls. Without this, the SDK has nothing to ferry and the
// scope fence fails closed — the host half of the gap (holomush-ndtq1).
//
// Verifies: INV-PLUGIN-51
func TestDeliveryMarshalsHostVouchedDispatchOntoOutgoingMetadata(t *testing.T) {
	const charID = "01HCHAR0000000000000000000"
	cases := []struct {
		name     string
		deliver  func(h *Host, ctx context.Context) error
		captured func(*mockGRPCPluginClient) context.Context
	}{
		{
			"DeliverEvent",
			func(h *Host, ctx context.Context) error {
				_, err := h.DeliverEvent(ctx, "plug-A", pluginsdk.Event{Type: "say"})
				return err
			},
			func(m *mockGRPCPluginClient) context.Context { return m.eventCtx },
		},
		{
			"DeliverCommand",
			func(h *Host, ctx context.Context) error {
				_, err := h.DeliverCommand(ctx, "plug-A", pluginsdk.CommandRequest{})
				return err
			},
			func(m *mockGRPCPluginClient) context.Context { return m.commandCtx },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			grpcClient := &mockGRPCPluginClient{}
			mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
			reg, _ := stubRegistryFor("plug-A")
			h := NewHostWithFactory(&mockClientFactory{client: mockClient}, WithIdentityRegistry(reg))
			defer func() { _ = h.Close(context.Background()) }()
			h.plugins["plug-A"] = &loadedPlugin{manifest: &plugins.Manifest{Name: "plug-A"}, plugin: grpcClient}

			ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorCharacter, ID: charID})
			require.NoError(t, tc.deliver(h, ctx))

			md, ok := metadata.FromOutgoingContext(tc.captured(grpcClient))
			require.True(t, ok, "delivery must carry outgoing metadata")
			// The delivery's outgoing metadata becomes the plugin's incoming metadata.
			dc, ok := dispatchwire.DecodeFromIncoming(metadata.NewIncomingContext(context.Background(), md))
			require.True(t, ok,
				"delivery must marshal the host-vouched dispatch envelope so a binary plugin can ferry it back")
			assert.Equal(t, "character:"+charID, dc.Subject)
		})
	}
}
