// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// luaType maps a proto field to a LuaLS type string (spec §3). Repeated and map
// fields are detected before scalar kind. Message fields render as the class
// name resolved by nameFor, which the stub collector makes collision-aware
// (holomush-t4tye) so a reference to one of two same-short-name messages from
// different packages resolves to that message's distinct class.
func luaType(fd protoreflect.FieldDescriptor, nameFor func(protoreflect.MessageDescriptor) string) string {
	// Map must be checked before IsList: a proto map is a repeated synthetic
	// message, so IsMap() is the discriminator.
	if fd.IsMap() {
		k := scalarLuaType(fd.MapKey())
		v := luaTypeNoCollection(fd.MapValue(), nameFor)
		return fmt.Sprintf("table<%s, %s>", k, v)
	}
	if fd.IsList() {
		return luaTypeNoCollection(fd, nameFor) + "[]"
	}
	return luaTypeNoCollection(fd, nameFor)
}

// luaTypeNoCollection maps the element type, ignoring list/map wrapping.
func luaTypeNoCollection(fd protoreflect.FieldDescriptor, nameFor func(protoreflect.MessageDescriptor) string) string {
	if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind {
		return nameFor(fd.Message())
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

// luaClassName returns the canonical LuaLS @class name for a message descriptor
// as holomush.msg.<ShortName>. This is the name every non-colliding message
// keeps; the stub collector's classNamer overrides it only for messages whose
// short name collides with another reachable message from a different package
// (see disambiguatedClassName, holomush-t4tye).
func luaClassName(md protoreflect.MessageDescriptor) string {
	return "holomush.msg." + string(md.Name())
}

// disambiguatedClassName returns a collision-free @class name derived from the
// message's full name. It strips the redundant leading "holomush." root (which
// the holomush.msg prefix already implies) and keeps the remaining
// package+message path, so two same-short-name messages from different packages
// (e.g. holomush.plugin.host.v1.DecryptOwnAuditRowsRequest vs
// holomush.plugin.v1.DecryptOwnAuditRowsRequest) yield distinct classes. The
// full name is unique per message, so the result is too.
func disambiguatedClassName(md protoreflect.MessageDescriptor) string {
	return "holomush.msg." + strings.TrimPrefix(string(md.FullName()), "holomush.")
}
