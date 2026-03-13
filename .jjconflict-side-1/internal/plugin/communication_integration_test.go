// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugins_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/capability"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// communicationFixture contains all components needed for communication plugin integration tests.
type communicationFixture struct {
	LuaHost  *pluginlua.Host
	Enforcer *capability.Enforcer
	Plugin   *plugins.DiscoveredPlugin
	Cleanup  func()
}

// setupCommunicationTest creates all components needed to test the communication plugins.
func setupCommunicationTest() (*communicationFixture, error) {
	pluginsDir, err := findPluginsDir()
	if err != nil {
		return nil, err
	}
	commDir := filepath.Join(pluginsDir, "communication")

	if _, statErr := os.Stat(commDir); os.IsNotExist(statErr) {
		return nil, statErr
	}

	enforcer := capability.NewEnforcer()
	hostFuncs := hostfunc.New(nil, enforcer)
	luaHost := pluginlua.NewHostWithFunctions(hostFuncs)

	manager := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))

	ctx := context.Background()
	discovered, err := manager.Discover(ctx)
	if err != nil {
		_ = luaHost.Close(ctx)
		return nil, err
	}

	var commPlugin *plugins.DiscoveredPlugin
	for _, dp := range discovered {
		if dp.Manifest.Name == "communication" {
			commPlugin = dp
			break
		}
	}

	if commPlugin == nil {
		_ = luaHost.Close(ctx)
		return nil, os.ErrNotExist
	}

	if err := luaHost.Load(ctx, commPlugin.Manifest, commPlugin.Dir); err != nil {
		_ = luaHost.Close(ctx)
		return nil, err
	}

	if err := enforcer.SetGrants("communication", commPlugin.Manifest.Capabilities); err != nil {
		_ = luaHost.Close(ctx)
		return nil, err
	}

	return &communicationFixture{
		LuaHost:  luaHost,
		Enforcer: enforcer,
		Plugin:   commPlugin,
		Cleanup: func() {
			_ = luaHost.Close(context.Background())
		},
	}, nil
}

var _ = Describe("Communication Plugin Integration", func() {
	var fixture *communicationFixture

	BeforeEach(func() {
		var err error
		fixture, err = setupCommunicationTest()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		fixture.Cleanup()
	})

	Describe("Plugin Discovery and Loading", func() {
		It("has correct manifest type", func() {
			Expect(fixture.Plugin.Manifest.Type).To(Equal(plugins.TypeLua))
		})

		It("has correct version", func() {
			Expect(fixture.Plugin.Manifest.Version).To(Equal("1.0.0"))
		})

		It("subscribes to command events", func() {
			Expect(slices.Contains(fixture.Plugin.Manifest.Events, "command")).To(BeTrue())
		})

		It("has events.emit.location capability", func() {
			Expect(slices.Contains(fixture.Plugin.Manifest.Capabilities, "events.emit.location")).To(BeTrue())
		})

		It("declares say command with comms.say capability", func() {
			var sayCmd *plugins.CommandSpec
			for i := range fixture.Plugin.Manifest.Commands {
				if fixture.Plugin.Manifest.Commands[i].Name == "say" {
					sayCmd = &fixture.Plugin.Manifest.Commands[i]
					break
				}
			}
			Expect(sayCmd).NotTo(BeNil())
			Expect(sayCmd.Capabilities).To(ContainElement("comms.say"))
			Expect(sayCmd.Help).To(Equal("Send a message to the room"))
			Expect(sayCmd.Usage).To(Equal("say <message>"))
		})

		It("declares pose command with comms.pose capability", func() {
			var poseCmd *plugins.CommandSpec
			for i := range fixture.Plugin.Manifest.Commands {
				if fixture.Plugin.Manifest.Commands[i].Name == "pose" {
					poseCmd = &fixture.Plugin.Manifest.Commands[i]
					break
				}
			}
			Expect(poseCmd).NotTo(BeNil())
			Expect(poseCmd.Capabilities).To(ContainElement("comms.pose"))
			Expect(poseCmd.Help).To(Equal("Perform an action in the room"))
		})

		It("declares emit command with comms.emit capability", func() {
			var emitCmd *plugins.CommandSpec
			for i := range fixture.Plugin.Manifest.Commands {
				if fixture.Plugin.Manifest.Commands[i].Name == "emit" {
					emitCmd = &fixture.Plugin.Manifest.Commands[i]
					break
				}
			}
			Expect(emitCmd).NotTo(BeNil())
			Expect(emitCmd.Capabilities).To(ContainElement("comms.emit"))
			Expect(emitCmd.Help).To(Equal("Emit raw text to the room (privileged)"))
		})
	})

	Describe("Say Command Event Handling", func() {
		Context("when receiving say command event", func() {
			It("emits say event to location stream with double quotes by default", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01ABC",
					Stream:    "character:char123",
					Type:      pluginsdk.EventType("command"),
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char123",
					Payload:   `{"name":"say","args":"Hello everyone!","character_name":"Alice","location_id":"loc456"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "communication", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(HaveLen(1))
				Expect(emits[0].Stream).To(Equal("location:loc456"))
				Expect(emits[0].Type).To(Equal(pluginsdk.EventTypeSay))
				Expect(emits[0].Payload).To(ContainSubstring(`Alice says, \"Hello everyone!\"`))
				Expect(emits[0].Payload).To(ContainSubstring(`"speaker":"Alice"`))
			})
		})

		Context("when say command has empty message", func() {
			It("returns error message to character", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01DEF",
					Stream:    "character:char123",
					Type:      pluginsdk.EventType("command"),
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char123",
					Payload:   `{"name":"say","args":"","character_name":"Alice","location_id":"loc456","character_id":"char123"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "communication", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(HaveLen(1))
				Expect(emits[0].Stream).To(Equal("character:char123"))
				Expect(string(emits[0].Type)).To(Equal("error"))
				Expect(emits[0].Payload).To(ContainSubstring("What do you want to say?"))
			})
		})
	})

	Describe("Pose Command Event Handling", func() {
		Context("when receiving pose command event", func() {
			It("emits pose event with character name and space prepended", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01GHI",
					Stream:    "character:char123",
					Type:      pluginsdk.EventType("command"),
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char123",
					Payload:   `{"name":"pose","args":"waves hello.","character_name":"Bob","location_id":"loc456"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "communication", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(HaveLen(1))
				Expect(emits[0].Stream).To(Equal("location:loc456"))
				Expect(emits[0].Type).To(Equal(pluginsdk.EventTypePose))
				Expect(emits[0].Payload).To(ContainSubstring(`Bob waves hello.`))
				Expect(emits[0].Payload).To(ContainSubstring(`"actor":"Bob"`))
			})
		})

		Context("when pose command uses : variant", func() {
			It("includes space before action (same as regular pose)", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01GHI1",
					Stream:    "character:char123",
					Type:      pluginsdk.EventType("command"),
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char123",
					Payload:   `{"name":"pose","args":":smiles warmly.","character_name":"Bob","location_id":"loc456"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "communication", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(HaveLen(1))
				// : variant should still include space (same as regular pose)
				Expect(emits[0].Payload).To(ContainSubstring(`Bob smiles warmly.`))
			})
		})

		Context("when pose command uses ; variant (no-space/possessive)", func() {
			It("omits space before action for possessives", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01GHI2A",
					Stream:    "character:char123",
					Type:      pluginsdk.EventType("command"),
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char123",
					Payload:   `{"name":"pose","args":";'s eyes widen.","character_name":"Bob","location_id":"loc456"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "communication", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(HaveLen(1))
				// ; variant should NOT include space (for possessives like 's)
				Expect(emits[0].Payload).To(ContainSubstring(`Bob's eyes widen.`))
			})
		})

		Context("when pose uses invoked_as field from prefix alias", func() {
			It("uses invoked_as=; for no-space variant", func() {
				ctx := context.Background()
				// Tests primary path: prefix alias sets invoked_as, args has no prefix marker
				event := pluginsdk.Event{
					ID:        "01GHI3",
					Stream:    "character:char123",
					Type:      pluginsdk.EventType("command"),
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char123",
					Payload:   `{"name":"pose","args":"'s sword gleams.","character_name":"Conan","location_id":"loc456","invoked_as":";"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "communication", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(HaveLen(1))
				// ; prefix alias means no space between name and action
				Expect(emits[0].Payload).To(ContainSubstring(`Conan's sword gleams.`))
			})

			It("uses invoked_as=: for space variant", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01GHI4",
					Stream:    "character:char123",
					Type:      pluginsdk.EventType("command"),
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char123",
					Payload:   `{"name":"pose","args":"draws their blade.","character_name":"Conan","location_id":"loc456","invoked_as":":"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "communication", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(HaveLen(1))
				// : prefix alias means space between name and action
				Expect(emits[0].Payload).To(ContainSubstring(`Conan draws their blade.`))
			})
		})

		Context("when pose command has empty action", func() {
			It("returns error message to character", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01GHI2",
					Stream:    "character:char123",
					Type:      pluginsdk.EventType("command"),
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char123",
					Payload:   `{"name":"pose","args":"","character_name":"Bob","location_id":"loc456","character_id":"char123"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "communication", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(HaveLen(1))
				Expect(emits[0].Stream).To(Equal("character:char123"))
				Expect(string(emits[0].Type)).To(Equal("error"))
				Expect(emits[0].Payload).To(ContainSubstring("What do you want to do?"))
			})
		})

		Context("when pose uses prefix marker with no action after it", func() {
			It("returns error message when only : is provided in args", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01GHI5",
					Stream:    "character:char123",
					Type:      pluginsdk.EventType("command"),
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char123",
					Payload:   `{"name":"pose","args":":","character_name":"Bob","location_id":"loc456","character_id":"char123"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "communication", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(HaveLen(1))
				Expect(emits[0].Stream).To(Equal("character:char123"))
				Expect(string(emits[0].Type)).To(Equal("error"))
				Expect(emits[0].Payload).To(ContainSubstring("What do you want to do?"))
			})

			It("returns error message when only ; is provided in args", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01GHI6",
					Stream:    "character:char123",
					Type:      pluginsdk.EventType("command"),
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char123",
					Payload:   `{"name":"pose","args":";","character_name":"Bob","location_id":"loc456","character_id":"char123"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "communication", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(HaveLen(1))
				Expect(emits[0].Stream).To(Equal("character:char123"))
				Expect(string(emits[0].Type)).To(Equal("error"))
				Expect(emits[0].Payload).To(ContainSubstring("What do you want to do?"))
			})
		})
	})

	Describe("Emit Command Event Handling", func() {
		Context("when receiving emit command event", func() {
			It("emits raw text without prefix", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01JKL",
					Stream:    "character:char123",
					Type:      pluginsdk.EventType("command"),
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char123",
					Payload:   `{"name":"emit","args":"The room shakes!","character_name":"Admin","location_id":"loc456"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "communication", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(HaveLen(1))
				Expect(emits[0].Stream).To(Equal("location:loc456"))
				Expect(string(emits[0].Type)).To(Equal("emit"))
				Expect(emits[0].Payload).To(ContainSubstring(`The room shakes!`))
			})
		})

		Context("when emit command has empty text", func() {
			It("returns error message to character", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01JKL2",
					Stream:    "character:char123",
					Type:      pluginsdk.EventType("command"),
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char123",
					Payload:   `{"name":"emit","args":"","character_name":"Admin","location_id":"loc456","character_id":"char123"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "communication", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(HaveLen(1))
				Expect(emits[0].Stream).To(Equal("character:char123"))
				Expect(string(emits[0].Type)).To(Equal("error"))
				Expect(emits[0].Payload).To(ContainSubstring("What do you want to emit?"))
			})
		})
	})

	Describe("Non-Command Event Handling", func() {
		Context("when receiving non-command events", func() {
			It("ignores them", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01MNO",
					Stream:    "location:123",
					Type:      pluginsdk.EventTypeSay,
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char_1",
					Payload:   `{"message":"Hello"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "communication", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(BeEmpty())
			})
		})
	})
})
