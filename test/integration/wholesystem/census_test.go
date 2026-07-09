// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package wholesystem_test

import (
	"context"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/access"
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
	"core-aliases", "core-building", "core-channels", "core-communication",
	"core-help", "core-objects", "core-scenes", "echo-bot",
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

	// INV-PLUGIN-54: loading every in-tree plugin through the real path now also
	// validates that each binary plugin declared the host capabilities its code
	// consumes — a misdeclared plugin fails Init → fails load → fails this census,
	// so the census is the integration guard for the capability-declaration invariant.
	It("loads core-scenes with its declared host capabilities (INV-PLUGIN-54 guard)", func() {
		loaded := srv.PluginManager().ListPlugins()
		Expect(loaded).To(ContainElement("core-scenes"),
			"core-scenes (heaviest capability consumer) must load with its declared capabilities")
	})

	It("registers plugin commands in the dispatcher registry", func() {
		reg := srv.CommandRegistry()
		_, ok := reg.Get("help")
		Expect(ok).To(BeTrue(), "core-help command must be registered")
	})

	// Wiring regression (holomush-2zjio): the production PluginSubsystem.Start()
	// path MUST late-bind a non-nil command querier into the stack via
	// SetCommandQuerier. hostfunc.New is constructed before cmdRegistry exists,
	// so without the late-bind the Lua list_commands would return "command
	// registry not available". Driving the REAL Start() here (not a hand-wired
	// hostfunc.New) proves the production wiring yields a non-nil querier that
	// enumerates real registered commands (design spec INV-COMMAND-1: single filter).
	It("late-binds a non-nil command querier that lists real commands (Start wiring)", func() {
		q := srv.CommandQuerier()
		Expect(q).NotTo(BeNil(), "Start() must produce a non-nil command querier")

		subject := access.CharacterSubject(ulid.Make().String())
		res, err := q.Available(context.Background(), subject)
		Expect(err).NotTo(HaveOccurred())

		var names []string
		for _, c := range res.Commands {
			names = append(names, c.Name)
		}
		Expect(names).To(ContainElement("help"),
			"the production querier must enumerate real registered commands, not return 'command registry not available'")
	})
})
