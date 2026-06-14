// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"context"
	"errors"

	"github.com/oklog/ulid/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// errNilWorldResult is the sentinel logged when a WorldQuerier returns a nil
// entity with no error. A well-behaved querier signals absence via
// world.ErrNotFound (mapped to codes.NotFound below), never a nil-without-error,
// so this is a contract violation: the worldServer fails closed with an opaque
// Internal rather than nil-dereferencing the result.
var errNilWorldResult = errors.New("world querier returned a nil result without an error")

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

	querier := s.host.WorldQuerier(s.pluginName)
	if querier == nil {
		return nil, status.Errorf(codes.Unimplemented, "world query not supported")
	}
	loc, err := querier.GetLocation(ctx, id)
	if err != nil {
		if errors.Is(err, world.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "not found")
		}
		errutil.LogErrorContext(ctx, "world.query_location failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	if loc == nil {
		errutil.LogErrorContext(ctx, "world.query_location returned nil without error", errNilWorldResult, "plugin", s.pluginName)
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

	querier := s.host.WorldQuerier(s.pluginName)
	if querier == nil {
		return nil, status.Errorf(codes.Unimplemented, "world query not supported")
	}
	char, err := querier.GetCharacter(ctx, id)
	if err != nil {
		if errors.Is(err, world.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "not found")
		}
		errutil.LogErrorContext(ctx, "world.query_character failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	if char == nil {
		errutil.LogErrorContext(ctx, "world.query_character returned nil without error", errNilWorldResult, "plugin", s.pluginName)
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
	querier := s.host.WorldQuerier(s.pluginName)
	if querier == nil {
		return nil, status.Errorf(codes.Unimplemented, "world query not supported")
	}
	chars, err := querier.GetCharactersByLocation(ctx, id, opts)
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

	querier := s.host.WorldQuerier(s.pluginName)
	if querier == nil {
		return nil, status.Errorf(codes.Unimplemented, "world query not supported")
	}
	obj, err := querier.GetObject(ctx, id)
	if err != nil {
		if errors.Is(err, world.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "not found")
		}
		errutil.LogErrorContext(ctx, "world.query_object failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	if obj == nil {
		errutil.LogErrorContext(ctx, "world.query_object returned nil without error", errNilWorldResult, "plugin", s.pluginName)
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

// FindLocation resolves a location by display name within the calling plugin's
// subject scope, mirroring the Lua holomush.find_location(name) host function
// (worldMutator.FindLocationByName). Returns the matched location's id and
// name. Maps world.ErrNotFound to codes.NotFound; other inner errors are logged
// and replaced with a generic Internal (no leak per grpc-errors.md).
//
// Note: FindLocation requires WorldMutator() to be non-nil (unlike the
// WorldQuerier-backed query RPCs). When WorldMutator() is nil this RPC returns
// codes.Unimplemented.
func (s *worldServer) FindLocation(ctx context.Context, req *hostv1.FindLocationRequest) (*hostv1.FindLocationResponse, error) {
	mutator := s.host.WorldMutator()
	if mutator == nil {
		return nil, status.Errorf(codes.Unimplemented, "world lookup not supported")
	}
	subject := access.PluginSubject(s.pluginName)
	loc, err := mutator.FindLocationByName(ctx, subject, req.GetName())
	if err != nil {
		if errors.Is(err, world.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "not found")
		}
		errutil.LogErrorContext(ctx, "world.find_location failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	if loc == nil {
		errutil.LogErrorContext(ctx, "world.find_location returned nil without error", errNilWorldResult, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	return &hostv1.FindLocationResponse{
		Id:   loc.ID.String(),
		Name: loc.Name,
	}, nil
}

// worldMutationServer implements holomush.plugin.host.v1.WorldMutationService.
// It delegates CreateLocation, CreateExit, and CreateObject to the plugin-
// subject-stamped WorldMutator obtained from the HostCapabilities port,
// mirroring the existing Lua holomush.create_location / create_exit /
// create_object host functions so both runtimes share identical semantics
// (INV-PLUGIN-49).
type worldMutationServer struct {
	hostv1.UnimplementedWorldMutationServiceServer
	hostCapabilityBase
}

// NewWorldMutationServer builds the WorldMutationService capability server
// bound to base. Returned as the narrow service interface so callers cannot
// reach into the struct.
func NewWorldMutationServer(base hostCapabilityBase) hostv1.WorldMutationServiceServer {
	return &worldMutationServer{hostCapabilityBase: base}
}

// CreateLocation creates a new location with the given name, description, and
// validated location type, mirroring the Lua holomush.create_location(name,
// description, type) host function (mutator.CreateLocation). Returns the new
// location's id and name. Returns Unimplemented when the world mutator is not
// configured; returns InvalidArgument for an invalid location type; other inner
// errors are logged and replaced with a generic Internal (no leak per
// grpc-errors.md).
func (s *worldMutationServer) CreateLocation(ctx context.Context, req *hostv1.CreateLocationRequest) (*hostv1.CreateLocationResponse, error) {
	mutator := s.host.WorldMutator()
	if mutator == nil {
		return nil, status.Errorf(codes.Unimplemented, "world mutation not supported")
	}

	locType := world.LocationType(req.GetType())
	if err := locType.Validate(); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid location type")
	}

	loc := &world.Location{
		ID:          idgen.New(),
		Name:        req.GetName(),
		Description: req.GetDescription(),
		Type:        locType,
	}
	subject := access.PluginSubject(s.pluginName)
	if err := mutator.CreateLocation(ctx, subject, loc); err != nil {
		errutil.LogErrorContext(ctx, "world.create_location failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	return &hostv1.CreateLocationResponse{
		Id:   loc.ID.String(),
		Name: loc.Name,
	}, nil
}

// CreateExit creates a new exit from one location to another, optionally
// bidirectional with a return name, mirroring the Lua
// holomush.create_exit(from_id, to_id, name, opts) host function
// (mutator.CreateExit). Returns the new exit's id and name. Returns
// Unimplemented when the world mutator is not configured; returns
// InvalidArgument for unparseable ULIDs; other inner errors are logged and
// replaced with a generic Internal (no leak per grpc-errors.md).
func (s *worldMutationServer) CreateExit(ctx context.Context, req *hostv1.CreateExitRequest) (*hostv1.CreateExitResponse, error) {
	mutator := s.host.WorldMutator()
	if mutator == nil {
		return nil, status.Errorf(codes.Unimplemented, "world mutation not supported")
	}

	fromID, err := ulid.Parse(req.GetFromId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid from_id")
	}
	toID, err := ulid.Parse(req.GetToId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid to_id")
	}

	exit := &world.Exit{
		ID:             idgen.New(),
		FromLocationID: fromID,
		ToLocationID:   toID,
		Name:           req.GetName(),
		Visibility:     world.VisibilityAll,
		Bidirectional:  req.GetBidirectional(),
		ReturnName:     req.GetReturnName(),
	}
	subject := access.PluginSubject(s.pluginName)
	if err := mutator.CreateExit(ctx, subject, exit); err != nil {
		errutil.LogErrorContext(ctx, "world.create_exit failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	return &hostv1.CreateExitResponse{
		Id:   exit.ID.String(),
		Name: exit.Name,
	}, nil
}

// CreateObject creates a new object with exactly one containment placement
// (location, holding character, or containing object) and optional description,
// mirroring the Lua holomush.create_object(name, opts) host function
// (mutator.CreateObject). Returns the new object's id and name. Returns
// Unimplemented when the world mutator is not configured; returns
// InvalidArgument for unparseable placement ULIDs or a missing placement;
// other inner errors are logged and replaced with a generic Internal (no leak
// per grpc-errors.md).
func (s *worldMutationServer) CreateObject(ctx context.Context, req *hostv1.CreateObjectRequest) (*hostv1.CreateObjectResponse, error) {
	mutator := s.host.WorldMutator()
	if mutator == nil {
		return nil, status.Errorf(codes.Unimplemented, "world mutation not supported")
	}

	var containment world.Containment
	switch p := req.GetPlacement().(type) {
	case *hostv1.CreateObjectRequest_LocationId:
		id, err := ulid.Parse(p.LocationId)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid location_id")
		}
		containment = world.InLocation(id)
	case *hostv1.CreateObjectRequest_CharacterId:
		id, err := ulid.Parse(p.CharacterId)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid character_id")
		}
		containment = world.HeldByCharacter(id)
	case *hostv1.CreateObjectRequest_ContainerId:
		id, err := ulid.Parse(p.ContainerId)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid container_id")
		}
		containment = world.ContainedInObject(id)
	default:
		return nil, status.Errorf(codes.InvalidArgument, "placement is required")
	}

	obj, err := world.NewObjectWithID(idgen.New(), req.GetName(), containment)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid object")
	}
	obj.Description = req.GetDescription()

	subject := access.PluginSubject(s.pluginName)
	if err := mutator.CreateObject(ctx, subject, obj); err != nil {
		errutil.LogErrorContext(ctx, "world.create_object failed", err, "plugin", s.pluginName)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	return &hostv1.CreateObjectResponse{
		Id:   obj.ID.String(),
		Name: obj.Name,
	}, nil
}
