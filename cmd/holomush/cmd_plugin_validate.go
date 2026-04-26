// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// NewPluginValidateCmd is `holomush plugin validate <manifest-path>`.
// Author-time manifest validator that runs ValidateCrypto +
// ResolveCryptoRefs (self-refs only, since at author time we don't
// have the full registry).
func NewPluginValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <manifest-path>",
		Short: "Validate a plugin manifest (grammar + crypto.emits rules)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			m, err := plugins.ParseManifest(raw)
			if err != nil {
				return fmt.Errorf("parse: %w", err)
			}
			err = plugins.ValidateCrypto(m)
			if err != nil {
				return fmt.Errorf("validate: %w", err)
			}
			selfReg := map[string][]plugins.CryptoEmit{}
			if m.Crypto != nil {
				selfReg[m.Name] = m.Crypto.Emits
			}
			err = plugins.ResolveCryptoRefs(m, selfReg)
			if err != nil {
				return fmt.Errorf("resolve: %w", err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "OK")
			return err
		},
	}
}
