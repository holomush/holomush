// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"

	"github.com/holomush/holomush/internal/access"
)

// systemSubject is the bare ABAC subject the policy engine's S1 system-bypass
// gate compares against. internal/access/policy/engine.go:92 tests EXACT string
// equality, not a prefix — so "system:bootstrap" is a normally policy-evaluated
// subject and never reaches the bypass branch.
const systemSubject = "system"

// Caller is the execution identity a world.Service command is invoked on behalf
// of. It replaces the bare subject string that every command used to take.
//
// Every field is unexported and there are exactly two exported constructors, so
// no code outside package world can build a Caller carrying arbitrary
// attributes. The type is modelled on
// internal/access/policy/types.Decision: unexported fields, a constructor that
// derives the dependent field, value receivers.
//
// THE ZERO VALUE IS INERT, NOT PRIVILEGED. Because every field is unexported
// rather than the struct itself being unconstructable, `world.Caller{}` is legal
// from any package. That is deliberate and safe: the zero value carries an empty
// subject and no system marker, so it fails closed at checkAccess —
// types.NewAccessRequest rejects an empty subject, and the command returns
// *_ACCESS_EVALUATION_FAILED. The hatch is closed by fail-closed evaluation, not
// by a construction guard.
//
// Attribute channel: attrs is forwarded to types.NewAccessRequest's 4th
// parameter, where it lands in the policy engine's bags.Action overlay and is
// readable from the DSL as action.<key>. Neither exported constructor populates
// it in 02.1 — the vocabulary is Phase 02.2's.
type Caller struct {
	// subject is the ABAC subject string, carried VERBATIM. It is also the
	// world-change outbox envelope Actor (see buildIntent / buildMoveIntent),
	// so its byte identity is an audit requirement, not only an authz one.
	subject string
	// system marks a caller that requests the S1 system bypass. Only
	// SystemCaller sets it, and it is the only mechanism by which a caller
	// VALUE can influence the evaluation CONTEXT (see evalContext).
	system bool
	// attrs is the per-call attribute channel. nil for both exported
	// constructors; populated only by same-package composite literal.
	attrs map[string]any
}

// HumanCaller wraps an already-built ABAC subject string as a Caller. It never
// sets the system flag, and it never re-derives or re-prefixes the string it is
// handed — the argument is passed through verbatim, because that same string
// becomes the world-change outbox envelope Actor.
//
// EMPTY SUBJECT: this constructor deliberately does NOT follow the
// panic-on-empty convention that access.PluginSubject, access.CharacterSubject
// and access.PlayerSubject share (internal/access/prefix.go:97, :107, :121).
// (access.ViewerSubject is a different shape and is not part of that
// convention: it panics on a NON-empty playerID for the anonymous rung, where an
// empty playerID is the correct call, and on an empty one only for the two
// non-anonymous rungs.)
//
// The deviation is intentional. The fail-closed guard already lives one layer
// down: types.NewAccessRequest rejects an empty subject
// (internal/access/policy/types/types.go:144-146), and checkAccess classifies
// that rejection as *_ACCESS_EVALUATION_FAILED. Panicking here would instead
// turn nine currently-green subtests of TestWorldService_MalformedAccessParams
// — all nine of which supply an empty subject — into panics.
func HumanCaller(subjectID string) Caller {
	return Caller{subject: subjectID}
}

// SystemCaller returns the caller for a server-internal operation that is not on
// behalf of any character. It takes no parameters and derives both the bare
// subject and the system flag itself, so a system caller cannot be
// misconstructed.
//
// Satisfying the S1 double-gate is the whole point: the engine requires BOTH
// req.Subject == "system" AND access.IsSystemContext(ctx)
// (internal/access/policy/engine.go:92-93). This value supplies the first
// directly and the second via evalContext, so call sites no longer need to stamp
// access.WithSystemSubject themselves. A bare "system" subject WITHOUT the
// context marker is a hard SYSTEM_SUBJECT_REJECTED, which is why a system call
// site must never be spelled HumanCaller("system").
func SystemCaller() Caller {
	return Caller{subject: systemSubject, system: true}
}

// evalContext returns the context to hand to Engine.Evaluate: the input context
// unchanged for a non-system caller, and a derived context carrying the system
// marker for a system caller.
//
// The input context is never mutated and never reassigned by callers of this
// method. checkAccess passes the derived context ONLY to Engine.Evaluate, so the
// marker cannot outlive the authorization decision and cannot reach repositories
// or the outbox.
func (c Caller) evalContext(ctx context.Context) context.Context {
	if !c.system {
		return ctx
	}
	return access.WithSystemSubject(ctx)
}
