// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
	"context"
	"database/sql"
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
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	policy "github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/audit"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	policytypes "github.com/holomush/holomush/internal/access/policy/types"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/store"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
	"github.com/holomush/holomush/test/testutil"
)

// pluginBinaryDir returns the directory containing the built core-scenes binary.
// Checks PLUGIN_BINARY_DIR env var first, then falls back to build/plugins in the repo root.
func pluginBinaryDir() string {
	if dir := os.Getenv("PLUGIN_BINARY_DIR"); dir != "" {
		return dir
	}
	// Walk up from the test file to the repo root (test/integration/plugin → repo root)
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(repoRoot, "build", "plugins")
}

// coreScenesBinaryPath returns the path to the core-scenes binary and its plugin dir.
// Returns (pluginDir, binaryPath). The binary lives under a platform-specific
// subdirectory: <pluginDir>/<os>-<arch>/<executable>.
func coreScenesBinaryPath() (string, string) {
	dir := filepath.Join(pluginBinaryDir(), "core-scenes")
	platformDir := runtime.GOOS + "-" + runtime.GOARCH
	return dir, filepath.Join(dir, platformDir, "core-scenes")
}

var _ = Describe("Binary Plugin Lifecycle", func() {
	var (
		ctx       context.Context
		cancel    context.CancelFunc
		container testcontainers.Container
		connStr   string
	)

	BeforeEach(func() {
		// Check that the plugin binary exists before proceeding
		pluginDir, binaryPath := coreScenesBinaryPath()
		if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
			Skip(fmt.Sprintf("core-scenes binary not found at %s — run 'GOOS=%s GOARCH=%s task plugin:build-all' first",
				binaryPath, runtime.GOOS, runtime.GOARCH))
		}
		if _, err := os.Stat(filepath.Join(pluginDir, "plugin.yaml")); os.IsNotExist(err) {
			Skip(fmt.Sprintf("plugin.yaml not found at %s/plugin.yaml — run 'task plugin:build-all' first", pluginDir))
		}

		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)

		// Start PostgreSQL via testcontainers
		pgEnv, err := testutil.StartPostgres(ctx)
		Expect(err).NotTo(HaveOccurred())
		container = pgEnv.Container
		connStr = pgEnv.ConnStr

		// Run core server migrations (needed for the base schema)
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

	Describe("plugin discovery, schema provisioning, and loading", func() {
		It("discovers core-scenes plugin from the build directory", func() {
			pluginsDir := pluginBinaryDir()
			manager := plugins.NewManager(pluginsDir)

			discovered, err := manager.Discover(ctx)
			Expect(err).NotTo(HaveOccurred())

			var found bool
			for _, dp := range discovered {
				if dp.Manifest.Name == "core-scenes" {
					found = true
					Expect(dp.Manifest.Type).To(Equal(plugins.TypeBinary))
					Expect(dp.Manifest.Storage).To(Equal(plugins.StoragePostgres))
					Expect(dp.Manifest.Provides).To(ContainElement("holomush.scene.v1.SceneService"))
					Expect(dp.Manifest.Requires).To(ContainElement("holomush.world.v1.WorldService"))
				}
			}
			Expect(found).To(BeTrue(), "core-scenes not discovered in %s", pluginsDir)
		})

		It("provisions a schema-isolated database for the plugin", func() {
			provisioner := plugins.NewSchemaProvisioner(connStr)
			Expect(provisioner.Init(ctx)).To(Succeed())
			defer provisioner.Close()

			scopedConn, err := provisioner.ProvisionSchema(ctx, "core-scenes")
			Expect(err).NotTo(HaveOccurred())
			Expect(scopedConn).To(ContainSubstring("search_path=plugin_core_scenes"))
		})
	})

	Describe("full binary plugin lifecycle via Manager", func() {
		It("loads core-scenes, registers SceneService, and answers RPCs", func() {
			// Create an isolated plugins dir containing only core-scenes so LoadAll
			// does not conflict with other test plugins (e.g. test-abac-widget) that
			// also provide holomush.plugin.v1.AttributeResolverService.
			//
			// Constraints:
			//   - Manager.Discover uses entry.IsDir() — symlinks to dirs are skipped
			//   - goplugin.Host uses EvalSymlinks + containment check — symlinked
			//     binaries that resolve outside the plugin dir are rejected
			// Solution: copy plugin.yaml + binary into a real directory structure.
			corePluginDir, coreBinaryPath := coreScenesBinaryPath()
			pluginsDir := GinkgoT().TempDir()
			coreSubDir := filepath.Join(pluginsDir, "core-scenes")
			platformDir := runtime.GOOS + "-" + runtime.GOARCH
			platformSubDir := filepath.Join(coreSubDir, platformDir)
			Expect(os.MkdirAll(platformSubDir, 0o755)).To(Succeed())
			// Copy plugin.yaml
			yamlSrc, yamlErr := os.ReadFile(filepath.Join(corePluginDir, "plugin.yaml"))
			Expect(yamlErr).NotTo(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(coreSubDir, "plugin.yaml"), yamlSrc, 0o644)).To(Succeed())
			// Copy binary
			binSrc, binErr := os.ReadFile(coreBinaryPath)
			Expect(binErr).NotTo(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(platformSubDir, "core-scenes"), binSrc, 0o755)).To(Succeed())

			// Set up schema provisioner
			provisioner := plugins.NewSchemaProvisioner(connStr)
			Expect(provisioner.Init(ctx)).To(Succeed())
			defer provisioner.Close()

			// Create a service registry with WorldService registered.
			// core-scenes declares requires: [holomush.world.v1.WorldService],
			// so the host must resolve it from the registry during Load.
			registry := plugins.NewServiceRegistry()
			worldSrv := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-memory bufconn only
			worldConn, worldConnErr := plugins.NewInProcessConn(worldSrv)
			Expect(worldConnErr).NotTo(HaveOccurred())
			defer func() { _ = worldConn.Close() }()

			Expect(registry.Register(plugins.RegisteredService{
				Name:       "holomush.world.v1.WorldService",
				Conn:       worldConn,
				PluginType: plugins.TypeServerInternal(),
			})).To(Succeed())

			// Create goplugin host with schema provisioner and service registry
			host := goplugin.NewHost(
				goplugin.WithSchemaProvisioner(provisioner),
				goplugin.WithServiceRegistry(registry),
			)
			defer func() { _ = host.Close(ctx) }()

			// Create manager with host and registry
			manager := plugins.NewManager(pluginsDir,
				plugins.WithServiceRegistry(registry),
			)
			manager.RegisterHost(plugins.TypeBinary, host)
			defer func() { _ = manager.Close(ctx) }()

			// LoadAll discovers + loads all plugins
			Expect(manager.LoadAll(ctx)).To(Succeed())

			// Verify the plugin is loaded
			loadedPlugins := manager.ListPlugins()
			Expect(loadedPlugins).To(ContainElement("core-scenes"))

			// Verify the service is registered in the registry
			svc, err := registry.Resolve("holomush.scene.v1.SceneService")
			Expect(err).NotTo(HaveOccurred())
			Expect(svc.Name).To(Equal("holomush.scene.v1.SceneService"))
			Expect(svc.PluginName).To(Equal("core-scenes"))
			Expect(svc.PluginType).To(Equal(plugins.TypeBinary))
			Expect(svc.Conn).NotTo(BeNil())

			// Use the plugin's gRPC connection directly (in-process, no network)
			sceneClient := scenev1.NewSceneServiceClient(svc.Conn)

			// Create a scene through the registry connection → plugin pipeline
			createResp, err := sceneClient.CreateScene(ctx, &scenev1.CreateSceneRequest{
				CharacterId: "test-char-001",
				Title:       "Integration Test Scene",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.GetScene()).NotTo(BeNil())
			Expect(createResp.GetScene().GetTitle()).To(Equal("Integration Test Scene"))
			Expect(createResp.GetScene().GetId()).NotTo(BeEmpty())
			Expect(createResp.GetScene().GetState()).To(Equal("active"))
			Expect(createResp.GetScene().GetOwnerId()).To(Equal("test-char-001"))

			sceneID := createResp.GetScene().GetId()

			// Verify via GetScene that it was persisted
			getResp, err := sceneClient.GetScene(ctx, &scenev1.GetSceneRequest{
				CharacterId: "test-char-001",
				SceneId:     sceneID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.GetScene().GetTitle()).To(Equal("Integration Test Scene"))
			Expect(getResp.GetScene().GetId()).To(Equal(sceneID))
		})
	})

	Describe("direct plugin connection without proxy", func() {
		It("calls SceneService directly via plugin gRPC connection", func() {
			provisioner := plugins.NewSchemaProvisioner(connStr)
			Expect(provisioner.Init(ctx)).To(Succeed())
			defer provisioner.Close()

			// Register WorldService so the plugin's requires are satisfied.
			registry := plugins.NewServiceRegistry()
			worldSrv := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-memory bufconn only
			worldConn, worldConnErr := plugins.NewInProcessConn(worldSrv)
			Expect(worldConnErr).NotTo(HaveOccurred())
			defer func() { _ = worldConn.Close() }()

			Expect(registry.Register(plugins.RegisteredService{
				Name:       "holomush.world.v1.WorldService",
				Conn:       worldConn,
				PluginType: plugins.TypeServerInternal(),
			})).To(Succeed())

			host := goplugin.NewHost(
				goplugin.WithSchemaProvisioner(provisioner),
				goplugin.WithServiceRegistry(registry),
			)
			defer func() { _ = host.Close(ctx) }()

			// Load the plugin manifest and binary directly
			pluginDir, _ := coreScenesBinaryPath()
			manifestData, err := os.ReadFile(filepath.Join(pluginDir, "plugin.yaml"))
			Expect(err).NotTo(HaveOccurred())

			manifest, err := plugins.ParseManifest(manifestData)
			Expect(err).NotTo(HaveOccurred())

			Expect(host.Load(ctx, manifest, pluginDir)).To(Succeed())

			// Verify plugin is loaded
			Expect(host.Plugins()).To(ContainElement("core-scenes"))

			// Get the raw gRPC connection to the plugin process
			conn, err := host.PluginConn("core-scenes")
			Expect(err).NotTo(HaveOccurred())
			Expect(conn).NotTo(BeNil())

			// Create a SceneService client directly on the plugin connection
			sceneClient := scenev1.NewSceneServiceClient(conn)

			// CreateScene directly on the plugin
			createResp, err := sceneClient.CreateScene(ctx, &scenev1.CreateSceneRequest{
				CharacterId: "direct-char-001",
				Title:       "Direct Connection Test",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.GetScene().GetTitle()).To(Equal("Direct Connection Test"))

			// GetScene directly
			getResp, err := sceneClient.GetScene(ctx, &scenev1.GetSceneRequest{
				CharacterId: "direct-char-001",
				SceneId:     createResp.GetScene().GetId(),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.GetScene().GetTitle()).To(Equal("Direct Connection Test"))
		})
	})

	Describe("GRPCBroker service injection", func() {
		It("fails to load when a required service is not in the registry", func() {
			provisioner := plugins.NewSchemaProvisioner(connStr)
			Expect(provisioner.Init(ctx)).To(Succeed())
			defer provisioner.Close()

			// Create host with an empty registry — WorldService is NOT registered.
			emptyRegistry := plugins.NewServiceRegistry()
			host := goplugin.NewHost(
				goplugin.WithSchemaProvisioner(provisioner),
				goplugin.WithServiceRegistry(emptyRegistry),
			)
			defer func() { _ = host.Close(ctx) }()

			// Load the plugin manifest directly
			pluginDir, _ := coreScenesBinaryPath()
			manifestData, err := os.ReadFile(filepath.Join(pluginDir, "plugin.yaml"))
			Expect(err).NotTo(HaveOccurred())

			manifest, err := plugins.ParseManifest(manifestData)
			Expect(err).NotTo(HaveOccurred())

			// core-scenes requires WorldService, which is missing from the registry.
			loadErr := host.Load(ctx, manifest, pluginDir)
			Expect(loadErr).To(HaveOccurred())
			Expect(loadErr.Error()).To(ContainSubstring("holomush.world.v1.WorldService"))
		})
	})

	Describe("scene plugin ABAC: read-own-scene", func() {
		var (
			abacCtx         context.Context
			abacCancel      context.CancelFunc
			abacContainer   testcontainers.Container
			abacConnStr     string
			abacHost        *goplugin.Host
			abacPs          *policystore.PostgresStore
			abacEngine      *policy.Engine
			abacPool        *pgxpool.Pool
			abacProvisioner *plugins.SchemaProvisioner
			abacRegistry    *plugins.ServiceRegistry
			sceneID         string
		)

		BeforeEach(func() {
			pluginDir, binaryPath := coreScenesBinaryPath()
			if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
				Skip(fmt.Sprintf("core-scenes binary not found at %s — run 'task plugin:build-all' first", binaryPath))
			}

			abacCtx, abacCancel = context.WithTimeout(context.Background(), 2*time.Minute)

			// Postgres + migrator — use suite-local handles so the outer
			// `container`/`connStr` vars are not clobbered. This keeps the
			// outer Describe block's Postgres instance reachable in its own
			// AfterEach instead of being terminated twice (once here, never
			// at the outer level).
			pgEnv, err := testutil.StartPostgres(abacCtx)
			Expect(err).NotTo(HaveOccurred())
			abacContainer = pgEnv.Container
			abacConnStr = pgEnv.ConnStr

			migrator, err := store.NewMigrator(abacConnStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(migrator.Up()).To(Succeed())
			_ = migrator.Close()

			// Provisioner outlives BeforeEach (closed in AfterEach)
			abacProvisioner = plugins.NewSchemaProvisioner(abacConnStr)
			Expect(abacProvisioner.Init(abacCtx)).To(Succeed())

			// Service registry with WorldService stub
			abacRegistry = plugins.NewServiceRegistry()
			worldSrv := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-memory bufconn only
			worldConn, worldConnErr := plugins.NewInProcessConn(worldSrv)
			Expect(worldConnErr).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = worldConn.Close() })

			Expect(abacRegistry.Register(plugins.RegisteredService{
				Name:       "holomush.world.v1.WorldService",
				Conn:       worldConn,
				PluginType: plugins.TypeServerInternal(),
			})).To(Succeed())

			// Host + load plugin
			abacHost = goplugin.NewHost(
				goplugin.WithSchemaProvisioner(abacProvisioner),
				goplugin.WithServiceRegistry(abacRegistry),
			)

			manifestData, err := os.ReadFile(filepath.Join(pluginDir, "plugin.yaml"))
			Expect(err).NotTo(HaveOccurred())
			manifest, err := plugins.ParseManifest(manifestData)
			Expect(err).NotTo(HaveOccurred())
			Expect(abacHost.Load(abacCtx, manifest, pluginDir)).To(Succeed())

			// Install policies into postgres store
			abacPool, err = pgxpool.New(abacCtx, abacConnStr)
			Expect(err).NotTo(HaveOccurred())
			abacPs = policystore.NewPostgresStore(abacPool)
			installer := plugins.NewPolicyInstaller(abacPs)
			Expect(installer.InstallPluginPoliciesWithManifest(abacCtx, manifest, manifest.Policies)).To(Succeed())

			// Build ABAC engine stack (mirrors abac_widget_test.go)
			// 1. Schema registry + attribute resolver
			schemaRegistry := attribute.NewSchemaRegistry()
			resolver := attribute.NewResolver(schemaRegistry)

			// 2. Register the command attribute provider (for resource.command.name)
			cmdProvider := attribute.NewCommandProvider()
			Expect(resolver.RegisterProvider(cmdProvider)).To(Succeed())

			// 3. Discover scene schema from the plugin and register proxy provider.
			arClient := abacHost.AttributeResolverClient("core-scenes")
			Expect(arClient).NotTo(BeNil())

			schemaResp, schemaErr := arClient.GetSchema(abacCtx, &pluginv1.GetSchemaRequest{})
			Expect(schemaErr).NotTo(HaveOccurred())

			schemas := plugins.ConvertProtoSchema(schemaResp)
			Expect(schemas).To(HaveKey("scene"))

			sceneAttrProvider := plugins.NewPluginAttributeProvider("scene", arClient, schemas["scene"])
			Expect(resolver.RegisterProvider(sceneAttrProvider)).To(Succeed())

			// 4. Create compiler and cache, then reload.
			compiler := policy.NewCompiler(schemaRegistry.Schema())
			cache := policy.NewCache(abacPs, compiler)
			Expect(cache.Reload(abacCtx)).To(Succeed())

			// 5. Create audit logger (minimal, in-memory writer).
			auditWriter := &testAuditWriter{}
			tmpDir := GinkgoT().TempDir()
			auditLogger := audit.NewLogger(audit.ModeAll, auditWriter, filepath.Join(tmpDir, "test-wal.jsonl"))

			// 6. Create mock session resolver (tests use character: subjects directly).
			sessionResolver := &testSessionResolver{}

			// 7. Assemble the engine.
			abacEngine = policy.NewEngine(resolver, cache, sessionResolver, auditLogger)

			// Create a scene owned by char-alice via the plugin's gRPC connection.
			// host.Load registers the plugin process but does not inject it into the
			// service registry; use PluginConn to get the raw gRPC connection directly.
			pluginConn, connErr := abacHost.PluginConn("core-scenes")
			Expect(connErr).NotTo(HaveOccurred())
			sceneClient := scenev1.NewSceneServiceClient(pluginConn)

			createResp, createErr := sceneClient.CreateScene(abacCtx, &scenev1.CreateSceneRequest{
				CharacterId: "char-alice",
				Title:       "Tea at the Manor",
			})
			Expect(createErr).NotTo(HaveOccurred())
			sceneID = createResp.GetScene().GetId()
		})

		AfterEach(func() {
			if abacHost != nil {
				_ = abacHost.Close(abacCtx)
			}
			if abacProvisioner != nil {
				abacProvisioner.Close()
			}
			if abacPool != nil {
				abacPool.Close()
			}
			if abacContainer != nil {
				_ = abacContainer.Terminate(context.Background())
			}
			if abacCancel != nil {
				abacCancel()
			}
		})

		It("permits scene command execution via Layer 1 execute policy", func() {
			req, err := policytypes.NewAccessRequest("character:char-alice", "execute", "command:scene")
			Expect(err).NotTo(HaveOccurred())

			decision, err := abacEngine.Evaluate(abacCtx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(decision.IsAllowed()).To(BeTrue(),
				"execute-scene-commands policy should permit Layer 1 command execution")
			Expect(decision.Effect()).To(Equal(policytypes.EffectAllow))
		})

		It("permits the owner to read their own scene via per-resource policy", func() {
			req, err := policytypes.NewAccessRequest("character:char-alice", "read", "scene:"+sceneID)
			Expect(err).NotTo(HaveOccurred())

			decision, err := abacEngine.Evaluate(abacCtx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(decision.IsAllowed()).To(BeTrue(),
				"read-own-scene policy should permit owner")
		})

		It("denies a non-owner attempting to read the scene", func() {
			req, err := policytypes.NewAccessRequest("character:char-bob", "read", "scene:"+sceneID)
			Expect(err).NotTo(HaveOccurred())

			decision, err := abacEngine.Evaluate(abacCtx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(decision.IsAllowed()).To(BeFalse(),
				"non-owner must be denied (no policy permits)")
		})
	})

	Describe("scene plugin lifecycle: state machine", func() {
		var (
			lifecyclectx       context.Context
			lifecyclecancel    context.CancelFunc
			lifecyclecontainer testcontainers.Container
			lifecyclehost      *goplugin.Host
			lifecyclepool      *pgxpool.Pool
			lifecyclesceneID   string
		)

		BeforeEach(func() {
			pluginDir, binaryPath := coreScenesBinaryPath()
			if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
				Skip(fmt.Sprintf("core-scenes binary not found at %s — run 'bash scripts/build-plugins.sh' first", binaryPath))
			}

			lifecyclectx, lifecyclecancel = context.WithTimeout(context.Background(), 2*time.Minute)

			// Postgres + core migrations (so the policy store schema exists)
			pgEnv, err := testutil.StartPostgres(lifecyclectx)
			Expect(err).NotTo(HaveOccurred())
			lifecyclecontainer = pgEnv.Container
			pgConnStr := pgEnv.ConnStr

			migrator, err := store.NewMigrator(pgConnStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(migrator.Up()).To(Succeed())
			_ = migrator.Close()

			// Provisioner + host
			provisioner := plugins.NewSchemaProvisioner(pgConnStr)
			Expect(provisioner.Init(lifecyclectx)).To(Succeed())
			DeferCleanup(func() { provisioner.Close() })

			registry := plugins.NewServiceRegistry()
			worldSrv := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-memory bufconn only
			worldConn, worldConnErr := plugins.NewInProcessConn(worldSrv)
			Expect(worldConnErr).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = worldConn.Close() })

			Expect(registry.Register(plugins.RegisteredService{
				Name:       "holomush.world.v1.WorldService",
				Conn:       worldConn,
				PluginType: plugins.TypeServerInternal(),
			})).To(Succeed())

			lifecyclehost = goplugin.NewHost(
				goplugin.WithSchemaProvisioner(provisioner),
				goplugin.WithServiceRegistry(registry),
			)

			manifestData, err := os.ReadFile(filepath.Join(pluginDir, "plugin.yaml"))
			Expect(err).NotTo(HaveOccurred())
			manifest, err := plugins.ParseManifest(manifestData)
			Expect(err).NotTo(HaveOccurred())
			Expect(lifecyclehost.Load(lifecyclectx, manifest, pluginDir)).To(Succeed())

			// Direct pool for schema-qualified DB verification
			lifecyclepool, err = pgxpool.New(lifecyclectx, pgConnStr)
			Expect(err).NotTo(HaveOccurred())

			// Create a scene to operate on in each test
			conn, err := lifecyclehost.PluginConn("core-scenes")
			Expect(err).NotTo(HaveOccurred())
			setupClient := scenev1.NewSceneServiceClient(conn)

			createResp, err := setupClient.CreateScene(lifecyclectx, &scenev1.CreateSceneRequest{
				CharacterId: "char-alice",
				Title:       "Lifecycle Test",
			})
			Expect(err).NotTo(HaveOccurred())
			lifecyclesceneID = createResp.GetScene().GetId()
			Expect(lifecyclesceneID).NotTo(BeEmpty())
		})

		AfterEach(func() {
			if lifecyclehost != nil {
				_ = lifecyclehost.Close(lifecyclectx)
			}
			if lifecyclepool != nil {
				lifecyclepool.Close()
			}
			if lifecyclecontainer != nil {
				_ = lifecyclecontainer.Terminate(context.Background())
			}
			if lifecyclecancel != nil {
				lifecyclecancel()
			}
		})

		// sceneClient builds a fresh SceneServiceClient from the host's
		// direct PluginConn helper. We use PluginConn rather than resolving
		// through the service registry because PluginConn is simpler and matches
		// the "direct plugin connection" test pattern.
		sceneClient := func() scenev1.SceneServiceClient {
			conn, err := lifecyclehost.PluginConn("core-scenes")
			Expect(err).NotTo(HaveOccurred())
			return scenev1.NewSceneServiceClient(conn)
		}

		// Helper for direct DB state read
		readSceneState := func(id string) (state string, endedAt sql.NullTime) {
			err := lifecyclepool.QueryRow(lifecyclectx,
				`SELECT state, ended_at FROM plugin_core_scenes.scenes WHERE id = $1`,
				id,
			).Scan(&state, &endedAt)
			Expect(err).NotTo(HaveOccurred())
			return state, endedAt
		}

		Describe("EndScene", func() {
			It("transitions an active scene to ended and sets ended_at", func() {
				_, err := sceneClient().EndScene(lifecyclectx, &scenev1.EndSceneRequest{
					CharacterId: "char-alice",
					SceneId:     lifecyclesceneID,
				})
				Expect(err).NotTo(HaveOccurred())

				state, endedAt := readSceneState(lifecyclesceneID)
				Expect(state).To(Equal("ended"))
				Expect(endedAt.Valid).To(BeTrue(), "ended_at should be set")
			})

			It("returns FailedPrecondition for an already-ended scene", func() {
				_, err := sceneClient().EndScene(lifecyclectx, &scenev1.EndSceneRequest{
					CharacterId: "char-alice",
					SceneId:     lifecyclesceneID,
				})
				Expect(err).NotTo(HaveOccurred())

				_, err = sceneClient().EndScene(lifecyclectx, &scenev1.EndSceneRequest{
					CharacterId: "char-alice",
					SceneId:     lifecyclesceneID,
				})
				Expect(err).To(HaveOccurred())
				Expect(status.Code(err)).To(Equal(codes.FailedPrecondition),
					"second EndScene on already-ended scene must map to FailedPrecondition")
			})

			It("returns NotFound for a missing scene", func() {
				_, err := sceneClient().EndScene(lifecyclectx, &scenev1.EndSceneRequest{
					CharacterId: "char-alice",
					SceneId:     "scene-does-not-exist",
				})
				Expect(err).To(HaveOccurred())
				Expect(status.Code(err)).To(Equal(codes.NotFound),
					"EndScene on a missing scene must map to NotFound")
			})

			It("rejects concurrent end attempts (race-safe WHERE clause)", func() {
				// The store uses UPDATE ... WHERE state IN ('active', 'paused')
				// to prevent races. Two goroutines calling EndScene on the same
				// scene at the same time MUST result in exactly one success
				// (whoever's UPDATE wins) and one FailedPrecondition (whoever's
				// UPDATE finds the row already in 'ended' state).
				client := sceneClient()
				var (
					wg        sync.WaitGroup
					firstErr  error
					secondErr error
				)
				wg.Add(2)
				go func() {
					defer wg.Done()
					_, firstErr = client.EndScene(lifecyclectx, &scenev1.EndSceneRequest{
						CharacterId: "char-alice",
						SceneId:     lifecyclesceneID,
					})
				}()
				go func() {
					defer wg.Done()
					_, secondErr = client.EndScene(lifecyclectx, &scenev1.EndSceneRequest{
						CharacterId: "char-alice",
						SceneId:     lifecyclesceneID,
					})
				}()
				wg.Wait()

				successes := 0
				if firstErr == nil {
					successes++
				}
				if secondErr == nil {
					successes++
				}
				Expect(successes).To(Equal(1),
					"exactly one concurrent end should succeed; got first=%v second=%v",
					firstErr, secondErr)

				state, endedAt := readSceneState(lifecyclesceneID)
				Expect(state).To(Equal("ended"))
				Expect(endedAt.Valid).To(BeTrue())
			})
		})

		Describe("PauseScene", func() {
			It("transitions an active scene to paused", func() {
				_, err := sceneClient().PauseScene(lifecyclectx, &scenev1.PauseSceneRequest{
					CharacterId: "char-alice",
					SceneId:     lifecyclesceneID,
				})
				Expect(err).NotTo(HaveOccurred())

				state, _ := readSceneState(lifecyclesceneID)
				Expect(state).To(Equal("paused"))
			})

			It("rejects pause on an already-paused scene", func() {
				_, err := sceneClient().PauseScene(lifecyclectx, &scenev1.PauseSceneRequest{
					CharacterId: "char-alice",
					SceneId:     lifecyclesceneID,
				})
				Expect(err).NotTo(HaveOccurred())

				_, err = sceneClient().PauseScene(lifecyclectx, &scenev1.PauseSceneRequest{
					CharacterId: "char-alice",
					SceneId:     lifecyclesceneID,
				})
				Expect(err).To(HaveOccurred())
				Expect(status.Code(err)).To(Equal(codes.FailedPrecondition),
					"second PauseScene on already-paused scene must map to FailedPrecondition")
			})
		})

		Describe("ResumeScene", func() {
			It("transitions a paused scene back to active", func() {
				_, err := sceneClient().PauseScene(lifecyclectx, &scenev1.PauseSceneRequest{
					CharacterId: "char-alice",
					SceneId:     lifecyclesceneID,
				})
				Expect(err).NotTo(HaveOccurred())

				_, err = sceneClient().ResumeScene(lifecyclectx, &scenev1.ResumeSceneRequest{
					CharacterId: "char-alice",
					SceneId:     lifecyclesceneID,
				})
				Expect(err).NotTo(HaveOccurred())

				state, _ := readSceneState(lifecyclesceneID)
				Expect(state).To(Equal("active"))
			})

			It("rejects resume on an active scene", func() {
				_, err := sceneClient().ResumeScene(lifecyclectx, &scenev1.ResumeSceneRequest{
					CharacterId: "char-alice",
					SceneId:     lifecyclesceneID,
				})
				Expect(err).To(HaveOccurred())
				Expect(status.Code(err)).To(Equal(codes.FailedPrecondition),
					"ResumeScene on an active scene must map to FailedPrecondition")
			})
		})

		Describe("UpdateScene", func() {
			It("applies a title change", func() {
				_, err := sceneClient().UpdateScene(lifecyclectx, &scenev1.UpdateSceneRequest{
					CharacterId: "char-alice",
					SceneId:     lifecyclesceneID,
					Title:       "Renamed Title",
					UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
				})
				Expect(err).NotTo(HaveOccurred())

				var title string
				err = lifecyclepool.QueryRow(lifecyclectx,
					`SELECT title FROM plugin_core_scenes.scenes WHERE id = $1`,
					lifecyclesceneID,
				).Scan(&title)
				Expect(err).NotTo(HaveOccurred())
				Expect(title).To(Equal("Renamed Title"))
			})

			It("rejects updates to an ended scene", func() {
				_, err := sceneClient().EndScene(lifecyclectx, &scenev1.EndSceneRequest{
					CharacterId: "char-alice",
					SceneId:     lifecyclesceneID,
				})
				Expect(err).NotTo(HaveOccurred())

				_, err = sceneClient().UpdateScene(lifecyclectx, &scenev1.UpdateSceneRequest{
					CharacterId: "char-alice",
					SceneId:     lifecyclesceneID,
					Title:       "Try",
					UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
				})
				Expect(err).To(HaveOccurred())
				Expect(status.Code(err)).To(Equal(codes.FailedPrecondition),
					"UpdateScene on an ended scene must map to FailedPrecondition")

				var title string
				err = lifecyclepool.QueryRow(lifecyclectx,
					`SELECT title FROM plugin_core_scenes.scenes WHERE id = $1`,
					lifecyclesceneID,
				).Scan(&title)
				Expect(err).NotTo(HaveOccurred())
				Expect(title).To(Equal("Lifecycle Test"))
			})
		})
	})
})
