// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
)

func TestNewSubjectAcceptsValidPatterns(t *testing.T) {
	cases := []string{
		"events.main.location.01JABC",
		"events.main.scene.01JABC.ic",
		"events.*.scene.*.lifecycle",
		"events.main.scene.>",
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			got, err := eventbus.NewSubject(s)
			require.NoError(t, err)
			require.Equal(t, eventbus.Subject(s), got)
		})
	}
}

func TestNewSubjectRejectsInvalidPatterns(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"missing events prefix", "main.location.01JABC"},
		{"empty token between dots", "events..main.location.X"},
		{"tilde character", "events.main.location.~"},
		{"> not last", "events.>.scene.ic"},
		{"too deep", "events." + strings.Repeat("a.", 16)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := eventbus.NewSubject(tc.in)
			require.Error(t, err)
			require.True(t, errors.Is(err, eventbus.ErrInvalidSubject), "got %v", err)
		})
	}
}

func TestNewTypeAcceptsValidPatterns(t *testing.T) {
	// Plain dot-segmented types and the plugin-qualified "plugin-name:verb"
	// form (spec §7.1) are both valid.
	cases := []string{
		"say",
		"scene.pose",
		"scene.lifecycle.created",
		"core-communication:say", // plugin-qualified form
		"location_state",
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			got, err := eventbus.NewType(s)
			require.NoError(t, err)
			require.Equal(t, eventbus.Type(s), got)
		})
	}
}

func TestNewTypeRejectsInvalidPatterns(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"uppercase start", "Scene.pose"},
		{"trailing dot", "scene."},
		{"double dot", "scene..pose"},
		{"mixed dot and colon", "core.communication:say"},
		{"mixed colon and dot", "core:communication.say"},
		{"multiple colons", "core-communication:say:extra"},
		{"trailing colon", "core-communication:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := eventbus.NewType(tc.in)
			require.Error(t, err)
			require.True(t, errors.Is(err, eventbus.ErrInvalidType), "got %v", err)
		})
	}
}

func TestMustSubjectPanicsOnInvalid(t *testing.T) {
	require.Panics(t, func() { eventbus.MustSubject("not-prefixed") })
}

func TestMustSubjectAcceptsValid(t *testing.T) {
	require.NotPanics(t, func() { eventbus.MustSubject("events.main.scene.>") })
}

func TestEventSeqIsZeroWhenUninitialized(t *testing.T) {
	t.Parallel()
	e := eventbus.Event{}
	assert.Equal(t, uint64(0), e.Seq)
}

func TestEventSeqPreservesLiteralValueWhenSet(t *testing.T) {
	t.Parallel()
	e := eventbus.Event{Seq: 42}
	assert.Equal(t, uint64(42), e.Seq)
}

// TestEventbus_NewEvent_AutoStampsULID verifies that eventbus.NewEvent()
// returns an Event with a non-zero ULID ID and that the provided fields are
// preserved (holomush-jxo8.7.53).
func TestEventbus_NewEvent_AutoStampsULID(t *testing.T) {
	t.Parallel()
	subj := eventbus.MustSubject("events.main.scene.01TEST")
	typ, err := eventbus.NewType("test.event")
	require.NoError(t, err)
	actor := eventbus.Actor{Kind: eventbus.ActorKindSystem}
	payload := []byte(`{"hello":"world"}`)

	ev := eventbus.NewEvent(subj, typ, actor, payload)

	assert.NotEmpty(t, ev.ID, "NewEvent must stamp a non-zero ULID")
	assert.Equal(t, subj, ev.Subject)
	assert.Equal(t, typ, ev.Type)
	assert.Equal(t, actor.Kind, ev.Actor.Kind)
	assert.Equal(t, payload, ev.Payload)
	assert.False(t, ev.Timestamp.IsZero(), "NewEvent must stamp a non-zero Timestamp")

	// Two consecutive NewEvent calls MUST mint distinct ULIDs (monotonic).
	ev2 := eventbus.NewEvent(subj, typ, actor, payload)
	assert.NotEqual(t, ev.ID, ev2.ID, "each NewEvent call must mint a unique ULID")
}
