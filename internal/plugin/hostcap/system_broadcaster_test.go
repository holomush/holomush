// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/plugin/hostcap"
)

// fakePublisher captures published events for assertion and can be made to
// fail. It satisfies eventbus.Publisher.
type fakePublisher struct {
	events []eventbus.Event
	err    error
}

func (f *fakePublisher) Publish(_ context.Context, e eventbus.Event) error {
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, e)
	return nil
}

func mainGameID() string { return "main" }

// TestSystemBroadcasterBroadcastAppendsSystemEventToReservedSubject proves the
// SessionAdmin broadcast backing delegates to sysbroadcast.Broadcaster,
// pinning the reserved broadcast subject — the same shape as
// command.Services.BroadcastSystemMessage / the shutdown command (ADR
// holomush-t019a). The host stamps the system actor so the plugin needs no
// `system` in actor_kinds_claimable.
func TestSystemBroadcasterBroadcastAppendsSystemEventToReservedSubject(t *testing.T) {
	pub := &fakePublisher{}
	b := hostcap.NewSystemBroadcaster(pub, mainGameID)

	err := b.BroadcastSystemMessage(context.Background(), "Server restart in 5 minutes.")

	require.NoError(t, err)
	require.Len(t, pub.events, 1)
	ev := pub.events[0]
	assert.Equal(t, eventbus.Subject("events.main."+core.SystemBroadcastSubject), ev.Subject,
		"broadcast must target the reserved system subject, fully qualified")
	assert.Equal(t, eventbus.Type("system"), ev.Type)
	assert.Equal(t, eventbus.ActorKindSystem, ev.Actor.Kind, "host stamps the system actor")
	assert.Equal(t, core.SystemActorULID, ev.Actor.ID)

	var payload map[string]string
	require.NoError(t, json.Unmarshal(ev.Payload, &payload))
	assert.Equal(t, "Server restart in 5 minutes.", payload["message"])
}

// TestSystemBroadcasterBroadcastWrapsPublishFailure proves a sink failure is
// surfaced (the sessionAdminServer then maps it to an opaque Internal status).
func TestSystemBroadcasterBroadcastWrapsPublishFailure(t *testing.T) {
	pub := &fakePublisher{err: errors.New("bus unavailable")}
	b := hostcap.NewSystemBroadcaster(pub, mainGameID)

	err := b.BroadcastSystemMessage(context.Background(), "hi")

	require.Error(t, err)
	assert.Empty(t, pub.events)
}

// TestSystemBroadcasterDisconnectReturnsUnsupportedSentinel proves forcible
// disconnect is NOT backed (no production mechanism; gateway concern — decision
// holomush-t019a, follow-up holomush-obo44). It returns the sentinel and
// publishes nothing; the sessionAdminServer maps the sentinel to
// codes.Unimplemented.
func TestSystemBroadcasterDisconnectReturnsUnsupportedSentinel(t *testing.T) {
	pub := &fakePublisher{}
	b := hostcap.NewSystemBroadcaster(pub, mainGameID)

	err := b.DisconnectSession(context.Background(), "01ABC", "idle timeout")

	require.Error(t, err)
	assert.ErrorIs(t, err, hostcap.ErrDisconnectUnsupported)
	assert.Empty(t, pub.events, "disconnect must not publish any event")
}
