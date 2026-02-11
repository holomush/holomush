// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

import "context"

type systemSubjectKey struct{}

// WithSystemSubject returns a context marked as a system-level operation,
// which bypasses normal access control checks.
func WithSystemSubject(ctx context.Context) context.Context {
	return context.WithValue(ctx, systemSubjectKey{}, true)
}

// IsSystemContext reports whether the context was marked as a system operation
// via WithSystemSubject.
func IsSystemContext(ctx context.Context) bool {
	v, ok := ctx.Value(systemSubjectKey{}).(bool)
	return ok && v
}
