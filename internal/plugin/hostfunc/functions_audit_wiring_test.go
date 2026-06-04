// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// stubAuditorForHostfunc is a minimal pluginauthz.Auditor implementation used
// by wiring-guard tests. It records nothing — its purpose is to provide a
// non-nil, identifiable Auditor instance for assert.Same checks.
type stubAuditorForHostfunc struct{}

func (s *stubAuditorForHostfunc) Log(_ context.Context, _ audit.Event) error { return nil }

// Compile-time: stubAuditorForHostfunc must satisfy pluginauthz.Auditor.
var _ pluginauthz.Auditor = (*stubAuditorForHostfunc)(nil)

// TestWithAuditLoggerStoresAuditorInFunctions is a behavioral wiring-guard for
// holomush-p1tq2.5 / INV-PLUGIN-25 on the Lua surface. It verifies that
// hostfunc.WithAuditLogger correctly propagates the given Auditor into the
// Functions' internal auditor field, confirming that holomush.evaluate Lua
// calls will emit audit events.
//
// In production, setup/subsystem.go Start() passes:
//
//	hostfunc.WithAuditLogger(s.cfg.ABAC.AuditLogger())
//
// to hostfunc.New. Without this propagation, holomush.evaluate silently drops
// all audit events regardless of the decision, violating spec §5 / INV-PLUGIN-25.
//
// This test confirms:
//  1. WithAuditLogger is a valid Option (compile-time guard).
//  2. The auditor stored in Functions is the identical instance that was
//     passed in (identity guard — ensures the same *audit.Logger that
//     ABACSubsystem built is the one pluginauthz.Evaluate calls on the Lua surface).
func TestWithAuditLoggerStoresAuditorInFunctions(t *testing.T) {
	aud := &stubAuditorForHostfunc{}

	funcs := hostfunc.New(nil, hostfunc.WithAuditLogger(aud))
	require.NotNil(t, funcs)

	got := funcs.AuditorForTest()
	assert.NotNil(t, got,
		"hostfunc.Functions MUST have a non-nil auditor after WithAuditLogger option is applied; "+
			"nil means holomush.evaluate never emits audit events (INV-PLUGIN-25 regression)")
	assert.Same(t, aud, got,
		"Functions must store the identical auditor instance passed to WithAuditLogger; "+
			"a different instance would break audit-log correlation across subsystems")
}
