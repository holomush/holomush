// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/admin/approval"
	adminauth "github.com/holomush/holomush/internal/admin/auth"
	"github.com/holomush/holomush/internal/admin/readstream"
	socket "github.com/holomush/holomush/internal/admin/socket"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// readstreamAuditEmitterWrapper is nil in production. Tests may install a
// one-shot wrapper to fault-inject the audit emitter. The wrapper fires once
// per buildReadStreamWiring call; tests MUST reset to nil after use.
//
// This pattern mirrors the test-only-var convention used elsewhere in cmd/holomush
// (e.g., crypto_rekey_wiring.go test seams). Deliberately NOT a field on
// readStreamWiringDeps — that would contaminate production call sites.
var readstreamAuditEmitterWrapper func(readstream.OperatorReadAuditEmitter) readstream.OperatorReadAuditEmitter

// readstreamGrantsOverrideForTest is nil in production. Tests may install a
// replacement access.SubjectResolver to fault-inject capability checks (e.g.,
// F-E11: simulate a player without crypto.operator). Reset to nil after use.
//
// Like readstreamAuditEmitterWrapper, this is intentionally a package-level
// var and NOT a field on readStreamWiringDeps or the handler config struct.
var readstreamGrantsOverrideForTest access.SubjectResolver

// readStreamWiring bundles the constructed pieces of the production
// AdminReadStream substrate. Returned by buildReadStreamWiring and consumed
// by runCoreWithDeps to populate the admin-socket Config and extend the
// VerifierSubsystem's handlers list.
//
// AdminReadStream wiring follows ADR 0017 — F bypasses HistoryReader/dispatcher
// entirely. See docs/adr/0017-admin-readstream-bypasses-history-reader.md for
// the architectural decision and the wrappers it deliberately omits.
type readStreamWiring struct {
	// Handler is the AdminReadStream RPC handler ready to install in the
	// admin-socket Config. nil when buildReadStreamWiring's deps gate
	// rejected the inputs — the admin socket then falls back to
	// Unimplemented for AdminReadStream and the rest of the server boots.
	Handler socket.ReadStreamRPCHandler
	// AuditChainHandler is the per-game chain.Handler for the operator_read
	// chain. Returned so the caller can append it to the
	// VerifierSubsystem.Handlers slice (the verifier walks each registered
	// chain at boot for integrity).
	AuditChainHandler chain.Handler
}

// readStreamWiringDeps bundles the inputs buildReadStreamWiring requires
// from runCoreWithDeps. Missing fields cause the helper to return a
// zero-valued wiring with no error — the caller logs the gap and the admin
// socket surfaces Unimplemented for AdminReadStream.
type readStreamWiringDeps struct {
	Pool            *pgxpool.Pool
	GameID          string
	AuditChainRepo  chain.Repo
	AuditPublisher  eventbus.Publisher
	SubjectResolver access.SubjectResolver
	SessionStore    adminauth.SessionStore
	DEKManager      dek.Manager
	PolicyHashSrc   dek.PolicyHashSource
	// MaxWindow/DefaultWindow/WriteDeadline/ApprovalTTL flow from
	// CryptoConfig.Defaults(); the caller MUST call Defaults() before
	// passing.
	MaxWindow     time.Duration
	DefaultWindow time.Duration
	WriteDeadline time.Duration
	ApprovalTTL   time.Duration
}

// buildReadStreamWiring constructs the production AdminReadStream handler.
// Returns a zero-valued readStreamWiring (no error) when any required
// dependency is unavailable — the caller logs the gap and the admin socket
// falls back to Unimplemented for AdminReadStream.
//
// Construction order:
//
//  1. readstream.NewColdReader(pool) — the cold-tier events_audit reader.
//  2. The OperatorRead chain handler + audit emitter (chained on the
//     operator_read scope, parallel to policy.PolicySetHandlerFor and
//     dek.RekeyHandlerFor).
//  3. Adapters mapping the wire-side substrate to F's narrow seams:
//     - SessionStore: adminauth.OperatorIdentity → readstream.OperatorSession
//     (PeerCred.{UID,PID} flow through; the GID field is dropped because
//     F's audit payload schema doesn't carry it).
//     - DEKResolver: dek.Manager.Resolve already matches F's interface
//     directly, so the Manager is passed through.
//     - CodecResolver: codecRegistryAdapter delegates to the package-level
//     codec.Resolve.
//  4. Compute the policy_hash via PolicyHashSrc.CurrentPolicyHash; encode
//     as "sha256:<hex>" so the audit payload + ReadStarted frame agree on
//     the canonical wire form (decodeHashString round-trips inside the
//     readstream package).
//  5. readstream.NewHandler bundles the lot.
func buildReadStreamWiring(
	ctx context.Context,
	deps readStreamWiringDeps,
) (readStreamWiring, error) {
	if deps.Pool == nil ||
		deps.GameID == "" ||
		deps.AuditChainRepo == nil ||
		deps.AuditPublisher == nil ||
		deps.SubjectResolver == nil ||
		deps.SessionStore == nil ||
		deps.DEKManager == nil ||
		deps.PolicyHashSrc == nil {
		// Caller is responsible for logging the gap; we return an empty
		// wiring so the admin socket falls back to Unimplemented for the
		// AdminReadStream RPC and the rest of the server continues to start.
		return readStreamWiring{}, nil
	}

	coldReader := readstream.NewColdReader(deps.Pool)

	// Per-game audit chain handler for the operator_read scope (parallel to
	// dek.RekeyHandlerFor / policy.PolicySetHandlerFor).
	operatorReadHandler := readstream.OperatorReadHandlerFor(deps.GameID)

	auditEmitter := readstream.NewOperatorReadAuditEmitter(
		chain.NewEmitter(deps.AuditChainRepo),
		deps.AuditPublisher,
		operatorReadHandler,
	)
	// Test-only fault-injection seam: wrap the emitter when a test installs
	// readstreamAuditEmitterWrapper (nil in production, always).
	if readstreamAuditEmitterWrapper != nil {
		auditEmitter = readstreamAuditEmitterWrapper(auditEmitter)
	}

	// Test-only grants override: substitute the SubjectResolver when a test
	// installs readstreamGrantsOverrideForTest (nil in production, always).
	grants := deps.SubjectResolver
	if readstreamGrantsOverrideForTest != nil {
		grants = readstreamGrantsOverrideForTest
	}

	// Compute the canonical policy_hash string. INV-CRYPTO-112 / INV-F-policy_hash:
	// the audit payload stores "sha256:<hex>"; the ReadStarted wire frame
	// decodes back to the raw 32 bytes via decodeHashString. Genesis (no
	// crypto.policy_set chain entries) maps to the all-zero 32-byte
	// sentinel — same convention dek.Orchestrator uses on Phase 1.
	policyHash, phErr := deps.PolicyHashSrc.CurrentPolicyHash(ctx, "dual_control_required")
	if phErr != nil {
		return readStreamWiring{}, oops.Code("READSTREAM_POLICY_HASH_READ_FAILED").Wrap(phErr)
	}
	if policyHash == nil {
		policyHash = make([]byte, 32)
	}
	policyHashStr := fmt.Sprintf("sha256:%s", hex.EncodeToString(policyHash))

	// approval.Repo is per-pool, stateless; constructing a fresh one here
	// (rather than threading the one built for adminauth's Approve handler)
	// avoids lifting state out of the gated KEK block in core.go.
	approvalRepo := approval.NewPostgresRepo(deps.Pool, nil)

	handler, hErr := readstream.NewHandler(readstream.Config{
		Sessions:      &readstreamSessionStore{inner: deps.SessionStore},
		Grants:        grants,
		Approvals:     approvalRepo,
		ColdReader:    coldReader,
		DEK:           deps.DEKManager, // dek.Manager.Resolve already satisfies readstream.DEKResolver
		Codecs:        codecRegistryAdapter{},
		AuditEmitter:  auditEmitter,
		PolicyHash:    policyHashStr,
		Clock:         time.Now,
		Logger:        slog.Default(),
		Game:          deps.GameID,
		MaxWindow:     deps.MaxWindow,
		DefaultWindow: deps.DefaultWindow,
		WriteDeadline: deps.WriteDeadline,
		ApprovalTTL:   deps.ApprovalTTL,
	})
	if hErr != nil {
		return readStreamWiring{}, oops.Code("READSTREAM_HANDLER_CONSTRUCT_FAILED").Wrap(hErr)
	}

	return readStreamWiring{
		Handler:           handler,
		AuditChainHandler: operatorReadHandler,
	}, nil
}

// readstreamSessionStore adapts adminauth.SessionStore to the narrow
// readstream.SessionStore seam. The handler asks for an operator session
// keyed by token; the adminauth layer already carries the structured
// PeerCred (UID/GID/PID) on OperatorIdentity, so we read the fields
// directly rather than parsing the audit-string form. F's audit payload
// records only UID + PID (the chain's GID is dropped at this boundary —
// peer_cred_gid does not appear in OperatorReadStartPayload).
type readstreamSessionStore struct {
	inner adminauth.SessionStore
}

// GetOperatorSession looks up the operator session by token. Errors are
// propagated unchanged so the handler's classifier maps DENY_SESSION_*
// codes uniformly.
func (a *readstreamSessionStore) GetOperatorSession(token string) (readstream.OperatorSession, error) {
	if a.inner == nil {
		return readstream.OperatorSession{}, oops.Code("DENY_SESSION_INVALID").
			Errorf("admin session store not wired")
	}
	identity, err := a.inner.Get(token)
	if err != nil {
		return readstream.OperatorSession{}, err
	}
	return readstream.OperatorSession{
		PlayerID:       identity.PlayerID,
		SessionTokenID: token,
		PeerCredUID:    identity.PeerCred.UID,
		PeerCredPID:    identity.PeerCred.PID,
	}, nil
}

// codecRegistryAdapter satisfies readstream.CodecResolver by delegating to
// the package-level codec.Resolve. The codec registry is process-wide and
// stateless, so no construction-time state is required.
type codecRegistryAdapter struct{}

// Resolve delegates to codec.Resolve. The handler treats unknown-codec
// errors as row-level (metadata-only frame), not stream-fatal. The codec
// package returns oops-coded errors directly; pass them through unchanged
// so DecryptRow's classifier sees the original code.
func (codecRegistryAdapter) Resolve(name codec.Name) (codec.Codec, error) {
	return codec.Resolve(name)
}
