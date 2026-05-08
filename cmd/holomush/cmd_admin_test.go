// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/totp"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestAdminCmdRegistered(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"admin"})
	assert.NoError(t, err)
	assert.Equal(t, "admin", cmd.Name())
}

func TestAdminTOTPCmdRegistered(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"admin", "totp"})
	assert.NoError(t, err)
	assert.Equal(t, "totp", cmd.Name())
}

func TestAdminTOTPBootstrapEnrollExists(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"admin", "totp", "bootstrap-enroll"})
	assert.NoError(t, err)
	assert.Equal(t, "bootstrap-enroll <username>", cmd.Use)
}

func TestAdminTOTPEnrollExists(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"admin", "totp", "enroll"})
	assert.NoError(t, err)
	assert.Equal(t, "enroll", cmd.Use)
	assert.NotNil(t, cmd.Flags().Lookup("username"))
}

func TestAdminTOTPRecoverExists(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"admin", "totp", "recover"})
	assert.NoError(t, err)
	assert.Equal(t, "recover", cmd.Use)
	assert.NotNil(t, cmd.Flags().Lookup("username"))
}

func TestPrintEnrollmentRendersAllSections(t *testing.T) {
	var buf bytes.Buffer
	enr := totp.Enrollment{
		Secret:          "JBSWY3DPEHPK3PXP",
		ProvisioningURI: "otpauth://totp/holomush-default:alice?secret=...",
		RecoveryCodes:   []string{"aaaa-bbbb-cccc-dddd", "eeee-ffff-1111-2222"},
	}
	require.NoError(t, printEnrollment(&buf, "alice", "01HZ", enr))
	out := buf.String()
	assert.Contains(t, out, "TOTP enrolled for alice")
	assert.Contains(t, out, "01HZ")
	assert.Contains(t, out, enr.ProvisioningURI)
	assert.Contains(t, out, "JBSWY") // start of formatted secret
	assert.Contains(t, out, "aaaa-bbbb-cccc-dddd")
	assert.Contains(t, out, "eeee-ffff-1111-2222")
	assert.Contains(t, out, "WILL NOT be shown again")
}

func TestFormatSecretForDisplayInsertsSpacesEveryFive(t *testing.T) {
	assert.Equal(t, "JBSWY 3DPEH PK3PX P", formatSecretForDisplay("JBSWY3DPEHPK3PXP"))
	assert.Equal(t, "ABCDE", formatSecretForDisplay("ABCDE"))
	assert.Equal(t, "ABCDE F", formatSecretForDisplay("ABCDEF"))
}

// resolveUsername paths.

func TestResolveUsernameReturnsFlagValueWhenSet(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&bytes.Buffer{})

	got, err := resolveUsername(cmd, "alice")
	require.NoError(t, err)
	assert.Equal(t, "alice", got)
}

func TestResolveUsernameReadsStdinWhenFlagEmpty(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("bob\n"))
	out := &bytes.Buffer{}
	cmd.SetOut(out)

	got, err := resolveUsername(cmd, "")
	require.NoError(t, err)
	assert.Equal(t, "bob", got)
	assert.Contains(t, out.String(), "username: ")
}

func TestResolveUsernameRejectsEmptyInput(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("   \n"))
	cmd.SetOut(&bytes.Buffer{})

	_, err := resolveUsername(cmd, "")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_TOTP_USERNAME_REQUIRED")
}

func TestResolveUsernameRejectsImmediateEOF(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("")) // EOF before any character
	cmd.SetOut(&bytes.Buffer{})

	_, err := resolveUsername(cmd, "")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_TOTP_PROMPT_FAILED")
}

// Piped input without trailing newline (e.g., `printf alice | holomush admin
// totp enroll`) should be accepted — bufio.Reader.ReadString returns the
// partial line + io.EOF, which is the documented behavior we tolerate.
func TestResolveUsernameAcceptsPipedInputWithoutNewline(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("alice")) // no trailing newline
	cmd.SetOut(&bytes.Buffer{})

	got, err := resolveUsername(cmd, "")
	require.NoError(t, err)
	assert.Equal(t, "alice", got)
}

// readPassword fallback path (stdin is not a TTY in tests).

func TestReadPasswordFallbackTrimsTrailingNewlines(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("hunter2\r\n"))
	out := &bytes.Buffer{}
	cmd.SetOut(out)

	got, err := readPassword(cmd, "pw: ")
	require.NoError(t, err)
	assert.Equal(t, "hunter2", got)
	assert.Contains(t, out.String(), "pw: ")
}

func TestReadPasswordFallbackRejectsImmediateEOF(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&bytes.Buffer{})

	_, err := readPassword(cmd, "pw: ")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_TOTP_PROMPT_FAILED")
}

func TestReadPasswordFallbackAcceptsPipedInputWithoutNewline(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("hunter2"))
	cmd.SetOut(&bytes.Buffer{})

	got, err := readPassword(cmd, "pw: ")
	require.NoError(t, err)
	assert.Equal(t, "hunter2", got)
}
