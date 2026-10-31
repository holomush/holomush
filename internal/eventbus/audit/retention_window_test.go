// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPartitionWindow(t *testing.T) {
	tests := []struct {
		name      string
		now       time.Time
		retain    time.Duration
		months    int
		wantStart string
		wantEnd   string
	}{
		{
			"ends one month ahead from the 31st of october", time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), 0, 1,
			"2026-10-01", "2026-11-01",
		},
		{
			"ends at march from the 31st of january with two months", time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC), 0, 2,
			"2027-01-01", "2027-03-01",
		},
		{
			"ends at the current month for zero months", time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), 0, 0,
			"2026-08-01", "2026-08-01",
		},
		{
			"starts at the month containing now minus the retain window", time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
			90 * 24 * time.Hour, 1, "2026-08-01", "2026-11-01",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := partitionWindow(tt.now, tt.retain, tt.months)
			assert.Equal(t, tt.wantStart, start.Format("2006-01-02"))
			assert.Equal(t, tt.wantEnd, end.Format("2006-01-02"))
		})
	}
}
