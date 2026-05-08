// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import "github.com/spf13/cobra"

// NewAdminCmd is the `holomush admin` parent command. Subcommands cover
// operator break-glass and admin flows that run on the host shell only;
// they MUST NOT be exposed over network surfaces.
func NewAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Operator break-glass and admin commands (host-shell only)",
	}
	cmd.AddCommand(NewAdminTOTPCmd())
	return cmd
}
