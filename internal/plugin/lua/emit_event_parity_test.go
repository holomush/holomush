// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// emitEventLuaKeyOverrides maps EmitEvent field names whose Lua table key
// diverges from the snake_case derivation. Stream is read from the Lua key
// "subject" (the F5 legacy naming — EmitEvent keeps the field name Stream while
// the plugin-return table uses subject; see parseEmitEvents). Every other field
// uses goFieldToLuaKey.
var emitEventLuaKeyOverrides = map[string]string{
	"Stream": "subject",
}

// TestParseEmitEventsCarriesEveryField is the Lua-runtime half of the EmitEvent
// parity guard (holomush-av954). It builds a Lua emit table with a sentinel
// under each field's conventional key, runs it through parseEmitEvents, and
// asserts every exported EmitEvent field round-trips. A field added to
// EmitEvent without a matching read in parseEmitEvents stays zero and fails
// here — the Lua analogue of the proto EmitEvent dropping Sensitive (the
// crypto-downgrade / runtime-symmetry bug holomush-av954 fixed on the binary
// side).
func TestParseEmitEventsCarriesEveryField(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	inner := state.NewTable()
	var want pluginsdk.EmitEvent
	wv := reflect.ValueOf(&want).Elem()
	typ := wv.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		key := emitEventLuaKeyOverrides[f.Name]
		if key == "" {
			key = goFieldToLuaKey(f.Name)
		}
		switch f.Type.Kind() {
		case reflect.String:
			sentinel := "sentinel::" + f.Name
			state.SetField(inner, key, lua.LString(sentinel))
			wv.Field(i).SetString(sentinel)
		case reflect.Bool:
			state.SetField(inner, key, lua.LBool(true))
			wv.Field(i).SetBool(true)
		default:
			t.Fatalf("EmitEvent.%s has unsupported kind %s — extend this guard AND ensure the "+
				"new field is read in parseEmitEvents (holomush-av954)", f.Name, f.Type.Kind())
		}
	}

	outer := state.NewTable()
	outer.Append(inner)

	emits, validationErrs := (&Host{}).parseEmitEvents(outer)
	require.Empty(t, validationErrs, "sentinel emit table MUST parse without validation errors")
	require.Len(t, emits, 1)
	require.Equal(t, want, emits[0],
		"every EmitEvent field MUST be carried from the Lua emit table by parseEmitEvents; a "+
			"mismatch means a field was added without wiring the Lua read path (holomush-av954)")
}
