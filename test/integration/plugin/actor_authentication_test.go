// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// stubIdentityRegistry is a minimal in-memory IdentityRegistry for
// integration tests that don't spin up the full Manager. The host needs
// it so RequestEmitToken and DeliverEvent can resolve the plugin name to
// a ULID via stampPluginActor (post-w9ml: actor IDs are ULID strings).
type stubIdentityRegistry struct {
	idsByName map[string]ulid.ULID
}

func (s *stubIdentityRegistry) NameByID(id ulid.ULID) (string, bool) {
	for name, candidate := range s.idsByName {
		if candidate == id {
			return name, true
		}
	}
	return "", false
}

func (s *stubIdentityRegistry) IDByName(name string) (ulid.ULID, bool) {
	id, ok := s.idsByName[name]
	return id, ok
}

// forgeryPluginSourceDir returns the absolute path to the forgery_plugin
// source directory under testdata. Build invocations resolve relative to
// this directory.
func forgeryPluginSourceDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "testdata", "forgery_plugin")
}

// buildForgeryPlugin compiles the forgery_plugin binary into a fresh
// per-test directory laid out the way goplugin.Host.Load expects:
//
//	<dir>/plugin.yaml
//	<dir>/<os>-<arch>/forgery-plugin
//
// Returns the plugin directory (suitable for host.Load's third argument)
// and the parsed manifest. Build is once per test invocation; the dir is
// cleaned up by Ginkgo via GinkgoT().TempDir().
func buildForgeryPlugin() (string, *plugins.Manifest) {
	GinkgoT().Helper()
	pluginDir := GinkgoT().TempDir()
	platformDir := runtime.GOOS + "-" + runtime.GOARCH
	platformSubDir := filepath.Join(pluginDir, platformDir)
	Expect(os.MkdirAll(platformSubDir, 0o755)).To(Succeed())

	srcDir := forgeryPluginSourceDir()
	manifestData, err := os.ReadFile(filepath.Join(srcDir, "plugin.yaml"))
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), manifestData, 0o644)).To(Succeed())

	manifest, err := plugins.ParseManifest(manifestData)
	Expect(err).NotTo(HaveOccurred())

	binaryPath := filepath.Join(platformSubDir, "forgery-plugin")
	cmd := exec.Command("go", "build", "-o", binaryPath, ".") // #nosec G204 -- in-tree test source dir
	cmd.Dir = srcDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, buildErr := cmd.CombinedOutput()
	Expect(buildErr).NotTo(HaveOccurred(), "go build forgery_plugin failed: %s", string(out))

	return pluginDir, manifest
}

// hostFixture wires a goplugin.Host + embedded JetStream bus with the
// PluginEventEmitter that uses the stored actor from ctx. This is the
// production wiring shape — the host's EmitEvent handler stamps
// core.WithActor(ctx, storedActor) before calling emitter.Emit, and the
// resolveActor func returns whatever's on ctx.
type hostFixture struct {
	host       *goplugin.Host
	bus        *eventbustest.Embedded
	manifest   *plugins.Manifest
	pluginULID ulid.ULID // registry-resolved ULID for the plugin's name
	cleanup    func()
}

func newHostFixture(ctx context.Context) *hostFixture {
	GinkgoT().Helper()
	pluginDir, manifest := buildForgeryPlugin()
	bus := eventbustest.New(GinkgoT())

	// Post-w9ml: the host MUST resolve plugin names to ULIDs via an
	// IdentityRegistry. We register the manifest's name with a fresh
	// ULID and inject the stub before construction so DeliverEvent and
	// RequestEmitToken can stamp ActorPlugin:<ULID> on emits.
	pluginULID := core.NewULID()
	reg := &stubIdentityRegistry{idsByName: map[string]ulid.ULID{
		manifest.Name: pluginULID,
	}}
	host := goplugin.NewHost(goplugin.WithIdentityRegistry(reg))

	// Wire the event emitter the same way binary_plugin_test does.
	// resolveActor reads the actor stamped on ctx by EmitEvent's
	// host_service.go (line 85: emitCtx := core.WithActor(ctx,
	// storedActor)). That's the host-vouched actor.
	configureBinaryHostEventEmitter(host, bus.Bus.Publisher(), manifest)

	Expect(host.Load(ctx, manifest, pluginDir)).To(Succeed())

	cleanup := func() {
		_ = host.Close(ctx)
	}
	return &hostFixture{
		host:       host,
		bus:        bus,
		manifest:   manifest,
		pluginULID: pluginULID,
		cleanup:    cleanup,
	}
}

// modePayload constructs a JSON payload encoding the forgery_plugin Mode.
// The schema lives in testdata/forgery_plugin/main.go.
func modePayload(subject string, opts ...modeOption) string {
	m := map[string]any{
		"subject": subject,
	}
	for _, o := range opts {
		o(m)
	}
	b, err := json.Marshal(m)
	Expect(err).NotTo(HaveOccurred())
	return string(b)
}

type modeOption func(map[string]any)

func withForgedActor(kind pluginsdk.ActorKind, id string) modeOption {
	return func(m map[string]any) {
		m["forgery_override_kind"] = strconv.Itoa(int(kind))
		m["forgery_override_id"] = id
	}
}

func withFabricatedToken(tok string) modeOption {
	return func(m map[string]any) {
		m["fabricate_token"] = tok
	}
}

func withBackgroundEmit(resultFile string) modeOption {
	return func(m map[string]any) {
		m["emit_from_background"] = true
		m["result_file"] = resultFile
	}
}

// drainPublishedEvents enumerates all messages currently on the EVENTS
// stream. Mirrors drainEventbusStream in goplugin/host_test.go; we
// re-implement here to keep the test self-contained inside this package.
func drainPublishedEvents(ctx context.Context, fx *hostFixture) []eventHeaders {
	GinkgoT().Helper()
	stream, err := fx.bus.JS.Stream(ctx, eventbus.StreamName)
	Expect(err).NotTo(HaveOccurred())
	info, err := stream.Info(ctx)
	Expect(err).NotTo(HaveOccurred())
	var out []eventHeaders
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq && seq != 0; seq++ {
		msg, gerr := stream.GetMsg(ctx, seq)
		Expect(gerr).NotTo(HaveOccurred())
		out = append(out, eventHeaders{
			Subject:   msg.Subject,
			ActorKind: msg.Header.Get(eventbus.HeaderActorKind),
			ActorID:   msg.Header.Get(eventbus.HeaderActorID),
		})
	}
	return out
}

type eventHeaders struct {
	Subject   string
	ActorKind string
	ActorID   string
}

// pollResultFile polls path until it exists or deadline elapses, returns
// the file contents.
func pollResultFile(path string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		b, err := os.ReadFile(path) //nolint:gosec // path is a test temp file
		if err == nil {
			return string(b), nil
		}
		if !time.Now().Before(deadline) {
			return "", fmt.Errorf("result file %s did not appear before timeout: %w", path, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

const (
	forgeryEmitSubject = "location.01HFORGEY00LOCATIONULID0000"
	dispatchCharID     = "01HCHAR0000000000000000000"
	dispatchCharKind   = core.ActorCharacter
	forgedTargetID     = "01HFAKE0000000000000000000"
)

var _ = Describe("Plugin actor-claim authentication (ec22.1)", func() {
	var (
		ctx       context.Context
		ctxCancel context.CancelFunc
	)

	BeforeEach(func() {
		ctx, ctxCancel = context.WithTimeout(context.Background(), 90*time.Second)
	})

	AfterEach(func() {
		if ctxCancel != nil {
			ctxCancel()
		}
	})

	Describe("Honest binary dispatch", func() {
		It("publishes events stamped with the dispatching character", func() {
			fx := newHostFixture(ctx)
			defer fx.cleanup()

			dispatchCtx := core.WithActor(ctx, core.Actor{Kind: dispatchCharKind, ID: dispatchCharID})
			_, err := fx.host.DeliverEvent(dispatchCtx, fx.manifest.Name, pluginsdk.Event{
				ID:        "01HEVT0000000000000000HONST",
				Stream:    "trigger:01HEVT0000000000000000TRIG",
				Type:      pluginsdk.EventType("core-communication:say"),
				ActorKind: pluginsdk.ActorCharacter,
				ActorID:   dispatchCharID,
				// Honest mode: no forgery overrides, no fabricated token.
				Payload: modePayload(forgeryEmitSubject),
			})
			Expect(err).NotTo(HaveOccurred(), "honest emit through forgery_plugin must succeed")

			events := drainPublishedEvents(ctx, fx)
			Expect(events).To(HaveLen(1))
			Expect(events[0].ActorKind).To(Equal("character"),
				"published event MUST carry the dispatching character's kind")
			Expect(events[0].ActorID).To(Equal(dispatchCharID),
				"published event MUST carry the dispatching character's ID")
		})
	})

	Describe("Forgery override (load-bearing G1)", func() {
		It("publishes events with the host-stored actor, ignoring the plugin's forged claim", func() {
			fx := newHostFixture(ctx)
			defer fx.cleanup()

			dispatchCtx := core.WithActor(ctx, core.Actor{Kind: dispatchCharKind, ID: dispatchCharID})
			_, err := fx.host.DeliverEvent(dispatchCtx, fx.manifest.Name, pluginsdk.Event{
				ID:        "01HEVT0000000000000000FORGE",
				Stream:    "trigger:01HEVT0000000000000000TRIG",
				Type:      pluginsdk.EventType("core-communication:say"),
				ActorKind: pluginsdk.ActorCharacter,
				ActorID:   dispatchCharID,
				// Plugin substitutes plugin-kind + a forged ID into outgoing
				// actor metadata. Token is ferried untouched.
				Payload: modePayload(forgeryEmitSubject,
					withForgedActor(pluginsdk.ActorPlugin, forgedTargetID)),
			})
			Expect(err).NotTo(HaveOccurred(),
				"emit with forged actor metadata + valid token MUST succeed (host ignores forgery)")

			events := drainPublishedEvents(ctx, fx)
			Expect(events).To(HaveLen(1))
			Expect(events[0].ActorKind).To(Equal("character"),
				"G1: published event MUST carry the host-stored character actor, NOT the plugin's plugin-kind claim")
			Expect(events[0].ActorID).To(Equal(dispatchCharID),
				"G1: published event MUST carry the host-stored character ID, NOT the plugin's forged ID")
			Expect(events[0].ActorID).NotTo(Equal(forgedTargetID),
				"G1: forged ID MUST NOT appear on the published event")
		})
	})

	Describe("Token fabrication", func() {
		It("rejects emits whose token was not issued by the host", func() {
			fx := newHostFixture(ctx)
			defer fx.cleanup()

			dispatchCtx := core.WithActor(ctx, core.Actor{Kind: dispatchCharKind, ID: dispatchCharID})
			_, err := fx.host.DeliverEvent(dispatchCtx, fx.manifest.Name, pluginsdk.Event{
				ID:        "01HEVT0000000000000000FAB00",
				Stream:    "trigger:01HEVT0000000000000000TRIG",
				Type:      pluginsdk.EventType("core-communication:say"),
				ActorKind: pluginsdk.ActorCharacter,
				ActorID:   dispatchCharID,
				Payload: modePayload(forgeryEmitSubject,
					withFabricatedToken("not-a-real-host-issued-token")),
			})
			Expect(err).To(HaveOccurred(),
				"emit with fabricated token MUST be rejected by the host")
			// gRPC drops oops Codes over the wire (unary errors carry only
			// the rendered message), so we assert on the EMIT_TOKEN_REJECTED
			// message text from internal/plugin/goplugin/host_service.go.
			Expect(err.Error()).To(ContainSubstring("dispatch token is not valid"),
				"error MUST surface EMIT_TOKEN_REJECTED ('dispatch token is not valid for this plugin')")

			Expect(drainPublishedEvents(ctx, fx)).To(BeEmpty(),
				"no event may be published when emit fails token authentication")
		})
	})

	Describe("Out-of-dispatch background emit", func() {
		// Background emits (a goroutine emitting AFTER HandleEvent
		// returned, with a fresh background ctx and NO dispatch token)
		// are the SAME architectural path as plugin-served gRPC handlers
		// like SceneService.CreateScene: the call did not originate at
		// DeliverEvent / DeliverCommand, so there is no dispatch token
		// to ferry.
		//
		// The SDK falls back to RequestEmitToken to obtain a self-token
		// bound to {ActorPlugin, pluginName}. The emit succeeds, and the
		// published event carries the plugin actor — NOT the original
		// dispatch character. That's the load-bearing G1 invariant: a
		// background goroutine cannot keep emitting under a dispatching
		// character's identity after the dispatch has returned.
		It("publishes a plugin-actor event via the self-token fallback", func() {
			fx := newHostFixture(ctx)
			defer fx.cleanup()

			resultFile := filepath.Join(GinkgoT().TempDir(), "background-result.txt")

			dispatchCtx := core.WithActor(ctx, core.Actor{Kind: dispatchCharKind, ID: dispatchCharID})
			_, err := fx.host.DeliverEvent(dispatchCtx, fx.manifest.Name, pluginsdk.Event{
				ID:        "01HEVT0000000000000000BKGND",
				Stream:    "trigger:01HEVT0000000000000000TRIG",
				Type:      pluginsdk.EventType("core-communication:say"),
				ActorKind: pluginsdk.ActorCharacter,
				ActorID:   dispatchCharID,
				// Mode tells the plugin to spawn a goroutine and emit
				// AFTER HandleEvent returns. The goroutine emits with a
				// fresh background ctx — NO token in metadata. The SDK's
				// self-token fallback picks this up.
				Payload: modePayload(forgeryEmitSubject, withBackgroundEmit(resultFile)),
			})
			Expect(err).NotTo(HaveOccurred(),
				"HandleEvent returns nil; the background emit succeeds via self-token")

			contents, readErr := pollResultFile(resultFile, 10*time.Second)
			Expect(readErr).NotTo(HaveOccurred())
			Expect(contents).To(Equal("ok"),
				"background emit via self-token fallback MUST succeed")

			// G1 invariant: published event carries the PLUGIN actor
			// (the self-token's hardcoded binding), NOT the original
			// dispatching character — a background goroutine cannot
			// continue acting under the dispatch character's identity.
			//
			// Post-w9ml: self-tokens are issued with a plugin-resolved
			// ULID via IdentityRegistry; HeaderActorID carries that
			// ULID, NOT the dispatching character ID. The strict gate
			// at internal/plugin/event_emitter.go::Emit now rejects
			// non-ULID actor IDs with ACTOR_ID_NOT_ULID, so the actor
			// id MUST be a parseable ULID.
			events := drainPublishedEvents(ctx, fx)
			Expect(events).To(HaveLen(1),
				"self-token fallback produces exactly one published event")
			Expect(events[0].ActorKind).To(Equal("plugin"),
				"G1: background self-token emit MUST carry plugin actor kind, NOT character")
			Expect(events[0].ActorID).To(Equal(fx.pluginULID.String()),
				"G1: background self-token emit MUST carry the registry-resolved plugin ULID")
			Expect(events[0].ActorID).NotTo(Equal(dispatchCharID),
				"G1: background emit MUST NOT inherit the dispatching character's ULID")
		})
	})

	// G3 cross-plugin token leak is unit-tested at host_service_test.go::
	// TestEmitEventCrossPluginTokenLeakFails. We do not duplicate it here
	// because it requires loading two binary plugins side-by-side and the
	// invariant is fully exercised by the unit test against the real
	// pluginHostServiceServer.

	// Lua plugin manifest-gate scenarios (spec §5.7 — Lua + binary
	// symmetry). The manifest gate lives in plugins.PluginEventEmitter.Emit
	// and fires identically for both runtimes; we drive Emit directly here
	// rather than spinning up a Lua VM so the assertion stays focused on
	// the gate itself. Full Lua-runtime coverage of the same code path is
	// at internal/plugin/event_emitter_test.go — these integration specs
	// exist to assert the plumbing reaches Emit unchanged when the
	// manifest is Lua-typed.
	Describe("Lua plugin manifest gate", func() {
		It("publishes events stamped with the dispatching character when manifest opts into [plugin, character]", func() {
			bus := eventbustest.New(GinkgoT())
			manifest := &plugins.Manifest{
				Name:                "echo-bot-test",
				Type:                plugins.TypeLua,
				Emits:               []string{"location"},
				ActorKindsClaimable: []string{"plugin", "character"},
			}
			emitter := plugins.NewPluginEventEmitter(
				bus.Bus.Publisher(),
				func(name string) *plugins.Manifest {
					if name == manifest.Name {
						return manifest
					}
					return nil
				},
				func(emitCtx context.Context, _ string) (core.Actor, error) {
					if a, ok := core.ActorFromContext(emitCtx); ok {
						return a, nil
					}
					return core.Actor{}, fmt.Errorf("no actor on ctx")
				},
			)

			emitCtx := core.WithActor(ctx, core.Actor{Kind: dispatchCharKind, ID: dispatchCharID})
			err := emitter.Emit(emitCtx, manifest.Name, pluginsdk.EmitIntent{
				Subject: forgeryEmitSubject,
				Type:    pluginsdk.EventType("core-communication:say"),
				Payload: `{"message":"lua-claim"}`,
			})
			Expect(err).NotTo(HaveOccurred(),
				"Lua-typed manifest declaring [plugin, character] MUST allow character-actor emit")

			stream, sErr := bus.JS.Stream(ctx, eventbus.StreamName)
			Expect(sErr).NotTo(HaveOccurred())
			info, iErr := stream.Info(ctx)
			Expect(iErr).NotTo(HaveOccurred())
			Expect(info.State.Msgs).To(BeNumerically(">=", uint64(1)))
		})

		It("rejects character-actor emits when the Lua manifest does not opt in", func() {
			bus := eventbustest.New(GinkgoT())
			manifest := &plugins.Manifest{
				Name:                "lua-plugin-only",
				Type:                plugins.TypeLua,
				Emits:               []string{"location"},
				ActorKindsClaimable: []string{"plugin"}, // character intentionally omitted
			}
			emitter := plugins.NewPluginEventEmitter(
				bus.Bus.Publisher(),
				func(name string) *plugins.Manifest {
					if name == manifest.Name {
						return manifest
					}
					return nil
				},
				func(emitCtx context.Context, _ string) (core.Actor, error) {
					if a, ok := core.ActorFromContext(emitCtx); ok {
						return a, nil
					}
					return core.Actor{}, fmt.Errorf("no actor on ctx")
				},
			)

			emitCtx := core.WithActor(ctx, core.Actor{Kind: dispatchCharKind, ID: dispatchCharID})
			err := emitter.Emit(emitCtx, manifest.Name, pluginsdk.EmitIntent{
				Subject: forgeryEmitSubject,
				Type:    pluginsdk.EventType("core-communication:say"),
				Payload: `{"message":"should-fail"}`,
			})
			Expect(err).To(HaveOccurred(),
				"Lua-typed manifest declaring only [plugin] MUST reject character-actor emit")
			// Direct emitter call — oops Code is preserved (not over the
			// wire). Assert via oops.AsOops to be precise about the code.
			oopsErr, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue(), "expected oops error, got %T", err)
			Expect(oopsErr.Code()).To(Equal("EMIT_ACTOR_KIND_NOT_CLAIMABLE"),
				"manifest gate MUST surface EMIT_ACTOR_KIND_NOT_CLAIMABLE")

			// Verify no event reached the stream.
			stream, sErr := bus.JS.Stream(ctx, eventbus.StreamName)
			Expect(sErr).NotTo(HaveOccurred())
			info, iErr := stream.Info(ctx)
			Expect(iErr).NotTo(HaveOccurred())
			Expect(info.State.Msgs).To(Equal(uint64(0)),
				"no event may be published when manifest gate rejects")
		})
	})
})
