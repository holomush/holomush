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
	response    *pluginv1.HandleEventResponse
	err         error
	returnNil   bool // If true, return nil response (simulates edge case)
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
	if host == nil {
		t.Fatal("NewHost returned nil")
	}
}

func TestNewHost_NilEnforcer(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when enforcer is nil")
		}
	}()
	NewHost(nil)
}

func TestNewHostWithFactory_NilEnforcer(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when enforcer is nil")
		}
	}()
	NewHostWithFactory(nil, &DefaultClientFactory{})
}

func TestNewHostWithFactory_NilFactory(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when factory is nil")
		}
	}()
	enforcer := capability.NewEnforcer()
	NewHostWithFactory(enforcer, nil)
}

func TestPlugins_Empty(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	plugins := host.Plugins()
	if len(plugins) != 0 {
		t.Errorf("expected empty plugins list, got %v", plugins)
	}
}

func TestPlugins_AfterClose(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	if err := host.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	plugins := host.Plugins()
	if plugins != nil {
		t.Errorf("expected nil plugins after close, got %v", plugins)
	}
}

func TestClose_NoPlugins(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	err := host.Close(context.Background())
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestClose_PreventsFurtherLoads(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	err := host.Close(context.Background())
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

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
	if err == nil {
		t.Error("expected error when loading after close")
	}
	if !errors.Is(err, ErrHostClosed) {
		t.Errorf("expected ErrHostClosed, got: %v", err)
	}
}

func TestClose_Idempotent(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	// First close should succeed
	err1 := host.Close(context.Background())
	if err1 != nil {
		t.Fatalf("first Close returned error: %v", err1)
	}

	// Second close should also succeed (idempotent)
	err2 := host.Close(context.Background())
	if err2 != nil {
		t.Errorf("second Close returned error: %v", err2)
	}
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
	if err == nil {
		t.Error("expected error when loading with cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	if !strings.Contains(err.Error(), "load cancelled") {
		t.Errorf("expected error to mention 'load cancelled', got: %v", err)
	}
}

func TestUnload_NotLoaded(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	err := host.Unload(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error when unloading nonexistent plugin")
	}
	if !errors.Is(err, ErrPluginNotLoaded) {
		t.Errorf("expected ErrPluginNotLoaded, got: %v", err)
	}
}

func TestUnload_AfterClose(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	err := host.Close(context.Background())
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	err = host.Unload(context.Background(), "any-plugin")
	if err == nil {
		t.Error("expected error when unloading after close")
	}
	if !errors.Is(err, ErrHostClosed) {
		t.Errorf("expected ErrHostClosed, got: %v", err)
	}
}

func TestDeliverEvent_NotLoaded(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	_, err := host.DeliverEvent(context.Background(), "nonexistent", pluginpkg.Event{})
	if err == nil {
		t.Error("expected error when delivering to nonexistent plugin")
	}
	if !errors.Is(err, ErrPluginNotLoaded) {
		t.Errorf("expected ErrPluginNotLoaded, got: %v", err)
	}
}

func TestDeliverEvent_HostClosed(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)

	if err := host.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	_, err := host.DeliverEvent(context.Background(), "any-plugin", pluginpkg.Event{})
	if err == nil {
		t.Error("expected error when delivering after close")
	}
	if !errors.Is(err, ErrHostClosed) {
		t.Errorf("expected ErrHostClosed, got: %v", err)
	}
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
	if err := createTempExecutable(tmpDir + "/test-plugin"); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	if err := host.Load(ctx, manifest, tmpDir); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	_, err := host.DeliverEvent(ctx, "test-plugin", pluginpkg.Event{})
	if err == nil {
		t.Error("expected error when HandleEvent fails")
	}
	if !strings.Contains(err.Error(), "HandleEvent failed") {
		t.Errorf("expected error to mention 'HandleEvent failed', got: %v", err)
	}
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
	if err := createTempExecutable(tmpDir + "/test-plugin"); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	if err := host.Load(ctx, manifest, tmpDir); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	// DeliverEvent should handle nil response gracefully (proto getters are nil-safe)
	emits, err := host.DeliverEvent(ctx, "test-plugin", pluginpkg.Event{})
	if err != nil {
		t.Errorf("unexpected error with nil response: %v", err)
	}
	if len(emits) != 0 {
		t.Errorf("expected empty emits for nil response, got %d", len(emits))
	}
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
	if err := createTempExecutable(tmpDir + "/test-plugin"); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	if err := host.Load(ctx, manifest, tmpDir); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	_, err := host.DeliverEvent(ctx, "test-plugin", pluginpkg.Event{})
	if err == nil {
		t.Error("expected error on timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
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
	if err := createTempExecutable(tmpDir + "/test-plugin"); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err := host.Load(ctx, manifest, tmpDir)
	if err == nil {
		t.Error("expected error when client connection fails")
	}
	if !strings.Contains(err.Error(), "failed to connect") {
		t.Errorf("expected error to mention 'failed to connect', got: %v", err)
	}
	if !mockClient.killed {
		t.Error("expected client to be killed after connection failure")
	}
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
	if err := createTempExecutable(tmpDir + "/test-plugin"); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err := host.Load(ctx, manifest, tmpDir)
	if err == nil {
		t.Error("expected error when dispense fails")
	}
	if !strings.Contains(err.Error(), "failed to dispense") {
		t.Errorf("expected error to mention 'failed to dispense', got: %v", err)
	}
	if !mockClient.killed {
		t.Error("expected client to be killed after dispense failure")
	}
}

func TestLoad_Unload_Plugins_Cycle(t *testing.T) {
	host, mockClient := newMockHost(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test-plugin"
	if err := createTempExecutable(tmpFile); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err := host.Load(ctx, manifest, tmpDir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	plugins := host.Plugins()
	if len(plugins) != 1 {
		t.Errorf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0] != "test-plugin" {
		t.Errorf("expected plugin name 'test-plugin', got %q", plugins[0])
	}

	err = host.Unload(ctx, "test-plugin")
	if err != nil {
		t.Errorf("Unload returned error: %v", err)
	}

	plugins = host.Plugins()
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins after unload, got %d", len(plugins))
	}

	if !mockClient.killed {
		t.Error("expected mock client to be killed on unload")
	}
}

func TestLoad_DuplicateName(t *testing.T) {
	host, _ := newMockHost(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test-plugin"
	if err := createTempExecutable(tmpFile); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err := host.Load(ctx, manifest, tmpDir)
	if err != nil {
		t.Fatalf("first Load returned error: %v", err)
	}

	err = host.Load(ctx, manifest, tmpDir)
	if err == nil {
		t.Fatal("expected error when loading duplicate plugin name")
	}
	if !errors.Is(err, ErrPluginAlreadyLoaded) {
		t.Errorf("expected ErrPluginAlreadyLoaded, got: %v", err)
	}
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
	if err == nil {
		t.Fatal("expected error when loading nonexistent executable")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error to mention 'not found', got: %v", err)
	}
	// Verify error is wrapped (contains underlying os error)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected error to wrap os.ErrNotExist, got: %v", err)
	}
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
	if err := createTempExecutable(tmpDir + "/test-plugin"); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

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

	err := host.Load(ctx, manifest, tmpDir)
	if err == nil {
		t.Fatal("expected error when SetGrants fails")
	}
	if !strings.Contains(err.Error(), "failed to set capabilities") {
		t.Errorf("expected error to mention 'failed to set capabilities', got: %v", err)
	}

	// Verify plugin was not added to the host
	if len(host.Plugins()) != 0 {
		t.Error("plugin should not be loaded after SetGrants failure")
	}

	// Verify client was killed (cleanup on error)
	if !mockClient.killed {
		t.Error("client should be killed on SetGrants failure")
	}
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
	if err := os.Mkdir(restrictedDir, 0o755); err != nil {
		t.Fatalf("failed to create restricted dir: %v", err)
	}

	execPath := restrictedDir + "/plugin"
	//nolint:gosec // G306 - needs execute permission for valid plugin executable
	if err := os.WriteFile(execPath, []byte("dummy"), 0o755); err != nil {
		t.Fatalf("failed to create executable: %v", err)
	}

	// Remove all permissions from the directory - this will cause os.Stat to fail
	// with permission denied, NOT file not found
	if err := os.Chmod(restrictedDir, 0o000); err != nil {
		t.Fatalf("failed to chmod directory: %v", err)
	}
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

	err := host.Load(ctx, manifest, restrictedDir)
	if err == nil {
		t.Fatal("expected error when stat fails with permission denied")
	}
	// Should get a resolution or access error, not "not found"
	if !strings.Contains(err.Error(), "cannot resolve") && !strings.Contains(err.Error(), "cannot access") {
		t.Errorf("expected error to mention 'cannot resolve' or 'cannot access', got: %v", err)
	}
	// Verify it's NOT the "not found" error
	if strings.Contains(err.Error(), "not found") {
		t.Errorf("expected resolution/access error, not 'not found', got: %v", err)
	}
}

func TestLoad_ExecutableNotExecutable(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)
	ctx := context.Background()

	tmpDir := t.TempDir()
	execPath := tmpDir + "/non-executable-plugin"
	// Create file without execute permission (0o600 = rw-------)
	if err := os.WriteFile(execPath, []byte("not executable"), 0o600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	manifest := &plugin.Manifest{
		Name:    "non-executable",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "non-executable-plugin",
		},
	}

	err := host.Load(ctx, manifest, tmpDir)
	if err == nil {
		t.Fatal("expected error when loading non-executable file")
	}
	if !strings.Contains(err.Error(), "not executable") {
		t.Errorf("expected error to mention 'not executable', got: %v", err)
	}
}

func TestLoad_ExecutablePathTraversal(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)
	ctx := context.Background()

	tmpDir := t.TempDir()

	// Create executable in parent directory (outside plugin dir)
	parentExec := filepath.Dir(tmpDir) + "/escaped-plugin"
	if err := createTempExecutable(parentExec); err != nil {
		t.Fatalf("failed to create escaped executable: %v", err)
	}
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

	err := host.Load(ctx, manifest, tmpDir)
	if err == nil {
		t.Fatal("expected error when executable path escapes plugin directory")
	}
	if !strings.Contains(err.Error(), "escapes plugin directory") {
		t.Errorf("expected error to mention 'escapes plugin directory', got: %v", err)
	}
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
	if err := os.Mkdir(pluginDir, 0o755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	// Create an executable outside the plugin directory
	outsideExec := filepath.Join(tmpDir, "outside-exec")
	if err := createTempExecutable(outsideExec); err != nil {
		t.Fatalf("failed to create outside executable: %v", err)
	}

	// Create a symlink inside the plugin directory pointing outside
	symlinkPath := filepath.Join(pluginDir, "evil-link")
	if err := os.Symlink(outsideExec, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	manifest := &plugin.Manifest{
		Name:    "symlink-escape",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "evil-link", // Symlink that points outside
		},
	}

	err := host.Load(ctx, manifest, pluginDir)
	if err == nil {
		t.Fatal("expected error when executable symlink escapes plugin directory")
	}
	if !strings.Contains(err.Error(), "escapes plugin directory") {
		t.Errorf("expected error to mention 'escapes plugin directory', got: %v", err)
	}
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
	if err := createTempExecutable(tmpDir + "/test-plugin"); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err := host.Load(ctx, manifest, tmpDir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

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
	if err != nil {
		t.Fatalf("DeliverEvent returned error: %v", err)
	}

	if len(emits) != 1 {
		t.Fatalf("expected 1 emit event, got %d", len(emits))
	}
	if emits[0].Stream != "room:123" {
		t.Errorf("expected stream 'room:123', got %q", emits[0].Stream)
	}
	if emits[0].Type != pluginpkg.EventTypeSay {
		t.Errorf("expected type 'say', got %q", emits[0].Type)
	}
}

func TestClose_KillsPlugins(t *testing.T) {
	host, mockClient := newMockHost(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	if err := createTempExecutable(tmpDir + "/test-plugin"); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err := host.Load(ctx, manifest, tmpDir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	err = host.Close(ctx)
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}

	if !mockClient.killed {
		t.Error("expected mock client to be killed on close")
	}
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
			if err := createTempExecutable(tmpDir + "/test-plugin"); err != nil {
				t.Fatalf("failed to create temp file: %v", err)
			}

			manifest := &plugin.Manifest{
				Name:    "test-plugin",
				Version: "1.0.0",
				Type:    plugin.TypeBinary,
				BinaryPlugin: &plugin.BinaryConfig{
					Executable: "test-plugin",
				},
			}

			if err := host.Load(ctx, manifest, tmpDir); err != nil {
				t.Fatalf("Load returned error: %v", err)
			}

			event := pluginpkg.Event{
				ID:        "evt-123",
				ActorKind: tt.actorKind,
			}

			_, err := host.DeliverEvent(ctx, "test-plugin", event)
			if err != nil {
				t.Errorf("DeliverEvent returned error: %v", err)
			}
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
	if err == nil {
		t.Fatal("expected error when BinaryPlugin is nil")
	}
	if !strings.Contains(err.Error(), "not a binary plugin") {
		t.Errorf("expected error to mention 'not a binary plugin', got: %v", err)
	}
}

func TestLoad_NilManifest(t *testing.T) {
	enforcer := capability.NewEnforcer()
	host := NewHost(enforcer)
	ctx := context.Background()
	tmpDir := t.TempDir()

	err := host.Load(ctx, nil, tmpDir)
	if err == nil {
		t.Fatal("expected error when manifest is nil")
	}
	if !strings.Contains(err.Error(), "manifest cannot be nil") {
		t.Errorf("expected error to mention 'manifest cannot be nil', got: %v", err)
	}
}

func TestLoad_EmptyPluginName(t *testing.T) {
	host, _ := newMockHost(t)
	ctx := context.Background()
	tmpDir := t.TempDir()
	if err := createTempExecutable(filepath.Join(tmpDir, "test-plugin")); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	manifest := &plugin.Manifest{
		Name:    "", // Empty name
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err := host.Load(ctx, manifest, tmpDir)
	if err == nil {
		t.Fatal("expected error for empty plugin name")
	}
	if !strings.Contains(err.Error(), "plugin name cannot be empty") {
		t.Errorf("expected error to mention 'plugin name cannot be empty', got: %v", err)
	}
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
	if err := createTempExecutable(tmpDir + "/test-plugin"); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
			Executable: "test-plugin",
		},
	}

	err := host.Load(ctx, manifest, tmpDir)
	if err == nil {
		t.Fatal("expected error when plugin does not implement PluginClient")
	}
	if !strings.Contains(err.Error(), "does not implement PluginClient") {
		t.Errorf("expected error to mention 'does not implement PluginClient', got: %v", err)
	}
	if !mockClient.killed {
		t.Error("expected client to be killed after type assertion failure")
	}
}
