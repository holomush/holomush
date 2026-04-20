// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hashiplug "github.com/hashicorp/go-plugin"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	plugins "github.com/holomush/holomush/internal/plugin"
	tlscerts "github.com/holomush/holomush/internal/tls"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

// drainEventbusStream returns every stored message on EVENTS by walking
// sequences via GetMsg. Stateless RPC — no consumer goroutines to race.
func drainEventbusStream(t *testing.T, js jetstream.JetStream) []*jetstream.RawStreamMsg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := js.Stream(ctx, eventbus.StreamName)
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	var out []*jetstream.RawStreamMsg
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq && seq != 0; seq++ {
		msg, gerr := stream.GetMsg(ctx, seq)
		require.NoError(t, gerr)
		out = append(out, msg)
	}
	return out
}

// createTempExecutable creates a dummy file with execute permissions.
func createTempExecutable(path string) error {
	//nolint:gosec // G306 - needs execute permission for testing
	return os.WriteFile(path, []byte("dummy"), 0o755)
}

// mockClientProtocol implements hashiplug.ClientProtocol for testing.
// It also implements the Conn() accessor so goplugin.Host.Load can capture
// the gRPC connection for service registration.
type mockClientProtocol struct {
	pluginClient pluginv1.PluginServiceClient
	dispenseErr  error
	rawDispense  interface{} // If set, return this instead of pluginClient
	conn         grpc.ClientConnInterface
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
func (m *mockClientProtocol) Ping() error                    { return nil }
func (m *mockClientProtocol) Conn() grpc.ClientConnInterface { return m.conn }

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

// mockGRPCPluginClient implements pluginv1.PluginServiceClient for testing.
type mockGRPCPluginClient struct {
	response  *pluginv1.HandleEventResponse
	err       error
	returnNil bool // If true, return nil response (simulates edge case)
	eventCtx  context.Context

	cmdResponse  *pluginv1.HandleCommandResponse
	cmdErr       error
	cmdReturnNil bool
	commandCtx   context.Context

	// Init tracking
	initCalled bool
	initReq    *pluginv1.InitRequest
	initErr    error

	QuerySessionStreamsFunc func(ctx context.Context, req *pluginv1.QuerySessionStreamsRequest) (*pluginv1.QuerySessionStreamsResponse, error)
}

func (m *mockGRPCPluginClient) Init(_ context.Context, req *pluginv1.InitRequest, _ ...grpc.CallOption) (*pluginv1.InitResponse, error) {
	m.initCalled = true
	m.initReq = req
	if m.initErr != nil {
		return nil, m.initErr
	}
	return &pluginv1.InitResponse{}, nil
}

func (m *mockGRPCPluginClient) HandleEvent(ctx context.Context, _ *pluginv1.HandleEventRequest, _ ...grpc.CallOption) (*pluginv1.HandleEventResponse, error) {
	m.eventCtx = ctx
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

func (m *mockGRPCPluginClient) HandleCommand(ctx context.Context, _ *pluginv1.HandleCommandRequest, _ ...grpc.CallOption) (*pluginv1.HandleCommandResponse, error) {
	m.commandCtx = ctx
	if m.cmdErr != nil {
		return nil, m.cmdErr
	}
	if m.cmdReturnNil {
		return nil, nil
	}
	if m.cmdResponse != nil {
		return m.cmdResponse, nil
	}
	return &pluginv1.HandleCommandResponse{Response: &pluginv1.CommandResponse{}}, nil
}

func (m *mockGRPCPluginClient) QuerySessionStreams(ctx context.Context, req *pluginv1.QuerySessionStreamsRequest, _ ...grpc.CallOption) (*pluginv1.QuerySessionStreamsResponse, error) {
	if m.QuerySessionStreamsFunc != nil {
		return m.QuerySessionStreamsFunc(ctx, req)
	}
	return &pluginv1.QuerySessionStreamsResponse{}, nil
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
	host := NewHostWithFactory(factory)
	return host, mockClient
}

func TestNewHost(t *testing.T) {
	host := NewHost()
	require.NotNil(t, host, "NewHost returned nil")
}

// captureLogs swaps slog.Default for the duration of the test and returns a
// buffer capturing all emitted records. The previous default is restored via
// t.Cleanup.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	orig := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(orig) })
	return &buf
}

func TestNewHostWithoutCALogsMTLSDisabledWarning(t *testing.T) {
	buf := captureLogs(t)

	host := NewHost()
	require.NotNil(t, host)

	out := buf.String()
	assert.Contains(t, out, "level=WARN", "expected a WARN-level log record")
	assert.Contains(t, out, "binary plugin mTLS disabled",
		"expected mTLS-disabled warning when host is constructed without a CA")
	assert.Contains(t, out, "mtls=disabled",
		"expected structured mtls=disabled attribute on warning")
}

func TestNewHostWithCADoesNotLogMTLSDisabledWarning(t *testing.T) {
	buf := captureLogs(t)

	ca, err := tlscerts.GenerateCA("test-game")
	require.NoError(t, err, "failed to generate test CA")

	host := NewHostWithFactory(&mockClientFactory{}, WithCA(ca, "test-game"))
	require.NotNil(t, host)

	out := buf.String()
	assert.NotContains(t, out, "binary plugin mTLS disabled",
		"mTLS-disabled warning must not fire when a CA is configured")
}

func TestNewHostWithFactoryNilFactory(t *testing.T) {
	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic when factory is nil")
	}()
	NewHostWithFactory(nil)
}

func TestPluginsEmpty(t *testing.T) {
	host := NewHost()

	plugins := host.Plugins()
	assert.Empty(t, plugins, "expected empty plugins list")
}

func TestPluginsAfterClose(t *testing.T) {
	host := NewHost()

	err := host.Close(context.Background())
	require.NoError(t, err, "Close returned error")

	plugins := host.Plugins()
	assert.Nil(t, plugins, "expected nil plugins after close")
}

func TestCloseNoPlugins(t *testing.T) {
	host := NewHost()

	err := host.Close(context.Background())
	assert.NoError(t, err, "Close returned error")
}

func TestClosePreventsFurtherLoads(t *testing.T) {
	host := NewHost()

	err := host.Close(context.Background())
	require.NoError(t, err, "Close returned error")

	tmpDir := t.TempDir()
	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}
	err = host.Load(context.Background(), manifest, tmpDir)
	require.Error(t, err, "expected error when loading after close")
	assert.ErrorIs(t, err, ErrHostClosed, "expected ErrHostClosed")
}

func TestCloseIdempotent(t *testing.T) {
	host := NewHost()

	// First close should succeed
	err1 := host.Close(context.Background())
	require.NoError(t, err1, "first Close returned error")

	// Second close should also succeed (idempotent)
	err2 := host.Close(context.Background())
	assert.NoError(t, err2, "second Close returned error")
}

func TestLoadContextCancelled(t *testing.T) {
	host := NewHost()

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tmpDir := t.TempDir()
	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err := host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when loading with cancelled context")
	assert.ErrorIs(t, err, context.Canceled, "expected context.Canceled")
	// oops.Error() returns underlying error message
	assert.Contains(t, err.Error(), "context canceled", "expected error to contain 'context canceled'")
}

func TestUnloadNotLoaded(t *testing.T) {
	host := NewHost()

	err := host.Unload(context.Background(), "nonexistent")
	require.Error(t, err, "expected error when unloading nonexistent plugin")
	assert.ErrorIs(t, err, ErrPluginNotLoaded, "expected ErrPluginNotLoaded")
}

func TestUnloadAfterClose(t *testing.T) {
	host := NewHost()

	err := host.Close(context.Background())
	require.NoError(t, err, "Close returned error")

	err = host.Unload(context.Background(), "any-plugin")
	require.Error(t, err, "expected error when unloading after close")
	assert.ErrorIs(t, err, ErrHostClosed, "expected ErrHostClosed")
}

func TestDeliverEventNotLoaded(t *testing.T) {
	host := NewHost()

	_, err := host.DeliverEvent(context.Background(), "nonexistent", pluginsdk.Event{})
	require.Error(t, err, "expected error when delivering to nonexistent plugin")
	assert.ErrorIs(t, err, ErrPluginNotLoaded, "expected ErrPluginNotLoaded")
}

func TestDeliverEventHostClosed(t *testing.T) {
	host := NewHost()

	err := host.Close(context.Background())
	require.NoError(t, err, "Close returned error")

	_, err = host.DeliverEvent(context.Background(), "any-plugin", pluginsdk.Event{})
	require.Error(t, err, "expected error when delivering after close")
	assert.ErrorIs(t, err, ErrHostClosed, "expected ErrHostClosed")
}

func TestDeliverEventHandleEventError(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{
		err: errors.New("plugin crashed"),
	}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err, "Load returned error")

	_, err = host.DeliverEvent(ctx, "test-plugin", pluginsdk.Event{})
	require.Error(t, err, "expected error when HandleEvent fails")
	// oops.Error() returns underlying error message from mock
	assert.Contains(t, err.Error(), "plugin crashed", "expected error to contain mock error message")
}

func TestDeliverEventNilResponse(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{
		returnNil: true, // Simulates nil response without error (edge case)
	}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err, "Load returned error")

	// DeliverEvent should handle nil response gracefully (proto getters are nil-safe)
	emits, err := host.DeliverEvent(ctx, "test-plugin", pluginsdk.Event{})
	assert.NoError(t, err, "unexpected error with nil response")
	assert.Empty(t, emits, "expected empty emits for nil response")
}

func TestDeliverEventTimeout(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{
		err: context.DeadlineExceeded, // Simulates timeout
	}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err, "Load returned error")

	_, err = host.DeliverEvent(ctx, "test-plugin", pluginsdk.Event{})
	require.Error(t, err, "expected error on timeout")
	assert.ErrorIs(t, err, context.DeadlineExceeded, "expected context.DeadlineExceeded")
}

func TestLoadClientError(t *testing.T) {
	mockClient := &mockPluginClient{
		clientErr: errors.New("connection failed"),
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when client connection fails")
	// oops.Error() returns underlying error message from mock
	assert.Contains(t, err.Error(), "connection failed", "expected error to contain mock error message")
	assert.True(t, mockClient.killed, "expected client to be killed after connection failure")
}

func TestLoadDispenseError(t *testing.T) {
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{
			dispenseErr: errors.New("dispense failed"),
		},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when dispense fails")
	// oops.Error() returns underlying error message from mock
	assert.Contains(t, err.Error(), "dispense failed", "expected error to contain mock error message")
	assert.True(t, mockClient.killed, "expected client to be killed after dispense failure")
}

func TestLoadUnloadPluginsCycle(t *testing.T) {
	host, mockClient := newMockHost(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test-plugin"
	err := createTempExecutable(tmpFile)
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
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

func TestLoadDuplicateName(t *testing.T) {
	host, _ := newMockHost(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test-plugin"
	err := createTempExecutable(tmpFile)
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err, "first Load returned error")

	err = host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when loading duplicate plugin name")
	assert.ErrorIs(t, err, ErrPluginAlreadyLoaded, "expected ErrPluginAlreadyLoaded")
}

func TestLoadExecutableNotFound(t *testing.T) {
	host := NewHost()
	ctx := context.Background()

	tmpDir := t.TempDir()
	manifest := &plugins.Manifest{
		Name:    "nonexistent",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "this-executable-does-not-exist-12345",
		},
	}

	err := host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when loading nonexistent executable")
	// oops.Error() returns underlying error message which contains file path
	assert.Contains(t, err.Error(), "no such file or directory", "expected error to contain OS error message")
	// Verify error is wrapped (contains underlying os error)
	assert.ErrorIs(t, err, os.ErrNotExist, "expected error to wrap os.ErrNotExist")
}

func TestLoadExecutableStatError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping test when running as root (permissions ignored)")
	}

	host := NewHost()
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

	manifest := &plugins.Manifest{
		Name:    "permission-denied",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "plugin",
		},
	}

	err = host.Load(ctx, manifest, restrictedDir)
	require.Error(t, err, "expected error when stat fails with permission denied")
	// oops.Error() returns underlying OS error which is "permission denied"
	assert.Contains(t, err.Error(), "permission denied",
		"expected error to contain 'permission denied', got: %v", err)
	// Verify it's NOT the "not found" error
	assert.NotContains(t, err.Error(), "not found", "expected permission error, not 'not found'")
}

func TestLoadExecutableNotExecutable(t *testing.T) {
	host := NewHost()
	ctx := context.Background()

	tmpDir := t.TempDir()
	execPath := tmpDir + "/non-executable-plugin"
	// Create file without execute permission (0o600 = rw-------)
	err := os.WriteFile(execPath, []byte("not executable"), 0o600)
	require.NoError(t, err, "failed to create test file")

	manifest := &plugins.Manifest{
		Name:    "non-executable",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "non-executable-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when loading non-executable file")
	assert.Contains(t, err.Error(), "not executable", "expected error to mention 'not executable'")
}

func TestLoadExecutablePathTraversal(t *testing.T) {
	host := NewHost()
	ctx := context.Background()

	tmpDir := t.TempDir()

	// Create executable in parent directory (outside plugin dir)
	parentExec := filepath.Dir(tmpDir) + "/escaped-plugin"
	err := createTempExecutable(parentExec)
	require.NoError(t, err, "failed to create escaped executable")
	t.Cleanup(func() { _ = os.Remove(parentExec) })

	// Try to load plugin with path traversal in executable path
	manifest := &plugins.Manifest{
		Name:    "malicious",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "../escaped-plugin", // Path traversal attempt
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when executable path escapes plugin directory")
	assert.Contains(t, err.Error(), "escapes plugin directory", "expected error to mention 'escapes plugin directory'")
}

func TestLoadExecutableSymlinkEscape(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping test when running as root")
	}

	host := NewHost()
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

	manifest := &plugins.Manifest{
		Name:    "symlink-escape",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "evil-link", // Symlink that points outside
		},
	}

	err = host.Load(ctx, manifest, pluginDir)
	require.Error(t, err, "expected error when executable symlink escapes plugin directory")
	assert.Contains(t, err.Error(), "escapes plugin directory", "expected error to mention 'escapes plugin directory'")
}

func TestDeliverEventSuccess(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{
		response: &pluginv1.HandleEventResponse{
			EmitEvents: []*pluginv1.EmitEvent{
				{Stream: "location:123", Type: "say", Payload: `{"text":"hello"}`},
			},
		},
	}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err, "Load returned error")

	event := pluginsdk.Event{
		ID:        "evt-123",
		Stream:    "location:456",
		Type:      pluginsdk.EventTypeSay,
		Timestamp: 1234567890,
		ActorKind: pluginsdk.ActorCharacter,
		ActorID:   "char-789",
		Payload:   `{"text":"hello world"}`,
	}

	emits, err := host.DeliverEvent(ctx, "test-plugin", event)
	require.NoError(t, err, "DeliverEvent returned error")
	require.Len(t, emits, 1, "expected 1 emit event")
	assert.Equal(t, "location:123", emits[0].Stream, "expected stream 'location:123'")
	assert.Equal(t, pluginsdk.EventTypeSay, emits[0].Type, "expected type 'say'")
}

func TestCloseKillsPlugins(t *testing.T) {
	host, mockClient := newMockHost(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
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
		actorKind pluginsdk.ActorKind
	}{
		{"character", pluginsdk.ActorCharacter},
		{"system", pluginsdk.ActorSystem},
		{"plugin", pluginsdk.ActorPlugin},
		{"unknown", pluginsdk.ActorKind(99)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grpcClient := &mockGRPCPluginClient{}
			mockClient := &mockPluginClient{
				protocol: &mockClientProtocol{pluginClient: grpcClient},
			}
			factory := &mockClientFactory{client: mockClient}
			host := NewHostWithFactory(factory)

			ctx := context.Background()
			tmpDir := t.TempDir()
			err := createTempExecutable(tmpDir + "/test-plugin")
			require.NoError(t, err, "failed to create temp file")

			manifest := &plugins.Manifest{
				Name:    "test-plugin",
				Version: "1.0.0",
				Type:    plugins.TypeBinary,
				BinaryPlugin: &plugins.BinaryConfig{
					Executable: "test-plugin",
				},
			}

			err = host.Load(ctx, manifest, tmpDir)
			require.NoError(t, err, "Load returned error")

			event := pluginsdk.Event{
				ID:        "evt-123",
				ActorKind: tt.actorKind,
			}

			_, err = host.DeliverEvent(ctx, "test-plugin", event)
			assert.NoError(t, err, "DeliverEvent returned error")
		})
	}
}

func TestLoadNilBinaryPlugin(t *testing.T) {
	host := NewHost()
	ctx := context.Background()

	tmpDir := t.TempDir()
	manifest := &plugins.Manifest{
		Name:         "wasm-plugin",
		Version:      "1.0.0",
		Type:         "wasm", // Wrong type for goplugin host
		BinaryPlugin: nil,    // No BinaryPlugin config
	}

	err := host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when BinaryPlugin is nil")
	assert.Contains(t, err.Error(), "not a binary plugin", "expected error to mention 'not a binary plugin'")
}

func TestLoadNilManifest(t *testing.T) {
	host := NewHost()
	ctx := context.Background()
	tmpDir := t.TempDir()

	err := host.Load(ctx, nil, tmpDir)
	require.Error(t, err, "expected error when manifest is nil")
	assert.Contains(t, err.Error(), "manifest cannot be nil", "expected error to mention 'manifest cannot be nil'")
}

func TestLoadEmptyPluginName(t *testing.T) {
	host, _ := newMockHost(t)
	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(filepath.Join(tmpDir, "test-plugin"))
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugins.Manifest{
		Name:    "", // Empty name
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error for empty plugin name")
	assert.Contains(t, err.Error(), "plugin name cannot be empty", "expected error to mention 'plugin name cannot be empty'")
}

func TestLoadInvalidPluginClient(t *testing.T) {
	// Return a non-PluginClient from Dispense to trigger type assertion failure
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{
			rawDispense: "not a PluginClient", // Return wrong type
		},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err, "failed to create temp file")

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when plugin does not implement PluginClient")
	assert.Contains(t, err.Error(), "does not implement PluginClient", "expected error to mention 'does not implement PluginClient'")
	assert.True(t, mockClient.killed, "expected client to be killed after type assertion failure")
}

// --- DeliverCommand tests ---

func TestDeliverCommandSuccess(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{
		cmdResponse: &pluginv1.HandleCommandResponse{
			Response: &pluginv1.CommandResponse{
				Status: pluginv1.CommandStatus_COMMAND_STATUS_OK,
				Output: "Hello, world!",
				Events: []*pluginv1.EmitEvent{
					{Stream: "location:123", Type: "say", Payload: `{"text":"hello"}`},
				},
			},
		},
	}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err)

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err)

	cmd := pluginsdk.CommandRequest{
		Command:       "say",
		Args:          "hello world",
		CharacterID:   "char-123",
		CharacterName: "Alice",
		LocationID:    "loc-456",
		SessionID:     "sess-789",
	}

	resp, err := host.DeliverCommand(ctx, "test-plugin", cmd)
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Equal(t, "Hello, world!", resp.Output)
	require.Len(t, resp.Events, 1)
	assert.Equal(t, "location:123", resp.Events[0].Stream)
	assert.Equal(t, pluginsdk.EventTypeSay, resp.Events[0].Type)
}

func TestDeliverCommandNotLoaded(t *testing.T) {
	host := NewHost()

	_, err := host.DeliverCommand(context.Background(), "nonexistent", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPluginNotLoaded)
}

func TestDeliverCommandHostClosed(t *testing.T) {
	host := NewHost()

	err := host.Close(context.Background())
	require.NoError(t, err)

	_, err = host.DeliverCommand(context.Background(), "any-plugin", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHostClosed)
}

func TestDeliverCommandHandleCommandError(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{
		cmdErr: errors.New("command handler crashed"),
	}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err)

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err)

	_, err = host.DeliverCommand(ctx, "test-plugin", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command handler crashed")
}

func TestDeliverCommandNilResponse(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{
		cmdReturnNil: true,
	}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err)

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err)

	resp, err := host.DeliverCommand(ctx, "test-plugin", pluginsdk.CommandRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Empty(t, resp.Output)
}

func TestDeliverCommand_StatusMapping(t *testing.T) {
	tests := []struct {
		name        string
		protoStatus pluginv1.CommandStatus
		sdkStatus   pluginsdk.CommandStatus
	}{
		{"OK", pluginv1.CommandStatus_COMMAND_STATUS_OK, pluginsdk.CommandOK},
		{"Error", pluginv1.CommandStatus_COMMAND_STATUS_ERROR, pluginsdk.CommandError},
		{"Failure", pluginv1.CommandStatus_COMMAND_STATUS_FAILURE, pluginsdk.CommandFailure},
		{"Fatal", pluginv1.CommandStatus_COMMAND_STATUS_FATAL, pluginsdk.CommandFatal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grpcClient := &mockGRPCPluginClient{
				cmdResponse: &pluginv1.HandleCommandResponse{
					Response: &pluginv1.CommandResponse{
						Status: tt.protoStatus,
						Output: "test output",
					},
				},
			}
			mockClient := &mockPluginClient{
				protocol: &mockClientProtocol{pluginClient: grpcClient},
			}
			factory := &mockClientFactory{client: mockClient}
			host := NewHostWithFactory(factory)

			ctx := context.Background()
			tmpDir := t.TempDir()
			err := createTempExecutable(tmpDir + "/test-plugin")
			require.NoError(t, err)

			manifest := &plugins.Manifest{
				Name:    "test-plugin",
				Version: "1.0.0",
				Type:    plugins.TypeBinary,
				BinaryPlugin: &plugins.BinaryConfig{
					Executable: "test-plugin",
				},
			}

			err = host.Load(ctx, manifest, tmpDir)
			require.NoError(t, err)

			resp, err := host.DeliverCommand(ctx, "test-plugin", pluginsdk.CommandRequest{})
			require.NoError(t, err)
			assert.Equal(t, tt.sdkStatus, resp.Status)
		})
	}
}

// --- Init / Service Injection tests ---

// mockSchemaProvisioner provides a test double for SchemaProvisioner.
// We cannot use *plugins.SchemaProvisioner directly because it needs a real
// Postgres connection. Instead, we test the Host logic by using a grpcClient
// that tracks Init calls and verifying the overall Load flow.
// For schema provisioning specifically, we verify behavior through integration tests.

func TestLoadCallsInitForPostgresStoragePlugin(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	// No schema provisioner — Init should still be called but without connection string
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/storage-plugin")
	require.NoError(t, err)

	manifest := &plugins.Manifest{
		Name:    "storage-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		Storage: plugins.StoragePostgres,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "storage-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err)

	assert.True(t, grpcClient.initCalled, "expected Init to be called for postgres storage plugin")
	require.NotNil(t, grpcClient.initReq, "expected InitRequest to be set")
	require.NotNil(t, grpcClient.initReq.Config, "expected ServiceConfig to be set")
	assert.Empty(t, grpcClient.initReq.Config.ConnectionString,
		"expected empty connection string when no schema provisioner configured")
}

func TestLoadCallsInitForPluginWithRequires(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/svc-plugin")
	require.NoError(t, err)

	manifest := &plugins.Manifest{
		Name:     "svc-plugin",
		Version:  "1.0.0",
		Type:     plugins.TypeBinary,
		Requires: []string{"event-store"},
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "svc-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err)

	assert.True(t, grpcClient.initCalled, "expected Init to be called for plugin with requires")
	require.NotNil(t, grpcClient.initReq, "expected InitRequest to be set")
	require.NotNil(t, grpcClient.initReq.Config, "expected ServiceConfig to be set")
	// RequiredServices is populated but empty in test path (no broker available
	// via mock factory). Production path with GRPCBroker populates broker IDs.
	assert.NotNil(t, grpcClient.initReq.Config.RequiredServices,
		"expected RequiredServices map to be set (empty in test path)")
}

func TestLoadCallsInitForPluginWithProvides(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(filepath.Join(tmpDir, "scene-provider"))
	require.NoError(t, err)

	manifest := &plugins.Manifest{
		Name:     "scene-provider",
		Version:  "1.0.0",
		Type:     plugins.TypeBinary,
		Provides: []string{"holomush.scene.v1.SceneService"},
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "scene-provider",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err)

	assert.True(t, grpcClient.initCalled, "expected Init to be called for plugin with provides")
	require.NotNil(t, grpcClient.initReq, "expected InitRequest to be set")
	require.NotNil(t, grpcClient.initReq.Config, "expected ServiceConfig to be set")
}

func TestLoadPassesRequiredServicesFromRegistryViaInit(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{}
	fakeConn := &stubClientConn{}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient, conn: fakeConn},
	}
	factory := &mockClientFactory{client: mockClient}

	// Set up a registry with a service the plugin requires.
	registry := plugins.NewServiceRegistry()
	require.NoError(t, registry.Register(plugins.RegisteredService{
		Name:       "holomush.scene.v1.SceneService",
		Conn:       fakeConn,
		PluginName: "scene-provider",
		PluginType: plugins.TypeBinary,
	}))

	host := NewHostWithFactory(factory, WithServiceRegistry(registry))

	ctx := context.Background()
	tmpDir := t.TempDir()
	require.NoError(t, createTempExecutable(filepath.Join(tmpDir, "needs-scene")))

	manifest := &plugins.Manifest{
		Name:     "needs-scene",
		Version:  "1.0.0",
		Type:     plugins.TypeBinary,
		Requires: []string{"holomush.scene.v1.SceneService"},
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "needs-scene",
		},
	}

	// In test path, broker is nil so broker proxies are not started and
	// RequiredServices remains empty. This verifies the graceful-degradation
	// path where no broker is available.
	err := host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err)

	assert.True(t, grpcClient.initCalled, "expected Init to be called")
	require.NotNil(t, grpcClient.initReq.Config, "expected ServiceConfig to be set")
	assert.Empty(t, grpcClient.initReq.Config.RequiredServices,
		"expected RequiredServices empty when broker is nil (test path)")
}

func TestHostDeliverCommandForwardsTrustedActorMetadata(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	host := NewHostWithFactory(&mockClientFactory{client: mockClient})
	host.plugins["core-scenes"] = &loadedPlugin{
		manifest: &plugins.Manifest{Name: "core-scenes"},
		plugin:   grpcClient,
	}

	ctx := core.WithActor(context.Background(), core.Actor{
		Kind: core.ActorCharacter,
		ID:   "char-alice",
	})
	_, err := host.DeliverCommand(ctx, "core-scenes", pluginsdk.CommandRequest{Command: "scene"})
	require.NoError(t, err)

	kind, id, ok := pluginsdk.ActorMetadataFromOutgoingContext(grpcClient.commandCtx)
	require.True(t, ok)
	assert.Equal(t, pluginsdk.ActorCharacter, kind)
	assert.Equal(t, "char-alice", id)
}

func TestHostDeliverEventForwardsTrustedActorMetadata(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	host := NewHostWithFactory(&mockClientFactory{client: mockClient})
	host.plugins["core-scenes"] = &loadedPlugin{
		manifest: &plugins.Manifest{Name: "core-scenes"},
		plugin:   grpcClient,
	}

	ctx := core.WithActor(context.Background(), core.Actor{
		Kind: core.ActorCharacter,
		ID:   "char-alice",
	})
	_, err := host.DeliverEvent(ctx, "core-scenes", pluginsdk.Event{
		Stream: "scene:01SCENE",
		Type:   pluginsdk.EventTypeSystem,
	})
	require.NoError(t, err)

	kind, id, ok := pluginsdk.ActorMetadataFromOutgoingContext(grpcClient.eventCtx)
	require.True(t, ok)
	assert.Equal(t, pluginsdk.ActorCharacter, kind)
	assert.Equal(t, "char-alice", id)
}

func TestPluginHostServiceEmitEventPreservesIncomingActor(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := plugins.NewPluginEventEmitter(
		bus.Bus.Publisher(),
		func(string) *plugins.Manifest {
			return &plugins.Manifest{Name: "core-scenes", Emits: []string{"scene"}}
		},
		func(ctx context.Context, _ string) (core.Actor, error) {
			actor, ok := core.ActorFromContext(ctx)
			if !ok {
				return core.Actor{}, errors.New("missing actor")
			}
			return actor, nil
		},
	)

	host := NewHost()
	host.SetEventEmitter(emitter)

	server := &pluginHostServiceServer{
		host:       host,
		pluginName: "core-scenes",
	}

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"x-holomush-actor-kind", "0",
		"x-holomush-actor-id", "char-alice",
	))
	_, err := server.EmitEvent(ctx, &pluginv1.PluginHostServiceEmitEventRequest{
		Stream:    "scene:01SCENE",
		EventType: "system",
		Payload:   []byte(`{"kind":"created"}`),
	})
	require.NoError(t, err)

	msgs := drainEventbusStream(t, bus.JS)
	require.Len(t, msgs, 1)
	assert.Equal(t, "character", msgs[0].Header.Get(eventbus.HeaderActorKind))
	// "char-alice" is not a ULID → bridge drops id; App-Actor-ID omitted.
	assert.Empty(t, msgs[0].Header.Get(eventbus.HeaderActorID))
	var env eventbusv1.Event
	require.NoError(t, proto.Unmarshal(msgs[0].Data, &env))
	assert.Equal(t, []byte(`{"kind":"created"}`), env.GetPayload())
}

func TestPluginHostServiceEmitEventFallsBackToPluginActorWhenIncomingActorMissing(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := plugins.NewPluginEventEmitter(
		bus.Bus.Publisher(),
		func(string) *plugins.Manifest {
			return &plugins.Manifest{Name: "core-scenes", Emits: []string{"scene"}}
		},
		func(ctx context.Context, _ string) (core.Actor, error) {
			actor, ok := core.ActorFromContext(ctx)
			if !ok {
				return core.Actor{}, errors.New("missing actor")
			}
			return actor, nil
		},
	)

	host := NewHost()
	host.SetEventEmitter(emitter)

	server := &pluginHostServiceServer{
		host:       host,
		pluginName: "core-scenes",
	}

	_, err := server.EmitEvent(context.Background(), &pluginv1.PluginHostServiceEmitEventRequest{
		Stream:    "scene:01SCENE",
		EventType: "system",
		Payload:   []byte(`{"kind":"created"}`),
	})
	require.NoError(t, err)

	msgs := drainEventbusStream(t, bus.JS)
	require.Len(t, msgs, 1)
	assert.Equal(t, "plugin", msgs[0].Header.Get(eventbus.HeaderActorKind))
}

func TestPluginHostServiceEmitEventReturnsErrorWhenHostIsMissing(t *testing.T) {
	server := &pluginHostServiceServer{pluginName: "core-scenes"}

	_, err := server.EmitEvent(context.Background(), &pluginv1.PluginHostServiceEmitEventRequest{
		Stream:    "scene:01SCENE",
		EventType: "system",
		Payload:   []byte(`{"kind":"created"}`),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestPluginHostServiceEmitEventReturnsErrorWhenEmitterIsMissing(t *testing.T) {
	server := &pluginHostServiceServer{
		host:       NewHost(),
		pluginName: "core-scenes",
	}

	_, err := server.EmitEvent(context.Background(), &pluginv1.PluginHostServiceEmitEventRequest{
		Stream:    "scene:01SCENE",
		EventType: "system",
		Payload:   []byte(`{"kind":"created"}`),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event emitter is not configured")
}

func TestSDKActorKindToCoreMapsSystemKind(t *testing.T) {
	assert.Equal(t, core.ActorSystem, sdkActorKindToCore(pluginsdk.ActorSystem))
}

func TestSDKActorKindToCoreDefaultsToPluginKind(t *testing.T) {
	assert.Equal(t, core.ActorPlugin, sdkActorKindToCore(pluginsdk.ActorKind(99)))
}

func TestNewPluginHostServiceServerRegistersPluginHostService(t *testing.T) {
	factory := newPluginHostServiceServer(NewHost(), "core-scenes")

	server := factory(nil)
	t.Cleanup(server.Stop)

	assert.Contains(t, server.GetServiceInfo(), "holomush.plugin.v1.PluginHostService")
}

func TestPluginHostServiceEmitEventReturnsWrappedEmitterError(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := plugins.NewPluginEventEmitter(
		bus.Bus.Publisher(),
		func(string) *plugins.Manifest {
			return &plugins.Manifest{Name: "core-scenes", Emits: []string{"scene"}}
		},
		func(ctx context.Context, _ string) (core.Actor, error) {
			actor, ok := core.ActorFromContext(ctx)
			if !ok {
				return core.Actor{}, errors.New("missing actor")
			}
			return actor, nil
		},
	)

	host := NewHost()
	host.SetEventEmitter(emitter)

	server := &pluginHostServiceServer{
		host:       host,
		pluginName: "core-scenes",
	}

	_, err := server.EmitEvent(context.Background(), &pluginv1.PluginHostServiceEmitEventRequest{
		Stream:    "scene:01SCENE",
		EventType: "system",
		Payload:   []byte(`{"kind":`),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "valid JSON")
}

func TestLoadSkipsInitForPluginWithoutStorageOrRequires(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/simple-plugin")
	require.NoError(t, err)

	manifest := &plugins.Manifest{
		Name:    "simple-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "simple-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err)

	assert.False(t, grpcClient.initCalled, "expected Init NOT to be called for simple plugin")
}

func TestLoadFailsWhenInitReturnsError(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{
		initErr: errors.New("init failed: migration error"),
	}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/broken-plugin")
	require.NoError(t, err)

	manifest := &plugins.Manifest{
		Name:    "broken-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		Storage: plugins.StoragePostgres,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "broken-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.Error(t, err, "expected error when Init fails")
	assert.Contains(t, err.Error(), "init failed: migration error")
	assert.True(t, mockClient.killed, "expected client to be killed after Init failure")

	// Plugin should not be in the loaded list
	assert.Empty(t, host.Plugins(), "expected no plugins loaded after Init failure")
}

func TestWithSchemaProvisionerOptionSetsField(t *testing.T) {
	provisioner := plugins.NewSchemaProvisioner("postgres://localhost/test")
	factory := &mockClientFactory{client: &mockPluginClient{}}
	host := NewHostWithFactory(factory, WithSchemaProvisioner(provisioner))

	assert.NotNil(t, host.schemaProvisioner, "expected schemaProvisioner to be set by option")
}

// stubClientConn is a minimal grpc.ClientConnInterface for testing.
type stubClientConn struct {
	grpc.ClientConnInterface
}

func TestPluginConnReturnsConnectionForLoadedPlugin(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, createTempExecutable(filepath.Join(tmpDir, "test-plugin")))

	fakeConn := &stubClientConn{}
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient, conn: fakeConn},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	require.NoError(t, host.Load(context.Background(), manifest, tmpDir))

	conn, err := host.PluginConn("test-plugin")
	require.NoError(t, err)
	assert.Same(t, fakeConn, conn, "expected the same connection that was set on the mock protocol")
}

func TestPluginConnReturnsErrorForUnknownPlugin(t *testing.T) {
	host := NewHost()

	_, err := host.PluginConn("nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPluginNotLoaded)
}

func TestPluginConnReturnsErrorAfterClose(t *testing.T) {
	host := NewHost()
	require.NoError(t, host.Close(context.Background()))

	_, err := host.PluginConn("any")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHostClosed)
}

func TestPluginConnReturnsErrorWhenNoConnection(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, createTempExecutable(filepath.Join(tmpDir, "test-plugin")))

	// Protocol without a Conn() — nil conn field.
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient, conn: nil},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	require.NoError(t, host.Load(context.Background(), manifest, tmpDir))

	_, err := host.PluginConn("test-plugin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no gRPC connection")
}

func TestHostImplementsServiceConnProvider(_ *testing.T) {
	var _ plugins.ServiceConnProvider = (*Host)(nil)
}

// --- QuerySessionStreams tests ---

func TestGopluginHostQuerySessionStreamsCallsPluginRPC(t *testing.T) {
	var capturedReq *pluginv1.QuerySessionStreamsRequest
	grpcClient := &mockGRPCPluginClient{
		QuerySessionStreamsFunc: func(_ context.Context, req *pluginv1.QuerySessionStreamsRequest) (*pluginv1.QuerySessionStreamsResponse, error) {
			capturedReq = req
			return &pluginv1.QuerySessionStreamsResponse{
				Streams: []string{"channel:general", "channel:ooc"},
			}, nil
		},
	}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err)

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err)

	req := plugins.SessionStreamsRequest{
		CharacterID: "char-123",
		PlayerID:    "player-456",
		SessionID:   "sess-789",
	}

	streams, err := host.QuerySessionStreams(ctx, "test-plugin", req)
	require.NoError(t, err)
	assert.Equal(t, []string{"channel:general", "channel:ooc"}, streams)
	require.NotNil(t, capturedReq)
	assert.Equal(t, "char-123", capturedReq.CharacterId)
	assert.Equal(t, "player-456", capturedReq.PlayerId)
	assert.Equal(t, "sess-789", capturedReq.SessionId)
}

func TestGopluginHostQuerySessionStreamsReturnsErrorFromPlugin(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{
		QuerySessionStreamsFunc: func(_ context.Context, _ *pluginv1.QuerySessionStreamsRequest) (*pluginv1.QuerySessionStreamsResponse, error) {
			return &pluginv1.QuerySessionStreamsResponse{
				Error: "db unavailable",
			}, nil
		},
	}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err)

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err)

	req := plugins.SessionStreamsRequest{
		CharacterID: "char-123",
		PlayerID:    "player-456",
		SessionID:   "sess-789",
	}

	_, err = host.QuerySessionStreams(ctx, "test-plugin", req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db unavailable")
}

func TestGopluginHostQuerySessionStreamsReturnsErrorOnRPCFailure(t *testing.T) {
	wantErr := errors.New("transport failure")
	grpcClient := &mockGRPCPluginClient{
		QuerySessionStreamsFunc: func(_ context.Context, _ *pluginv1.QuerySessionStreamsRequest) (*pluginv1.QuerySessionStreamsResponse, error) {
			return nil, wantErr
		},
	}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory)

	ctx := context.Background()
	tmpDir := t.TempDir()
	err := createTempExecutable(tmpDir + "/test-plugin")
	require.NoError(t, err)

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err = host.Load(ctx, manifest, tmpDir)
	require.NoError(t, err)

	_, err = host.QuerySessionStreams(ctx, "test-plugin", plugins.SessionStreamsRequest{
		CharacterID: "char-1",
		PlayerID:    "player-1",
		SessionID:   "sess-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transport failure")
}

func TestGopluginHostQuerySessionStreamsReturnsErrorWhenHostClosed(t *testing.T) {
	factory := &mockClientFactory{client: &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: &mockGRPCPluginClient{}},
	}}
	host := NewHostWithFactory(factory)

	tmpDir := t.TempDir()
	require.NoError(t, createTempExecutable(tmpDir+"/test-plugin"))
	manifest := &plugins.Manifest{
		Name:         "test-plugin",
		Version:      "1.0.0",
		Type:         plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{Executable: "test-plugin"},
	}
	require.NoError(t, host.Load(context.Background(), manifest, tmpDir))
	require.NoError(t, host.Close(context.Background()))

	_, err := host.QuerySessionStreams(context.Background(), "test-plugin", plugins.SessionStreamsRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHostClosed)
}

func TestGopluginHostQuerySessionStreamsReturnsErrorForUnknownPlugin(t *testing.T) {
	host := NewHostWithFactory(&mockClientFactory{client: &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: &mockGRPCPluginClient{}},
	}})

	_, err := host.QuerySessionStreams(context.Background(), "nonexistent", plugins.SessionStreamsRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPluginNotLoaded)
}

func TestHostFocusCoordinatorReturnsNilByDefault(t *testing.T) {
	host := NewHost()
	assert.Nil(t, host.FocusCoordinator())
}

func TestHostHistoryReaderReturnsNilByDefault(t *testing.T) {
	host := NewHost()
	assert.Nil(t, host.HistoryReader())
}

func TestHostSetFocusCoordinatorStoresAndReturnsCoordinator(t *testing.T) {
	host := NewHost()
	fc := &stubCoordinator{}
	host.SetFocusCoordinator(fc)
	assert.Equal(t, fc, host.FocusCoordinator())
}

func TestHostSetHistoryReaderStoresAndReturnsHistoryReader(t *testing.T) {
	host := NewHost()
	hr := &stubHistoryReader{}
	host.SetHistoryReader(hr)
	assert.Equal(t, hr, host.HistoryReader())
}

func TestHostWithFocusCoordinatorOptionSetsCoordinator(t *testing.T) {
	fc := &stubCoordinator{}
	host := NewHost(WithFocusCoordinator(fc))
	assert.Equal(t, fc, host.FocusCoordinator())
}

func TestHostWithHistoryReaderOptionSetsHistoryReader(t *testing.T) {
	hr := &stubHistoryReader{}
	host := NewHost(WithHistoryReader(hr))
	assert.Equal(t, hr, host.HistoryReader())
}
