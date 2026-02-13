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
