// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

// admin_rekey_e2e_test.go — production-boot E2E test for the Rekey RPC.
//
// Closes the gap between (a) in-process orchestrator tests under
// test/integration/crypto/ (which use holomushtest.Server) and (b) the full
// production-boot path exercised via runCoreWithDeps + the real admin UDS.
//
// prometheus.DefaultRegisterer is process-wide, so runCoreWithDeps must be
// called at most once per test binary. This file piggybacks onto the existing
// TestAdminAuthenticateE2E boot: runAdminRekeyScenario is called from the
// tail of admin_authenticate_e2e_test.go's Describe+It block after T25/T26.
//
// TDD acceptance criterion (bead jxo8.7.46):
//   - Invoke Rekey via the production UDS against the running server.
//   - Assert RekeyCompleted terminal event with non-empty request_id and
//     audit_event_id.
//   - Assert Phase 7 audit event (crypto.system.rekey) in events_audit with
//     matching request_id and correct context.type/context.id.
//   - Assert DEK rotation: new active DEK at version 2, old DEK destroyed.
//
// Design notes:
//   - Single-control rekey (DualControlRequired: []string{} from the Auth env's
//     cryptoConfig) — dual-control E2E is a separate bead.
//   - The DEK for the scene context is seeded by seedAdminRekeyDEK, called
//     from setupAdminAuthEnv BEFORE runCoreWithDeps, using the kekProvider
//     that was constructed from the same KEK file the server will load.
//     This satisfies INV-CRYPTO-19 (wrap_key_id fingerprint must match runtime KEK).
//   - Two fields are added to adminAuthEnv: rekeySceneContextType and
//     rekeySceneContextID, populated by seedAdminRekeyDEK.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// seedAdminRekeyDEK mints a v1 DEK for the given (contextType, contextID)
// into the already-initialised PG database. It uses kekProv — the same
// kek.Provider constructed from the test KEK file source that the server
// will load at boot — so the wrap_key_id fingerprint matches the runtime
// KEK (INV-CRYPTO-19).
//
// pool MUST still be open when this is called. It is the seedPool that
// setupAdminAuthEnv constructs before closing at step 4. Returns the
// (contextType, contextID) for the caller to store in the env fixture.
func seedAdminRekeyDEK(
	ctx context.Context,
	pool *pgxpool.Pool,
	kekProv kek.Provider,
	contextType, contextID string,
) {
	dekStore := dek.NewStore(pool)
	dekCache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: 5 * time.Minute})
	partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: 5 * time.Minute})
	noopInv := func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil }
	bindings := worldpostgres.NewBindingRepository(pool)

	mgr, err := dek.NewManager(kekProv, dekStore, dekCache, partCache, noopInv, bindings)
	Expect(err).NotTo(HaveOccurred(), "seedAdminRekeyDEK: dek.NewManager")

	_, err = mgr.GetOrCreate(ctx, dek.ContextID{Type: contextType, ID: contextID}, nil)
	Expect(err).NotTo(HaveOccurred(), "seedAdminRekeyDEK: GetOrCreate v1 DEK")
}

// runAdminRekeyScenario drives the T27 Rekey E2E scenario against the
// already-booted server in env. Called from admin_authenticate_e2e_test.go's
// Describe+It block at the end of the T25/T26 scenarios.
func runAdminRekeyScenario(env *adminAuthEnv) {
	// Capture pre-Rekey DEK state.
	preDEKVersion := rekeyActiveDEKVersion(env, env.rekeySceneContextType, env.rekeySceneContextID)
	Expect(preDEKVersion).To(Equal(1),
		"pre-Rekey active DEK must be v1 (seeded by seedAdminRekeyDEK)")
	preDEKID := rekeyInitialDEKID(env, env.rekeySceneContextType, env.rekeySceneContextID)
	Expect(preDEKID).To(BeNumerically(">", int64(0)),
		"pre-Rekey DEK id must be positive")

	By("T27 Rekey E2E: reuse carol's session token from T26 and Rekey the seeded scene context")

	// Reuse carol's session token captured in T26 scenario 2 (stored in
	// env.carolSessionToken). Re-authenticating would fail with TOTP replay
	// prevention if T27 runs within the same 30-second HOTP step window as T26.
	// The in-memory session store has no expiry, so the T26 token is still valid.
	carolToken := env.carolSessionToken
	Expect(carolToken).NotTo(BeEmpty(),
		"T27: env.carolSessionToken must be set by T26 scenario 2")

	By("T27 Rekey E2E: invoke Rekey RPC via production UDS and read stream to completion")
	requestID, auditEventID, rekeyErr := rekeyRunViaUDS(env, carolToken,
		env.rekeySceneContextType, env.rekeySceneContextID,
		"T27 production-boot E2E rekey test (jxo8.7.46)")
	Expect(rekeyErr).NotTo(HaveOccurred(), "T27: Rekey must complete without error")
	Expect(requestID).NotTo(BeEmpty(), "T27: RekeyCompleted.RequestId must be non-empty")
	Expect(auditEventID).NotTo(BeEmpty(), "T27: RekeyCompleted.AuditEventId must be non-empty")

	By("T27 Rekey E2E: assert crypto.system.rekey audit event appears in events_audit")
	// Audit projection is asynchronous (JetStream consumer). Use Eventually
	// to drain before asserting row presence.
	Eventually(func() int {
		return rekeyAuditEventCount(env, env.rekeySceneContextType, env.rekeySceneContextID)
	}, "15s", "200ms").Should(BeNumerically(">=", 1),
		"T27: at least one crypto.system.rekey row must appear in events_audit")

	By("T27 Rekey E2E: assert audit payload shape — request_id + context fields")
	// The production audit projection stores the JetStream message data
	// (proto-marshalled eventbusv1.Event) as envelope. Unmarshal the proto
	// first, then decode the JSON payload to get RekeyAuditPayload.
	var rawRID dek.RequestID
	copy(rawRID[:], requestID)
	ridStr := rawRID.String()

	// Query the most recent crypto.system.rekey row for this context's subject.
	auditSubject := "events." + env.gameID + ".system.rekey." +
		env.rekeySceneContextType + "." + env.rekeySceneContextID
	var envelopeBytes []byte
	err := env.queryPool.QueryRow(
		env.ctx,
		`SELECT envelope FROM events_audit
		  WHERE subject = $1 AND type = 'crypto.system.rekey'
		  ORDER BY js_seq DESC LIMIT 1`,
		auditSubject,
	).Scan(&envelopeBytes)
	Expect(err).NotTo(HaveOccurred(), "T27: fetch crypto.system.rekey envelope from events_audit")

	// The envelope is a proto-marshalled eventbusv1.Event. Decode it to
	// get the raw JSON payload (RekeyAuditPayload).
	var protoEv eventbusv1.Event
	Expect(proto.Unmarshal(envelopeBytes, &protoEv)).To(Succeed(),
		"T27: envelope must be proto-unmarshalable as eventbusv1.Event")
	var auditPayload dek.RekeyAuditPayload
	Expect(json.Unmarshal(protoEv.Payload, &auditPayload)).To(Succeed(),
		"T27: eventbusv1.Event.Payload must be valid RekeyAuditPayload JSON")
	Expect(auditPayload.RequestID).To(Equal(ridStr),
		"T27: audit payload request_id must match RekeyCompleted.RequestId (Phase 7 audit)")
	Expect(auditPayload.Context.Type).To(Equal(env.rekeySceneContextType),
		"T27: audit payload context.type must match Rekey context")
	Expect(auditPayload.Context.ID).To(Equal(env.rekeySceneContextID),
		"T27: audit payload context.id must match Rekey context")

	By("T27 Rekey E2E: assert DEK rotation — new active DEK at version 2")
	postDEKVersion := rekeyActiveDEKVersion(env, env.rekeySceneContextType, env.rekeySceneContextID)
	Expect(postDEKVersion).To(Equal(2),
		"T27: post-Rekey active DEK MUST be v2 (rotated from v1)")

	By("T27 Rekey E2E: assert old DEK has destroyed_at set (Phase 6 destruction)")
	var destroyedAtCount int
	err = env.queryPool.QueryRow(
		env.ctx,
		`SELECT COUNT(*) FROM crypto_keys WHERE id = $1 AND destroyed_at IS NOT NULL`,
		preDEKID,
	).Scan(&destroyedAtCount)
	Expect(err).NotTo(HaveOccurred(), "T27: query destroyed_at on old DEK row")
	Expect(destroyedAtCount).To(Equal(1),
		"T27: old DEK (id=%d) MUST have destroyed_at set after Rekey (Phase 6)", preDEKID)
}

// --- Private helpers ---

// rekeyRunViaUDS calls AdminService.Rekey over the UDS using sessionToken.
// Reads the server stream until RekeyCompleted or RekeyError arrives.
// Returns (requestID bytes, auditEventID bytes, error).
func rekeyRunViaUDS(
	env *adminAuthEnv,
	sessionToken, contextType, contextID, justification string,
) ([]byte, []byte, error) {
	ctx, cancel := context.WithTimeout(env.ctx, 120*time.Second)
	defer cancel()

	stream, err := env.client.Rekey(ctx, connect.NewRequest(&adminv1.RekeyRequest{
		SessionToken:  sessionToken,
		ContextType:   contextType,
		ContextId:     contextID,
		Justification: justification,
	}))
	if err != nil {
		return nil, nil, err
	}

	for stream.Receive() {
		msg := stream.Msg()
		switch ev := msg.Event.(type) {
		case *adminv1.RekeyProgress_Completed:
			return ev.Completed.GetRequestId(), ev.Completed.GetAuditEventId(), nil
		case *adminv1.RekeyProgress_Error:
			//nolint:goerr113 // dynamic test error — not production code
			return nil, nil, connect.NewError(connect.CodeInternal,
				rekeyE2EStringErr(ev.Error.GetCode()+": "+ev.Error.GetMessage()))
		}
	}
	if streamErr := stream.Err(); streamErr != nil {
		return nil, nil, streamErr
	}
	//nolint:goerr113 // dynamic test error — not production code
	return nil, nil, connect.NewError(connect.CodeInternal,
		rekeyE2EStringErr("Rekey stream closed without terminal event"))
}

// rekeyE2EStringErr is a string-backed error for Rekey stream failures.
type rekeyE2EStringErr string

func (e rekeyE2EStringErr) Error() string { return string(e) }

// rekeyAuditEventCount returns the number of crypto.system.rekey rows in
// events_audit for the given (contextType, contextID) scope.
func rekeyAuditEventCount(env *adminAuthEnv, contextType, contextID string) int {
	subject := "events." + env.gameID + ".system.rekey." + contextType + "." + contextID
	var count int
	err := env.queryPool.QueryRow(
		env.ctx,
		`SELECT COUNT(*) FROM events_audit WHERE subject = $1 AND type = 'crypto.system.rekey'`,
		subject,
	).Scan(&count)
	Expect(err).NotTo(HaveOccurred(), "rekeyAuditEventCount: events_audit COUNT(*)")
	return count
}

// rekeyActiveDEKVersion returns the version of the active DEK row for the
// given context. Returns 0 if no active row exists. Connection / non-ErrNoRows
// errors fail loudly so a transient DB issue during the post-Rekey check
// surfaces as the real cause rather than masquerading as "no rotation".
func rekeyActiveDEKVersion(env *adminAuthEnv, contextType, contextID string) int {
	var version int
	err := env.queryPool.QueryRow(
		env.ctx,
		`SELECT version FROM crypto_keys
		  WHERE context_type = $1 AND context_id = $2
		    AND rotated_at IS NULL AND destroyed_at IS NULL
		  ORDER BY version DESC LIMIT 1`,
		contextType, contextID,
	).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0
	}
	Expect(err).NotTo(HaveOccurred(), "rekeyActiveDEKVersion query")
	return version
}

// rekeyInitialDEKID returns the primary-key id of the current active DEK
// row for (contextType, contextID). Used to record the pre-Rekey DEK id.
func rekeyInitialDEKID(env *adminAuthEnv, contextType, contextID string) int64 {
	var id int64
	err := env.queryPool.QueryRow(
		env.ctx,
		`SELECT id FROM crypto_keys
		  WHERE context_type = $1 AND context_id = $2
		    AND rotated_at IS NULL AND destroyed_at IS NULL
		  ORDER BY version DESC LIMIT 1`,
		contextType, contextID,
	).Scan(&id)
	Expect(err).NotTo(HaveOccurred(), "rekeyInitialDEKID query")
	return id
}
