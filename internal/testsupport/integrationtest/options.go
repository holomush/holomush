// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

// WithExtraPluginDir stages an additional plugin directory (e.g. a test-only
// Lua fixture under test/integration/.../testdata/lua/<name>) into the plugin
// load path so the real plugin subsystem loads it alongside the in-tree
// plugins. Used by focus runtime-symmetry tests that need a Lua plugin which
// calls the auto_focus_on_join hostfunc. dir is resolved relative to the test's
// package directory (Go runs tests with CWD = package dir).
func WithExtraPluginDir(dir string) StartOption {
	return func(c *startConfig) { c.extraPluginDirs = append(c.extraPluginDirs, dir) }
}
