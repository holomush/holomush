package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"
	"github.com/pashagolub/pgxmock/v4"

	"github.com/holomush/holomush/internal/core"
)

// testEvent creates a test event with the given parameters.
func testEvent(stream string, eventType core.EventType) core.Event {
	return core.Event{
		ID:        core.NewULID(),
		Stream:    stream,
		Type:      eventType,
		Timestamp: time.Now().UTC().Truncate(time.Microsecond),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-123"},
		Payload:   []byte(`{"message":"test"}`),
	}
}

func TestPostgresEventStore_Append(t *testing.T) {
	tests := []struct {
		name      string
		event     core.Event
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name:  "successful append",
			event: testEvent("location:room-1", core.EventTypeSay),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO events`).
					WithArgs(
						pgxmock.AnyArg(), // id
						pgxmock.AnyArg(), // stream
						pgxmock.AnyArg(), // type
						pgxmock.AnyArg(), // actor_kind
						pgxmock.AnyArg(), // actor_id
						pgxmock.AnyArg(), // payload
						pgxmock.AnyArg(), // created_at
					).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
			wantErr: false,
		},
		{
			name:  "database error on insert",
			event: testEvent("location:room-1", core.EventTypeSay),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO events`).
					WithArgs(
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
					).
					WillReturnError(errors.New("connection refused"))
			},
			wantErr: true,
			errMsg:  "failed to append event",
		},
		{
			name:  "append pose event",
			event: testEvent("location:room-1", core.EventTypePose),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO events`).
					WithArgs(
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
					).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
			wantErr: false,
		},
		{
			name:  "append system event",
			event: testEvent("system:global", core.EventTypeSystem),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO events`).
					WithArgs(
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
					).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer mock.Close()

			tt.setupMock(mock)

			store := &PostgresEventStore{pool: mock}
			err = store.Append(context.Background(), tt.event)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errMsg != "" && !containsString(err.Error(), tt.errMsg) {
					t.Errorf("error message %q should contain %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestPostgresEventStore_Replay(t *testing.T) {
	validULID := core.NewULID()
	validULIDStr := validULID.String()
	timestamp := time.Now().UTC().Truncate(time.Microsecond)

	tests := []struct {
		name       string
		stream     string
		afterID    ulid.ULID
		limit      int
		setupMock  func(mock pgxmock.PgxPoolIface)
		wantCount  int
		wantErr    bool
		errMsg     string
		checkEvent func(t *testing.T, events []core.Event)
	}{
		{
			name:    "replay from beginning",
			stream:  "location:room-1",
			afterID: ulid.ULID{},
			limit:   10,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"id", "stream", "type", "actor_kind", "actor_id", "payload", "created_at"}).
					AddRow(validULIDStr, "location:room-1", "say", core.ActorCharacter, "char-123", []byte(`{}`), timestamp).
					AddRow(core.NewULID().String(), "location:room-1", "pose", core.ActorCharacter, "char-456", []byte(`{}`), timestamp)
				mock.ExpectQuery(`SELECT id, stream, type, actor_kind, actor_id, payload, created_at FROM events WHERE stream = \$1 ORDER BY id LIMIT \$2`).
					WithArgs("location:room-1", 10).
					WillReturnRows(rows)
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:    "replay after specific ID",
			stream:  "location:room-1",
			afterID: validULID,
			limit:   10,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"id", "stream", "type", "actor_kind", "actor_id", "payload", "created_at"}).
					AddRow(core.NewULID().String(), "location:room-1", "say", core.ActorCharacter, "char-123", []byte(`{}`), timestamp)
				mock.ExpectQuery(`SELECT id, stream, type, actor_kind, actor_id, payload, created_at FROM events WHERE stream = \$1 AND id > \$2 ORDER BY id LIMIT \$3`).
					WithArgs("location:room-1", validULIDStr, 10).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:    "replay empty stream",
			stream:  "location:empty",
			afterID: ulid.ULID{},
			limit:   10,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"id", "stream", "type", "actor_kind", "actor_id", "payload", "created_at"})
				mock.ExpectQuery(`SELECT id, stream, type, actor_kind, actor_id, payload, created_at FROM events WHERE stream = \$1 ORDER BY id LIMIT \$2`).
					WithArgs("location:empty", 10).
					WillReturnRows(rows)
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:    "query error",
			stream:  "location:room-1",
			afterID: ulid.ULID{},
			limit:   10,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT id, stream, type, actor_kind, actor_id, payload, created_at FROM events WHERE stream = \$1 ORDER BY id LIMIT \$2`).
					WithArgs("location:room-1", 10).
					WillReturnError(errors.New("database error"))
			},
			wantErr: true,
			errMsg:  "failed to query events",
		},
		{
			name:    "scan error - invalid ULID",
			stream:  "location:room-1",
			afterID: ulid.ULID{},
			limit:   10,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"id", "stream", "type", "actor_kind", "actor_id", "payload", "created_at"}).
					AddRow("invalid-ulid", "location:room-1", "say", core.ActorCharacter, "char-123", []byte(`{}`), timestamp)
				mock.ExpectQuery(`SELECT id, stream, type, actor_kind, actor_id, payload, created_at FROM events WHERE stream = \$1 ORDER BY id LIMIT \$2`).
					WithArgs("location:room-1", 10).
					WillReturnRows(rows)
			},
			wantErr: true,
			errMsg:  "corrupt event ID",
		},
		{
			name:    "replay with limit",
			stream:  "location:room-1",
			afterID: ulid.ULID{},
			limit:   2,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"id", "stream", "type", "actor_kind", "actor_id", "payload", "created_at"}).
					AddRow(core.NewULID().String(), "location:room-1", "say", core.ActorCharacter, "char-123", []byte(`{}`), timestamp).
					AddRow(core.NewULID().String(), "location:room-1", "pose", core.ActorCharacter, "char-456", []byte(`{}`), timestamp)
				mock.ExpectQuery(`SELECT id, stream, type, actor_kind, actor_id, payload, created_at FROM events WHERE stream = \$1 ORDER BY id LIMIT \$2`).
					WithArgs("location:room-1", 2).
					WillReturnRows(rows)
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:    "replay all event types",
			stream:  "location:room-1",
			afterID: ulid.ULID{},
			limit:   10,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"id", "stream", "type", "actor_kind", "actor_id", "payload", "created_at"}).
					AddRow(core.NewULID().String(), "location:room-1", "say", core.ActorCharacter, "char-123", []byte(`{}`), timestamp).
					AddRow(core.NewULID().String(), "location:room-1", "pose", core.ActorCharacter, "char-123", []byte(`{}`), timestamp).
					AddRow(core.NewULID().String(), "location:room-1", "arrive", core.ActorCharacter, "char-123", []byte(`{}`), timestamp).
					AddRow(core.NewULID().String(), "location:room-1", "leave", core.ActorCharacter, "char-123", []byte(`{}`), timestamp).
					AddRow(core.NewULID().String(), "location:room-1", "system", core.ActorSystem, "system", []byte(`{}`), timestamp)
				mock.ExpectQuery(`SELECT id, stream, type, actor_kind, actor_id, payload, created_at FROM events WHERE stream = \$1 ORDER BY id LIMIT \$2`).
					WithArgs("location:room-1", 10).
					WillReturnRows(rows)
			},
			wantCount: 5,
			wantErr:   false,
			checkEvent: func(t *testing.T, events []core.Event) {
				t.Helper()
				expectedTypes := []core.EventType{
					core.EventTypeSay,
					core.EventTypePose,
					core.EventTypeArrive,
					core.EventTypeLeave,
					core.EventTypeSystem,
				}
				for i, et := range expectedTypes {
					if events[i].Type != et {
						t.Errorf("event %d: expected type %q, got %q", i, et, events[i].Type)
					}
				}
			},
		},
		{
			name:    "replay all actor kinds",
			stream:  "location:room-1",
			afterID: ulid.ULID{},
			limit:   10,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"id", "stream", "type", "actor_kind", "actor_id", "payload", "created_at"}).
					AddRow(core.NewULID().String(), "location:room-1", "say", core.ActorCharacter, "char-123", []byte(`{}`), timestamp).
					AddRow(core.NewULID().String(), "location:room-1", "system", core.ActorSystem, "system", []byte(`{}`), timestamp).
					AddRow(core.NewULID().String(), "location:room-1", "say", core.ActorPlugin, "plugin-test", []byte(`{}`), timestamp)
				mock.ExpectQuery(`SELECT id, stream, type, actor_kind, actor_id, payload, created_at FROM events WHERE stream = \$1 ORDER BY id LIMIT \$2`).
					WithArgs("location:room-1", 10).
					WillReturnRows(rows)
			},
			wantCount: 3,
			wantErr:   false,
			checkEvent: func(t *testing.T, events []core.Event) {
				t.Helper()
				expectedKinds := []core.ActorKind{
					core.ActorCharacter,
					core.ActorSystem,
					core.ActorPlugin,
				}
				for i, ak := range expectedKinds {
					if events[i].Actor.Kind != ak {
						t.Errorf("event %d: expected actor kind %d, got %d", i, ak, events[i].Actor.Kind)
					}
				}
			},
		},
		{
			name:    "scan error - type mismatch",
			stream:  "location:room-1",
			afterID: ulid.ULID{},
			limit:   10,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				// Return mismatched types to trigger scan error
				rows := pgxmock.NewRows([]string{"id", "stream", "type", "actor_kind", "actor_id", "payload", "created_at"}).
					AddRow(core.NewULID().String(), "location:room-1", "say", "not-an-int", "char-123", []byte(`{}`), timestamp)
				mock.ExpectQuery(`SELECT id, stream, type, actor_kind, actor_id, payload, created_at FROM events WHERE stream = \$1 ORDER BY id LIMIT \$2`).
					WithArgs("location:room-1", 10).
					WillReturnRows(rows)
			},
			wantErr: true,
			errMsg:  "failed to scan event row",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer mock.Close()

			tt.setupMock(mock)

			store := &PostgresEventStore{pool: mock}
			events, err := store.Replay(context.Background(), tt.stream, tt.afterID, tt.limit)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errMsg != "" && !containsString(err.Error(), tt.errMsg) {
					t.Errorf("error message %q should contain %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(events) != tt.wantCount {
					t.Errorf("expected %d events, got %d", tt.wantCount, len(events))
				}
				if tt.checkEvent != nil {
					tt.checkEvent(t, events)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestPostgresEventStore_LastEventID(t *testing.T) {
	validULID := core.NewULID()
	validULIDStr := validULID.String()

	tests := []struct {
		name      string
		stream    string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantID    ulid.ULID
		wantErr   error
		errMsg    string
	}{
		{
			name:   "successful last event ID",
			stream: "location:room-1",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"id"}).AddRow(validULIDStr)
				mock.ExpectQuery(`SELECT id FROM events WHERE stream = \$1 ORDER BY id DESC LIMIT 1`).
					WithArgs("location:room-1").
					WillReturnRows(rows)
			},
			wantID:  validULID,
			wantErr: nil,
		},
		{
			name:   "empty stream returns ErrStreamEmpty",
			stream: "location:empty",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT id FROM events WHERE stream = \$1 ORDER BY id DESC LIMIT 1`).
					WithArgs("location:empty").
					WillReturnError(pgx.ErrNoRows)
			},
			wantID:  ulid.ULID{},
			wantErr: core.ErrStreamEmpty,
		},
		{
			name:   "database error",
			stream: "location:room-1",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT id FROM events WHERE stream = \$1 ORDER BY id DESC LIMIT 1`).
					WithArgs("location:room-1").
					WillReturnError(errors.New("connection lost"))
			},
			wantID: ulid.ULID{},
			errMsg: "failed to query last event ID",
		},
		{
			name:   "corrupt ULID in database",
			stream: "location:room-1",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"id"}).AddRow("not-a-valid-ulid")
				mock.ExpectQuery(`SELECT id FROM events WHERE stream = \$1 ORDER BY id DESC LIMIT 1`).
					WithArgs("location:room-1").
					WillReturnRows(rows)
			},
			wantID: ulid.ULID{},
			errMsg: "corrupt event ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer mock.Close()

			tt.setupMock(mock)

			store := &PostgresEventStore{pool: mock}
			id, err := store.LastEventID(context.Background(), tt.stream)

			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
			case tt.errMsg != "":
				if err == nil {
					t.Error("expected error, got nil")
				} else if !containsString(err.Error(), tt.errMsg) {
					t.Errorf("error message %q should contain %q", err.Error(), tt.errMsg)
				}
			default:
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if id != tt.wantID {
					t.Errorf("expected ID %v, got %v", tt.wantID, id)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestPostgresEventStore_GetSystemInfo(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantValue string
		wantErr   bool
		errIs     error
		errMsg    string
	}{
		{
			name: "successful get",
			key:  "game_id",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"value"}).AddRow("test-value-123")
				mock.ExpectQuery(`SELECT value FROM holomush_system_info WHERE key = \$1`).
					WithArgs("game_id").
					WillReturnRows(rows)
			},
			wantValue: "test-value-123",
			wantErr:   false,
		},
		{
			name: "key not found",
			key:  "nonexistent",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT value FROM holomush_system_info WHERE key = \$1`).
					WithArgs("nonexistent").
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: true,
			errIs:   ErrSystemInfoNotFound,
		},
		{
			name: "database error",
			key:  "game_id",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT value FROM holomush_system_info WHERE key = \$1`).
					WithArgs("game_id").
					WillReturnError(errors.New("connection timeout"))
			},
			wantErr: true,
			errMsg:  "failed to get system info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer mock.Close()

			tt.setupMock(mock)

			store := &PostgresEventStore{pool: mock}
			value, err := store.GetSystemInfo(context.Background(), tt.key)

			switch {
			case tt.wantErr:
				switch {
				case err == nil:
					t.Error("expected error, got nil")
				case tt.errIs != nil && !errors.Is(err, tt.errIs):
					t.Errorf("expected error to wrap %v, got %v", tt.errIs, err)
				case tt.errMsg != "" && !containsString(err.Error(), tt.errMsg):
					t.Errorf("error message %q should contain %q", err.Error(), tt.errMsg)
				}
			default:
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if value != tt.wantValue {
					t.Errorf("expected value %q, got %q", tt.wantValue, value)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestPostgresEventStore_SetSystemInfo(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		value     string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name:  "successful insert",
			key:   "game_id",
			value: "test-game-id",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO holomush_system_info`).
					WithArgs("game_id", "test-game-id").
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
			wantErr: false,
		},
		{
			name:  "successful upsert (update existing)",
			key:   "game_id",
			value: "updated-game-id",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO holomush_system_info`).
					WithArgs("game_id", "updated-game-id").
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			wantErr: false,
		},
		{
			name:  "database error",
			key:   "game_id",
			value: "test-value",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO holomush_system_info`).
					WithArgs("game_id", "test-value").
					WillReturnError(errors.New("disk full"))
			},
			wantErr: true,
			errMsg:  "failed to set system info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer mock.Close()

			tt.setupMock(mock)

			store := &PostgresEventStore{pool: mock}
			err = store.SetSystemInfo(context.Background(), tt.key, tt.value)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errMsg != "" && !containsString(err.Error(), tt.errMsg) {
					t.Errorf("error message %q should contain %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestPostgresEventStore_InitGameID(t *testing.T) {
	existingID := core.NewULID().String()

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
		checkID   func(t *testing.T, id string)
	}{
		{
			name: "returns existing game_id",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"value"}).AddRow(existingID)
				mock.ExpectQuery(`SELECT value FROM holomush_system_info WHERE key = \$1`).
					WithArgs("game_id").
					WillReturnRows(rows)
			},
			wantErr: false,
			checkID: func(t *testing.T, id string) {
				t.Helper()
				if id != existingID {
					t.Errorf("expected existing ID %q, got %q", existingID, id)
				}
			},
		},
		{
			name: "generates new game_id when not found",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				// GetSystemInfo returns not found
				mock.ExpectQuery(`SELECT value FROM holomush_system_info WHERE key = \$1`).
					WithArgs("game_id").
					WillReturnError(pgx.ErrNoRows)
				// SetSystemInfo succeeds
				mock.ExpectExec(`INSERT INTO holomush_system_info`).
					WithArgs("game_id", pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
			wantErr: false,
			checkID: func(t *testing.T, id string) {
				t.Helper()
				if id == "" {
					t.Error("expected non-empty ID")
				}
				if len(id) != 26 {
					t.Errorf("expected ULID length 26, got %d", len(id))
				}
			},
		},
		{
			name: "returns error on GetSystemInfo failure (non-NotFound)",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT value FROM holomush_system_info WHERE key = \$1`).
					WithArgs("game_id").
					WillReturnError(errors.New("connection refused"))
			},
			wantErr: true,
			errMsg:  "failed to check for existing game_id",
		},
		{
			name: "returns error on SetSystemInfo failure",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT value FROM holomush_system_info WHERE key = \$1`).
					WithArgs("game_id").
					WillReturnError(pgx.ErrNoRows)
				mock.ExpectExec(`INSERT INTO holomush_system_info`).
					WithArgs("game_id", pgxmock.AnyArg()).
					WillReturnError(errors.New("write failed"))
			},
			wantErr: true,
			errMsg:  "failed to set system info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer mock.Close()

			tt.setupMock(mock)

			store := &PostgresEventStore{pool: mock}
			id, err := store.InitGameID(context.Background())

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errMsg != "" && !containsString(err.Error(), tt.errMsg) {
					t.Errorf("error message %q should contain %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if tt.checkID != nil {
					tt.checkID(t, id)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestPostgresEventStore_Migrate(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful migration",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				// First migration
				mock.ExpectExec("").WillReturnResult(pgxmock.NewResult("", 0))
				// Second migration
				mock.ExpectExec("").WillReturnResult(pgxmock.NewResult("", 0))
			},
			wantErr: false,
		},
		{
			name: "first migration fails",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("").WillReturnError(errors.New("syntax error"))
			},
			wantErr: true,
			errMsg:  "failed to run migration 001",
		},
		{
			name: "second migration fails",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("").WillReturnResult(pgxmock.NewResult("", 0))
				mock.ExpectExec("").WillReturnError(errors.New("table already exists"))
			},
			wantErr: true,
			errMsg:  "failed to run migration 002",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer mock.Close()

			tt.setupMock(mock)

			store := &PostgresEventStore{pool: mock}
			err = store.Migrate(context.Background())

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errMsg != "" && !containsString(err.Error(), tt.errMsg) {
					t.Errorf("error message %q should contain %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestPostgresEventStore_Close(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}

	store := &PostgresEventStore{pool: mock}
	store.Close()

	// Verify the mock was closed (pgxmock tracks this internally)
	// After Close(), pool operations should fail
}

func TestErrSystemInfoNotFound(t *testing.T) {
	if ErrSystemInfoNotFound.Error() != "system info key not found" {
		t.Errorf("unexpected error message: %s", ErrSystemInfoNotFound.Error())
	}
}

// containsString checks if s contains substr.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
