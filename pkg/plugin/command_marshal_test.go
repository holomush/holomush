// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// fillStringFieldsWithSentinels sets every exported string field of the struct
// pointed to by ptr to a unique, field-name-derived sentinel, so that a field
// dropped by a marshaling step is detectable after a round-trip. It fails the
// test on any exported field whose kind it cannot fill — forcing the author of
// a new non-string CommandRequest field to teach this helper and, in doing so,
// confront the parity contract (holomush-peqfu).
func fillStringFieldsWithSentinels(t *testing.T, ptr any) {
	t.Helper()
	v := reflect.ValueOf(ptr).Elem()
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		switch f.Type.Kind() {
		case reflect.String:
			v.Field(i).SetString("sentinel::" + f.Name)
		default:
			t.Fatalf("fillStringFieldsWithSentinels: %s.%s has unsupported kind %s — "+
				"extend this helper AND ensure the new field is marshaled through both "+
				"plugin runtimes (holomush-peqfu)", typ.Name(), f.Name, f.Type.Kind())
		}
	}
}

// TestCommandRequestProtoRoundTripCarriesEveryField is the binary-runtime half
// of the CommandRequest parity guard (holomush-peqfu). It fills every exported
// field with a sentinel and asserts the value survives cmd -> proto -> cmd. A
// field wired in only one direction — or missing from the proto message
// entirely — is dropped by the round-trip and fails this test. This is the
// structural class-killer behind holomush-dble7 (binary plugins silently
// dropped connection_id on the proto receive side): adding a CommandRequest
// field without wiring CommandRequestToProto AND CommandRequestFromProto can no
// longer pass silently.
func TestCommandRequestProtoRoundTripCarriesEveryField(t *testing.T) {
	var req CommandRequest
	fillStringFieldsWithSentinels(t, &req)

	got := CommandRequestFromProto(CommandRequestToProto(req))

	require.Equal(t, req, got,
		"every exported CommandRequest field MUST survive the cmd->proto->cmd round "+
			"trip; a mismatch means a field is wired in one direction but not the other, "+
			"or is missing from the proto message (holomush-peqfu)")
}
