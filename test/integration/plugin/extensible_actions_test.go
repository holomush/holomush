// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
)

var _ = Describe("Plugin loading with custom actions", func() {
	var (
		pluginsDir string
		luaHost    *pluginlua.Host
	)

	BeforeEach(func() {
		pluginsDir = GinkgoT().TempDir()
		luaHost = pluginlua.NewHost()
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

	It("loads a plugin that declares non-core actions in the actions field", func() {
		writePlugin("channels", `
name: channels
version: 1.0.0
type: lua
actions: [join, leave]
commands:
  - name: channel
    capabilities:
      - action: join
        resource: location
      - action: leave
        resource: location
lua-plugin:
  entry: main.lua
`, "function on_event(e) end")

		mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))
		Expect(mgr.LoadAll(context.Background())).To(Succeed())
		Expect(mgr.ListPlugins()).To(ContainElement("channels"))
	})

	It("rejects a plugin whose capability uses an undeclared action", func() {
		writePlugin("bad-plugin", `
name: bad-plugin
version: 1.0.0
type: lua
commands:
  - name: channel
    capabilities:
      - action: join
        resource: location
lua-plugin:
  entry: main.lua
`, "function on_event(e) end")

		mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))
		err := mgr.LoadAll(context.Background())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("join"))
	})

	It("loads two plugins where one borrows an action declared by the other", func() {
		// Plugin A declares "join"; Plugin B uses it without declaring it.
		writePlugin("action-declarer", `
name: action-declarer
version: 1.0.0
type: binary
actions: [join]
binary-plugin:
  executable: action-declarer
`, "")

		writePlugin("action-borrower", `
name: action-borrower
version: 1.0.0
type: lua
commands:
  - name: channel
    capabilities:
      - action: join
        resource: location
lua-plugin:
  entry: main.lua
`, "function on_event(e) end")

		// No binary host — declarer is silently skipped; its declared actions
		// still feed CollectActions during Phase 2.
		mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))
		Expect(mgr.LoadAll(context.Background())).To(Succeed())
		Expect(mgr.ListPlugins()).To(ContainElement("action-borrower"))
	})
})
