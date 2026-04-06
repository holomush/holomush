# GRPCBroker Service Injection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable binary plugins to call host services (WorldService, etc.) through go-plugin's GRPCBroker with mTLS using the existing HoloMUSH CA.

**Architecture:** The host starts a reverse gRPC server per required service on the GRPCBroker, transparently proxying calls to the ServiceRegistry's `ClientConnInterface`. Per-plugin mTLS certs are generated from the game CA at load time. The plugin SDK provides helpers to parse broker IDs and dial services.

**Tech Stack:** Go, hashicorp/go-plugin (GRPCBroker), gRPC, x509/TLS (internal/tls), OpenTelemetry

**Spec:** `docs/superpowers/specs/2026-04-06-grpcbroker-service-injection-design.md`

---

## Task 1: Broker Proxy (transparent gRPC proxy for broker)

**Files:**

- Create: `internal/plugin/goplugin/broker_proxy.go`
- Create: `internal/plugin/goplugin/broker_proxy_test.go`

This is the core proxy that bridges a `grpc.ClientConnInterface` to a gRPC
server for use with `broker.AcceptAndServe`. It reuses the stream proxy logic
from `internal/plugin/grpc_proxy.go`.

- [ ] **Step 1: Write failing test for NewBrokerProxy**

Create `internal/plugin/goplugin/broker_proxy_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestNewBrokerProxyReturnsServerFactory(t *testing.T) {
	// Use a nil conn — we're testing the factory shape, not the proxy behavior.
	// Real proxy tests happen at the integration level with actual gRPC streams.
	factory := NewBrokerProxy(nil, "test-plugin")
	require.NotNil(t, factory)

	server := factory([]grpc.ServerOption{})
	require.NotNil(t, server)

	// The server should have an unknown service handler installed
	// (we can't directly assert this, but it should not panic on creation)
	server.Stop()
}

func TestNewBrokerProxyIncludesPluginNameInServerOptions(t *testing.T) {
	factory := NewBrokerProxy(nil, "core-scenes")
	assert.NotNil(t, factory)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Volumes/Code/github.com/holomush/holomush_worktrees/plugin-arch && task test -- -run TestNewBrokerProxy ./internal/plugin/goplugin/`
Expected: FAIL — `NewBrokerProxy` undefined

- [ ] **Step 3: Implement NewBrokerProxy**

Create `internal/plugin/goplugin/broker_proxy.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"log/slog"
	"time"

	plugins "github.com/holomush/holomush/internal/plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NewBrokerProxy creates a gRPC server factory compatible with
// broker.AcceptAndServe. The returned server transparently proxies all
// RPCs to the given connection using an UnknownServiceHandler.
//
// This reuses the same bidirectional stream proxy logic as
// GRPCServiceProxy in grpc_proxy.go (proxyStreams + rawMessage).
func NewBrokerProxy(conn grpc.ClientConnInterface, pluginName string) func([]grpc.ServerOption) *grpc.Server {
	return func(opts []grpc.ServerOption) *grpc.Server {
		handler := grpc.UnknownServiceHandler(func(_ interface{}, stream grpc.ServerStream) error {
			method, ok := grpc.MethodFromServerStream(stream)
			if !ok {
				return status.Error(codes.Internal, "failed to get method from stream")
			}

			if conn == nil {
				return status.Errorf(codes.Unavailable, "no connection for proxied service")
			}

			start := time.Now()
			clientStream, err := conn.NewStream(
				stream.Context(),
				&grpc.StreamDesc{ServerStreams: true, ClientStreams: true},
				method,
			)
			if err != nil {
				slog.Error("broker proxy: failed to create upstream stream",
					"plugin", pluginName, "method", method, "error", err)
				return status.Errorf(codes.Internal, "broker proxy: upstream unavailable")
			}

			proxyErr := plugins.ProxyStreams(stream, clientStream)

			slog.Debug("broker proxy: call completed",
				"plugin", pluginName,
				"method", method,
				"duration", time.Since(start),
				"error", proxyErr,
			)

			return proxyErr
		})

		allOpts := append([]grpc.ServerOption{handler}, opts...)
		return grpc.NewServer(allOpts...)
	}
}
```

**Important:** This requires extracting `proxyStreams` and `rawMessage` from
`internal/plugin/grpc_proxy.go` into exported names (`ProxyStreams`,
`RawMessage`) so `goplugin` package can import them. The `grpc_proxy.go`
file is in `internal/plugin` (package `plugins`), and `broker_proxy.go` is in
`internal/plugin/goplugin`. This is fine — child packages can import parent.

- [ ] **Step 4: Export ProxyStreams from grpc_proxy.go**

In `internal/plugin/grpc_proxy.go`, rename `proxyStreams` to `ProxyStreams`
and `rawMessage` to `RawMessage`. Update all references within that file
(the `GRPCServiceProxy.streamHandler` method calls `proxyStreams`).

- [ ] **Step 5: Run tests to verify everything passes**

Run: `cd /Volumes/Code/github.com/holomush/holomush_worktrees/plugin-arch && task test -- -run TestNewBrokerProxy ./internal/plugin/goplugin/`
Expected: PASS

Run: `cd /Volumes/Code/github.com/holomush/holomush_worktrees/plugin-arch && task test -- ./internal/plugin/`
Expected: PASS (existing grpc_proxy tests still pass with renamed exports)

- [ ] **Step 6: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(plugin): add broker proxy for transparent gRPC forwarding

NewBrokerProxy creates a gRPC server factory for broker.AcceptAndServe
that transparently proxies all RPCs to a given ClientConnInterface.
Exports ProxyStreams from grpc_proxy.go for reuse.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Plugin SDK broker helpers

**Files:**

- Create: `pkg/plugin/broker.go`
- Create: `pkg/plugin/broker_test.go`

Plugin-side helpers for parsing broker service maps and dialing services.

- [ ] **Step 1: Write failing tests**

Create `pkg/plugin/broker_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBrokerServicesExtractsBrokerIDs(t *testing.T) {
	services := map[string]string{
		"holomush.world.v1.WorldService": "broker:42",
		"holomush.scene.v1.SceneService": "broker:7",
	}

	result, err := ParseBrokerServices(services)
	require.NoError(t, err)

	assert.Equal(t, uint32(42), result["holomush.world.v1.WorldService"])
	assert.Equal(t, uint32(7), result["holomush.scene.v1.SceneService"])
}

func TestParseBrokerServicesRejectsInvalidFormat(t *testing.T) {
	services := map[string]string{
		"holomush.world.v1.WorldService": "invalid:format",
	}

	_, err := ParseBrokerServices(services)
	require.Error(t, err)
}

func TestParseBrokerServicesRejectsMissingPrefix(t *testing.T) {
	services := map[string]string{
		"holomush.world.v1.WorldService": "42",
	}

	_, err := ParseBrokerServices(services)
	require.Error(t, err)
}

func TestParseBrokerServicesHandlesEmptyMap(t *testing.T) {
	result, err := ParseBrokerServices(map[string]string{})
	require.NoError(t, err)
	assert.Empty(t, result)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Volumes/Code/github.com/holomush/holomush_worktrees/plugin-arch && task test -- -run TestParseBrokerServices ./pkg/plugin/`
Expected: FAIL — `ParseBrokerServices` undefined

- [ ] **Step 3: Implement broker helpers**

Create `pkg/plugin/broker.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"fmt"
	"strconv"
	"strings"
)

const brokerPrefix = "broker:"

// ParseBrokerServices parses the required_services map from InitRequest
// into a map of service name to broker ID. Each value must have the format
// "broker:<uint32>".
func ParseBrokerServices(services map[string]string) (map[string]uint32, error) {
	result := make(map[string]uint32, len(services))
	for name, value := range services {
		if !strings.HasPrefix(value, brokerPrefix) {
			return nil, fmt.Errorf("service %q: expected %q prefix, got %q", name, brokerPrefix, value)
		}
		idStr := strings.TrimPrefix(value, brokerPrefix)
		id, err := strconv.ParseUint(idStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("service %q: invalid broker ID %q: %w", name, idStr, err)
		}
		result[name] = uint32(id)
	}
	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Volumes/Code/github.com/holomush/holomush_worktrees/plugin-arch && task test -- -run TestParseBrokerServices ./pkg/plugin/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(plugin/sdk): add ParseBrokerServices helper for service injection

Parses required_services map from InitRequest into broker IDs.
Format: 'broker:<uint32>' per entry. Plugin authors use this to
get broker IDs for dialing required services.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Plugin mTLS cert generation and SDK TLS support

**Files:**

- Modify: `internal/plugin/goplugin/host.go` (Host struct, NewHost, DefaultClientFactory)
- Modify: `pkg/plugin/sdk.go` (Serve, ServeWithServices)
- Modify: `pkg/plugin/service.go` (ServeWithServices)

This task adds mTLS support to the plugin transport without changing the
service injection flow (that's Task 4).

- [ ] **Step 1: Add CA and gameID to Host struct**

In `internal/plugin/goplugin/host.go`, add fields to `Host` and a new option:

```go
import (
	// ... existing imports ...
	tlscerts "github.com/holomush/holomush/internal/tls"
)

type Host struct {
	clientFactory     ClientFactory
	schemaProvisioner *plugins.SchemaProvisioner
	registry          *plugins.ServiceRegistry
	ca                *tlscerts.CA
	gameID            string
	hostClientCert    *tlscerts.ClientCert // generated once at construction
	plugins           map[string]*loadedPlugin
	mu                sync.RWMutex
	closed            bool
}

// WithCA configures the host with a CA for plugin mTLS.
func WithCA(ca *tlscerts.CA, gameID string) HostOption {
	return func(h *Host) {
		h.ca = ca
		h.gameID = gameID
	}
}

// WithServiceRegistry configures the host with a service registry for
// resolving required services.
func WithServiceRegistry(r *plugins.ServiceRegistry) HostOption {
	return func(h *Host) { h.registry = r }
}
```

In `NewHostWithFactory`, after applying options, generate the host client cert
if CA is set:

```go
func NewHostWithFactory(factory ClientFactory, opts ...HostOption) *Host {
	// ... existing code ...
	for _, opt := range opts {
		opt(h)
	}
	if h.ca != nil {
		cert, err := tlscerts.GenerateClientCert(h.ca, "holomush-plugin-host")
		if err != nil {
			panic("goplugin: failed to generate host client cert: " + err.Error())
		}
		h.hostClientCert = cert
	}
	return h
}
```

- [ ] **Step 2: Modify DefaultClientFactory to accept TLS config**

Change `DefaultClientFactory` to carry optional TLS config and cert paths for the plugin env:

```go
type DefaultClientFactory struct {
	tlsConfig *tls.Config  // host-side TLS config (client role)
	certEnv   []string     // env vars for plugin subprocess: HOLOMUSH_PLUGIN_CERT, etc.
}

func (f *DefaultClientFactory) NewClient(execPath string) PluginClient {
	cmd := exec.Command(execPath)
	cmd.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
	}, f.certEnv...)

	clientConfig := &hashiplug.ClientConfig{
		HandshakeConfig:  HandshakeConfig,
		Plugins:          PluginMap,
		Cmd:              cmd,
		AllowedProtocols: []hashiplug.Protocol{hashiplug.ProtocolGRPC},
	}
	if f.tlsConfig != nil {
		clientConfig.TLSConfig = f.tlsConfig
	}
	return hashiplug.NewClient(clientConfig)
}
```

- [ ] **Step 3: Generate per-plugin certs in Load()**

In `Host.Load()`, after resolving the binary path and before creating the
client, add cert generation:

```go
// Generate per-plugin mTLS certs if CA is configured.
var pluginCertEnv []string
var hostTLSConfig *tls.Config
if h.ca != nil {
	serverCert, certErr := tlscerts.GenerateServerCert(h.ca, h.gameID, "plugin-"+manifest.Name)
	if certErr != nil {
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "generate_cert").Wrap(certErr)
	}

	// Write certs to temp directory
	tmpCertDir, tmpErr := os.MkdirTemp("", "holomush-plugin-certs-*")
	if tmpErr != nil {
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "cert_tmpdir").Wrap(tmpErr)
	}

	if saveErr := tlscerts.SaveCertificates(tmpCertDir, h.ca, serverCert); saveErr != nil {
		os.RemoveAll(tmpCertDir)
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "save_cert").Wrap(saveErr)
	}

	pluginCertEnv = []string{
		"HOLOMUSH_PLUGIN_CERT=" + filepath.Join(tmpCertDir, "plugin-"+manifest.Name+".crt"),
		"HOLOMUSH_PLUGIN_KEY=" + filepath.Join(tmpCertDir, "plugin-"+manifest.Name+".key"),
		"HOLOMUSH_CA_CERT=" + filepath.Join(tmpCertDir, "root-ca.crt"),
	}

	// Build host-side TLS config (host is the gRPC client)
	hostTLSConfig = buildHostTLSConfig(h.ca, h.hostClientCert, h.gameID, manifest.Name)
}
```

Add a helper for the host TLS config:

```go
func buildHostTLSConfig(ca *tlscerts.CA, clientCert *tlscerts.ClientCert, gameID, pluginName string) *tls.Config {
	caPool := x509.NewCertPool()
	caPool.AddCert(ca.Certificate)

	clientTLSCert := tls.Certificate{
		Certificate: [][]byte{clientCert.Certificate.Raw},
		PrivateKey:  clientCert.PrivateKey,
	}

	return &tls.Config{
		Certificates: []tls.Certificate{clientTLSCert},
		RootCAs:      caPool,
		ServerName:   "holomush-plugin-" + pluginName,
		MinVersion:   tls.VersionTLS13,
	}
}
```

Then use the per-plugin factory:

```go
// Create per-plugin client factory with TLS if configured.
pluginFactory := &DefaultClientFactory{
	tlsConfig: hostTLSConfig,
	certEnv:   pluginCertEnv,
}
client := pluginFactory.NewClient(realExec)
```

Store the cert dir on `loadedPlugin` for cleanup on unload:

```go
type loadedPlugin struct {
	manifest *plugins.Manifest
	client   PluginClient
	plugin   pluginv1.PluginServiceClient
	conn     grpc.ClientConnInterface
	certDir  string // temp cert directory, cleaned up on unload
}
```

In `Unload()`, add cert cleanup:

```go
if p.certDir != "" {
	os.RemoveAll(p.certDir)
}
```

- [ ] **Step 4: Add TLS to plugin SDK**

In `pkg/plugin/sdk.go`, modify `Serve` and `ServeWithServices` to load TLS
from env vars when present:

```go
func Serve(config *ServeConfig) {
	// ... existing validation ...
	serveConfig := &hashiplug.ServeConfig{
		HandshakeConfig: HandshakeConfig,
		Plugins: map[string]hashiplug.Plugin{
			"plugin": &grpcPlugin{handler: config.Handler},
		},
		GRPCServer: hashiplug.DefaultGRPCServer,
	}

	if tlsProvider := loadPluginTLSProvider(); tlsProvider != nil {
		serveConfig.TLSProvider = tlsProvider
	}

	hashiplug.Serve(serveConfig)
}
```

Add the TLS loader:

```go
import (
	cryptotls "crypto/tls"
	"crypto/x509"
	"os"
)

func loadPluginTLSProvider() func() (*cryptotls.Config, error) {
	certPath := os.Getenv("HOLOMUSH_PLUGIN_CERT")
	keyPath := os.Getenv("HOLOMUSH_PLUGIN_KEY")
	caPath := os.Getenv("HOLOMUSH_CA_CERT")

	if certPath == "" || keyPath == "" || caPath == "" {
		return nil
	}

	return func() (*cryptotls.Config, error) {
		cert, err := cryptotls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("load plugin cert: %w", err)
		}

		caCert, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read CA cert: %w", err)
		}

		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to add CA cert to pool")
		}

		return &cryptotls.Config{
			Certificates: []cryptotls.Certificate{cert},
			ClientCAs:    caPool,
			ClientAuth:   cryptotls.RequireAndVerifyClientCert,
			MinVersion:   cryptotls.VersionTLS13,
		}, nil
	}
}
```

Apply the same change to `ServeWithServices` in `pkg/plugin/service.go`.

- [ ] **Step 5: Run tests**

Run: `cd /Volumes/Code/github.com/holomush/holomush_worktrees/plugin-arch && task test -- ./internal/plugin/goplugin/ ./pkg/plugin/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(plugin): add mTLS support using existing HoloMUSH CA

Host generates per-plugin server certs and a host client cert from the
game CA. Plugin SDK loads TLS config from env vars. Certs are ephemeral
(temp dir, cleaned up on unload). GRPCBroker inherits TLS from the
parent go-plugin connection.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Wire service injection in Host.Load()

**Files:**

- Modify: `internal/plugin/goplugin/host.go` (Load method)
- Modify: `internal/plugin/goplugin/plugin.go` (GRPCPlugin, store broker)

This task connects the broker proxy to the Load flow: capture the GRPCBroker,
start broker proxies for required services, and populate `required_services`.

- [ ] **Step 1: Store GRPCBroker in GRPCPlugin**

Modify `internal/plugin/goplugin/plugin.go` to capture the broker:

```go
type GRPCPlugin struct {
	goplugin.NetRPCUnsupportedPlugin
	broker *goplugin.GRPCBroker
}

func (p *GRPCPlugin) GRPCClient(_ context.Context, broker *goplugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	p.broker = broker
	return pluginv1.NewPluginServiceClient(c), nil
}
```

Update `PluginMap` to use a pointer so the broker is accessible after Dispense:

```go
var sharedGRPCPlugin = &GRPCPlugin{}

var PluginMap = map[string]goplugin.Plugin{
	"plugin": sharedGRPCPlugin,
}
```

Wait — `PluginMap` is used per-client. Each `hashiplug.NewClient` gets its
own plugin map. The plugin map should NOT be shared globally. Instead, create
a fresh map per Load call and access the `GRPCPlugin` instance after Dispense.

Revised approach: In `Host.Load()`, create a per-plugin `GRPCPlugin` and pass
it in the client config:

```go
grpcPlugin := &GRPCPlugin{}
pluginMap := map[string]hashiplug.Plugin{"plugin": grpcPlugin}

// In DefaultClientFactory, accept pluginMap:
clientConfig.Plugins = pluginMap
```

After `rpcClient.Dispense("plugin")`, the broker is available via
`grpcPlugin.broker`.

- [ ] **Step 2: Wire broker proxies for required services**

In `Host.Load()`, after getting the `pluginClient` and before calling Init,
add service injection:

```go
// Start broker proxies for required services.
requiredServices := make(map[string]string)
if len(manifest.Requires) > 0 && grpcPlugin.broker != nil && h.registry != nil {
	var nextBrokerID uint32 = 1
	for _, svcName := range manifest.Requires {
		svc, resolveErr := h.registry.Resolve(svcName)
		if resolveErr != nil {
			client.Kill()
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
```

Then populate the InitRequest:

```go
initReq := &pluginv1.InitRequest{
	Config: &pluginv1.ServiceConfig{
		RequiredServices: requiredServices,
	},
}
```

- [ ] **Step 3: Update DefaultClientFactory to accept plugin map**

Change `DefaultClientFactory` to accept a `pluginMap` parameter so each Load
gets a fresh GRPCPlugin instance:

```go
type DefaultClientFactory struct {
	tlsConfig *tls.Config
	certEnv   []string
	pluginMap map[string]hashiplug.Plugin
}
```

Or better — change the `NewClient` signature to accept the map. But
`ClientFactory` is an interface used by tests. Instead, keep the interface
simple and have `Load()` create the factory per-plugin with all config baked in.

The `ClientFactory` interface already exists and is used in tests. Rather than
changing the interface, have `Host.Load()` create the client directly (bypassing
the factory) when TLS/broker is needed, OR extend `ClientFactory.NewClient` to
accept options. The simplest approach: make the factory an internal detail of
Load for TLS-enabled plugins, keeping the factory for non-TLS (test) paths.

- [ ] **Step 4: Store broker servers for cleanup**

Add to `loadedPlugin`:

```go
type loadedPlugin struct {
	manifest *plugins.Manifest
	client   PluginClient
	plugin   pluginv1.PluginServiceClient
	conn     grpc.ClientConnInterface
	certDir  string
	broker   *hashiplug.GRPCBroker // for cleanup
}
```

- [ ] **Step 5: Run all tests**

Run: `cd /Volumes/Code/github.com/holomush/holomush_worktrees/plugin-arch && task test -- ./internal/plugin/goplugin/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(plugin): wire GRPCBroker service injection in Host.Load

Captures GRPCBroker from go-plugin handshake. For each required service,
starts a broker proxy that transparently forwards RPCs to the service
registry. Populates required_services in InitRequest with broker IDs.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Wire CA into plugin subsystem setup

**Files:**

- Modify: `internal/plugin/setup/subsystem.go` (PluginSubsystemConfig, Start)
- Modify: `cmd/holomush/core.go` (pass CA to plugin subsystem)

- [ ] **Step 1: Add CA fields to PluginSubsystemConfig**

```go
type PluginSubsystemConfig struct {
	DataDir         string
	DatabaseConnStr string
	CertsDir        string // path to game certs directory (for loading CA)
	GameID          string // game ID for cert SANs
	ABAC            EngineProvider
	// ... rest unchanged ...
}
```

- [ ] **Step 2: Pass CA to goplugin.NewHost in Start()**

In `Start()`, after creating the schema provisioner, load the CA and pass it
to the binary host:

```go
// Load CA for plugin mTLS (optional — runs without TLS if certs not available).
var hostOpts []goplugin.HostOption
hostOpts = append(hostOpts, goplugin.WithSchemaProvisioner(schemaProvisioner))
hostOpts = append(hostOpts, goplugin.WithServiceRegistry(s.registry))

if s.cfg.CertsDir != "" {
	ca, caErr := tlscerts.LoadCA(s.cfg.CertsDir)
	if caErr != nil {
		slog.Warn("plugin mTLS disabled: could not load CA", "error", caErr)
	} else {
		hostOpts = append(hostOpts, goplugin.WithCA(ca, s.cfg.GameID))
		slog.Info("plugin mTLS enabled", "certs_dir", s.cfg.CertsDir)
	}
}

binaryHost := goplugin.NewHost(hostOpts...)
```

- [ ] **Step 3: Pass CertsDir and GameID in core.go**

In `cmd/holomush/core.go`, add the certs config to `PluginSubsystemConfig`:

```go
pluginSub := pluginsetup.NewPluginSubsystem(pluginsetup.PluginSubsystemConfig{
	DataDir:         cfg.DataDir,
	DatabaseConnStr: databaseURL,
	CertsDir:        cfg.CertsDir, // from server config
	GameID:          gameID,       // from event store InitGameID
	// ... rest unchanged ...
})
```

Find where `cfg.CertsDir` and `gameID` are available in the core startup flow
and thread them through. The `gameID` comes from `eventStore.InitGameID(ctx)`
which is called during database subsystem startup.

- [ ] **Step 4: Run tests**

Run: `cd /Volumes/Code/github.com/holomush/holomush_worktrees/plugin-arch && task test`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(plugin): wire CA and service registry into plugin subsystem

PluginSubsystemConfig gains CertsDir and GameID fields. Start() loads
the CA and passes it to goplugin.Host for per-plugin mTLS. Falls back
gracefully if certs are not available.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Integration tests

**Files:**

- Modify: `test/integration/plugin/binary_plugin_test.go`
- Create: `test/integration/plugin/service_injection_test.go`

- [ ] **Step 1: Write integration test for service injection**

Create `test/integration/plugin/service_injection_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/world"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/test/testutil"
)

var _ = Describe("binary plugin service injection", func() {
	var (
		ctx       context.Context
		cancel    context.CancelFunc
		pgEnv     *testutil.PostgresEnv
		connStr   string
		registry  *plugins.ServiceRegistry
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)

		var err error
		pgEnv, err = testutil.StartPostgres(ctx)
		Expect(err).NotTo(HaveOccurred())
		connStr = pgEnv.ConnStr

		migrator, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migrator.Up()).To(Succeed())
		_ = migrator.Close()

		// Set up WorldService in registry
		registry = plugins.NewServiceRegistry()
		// Register WorldService as server-internal (same as production setup)
	})

	AfterEach(func() {
		if pgEnv != nil {
			_ = pgEnv.Terminate(ctx)
		}
		cancel()
	})

	It("populates required_services when plugin declares requires", func() {
		// This test verifies that:
		// 1. The host resolves required services from the registry
		// 2. The InitRequest contains broker IDs
		// 3. The plugin can receive the service map
		//
		// Full end-to-end test with the core-scenes binary plugin.
		// The plugin declares requires: [holomush.world.v1.WorldService]
		Skip("requires core-scenes plugin binary — run with task test:int")
	})
})
```

This is a framework for the integration test. The actual end-to-end test
requires the core-scenes plugin binary and a WorldService registration.
The test should verify that:

1. Plugin loads without error when required services are in the registry
2. Plugin's Init receives non-empty `required_services`
3. Plugin can dial and call the service via the broker

- [ ] **Step 2: Update existing binary_plugin_test.go**

The existing test creates a `goplugin.Host` without a registry. Update it to
pass a registry so the core-scenes plugin gets its required WorldService:

```go
// In the BeforeEach, after creating the schema provisioner:
registry := plugins.NewServiceRegistry()
// Register WorldService...

host := goplugin.NewHost(
	goplugin.WithSchemaProvisioner(schemaProvisioner),
	goplugin.WithServiceRegistry(registry),
)
```

- [ ] **Step 3: Run integration tests**

Run: `cd /Volumes/Code/github.com/holomush/holomush_worktrees/plugin-arch && task test:int -- -run "plugin" -count=1 ./test/integration/plugin/`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "test(plugin): integration tests for GRPCBroker service injection

Tests verify that required services are resolved from the registry,
broker proxies are started, and required_services is populated in
the plugin's InitRequest.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Verification

- [ ] **Step 1: Run full unit test suite**

Run: `cd /Volumes/Code/github.com/holomush/holomush_worktrees/plugin-arch && task test`
Expected: PASS

- [ ] **Step 2: Run linter**

Run: `cd /Volumes/Code/github.com/holomush/holomush_worktrees/plugin-arch && task lint`
Expected: clean

- [ ] **Step 3: Run integration tests**

Run: `cd /Volumes/Code/github.com/holomush/holomush_worktrees/plugin-arch && task test:int`
Expected: PASS

- [ ] **Step 4: Run E2E tests**

Run: `cd /Volumes/Code/github.com/holomush/holomush_worktrees/plugin-arch && task test:e2e`
Expected: PASS

- [ ] **Step 5: Close the bead**

Run: `bd close holomush-z82s --reason "GRPCBroker service injection implemented with mTLS and observability"`

- [ ] **Step 6: Commit any remaining fixes**

```bash
JJ_EDITOR=true jj --no-pager commit -m "chore: fix lint/format issues from verification

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```
