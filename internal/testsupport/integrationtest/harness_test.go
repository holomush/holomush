// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest_test

import (
	"testing"

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
