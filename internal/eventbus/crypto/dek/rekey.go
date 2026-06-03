// SPDX-License-Identifier: Apache-2.0

// Package dek provides the orchestrator-internal types for the Rekey lifecycle,
// including RekeyRequest, RekeyOutcome, OperatorIdentity, DualControlBinding,
// and ComputeRekeyArgsHash for INV-E24 request idempotency.
package dek

import (
	"crypto/sha256"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/admin/approval"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// RequestID is the 16-byte ULID PK of a crypto_rekey_checkpoints row.
// Generated via idgen.New() per CLAUDE.md "ULID Generation".
type RequestID [16]byte

// String returns the ULID-formatted string.
func (r RequestID) String() string { return ulid.ULID(r).String() }

// IsZero reports whether r is the all-zero ULID.
func (r RequestID) IsZero() bool {
	for _, b := range r {
		if b != 0 {
			return false
		}
	}
	return true
}

// RekeyRequest is the orchestrator-internal input shape. Built by the
// admin handler from the wire RekeyRequestProto after auth.
type RekeyRequest struct {
	ContextType   string
	ContextID     string
	Justification string
	Operator      OperatorIdentity
	DualControl   *DualControlBinding
	ForceDestroy  bool // only honored on resume path
}

// OperatorIdentity captures the authenticated operator credentials for
// embedding in the rekey audit event payload (spec §3.3).
type OperatorIdentity struct {
	PlayerID         string
	OSUser           string
	TOTPVerified     bool
	AuthProviderName string
}

// DualControlBinding carries the second-operator approval details when a
// rekey invocation uses --dual-control. Nil when single-control.
type DualControlBinding struct {
	ApprovalRequestID approval.RequestID
	PartnerPlayerID   string
}

// RekeyOutcome summarises the completed rekey for CLI rendering and audit.
type RekeyOutcome struct {
	RequestID        RequestID
	AuditEventID     ulid.ULID // eventbus.EventID = ulid.ULID; avoided to prevent import cycle
	Phase3RowCount   int
	Phase5Attempts   int
	ForceDestroyUsed bool
	Resumed          bool
	DurationMs       int64
	StartedAt        time.Time
	CompletedAt      time.Time
}

// ArgsHash is the 32-byte SHA-256 over the proto-deterministic-marshal of
// the stable fields of a RekeyRequest (context_type, context_id,
// justification). Named type to satisfy INV-CRYPTO-16 (dek package must not export
// bare []byte). Callers needing a []byte (e.g., SQL driver) use hash[:].
type ArgsHash [32]byte

// ComputeRekeyArgsHash matches the algorithm D ships in approval.ComputeOpArgsHash:
// SHA-256 over proto.MarshalOptions{Deterministic: true}.Marshal(args) where
// args is the proto RekeyRequest. INV-E24 (stable across binary builds with
// protobuf-go pinned per INV-D18).
//
// The hash binds the WORK (context_type, context_id, justification), not WHO.
// Different operators attempting the same rekey args produce the same hash,
// enabling same-args resume (Q1). Returns ArgsHash (a [32]byte) to comply
// with the dek package's INV-CRYPTO-16 no-exported-[]byte constraint.
func ComputeRekeyArgsHash(req RekeyRequest) (ArgsHash, error) {
	protoReq := &adminv1.RekeyRequest{
		ContextType:   req.ContextType,
		ContextId:     req.ContextID,
		Justification: req.Justification,
		// Operator + DualControl deliberately excluded — the hash binds
		// the WORK (what's being rekeyed), not WHO. Different operators
		// attempting the same rekey args produce the same hash.
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(protoReq)
	if err != nil {
		return ArgsHash{}, oops.Code("DEK_REKEY_OP_ARGS_HASH_MARSHAL_FAILED").Wrap(err)
	}
	return sha256.Sum256(raw), nil
}
