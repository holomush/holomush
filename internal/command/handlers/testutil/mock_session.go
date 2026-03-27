// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package testutil

import (
	"context"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/session"
)

// MockSessionAccess implements session.Access for handler tests.
type MockSessionAccess struct {
	mu       sync.Mutex
	sessions []*session.Info
}

// NewMockSessionAccess creates a MockSessionAccess with the given sessions.
func NewMockSessionAccess(sessions ...*session.Info) *MockSessionAccess {
	return &MockSessionAccess{sessions: sessions}
}

// ListActive returns all sessions with status=active.
func (m *MockSessionAccess) ListActive(_ context.Context) ([]*session.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*session.Info
	for _, s := range m.sessions {
		if s.Status == session.StatusActive {
			result = append(result, s)
		}
	}
	return result, nil
}

// FindByCharacter returns the session for a character, or nil.
func (m *MockSessionAccess) FindByCharacter(_ context.Context, charID ulid.ULID) (*session.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.CharacterID == charID {
			return s, nil
		}
	}
	return nil, nil
}

// DeleteByCharacter removes and returns the session for a character.
func (m *MockSessionAccess) DeleteByCharacter(_ context.Context, charID ulid.ULID, _ string) (*session.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, s := range m.sessions {
		if s.CharacterID == charID {
			m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
			return s, nil
		}
	}
	return nil, nil
}

// UpdateActivity is a no-op for the mock.
func (m *MockSessionAccess) UpdateActivity(_ context.Context, _ string) error {
	return nil
}

// AddSession adds a session to the mock (helper for test setup).
func (m *MockSessionAccess) AddSession(charID ulid.ULID, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = append(m.sessions, &session.Info{
		ID:            ulid.Make().String(),
		CharacterID:   charID,
		CharacterName: name,
		Status:        session.StatusActive,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	})
}

// Snapshot returns a copy of all sessions (thread-safe read accessor).
func (m *MockSessionAccess) Snapshot() []*session.Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*session.Info, len(m.sessions))
	copy(result, m.sessions)
	return result
}
