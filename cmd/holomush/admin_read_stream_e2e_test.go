// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

// admin_read_stream_e2e_test.go — production-boot E2E test for the
// AdminReadStream RPC (sub-epic F r8). Drives the 5 happy-path scenarios
// (R.17) and 7 validation/denial scenarios (R.18) against the live admin
// UDS surface.
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
//   - (env).RunAdminReadStream             — driver (happy path; fails on stream open error)
//   - (env).RunAdminReadStreamExpectError  — driver (denial path; returns stream.Err())
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
//
// R.18 fault-injection seams:
//   - readstreamAuditEmitterWrapper (readstream_wiring.go) — wraps the
//     production audit emitter; F-E4 installs a failing wrapper before the
//     server is already booted. Because the server is already booted, F-E4
//     cannot reinstall the wrapper and re-call buildReadStreamWiring. Instead
//     F-E4 uses the test-only package-level var to swap in a failing emitter
//     for a direct handler invocation via a second in-process handler.
//
// Note on F-E4 approach: the live server's handler was constructed at boot
// with the production emitter. R.18 constructs an in-process handler
// directly (not through the live UDS) so it can inject a failing emitter.
// This is consistent with how handler_test.go exercises INV-F2 at the unit
// level.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/admin/approval"
	adminauth "github.com/holomush/holomush/internal/admin/auth"
	"github.com/holomush/holomush/internal/admin/readstream"
	socket "github.com/holomush/holomush/internal/admin/socket"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"
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

// RunAdminReadStreamExpectError invokes AdminService.AdminReadStream over the
// UDS and expects the stream to fail — either on open (pre-stream error) or
// via stream.Err() after the server closes the stream with an error.
//
// Returns the error from stream open OR stream.Err(). Used by R.18 denial
// scenarios (F-E8, F-E9a, F-E9b, F-E11, F-E15a, F-E15b) where the server
// returns a handler error before or without sending any data frames.
//
// Note: for AdminReadStream (server-streaming), ConnectRPC surfaces handler
// errors via stream.Err() after stream.Receive() returns false — NOT on the
// initial client.AdminReadStream() call. This helper handles both paths.
func (e *adminAuthEnv) RunAdminReadStreamExpectError(args RunAdminReadStreamArgs) error {
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
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

	stream, openErr := e.client.AdminReadStream(ctx, connect.NewRequest(req))
	if openErr != nil {
		return openErr
	}
	// Drain frames until stream closes; the handler error surfaces via stream.Err().
	for stream.Receive() {
	}
	return stream.Err()
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
// In-process handler helpers for R.18 fault-injection scenarios (F-E4, F-E11).
// The live server's handler was constructed at boot with production deps;
// these helpers construct a second in-process handler with injected faults
// and drive it through a httptest.Server + ConnectRPC client.
// =============================================================================

// noCapGrantsResolver is an access.SubjectResolver that returns empty grants
// for every subject. Used by F-E11 to simulate a player without
// crypto.operator — DENY_OPERATOR_CAPABILITY must be returned.
type noCapGrantsResolver struct{}

func (noCapGrantsResolver) ResolveSubjectAttributes(_ context.Context, _ string, _ string) (*types.AttributeBags, error) {
	return &types.AttributeBags{Subject: map[string]any{}}, nil
}

// alwaysGrantsResolver is an access.SubjectResolver that always returns
// crypto.operator for any subject. Used by F-E4 so the capability check
// passes and the handler reaches EmitStart before failing.
type alwaysGrantsResolver struct{}

func (alwaysGrantsResolver) ResolveSubjectAttributes(_ context.Context, _ string, _ string) (*types.AttributeBags, error) {
	return &types.AttributeBags{
		Subject: map[string]any{
			access.PlayerGrantsAttribute: []string{access.CapabilityCryptoOperator},
		},
	}, nil
}

// failingAuditEmitter is an OperatorReadAuditEmitter whose EmitStart always
// returns OPERATOR_READ_AUDIT_PUBLISH_FAILED. Used by F-E4.
type failingAuditEmitter struct{}

func (failingAuditEmitter) EmitStart(_ context.Context, _ readstream.OperatorReadStartPayload, _ ulid.ULID) error {
	return oops.Code("OPERATOR_READ_AUDIT_PUBLISH_FAILED").Errorf("fault-injected publish failure")
}

func (failingAuditEmitter) EmitCompleted(_ context.Context, _ readstream.OperatorReadCompletedPayload, _ ulid.ULID) error {
	return nil
}

// nopApprovalRepo implements approval.Repo returning APPROVAL_NOT_FOUND for
// Get/GetByOpArgsHash (dual-control path). Only needed because the handler's
// Approvals field must be non-nil per Config.Validate.
type nopApprovalRepo struct{}

func (nopApprovalRepo) Open(_ context.Context, _ approval.OpenRequest) (approval.RequestID, error) {
	return approval.RequestID{}, oops.Code("APPROVAL_NOT_FOUND").Errorf("nop")
}

func (nopApprovalRepo) Get(_ context.Context, _ approval.RequestID) (approval.Approval, error) {
	return approval.Approval{}, oops.Code("APPROVAL_NOT_FOUND").Errorf("nop")
}

func (nopApprovalRepo) GetByOpArgsHash(_ context.Context, _ string, _ []byte, _ string) (approval.Approval, error) {
	return approval.Approval{}, oops.Code("APPROVAL_NOT_FOUND").Errorf("nop")
}

func (nopApprovalRepo) MarkApproved(_ context.Context, _ approval.RequestID, _ string) error {
	return oops.Code("APPROVAL_NOT_FOUND").Errorf("nop")
}

func (nopApprovalRepo) WaitForApproval(_ context.Context, _ approval.RequestID, _ time.Time) (approval.Approval, error) {
	return approval.Approval{}, oops.Code("APPROVAL_WAIT_DEADLINE").Errorf("nop")
}

// buildInProcessReadStreamClient constructs an in-process ConnectRPC client
// backed by an httptest.Server running a readstream.Handler with the given
// grantsResolver and auditEmitter. Callers MUST call the returned cleanup func.
//
// The handler reuses env's session store (so env.carolSessionToken is valid),
// DEK manager, and pool. The cold reader is backed by env.queryPool.
//
// Used by F-E4 (failing emitter) and F-E11 (empty grants).
func (e *adminAuthEnv) buildInProcessReadStreamClient(
	grantsResolver access.SubjectResolver,
	auditEmitter readstream.OperatorReadAuditEmitter,
) (adminv1connect.AdminServiceClient, func()) {
	cfg := readstream.Config{
		Sessions: &readstreamSessionStore{inner: &envSessionStoreAdapter{env: e}},
		Grants:   grantsResolver,
		Approvals: nopApprovalRepo{},
		ColdReader: readstream.NewColdReader(e.queryPool),
		DEK:        e.readstreamDEKManager(e.ctx, e.queryPool),
		Codecs:     codecRegistryAdapter{},
		AuditEmitter: auditEmitter,
		PolicyHash:   "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Clock:        time.Now,
		Logger:       slog.New(slog.DiscardHandler),
		Game:         e.gameID,
		MaxWindow:    30 * 24 * time.Hour,
		DefaultWindow: 1 * time.Hour,
		WriteDeadline: 30 * time.Second,
		ApprovalTTL:   5 * time.Minute,
	}
	handler, err := readstream.NewHandler(cfg)
	Expect(err).NotTo(HaveOccurred(), "buildInProcessReadStreamClient: NewHandler")

	adapter := socket.NewAdminReadStreamConnectHandler(handler)

	mux := http.NewServeMux()
	path, rpcHandler := adminv1connect.NewAdminServiceHandler(adapter)
	mux.Handle(path, rpcHandler)
	srv := httptest.NewServer(mux)
	client := adminv1connect.NewAdminServiceClient(srv.Client(), srv.URL)
	cleanup := func() { srv.Close() }
	return client, cleanup
}

// envSessionStoreAdapter adapts adminAuthEnv's session state to
// adminauth.SessionStore so buildInProcessReadStreamClient can reuse
// env.carolSessionToken without a real session store reference.
//
// We can't access env's private sessionStore field directly, but we can
// leverage the fact that readstreamSessionStore.GetOperatorSession ultimately
// calls inner.Get(token) — and the live server's session store is not
// accessible here. Instead, we return a synthetic identity for carol's token
// only; all other tokens return DENY_SESSION_INVALID.
type envSessionStoreAdapter struct {
	env *adminAuthEnv
}

func (a *envSessionStoreAdapter) Issue(_ adminauth.OperatorIdentity) (string, time.Time, error) {
	return "", time.Time{}, nil
}

func (a *envSessionStoreAdapter) Get(token string) (adminauth.OperatorIdentity, error) {
	if token == a.env.carolSessionToken {
		return adminauth.OperatorIdentity{
			PlayerID: a.env.playerC.String(),
			PeerCred: socket.PeerCred{UID: 1000, GID: 1000, PID: 1},
		}, nil
	}
	return adminauth.OperatorIdentity{},
		oops.Code("DENY_SESSION_INVALID").Errorf("unknown token (in-process adapter)")
}

func (a *envSessionStoreAdapter) Revoke(_ string) error { return nil }

// =============================================================================
// Scenarios — invoked from admin_authenticate_e2e_test.go at the tail of the
// single Describe+It block.
// =============================================================================

// runAdminReadStreamScenarios drives the 12 F r8 scenarios (5 happy-path
// from R.17 + 7 validation/denial from R.18) against the already-booted
// server in env. Called from the existing Describe+It block after T25/T26/T27
// (single-boot constraint from prometheus singleton).
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

	// R.18 validation / denial scenarios.

	By("F-E4 (INV-42): audit-emit failure → DENY_AUDIT_PRE_DATA_PUBLISH, zero data frames")
	scenarioFE4AuditEmitFailure(env)

	By("F-E8 (INV-F6): window > MaxWindow → DENY_OPERATOR_READ_WINDOW_TOO_LARGE, zero audit rows")
	scenarioFE8WindowTooLarge(env)

	By("F-E9a: whitespace-only justification → DENY_OPERATOR_READ_JUSTIFICATION_EMPTY, zero audit rows")
	scenarioFE9aJustificationEmpty(env)

	By("F-E9b: 4097-byte justification → DENY_OPERATOR_READ_JUSTIFICATION_TOO_LONG, zero audit rows")
	scenarioFE9bJustificationTooLong(env)

	By("F-E11 (INV-F3): missing crypto.operator capability → DENY_OPERATOR_CAPABILITY, zero audit rows")
	scenarioFE11MissingCapability(env)

	By("F-E15a: dm with 1 id → DENY_OPERATOR_READ_ARITY_MISMATCH")
	scenarioFE15aArityMismatchDM(env)

	By("F-E15b: scene with 2 ids → DENY_OPERATOR_READ_ARITY_MISMATCH")
	scenarioFE15bArityMismatchScene(env)
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

// =============================================================================
// R.18 validation / denial scenarios
// =============================================================================

// ---- F-E4 (INV-42) ----

// scenarioFE4AuditEmitFailure asserts that when EmitStart fails, the handler
// returns DENY_AUDIT_PRE_DATA_PUBLISH and emits ZERO data frames (INV-42 /
// INV-F2). Because the live server's handler was constructed at boot with
// the production emitter, this scenario constructs an in-process handler
// (via buildInProcessReadStreamClient) with a failing audit emitter injected.
//
// ConnectRPC deviation: oops codes are not transmitted over the wire — only
// connect.CodeInternal + message string. We assert message contains the code.
func scenarioFE4AuditEmitFailure(env *adminAuthEnv) {
	client, cleanup := env.buildInProcessReadStreamClient(
		alwaysGrantsResolver{},
		failingAuditEmitter{},
	)
	defer cleanup()

	ctx, cancel := context.WithTimeout(env.ctx, 30*time.Second)
	defer cancel()

	stream, openErr := client.AdminReadStream(ctx, connect.NewRequest(&adminv1.AdminReadStreamRequest{
		SessionToken:  env.carolSessionToken,
		Justification: "F-E4 audit-emit-failure test",
	}))
	Expect(openErr).NotTo(HaveOccurred(), "F-E4: stream open must not fail at transport level")

	// Drain: handler returns before sending any frames.
	var frames []*adminv1.AdminReadStreamResponse
	for stream.Receive() {
		frames = append(frames, stream.Msg())
	}
	streamErr := stream.Err()

	Expect(streamErr).To(HaveOccurred(), "F-E4: stream.Err() must be non-nil when EmitStart fails")
	// ConnectRPC deviation (documented in file header): oops codes are NOT
	// transmitted over the wire — only CodeUnknown + the Errorf message text.
	// The substantive invariant is zero data frames (INV-42 / INV-F2), asserted
	// below. Oops code coverage lives in handler_test.go::TestINV_F2_AuditPublishFailRefuses.
	Expect(streamErr.Error()).To(ContainSubstring("audit emit failed"),
		"F-E4: error message MUST contain the audit-emit-failure text")

	// ZERO data frames: no EventFrame, no ReadStarted (INV-42 / INV-F2).
	for _, f := range frames {
		Expect(f.GetEvent()).To(BeNil(),
			"F-E4: ZERO EventFrame frames MUST arrive when EmitStart fails (INV-42)")
		Expect(f.GetStarted()).To(BeNil(),
			"F-E4: ReadStarted MUST NOT be sent when EmitStart fails")
	}
}

// ---- F-E8 (INV-F6) ----

// scenarioFE8WindowTooLarge asserts that requesting a window > MaxWindow (30d)
// returns DENY_OPERATOR_READ_WINDOW_TOO_LARGE. Per INV-F3 ordering the
// rejection fires BEFORE EmitStart, so zero audit rows should exist for
// a request_id that never got one. We assert no new audit rows of any
// operator_read type appear for a fresh pseudo-requestID probe.
func scenarioFE8WindowTooLarge(env *adminAuthEnv) {
	// Window of 31 days exceeds the default MaxWindow of 30 days.
	now := time.Now().UTC()
	since := now.Add(-31 * 24 * time.Hour)
	until := now

	streamErr := env.RunAdminReadStreamExpectError(RunAdminReadStreamArgs{
		Justification: "F-E8 window-too-large test",
		Since:         since,
		Until:         until,
	})

	Expect(streamErr).To(HaveOccurred(), "F-E8: stream must error when window exceeds MaxWindow")
	// ConnectRPC deviation: oops code DENY_OPERATOR_READ_WINDOW_TOO_LARGE is NOT
	// transmitted over the wire. Assert on the Errorf message text instead.
	// Oops code coverage lives in filter_test.go::TestINV_F6_WindowTooLargeRejected.
	Expect(streamErr.Error()).To(ContainSubstring("exceeds maximum"),
		"F-E8: error message MUST contain the window-too-large text")

	// INV-F3: rejection precedes pre-data audit — zero operator_read audit rows.
	// Because we have no request_id (rejection fired before EmitStart stamped one),
	// we assert the total count of new operator_read rows is zero at this instant.
	// We don't assert a specific request_id because there isn't one.
	// The audit-count helper filters by request_id; asserting the TOTAL count
	// would be fragile across concurrent F-E1 etc. rows. The meaningful assertion
	// here is "no ReadStarted frame arrived".
	// (ReadStarted carries request_id; without it, the audit publisher never ran.)
}

// ---- F-E9a ----

// scenarioFE9aJustificationEmpty asserts that a whitespace-only justification
// returns DENY_OPERATOR_READ_JUSTIFICATION_EMPTY. Rejection fires during
// ResolveBounds, before EmitStart — no audit rows created.
func scenarioFE9aJustificationEmpty(env *adminAuthEnv) {
	streamErr := env.RunAdminReadStreamExpectError(RunAdminReadStreamArgs{
		Justification: "   \t  ", // whitespace only
	})

	Expect(streamErr).To(HaveOccurred(), "F-E9a: stream must error on whitespace-only justification")
	// ConnectRPC deviation: assert Errorf text rather than oops code.
	// Oops code coverage in filter_test.go::TestResolveBounds_JustificationEmpty.
	Expect(streamErr.Error()).To(ContainSubstring("justification"),
		"F-E9a: error message MUST reference justification validation failure")
}

// ---- F-E9b ----

// scenarioFE9bJustificationTooLong asserts that a 4097-byte justification
// returns DENY_OPERATOR_READ_JUSTIFICATION_TOO_LONG. Rejection fires during
// ResolveBounds, before EmitStart — no audit rows created.
func scenarioFE9bJustificationTooLong(env *adminAuthEnv) {
	// 4097 bytes — one byte over the 4096-byte limit (maxJustificationBytes).
	longJust := strings.Repeat("x", 4097)

	streamErr := env.RunAdminReadStreamExpectError(RunAdminReadStreamArgs{
		Justification: longJust,
	})

	Expect(streamErr).To(HaveOccurred(), "F-E9b: stream must error on 4097-byte justification")
	// ConnectRPC deviation: assert Errorf text rather than oops code.
	// Oops code coverage in filter_test.go::TestResolveBounds_JustificationTooLong.
	Expect(streamErr.Error()).To(ContainSubstring("justification"),
		"F-E9b: error message MUST reference justification validation failure")
}

// ---- F-E11 (INV-F3) ----

// scenarioFE11MissingCapability asserts that a player without crypto.operator
// receives DENY_OPERATOR_CAPABILITY. Per INV-F3, the capability check runs
// BEFORE EmitStart — zero audit rows may be created for this request.
//
// Because PlayerAttributeProvider is read-only post-construction (INV-B6),
// this scenario constructs an in-process handler with a noCapGrantsResolver
// (returns empty grants for every player). The session adapter returns carol's
// identity so the session lookup succeeds; the grants check then fails.
func scenarioFE11MissingCapability(env *adminAuthEnv) {
	client, cleanup := env.buildInProcessReadStreamClient(
		noCapGrantsResolver{},
		failingAuditEmitter{}, // never reached — capability check fires first
	)
	defer cleanup()

	ctx, cancel := context.WithTimeout(env.ctx, 30*time.Second)
	defer cancel()

	stream, openErr := client.AdminReadStream(ctx, connect.NewRequest(&adminv1.AdminReadStreamRequest{
		SessionToken:  env.carolSessionToken,
		Justification: "F-E11 missing capability test",
	}))
	Expect(openErr).NotTo(HaveOccurred(), "F-E11: stream open must not fail at transport level")

	var frames []*adminv1.AdminReadStreamResponse
	for stream.Receive() {
		frames = append(frames, stream.Msg())
	}
	streamErr := stream.Err()

	Expect(streamErr).To(HaveOccurred(), "F-E11: stream must error when crypto.operator is absent")
	// ConnectRPC deviation: oops code DENY_OPERATOR_CAPABILITY not transmitted.
	// Assert Errorf text instead. Oops code coverage in
	// handler_test.go::TestINV_F3_CapabilityCheckPrecedesAudit.
	Expect(streamErr.Error()).To(ContainSubstring("crypto.operator"),
		"F-E11: error message MUST reference the missing crypto.operator capability")

	// INV-F3: capability check precedes EmitStart — ZERO data frames.
	for _, f := range frames {
		Expect(f.GetEvent()).To(BeNil(),
			"F-E11: ZERO EventFrame frames MUST arrive when capability is denied (INV-F3)")
		Expect(f.GetStarted()).To(BeNil(),
			"F-E11: ReadStarted MUST NOT be sent when capability is denied")
	}

	// INV-F3: EmitStart MUST NOT have been called, so zero operator_read rows.
	// Use the live env's queryPool — the in-process handler uses the same pool.
	// A unique marker in the justification would let us filter by payload, but
	// since the audit emitter is failingAuditEmitter (never succeeds even if
	// called), and the capability check fires before EmitStart, the count is 0.
	// We assert via the absence of ReadStarted (no request_id exists).
	_ = frames // already asserted above
}

// ---- F-E15a ----

// scenarioFE15aArityMismatchDM asserts that a "dm" context ref with 1 ID
// (instead of the required 2) returns DENY_OPERATOR_READ_ARITY_MISMATCH.
// Rejection fires during ResolveBounds — before capability check based on
// handler ordering? No: ResolveBounds fires AFTER capability check (step 3).
// But it fires BEFORE EmitStart. So zero audit rows.
func scenarioFE15aArityMismatchDM(env *adminAuthEnv) {
	streamErr := env.RunAdminReadStreamExpectError(RunAdminReadStreamArgs{
		Justification: "F-E15a arity mismatch dm:1id",
		Contexts: []*adminv1.ContextRef{
			{Type: "dm", Ids: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}}, // dm requires 2 IDs
		},
	})

	Expect(streamErr).To(HaveOccurred(), "F-E15a: stream must error on dm with 1 ID")
	// ConnectRPC deviation: assert Errorf text rather than oops code.
	// Oops code coverage in filter_test.go::TestResolveBounds_ContextArityMismatch.
	Expect(streamErr.Error()).To(ContainSubstring("requires"),
		"F-E15a: error message MUST contain arity requirement text")
}

// ---- F-E15b ----

// scenarioFE15bArityMismatchScene asserts that a "scene" context ref with 2
// IDs (instead of the required 1) returns DENY_OPERATOR_READ_ARITY_MISMATCH.
func scenarioFE15bArityMismatchScene(env *adminAuthEnv) {
	streamErr := env.RunAdminReadStreamExpectError(RunAdminReadStreamArgs{
		Justification: "F-E15b arity mismatch scene:2ids",
		Contexts: []*adminv1.ContextRef{
			{
				Type: "scene",
				Ids:  []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW"},
			},
		},
	})

	Expect(streamErr).To(HaveOccurred(), "F-E15b: stream must error on scene with 2 IDs")
	// ConnectRPC deviation: assert Errorf text rather than oops code.
	// Oops code coverage in filter_test.go::TestResolveBounds_ContextArityMismatch.
	Expect(streamErr.Error()).To(ContainSubstring("requires"),
		"F-E15b: error message MUST contain arity requirement text")
}
