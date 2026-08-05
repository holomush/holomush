// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/charname/blocklist"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
)

// recordingGuestGenesis is a hand-rolled auth.CharacterGenesis fake for guest
// tests: it records the character and bind reason and returns the configured
// error (simulating the character + binding + envelope atomic unit).
type recordingGuestGenesis struct {
	err            error
	calls          int
	lastChar       *world.Character
	lastAdmitted   charname.Admitted
	lastBindReason string
}

func (g *recordingGuestGenesis) Create(_ context.Context, char *world.Character, name charname.Admitted, bindReason string) error {
	g.calls++
	g.lastChar = char
	g.lastAdmitted = name
	g.lastBindReason = bindReason
	return g.err
}

// recordingGuestCleaner is a hand-rolled auth.GuestCleaner fake standing in for
// the tombstone-emitting CharacterReapingService in guest-service unit tests. It
// records the cleanup call so a test can assert failed-guest cleanup routes
// through the reaping service (not a raw player-cascade delete).
type recordingGuestCleaner struct {
	err    error
	calls  int
	lastID ulid.ULID
}

func (c *recordingGuestCleaner) DeleteGuestPlayer(_ context.Context, playerID ulid.ULID) error {
	c.calls++
	c.lastID = playerID
	return c.err
}

func TestNewGuestServiceNilDeps(t *testing.T) {
	validNamer := mocks.NewMockGuestNamer(t)
	validPlayers := mocks.NewMockPlayerRepository(t)
	validChars := mocks.NewMockGuestCharacterRepository(t)
	validSessions := mocks.NewMockPlayerSessionRepository(t)
	validGenesis := &recordingGuestGenesis{}
	validCleaner := &recordingGuestCleaner{}

	tests := []struct {
		name     string
		namer    auth.GuestNamer
		players  auth.PlayerRepository
		chars    auth.GuestCharacterRepository
		sessions auth.PlayerSessionRepository
		genesis  auth.CharacterGenesis
		cleaner  auth.GuestCleaner
		wantErr  string
	}{
		{"nil namer", nil, validPlayers, validChars, validSessions, validGenesis, validCleaner, "guest namer is required"},
		{"nil players", validNamer, nil, validChars, validSessions, validGenesis, validCleaner, "players repository is required"},
		{"nil chars", validNamer, validPlayers, nil, validSessions, validGenesis, validCleaner, "character repository is required"},
		{"nil sessions", validNamer, validPlayers, validChars, nil, validGenesis, validCleaner, "player sessions repository is required"},
		{"nil genesis", validNamer, validPlayers, validChars, validSessions, nil, validCleaner, "character genesis service is required"},
		{"nil cleaner", validNamer, validPlayers, validChars, validSessions, validGenesis, nil, "guest cleaner is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := auth.NewGuestService(tt.namer, tt.players, tt.chars, tt.sessions, tt.genesis, tt.cleaner, testGate())
			require.Error(t, err)
			assert.Nil(t, svc)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestGuestServiceCreatesGuestSuccessfully(t *testing.T) {
	ctx := context.Background()
	startLoc := ulid.MustNew(ulid.Now(), nil)
	guestName := "Sapphire_Diamond"

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{}

	charName := "Sapphire Diamond" // underscore→space conversion
	cleaner := &recordingGuestCleaner{}

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)

	chars.EXPECT().ExistsByNormalizedName(ctx, strings.ToLower(charName), (*ulid.ULID)(nil)).Return(false, nil).Once()
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(nil).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner, testGate())
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, guestName, result.Player.Username)
	assert.True(t, result.Player.IsGuest)
	assert.Equal(t, charName, result.Character.Name)
	assert.NotNil(t, result.Character.LocationID)
	assert.Equal(t, startLoc, *result.Character.LocationID)
	assert.NotEmpty(t, result.RawToken)
	assert.NotNil(t, result.PlayerSession)
	assert.Equal(t, result.Player.ID, result.PlayerSession.PlayerID)

	// Character routed through the genesis service with the guest binding reason.
	assert.Equal(t, 1, genesis.calls)
	assert.Equal(t, "initial_bind_guest", genesis.lastBindReason)
	assert.Equal(t, result.Character, genesis.lastChar)
}

func TestGuestServiceRetriesOnNameCollision(t *testing.T) {
	ctx := context.Background()
	startLoc := ulid.MustNew(ulid.Now(), nil)
	takenName := "Ruby_Flame"
	freeName := "Jade_River"

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{}

	cleaner := &recordingGuestCleaner{}
	takenCharName := "Ruby Flame" // underscore→space form
	freeCharName := "Jade River"  // underscore→space form

	// First name is taken in DB; second name is free.
	namer.EXPECT().GenerateName().Return(takenName, nil).Once()
	chars.EXPECT().ExistsByNormalizedName(ctx, strings.ToLower(takenCharName), (*ulid.ULID)(nil)).Return(true, nil).Once()
	namer.EXPECT().ReleaseGuest(takenName).Once()

	namer.EXPECT().GenerateName().Return(freeName, nil).Once()
	chars.EXPECT().ExistsByNormalizedName(ctx, strings.ToLower(freeCharName), (*ulid.ULID)(nil)).Return(false, nil).Once()

	namer.EXPECT().StartLocation().Return(startLoc)
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(nil).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner, testGate())
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, freeName, result.Player.Username)
	assert.Equal(t, freeCharName, result.Character.Name)
	assert.Equal(t, 1, genesis.calls)
}

func TestGuestServiceSucceedsWhenDefaultCharacterUpdateFails(t *testing.T) {
	// Update failure is best-effort — CreateGuest must still succeed.
	ctx := context.Background()
	startLoc := ulid.MustNew(ulid.Now(), nil)
	guestName := "Coral_Breeze"

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{}
	cleaner := &recordingGuestCleaner{}

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)
	chars.EXPECT().ExistsByNormalizedName(ctx, "coral breeze", (*ulid.ULID)(nil)).Return(false, nil).Once()
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(errors.New("db timeout")).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(nil).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner, testGate())
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestGuestServiceReturnsErrorWhenPlayerCreateFails(t *testing.T) {
	ctx := context.Background()
	guestName := "Amber_Storm"
	dbErr := errors.New("db error")

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{}

	cleaner := &recordingGuestCleaner{}
	amberStartLoc := ulid.MustNew(ulid.Now(), nil)
	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(amberStartLoc)
	chars.EXPECT().ExistsByNormalizedName(ctx, "amber storm", (*ulid.ULID)(nil)).Return(false, nil).Once()
	// player.Create (committed first, own pool) fails -> release name, no genesis.
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(dbErr).Once()
	namer.EXPECT().ReleaseGuest(guestName).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner, testGate())
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, 0, genesis.calls)
}

func TestGuestServiceReturnsErrorWhenCharCreateFails(t *testing.T) {
	ctx := context.Background()
	guestName := "Topaz_Wind"
	startLoc := ulid.MustNew(ulid.Now(), nil)

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{err: errors.New("db error")}
	cleaner := &recordingGuestCleaner{}

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)
	chars.EXPECT().ExistsByNormalizedName(ctx, "topaz wind", (*ulid.ULID)(nil)).Return(false, nil).Once()
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	// genesis fails after the player commit -> release name + orphan-player cleanup
	// through the tombstone-emitting reaping service (D-06), NOT a raw player delete.
	namer.EXPECT().ReleaseGuest(guestName).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner, testGate())
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
	errutil.AssertErrorCode(t, err, "GUEST_CREATE_FAILED")

	// Failed-guest cleanup routed through the reaping service (tombstone-emitting),
	// not a raw player-cascade delete.
	assert.Equal(t, 1, cleaner.calls)
}

func TestGuestServiceReturnsErrorWhenSessionCreateFails(t *testing.T) {
	ctx := context.Background()
	guestName := "Marble_Creek"
	startLoc := ulid.MustNew(ulid.Now(), nil)

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{}
	cleaner := &recordingGuestCleaner{}

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)
	chars.EXPECT().ExistsByNormalizedName(ctx, "marble creek", (*ulid.ULID)(nil)).Return(false, nil).Once()
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(errors.New("session db error")).Once()
	namer.EXPECT().ReleaseGuest(guestName).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner, testGate())
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
	errutil.AssertErrorCode(t, err, "GUEST_CREATE_FAILED")

	// best-effort cleanup after session-create failure routes through the reaping
	// service (character tombstoned before player delete), not a raw player delete.
	assert.Equal(t, 1, cleaner.calls)
}

func TestGuestServiceReturnsErrorWhenNameExhausted(t *testing.T) {
	ctx := context.Background()

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{}
	cleaner := &recordingGuestCleaner{}

	// All 10 generated names already exist in the database.
	for range 10 {
		name := "Taken_Name"
		namer.EXPECT().GenerateName().Return(name, nil).Once()
		chars.EXPECT().ExistsByNormalizedName(ctx, "taken name", (*ulid.ULID)(nil)).Return(true, nil).Once()
		namer.EXPECT().ReleaseGuest(name).Once()
	}

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner, testGate())
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
	errutil.AssertErrorCode(t, err, "GUEST_NAME_EXHAUSTED")
}

func TestGuestServiceReturnsErrorWhenExistsByNameFails(t *testing.T) {
	ctx := context.Background()
	guestName := "Crystal_Fog"

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{}
	cleaner := &recordingGuestCleaner{}

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	chars.EXPECT().ExistsByNormalizedName(ctx, "crystal fog", (*ulid.ULID)(nil)).Return(false, errors.New("db error")).Once()
	namer.EXPECT().ReleaseGuest(guestName).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner, testGate())
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
	errutil.AssertErrorCode(t, err, "GUEST_CREATE_FAILED")
}

// Verifies: INV-CRYPTO-120
// Asserts guest creation routes the character through the genesis service with
// reason "initial_bind_guest" — so the binding is minted in the SAME transaction
// as the character + genesis envelope (no orphan character row without a binding).
func TestCreateGuestMintsBinding(t *testing.T) {
	ctx := context.Background()
	startLoc := ulid.MustNew(ulid.Now(), nil)
	guestName := "Onyx_River"

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{}
	cleaner := &recordingGuestCleaner{}

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)
	chars.EXPECT().ExistsByNormalizedName(ctx, "onyx river", (*ulid.ULID)(nil)).Return(false, nil).Once()
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(nil).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner, testGate())
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Character routed through genesis with reason "initial_bind_guest" — the
	// genesis service mints the binding atomically with the character + envelope.
	assert.Equal(t, 1, genesis.calls)
	assert.Equal(t, "initial_bind_guest", genesis.lastBindReason)
	assert.Equal(t, result.Character.ID, genesis.lastChar.ID)
	assert.Equal(t, result.Player.ID, genesis.lastChar.PlayerID)
}

// unverifiableCorpusLookup is the D-30 fail-closed state: the corpus carries a
// row with a NULL skeleton, so the gate refuses to adjudicate ANY name.
type unverifiableCorpusLookup struct{}

func (unverifiableCorpusLookup) SkeletonExists(_ context.Context, _ string, _ *ulid.ULID) (bool, bool, error) {
	return false, true, nil
}

// failingCorpusLookup is the other corpus-level fault: the lookup query itself
// failed (database down, pool exhausted).
type failingCorpusLookup struct{}

func (failingCorpusLookup) SkeletonExists(_ context.Context, _ string, _ *ulid.ULID) (bool, bool, error) {
	return false, false, errors.New("connection refused")
}

// TestGuestServiceReportsACorpusFaultAsItselfNotAsNameExhaustion is WR-01's
// regression.
//
// Both faults below refuse EVERY generated candidate identically, so the retry
// loop used to burn all ten attempts and return GUEST_NAME_EXHAUSTED — "unable
// to find unique guest name" — for a database outage, with the real error
// discarded and nothing logged. The operator-visible symptom pointed at the
// name generator; the cause was the database.
//
// `.Once()` expectations are deliberately absent on the namer: the point of the
// fix is that the loop STOPS on the first attempt, so a second GenerateName
// would be a mock failure.
func TestGuestServiceReportsACorpusFaultAsItselfNotAsNameExhaustion(t *testing.T) {
	tests := []struct {
		name string
		// wantCode is the GATE's own code, not the outer GUEST_CREATE_FAILED
		// wrapper: errutil.AssertErrorCode resolves the DEEPEST code in the
		// chain (issue #4902), and that is the right contract here — the
		// operator wants "the corpus could not be adjudicated", not a generic
		// create failure.
		wantCode string
		lookup   charname.SkeletonLookup
	}{
		{
			"an unverifiable corpus (the 000054 -> 000055 fail-closed window)",
			"NAME_SKELETON_UNVERIFIABLE",
			unverifiableCorpusLookup{},
		},
		{
			"a failed corpus lookup (database unreachable)",
			"NAME_SKELETON_LOOKUP_FAILED",
			failingCorpusLookup{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			namer := mocks.NewMockGuestNamer(t)
			players := mocks.NewMockPlayerRepository(t)
			chars := mocks.NewMockGuestCharacterRepository(t)
			sessions := mocks.NewMockPlayerSessionRepository(t)

			const candidate = "Crystal_Fog"
			namer.EXPECT().GenerateName().Return(candidate, nil).Once()
			namer.EXPECT().ReleaseGuest(candidate).Once()

			svc, err := auth.NewGuestService(
				namer, players, chars, sessions,
				&recordingGuestGenesis{}, &recordingGuestCleaner{},
				&charname.Gate{Skeletons: tt.lookup},
			)
			require.NoError(t, err)

			result, err := svc.CreateGuest(ctx)
			require.Error(t, err)
			assert.Nil(t, result)
			errutil.AssertErrorCode(t, err, tt.wantCode)
			assert.NotContains(t, err.Error(), "unable to find unique guest name",
				"a corpus fault must not be reported as name exhaustion")
		})
	}
}

// TestGuestServiceStillReportsGenuineNameExhaustion is the paired control.
//
// A per-candidate refusal — one the NEXT generated name could clear — must keep
// retrying and must keep the existing GUEST_NAME_EXHAUSTED contract, so the fix
// above cannot have been achieved by aborting on every refusal.
func TestGuestServiceStillReportsGenuineNameExhaustion(t *testing.T) {
	ctx := context.Background()

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)

	// Every candidate is refused by the block list — a policy verdict about the
	// candidate, not about the corpus.
	blocked, err := blocklist.Compile([]string{"taken*"})
	require.NoError(t, err)

	for range 10 {
		const name = "Taken_Name"
		namer.EXPECT().GenerateName().Return(name, nil).Once()
		namer.EXPECT().ReleaseGuest(name).Once()
	}

	svc, err := auth.NewGuestService(
		namer, players, chars, sessions,
		&recordingGuestGenesis{}, &recordingGuestCleaner{},
		&charname.Gate{Skeletons: noCollisionLookup{}, BlockList: blocked},
	)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
	errutil.AssertErrorCode(t, err, "GUEST_NAME_EXHAUSTED")
	errutil.AssertErrorContext(t, err, "last_refusal_code", "NAME_BLOCKED")
}
