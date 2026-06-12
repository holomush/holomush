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

func (stubHostCaps) IssueEmitToken(context.Context, string, core.Actor) (string, error) {
	return "", nil
}

func (stubHostCaps) OwnedResourceTypes(string) map[string]bool          { return nil }
func (stubHostCaps) GameSettings() settings.GameSettings                { return nil }
func (stubHostCaps) PlayerSettings() settings.PlayerSettingsStore       { return nil }
func (stubHostCaps) CharacterSettings() settings.CharacterSettingsStore { return nil }
func (stubHostCaps) FocusCoordinator() focus.Coordinator                { return nil }
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
	if _, ok := info["holomush.plugin.host.v1.EvalService"]; !ok {
		t.Fatal("binary default set must register EvalService")
	}
}
