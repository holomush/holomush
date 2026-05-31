// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings

import (
	"context"
	"encoding/json"
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

// For returns a Scoped handle whose host partition is empty: bare reads
// report all keys as unset (matching the Phase 4 null behavior). Plugin
// partitions are usable in-memory only and are NOT persisted — the commit func
// is nil, so any Plugin/Host write is silently dropped when the handle goes out
// of scope. Real character-scope persistence is provided by the repo-backed
// CharacterSettings store (NewRepoCharacterSettingsStore); this null store is
// the deferred-persistence placeholder (see iokti.3).
func (n *nullCharacterSettingsStore) For(_ context.Context, _ ulid.ULID) Scoped {
	return newScopedView(map[string]json.RawMessage{})
}

func (n *nullCharacterSettingsStore) SetString(
	_ context.Context, _ ulid.ULID, _, _ string,
) error {
	return errors.New("character settings not implemented in Phase 4")
}

// Compile-time interface check.
var _ CharacterSettingsStore = (*nullCharacterSettingsStore)(nil)
