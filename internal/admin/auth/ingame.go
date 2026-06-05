// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package adminauth

import (
	"context"
	"fmt"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/totp"
)

// CredentialValidator is the narrow surface InGameCredentialsProvider
// requires from auth.Service. Decoupling for testability.
type CredentialValidator interface {
	ValidateCredentials(ctx context.Context, username, password string) (*auth.Player, error)
}

// EnrollmentChecker is the narrow surface for "is this player TOTP-enrolled?".
type EnrollmentChecker interface {
	IsEnrolled(ctx context.Context, playerID ulid.ULID) (bool, error)
	Verify(ctx context.Context, playerID ulid.ULID, code string) (totp.VerifyResult, error)
}

// InGameCredentialsProvider implements OperatorAuthProvider with the
// 6-step sequence per master spec §5.9 (amended) and design spec §4.
type InGameCredentialsProvider struct {
	creds     CredentialValidator
	totp      EnrollmentChecker
	resolver  access.SubjectResolver
	roleStore store.RoleStore
}

// NewInGameCredentialsProvider constructs a provider with the named
// dependencies. None may be nil.
func NewInGameCredentialsProvider(
	creds CredentialValidator,
	totpSvc EnrollmentChecker,
	resolver access.SubjectResolver,
	roleStore store.RoleStore,
) (*InGameCredentialsProvider, error) {
	if creds == nil {
		return nil, oops.Code("INGAME_NIL_CREDS").Errorf("CredentialValidator is required")
	}
	if totpSvc == nil {
		return nil, oops.Code("INGAME_NIL_TOTP").Errorf("EnrollmentChecker is required")
	}
	if resolver == nil {
		return nil, oops.Code("INGAME_NIL_RESOLVER").Errorf("access.SubjectResolver is required")
	}
	if roleStore == nil {
		return nil, oops.Code("INGAME_NIL_ROLESTORE").Errorf("store.RoleStore is required")
	}
	return &InGameCredentialsProvider{
		creds:     creds,
		totp:      totpSvc,
		resolver:  resolver,
		roleStore: roleStore,
	}, nil
}

// Name returns the provider name, persisted in audit metadata.
func (p *InGameCredentialsProvider) Name() string { return "ingame-creds-totp" }

// Authenticate runs the 6-step check sequence per design spec §4.
// Steps later than a failure MUST NOT execute (INV-CRYPTO-68).
func (p *InGameCredentialsProvider) Authenticate(ctx context.Context, req AuthRequest) (OperatorIdentity, error) {
	// Step 1: credentials.
	player, err := p.creds.ValidateCredentials(ctx, req.Username, req.Password)
	if err != nil {
		return OperatorIdentity{}, oops.Code("DENY_INVALID_CREDENTIALS").
			With("username", req.Username).Wrap(err)
	}

	// Step 2: TOTP enrolled?
	enrolled, err := p.totp.IsEnrolled(ctx, player.ID)
	if err != nil {
		return OperatorIdentity{}, oops.Code("INGAME_TOTP_LOOKUP_FAILED").
			With("player_id", player.ID.String()).Wrap(err)
	}
	if !enrolled {
		return OperatorIdentity{}, oops.Code("DENY_NOT_ENROLLED").
			With("player_id", player.ID.String()).
			Errorf("player has not enrolled TOTP")
	}

	// Step 3: TOTP verify.
	res, err := p.totp.Verify(ctx, player.ID, req.TOTPCode)
	if err != nil {
		return OperatorIdentity{}, oops.Code("INGAME_TOTP_VERIFY_FAILED").
			With("player_id", player.ID.String()).Wrap(err)
	}
	switch res.Outcome {
	case totp.OutcomeOK:
		// Continue.
	case totp.OutcomeLocked:
		op := oops.Code("DENY_LOCKED").
			With("player_id", player.ID.String())
		if res.LockedUntil != nil {
			op = op.With("locked_until", *res.LockedUntil)
		}
		return OperatorIdentity{}, op.Errorf("player TOTP is locked")
	default:
		return OperatorIdentity{}, oops.Code("DENY_BAD_TOTP").
			With("player_id", player.ID.String()).
			With("outcome", fmt.Sprintf("%d", res.Outcome)).
			Errorf("TOTP verify failed")
	}

	// Steps 4-5: capability allow-list + RoleAdmin (any character).
	// Both gates are re-asserted at every admin RPC entry point per
	// INV-CRYPTO-83; the shared helper keeps the three sites in lockstep.
	if err := AssertOperatorAdmin(ctx, p.resolver, p.roleStore, player.ID.String()); err != nil {
		return OperatorIdentity{}, err
	}

	// Step 6: PeerCred capture (audit only). The struct is stored as-is;
	// the audit string is formatted at serialization time via
	// OperatorIdentity.PeerCredString() so we don't depend on os/user
	// resolution inside the auth path.
	return OperatorIdentity{
		PlayerID:         player.ID.String(),
		PeerCred:         req.PeerCred, // {UID, GID, PID} struct
		TOTPVerified:     true,
		AuthProviderName: p.Name(),
	}, nil
}

// Compile-time assertion: InGameCredentialsProvider is an OperatorAuthProvider.
var _ OperatorAuthProvider = (*InGameCredentialsProvider)(nil)
