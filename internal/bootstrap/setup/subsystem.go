// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package setup provides the bootstrap subsystem lifecycle wrapper.
// It lives in a sub-package to avoid potential import cycles: the bootstrap
// package imports internal/plugin (for BootstrapRunner), so the subsystem
// that also imports internal/plugin types cannot reside there directly.
package setup

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/bootstrap"
	"github.com/holomush/holomush/internal/content"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/naming"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/world"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
)

// PoolProvider provides a database connection pool.
type PoolProvider interface {
	Pool() *pgxpool.Pool
}

// WorldServiceProvider provides the world service.
type WorldServiceProvider interface {
	Service() *world.Service
}

// PluginManagerProvider provides the plugin manager for setting plugin discovery.
type PluginManagerProvider interface {
	Manager() *plugins.Manager
}

// PlayerRepoProvider provides a player repository lazily. Implemented by the
// auth subsystem without requiring a direct import.
type PlayerRepoProvider interface {
	PlayerRepo() auth.PlayerRepository
}

// HasherProvider provides a password hasher lazily. Implemented by the
// auth subsystem without requiring a direct import.
type HasherProvider interface {
	Hasher() auth.PasswordHasher
}

// PolicyStoreProvider provides the policy store with invalidation hook wired.
// Implemented by the ABAC subsystem.
type PolicyStoreProvider interface {
	PolicyStore() policystore.PolicyStore
}

// BootstrapSubsystemConfig configures the bootstrap subsystem.
type BootstrapSubsystemConfig struct {
	DB                 PoolProvider
	ABAC               PolicyStoreProvider // for policy bootstrap (uses store with invalidation hook)
	World              WorldServiceProvider
	Plugins            PluginManagerProvider
	PlayerRepos        PlayerRepoProvider // from auth subsystem (lazy)
	Hashers            HasherProvider     // from auth subsystem (lazy)
	Setting            string
	ResetSetting       bool
	SkipSeedMigrations bool
	GuestStartLocation string // pre-parsed ULID string; empty = resolve from metadata
}

// BootstrapSubsystem orchestrates the multi-step bootstrap sequence:
// policy seeding, setting/world seeding, and admin creation.
type BootstrapSubsystem struct {
	cfg             BootstrapSubsystemConfig
	startLocationID ulid.ULID
	started         bool
}

// NewBootstrapSubsystem creates a BootstrapSubsystem configured by cfg.
// The subsystem performs no resource allocation until Start is called.
//
// The cfg parameter supplies lazy providers and bootstrap options: database pool,
// ABAC policy store, world service and transactor, plugin manager, player
// repository and password hasher providers, optional setting plugin name and
// reset flag, SkipSeedMigrations, and an optional GuestStartLocation ULID string.
func NewBootstrapSubsystem(cfg BootstrapSubsystemConfig) *BootstrapSubsystem {
	return &BootstrapSubsystem{cfg: cfg}
}

// ID returns SubsystemBootstrap.
func (s *BootstrapSubsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemBootstrap }

// DependsOn returns all subsystems that must start before bootstrap.
func (s *BootstrapSubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemABAC,
		lifecycle.SubsystemWorld,
		lifecycle.SubsystemAuth,
		lifecycle.SubsystemPlugins,
		lifecycle.SubsystemSessions,
	}
}

// Prepare runs the full bootstrap sequence — one-shot DB work against a
// prepared pool, not a loop and not external (D-13.3 row 8):
//  1. Create BootstrapRunner
//  2. Register policy bootstrapper (priority 200)
//  3. Register setting bootstrapper (priority 300) if configured
//  4. Register admin bootstrapper (priority 400)
//  5. Run all bootstrappers
//  6. Resolve starting location
//
// No idempotency guard: seeding already runs on every boot by design, and
// re-running is harmless.
// codecov:ignore — tested by integration and E2E tests
func (s *BootstrapSubsystem) Prepare(ctx context.Context) error {
	pool := s.cfg.DB.Pool()

	// Defense-in-depth: refuse to start if any plugin-kind event in
	// events_audit lacks an actor_id. Migration 000018 makes orphans
	// impossible from a clean install; this guards against manual restore
	// from an old backup. Relocated from cmd/holomush's pre-orchestrator
	// call site (07-09 item 5) — it now runs first against this same pool,
	// behind the Bootstrap -> Database edge, instead of racing a
	// hand-sequenced DB pre-start.
	if orphanErr := runBootstrapOrphanCheck(ctx, pool); orphanErr != nil {
		return orphanErr
	}

	// Parse pre-configured start location if provided.
	if s.cfg.GuestStartLocation != "" {
		parsed, err := ulid.Parse(s.cfg.GuestStartLocation)
		if err != nil {
			return oops.Code("INVALID_START_LOCATION").With("value", s.cfg.GuestStartLocation).Wrap(err)
		}
		s.startLocationID = parsed
	}

	// 1. Create bootstrap runner.
	runner := plugins.NewBootstrapRunner(slog.Default())

	// 2. Register policy bootstrapper (priority 200).
	// Uses the ABAC subsystem's policy store (which has the invalidation hook wired)
	// so that seed policy writes automatically invalidate the cache.
	policyBootstrapFn := func(ctx context.Context, skipSeedMigrations bool) error {
		partitions := audit.NewPostgresPartitionCreator(pool)
		ps := s.cfg.ABAC.PolicyStore()
		schema := types.NewAttributeSchema()
		compiler := policy.NewCompiler(schema)
		opts := policy.BootstrapOptions{SkipSeedMigrations: skipSeedMigrations}
		return policy.Bootstrap(ctx, partitions, ps, compiler, slog.Default(), opts)
	}
	runner.Register(bootstrap.NewPolicyBootstrapper(policyBootstrapFn, s.cfg.SkipSeedMigrations))

	// 3. Register setting bootstrapper (priority 300) if configured.
	if err := s.registerSettingBootstrapper(ctx, runner, pool); err != nil {
		return err
	}

	// 4. Register admin bootstrapper (priority 400).
	charRepo := worldpostgres.NewCharacterRepository(pool)
	locRepo := worldpostgres.NewLocationRepository(pool)

	// Build the atomic character-genesis service (character + envelope in one
	// world transaction) and back CharacterService with it (05-15). All four
	// deps share the same pool so the character write, binding, and outbox
	// envelope enroll in the same world txKey.
	genesis, genErr := auth.NewCharacterGenesisService(
		charRepo,
		worldpostgres.NewTransactor(pool),
		worldpostgres.NewBindingRepository(pool),
		worldpostgres.NewOutboxStore(pool),
		worldpostgres.NewReapingGuard(pool),
	)
	if genErr != nil {
		return oops.Code("AUTH_SETUP_FAILED").Wrap(genErr)
	}

	// Build CharacterService with a location adapter that defers to the
	// startLocationID pointer (resolved after bootstrap completes).
	authCharRepo := NewCharRepoAdapter(pool, charRepo)
	authLocRepo := NewLocRepoAdapter(&s.startLocationID, locRepo)
	characterService, err := auth.NewCharacterService(authCharRepo, authLocRepo, genesis)
	if err != nil {
		return oops.Code("AUTH_SETUP_FAILED").Wrap(err)
	}

	roleStore := store.NewPostgresRoleStore(pool)
	runner.Register(bootstrap.NewAdminBootstrapper(bootstrap.SeedAdminDeps{
		PlayerRepo:  s.cfg.PlayerRepos.PlayerRepo(),
		CharService: characterService,
		RoleStore:   roleStore,
		Hasher:      s.cfg.Hashers.Hasher(),
		NameTheme:   naming.NewStarTheme(),
	}))

	// 5. Run all bootstrappers in priority order.
	if err := runner.RunAll(ctx); err != nil {
		return oops.Code("BOOTSTRAP_FAILED").With("operation", "run bootstrap plugins").Wrap(err)
	}

	// 6. Resolve starting location from bootstrap metadata if not explicitly configured.
	if s.startLocationID.IsZero() {
		metadataStore := bootstrap.NewPostgresMetadataStore(pool)
		locIDStr, found, metaErr := metadataStore.Get(ctx, "starting_location_id")
		if metaErr != nil {
			return oops.Code("START_LOCATION_FAILED").Wrap(metaErr)
		}
		if !found {
			return oops.Code("START_LOCATION_NOT_FOUND").
				Hint("set guest-start-location in config or add starting_location to the setting plugin manifest").
				New("no starting_location_id in bootstrap metadata")
		}
		parsed, parseErr := ulid.Parse(locIDStr)
		if parseErr != nil {
			return oops.Code("START_LOCATION_INVALID").With("value", locIDStr).Wrap(parseErr)
		}
		s.startLocationID = parsed
		slog.InfoContext(ctx, "resolved starting location from bootstrap metadata", "id", locIDStr)
	}

	s.started = true
	slog.InfoContext(ctx, "bootstrap subsystem prepared")
	return nil
}

// Activate is a no-op — bootstrap's work is a one-shot Prepare-time seed
// (D-13.3 row 8).
func (s *BootstrapSubsystem) Activate(_ context.Context) error { return nil }

// Stop is a no-op — bootstrap runs once during startup.
// codecov:ignore — tested by integration and E2E tests
func (s *BootstrapSubsystem) Stop(_ context.Context) error { return nil }

// StartLocationID returns the resolved starting location ULID.
// Panics if called before Prepare().
func (s *BootstrapSubsystem) StartLocationID() ulid.ULID {
	if !s.started {
		panic("bootstrap/setup: StartLocationID() called before Prepare()")
	}
	return s.startLocationID
}

// registerSettingBootstrapper discovers and registers the setting bootstrapper
// if a setting name is configured and the plugin is found.
func (s *BootstrapSubsystem) registerSettingBootstrapper(ctx context.Context, runner *plugins.BootstrapRunner, pool *pgxpool.Pool) error {
	if s.cfg.Setting == "" || s.cfg.Plugins == nil {
		return nil
	}

	mgr := s.cfg.Plugins.Manager()
	discovered, err := mgr.Discover(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to discover setting plugins", "error", err)
		return nil
	}

	var settingPlugin *plugins.DiscoveredPlugin
	for _, dp := range discovered {
		if dp.Manifest.Type == plugins.TypeSetting && dp.Manifest.Name == s.cfg.Setting {
			settingPlugin = dp
			break
		}
	}

	if settingPlugin == nil {
		metadataStore := bootstrap.NewPostgresMetadataStore(pool)
		_, settingRecorded, metaErr := metadataStore.Get(ctx, "active_setting")
		if metaErr != nil {
			return oops.Code("BOOTSTRAP_FAILED").
				With("setting", s.cfg.Setting).
				With("operation", "check active_setting metadata").
				Wrap(metaErr)
		}
		if !settingRecorded {
			return oops.Code("BOOTSTRAP_FAILED").
				With("setting", s.cfg.Setting).
				New("setting plugin not found and no active_setting recorded; cannot bootstrap world on first boot")
		}
		slog.WarnContext(ctx, "setting plugin not found, skipping setting bootstrap (world already seeded)", "setting", s.cfg.Setting)
		return nil
	}

	contentStore := content.NewPostgresStore(pool)
	metadataStore := bootstrap.NewPostgresMetadataStore(pool)
	runner.Register(bootstrap.NewSettingBootstrapper(bootstrap.SettingBootstrapperOpts{
		ContentStore:  contentStore,
		WorldService:  s.cfg.World.Service(),
		MetadataStore: metadataStore,
		SettingName:   s.cfg.Setting,
		ResetSetting:  s.cfg.ResetSetting,
		Manifest:      settingPlugin.Manifest,
		PluginDir:     settingPlugin.Dir,
		Logger:        slog.Default(),
	}))
	slog.InfoContext(ctx, "setting bootstrapper registered", "setting", s.cfg.Setting)

	return nil
}

// runBootstrapOrphanCheck refuses to start if any plugin-kind event lacks an
// actor_id (a legacy event that survived a w9ml migration mis-step).
// Defense-in-depth: migration 000018 makes orphans impossible from a clean
// install. This guards against manual restore from old backup or partial
// migration recovery.
//
// After 06-01's migration 000052 renamed the pre-partition history off
// events_audit into events_audit_unpartitioned, a restore-from-old-backup
// lands residual orphans in the LEGACY table — invisible to a probe that only
// scans events_audit. Since this check runs BEFORE 06-02's Backfill re-homes
// those rows (core.go), it must scan BOTH tables. events_audit_unpartitioned is
// referenced only when present (to_regclass guard) so a clean install — where
// the legacy table is absent — does not error with "relation does not exist".
//
// Uses an EXISTS probe (returns on the first matching row) instead of a
// full-table COUNT(*) — on large audit tables the aggregate scan can
// noticeably delay startup. We only count on the failure path, where startup
// is already aborting.
//
// Relocated verbatim from cmd/holomush/bootstrap_orphan.go (07-09 item 5) —
// package main cannot reference it once BootstrapSubsystem.Start is the call
// site, so the definition moved behind the Bootstrap -> Database edge.
func runBootstrapOrphanCheck(ctx context.Context, pool *pgxpool.Pool) error {
	pluginKind := eventbus.ActorKindPlugin.String()

	// Is the legacy unpartitioned table present? (Absent on a clean install and
	// after Backfill re-homes it.)
	var legacyPresent *string
	if err := pool.QueryRow(ctx,
		`SELECT to_regclass('public.events_audit_unpartitioned')::text`).Scan(&legacyPresent); err != nil {
		return oops.Code("BOOTSTRAP_ORPHAN_CHECK_FAILED").Wrap(err)
	}

	existsQuery := `SELECT EXISTS (
		  SELECT 1 FROM events_audit WHERE actor_kind = $1 AND actor_id IS NULL
		)`
	countQuery := `SELECT COUNT(*) FROM events_audit WHERE actor_kind = $1 AND actor_id IS NULL`
	if legacyPresent != nil {
		slog.DebugContext(ctx, "bootstrap orphan check also scanning events_audit_unpartitioned")
		existsQuery = `SELECT
		  EXISTS (SELECT 1 FROM events_audit WHERE actor_kind = $1 AND actor_id IS NULL)
		  OR EXISTS (SELECT 1 FROM events_audit_unpartitioned WHERE actor_kind = $1 AND actor_id IS NULL)`
		countQuery = `SELECT
		  (SELECT COUNT(*) FROM events_audit WHERE actor_kind = $1 AND actor_id IS NULL)
		  + (SELECT COUNT(*) FROM events_audit_unpartitioned WHERE actor_kind = $1 AND actor_id IS NULL)`
	}

	var hasOrphan bool
	if err := pool.QueryRow(ctx, existsQuery, pluginKind).Scan(&hasOrphan); err != nil {
		return oops.Code("BOOTSTRAP_ORPHAN_CHECK_FAILED").Wrap(err)
	}
	if !hasOrphan {
		return nil
	}
	// Only count on the failure path so the operator gets the magnitude.
	var count int
	if cerr := pool.QueryRow(ctx, countQuery, pluginKind).Scan(&count); cerr != nil {
		// Fall through with count=0 if the count fails — the EXISTS probe
		// already proved there's at least one orphan.
		count = 0
	}
	return oops.Code("PLUGIN_ACTOR_ORPHAN_DETECTED").
		With("count", count).
		Errorf("legacy plugin-actor events present after w9ml migration")
}
