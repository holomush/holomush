// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/oklog/ulid/v2"
)

// stubIdentityRegistry verifies that the interface can be satisfied
// independently of *Manager (the *Manager conformance is added in T5).
type stubIdentityRegistry struct{}

func (stubIdentityRegistry) NameByID(ulid.ULID) (string, bool) { return "", false }
func (stubIdentityRegistry) IDByName(string) (ulid.ULID, bool) { return ulid.ULID{}, false }

func TestIdentityRegistryInterfaceIsSatisfiable(_ *testing.T) {
	var _ IdentityRegistry = stubIdentityRegistry{}
}
