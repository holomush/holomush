// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package access_test

import (
	"context"
	"encoding/json"
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
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/test/testutil"
	"github.com/oklog/ulid/v2"
)

var suiteT *testing.T

func TestAccessIntegration(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "ABAC Integration Suite")
}

type accessTestEnv struct {
	ctx               context.Context
	pool              *pgxpool.Pool
	engine            *policy.Engine
	pStore            policystore.PolicyStore
	cache             *policy.Cache
	charRepo          *worldpg.CharacterRepository
	locRepo           *worldpg.LocationRepository
	objRepo           *worldpg.ObjectRepository
	auditWriter       *testAuditWriter
	auditLogger       *audit.Logger
	propRepo          *worldpg.PropertyRepository
	parentLocResolver *worldpg.ParentLocationResolver
	propProvider      *attribute.PropertyProvider
	worldService      *world.Service
	// roleResolver is exposed so tests can assign roles to subjects
	// (e.g. env.roleResolver.roles[access.CharacterSubject(adminID)] = []string{"admin"})
	// for tests that exercise admin-visibility property seeds.
	roleResolver *staticRoleResolver
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
	// the REAL provider stack (the integrationtest harness uses allowAllPolicyEngine
	// and would silently pass even with the provider missing — exactly the
	// blind spot that hid the g776/xxel/k3ud bugs for weeks).
	objProvider := attribute.NewObjectProvider(objRepo, charRepo)
	if err := resolver.RegisterProvider(objProvider); err != nil {
		pool.Close()
		return nil, err
	}

	// holomush-72ou: PropertyProvider needs a postgres-layer ParentLocationResolver
	// so property visibility seeds (public-read / private-read / admin-read /
	// owner-write / restricted-visible-to / restricted-excluded) evaluate against
	// real attributes via the REAL ABAC engine. Mirrors the k3ud/g776 pattern.
	propRepo := worldpg.NewPropertyRepository(pool)
	parentLocResolver := worldpg.NewParentLocationResolver(pool)
	propProvider := attribute.NewPropertyProvider(propRepo, parentLocResolver)
	if err := resolver.RegisterProvider(propProvider); err != nil {
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

	// Build world.Service with only PropertyRepo + Engine wired — sufficient
	// for ListPropertiesByParent integration tests (F1-F5). No EventEmitter
	// or Transactor needed since we only exercise the read-filter path.
	worldService := world.NewService(world.ServiceConfig{
		PropertyRepo: propRepo,
		Engine:       engine,
	})

	return &accessTestEnv{
		ctx:               ctx,
		pool:              pool,
		engine:            engine,
		pStore:            pStore,
		cache:             cache,
		charRepo:          charRepo,
		locRepo:           locRepo,
		objRepo:           objRepo,
		auditWriter:       testWriter,
		auditLogger:       auditLogger,
		propRepo:          propRepo,
		parentLocResolver: parentLocResolver,
		propProvider:      propProvider,
		worldService:      worldService,
		roleResolver:      roleResolver,
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

// insertProperty inserts an entity_properties row via raw SQL for test setup.
// Reads the package-global env (set in BeforeSuite). Mirrors the production
// PropertyRepository.Create JSON encoding for visible_to / excluded_from
// (JSONB columns, not text[]).
//
// DB constraint visibility_restricted_requires_lists: when visibility='restricted',
// BOTH visible_to AND excluded_from must be non-NULL. This helper emits an empty
// JSON array [] (not SQL NULL) for each omitted list when visibility='restricted'.
func insertProperty(parentType string, parentID ulid.ULID, name, value, visibility string, owner *ulid.ULID, visibleTo, excludedFrom []ulid.ULID) ulid.ULID {
	id := core.NewULID()
	var ownerStr *string
	if owner != nil {
		s := owner.String()
		ownerStr = &s
	}
	stringify := func(in []ulid.ULID) []string {
		if len(in) == 0 {
			return nil
		}
		out := make([]string, len(in))
		for i, u := range in {
			out[i] = u.String()
		}
		return out
	}

	vtStrs := stringify(visibleTo)
	efStrs := stringify(excludedFrom)

	// For restricted visibility the visibility_restricted_requires_lists CHECK
	// constraint (migration 000001) requires both lists non-NULL. Use an empty
	// JSON array [] (not SQL NULL) when the caller passed nil.
	var visibleToJSON, excludedFromJSON []byte
	var err error
	if visibility == "restricted" {
		if vtStrs == nil {
			vtStrs = []string{}
		}
		if efStrs == nil {
			efStrs = []string{}
		}
		visibleToJSON, err = json.Marshal(vtStrs)
		Expect(err).NotTo(HaveOccurred())
		excludedFromJSON, err = json.Marshal(efStrs)
		Expect(err).NotTo(HaveOccurred())
	} else {
		visibleToJSON, err = jsonNullableStringSlice(vtStrs)
		Expect(err).NotTo(HaveOccurred())
		excludedFromJSON, err = jsonNullableStringSlice(efStrs)
		Expect(err).NotTo(HaveOccurred())
	}

	_, err = env.pool.Exec(env.ctx, `
		INSERT INTO entity_properties (id, parent_type, parent_id, name, value, owner, visibility, flags, visible_to, excluded_from, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT, (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)`,
		id.String(), parentType, parentID.String(), name, value, ownerStr, visibility,
		[]byte("[]"), visibleToJSON, excludedFromJSON)
	Expect(err).NotTo(HaveOccurred())
	return id
}

// jsonNullableStringSlice mirrors production marshalNullableStringSlice:
// nil/empty input → SQL NULL (returned as nil []byte), non-empty →
// JSON-encoded array.
func jsonNullableStringSlice(s []string) ([]byte, error) {
	if len(s) == 0 {
		return nil, nil
	}
	return json.Marshal(s)
}
