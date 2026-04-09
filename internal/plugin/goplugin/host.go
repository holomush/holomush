// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package goplugin provides a Host implementation for binary plugins
// using HashiCorp's go-plugin system over gRPC.
package goplugin

import (
	"context"
	cryptotls "crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	hashiplug "github.com/hashicorp/go-plugin"
	"github.com/samber/oops"
	"google.golang.org/grpc"

	plugins "github.com/holomush/holomush/internal/plugin"
	tlscerts "github.com/holomush/holomush/internal/tls"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// DefaultEventTimeout is the default timeout for plugin event handling.
const DefaultEventTimeout = 5 * time.Second

// Sentinel errors for programmatic error checking.
var (
	// ErrHostClosed is returned when operations are attempted on a closed host.
	ErrHostClosed = errors.New("host is closed")
	// ErrPluginNotLoaded is returned when operating on a plugin that isn't loaded.
	ErrPluginNotLoaded = errors.New("plugin not loaded")
	// ErrPluginAlreadyLoaded is returned when loading a plugin that's already loaded.
	ErrPluginAlreadyLoaded = errors.New("plugin already loaded")
)

// Compile-time interface checks.
var (
	_ plugins.Host                      = (*Host)(nil)
	_ plugins.ServiceConnProvider       = (*Host)(nil)
	_ plugins.AttributeResolverProvider = (*Host)(nil)
)

// PluginClient wraps go-plugin client for testability.
type PluginClient interface {
	// Client returns the gRPC client protocol.
	Client() (hashiplug.ClientProtocol, error)
	// Kill terminates the plugin process.
	Kill()
}

// ClientFactory creates plugin clients.
type ClientFactory interface {
	// NewClient creates a client for the given executable path.
	NewClient(execPath string) PluginClient
}

// DefaultClientFactory creates real go-plugin clients.
type DefaultClientFactory struct{}

// NewClient creates a real go-plugin client.
func (f *DefaultClientFactory) NewClient(execPath string) PluginClient {
	cmd := exec.Command(execPath) // #nosec G204 -- nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- execPath resolved from plugin manifest; manifests validated during discovery (symlink-resolved, path-contained, executable-checked)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
	}
	return hashiplug.NewClient(&hashiplug.ClientConfig{
		HandshakeConfig:  HandshakeConfig,
		Plugins:          PluginMap,
		Cmd:              cmd,
		AllowedProtocols: []hashiplug.Protocol{hashiplug.ProtocolGRPC},
	})
}

// HostOption configures a Host during construction.
type HostOption func(*Host)

// WithSchemaProvisioner configures the host to provision per-plugin Postgres
// schemas for binary plugins that declare storage: postgres.
func WithSchemaProvisioner(p *plugins.SchemaProvisioner) HostOption {
	return func(h *Host) { h.schemaProvisioner = p }
}

// WithCA configures the host to use the given CA for mTLS with plugins.
// A host client cert is generated at construction time.
func WithCA(ca *tlscerts.CA, gameID string) HostOption {
	return func(h *Host) {
		h.ca = ca
		h.gameID = gameID
	}
}

// WithServiceRegistry configures the host to register plugin-provided services.
func WithServiceRegistry(r *plugins.ServiceRegistry) HostOption {
	return func(h *Host) { h.registry = r }
}

// Host manages binary plugins via HashiCorp go-plugins.
type Host struct {
	clientFactory     ClientFactory
	schemaProvisioner *plugins.SchemaProvisioner
	registry          *plugins.ServiceRegistry
	ca                *tlscerts.CA
	gameID            string
	hostClientCert    *tlscerts.ClientCert
	plugins           map[string]*loadedPlugin
	mu                sync.RWMutex
	closed            bool
}

// loadedPlugin holds state for a single loaded binary plugin.
type loadedPlugin struct {
	manifest *plugins.Manifest
	client   PluginClient
	plugin   pluginv1.PluginServiceClient
	conn     grpc.ClientConnInterface // underlying gRPC conn to the plugin process
	certDir  string                   // temp cert directory, cleaned up on unload
	broker   *hashiplug.GRPCBroker    // broker for service injection, nil if factory-mocked
}

// NewHost creates a new binary plugin host.
func NewHost(opts ...HostOption) *Host {
	return NewHostWithFactory(&DefaultClientFactory{}, opts...)
}

// NewHostWithFactory creates a host with a custom client factory (for testing).
// Panics if factory is nil.
func NewHostWithFactory(factory ClientFactory, opts ...HostOption) *Host {
	if factory == nil {
		panic("goplugin: factory cannot be nil")
	}
	h := &Host{
		clientFactory: factory,
		plugins:       make(map[string]*loadedPlugin),
	}
	for _, opt := range opts {
		opt(h)
	}
	if h.ca != nil {
		cert, certErr := tlscerts.GenerateClientCert(h.ca, "plugin-host")
		if certErr != nil {
			panic("goplugin: failed to generate host client cert: " + certErr.Error())
		}
		h.hostClientCert = cert
	}
	return h
}

// Load initializes a plugin from its manifest.
func (h *Host) Load(ctx context.Context, manifest *plugins.Manifest, dir string) error {
	// Check context before expensive operations
	if err := ctx.Err(); err != nil {
		return oops.In("goplugin").With("operation", "load").Wrap(err)
	}

	if manifest == nil {
		return oops.In("goplugin").With("operation", "load").New("manifest cannot be nil")
	}

	if manifest.Name == "" {
		return oops.In("goplugin").With("operation", "load").New("plugin name cannot be empty")
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return ErrHostClosed
	}

	if _, ok := h.plugins[manifest.Name]; ok {
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").Wrap(ErrPluginAlreadyLoaded)
	}

	if manifest.BinaryPlugin == nil {
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").New("not a binary plugin")
	}

	// Resolve the binary path. Check platform-specific subdirectory first
	// (e.g., linux-amd64/core-scenes), fall back to direct path for backward
	// compatibility (e.g., core-scenes).
	platformDir := runtime.GOOS + "-" + runtime.GOARCH
	execPath := filepath.Join(dir, platformDir, manifest.BinaryPlugin.Executable)
	if _, statErr := os.Stat(execPath); os.IsNotExist(statErr) {
		execPath = filepath.Join(dir, manifest.BinaryPlugin.Executable)
	}

	// Verify resolved path is within the plugin directory (prevent path traversal)
	// Use EvalSymlinks to resolve symlinks and prevent symlink-based escapes
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").With("dir", dir).Hint("cannot resolve plugin directory").Wrap(err)
	}
	realExec, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		if os.IsNotExist(err) {
			return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").With("path", execPath).Hint("plugin executable not found").Wrap(err)
		}
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").With("path", execPath).Hint("cannot resolve executable path").Wrap(err)
	}
	// Use filepath.Rel for robust cross-platform path containment check
	rel, err := filepath.Rel(realDir, realExec)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").With("executable", manifest.BinaryPlugin.Executable).New("plugin executable path escapes plugin directory")
	}

	// Use realExec (resolved symlink) for stat and client to prevent TOCTOU attacks
	info, err := os.Stat(realExec)
	if err != nil {
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").With("path", realExec).Hint("cannot access plugin executable").Wrap(err)
	}
	// Check execute permission (user, group, or other)
	if info.Mode()&0o111 == 0 {
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").With("path", realExec).New("plugin executable not executable")
	}

	// Generate per-plugin mTLS certificates when a CA is configured.
	var pluginCertEnv []string
	var hostTLSConfig *cryptotls.Config
	var certDir string

	if h.ca != nil {
		serverCert, certErr := tlscerts.GenerateServerCert(h.ca, h.gameID, "plugin-"+manifest.Name)
		if certErr != nil {
			return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "generate_cert").Wrap(certErr)
		}

		tmpCertDir, tmpErr := os.MkdirTemp("", "holomush-plugin-certs-*")
		if tmpErr != nil {
			return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "cert_tmpdir").Wrap(tmpErr)
		}
		certDir = tmpCertDir

		if saveErr := tlscerts.SaveCertificates(tmpCertDir, h.ca, serverCert); saveErr != nil {
			_ = os.RemoveAll(tmpCertDir) //nolint:errcheck // best-effort cleanup
			return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "save_cert").Wrap(saveErr)
		}

		pluginCertEnv = []string{
			"HOLOMUSH_PLUGIN_CERT=" + filepath.Join(tmpCertDir, "plugin-"+manifest.Name+".crt"),
			"HOLOMUSH_PLUGIN_KEY=" + filepath.Join(tmpCertDir, "plugin-"+manifest.Name+".key"),
			"HOLOMUSH_CA_CERT=" + filepath.Join(tmpCertDir, "root-ca.crt"),
		}

		hostTLSConfig = buildHostTLSConfig(h.ca, h.hostClientCert, manifest.Name)
	}

	// Create a per-plugin GRPCPlugin instance to capture the GRPCBroker
	// from the go-plugin handshake. This enables service injection via broker
	// proxies for plugins that declare required services.
	grpcPlugin := &GRPCPlugin{}
	pluginMap := map[string]hashiplug.Plugin{"plugin": grpcPlugin}

	// Create the plugin client. In production (DefaultClientFactory), we create
	// the client directly to use the per-plugin GRPCPlugin (for broker capture).
	// Test mocks use the factory path (no broker needed).
	var client PluginClient
	switch h.clientFactory.(type) {
	case *DefaultClientFactory:
		cmd := exec.Command(realExec) // #nosec G204 -- nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command, go.grpc.command-injection.grpc-command-injection.grpc-http-command-injection-taint -- realExec resolved from plugin manifest; manifests validated during discovery (symlink-resolved, path-contained, executable-checked)
		cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, pluginCertEnv...)
		clientConfig := &hashiplug.ClientConfig{
			HandshakeConfig:  HandshakeConfig,
			Plugins:          pluginMap,
			Cmd:              cmd,
			AllowedProtocols: []hashiplug.Protocol{hashiplug.ProtocolGRPC},
		}
		if hostTLSConfig != nil {
			clientConfig.TLSConfig = hostTLSConfig
		}
		client = hashiplug.NewClient(clientConfig)
	default:
		client = h.clientFactory.NewClient(realExec)
	}

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "connect").Wrap(err)
	}

	// Capture the underlying gRPC connection for service registration.
	// The concrete type behind ClientProtocol is *hashiplug.GRPCClient which
	// exposes a public Conn field. We also accept any type implementing a
	// Conn() accessor (used by test mocks).
	var pluginConn grpc.ClientConnInterface
	switch c := rpcClient.(type) {
	case *hashiplug.GRPCClient:
		pluginConn = c.Conn
	case interface {
		Conn() grpc.ClientConnInterface
	}:
		pluginConn = c.Conn()
	}

	raw, err := rpcClient.Dispense("plugin")
	if err != nil {
		client.Kill()
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "dispense").Wrap(err)
	}

	pluginClient, ok := raw.(pluginv1.PluginServiceClient)
	if !ok {
		client.Kill()
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").New("plugin does not implement PluginClient")
	}

	// Start broker proxies for required services. Each required service gets
	// a broker ID that the plugin can use to dial back to the host.
	requiredServices := make(map[string]string)
	if len(manifest.Requires) > 0 && grpcPlugin.broker != nil && h.registry != nil {
		var nextBrokerID uint32 = 1
		for _, svcName := range manifest.Requires {
			svc, resolveErr := h.registry.Resolve(svcName)
			if resolveErr != nil {
				client.Kill()
				if certDir != "" {
					_ = os.RemoveAll(certDir) //nolint:errcheck // best-effort cleanup
				}
				return oops.Code("PLUGIN_SERVICE_NOT_FOUND").
					With("plugin", manifest.Name).
					With("service", svcName).
					Wrap(resolveErr)
			}

			brokerID := nextBrokerID
			nextBrokerID++

			proxyFactory := NewBrokerProxy(svc.Conn, manifest.Name)
			go grpcPlugin.broker.AcceptAndServe(brokerID, proxyFactory)

			requiredServices[svcName] = fmt.Sprintf("broker:%d", brokerID)
			slog.Info("started broker proxy for required service",
				"plugin", manifest.Name,
				"service", svcName,
				"broker_id", brokerID,
			)
		}
	}

	// Call Init on plugins that need service injection (storage or requires).
	if len(manifest.Requires) > 0 || manifest.Storage == plugins.StoragePostgres {
		initReq := &pluginv1.InitRequest{
			Config: &pluginv1.ServiceConfig{
				RequiredServices: requiredServices,
			},
		}

		if manifest.Storage == plugins.StoragePostgres && h.schemaProvisioner != nil {
			connStr, provErr := h.schemaProvisioner.ProvisionSchema(ctx, manifest.Name)
			if provErr != nil {
				client.Kill()
				if certDir != "" {
					_ = os.RemoveAll(certDir) //nolint:errcheck // best-effort cleanup
				}
				return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "provision_schema").Wrap(provErr)
			}
			initReq.Config.ConnectionString = connStr
		}

		if _, initErr := pluginClient.Init(ctx, initReq); initErr != nil {
			client.Kill()
			if certDir != "" {
				_ = os.RemoveAll(certDir) //nolint:errcheck // best-effort cleanup
			}
			return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "init").Wrap(initErr)
		}
	}

	h.plugins[manifest.Name] = &loadedPlugin{
		manifest: manifest,
		client:   client,
		plugin:   pluginClient,
		conn:     pluginConn,
		certDir:  certDir,
		broker:   grpcPlugin.broker,
	}

	return nil
}

// Unload tears down a plugin.
func (h *Host) Unload(_ context.Context, name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return ErrHostClosed
	}

	p, ok := h.plugins[name]
	if !ok {
		return oops.In("goplugin").With("plugin", name).With("operation", "unload").Wrap(ErrPluginNotLoaded)
	}

	if p.client != nil {
		p.client.Kill()
	}

	if p.certDir != "" {
		_ = os.RemoveAll(p.certDir) //nolint:errcheck // best-effort cleanup
	}

	delete(h.plugins, name)
	return nil
}

// DeliverEvent sends an event to a plugin and returns response events.
//
// Note: The RLock is released before making the gRPC call to avoid serializing
// all plugin calls. If Close() or Unload() is called concurrently, the gRPC
// call will fail gracefully when the plugin process is killed. This is the
// standard trade-off in go-plugin based systems.
func (h *Host) DeliverEvent(ctx context.Context, name string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, ErrHostClosed
	}
	p, ok := h.plugins[name]
	h.mu.RUnlock()

	if !ok {
		return nil, oops.In("goplugin").With("plugin", name).With("operation", "deliver_event").Wrap(ErrPluginNotLoaded)
	}

	// Log warning for unrecognized actor kinds (useful for debugging)
	actorKind := event.ActorKind.String()
	if actorKind == "unknown" {
		slog.Warn("unrecognized actor kind, using 'unknown'",
			"kind", int(event.ActorKind))
	}

	protoEvent := &pluginv1.Event{
		Id:        event.ID,
		Stream:    event.Stream,
		Type:      string(event.Type),
		Timestamp: event.Timestamp,
		ActorKind: actorKind,
		ActorId:   event.ActorID,
		Payload:   event.Payload,
	}

	callCtx, cancel := context.WithTimeout(ctx, DefaultEventTimeout)
	defer cancel()

	resp, err := p.plugin.HandleEvent(callCtx, &pluginv1.HandleEventRequest{Event: protoEvent})
	if err != nil {
		return nil, oops.In("goplugin").With("plugin", name).With("operation", "handle_event").Wrap(err)
	}

	emits := make([]pluginsdk.EmitEvent, len(resp.GetEmitEvents()))
	for i, e := range resp.GetEmitEvents() {
		emits[i] = pluginsdk.EmitEvent{
			Stream:  e.GetStream(),
			Type:    pluginsdk.EventType(e.GetType()),
			Payload: e.GetPayload(),
		}
	}

	return emits, nil
}

// DeliverCommand sends a command to a binary plugin and returns the response.
//
// The RLock is released before the gRPC call (same pattern as DeliverEvent).
func (h *Host) DeliverCommand(ctx context.Context, name string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, ErrHostClosed
	}
	p, ok := h.plugins[name]
	h.mu.RUnlock()

	if !ok {
		return nil, oops.In("goplugin").With("plugin", name).With("operation", "deliver_command").Wrap(ErrPluginNotLoaded)
	}

	protoReq := &pluginv1.HandleCommandRequest{
		Command: &pluginv1.CommandRequest{
			Command:       cmd.Command,
			Args:          cmd.Args,
			RawInput:      cmd.InvokedAs,
			CharacterId:   cmd.CharacterID,
			CharacterName: cmd.CharacterName,
			LocationId:    cmd.LocationID,
			SessionId:     cmd.SessionID,
			PlayerId:      cmd.PlayerID,
		},
	}

	callCtx, cancel := context.WithTimeout(ctx, DefaultEventTimeout)
	defer cancel()

	resp, err := p.plugin.HandleCommand(callCtx, protoReq)
	if err != nil {
		return nil, oops.In("goplugin").With("plugin", name).With("operation", "handle_command").Wrap(err)
	}

	return protoCommandResponseToSDK(resp.GetResponse()), nil
}

// protoCommandResponseToSDK converts a proto CommandResponse to an SDK CommandResponse.
func protoCommandResponseToSDK(r *pluginv1.CommandResponse) *pluginsdk.CommandResponse {
	if r == nil {
		return &pluginsdk.CommandResponse{}
	}

	events := make([]pluginsdk.EmitEvent, len(r.GetEvents()))
	for i, e := range r.GetEvents() {
		events[i] = pluginsdk.EmitEvent{
			Stream:  e.GetStream(),
			Type:    pluginsdk.EventType(e.GetType()),
			Payload: e.GetPayload(),
		}
	}

	auditHints := make([]pluginsdk.AuditHint, len(r.GetAuditHints()))
	for i, h := range r.GetAuditHints() {
		auditHints[i] = pluginsdk.AuditHint{
			ID:              h.GetId(),
			Name:            h.GetName(),
			Message:         h.GetMessage(),
			Effect:          protoAuditEffectToSDK(h.GetEffect()),
			ActionQualifier: h.GetActionQualifier(),
			Resource:        h.GetResource(),
			Attributes:      h.GetAttributes(),
		}
	}

	return &pluginsdk.CommandResponse{
		Status:     protoCommandStatusToSDK(r.GetStatus()),
		Output:     r.GetOutput(),
		Events:     events,
		AuditHints: auditHints,
	}
}

// protoAuditEffectToSDK converts a proto AuditEffect enum to the SDK
// AuditEffect string. Unspecified and unknown values return an empty SDK
// effect so the dispatcher's extractAuditHints can detect the malformed
// hint and drop it with a warning — this surfaces plugin misbehavior to
// operators rather than silently coercing to allow/deny.
func protoAuditEffectToSDK(e pluginv1.AuditEffect) pluginsdk.AuditEffect {
	switch e {
	case pluginv1.AuditEffect_AUDIT_EFFECT_DENY:
		return pluginsdk.AuditEffectDeny
	case pluginv1.AuditEffect_AUDIT_EFFECT_ALLOW:
		return pluginsdk.AuditEffectAllow
	default:
		// Unspecified or unknown — return empty so the dispatcher's
		// extractAuditHints unknown-effect path drops the hint with a
		// warning, surfacing the misbehavior to operators.
		return ""
	}
}

// protoCommandStatusToSDK converts a proto CommandStatus to an SDK CommandStatus.
func protoCommandStatusToSDK(s pluginv1.CommandStatus) pluginsdk.CommandStatus {
	switch s {
	case pluginv1.CommandStatus_COMMAND_STATUS_OK:
		return pluginsdk.CommandOK
	case pluginv1.CommandStatus_COMMAND_STATUS_ERROR:
		return pluginsdk.CommandError
	case pluginv1.CommandStatus_COMMAND_STATUS_FAILURE:
		return pluginsdk.CommandFailure
	case pluginv1.CommandStatus_COMMAND_STATUS_FATAL:
		return pluginsdk.CommandFatal
	default:
		return pluginsdk.CommandOK
	}
}

// AttributeResolverClient returns the AttributeResolver gRPC client for a loaded plugin.
// Returns nil if the plugin is not loaded or doesn't support attribute resolution.
func (h *Host) AttributeResolverClient(pluginName string) pluginv1.AttributeResolverServiceClient {
	h.mu.RLock()
	defer h.mu.RUnlock()
	lp, ok := h.plugins[pluginName]
	if !ok || lp.conn == nil {
		return nil
	}
	return pluginv1.NewAttributeResolverServiceClient(lp.conn)
}

// PluginConn returns the gRPC client connection for the named plugin.
// This enables the manager to register plugin-provided services in the
// ServiceRegistry after loading.
func (h *Host) PluginConn(name string) (grpc.ClientConnInterface, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.closed {
		return nil, ErrHostClosed
	}

	p, ok := h.plugins[name]
	if !ok {
		return nil, oops.In("goplugin").With("plugin", name).With("operation", "plugin_conn").Wrap(ErrPluginNotLoaded)
	}

	if p.conn == nil {
		return nil, oops.In("goplugin").With("plugin", name).With("operation", "plugin_conn").New("plugin has no gRPC connection")
	}

	return p.conn, nil
}

// Plugins returns names of all loaded plugins.
func (h *Host) Plugins() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.closed {
		return nil
	}

	names := make([]string, 0, len(h.plugins))
	for name := range h.plugins {
		names = append(names, name)
	}
	return names
}

// QuerySessionStreams calls the plugin's QuerySessionStreams RPC.
// Returns nil if the plugin has no streams to contribute.
func (h *Host) QuerySessionStreams(ctx context.Context, name string, req plugins.SessionStreamsRequest) ([]string, error) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, ErrHostClosed
	}
	p, ok := h.plugins[name]
	h.mu.RUnlock()

	if !ok {
		return nil, oops.In("goplugin").With("plugin", name).With("operation", "query_session_streams").Wrap(ErrPluginNotLoaded)
	}

	callCtx, cancel := context.WithTimeout(ctx, DefaultEventTimeout)
	defer cancel()

	resp, err := p.plugin.QuerySessionStreams(callCtx, &pluginv1.QuerySessionStreamsRequest{
		CharacterId: req.CharacterID,
		PlayerId:    req.PlayerID,
		SessionId:   req.SessionID,
	})
	if err != nil {
		return nil, oops.In("goplugin").With("plugin", name).With("operation", "query_session_streams").Wrap(err)
	}
	if resp.GetError() != "" {
		return nil, oops.In("goplugin").With("plugin", name).With("operation", "query_session_streams").New(resp.GetError())
	}
	return resp.GetStreams(), nil
}

// Close shuts down the host and all plugins.
func (h *Host) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil
	}

	for _, p := range h.plugins {
		if p.client != nil {
			p.client.Kill()
		}
		if p.certDir != "" {
			_ = os.RemoveAll(p.certDir) //nolint:errcheck // best-effort cleanup
		}
	}

	h.closed = true
	clear(h.plugins)
	return nil
}

// buildHostTLSConfig creates a TLS config for the host to connect to a plugin
// as a gRPC client with mTLS. The host presents its client cert and verifies
// the plugin's server cert against the CA.
func buildHostTLSConfig(ca *tlscerts.CA, clientCert *tlscerts.ClientCert, pluginName string) *cryptotls.Config {
	caPool := x509.NewCertPool()
	caPool.AddCert(ca.Certificate)

	clientTLSCert := cryptotls.Certificate{
		Certificate: [][]byte{clientCert.Certificate.Raw},
		PrivateKey:  clientCert.PrivateKey,
	}

	return &cryptotls.Config{
		Certificates: []cryptotls.Certificate{clientTLSCert},
		RootCAs:      caPool,
		ServerName:   "holomush-plugin-" + pluginName,
		MinVersion:   cryptotls.VersionTLS13,
	}
}
