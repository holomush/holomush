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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega" //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/admin/policy"
	auditchain "github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/testsupport/holomushtest"
)

// Harness is the per-spec rekey test fixture. It holds two server replicas
// sharing one Postgres pool, a pre-seeded 1000-event fixture, and assertion /
// fault-injection helpers.
type Harness struct {
	Primary       *holomushtest.Server
	Secondary     *holomushtest.Server
	AdminCli      *holomushtest.AdminClient
	DB            *pgxpool.Pool
	Game          string
	AdminPlayer   holomushtest.PlayerCreds
	PartnerPlayer holomushtest.PlayerCreds
	SceneContext  dek.ContextID
	// OriginalPolicyHash stores the policy_hash captured by RememberCurrentPolicyHash.
	// Used by INV-CRYPTO-112 tests to assert the hash did not change after a policy edit.
	OriginalPolicyHash string
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

	// Wire the default player ID for AdminCli.Rekey/RekeyStatus/RekeyList helpers.
	h.AdminCli.SetDefaultPlayerID(h.AdminPlayer.PlayerID)

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

	// Seed EventCount plaintext events_audit rows in a single round-trip.
	// The rows use a fixed subject matching HarnessConfig.EventSubject. For
	// fixture purposes the envelope is a minimal identity-codec JSON payload
	// (`{"type":"test","seq":N}`, N starting at 0); E2E specs exercise Phase 3
	// cold re-encryption which transforms these rows. js_seq is 1-indexed.
	//
	// Note: timestamp column is BIGINT-ns post-gfo6 (INV-STORE-1); the SQL-side
	// (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT expression evaluates once per
	// statement, so all seeded rows share one timestamp. Specs that need a
	// deterministic order over the seed rows MUST order by js_seq, not timestamp.
	_, err := h.DB.Exec(ctx, `
		INSERT INTO events_audit
		    (id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq, rendering)
		SELECT
		    gen_random_bytes(16),
		    $1,
		    'test.event',
		    (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
		    'system',
		    convert_to('{"type":"test","seq":' || (g.i - 1)::text || '}', 'UTF8'),
		    1,
		    'identity',
		    g.i,
		    '{}'::jsonb
		FROM generate_series(1, $2::int) AS g(i)`,
		cfg.EventSubject, cfg.EventCount)
	Expect(err).NotTo(HaveOccurred(), "seedDEKAndEvents: bulk insert events")
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
// the primary replica's chain verifier (INV-CRYPTO-101/INV-CRYPTO-102).
func (h *Harness) AssertRekeyChainIntactForContext(ctx dek.ContextID) {
	scope := ctx.Type + ":" + ctx.ID
	err := h.Primary.GetAuditChainVerifier().VerifyScope(
		context.Background(), dek.RekeyHandlerFor(h.Game), scope,
	)
	Expect(err).NotTo(HaveOccurred(), "INV-CRYPTO-101/INV-CRYPTO-102: chain intact for %s", scope)
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

// SeedStaleCheckpoint inserts a checkpoint row directly into the DB with
// last_heartbeat_at set to now() minus age, simulating a stale in-flight
// rekey whose operator has gone away. Used by sweep-TTL E2E specs.
//
// It allocates its own DEK row (with a unique ID based on contextType+contextID
// to avoid conflicts) to satisfy the foreign-key constraint.
func (h *Harness) SeedStaleCheckpoint(ctxType, ctxID string, age time.Duration) dek.RequestID {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Insert a minimal crypto_keys row to satisfy the FK on old_dek_id.
	// Use a large synthetic ID to avoid collisions with DEKs seeded by SetupRekeyHarness.
	const syntheticDEKID int64 = 88880001
	_, _ = h.DB.Exec(ctx,
		`INSERT INTO crypto_keys
		   (id, context_type, context_id, version, wrapped_dek, wrap_provider,
		    wrap_key_id, participants, created_at)
		 VALUES ($1, $2, $3, 99, '\x00', 'stale-test', 'stale-test', '[]'::jsonb, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
		 ON CONFLICT (id) DO NOTHING`,
		syntheticDEKID, ctxType, ctxID)

	rid := dek.RequestID(idgen.New())
	_, err := h.DB.Exec(ctx, `
		INSERT INTO crypto_rekey_checkpoints
		  (request_id, context_type, context_id, op_args_hash, policy_hash,
		   primary_player_id, status, old_dek_id, last_heartbeat_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT - $8::BIGINT)
	`, rid[:], ctxType, ctxID,
		make([]byte, 32), make([]byte, 32),
		h.AdminPlayer.PlayerID, syntheticDEKID,
		age.Nanoseconds())
	Expect(err).NotTo(HaveOccurred(), "SeedStaleCheckpoint: INSERT failed")
	return rid
}

// findCheckpointByID loads the checkpoint row for the given RequestID. Returns
// (Checkpoint, error) rather than calling Expect so callers can use Eventually.
func (h *Harness) findCheckpointByID(rid dek.RequestID) (dek.Checkpoint, error) {
	return h.repo().Get(context.Background(), rid)
}

// repo is a convenience accessor for the primary's CheckpointRepo.
func (h *Harness) repo() *dek.CheckpointRepo {
	return h.Primary.GetCheckpointRepo()
}

// --- Status/list helpers (Task 48) ---

// findCheckpoint returns the most recent checkpoint for the given context.
// Fails the test if no checkpoint exists.
func (h *Harness) findCheckpoint(ctxType, ctxID string) dek.Checkpoint {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ckpt, found, err := h.Primary.GetCheckpointRepo().FindNonTerminalByContext(ctx, ctxType, ctxID)
	if !found {
		// Fall back to any terminal row for status/list assertions.
		rows, ferr := h.Primary.GetCheckpointRepo().ListFiltered(ctx, dek.CheckpointListFilter{
			IncludeTerminal: true,
			ContextPattern:  &ctxID,
		})
		Expect(ferr).NotTo(HaveOccurred(), "findCheckpoint: ListFiltered for %s:%s", ctxType, ctxID)
		Expect(rows).NotTo(BeEmpty(), "findCheckpoint: no checkpoint found for %s:%s", ctxType, ctxID)
		return rows[0]
	}
	Expect(err).NotTo(HaveOccurred(), "findCheckpoint: FindNonTerminalByContext for %s:%s", ctxType, ctxID)
	return ckpt
}

// SeedCompletedCheckpoint inserts a terminal (complete) checkpoint row for
// (ctxType, ctxID). Used by list-filter specs that need a pre-existing
// completed entry. Allocates its own DEK row using the same pattern as
// SeedStaleCheckpoint (version=99, large synthetic id based on context strings).
func (h *Harness) SeedCompletedCheckpoint(ctxType, ctxID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Compute a synthetic DEK id that is unlikely to collide: hash the key string
	// into a large int64 using a stable deterministic formula.
	dekID := seedDEKID(77770000, ctxType, ctxID)
	// crypto_keys.created_at is BIGINT-ns post-gfo6 (INV-STORE-1).
	_, _ = h.DB.Exec(ctx,
		`INSERT INTO crypto_keys
		   (id, context_type, context_id, version, wrapped_dek, wrap_provider,
		    wrap_key_id, participants, created_at)
		 VALUES ($1, $2, $3, 99, '\x00', 'seed-complete', 'seed-complete', '[]'::jsonb,
		         (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
		 ON CONFLICT (id) DO NOTHING`,
		dekID, ctxType, ctxID)
	// Verify the row exists (handles both "just inserted" and "already existed" cases).
	var actualDEKID int64
	err := h.DB.QueryRow(ctx,
		`SELECT id FROM crypto_keys WHERE id = $1`, dekID).Scan(&actualDEKID)
	Expect(err).NotTo(HaveOccurred(), "SeedCompletedCheckpoint: DEK not found for %s:%s", ctxType, ctxID)

	rid := dek.RequestID(idgen.New())
	_, err = h.DB.Exec(ctx, `
		INSERT INTO crypto_rekey_checkpoints
		  (request_id, context_type, context_id, op_args_hash, policy_hash,
		   primary_player_id, status, old_dek_id, completed_at, last_heartbeat_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'complete', $7,
		        (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
		        (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
	`, rid[:], ctxType, ctxID,
		make([]byte, 32), make([]byte, 32),
		h.AdminPlayer.PlayerID, actualDEKID)
	Expect(err).NotTo(HaveOccurred(), "SeedCompletedCheckpoint: checkpoint INSERT failed for %s:%s", ctxType, ctxID)
}

// SeedActiveCheckpoint inserts a non-terminal (pending) checkpoint row for
// (ctxType, ctxID). Used by list-filter specs that need a live in-flight entry.
// Allocates its own DEK row using the same synthetic-id pattern.
func (h *Harness) SeedActiveCheckpoint(ctxType, ctxID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dekID := seedDEKID(88890000, ctxType, ctxID)
	// crypto_keys.created_at is BIGINT-ns post-gfo6 (INV-STORE-1).
	_, _ = h.DB.Exec(ctx,
		`INSERT INTO crypto_keys
		   (id, context_type, context_id, version, wrapped_dek, wrap_provider,
		    wrap_key_id, participants, created_at)
		 VALUES ($1, $2, $3, 98, '\x00', 'seed-active', 'seed-active', '[]'::jsonb,
		         (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
		 ON CONFLICT (id) DO NOTHING`,
		dekID, ctxType, ctxID)
	var actualDEKID int64
	err := h.DB.QueryRow(ctx,
		`SELECT id FROM crypto_keys WHERE id = $1`, dekID).Scan(&actualDEKID)
	Expect(err).NotTo(HaveOccurred(), "SeedActiveCheckpoint: DEK not found for %s:%s", ctxType, ctxID)

	rid := dek.RequestID(idgen.New())
	_, err = h.DB.Exec(ctx, `
		INSERT INTO crypto_rekey_checkpoints
		  (request_id, context_type, context_id, op_args_hash, policy_hash,
		   primary_player_id, status, old_dek_id, last_heartbeat_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7,
		        (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
	`, rid[:], ctxType, ctxID,
		make([]byte, 32), make([]byte, 32),
		h.AdminPlayer.PlayerID, actualDEKID)
	Expect(err).NotTo(HaveOccurred(), "SeedActiveCheckpoint: checkpoint INSERT failed for %s:%s", ctxType, ctxID)
}

// seedDEKID produces a stable synthetic DEK id from a base and context strings.
// The formula uses string lengths and byte sums to produce a value that
// is deterministic yet unlikely to collide with production or harness DEK ids.
func seedDEKID(base int64, ctxType, ctxID string) int64 {
	var sum int64
	for _, b := range []byte(ctxType + ctxID) {
		sum += int64(b)
	}
	return base + sum%10000
}

// --- policy_set chain helpers (Task 49) ---

// AssertPolicySetChainIntact walks the policy_set chain for policyName via
// the primary's chain verifier (INV-CRYPTO-77/INV-CRYPTO-78/INV-CRYPTO-79 via the generalized verifier).
func (h *Harness) AssertPolicySetChainIntact(policyName string) {
	handler := policy.PolicySetHandlerFor(h.Game)
	err := h.Primary.VerifierForChain(handler).VerifyScope(
		context.Background(), handler, policyName,
	)
	Expect(err).NotTo(HaveOccurred(),
		"INV-CRYPTO-77/INV-CRYPTO-78/INV-CRYPTO-79: policy_set chain must be intact for %q", policyName)
}

// TamperPolicySetSelfHash overwrites the policy_hash field in the most recent
// policy_set audit row for policyName. The tampered value is a valid base64-
// encoded 32-byte sequence (0xdeadbeef... repeated), which is a structurally
// valid []byte but does not match the SHA-256 that RecomputeSelfHash would
// compute. This causes AUDIT_CHAIN_HASH_MISMATCH on the next verification pass.
//
// The envelope is replaced with a JSON body whose policy_hash is the
// base64 encoding of 32 0xde bytes (a sentinel "dead" value). Since
// decodePolicyPayloadJSON falls back to raw-JSON parse for non-proto bytes,
// and PolicySetPayload.PolicyHash is []byte (base64 in JSON), this produces
// a valid parsed payload with the wrong hash.
func (h *Harness) TamperPolicySetSelfHash(policyName string) {
	subject := "events." + h.Game + ".system.crypto_policy." + policyName

	// base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xde}, 32))
	// = "3t7e3t7e3t7e3t7e3t7e3t7e3t7e3t7e3t7e3t7e3t7e"  (32 bytes of 0xde)
	deadHash := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xde}, 32))

	tamperedJSON := `{"policy_name":"` + policyName + `","policy_hash":"` + deadHash + `","prev_hash":null,"policy_snapshot":{"required_op_kinds":["rekey"]},"timestamp":"2026-01-01T00:00:00Z","server_start_ulid":"01JX00000000000000000000000","server_identity":"tampered"}`

	tag, err := h.DB.Exec(context.Background(),
		`UPDATE events_audit
		    SET envelope = $1
		  WHERE id = (
		    SELECT id FROM events_audit
		     WHERE subject = $2 AND type = 'crypto.policy_set'
		     ORDER BY js_seq DESC
		     LIMIT 1
		  )`,
		[]byte(tamperedJSON), subject)
	Expect(err).NotTo(HaveOccurred(), "TamperPolicySetSelfHash: UPDATE failed for %q", policyName)
	Expect(tag.RowsAffected()).To(BeNumerically("==", 1),
		"TamperPolicySetSelfHash: expected exactly 1 row updated for subject=%q", subject)
}

// --- policy-hash-frozen helpers (Task 50) ---

// RememberCurrentPolicyHash reads the current tail hash of the policy_set chain
// for policyName and stores it in h.OriginalPolicyHash. Used by INV-CRYPTO-112 tests
// to capture the hash before a mid-Rekey policy edit.
//
// Encoding mirrors Phase 7's audit payload format:
//   - "sha256:<hex>" for an existing chain head (what Phase 1 would freeze).
//   - "sha256:" + 64 zero hex digits when the chain is empty (genesis): Phase 1
//     stores make([]byte, 32) (zero sentinel) and Phase 7 encodes it as zeros.
func (h *Harness) RememberCurrentPolicyHash(policyName string) {
	chainRepo := h.Primary.GetChainRepo()
	prevHash, err := chainEmitterForPolicy(chainRepo, h.Game, policyName)
	Expect(err).NotTo(HaveOccurred(), "RememberCurrentPolicyHash: ComputePrevHashFor failed for %q", policyName)
	if prevHash == nil {
		// Genesis: Phase 1 stores 32 zero bytes. Phase 7 encodes as sha256:<zeros>.
		h.OriginalPolicyHash = "sha256:" + hex.EncodeToString(make([]byte, 32))
		return
	}
	h.OriginalPolicyHash = "sha256:" + hex.EncodeToString(prevHash)
}

// LoadRekeyAuditEvent loads and decodes the rekey audit event identified by
// the given 16-byte raw request_id slice. Returns the decoded RekeyAuditPayload
// so callers can assert PolicyHash (INV-CRYPTO-112) and other fields.
//
// The raw 16 bytes are converted to ULID string format (same as rid.String())
// and matched against the "request_id" field in the JSON payload.
func (h *Harness) LoadRekeyAuditEvent(rid []byte) dek.RekeyAuditPayload {
	// Convert raw bytes → dek.RequestID → ULID string (same format as Phase 7 stores).
	var rawRID dek.RequestID
	copy(rawRID[:], rid)
	ridStr := rawRID.String()

	var envelope []byte
	err := h.DB.QueryRow(context.Background(),
		`SELECT envelope FROM events_audit
		  WHERE type = 'crypto.system.rekey'
		    AND convert_from(envelope, 'UTF8') LIKE $1
		  ORDER BY js_seq DESC
		  LIMIT 1`,
		"%"+ridStr+"%").Scan(&envelope)
	Expect(err).NotTo(HaveOccurred(), "LoadRekeyAuditEvent: query for request_id=%s", ridStr)

	var payload dek.RekeyAuditPayload
	Expect(json.Unmarshal(envelope, &payload)).NotTo(HaveOccurred(),
		"LoadRekeyAuditEvent: unmarshal payload for request_id=%s", ridStr)
	return payload
}

// chainEmitterForPolicy constructs a chain.Emitter from a chain.Repo and
// calls ComputePrevHashFor for policyName. Returns the prev_hash bytes or nil
// (genesis). Used by RememberCurrentPolicyHash.
func chainEmitterForPolicy(repo auditchain.Repo, gameID, policyName string) ([]byte, error) {
	em := auditchain.NewEmitter(repo)
	handler := policy.PolicySetHandlerFor(gameID)
	prevHash, _, err := em.ComputePrevHashFor(context.Background(), handler, policyName)
	return prevHash, err
}
