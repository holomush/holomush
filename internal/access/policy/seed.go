// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

// SeedPolicy defines a system-installed default policy.
type SeedPolicy struct {
	Name        string
	Description string
	DSLText     string
	SeedVersion int
}

// SeedPolicies returns the complete set of 36 seed policies (27 permit, 9 forbid).
// The initial 18 (T22) minus 2 removed command policies, plus 5 gap-fill policies (T22b: G1-G5),
// 1 phase-2 command policy, and 2 system bootstrap policies.
// Default deny behavior is provided by EffectDefaultDeny (no matching policy = denied).
// See ADR 087 for rationale on default-deny instead of explicit forbid for system properties.
//
// Note: guest restrictions for character creation and switching are enforced at the RPC layer
// (web handlers and grpc auth_handlers), not via game commands. No seed policies are needed.
//
// Note: command execute policies for plugin-provided commands (say, pose, emit, page, whisper,
// examine, help, alias, unalias, aliases, set, dig, create, describe, link, pemit) have
// migrated to plugin manifests. Only core compiled-in commands remain in seed policies.
//
// Attribute paths use fully-qualified namespace.key syntax matching the resolver's
// storage format (e.g., principal.character.roles, resource.location.id).
//
// Gap decisions (T22b):
//   - G5 (plugin commands): intentional default-deny; plugins define own policies at install time.
//   - G6 (MoveObject): intentional builder-only; players cannot move objects they don't own.
func SeedPolicies() []SeedPolicy {
	return []SeedPolicy{
		{
			Name:        "seed:player-self-access",
			Description: "Characters can read and write their own character",
			DSLText:     `permit(principal is character, action in ["read", "write"], resource is character) when { resource.character.id == principal.character.id };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:player-location-read",
			Description: "Characters can read their current location",
			DSLText:     `permit(principal is character, action in ["read"], resource is location) when { resource.location.id == principal.character.location };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:player-character-colocation",
			Description: "Characters can read co-located characters",
			DSLText:     `permit(principal is character, action in ["read"], resource is character) when { resource.character.location == principal.character.location };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:player-object-colocation",
			Description: "Characters can read co-located objects",
			DSLText:     `permit(principal is character, action in ["read"], resource is object) when { resource.object.location == principal.character.location };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:player-stream-emit",
			Description: "Characters can emit to co-located location streams",
			DSLText:     `permit(principal is character, action in ["emit"], resource is stream) when { resource.stream.has_location == true && resource.stream.location == principal.character.location };`,
			SeedVersion: 3,
		},
		{
			Name:        "seed:player-location-stream-read",
			Description: "Characters can read history of their current location stream",
			DSLText:     `permit(principal is character, action in ["read"], resource is stream) when { resource.stream.has_location == true && resource.stream.location == principal.character.location };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:player-movement",
			Description: "Characters can enter any location (restrict via forbid policies)",
			DSLText:     `permit(principal is character, action in ["enter"], resource is location);`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:player-exit-use",
			Description: "Characters can use exits for navigation",
			DSLText:     `permit(principal is character, action in ["use"], resource is exit);`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:player-basic-commands",
			Description: "Characters can execute core compiled-in and unimplemented commands",
			DSLText:     `permit(principal is character, action in ["execute"], resource is command) when { resource.command.name in ["quit", "look", "go", "who"] };`,
			SeedVersion: 5,
		},
		{
			Name:        "seed:builder-location-write",
			Description: "Builders and admins can create/modify/delete locations",
			DSLText:     `permit(principal is character, action in ["write", "delete"], resource is location) when { "builder" in principal.character.roles };`,
			SeedVersion: 3,
		},
		{
			Name:        "seed:builder-object-write",
			Description: "Builders and admins can create/modify/delete objects",
			DSLText:     `permit(principal is character, action in ["write", "delete"], resource is object) when { "builder" in principal.character.roles };`,
			SeedVersion: 3,
		},
		{
			Name:        "seed:admin-full-access",
			Description: "Admins have full access to everything",
			DSLText:     `permit(principal is character, action, resource) when { "admin" in principal.character.roles };`,
			SeedVersion: 3,
		},
		{
			Name:        "seed:property-public-read",
			Description: "Public properties readable by co-located characters",
			DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.property.visibility == "public" && principal.character.location == resource.property.parent_location };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:property-private-read",
			Description: "Private properties readable only by owner",
			DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.property.visibility == "private" && resource.property.owner == principal.character.id };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:property-admin-read",
			Description: "Admin properties readable only by admins",
			DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.property.visibility == "admin" && "admin" in principal.character.roles };`,
			SeedVersion: 3,
		},
		{
			Name:        "seed:property-owner-write",
			Description: "Property owners can write and delete their properties",
			DSLText:     `permit(principal is character, action in ["write", "delete"], resource is property) when { resource.property.owner == principal.character.id };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:property-restricted-visible-to",
			Description: "Restricted properties readable by characters in the visible_to list",
			DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.property.visibility == "restricted" && resource has property.visible_to && principal.character.id in resource.property.visible_to };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:property-restricted-excluded",
			Description: "Restricted properties denied to characters in the excluded_from list",
			DSLText:     `forbid(principal is character, action in ["read"], resource is property) when { resource.property.visibility == "restricted" && resource has property.excluded_from && principal.character.id in resource.property.excluded_from };`,
			SeedVersion: 2,
		},

		// --- Gap-fill policies (T22b) ---

		// G1: Players can read exits (target matching only; exit provider is a stub).
		{
			Name:        "seed:player-exit-read",
			Description: "Characters can read exits for navigation",
			DSLText:     `permit(principal is character, action in ["read"], resource is exit);`,
			SeedVersion: 1,
		},
		// G2: Builders can create/update/delete exits.
		{
			Name:        "seed:builder-exit-write",
			Description: "Builders and admins can create/modify/delete exits",
			DSLText:     `permit(principal is character, action in ["write", "delete"], resource is exit) when { "builder" in principal.character.roles };`,
			SeedVersion: 3,
		},
		// G3: Players can list characters in their current location (ADR #109).
		{
			Name:        "seed:player-location-list-characters",
			Description: "Characters can list other characters in their current location",
			DSLText:     `permit(principal is character, action in ["list_characters"], resource is location) when { resource.location.id == principal.character.location };`,
			SeedVersion: 2,
		},
		// G5: Players can query presence at their current location.
		{
			Name:        "seed:player-location-list-presence",
			Description: "Allow characters to query presence at their current location",
			DSLText:     `permit(principal is character, action in ["list_presence"], resource is location) when { resource.location.id == principal.character.location };`,
			SeedVersion: 6,
		},
		// G4: Scene participant access (target matching only; scene provider is a stub).
		{
			Name:        "seed:player-scene-participant",
			Description: "Characters can join and leave scenes",
			DSLText:     `permit(principal is character, action in ["write"], resource is scene);`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:player-scene-read",
			Description: "Characters can view scenes",
			DSLText:     `permit(principal is character, action in ["read"], resource is scene);`,
			SeedVersion: 1,
		},

		// --- Phase-2 command policies ---

		// All players can execute home and teleport commands.
		// Scope enforcement (home-only for default role, self-only for builder)
		// is handled in the command handlers, not here, because the ABAC engine
		// does not have access to runtime command arguments like target location.
		{
			Name:        "seed:player-teleport",
			Description: "All players can execute home and teleport commands",
			DSLText:     `permit(principal is character, action in ["execute"], resource is command) when { resource.command.name in ["teleport", "home"] };`,
			SeedVersion: 1,
		},
		// System bootstrap: setting plugins create locations and exits during server startup.
		// The subject "system:bootstrap" is used by the SettingBootstrapper, which runs
		// before any user connections. This policy grants the minimum privileges needed.
		{
			Name:        "seed:system-bootstrap-world",
			Description: "System bootstrap can create and read locations for world seeding",
			DSLText:     `permit(principal is system, action in ["read", "write"], resource is location);`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:system-bootstrap-exits",
			Description: "System bootstrap can create exits for world seeding",
			DSLText:     `permit(principal is system, action in ["read", "write"], resource is exit);`,
			SeedVersion: 1,
		},

		// --- Phase-3b audit namespace deny policies (§7.7 ABAC layer) ---

		// Decision 3 supplement: characters MUST NOT read audit.* streams.
		// Phase 3d Decision 4: ABAC at the gRPC subscribe handler is the sole
		// authoritative isolation gate; NATS-level deny rules retired (game-topic
		// NATS is single-principal by architectural design).
		{
			Name:        "seed:deny-audit-read-character",
			Description: "Characters MUST NOT read audit.* streams (§7.7 ABAC layer; sole authoritative gate per Phase 3d Decision 4 — NATS-level deny retired)",
			DSLText:     `forbid(principal is character, action in ["read"], resource is stream) when { resource.stream.name like "audit.*" };`,
			SeedVersion: 1,
		},
		// Decision 3 supplement: plugins MUST NOT read audit.* streams.
		// Phase 3d Decision 4: see above.
		{
			Name:        "seed:deny-audit-read-plugin",
			Description: "Plugins MUST NOT read audit.* streams (§7.7 ABAC layer; sole authoritative gate per Phase 3d Decision 4 — NATS-level deny retired)",
			DSLText:     `forbid(principal is plugin, action in ["read"], resource is stream) when { resource.stream.name like "audit.*" };`,
			SeedVersion: 1,
		},

		// --- Phase-5 sub-epic A TOTP-substrate audit namespace deny policies (INV-A16) ---
		//
		// Reserved subject namespace events.<game>.system.crypto_totp.<scope>.<event>
		// (sub-epic D emits; sub-epic A reserves). Parallel to the audit.* denies above.
		{
			Name:        "seed:deny-events-system-crypto-totp-read-character",
			Description: "Characters MUST NOT read events.*.system.crypto_totp.* streams (Phase 5 sub-epic A; parallel to seed:deny-audit-read-character)",
			DSLText:     `forbid(principal is character, action in ["read"], resource is stream) when { resource.stream.name like "events.*.system.crypto_totp.*" };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:deny-events-system-crypto-totp-read-plugin",
			Description: "Plugins MUST NOT read events.*.system.crypto_totp.* streams (Phase 5 sub-epic A; parallel to seed:deny-audit-read-plugin)",
			DSLText:     `forbid(principal is plugin, action in ["read"], resource is stream) when { resource.stream.name like "events.*.system.crypto_totp.*" };`,
			SeedVersion: 1,
		},

		// --- Phase-5 sub-epic D crypto-policy audit namespace deny policies ---
		//
		// Reserved subject namespace events.<game>.system.crypto_policy.<event>
		// (sub-epic D emits crypto.policy_set audit events). Parallel to the
		// crypto_totp.* denies above. The dispatchDelivery filter at
		// internal/grpc/server.go (~line 1019) drops AUDIT_ONLY events before
		// stream.Send; these seeds are the second, ABAC-layer gate so the
		// namespace is forbidden by policy and not only by display_target.
		{
			Name:        "seed:deny-events-system-crypto-policy-read-character",
			Description: "Characters MUST NOT read events.*.system.crypto_policy.* streams (Phase 5 sub-epic D; parallel to seed:deny-events-system-crypto-totp-read-character)",
			DSLText:     `forbid(principal is character, action in ["read"], resource is stream) when { resource.stream.name like "events.*.system.crypto_policy.*" };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:deny-events-system-crypto-policy-read-plugin",
			Description: "Plugins MUST NOT read events.*.system.crypto_policy.* streams (Phase 5 sub-epic D; parallel to seed:deny-events-system-crypto-totp-read-plugin)",
			DSLText:     `forbid(principal is plugin, action in ["read"], resource is stream) when { resource.stream.name like "events.*.system.crypto_policy.*" };`,
			SeedVersion: 1,
		},

		// --- Phase-5 sub-epic E broad events.*.system.* deny policies (A16 / INV-15 extension) ---
		//
		// Amendment A16 extended INV-15 to deny plugin/character subscribes to ALL
		// events.*.system.* namespaces, explicitly including the rekey audit chain
		// (events.<gameID>.system.rekey.<ct>.<cid>) added in sub-epic E.
		// These broad seeds future-proof subsequent audit chains (future sub-epics
		// that add new events.*.system.<name>.* namespaces are covered automatically).
		// The narrow per-namespace seeds above (crypto_totp, crypto_policy) remain
		// as explicit intent anchors; these broad seeds add the ABAC-layer gate
		// required by master spec §4.6 + §7.7. The dispatchDelivery AUDIT_ONLY
		// filter remains as defense-in-depth.
		{
			Name:        "seed:deny-events-system-read-character",
			Description: "Characters MUST NOT read events.*.system.* streams (Phase 5 sub-epic E; A16 / INV-15 extension — covers rekey and all future system audit namespaces)",
			DSLText:     `forbid(principal is character, action in ["read"], resource is stream) when { resource.stream.name like "events.*.system.*" };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:deny-events-system-read-plugin",
			Description: "Plugins MUST NOT read events.*.system.* streams (Phase 5 sub-epic E; A16 / INV-15 extension — covers rekey and all future system audit namespaces)",
			DSLText:     `forbid(principal is plugin, action in ["read"], resource is stream) when { resource.stream.name like "events.*.system.*" };`,
			SeedVersion: 1,
		},

		// --- Phase-5 iwzt history-scope-privacy staff override policy (I-PRIV-6) ---
		//
		// Staff and admins may bypass the per-session temporal floor (the read_unrestricted_history
		// action) but NOT the location hard-gate (ADR wxty preserves the gate for all principals).
		// Action-only seed; no resource-type guard because the gate is about the action class,
		// not the resource namespace.
		{
			Name:        "seed:staff-read-unrestricted-history",
			Description: "Staff and admins may bypass the per-session temporal floor for history reads (I-PRIV-6); location hard-gate still applies (ADR wxty)",
			DSLText:     `permit(principal is character, action in ["read_unrestricted_history"], resource) when { "staff" in principal.character.roles || "admin" in principal.character.roles };`,
			SeedVersion: 1,
		},
	}
}
