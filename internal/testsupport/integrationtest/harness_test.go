// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// TestSceneServiceClientResolves verifies that Server.SceneServiceClient()
// resolves the loaded core-scenes SceneService from the plugin registry and
// returns a non-nil gRPC client.
func TestSceneServiceClientResolves(t *testing.T) {
	ts := integrationtest.Start(t, integrationtest.WithInTreePlugins())
	defer ts.Stop()
	require.NotNil(t, ts.SceneServiceClient())
}

// TestSessionCreateSceneReturnsULID verifies that Session.CreateScene drives a
// real SceneService.CreateScene RPC and returns the created scene's ULID.
func TestSessionCreateSceneReturnsULID(t *testing.T) {
	// WithPluginCrypto wires the plugin event emitter (required for
	// SceneService.CreateScene, which emits a scene_created event);
	// WithInTreePlugins alone leaves the emitter unconfigured (plugins.go:155-161).
	ts := integrationtest.Start(t, integrationtest.WithInTreePlugins(), integrationtest.WithPluginCrypto())
	defer ts.Stop()
	loc := ts.NewLocation(context.Background())
	alice := ts.ConnectAuthed(context.Background(), "Alice")
	sceneID := alice.CreateScene(context.Background(), loc)
	require.NotEqual(t, ulid.ULID{}, sceneID)
}
