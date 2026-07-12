// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/testsupport/natstest"
)

// The two-replica resilience suite (OPS-05, #4791) exercises the external-NATS
// and shared-database seams, but it self-skips off the gating CI lane
// (quarantinetest.Enabled(), D-05). These tests give the seams gating-CI
// coverage directly — a deterministic single/two-replica boot with no chaos —
// so a regression in the harness wiring is caught in the normal `task test:int`
// run rather than only nightly.

// TestStartOptionsApplyToConfig verifies each resilience StartOption mutates the
// startConfig it is applied to. Pure (no containers): it exercises the option
// closures in options.go directly.
func TestStartOptionsApplyToConfig(t *testing.T) {
	t.Run("WithExternalNATS sets externalNATSURL", func(t *testing.T) {
		cfg := &startConfig{}
		WithExternalNATS("nats://broker:4222")(cfg)
		require.Equal(t, "nats://broker:4222", cfg.externalNATSURL)
	})

	t.Run("WithSharedDatabase sets sharedConnStr", func(t *testing.T) {
		cfg := &startConfig{}
		WithSharedDatabase("postgres://shared/db")(cfg)
		require.Equal(t, "postgres://shared/db", cfg.sharedConnStr)
	})

	t.Run("WithExtraPluginDir appends in order", func(t *testing.T) {
		cfg := &startConfig{}
		WithExtraPluginDir("testdata/lua/a")(cfg)
		WithExtraPluginDir("testdata/lua/b")(cfg)
		require.Equal(t, []string{"testdata/lua/a", "testdata/lua/b"}, cfg.extraPluginDirs)
	})
}

// TestStartWithExternalNATSAndSharedDatabaseWiresBothReplicas boots two
// in-process replicas against one real NATS broker and one shared database — the
// exact wiring the resilience suite relies on — and asserts the seams took
// effect: replica B joins replica A's database (WithSharedDatabase) and each
// replica wires its own real external-mode bus (WithExternalNATS). No chaos, so
// it is deterministic and gating-CI safe.
func TestStartWithExternalNATSAndSharedDatabaseWiresBothReplicas(t *testing.T) {
	ctx := context.Background()

	env, err := natstest.StartNATS(ctx)
	require.NoError(t, err, "start shared NATS broker")
	t.Cleanup(func() { _ = env.Terminate(context.Background()) })

	// Replica A: external broker + a fresh per-test database.
	replicaA := Start(t, WithExternalNATS(env.URL))
	require.NotNil(t, replicaA)
	require.NotEmpty(t, replicaA.ConnStr(), "replica A exposes its fresh database connStr")
	require.NotNil(t, replicaA.Bus(), "WithExternalNATS wires a real bus on replica A")

	// Replica B: SAME broker + SAME database as replica A.
	replicaB := Start(t, WithExternalNATS(env.URL), WithSharedDatabase(replicaA.ConnStr()))
	require.NotNil(t, replicaB)
	require.Equal(t, replicaA.ConnStr(), replicaB.ConnStr(),
		"WithSharedDatabase joins replica A's database")
	require.NotNil(t, replicaB.Bus(), "WithExternalNATS wires a real bus on replica B")
	require.NotSame(t, replicaA.Bus(), replicaB.Bus(),
		"each replica builds its own external-mode subsystem")
}

// TestStartPanicsWhenPluginDependentOptionLacksPlugins verifies the harness
// fail-fast guards: a plugin-dependent delivery/crypto option without
// WithInTreePlugins panics during option resolution (before any container work).
func TestStartPanicsWhenPluginDependentOptionLacksPlugins(t *testing.T) {
	cases := []struct {
		name string
		opt  StartOption
	}{
		{"WithPluginCrypto requires plugins", WithPluginCrypto()},
		{"WithFocusDelivery requires plugins", WithFocusDelivery()},
		{"WithSessionStreamDelivery requires plugins", WithSessionStreamDelivery()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Panics(t, func() { Start(t, tc.opt) },
				"option without WithInTreePlugins must panic")
		})
	}
}
