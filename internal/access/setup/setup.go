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
}

// BuildABACStack constructs and wires all ABAC components in the correct dependency order:
// policy store, cache (with initial reload), attribute resolver and providers, policy engine,
// audit logger, health tracker, poller, and policy installer. If cfg.AuditMode is empty it
// defaults to denials-only.
// codecov:ignore — tested by integration and E2E tests
func BuildABACStack(ctx context.Context, cfg ABACConfig) (*ABACStack, error) {
	eb := oops.In("abac_setup")

	if cfg.AuditMode == "" {
		cfg.AuditMode = audit.ModeDenialsOnly
	}

	// 1. Policy store
	ps := policystore.NewPostgresStore(cfg.Pool)

	// 2-3. Schema and compiler
	schema := types.NewAttributeSchema()
	compiler := policy.NewCompiler(schema)

	// 4-5. Cache with initial load
	cache := policy.NewCache(ps, compiler)
	if err := cache.Reload(ctx); err != nil {
		return nil, eb.Wrapf(err, "cache initial reload failed")
	}

	// 6-7. Attribute resolver
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
		propProvider := attribute.NewPropertyProvider(cfg.PropertyRepo, cfg.ParentLocationResolver)
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
	playerProvider := attribute.NewPlayerAttributeProvider(cfg.CryptoOperators, playerOpts...)
	if err := resolver.RegisterProvider(playerProvider); err != nil {
		return nil, eb.Wrapf(err, "register player provider")
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

	// 10a. Seed-coverage validator (holomush-xxel). After all providers are
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

	// 10-11. SQL bridge for audit writer
	sqlDB := stdlib.OpenDBFromPool(cfg.Pool)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close() //nolint:errcheck // best-effort cleanup; ping error takes precedence
		return nil, eb.Wrapf(err, "sql bridge ping failed")
	}

	// 12-13. Audit logger
	writer := audit.NewPostgresWriter(sqlDB)
	auditLogger := audit.NewLogger(cfg.AuditMode, writer, "")

	// 14. Replay WAL (non-fatal)
	if err := auditLogger.ReplayWAL(ctx); err != nil {
		slog.WarnContext(ctx, "audit WAL replay failed (non-fatal)", "error", err)
	}

	// 15. Session resolver (no-op — fails closed)
	sessionRes := &noopSessionResolver{}

	// 16. Engine
	engine := policy.NewEngine(resolver, cache, sessionRes, auditLogger)

	// 17. Health tracker for policy cache
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

	// 18. Wire store → cache invalidation (fast path).
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

	// 19. Create poller (safety net)
	poller, pollerErr := policy.NewPoller(policy.PollerConfig{
		Querier:  ps,
		Reloader: cache,
		Tracker:  healthTracker,
		Interval: 10 * time.Second,
	})
	if pollerErr != nil {
		return nil, eb.Wrapf(pollerErr, "create policy poller")
	}

	// 20. Policy installer
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
