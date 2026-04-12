// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/settings"
)

// StreamSender delivers stream subscription updates to the live loop.
// Decouples the coordinator from the concrete SessionStreamRegistry
// type in internal/grpc (avoiding an import cycle).
type StreamSender interface {
	Send(sessionID string, stream string, add bool, mode ReplayMode) error
}

// CursorLocker provides per-session mutex access for serializing focus
// transitions against live-loop cursor commits.
type CursorLocker interface {
	Lock(sessionID string) (unlock func())
}

// FocusCoordinator is the sole authoritative mutator of a session's
// focused-context state.
type FocusCoordinator interface {
	JoinFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	LeaveFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	PresentFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	RestoreFocus(ctx context.Context, sessionID string) (RestorePlan, error)
}

// RestorePlan is the ordered list of streams and their replay modes to
// apply when a Subscribe handler starts.
type RestorePlan struct {
	Streams          []StreamWithMode
	PresentingStream string // empty if no presenting focus
}

// defaultCoordinator is the production FocusCoordinator implementation.
type defaultCoordinator struct {
	sessionStore session.Store
	eventStore   core.EventStore
	streamSender StreamSender
	cursorLocker CursorLocker
	policies     map[session.FocusKind]FocusKindPolicy

	// Settings stores for preference resolution.
	gameSettings      settings.Settings
	playerSettings    settings.PlayerSettingsStore
	characterSettings settings.CharacterSettingsStore
}

// CoordinatorOption configures a defaultCoordinator.
type CoordinatorOption func(*defaultCoordinator)

// WithSessionStore sets the session store.
func WithSessionStore(store session.Store) CoordinatorOption {
	return func(c *defaultCoordinator) { c.sessionStore = store }
}

// WithEventStore sets the event store.
func WithEventStore(store core.EventStore) CoordinatorOption {
	return func(c *defaultCoordinator) { c.eventStore = store }
}

// WithStreamSender sets the stream sender.
func WithStreamSender(sender StreamSender) CoordinatorOption {
	return func(c *defaultCoordinator) { c.streamSender = sender }
}

// WithCursorLocker sets the cursor locker.
func WithCursorLocker(locker CursorLocker) CoordinatorOption {
	return func(c *defaultCoordinator) { c.cursorLocker = locker }
}

// WithKindPolicy registers a FocusKindPolicy for its kind.
func WithKindPolicy(policy FocusKindPolicy) CoordinatorOption {
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

// NewCoordinator constructs a defaultCoordinator. sessionStore is required.
func NewCoordinator(opts ...CoordinatorOption) (FocusCoordinator, error) {
	c := &defaultCoordinator{
		policies: make(map[session.FocusKind]FocusKindPolicy),
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.sessionStore == nil {
		return nil, oops.Errorf("session store is required")
	}
	return c, nil
}

// policyFor looks up the registered FocusKindPolicy for the given kind.
func (c *defaultCoordinator) policyFor(kind session.FocusKind) (FocusKindPolicy, error) {
	p, ok := c.policies[kind]
	if !ok {
		return nil, oops.Code("FOCUS_KIND_UNREGISTERED").
			With("kind", string(kind)).
			Errorf("no FocusKindPolicy registered for kind %q", kind)
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
) FocusPolicyContext {
	pctx := FocusPolicyContext{
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
