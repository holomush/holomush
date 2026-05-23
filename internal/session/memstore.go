// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// MemStore is an in-memory implementation of Store for testing.
type MemStore struct {
	mu          sync.RWMutex
	sessions    map[string]*Info
	connections map[ulid.ULID]*Connection // keyed by connection ID
}

// NewMemStore creates a new in-memory session store.
func NewMemStore() *MemStore {
	return &MemStore{
		sessions:    make(map[string]*Info),
		connections: make(map[ulid.ULID]*Connection),
	}
}

// compile-time check that MemStore implements Store.
var _ Store = (*MemStore)(nil)

// Get retrieves a session by ID.
func (m *MemStore) Get(_ context.Context, id string) (*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, ok := m.sessions[id]
	if !ok {
		return nil, oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	return copyInfo(info), nil
}

// Set creates or updates a session.
func (m *MemStore) Set(_ context.Context, id string, info *Info) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessions[id] = copyInfo(info)
	return nil
}

// Delete removes a session and its associated connections.
func (m *MemStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, id)
	for connID, conn := range m.connections {
		if conn.SessionID == id {
			delete(m.connections, connID)
		}
	}
	return nil
}

// FindByCharacter returns the active or detached session for a character.
func (m *MemStore) FindByCharacter(_ context.Context, characterID ulid.ULID) (*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, info := range m.sessions {
		if info.CharacterID == characterID &&
			(info.Status == StatusActive || info.Status == StatusDetached) {
			return copyInfo(info), nil
		}
	}
	return nil, oops.Code("SESSION_NOT_FOUND").
		With("character_id", characterID.String()).
		Errorf("no active or detached session for character")
}

// ListByPlayer returns all non-expired sessions. MemStore does not track
// player-to-character relationships, so this returns all non-expired sessions.
// TODO: filter by playerID when player-character relationship table exists.
func (m *MemStore) ListByPlayer(_ context.Context, _ ulid.ULID) ([]*Info, error) {
	// MemStore does not track player-to-character relationships.
	// This is a stub that returns all non-expired sessions.
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Info
	for _, info := range m.sessions {
		if info.Status != StatusExpired {
			result = append(result, copyInfo(info))
		}
	}
	return result, nil
}

// ListByPlayerSession returns all active/detached sessions whose PlayerSessionID
// matches any of the given IDs.
func (m *MemStore) ListByPlayerSession(_ context.Context, playerSessionIDs []ulid.ULID) ([]*Info, error) {
	if len(playerSessionIDs) == 0 {
		return nil, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	want := make(map[ulid.ULID]struct{}, len(playerSessionIDs))
	for _, id := range playerSessionIDs {
		want[id] = struct{}{}
	}

	var result []*Info
	for _, info := range m.sessions {
		if info.Status == StatusExpired {
			continue
		}
		if _, ok := want[info.PlayerSessionID]; ok {
			result = append(result, copyInfo(info))
		}
	}
	return result, nil
}

// ListExpired returns all detached sessions past their expiry time.
func (m *MemStore) ListExpired(_ context.Context) ([]*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	var result []*Info
	for _, info := range m.sessions {
		if info.Status == StatusDetached && info.ExpiresAt != nil && now.After(*info.ExpiresAt) {
			result = append(result, copyInfo(info))
		}
	}
	return result, nil
}

// UpdateStatus transitions a session's status.
func (m *MemStore) UpdateStatus(_ context.Context, id string, status Status,
	detachedAt *time.Time, expiresAt *time.Time,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	info.Status = status
	info.DetachedAt = detachedAt
	info.ExpiresAt = expiresAt
	info.UpdatedAt = time.Now()
	return nil
}

// ReattachCAS atomically transitions a detached session to active.
// Returns true if the transition succeeded, false if the session was not detached.
func (m *MemStore) ReattachCAS(_ context.Context, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return false, oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	if info.Status != StatusDetached {
		return false, nil
	}
	info.Status = StatusActive
	info.DetachedAt = nil
	info.ExpiresAt = nil
	info.UpdatedAt = time.Now()
	return true, nil
}

// AppendCommand adds a command to the session's history, enforcing the cap.
func (m *MemStore) AppendCommand(_ context.Context, id, command string, maxHistory int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	info.CommandHistory = append(info.CommandHistory, command)
	if len(info.CommandHistory) > maxHistory {
		info.CommandHistory = info.CommandHistory[len(info.CommandHistory)-maxHistory:]
	}
	return nil
}

// GetCommandHistory returns the session's command history.
func (m *MemStore) GetCommandHistory(_ context.Context, id string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, ok := m.sessions[id]
	if !ok {
		return nil, oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	result := make([]string, len(info.CommandHistory))
	copy(result, info.CommandHistory)
	return result, nil
}

// validClientTypes is the set of allowed client_type values for MemStore.
var validClientTypes = map[string]bool{
	"terminal":  true,
	"comms_hub": true,
	"telnet":    true,
}

// AddConnection registers a new connection to a session.
func (m *MemStore) AddConnection(_ context.Context, conn *Connection) error {
	if !validClientTypes[conn.ClientType] {
		return oops.With("operation", "add connection").
			With("client_type", conn.ClientType).
			Errorf("invalid client_type %q: must be one of terminal, comms_hub, telnet", conn.ClientType)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Deep-copy on ingress: caller-owned Streams slice + FocusKey pointer
	// must not alias the store, or external mutation outside m.mu can
	// corrupt internal state. Mirrors the egress copy via GetConnection /
	// ListConnectionsBySession. (CodeRabbit PR #4191)
	m.connections[conn.ID] = copyConnection(conn)
	return nil
}

// RemoveConnection removes a connection from a session.
func (m *MemStore) RemoveConnection(_ context.Context, connectionID ulid.ULID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.connections, connectionID)
	return nil
}

// CountConnections returns the number of active connections for a session.
func (m *MemStore) CountConnections(_ context.Context, sessionID string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, conn := range m.connections {
		if conn.SessionID == sessionID {
			count++
		}
	}
	return count, nil
}

// CountConnectionsByType returns the number of active connections of a specific client type for a session.
func (m *MemStore) CountConnectionsByType(_ context.Context, sessionID, clientType string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, conn := range m.connections {
		if conn.SessionID == sessionID && conn.ClientType == clientType {
			count++
		}
	}
	return count, nil
}

// ListConnectionsBySession returns a snapshot of all active Connections
// for a session. Used by AutoFocusOnJoin's fan-out enumeration.
func (m *MemStore) ListConnectionsBySession(_ context.Context, sessionID string) ([]*Connection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.sessions[sessionID]; !ok {
		return nil, oops.Code("SESSION_NOT_FOUND").
			With("session_id", sessionID).
			Errorf("session not found")
	}

	out := make([]*Connection, 0)
	for _, conn := range m.connections {
		if conn.SessionID == sessionID {
			// Deep-copy: shallow struct copy leaves Streams + FocusKey
			// aliasing the store's underlying memory.
			out = append(out, copyConnection(conn))
		}
	}
	return out, nil
}

// UpdateGridPresent sets the grid_present flag on a session.
func (m *MemStore) UpdateGridPresent(_ context.Context, id string, present bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	info.GridPresent = present
	info.UpdatedAt = time.Now()
	return nil
}

// ListActiveByLocation returns active sessions whose LocationID matches.
func (m *MemStore) ListActiveByLocation(_ context.Context, locationID ulid.ULID) ([]*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Info
	for _, info := range m.sessions {
		if info.Status == StatusActive && info.LocationID == locationID {
			result = append(result, copyInfo(info))
		}
	}
	return result, nil
}

// ListByFocus returns all non-expired sessions whose FocusMemberships
// include the given target.
func (m *MemStore) ListByFocus(_ context.Context, target FocusKey) ([]*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Info
	for _, info := range m.sessions {
		if info.Status == StatusExpired {
			continue
		}
		for _, mem := range info.FocusMemberships {
			if mem.Kind == target.Kind && mem.TargetID == target.TargetID {
				result = append(result, copyInfo(info))
				break
			}
		}
	}
	return result, nil
}

// ListActive returns all sessions with status=active.
func (m *MemStore) ListActive(_ context.Context) ([]*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Info
	for _, info := range m.sessions {
		if info.Status == StatusActive {
			result = append(result, copyInfo(info))
		}
	}
	return result, nil
}

// DeleteByCharacter finds and deletes a character's session.
// Returns the deleted Info, or nil if no session exists.
func (m *MemStore) DeleteByCharacter(ctx context.Context, characterID ulid.ULID) (*Info, error) {
	// FindByCharacter uses RLock, so call it before taking the write lock.
	info, err := m.FindByCharacter(ctx, characterID)
	if err != nil {
		if oopsErr, ok := oops.AsOops(err); ok && oopsErr.Code() == "SESSION_NOT_FOUND" {
			return nil, nil
		}
		return nil, err
	}
	if info == nil {
		return nil, nil
	}

	// Delete uses its own Lock internally.
	if err := m.Delete(ctx, info.ID); err != nil {
		return nil, err
	}
	return info, nil
}

// UpdateActivity bumps the updated_at timestamp for a session.
func (m *MemStore) UpdateActivity(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	info.UpdatedAt = time.Now()
	return nil
}

// FindByCharacterName returns the active session for a character by name.
// The lookup is case-insensitive.
func (m *MemStore) FindByCharacterName(_ context.Context, name string) (*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, info := range m.sessions {
		if info.Status == StatusActive && strings.EqualFold(info.CharacterName, name) {
			return copyInfo(info), nil
		}
	}
	return nil, oops.Code("SESSION_NOT_FOUND").
		With("character_name", name).
		Errorf("no active session for character name")
}

// UpdateLastPaged records the name of the character most recently paged.
func (m *MemStore) UpdateLastPaged(_ context.Context, id, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	info.LastPaged = name
	info.UpdatedAt = time.Now()
	return nil
}

// UpdateFocusMemberships atomically applies the mutator callback to the
// session's focus memberships and presenting focus. The mutator receives
// copies of the current state and returns the desired state. On mutator
// error, the session is left unchanged.
func (m *MemStore) UpdateFocusMemberships(_ context.Context, sessionID string, mut FocusMutator) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[sessionID]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", sessionID).
			Errorf("session not found")
	}
	if info.Status == StatusExpired {
		return oops.Code("SESSION_EXPIRED").
			With("session_id", sessionID).
			Errorf("cannot mutate focus on expired session")
	}

	// Snapshot current state for the mutator (defensive copies).
	currentMemberships := make([]FocusMembership, len(info.FocusMemberships))
	copy(currentMemberships, info.FocusMemberships)

	var currentPresenting *FocusKey
	if info.PresentingFocus != nil {
		cp := *info.PresentingFocus
		currentPresenting = &cp
	}

	nextMemberships, nextPresenting, err := mut.Mutate(currentMemberships, currentPresenting)
	if err != nil {
		return oops.Code("FOCUS_MUTATOR_ERROR").
			With("session_id", sessionID).
			Wrap(err)
	}

	info.FocusMemberships = nextMemberships
	info.PresentingFocus = nextPresenting
	info.UpdatedAt = time.Now()
	return nil
}

// GetConnection looks up a single Connection by ID. O(1) via the
// store's existing connections map.
func (m *MemStore) GetConnection(_ context.Context, connectionID ulid.ULID) (*Connection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	conn, ok := m.connections[connectionID]
	if !ok {
		return nil, oops.Code("CONNECTION_NOT_FOUND").
			With("connection_id", connectionID.String()).
			Errorf("connection not found")
	}
	return copyConnection(conn), nil
}

// copyConnection returns a deep copy of a Connection: new Streams slice and
// a freshly-allocated FocusKey pointer. Without this, callers holding the
// returned pointer could mutate the store's underlying state outside m.mu.
func copyConnection(c *Connection) *Connection {
	out := *c
	if c.Streams != nil {
		out.Streams = make([]string, len(c.Streams))
		copy(out.Streams, c.Streams)
	}
	if c.FocusKey != nil {
		fk := *c.FocusKey
		out.FocusKey = &fk
	}
	return &out
}

// (copyInfo is defined below at line ~680 and pre-existing; reused here for
// defensive deep-copy on mutator entry per CodeRabbit PR #4191.)

// UpdateSessionConnection runs the mutator under the store-wide m.mu
// lock. Both Info and Connection writes commit atomically. INV-P5-7:
// external observers cannot see one field updated while the other lags.
func (m *MemStore) UpdateSessionConnection(
	_ context.Context,
	sessionID string,
	connectionID ulid.ULID,
	mut SessionConnectionMutator,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[sessionID]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", sessionID).
			Errorf("session not found")
	}

	conn, ok := m.connections[connectionID]
	if !ok || conn.SessionID != sessionID {
		return oops.Code("CONNECTION_NOT_FOUND").
			With("session_id", sessionID).
			With("connection_id", connectionID.String()).
			Errorf("connection not found in session")
	}

	// Nil-Mutate guard before invocation: a zero-value
	// SessionConnectionMutator{} or a keyed-literal bypass with no
	// Mutate field would otherwise panic here. (CodeRabbit PR #4191)
	if nerr := mut.NilSafe(); nerr != nil {
		return nerr
	}

	// Deep-copy before handing to the mutator: even though the mutator
	// receives value copies of the structs, slice and pointer fields
	// (CommandHistory, FocusMemberships, PresentingFocus, Streams,
	// FocusKey) still alias the store's underlying memory. A buggy
	// mutator could mutate those in place. Defensive copy on entry
	// matches the defensive copy on exit at line 545 below.
	infoIn := copyInfo(info)
	connIn := copyConnection(conn)
	nextInfo, nextConn, err := mut.Mutate(*infoIn, *connIn)
	if err != nil {
		return err
	}

	// Narrow assignment for parity with the Postgres impl (T6) which
	// only UPDATEs focus_key + presenting_focus. Mutator changes to any
	// other Info/Connection field are silently dropped on both backends.
	// CONTRACT: Phase 5's mutator only writes these two fields.
	//
	// Defensive-copy the pointers: the mutator received value copies but
	// may have written pointers to its own allocations. We MUST NOT let
	// the caller retain mutability of store state via the returned
	// pointer. (Postgres impl is inherently isolated by the txn boundary.)
	if nextInfo.PresentingFocus != nil {
		fk := *nextInfo.PresentingFocus
		info.PresentingFocus = &fk
	} else {
		info.PresentingFocus = nil
	}
	if nextConn.FocusKey != nil {
		fk := *nextConn.FocusKey
		conn.FocusKey = &fk
	} else {
		conn.FocusKey = nil
	}
	return nil
}

// UpdateLastWhispered records the name of the character most recently whispered to.
func (m *MemStore) UpdateLastWhispered(_ context.Context, id, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	info.LastWhispered = name
	info.UpdatedAt = time.Now()
	return nil
}

// UpdateLocationOnMove atomically updates LocationID and LocationArrivedAt for
// all Active sessions belonging to characterID. Detached and Expired sessions
// are not modified.
//
// arrivedAt MUST be non-zero — a zero time.Time would collapse the I-PRIV-1
// per-session location floor to year-1 and silently disable history privacy.
func (m *MemStore) UpdateLocationOnMove(_ context.Context, characterID, newLocationID ulid.ULID, arrivedAt time.Time) error {
	if arrivedAt.IsZero() {
		return oops.Code("INVALID_ARGUMENT").
			With("operation", "update_location_on_move").
			With("character_id", characterID.String()).
			Errorf("arrivedAt must be non-zero")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, info := range m.sessions {
		if info.CharacterID != characterID {
			continue
		}
		if info.Status != StatusActive {
			continue
		}
		info.LocationID = newLocationID
		info.LocationArrivedAt = arrivedAt
		info.UpdatedAt = arrivedAt
	}
	return nil
}

// BumpLocationArrivedAt updates LocationArrivedAt for a single session
// regardless of status.
//
// arrivedAt MUST be non-zero — a zero time.Time would collapse the I-PRIV-1
// per-session location floor to year-1 and silently disable history privacy.
//
// Errors:
//
//	INVALID_ARGUMENT — arrivedAt is the zero value.
//	SESSION_NOT_FOUND — sessionID does not match any session.
func (m *MemStore) BumpLocationArrivedAt(_ context.Context, sessionID string, arrivedAt time.Time) error {
	if arrivedAt.IsZero() {
		return oops.Code("INVALID_ARGUMENT").
			With("operation", "bump_location_arrived_at").
			With("session_id", sessionID).
			Errorf("arrivedAt must be non-zero")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.sessions[sessionID]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").With("session_id", sessionID).Errorf("session not found")
	}
	info.LocationArrivedAt = arrivedAt
	info.UpdatedAt = arrivedAt
	return nil
}

// copyInfo returns a defensive copy of an Info to prevent external modification.
func copyInfo(info *Info) *Info {
	history := make([]string, len(info.CommandHistory))
	copy(history, info.CommandHistory)

	memberships := make([]FocusMembership, len(info.FocusMemberships))
	copy(memberships, info.FocusMemberships)

	cp := *info
	cp.CommandHistory = history
	cp.FocusMemberships = memberships

	if info.PresentingFocus != nil {
		pf := *info.PresentingFocus
		cp.PresentingFocus = &pf
	}

	return &cp
}
