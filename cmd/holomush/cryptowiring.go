// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/samber/oops"

	abacsetup "github.com/holomush/holomush/internal/access/setup"
	"github.com/holomush/holomush/internal/admin/approval"
	adminauth "github.com/holomush/holomush/internal/admin/auth"
	"github.com/holomush/holomush/internal/admin/policy"
	socket "github.com/holomush/holomush/internal/admin/socket"
	totpaudit "github.com/holomush/holomush/internal/admin/totp_audit"
	authsetup "github.com/holomush/holomush/internal/auth/setup"
	"github.com/holomush/holomush/internal/cluster"
	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/invalidation"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/totp"
)

// cryptoWiring bundles the outputs of the hoisted crypto/admin wiring block
// (core.go:705-1060 pre-plan) that five lifecycle subsystems' Start methods
// consume via consumer-owned providers (07-09 D-12 Wave A). It is a plain
// package-main struct — NOT a lifecycle.Subsystem (no ID()/DependsOn(), never
// registered with the orchestrator; the subsystem count stays 17).
//
// No package outside cmd/holomush names this type — every internal consumer
// config takes a narrow, consumer-owned provider (policy.EmitDepsProvider,
// dek.CheckpointSweepConfig.DepsProvider, chain.VerifierSubsystemConfig.
// RepoProvider/HandlersProvider, socket.AdminSocketSubsystemConfig.
// HandlersProvider), and the closures projecting fields off this struct live
// in runCore.
type cryptoWiring struct {
	policyEmitDeps         policy.EmitDeps
	checkpointRepo         *dek.CheckpointRepo
	checkpointAuditEmitter dek.AuditEmitter
	chainRepo              chain.Repo
	chainHandlers          []chain.Handler
	adminHandlers          socket.Handlers
	// rekeyManager is the production dek.Manager (nil when KEK is
	// unavailable). grpcSubsystem is the sole consumer that reads this
	// field directly off the resolved wiring.
	rekeyManager dek.Manager
}

// cryptoWiringInputs bundles everything resolveCryptoWiring's memoized
// builder needs, captured once in runCore and closed over. Every field here
// is either a subsystem handle (resolved lazily, inside the builder, after
// its own subsystem has started) or a pure value computed before StartAll.
type cryptoWiringInputs struct {
	DB       *store.DatabaseSubsystem
	Auth     *authsetup.AuthSubsystem
	ABAC     *abacsetup.ABACSubsystem
	EventBus *eventbus.Subsystem
	Cluster  cluster.Registry

	// GameID resolves the game ID — the same single gameIDProvider closure
	// every other consumer in this plan resolves through.
	GameID func() string

	DataDir    string
	AutoGenKEK bool

	// CryptoCfg is cryptoConfig.Defaults() — already applied in runCore
	// (pure; no live reads), reused here for RekeyCheckpoint*/OperatorRead*
	// fields.
	CryptoCfg config.CryptoConfig

	// ValidatedDualControl is the pure-config-filtered
	// crypto.dual_control_required list (validateDualControlRequired runs
	// in runCore, before the hoist — it reads no live value).
	ValidatedDualControl []string

	VerbRegistry *core.VerbRegistry

	// MetricsRegisterer is the served Prometheus registry (obsServer's, or
	// the default when metrics are disabled) — the same registry the
	// cluster metrics above register on, so invalidation_* metrics are
	// scraped rather than silently orphaned.
	MetricsRegisterer prometheus.Registerer

	// CoordHolder is the late-bound holder for the invalidation.Coordinator
	// (see crypto_rekey_wiring.go's coordHolder doc). The builder sets
	// CoordHolder.coord as a side effect of a successful Coordinator
	// construction+Start (07-09 item 4).
	//
	// 07-11 note: the FIRST caller of the memoized builder in the running
	// system is whichever wiring consumer's Prepare is scheduled earliest in
	// topological order — CryptoChainVerifier is required to prepare before
	// grpcSubsystem (pinned by cmd/holomush/core_topo_order_test.go), so it
	// is frequently that consumer, not grpcSubsystem. This is unchanged
	// behavior from pre-07-11 (the "grpcSub is the first caller" framing in
	// the code predates the Prepare/Activate split and was already
	// inaccurate under the single-phase topology). Construction+Start
	// deliberately stay bundled inside this builder rather than split
	// across phases: whichever Prepare call triggers it, the build —
	// including the Coordinator's Start() — runs during the Prepare sweep,
	// strictly before any Activate, so D-13.0's barrier (no
	// externally-reachable DOMAIN surface or host-owned domain work loop
	// before every subsystem has prepared) still holds. The Coordinator's
	// own pub/sub is process/cluster-internal invalidation signaling over
	// the cluster's already-prepared connection, not client-facing domain
	// traffic — the same class of exception D-13.0 grants eventbus's whole
	// Prepare body.
	CoordHolder *coordHolder
}

// resolveCryptoWiring returns a memoized func() (*cryptoWiring, error). The
// sync.Once body runs AT MOST ONCE per process: the first consumer to call
// the returned func builds the wiring (constructing dek.Manager, the admin
// handlers, the audit-chain repo/handlers, and — as a side effect — the
// invalidation.Coordinator), and every subsequent call returns the SAME
// (*cryptoWiring, error) pair. A failed build is therefore never retried
// within one process: StartAll aborts the boot on the first consumer's
// error anyway, and a retrying memoizer would let consumers observe
// divergent wiring.
//
// THE RULE: every subsystem whose config holds a provider backed by this
// builder MUST declare DependsOn ⊇ {SubsystemDatabase, SubsystemAuth,
// SubsystemABAC, SubsystemEventBus} — the FIRST consumer to resolve the
// provider is the one that builds it, and a missing edge is a boot panic
// (dbSub.Pool()/authSub.Hasher()/abacSub.Resolver()/eventBusSub.Conn() all
// panic-guard before their subsystem's Start has run).
//
// This is not a second ordering authority (D-10): the builder does no
// ordering of its own — it is lazy memoization. Ordering remains topoSort +
// DependsOn.
func resolveCryptoWiring(in cryptoWiringInputs) func() (*cryptoWiring, error) {
	var (
		once     sync.Once
		result   *cryptoWiring
		buildErr error
	)
	return func() (*cryptoWiring, error) {
		once.Do(func() {
			result, buildErr = buildCryptoWiring(in)
		})
		return result, buildErr
	}
}

// buildCryptoWiring is the hoisted body of the former core.go:705-1060
// block. Every dbSub.Pool()/authSub.Hasher()/authSub.AuthService()/
// abacSub.Resolver()/eventBusSub.Conn()/eventBusSub.Publisher() read below
// executes here, inside the memoized builder — never on runCore's
// straight-line path. The reads themselves are unchanged from the
// pre-hoist block; only their execution point moved (from eager, at
// wiring time, to lazy, at the first consumer's Start).
func buildCryptoWiring(in cryptoWiringInputs) (*cryptoWiring, error) {
	ctx := context.Background()

	gameID := in.GameID()

	// auditchain.VerifierSubsystem walks every registered hash chain at
	// boot. RekeyChain wired here per Task 19; OperatorReadChain wired per
	// sub-epic F R.14 — computed below, after readstream wiring runs, so we
	// can append readstream.OperatorReadHandlerFor(gameID) to the handler set.
	dek.SetGameIDForRekey(gameID) // must be set before RekeyHandlerFor is called below.
	auditChainRepo := chain.NewPostgresRepo(in.DB.Pool())

	// Mint a server-start ULID to stamp the CryptoPolicySubsystem's emits so
	// a given run's events can be correlated in events_audit.
	serverStartULID := core.NewULID().String()

	// Derive server identity: prefer hostname, fall back to "holomush".
	serverIdentity := "holomush"
	if hostname, hostErr := os.Hostname(); hostErr == nil && hostname != "" {
		serverIdentity = "holomush@" + hostname
	}

	effectiveConfig := policy.CryptoEffectiveConfig{
		DualControlRequired: in.ValidatedDualControl,
	}

	// Wrap the bare EventBus publisher with RenderingPublisher so the
	// host-emit audit publishers stamp the App-Rendering NATS header
	// required by audit/projection.go::persist (headerRendering check).
	// Without this wrapping the projection rejects every host-emit audit
	// event with AUDIT_MISSING_HEADER and they never reach events_audit.
	// (holomush-jxo8.6.26 / INV-CRYPTO-81, INV-CRYPTO-84.)
	auditPublisher := eventbus.NewRenderingPublisher(in.EventBus.Publisher(), in.VerbRegistry)

	policyEmitDeps := policy.EmitDeps{
		GameID:          gameID,
		ServerStartULID: serverStartULID,
		ServerIdentity:  serverIdentity,
		Pool:            in.DB.Pool(),
		Publisher:       auditPublisher,
		Clock:           totp.NewRealClock(),
		Config:          effectiveConfig,
	}

	// --- Phase 5 sub-epic E rekey wiring (holomush-jxo8.7.34 / T37) ---
	//
	// Constructs the rekey audit emitter, the two components that can stand
	// alone without the full DEK manager stack.
	rekeyCheckpointRepo := dek.NewCheckpointRepo(in.DB.Pool())
	rekeyAuditEmitter := dek.NewRekeyAuditEmitter(
		chain.NewEmitter(auditChainRepo),
		&rekeyAuditPublisherAdapter{publisher: auditPublisher, clock: totp.NewRealClock()},
	)
	// policyHashSource backs the Phase 1 policy_hash freeze (INV-CRYPTO-112).
	policyHashSrc := newPolicyHashSourceFromAuditChain(auditChainRepo, gameID)

	// --- Admin handler construction (T22 / holomush-jxo8.6.21) ---
	//
	// Build the in-memory session store for Authenticate → Approve / ResetTOTP flow.
	// totp.NewRealClock() satisfies adminauth.Clock (both require Now() time.Time).
	adminSessionStore := adminauth.NewSessionStore(totp.NewRealClock(), 10*time.Minute)

	// Provision the boot KEK provider: a KEK is REQUIRED to start. The keyfile
	// path comes from HOLOMUSH_KEK_FILE and the passphrase from env / file-ref /
	// prompt (resolvePassphrase); --auto-gen-kek mints the keyfile on first boot.
	// Any provisioning failure is fatal (BOOT_KEK_REQUIRED) — there is no
	// degraded KEK-less mode. The admin-totp CLI keeps its own stricter path
	// (buildKEKProviderFromConfig: pre-existing keyfile only, never auto-gen).
	kekProvider, kekErr := provisionBootKEKProvider(ctx, in.DB.Pool(), in.AutoGenKEK)
	if kekErr != nil {
		return nil, oops.Code("BOOT_KEK_REQUIRED").
			Errorf("a KEK is required to start: %w (set %s + a passphrase source, or pass --auto-gen-kek)", kekErr, envKEKFile)
	}

	var adminTOTPSvc totp.Service
	if kekProvider != nil {
		totpRepo := totp.NewRepository(in.DB.Pool())
		builtTOTP, totpErr := totp.NewService(
			totp.Config{GameID: gameID},
			totpRepo,
			kekProvider,
			totp.NewRealClock(),
			in.Auth.Hasher(),
		)
		if totpErr != nil {
			slog.WarnContext(ctx, "admin handlers: TOTP service construction failed — admin TOTP RPCs will be unavailable at runtime",
				"error", totpErr)
		} else {
			adminTOTPSvc = builtTOTP
		}
	}

	// AuditingService wraps the totp.Service to emit crypto.totp_* events.
	// When adminTOTPSvc is nil (KEK unavailable), totpAuditSvc is also nil and
	// the admin handlers will return errors on any TOTP operation.
	var totpAuditSvc *totpaudit.AuditingService
	if adminTOTPSvc != nil {
		builtAudit, auditErr := totpaudit.NewAuditingService(
			adminTOTPSvc,
			auditPublisher,
			gameID,
			totp.NewRealClock(),
			slog.Default(),
		)
		if auditErr != nil {
			slog.WarnContext(ctx, "admin handlers: TOTP audit service construction failed", "error", auditErr)
		} else {
			totpAuditSvc = builtAudit
		}
	}

	// Build the in-game credentials provider that walks the 6-step auth sequence.
	adminRoleStore := store.NewPostgresRoleStore(in.DB.Pool())

	var authenticateHandler socket.AuthenticateHandler
	var approveHandler socket.ApproveHandler
	var resetTOTPHandler socket.ResetTOTPHandler

	if totpAuditSvc != nil {
		ingameProvider, provErr := adminauth.NewInGameCredentialsProvider(
			in.Auth.AuthService(),
			totpAuditSvc,
			in.ABAC.Resolver(),
			adminRoleStore,
		)
		if provErr != nil {
			slog.WarnContext(ctx, "admin handlers: InGameCredentialsProvider construction failed — Authenticate will be unavailable",
				"error", provErr)
		} else {
			approvalRepo := approval.NewPostgresRepo(in.DB.Pool(), nil)
			authenticateHandler = adminauth.NewAuthenticateHandler(ingameProvider, adminSessionStore)
			approveHandler = approval.NewApproveHandler(adminSessionStore, approvalRepo, in.ABAC.Resolver(), adminRoleStore)
			resetTOTPHandler = adminauth.NewResetTOTPHandler(adminSessionStore, in.ABAC.Resolver(), adminRoleStore, totpAuditSvc)
		}
	} else {
		slog.WarnContext(ctx, "admin handlers: TOTP audit service unavailable — all three admin RPCs (Authenticate/Approve/ResetTOTP) will return errors")
	}

	// --- Phase 5 sub-epic E T44: production dek.Manager + rekey RPC wiring ---
	//
	// Construction order is load-bearing per Phase 3c grounding doc
	// Decision 5: the invalidation.Coordinator's Deps.DEKCache and
	// Deps.PartCache MUST share pointer identity with the dek.Manager's own
	// caches. Without that identity, the receive-side handler evicts
	// dedicated caches while the Manager's caches keep serving stale OLD DEK
	// material for up to the cache TTL (~5min) after a cross-replica Rekey —
	// a forward-secrecy regression on the master crypto spec.
	rekeyW, rekeyWErr := buildRekeyWiring(ctx, rekeyWiringDeps{
		Pool:              in.DB.Pool(),
		KEKProvider:       kekProvider,
		GameID:            gameID,
		DataDir:           in.DataDir,
		AuditChainRepo:    auditChainRepo,
		RekeyAuditEmitter: rekeyAuditEmitter,
		CheckpointRepo:    rekeyCheckpointRepo,
		SubjectResolver:   in.ABAC.Resolver(),
		SessionStore:      adminSessionStore,
		RoleChecker:       adminRoleStore,
		CoordHolder:       in.CoordHolder,
		PolicyHashSrc:     policyHashSrc,
	})
	if rekeyWErr != nil {
		// Construction failure is fatal because it means the production
		// shape is incoherent (Manager interface satisfied wrong, etc.). A
		// missing dependency returns a zero wiring with nil error, NOT this
		// branch.
		return nil, rekeyWErr
	}
	if rekeyW.RekeyHandler == nil {
		slog.WarnContext(ctx, "rekey wiring incomplete — Rekey admin RPCs will return Unimplemented",
			"kek_available", kekProvider != nil)
	}

	// Build the invalidation.Coordinator USING the Manager's own caches
	// (Phase 3c grounding doc Decision 5). Then set CoordHolder.coord so the
	// Manager's Invalidator closure and the Orchestrator's Phase5Coordinator
	// pick up the live Coordinator on their next call. Only proceed if the
	// Manager was successfully constructed (rekeyW.Manager non-nil) —
	// otherwise the Coordinator has no caches to bind to.
	//
	// grpcSubsystem owns the Coordinator's lifecycle for STOP purposes
	// (07-09 item 4: grpcSubsystem.Stop drives Coordinator.Stop via
	// CoordHolder). Construction + Start() happen HERE, inside this
	// memoized builder, as a side effect of the first call to it — which
	// may be any wiring consumer's Prepare, not necessarily grpcSubsystem's
	// (see cryptoWiringInputs.CoordHolder's doc for why this is
	// deliberately unchanged from pre-07-11 and still D-13.0-compliant).
	if rekeyW.Manager != nil {
		accessor, accessorOK := rekeyW.Manager.(dek.CacheAccessor)
		if !accessorOK {
			// Fail closed: a regenerated Manager that drops the accessor
			// would silently revert to the dedicated-caches regression.
			return nil, oops.Code("CRYPTO_DEK_MANAGER_CACHE_ACCESSOR_NOT_SATISFIED").
				Errorf("dek.NewManager return value does not satisfy dek.CacheAccessor — required for invalidation.Coordinator wiring per Phase 3c grounding Decision 5")
		}
		// Same scraped-registry rationale as the cluster metrics in runCore.
		invMetrics := invalidation.NewMetrics(in.MetricsRegisterer)
		c, invErr := invalidation.New(invalidation.Config{ClusterID: gameID}, invalidation.Deps{
			Conn:      in.EventBus.Conn(),
			Registry:  in.Cluster,
			DEKCache:  accessor.Cache(),
			PartCache: accessor.PartCache(),
			Logger:    slog.Default(),
			Metrics:   invMetrics,
		})
		if invErr != nil {
			slog.WarnContext(ctx, "invalidation.Coordinator construction failed — cluster fan-out unavailable; Rekey will surface INVALIDATION_NO_LIVE_MEMBERS",
				"error", invErr)
		} else if startErr := c.Start(ctx); startErr != nil {
			slog.WarnContext(ctx, "invalidation.Coordinator start failed — cluster fan-out unavailable",
				"error", startErr)
		} else {
			in.CoordHolder.coord = c
			// Stop is owned by grpcSubsystem.Stop (07-09 item 4) — no
			// ad-hoc defer here; the orchestrator now drives shutdown
			// ordering (and deadline handling, 07-10) for the Coordinator
			// just like every other subsystem.
		}
	}

	// --- Phase 5 sub-epic F R.14 AdminReadStream wiring (holomush-jxo8.8.38) ---
	//
	// WARN-log on OperatorReadMaxWindow > 90d (operators can configure
	// values above the cap, but oversized windows greatly inflate row-count
	// and memory pressure on the cold-tier reader).
	const operatorReadMaxWindowWarnThreshold = 90 * 24 * time.Hour
	if in.CryptoCfg.OperatorReadMaxWindow > operatorReadMaxWindowWarnThreshold {
		slog.WarnContext(ctx, "admin readstream: OperatorReadMaxWindow > 90d — oversized windows greatly inflate cold-tier read row-count and memory pressure",
			"configured", in.CryptoCfg.OperatorReadMaxWindow,
			"threshold", operatorReadMaxWindowWarnThreshold)
	}

	readStreamW, readStreamWErr := buildReadStreamWiring(ctx, readStreamWiringDeps{
		Pool:            in.DB.Pool(),
		GameID:          gameID,
		AuditChainRepo:  auditChainRepo,
		AuditPublisher:  auditPublisher,
		SubjectResolver: in.ABAC.Resolver(),
		SessionStore:    adminSessionStore,
		DEKManager:      rekeyW.Manager,
		PolicyHashSrc:   policyHashSrc,
		MaxWindow:       in.CryptoCfg.OperatorReadMaxWindow,
		DefaultWindow:   in.CryptoCfg.OperatorReadDefaultWindow,
		WriteDeadline:   in.CryptoCfg.OperatorReadWriteDeadline,
		ApprovalTTL:     in.CryptoCfg.OperatorReadApprovalTTL,
	})
	if readStreamWErr != nil {
		// Construction failure is fatal because it means a required
		// invariant is incoherent (e.g., handler.Validate rejected the
		// config). A missing dependency returns a zero wiring with nil
		// error, NOT this branch.
		return nil, readStreamWErr
	}
	if readStreamW.Handler == nil {
		slog.WarnContext(ctx, "admin readstream wiring incomplete — AdminReadStream RPC will return Unimplemented",
			"dek_manager_available", rekeyW.Manager != nil)
	}

	// Construct the audit-chain handler set now that we know which chains
	// the host registered. operator_read joins the static policy_set + rekey
	// pair when readstream wiring succeeded; otherwise the chain stays
	// unregistered (its DB rows would still be visible to a future verifier
	// run, but boot-time integrity is skipped — matching the "no events yet"
	// shape).
	chainHandlers := []chain.Handler{
		policy.PolicySetHandlerFor(gameID),
		dek.RekeyHandlerFor(gameID),
	}
	if readStreamW.Handler != nil {
		chainHandlers = append(chainHandlers, readStreamW.AuditChainHandler)
	}

	return &cryptoWiring{
		policyEmitDeps:         policyEmitDeps,
		checkpointRepo:         rekeyCheckpointRepo,
		checkpointAuditEmitter: rekeyAuditEmitter,
		chainRepo:              auditChainRepo,
		chainHandlers:          chainHandlers,
		adminHandlers: socket.Handlers{
			Authenticate: authenticateHandler,
			Approve:      approveHandler,
			ResetTOTP:    resetTOTPHandler,
			Rekey:        rekeyW.RekeyHandler,
			ReadStream:   readStreamW.Handler,
		},
		rekeyManager: rekeyW.Manager,
	}, nil
}
