// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// This file is package section_test, not section: it drives the REAL
// interceptor, which lives in internal/grpc and imports this package. Only an
// external test package may close that loop.
package section_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/admin/section"
	"github.com/holomush/holomush/internal/auth"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/testsupport/abactest"
	"github.com/holomush/holomush/pkg/errutil"
	adminportalv1 "github.com/holomush/holomush/pkg/proto/holomush/adminportal/v1"
)

const completenessPlayerID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

// countingRepo satisfies auth.PlayerSessionRepository and COUNTS resolutions.
// The count is what makes "the undeclared arm runs BEFORE subject resolution"
// observable: an unreached repository is indistinguishable from a reached one
// unless something counts.
type countingRepo struct {
	auth.PlayerSessionRepository // nil: an unoverridden method panics, which is deliberate

	calls int
}

func (r *countingRepo) GetByTokenHash(context.Context, string) (*auth.PlayerSession, error) {
	r.calls++
	return &auth.PlayerSession{
		ID:        ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FCX"),
		PlayerID:  ulid.MustParse(completenessPlayerID),
		TokenHash: "hash",
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func (r *countingRepo) RefreshTTL(context.Context, ulid.ULID, time.Duration) error { return nil }

// TestInterceptorAdminMethodWithoutDescriptorFailsClosed proves the
// ADMIN_SECTION_NOT_DECLARED arm is REACHABLE — a live refusal, not dead code
// that only looks protective.
//
// The technique is to mutate the REAL table rather than build a fake one: a
// fake would prove the interceptor refuses a method missing from a fake, which
// is a different and much weaker claim. Deleting the one shipped entry drives a
// request for a method the gRPC runtime genuinely serves into the arm, and
// t.Cleanup restores it.
//
// The two subtests are a matched pair over one variable — whether the
// descriptor exists — so the refusal is attributable to the declaration and to
// nothing else.
//
// Verifies: INV-ACCESS-16
func TestInterceptorAdminMethodWithoutDescriptorFailsClosed(t *testing.T) {
	const method = "AdminListSections"
	fullMethod := adminportalv1.AdminPortalService_AdminListSections_FullMethodName

	original, existed := section.AdminDescriptors[method]
	require.True(t, existed,
		"the entry this test deletes MUST exist first; deleting nothing would make the refusal vacuous")

	newInterceptor := func(repo auth.PlayerSessionRepository) grpc.UnaryServerInterceptor {
		return holoGRPC.NewAdminSectionInterceptor(holoGRPC.AdminInterceptorDeps{
			Engine: abactest.NewSeedEngine(t, abactest.PlayerProvider(abactest.Player{
				ID: completenessPlayerID, Roles: []string{"admin"},
			})),
			SessionRepo: repo,
		})
	}

	t.Run("declared: the call reaches subject resolution and the handler", func(t *testing.T) {
		repo := &countingRepo{}
		handlerCalls := 0
		_, err := newInterceptor(repo)(t.Context(),
			&adminportalv1.AdminListSectionsRequest{PlayerSessionToken: "raw-token"},
			&grpc.UnaryServerInfo{FullMethod: fullMethod},
			func(context.Context, any) (any, error) {
				handlerCalls++
				return &adminportalv1.AdminListSectionsResponse{}, nil
			})

		require.NoError(t, err, "positive control: with the entry present an admin MUST get through")
		require.Equal(t, 1, repo.calls, "a DECLARED method resolves its subject exactly once")
		require.Equal(t, 1, handlerCalls)
	})

	t.Run("undeclared: refused before any session lookup", func(t *testing.T) {
		delete(section.AdminDescriptors, method)
		t.Cleanup(func() { section.AdminDescriptors[method] = original })

		repo := &countingRepo{}
		handlerCalls := 0
		_, err := newInterceptor(repo)(t.Context(),
			&adminportalv1.AdminListSectionsRequest{PlayerSessionToken: "raw-token"},
			&grpc.UnaryServerInfo{FullMethod: fullMethod},
			func(context.Context, any) (any, error) {
				handlerCalls++
				return &adminportalv1.AdminListSectionsResponse{}, nil
			})

		require.Error(t, err,
			"a served admin method with no descriptor MUST be refused, never defaulted to a section")
		errutil.AssertErrorCode(t, err, "ADMIN_SECTION_NOT_DECLARED")
		require.Zero(t, repo.calls,
			"declaration MUST be checked BEFORE subject resolution — the session repository was reached")
		require.Zero(t, handlerCalls, "the wrapped handler MUST NOT be invoked")
	})
}

// TestTheRestoredDescriptorTableAdmitsAgain is the other half of the
// mutate-and-restore contract: it asserts t.Cleanup actually put the entry
// back, so a later test cannot silently inherit the hole and pass for the wrong
// reason.
func TestTheRestoredDescriptorTableAdmitsAgain(t *testing.T) {
	d, ok := section.LookupMethodDescriptor("AdminListSections")
	require.True(t, ok, "the shipped entry MUST be present outside the mutating test")
	require.True(t, d.EnumeratesAllSections)
	require.Equal(t, section.ActionRead, d.Action)
}
