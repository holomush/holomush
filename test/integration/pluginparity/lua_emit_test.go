// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package pluginparity

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/plugin/lua"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// emitIntentFrom bridges pluginsdk.EmitEvent (the plugin-return shape, with
// Stream as the legacy field name) to pluginsdk.EmitIntent (the host-facing
// shape, with Subject). This mirrors Manager.emitIntentFromEmitEvent, which
// is unexported; we inline the mapping here so the test package can call
// PluginEventEmitter.Emit without depending on Manager internals.
func emitIntentFrom(ev pluginsdk.EmitEvent) pluginsdk.EmitIntent {
	return pluginsdk.EmitIntent{
		Subject:   ev.Stream,
		Type:      ev.Type,
		Payload:   ev.Payload,
		Sensitive: ev.Sensitive,
	}
}

// emitLocationLua is a minimal Lua plugin body that returns one emit event
// targeting a location stream. The plugin uses the canonical return-table
// shape: {subject=..., type=..., payload=...} read by parseEmitEvents in
// internal/plugin/lua/host.go. Payload is valid JSON (Go-side validation
// at event_emitter.go::Emit rejects non-JSON payloads).
const emitLocationLua = `
function on_event(event)
    return {
        {
            subject = "location.01HLOC0000000000000000000",
            type    = "test:ping",
            payload = '{"msg":"hi"}',
        }
    }
end
`

// pluginActorULID is a deterministic ULID used as the plugin actor ID in emit
// tests. Actor IDs MUST be parseable ULIDs post-w9ml; this satisfies that
// constraint without spinning up an IdentityRegistry.
var pluginActorULID = ulid.MustNew(0xAAAA0001, bytes.NewReader(make([]byte, 16)))

// actorFromCtxResolver is the actor resolver for Lua emit tests: it returns
// whatever core.Actor is on the context (stamped by the caller, mirroring
// what the production actor-stamp interceptor does for hostcap RPCs and what
// the production Subscriber does for event delivery). This is the same
// resolver shape used in test/integration/plugin/binary_plugin_test.go's
// configureBinaryHostEventEmitter — the actor on ctx IS the host-vouched actor.
func actorFromCtxLuaResolver(emitCtx context.Context, _ string) (core.Actor, error) {
	if a, ok := core.ActorFromContext(emitCtx); ok {
		return a, nil
	}
	return core.Actor{}, oops.New("no actor on emit context")
}

// loadLuaEmitPlugin writes the given Lua body into a temp dir, builds a
// minimal manifest, and loads the plugin into the provided host. Returns the
// manifest so callers can wire the emitter lookup.
func loadLuaEmitPlugin(host *lua.Host, pluginName string, luaBody string, emitsNS string, actorKinds []string) *plugins.Manifest {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	Expect(os.WriteFile(filepath.Join(dir, "main.lua"), []byte(luaBody), 0o600)).To(Succeed())

	manifest := &plugins.Manifest{
		Name:                pluginName,
		Version:             "1.0.0",
		Type:                plugins.TypeLua,
		Emits:               []string{emitsNS},
		ActorKindsClaimable: actorKinds,
		LuaPlugin:           &plugins.LuaConfig{Entry: "main.lua"},
	}
	Expect(host.Load(context.Background(), manifest, dir)).To(Succeed())
	return manifest
}

var _ = Describe("Lua emit through the production bridge", func() {
	// These specs exercise the end-to-end path:
	//   lua.Host.DeliverEvent → returns []pluginsdk.EmitEvent
	//   → caller drives PluginEventEmitter.Emit for each
	//   → actor_kinds_claimable gate at event_emitter.go::Emit (line 129)
	//
	// This is the production Lua emit path: the on_event handler returns an
	// emit table, the host reads it via parseEmitEvents in DeliverEvent, and
	// PluginEventEmitter.Emit (the production emitter) is called for each.
	// Both gate passes and gate rejections are proven here.
	//
	// The actor identity is supplied by the test's actorFromCtxLuaResolver,
	// which reads core.ActorFromContext — mirroring how the production
	// Subscriber stamps the dispatching actor before DeliverEvent (and how
	// the actor-stamp interceptor added in holomush-eykuh.4.5 stamps
	// {ActorPlugin, pluginName} on hostcap RPC contexts for the bufconn
	// endpoint path). The two paths converge at PluginEventEmitter.Emit.
	//
	// The gate under test (actor_kinds_claimable, event_emitter.go:129) is
	// the same code path exercised by the unit tests in
	// internal/plugin/event_emitter_test.go; this spec proves it is reached
	// when actual Lua hostfunc emit flows through DeliverEvent, not just
	// when Emit is called directly.

	It("allows emit when the manifest declares the plugin actor kind", func() {
		// Verifies: the actor_kinds_claimable gate PASSES when the actor's Kind
		// (ActorPlugin) is listed in the manifest. The event reaches the bus.
		host := lua.NewHostWithFunctions(hostfunc.New(nil))
		DeferCleanup(func() { _ = host.Close(context.Background()) })

		const pluginName = "lua-emit-plugin-allow"
		manifest := loadLuaEmitPlugin(host, pluginName, emitLocationLua, "location", []string{"plugin"})

		bus := eventbustest.New(GinkgoT())
		emitter := plugins.NewPluginEventEmitter(
			bus.Bus.Publisher(),
			func(name string) *plugins.Manifest {
				if name == manifest.Name {
					return manifest
				}
				return nil
			},
			actorFromCtxLuaResolver,
		)

		// Stamp the host-vouched plugin actor onto ctx, mirroring what the
		// production Subscriber / actor-stamp interceptor (eykuh.4.5) does.
		dispatchCtx := core.WithActor(context.Background(), core.Actor{
			Kind: core.ActorPlugin,
			ID:   pluginActorULID.String(),
		})

		emitEvents, err := host.DeliverEvent(dispatchCtx, pluginName, pluginsdk.Event{
			ID:     core.NewULID().String(),
			Stream: "location.01HLOC0000000000000000000",
			Type:   "test:ping",
		})
		Expect(err).NotTo(HaveOccurred(), "DeliverEvent must succeed — Lua body is valid")
		Expect(emitEvents).NotTo(BeEmpty(), "Lua body must return at least one emit event in the on_event return table")

		// Drive PluginEventEmitter.Emit for each returned event, mirroring
		// Manager.EmitPluginEvent. The actor_kinds_claimable gate at
		// event_emitter.go:129 must PASS because "plugin" is declared.
		for _, ev := range emitEvents {
			emitErr := emitter.Emit(dispatchCtx, pluginName, emitIntentFrom(ev))
			Expect(emitErr).NotTo(HaveOccurred(),
				"emit MUST succeed: manifest declares [plugin] and actor is ActorPlugin")
		}
	})

	It("rejects emit when the manifest does not declare the plugin actor kind", func() {
		// Verifies: the actor_kinds_claimable gate REJECTS the emit when the
		// actor's Kind (ActorPlugin) is absent from the manifest. No event
		// reaches the bus and EMIT_ACTOR_KIND_NOT_CLAIMABLE is surfaced.
		host := lua.NewHostWithFunctions(hostfunc.New(nil))
		DeferCleanup(func() { _ = host.Close(context.Background()) })

		const pluginName = "lua-emit-plugin-deny"
		// Empty actor_kinds_claimable → no actor kind is permitted.
		manifest := loadLuaEmitPlugin(host, pluginName, emitLocationLua, "location", []string{})

		bus := eventbustest.New(GinkgoT())
		emitter := plugins.NewPluginEventEmitter(
			bus.Bus.Publisher(),
			func(name string) *plugins.Manifest {
				if name == manifest.Name {
					return manifest
				}
				return nil
			},
			actorFromCtxLuaResolver,
		)

		dispatchCtx := core.WithActor(context.Background(), core.Actor{
			Kind: core.ActorPlugin,
			ID:   pluginActorULID.String(),
		})

		emitEvents, err := host.DeliverEvent(dispatchCtx, pluginName, pluginsdk.Event{
			ID:     core.NewULID().String(),
			Stream: "location.01HLOC0000000000000000000",
			Type:   "test:ping",
		})
		Expect(err).NotTo(HaveOccurred(), "DeliverEvent itself must succeed — the gate fires at Emit, not DeliverEvent")
		Expect(emitEvents).NotTo(BeEmpty(), "Lua body must still buffer the emit; gate fires downstream")

		// The actor_kinds_claimable gate at event_emitter.go:129 must REJECT
		// because "plugin" is absent from ActorKindsClaimable.
		for _, ev := range emitEvents {
			emitErr := emitter.Emit(dispatchCtx, pluginName, emitIntentFrom(ev))
			Expect(emitErr).To(HaveOccurred(),
				"emit MUST be rejected when manifest does not declare the actor kind")
			oopsErr, ok := oops.AsOops(emitErr)
			Expect(ok).To(BeTrue(), "expected oops error, got %T", emitErr)
			Expect(oopsErr.Code()).To(Equal("EMIT_ACTOR_KIND_NOT_CLAIMABLE"),
				"gate at event_emitter.go::Emit MUST surface EMIT_ACTOR_KIND_NOT_CLAIMABLE")
		}
	})
})
