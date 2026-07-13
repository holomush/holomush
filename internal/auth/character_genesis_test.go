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

// --- fakes ---

type fakeCharWriter struct {
	seq       *[]string
	createErr error
	created   *world.Character
}

func (f *fakeCharWriter) Create(_ context.Context, char *world.Character) (*wmodel.MutationDelta, error) {
	*f.seq = append(*f.seq, "writer")
	f.created = char
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &wmodel.MutationDelta{
		Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateCharacter, ID: char.ID},
	}, nil
}

type fakeGenesisBindingCreator struct {
	seq            *[]string
	bindErr        error
	calls          int
	lastReason     string
	lastPlayerID   string
	lastCharacter  string
	returnBindData string
}

func (f *fakeGenesisBindingCreator) Create(_ context.Context, playerID, characterID, reason string) (string, error) {
	*f.seq = append(*f.seq, "binding")
	f.calls++
	f.lastReason = reason
	f.lastPlayerID = playerID
	f.lastCharacter = characterID
	if f.bindErr != nil {
		return "", f.bindErr
	}
	if f.returnBindData == "" {
		return "bind-id", nil
	}
	return f.returnBindData, nil
}

type fakeOutboxWriter struct {
	seq        *[]string
	writeErr   error
	calls      int
	lastIntent wmodel.EnvelopeIntent
	lastDelta  *wmodel.MutationDelta
}

func (f *fakeOutboxWriter) WriteIntent(_ context.Context, intent wmodel.EnvelopeIntent, delta *wmodel.MutationDelta) (*wmodel.Envelope, error) {
	*f.seq = append(*f.seq, "outbox")
	f.calls++
	f.lastIntent = intent
	f.lastDelta = delta
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	return &wmodel.Envelope{EventID: intent.EventID, Kind: intent.Kind}, nil
}

// fakeGenesisTransactor executes fn directly with the provided ctx, simulating a
// committed (or, on fn error, rolled-back) transaction — the unit-test seam.
type fakeGenesisTransactor struct{}

func (fakeGenesisTransactor) InTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// fakeReapingGuard is the unit-test PlayerReapingGuard seam. By default it
// permits creation (returns nil); set guardErr to simulate a reaping player.
type fakeReapingGuard struct {
	seq      *[]string
	guardErr error
	calls    int
}

func (f *fakeReapingGuard) EnsureNotReaping(_ context.Context, _ ulid.ULID) error {
	if f.seq != nil {
		*f.seq = append(*f.seq, "guard")
	}
	f.calls++
	return f.guardErr
}

func newGenesisChar(t *testing.T) *world.Character {
	t.Helper()
	char, err := world.NewCharacter(ulid.Make(), "Genesis Hero")
	require.NoError(t, err)
	loc := ulid.Make()
	char.LocationID = &loc
	return char
}

// --- constructor fail-closed ---

func TestNewCharacterGenesisServiceFailsClosedOnNilDeps(t *testing.T) {
	seq := []string{}
	validWriter := &fakeCharWriter{seq: &seq}
	validTx := fakeGenesisTransactor{}
	validBind := &fakeGenesisBindingCreator{seq: &seq}
	validOutbox := &fakeOutboxWriter{seq: &seq}
	validGuard := &fakeReapingGuard{seq: &seq}

	tests := []struct {
		name    string
		writer  auth.CharacterWriter
		tx      auth.GenesisTransactor
		bind    auth.GenesisBindingCreator
		outbox  world.OutboxWriter
		guard   auth.PlayerReapingGuard
		wantErr string
	}{
		{"nil writer", nil, validTx, validBind, validOutbox, validGuard, "character writer is required"},
		{"nil transactor", validWriter, nil, validBind, validOutbox, validGuard, "transactor is required"},
		{"nil bindings", validWriter, validTx, nil, validOutbox, validGuard, "binding creator is required"},
		{"nil outbox", validWriter, validTx, validBind, nil, validGuard, "outbox writer is required"},
		{"nil guard", validWriter, validTx, validBind, validOutbox, nil, "player reaping guard is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := auth.NewCharacterGenesisService(tt.writer, tt.tx, tt.bind, tt.outbox, tt.guard)
			require.Error(t, err)
			assert.Nil(t, svc)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// --- call order + binding semantics ---

func TestCharacterGenesisCreateWritesCharacterBindingThenEnvelopeInOrder(t *testing.T) {
	seq := []string{}
	writer := &fakeCharWriter{seq: &seq}
	bind := &fakeGenesisBindingCreator{seq: &seq}
	outboxW := &fakeOutboxWriter{seq: &seq}

	svc, err := auth.NewCharacterGenesisService(writer, fakeGenesisTransactor{}, bind, outboxW, &fakeReapingGuard{})
	require.NoError(t, err)

	char := newGenesisChar(t)
	require.NoError(t, svc.Create(context.Background(), char, "initial_bind"))

	// character write, then binding, then envelope — in that order.
	assert.Equal(t, []string{"writer", "binding", "outbox"}, seq)
	assert.Equal(t, 1, bind.calls)
	assert.Equal(t, "initial_bind", bind.lastReason)
	assert.Equal(t, char.PlayerID.String(), bind.lastPlayerID)
	assert.Equal(t, char.ID.String(), bind.lastCharacter)

	// envelope is the character-genesis kind with a fresh (non-zero) event ULID.
	assert.Equal(t, 1, outboxW.calls)
	assert.Equal(t, "character_genesis", outboxW.lastIntent.Kind)
	assert.False(t, outboxW.lastIntent.EventID.IsZero())
	assert.Equal(t, wmodel.AggregateCharacter, outboxW.lastIntent.AggregateType)
	assert.Equal(t, char.ID, outboxW.lastIntent.AggregateID)
	assert.NotNil(t, outboxW.lastDelta)
}

func TestCharacterGenesisCreateEmptyBindReasonEmitsEnvelopeWithNoBinding(t *testing.T) {
	seq := []string{}
	writer := &fakeCharWriter{seq: &seq}
	bind := &fakeGenesisBindingCreator{seq: &seq}
	outboxW := &fakeOutboxWriter{seq: &seq}

	svc, err := auth.NewCharacterGenesisService(writer, fakeGenesisTransactor{}, bind, outboxW, &fakeReapingGuard{})
	require.NoError(t, err)

	require.NoError(t, svc.Create(context.Background(), newGenesisChar(t), ""))

	// No binding created (bootstrap-admin mode) but the envelope IS still emitted.
	assert.Equal(t, []string{"writer", "outbox"}, seq)
	assert.Equal(t, 0, bind.calls)
	assert.Equal(t, 1, outboxW.calls)
}

// --- failed-step rollback (no envelope on writer/binding failure; caller sees error) ---

func TestCharacterGenesisCreateFailsWhenWriterFails(t *testing.T) {
	seq := []string{}
	writer := &fakeCharWriter{seq: &seq, createErr: errors.New("insert boom")}
	bind := &fakeGenesisBindingCreator{seq: &seq}
	outboxW := &fakeOutboxWriter{seq: &seq}

	svc, err := auth.NewCharacterGenesisService(writer, fakeGenesisTransactor{}, bind, outboxW, &fakeReapingGuard{})
	require.NoError(t, err)

	err = svc.Create(context.Background(), newGenesisChar(t), "initial_bind")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHARACTER_GENESIS_FAILED")
	// Neither binding nor envelope written when the character insert fails.
	assert.Equal(t, 0, bind.calls)
	assert.Equal(t, 0, outboxW.calls)
}

func TestCharacterGenesisCreateFailsWhenBindingFails(t *testing.T) {
	seq := []string{}
	writer := &fakeCharWriter{seq: &seq}
	bind := &fakeGenesisBindingCreator{seq: &seq, bindErr: errors.New("bind boom")}
	outboxW := &fakeOutboxWriter{seq: &seq}

	svc, err := auth.NewCharacterGenesisService(writer, fakeGenesisTransactor{}, bind, outboxW, &fakeReapingGuard{})
	require.NoError(t, err)

	err = svc.Create(context.Background(), newGenesisChar(t), "initial_bind")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHARACTER_GENESIS_BINDING_FAILED")
	// Envelope never written when the binding fails.
	assert.Equal(t, 0, outboxW.calls)
}

func TestCharacterGenesisCreateFailsWhenEnvelopeFails(t *testing.T) {
	seq := []string{}
	writer := &fakeCharWriter{seq: &seq}
	bind := &fakeGenesisBindingCreator{seq: &seq}
	outboxW := &fakeOutboxWriter{seq: &seq, writeErr: errors.New("outbox boom")}

	svc, err := auth.NewCharacterGenesisService(writer, fakeGenesisTransactor{}, bind, outboxW, &fakeReapingGuard{})
	require.NoError(t, err)

	err = svc.Create(context.Background(), newGenesisChar(t), "initial_bind")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHARACTER_GENESIS_ENVELOPE_FAILED")
}

func TestCharacterGenesisCreateRejectsNilCharacter(t *testing.T) {
	seq := []string{}
	svc, err := auth.NewCharacterGenesisService(
		&fakeCharWriter{seq: &seq}, fakeGenesisTransactor{},
		&fakeGenesisBindingCreator{seq: &seq}, &fakeOutboxWriter{seq: &seq},
		&fakeReapingGuard{seq: &seq},
	)
	require.NoError(t, err)

	err = svc.Create(context.Background(), nil, "initial_bind")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHARACTER_GENESIS_FAILED")
}

// --- reaping-reject guard (round-6 R6-2) ---

func TestCharacterGenesisCreateRejectsReapingPlayer(t *testing.T) {
	seq := []string{}
	writer := &fakeCharWriter{seq: &seq}
	bind := &fakeGenesisBindingCreator{seq: &seq}
	outboxW := &fakeOutboxWriter{seq: &seq}
	guard := &fakeReapingGuard{seq: &seq, guardErr: oops.Code("PLAYER_REAPING").Errorf("reaping")}

	svc, err := auth.NewCharacterGenesisService(writer, fakeGenesisTransactor{}, bind, outboxW, guard)
	require.NoError(t, err)

	err = svc.Create(context.Background(), newGenesisChar(t), "initial_bind_guest")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLAYER_REAPING")

	// The guard ran FIRST and rejected: no character inserted, no binding, no
	// envelope written for a reaping player.
	assert.Equal(t, 1, guard.calls)
	assert.Equal(t, []string{"guard"}, seq)
	assert.Nil(t, writer.created)
	assert.Equal(t, 0, bind.calls)
	assert.Equal(t, 0, outboxW.calls)
}
