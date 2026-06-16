// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// luaType maps a proto field to a LuaLS type string (spec §3). Repeated and map
// fields are detected before scalar kind. Message fields render as the namespaced
// class name from luaClassName.
func luaType(fd protoreflect.FieldDescriptor) string {
	// Map must be checked before IsList: a proto map is a repeated synthetic
	// message, so IsMap() is the discriminator.
	if fd.IsMap() {
		k := scalarLuaType(fd.MapKey())
		v := luaTypeNoCollection(fd.MapValue())
		return fmt.Sprintf("table<%s, %s>", k, v)
	}
	if fd.IsList() {
		return luaTypeNoCollection(fd) + "[]"
	}
	return luaTypeNoCollection(fd)
}

// luaTypeNoCollection maps the element type, ignoring list/map wrapping.
func luaTypeNoCollection(fd protoreflect.FieldDescriptor) string {
	if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind {
		return luaClassName(fd.Message())
	}
	return scalarLuaType(fd)
}

// scalarLuaType maps a proto scalar kind to a LuaLS primitive.
func scalarLuaType(fd protoreflect.FieldDescriptor) string {
	switch fd.Kind() {
	case protoreflect.StringKind, protoreflect.BytesKind:
		return "string"
	case protoreflect.BoolKind:
		return "boolean"
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return "number"
	case protoreflect.EnumKind:
		return "integer"
	default:
		// All int/uint/sint/fixed kinds.
		return "integer"
	}
}

// luaClassName returns the LuaLS @class name for a message descriptor as
// holomush.msg.<ShortName>. The short name is used deliberately for this task;
// fully-qualified namespacing (spec §3's holomush.host.<token>.<MessageName>)
// is applied downstream by the stub collector (plan Task 3/4), not here.
func luaClassName(md protoreflect.MessageDescriptor) string {
	return "holomush.msg." + string(md.Name())
}
