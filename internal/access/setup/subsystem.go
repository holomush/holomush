// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
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
	// CryptoOperators is the list of player IDs (ULIDs) holding the
	// crypto.operator capability. Forwarded verbatim to ABACConfig and
	// ultimately to PlayerAttributeProvider. Empty / nil → no operators
	// (break-glass disabled). Sub-epic B (Phase 5).
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

// Start builds the ABAC stack, registers health, and starts the poller.
// Start is idempotent: if the subsystem is already started, it returns nil
// immediately. This allows the ABAC subsystem to be pre-started in core
// boot when admin handler construction needs Resolver() before the
// orchestrator drives StartAll. Mirrors store.DatabaseSubsystem.Start.
// codecov:ignore — tested by integration and E2E tests
func (s *ABACSubsystem) Start(ctx context.Context) error {
	if s.stack != nil {
		return nil // already started — guard against double-start (would launch a duplicate poller goroutine)
	}
	pool := s.cfg.DB.Pool()

	roleStore := store.NewPostgresRoleStore(pool)
	stack, err := BuildABACStack(ctx, ABACConfig{
		Pool:                   pool,
		CharacterRepo:          postgres.NewCharacterRepository(pool),
		LocationRepo:           postgres.NewLocationRepository(pool),
		ObjectRepo:             postgres.NewObjectRepository(pool),
		PropertyRepo:           postgres.NewPropertyRepository(pool),
		ParentLocationResolver: postgres.NewParentLocationResolver(pool),
		RoleStore:              roleStore,
		AuditMode:              s.cfg.AuditMode,
		CryptoOperators:        s.cfg.CryptoOperators,
	})
	if err != nil {
		return oops.Code("ABAC_SETUP_FAILED").Wrap(err)
	}
	s.stack = stack

	// Register health tracker with readiness registry.
	if s.cfg.Registry != nil {
		s.cfg.Registry.Register(lifecycle.SubsystemABAC, stack.HealthTracker)
	}

	// Start policy poller with background context so it outlives the startup context.
	// Shutdown is driven by pollerCancel, not the caller's ctx.
	pollerCtx, pollerCancel := context.WithCancel(context.Background())
	s.pollerCancel = pollerCancel
	go stack.Poller.Run(pollerCtx)

	slog.InfoContext(ctx, "ABAC subsystem started")
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

// Engine returns the ABAC policy engine. Panics if called before Start().
func (s *ABACSubsystem) Engine() types.AccessPolicyEngine {
	if s.stack == nil {
		panic("setup: Engine() called before Start()")
	}
	return s.stack.Engine
}

// PolicyStore returns the policy store (with invalidation hook wired).
// Panics if called before Start().
func (s *ABACSubsystem) PolicyStore() policystore.PolicyStore {
	if s.stack == nil {
		panic("setup: PolicyStore() called before Start()")
	}
	return s.stack.PolicyStore
}

// PolicyInstaller returns the plugin policy installer. Panics if called before Start().
func (s *ABACSubsystem) PolicyInstaller() *plugins.PolicyInstaller {
	if s.stack == nil {
		panic("setup: PolicyInstaller() called before Start()")
	}
	return s.stack.PolicyInstaller
}

// PluginProvider returns the ABAC plugin attribute provider. Panics if called before Start().
func (s *ABACSubsystem) PluginProvider() *attribute.PluginProvider {
	if s.stack == nil {
		panic("setup: PluginProvider() called before Start()")
	}
	return s.stack.PluginProvider
}

// HealthTracker returns the health tracker. Panics if called before Start().
func (s *ABACSubsystem) HealthTracker() *lifecycle.HealthTracker {
	if s.stack == nil {
		panic("setup: HealthTracker() called before Start()")
	}
	return s.stack.HealthTracker
}

// Resolver returns the attribute resolver as the narrow access.SubjectResolver
// interface. The concrete *attribute.Resolver also exposes mutator methods
// (RegisterProvider, UnregisterProvider, RegisterEnvironmentProvider) that
// callers MUST NOT touch after Start — narrowing the return type makes that
// invariant compile-time enforceable. Panics if called before Start().
func (s *ABACSubsystem) Resolver() access.SubjectResolver {
	if s.stack == nil {
		panic("setup: Resolver() called before Start()")
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
		panic("setup: AttributeResolver() called before Start()")
	}
	return s.stack.Resolver
}

// AuditLogger returns the audit logger from the ABAC stack as a
// pluginauthz.Auditor. The concrete type is *audit.Logger; callers may pass
// the return value directly to goplugin.WithAuditLogger or
// hostfunc.WithAuditLogger to satisfy spec §5 / INV-PLUGIN-25. Returns nil when
// AuditLogger is nil (audit logging disabled), giving callers a clean nil
// interface value rather than an interface wrapping a nil pointer.
// Panics if called before Start().
func (s *ABACSubsystem) AuditLogger() pluginauthz.Auditor {
	if s.stack == nil {
		panic("setup: AuditLogger() called before Start()")
	}
	if s.stack.AuditLogger == nil {
		return nil
	}
	return s.stack.AuditLogger
}
