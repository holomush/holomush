// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/samber/oops"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/access"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// CheckpointView is a read-only projection of a crypto_rekey_checkpoints row
// for use by the Status and List RPC handlers. It avoids importing dek
// (which would create an import cycle via approval → adminauth → socket)
// by carrying only the fields needed for the RPC response.
type CheckpointView struct {
	RequestID            [16]byte
	ContextType          string
	ContextID            string
	Status               string
	PrimaryPlayerID      string
	StartedAt            time.Time
	LastHeartbeatAt      time.Time
	CompletedAt          *time.Time
	Phase5AttemptCount   int
	Phase5MissingMembers []string
	ForceDestroy         bool
	OldDEKID             int64
	NewDEKID             *int64
}

// CheckpointListFilter parameterises the admin list query.
// Limit is capped at 100 by the handler; the repo applies whatever value
// is passed (handler guards the cap before calling).
type CheckpointListFilter struct {
	IncludeTerminal bool
	ContextPattern  *string
	Since           *time.Time
	Limit           int
}

// CheckpointStatusReader is the narrow interface the RekeyHandler needs for
// the Status and List RPCs. The production adapter wraps dek.CheckpointRepo,
// projecting Checkpoint rows into CheckpointView values, and lives in the
// wiring layer (cmd/holomush or internal/bootstrap) where both packages
// are importable.
type CheckpointStatusReader interface {
	GetCheckpoint(ctx context.Context, rid [16]byte) (CheckpointView, error)
	ListCheckpoints(ctx context.Context, filter CheckpointListFilter) ([]CheckpointView, error)
}

// RekeyListStream is the narrow streaming interface the List handler uses.
// *connect.ServerStream[adminv1.RekeyStatusResponse] satisfies this at the
// ConnectRPC call site; tests use a fake.
type RekeyListStream interface {
	Send(*adminv1.RekeyStatusResponse) error
}

// OperatorSession is the minimal identity shape the RekeyHandler needs from
// an authenticated operator session. It mirrors the fields the handler reads
// from adminauth.OperatorIdentity without importing the adminauth package
// (which would create an import cycle: adminauth → socket → adminauth).
type OperatorSession struct {
	PlayerID         string
	OSUser           string // PeerCredString() — "uid=N gid=N pid=N"
	TOTPVerified     bool
	AuthProviderName string
}

// RekeySessionStore is the narrow interface the RekeyHandler requires from
// the operator session store. The production adapter wraps adminauth.SessionStore,
// mapping adminauth.OperatorIdentity to OperatorSession, and lives in
// the package that wires the two together (cmd/holomush or internal/bootstrap)
// where both packages are importable.
type RekeySessionStore interface {
	GetOperatorSession(token string) (OperatorSession, error)
}

// OperatorRoleChecker is the narrow interface for the role re-check
// (INV-CRYPTO-83 defense-in-depth). adminauth.PlayerRoleHasher satisfies this
// interface implicitly; defined narrowly to avoid importing adminauth.
type OperatorRoleChecker interface {
	PlayerHasRole(ctx context.Context, playerID, role string) (bool, error)
}

// RekeyOperatorIdentity carries operator credentials through the
// OrchestratorRunner boundary. Defined here (not in dek) so the socket
// package never imports the dek→approval→auth→socket chain.
type RekeyOperatorIdentity struct {
	PlayerID         string
	OSUser           string
	TOTPVerified     bool
	AuthProviderName string
}

// RekeyRunRequest is the orchestrator input shape the socket package needs.
// It is a socket-layer projection of dek.RekeyRequest to avoid importing dek
// (which transitively imports adminauth, creating an import cycle through
// approval.handler → adminauth → socket). The production adapter converts
// RekeyRunRequest → dek.RekeyRequest and lives in the wiring layer.
//
// On the resume path (RekeyResume RPC), only RequestID, Operator, and
// ForceDestroy are populated by the handler. ContextType, ContextID, and
// Justification are zero — the production adapter resolves them from the
// checkpoint row keyed by RequestID before forwarding to dek.Orchestrator.Run.
type RekeyRunRequest struct {
	// RequestID is the 16-byte ULID of the checkpoint to resume.
	// Non-zero only on the RekeyResume path; zero on the fresh-start Rekey path.
	// When non-zero, the adapter MUST resolve ContextType/ContextID/Justification
	// from the checkpoint row before calling dek.Orchestrator.Run.
	RequestID     [16]byte
	ContextType   string
	ContextID     string
	Justification string
	Operator      RekeyOperatorIdentity
	ForceDestroy  bool
}

// RekeyRunOutcome is the orchestrator result shape the socket package needs.
// Mirrors dek.RekeyOutcome fields needed for stream emission.
type RekeyRunOutcome struct {
	RequestID        [16]byte
	AuditEventID     [16]byte
	Phase3RowCount   int
	Phase5Attempts   int
	ForceDestroyUsed bool
	Resumed          bool
	DurationMs       int64
}

// OrchestratorRunner is the narrow seam the RekeyHandler consumes from the
// rekey orchestrator. The production wiring supplies an adapter that
// converts RekeyRunRequest to dek.RekeyRequest and dek.RekeyOutcome back to
// RekeyRunOutcome. Decoupled to avoid importing dek (and its transitive chain
// through approval → adminauth → socket).
type OrchestratorRunner interface {
	Run(ctx context.Context, req RekeyRunRequest) (RekeyRunOutcome, error)
}

// RekeyAbortRequest is the socket-layer projection of the abort input.
// It carries the request_id (as a fixed-size [16]byte) and the aborter's
// player ID. The production adapter converts this to dek.RequestID.
type RekeyAbortRequest struct {
	RequestID [16]byte
	PlayerID  string
}

// RekeyAbortOutcome is the socket-layer projection of the abort result.
type RekeyAbortOutcome struct {
	AbortedAt    time.Time
	AuditEventID [16]byte
}

// RekeyAbortRunner is the narrow seam the RekeyHandler consumes for the
// unary abort operation. The production wiring adapts dek.CheckpointRepo
// and dek.RekeyAuditEmitter behind this interface to avoid importing dek
// (which would create an import cycle via approval → adminauth → socket).
//
// INV-E17: the runner MUST accept single-control regardless of site policy.
type RekeyAbortRunner interface {
	RunAbort(ctx context.Context, req RekeyAbortRequest) (RekeyAbortOutcome, error)
}

// RekeyStreamSender is the narrow interface the RekeyHandler uses to emit
// progress events. *connect.ServerStream[adminv1.RekeyProgress] satisfies
// this at the ConnectRPC call site; tests use a fake.
type RekeyStreamSender interface {
	Send(*adminv1.RekeyProgress) error
}

// RekeyHandler implements the Rekey, RekeyResume, RekeyAbort, RekeyStatus,
// and RekeyList admin RPCs.
//
// It validates the operator session via RekeySessionStore, re-asserts the
// crypto.operator capability and admin role (INV-CRYPTO-83 defense-in-depth),
// then delegates to the OrchestratorRunner for the 7-phase lifecycle,
// RekeyAbortRunner for the unary abort operation, or CheckpointStatusReader
// for the read-only status and list operations.
//
// MVP streaming: one RekeyCompleted or RekeyError event at end.
// Per-phase progress updates are a follow-up enhancement; the proto
// messages are pre-defined to support richer streaming.
type RekeyHandler struct {
	sessions  RekeySessionStore
	grants    access.SubjectResolver
	roleStore OperatorRoleChecker
	orch      OrchestratorRunner
	abort     RekeyAbortRunner
	repo      CheckpointStatusReader
}

// NewRekeyHandler constructs a RekeyHandler with explicit dependencies.
// repo may be nil when only the Rekey, RekeyResume, and RekeyAbort RPCs
// are needed; RekeyStatus and RekeyList will return DENY_SESSION_INVALID
// or propagate the nil-repo panic if called without a repo.
// Production callers MUST supply a non-nil repo.
func NewRekeyHandler(
	sessions RekeySessionStore,
	grants access.SubjectResolver,
	roleStore OperatorRoleChecker,
	orch OrchestratorRunner,
	abort RekeyAbortRunner,
	repo CheckpointStatusReader,
) *RekeyHandler {
	return &RekeyHandler{
		sessions:  sessions,
		grants:    grants,
		roleStore: roleStore,
		orch:      orch,
		abort:     abort,
		repo:      repo,
	}
}

// Rekey is the AdminService.Rekey RPC entry point. Validates session,
// re-asserts capability + role, constructs a RekeyRunRequest, runs the
// orchestrator, and emits a terminal RekeyCompleted or RekeyError event.
func (h *RekeyHandler) Rekey(
	ctx context.Context,
	req *adminv1.RekeyRequest,
	stream RekeyStreamSender,
) error {
	identity, err := h.sessions.GetOperatorSession(req.GetSessionToken())
	if err != nil {
		return oops.Wrap(err)
	}
	if err := h.assertOperatorAdmin(ctx, identity.PlayerID); err != nil {
		return err
	}

	orchReq := h.buildRekeyRequest(req, identity)
	return h.runWithProgress(ctx, orchReq, stream)
}

// RekeyResume is the AdminService.RekeyResume RPC entry point. Validates
// session, re-asserts capability + role, and resumes an in-flight rekey by
// delegating to OrchestratorRunner.Run with the RequestID from the proto.
//
// INV-E16 idempotency and INV-E4 same-args resume are enforced inside
// Orchestrator.Run — the handler does not need to re-check them.
func (h *RekeyHandler) RekeyResume(
	ctx context.Context,
	req *adminv1.RekeyResumeRequest,
	stream RekeyStreamSender,
) error {
	identity, err := h.sessions.GetOperatorSession(req.GetSessionToken())
	if err != nil {
		return oops.Wrap(err)
	}
	if err := h.assertOperatorAdmin(ctx, identity.PlayerID); err != nil {
		return err
	}

	if len(req.GetRequestId()) != 16 {
		return oops.Code("REKEY_INVALID_REQUEST_ID").
			Errorf("request_id must be a 16-byte ULID")
	}
	// Detect zero request_id (all-bytes-zero sentinel, never a valid ULID).
	var hasNonZero bool
	for _, b := range req.GetRequestId() {
		if b != 0 {
			hasNonZero = true
			break
		}
	}
	if !hasNonZero {
		return oops.Code("REKEY_INVALID_REQUEST_ID").
			Errorf("request_id must be a non-zero ULID")
	}

	// Pack the validated request_id into a fixed-size [16]byte field.
	// The adapter resolves ContextType/ContextID/Justification from the
	// checkpoint row using this RequestID before forwarding to the orchestrator.
	var ridFixed [16]byte
	copy(ridFixed[:], req.GetRequestId())

	orchReq := RekeyRunRequest{
		RequestID:    ridFixed,
		Operator:     RekeyOperatorIdentity(identity),
		ForceDestroy: req.GetForceDestroy(),
		// ContextType/ContextID/Justification are intentionally zero here.
		// The OrchestratorRunner adapter looks them up from the checkpoint row
		// keyed by RequestID before calling dek.Orchestrator.Run (INV-E4/E16).
	}
	return h.runWithProgress(ctx, orchReq, stream)
}

// RekeyAbort is the AdminService.RekeyAbort RPC entry point. Validates the
// session and crypto.operator capability, then delegates to RekeyAbortRunner.
//
// INV-E17-ABORT-NO-DUAL-CONTROL: abort is single-control regardless of site
// dual_control_required policy. Any crypto.operator session may abort any
// non-terminal checkpoint — not just the original primary operator.
func (h *RekeyHandler) RekeyAbort(
	ctx context.Context,
	req *adminv1.RekeyAbortRequest,
) (*adminv1.RekeyAbortResponse, error) {
	identity, err := h.sessions.GetOperatorSession(req.GetSessionToken())
	if err != nil {
		return nil, oops.Wrap(err)
	}
	// INV-E17: only the crypto.operator capability is required — no admin role
	// re-check, no dual-control approval. Abort is a non-destructive control
	// operation; the destructive phase (DEK destroy) is part of rekey itself.
	hasCap, err := access.HasPlayerGrant(ctx, h.grants, identity.PlayerID, access.CapabilityCryptoOperator)
	if err != nil {
		return nil, oops.Code("INGAME_GRANT_LOOKUP_FAILED").
			With("player_id", identity.PlayerID).Wrap(err)
	}
	if !hasCap {
		return nil, oops.Code("DENY_NOT_OPERATOR").
			With("player_id", identity.PlayerID).
			Errorf("crypto.operator capability absent")
	}

	if len(req.GetRequestId()) != 16 {
		return nil, oops.Code("REKEY_INVALID_REQUEST_ID").
			Errorf("request_id must be a 16-byte ULID")
	}
	var hasNonZero bool
	for _, b := range req.GetRequestId() {
		if b != 0 {
			hasNonZero = true
			break
		}
	}
	if !hasNonZero {
		return nil, oops.Code("REKEY_INVALID_REQUEST_ID").
			Errorf("request_id must be a non-zero ULID")
	}

	var rid [16]byte
	copy(rid[:], req.GetRequestId())

	out, err := h.abort.RunAbort(ctx, RekeyAbortRequest{
		RequestID: rid,
		PlayerID:  identity.PlayerID,
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // runner returns typed oops errors; re-wrapping would discard the error code
	}
	return &adminv1.RekeyAbortResponse{
		AbortedAt:    timestamppb.New(out.AbortedAt),
		AuditEventId: out.AuditEventID[:],
	}, nil
}

// assertOperatorAdmin re-asserts the two INV-CRYPTO-83 defense-in-depth gates:
// (1) the player still holds the crypto.operator capability, and (2) the
// player still holds the admin role.
func (h *RekeyHandler) assertOperatorAdmin(ctx context.Context, playerID string) error {
	hasCap, err := access.HasPlayerGrant(ctx, h.grants, playerID, access.CapabilityCryptoOperator)
	if err != nil {
		return oops.Code("INGAME_GRANT_LOOKUP_FAILED").
			With("player_id", playerID).Wrap(err)
	}
	if !hasCap {
		return oops.Code("DENY_NOT_OPERATOR").
			With("player_id", playerID).
			Errorf("crypto.operator capability absent")
	}
	hasRole, err := h.roleStore.PlayerHasRole(ctx, playerID, access.RoleAdmin)
	if err != nil {
		return oops.Code("INGAME_ROLE_LOOKUP_FAILED").
			With("player_id", playerID).Wrap(err)
	}
	if !hasRole {
		return oops.Code("DENY_NOT_ADMIN_ROLE").
			With("player_id", playerID).
			Errorf("admin role absent")
	}
	return nil
}

// runWithProgress calls OrchestratorRunner.Run and emits a single terminal
// RekeyProgress event (RekeyCompleted on success, RekeyError on failure).
// Orchestrator errors are surfaced via stream, not returned as handler errors.
func (h *RekeyHandler) runWithProgress(
	ctx context.Context,
	req RekeyRunRequest,
	stream RekeyStreamSender,
) error {
	out, err := h.orch.Run(ctx, req)
	if err != nil {
		errCode := extractOopsCode(err)
		return stream.Send(&adminv1.RekeyProgress{ //nolint:wrapcheck // stream.Send returns transport errors; wrapping hides the semantic
			Event: &adminv1.RekeyProgress_Error{Error: &adminv1.RekeyError{
				Code:    errCode,
				Message: err.Error(),
			}},
		})
	}
	return stream.Send(&adminv1.RekeyProgress{ //nolint:wrapcheck // stream.Send returns transport errors; wrapping hides the semantic
		Event: &adminv1.RekeyProgress_Completed{Completed: &adminv1.RekeyCompleted{
			RequestId:           out.RequestID[:],
			AuditEventId:        out.AuditEventID[:],
			DurationMs:          out.DurationMs,
			Phase3RowsRewritten: int64(out.Phase3RowCount),
			Phase5Attempts:      int32(out.Phase5Attempts), //nolint:gosec // G115: Phase5Attempts is a small counter; int32 overflow not possible in practice
			ForceDestroyUsed:    out.ForceDestroyUsed,
			Resumed:             out.Resumed,
		}},
	})
}

// buildRekeyRequest converts a wire RekeyRequest proto + resolved identity
// into an orchestrator RekeyRunRequest.
func (h *RekeyHandler) buildRekeyRequest(
	req *adminv1.RekeyRequest,
	identity OperatorSession,
) RekeyRunRequest {
	return RekeyRunRequest{
		ContextType:   req.GetContextType(),
		ContextID:     req.GetContextId(),
		Justification: req.GetJustification(),
		Operator:      RekeyOperatorIdentity(identity),
	}
}

// extractOopsCode returns the oops error code string from err, or
// "UNKNOWN" if err is not an oops error or has no string code.
func extractOopsCode(err error) string {
	if oopsErr, ok := oops.AsOops(err); ok {
		if code, ok := oopsErr.Code().(string); ok && code != "" {
			return code
		}
	}
	return "UNKNOWN"
}

// RekeyStatus is the AdminService.RekeyStatus RPC entry point. Validates
// the session and crypto.operator capability, then returns the full
// checkpoint state for the given request_id.
func (h *RekeyHandler) RekeyStatus(
	ctx context.Context,
	req *adminv1.RekeyStatusRequest,
) (*adminv1.RekeyStatusResponse, error) {
	identity, err := h.sessions.GetOperatorSession(req.GetSessionToken())
	if err != nil {
		return nil, oops.Wrap(err)
	}
	hasCap, err := access.HasPlayerGrant(ctx, h.grants, identity.PlayerID, access.CapabilityCryptoOperator)
	if err != nil {
		return nil, oops.Code("INGAME_GRANT_LOOKUP_FAILED").
			With("player_id", identity.PlayerID).Wrap(err)
	}
	if !hasCap {
		return nil, oops.Code("DENY_NOT_OPERATOR").
			With("player_id", identity.PlayerID).
			Errorf("crypto.operator capability absent")
	}
	var rid [16]byte
	copy(rid[:], req.GetRequestId())
	view, err := h.repo.GetCheckpoint(ctx, rid)
	if err != nil {
		return nil, err //nolint:wrapcheck // repo returns typed oops errors; re-wrapping would discard the error code
	}
	return checkpointViewToProto(view), nil
}

// RekeyList is the AdminService.RekeyList RPC entry point. Validates
// the session and crypto.operator capability, then streams checkpoint rows
// matching the filter (non-terminal by default, 100-row cap).
func (h *RekeyHandler) RekeyList(
	ctx context.Context,
	req *adminv1.RekeyListRequest,
	stream RekeyListStream,
) error {
	identity, err := h.sessions.GetOperatorSession(req.GetSessionToken())
	if err != nil {
		return oops.Wrap(err)
	}
	hasCap, err := access.HasPlayerGrant(ctx, h.grants, identity.PlayerID, access.CapabilityCryptoOperator)
	if err != nil {
		return oops.Code("INGAME_GRANT_LOOKUP_FAILED").
			With("player_id", identity.PlayerID).Wrap(err)
	}
	if !hasCap {
		return oops.Code("DENY_NOT_OPERATOR").
			With("player_id", identity.PlayerID).
			Errorf("crypto.operator capability absent")
	}
	limit := int(req.GetLimit())
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var since *time.Time
	if req.GetSince() != nil {
		t := req.GetSince().AsTime()
		since = &t
	}
	var ctxPattern *string
	if req.ContextPattern != nil {
		cp := req.GetContextPattern()
		ctxPattern = &cp
	}
	views, err := h.repo.ListCheckpoints(ctx, CheckpointListFilter{
		IncludeTerminal: req.GetIncludeTerminal(),
		ContextPattern:  ctxPattern,
		Since:           since,
		Limit:           limit,
	})
	if err != nil {
		return err //nolint:wrapcheck // repo returns typed oops errors; re-wrapping would discard the error code
	}
	for i := range views {
		if err := stream.Send(checkpointViewToProto(views[i])); err != nil {
			return err //nolint:wrapcheck // stream.Send returns transport errors; wrapping hides the semantic
		}
	}
	return nil
}

// checkpointViewToProto converts a CheckpointView to an adminv1.RekeyStatusResponse.
func checkpointViewToProto(v CheckpointView) *adminv1.RekeyStatusResponse {
	res := &adminv1.RekeyStatusResponse{
		RequestId:            v.RequestID[:],
		ContextType:          v.ContextType,
		ContextId:            v.ContextID,
		Status:               v.Status,
		PrimaryPlayerId:      v.PrimaryPlayerID,
		StartedAt:            timestamppb.New(v.StartedAt),
		LastHeartbeatAt:      timestamppb.New(v.LastHeartbeatAt),
		Phase5AttemptCount:   int32(v.Phase5AttemptCount), //nolint:gosec // G115: Phase5AttemptCount is a small counter; int32 overflow not possible in practice
		Phase5MissingMembers: v.Phase5MissingMembers,
		ForceDestroy:         v.ForceDestroy,
	}
	if v.CompletedAt != nil {
		res.CompletedAt = timestamppb.New(*v.CompletedAt)
	}
	if v.OldDEKID != 0 {
		res.OldDekId = &v.OldDEKID
	}
	if v.NewDEKID != nil {
		res.NewDekId = v.NewDEKID
	}
	return res
}

// RekeyConnectHandler wraps RekeyHandler to implement the RekeyRPCHandler
// interface that compositeHandler delegates to. It adapts between the
// ConnectRPC *connect.ServerStream and the RekeyStreamSender interface,
// and translates oops errors to connect errors via the deny code map.
type RekeyConnectHandler struct {
	inner *RekeyHandler
	deny  func(error) error
}

// NewRekeyConnectHandler constructs the connect adapter with the provided
// deny-code-to-connect mapper (typically adminauth.MapDenyToConnect, injected
// to avoid the import cycle in this package).
func NewRekeyConnectHandler(h *RekeyHandler, denyMapper func(error) error) *RekeyConnectHandler {
	return &RekeyConnectHandler{inner: h, deny: denyMapper}
}

// HandleRekey adapts a ConnectRPC server stream to the RekeyHandler.Rekey method.
func (c *RekeyConnectHandler) HandleRekey(
	ctx context.Context,
	req *connect.Request[adminv1.RekeyRequest],
	stream *connect.ServerStream[adminv1.RekeyProgress],
) error {
	if err := c.inner.Rekey(ctx, req.Msg, stream); err != nil {
		return c.deny(err)
	}
	return nil
}

// HandleRekeyResume adapts a ConnectRPC server stream to the
// RekeyHandler.RekeyResume method.
func (c *RekeyConnectHandler) HandleRekeyResume(
	ctx context.Context,
	req *connect.Request[adminv1.RekeyResumeRequest],
	stream *connect.ServerStream[adminv1.RekeyProgress],
) error {
	if err := c.inner.RekeyResume(ctx, req.Msg, stream); err != nil {
		return c.deny(err)
	}
	return nil
}

// HandleRekeyAbort adapts a ConnectRPC unary call to the
// RekeyHandler.RekeyAbort method.
func (c *RekeyConnectHandler) HandleRekeyAbort(
	ctx context.Context,
	req *connect.Request[adminv1.RekeyAbortRequest],
) (*connect.Response[adminv1.RekeyAbortResponse], error) {
	res, err := c.inner.RekeyAbort(ctx, req.Msg)
	if err != nil {
		return nil, c.deny(err)
	}
	return connect.NewResponse(res), nil
}

// HandleRekeyStatus adapts a ConnectRPC unary call to the
// RekeyHandler.RekeyStatus method.
func (c *RekeyConnectHandler) HandleRekeyStatus(
	ctx context.Context,
	req *connect.Request[adminv1.RekeyStatusRequest],
) (*connect.Response[adminv1.RekeyStatusResponse], error) {
	res, err := c.inner.RekeyStatus(ctx, req.Msg)
	if err != nil {
		return nil, c.deny(err)
	}
	return connect.NewResponse(res), nil
}

// HandleRekeyList adapts a ConnectRPC server stream to the
// RekeyHandler.RekeyList method.
func (c *RekeyConnectHandler) HandleRekeyList(
	ctx context.Context,
	req *connect.Request[adminv1.RekeyListRequest],
	stream *connect.ServerStream[adminv1.RekeyStatusResponse],
) error {
	if err := c.inner.RekeyList(ctx, req.Msg, stream); err != nil {
		return c.deny(err)
	}
	return nil
}
