// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle

import "testing"

func TestSubsystemClusterStringIsCluster(t *testing.T) {
	if got := SubsystemCluster.String(); got != "cluster" {
		t.Fatalf("SubsystemCluster.String() = %q; want %q", got, "cluster")
	}
}
