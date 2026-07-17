// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventvocab_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/holomush/holomush/internal/eventvocab"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestEventTypeArriveIsTheArriveWireString pins every EventType constant's
// wire string. These are durable discriminators written to events_audit and
// matched by plugin manifests / ABAC policy — a typo here silently
// reclassifies events.
func TestEventTypeArriveIsTheArriveWireString(t *testing.T) {
	tests := []struct {
		name  string
		value eventvocab.EventType
		want  string
	}{
		{"arrive constant is the arrive wire string", eventvocab.EventTypeArrive, "arrive"},
		{"leave constant is the leave wire string", eventvocab.EventTypeLeave, "leave"},
		{"system constant is the system wire string", eventvocab.EventTypeSystem, "system"},
		{"move constant is the move wire string", eventvocab.EventTypeMove, "move"},
		{"command_response constant is the command_response wire string", eventvocab.EventTypeCommandResponse, "command_response"},
		{"command_error constant is the command_error wire string", eventvocab.EventTypeCommandError, "command_error"},
		{"location_state constant is the location_state wire string", eventvocab.EventTypeLocationState, "location_state"},
		{"exit_update constant is the exit_update wire string", eventvocab.EventTypeExitUpdate, "exit_update"},
		{"session_ended constant is the session_ended wire string", eventvocab.EventTypeSessionEnded, "session_ended"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("got %q, want %q", string(tt.value), tt.want)
			}
		})
	}
}

// TestValidatePayloadAcceptsExactlyMaxPayloadSize verifies the 64 KiB boundary
// is inclusive: a payload at exactly MaxPayloadSize is accepted.
func TestValidatePayloadAcceptsExactlyMaxPayloadSize(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), eventvocab.MaxPayloadSize)

	if err := eventvocab.ValidatePayload(payload); err != nil {
		t.Errorf("ValidatePayload(%d bytes) = %v, want nil", len(payload), err)
	}
}

// TestValidatePayloadRejectsOneByteOverMaxPayloadSize verifies a payload one
// byte over the boundary is rejected with the EVENT_PAYLOAD_TOO_LARGE code
// and the size context values needed for diagnostics.
func TestValidatePayloadRejectsOneByteOverMaxPayloadSize(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), eventvocab.MaxPayloadSize+1)

	err := eventvocab.ValidatePayload(payload)
	if err == nil {
		t.Fatalf("ValidatePayload(%d bytes) = nil, want error", len(payload))
	}
	errutil.AssertErrorCode(t, err, "EVENT_PAYLOAD_TOO_LARGE")
	errutil.AssertErrorContext(t, err, "payload_size", eventvocab.MaxPayloadSize+1)
	errutil.AssertErrorContext(t, err, "max_payload_size", eventvocab.MaxPayloadSize)
}

// TestValidatePayloadAcceptsPayloadWellBelowLimit verifies a small payload
// far under the boundary is accepted (the common case).
func TestValidatePayloadAcceptsPayloadWellBelowLimit(t *testing.T) {
	payload := make([]byte, 1024)
	if err := eventvocab.ValidatePayload(payload); err != nil {
		t.Errorf("ValidatePayload(%d bytes) = %v, want nil", len(payload), err)
	}
}

// TestValidatePayloadAcceptsEmptyOrNilPayload verifies both a nil and a
// zero-length payload are accepted.
func TestValidatePayloadAcceptsEmptyOrNilPayload(t *testing.T) {
	if err := eventvocab.ValidatePayload(nil); err != nil {
		t.Errorf("ValidatePayload(nil) = %v, want nil", err)
	}
	if err := eventvocab.ValidatePayload([]byte{}); err != nil {
		t.Errorf("ValidatePayload([]byte{}) = %v, want nil", err)
	}
}

// TestLocationStatePayloadJSONRoundTripPreservesExactTagNames pins the wire
// tag names of LocationStatePayload — these are the audit record's field
// names, and drift here breaks any downstream consumer keying on them.
func TestLocationStatePayloadJSONRoundTripPreservesExactTagNames(t *testing.T) {
	payload := eventvocab.LocationStatePayload{
		Location: eventvocab.LocationStateInfo{ID: "loc-1", Name: "The Foyer", Description: "A quiet room."},
		Exits:    []eventvocab.LocationStateExit{{Direction: "north", Name: "North Exit", Locked: false}},
		Present:  []eventvocab.LocationStateChar{{CharacterID: "char-1", Name: "Alice", Idle: false}},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	for _, tag := range []string{`"location"`, `"exits"`, `"present"`} {
		if !bytes.Contains(data, []byte(tag)) {
			t.Errorf("marshaled JSON missing expected tag %s: %s", tag, data)
		}
	}

	var round eventvocab.LocationStatePayload
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if round.Location.ID != payload.Location.ID || round.Exits[0].Direction != payload.Exits[0].Direction ||
		round.Present[0].CharacterID != payload.Present[0].CharacterID {
		t.Errorf("round-tripped payload = %+v, want %+v", round, payload)
	}
}

// TestCommandResponsePayloadJSONRoundTripPreservesExactTagNames pins the
// "text" wire tag on CommandResponsePayload, the payload for
// command_response/command_error events.
func TestCommandResponsePayloadJSONRoundTripPreservesExactTagNames(t *testing.T) {
	payload := eventvocab.CommandResponsePayload{Text: "You look around."}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	if !bytes.Contains(data, []byte(`"text"`)) {
		t.Errorf("marshaled JSON missing expected tag \"text\": %s", data)
	}

	var round eventvocab.CommandResponsePayload
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if round.Text != payload.Text {
		t.Errorf("round-tripped payload = %+v, want %+v", round, payload)
	}
}
