// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package authguard

import "github.com/holomush/holomush/internal/eventbus"

// ToSessionIdentity converts a typed authguard.Identity into the
// eventbus.SessionIdentity wire-shape consumed by Subscriber.OpenSession
// and HistoryQuery.Identity. The bridge exists because the eventbus
// package cannot import authguard (cycle: eventbus → authguard → plugin
// → eventbus); SessionIdentity in eventbus mirrors Identity here, and
// this converter is the single conversion point used by the gRPC
// Subscribe and QueryStreamHistory handlers (T10).
func ToSessionIdentity(id Identity) eventbus.SessionIdentity {
	return eventbus.SessionIdentity{
		Kind:        eventbus.IdentityKind(id.Kind),
		PlayerID:    id.PlayerID,
		CharacterID: id.CharacterID,
		BindingID:   id.BindingID,
		PluginName:  id.PluginName,
		InstanceID:  id.InstanceID,
	}
}
