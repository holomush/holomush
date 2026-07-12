// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core_test

import (
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// Per-file forbidden-string assertions. These guard against regression
// of the w9ml T10 migration: system stamp sites MUST resolve their ID
// via the typed sentinel constants, not string literals.
//
// Paths are relative to the internal/core/ package directory (Go tests
// run from the package's source directory).

func TestEventStoreAdapterDoesNotUseStringWorldServiceLabel(t *testing.T) {
	body, err := os.ReadFile("../world/event_store_adapter.go")
	if errors.Is(err, fs.ErrNotExist) {
		// event_store_adapter.go was deleted in 05-06 (the post-commit emit path
		// folded into WR-01/D-03). The guarded string stamp cannot exist once the
		// file is gone, so the regression this guards against is structurally
		// impossible.
		t.Skip("../world/event_store_adapter.go removed in 05-06; guard is vacuously satisfied")
	}
	require.NoError(t, err)
	// Look for the actor-stamp form specifically; "world-service" can
	// legitimately appear in comments or registry strings.
	require.NotContains(t, string(body), `ID:   "world-service"`,
		"world-service stamp must use core.WorldServiceActorULID.String()")
	require.NotContains(t, string(body), `ID: "world-service"`,
		"world-service stamp must use core.WorldServiceActorULID.String()")
}

func TestGrpcServerDoesNotUseStringSystemLabel(t *testing.T) {
	body, err := os.ReadFile("../grpc/server.go")
	require.NoError(t, err)
	// grpc/server.go has many uses of the word "system" in comments;
	// scope to the actor-stamp form specifically.
	require.NotContains(t, string(body), `ID:   "system",`,
		"ActorSystem stamp must use core.ActorSystemID")
	require.NotContains(t, string(body), `ID: "system",`,
		"ActorSystem stamp must use core.ActorSystemID")
}

func TestCommandTypesDoesNotUseStringSystemLabel(t *testing.T) {
	body, err := os.ReadFile("../command/types.go")
	require.NoError(t, err)
	require.NotContains(t, string(body), `ID:   "system",`,
		"ActorSystem stamp must use core.ActorSystemID")
	require.NotContains(t, string(body), `ID: "system",`,
		"ActorSystem stamp must use core.ActorSystemID")
}
