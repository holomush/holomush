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

// SeedPolicies returns the complete set of 50 seed policies (41 permit, 9 forbid).
// The initial 18 (T22) minus 2 removed command policies, plus 5 gap-fill policies (T22b: G1-G5),
// 1 phase-2 command policy, 2 system bootstrap policies, and 1 plugin host-capability
// scope policy (eykuh.3; world.mutation own-location), 11 holomush-kplrr plugin
// host-capability default-permit seeds, 1 holomush-xakba plugin instance-level stream read,
// and 1 character-directory seed (INV-ACCESS-9).
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
		// NOTE: the former "seed:player-scene-participant" (write) and
		// "seed:player-scene-read" seeds were removed in holomush-8m01u and
		// holomush-sjtlz respectively. Both were UNCONDITIONAL
		// permit(character, <action>, scene) stubs that — because the engine
		// OR-combines matching permits — subsumed and nullified the core-scenes
		// plugin's membership-conditioned policies (plugins/core-scenes/
		// plugin.yaml), so a non-participant could emit IC/OOC into any scene
		// (write) or `scene info` any scene's metadata (read). Scene
		// authorization now lives solely in the plugin policies
		// (write-scene-as-participant; read-scene-as-participant /
		// read-scene-as-invitee / read-open-scene), evaluated against the
		// plugin's SceneResolver attributes. Existing deployments have the
		// stale rows disabled by migrations 000047 (write) and 000048 (read).

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

		// --- Phase-5 sub-epic A TOTP-substrate audit namespace deny policies (INV-ACCESS-8) ---
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

		// --- Phase-5 sub-epic E broad events.*.system.* deny policies (A16 / INV-ACCESS-7 extension) ---
		//
		// Amendment A16 extended INV-ACCESS-7 to deny plugin/character subscribes to ALL
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
			Description: "Characters MUST NOT read events.*.system.* streams (Phase 5 sub-epic E; A16 / INV-ACCESS-7 extension — covers rekey and all future system audit namespaces)",
			DSLText:     `forbid(principal is character, action in ["read"], resource is stream) when { resource.stream.name like "events.*.system.*" };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:deny-events-system-read-plugin",
			Description: "Plugins MUST NOT read events.*.system.* streams (Phase 5 sub-epic E; A16 / INV-ACCESS-7 extension — covers rekey and all future system audit namespaces)",
			DSLText:     `forbid(principal is plugin, action in ["read"], resource is stream) when { resource.stream.name like "events.*.system.*" };`,
			SeedVersion: 1,
		},

		// --- Phase-5 iwzt history-scope-privacy staff override policy (INV-PRIVACY-6) ---
		//
		// Staff and admins may bypass the per-session temporal floor (the read_unrestricted_history
		// action) but NOT the location hard-gate (ADR wxty preserves the gate for all principals).
		// Action-only seed; no resource-type guard because the gate is about the action class,
		// not the resource namespace.
		{
			Name:        "seed:staff-read-unrestricted-history",
			Description: "Staff and admins may bypass the per-session temporal floor for history reads (INV-PRIVACY-6); location hard-gate still applies (ADR wxty)",
			DSLText:     `permit(principal is character, action in ["read_unrestricted_history"], resource) when { "staff" in principal.character.roles || "admin" in principal.character.roles };`,
			SeedVersion: 1,
		},

		// --- Plugin host-capability scope policies (eykuh.3; INV-PLUGIN-50) ---
		//
		// world.mutation own-location: a plugin (subject plugin:<name>) may write
		// to a location only when that location is the acting character's dispatch
		// location. The host-capability interceptor (internal/plugin/hostcap) calls
		// pluginauthz.EvaluateCapabilityAccess for the scope-eligible CreateExit /
		// CreateObject methods, passing the extracted location as the resource
		// (location:<id>) and the host-vouched acting-character location as the
		// caller action attribute dispatch_location. The `action` namespace is not
		// schema-registered (no AttributeProvider owns it), so dispatch_location is
		// a free caller-overlay attribute the engine injects into bags.Action
		// (engine.go: caller attributes overlay bags.Action). Manifest declaration
		// is necessary but not sufficient — this default-deny seed is the operator
		// policy that authorizes the declared capability (INV-PLUGIN-50).
		{
			Name:        "seed:plugin-world-mutation-own-location",
			Description: "Plugins may write to a location only when it is the acting character's dispatch location (world.mutation own-location scope; INV-PLUGIN-50)",
			DSLText:     `permit(principal is plugin, action in ["write"], resource is location) when { resource.location.id == action.dispatch_location };`,
			SeedVersion: 1,
		},

		// --- Plugin host-capability default-permit seeds (holomush-kplrr; INV-PLUGIN-50) ---
		//
		// Every declared non-exempt capability is now authorized by a default-deny
		// ABAC decision in the host-capability interceptor (internal/plugin/hostcap):
		// declaration is necessary but NOT sufficient. NON-scope-eligible methods
		// (kv, settings, world.query, property, session, focus, stream, audit, eval,
		// and the non-scoped world.mutation CreateLocation) are evaluated at the
		// capability TYPE level — the interceptor passes the wildcard sentinel
		// resource "<type>:*". These seeds default-permit those type-level calls so a
		// declared, undifferentiated capability succeeds; an operator MAY layer a
		// `forbid(principal is plugin, ... resource is <type>)` on top to deny a
		// declared capability (the operator-override lever INV-PLUGIN-50 promises).
		//
		// They match the wildcard sentinel EXACTLY (`resource == "<type>:*"`), never
		// by type (`resource is <type>`): a type-match permit on `location` would
		// also match the SCOPED CreateExit/CreateObject calls (resource
		// "location:<id>") and silently defeat the own-location seed above. Exact
		// wildcard match authorizes only the non-scoped type-level calls; scoped
		// instance writes remain gated by their own-location seed. Every served
		// non-exempt capability resource type MUST carry one of these — absence
		// fails the call closed (guarded by TestEverySeededCapabilityResourceHasDefaultPermit).
		{
			Name:        "seed:plugin-cap-eval",
			Description: "Default-permit a declared plugin's policy-evaluation capability at the type level (INV-PLUGIN-50; operator MAY forbid)",
			DSLText:     `permit(principal is plugin, action in ["evaluate"], resource == "policy:*");`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:plugin-cap-settings",
			Description: "Default-permit a declared plugin's settings capability at the type level (INV-PLUGIN-50; operator MAY forbid)",
			DSLText:     `permit(principal is plugin, action in ["read", "write"], resource == "setting:*");`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:plugin-cap-kv",
			Description: "Default-permit a declared plugin's kv capability at the type level (INV-PLUGIN-50; operator MAY forbid)",
			DSLText:     `permit(principal is plugin, action in ["read", "write"], resource == "kv:*");`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:plugin-cap-world-location",
			Description: "Default-permit a declared plugin's type-level location capability: world.query location reads AND the non-scoped CreateLocation write (creating a NEW location, no pre-existing operand). Scoped writes to EXISTING locations (CreateExit/CreateObject) stay gated by seed:plugin-world-mutation-own-location — this exact-wildcard permit cannot match their location:<id> resource (INV-PLUGIN-50)",
			DSLText:     `permit(principal is plugin, action in ["read", "write"], resource == "location:*");`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:plugin-cap-world-query-character",
			Description: "Default-permit a declared plugin's world.query reads of characters at the type level (INV-PLUGIN-50; operator MAY forbid)",
			DSLText:     `permit(principal is plugin, action in ["read"], resource == "character:*");`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:plugin-cap-world-query-object",
			Description: "Default-permit a declared plugin's world.query reads of objects at the type level (INV-PLUGIN-50; operator MAY forbid)",
			DSLText:     `permit(principal is plugin, action in ["read"], resource == "object:*");`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:plugin-cap-property",
			Description: "Default-permit a declared plugin's property capability at the type level (INV-PLUGIN-50; operator MAY forbid)",
			DSLText:     `permit(principal is plugin, action in ["read", "write"], resource == "property:*");`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:plugin-cap-session",
			Description: "Default-permit a declared plugin's session capability (read/list/write, incl. session.admin) at the type level (INV-PLUGIN-50; operator MAY forbid)",
			DSLText:     `permit(principal is plugin, action in ["read", "list", "write"], resource == "session:*");`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:plugin-cap-focus",
			Description: "Default-permit a declared plugin's focus capability at the type level (INV-PLUGIN-50; operator MAY forbid)",
			DSLText:     `permit(principal is plugin, action in ["read", "write"], resource == "focus:*");`,
			SeedVersion: 1,
		},
		// Stream authorization is two-layer (holomush-kplrr + holomush-xakba):
		//   1. seed:plugin-cap-stream below is the TYPE-level capability gate the
		//      interceptor evaluates (resource "stream:*"). The system-namespace
		//      forbid (seed:deny-events-system-read-plugin, when resource.stream.name
		//      like "events.*.system.*") cannot fire here: its WHEN clause evaluates
		//      resource.stream.name to the literal "*" (StreamProvider on a wildcard
		//      id), and "*" like "events.*.system.*" is false.
		//   2. seed:plugin-stream-read (further below) is the INSTANCE-level read
		//      permit the plugin stream.history handler evaluates against the CONCRETE
		//      stream name (resource "stream:<name>"); there the audit/crypto/system
		//      forbids match real names and override, so a plugin can read non-system
		//      streams but not forbidden namespaces.
		{
			Name:        "seed:plugin-cap-stream",
			Description: "Default-permit a declared plugin's stream capability at the type level (INV-PLUGIN-50; operator MAY forbid)",
			DSLText:     `permit(principal is plugin, action in ["read", "write"], resource == "stream:*");`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:plugin-cap-audit",
			Description: "Default-permit a declared plugin's audit capability (DecryptOwnAuditRows) at the type level (INV-PLUGIN-50; operator MAY forbid)",
			DSLText:     `permit(principal is plugin, action in ["read"], resource == "audit:*");`,
			SeedVersion: 1,
		},

		// Instance-level plugin stream read (holomush-xakba). Type-match (resource
		// is stream) so it matches a CONCRETE stream:<name>, unlike the exact-wildcard
		// capability gate above. The plugin stream.history handler
		// (internal/plugin/hostcap/servers.go::QueryStreamHistory) evaluates the
		// concrete stream against this permit; the audit/crypto_totp/crypto_policy/
		// system forbids (deny-overrides) carve out the forbidden namespaces. This is
		// the plugin analogue of seed:player-location-stream-read, minus the
		// co-location condition (plugins have no location). Read-only.
		//
		// Stream WRITES (stream.subscription: AddSessionStream/RemoveSessionStream)
		// are permitted at the instance level by seed:plugin-stream-subscribe below.
		// The forbidden-namespace + owned-namespace protection on the write path is
		// NOT these read-only forbids (they are all action in ["read"] and do NOT
		// carve back a write permit) — it is the IN-HANDLER pluginauthz.
		// AuthorizeStreamSubscribe rejection (review R2-B), which fences a plugin to
		// its own declared emit domains and rejects system/audit/crypto directly.
		{
			Name:        "seed:plugin-stream-read",
			Description: "Permit a declared plugin to read a concrete stream's history; audit/crypto/system forbids override forbidden namespaces (INV-PLUGIN-50; holomush-xakba)",
			DSLText:     `permit(principal is plugin, action in ["read"], resource is stream);`,
			SeedVersion: 1,
		},

		// Instance-level plugin stream write (stream.subscription; HIGH-3). Type-match
		// (resource is stream) so it matches a CONCRETE stream:<name>. This is the
		// write analogue of seed:plugin-stream-read and is the type/instance capability
		// grant ONLY. It is INTENTIONALLY broad: the existing audit/system/crypto
		// forbids are all action in ["read"] and do NOT restrict it. The load-bearing
		// forbidden/owned-namespace control is the in-handler
		// pluginauthz.AuthorizeStreamSubscribe fence (review R2-B / R3-A), which is
		// independently tested and denies cross-namespace subscription EVEN WITH this
		// permit active. Operators MAY add write-specific forbids as defense-in-depth.
		{
			Name:        "seed:plugin-stream-subscribe",
			Description: "Permit a declared plugin to mutate a session's stream subscriptions (stream.subscription); the in-handler AuthorizeStreamSubscribe fence, not read-only forbids, bounds it to owned namespaces (INV-PLUGIN-50; HIGH-3)",
			DSLText:     `permit(principal is plugin, action in ["write"], resource is stream);`,
			SeedVersion: 1,
		},

		// --- Background-job fixture: instance-scoped character write (AUTHZ-02) ---
		//
		// WHAT THIS SEED PROVES. A background job (subject job:<name>) may write
		// exactly the character its triggering event names — and no other. It is
		// the phase tracer's grant: the `when` clause binds the caller-supplied
		// provenance (world.JobCaller's action.job.* triple) against the
		// resolver-stamped resource.id, so a handler that derives the wrong
		// aggregate is denied rather than corrupting it.
		//
		// `fixture` IS A FIXTURE. No job by that name is ever registered in
		// production, so the liveness gate in attribute.JobProvider resolves it to
		// nothing and this permit cannot match: principal.job.name is absent, and
		// a missing attribute is false for every operator (ADR holomush-iv43).
		// Grants for the REAL consumers — job:retirement, job:activity-flush — are
		// deliberately deferred to Phase 3 (D-52); none shipped here.
		//
		// THE `when` CLAUSE IS MANDATORY, NOT DECORATION. parseEntityType matches
		// the PREFIX ONLY, so a bare `permit(principal is job, ...)` would be a
		// blanket grant to every present and future job — exactly the failure D-48
		// created a disjoint namespace to avoid.
		//
		// BOTH action.job.* CONJUNCTS ARE LOAD-BEARING (D-54). trigger_subject
		// bounds WHAT the job may touch; trigger_event_type bounds WHAT CAUSED the
		// write. The event-type half reads as redundant against the subject match
		// and is not: dropping it would grant this job every event type its
		// subscription happens to deliver for that aggregate, which is how a job
		// on a broad filter acquires broader authority than intended.
		//
		// THE CAPABILITY-CLASS CONJUNCT IS THE SECOND GATE (D-51). A job declares
		// its write kinds in Go at registration; that declaration NARROWS, and
		// this seed GRANTS. Both must pass, and the declaration alone authorizes
		// nothing unless a seed reads it — which is exactly what
		// `principal.job.writes.containsAll(["character"])` does. Delete it and
		// the declaration becomes decoration.
		//
		// trigger_subject is the BARE AGGREGATE ULID — byte-identical to
		// bags.Resource["id"], which is the substring after the first ':' of the
		// resource ref. Not a dotted NATS subject, not a prefixed entity ref:
		// either compares unequal and every job write silently default-denies.
		{
			Name:        "seed:job-fixture-instance-scoped",
			Description: "A live background job may write only the character its triggering event names (AUTHZ-02 fixture; no production consumer — real job grants are Phase 3's per D-52)",
			DSLText:     `permit(principal is job, action in ["write"], resource is character) when { principal.job.name == "fixture" && principal.job.writes.containsAll(["character"]) && action.job.trigger_event_type == "fixture_triggered" && action.job.trigger_subject == resource.id };`,
			SeedVersion: 1,
		},

		// --- Character directory (INV-ACCESS-9) ---
		//
		// Any authenticated character (registered or guest) may list the server-wide
		// character directory (id + name only). Connection/online state requires a
		// separate, more-restrictive permission (not part of this seed). The resource
		// is the singleton "character_directory:all" — target-only match, no `when`
		// clause needed. Evaluated by CoreService.ListAllCharacters.
		{
			Name:        "seed:directory-list-characters",
			Description: "Any authenticated character (incl. guest) may list the character directory (names only)",
			DSLText:     `permit(principal is character, action in ["list_character_directory"], resource is character_directory);`,
			SeedVersion: 1,
		},

		// --- Profile visibility: viewer-tier floors (§8.2.1, §8.6) ---
		//
		// TERM A of 01-SPEC §8.5.1's conjunction. A profile attribute publishes
		// only when BOTH decisions permit: the viewer clears the attribute's
		// tier floor (this family), AND the underlying entity_properties row's
		// own visibility / visible_to / excluded_from permits the read (the
		// row-keyed family, further below). The caller ANDs the two. This family
		// MUST NOT be dropped into the same evaluation as the row-keyed one:
		// combineDecisions (engine.go) returns the FIRST satisfied permit, so
		// two permits in one evaluation are ADDITIVE, and a row carrying
		// visibility='private' or 'admin' would publish to every viewer that
		// clears its name's floor.
		//
		// THE ACTION TOKEN IS THE SEPARATOR. The tier floors carry the action
		// "read_profile_attribute"; the row-keyed twins carry "read". If both
		// carried "read" against the same property:<id> resource, each
		// evaluation would match BOTH families and the conjunction would
		// silently reduce to the additive shape §8.5.1.1 exists to prevent.
		// Actions are free-form strings in this engine and the corpus already
		// carries domain-specific tokens (list_character_directory,
		// read_unrestricted_history) — this is house style, not a new mechanism.
		// A future reader seeing two actions on one resource should NOT unify
		// them.
		//
		// SET MEMBERSHIP, NEVER ORDINAL COMPARISON (§8.2.1). The clearing tests
		// below are transcribed verbatim. The DSL's only string ordering is Go
		// byte order (compareStrings, dsl/evaluator.go); the three v0.13 tokens
		// sort in ladder order by alphabetical accident, and each of
		// "spectator", "unverified" and "visitor" sorts lexicographically ABOVE
		// "player" — so a >= test would hand a newly appended fourth rung the
		// highest clearance in the system on the day the token is added, with no
		// policy edit anywhere. No numeric rank attribute either: that
		// reintroduces the ordering in a second place that can drift.
		//
		// WHOLE-STRING NAME MATCHING, NO GLOB / PREFIX / WILDCARD / CATCH-ALL
		// (§8.6 totality rule). A name appearing in no list is DENIED, not
		// defaulted. The earlier residual-`guest` formulation was wrong in the
		// permissive direction: §7.1 makes a new profile field an INSERT, so the
		// namespace is open and seed:property-owner-write lets an owner write
		// any property on their own character — a name-keyed residual permit is
		// therefore a permit for a name nobody has ever considered, which is
		// MORE permissive than the engine's own default-deny. Adding a governed
		// field means adding it to a list here in the same change.
		//
		// The media names are eleven exact bytes (§7.3), enumerated literally
		// below; gallery index 10 is not a member and is denied.
		//
		// ONE POLICY PER RUNG THAT HAS AT LEAST ONE SEEDED §8.6 MEMBER — TWO,
		// NOT THREE. This is a RECORDED DEVIATION from locked decision D-03,
		// which as originally written mandated three (one per rung). §8.6's
		// seeded-default column places every governed row at `anonymous` or
		// `guest`, so the `player` rung has NO seeded member; and the DSL's list
		// grammar is `'[' @@ (',' @@)* ']'` (dsl/ast.go), which requires at
		// least one literal — an empty `in []` does not parse. A third policy
		// cannot be written at all without inventing a member, which would be
		// worse. The shape D-03 mandates (one policy per rung, literal name
		// lists, set-membership clearing) is unchanged; only the count follows
		// from the seeded data. 02-CONTEXT.md's D-03 carries the amendment.
		//
		// The clearing test the absent `player`-rung policy would carry, so the
		// day it becomes writable it does not have to be re-derived:
		//
		//	principal.viewer.tier in ["player"]
		//
		// TestAPlayerRungTierFloorPolicyIsRequiredExactlyWhenSpec86SeedsAName is
		// green while that rung has no seeded member and turns RED the moment
		// one appears.
		{
			Name:        "seed:profile-tier-floor-anonymous",
			Description: "Term A: profile attributes seeded at the anonymous floor are readable by every viewer rung (§8.2.1, §8.6)",
			DSLText:     `permit(principal is viewer, action in ["read_profile_attribute"], resource is property) when { principal.viewer.tier in ["anonymous", "guest", "player"] && resource.property.name in ["profile.pronouns"] };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:profile-tier-floor-guest",
			Description: "Term A: profile attributes seeded at the guest floor are readable by guest and player viewers (§8.2.1, §8.6)",
			DSLText:     `permit(principal is viewer, action in ["read_profile_attribute"], resource is property) when { principal.viewer.tier in ["guest", "player"] && resource.property.name in ["profile.rumors", "profile.currently", "profile.rp_preferences", "profile.timezone", "profile.concept", "profile.species", "profile.age", "profile.faction", "profile.appearance", "profile.personality", "profile.biography", "profile.image.primary", "profile.image.gallery.00", "profile.image.gallery.01", "profile.image.gallery.02", "profile.image.gallery.03", "profile.image.gallery.04", "profile.image.gallery.05", "profile.image.gallery.06", "profile.image.gallery.07", "profile.image.gallery.08", "profile.image.gallery.09"] };`,
			SeedVersion: 1,
		},

		// Profile reachability (§8.4.2). A facet ABOVE the fields: it governs
		// whether the profile resolves at all, and it is evaluated FIRST and
		// INDEPENDENTLY of every per-field result — a DENY returns §8.7's
		// not-found-equivalent and no per-field evaluation runs.
		//
		// It reads NO resource attributes, so it needs no `profile`-namespace
		// AttributeProvider: `resource is profile` is a target match on the
		// parsed resource type (parseEntityType, engine.go). Raising the
		// reachability floor is an edit to this clearing set and nothing else.
		//
		// Not `character:<id>` with action `read`: that pair is already
		// permitted for character subjects by seed:admin-full-access and
		// seed:player-character-colocation, and it means "may read the character
		// entity", which is a different question from "does this character's
		// profile resolve on the web".
		{
			Name:        "seed:profile-reachable",
			Description: "A viewer at any rung may reach a character's profile (§8.4.2 reachability floor: anonymous)",
			DSLText:     `permit(principal is viewer, action in ["read"], resource is profile) when { principal.viewer.tier in ["anonymous", "guest", "player"] };`,
			SeedVersion: 1,
		},

		// --- Profile visibility: viewer-flavored row-keyed reads (D-01, §8.5.1) ---
		//
		// TERM B of §8.5.1's conjunction, for the viewer path. Each entry is the
		// corresponding shipped seed:property-* policy with `principal is
		// character` replaced by `principal is viewer` and every
		// principal.character.* reference re-expressed against the vocabulary
		// plan 02-13 supplies, evaluating the SAME visibility / visible_to /
		// excluded_from row semantics. The colocation clause is dropped: the
		// viewer path is a web read, and colocation has no meaning for a viewer
		// with no character. The tier floor is term A and is a SEPARATE
		// evaluation, ANDed by the caller.
		//
		// §8.5.1.1's OPTION 2 IS REJECTED (D-02). "Term B evaluates against a
		// co-located character subject and DENIES where one does not exist"
		// violates §8.8 / INV-PRIVACY-10 on the anonymous rung: an anonymous
		// viewer has no character, profile.pronouns is an entity_properties row
		// seeded at the anonymous floor, so term B would deny it and a reachable
		// profile would carry `name` without `pronouns`. That failure is CLOSED,
		// so no test in the suite catches it — it surfaces as "the public
		// profile looks bare", which §8.5.1.1 records as the symptom that
		// provokes the forbidden repair of dropping term B.
		//
		// THE IDENTITY MAPPING, transcribed (cross-AI review 02-07 HIGH). The
		// shipped row semantics are CHARACTER-keyed; a `viewer:` subject is
		// PLAYER-flavored. Comparing a player id against character ids never
		// matches, and a non-matching key evaluates FALSE
		// (dsl/evaluator.go), so a twin keyed on the character-keyed field would
		// make every private / restricted / admin field permanently invisible —
		// silently, fail-closed, with no error and no failing behavioural test:
		//
		//	resource.property.owner == principal.character.id
		//	  → resource.property.owner_player_id == principal.viewer.player_id
		//	principal.character.id in resource.property.visible_to
		//	  → principal.viewer.player_id in resource.property.visible_to_players
		//	principal.character.id in resource.property.excluded_from
		//	  → principal.viewer.player_id in resource.property.excluded_from_players
		//	"admin" in principal.character.roles
		//	  → "admin" in principal.viewer.roles
		//
		// player_id IS OMITTED ON THE ANONYMOUS RUNG, so every identity-bearing
		// twin is unsatisfiable there rather than matching an empty peer. That
		// is the omit-don't-sentinel guarantee from plan 02-03
		// (.claude/rules/abac-providers.md), not an accident of the policy text.
		//
		// THE DERIVED PEERS COME FROM THE ROW, NEVER FROM THE CALLER. A peer
		// computed from the requesting subject would make every row match its
		// own reader — a total authorization bypass that reads as a working
		// feature.
		//
		// WHY DERIVED PEERS EXIST AT ALL: the DSL cannot intersect two attribute
		// lists. `X in Y` needs a scalar left operand, and containsAll /
		// containsAny take literal needles only. So the character→player
		// relation is resolved once, server-side, in PropertyProvider — where
		// ParentLocationResolver is the precedent — and the policy compares
		// player to player. Seeing `owner` and `owner_player_id` side by side is
		// not duplication; deleting either breaks one of the two families.
		//
		// THE PEERS ARE NOT A PLAIN PLAYER UNION, AND THE DSL TEXT CANNOT SHOW
		// IT (02-CONTEXT.md D-27). owner_player_id and visible_to_players hold a
		// player only when EVERY character of that player appears in the source
		// field (the ALL direction, permit side); excluded_from_players holds a
		// player when ANY does (the ANY direction, forbid side). Each peer leans
		// the way that CANNOT WIDEN ITS OWN POLICY'S EFFECT. The plain union was
		// proposed for both sides and DECLINED for the permit side, because it
		// broadens a grant made to a NAMED character into a grant to the human
		// behind it. These twins read identically to their character-flavored
		// originals, so this comment is the only place a reviewer asking "does
		// the viewer path widen access across a player's alternate characters?"
		// can find the answer.
		//
		// seed:property-owner-write gets NO twin. D-01 is explicit: a `viewer:`
		// subject must never hold a write permit. Its absence is a decision.
		{
			Name:        "seed:viewer-property-public-read",
			Description: "Term B (viewer path): public properties readable by any viewer, with no colocation clause",
			DSLText:     `permit(principal is viewer, action in ["read"], resource is property) when { resource.property.visibility == "public" };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:viewer-property-private-read",
			Description: "Term B (viewer path): private properties readable only by the owning player (derived peer, D-27 ALL direction)",
			DSLText:     `permit(principal is viewer, action in ["read"], resource is property) when { resource.property.visibility == "private" && resource.property.owner_player_id == principal.viewer.player_id };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:viewer-property-admin-read",
			Description: "Term B (viewer path): admin properties readable only by admins, resolved per player (§10.5)",
			DSLText:     `permit(principal is viewer, action in ["read"], resource is property) when { resource.property.visibility == "admin" && "admin" in principal.viewer.roles };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:viewer-property-restricted-visible-to",
			Description: "Term B (viewer path): restricted properties readable by players in the derived visible_to peer (D-27 ALL direction)",
			DSLText:     `permit(principal is viewer, action in ["read"], resource is property) when { resource.property.visibility == "restricted" && resource has property.visible_to_players && principal.viewer.player_id in resource.property.visible_to_players };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:viewer-property-restricted-excluded",
			Description: "Term B (viewer path): restricted properties denied to players in the derived excluded_from peer (D-27 ANY direction)",
			DSLText:     `forbid(principal is viewer, action in ["read"], resource is property) when { resource.property.visibility == "restricted" && resource has property.excluded_from_players && principal.viewer.player_id in resource.property.excluded_from_players };`,
			SeedVersion: 1,
		},

		// --- The seed:profile-public-read widening (PROFILE-11, D-10, D-11) ---
		//
		// seed:property-public-read minus its colocation clause, guarded on
		// parent_type so the widening reaches character rows only. Per D-10 it
		// covers ANY public parent_type='character' row and is NOT scoped to
		// §8.6's enumeration — web exposure stays bounded regardless, because a
		// name in no §8.6 row is denied by term A.
		//
		// It is a NEW ADDITIVE PERMIT. seed:player-character-colocation and
		// seed:property-public-read are left untouched at their existing
		// SeedVersion: permits combine disjunctively (combineDecisions,
		// engine.go), so adding a permit widens without editing a shipped policy
		// and without an upgrade path that could collide with an
		// admin-customized row.
		//
		// THE GRID-PATH CONSEQUENCE IS INTENDED (D-11). An off-location
		// character in-game can now read public character properties it
		// previously could not, and the tier floor does not gate that path.
		// `public` means public on the grid as well as the web; the colocation
		// restriction was the anomaly. Plan 02-10's audit exists to find rows
		// that were relying on colocation as de-facto privacy, and THE FIX FOR
		// ANY SUCH ROW IS TO CHANGE THAT ROW'S `visibility`, NEVER TO NARROW THE
		// POLICY. That remedy is available precisely because entity_properties
		// HAS a visibility column. The phase MUST NOT merge before that audit's
		// result is recorded.
		//
		// D-13: the in-world description stays at the `anonymous` floor — a
		// deliberate divergence from strict grid-parity, already recorded in
		// §8.11.
		//
		// REJECTED: a single seed:profile-public-read with a BARE `resource`
		// clause. It would also match location:, object: and stream:.
		//
		// NOT SHIPPED IN PHASE 2 — the character half, deferred by D-29.
		// An earlier revision of this family added, with no `when` clause:
		//
		//	permit(principal is character, action in ["read"], resource is character);
		//
		// Do not write it, and do not write a narrowed variant: Phase 2 needs no
		// character-typed resource permit at all. Reasons, so the absence reads
		// as a decision rather than an oversight:
		//
		//   - It gates world.Service.GetCharacter (internal/world/service.go) on
		//     exactly that action/resource pair, and that RPC's projection
		//     characterToProto (internal/world/grpc_server.go) returns Id,
		//     PlayerId, Name, Description and LocationId. It was justified as
		//     carrying characters.description; it carries the whole
		//     CharacterInfo.
		//   - Its principal test is `principal is character`, and guests HAVE
		//     characters — so every ephemeral guest could enumerate
		//     alt-to-player ownership and live grid position for the entire
		//     roster.
		//   - It was justified by PROFILE-10a, which is not in this phase's
		//     requirement set (IDENT-06, IDENT-07, IDENT-08, IDENT-09,
		//     PROFILE-11, EXT-07), and Phase 2 ships no RPCs, so nothing here
		//     consumes it.
		//   - It is NOT an instance of D-10/D-11 and MUST NOT be justified by
		//     citing them. Those govern entity_properties rows, which carry a
		//     visibility column; D-11's mandated remedy — change the row's
		//     visibility — is what makes that widening acceptable. The
		//     characters table (000001_baseline.sql) is id, player_id, name,
		//     description, location_id, created_at, with NO visibility column,
		//     and no plan in this phase adds one. The escape hatch does not
		//     exist for that resource.
		//
		// It moves to Phase 4, to land with the projection narrowing that makes
		// it safe. CONSEQUENCE, stated so no plan silently depends on the
		// permit: PROFILE-11's characters.description half is NOT discharged in
		// Phase 2. The entry below discharges the entity_properties half alone.
		{
			Name:        "seed:profile-public-read-property",
			Description: "Public properties on a character are readable off-location (PROFILE-11 widening; D-10/D-11)",
			DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.property.visibility == "public" && resource.property.parent_type == "character" };`,
			SeedVersion: 1,
		},

		// --- Admin sections (EXT-07, §10.4, §10.5) ---
		//
		// SCOPED BY RESOURCE TYPE, NOT BY ENUMERATED ID. `resource is
		// admin_section` is a target match on the parsed resource type, so all
		// seven registered sections AND EVERY FUTURE SECTION are covered at zero
		// additional policy cost (EXT-07) — an eighth section needs no policy
		// edit. seed:directory-list-characters is the in-tree precedent that an
		// underscore-bearing resource type matches. Type scoping is also what
		// makes D-06's gate-first-then-distinguish ordering work: an
		// UNREGISTERED section id is still covered by this policy, so a caller
		// the gate denies gets DENY_ADMIN_SECTION whether or not the id exists,
		// and only a caller the gate permits can ever see
		// DENY_ADMIN_SECTION_UNREGISTERED. An id-enumerated policy would leak
		// the registry through that difference.
		//
		// PLAYER-FLAVORED PRINCIPAL. §10.5's verdict is normative — the admin
		// gate is evaluated PER PLAYER — and it is what the tree already does at
		// every site the check exists: PostgresRoleStore.PlayerHasRole reads
		// roles per player, and the shipped operator path calls
		// AssertOperatorAdmin with a player id
		// (internal/admin/auth/operator_admin.go). A character-flavored
		// principal would put two different answers to "is this caller an admin"
		// over one table, the operator socket saying yes and the web saying no
		// for the same human at the same moment.
		//
		// BOTH ACTIONS ON THE SECTION RESOURCE: `read` to reach a section,
		// `write` for a mutation within it (§10.4).
		{
			Name:        "seed:admin-section-access",
			Description: "Admin players may read and write every admin section, scoped by resource type (EXT-07, §10.4, §10.5)",
			DSLText:     `permit(principal is player, action in ["read", "write"], resource is admin_section) when { "admin" in principal.player.roles };`,
			SeedVersion: 1,
		},
	}
}
