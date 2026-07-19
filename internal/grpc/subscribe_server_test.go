// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/session"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// concurrentSubscribeStream is a thread-safe variant of fakeSubscribeStream
// for tests that run Subscribe() in a goroutine concurrently with polling.
type concurrentSubscribeStream struct {
	mu   sync.Mutex
	ctx  context.Context //nolint:containedctx // test stub
	sent []*corev1.SubscribeResponse
	err  error
}

func (c *concurrentSubscribeStream) Context() context.Context       { return c.ctx }
func (c *concurrentSubscribeStream) SendHeader(_ metadata.MD) error { return nil }
func (c *concurrentSubscribeStream) SetHeader(_ metadata.MD) error  { return nil }
func (c *concurrentSubscribeStream) SetTrailer(_ metadata.MD)       {}
func (c *concurrentSubscribeStream) SendMsg(_ any) error            { return nil }
func (c *concurrentSubscribeStream) RecvMsg(_ any) error            { return nil }
func (c *concurrentSubscribeStream) Send(r *corev1.SubscribeResponse) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.sent = append(c.sent, r)
	return nil
}

func (c *concurrentSubscribeStream) sentLen() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

func (c *concurrentSubscribeStream) first() *corev1.SubscribeResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) == 0 {
		return nil
	}
	return c.sent[0]
}

// fakeSubscriber is a full eventbus.Subscriber stub whose OpenSession returns
// a caller-supplied fakeSessionStream.
type fakeSubscriber struct {
	stream  eventbus.SessionStream
	openErr error
	opens   int
}

func (f *fakeSubscriber) OpenSession(_ context.Context, _ string, _ eventbus.SessionIdentity, _ []eventbus.Subject, _ time.Time) (eventbus.SessionStream, error) {
	f.opens++
	if f.openErr != nil {
		return nil, f.openErr
	}
	return f.stream, nil
}

func TestSubscribeOpenSessionErrorSurfacesWrapped(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	info := &session.Info{
		ID:          "s1",
		ExpiresAt:   &future,
		Status:      session.StatusActive,
		CharacterID: core.NewULID(),
	}
	sub := &fakeSubscriber{openErr: errors.New("js down")}
	s := &CoreServer{
		subscriber:        sub,
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"s1": info}),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	s.subscribeHandler = s.newSubscribeHandler()
	err := s.Subscribe(&corev1.SubscribeRequest{
		SessionId:          "s1",
		PlayerSessionToken: testPlayerSessionToken,
	}, &fakeSubscribeStream{ctx: context.Background()})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SUBSCRIBE_FAILED", o.Code())
	assert.Equal(t, 1, sub.opens)
}

func TestSubscribeHappyPathSendsReplayCompleteAndReturnsOnCtxCancel(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	info := &session.Info{
		ID:          "s1",
		ExpiresAt:   &future,
		Status:      session.StatusActive,
		CharacterID: core.NewULID(),
		// LocationID zero — avoids locationFollower.sendSynthetic path.
	}
	bs := newFakeSessionStream()
	sub := &fakeSubscriber{stream: bs}
	s := &CoreServer{
		subscriber:        sub,
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"s1": info}),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	s.subscribeHandler = s.newSubscribeHandler()

	ctx, cancel := context.WithCancel(context.Background())
	stream := &concurrentSubscribeStream{ctx: ctx}
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Subscribe(&corev1.SubscribeRequest{
			SessionId:          "s1",
			PlayerSessionToken: testPlayerSessionToken,
		}, stream)
	}()

	// Wait for REPLAY_COMPLETE to land, then cancel.
	require.Eventually(t, func() bool {
		return stream.sentLen() >= 1
	}, 2*time.Second, 10*time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return after ctx cancel")
	}
	require.GreaterOrEqual(t, stream.sentLen(), 1)
	assert.Equal(t, corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE, stream.first().GetControl().GetSignal())
}

func TestSubscribeReattachesDetachedSession(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	info := &session.Info{
		ID:          "s1",
		ExpiresAt:   &future,
		Status:      session.StatusDetached,
		CharacterID: core.NewULID(),
	}
	bs := newFakeSessionStream()
	sub := &fakeSubscriber{stream: bs}
	s := &CoreServer{
		subscriber:        sub,
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"s1": info}),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	s.subscribeHandler = s.newSubscribeHandler()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &concurrentSubscribeStream{ctx: ctx}
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Subscribe(&corev1.SubscribeRequest{
			SessionId:          "s1",
			PlayerSessionToken: testPlayerSessionToken,
		}, stream)
	}()

	require.Eventually(t, func() bool {
		return stream.sentLen() >= 1
	}, 2*time.Second, 10*time.Millisecond)
	cancel()
	// Bound the wait so the test fails fast if Subscribe stops returning
	// on cancellation instead of hanging until the package timeout. Assert
	// the returned value explicitly — a NoError check pins the happy-path
	// cancellation contract (Subscribe wraps ctx.Canceled into nil per the
	// gRPC conventions used by the other Subscribe tests in this file).
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return after ctx cancel")
	}
}

func TestSubscribeRejectsBadConnectionID(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	info := &session.Info{
		ID:          "s1",
		ExpiresAt:   &future,
		Status:      session.StatusActive,
		CharacterID: core.NewULID(),
	}
	bs := newFakeSessionStream()
	sub := &fakeSubscriber{stream: bs}
	s := &CoreServer{
		subscriber:        sub,
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"s1": info}),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	s.subscribeHandler = s.newSubscribeHandler()

	err := s.Subscribe(&corev1.SubscribeRequest{
		SessionId:          "s1",
		PlayerSessionToken: testPlayerSessionToken,
		ConnectionId:       "not-a-ulid",
		ClientType:         "telnet",
	}, &fakeSubscribeStream{ctx: context.Background()})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SUBSCRIBE_INVALID_CONNECTION", o.Code())
}

func TestSubscribeRejectsConnectionIDWithoutClientType(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	info := &session.Info{
		ID:          "s1",
		ExpiresAt:   &future,
		Status:      session.StatusActive,
		CharacterID: core.NewULID(),
	}
	bs := newFakeSessionStream()
	sub := &fakeSubscriber{stream: bs}
	s := &CoreServer{
		subscriber:        sub,
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"s1": info}),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	s.subscribeHandler = s.newSubscribeHandler()

	connID := core.NewULID().String()
	err := s.Subscribe(&corev1.SubscribeRequest{
		SessionId:          "s1",
		PlayerSessionToken: testPlayerSessionToken,
		ConnectionId:       connID,
		// ClientType missing
	}, &fakeSubscribeStream{ctx: context.Background()})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SUBSCRIBE_INVALID_CONNECTION", o.Code())
}

// errAddConnStore wraps a session.Store and forces AddConnection to fail;
// every other method delegates to the embedded store. Used to drive the
// Subscribe fail-fast path (holomush-x6tr).
type errAddConnStore struct {
	session.Store
	err error
}

func (e errAddConnStore) AddConnection(context.Context, *session.Connection) error { return e.err }

// TestSubscribeAddConnectionErrorFailsFastWithoutOpeningSession pins
// holomush-x6tr: when AddConnection fails, Subscribe MUST abort with
// SUBSCRIBE_ADD_CONNECTION_FAILED before opening the bus session, leaving no
// untracked ("zombie") connection. A silent log-and-continue (the original
// bug) would let CountConnections later undercount and prematurely
// detach/delete the session.
func TestSubscribeAddConnectionErrorFailsFastWithoutOpeningSession(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	info := &session.Info{
		ID:          "s1",
		ExpiresAt:   &future,
		Status:      session.StatusActive,
		CharacterID: core.NewULID(),
	}
	sub := &fakeSubscriber{stream: newFakeSessionStream()}
	s := &CoreServer{
		subscriber: sub,
		sessionStore: errAddConnStore{
			Store: newTestSessionStore(t, map[string]*session.Info{"s1": info}),
			err:   errors.New("connection insert failed"),
		},
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	s.subscribeHandler = s.newSubscribeHandler()

	err := s.Subscribe(&corev1.SubscribeRequest{
		SessionId:          "s1",
		PlayerSessionToken: testPlayerSessionToken,
		ConnectionId:       core.NewULID().String(),
		ClientType:         "terminal",
	}, &fakeSubscribeStream{ctx: context.Background()})

	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SUBSCRIBE_ADD_CONNECTION_FAILED", o.Code())
	assert.Equal(t, 0, sub.opens,
		"fail-fast: the bus session MUST NOT open after AddConnection fails")
}

var _ eventbus.Subscriber = (*fakeSubscriber)(nil)

// concurrentFakeSubscriber is a thread-safe eventbus.Subscriber for tests
// that fire multiple concurrent Subscribe RPCs through the same CoreServer.
type concurrentFakeSubscriber struct {
	mu      sync.Mutex
	opens   int
	openErr error
}

func (c *concurrentFakeSubscriber) OpenSession(_ context.Context, _ string, _ eventbus.SessionIdentity, _ []eventbus.Subject, _ time.Time) (eventbus.SessionStream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.opens++
	if c.openErr != nil {
		return nil, c.openErr
	}
	// Return a fresh fake stream per OpenSession call so concurrent
	// Subscribe invocations don't share Next/ack state through one stream.
	return newFakeSessionStream(), nil
}

var _ eventbus.Subscriber = (*concurrentFakeSubscriber)(nil)

// setupSubscribeTestServer creates a CoreServer backed by a real
// SessionStreamRegistry, pre-seeded with a single session entry. Returns
// both so tests can inspect registry state after Subscribe calls.
func setupSubscribeTestServer(t *testing.T) (*CoreServer, *SessionStreamRegistry) {
	t.Helper()
	future := time.Now().Add(time.Hour)
	registry := NewSessionStreamRegistry()
	sub := &concurrentFakeSubscriber{}
	s := &CoreServer{
		subscriber: sub,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			"sess-rbcid": {
				ID:          "sess-rbcid",
				ExpiresAt:   &future,
				Status:      session.StatusActive,
				CharacterID: core.NewULID(),
			},
		}),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
		streamRegistry:    registry,
	}
	s.subscribeHandler = s.newSubscribeHandler()
	return s, registry
}

// waitForRegistrations polls until at least n ConnectionIDs are registered
// for sessionID in registry, or the test deadline is reached.
func waitForRegistrations(t *testing.T, registry *SessionStreamRegistry, sessionID string, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		registry.mu.Lock()
		defer registry.mu.Unlock()
		return len(registry.connections[sessionID]) >= n
	}, 5*time.Second, 10*time.Millisecond, "timed out waiting for %d connection registrations", n)
}

// fakeBindingRepo implements BindingRepo for testing. Create panics (not used
// in Subscribe path); Current returns the canned bindingID or error.
type fakeBindingRepo struct {
	bindingID string
	err       error
}

func (f *fakeBindingRepo) Create(_ context.Context, _, _, _ string) (string, error) {
	panic("fakeBindingRepo.Create: not implemented in Subscribe tests")
}

func (f *fakeBindingRepo) Current(_ context.Context, _ string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.bindingID, nil
}

var _ BindingRepo = (*fakeBindingRepo)(nil)

func TestSubscribeBindingLookupFailureReturnsError(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	charID := core.NewULID()
	playerID := core.NewULID()
	info := &session.Info{
		ID:          "s1",
		ExpiresAt:   &future,
		Status:      session.StatusActive,
		CharacterID: charID,
		PlayerID:    playerID,
	}
	bs := newFakeSessionStream()
	sub := &fakeSubscriber{stream: bs}
	s := &CoreServer{
		subscriber:        sub,
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"s1": info}),
		playerSessionRepo: newFakePlayerSessionRepo(playerID),
		bindings:          &fakeBindingRepo{err: errors.New("db down")},
		cryptoActive:      true, // required to activate binding lookup (KEK-presence gate)
	}
	s.subscribeHandler = s.newSubscribeHandler()
	err := s.Subscribe(&corev1.SubscribeRequest{
		SessionId:          "s1",
		PlayerSessionToken: testPlayerSessionToken,
	}, &fakeSubscribeStream{ctx: context.Background()})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SUBSCRIBE_BINDING_LOOKUP_FAILED", o.Code())
	assert.Equal(t, 0, sub.opens, "OpenSession must not be called after binding lookup failure")
}

func TestSubscribePassesNonZeroSessionIdentityWhenBindingsWired(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	charID := core.NewULID()
	playerID := core.NewULID()
	info := &session.Info{
		ID:          "s1",
		ExpiresAt:   &future,
		Status:      session.StatusActive,
		CharacterID: charID,
		PlayerID:    playerID,
	}
	bs := newFakeSessionStream()
	bindingID := "bnd-001"
	identitySub := &identityCapturingSubscriber{stream: bs}
	s := &CoreServer{
		subscriber:        identitySub,
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"s1": info}),
		playerSessionRepo: newFakePlayerSessionRepo(playerID),
		bindings:          &fakeBindingRepo{bindingID: bindingID},
		cryptoActive:      true, // required to activate binding lookup (KEK-presence gate)
	}
	s.subscribeHandler = s.newSubscribeHandler()

	ctx, cancel := context.WithCancel(context.Background())
	stream := &concurrentSubscribeStream{ctx: ctx}
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Subscribe(&corev1.SubscribeRequest{
			SessionId:          "s1",
			PlayerSessionToken: testPlayerSessionToken,
		}, stream)
	}()

	require.Eventually(t, func() bool {
		return stream.sentLen() >= 1
	}, 2*time.Second, 10*time.Millisecond)
	cancel()
	subscribeErr := <-errCh
	// Subscribe returns nil (clean shutdown) or context.Canceled when the
	// client cancels; neither is a test failure. We capture the value here
	// to prevent the goroutine result from being silently discarded.
	require.True(t, subscribeErr == nil || errors.Is(subscribeErr, context.Canceled),
		"unexpected Subscribe error: %v", subscribeErr)

	assert.Equal(t, 1, identitySub.opens)
	assert.Equal(t, eventbus.IdentityKindCharacter, identitySub.capturedIdentity.Kind)
	assert.Equal(t, playerID.String(), identitySub.capturedIdentity.PlayerID)
	assert.Equal(t, charID.String(), identitySub.capturedIdentity.CharacterID)
	assert.Equal(t, bindingID, identitySub.capturedIdentity.BindingID)
}

// identityCapturingSubscriber is an eventbus.Subscriber that records the
// SessionIdentity passed to OpenSession for assertion in binding-wiring tests.
type identityCapturingSubscriber struct {
	stream           eventbus.SessionStream
	opens            int
	capturedIdentity eventbus.SessionIdentity
}

func (c *identityCapturingSubscriber) OpenSession(_ context.Context, _ string, identity eventbus.SessionIdentity, _ []eventbus.Subject, _ time.Time) (eventbus.SessionStream, error) {
	c.opens++
	c.capturedIdentity = identity
	return c.stream, nil
}

var _ eventbus.Subscriber = (*identityCapturingSubscriber)(nil)

// TestSubscribeReattachCAS_PreservesLocationArrivedAt asserts the
// session-row-as-continuity rule (spec §5 row 3 + INV-PRIVACY-3, amended
// 2026-05-18): transport-level reattach via Subscribe.ReattachCAS leaves
// LocationArrivedAt UNCHANGED. The session row exists across the
// disconnect; the floor was set at session-create and is only advanced
// by character-move (§5 row 5).
func TestSubscribeReattachCAS_PreservesLocationArrivedAt(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour)
	originalArrival := time.Now().Add(-2 * time.Hour)
	info := &session.Info{
		ID:                "s1",
		ExpiresAt:         &future,
		Status:            session.StatusDetached,
		CharacterID:       core.NewULID(),
		LocationArrivedAt: originalArrival,
	}
	bs := newFakeSessionStream()
	sub := &fakeSubscriber{stream: bs}
	sessStore := newTestSessionStore(t, map[string]*session.Info{"s1": info})
	s := &CoreServer{
		subscriber:        sub,
		sessionStore:      sessStore,
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	s.subscribeHandler = s.newSubscribeHandler()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &concurrentSubscribeStream{ctx: ctx}
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Subscribe(&corev1.SubscribeRequest{
			SessionId:          "s1",
			PlayerSessionToken: testPlayerSessionToken,
		}, stream)
	}()

	require.Eventually(t, func() bool {
		return stream.sentLen() >= 1
	}, 2*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return after ctx cancel")
	}

	stored, err := sessStore.Get(context.Background(), "s1")
	require.NoError(t, err)
	assert.True(t, stored.LocationArrivedAt.Equal(originalArrival),
		"LocationArrivedAt MUST be unchanged on ReattachCAS (spec §5 row 3, INV-PRIVACY-3); got %v, want %v",
		stored.LocationArrivedAt, originalArrival)
	assert.Equal(t, session.StatusActive, stored.Status,
		"ReattachCAS MUST flip status back to Active")
}

// TestSubscribe_RegistersByConnectionID verifies INV-SCENE-24: when Subscribe
// is called with a ConnectionId, the SessionStreamRegistry MUST register
// the connection via RegisterConnection (per-connection routing), not just
// the session-wide Register path. Two concurrent subscribers on the same
// session with distinct ConnectionIds both appear in registry.connections.
func TestSubscribe_RegistersByConnectionID(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	srv, registry := setupSubscribeTestServer(t)

	sessionID := "sess-rbcid"
	connA := ulid.Make()
	connB := ulid.Make()

	// Fire two Subscribe RPCs in goroutines (Subscribe is a streaming
	// RPC; we let it register, then cancel ctx to deregister cleanly).
	subA, cancelA := context.WithCancel(ctx)
	subB, cancelB := context.WithCancel(ctx)
	defer cancelA()
	defer cancelB()

	go func() {
		_ = srv.Subscribe(&corev1.SubscribeRequest{
			SessionId:          sessionID,
			PlayerSessionToken: testPlayerSessionToken,
			ConnectionId:       connA.String(),
			ClientType:         "terminal",
		}, &fakeSubscribeStream{ctx: subA})
	}()
	go func() {
		_ = srv.Subscribe(&corev1.SubscribeRequest{
			SessionId:          sessionID,
			PlayerSessionToken: testPlayerSessionToken,
			ConnectionId:       connB.String(),
			ClientType:         "comms_hub",
		}, &fakeSubscribeStream{ctx: subB})
	}()

	// Wait until both registrations have landed.
	waitForRegistrations(t, registry, sessionID, 2)

	// INV-SCENE-24: the registry tracks both connections as distinct
	// (sessionID, connectionID) keys in the per-connection routing map.
	assert.True(t, registry.HasConnection(sessionID, connA),
		"Subscribe MUST register connA via RegisterConnection")
	assert.True(t, registry.HasConnection(sessionID, connB),
		"Subscribe MUST register connB via RegisterConnection")
}
