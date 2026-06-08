// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/settings"
)

// PlayerPreferencesReader provides read access to player preferences.
// Narrow interface to decouple from the full auth.PlayerRepository.
type PlayerPreferencesReader interface {
	SceneFocusReplayTail(ctx context.Context, playerID ulid.ULID) *int
}

// StreamSender delivers stream subscription updates to the live loop.
// Decouples the coordinator from the concrete SessionStreamRegistry
// type in internal/grpc (avoiding an import cycle).
type StreamSender interface {
	Send(sessionID string, stream string, add bool, mode ReplayMode) error
}

// StreamContributor collects plugin-contributed stream names for a session.
// Used by RestoreFocus to include ambient plugin streams in the plan.
type StreamContributor interface {
	QuerySessionStreams(ctx context.Context, req StreamContributorRequest) []string
}

// StreamContributorRequest carries identifiers for a stream query.
type StreamContributorRequest struct {
	CharacterID string
	PlayerID    string
	SessionID   string
}

// Coordinator is the sole authoritative mutator of a session's
// focused-context state.
type Coordinator interface {
	JoinFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	LeaveFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	// LeaveFocusByTarget removes the given focus membership from every
	// non-expired session that holds it. Used for cross-session fan-out
	// (e.g., scene end). Returns a LeaveByTargetResult describing the
	// sweep; per-session failures are carried in result.Failed.
	//
	// Error semantics: the returned error covers only the enumeration
	// step (session store ListByFocus). On enumeration failure the
	// result is zero-valued and the error is coded FOCUS_SWEEP_LIST_FAILED.
	// Per-session errors live on result.Failed[].Err; callers that want
	// to retry iterate that slice. See LeaveByTargetResult for the full
	// state-space.
	LeaveFocusByTarget(ctx context.Context, target session.FocusKey) (session.LeaveByTargetResult, error)
	PresentFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	RestoreFocus(ctx context.Context, sessionID string) (RestorePlan, error)
	// IsAnyConnFocused reports whether any of the character's connections are
	// currently focused on the given scene. Returns (false, nil) when the
	// character has no active session (SESSION_NOT_FOUND → false per spec §6.3).
	IsAnyConnFocused(ctx context.Context, characterID, sceneID ulid.ULID) (bool, error)
	// RestoreConnectionFocus restores a reconnecting Connection's FocusKey
	// from the session's PresentingFocus, gated on FocusMemberships. INV-SCENE-18
	// (validation + grid fallback under one Store-lock acquisition) +
	// INV-SCENE-25 (reconnect vs concurrent LeaveFocus serializes via the
	// SessionConnectionMutator path). See restore_connection_focus.go for
	// the three-branch decision table.
	RestoreConnectionFocus(ctx context.Context, sessionID string, connectionID ulid.ULID) error
	// SetConnectionFocus mutates a single Connection.FocusKey and
	// (D9-gated) Info.PresentingFocus atomically under one Store-lock
	// acquisition. Pins INV-SCENE-14 (FocusMemberships gate on scene targets)
	// and INV-SCENE-26 (scene grid preserves PresentingFocus). Returns
	// SetConnectionFocusResult so the coordinator itself can drive
	// per-connection subscription deltas via ComputeFocusManagedStreams +
	// StreamDeltas + SendToConnection without a second store round-trip
	// (INV-SCENE-38, see driveFocusDeltas).
	// isSceneGrid=true MUST NOT touch Info.PresentingFocus.
	SetConnectionFocus(
		ctx context.Context,
		connectionID ulid.ULID,
		focusKey *session.FocusKey,
		isSceneGrid bool,
	) (SetConnectionFocusResult, error)
	// AutoFocusOnJoin fans out a focus assignment to every terminal/telnet
	// connection belonging to characterID's active session, targeting sceneID.
	// Pins INV-SCENE-17 (terminal-only filter) and INV-SCENE-24 (D8 skip-already-focused).
	// SESSION_NOT_FOUND → empty response, nil error (consistent with T16).
	// Per-connection failures (membership_absent, connection_not_found) are
	// carried in AutoFocusOnJoinResponse.FailedConnectionIDs, not returned as
	// the error. The error return is reserved for store-level failures.
	AutoFocusOnJoin(
		ctx context.Context,
		characterID, sceneID ulid.ULID,
	) (AutoFocusOnJoinResponse, error)
	// GetConnectionFocus returns the current FocusKey for the given connection,
	// or nil when the connection is grid-focused. Returns CONNECTION_NOT_FOUND
	// when the connection does not exist; callers SHOULD treat this as absent
	// focus rather than an error (the connection may have disconnected between
	// the command dispatch and this lookup).
	GetConnectionFocus(ctx context.Context, connectionID ulid.ULID) (*session.FocusKey, error)
}

// RestorePlan is the ordered list of streams and their replay modes to
// apply when a Subscribe handler starts.
type RestorePlan struct {
	Streams          []StreamWithMode
	PresentingStream string // empty if no presenting focus
}

// SetConnectionFocusResult carries the outputs of a SetConnectionFocus call
// consumed by focus.Coordinator.driveFocusDeltas to compute per-Connection
// subscription deltas without a second store round-trip (INV-SCENE-38).
type SetConnectionFocusResult struct {
	// OldFocusKey is the Connection.FocusKey value captured before the mutation.
	// Nil means the connection was on the grid (no prior explicit focus).
	OldFocusKey *session.FocusKey
	// SessionID is the session that owns the mutated connection. Used by
	// SendToConnection to route the subscription update to the right goroutine.
	SessionID string
	// CharLocationID is the session's LocationID at mutation time, used to
	// compute grid stream names (location:<charLocationID>) for subscription
	// delta routing.
	CharLocationID ulid.ULID
}

// defaultCoordinator is the production Coordinator implementation.
type defaultCoordinator struct {
	sessionStore      session.Store
	streamSender      StreamSender
	connectionSender  ConnectionSender
	streamContributor StreamContributor
	policies          map[session.FocusKind]KindPolicy
	gameID            string

	// Settings stores for preference resolution.
	gameSettings      settings.Settings
	playerSettings    settings.PlayerSettingsStore
	characterSettings settings.CharacterSettingsStore

	// Typed player preference reader (highest precedence in resolution).
	playerPrefs PlayerPreferencesReader
}

// CoordinatorOption configures a defaultCoordinator.
type CoordinatorOption func(*defaultCoordinator)

// WithSessionStore sets the session store.
func WithSessionStore(store session.Store) CoordinatorOption {
	return func(c *defaultCoordinator) { c.sessionStore = store }
}

// WithStreamSender sets the stream sender.
func WithStreamSender(sender StreamSender) CoordinatorOption {
	return func(c *defaultCoordinator) { c.streamSender = sender }
}

// WithConnectionSender sets the per-Connection stream sender used to deliver
// focus-driven subscription deltas (INV-SCENE-38: the coordinator is the sole
// driver). A nil sender disables per-connection delta delivery (best-effort).
func WithConnectionSender(sender ConnectionSender) CoordinatorOption {
	return func(c *defaultCoordinator) { c.connectionSender = sender }
}

// WithGameID sets the game ID used to compute focus-managed scene stream
// names (events.<gameID>.scene.<id>.{ic,ooc}). An empty string is treated as
// the default game "main", applied inside driveFocusDeltas. Production always
// supplies a concrete game id (cmd/holomush wires s.cfg.EventBus.GameID());
// the empty default exists for tests that don't set one.
func WithGameID(gameID string) CoordinatorOption {
	return func(c *defaultCoordinator) { c.gameID = gameID }
}

// WithKindPolicy registers a KindPolicy for its kind.
func WithKindPolicy(policy KindPolicy) CoordinatorOption {
	return func(c *defaultCoordinator) {
		c.policies[policy.Kind()] = policy
	}
}

// WithGameSettings sets the game-wide settings store.
func WithGameSettings(gs settings.Settings) CoordinatorOption {
	return func(c *defaultCoordinator) { c.gameSettings = gs }
}

// WithPlayerSettings sets the player settings store.
func WithPlayerSettings(ps settings.PlayerSettingsStore) CoordinatorOption {
	return func(c *defaultCoordinator) { c.playerSettings = ps }
}

// WithCharacterSettings sets the character settings store.
func WithCharacterSettings(cs settings.CharacterSettingsStore) CoordinatorOption {
	return func(c *defaultCoordinator) { c.characterSettings = cs }
}

// WithPlayerPreferences sets the player preference reader.
func WithPlayerPreferences(pr PlayerPreferencesReader) CoordinatorOption {
	return func(c *defaultCoordinator) { c.playerPrefs = pr }
}

// WithStreamContributor sets the plugin stream contributor for ambient streams.
func WithStreamContributor(sc StreamContributor) CoordinatorOption {
	return func(c *defaultCoordinator) { c.streamContributor = sc }
}

// NewCoordinator constructs a defaultCoordinator. sessionStore is required.
func NewCoordinator(opts ...CoordinatorOption) (Coordinator, error) {
	c := &defaultCoordinator{
		policies: make(map[session.FocusKind]KindPolicy),
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.sessionStore == nil {
		return nil, oops.Errorf("session store is required")
	}
	return c, nil
}

// policyFor looks up the registered KindPolicy for the given kind.
func (c *defaultCoordinator) policyFor(kind session.FocusKind) (KindPolicy, error) {
	p, ok := c.policies[kind]
	if !ok {
		return nil, oops.Code("FOCUS_KIND_UNREGISTERED").
			With("kind", string(kind)).
			Errorf("no KindPolicy registered for kind %q", kind)
	}
	return p, nil
}

// sessionOrError retrieves the session and validates it's not expired.
func (c *defaultCoordinator) sessionOrError(ctx context.Context, sessionID string) (*session.Info, error) {
	info, err := c.sessionStore.Get(ctx, sessionID)
	if err != nil {
		return nil, oops.Code("SESSION_NOT_FOUND").
			With("session_id", sessionID).
			Wrap(err)
	}
	if info.Status == session.StatusExpired {
		return nil, oops.Code("SESSION_EXPIRED").
			With("session_id", sessionID).
			Errorf("session %s is expired", sessionID)
	}
	return info, nil
}

// buildPolicyContext resolves preference inputs for the kind policy.
func (c *defaultCoordinator) buildPolicyContext(
	ctx context.Context,
	info *session.Info,
	target session.FocusKey,
) PolicyContext {
	pctx := PolicyContext{
		SessionID:            info.ID,
		Target:               target,
		SceneFocusReplayTail: 3, // substrate default
	}

	var scopes []settings.Settings
	if c.characterSettings != nil {
		scopes = append(scopes, c.characterSettings.For(ctx, info.CharacterID))
	}
	if c.playerSettings != nil {
		scopes = append(scopes, c.playerSettings.For(ctx, info.PlayerID))
	}
	if c.gameSettings != nil {
		scopes = append(scopes, c.gameSettings)
	}
	if len(scopes) > 0 {
		chain := settings.NewChain(scopes...)
		if tail, ok := chain.IntN(ctx, "scenes.focus.replay_tail_default"); ok {
			pctx.SceneFocusReplayTail = clamp(tail, 0, 10)
		}
	}

	// Layer 2: typed player preference override (highest precedence).
	if c.playerPrefs != nil {
		if tail := c.playerPrefs.SceneFocusReplayTail(ctx, info.PlayerID); tail != nil {
			pctx.SceneFocusReplayTail = clamp(*tail, 0, 10)
		}
	}

	return pctx
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
