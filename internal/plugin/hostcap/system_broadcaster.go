// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
)

// ErrDisconnectUnsupported is returned by SessionAdmin.DisconnectSession backings
// that have no forcible-disconnect mechanism. The sessionAdminServer maps it to
// codes.Unimplemented so a wired-but-unsupported backing is indistinguishable to
// the caller from the unwired nil case. Forcible session disconnect is a
// gateway-layer concern with no core sink today (decision holomush-t019a;
// follow-up holomush-obo44).
var ErrDisconnectUnsupported = errors.New("session disconnect is not supported")

// systemBroadcaster backs hostcap.SessionAdmin by emitting a system-actor system
// event to the reserved broadcast subject via a core.EventAppender — the same
// mechanism as command.Services.BroadcastSystemMessage and the shutdown command
// (decision holomush-t019a). The host stamps the system actor, so a broadcasting
// plugin does not need `system` in its actor_kinds_claimable: the privileged emit
// is performed by the host on the plugin's behalf, gated by the session.admin
// capability declaration at the brokered SessionAdminService boundary.
//
// DisconnectSession is intentionally unbacked (ErrDisconnectUnsupported): no
// production forcible-disconnect mechanism exists and none has a caller; it is a
// gateway concern tracked in holomush-obo44.
type systemBroadcaster struct {
	appender core.EventAppender
}

// NewSystemBroadcaster builds a SessionAdmin broadcast backing over appender.
// Returned as the SessionAdmin interface so callers cannot reach the struct.
func NewSystemBroadcaster(appender core.EventAppender) SessionAdmin {
	return &systemBroadcaster{appender: appender}
}

// BroadcastSystemMessage emits a single system-actor system event carrying
// message to the reserved broadcast subject. Mirrors the {"message": ...} payload
// shape of command.Services.BroadcastSystemMessage so terminal/web clients render
// host announcements and plugin `wall` announcements identically.
func (b *systemBroadcaster) BroadcastSystemMessage(ctx context.Context, message string) error {
	//nolint:errcheck // json.Marshal cannot fail for map[string]string
	payload, _ := json.Marshal(map[string]string{"message": message})
	event := core.NewEvent(
		core.SystemBroadcastSubject,
		core.EventTypeSystem,
		core.Actor{Kind: core.ActorSystem, ID: core.ActorSystemID},
		payload,
	)
	if err := b.appender.Append(ctx, event); err != nil {
		return oops.Code("SYSTEM_BROADCAST_FAILED").Wrap(err)
	}
	return nil
}

// DisconnectSession is unsupported — see ErrDisconnectUnsupported.
func (b *systemBroadcaster) DisconnectSession(_ context.Context, _, _ string) error {
	return ErrDisconnectUnsupported
}
