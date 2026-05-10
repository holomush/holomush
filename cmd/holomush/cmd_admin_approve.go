// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/hex"
	"fmt"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/spf13/cobra"

	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"
)

// newAdminApproveCmd returns the `holomush admin approve <request_id>`
// subcommand. It prompts the second operator for credentials, authenticates
// over the UDS admin socket, then calls Approve as the dual-control signoff.
func newAdminApproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approve <request_id>",
		Short: "Second-op signoff on a pending admin_approvals row (dual-control)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			rid, err := parseRequestID(args[0])
			if err != nil {
				return oops.Code("ADMIN_APPROVE_INVALID_REQUEST_ID").
					With("input", args[0]).Wrap(err)
			}
			socketPath := adminSocketPathFromConfig(c)
			client := adminClientFromSocket(socketPath)
			return runAdminApprove(c, client, args[0], rid)
		},
	}
	bindAdminSocketFlag(cmd)
	return cmd
}

// runAdminApprove is the testable core of `admin approve`. The client is
// injected so tests can substitute a fake without a live UDS server.
func runAdminApprove(
	cmd *cobra.Command,
	client adminv1connect.AdminServiceClient,
	rawID string,
	requestID []byte,
) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	sessionToken, err := authenticateInteractive(ctx, client, cmd)
	if err != nil {
		return oops.Code("ADMIN_APPROVE_AUTH_FAILED").Wrap(err)
	}

	_, err = client.Approve(ctx, connect.NewRequest(&adminv1.ApproveRequest{
		SessionToken: sessionToken,
		RequestId:    requestID,
	}))
	if err != nil {
		return oops.Code("ADMIN_APPROVE_FAILED").
			With("request_id", rawID).Wrap(err)
	}

	if _, werr := fmt.Fprintf(cmd.OutOrStdout(), "Approved request %s.\n", rawID); werr != nil {
		return oops.Code("ADMIN_APPROVE_PRINT_FAILED").Wrap(werr)
	}
	return nil
}

// parseRequestID accepts either a 26-char ULID string or a 32-char hex
// representation of 16 bytes. Returns the 16-byte form for the proto field.
func parseRequestID(s string) ([]byte, error) {
	// Try ULID first (26-char base32 Crockford).
	if id, err := ulid.Parse(s); err == nil {
		b := id.Bytes()
		return b, nil
	}
	// Fall back to 32-char hex of 16 bytes.
	if len(s) == 32 {
		b, err := hex.DecodeString(s)
		if err == nil && len(b) == 16 {
			return b, nil
		}
	}
	return nil, fmt.Errorf("request_id must be a 26-char ULID or 32-char hex of 16 bytes, got %q", s)
}
