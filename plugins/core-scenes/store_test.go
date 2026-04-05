// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSceneRowHoldsAllFields(t *testing.T) {
	loc := "loc-1"
	tmpl := "tmpl-1"
	timeout := 300
	ended := time.Now()
	archived := time.Now()

	row := &SceneRow{
		ID:              "scene-1",
		Title:           "Adventure Begins",
		Description:     "A thrilling start",
		LocationID:      &loc,
		OwnerID:         "owner-1",
		State:           "active",
		PoseOrder:       "free",
		Visibility:      "open",
		IdleTimeoutSecs: &timeout,
		TemplateID:      &tmpl,
		ContentWarnings: []string{"violence"},
		Tags:            []string{"action"},
		CreatedAt:       time.Now(),
		EndedAt:         &ended,
		ArchivedAt:      &archived,
	}

	assert.Equal(t, "scene-1", row.ID)
	assert.Equal(t, "Adventure Begins", row.Title)
	assert.Equal(t, "A thrilling start", row.Description)
	assert.Equal(t, &loc, row.LocationID)
	assert.Equal(t, "owner-1", row.OwnerID)
	assert.Equal(t, "active", row.State)
	assert.Equal(t, "free", row.PoseOrder)
	assert.Equal(t, "open", row.Visibility)
	assert.Equal(t, &timeout, row.IdleTimeoutSecs)
	assert.Equal(t, &tmpl, row.TemplateID)
	assert.Equal(t, []string{"violence"}, row.ContentWarnings)
	assert.Equal(t, []string{"action"}, row.Tags)
	assert.NotZero(t, row.CreatedAt)
	assert.Equal(t, &ended, row.EndedAt)
	assert.Equal(t, &archived, row.ArchivedAt)
}

func TestSceneRowDefaultsNilForOptionalFields(t *testing.T) {
	row := &SceneRow{
		ID:      "scene-2",
		Title:   "Minimal Scene",
		OwnerID: "owner-2",
		State:   "active",
	}

	assert.Nil(t, row.LocationID)
	assert.Nil(t, row.TemplateID)
	assert.Nil(t, row.IdleTimeoutSecs)
	assert.Nil(t, row.EndedAt)
	assert.Nil(t, row.ArchivedAt)
}

func TestParticipantRowHoldsAllFields(t *testing.T) {
	origin := "loc-origin"
	vote := true

	row := &ParticipantRow{
		SceneID:          "scene-1",
		CharacterID:      "char-1",
		Role:             "owner",
		OriginLocationID: &origin,
		JoinedAt:         time.Now(),
		PublishVote:      &vote,
	}

	assert.Equal(t, "scene-1", row.SceneID)
	assert.Equal(t, "char-1", row.CharacterID)
	assert.Equal(t, "owner", row.Role)
	assert.Equal(t, &origin, row.OriginLocationID)
	assert.NotZero(t, row.JoinedAt)
	assert.Equal(t, &vote, row.PublishVote)
}

func TestParticipantRowDefaultsNilForOptionalFields(t *testing.T) {
	row := &ParticipantRow{
		SceneID:     "scene-1",
		CharacterID: "char-2",
		Role:        "member",
		JoinedAt:    time.Now(),
	}

	assert.Nil(t, row.OriginLocationID)
	assert.Nil(t, row.PublishVote)
}

func TestItoaConvertsIntsToStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected string
	}{
		{"converts single digit", 1, "1"},
		{"converts two digits", 10, "10"},
		{"converts zero", 0, "0"},
		{"converts larger number", 123, "123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, itoa(tt.input))
		})
	}
}

func TestMigrationsEmbedContainsUpFile(t *testing.T) {
	entries, err := migrationsFS.ReadDir("migrations")
	assert.NoError(t, err)

	var found bool
	for _, e := range entries {
		if e.Name() == "000001_scenes.up.sql" {
			found = true
			break
		}
	}
	assert.True(t, found, "embedded migrations should contain 000001_scenes.up.sql")
}

func TestSceneStoreRequiresDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Real DB tests will use testcontainers when wired up.
	// For now, verify the store cannot connect to a bogus address.
	_, err := NewSceneStore(t.Context(), "postgres://invalid:5432/nonexistent")
	assert.Error(t, err)
}
