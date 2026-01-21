// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hashiplug "github.com/hashicorp/go-plugin"
	"github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/capability"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	pluginpkg "github.com/holomush/holomush/pkg/plugin"
	"google.golang.org/grpc"
)

// createTempExecutable creates a dummy file with execute permissions.
func createTempExecutable(path string) error {
	//nolint:wrapcheck,gosec // test helper, no wrap; G306 - needs execute permission for testing
	return os.WriteFile(path, []byte("dummy"), 0o755)
}

// mockClientProtocol implements hashiplug.ClientProtocol for testing.
type mockClientProtocol struct {
	pluginClient pluginv1.PluginClient
	dispenseErr  error
	rawDispense  interface{} // If set, return this instead of pluginClient
}

func (m *mockClientProtocol) Close() error { return nil }
func (m *mockClientProtocol) Dispense(_ string) (interface{}, error) {
	if m.dispenseErr != nil {
		return nil, m.dispenseErr
	}
	if m.rawDispense != nil {
		return m.rawDispense, nil
	}
	return m.pluginClient, nil
}
func (m *mockClientProtocol) Ping() error { return nil }

// mockPluginClient implements PluginClient for testing.
type mockPluginClient struct {
	protocol  *mockClientProtocol
	killed    bool
	clientErr error
}

func (m *mockPluginClient) Client() (hashiplug.ClientProtocol, error) {
	if m.clientErr != nil {
		return nil, m.clientErr
	}
	return m.protocol, nil
}

func (m *mockPluginClient) Kill() {
	m.killed = true
}

// mockGRPCPluginClient implements pluginv1.PluginClient for testing.
type mockGRPCPluginClient struct {
	response  *pluginv1.HandleEventResponse
	err       error
	returnNil bool // If true, return nil response (simulates edge case)
}

func (m *mockGRPCPluginClient) HandleEvent(_ context.Context, _ *pluginv1.HandleEventRequest, _ ...grpc.CallOption) (*pluginv1.HandleEventResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.returnNil {
		return nil, nil
	}
	if m.response != nil {
		return m.response, nil
	}
	return &pluginv1.HandleEventResponse{}, nil
}

// mockClientFactory creates mock clients for testing.
type mockClientFactory struct {
	client *mockPluginClient
}

func (f *mockClientFactory) NewClient(_ string) PluginClient {
	return f.client
}

// newMockHost creates a host with mock client for testing.
func newMockHost(t *testing.T) (*Host, *mockPluginClient) {
	t.Helper()
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	enforcer := capability.NewEnforcer()
	host := NewHostWithFactory(enforcer, factory)
	return host, mockClient
}

func TestNewHost(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)
	require.NotNil(t, host, "NewHost returned nil")
}

func TestNewHost_NilEnforcer(t *testing.T) {
	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic when enforcer is nil")
	}()
	NewHost(nil)
}

func TestNewHostWithFactory_NilEnforcer(t *testing.T) {
	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic when enforcer is nil")
	}()
	NewHostWithFactory(nil, &DefaultClientFactory{})
}

func TestNewHostWithFactory_NilFactory(t *testing.T) {
	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic when factory is nil")
	}()
	enforcer := capability.NewEnforcer()
	NewHostWithFactory(enforcer, nil)
}

func TestPlugins_Empty(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	plugins := host.Plugins()
	assert.Empty(t, plugins, "expected empty plugins list")
}

func TestPlugins_AfterClose(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	err := host.Close(context.Background())
	require.NoError(t, err, "Close returned error")

	plugins := host.Plugins()
	assert.Nil(t, plugins, "expected nil plugins after close")
}

func TestClose_NoPlugins(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	err := host.Close(context.Background())
	assert.NoError(t, err, "Close returned error")
}

func TestClose_PreventsFurtherLoads(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	err := host.Close(context.Background())
	require.NoError(t, err, "Close returned error")

	tmpDir := t.TempDir()
	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}
	err = host.Load(context.Background(), manifest, tmpDir)
	require.Error(t, err, "expected error when loading after close")
	assert.ErrorIs(t, err, ErrHostClosed, "expected ErrHostClosed")
}

func TestClose_Idempotent(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	// First close should succeed
	err1 := host.Close(context.Background())
	require.NoError(t, err1, "first Close returned error")

	// Second close should also succeed (idempotent)
	err2 := host.Close(context.Background())
	assert.NoError(t, err2, "second Close returned error")
}

func TestLoad_ContextCancelled(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tmpDir := t.TempDir()
	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err := host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when loading with cancelled context")
	assert.ErrorIs(t, err, context.Canceled, "expected context.Canceled")
	assert.Contains(t, err.Error(), "load cancelled", "expected error to mention 'load cancelled'")
}

func TestUnload_NotLoaded(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	err := host.Unload(context.Background(), "nonexistent")
	require.Error(t, err, "expected error when unloading nonexistent plugin")
	assert.ErrorIs(t, err, ErrPluginNotLoaded, "expected ErrPluginNotLoaded")
}

func TestUnload_AfterClose(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	err := host.Close(context.Background())
	require.NoError(t, err, "Close returned error")

	err = host.Unload(context.Background(), "any-plugin")
	require.Error(t, err, "expected error when unloading after close")
	assert.ErrorIs(t, err, ErrHostClosed, "expected ErrHostClosed")
}

func TestDeliverEvent_NotLoaded(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	_, err := host.DeliverEvent(context.Background(), "nonexistent", pluginpkg.Event{})
	require.Error(t, err, "expected error when delivering to nonexistent plugin")
	assert.ErrorIs(t, err, ErrPluginNotLoaded, "expected ErrPluginNotLoaded")
}

func TestDeliverEvent_HostClosed(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	err := host.Close(context.Background())
	require.NoError(t, err, "Close returned error")

	_, err = host.DeliverEvent(context.Background(), "any-plugin", pluginpkg.Event{})
	require.Error(t, err, "expected error when delivering after close")
	assert.ErrorIs(t, err, ErrHostClosed, "expected ErrHostClosed")
}

func TestDeliverEvent_HandleEventError(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{
		err: errors.New("plugin crashed"),
	}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	enforcer := capability.NewEnforcer()
	host := NewHostWithFactory(enforcer, factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err, "Load returned error")

	_, err = host.DeliverEvent(ctx, "test-plugin", pluginpkg.Event{})
	require.Error(t, err, "expected error when HandleEvent fails")
	assert.Contains(t, err.Error(), "HandleEvent failed", "expected error to mention 'HandleEvent failed'")
}

func TestDeliverEvent_NilResponse(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{
		returnNil: true, // Simulates nil response without error (edge case)
	}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	enforcer := capability.NewEnforcer()
	host := NewHostWithFactory(enforcer, factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err, "Load returned error")

	// DeliverEvent should handle nil response gracefully (proto getters are nil-safe)
	emits, err := host.DeliverEvent(ctx, "test-plugin", pluginpkg.Event{})
	assert.NoError(t, err, "unexpected error with nil response")
	assert.Empty(t, emits, "expected empty emits for nil response")
}

func TestDeliverEvent_Timeout(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{
		err: context.DeadlineExceeded, // Simulates timeout
	}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	enforcer := capability.NewEnforcer()
	host := NewHostWithFactory(enforcer, factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err, "Load returned error")

	_, err = host.DeliverEvent(ctx, "test-plugin", pluginpkg.Event{})
	require.Error(t, err, "expected error on timeout")
	assert.ErrorIs(t, err, context.DeadlineExceeded, "expected context.DeadlineExceeded")
}

func TestLoad_ClientError(t *testing.T) {
	mockClient := &mockPluginClient{
		clientErr: errors.New("connection failed"),
	}
	factory := &mockClientFactory{client: mockClient}
	enforcer := capability.NewEnforcer()
	host := NewHostWithFactory(enforcer, factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when client connection fails")
	assert.Contains(t, err.Error(), "failed to connect", "expected error to mention 'failed to connect'")
	assert.True(t, mockClient.killed, "expected client to be killed after connection failure")
}

func TestLoad_DispenseError(t *testing.T) {
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{
			dispenseErr: errors.New("dispense failed"),
		},
	}
	factory := &mockClientFactory{client: mockClient}
	enforcer := capability.NewEnforcer()
	host := NewHostWithFactory(enforcer, factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when dispense fails")
	assert.Contains(t, err.Error(), "failed to dispense", "expected error to mention 'failed to dispense'")
	assert.True(t, mockClient.killed, "expected client to be killed after dispense failure")
}

func TestLoad_Unload_Plugins_Cycle(t *testing.T) {
	host, mockClient := newMockHost(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test-plugin"
	err := createTempExecutable(tmpFile)
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err, "Load returned error")

	plugins := host.Plugins()
	require.Len(t, plugins, 1, "expected 1 plugin")
	assert.Equal(t, "test-plugin", plugins[0], "expected plugin name 'test-plugin'")

	err = host.Unload(ctx, "test-plugin")
	assert.NoError(t, err, "Unload returned error")

	plugins = host.Plugins()
	assert.Empty(t, plugins, "expected 0 plugins after unload")

	assert.True(t, mockClient.killed, "expected mock client to be killed on unload")
}

func TestLoad_DuplicateName(t *testing.T) {
	host, _ := newMockHost(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test-plugin"
	err := createTempExecutable(tmpFile)
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err, "first Load returned error")

	err = host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when loading duplicate plugin name")
	assert.ErrorIs(t, err, ErrPluginAlreadyLoaded, "expected ErrPluginAlreadyLoaded")
}

func TestLoad_ExecutableNotFound(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)
	ctx := context.Background()

	tmpDir := t.TempDir()
	manifest := &plugin.Manifest{
		Name:    "nonexistent",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "this-executable-does-not-exist-12345",
		},
	}

	err := host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when loading nonexistent executable")
	assert.Contains(t, err.Error(), "not found", "expected error to mention 'not found'")
	// Verify error is wrapped (contains underlying os error)
	assert.ErrorIs(t, err, os.ErrNotExist, "expected error to wrap os.ErrNotExist")
}

func TestLoad_SetGrantsFailure(t *testing.T) {
	// Create mock plugin client that succeeds
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	enforcer := capability.NewEnforcer()
	host := NewHostWithFactory(enforcer, factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	// Create manifest with invalid capability pattern (empty string)
	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
		Capabilities: []string{"valid.capability", ""}, // Empty pattern will cause SetGrants to fail
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when SetGrants fails")
	assert.Contains(t, err.Error(), "failed to set capabilities", "expected error to mention 'failed to set capabilities'")

	// Verify plugin was not added to the host
	assert.Empty(t, host.Plugins(), "plugin should not be loaded after SetGrants failure")

	// Verify client was killed (cleanup on error)
	assert.True(t, mockClient.killed, "client should be killed on SetGrants failure")
}

func TestLoad_ExecutableStatError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping test when running as root (permissions ignored)")
	}

	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)
	ctx := context.Background()

	// Create a directory structure where the parent directory has no read permission
	tmpDir := t.TempDir()
	restrictedDir := tmpDir + "/restricted"
	//nolint:gosec // G301 - needs execute permission to enter directory initially
	err := os.Mkdir(restrictedDir, 0o755)
	require.NoError(t, err, "failed to create restricted dir")

	execPath := restrictedDir + "/plugin"
	//nolint:gosec // G306 - needs execute permission for valid plugin executable
	err = os.WriteFile(execPath, []byte("dummy"), 0o755)
	require.NoError(t, err, "failed to create executable")

	// Remove all permissions from the directory - this will cause os.Stat to fail
	// with permission denied, NOT file not found
	err = os.Chmod(restrictedDir, 0o000)
	require.NoError(t, err, "failed to chmod directory")
	// Restore permissions on cleanup so t.TempDir() can clean up
	t.Cleanup(func() {
		//nolint:gosec // G302 - restore permissions for cleanup
		_ = os.Chmod(restrictedDir, 0o755)
	})

	manifest := &plugin.Manifest{
		Name:    "permission-denied",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "plugin",
		},
	}

	err = host.Load(ctx, manifest, restrictedDir)
	require.Error(t, err, "expected error when stat fails with permission denied")
	// Should get a resolution or access error, not "not found"
	assert.True(t, strings.Contains(err.Error(), "cannot resolve") || strings.Contains(err.Error(), "cannot access"),
		"expected error to mention 'cannot resolve' or 'cannot access', got: %v", err)
	// Verify it's NOT the "not found" error
	assert.NotContains(t, err.Error(), "not found", "expected resolution/access error, not 'not found'")
}

func TestLoad_ExecutableNotExecutable(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)
	ctx := context.Background()

	tmpDir := t.TempDir()
	execPath := tmpDir + "/non-executable-plugin"
	// Create file without execute permission (0o600 = rw-------)
	err := os.WriteFile(execPath, []byte("not executable"), 0o600)
	require.NoError(t, err, "failed to create test file")

	manifest := &plugin.Manifest{
		Name:    "non-executable",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "non-executable-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when loading non-executable file")
	assert.Contains(t, err.Error(), "not executable", "expected error to mention 'not executable'")
}

func TestLoad_ExecutablePathTraversal(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)
	ctx := context.Background()

	tmpDir := t.TempDir()

	// Create executable in parent directory (outside plugin dir)
	parentExec := filepath.Dir(tmpDir) + "/escaped-plugin"
	err := createTempExecutable(parentExec)
	require.NoError(t, err, "failed to create escaped executable")
	t.Cleanup(func() { _ = os.Remove(parentExec) })

	// Try to load plugin with path traversal in executable path
	manifest := &plugin.Manifest{
		Name:    "malicious",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "../escaped-plugin", // Path traversal attempt
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when executable path escapes plugin directory")
	assert.Contains(t, err.Error(), "escapes plugin directory", "expected error to mention 'escapes plugin directory'")
}

func TestLoad_ExecutableSymlinkEscape(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping test when running as root")
	}

	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)
	ctx := context.Background()

	// Create a temp directory structure
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "plugin")
	//nolint:gosec // G301 - needs execute permission to enter directory
	err := os.Mkdir(pluginDir, 0o755)
	require.NoError(t, err, "failed to create plugin dir")

	// Create an executable outside the plugin directory
	outsideExec := filepath.Join(tmpDir, "outside-exec")
	err = createTempExecutable(outsideExec)
	require.NoError(t, err, "failed to create outside executable")

	// Create a symlink inside the plugin directory pointing outside
	symlinkPath := filepath.Join(pluginDir, "evil-link")
	err = os.Symlink(outsideExec, symlinkPath)
	require.NoError(t, err, "failed to create symlink")

	manifest := &plugin.Manifest{
		Name:    "symlink-escape",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "evil-link", // Symlink that points outside
		},
	}

	err = host.Load(ctx, manifest, pluginDir)
	require.Error(t, err, "expected error when executable symlink escapes plugin directory")
	assert.Contains(t, err.Error(), "escapes plugin directory", "expected error to mention 'escapes plugin directory'")
}

func TestDeliverEvent_Success(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{
		response: &pluginv1.HandleEventResponse{
			EmitEvents: []*pluginv1.EmitEvent{
				{Stream: "room:123", Type: "say", Payload: `{"text":"hello"}`},
			},
		},
	}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	enforcer := capability.NewEnforcer()
	host := NewHostWithFactory(enforcer, factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err, "Load returned error")

	event := pluginpkg.Event{
		ID:        "evt-123",
		Stream:    "room:456",
		Type:      pluginpkg.EventTypeSay,
		Timestamp: 1234567890,
		ActorKind: pluginpkg.ActorCharacter,
		ActorID:   "char-789",
		Payload:   `{"text":"hello world"}`,
	}

	emits, err := host.DeliverEvent(ctx, "test-plugin", event)
	require.NoError(t, err, "DeliverEvent returned error")
	require.Len(t, emits, 1, "expected 1 emit event")
	assert.Equal(t, "room:123", emits[0].Stream, "expected stream 'room:123'")
	assert.Equal(t, pluginpkg.EventTypeSay, emits[0].Type, "expected type 'say'")
}

func TestClose_KillsPlugins(t *testing.T) {
	host, mockClient := newMockHost(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err, "Load returned error")

	err = host.Close(ctx)
	assert.NoError(t, err, "Close returned error")

	assert.True(t, mockClient.killed, "expected mock client to be killed on close")
}

func TestDeliverEvent_ActorKinds(t *testing.T) {
	tests := []struct {
		name      string
		actorKind pluginpkg.ActorKind
	}{
		{"character", pluginpkg.ActorCharacter},
		{"system", pluginpkg.ActorSystem},
		{"plugin", pluginpkg.ActorPlugin},
		{"unknown", pluginpkg.ActorKind(99)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grpcClient := &mockGRPCPluginClient{}
			mockClient := &mockPluginClient{
				protocol: &mockClientProtocol{pluginClient: grpcClient},
			}
			factory := &mockClientFactory{client: mockClient}
			enforcer := capability.NewEnforcer()
			host := NewHostWithFactory(enforcer, factory)

			ctx := context.Background()
			tmpDir := t.TempDir()
			err := createTempExecutable(tmpDir + "/test-plugin")
			require.NoError(t, err, "failed to create temp file")

			manifest := &plugin.Manifest{
				Name:    "test-plugin",
				Version: "1.0.0",
				Type:    plugin.TypeBinary,
				BinaryPlugin: &plugin.BinaryConfig{
					Executable: "test-plugin",
				},
			}

			err = host.Load(ctx, manifest, tmpDir)
			require.NoError(t, err, "Load returned error")

			event := pluginpkg.Event{
				ID:        "evt-123",
				ActorKind: tt.actorKind,
			}

			_, err = host.DeliverEvent(ctx, "test-plugin", event)
			assert.NoError(t, err, "DeliverEvent returned error")
		})
	}
}

func TestLoad_NilBinaryPlugin(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)
	ctx := context.Background()

	tmpDir := t.TempDir()
	manifest := &plugin.Manifest{
		Name:         "wasm-plugin",
		Version:      "1.0.0",
		Type:         "wasm",  // Wrong type for goplugin host
		BinaryPlugin: nil,     // No BinaryPlugin config
	}

	err := host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when BinaryPlugin is nil")
	assert.Contains(t, err.Error(), "not a binary plugin", "expected error to mention 'not a binary plugin'")
}

func TestLoad_NilManifest(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)
	ctx := context.Background()
	tmpDir := t.TempDir()

	err := host.Load(ctx, nil, tmpDir)
	require.Error(t, err, "expected error when manifest is nil")
	assert.Contains(t, err.Error(), "manifest cannot be nil", "expected error to mention 'manifest cannot be nil'")
}

func TestLoad_EmptyPluginName(t *testing.T) {
	host, _ := newMockHost(t)
	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(filepath.Join(tmpDir, "test-plugin"))
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugin.Manifest{
		Name:    "", // Empty name
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error for empty plugin name")
	assert.Contains(t, err.Error(), "plugin name cannot be empty", "expected error to mention 'plugin name cannot be empty'")
}

func TestLoad_InvalidPluginClient(t *testing.T) {
	// Return a non-PluginClient from Dispense to trigger type assertion failure
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{
			rawDispense: "not a PluginClient", // Return wrong type
		},
	}
	factory := &mockClientFactory{client: mockClient}
	enforcer := capability.NewEnforcer()
	host := NewHostWithFactory(enforcer, factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when plugin does not implement PluginClient")
	assert.Contains(t, err.Error(), "does not implement PluginClient", "expected error to mention 'does not implement PluginClient'")
	assert.True(t, mockClient.killed, "expected client to be killed after type assertion failure")
}
