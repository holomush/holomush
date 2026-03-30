// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"log/slog"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Compile-time interface check.
var _ pluginv1.PluginHostServiceServer = (*PluginHostService)(nil)

// PluginHostService is a gRPC server that wraps ServiceProxy, allowing binary
// plugins to call back to the host for world queries, event emission, and KV storage.
type PluginHostService struct {
	pluginv1.UnimplementedPluginHostServiceServer
	proxy  plugins.ServiceProxy
	logger *slog.Logger
}

// NewPluginHostService creates a PluginHostService backed by the given ServiceProxy.
func NewPluginHostService(proxy plugins.ServiceProxy, logger *slog.Logger) *PluginHostService {
	if logger == nil {
		logger = slog.Default()
	}
	return &PluginHostService{
		proxy:  proxy,
		logger: logger,
	}
}

// QueryLocation retrieves a location by ID.
func (s *PluginHostService) QueryLocation(ctx context.Context, req *pluginv1.QueryLocationRequest) (*pluginv1.QueryLocationResponse, error) {
	result, err := s.proxy.QueryLocation(ctx, req.GetSubjectId(), req.GetLocationId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "query location: %v", err)
	}
	if result == nil {
		return &pluginv1.QueryLocationResponse{}, nil
	}
	return &pluginv1.QueryLocationResponse{
		Location: &pluginv1.LocationInfo{
			Id:          result.ID,
			Name:        result.Name,
			Description: result.Description,
			Type:        result.Type,
			OwnerId:     result.OwnerID,
		},
	}, nil
}

// QueryCharacter retrieves a character by ID.
func (s *PluginHostService) QueryCharacter(ctx context.Context, req *pluginv1.HostQueryCharacterRequest) (*pluginv1.HostQueryCharacterResponse, error) {
	result, err := s.proxy.QueryCharacter(ctx, req.GetSubjectId(), req.GetCharacterId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "query character: %v", err)
	}
	if result == nil {
		return &pluginv1.HostQueryCharacterResponse{}, nil
	}
	return &pluginv1.HostQueryCharacterResponse{
		Character: characterResultToProto(result),
	}, nil
}

// QueryLocationCharacters returns all characters present at a location.
func (s *PluginHostService) QueryLocationCharacters(ctx context.Context, req *pluginv1.HostQueryLocationCharactersRequest) (*pluginv1.HostQueryLocationCharactersResponse, error) {
	results, err := s.proxy.QueryLocationCharacters(ctx, req.GetSubjectId(), req.GetLocationId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "query location characters: %v", err)
	}
	chars := make([]*pluginv1.HostCharacterInfo, len(results))
	for i := range results {
		chars[i] = characterResultToProto(&results[i])
	}
	return &pluginv1.HostQueryLocationCharactersResponse{Characters: chars}, nil
}

// EmitEvent publishes an event to a stream.
func (s *PluginHostService) EmitEvent(ctx context.Context, req *pluginv1.HostEmitEventRequest) (*pluginv1.HostEmitEventResponse, error) {
	err := s.proxy.EmitEvent(ctx, req.GetStream(), req.GetEventType(), req.GetPayload())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "emit event: %v", err)
	}
	return &pluginv1.HostEmitEventResponse{}, nil
}

// Log writes a log message through the host's logging system.
func (s *PluginHostService) Log(ctx context.Context, req *pluginv1.HostLogRequest) (*pluginv1.HostLogResponse, error) {
	s.proxy.Log(ctx, req.GetLevel(), req.GetMessage())
	return &pluginv1.HostLogResponse{}, nil
}

// KVGet retrieves a value from the plugin's key-value store.
func (s *PluginHostService) KVGet(ctx context.Context, req *pluginv1.HostKVGetRequest) (*pluginv1.HostKVGetResponse, error) {
	value, found, err := s.proxy.KVGet(ctx, req.GetPluginName(), req.GetKey())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "kv get: %v", err)
	}
	return &pluginv1.HostKVGetResponse{Value: value, Found: found}, nil
}

// KVSet stores a value in the plugin's key-value store.
func (s *PluginHostService) KVSet(ctx context.Context, req *pluginv1.HostKVSetRequest) (*pluginv1.HostKVSetResponse, error) {
	err := s.proxy.KVSet(ctx, req.GetPluginName(), req.GetKey(), req.GetValue())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "kv set: %v", err)
	}
	return &pluginv1.HostKVSetResponse{}, nil
}

// KVDelete removes a value from the plugin's key-value store.
func (s *PluginHostService) KVDelete(ctx context.Context, req *pluginv1.HostKVDeleteRequest) (*pluginv1.HostKVDeleteResponse, error) {
	err := s.proxy.KVDelete(ctx, req.GetPluginName(), req.GetKey())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "kv delete: %v", err)
	}
	return &pluginv1.HostKVDeleteResponse{}, nil
}

// characterResultToProto converts a CharacterResult to a proto HostCharacterInfo.
func characterResultToProto(r *plugins.CharacterResult) *pluginv1.HostCharacterInfo {
	return &pluginv1.HostCharacterInfo{
		Id:          r.ID,
		PlayerId:    r.PlayerID,
		Name:        r.Name,
		Description: r.Description,
		LocationId:  r.LocationID,
	}
}
