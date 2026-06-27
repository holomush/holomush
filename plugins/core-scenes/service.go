// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// sceneStorer is the persistence interface required by SceneServiceImpl.
// Defined here so the service layer is not coupled to the concrete
// SceneStore type — tests can substitute a fake implementation.
//
// Phase 1: Create + Get
// Phase 2: + End, Pause, Resume, Update — all return the post-update row
//
//	via Postgres RETURNING so the service handler doesn't need a
//	separate Get call (eliminates a class of races).
type sceneStorer interface {
	Create(ctx context.Context, row *SceneRow) error
	CreateWithOwner(ctx context.Context, row *SceneRow) error
	Get(ctx context.Context, id string) (*SceneRow, error)
	GetWithMembership(ctx context.Context, id string) (*SceneRow, []string, []string, error)
	// GetWithMembershipAndObservers extends GetWithMembership with a fourth
	// slice of observer character IDs (role='observer'). Used to populate
	// SceneInfo.Observers (INV-SCENE-61). The participants filter is unchanged.
	GetWithMembershipAndObservers(ctx context.Context, id string) (*SceneRow, []string, []string, []string, error)
	// AddObserver inserts a role=observer row for an open, active/paused scene.
	// Returns ObserverAlreadyParticipant unchanged when the character already
	// has any row. Returns ObserverSceneNotOpen/NotActive/NotFound without error
	// when the scene gates reject the request.
	AddObserver(ctx context.Context, sceneID, characterID string) (*ParticipantRow, ObserverAddResult, error)
	End(ctx context.Context, id string) (*SceneRow, error)
	Pause(ctx context.Context, id string) (*SceneRow, error)
	Resume(ctx context.Context, id string) (*SceneRow, error)
	Update(ctx context.Context, id string, update *SceneUpdate) (*SceneRow, error)
	AddParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, ParticipantOpResult, error)
	RemoveParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, error)
	InviteParticipant(ctx context.Context, sceneID, inviterID, targetID string) (*ParticipantRow, error)
	KickParticipant(ctx context.Context, sceneID, kickerID, targetID string) (*ParticipantRow, error)
	TransferOwnership(ctx context.Context, sceneID, currentOwnerID, newOwnerID string) error
	ListParticipants(ctx context.Context, sceneID string) ([]ParticipantRow, error)
	GetParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, error)
	// IsParticipant returns true if the character is a current participant
	// of the scene with role "owner" or "member" (NOT "invited"). Used by
	// the INV-SCENE-60 plugin-code gate at GetPoseOrder per ADR holomush-nt2d.
	// Returns (false, nil) if the character is not a participant — no
	// distinction from "not found"; the gate's contract is binary.
	IsParticipant(ctx context.Context, sceneID, characterID string) (bool, error)
	// ListParticipantsWithPoseMeta fetches scenes.total_pose_count and the
	// per-participant pose metadata (last_pose_at, last_pose_seq) for all
	// owner+member rows in a single SELECT. Excludes invited role per
	// INV-SCENE-60 pose-order-only-for-participants discipline. Pinned by spec
	// §6.1 / INV-SCENE-7. See ADR holomush-r4th (denormalize pose-order metadata).
	ListParticipantsWithPoseMeta(ctx context.Context, sceneID string) (ParticipantsWithPoseMeta, error)
	// ListScenesForCharacter returns the scene IDs the character is
	// currently a participant of (role IN ('owner', 'member'), excluding
	// 'invited') for scenes in state IN ('active', 'paused'). Used by
	// handleEmit's single-membership inference per spec §5.2 (Phase 5 will
	// replace with focus-aware routing).
	ListScenesForCharacter(ctx context.Context, characterID string) ([]string, error)
	// ListBoard returns the paginated public scene board: open scenes in state
	// 'active' or 'paused', optionally filtered by tags. CW and identity
	// filtering are applied by the caller (iokti.13). The icSubjectPrefix is
	// "events.<gameID>.scene." — used for the last_activity_ms correlated
	// subquery; pass empty string when activity timestamps are not needed.
	ListBoard(ctx context.Context, q BoardQuery, icSubjectPrefix string) ([]*SceneRow, error)
	// Phase 6 publication reads used by the publish-vote handlers. The
	// header read deliberately EXCLUDES content_entries so the INV-SCENE-60
	// participant gate runs between the header read and the content read
	// (INV-SCENE-32). Implemented by *SceneStore in publish_store.go.
	GetPublishedSceneHeader(ctx context.Context, id string) (*PublishedScene, error)
	GetPublishedSceneContent(ctx context.Context, id string) ([]PublishedSceneEntry, error)
	TallyVotes(ctx context.Context, publishedSceneID string) (*VoteTally, error)
	// CastVote upserts a roster member's vote (is_change tracking) and
	// TransitionStatus applies a state-machine transition with its side-effect
	// fields. Both back CastPublishSceneVote + applyTrigger (B3).
	CastVote(ctx context.Context, publishedSceneID, characterID string, vote bool) (*CastVoteResult, error)
	TransitionStatus(ctx context.Context, id string, in TransitionInput) error
	// StartScenePublish preconditions: attempt budget + one-and-done checks
	// (CountAttempts), the per-scene max-attempts read
	// (GetSceneMaxPublishAttempts), and the transactional attempt+roster
	// create (CreatePublishAttempt). Implemented by *SceneStore in
	// publish_store.go.
	CountAttempts(ctx context.Context, sceneID string) (AttemptCounts, error)
	CreatePublishAttempt(ctx context.Context, in CreatePublishAttemptInput) (*PublishedScene, error)
	GetSceneMaxPublishAttempts(ctx context.Context, sceneID string) (int, error)
	// ListPublishVoters returns all voter rows for a publish attempt. Used by
	// the Phase D event emitter to build the roster snapshot for
	// scene_publish_started. Implemented by *SceneStore in publish_store.go.
	ListPublishVoters(ctx context.Context, publishedSceneID string) ([]PublishedSceneVote, error)
	// ListSceneAttempts returns all publish attempts for a scene (header only,
	// no content_entries), ordered by attempt_number. Used by the
	// participant-gated ListScenePublishAttempts audit list (B7).
	ListSceneAttempts(ctx context.Context, sceneID string) ([]PublishedScene, error)
	// ExtendMaxPublishAttempts bumps a scene's max_publish_attempts by
	// `additional` and returns the new budget. Backs the admin-only
	// ExtendScenePublishVoteAttempts RPC (E1).
	ExtendMaxPublishAttempts(ctx context.Context, sceneID string, additional int) (int, error)
	// ListCharacterScenes returns every non-archived scene the character has a
	// participant row in (any role), with activity aggregates. The
	// icSubjectPrefix is "events.<gameID>.scene." — supplied by the service.
	// Ordered by last_activity_ms DESC.
	ListCharacterScenes(ctx context.Context, characterID, icSubjectPrefix string) ([]CharacterSceneResult, error)
	// ListPublishedScenes returns PUBLISHED archive summaries newest first,
	// with optional tag filtering and LIMIT/OFFSET paging.
	ListPublishedScenes(ctx context.Context, q ListPublishedScenesQuery) ([]PublishedSceneArchiveSummary, error)

	// ReadSceneLogForExport reads the IC log for a scene in chronological order
	// (ORDER BY id ASC) without a transaction — used by ExportSceneLog where
	// snapshot-level consistency is not required. fullSubject is the complete
	// NATS dot-style IC subject (events.<game_id>.scene.<scene_id>.ic) and is
	// matched with an exact WHERE subject = $1 (not LIKE). Returns at most
	// exportLogMaxRows rows; returns SCENE_EXPORT_TOO_LARGE (FailedPrecondition)
	// when the log exceeds that ceiling rather than silently truncating.
	ReadSceneLogForExport(ctx context.Context, fullSubject string) ([]LogRow, error)

	// ── C7 snapshot pipeline (COOLOFF→PUBLISHED) ──────────────────────────
	// SnapshotPool exposes the connection pool so runSnapshot can orchestrate
	// the read-tx → decrypt(outside tx) → write-tx sequence (read-back design
	// §6). The remaining methods are tx-scoped so the lock + re-validate +
	// MarkPublished + archive run in one write transaction (INV-CRYPTO-33).
	SnapshotPool() *pgxpool.Pool
	LockForSnapshot(ctx context.Context, tx pgx.Tx, id string) (*PublishedScene, error)
	TallyVotesTx(ctx context.Context, tx pgx.Tx, publishedSceneID string) (*VoteTally, error)
	ReadSceneLogForSnapshot(ctx context.Context, tx pgx.Tx, subject string) ([]LogRow, error)
	ReadSceneMetaForSnapshot(ctx context.Context, tx pgx.Tx, sceneID string) (SnapshotSceneMeta, error)
	MarkPublished(ctx context.Context, tx pgx.Tx, id string, in MarkPublishedInput) error
	ArchiveSceneStateForPublish(ctx context.Context, tx pgx.Tx, sceneID string) (bool, error)
	FailAttemptTx(ctx context.Context, tx pgx.Tx, id string, reason PublishFailureReason) error
}

// SceneServiceImpl implements scenev1.SceneServiceServer for Phase 1.
//
// The store field is wired by main()'s Init via direct field assignment
// after NewSceneStore returns. The pre-allocated zero-value SceneServiceImpl
// is registered with the gRPC server in RegisterServices, before Init is
// called, so the field assignment in Init wires the store after RegisterServices.
type SceneServiceImpl struct {
	scenev1.UnimplementedSceneServiceServer
	store     sceneStorer
	eventSink pluginsdk.EventSink
	gameID    string // per substrate INV-EVENTBUS-28. Defaults to "main"; wired in Init.
	// Phase 6 publish-vote machinery.
	cfg    SceneServiceConfig // game-wide vote/cool-off defaults.
	events publishEventer     // scene_publish_* notice emitter; noop until Phase D wires the real one.
	// decryptor is the host-mediated read-back decrypt seam used by the
	// COOLOFF→PUBLISHED snapshot pipeline (C7). nil until SetSnapshotDecryptor
	// wires it at Init; runSnapshot fails closed when nil. The plugin never
	// holds a DEK — it submits ciphertext rows and receives plaintext (INV-CRYPTO-26).
	decryptor snapshotDecryptor
	// settings is the SDK-injected host settings client used by
	// effectiveTaxonomy to read the game-scope "content.cw_taxonomy" override.
	// nil until SetSettingsClient wires it; effectiveTaxonomy falls back to
	// DefaultCWTaxonomy when nil (INV-SCENE-57).
	settings pluginsdk.SettingsClient
	// evaluator is the host ABAC evaluator used by WatchScene's spectate gate
	// and the lifecycle handlers' end/pause/resume gates. nil until
	// scenePlugin.SetHostEvaluator forwards it; all gated handlers fail closed
	// when nil (mirrors handleEmit's nil-evaluator handling).
	evaluator pluginsdk.HostEvaluator
	// focusClient drives session focus state for service-owned RPCs
	// (WatchScene registers the watcher's scene FocusMembership). nil until
	// scenePlugin.SetFocusClient forwards it; WatchScene fails closed when nil.
	focusClient pluginsdk.FocusClient
}

// NewSceneServiceImpl returns a service backed by the given store.
// main() constructs the service directly with a nil store and assigns it
// after Init. The gameID defaults to "main" matching the substrate default
// (see internal/grpc/server.go:181). A no-op publish eventer is seeded so
// Phase B handlers compile before Phase D wires the real one.
//
// Config (vote/cool-off windows) is NOT seeded here — applyConfig (called
// from Init and from newTestService in tests) is the sole config source so
// production and tests both derive windows from the manifest (INV-PLUGIN-7).
func NewSceneServiceImpl(store sceneStorer) *SceneServiceImpl {
	return &SceneServiceImpl{
		store:  store,
		gameID: "main",
		events: noopPublishEventer{},
	}
}

// SetEventSink installs the host callback event sink used for service-owned
// emissions from the binary plugin.
func (s *SceneServiceImpl) SetEventSink(sink pluginsdk.EventSink) {
	s.eventSink = sink
}

// SetPublishEventer installs the Phase 6 scene_publish_* notice emitter.
// Phase D (Task D2) calls this with the real publishEventEmitter; until then
// the constructor's noopPublishEventer absorbs every emit.
func (s *SceneServiceImpl) SetPublishEventer(e publishEventer) {
	s.events = e
}

// SetSnapshotDecryptor installs the host-mediated read-back decryptor used by
// the COOLOFF→PUBLISHED snapshot pipeline (C7). Wired at Init from the SDK's
// SnapshotDecryptorAware injection; tests substitute a real-stack adapter.
func (s *SceneServiceImpl) SetSnapshotDecryptor(d snapshotDecryptor) {
	s.decryptor = d
}

// SetSettingsClient installs the SDK-injected host settings client used by
// effectiveTaxonomy to read the game-scope content-warning taxonomy override.
// Wired via scenePlugin.SetSettingsClient before Init; nil until then.
func (s *SceneServiceImpl) SetSettingsClient(c pluginsdk.SettingsClient) {
	s.settings = c
}

// SetHostEvaluator installs the host ABAC evaluator used by WatchScene's
// spectate gate, the lifecycle handlers' end/pause/resume gates, and the
// membership handlers' invite/kick/transfer-ownership/leave gates. Wired via
// scenePlugin.SetHostEvaluator before Init; nil until then (all gated
// handlers fail closed).
func (s *SceneServiceImpl) SetHostEvaluator(ev pluginsdk.HostEvaluator) {
	s.evaluator = ev
}

// SetFocusClient installs the SDK-injected focus client used by WatchScene
// to register the watcher's scene FocusMembership. Wired via
// scenePlugin.SetFocusClient before Init; nil until then (WatchScene fails
// closed).
func (s *SceneServiceImpl) SetFocusClient(c pluginsdk.FocusClient) {
	s.focusClient = c
}

// CreateScene generates a new scene ID, persists the scene, and returns it.
// The caller (host) is responsible for ensuring ABAC has authorised the
// command-execute action; per-resource ABAC for the new scene happens at
// the read path.
//
// Per-field validation (character_id non-empty, title min_len: 1, etc.)
// happens via the protovalidate interceptor before this handler runs.
func (s *SceneServiceImpl) CreateScene(ctx context.Context, req *scenev1.CreateSceneRequest) (*scenev1.CreateSceneResponse, error) {
	ctx, span := startSpan(
		ctx, "scene.service.create_scene",
		attribute.String("subject_id", req.GetCharacterId()),
	)
	defer span.End()

	// Title is trimmed before storage so empty-only-after-trim becomes
	// empty after trimming. The protovalidate annotation rejects empty
	// titles at unmarshal time, but a title of "   " (spaces) passes
	// protovalidate's min_len check and would be stored as a blank
	// title without this trim. Service-level cleanup, not validation.
	title := strings.TrimSpace(req.GetTitle())
	if title == "" {
		recordError(span, errors.New("title cannot be whitespace-only"))
		return nil, status.Errorf(codes.InvalidArgument, "title cannot be whitespace-only")
	}

	if err := s.validateContentWarnings(ctx, req.GetContentWarnings()); err != nil {
		recordError(span, err)
		return nil, err
	}

	id, err := newSceneID()
	if err != nil {
		recordError(span, err)
		slog.WarnContext(
			ctx, "scene.service.create_scene id generation error",
			"subject_id", req.GetCharacterId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	span.SetAttributes(attribute.String("scene_id", id))

	visibility := SceneVisibilityOpen
	if v := req.GetVisibility(); v != "" {
		visibility = SceneVisibility(v)
	}

	// Persist the validated content warnings. If the request carries none,
	// use an empty non-nil slice to match the storage shape (store/rowToProto
	// expects a non-nil []string for JSON serialisation).
	contentWarnings := req.GetContentWarnings()
	if contentWarnings == nil {
		contentWarnings = []string{}
	}
	row := &SceneRow{
		ID:              id,
		Title:           title,
		Description:     req.GetDescription(),
		OwnerID:         req.GetCharacterId(),
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(visibility),
		ContentWarnings: contentWarnings,
		Tags:            []string{},
	}
	if loc := req.GetLocationId(); loc != "" {
		row.LocationID = &loc
	}
	intent, err := s.sceneCreatedIntent(row)
	if err != nil {
		recordError(span, err)
		slog.WarnContext(
			ctx, "scene.service.create_scene emit-intent error",
			"subject_id", req.GetCharacterId(),
			"scene_id", id,
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	if s.eventSink == nil {
		err := oops.Code("SCENE_EVENT_SINK_NOT_CONFIGURED").
			With("scene_id", row.ID).
			New("scene event sink is not configured")
		recordError(span, err)
		slog.WarnContext(
			ctx, "scene.service.create_scene emit preflight error",
			"subject_id", req.GetCharacterId(),
			"scene_id", id,
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	if err := s.store.CreateWithOwner(ctx, row); err != nil {
		recordError(span, err)
		slog.WarnContext(
			ctx, "scene.service.create_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", id,
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	if err := s.eventSink.Emit(ctx, intent); err != nil {
		err = oops.Code("SCENE_EVENT_EMIT_FAILED").
			With("scene_id", row.ID).
			Wrap(err)
		recordError(span, err)
		// Do not delete persisted scene rows here. CreateWithOwner already
		// appended lifecycle ops events, and those rows are append-only.
		slog.WarnContext(
			ctx, "scene.service.create_scene emit error",
			"subject_id", req.GetCharacterId(),
			"scene_id", id,
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	metricSceneCreated(string(visibility), false)
	slog.InfoContext(
		ctx, "scene.service.create_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", id,
		"title", title,
	)

	return &scenev1.CreateSceneResponse{
		Scene: rowToProto(row, time.Now().UTC()),
	}, nil
}

func (s *SceneServiceImpl) sceneCreatedIntent(row *SceneRow) (pluginsdk.EmitIntent, error) {
	if row == nil {
		return pluginsdk.EmitIntent{}, nil
	}

	payload, err := json.Marshal(map[string]string{
		"kind":     "scene.lifecycle.created",
		"scene_id": row.ID,
		"owner_id": row.OwnerID,
		"title":    row.Title,
	})
	if err != nil {
		return pluginsdk.EmitIntent{}, oops.Code("SCENE_EVENT_PAYLOAD_MARSHAL_FAILED").
			With("scene_id", row.ID).
			Wrap(err)
	}

	return pluginsdk.EmitIntent{
		Subject: dotStyleSceneSubject(s.gameID, row.ID),
		Type:    pluginsdk.HostEventTypeSystem,
		Payload: string(payload),
	}, nil
}

// GetScene loads a scene by ID, gates private-scene roster exposure on viewer
// membership, and returns the scene with its participant and observer roster
// populated.
//
// Visibility gate (privacy boundary): if the scene's visibility is not "open"
// AND req.character_id is not in the participants, invitees, or observers set,
// NotFound is returned — the scene's existence and roster are not revealed to
// non-members of a private scene.
//
// The host ABAC engine evaluates the read-own-scene policy before this RPC is
// invoked. Per-field validation (scene_id non-empty) happens via the
// protovalidate interceptor before this handler runs.
func (s *SceneServiceImpl) GetScene(ctx context.Context, req *scenev1.GetSceneRequest) (*scenev1.GetSceneResponse, error) {
	ctx, span := startSpan(
		ctx, "scene.service.get_scene",
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	row, participants, invitees, observers, err := s.store.GetWithMembershipAndObservers(ctx, req.GetSceneId())
	if err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "SCENE_NOT_FOUND" {
			return nil, status.Error(codes.NotFound, "scene not found") //nolint:wrapcheck // gRPC status is the wire contract; opaque per grpc-errors.md
		}
		slog.WarnContext(
			ctx, "scene.service.get_scene store error",
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status is the wire contract; opaque per grpc-errors.md
	}

	// Privacy gate: private scenes are not visible to non-members.
	// This is the canonical plugin-code visibility boundary — the plugin has
	// the membership data and MUST enforce it before returning any scene detail.
	// An open scene's roster is visible board metadata (no gate needed).
	if row.Visibility != string(SceneVisibilityOpen) {
		viewerID := req.GetCharacterId()
		inMembership := false
		for _, id := range participants {
			if id == viewerID {
				inMembership = true
				break
			}
		}
		if !inMembership {
			for _, id := range invitees {
				if id == viewerID {
					inMembership = true
					break
				}
			}
		}
		if !inMembership {
			for _, id := range observers {
				if id == viewerID {
					inMembership = true
					break
				}
			}
		}
		if !inMembership {
			// Return NotFound — do NOT reveal the scene's existence or roster.
			return nil, status.Error(codes.NotFound, "scene not found") //nolint:wrapcheck // gRPC status is the wire contract; existence non-disclosure for private scenes
		}
	}

	slog.InfoContext(
		ctx, "scene.service.get_scene ok",
		"scene_id", row.ID,
	)

	resp := rowToProto(row, row.CreatedAt.Time())

	// Populate participants roster: owners and members.
	resp.Participants = make([]*scenev1.ParticipantInfo, 0, len(participants))
	for _, id := range participants {
		role := "member"
		if id == row.OwnerID {
			role = "owner"
		}
		resp.Participants = append(resp.Participants, &scenev1.ParticipantInfo{
			CharacterId: id,
			// Best-effort display name: no name resolver is wired in the plugin,
			// so fall back to the ID per the proto contract.
			CharacterName: id,
			Role:          role,
		})
	}

	// Populate observers roster (distinct from participants).
	resp.Observers = make([]*scenev1.ParticipantInfo, 0, len(observers))
	for _, id := range observers {
		resp.Observers = append(resp.Observers, &scenev1.ParticipantInfo{
			CharacterId:   id,
			CharacterName: id,
			Role:          "observer",
		})
	}

	return &scenev1.GetSceneResponse{Scene: resp}, nil
}

// resolveBlockedCW returns the effective CW block set for a board request.
// It is the UNION (INV-SCENE-58, safety-accumulating) of:
//   - req.GetExcludeContentWarnings() (per-query caller-supplied excludes)
//   - GAME-scope "content.cw_block" setting (principal "")
//   - PLAYER-scope "content.cw_block" setting (principal = req.GetPlayerId())
//   - CHARACTER-scope "content.cw_block" setting (principal = req.GetCharacterId())
//
// When s.settings is nil only the per-query excludes are returned. For each
// scope read, expected errors (ownership-denied for a foreign principal,
// not-found, missing dispatch token) are silently skipped — the board is never
// failed by a blocked settings read. An unexpected (infrastructure) error is
// logged at WARN before skipping, because it means a configured CW block may be
// silently dropped from the result and ops needs visibility. The returned slice
// is deduplicated; order is not specified.
func (s *SceneServiceImpl) resolveBlockedCW(ctx context.Context, req *scenev1.ListScenesRequest) []string {
	seen := make(map[string]struct{})
	for _, cw := range req.GetExcludeContentWarnings() {
		seen[cw] = struct{}{}
	}

	if s.settings != nil {
		type scopeRead struct {
			scope     pluginsdk.SettingScope
			principal string
		}
		reads := []scopeRead{
			{pluginsdk.SettingScopeGame, ""},
			{pluginsdk.SettingScopePlayer, req.GetPlayerId()},
			{pluginsdk.SettingScopeCharacter, req.GetCharacterId()},
		}
		for _, r := range reads {
			vals, found, err := s.settings.GetSetting(ctx, r.scope, r.principal, "content.cw_block")
			if err != nil {
				if isUnexpectedSettingsError(err) {
					slog.WarnContext(
						ctx,
						"scene.service.resolve_blocked_cw scope read failed; CW block may be unenforced for this query",
						"scope", r.scope,
						"key", "content.cw_block",
						"error", err,
					)
				}
				// Skip denied / errored scopes — never fail the board.
				continue
			}
			if !found {
				continue
			}
			for _, cw := range vals {
				seen[cw] = struct{}{}
			}
		}
	}

	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for cw := range seen {
		out = append(out, cw)
	}
	return out
}

// ListScenes returns the public scene board: open scenes in state 'active' or
// 'paused', optionally filtered by tags and with blocked content warnings
// excluded via the union of all scope-based cw_block settings plus any
// per-query ExcludeContentWarnings (iokti.13).
func (s *SceneServiceImpl) ListScenes(ctx context.Context, req *scenev1.ListScenesRequest) (*scenev1.ListScenesResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.list_scenes")
	defer span.End()

	q := BoardQuery{
		Limit:     int(req.GetLimit()),
		Offset:    int(req.GetOffset()),
		Tags:      req.GetTags(),
		BlockedCW: s.resolveBlockedCW(ctx, req),
	}
	icPrefix := "events." + s.gameID + ".scene."

	rows, err := s.store.ListBoard(ctx, q, icPrefix)
	if err != nil {
		recordError(span, err)
		slog.WarnContext(
			ctx, "scene.service.list_scenes store error",
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "failed to list scenes")
	}

	scenes := make([]*scenev1.SceneInfo, 0, len(rows))
	for _, row := range rows {
		scenes = append(scenes, rowToProto(row, row.CreatedAt.Time()))
	}
	return &scenev1.ListScenesResponse{Scenes: scenes}, nil
}

// ListCharacterScenes returns every non-archived scene the character has a
// participant row in (any role, including observer), with the character's
// role and IC-subject activity metadata. Ordered by last_activity_ms DESC.
// Caller validation is minimal (non-empty character_id, handled by
// protovalidate before this handler runs).
func (s *SceneServiceImpl) ListCharacterScenes(ctx context.Context, req *scenev1.ListCharacterScenesRequest) (*scenev1.ListCharacterScenesResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.list_character_scenes",
		attribute.String("character_id", req.GetCharacterId()))
	defer span.End()

	icPrefix := "events." + s.gameID + ".scene."
	results, err := s.store.ListCharacterScenes(ctx, req.GetCharacterId(), icPrefix)
	if err != nil {
		recordError(span, err)
		slog.WarnContext(ctx, "scene.service.list_character_scenes store error", "error", err)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	out := make([]*scenev1.CharacterSceneInfo, 0, len(results))
	for _, r := range results {
		out = append(out, &scenev1.CharacterSceneInfo{
			Scene:          rowToProto(r.Scene, r.Scene.CreatedAt.Time()),
			Role:           r.Role,
			LastActivityMs: r.LastActivityMS,
			EntryCount:     r.EntryCount,
		})
	}
	return &scenev1.ListCharacterScenesResponse{Scenes: out}, nil
}

// EndScene transitions a scene to the ended state. Only the scene owner is
// authorized (gated by ABAC end-own-scene policy). The transition is
// rejected if the scene is already ended or archived (FailedPrecondition).
//
// The store's End method uses Postgres RETURNING * to atomically return
// the post-update row, so this handler doesn't need a separate Get call.
func (s *SceneServiceImpl) EndScene(ctx context.Context, req *scenev1.EndSceneRequest) (*scenev1.EndSceneResponse, error) {
	ctx, span := startSpan(
		ctx, "scene.lifecycle.end",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	if s.evaluator == nil {
		slog.WarnContext(ctx, "scene.lifecycle.end evaluator not configured",
			"subject_id", req.GetCharacterId(), "scene_id", req.GetSceneId())
		return nil, status.Error(codes.Internal, "permission check unavailable") //nolint:wrapcheck // gRPC status is the wire contract; fail-closed opaque error
	}
	dec, evalErr := s.evaluator.Evaluate(ctx, "end", "scene:"+req.GetSceneId())
	if evalErr != nil {
		recordError(span, evalErr)
		errutil.LogErrorContext(ctx, "scene.lifecycle.end evaluation failed", evalErr)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // opaque Internal per grpc-errors.md
	}
	if !dec.Allowed {
		return nil, status.Error(codes.PermissionDenied, "not permitted to end this scene") //nolint:wrapcheck // gRPC status is the wire contract
	}

	row, err := s.store.End(ctx, req.GetSceneId())
	if err != nil {
		recordError(span, err)
		if grpcErr := mapTransitionError(err, req.GetSceneId()); grpcErr != nil {
			return nil, grpcErr
		}
		slog.WarnContext(
			ctx, "scene.lifecycle.end store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	metricSceneStateTransition(string(SceneStateActive)+"_or_paused", "ended", "rpc")
	slog.InfoContext(
		ctx, "scene.lifecycle.end ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", row.ID,
	)

	return &scenev1.EndSceneResponse{Scene: rowToProto(row, row.CreatedAt.Time())}, nil
}

// PauseScene transitions an active scene to paused. Owner-only.
func (s *SceneServiceImpl) PauseScene(ctx context.Context, req *scenev1.PauseSceneRequest) (*scenev1.PauseSceneResponse, error) {
	ctx, span := startSpan(
		ctx, "scene.lifecycle.pause",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	if s.evaluator == nil {
		slog.WarnContext(ctx, "scene.lifecycle.pause evaluator not configured",
			"subject_id", req.GetCharacterId(), "scene_id", req.GetSceneId())
		return nil, status.Error(codes.Internal, "permission check unavailable") //nolint:wrapcheck // gRPC status is the wire contract; fail-closed opaque error
	}
	dec, evalErr := s.evaluator.Evaluate(ctx, "pause", "scene:"+req.GetSceneId())
	if evalErr != nil {
		recordError(span, evalErr)
		errutil.LogErrorContext(ctx, "scene.lifecycle.pause evaluation failed", evalErr)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // opaque Internal per grpc-errors.md
	}
	if !dec.Allowed {
		return nil, status.Error(codes.PermissionDenied, "not permitted to pause this scene") //nolint:wrapcheck // gRPC status is the wire contract
	}

	row, err := s.store.Pause(ctx, req.GetSceneId())
	if err != nil {
		recordError(span, err)
		if grpcErr := mapTransitionError(err, req.GetSceneId()); grpcErr != nil {
			return nil, grpcErr
		}
		slog.WarnContext(
			ctx, "scene.lifecycle.pause store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	metricSceneStateTransition("active", "paused", "rpc")
	slog.InfoContext(
		ctx, "scene.lifecycle.pause ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", row.ID,
	)

	return &scenev1.PauseSceneResponse{Scene: rowToProto(row, row.CreatedAt.Time())}, nil
}

// ResumeScene transitions a paused scene to active. Phase 2 is owner-only;
// Phase 3 widens to any member per spec D6 (async safety).
func (s *SceneServiceImpl) ResumeScene(ctx context.Context, req *scenev1.ResumeSceneRequest) (*scenev1.ResumeSceneResponse, error) {
	ctx, span := startSpan(
		ctx, "scene.lifecycle.resume",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	if s.evaluator == nil {
		slog.WarnContext(ctx, "scene.lifecycle.resume evaluator not configured",
			"subject_id", req.GetCharacterId(), "scene_id", req.GetSceneId())
		return nil, status.Error(codes.Internal, "permission check unavailable") //nolint:wrapcheck // gRPC status is the wire contract; fail-closed opaque error
	}
	dec, evalErr := s.evaluator.Evaluate(ctx, "resume", "scene:"+req.GetSceneId())
	if evalErr != nil {
		recordError(span, evalErr)
		errutil.LogErrorContext(ctx, "scene.lifecycle.resume evaluation failed", evalErr)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // opaque Internal per grpc-errors.md
	}
	if !dec.Allowed {
		return nil, status.Error(codes.PermissionDenied, "not permitted to resume this scene") //nolint:wrapcheck // gRPC status is the wire contract
	}

	row, err := s.store.Resume(ctx, req.GetSceneId())
	if err != nil {
		recordError(span, err)
		if grpcErr := mapTransitionError(err, req.GetSceneId()); grpcErr != nil {
			return nil, grpcErr
		}
		slog.WarnContext(
			ctx, "scene.lifecycle.resume store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	metricSceneStateTransition("paused", "active", "rpc")
	slog.InfoContext(
		ctx, "scene.lifecycle.resume ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", row.ID,
	)

	return &scenev1.ResumeSceneResponse{Scene: rowToProto(row, row.CreatedAt.Time())}, nil
}

// UpdateScene applies a partial update to mutable scene metadata. Owner-only.
// Rejected for ended/archived scenes. Empty mask updates (no fields specified)
// succeed as no-ops without touching the database.
//
// The update is driven by req.UpdateMask: each path in the mask is a field
// name to apply from the request. Per-field semantic validation (e.g.,
// "title cannot be empty when in the mask") happens in the switch statement
// in buildSceneUpdate; protovalidate constraints in scene.proto handle the
// wire-level max_len / enum-value checks.
func (s *SceneServiceImpl) UpdateScene(ctx context.Context, req *scenev1.UpdateSceneRequest) (*scenev1.UpdateSceneResponse, error) {
	ctx, span := startSpan(
		ctx, "scene.service.update_scene",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	update, err := buildSceneUpdate(req)
	if err != nil {
		recordError(span, err)
		return nil, err // already a gRPC status error
	}

	if update.UpdateContentWarnings {
		if cwErr := s.validateContentWarnings(ctx, update.ContentWarnings); cwErr != nil {
			recordError(span, cwErr)
			return nil, cwErr
		}
	}

	// Best-effort pre-update read: only needed when pose_order_mode is in the
	// mask, to detect whether the mode actually changed so we can emit
	// scene_pose_order_changed_ic. We capture the string value (not the
	// pointer) so that an in-place store mutation cannot alias the pre-value.
	// Read failure is non-fatal — we just won't emit the notice.
	var (
		preMode string
		hasPre  bool
	)
	if update.PoseOrder != nil {
		if r, readErr := s.store.Get(ctx, req.GetSceneId()); readErr == nil {
			preMode = r.PoseOrder
			hasPre = true
		} else {
			slog.WarnContext(ctx, "scene.service.update_scene pre-read for pose_order_changed_ic failed",
				"scene_id", req.GetSceneId(), "error", readErr)
		}
	}

	row, err := s.store.Update(ctx, req.GetSceneId(), update)
	if err != nil {
		recordError(span, err)
		if grpcErr := mapTransitionError(err, req.GetSceneId()); grpcErr != nil {
			return nil, grpcErr
		}
		slog.WarnContext(
			ctx, "scene.service.update_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	// Auto-emit scene_pose_order_changed_ic when pose_order_mode actually
	// changed. No-op updates (mask present but value unchanged) MUST NOT emit.
	if hasPre && preMode != row.PoseOrder {
		s.emitScenePoseOrderChangedIC(ctx, req.GetSceneId(), req.GetCharacterId(), preMode, row.PoseOrder)
	}

	slog.InfoContext(
		ctx, "scene.service.update_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", row.ID,
	)

	return &scenev1.UpdateSceneResponse{Scene: rowToProto(row, row.CreatedAt.Time())}, nil
}

// buildSceneUpdate iterates the request's FieldMask and constructs a store
// SceneUpdate. Each mask path is matched to the corresponding request field
// AND validated semantically (e.g., title cannot be empty even though the
// proto annotation allows max_len-only).
//
// Returns a gRPC status error directly if validation fails — the caller
// passes it through unchanged.
//
// Unknown mask paths return InvalidArgument so clients can't silently send
// updates that get dropped.
func buildSceneUpdate(req *scenev1.UpdateSceneRequest) (*SceneUpdate, error) {
	update := &SceneUpdate{}
	for _, path := range req.GetUpdateMask().GetPaths() {
		switch path {
		case "title":
			t := strings.TrimSpace(req.GetTitle())
			if t == "" {
				return nil, status.Errorf(codes.InvalidArgument, "title cannot be empty or whitespace-only")
			}
			update.Title = &t
		case "description":
			d := req.GetDescription()
			update.Description = &d
		case "visibility":
			v := req.GetVisibility()
			if v == "" {
				return nil, status.Errorf(codes.InvalidArgument, "visibility cannot be empty when in update_mask")
			}
			update.Visibility = &v
		case "pose_order_mode":
			p := req.GetPoseOrderMode()
			if p == "" {
				return nil, status.Errorf(codes.InvalidArgument, "pose_order_mode cannot be empty when in update_mask")
			}
			update.PoseOrder = &p
		case "location_id":
			l := req.GetLocationId()
			update.LocationID = &l // empty string clears the location
		case "content_warnings":
			update.ContentWarnings = req.GetContentWarnings()
			update.UpdateContentWarnings = true
		case "tags":
			update.Tags = req.GetTags()
			update.UpdateTags = true
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unknown update_mask path: %q", path)
		}
	}
	return update, nil
}

// JoinScene attempts to add the calling character to a scene. The store
// method handles all eligibility checks (open vs private, state, etc.).
//
// Per design decision P3.D5, the operation is idempotent: same-character
// retries return success without polluting the audit log with extra
// membership.join events. The store's ParticipantOpResult enum drives
// the emit-or-not decision inside the store transaction.
func (s *SceneServiceImpl) JoinScene(ctx context.Context, req *scenev1.JoinSceneRequest) (*scenev1.JoinSceneResponse, error) {
	ctx, span := startSpan(
		ctx, "scene.service.join_scene",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	_, result, err := s.store.AddParticipant(ctx, req.GetSceneId(), req.GetCharacterId())
	if err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) {
			switch oe.Code() {
			case "SCENE_NOT_FOUND":
				return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetSceneId())
			case "SCENE_TRANSITION_FORBIDDEN":
				return nil, status.Errorf(codes.FailedPrecondition,
					"scene cannot be joined in its current state: %v", err)
			case "SCENE_JOIN_NOT_INVITED":
				return nil, status.Errorf(codes.PermissionDenied,
					"character not invited to private scene")
			}
		}
		slog.WarnContext(
			ctx, "scene.service.join_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	// Auto-emit scene_join_ic notice event when this is a NEW membership
	// (OpInserted = fresh row; OpPromoted = invited→member;
	// ParticipantUpgraded = observer→member). Skipped on OpNoChange per
	// Phase 3 D5 retry-idempotency.
	if result == OpInserted || result == OpPromoted || result == ParticipantUpgraded {
		s.emitSceneJoinIC(ctx, req.GetSceneId(), req.GetCharacterId(), result)
	}

	slog.InfoContext(
		ctx, "scene.service.join_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
	)

	return &scenev1.JoinSceneResponse{}, nil
}

// emitSceneJoinIC emits a scene_join_ic notice event to the scene's IC
// stream. sensitivity:never per crypto.emits §2 (notice events carry metadata
// only — actor_id + from_role + scene_id, no RP content). Emit failure is
// non-fatal — membership is already committed; we log and continue.
func (s *SceneServiceImpl) emitSceneJoinIC(ctx context.Context, sceneID, actorID string, result ParticipantOpResult) {
	if s.eventSink == nil {
		slog.WarnContext(ctx, "scene.service.join_scene scene_join_ic emit skipped: event sink nil",
			"scene_id", sceneID, "actor_id", actorID)
		return
	}

	fromRole := "none"
	switch result {
	case OpPromoted:
		fromRole = "invited"
	case ParticipantUpgraded:
		fromRole = "observer"
	}

	payload, err := json.Marshal(map[string]string{
		"actor_id":  actorID,
		"scene_id":  sceneID,
		"from_role": fromRole,
	})
	if err != nil {
		slog.WarnContext(ctx, "scene.service.join_scene scene_join_ic payload marshal failed",
			"scene_id", sceneID, "actor_id", actorID, "error", err)
		return
	}

	intent := pluginsdk.EmitIntent{
		Subject:   dotStyleSceneSubjectIC(s.gameID, sceneID),
		Type:      "core-scenes:scene_join_ic",
		Payload:   string(payload),
		Sensitive: false, // sensitivity:never per crypto.emits manifest
	}
	if err := s.eventSink.Emit(ctx, intent); err != nil {
		slog.WarnContext(ctx, "scene.service.join_scene scene_join_ic emit failed",
			"scene_id", sceneID, "actor_id", actorID, "error", err)
		// Non-fatal: membership is committed; the notice is best-effort.
	}
}

// WatchScene auto-joins the requesting character into an OPEN scene as a
// role=observer participant and registers the scene FocusMembership on the
// supplied session so focus/Subscribe/history gates admit the watcher.
//
// Gate order is fail-closed per INV-SCENE-61: the plugin-code
// visibility==open and state∈{active,paused} checks run BEFORE the ABAC
// spectate action is evaluated — a non-open scene is rejected without
// consulting ABAC. The store re-checks both gates inside the AddObserver
// transaction (TOCTOU guard).
//
// Per-field validation (character_id/scene_id/session_id non-empty) happens
// via the protovalidate interceptor before this handler runs.
//
// Identity contract: the ABAC subject for the spectate check is derived
// host-side from the dispatch token, NOT from req.character_id. The host
// attaches advisory actor metadata alongside the token; when that metadata
// names a character that differs from req.character_id the request is
// rejected (PermissionDenied) as defense-in-depth against a payload/subject
// mismatch. Absent metadata proceeds — the token still gates the subject.
func (s *SceneServiceImpl) WatchScene(ctx context.Context, req *scenev1.WatchSceneRequest) (*scenev1.WatchSceneResponse, error) {
	ctx, span := startSpan(
		ctx, "scene.service.watch_scene",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	// 0. Defense-in-depth identity cross-check (see contract above): runs
	// before any store or ABAC work so a mismatched request does no work.
	if kind, id, ok := pluginsdk.ActorMetadataFromIncomingContext(ctx); ok &&
		kind == pluginsdk.ActorCharacter && id != req.GetCharacterId() {
		slog.WarnContext(
			ctx, "scene.service.watch_scene actor metadata mismatch",
			"metadata_character_id", id,
			"request_character_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
		)
		return nil, status.Error(codes.PermissionDenied, "not permitted to watch this scene") //nolint:wrapcheck // gRPC status is the wire contract; opaque per grpc-errors.md
	}

	// 1. Load the scene and run the CODE GATES FIRST (INV-SCENE-61): a
	// non-open or non-watchable-state scene MUST fail before ABAC is
	// consulted.
	scene, err := s.store.Get(ctx, req.GetSceneId())
	if err != nil {
		recordError(span, err)
		return nil, mapStoreErr(ctx, err)
	}
	if scene.Visibility != string(SceneVisibilityOpen) ||
		(scene.State != string(SceneStateActive) && scene.State != string(SceneStatePaused)) {
		gateErr := oops.Code("SCENE_NOT_WATCHABLE").
			With("scene_id", req.GetSceneId()).
			With("visibility", scene.Visibility).
			With("state", scene.State).
			Errorf("scene is not watchable")
		recordError(span, gateErr)
		return nil, mapStoreErr(ctx, gateErr)
	}

	// 2. ABAC spectate gate — fails closed when no evaluator is configured
	// (mirrors handleEmit's nil-evaluator handling).
	if s.evaluator == nil {
		slog.WarnContext(
			ctx, "scene.service.watch_scene evaluator not configured",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
		)
		return nil, status.Error(codes.Internal, "permission check unavailable") //nolint:wrapcheck // gRPC status is the wire contract; fail-closed opaque error
	}
	dec, evalErr := s.evaluator.Evaluate(ctx, "spectate", "scene:"+req.GetSceneId())
	if evalErr != nil {
		recordError(span, evalErr)
		errutil.LogErrorContext(ctx, "scene.service.watch_scene spectate evaluation failed", evalErr)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status is the wire contract; opaque Internal per grpc-errors.md
	}
	if !dec.Allowed {
		return nil, status.Error(codes.PermissionDenied, "not permitted to watch this scene") //nolint:wrapcheck // gRPC status is the wire contract
	}

	// 3. Insert the observer row. The store re-checks the visibility/state
	// gates under a shared lock in-tx; map its result classifications.
	row, result, err := s.store.AddObserver(ctx, req.GetSceneId(), req.GetCharacterId())
	if err != nil {
		recordError(span, err)
		return nil, mapStoreErr(ctx, err)
	}
	switch result {
	case ObserverAdded, ObserverAlreadyParticipant:
		// proceed — row is valid for both.
	case ObserverSceneNotFound:
		return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetSceneId())
	case ObserverSceneNotOpen, ObserverSceneNotActive:
		return nil, status.Error(codes.FailedPrecondition, "SCENE_NOT_WATCHABLE") //nolint:wrapcheck // gRPC status is the wire contract; code as message mirrors mapStoreErr
	}

	// 4. Register the scene FocusMembership on the watcher's session.
	// Fail closed: a watcher without focus membership cannot read the scene,
	// so a missing client or an unexpected join failure surfaces as an error
	// rather than a silent half-join.
	// FOCUS_ALREADY_MEMBER is idempotent success — the session is already a
	// focus member (e.g. joined via `scene join`, or a retry/page-reload).
	// Mirror the precedent at commands.go JoinFocus handling.
	if s.focusClient == nil {
		slog.WarnContext(
			ctx, "scene.service.watch_scene focus client not configured",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
		)
		return nil, status.Error(codes.Internal, "focus registration unavailable") //nolint:wrapcheck // gRPC status is the wire contract; fail-closed opaque error
	}
	if joinErr := s.focusClient.JoinFocus(ctx, req.GetSessionId(), pluginsdk.FocusKey{
		Kind:     pluginsdk.FocusKindScene,
		TargetID: req.GetSceneId(),
	}); joinErr != nil {
		var oe oops.OopsError
		if !errors.As(joinErr, &oe) || oe.Code() != "FOCUS_ALREADY_MEMBER" {
			recordError(span, joinErr)
			errutil.LogErrorContext(ctx, "scene.service.watch_scene focus join failed", joinErr)
			return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status is the wire contract; opaque Internal per grpc-errors.md
		}
	}

	slog.InfoContext(
		ctx, "scene.service.watch_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
		"role", row.Role,
		"pre_existing", result == ObserverAlreadyParticipant,
	)

	return &scenev1.WatchSceneResponse{
		Participant: &scenev1.ParticipantInfo{
			CharacterId: row.CharacterID,
			// Best-effort display name: no name resolver is wired in the
			// service, so fall back to the ID per the proto contract.
			CharacterName: row.CharacterID,
			Role:          row.Role,
			JoinedAt:      timestamppb.New(row.JoinedAt.Time()),
		},
	}, nil
}

// emitSceneLeaveIC emits a scene_leave_ic notice event. reason discriminates
// voluntary ("left") vs involuntary ("kicked"). removedBy is the kicker's
// character_id for kicks; empty string for voluntary leaves.
// sensitivity:never per spec §2. Non-fatal emit failure (membership already
// removed; notice is best-effort).
func (s *SceneServiceImpl) emitSceneLeaveIC(ctx context.Context, sceneID, actorID, reason, removedBy string) {
	if s.eventSink == nil {
		slog.WarnContext(ctx, "scene.service.leave_scene scene_leave_ic emit skipped: event sink nil",
			"scene_id", sceneID, "actor_id", actorID, "reason", reason)
		return
	}

	fields := map[string]string{
		"actor_id": actorID,
		"scene_id": sceneID,
		"reason":   reason,
	}
	if removedBy != "" {
		fields["removed_by"] = removedBy
	}

	payload, err := json.Marshal(fields)
	if err != nil {
		slog.WarnContext(ctx, "scene.service.leave_scene scene_leave_ic payload marshal failed",
			"scene_id", sceneID, "actor_id", actorID, "error", err)
		return
	}

	intent := pluginsdk.EmitIntent{
		Subject:   dotStyleSceneSubjectIC(s.gameID, sceneID),
		Type:      "core-scenes:scene_leave_ic",
		Payload:   string(payload),
		Sensitive: false, // sensitivity:never per crypto.emits manifest
	}
	if err := s.eventSink.Emit(ctx, intent); err != nil {
		slog.WarnContext(ctx, "scene.service.leave_scene scene_leave_ic emit failed",
			"scene_id", sceneID, "actor_id", actorID, "reason", reason, "error", err)
		// Non-fatal: membership is removed; the notice is best-effort.
	}
}

// emitScenePoseOrderChangedIC emits a scene_pose_order_changed_ic notice
// event when the pose order mode actually changes. sensitivity:never per
// spec §2 — payload carries actor_id, scene_id, and mode strings only, no
// RP content. Non-fatal emit failure (the mode change is already committed).
// actor_name omitted (no nameResolver wired; same convention as T17/T18).
func (s *SceneServiceImpl) emitScenePoseOrderChangedIC(ctx context.Context, sceneID, actorID, oldMode, newMode string) {
	if s.eventSink == nil {
		slog.WarnContext(ctx, "scene.service.update_scene scene_pose_order_changed_ic emit skipped: event sink nil",
			"scene_id", sceneID, "actor_id", actorID)
		return
	}

	payload, err := json.Marshal(map[string]string{
		"actor_id": actorID,
		"scene_id": sceneID,
		"old_mode": oldMode,
		"new_mode": newMode,
	})
	if err != nil {
		slog.WarnContext(ctx, "scene.service.update_scene scene_pose_order_changed_ic payload marshal failed",
			"scene_id", sceneID, "actor_id", actorID, "error", err)
		return
	}

	intent := pluginsdk.EmitIntent{
		Subject:   dotStyleSceneSubjectIC(s.gameID, sceneID),
		Type:      "core-scenes:scene_pose_order_changed_ic",
		Payload:   string(payload),
		Sensitive: false, // sensitivity:never per crypto.emits manifest
	}
	if err := s.eventSink.Emit(ctx, intent); err != nil {
		slog.WarnContext(ctx, "scene.service.update_scene scene_pose_order_changed_ic emit failed",
			"scene_id", sceneID, "actor_id", actorID, "error", err)
		// Non-fatal: mode change is committed; the notice is best-effort.
	}
}

// LeaveScene removes the calling character from a scene. Per design decision
// P3.D7, scene owners cannot leave their own scene — they must use scene end
// or transfer ownership first. The service-layer pre-check returns
// FailedPrecondition with an actionable hint message.
//
// The store's RemoveParticipant ALSO has a `WHERE role <> 'owner'` filter
// for defense-in-depth.
func (s *SceneServiceImpl) LeaveScene(ctx context.Context, req *scenev1.LeaveSceneRequest) (*scenev1.LeaveSceneResponse, error) {
	ctx, span := startSpan(
		ctx, "scene.service.leave_scene",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	if s.evaluator == nil {
		slog.WarnContext(ctx, "scene.membership.leave evaluator not configured",
			"subject_id", req.GetCharacterId(), "scene_id", req.GetSceneId())
		return nil, status.Error(codes.Internal, "permission check unavailable") //nolint:wrapcheck // gRPC status is the wire contract; fail-closed opaque error
	}
	dec, evalErr := s.evaluator.Evaluate(ctx, "leave", "scene:"+req.GetSceneId())
	if evalErr != nil {
		recordError(span, evalErr)
		errutil.LogErrorContext(ctx, "scene.membership.leave evaluation failed", evalErr)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // opaque Internal per grpc-errors.md
	}
	if !dec.Allowed {
		return nil, status.Error(codes.PermissionDenied, "not permitted to leave this scene") //nolint:wrapcheck // gRPC status is the wire contract
	}

	// Service-layer owner-leave pre-check. Reads the scene first so we can
	// give the user a helpful message before hitting the store's defensive
	// WHERE filter (which would return SCENE_OWNER_CANNOT_LEAVE — same
	// outcome but the error path is uglier).
	sceneRow, err := s.store.Get(ctx, req.GetSceneId())
	if err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "SCENE_NOT_FOUND" {
			return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetSceneId())
		}
		slog.WarnContext(
			ctx, "scene.service.leave_scene load error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	if sceneRow.OwnerID == req.GetCharacterId() {
		err := status.Errorf(codes.FailedPrecondition,
			"scene owners cannot leave; use `scene end` to terminate the scene or transfer ownership first")
		recordError(span, err)
		return nil, err
	}

	if _, err := s.store.RemoveParticipant(ctx, req.GetSceneId(), req.GetCharacterId()); err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) {
			switch oe.Code() {
			case "SCENE_PARTICIPANT_NOT_FOUND":
				return nil, status.Errorf(codes.NotFound, "character not in scene")
			case "SCENE_OWNER_CANNOT_LEAVE":
				// Defense-in-depth path — should never trigger after the
				// service-layer pre-check above, but mapped for completeness.
				return nil, status.Errorf(codes.FailedPrecondition,
					"scene owners cannot leave")
			}
		}
		slog.WarnContext(
			ctx, "scene.service.leave_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	// Auto-emit scene_leave_ic notice event. Non-fatal: membership is already
	// removed; the notice is best-effort.
	s.emitSceneLeaveIC(ctx, req.GetSceneId(), req.GetCharacterId(), "left", "")

	slog.InfoContext(
		ctx, "scene.service.leave_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
	)

	return &scenev1.LeaveSceneResponse{}, nil
}

// InviteToScene adds an 'invited' participant row for the target character.
// ABAC enforces participant-wide invite (any owner or member of an
// active/paused scene) — self-gated at this handler and at the dispatcher.
func (s *SceneServiceImpl) InviteToScene(ctx context.Context, req *scenev1.InviteToSceneRequest) (*scenev1.InviteToSceneResponse, error) {
	ctx, span := startSpan(
		ctx, "scene.service.invite_to_scene",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
		attribute.String("target_id", req.GetTargetCharacterId()),
	)
	defer span.End()

	if s.evaluator == nil {
		slog.WarnContext(ctx, "scene.membership.invite evaluator not configured",
			"subject_id", req.GetCharacterId(), "scene_id", req.GetSceneId())
		return nil, status.Error(codes.Internal, "permission check unavailable") //nolint:wrapcheck // gRPC status is the wire contract; fail-closed opaque error
	}
	dec, evalErr := s.evaluator.Evaluate(ctx, "invite", "scene:"+req.GetSceneId())
	if evalErr != nil {
		recordError(span, evalErr)
		errutil.LogErrorContext(ctx, "scene.membership.invite evaluation failed", evalErr)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // opaque Internal per grpc-errors.md
	}
	if !dec.Allowed {
		return nil, status.Error(codes.PermissionDenied, "not permitted to invite to this scene") //nolint:wrapcheck // gRPC status is the wire contract
	}

	if _, err := s.store.InviteParticipant(ctx, req.GetSceneId(), req.GetCharacterId(), req.GetTargetCharacterId()); err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "SCENE_INVITE_TARGET_ALREADY_MEMBER" {
			return nil, status.Errorf(codes.AlreadyExists, "character is already a member of this scene")
		}
		slog.WarnContext(
			ctx, "scene.service.invite_to_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"target_id", req.GetTargetCharacterId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	slog.InfoContext(
		ctx, "scene.service.invite_to_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
		"target_id", req.GetTargetCharacterId(),
	)
	return &scenev1.InviteToSceneResponse{}, nil
}

// KickFromScene removes a target character from a scene. ABAC enforces
// owner-only kick at the dispatcher layer. The store's WHERE filter is
// the defense-in-depth layer that prevents owner removal.
func (s *SceneServiceImpl) KickFromScene(ctx context.Context, req *scenev1.KickFromSceneRequest) (*scenev1.KickFromSceneResponse, error) {
	ctx, span := startSpan(
		ctx, "scene.service.kick_from_scene",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
		attribute.String("target_id", req.GetTargetCharacterId()),
	)
	defer span.End()

	if s.evaluator == nil {
		slog.WarnContext(ctx, "scene.membership.kick evaluator not configured",
			"subject_id", req.GetCharacterId(), "scene_id", req.GetSceneId())
		return nil, status.Error(codes.Internal, "permission check unavailable") //nolint:wrapcheck // gRPC status is the wire contract; fail-closed opaque error
	}
	dec, evalErr := s.evaluator.Evaluate(ctx, "kick", "scene:"+req.GetSceneId())
	if evalErr != nil {
		recordError(span, evalErr)
		errutil.LogErrorContext(ctx, "scene.membership.kick evaluation failed", evalErr)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // opaque Internal per grpc-errors.md
	}
	if !dec.Allowed {
		return nil, status.Error(codes.PermissionDenied, "not permitted to kick from this scene") //nolint:wrapcheck // gRPC status is the wire contract
	}

	if _, err := s.store.KickParticipant(ctx, req.GetSceneId(), req.GetCharacterId(), req.GetTargetCharacterId()); err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) {
			switch oe.Code() {
			case "SCENE_PARTICIPANT_NOT_FOUND":
				return nil, status.Errorf(codes.NotFound, "target not in scene")
			case "SCENE_KICK_FORBIDDEN":
				return nil, status.Errorf(codes.FailedPrecondition,
					"scene owner cannot be kicked")
			}
		}
		slog.WarnContext(
			ctx, "scene.service.kick_from_scene store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"target_id", req.GetTargetCharacterId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	// Auto-emit scene_leave_ic notice event (reason=kicked). Non-fatal:
	// membership is already removed; the notice is best-effort.
	s.emitSceneLeaveIC(ctx, req.GetSceneId(), req.GetTargetCharacterId(), "kicked", req.GetCharacterId())

	slog.InfoContext(
		ctx, "scene.service.kick_from_scene ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
		"target_id", req.GetTargetCharacterId(),
	)
	return &scenev1.KickFromSceneResponse{}, nil
}

// TransferOwnership reassigns ownership of a scene from the calling character
// to a target member. ABAC enforces owner-only transfer at the dispatcher.
// Per design decision P3.D8, the target MUST be an existing member; the
// previous owner becomes a member.
func (s *SceneServiceImpl) TransferOwnership(ctx context.Context, req *scenev1.TransferOwnershipRequest) (*scenev1.TransferOwnershipResponse, error) {
	ctx, span := startSpan(
		ctx, "scene.service.transfer_ownership",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
		attribute.String("new_owner", req.GetNewOwnerCharacterId()),
	)
	defer span.End()

	if s.evaluator == nil {
		slog.WarnContext(ctx, "scene.membership.transfer evaluator not configured",
			"subject_id", req.GetCharacterId(), "scene_id", req.GetSceneId())
		return nil, status.Error(codes.Internal, "permission check unavailable") //nolint:wrapcheck // gRPC status is the wire contract; fail-closed opaque error
	}
	dec, evalErr := s.evaluator.Evaluate(ctx, "transfer-ownership", "scene:"+req.GetSceneId())
	if evalErr != nil {
		recordError(span, evalErr)
		errutil.LogErrorContext(ctx, "scene.membership.transfer evaluation failed", evalErr)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // opaque Internal per grpc-errors.md
	}
	if !dec.Allowed {
		return nil, status.Error(codes.PermissionDenied, "not permitted to transfer this scene") //nolint:wrapcheck // gRPC status is the wire contract
	}

	if err := s.store.TransferOwnership(ctx, req.GetSceneId(), req.GetCharacterId(), req.GetNewOwnerCharacterId()); err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) {
			switch oe.Code() {
			case "SCENE_NOT_FOUND":
				return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetSceneId())
			case "SCENE_NOT_OWNER":
				return nil, status.Errorf(codes.PermissionDenied,
					"only the scene owner can transfer ownership")
			case "SCENE_TRANSITION_FORBIDDEN":
				return nil, status.Errorf(codes.FailedPrecondition,
					"scene cannot have ownership transferred in its current state")
			case "SCENE_TRANSFER_TARGET_NOT_MEMBER":
				return nil, status.Errorf(codes.FailedPrecondition,
					"transfer target must be an existing member of the scene")
			}
		}
		slog.WarnContext(
			ctx, "scene.service.transfer_ownership store error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"new_owner", req.GetNewOwnerCharacterId(),
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	slog.InfoContext(
		ctx, "scene.service.transfer_ownership ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
		"new_owner", req.GetNewOwnerCharacterId(),
	)
	return &scenev1.TransferOwnershipResponse{}, nil
}

// GetPoseOrder returns the current pose-order entries for a scene.
//
// INV-SCENE-60 plugin-code gate: the caller MUST be a current participant
// of the scene (owner or member, NOT invited). The ABAC engine is
// NEVER consulted from this handler; INV-SCENE-4 (T23 meta-test, future
// bead) enforces this by rg-asserting that no engine.Evaluate /
// engine.CanPerformAction call appears in this function body.
//
// The PermissionDenied gate fires before any scene-existence check —
// non-participants MUST NOT be able to distinguish "scene does not
// exist" from "scene exists but you are not a member" via the error
// code. The store's IsParticipant returns (false, nil) for both cases,
// which is the desired flattening.
//
// Composition: IsParticipant (T4) → Get + ListParticipantsWithPoseMeta
// (T5) → Compute (T9) → proto wire form.
//
// Names map is empty for Phase 4 (no nameResolver wired in this
// plugin yet); Compute renders missing names as the character_id —
// safe fallback per poseorder.go::resolveName.
//
// Per spec §7.4.
func (s *SceneServiceImpl) GetPoseOrder(ctx context.Context, req *scenev1.GetPoseOrderRequest) (*scenev1.GetPoseOrderResponse, error) {
	ctx, span := startSpan(
		ctx, "scene.service.get_pose_order",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("scene_id", req.GetSceneId()),
	)
	defer span.End()

	// INV-SCENE-60 gate: direct plugin-code participant check, NO ABAC.
	// Fires before scene-existence check to avoid leaking the
	// existence of a scene to non-participants.
	ok, err := s.store.IsParticipant(ctx, req.GetSceneId(), req.GetCharacterId())
	if err != nil {
		recordError(span, err)
		slog.WarnContext(
			ctx, "scene.service.get_pose_order participant check error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		// Do not surface the underlying pgx/wrap text to the client —
		// it can leak schema/connection detail across the gRPC boundary.
		// The server-side log above carries the diagnostic.
		return nil, status.Error(codes.Internal, "participant check failed") //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	if !ok {
		return nil, status.Error(codes.PermissionDenied, "not a participant of scene") //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	// Load scene row for pose_order mode. ListParticipantsWithPoseMeta
	// (T5) carries TotalPoseCount + per-participant pose metadata but
	// not the scene's mode, so a separate Get is required.
	sceneRow, err := s.store.Get(ctx, req.GetSceneId())
	if err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "SCENE_NOT_FOUND" {
			// Defense-in-depth: IsParticipant returned true above, so
			// the scene existed moments ago. A race deleted it; surface
			// NotFound rather than masquerading as a permission error.
			return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetSceneId())
		}
		slog.WarnContext(
			ctx, "scene.service.get_pose_order get scene error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Error(codes.Internal, "scene lookup failed") //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	poseMeta, err := s.store.ListParticipantsWithPoseMeta(ctx, req.GetSceneId())
	if err != nil {
		recordError(span, err)
		slog.WarnContext(
			ctx, "scene.service.get_pose_order list pose meta error",
			"subject_id", req.GetCharacterId(),
			"scene_id", req.GetSceneId(),
			"error", err,
		)
		return nil, status.Error(codes.Internal, "pose metadata lookup failed") //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	// Compute pose order. Names map is nil for Phase 4; Compute
	// renders missing names as the character_id per resolveName's
	// best-effort contract. Future nameResolver integration can
	// populate this map.
	entries := Compute(sceneRow.PoseOrder, poseMeta.TotalPoseCount, poseMeta.Participants, nil)

	// Map to proto wire form.
	protoEntries := make([]*scenev1.PoseOrderEntry, 0, len(entries))
	for _, e := range entries {
		pe := &scenev1.PoseOrderEntry{
			CharacterId:   e.CharacterID,
			CharacterName: e.CharacterName,
			Eligible:      e.Eligible,
		}
		if e.LastPosedAt != nil {
			pe.LastPosedAt = timestamppb.New(*e.LastPosedAt)
		}
		if e.PosesSinceLast != nil {
			gap := *e.PosesSinceLast
			pe.PosesSinceLast = &gap
		}
		protoEntries = append(protoEntries, pe)
	}

	slog.InfoContext(
		ctx, "scene.service.get_pose_order ok",
		"subject_id", req.GetCharacterId(),
		"scene_id", req.GetSceneId(),
		"mode", sceneRow.PoseOrder,
		"entry_count", len(protoEntries),
	)

	return &scenev1.GetPoseOrderResponse{
		Mode:           sceneRow.PoseOrder,
		TotalPoseCount: poseMeta.TotalPoseCount,
		Entries:        protoEntries,
	}, nil
}

// mapTransitionError translates store-layer transition errors into gRPC
// status errors. Returns nil if the error is not a transition error
// (caller should fall through to a generic Internal status).
func mapTransitionError(err error, sceneID string) error {
	var oe oops.OopsError
	if !errors.As(err, &oe) {
		return nil
	}
	switch oe.Code() {
	case "SCENE_NOT_FOUND":
		return status.Errorf(codes.NotFound, "scene not found: %s", sceneID)
	case "SCENE_TRANSITION_FORBIDDEN":
		return status.Errorf(codes.FailedPrecondition,
			"scene transition forbidden: %v", err)
	}
	return nil
}

// newSceneID generates a ULID using crypto/rand for entropy. Per project
// convention, math/rand is forbidden everywhere — see CLAUDE.md.
func newSceneID() (string, error) {
	ms := ulid.Timestamp(time.Now())
	id, err := ulid.New(ms, rand.Reader)
	if err != nil {
		return "", oops.Code("SCENE_ID_GEN_FAILED").Wrap(err)
	}
	return id.String(), nil
}

// Phase 6 publication RPC stubs — present per plan A3 Step 5 so the Phase 6
// surface is explicit. Real handlers land in Phase B (publish_service.go),
// which replaces these. UnimplementedSceneServiceServer is embedded, so these
// override the embedded defaults with the same Unimplemented status.
// StartScenePublish is implemented in publish_service.go (Task B2).

// CastPublishSceneVote is implemented in publish_service.go (Task B3).
// WithdrawScenePublish is implemented in publish_service.go (Task B4).

// GetPublishedScene is implemented in publish_service.go (Task B5).

// DownloadPublishedScene is implemented in publish_service.go (Task B6).

// ListScenePublishAttempts is implemented in publish_service.go (Task B7).

// GetPublicSceneArchive is implemented in publish_service.go (Task C4).
// DownloadPublicSceneArchive is implemented in publish_service.go (Task C5).
// ExtendScenePublishVoteAttempts is implemented in publish_service.go (Task E1).

// rowToProto converts a SceneRow to the proto representation.
//
// createdAt is passed in to allow CreateScene (which has not re-fetched
// from the database) to use the host's wall clock; GetScene passes the
// row's actual CreatedAt.
func rowToProto(row *SceneRow, createdAt time.Time) *scenev1.SceneInfo {
	info := &scenev1.SceneInfo{
		Id:              row.ID,
		Title:           row.Title,
		Description:     row.Description,
		OwnerId:         row.OwnerID,
		State:           row.State,
		Visibility:      row.Visibility,
		PoseOrderMode:   row.PoseOrder,
		ContentWarnings: row.ContentWarnings,
		Tags:            row.Tags,
		CreatedAt:       timestamppb.New(createdAt),
		LastActivityMs:  row.LastActivityMs,
	}
	if row.LocationID != nil {
		info.LocationId = *row.LocationID
	}
	return info
}
