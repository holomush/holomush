// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import "testing"

func TestQualifyPrependsEventsAndGameID(t *testing.T) {
	got, err := Qualify("main", "location.01ABC")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "events.main.location.01ABC"; string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestQualifyRejectsEmptyGameID(t *testing.T) {
	if _, err := Qualify("", "location.01ABC"); err == nil {
		t.Fatal("expected error for empty game id")
	}
}

func TestQualifyRejectsEmptyReference(t *testing.T) {
	if _, err := Qualify("main", ""); err == nil {
		t.Fatal("expected error for empty stream reference")
	}
}

func TestQualifyPassesThroughAlreadyQualified(t *testing.T) {
	got, err := Qualify("main", "events.main.scene.01S.ic")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "events.main.scene.01S.ic"; string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
