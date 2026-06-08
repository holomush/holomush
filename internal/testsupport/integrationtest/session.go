// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

import (
	"context"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/session"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// Tunable timeouts for the Subscribe-stream transport lifecycle. Set high
// enough to absorb CI latency variance while still failing fast on hangs.
const (
	transportAttachReplayTimeout = 10 * time.Second
	transportDetachExitTimeout   = 5 * time.Second
)

// Session wraps an authenticated or guest game session for privacy integration
// testing. It holds the session metadata set at connect time and exposes
// helpers that delegate to the in-process CoreServer.
//
// Fields marked as "set at connect; NOT mutated by MoveTo" preserve their
// original values so tests can compare before/after state.
type Session struct {
	server *Server

	// SessionID is the game session identifier returned by SelectCharacter.
	SessionID string
	// PlayerID is the ULID of the player account that owns CharacterID. Set at
	// connect time; used by crypto helpers that need the player_id for DEK
	// participant rows / bindings (e.g. SeedSceneDEKParticipant).
	PlayerID ulid.ULID
	// CharacterID is the ULID of the in-game character for this session.
	CharacterID ulid.ULID
	// CharacterName is the display name of the character.
	CharacterName string
	// LocationID is the current location. Updated by MoveTo.
	LocationID ulid.ULID
	// OriginalLocationID is the location at connect time; NOT mutated by MoveTo.
	OriginalLocationID ulid.ULID
	// LocationArrivedAt is the time the session arrived at the current location.
	LocationArrivedAt time.Time
	// SessionCreatedAt is the time the game session was created.
	SessionCreatedAt time.Time

	// Reattached is true when the SessionID was returned by SelectCharacter's
	// reattach branch (i.e. an existing active/detached session row matched
	// FindByCharacter — see internal/grpc/auth_handlers.go:319-356). False on
	// fresh session creation. Allows tests to assert the production-side
	// reattach path was actually taken on a second OpenWebSession call.
	Reattached bool

	// playerSessionToken is the raw bearer token for player-session auth.
	// Kept internal; used by SendCommand / Logout.
	playerSessionToken string

	// Transport state — auto-managed by ConnectAuthed/ConnectGuest/
	// OpenWebSession (via s.attach), cycled by DetachTransport/
	// ReattachTransport. Mutex protects all five fields so the
	// detach-during-reattach race is well-defined.
	transportMu     sync.Mutex
	transportStream *subscribeStream
	transportCancel context.CancelFunc
	// transportDone is closed when the in-flight Subscribe goroutine returns;
	// the error it received (if any) is stored on transportErr.
	transportDone chan struct{}
	transportErr  error
	// transportConnID is the connection_id ULID used for the active Subscribe
	// call (stamped in session_connections). Set in attach(), cleared in
	// teardownTransport(). Used by SetSceneFocus to call SetConnectionFocus.
	transportConnID ulid.ULID
}

// SendCommand dispatches a text command via HandleCommand. Returns the RPC
// transport error if the call itself failed, or a wrapped error if the server
// rejected the command (resp.Success == false). Tests that expect rejection
// should assert on the returned error rather than ignoring it.
func (s *Session) SendCommand(ctx context.Context, cmd string) error {
	resp, err := s.server.coreServer.HandleCommand(ctx, &corev1.HandleCommandRequest{
		PlayerSessionToken: s.playerSessionToken,
		SessionId:          s.SessionID,
		Command:            cmd,
	})
	if err != nil {
		return oops.With("operation", "send_command").With("command", cmd).Wrap(err)
	}
	if !resp.GetSuccess() {
		return oops.Code("COMMAND_REJECTED").
			With("operation", "send_command").
			With("command", cmd).
			With("error_message", resp.GetError()).
			Errorf("command rejected by server: %s", resp.GetError())
	}
	return nil
}

// WaitForEvent blocks until an event matching eventType is received on the
// session's live event stream, the session's transport is detached, or ctx
// is cancelled. Events whose Type does NOT match eventType are skipped (they
// remain consumed from the stream — WaitForEvent advances the cursor).
//
// Returns the first matching event. On ctx cancellation or transport detach,
// the test fails via t.Fatalf rather than returning a nil sentinel; callers
// should ALWAYS treat a non-nil return as the desired event.
//
// The Subscribe stream is wired automatically by ConnectAuthed / ConnectGuest
// / AuthedPlayer.OpenWebSession (and re-wired on ReattachTransport). Tests
// MUST NOT call WaitForEvent on a session whose transport is currently
// detached — DetachTransport blocks new sends; ReattachTransport spins up a
// fresh goroutine that resumes from the durable consumer's last-acked-seq.
func (s *Session) WaitForEvent(ctx context.Context, eventType string) *corev1.EventFrame {
	s.server.t.Helper()
	s.transportMu.Lock()
	stream := s.transportStream
	done := s.transportDone
	s.transportMu.Unlock()
	if stream == nil {
		s.server.t.Fatalf("integrationtest.Session.WaitForEvent: no active transport (call AttachTransport / ReattachTransport first)")
		return nil
	}

	for {
		select {
		case ev := <-stream.events:
			if ev == nil {
				continue
			}
			if ev.GetType() == eventType {
				return ev
			}
			// Type mismatch — keep waiting for the right event.
		case <-done:
			// Subscribe goroutine exited (Detach or error). Surface the
			// stored error if any; otherwise fail with a generic message.
			s.transportMu.Lock()
			err := s.transportErr
			s.transportMu.Unlock()
			if err != nil {
				s.server.t.Fatalf("integrationtest.Session.WaitForEvent: transport exited before matching event %q (err=%v)", eventType, err)
			} else {
				s.server.t.Fatalf("integrationtest.Session.WaitForEvent: transport detached before matching event %q", eventType)
			}
			return nil
		case <-ctx.Done():
			s.server.t.Fatalf("integrationtest.Session.WaitForEvent: ctx cancelled before matching event %q (overflow=%d)", eventType, stream.overflowCount())
			return nil
		}
	}
}

// WaitForSceneActivityBadge blocks until a CONTROL_SIGNAL_SCENE_ACTIVITY
// frame carrying sceneID is received on the session's live stream, the
// transport is detached, or ctx is cancelled. Badges for other scenes are
// skipped (consumed from the buffer but ignored). On failure the test is
// killed via t.Fatalf.
//
// Callers MUST NOT call this on a session whose transport is detached.
func (s *Session) WaitForSceneActivityBadge(ctx context.Context, sceneID string) *corev1.ControlFrame {
	s.server.t.Helper()
	s.transportMu.Lock()
	stream := s.transportStream
	done := s.transportDone
	s.transportMu.Unlock()
	if stream == nil {
		s.server.t.Fatalf("integrationtest.Session.WaitForSceneActivityBadge: no active transport (call AttachTransport first)")
		return nil
	}

	for {
		select {
		case ctrl := <-stream.sceneActivityBadges:
			if ctrl == nil {
				continue
			}
			if ctrl.GetSceneId() == sceneID {
				return ctrl
			}
			// Badge for a different scene — keep waiting.
		case <-done:
			s.transportMu.Lock()
			err := s.transportErr
			s.transportMu.Unlock()
			if err != nil {
				s.server.t.Fatalf("integrationtest.Session.WaitForSceneActivityBadge: transport exited before badge for scene %q (err=%v)", sceneID, err)
			} else {
				s.server.t.Fatalf("integrationtest.Session.WaitForSceneActivityBadge: transport detached before badge for scene %q", sceneID)
			}
			return nil
		case <-ctx.Done():
			s.server.t.Fatalf("integrationtest.Session.WaitForSceneActivityBadge: ctx cancelled before badge for scene %q (overflow=%d)", sceneID, stream.overflowCount())
			return nil
		}
	}
}

// DrainEvents returns all events received within timeout. If no events arrive
// it returns nil (not an error). The smoke test calls this to exercise the
// event-delivery path; an empty result is acceptable.
//
// Honors ctx cancellation: returns immediately if ctx is done before the
// timeout elapses.
//
// Current implementation: waits up to timeout (or ctx cancellation), since
// the Subscribe goroutine fan-out is not yet wired (see TODO iwzt-9).
// Downstream tests that need real event delivery must implement the goroutine
// in that bead.
func (s *Session) DrainEvents(ctx context.Context, timeout time.Duration) []*corev1.EventFrame {
	select {
	case <-ctx.Done():
	case <-time.After(timeout):
	}
	return nil
}

// Logout logs out the player session, invalidating the bearer token and
// deleting the game session. Tears down the Subscribe transport first so
// the in-flight goroutine doesn't race against the session-delete and emit
// confusing "session not found" errors into the test log.
func (s *Session) Logout(ctx context.Context) {
	s.server.t.Helper()
	s.teardownTransport()
	_, err := s.server.coreServer.Logout(ctx, &corev1.LogoutRequest{
		PlayerSessionToken: s.playerSessionToken,
	})
	require.NoError(s.server.t, err, "integrationtest.Session.Logout")
}

// MoveTo simulates a character move by updating the session row's
// location_id + location_arrived_at columns directly in Postgres. Mirrors
// the post-MovementHook state (per holomush-iwzt.4) that production would
// produce without invoking the full world.Service.MoveCharacter pipeline,
// which is out of scope for privacy-floor tests.
//
// Scope: only the `sessions` row is updated. `characters.location_id` is
// NOT changed. Tests that query the world repo (e.g.,
// GetCharactersByLocation) will see stale character placement; tests that
// read via the session store (QueryStreamHistory's hard-gate, presence
// snapshot) see the updated value.
//
// Updates the harness-side Session.LocationID + LocationArrivedAt so test
// assertions read the same values the server-side session row carries.
// OriginalLocationID is never mutated — tests can compare before/after.
func (s *Session) MoveTo(ctx context.Context, newLocationID ulid.ULID) {
	s.server.t.Helper()
	now := time.Now().UTC()
	tag, err := s.server.pool.Exec(ctx,
		`UPDATE sessions SET location_id = $1, location_arrived_at = $2, updated_at = $2 WHERE id = $3`,
		newLocationID.String(), now.UnixNano(), s.SessionID)
	require.NoError(s.server.t, err, "integrationtest.Session.MoveTo")
	require.Equalf(s.server.t, int64(1), tag.RowsAffected(),
		"integrationtest.Session.MoveTo: expected 1 row affected, got %d (sessionID=%s)",
		tag.RowsAffected(), s.SessionID)
	s.LocationID = newLocationID
	s.LocationArrivedAt = now
}

// RefreshFromPersisted re-reads the harness's mutable Session fields
// (LocationID, LocationArrivedAt) from the persisted sessions row.
//
// Connect-time hydration is asymmetric: LocationArrivedAt is sourced
// from the persisted row by all three connect paths
// (ConnectAuthedWithRoles, ConnectGuest, AuthedPlayer.OpenWebSession),
// but LocationID is sourced from the persisted row only by
// OpenWebSession — ConnectAuthed* and ConnectGuest seed it from
// Server.guestStartLocationID, which equals persisted.LocationID
// immediately post-connect but diverges as soon as the DB row's
// location_id is mutated out-of-band. RefreshFromPersisted surfaces
// the DB-side value uniformly.
//
// Used by tests asserting on harness-side state after any path that
// may have mutated the session row out-of-band: Server.MoveTo (which
// does update the struct in-place, so a refresh is a no-op) or the
// direct-SQL helpers Server.SetLocationArrivedAt / Server.ExpireSession
// (which do NOT update the struct).
//
// Scope: only LocationID and LocationArrivedAt are refreshed.
// Identity-bound fields — SessionID, CharacterID, CharacterName,
// OriginalLocationID, SessionCreatedAt, Reattached — are NOT mutated.
// The transport-state fields under transportMu are out of scope; cycle
// DetachTransport/ReattachTransport to manage those.
//
// On lookup failure (session deleted, store error) the call fails the
// test via require.NoError — there is no "soft" path. Tests that
// expect the session row to be gone should assert that condition
// directly rather than calling RefreshFromPersisted.
func (s *Session) RefreshFromPersisted(ctx context.Context) {
	s.server.t.Helper()
	persisted, err := s.server.sessionStore.Get(ctx, s.SessionID)
	require.NoError(s.server.t, err, "integrationtest.Session.RefreshFromPersisted: sessionStore.Get(%s)", s.SessionID)
	s.LocationID = persisted.LocationID
	s.LocationArrivedAt = persisted.LocationArrivedAt
}

// DetachTransport simulates a client disconnect: cancels the in-flight
// Subscribe RPC's stream context (production's Subscribe defer removes the
// session_connections row), waits for the goroutine to exit, then calls the
// production Disconnect RPC to transition the session row to StatusDetached.
//
// Together these mirror what production does when ALL transport connections
// for a session drop — the session row enters its TTL window awaiting either
// a reattach (transport returns within TTL → ReattachCAS flips back to Active)
// or expiry (reaper deletes the row → fresh session on next SelectCharacter).
//
// Idempotent: calling DetachTransport on a session whose transport is already
// detached is a no-op.
func (s *Session) DetachTransport(ctx context.Context) {
	s.server.t.Helper()
	s.transportMu.Lock()
	cancel := s.transportCancel
	done := s.transportDone
	s.transportMu.Unlock()
	if cancel == nil {
		return // already detached
	}

	cancel()
	select {
	case <-done:
	case <-time.After(transportDetachExitTimeout):
		s.server.t.Fatalf("integrationtest.Session.DetachTransport: Subscribe goroutine did not exit within %s", transportDetachExitTimeout)
	}

	s.transportMu.Lock()
	// Nil-guard: between this method's first unlock (above) and the relock
	// here, a concurrent teardownTransport (e.g. via Logout) may have
	// already cleared transportStream. Idempotency would otherwise panic
	// dereferencing nil. transportDone is left in place so a concurrent
	// WaitForEvent can read the close. transportErr is preserved.
	if s.transportStream != nil {
		s.transportStream.close()
	}
	s.transportStream = nil
	s.transportCancel = nil
	s.transportConnID = ulid.ULID{}
	s.transportMu.Unlock()

	// Transition the session row to StatusDetached via the production
	// Disconnect RPC — mirrors what happens when the last transport drops.
	_, err := s.server.coreServer.Disconnect(ctx, &corev1.DisconnectRequest{
		SessionId:          s.SessionID,
		PlayerSessionToken: s.playerSessionToken,
	})
	require.NoError(s.server.t, err, "integrationtest.Session.DetachTransport: Disconnect RPC")
}

// ReattachTransport reopens the Subscribe stream after a DetachTransport.
// Mirrors the client's reconnection flow.
//
// Per spec INV-PRIVACY-3 (Round 3 amendment), reattach MUST NOT change the
// session's LocationArrivedAt — the durable JetStream consumer's
// OptStartTime is immutable post-creation (NATS error 10012) and the
// filter-at-delivery in dispatchDelivery enforces the original floor.
// There is therefore NO post-reattach floor for tests to assert against;
// any pre-Round-3 wording referring to a "LastReattachAt" timestamp does
// not match production semantics.
//
// Production's Subscribe handler runs ReattachCAS automatically when the
// session row is in StatusDetached, transitioning it back to Active.
func (s *Session) ReattachTransport(ctx context.Context) {
	s.server.t.Helper()
	s.attach(ctx)
}

// attach starts a Subscribe RPC against the in-process CoreServer in a
// background goroutine, blocking until the production handler has emitted
// CONTROL_SIGNAL_REPLAY_COMPLETE (signalling the durable consumer is fully
// wired and ready to receive live events). Without that wait, an event
// published immediately after attach could be missed if the durable
// consumer's OpenSession hasn't completed yet.
//
// Each attach uses a fresh connection_id ULID so production's
// session_connections row tracks per-attach lifetime correctly. The
// client_type is "terminal" — the canonical value the web frontend uses
// (internal/web/handler.go:188). The session_store validates against
// {"terminal", "comms_hub", "telnet"} so any other choice is rejected.
func (s *Session) attach(ctx context.Context) {
	s.server.t.Helper()
	s.transportMu.Lock()
	if s.transportCancel != nil {
		s.transportMu.Unlock()
		s.server.t.Fatalf("integrationtest.Session.attach: session %s already has an active transport (call DetachTransport first)", s.SessionID)
		return
	}

	streamCtx, cancel := context.WithCancel(context.Background())
	stream := newSubscribeStream(streamCtx, 0)
	done := make(chan struct{})
	connID := idgen.New()
	s.transportStream = stream
	s.transportCancel = cancel
	s.transportDone = done
	s.transportErr = nil
	s.transportConnID = connID
	s.transportMu.Unlock()
	req := &corev1.SubscribeRequest{
		SessionId:          s.SessionID,
		PlayerSessionToken: s.playerSessionToken,
		ConnectionId:       connID.String(),
		ClientType:         "terminal",
	}

	go func() {
		err := s.server.coreServer.Subscribe(req, stream)
		s.transportMu.Lock()
		s.transportErr = err
		s.transportMu.Unlock()
		close(done)
	}()

	// Block until REPLAY_COMPLETE arrives or the goroutine fails fast.
	select {
	case <-stream.replayDone:
		// Durable consumer wired; safe to return to caller.
	case <-done:
		// Subscribe goroutine returned before REPLAY_COMPLETE — production
		// hit a setup failure. Surface the stored error.
		s.transportMu.Lock()
		err := s.transportErr
		s.transportMu.Unlock()
		s.server.t.Fatalf("integrationtest.Session.attach: Subscribe goroutine exited before REPLAY_COMPLETE (err=%v)", err)
	case <-time.After(transportAttachReplayTimeout):
		cancel()
		s.server.t.Fatalf("integrationtest.Session.attach: REPLAY_COMPLETE not received within %s for session %s", transportAttachReplayTimeout, s.SessionID)
	case <-ctx.Done():
		cancel()
		s.server.t.Fatalf("integrationtest.Session.attach: caller ctx cancelled before REPLAY_COMPLETE for session %s: %v", s.SessionID, ctx.Err())
	}
}

// teardownTransport cancels any active Subscribe goroutine without calling
// Disconnect. Used by Logout (which deletes the session row outright) and
// by Stop-equivalent paths where the session_connections cleanup is implicit
// via CASCADE.
//
// Idempotent: returns immediately if no transport is active.
//
// Asymmetry vs DetachTransport: a wedged goroutine here is logged
// non-fatally and teardownTransport returns anyway. DetachTransport
// Fatalfs on the same condition because it's part of the test's
// observable sequence (the test's next assertion depends on the session
// being Detached), whereas Logout is the cleanup path — failing here
// would mask whatever real assertion the test was trying to make. The
// testcontainer + embedded NATS teardown handles leaked goroutines at
// process exit.
func (s *Session) teardownTransport() {
	s.transportMu.Lock()
	cancel := s.transportCancel
	done := s.transportDone
	stream := s.transportStream
	s.transportStream = nil
	s.transportCancel = nil
	s.transportConnID = ulid.ULID{}
	s.transportMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if stream != nil {
		stream.close()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(transportDetachExitTimeout):
			// Goroutine wedged — log but don't fail; Logout still needs to run.
			s.server.t.Logf("integrationtest.Session.teardownTransport: Subscribe goroutine did not exit within %s for session %s", transportDetachExitTimeout, s.SessionID)
		}
	}

	// Surface any silent overflow drops (holomush-2p6o). subscribeStream.Send
	// is non-blocking on a full events channel — events that arrive while the
	// buffer is full are silently dropped with an overflow-counter increment.
	// Without this assertion a test that emits more events than the buffer
	// holds AND doesn't drain via WaitForEvent would lose events undetectably.
	// Use t.Errorf (non-fatal) rather than t.Fatalf so any in-progress test
	// assertion completes — Logout is the cleanup path, NOT the assertion
	// path; failing fatally here would mask whatever the test was trying to
	// verify. Tests intentionally exceeding the buffer should size up via a
	// future ConnectOption (filed in 2p6o follow-up scope if needed).
	if stream != nil {
		if n := stream.overflowCount(); n > 0 {
			s.server.t.Errorf(
				"integrationtest.Session.teardownTransport: %d Subscribe events dropped due to full buffer (size=%d) for session %s — "+
					"tests asserting on durable replay may have silently lost events. "+
					"Either drain via WaitForEvent more aggressively or surface a buffer-size opt-in.",
				n, defaultEventBufferSize, s.SessionID,
			)
		}
	}
}

// transportActive returns true if a Subscribe goroutine is currently running
// for this session. Test helper for assertions.
func (s *Session) transportActive() bool {
	s.transportMu.Lock()
	defer s.transportMu.Unlock()
	return s.transportCancel != nil
}

// CreateScene creates a scene owned by this session's character via the
// loaded core-scenes SceneService and returns its ULID.
func (s *Session) CreateScene(ctx context.Context, locationID ulid.ULID) ulid.ULID {
	s.server.t.Helper()
	resp, err := s.server.SceneServiceClient().CreateScene(ctx, &scenev1.CreateSceneRequest{
		CharacterId: s.CharacterID.String(),
		Title:       "test scene",
		LocationId:  locationID.String(),
		Visibility:  "open",
	})
	require.NoError(s.server.t, err, "integrationtest.Session.CreateScene")
	// core-scenes mints bare ULID scene ids (plugins/core-scenes/service.go:1113,
	// holomush-y5inx). The returned id parses directly — no prefix to strip.
	id, err := ulid.Parse(resp.GetScene().GetId())
	require.NoError(s.server.t, err, "integrationtest.Session.CreateScene: parse scene id")
	return id
}

// JoinScene adds a FocusMembership{Kind: Scene, TargetID: sceneID} to the
// session's focus_memberships JSONB, stamped with JoinedAt = time.Now().UTC().
// Returns the canonical JoinedAt so tests can assert against the exact floor
// that streamScopeFloor reads — avoids wall-clock skew between the mutator's
// internal time.Now() and a caller-side snapshot.
//
// Bypasses the production FocusCoordinator path
// (internal/grpc/focus/join.go::defaultCoordinator.JoinFocus): the
// coordinator additionally runs the scene policy (OnJoin returns join
// streams) and notifies the streamSender of the new subscription. JoinFocus
// itself does NOT consult the ABAC engine — scene-stream reads in
// QueryStreamHistory are gated purely by the I-17 membership check
// (sessionHasMembership) and the temporal scope floor (streamScopeFloor),
// both of which read directly from session.FocusMemberships. The
// privacy-floor tests care about the floor, not the subscription wiring,
// so the direct store update is observationally equivalent for the
// authorization paths under test.
func (s *Session) JoinScene(ctx context.Context, sceneID ulid.ULID) time.Time {
	s.server.t.Helper()
	now := time.Now().UTC()
	err := s.server.sessionStore.UpdateFocusMemberships(ctx, s.SessionID, session.NewFocusMutator(
		func(current []session.FocusMembership, presenting *session.FocusKey) ([]session.FocusMembership, *session.FocusKey, error) {
			current = append(current, session.FocusMembership{
				Kind:     session.FocusKindScene,
				TargetID: sceneID,
				JoinedAt: now,
			})
			return current, presenting, nil
		},
	))
	require.NoError(s.server.t, err, "integrationtest.Session.JoinScene: update focus memberships")
	return now
}

// SceneActivityBadgeCount returns the number of SCENE_ACTIVITY control frames
// currently buffered on the session's badge channel, without blocking. Used by
// tests that assert a session receives NO badge (non-members, etc.).
// The count is taken at the moment of the call; concurrent deliveries may
// race — callers should allow brief settle time (e.g. after an emit).
func (s *Session) SceneActivityBadgeCount() int {
	s.transportMu.Lock()
	stream := s.transportStream
	s.transportMu.Unlock()
	if stream == nil {
		return 0
	}
	return len(stream.sceneActivityBadges)
}

// SetSceneFocus updates this connection's FocusKey in the session store to
// point at sceneID. The session must already have sceneID in its FocusMemberships
// (JoinScene first). This bypasses the FocusCoordinator's filter-delta delivery
// path (which would also update the Subscribe loop's active filter set); it is
// sufficient for badge-downgrade tests, which only need GetConnection().FocusKey
// to return the right value inside dispatchDelivery.
//
// Requires an active transport (attach must have been called first) so that
// transportConnID is set.
func (s *Session) SetSceneFocus(ctx context.Context, sceneID ulid.ULID) {
	s.server.t.Helper()
	s.transportMu.Lock()
	connID := s.transportConnID
	s.transportMu.Unlock()
	if connID == (ulid.ULID{}) {
		s.server.t.Fatalf("integrationtest.Session.SetSceneFocus: no active transport (call AttachTransport / ConnectAuthed first)")
		return
	}
	fk := &session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}
	m := session.NewSessionConnectionMutator(func(info session.Info, conn session.Connection) (session.Info, session.Connection, error) {
		conn.FocusKey = fk
		return info, conn, nil
	})
	err := s.server.sessionStore.UpdateSessionConnection(ctx, s.SessionID, connID, m)
	require.NoError(s.server.t, err, "integrationtest.Session.SetSceneFocus: UpdateSessionConnection")
}

// QueryStreamHistory fetches the event history for the given stream subject.
// Returns the events from the response (may be empty if no history exists).
// The caller must pass a stream the session is authorized to read; access
// denials propagate as test failures via the returned error.
func (s *Session) QueryStreamHistory(ctx context.Context, stream string) ([]*corev1.EventFrame, error) {
	resp, err := s.server.coreServer.QueryStreamHistory(ctx, &corev1.QueryStreamHistoryRequest{
		SessionId: s.SessionID,
		Stream:    stream,
	})
	if err != nil {
		return nil, err
	}
	return resp.GetEvents(), nil
}

// QueryStreamHistoryBounded fetches event history with a NotAfterMs
// ceiling (holomush-iu8j cursor-bounded backfill). Mirrors the
// production client's connect-time backfill call where notAfterMs is
// the Subscribe attach moment received on the REPLAY_COMPLETE
// ControlFrame. Passing 0 is equivalent to QueryStreamHistory (no
// upper bound, back-compat with legacy clients).
func (s *Session) QueryStreamHistoryBounded(ctx context.Context, stream string, notAfterMs int64) ([]*corev1.EventFrame, error) {
	resp, err := s.server.coreServer.QueryStreamHistory(ctx, &corev1.QueryStreamHistoryRequest{
		SessionId:  s.SessionID,
		Stream:     stream,
		NotAfterMs: notAfterMs,
	})
	if err != nil {
		return nil, err
	}
	return resp.GetEvents(), nil
}

// AttachMomentMs returns the server-side attach moment captured from
// the most recent REPLAY_COMPLETE ControlFrame on the session's
// Subscribe transport (holomush-iu8j). Returns 0 if no transport has
// attached yet, or if the server sent 0 (legacy back-compat sentinel).
//
// Production clients pass this value as not_after_ms on backfill
// (WebQueryStreamHistory) calls so backfill returns only events that
// existed before the live Subscribe stream attached — closing the
// connect-time replay/backfill race.
//
// Synchronization: ConnectAuthed / ConnectGuest / OpenWebSession
// (via Session.attach) block until REPLAY_COMPLETE arrives, so a
// post-Connect caller is guaranteed to see the stamped value. Tests
// that call AttachMomentMs after DetachTransport followed by
// ReattachTransport see the value from the LATEST reattach.
func (s *Session) AttachMomentMs() int64 {
	s.transportMu.Lock()
	stream := s.transportStream
	s.transportMu.Unlock()
	if stream == nil {
		return 0
	}
	return stream.getAttachMomentMs()
}

// EmitDirectEvent publishes an event to the embedded bus, bypassing the
// command dispatcher (which the harness wires with an empty registry). Tests
// use this to inject events into a stream so downstream QueryStreamHistory
// reads have material to operate on. The event is published via the same
// production path callers use — eventbus.Subsystem.Publisher.Publish — so
// JetStream-side persistence and audit semantics match production.
//
// stream is a domain-relative dot subject (e.g., "location.01ABC"); the
// helper qualifies it to the JetStream-native subject. Returns once the
// underlying Publish completes — JetStream's ack guarantees the event is
// queryable on return.
func (s *Session) EmitDirectEvent(ctx context.Context, stream, evType string, payload []byte) error {
	sub, err := eventbus.Qualify(s.server.bus.Bus.GameID(), stream)
	if err != nil {
		return oops.With("stream", stream).Wrap(err)
	}
	event := eventbus.NewEvent(
		sub,
		eventbus.Type(evType),
		eventbus.Actor{Kind: eventbus.ActorKindCharacter, ID: s.CharacterID},
		payload,
	)
	pub := s.server.bus.Bus.Publisher()
	if pub == nil {
		return oops.Errorf("integrationtest.Session.EmitDirectEvent: bus has no publisher (JS not ready)")
	}
	return pub.Publish(ctx, event) //nolint:wrapcheck // test helper: callers see bus errors directly
}

// ListFocusPresence calls the snapshot RPC for the session's current focus
// (location-scoped) and returns the response. The caller is responsible for
// asserting on response.Entries / response.Context / response.ContextId.
// Access denials propagate as test failures via the returned error.
func (s *Session) ListFocusPresence(ctx context.Context) (*corev1.ListFocusPresenceResponse, error) {
	return s.server.coreServer.ListFocusPresence(ctx, &corev1.ListFocusPresenceRequest{
		SessionId:          s.SessionID,
		PlayerSessionToken: s.playerSessionToken,
	})
}

// AuthedPlayer represents a named player (with hashed password) that can
// open multiple concurrent game sessions for the same character, used by
// multi-session continuity tests (iwzt.9+).
type AuthedPlayer struct {
	// PlayerID is the ULID of the player account.
	PlayerID ulid.ULID
	// CharacterID is the ULID of the player's primary character.
	CharacterID ulid.ULID
	// LocationID is the starting location of the character.
	LocationID ulid.ULID

	server *Server

	// rawToken is the active player-session bearer token.
	rawToken string
}

// OpenWebSession opens a game session for this AuthedPlayer via the
// production SelectCharacter handler. On the first call, SelectCharacter
// creates a fresh sessions row with LocationArrivedAt = now. On
// subsequent calls (with the existing active/detached session matched by
// FindByCharacter at internal/store/session_store.go:340-354), the
// handler reattaches and returns the SAME SessionID — per spec §5 row 2
// and the schema-enforced one-session-per-character invariant
// (idx_sessions_active_character). LocationArrivedAt is preserved across
// reattach per INV-PRIVACY-3.
//
// Returns a Session hydrated from the persisted row so the harness-side
// LocationArrivedAt / SessionCreatedAt fields reflect the canonical
// server-side values — the same hydration pattern ConnectAuthedWithRoles
// uses for the same reason (per CodeRabbit thread on PR #4048).
func (p *AuthedPlayer) OpenWebSession(ctx context.Context) *Session {
	p.server.t.Helper()
	selResp, err := p.server.coreServer.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: p.rawToken,
		CharacterId:        p.CharacterID.String(),
	})
	require.NoError(p.server.t, err, "integrationtest.AuthedPlayer.OpenWebSession: SelectCharacter RPC")
	require.True(p.server.t, selResp.GetSuccess(),
		"integrationtest.AuthedPlayer.OpenWebSession: SelectCharacter failed: %s", selResp.GetErrorMessage())

	persisted, getErr := p.server.sessionStore.Get(ctx, selResp.GetSessionId())
	require.NoError(p.server.t, getErr, "integrationtest.AuthedPlayer.OpenWebSession: read persisted session")

	// Source LocationID from the persisted session row, NOT from
	// AuthedPlayer.LocationID (set once at AuthedPlayer construction).
	// A future test that calls Session.MoveTo between OpenWebSession calls
	// would otherwise return a Session with stale LocationID but fresh
	// LocationArrivedAt — inconsistent. The persisted row is the canonical
	// source for both.
	sess := &Session{
		server:             p.server,
		SessionID:          selResp.GetSessionId(),
		PlayerID:           p.PlayerID,
		CharacterID:        p.CharacterID,
		CharacterName:      selResp.GetCharacterName(),
		LocationID:         persisted.LocationID,
		OriginalLocationID: persisted.LocationID,
		LocationArrivedAt:  persisted.LocationArrivedAt,
		SessionCreatedAt:   persisted.CreatedAt,
		Reattached:         selResp.GetReattached(),
		playerSessionToken: p.rawToken,
	}
	sess.attach(ctx)
	return sess
}

// OpenTelnetSession opens a game session simulating a telnet client.
//
// Production note: web and telnet share the same underlying SelectCharacter
// path; the only meaningful difference is the client_type recorded on the
// session_connections row by Subscribe (internal/grpc/server.go:749-756).
// Since iwzt.17's QueryStreamHistory assertions don't observe
// session_connections, this helper is currently TODO-fatal — callers wanting
// to exercise the live-Subscribe transport differentiation belong to iwzt.16's
// scope (Subscribe goroutine fan-out + per-connection DetachTransport).
func (p *AuthedPlayer) OpenTelnetSession(_ context.Context) *Session {
	p.server.t.Fatalf("integrationtest.AuthedPlayer.OpenTelnetSession: TODO iwzt-16 — telnet transport differentiation requires Subscribe goroutine wiring")
	return nil
}
