// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// TestWithEngineStoresEngineInHost is a behavioral wiring-guard for
// holomush-8kkv5.18. It verifies that goplugin.WithEngine correctly propagates
// the given engine into the host's internal engine field.
//
// In production, setup/subsystem.go Start() passes:
//
//	goplugin.WithEngine(s.cfg.ABAC.Engine())
//
// to goplugin.NewHost. Before this fix, WithEngine was absent from hostOpts,
// so s.host.engine was nil and PluginHostService.Evaluate always returned
// EVALUATE_ENGINE_UNCONFIGURED for every binary plugin call.
//
// This test confirms:
//  1. WithEngine is a valid HostOption (compile-time guard).
//  2. The engine stored in the host is the identical instance that was passed in
//     (identity guard — ensures the engine+resolver pair from BuildABACStack is
//     shared between the Lua hostfunc bridge and the binary host).
//
// stubAuditorForGoplugin is a minimal pluginauthz.Auditor implementation used
// by wiring-guard tests. It records nothing — its purpose is to provide a
// non-nil, identifiable Auditor instance for assert.Same checks.
type stubAuditorForGoplugin struct{}

func (s *stubAuditorForGoplugin) Log(_ context.Context, _ audit.Event) error { return nil }

// Compile-time: stubAuditorForGoplugin must satisfy pluginauthz.Auditor.
var _ pluginauthz.Auditor = (*stubAuditorForGoplugin)(nil)

// TestWithAuditLoggerStoresAuditorInHost is a behavioral wiring-guard for
// holomush-p1tq2.5 / INV-4. It verifies that goplugin.WithAuditLogger
// correctly propagates the given Auditor into the host's internal auditor
// field, confirming the PluginHostService.Evaluate path will emit audit
// events.
//
// In production, setup/subsystem.go Start() passes:
//
//	goplugin.WithAuditLogger(s.cfg.ABAC.AuditLogger())
//
// to goplugin.NewHost. Without this propagation, PluginHostService.Evaluate
// silently drops all audit events regardless of the decision, violating
// spec §5 / INV-4.
//
// This test confirms:
//  1. WithAuditLogger is a valid HostOption (compile-time guard).
//  2. The auditor stored in the host is the identical instance that was
//     passed in (identity guard — ensures the same *audit.Logger that
//     ABACSubsystem built is the one pluginauthz.Evaluate calls).
func TestWithAuditLoggerStoresAuditorInHost(t *testing.T) {
	aud := &stubAuditorForGoplugin{}

	host := goplugin.NewHost(goplugin.WithAuditLogger(aud))
	require.NotNil(t, host)

	got := host.AuditorForTest()
	assert.NotNil(t, got,
		"binary host MUST have a non-nil auditor after WithAuditLogger option is applied; "+
			"nil means PluginHostService.Evaluate never emits audit events (INV-4 regression)")
	assert.Same(t, aud, got,
		"host must store the identical auditor instance passed to WithAuditLogger; "+
			"a different instance would break audit-log correlation across subsystems")
}

func TestWithEngineStoresEngineInHost(t *testing.T) {
	eng := policytest.AllowAllEngine()
	require.NotNil(t, eng)

	host := goplugin.NewHost(goplugin.WithEngine(eng))
	require.NotNil(t, host)

	got := host.EngineForTest()
	assert.NotNil(t, got,
		"binary host MUST have a non-nil engine after WithEngine option is applied; "+
			"nil means PluginHostService.Evaluate returns EVALUATE_ENGINE_UNCONFIGURED "+
			"for all binary plugins (holomush-8kkv5.18 regression)")
	assert.Same(t, eng, got,
		"host must store the identical engine instance passed to WithEngine; "+
			"a copy would break attribute-resolver sharing between Lua and binary surfaces")
}
