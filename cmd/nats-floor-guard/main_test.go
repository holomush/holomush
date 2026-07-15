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
			err := checkNatsFloor(gomodFixture(tt.natsVersion), natsSecurityFloor)
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

	err := checkNatsFloor(gomod, natsSecurityFloor)

	require.Error(t, err)
	assert.Contains(t, err.Error(), natsModulePath)
}
