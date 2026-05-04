// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
	"math"
	"testing"
	"time"
)

func TestComputeSkewReturnsAbsoluteDriftInSeconds(t *testing.T) {
	local := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	remote := time.Date(2026, 5, 2, 12, 0, 35, 0, time.UTC) // 35s ahead

	skew := computeSkew(local, remote)
	if math.Abs(skew-35.0) > 0.001 {
		t.Errorf("skew = %v; want approx 35s", skew)
	}
}

func TestComputeSkewIsAbsoluteValue(t *testing.T) {
	local := time.Date(2026, 5, 2, 12, 0, 35, 0, time.UTC)
	remote := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC) // local 35s ahead

	skew := computeSkew(local, remote)
	if math.Abs(skew-35.0) > 0.001 {
		t.Errorf("skew = %v; want approx 35s (absolute)", skew)
	}
}
