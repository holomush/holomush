// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// loadStubBinaryPlugin loads a no-op binary plugin under the given name and
// returns the grpc mock so tests can inspect the ctx the plugin sees.
func loadStubBinaryPlugin(t *testing.T, name string) (*Host, *mockGRPCPluginClient) {
	t.Helper()
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	reg, _ := stubRegistryFor(name)
	host := NewHostWithFactory(factory, WithIdentityRegistry(reg))
	t.Cleanup(func() { _ = host.Close(context.Background()) })

	tmpDir := t.TempDir()
	require.NoError(t, createTempExecutable(tmpDir+"/"+name), "create temp executable")

	manifest := &plugins.Manifest{
		Name:    name,
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: name,
		},
	}
	require.NoError(t, host.Load(context.Background(), manifest, tmpDir), "Load plugin")
	return host, grpcClient
}

// Verifies: INV-PLUGIN-51
func TestDeliverCommandStampsHostVouchedDispatchSubjectForCharacterActor(t *testing.T) {
	host, grpcMock := loadStubBinaryPlugin(t, "test-plugin")

	charID := core.NewULID().String()
	ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorCharacter, ID: charID})

	_, err := host.DeliverCommand(ctx, "test-plugin", pluginsdk.CommandRequest{})
	require.NoError(t, err, "DeliverCommand")

	dc, ok := pluginauthz.DispatchForHost(grpcMock.commandCtx)
	require.True(t, ok, "dispatch context must be stamped on the delivery ctx")
	assert.Equal(t, access.CharacterSubject(charID), dc.Subject, "host-vouched subject")
	assert.Nil(t, dc.Attributes, "attributes resolved by a follow-up wiring task")
}

// Verifies: INV-PLUGIN-51
func TestDeliverEventStampsHostVouchedDispatchSubjectForCharacterActor(t *testing.T) {
	host, grpcMock := loadStubBinaryPlugin(t, "test-plugin")

	charID := core.NewULID().String()
	ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorCharacter, ID: charID})

	_, err := host.DeliverEvent(ctx, "test-plugin", pluginsdk.Event{})
	require.NoError(t, err, "DeliverEvent")

	dc, ok := pluginauthz.DispatchForHost(grpcMock.eventCtx)
	require.True(t, ok, "dispatch context must be stamped on the delivery ctx")
	assert.Equal(t, access.CharacterSubject(charID), dc.Subject, "host-vouched subject")
}

// newDispatchHost builds a Host with the given options and registers cleanup so
// the host-owned emitTokenStore sweeper goroutine is stopped (package-level
// goleak guard in emit_token_store_test.go).
func newDispatchHost(t *testing.T, opts ...HostOption) *Host {
	t.Helper()
	h := NewHost(opts...)
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	return h
}

func TestStampDispatchLeavesNonCharacterActorUnchanged(t *testing.T) {
	base := context.Background()
	h := newDispatchHost(t)

	// Plugin actor: not vouched as a character subject.
	got := h.stampDispatch(base, core.Actor{Kind: core.ActorPlugin, ID: core.NewULID().String()})
	_, ok := pluginauthz.DispatchForHost(got)
	assert.False(t, ok, "plugin actor must not stamp a dispatch subject")

	// Character actor with empty ID: fail-closed, no stamp.
	got = h.stampDispatch(base, core.Actor{Kind: core.ActorCharacter, ID: ""})
	_, ok = pluginauthz.DispatchForHost(got)
	assert.False(t, ok, "empty-ID character actor must not stamp a dispatch subject")
}

// fakeAttrResolver is a test double for pluginauthz.AttributeResolver.
type fakeAttrResolver struct {
	attrs map[string]any
	err   error
}

func (f fakeAttrResolver) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return f.attrs, f.err
}

// Verifies: INV-PLUGIN-51
func TestStampDispatchResolvesCharacterAttributesWhenResolverWired(t *testing.T) {
	charID := core.NewULID().String()
	actor := core.Actor{Kind: core.ActorCharacter, ID: charID}

	t.Run("projects string-valued attributes and drops non-strings", func(t *testing.T) {
		h := newDispatchHost(t, WithDispatchAttributeResolver(fakeAttrResolver{
			attrs: map[string]any{
				"location":     "01LOC",
				"has_location": true,
				"roles":        []string{"player"},
			},
		}))
		got := h.stampDispatch(context.Background(), actor)
		dc, ok := pluginauthz.DispatchForHost(got)
		require.True(t, ok, "character actor must stamp a dispatch subject")
		assert.Equal(t, access.CharacterSubject(charID), dc.Subject, "host-vouched subject")
		require.NotNil(t, dc.Attributes, "string attributes must be projected")
		assert.Equal(t, "01LOC", dc.Attributes["location"], "location attribute projected")
		_, hasBool := dc.Attributes["has_location"]
		assert.False(t, hasBool, "non-string attribute (bool) must be dropped")
		_, hasSlice := dc.Attributes["roles"]
		assert.False(t, hasSlice, "non-string attribute (slice) must be dropped")
	})

	t.Run("resolver error is fail-closed with nil attributes", func(t *testing.T) {
		h := newDispatchHost(t, WithDispatchAttributeResolver(fakeAttrResolver{
			err: assert.AnError,
		}))
		got := h.stampDispatch(context.Background(), actor)
		dc, ok := pluginauthz.DispatchForHost(got)
		require.True(t, ok, "subject is still stamped on resolver error")
		assert.Equal(t, access.CharacterSubject(charID), dc.Subject, "host-vouched subject")
		assert.Nil(t, dc.Attributes, "resolver error leaves Attributes nil (fail-closed)")
	})

	t.Run("nil resolver leaves attributes nil", func(t *testing.T) {
		h := newDispatchHost(t)
		got := h.stampDispatch(context.Background(), actor)
		dc, ok := pluginauthz.DispatchForHost(got)
		require.True(t, ok, "character actor must stamp a dispatch subject")
		assert.Nil(t, dc.Attributes, "no resolver wired ⇒ Attributes nil (current behavior)")
	})

	t.Run("resolver returning only non-string attributes yields nil", func(t *testing.T) {
		h := newDispatchHost(t, WithDispatchAttributeResolver(fakeAttrResolver{
			attrs: map[string]any{"has_location": false},
		}))
		got := h.stampDispatch(context.Background(), actor)
		dc, ok := pluginauthz.DispatchForHost(got)
		require.True(t, ok, "character actor must stamp a dispatch subject")
		assert.Nil(t, dc.Attributes, "no string-valued attributes ⇒ nil, not empty map")
	})
}
