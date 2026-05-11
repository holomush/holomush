// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// RekeyTestHarness: reusable Ginkgo E2E harness for rekey lifecycle specs.
//
// SetupRekeyHarness boots two in-process replicas sharing one Postgres pool
// and an embedded NATS server, seeds a 1000-event fixture under the active
// DEK for context scene:01ABC, and exposes assertion + fault-injection helpers
// consumed by sub-epic E and sub-epic F E2E specs.
package crypto_test

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega" //nolint:revive // gomega convention
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/testsupport/holomushtest"
)

// Harness is the per-spec rekey test fixture. It holds two server replicas
// sharing one Postgres pool, a pre-seeded 1000-event fixture, and assertion /
// fault-injection helpers.
type Harness struct {
	Primary        *holomushtest.Server
	Secondary      *holomushtest.Server
	AdminCli       *holomushtest.AdminClient
	DB             *pgxpool.Pool
	Game           string
	AdminPlayer    holomushtest.PlayerCreds
	PartnerPlayer  holomushtest.PlayerCreds
	SceneContext   dek.ContextID
}

// HarnessOption is a functional option for SetupRekeyHarness.
type HarnessOption func(*HarnessConfig)

// HarnessConfig controls fixture generation and fault injection.
type HarnessConfig struct {
	// EventCount is the number of events to seed under SceneContext.
	// Default: 1000.
	EventCount int
	// EventSubject is the NATS subject for seeded events.
	// Default: "events.g1.scene.01ABC.ic".
	EventSubject string
	// EncryptUnderDEK mints a v1 DEK for SceneContext and encrypts each
	// seeded event. Default: true.
	EncryptUnderDEK bool
	// FaultAtRow, when > 0, installs a Phase 3 batch hook that panics
	// (simulating a crash) after processing FaultAtRow rows. Used by
	// crash-resume E2E specs.
	FaultAtRow int
}

// WithEventCount overrides the number of events seeded by the fixture.
func WithEventCount(n int) HarnessOption {
	return func(c *HarnessConfig) { c.EventCount = n }
}

// WithFaultAtRow installs a Phase 3 crash after processing n rows. Used by
// crash-resume E2E specs.
func WithFaultAtRow(n int) HarnessOption {
	return func(c *HarnessConfig) { c.FaultAtRow = n }
}

func defaultHarnessConfig() HarnessConfig {
	return HarnessConfig{
		EventCount:      1000,
		EventSubject:    "events.g1.scene.01ABC.ic",
		EncryptUnderDEK: true,
	}
}

// SetupRekeyHarness boots two in-process replicas, seeds a default 1000-event
// fixture under the active DEK for context scene:01ABC, and returns the Harness.
//
// Cleanup is registered on t via holomushtest.StartServer, which calls
// Server.Shutdown on t.Cleanup. Callers that want explicit control can also
// call Harness.Cleanup.
func SetupRekeyHarness(t *testing.T, opts ...HarnessOption) *Harness {
	t.Helper()

	cfg := defaultHarnessConfig()
	for _, o := range opts {
		o(&cfg)
	}

	h := &Harness{
		Game:         "g1",
		SceneContext: dek.ContextID{Type: "scene", ID: "01ABC"},
	}

	// Boot shared Postgres + embedded NATS.
	natsURL := holomushtest.StartEmbeddedNATS(t)
	pgPool := holomushtest.StartPG(t)
	h.DB = pgPool

	// Boot two replicas sharing the same Postgres pool and NATS URL.
	h.Primary = holomushtest.StartServer(t, holomushtest.ServerConfig{
		MemberID: "member-1",
		NATSURL:  natsURL,
		PG:       pgPool,
		Game:     h.Game,
	})
	h.Secondary = holomushtest.StartServer(t, holomushtest.ServerConfig{
		MemberID: "member-2",
		NATSURL:  natsURL,
		PG:       pgPool,
		Game:     h.Game,
	})

	// Wire admin client to primary's UDS.
	h.AdminCli = holomushtest.NewAdminClient(pgPool, h.Primary.UDSPath(), t)

	// Seed operator players — playerID echoed as session token by noopRekeySessionStore.
	h.AdminPlayer = h.AdminCli.SeedAdminPlayer("01PRIMPLAYERID00000000", "wizard", "admin-pass")
	h.PartnerPlayer = h.AdminCli.SeedAdminPlayer("01PARTPLAYERID00000000", "second-op", "partner-pass")

	// Mint initial DEK + seed fixture events.
	h.seedDEKAndEvents(t, cfg)

	// If fault injection requested, install the per-batch hook.
	if cfg.FaultAtRow > 0 {
		targetRow := cfg.FaultAtRow
		h.Primary.GetRekeyOrchestrator().SetBatchHookForTest(func(rowsSoFar int) {
			if rowsSoFar >= targetRow {
				panic("simulated mid-Phase-3 crash")
			}
		})
	}

	return h
}

// Cleanup shuts down both replicas and closes the pool. It is safe to call
// multiple times (additional calls are no-ops from the server's perspective).
func (h *Harness) Cleanup() {
	if h.Primary != nil {
		h.Primary.Shutdown()
	}
	if h.Secondary != nil {
		h.Secondary.Shutdown()
	}
	// Pool is closed by t.Cleanup registered in StartPG.
}

// seedDEKAndEvents mints the initial DEK and inserts EventCount plaintext
// events_audit rows under SceneContext.
func (h *Harness) seedDEKAndEvents(t *testing.T, cfg HarnessConfig) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mgr := h.Primary.GetDEKManager()

	if cfg.EncryptUnderDEK {
		// Mint the initial DEK for the scene context.
		_, err := mgr.GetOrCreate(ctx, h.SceneContext, nil)
		Expect(err).NotTo(HaveOccurred(), "seedDEKAndEvents: GetOrCreate DEK")
	}

	// Seed EventCount plaintext events_audit rows. The rows use a fixed
	// subject matching HarnessConfig.EventSubject. For fixture purposes
	// the envelope is a minimal identity-codec JSON payload; E2E specs
	// exercise Phase 3 cold re-encryption which transforms these rows.
	for i := 0; i < cfg.EventCount; i++ {
		payload := []byte(`{"type":"test","seq":` + itoa(i) + `}`)
		_, err := h.DB.Exec(ctx,
			`INSERT INTO events_audit
			   (id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq, rendering)
			 VALUES (gen_random_bytes(16), $1, 'test.event', now(), 'system', $2, 1, 'identity', $3, '{}'::jsonb)`,
			cfg.EventSubject, payload, int64(i+1))
		Expect(err).NotTo(HaveOccurred(), "seedDEKAndEvents: insert event %d", i)
	}
}

// --- Assertion helpers ---

// AssertCheckpointStatus asserts that the checkpoint for reqID has the given status.
func (h *Harness) AssertCheckpointStatus(reqID dek.RequestID, expected dek.CheckpointStatus) {
	ckpt, err := h.repo().Get(context.Background(), reqID)
	Expect(err).NotTo(HaveOccurred())
	Expect(ckpt.Status).To(Equal(expected))
}

// AssertCryptoKeysActiveVersion asserts that the active DEK for ctx has the given version.
func (h *Harness) AssertCryptoKeysActiveVersion(ctx dek.ContextID, version uint32) {
	var v uint32
	err := h.DB.QueryRow(context.Background(),
		`SELECT version FROM crypto_keys
		  WHERE context_type = $1 AND context_id = $2
		    AND rotated_at IS NULL AND destroyed_at IS NULL`,
		ctx.Type, ctx.ID).Scan(&v)
	Expect(err).NotTo(HaveOccurred(), "AssertCryptoKeysActiveVersion")
	Expect(v).To(Equal(version))
}

// AssertCryptoKeysDestroyedAtSet asserts that the crypto_keys row with id=dekID
// has destroyed_at set.
func (h *Harness) AssertCryptoKeysDestroyedAtSet(dekID int64) {
	var count int
	err := h.DB.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM crypto_keys WHERE id = $1 AND destroyed_at IS NOT NULL`,
		dekID).Scan(&count)
	Expect(err).NotTo(HaveOccurred(), "AssertCryptoKeysDestroyedAtSet")
	Expect(count).To(Equal(1), "crypto_keys row id=%d must have destroyed_at set", dekID)
}

// AssertAuditEventEmitted asserts that at least one events_audit row exists with
// subject matching subjectPattern (SQL LIKE) and type = eventType.
func (h *Harness) AssertAuditEventEmitted(subjectPattern, eventType string) {
	var count int
	err := h.DB.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM events_audit WHERE subject LIKE $1 AND type = $2`,
		subjectPattern, eventType).Scan(&count)
	Expect(err).NotTo(HaveOccurred(), "AssertAuditEventEmitted")
	Expect(count).To(BeNumerically(">", 0),
		"expected at least one events_audit row with subject LIKE %q and type %q",
		subjectPattern, eventType)
}

// AssertRekeyChainIntactForContext walks the rekey audit chain for ctx via
// the primary replica's chain verifier (INV-E14/E15).
func (h *Harness) AssertRekeyChainIntactForContext(ctx dek.ContextID) {
	scope := ctx.Type + ":" + ctx.ID
	err := h.Primary.GetAuditChainVerifier().VerifyScope(
		context.Background(), dek.RekeyHandlerFor(h.Game), scope)
	Expect(err).NotTo(HaveOccurred(), "INV-E14/E15: chain intact for %s", scope)
}

// --- Fault injection helpers ---

// KillPrimaryMidPhase3 installs a batch hook that panics after reqID has been
// running for targetRow rows, simulating a mid-Phase-3 crash.
func (h *Harness) KillPrimaryMidPhase3(targetRow int) {
	h.Primary.GetRekeyOrchestrator().SetBatchHookForTest(func(rowsSoFar int) {
		if rowsSoFar >= targetRow {
			h.Primary.GetRekeyOrchestrator().SetBatchHookForTest(nil)
			panic("simulated mid-Phase-3 crash")
		}
	})
}

// RestartPrimary clears the crash hook so the next Run invocation resumes
// normally. The orchestrator state (checkpoint, DEK rows) is in Postgres and
// survives the simulated crash.
func (h *Harness) RestartPrimary() {
	h.Primary.GetRekeyOrchestrator().SetBatchHookForTest(nil)
}

// IsolateReplica simulates a network partition by name (currently a no-op
// placeholder; Phase 5 isolation tests wire this via real cluster tooling).
func (h *Harness) IsolateReplica(_ string) {
	// TODO(holomush-jxo8.7.36): wire real NATS connection blocking
}

// ReconnectReplica reverses IsolateReplica.
func (h *Harness) ReconnectReplica(_ string) {
	// TODO(holomush-jxo8.7.36): wire real NATS connection unblocking
}

// repo is a convenience accessor for the primary's CheckpointRepo.
func (h *Harness) repo() *dek.CheckpointRepo {
	return h.Primary.GetCheckpointRepo()
}

// itoa converts an int to its decimal string representation without importing fmt.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 20)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
