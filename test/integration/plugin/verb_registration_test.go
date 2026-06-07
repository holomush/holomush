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
  - type: verb-plugin:custom_say
    category: communication
    format: speech
    label: "says"
    display_target: terminal
  - type: verb-plugin:custom_action
    category: communication
    format: action
    display_target: both
lua-plugin:
  entry: main.lua
`, "function on_event(e) end")

		mgr, mgrErr := plugins.NewManager(
			pluginsDir,
			plugins.WithLuaHost(luaHost),
			plugins.WithVerbRegistry(verbReg),
		)
		Expect(mgrErr).NotTo(HaveOccurred())
		Expect(mgr.LoadAll(context.Background())).To(Succeed())

		reg, ok := verbReg.Lookup("verb-plugin:custom_say")
		Expect(ok).To(BeTrue(), "custom_say should be registered")
		Expect(reg.Category).To(Equal("communication"))
		Expect(reg.Format).To(Equal("speech"))
		Expect(reg.Label).To(Equal("says"))
		Expect(reg.Source).To(Equal("verb-plugin"))

		reg, ok = verbReg.Lookup("verb-plugin:custom_action")
		Expect(ok).To(BeTrue(), "custom_action should be registered")
		Expect(reg.Source).To(Equal("verb-plugin"))
	})

	It("skips a plugin that declares an unqualified (bare) verb type", func() {
		// The qualification gate (INV-PLUGIN-40, holomush-aneim) rejects a bare
		// verb type at manifest parse, so the plugin is skipped at discovery
		// (warn + continue). A plugin can no longer shadow a host-owned builtin
		// like "system": every plugin verb MUST be <plugin>:<verb>.
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

		mgr, mgrErr := plugins.NewManager(
			pluginsDir,
			plugins.WithLuaHost(luaHost),
			plugins.WithVerbRegistry(verbReg),
		)
		Expect(mgrErr).NotTo(HaveOccurred())
		// An invalid manifest is skipped, not a hard load error.
		Expect(mgr.LoadAll(context.Background())).To(Succeed())
		_, loaded := mgr.GetLoadedPlugin("conflict-plugin")
		Expect(loaded).To(BeFalse(), "plugin with a bare verb type must be skipped at discovery")
	})

	It("cleans up verbs when plugin load fails partway through verb list", func() {
		// Pre-register "partial-fail:conflict" so the second verb in the manifest fails
		Expect(verbReg.RegisterWithSource(core.VerbRegistration{
			Type: "partial-fail:conflict", Category: "system", Format: "notification",
			DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "pre-existing",
		}, "1.0.0")).To(Succeed())

		writePlugin("partial-fail", `
name: partial-fail
version: 1.0.0
type: lua
verbs:
  - type: partial-fail:good_verb
    category: communication
    format: action
    display_target: terminal
  - type: partial-fail:conflict
    category: system
    format: notification
    display_target: terminal
lua-plugin:
  entry: main.lua
`, "function on_event(e) end")

		mgr, mgrErr := plugins.NewManager(
			pluginsDir,
			plugins.WithLuaHost(luaHost),
			plugins.WithVerbRegistry(verbReg),
		)
		Expect(mgrErr).NotTo(HaveOccurred())
		err := mgr.LoadAll(context.Background())
		Expect(err).To(HaveOccurred())

		// good_verb should have been cleaned up by UnregisterBySource
		_, ok := verbReg.Lookup("partial-fail:good_verb")
		Expect(ok).To(BeFalse(), "good_verb should have been cleaned up after partial failure")
	})
})
