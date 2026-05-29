// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugins_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/stretchr/testify/mock"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	accesstypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// mockHelpCommandRegistry provides test commands for the help plugins.
type mockHelpCommandRegistry struct{}

func (m *mockHelpCommandRegistry) All() []command.CommandEntry {
	return []command.CommandEntry{
		command.NewTestEntry(command.CommandEntryConfig{
			Name:         "say",
			Help:         "Say something to the room",
			Usage:        "say <message>",
			HelpText:     "## Say\n\nSpeak a message to everyone in your location.",
			Capabilities: []command.Capability{{Action: "emit", Resource: "stream", Scope: command.ScopeLocal}},
			Source:       "communication",
		}),
		command.NewTestEntry(command.CommandEntryConfig{
			Name:         "look",
			Help:         "Look at your surroundings",
			Usage:        "look [target]",
			HelpText:     "## Look\n\nExamine your surroundings or a specific target.",
			Capabilities: []command.Capability{},
			Source:       "core",
		}),
		command.NewTestEntry(command.CommandEntryConfig{
			Name:         "dig",
			Help:         "Create a new room or exit",
			Usage:        "dig <direction> = <room name>",
			HelpText:     "## Dig\n\nCreate new rooms and connections.",
			Capabilities: []command.Capability{{Action: "write", Resource: "location", Scope: command.ScopeLocal}},
			Source:       "building",
		}),
	}
}

func (m *mockHelpCommandRegistry) Get(name string) (command.CommandEntry, bool) {
	for _, cmd := range m.All() {
		if cmd.Name == name {
			return cmd, true
		}
	}
	return command.CommandEntry{}, false
}

// helpFixture contains all components needed for help plugin integration tests.
type helpFixture struct {
	LuaHost *pluginlua.Host
	Plugin  *plugins.DiscoveredPlugin
	Cleanup func()
}

// setupHelpTest creates all components needed to test the help plugins
// using an AllowAll engine. For custom engine tests, use setupHelpTestWithEngine.
func setupHelpTest() (*helpFixture, error) {
	return setupHelpTestWithEngine(policytest.AllowAllEngine())
}

var _ = Describe("Help Plugin Integration", func() {
	var fixture *helpFixture

	BeforeEach(func() {
		var err error
		fixture, err = setupHelpTest()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if fixture != nil && fixture.Cleanup != nil {
			fixture.Cleanup()
		}
	})

	// All help tests use DeliverCommand. The help plugin's on_command returns
	// plain strings or {status, output} tables — no emit events. DeliverCommand
	// parses these correctly via parseCommandResponse; DeliverEvent feeds the
	// wrapped result to parseEmitEvents (wrong path) returning nil.

	Describe("help command", func() {
		It("lists all available commands when called without args", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-help", pluginsdk.CommandRequest{
				Command:     "help",
				Args:        "",
				CharacterID: "01HTEST000000000000000CHAR",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Status).To(Equal(pluginsdk.CommandOK))

			// Verify key content is present in the output text
			Expect(resp.Output).To(ContainSubstring("say"))
			Expect(resp.Output).To(ContainSubstring("look"))
			Expect(resp.Output).To(ContainSubstring("dig"))
		})

		It("shows detailed help for a specific command", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-help", pluginsdk.CommandRequest{
				Command:     "help",
				Args:        "say",
				CharacterID: "01HTEST000000000000000CHAR",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Status).To(Equal(pluginsdk.CommandOK))

			// Verify detailed help content
			Expect(resp.Output).To(ContainSubstring("say"))
			Expect(resp.Output).To(ContainSubstring("say <message>"))
			Expect(resp.Output).To(ContainSubstring("communication"))
		})

		It("returns error for unknown command", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-help", pluginsdk.CommandRequest{
				Command:     "help",
				Args:        "nonexistent",
				CharacterID: "01HTEST000000000000000CHAR",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Status).To(Equal(pluginsdk.CommandError))
			Expect(resp.Output).To(ContainSubstring("Unknown command"))
			Expect(resp.Output).To(ContainSubstring("nonexistent"))
		})

		It("searches commands by help field", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// "room" appears in the help text of "say" ("Say something to the room")
			// and "dig" ("Create a new room or exit"), but not "look". The help
			// plugin searches the name and help fields (not usage), so this exercises
			// help-field matching with selectivity.
			resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-help", pluginsdk.CommandRequest{
				Command:     "help",
				Args:        "search room",
				CharacterID: "01HTEST000000000000000CHAR",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Status).To(Equal(pluginsdk.CommandOK))

			Expect(resp.Output).To(ContainSubstring("say"))
			Expect(resp.Output).To(ContainSubstring("dig"))
			Expect(resp.Output).NotTo(ContainSubstring("look"))
		})
	})
})

// setupHelpTestWithEngine creates help plugin fixture with a custom access engine.
func setupHelpTestWithEngine(engine accesstypes.AccessPolicyEngine) (*helpFixture, error) {
	pluginsDir, err := findPluginsDir()
	if err != nil {
		return nil, err
	}
	helpDir := filepath.Join(pluginsDir, "core-help")

	if _, statErr := os.Stat(helpDir); os.IsNotExist(statErr) {
		return nil, statErr
	}

	registry := &mockHelpCommandRegistry{}
	hostFuncs := hostfunc.New(
		nil,
		hostfunc.WithCommandRegistry(registry),
		hostfunc.WithEngine(engine),
	)
	luaHost := pluginlua.NewHostWithFunctions(hostFuncs)

	manager, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	if mgrErr != nil {
		_ = luaHost.Close(context.Background())
		return nil, mgrErr
	}

	ctx := context.Background()
	discovered, err := manager.Discover(ctx)
	if err != nil {
		_ = luaHost.Close(ctx)
		return nil, err
	}

	var helpPlugin *plugins.DiscoveredPlugin
	for _, dp := range discovered {
		if dp.Manifest.Name == "core-help" {
			helpPlugin = dp
			break
		}
	}

	if helpPlugin == nil {
		_ = luaHost.Close(ctx)
		return nil, os.ErrNotExist
	}

	if err := luaHost.Load(ctx, helpPlugin.Manifest, helpPlugin.Dir); err != nil {
		_ = luaHost.Close(ctx)
		return nil, err
	}

	return &helpFixture{
		LuaHost: luaHost,
		Plugin:  helpPlugin,
		Cleanup: func() {
			_ = luaHost.Close(context.Background())
		},
	}, nil
}

var _ = Describe("Help Plugin – list_commands result format", func() {
	Describe("extracting commands from wrapper table", func() {
		It("extracts .commands field and iterates correctly", func() {
			// This test verifies the Lua plugin correctly unwraps the
			// {commands: [...], incomplete: bool} result from list_commands.
			// Uses DeliverCommand which correctly parses the on_command return shape.
			fixture, err := setupHelpTest()
			Expect(err).NotTo(HaveOccurred())
			defer fixture.Cleanup()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-help", pluginsdk.CommandRequest{
				Command:     "help",
				Args:        "",
				CharacterID: "01HTEST000000000000000CHAR",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Status).To(Equal(pluginsdk.CommandOK),
				"help list should return CommandOK, not error (CommandOK==0 bug would mean #commands==0)")

			Expect(resp.Output).To(ContainSubstring("say"))
			Expect(resp.Output).To(ContainSubstring("look"))
			Expect(resp.Output).To(ContainSubstring("dig"))
		})

		It("search_commands extracts .commands field correctly", func() {
			fixture, err := setupHelpTest()
			Expect(err).NotTo(HaveOccurred())
			defer fixture.Cleanup()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-help", pluginsdk.CommandRequest{
				Command:     "help",
				Args:        "search room",
				CharacterID: "01HTEST000000000000000CHAR",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Status).To(Equal(pluginsdk.CommandOK),
				"search should find commands and return CommandOK")

			Expect(resp.Output).To(ContainSubstring("say"))
		})
	})

	Describe("error engine handling", func() {
		It("renders the available partial list when list_commands engine errors", func() {
			// When the policy engine errors, list_commands returns a populated list
			// of no-capability commands (always included; here "look") plus
			// incomplete=true and a non-nil err — the SOFT-failure tier of the host
			// contract (internal/plugin/hostfunc/commands.go). The Lua handler honors
			// it: render the usable commands with an incompleteness indicator rather
			// than hiding everything behind a blanket message (holomush-869o8). The
			// blanket "temporarily unavailable" message is reserved for a genuinely
			// nil result (registry/engine nil), which an error engine never produces.
			errorEngine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
			fixture, err := setupHelpTestWithEngine(errorEngine)
			Expect(err).NotTo(HaveOccurred())
			defer fixture.Cleanup()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-help", pluginsdk.CommandRequest{
				Command:     "help",
				Args:        "",
				CharacterID: "01HTEST000000000000000CHAR",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())

			Expect(resp.Status).To(Equal(pluginsdk.CommandOK),
				"a populated-but-incomplete list must render, not fail")
			Expect(resp.Output).NotTo(ContainSubstring("temporarily unavailable"),
				"the blanket message is reserved for a genuinely nil result")
			Expect(resp.Output).To(ContainSubstring("look"),
				"no-capability commands are always available and must be shown")
			Expect(resp.Output).To(ContainSubstring("incomplete"),
				"the user must be told the list may be incomplete")
		})

		It("returns full command list when engine succeeds", func() {
			fixture, err := setupHelpTest() // AllowAll engine, no errors
			Expect(err).NotTo(HaveOccurred())
			defer fixture.Cleanup()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-help", pluginsdk.CommandRequest{
				Command:     "help",
				Args:        "",
				CharacterID: "01HTEST000000000000000CHAR",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())

			Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
			Expect(resp.Output).NotTo(ContainSubstring("unavailable"))
		})

		It("renders partial search results when search engine errors", func() {
			// search_commands shares list_commands' contract: an engine error yields
			// a populated list of no-capability commands + incomplete=true, so the
			// search runs over the usable subset and renders matches with an
			// incompleteness indicator rather than a blanket failure (holomush-869o8).
			errorEngine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
			fixture, err := setupHelpTestWithEngine(errorEngine)
			Expect(err).NotTo(HaveOccurred())
			defer fixture.Cleanup()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-help", pluginsdk.CommandRequest{
				Command:     "help",
				Args:        "search look",
				CharacterID: "01HTEST000000000000000CHAR",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())

			Expect(resp.Status).To(Equal(pluginsdk.CommandOK),
				"a populated-but-incomplete search must render, not fail")
			Expect(resp.Output).NotTo(ContainSubstring("temporarily unavailable"),
				"the blanket message is reserved for a genuinely nil result")
			Expect(resp.Output).To(ContainSubstring("look"),
				"the matching no-capability command must be shown")
			Expect(resp.Output).To(ContainSubstring("incomplete"),
				"the user must be told the search list may be incomplete")
		})

		It("renders the granted partial list with an incomplete indicator when only some commands' ABAC evaluations error", func() {
			// The genuinely *partial* path the all-error NewErrorEngine cases above
			// cannot reach: one capability-gated command survives, another is hidden
			// by a per-command engine error, and result.incomplete must cross the
			// Go→Lua host serialization boundary as true so the indicator renders
			// rather than the whole list being discarded (the holomush-869o8
			// regression; holomush-mexs). This drives that boundary through the full
			// Lua host stack (hostfunc.New → pluginlua.Host → Load → DeliverCommand),
			// complementing the hostfunc-level unit test
			// TestListCommandsIncompleteFieldTrueWhenPartialErrors.
			//
			// Layer-1 Evaluate allows everything so control reaches the capability
			// pre-flight; the subject matcher is pinned to the resolved CharacterSubject
			// so a regression in subject construction would surface here.
			const charID = "01HTEST000000000000000CHAR"
			subject := access.CharacterSubject(charID)

			partialEngine := policytest.NewMockAccessPolicyEngine(GinkgoT())
			partialEngine.On("Evaluate", mock.Anything, mock.Anything).
				Return(accesstypes.NewDecision(accesstypes.EffectAllow, "test-allow", ""), nil)
			partialEngine.On("CanPerformAction", mock.Anything, subject, "emit", "stream", "local").
				Return(true, nil).Maybe() // say: granted
			partialEngine.On("CanPerformAction", mock.Anything, subject, "write", "location", "local").
				Return(false, errors.New("policy store unavailable")).Maybe() // dig: engine error

			fixture, err := setupHelpTestWithEngine(partialEngine)
			Expect(err).NotTo(HaveOccurred())
			defer fixture.Cleanup()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-help", pluginsdk.CommandRequest{
				Command:     "help",
				Args:        "",
				CharacterID: charID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())

			Expect(resp.Status).To(Equal(pluginsdk.CommandOK),
				"a populated-but-incomplete list must render, not fail")
			Expect(resp.Output).To(ContainSubstring("say"),
				"the capability-gated command whose ABAC succeeded must be shown")
			Expect(resp.Output).To(ContainSubstring("look"),
				"no-capability commands are always available and must be shown")
			Expect(resp.Output).NotTo(ContainSubstring("dig"),
				"the capability-gated command whose ABAC errored must be hidden")
			Expect(resp.Output).To(ContainSubstring("incomplete"),
				"result.incomplete must cross the host boundary as true and surface the indicator")
			Expect(resp.Output).NotTo(ContainSubstring("temporarily unavailable"),
				"the blanket message is reserved for a genuinely nil result")
		})
	})
})
