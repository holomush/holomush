// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"context"

	"github.com/oklog/ulid/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/pkg/errutil"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// propertyServer implements holomush.plugin.host.v1.PropertyService. It reads
// and writes entity properties through the runtime-neutral HostCapabilities port,
// mirroring the existing Lua holomush.get_property / holomush.set_property path so
// both runtimes share identical semantics (INV-PLUGIN-49).
type propertyServer struct {
	hostv1.UnimplementedPropertyServiceServer
	hostCapabilityBase
}

// NewPropertyServer builds the PropertyService capability server bound to base.
// Returned as the narrow service interface so callers cannot reach into the struct.
func NewPropertyServer(base hostCapabilityBase) hostv1.PropertyServiceServer {
	return &propertyServer{hostCapabilityBase: base}
}

// GetProperty resolves a property by name via PropertyDefinition.Get and returns
// its string value. Maps to the same code path as the Lua holomush.get_property
// host function. Returns InvalidArgument for unknown property names or unparseable
// entity IDs; returns a generic Internal on unexpected failures (no inner error
// detail leaks to the caller per grpc-errors.md).
func (s *propertyServer) GetProperty(ctx context.Context, req *hostv1.GetPropertyRequest) (*hostv1.GetPropertyResponse, error) {
	def, ok := s.host.PropertyDefinition(req.GetProperty())
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "unknown property")
	}

	entityID, err := ulid.Parse(req.GetEntityId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid entity id")
	}

	querier := s.host.WorldQuerier(s.pluginName)
	if querier == nil {
		return nil, status.Errorf(codes.Unimplemented, "property service not supported")
	}
	val, err := def.Get(ctx, querier, req.GetEntityType(), entityID)
	if err != nil {
		errutil.LogErrorContext(ctx, "property.get failed", err, "plugin", s.pluginName, "property", req.GetProperty())
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	return &hostv1.GetPropertyResponse{Value: val}, nil
}

// SetProperty resolves a property by name via PropertyDefinition.Set and writes
// the given value. The ABAC subject is derived from the plugin name via
// access.PluginSubject (mirroring the Lua withMutatorContext path). Returns
// InvalidArgument for unknown property names or unparseable entity IDs; returns
// a generic Internal on unexpected failures (no inner error detail leaks per
// grpc-errors.md).
func (s *propertyServer) SetProperty(ctx context.Context, req *hostv1.SetPropertyRequest) (*hostv1.SetPropertyResponse, error) {
	def, ok := s.host.PropertyDefinition(req.GetProperty())
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "unknown property")
	}

	entityID, err := ulid.Parse(req.GetEntityId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid entity id")
	}

	querier := s.host.WorldQuerier(s.pluginName)
	if querier == nil {
		return nil, status.Errorf(codes.Unimplemented, "property service not supported")
	}
	subjectID := access.PluginSubject(s.pluginName)
	if err := def.Set(ctx, querier, s.host.WorldMutator(), subjectID, req.GetEntityType(), entityID, req.GetValue()); err != nil {
		errutil.LogErrorContext(ctx, "property.set failed", err, "plugin", s.pluginName, "property", req.GetProperty())
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	return &hostv1.SetPropertyResponse{}, nil
}
