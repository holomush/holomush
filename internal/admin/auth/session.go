// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package adminauth

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// SessionStore is the in-memory map of issued operator session tokens.
// Per spec §4 / INV-CRYPTO-69 / INV-CRYPTO-70:
//   - Tokens are ULIDs.
//   - TTL is per-construction (production: 10 min).
//   - The map is per-process; restart loses all sessions by design.
//   - Get is cleanup-on-access: expired tokens are deleted in-place.
type SessionStore interface {
	Issue(identity OperatorIdentity) (token string, expiresAt time.Time, err error)
	Get(token string) (OperatorIdentity, error)
	Revoke(token string) error
}

type sessionEntry struct {
	Identity  OperatorIdentity
	ExpiresAt time.Time
}

type memSessionStore struct {
	clock Clock
	ttl   time.Duration
	mu    sync.RWMutex
	m     map[string]sessionEntry
}

// NewSessionStore constructs an in-memory SessionStore with the given TTL.
func NewSessionStore(clock Clock, ttl time.Duration) SessionStore {
	return &memSessionStore{clock: clock, ttl: ttl, m: make(map[string]sessionEntry)}
}

func (s *memSessionStore) Issue(id OperatorIdentity) (string, time.Time, error) {
	now := s.clock.Now()
	entropy := ulid.Monotonic(rand.Reader, 0)
	tokenULID, err := ulid.New(ulid.Timestamp(now), entropy)
	if err != nil {
		return "", time.Time{}, oops.Code("SESSION_TOKEN_MINT_FAILED").Wrap(err)
	}
	token := tokenULID.String()
	expiresAt := now.Add(s.ttl)

	s.mu.Lock()
	s.m[token] = sessionEntry{Identity: id, ExpiresAt: expiresAt}
	s.mu.Unlock()
	return token, expiresAt, nil
}

func (s *memSessionStore) Get(token string) (OperatorIdentity, error) {
	s.mu.RLock()
	entry, ok := s.m[token]
	s.mu.RUnlock()
	if !ok {
		return OperatorIdentity{}, oops.Code("DENY_SESSION_INVALID").Errorf("session token not found")
	}
	if !s.clock.Now().Before(entry.ExpiresAt) {
		s.mu.Lock()
		delete(s.m, token)
		s.mu.Unlock()
		return OperatorIdentity{}, oops.Code("DENY_SESSION_EXPIRED").Errorf("session token expired")
	}
	return entry.Identity, nil
}

func (s *memSessionStore) Revoke(token string) error {
	s.mu.Lock()
	delete(s.m, token)
	s.mu.Unlock()
	return nil
}
