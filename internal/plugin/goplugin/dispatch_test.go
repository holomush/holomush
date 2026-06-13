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

func TestStampDispatchLeavesNonCharacterActorUnchanged(t *testing.T) {
	base := context.Background()

	// Plugin actor: not vouched as a character subject.
	got := stampDispatch(base, core.Actor{Kind: core.ActorPlugin, ID: core.NewULID().String()})
	_, ok := pluginauthz.DispatchForHost(got)
	assert.False(t, ok, "plugin actor must not stamp a dispatch subject")

	// Character actor with empty ID: fail-closed, no stamp.
	got = stampDispatch(base, core.Actor{Kind: core.ActorCharacter, ID: ""})
	_, ok = pluginauthz.DispatchForHost(got)
	assert.False(t, ok, "empty-ID character actor must not stamp a dispatch subject")
}
