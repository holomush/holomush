// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"reflect"
	"testing"

	lua "github.com/yuin/gopher-lua"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// eventCursorFieldName is the one Event field intentionally excluded from the
// Lua delivery parity guard: Cursor is the history/tail-read pagination token
// (see pluginsdk.EventToProto), never delivered to a handler, and Lua has no
// tail-read equivalent.
const eventCursorFieldName = "Cursor"

// fillEventTableSentinels fills every exported Event field (except the
// allowlisted Cursor) with a non-zero value so its presence in the Lua table is
// detectable, and fails on any kind it cannot fill — forcing the author of a
// new Event field to teach this guard and confront the per-runtime parity
// contract (holomush-av954).
func fillEventTableSentinels(t *testing.T, e *pluginsdk.Event) {
	t.Helper()
	v := reflect.ValueOf(e).Elem()
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() || f.Name == eventCursorFieldName {
			continue
		}
		switch {
		case f.Type.Kind() == reflect.String:
			v.Field(i).SetString("sentinel::" + f.Name)
		case f.Type.Kind() == reflect.Int64:
			v.Field(i).SetInt(0x5EE7)
		case f.Type == reflect.TypeOf(pluginsdk.ActorKind(0)):
			v.Field(i).Set(reflect.ValueOf(pluginsdk.ActorSystem))
		default:
			t.Fatalf("Event.%s has unsupported kind %s — extend this guard AND set the field "+
				"in buildEventTable (holomush-av954)", f.Name, f.Type.Kind())
		}
	}
}

// TestBuildEventTableCarriesEveryField is the Lua-runtime half of the Event
// parity guard (holomush-av954). It fills every exported field (bar the
// allowlisted Cursor) and asserts each reaches the Lua event table under its
// conventional snake_case key. A field added to Event without a matching
// state.SetField in buildEventTable is invisible to Lua event handlers — the
// host→plugin analogue of holomush-dble7. This guard turns that omission into a
// test failure. String fields are additionally checked by value; non-string
// fields are checked for presence (a dropped key reads back as Lua nil).
func TestBuildEventTableCarriesEveryField(t *testing.T) {
	var e pluginsdk.Event
	fillEventTableSentinels(t, &e)

	state := lua.NewState()
	defer state.Close()

	tbl := (&Host{}).buildEventTable(state, e)

	v := reflect.ValueOf(e)
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() || f.Name == eventCursorFieldName {
			continue
		}
		key := goFieldToLuaKey(f.Name)
		got := tbl.RawGetString(key)
		if got.Type() == lua.LTNil {
			t.Errorf("Event.%s MUST appear in the Lua event table under key %q, got nil — a "+
				"missing field means the Lua runtime never receives it (holomush-av954)", f.Name, key)
			continue
		}
		// Value-check every field, not just strings: a buildEventTable bug that
		// writes a wrong-but-non-nil value (e.g. a zero timestamp or the wrong
		// actor-kind string) must fail too. want is the Lua representation each
		// non-Cursor field's sentinel marshals to.
		var want string
		switch {
		case f.Type.Kind() == reflect.String:
			want = "sentinel::" + f.Name
		case f.Type.Kind() == reflect.Int64:
			want = lua.LNumber(v.Field(i).Int()).String()
		case f.Type == reflect.TypeOf(pluginsdk.ActorKind(0)):
			want = v.Field(i).Interface().(pluginsdk.ActorKind).String()
		}
		if got.String() != want {
			t.Errorf("Event.%s MUST carry its value into the Lua event table under key %q, got %q want %q "+
				"(holomush-av954)", f.Name, key, got.String(), want)
		}
	}
}
