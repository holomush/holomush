// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"reflect"
	"strings"
	"testing"
	"unicode"

	lua "github.com/yuin/gopher-lua"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// goFieldToLuaKey derives the snake_case Lua table key for a Go struct field
// name, matching the convention buildCommandRequestTable uses (CharacterID ->
// character_id, InvokedAs -> invoked_as, ConnectionID -> connection_id). A new
// field whose Lua key does not follow this convention will fail the parity test
// below, which is the intended tripwire: the author must either conform the key
// or consciously teach this helper.
func goFieldToLuaKey(name string) string {
	var b strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1])) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// TestBuildCommandRequestTableCarriesEveryField is the Lua-runtime half of the
// CommandRequest parity guard (holomush-peqfu). It fills every exported field
// with a sentinel and asserts each one reaches the Lua command table under its
// conventional snake_case key. A field added to CommandRequest without a
// matching state.SetField in buildCommandRequestTable is invisible to Lua
// command handlers — the same structural omission as holomush-dble7, but on the
// in-process runtime. This guard turns that omission into a build failure.
func TestBuildCommandRequestTableCarriesEveryField(t *testing.T) {
	var cmd pluginsdk.CommandRequest
	v := reflect.ValueOf(&cmd).Elem()
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Type.Kind() != reflect.String {
			t.Fatalf("CommandRequest.%s has unsupported kind %s — extend this guard AND "+
				"ensure the new field is set in buildCommandRequestTable (holomush-peqfu)",
				f.Name, f.Type.Kind())
		}
		v.Field(i).SetString("sentinel::" + f.Name)
	}

	state := lua.NewState()
	defer state.Close()

	tbl := (&Host{}).buildCommandRequestTable(state, cmd)

	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		key := goFieldToLuaKey(f.Name)
		got := tbl.RawGetString(key)
		if got.String() != "sentinel::"+f.Name {
			t.Errorf("CommandRequest.%s MUST appear in the Lua command table under key %q, "+
				"got %q — a missing field means the Lua runtime never receives it "+
				"(holomush-peqfu)", f.Name, key, got.String())
		}
	}
}
