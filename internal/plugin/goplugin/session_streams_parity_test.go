// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// fillStringSentinels sets every exported string field of the struct pointed to
// by ptr to a unique, field-name-derived sentinel, and fails the test on any
// exported field whose kind it cannot fill — forcing the author of a new
// non-string field to teach this guard AND confront the per-runtime parity
// contract (holomush-av954, generalizing holomush-peqfu).
func fillStringSentinels(t *testing.T, ptr any) {
	t.Helper()
	v := reflect.ValueOf(ptr).Elem()
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Type.Kind() != reflect.String {
			t.Fatalf("%s.%s has unsupported kind %s — extend this guard AND ensure the "+
				"new field is marshaled through every plugin runtime (holomush-av954)",
				typ.Name(), f.Name, f.Type.Kind())
		}
		v.Field(i).SetString("sentinel::" + f.Name)
	}
}

// protoStringValues collects every populated string-kind field value carried by
// a proto message, via protoreflect, so the parity guard can assert presence
// without hard-coding the proto's field list (a proto field rename or addition
// does not break the guard; only a dropped source field does).
func protoStringValues(m proto.Message) map[string]bool {
	set := map[string]bool{}
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, val protoreflect.Value) bool {
		if fd.Kind() == protoreflect.StringKind {
			set[val.String()] = true
		}
		return true
	})
	return set
}

// TestSessionStreamsRequestToProtoCarriesEveryField is the binary-runtime half
// of the SessionStreamsRequest parity guard (holomush-av954). SessionStreamsRequest
// forks per runtime — binary marshals it onto pluginv1.QuerySessionStreamsRequest
// (here), Lua passes it as positional on_session_subscribe args. There is no
// clean proto→struct inverse (binary plugins implement the QuerySessionStreams
// server directly; the host never reconstructs the struct from proto), so this
// is a one-directional presence guard rather than a round-trip: every exported
// field MUST appear in the marshaled proto. A field added without wiring
// sessionStreamsRequestToProto is dropped and fails here.
func TestSessionStreamsRequestToProtoCarriesEveryField(t *testing.T) {
	var req plugins.SessionStreamsRequest
	fillStringSentinels(t, &req)

	carried := protoStringValues(sessionStreamsRequestToProto(req))

	typ := reflect.TypeOf(req)
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue // fillStringSentinels skips unexported fields; match it here.
		}
		require.True(t, carried["sentinel::"+f.Name],
			"SessionStreamsRequest.%s MUST be carried into the QuerySessionStreams proto; "+
				"a missing field means the binary runtime never receives it (holomush-av954)", f.Name)
	}
}
