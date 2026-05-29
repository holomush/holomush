// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"testing"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureCommandHandler records the CommandRequest it receives so a test can
// assert which proto fields the server adapter mapped through.
type captureCommandHandler struct {
	got CommandRequest
}

func (h *captureCommandHandler) HandleCommand(_ context.Context, req CommandRequest) (*CommandResponse, error) {
	h.got = req
	return OK("ok"), nil
}

// TestPluginServerAdapterHandleCommandMapsConnectionID is the regression test
// for holomush-dble7: the binary-plugin SDK server adapter MUST copy the proto
// CommandRequest.connection_id (field 9) into pluginsdk.CommandRequest.ConnectionID.
// Dropping it (the original bug) made `scene focus`/`scene grid` reject every
// real web command with "requires a live connection", because the handler's
// req.ConnectionID was always empty. This is the binary-runtime analogue of the
// in-process Lua path (which never round-trips through proto), so dropping it
// here is also a plugin-runtime-symmetry violation.
func TestPluginServerAdapterHandleCommandMapsConnectionID(t *testing.T) {
	tests := []struct {
		name   string
		connID string
	}{
		{"maps a populated connection_id through to the handler", "01KSTEHGQEXR7J0B0VK5GBKG1F"},
		// Legacy/scripted callers omit connection_id (executeViaDispatcher
		// allows empty); the adapter must pass the empty value through, not
		// substitute a sentinel.
		{"passes an empty connection_id through unchanged", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &captureCommandHandler{}
			adapter := &pluginServerAdapter{cmdHandler: handler}

			_, err := adapter.HandleCommand(context.Background(), &pluginv1.HandleCommandRequest{
				Command: &pluginv1.CommandRequest{
					Command:      "scene",
					Args:         "focus #01KSTEHGRW53M7Y5RE0XCP74HZ",
					CharacterId:  "char-1",
					SessionId:    "01KSTEHGPG40KRF87BB72WNZNM",
					ConnectionId: tt.connID,
				},
			})
			require.NoError(t, err)
			assert.Equal(t, tt.connID, handler.got.ConnectionID,
				"adapter MUST map proto connection_id into CommandRequest.ConnectionID (holomush-dble7)")
		})
	}
}
