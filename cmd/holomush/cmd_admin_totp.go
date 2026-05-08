// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import "github.com/spf13/cobra"

// NewAdminTOTPCmd is the `holomush admin totp` parent. Subcommands land
// in later beads (T13 bootstrap-enroll; T14 enroll + recover).
func NewAdminTOTPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "totp",
		Short: "TOTP enrollment, verification, recovery (host-shell only)",
	}
	// Subcommands wire in via T13 (bootstrap-enroll) and T14
	// (enroll, recover).
	return cmd
}
