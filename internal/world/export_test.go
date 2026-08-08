// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import "context"

// This file is the standard Go export_test.go bridge: it exposes Caller's
// unexported state to the EXTERNAL test package (package world_test) in
// internal/world/caller_test.go, and to nothing else.
//
// WHY THE PROOFS LIVE IN AN EXTERNAL TEST PACKAGE. The criterion-2 and
// criterion-4 proofs must drive a REAL *policy.Engine, and policy.NewEngine's
// first parameter is a concrete *attribute.Resolver. But
// internal/access/policy/attribute imports internal/world (attribute/character.go,
// attribute/property.go), so an IN-PACKAGE test file (package world) importing
// it is "import cycle not allowed in test". An external test package is exempt
// from that cycle — which is also why internal/world/service_test.go is
// package world_test. Hence caller_test.go is package world_test and reaches
// Caller's unexported fields through the helpers below.
//
// THIS IS NOT AN ESCAPE HATCH FROM D-62. Symbols declared in a _test.go file
// are compiled only into package world's own test binary. No production code,
// and no other package's non-test build, can reach them. The prohibition D-62
// states — that nothing outside package world may build a caller carrying
// arbitrary attributes — continues to hold for every production-reachable path:
// world.Caller still has unexported fields, exactly two exported constructors
// (neither populating attrs), and no exported accessor in caller.go.

// SubjectForTest returns the caller's verbatim ABAC subject string.
func (c Caller) SubjectForTest() string { return c.subject }

// IsSystemForTest reports whether the caller requests the S1 system bypass.
func (c Caller) IsSystemForTest() bool { return c.system }

// AttrsForTest returns the caller's per-call attribute channel.
func (c Caller) AttrsForTest() map[string]any { return c.attrs }

// EvalContextForTest exposes the unexported context-derivation method.
func (c Caller) EvalContextForTest(ctx context.Context) context.Context {
	return c.evalContext(ctx)
}

// NewCallerWithAttrsForTest builds a Caller carrying attributes — the injection
// path that deliberately does NOT exist in production before Phase 02.2 defines
// the action vocabulary.
func NewCallerWithAttrsForTest(subject string, attrs map[string]any) Caller {
	return Caller{subject: subject, attrs: attrs}
}
