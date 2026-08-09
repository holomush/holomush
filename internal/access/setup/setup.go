// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package setup provides production wiring for the ABAC access control stack.
package setup

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/lifecycle"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/world"
)

// ABACStack holds all ABAC subsystem components constructed by BuildABACStack.
type ABACStack struct {
	Engine          types.AccessPolicyEngine
	Cache           *policy.Cache
	Poller          *policy.Poller
	HealthTracker   *lifecycle.HealthTracker
	PolicyStore     *policystore.PostgresStore
	Resolver        *attribute.Resolver
	AuditLogger     *audit.Logger
	PolicyInstaller *plugins.PolicyInstaller
	PluginProvider  *attribute.PluginProvider
	JobProvider     *attribute.JobProvider
	sqlDB           *sql.DB
}

// Close shuts down the ABAC stack, flushing audit logs and closing the SQL connection.
func (s *ABACStack) Close() error {
	var firstErr error
	if s.AuditLogger != nil {
		if err := s.AuditLogger.Close(); err != nil {
			firstErr = err
		}
	}
	if s.sqlDB != nil {
		if err := s.sqlDB.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ABACConfig holds configuration for building the ABAC stack.
type ABACConfig struct {
	Pool          *pgxpool.Pool
	CharacterRepo world.CharacterRepository
	// LocationRepo is required for any seed that compares
	// `resource.location.X` (e.g., seed:player-location-list-presence,
	// seed:player-location-read). Without it the LocationProvider
	// is not registered, no provider populates `resource.location.*`, and
	// every such seed silently default-denies. Per holomush-g776.
	LocationRepo world.LocationRepository
	// ObjectRepo is required for any seed that compares
	// `resource.object.X` (e.g., seed:player-object-colocation). Without
	// it the ObjectProvider is not registered, no provider populates
	// `resource.object.*`, and every such seed silently default-denies —
	// the same fingerprint as the original holomush-g776 bug. The provider
	// also needs CharacterRepo to resolve transitive locations for objects
	// held by characters. Per holomush-k3ud.
	ObjectRepo world.ObjectRepository
	// PropertyRepo is required for any seed that gates on resource.property.*
	// (e.g., seed:property-public-read, seed:property-private-read,
	// seed:property-restricted-visible-to). Without it the PropertyProvider
	// is not registered, no provider populates resource.property.*, and
	// every such seed silently default-denies — the same fingerprint as
	// the original holomush-g776 bug. Per holomush-72ou.
	PropertyRepo world.PropertyRepository
	// ParentLocationResolver resolves a property's parent entity's
	// effective location at evaluation time. Required alongside PropertyRepo
	// for PropertyProvider registration. The production wiring at
	// internal/access/setup/subsystem.go passes
	// postgres.NewParentLocationResolver(pool). Per holomush-72ou.
	ParentLocationResolver attribute.ParentLocationResolver
	// CharacterOwnerResolver resolves the row's character-keyed owner /
	// visible_to / excluded_from fields into their PLAYER-keyed peers
	// (resource.property.owner_player_id, .visible_to_players,
	// .excluded_from_players), which is what makes the 02-07 `viewer:` twins
	// expressible at all — the DSL cannot intersect two attribute lists.
	//
	// LEAVING THIS NIL IS SILENT: PropertyProvider still registers, the three
	// derived keys are simply absent, every condition referencing them
	// evaluates false, and the viewer twins default-deny with no error and no
	// failing test (RESEARCH P-7's failure mode). Production wiring at
	// internal/access/setup/subsystem.go passes
	// postgres.NewCharacterOwnerResolver(pool). Per plan 02-13.
	CharacterOwnerResolver attribute.CharacterOwnerResolver
	RoleStore              store.RoleStore
	AuditMode              audit.Mode
	// CryptoOperators is the list of player IDs (ULIDs) holding the
	// crypto.operator capability. Passed to PlayerAttributeProvider at
	// construction. Empty / nil → no operators (break-glass disabled).
	// Sub-epic B (Phase 5).
	CryptoOperators []string
	// PlayerKindLookup is an optional func that resolves whether a player is
	// an ephemeral guest. When nil the PlayerAttributeProvider omits the
	// is_guest key (has_is_guest=false) per the omit-don't-sentinel rule
	// (ADR holomush-ti1b). Production wiring at subsystem.go always supplies
	// this via auth/postgres.PlayerRepository.
	PlayerKindLookup attribute.PlayerKindLookup
	// PlayerRoleLookup is an optional func that resolves the PER-PLAYER role
	// union (the roles held by any character of that player, per 01-SPEC
	// §10.5). It feeds BOTH the viewer namespace (viewer.roles) and the player
	// namespace (player.roles) from ONE source, so the web read path and the
	// operator socket cannot disagree about whether the same human is an admin.
	//
	// It is a FUNC FIELD rather than a method on store.RoleStore deliberately:
	// only the concrete *PostgresRoleStore can answer the query, and widening
	// the interface would force every fake and mock of it to grow a method for
	// no benefit. PlayerKindLookup above is the same shape.
	//
	// LEAVING THIS NIL IS SILENT: both namespaces simply omit roles
	// (has_roles=false), every condition referencing them is false, and the
	// viewer twins plus seed:admin-section-access default-deny with no error
	// and no failing test (RESEARCH P-7's failure mode). Production wiring at
	// internal/access/setup/subsystem.go passes roleStore.PlayerRoles.
	PlayerRoleLookup attribute.PlayerRoleLookup
	// JobRegistry is the liveness registry for background jobs
	// (internal/jobs.Registry satisfies it). It feeds JobProvider, which
	// populates principal.job.* for job:<name> subjects.
	//
	// NIL IS TOLERATED AND FAILS CLOSED, which is the difference between this
	// field and the repo fields above: a nil registry makes the provider answer
	// "not running" for every job, so principal.job.* is absent, every
	// job-gating seed default-denies, and an entrypoint that runs no background
	// jobs needs no wiring. The provider still registers, so the `job`
	// namespace is known to the resolver either way. Per 02.2-CONTEXT D-49.
	JobRegistry attribute.JobRegistry
}

// BuildABACStack constructs and wires all ABAC components in the correct
// dependency order: policy store, attribute schema registry and resolver, every
// attribute provider, the `action` namespace schema, the compiler (built on that
// now-populated registry), the cache and its initial reload, the seed-coverage
// sweep, the audit logger, the policy engine, the health tracker, the poller, and
// the policy installer. If cfg.AuditMode is empty it defaults to denials-only.
//
// The registry-and-registrations-before-compiler-before-reload segment of that
// order is a correctness constraint, not a preference (02.2-04 / D-66). The
// compiler validates against the registry the providers and the `action`
// registration populate, so compiling the boot snapshot ahead of them would
// validate boot against an empty schema while every later reload validated
// against a populated one. See the comments at steps 11 and 12.
//
// A consequence worth stating at the entry point: THIS FUNCTION NOW FAILS BOOT ON
// A BAD POLICY. A stored policy referencing an undeclared action.* key fails to
// compile, which fails the reload, which fails this call — for in-tree seeds and
// operator-authored database rows alike (D-67). The error names the policy, its
// id, and the offending key.
// codecov:ignore — tested by integration and E2E tests
func BuildABACStack(ctx context.Context, cfg ABACConfig) (*ABACStack, error) {
	eb := oops.In("abac_setup")

	if cfg.AuditMode == "" {
		cfg.AuditMode = audit.ModeDenialsOnly
	}

	// 1. Policy store
	ps := policystore.NewPostgresStore(cfg.Pool)

	// 2-3. Attribute schema registry and resolver.
	//
	// These now come FIRST. Until 02.2-04 the compiler at what was step 2 was
	// built on a separately allocated types.AttributeSchema that nothing ever
	// populated, so validateAttributes saw an empty schema and every attribute
	// reference — including the `action` hard-error branch — was skipped. The
	// compiler is built from schemaReg below instead, which means the whole
	// ordering of this function is now load-bearing.
	schemaReg := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(schemaReg)

	// 8. Character provider (optional)
	if cfg.CharacterRepo != nil {
		var roleResolver attribute.RoleResolver
		if cfg.RoleStore != nil {
			roleResolver = store.NewPostgresRoleResolver(cfg.RoleStore)
		}
		// The guest-kind lookup is wired onto the character namespace so the
		// Layer-1 scene-command gate (plugins/core-scenes execute-scene-commands)
		// can read principal.character.is_guest — command dispatch evaluates a
		// character: subject, which never carries the player: namespace. Omitted
		// when nil (tests / alternate entrypoints); production wiring at
		// internal/access/setup/subsystem.go always supplies it. Per holomush-5rh.23.
		charOpts := []attribute.CharacterProviderOption{}
		if cfg.PlayerKindLookup != nil {
			charOpts = append(charOpts, attribute.WithCharacterKindLookup(cfg.PlayerKindLookup))
		}
		charProvider := attribute.NewCharacterProvider(cfg.CharacterRepo, roleResolver, charOpts...)
		if err := resolver.RegisterProvider(charProvider); err != nil {
			return nil, eb.Wrapf(err, "register character provider")
		}
	}

	// 8b. Location provider (optional in signature, but required in practice
	// for any seed referencing resource.location.*). Holds the
	// LocationRepository the provider uses to fetch loc.ID/Name/Type/... for
	// ABAC evaluation. Without this, three seeds silently default-deny:
	// seed:player-location-read, seed:player-location-list-characters,
	// seed:player-location-list-presence — because `resource.location.id` is
	// never populated in the resource bag (only the un-namespaced `id`
	// injected by the resolver). Per holomush-g776.
	//
	// The optional shape is preserved for tests and alternate entrypoints
	// that don't need location-resource policies. Production wiring at
	// internal/access/setup/subsystem.go ALWAYS supplies the repo. Emit a
	// loud WARN at construction time when it's missing so any future
	// caller that drops the repo gets a recurrence signal — the original
	// g776 bug was silent at startup and only manifested via e2e symptoms.
	if cfg.LocationRepo != nil {
		locProvider := attribute.NewLocationProvider(cfg.LocationRepo)
		if err := resolver.RegisterProvider(locProvider); err != nil {
			return nil, eb.Wrapf(err, "register location provider")
		}
	} else {
		slog.WarnContext(ctx,
			"ABAC setup: LocationRepo not provided — seeds referencing resource.location.* will silently default-deny",
			"affected_seeds", "seed:player-location-read, seed:player-location-list-characters, seed:player-location-list-presence",
			"reference", "holomush-g776")
	}

	// 8c. Object provider (optional in signature, required in practice for
	// any seed referencing resource.object.*). Without this,
	// seed:player-object-colocation silently default-denies and every
	// object read/write/move/delete via internal/world/service.go (six
	// production call sites at GetObject, CreateObject, UpdateObject,
	// DeleteObject, MoveObject, plus the look-into-object check in the
	// command pipeline at service.go:940) returns "no policies satisfied".
	// Mirrors LocationProvider/CharacterProvider wildcard tolerance —
	// CreateObject emits access.ObjectResource("*") at service.go:449.
	//
	// Production wiring at internal/access/setup/subsystem.go ALWAYS
	// supplies both ObjectRepo and CharacterRepo. Loud WARN when missing
	// so any future caller that drops the repo gets a recurrence signal;
	// the original g776 bug was silent at startup and only manifested via
	// e2e symptoms. Per holomush-k3ud.
	if cfg.ObjectRepo != nil {
		objProvider := attribute.NewObjectProvider(cfg.ObjectRepo, cfg.CharacterRepo)
		if err := resolver.RegisterProvider(objProvider); err != nil {
			return nil, eb.Wrapf(err, "register object provider")
		}
	} else {
		slog.WarnContext(ctx,
			"ABAC setup: ObjectRepo not provided — seeds referencing resource.object.* will silently default-deny",
			"affected_seeds", "seed:player-object-colocation",
			"reference", "holomush-k3ud")
	}

	// 8d. Property provider (optional in signature; required in practice for
	// any seed gating on resource.property.*). Without this, all six
	// property visibility seeds silently default-deny:
	// seed:property-public-read, seed:property-private-read,
	// seed:property-admin-read, seed:property-owner-write,
	// seed:property-restricted-visible-to, seed:property-restricted-excluded
	// — same fingerprint as holomush-g776 (location) and holomush-k3ud (object).
	//
	// Production wiring at internal/access/setup/subsystem.go ALWAYS
	// supplies both PropertyRepo and ParentLocationResolver. Loud WARN when
	// either is missing so any future caller that drops the dependency
	// gets a recurrence signal. Per holomush-72ou.
	if cfg.PropertyRepo != nil && cfg.ParentLocationResolver != nil {
		propProvider := attribute.NewPropertyProvider(cfg.PropertyRepo, cfg.ParentLocationResolver, cfg.CharacterOwnerResolver)
		if err := resolver.RegisterProvider(propProvider); err != nil {
			return nil, eb.Wrapf(err, "register property provider")
		}
	} else {
		slog.WarnContext(ctx,
			"ABAC setup: PropertyRepo or ParentLocationResolver not provided — seeds referencing resource.property.* will silently default-deny",
			"property_repo_set", cfg.PropertyRepo != nil,
			"parent_location_resolver_set", cfg.ParentLocationResolver != nil,
			"affected_seeds", "seed:property-public-read, seed:property-private-read, seed:property-admin-read, seed:property-owner-write, seed:property-restricted-visible-to, seed:property-restricted-excluded",
			"reference", "holomush-72ou")
	}

	// 8a. Player provider (subject namespace; resolves player.id, player.grants,
	// player.is_guest, player.has_is_guest for "player:<ulid>" subjects).
	// Sub-epic B (Phase 5); is_guest added per holomush-5rh.8.13.
	playerOpts := []attribute.PlayerAttributeProviderOption{}
	if cfg.PlayerKindLookup != nil {
		playerOpts = append(playerOpts, attribute.WithPlayerKindLookup(cfg.PlayerKindLookup))
	}
	if cfg.PlayerRoleLookup != nil {
		playerOpts = append(playerOpts, attribute.WithPlayerRoleLookup(cfg.PlayerRoleLookup))
	}
	playerProvider := attribute.NewPlayerAttributeProvider(cfg.CryptoOperators, playerOpts...)
	if err := resolver.RegisterProvider(playerProvider); err != nil {
		return nil, eb.Wrapf(err, "register player provider")
	}

	// 8e. Viewer provider (subject namespace; resolves viewer.tier,
	// viewer.player_id, viewer.has_player_id, viewer.roles, viewer.has_roles for
	// the "viewer:anonymous" / "viewer:guest:<ulid>" / "viewer:player:<ulid>"
	// rungs of 01-SPEC §8.4.1's tier ladder).
	//
	// This registration is 01-SPEC §8.4.1's Phase-2 obligation 1, and SKIPPING
	// IT IS SILENT — which is exactly why the obligation names it. An
	// unregistered namespace does not error: principal.viewer.tier is simply
	// absent, a missing key evaluates FALSE for every operator, and the whole
	// tier-floor family default-denies in production while unit tests that stub
	// the attribute bag stay green. Plan 02-03 shipped ViewerTierProvider
	// deliberately unregistered (provider before any seed references it); this
	// is where it joins the stack, ahead of the 02-07 seeds that read it.
	//
	// Unconditional: unlike the repo-backed providers above, the viewer provider
	// has no required dependency. The role lookup is optional and omits
	// viewer.roles when absent (ADR holomush-ti1b).
	viewerOpts := []attribute.ViewerTierProviderOption{}
	if cfg.PlayerRoleLookup != nil {
		viewerOpts = append(viewerOpts, attribute.WithViewerRoleLookup(cfg.PlayerRoleLookup))
	}
	viewerProvider := attribute.NewViewerTierProvider(viewerOpts...)
	if err := resolver.RegisterProvider(viewerProvider); err != nil {
		return nil, eb.Wrapf(err, "register viewer provider")
	}

	// 9. Command provider (resolves resource.command.name for seed policies)
	cmdProvider := attribute.NewCommandProvider()
	if err := resolver.RegisterProvider(cmdProvider); err != nil {
		return nil, eb.Wrapf(err, "register command provider")
	}

	// 9a. Stream provider (resolves resource.stream.{name,location} for
	// seed:player-stream-emit and seed:player-location-stream-read policies).
	streamProvider := attribute.NewStreamProvider()
	if err := resolver.RegisterProvider(streamProvider); err != nil {
		return nil, eb.Wrapf(err, "register stream provider")
	}

	// 10. Plugin provider (nil registry — two-phase init)
	pluginProvider := attribute.NewPluginProvider(nil)
	if err := resolver.RegisterProvider(pluginProvider); err != nil {
		return nil, eb.Wrapf(err, "register plugin provider")
	}

	// 10a. Job provider (background-job principals; 02.2 AUTHZ-02). It resolves
	// principal.job.{name,...} for job:<name> subjects, gated on the liveness
	// registry. cfg.JobRegistry is nil-tolerant: a nil registry fails closed for
	// every job, which is correct for an entrypoint that runs none.
	//
	// REGISTERING IT IS NOT OPTIONAL once a seed references principal.job.*.
	// Without this step no provider owns the `job` namespace, so every such seed
	// silently default-denies with no startup signal — the holomush-g776 / xxel
	// bug class. Its PLACEMENT is load-bearing too: it must precede
	// warnOnMissingSeedCoverage below, so `job` is already in
	// resolver.RegisteredNamespaces() when the corpus sweep runs.
	jobProvider := attribute.NewJobProvider(cfg.JobRegistry)
	if err := resolver.RegisterProvider(jobProvider); err != nil {
		return nil, eb.Wrapf(err, "register job provider")
	}

	// 10b. Action namespace schema (02.2 D-60). `action` is a caller-supplied
	// bag, not an entity that gets resolved, so it is registered by a direct
	// package-level Register call rather than through a synthetic provider whose
	// ResolveSubject/ResolveResource would return (nil, nil) purely to carry a
	// Schema(). attribute.ActionNamespaceSchema is the single source of truth for
	// the key set; its provenance is
	// .planning/phases/02.2-background-job-authorization-model/02.2-ACTION-AUDIT.md.
	//
	// STATUS: LOAD-BEARING as of 02.2-04 (D-66). The compiler built immediately
	// below is constructed on schemaReg.Schema(), so validateAttributes sees
	// HasNamespace("action") == true and its hard-error branch is live: a policy
	// referencing an undeclared action.* key fails to compile, which fails the
	// reload below, which fails this function, which fails boot. That is
	// deliberate and applies to EVERY policy source — in-tree seeds and
	// operator-authored database rows alike (D-67).
	//
	// (Until 02.2-04 this registration was a documented no-op, because the
	// compiler was built on a separate, never-populated schema. 02.2-CONTEXT D-59
	// described both a landmine and a benefit here; its own AMENDED banner records
	// that research finding F1 falsified both AS MECHANISM. This step is what D-59
	// intended; the wiring below is what delivers it.)
	if err := attribute.Register(schemaReg, "action", attribute.ActionNamespaceSchema()); err != nil {
		return nil, eb.Wrapf(err, "register action namespace schema")
	}

	// 11. Compiler, built on the registry the steps above populated.
	//
	// SchemaRegistry.Schema() returns the LIVE *types.AttributeSchema pointer
	// (attribute/schema.go:96-99), so relative CONSTRUCTION order against the
	// registrations does not matter — only relative COMPILE order does, which is
	// what step 12 is positioned for.
	compiler := policy.NewCompiler(schemaReg.Schema())

	// 12. Cache, with the initial load.
	//
	// THE RELOAD'S POSITION IS THE POINT, not an accident of layout. It used to
	// run near the top of this function, before any provider or the `action`
	// schema had registered. Once the compiler is wired to schemaReg (step 11),
	// reloading there would compile the BOOT snapshot against a still-empty
	// registry while every subsequent poller and invalidation reload compiled
	// against a populated one — different validation behaviour at boot than at
	// steady state, so a bad policy would sail through boot and then kill the
	// first reload with no operator anywhere near the console. That is strictly
	// worse than the uniform no-op it replaced (research Pitfall 2; threat
	// T-02.2-18). Moving the reload here makes boot and steady state validate
	// identically.
	//
	// Nothing between the registry allocation and this line reads `cache`; the
	// health tracker, the ps.SetOnMutate invalidation hook and the poller are all
	// constructed further down (research assumption A4, traced 2026-08-09).
	// TestBuildABACStackReloadsTheCacheAfterTheActionRegistration pins the order
	// so a future edit cannot quietly undo it.
	cache := policy.NewCache(ps, compiler)
	if err := cache.Reload(ctx); err != nil {
		return nil, eb.Wrapf(err, "cache initial reload failed")
	}

	// 13. Seed-coverage validator (holomush-xxel). Renumbered repeatedly as steps
	// were inserted ahead of it — the job provider took 10a, the `action` schema
	// registration is 10b, and 02.2-04 moved the compiler and the cache reload to
	// 11 and 12 — so the labels keep reading in execution order.
	// After all providers are
	// registered, walk the seed corpus and WARN per namespace referenced by
	// any seed but not registered. Catches the holomush-g776 / xxel bug
	// class at construction time: a missing provider means every seed
	// gating on `resource.<ns>.*` silently default-denies, with no startup
	// signal. The validator is non-fatal by design — production resilience
	// over fail-closed-at-boot for an issue that was historically silent
	// anyway. Specific provider-nil branches above (CharacterRepo,
	// LocationRepo) already WARN at their own grain; this is the corpus-
	// level sweep that catches a missing provider regardless of cause.
	warnOnMissingSeedCoverage(ctx, resolver.RegisteredNamespaces(), policy.SeedPolicies())

	// 14. SQL bridge for audit writer
	sqlDB := stdlib.OpenDBFromPool(cfg.Pool)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close() //nolint:errcheck // best-effort cleanup; ping error takes precedence
		return nil, eb.Wrapf(err, "sql bridge ping failed")
	}

	// 15. Audit logger
	writer := audit.NewPostgresWriter(sqlDB)
	auditLogger := audit.NewLogger(cfg.AuditMode, writer, "")

	// 16. Replay WAL (non-fatal)
	if err := auditLogger.ReplayWAL(ctx); err != nil {
		slog.WarnContext(ctx, "audit WAL replay failed (non-fatal)", "error", err)
	}

	// 17. Session resolver (no-op — fails closed)
	sessionRes := &noopSessionResolver{}

	// 18. Engine
	engine := policy.NewEngine(resolver, cache, sessionRes, auditLogger)

	// 19. Health tracker for policy cache
	healthTracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{
		SubsystemName: "abac.policy-cache",
		GracePeriod:   60 * time.Second,
		MaxFailures:   30,
		OnTierChange: func(from, to lifecycle.HealthTier) {
			eng := engine
			switch {
			case to == lifecycle.HealthDead:
				eng.EnterDegradedMode("policy cache dead — initiating shutdown")
				slog.Error("ABAC policy cache dead — initiating graceful shutdown")
			case to >= lifecycle.HealthStale:
				eng.EnterDegradedMode("policy cache " + to.String())
			case to == lifecycle.HealthWarm && from >= lifecycle.HealthStale:
				eng.ClearDegradedMode()
			}
		},
	})

	// 20. Wire store → cache invalidation (fast path).
	// Use a detached context so invalidation isn't cancelled if the request context expires.
	ps.SetOnMutate(func(ctx context.Context) {
		invalidateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cache.Invalidate(invalidateCtx); err != nil {
			slog.ErrorContext(ctx, "cache invalidation after store mutation failed",
				"error", err)
			healthTracker.RecordFailure("invalidation failed: " + err.Error())
		} else {
			healthTracker.RecordSuccess()
		}
	})

	// 21. Create poller (safety net)
	poller, pollerErr := policy.NewPoller(policy.PollerConfig{
		Querier:  ps,
		Reloader: cache,
		Tracker:  healthTracker,
		Interval: 10 * time.Second,
	})
	if pollerErr != nil {
		return nil, eb.Wrapf(pollerErr, "create policy poller")
	}

	// 22. Policy installer
	installer := plugins.NewPolicyInstaller(ps)

	return &ABACStack{
		Engine:          engine,
		Cache:           cache,
		Poller:          poller,
		HealthTracker:   healthTracker,
		PolicyStore:     ps,
		Resolver:        resolver,
		AuditLogger:     auditLogger,
		PolicyInstaller: installer,
		PluginProvider:  pluginProvider,
		JobProvider:     jobProvider,
		sqlDB:           sqlDB,
	}, nil
}

// noopSessionResolver rejects all session resolution requests.
// It fails closed with a SESSION_INVALID error code.
type noopSessionResolver struct{}

func (n *noopSessionResolver) ResolveSession(_ context.Context, _ string) (string, error) {
	return "", oops.Code("SESSION_INVALID").
		With("session_provided", true).
		Errorf("session resolution not yet implemented")
}

// NewNoopSessionResolver creates a session resolver that rejects all sessions.
func NewNoopSessionResolver() policy.SessionResolver {
	return &noopSessionResolver{}
}
