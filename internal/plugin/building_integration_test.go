// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugins_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	"github.com/holomush/holomush/internal/world"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// buildingTestLocationID is the acting location these specs scope their scoped
// (own-location) capability writes against. The dispatch attribute resolver
// returns it so DispatchContext.Attributes["location"] is populated and the
// scope fence resolves; the scoped dig/link exit writes carry it as from_id.
const buildingTestLocationID = "01HTEST000000000000000ROOM"

// scopedActorContext stamps a character actor on ctx so the Lua host's
// stampDispatch vouches the per-call dispatch context (subject + resolved
// location attribute). Scoped capability writes (dig/link exit creation) fail
// closed with SCOPE_NO_DISPATCH without it — this mirrors production, where the
// dispatch layer stamps the acting character before delivering the command.
func scopedActorContext(ctx context.Context, characterID string) context.Context {
	return core.WithActor(ctx, core.Actor{Kind: core.ActorCharacter, ID: characterID})
}

// fixedBuildingAttrResolver is a pluginauthz.AttributeResolver test double that
// returns a fixed dispatch-attribute bag (notably "location") for any subject,
// so stampDispatch populates DispatchContext.Attributes for scoped capability
// fences during building/objects integration tests.
type fixedBuildingAttrResolver struct {
	location string
}

func (r fixedBuildingAttrResolver) ResolveSubject(context.Context, string) (map[string]any, error) {
	return map[string]any{"location": r.location, "has_location": true}, nil
}

// mockWorldMutator implements hostfunc.WorldMutator for building integration tests.
type mockWorldMutator struct {
	createLocationErr error
	createExitErr     error
	findLocationRet   *world.Location
	findLocationErr   error
	getLocationRet    *world.Location
	getLocationErr    error
}

func (m *mockWorldMutator) GetLocation(_ context.Context, _ string, _ ulid.ULID) (*world.Location, error) {
	return m.getLocationRet, m.getLocationErr
}

func (m *mockWorldMutator) GetCharacter(_ context.Context, _ string, _ ulid.ULID) (*world.Character, error) {
	return nil, world.ErrNotFound
}

func (m *mockWorldMutator) GetCharactersByLocation(_ context.Context, _ string, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	return nil, nil
}

func (m *mockWorldMutator) GetObject(_ context.Context, _ string, _ ulid.ULID) (*world.Object, error) {
	return nil, world.ErrNotFound
}

func (m *mockWorldMutator) CreateLocation(_ context.Context, _ string, _ *world.Location) error {
	return m.createLocationErr
}

func (m *mockWorldMutator) CreateExit(_ context.Context, _ string, _ *world.Exit) error {
	return m.createExitErr
}

func (m *mockWorldMutator) CreateObject(_ context.Context, _ string, _ *world.Object) error {
	return nil
}

func (m *mockWorldMutator) UpdateLocation(_ context.Context, _ string, _ *world.Location) error {
	return nil
}

func (m *mockWorldMutator) UpdateObject(_ context.Context, _ string, _ *world.Object) error {
	return nil
}

func (m *mockWorldMutator) FindLocationByName(_ context.Context, _, _ string) (*world.Location, error) {
	if m.findLocationRet != nil {
		return m.findLocationRet, nil
	}
	if m.findLocationErr != nil {
		return nil, m.findLocationErr
	}
	return nil, world.ErrNotFound
}

// Compile-time interface check.
var _ hostfunc.WorldMutator = (*mockWorldMutator)(nil)

// buildingFixture contains all components needed for building plugin integration tests.
type buildingFixture struct {
	LuaHost *pluginlua.Host
	Plugin  *plugins.DiscoveredPlugin
	Cleanup func()
}

// setupBuildingTest creates all components needed to test the building plugin.
func setupBuildingTest(mutator hostfunc.WorldMutator) (*buildingFixture, error) {
	pluginsDir, err := findPluginsDir()
	if err != nil {
		return nil, err
	}
	buildingDir := filepath.Join(pluginsDir, "core-building")

	if _, statErr := os.Stat(buildingDir); os.IsNotExist(statErr) {
		return nil, statErr
	}

	opts := []hostfunc.Option{
		// Scoped capability writes (dig/link exit creation) run the ABAC engine to
		// evaluate the own-location scope fence; production wires the access engine
		// here (interceptor sources it via adapter.AccessEngine() → Functions.Engine()).
		// AllowAllEngine lets the scope evaluation proceed so the create RPCs reach
		// the mutator. Without an engine the scope check fails closed EVALUATE_NO_ENGINE.
		hostfunc.WithEngine(policytest.AllowAllEngine()),
	}
	if mutator != nil {
		opts = append(opts, hostfunc.WithWorldService(mutator))
	}
	hostFuncs := hostfunc.New(nil, opts...)
	// The dispatch attribute resolver populates DispatchContext.Attributes["location"]
	// so scoped capability calls have an acting-character location to fence against —
	// the production AttributeResolver equivalent.
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

	var buildingPlugin *plugins.DiscoveredPlugin
	for _, dp := range discovered {
		if dp.Manifest.Name == "core-building" {
			buildingPlugin = dp
			break
		}
	}

	if buildingPlugin == nil {
		_ = luaHost.Close(ctx)
		return nil, os.ErrNotExist
	}

	if err := luaHost.Load(ctx, buildingPlugin.Manifest, buildingPlugin.Dir); err != nil {
		_ = luaHost.Close(ctx)
		return nil, err
	}

	return &buildingFixture{
		LuaHost: luaHost,
		Plugin:  buildingPlugin,
		Cleanup: func() {
			_ = luaHost.Close(context.Background())
		},
	}, nil
}

var _ = Describe("Building Plugin Integration", func() {
	Describe("Plugin Discovery and Loading", func() {
		var fixture *buildingFixture

		BeforeEach(func() {
			var err error
			fixture, err = setupBuildingTest(nil)
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

		It("uses on_command handler for command dispatch", func() {
			// Building plugin uses DeliverCommand, not event subscriptions
			Expect(fixture.Plugin.Manifest.Commands).NotTo(BeEmpty())
		})

		It("declares dig command with write capabilities", func() {
			var digCmd *plugins.CommandSpec
			for i := range fixture.Plugin.Manifest.Commands {
				if fixture.Plugin.Manifest.Commands[i].Name == "dig" {
					digCmd = &fixture.Plugin.Manifest.Commands[i]
					break
				}
			}
			Expect(digCmd).NotTo(BeNil())
			Expect(digCmd.Capabilities).To(ContainElement(command.Capability{Action: "write", Resource: "location", Scope: command.ScopeLocal}))
			Expect(digCmd.Capabilities).To(ContainElement(command.Capability{Action: "write", Resource: "exit", Scope: command.ScopeLocal}))
		})

		It("declares link command with write exit capability", func() {
			var linkCmd *plugins.CommandSpec
			for i := range fixture.Plugin.Manifest.Commands {
				if fixture.Plugin.Manifest.Commands[i].Name == "link" {
					linkCmd = &fixture.Plugin.Manifest.Commands[i]
					break
				}
			}
			Expect(linkCmd).NotTo(BeNil())
			Expect(linkCmd.Capabilities).To(ContainElement(command.Capability{Action: "write", Resource: "exit", Scope: command.ScopeLocal}))
		})
	})

	Describe("Dig Command", func() {
		Context("when called without arguments", func() {
			It("returns usage message", func() {
				fixture, err := setupBuildingTest(nil)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-building", pluginsdk.CommandRequest{
					Command:       "dig",
					Args:          "",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "Builder",
					LocationID:    "01HTEST000000000000000ROOM",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("Usage:"))
				Expect(resp.Output).To(ContainSubstring("dig"))
			})
		})

		Context("when called with malformed arguments", func() {
			It("returns error status for missing quotes", func() {
				fixture, err := setupBuildingTest(nil)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-building", pluginsdk.CommandRequest{
					Command:       "dig",
					Args:          "north to Town Square",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "Builder",
					LocationID:    "01HTEST000000000000000ROOM",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
			})
		})

		Context("when called with valid arguments and world service", func() {
			It("creates location and exit successfully", func() {
				mutator := &mockWorldMutator{}
				fixture, err := setupBuildingTest(mutator)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				ctx = scopedActorContext(ctx, "01HTEST000000000000000CHAR")

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-building", pluginsdk.CommandRequest{
					Command:       "dig",
					Args:          `north to "Town Square"`,
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "Builder",
					LocationID:    buildingTestLocationID,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Output).To(ContainSubstring("Town Square"))
				Expect(resp.Output).To(ContainSubstring("north"))
			})

			It("creates bidirectional exits with return keyword", func() {
				mutator := &mockWorldMutator{}
				fixture, err := setupBuildingTest(mutator)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				ctx = scopedActorContext(ctx, "01HTEST000000000000000CHAR")

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-building", pluginsdk.CommandRequest{
					Command:       "dig",
					Args:          `north to "Market" return south`,
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "Builder",
					LocationID:    buildingTestLocationID,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Output).To(ContainSubstring("Market"))
				Expect(resp.Output).To(ContainSubstring("north"))
				Expect(resp.Output).To(ContainSubstring("south"))
			})
		})
	})

	Describe("Link Command", func() {
		Context("when called without arguments", func() {
			It("returns usage message", func() {
				fixture, err := setupBuildingTest(nil)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-building", pluginsdk.CommandRequest{
					Command:       "link",
					Args:          "",
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "Builder",
					LocationID:    "01HTEST000000000000000ROOM",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandError))
				Expect(resp.Output).To(ContainSubstring("Usage:"))
				Expect(resp.Output).To(ContainSubstring("link"))
			})
		})

		Context("when called with valid arguments and world service", func() {
			It("creates exit to existing location by name", func() {
				targetLoc := &world.Location{
					ID:   ulid.Make(),
					Name: "Garden",
				}
				mutator := &mockWorldMutator{
					findLocationRet: targetLoc,
				}
				fixture, err := setupBuildingTest(mutator)
				Expect(err).NotTo(HaveOccurred())
				defer fixture.Cleanup()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				ctx = scopedActorContext(ctx, "01HTEST000000000000000CHAR")

				resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-building", pluginsdk.CommandRequest{
					Command:       "link",
					Args:          `east to Garden`,
					CharacterID:   "01HTEST000000000000000CHAR",
					CharacterName: "Builder",
					LocationID:    buildingTestLocationID,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
				Expect(resp.Output).To(ContainSubstring("east"))
				Expect(resp.Output).To(ContainSubstring("Garden"))
			})
		})
	})

	Describe("Unknown Command", func() {
		It("returns error for unknown building command", func() {
			fixture, err := setupBuildingTest(nil)
			Expect(err).NotTo(HaveOccurred())
			defer fixture.Cleanup()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := fixture.LuaHost.DeliverCommand(ctx, "core-building", pluginsdk.CommandRequest{
				Command:       "teleport",
				Args:          "",
				CharacterID:   "01HTEST000000000000000CHAR",
				CharacterName: "Builder",
				LocationID:    "01HTEST000000000000000ROOM",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Status).To(Equal(pluginsdk.CommandError))
			Expect(resp.Output).To(ContainSubstring("Unknown building command"))
		})
	})
})
