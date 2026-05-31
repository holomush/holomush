// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestReconnectDedupRecordsAndDeduplicates verifies the bounded reconnect-overlap
// dedup window: a fresh id is reported unseen and recorded, a repeated id within
// the window is reported seen (so the caller skips the JetStream redelivery), and
// the window is bounded — ids beyond reconnectDedupSize are evicted rather than
// retained for the stream's lifetime (holomush-rsoe6.21).
func TestReconnectDedupRecordsAndDeduplicates(t *testing.T) {
	// window holds 2× the bound so the eviction cases push the earliest ids out.
	window := make([]string, 2*reconnectDedupSize)
	for i := range window {
		window[i] = fmt.Sprintf("evt-%06d", i)
	}

	tests := []struct {
		name    string
		prefill []string // ids recorded (in order) before the checked call
		check   string   // id whose seenOrRecord return value is asserted
		want    bool     // expected return: true == already seen within the window
		msg     string
	}{
		{
			name: "reports a fresh id as unseen", check: "evt-a", want: false,
			msg: "first sighting of an id must be reported unseen",
		},
		{
			name:    "reports a repeated id within the window as seen",
			prefill: []string{"evt-a"}, check: "evt-a", want: true,
			msg: "a repeated id must be reported seen so the redelivery is skipped",
		},
		{
			// Recording 2× the bound evicts the earliest id; an unbounded map
			// would still report it seen. seenOrRecord re-records it, but the
			// asserted return value proves it was not retained.
			name:    "evicts the earliest id beyond the bounded window",
			prefill: window, check: "evt-000000", want: false,
			msg: "an id beyond the bounded window must be evicted, not retained for the stream lifetime",
		},
		{
			name:    "keeps the most-recently-recorded id within the window",
			prefill: window, check: fmt.Sprintf("evt-%06d", 2*reconnectDedupSize-1), want: true,
			msg: "the most-recently-recorded id must still be within the window",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newReconnectDedup()
			for _, id := range tt.prefill {
				d.seenOrRecord(id)
			}
			assert.Equal(t, tt.want, d.seenOrRecord(tt.check), tt.msg)
		})
	}
}
