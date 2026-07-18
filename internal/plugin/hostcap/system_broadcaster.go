// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"context"
	"errors"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/sysbroadcast"
)

// ErrDisconnectUnsupported is returned by SessionAdmin.DisconnectSession backings
// that have no forcible-disconnect mechanism. The sessionAdminServer maps it to
// codes.Unimplemented so a wired-but-unsupported backing is indistinguishable to
// the caller from the unwired nil case. Forcible session disconnect is a
// gateway-layer concern with no core sink today (decision holomush-t019a;
// follow-up holomush-obo44).
var ErrDisconnectUnsupported = errors.New("session disconnect is not supported")

// systemBroadcaster backs hostcap.SessionAdmin by pinning the reserved
// broadcast subject and delegating to the one system-broadcast builder,
// sysbroadcast.Broadcaster (D-02). The host stamps the system
// actor, so a broadcasting plugin does not need `system` in its
// actor_kinds_claimable: the privileged emit is performed by the host on the
// plugin's behalf, gated by the session.admin capability declaration at the
// brokered SessionAdminService boundary.
//
// DisconnectSession is intentionally unbacked (ErrDisconnectUnsupported): no
// production forcible-disconnect mechanism exists and none has a caller; it is a
// gateway concern tracked in holomush-obo44.
type systemBroadcaster struct {
	b *sysbroadcast.Broadcaster
}

// NewSystemBroadcaster builds a SessionAdmin broadcast backing over pub,
// qualifying subjects with the game id returned by gameID. Returned as the
// SessionAdmin interface so callers cannot reach the struct.
func NewSystemBroadcaster(pub eventbus.Publisher, gameID func() string) SessionAdmin {
	return &systemBroadcaster{b: sysbroadcast.NewBroadcaster(pub, gameID)}
}

// BroadcastSystemMessage emits a single system-actor system event carrying
// message to the reserved broadcast subject.
func (b *systemBroadcaster) BroadcastSystemMessage(ctx context.Context, message string) error {
	if err := b.b.Broadcast(ctx, core.SystemBroadcastSubject, message); err != nil {
		return oops.Wrap(err)
	}
	return nil
}

// DisconnectSession is unsupported — see ErrDisconnectUnsupported.
func (b *systemBroadcaster) DisconnectSession(_ context.Context, _, _ string) error {
	return ErrDisconnectUnsupported
}
