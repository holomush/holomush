// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// fillStructSentinels sets every exported field of the struct pointed to by ptr
// to a distinct sentinel value, failing on any kind it cannot fill — so the
// author of a new field must teach this helper and confront the parity contract.
// It matches the "sentinel::" convention used by the sibling parity guards
// (holomush-av954).
func fillStructSentinels(t *testing.T, ptr any) {
	t.Helper()
	v := reflect.ValueOf(ptr).Elem()
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		switch f.Type.Kind() {
		case reflect.String:
			v.Field(i).SetString("sentinel::" + f.Name)
		case reflect.Bool:
			v.Field(i).SetBool(true)
		default:
			t.Fatalf("%s.%s has unsupported kind %s — extend this helper (holomush-av954)",
				typ.Name(), f.Name, f.Type.Kind())
		}
	}
}

// requireEmitIntentFullyPopulated asserts every exported EmitIntent field is
// non-zero. EmitIntent forks per runtime via two host-side construction sites
// (emitIntentFromEmitEvent here for the Lua + binary return-value paths;
// emitIntentFromEmitRequest in goplugin for the binary active-emit RPC). Given a
// fully-populated input, a correctly-wired construction site leaves no field
// zero; a field added to EmitIntent without wiring stays zero and fails here.
func requireEmitIntentFullyPopulated(t *testing.T, intent pluginsdk.EmitIntent) {
	t.Helper()
	v := reflect.ValueOf(intent)
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		require.False(t, v.Field(i).IsZero(),
			"EmitIntent.%s MUST be populated by emitIntentFromEmitEvent; a zero value means a "+
				"field was added without wiring the EmitEvent->EmitIntent path (holomush-av954)", f.Name)
	}
}

// TestEmitIntentFromEmitEventCarriesEveryField guards the EmitEvent->EmitIntent
// construction site that backs the Lua and binary return-value emit paths
// (holomush-av954).
func TestEmitIntentFromEmitEventCarriesEveryField(t *testing.T) {
	var e pluginsdk.EmitEvent
	fillStructSentinels(t, &e)

	requireEmitIntentFullyPopulated(t, emitIntentFromEmitEvent(e))
}
