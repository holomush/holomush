// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"testing"
)

// runCmd executes the holomush root command with the given args and
// returns captured stdout/stderr and the exit code (0 on success,
// nonzero if the command returned an error). Used by CLI subcommand
// tests in this package.
func runCmd(t *testing.T, args []string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	root := NewRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		return out.String(), 1
	}
	return out.String(), 0
}
