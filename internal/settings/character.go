// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings

import (
	"context"
	"errors"

	"github.com/oklog/ulid/v2"
)

// nullCharacterSettingsStore is the Phase 4 null implementation of
// CharacterSettingsStore. All reads return (zero, false). Writes return
// an error. When character-scope preferences become a real need, this
// is replaced with a Postgres-backed implementation.
type nullCharacterSettingsStore struct{}

// NewNullCharacterSettingsStore returns a CharacterSettingsStore that
// always reports all keys as unset.
func NewNullCharacterSettingsStore() CharacterSettingsStore {
	return &nullCharacterSettingsStore{}
}

func (n *nullCharacterSettingsStore) For(_ context.Context, _ ulid.ULID) Settings {
	return &emptySettings{}
}

func (n *nullCharacterSettingsStore) SetString(
	_ context.Context, _ ulid.ULID, _, _ string,
) error {
	return errors.New("character settings not implemented in Phase 4")
}

// Compile-time interface check.
var _ CharacterSettingsStore = (*nullCharacterSettingsStore)(nil)
