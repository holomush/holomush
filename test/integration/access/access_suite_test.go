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

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/test/testutil"
)

var suiteT *testing.T

func TestAccessIntegration(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "ABAC Integration Suite")
}

type accessTestEnv struct {
	ctx         context.Context
	pool        *pgxpool.Pool
	engine      *policy.Engine
	pStore      policystore.PolicyStore
	cache       *policy.Cache
	charRepo    *worldpg.CharacterRepository
	locRepo     *worldpg.LocationRepository
	objRepo     *worldpg.ObjectRepository
	auditWriter *testAuditWriter
	auditLogger *audit.Logger
}

type testAuditWriter struct {
	mu      sync.Mutex
	entries []audit.Event
}

func (w *testAuditWriter) WriteSync(_ context.Context, event audit.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = append(w.entries, event)
	return nil
}

func (w *testAuditWriter) WriteAsync(event audit.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = append(w.entries, event)
	return nil
}

func (w *testAuditWriter) Close() error { return nil }

func (w *testAuditWriter) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = nil
}

func (w *testAuditWriter) Entries() []audit.Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	cp := make([]audit.Event, len(w.entries))
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
	roles map[string][]string
}

func (s *staticRoleResolver) GetRoles(_ context.Context, subject string) []string {
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

	shared := testutil.SharedPostgres(suiteT)
	connStr := testutil.FreshDatabase(suiteT, shared)

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, err
	}

	pStore := policystore.NewPostgresStore(pool)
	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)

	charRepo := worldpg.NewCharacterRepository(pool)
	locRepo := worldpg.NewLocationRepository(pool)
	objRepo := worldpg.NewObjectRepository(pool)

	roleResolver := &staticRoleResolver{roles: make(map[string][]string)}

	charProvider := attribute.NewCharacterProvider(charRepo, roleResolver)
	if err := resolver.RegisterProvider(charProvider); err != nil {
		pool.Close()
		return nil, err
	}

	locProvider := attribute.NewLocationProvider(locRepo)
	if err := resolver.RegisterProvider(locProvider); err != nil {
		pool.Close()
		return nil, err
	}

	// holomush-k3ud: ObjectProvider needs charRepo to walk held-by-character
	// chains. Registered here so seed:player-object-colocation evaluates via
	// the REAL provider stack (privacytest harness uses allowAllPolicyEngine
	// and would silently pass even with the provider missing — exactly the
	// blind spot that hid the g776/xxel/k3ud bugs for weeks).
	objProvider := attribute.NewObjectProvider(objRepo, charRepo)
	if err := resolver.RegisterProvider(objProvider); err != nil {
		pool.Close()
		return nil, err
	}

	compiler := policy.NewCompiler(registry.Schema())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := policy.Bootstrap(ctx, &noopPartitionCreator{}, pStore, compiler, logger, policy.BootstrapOptions{}); err != nil {
		pool.Close()
		return nil, err
	}

	cache := policy.NewCache(pStore, compiler)
	if err := cache.Reload(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	testWriter := &testAuditWriter{}
	walPath := filepath.Join(os.TempDir(), fmt.Sprintf("holomush-test-audit-%d.jsonl", os.Getpid()))
	auditLogger := audit.NewLogger(audit.ModeAll, testWriter, walPath)

	engine := policy.NewEngine(resolver, cache, &noopSessionResolver{}, auditLogger)

	return &accessTestEnv{
		ctx:         ctx,
		pool:        pool,
		engine:      engine,
		pStore:      pStore,
		cache:       cache,
		charRepo:    charRepo,
		locRepo:     locRepo,
		objRepo:     objRepo,
		auditWriter: testWriter,
		auditLogger: auditLogger,
	}, nil
}

func (e *accessTestEnv) cleanup() {
	if e.auditLogger != nil {
		_ = e.auditLogger.Close()
	}
	if e.pool != nil {
		e.pool.Close()
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
