// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package access_test's admin-section specs assert the /admin trust boundary at
// the WIRE, over a real gRPC connection to a server built by the production
// factory.
//
// # SECTION_NOT_IMPLEMENTED is only ever visible to a PERMITTED caller
//
// section.AssertSectionAccess evaluates the ABAC gate (step 1) BEFORE it
// consults the registry (step 2) and before it reads a section's availability
// (step 4). A DENIED caller therefore receives DENY_ADMIN_SECTION for every one
// of the seven sections — the six planned ones included — and never learns that
// a section exists, let alone that it is unbuilt. The FailedPrecondition a
// planned section produces is reachable only after the gate said yes, so it
// discloses nothing a permitted caller could not read off AdminListSections.
//
// That ordering IS the property (INV-PRIVACY-11), and reordering the two steps
// is what the differential case below turns red.
package access_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/admin/section"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	adminportalv1 "github.com/holomush/holomush/pkg/proto/holomush/adminportal/v1"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
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

// TestTheAdminSectionGateHoldsForEverySectionAtTheWire is ROADMAP Success
// Criterion 4: ONE table that walks section.All() once and puts every entry
// through FOUR cases over a real gRPC connection.
//
//  1. a NON-ADMIN naming the section              → PermissionDenied, static message
//  2. an ADMIN naming the same section            → the row, or FailedPrecondition if planned
//  3. an ADMIN naming a MIS-CASED variant         → PermissionDenied, byte-identical to (1)
//  4. a NON-ADMIN naming an UNREGISTERED variant  → PermissionDenied, byte-identical to (1)
//
// Case 2 is the paired positive control: without it, case 1 cannot distinguish
// "denied for lack of the admin role" from "the RPC is broken" or "the listener
// was never wired".
//
// Cases 3 and 4 are the two halves of the opacity contract, and they lean
// opposite ways on purpose. Case 4 is INV-PRIVACY-11's differential: a DENIED
// caller must not be able to tell a registered section from one that does not
// exist, which holds only because the gate is evaluated before the registry is
// consulted. Case 3 is its permitted-caller counterpart: an admin who mis-cases
// an id receives DENY_ADMIN_SECTION_UNREGISTERED, which maps to the SAME static
// refusal — so even a permitted caller gets no near-miss oracle, and matching
// stays exact byte equality with no case folding anywhere on this path.
//
// # The counts are the anti-vacuity guard
//
// A loop that silently iterated nothing would satisfy every assertion inside it.
// The denial and positive-control counts are asserted >= 7, and the outcome
// counts by EXACT equality — a registry that lost a planned section, or promoted
// one to available without the rest of this phase noticing, fails here.
//
// Wire assertions carry NO oops code: an oops value does not survive a gRPC
// round trip, so an assertion that appeared to pass on one here would be reading
// something else. The typed internal codes are asserted in-process in
// internal/grpc/admin_sections_test.go and admin_interceptor_test.go.
//
// Verifies: INV-PRIVACY-11
func TestTheAdminSectionGateHoldsForEverySectionAtTheWire(t *testing.T) {
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

	// The one static refusal every denied answer must carry, spelled once.
	const staticRefusal = "admin section access denied"

	deniedCount, permittedCount, notImplementedCount, availableCount := 0, 0, 0, 0

	for _, entry := range section.All() {
		id := string(entry.ID)
		t.Run(id, func(t *testing.T) {
			// --- case 1: the non-admin denial ---
			_, denyErr := get(nonAdmin.PlayerSessionToken(), id)
			require.Error(t, denyErr, "a non-admin MUST be refused for every section")
			require.Equal(t, codes.PermissionDenied, status.Code(denyErr))
			denyMsg := status.Convert(denyErr).Message()
			require.Equal(t, staticRefusal, denyMsg,
				"the refusal MUST be the same static message for every section")
			deniedCount++

			// --- case 2: the paired positive control ---
			resp, adminErr := get(admin.PlayerSessionToken(), id)
			switch entry.Status {
			case section.StatusAvailable:
				require.NoError(t, adminErr, "positive control: an admin MUST reach an available section")
				require.Equal(t, id, resp.GetSection().GetId())
				availableCount++
			case section.StatusPlanned:
				require.Error(t, adminErr)
				require.Equal(t, codes.FailedPrecondition, status.Code(adminErr),
					"a PERMITTED caller MUST get past the gate and be refused for a DIFFERENT reason")
				notImplementedCount++
			default:
				t.Fatalf("section %q carries status %q, outside the closed vocabulary", id, entry.Status)
			}
			permittedCount++

			// --- case 3: a PERMITTED caller mis-cases the id ---
			_, misCasedErr := get(admin.PlayerSessionToken(), strings.ToUpper(id))
			require.Error(t, misCasedErr, "matching is exact byte equality; a mis-cased id MUST NOT resolve")
			require.Equal(t, codes.PermissionDenied, status.Code(misCasedErr))
			require.Equal(t, denyMsg, status.Convert(misCasedErr).Message(),
				"even a permitted caller MUST NOT be told their id was a near-miss")

			// --- case 4: the INV-PRIVACY-11 differential ---
			//
			// require.Equal on the EXACT strings, never Contains: a substring
			// match would pass for two messages differing in a suffix, which is
			// precisely the distinguishing field the contract forbids.
			_, unregisteredErr := get(nonAdmin.PlayerSessionToken(), id+"-no-such-01JQ")
			require.Error(t, unregisteredErr)
			require.Equal(t, status.Code(denyErr), status.Code(unregisteredErr))
			require.Equal(t, denyMsg, status.Convert(unregisteredErr).Message(),
				"a denied caller MUST NOT be able to tell a registered section from one that does not exist")
		})
	}

	require.GreaterOrEqual(t, deniedCount, 7, "a loop that iterated zero sections MUST fail loudly")
	require.GreaterOrEqual(t, permittedCount, 7, "every denial MUST be paired with a positive control")
	require.Equal(t, 6, notImplementedCount, "all six deferred sections MUST refuse AFTER the gate")
	require.Equal(t, 1, availableCount, "exactly one section is implemented in this phase")
}

// TestARolesHintSayingAdminDoesNotSurviveAnABACDenial is ADMIN-08's boundary
// assertion: the `roles` field changes only what is DRAWN.
//
// The two halves are made to DISAGREE on purpose. The role lookup is real — the
// harness wires store.PostgresRoleStore.PlayerRoles into the CoreServer exactly
// as cmd/holomush does — so a player granted the admin role reports
// roles == ["admin"]. The ABAC engine is DenyAllEngine, so the same player is
// refused by AdminGetSection. A handler that ever short-circuited on `roles`
// would turn this green.
//
// # Its first assertion is a setup precondition, and that is load-bearing
//
// require.Contains on the roles list must run BEFORE the denial, because the
// denial path never consults `roles` at all — that is the whole point. Without
// the precondition this test would never touch the field, and removing the
// harness's WithPlayerRoleLookup wiring would leave it green: it would be a
// demonstration of nothing. The precondition is what makes that wiring
// load-bearing.
//
// It reads the field off CoreServer.CheckPlayerSession rather than
// WebCheckSession because Handler.WebCheckSession forwards it verbatim — the
// core response is the only source, so asserting here asserts the same value the
// browser would receive.
func TestARolesHintSayingAdminDoesNotSurviveAnABACDenial(t *testing.T) {
	ctx := context.Background()

	srv := integrationtest.Start(
		t,
		integrationtest.WithPolicyEngine(policytest.DenyAllEngine()),
		integrationtest.WithGatedGRPCListener(),
	)
	defer srv.Stop()

	claimsAdmin := srv.ConnectAuthedWithRoles(ctx, "Rolesbound", []string{"admin"})

	// PRECONDITION — read the field, before anything else.
	checked, checkErr := srv.CoreServer().CheckPlayerSession(ctx, &corev1.CheckPlayerSessionRequest{
		PlayerSessionToken: claimsAdmin.PlayerSessionToken(),
	})
	require.NoError(t, checkErr)
	require.Contains(t, checked.GetRoles(), "admin",
		"precondition: the nav hint MUST report admin, or the denial below proves nothing about it")

	// The same caller, the same role, denied anyway.
	client := adminportalv1.NewAdminPortalServiceClient(srv.GatedGRPCConn())
	_, err := client.AdminGetSection(ctx, &adminportalv1.AdminGetSectionRequest{
		SectionId:          "characters",
		PlayerSessionToken: claimsAdmin.PlayerSessionToken(),
	})

	require.Error(t, err, "roles is a nav hint; the RPC MUST still evaluate ABAC and deny")
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.Equal(t, "admin section access denied", status.Convert(err).Message())
}
