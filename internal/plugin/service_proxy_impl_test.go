// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
)

// --- Mock implementations for narrow interfaces ---

type mockWorldService struct {
	getLocation             func(ctx context.Context, subjectID string, id ulid.ULID) (*world.Location, error)
	getCharacter            func(ctx context.Context, subjectID string, id ulid.ULID) (*world.Character, error)
	getCharactersByLocation func(ctx context.Context, subjectID string, locationID ulid.ULID, opts world.ListOptions) ([]*world.Character, error)
	getObject               func(ctx context.Context, subjectID string, id ulid.ULID) (*world.Object, error)
	findLocationByName      func(ctx context.Context, subjectID, name string) (*world.Location, error)
	getObjectsByLocation    func(ctx context.Context, subjectID string, locationID ulid.ULID) ([]*world.Object, error)
	createLocation          func(ctx context.Context, subjectID string, loc *world.Location) error
	createExit              func(ctx context.Context, subjectID string, exit *world.Exit) error
	createObject            func(ctx context.Context, subjectID string, obj *world.Object) error
	updateLocation          func(ctx context.Context, subjectID string, loc *world.Location) error
	getExitsByLocation      func(ctx context.Context, subjectID string, locationID ulid.ULID) ([]*world.Exit, error)
	moveCharacter           func(ctx context.Context, subjectID string, characterID, toLocationID ulid.ULID) error
	updateObject            func(ctx context.Context, subjectID string, obj *world.Object) error
	updateCharacterDesc     func(ctx context.Context, subjectID string, characterID ulid.ULID, description string) error
	listPropertiesByParent  func(ctx context.Context, subjectID string, parentType string, parentID ulid.ULID) ([]*world.EntityProperty, error)
}

func (m *mockWorldService) GetLocation(ctx context.Context, subjectID string, id ulid.ULID) (*world.Location, error) {
	if m.getLocation != nil {
		return m.getLocation(ctx, subjectID, id)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorldService) GetCharacter(ctx context.Context, subjectID string, id ulid.ULID) (*world.Character, error) {
	if m.getCharacter != nil {
		return m.getCharacter(ctx, subjectID, id)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorldService) GetCharactersByLocation(ctx context.Context, subjectID string, locationID ulid.ULID, opts world.ListOptions) ([]*world.Character, error) {
	if m.getCharactersByLocation != nil {
		return m.getCharactersByLocation(ctx, subjectID, locationID, opts)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorldService) GetObject(ctx context.Context, subjectID string, id ulid.ULID) (*world.Object, error) {
	if m.getObject != nil {
		return m.getObject(ctx, subjectID, id)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorldService) FindLocationByName(ctx context.Context, subjectID, name string) (*world.Location, error) {
	if m.findLocationByName != nil {
		return m.findLocationByName(ctx, subjectID, name)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorldService) GetObjectsByLocation(ctx context.Context, subjectID string, locationID ulid.ULID) ([]*world.Object, error) {
	if m.getObjectsByLocation != nil {
		return m.getObjectsByLocation(ctx, subjectID, locationID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorldService) CreateLocation(ctx context.Context, subjectID string, loc *world.Location) error {
	if m.createLocation != nil {
		return m.createLocation(ctx, subjectID, loc)
	}
	return errors.New("not implemented")
}

func (m *mockWorldService) CreateExit(ctx context.Context, subjectID string, exit *world.Exit) error {
	if m.createExit != nil {
		return m.createExit(ctx, subjectID, exit)
	}
	return errors.New("not implemented")
}

func (m *mockWorldService) CreateObject(ctx context.Context, subjectID string, obj *world.Object) error {
	if m.createObject != nil {
		return m.createObject(ctx, subjectID, obj)
	}
	return errors.New("not implemented")
}

func (m *mockWorldService) UpdateLocation(ctx context.Context, subjectID string, loc *world.Location) error {
	if m.updateLocation != nil {
		return m.updateLocation(ctx, subjectID, loc)
	}
	return errors.New("not implemented")
}

func (m *mockWorldService) GetExitsByLocation(ctx context.Context, subjectID string, locationID ulid.ULID) ([]*world.Exit, error) {
	if m.getExitsByLocation != nil {
		return m.getExitsByLocation(ctx, subjectID, locationID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorldService) MoveCharacter(ctx context.Context, subjectID string, characterID, toLocationID ulid.ULID) error {
	if m.moveCharacter != nil {
		return m.moveCharacter(ctx, subjectID, characterID, toLocationID)
	}
	return errors.New("not implemented")
}

func (m *mockWorldService) UpdateObject(ctx context.Context, subjectID string, obj *world.Object) error {
	if m.updateObject != nil {
		return m.updateObject(ctx, subjectID, obj)
	}
	return errors.New("not implemented")
}

func (m *mockWorldService) UpdateCharacterDescription(ctx context.Context, subjectID string, characterID ulid.ULID, description string) error {
	if m.updateCharacterDesc != nil {
		return m.updateCharacterDesc(ctx, subjectID, characterID, description)
	}
	return errors.New("not implemented")
}

func (m *mockWorldService) ListPropertiesByParent(ctx context.Context, subjectID string, parentType string, parentID ulid.ULID) ([]*world.EntityProperty, error) {
	if m.listPropertiesByParent != nil {
		return m.listPropertiesByParent(ctx, subjectID, parentType, parentID)
	}
	return nil, errors.New("not implemented")
}

type mockEventStore struct {
	appendFn func(ctx context.Context, event core.Event) error
}

func (m *mockEventStore) Append(ctx context.Context, event core.Event) error {
	if m.appendFn != nil {
		return m.appendFn(ctx, event)
	}
	return nil
}

func (m *mockEventStore) Replay(_ context.Context, _ string, _ ulid.ULID, _ int) ([]core.Event, error) {
	return nil, nil
}

func (m *mockEventStore) LastEventID(_ context.Context, _ string) (ulid.ULID, error) {
	return ulid.ULID{}, nil
}

func (m *mockEventStore) Subscribe(_ context.Context, _ string) (<-chan ulid.ULID, <-chan error, error) {
	return nil, nil, nil
}

type mockSessionAccess struct {
	findByCharacterName func(ctx context.Context, name string) (*session.Info, error)
	listActive          func(ctx context.Context) ([]*session.Info, error)
	deleteSession       func(ctx context.Context, id string, reason string) error
	deleteByCharacter   func(ctx context.Context, characterID ulid.ULID, reason string) (*session.Info, error)
	updateActivity      func(ctx context.Context, id string) error
	updateLastWhispered func(ctx context.Context, sessionID string, name string) error
}

func (m *mockSessionAccess) FindByCharacterName(ctx context.Context, name string) (*session.Info, error) {
	if m.findByCharacterName != nil {
		return m.findByCharacterName(ctx, name)
	}
	return nil, nil
}

func (m *mockSessionAccess) ListActive(ctx context.Context) ([]*session.Info, error) {
	if m.listActive != nil {
		return m.listActive(ctx)
	}
	return nil, nil
}

func (m *mockSessionAccess) Delete(ctx context.Context, id string, reason string) error {
	if m.deleteSession != nil {
		return m.deleteSession(ctx, id, reason)
	}
	return nil
}

func (m *mockSessionAccess) DeleteByCharacter(ctx context.Context, characterID ulid.ULID, reason string) (*session.Info, error) {
	if m.deleteByCharacter != nil {
		return m.deleteByCharacter(ctx, characterID, reason)
	}
	return nil, nil
}

func (m *mockSessionAccess) UpdateActivity(ctx context.Context, id string) error {
	if m.updateActivity != nil {
		return m.updateActivity(ctx, id)
	}
	return nil
}

func (m *mockSessionAccess) UpdateLastWhispered(ctx context.Context, sessionID string, name string) error {
	if m.updateLastWhispered != nil {
		return m.updateLastWhispered(ctx, sessionID, name)
	}
	return nil
}

type mockAliasCacheAccess struct {
	setPlayerAlias    func(playerID ulid.ULID, alias, cmd string) error
	removePlayerAlias func(playerID ulid.ULID, alias string)
	listPlayerAliases func(playerID ulid.ULID) map[string]string
	setSystemAlias    func(alias, cmd string) error
	removeSystemAlias func(alias string)
	listSystemAliases func() map[string]string
}

func (m *mockAliasCacheAccess) SetPlayerAlias(playerID ulid.ULID, alias, cmd string) error {
	if m.setPlayerAlias != nil {
		return m.setPlayerAlias(playerID, alias, cmd)
	}
	return nil
}

func (m *mockAliasCacheAccess) RemovePlayerAlias(playerID ulid.ULID, alias string) {
	if m.removePlayerAlias != nil {
		m.removePlayerAlias(playerID, alias)
	}
}

func (m *mockAliasCacheAccess) ListPlayerAliases(playerID ulid.ULID) map[string]string {
	if m.listPlayerAliases != nil {
		return m.listPlayerAliases(playerID)
	}
	return nil
}

func (m *mockAliasCacheAccess) SetSystemAlias(alias, cmd string) error {
	if m.setSystemAlias != nil {
		return m.setSystemAlias(alias, cmd)
	}
	return nil
}

func (m *mockAliasCacheAccess) RemoveSystemAlias(alias string) {
	if m.removeSystemAlias != nil {
		m.removeSystemAlias(alias)
	}
}

func (m *mockAliasCacheAccess) ListSystemAliases() map[string]string {
	if m.listSystemAliases != nil {
		return m.listSystemAliases()
	}
	return nil
}

type mockAliasWriter struct {
	setPlayerAlias    func(ctx context.Context, playerID ulid.ULID, alias, cmd string) error
	deletePlayerAlias func(ctx context.Context, playerID ulid.ULID, alias string) error
	setSystemAlias    func(ctx context.Context, alias, cmd, createdBy string) error
	deleteSystemAlias func(ctx context.Context, alias string) error
}

func (m *mockAliasWriter) SetPlayerAlias(ctx context.Context, playerID ulid.ULID, alias, cmd string) error {
	if m.setPlayerAlias != nil {
		return m.setPlayerAlias(ctx, playerID, alias, cmd)
	}
	return nil
}

func (m *mockAliasWriter) DeletePlayerAlias(ctx context.Context, playerID ulid.ULID, alias string) error {
	if m.deletePlayerAlias != nil {
		return m.deletePlayerAlias(ctx, playerID, alias)
	}
	return nil
}

func (m *mockAliasWriter) SetSystemAlias(ctx context.Context, alias, cmd, createdBy string) error {
	if m.setSystemAlias != nil {
		return m.setSystemAlias(ctx, alias, cmd, createdBy)
	}
	return nil
}

func (m *mockAliasWriter) DeleteSystemAlias(ctx context.Context, alias string) error {
	if m.deleteSystemAlias != nil {
		return m.deleteSystemAlias(ctx, alias)
	}
	return nil
}

type mockCommandRegistry struct {
	get func(name string) (command.CommandEntry, bool)
	all func() []command.CommandEntry
}

func (m *mockCommandRegistry) Get(name string) (command.CommandEntry, bool) {
	if m.get != nil {
		return m.get(name)
	}
	return command.CommandEntry{}, false
}

func (m *mockCommandRegistry) All() []command.CommandEntry {
	if m.all != nil {
		return m.all()
	}
	return nil
}


// --- Test helpers ---

func makeTestID() ulid.ULID {
	return ulid.Make()
}

func newTestProxy(t *testing.T, worldSvc command.WorldService, events core.EventStore) *ServiceProxyImpl {
	t.Helper()
	proxy, err := NewServiceProxy(ServiceProxyConfig{
		World:  worldSvc,
		Events: events,
		Logger: slog.Default(),
	})
	require.NoError(t, err)
	return proxy
}

// --- Tests ---

func TestNewServiceProxy(t *testing.T) {
	t.Run("requires world service", func(t *testing.T) {
		_, err := NewServiceProxy(ServiceProxyConfig{
			Events: &mockEventStore{},
		})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "NIL_WORLD")
	})

	t.Run("requires event store", func(t *testing.T) {
		_, err := NewServiceProxy(ServiceProxyConfig{
			World: &mockWorldService{},
		})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "NIL_EVENTS")
	})

	t.Run("creates proxy with required deps", func(t *testing.T) {
		proxy, err := NewServiceProxy(ServiceProxyConfig{
			World:  &mockWorldService{},
			Events: &mockEventStore{},
		})
		require.NoError(t, err)
		assert.NotNil(t, proxy)
	})

	t.Run("defaults logger when nil", func(t *testing.T) {
		proxy, err := NewServiceProxy(ServiceProxyConfig{
			World:  &mockWorldService{},
			Events: &mockEventStore{},
		})
		require.NoError(t, err)
		assert.NotNil(t, proxy.logger)
	})
}

func TestServiceProxy_QueryLocation(t *testing.T) {
	locID := makeTestID()

	tests := []struct {
		name      string
		id        string
		setup     func(m *mockWorldService)
		wantName  string
		wantErr   bool
		errCode   string
	}{
		{
			name: "valid location",
			id:   locID.String(),
			setup: func(m *mockWorldService) {
				m.getLocation = func(_ context.Context, _ string, id ulid.ULID) (*world.Location, error) {
					return &world.Location{
						ID:          id,
						Name:        "Town Square",
						Description: "A bustling square",
						Type:        world.LocationTypePersistent,
					}, nil
				}
			},
			wantName: "Town Square",
		},
		{
			name:    "invalid ULID",
			id:      "not-a-ulid",
			setup:   func(_ *mockWorldService) {},
			wantErr: true,
			errCode: "INVALID_ID",
		},
		{
			name: "world service error",
			id:   locID.String(),
			setup: func(m *mockWorldService) {
				m.getLocation = func(_ context.Context, _ string, _ ulid.ULID) (*world.Location, error) {
					return nil, errors.New("db error")
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := &mockWorldService{}
			tt.setup(ws)
			proxy := newTestProxy(t, ws, &mockEventStore{})

			result, err := proxy.QueryLocation(context.Background(), "subject-1", tt.id)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCode != "" {
					errutil.AssertErrorCode(t, err, tt.errCode)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, result.Name)
			assert.Equal(t, locID.String(), result.ID)
		})
	}
}

func TestServiceProxy_EmitEvent(t *testing.T) {
	tests := []struct {
		name      string
		stream    string
		eventType string
		payload   []byte
		setup     func(m *mockEventStore)
		wantErr   bool
	}{
		{
			name:      "successful emit",
			stream:    "location:123",
			eventType: "say",
			payload:   []byte(`{"message":"hello"}`),
			setup: func(m *mockEventStore) {
				m.appendFn = func(_ context.Context, event core.Event) error {
					assert.Equal(t, "location:123", event.Stream)
					assert.Equal(t, core.EventType("say"), event.Type)
					assert.Equal(t, core.ActorPlugin, event.Actor.Kind)
					return nil
				}
			},
		},
		{
			name:      "event store error",
			stream:    "location:456",
			eventType: "pose",
			payload:   []byte(`{}`),
			setup: func(m *mockEventStore) {
				m.appendFn = func(_ context.Context, _ core.Event) error {
					return errors.New("store unavailable")
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			es := &mockEventStore{}
			tt.setup(es)
			proxy := newTestProxy(t, &mockWorldService{}, es)

			err := proxy.EmitEvent(context.Background(), tt.stream, tt.eventType, tt.payload)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestServiceProxy_SetPlayerAlias(t *testing.T) {
	testPlayerID := makeTestID()

	tests := []struct {
		name        string
		playerID    string
		alias       string
		cmd         string
		aliasCache  *mockAliasCacheAccess
		aliasWriter *mockAliasWriter
		wantErr     bool
		errCode     string
	}{
		{
			name:     "valid alias set",
			playerID: testPlayerID.String(),
			alias:    "n",
			cmd:      "move north",
			aliasCache: &mockAliasCacheAccess{
				setPlayerAlias: func(playerID ulid.ULID, alias, cmd string) error {
					assert.Equal(t, testPlayerID, playerID)
					assert.Equal(t, "n", alias)
					assert.Equal(t, "move north", cmd)
					return nil
				},
			},
			aliasWriter: &mockAliasWriter{
				setPlayerAlias: func(_ context.Context, playerID ulid.ULID, _, _ string) error {
					assert.Equal(t, testPlayerID, playerID)
					return nil
				},
			},
		},
		{
			name:     "invalid player ID",
			playerID: "not-valid",
			alias:    "n",
			cmd:      "move north",
			wantErr:  true,
			errCode:  "INVALID_ID",
		},
		{
			name:     "cache error",
			playerID: testPlayerID.String(),
			alias:    "circ",
			cmd:      "circ",
			aliasWriter: &mockAliasWriter{
				setPlayerAlias: func(_ context.Context, _ ulid.ULID, _, _ string) error {
					return nil
				},
			},
			aliasCache: &mockAliasCacheAccess{
				setPlayerAlias: func(_ ulid.ULID, _, _ string) error {
					return errors.New("circular alias")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy, err := NewServiceProxy(ServiceProxyConfig{
				World:       &mockWorldService{},
				Events:      &mockEventStore{},
				AliasCache:  tt.aliasCache,
				AliasWriter: tt.aliasWriter,
			})
			require.NoError(t, err)

			err = proxy.SetPlayerAlias(context.Background(), tt.playerID, tt.alias, tt.cmd)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCode != "" {
					errutil.AssertErrorCode(t, err, tt.errCode)
				}
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestServiceProxy_KVGet(t *testing.T) {
	proxy := newTestProxy(t, &mockWorldService{}, &mockEventStore{})

	_, _, err := proxy.KVGet(context.Background(), "my-plugin", "key")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "NOT_IMPLEMENTED")
}

func TestServiceProxy_IDConversion(t *testing.T) {
	t.Run("valid ULID string parses correctly", func(t *testing.T) {
		id := ulid.Make()
		parsed, err := parseULID(id.String())
		require.NoError(t, err)
		assert.Equal(t, id, parsed)
	})

	t.Run("invalid ULID string returns INVALID_ID error", func(t *testing.T) {
		_, err := parseULID("garbage")
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "INVALID_ID")
	})

	t.Run("empty string returns INVALID_ID error", func(t *testing.T) {
		_, err := parseULID("")
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "INVALID_ID")
	})

	t.Run("ulidPtrToString handles nil", func(t *testing.T) {
		assert.Equal(t, "", ulidPtrToString(nil))
	})

	t.Run("ulidPtrToString handles non-nil", func(t *testing.T) {
		id := ulid.Make()
		assert.Equal(t, id.String(), ulidPtrToString(&id))
	})
}

func TestServiceProxy_GetStartingLocationID(t *testing.T) {
	t.Run("returns configured ID", func(t *testing.T) {
		proxy, err := NewServiceProxy(ServiceProxyConfig{
			World:         &mockWorldService{},
			Events:        &mockEventStore{},
			StartingLocID: "01ABC",
		})
		require.NoError(t, err)

		id, err := proxy.GetStartingLocationID(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "01ABC", id)
	})

	t.Run("returns error when not configured", func(t *testing.T) {
		proxy := newTestProxy(t, &mockWorldService{}, &mockEventStore{})

		_, err := proxy.GetStartingLocationID(context.Background())
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "NO_STARTING_LOCATION")
	})
}

func TestServiceProxy_CheckAliasShadow(t *testing.T) {
	t.Run("no shadow when no registry", func(t *testing.T) {
		proxy := newTestProxy(t, &mockWorldService{}, &mockEventStore{})

		shadows, name, err := proxy.CheckAliasShadow(context.Background(), "say")
		require.NoError(t, err)
		assert.False(t, shadows)
		assert.Empty(t, name)
	})

	t.Run("detects command shadow", func(t *testing.T) {
		reg := &mockCommandRegistry{
			get: func(name string) (command.CommandEntry, bool) {
				if name == "say" {
					return command.NewTestEntry(command.CommandEntryConfig{
						Name: "say",
						Help: "Say something",
					}), true
				}
				return command.CommandEntry{}, false
			},
		}

		proxy, err := NewServiceProxy(ServiceProxyConfig{
			World:           &mockWorldService{},
			Events:          &mockEventStore{},
			CommandRegistry: reg,
		})
		require.NoError(t, err)

		shadows, name, err := proxy.CheckAliasShadow(context.Background(), "say")
		require.NoError(t, err)
		assert.True(t, shadows)
		assert.Equal(t, "say", name)
	})

	t.Run("no shadow for unknown command", func(t *testing.T) {
		reg := &mockCommandRegistry{
			get: func(_ string) (command.CommandEntry, bool) {
				return command.CommandEntry{}, false
			},
		}

		proxy, err := NewServiceProxy(ServiceProxyConfig{
			World:           &mockWorldService{},
			Events:          &mockEventStore{},
			CommandRegistry: reg,
		})
		require.NoError(t, err)

		shadows, name, err := proxy.CheckAliasShadow(context.Background(), "nonexistent")
		require.NoError(t, err)
		assert.False(t, shadows)
		assert.Empty(t, name)
	})
}

func TestServiceProxy_ListCommands(t *testing.T) {
	t.Run("returns nil when no registry", func(t *testing.T) {
		proxy := newTestProxy(t, &mockWorldService{}, &mockEventStore{})

		cmds, err := proxy.ListCommands(context.Background(), "char-1")
		require.NoError(t, err)
		assert.Nil(t, cmds)
	})

	t.Run("returns command list", func(t *testing.T) {
		reg := &mockCommandRegistry{
			all: func() []command.CommandEntry {
				return []command.CommandEntry{
					command.NewTestEntry(command.CommandEntryConfig{
						Name:   "say",
						Help:   "Say something",
						Usage:  "say <message>",
						Source: "core",
					}),
					command.NewTestEntry(command.CommandEntryConfig{
						Name:   "pose",
						Help:   "Pose an action",
						Usage:  "pose <action>",
						Source: "core",
					}),
				}
			},
		}

		proxy, err := NewServiceProxy(ServiceProxyConfig{
			World:           &mockWorldService{},
			Events:          &mockEventStore{},
			CommandRegistry: reg,
		})
		require.NoError(t, err)

		cmds, err := proxy.ListCommands(context.Background(), "char-1")
		require.NoError(t, err)
		require.Len(t, cmds, 2)
		assert.Equal(t, "say", cmds[0].Name)
		assert.Equal(t, "pose", cmds[1].Name)
	})
}

func TestServiceProxy_FindSessionByName(t *testing.T) {
	charID := makeTestID()
	locID := makeTestID()

	t.Run("returns nil when no sessions configured", func(t *testing.T) {
		proxy := newTestProxy(t, &mockWorldService{}, &mockEventStore{})

		_, err := proxy.FindSessionByName(context.Background(), "Alaric")
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "NO_SESSION_STORE")
	})

	t.Run("returns session result", func(t *testing.T) {
		sess := &mockSessionAccess{
			findByCharacterName: func(_ context.Context, name string) (*session.Info, error) {
				if name == "Alaric" {
					return &session.Info{
						ID:            "sess-1",
						CharacterID:   charID,
						CharacterName: "Alaric",
						LocationID:    locID,
						GridPresent:   true,
					}, nil
				}
				return nil, nil
			},
		}

		proxy, err := NewServiceProxy(ServiceProxyConfig{
			World:    &mockWorldService{},
			Events:   &mockEventStore{},
			Sessions: sess,
		})
		require.NoError(t, err)

		result, err := proxy.FindSessionByName(context.Background(), "Alaric")
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "Alaric", result.CharacterName)
		assert.Equal(t, charID.String(), result.CharacterID)
		assert.True(t, result.GridPresent)
	})

	t.Run("returns nil for unknown name", func(t *testing.T) {
		sess := &mockSessionAccess{
			findByCharacterName: func(_ context.Context, _ string) (*session.Info, error) {
				return nil, nil
			},
		}

		proxy, err := NewServiceProxy(ServiceProxyConfig{
			World:    &mockWorldService{},
			Events:   &mockEventStore{},
			Sessions: sess,
		})
		require.NoError(t, err)

		result, err := proxy.FindSessionByName(context.Background(), "Nobody")
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestServiceProxy_Log(t *testing.T) {
	proxy := newTestProxy(t, &mockWorldService{}, &mockEventStore{})

	// Log should not panic for any level
	proxy.Log(context.Background(), "debug", "test debug")
	proxy.Log(context.Background(), "info", "test info")
	proxy.Log(context.Background(), "warn", "test warn")
	proxy.Log(context.Background(), "error", "test error")
	proxy.Log(context.Background(), "unknown", "test unknown level")
}
