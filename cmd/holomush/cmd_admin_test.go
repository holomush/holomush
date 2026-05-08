// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
