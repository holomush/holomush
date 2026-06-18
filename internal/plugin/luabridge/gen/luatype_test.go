// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// fieldByPath looks up a field descriptor on a host.v1 message by message +
// field name, using the registered descriptors.
func fieldByPath(t *testing.T, msgFullName, field string) protoreflect.FieldDescriptor {
	t.Helper()
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(msgFullName))
	if err != nil {
		t.Fatalf("descriptor %s: %v", msgFullName, err)
	}
	md, ok := d.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatalf("%s is not a message", msgFullName)
	}
	fd := md.Fields().ByName(protoreflect.Name(field))
	if fd == nil {
		t.Fatalf("field %s not found on %s", field, msgFullName)
	}
	return fd
}

func TestLuaTypeMapsScalarStringField(t *testing.T) {
	// EmitEventRequest.stream is a proto string.
	fd := fieldByPath(t, "holomush.plugin.host.v1.EmitEventRequest", "stream")
	assert.Equal(t, "string", luaType(fd, luaClassName))
}

func TestLuaTypeMapsRepeatedMessageAndScalarFields(t *testing.T) {
	// QueryStreamHistoryResponse.events is repeated Event (message) → Event class[].
	events := fieldByPath(t, "holomush.plugin.host.v1.QueryStreamHistoryResponse", "events")
	assert.Equal(t, "holomush.msg.Event[]", luaType(events, luaClassName))

	// EmitEventRequest.event_type is string.
	et := fieldByPath(t, "holomush.plugin.host.v1.EmitEventRequest", "event_type")
	assert.Equal(t, "string", luaType(et, luaClassName))
}

// Map-branch (table<K,V>) coverage is deferred: host.v1 exposes no map field.
// It will be exercised by the structural stub test once a map-bearing descriptor
// is in the surface.
func TestLuaTypeMapsScalarKindsEnumAndOptional(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		field    string
		expected string
	}{
		{"bytes payload maps to string", "holomush.plugin.host.v1.EmitEventRequest", "payload", "string"},
		{"bool sensitive maps to boolean", "holomush.plugin.host.v1.EmitEventRequest", "sensitive", "boolean"},
		{"enum replay_mode maps to integer", "holomush.plugin.host.v1.AddSessionStreamRequest", "replay_mode", "integer"},
		{"proto3 optional message maps to bare class name", "holomush.plugin.host.v1.SetConnectionFocusRequest", "focus_key", "holomush.msg.FocusKey"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fd := fieldByPath(t, tt.msg, tt.field)
			assert.Equal(t, tt.expected, luaType(fd, luaClassName))
		})
	}
}
