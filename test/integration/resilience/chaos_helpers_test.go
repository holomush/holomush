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
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/testsupport/natstest"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/outbox"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	worldsetup "github.com/holomush/holomush/internal/world/setup"
	"github.com/holomush/holomush/internal/world/wmodel"
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

// startExternalNATS boots a fresh single-node NATS JetStream container loading
// the shipped CLUSTER-02 scoped account (deploy/nats/cluster-server.conf) and
// registers its teardown on the spec. Scoped (not a bare unscoped node) so
// that a full eventbus.Subsystem.Start — dial + EnsureStream +
// VerifyAccountScoping (07-09) — succeeds against it; a bare node is
// deliberately over-scoped by design and would refuse to boot with
// EVENTBUS_ACCOUNT_OVERSCOPED (see test/integration/eventbus_external's
// scopecheck_test.go Case A).
func startExternalNATS(ctx context.Context) *natstest.NATSEnv {
	env, err := natstest.StartScopedNATS(ctx)
	Expect(err).NotTo(HaveOccurred(), "StartScopedNATS should return a running container")
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
	opts := []integrationtest.StartOption{integrationtest.WithExternalNATS(natstest.ScopedURL(url))}
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

// --- outbox relay + reference consumer + wire-inspection seams (plan 05-08) ---
//
// The integrationtest harness does NOT run the OutboxRelaySubsystem (it is
// production-wired at the composition root in cmd/holomush/core.go), so the
// fault-injection specs construct the relay + reference consumer directly over
// the shared stack, exactly as production wires them. That keeps the relay under
// test the REAL relay (05-07) with a real generation-fenced advisory-lock lease
// and a real external-NATS publisher, while the specs drive Drain/Apply
// explicitly so each fault window is deterministic (no background wakeup racing
// an assertion).

// outboxStoreFor builds the leased outbox store adapter over a replica's shared
// pool exactly as production wires it (setup.NewOutboxStore over the postgres
// OutboxStore), so the relay's DB ops run through the same generation-fenced
// advisory-lock lease the composition root uses.
func outboxStoreFor(s *integrationtest.Server) outbox.OutboxStore {
	return worldsetup.NewOutboxStore(worldpostgres.NewOutboxStore(s.Pool()))
}

// busPublisher returns the replica's production eventbus publisher (external-mode
// under WithExternalNATS). The relay publishes through it; it stamps
// Nats-Msg-Id = Event.ID for JetStream dedup.
func busPublisher(s *integrationtest.Server) eventbus.Publisher {
	return s.Bus().Bus.Publisher()
}

// newOutboxRelay constructs a single leased relay draining game's feed over the
// shared stack. It is sweep-only (nil Waker): the specs drive Drain explicitly.
func newOutboxRelay(store outbox.OutboxStore, pub eventbus.Publisher, game string) *outbox.Relay {
	return outbox.NewRelay(outbox.RelayConfig{Store: store, Publisher: pub, GameID: game})
}

// seedOutboxRow writes ONE committed outbox row for game via the production
// same-tx writer (no MoveCharacter needed) and returns the finalized envelope
// (carrying the writer-allocated epoch + feed_position). kind must be a valid
// event type (no spaces) so the relay can build a wire event; the aggregate id is
// fresh per call, so each seeded row lands on a UNIQUE subject the wire
// assertions can isolate on.
func seedOutboxRow(ctx context.Context, s *integrationtest.Server, game, kind string) *wmodel.Envelope {
	GinkgoHelper()
	store := worldpostgres.NewOutboxStore(s.Pool())
	intent := wmodel.NewEnvelopeIntent(wmodel.IntentParams{
		GameID:        game,
		Kind:          kind,
		SchemaVersion: 1,
		Actor:         "system",
		AggregateType: wmodel.AggregateLocation,
		AggregateID:   ulid.Make(),
		Payload:       []byte(`{"name":"chaos"}`),
	})
	delta := &wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{
		Type: wmodel.AggregateLocation, ID: intent.AggregateID, BeforeVersion: 0, AfterVersion: 1,
	}}
	env, err := store.WriteIntent(ctx, intent, delta)
	Expect(err).NotTo(HaveOccurred(), "seedOutboxRow: WriteIntent")
	return env
}

// envelopeSubject reproduces the relay's wire subject for env
// (events.<game>.<aggregate-type>.<aggregate-id>) so a per-subject stream count
// can isolate one seeded row's deliveries from all other broker traffic.
func envelopeSubject(env *wmodel.Envelope) string {
	GinkgoHelper()
	subj, err := eventbus.Qualify(env.GameID, string(env.AggregateType)+"."+env.AggregateID.String())
	Expect(err).NotTo(HaveOccurred(), "envelopeSubject: Qualify")
	return string(subj)
}

// checkpointStoreAdapter bridges the concrete postgres checkpoint store to the
// consumer-owned outbox.ConsumerCheckpointStore. The setup package's equivalent
// adapter is unexported, so this test-local mirror (identical to
// setup.checkpointStoreAdapter) keeps the suite self-contained. The two
// TxExecutor interfaces are structurally identical, so the effect bridge is a
// direct pass-through.
type checkpointStoreAdapter struct {
	inner *worldpostgres.ConsumerCheckpointStore
}

func (a checkpointStoreAdapter) ApplyOnce(
	ctx context.Context, consumer string, env wmodel.Envelope,
	effect func(effCtx context.Context, exec outbox.TxExecutor) error,
) (bool, error) {
	return a.inner.ApplyOnce(ctx, consumer, env, func(effCtx context.Context, exec worldpostgres.TxExecutor) error {
		return effect(effCtx, exec)
	})
}

func (a checkpointStoreAdapter) InitWatermark(ctx context.Context, consumer, gameID string, epoch, position int64) error {
	return a.inner.InitWatermark(ctx, consumer, gameID, epoch, position)
}

func (a checkpointStoreAdapter) Watermark(ctx context.Context, consumer, gameID string) (int64, int64, bool, error) {
	return a.inner.Watermark(ctx, consumer, gameID)
}

// referenceConsumer bundles the reference idempotent consumer with the concrete
// checkpoint store so a spec can both InitWatermark the (consumer, game) baseline
// and Apply deliveries through the same durable receipt+watermark store.
type referenceConsumer struct {
	*outbox.Consumer
	checkpoint *worldpostgres.ConsumerCheckpointStore
	name       string
}

// newReferenceConsumer builds a reference consumer over the shared pool with a
// UNIQUE durable name (so specs never contend on receipts/watermarks). effect MAY
// be nil (pure receipt+watermark recording).
func newReferenceConsumer(s *integrationtest.Server, effect outbox.EffectFunc) *referenceConsumer {
	name := "resilience-" + ulid.Make().String()
	checkpoint := worldpostgres.NewConsumerCheckpointStore(s.Pool())
	return &referenceConsumer{
		Consumer:   outbox.NewConsumer(name, checkpointStoreAdapter{inner: checkpoint}, effect, nil),
		checkpoint: checkpoint,
		name:       name,
	}
}

// initWatermark seeds the consumer's (consumer, game) watermark so envelope at
// (epoch, position) is the next contiguous delivery — letting a spec drive a
// single row through Apply without replaying the whole feed prefix.
func (c *referenceConsumer) initWatermark(ctx context.Context, game string, epoch, position int64) {
	GinkgoHelper()
	Expect(c.checkpoint.InitWatermark(ctx, c.name, game, epoch, position)).
		To(Succeed(), "reference consumer InitWatermark")
}

// streamSubjectCount reports how many messages the shared EVENTS stream has
// stored on subject — read over an INDEPENDENT connection (never a replica's
// cached view), so the wire assertion binds on broker state and is isolated to
// the one subject (immune to other traffic). Nats-Msg-Id dedup makes a duplicate
// publish a no-op here: two publishes of the same event ULID leave the count at 1.
func streamSubjectCount(ctx context.Context, env *natstest.NATSEnv, subject string) uint64 {
	GinkgoHelper()
	conn := env.Conn(suiteT)
	js, err := jetstream.New(conn)
	Expect(err).NotTo(HaveOccurred(), "streamSubjectCount: jetstream.New")
	stream, err := js.Stream(ctx, eventbus.StreamName)
	Expect(err).NotTo(HaveOccurred(), "streamSubjectCount: EVENTS stream must exist")
	info, err := stream.Info(ctx, jetstream.WithSubjectFilter(subject))
	Expect(err).NotTo(HaveOccurred(), "streamSubjectCount: stream.Info")
	return info.State.Subjects[subject]
}

// outboxPublishedAt reads whether the outbox row for eventID has been marked
// published (published_at IS NOT NULL) — the DB-side proof the relay PubAcked it
// (MarkPublished runs only AFTER a successful publish). Read over the shared pool,
// so it observes only committed state.
func outboxPublishedAt(ctx context.Context, s *integrationtest.Server, eventID ulid.ULID) bool {
	GinkgoHelper()
	var published bool
	Expect(s.Pool().QueryRow(ctx,
		`SELECT published_at IS NOT NULL FROM outbox WHERE event_id = $1`, eventID.String()).
		Scan(&published)).To(Succeed(), "outboxPublishedAt: read row")
	return published
}
