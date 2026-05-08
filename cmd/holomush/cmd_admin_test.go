// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/totp"
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
