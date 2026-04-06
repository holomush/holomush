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
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/store"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
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
		container *postgres.PostgresContainer
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
		var err error
		container, err = postgres.Run(ctx,
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
		Expect(err).NotTo(HaveOccurred())

		connStr, err = container.ConnectionString(ctx, "sslmode=disable")
		Expect(err).NotTo(HaveOccurred())

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
			pluginsDir := pluginBinaryDir()

			// Set up schema provisioner
			provisioner := plugins.NewSchemaProvisioner(connStr)
			Expect(provisioner.Init(ctx)).To(Succeed())
			defer provisioner.Close()

			// Create a service registry
			registry := plugins.NewServiceRegistry()

			// Create goplugin host with schema provisioner
			host := goplugin.NewHost(goplugin.WithSchemaProvisioner(provisioner))
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
				SceneId: sceneID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.GetScene().GetTitle()).To(Equal("Integration Test Scene"))
			Expect(getResp.GetScene().GetId()).To(Equal(sceneID))

			// Verify the owner is a participant
			Expect(getResp.GetScene().GetParticipants()).To(HaveLen(1))
			Expect(getResp.GetScene().GetParticipants()[0].GetCharacterId()).To(Equal("test-char-001"))
			Expect(getResp.GetScene().GetParticipants()[0].GetRole()).To(Equal("owner"))

			// Verify ListScenes returns the scene (open visibility by default)
			listResp, err := sceneClient.ListScenes(ctx, &scenev1.ListScenesRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(listResp.GetScenes()).NotTo(BeEmpty())

			var foundInList bool
			for _, s := range listResp.GetScenes() {
				if s.GetId() == sceneID {
					foundInList = true
					break
				}
			}
			Expect(foundInList).To(BeTrue(), "created scene should appear in ListScenes")
		})
	})

	Describe("direct plugin connection without proxy", func() {
		It("calls SceneService directly via plugin gRPC connection", func() {
			provisioner := plugins.NewSchemaProvisioner(connStr)
			Expect(provisioner.Init(ctx)).To(Succeed())
			defer provisioner.Close()

			host := goplugin.NewHost(goplugin.WithSchemaProvisioner(provisioner))
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
				SceneId: createResp.GetScene().GetId(),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.GetScene().GetTitle()).To(Equal("Direct Connection Test"))
		})
	})
})
