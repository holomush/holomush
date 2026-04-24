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
	cases := []string{"say", "scene.pose", "scene.lifecycle.created"}
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
		{"hyphen", "scene-pose"},
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
