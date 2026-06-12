// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"context"
	"errors"

	"github.com/oklog/ulid/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// worldServer implements holomush.plugin.host.v1.WorldQueryService. It delegates
// all four query operations to the plugin-subject-stamped WorldQuerier obtained
// from the HostCapabilities port, mirroring the existing Lua
// holomush.query_location / query_character / query_location_characters /
// query_object host functions so both runtimes share identical semantics
// (INV-PLUGIN-49).
type worldServer struct {
	hostv1.UnimplementedWorldQueryServiceServer
	hostCapabilityBase
}

// NewWorldQueryServer builds the WorldQueryService capability server bound to
// base. Returned as the narrow service interface so callers cannot reach into
// the struct.
func NewWorldQueryServer(base hostCapabilityBase) hostv1.WorldQueryServiceServer {
	return &worldServer{hostCapabilityBase: base}
}

// QueryLocation returns a location's identity, name, description, and type by
// ULID, mirroring the Lua holomush.query_location(location_id) host function.
// Maps world.ErrNotFound to codes.NotFound. Returns InvalidArgument for
// unparseable IDs; returns a generic Internal on unexpected failures (no inner
// error detail leaks per grpc-errors.md).
func (s *worldServer) QueryLocation(ctx context.Context, req *hostv1.QueryLocationRequest) (*hostv1.QueryLocationResponse, error) {
	id, err := ulid.Parse(req.GetLocationId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid location id")
	}

	loc, err := s.host.WorldQuerier(s.pluginName).GetLocation(ctx, id)
	if err != nil {
		if errors.Is(err, world.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "not found")
		}
		errutil.LogErrorContext(ctx, "world.query_location failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	return &hostv1.QueryLocationResponse{
		Id:          loc.ID.String(),
		Name:        loc.Name,
		Description: loc.Description,
		Type:        string(loc.Type),
	}, nil
}

// QueryCharacter returns a character's identity, player, name, description, and
// optional current location by ULID, mirroring the Lua
// holomush.query_character(character_id) host function. Maps world.ErrNotFound
// to codes.NotFound. LocationID is omitted from the response when nil (matching
// the Lua handler's behavior). Returns InvalidArgument for unparseable IDs;
// returns a generic Internal on unexpected failures (no inner error detail
// leaks per grpc-errors.md).
func (s *worldServer) QueryCharacter(ctx context.Context, req *hostv1.QueryCharacterRequest) (*hostv1.QueryCharacterResponse, error) {
	id, err := ulid.Parse(req.GetCharacterId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid character id")
	}

	char, err := s.host.WorldQuerier(s.pluginName).GetCharacter(ctx, id)
	if err != nil {
		if errors.Is(err, world.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "not found")
		}
		errutil.LogErrorContext(ctx, "world.query_character failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	resp := &hostv1.QueryCharacterResponse{
		Id:          char.ID.String(),
		PlayerId:    char.PlayerID.String(),
		Name:        char.Name,
		Description: char.Description,
	}
	if char.LocationID != nil {
		resp.LocationId = char.LocationID.String()
	}
	return resp, nil
}

// QueryLocationCharacters returns the lightweight (id, name) set of characters
// at a location with optional limit/offset pagination, mirroring the Lua
// holomush.query_location_characters(location_id, opts) host function. Returns
// InvalidArgument for unparseable IDs; returns a generic Internal on unexpected
// failures (no inner error detail leaks per grpc-errors.md).
func (s *worldServer) QueryLocationCharacters(ctx context.Context, req *hostv1.QueryLocationCharactersRequest) (*hostv1.QueryLocationCharactersResponse, error) {
	id, err := ulid.Parse(req.GetLocationId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid location id")
	}

	opts := world.ListOptions{
		Limit:  int(req.GetLimit()),
		Offset: int(req.GetOffset()),
	}
	chars, err := s.host.WorldQuerier(s.pluginName).GetCharactersByLocation(ctx, id, opts)
	if err != nil {
		errutil.LogErrorContext(ctx, "world.query_location_characters failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	summaries := make([]*hostv1.CharacterSummary, 0, len(chars))
	for _, c := range chars {
		summaries = append(summaries, &hostv1.CharacterSummary{
			Id:   c.ID.String(),
			Name: c.Name,
		})
	}
	return &hostv1.QueryLocationCharactersResponse{Characters: summaries}, nil
}

// QueryObject returns an object's identity, description, container flag,
// containment placement, and owner by ULID, mirroring the Lua
// holomush.query_object(object_id) host function. Maps world.ErrNotFound to
// codes.NotFound. Optional containment fields are omitted when nil (matching
// the Lua handler's behavior). Returns InvalidArgument for unparseable IDs;
// returns a generic Internal on unexpected failures (no inner error detail
// leaks per grpc-errors.md).
func (s *worldServer) QueryObject(ctx context.Context, req *hostv1.QueryObjectRequest) (*hostv1.QueryObjectResponse, error) {
	id, err := ulid.Parse(req.GetObjectId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid object id")
	}

	obj, err := s.host.WorldQuerier(s.pluginName).GetObject(ctx, id)
	if err != nil {
		if errors.Is(err, world.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "not found")
		}
		errutil.LogErrorContext(ctx, "world.query_object failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	containment := obj.Containment()
	resp := &hostv1.QueryObjectResponse{
		Id:              obj.ID.String(),
		Name:            obj.Name,
		Description:     obj.Description,
		IsContainer:     obj.IsContainer,
		ContainmentType: string(containment.Type()),
	}
	if obj.LocationID() != nil {
		resp.LocationId = obj.LocationID().String()
	}
	if obj.HeldByCharacterID() != nil {
		resp.HeldByCharacterId = obj.HeldByCharacterID().String()
	}
	if obj.ContainedInObjectID() != nil {
		resp.ContainedInObjectId = obj.ContainedInObjectID().String()
	}
	if obj.OwnerID != nil {
		resp.OwnerId = obj.OwnerID.String()
	}
	return resp, nil
}
