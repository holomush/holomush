// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/holomush/holomush/internal/access/policy/types"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/dispatchwire"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/world"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// dispatchTestLocID is the acting character's resolved location, used both as the
// dispatch attribute and the CreateExit source location so the own-location fence
// permits the call on the happy path. dispatchTestDestID is the exit destination.
// Both MUST be valid ULIDs — the CreateExit handler parses from_id/to_id, and the
// scope fence compares the raw from_id string against the dispatch location.
const (
	dispatchTestLocID  = "01KV4GA88FGR0W8Y30ZK1K9XM5"
	dispatchTestDestID = "01KV4GA88FBGE9HRP0GSMSG7BA"
)

// noopWorldMutator is a world.Mutator whose write methods succeed without
// touching any store, so a scoped CreateExit that clears the scope fence reaches
// a real handler and returns success. Read methods return zero values; they are
// not exercised by the dispatch-propagation tests.
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
// "dispatch_location" equals <id>. This lets the bufconn tests assert the scope
// fence end-to-end through the real endpoint without standing up the full DSL
// engine — the resolved dispatch location MUST cross the bufconn for the call to
// be allowed, which is exactly the property under test.
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

// newWorldMutationEndpoint stands up a per-plugin bufconn endpoint backed by a
// no-op world mutator and the own-location engine, with the plugin declaring the
// world.mutation capability at write access. This is the production wiring
// (newPluginEndpoint), so the scope interceptor is the real one.
func newWorldMutationEndpoint(t *testing.T, pluginName string) *pluginEndpoint {
	t.Helper()
	funcs := hostfunc.New(
		nil,
		hostfunc.WithWorldService(&noopWorldMutator{}),
		hostfunc.WithEngine(ownLocationDispatchEngine{}),
	)
	adapter := newLuaHostCapAdapter(funcs)
	ep, err := newPluginEndpoint(adapter, &plugins.Manifest{
		Name: pluginName,
		Requires: []plugins.Dependency{
			{Kind: plugins.DependencyCapability, Name: "world.mutation", Access: "write"},
		},
	})
	require.NoError(t, err)
	return ep
}

// dispatchCtx returns a call ctx carrying a host-vouched dispatch context whose
// acting-character location is loc. This mirrors what Host.stampDispatch stamps
// onto the Lua state's ctx (dispatch.go) before brokered calls are issued with
// luaContext(L).
func dispatchCtx(loc string) context.Context {
	return pluginauthz.WithDispatch(context.Background(), pluginauthz.DispatchContext{
		Subject:    "character:01TEST",
		Attributes: map[string]string{"location": loc},
	})
}

// TestScopedCapabilityCallSucceedsWhenDispatchPropagatesAcrossBufconn drives a
// scope: local world.mutation CreateExit through the REAL per-plugin bufconn
// endpoint with a host-vouched dispatch context on the call ctx. The own-location
// fence can only be satisfied if the dispatch "location" attribute crosses the
// bufconn to the server-side scope interceptor. Pre-fix this fails closed with
// SCOPE_NO_DISPATCH because plugins.NewInProcessConn drops context values; the
// dispatch-propagation interceptors restore it.
func TestScopedCapabilityCallSucceedsWhenDispatchPropagatesAcrossBufconn(t *testing.T) {
	ep := newWorldMutationEndpoint(t, "builder-bot")
	defer ep.Close()

	client := hostv1.NewWorldMutationServiceClient(ep.Conn())
	// CreateExit out of the dispatch location: own-location permits this only if
	// the resolved dispatch location reached the server-side scope interceptor.
	resp, err := client.CreateExit(dispatchCtx(dispatchTestLocID), &hostv1.CreateExitRequest{
		FromId: dispatchTestLocID,
		ToId:   dispatchTestDestID,
		Name:   "north",
	})
	require.NoError(t, err,
		"scoped CreateExit must succeed once the host-vouched dispatch location crosses the bufconn; "+
			"SCOPE_NO_DISPATCH here means dispatch context was lost across the in-process boundary")
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.GetId())
}

// TestScopedCapabilityCallFailsClosedWithoutDispatch proves the fence still
// denies when NO host dispatch is stamped on the call ctx: the same scoped
// CreateExit must fail closed (SCOPE_NO_DISPATCH). This guards against the
// propagation fix opening a fail-open hole — absence of dispatch must keep
// denying after the fix, not before it only.
//
// Verifies: INV-PLUGIN-51
func TestScopedCapabilityCallFailsClosedWithoutDispatch(t *testing.T) {
	ep := newWorldMutationEndpoint(t, "builder-bot")
	defer ep.Close()

	client := hostv1.NewWorldMutationServiceClient(ep.Conn())
	// No WithDispatch on the call ctx => no envelope is written => the server-side
	// scope interceptor finds no dispatch and fails closed.
	_, err := client.CreateExit(context.Background(), &hostv1.CreateExitRequest{
		FromId: dispatchTestLocID,
		ToId:   dispatchTestDestID,
		Name:   "north",
	})
	require.Error(t, err, "scoped call with no host dispatch must fail closed")
	assert.Contains(t, err.Error(), "scoped capability call without dispatch context")
}

// TestPluginForgedDispatchMetadataIsIgnored proves a plugin cannot forge scope
// attributes by injecting the dispatch metadata key itself. The host's outgoing
// interceptor writes the envelope ONLY from the host-vouched DispatchForHost(ctx)
// value; when the call ctx carries no host dispatch, plugin-supplied outgoing
// metadata on the reserved key must NOT be honored as authoritative scope
// attributes. The forged envelope names the exit's own source location (which
// would satisfy own-location if trusted), yet the call must still fail closed.
//
// Verifies: INV-PLUGIN-51
func TestPluginForgedDispatchMetadataIsIgnored(t *testing.T) {
	ep := newWorldMutationEndpoint(t, "builder-bot")
	defer ep.Close()

	client := hostv1.NewWorldMutationServiceClient(ep.Conn())
	// Simulate a plugin attempting to forge the dispatch envelope directly on the
	// outgoing metadata, WITHOUT a host-vouched WithDispatch on the ctx. The forged
	// payload claims dispatch_location == the exit source, which would clear the
	// own-location fence if the server trusted plugin-supplied metadata.
	forged := `{"subject":"character:01TEST","attributes":{"location":"` + dispatchTestLocID + `"}}`
	ctx := metadata.AppendToOutgoingContext(context.Background(), dispatchwire.MetadataKey, forged)
	_, err := client.CreateExit(ctx, &hostv1.CreateExitRequest{
		FromId: dispatchTestLocID,
		ToId:   dispatchTestDestID,
		Name:   "north",
	})
	require.Error(t, err,
		"plugin-forged dispatch metadata must not authorize a scoped call: the host outgoing "+
			"interceptor overwrites the reserved key from host-vouched dispatch only")
	assert.Contains(t, err.Error(), "scoped capability call without dispatch context")
}
