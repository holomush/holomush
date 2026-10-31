// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPartitionRange(t *testing.T) {
	tests := []struct {
		name      string
		input     time.Time
		wantName  string
		wantStart string
		wantEnd   string
	}{
		{
			name:      "february 2026",
			input:     time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
			wantName:  "access_audit_log_2026_02",
			wantStart: "2026-02-01",
			wantEnd:   "2026-03-01",
		},
		{
			name:      "december wraps to next year",
			input:     time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
			wantName:  "access_audit_log_2026_12",
			wantStart: "2026-12-01",
			wantEnd:   "2027-01-01",
		},
		{
			name:      "january",
			input:     time.Date(2027, 1, 31, 23, 59, 59, 0, time.UTC),
			wantName:  "access_audit_log_2027_01",
			wantStart: "2027-01-01",
			wantEnd:   "2027-02-01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, start, end := partitionRange(tt.input)
			assert.Equal(t, tt.wantName, name)
			assert.Equal(t, tt.wantStart, start.Format("2006-01-02"))
			assert.Equal(t, tt.wantEnd, end.Format("2006-01-02"))
		})
	}
}

func TestPartitionMonths(t *testing.T) {
	tests := []struct {
		name   string
		now    time.Time
		months int
		want   []string
	}{
		{
			"steps whole months from the 31st of october", time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), 3,
			[]string{"2026-10-01", "2026-11-01", "2026-12-01"},
		},
		{
			"does not skip february from the 31st of january", time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC), 2,
			[]string{"2027-01-01", "2027-02-01"},
		},
		{
			"does not skip february from the 29th of january", time.Date(2027, 1, 29, 0, 0, 0, 0, time.UTC), 3,
			[]string{"2027-01-01", "2027-02-01", "2027-03-01"},
		},
		{"returns no months for zero", time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), 0, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := []string{}
			for _, m := range partitionMonths(tt.now, tt.months) {
				got = append(got, m.Format("2006-01-02"))
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
