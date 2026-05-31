// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"

	"github.com/samber/oops"
	"google.golang.org/grpc/metadata"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// SettingScope selects which settings partition a Get/Set targets. Ordinals
// mirror pluginv1.SettingScope so the mapping in toProtoSettingScope stays a
// one-to-one translation.
type SettingScope int

const (
	// SettingScopeUnspecified is the zero value and the host rejects it.
	SettingScopeUnspecified SettingScope = iota
	// SettingScopeGame targets the game-wide partition. The host binds the
	// plugin partition from the authenticated plugin name; PrincipalID is
	// ignored.
	SettingScopeGame
	// SettingScopePlayer targets a single player's partition keyed by
	// PrincipalID (a player ULID).
	SettingScopePlayer
	// SettingScopeCharacter targets a single character's partition keyed by
	// PrincipalID (a character ULID).
	SettingScopeCharacter
)

// toProtoSettingScope maps an SDK SettingScope to its proto enum value.
// Unknown values fall back to UNSPECIFIED, which the host rejects.
func toProtoSettingScope(s SettingScope) pluginv1.SettingScope {
	switch s {
	case SettingScopeGame:
		return pluginv1.SettingScope_SETTING_SCOPE_GAME
	case SettingScopePlayer:
		return pluginv1.SettingScope_SETTING_SCOPE_PLAYER
	case SettingScopeCharacter:
		return pluginv1.SettingScope_SETTING_SCOPE_CHARACTER
	default:
		return pluginv1.SettingScope_SETTING_SCOPE_UNSPECIFIED
	}
}

// SettingsClient is the SDK-facing facade binary plugins use to read and write
// host-managed settings via PluginHostService.GetSetting/SetSetting. The host
// binds the plugin partition from the authenticated plugin name, so there is no
// plugin parameter. Phase 8 settings are list-valued.
//
// For PLAYER/CHARACTER scopes the host enforces principal_id ownership; a
// plugin cannot read or write another principal's partition. PLAYER-scope
// resolution is functional as of holomush-iokti.19: principal_id is compared
// against the host-vouched owning player of the acting character (stamped at
// command dispatch). It fails closed only when the dispatch carried no player
// context.
type SettingsClient interface {
	// GetSetting reads the string-list value for key in the given scope and
	// principal partition. found is false when no value is stored. A nil
	// client fails closed.
	GetSetting(ctx context.Context, scope SettingScope, principalID, key string) (values []string, found bool, err error)

	// SetSetting writes the string-list value for key in the given scope and
	// principal partition. A nil client fails closed.
	SetSetting(ctx context.Context, scope SettingScope, principalID, key string, values []string) error
}

// SettingsClientAware is the optional interface service providers implement to
// receive a SettingsClient during Init, parallel to HostEvaluatorAware and
// FocusClientAware. Implement this on the plugin struct to get the settings
// client injected before Init is called.
type SettingsClientAware interface {
	SetSettingsClient(SettingsClient)
}

// pluginHostSettingsClient is the concrete SettingsClient used by binary
// plugins. It wraps the generated PluginHostServiceClient.
type pluginHostSettingsClient struct {
	client pluginv1.PluginHostServiceClient
}

// newPluginHostSettingsClient constructs a SettingsClient wrapping the given
// PluginHostServiceClient. Exposed to the adapter for wiring; test code
// constructs a pluginHostSettingsClient directly.
func newPluginHostSettingsClient(client pluginv1.PluginHostServiceClient) SettingsClient {
	return &pluginHostSettingsClient{client: client}
}

// withDispatchToken ferries the host-issued per-dispatch token from the
// incoming command context to the outgoing settings RPC. The host's
// GetSetting/SetSetting handlers are token-gated for every scope (GAME
// included) via resolveSettingScope → actorFromToken, returning
// EMIT_TOKEN_MISSING without the x-holomush-emit-token header. This is the
// identical mechanism Evaluate uses (see emitTokenHeader in event_sink.go).
func withDispatchToken(ctx context.Context) context.Context {
	if existing, ok := metadata.FromOutgoingContext(ctx); ok && len(existing.Get(emitTokenHeader)) > 0 {
		return ctx
	}
	if incoming, ok := metadata.FromIncomingContext(ctx); ok {
		if tokens := incoming.Get(emitTokenHeader); len(tokens) > 0 && tokens[0] != "" {
			return metadata.AppendToOutgoingContext(ctx, emitTokenHeader, tokens[0])
		}
	}
	return ctx
}

func (c *pluginHostSettingsClient) GetSetting(ctx context.Context, scope SettingScope, principalID, key string) (values []string, found bool, err error) {
	if c.client == nil {
		return nil, false, oops.New("plugin host settings client is not configured")
	}
	resp, err := c.client.GetSetting(withDispatchToken(ctx), &pluginv1.PluginHostServiceGetSettingRequest{
		Scope:       toProtoSettingScope(scope),
		PrincipalId: principalID,
		Key:         key,
	})
	if err != nil {
		return nil, false, oops.With("scope", scope).With("key", key).Wrap(err)
	}
	return resp.GetStringList(), resp.GetFound(), nil
}

func (c *pluginHostSettingsClient) SetSetting(ctx context.Context, scope SettingScope, principalID, key string, values []string) error {
	if c.client == nil {
		return oops.New("plugin host settings client is not configured")
	}
	_, err := c.client.SetSetting(withDispatchToken(ctx), &pluginv1.PluginHostServiceSetSettingRequest{
		Scope:       toProtoSettingScope(scope),
		PrincipalId: principalID,
		Key:         key,
		StringList:  values,
	})
	if err != nil {
		return oops.With("scope", scope).With("key", key).Wrap(err)
	}
	return nil
}
