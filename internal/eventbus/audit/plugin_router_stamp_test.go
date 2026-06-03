// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// TestAuditRowOfStampedByRouter verifies the C.0 substrate: when the
// PluginHistoryRouter converts a QueryHistoryResponse into an
// eventbus.Event, the underlying *pluginauditpb.AuditRow MUST be
// stamped onto the event so eventbus.AuditRowOf returns it (pointer-
// equal). This is the substrate the Phase 7 PluginDowngradeFence
// (INV-CRYPTO-42 + INV-CRYPTO-50) reads from.
func TestAuditRowOfStampedByRouter(t *testing.T) {
	t.Parallel()

	id := core.NewULID()
	idBytes := id.Bytes()
	dekRef := uint64(42)
	dekVer := uint32(1)

	row := &pluginv1.AuditRow{
		Id:         idBytes[:],
		Subject:    "events.main.scene.01ABC.ic",
		Type:       "test-plugin:secret",
		Timestamp:  timestamppb.New(time.Unix(1234, 0)),
		Codec:      "xchacha20poly1305-v1",
		Payload:    []byte("ciphertext"),
		DekRef:     &dekRef,
		DekVersion: &dekVer,
	}

	fs := &fakeStream{
		resps: []*pluginv1.QueryHistoryResponse{{Row: row}},
	}
	provider := stubProvider{name: "test-plugin", client: &fakeHistoryClient{stream: fs}}
	router := audit.NewPluginHistoryRouter(provider)

	stream, err := router.QueryHistory(context.Background(), "test-plugin", eventbus.HistoryQuery{
		Subject:  "events.main.scene.01ABC.ic",
		PageSize: 10,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	ev, err := stream.Next(context.Background())
	require.NoError(t, err)

	got := eventbus.AuditRowOf(ev)
	require.NotNil(t, got, "AuditRowOf MUST return non-nil row stamped by router")
	// Pointer-equal: the router MUST stamp the same pointer it received,
	// not a copy. The fence relies on this for cheap field access.
	assert.Same(t, row, got, "stamped audit row MUST be pointer-equal to source")
}

// TestAuditRowOfNilForUnstampedEvent confirms the accessor's nil-safety
// contract — an Event constructed without a router stamp returns nil,
// distinguishing host-owned subjects (no plugin source-of-truth) from
// plugin-routed events.
func TestAuditRowOfNilForUnstampedEvent(t *testing.T) {
	t.Parallel()
	ev := eventbus.Event{Subject: "events.main.host.foo", Type: "host.event"}
	assert.Nil(t, eventbus.AuditRowOf(ev),
		"Event constructed without router stamp MUST have nil audit row")
}
