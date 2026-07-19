// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package hostcap holds the runtime-neutral holomush.plugin.host.v1 capability
// server dependencies. Both the binary (goplugin) and Lua runtimes consume the
// same capability servers through the HostCapabilities port, satisfying
// INV-PLUGIN-49 at the server level: one handler body, no runtime-specific
// surface. Each runtime supplies an adapter satisfying HostCapabilities; the
// only per-runtime difference is what the adapter wires (the binary adapter
// reads *goplugin.Host fields under its mutex; the Lua adapter wraps
// *hostfunc.Functions and recovers identity from the context).
package hostcap

import (
	"context"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/focuscontract"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/property"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/settings"
	"github.com/holomush/holomush/internal/world"
)

// PropertyDefinition is the registry property behavior the PropertyService
// server reaches through the port. Aliased to the concrete registry type so the
// server can call Get / Set without an import cycle on hostcap.
type PropertyDefinition = property.Definition

// WorldQuerier is the plugin-subject-stamped world read surface the
// PropertyService / WorldQueryService servers consume. It extends
// property.WorldQuerier with GetCharacter and GetCharactersByLocation, covering
// all four query host functions (query_location, query_character,
// query_location_characters, query_object). *hostfunc.WorldQuerierAdapter
// satisfies the full set; property.WorldQuerier satisfies the subset used by
// property.Definition.Get.
type WorldQuerier interface {
	GetLocation(ctx context.Context, id ulid.ULID) (*world.Location, error)
	GetCharacter(ctx context.Context, id ulid.ULID) (*world.Character, error)
	GetCharactersByLocation(ctx context.Context, locationID ulid.ULID, opts world.ListOptions) ([]*world.Character, error)
	GetObject(ctx context.Context, id ulid.ULID) (*world.Object, error)
}

// WorldMutator is the world write surface backing property mutation. Aliased to
// world.Mutator (the full authorized world operation set).
type WorldMutator = world.Mutator

// SessionAdmin covers the admin session operations core session.Access lacks —
// broadcast and forced disconnect. These live on the hostfunc.SessionAccess
// shim surface; the binary adapter has no consumer and returns nil (the
// SessionAdminService is registered only in the Lua capability set).
type SessionAdmin interface {
	// BroadcastSystemMessage sends a system message to all active sessions.
	BroadcastSystemMessage(ctx context.Context, message string) error
	// DisconnectSession forcibly disconnects a session with a reason.
	DisconnectSession(ctx context.Context, sessionID, reason string) error
}

// HostCapabilities is the narrow port the capability servers depend on instead
// of a concrete *goplugin.Host. The method set is exactly what the relocated
// host.v1 servers call — no more. Accessors that read mutable host state
// (AccessEngine, Auditor, EventEmitter, CommandQuerier, the token operations,
// IdentityRegistrySnapshot, OwnedResourceTypes) lock internally on the binary
// adapter; the port never exposes the host mutex.
type HostCapabilities interface {
	// AccessEngine returns the ABAC engine for Evaluate + capability checks.
	AccessEngine() types.AccessPolicyEngine
	// Auditor returns the plugin-authz auditor (nil ⇒ no audit sink).
	Auditor() pluginauthz.Auditor
	// EventEmitter returns the plugin intent emitter for EmitEvent.
	EventEmitter() plugins.PluginIntentEmitter
	// CommandQuerier returns the command-visibility querier for the command
	// registry RPCs (nil ⇒ unconfigured).
	CommandQuerier() *commandquery.Querier

	// LookupActor recovers the vouched actor AND the host-vouched owning player
	// for a dispatch identity. The binary adapter reads the host-issued emit
	// token from ctx metadata and looks it up in the token store; the Lua
	// adapter reads core.ActorFromContext(ctx) (connection-scoped, no token
	// store). pluginName is the host-established calling-plugin identity.
	LookupActor(ctx context.Context, pluginName string) (core.Actor, string, error)
	// LookupDispatch recovers the host-vouched DispatchContext bound to the
	// host-issued emit token in ctx metadata. It is the binary scope path's
	// unforgeable dispatch source: the dispatch is keyed by the host-minted token,
	// so an untrusted out-of-process plugin cannot forge the acting-character scope
	// via gRPC metadata (INV-PLUGIN-51). ok=false fails closed. The Lua adapter
	// returns (zero, false) — the Lua path recovers dispatch in-process from
	// host-stamped bufconn metadata, never via a token.
	LookupDispatch(ctx context.Context, pluginName string) (pluginauthz.DispatchContext, bool)
	// IssueEmitToken mints a host dispatch token (the binary RequestEmitToken
	// path). The Lua adapter returns an unsupported error (no Lua forgery
	// surface). The minted token vouches for actor on behalf of pluginName.
	IssueEmitToken(ctx context.Context, pluginName string, actor core.Actor) (string, error)
	// IdentityRegistrySnapshot returns the plugin identity registry snapshot used
	// to stamp the self-token actor (nil ⇒ no registry; Lua may return nil).
	IdentityRegistrySnapshot() plugins.IdentityRegistry
	// OwnedResourceTypes returns the ABAC resource types the named plugin owns,
	// keyed by type name (nil/empty ⇒ none, e.g. Lua).
	OwnedResourceTypes(pluginName string) map[string]bool

	// GameSettings / PlayerSettings / CharacterSettings back GetSetting /
	// SetSetting (nil ⇒ settings not configured, fail closed).
	GameSettings() settings.GameSettings
	PlayerSettings() settings.PlayerSettingsStore
	CharacterSettings() settings.CharacterSettingsStore

	// FocusCoordinator backs the FocusService RPCs (nil ⇒ not configured).
	FocusCoordinator() focuscontract.Coordinator
	// GameID returns the game ID used to qualify a domain-relative stream
	// reference before the QueryStreamHistory instance-level ABAC check
	// ("" ⇒ unqualifiable ⇒ the gate fails closed; holomush-xakba).
	GameID() string
	// HistoryReader backs QueryStreamHistory (nil ⇒ not configured).
	HistoryReader() plugins.HistoryReader
	// ReadbackDecryptor backs DecryptOwnAuditRows (nil ⇒ not configured).
	ReadbackDecryptor() plugins.ReadbackDecryptor

	// StreamRegistry backs the AddSessionStream / RemoveSessionStream
	// (stream.subscription) capability RPCs (nil ⇒ not configured ⇒ the served
	// handler fails closed with Internal). Both runtimes reach the same host
	// SessionStreamRegistry through this accessor (plugin-runtime-symmetry).
	StreamRegistry() plugins.StreamRegistry
	// OwnedEmitDomains returns the manifest-declared emit domains of the named
	// plugin — the owned-namespace fence input for AuthorizeStreamSubscribe
	// (nil/empty ⇒ the plugin owns no emit namespaces ⇒ the fence rejects every
	// stream contribution, fail closed). Host-derived; NOT trusted from the
	// request. The binary host reads its loaded manifest; the Lua adapter has no
	// per-plugin manifest emit surface and returns nil (Lua session-stream
	// contribution via the served capability is not wired — see the adapter doc).
	OwnedEmitDomains(pluginName string) []string

	// PropertyDefinition resolves a registry property by name (property server).
	// The binary host has no property registry and returns (nil, false).
	PropertyDefinition(name string) (PropertyDefinition, bool)
	// WorldQuerier returns the plugin-subject-stamped world read adapter for the
	// named plugin. The binary host has no world surface and returns nil.
	WorldQuerier(pluginName string) WorldQuerier
	// WorldMutator returns the world write surface. The binary host has no world
	// surface and returns nil.
	WorldMutator() WorldMutator
	// SessionAccess returns the session read/update surface. The binary host has
	// no session surface and returns nil.
	SessionAccess() session.Access
	// SessionAdmin returns the admin session surface (broadcast/disconnect). The
	// binary host has no consumer and returns nil.
	SessionAdmin() SessionAdmin
}
