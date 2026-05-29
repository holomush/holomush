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
// character_id, InvokedAs -> invoked_as, ConnectionID -> connection_id). It
// handles acronym→word boundaries (HTTPMethod -> http_method, URLPath ->
// url_path) so that a future field does not silently derive a malformed key.
// A field whose actual Lua key still diverges from this derivation fails the
// parity test below — the intended tripwire: conform the key or teach this
// helper.
func goFieldToLuaKey(name string) string {
	var b strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			prevIsLowerOrDigit := i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]))
			// Acronym→word boundary: an uppercase run ending where the next
			// rune is lowercase (the M in HTTPMethod, the P in URLPath).
			acronymBoundary := i > 0 && unicode.IsUpper(runes[i-1]) &&
				i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if prevIsLowerOrDigit || acronymBoundary {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// fillStringSentinels sets every exported string field of the struct pointed to
// by ptr to a unique, field-name-derived sentinel, and fails the test on any
// exported field whose kind it cannot fill — forcing the author of a new
// non-string CommandRequest field to teach this guard and confront the parity
// contract (holomush-peqfu). It mirrors fillStringFieldsWithSentinels in
// pkg/plugin; the duplication is unavoidable across the package boundary.
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
			t.Fatalf("CommandRequest.%s has unsupported kind %s — extend this guard AND "+
				"ensure the new field is set in buildCommandRequestTable (holomush-peqfu)",
				f.Name, f.Type.Kind())
		}
		v.Field(i).SetString("sentinel::" + f.Name)
	}
}

// TestGoFieldToLuaKeyDerivesSnakeCaseKeys locks the snake_case derivation,
// including the acronym→word boundary handling that keeps the parity guard's
// key inference correct for plausible future field names.
func TestGoFieldToLuaKeyDerivesSnakeCaseKeys(t *testing.T) {
	tests := []struct {
		name  string
		field string
		want  string
	}{
		{"single word lowercases", "Command", "command"},
		{"trailing acronym splits", "CharacterID", "character_id"},
		{"two words split", "CharacterName", "character_name"},
		{"verb-preposition splits", "InvokedAs", "invoked_as"},
		{"leading acronym then word", "HTTPMethod", "http_method"},
		{"leading acronym then word URL", "URLPath", "url_path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := goFieldToLuaKey(tt.field); got != tt.want {
				t.Errorf("goFieldToLuaKey(%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

// TestBuildCommandRequestTableCarriesEveryField is the Lua-runtime half of the
// CommandRequest parity guard (holomush-peqfu). It fills every exported field
// with a sentinel and asserts each one reaches the Lua command table under its
// conventional snake_case key. A field added to CommandRequest without a
// matching state.SetField in buildCommandRequestTable is invisible to Lua
// command handlers — the same structural omission as holomush-dble7, but on the
// in-process runtime. This guard turns that omission into a test failure.
func TestBuildCommandRequestTableCarriesEveryField(t *testing.T) {
	var cmd pluginsdk.CommandRequest
	fillStringSentinels(t, &cmd)

	state := lua.NewState()
	defer state.Close()

	tbl := (&Host{}).buildCommandRequestTable(state, cmd)

	typ := reflect.TypeOf(cmd)
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
