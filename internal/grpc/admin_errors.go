// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"

	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/pkg/errutil"
)

// adminDeniedMessage is the ONE message every admin-portal refusal carries over
// the wire, whichever arm produced it.
//
// It is a package constant with no formatting verb, so the arm a caller tripped
// — no admin role, an undeclared method, a missing subject, a blank section id,
// an unregistered section, a misconfigured gate — is invisible to them. Per
// .claude/rules/grpc-errors.md, a distinguishing field substituted into a status
// message reaches the client; here that field IS the disclosure, so every one of
// them goes to slog.WarnContext instead.
const adminDeniedMessage = "admin section access denied"

// sectionNotImplementedMessage is the §10.3 refusal for a section that is
// registered and permitted but has no handler yet.
//
// It is a SEPARATE, equally static message under a DIFFERENT code
// (codes.FailedPrecondition) because it answers a different question from the
// denial: the caller may reach the admin surface and this section exists, it is
// simply not built. It is reachable ONLY by a caller the gate already permitted
// — a denied caller receives [adminDeniedMessage] for a planned section exactly
// as for any other — so distinguishing it discloses nothing a permitted caller
// could not learn from AdminListSections.
//
// It carries no section id and no response body. Plan 06.1-02's planned-section
// screen therefore renders from the already-authorized AdminListSections layout
// data rather than from this refusal.
const sectionNotImplementedMessage = "admin section is not implemented"

// adminRefusal carries BOTH halves of a refusal in one value, because the two
// layers assert two different things about the same error and neither may be
// sacrificed to the other:
//
//   - Over the WIRE it is a gRPC status carrying a static message verbatim.
//     GRPCStatus() is satisfied DIRECTLY (value receiver, no wrapper), which
//     matters: status.FromError returns a wrapped error's status with p.Message
//     REPLACED by the outer error's full text, so an `oops.Wrap(statusErr)` shape
//     would silently leak the typed code into the message the client reads.
//   - IN PROCESS it unwraps to the typed oops error, so errutil.AssertErrorCode
//     can read the code at the interceptor — the one place an oops value still
//     exists, since none survives a round trip.
//
// Error() deliberately returns the STATIC message, not the oops text: any caller
// that does wrap this value still cannot turn the code into wire output.
type adminRefusal struct {
	st  *status.Status
	err error
}

func (r adminRefusal) Error() string              { return r.st.Message() }
func (r adminRefusal) GRPCStatus() *status.Status { return r.st }
func (r adminRefusal) Unwrap() error              { return r.err }

// mapAdminSectionError is the ONE place an admin-portal error crosses the gRPC
// boundary, for the interceptor and for every admin handler.
//
// Translating at exactly one layer is not tidiness: .claude/rules/grpc-errors.md
// records that a double translation breaks status.FromError chain-walking,
// because the inner conversion produces a fresh error with no GRPCStatus method
// and the outer code then survives in place of the real one.
//
// The mapping, and why each arm is what it is:
//
//   - ADMIN_SECTION_EVALUATION_FAILED → codes.Internal with a static "internal
//     error". §8.10 forbids rendering an ABAC outage as an authorization answer:
//     an operator reading "denied" would hunt a policy bug that does not exist.
//     The real error goes to errutil.LogErrorContext and nowhere else.
//   - SECTION_NOT_IMPLEMENTED → codes.FailedPrecondition with
//     [sectionNotImplementedMessage]. Reachable only AFTER the gate permitted.
//   - EVERYTHING ELSE → codes.PermissionDenied with [adminDeniedMessage]. That
//     includes DENY_ADMIN_SECTION, DENY_ADMIN_SECTION_UNREGISTERED and
//     ADMIN_SECTION_NO_SECTION_ID, which are deliberately indistinguishable on
//     the wire: telling a caller their id was merely unregistered would hand any
//     permitted caller a registry-probing oracle (T-06-09), and it is the
//     default arm rather than an enumeration so a code added later denies
//     instead of escaping the mapping.
//
// No inner error is ever formatted into a status message; logAttrs are the
// distinguishing fields, and they go to the log only.
func mapAdminSectionError(ctx context.Context, err error, logAttrs ...any) error {
	switch adminSectionCode(err) {
	case "ADMIN_SECTION_EVALUATION_FAILED":
		errutil.LogErrorContext(ctx, "admin section: gate evaluation failed", err, logAttrs...)
		return status.Errorf(codes.Internal, "internal error")
	case "SECTION_NOT_IMPLEMENTED":
		slog.WarnContext(ctx, "admin section: refused a planned section after the gate permitted", logAttrs...)
		return adminRefusal{
			st:  status.New(codes.FailedPrecondition, sectionNotImplementedMessage),
			err: err,
		}
	default:
		slog.WarnContext(ctx, "admin section refused a portal call", logAttrs...)
		return adminRefusal{
			st:  status.New(codes.PermissionDenied, adminDeniedMessage),
			err: err,
		}
	}
}

// adminSectionCode reads the typed oops code off an error, or "" when it carries
// none.
//
// oops resolves the DEEPEST code in a chain, which is correct here precisely
// because every error this mapper receives is constructed once and never
// re-wrapped (asserted as unwrap-chain depth by
// TestTheInterceptorRefusalIsWrappedExactlyOnce).
func adminSectionCode(err error) string {
	oopsErr, isOops := oops.AsOops(err)
	if !isOops {
		return ""
	}
	code, isString := oopsErr.Code().(string)
	if !isString {
		return ""
	}
	return code
}
