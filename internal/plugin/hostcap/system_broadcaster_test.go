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
	"github.com/holomush/holomush/internal/plugin/hostcap"
)

// fakeEventAppender captures appended events for assertion and can be made to
// fail. It satisfies core.EventAppender.
type fakeEventAppender struct {
	events []core.Event
	err    error
}

func (f *fakeEventAppender) Append(_ context.Context, e core.Event) error {
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, e)
	return nil
}

// TestSystemBroadcasterBroadcastAppendsSystemEventToReservedSubject proves the
// SessionAdmin broadcast backing emits a single system-actor system event to the
// reserved broadcast subject — the same shape as command.Services.Broadcast-
// SystemMessage / the shutdown command (ADR holomush-t019a). The host stamps the
// system actor so the plugin needs no `system` in actor_kinds_claimable.
func TestSystemBroadcasterBroadcastAppendsSystemEventToReservedSubject(t *testing.T) {
	app := &fakeEventAppender{}
	b := hostcap.NewSystemBroadcaster(app)

	err := b.BroadcastSystemMessage(context.Background(), "Server restart in 5 minutes.")

	require.NoError(t, err)
	require.Len(t, app.events, 1)
	ev := app.events[0]
	assert.Equal(t, core.SystemBroadcastSubject, ev.Stream, "broadcast must target the reserved system subject")
	assert.Equal(t, core.EventTypeSystem, ev.Type)
	assert.Equal(t, core.ActorSystem, ev.Actor.Kind, "host stamps the system actor")
	assert.Equal(t, core.ActorSystemID, ev.Actor.ID)

	var payload map[string]string
	require.NoError(t, json.Unmarshal(ev.Payload, &payload))
	assert.Equal(t, "Server restart in 5 minutes.", payload["message"])
}

// TestSystemBroadcasterBroadcastWrapsAppenderFailure proves a sink failure is
// surfaced (the sessionAdminServer then maps it to an opaque Internal status).
func TestSystemBroadcasterBroadcastWrapsAppenderFailure(t *testing.T) {
	app := &fakeEventAppender{err: errors.New("bus unavailable")}
	b := hostcap.NewSystemBroadcaster(app)

	err := b.BroadcastSystemMessage(context.Background(), "hi")

	require.Error(t, err)
	assert.Empty(t, app.events)
}

// TestSystemBroadcasterDisconnectReturnsUnsupportedSentinel proves forcible
// disconnect is NOT backed (no production mechanism; gateway concern — decision
// holomush-t019a, follow-up holomush-obo44). It returns the sentinel and appends
// nothing; the sessionAdminServer maps the sentinel to codes.Unimplemented.
func TestSystemBroadcasterDisconnectReturnsUnsupportedSentinel(t *testing.T) {
	app := &fakeEventAppender{}
	b := hostcap.NewSystemBroadcaster(app)

	err := b.DisconnectSession(context.Background(), "01ABC", "idle timeout")

	require.Error(t, err)
	assert.ErrorIs(t, err, hostcap.ErrDisconnectUnsupported)
	assert.Empty(t, app.events, "disconnect must not append any event")
}
