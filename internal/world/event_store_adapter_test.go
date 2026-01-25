// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
)

// mockEventStore implements the minimal interface needed by EventStoreAdapter.
type mockEventStore struct {
	mock.Mock
}

func (m *mockEventStore) Append(ctx context.Context, event core.Event) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func TestEventStoreAdapter_Emit(t *testing.T) {
	tests := []struct {
		name         string
		stream       string
		eventType    string
		payload      []byte
		storeErr     error
		wantErr      bool
		wantErrCode  string
		checkEventFn func(t *testing.T, event core.Event)
	}{
		{
			name:      "successful emit",
			stream:    "location:01HWGA12345678901234567890",
			eventType: "move",
			payload:   []byte(`{"entity_type":"character"}`),
			storeErr:  nil,
			wantErr:   false,
			checkEventFn: func(t *testing.T, event core.Event) {
				assert.Equal(t, "location:01HWGA12345678901234567890", event.Stream)
				assert.Equal(t, core.EventType("move"), event.Type)
				assert.Equal(t, []byte(`{"entity_type":"character"}`), event.Payload)
				assert.Equal(t, core.ActorSystem, event.Actor.Kind)
				assert.Equal(t, "world-service", event.Actor.ID)
				assert.False(t, event.ID.IsZero(), "event ID should be generated")
				assert.False(t, event.Timestamp.IsZero(), "timestamp should be set")
			},
		},
		{
			name:        "store error propagated",
			stream:      "location:01HWGA12345678901234567890",
			eventType:   "move",
			payload:     []byte(`{}`),
			storeErr:    errors.New("database connection failed"),
			wantErr:     true,
			wantErrCode: "EVENT_STORE_APPEND_FAILED",
		},
		{
			name:      "empty payload allowed",
			stream:    "character:01HWGA12345678901234567890",
			eventType: "ping",
			payload:   []byte{},
			storeErr:  nil,
			wantErr:   false,
		},
		{
			name:      "nil payload allowed",
			stream:    "character:01HWGA12345678901234567890",
			eventType: "ping",
			payload:   nil,
			storeErr:  nil,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := &mockEventStore{}

			var capturedEvent core.Event
			store.On("Append", ctx, mock.AnythingOfType("core.Event")).
				Run(func(args mock.Arguments) {
					capturedEvent = args.Get(1).(core.Event)
				}).
				Return(tt.storeErr)

			adapter := world.NewEventStoreAdapter(store)
			err := adapter.Emit(ctx, tt.stream, tt.eventType, tt.payload)

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrCode != "" {
					var oopsErr oops.OopsError
					require.True(t, errors.As(err, &oopsErr))
					assert.Equal(t, tt.wantErrCode, oopsErr.Code())
				}
			} else {
				require.NoError(t, err)
				store.AssertCalled(t, "Append", ctx, mock.AnythingOfType("core.Event"))
				if tt.checkEventFn != nil {
					tt.checkEventFn(t, capturedEvent)
				}
			}
		})
	}
}

func TestEventStoreAdapter_EventIDGeneration(t *testing.T) {
	// Verify each emit generates a unique event ID
	ctx := context.Background()
	store := &mockEventStore{}

	var capturedIDs []ulid.ULID
	store.On("Append", ctx, mock.AnythingOfType("core.Event")).
		Run(func(args mock.Arguments) {
			event := args.Get(1).(core.Event)
			capturedIDs = append(capturedIDs, event.ID)
		}).
		Return(nil)

	adapter := world.NewEventStoreAdapter(store)

	// Emit multiple events
	for i := 0; i < 3; i++ {
		err := adapter.Emit(ctx, "test:stream", "test", []byte(`{}`))
		require.NoError(t, err)
	}

	// All IDs should be unique
	require.Len(t, capturedIDs, 3)
	assert.NotEqual(t, capturedIDs[0], capturedIDs[1])
	assert.NotEqual(t, capturedIDs[1], capturedIDs[2])
	assert.NotEqual(t, capturedIDs[0], capturedIDs[2])
}

func TestEventStoreAdapter_ImplementsEventEmitter(_ *testing.T) {
	// Compile-time check that EventStoreAdapter implements EventEmitter
	var _ world.EventEmitter = (*world.EventStoreAdapter)(nil)
}
