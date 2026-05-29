// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/plugin/plugintest"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// buildConnIDEchoPlugin compiles the connid_echo_plugin testdata binary into a
// fresh per-test directory laid out the way goplugin.Host.Load expects
// (<dir>/plugin.yaml + <dir>/<os>-<arch>/connid-echo-plugin). Mirrors
// buildForgeryPlugin in actor_authentication_test.go.
func buildConnIDEchoPlugin() (string, *plugins.Manifest) {
	GinkgoT().Helper()
	pluginDir := GinkgoT().TempDir()
	platformDir := runtime.GOOS + "-" + runtime.GOARCH
	platformSubDir := filepath.Join(pluginDir, platformDir)
	Expect(os.MkdirAll(platformSubDir, 0o755)).To(Succeed())

	_, thisFile, _, _ := runtime.Caller(0)
	srcDir := filepath.Join(filepath.Dir(thisFile), "testdata", "connid_echo_plugin")
	manifestData, err := os.ReadFile(filepath.Join(srcDir, "plugin.yaml"))
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), manifestData, 0o644)).To(Succeed())

	manifest, err := plugins.ParseManifest(manifestData)
	Expect(err).NotTo(HaveOccurred())

	binaryPath := filepath.Join(platformSubDir, "connid-echo-plugin")
	cmd := exec.Command("go", "build", "-o", binaryPath, ".") // #nosec G204 -- in-tree test source dir
	cmd.Dir = srcDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, buildErr := cmd.CombinedOutput()
	Expect(buildErr).NotTo(HaveOccurred(), "go build connid_echo_plugin failed: %s", string(out))

	return pluginDir, manifest
}

// This suite is the Go-tier regression guard for holomush-dble7. The bug was a
// dropped ConnectionID in the binary-plugin SDK receive adapter
// (pkg/plugin/sdk.go); it evaded TestDispatcher_PassesConnectionIDToPluginCommand
// because that test uses a fake deliverer at the struct boundary, never the
// real proto round-trip. Here a real binary plugin is loaded over go-plugin
// gRPC and a command is delivered through host.DeliverCommand
// (host send -> proto -> SDK adapter receive -> handler); the plugin echoes
// the ConnectionID it observed so we can assert it survived.
var _ = Describe("Binary plugin command connection_id round-trip (holomush-dble7)", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		host       *goplugin.Host
		pluginName string
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)
		pluginDir, manifest := buildConnIDEchoPlugin()
		pluginName = manifest.Name
		// DeliverCommand stamps an actor + issues an emit token, which needs
		// the plugin name resolvable to a ULID via the identity registry.
		host = goplugin.NewHost(
			goplugin.WithIdentityRegistry(plugintest.NewStubRegistry(manifest.Name)),
		)
		Expect(host.Load(ctx, manifest, pluginDir)).To(Succeed())
	})

	AfterEach(func() {
		if host != nil {
			_ = host.Close(ctx)
		}
		if cancel != nil {
			cancel()
		}
	})

	DescribeTable(
		"delivers connection_id intact across the gRPC proto boundary",
		func(connID string) {
			resp, err := host.DeliverCommand(ctx, pluginName, pluginsdk.CommandRequest{
				Command:      "echo",
				CharacterID:  "char-1",
				ConnectionID: connID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			// The plugin echoes "connid=<ConnectionID it received>". If the SDK
			// adapter drops connection_id (the dble7 regression), a populated
			// connID arrives empty at the handler and this assertion fails.
			Expect(resp.Output).To(Equal("connid=" + connID))
		},
		Entry("a populated connection_id reaches the handler", "01KSTEHGQEXR7J0B0VK5GBKG1F"),
		Entry("an empty connection_id passes through unchanged (legacy/scripted caller)", ""),
	)
})
