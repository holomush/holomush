// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import "github.com/spf13/cobra"

// NewKEKCmd returns the `holomush kek` parent command.
//
// Subcommands cover KEK lifecycle operations (generation, future rotation).
// These commands are host-shell only and MUST NOT be exposed over network surfaces.
func NewKEKCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kek",
		Short: "KEK (master key encryption key) lifecycle commands",
	}
	cmd.AddCommand(newKEKInitCmd())
	return cmd
}
