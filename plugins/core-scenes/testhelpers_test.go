// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	_ "embed"

	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

//go:embed plugin.yaml
var embeddedManifest []byte

// testingT is the minimal testing surface the manifest helpers need, satisfied
// by both *testing.T (unit suites) and Ginkgo's GinkgoT() (the integration-tagged
// scheduler/snapshot suites call newTestService). testing.TB cannot be used here:
// GinkgoT() does not implement its sealed private() method. These three methods
// are exactly what require.NoError + t.Helper() consume.
type testingT interface {
	Helper()
	Errorf(format string, args ...any)
	FailNow()
}

// manifestServiceConfig parses plugin.yaml and returns a *pluginv1.ServiceConfig
// populated with the manifest defaults (no server-side overrides). Used by
// newTestService to source config from the real manifest, so tests exercise the
// same config path as production Init.
func manifestServiceConfig(t testingT) *pluginv1.ServiceConfig {
	t.Helper()
	m, err := plugins.ParseManifest(embeddedManifest)
	require.NoError(t, err, "testhelpers: parse embedded plugin.yaml")
	merged, err := plugins.MergePluginConfig(m.Config, nil)
	require.NoError(t, err, "testhelpers: merge manifest config with no overrides")
	return &pluginv1.ServiceConfig{PluginConfig: merged}
}

// newTestService creates a *SceneServiceImpl backed by store and applies the
// manifest config defaults via applyConfig. This is the canonical test
// constructor that replaces NewSceneServiceImpl(store) in all test files —
// it ensures tests source config from the real manifest (INV-PLUGIN-7) rather
// than a hard-coded Go default.
func newTestService(t testingT, store sceneStorer) *SceneServiceImpl {
	t.Helper()
	p := &scenePlugin{service: NewSceneServiceImpl(store)}
	require.NoError(t, p.applyConfig(manifestServiceConfig(t)), "testhelpers: applyConfig")
	return p.service
}
