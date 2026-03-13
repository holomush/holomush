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
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/stretchr/testify/mock"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/capability"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	"github.com/holomush/holomush/internal/plugin/mocks"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// echoBotFixture contains all components needed for echo-bot integration tests.
type echoBotFixture struct {
	LuaHost  *pluginlua.Host
	Enforcer *capability.Enforcer
	Plugin   *plugins.DiscoveredPlugin
	Cleanup  func()
}

// setupEchoBotTest creates all components needed to test the echo-bot plugins.
func setupEchoBotTest() (*echoBotFixture, error) {
	pluginsDir, err := findPluginsDir()
	if err != nil {
		return nil, err
	}
	echoBotDir := filepath.Join(pluginsDir, "echo-bot")

	if _, statErr := os.Stat(echoBotDir); os.IsNotExist(statErr) {
		return nil, statErr
	}

	enforcer := capability.NewEnforcer()
	hostFuncs := hostfunc.New(nil, enforcer)
	luaHost := pluginlua.NewHostWithFunctions(hostFuncs)

	manager := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))

	ctx := context.Background()
	discovered, err := manager.Discover(ctx)
	if err != nil {
		luaHost.Close(ctx) //nolint:errcheck
		return nil, err
	}

	var echoBotPlugin *plugins.DiscoveredPlugin
	for _, dp := range discovered {
		if dp.Manifest.Name == "echo-bot" {
			echoBotPlugin = dp
			break
		}
	}

	if echoBotPlugin == nil {
		luaHost.Close(ctx) //nolint:errcheck
		return nil, os.ErrNotExist
	}

	if err := luaHost.Load(ctx, echoBotPlugin.Manifest, echoBotPlugin.Dir); err != nil {
		luaHost.Close(ctx) //nolint:errcheck
		return nil, err
	}

	if err := enforcer.SetGrants("echo-bot", echoBotPlugin.Manifest.Capabilities); err != nil {
		luaHost.Close(ctx) //nolint:errcheck
		return nil, err
	}

	return &echoBotFixture{
		LuaHost:  luaHost,
		Enforcer: enforcer,
		Plugin:   echoBotPlugin,
		Cleanup: func() {
			_ = luaHost.Close(context.Background())
		},
	}, nil
}

// findPluginsDir locates the plugins directory relative to the test.
func findPluginsDir() (string, error) {
	// Try relative paths from test location
	candidates := []string{
		"../../plugins",       // From internal/plugin
		"../../../plugins",    // If test is deeper
		"./plugins",           // Current directory
		"../../../../plugins", // Deeper nesting
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for _, candidate := range candidates {
		path := filepath.Join(cwd, candidate)
		absPath, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		if _, err := os.Stat(absPath); err == nil {
			return absPath, nil
		}
	}

	return "", os.ErrNotExist
}

var _ = Describe("Echo Bot Integration", func() {
	var fixture *echoBotFixture

	BeforeEach(func() {
		var err error
		fixture, err = setupEchoBotTest()
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

		It("subscribes to say events", func() {
			Expect(slices.Contains(fixture.Plugin.Manifest.Events, "say")).To(BeTrue())
		})

		It("has events.emit.location capability", func() {
			Expect(slices.Contains(fixture.Plugin.Manifest.Capabilities, "events.emit.location")).To(BeTrue())
		})
	})

	Describe("Event Handling", func() {
		Context("when receiving say events from characters", func() {
			It("responds with echo message", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01ABC",
					Stream:    "location:123",
					Type:      pluginsdk.EventTypeSay,
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char_1",
					Payload:   `{"message":"Hello, world!"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "echo-bot", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(HaveLen(1))

				Expect(emits[0].Stream).To(Equal("location:123"))
				Expect(emits[0].Type).To(Equal(pluginsdk.EventTypeSay))

				var payload map[string]string
				err = json.Unmarshal([]byte(emits[0].Payload), &payload)
				Expect(err).NotTo(HaveOccurred())

				Expect(strings.HasPrefix(payload["message"], "Echo:")).To(BeTrue())
				Expect(payload["message"]).To(ContainSubstring("Hello, world!"))
			})
		})

		Context("when receiving events from plugins", func() {
			It("ignores them to prevent loops", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01DEF",
					Stream:    "location:123",
					Type:      pluginsdk.EventTypeSay,
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorPlugin,
					ActorID:   "some-plugin",
					Payload:   `{"message":"Echo: something"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "echo-bot", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(BeEmpty())
			})
		})

		Context("when receiving non-say events", func() {
			It("ignores them", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01GHI",
					Stream:    "location:123",
					Type:      pluginsdk.EventTypePose,
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char_1",
					Payload:   `{"message":"waves hello"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "echo-bot", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(BeEmpty())
			})
		})

		Context("when receiving empty messages", func() {
			It("ignores them", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01JKL",
					Stream:    "location:123",
					Type:      pluginsdk.EventTypeSay,
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char_1",
					Payload:   `{"message":""}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "echo-bot", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(BeEmpty())
			})
		})

		Context("when payload has no message key", func() {
			It("handles gracefully", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01MNO",
					Stream:    "location:123",
					Type:      pluginsdk.EventTypeSay,
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char_1",
					Payload:   `{"text":"wrong key"}`,
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "echo-bot", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(BeEmpty())
			})
		})

		Context("when payload is empty", func() {
			It("handles gracefully", func() {
				ctx := context.Background()
				event := pluginsdk.Event{
					ID:        "01PQR",
					Stream:    "location:123",
					Type:      pluginsdk.EventTypeSay,
					Timestamp: time.Now().UnixMilli(),
					ActorKind: pluginsdk.ActorCharacter,
					ActorID:   "char_1",
					Payload:   "",
				}

				emits, err := fixture.LuaHost.DeliverEvent(ctx, "echo-bot", event)
				Expect(err).NotTo(HaveOccurred())
				Expect(emits).To(BeEmpty())
			})
		})
	})

	Describe("Subscriber Integration", func() {
		It("processes events through the full subscriber flow", func() {
			// Create mock emitter to capture emitted events with thread-safe storage
			var mu sync.Mutex
			var emitted []pluginsdk.EmitEvent

			emitter := mocks.NewMockEventEmitter(GinkgoT())
			emitter.EXPECT().EmitPluginEvent(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
				func(_ context.Context, _ string, event pluginsdk.EmitEvent) error {
					mu.Lock()
					defer mu.Unlock()
					emitted = append(emitted, event)
					return nil
				},
			)

			// Create subscriber
			subscriber := plugins.NewSubscriber(fixture.LuaHost, emitter)
			subscriber.Subscribe("echo-bot", "location:123", fixture.Plugin.Manifest.Events)

			// Start subscriber with event channel
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			events := make(chan pluginsdk.Event, 10)
			subscriber.Start(ctx, events)

			// Send a say event
			events <- pluginsdk.Event{
				ID:        "01STU",
				Stream:    "location:123",
				Type:      pluginsdk.EventTypeSay,
				Timestamp: time.Now().UnixMilli(),
				ActorKind: pluginsdk.ActorCharacter,
				ActorID:   "char_1",
				Payload:   `{"message":"Test message"}`,
			}

			// Wait for processing
			Eventually(func() int {
				mu.Lock()
				defer mu.Unlock()
				return len(emitted)
			}).Should(Equal(1))

			// Stop subscriber
			cancel()
			subscriber.Stop()

			// Verify emitted event
			mu.Lock()
			emittedCopy := make([]pluginsdk.EmitEvent, len(emitted))
			copy(emittedCopy, emitted)
			mu.Unlock()

			Expect(emittedCopy).To(HaveLen(1))
			Expect(emittedCopy[0].Stream).To(Equal("location:123"))

			var payload map[string]string
			err := json.Unmarshal([]byte(emittedCopy[0].Payload), &payload)
			Expect(err).NotTo(HaveOccurred())
			Expect(payload["message"]).To(ContainSubstring("Echo:"))
		})
	})
})
