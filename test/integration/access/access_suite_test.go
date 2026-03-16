// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package access_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/audit"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/store"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
)

func TestAccessIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ABAC Integration Suite")
}

type accessTestEnv struct {
	ctx         context.Context
	pool        *pgxpool.Pool
	container   testcontainers.Container
	engine      *policy.Engine
	pStore      policystore.PolicyStore
	cache       *policy.Cache
	charRepo    *worldpg.CharacterRepository
	locRepo     *worldpg.LocationRepository
	auditWriter  *testAuditWriter
	auditLogger  *audit.Logger
}

type testAuditWriter struct {
	mu      sync.Mutex
	entries []audit.Entry
}

func (w *testAuditWriter) WriteSync(_ context.Context, entry audit.Entry) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = append(w.entries, entry)
	return nil
}

func (w *testAuditWriter) WriteAsync(entry audit.Entry) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = append(w.entries, entry)
	return nil
}

func (w *testAuditWriter) Close() error { return nil }

func (w *testAuditWriter) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = nil
}

func (w *testAuditWriter) Entries() []audit.Entry {
	w.mu.Lock()
	defer w.mu.Unlock()
	cp := make([]audit.Entry, len(w.entries))
	copy(cp, w.entries)
	return cp
}

// noopSessionResolver returns an error for all session resolutions.
// Prevents nil pointer panics if Evaluate receives a "session:" subject.
type noopSessionResolver struct{}

func (n *noopSessionResolver) ResolveSession(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("session resolution not configured in test")
}

type noopPartitionCreator struct{}

func (n *noopPartitionCreator) EnsurePartitions(_ context.Context, _ int) error { return nil }

type staticRoleResolver struct {
	roles map[string]string
}

func (s *staticRoleResolver) GetRole(subject string) string {
	return s.roles[subject]
}

var env *accessTestEnv

var _ = BeforeSuite(func() {
	var err error
	env, err = setupAccessTestEnv()
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	if env != nil {
		env.cleanup()
	}
})

func setupAccessTestEnv() (*accessTestEnv, error) {
	ctx := context.Background()

	container, err := postgres.Run(ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("holomush_test"),
		postgres.WithUsername("holomush"),
		postgres.WithPassword("holomush"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		return nil, err
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, err
	}

	migrator, err := store.NewMigrator(connStr)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, err
	}
	if err := migrator.Up(); err != nil {
		_ = migrator.Close()
		_ = container.Terminate(ctx)
		return nil, err
	}
	_ = migrator.Close()

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, err
	}

	pStore := policystore.NewPostgresStore(pool)
	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)

	charRepo := worldpg.NewCharacterRepository(pool)
	locRepo := worldpg.NewLocationRepository(pool)

	roleResolver := &staticRoleResolver{roles: make(map[string]string)}

	charProvider := attribute.NewCharacterProvider(charRepo, roleResolver)
	if err := resolver.RegisterProvider(charProvider); err != nil {
		pool.Close()
		_ = container.Terminate(ctx)
		return nil, err
	}

	locProvider := attribute.NewLocationProvider(locRepo)
	if err := resolver.RegisterProvider(locProvider); err != nil {
		pool.Close()
		_ = container.Terminate(ctx)
		return nil, err
	}

	compiler := policy.NewCompiler(registry.Schema())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := policy.Bootstrap(ctx, &noopPartitionCreator{}, pStore, compiler, logger, policy.BootstrapOptions{}); err != nil {
		pool.Close()
		_ = container.Terminate(ctx)
		return nil, err
	}

	cache := policy.NewCache(pStore, compiler)
	if err := cache.Reload(ctx); err != nil {
		pool.Close()
		_ = container.Terminate(ctx)
		return nil, err
	}

	testWriter := &testAuditWriter{}
	walPath := filepath.Join(os.TempDir(), fmt.Sprintf("holomush-test-audit-%d.jsonl", os.Getpid()))
	auditLogger := audit.NewLogger(audit.ModeAll, testWriter, walPath)

	engine := policy.NewEngine(resolver, cache, &noopSessionResolver{}, auditLogger)

	return &accessTestEnv{
		ctx:         ctx,
		pool:        pool,
		container:   container,
		engine:      engine,
		pStore:      pStore,
		cache:       cache,
		charRepo:    charRepo,
		locRepo:     locRepo,
		auditWriter:  testWriter,
		auditLogger:  auditLogger,
	}, nil
}

func (e *accessTestEnv) cleanup() {
	if e.auditLogger != nil {
		_ = e.auditLogger.Close()
	}
	if e.pool != nil {
		e.pool.Close()
	}
	if e.container != nil {
		_ = e.container.Terminate(e.ctx)
	}
}

func evalAccess(subject, action, resource string) types.Decision {
	req := types.AccessRequest{
		Subject:  subject,
		Action:   action,
		Resource: resource,
	}
	decision, err := env.engine.Evaluate(env.ctx, req)
	Expect(err).NotTo(HaveOccurred())
	return decision
}
