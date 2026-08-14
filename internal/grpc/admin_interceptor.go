// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"
	"strings"

	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/admin/section"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/pkg/errutil"
)

// adminPortalServicePrefix is the gRPC FullMethod prefix this interceptor
// gates: every method of holomush.adminportal.v1.AdminPortalService, the
// player-session admin portal surface.
//
// What it deliberately does NOT cover: `/holomush.admin.v1.` — the break-glass
// OPERATOR control plane (Status, Authenticate, Approve, ResetTOTP, Rekey*,
// AdminReadStream). That service is served over a UNIX domain socket by
// internal/admin/socket/ under AssertOperatorAdmin and is never mounted on this
// server, so it is out of scope BY CONSTRUCTION rather than by omission. Fusing
// the two under one prefix would put a player-session ABAC gate and an operator
// break-glass surface behind one descriptor table — weakening exactly the
// property this prefix gate exists to hold.
const adminPortalServicePrefix = "/holomush.adminportal.v1."

// adminDeniedMessage is the ONE message every admin-portal refusal carries over
// the wire, whichever arm produced it.
//
// It is a package constant with no formatting verb, so the arm a caller tripped
// — no admin role, an undeclared method, a missing subject, a misconfigured
// gate — is invisible to them. Per .claude/rules/grpc-errors.md, a
// distinguishing field substituted into a status message reaches the client;
// here that field IS the disclosure, so every one of them goes to
// slog.WarnContext instead.
const adminDeniedMessage = "admin section access denied"

// adminPlayerContextKey is the unexported key the resolved player is stashed
// under. Unexported so nothing outside this package can forge one onto a
// context; reads go through [AdminPlayerFromContext].
type adminPlayerContextKey struct{}

// AdminPlayerFromContext returns the player session the admin interceptor
// already resolved for this request.
//
// It exists so a handler downstream of a passed gate reads the subject rather
// than resolving it a SECOND time — a double lookup per request that also opens
// the possibility of the two lookups disagreeing.
//
// ok is false when no admin interceptor ran, and the returned session is nil in
// that case. There is deliberately no zero-valued fallback: a zero PlayerSession
// is an unauthenticated caller wearing an authenticated shape.
func AdminPlayerFromContext(ctx context.Context) (*auth.PlayerSession, bool) {
	ps, ok := ctx.Value(adminPlayerContextKey{}).(*auth.PlayerSession)
	if !ok || ps == nil {
		return nil, false
	}
	return ps, true
}

// AdminInterceptorDeps are the two dependencies the gate needs. Both are
// REQUIRED: a nil either one fails the whole portal closed rather than passing
// requests through ungated.
type AdminInterceptorDeps struct {
	// Engine is the ABAC engine seed:admin-section-access is evaluated against.
	Engine section.PolicyEvaluator
	// SessionRepo resolves the request's bearer token to a player.
	SessionRepo auth.PlayerSessionRepository
}

// adminTokenCarrier is the single interface assertion that resolves a subject
// for EVERY admin method with no per-method configuration: protoc-gen-go emits
// GetPlayerSessionToken() on every message carrying a `player_session_token`
// field, and every AdminPortalService request declares one.
//
// A request type that does NOT satisfy it is REFUSED (ADMIN_SECTION_NO_SUBJECT),
// never passed through — so a future request message that drops the field
// denies rather than escaping the gate.
type adminTokenCarrier interface{ GetPlayerSessionToken() string }

// NewAdminSectionInterceptor builds the admin-portal unary gate: the ONE place
// every holomush.adminportal.v1 method is authorized (D-99).
//
// # Why an interceptor and a table rather than a call in each handler
//
// A per-handler assertion is a thing a future author must remember, and the one
// they forget ships ungated with nothing red. Here, authorization is driven by
// section.AdminDescriptors — a DECLARATION — and a method with no entry is
// refused. Forgetting denies.
//
// # The arms, in evaluation order
//
//  1. MISCONFIGURED (nil Engine or nil SessionRepo) → every in-prefix method is
//     refused with ADMIN_SECTION_GATE_UNAVAILABLE. Out-of-prefix still passes
//     through, so the refusal is scoped to the portal rather than a blanket
//     outage.
//  2. OUT OF PREFIX → handler(ctx, req), untouched.
//  3. NO DESCRIPTOR → ADMIN_SECTION_NOT_DECLARED. This runs BEFORE any session
//     work, so an undeclared method cannot be defaulted into a section by a
//     caller who happens to hold a valid token.
//  4. NO SUBJECT ACCESSOR, or an unresolvable session → ADMIN_SECTION_NO_SUBJECT.
//     The resolver's own error is logged, never returned.
//
// Then the gate itself, on both descriptor shapes — THERE IS NO UNGATED ARM:
//
//   - A descriptor naming a fixed section → section.AssertSectionAccess, the
//     full check including registration, descriptor consistency and
//     availability.
//   - A descriptor carrying EnumeratesAllSections (AdminListSections) →
//     section.AssertSectionAdmission against section.PortalProbeSectionID. The
//     interceptor still chooses no SECTION — admission is the resource-TYPE
//     answer and the probe selects no scope — but a caller with no
//     `admin_section:` access is refused HERE, before the handler runs. That
//     distinction is load-bearing: an AdminListSections that could not deny
//     would turn the portal's denial proof into an assertion about an empty 200.
//
// Every refusal maps to codes.PermissionDenied with [adminDeniedMessage].
// Translation happens at THIS layer only (.claude/rules/grpc-errors.md); no
// inner error is ever formatted into the status message.
func NewAdminSectionInterceptor(deps AdminInterceptorDeps) grpc.UnaryServerInterceptor {
	if deps.Engine == nil || deps.SessionRepo == nil {
		return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			if strings.HasPrefix(info.FullMethod, adminPortalServicePrefix) {
				return nil, adminDeny(ctx, "ADMIN_SECTION_GATE_UNAVAILABLE",
					"admin section gate misconfigured: a dependency is nil",
					"method", info.FullMethod)
			}
			return handler(ctx, req)
		}
	}

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !strings.HasPrefix(info.FullMethod, adminPortalServicePrefix) {
			return handler(ctx, req)
		}

		// Arm 3 — declaration, BEFORE any session work.
		d, declared := section.LookupMethodDescriptor(bareAdminMethod(info.FullMethod))
		if !declared {
			return nil, adminDeny(ctx, "ADMIN_SECTION_NOT_DECLARED",
				"admin portal method has no method→section declaration",
				"method", info.FullMethod)
		}

		// Arm 4 — subject.
		carrier, carries := req.(adminTokenCarrier)
		if !carries {
			return nil, adminDeny(ctx, "ADMIN_SECTION_NO_SUBJECT",
				"admin portal request carries no player_session_token accessor",
				"method", info.FullMethod)
		}
		playerSession, resolveErr := resolvePlayerSessionWithRepo(ctx, deps.SessionRepo, carrier.GetPlayerSessionToken())
		if resolveErr != nil {
			// The resolver's inner error is LOGGED, never wrapped into the
			// refusal: it distinguishes "no such session" from "expired", which
			// is a probe a denied caller must not have.
			slog.WarnContext(ctx, "admin section: session resolution failed",
				"method", info.FullMethod, "error", resolveErr.Error())
			return nil, adminDeny(ctx, "ADMIN_SECTION_NO_SUBJECT",
				"admin portal request could not be resolved to a player",
				"method", info.FullMethod)
		}
		playerID := playerSession.PlayerID.String()

		// The gate. Both shapes are gated; neither passes through.
		var gateErr error
		if d.EnumeratesAllSections {
			gateErr = section.AssertSectionAdmission(ctx, deps.Engine, playerID,
				string(section.PortalProbeSectionID), d.Action)
		} else {
			_, gateErr = section.AssertSectionAccess(ctx, deps.Engine, playerID, d.SectionID, d.Action)
		}
		if gateErr != nil {
			return nil, adminGateFailure(ctx, gateErr, info.FullMethod, playerID)
		}

		return handler(context.WithValue(ctx, adminPlayerContextKey{}, playerSession), req)
	}
}

// bareAdminMethod extracts the method component of a gRPC FullMethod. A path
// carrying none resolves to "", which matches no descriptor and therefore
// denies.
func bareAdminMethod(fullMethod string) string {
	i := strings.LastIndex(fullMethod, "/")
	if i < 0 || i == len(fullMethod)-1 {
		return ""
	}
	return fullMethod[i+1:]
}

// adminRefusal carries BOTH halves of a refusal in one value, because the two
// layers assert two different things about the same error and neither may be
// sacrificed to the other:
//
//   - Over the WIRE it is a gRPC status: codes.PermissionDenied carrying
//     [adminDeniedMessage] verbatim. GRPCStatus() is satisfied DIRECTLY (value
//     receiver, no wrapper), which matters: status.FromError returns a wrapped
//     error's status with p.Message REPLACED by the outer error's full text, so
//     an `oops.Wrap(statusErr)` shape would silently leak the typed code into
//     the message the client reads.
//   - IN PROCESS it unwraps to the typed oops error, so
//     errutil.AssertErrorCode can read the code at the interceptor — the one
//     place an oops value still exists, since none survives a round trip.
//
// Error() deliberately returns the STATIC message, not the oops text: any
// caller that does wrap this value still cannot turn the code into wire output.
type adminRefusal struct {
	st  *status.Status
	err error
}

func (r adminRefusal) Error() string              { return r.st.Message() }
func (r adminRefusal) GRPCStatus() *status.Status { return r.st }
func (r adminRefusal) Unwrap() error              { return r.err }

// adminDeny builds one refusal: the typed oops code for in-process assertion,
// the static status message for the wire, and the distinguishing fields for the
// log and nowhere else.
//
// The oops error is constructed ONCE here and is never re-wrapped. That
// single-wrap property is asserted as unwrap-chain DEPTH rather than by code,
// because oops's Code() resolves the DEEPEST code in a chain — under a double
// wrap it would agree with itself and disagree with the truth.
func adminDeny(ctx context.Context, code, why string, logAttrs ...any) error {
	slog.WarnContext(ctx, "admin section refused a portal call", append([]any{"reason", why}, logAttrs...)...)
	return adminRefusal{
		st:  status.New(codes.PermissionDenied, adminDeniedMessage),
		err: oops.Code(code).Errorf("%s", why),
	}
}

// adminGateFailure maps a section-gate refusal onto the wire, preserving the
// gate's OWN typed error rather than minting a second one — so the code an
// in-process assertion reads is the code the gate actually produced.
//
// An EVALUATION FAILURE is the one refusal that is NOT PermissionDenied: §8.10
// forbids rendering an ABAC outage as an authorization answer, because an
// operator reading "denied" would look for a policy problem that does not
// exist. It becomes codes.Internal with its own static message.
func adminGateFailure(ctx context.Context, gateErr error, fullMethod, playerID string) error {
	if oopsErr, isOops := oops.AsOops(gateErr); isOops {
		if code, isString := oopsErr.Code().(string); isString && code == "ADMIN_SECTION_EVALUATION_FAILED" {
			errutil.LogErrorContext(ctx, "admin section: gate evaluation failed", gateErr,
				"method", fullMethod, "player_id", playerID)
			return status.Errorf(codes.Internal, "internal error")
		}
	}
	slog.WarnContext(ctx, "admin section: gate refused the caller",
		"method", fullMethod, "player_id", playerID)
	return adminRefusal{
		st:  status.New(codes.PermissionDenied, adminDeniedMessage),
		err: gateErr,
	}
}
