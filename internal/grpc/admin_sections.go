// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/admin/section"
	"github.com/holomush/holomush/pkg/errutil"
	adminportalv1 "github.com/holomush/holomush/pkg/proto/holomush/adminportal/v1"
)

// AdminGetSection returns the ONE registry entry the caller named.
//
// # It is a pure PROJECTION, and that is the security property
//
// This handler calls no gate helper and performs no registry lookup. Both
// omissions are deliberate, and this RPC is the one where they matter most:
// AdminGetSection is the single admin-portal method whose section id is
// ATTACKER-CONTROLLED.
//
//   - A gate call HERE would be the per-handler assertion D-99 abolished — a
//     check a future method can forget, on the very method that can least afford
//     it. NewAdminSectionInterceptor already ran the full check from the
//     fail-closed section.AdminDescriptors declaration, driven by the descriptor
//     entry's SectionFromRequest shape.
//   - A registry lookup HERE would reintroduce the enumeration oracle D-06
//     closed: consulting the registry before (or independently of) the ABAC
//     decision lets a denied caller tell a registered id from an unregistered
//     one by the shape of the answer.
//
// So the interceptor resolves and stashes; this reads and projects. Every
// refusal a caller can observe from this RPC — the denial, the
// indistinguishable unregistered-id refusal, the blank-id refusal, and the §10.3
// FailedPrecondition for a section that is registered but not yet implemented —
// originates upstream of this function. Stubbing this body to return an empty
// response leaves all of them intact, which is how that claim is proven rather
// than asserted.
func (s *AdminPortalServer) AdminGetSection(
	ctx context.Context,
	_ *adminportalv1.AdminGetSectionRequest,
) (*adminportalv1.AdminGetSectionResponse, error) {
	entry, gated := AdminSectionFromContext(ctx)
	if !gated {
		// Unreachable through the gated server: the interceptor stashes the
		// resolved entry before it calls this handler. Reached only by a
		// composition that mounted this service WITHOUT the gate — which must
		// fail closed rather than answer for a section nothing authorized.
		errutil.LogErrorContext(ctx, "admin portal: no gated section on context", nil)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	return &adminportalv1.AdminGetSectionResponse{
		Section: &adminportalv1.AdminSection{
			Id:          string(entry.ID),
			DisplayName: section.DisplayName(entry.ID),
			Status:      string(entry.Status),
		},
	}, nil
}
