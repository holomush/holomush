// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package pluginparity holds the cross-runtime parity tests that bind the
// plugin host-capability invariants. They stand up the SAME hostcap capability
// servers behind BOTH runtime adapters — the binary *goplugin.Host and the Lua
// *hostfunc.Functions-backed adapter — over the SAME in-process transport, and
// assert the two runtimes consume the single shared RPC contract identically.
//
// The suite is Ginkgo/Gomega per the test/integration convention. Each spec
// stands up its own pair of in-process endpoints; per-spec resources (hosts,
// conns) tear down via DeferCleanup inside the endpoint helpers, so nothing
// accumulates across specs.
package pluginparity

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// TestPluginParity is the Ginkgo entry point for the plugin-parity suite.
func TestPluginParity(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Plugin Parity Integration Suite")
}
