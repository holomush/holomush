// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/spf13/cobra"
	"golang.org/x/term"

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
	cmd.AddCommand(newAdminTOTPEnrollCmd())
	cmd.AddCommand(newAdminTOTPRecoverCmd())
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

func newAdminTOTPEnrollCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "Self-enroll TOTP after credential check (host-shell only)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			deps, cleanup, err := buildAdminTOTPDeps(ctx)
			if err != nil {
				return err
			}
			defer cleanup()

			user, err := resolveUsername(cmd, username)
			if err != nil {
				return err
			}
			password, err := readPassword(cmd, "password: ")
			if err != nil {
				return err
			}
			player, err := deps.authSvc.ValidateCredentials(ctx, user, password)
			if err != nil {
				return oops.With("username", user).Wrap(err)
			}
			res, err := deps.totpSvc.Enroll(ctx, player.ID)
			if err != nil {
				return oops.With("username", user).Wrap(err)
			}
			return printEnrollment(cmd.OutOrStdout(), user, player.ID.String(), res.Enrollment)
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "username (prompt if not set)")
	return cmd
}

func newAdminTOTPRecoverCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "recover",
		Short: "Consume a recovery code, clear TOTP, and instruct re-enrollment (host-shell only)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			deps, cleanup, err := buildAdminTOTPDeps(ctx)
			if err != nil {
				return err
			}
			defer cleanup()

			user, err := resolveUsername(cmd, username)
			if err != nil {
				return err
			}
			recoveryCode, err := readPassword(cmd, "recovery code: ")
			if err != nil {
				return err
			}
			// Timing-safe: surface generic ErrInvalidRecoveryCode whether the
			// player lookup or the code check fails. Operators get the same
			// signal for "wrong username" and "wrong code" to avoid leaking
			// which usernames have TOTP enrollments.
			pidStr, err := deps.totpRepo.PlayerIDFromUsername(ctx, user)
			if err != nil {
				return totp.ErrInvalidRecoveryCode
			}
			pidULID, err := ulid.Parse(pidStr)
			if err != nil {
				return totp.ErrInvalidRecoveryCode
			}
			if _, err := deps.totpSvc.ConsumeRecoveryCode(ctx, pidULID, recoveryCode); err != nil {
				return oops.With("username", user).Wrap(err)
			}
			if _, err := deps.totpSvc.ClearTOTP(ctx, pidULID, totp.ClearReasonRecoveryCode); err != nil {
				return oops.With("username", user).Wrap(err)
			}
			_, werr := fmt.Fprintf(cmd.OutOrStdout(),
				"TOTP cleared for %s. Run `holomush admin totp enroll --username %s` to re-enroll.\n",
				user, user)
			if werr != nil {
				return oops.Code("ADMIN_TOTP_PRINT_FAILED").Wrap(werr)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "username (prompt if not set)")
	return cmd
}

// resolveUsername returns the username flag value or prompts on stdin.
func resolveUsername(cmd *cobra.Command, flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), "username: "); err != nil {
		return "", oops.Code("ADMIN_TOTP_PROMPT_FAILED").Wrap(err)
	}
	r := bufio.NewReader(cmd.InOrStdin())
	line, err := r.ReadString('\n')
	if err != nil {
		return "", oops.Code("ADMIN_TOTP_PROMPT_FAILED").Wrap(err)
	}
	user := strings.TrimSpace(line)
	if user == "" {
		return "", oops.Code("ADMIN_TOTP_USERNAME_REQUIRED").
			Errorf("username is required")
	}
	return user, nil
}

// readPassword reads a secret from the terminal without echoing. Falls
// back to a plain stdin read if stdin is not a terminal (e.g., piped
// input in CI).
func readPassword(cmd *cobra.Command, prompt string) (string, error) {
	if _, err := fmt.Fprint(cmd.OutOrStdout(), prompt); err != nil {
		return "", oops.Code("ADMIN_TOTP_PROMPT_FAILED").Wrap(err)
	}
	fd := int(os.Stdin.Fd()) //nolint:gosec // G115: stdin fd is small and platform-bounded; conversion is safe
	if term.IsTerminal(fd) {
		buf, err := term.ReadPassword(fd)
		if err != nil {
			return "", oops.Code("ADMIN_TOTP_PROMPT_FAILED").Wrap(err)
		}
		if _, werr := fmt.Fprintln(cmd.OutOrStdout()); werr != nil {
			return "", oops.Code("ADMIN_TOTP_PROMPT_FAILED").Wrap(werr)
		}
		return string(buf), nil
	}
	r := bufio.NewReader(cmd.InOrStdin())
	line, err := r.ReadString('\n')
	if err != nil {
		return "", oops.Code("ADMIN_TOTP_PROMPT_FAILED").Wrap(err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

