// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	worldv1 "github.com/holomush/holomush/pkg/proto/holomush/world/v1"
)

// Compile-time interface check.
var _ worldv1.WorldServiceServer = (*GRPCServer)(nil)

// GRPCServer adapts world.Service to the WorldService gRPC contract.
// It is intended to be registered on an in-process gRPC server so binary
// plugins can query the world model via InProcessConn.
type GRPCServer struct {
	worldv1.UnimplementedWorldServiceServer
	svc *Service
}

// NewGRPCServer creates a GRPCServer backed by the given Service.
func NewGRPCServer(svc *Service) *GRPCServer {
	return &GRPCServer{svc: svc}
}

// GetLocation retrieves a location by ID.
func (s *GRPCServer) GetLocation(ctx context.Context, req *worldv1.GetLocationRequest) (*worldv1.GetLocationResponse, error) {
	locID, err := ulid.ParseStrict(req.GetLocationId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid location_id: %v", err)
	}

	subjectID := access.CharacterSubject(req.GetSubjectId())
	loc, err := s.svc.GetLocation(ctx, subjectID, locID)
	if err != nil {
		return nil, mapWorldError(err)
	}

	return &worldv1.GetLocationResponse{
		Location: locationToProto(loc),
	}, nil
}

// GetCharacter retrieves a character by ID.
func (s *GRPCServer) GetCharacter(ctx context.Context, req *worldv1.GetCharacterRequest) (*worldv1.GetCharacterResponse, error) {
	charID, err := ulid.ParseStrict(req.GetCharacterId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid character_id: %v", err)
	}

	subjectID := access.CharacterSubject(req.GetSubjectId())
	char, err := s.svc.GetCharacter(ctx, subjectID, charID)
	if err != nil {
		return nil, mapWorldError(err)
	}

	return &worldv1.GetCharacterResponse{
		Character: characterToProto(char),
	}, nil
}

// ListCharactersAtLocation returns all characters at a location.
func (s *GRPCServer) ListCharactersAtLocation(ctx context.Context, req *worldv1.ListCharactersAtLocationRequest) (*worldv1.ListCharactersAtLocationResponse, error) {
	locID, err := ulid.ParseStrict(req.GetLocationId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid location_id: %v", err)
	}

	subjectID := access.CharacterSubject(req.GetSubjectId())
	chars, err := s.svc.GetCharactersByLocation(ctx, subjectID, locID, ListOptions{})
	if err != nil {
		return nil, mapWorldError(err)
	}

	protoChars := make([]*worldv1.CharacterInfo, len(chars))
	for i, c := range chars {
		protoChars[i] = characterToProto(c)
	}

	return &worldv1.ListCharactersAtLocationResponse{Characters: protoChars}, nil
}

// ListExits returns all exits from a location.
func (s *GRPCServer) ListExits(ctx context.Context, req *worldv1.ListExitsRequest) (*worldv1.ListExitsResponse, error) {
	locID, err := ulid.ParseStrict(req.GetLocationId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid location_id: %v", err)
	}

	subjectID := access.CharacterSubject(req.GetSubjectId())
	exits, err := s.svc.GetExitsByLocation(ctx, subjectID, locID)
	if err != nil {
		return nil, mapWorldError(err)
	}

	protoExits := make([]*worldv1.ExitInfo, len(exits))
	for i, e := range exits {
		protoExits[i] = exitToProto(e)
	}

	return &worldv1.ListExitsResponse{Exits: protoExits}, nil
}

func locationToProto(loc *Location) *worldv1.LocationInfo {
	info := &worldv1.LocationInfo{
		Id:          loc.ID.String(),
		Name:        loc.Name,
		Description: loc.Description,
		Type:        string(loc.Type),
	}
	if loc.OwnerID != nil {
		info.OwnerId = loc.OwnerID.String()
	}
	return info
}

func characterToProto(c *Character) *worldv1.CharacterInfo {
	info := &worldv1.CharacterInfo{
		Id:          c.ID.String(),
		PlayerId:    c.PlayerID.String(),
		Name:        c.Name,
		Description: c.Description,
	}
	if c.LocationID != nil {
		info.LocationId = c.LocationID.String()
	}
	return info
}

func exitToProto(e *Exit) *worldv1.ExitInfo {
	return &worldv1.ExitInfo{
		Id:             e.ID.String(),
		Name:           e.Name,
		FromLocationId: e.FromLocationID.String(),
		ToLocationId:   e.ToLocationID.String(),
		Bidirectional:  e.Bidirectional,
		ReturnName:     e.ReturnName,
		Locked:         e.Locked,
	}
}

// mapWorldError converts oops-coded domain errors to gRPC status errors.
func mapWorldError(err error) error {
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return status.Errorf(codes.Internal, "%v", err)
	}

	code, ok2 := oopsErr.Code().(string)
	if !ok2 {
		return status.Errorf(codes.Internal, "%v", err)
	}
	switch {
	case strings.HasSuffix(code, "_NOT_FOUND"):
		return status.Errorf(codes.NotFound, "%v", err)
	case strings.HasSuffix(code, "_ACCESS_DENIED"):
		return status.Errorf(codes.PermissionDenied, "%v", err)
	default:
		return status.Errorf(codes.Internal, "%v", err)
	}
}
