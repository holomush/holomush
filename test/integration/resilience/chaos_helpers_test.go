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
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"
	"github.com/testcontainers/testcontainers-go"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/testsupport/natstest"
	"github.com/holomush/holomush/internal/world"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
)

// newWorldService constructs a world.Service over one replica's shared pgxpool,
// mirroring the production construction in
// internal/testsupport/integrationtest/plugins.go:267 (which itself mirrors
// internal/world/setup/subsystem.go). EventEmitter is intentionally omitted —
// M2's emitter wiring is plan 03's concern, and production world/setup omits it
// too (world.NewService logs a benign slog.Warn). The allow-all default engine
// (no WithRealABAC on the resilience replicas) accepts any subjectID string, so
// the deterministic-interleave spec can drive UpdateLocation directly without a
// seeded policy.
//
// Because both replicas share ONE database, newWorldService(replicaA) and
// newWorldService(replicaB) are two independent write paths onto the identical
// unguarded full-row UPDATE (location_repo.go:73) — exactly the two-replica
// concurrency surface the M12 verdict characterizes.
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
	})
}

// worldBusAppender implements world.EventAppender by translating a core.Event
// into an eventbus.Event and publishing it through the RAW subsystem publisher
// (s.Bus().Bus.Publisher()) — NOT the host's rendering publisher.
//
// This raw-publisher choice is deliberate and load-bearing for the M2 experiment:
// the rendering publisher runs a verb-registry Lookup on every publish and
// hard-fails EMIT_UNKNOWN_VERB for host-owned world event types (e.g. "move"),
// which have no plugin-qualified verb registration. The M2 window is a
// stream-PRESENCE question (did the post-commit move notification reach the
// broker or not?), so rendering metadata is irrelevant and the verb-registry
// lookup would only get in the way. The translation mirrors the harness's
// busEventAppenderAdapter (Qualify → NewType → actor-kind mapping) verbatim.
type worldBusAppender struct {
	publisher eventbus.Publisher
	gameID    func() string
}

var _ world.EventAppender = (*worldBusAppender)(nil)

// Append translates event to an eventbus.Event and publishes it to the shared
// broker via the raw publisher. Domain-relative stream references (e.g.
// "location.01ABC") are qualified to full subjects via eventbus.Qualify.
func (w *worldBusAppender) Append(ctx context.Context, event core.Event) error {
	gid := w.gameID()
	if gid == "" {
		gid = "main"
	}
	sub, err := eventbus.Qualify(gid, event.Stream)
	if err != nil {
		return oops.With("stream", event.Stream).Wrap(err)
	}
	typ, err := eventbus.NewType(string(event.Type))
	if err != nil {
		return oops.With("type", string(event.Type)).Wrap(err)
	}
	return oops.Wrap(w.publisher.Publish(ctx, eventbus.Event{
		ID:        event.ID,
		Subject:   sub,
		Type:      typ,
		Timestamp: event.Timestamp,
		Actor:     resilienceCoreToBusActor(event.Actor),
		Payload:   event.Payload,
	}))
}

// resilienceCoreToBusActor mirrors the harness's harnessCoreToBusActor: it maps
// a core.Actor to an eventbus.Actor, parsing the ULID id when present. The
// EventStoreAdapter stamps a system actor (core.WorldServiceActorULID), so this
// resolves to ActorKindSystem with the parsed world-service ULID.
func resilienceCoreToBusActor(a core.Actor) eventbus.Actor {
	out := eventbus.Actor{Kind: resilienceCoreActorKindToBus(a.Kind)}
	if a.ID == "" {
		return out
	}
	if parsed, parseErr := ulid.Parse(a.ID); parseErr == nil {
		out.ID = parsed
	}
	return out
}

func resilienceCoreActorKindToBus(k core.ActorKind) eventbus.ActorKind {
	switch k {
	case core.ActorCharacter:
		return eventbus.ActorKindCharacter
	case core.ActorPlugin:
		return eventbus.ActorKindPlugin
	default:
		return eventbus.ActorKindSystem
	}
}

// newEmittingWorldService constructs a world.Service identical to newWorldService
// but WITH an EventEmitter wired: a world.EventStoreAdapter over a worldBusAppender
// that publishes to the replica's shared broker via the raw publisher. This is the
// deliberately-wired emitter the M2 dual-write experiment needs to make the
// post-commit notification leg observable — production world/setup omits it
// entirely (internal/world/setup/subsystem.go:66-77), which is itself the M2
// production finding spec 3 pins.
func newEmittingWorldService(s *integrationtest.Server) *world.Service {
	pool := s.Pool()
	appender := &worldBusAppender{
		publisher: s.Bus().Bus.Publisher(),
		gameID:    s.GameID,
	}
	return world.NewService(world.ServiceConfig{
		LocationRepo:  worldpostgres.NewLocationRepository(pool),
		ExitRepo:      worldpostgres.NewExitRepository(pool),
		ObjectRepo:    worldpostgres.NewObjectRepository(pool),
		SceneRepo:     worldpostgres.NewSceneRepository(pool),
		CharacterRepo: worldpostgres.NewCharacterRepository(pool),
		PropertyRepo:  worldpostgres.NewPropertyRepository(pool),
		Engine:        s.AccessEngine(),
		Transactor:    worldpostgres.NewTransactor(pool),
		EventEmitter:  world.NewEventStoreAdapter(appender),
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
