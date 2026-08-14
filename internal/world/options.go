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

// AuditContextOption is the option WithAuditContext returns. It is valid
// wherever an admin-evaluated section and action belong.
type AuditContextOption interface {
	LifecycleOption
}

type auditContextOption struct{ ctx AuditContext }

func (o auditContextOption) applyLifecycle(opts *lifecycleOptions) { opts.audit = o.ctx }

// WithAuditContext supplies the admin section and action a call was gated
// under, for the command's envelope payload (§10.7).
//
// Only the ADMIN surface supplies it. A caller that does not gets the zero
// AuditContext and therefore empty section/action in the emitted payload, which
// is the shipped player behaviour.
func WithAuditContext(ctx AuditContext) AuditContextOption {
	return auditContextOption{ctx: ctx}
}
