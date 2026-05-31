// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/pkg/errutil"
)

func newStoreForTest(t *testing.T) *emitTokenStore {
	t.Helper()
	// Use the production constructor so its defaults stay covered; tests
	// override .now / .rand / .sweep individually as needed.
	s := newEmitTokenStore()
	s.now = time.Now
	s.rand = rand.Reader
	return s
}

func TestEmitTokenStoreIssueLookupHappyPathCarriesActorAndOwnerPlayer(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	actor := core.Actor{Kind: core.ActorCharacter, ID: "01HCHAR..."}
	ownerPlayer := "01HPLAYER0000000000000000"
	tok, err := s.Issue("plug-A", actor, ownerPlayer)
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	got, gotOwner, ok := s.Lookup("plug-A", tok)
	require.True(t, ok)
	assert.Equal(t, actor, got)
	assert.Equal(t, ownerPlayer, gotOwner,
		"Lookup MUST return the owning player supplied at Issue")
}

func TestEmitTokenStoreLookupReturnsEmptyOwnerPlayerWhenNoneSupplied(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	actor := core.Actor{Kind: core.ActorPlugin, ID: "plug-A"}
	tok, err := s.Issue("plug-A", actor, "")
	require.NoError(t, err)

	got, gotOwner, ok := s.Lookup("plug-A", tok)
	require.True(t, ok)
	assert.Equal(t, actor, got)
	assert.Empty(t, gotOwner,
		"an Issue with no owning player MUST Lookup an empty owner (PLAYER fails closed)")
}

func TestEmitTokenStoreLookupWrongPluginNameReturnsFalse(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	actor := core.Actor{Kind: core.ActorCharacter, ID: "01HCHAR..."}
	tok, err := s.Issue("plug-A", actor, "")
	require.NoError(t, err)

	_, _, ok := s.Lookup("plug-B", tok)
	assert.False(t, ok)
}

func TestEmitTokenStoreLookupUnknownTokenReturnsFalse(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	_, _, ok := s.Lookup("plug-A", "not-a-real-token")
	assert.False(t, ok)
}

func TestEmitTokenStoreLookupExpiredEntryReturnsFalse(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s := newStoreForTest(t)
	s.now = func() time.Time { return now }
	tok, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"}, "")
	require.NoError(t, err)

	// Advance clock past TTL.
	s.now = func() time.Time { return now.Add(s.ttl + time.Second) }
	_, _, ok := s.Lookup("plug-A", tok)
	assert.False(t, ok)
}

func TestEmitTokenStoreRevokeRemovesEntry(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	tok, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"}, "")
	require.NoError(t, err)
	s.Revoke(tok)
	_, _, ok := s.Lookup("plug-A", tok)
	assert.False(t, ok)
}

func TestEmitTokenStoreRevokeIsIdempotent(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	s.Revoke("never-issued")
	tok, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"}, "")
	require.NoError(t, err)
	s.Revoke(tok)
	s.Revoke(tok) // second call must not panic
}

func TestEmitTokenStoreTokenFormat(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	tok, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"}, "")
	require.NoError(t, err)
	assert.Len(t, tok, 22, "token MUST be 22 base64url chars (16 bytes unpadded)")
	decoded, decodeErr := base64.RawURLEncoding.DecodeString(tok)
	require.NoError(t, decodeErr)
	assert.Len(t, decoded, 16)
}

func TestEmitTokenStoreTokenUniqueness(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	const N = 10000
	seen := make(map[string]bool, N)
	for i := 0; i < N; i++ {
		tok, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"}, "")
		require.NoError(t, err)
		require.False(t, seen[tok], "token collision at i=%d", i)
		seen[tok] = true
	}
}

func TestEmitTokenStoreConcurrentIssueLookupSafety(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	const N = 100
	var wg sync.WaitGroup
	// Collect errors via channel — calling require.* inside a worker only
	// terminates the worker goroutine (testing.T.FailNow does runtime.Goexit
	// on the calling goroutine). Failures must be asserted on the main test
	// goroutine after wg.Wait.
	errCh := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"}, "")
			if err != nil {
				errCh <- err
				return
			}
			if _, _, ok := s.Lookup("plug-A", tok); !ok {
				errCh <- errors.New("lookup returned ok=false for freshly-issued token")
				return
			}
			s.Revoke(tok)
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
}

func TestEmitTokenStoreIssueFailsOnRandFailure(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	s.rand = bytes.NewReader(nil) // exhausted reader → io.EOF on Read
	_, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"}, "")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_ISSUE_FAILED")
}

func TestEmitTokenStoreIssueFailsAfterClose(t *testing.T) {
	t.Parallel()
	s := newStoreForTest(t)
	require.NoError(t, s.Close())
	_, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"}, "")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_STORE_CLOSED")
}

func TestEmitTokenStoreSweeperRemovesExpired(t *testing.T) {
	// NOTE: NOT t.Parallel — goleak.VerifyNone observes ALL live goroutines
	// at the moment it runs, including sibling t.Parallel test runners.
	defer goleak.VerifyNone(t)

	// Use an atomic int64 unix-nano holder rather than reassigning s.now —
	// the sweeper goroutine reads s.now() concurrently with the test's clock
	// advance, and overwriting the function pointer would race. Reading
	// atomic.Int64 is race-free; the function value itself never changes.
	now := time.Now()
	var nowUnix atomic.Int64
	nowUnix.Store(now.UnixNano())

	s := newStoreForTest(t)
	s.now = func() time.Time { return time.Unix(0, nowUnix.Load()) }
	s.sweep = 10 * time.Millisecond
	tok, err := s.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"}, "")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	// Advance clock past TTL via atomic store — race-free with sweepExpired.
	nowUnix.Store(now.Add(s.ttl + time.Second).UnixNano())
	// Wait for sweeper to fire.
	require.Eventually(t, func() bool {
		_, _, ok := s.Lookup("plug-A", tok)
		return !ok
	}, 200*time.Millisecond, 5*time.Millisecond, "sweeper should remove expired entry")
}

func TestEmitTokenStoreCloseStopsSweeper(t *testing.T) {
	// NOTE: NOT t.Parallel — see TestEmitTokenStoreSweeperRemovesExpired.
	defer goleak.VerifyNone(t)

	s := newStoreForTest(t)
	s.sweep = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	require.NoError(t, s.Close())
	// goleak.VerifyNone in defer asserts no goroutines leak.
}
