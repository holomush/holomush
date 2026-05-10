// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/samber/oops"
	"github.com/spf13/cobra"

	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"
)

// newAdminTOTPResetCmd returns the `holomush admin totp reset <player_id>`
// subcommand. It prompts the operator for credentials, authenticates over the
// UDS admin socket, then calls ResetTOTP to clear the target player's TOTP
// enrollment.
func newAdminTOTPResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset <player_id>",
		Short: "Clear a player's TOTP enrollment via the admin socket (second-op break-glass)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			targetPlayerID := args[0]
			socketPath := adminSocketPathFromConfig(c)
			client := adminClientFromSocket(socketPath)
			return runAdminTOTPReset(c, client, targetPlayerID)
		},
	}
	bindAdminSocketFlag(cmd)
	return cmd
}

// runAdminTOTPReset is the testable core of `admin totp reset`. The client is
// injected so tests can substitute a fake without a live UDS server.
func runAdminTOTPReset(
	cmd *cobra.Command,
	client adminv1connect.AdminServiceClient,
	targetPlayerID string,
) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	sessionToken, err := authenticateInteractive(ctx, client, cmd)
	if err != nil {
		return oops.Code("ADMIN_TOTP_RESET_AUTH_FAILED").Wrap(err)
	}

	resp, err := client.ResetTOTP(ctx, connect.NewRequest(&adminv1.ResetTOTPRequest{
		SessionToken:   sessionToken,
		TargetPlayerId: targetPlayerID,
	}))
	if err != nil {
		return oops.Code("ADMIN_TOTP_RESET_FAILED").
			With("target_player_id", targetPlayerID).Wrap(err)
	}

	var msg string
	if resp.Msg.GetCleared() {
		msg = fmt.Sprintf("Cleared TOTP enrollment for player %s.\n", targetPlayerID)
	} else {
		msg = fmt.Sprintf("Player %s was not enrolled; no change.\n", targetPlayerID)
	}
	if _, werr := fmt.Fprint(cmd.OutOrStdout(), msg); werr != nil {
		return oops.Code("ADMIN_TOTP_RESET_PRINT_FAILED").Wrap(werr)
	}
	return nil
}
