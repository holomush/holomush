// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCharacterRef_String(t *testing.T) {
	char := CharacterRef{
		ID:         NewULID(),
		Name:       "Emerald_Zephyr",
		LocationID: NewULID(),
	}
	assert.Equal(t, "Emerald_Zephyr", char.String())
}
