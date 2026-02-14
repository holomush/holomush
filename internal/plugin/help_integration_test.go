// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugins_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/capability"
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
			Capabilities: []string{"comms.say"},
			Source:       "communication",
		}),
		command.NewTestEntry(command.CommandEntryConfig{
			Name:         "look",
			Help:         "Look at your surroundings",
			Usage:        "look [target]",
			HelpText:     "## Look\n\nExamine your surroundings or a specific target.",
			Capabilities: []string{},
			Source:       "core",
		}),
		command.NewTestEntry(command.CommandEntryConfig{
			Name:         "dig",
			Help:         "Create a new room or exit",
			Usage:        "dig <direction> = <room name>",
			HelpText:     "## Dig\n\nCreate new rooms and connections.",
			Capabilities: []string{"building.dig"},
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
	LuaHost  *pluginlua.Host
	Enforcer *capability.Enforcer
	Plugin   *plugins.DiscoveredPlugin
	Cleanup  func()
}

// setupHelpTest creates all components needed to test the help plugins.
func setupHelpTest() (*helpFixture, error) {
	pluginsDir, err := findPluginsDir()
	if err != nil {
		return nil, err
	}
	helpDir := filepath.Join(pluginsDir, "help")

	if _, statErr := os.Stat(helpDir); os.IsNotExist(statErr) {
		return nil, statErr
	}

	enforcer := capability.NewEnforcer()
	registry := &mockHelpCommandRegistry{}
	hostFuncs := hostfunc.New(nil, enforcer,
		hostfunc.WithCommandRegistry(registry),
		hostfunc.WithEngine(policytest.AllowAllEngine()),
	)
	luaHost := pluginlua.NewHostWithFunctions(hostFuncs)

	manager := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))

	ctx := context.Background()
	discovered, err := manager.Discover(ctx)
	if err != nil {
		_ = luaHost.Close(ctx)
		return nil, err
	}

	var helpPlugin *plugins.DiscoveredPlugin
	for _, dp := range discovered {
		if dp.Manifest.Name == "help" {
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

	if err := enforcer.SetGrants("help", helpPlugin.Manifest.Capabilities); err != nil {
		_ = luaHost.Close(ctx)
		return nil, err
	}

	return &helpFixture{
		LuaHost:  luaHost,
		Enforcer: enforcer,
		Plugin:   helpPlugin,
		Cleanup: func() {
			_ = luaHost.Close(context.Background())
		},
	}, nil
}

// makeCommandPayload creates a JSON payload for a command event.
func makeCommandPayload(name, args string) string {
	payload := map[string]any{
		"name":           name,
		"args":           args,
		"character_id":   "01HTEST000000000000000CHAR",
		"location_id":    "01HTEST000000000000000ROOM",
		"character_name": "TestPlayer",
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

// parsePayload parses a JSON payload string into a map.
func parsePayload(payload string) map[string]any {
	var result map[string]any
	_ = json.Unmarshal([]byte(payload), &result)
	return result
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

	Describe("help command", func() {
		It("lists all available commands when called without args", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			event := pluginsdk.Event{
				ID:        "01HTEST",
				Stream:    "character:01HTEST000000000000000CHAR",
				Type:      pluginsdk.EventType("command"),
				Timestamp: time.Now().UnixMilli(),
				ActorKind: pluginsdk.ActorCharacter,
				ActorID:   "01HTEST000000000000000CHAR",
				Payload:   makeCommandPayload("help", ""),
			}

			result, err := fixture.LuaHost.DeliverEvent(ctx, "help", event)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(len(result)).To(BeNumerically(">=", 1))

			// Verify events were emitted
			outputEvent := result[0]
			Expect(outputEvent.Type).To(Equal(pluginsdk.EventType("help")))

			// Check that the output contains command names
			payload := parsePayload(outputEvent.Payload)
			message, hasMessage := payload["message"].(string)
			Expect(hasMessage).To(BeTrue())

			// Verify key content is present
			Expect(message).To(ContainSubstring("say"))
			Expect(message).To(ContainSubstring("look"))
			Expect(message).To(ContainSubstring("dig"))
		})

		It("shows detailed help for a specific command", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			event := pluginsdk.Event{
				ID:        "01HTEST",
				Stream:    "character:01HTEST000000000000000CHAR",
				Type:      pluginsdk.EventType("command"),
				Timestamp: time.Now().UnixMilli(),
				ActorKind: pluginsdk.ActorCharacter,
				ActorID:   "01HTEST000000000000000CHAR",
				Payload:   makeCommandPayload("help", "say"),
			}

			result, err := fixture.LuaHost.DeliverEvent(ctx, "help", event)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(len(result)).To(BeNumerically(">=", 1))

			outputEvent := result[0]
			payload := parsePayload(outputEvent.Payload)
			message := payload["message"].(string)

			// Verify detailed help content
			Expect(message).To(ContainSubstring("say"))
			Expect(message).To(ContainSubstring("say <message>"))
			Expect(message).To(ContainSubstring("communication"))
		})

		It("returns error for unknown command", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			event := pluginsdk.Event{
				ID:        "01HTEST",
				Stream:    "character:01HTEST000000000000000CHAR",
				Type:      pluginsdk.EventType("command"),
				Timestamp: time.Now().UnixMilli(),
				ActorKind: pluginsdk.ActorCharacter,
				ActorID:   "01HTEST000000000000000CHAR",
				Payload:   makeCommandPayload("help", "nonexistent"),
			}

			result, err := fixture.LuaHost.DeliverEvent(ctx, "help", event)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(len(result)).To(BeNumerically(">=", 1))

			outputEvent := result[0]
			Expect(outputEvent.Type).To(Equal(pluginsdk.EventType("error")))

			payload := parsePayload(outputEvent.Payload)
			message := payload["message"].(string)
			Expect(message).To(ContainSubstring("Unknown command"))
			Expect(message).To(ContainSubstring("nonexistent"))
		})

		It("searches commands by keyword", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			event := pluginsdk.Event{
				ID:        "01HTEST",
				Stream:    "character:01HTEST000000000000000CHAR",
				Type:      pluginsdk.EventType("command"),
				Timestamp: time.Now().UnixMilli(),
				ActorKind: pluginsdk.ActorCharacter,
				ActorID:   "01HTEST000000000000000CHAR",
				Payload:   makeCommandPayload("help", "search room"),
			}

			result, err := fixture.LuaHost.DeliverEvent(ctx, "help", event)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(len(result)).To(BeNumerically(">=", 1))

			outputEvent := result[0]
			payload := parsePayload(outputEvent.Payload)
			message := payload["message"].(string)

			// Should find commands mentioning "room"
			Expect(message).To(ContainSubstring("say")) // "Say something to the room"
			Expect(message).To(ContainSubstring("dig")) // "Create a new room"
		})

		It("searches commands by usage field", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Search for "message" which only appears in usage: "say <message>"
			event := pluginsdk.Event{
				ID:        "01HTEST",
				Stream:    "character:01HTEST000000000000000CHAR",
				Type:      pluginsdk.EventType("command"),
				Timestamp: time.Now().UnixMilli(),
				ActorKind: pluginsdk.ActorCharacter,
				ActorID:   "01HTEST000000000000000CHAR",
				Payload:   makeCommandPayload("help", "search message"),
			}

			result, err := fixture.LuaHost.DeliverEvent(ctx, "help", event)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(len(result)).To(BeNumerically(">=", 1))

			outputEvent := result[0]
			payload := parsePayload(outputEvent.Payload)
			message := payload["message"].(string)

			// Should find "say" command via usage field "say <message>"
			Expect(message).To(ContainSubstring("say"))
			// Should NOT find "look" or "dig" (they don't have "message" in any field)
			Expect(message).NotTo(ContainSubstring("look"))
			Expect(message).NotTo(ContainSubstring("dig"))
		})
	})
})

// Ensure slices is used to avoid import error
var _ = slices.Contains([]string{}, "")
