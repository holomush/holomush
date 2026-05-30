// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// cursorFieldName is the one Event field intentionally excluded from the
// delivery parity guard — see EventToProto for why (history/tail token, never
// delivered, no Lua equivalent).
const cursorFieldName = "Cursor"

// fillEventSentinels sets every exported Event field (except the allowlisted
// Cursor) to a distinct, round-trippable, non-zero sentinel so a field dropped
// by EventToProto/EventFromProto is detectable after a round-trip. It fails the
// test on any exported field whose kind it cannot fill — forcing the author of
// a new Event field to teach this helper AND, in doing so, confront the parity
// contract for every plugin runtime (holomush-av954).
func fillEventSentinels(t *testing.T, e *Event) {
	t.Helper()
	v := reflect.ValueOf(e).Elem()
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Name == cursorFieldName {
			continue // ALLOWLISTED: not a delivery field (see EventToProto).
		}
		switch {
		case f.Type.Kind() == reflect.String:
			v.Field(i).SetString("sentinel::" + f.Name)
		case f.Type.Kind() == reflect.Int64:
			v.Field(i).SetInt(0x5EE7) // distinct non-zero; round-trips as int64.
		case f.Type == reflect.TypeOf(ActorKind(0)):
			// ActorSystem is non-zero and round-trips ("system" both ways), so a
			// dropped ActorKind field (which would leave the zero ActorCharacter)
			// is detectable.
			v.Field(i).Set(reflect.ValueOf(ActorSystem))
		default:
			t.Fatalf("Event.%s has unsupported kind %s — extend fillEventSentinels AND ensure the "+
				"new field is marshaled through EventToProto/EventFromProto AND the Lua "+
				"buildEventTable (holomush-av954)", f.Name, f.Type.Kind())
		}
	}
}

// TestEventProtoRoundTripCarriesEveryField is the binary-runtime half of the
// Event parity guard (holomush-av954). It fills every exported field (bar the
// allowlisted Cursor) with a sentinel and asserts the value survives
// event -> proto -> event. A field wired in only one direction — or missing
// from the proto message — is dropped by the round-trip and fails this test.
// This generalizes the CommandRequest class-killer (holomush-peqfu) to the
// host→plugin event-delivery boundary.
func TestEventProtoRoundTripCarriesEveryField(t *testing.T) {
	var e Event
	fillEventSentinels(t, &e)

	got := EventFromProto(EventToProto(e))

	require.Equal(t, e, got,
		"every Event field (except the allowlisted Cursor) MUST survive the "+
			"event->proto->event round trip; a mismatch means a field is wired in one "+
			"direction but not the other, or is missing from the proto message (holomush-av954)")
}

// fillEmitEventSentinels sets every exported EmitEvent field to a distinct,
// round-trippable, non-zero sentinel so a dropped field is detectable after a
// round-trip, failing on any kind it cannot fill — forcing the author of a new
// EmitEvent field to teach this helper and confront the parity contract for
// every plugin runtime (holomush-av954).
func fillEmitEventSentinels(t *testing.T, e *EmitEvent) {
	t.Helper()
	v := reflect.ValueOf(e).Elem()
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
			v.Field(i).SetBool(true) // non-zero so a dropped bool (false) is detectable.
		default:
			t.Fatalf("EmitEvent.%s has unsupported kind %s — extend this helper AND ensure the "+
				"new field is marshaled through EmitEventToProto/EmitEventFromProto AND the Lua "+
				"parseEmitEvents path (holomush-av954)", f.Name, f.Type.Kind())
		}
	}
}

// TestEmitEventProtoRoundTripCarriesEveryField is the binary-runtime half of the
// EmitEvent parity guard (holomush-av954). It fills every exported field with a
// sentinel and asserts the value survives emit -> proto -> emit. Before
// holomush-av954 the proto EmitEvent had no sensitive field, so a binary
// plugin's return-value sensitive emit was silently dropped to plaintext on the
// wire while the Lua runtime honored it — a crypto downgrade and a plugin-
// runtime-symmetry violation. This guard makes any such omission a test failure.
func TestEmitEventProtoRoundTripCarriesEveryField(t *testing.T) {
	var e EmitEvent
	fillEmitEventSentinels(t, &e)

	got := EmitEventFromProto(EmitEventToProto(e))

	require.Equal(t, e, got,
		"every EmitEvent field MUST survive the emit->proto->emit round trip; a mismatch means "+
			"a field is wired in one direction but not the other, or is missing from the proto "+
			"message (holomush-av954)")
}

// fillEmitIntentSentinels fills every exported EmitIntent field with a distinct
// non-zero, round-trippable sentinel, failing on any kind it cannot fill —
// forcing the author of a new EmitIntent field to teach this helper and
// confront the parity contract for the binary active-emit boundary
// (holomush-av954).
func fillEmitIntentSentinels(t *testing.T, intent *EmitIntent) {
	t.Helper()
	v := reflect.ValueOf(intent).Elem()
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
			t.Fatalf("EmitIntent.%s has unsupported kind %s — extend this helper AND ensure the "+
				"new field is marshaled through EmitIntentToEmitRequest/EmitIntentFromEmitRequest "+
				"(holomush-av954)", f.Name, f.Type.Kind())
		}
	}
}

// TestEmitIntentEmitRequestRoundTripCarriesEveryField guards the binary
// active-emit boundary (holomush-av954): EmitIntent crosses to the host as a
// PluginHostServiceEmitEventRequest on the send side (EventSink.Emit) and back
// on the receive side (the plugin host service). It fills every exported field
// with a sentinel and asserts the value survives intent -> request -> intent. A
// field wired in only one direction — Sensitive was dropped on the send side
// before this change — is caught here.
func TestEmitIntentEmitRequestRoundTripCarriesEveryField(t *testing.T) {
	var intent EmitIntent
	fillEmitIntentSentinels(t, &intent)

	got := EmitIntentFromEmitRequest(EmitIntentToEmitRequest(intent))

	require.Equal(t, intent, got,
		"every EmitIntent field MUST survive the intent->request->intent round trip; a mismatch "+
			"means a field is wired in one direction but not the other on the binary active-emit "+
			"path (holomush-av954)")
}
