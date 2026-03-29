// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/property"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
)

// SessionAccess provides session operations needed by the service proxy.
// This is a narrow interface matching session.Access methods the proxy uses.
type SessionAccess interface {
	FindByCharacterName(ctx context.Context, name string) (*session.Info, error)
	ListActive(ctx context.Context) ([]*session.Info, error)
	Delete(ctx context.Context, id string, reason string) error
	DeleteByCharacter(ctx context.Context, characterID ulid.ULID, reason string) (*session.Info, error)
	UpdateActivity(ctx context.Context, id string) error
	UpdateLastWhispered(ctx context.Context, sessionID string, name string) error
}

// AliasCacheAccess provides alias operations needed by the service proxy.
type AliasCacheAccess interface {
	SetPlayerAlias(playerID ulid.ULID, alias, cmd string) error
	RemovePlayerAlias(playerID ulid.ULID, alias string)
	ListPlayerAliases(playerID ulid.ULID) map[string]string
	SetSystemAlias(alias, cmd string) error
	RemoveSystemAlias(alias string)
	ListSystemAliases() map[string]string
}

// CommandRegistry provides command lookup needed by the service proxy.
type CommandRegistry interface {
	Get(name string) (command.CommandEntry, bool)
	All() []command.CommandEntry
}

// PropertyRegistry provides property operations needed by the service proxy.
type PropertyRegistry interface {
	Resolve(nameOrPrefix string) (property.Entry, error)
}

// Compile-time check that ServiceProxyImpl implements ServiceProxy.
var _ ServiceProxy = (*ServiceProxyImpl)(nil)

// ServiceProxyImpl wraps real game services, converting between string IDs
// (plugin SDK boundary) and ulid.ULID (internal types).
type ServiceProxyImpl struct {
	world           command.WorldService
	sessions        SessionAccess
	events          core.EventStore
	aliasWriter     command.AliasWriter
	aliasCache      AliasCacheAccess
	commandRegistry CommandRegistry
	propertyReg     PropertyRegistry
	startingLocID   string
	logger          *slog.Logger
}

// ServiceProxyConfig holds dependencies for constructing a ServiceProxyImpl.
type ServiceProxyConfig struct {
	World           command.WorldService
	Sessions        SessionAccess
	Events          core.EventStore
	AliasWriter     command.AliasWriter
	AliasCache      AliasCacheAccess
	CommandRegistry CommandRegistry
	PropertyReg     PropertyRegistry
	StartingLocID   string
	Logger          *slog.Logger
}

// NewServiceProxy creates a new ServiceProxyImpl with the given dependencies.
func NewServiceProxy(cfg ServiceProxyConfig) (*ServiceProxyImpl, error) {
	if cfg.World == nil {
		return nil, oops.Code("NIL_WORLD").Errorf("world service is required")
	}
	if cfg.Events == nil {
		return nil, oops.Code("NIL_EVENTS").Errorf("event store is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &ServiceProxyImpl{
		world:           cfg.World,
		sessions:        cfg.Sessions,
		events:          cfg.Events,
		aliasWriter:     cfg.AliasWriter,
		aliasCache:      cfg.AliasCache,
		commandRegistry: cfg.CommandRegistry,
		propertyReg:     cfg.PropertyReg,
		startingLocID:   cfg.StartingLocID,
		logger:          cfg.Logger,
	}, nil
}

// SetLateBindings updates the proxy with dependencies that are only available
// after the full command stack is initialized. This allows the proxy to be
// created early (with World/Sessions/Events) and completed later once
// AliasWriter, AliasCache, CommandRegistry, and StartingLocID are wired.
func (p *ServiceProxyImpl) SetLateBindings(cfg LateBindingsConfig) {
	p.aliasWriter = cfg.AliasWriter
	p.aliasCache = cfg.AliasCache
	p.commandRegistry = cfg.CommandRegistry
	p.startingLocID = cfg.StartingLocID
}

// LateBindingsConfig holds dependencies that are resolved after initial proxy creation.
type LateBindingsConfig struct {
	AliasWriter     command.AliasWriter
	AliasCache      AliasCacheAccess
	CommandRegistry CommandRegistry
	StartingLocID   string
}

// --- ID conversion helpers ---

func parseULID(id string) (ulid.ULID, error) {
	parsed, err := ulid.Parse(id)
	if err != nil {
		return ulid.ULID{}, oops.Code("INVALID_ID").With("id", id).Wrap(err)
	}
	return parsed, nil
}

func ulidToString(id ulid.ULID) string {
	return id.String()
}

func ulidPtrToString(id *ulid.ULID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// --- World read ---

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) QueryLocation(ctx context.Context, subjectID, id string) (*LocationResult, error) {
	locID, err := parseULID(id)
	if err != nil {
		return nil, err
	}

	loc, err := p.world.GetLocation(ctx, subjectID, locID)
	if err != nil {
		return nil, oops.With("operation", "QueryLocation").Wrap(err)
	}

	return locationToResult(loc), nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) QueryCharacter(ctx context.Context, subjectID, id string) (*CharacterResult, error) {
	charID, err := parseULID(id)
	if err != nil {
		return nil, err
	}

	ch, err := p.world.GetCharacter(ctx, subjectID, charID)
	if err != nil {
		return nil, oops.With("operation", "QueryCharacter").Wrap(err)
	}

	return characterToResult(ch), nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) QueryLocationCharacters(ctx context.Context, subjectID, locationID string) ([]CharacterResult, error) {
	locID, err := parseULID(locationID)
	if err != nil {
		return nil, err
	}

	chars, err := p.world.GetCharactersByLocation(ctx, subjectID, locID, world.ListOptions{})
	if err != nil {
		return nil, oops.With("operation", "QueryLocationCharacters").Wrap(err)
	}

	return charactersToResults(chars), nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) QueryObject(ctx context.Context, subjectID, id string) (*ObjectResult, error) {
	objID, err := parseULID(id)
	if err != nil {
		return nil, err
	}

	obj, err := p.world.GetObject(ctx, subjectID, objID)
	if err != nil {
		return nil, oops.With("operation", "QueryObject").Wrap(err)
	}

	return objectToResult(obj), nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) FindLocation(ctx context.Context, subjectID, name string) (*LocationResult, error) {
	loc, err := p.world.FindLocationByName(ctx, subjectID, name)
	if err != nil {
		return nil, oops.With("operation", "FindLocation").Wrap(err)
	}

	return locationToResult(loc), nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) GetCharactersByLocation(ctx context.Context, subjectID, locationID string) ([]CharacterResult, error) {
	locID, err := parseULID(locationID)
	if err != nil {
		return nil, err
	}

	chars, err := p.world.GetCharactersByLocation(ctx, subjectID, locID, world.ListOptions{})
	if err != nil {
		return nil, oops.With("operation", "GetCharactersByLocation").Wrap(err)
	}

	return charactersToResults(chars), nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) GetObjectsByLocation(ctx context.Context, subjectID, locationID string) ([]ObjectResult, error) {
	locID, err := parseULID(locationID)
	if err != nil {
		return nil, err
	}

	objs, err := p.world.GetObjectsByLocation(ctx, subjectID, locID)
	if err != nil {
		return nil, oops.With("operation", "GetObjectsByLocation").Wrap(err)
	}

	results := make([]ObjectResult, len(objs))
	for i, obj := range objs {
		results[i] = *objectToResult(obj)
	}
	return results, nil
}

// --- World write ---

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) CreateLocation(ctx context.Context, subjectID, name, description, locationType string) (*LocationResult, error) {
	loc := &world.Location{
		ID:          ulid.Make(),
		Name:        name,
		Description: description,
		Type:        world.LocationType(locationType),
	}

	if err := p.world.CreateLocation(ctx, subjectID, loc); err != nil {
		return nil, oops.With("operation", "CreateLocation").Wrap(err)
	}

	return locationToResult(loc), nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) CreateExit(ctx context.Context, subjectID, fromID, toID, name string, opts CreateExitOpts) error {
	from, err := parseULID(fromID)
	if err != nil {
		return err
	}
	to, err := parseULID(toID)
	if err != nil {
		return err
	}

	exit := &world.Exit{
		ID:             ulid.Make(),
		FromLocationID: from,
		ToLocationID:   to,
		Name:           name,
		Aliases:        opts.Aliases,
		Bidirectional:  opts.Bidirectional,
		ReturnName:     opts.ReturnName,
	}

	return oops.With("operation", "CreateExit").Wrap(p.world.CreateExit(ctx, subjectID, exit))
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) CreateObject(ctx context.Context, subjectID, name, description string) (*ObjectResult, error) {
	obj := &world.Object{
		ID:          ulid.Make(),
		Name:        name,
		Description: description,
	}

	if err := p.world.CreateObject(ctx, subjectID, obj); err != nil {
		return nil, oops.With("operation", "CreateObject").Wrap(err)
	}

	return objectToResult(obj), nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) UpdateLocation(ctx context.Context, subjectID, id, name, description string) error {
	locID, err := parseULID(id)
	if err != nil {
		return err
	}

	loc, err := p.world.GetLocation(ctx, subjectID, locID)
	if err != nil {
		return oops.With("operation", "UpdateLocation").Wrap(err)
	}

	loc.Name = name
	loc.Description = description
	return oops.With("operation", "UpdateLocation").Wrap(p.world.UpdateLocation(ctx, subjectID, loc))
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) UpdateCharacterDescription(ctx context.Context, subjectID, characterID, description string) error {
	charID, err := parseULID(characterID)
	if err != nil {
		return err
	}

	return oops.With("operation", "UpdateCharacterDescription").
		Wrap(p.world.UpdateCharacterDescription(ctx, subjectID, charID, description))
}

// --- Properties ---

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) SetProperty(ctx context.Context, subjectID, parentType, parentID, key, value string) error {
	if p.propertyReg == nil {
		return oops.Code("NO_PROPERTY_REGISTRY").Errorf("property registry not configured")
	}

	entry, err := p.propertyReg.Resolve(key)
	if err != nil {
		return oops.With("operation", "SetProperty").With("key", key).Wrap(err)
	}

	entityID, err := parseULID(parentID)
	if err != nil {
		return err
	}

	querier := proxyQuerier{service: p.world, subjectID: subjectID}
	mutator := proxyMutator{service: p.world}

	return oops.With("operation", "SetProperty").
		Wrap(entry.Definition.Set(ctx, querier, mutator, subjectID, parentType, entityID, value))
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) GetProperty(ctx context.Context, subjectID, parentType, parentID, key string) (string, error) {
	if p.propertyReg == nil {
		return "", oops.Code("NO_PROPERTY_REGISTRY").Errorf("property registry not configured")
	}

	entry, err := p.propertyReg.Resolve(key)
	if err != nil {
		return "", oops.With("operation", "GetProperty").With("key", key).Wrap(err)
	}

	entityID, err := parseULID(parentID)
	if err != nil {
		return "", err
	}

	querier := proxyQuerier{service: p.world, subjectID: subjectID}
	val, err := entry.Definition.Get(ctx, querier, parentType, entityID)
	if err != nil {
		return "", oops.With("operation", "GetProperty").Wrap(err)
	}
	return val, nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) FindPropertyByPrefix(_ context.Context, prefix string) ([]PropertyInfo, error) {
	if p.propertyReg == nil {
		return nil, oops.Code("NO_PROPERTY_REGISTRY").Errorf("property registry not configured")
	}

	entry, err := p.propertyReg.Resolve(prefix)
	if err != nil {
		return nil, oops.With("operation", "FindPropertyByPrefix").Wrap(err)
	}

	return []PropertyInfo{{Name: entry.Name}}, nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) ListPropertiesByParent(ctx context.Context, subjectID, parentType, parentID string) ([]PropertyInfo, error) {
	entityID, err := parseULID(parentID)
	if err != nil {
		return nil, err
	}

	props, err := p.world.ListPropertiesByParent(ctx, subjectID, parentType, entityID)
	if err != nil {
		return nil, oops.With("operation", "ListPropertiesByParent").Wrap(err)
	}

	results := make([]PropertyInfo, len(props))
	for i, prop := range props {
		val := ""
		if prop.Value != nil {
			val = *prop.Value
		}
		results[i] = PropertyInfo{
			Name:  prop.Name,
			Value: val,
		}
	}
	return results, nil
}

// --- Plugin KV ---
// KV operations are stubs until the plugin KV store is implemented.

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) KVGet(_ context.Context, _, _ string) (val string, found bool, err error) {
	return "", false, oops.Code("NOT_IMPLEMENTED").Errorf("plugin KV store not yet implemented")
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) KVSet(_ context.Context, _, _, _ string) error {
	return oops.Code("NOT_IMPLEMENTED").Errorf("plugin KV store not yet implemented")
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) KVDelete(_ context.Context, _, _ string) error {
	return oops.Code("NOT_IMPLEMENTED").Errorf("plugin KV store not yet implemented")
}

// --- Session ---

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) FindSessionByName(ctx context.Context, name string) (*SessionResult, error) {
	if p.sessions == nil {
		return nil, oops.Code("NO_SESSION_STORE").Errorf("session store not configured")
	}

	info, err := p.sessions.FindByCharacterName(ctx, name)
	if err != nil {
		return nil, oops.With("operation", "FindSessionByName").Wrap(err)
	}
	if info == nil {
		return nil, nil
	}

	return sessionInfoToResult(info), nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) SetLastWhispered(ctx context.Context, sessionID, name string) error {
	if p.sessions == nil {
		return oops.Code("NO_SESSION_STORE").Errorf("session store not configured")
	}

	return oops.With("operation", "SetLastWhispered").
		Wrap(p.sessions.UpdateLastWhispered(ctx, sessionID, name))
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) DisconnectSession(ctx context.Context, sessionID, reason string) error {
	if p.sessions == nil {
		return oops.Code("NO_SESSION_STORE").Errorf("session store not configured")
	}

	if sessionID == "" {
		return oops.Code("INVALID_SESSION_ID").Errorf("session ID cannot be empty")
	}

	return oops.With("operation", "DisconnectSession").
		Wrap(p.sessions.Delete(ctx, sessionID, reason))
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) ListActiveSessions(ctx context.Context) ([]SessionResult, error) {
	if p.sessions == nil {
		return nil, oops.Code("NO_SESSION_STORE").Errorf("session store not configured")
	}

	sessions, err := p.sessions.ListActive(ctx)
	if err != nil {
		return nil, oops.With("operation", "ListActiveSessions").Wrap(err)
	}

	results := make([]SessionResult, len(sessions))
	for i, s := range sessions {
		results[i] = *sessionInfoToResult(s)
	}
	return results, nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) BroadcastSystemMessage(ctx context.Context, message string) error {
	//nolint:errcheck // json.Marshal cannot fail for map[string]string
	payload, _ := json.Marshal(map[string]string{
		"message": message,
	})

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "system",
		Type:      core.EventTypeSystem,
		Timestamp: time.Now(),
		Actor: core.Actor{
			Kind: core.ActorSystem,
			ID:   "system",
		},
		Payload: payload,
	}

	return oops.With("operation", "BroadcastSystemMessage").
		Wrap(p.events.Append(ctx, event))
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) UpdateActivity(ctx context.Context, sessionID string) error {
	if p.sessions == nil {
		return oops.Code("NO_SESSION_STORE").Errorf("session store not configured")
	}

	return oops.With("operation", "UpdateActivity").
		Wrap(p.sessions.UpdateActivity(ctx, sessionID))
}

// --- Aliases ---

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) SetPlayerAlias(ctx context.Context, playerID, alias, cmd string) error {
	if p.aliasWriter == nil && p.aliasCache == nil {
		return oops.Code("NO_ALIAS_STORE").Errorf("alias subsystem not configured")
	}

	pID, err := parseULID(playerID)
	if err != nil {
		return err
	}

	// Persist first, then update cache to avoid divergence on write failure.
	if p.aliasWriter != nil {
		if writeErr := p.aliasWriter.SetPlayerAlias(ctx, pID, alias, cmd); writeErr != nil {
			return oops.With("operation", "SetPlayerAlias").Wrap(writeErr)
		}
	}

	if p.aliasCache != nil {
		if cacheErr := p.aliasCache.SetPlayerAlias(pID, alias, cmd); cacheErr != nil {
			return oops.With("operation", "SetPlayerAlias").Wrap(cacheErr)
		}
	}

	return nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) DeletePlayerAlias(ctx context.Context, playerID, alias string) error {
	if p.aliasWriter == nil && p.aliasCache == nil {
		return oops.Code("NO_ALIAS_STORE").Errorf("alias subsystem not configured")
	}

	pID, err := parseULID(playerID)
	if err != nil {
		return err
	}

	// Persist first, then update cache to avoid divergence on write failure.
	if p.aliasWriter != nil {
		if writeErr := p.aliasWriter.DeletePlayerAlias(ctx, pID, alias); writeErr != nil {
			return oops.With("operation", "DeletePlayerAlias").Wrap(writeErr)
		}
	}

	if p.aliasCache != nil {
		p.aliasCache.RemovePlayerAlias(pID, alias)
	}

	return nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) ListPlayerAliases(_ context.Context, playerID string) ([]AliasEntry, error) {
	pID, err := parseULID(playerID)
	if err != nil {
		return nil, err
	}

	if p.aliasCache == nil {
		return nil, nil
	}

	aliases := p.aliasCache.ListPlayerAliases(pID)
	return aliasMapToEntries(aliases), nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) SetSystemAlias(ctx context.Context, alias, cmd, createdBy string) error {
	if p.aliasWriter == nil && p.aliasCache == nil {
		return oops.Code("NO_ALIAS_STORE").Errorf("alias subsystem not configured")
	}

	// Persist first, then update cache to avoid divergence on write failure.
	if p.aliasWriter != nil {
		if writeErr := p.aliasWriter.SetSystemAlias(ctx, alias, cmd, createdBy); writeErr != nil {
			return oops.With("operation", "SetSystemAlias").Wrap(writeErr)
		}
	}

	if p.aliasCache != nil {
		if cacheErr := p.aliasCache.SetSystemAlias(alias, cmd); cacheErr != nil {
			return oops.With("operation", "SetSystemAlias").Wrap(cacheErr)
		}
	}

	return nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) DeleteSystemAlias(ctx context.Context, alias string) error {
	if p.aliasWriter == nil && p.aliasCache == nil {
		return oops.Code("NO_ALIAS_STORE").Errorf("alias subsystem not configured")
	}

	// Persist first, then update cache to avoid divergence on write failure.
	if p.aliasWriter != nil {
		if writeErr := p.aliasWriter.DeleteSystemAlias(ctx, alias); writeErr != nil {
			return oops.With("operation", "DeleteSystemAlias").Wrap(writeErr)
		}
	}

	if p.aliasCache != nil {
		p.aliasCache.RemoveSystemAlias(alias)
	}

	return nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) ListSystemAliases(_ context.Context) ([]AliasEntry, error) {
	if p.aliasCache == nil {
		return nil, nil
	}

	aliases := p.aliasCache.ListSystemAliases()
	return aliasMapToEntries(aliases), nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) CheckAliasShadow(_ context.Context, alias string) (shadows bool, cmdName string, err error) {
	if p.commandRegistry == nil {
		return false, "", nil
	}

	entry, exists := p.commandRegistry.Get(alias)
	if !exists {
		return false, "", nil
	}

	return true, entry.Name, nil
}

// --- Commands ---

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) ListCommands(_ context.Context, _ string) ([]CommandInfo, error) {
	if p.commandRegistry == nil {
		return nil, nil
	}

	entries := p.commandRegistry.All()
	results := make([]CommandInfo, len(entries))
	for i := range entries {
		results[i] = CommandInfo{
			Name:   entries[i].Name,
			Help:   entries[i].Help,
			Source: entries[i].Source,
		}
	}
	return results, nil
}

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) GetCommandHelp(_ context.Context, name, _ string) (*CommandHelpInfo, error) {
	if p.commandRegistry == nil {
		return nil, nil
	}

	entry, exists := p.commandRegistry.Get(name)
	if !exists {
		return nil, nil
	}

	return &CommandHelpInfo{
		Name:     entry.Name,
		Help:     entry.Help,
		Usage:    entry.Usage,
		HelpText: entry.HelpText,
		Source:   entry.Source,
	}, nil
}

// --- Events ---

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) EmitEvent(ctx context.Context, stream, eventType string, payload []byte) error {
	return p.EmitEventAs(ctx, "service-proxy", stream, eventType, payload)
}

// EmitEventAs emits an event using a specific actor identity (typically a plugin name).
func (p *ServiceProxyImpl) EmitEventAs(ctx context.Context, actorID, stream, eventType string, payload []byte) error {
	event := core.Event{
		ID:        ulid.Make(),
		Stream:    stream,
		Type:      core.EventType(eventType),
		Timestamp: time.Now(),
		Actor: core.Actor{
			Kind: core.ActorPlugin,
			ID:   actorID,
		},
		Payload: payload,
	}

	return oops.With("operation", "EmitEvent").
		Wrap(p.events.Append(ctx, event))
}

// --- Config ---

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) GetStartingLocationID(_ context.Context) (string, error) {
	if p.startingLocID == "" {
		return "", oops.Code("NO_STARTING_LOCATION").Errorf("starting location not configured")
	}
	return p.startingLocID, nil
}

// --- Utility ---

//nolint:revive // implements ServiceProxy
func (p *ServiceProxyImpl) Log(_ context.Context, level, message string) {
	switch level {
	case "debug":
		p.logger.Debug(message)
	case "info":
		p.logger.Info(message)
	case "warn":
		p.logger.Warn(message)
	case "error":
		p.logger.Error(message)
	default:
		p.logger.Info(message, "requested_level", level)
	}
}

// --- Conversion helpers ---

func locationToResult(loc *world.Location) *LocationResult {
	if loc == nil {
		return nil
	}
	return &LocationResult{
		ID:          ulidToString(loc.ID),
		Name:        loc.Name,
		Description: loc.Description,
		Type:        string(loc.Type),
	}
}

func characterToResult(ch *world.Character) *CharacterResult {
	if ch == nil {
		return nil
	}
	return &CharacterResult{
		ID:          ulidToString(ch.ID),
		PlayerID:    ulidToString(ch.PlayerID),
		Name:        ch.Name,
		Description: ch.Description,
		LocationID:  ulidPtrToString(ch.LocationID),
	}
}

func charactersToResults(chars []*world.Character) []CharacterResult {
	results := make([]CharacterResult, len(chars))
	for i, ch := range chars {
		results[i] = *characterToResult(ch)
	}
	return results
}

func objectToResult(obj *world.Object) *ObjectResult {
	if obj == nil {
		return nil
	}
	return &ObjectResult{
		ID:          ulidToString(obj.ID),
		Name:        obj.Name,
		Description: obj.Description,
	}
}

func sessionInfoToResult(info *session.Info) *SessionResult {
	if info == nil {
		return nil
	}
	return &SessionResult{
		ID:            info.ID,
		CharacterID:   ulidToString(info.CharacterID),
		CharacterName: info.CharacterName,
		LocationID:    ulidToString(info.LocationID),
		Status:        string(info.Status),
		GridPresent:   info.GridPresent,
		LastWhispered: info.LastWhispered,
	}
}

func aliasMapToEntries(aliases map[string]string) []AliasEntry {
	entries := make([]AliasEntry, 0, len(aliases))
	for alias, cmd := range aliases {
		entries = append(entries, AliasEntry{Alias: alias, Command: cmd})
	}
	return entries
}

// --- Property adapters ---
// These bridge command.WorldService (subjectID-aware) to property.WorldQuerier
// and property.WorldMutator (subjectID-free) interfaces.

type proxyQuerier struct {
	service   command.WorldService
	subjectID string
}

func (q proxyQuerier) GetLocation(ctx context.Context, id ulid.ULID) (*world.Location, error) {
	loc, err := q.service.GetLocation(ctx, q.subjectID, id)
	if err != nil {
		return nil, oops.With("operation", "proxyQuerier.GetLocation").Wrap(err)
	}
	return loc, nil
}

func (q proxyQuerier) GetObject(ctx context.Context, id ulid.ULID) (*world.Object, error) {
	obj, err := q.service.GetObject(ctx, q.subjectID, id)
	if err != nil {
		return nil, oops.With("operation", "proxyQuerier.GetObject").Wrap(err)
	}
	return obj, nil
}

type proxyMutator struct {
	service command.WorldService
}

func (m proxyMutator) UpdateLocation(ctx context.Context, subjectID string, loc *world.Location) error {
	return oops.With("operation", "proxyMutator.UpdateLocation").
		Wrap(m.service.UpdateLocation(ctx, subjectID, loc))
}

func (m proxyMutator) UpdateObject(ctx context.Context, subjectID string, obj *world.Object) error {
	return oops.With("operation", "proxyMutator.UpdateObject").
		Wrap(m.service.UpdateObject(ctx, subjectID, obj))
}
