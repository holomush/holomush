// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// TestSessionStreamsRequestToLuaArgsCarriesEveryField is the Lua-runtime half of
// the SessionStreamsRequest parity guard (holomush-av954). The Lua runtime passes
// SessionStreamsRequest to on_session_subscribe as POSITIONAL arguments (not a
// keyed table), so this is an ordered presence guard: the arg slice MUST hold
// exactly one value per exported struct field, and every field's sentinel MUST
// appear. A field added to SessionStreamsRequest without appending it to
// sessionStreamsRequestToLuaArgs is dropped from the Lua call and fails here —
// the same structural omission holomush-peqfu killed for CommandRequest, on the
// session-subscribe boundary.
func TestSessionStreamsRequestToLuaArgsCarriesEveryField(t *testing.T) {
	var req plugins.SessionStreamsRequest
	fillSessionStreamsSentinels(t, &req)

	args := sessionStreamsRequestToLuaArgs(req)

	// Exported fields only — sessionStreamsRequestToLuaArgs (and the fill helper)
	// skip unexported fields, so the positional contract is over exported fields.
	typ := reflect.TypeOf(req)
	var exported []reflect.StructField
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).IsExported() {
			exported = append(exported, typ.Field(i))
		}
	}

	require.Len(t, args, len(exported),
		"on_session_subscribe receives one positional arg per exported SessionStreamsRequest field; "+
			"a count mismatch means a field was added without wiring the Lua call (holomush-av954)")

	// Positional, not set-membership: on_session_subscribe args are positional,
	// so a field REORDER (which swaps which value Lua receives as character_id vs
	// player_id) must fail too — assert each arg matches its field IN ORDER.
	for j, f := range exported {
		require.Equal(t, "sentinel::"+f.Name, args[j].String(),
			"SessionStreamsRequest.%s MUST be the on_session_subscribe arg at position %d; a "+
				"missing or reordered field corrupts what the Lua runtime receives (holomush-av954)", f.Name, j)
	}
}

// fillSessionStreamsSentinels fills every exported string field of req with a
// field-name-derived sentinel, failing on any non-string field so a new field's
// author must teach this guard and confront the per-runtime parity contract.
func fillSessionStreamsSentinels(t *testing.T, ptr any) {
	t.Helper()
	v := reflect.ValueOf(ptr).Elem()
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Type.Kind() != reflect.String {
			t.Fatalf("SessionStreamsRequest.%s has unsupported kind %s — extend this guard AND "+
				"wire the field into sessionStreamsRequestToLuaArgs (holomush-av954)", f.Name, f.Type.Kind())
		}
		v.Field(i).SetString("sentinel::" + f.Name)
	}
}
