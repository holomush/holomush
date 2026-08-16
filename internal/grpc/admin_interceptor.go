// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"
	"strings"

	"github.com/samber/oops"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/admin/section"
	"github.com/holomush/holomush/internal/auth"
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

// adminSectionIDCarrier is the typed accessor the SectionFromRequest arm reads
// the caller-supplied section id through, mirroring [adminTokenCarrier]:
// protoc-gen-go emits GetSectionId() on every message carrying a `section_id`
// field.
//
// A request type that does NOT satisfy it is REFUSED
// (ADMIN_SECTION_NO_SECTION_ID), never defaulted to a section — so a request
// message that drops the field denies rather than escaping the gate.
type adminSectionIDCarrier interface{ GetSectionId() string }

// adminSectionContextKey is the unexported key the GATED section is stashed
// under. Unexported so nothing outside this package can forge one onto a
// context; reads go through [AdminSectionFromContext].
type adminSectionContextKey struct{}

// AdminSectionFromContext returns the registry entry the admin interceptor
// already resolved for this request, after AssertSectionAccess permitted it.
//
// It exists so a handler downstream of a passed gate PROJECTS the entry rather
// than resolving it again. A second resolution would be either a second gate
// call (the per-handler exception D-99 abolished) or a bare registry lookup
// (the enumeration oracle D-06 closed) — on the one RPC whose section id is
// attacker-controlled.
//
// ok is false when no admin interceptor ran, or when the method's descriptor
// resolves no single section (an EnumeratesAllSections method has none). The
// returned Section is the zero value in that case, and a caller MUST treat it as
// absence: a zero Section is an unauthorized entry wearing an authorized shape.
func AdminSectionFromContext(ctx context.Context) (section.Section, bool) {
	s, ok := ctx.Value(adminSectionContextKey{}).(section.Section)
	if !ok || s.ID == "" {
		return section.Section{}, false
	}
	return s, true
}

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
// Then the gate itself, over the descriptor's SHAPE — THERE IS NO UNGATED ARM,
// and the switch is exhaustive with a DENYING default:
//
//   - EnumeratesAllSections (AdminListSections) →
//     section.AssertSectionAdmission against section.PortalProbeSectionID. The
//     interceptor still chooses no SECTION — admission is the resource-TYPE
//     answer and the probe selects no scope — but a caller with no
//     `admin_section:` access is refused HERE, before the handler runs. That
//     distinction is load-bearing: an AdminListSections that could not deny
//     would turn the portal's denial proof into an assertion about an empty 200.
//   - SectionFromRequest (AdminGetSection) → the id is read off the request
//     through a typed GetSectionId() assertion; a missing accessor or a blank id
//     is refused with ADMIN_SECTION_NO_SECTION_ID; otherwise
//     section.AssertSectionAccess runs against the CALLER-SUPPLIED id and the
//     resolved entry is stashed for the handler to project. This arm exists
//     because falling through to the fixed-SectionID arm would gate on an EMPTY
//     id, which gate.go refuses before evaluation — failing every such call.
//   - A fixed SectionID → section.AssertSectionAccess, the full check including
//     registration, descriptor consistency and availability.
//   - ANYTHING ELSE → ADMIN_SECTION_NOT_DECLARED. A fourth shape added later
//     denies rather than acquiring whichever arm happened to be reachable.
//
// Every refusal is translated by [mapAdminSectionError] — at THIS layer only
// (.claude/rules/grpc-errors.md) — and no inner error is ever formatted into the
// status message.
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

		// The gate. Every shape is gated and the default DENIES; no arm passes
		// through ungated.
		var (
			gateErr  error
			resolved section.Section
		)
		switch {
		case d.EnumeratesAllSections:
			gateErr = section.AssertSectionAdmission(ctx, deps.Engine, playerID,
				string(section.PortalProbeSectionID), d.Action)

		case d.SectionFromRequest:
			idCarrier, carriesID := req.(adminSectionIDCarrier)
			if !carriesID {
				return nil, adminDeny(ctx, "ADMIN_SECTION_NO_SECTION_ID",
					"admin portal request declares a section from the request but carries no section_id accessor",
					"method", info.FullMethod)
			}
			// TrimSpace decides only whether the id is BLANK. The gate is then
			// called with the RAW value: trimming the id we authorize would be a
			// normalization, and §10.1 matching is exact byte equality — a
			// normalized near-miss could resolve to a neighbouring section.
			rawID := idCarrier.GetSectionId()
			if strings.TrimSpace(rawID) == "" {
				return nil, adminDeny(ctx, "ADMIN_SECTION_NO_SECTION_ID",
					"admin portal request carries a blank section_id",
					"method", info.FullMethod)
			}
			resolved, gateErr = section.AssertSectionAccess(ctx, deps.Engine, playerID, rawID, d.Action)

		case d.SectionID != "":
			resolved, gateErr = section.AssertSectionAccess(ctx, deps.Engine, playerID, d.SectionID, d.Action)

		default:
			// A descriptor carrying no recognised shape. validateAdminDescriptors
			// refuses to let one exist at boot; this arm is the second line of
			// defence, so a FOURTH shape added later denies rather than acquiring
			// whichever arm happened to be reachable.
			return nil, adminDeny(ctx, "ADMIN_SECTION_NOT_DECLARED",
				"admin portal method descriptor carries no recognised section shape",
				"method", info.FullMethod)
		}
		if gateErr != nil {
			return nil, adminGateFailure(ctx, gateErr, info.FullMethod, playerID)
		}

		gatedCtx := context.WithValue(ctx, adminPlayerContextKey{}, playerSession)
		if resolved.ID != "" {
			gatedCtx = context.WithValue(gatedCtx, adminSectionContextKey{}, resolved)
		}
		return handler(gatedCtx, req)
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

// adminDeny builds one refusal for an arm the INTERCEPTOR itself decided: the
// typed oops code for in-process assertion, and the distinguishing fields for
// the log and nowhere else.
//
// The oops error is constructed ONCE here and is never re-wrapped. That
// single-wrap property is asserted as unwrap-chain DEPTH rather than by code,
// because oops's Code() resolves the DEEPEST code in a chain — under a double
// wrap it would agree with itself and disagree with the truth.
//
// The wire mapping is [mapAdminSectionError]'s, not this function's: boundary
// translation happens at exactly one layer.
func adminDeny(ctx context.Context, code, why string, logAttrs ...any) error {
	return mapAdminSectionError(ctx, oops.Code(code).Errorf("%s", why),
		append([]any{"reason", why}, logAttrs...)...)
}

// adminGateFailure maps a section-GATE refusal onto the wire, preserving the
// gate's OWN typed error rather than minting a second one — so the code an
// in-process assertion reads is the code the gate actually produced, and the
// §10.3 planned-section refusal keeps its own status class.
func adminGateFailure(ctx context.Context, gateErr error, fullMethod, playerID string) error {
	return mapAdminSectionError(ctx, gateErr, "method", fullMethod, "player_id", playerID)
}
