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
// Every field is unexported and every exported constructor derives its own
// state from typed arguments, so no code outside package world can build a
// Caller carrying arbitrary attributes. The type is modelled on
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
// readable from the DSL as action.<key>. HumanCaller and SystemCaller leave it
// nil; JobCaller is the one constructor that populates it, and it builds the
// keys itself from a typed Provenance — no constructor accepts a caller-chosen
// attribute map (02.2-CONTEXT D-56, D-57).
type Caller struct {
	// subject is the ABAC subject string, carried VERBATIM. It is also the
	// world-change outbox envelope Actor (see buildIntent / buildMoveIntent),
	// so its byte identity is an audit requirement, not only an authz one.
	subject string
	// system marks a caller that requests the S1 system bypass. Only
	// SystemCaller sets it, and it is the only mechanism by which a caller
	// VALUE can influence the evaluation CONTEXT (see evalContext).
	system bool
	// attrs is the per-call attribute channel. nil for HumanCaller and
	// SystemCaller; populated by JobCaller from a typed Provenance.
	attrs map[string]any
	// principalKind names which principal namespace this caller acts in. The
	// ZERO VALUE is the pre-existing human/system behavior, unchanged — only
	// the job constructors set it. It exists so a deny signal can compose the
	// principal kind with the resource kind (D-58, plan 02).
	principalKind principalKind
}

// principalKind is the principal-namespace axis of a Caller. It is unexported
// and its values are unexported: nothing outside package world may assert a
// principal kind.
type principalKind string

// principalKindJob marks a caller acting as a background job (subject
// "job:<name>").
const principalKindJob principalKind = "JOB"

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

// Provenance is the per-execution vocabulary of an event-driven background job:
// the provenance triple of the event that caused this execution, taken VERBATIM
// from the delivered message (02.2-CONTEXT D-54).
//
// It carries exactly three fields and deliberately no more. JetStream delivery
// metadata (stream sequence, redelivery count) was considered and rejected: no
// consumer needs it to authorize, and carrying it would couple the ABAC
// vocabulary to the transport.
//
// The triple is stamped at the consumer boundary BEFORE handler logic runs, and
// the handler cannot alter it (D-55). That is what keeps the authorization
// check from being tautological: the RESOURCE is whatever the handler
// independently derives, so a handler that derives the wrong aggregate is
// denied rather than corrupting it.
type Provenance struct {
	// EventID is the delivered event's ULID, verbatim.
	EventID string
	// EventType is the delivered event's type, verbatim (e.g.
	// "character_retired"). A seed binds it so a job subscribed to a broad
	// filter cannot acquire broader authority than intended.
	EventType string
	// Subject is the BARE AGGREGATE ULID the event concerns — byte-comparable
	// to the resolver-stamped bags.Resource["id"], which is the substring after
	// the first ':' of the resource ref.
	//
	// IT IS NOT A DOTTED NATS SUBJECT. A seed binds
	// `action.job.trigger_subject == resource.id`, so anything other than the
	// bare ULID (a qualified "events.<game>.character.<id>" subject, a prefixed
	// "character:<id>" ref) compares unequal and every such write silently
	// default-denies.
	Subject string
}

// JobCaller returns the caller for an EVENT-DRIVEN background job acting on the
// world. The subject is "job:<name>" and the per-call attributes are exactly the
// D-54 provenance triple, keyed under the `job.` prefix so a seed reads them as
// action.job.trigger_event_id / .trigger_event_type / .trigger_subject.
//
// THE `job.` PREFIX IS NOT STYLISTIC (D-57). It mirrors the resource.<ns>.<attr>
// convention every shipped seed uses — the DSL joins the dotted path, so
// `action.job.trigger_event_id` reads the bag key "job.trigger_event_id" — and
// it permanently avoids collision with resolver-owned action keys. The resolver
// itself writes bags.Action["name"], the very collision that made "name" a
// reserved key.
//
// THERE IS NO ATTRIBUTE-MAP PARAMETER, and there must never be one. A
// map[string]any (or a variadic option, or an exported setter) would hand every
// caller the ability to assert action.dispatch_location — the host-vouched key
// behind INV-PLUGIN-50 — through a brand-new door. The typed Provenance is the
// whole channel.
//
// EMPTY SUBJECT: unlike HumanCaller, this constructor PANICS on an empty name,
// because it derives the subject through access.JobSubject and inherits that
// function's panic-on-empty guard. The deviation is deliberate: HumanCaller
// accepts an empty subject to keep an existing fail-closed classification path
// green, whereas no call site legitimately runs a job with no name.
//
// Empty provenance fields are OMITTED from the attribute bag rather than
// written as empty strings: an empty-string sentinel is fail-OPEN in this DSL,
// since it matches any other unresolved peer (.claude/rules/abac-providers.md).
func JobCaller(name string, prov Provenance) Caller {
	attrs := make(map[string]any, 3)
	if prov.EventID != "" {
		attrs["job.trigger_event_id"] = prov.EventID
	}
	if prov.EventType != "" {
		attrs["job.trigger_event_type"] = prov.EventType
	}
	if prov.Subject != "" {
		attrs["job.trigger_subject"] = prov.Subject
	}
	if len(attrs) == 0 {
		attrs = nil
	}
	return Caller{
		subject:       access.JobSubject(name),
		attrs:         attrs,
		principalKind: principalKindJob,
	}
}

// ScheduledJobCaller returns the caller for a TIMER-DRIVEN background job. It
// carries the same "job:<name>" subject and job principal kind as JobCaller, and
// NO per-execution attributes at all.
//
// ITS AUTHORITY IS NECESSARILY COARSE, and this comment says so plainly rather
// than implying otherwise (02.2-CONTEXT D-68). A scheduled job has no
// triggering event, so there is no aggregate to scope it to: its authority is
// identity plus declared capability class, and nothing narrows it per
// execution. A synthetic tick provenance was considered and REJECTED — an empty
// or sentinel trigger_subject is fail-OPEN, and a tick window authorizes
// nothing a flusher's real authority is narrowed by, so it would look like
// instance scoping without being it.
func ScheduledJobCaller(name string) Caller {
	return Caller{
		subject:       access.JobSubject(name),
		principalKind: principalKindJob,
	}
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
