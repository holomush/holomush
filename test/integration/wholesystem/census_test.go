// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package wholesystem_test

import (
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// expectedPlugins is the in-tree set discover-all loads. NOTE: these are plugin
// MANIFEST names (ListPlugins returns manifest names, not dir names): the
// setting-crossroads/setting-skeleton dirs have manifest names crossroads/skeleton,
// but TypeSetting is not handled by loadPlugin (falls through to default/skip with
// warning), so setting plugins do NOT appear in ListPlugins(). Only Lua and Binary
// plugins are registered in Manager.loaded.
var expectedPlugins = []string{
	"core-aliases", "core-building", "core-communication", "core-help",
	"core-objects", "core-scenes", "echo-bot",
	"test-abac-widget",
}

var _ = Describe("whole-system plugin load (INV-5)", Ordered, func() {
	var srv *integrationtest.Server

	BeforeAll(func() {
		srv = integrationtest.Start(suiteT, integrationtest.WithInTreePlugins())
	})
	AfterAll(func() {
		if srv != nil {
			srv.Stop()
		}
	})

	It("loads every in-tree plugin via Manager.LoadAll", func() {
		loaded := srv.PluginManager().ListPlugins()
		for _, name := range expectedPlugins {
			Expect(loaded).To(ContainElement(name), "plugin %q must load", name)
		}
	})

	It("registers plugin commands in the dispatcher registry", func() {
		reg := srv.CommandRegistry()
		_, ok := reg.Get("help")
		Expect(ok).To(BeTrue(), "core-help command must be registered")
	})
})
