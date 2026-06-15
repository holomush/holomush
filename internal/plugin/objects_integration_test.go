// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugins_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// objectsFixture contains all components needed for objects plugin integration tests.
type objectsFixture struct {
	LuaHost *pluginlua.Host
	Plugin  *plugins.DiscoveredPlugin
	Cleanup func()
}

// setupObjectsTest creates all components needed to test the objects plugin.
func setupObjectsTest(mutator hostfunc.WorldMutator) (*objectsFixture, error) {
	pluginsDir, err := findPluginsDir()
	if err != nil {
		return nil, err
	}
	objectsDir := filepath.Join(pluginsDir, "core-objects")

	if _, statErr := os.Stat(objectsDir); os.IsNotExist(statErr) {
		return nil, statErr
	}

	opts := []hostfunc.Option{
		// create object is a scoped (own-location) capability write, so the host
		// runs the ABAC engine for the scope fence; production wires the access
		// engine here. AllowAllEngine lets the scope evaluation proceed so the
		// brokered CreateObject reaches the mutator. Without an engine the scoped
		// call fails closed.
		hostfunc.WithEngine(policytest.AllowAllEngine()),
	}
	if mutator != nil {
		opts = append(opts, hostfunc.WithWorldService(mutator))
	}
	hostFuncs := hostfunc.New(nil, opts...)
	// Populate DispatchContext.Attributes["location"] for the scoped create object
	// fence — the production AttributeResolver equivalent.
	luaHost := pluginlua.NewHostWithFunctions(hostFuncs,
		pluginlua.WithDispatchAttributeResolver(fixedBuildingAttrResolver{location: buildingTestLocationID}))

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

	var objectsPlugin *plugins.DiscoveredPlugin
	for _, dp := range discovered {
		if dp.Manifest.Name == "core-objects" {
			objectsPlugin = dp
			break
		}
	}

	if objectsPlugin == nil {
		_ = luaHost.Close(ctx)
		return nil, os.ErrNotExist
	}

	if err := luaHost.Load(ctx, objectsPlugin.Manifest, objectsPlugin.Dir); err != nil {
		_ = luaHost.Close(ctx)
		return nil, err
	}

	return &objectsFixture{
		LuaHost: luaHost,
		Plugin:  objectsPlugin,
		Cleanup: func() {
			_ = luaHost.Close(context.Background())
		},
	}, nil
}

var _ = Describe("Objects Plugin Integration", func() {
	Describe("Plugin Discovery and Loading", func() {
		var fixture *objectsFixture

		BeforeEach(func() {
			var err error
			fixture, err = setupObjectsTest(nil)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			fixture.Cleanup()
		})

		It("has correct manifest type", func() {
			Expect(fixture.Plugin.Manifest.Type).To(Equal(plugins.TypeLua))
		})

		It("has correct version", func() {
			Expect(fixture.Plugin.Manifest.Version).To(Equal("0.1.0"))
		})

		It("declares describe command with write object capability", func() {
			var descCmd *plugins.CommandSpec
			for i := range fixture.Plugin.Manifest.Commands {
				if fixture.Plugin.Manifest.Commands[i].Name == "describe" {
					descCmd = &fixture.Plugin.Manifest.Commands[i]
					break
				}
			}
			Expect(descCmd).NotTo(BeNil())
			Expect(descCmd.Capabilities).To(ContainElement(command.Capability{Action: "write", Resource: "object", Scope: command.ScopeLocal}))
			Expect(descCmd.Help).To(Equal("Set a description"))
		})

		It("declares examine command with read object capability", func() {
			var examCmd *plugins.CommandSpec
			for i := range fixture.Plugin.Manifest.Commands {
				if fixture.Plugin.Manifest.Commands[i].Name == "examine" {
					examCmd = &fixture.Plugin.Manifest.Commands[i]
					break
				}
			}
			Expect(examCmd).NotTo(BeNil())
			Expect(examCmd.Capabilities).To(ContainElement(command.Capability{Action: "read", Resource: "object", Scope: command.ScopeLocal}))
		})

		It("declares create command", func() {
			var createCmd *plugins.CommandSpec
			for i := range fixture.Plugin.Manifest.Commands {
				if fixture.Plugin.Manifest.Commands[i].Name == "create" {
					createCmd = &fixture.Plugin.Manifest.Commands[i]
					break
				}
			}
			Expect(createCmd).NotTo(BeNil())
			Expect(createCmd.Capabilities).To(ContainElement(command.Capability{Action: "write", Resource: "object", Scope: command.ScopeLocal}))
		})

		It("declares set command with write property capability", func() {
			var setCmd *plugins.CommandSpec
			for i := range fixture.Plugin.Manifest.Commands {
				if fixture.Plugin.Manifest.Commands[i].Name == "set" {
					setCmd = &fixture.Plugin.Manifest.Commands[i]
					break
				}
			}
			Expect(setCmd).NotTo(BeNil())
			Expect(setCmd.Capabilities).To(ContainElement(command.Capability{Action: "write", Resource: "property", Scope: command.ScopeLocal}))
		})
	})

	Describe("Describe Command", func() {
		Context("when called without arguments", func() {
			It("returns usage message", func() {
				fixture, err := setupObjectsTest(nil)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-objects", pluginsdk.CommandRequest{
					Command:       "describe",
					Args:          "",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("Usage:"))
				Expect(resp.Output).To(ContainSubstring("describe"))
			})
		})

		Context("when describe me has empty text", func() {
			It("returns usage message", func() {
				fixture, err := setupObjectsTest(nil)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-objects", pluginsdk.CommandRequest{
					Command:       "describe",
					Args:          "me ",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("Usage:"))
			})
		})
	})

	Describe("Create Command", func() {
		Context("when called without arguments", func() {
			It("returns usage message", func() {
				fixture, err := setupObjectsTest(nil)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-objects", pluginsdk.CommandRequest{
					Command:       "create",
					Args:          "",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("Usage:"))
				Expect(resp.Output).To(ContainSubstring("create"))
			})
		})

		Context("when called with invalid type", func() {
			It("returns usage with valid types", func() {
				fixture, err := setupObjectsTest(nil)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-objects", pluginsdk.CommandRequest{
					Command:       "create",
					Args:          `widget "Test Widget"`,
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("valid types"))
			})
		})

		Context("when creating an object with world service", func() {
			It("creates object successfully", func() {
				mutator := &mockWorldMutator{}
				fixture, err := setupObjectsTest(mutator)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				// create object is a scoped (own-location) write; stamp the acting
				// character so stampDispatch vouches the dispatch context.
				ctx = scopedActorContext(ctx, "01HTEST000000000000000CHAR")

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-objects", pluginsdk.CommandRequest{
					Command:       "create",
					Args:          `object "Enchanted Sword"`,
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    buildingTestLocationID,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Output).To(ContainSubstring("Enchanted Sword"))
				Expect(resp.Output).To(ContainSubstring("Created object"))
			})
		})

		Context("when creating a location with world service", func() {
			It("creates location successfully", func() {
				mutator := &mockWorldMutator{}
				fixture, err := setupObjectsTest(mutator)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-objects", pluginsdk.CommandRequest{
					Command:       "create",
					Args:          `location "The Library"`,
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Output).To(ContainSubstring("The Library"))
				Expect(resp.Output).To(ContainSubstring("Created location"))
			})
		})
	})

	Describe("Examine Command", func() {
		Context("when examining a target not in location", func() {
			It("returns not found message", func() {
				fixture, err := setupObjectsTest(nil)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-objects", pluginsdk.CommandRequest{
					Command:       "examine",
					Args:          "nonexistent",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
				})
				Expect(err).NotTo(HaveOccurred())
				// Without world service, examine falls back to querying base functions
				// which return nil/error, resulting in a not-found or error response
				Expect(resp.Status).NotTo(Equal(pluginsdk.CommandOK))
			})
		})
	})

	Describe("Set Command", func() {
		Context("when called without arguments", func() {
			It("returns usage message", func() {
				fixture, err := setupObjectsTest(nil)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-objects", pluginsdk.CommandRequest{
					Command:       "set",
					Args:          "",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("Usage:"))
				Expect(resp.Output).To(ContainSubstring("set"))
			})
		})
	})

	Describe("Unknown Command", func() {
		It("returns error for unknown objects command", func() {
			fixture, err := setupObjectsTest(nil)
			Expect(err).NotTo(HaveOccurred())
			defer fixture.Cleanup()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-objects", pluginsdk.CommandRequest{
				Command:       "destroy",
				Args:          "",
				CharacterID:   "01HTEST000000000000000CHAR",
				CharacterName: "TestPlayer",
				LocationID:    "01HTEST000000000000000ROOM",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Status).To(Equal(pluginsdk.CommandError))
			Expect(resp.Output).To(ContainSubstring("Unknown command"))
		})
	})
})
