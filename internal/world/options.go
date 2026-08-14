// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

// The TYPED, VARIADIC option seams by which an ADMIN caller supplies context a
// player caller has none of.
//
// # Why variadic options and not a wider signature
//
// The admin write surface (01-SPEC §10.6, v0.13 phase-06) reuses the SAME domain
// commands the player path uses, so admin and any future player-initiated
// transition cannot diverge. But it needs to tell those commands two things a
// player caller never has: the admin section/action the call was gated under
// (§10.7), and — for the profile write — an in-world description to apply and a
// request to suppress equal-valued rewrites.
//
// Threading those through the shared signature would make every existing caller
// reason about parameters that are meaningless to it. Overloading Caller would be
// worse: Caller.subject's byte identity is an audit requirement, and widening it
// with an admin concern puts that concern on every world write in the tree.
//
// A trailing variadic option leaves every existing call site compiling
// unmodified and supplying nothing, which yields the zero AuditContext and the
// PLAYER behaviour byte-for-byte. That property is asserted, not merely claimed:
// see TestWorldServiceLifecycleWithNoOptionsLeavesTheAdminContextEmpty and the
// character-access facade's own zero-option assertion.
//
// # Interfaces rather than func types
//
// WithAuditContext is valid on BOTH the lifecycle commands and the profile
// write, and two distinct `func(*T)` option types could not share one
// constructor name. Declaring each option family as a one-method interface lets
// a single returned value satisfy both, which is what keeps the admin handler's
// call sites reading the same way at every command.

// lifecycleOptions is the resolved option set for RetireCharacter /
// UnretireCharacter. Its zero value is the player path.
type lifecycleOptions struct {
	audit AuditContext
}

// LifecycleOption configures a character lifecycle transition.
type LifecycleOption interface {
	applyLifecycle(*lifecycleOptions)
}

func resolveLifecycleOptions(opts []LifecycleOption) lifecycleOptions {
	var resolved lifecycleOptions
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt.applyLifecycle(&resolved)
	}
	return resolved
}

// profileUpdateOptions is the resolved option set for
// UpdateCharacterProfileAttributes. Its zero value is the player path.
type profileUpdateOptions struct {
	audit AuditContext
	// description is non-nil when WithDescription was supplied. A POINTER, not a
	// string: the empty description is a legal edit (clearing the column), so
	// "supplied" and "empty" must be distinguishable.
	description *string
	// skipUnchangedProperties drops a requested name whose STORED value already
	// equals the requested value from the write partition, INSIDE the
	// transaction. Admin-only — see WithSkipUnchangedProperties.
	skipUnchangedProperties bool
}

// ProfileUpdateOption configures a character profile-attribute write.
type ProfileUpdateOption interface {
	applyProfileUpdate(*profileUpdateOptions)
}

func resolveProfileUpdateOptions(opts []ProfileUpdateOption) profileUpdateOptions {
	var resolved profileUpdateOptions
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt.applyProfileUpdate(&resolved)
	}
	return resolved
}

// AuditContextOption is the option WithAuditContext returns. It is valid
// wherever an admin-evaluated section and action belong — both the lifecycle
// commands and the profile write.
type AuditContextOption interface {
	LifecycleOption
	ProfileUpdateOption
}

type auditContextOption struct{ ctx AuditContext }

func (o auditContextOption) applyLifecycle(opts *lifecycleOptions)         { opts.audit = o.ctx }
func (o auditContextOption) applyProfileUpdate(opts *profileUpdateOptions) { opts.audit = o.ctx }

type descriptionOption struct{ value string }

func (o descriptionOption) applyProfileUpdate(opts *profileUpdateOptions) {
	v := o.value
	opts.description = &v
}

// WithDescription supplies the in-world characters.description to apply in the
// SAME transaction, under the SAME single version bump, as the profile
// attributes.
//
// It exists because 01-SPEC §10.6's admin mask is `description` PLUS the twelve
// `profile.*` names, while this command's closed §7.2 name set REJECTS
// `description`. Routing the description through a SEPARATE channel is what
// keeps that closed name-set validation untouched: `description` never enters
// the attributes map.
//
// The alternative — calling UpdateCharacterDescription as a second write — is
// forbidden twice over. It would consume the caller's single expected_version
// twice, so the second write fails its own precheck and one request emits two
// envelopes in two transactions with a partial commit reachable. And it emits
// kindCharacterUpdated, whose declared payload carries a `description` STRING —
// the prose VALUE — which would write player-authored text into the RETAINED
// events_audit that D-103 forbids.
//
// An EMPTY string is a legal edit (clearing the column), which is why the
// resolved value is a pointer.
func WithDescription(v string) ProfileUpdateOption { return descriptionOption{value: v} }

type skipUnchangedPropertiesOption struct{}

func (skipUnchangedPropertiesOption) applyProfileUpdate(opts *profileUpdateOptions) {
	opts.skipUnchangedProperties = true
}

// WithSkipUnchangedProperties drops a requested attribute whose STORED value
// already equals the requested value from the write partition, inside the
// authoritative transaction.
//
// # It is ADMIN-ONLY, and that is load-bearing
//
// The shipped default REWRITES an equal-valued row UNCONDITIONALLY: the name
// lands in `updates`, so the write, the CAS and the version bump all happen
// while `changed` stays empty, and the emitted envelope carries an empty
// changed_attributes. UpdateCharacterProfileAttributes documents that as "both
// representable and honest" for the player path, where a web edit form PUTs
// every field it rendered and the user touched one.
//
// On an AUDITED cross-owner surface that is noise: an operator who opened a
// sheet and saved without editing would produce a version bump and an audit
// envelope claiming nothing. So the admin handler supplies this option and the
// player facade supplies nothing, leaving the player contract byte-identical —
// a property its own test asserts.
//
// # Why an option and not a handler precheck
//
// A pre-transaction comparison reads rows it holds no lock on, so a concurrent
// write between the comparison and the transaction makes the answer wrong in
// both directions. And it could not suppress anything anyway, because the
// equal-valued rewrite above is unconditional regardless of what the handler
// decided first. Here the comparison runs against rows read INSIDE the
// transaction and under the CAS, so it is race-safe by construction rather than
// by timing.
func WithSkipUnchangedProperties() ProfileUpdateOption { return skipUnchangedPropertiesOption{} }

// WithAuditContext supplies the admin section and action a call was gated
// under, for the command's envelope payload (§10.7).
//
// Only the ADMIN surface supplies it. A caller that does not gets the zero
// AuditContext and therefore empty section/action in the emitted payload, which
// is the shipped player behaviour.
func WithAuditContext(ctx AuditContext) AuditContextOption {
	return auditContextOption{ctx: ctx}
}
