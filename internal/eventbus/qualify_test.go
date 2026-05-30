// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import "testing"

func TestQualify(t *testing.T) {
	tests := []struct {
		name    string
		gameID  string
		ref     string
		want    string
		wantErr bool
	}{
		{"prepends events and game id to a relative ref", "main", "location.01ABC", "events.main.location.01ABC", false},
		{"passes through an already-qualified subject unchanged", "main", "events.main.scene.01S.ic", "events.main.scene.01S.ic", false},
		{"rejects an empty game id", "", "location.01ABC", "", true},
		{"rejects an empty stream reference", "main", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Qualify(tt.gameID, tt.ref)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}
