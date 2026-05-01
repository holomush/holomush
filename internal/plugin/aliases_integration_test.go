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

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// mockAliasAccessInteg implements hostfunc.AliasAccess for alias integration tests.
type mockAliasAccessInteg struct {
	playerAliases []hostfunc.AliasEntry
	systemAliases []hostfunc.AliasEntry
}

func (m *mockAliasAccessInteg) SetPlayerAlias(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *mockAliasAccessInteg) DeletePlayerAlias(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockAliasAccessInteg) ListPlayerAliases(_ context.Context, _ string) ([]hostfunc.AliasEntry, error) {
	return m.playerAliases, nil
}

func (m *mockAliasAccessInteg) CheckAliasShadow(_ context.Context, _ string) (bool, string, error) {
	return false, "", nil
}

func (m *mockAliasAccessInteg) SetSystemAlias(_ context.Context, _, _, _, _ string) error {
	return nil
}

func (m *mockAliasAccessInteg) DeleteSystemAlias(_ context.Context, _ string) error {
	return nil
}

func (m *mockAliasAccessInteg) ListSystemAliases(_ context.Context) ([]hostfunc.AliasEntry, error) {
	return m.systemAliases, nil
}

// Compile-time interface check.
var _ hostfunc.AliasAccess = (*mockAliasAccessInteg)(nil)

// aliasesFixture contains all components needed for aliases plugin integration tests.
type aliasesFixture struct {
	LuaHost *pluginlua.Host
	Plugin  *plugins.DiscoveredPlugin
	Cleanup func()
}

// setupAliasesTest creates all components needed to test the aliases plugin.
// If aliasAccess is nil, no alias capability is wired (tests the "not available" path).
func setupAliasesTest(aliasAccess hostfunc.AliasAccess) (*aliasesFixture, error) {
	pluginsDir, err := findPluginsDir()
	if err != nil {
		return nil, err
	}
	aliasesDir := filepath.Join(pluginsDir, "core-aliases")

	if _, statErr := os.Stat(aliasesDir); os.IsNotExist(statErr) {
		return nil, statErr
	}

	var opts []hostfunc.Option
	if aliasAccess != nil {
		capReg := hostfunc.NewCapabilityRegistry()
		capReg.Register("holomush.alias.v1.AliasService", hostfunc.NewAliasCapability(aliasAccess))
		opts = append(opts, hostfunc.WithCapabilities(capReg))
	}
	hostFuncs := hostfunc.New(nil, opts...)
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

	var aliasesPlugin *plugins.DiscoveredPlugin
	for _, dp := range discovered {
		if dp.Manifest.Name == "core-aliases" {
			aliasesPlugin = dp
			break
		}
	}

	if aliasesPlugin == nil {
		_ = luaHost.Close(ctx)
		return nil, os.ErrNotExist
	}

	if err := luaHost.Load(ctx, aliasesPlugin.Manifest, aliasesPlugin.Dir); err != nil {
		_ = luaHost.Close(ctx)
		return nil, err
	}

	return &aliasesFixture{
		LuaHost: luaHost,
		Plugin:  aliasesPlugin,
		Cleanup: func() {
			_ = luaHost.Close(context.Background())
		},
	}, nil
}

var _ = Describe("Aliases Plugin Integration", func() {
	Describe("Plugin Discovery and Loading", func() {
		var fixture *aliasesFixture

		BeforeEach(func() {
			var err error
			fixture, err = setupAliasesTest(nil)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			fixture.Cleanup()
		})

		It("has correct manifest type", func() {
			Expect(fixture.Plugin.Manifest.Type).To(Equal(plugins.TypeLua))
		})

		It("has correct version", func() {
			Expect(fixture.Plugin.Manifest.Version).To(Equal("1.0.0"))
		})

		It("declares alias command with write alias capability", func() {
			var aliasCmd *plugins.CommandSpec
			for i := range fixture.Plugin.Manifest.Commands {
				if fixture.Plugin.Manifest.Commands[i].Name == "alias" {
					aliasCmd = &fixture.Plugin.Manifest.Commands[i]
					break
				}
			}
			Expect(aliasCmd).NotTo(BeNil())
			Expect(aliasCmd.Capabilities).To(ContainElement(command.Capability{Action: "write", Resource: "alias"}))
		})

		It("declares sysalias command with write alias global scope", func() {
			var sysCmd *plugins.CommandSpec
			for i := range fixture.Plugin.Manifest.Commands {
				if fixture.Plugin.Manifest.Commands[i].Name == "sysalias" {
					sysCmd = &fixture.Plugin.Manifest.Commands[i]
					break
				}
			}
			Expect(sysCmd).NotTo(BeNil())
			Expect(sysCmd.Capabilities).To(ContainElement(command.Capability{Action: "write", Resource: "alias", Scope: command.ScopeGlobal}))
		})
	})

	Describe("Without Alias Service", func() {
		Context("when alias capability is not wired", func() {
			It("returns service unavailable error for alias command", func() {
				fixture, err := setupAliasesTest(nil)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-aliases", pluginsdk.CommandRequest{
					Command:       "alias",
					Args:          "l=look",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
					PlayerID:      "01HTEST00000000000000PLYR",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("not yet available"))
			})

			It("returns service unavailable error for aliases command", func() {
				fixture, err := setupAliasesTest(nil)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-aliases", pluginsdk.CommandRequest{
					Command:       "aliases",
					Args:          "",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
					PlayerID:      "01HTEST00000000000000PLYR",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("not yet available"))
			})

			It("returns service unavailable error for sysaliases command", func() {
				fixture, err := setupAliasesTest(nil)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-aliases", pluginsdk.CommandRequest{
					Command:       "sysaliases",
					Args:          "",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
					PlayerID:      "01HTEST00000000000000PLYR",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("not yet available"))
			})
		})
	})

	Describe("With Alias Service", func() {
		Context("alias command", func() {
			It("creates a player alias successfully", func() {
				mock := &mockAliasAccessInteg{}
				fixture, err := setupAliasesTest(mock)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-aliases", pluginsdk.CommandRequest{
					Command:       "alias",
					Args:          "l=look",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
					PlayerID:      "01HTEST00000000000000PLYR",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Output).To(ContainSubstring("Alias 'l' added"))
				Expect(resp.Output).To(ContainSubstring("look"))
			})

			It("returns error for empty alias definition", func() {
				mock := &mockAliasAccessInteg{}
				fixture, err := setupAliasesTest(mock)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-aliases", pluginsdk.CommandRequest{
					Command:       "alias",
					Args:          "",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
					PlayerID:      "01HTEST00000000000000PLYR",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("usage"))
			})

			It("returns error for invalid alias name", func() {
				mock := &mockAliasAccessInteg{}
				fixture, err := setupAliasesTest(mock)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-aliases", pluginsdk.CommandRequest{
					Command:       "alias",
					Args:          "bad name=look",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
					PlayerID:      "01HTEST00000000000000PLYR",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("invalid alias name"))
			})
		})

		Context("aliases command", func() {
			It("lists player aliases when none defined", func() {
				mock := &mockAliasAccessInteg{}
				fixture, err := setupAliasesTest(mock)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-aliases", pluginsdk.CommandRequest{
					Command:       "aliases",
					Args:          "",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
					PlayerID:      "01HTEST00000000000000PLYR",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Output).To(ContainSubstring("no aliases"))
			})

			It("lists player aliases when some exist", func() {
				mock := &mockAliasAccessInteg{
					playerAliases: []hostfunc.AliasEntry{
						{Alias: "l", Command: "look"},
						{Alias: "n", Command: "north"},
					},
				}
				fixture, err := setupAliasesTest(mock)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-aliases", pluginsdk.CommandRequest{
					Command:       "aliases",
					Args:          "",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
					PlayerID:      "01HTEST00000000000000PLYR",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Output).To(ContainSubstring("l = look"))
				Expect(resp.Output).To(ContainSubstring("n = north"))
			})
		})

		Context("unalias command", func() {
			It("returns error when alias does not exist", func() {
				mock := &mockAliasAccessInteg{}
				fixture, err := setupAliasesTest(mock)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-aliases", pluginsdk.CommandRequest{
					Command:       "unalias",
					Args:          "nonexistent",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
					PlayerID:      "01HTEST00000000000000PLYR",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("No alias"))
			})

			It("returns usage when called without arguments", func() {
				mock := &mockAliasAccessInteg{}
				fixture, err := setupAliasesTest(mock)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-aliases", pluginsdk.CommandRequest{
					Command:       "unalias",
					Args:          "",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
					PlayerID:      "01HTEST00000000000000PLYR",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("Usage"))
			})
		})

		Context("sysaliases command", func() {
			It("lists system aliases when none defined", func() {
				mock := &mockAliasAccessInteg{}
				fixture, err := setupAliasesTest(mock)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-aliases", pluginsdk.CommandRequest{
					Command:       "sysaliases",
					Args:          "",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
					PlayerID:      "01HTEST00000000000000PLYR",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Output).To(ContainSubstring("No system aliases"))
			})
		})

		Context("sysalias command", func() {
			It("creates a system alias successfully", func() {
				mock := &mockAliasAccessInteg{}
				fixture, err := setupAliasesTest(mock)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-aliases", pluginsdk.CommandRequest{
					Command:       "sysalias",
					Args:          "l=look",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "TestPlayer",
					LocationID:    "01HTEST000000000000000ROOM",
					PlayerID:      "01HTEST00000000000000PLYR",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Output).To(ContainSubstring("System alias 'l' added"))
			})
		})
	})
})
