// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package resilience_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	dockerclient "github.com/moby/moby/client"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/testsupport/natstest"
	"github.com/holomush/holomush/internal/world"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
)

// newWorldService constructs a world.Service over one replica's shared pgxpool,
// mirroring the production construction in
// internal/testsupport/integrationtest/plugins.go:267 (which itself mirrors
// internal/world/setup/subsystem.go). It wires the transactional-outbox writer
// (05-06): MoveCharacter commits its state change and its ONE move envelope in the
// SAME transaction via world.OutboxWriter — the post-commit emit path is deleted
// (D-03), so there is no separate notification leg to lose on a broker flap. The
// allow-all default engine (no WithRealABAC on the resilience replicas) accepts any
// subjectID string, so the specs can drive MoveCharacter/UpdateLocation directly
// without a seeded policy.
//
// Because both replicas share ONE database, newWorldService(replicaA) and
// newWorldService(replicaB) are two independent write paths onto the identical
// version-predicated guarded CAS Update (location_repo.go) — exactly the
// two-replica concurrency surface the M12 regression gate exercises now that the
// guard (plans 05-01..05-04) closes last-write-wins.
func newWorldService(s *integrationtest.Server) *world.Service {
	pool := s.Pool()
	return world.NewService(world.ServiceConfig{
		LocationRepo:  worldpostgres.NewLocationRepository(pool),
		ExitRepo:      worldpostgres.NewExitRepository(pool),
		ObjectRepo:    worldpostgres.NewObjectRepository(pool),
		SceneRepo:     worldpostgres.NewSceneRepository(pool),
		CharacterRepo: worldpostgres.NewCharacterRepository(pool),
		PropertyRepo:  worldpostgres.NewPropertyRepository(pool),
		Engine:        s.AccessEngine(),
		Transactor:    worldpostgres.NewTransactor(pool),
		OutboxWriter:  worldpostgres.NewOutboxStore(pool),
		GameID:        s.GameID(),
	})
}

// startExternalNATS boots a fresh single-node NATS JetStream container and
// registers its teardown on the spec. Copied from the eventbus_external suite —
// the transport-only helper that hands each replica its own connection.
func startExternalNATS(ctx context.Context) *natstest.NATSEnv {
	env, err := natstest.StartNATS(ctx)
	Expect(err).NotTo(HaveOccurred(), "StartNATS should return a running container")
	DeferCleanup(func() {
		_ = env.Terminate(context.Background())
	})
	return env
}

// startReplica boots one in-process CoreServer replica against the shared broker
// at url. Replica 1 passes connStr="" so it CREATES the fresh per-test database;
// replica 2+ passes replica 1's ConnStr() so it JOINS the same database. extra
// options compose after the external-NATS + shared-DB seams (e.g. a suite could
// add WithInTreePlugins for a heavier scenario).
func startReplica(t *testing.T, url, connStr string, extra ...integrationtest.StartOption) *integrationtest.Server {
	t.Helper()
	opts := []integrationtest.StartOption{integrationtest.WithExternalNATS(url)}
	if connStr != "" {
		opts = append(opts, integrationtest.WithSharedDatabase(connStr))
	}
	opts = append(opts, extra...)
	return integrationtest.Start(t, opts...)
}

// reportVerdict emits a stable-prefix evidence line (M12-VERDICT: / CHAOS-VERDICT:)
// on three channels so plan 03's evidence doc can quote it verbatim regardless of
// how the suite was run:
//
//   - GinkgoWriter — captured-on-failure and shown under `ginkgo -v`;
//   - AddReportEntry — structured, surfaced in Ginkgo's JSON/JUnit report;
//   - os.Stdout — survives `gotestsum --format pkgname` when the run is inspected
//     with a passing-output-visible reader (the line is a genuine test2json
//     Output event), so a `| grep M12-VERDICT` over a captured run finds it.
//
// forbidigo (the fmt/print ban) is disabled outside internal/eventbus paths, so
// the direct stdout write is lint-clean here.
func reportVerdict(line string) {
	GinkgoHelper()
	_, _ = fmt.Fprintln(GinkgoWriter, line)
	_, _ = fmt.Fprintln(os.Stdout, line)
	AddReportEntry(line)
}

// The suite's verdict lines (M12-VERDICT: / CHAOS-VERDICT:) are added as Ginkgo
// report entries by reportVerdict. gotestsum's `--format pkgname` (the
// `task test:int` default) suppresses a PASSING test's stdout, so those lines do
// not appear on a green run's console. This ReportAfterSuite is the reliable
// capture channel: when RESILIENCE_VERDICT_LOG names a path, every verdict report
// entry is written there verbatim, so the run's evidence is quotable regardless
// of the console format. With the env var unset it is a no-op.
var _ = ReportAfterSuite("resilience verdicts", func(report Report) {
	path := os.Getenv("RESILIENCE_VERDICT_LOG")
	if path == "" {
		return
	}
	f, err := os.Create(path)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "resilience verdict log: create %s: %v\n", path, err)
		return
	}
	defer func() { _ = f.Close() }()
	for _, spec := range report.SpecReports {
		for _, entry := range spec.ReportEntries {
			if strings.Contains(entry.Name, "-VERDICT:") {
				_, _ = fmt.Fprintln(f, entry.Name)
			}
		}
	}
})

// pauseBroker freezes the NATS container with `docker pause` — SIGSTOP on every
// process in the container. Networking stays intact and the mapped port is
// stable, so replicas' connections are not dropped: publishes and acks simply
// stall until unpauseBroker resumes the process. This is the primary broker-flap
// primitive for the dual-write window experiment (plan 03); Stop/Start restart
// fidelity is a later experiment. Verified against pinned testcontainers-go
// v0.43.0 (DockerClient embeds *docker/client.Client, exposing ContainerPause).
func pauseBroker(ctx context.Context, env *natstest.NATSEnv) {
	GinkgoHelper()
	cli, err := testcontainers.NewDockerClientWithOpts(ctx)
	Expect(err).NotTo(HaveOccurred(), "pauseBroker: docker client")
	// Close the client each call — NewDockerClientWithOpts leaks the underlying
	// net/http persistConn goroutines otherwise, across repeated flap windows.
	defer func() { _ = cli.Close() }()
	// ContainerPause returns (ContainerPauseResult, error) in the pinned
	// moby/moby client — the result value is discarded; only the error matters.
	_, err = cli.ContainerPause(ctx, env.Container.GetContainerID(), dockerclient.ContainerPauseOptions{})
	Expect(err).NotTo(HaveOccurred(), "pauseBroker: ContainerPause")
}

// unpauseBroker resumes a container frozen by pauseBroker (`docker unpause` —
// SIGCONT). Paired with pauseBroker around the window under test.
func unpauseBroker(ctx context.Context, env *natstest.NATSEnv) {
	GinkgoHelper()
	cli, err := testcontainers.NewDockerClientWithOpts(ctx)
	Expect(err).NotTo(HaveOccurred(), "unpauseBroker: docker client")
	// Close the client each call — NewDockerClientWithOpts leaks the underlying
	// net/http persistConn goroutines otherwise, across repeated flap windows.
	defer func() { _ = cli.Close() }()
	// ContainerUnpause returns (ContainerUnpauseResult, error) in the pinned
	// moby/moby client — the result value is discarded; only the error matters.
	_, err = cli.ContainerUnpause(ctx, env.Container.GetContainerID(), dockerclient.ContainerUnpauseOptions{})
	Expect(err).NotTo(HaveOccurred(), "unpauseBroker: ContainerUnpause")
}
