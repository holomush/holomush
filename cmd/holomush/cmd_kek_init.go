// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
)

// newKEKInitCmd returns the `holomush kek init <path>` command.
//
// Generates a fresh 32-byte random KEK, seals it with Argon2id + XChaCha20-Poly1305
// using the passphrase from HOLOMUSH_KEK_PASSPHRASE, and writes the key file to
// <path>. The passphrase env var is the same one the server reads at boot via
// buildKEKProviderFromConfig, so the file produced here loads cleanly.
//
// Intended for CI/E2E init containers and first-time server provisioning.
// MUST NOT be called on a server that already has crypto_keys rows unless
// you intend to replace the KEK (which requires a full provider-migrate).
//
// Flagged for crypto-review: this command generates KEK material and persists
// it using the kek.FileSource.Persist path (same code exercised by
// setupAdminAuthEnv in admin_authenticate_e2e_test.go — no new crypto logic).
func newKEKInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init <path>",
		Short: "Generate a new KEK file (for first-time provisioning and CI/E2E)",
		Long: `Generate a fresh 32-byte random KEK, encrypt it with the passphrase from
HOLOMUSH_KEK_PASSPHRASE, and write the key file to <path>.

The file format is identical to what the core server reads at boot via
HOLOMUSH_KEK_FILE + HOLOMUSH_KEK_PASSPHRASE.

This command is intended for first-time server provisioning and CI/E2E
init containers. Do NOT run it against a server that already has crypto_keys
rows without also running 'holomush crypto rekey' to re-wrap the existing DEKs.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			passphrase := os.Getenv(envKEKPassphrase)
			if passphrase == "" {
				return fmt.Errorf("environment variable %s is required", envKEKPassphrase)
			}

			pf := func(_ context.Context) ([]byte, error) {
				return []byte(passphrase), nil
			}
			src, err := kek.NewFileSource(path, pf)
			if err != nil {
				return fmt.Errorf("kek.NewFileSource: %w", err)
			}

			kekBytes := make([]byte, kek.KEKByteLength)
			if _, err := io.ReadFull(rand.Reader, kekBytes); err != nil {
				return fmt.Errorf("generating KEK material: %w", err)
			}

			if err := src.Persist(cmd.Context(), kekBytes); err != nil {
				return fmt.Errorf("writing KEK file: %w", err)
			}

			cmd.Printf("KEK file written to %s\n", path)
			return nil
		},
	}
}
