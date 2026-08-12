// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/auth"
	authmocks "github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/internal/charname/syntax"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/world"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
)

// This file drives the owner audience's CREATE surface.
//
// Three properties separate it from the mutation surface next door, and each is
// asserted here rather than assumed:
//
//   - the create reaches the shared guest gate and NOTHING else — there is no
//     ownership to resolve and no version to guard, so the specs that must not
//     write wire doubles that fail the test the moment a second gate is reached;
//   - EVERY refusal carries an AUTHORED constant, never an interpolated inner
//     error, so each spec asserts the wire MESSAGE and not merely the code;
//   - a profile-seeding failure is SWALLOWED (the ratified two-transaction
//     shape), so the failure specs assert SUCCESS with an absent field rather
//     than an error.

const createTestToken = "create-surface-session-token"

// recordedCreateCall is one observed CreateBound call. Both the player and the
// bind reason are recorded, not just the name: a handler that seated the RIGHT
// name on the WRONG player, or with no binding at all, would satisfy an
// assertion that looked only at the name.
type recordedCreateCall struct {
	playerID   ulid.ULID
	name       string
	bindReason string
}

// recordingCreator is the double behind characterAccessCreator. It returns a
// fixed character or a fixed error, and records every call.
type recordingCreator struct {
	created *world.Character
	err     error
	calls   []recordedCreateCall
}

func (c *recordingCreator) CreateBound(_ context.Context, playerID ulid.ULID, name, bindReason string) (*world.Character, error) {
	c.calls = append(c.calls, recordedCreateCall{playerID: playerID, name: name, bindReason: bindReason})
	if c.err != nil {
		return nil, c.err
	}
	return c.created, nil
}

// createHarness is one wired facade plus the fixture identifiers a create spec
// drives it with.
type createHarness struct {
	srv      *CharacterAccessServer
	creator  *recordingCreator
	mutator  *recordingWorldMutator
	created  *world.Character
	playerID ulid.ULID
	token    string
}

// createFixture describes the caller and the doubles a create spec wants.
type createFixture struct {
	// isGuest makes the resolved player a guest, so resolveAndGate denies.
	isGuest bool
	// createErr is what the create pipeline returns.
	createErr error
	// failOnCreate leaves the creator UNWIRED and installs the fail-on-call
	// double instead, so a spec whose behavior is "the handler refuses BEFORE
	// seating anything" fails at the call rather than passing on a nil check.
	failOnCreate bool
	// profileErr is what the profile-seeding write returns.
	profileErr error
	// failOnProfileWrite fails the test if the seeding write is reached at all.
	failOnProfileWrite bool
	// storedName is the name the read-back reports. It defaults to the
	// normalized display form so the D-88 echo is exercised by default.
	storedName string
	// rows is what the post-create property enumeration returns.
	rows []*world.EntityProperty
}

// createTestStoredName is the SERVER-stored display form the read-back reports.
// It differs from every submitted name the specs below send, so an assertion on
// it fails the moment a handler echoes the request instead of the row.
const createTestStoredName = "Ada Lovelace"

func newCreateHarness(t *testing.T, f createFixture) *createHarness {
	t.Helper()

	playerID := idgen.New()
	ps := &auth.PlayerSession{ID: idgen.New(), PlayerID: playerID, TokenHash: auth.HashSessionToken(createTestToken)}

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, ps.TokenHash).Return(ps, nil).Maybe()
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, mock.Anything).
		Return(nil, oops.Code("PLAYER_SESSION_NOT_FOUND").Errorf("no such session")).Maybe()
	sessionRepo.EXPECT().RefreshTTL(mock.Anything, ps.ID, auth.PlayerSessionTTL).Return(nil).Maybe()

	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).
		Return(&auth.Player{ID: playerID, IsGuest: f.isGuest}, nil).Maybe()

	storedName := f.storedName
	if storedName == "" {
		storedName = createTestStoredName
	}
	created := ownedCharacterFixture(playerID, storedName, world.StatusActive)

	// The read-back is the SAME owned-character path GetMyCharacter uses, so the
	// roster the gate reads must carry the freshly created row.
	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{created}, nil).Maybe()

	reader := &ownerWorldReader{
		rows: f.rows,
		desc: world.CharacterDescription{Name: storedName, Description: profileTestDescription},
	}
	mutator := &recordingWorldMutator{
		t:          t,
		failOnCall: f.failOnProfileWrite,
		profileErr: f.profileErr,
	}

	creator := &recordingCreator{created: created, err: f.createErr}
	var handle characterAccessCreator = creator
	if f.failOnCreate {
		handle = &failOnCallCreator{t: t}
	}

	return &createHarness{
		srv: NewCharacterAccessServer(reader, mutator, &failOnCallProfileVisibility{t: t},
			&failOnCallDirectoryGate{t: t}, &failOnCallDirectoryReader{t: t},
			sessionRepo, playerRepo, charRepo, handle),
		creator:  creator,
		mutator:  mutator,
		created:  created,
		playerID: playerID,
		token:    createTestToken,
	}
}

// createRequest builds a well-formed name-only create. Individual specs set the
// prose fields afterwards.
func (h *createHarness) createRequest(name string) *characteraccessv1.CreateCharacterRequest {
	return &characteraccessv1.CreateCharacterRequest{
		PlayerSessionToken: h.token,
		Name:               name,
	}
}

// propertyRow builds one entity_properties row the post-create read-back
// returns.
func propertyRow(name, value string) *world.EntityProperty {
	return &world.EntityProperty{ID: idgen.New(), Name: name, Value: &value}
}

// TestCreateCharacterSeatsTheWholeIdentityCardAndEchoesTheServerStoredName is
// behavior 1: a create carrying the name plus all five prose values writes all
// five in ONE guarded domain call and answers with the SERVER's display name.
func TestCreateCharacterSeatsTheWholeIdentityCardAndEchoesTheServerStoredName(t *testing.T) {
	t.Parallel()

	h := newCreateHarness(t, createFixture{rows: []*world.EntityProperty{
		propertyRow("profile.pronouns", "they/them"),
		propertyRow("profile.concept", "a wandering archivist"),
		propertyRow("profile.species", "human"),
		propertyRow("profile.age", "early 30s"),
		propertyRow("profile.faction", "the Cartographers"),
	}})

	req := h.createRequest("  ada   lovelace ")
	req.Pronouns = "they/them"
	req.Concept = "a wandering archivist"
	req.Species = "human"
	req.Age = "early 30s"
	req.Faction = "the Cartographers"

	resp, err := h.srv.CreateCharacter(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp.GetCharacter())

	assert.Equal(t, h.created.ID.String(), resp.GetCharacter().GetId())
	assert.Equal(t, createTestStoredName, resp.GetCharacter().GetName(),
		"the response carries the SERVER-stored display form, never the submitted bytes (D-88)")
	assert.NotEqual(t, req.GetName(), resp.GetCharacter().GetName(),
		"this fixture submits a form normalization changes, so an echo of the request would be visible here")
	assert.Equal(t, map[string]string{
		"profile.pronouns": "they/them",
		"profile.concept":  "a wandering archivist",
		"profile.species":  "human",
		"profile.age":      "early 30s",
		"profile.faction":  "the Cartographers",
	}, resp.GetCharacter().GetProfile())

	require.Len(t, h.creator.calls, 1, "exactly one create per accepted request")
	assert.Equal(t, h.playerID, h.creator.calls[0].playerID)
	assert.Equal(t, req.GetName(), h.creator.calls[0].name,
		"the SUBMITTED name is forwarded verbatim; the server, not this handler, normalizes it")
	assert.Equal(t, "initial_bind", h.creator.calls[0].bindReason,
		"the web create and the telnet CREATE verb must stamp the SAME binding reason")

	require.Len(t, h.mutator.profileWrites, 1, "the five values are ONE second-transaction write, not five")
	got := h.mutator.profileWrites[0]
	assert.Equal(t, h.created.ID, got.characterID)
	assert.Equal(t, 1, got.expectedVersion,
		"a freshly created row is at version 1 (migration 000049, 01-SPEC §9.4.2)")
	assert.Equal(t, world.HumanCaller(access.CharacterSubject(h.created.ID.String())), got.caller,
		"the seeding write is driven with the OWNING CHARACTER's subject, never a viewer principal")
	assert.Len(t, got.attrs, 5)
}

// TestCreateCharacterWithOnlyANameMakesNoSecondDomainCall is behavior 2. The
// mutator fails the test on call, so a regression that seeded five blank rows
// cannot pass as an empty profile.
func TestCreateCharacterWithOnlyANameMakesNoSecondDomainCall(t *testing.T) {
	t.Parallel()

	h := newCreateHarness(t, createFixture{failOnProfileWrite: true})

	resp, err := h.srv.CreateCharacter(context.Background(), h.createRequest("Ada"))
	require.NoError(t, err)
	require.NotNil(t, resp.GetCharacter())
	assert.Empty(t, resp.GetCharacter().GetProfile(),
		"a name-only create seeds nothing, so the profile map is absent rather than five empty strings")
	assert.Empty(t, h.mutator.profileWrites)
}

// TestCreateCharacterReturnsSuccessWhenTheProfileSeedingWriteFails is behavior
// 3 and the ratified Q1 shape (option-a, two transactions, the create
// authoritative). The character is real and the un-set fields are simply absent.
func TestCreateCharacterReturnsSuccessWhenTheProfileSeedingWriteFails(t *testing.T) {
	t.Parallel()

	h := newCreateHarness(t, createFixture{
		profileErr: oops.Code("WORLD_WRITE_FAILED").Errorf("the property write did not commit"),
	})

	req := h.createRequest("Ada")
	req.Pronouns = "they/them"

	resp, err := h.srv.CreateCharacter(context.Background(), req)
	require.NoError(t, err,
		"the create transaction committed and its name is reserved; reporting failure would send the player into a retry that collides with their own new character")
	require.NotNil(t, resp.GetCharacter())
	assert.Equal(t, h.created.ID.String(), resp.GetCharacter().GetId(),
		"the character id in the response is a real, durable row")
	assert.Empty(t, resp.GetCharacter().GetProfile(),
		"the field that failed to write is ABSENT, not present-and-empty")
	require.Len(t, h.mutator.profileWrites, 1, "the seeding write was attempted")
}

// TestCreateCharacterDeniesAGuestAndAdmitsTheSameFixturesNonGuest is behavior 4.
// The negative and its paired positive control are one spec on purpose: a
// denial assertion that passed because the fixture was broken would look
// identical to a working gate.
func TestCreateCharacterDeniesAGuestAndAdmitsTheSameFixturesNonGuest(t *testing.T) {
	t.Parallel()

	t.Run("a guest is refused before the create pipeline is reached", func(t *testing.T) {
		t.Parallel()
		h := newCreateHarness(t, createFixture{isGuest: true, failOnCreate: true, failOnProfileWrite: true})

		resp, err := h.srv.CreateCharacter(context.Background(), h.createRequest("Ada"))
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
		assert.Equal(t, characterGuestDenialMessage, status.Convert(err).Message(),
			"the character facade denies with its OWN message, never the scene facade's")
	})

	t.Run("the same fixture's non-guest is admitted", func(t *testing.T) {
		t.Parallel()
		h := newCreateHarness(t, createFixture{})

		resp, err := h.srv.CreateCharacter(context.Background(), h.createRequest("Ada"))
		require.NoError(t, err)
		require.NotNil(t, resp.GetCharacter())
		assert.Len(t, h.creator.calls, 1)
	})
}

// TestCreateCharacterRendersBothNameTakenProducersIdentically is behavior 5. The
// §6.1.1 pre-check and the 23505 handler over characters_normalized_name_key are
// two paths to one contract; a differential between them would report which one
// won a race.
func TestCreateCharacterRendersBothNameTakenProducersIdentically(t *testing.T) {
	t.Parallel()

	// Both error shapes are transcribed from internal/auth/character_service.go:
	// the pre-check at :153 and the unique-violation handler at :222.
	producers := map[string]error{
		"the §6.1.1 uniqueness pre-check": oops.Code("CHARACTER_NAME_TAKEN").
			With("name", "Ada Lovelace").
			Errorf("character name %q is already taken", "Ada Lovelace"),
		"the 23505 handler on characters_normalized_name_key": oops.Code("CHARACTER_NAME_TAKEN").
			With("name", "Ada Lovelace").
			Errorf("character name %q is already taken", "Ada Lovelace"),
	}

	for name, produced := range producers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newCreateHarness(t, createFixture{createErr: produced, failOnProfileWrite: true})

			resp, err := h.srv.CreateCharacter(context.Background(), h.createRequest("Ada Lovelace"))
			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Equal(t, codes.AlreadyExists, status.Code(err))
			assert.Equal(t, msgCharacterNameTaken, status.Convert(err).Message())
		})
	}
}

// TestCreateCharacterRefusesAConfusableNameWithoutNamingTheCollidingCharacter is
// behavior 6 and threat T-05-03-03: the refusal must not become an enumeration
// oracle.
func TestCreateCharacterRefusesAConfusableNameWithoutNamingTheCollidingCharacter(t *testing.T) {
	t.Parallel()

	// The gate's own error carries the colliding neighbourhood in its context
	// map and its message; neither may reach the wire.
	collider := "Ada Lovelace"
	h := newCreateHarness(t, createFixture{
		createErr: oops.Code("NAME_CONFUSABLE").
			With("colliding_name", collider).
			Errorf("that character name is too similar to an existing one"),
		failOnProfileWrite: true,
	})

	resp, err := h.srv.CreateCharacter(context.Background(), h.createRequest("Adа Lovelace"))
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	message := status.Convert(err).Message()
	assert.Equal(t, msgCharacterNameConfusable, message)
	assert.NotContains(t, message, collider,
		"a refusal naming the colliding character would let an attacker read back the corpus one submission at a time (§6.1.2)")
	assert.NotContains(t, message, "colliding_name",
		"the oops context map must not reach the wire")
}

// TestCreateCharacterSeparatesABlankNameFromAnInvisibleOnlyName is behavior 7 —
// the one-code-two-messages split. Both are CHARACTER_INVALID_NAME on the wire
// status; the COPY is what differs, and it is chosen on the handler's own input.
func TestCreateCharacterSeparatesABlankNameFromAnInvisibleOnlyName(t *testing.T) {
	t.Parallel()

	// Both errors carry the SAME code, exactly as auth.mapGateError produces
	// them from charname's shared NAME_EMPTY_NORMAL_FORM.
	emptyNormalForm := oops.Code("CHARACTER_INVALID_NAME").Errorf("the submitted name has no normal form")

	cases := []struct {
		name      string
		submitted string
		expected  string
	}{
		{"a blank submission", "   ", msgCharacterNameBlank},
		// ESCAPE SEQUENCES, NOT THE LITERAL CODEPOINTS. U+200B zero-width space,
		// U+200D zero-width joiner and U+2060 word joiner are exactly the input
		// this case exists to cover, and pasting them raw makes a source line that
		// renders as an empty string in every editor and diff — the reviewer's own
		// version of the bug the player hits.
		{"a submission of nothing but invisible codepoints", "\u200b\u200d\u2060", msgCharacterNameNoVisibleCharacter},
	}

	seen := make([]string, 0, len(cases))
	for _, tc := range cases {
		h := newCreateHarness(t, createFixture{createErr: emptyNormalForm, failOnProfileWrite: true})

		resp, err := h.srv.CreateCharacter(context.Background(), h.createRequest(tc.submitted))
		require.Error(t, err, tc.name)
		assert.Nil(t, resp)
		assert.Equal(t, codes.InvalidArgument, status.Code(err), tc.name)
		assert.Equal(t, tc.expected, status.Convert(err).Message(), tc.name)
		seen = append(seen, status.Convert(err).Message())
	}

	require.Len(t, seen, 2)
	assert.NotEqual(t, seen[0], seen[1],
		"telling someone who typed zero-width joiners to 'please enter a character name' tells them to do the thing they believe they already did")
}

// TestCreateCharacterRendersASyntaxRefusalWithTheSyntaxCopy is the third
// CHARACTER_INVALID_NAME arm: a typed *syntax.ValidationError in the chain wins
// over both empty-normal-form messages, whatever the submission looked like.
func TestCreateCharacterRendersASyntaxRefusalWithTheSyntaxCopy(t *testing.T) {
	t.Parallel()

	// Transcribed from auth.mapGateError's NAME_INVALID_SYNTAX arm (:303), which
	// carries the typed error forward so the errors.As chain survives.
	h := newCreateHarness(t, createFixture{
		createErr: oops.Code("CHARACTER_INVALID_NAME").
			With("name", "Ada9").
			Wrap(&syntax.ValidationError{Field: "name", Message: "contains a digit"}),
		failOnProfileWrite: true,
	})

	resp, err := h.srv.CreateCharacter(context.Background(), h.createRequest("Ada9"))
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, msgCharacterInvalidName, status.Convert(err).Message())
	assert.NotContains(t, status.Convert(err).Message(), "contains a digit",
		"the inner validation message is logged, never interpolated into the returned status")
}

// TestCreateCharacterMapsEveryCreatePathCodeToItsPinnedStatusAndAuthoredMessage
// is behaviors 8 and 9 as one closed table. Every row is asserted at the WIRE —
// the status code and the status message — so a mapping that produced the right
// code with the wrong copy still fails.
func TestCreateCharacterMapsEveryCreatePathCodeToItsPinnedStatusAndAuthoredMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		produced error
		wantCode codes.Code
		wantMsg  string
	}{
		{
			"a reached character limit is a precondition, not an invalid argument",
			oops.Code("CHARACTER_LIMIT_REACHED").With("max", 5).Errorf("player has reached the maximum of 5 characters"),
			codes.FailedPrecondition, msgCharacterLimitReached,
		},
		{
			"a grid with no starting location is a precondition the player cannot fix",
			oops.Code("CHARACTER_NO_STARTING_LOCATION").Errorf("no starting location is configured"),
			codes.FailedPrecondition, msgCharacterNoStartingLocation,
		},
		{
			"an unverifiable skeleton is transient, so the name was not refused",
			oops.Code("NAME_SKELETON_UNVERIFIABLE").Errorf("the corpus cannot answer yet"),
			codes.Unavailable, msgCharacterNameUnverifiable,
		},
		{
			"a blocked name names neither the pattern nor its index",
			oops.Code("NAME_BLOCKED").Errorf("that character name is not available"),
			codes.InvalidArgument, msgCharacterNameBlocked,
		},
		{
			"a mixed-script name is refused with the one-writing-system copy",
			oops.Code("NAME_MIXED_SCRIPT").With("scripts", "Cyrillic+Latin").Errorf("character names cannot mix the Cyrillic and Latin scripts; use one writing system"),
			codes.InvalidArgument, msgCharacterNameMixedScript,
		},
		{
			"an unclassifiable codepoint is refused with its own copy, not the mixed-script advice",
			oops.Code("NAME_UNASSIGNED_SCRIPT").With("codepoint", "U+E0000").Errorf("character name contains a character this server cannot classify"),
			codes.InvalidArgument, msgCharacterNameUnassignedScript,
		},
		{
			"a skeleton lookup failure is an outage with the generic create copy",
			oops.Code("NAME_SKELETON_LOOKUP_FAILED").Errorf("the skeleton store is unreachable"),
			codes.Internal, msgCharacterCreateFailed,
		},
		{
			"an unwrapped create failure is an outage with the generic create copy",
			oops.Code("CHARACTER_CREATE_FAILED").With("id", "01J").Errorf("the genesis transaction did not commit"),
			codes.Internal, msgCharacterCreateFailed,
		},
		{
			// THE DEEPEST-CODE PROPERTY, pinned rather than commented. oops
			// resolves the DEEPEST code in the chain (#4902), so this error
			// reports PG_CONNECTION_LOST and never CHARACTER_CREATE_FAILED. The
			// mapping is safe because the default arm returns the IDENTICAL pair
			// the CHARACTER_CREATE_FAILED arm does — which is exactly what this
			// row asserts.
			"a create failure WRAPPING a differently-coded inner error still lands on Internal",
			oops.Code("CHARACTER_CREATE_FAILED").Wrap(
				oops.Code("PG_CONNECTION_LOST").Errorf("connection reset by peer"),
			),
			codes.Internal, msgCharacterCreateFailed,
		},
		{
			"a non-oops error is an outage with the generic create copy",
			errFromPipeline("the pipeline returned a bare error"),
			codes.Internal, msgCharacterCreateFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newCreateHarness(t, createFixture{createErr: tt.produced, failOnProfileWrite: true})

			resp, err := h.srv.CreateCharacter(context.Background(), h.createRequest("Ada"))
			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Equal(t, tt.wantCode, status.Code(err))
			assert.Equal(t, tt.wantMsg, status.Convert(err).Message())
		})
	}
}

// errFromPipeline builds a plain, un-coded error — the shape oops.AsOops
// declines.
func errFromPipeline(msg string) error {
	return &plainPipelineError{msg: msg}
}

type plainPipelineError struct{ msg string }

func (e *plainPipelineError) Error() string { return e.msg }

// TestCreateCharacterNeverReturnsAnInternalCodeStringOnTheWire is behavior 10.
// It drives the whole mapping table again and asserts the NEGATIVE, because a
// message assertion proves what a message IS and this proves what no message may
// contain.
func TestCreateCharacterNeverReturnsAnInternalCodeStringOnTheWire(t *testing.T) {
	t.Parallel()

	internalCodes := []string{
		"CHARACTER_INVALID_NAME", "CHARACTER_NAME_TAKEN", "CHARACTER_LIMIT_REACHED",
		"CHARACTER_CREATE_FAILED", "CHARACTER_NO_STARTING_LOCATION",
		"NAME_CONFUSABLE", "NAME_BLOCKED", "NAME_MIXED_SCRIPT",
		"NAME_UNASSIGNED_SCRIPT", "NAME_SKELETON_UNVERIFIABLE", "NAME_SKELETON_LOOKUP_FAILED",
		"NAME_EMPTY_NORMAL_FORM", "NAME_INVALID_SYNTAX",
	}

	for _, code := range internalCodes {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			h := newCreateHarness(t, createFixture{
				createErr:          oops.Code(code).Errorf("refused: %s", code),
				failOnProfileWrite: true,
			})

			_, err := h.srv.CreateCharacter(context.Background(), h.createRequest("Ada"))
			require.Error(t, err)
			message := status.Convert(err).Message()
			for _, candidate := range internalCodes {
				assert.NotContains(t, message, candidate,
					"the wire message must be an authored constant, never an internal code string")
			}
			assert.NotContains(t, message, "refused:",
				"the inner error text must not be interpolated into the returned status")
		})
	}
}

// TestCreateCharacterRefusesAnOversizedProfileValueBeforeSeatingAnything is the
// create surface's half of threat T-05-03-08: the five values run through the
// SAME cap table the mask surface uses, and a refusal happens while refusing is
// still free — before a row exists and before a name is reserved.
func TestCreateCharacterRefusesAnOversizedProfileValueBeforeSeatingAnything(t *testing.T) {
	t.Parallel()

	h := newCreateHarness(t, createFixture{failOnCreate: true, failOnProfileWrite: true})

	req := h.createRequest("Ada")
	req.Concept = strings.Repeat("a", world.MaxNameLength+1)

	resp, err := h.srv.CreateCharacter(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, characterProfileFieldInvalidMessage, status.Convert(err).Message())
}

// TestEveryCreateProfileSeedPathIsAMaskablePath pins the linkage the create
// surface's cap lookup depends on. A seed naming a path the mask table lacks
// would read a ZERO byte cap and refuse every non-empty value — fail-closed, but
// silently, so the state is made unreachable here rather than merely survivable.
func TestEveryCreateProfileSeedPathIsAMaskablePath(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, createCharacterProfileSeeds)
	for _, seed := range createCharacterProfileSeeds {
		field, ok := updateCharacterProfileMaskablePaths[seed.path]
		require.Truef(t, ok,
			"create seed %q is not a member of updateCharacterProfileMaskablePaths, so its cap lookup reads zero and every value is refused", seed.path)
		assert.Positivef(t, field.maxBytes, "create seed %q must carry a positive byte cap", seed.path)
	}
}
