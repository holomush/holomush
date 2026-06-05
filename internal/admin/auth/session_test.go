// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package adminauth_test

import (
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adminauth "github.com/holomush/holomush/internal/admin/auth"
	"github.com/holomush/holomush/pkg/errutil"
)

// fakeClock is a deterministic Clock implementation for TTL tests.
type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// TestSessionStoreEmptiedOnConstruction — INV-CRYPTO-70.
func TestSessionStoreEmptiedOnConstruction(t *testing.T) {
	fc := &fakeClock{t: time.Unix(1700000000, 0)}
	s := adminauth.NewSessionStore(fc, 10*time.Minute)
	_, err := s.Get("any-token-value")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DENY_SESSION_INVALID")
}

// TestSessionStoreIssueAndGetReturnsIdentity — happy path.
func TestSessionStoreIssueAndGetReturnsIdentity(t *testing.T) {
	fc := &fakeClock{t: time.Unix(1700000000, 0)}
	s := adminauth.NewSessionStore(fc, 10*time.Minute)
	id := adminauth.OperatorIdentity{PlayerID: "01HZA", AuthProviderName: "ingame-creds-totp", TOTPVerified: true}

	token, expires, err := s.Issue(id)
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.True(t, expires.After(fc.t))

	got, err := s.Get(token)
	require.NoError(t, err)
	assert.Equal(t, id, got)
}

// TestSessionStoreRejectsExpiredToken — INV-CRYPTO-69.
func TestSessionStoreRejectsExpiredToken(t *testing.T) {
	fc := &fakeClock{t: time.Unix(1700000000, 0)}
	s := adminauth.NewSessionStore(fc, 10*time.Minute)
	id := adminauth.OperatorIdentity{PlayerID: "01HZA"}

	token, _, err := s.Issue(id)
	require.NoError(t, err)

	fc.Advance(11 * time.Minute) // beyond 10-min TTL

	_, err = s.Get(token)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DENY_SESSION_EXPIRED")

	// Cleanup-on-Get: subsequent lookup is INVALID, not EXPIRED.
	_, err = s.Get(token)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T", err)
	assert.Equal(t, "DENY_SESSION_INVALID", oopsErr.Code())
}

// TestSessionStoreRevoke removes a token.
func TestSessionStoreRevoke(t *testing.T) {
	fc := &fakeClock{t: time.Unix(1700000000, 0)}
	s := adminauth.NewSessionStore(fc, 10*time.Minute)
	id := adminauth.OperatorIdentity{PlayerID: "01HZA"}
	token, _, _ := s.Issue(id)

	require.NoError(t, s.Revoke(token))
	_, err := s.Get(token)
	errutil.AssertErrorCode(t, err, "DENY_SESSION_INVALID")
}

// TestSessionStoreConcurrentIssueAndGet — race-detector clean.
func TestSessionStoreConcurrentIssueAndGet(t *testing.T) {
	fc := &fakeClock{t: time.Unix(1700000000, 0)}
	s := adminauth.NewSessionStore(fc, 10*time.Minute)
	var tokens []string
	for i := 0; i < 100; i++ {
		tk, _, err := s.Issue(adminauth.OperatorIdentity{PlayerID: "01HZA"})
		require.NoError(t, err)
		tokens = append(tokens, tk)
	}
	done := make(chan struct{})
	go func() {
		for _, tk := range tokens {
			_, _ = s.Get(tk)
		}
		close(done)
	}()
	for i := 0; i < 100; i++ {
		_, _, _ = s.Issue(adminauth.OperatorIdentity{PlayerID: "01HZB"})
	}
	<-done
}
