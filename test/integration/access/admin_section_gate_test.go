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

	"github.com/holomush/holomush/internal/admin/section"
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
//
// Verifies: INV-PRIVACY-12
func TestANonAdminCallingTheAdminPortalDirectlyOverGRPCIsDeniedAtTheWire(t *testing.T) {
	ctx := context.Background()

	srv := integrationtest.Start(
		t,
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

	srv := integrationtest.Start(
		t,
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

// TestEverySectionIsReachableGatedAndRefusingAfterTheGateAtTheWire is ROADMAP
// Success Criterion 4, asserted over a real gRPC connection for ALL SEVEN
// sections through ONE RPC.
//
// The ids come from section.All(), never a hand-written list: a registry that
// grew or lost a section changes what this test covers rather than leaving the
// new one unproven. Every denial is paired with a positive control on the SAME
// section through the SAME RPC, and the counts are asserted at the end so a
// loop that silently iterated nothing fails loudly instead of passing vacuously.
//
// Wire assertions here carry NO oops code: an oops value does not survive a
// gRPC round trip. The typed internal codes are asserted in-process in
// internal/grpc/admin_sections_test.go.
func TestEverySectionIsReachableGatedAndRefusingAfterTheGateAtTheWire(t *testing.T) {
	ctx := context.Background()

	srv := integrationtest.Start(
		t,
		integrationtest.WithRealABAC(),
		integrationtest.WithGatedGRPCListener(),
	)
	defer srv.Stop()

	client := adminportalv1.NewAdminPortalServiceClient(srv.GatedGRPCConn())
	nonAdmin := srv.ConnectAuthed(ctx, "Plainget")
	admin := srv.ConnectAuthedWithRoles(ctx, "Adminget", []string{"admin"})

	get := func(token, id string) (*adminportalv1.AdminGetSectionResponse, error) {
		return client.AdminGetSection(ctx, &adminportalv1.AdminGetSectionRequest{
			SectionId:          id,
			PlayerSessionToken: token,
		})
	}

	denied, permitted, notImplemented, available := 0, 0, 0, 0

	for _, entry := range section.All() {
		t.Run(string(entry.ID), func(t *testing.T) {
			_, denyErr := get(nonAdmin.PlayerSessionToken(), string(entry.ID))
			require.Error(t, denyErr, "a non-admin MUST be refused for every section")
			require.Equal(t, codes.PermissionDenied, status.Code(denyErr))
			require.Equal(t, "admin section access denied", status.Convert(denyErr).Message(),
				"the refusal MUST be the same static message for every section")
			denied++

			resp, adminErr := get(admin.PlayerSessionToken(), string(entry.ID))
			switch entry.Status {
			case section.StatusAvailable:
				require.NoError(t, adminErr, "positive control: an admin MUST reach an available section")
				require.Equal(t, string(entry.ID), resp.GetSection().GetId())
				available++
			case section.StatusPlanned:
				require.Error(t, adminErr)
				require.Equal(t, codes.FailedPrecondition, status.Code(adminErr),
					"a PERMITTED caller MUST get past the gate and be refused for a different reason")
				notImplemented++
			default:
				t.Fatalf("section %q carries status %q, outside the closed vocabulary", entry.ID, entry.Status)
			}
			permitted++
		})
	}

	require.GreaterOrEqual(t, denied, 7, "a loop that iterated zero sections MUST fail")
	require.GreaterOrEqual(t, permitted, 7, "every denial MUST be paired with a positive control")
	require.Equal(t, 6, notImplemented, "all six deferred sections MUST refuse AFTER the gate")
	require.Equal(t, 1, available)
}

// TestADeniedCallerGetsByteIdenticalRefusalsForRegisteredAndUnregisteredIDsAtTheWire
// is the INV-PRIVACY-11 differential asserted where it matters — on the wire,
// for the ONE RPC whose section id is attacker-controlled.
//
// The comparison is require.Equal on the exact strings, not Contains: a
// substring match would pass for two messages that differ in a suffix, which is
// precisely the distinguishing field the contract forbids.
func TestADeniedCallerGetsByteIdenticalRefusalsForRegisteredAndUnregisteredIDsAtTheWire(t *testing.T) {
	ctx := context.Background()

	srv := integrationtest.Start(
		t,
		integrationtest.WithRealABAC(),
		integrationtest.WithGatedGRPCListener(),
	)
	defer srv.Stop()

	client := adminportalv1.NewAdminPortalServiceClient(srv.GatedGRPCConn())
	nonAdmin := srv.ConnectAuthed(ctx, "Plaindiff")

	call := func(id string) error {
		_, err := client.AdminGetSection(ctx, &adminportalv1.AdminGetSectionRequest{
			SectionId:          id,
			PlayerSessionToken: nonAdmin.PlayerSessionToken(),
		})
		return err
	}

	registeredErr := call("characters")
	unregisteredErr := call("no-such-section-01JQ")

	require.Error(t, registeredErr)
	require.Error(t, unregisteredErr)
	require.Equal(t, status.Code(registeredErr), status.Code(unregisteredErr))
	require.Equal(t,
		status.Convert(registeredErr).Message(),
		status.Convert(unregisteredErr).Message(),
		"the registry MUST NOT be enumerable through this parameter")
}
