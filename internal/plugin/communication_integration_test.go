// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugins_test

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corecomm "github.com/holomush/holomush/plugins/core-communication"
)

// communicationFixture contains all components needed for communication plugin integration tests.
type communicationFixture struct {
	LuaHost *pluginlua.Host
	Plugin  *plugins.DiscoveredPlugin
	Cleanup func()
}

// setupCommunicationTest creates all components needed to test the communication plugins.
func setupCommunicationTest() (*communicationFixture, error) {
	pluginsDir, err := findPluginsDir()
	if err != nil {
		return nil, err
	}
	commDir := filepath.Join(pluginsDir, "core-communication")

	if _, statErr := os.Stat(commDir); os.IsNotExist(statErr) {
		return nil, statErr
	}

	hostFuncs := hostfunc.New(nil)
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

	var commPlugin *plugins.DiscoveredPlugin
	for _, dp := range discovered {
		if dp.Manifest.Name == "core-communication" {
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

	return &communicationFixture{
		LuaHost: luaHost,
		Plugin:  commPlugin,
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

		It("handles commands via on_command (no event subscriptions)", func() {
			// core-communication uses on_command for dispatch; it does not
			// subscribe to any event stream (events: field is absent in manifest).
			Expect(fixture.Plugin.Manifest.Events).To(BeEmpty())
		})

		It("declares execute-communication and execute-pemit policies", func() {
			// The manifest ships two ABAC policies.
			policyNames := make([]string, len(fixture.Plugin.Manifest.Policies))
			for i, p := range fixture.Plugin.Manifest.Policies {
				policyNames[i] = p.Name
			}
			Expect(policyNames).To(ContainElements("execute-communication", "execute-pemit"))
		})

		It("declares say command with emit capability", func() {
			var sayCmd *plugins.CommandSpec
			for i := range fixture.Plugin.Manifest.Commands {
				if fixture.Plugin.Manifest.Commands[i].Name == "say" {
					sayCmd = &fixture.Plugin.Manifest.Commands[i]
					break
				}
			}
			Expect(sayCmd).NotTo(BeNil())
			Expect(sayCmd.Capabilities).To(ContainElement(command.Capability{Action: "emit", Resource: "stream", Scope: command.ScopeLocal}))
			// Help text matches plugin.yaml as of current manifest.
			Expect(sayCmd.Help).To(Equal("Say something to the location"))
			Expect(sayCmd.Usage).To(Equal("say <message>"))
		})

		It("declares pose command with emit capability", func() {
			var poseCmd *plugins.CommandSpec
			for i := range fixture.Plugin.Manifest.Commands {
				if fixture.Plugin.Manifest.Commands[i].Name == "pose" {
					poseCmd = &fixture.Plugin.Manifest.Commands[i]
					break
				}
			}
			Expect(poseCmd).NotTo(BeNil())
			Expect(poseCmd.Capabilities).To(ContainElement(command.Capability{Action: "emit", Resource: "stream", Scope: command.ScopeLocal}))
			// Help text matches plugin.yaml as of current manifest.
			Expect(poseCmd.Help).To(Equal("Perform an action"))
		})

		It("declares emit command with emit capability", func() {
			var emitCmd *plugins.CommandSpec
			for i := range fixture.Plugin.Manifest.Commands {
				if fixture.Plugin.Manifest.Commands[i].Name == "emit" {
					emitCmd = &fixture.Plugin.Manifest.Commands[i]
					break
				}
			}
			Expect(emitCmd).NotTo(BeNil())
			Expect(emitCmd.Capabilities).To(ContainElement(command.Capability{Action: "emit", Resource: "stream", Scope: command.ScopeLocal}))
			// Help text matches plugin.yaml as of current manifest.
			Expect(emitCmd.Help).To(Equal("Emit a message to the location"))
		})
	})

	// All command tests use DeliverCommand, the sole path that invokes
	// on_command: it parses the {status, output, events} wrapper table the
	// handler returns via parseCommandResponse.

	Describe("Say Command Handling", func() {
		Context("when say command is invoked with a message", func() {
			It("emits say event to location stream", func() {
				ctx := context.Background()
				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-communication", pluginsdk.CommandRequest{
					Command:       "say",
					Args:          "Hello everyone!",
					CharacterName: "Alice",
					LocationID:    "loc456",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Events).To(HaveLen(1))
				Expect(resp.Events[0].Stream).To(Equal("location.loc456"))
				Expect(resp.Events[0].Type).To(Equal(pluginsdk.EventType(corecomm.EventTypeSay)))
				Expect(resp.Events[0].Payload).To(ContainSubstring(`"actor_display_name":"Alice"`))
				Expect(resp.Events[0].Payload).To(ContainSubstring(`Hello everyone!`))
			})
		})

		Context("when say command has empty message", func() {
			It("returns error status with prompt", func() {
				ctx := context.Background()
				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-communication", pluginsdk.CommandRequest{
					Command:       "say",
					Args:          "",
					CharacterName: "Alice",
					LocationID:    "loc456",
					CharacterID:   "char123",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("What do you want to say?"))
			})
		})
	})

	Describe("Pose Command Handling", func() {
		Context("when pose command is invoked with an action", func() {
			It("emits pose event with structured payload", func() {
				ctx := context.Background()
				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-communication", pluginsdk.CommandRequest{
					Command:       "pose",
					Args:          "waves hello.",
					CharacterName: "Bob",
					LocationID:    "loc456",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Events).To(HaveLen(1))
				Expect(resp.Events[0].Stream).To(Equal("location.loc456"))
				Expect(resp.Events[0].Type).To(Equal(pluginsdk.EventType(corecomm.EventTypePose)))
				// Payload is structured JSON: {actor_display_name, text}; rendering happens at display layer
				Expect(resp.Events[0].Payload).To(ContainSubstring(`"actor_display_name":"Bob"`))
				Expect(resp.Events[0].Payload).To(ContainSubstring(`"text":"waves hello."`))
			})
		})

		Context("when pose command uses : variant in args", func() {
			It("strips : prefix and emits action without no_space flag", func() {
				ctx := context.Background()
				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-communication", pluginsdk.CommandRequest{
					Command:       "pose",
					Args:          ":smiles warmly.",
					CharacterName: "Bob",
					LocationID:    "loc456",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Events).To(HaveLen(1))
				// : prefix stripped; text is "smiles warmly." with no no_space flag
				Expect(resp.Events[0].Payload).To(ContainSubstring(`"text":"smiles warmly."`))
				Expect(resp.Events[0].Payload).NotTo(ContainSubstring(`"no_space"`))
			})
		})

		Context("when pose command uses ; variant in args (no-space/possessive)", func() {
			It("strips ; prefix and sets no_space flag in payload", func() {
				ctx := context.Background()
				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-communication", pluginsdk.CommandRequest{
					Command:       "pose",
					Args:          ";'s eyes widen.",
					CharacterName: "Bob",
					LocationID:    "loc456",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Events).To(HaveLen(1))
				// ; prefix stripped; text includes the possessive and no_space=true is set
				Expect(resp.Events[0].Payload).To(ContainSubstring(`"text":"'s eyes widen."`))
				Expect(resp.Events[0].Payload).To(ContainSubstring(`"no_space":true`))
			})
		})

		Context("when pose uses invoked_as field from prefix alias", func() {
			It("uses invoked_as=; for no-space variant (sets no_space flag)", func() {
				ctx := context.Background()
				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-communication", pluginsdk.CommandRequest{
					Command:       "pose",
					Args:          "'s sword gleams.",
					CharacterName: "Conan",
					LocationID:    "loc456",
					InvokedAs:     ";",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Events).To(HaveLen(1))
				// invoked_as=; sets no_space=true in payload
				Expect(resp.Events[0].Payload).To(ContainSubstring(`"text":"'s sword gleams."`))
				Expect(resp.Events[0].Payload).To(ContainSubstring(`"no_space":true`))
			})

			It("uses invoked_as=: for space variant (no no_space flag)", func() {
				ctx := context.Background()
				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-communication", pluginsdk.CommandRequest{
					Command:       "pose",
					Args:          "draws their blade.",
					CharacterName: "Conan",
					LocationID:    "loc456",
					InvokedAs:     ":",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Events).To(HaveLen(1))
				// invoked_as=: does not set no_space flag
				Expect(resp.Events[0].Payload).To(ContainSubstring(`"text":"draws their blade."`))
				Expect(resp.Events[0].Payload).NotTo(ContainSubstring(`"no_space"`))
			})
		})

		Context("when pose command has empty action", func() {
			It("returns error status with pose-specific prompt", func() {
				ctx := context.Background()
				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-communication", pluginsdk.CommandRequest{
					Command:       "pose",
					Args:          "",
					CharacterName: "Bob",
					LocationID:    "loc456",
					CharacterID:   "char123",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("What do you want to pose?"))
			})
		})

		Context("when pose uses prefix marker with no action after it", func() {
			It("returns error when only : is provided in args", func() {
				ctx := context.Background()
				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-communication", pluginsdk.CommandRequest{
					Command:       "pose",
					Args:          ":",
					CharacterName: "Bob",
					LocationID:    "loc456",
					CharacterID:   "char123",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("What do you want to pose?"))
			})

			It("returns error when only ; is provided in args", func() {
				ctx := context.Background()
				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-communication", pluginsdk.CommandRequest{
					Command:       "pose",
					Args:          ";",
					CharacterName: "Bob",
					LocationID:    "loc456",
					CharacterID:   "char123",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("What do you want to pose?"))
			})
		})
	})

	Describe("Emit Command Handling", func() {
		Context("when emit command is invoked with text", func() {
			It("emits raw text to the location stream", func() {
				ctx := context.Background()
				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-communication", pluginsdk.CommandRequest{
					Command:       "emit",
					Args:          "The room shakes!",
					CharacterName: "Admin",
					LocationID:    "loc456",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Events).To(HaveLen(1))
				Expect(resp.Events[0].Stream).To(Equal("location.loc456"))
				// The Lua emit handler now returns the plugin-qualified wire type
				// core-communication:emit directly (holomush-aneim), matching its
				// verbs[].type so RenderingPublisher.Lookup resolves it instead of
				// hard-failing EMIT_UNKNOWN_VERB.
				Expect(string(resp.Events[0].Type)).To(Equal("core-communication:emit"))
				Expect(resp.Events[0].Payload).To(ContainSubstring(`The room shakes!`))
			})
		})

		Context("when emit command has empty text", func() {
			It("returns error status with prompt", func() {
				ctx := context.Background()
				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-communication", pluginsdk.CommandRequest{
					Command:       "emit",
					Args:          "",
					CharacterName: "Admin",
					LocationID:    "loc456",
					CharacterID:   "char123",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("What do you want to emit?"))
			})
		})
	})

	Describe("Non-Command Event Handling", func() {
		Context("when receiving non-command events", func() {
			It("ignores them (no on_event handler)", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01MNO",
					Stream:    "location.123",
					Type:      pluginsdk.EventType(corecomm.EventTypeSay),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char_1",
					Payload:   `{"message":"Hello"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "core-communication", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(BeEmpty())
			})
		})
	})
})
