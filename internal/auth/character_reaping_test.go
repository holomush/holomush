// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/wmodel"
	"github.com/holomush/holomush/pkg/errutil"
)

// --- reaping fakes ---

type fakeReapLister struct {
	chars   []*world.Character
	listErr error
	calls   int
}

func (f *fakeReapLister) ListByPlayer(_ context.Context, _ ulid.ULID) ([]*world.Character, error) {
	f.calls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.chars, nil
}

type fakeReapDeleter struct {
	seq          *[]string
	deleted      []ulid.ULID
	gotVersions  []int
	errForID     map[string]error
	deleteCalled int
}

func (f *fakeReapDeleter) Delete(_ context.Context, id ulid.ULID, expectedVersion int) (*wmodel.MutationDelta, error) {
	*f.seq = append(*f.seq, "delete:"+id.String())
	f.deleteCalled++
	f.gotVersions = append(f.gotVersions, expectedVersion)
	if e, ok := f.errForID[id.String()]; ok && e != nil {
		return nil, e
	}
	f.deleted = append(f.deleted, id)
	return &wmodel.MutationDelta{
		Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateCharacter, ID: id, Tombstone: true},
	}, nil
}

type fakeReapProps struct {
	seq        *[]string
	byParent   []string
	deleteErr  error
	deleteCall int
}

func (f *fakeReapProps) DeleteByParent(_ context.Context, parentType string, parentID ulid.ULID) error {
	*f.seq = append(*f.seq, "props:"+parentID.String())
	f.deleteCall++
	f.byParent = append(f.byParent, parentType+":"+parentID.String())
	return f.deleteErr
}

type fakeReapBindings struct {
	seq       *[]string
	byChar    []string
	deleteErr error
}

func (f *fakeReapBindings) DeleteByCharacter(_ context.Context, characterID string) error {
	*f.seq = append(*f.seq, "bind:"+characterID)
	f.byChar = append(f.byChar, characterID)
	return f.deleteErr
}

type fakeReapMarker struct {
	seq     *[]string
	markErr error
	calls   int
	lastID  ulid.ULID
}

func (f *fakeReapMarker) MarkReaping(_ context.Context, playerID ulid.ULID) error {
	if f.seq != nil {
		*f.seq = append(*f.seq, "mark")
	}
	f.calls++
	f.lastID = playerID
	return f.markErr
}

type fakeReapPlayerDeleter struct {
	seq       *[]string
	deleteErr error
	calls     int
	lastID    ulid.ULID
}

func (f *fakeReapPlayerDeleter) DeleteGuestPlayer(_ context.Context, playerID ulid.ULID) error {
	if f.seq != nil {
		*f.seq = append(*f.seq, "player")
	}
	f.calls++
	f.lastID = playerID
	return f.deleteErr
}

func reapChar(t *testing.T, version int) *world.Character {
	t.Helper()
	char, err := world.NewCharacter(ulid.Make(), "Reap Target")
	require.NoError(t, err)
	char.Version = version
	return char
}

// --- constructor fail-closed ---

func TestNewCharacterReapingServiceFailsClosedOnNilDeps(t *testing.T) {
	seq := []string{}
	l := &fakeReapLister{}
	d := &fakeReapDeleter{seq: &seq, errForID: map[string]error{}}
	p := &fakeReapProps{seq: &seq}
	b := &fakeReapBindings{seq: &seq}
	tx := fakeGenesisTransactor{}
	o := &fakeOutboxWriter{seq: &seq}
	pd := &fakeReapPlayerDeleter{seq: &seq}
	mk := &fakeReapMarker{seq: &seq}

	tests := []struct {
		name     string
		lister   auth.ReapingCharacterLister
		deleter  auth.ReapingCharacterDeleter
		props    auth.ReapingPropertyDeleter
		bindings auth.ReapingBindingDeleter
		tx       auth.GenesisTransactor
		outbox   world.OutboxWriter
		players  auth.GuestPlayerDeleter
		marker   auth.PlayerReapMarker
		wantErr  string
	}{
		{"nil lister", nil, d, p, b, tx, o, pd, mk, "character lister is required"},
		{"nil deleter", l, nil, p, b, tx, o, pd, mk, "character deleter is required"},
		{"nil props", l, d, nil, b, tx, o, pd, mk, "property deleter is required"},
		{"nil bindings", l, d, p, nil, tx, o, pd, mk, "binding deleter is required"},
		{"nil transactor", l, d, p, b, nil, o, pd, mk, "transactor is required"},
		{"nil outbox", l, d, p, b, tx, nil, pd, mk, "outbox writer is required"},
		{"nil players", l, d, p, b, tx, o, nil, mk, "player deleter is required"},
		{"nil marker", l, d, p, b, tx, o, pd, nil, "reaping marker is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := auth.NewCharacterReapingService(tt.lister, tt.deleter, tt.props, tt.bindings, tt.tx, tt.outbox, tt.players, tt.marker)
			require.Error(t, err)
			assert.Nil(t, svc)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// --- happy path: mark → per-char {props → delete → tombstone} → player delete ---

func TestCharacterReapingDeleteGuestPlayerTombstonesEachCharacterThenDeletesPlayer(t *testing.T) {
	seq := []string{}
	c1 := reapChar(t, 3)
	c2 := reapChar(t, 7)
	lister := &fakeReapLister{chars: []*world.Character{c1, c2}}
	deleter := &fakeReapDeleter{seq: &seq, errForID: map[string]error{}}
	props := &fakeReapProps{seq: &seq}
	bindings := &fakeReapBindings{seq: &seq}
	outboxW := &fakeOutboxWriter{seq: &seq}
	players := &fakeReapPlayerDeleter{seq: &seq}
	marker := &fakeReapMarker{seq: &seq}

	svc, err := auth.NewCharacterReapingService(lister, deleter, props, bindings, fakeGenesisTransactor{}, outboxW, players, marker)
	require.NoError(t, err)

	playerID := ulid.Make()
	require.NoError(t, svc.DeleteGuestPlayer(context.Background(), playerID))

	// mark FIRST, then per-character (bind → props → delete → outbox) twice, then player.
	assert.Equal(t, []string{
		"mark",
		"bind:" + c1.ID.String(), "props:" + c1.ID.String(), "delete:" + c1.ID.String(), "outbox",
		"bind:" + c2.ID.String(), "props:" + c2.ID.String(), "delete:" + c2.ID.String(), "outbox",
		"player",
	}, seq)

	assert.Equal(t, 1, marker.calls)
	assert.Equal(t, playerID, marker.lastID)
	assert.Equal(t, 1, lister.calls)
	assert.Equal(t, 2, outboxW.calls)
	assert.Equal(t, 1, players.calls)
	assert.Equal(t, playerID, players.lastID)

	// Version-bearing list (R6-1): each CAS Delete got the stored version.
	assert.Equal(t, []int{3, 7}, deleter.gotVersions)

	// Property cascade parity (R6-3): DeleteByParent("character", id) per char.
	assert.Equal(t, []string{"character:" + c1.ID.String(), "character:" + c2.ID.String()}, props.byParent)

	// Envelope is the character_deleted tombstone kind with a fresh event ULID.
	assert.Equal(t, "character_deleted", outboxW.lastIntent.Kind)
	assert.False(t, outboxW.lastIntent.EventID.IsZero())
	assert.Equal(t, wmodel.AggregateCharacter, outboxW.lastIntent.AggregateType)
	assert.Equal(t, c2.ID, outboxW.lastIntent.AggregateID)
	assert.Equal(t, "system", outboxW.lastIntent.Actor)
}

// A player with zero characters is marked + deleted cleanly (no tombstones).
func TestCharacterReapingDeleteGuestPlayerWithNoCharacters(t *testing.T) {
	seq := []string{}
	lister := &fakeReapLister{chars: nil}
	deleter := &fakeReapDeleter{seq: &seq, errForID: map[string]error{}}
	props := &fakeReapProps{seq: &seq}
	bindings := &fakeReapBindings{seq: &seq}
	outboxW := &fakeOutboxWriter{seq: &seq}
	players := &fakeReapPlayerDeleter{seq: &seq}
	marker := &fakeReapMarker{seq: &seq}

	svc, err := auth.NewCharacterReapingService(lister, deleter, props, bindings, fakeGenesisTransactor{}, outboxW, players, marker)
	require.NoError(t, err)

	require.NoError(t, svc.DeleteGuestPlayer(context.Background(), ulid.Make()))
	assert.Equal(t, []string{"mark", "player"}, seq)
	assert.Equal(t, 0, deleter.deleteCalled)
	assert.Equal(t, 0, outboxW.calls)
}

// --- resumability: a per-character conflict leaves earlier tombstones committed
// and the player un-deleted (round-6 Codex MEDIUM) ---

func TestCharacterReapingPerCharacterConflictLeavesEarlierTombstonesAndSkipsPlayerDelete(t *testing.T) {
	seq := []string{}
	c1 := reapChar(t, 1)
	c2 := reapChar(t, 1)
	conflict := oops.Code(world.CodeConcurrentEdit).Wrap(world.ErrConcurrentEdit)
	lister := &fakeReapLister{chars: []*world.Character{c1, c2}}
	deleter := &fakeReapDeleter{seq: &seq, errForID: map[string]error{c2.ID.String(): conflict}}
	props := &fakeReapProps{seq: &seq}
	bindings := &fakeReapBindings{seq: &seq}
	outboxW := &fakeOutboxWriter{seq: &seq}
	players := &fakeReapPlayerDeleter{seq: &seq}
	marker := &fakeReapMarker{seq: &seq}

	svc, err := auth.NewCharacterReapingService(lister, deleter, props, bindings, fakeGenesisTransactor{}, outboxW, players, marker)
	require.NoError(t, err)

	err = svc.DeleteGuestPlayer(context.Background(), ulid.Make())
	require.Error(t, err)

	// The conflict is retriable — its WORLD_CONCURRENT_EDIT signal survives.
	assert.True(t, errors.Is(err, world.ErrConcurrentEdit))
	errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)

	// c1 was fully tombstoned (its envelope committed); c2 conflicted.
	assert.Equal(t, 1, outboxW.calls)
	assert.Equal(t, []ulid.ULID{c1.ID}, deleter.deleted)

	// The player is NOT deleted while a character remains un-tombstoned; it stays
	// marked reaping for the next reap cycle.
	assert.Equal(t, 1, marker.calls)
	assert.Equal(t, 0, players.calls)
}

// A property-cascade failure aborts the reap (no tombstone, no player delete).
func TestCharacterReapingPropertyCascadeFailureAbortsReap(t *testing.T) {
	seq := []string{}
	c1 := reapChar(t, 1)
	lister := &fakeReapLister{chars: []*world.Character{c1}}
	deleter := &fakeReapDeleter{seq: &seq, errForID: map[string]error{}}
	props := &fakeReapProps{seq: &seq, deleteErr: errors.New("props boom")}
	bindings := &fakeReapBindings{seq: &seq}
	outboxW := &fakeOutboxWriter{seq: &seq}
	players := &fakeReapPlayerDeleter{seq: &seq}
	marker := &fakeReapMarker{seq: &seq}

	svc, err := auth.NewCharacterReapingService(lister, deleter, props, bindings, fakeGenesisTransactor{}, outboxW, players, marker)
	require.NoError(t, err)

	err = svc.DeleteGuestPlayer(context.Background(), ulid.Make())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "GUEST_REAP_FAILED")
	assert.Equal(t, 0, deleter.deleteCalled) // never reached the delete
	assert.Equal(t, 0, outboxW.calls)
	assert.Equal(t, 0, players.calls)
}

// A MarkReaping failure aborts before enumeration (fail-closed).
func TestCharacterReapingMarkFailureAbortsBeforeEnumeration(t *testing.T) {
	seq := []string{}
	lister := &fakeReapLister{chars: []*world.Character{reapChar(t, 1)}}
	deleter := &fakeReapDeleter{seq: &seq, errForID: map[string]error{}}
	props := &fakeReapProps{seq: &seq}
	bindings := &fakeReapBindings{seq: &seq}
	outboxW := &fakeOutboxWriter{seq: &seq}
	players := &fakeReapPlayerDeleter{seq: &seq}
	marker := &fakeReapMarker{seq: &seq, markErr: errors.New("mark boom")}

	svc, err := auth.NewCharacterReapingService(lister, deleter, props, bindings, fakeGenesisTransactor{}, outboxW, players, marker)
	require.NoError(t, err)

	err = svc.DeleteGuestPlayer(context.Background(), ulid.Make())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "GUEST_REAP_FAILED")
	assert.Equal(t, 0, lister.calls) // enumeration never happened
	assert.Equal(t, 0, players.calls)
}

// Compile-time check: the reaping service satisfies auth.GuestCleaner.
var _ auth.GuestCleaner = (*auth.CharacterReapingService)(nil)
