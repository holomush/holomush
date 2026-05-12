// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package readstream_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/readstream"
	"github.com/holomush/holomush/internal/eventbus"
)

const testGame = "main"

func TestBuildSubjects_EmptyContextsReturnsGameWildcard(t *testing.T) {
	subjects := readstream.BuildSubjects(nil, testGame)
	require.Len(t, subjects, 1)
	assert.Equal(t, eventbus.Subject("events.main.>"), subjects[0])
}

func TestBuildSubjects_EmptySliceReturnsGameWildcard(t *testing.T) {
	subjects := readstream.BuildSubjects([]readstream.ContextRef{}, testGame)
	require.Len(t, subjects, 1)
	assert.Equal(t, eventbus.Subject("events.main.>"), subjects[0])
}

func TestBuildSubjects_SingleContextArity1(t *testing.T) {
	contexts := []readstream.ContextRef{
		{Type: "scene", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}},
	}
	subjects := readstream.BuildSubjects(contexts, testGame)
	require.Len(t, subjects, 1)
	assert.Equal(t, eventbus.Subject("events.main.scene.01ARZ3NDEKTSV4RRFFQ69G5FAV.>"), subjects[0])
}

func TestBuildSubjects_DMArity2(t *testing.T) {
	contexts := []readstream.ContextRef{
		{Type: "dm", IDs: []string{"01A", "01B"}},
	}
	subjects := readstream.BuildSubjects(contexts, testGame)
	require.Len(t, subjects, 1)
	// dm with two IDs: events.<game>.dm.<id1>.<id2>.>
	assert.Equal(t, eventbus.Subject("events.main.dm.01A.01B.>"), subjects[0])
}

func TestBuildSubjects_MultipleContexts(t *testing.T) {
	contexts := []readstream.ContextRef{
		{Type: "scene", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}},
		{Type: "location", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAX"}},
	}
	subjects := readstream.BuildSubjects(contexts, testGame)
	require.Len(t, subjects, 2)
	assert.Equal(t, eventbus.Subject("events.main.scene.01ARZ3NDEKTSV4RRFFQ69G5FAV.>"), subjects[0])
	assert.Equal(t, eventbus.Subject("events.main.location.01ARZ3NDEKTSV4RRFFQ69G5FAX.>"), subjects[1])
}

func TestBuildSubjects_PreservesInputOrder(t *testing.T) {
	contexts := []readstream.ContextRef{
		{Type: "character", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}},
		{Type: "scene", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAX"}},
		{Type: "location", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAZ"}},
	}
	subjects := readstream.BuildSubjects(contexts, testGame)
	require.Len(t, subjects, 3)
	assert.Contains(t, string(subjects[0]), "character")
	assert.Contains(t, string(subjects[1]), "scene")
	assert.Contains(t, string(subjects[2]), "location")
}

func TestBuildSubjects_DoesNotMutateInput(t *testing.T) {
	contexts := []readstream.ContextRef{
		{Type: "scene", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}},
	}
	origType := contexts[0].Type
	origID := contexts[0].IDs[0]
	_ = readstream.BuildSubjects(contexts, testGame)
	assert.Equal(t, origType, contexts[0].Type)
	assert.Equal(t, origID, contexts[0].IDs[0])
}
