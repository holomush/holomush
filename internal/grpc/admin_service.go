// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"

	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/admin/section"
	"github.com/holomush/holomush/pkg/errutil"
	adminportalv1 "github.com/holomush/holomush/pkg/proto/holomush/adminportal/v1"
)

// AdminPortalServer serves holomush.adminportal.v1.AdminPortalService — the
// player-session admin portal.
//
// It holds NO gate of its own. Every method on this receiver runs DOWNSTREAM of
// NewAdminSectionInterceptor, which authorized the call from the fail-closed
// section.AdminDescriptors table before the handler was reached and stashed the
// resolved player on the context. A handler here that re-derived authority
// would be the pre-D-99 model: a check a future method can forget.
type AdminPortalServer struct {
	adminportalv1.UnimplementedAdminPortalServiceServer

	engine section.PolicyEvaluator
}

// AdminPortalServerOption is the construction seam for dependencies later
// sections add to THIS receiver.
//
// It is variadic from the first commit even though nothing passes an option
// yet, mirroring CoreServerOption: plans 06-04 and 06-05 add a character
// repository reader and a world command service here, and a positional
// constructor would force each of them to change the signature and every call
// site. Each later plan adds its own WithX and its own sub_grpc.go wiring line;
// none rewrites the constructor.
type AdminPortalServerOption func(*AdminPortalServer)

// NewAdminPortalServer builds the portal receiver.
//
// A nil engine is REFUSED rather than tolerated: the enumeration filter is an
// authorization decision, and a server that cannot evaluate one would answer
// every caller identically — which reads as a correct empty nav and is actually
// an absent gate.
func NewAdminPortalServer(engine section.PolicyEvaluator, opts ...AdminPortalServerOption) *AdminPortalServer {
	if engine == nil {
		panic("grpc.NewAdminPortalServer: nil policy engine would make the section filter decide nothing")
	}
	s := &AdminPortalServer{engine: engine}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// AdminListSections returns the admin sections this player may reach, in the
// registry's row order.
//
// # It runs downstream of a PASSED admission
//
// The interceptor already refused a caller holding no `admin_section:` access,
// so this handler never sees one. An empty list therefore means "admitted, but
// nothing permitted" — not "not an admin". That branch is unreachable in v0.13
// (seed:admin-section-access targets the resource TYPE, so admission is
// all-or-nothing) but the shape is an empty list rather than an error, because
// six downstream plans build on those two answers being distinct.
//
// # The filter is ADMISSION, deliberately not AssertSectionAccess
//
// AssertSectionAccess's availability step (§10.3) refuses a `planned` section
// with SECTION_NOT_IMPLEMENTED. Filtering on it would silently drop all six
// planned sections out of the nav that EXT-02 and D-101 require them to appear
// in — the section's status belongs in the response as DATA, for the nav to
// render in its own treatment, not as a reason to withhold the row.
//
// An evaluation FAILURE is propagated as codes.Internal rather than folded into
// an omission (§8.10): a nav that renders as legitimately short when in fact
// nothing was evaluated is an outage masquerading as an authorization answer.
func (s *AdminPortalServer) AdminListSections(
	ctx context.Context,
	_ *adminportalv1.AdminListSectionsRequest,
) (*adminportalv1.AdminListSectionsResponse, error) {
	playerSession, ok := AdminPlayerFromContext(ctx)
	if !ok {
		// Unreachable through the gated server: the interceptor stashes the
		// player before it calls any handler. Reached only by a composition that
		// mounted this service WITHOUT the gate — which must fail closed rather
		// than enumerate for an unidentified caller.
		errutil.LogErrorContext(ctx, "admin portal: no resolved player on context", nil)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	playerID := playerSession.PlayerID.String()

	entries := section.All()
	out := make([]*adminportalv1.AdminSection, 0, len(entries))
	for _, e := range entries {
		err := section.AssertSectionAdmission(ctx, s.engine, playerID, string(e.ID), section.ActionRead)
		if err != nil {
			if adminSectionEvaluationFailed(err) {
				errutil.LogErrorContext(ctx, "admin portal: section admission could not be evaluated", err,
					"player_id", playerID, "section_id", string(e.ID))
				return nil, status.Errorf(codes.Internal, "internal error")
			}
			// A refusal omits the row and says nothing else: absence is the only
			// signal a caller gets, so a withheld section is indistinguishable
			// from one that does not exist.
			continue
		}
		out = append(out, &adminportalv1.AdminSection{
			Id:          string(e.ID),
			DisplayName: section.DisplayName(e.ID),
			Status:      string(e.Status),
		})
	}

	return &adminportalv1.AdminListSectionsResponse{Sections: out}, nil
}

// adminSectionEvaluationFailed distinguishes §8.10's infrastructure failure from
// an ordinary denial. The two MUST NOT be collapsed: one is "you may not", the
// other is "nobody could tell", and rendering the second as the first sends an
// operator hunting a policy bug that does not exist.
func adminSectionEvaluationFailed(err error) bool {
	oopsErr, isOops := oops.AsOops(err)
	if !isOops {
		return false
	}
	code, isString := oopsErr.Code().(string)
	return isString && code == "ADMIN_SECTION_EVALUATION_FAILED"
}
