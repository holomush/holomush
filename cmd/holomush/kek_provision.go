// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"fmt"
	"os"

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
