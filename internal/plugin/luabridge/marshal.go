// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package luabridge marshals typed host.v1 protobuf messages to and from Lua
// tables and hosts the codegen'd typed host-capability bindings. The marshaler
// is reflection-based (protoreflect) so a single pair of functions serves every
// host.v1 RPC; the generated registrars in bindings_gen.go call it.
//
// Lua tables use the proto field's snake_case name as the key (e.g.
// entity_type), matching protoreflect's FieldDescriptor.Name(). Field values
// map to Lua values per the proto kind: scalars to numbers/strings/booleans,
// bytes to a Lua string, enums to their number, nested messages to nested
// tables, and repeated fields to a 1-indexed Lua array table.
//
//nolint:gocritic // captLocal: L is the idiomatic name for lua.LState
package luabridge

import (
	"context"

	"github.com/samber/oops"
	lua "github.com/yuin/gopher-lua"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ProtoToLuaTable converts a protobuf message into a freshly-allocated Lua
// table, keyed by each populated field's snake_case proto name. Unpopulated
// (default-valued) fields are omitted, matching proto3 presence semantics for
// scalars and the absence of nested messages.
func ProtoToLuaTable(L *lua.LState, msg proto.Message) *lua.LTable {
	tbl := L.NewTable()
	if msg == nil {
		return tbl
	}
	m := msg.ProtoReflect()
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		L.SetField(tbl, string(fd.Name()), valueToLua(L, fd, v))
		return true
	})
	return tbl
}

// valueToLua converts a single protoreflect value into its Lua representation,
// dispatching on the field's cardinality (repeated vs singular) and kind.
func valueToLua(L *lua.LState, fd protoreflect.FieldDescriptor, v protoreflect.Value) lua.LValue {
	if fd.IsList() {
		list := v.List()
		arr := L.NewTable()
		for i := 0; i < list.Len(); i++ {
			arr.Append(scalarToLua(L, fd, list.Get(i)))
		}
		return arr
	}
	return scalarToLua(L, fd, v)
}

// scalarToLua converts a non-repeated protoreflect value (one element of a list,
// or a singular field) to a Lua value based on the field's proto kind.
func scalarToLua(L *lua.LState, fd protoreflect.FieldDescriptor, v protoreflect.Value) lua.LValue {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return lua.LBool(v.Bool())
	case protoreflect.StringKind:
		return lua.LString(v.String())
	case protoreflect.BytesKind:
		return lua.LString(string(v.Bytes()))
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return lua.LNumber(v.Int())
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return lua.LNumber(v.Uint())
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return lua.LNumber(v.Float())
	case protoreflect.EnumKind:
		return lua.LNumber(v.Enum())
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return ProtoToLuaTable(L, v.Message().Interface())
	default:
		// Unsupported kind (no host.v1 message uses one today). Represent as nil
		// rather than panicking so a future field addition fails loudly in a test
		// (the round-trip assertion) instead of crashing a plugin VM.
		return lua.LNil
	}
}

// LuaTableToProto populates msg from a Lua table whose keys are snake_case proto
// field names. Keys with no matching field are ignored; type-mismatched values
// for a known field return an error. It is the inverse of ProtoToLuaTable.
func LuaTableToProto(tbl *lua.LTable, msg proto.Message) error {
	if tbl == nil {
		return oops.Code("LUABRIDGE_NIL_TABLE").Errorf("nil lua table")
	}
	m := msg.ProtoReflect()
	fields := m.Descriptor().Fields()
	var rangeErr error
	tbl.ForEach(func(k, val lua.LValue) {
		if rangeErr != nil {
			return
		}
		key, ok := k.(lua.LString)
		if !ok {
			return
		}
		fd := fields.ByName(protoreflect.Name(string(key)))
		if fd == nil {
			// Unknown key: ignore (forward-compatible with extra Lua fields).
			return
		}
		if err := setProtoField(m, fd, val); err != nil {
			rangeErr = err
		}
	})
	return rangeErr
}

// setProtoField writes a single Lua value into message field fd, handling
// repeated fields (Lua array table) and nested messages recursively.
func setProtoField(m protoreflect.Message, fd protoreflect.FieldDescriptor, val lua.LValue) error {
	if fd.IsList() {
		arr, ok := val.(*lua.LTable)
		if !ok {
			return oops.Code("LUABRIDGE_FIELD_TYPE").
				With("field", string(fd.Name())).
				Errorf("repeated field %s expects a table", fd.Name())
		}
		list := m.Mutable(fd).List()
		var elemErr error
		arr.ForEach(func(_, elem lua.LValue) {
			if elemErr != nil {
				return
			}
			pv, err := luaListElement(list, fd, elem)
			if err != nil {
				elemErr = err
				return
			}
			list.Append(pv)
		})
		return elemErr
	}
	pv, err := luaToProtoValue(fd, val, func() protoreflect.Message {
		return m.NewField(fd).Message()
	})
	if err != nil {
		return err
	}
	m.Set(fd, pv)
	return nil
}

// luaListElement converts one Lua value into a protoreflect.Value suitable for
// appending to list. Message elements are constructed via list.NewElement so
// the element gets the list's element type (not the list type).
func luaListElement(list protoreflect.List, fd protoreflect.FieldDescriptor, val lua.LValue) (protoreflect.Value, error) {
	return luaToProtoValue(fd, val, func() protoreflect.Message {
		return list.NewElement().Message()
	})
}

// luaToProtoValue converts a single Lua value into the protoreflect.Value for
// field fd. For nested message fields it recurses through LuaTableToProto,
// allocating the destination message via newMessage (which differs for singular
// fields vs. list elements).
func luaToProtoValue(fd protoreflect.FieldDescriptor, val lua.LValue, newMessage func() protoreflect.Message) (protoreflect.Value, error) {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return protoreflect.ValueOfBool(lua.LVAsBool(val)), nil
	case protoreflect.StringKind:
		return protoreflect.ValueOfString(lua.LVAsString(val)), nil
	case protoreflect.BytesKind:
		return protoreflect.ValueOfBytes([]byte(lua.LVAsString(val))), nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		n, err := luaNumber(fd, val)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfInt32(int32(n)), nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		n, err := luaNumber(fd, val)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfInt64(int64(n)), nil
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		n, err := luaNumber(fd, val)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfUint32(uint32(n)), nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		n, err := luaNumber(fd, val)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfUint64(uint64(n)), nil
	case protoreflect.FloatKind:
		n, err := luaNumber(fd, val)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfFloat32(float32(n)), nil
	case protoreflect.DoubleKind:
		n, err := luaNumber(fd, val)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfFloat64(n), nil
	case protoreflect.EnumKind:
		n, err := luaNumber(fd, val)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfEnum(protoreflect.EnumNumber(int32(n))), nil
	case protoreflect.MessageKind, protoreflect.GroupKind:
		sub, ok := val.(*lua.LTable)
		if !ok {
			return protoreflect.Value{}, oops.Code("LUABRIDGE_FIELD_TYPE").
				With("field", string(fd.Name())).
				Errorf("message field %s expects a table", fd.Name())
		}
		nested := newMessage()
		if err := LuaTableToProto(sub, nested.Interface()); err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfMessage(nested), nil
	default:
		return protoreflect.Value{}, oops.Code("LUABRIDGE_FIELD_KIND").
			With("field", string(fd.Name())).
			With("kind", fd.Kind().String()).
			Errorf("unsupported field kind %s for %s", fd.Kind(), fd.Name())
	}
}

// luaNumber extracts a float64 from a numeric Lua value, returning a typed error
// when the value is not a number (e.g. a string was passed for an int field).
func luaNumber(fd protoreflect.FieldDescriptor, val lua.LValue) (float64, error) {
	n, ok := val.(lua.LNumber)
	if !ok {
		return 0, oops.Code("LUABRIDGE_FIELD_TYPE").
			With("field", string(fd.Name())).
			Errorf("numeric field %s expects a number, got %s", fd.Name(), val.Type())
	}
	return float64(n), nil
}

// luaContext returns the context carried on the Lua state, falling back to
// context.Background(). It is a luabridge-local copy of hostfunc.luaContext
// (which is package-private to hostfunc); the generated bindings need their own.
func luaContext(L *lua.LState) context.Context {
	if ctx := L.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// pushBridgeError pushes (nil, "<status message>") onto the Lua stack and
// returns 2, mirroring hostfunc.pushError. It runs the error through
// status.FromError so a gRPC error surfaces only its status message — never the
// inner detail (table names, query fragments) past the host trust boundary.
func pushBridgeError(L *lua.LState, err error) int {
	msg := status.Convert(err).Message()
	L.Push(lua.LNil)
	L.Push(lua.LString(msg))
	return 2
}
