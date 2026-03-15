// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package setup provides production wiring for the ABAC access control stack.
package setup

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/audit"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/world"
)

// ABACStack holds all ABAC subsystem components constructed by BuildABACStack.
type ABACStack struct {
	Engine          types.AccessPolicyEngine
	Cache           *policy.Cache
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
	AuditMode     audit.Mode
}

// BuildABACStack constructs all ABAC components in the correct dependency order.
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
		charProvider := attribute.NewCharacterProvider(cfg.CharacterRepo, nil)
		if err := resolver.RegisterProvider(charProvider); err != nil {
			return nil, eb.Wrapf(err, "register character provider")
		}
	}

	// 9. Plugin provider (nil registry — two-phase init)
	pluginProvider := attribute.NewPluginProvider(nil)
	if err := resolver.RegisterProvider(pluginProvider); err != nil {
		return nil, eb.Wrapf(err, "register plugin provider")
	}

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
		slog.Warn("audit WAL replay failed (non-fatal)", "error", err)
	}

	// 15. Session resolver (no-op — fails closed)
	sessionRes := &noopSessionResolver{}

	// 16. Engine
	engine := policy.NewEngine(resolver, cache, sessionRes, auditLogger)

	// 17. Policy installer
	installer := plugins.NewPolicyInstaller(ps)

	return &ABACStack{
		Engine:          engine,
		Cache:           cache,
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

func (n *noopSessionResolver) ResolveSession(_ context.Context, sessionID string) (string, error) {
	return "", oops.Code("SESSION_INVALID").
		With("session", sessionID).
		Errorf("session resolution not yet implemented")
}

// NewNoopSessionResolver creates a session resolver that rejects all sessions.
func NewNoopSessionResolver() policy.SessionResolver {
	return &noopSessionResolver{}
}
