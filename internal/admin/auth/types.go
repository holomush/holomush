// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package adminauth provides the OperatorAuthProvider for sub-epic D's
// break-glass admin authentication path. See docs/superpowers/specs/
// 2026-05-09-event-payload-crypto-phase5-sub-epic-d-design.md.
package adminauth

import (
	"context"
	"fmt"
	"time"

	"github.com/holomush/holomush/internal/admin/socket"
)

// AuthRequest is the credential bundle the CLI collected via prompts and
// sends in the Authenticate RPC payload. Per spec §4. PeerCred is the
// raw struct from sub-epic C's middleware (UID/GID/PID); the formatted
// audit string is built at session-issue time via PeerCredString below.
type AuthRequest struct {
	Username string
	Password string
	TOTPCode string
	PeerCred socket.PeerCred // captured by middleware; for audit only
}

// OperatorIdentity is the audit record shape per master spec §4.6 and
// design spec §4. Stored in the SessionStore keyed by a ULID token.
//
// PeerCred is preserved as the raw struct (matching internal/admin/socket/
// peercred.go's UID/GID/PID shape). PeerCredString returns the formatted
// "uid=<n> gid=<n> pid=<n>" form for audit serialization. (Resolving UID
// to a username requires /etc/passwd lookup which we deliberately avoid:
// the audit record is a numeric kernel-provided fact, not a translated
// user-facing label.)
type OperatorIdentity struct {
	PlayerID           string          // ULID
	PeerCred           socket.PeerCred // captured by middleware; for audit only
	TOTPVerified       bool            // always true on successful Authenticate
	AuthProviderName   string          // "ingame-creds-totp"
	ProviderSpecificID string          // empty for in-game provider
}

// PeerCredString returns the audit-format string for an OperatorIdentity's
// PeerCred. Format: "uid=<n> gid=<n> pid=<n>" — chosen to match the
// fields kernel SO_PEERCRED actually returns, with no /etc/passwd lookup.
func (o OperatorIdentity) PeerCredString() string {
	return fmt.Sprintf("uid=%d gid=%d pid=%d", o.PeerCred.UID, o.PeerCred.GID, o.PeerCred.PID)
}

// OperatorAuthProvider authenticates an operator for destructive or
// information-disclosure admin operations. Pluggable like KEKProvider.
type OperatorAuthProvider interface {
	Name() string
	Authenticate(ctx context.Context, req AuthRequest) (OperatorIdentity, error)
}

// Clock abstracts time.Now for deterministic tests.
type Clock interface {
	Now() time.Time
}
