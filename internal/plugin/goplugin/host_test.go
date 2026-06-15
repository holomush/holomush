// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	hashiplug "github.com/hashicorp/go-plugin"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	plugins "github.com/holomush/holomush/internal/plugin"
	tlscerts "github.com/holomush/holomush/internal/tls"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"github.com/oklog/ulid/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
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
	initCalled   bool
	initReq      *pluginv1.InitRequest
	initErr      error
	initResponse *pluginv1.InitResponse // INV-PLUGIN-32: per-test override; nil falls back to empty InitResponse

	QuerySessionStreamsFunc func(ctx context.Context, req *pluginv1.QuerySessionStreamsRequest) (*pluginv1.QuerySessionStreamsResponse, error)
}

// setInitResponse installs a per-test override for the Init RPC response
// (used by INV-PLUGIN-32 tests to feed RegisteredEmitTypes through the host).
func (m *mockGRPCPluginClient) setInitResponse(r *pluginv1.InitResponse) {
	m.initResponse = r
}

// grpcMockFor extracts the underlying *mockGRPCPluginClient from a
// *mockPluginClient returned by newMockHost. Use this when tests need
// to configure InitResponse via setInitResponse.
func grpcMockFor(c *mockPluginClient) *mockGRPCPluginClient {
	return c.protocol.pluginClient.(*mockGRPCPluginClient)
}

func (m *mockGRPCPluginClient) Init(_ context.Context, req *pluginv1.InitRequest, _ ...grpc.CallOption) (*pluginv1.InitResponse, error) {
	m.initCalled = true
	m.initReq = req
	if m.initErr != nil {
		return nil, m.initErr
	}
	if m.initResponse != nil {
		return m.initResponse, nil
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

// stubRegistryFor returns a pre-seeded IdentityRegistry stub for the given
// plugin names. Each name maps to a freshly-minted ULID. Tests that call
// DeliverEvent/DeliverCommand MUST inject this via WithIdentityRegistry so
// stampPluginActor can resolve names to ULIDs.
func stubRegistryFor(names ...string) (plugins.IdentityRegistry, map[string]ulid.ULID) {
	ids := make(map[string]ulid.ULID, len(names))
	for _, n := range names {
		ids[n] = core.NewULID()
	}
	return &stubIdentityRegistry{idsByName: ids}, ids
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
	reg, _ := stubRegistryFor("test-plugin")
	host := NewHostWithFactory(factory, WithIdentityRegistry(reg))

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
	reg, _ := stubRegistryFor("test-plugin")
	host := NewHostWithFactory(factory, WithIdentityRegistry(reg))

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
	reg, _ := stubRegistryFor("test-plugin")
	host := NewHostWithFactory(factory, WithIdentityRegistry(reg))

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
	reg, _ := stubRegistryFor("test-plugin")
	host := NewHostWithFactory(factory, WithIdentityRegistry(reg))

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
		Type:      pluginsdk.EventType("say"),
		Timestamp: 1234567890,
		ActorKind: pluginsdk.ActorCharacter,
		ActorID:   "char-789",
		Payload:   `{"text":"hello world"}`,
	}

	emits, err := host.DeliverEvent(ctx, "test-plugin", event)
	require.NoError(t, err, "DeliverEvent returned error")
	require.Len(t, emits, 1, "expected 1 emit event")
	assert.Equal(t, "location:123", emits[0].Stream, "expected stream 'location:123'")
	assert.Equal(t, pluginsdk.EventType("say"), emits[0].Type, "expected type 'say'")
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
			reg, _ := stubRegistryFor("test-plugin")
			host := NewHostWithFactory(factory, WithIdentityRegistry(reg))

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
	reg, _ := stubRegistryFor("test-plugin")
	host := NewHostWithFactory(factory, WithIdentityRegistry(reg))

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
	assert.Equal(t, pluginsdk.EventType("say"), resp.Events[0].Type)
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
	reg, _ := stubRegistryFor("test-plugin")
	host := NewHostWithFactory(factory, WithIdentityRegistry(reg))

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
	reg, _ := stubRegistryFor("test-plugin")
	host := NewHostWithFactory(factory, WithIdentityRegistry(reg))

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
			reg, _ := stubRegistryFor("test-plugin")
			host := NewHostWithFactory(factory, WithIdentityRegistry(reg))

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
		Requires: plugins.RequireServices("event-store"),
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
		Requires: plugins.RequireServices("holomush.scene.v1.SceneService"),
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
	reg, _ := stubRegistryFor("core-scenes")
	host := NewHostWithFactory(&mockClientFactory{client: mockClient}, WithIdentityRegistry(reg))
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
	reg, _ := stubRegistryFor("core-scenes")
	host := NewHostWithFactory(&mockClientFactory{client: mockClient}, WithIdentityRegistry(reg))
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
		Type:   pluginsdk.EventType(core.EventTypeSystem),
	})
	require.NoError(t, err)

	kind, id, ok := pluginsdk.ActorMetadataFromOutgoingContext(grpcClient.eventCtx)
	require.True(t, ok)
	assert.Equal(t, pluginsdk.ActorCharacter, kind)
	assert.Equal(t, "char-alice", id)
}

// Note: the legacy "preserves incoming actor metadata" and "falls back to
// plugin actor when metadata missing" tests were removed in Task 9 of the
// plugin actor-claim authentication rollout. Those behaviors trusted
// plugin-supplied x-holomush-actor-kind / -actor-id headers, which the
// new token-based EmitEvent (host_service.go) explicitly DISCARDS per
// spec §3.3.5. Their replacements live in host_service_test.go:
//   - TestEmitEventUsesStoredActorIgnoringPluginClaim (G1 forgery override)
//   - TestEmitEventMissingTokenFails (EMIT_TOKEN_MISSING)
//   - TestEmitEventUnknownTokenFails (EMIT_TOKEN_REJECTED)
//   - TestEmitEventCrossPluginTokenLeakFails (cross-plugin token defense)

func TestPluginHostServiceEmitEventReturnsErrorWhenHostIsMissing(t *testing.T) {
	server := &pluginHostServiceServer{pluginName: "core-scenes"}

	_, err := server.EmitEvent(context.Background(), &hostv1.EmitEventRequest{
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

	_, err := server.EmitEvent(context.Background(), &hostv1.EmitEventRequest{
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

func TestNewPluginHostServiceServerRegistersCapabilityServices(t *testing.T) {
	factory := newPluginHostServiceServer(NewHost(), &plugins.Manifest{Name: "core-scenes"})

	server := factory(nil)
	t.Cleanup(server.Stop)

	info := server.GetServiceInfo()
	// The monolithic god-service is gone (holomush-eykuh.1, Task 12); the broker
	// now serves the capability-scoped host.v1 services instead.
	assert.NotContains(t, info, "holomush.plugin.v1.PluginHostService")
	assert.Contains(t, info, "holomush.plugin.host.v1.EmitService")
}

func TestPluginHostServiceEmitEventReturnsWrappedEmitterError(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := plugins.NewPluginEventEmitter(
		bus.Bus.Publisher(),
		func(string) *plugins.Manifest {
			return &plugins.Manifest{
				Name:                "core-scenes",
				Emits:               []string{"scene"},
				ActorKindsClaimable: []string{"plugin", "character"},
			}
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
	defer func() { _ = host.Close(context.Background()) }()

	server := &pluginHostServiceServer{
		host:       host,
		pluginName: "core-scenes",
	}

	// Token-based EmitEvent (Task 9): the emitter is only reached after
	// the host validates a per-dispatch token, so the test issues one and
	// presents it on the incoming context. The malformed JSON payload
	// then exercises the emitter's error-wrap path.
	tok, err := host.tokenStore.Issue("core-scenes", core.Actor{Kind: core.ActorPlugin, ID: "core-scenes"}, "")
	require.NoError(t, err)
	defer host.tokenStore.Revoke(tok)
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("x-holomush-emit-token", tok))

	_, err = server.EmitEvent(ctx, &hostv1.EmitEventRequest{
		Stream:    "scene:01SCENE",
		EventType: "system",
		Payload:   []byte(`{"kind":`),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "valid JSON")
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

// Compile-time interface check: *Host must satisfy plugins.ServiceConnProvider.
var _ plugins.ServiceConnProvider = (*Host)(nil)

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

// TestNewHostInitializesTokenStore verifies Host construction wires the
// emitTokenStore and starts its sweeper goroutine. The closed-host
// snapshot taken via goleak.IgnoreCurrent() filters out sweeper
// goroutines leaked by sibling tests in this file that construct a
// Host but never call Close — the assertion only catches NEW goroutines
// our Host fails to clean up.
//
// NOTE: NOT t.Parallel — goleak.VerifyNone observes ALL live goroutines.
func TestNewHostInitializesTokenStore(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	h := NewHost()
	require.NotNil(t, h.tokenStore, "Host must construct emitTokenStore")
	require.NoError(t, h.Close(context.Background()))
}

// TestHostCloseClosesTokenStore verifies Host.Close shuts the token
// store down (sweeper goroutine exits, entries cleared).
func TestHostCloseClosesTokenStore(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	h := NewHost()
	_, err := h.tokenStore.Issue("plug-A", core.Actor{Kind: core.ActorPlugin, ID: "plug-A"}, "")
	require.NoError(t, err)
	require.NoError(t, h.Close(context.Background()))
	// After Close, the token store is reset.
	h.tokenStore.mu.RLock()
	n := len(h.tokenStore.items)
	h.tokenStore.mu.RUnlock()
	assert.Equal(t, 0, n)
}

// TestHostCloseIdempotentWithTokenStore verifies the second Close call
// hits the closed-guard early-return and does not double-cancel the
// sweeper context or panic on already-closed channel.
func TestHostCloseIdempotentWithTokenStore(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	h := NewHost()
	require.NoError(t, h.Close(context.Background()))
	require.NoError(t, h.Close(context.Background()))
}

// TestDeliverEventIssuesTokenWithCharacterActor verifies the host issues
// a per-dispatch token, attaches it to outgoing metadata, and revokes
// the entry on call return (defer Revoke).
func TestDeliverEventIssuesTokenWithCharacterActor(t *testing.T) {
	t.Parallel()
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
	reg, _ := stubRegistryFor("plug-A")
	h := NewHostWithFactory(&mockClientFactory{client: mockClient}, WithIdentityRegistry(reg))
	defer func() { _ = h.Close(context.Background()) }()
	h.plugins["plug-A"] = &loadedPlugin{
		manifest: &plugins.Manifest{Name: "plug-A"},
		plugin:   grpcClient,
	}

	charID := "01HCHAR0000000000000000000"
	ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorCharacter, ID: charID})
	_, err := h.DeliverEvent(ctx, "plug-A", pluginsdk.Event{Type: "say"})
	require.NoError(t, err)

	// Inspect the outgoing metadata that the host attached to the gRPC call.
	md, ok := metadata.FromOutgoingContext(grpcClient.eventCtx)
	require.True(t, ok, "outgoing metadata MUST be present on the call to plugin")
	tokens := md.Get("x-holomush-emit-token")
	require.Len(t, tokens, 1, "x-holomush-emit-token MUST be set exactly once")
	capturedToken := tokens[0]
	require.NotEmpty(t, capturedToken)

	// The entry will already be Revoke'd by defer when DeliverEvent returns.
	_, _, ok = h.tokenStore.Lookup("plug-A", capturedToken)
	assert.False(t, ok, "deferred Revoke MUST clear the token after DeliverEvent returns")
}

// TestDeliverEventStoresUpstreamCharacterActorVerbatim asserts the host
// attaches ActorCharacter:<charID> verbatim as outgoing actor metadata
// (cascade preserved per spec §3.3.4).
func TestDeliverEventStoresUpstreamCharacterActorVerbatim(t *testing.T) {
	t.Parallel()
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
	reg, _ := stubRegistryFor("plug-A")
	h := NewHostWithFactory(&mockClientFactory{client: mockClient}, WithIdentityRegistry(reg))
	defer func() { _ = h.Close(context.Background()) }()
	h.plugins["plug-A"] = &loadedPlugin{
		manifest: &plugins.Manifest{Name: "plug-A"},
		plugin:   grpcClient,
	}

	charID := "01HCHAR0000000000000000000"
	ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorCharacter, ID: charID})
	_, err := h.DeliverEvent(ctx, "plug-A", pluginsdk.Event{Type: "say"})
	require.NoError(t, err)

	kind, id, ok := pluginsdk.ActorMetadataFromOutgoingContext(grpcClient.eventCtx)
	require.True(t, ok)
	assert.Equal(t, pluginsdk.ActorCharacter, kind)
	assert.Equal(t, charID, id)
}

// TestDeliverEventReanchorsActorSystem verifies spec §3.3.4: when ctx has
// ActorSystem, the host attaches ActorPlugin:<self> as outgoing metadata
// (re-anchored). Plugins can never speak as the host's system identity.
func TestDeliverEventReanchorsActorSystem(t *testing.T) {
	t.Parallel()
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
	reg, ids := stubRegistryFor("plug-A")
	h := NewHostWithFactory(&mockClientFactory{client: mockClient}, WithIdentityRegistry(reg))
	defer func() { _ = h.Close(context.Background()) }()
	h.plugins["plug-A"] = &loadedPlugin{
		manifest: &plugins.Manifest{Name: "plug-A"},
		plugin:   grpcClient,
	}

	ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorSystem, ID: core.ActorSystemID})
	_, err := h.DeliverEvent(ctx, "plug-A", pluginsdk.Event{Type: "tick"})
	require.NoError(t, err)

	kind, id, ok := pluginsdk.ActorMetadataFromOutgoingContext(grpcClient.eventCtx)
	require.True(t, ok)
	assert.Equal(t, pluginsdk.ActorPlugin, kind, "ActorSystem MUST be re-anchored to ActorPlugin at issuance")
	assert.Equal(t, ids["plug-A"].String(), id, "re-anchored ID MUST be the plugin ULID, not the name")
}

// TestDeliverEventNoActorFallsBackToPluginIdentity covers the bootstrap
// edge case: no actor in ctx → outgoing metadata carries ActorPlugin:<self>.
func TestDeliverEventNoActorFallsBackToPluginIdentity(t *testing.T) {
	t.Parallel()
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
	reg, ids := stubRegistryFor("plug-A")
	h := NewHostWithFactory(&mockClientFactory{client: mockClient}, WithIdentityRegistry(reg))
	defer func() { _ = h.Close(context.Background()) }()
	h.plugins["plug-A"] = &loadedPlugin{
		manifest: &plugins.Manifest{Name: "plug-A"},
		plugin:   grpcClient,
	}

	_, err := h.DeliverEvent(context.Background(), "plug-A", pluginsdk.Event{Type: "test"})
	require.NoError(t, err)

	kind, id, ok := pluginsdk.ActorMetadataFromOutgoingContext(grpcClient.eventCtx)
	require.True(t, ok)
	assert.Equal(t, pluginsdk.ActorPlugin, kind)
	assert.Equal(t, ids["plug-A"].String(), id, "fallback ID MUST be the plugin ULID, not the name")
}

// TestDeliverCommandIssuesTokenWithCharacterActor mirrors the DeliverEvent
// case for the command boundary.
func TestDeliverCommandIssuesTokenWithCharacterActor(t *testing.T) {
	t.Parallel()
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
	reg, _ := stubRegistryFor("plug-A")
	h := NewHostWithFactory(&mockClientFactory{client: mockClient}, WithIdentityRegistry(reg))
	defer func() { _ = h.Close(context.Background()) }()
	h.plugins["plug-A"] = &loadedPlugin{
		manifest: &plugins.Manifest{Name: "plug-A"},
		plugin:   grpcClient,
	}

	charID := "01HCHAR0000000000000000000"
	ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorCharacter, ID: charID})
	_, err := h.DeliverCommand(ctx, "plug-A", pluginsdk.CommandRequest{Command: "say"})
	require.NoError(t, err)

	md, ok := metadata.FromOutgoingContext(grpcClient.commandCtx)
	require.True(t, ok, "outgoing metadata MUST be present on the call to plugin")
	tokens := md.Get("x-holomush-emit-token")
	require.Len(t, tokens, 1)
	assert.NotEmpty(t, tokens[0])

	_, _, ok = h.tokenStore.Lookup("plug-A", tokens[0])
	assert.False(t, ok, "deferred Revoke MUST clear the token after DeliverCommand returns")
}

// TestDeliverCommandReanchorsActorSystem mirrors the ActorSystem
// re-anchor invariant (spec §3.3.4) for the command path.
func TestDeliverCommandReanchorsActorSystem(t *testing.T) {
	t.Parallel()
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
	reg, ids := stubRegistryFor("plug-A")
	h := NewHostWithFactory(&mockClientFactory{client: mockClient}, WithIdentityRegistry(reg))
	defer func() { _ = h.Close(context.Background()) }()
	h.plugins["plug-A"] = &loadedPlugin{
		manifest: &plugins.Manifest{Name: "plug-A"},
		plugin:   grpcClient,
	}

	ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorSystem, ID: core.ActorSystemID})
	_, err := h.DeliverCommand(ctx, "plug-A", pluginsdk.CommandRequest{Command: "tick"})
	require.NoError(t, err)

	kind, id, ok := pluginsdk.ActorMetadataFromOutgoingContext(grpcClient.commandCtx)
	require.True(t, ok)
	assert.Equal(t, pluginsdk.ActorPlugin, kind)
	assert.Equal(t, ids["plug-A"].String(), id, "re-anchored ID MUST be the plugin ULID, not the name")
}

// TestDeliverEventNoRecoverWrapper (round-1 plan-reviewer N2) — asserts
// Host.DeliverEvent does NOT contain a recover() call. A recover() in this
// path would swallow host-side panics and skip the deferred token Revoke,
// masking forgery attempts and other security failures. The runtime's
// default panic semantics are correct: surface, defer-Revoke, propagate.
//
// Static-analysis approach via go/ast — parsed from CWD = the goplugin/
// package directory, which is where `go test` runs.
func TestDeliverEventNoRecoverWrapper(t *testing.T) {
	t.Parallel()
	assertNoRecoverInFunction(t, "DeliverEvent")
}

// TestDeliverCommandNoRecoverWrapper applies the same recover()-guard
// invariant to the command boundary.
func TestDeliverCommandNoRecoverWrapper(t *testing.T) {
	t.Parallel()
	assertNoRecoverInFunction(t, "DeliverCommand")
}

// assertNoRecoverInFunction parses host.go and asserts that the named
// top-level function does not contain a recover() call.
func assertNoRecoverInFunction(t *testing.T, funcName string) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "host.go", nil, parser.ParseComments)
	require.NoError(t, err)
	var decl *ast.FuncDecl
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == funcName {
			decl = fn
			break
		}
	}
	require.NotNilf(t, decl, "%s function must be in host.go", funcName)
	ast.Inspect(decl, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		assert.NotEqualf(t, "recover", ident.Name,
			"Host.%s MUST NOT contain recover() — would swallow panics and skip deferred token Revoke", funcName)
		return true
	})
}

// TestBinaryHost_PluginEmitRegistry_NotLoaded verifies that
// PluginEmitRegistry returns (nil, false) for a plugin that has not been
// loaded.
func TestBinaryHost_PluginEmitRegistry_NotLoaded(t *testing.T) {
	host, _ := newMockHost(t)

	got, ok := host.PluginEmitRegistry("missing")
	assert.False(t, ok)
	assert.Nil(t, got)
}

// TestBinaryHost_PluginEmitRegistry_LoadedPluginCapturesInitResponse
// verifies that a successfully-loaded plugin with InitResponse populated by
// the plugin returns the captured set.
func TestBinaryHost_PluginEmitRegistry_LoadedPluginCapturesInitResponse(t *testing.T) {
	host, mockClient := newMockHost(t)
	grpcMockFor(mockClient).setInitResponse(&pluginv1.InitResponse{
		RegisteredEmitTypes: []string{"a", "b"},
	})

	tmpDir := t.TempDir()
	require.NoError(t, createTempExecutable(tmpDir+"/withemits"))

	manifest := &plugins.Manifest{
		Name:    "withemits",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "withemits",
		},
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "a", Sensitivity: plugins.SensitivityNever},
				{EventType: "b", Sensitivity: plugins.SensitivityNever},
			},
		},
	}
	require.NoError(t, host.Load(context.Background(), manifest, tmpDir))

	got, ok := host.PluginEmitRegistry("withemits")
	assert.True(t, ok)
	assert.Equal(t, []string{"a", "b"}, got)
}

func TestHostConfigOverrideForPlugin(t *testing.T) {
	h := &Host{configOverrides: map[string]map[string]string{
		"demo": {"vote_window": "5s"},
	}}
	require.Equal(t, map[string]string{"vote_window": "5s"}, h.overrideFor("demo"))
	require.Nil(t, h.overrideFor("absent")) // no override → nil (defaults apply)
}

// INV-PLUGIN-8 anchor: this test also exercises the INV-PLUGIN-8 guarantee — a
// config-bearing binary plugin receives its merged plugin_config via InitRequest.
// Its formal registry binding is INV-PLUGIN-3 (annotated below); the INV-PLUGIN-8
// reference here is the provenance anchor for that pending entry's refs.
//
// TestBinaryHostDeliversCanonicalMergedConfigViaInitRequest asserts that when a
// binary plugin is loaded with a config override, the InitRequest.Config.PluginConfig
// field delivered to the plugin equals the canonical plugins.MergePluginConfig
// output for the same (schema, override) inputs. This proves INV-PLUGIN-3 for the
// binary (gRPC) delivery path: the host does not re-derive the config per runtime
// but threads through the shared MergePluginConfig computation.
//
// The schema has both a default-bearing key and an overridable key so a fork that
// ignored either defaults or overrides would produce a different map and fail the
// assertion.
//
// Verifies: INV-PLUGIN-3
func TestBinaryHostDeliversCanonicalMergedConfigViaInitRequest(t *testing.T) {
	schema := map[string]plugins.ConfigParam{
		"vote_window":    {Type: "duration", Default: "168h", Required: true},
		"cooloff_window": {Type: "duration", Default: "30m"},
	}
	override := map[string]string{"cooloff_window": "5s"}

	// Compute the canonical expected output via MergePluginConfig directly.
	want, err := plugins.MergePluginConfig(schema, override)
	require.NoError(t, err, "MergePluginConfig must not error on valid inputs")

	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{
		protocol: &mockClientProtocol{pluginClient: grpcClient},
	}
	factory := &mockClientFactory{client: mockClient}
	host := NewHostWithFactory(factory, WithConfigOverrides(map[string]map[string]string{
		"parity-plugin": override,
	}))

	tmpDir := t.TempDir()
	require.NoError(t, createTempExecutable(filepath.Join(tmpDir, "parity-plugin")))

	manifest := &plugins.Manifest{
		Name:    "parity-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		Config:  schema,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "parity-plugin",
		},
	}

	require.NoError(t, host.Load(context.Background(), manifest, tmpDir))

	require.True(t, grpcClient.initCalled, "Init must be called for a config-bearing plugin")
	require.NotNil(t, grpcClient.initReq, "InitRequest must be set")
	require.NotNil(t, grpcClient.initReq.Config, "ServiceConfig must be set")
	require.Equal(t, want, grpcClient.initReq.Config.PluginConfig,
		"binary host's InitRequest.Config.PluginConfig must equal the canonical "+
			"MergePluginConfig output (INV-PLUGIN-3: no per-runtime config fork)")
}

// Verifies: INV-PLUGIN-54
func TestLoadPassesDeclaredCapabilitiesToInit(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
	host := NewHostWithFactory(&mockClientFactory{client: mockClient})

	ctx := context.Background()
	tmpDir := t.TempDir()
	require.NoError(t, createTempExecutable(tmpDir+"/test-plugin"))

	manifest := &plugins.Manifest{
		Name: "test-plugin", Version: "1.0.0", Type: plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{Executable: "test-plugin"},
		Requires: []plugins.Dependency{
			{Kind: plugins.DependencyCapability, Name: "focus"},
			{Kind: plugins.DependencyCapability, Name: "stream.history"},
		},
	}

	require.NoError(t, host.Load(ctx, manifest, tmpDir))
	require.NotNil(t, grpcClient.initReq, "Init must be called")
	require.NotNil(t, grpcClient.initReq.Config, "ServiceConfig must be set")
	assert.ElementsMatch(t, []string{"focus", "stream.history"},
		grpcClient.initReq.Config.GetDeclaredCapabilities())
}

// Verifies: INV-PLUGIN-54 — unconditional Init: a binary plugin with no
// requires/storage/config still gets Init, so the SDK capability validation
// always runs (closes the degenerate "declares nothing" escape).
func TestLoadCallsInitForBinaryPluginWithNoRequires(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
	host := NewHostWithFactory(&mockClientFactory{client: mockClient})

	ctx := context.Background()
	tmpDir := t.TempDir()
	require.NoError(t, createTempExecutable(tmpDir+"/bare-plugin"))

	manifest := &plugins.Manifest{
		Name: "bare-plugin", Version: "1.0.0", Type: plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{Executable: "bare-plugin"},
	}

	require.NoError(t, host.Load(ctx, manifest, tmpDir))
	assert.True(t, grpcClient.initCalled, "Init must be called even with no requires (INV-PLUGIN-54)")
}

// TestBinaryHostWiresOnlyGrantedServices asserts that when WithPluginGrants is
// set, the broker loop skips any RequiredServiceName NOT in the grant set, and
// DeclaredCapabilities only includes granted capability tokens — not all
// manifest-declared caps (holomush-eykuh.4.7).
func TestBinaryHostWiresOnlyGrantedServices(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
	// Host with a plugin grant that includes only "focus" (capability) but
	// NOT "stream.history" — even though the manifest declares both.
	host := NewHostWithFactory(
		&mockClientFactory{client: mockClient},
		WithPluginGrants(map[string][]string{
			"test-plugin": {"focus"},
		}),
	)

	ctx := context.Background()
	tmpDir := t.TempDir()
	require.NoError(t, createTempExecutable(tmpDir+"/test-plugin"))

	manifest := &plugins.Manifest{
		Name: "test-plugin", Version: "1.0.0", Type: plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{Executable: "test-plugin"},
		Requires: []plugins.Dependency{
			{Kind: plugins.DependencyCapability, Name: "focus"},
			{Kind: plugins.DependencyCapability, Name: "stream.history"},
		},
	}

	require.NoError(t, host.Load(ctx, manifest, tmpDir))
	require.NotNil(t, grpcClient.initReq, "Init must be called")
	require.NotNil(t, grpcClient.initReq.Config, "ServiceConfig must be set")
	// Only "focus" must appear — "stream.history" is excluded by the grant set.
	assert.ElementsMatch(t, []string{"focus"},
		grpcClient.initReq.Config.GetDeclaredCapabilities(),
		"DeclaredCapabilities must reflect the grant set, not the full manifest")
	assert.NotContains(t, grpcClient.initReq.Config.GetDeclaredCapabilities(), "stream.history",
		"stream.history must not be wired: it is absent from the grant set")
}

// TestBinaryHostGrantsNilFallsBackToManifest asserts that when WithPluginGrants
// is NOT set (nil), DeclaredCapabilities contains all manifest-declared caps —
// preserving backward-compat for the no-registry path (holomush-eykuh.4.7).
func TestBinaryHostGrantsNilFallsBackToManifest(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
	host := NewHostWithFactory(&mockClientFactory{client: mockClient})

	ctx := context.Background()
	tmpDir := t.TempDir()
	require.NoError(t, createTempExecutable(tmpDir+"/test-plugin"))

	manifest := &plugins.Manifest{
		Name: "test-plugin", Version: "1.0.0", Type: plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{Executable: "test-plugin"},
		Requires: []plugins.Dependency{
			{Kind: plugins.DependencyCapability, Name: "focus"},
			{Kind: plugins.DependencyCapability, Name: "stream.history"},
		},
	}

	require.NoError(t, host.Load(ctx, manifest, tmpDir))
	require.NotNil(t, grpcClient.initReq, "Init must be called")
	// No grants set → falls back to manifest (all caps).
	assert.ElementsMatch(t, []string{"focus", "stream.history"},
		grpcClient.initReq.Config.GetDeclaredCapabilities(),
		"nil grants must fall back to manifest RequiredCapabilities")
}
