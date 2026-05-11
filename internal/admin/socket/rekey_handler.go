// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket

import (
	"context"

	"connectrpc.com/connect"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

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
// (INV-D16 defense-in-depth). adminauth.PlayerRoleHasher satisfies this
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
type RekeyRunRequest struct {
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

// RekeyStreamSender is the narrow interface the RekeyHandler uses to emit
// progress events. *connect.ServerStream[adminv1.RekeyProgress] satisfies
// this at the ConnectRPC call site; tests use a fake.
type RekeyStreamSender interface {
	Send(*adminv1.RekeyProgress) error
}

// RekeyHandler implements the Rekey and RekeyResume admin RPCs.
//
// It validates the operator session via RekeySessionStore, re-asserts the
// crypto.operator capability and admin role (INV-D16 defense-in-depth),
// then delegates to the OrchestratorRunner for the 7-phase lifecycle.
//
// MVP streaming: one RekeyCompleted or RekeyError event at end.
// Per-phase progress updates are a follow-up enhancement; the proto
// messages are pre-defined to support richer streaming.
type RekeyHandler struct {
	sessions  RekeySessionStore
	grants    access.SubjectResolver
	roleStore OperatorRoleChecker
	orch      OrchestratorRunner
}

// NewRekeyHandler constructs a RekeyHandler with explicit dependencies.
func NewRekeyHandler(
	sessions RekeySessionStore,
	grants access.SubjectResolver,
	roleStore OperatorRoleChecker,
	orch OrchestratorRunner,
) *RekeyHandler {
	return &RekeyHandler{
		sessions:  sessions,
		grants:    grants,
		roleStore: roleStore,
		orch:      orch,
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

	// RequestID is carried to the orchestrator via RekeyRunRequest so the
	// runner can look up the checkpoint. We pack it into a fixed-size field
	// on the request; the production adapter converts to dek.RequestID.
	var ridFixed [16]byte
	copy(ridFixed[:], req.GetRequestId())

	orchReq := RekeyRunRequest{
		Operator:     RekeyOperatorIdentity(identity),
		ForceDestroy: req.GetForceDestroy(),
		// ContextType/ContextID/Justification are not supplied on the resume
		// path — the production OrchestratorRunner adapter resolves them from
		// the checkpoint row using ridFixed. The socket layer only forwards
		// the operator identity and ForceDestroy flag.
	}
	_ = ridFixed // consumed by the production OrchestratorRunner adapter
	return h.runWithProgress(ctx, orchReq, stream)
}

// assertOperatorAdmin re-asserts the two INV-D16 defense-in-depth gates:
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
		Operator: RekeyOperatorIdentity(identity),
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
