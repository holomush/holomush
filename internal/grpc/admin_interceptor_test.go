// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/admin/section"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/testsupport/abactest"
	"github.com/holomush/holomush/pkg/errutil"
	adminportalv1 "github.com/holomush/holomush/pkg/proto/holomush/adminportal/v1"
)

// adminListSectionsFullMethod is the wire method name the generated
// ServiceDesc carries. Spelled from the generated constant rather than
// hand-written so a package rename breaks compilation here.
const adminListSectionsFullMethod = adminportalv1.AdminPortalService_AdminListSections_FullMethodName

// countingSessionRepo is an auth.PlayerSessionRepository that COUNTS the
// resolutions it was asked for and fails the test on any other method.
//
// The count is the assertion, not a convenience: "the undeclared-method arm
// runs before subject resolution" is only provable by observing that the
// repository was never reached, and an unreached repository is indistinguishable
// from a reached one unless something counts.
type countingSessionRepo struct {
	auth.PlayerSessionRepository // nil: any method not overridden below panics, which is the point

	t       *testing.T
	calls   int
	session *auth.PlayerSession
	err     error
}

func (r *countingSessionRepo) GetByTokenHash(_ context.Context, _ string) (*auth.PlayerSession, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return r.session, nil
}

func (r *countingSessionRepo) RefreshTTL(_ context.Context, _ ulid.ULID, _ time.Duration) error {
	return nil
}

// sessionRepoFor builds a repo that resolves any token to playerID.
func sessionRepoFor(t *testing.T, playerID ulid.ULID) *countingSessionRepo {
	t.Helper()
	return &countingSessionRepo{
		t: t,
		session: &auth.PlayerSession{
			ID:        adminSessionULID(),
			PlayerID:  playerID,
			TokenHash: "hash",
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}
}

// recordingHandler counts invocations so "the handler never ran" and "the
// handler ran exactly once" are both assertable.
type recordingHandler struct {
	calls int
}

func (h *recordingHandler) handle(_ context.Context, _ any) (any, error) {
	h.calls++
	return &adminportalv1.AdminListSectionsResponse{}, nil
}

func (h *recordingHandler) unary() grpc.UnaryHandler {
	return func(ctx context.Context, req any) (any, error) { return h.handle(ctx, req) }
}

func listSectionsInfo() *grpc.UnaryServerInfo {
	return &grpc.UnaryServerInfo{FullMethod: adminListSectionsFullMethod}
}

// The two player ids are opaque to the abactest player double; distinct values
// only so a failure message names which fixture spoke.
func adminPlayerULID() ulid.ULID    { return ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV") }
func nonAdminPlayerULID() ulid.ULID { return ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FBW") }

// adminSessionULID is the PlayerSession primary key the double hands back. Its
// value decides nothing; only PlayerID is read by the gate.
func adminSessionULID() ulid.ULID { return ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FCX") }

// seedEngineFor builds a REAL engine over the FULL seed corpus for a player
// carrying the given roles. Deliberately not a canned-decision double: a fake
// would make every assertion below a test of the double's opinion rather than
// of seed:admin-section-access.
func seedEngineFor(t *testing.T, id ulid.ULID, roles ...string) section.PolicyEvaluator {
	t.Helper()
	return abactest.NewSeedEngine(t, abactest.PlayerProvider(abactest.Player{
		ID: id.String(), Roles: roles,
	}))
}

// TestTheInterceptorRefusesANonAdminWithTheTypedDenyCode is where the TYPED
// internal code is provable — in-process, where an oops value still exists.
//
// Its wire-level twin (opacity: PermissionDenied plus a static, field-free
// message) lives in test/integration/access/admin_section_gate_test.go. The two
// are deliberately never collapsed: an oops object does NOT survive a gRPC round
// trip, so an oops assertion made over the wire reads something other than what
// it claims.
func TestTheInterceptorRefusesANonAdminWithTheTypedDenyCode(t *testing.T) {
	repo := sessionRepoFor(t, nonAdminPlayerULID())
	handler := &recordingHandler{}

	interceptor := NewAdminSectionInterceptor(AdminInterceptorDeps{
		Engine:      seedEngineFor(t, nonAdminPlayerULID()),
		SessionRepo: repo,
	})

	resp, err := interceptor(t.Context(),
		&adminportalv1.AdminListSectionsRequest{PlayerSessionToken: "raw-token"},
		listSectionsInfo(), handler.unary())

	require.Error(t, err)
	require.Nil(t, resp)
	errutil.AssertErrorCode(t, err, "DENY_ADMIN_SECTION")
	require.Zero(t, handler.calls, "a denied caller MUST NOT reach the handler")

	// Paired positive control: the SAME call, SAME method, by an admin.
	adminRepo := sessionRepoFor(t, adminPlayerULID())
	adminHandler := &recordingHandler{}
	adminInterceptor := NewAdminSectionInterceptor(AdminInterceptorDeps{
		Engine:      seedEngineFor(t, adminPlayerULID(), "admin"),
		SessionRepo: adminRepo,
	})
	_, adminErr := adminInterceptor(t.Context(),
		&adminportalv1.AdminListSectionsRequest{PlayerSessionToken: "raw-token"},
		listSectionsInfo(), adminHandler.unary())
	require.NoError(t, adminErr, "positive control: an admin MUST pass the gate")
	require.Equal(t, 1, adminHandler.calls, "an admitted caller MUST reach the handler exactly once")
}

// TestTheInterceptorRefusalIsWrappedExactlyOnce is the assertable form of the
// property "the refusal carries ONE code".
//
// oops's Code() resolves the DEEPEST code in the chain, so under a double wrap
// an assertion on the code agrees with itself and disagrees with the truth —
// it cannot express single-wrap. Chain DEPTH can. Wrapping the refusal in a
// second oops.Code(...) makes this test fail.
func TestTheInterceptorRefusalIsWrappedExactlyOnce(t *testing.T) {
	interceptor := NewAdminSectionInterceptor(AdminInterceptorDeps{
		Engine:      seedEngineFor(t, nonAdminPlayerULID()),
		SessionRepo: sessionRepoFor(t, nonAdminPlayerULID()),
	})

	_, err := interceptor(t.Context(),
		&adminportalv1.AdminListSectionsRequest{PlayerSessionToken: "raw-token"},
		listSectionsInfo(), (&recordingHandler{}).unary())
	require.Error(t, err)

	oopsFrames := 0
	for cur := err; cur != nil; cur = errors.Unwrap(cur) {
		var o oops.OopsError
		if errors.As(cur, &o) {
			// errors.As walks the chain itself, so count only an EXACT match at
			// this link rather than letting one deep frame be counted at every
			// shallower link.
			if _, direct := cur.(oops.OopsError); direct { //nolint:errorlint // exact-link check is the assertion
				oopsFrames++
			}
		}
	}
	require.Equal(t, 1, oopsFrames,
		"the refusal MUST be constructed once and never re-wrapped: a second oops frame "+
			"would make Code() report the deepest code while the caller believes it reads the outermost")
}

// TestAnAdminMethodWithNoDescriptorIsRefusedBeforeSubjectResolution pins the
// evaluation ORDER: declaration is checked before any session work, so an
// undeclared method cannot be defaulted into a section by a caller who happens
// to hold a valid token.
func TestAnAdminMethodWithNoDescriptorIsRefusedBeforeSubjectResolution(t *testing.T) {
	repo := sessionRepoFor(t, adminPlayerULID())
	handler := &recordingHandler{}

	interceptor := NewAdminSectionInterceptor(AdminInterceptorDeps{
		Engine:      seedEngineFor(t, adminPlayerULID(), "admin"),
		SessionRepo: repo,
	})

	_, err := interceptor(t.Context(),
		&adminportalv1.AdminListSectionsRequest{PlayerSessionToken: "raw-token"},
		&grpc.UnaryServerInfo{FullMethod: "/holomush.adminportal.v1.AdminPortalService/AdminPurgeEverything"},
		handler.unary())

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_SECTION_NOT_DECLARED")
	require.Zero(t, repo.calls,
		"declaration MUST be checked BEFORE subject resolution: the session repository was reached")
	require.Zero(t, handler.calls)
}

// noTokenRequest is a request message with no GetPlayerSessionToken method, so
// the interceptor's single interface assertion fails on it.
type noTokenRequest struct{}

// TestARequestWithNoSessionTokenAccessorIsRefusedWithoutASessionLookup pins the
// fourth fail-closed arm: an admin method whose request cannot carry a subject
// is REFUSED, never passed through.
func TestARequestWithNoSessionTokenAccessorIsRefusedWithoutASessionLookup(t *testing.T) {
	repo := sessionRepoFor(t, adminPlayerULID())
	handler := &recordingHandler{}

	interceptor := NewAdminSectionInterceptor(AdminInterceptorDeps{
		Engine:      seedEngineFor(t, adminPlayerULID(), "admin"),
		SessionRepo: repo,
	})

	_, err := interceptor(t.Context(), &noTokenRequest{}, listSectionsInfo(), handler.unary())

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_SECTION_NO_SUBJECT")
	require.Zero(t, repo.calls, "a request with no token accessor MUST NOT reach the session repository")
	require.Zero(t, handler.calls)
}

// TestAnUnresolvableSessionIsRefusedWithoutLeakingTheResolverError pins that a
// resolver failure denies rather than passing through, and that the resolver's
// own error text never becomes the refusal.
func TestAnUnresolvableSessionIsRefusedWithoutLeakingTheResolverError(t *testing.T) {
	repo := &countingSessionRepo{t: t, err: oops.Code("PLAYER_SESSION_NOT_FOUND").
		Errorf("no session row for hash deadbeef")}
	handler := &recordingHandler{}

	interceptor := NewAdminSectionInterceptor(AdminInterceptorDeps{
		Engine:      seedEngineFor(t, adminPlayerULID(), "admin"),
		SessionRepo: repo,
	})

	_, err := interceptor(t.Context(),
		&adminportalv1.AdminListSectionsRequest{PlayerSessionToken: "raw-token"},
		listSectionsInfo(), handler.unary())

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_SECTION_NO_SUBJECT")
	require.Equal(t, 1, repo.calls)
	require.Zero(t, handler.calls)
	require.NotContains(t, status.Convert(err).Message(), "deadbeef",
		"the resolver's inner error MUST NOT reach the wire message")
}

// TestAMethodOutsideThePortalPrefixPassesThroughUntouched uses a REAL method on
// the REAL break-glass operator service as its out-of-prefix fixture, so the
// test also pins that the portal gate does not reach that surface — which is
// served over a UNIX socket and never mounted on this server at all.
func TestAMethodOutsideThePortalPrefixPassesThroughUntouched(t *testing.T) {
	repo := sessionRepoFor(t, nonAdminPlayerULID())
	handler := &recordingHandler{}

	interceptor := NewAdminSectionInterceptor(AdminInterceptorDeps{
		Engine:      seedEngineFor(t, nonAdminPlayerULID()),
		SessionRepo: repo,
	})

	_, err := interceptor(t.Context(), &noTokenRequest{},
		&grpc.UnaryServerInfo{FullMethod: "/holomush.admin.v1.AdminService/Status"},
		handler.unary())

	require.NoError(t, err)
	require.Equal(t, 1, handler.calls, "an out-of-prefix method MUST reach its handler exactly once")
	require.Zero(t, repo.calls, "an out-of-prefix method MUST NOT trigger admin subject resolution")
}

// TestAMisconfiguredInterceptorDeniesTheWholePortalPrefix pins arm 1: a nil
// dependency FAILS CLOSED for every portal method rather than passing traffic
// through ungated, while out-of-prefix traffic still flows.
func TestAMisconfiguredInterceptorDeniesTheWholePortalPrefix(t *testing.T) {
	for _, tc := range []struct {
		name string
		deps AdminInterceptorDeps
	}{
		{"nil engine", AdminInterceptorDeps{SessionRepo: sessionRepoFor(t, adminPlayerULID())}},
		{"nil session repo", AdminInterceptorDeps{Engine: seedEngineFor(t, adminPlayerULID(), "admin")}},
		{"both nil", AdminInterceptorDeps{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := &recordingHandler{}
			interceptor := NewAdminSectionInterceptor(tc.deps)

			_, err := interceptor(t.Context(),
				&adminportalv1.AdminListSectionsRequest{PlayerSessionToken: "raw-token"},
				listSectionsInfo(), handler.unary())
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "ADMIN_SECTION_GATE_UNAVAILABLE")
			require.Zero(t, handler.calls)

			// Out-of-prefix still passes through: the refusal is scoped to the
			// portal, not a blanket outage.
			passthrough := &recordingHandler{}
			_, passErr := interceptor(t.Context(), &noTokenRequest{},
				&grpc.UnaryServerInfo{FullMethod: "/holomush.admin.v1.AdminService/Status"},
				passthrough.unary())
			require.NoError(t, passErr)
			require.Equal(t, 1, passthrough.calls)
		})
	}
}

// TestEveryInterceptorRefusalIsMappedToPermissionDeniedWithTheStaticMessage
// pins the boundary translation at the ONE layer that performs it: every arm
// maps to codes.PermissionDenied carrying the same field-free message, so the
// arm a caller tripped is invisible to them.
func TestEveryInterceptorRefusalIsMappedToPermissionDeniedWithTheStaticMessage(t *testing.T) {
	deny := NewAdminSectionInterceptor(AdminInterceptorDeps{
		Engine:      seedEngineFor(t, nonAdminPlayerULID()),
		SessionRepo: sessionRepoFor(t, nonAdminPlayerULID()),
	})
	admit := NewAdminSectionInterceptor(AdminInterceptorDeps{
		Engine:      seedEngineFor(t, adminPlayerULID(), "admin"),
		SessionRepo: sessionRepoFor(t, adminPlayerULID()),
	})

	for _, tc := range []struct {
		name        string
		interceptor grpc.UnaryServerInterceptor
		req         any
		info        *grpc.UnaryServerInfo
	}{
		{"policy denial", deny, &adminportalv1.AdminListSectionsRequest{PlayerSessionToken: "t"}, listSectionsInfo()},
		{
			"undeclared method", admit, &adminportalv1.AdminListSectionsRequest{PlayerSessionToken: "t"},
			&grpc.UnaryServerInfo{FullMethod: "/holomush.adminportal.v1.AdminPortalService/AdminPurgeEverything"},
		},
		{"no subject", admit, &noTokenRequest{}, listSectionsInfo()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.interceptor(t.Context(), tc.req, tc.info, (&recordingHandler{}).unary())
			require.Error(t, err)
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			msg := status.Convert(err).Message()
			require.Equal(t, adminDeniedMessage, msg)
			for _, leak := range []string{"DENY_", "ADMIN_SECTION", "admin_section", "characters", "player_id"} {
				require.NotContains(t, msg, leak,
					"the refusal message MUST carry no distinguishing field")
			}
		})
	}
}

// TestTheResolvedPlayerIsReadableFromContextByTheHandler pins the ctx stash: a
// handler downstream of a passed admission reads the ALREADY-resolved player
// instead of resolving it a second time.
func TestTheResolvedPlayerIsReadableFromContextByTheHandler(t *testing.T) {
	repo := sessionRepoFor(t, adminPlayerULID())
	interceptor := NewAdminSectionInterceptor(AdminInterceptorDeps{
		Engine:      seedEngineFor(t, adminPlayerULID(), "admin"),
		SessionRepo: repo,
	})

	var seen *auth.PlayerSession
	_, err := interceptor(t.Context(),
		&adminportalv1.AdminListSectionsRequest{PlayerSessionToken: "raw-token"},
		listSectionsInfo(),
		func(ctx context.Context, _ any) (any, error) {
			var ok bool
			seen, ok = AdminPlayerFromContext(ctx)
			require.True(t, ok, "the handler MUST find the resolved player on the context")
			return &adminportalv1.AdminListSectionsResponse{}, nil
		})

	require.NoError(t, err)
	require.NotNil(t, seen)
	require.Equal(t, adminPlayerULID(), seen.PlayerID)
	require.Equal(t, 1, repo.calls, "the player MUST be resolved exactly once per request")
}

// tokenOnlyRequest carries a session token but NO GetSectionId accessor, so it
// passes the subject arm and then fails the SectionFromRequest arm's typed
// assertion — the exact shape a future request message that forgot the field
// would have.
type tokenOnlyRequest struct{}

func (tokenOnlyRequest) GetPlayerSessionToken() string { return "raw-token" }

// TestASectionFromRequestMethodWithNoUsableIDIsRefusedBeforeTheHandler pins the
// third gating arm's two fail-closed inputs: a request that cannot carry a
// section id at all, and one whose id is blank once trimmed.
//
// Neither may be defaulted. Falling through to the fixed-SectionID arm would
// call AssertSectionAccess with an empty id, which gate.go refuses with
// ADMIN_SECTION_REQUEST_MALFORMED BEFORE evaluation — turning every
// AdminGetSection call, admin or not, into a failure before the handler ran.
func TestASectionFromRequestMethodWithNoUsableIDIsRefusedBeforeTheHandler(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  any
	}{
		{"no GetSectionId accessor", tokenOnlyRequest{}},
		{
			"whitespace-only id",
			&adminportalv1.AdminGetSectionRequest{SectionId: "  \t ", PlayerSessionToken: "raw-token"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := &recordingHandler{}
			interceptor := NewAdminSectionInterceptor(AdminInterceptorDeps{
				Engine:      seedEngineFor(t, adminPlayerULID(), "admin"),
				SessionRepo: sessionRepoFor(t, adminPlayerULID()),
			})

			_, err := interceptor(t.Context(), tc.req, getSectionInfo(), handler.unary())

			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "ADMIN_SECTION_NO_SECTION_ID")
			require.Zero(t, handler.calls, "the wrapped handler MUST record zero invocations")
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			require.Equal(t, adminDeniedMessage, status.Convert(err).Message())
		})
	}
}

// TestADescriptorCarryingNoRecognisedShapeHitsTheDenyingDefault pins that the
// shape switch is EXHAUSTIVE with a denying default.
//
// A fourth shape added later must DENY rather than acquire whichever arm
// happened to be reachable — an entry that silently inherited the enumerating
// arm would be gated at the resource type instead of at its section, and an
// entry that inherited the fixed arm would be gated against an empty id.
//
// The entry is planted at runtime because validateAdminDescriptors refuses to
// let one exist at boot; that refusal is the first line of defence and this is
// the second.
func TestADescriptorCarryingNoRecognisedShapeHitsTheDenyingDefault(t *testing.T) {
	const planted = "AdminShapelessMethod"
	section.AdminDescriptors[planted] = section.MethodDescriptor{Action: section.ActionRead}
	t.Cleanup(func() { delete(section.AdminDescriptors, planted) })

	handler := &recordingHandler{}
	interceptor := NewAdminSectionInterceptor(AdminInterceptorDeps{
		Engine:      seedEngineFor(t, adminPlayerULID(), "admin"),
		SessionRepo: sessionRepoFor(t, adminPlayerULID()),
	})

	_, err := interceptor(t.Context(),
		&adminportalv1.AdminGetSectionRequest{SectionId: "characters", PlayerSessionToken: "raw-token"},
		&grpc.UnaryServerInfo{FullMethod: "/holomush.adminportal.v1.AdminPortalService/" + planted},
		handler.unary())

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_SECTION_NOT_DECLARED")
	require.Zero(t, handler.calls, "an unrecognised descriptor shape MUST deny, never pass through")
}

// TestAdminSectionFromContextReportsAbsenceRatherThanAnEmptySection pins the
// section accessor's fail-closed shape, for the same reason its player twin
// does: a zero-valued Section is an unauthorized entry wearing an authorized
// shape, and a handler projecting one would answer for a section no gate
// resolved.
func TestAdminSectionFromContextReportsAbsenceRatherThanAnEmptySection(t *testing.T) {
	got, ok := AdminSectionFromContext(context.Background())
	require.False(t, ok)
	require.Empty(t, got.ID)
}

// TestTheResolvedSectionIsReadableFromContextByTheHandler is the positive
// control for the accessor above: after a PASSED gate on a SectionFromRequest
// method, the resolved entry is on the context for the handler to project.
func TestTheResolvedSectionIsReadableFromContextByTheHandler(t *testing.T) {
	interceptor := NewAdminSectionInterceptor(AdminInterceptorDeps{
		Engine:      seedEngineFor(t, adminPlayerULID(), "admin"),
		SessionRepo: sessionRepoFor(t, adminPlayerULID()),
	})

	var seen section.Section
	_, err := interceptor(t.Context(),
		&adminportalv1.AdminGetSectionRequest{SectionId: "characters", PlayerSessionToken: "raw-token"},
		getSectionInfo(),
		func(ctx context.Context, _ any) (any, error) {
			var ok bool
			seen, ok = AdminSectionFromContext(ctx)
			require.True(t, ok, "the handler MUST find the gated section on the context")
			return &adminportalv1.AdminGetSectionResponse{}, nil
		})

	require.NoError(t, err)
	require.Equal(t, section.ID("characters"), seen.ID)
	require.Equal(t, section.StatusAvailable, seen.Status)
}

// TestAdminPlayerFromContextReportsAbsenceRatherThanAZeroPlayer pins the
// accessor's fail-closed shape: a bare context yields ok=false, never a
// zero-valued session a caller could mistake for an authenticated one.
func TestAdminPlayerFromContextReportsAbsenceRatherThanAZeroPlayer(t *testing.T) {
	got, ok := AdminPlayerFromContext(context.Background())
	require.False(t, ok)
	require.Nil(t, got)
}
