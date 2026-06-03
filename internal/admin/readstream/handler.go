// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// handler.go is the AdminReadStream entry point. It orchestrates the full
// operator-read flow per ADR-0017:
//
//  1. Capability check (INV-F3): the bearer must hold crypto.operator BEFORE
//     any audit publish or data read.
//  2. ResolveBounds (INV-F6/F7): validate + default + canonicalise the request.
//  3. Dual-control (INV-F11/F17): on req.DualControl, reuse a fresh approved
//     row from approval.Repo.GetByOpArgsHash or Open + WaitForApproval.
//  4. Pre-data audit publish (INV-F1/F2): EmitStart MUST succeed BEFORE the
//     first frame is sent or any cold-tier row is read.
//  5. scanAndStream: ColdReader.Read → DecryptRow per row → typed frame send
//     via SendWithDeadline (INV-F12/F14).
//  6. Post-data audit publish (INV-F10): EmitCompleted is best-effort; its
//     failure increments the metric but does NOT raise.
//
// The production handler is wired in R.14. R.15 supplies the in-process
// httptest adapter for E2E tests. This file's only public surface is
// NewHandler + HandleAdminReadStream + the Config struct.

package readstream

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/admin/approval"
	"github.com/holomush/holomush/internal/admin/socket"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/idgen"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// OperatorSession is the narrow identity shape the readstream handler needs
// from an authenticated operator session. Mirrors the layering-decoupling
// pattern in internal/admin/socket/rekey_handler.go::OperatorSession but
// preserves PeerCred (UID/PID) because F's audit payload requires it
// (INV-F7).
type OperatorSession struct {
	PlayerID       string
	SessionTokenID string
	PeerCredUID    uint32
	PeerCredPID    int32
}

// SessionStore is the narrow seam the handler uses to resolve a session
// token to an OperatorSession. The production adapter wraps
// adminauth.SessionStore + the middleware-captured peer creds, mapping
// adminauth.OperatorIdentity → OperatorSession at the wiring boundary
// (R.14). Decoupled to avoid the adminauth → socket → readstream import
// cycle.
type SessionStore interface {
	GetOperatorSession(token string) (OperatorSession, error)
}

// ColdRowReader is the narrow seam the handler consumes for cold-tier reads.
// *ColdReader (the production implementation, backed by pgxpool) satisfies
// this implicitly; tests substitute a stub that returns canned rows without
// requiring a real database. Defined here (not in cold_reader.go) so the
// handler's seam is colocated with its consumer.
type ColdRowReader interface {
	Read(ctx context.Context, q ColdQuery) ([]ColdRow, error)
}

// Config bundles all dependencies for a Handler. All fields except
// Logger MUST be set; Validate enforces this at construction time.
type Config struct {
	// Sessions resolves the session_token to an OperatorSession.
	Sessions SessionStore
	// Grants is the ABAC resolver for the crypto.operator capability check
	// (INV-F3). Passed to access.HasPlayerGrant.
	Grants access.SubjectResolver
	// Approvals is the dual-control repository (INV-F11/F17). When
	// req.DualControl=false, this is never called.
	Approvals approval.Repo
	// ColdReader executes the cold-tier read against events_audit. Owned
	// directly by F per ADR-0017 (no HistoryReader/dispatcher). Production
	// passes *ColdReader; tests pass a stub.
	ColdReader ColdRowReader
	// DEK resolves the DEK for per-row decryption (R.11).
	DEK DEKResolver
	// Codecs resolves the codec for per-row decryption (R.11).
	Codecs CodecResolver
	// AuditEmitter publishes the start + completed audit events (INV-F1/F2/F10).
	AuditEmitter OperatorReadAuditEmitter
	// PolicyHash is the canonical "sha256:<hex>" of the active site
	// policy, captured at bootstrap. Stamped into both audit payloads.
	PolicyHash string
	// Clock returns the current time. Tests inject a fixed clock for
	// deterministic timestamps; production passes time.Now.
	Clock func() time.Time
	// Logger is optional. nil is replaced with slog.Default().
	Logger *slog.Logger
	// Game is the game ID used to build NATS subjects (BuildSubjects).
	Game string
	// MaxWindow caps the (until - since) span (INV-F6).
	MaxWindow time.Duration
	// DefaultWindow is the size of since-defaulted reads (INV-F6).
	DefaultWindow time.Duration
	// WriteDeadline is the per-frame send deadline (INV-F14).
	WriteDeadline time.Duration
	// ApprovalTTL is the dual-control wait budget. Used as the deadline
	// passed to approval.Repo.WaitForApproval.
	ApprovalTTL time.Duration
}

// Validate reports the first missing-required-field problem.
func (c Config) Validate() error {
	switch {
	case c.Sessions == nil:
		return oops.Code("READSTREAM_CONFIG_INVALID").Errorf("Sessions is required")
	case c.Grants == nil:
		return oops.Code("READSTREAM_CONFIG_INVALID").Errorf("Grants is required")
	case c.Approvals == nil:
		return oops.Code("READSTREAM_CONFIG_INVALID").Errorf("Approvals is required")
	case c.ColdReader == nil:
		return oops.Code("READSTREAM_CONFIG_INVALID").Errorf("ColdReader is required")
	case c.DEK == nil:
		return oops.Code("READSTREAM_CONFIG_INVALID").Errorf("DEK is required")
	case c.Codecs == nil:
		return oops.Code("READSTREAM_CONFIG_INVALID").Errorf("Codecs is required")
	case c.AuditEmitter == nil:
		return oops.Code("READSTREAM_CONFIG_INVALID").Errorf("AuditEmitter is required")
	case c.PolicyHash == "":
		return oops.Code("READSTREAM_CONFIG_INVALID").Errorf("PolicyHash is required")
	case c.Clock == nil:
		return oops.Code("READSTREAM_CONFIG_INVALID").Errorf("Clock is required")
	case c.Game == "":
		return oops.Code("READSTREAM_CONFIG_INVALID").Errorf("Game is required")
	case c.MaxWindow <= 0:
		return oops.Code("READSTREAM_CONFIG_INVALID").Errorf("MaxWindow must be positive")
	case c.DefaultWindow <= 0:
		return oops.Code("READSTREAM_CONFIG_INVALID").Errorf("DefaultWindow must be positive")
	case c.DefaultWindow > c.MaxWindow:
		return oops.Code("READSTREAM_CONFIG_INVALID").Errorf("DefaultWindow must be <= MaxWindow")
	case c.WriteDeadline <= 0:
		return oops.Code("READSTREAM_CONFIG_INVALID").Errorf("WriteDeadline must be positive")
	case c.ApprovalTTL <= 0:
		return oops.Code("READSTREAM_CONFIG_INVALID").Errorf("ApprovalTTL must be positive")
	}
	return nil
}

// Handler is the AdminReadStream handler. Methods are safe for concurrent use.
type Handler struct {
	cfg Config
}

// NewHandler constructs a Handler from validated config. Returns an error
// when any required field is unset.
func NewHandler(cfg Config) (*Handler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Handler{cfg: cfg}, nil
}

// streamSender is the minimal frame-sending surface handleInternal uses.
// *connect.ServerStream[adminv1.AdminReadStreamResponse] satisfies this at
// the production call site; tests substitute a recording fake.
type streamSender interface {
	Send(*adminv1.AdminReadStreamResponse) error
}

// HandleAdminReadStream is the socket.ReadStreamRPCHandler implementation
// invoked by the ConnectRPC server. It adapts the connect.ServerStream to
// the package-private streamSender surface and delegates to handleInternal.
func (h *Handler) HandleAdminReadStream(
	ctx context.Context,
	req *connect.Request[adminv1.AdminReadStreamRequest],
	stream *connect.ServerStream[adminv1.AdminReadStreamResponse],
) error {
	return h.handleInternal(ctx, req.Msg, &connectStream{stream: stream})
}

// connectStream adapts the ConnectRPC server stream to streamSender.
type connectStream struct {
	stream *connect.ServerStream[adminv1.AdminReadStreamResponse]
}

func (c *connectStream) Send(resp *adminv1.AdminReadStreamResponse) error {
	return c.stream.Send(resp) //nolint:wrapcheck // transport-layer error passes through verbatim; wrapping hides the semantic
}

// handleInternal runs the full operator-read flow per ADR-0017. The
// invariant ordering enforced here is INV-CRYPTO-23 / INV-F1 / INV-F2:
//
//	capability check → resolve bounds → (optional) dual-control →
//	EmitStart → first frame send → cold read → frame stream →
//	final frame → EmitCompleted (best-effort)
//
// Returning early before EmitStart guarantees ZERO data leaks downstream;
// returning early after EmitStart still emits the final ReadFinished frame
// when possible so the operator sees an explanation.
func (h *Handler) handleInternal(
	ctx context.Context,
	req *adminv1.AdminReadStreamRequest,
	stream streamSender,
) error {
	// Step 1: resolve the session → operator identity.
	session, err := h.cfg.Sessions.GetOperatorSession(req.GetSessionToken())
	if err != nil {
		return oops.Wrap(err)
	}

	// Step 2: capability check (INV-F3 — MUST precede EmitStart).
	hasCap, err := access.HasPlayerGrant(ctx, h.cfg.Grants, session.PlayerID, access.CapabilityCryptoOperator)
	if err != nil {
		return oops.Code("INGAME_GRANT_LOOKUP_FAILED").
			With("player_id", session.PlayerID).Wrap(err)
	}
	if !hasCap {
		return oops.Code("DENY_OPERATOR_CAPABILITY").
			With("player_id", session.PlayerID).
			Errorf("crypto.operator capability absent")
	}

	// Step 3: validate + canonicalise the request shape (INV-F6/F7).
	domesticReq := protoToDomesticRequest(req)
	resolved, flags, err := ResolveBounds(&domesticReq, h.cfg.Clock(), h.cfg.DefaultWindow, h.cfg.MaxWindow)
	if err != nil {
		return oops.Wrap(err)
	}

	// Step 4: compute op-args hash from the RESOLVED bounds so that two
	// requests that omit since/until hash identically only when they resolve
	// to the same effective window — preventing approval reuse across
	// different actual time ranges.
	opArgsHash, err := computeReadStreamArgsHash(resolved)
	if err != nil {
		return oops.Wrap(err)
	}

	// Step 5: dual-control (INV-F11/F17). On reuse, approvalInfo carries the
	// existing row; on Open + Wait, it carries the freshly approved row.
	var approvalInfo *approvalRecord
	if req.GetDualControl() {
		approvalInfo, err = h.acquireApproval(ctx, session, opArgsHash, stream)
		if err != nil {
			// On dual-control timeout, emit ReadFinished{DUAL_CONTROL_TIMEOUT}
			// best-effort so the operator sees an explicit reason for the
			// stream close. The audit start MUST NOT be published — no audit
			// row is required for a never-started read.
			if isOopsCode(err, "READSTREAM_DUAL_CONTROL_TIMEOUT") {
				_ = stream.Send(buildFinishedFrame( //nolint:errcheck // best-effort terminator send on already-failing handler path; transport errors are unrecoverable here
					adminv1.ReadFinished_TERMINATED_BY_DUAL_CONTROL_TIMEOUT,
					0, 0, h.cfg.Clock(),
				))
			}
			return oops.Wrap(err)
		}
	}

	// Step 6: build the start payload and emit (INV-F1/F2). After this point
	// the audit trail has captured the operator's intent.
	requestID := idgen.New()
	startPayload := h.buildStartPayload(session, resolved, flags, domesticReq, req, requestID, approvalInfo)
	if err := h.cfg.AuditEmitter.EmitStart(ctx, startPayload, requestID); err != nil {
		// Build a fresh DENY_AUDIT_PRE_DATA_PUBLISH oops carrying the inner
		// error's message. oops.Code().Wrap() preserves the DEEPEST code in
		// the chain — wrapping here would surface EMITTER_PUBLISH_FAILED to
		// the operator, defeating INV-F2 classification. Use .Errorf with
		// inner.Error() so the outer code wins (classifyTerminator + the
		// audit-emit-failure terminator depend on this).
		return oops.Code("DENY_AUDIT_PRE_DATA_PUBLISH").
			With("request_id", requestID.String()).
			With("inner_err", err.Error()).
			Errorf("audit emit failed: %s", err.Error())
	}

	// Step 7: send the ReadStarted frame. This is the FIRST data-adjacent
	// frame the client sees. Failures here are stream errors and propagate
	// to the ReadFinished classification below.
	streamErr := stream.Send(buildStartedFrame(requestID.String(), resolved, h.cfg.PolicyHash))

	var eventsScanned, decryptFails int64
	if streamErr == nil {
		// Step 8: cold-tier read + per-row decrypt + frame send.
		eventsScanned, decryptFails, streamErr = h.scanAndStream(ctx, resolved, stream)
	}

	// Step 9: classify the terminator and send the ReadFinished frame.
	term := classifyTerminator(streamErr)
	_ = stream.Send(buildFinishedFrame(term, eventsScanned, decryptFails, h.cfg.Clock())) //nolint:errcheck // best-effort terminator send; client may have disconnected and we still record the audit completion

	// Step 10: emit the completed audit event (INV-F10 — failure does NOT
	// raise, just logs WARN; metric increments inside EmitCompleted).
	completedPayload := buildCompletedPayload(requestID, term, eventsScanned, decryptFails, h.cfg.PolicyHash, h.cfg.Clock())
	if cerr := h.cfg.AuditEmitter.EmitCompleted(ctx, completedPayload, requestID); cerr != nil {
		h.cfg.Logger.WarnContext(ctx, "operator_read completed audit emit failed",
			"request_id", requestID.String(),
			"err", cerr)
	}

	if streamErr != nil {
		return oops.Wrap(streamErr)
	}
	return nil
}

// approvalRecord captures the relevant fields from an approval.Approval that
// the start-payload builder needs. Defined locally to avoid passing the
// full approval struct around.
type approvalRecord struct {
	RequestID  ulid.ULID
	ApprovedBy ulid.ULID
}

// acquireApproval implements the INV-F11/F17 dual-control flow:
//
//   - Reuse: if a fresh approved row exists for (opKind, opArgsHash) authored
//     by another operator, return it directly. No PendingApproval frame.
//   - Open + Wait: otherwise, open a fresh row, emit PendingApproval, and
//     poll WaitForApproval until ApprovalTTL elapses.
func (h *Handler) acquireApproval(
	ctx context.Context,
	session OperatorSession,
	opArgsHash []byte,
	stream streamSender,
) (*approvalRecord, error) {
	const opKind = "readstream"

	// Reuse path.
	existing, err := h.cfg.Approvals.GetByOpArgsHash(ctx, opKind, opArgsHash, session.PlayerID)
	if err == nil {
		rec := &approvalRecord{RequestID: ulid.ULID(existing.RequestID)}
		if existing.ApprovedByPlayerID != "" {
			if approverULID, parseErr := ulid.Parse(existing.ApprovedByPlayerID); parseErr == nil {
				rec.ApprovedBy = approverULID
			}
		}
		return rec, nil
	}
	if !isOopsCode(err, "APPROVAL_NOT_FOUND") {
		return nil, oops.Wrap(err)
	}

	// Open + Wait path.
	rid, openErr := h.cfg.Approvals.Open(ctx, approval.OpenRequest{
		PrimaryPlayerID: session.PlayerID,
		OpKind:          opKind,
		OpArgsHash:      opArgsHash,
	})
	if openErr != nil {
		return nil, oops.Wrap(openErr)
	}

	deadline := h.cfg.Clock().Add(h.cfg.ApprovalTTL)
	pending := &adminv1.AdminReadStreamResponse{
		Payload: &adminv1.AdminReadStreamResponse_PendingApproval{
			PendingApproval: &adminv1.PendingApproval{
				RequestId: rid[:],
				ExpiresAt: timestamppb.New(deadline),
			},
		},
	}
	if sendErr := stream.Send(pending); sendErr != nil {
		return nil, oops.Wrap(sendErr)
	}

	approved, waitErr := h.cfg.Approvals.WaitForApproval(ctx, rid, deadline)
	if waitErr != nil {
		if isOopsCode(waitErr, "APPROVAL_WAIT_DEADLINE") {
			// Same code-promotion rationale as DENY_AUDIT_PRE_DATA_PUBLISH:
			// oops.Code(x).Wrap(oopsInner) returns the DEEPEST code. Build a
			// fresh outer-coded oops with the inner detail in Errorf message.
			return nil, oops.Code("READSTREAM_DUAL_CONTROL_TIMEOUT").
				With("request_id", rid.String()).
				With("inner_err", waitErr.Error()).
				Errorf("dual-control approval timeout: %s", waitErr.Error())
		}
		// Non-deadline WaitForApproval failures (DB outage mid-poll, ctx cancel,
		// etc.) are wrapped with a distinct code so the CLI exit-code mapper can
		// distinguish dual-control errors from other server errors. A best-effort
		// ReadFinished{SERVER_ERROR} is sent so the operator CLI receives a
		// structured terminator rather than an abrupt stream error. EmitStart has
		// not been called yet so no audit row is emitted — absence is per-spec.
		wrapped := oops.Code("READSTREAM_DUAL_CONTROL_ERROR").
			With("request_id", rid.String()).
			Wrap(waitErr)
		_ = stream.Send(buildFinishedFrame( //nolint:errcheck // best-effort terminator send; client may have disconnected
			adminv1.ReadFinished_TERMINATED_BY_SERVER_ERROR, 0, 0, h.cfg.Clock(),
		))
		return nil, wrapped
	}

	rec := &approvalRecord{RequestID: ulid.ULID(rid)}
	if approved.ApprovedByPlayerID != "" {
		if approverULID, parseErr := ulid.Parse(approved.ApprovedByPlayerID); parseErr == nil {
			rec.ApprovedBy = approverULID
		}
	}
	return rec, nil
}

// scanAndStream executes the cold-tier read and streams each row as an
// EventFrame. Successful decrypts produce a plaintext frame; row-level
// classifier-mapped failures produce a metadata-only frame; fatal errors
// (ctx canceled / deadline) bail and return non-nil err.
//
// On err == nil, the scan completed cleanly (TerminatedBy will be
// CLIENT_EOF in the caller).
func (h *Handler) scanAndStream( //nolint:gocritic // unnamedResult: three-return aggregate; the four-tuple is documented in the doc comment so naming each value adds no clarity
	ctx context.Context,
	resolved Resolved,
	stream streamSender,
) (int64, int64, error) {
	subjects := BuildSubjects(resolved.Contexts, h.cfg.Game)
	q := ColdQuery{Subjects: subjects, Since: resolved.Since, Until: resolved.Until}

	rows, err := h.cfg.ColdReader.Read(ctx, q)
	if err != nil {
		return 0, 0, oops.Wrap(err)
	}

	var eventsScanned, decryptFails int64
	send := stream.Send

	// Per INV-F14: per-frame write deadline. Production requests carry the
	// underlying *net.UnixConn via socket.UnixConnFromContext (wired through
	// http.Server.ConnContext), so SendWithDeadline enforces the deadline at
	// the kernel I/O layer with no orphan goroutine. Unit tests that bypass
	// the admin socket fall back to a no-op setter — the unit tests for
	// deadline semantics live in deadline_writer_test.go.
	setDeadline := func(_ time.Time) error { return nil }
	if conn, ok := socket.UnixConnFromContext(ctx); ok {
		setDeadline = conn.SetWriteDeadline
	}

	for i := range rows {
		row := rows[i]
		plaintext, reason, fatal, decryptErr := DecryptRow(ctx, row, h.cfg.DEK, h.cfg.Codecs)
		if fatal {
			return eventsScanned, decryptFails, oops.Wrap(decryptErr)
		}

		var frame *adminv1.AdminReadStreamResponse
		if decryptErr != nil {
			frame = buildMetadataOnlyEventFrame(row, reason)
		} else {
			frame = buildPlaintextEventFrame(row, plaintext)
		}

		if sendErr := SendWithDeadline(ctx, send, frame, h.cfg.WriteDeadline, setDeadline); sendErr != nil {
			return eventsScanned, decryptFails, oops.Wrap(sendErr)
		}

		eventsScanned++
		if decryptErr != nil {
			decryptFails++
		}
	}

	return eventsScanned, decryptFails, nil
}

// buildStartPayload assembles the audit start payload. INV-F7: both
// Requested-* (nullable, captures defaulting) and Resolved-* (always
// populated) MUST be present.
func (h *Handler) buildStartPayload(
	session OperatorSession,
	resolved Resolved,
	flags ResolvedFlags,
	domesticReq Request,
	protoReq *adminv1.AdminReadStreamRequest,
	requestID ulid.ULID,
	appr *approvalRecord,
) OperatorReadStartPayload {
	p := OperatorReadStartPayload{
		OperatorSessionTokenID: session.SessionTokenID,
		PeerCredUID:            session.PeerCredUID,
		PeerCredPID:            session.PeerCredPID,
		Justification:          resolved.Justification,
		RequestedContexts:      domesticReq.Contexts,
		ResolvedContexts:       resolved.Contexts,
		ResolvedSince:          resolved.Since,
		ResolvedUntil:          resolved.Until,
		PolicyHash:             h.cfg.PolicyHash,
		RequestID:              requestID.String(),
		StartedAt:              h.cfg.Clock(),
	}
	if pid, err := ulid.Parse(session.PlayerID); err == nil {
		p.OperatorPlayerID = pid
	}
	// RequestedSince/Until: nil when defaulted (captures intent); the
	// resolved fields always carry the effective values.
	if !flags.SinceDefaulted {
		s := protoReq.GetSince().AsTime()
		p.RequestedSince = &s
	}
	if !flags.UntilDefaulted {
		u := protoReq.GetUntil().AsTime()
		p.RequestedUntil = &u
	}
	if appr != nil {
		p.DualControl = true
		approvalULID := appr.RequestID
		p.ApprovalID = &approvalULID
		if appr.ApprovedBy != (ulid.ULID{}) {
			approverULID := appr.ApprovedBy
			p.ApproverPlayerID = &approverULID
		}
	}
	return p
}

// buildCompletedPayload assembles the audit completed payload. Chain
// bookkeeping (PrevHash, SelfHash) is stamped inside the emitter.
func buildCompletedPayload(
	requestID ulid.ULID,
	term adminv1.ReadFinished_TerminatedBy,
	eventsScanned, decryptFails int64,
	policyHash string,
	now time.Time,
) OperatorReadCompletedPayload {
	return OperatorReadCompletedPayload{
		RequestID:        requestID.String(),
		TerminatedBy:     terminatedByLabel(term),
		EventsScanned:    eventsScanned,
		DecryptFailCount: decryptFails,
		PolicyHash:       policyHash,
		FinishedAt:       now,
	}
}

// buildStartedFrame builds the ReadStarted frame. policy_hash is delivered
// as raw 32 bytes; the audit payload stores the "sha256:<hex>" string form.
func buildStartedFrame(requestID string, resolved Resolved, policyHashHex string) *adminv1.AdminReadStreamResponse {
	contexts := make([]*adminv1.ContextRef, len(resolved.Contexts))
	for i, c := range resolved.Contexts {
		contexts[i] = &adminv1.ContextRef{Type: c.Type, Ids: append([]string(nil), c.IDs...)}
	}
	return &adminv1.AdminReadStreamResponse{
		Payload: &adminv1.AdminReadStreamResponse_Started{
			Started: &adminv1.ReadStarted{
				RequestId:        requestID,
				PolicyHash:       decodePolicyHashOrEmpty(policyHashHex),
				ResolvedSince:    timestamppb.New(resolved.Since),
				ResolvedUntil:    timestamppb.New(resolved.Until),
				ResolvedContexts: contexts,
			},
		},
	}
}

// buildFinishedFrame builds the terminal ReadFinished frame.
func buildFinishedFrame(
	term adminv1.ReadFinished_TerminatedBy,
	eventsScanned, decryptFails int64,
	now time.Time,
) *adminv1.AdminReadStreamResponse {
	return &adminv1.AdminReadStreamResponse{
		Payload: &adminv1.AdminReadStreamResponse_Finished{
			Finished: &adminv1.ReadFinished{
				TerminatedBy:     term,
				EventsScanned:    eventsScanned,
				DecryptFailCount: decryptFails,
				FinishedAt:       timestamppb.New(now),
			},
		},
	}
}

// buildPlaintextEventFrame builds an EventFrame carrying decrypted plaintext.
// metadata_only=false and no_plaintext_reason=UNSPECIFIED.
func buildPlaintextEventFrame(row ColdRow, plaintext []byte) *adminv1.AdminReadStreamResponse {
	return &adminv1.AdminReadStreamResponse{
		Payload: &adminv1.AdminReadStreamResponse_Event{
			Event: &corev1.EventFrame{
				Id:                row.ID.String(),
				Stream:            string(row.Subject),
				Type:              string(row.Type),
				Timestamp:         timestamppb.New(row.Timestamp),
				ActorType:         row.Actor.Kind.String(),
				ActorId:           row.Actor.ID.String(),
				Payload:           plaintext,
				MetadataOnly:      false,
				NoPlaintextReason: corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_UNSPECIFIED,
			},
		},
	}
}

// buildMetadataOnlyEventFrame builds an EventFrame for a row whose plaintext
// could not be recovered. metadata_only=true and no_plaintext_reason carries
// the classifier-mapped reason.
func buildMetadataOnlyEventFrame(row ColdRow, reason eventbus.NoPlaintextReason) *adminv1.AdminReadStreamResponse {
	return &adminv1.AdminReadStreamResponse{
		Payload: &adminv1.AdminReadStreamResponse_Event{
			Event: &corev1.EventFrame{
				Id:                row.ID.String(),
				Stream:            string(row.Subject),
				Type:              string(row.Type),
				Timestamp:         timestamppb.New(row.Timestamp),
				ActorType:         row.Actor.Kind.String(),
				ActorId:           row.Actor.ID.String(),
				Payload:           nil,
				MetadataOnly:      true,
				NoPlaintextReason: noPlaintextReasonToProto(reason),
			},
		},
	}
}

// classifyTerminator maps a streamErr to the ReadFinished_TerminatedBy enum.
//
// Order matters: more specific oops codes win over context.* errors so an
// audit-emit failure isn't misclassified as SERVER_ERROR.
func classifyTerminator(err error) adminv1.ReadFinished_TerminatedBy {
	switch {
	case err == nil:
		return adminv1.ReadFinished_TERMINATED_BY_CLIENT_EOF
	case isOopsCode(err, "DENY_AUDIT_PRE_DATA_PUBLISH"):
		return adminv1.ReadFinished_TERMINATED_BY_AUDIT_EMIT_FAILURE
	case isOopsCode(err, "READSTREAM_DUAL_CONTROL_TIMEOUT"):
		return adminv1.ReadFinished_TERMINATED_BY_DUAL_CONTROL_TIMEOUT
	case errors.Is(err, context.Canceled):
		return adminv1.ReadFinished_TERMINATED_BY_CLIENT_DISCONNECT
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, ErrWriteDeadlineExceeded):
		return adminv1.ReadFinished_TERMINATED_BY_DEADLINE_EXCEEDED
	default:
		return adminv1.ReadFinished_TERMINATED_BY_SERVER_ERROR
	}
}

// terminatedByLabel returns the spec-canonical string label for the
// ReadFinished_TerminatedBy enum. Used in the audit completed payload's
// "terminated_by" field.
func terminatedByLabel(t adminv1.ReadFinished_TerminatedBy) string {
	switch t {
	case adminv1.ReadFinished_TERMINATED_BY_CLIENT_EOF:
		return "client_eof"
	case adminv1.ReadFinished_TERMINATED_BY_CLIENT_DISCONNECT:
		return "client_disconnect"
	case adminv1.ReadFinished_TERMINATED_BY_DEADLINE_EXCEEDED:
		return "deadline_exceeded"
	case adminv1.ReadFinished_TERMINATED_BY_SERVER_ERROR:
		return "server_error"
	case adminv1.ReadFinished_TERMINATED_BY_DUAL_CONTROL_TIMEOUT:
		return "dual_control_timeout"
	case adminv1.ReadFinished_TERMINATED_BY_AUDIT_EMIT_FAILURE:
		return "audit_emit_failure"
	default:
		return "unspecified"
	}
}

// noPlaintextReasonToProto maps the eventbus.NoPlaintextReason enum (Go
// internal) to the corev1.NoPlaintextReason proto enum. Mirrors the
// 1:1 mapping enforced by TestINV_F16_NoPlaintextReasonProtoGoParity.
func noPlaintextReasonToProto(r eventbus.NoPlaintextReason) corev1.NoPlaintextReason {
	switch r {
	case eventbus.NoPlaintextReasonAuthGuardDeny:
		return corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_AUTHGUARD_DENY
	case eventbus.NoPlaintextReasonStaleDEK:
		return corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_STALE_DEK
	case eventbus.NoPlaintextReasonAuditQueueFull:
		return corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_AUDIT_QUEUE_FULL
	case eventbus.NoPlaintextReasonDEKMissing:
		return corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_DEK_MISSING
	case eventbus.NoPlaintextReasonDEKBadColumns:
		return corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_DEK_BAD_COLUMNS
	case eventbus.NoPlaintextReasonInternal:
		return corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_INTERNAL
	default:
		return corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_UNSPECIFIED
	}
}

// protoToDomesticRequest converts the wire proto AdminReadStreamRequest into
// the package-private Request shape that ResolveBounds consumes.
func protoToDomesticRequest(req *adminv1.AdminReadStreamRequest) Request {
	contexts := make([]ContextRef, len(req.GetContext()))
	for i, c := range req.GetContext() {
		contexts[i] = ContextRef{
			Type: c.GetType(),
			IDs:  append([]string(nil), c.GetIds()...),
		}
	}
	var since, until time.Time
	if t := req.GetSince(); t != nil {
		since = t.AsTime()
	}
	if t := req.GetUntil(); t != nil {
		until = t.AsTime()
	}
	return Request{
		Contexts:      contexts,
		Since:         since,
		Until:         until,
		Justification: req.GetJustification(),
	}
}

// computeReadStreamArgsHash returns the op-args hash for dual-control reuse.
//
// The hash is computed from the RESOLVED bounds (Resolved.Since,
// Resolved.Until, Resolved.Contexts, Resolved.Justification) — NOT from the
// raw proto request. This ensures that:
//   - Two requests that omit since/until resolve to the same effective window
//     AND hash identically, so approval reuse works correctly for equivalent
//     requests.
//   - Two requests that default to different wall-clock windows (e.g., invoked
//     minutes apart) produce different hashes, preventing an approval for one
//     window from being incorrectly reused for another.
//
// The resolved values are carried via a stable proto message to reuse
// approval.ComputeOpArgsHash's deterministic-marshal + SHA-256 primitive.
// Contexts are materialized from Resolved.Contexts (canonical, sorted by
// ResolveBounds) rather than from the raw proto Context repeated field.
func computeReadStreamArgsHash(resolved Resolved) ([]byte, error) {
	contexts := make([]*adminv1.ContextRef, 0, len(resolved.Contexts))
	for _, c := range resolved.Contexts {
		ids := make([]string, len(c.IDs))
		copy(ids, c.IDs)
		contexts = append(contexts, &adminv1.ContextRef{
			Type: c.Type,
			Ids:  ids,
		})
	}
	stable := &adminv1.AdminReadStreamRequest{
		Context:       contexts,
		Since:         timestamppb.New(resolved.Since),
		Until:         timestamppb.New(resolved.Until),
		Justification: resolved.Justification,
		// SessionToken, DualControl, SubjectPattern, TypeFilter, Limit are
		// intentionally omitted: they identify WHO or are not part of the
		// effective query shape (contexts+window+justification are).
	}
	return approval.ComputeOpArgsHash(stable) //nolint:wrapcheck // helper returns oops-coded error; wrapping discards the code
}

// decodePolicyHashOrEmpty returns the raw 32-byte SHA-256 of the policy
// hash string. The audit payload stores the "sha256:<hex>" form; the
// ReadStarted frame delivers the raw bytes. Returns nil when the policy
// hash is empty (genesis) or malformed (skipped silently — the audit
// payload still carries the canonical string form).
func decodePolicyHashOrEmpty(s string) []byte {
	b, err := decodeHashString(s)
	if err != nil {
		return nil
	}
	return b
}
