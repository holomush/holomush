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

func (f *fakeSubscriber) OpenSession(_ context.Context, _ string, _ eventbus.SessionIdentity, _ []eventbus.Subject) (eventbus.SessionStream, error) {
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

var _ eventbus.Subscriber = (*fakeSubscriber)(nil)
