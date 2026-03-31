// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//nolint:wrapcheck // transparent middleware wrapper; re-wrapping errors adds no value
package plugins

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/holomush/holomush/internal/content"
)

// Compile-time interface check.
var _ ServiceProxy = (*ServiceProxyMiddleware)(nil)

// ServiceProxyMiddleware wraps a ServiceProxy with OpenTelemetry tracing and metrics.
type ServiceProxyMiddleware struct {
	next   ServiceProxy
	tracer trace.Tracer

	callDuration metric.Float64Histogram
	callCount    metric.Int64Counter
	callErrors   metric.Int64Counter
}

// NewServiceProxyMiddleware wraps a ServiceProxy with OTel instrumentation.
func NewServiceProxyMiddleware(next ServiceProxy, tp trace.TracerProvider, mp metric.MeterProvider) (*ServiceProxyMiddleware, error) {
	tracer := tp.Tracer("holomush.plugin.service")
	meter := mp.Meter("holomush.plugin.service")

	callDuration, err := meter.Float64Histogram("plugin_service_duration_seconds",
		metric.WithDescription("Duration of plugin service proxy calls"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	callCount, err := meter.Int64Counter("plugin_service_calls_total",
		metric.WithDescription("Total plugin service proxy calls"),
	)
	if err != nil {
		return nil, err
	}

	callErrors, err := meter.Int64Counter("plugin_service_errors_total",
		metric.WithDescription("Total plugin service proxy errors"),
	)
	if err != nil {
		return nil, err
	}

	return &ServiceProxyMiddleware{
		next:         next,
		tracer:       tracer,
		callDuration: callDuration,
		callCount:    callCount,
		callErrors:   callErrors,
	}, nil
}

// instrument wraps an operation with tracing and metrics. Errors are passed
// through without wrapping since they originate from the underlying
// ServiceProxy which already uses oops for structured context.
func (m *ServiceProxyMiddleware) instrument(ctx context.Context, operation string, fn func(context.Context) error) error {
	ctx, span := m.tracer.Start(ctx, "plugin.service."+operation)
	defer span.End()

	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start).Seconds()

	attrs := metric.WithAttributes(attribute.String("operation", operation))
	m.callDuration.Record(ctx, duration, attrs)
	m.callCount.Add(ctx, 1, attrs)

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		m.callErrors.Add(ctx, 1, attrs)
	}
	return err
}

// QueryLocation instruments a location query. See ServiceProxy.QueryLocation.
func (m *ServiceProxyMiddleware) QueryLocation(ctx context.Context, subjectID, id string) (result *LocationResult, err error) {
	err = m.instrument(ctx, "query_location", func(ctx context.Context) error {
		result, err = m.next.QueryLocation(ctx, subjectID, id)
		return err
	})
	return
}

// QueryCharacter instruments a character query. See ServiceProxy.QueryCharacter.
func (m *ServiceProxyMiddleware) QueryCharacter(ctx context.Context, subjectID, id string) (result *CharacterResult, err error) {
	err = m.instrument(ctx, "query_character", func(ctx context.Context) error {
		result, err = m.next.QueryCharacter(ctx, subjectID, id)
		return err
	})
	return
}

// QueryLocationCharacters instruments a location characters query. See ServiceProxy.QueryLocationCharacters.
func (m *ServiceProxyMiddleware) QueryLocationCharacters(ctx context.Context, subjectID, locationID string) (results []CharacterResult, err error) {
	err = m.instrument(ctx, "query_location_characters", func(ctx context.Context) error {
		results, err = m.next.QueryLocationCharacters(ctx, subjectID, locationID)
		return err
	})
	return
}

// QueryObject instruments an object query. See ServiceProxy.QueryObject.
func (m *ServiceProxyMiddleware) QueryObject(ctx context.Context, subjectID, id string) (result *ObjectResult, err error) {
	err = m.instrument(ctx, "query_object", func(ctx context.Context) error {
		result, err = m.next.QueryObject(ctx, subjectID, id)
		return err
	})
	return
}

// FindLocation instruments a location search. See ServiceProxy.FindLocation.
func (m *ServiceProxyMiddleware) FindLocation(ctx context.Context, subjectID, name string) (result *LocationResult, err error) {
	err = m.instrument(ctx, "find_location", func(ctx context.Context) error {
		result, err = m.next.FindLocation(ctx, subjectID, name)
		return err
	})
	return
}

// GetCharactersByLocation instruments a characters-by-location query. See ServiceProxy.GetCharactersByLocation.
func (m *ServiceProxyMiddleware) GetCharactersByLocation(ctx context.Context, subjectID, locationID string) (results []CharacterResult, err error) {
	err = m.instrument(ctx, "get_characters_by_location", func(ctx context.Context) error {
		results, err = m.next.GetCharactersByLocation(ctx, subjectID, locationID)
		return err
	})
	return
}

// GetObjectsByLocation instruments an objects-by-location query. See ServiceProxy.GetObjectsByLocation.
func (m *ServiceProxyMiddleware) GetObjectsByLocation(ctx context.Context, subjectID, locationID string) (results []ObjectResult, err error) {
	err = m.instrument(ctx, "get_objects_by_location", func(ctx context.Context) error {
		results, err = m.next.GetObjectsByLocation(ctx, subjectID, locationID)
		return err
	})
	return
}

// CreateLocation instruments location creation. See ServiceProxy.CreateLocation.
func (m *ServiceProxyMiddleware) CreateLocation(ctx context.Context, subjectID, name, description, locationType string) (result *LocationResult, err error) {
	err = m.instrument(ctx, "create_location", func(ctx context.Context) error {
		result, err = m.next.CreateLocation(ctx, subjectID, name, description, locationType)
		return err
	})
	return
}

// CreateExit instruments exit creation. See ServiceProxy.CreateExit.
func (m *ServiceProxyMiddleware) CreateExit(ctx context.Context, subjectID, fromID, toID, name string, opts CreateExitOpts) error {
	return m.instrument(ctx, "create_exit", func(ctx context.Context) error {
		return m.next.CreateExit(ctx, subjectID, fromID, toID, name, opts)
	})
}

// CreateObject instruments object creation. See ServiceProxy.CreateObject.
func (m *ServiceProxyMiddleware) CreateObject(ctx context.Context, subjectID, name, description string) (result *ObjectResult, err error) {
	err = m.instrument(ctx, "create_object", func(ctx context.Context) error {
		result, err = m.next.CreateObject(ctx, subjectID, name, description)
		return err
	})
	return
}

// UpdateLocation instruments location update. See ServiceProxy.UpdateLocation.
func (m *ServiceProxyMiddleware) UpdateLocation(ctx context.Context, subjectID, id, name, description string) error {
	return m.instrument(ctx, "update_location", func(ctx context.Context) error {
		return m.next.UpdateLocation(ctx, subjectID, id, name, description)
	})
}

// UpdateCharacterDescription instruments character description update. See ServiceProxy.UpdateCharacterDescription.
func (m *ServiceProxyMiddleware) UpdateCharacterDescription(ctx context.Context, subjectID, characterID, description string) error {
	return m.instrument(ctx, "update_character_description", func(ctx context.Context) error {
		return m.next.UpdateCharacterDescription(ctx, subjectID, characterID, description)
	})
}

// SetProperty instruments property set. See ServiceProxy.SetProperty.
func (m *ServiceProxyMiddleware) SetProperty(ctx context.Context, subjectID, parentType, parentID, key, value string) error {
	return m.instrument(ctx, "set_property", func(ctx context.Context) error {
		return m.next.SetProperty(ctx, subjectID, parentType, parentID, key, value)
	})
}

// GetProperty instruments property retrieval. See ServiceProxy.GetProperty.
func (m *ServiceProxyMiddleware) GetProperty(ctx context.Context, subjectID, parentType, parentID, key string) (value string, err error) {
	err = m.instrument(ctx, "get_property", func(ctx context.Context) error {
		value, err = m.next.GetProperty(ctx, subjectID, parentType, parentID, key)
		return err
	})
	return
}

// FindPropertyByPrefix instruments property prefix search. See ServiceProxy.FindPropertyByPrefix.
func (m *ServiceProxyMiddleware) FindPropertyByPrefix(ctx context.Context, prefix string) (results []PropertyInfo, err error) {
	err = m.instrument(ctx, "find_property_by_prefix", func(ctx context.Context) error {
		results, err = m.next.FindPropertyByPrefix(ctx, prefix)
		return err
	})
	return
}

// ListPropertiesByParent instruments property listing. See ServiceProxy.ListPropertiesByParent.
func (m *ServiceProxyMiddleware) ListPropertiesByParent(ctx context.Context, subjectID, parentType, parentID string) (results []PropertyInfo, err error) {
	err = m.instrument(ctx, "list_properties_by_parent", func(ctx context.Context) error {
		results, err = m.next.ListPropertiesByParent(ctx, subjectID, parentType, parentID)
		return err
	})
	return
}

// KVGet instruments plugin key-value retrieval. See ServiceProxy.KVGet.
func (m *ServiceProxyMiddleware) KVGet(ctx context.Context, pluginName, key string) (value string, exists bool, err error) {
	err = m.instrument(ctx, "kv_get", func(ctx context.Context) error {
		value, exists, err = m.next.KVGet(ctx, pluginName, key)
		return err
	})
	return
}

// KVSet instruments plugin key-value storage. See ServiceProxy.KVSet.
func (m *ServiceProxyMiddleware) KVSet(ctx context.Context, pluginName, key, value string) error {
	return m.instrument(ctx, "kv_set", func(ctx context.Context) error {
		return m.next.KVSet(ctx, pluginName, key, value)
	})
}

// KVDelete instruments plugin key-value deletion. See ServiceProxy.KVDelete.
func (m *ServiceProxyMiddleware) KVDelete(ctx context.Context, pluginName, key string) error {
	return m.instrument(ctx, "kv_delete", func(ctx context.Context) error {
		return m.next.KVDelete(ctx, pluginName, key)
	})
}

// FindSessionByName instruments session lookup by name. See ServiceProxy.FindSessionByName.
func (m *ServiceProxyMiddleware) FindSessionByName(ctx context.Context, name string) (result *SessionResult, err error) {
	err = m.instrument(ctx, "find_session_by_name", func(ctx context.Context) error {
		result, err = m.next.FindSessionByName(ctx, name)
		return err
	})
	return
}

// SetLastWhispered instruments whisper target recording. See ServiceProxy.SetLastWhispered.
func (m *ServiceProxyMiddleware) SetLastWhispered(ctx context.Context, sessionID, name string) error {
	return m.instrument(ctx, "set_last_whispered", func(ctx context.Context) error {
		return m.next.SetLastWhispered(ctx, sessionID, name)
	})
}

// DisconnectSession instruments session disconnection. See ServiceProxy.DisconnectSession.
func (m *ServiceProxyMiddleware) DisconnectSession(ctx context.Context, sessionID, reason string) error {
	return m.instrument(ctx, "disconnect_session", func(ctx context.Context) error {
		return m.next.DisconnectSession(ctx, sessionID, reason)
	})
}

// ListActiveSessions instruments active session listing. See ServiceProxy.ListActiveSessions.
func (m *ServiceProxyMiddleware) ListActiveSessions(ctx context.Context) (results []SessionResult, err error) {
	err = m.instrument(ctx, "list_active_sessions", func(ctx context.Context) error {
		results, err = m.next.ListActiveSessions(ctx)
		return err
	})
	return
}

// BroadcastSystemMessage instruments system broadcast. See ServiceProxy.BroadcastSystemMessage.
func (m *ServiceProxyMiddleware) BroadcastSystemMessage(ctx context.Context, message string) error {
	return m.instrument(ctx, "broadcast_system_message", func(ctx context.Context) error {
		return m.next.BroadcastSystemMessage(ctx, message)
	})
}

// UpdateActivity instruments activity timestamp update. See ServiceProxy.UpdateActivity.
func (m *ServiceProxyMiddleware) UpdateActivity(ctx context.Context, sessionID string) error {
	return m.instrument(ctx, "update_activity", func(ctx context.Context) error {
		return m.next.UpdateActivity(ctx, sessionID)
	})
}

// SetPlayerAlias instruments player alias creation. See ServiceProxy.SetPlayerAlias.
func (m *ServiceProxyMiddleware) SetPlayerAlias(ctx context.Context, playerID, alias, command string) error {
	return m.instrument(ctx, "set_player_alias", func(ctx context.Context) error {
		return m.next.SetPlayerAlias(ctx, playerID, alias, command)
	})
}

// DeletePlayerAlias instruments player alias deletion. See ServiceProxy.DeletePlayerAlias.
func (m *ServiceProxyMiddleware) DeletePlayerAlias(ctx context.Context, playerID, alias string) error {
	return m.instrument(ctx, "delete_player_alias", func(ctx context.Context) error {
		return m.next.DeletePlayerAlias(ctx, playerID, alias)
	})
}

// ListPlayerAliases instruments player alias listing. See ServiceProxy.ListPlayerAliases.
func (m *ServiceProxyMiddleware) ListPlayerAliases(ctx context.Context, playerID string) (results []AliasEntry, err error) {
	err = m.instrument(ctx, "list_player_aliases", func(ctx context.Context) error {
		results, err = m.next.ListPlayerAliases(ctx, playerID)
		return err
	})
	return
}

// SetSystemAlias instruments system alias creation. See ServiceProxy.SetSystemAlias.
func (m *ServiceProxyMiddleware) SetSystemAlias(ctx context.Context, alias, command, createdBy string) error {
	return m.instrument(ctx, "set_system_alias", func(ctx context.Context) error {
		return m.next.SetSystemAlias(ctx, alias, command, createdBy)
	})
}

// DeleteSystemAlias instruments system alias deletion. See ServiceProxy.DeleteSystemAlias.
func (m *ServiceProxyMiddleware) DeleteSystemAlias(ctx context.Context, alias string) error {
	return m.instrument(ctx, "delete_system_alias", func(ctx context.Context) error {
		return m.next.DeleteSystemAlias(ctx, alias)
	})
}

// ListSystemAliases instruments system alias listing. See ServiceProxy.ListSystemAliases.
func (m *ServiceProxyMiddleware) ListSystemAliases(ctx context.Context) (results []AliasEntry, err error) {
	err = m.instrument(ctx, "list_system_aliases", func(ctx context.Context) error {
		results, err = m.next.ListSystemAliases(ctx)
		return err
	})
	return
}

// CheckAliasShadow instruments alias shadow check. See ServiceProxy.CheckAliasShadow.
func (m *ServiceProxyMiddleware) CheckAliasShadow(ctx context.Context, alias string) (shadows bool, cmdName string, err error) {
	err = m.instrument(ctx, "check_alias_shadow", func(ctx context.Context) error {
		shadows, cmdName, err = m.next.CheckAliasShadow(ctx, alias)
		return err
	})
	return
}

// ListCommands instruments command listing. See ServiceProxy.ListCommands.
func (m *ServiceProxyMiddleware) ListCommands(ctx context.Context, characterID string) (results []CommandInfo, err error) {
	err = m.instrument(ctx, "list_commands", func(ctx context.Context) error {
		results, err = m.next.ListCommands(ctx, characterID)
		return err
	})
	return
}

// GetCommandHelp instruments command help retrieval. See ServiceProxy.GetCommandHelp.
func (m *ServiceProxyMiddleware) GetCommandHelp(ctx context.Context, name, characterID string) (result *CommandHelpInfo, err error) {
	err = m.instrument(ctx, "get_command_help", func(ctx context.Context) error {
		result, err = m.next.GetCommandHelp(ctx, name, characterID)
		return err
	})
	return
}

// EmitEvent instruments event emission. See ServiceProxy.EmitEvent.
func (m *ServiceProxyMiddleware) EmitEvent(ctx context.Context, stream, eventType string, payload []byte) error {
	return m.instrument(ctx, "emit_event", func(ctx context.Context) error {
		return m.next.EmitEvent(ctx, stream, eventType, payload)
	})
}

// GetStartingLocationID instruments starting location retrieval. See ServiceProxy.GetStartingLocationID.
func (m *ServiceProxyMiddleware) GetStartingLocationID(ctx context.Context) (id string, err error) {
	err = m.instrument(ctx, "get_starting_location_id", func(ctx context.Context) error {
		id, err = m.next.GetStartingLocationID(ctx)
		return err
	})
	return
}

// GetContent delegates to the wrapped proxy with OTel instrumentation.
func (m *ServiceProxyMiddleware) GetContent(ctx context.Context, key string) (result *content.Item, err error) {
	err = m.instrument(ctx, "GetContent", func(ctx context.Context) error {
		result, err = m.next.GetContent(ctx, key)
		return err
	})
	return
}

// ListContent delegates to the wrapped proxy with OTel instrumentation.
func (m *ServiceProxyMiddleware) ListContent(ctx context.Context, prefix string, opts content.ListOptions) (result *content.ListResult, err error) {
	err = m.instrument(ctx, "ListContent", func(ctx context.Context) error {
		result, err = m.next.ListContent(ctx, prefix, opts)
		return err
	})
	return
}

// Log delegates to the wrapped proxy without instrumentation.
func (m *ServiceProxyMiddleware) Log(ctx context.Context, level, message string) {
	m.next.Log(ctx, level, message)
}
