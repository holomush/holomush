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
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/world"
	adminportalv1 "github.com/holomush/holomush/pkg/proto/holomush/adminportal/v1"
)

// spec106WritablePaths is 01-SPEC §10.6's checked-in list of the THIRTEEN paths
// an admin may write, in the order the SPEC lists them.
//
// It is a literal here on purpose: the point of the set-equality test below is
// that the shipped map and the SPEC agree, and deriving one from the other would
// make the assertion tautological. §10.6 (01-SPEC:2546-2560) is the source of
// truth; if this list and that section ever disagree, the SPEC wins and the map
// follows.
//
// THIRTEEN, and only ONE of them lacks the `profile.` prefix. Never assert this
// by counting that prefix — a correct implementation yields TWELVE such matches,
// so a count of thirteen is unsatisfiable and the only ways to "fix" it are to
// invent a thirteenth profile path or to delete `description`.
var spec106WritablePaths = []string{
	"description",
	"profile.pronouns",
	"profile.concept",
	"profile.species",
	"profile.age",
	"profile.faction",
	"profile.appearance",
	"profile.personality",
	"profile.biography",
	"profile.rumors",
	"profile.currently",
	"profile.rp_preferences",
	"profile.timezone",
}

// TestAdminProfileMaskAllowlistMatchesSpec is the ALLOWLIST half of §10.6's
// designated pair of durable verifications (the other is
// TestAdminCharacterMessagesCarryNoRoleBearingField in test/meta, which walks the
// generated proto descriptors because a source grep cannot see a field added to a
// generated message).
//
// It asserts set equality in BOTH directions, because the two failure modes are
// different bugs: a MISSING path is a silent ADMIN-04 scope loss (the operator's
// edit to that field is refused), and an EXTRA path is an unadvertised mutation
// a direct gRPC caller can drive.
func TestAdminProfileMaskAllowlistMatchesSpec(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, spec106WritablePaths,
		"an empty checked-in list would satisfy the MISSING direction vacuously")
	require.NotEmpty(t, adminProfileMaskablePaths,
		"an empty allowlist would satisfy the EXTRA direction vacuously")

	spec := make(map[string]struct{}, len(spec106WritablePaths))
	for _, p := range spec106WritablePaths {
		spec[p] = struct{}{}
	}
	require.Len(t, spec, len(spec106WritablePaths), "the checked-in list carries no duplicate")

	var missing, extra []string
	for p := range spec {
		if _, ok := adminProfileMaskablePaths[p]; !ok {
			missing = append(missing, p)
		}
	}
	for p := range adminProfileMaskablePaths {
		if _, ok := spec[p]; !ok {
			extra = append(extra, p)
		}
	}

	assert.Empty(t, missing,
		"01-SPEC §10.6 names a writable path the allowlist does not carry — the admin edit for that "+
			"field is silently refused, which is a real ADMIN-04 scope loss")
	assert.Empty(t, extra,
		"the allowlist carries a path §10.6 does not name — an unadvertised mutation a direct gRPC "+
			"caller can drive through the mask")

	// Exactly one entry is the in-world description COLUMN; the other twelve are
	// entity_properties rows. That asymmetry is why the writable set is thirteen
	// while the character-access facade's sibling is twelve.
	descriptions := 0
	for path, field := range adminProfileMaskablePaths {
		require.NotNil(t, field.value, "%s: every path resolves to a request accessor", path)
		assert.Contains(t, []int{world.MaxNameLength, world.MaxDescriptionLength}, field.maxBytes,
			"%s: caps reuse the shipped world constants rather than minting new numbers", path)
		if field.isDescription {
			descriptions++
			assert.Equal(t, "description", path)
			continue
		}
		assert.True(t, strings.HasPrefix(path, "profile."),
			"%s: every non-description path is a §7.2 property name verbatim", path)
	}
	assert.Equal(t, 1, descriptions, "exactly one entry is the characters.description COLUMN")
}

// --- The handler harness ---

// recordingAdminCharacterWriter records what reached the domain, and which
// options came with it.
type recordingAdminCharacterWriter struct {
	t *testing.T

	profileCalls  []recordedAdminProfileWrite
	retireCalls   []recordedAdminLifecycle
	unretireCalls []recordedAdminLifecycle

	profileErr  error
	retireErr   error
	unretireErr error
}

type recordedAdminProfileWrite struct {
	caller          world.Caller
	characterID     ulid.ULID
	expectedVersion int
	attrs           map[string]string
	optCount        int
}

type recordedAdminLifecycle struct {
	caller          world.Caller
	characterID     ulid.ULID
	expectedVersion int
	optCount        int
}

func (w *recordingAdminCharacterWriter) UpdateCharacterProfileAttributes(
	_ context.Context, caller world.Caller, characterID ulid.ULID, expectedVersion int,
	attributes map[string]string, opts ...world.ProfileUpdateOption,
) error {
	w.t.Helper()
	copied := make(map[string]string, len(attributes))
	for k, v := range attributes {
		copied[k] = v
	}
	w.profileCalls = append(w.profileCalls, recordedAdminProfileWrite{
		caller: caller, characterID: characterID, expectedVersion: expectedVersion,
		attrs: copied, optCount: len(opts),
	})
	return w.profileErr
}

func (w *recordingAdminCharacterWriter) RetireCharacter(
	_ context.Context, caller world.Caller, characterID ulid.ULID, expectedVersion int, opts ...world.LifecycleOption,
) error {
	w.t.Helper()
	w.retireCalls = append(w.retireCalls, recordedAdminLifecycle{
		caller: caller, characterID: characterID, expectedVersion: expectedVersion, optCount: len(opts),
	})
	return w.retireErr
}

func (w *recordingAdminCharacterWriter) UnretireCharacter(
	_ context.Context, caller world.Caller, characterID ulid.ULID, expectedVersion int, opts ...world.LifecycleOption,
) error {
	w.t.Helper()
	w.unretireCalls = append(w.unretireCalls, recordedAdminLifecycle{
		caller: caller, characterID: characterID, expectedVersion: expectedVersion, optCount: len(opts),
	})
	return w.unretireErr
}

// adminWriteHarness stands the handler up over recording fakes plus a context
// carrying a resolved admin player — the shape the interceptor leaves behind.
type adminWriteHarness struct {
	srv      *AdminPortalServer
	reader   *fakeAdminCharacterReader
	writer   *recordingAdminCharacterWriter
	ctx      context.Context //nolint:containedctx // a test fixture's prepared request context
	charID   ulid.ULID
	playerID ulid.ULID
	version  int
}

func newAdminWriteHarness(t *testing.T) *adminWriteHarness {
	t.Helper()
	charID := ulid.Make()
	playerID := ulid.Make()
	reader := &fakeAdminCharacterReader{
		row: world.AdminCharacterRow{
			Character: &world.Character{
				ID: charID, PlayerID: ulid.Make(), Name: "Alice",
				Status: world.StatusActive, Version: 5, Description: "a tall figure",
			},
			PlayerUsername: "owner",
		},
	}
	writer := &recordingAdminCharacterWriter{t: t}
	srv := NewAdminPortalServer(
		seedEngineFor(t, ulid.Make()),
		WithAdminCharacterReader(reader),
		WithAdminCharacterWriter(writer),
	)
	ctx := context.WithValue(context.Background(), adminPlayerContextKey{},
		&auth.PlayerSession{ID: ulid.Make(), PlayerID: playerID})
	return &adminWriteHarness{
		srv: srv, reader: reader, writer: writer, ctx: ctx,
		charID: charID, playerID: playerID, version: 5,
	}
}

func (h *adminWriteHarness) updateRequest(paths ...string) *adminportalv1.AdminUpdateCharacterRequest {
	return &adminportalv1.AdminUpdateCharacterRequest{
		PlayerSessionToken: "token",
		CharacterId:        h.charID.String(),
		ExpectedVersion:    int32(h.version), //nolint:gosec // a small fixture version
		UpdateMask:         &fieldmaskpb.FieldMask{Paths: paths},
	}
}

// TestAdminUpdateCharacterAppliesAnAllowlistedPathAndReturnsTheNewVersion is
// behavior 1: a mask naming one of the thirteen applies it and the response
// carries the post-write row.
func TestAdminUpdateCharacterAppliesAnAllowlistedPathAndReturnsTheNewVersion(t *testing.T) {
	t.Parallel()
	h := newAdminWriteHarness(t)

	req := h.updateRequest("profile.biography")
	req.Biography = "Born under a red sky."
	// The read-back returns the BUMPED version, exactly as the repository would.
	h.reader.row.Version = h.version + 1

	resp, err := h.srv.AdminUpdateCharacter(h.ctx, req)
	require.NoError(t, err)
	require.Len(t, h.writer.profileCalls, 1, "ONE request is ONE domain write")

	got := h.writer.profileCalls[0]
	assert.Equal(t, map[string]string{"profile.biography": "Born under a red sky."}, got.attrs)
	assert.Equal(t, h.version, got.expectedVersion, "the caller's version is threaded STRAIGHT through")
	assert.Equal(t, world.HumanCaller(access.PlayerSubject(h.playerID.String())), got.caller,
		"D-104: the domain caller is PLAYER-flavoured, so the envelope Actor is player:<id>")
	assert.Equal(t, h.version+1, int(resp.GetCharacter().GetVersion()),
		"the response carries the NEW version the client sends as its next expected_version")
}

// TestAdminUpdateCharacterRejectsEveryPathOutsideTheThirteen is behavior 2, and
// the four named paths are the ones §10.6 excludes by name.
func TestAdminUpdateCharacterRejectsEveryPathOutsideTheThirteen(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"roles", "name", "status", "version", "player_id"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			h := newAdminWriteHarness(t)
			_, err := h.srv.AdminUpdateCharacter(h.ctx, h.updateRequest(path))
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err),
				"an unlisted path is REJECTED, never silently ignored (§9.5 rule 2)")
			assert.Empty(t, h.writer.profileCalls, "no domain write is reached")
		})
	}
}

// TestAdminUpdateCharacterMatchesMaskPathsByExactStringOnly is the BEHAVIOURAL
// proof that matching is exact-string map access.
//
// A prefix, glob or Contains implementation accepts at least one of these three.
// It replaces an earlier grep-shaped criterion that could not fail: `\*` matches
// every Go pointer, and this handler is full of `*adminportalv1.…Request`.
func TestAdminUpdateCharacterMatchesMaskPathsByExactStringOnly(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"profile.",           // a bare container prefix
		"profile.*",          // a glob
		"profile.biograph",   // a proper prefix of a real path
		"Profile.Biography",  // a case fold
		" profile.biography", // untrimmed
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			h := newAdminWriteHarness(t)
			_, err := h.srv.AdminUpdateCharacter(h.ctx, h.updateRequest(path))
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
			assert.Empty(t, h.writer.profileCalls)
		})
	}
}

// TestAdminUpdateCharacterTreatsAnEmptyMaskAsANoOpSuccess is behavior 3, the
// normative §9.5 rule 4 contract (01-SPEC:2197, restated for admin character
// editing at :2534).
func TestAdminUpdateCharacterTreatsAnEmptyMaskAsANoOpSuccess(t *testing.T) {
	t.Parallel()
	h := newAdminWriteHarness(t)

	resp, err := h.srv.AdminUpdateCharacter(h.ctx, h.updateRequest())
	require.NoError(t, err, "an empty mask changes nothing and returns SUCCESS, never InvalidArgument")
	assert.Equal(t, h.version, int(resp.GetCharacter().GetVersion()),
		"the returned version equals the PRE-call version: nothing was bumped")
	assert.Empty(t, h.writer.profileCalls,
		"no domain write is reached, so no envelope and no audit row can exist")
}

// TestAdminUpdateCharacterRunsTheGuardsBeforeTheNoOpAnswer is behavior 3b: the
// no-op comes AFTER the guards, not instead of them.
//
// A handler that returned early before the guards would pass the test above and
// fail this one.
func TestAdminUpdateCharacterRunsTheGuardsBeforeTheNoOpAnswer(t *testing.T) {
	t.Parallel()

	t.Run("a nonexistent character is NotFound even with an empty mask", func(t *testing.T) {
		t.Parallel()
		h := newAdminWriteHarness(t)
		h.reader.rowErr = oops.Code("CHARACTER_NOT_FOUND").Wrap(world.ErrNotFound)

		_, err := h.srv.AdminUpdateCharacter(h.ctx, h.updateRequest())
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("a zero expected_version is InvalidArgument even with an empty mask", func(t *testing.T) {
		t.Parallel()
		h := newAdminWriteHarness(t)
		req := h.updateRequest()
		req.ExpectedVersion = 0

		_, err := h.srv.AdminUpdateCharacter(h.ctx, req)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Equal(t, 0, h.reader.getCalls,
			"the version guard is a caller fault diagnosable without a read, so it runs FIRST")
	})
}

// TestAdminUpdateCharacterRefusesAnUnguardedWrite is behavior 4: absent and
// explicit zero take the same branch, and neither is a guard-free write.
func TestAdminUpdateCharacterRefusesAnUnguardedWrite(t *testing.T) {
	t.Parallel()

	for _, v := range []int32{0, -1} {
		h := newAdminWriteHarness(t)
		req := h.updateRequest("profile.biography")
		req.ExpectedVersion = v

		_, err := h.srv.AdminUpdateCharacter(h.ctx, req)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Empty(t, h.writer.profileCalls, "expected_version %d MUST NOT reach the domain", v)
	}
}

// TestAdminCharacterWritesSurfaceAStaleVersionAsAborted is behavior 5, across
// all three RPCs — the loser of two concurrent edits is SURFACED, never retried:
// a retry loop here would reintroduce last-write-wins one layer above the guard.
func TestAdminCharacterWritesSurfaceAStaleVersionAsAborted(t *testing.T) {
	t.Parallel()

	stale := oops.Code(world.CodeConcurrentEdit).Wrap(world.ErrConcurrentEdit)

	t.Run("update", func(t *testing.T) {
		t.Parallel()
		h := newAdminWriteHarness(t)
		h.writer.profileErr = stale
		_, err := h.srv.AdminUpdateCharacter(h.ctx, h.updateRequest("profile.biography"))
		require.Error(t, err)
		assert.Equal(t, codes.Aborted, status.Code(err))
	})

	t.Run("retire", func(t *testing.T) {
		t.Parallel()
		h := newAdminWriteHarness(t)
		h.writer.retireErr = stale
		_, err := h.srv.AdminRetireCharacter(h.ctx, &adminportalv1.AdminRetireCharacterRequest{
			PlayerSessionToken: "t", CharacterId: h.charID.String(), ExpectedVersion: int32(h.version), //nolint:gosec // a small fixture version
		})
		require.Error(t, err)
		assert.Equal(t, codes.Aborted, status.Code(err))
	})

	t.Run("unretire", func(t *testing.T) {
		t.Parallel()
		h := newAdminWriteHarness(t)
		h.writer.unretireErr = stale
		_, err := h.srv.AdminUnretireCharacter(h.ctx, &adminportalv1.AdminUnretireCharacterRequest{
			PlayerSessionToken: "t", CharacterId: h.charID.String(), ExpectedVersion: int32(h.version), //nolint:gosec // a small fixture version
		})
		require.Error(t, err)
		assert.Equal(t, codes.Aborted, status.Code(err))
	})
}

// TestAdminUpdateCharacterEnforcesBothByteCapsAtTheirOwnBoundaries is behavior 6.
//
// TWO caps, not one. The seven short fields cap at world.MaxNameLength (100) and
// the five long ones at world.MaxDescriptionLength (4000). Asking a 4000-byte
// field to reject 101 bytes would assert behaviour a correct server refuses to
// exhibit — which is why each field is tested against ITS OWN triple.
func TestAdminUpdateCharacterEnforcesBothByteCapsAtTheirOwnBoundaries(t *testing.T) {
	t.Parallel()

	// A three-byte CJK rune: 34 of them are 102 bytes but only 34 runes, and
	// 1334 are 4002 bytes but only 1334 runes. A rune-measured cap would accept
	// both; a byte-measured one refuses both at the same boundary an ASCII value
	// of the same byte length is refused.
	const cjk = "字"
	require.Len(t, cjk, 3, "the CJK fixture must be multi-byte or the rune/byte cases coincide")

	cases := []struct {
		name     string
		path     string
		set      func(*adminportalv1.AdminUpdateCharacterRequest, string)
		cap      int
		overCJK  string
		underCJK string
	}{
		{
			name: "short field caps at world.MaxNameLength",
			path: "profile.pronouns",
			set:  func(r *adminportalv1.AdminUpdateCharacterRequest, v string) { r.Pronouns = v },
			cap:  world.MaxNameLength,
			// 34 runes = 102 bytes: over the 100-byte cap while 34 runes is not.
			overCJK: strings.Repeat(cjk, 34),
			// 33 runes = 99 bytes: under.
			underCJK: strings.Repeat(cjk, 33),
		},
		{
			name: "long field caps at world.MaxDescriptionLength",
			path: "profile.biography",
			set:  func(r *adminportalv1.AdminUpdateCharacterRequest, v string) { r.Biography = v },
			cap:  world.MaxDescriptionLength,
			// 1334 runes = 4002 bytes: over the 4000-byte cap.
			overCJK: strings.Repeat(cjk, 1334),
			// 1333 runes = 3999 bytes: under.
			underCJK: strings.Repeat(cjk, 1333),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, sub := range []struct {
				label    string
				value    string
				accepted bool
			}{
				{"one byte under the cap", strings.Repeat("a", tc.cap-1), true},
				{"exactly at the cap", strings.Repeat("a", tc.cap), true},
				{"one byte over the cap", strings.Repeat("a", tc.cap+1), false},
				{"CJK under the cap in BYTES", tc.underCJK, true},
				{"CJK over the cap in BYTES but not in runes", tc.overCJK, false},
			} {
				t.Run(sub.label, func(t *testing.T) {
					t.Parallel()
					h := newAdminWriteHarness(t)
					req := h.updateRequest(tc.path)
					tc.set(req, sub.value)

					_, err := h.srv.AdminUpdateCharacter(h.ctx, req)
					if sub.accepted {
						require.NoError(t, err)
						require.Len(t, h.writer.profileCalls, 1)
						return
					}
					require.Error(t, err)
					assert.Equal(t, codes.InvalidArgument, status.Code(err))
					assert.Empty(t, h.writer.profileCalls,
						"a value over its own byte cap MUST NOT reach the domain")
				})
			}
		})
	}
}

// TestAdminUpdateCharacterServesAMixedMaskAsOneDomainWrite is behavior 6b, the
// case that previously had no implementation at all.
//
// A mask naming `description` AND a `profile.*` path, under ONE
// expected_version, is ONE call. Implementing it as two domain calls fails here
// on the call count, and would fail the integration proof on the version
// increment, the envelope count and the prose-absence assertion at once.
func TestAdminUpdateCharacterServesAMixedMaskAsOneDomainWrite(t *testing.T) {
	t.Parallel()
	h := newAdminWriteHarness(t)

	req := h.updateRequest("description", "profile.biography")
	req.Description = "a short figure"
	req.Biography = "Born under a green sky."

	_, err := h.srv.AdminUpdateCharacter(h.ctx, req)
	require.NoError(t, err)
	require.Len(t, h.writer.profileCalls, 1,
		"a mixed mask is ONE domain write; two calls would consume the single expected_version twice")

	got := h.writer.profileCalls[0]
	assert.Equal(t, map[string]string{"profile.biography": "Born under a green sky."}, got.attrs,
		"`description` MUST NOT enter attributes — the domain's closed §7.2 name set rejects it")
	assert.Equal(t, 3, got.optCount,
		"the audit context, the skip-unchanged option and the description option all travel together")
}

// TestAdminUpdateCharacterAlwaysSuppliesTheAdminOnlyOptions pins the two options
// that are supplied on EVERY update, description or not.
func TestAdminUpdateCharacterAlwaysSuppliesTheAdminOnlyOptions(t *testing.T) {
	t.Parallel()
	h := newAdminWriteHarness(t)

	req := h.updateRequest("profile.biography")
	req.Biography = "prose"

	_, err := h.srv.AdminUpdateCharacter(h.ctx, req)
	require.NoError(t, err)
	require.Len(t, h.writer.profileCalls, 1)
	assert.Equal(t, 2, h.writer.profileCalls[0].optCount,
		"the audit context and the admin-only skip-unchanged option, with no description supplied")
}

// TestAdminRetireCharacterReachesTheCanonicalLifecycleCommand is behavior 7.
func TestAdminRetireCharacterReachesTheCanonicalLifecycleCommand(t *testing.T) {
	t.Parallel()
	h := newAdminWriteHarness(t)
	h.reader.row.Status = world.StatusRetired
	h.reader.row.Version = h.version + 1

	resp, err := h.srv.AdminRetireCharacter(h.ctx, &adminportalv1.AdminRetireCharacterRequest{
		PlayerSessionToken: "t", CharacterId: h.charID.String(), ExpectedVersion: int32(h.version), //nolint:gosec // a small fixture version
	})
	require.NoError(t, err)
	require.Len(t, h.writer.retireCalls, 1,
		"the transition goes through world.Service.RetireCharacter, never a repository or an admin-only path")
	assert.Equal(t, h.charID, h.writer.retireCalls[0].characterID)
	assert.Equal(t, h.version, h.writer.retireCalls[0].expectedVersion)
	assert.Equal(t, 1, h.writer.retireCalls[0].optCount, "the evaluated audit context travels with it")
	assert.Equal(t, world.HumanCaller(access.PlayerSubject(h.playerID.String())), h.writer.retireCalls[0].caller)
	assert.Equal(t, string(world.StatusRetired), resp.GetCharacter().GetStatus())
}

// TestAdminRetireOfAnAlreadyRetiredCharacterIsRefusedByTheLifecycleGuard is the
// second half of behavior 7: idempotency comes from the shipped guard, not from a
// handler check, so exactly one envelope can ever exist for one retirement.
func TestAdminRetireOfAnAlreadyRetiredCharacterIsRefusedByTheLifecycleGuard(t *testing.T) {
	t.Parallel()
	h := newAdminWriteHarness(t)
	h.writer.retireErr = oops.Code("CHARACTER_ALREADY_RETIRED").Errorf("already retired")

	_, err := h.srv.AdminRetireCharacter(h.ctx, &adminportalv1.AdminRetireCharacterRequest{
		PlayerSessionToken: "t", CharacterId: h.charID.String(), ExpectedVersion: int32(h.version), //nolint:gosec // a small fixture version
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// TestAdminUnretireCharacterReachesTheCanonicalLifecycleCommand is behavior 8.
func TestAdminUnretireCharacterReachesTheCanonicalLifecycleCommand(t *testing.T) {
	t.Parallel()
	h := newAdminWriteHarness(t)
	h.reader.row.Status = world.StatusActive
	h.reader.row.Version = h.version + 1

	resp, err := h.srv.AdminUnretireCharacter(h.ctx, &adminportalv1.AdminUnretireCharacterRequest{
		PlayerSessionToken: "t", CharacterId: h.charID.String(), ExpectedVersion: int32(h.version), //nolint:gosec // a small fixture version
	})
	require.NoError(t, err)
	require.Len(t, h.writer.unretireCalls, 1)
	assert.Equal(t, 1, h.writer.unretireCalls[0].optCount)
	assert.Equal(t, string(world.StatusActive), resp.GetCharacter().GetStatus())
}

// TestAdminCharacterWritesRefuseAnUnwiredServerRatherThanSucceedingSilently pins
// the nil-writer branch: codes.Internal, never a silent success.
//
// A silent success would tell an operator they changed a character they did not,
// which on an audited surface is worse than an error.
func TestAdminCharacterWritesRefuseAnUnwiredServerRatherThanSucceedingSilently(t *testing.T) {
	t.Parallel()

	charID := ulid.Make()
	reader := &fakeAdminCharacterReader{row: world.AdminCharacterRow{
		Character: &world.Character{ID: charID, Name: "Alice", Status: world.StatusActive, Version: 3},
	}}
	srv := NewAdminPortalServer(seedEngineFor(t, ulid.Make()), WithAdminCharacterReader(reader))
	ctx := context.WithValue(context.Background(), adminPlayerContextKey{},
		&auth.PlayerSession{ID: ulid.Make(), PlayerID: ulid.Make()})

	_, err := srv.AdminUpdateCharacter(ctx, &adminportalv1.AdminUpdateCharacterRequest{
		PlayerSessionToken: "t", CharacterId: charID.String(), ExpectedVersion: 3,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"profile.biography"}},
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))

	_, err = srv.AdminRetireCharacter(ctx, &adminportalv1.AdminRetireCharacterRequest{
		PlayerSessionToken: "t", CharacterId: charID.String(), ExpectedVersion: 3,
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))

	_, err = srv.AdminUnretireCharacter(ctx, &adminportalv1.AdminUnretireCharacterRequest{
		PlayerSessionToken: "t", CharacterId: charID.String(), ExpectedVersion: 3,
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// TestAdminCharacterWritesReportAWorldLayerDenialOpaquely pins that the SECOND
// gate's refusal is indistinguishable on the wire from the section gate's.
//
// If they differed, a caller who passed the section gate could tell "the world
// policy refused me on this character" apart from "you are not an admin" — which
// makes the pair an oracle for which characters a policy covers.
func TestAdminCharacterWritesReportAWorldLayerDenialOpaquely(t *testing.T) {
	t.Parallel()
	h := newAdminWriteHarness(t)
	h.writer.profileErr = oops.Code("CHARACTER_ACCESS_DENIED").Wrap(world.ErrPermissionDenied)

	_, err := h.srv.AdminUpdateCharacter(h.ctx, h.updateRequest("profile.biography"))
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Equal(t, adminDeniedMessage, status.Convert(err).Message(),
		"the world-layer refusal carries the SAME static message the section gate does")
}
