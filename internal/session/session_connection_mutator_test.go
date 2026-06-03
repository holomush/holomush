// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import (
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionConnectionMutator_OnlyConstructibleViaConstructor pins
// INV-SCENE-20: the sentinel pattern blocks direct struct construction
// from outside this package. The constructor is the sole legitimate
// path. This is a runtime check; a parallel compile-fail doc test
// in internal/grpc/focus enforces the actual at-rest invariant.
func TestSessionConnectionMutator_OnlyConstructibleViaConstructor(t *testing.T) {
	t.Parallel()
	fn := func(info Info, conn Connection) (Info, Connection, error) {
		return info, conn, nil
	}
	m := NewSessionConnectionMutator(fn)
	require.NotNil(t, m.Mutate, "constructor MUST populate the callback")
}

// TestSessionConnectionMutator_NewPanicsOnNilFn pins the construction-time
// nil guard added in PR #4191 round 5 — a nil Mutate function cannot do
// useful work and would panic later inside Store.UpdateSessionConnection,
// so fail loudly at the constructor boundary.
func TestSessionConnectionMutator_NewPanicsOnNilFn(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewSessionConnectionMutator(nil) MUST panic; no panic observed")
		}
	}()
	NewSessionConnectionMutator(nil)
}

// TestSessionConnectionMutator_NilSafeRejectsZeroValue pins the call-site
// nil guard: a zero-value SessionConnectionMutator{} (e.g., from a keyed
// literal that omits Mutate) MUST surface ErrNilMutator rather than panic
// when passed to a Store.UpdateSessionConnection. CodeRabbit PR #4191.
func TestSessionConnectionMutator_NilSafeRejectsZeroValue(t *testing.T) {
	t.Parallel()
	var zero SessionConnectionMutator // sentinel zero; Mutate is nil
	err := zero.NilSafe()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNilMutator)
}

// TestSessionConnectionMutator_NilSafeAcceptsConstructed verifies the
// happy path: a properly-constructed mutator passes the nil-safe gate.
func TestSessionConnectionMutator_NilSafeAcceptsConstructed(t *testing.T) {
	t.Parallel()
	m := NewSessionConnectionMutator(func(info Info, conn Connection) (Info, Connection, error) {
		return info, conn, nil
	})
	require.NoError(t, m.NilSafe())
}

func TestSessionConnectionMutator_AppliesBothFields(t *testing.T) {
	t.Parallel()
	sceneID := ulid.Make()
	target := &FocusKey{Kind: FocusKindScene, TargetID: sceneID}

	m := NewSessionConnectionMutator(func(info Info, conn Connection) (Info, Connection, error) {
		conn.FocusKey = target
		info.PresentingFocus = target
		return info, conn, nil
	})

	info := Info{}
	conn := Connection{}
	nextInfo, nextConn, err := m.Mutate(info, conn)
	require.NoError(t, err)
	assert.Equal(t, target, nextInfo.PresentingFocus)
	assert.Equal(t, target, nextConn.FocusKey)
}

func TestSessionConnectionMutator_ErrorPropagates(t *testing.T) {
	t.Parallel()
	sentinelErr := errors.New("FOCUS_WITHOUT_MEMBERSHIP")
	m := NewSessionConnectionMutator(func(info Info, conn Connection) (Info, Connection, error) {
		return info, conn, sentinelErr
	})
	_, _, err := m.Mutate(Info{}, Connection{})
	require.ErrorIs(t, err, sentinelErr)
}
