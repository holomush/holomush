// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestResolveFromTally asserts the COLLECTING-phase resolution rule: no
// resolution while votes are pending; unanimous yes → all_yes; any no once
// everyone has voted → all_voted_any_no.
func TestResolveFromTally(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		tally  VoteTally
		wantT  PublishTrigger
		wantOK bool
	}{
		{"all yes unanimous", VoteTally{Yes: 3, No: 0, Pending: 0}, TriggerAllYes, true},
		{"any no after all voted", VoteTally{Yes: 2, No: 1, Pending: 0}, TriggerAllVotedAnyNo, true},
		{"still pending", VoteTally{Yes: 2, No: 0, Pending: 1}, "", false},
		{"mixed with pending", VoteTally{Yes: 1, No: 1, Pending: 1}, "", false},
		{"single yes single voter", VoteTally{Yes: 1, No: 0, Pending: 0}, TriggerAllYes, true},
		{"all no", VoteTally{Yes: 0, No: 2, Pending: 0}, TriggerAllVotedAnyNo, true},
		{"zero-vote tally does NOT auto-resolve (no silent empty-roster publish)", VoteTally{Yes: 0, No: 0, Pending: 0}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ResolveFromTally(tc.tally)
			assert.Equal(t, tc.wantOK, ok)
			if ok {
				assert.Equal(t, tc.wantT, got)
			}
		})
	}
}
