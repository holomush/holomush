// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package pluginparity

import (
	"context"
	"encoding/json"
	"sync"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/plugin/lua"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// capturePublisher records published events for assertion. The mutex guards
// the slice against the bufconn server goroutine writing while the spec
// reads.
type capturePublisher struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (c *capturePublisher) Publish(_ context.Context, e eventbus.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
	return nil
}

func (c *capturePublisher) captured() []eventbus.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]eventbus.Event(nil), c.events...)
}

func mainGameID() string { return "main" }

var _ = Describe("SessionAdmin broadcast backing over the Lua bufconn path (holomush-eykuh.4.2)", func() {
	// End-to-end proof that a brokered SessionAdminService.Broadcast — the surface
	// the migrated `wall` command reaches after the cutover — emits a system event
	// to the reserved subject through the production backing. It drives the REAL
	// Lua host-cap adapter, the REAL sessionAdminServer (LuaDefaultSet), and the
	// REAL hostcap.NewSystemBroadcaster over the SAME in-process transport
	// production uses, wired via the production late path (Host.SetSessionAdmin).
	It("emits a system event to the reserved subject when a plugin broadcasts", func() {
		pub := &capturePublisher{}

		luaHost := lua.NewHostWithFunctions(hostfunc.New(nil))
		DeferCleanup(func() { _ = luaHost.Close(context.Background()) })
		// Production wires the backing late, after the publisher exists.
		luaHost.SetSessionAdmin(hostcap.NewSystemBroadcaster(pub, mainGameID))

		srv := grpc.NewServer()
		hostcap.RegisterCapabilities(srv, hostcap.NewBase(luaHost.HostCapabilitiesAdapter(), parityPluginName), hostcap.LuaDefaultSet)
		conn, err := plugins.NewInProcessConn(srv)
		Expect(err).NotTo(HaveOccurred(), "lua in-process conn must stand up")
		DeferCleanup(func() { _ = conn.Close() })

		client := hostv1.NewSessionAdminServiceClient(conn)

		_, err = client.Broadcast(context.Background(), &hostv1.BroadcastRequest{
			Message: "Server restart in 5 minutes.",
		})
		Expect(err).NotTo(HaveOccurred(), "brokered SessionAdminService.Broadcast must succeed with a wired backing")

		events := pub.captured()
		Expect(events).To(HaveLen(1), "broadcast must emit exactly one event")
		ev := events[0]
		// Post-collapse the recorded Subject is qualified — the pre-collapse bare
		// core.SystemBroadcastSubject ("system") qualified later inside
		// busEventAppender; now sysbroadcast.Broadcaster qualifies before publish
		// (FINDING-5). Pin the exact literal, not a recomputed eventbus.Qualify(...).
		Expect(ev.Subject).To(Equal(eventbus.Subject("events.main."+core.SystemBroadcastSubject)),
			"broadcast must target the reserved system subject, fully qualified")
		Expect(ev.Type).To(Equal(eventbus.Type("system")))
		Expect(ev.Actor.Kind).To(Equal(eventbus.ActorKindSystem), "host stamps the system actor on the plugin's behalf")
		Expect(ev.Actor.ID).To(Equal(core.SystemActorULID))

		var payload map[string]string
		Expect(json.Unmarshal(ev.Payload, &payload)).To(Succeed())
		Expect(payload["message"]).To(Equal("Server restart in 5 minutes."))
	})

	// Forcible disconnect has no production sink (decision holomush-t019a;
	// follow-up holomush-obo44). Even with the broadcast backing wired, the
	// brokered Disconnect fails closed with Unimplemented.
	It("fails closed with Unimplemented for forcible disconnect", func() {
		luaHost := lua.NewHostWithFunctions(hostfunc.New(nil))
		DeferCleanup(func() { _ = luaHost.Close(context.Background()) })
		luaHost.SetSessionAdmin(hostcap.NewSystemBroadcaster(&capturePublisher{}, mainGameID))

		srv := grpc.NewServer()
		hostcap.RegisterCapabilities(srv, hostcap.NewBase(luaHost.HostCapabilitiesAdapter(), parityPluginName), hostcap.LuaDefaultSet)
		conn, err := plugins.NewInProcessConn(srv)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = conn.Close() })

		client := hostv1.NewSessionAdminServiceClient(conn)

		_, err = client.Disconnect(context.Background(), &hostv1.DisconnectRequest{
			SessionId: "01ABCDEF",
			Reason:    "idle timeout",
		})
		Expect(err).To(HaveOccurred())
		Expect(status.Code(err)).To(Equal(codes.Unimplemented),
			"disconnect has no production backing — the broadcaster reports unsupported, mapped to Unimplemented")
	})
})
