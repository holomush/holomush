//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugin_test

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

var _ = Describe("Plugin verb registration", func() {
	var (
		pluginsDir string
		luaHost    *pluginlua.Host
		verbReg    *core.VerbRegistry
	)

	BeforeEach(func() {
		pluginsDir = GinkgoT().TempDir()
		luaHost = pluginlua.NewHost()
		var bootErr error
		verbReg, bootErr = core.BootstrapVerbRegistry("test")
		Expect(bootErr).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = luaHost.Close(context.Background()) })
	})

	writePlugin := func(name, yamlContent, luaContent string) {
		dir := filepath.Join(pluginsDir, name)
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(yamlContent), 0o644)).To(Succeed())
		if luaContent != "" {
			Expect(os.WriteFile(filepath.Join(dir, "main.lua"), []byte(luaContent), 0o644)).To(Succeed())
		}
	}

	It("registers plugin-declared verbs in the VerbRegistry", func() {
		writePlugin("verb-plugin", `
name: verb-plugin
version: 1.0.0
type: lua
verbs:
  - type: custom_say
    category: communication
    format: speech
    label: "says"
    display_target: terminal
  - type: custom_action
    category: communication
    format: action
    display_target: both
lua-plugin:
  entry: main.lua
`, "function on_event(e) end")

		mgr, mgrErr := plugins.NewManager(pluginsDir,
			plugins.WithLuaHost(luaHost),
			plugins.WithVerbRegistry(verbReg),
		)
		Expect(mgrErr).NotTo(HaveOccurred())
		Expect(mgr.LoadAll(context.Background())).To(Succeed())

		reg, ok := verbReg.Lookup("custom_say")
		Expect(ok).To(BeTrue(), "custom_say should be registered")
		Expect(reg.Category).To(Equal("communication"))
		Expect(reg.Format).To(Equal("speech"))
		Expect(reg.Label).To(Equal("says"))
		Expect(reg.Source).To(Equal("verb-plugin"))

		reg, ok = verbReg.Lookup("custom_action")
		Expect(ok).To(BeTrue(), "custom_action should be registered")
		Expect(reg.Source).To(Equal("verb-plugin"))
	})

	It("rejects a plugin whose verb type conflicts with a builtin", func() {
		// "system" is a host-owned event type registered by RegisterBuiltinTypes.
		// (Plugin-owned types like say/pose are no longer registered as builtins
		// per the plugin-boundary discipline; they're owned by their plugin.)
		writePlugin("conflict-plugin", `
name: conflict-plugin
version: 1.0.0
type: lua
verbs:
  - type: system
    category: system
    format: notification
    display_target: terminal
lua-plugin:
  entry: main.lua
`, "function on_event(e) end")

		mgr, mgrErr := plugins.NewManager(pluginsDir,
			plugins.WithLuaHost(luaHost),
			plugins.WithVerbRegistry(verbReg),
		)
		Expect(mgrErr).NotTo(HaveOccurred())
		err := mgr.LoadAll(context.Background())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("already registered"))
	})

	It("cleans up verbs when plugin load fails partway through verb list", func() {
		// Pre-register "conflict" so the second verb in the manifest fails
		Expect(verbReg.RegisterWithSource(core.VerbRegistration{
			Type: "conflict", Category: "system", Format: "notification",
			DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "pre-existing",
		}, "1.0.0")).To(Succeed())

		writePlugin("partial-fail", `
name: partial-fail
version: 1.0.0
type: lua
verbs:
  - type: good_verb
    category: communication
    format: action
    display_target: terminal
  - type: conflict
    category: system
    format: notification
    display_target: terminal
lua-plugin:
  entry: main.lua
`, "function on_event(e) end")

		mgr, mgrErr := plugins.NewManager(pluginsDir,
			plugins.WithLuaHost(luaHost),
			plugins.WithVerbRegistry(verbReg),
		)
		Expect(mgrErr).NotTo(HaveOccurred())
		err := mgr.LoadAll(context.Background())
		Expect(err).To(HaveOccurred())

		// good_verb should have been cleaned up by UnregisterBySource
		_, ok := verbReg.Lookup("good_verb")
		Expect(ok).To(BeFalse(), "good_verb should have been cleaned up after partial failure")
	})
})
