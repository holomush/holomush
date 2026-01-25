// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"log/slog"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world"
)

// WorldService defines the world service methods needed by the adapter.
// This interface allows decoupling from the concrete world.Service type.
type WorldService interface {
	GetLocation(ctx context.Context, subjectID string, id ulid.ULID) (*world.Location, error)
	GetCharacter(ctx context.Context, subjectID string, id ulid.ULID) (*world.Character, error)
	GetCharactersByLocation(ctx context.Context, subjectID string, locationID ulid.ULID) ([]*world.Character, error)
	GetObject(ctx context.Context, subjectID string, id ulid.ULID) (*world.Object, error)
}

// WorldQuerierAdapter wraps a WorldService to provide plugin access with
// system-level authorization. Each plugin gets its own adapter instance
// with a subject ID like "system:plugin:<name>".
type WorldQuerierAdapter struct {
	service    WorldService
	pluginName string
}

// NewWorldQuerierAdapter creates a new adapter for the given plugin.
// The adapter uses "system:plugin:<pluginName>" as the authorization subject.
// Panics if svc is nil or pluginName is empty.
func NewWorldQuerierAdapter(svc WorldService, pluginName string) *WorldQuerierAdapter {
	if svc == nil {
		panic("hostfunc.NewWorldQuerierAdapter: service is required")
	}
	if pluginName == "" {
		panic("hostfunc.NewWorldQuerierAdapter: pluginName is required")
	}
	return &WorldQuerierAdapter{
		service:    svc,
		pluginName: pluginName,
	}
}

// SubjectID returns the authorization subject for this plugin.
func (a *WorldQuerierAdapter) SubjectID() string {
	return "system:plugin:" + a.pluginName
}

// GetLocation retrieves a location by ID with plugin authorization.
func (a *WorldQuerierAdapter) GetLocation(ctx context.Context, id ulid.ULID) (*world.Location, error) {
	loc, err := a.service.GetLocation(ctx, a.SubjectID(), id)
	if err != nil {
		return nil, oops.Code("PLUGIN_QUERY_FAILED").
			With("plugin", a.pluginName).
			With("entity_type", "location").
			Wrapf(err, "get location")
	}
	if loc == nil {
		slog.Warn("service returned nil without error, treating as not found",
			"plugin", a.pluginName,
			"entity_type", "location",
			"entity_id", id.String())
		return nil, oops.Code("PLUGIN_QUERY_FAILED").
			With("plugin", a.pluginName).
			With("entity_type", "location").
			Wrap(world.ErrNotFound)
	}
	return loc, nil
}

// GetCharacter retrieves a character by ID with plugin authorization.
func (a *WorldQuerierAdapter) GetCharacter(ctx context.Context, id ulid.ULID) (*world.Character, error) {
	char, err := a.service.GetCharacter(ctx, a.SubjectID(), id)
	if err != nil {
		return nil, oops.Code("PLUGIN_QUERY_FAILED").
			With("plugin", a.pluginName).
			With("entity_type", "character").
			Wrapf(err, "get character")
	}
	if char == nil {
		slog.Warn("service returned nil without error, treating as not found",
			"plugin", a.pluginName,
			"entity_type", "character",
			"entity_id", id.String())
		return nil, oops.Code("PLUGIN_QUERY_FAILED").
			With("plugin", a.pluginName).
			With("entity_type", "character").
			Wrap(world.ErrNotFound)
	}
	return char, nil
}

// GetCharactersByLocation retrieves all characters at a location with plugin authorization.
func (a *WorldQuerierAdapter) GetCharactersByLocation(ctx context.Context, locationID ulid.ULID) ([]*world.Character, error) {
	chars, err := a.service.GetCharactersByLocation(ctx, a.SubjectID(), locationID)
	if err != nil {
		return nil, oops.Code("PLUGIN_QUERY_FAILED").
			With("plugin", a.pluginName).
			With("entity_type", "characters_by_location").
			Wrapf(err, "get characters by location")
	}
	// Normalize nil slice to empty slice for consistent behavior.
	// Unlike single-entity methods, nil is technically valid for slices,
	// but we normalize for consistency and to detect potential service issues.
	if chars == nil {
		slog.Debug("service returned nil slice, normalizing to empty",
			"plugin", a.pluginName,
			"location_id", locationID.String())
		return []*world.Character{}, nil
	}
	return chars, nil
}

// GetObject retrieves an object by ID with plugin authorization.
func (a *WorldQuerierAdapter) GetObject(ctx context.Context, id ulid.ULID) (*world.Object, error) {
	obj, err := a.service.GetObject(ctx, a.SubjectID(), id)
	if err != nil {
		return nil, oops.Code("PLUGIN_QUERY_FAILED").
			With("plugin", a.pluginName).
			With("entity_type", "object").
			Wrapf(err, "get object")
	}
	if obj == nil {
		slog.Warn("service returned nil without error, treating as not found",
			"plugin", a.pluginName,
			"entity_type", "object",
			"entity_id", id.String())
		return nil, oops.Code("PLUGIN_QUERY_FAILED").
			With("plugin", a.pluginName).
			With("entity_type", "object").
			Wrap(world.ErrNotFound)
	}
	return obj, nil
}

// Compile-time interface check.
var _ WorldQuerier = (*WorldQuerierAdapter)(nil)
