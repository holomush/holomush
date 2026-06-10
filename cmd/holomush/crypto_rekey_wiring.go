// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	adminauth "github.com/holomush/holomush/internal/admin/auth"
	socket "github.com/holomush/holomush/internal/admin/socket"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/invalidation"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
)

// rekeyWiring bundles the constructed pieces of the production Rekey
// substrate. It is the return shape of buildRekeyWiring — callers wire its
// fields into the admin-socket Config, the history dispatcher, and the
// orchestrator's dependency seams.
//
// Sub-epic E T44 production wiring (holomush-jxo8.7.44). The .35 in-process
// harness (internal/testsupport/holomushtest) constructs the same shape for
// tests; this file is the production-grade equivalent. The two construct
// independent dek.Manager instances (one per process) so test isolation is
// preserved.
type rekeyWiring struct {
	// Manager is the production dek.Manager backed by the real KEK provider,
	// pgxpool-backed Store, and DEK / Participants caches.
	Manager dek.Manager
	// Orchestrator is the 7-phase Rekey orchestrator with all five
	// post-construction seams installed (MaterialResolver, Phase5Coordinator,
	// Destroyer, AuditEmitter, DataDir).
	Orchestrator *dek.Orchestrator
	// CheckpointRepo is the underlying *dek.CheckpointRepo, retained for the
	// admin handler's CheckpointStatusReader + RekeyAbortRunner adapters.
	CheckpointRepo *dek.CheckpointRepo
	// AuditEmitter is the *dek.RekeyAuditEmitter; the abort path reuses it
	// to emit the abort audit event (INV-CRYPTO-104).
	AuditEmitter *dek.RekeyAuditEmitter
	// RekeyHandler is the socket-layer RekeyConnectHandler ready to install
	// in the admin-socket Config. nil when the upstream wiring (KEK / Manager)
	// is unavailable — the admin socket then surfaces Unimplemented for the
	// Rekey RPCs.
	RekeyHandler socket.RekeyRPCHandler
}

// rekeyWiringDeps bundles the inputs buildRekeyWiring requires from the
// caller (runCoreWithDeps). All fields are required when non-nil construction
// is desired; missing fields cause buildRekeyWiring to return a zero-valued
// rekeyWiring with no error — production deployments missing KEK still boot,
// they just cannot serve Rekey RPCs.
type rekeyWiringDeps struct {
	Pool              *pgxpool.Pool
	KEKProvider       kek.Provider
	GameID            string
	DataDir           string
	AuditChainRepo    chain.Repo
	RekeyAuditEmitter *dek.RekeyAuditEmitter
	CheckpointRepo    *dek.CheckpointRepo
	SubjectResolver   access.SubjectResolver
	SessionStore      adminauth.SessionStore
	RoleChecker       socket.OperatorRoleChecker
	// CoordHolder is the late-bound holder for the cluster invalidation
	// Coordinator. Manager + Orchestrator are constructed before the
	// Coordinator (the Coordinator's Deps.DEKCache/Deps.PartCache MUST
	// reference the Manager's own caches per Phase 3c grounding Decision 5
	// to preserve cross-replica forward secrecy). The Manager's Invalidator
	// closure and the Orchestrator's Phase5Coordinator closure both
	// indirect through this holder; the caller (cmd/holomush/core.go)
	// MUST set holder.coord after construction and BEFORE serving traffic.
	// Required (non-nil) when KEK is wired; the holder itself may carry a
	// nil Coord during a degraded deployment, which surfaces
	// INVALIDATION_NO_LIVE_MEMBERS at call time.
	CoordHolder *coordHolder
	// PolicyHashSrc is the auditchain-backed PolicyHashSource for Phase 1
	// policy_hash freezing.
	PolicyHashSrc dek.PolicyHashSource
}

// coordHolder is the late-bound indirection through which the Manager's
// Invalidator closure and the Orchestrator's Phase5Coordinator reach the
// invalidation.Coordinator. The Coordinator is constructed AFTER the
// Manager (so Deps.DEKCache/PartCache can be sourced from the Manager's
// dek.CacheAccessor — Phase 3c grounding Decision 5), so the closure
// CANNOT capture the Coordinator by value at construction time. The
// holder is set by the caller (cmd/holomush/core.go) after Coordinator
// construction; a nil Coord during the construction window or when the
// deployment cannot wire a Coordinator returns
// INVALIDATION_NO_LIVE_MEMBERS at call time.
type coordHolder struct {
	coord invalidation.Coordinator
}

// buildRekeyWiring constructs the production dek.Manager + Orchestrator +
// admin RekeyHandler. Returns a zero-valued rekeyWiring (no error) when any
// required dependency is unavailable — the caller logs the gap and the admin
// socket falls back to Unimplemented for the Rekey RPCs.
//
// The constructed orchestrator has all five seams installed:
//
//   - SetMaterialResolver: *dek.manager (satisfies MaterialResolver via
//     Resolve + VersionForDEKID)
//   - SetPhase5Coordinator: Phase5CoordinatorFunc wrapping
//     invalidation.Coordinator.RequestInvalidation, falling back to an
//     INVALIDATION_NO_LIVE_MEMBERS error when invCoord is nil
//   - SetDestroyer: *dek.manager (satisfies Destroyer via DestroyDEK +
//     EvictCachedDEK)
//   - SetAuditEmitter: the supplied *dek.RekeyAuditEmitter
//   - SetDataDir: the runtime data directory for INV-CRYPTO-100 fallback log
func buildRekeyWiring(
	_ context.Context,
	deps rekeyWiringDeps,
) (rekeyWiring, error) {
	if deps.Pool == nil || deps.KEKProvider == nil || deps.CheckpointRepo == nil ||
		deps.RekeyAuditEmitter == nil || deps.PolicyHashSrc == nil ||
		deps.SubjectResolver == nil || deps.SessionStore == nil || deps.RoleChecker == nil ||
		deps.CoordHolder == nil {
		// Caller is responsible for logging the gap; we return an empty
		// wiring so the admin socket falls back to Unimplemented and the
		// rest of the server continues to start. CoordHolder is required
		// (non-nil) — the caller MUST construct an empty holder so the
		// Manager's Invalidator closure has a stable address to dereference;
		// the holder.coord field MAY remain nil for the degraded path.
		return rekeyWiring{}, nil
	}

	cacheCfg := dek.CacheConfig{Capacity: 1024, TTL: 5 * time.Minute}
	dekStore := dek.NewStore(deps.Pool)
	dekCache := dek.NewCache(cacheCfg)
	partCache := dek.NewParticipantsCache(cacheCfg)

	// Invalidator closure: dereferences the CoordHolder at call time so
	// the Manager (and Phase5Coordinator) can be constructed BEFORE the
	// Coordinator — the Coordinator's Deps.DEKCache/PartCache MUST share
	// identity with the Manager's caches (Phase 3c grounding doc Decision
	// 5; without that identity, cross-replica invalidation evicts
	// dedicated caches while the Manager's caches keep serving stale OLD
	// DEK material for up to TTL after a Rekey, breaking forward
	// secrecy). When the holder's coord is still nil (degraded deployment
	// or the construction window between Manager build and Coordinator
	// build), short-circuit to a typed error so callers observing the
	// no-cluster shape see the consistent failure rather than a silent
	// success that could mask split-brain risk.
	holder := deps.CoordHolder
	invFn := func(ctx context.Context, ctxID dek.ContextID, action string, version, succVersion uint32) error {
		if holder.coord == nil {
			return oops.Code("INVALIDATION_NO_LIVE_MEMBERS").
				With("context_type", ctxID.Type).
				With("context_id", ctxID.ID).
				With("action", action).
				Errorf("invalidation.Coordinator not wired — cluster fan-out disabled")
		}
		return holder.coord.RequestInvalidation(
			ctx, ctxID, invalidation.Action(action), version, succVersion,
		)
	}

	// BindingResolver: production uses worldpostgres.NewBindingRepository.
	// dek.NewManager requires a non-nil BindingResolver at construction
	// time; the BindingRepository's CurrentWithPlayer method satisfies the
	// interface shape directly. The rekey path itself does not invoke
	// BindingResolver — it is only consumed by Manager.GetOrCreate/Add for
	// character participant management.
	bindings := worldpostgres.NewBindingRepository(deps.Pool)

	mgr, mgrErr := dek.NewManager(deps.KEKProvider, dekStore, dekCache, partCache, invFn, bindings)
	if mgrErr != nil {
		return rekeyWiring{}, oops.Code("CRYPTO_DEK_MANAGER_CONSTRUCT_FAILED").Wrap(mgrErr)
	}

	// Construct the orchestrator and wire all five post-construction seams.
	orch := dek.NewOrchestrator(dekStore, deps.CheckpointRepo, deps.PolicyHashSrc, mgr)

	// MaterialResolver + Destroyer are satisfied by *dek.manager directly.
	// dek.Manager is an interface; the concrete *manager type satisfies
	// these wider interfaces. Type-assert to surface a clear failure if a
	// future refactor changes the concrete type.
	mr, ok := mgr.(dek.MaterialResolver)
	if !ok {
		return rekeyWiring{}, oops.Code("CRYPTO_DEK_MANAGER_MATERIAL_RESOLVER_NOT_SATISFIED").
			Errorf("dek.NewManager return value does not satisfy dek.MaterialResolver — regenerated Manager interface?")
	}
	destroyer, ok := mgr.(dek.Destroyer)
	if !ok {
		return rekeyWiring{}, oops.Code("CRYPTO_DEK_MANAGER_DESTROYER_NOT_SATISFIED").
			Errorf("dek.NewManager return value does not satisfy dek.Destroyer — regenerated Manager interface?")
	}

	orch.SetMaterialResolver(mr)
	orch.SetDestroyer(destroyer)
	orch.SetAuditEmitter(deps.RekeyAuditEmitter)
	orch.SetDataDir(deps.DataDir)
	orch.SetPhase5Coordinator(dek.Phase5CoordinatorFunc(invFn))

	// Wire the admin RekeyHandler with production adapters.
	sessionAdapter := &productionRekeySessionAdapter{store: deps.SessionStore}
	orchAdapter := &productionOrchestratorRunner{orch: orch}
	abortAdapter := &productionRekeyAbortRunner{repo: deps.CheckpointRepo, emitter: deps.RekeyAuditEmitter}
	readerAdapter := &productionCheckpointReader{repo: deps.CheckpointRepo}

	rekeyHandler := socket.NewRekeyHandler(
		sessionAdapter,
		deps.SubjectResolver,
		deps.RoleChecker,
		orchAdapter,
		abortAdapter,
		readerAdapter,
	)
	connectHandler := socket.NewRekeyConnectHandler(rekeyHandler, adminauth.MapDenyToConnect)

	return rekeyWiring{
		Manager:        mgr,
		Orchestrator:   orch,
		CheckpointRepo: deps.CheckpointRepo,
		AuditEmitter:   deps.RekeyAuditEmitter,
		RekeyHandler:   connectHandler,
	}, nil
}

// productionRekeySessionAdapter adapts adminauth.SessionStore to
// socket.RekeySessionStore. Mirrors the harness adapter pattern (the
// in-process .35 harness echoes tokens; this adapter does real session
// lookup against the live adminauth.SessionStore).
type productionRekeySessionAdapter struct {
	store adminauth.SessionStore
}

// GetOperatorSession looks up the session token in the live store. Errors
// are propagated unchanged — the handler treats DENY_SESSION_* codes as
// PermissionDenied via socket.NewRekeyConnectHandler's denyMapper.
func (a *productionRekeySessionAdapter) GetOperatorSession(token string) (socket.OperatorSession, error) {
	if a.store == nil {
		return socket.OperatorSession{}, oops.Code("DENY_SESSION_INVALID").
			Errorf("admin session store not wired")
	}
	identity, err := a.store.Get(token)
	if err != nil {
		return socket.OperatorSession{}, err
	}
	return socket.OperatorSession{
		PlayerID:         identity.PlayerID,
		OSUser:           identity.PeerCredString(),
		TOTPVerified:     identity.TOTPVerified,
		AuthProviderName: identity.AuthProviderName,
	}, nil
}

// productionOrchestratorRunner adapts *dek.Orchestrator to
// socket.OrchestratorRunner. Mirrors the harness adapter but uses the
// production orchestrator.
type productionOrchestratorRunner struct {
	orch *dek.Orchestrator
}

// Run delegates to Orchestrator.Run (fresh-start / auto-resume path) or
// Orchestrator.RunByRequestID (explicit-resume path), converting between the
// socket-layer projection types and the dek-layer types so the socket package
// stays free of dek imports.
//
// Resume path: when req.RequestID is non-zero (RekeyResume RPC), the handler
// supplies only RequestID + Operator + ForceDestroy. RunByRequestID looks up
// the checkpoint by ID, enforces INV-CRYPTO-103 operator-binding, and drives the
// remaining phases to completion.
func (a *productionOrchestratorRunner) Run(ctx context.Context, req socket.RekeyRunRequest) (socket.RekeyRunOutcome, error) {
	operator := dek.OperatorIdentity{
		PlayerID:         req.Operator.PlayerID,
		OSUser:           req.Operator.OSUser,
		TOTPVerified:     req.Operator.TOTPVerified,
		AuthProviderName: req.Operator.AuthProviderName,
	}

	var (
		outcome dek.RekeyOutcome
		err     error
	)

	if req.RequestID != ([16]byte{}) {
		// Explicit-resume path (RekeyResume RPC): delegate to RunByRequestID
		// which loads the checkpoint and bypasses the context-and-args lookup.
		rid := dek.RequestID(req.RequestID)
		outcome, err = a.orch.RunByRequestID(ctx, rid, dek.RekeyRequest{
			Operator:     operator,
			ForceDestroy: req.ForceDestroy,
		})
	} else {
		// Fresh-start / auto-resume path (Rekey RPC).
		outcome, err = a.orch.Run(ctx, dek.RekeyRequest{
			ContextType:   req.ContextType,
			ContextID:     req.ContextID,
			Justification: req.Justification,
			Operator:      operator,
			ForceDestroy:  req.ForceDestroy,
		})
	}
	if err != nil {
		return socket.RekeyRunOutcome{}, err
	}
	return socket.RekeyRunOutcome{
		RequestID:        [16]byte(outcome.RequestID),
		AuditEventID:     [16]byte(outcome.AuditEventID),
		Phase3RowCount:   outcome.Phase3RowCount,
		Phase5Attempts:   outcome.Phase5Attempts,
		ForceDestroyUsed: outcome.ForceDestroyUsed,
		Resumed:          outcome.Resumed,
		DurationMs:       outcome.DurationMs,
	}, nil
}

// productionRekeyAbortRunner adapts *dek.CheckpointRepo + *dek.RekeyAuditEmitter
// to socket.RekeyAbortRunner. INV-CRYPTO-104: abort is single-control regardless of
// dual_control_required policy.
type productionRekeyAbortRunner struct {
	repo    *dek.CheckpointRepo
	emitter *dek.RekeyAuditEmitter
}

// RunAbort marks the checkpoint aborted and emits the abort audit event.
// Audit emit is best-effort: the abort transition is committed even if
// emit fails (a follow-up reconciler should re-emit; out of scope for this
// bead).
func (a *productionRekeyAbortRunner) RunAbort(ctx context.Context, req socket.RekeyAbortRequest) (socket.RekeyAbortOutcome, error) {
	rid := dek.RequestID(req.RequestID)
	abortReason := "operator:" + req.PlayerID
	if err := a.repo.MarkAborted(ctx, rid, abortReason); err != nil {
		return socket.RekeyAbortOutcome{}, err
	}
	ckpt, err := a.repo.Get(ctx, rid)
	if err != nil {
		return socket.RekeyAbortOutcome{}, err
	}
	abortedAt := time.Now().UTC()
	if ckpt.AbortedAt != nil {
		abortedAt = *ckpt.AbortedAt
	}
	payload := dek.RekeyAuditPayload{
		RequestID:       rid.String(),
		Context:         dek.RekeyAuditContext{Type: ckpt.ContextType, ID: ckpt.ContextID},
		OldDEK:          dek.RekeyAuditDEK{ID: ckpt.OldDEKID},
		PrimaryOperator: dek.RekeyAuditOp{PlayerID: req.PlayerID},
		Justification:   "aborted by operator",
		StartedAt:       ckpt.StartedAt,
		CompletedAt:     abortedAt,
		SpecVersion:     "abort",
	}
	auditID, _, emitErr := a.emitter.Emit(ctx, payload)
	if emitErr != nil {
		// Non-fatal: abort is committed; audit emit is best-effort.
		// Log loudly so an operator can investigate.
		slog.ErrorContext(ctx, "rekey abort audit emit failed", "request_id", rid.String(), "error", emitErr.Error())
		auditID = ulid.ULID{}
	}
	return socket.RekeyAbortOutcome{
		AbortedAt:    abortedAt,
		AuditEventID: [16]byte(auditID),
	}, nil
}

// productionCheckpointReader adapts *dek.CheckpointRepo to
// socket.CheckpointStatusReader. Mirrors the harness adapter shape, projecting
// dek.Checkpoint rows into the socket-layer CheckpointView.
type productionCheckpointReader struct {
	repo *dek.CheckpointRepo
}

// GetCheckpoint reads a single checkpoint row.
func (a *productionCheckpointReader) GetCheckpoint(ctx context.Context, rid [16]byte) (socket.CheckpointView, error) {
	ckpt, err := a.repo.Get(ctx, dek.RequestID(rid))
	if err != nil {
		return socket.CheckpointView{}, err
	}
	return checkpointToView(rid, ckpt), nil
}

// ListCheckpoints returns up to filter.Limit non-terminal (or all, when
// IncludeTerminal) checkpoints matching the filter. The caller already caps
// limit ≤100 (see socket.RekeyHandler.RekeyList).
func (a *productionCheckpointReader) ListCheckpoints(ctx context.Context, filter socket.CheckpointListFilter) ([]socket.CheckpointView, error) {
	rows, err := a.repo.ListFiltered(ctx, dek.CheckpointListFilter{
		IncludeTerminal: filter.IncludeTerminal,
		ContextPattern:  filter.ContextPattern,
		Since:           filter.Since,
		Limit:           filter.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]socket.CheckpointView, 0, len(rows))
	for i := range rows {
		out = append(out, checkpointToView([16]byte(rows[i].RequestID), rows[i]))
	}
	return out, nil
}

// checkpointToView projects a dek.Checkpoint into a socket.CheckpointView.
// Mirrors the harness ckptToView helper; lives here to keep the wiring file
// self-contained and the harness untouched.
func checkpointToView(rid [16]byte, ckpt dek.Checkpoint) socket.CheckpointView {
	members, _ := ckpt.Phase5MissingMembers() //nolint:errcheck // nil on decode failure is safe; callers treat nil == empty
	return socket.CheckpointView{
		RequestID:            rid,
		ContextType:          ckpt.ContextType,
		ContextID:            ckpt.ContextID,
		Status:               string(ckpt.Status),
		PrimaryPlayerID:      ckpt.PrimaryPlayerID,
		StartedAt:            ckpt.StartedAt,
		LastHeartbeatAt:      ckpt.LastHeartbeatAt,
		CompletedAt:          ckpt.CompletedAt,
		Phase5AttemptCount:   ckpt.Phase5AttemptCount,
		Phase5MissingMembers: members,
		ForceDestroy:         ckpt.ForceDestroy,
		OldDEKID:             ckpt.OldDEKID,
		NewDEKID:             ckpt.NewDEKID,
	}
}
