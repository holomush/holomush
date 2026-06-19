// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap_test

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/grpc/focus"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/settings"
)

// stubHostCaps is a zero-value hand stub satisfying hostcap.HostCapabilities.
// Every accessor returns the unconfigured/nil value, which is exactly what the
// servers' fail-closed guards expect. RegisterCapabilities never calls any port
// method (it only constructs servers), so the stub needs no behavior — it exists
// solely to let NewBase compile a real base.
type stubHostCaps struct{}

func (stubHostCaps) AccessEngine() types.AccessPolicyEngine             { return nil }
func (stubHostCaps) Auditor() pluginauthz.Auditor                       { return nil }
func (stubHostCaps) EventEmitter() plugins.PluginIntentEmitter          { return nil }
func (stubHostCaps) CommandQuerier() *commandquery.Querier              { return nil }
func (stubHostCaps) IdentityRegistrySnapshot() plugins.IdentityRegistry { return nil }

func (stubHostCaps) LookupActor(context.Context, string) (core.Actor, string, error) {
	return core.Actor{}, "", nil
}

func (stubHostCaps) LookupDispatch(context.Context, string) (pluginauthz.DispatchContext, bool) {
	return pluginauthz.DispatchContext{}, false
}

func (stubHostCaps) IssueEmitToken(context.Context, string, core.Actor) (string, error) {
	return "", nil
}

func (stubHostCaps) OwnedResourceTypes(string) map[string]bool          { return nil }
func (stubHostCaps) GameSettings() settings.GameSettings                { return nil }
func (stubHostCaps) PlayerSettings() settings.PlayerSettingsStore       { return nil }
func (stubHostCaps) CharacterSettings() settings.CharacterSettingsStore { return nil }
func (stubHostCaps) FocusCoordinator() focus.Coordinator                { return nil }
func (stubHostCaps) GameID() string                                     { return "" }
func (stubHostCaps) HistoryReader() plugins.HistoryReader               { return nil }
func (stubHostCaps) ReadbackDecryptor() plugins.ReadbackDecryptor       { return nil }

func (stubHostCaps) PropertyDefinition(string) (hostcap.PropertyDefinition, bool) {
	return nil, false
}

func (stubHostCaps) WorldQuerier(string) hostcap.WorldQuerier { return nil }
func (stubHostCaps) WorldMutator() hostcap.WorldMutator       { return nil }
func (stubHostCaps) SessionAccess() session.Access            { return nil }
func (stubHostCaps) SessionAdmin() hostcap.SessionAdmin       { return nil }

var _ hostcap.HostCapabilities = stubHostCaps{}

// TestRegisterCapabilitiesRegistersLuaDefaultSet asserts that the LuaDefaultSet
// branch registers all four Lua-only capability services in addition to the 9
// binary services. Prevents a dropped registration line from passing CI silently.
func TestRegisterCapabilitiesRegistersLuaDefaultSet(t *testing.T) {
	srv := grpc.NewServer()
	base := hostcap.NewBase(stubHostCaps{}, "test-plugin")
	hostcap.RegisterCapabilities(srv, base, hostcap.LuaDefaultSet)

	info := srv.GetServiceInfo()

	required := []string{
		"holomush.plugin.host.v1.PropertyService",
		"holomush.plugin.host.v1.SessionService",
		"holomush.plugin.host.v1.SessionAdminService",
		"holomush.plugin.host.v1.WorldQueryService",
		"holomush.plugin.host.v1.WorldMutationService",
	}
	for _, svc := range required {
		if _, ok := info[svc]; !ok {
			t.Errorf("LuaDefaultSet must register %s", svc)
		}
	}
	// Sanity-check that the 9 binary services are still present.
	if _, ok := info["holomush.plugin.host.v1.EvalService"]; !ok {
		t.Fatal("LuaDefaultSet must include EvalService (inherited from binary set)")
	}
}

// TestRegisterCapabilitiesRegistersBinaryDefaultSet asserts the helper wires the
// binary default capability set onto a server without panicking and that the set
// excludes Session/Property/World (no binary consumer; spec §1) while including
// the 9 services that do have a binary consumer (EvalService is the witness).
func TestRegisterCapabilitiesRegistersBinaryDefaultSet(t *testing.T) {
	srv := grpc.NewServer()
	base := hostcap.NewBase(stubHostCaps{}, "test-plugin")
	hostcap.RegisterCapabilities(srv, base, hostcap.BinaryDefaultSet)

	info := srv.GetServiceInfo()
	if _, ok := info["holomush.plugin.host.v1.SessionService"]; ok {
		t.Fatal("binary default set must not register SessionService")
	}
	if _, ok := info["holomush.plugin.host.v1.PropertyService"]; ok {
		t.Fatal("binary default set must not register PropertyService")
	}
	if _, ok := info["holomush.plugin.host.v1.WorldQueryService"]; ok {
		t.Fatal("binary default set must not register WorldQueryService")
	}
	if _, ok := info["holomush.plugin.host.v1.WorldMutationService"]; ok {
		t.Fatal("binary default set must not register WorldMutationService")
	}
	if _, ok := info["holomush.plugin.host.v1.SessionAdminService"]; ok {
		t.Fatal("binary default set must not register SessionAdminService")
	}
	if _, ok := info["holomush.plugin.host.v1.EvalService"]; !ok {
		t.Fatal("binary default set must register EvalService")
	}
}
