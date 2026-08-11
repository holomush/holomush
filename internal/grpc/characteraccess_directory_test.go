// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/access/profilevis"
	"github.com/holomush/holomush/internal/auth"
	authmocks "github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/testsupport/abactest"
	"github.com/holomush/holomush/internal/world"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
)

// This file drives the two-layer directory decision 01-SPEC §9.2 and §8.7
// describe, and it keeps the two layers SEPARABLE at every point:
//
//   - the §9.2 GATE is one ABAC decision on character_directory:all, made
//     through characterAccessPolicyEvaluator, BEFORE anything is enumerated;
//   - the §8.7 MEMBERSHIP rule is profilevis.Reachable per character, applied
//     AFTER the gate to the rows the enumeration returned.
//
// The specs below deny at one layer while the other permits, in both
// directions. That is the property the substituted design (gating on
// per-character reachability) could not express at all: with one mechanism doing
// both jobs, "the directory is closed" and "this character is not listed" are
// the same sentence.

const directoryTestToken = "character-directory-session-token"

// recordingDirectoryReader is the double behind characterAccessDirectoryReader.
// It COUNTS calls, because "the gate precedes enumeration" is a claim about
// whether ListAll ran at all — a claim no assertion about the response body can
// make. A denied call that still enumerated would return the same empty list.
type recordingDirectoryReader struct {
	chars     []*world.Character
	listErr   error
	listCalls int
}

func (r *recordingDirectoryReader) ListAll(context.Context) ([]*world.Character, error) {
	r.listCalls++
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.chars, nil
}

// failOnCallDirectoryWorldReader fails the test if EITHER world read is
// reached. The directory publishes IDENTITY ONLY: it neither enumerates
// property rows nor reads characters.description, so both methods of the
// facade's read seam are unreachable on this path and the doubles say so
// continuously rather than in one spec.
type failOnCallDirectoryWorldReader struct{ t *testing.T }

func (f *failOnCallDirectoryWorldReader) ListPropertiesByParent(context.Context, world.Caller, string, ulid.ULID) ([]*world.EntityProperty, error) {
	f.t.Helper()
	f.t.Fatal("the directory MUST NOT enumerate property rows: ListPropertiesByParent was called")
	return nil, nil
}

func (f *failOnCallDirectoryWorldReader) GetCharacterDescription(context.Context, world.Caller, ulid.ULID) (world.CharacterDescription, error) {
	f.t.Helper()
	f.t.Fatal("the directory MUST NOT read the in-world description: GetCharacterDescription was called")
	return world.CharacterDescription{}, nil
}

// denyingGate is an ORDINARY policy denial: a DENY decision with a non-infra
// policy id and a nil error. It stands for a viewer whose rung is outside
// seed:viewer-directory-list-characters' clearing set — a rung the SHIPPED
// corpus has none of, because that seed clears all three, so the branch is
// exercised here rather than by contorting the corpus.
type denyingGate struct{ calls int }

func (g *denyingGate) Evaluate(context.Context, types.AccessRequest) (types.Decision, error) {
	g.calls++
	return types.NewDecision(types.EffectDefaultDeny, "viewer tier is outside the directory clearing set", "seed:viewer-directory-list-characters"), nil
}

// erroringGate is the obvious evaluation failure: a non-nil error from the
// engine. A failure to EVALUATE the gate is neither a permit nor a deny.
type erroringGate struct{ calls int }

func (g *erroringGate) Evaluate(context.Context, types.AccessRequest) (types.Decision, error) {
	g.calls++
	return types.Decision{}, oops.Code("POLICY_STORE_UNREACHABLE").Errorf("the policy store is unreachable")
}

// selectiveReachability denies profile reachability for a named set of
// character ids and permits every other. It doubles the MEMBERSHIP layer only:
// VisibleAttributes is unreachable from the directory handler, so reaching it
// fails the test rather than returning a plausible empty map.
type selectiveReachability struct {
	t           *testing.T
	unreachable map[string]bool
	err         error
	calls       int
}

func (s *selectiveReachability) Reachable(_ context.Context, _, characterID string) (bool, error) {
	s.calls++
	if s.err != nil {
		return false, s.err
	}
	return !s.unreachable[characterID], nil
}

func (s *selectiveReachability) VisibleAttributes(context.Context, string, string, []profilevis.Property) (map[string]profilevis.Property, error) {
	s.t.Helper()
	s.t.Fatal("the directory MUST NOT evaluate per-attribute visibility: VisibleAttributes was called")
	return nil, nil
}

// failOnCallDirectoryGate and failOnCallDirectoryReader are the doubles every
// NON-directory spec in this package passes. The directory listing is the ONLY
// path in this facade that evaluates a raw ABAC decision of its own or performs
// a bulk character enumeration, so both properties are enforced continuously by
// every profile, owner, viewer and write spec rather than by one test each.
type failOnCallDirectoryGate struct{ t *testing.T }

func (f *failOnCallDirectoryGate) Evaluate(context.Context, types.AccessRequest) (types.Decision, error) {
	f.t.Helper()
	f.t.Fatal("only the directory listing makes a raw ABAC decision in this facade: Evaluate was called")
	return types.Decision{}, nil
}

type failOnCallDirectoryReader struct{ t *testing.T }

func (f *failOnCallDirectoryReader) ListAll(context.Context) ([]*world.Character, error) {
	f.t.Helper()
	f.t.Fatal("only the directory listing enumerates every character: ListAll was called")
	return nil, nil
}

// directoryFixture is one wired facade. A nil gate or a nil visibility means
// "use the REAL seeded corpus for that layer", so a spec that doubles one layer
// is visibly doubling exactly one.
type directoryFixture struct {
	tier       string
	chars      []*world.Character
	gate       characterAccessPolicyEvaluator
	visibility characterAccessProfileVisibility
	listErr    error
}

type directoryHarness struct {
	srv    *CharacterAccessServer
	reader *recordingDirectoryReader
	token  string
}

func (h *directoryHarness) list(t *testing.T) (*characteraccessv1.ListCharacterDirectoryResponse, error) {
	t.Helper()
	return h.srv.ListCharacterDirectory(context.Background(), &characteraccessv1.ListCharacterDirectoryRequest{
		PlayerSessionToken: h.token,
	})
}

func newDirectoryHarness(t *testing.T, f directoryFixture) *directoryHarness {
	t.Helper()

	playerID := idgen.New()

	viewerPlayerID := ""
	token := ""
	if f.tier != access.ViewerTierAnonymous {
		viewerPlayerID = playerID.String()
		token = directoryTestToken
	}

	engine := abactest.NewSeedEngine(t,
		abactest.ViewerProvider(abactest.Viewer{Tier: f.tier, PlayerID: viewerPlayerID}))

	gate := f.gate
	if gate == nil {
		gate = engine
	}
	visibility := f.visibility
	if visibility == nil {
		visibility = &profilevis.Evaluator{Engine: engine}
	}

	reader := &recordingDirectoryReader{chars: f.chars, listErr: f.listErr}
	sessionRepo, playerRepo := directoryAuthRepos(t, playerID, f.tier)

	return &directoryHarness{
		srv: NewCharacterAccessServer(
			&failOnCallDirectoryWorldReader{t: t},
			&recordingWorldMutator{t: t, failOnCall: true},
			visibility,
			gate,
			reader,
			sessionRepo,
			playerRepo,
			authmocks.NewMockCharacterRepository(t),
		),
		reader: reader,
		token:  token,
	}
}

func directoryAuthRepos(t *testing.T, playerID ulid.ULID, tier string) (auth.PlayerSessionRepository, auth.PlayerRepository) {
	t.Helper()

	ps := &auth.PlayerSession{ID: idgen.New(), PlayerID: playerID, TokenHash: auth.HashSessionToken(directoryTestToken)}
	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, ps.TokenHash).Return(ps, nil).Maybe()
	sessionRepo.EXPECT().RefreshTTL(mock.Anything, ps.ID, auth.PlayerSessionTTL).Return(nil).Maybe()

	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).
		Return(&auth.Player{ID: playerID, IsGuest: tier == access.ViewerTierGuest}, nil).Maybe()

	return sessionRepo, playerRepo
}

// directoryChar is the fixture shape ListAll returns: id and name only, which
// is all auth.CharacterRepository.ListAll's own contract promises.
func directoryChar(name string) *world.Character {
	return &world.Character{ID: idgen.New(), Name: name}
}

// --- The §9.2 gate ---

// TestListCharacterDirectoryDeniesABelowFloorViewerBeforeEnumerating is the
// gate itself, and the ListAll call count is the whole assertion. A denial that
// still enumerated would produce an identical response — so a spec that only
// inspected the body could not tell the gate from a post-hoc filter.
func TestListCharacterDirectoryDeniesABelowFloorViewerBeforeEnumerating(t *testing.T) {
	t.Parallel()

	gate := &denyingGate{}
	h := newDirectoryHarness(t, directoryFixture{
		tier:  access.ViewerTierAnonymous,
		chars: []*world.Character{directoryChar("Ada"), directoryChar("Bram")},
		gate:  gate,
	})

	resp, err := h.list(t)
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Equal(t, 1, gate.calls, "the gate is exactly ONE decision")
	assert.Equal(t, 0, h.reader.listCalls,
		"a denied viewer MUST learn nothing about the corpus — not even its size")
}

// TestListCharacterDirectoryReachesEnumerationAtAClearingRung is the paired
// positive control for the denial above: the SAME fixture, the same handler,
// the real seeded floor. Without it the denial cannot be told from an empty
// corpus or a broken harness.
func TestListCharacterDirectoryReachesEnumerationAtAClearingRung(t *testing.T) {
	t.Parallel()

	h := newDirectoryHarness(t, directoryFixture{
		tier:  access.ViewerTierAnonymous,
		chars: []*world.Character{directoryChar("Ada"), directoryChar("Bram")},
	})

	resp, err := h.list(t)
	require.NoError(t, err)
	assert.Len(t, resp.GetCharacters(), 2)
	assert.Equal(t, 1, h.reader.listCalls)
}

// TestListCharacterDirectoryGateIsIndependentOfReachability is the property the
// collapsed design could not express: the gate DENIES while reachability would
// have permitted every character.
//
// A game may therefore close the directory without touching profile
// reachability, and the two clearing sets are two independent configuration
// surfaces rather than one.
func TestListCharacterDirectoryGateIsIndependentOfReachability(t *testing.T) {
	t.Parallel()

	chars := []*world.Character{directoryChar("Ada"), directoryChar("Bram")}
	reach := &selectiveReachability{t: t} // permits everything

	h := newDirectoryHarness(t, directoryFixture{
		tier:       access.ViewerTierAnonymous,
		chars:      chars,
		gate:       &denyingGate{},
		visibility: reach,
	})

	resp, err := h.list(t)
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Zero(t, reach.calls,
		"the membership rule is layered ON the gate: a closed gate never reaches it")
	assert.Zero(t, h.reader.listCalls)
}

// --- The §8.7 membership rule ---

// TestListCharacterDirectoryReturnsEveryReachableCharacterAsIdentityOnly drives
// the real seeded corpus at the anonymous rung.
func TestListCharacterDirectoryReturnsEveryReachableCharacterAsIdentityOnly(t *testing.T) {
	t.Parallel()

	ada := directoryChar("Ada")
	bram := directoryChar("Bram")
	h := newDirectoryHarness(t, directoryFixture{
		tier:  access.ViewerTierAnonymous,
		chars: []*world.Character{ada, bram},
	})

	resp, err := h.list(t)
	require.NoError(t, err)
	require.Len(t, resp.GetCharacters(), 2)

	byID := make(map[string]string, 2)
	for _, row := range resp.GetCharacters() {
		byID[row.GetId()] = row.GetName()
	}
	assert.Equal(t, "Ada", byID[ada.ID.String()])
	assert.Equal(t, "Bram", byID[bram.ID.String()])
}

// TestListCharacterDirectoryOmitsAnUnreachableCharacterIndistinguishably is the
// §8.7 rule, asserted the only way that actually proves it: by comparing the
// WHOLE response for a viewer who cannot reach a character against the whole
// response for a corpus in which that character never existed.
//
// A spec that merely asserted "Bram is absent" would pass against a response
// carrying a tombstone, a null entry or a count the client could subtract.
func TestListCharacterDirectoryOmitsAnUnreachableCharacterIndistinguishably(t *testing.T) {
	t.Parallel()

	ada := directoryChar("Ada")
	bram := directoryChar("Bram")

	withheld := newDirectoryHarness(t, directoryFixture{
		tier:       access.ViewerTierAnonymous,
		chars:      []*world.Character{ada, bram},
		visibility: &selectiveReachability{t: t, unreachable: map[string]bool{bram.ID.String(): true}},
	})
	withheldResp, err := withheld.list(t)
	require.NoError(t, err)

	absent := newDirectoryHarness(t, directoryFixture{
		tier:       access.ViewerTierAnonymous,
		chars:      []*world.Character{ada},
		visibility: &selectiveReachability{t: t},
	})
	absentResp, err := absent.list(t)
	require.NoError(t, err)

	assert.True(t, proto.Equal(withheldResp, absentResp),
		"a withheld character and a nonexistent one MUST produce the same response")
}

// TestListCharacterDirectoryIncludesTheSameCharacterAtAClearingRung is the
// paired positive control for the absence above: the same character, the same
// fixture, a viewer who CAN reach it.
func TestListCharacterDirectoryIncludesTheSameCharacterAtAClearingRung(t *testing.T) {
	t.Parallel()

	ada := directoryChar("Ada")
	bram := directoryChar("Bram")

	h := newDirectoryHarness(t, directoryFixture{
		tier:       access.ViewerTierAnonymous,
		chars:      []*world.Character{ada, bram},
		visibility: &selectiveReachability{t: t}, // nothing withheld
	})

	resp, err := h.list(t)
	require.NoError(t, err)
	require.Len(t, resp.GetCharacters(), 2)

	// Membership, not position: the listing is sorted by id, and which of two
	// freshly minted ULIDs sorts first is not this spec's subject.
	ids := make([]string, 0, 2)
	for _, row := range resp.GetCharacters() {
		ids = append(ids, row.GetId())
	}
	assert.Contains(t, ids, bram.ID.String(),
		"the character withheld from the previous spec's viewer IS listed for one that can reach it")
	assert.Contains(t, ids, ada.ID.String())
}

// --- Shape, order and outage ---

// TestListCharacterDirectoryReturnsAnEmptyListForAnEmptyDirectory pins that
// "nobody exists" is a success. A not-found here would make an empty game
// indistinguishable from a broken one.
func TestListCharacterDirectoryReturnsAnEmptyListForAnEmptyDirectory(t *testing.T) {
	t.Parallel()

	h := newDirectoryHarness(t, directoryFixture{
		tier:  access.ViewerTierAnonymous,
		chars: nil,
	})

	resp, err := h.list(t)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Empty(t, resp.GetCharacters())
}

// TestListCharacterDirectoryIsDeterministicAcrossIdenticalCalls asserts the
// listing order is a property of the handler rather than of whatever order the
// repository happened to return, by feeding the SAME two calls a fixture whose
// slice order is the reverse of the id order.
func TestListCharacterDirectoryIsDeterministicAcrossIdenticalCalls(t *testing.T) {
	t.Parallel()

	first := directoryChar("Ada")
	second := directoryChar("Bram")
	// Order the fixture by id descending so a handler that simply forwarded the
	// repository's order would emit a different sequence than the sorted one.
	if first.ID.String() < second.ID.String() {
		first, second = second, first
	}

	h := newDirectoryHarness(t, directoryFixture{
		tier:  access.ViewerTierAnonymous,
		chars: []*world.Character{first, second},
	})

	respA, err := h.list(t)
	require.NoError(t, err)
	respB, err := h.list(t)
	require.NoError(t, err)

	require.Len(t, respA.GetCharacters(), 2)
	assert.Less(t, respA.GetCharacters()[0].GetId(), respA.GetCharacters()[1].GetId(),
		"the listing is sorted by id, not forwarded in repository order")

	bytesA, err := proto.Marshal(respA)
	require.NoError(t, err)
	bytesB, err := proto.Marshal(respB)
	require.NoError(t, err)
	assert.Equal(t, bytesA, bytesB,
		"two identical calls against unchanged data MUST marshal to identical bytes")
}

// TestListCharacterDirectoryReportsAnEnumerationFailureAsInternal keeps an
// outage from rendering as a small directory.
func TestListCharacterDirectoryReportsAnEnumerationFailureAsInternal(t *testing.T) {
	t.Parallel()

	h := newDirectoryHarness(t, directoryFixture{
		tier:    access.ViewerTierAnonymous,
		listErr: oops.Code("CHARACTER_LIST_FAILED").Errorf("the character repository is unreachable"),
	})

	resp, err := h.list(t)
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// TestListCharacterDirectoryReportsAReachabilityFailureAsInternal is the
// §8.10 rule applied to the MEMBERSHIP layer: a failure to evaluate one
// character's reachability aborts the whole call rather than shortening the
// list. A silently shortened directory renders as legitimately small.
func TestListCharacterDirectoryReportsAReachabilityFailureAsInternal(t *testing.T) {
	t.Parallel()

	h := newDirectoryHarness(t, directoryFixture{
		tier:  access.ViewerTierAnonymous,
		chars: []*world.Character{directoryChar("Ada"), directoryChar("Bram")},
		visibility: &selectiveReachability{
			t:   t,
			err: oops.Code(profilevis.CodeEvaluationFailed).Errorf("the policy store is unreachable"),
		},
	})

	resp, err := h.list(t)
	require.Error(t, err)
	assert.Nil(t, resp, "a partially populated list is the silently-shortened response §8.10 forbids")
	assert.Equal(t, codes.Internal, status.Code(err))
}

// TestListCharacterDirectoryReportsAGateEvaluationFailureAsInternal is the
// obvious half of the gate's three-outcome collapse: a non-nil error from the
// engine is neither a permit nor a deny, and it must not fall through into
// enumeration.
func TestListCharacterDirectoryReportsAGateEvaluationFailureAsInternal(t *testing.T) {
	t.Parallel()

	gate := &erroringGate{}
	h := newDirectoryHarness(t, directoryFixture{
		tier:  access.ViewerTierAnonymous,
		chars: []*world.Character{directoryChar("Ada")},
		gate:  gate,
	})

	resp, err := h.list(t)
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Equal(t, 1, gate.calls)
	assert.Equal(t, 0, h.reader.listCalls,
		"a gate that could not be evaluated MUST NOT fall through to enumeration")
}

// TestListCharacterDirectoryTreatsAnInfraFailureDenyAsAnEvaluationFailure is
// the SUBTLE half, and the one branch a hand-written gate reliably gets wrong:
// a DENY decision carrying an `infra:` policy id and a NIL error.
//
// The engine's degraded-mode and session-resolution paths return exactly that
// shape, and collapsing it into an ordinary denial is 01-SPEC §8.10's forbidden
// masking — the caller would be told "you may not list the directory" when in
// fact nothing was evaluated. The double is the SHIPPED
// policytest.NewInfraFailureEngine rather than a fixture invented here: its
// Evaluate has exactly the seam's signature and produces exactly that decision.
func TestListCharacterDirectoryTreatsAnInfraFailureDenyAsAnEvaluationFailure(t *testing.T) {
	t.Parallel()

	h := newDirectoryHarness(t, directoryFixture{
		tier:  access.ViewerTierAnonymous,
		chars: []*world.Character{directoryChar("Ada")},
		gate:  policytest.NewInfraFailureEngine(t, "session resolution failed", "infra:session-resolver"),
	})

	resp, err := h.list(t)
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.Internal, status.Code(err),
		"an infra-failure DENY is an OUTAGE, not a policy answer — rendering it as PermissionDenied is the §8.10 masking")
	assert.Equal(t, 0, h.reader.listCalls)
}
