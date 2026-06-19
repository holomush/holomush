// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// recordingHistoryReader records whether ReplayTail was reached and with which
// stream, so a test can assert a denied read never hits the bus.
type recordingHistoryReader struct {
	called    bool
	gotStream string
}

func (r *recordingHistoryReader) ReplayTail(_ context.Context, stream string, _ int, _ time.Time, _ ulid.ULID) ([]core.Event, error) {
	r.called = true
	r.gotStream = stream
	return nil, nil
}

// streamHostCaps is a focused HostCapabilities stub for streamHistoryServer
// tests: a configurable ABAC engine, game ID, and a recording history reader.
// Everything else inherits stubHostCaps' nil accessors.
type streamHostCaps struct {
	stubHostCaps
	engine types.AccessPolicyEngine
	gameID string
	reader plugins.HistoryReader
}

func (c streamHostCaps) AccessEngine() types.AccessPolicyEngine { return c.engine }
func (c streamHostCaps) GameID() string                         { return c.gameID }
func (c streamHostCaps) HistoryReader() plugins.HistoryReader   { return c.reader }

func newStreamServer(engine types.AccessPolicyEngine, reader plugins.HistoryReader) hostv1.StreamHistoryServiceServer {
	return hostcap.NewStreamHistoryServer(hostcap.NewBase(streamHostCaps{engine: engine, gameID: "main", reader: reader}, "test-plugin"))
}

// recordingStreamEngine records the resource it is asked to evaluate and denies,
// so a test can prove the handler QUALIFIES a relative stream before the ABAC
// check (the bug holomush-xakba fixes: evaluating the un-qualified form).
type recordingStreamEngine struct{ gotResource string }

func (e *recordingStreamEngine) Evaluate(_ context.Context, req types.AccessRequest) (types.Decision, error) {
	e.gotResource = req.Resource
	return types.NewDecision(types.EffectDefaultDeny, "test", ""), nil
}

func (e *recordingStreamEngine) CanPerformAction(_ context.Context, _, _, _, _ string) (bool, error) {
	return true, nil
}

// Verifies: INV-PLUGIN-50
func TestStreamHistoryQueryStreamHistoryGate(t *testing.T) {
	// The capability interceptor authorizes stream.history only at the type level
	// (stream:*); the handler MUST evaluate the concrete stream and reach ReplayTail
	// ONLY when the gate permits. A denied / wildcard / nil-engine read must fail
	// closed and never reach ReplayTail; a permitted read is delegated with the
	// relative ref intact (holomush-xakba).
	tests := []struct {
		name         string
		engine       types.AccessPolicyEngine
		stream       string
		wantCode     codes.Code // codes.OK ⇒ expect success
		wantReplayed bool
	}{
		{"policy-denied stream not replayed", policytest.DenyAllEngine(), "system.rekey.01CT000.01CID00", codes.PermissionDenied, false},
		{"permitted stream replayed", policytest.AllowAllEngine(), "location.01LOCAAAAAAAAAAAAAAAAAA", codes.OK, true},
		{"wildcard stream rejected", policytest.AllowAllEngine(), "location.>", codes.Internal, false},
		{"nil engine fails closed", nil, "location.01LOCAAAAAAAAAAAAAAAAAA", codes.Internal, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reader := &recordingHistoryReader{}
			srv := newStreamServer(tc.engine, reader)

			_, err := srv.QueryStreamHistory(context.Background(), &hostv1.QueryStreamHistoryRequest{Stream: tc.stream})
			if tc.wantCode == codes.OK {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Equal(t, tc.wantCode, status.Code(err))
			}
			assert.Equal(t, tc.wantReplayed, reader.called)
			if tc.wantReplayed {
				assert.Equal(t, tc.stream, reader.gotStream,
					"ReplayTail receives the relative ref (the bus adapter re-qualifies)")
			}
		})
	}
}

// Verifies: INV-PLUGIN-50
func TestStreamHistoryQualifiesRelativeStreamBeforeABAC(t *testing.T) {
	// A plugin sends a DOMAIN-RELATIVE stream ref ("system.rekey..."); the handler
	// MUST qualify it (events.<gameID>.<rel>) before the ABAC check so the
	// system/audit/crypto forbids (keyed on the qualified resource.stream.name) can
	// match. Without qualification the resource was "stream:system.rekey..." and no
	// forbid fired — the holomush-xakba bug.
	eng := &recordingStreamEngine{}
	reader := &recordingHistoryReader{}
	srv := hostcap.NewStreamHistoryServer(hostcap.NewBase(
		streamHostCaps{engine: eng, gameID: "main", reader: reader}, "test-plugin",
	))

	_, err := srv.QueryStreamHistory(context.Background(), &hostv1.QueryStreamHistoryRequest{
		Stream: "system.rekey.01CT000.01CID00", // relative, as a plugin sends it
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Equal(t, "stream:events.main.system.rekey.01CT000.01CID00", eng.gotResource,
		"handler must evaluate the QUALIFIED stream so the system forbid can match")
	assert.False(t, reader.called)
}
