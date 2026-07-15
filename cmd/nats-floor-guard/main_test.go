// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gomodFixture builds a minimal but valid go.mod pinning nats-server/v2 at the
// given version, so the floor guard can be exercised without touching the real
// module file.
func gomodFixture(natsVersion string) []byte {
	return []byte("module example.com/holomush-test\n\ngo 1.26\n\nrequire github.com/nats-io/nats-server/v2 " + natsVersion + "\n")
}

// TestCheckNatsFloor exercises the compensating control for the OSV blind spot:
// the nats-server advisory GHSA-q59r-vq66-pxc2 (alias CVE-2026-58207) is a
// git-commit-range-only OSV record with no Go-ecosystem package binding, so no
// manifest scanner (osv-scanner) or reachability scanner (govulncheck) can flag
// a vulnerable nats-server v2.14.2 from go.mod. The deterministic version-floor
// guard is what catches a regression below v2.14.3.
func TestCheckNatsFloor(t *testing.T) {
	tests := []struct {
		name        string
		natsVersion string
		wantErr     bool
	}{
		{"fails when nats-server is one patch below the security floor", "v2.14.2", true},
		{"fails when nats-server is far below the security floor", "v2.10.0", true},
		{"passes when nats-server is exactly at the security floor", "v2.14.3", false},
		{"passes when nats-server is above the floor on the same minor", "v2.14.9", false},
		{"passes when nats-server is above the floor on a later minor", "v2.15.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkNatsFloor(gomodFixture(tt.natsVersion))
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), natsSecurityFloor)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckNatsFloorFailsWhenNatsAbsentFromGoMod(t *testing.T) {
	gomod := []byte("module example.com/holomush-test\n\ngo 1.26\n")

	err := checkNatsFloor(gomod)

	require.Error(t, err)
	assert.Contains(t, err.Error(), natsModulePath)
}

// gomodWithReplace builds a go.mod that pins nats-server/v2 at requireVersion and
// adds a `replace` directive with the given right-hand side (target path plus an
// optional version), so the guard's replace-resolution can be exercised.
func gomodWithReplace(requireVersion, replaceRHS string) []byte {
	return []byte("module example.com/holomush-test\n\ngo 1.26\n\n" +
		"require github.com/nats-io/nats-server/v2 " + requireVersion + "\n\n" +
		"replace github.com/nats-io/nats-server/v2 => " + replaceRHS + "\n")
}

// TestCheckNatsFloorHonorsReplaceDirectives proves a `replace` cannot smuggle a
// vulnerable nats-server past a compliant `require` pin: the guard resolves the
// effective replacement version, and fails closed on an unverifiable
// (versionless / local-path) replacement.
func TestCheckNatsFloorHonorsReplaceDirectives(t *testing.T) {
	tests := []struct {
		name       string
		requireVer string
		replaceRHS string
		wantErr    bool
	}{
		{
			"fails when a replace downgrades a compliant require below the floor",
			"v2.14.3", "github.com/nats-io/nats-server/v2 v2.10.0", true,
		},
		{
			"passes when a replace lifts a below-floor require to the floor",
			"v2.14.2", "github.com/nats-io/nats-server/v2 v2.14.3", false,
		},
		{
			"passes when a replace points at a fork at or above the floor",
			"v2.14.3", "github.com/acme/nats-server/v2 v2.20.0", false,
		},
		{
			"fails closed when a replace points at a versionless local path",
			"v2.14.3", "../local-nats-server", true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkNatsFloor(gomodWithReplace(tt.requireVer, tt.replaceRHS))
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
