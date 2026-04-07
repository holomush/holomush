// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go"

	policy "github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/audit"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	policytypes "github.com/holomush/holomush/internal/access/policy/types"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/store"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"github.com/holomush/holomush/test/testutil"
)

// abacWidgetBinaryPath returns the plugin directory and binary path for the
// test-abac-widget plugin.
func abacWidgetBinaryPath() (string, string) {
	dir := filepath.Join(pluginBinaryDir(), "test-abac-widget")
	platformDir := runtime.GOOS + "-" + runtime.GOARCH
	return dir, filepath.Join(dir, platformDir, "test-abac-widget")
}

// loadWidgetManifest reads and parses the test-abac-widget plugin.yaml.
func loadWidgetManifest() *plugins.Manifest {
	pluginDir, _ := abacWidgetBinaryPath()
	data, err := os.ReadFile(filepath.Join(pluginDir, "plugin.yaml"))
	Expect(err).NotTo(HaveOccurred())

	manifest, err := plugins.ParseManifest(data)
	Expect(err).NotTo(HaveOccurred())
	return manifest
}

var _ = Describe("Plugin ABAC Trust Boundary", func() {
	// ---------------------------------------------------------------
	// Tests 6 & 7: Policy installer validation (no DB needed)
	// ---------------------------------------------------------------
	Describe("policy installer validation", func() {
		It("rejects a plugin policy targeting a protected resource type", func() {
			fs := &fakePolicyStoreWriter{}
			installer := plugins.NewPolicyInstaller(fs)

			manifest := &plugins.Manifest{
				Name:          "evil-plugin",
				Version:       "1.0.0",
				Type:          plugins.TypeBinary,
				ResourceTypes: []string{"widget"},
			}

			policies := []plugins.ManifestPolicy{
				{
					Name: "grab-locations",
					DSL:  `permit(principal is character, action in ["read"], resource is location);`,
				},
			}

			err := installer.InstallPluginPoliciesWithManifest(context.Background(), manifest, policies)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("protected"))
		})

		It("rejects trust escalation when manifest requests it but server allowlist is empty", func() {
			fs := &fakePolicyStoreWriter{}
			installer := plugins.NewPolicyInstaller(fs)
			// Empty allowlist — sneaky-plugin is NOT allowlisted server-side.
			installer.SetTrustAllowlist([]string{})

			// Manifest explicitly requests trust escalation (AllPrincipals: true).
			// The server-side allowlist must still gate it; without an entry
			// for "sneaky-plugin", the escalation must be denied even though
			// the manifest asks for it. This proves both halves of the
			// "manifest declaration AND server allowlist" gate are enforced.
			manifest := &plugins.Manifest{
				Name:          "sneaky-plugin",
				Version:       "1.0.0",
				Type:          plugins.TypeBinary,
				ResourceTypes: []string{"gadget"},
				Trust:         &plugins.TrustConfig{AllPrincipals: true},
			}

			policies := []plugins.ManifestPolicy{
				{
					Name: "grab-locations",
					DSL:  `permit(principal is character, action in ["read"], resource is location);`,
				},
			}

			err := installer.InstallPluginPoliciesWithManifest(context.Background(), manifest, policies)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("protected"))
		})
	})

	// ---------------------------------------------------------------
	// Tests 1 & 2: Plugin load + policy installation (need DB + binary)
	// ---------------------------------------------------------------
	Describe("plugin load and policy installation", func() {
		var (
			ctx       context.Context
			cancel    context.CancelFunc
			container testcontainers.Container
			connStr   string
		)

		BeforeEach(func() {
			_, binaryPath := abacWidgetBinaryPath()
			if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
				Skip(fmt.Sprintf("test-abac-widget binary not found at %s — run 'task plugin:build-all' first",
					binaryPath))
			}

			pluginDir, _ := abacWidgetBinaryPath()
			if _, err := os.Stat(filepath.Join(pluginDir, "plugin.yaml")); os.IsNotExist(err) {
				Skip(fmt.Sprintf("plugin.yaml not found at %s/plugin.yaml — run 'task plugin:build-all' first", pluginDir))
			}

			ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)

			pgEnv, err := testutil.StartPostgres(ctx)
			Expect(err).NotTo(HaveOccurred())
			container = pgEnv.Container
			connStr = pgEnv.ConnStr

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(migrator.Up()).To(Succeed())
			_ = migrator.Close()
		})

		AfterEach(func() {
			if container != nil {
				_ = container.Terminate(context.Background())
			}
			if cancel != nil {
				cancel()
			}
		})

		It("loads the test-abac-widget plugin with resource_types and schema discovery", func() {
			provisioner := plugins.NewSchemaProvisioner(connStr)
			Expect(provisioner.Init(ctx)).To(Succeed())
			defer provisioner.Close()

			registry := plugins.NewServiceRegistry()
			host := goplugin.NewHost(
				goplugin.WithSchemaProvisioner(provisioner),
				goplugin.WithServiceRegistry(registry),
			)
			defer func() { _ = host.Close(ctx) }()

			manifest := loadWidgetManifest()
			pluginDir, _ := abacWidgetBinaryPath()
			Expect(host.Load(ctx, manifest, pluginDir)).To(Succeed())

			Expect(host.Plugins()).To(ContainElement("test-abac-widget"))

			// Verify the attribute resolver client is available (schema discovery worked)
			arClient := host.AttributeResolverClient("test-abac-widget")
			Expect(arClient).NotTo(BeNil())
		})

		It("installs character-level policies from the manifest into the policy store", func() {
			provisioner := plugins.NewSchemaProvisioner(connStr)
			Expect(provisioner.Init(ctx)).To(Succeed())
			defer provisioner.Close()

			registry := plugins.NewServiceRegistry()
			host := goplugin.NewHost(
				goplugin.WithSchemaProvisioner(provisioner),
				goplugin.WithServiceRegistry(registry),
			)
			defer func() { _ = host.Close(ctx) }()

			manifest := loadWidgetManifest()
			pluginDir, _ := abacWidgetBinaryPath()
			Expect(host.Load(ctx, manifest, pluginDir)).To(Succeed())

			pool, err := pgxpool.New(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			defer pool.Close()

			ps := policystore.NewPostgresStore(pool)
			installer := plugins.NewPolicyInstaller(ps)

			// Install policies using the manifest-aware installer (trust boundary validation)
			Expect(installer.InstallPluginPoliciesWithManifest(ctx, manifest, manifest.Policies)).To(Succeed())

			// Query the policy store for plugin-sourced policies
			pluginPolicies, err := ps.List(ctx, policystore.ListOptions{Source: "plugin"})
			Expect(err).NotTo(HaveOccurred())

			policyNames := make([]string, len(pluginPolicies))
			for i, p := range pluginPolicies {
				policyNames[i] = p.Name
			}
			Expect(policyNames).To(ContainElement("plugin:test-abac-widget:widget-execute"))
			Expect(policyNames).To(ContainElement("plugin:test-abac-widget:widget-read-normal"))
			Expect(policyNames).To(ContainElement("plugin:test-abac-widget:widget-forbid-restricted"))
			Expect(pluginPolicies).To(HaveLen(3))
		})
	})

	// ---------------------------------------------------------------
	// Tests 3-5: Full ABAC engine pipeline with plugin-resolved attributes
	// ---------------------------------------------------------------
	Describe("full ABAC engine evaluation with plugin policies", func() {
		var (
			ctx         context.Context
			cancel      context.CancelFunc
			container   testcontainers.Container
			connStr     string
			host        *goplugin.Host
			ps          *policystore.PostgresStore
			engine      *policy.Engine
			pool        *pgxpool.Pool
			provisioner *plugins.SchemaProvisioner
		)

		BeforeEach(func() {
			_, binaryPath := abacWidgetBinaryPath()
			if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
				Skip(fmt.Sprintf("test-abac-widget binary not found at %s — run 'task plugin:build-all' first",
					binaryPath))
			}

			pluginDir, _ := abacWidgetBinaryPath()
			if _, err := os.Stat(filepath.Join(pluginDir, "plugin.yaml")); os.IsNotExist(err) {
				Skip(fmt.Sprintf("plugin.yaml not found at %s/plugin.yaml — run 'task plugin:build-all' first", pluginDir))
			}

			ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)

			// Start postgres and run migrations.
			pgEnv, err := testutil.StartPostgres(ctx)
			Expect(err).NotTo(HaveOccurred())
			container = pgEnv.Container
			connStr = pgEnv.ConnStr

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(migrator.Up()).To(Succeed())
			_ = migrator.Close()

			// Load the plugin binary. The provisioner must outlive BeforeEach
			// so the host can use it during the spec — closed in AfterEach.
			provisioner = plugins.NewSchemaProvisioner(connStr)
			Expect(provisioner.Init(ctx)).To(Succeed())

			svcRegistry := plugins.NewServiceRegistry()
			host = goplugin.NewHost(
				goplugin.WithSchemaProvisioner(provisioner),
				goplugin.WithServiceRegistry(svcRegistry),
			)

			manifest := loadWidgetManifest()
			Expect(host.Load(ctx, manifest, pluginDir)).To(Succeed())

			// Install policies into the postgres store.
			pool, err = pgxpool.New(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())

			ps = policystore.NewPostgresStore(pool)
			installer := plugins.NewPolicyInstaller(ps)
			Expect(installer.InstallPluginPoliciesWithManifest(ctx, manifest, manifest.Policies)).To(Succeed())

			// Build the ABAC engine stack:
			// 1. Schema registry + attribute resolver
			schemaRegistry := attribute.NewSchemaRegistry()
			resolver := attribute.NewResolver(schemaRegistry)

			// 2. Register the command attribute provider (for resource.command.name)
			cmdProvider := attribute.NewCommandProvider()
			Expect(resolver.RegisterProvider(cmdProvider)).To(Succeed())

			// 3. Discover widget schema from the plugin and register proxy provider.
			arClient := host.AttributeResolverClient("test-abac-widget")
			Expect(arClient).NotTo(BeNil())

			schemaResp, schemaErr := arClient.GetSchema(ctx, &pluginv1.GetSchemaRequest{})
			Expect(schemaErr).NotTo(HaveOccurred())

			schemas := plugins.ConvertProtoSchema(schemaResp)
			Expect(schemas).To(HaveKey("widget"))

			widgetProvider := plugins.NewPluginAttributeProvider("widget", arClient, schemas["widget"])
			Expect(resolver.RegisterProvider(widgetProvider)).To(Succeed())

			// 4. Create compiler and cache, then reload.
			compiler := policy.NewCompiler(schemaRegistry.Schema())
			cache := policy.NewCache(ps, compiler)
			Expect(cache.Reload(ctx)).To(Succeed())

			// 5. Create audit logger (minimal, in-memory writer).
			auditWriter := &testAuditWriter{}
			tmpDir := GinkgoT().TempDir()
			auditLogger := audit.NewLogger(audit.ModeAll, auditWriter, filepath.Join(tmpDir, "test-wal.jsonl"))

			// 6. Create mock session resolver (tests use character: subjects directly).
			sessionResolver := &testSessionResolver{}

			// 7. Assemble the engine.
			engine = policy.NewEngine(resolver, cache, sessionResolver, auditLogger)
		})

		AfterEach(func() {
			if host != nil {
				_ = host.Close(ctx)
			}
			if provisioner != nil {
				provisioner.Close()
			}
			if pool != nil {
				pool.Close()
			}
			if container != nil {
				_ = container.Terminate(context.Background())
			}
			if cancel != nil {
				cancel()
			}
		})

		It("permits command execution via Layer 1 execute policy from plugin", func() {
			// The widget-execute policy:
			// permit(principal is character, action in ["execute"], resource is command) when { resource.command.name == "widget" };
			req, err := policytypes.NewAccessRequest("character:01ABC", "execute", "command:widget")
			Expect(err).NotTo(HaveOccurred())

			decision, err := engine.Evaluate(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(decision.IsAllowed()).To(BeTrue(), "widget-execute policy should permit Layer 1 command execution")
			Expect(decision.Effect()).To(Equal(policytypes.EffectAllow))
		})

		It("permits reading a normal widget via plugin-resolved attributes", func() {
			// The widget-read-normal policy:
			// permit(principal is character, action in ["read"], resource is widget) when { resource.widget.type == "normal" };
			//
			// The plugin's AttributeResolver resolves widget:normal-1 → {type: "normal"}
			req, err := policytypes.NewAccessRequest("character:01ABC", "read", "widget:normal-1")
			Expect(err).NotTo(HaveOccurred())

			decision, err := engine.Evaluate(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(decision.IsAllowed()).To(BeTrue(), "widget-read-normal policy should permit reading normal widgets")
			Expect(decision.Effect()).To(Equal(policytypes.EffectAllow))
		})

		It("forbids reading a restricted widget via plugin-resolved attributes", func() {
			// The widget-forbid-restricted policy:
			// forbid(principal is character, action in ["read"], resource is widget) when { resource.widget.type == "restricted" };
			//
			// The plugin's AttributeResolver resolves widget:restricted-1 → {type: "restricted"}
			req, err := policytypes.NewAccessRequest("character:01ABC", "read", "widget:restricted-1")
			Expect(err).NotTo(HaveOccurred())

			decision, err := engine.Evaluate(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(decision.IsAllowed()).To(BeFalse(), "widget-forbid-restricted policy should deny reading restricted widgets")
			Expect(decision.Effect()).To(Equal(policytypes.EffectDeny))
		})
	})

	// ---------------------------------------------------------------
	// Plugin ABAC Hardening (spec 2026-04-07): Sharp Edge 1 tests
	// ---------------------------------------------------------------
	Describe("plugin ResolveResource call semantics under hardening", func() {
		var (
			ctx         context.Context
			cancel      context.CancelFunc
			container   testcontainers.Container
			connStr     string
			host        *goplugin.Host
			ps          *policystore.PostgresStore
			engine      *policy.Engine
			pool        *pgxpool.Pool
			provisioner *plugins.SchemaProvisioner
			countingAR  *countingAttributeResolverClient
		)

		BeforeEach(func() {
			_, binaryPath := abacWidgetBinaryPath()
			if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
				Skip(fmt.Sprintf("test-abac-widget binary not found at %s — run 'task plugin:build-all' first",
					binaryPath))
			}

			pluginDir, _ := abacWidgetBinaryPath()
			if _, err := os.Stat(filepath.Join(pluginDir, "plugin.yaml")); os.IsNotExist(err) {
				Skip(fmt.Sprintf("plugin.yaml not found at %s/plugin.yaml — run 'task plugin:build-all' first", pluginDir))
			}

			ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)

			pgEnv, err := testutil.StartPostgres(ctx)
			Expect(err).NotTo(HaveOccurred())
			container = pgEnv.Container
			connStr = pgEnv.ConnStr

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(migrator.Up()).To(Succeed())
			_ = migrator.Close()

			provisioner = plugins.NewSchemaProvisioner(connStr)
			Expect(provisioner.Init(ctx)).To(Succeed())

			svcRegistry := plugins.NewServiceRegistry()
			host = goplugin.NewHost(
				goplugin.WithSchemaProvisioner(provisioner),
				goplugin.WithServiceRegistry(svcRegistry),
			)

			manifest := loadWidgetManifest()
			Expect(host.Load(ctx, manifest, pluginDir)).To(Succeed())

			pool, err = pgxpool.New(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			ps = policystore.NewPostgresStore(pool)
			installer := plugins.NewPolicyInstaller(ps)
			Expect(installer.InstallPluginPoliciesWithManifest(ctx, manifest, manifest.Policies)).To(Succeed())

			// Wire the counting proxy in place of the raw plugin client.
			rawClient := host.AttributeResolverClient("test-abac-widget")
			Expect(rawClient).NotTo(BeNil())
			countingAR = newCountingAttributeResolverClient(rawClient)

			// Build the engine stack using the counting proxy when
			// registering the attribute provider.
			schemaRegistry := attribute.NewSchemaRegistry()
			resolver := attribute.NewResolver(schemaRegistry)

			cmdProvider := attribute.NewCommandProvider()
			Expect(resolver.RegisterProvider(cmdProvider)).To(Succeed())

			schemaResp, schemaErr := countingAR.GetSchema(ctx, &pluginv1.GetSchemaRequest{})
			Expect(schemaErr).NotTo(HaveOccurred())
			schemas := plugins.ConvertProtoSchema(schemaResp)
			Expect(schemas).To(HaveKey("widget"))

			widgetProvider := plugins.NewPluginAttributeProvider("widget", countingAR, schemas["widget"])
			Expect(resolver.RegisterProvider(widgetProvider)).To(Succeed())

			compiler := policy.NewCompiler(schemaRegistry.Schema())
			cache := policy.NewCache(ps, compiler)
			Expect(cache.Reload(ctx)).To(Succeed())

			auditWriter := &testAuditWriter{}
			tmpDir := GinkgoT().TempDir()
			auditLogger := audit.NewLogger(audit.ModeAll, auditWriter, filepath.Join(tmpDir, "test-wal.jsonl"))

			sessionResolver := &testSessionResolver{}
			engine = policy.NewEngine(resolver, cache, sessionResolver, auditLogger)

			// Reset counters after BeforeEach so test assertions measure only
			// the activity that the test body triggers.
			countingAR.ResetCallCounts()
		})

		AfterEach(func() {
			if host != nil {
				_ = host.Close(ctx)
			}
			if provisioner != nil {
				provisioner.Close()
			}
			if pool != nil {
				pool.Close()
			}
			if container != nil {
				_ = container.Terminate(context.Background())
			}
			if cancel != nil {
				cancel()
			}
		})

		It("never invokes the plugin ResolveResource RPC during type-level preflight", func() {
			// T13: The C1 invariant at E2E layer with a real plugin binary.
			allowed, err := engine.CanPerformAction(ctx, "character:01ABC", "read", "widget", "self")
			Expect(err).NotTo(HaveOccurred())
			Expect(allowed).To(BeTrue(), "preflight should permit via optimistic branch")

			Expect(countingAR.ResolveResourceCallCount()).To(BeEquivalentTo(0),
				"ResolveResource MUST NOT be called during type-level preflight")
		})

		It("still invokes the plugin ResolveResource RPC for instance-level Evaluate", func() {
			// T14: Instance-level evaluation is unaffected.
			req, reqErr := policytypes.NewAccessRequest("character:01ABC", "read", "widget:normal-1")
			Expect(reqErr).NotTo(HaveOccurred())

			decision, err := engine.Evaluate(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(decision.IsAllowed()).To(BeTrue())

			Expect(countingAR.ResolveResourceCallCount()).To(BeEquivalentTo(1),
				"ResolveResource should be called exactly once for one Evaluate")
		})

		It("permits character:01ABC execute widget command via full database-backed engine stack without invoking plugin ResolveResource", func() {
			// T38: Full DB-backed stack, CanPerformAction on command execution,
			// counter asserted zero.
			allowed, err := engine.CanPerformAction(ctx, "character:01ABC", "execute", "command", "self")
			Expect(err).NotTo(HaveOccurred())
			Expect(allowed).To(BeTrue())

			Expect(countingAR.ResolveResourceCallCount()).To(BeEquivalentTo(0),
				"execute command preflight must not touch the plugin")
		})
	})
})

// testAuditWriter captures audit entries in memory for testing.
type testAuditWriter struct {
	entries []audit.Entry
	mu      sync.Mutex
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

// testSessionResolver is a no-op for tests using character: subjects directly.
type testSessionResolver struct{}

func (r *testSessionResolver) ResolveSession(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("session resolution not configured in test")
}

// fakePolicyStoreWriter is a minimal in-memory writer for unit-style installer
// tests that don't need a real database.
type fakePolicyStoreWriter struct {
	created []*policystore.StoredPolicy
}

func (f *fakePolicyStoreWriter) Create(_ context.Context, p *policystore.StoredPolicy) error {
	f.created = append(f.created, p)
	return nil
}

func (f *fakePolicyStoreWriter) CreateBatch(_ context.Context, policies []*policystore.StoredPolicy) error {
	f.created = append(f.created, policies...)
	return nil
}

func (f *fakePolicyStoreWriter) DeleteBySource(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

func (f *fakePolicyStoreWriter) ReplaceBySource(_ context.Context, _, _ string, policies []*policystore.StoredPolicy) error {
	f.created = append(f.created, policies...)
	return nil
}
