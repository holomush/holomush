// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/admin/section"
	"github.com/holomush/holomush/pkg/errutil"
	adminportalv1 "github.com/holomush/holomush/pkg/proto/holomush/adminportal/v1"
)

// adminGetSectionFullMethod is spelled from the generated constant so a package
// or method rename breaks compilation here rather than silently detaching these
// tests from the RPC they claim to cover.
const adminGetSectionFullMethod = adminportalv1.AdminPortalService_AdminGetSection_FullMethodName

func getSectionInfo() *grpc.UnaryServerInfo {
	return &grpc.UnaryServerInfo{FullMethod: adminGetSectionFullMethod}
}

// getSectionOutcome is what one gated AdminGetSection call produced: the
// response, the error, and whether the HANDLER was reached at all.
//
// handlerRan is carried because several assertions below are about WHERE an
// answer came from, not only what it was — a refusal that never reached the
// handler is the property, and it is invisible unless something records it.
type getSectionOutcome struct {
	resp       *adminportalv1.AdminGetSectionResponse
	err        error
	handlerRan bool
}

// callGetSection drives the REAL interceptor in front of the REAL handler, the
// same composition NewGRPCServer mounts in production.
//
// Neither half is stubbed. A test that called the handler directly would prove
// nothing about AdminGetSection, whose entire authorization — including the
// planned-section refusal — happens upstream of it.
func callGetSection(t *testing.T, playerID ulid.ULID, roles []string, sectionID string) getSectionOutcome {
	t.Helper()

	engine := seedEngineFor(t, playerID, roles...)
	interceptor := NewAdminSectionInterceptor(AdminInterceptorDeps{
		Engine:      engine,
		SessionRepo: sessionRepoFor(t, playerID),
	})
	server := NewAdminPortalServer(engine)

	out := getSectionOutcome{}
	raw, err := interceptor(t.Context(),
		&adminportalv1.AdminGetSectionRequest{
			SectionId:          sectionID,
			PlayerSessionToken: "raw-token",
		},
		getSectionInfo(),
		func(ctx context.Context, req any) (any, error) {
			out.handlerRan = true
			return server.AdminGetSection(ctx, req.(*adminportalv1.AdminGetSectionRequest))
		})
	out.err = err
	if raw != nil {
		out.resp, _ = raw.(*adminportalv1.AdminGetSectionResponse)
	}
	return out
}

// TestAnAdminReachesTheAvailableSectionThroughAdminGetSection is the positive
// control the whole file rests on: without it, every refusal below could be a
// broken RPC rather than a gate doing its job.
func TestAnAdminReachesTheAvailableSectionThroughAdminGetSection(t *testing.T) {
	got := callGetSection(t, adminPlayerULID(), []string{"admin"}, "characters")

	require.NoError(t, got.err)
	require.True(t, got.handlerRan, "a permitted caller MUST reach the handler")
	require.Equal(t, "characters", got.resp.GetSection().GetId())
	require.Equal(t, string(section.StatusAvailable), got.resp.GetSection().GetStatus())
	require.NotEmpty(t, got.resp.GetSection().GetDisplayName())
}

// TestAnAdminNamingAPlannedSectionIsRefusedAfterTheGate is ROADMAP Success
// Criterion 4's other half: the six deferred sections are REACHABLE over the
// wire and refuse only AFTER the ABAC decision permitted the caller.
//
// The refusal is produced by section.AssertSectionAccess step 4, which the
// INTERCEPTOR calls — so it is provable that the handler never ran, which is
// what makes "after the gate" more than a claim about source order.
func TestAnAdminNamingAPlannedSectionIsRefusedAfterTheGate(t *testing.T) {
	planned := 0
	for _, entry := range section.All() {
		if entry.Status != section.StatusPlanned {
			continue
		}
		planned++
		t.Run(string(entry.ID), func(t *testing.T) {
			got := callGetSection(t, adminPlayerULID(), []string{"admin"}, string(entry.ID))

			require.Error(t, got.err)
			require.False(t, got.handlerRan,
				"the planned-section refusal MUST originate in the interceptor, not the handler")
			require.Equal(t, codes.FailedPrecondition, status.Code(got.err))
			require.Equal(t, sectionNotImplementedMessage, status.Convert(got.err).Message())
			errutil.AssertErrorCode(t, got.err, "SECTION_NOT_IMPLEMENTED")
		})
	}
	require.Equal(t, 6, planned,
		"all six deferred sections MUST be exercised; a registry that lost one makes this loop weaker without failing")
}

// TestANonAdminIsDeniedIdenticallyForEverySection walks section.All() rather
// than a hand-written list, and pairs EVERY denial with a positive control on
// the SAME section through the SAME RPC.
//
// Without the pairing, a denial is indistinguishable from an RPC that refuses
// everyone; without the section.All() iteration, a registry that grew a section
// would leave it unproven.
func TestANonAdminIsDeniedIdenticallyForEverySection(t *testing.T) {
	denials, controls := 0, 0

	for _, entry := range section.All() {
		t.Run(string(entry.ID), func(t *testing.T) {
			denied := callGetSection(t, nonAdminPlayerULID(), nil, string(entry.ID))
			require.Error(t, denied.err)
			require.False(t, denied.handlerRan, "a denied caller MUST NOT reach the handler")
			require.Equal(t, codes.PermissionDenied, status.Code(denied.err))
			require.Equal(t, adminDeniedMessage, status.Convert(denied.err).Message())
			errutil.AssertErrorCode(t, denied.err, "DENY_ADMIN_SECTION")
			denials++

			// Paired positive control: the same section, the same RPC, a caller
			// differing only in holding the admin role.
			permitted := callGetSection(t, adminPlayerULID(), []string{"admin"}, string(entry.ID))
			switch entry.Status {
			case section.StatusAvailable:
				require.NoError(t, permitted.err)
			case section.StatusPlanned:
				require.Equal(t, codes.FailedPrecondition, status.Code(permitted.err),
					"a permitted caller MUST get past the gate and be refused for a DIFFERENT reason")
			default:
				t.Fatalf("section %q carries status %q, outside the closed vocabulary", entry.ID, entry.Status)
			}
			controls++
		})
	}

	require.GreaterOrEqual(t, denials, 7, "a loop that iterated nothing MUST fail rather than pass vacuously")
	require.GreaterOrEqual(t, controls, 7)
}

// TestADeniedCallerCannotTellARegisteredSectionFromAnUnregisteredOne is the
// INV-PRIVACY-11 differential: for the SAME denied caller, a registered id and
// an id that does not exist produce byte-identical answers.
//
// Reordering section.AssertSectionAccess's step 1 (the gate) and step 2 (the
// registry lookup) makes this fail — which is the point: the ordering IS the
// property, and nothing else in the suite observes it.
func TestADeniedCallerCannotTellARegisteredSectionFromAnUnregisteredOne(t *testing.T) {
	registered := callGetSection(t, nonAdminPlayerULID(), nil, "characters")
	unregistered := callGetSection(t, nonAdminPlayerULID(), nil, "no-such-section-01JQ")

	require.Error(t, registered.err)
	require.Error(t, unregistered.err)
	require.Equal(t, status.Code(registered.err), status.Code(unregistered.err))
	require.Equal(t,
		status.Convert(registered.err).Message(),
		status.Convert(unregistered.err).Message(),
		"a denied caller MUST NOT be able to distinguish a registered section from one that does not exist")
}

// TestAMisCasedSectionIDBehavesExactlyLikeAnUnregisteredOne pins that matching
// is exact byte equality with no case folding, anywhere on this path.
//
// # The wire consequence looks like a bug and is not
//
// mapAdminSectionError maps DENY_ADMIN_SECTION_UNREGISTERED to the SAME static
// PermissionDenied message a real denial produces. So a caller the gate
// PERMITTED who mis-cases an id is deliberately not told the id was close — the
// answer is indistinguishable from "you may not". That is the opacity-preserving
// choice T-06-09 requires; turning it into a distinguishable NotFound would
// hand any permitted caller a registry-probing oracle and is exactly what this
// comment exists to forestall.
func TestAMisCasedSectionIDBehavesExactlyLikeAnUnregisteredOne(t *testing.T) {
	got := callGetSection(t, adminPlayerULID(), []string{"admin"}, "Characters")

	require.Error(t, got.err)
	require.False(t, got.handlerRan)
	errutil.AssertErrorCode(t, got.err, "DENY_ADMIN_SECTION_UNREGISTERED")
	require.Equal(t, codes.PermissionDenied, status.Code(got.err))
	require.Equal(t, adminDeniedMessage, status.Convert(got.err).Message(),
		"a mis-cased id MUST read as an ordinary refusal on the wire, never as a near-miss")
}

// TestABlankSectionIDIsRefusedByTheInterceptorBeforeTheHandlerRuns pins the
// MECHANISM, not merely the outcome.
//
// The refusal comes from the interceptor's own strings.TrimSpace check, NOT
// from the field's buf.validate annotation: no protovalidate interceptor is
// installed on any server path in this repo, so the shipped annotations are
// inert at RPC runtime and the annotation on section_id is schema documentation
// only. Asserting the annotation produced this refusal would assert a mechanism
// that does not exist.
func TestABlankSectionIDIsRefusedByTheInterceptorBeforeTheHandlerRuns(t *testing.T) {
	for name, id := range map[string]string{"empty": "", "whitespace only": "   "} {
		t.Run(name, func(t *testing.T) {
			got := callGetSection(t, adminPlayerULID(), []string{"admin"}, id)

			require.Error(t, got.err)
			require.False(t, got.handlerRan,
				"a blank section id MUST be refused BEFORE the handler body runs")
			errutil.AssertErrorCode(t, got.err, "ADMIN_SECTION_NO_SECTION_ID")
			require.Equal(t, codes.PermissionDenied, status.Code(got.err))
			require.Equal(t, adminDeniedMessage, status.Convert(got.err).Message())
		})
	}
}

// TestTheAdminGetSectionHandlerReadsTheSectionTheInterceptorResolved pins that
// the handler is a PROJECTION: it reads the already-gated entry off the context
// and computes nothing.
//
// A handler that resolved the section itself would either re-run the gate (the
// per-handler exception D-99 abolished) or look the id up in the registry (the
// enumeration oracle D-06 closed) — on the ONE RPC whose section id is
// attacker-controlled.
func TestTheAdminGetSectionHandlerReadsTheSectionTheInterceptorResolved(t *testing.T) {
	server := NewAdminPortalServer(seedEngineFor(t, adminPlayerULID(), "admin"))

	// No interceptor ran, so nothing stashed a section: the handler MUST fail
	// closed rather than fall back to a lookup of its own.
	_, err := server.AdminGetSection(t.Context(), &adminportalv1.AdminGetSectionRequest{
		SectionId: "characters",
	})

	require.Error(t, err, "with no gated section on the context the handler MUST NOT answer")
	require.Equal(t, codes.Internal, status.Code(err))
}
