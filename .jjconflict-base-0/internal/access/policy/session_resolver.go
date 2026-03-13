// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import "context"

// SessionResolver resolves session: subjects to character: subjects.
// Implementations query the session store to map session IDs to character IDs.
type SessionResolver interface {
	// ResolveSession returns the character ID associated with a session ID.
	// Returns an error with code "SESSION_INVALID" if the session does not exist or is expired.
	// Returns other errors for store/infrastructure failures.
	ResolveSession(ctx context.Context, sessionID string) (characterID string, err error)
}
