// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package access_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	adminportalv1 "github.com/holomush/holomush/pkg/proto/holomush/adminportal/v1"
)

// TestANonAdminCallingTheAdminPortalDirectlyOverGRPCIsDeniedAtTheWire is
// ROADMAP Success Criterion 1, asserted where the threat actually lives.
//
// The caller here bypasses the browser and the gateway entirely and speaks gRPC
// to the core server — the exact shape T-06-01 describes. The server it reaches
// is built by the ONE production factory (integrationtest.WithGatedGRPCListener
// calls holoGRPC.NewGRPCServer, the same constructor cmd/holomush/sub_grpc.go
// uses), so the denial proven here is a property of the production composition
// rather than of a constructor nothing calls.
//
// # Two layers, two assertions, deliberately never collapsed
//
// This test asserts WIRE OPACITY only: the mapped status code, the exact static
// message, and the absence of every distinguishing substring. It asserts NO
// oops code, because an oops value does not survive a gRPC round trip — an
// assertion that appeared to pass on one here would be reading something else.
// The typed internal code (DENY_ADMIN_SECTION) is asserted in-process at the
// interceptor, in internal/grpc/admin_interceptor_test.go.
func TestANonAdminCallingTheAdminPortalDirectlyOverGRPCIsDeniedAtTheWire(t *testing.T) {
	ctx := context.Background()

	srv := integrationtest.Start(t,
		integrationtest.WithRealABAC(),
		integrationtest.WithGatedGRPCListener(),
	)
	defer srv.Stop()

	client := adminportalv1.NewAdminPortalServiceClient(srv.GatedGRPCConn())

	nonAdmin := srv.ConnectAuthed(ctx, "Plainwire")

	resp, err := client.AdminListSections(ctx, &adminportalv1.AdminListSectionsRequest{
		PlayerSessionToken: nonAdmin.PlayerSessionToken(),
	})

	require.Error(t, err, "a player with no admin role MUST be refused")
	require.Nil(t, resp)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	msg := status.Convert(err).Message()
	require.Equal(t, "admin section access denied", msg,
		"every refusal MUST carry the SAME static message, so the arm the caller tripped is invisible")
	for _, leak := range []string{"DENY_", "admin_section", "characters", "player_id"} {
		require.NotContains(t, msg, leak,
			"the wire message MUST NOT carry %q: a distinguishing field turns a refusal into an oracle", leak)
	}
}

// TestAnAdminCallingTheAdminPortalDirectlyOverGRPCReceivesEverySection is the
// PAIRED POSITIVE CONTROL for the denial above: the same RPC, over the same
// connection, against the same section registry, by a player who differs only
// in holding the admin role.
//
// Without it, `err != nil` above cannot distinguish "denied for lack of the
// admin role" from "the RPC is broken" or "the listener was never wired".
//
// It asserts SEVEN entries including all six planned ones. A handler written
// against AssertSectionAccess rather than AssertSectionAdmission returns one —
// its availability step refuses every planned section with
// SECTION_NOT_IMPLEMENTED — and fails here.
func TestAnAdminCallingTheAdminPortalDirectlyOverGRPCReceivesEverySection(t *testing.T) {
	ctx := context.Background()

	srv := integrationtest.Start(t,
		integrationtest.WithRealABAC(),
		integrationtest.WithGatedGRPCListener(),
	)
	defer srv.Stop()

	client := adminportalv1.NewAdminPortalServiceClient(srv.GatedGRPCConn())

	admin := srv.ConnectAuthedWithRoles(ctx, "Adminwire", []string{"admin"})

	resp, err := client.AdminListSections(ctx, &adminportalv1.AdminListSectionsRequest{
		PlayerSessionToken: admin.PlayerSessionToken(),
	})

	require.NoError(t, err, "positive control: an admin MUST pass the same gate")
	require.Len(t, resp.GetSections(), 7,
		"the whole registry is admitted at once — seed:admin-section-access is scoped by resource TYPE")

	planned, available := 0, 0
	ids := make([]string, 0, len(resp.GetSections()))
	for _, s := range resp.GetSections() {
		ids = append(ids, s.GetId())
		require.NotEmpty(t, s.GetDisplayName(), "section %q MUST carry a nav label", s.GetId())
		switch s.GetStatus() {
		case "planned":
			planned++
		case "available":
			available++
		default:
			t.Fatalf("section %q carries status %q, outside the closed vocabulary", s.GetId(), s.GetStatus())
		}
	}
	require.Equal(t, 6, planned,
		"all six PLANNED sections MUST be listed: the enumeration filter is admission, not access")
	require.Equal(t, 1, available)
	require.Contains(t, ids, "characters")
}
