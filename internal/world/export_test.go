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
// world.Caller still has unexported fields, no exported accessor in caller.go,
// and no exported constructor that accepts an attribute map (JobCaller derives
// its three keys from a typed Provenance).

// MapWorldErrorForTest exposes grpc_server.go's unexported oops-code → gRPC
// status classifier to the external test package.
//
// The seam exists because the classification of a JOB_-qualified deny code
// cannot be driven through the gRPC surface: every GRPCServer method builds a
// HumanCaller from the request's subject_id, so no request shape can produce a
// job principal. Asserting the suffix rule by re-implementing strings.HasSuffix
// in a test would prove only that the test agrees with itself, so the real
// mapper is called instead.
func MapWorldErrorForTest(err error) error { return mapWorldError(err) }

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

// NewCallerWithAttrsForTest builds a Caller carrying an ARBITRARY attribute map
// — an injection path that deliberately does not exist in production. Phase
// 02.2 defined the action vocabulary, but it did so through JobCaller's typed
// Provenance: no exported constructor takes a caller-chosen map, so this helper
// remains test-only.
func NewCallerWithAttrsForTest(subject string, attrs map[string]any) Caller {
	return Caller{subject: subject, attrs: attrs}
}
