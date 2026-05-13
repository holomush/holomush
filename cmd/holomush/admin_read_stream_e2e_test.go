// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

// admin_read_stream_e2e_test.go — production-boot E2E test for the
// AdminReadStream RPC (sub-epic F r8). Drives the 5 happy-path scenarios
// F-E1, F-E2, F-E13, F-E14, F-E17 against the live admin UDS surface.
//
// ADR 0017: F bypasses HistoryReader/dispatcher entirely. Seeding writes
// events_audit rows directly via pgxpool — no HistoryReader detour.
//
// Single-process constraint: prometheus.DefaultRegisterer is process-wide,
// so runCoreWithDeps may only be called once per test binary. This file
// piggybacks onto the existing TestAdminAuthenticateE2E boot:
// runAdminReadStreamScenarios is called from the tail of
// admin_authenticate_e2e_test.go's Describe+It block (after T25/T26/T27).
//
// Helpers exported for R.18 (validation scenarios) and R.19 (lifecycle
// scenarios) reuse:
//   - seedAdminReadStreamData              — bulk-seed encrypted rows
//   - seedPlainAuditRow                    — identity-codec sentinel
//   - seedOrphanDEKAuditRow                — DEK reference that won't resolve
//   - adminReadStreamView                  — accumulator type
//   - (env).RunAdminReadStream             — driver
//   - RunAdminReadStreamArgs               — driver args
//   - (env).encryptForCold                 — DEK + codec + AAD helper
//   - (env).readstreamKEKProvider          — post-boot KEK provider builder
//   - (env).readstreamDEKManager           — post-boot DEK manager builder
//
// Classifier note (F-E17): r8's DecryptRow classifier never emits DEKMissing
// or DEKBadColumns in the realised production codepaths. The brief's
// originally-planned shape ("dek_ref set but dek_version=0" / "malformed
// dek_ref bytes") is unreachable today — R.10's cold_reader either rejects
// the row at scan time (negative dek_ref / negative dek_version) or the
// row's DEK lookup fails through dek.Manager and returns DEK_NOT_FOUND,
// which the classifier maps to STALE_DEK (not DEKMissing / DEKBadColumns).
// F-E17 therefore exercises the realistic STALE_DEK path: 5 rows whose
// dek_ref points to nonexistent crypto_keys IDs (orphans). All 5 frames
// arrive metadata-only with NoPlaintextReason=STALE_DEK. INV-F12's
// "metadata-only on missing DEK" contract is enforced; the specific reason
// enum is whichever the production classifier emits.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// =============================================================================
// Helpers — exported (lowercase pkg-private; the suffix _test.go limits scope
// to the same package, sibling files reuse them).
// =============================================================================

// RunAdminReadStreamArgs is the per-call driver argument set. Every field has
// a zero-value-safe default: SessionToken empty → uses env.carolSessionToken;
// Justification empty → boilerplate string; Contexts nil → whole-game wildcard;
// Since/Until zero → server defaults; DualControl false → fast path.
type RunAdminReadStreamArgs struct {
	SessionToken   string
	Justification  string
	Contexts       []*adminv1.ContextRef
	Since          time.Time
	Until          time.Time
	DualControl    bool
	DualControlTTL time.Duration
	Limit          uint32
}

// adminReadStreamView accumulates the streamed frames into an
// assertion-friendly shape.
type adminReadStreamView struct {
	pendingApprovalCount int
	started              *adminv1.ReadStarted
	events               []*corev1.EventFrame
	finished             *adminv1.ReadFinished
}

// EventCount returns the number of EventFrame frames received.
func (v *adminReadStreamView) EventCount() int { return len(v.events) }

// DecryptFailCount returns the number of metadata-only frames received.
// Mirrors the server-side decrypt_fail_count counter for INV-F12.
func (v *adminReadStreamView) DecryptFailCount() int {
	var n int
	for _, e := range v.events {
		if e.GetMetadataOnly() {
			n++
		}
	}
	return n
}

// TerminatedBy returns the ReadFinished terminator, or UNSPECIFIED if no
// ReadFinished frame was received.
func (v *adminReadStreamView) TerminatedBy() adminv1.ReadFinished_TerminatedBy {
	if v.finished == nil {
		return adminv1.ReadFinished_TERMINATED_BY_UNSPECIFIED
	}
	return v.finished.GetTerminatedBy()
}

// MetadataOnlyReasons returns the NoPlaintextReason for each metadata-only
// frame in receive order.
func (v *adminReadStreamView) MetadataOnlyReasons() []corev1.NoPlaintextReason {
	out := make([]corev1.NoPlaintextReason, 0)
	for _, e := range v.events {
		if e.GetMetadataOnly() {
			out = append(out, e.GetNoPlaintextReason())
		}
	}
	return out
}

// EventsAreTimestampOrdered reports whether the events were received in
// ascending Timestamp order. Used by F-E13 to validate global ordering.
func (v *adminReadStreamView) EventsAreTimestampOrdered() bool {
	for i := 1; i < len(v.events); i++ {
		prev := v.events[i-1].GetTimestamp()
		curr := v.events[i].GetTimestamp()
		if prev == nil || curr == nil {
			return false
		}
		if curr.AsTime().Before(prev.AsTime()) {
			return false
		}
	}
	return true
}

// PayloadsAllEmpty reports whether every metadata-only frame has an empty
// payload. Used as a defense-in-depth check against plaintext leakage.
func (v *adminReadStreamView) PayloadsAllEmpty() bool {
	for _, e := range v.events {
		if e.GetMetadataOnly() && len(e.GetPayload()) > 0 {
			return false
		}
	}
	return true
}

// RunAdminReadStream invokes AdminService.AdminReadStream over the UDS,
// drains frames until ReadFinished arrives (or the stream errors), and
// returns the accumulated view. Test asserts read view fields after this
// returns.
func (e *adminAuthEnv) RunAdminReadStream(args RunAdminReadStreamArgs) *adminReadStreamView {
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()

	token := args.SessionToken
	if token == "" {
		token = e.carolSessionToken
	}
	just := args.Justification
	if just == "" {
		just = "F-E2E test read"
	}

	req := &adminv1.AdminReadStreamRequest{
		SessionToken:  token,
		Justification: just,
		Context:       args.Contexts,
		DualControl:   args.DualControl,
		Limit:         args.Limit,
	}
	if !args.Since.IsZero() {
		req.Since = timestamppb.New(args.Since)
	}
	if !args.Until.IsZero() {
		req.Until = timestamppb.New(args.Until)
	}
	if args.DualControlTTL > 0 {
		req.DualControlTimeoutSeconds = uint32(args.DualControlTTL / time.Second) //nolint:gosec // G115: TTL is positive duration; bounded by tests
	}

	stream, err := e.client.AdminReadStream(ctx, connect.NewRequest(req))
	Expect(err).NotTo(HaveOccurred(), "AdminReadStream stream open")

	view := &adminReadStreamView{}
	for stream.Receive() {
		msg := stream.Msg()
		switch {
		case msg.GetPendingApproval() != nil:
			view.pendingApprovalCount++
		case msg.GetStarted() != nil:
			view.started = msg.GetStarted()
		case msg.GetEvent() != nil:
			view.events = append(view.events, msg.GetEvent())
		case msg.GetFinished() != nil:
			view.finished = msg.GetFinished()
		}
	}
	if streamErr := stream.Err(); streamErr != nil {
		// Stream errors are valid outcomes for some scenarios (R.18 will
		// exercise them). Happy-path scenarios assert TerminatedBy below.
		_ = streamErr //nolint:dogsled // intentional discard; the view captures the terminator
	}
	return view
}

// readstreamKEKProvider constructs a fresh kek.Provider against the env's
// PG pool using the KEK file path + passphrase exported as env vars by
// setupAdminAuthEnv. The returned provider unwraps the SAME KEK material
// that the running server's DEK manager uses, so DEK rows minted here are
// resolvable in-process by the server's DEK manager (INV-33).
func (e *adminAuthEnv) readstreamKEKProvider(ctx context.Context) kek.Provider {
	kekFile := os.Getenv("HOLOMUSH_KEK_FILE")
	Expect(kekFile).NotTo(BeEmpty(), "readstreamKEKProvider: HOLOMUSH_KEK_FILE env var must be set")
	passphrase := os.Getenv("HOLOMUSH_KEK_PASSPHRASE")
	Expect(passphrase).NotTo(BeEmpty(), "readstreamKEKProvider: HOLOMUSH_KEK_PASSPHRASE env var must be set")

	pf := func(_ context.Context) ([]byte, error) { return []byte(passphrase), nil }
	src, err := kek.NewFileSource(kekFile, pf)
	Expect(err).NotTo(HaveOccurred(), "readstreamKEKProvider: kek.NewFileSource")

	provider, err := kek.NewLocalAEADProvider(ctx, src, e.queryPool)
	Expect(err).NotTo(HaveOccurred(), "readstreamKEKProvider: kek.NewLocalAEADProvider")
	return provider
}

// readstreamDEKManager constructs a dek.Manager backed by env.queryPool and
// readstreamKEKProvider. The manager mints / resolves DEKs that the running
// server can also resolve (same KEK fingerprint, same PG store). Each call
// produces a fresh manager with its own cache — intentional, so a seeded DEK
// row is always visible via PG read regardless of cache state.
func (e *adminAuthEnv) readstreamDEKManager(ctx context.Context, pool *pgxpool.Pool) dek.Manager {
	provider := e.readstreamKEKProvider(ctx)
	dekStore := dek.NewStore(pool)
	dekCache := dek.NewCache(dek.CacheConfig{Capacity: 64, TTL: 5 * time.Minute})
	partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64, TTL: 5 * time.Minute})
	noopInv := func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil }
	bindings := worldpostgres.NewBindingRepository(pool)

	mgr, err := dek.NewManager(provider, dekStore, dekCache, partCache, noopInv, bindings)
	Expect(err).NotTo(HaveOccurred(), "readstreamDEKManager: dek.NewManager")
	return mgr
}

// readstreamSubjectForContext builds the publish-time subject for a context
// ref. Mirrors readstream.BuildSubjects's per-context shape, but produces an
// EXACT subject (no trailing ".>" — that wildcard is the server-side LIKE
// query's surface). The seed inserts the exact subject; the LIKE pattern
// "events.<game>.<type>.<id>.%" matches it.
func (e *adminAuthEnv) readstreamSubjectForContext(ctxType, ctxID, eventType string) string {
	return fmt.Sprintf("events.%s.%s.%s.%s", e.gameID, ctxType, ctxID, eventType)
}

// encryptForCold mints (if absent) a DEK for ctx, encrypts plaintext under
// the active key + AAD bound to (eventID, subject, ts), and returns the
// ciphertext along with the (dek_ref, dek_version, codec) tuple that
// events_audit consumers expect.
//
// The returned ciphertext is decryptable by the running server's DEK manager
// (same KEK file) — assuming dekManager and the server's manager both look
// at the same crypto_keys row.
func (e *adminAuthEnv) encryptForCold(
	ctx context.Context,
	dekManager dek.Manager,
	eventID ulid.ULID,
	subject string,
	ts time.Time,
	plaintext []byte,
	ctxType, ctxID string,
) (ciphertext []byte, dekRef int64, dekVersion int32, codecName string) {
	// 1. Get-or-create the v1 DEK for (ctxType, ctxID).
	key, err := dekManager.GetOrCreate(ctx, dek.ContextID{Type: ctxType, ID: ctxID}, nil)
	Expect(err).NotTo(HaveOccurred(), "encryptForCold: dekManager.GetOrCreate")

	// 2. Build the proto envelope (cleartext fields only; AAD binds them).
	envProto := &eventbusv1.Event{
		Id:        eventID[:],
		Subject:   subject,
		Type:      "test.encrypted",
		Timestamp: timestamppb.New(ts),
		Actor:     &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
	}

	// 3. Build AAD from cleartext envelope fields.
	aadBytes, err := aad.Build(envProto, string(codec.NameXChaCha20v1), uint64(key.ID), key.Version)
	Expect(err).NotTo(HaveOccurred(), "encryptForCold: aad.Build")

	// 4. Encrypt.
	c := codec.NewXChaCha20Poly1305v1()
	ct, err := c.Encode(ctx, plaintext, key, aadBytes)
	Expect(err).NotTo(HaveOccurred(), "encryptForCold: codec.Encode")

	return ct, int64(key.ID), int32(key.Version), string(codec.NameXChaCha20v1) //nolint:gosec // G115: KeyID is uint64 < 2^63; version is uint32 < 2^31
}

// seedAdminReadStreamData writes count*len(contexts) encrypted rows into
// events_audit. Each row is encrypted under the DEK for its (scene, ctxID)
// pair so the running server's DEK manager will resolve and decrypt it.
//
// Timestamps are spaced in millisecond increments anchored at (now - 1m) so
// they fall inside the server's default 1h since-window.
//
// Returns the generated event IDs in seed order. The caller may use these
// to assert receive-order matches insert-order on a single subject.
func (e *adminAuthEnv) seedAdminReadStreamData(
	ctxType string,
	contextIDs []string,
	countPerContext int,
) []ulid.ULID {
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()

	dekManager := e.readstreamDEKManager(ctx, e.queryPool)

	out := make([]ulid.ULID, 0, len(contextIDs)*countPerContext)
	baseTime := time.Now().UTC().Add(-1 * time.Minute)

	for ctxIdx, ctxID := range contextIDs {
		subject := e.readstreamSubjectForContext(ctxType, ctxID, "test.encrypted")
		for i := 0; i < countPerContext; i++ {
			id := ulid.Make()
			ts := baseTime.Add(time.Duration(ctxIdx*1000+i) * time.Millisecond)
			plaintext := []byte(fmt.Sprintf(`{"ctx":%q,"i":%d}`, ctxID, i))

			ciphertext, dekRef, dekVersion, codecName := e.encryptForCold(
				ctx, dekManager, id, subject, ts, plaintext, ctxType, ctxID,
			)

			// Build the full envelope WITH the ciphertext payload (mirror
			// publisher.go behaviour — the envelope stored in events_audit
			// carries Payload=ciphertext).
			envProto := &eventbusv1.Event{
				Id:        id[:],
				Subject:   subject,
				Type:      "test.encrypted",
				Timestamp: timestamppb.New(ts),
				Actor:     &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
				Payload:   ciphertext,
			}
			envelopeBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(envProto)
			Expect(err).NotTo(HaveOccurred(), "seedAdminReadStreamData: proto.Marshal")

			_, err = e.queryPool.Exec(ctx, `
				INSERT INTO events_audit
				  (id, subject, type, timestamp, actor_kind, actor_id,
				   envelope, schema_ver, codec, js_seq, rendering,
				   dek_ref, dek_version)
				VALUES ($1, $2, 'test.encrypted', $3, 'system', NULL,
				        $4, 1, $5, $6, '{}'::jsonb,
				        $7, $8)
			`, id[:], subject, ts, envelopeBytes, codecName,
				int64(time.Now().UnixNano())+int64(ctxIdx*1_000_000+i),
				dekRef, dekVersion)
			Expect(err).NotTo(HaveOccurred(), "seedAdminReadStreamData: INSERT events_audit")

			out = append(out, id)
		}
	}
	return out
}

// seedPlainAuditRow inserts a single identity-codec (cleartext) row into
// events_audit. dek_ref is NULL — the row MUST NOT be returned by the
// cold-tier filter (R.10's WHERE dek_ref IS NOT NULL clause excludes it).
// Used by F-E14 to assert the sensitive-content filter (INV-F15).
func (e *adminAuthEnv) seedPlainAuditRow(subject string, ts time.Time) ulid.ULID {
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()

	id := ulid.Make()
	envProto := &eventbusv1.Event{
		Id:        id[:],
		Subject:   subject,
		Type:      "test.cleartext",
		Timestamp: timestamppb.New(ts),
		Actor:     &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
		Payload:   []byte(`{"sensitive":false}`),
	}
	envelopeBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(envProto)
	Expect(err).NotTo(HaveOccurred(), "seedPlainAuditRow: proto.Marshal")

	_, err = e.queryPool.Exec(ctx, `
		INSERT INTO events_audit
		  (id, subject, type, timestamp, actor_kind, actor_id,
		   envelope, schema_ver, codec, js_seq, rendering)
		VALUES ($1, $2, 'test.cleartext', $3, 'system', NULL,
		        $4, 1, 'identity', $5, '{}'::jsonb)
	`, id[:], subject, ts, envelopeBytes, int64(time.Now().UnixNano()))
	Expect(err).NotTo(HaveOccurred(), "seedPlainAuditRow: INSERT events_audit")
	return id
}

// seedOrphanDEKAuditRow inserts an events_audit row whose dek_ref points to
// a nonexistent crypto_keys row. The envelope payload is a random byte
// string (cannot be decrypted; DEK lookup fails first anyway). DecryptRow's
// DEK-resolve step returns DEK_NOT_FOUND, which classifyDecryptErr maps to
// NoPlaintextReason_STALE_DEK (INV-F12 branch 3/4).
//
// orphanDEKRef is the synthetic dek_ref value (caller-chosen, MUST be
// unused; e.g., 0x7FFF_FFFF_FFFF_FFF0 + offset).
func (e *adminAuthEnv) seedOrphanDEKAuditRow(
	subject string,
	ts time.Time,
	orphanDEKRef int64,
) ulid.ULID {
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()

	id := ulid.Make()
	// Use random bytes for payload — DecryptRow never reaches Decode because
	// DEK resolution fails first.
	payload := make([]byte, 64)
	_, err := rand.Read(payload)
	Expect(err).NotTo(HaveOccurred(), "seedOrphanDEKAuditRow: rand.Read")

	envProto := &eventbusv1.Event{
		Id:        id[:],
		Subject:   subject,
		Type:      "test.orphan",
		Timestamp: timestamppb.New(ts),
		Actor:     &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
		Payload:   payload,
	}
	envelopeBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(envProto)
	Expect(err).NotTo(HaveOccurred(), "seedOrphanDEKAuditRow: proto.Marshal")

	_, err = e.queryPool.Exec(ctx, `
		INSERT INTO events_audit
		  (id, subject, type, timestamp, actor_kind, actor_id,
		   envelope, schema_ver, codec, js_seq, rendering,
		   dek_ref, dek_version)
		VALUES ($1, $2, 'test.orphan', $3, 'system', NULL,
		        $4, 1, $5, $6, '{}'::jsonb,
		        $7, 1)
	`, id[:], subject, ts, envelopeBytes,
		string(codec.NameXChaCha20v1),
		int64(time.Now().UnixNano()),
		orphanDEKRef)
	Expect(err).NotTo(HaveOccurred(), "seedOrphanDEKAuditRow: INSERT events_audit")
	return id
}

// operatorReadAuditCount returns the count of crypto.system.operator_read*
// rows whose request_id matches reqID. requestID is the 26-char ULID Base32
// form (matches ReadStarted.RequestId on the wire and the payload field).
func (e *adminAuthEnv) operatorReadAuditCount(eventType, requestID string) int {
	rows, err := e.queryPool.Query(
		e.ctx,
		`SELECT envelope FROM events_audit WHERE type = $1`,
		eventType,
	)
	Expect(err).NotTo(HaveOccurred(), "operatorReadAuditCount: query")
	defer rows.Close()

	var matches int
	for rows.Next() {
		var envBytes []byte
		Expect(rows.Scan(&envBytes)).To(Succeed())
		var ev eventbusv1.Event
		if proto.Unmarshal(envBytes, &ev) != nil {
			continue
		}
		// Both start and completed payloads carry "request_id" at top level.
		var pl struct {
			RequestID string `json:"request_id"`
		}
		if json.Unmarshal(ev.GetPayload(), &pl) != nil {
			continue
		}
		if pl.RequestID == requestID {
			matches++
		}
	}
	Expect(rows.Err()).NotTo(HaveOccurred(), "operatorReadAuditCount: rows.Err")
	return matches
}

// =============================================================================
// Scenarios — invoked from admin_authenticate_e2e_test.go at the tail of the
// single Describe+It block.
// =============================================================================

// runAdminReadStreamScenarios drives the 5 F r8 happy-path scenarios against
// the already-booted server in env. Called from the existing Describe+It
// block after T25/T26/T27 (single-boot constraint from prometheus singleton).
func runAdminReadStreamScenarios(env *adminAuthEnv) {
	// Carol's session token is captured during T26 scenario 2; it survives
	// the in-memory session store (no expiry) and is reused here.
	Expect(env.carolSessionToken).NotTo(BeEmpty(),
		"F r8: env.carolSessionToken must be set by T26 scenario 2")

	By("F-E1: happy path single context — 50 encrypted events received in order, no decrypt fails")
	scenarioFE1HappyPath(env)

	By("F-E2: contexts omitted — whole-game wildcard, defaulted bounds")
	scenarioFE2WholeGameWildcard(env)

	By("F-E13: multi-context — 20 events in global timestamp order")
	scenarioFE13MultiContext(env)

	By("F-E14: sensitive-content filter — 5 encrypted received, 3 plain filtered (INV-F15)")
	scenarioFE14SensitiveFilter(env)

	By("F-E17: classifier surface — 5 metadata-only frames with STALE_DEK reason (INV-F12 producers)")
	scenarioFE17ClassifierSurface(env)
}

// ---- F-E1 ----

func scenarioFE1HappyPath(env *adminAuthEnv) {
	const eventCount = 50
	sceneID := ulid.Make().String()

	seededIDs := env.seedAdminReadStreamData("scene", []string{sceneID}, eventCount)
	Expect(seededIDs).To(HaveLen(eventCount), "F-E1 seed count")

	view := env.RunAdminReadStream(RunAdminReadStreamArgs{
		Justification: "F-E1 happy path",
		Contexts: []*adminv1.ContextRef{
			{Type: "scene", Ids: []string{sceneID}},
		},
	})

	// Server defaults Since to now-1h; seeded rows are at now-1m → inside window.
	Expect(view.started).NotTo(BeNil(), "F-E1: ReadStarted frame must arrive")
	Expect(view.finished).NotTo(BeNil(), "F-E1: ReadFinished frame must arrive")
	Expect(view.EventCount()).To(Equal(eventCount),
		"F-E1: all 50 seeded events must arrive")
	Expect(view.DecryptFailCount()).To(Equal(0),
		"F-E1: all rows must decrypt successfully")
	Expect(view.TerminatedBy()).To(Equal(adminv1.ReadFinished_TERMINATED_BY_CLIENT_EOF),
		"F-E1: clean-EOF terminator")
	Expect(view.EventsAreTimestampOrdered()).To(BeTrue(),
		"F-E1: events must arrive in ascending timestamp order")
	Expect(view.finished.GetEventsScanned()).To(Equal(int64(eventCount)),
		"F-E1: finished.events_scanned = 50")
	Expect(view.finished.GetDecryptFailCount()).To(Equal(int64(0)),
		"F-E1: finished.decrypt_fail_count = 0")

	// Audit-row assertion: exactly 1 start + 1 completed for this request_id.
	// Projection is async, so use Eventually.
	requestID := view.started.GetRequestId()
	Eventually(func() int {
		return env.operatorReadAuditCount("crypto.system.operator_read", requestID)
	}, "10s", "200ms").Should(Equal(1),
		"F-E1: exactly one crypto.system.operator_read audit row (INV-F1)")
	Eventually(func() int {
		return env.operatorReadAuditCount("crypto.system.operator_read_completed", requestID)
	}, "10s", "200ms").Should(Equal(1),
		"F-E1: exactly one crypto.system.operator_read_completed audit row (INV-F10)")
}

// ---- F-E2 ----

func scenarioFE2WholeGameWildcard(env *adminAuthEnv) {
	const eventCount = 5
	sceneID := ulid.Make().String()

	seededIDs := env.seedAdminReadStreamData("scene", []string{sceneID}, eventCount)
	Expect(seededIDs).To(HaveLen(eventCount), "F-E2 seed count")

	// Plant a cross-game contaminant: a row under a different game's subject
	// MUST NEVER appear in the result. Use a subject prefix matching no
	// pattern the F handler would generate.
	otherGameSubject := "events.othergame.scene." + ulid.Make().String() + ".test.encrypted"
	contaminantTS := time.Now().UTC().Add(-30 * time.Second)
	// Use orphan-DEK style for the contaminant — we never care about decrypt
	// success; the assertion is "MUST NOT appear in result", not "appears as
	// metadata-only".
	contaminantID := env.seedOrphanDEKAuditRow(otherGameSubject, contaminantTS, 0x7FFF_FFFF_FFFF_FFE0)

	view := env.RunAdminReadStream(RunAdminReadStreamArgs{
		Justification: "F-E2 whole-game default",
		Contexts:      nil, // whole-game wildcard
	})

	Expect(view.started).NotTo(BeNil(), "F-E2: ReadStarted must arrive")
	Expect(view.finished).NotTo(BeNil(), "F-E2: ReadFinished must arrive")
	Expect(view.TerminatedBy()).To(Equal(adminv1.ReadFinished_TERMINATED_BY_CLIENT_EOF),
		"F-E2: clean-EOF terminator")

	// Defaulted contexts MUST resolve to wildcard "events.<game>.>" — the F-E1
	// rows + this scenario's 5 rows + any prior-scenario residue all appear.
	// At minimum the 5 seeded rows MUST be present.
	Expect(view.EventCount()).To(BeNumerically(">=", eventCount),
		"F-E2: at least the 5 seeded events must arrive")

	// Cross-game invariant: the contaminant ULID MUST NOT appear.
	for _, ev := range view.events {
		Expect(ev.GetId()).NotTo(Equal(contaminantID.String()),
			"F-E2: cross-game contaminant MUST NEVER appear in whole-game read of game %q (contaminant subject %s)",
			env.gameID, otherGameSubject)
	}
}

// ---- F-E13 ----

func scenarioFE13MultiContext(env *adminAuthEnv) {
	const perContext = 10
	sceneA := ulid.Make().String()
	sceneB := ulid.Make().String()

	seededIDs := env.seedAdminReadStreamData("scene", []string{sceneA, sceneB}, perContext)
	Expect(seededIDs).To(HaveLen(2*perContext), "F-E13 seed count")

	view := env.RunAdminReadStream(RunAdminReadStreamArgs{
		Justification: "F-E13 multi-context",
		Contexts: []*adminv1.ContextRef{
			{Type: "scene", Ids: []string{sceneA}},
			{Type: "scene", Ids: []string{sceneB}},
		},
	})

	Expect(view.started).NotTo(BeNil(), "F-E13: ReadStarted must arrive")
	Expect(view.finished).NotTo(BeNil(), "F-E13: ReadFinished must arrive")
	Expect(view.TerminatedBy()).To(Equal(adminv1.ReadFinished_TERMINATED_BY_CLIENT_EOF),
		"F-E13: clean-EOF terminator")
	Expect(view.EventCount()).To(Equal(2*perContext),
		"F-E13: all 20 seeded events (10 per context) must arrive")
	Expect(view.DecryptFailCount()).To(Equal(0),
		"F-E13: all rows must decrypt successfully")

	// Global ordering: r8's R.10 emits rows in ORDER BY timestamp ASC, so the
	// merge across contexts is delegated to PostgreSQL — the test still
	// validates the contract.
	Expect(view.EventsAreTimestampOrdered()).To(BeTrue(),
		"F-E13: events from both contexts MUST be in global timestamp order")
}

// ---- F-E14 ----

func scenarioFE14SensitiveFilter(env *adminAuthEnv) {
	const encryptedCount = 5
	const plainCount = 3
	sceneID := ulid.Make().String()

	seededIDs := env.seedAdminReadStreamData("scene", []string{sceneID}, encryptedCount)
	Expect(seededIDs).To(HaveLen(encryptedCount), "F-E14 encrypted seed count")

	// Plain rows under the SAME subject pattern — these MUST be filtered by
	// R.10's `dek_ref IS NOT NULL` clause (INV-F15).
	plainSubject := env.readstreamSubjectForContext("scene", sceneID, "test.cleartext")
	plainTimestamps := make([]time.Time, plainCount)
	plainIDs := make([]ulid.ULID, plainCount)
	baseTime := time.Now().UTC().Add(-30 * time.Second)
	for i := 0; i < plainCount; i++ {
		plainTimestamps[i] = baseTime.Add(time.Duration(i) * time.Millisecond)
		plainIDs[i] = env.seedPlainAuditRow(plainSubject, plainTimestamps[i])
	}

	view := env.RunAdminReadStream(RunAdminReadStreamArgs{
		Justification: "F-E14 sensitive filter",
		Contexts: []*adminv1.ContextRef{
			{Type: "scene", Ids: []string{sceneID}},
		},
	})

	Expect(view.started).NotTo(BeNil(), "F-E14: ReadStarted must arrive")
	Expect(view.finished).NotTo(BeNil(), "F-E14: ReadFinished must arrive")
	Expect(view.TerminatedBy()).To(Equal(adminv1.ReadFinished_TERMINATED_BY_CLIENT_EOF),
		"F-E14: clean-EOF terminator")
	Expect(view.EventCount()).To(Equal(encryptedCount),
		"F-E14: exactly 5 encrypted events (plain rows MUST be filtered, INV-F15)")
	Expect(view.DecryptFailCount()).To(Equal(0),
		"F-E14: no decrypt failures expected")
	Expect(view.finished.GetEventsScanned()).To(Equal(int64(encryptedCount)),
		"F-E14: events_scanned excludes the filtered plain rows")

	// Defense-in-depth: none of the plain IDs may appear.
	plainIDSet := make(map[string]struct{}, plainCount)
	for _, p := range plainIDs {
		plainIDSet[p.String()] = struct{}{}
	}
	for _, ev := range view.events {
		_, leaked := plainIDSet[ev.GetId()]
		Expect(leaked).To(BeFalse(),
			"F-E14: plain (identity-codec) row %s MUST NEVER appear (INV-F15)", ev.GetId())
	}
}

// ---- F-E17 ----

func scenarioFE17ClassifierSurface(env *adminAuthEnv) {
	const orphanCount = 5
	sceneID := ulid.Make().String()
	subject := env.readstreamSubjectForContext("scene", sceneID, "test.orphan")

	// Seed 5 rows whose dek_ref points to nonexistent crypto_keys IDs.
	// dek.Manager.Resolve returns DEK_NOT_FOUND for each — classifyDecryptErr
	// maps that to STALE_DEK (INV-F12 branch 3/4). The brief's originally-
	// planned DEK_MISSING / DEK_BAD_COLUMNS shape is unreachable in r8
	// (see file header for rationale).
	baseTime := time.Now().UTC().Add(-45 * time.Second)
	orphanIDs := make([]ulid.ULID, orphanCount)
	for i := 0; i < orphanCount; i++ {
		ts := baseTime.Add(time.Duration(i) * time.Millisecond)
		// Use very large dek_ref values that won't collide with real
		// crypto_keys.id (BIGSERIAL starts at 1).
		orphanDEKRef := int64(0x7FFF_FFFF_FFFF_FF00) + int64(i) //nolint:mnd // unique high-range sentinel per row
		orphanIDs[i] = env.seedOrphanDEKAuditRow(subject, ts, orphanDEKRef)
	}

	view := env.RunAdminReadStream(RunAdminReadStreamArgs{
		Justification: "F-E17 classifier surface",
		Contexts: []*adminv1.ContextRef{
			{Type: "scene", Ids: []string{sceneID}},
		},
	})

	Expect(view.started).NotTo(BeNil(), "F-E17: ReadStarted must arrive")
	Expect(view.finished).NotTo(BeNil(), "F-E17: ReadFinished must arrive")
	Expect(view.TerminatedBy()).To(Equal(adminv1.ReadFinished_TERMINATED_BY_CLIENT_EOF),
		"F-E17: clean-EOF terminator (row-level errors are NOT fatal)")

	Expect(view.EventCount()).To(Equal(orphanCount),
		"F-E17: all 5 orphan rows must arrive as frames")
	Expect(view.DecryptFailCount()).To(Equal(orphanCount),
		"F-E17: all 5 must be metadata-only (decrypt failed at DEK-resolve)")
	Expect(view.finished.GetDecryptFailCount()).To(Equal(int64(orphanCount)),
		"F-E17: finished.decrypt_fail_count = 5")

	// Plaintext MUST NEVER leak — metadata-only payloads are empty.
	Expect(view.PayloadsAllEmpty()).To(BeTrue(),
		"F-E17: metadata-only frames MUST carry empty Payload (no plaintext leak, INV-F12)")

	// Classifier verdict: every row's reason is STALE_DEK in r8.
	// (See file header note — DEK_MISSING / DEK_BAD_COLUMNS branches are
	// declared in the proto but no production codepath produces them today.)
	reasons := view.MetadataOnlyReasons()
	Expect(reasons).To(HaveLen(orphanCount),
		"F-E17: one reason per metadata-only frame")
	for i, r := range reasons {
		Expect(r).To(Equal(corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_STALE_DEK),
			"F-E17: row %d must classify as STALE_DEK (orphan dek_ref → DEK_NOT_FOUND → STALE_DEK per INV-F12)", i)
	}

	// The dek_ref classifier branches DEK_MISSING and DEK_BAD_COLUMNS are
	// not exercisable in r8 production code today:
	//   - DEK_MISSING is only emitted when the oops code is
	//     "ADMIN_READSTREAM_COLD_NO_DEK", which is referenced solely from
	//     classifyDecryptErr and never raised by callers (R.10's cold reader
	//     filters NULL dek_ref at SQL level and rejects negative dek_ref at
	//     scan time, returning an error rather than a row).
	//   - DEK_BAD_COLUMNS is enum-declared (proto + Go) but classifyDecryptErr
	//     never returns it.
	// R.18 / R.19 SHOULD revisit if the classifier surface gains producers.
	_ = orphanIDs
}
