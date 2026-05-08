// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"fmt"
	"io"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/totp"
)

// NewAdminTOTPCmd is the `holomush admin totp` parent. Subcommands cover
// host-shell TOTP enrollment + recovery flows.
func NewAdminTOTPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "totp",
		Short: "TOTP enrollment, verification, recovery (host-shell only)",
	}
	cmd.AddCommand(newAdminTOTPBootstrapEnrollCmd())
	// T14: enroll + recover land later.
	return cmd
}

func newAdminTOTPBootstrapEnrollCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap-enroll <username>",
		Short: "Once-only first-admin TOTP enrollment (host-shell only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			deps, cleanup, err := buildAdminTOTPDeps(ctx)
			if err != nil {
				return err
			}
			defer cleanup()

			username := args[0]
			playerID, err := deps.totpRepo.PlayerIDFromUsername(ctx, username)
			if err != nil {
				return oops.With("username", username).Wrap(err)
			}
			pidULID, err := ulid.Parse(playerID)
			if err != nil {
				return oops.Code("ADMIN_TOTP_PLAYER_ULID_PARSE").
					With("player_id", playerID).Wrap(err)
			}
			res, err := deps.totpSvc.BootstrapEnroll(ctx, pidULID)
			if err != nil {
				return oops.With("username", username).Wrap(err)
			}
			return printEnrollment(cmd.OutOrStdout(), username, playerID, res.Enrollment)
		},
	}
}

// printEnrollment writes the human-readable enrollment block. Recovery
// codes appear once and only once — operators MUST persist them offline.
func printEnrollment(w io.Writer, username, playerID string, enr totp.Enrollment) error {
	header := fmt.Sprintf(`TOTP enrolled for %s (player_id=%s).

Provisioning URI (scan into authenticator app):
  %s

Manual entry secret (if QR scanning unavailable):
  %s

Recovery codes — STORE THESE OFFLINE NOW (each is single-use):
`, username, playerID, enr.ProvisioningURI, formatSecretForDisplay(enr.Secret))
	if _, err := io.WriteString(w, header); err != nil {
		return oops.Code("ADMIN_TOTP_PRINT_FAILED").Wrap(err)
	}
	for i, c := range enr.RecoveryCodes {
		if _, err := fmt.Fprintf(w, "  %d.  %s\n", i+1, c); err != nil {
			return oops.Code("ADMIN_TOTP_PRINT_FAILED").Wrap(err)
		}
	}
	if _, err := io.WriteString(w, `
This output WILL NOT be shown again. Lose your authenticator and these
codes, and you may be permanently locked out of break-glass operations.

NOTE (R5 Option Y): no audit event is emitted for this host-shell
invocation. The crypto_bootstrap_state row in PG is the durable record.
See spec §"Audit events emitted" / "Emission ownership and the
host-shell-CLI gap".
`); err != nil {
		return oops.Code("ADMIN_TOTP_PRINT_FAILED").Wrap(err)
	}
	return nil
}

// formatSecretForDisplay groups the base32 secret into 5-char chunks for
// readability when manually entered into authenticator apps that don't
// scan a QR code.
func formatSecretForDisplay(s string) string {
	out := make([]rune, 0, len(s)+len(s)/5)
	for i, r := range s {
		if i > 0 && i%5 == 0 {
			out = append(out, ' ')
		}
		out = append(out, r)
	}
	return string(out)
}
