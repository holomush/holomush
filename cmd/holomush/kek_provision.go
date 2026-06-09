// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/samber/oops"
	"golang.org/x/term"
)

const envKEKPassphraseFile = "HOLOMUSH_KEK_PASSPHRASE_FILE" //nolint:gosec // env var name, not a credential

// passphraseSources controls how resolvePassphrase may obtain the KEK passphrase.
type passphraseSources struct {
	// interactive permits prompting the user on stdin when no env var is set.
	interactive bool
}

// resolvePassphrase returns the KEK unlock passphrase from (first hit wins):
// env HOLOMUSH_KEK_PASSPHRASE, env HOLOMUSH_KEK_PASSPHRASE_FILE (file contents,
// trailing whitespace trimmed), or an interactive prompt when a TTY is attached.
// It NEVER logs the passphrase and NEVER auto-generates one.
func resolvePassphrase(src passphraseSources) ([]byte, error) {
	if p := os.Getenv(envKEKPassphrase); p != "" {
		return []byte(p), nil
	}
	if f := os.Getenv(envKEKPassphraseFile); f != "" {
		raw, err := os.ReadFile(f) //nolint:gosec // path comes from operator-controlled env var, not user input
		if err != nil {
			return nil, oops.Code("KEK_PASSPHRASE_FILE_READ_FAILED").With("path", f).Wrap(err)
		}
		return bytes.TrimRight(raw, "\r\n \t"), nil
	}
	if src.interactive {
		return promptPassphrase()
	}
	return nil, oops.Code("KEK_PASSPHRASE_UNAVAILABLE").
		Errorf("no KEK passphrase: set %s, %s, or run interactively", envKEKPassphrase, envKEKPassphraseFile)
}

// ensureKeyfile guarantees a sealed keyfile exists at path. If present: no-op.
// If absent and autoGen: mint a fresh master KEK, seal it with the passphrase,
// persist. If absent and !autoGen: return KEK_FILE_NOT_FOUND (refuse to boot).
// It MUST NOT overwrite an existing keyfile.
func ensureKeyfile(ctx context.Context, path string, pf kek.PassphraseFunc, autoGen bool) error {
	src, err := kek.NewFileSource(path, pf)
	if err != nil {
		return oops.Wrap(err)
	}
	if _, loadErr := src.Load(ctx); loadErr == nil {
		return nil // present (load succeeded) → reuse, never regenerate
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return oops.Wrap(loadErr) // corrupt / wrong-passphrase / other → surface
	}
	if !autoGen {
		return oops.Code("KEK_FILE_NOT_FOUND").With("path", path).
			Errorf("no KEK file at %s; pass --auto-gen-kek for first start", path)
	}
	// Claim the path with O_EXCL so exactly one of N concurrently auto-gen'ing
	// boots mints the KEK. Without this, two first boots could each Persist an
	// independent key and the rename of one would silently discard the other —
	// any DEKs already sealed under the discarded key would be unrecoverable.
	claim, claimErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if claimErr != nil {
		if errors.Is(claimErr, os.ErrExist) {
			// Lost the creation race: a sibling boot owns the file. Reuse it via
			// a fresh load; a sealed-format error here means the winner has not
			// finished persisting yet — fail loudly rather than overwrite.
			if _, loadErr := src.Load(ctx); loadErr != nil {
				return oops.Wrap(loadErr)
			}
			return nil
		}
		return oops.Code("KEK_FILE_CREATE_FAILED").With("path", path).Wrap(claimErr)
	}
	if closeErr := claim.Close(); closeErr != nil {
		return oops.Code("KEK_FILE_CREATE_FAILED").With("path", path).Wrap(closeErr)
	}
	master := make([]byte, kek.KEKByteLength)
	defer clear(master)
	if _, err := io.ReadFull(rand.Reader, master); err != nil {
		return oops.Code("KEK_GENERATE_FAILED").Wrap(err)
	}
	if persistErr := src.Persist(ctx, master); persistErr != nil {
		// Remove the empty placeholder so a retry sees a clean "absent" state
		// instead of a corrupt zero-byte keyfile.
		if removeErr := os.Remove(path); removeErr != nil {
			slog.WarnContext(ctx, "failed to remove placeholder keyfile after persist failure",
				"path", path, "error", removeErr)
		}
		return oops.Wrap(persistErr)
	}
	return nil
}

// promptPassphrase reads a passphrase from stdin without echoing it.
// The prompt is written to stderr. Returns an error if stdin is not a terminal.
func promptPassphrase() ([]byte, error) {
	fd := int(os.Stdin.Fd()) //nolint:gosec // term.ReadPassword requires int; fd is always a small safe value
	if !term.IsTerminal(fd) {
		return nil, oops.Code("KEK_PASSPHRASE_UNAVAILABLE").
			Errorf("stdin is not a TTY; set %s or %s", envKEKPassphrase, envKEKPassphraseFile)
	}
	fmt.Fprintf(os.Stderr, "Enter KEK passphrase: ")
	pass, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr) // newline after hidden input
	if err != nil {
		return nil, oops.Code("KEK_PASSPHRASE_UNAVAILABLE").Wrap(err)
	}
	return pass, nil
}
