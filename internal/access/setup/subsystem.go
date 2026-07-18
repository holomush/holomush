// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
	authpostgres "github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/internal/lifecycle"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/world/postgres"
)

// PoolProvider provides a database connection pool. Implemented by the
// database subsystem without requiring a direct import.
type PoolProvider interface {
	Pool() *pgxpool.Pool
}

// ABACSubsystemConfig configures the ABAC subsystem.
type ABACSubsystemConfig struct {
	DB        PoolProvider
	Registry  *lifecycle.ReadinessRegistry
	AuditMode audit.Mode
	// CryptoOperators is the RAW configured list of player IDs (ULIDs) —
	// straight from crypto.operators config, not yet cross-checked against
	// the players table. Start validates it (validateCryptoOperators,
	// lax+warn) against this subsystem's own pool and forwards the
	// validated/deduplicated slice to ABACConfig / PlayerAttributeProvider.
	// Empty / nil → no operators (break-glass disabled). Sub-epic B (Phase 5).
	CryptoOperators []string
}

// ABACSubsystem manages the ABAC policy engine, cache, and health tracker.
type ABACSubsystem struct {
	cfg          ABACSubsystemConfig
	stack        *ABACStack
	pollerCancel context.CancelFunc
}

// NewABACSubsystem creates an ABAC subsystem. No live resources are allocated.
// If cfg.AuditMode is empty, it defaults to audit.ModeDenialsOnly.
func NewABACSubsystem(cfg ABACSubsystemConfig) *ABACSubsystem {
	if cfg.AuditMode == "" {
		cfg.AuditMode = audit.ModeDenialsOnly
	}
	return &ABACSubsystem{cfg: cfg}
}

// ID returns SubsystemABAC.
func (s *ABACSubsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemABAC }

// DependsOn returns [SubsystemDatabase].
func (s *ABACSubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}
}

// Prepare builds the ABAC stack, including the crypto-operator allow-list
// validation, and registers health (D-13.3 row 3). The poller goroutine is
// NOT started here — it is domain work and belongs in Activate.
// Prepare is idempotent: if the subsystem is already prepared, it returns
// nil immediately — a real guard, because a second Prepare would rebuild the
// stack (wasted DB round trips) even though the poller itself now guards
// separately in Activate.
// codecov:ignore — tested by integration and E2E tests
func (s *ABACSubsystem) Prepare(ctx context.Context) error {
	if s.stack != nil {
		return nil // already prepared
	}
	pool := s.cfg.DB.Pool()

	// Cross-check the configured crypto.operator allow-list against the
	// players table. Lax+warn: unknown IDs and transient PG failures emit
	// a structured warning but MUST NOT gate startup (Phase 5 sub-epic B
	// INV-B5 / INV-B7). validateCryptoOperators always returns nil error in
	// Phase 5 sub-epic B (lax+warn); the signature reserves the slot for a
	// future fail-closed mode (sub-epic D), but today there is no error
	// path to handle. Relocated from cmd/holomush (07-09 item 6) — this is
	// the first point after Start where the pool exists.
	operators, _ := validateCryptoOperators(ctx, pool, s.cfg.CryptoOperators, slog.Default()) //nolint:errcheck // Phase 5 sub-epic B is lax+warn; sub-epic D will rewire to handle errors here.

	roleStore := store.NewPostgresRoleStore(pool)
	playerRepo := authpostgres.NewPlayerRepository(pool)
	stack, err := BuildABACStack(ctx, ABACConfig{
		Pool:                   pool,
		CharacterRepo:          postgres.NewCharacterRepository(pool),
		LocationRepo:           postgres.NewLocationRepository(pool),
		ObjectRepo:             postgres.NewObjectRepository(pool),
		PropertyRepo:           postgres.NewPropertyRepository(pool),
		ParentLocationResolver: postgres.NewParentLocationResolver(pool),
		RoleStore:              roleStore,
		AuditMode:              s.cfg.AuditMode,
		CryptoOperators:        operators,
		PlayerKindLookup: func(ctx context.Context, playerID string) (bool, error) {
			id, err := ulid.Parse(playerID)
			if err != nil {
				return false, oops.Code("INVALID_PLAYER_ID").With("player_id", playerID).Wrap(err)
			}
			player, err := playerRepo.GetByID(ctx, id)
			if err != nil {
				return false, oops.Wrap(err)
			}
			return player.IsGuest, nil
		},
	})
	if err != nil {
		return oops.Code("ABAC_SETUP_FAILED").Wrap(err)
	}
	s.stack = stack

	// Register health tracker with readiness registry.
	if s.cfg.Registry != nil {
		s.cfg.Registry.Register(lifecycle.SubsystemABAC, stack.HealthTracker)
	}

	slog.InfoContext(ctx, "ABAC subsystem prepared")
	return nil
}

// Activate starts the policy poller — a domain work loop (D-13.3 row 3).
// Idempotent: guarded on the poller's own phase-owned state (pollerCancel),
// not on s.stack, so a repeated Activate does not launch a duplicate poller
// goroutine.
// codecov:ignore — tested by integration and E2E tests
func (s *ABACSubsystem) Activate(ctx context.Context) error {
	if s.pollerCancel != nil {
		return nil // already activated — guard against a duplicate poller goroutine
	}

	// Start policy poller with background context so it outlives the startup context.
	// Shutdown is driven by pollerCancel, not the caller's ctx.
	pollerCtx, pollerCancel := context.WithCancel(context.Background())
	s.pollerCancel = pollerCancel
	go s.stack.Poller.Run(pollerCtx)

	slog.InfoContext(ctx, "ABAC subsystem activated")
	return nil
}

// Stop cancels the poller and closes the ABAC stack.
// codecov:ignore — tested by integration and E2E tests
func (s *ABACSubsystem) Stop(_ context.Context) error {
	if s.pollerCancel != nil {
		s.pollerCancel()
	}
	if s.stack != nil {
		return s.stack.Close()
	}
	return nil
}

// Engine returns the ABAC policy engine. Panics if called before Prepare().
func (s *ABACSubsystem) Engine() types.AccessPolicyEngine {
	if s.stack == nil {
		panic("setup: Engine() called before Prepare()")
	}
	return s.stack.Engine
}

// PolicyStore returns the policy store (with invalidation hook wired).
// Panics if called before Prepare().
func (s *ABACSubsystem) PolicyStore() policystore.PolicyStore {
	if s.stack == nil {
		panic("setup: PolicyStore() called before Prepare()")
	}
	return s.stack.PolicyStore
}

// PolicyInstaller returns the plugin policy installer. Panics if called before Prepare().
func (s *ABACSubsystem) PolicyInstaller() *plugins.PolicyInstaller {
	if s.stack == nil {
		panic("setup: PolicyInstaller() called before Prepare()")
	}
	return s.stack.PolicyInstaller
}

// PluginProvider returns the ABAC plugin attribute provider. Panics if called before Prepare().
func (s *ABACSubsystem) PluginProvider() *attribute.PluginProvider {
	if s.stack == nil {
		panic("setup: PluginProvider() called before Prepare()")
	}
	return s.stack.PluginProvider
}

// HealthTracker returns the health tracker. Panics if called before Prepare().
func (s *ABACSubsystem) HealthTracker() *lifecycle.HealthTracker {
	if s.stack == nil {
		panic("setup: HealthTracker() called before Prepare()")
	}
	return s.stack.HealthTracker
}

// Resolver returns the attribute resolver as the narrow access.SubjectResolver
// interface. The concrete *attribute.Resolver also exposes mutator methods
// (RegisterProvider, UnregisterProvider, RegisterEnvironmentProvider) that
// callers MUST NOT touch after Start — narrowing the return type makes that
// invariant compile-time enforceable. Panics if called before Prepare().
func (s *ABACSubsystem) Resolver() access.SubjectResolver {
	if s.stack == nil {
		panic("setup: Resolver() called before Prepare()")
	}
	return s.stack.Resolver
}

// AttributeResolver returns the concrete *attribute.Resolver so that wiring
// layers (e.g. the plugin subsystem) can register plugin-declared attribute
// providers via RegisterProvider/UnregisterProvider callbacks. This is the
// same resolver instance passed to policy.NewEngine during BuildABACStack, so
// registrations take effect immediately on the live engine. Panics if called
// before Start().
func (s *ABACSubsystem) AttributeResolver() *attribute.Resolver {
	if s.stack == nil {
		panic("setup: AttributeResolver() called before Prepare()")
	}
	return s.stack.Resolver
}

// AuditLogger returns the audit logger from the ABAC stack as a
// pluginauthz.Auditor. The concrete type is *audit.Logger; callers may pass
// the return value directly to goplugin.WithAuditLogger or
// hostfunc.WithAuditLogger to satisfy spec §5 / INV-PLUGIN-25. Returns nil when
// AuditLogger is nil (audit logging disabled), giving callers a clean nil
// interface value rather than an interface wrapping a nil pointer.
// Panics if called before Prepare().
func (s *ABACSubsystem) AuditLogger() pluginauthz.Auditor {
	if s.stack == nil {
		panic("setup: AuditLogger() called before Prepare()")
	}
	if s.stack.AuditLogger == nil {
		return nil
	}
	return s.stack.AuditLogger
}
