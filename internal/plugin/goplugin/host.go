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
	"maps"
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
	"google.golang.org/grpc/metadata"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/grpc/focus"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/settings"
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
	_ plugins.Host                       = (*Host)(nil)
	_ plugins.ServiceConnProvider        = (*Host)(nil)
	_ plugins.AttributeResolverProvider  = (*Host)(nil)
	_ plugins.EventEmitterConfigurer     = (*Host)(nil)
	_ plugins.FocusDepsConfigurer        = (*Host)(nil)
	_ plugins.IdentityRegistryConfigurer = (*Host)(nil)
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

// stampPluginActor resolves a plugin name to a core.Actor with a ULID-string
// ID via the IdentityRegistry. Returns PLUGIN_UNREGISTERED_INVOKE if the
// plugin is not active in the registry, or if the registry is nil (which is
// operationally equivalent: "no registry" and "registry doesn't have plugin"
// both mean the ULID cannot be resolved). This defensive nil-check keeps
// existing test fixtures that construct Host directly without Manager.RegisterHost
// safe — they'll receive a clean error rather than a nil-pointer panic.
func stampPluginActor(reg plugins.IdentityRegistry, name string) (core.Actor, error) {
	if reg == nil {
		return core.Actor{}, oops.Code("PLUGIN_UNREGISTERED_INVOKE").
			With("plugin", name).
			Errorf("IdentityRegistry not configured on Host")
	}
	id, ok := reg.IDByName(name)
	if !ok {
		return core.Actor{}, oops.Code("PLUGIN_UNREGISTERED_INVOKE").
			With("plugin", name).
			Errorf("plugin not registered in IdentityRegistry")
	}
	return core.Actor{Kind: core.ActorPlugin, ID: id.String()}, nil
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

// WithFocusCoordinator configures the host to inject a focus coordinator
// into the plugin host service for JoinFocus/LeaveFocus/PresentFocus RPCs.
func WithFocusCoordinator(fc focus.Coordinator) HostOption {
	return func(h *Host) { h.focusCoordinator = fc }
}

// WithHistoryReader configures the host with a history reader for
// QueryStreamHistory RPCs.
func WithHistoryReader(hr plugins.HistoryReader) HostOption {
	return func(h *Host) { h.historyReader = hr }
}

// WithReadbackDecryptor configures the host with the read-back decryptor used
// by the DecryptOwnAuditRows RPC. The interface lives in package plugin so the
// Manager can late-inject it via ReadbackDepsConfigurer.
func WithReadbackDecryptor(d plugins.ReadbackDecryptor) HostOption {
	return func(h *Host) { h.readbackDecryptor = d }
}

// WithIdentityRegistry configures the host with an IdentityRegistry for
// ULID-string actor stamping at DeliverEvent and DeliverCommand call sites.
// In production this is wired via Manager.RegisterHost (findOptional pattern).
// Tests that exercise DeliverEvent/DeliverCommand directly MUST provide a
// stub registry so stampPluginActor can resolve plugin names to ULIDs.
func WithIdentityRegistry(reg plugins.IdentityRegistry) HostOption {
	return func(h *Host) { h.identityRegistry = reg }
}

// WithEngine configures the host with an ABAC policy engine for
// PluginHostService.Evaluate calls. Without this option Evaluate fails closed
// with EVALUATE_ENGINE_UNCONFIGURED.
func WithEngine(eng types.AccessPolicyEngine) HostOption {
	return func(h *Host) { h.engine = eng }
}

// WithPlayerSettings configures the host with the player-scope settings store
// used by the GetSetting / SetSetting host RPCs (holomush-iokti.7). Without it
// PLAYER-scope settings calls fail closed.
func WithPlayerSettings(s settings.PlayerSettingsStore) HostOption {
	return func(h *Host) { h.playerSettings = s }
}

// WithCharacterSettings configures the host with the character-scope settings
// store used by the GetSetting / SetSetting host RPCs (holomush-iokti.7).
// Without it CHARACTER-scope settings calls fail closed.
func WithCharacterSettings(s settings.CharacterSettingsStore) HostOption {
	return func(h *Host) { h.characterSettings = s }
}

// WithGameSettings configures the host with the game-scope (server-wide)
// settings store used by the GetSetting / SetSetting host RPCs
// (holomush-iokti.7). Without it GAME-scope settings calls fail closed.
func WithGameSettings(s settings.GameSettings) HostOption {
	return func(h *Host) { h.gameSettings = s }
}

// WithAuditLogger configures the host with an audit logger for
// PluginHostService.Evaluate calls. Optional — omitting it skips audit
// logging without affecting authorization decisions.
func WithAuditLogger(a pluginauthz.Auditor) HostOption {
	return func(h *Host) { h.auditor = a }
}

// WithConfigOverrides threads the per-plugin server-provided config override
// map (plugin name → key → value) into the host so it can be consulted at
// plugin init time. Populated from PluginSubsystemConfig.PluginConfigOverrides.
func WithConfigOverrides(overrides map[string]map[string]string) HostOption {
	// Defensively deep-copy: the caller retains ownership of overrides, and a
	// later mutation must not race with reads of h.configOverrides. Mirrors the
	// Lua host's WithPluginConfigOverrides (plugin-runtime-symmetry).
	cloned := make(map[string]map[string]string, len(overrides))
	for name, cfg := range overrides {
		cloned[name] = maps.Clone(cfg)
	}
	return func(h *Host) { h.configOverrides = cloned }
}

// WithCommandQuerier wires the shared command querier so
// PluginHostService.ListCommands / GetCommandHelp resolve for binary plugins
// (parity with the Lua list_commands host function; plugin-runtime-symmetry).
// The querier is constructed after the Host in subsystem.go, so use
// SetCommandQuerier for the late-bind path instead of this option when the
// construction order requires it.
func WithCommandQuerier(q *commandquery.Querier) HostOption {
	return func(h *Host) { h.commandQuerier = q }
}

// Host manages binary plugins via HashiCorp go-plugins.
type Host struct {
	clientFactory     ClientFactory
	schemaProvisioner *plugins.SchemaProvisioner
	registry          *plugins.ServiceRegistry
	ca                *tlscerts.CA
	gameID            string
	hostBrokerCert    *tlscerts.ServerCert
	hostClientCert    *tlscerts.ClientCert
	eventEmitter      plugins.PluginIntentEmitter
	focusCoordinator  focus.Coordinator
	historyReader     plugins.HistoryReader
	readbackDecryptor plugins.ReadbackDecryptor
	identityRegistry  plugins.IdentityRegistry
	engine            types.AccessPolicyEngine
	auditor           pluginauthz.Auditor
	commandQuerier    *commandquery.Querier
	// playerSettings / characterSettings / gameSettings back the owner-
	// partitioned GetSetting / SetSetting host RPCs (holomush-iokti.7). They are
	// late-bound (SetSettingsStores) because the settings stores are assembled in
	// the gRPC subsystem after the plugin host is constructed — same rationale as
	// focusCoordinator / historyReader.
	playerSettings    settings.PlayerSettingsStore
	characterSettings settings.CharacterSettingsStore
	gameSettings      settings.GameSettings
	// configOverrides is the per-plugin server-provided config override
	// (plugin name → key → value), threaded from PluginSubsystemConfig.
	configOverrides map[string]map[string]string
	plugins         map[string]*loadedPlugin
	mu              sync.RWMutex
	closed          bool

	// tokenStore authenticates per-dispatch actor claims on the binary-plugin
	// EmitEvent boundary. The sweeper goroutine is host-owned: tokenStoreCtx
	// is canceled in Close() to deterministically stop it before
	// tokenStore.Close() does the belt-and-braces shutdown.
	tokenStore       *emitTokenStore
	tokenStoreCtx    context.Context
	tokenStoreCancel context.CancelFunc
}

// loadedPlugin holds state for a single loaded binary plugin.
type loadedPlugin struct {
	manifest            *plugins.Manifest
	client              PluginClient
	plugin              pluginv1.PluginServiceClient
	conn                grpc.ClientConnInterface // underlying gRPC conn to the plugin process
	certDir             string                   // temp cert directory, cleaned up on unload
	broker              *hashiplug.GRPCBroker    // broker for service injection, nil if factory-mocked
	registeredEmitTypes []string                 // INV-S5: populated from InitResponse.RegisteredEmitTypes
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
	tokenStoreCtx, tokenStoreCancel := context.WithCancel(context.Background())
	h := &Host{
		clientFactory:    factory,
		plugins:          make(map[string]*loadedPlugin),
		tokenStore:       newEmitTokenStore(),
		tokenStoreCtx:    tokenStoreCtx,
		tokenStoreCancel: tokenStoreCancel,
	}
	for _, opt := range opts {
		opt(h)
	}
	// Start the token-store sweeper. tokenStoreCancel (called from Close)
	// signals the sweeper to exit; tokenStore.Close() is idempotent and
	// also closes the s.stop channel as a belt-and-braces safety.
	go h.tokenStore.Run(h.tokenStoreCtx)
	if h.ca != nil {
		brokerCert, brokerCertErr := tlscerts.GenerateServerCert(h.ca, h.gameID, "plugin-host")
		if brokerCertErr != nil {
			panic("goplugin: failed to generate host broker cert: " + brokerCertErr.Error())
		}
		h.hostBrokerCert = brokerCert

		cert, certErr := tlscerts.GenerateClientCert(h.ca, "plugin-host")
		if certErr != nil {
			panic("goplugin: failed to generate host TLS cert: " + certErr.Error())
		}
		h.hostClientCert = cert
	} else {
		// Loud one-shot warning: without a CA, go-plugin falls back to its
		// default unencrypted, unauthenticated gRPC transport between the
		// host and each plugin subprocess. Production deployments MUST
		// configure WithCA; surface the misconfiguration in operator logs
		// so it cannot silently slip through.
		slog.WarnContext(
			context.Background(),
			"binary plugin mTLS disabled: gRPC channel with plugin subprocess is unauthenticated and unencrypted; configure WithCA for production deployments",
			"component", "goplugin.Host",
			"mtls", "disabled",
		)
	}
	return h
}

// overrideFor returns the server-provided config override for a plugin, or nil
// when none is configured (manifest defaults then apply).
func (h *Host) overrideFor(pluginName string) map[string]string {
	return h.configOverrides[pluginName]
}

// manifestNeedsInit reports whether the host must call Init on a plugin.
// Init injects services (requires/provides), provisions storage, captures
// crypto.emits (INV-S5), AND — INV-PC-8 — delivers plugin_config for any
// plugin declaring a config schema.
func manifestNeedsInit(m *plugins.Manifest) bool {
	return len(m.Requires) > 0 ||
		len(m.Provides) > 0 ||
		m.Storage == plugins.StoragePostgres ||
		(m.Crypto != nil && len(m.Crypto.Emits) > 0) ||
		len(m.Config) > 0
}

// SetEventEmitter injects the shared plugin intent emitter used by the host
// callback service for binary plugins.
func (h *Host) SetEventEmitter(emitter plugins.PluginIntentEmitter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.eventEmitter = emitter
}

// SetFocusCoordinator injects the focus coordinator after construction.
// This supports late-binding: the plugin subsystem starts before gRPC,
// so the coordinator is not available at Host construction time. The
// coordinator is resolved lazily by the plugin host service when a
// plugin calls JoinFocus/LeaveFocus/PresentFocus.
func (h *Host) SetFocusCoordinator(fc focus.Coordinator) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.focusCoordinator = fc
}

// FocusCoordinator returns the current focus coordinator, or nil if not set.
func (h *Host) FocusCoordinator() focus.Coordinator {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.focusCoordinator
}

// SetHistoryReader injects the history reader after construction.
// Same late-binding rationale as SetFocusCoordinator.
func (h *Host) SetHistoryReader(hr plugins.HistoryReader) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.historyReader = hr
}

// HistoryReader returns the current history reader, or nil if not set.
func (h *Host) HistoryReader() plugins.HistoryReader {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.historyReader
}

// SetCommandQuerier late-binds the shared command querier into the host after
// construction. The querier is built after binaryHost in subsystem.go (it
// depends on the command registry which is populated after plugin load), so
// it cannot be passed via WithCommandQuerier at Host construction time.
// Mirrors hostfunc.SetCommandQuerier (plugin-runtime-symmetry).
func (h *Host) SetCommandQuerier(q *commandquery.Querier) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.commandQuerier = q
}

// SetReadbackDecryptor injects the read-back decryptor after construction.
// Same late-binding rationale as SetHistoryReader: the OwnerMap and crypto
// deps are only assembled once the plugin subsystem has started and the
// history reader is built (cmd/holomush/sub_grpc.go).
func (h *Host) SetReadbackDecryptor(d plugins.ReadbackDecryptor) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.readbackDecryptor = d
}

// ReadbackDecryptor returns the current read-back decryptor, or nil if not set.
func (h *Host) ReadbackDecryptor() plugins.ReadbackDecryptor {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.readbackDecryptor
}

// SetSettingsStores injects the player / character / game settings stores after
// construction. Same late-binding rationale as SetFocusCoordinator: the stores
// are assembled in the gRPC subsystem (cmd/holomush/sub_grpc.go) after the
// plugin host is constructed. Used by the GetSetting / SetSetting host RPCs
// (holomush-iokti.7). Implements plugins.SettingsDepsConfigurer.
func (h *Host) SetSettingsStores(
	player settings.PlayerSettingsStore,
	character settings.CharacterSettingsStore,
	game settings.GameSettings,
) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.playerSettings = player
	h.characterSettings = character
	h.gameSettings = game
}

// PlayerSettings returns the player-scope settings store, or nil if not set.
func (h *Host) PlayerSettings() settings.PlayerSettingsStore {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.playerSettings
}

// CharacterSettings returns the character-scope settings store, or nil if not set.
func (h *Host) CharacterSettings() settings.CharacterSettingsStore {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.characterSettings
}

// GameSettings returns the game-scope settings store, or nil if not set.
func (h *Host) GameSettings() settings.GameSettings {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.gameSettings
}

// SetIdentityRegistry implements plugins.IdentityRegistryConfigurer.
// Called by Manager.RegisterHost via findOptional after both are constructed.
// The Manager itself implements IdentityRegistry, so passing m to the host
// satisfies the late-binding contract (Hosts are constructed before Manager
// RegisterHost returns).
func (h *Host) SetIdentityRegistry(reg plugins.IdentityRegistry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.identityRegistry = reg
}

// identityRegistrySnapshot returns the currently-configured registry under
// RLock. Stamp sites in DeliverEvent / DeliverCommand release h.mu before
// invoking the registry, so a concurrent SetIdentityRegistry would race
// against an unlocked read of the two-word interface value. Callers MUST
// use this accessor rather than reading h.identityRegistry directly.
func (h *Host) identityRegistrySnapshot() plugins.IdentityRegistry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.identityRegistry
}

// ownedResourceTypes returns a map of resource type names owned by the named
// plugin (from its manifest's ResourceTypes field), guarded by RLock. Returns
// nil when the plugin is not loaded. Used by Evaluate's entitlement check.
func (h *Host) ownedResourceTypes(pluginName string) map[string]bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	lp, ok := h.plugins[pluginName]
	if !ok || lp.manifest == nil {
		return nil
	}
	m := make(map[string]bool, len(lp.manifest.ResourceTypes))
	for _, rt := range lp.manifest.ResourceTypes {
		m[rt] = true
	}
	return m
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

		hostTLSConfig = buildHostTLSConfig(h.ca, h.hostBrokerCert, h.hostClientCert, manifest.Name)
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
	var nextBrokerID uint32 = 1
	if grpcPlugin.broker != nil {
		hostBrokerID := nextBrokerID
		nextBrokerID++
		go grpcPlugin.broker.AcceptAndServe(hostBrokerID, newPluginHostServiceServer(h, manifest.Name))
		requiredServices[pluginsdk.PluginHostServiceName] = fmt.Sprintf("broker:%d", hostBrokerID)
	}
	if len(manifest.Requires) > 0 && grpcPlugin.broker != nil && h.registry != nil {
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
			slog.InfoContext(
				ctx,
				"started broker proxy for required service",
				"plugin", manifest.Name,
				"service", svcName,
				"broker_id", brokerID,
			)
		}
	}

	// Call Init on plugins that need service injection (storage or requires),
	// declare crypto.emits (INV-S5 needs InitResponse), or declare a config
	// schema (INV-PC-8: plugin_config must be delivered).
	needsInit := manifestNeedsInit(manifest)
	var registeredEmitTypes []string
	if needsInit {
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

		if len(manifest.Config) > 0 {
			merged, mergeErr := plugins.MergePluginConfig(manifest.Config, h.overrideFor(manifest.Name))
			if mergeErr != nil {
				client.Kill()
				if certDir != "" {
					_ = os.RemoveAll(certDir) //nolint:errcheck // best-effort cleanup
				}
				return oops.In("goplugin").With("plugin", manifest.Name).
					With("operation", "merge_plugin_config").Wrap(mergeErr)
			}
			initReq.Config.PluginConfig = merged
		}

		initResp, initErr := pluginClient.Init(ctx, initReq)
		if initErr != nil {
			client.Kill()
			if certDir != "" {
				_ = os.RemoveAll(certDir) //nolint:errcheck // best-effort cleanup
			}
			return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "init").Wrap(initErr)
		}
		if initResp != nil {
			registeredEmitTypes = initResp.GetRegisteredEmitTypes()
		}
	}

	h.plugins[manifest.Name] = &loadedPlugin{
		manifest:            manifest,
		client:              client,
		plugin:              pluginClient,
		conn:                pluginConn,
		certDir:             certDir,
		broker:              grpcPlugin.broker,
		registeredEmitTypes: registeredEmitTypes,
	}

	return nil
}

// PluginEmitRegistry implements plugins.Host. Returns the
// InitResponse.RegisteredEmitTypes captured at Load time, or (nil, false)
// when the plugin is not loaded.
func (h *Host) PluginEmitRegistry(name string) ([]string, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	p, ok := h.plugins[name]
	if !ok {
		return nil, false
	}
	return p.registeredEmitTypes, true
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
		slog.WarnContext(ctx, "unrecognized actor kind, using 'unknown'",
			"kind", int(event.ActorKind))
	}

	// Single host-side event->proto mapping for delivery (holomush-av954),
	// guarded by TestEventProtoRoundTripCarriesEveryField so a field added to
	// pluginsdk.Event cannot silently miss the binary runtime.
	protoEvent := pluginsdk.EventToProto(event)

	callCtx, cancel := context.WithTimeout(ctx, DefaultEventTimeout)
	defer cancel()

	// Per spec §3.3.4: compute the stored actor with re-anchor for ActorSystem.
	// Plugins never speak as the host's system identity — when the upstream
	// ctx carries ActorSystem the host re-anchors to ActorPlugin:<ULID>.
	storedActor, err := stampPluginActor(h.identityRegistrySnapshot(), name)
	if err != nil {
		return nil, oops.In("goplugin").With("plugin", name).With("operation", "stamp_actor").Wrap(err)
	}
	if upstream, ok := core.ActorFromContext(ctx); ok {
		switch upstream.Kind {
		case core.ActorCharacter, core.ActorPlugin:
			storedActor = upstream // verbatim — cascade preserved
		case core.ActorSystem:
			// re-anchor: ActorSystem may not emit as system identity; use plugin ULID
			// storedActor already holds the plugin ULID from stampPluginActor above
		}
	}

	emitToken, err := h.tokenStore.Issue(name, storedActor)
	if err != nil {
		return nil, oops.In("goplugin").With("plugin", name).With("operation", "issue_emit_token").Wrap(err)
	}
	defer h.tokenStore.Revoke(emitToken)

	callCtx = metadata.AppendToOutgoingContext(callCtx, "x-holomush-emit-token", emitToken)
	// Existing actor-kind / -id metadata still attached for plugin-side advisory
	// consumption (pkg/plugin/sdk.go ActorMetadataFromIncomingContext).
	callCtx = pluginsdk.WithOutgoingActorMetadata(callCtx, coreActorKindToSDK(storedActor.Kind), storedActor.ID)

	resp, err := p.plugin.HandleEvent(callCtx, &pluginv1.HandleEventRequest{Event: protoEvent})
	if err != nil {
		return nil, oops.In("goplugin").With("plugin", name).With("operation", "handle_event").Wrap(err)
	}

	// Single proto->emit mapping site (holomush-av954), guarded by
	// TestEmitEventProtoRoundTripCarriesEveryField so a field added to EmitEvent
	// (notably Sensitive) cannot be silently dropped on the return-value receive path.
	emits := make([]pluginsdk.EmitEvent, len(resp.GetEmitEvents()))
	for i, e := range resp.GetEmitEvents() {
		emits[i] = pluginsdk.EmitEventFromProto(e)
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

	// Single cmd->proto mapping site (holomush-peqfu); see
	// pluginsdk.CommandRequestToProto for the parity contract.
	protoReq := &pluginv1.HandleCommandRequest{
		Command: pluginsdk.CommandRequestToProto(cmd),
	}

	callCtx, cancel := context.WithTimeout(ctx, DefaultEventTimeout)
	defer cancel()

	// Per spec §3.3.4: same actor re-anchor + token issuance as DeliverEvent.
	storedActor, err := stampPluginActor(h.identityRegistrySnapshot(), name)
	if err != nil {
		return nil, oops.In("goplugin").With("plugin", name).With("operation", "stamp_actor").Wrap(err)
	}
	if upstream, ok := core.ActorFromContext(ctx); ok {
		switch upstream.Kind {
		case core.ActorCharacter, core.ActorPlugin:
			storedActor = upstream
		case core.ActorSystem:
			// re-anchor: ActorSystem may not emit as system identity; use plugin ULID
			// storedActor already holds the plugin ULID from stampPluginActor above
		}
	}

	emitToken, err := h.tokenStore.Issue(name, storedActor)
	if err != nil {
		return nil, oops.In("goplugin").With("plugin", name).With("operation", "issue_emit_token").Wrap(err)
	}
	defer h.tokenStore.Revoke(emitToken)

	callCtx = metadata.AppendToOutgoingContext(callCtx, "x-holomush-emit-token", emitToken)
	callCtx = pluginsdk.WithOutgoingActorMetadata(callCtx, coreActorKindToSDK(storedActor.Kind), storedActor.ID)

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

	// Single proto->emit mapping site for the command return-value path
	// (holomush-av954), guarded by TestEmitEventProtoRoundTripCarriesEveryField
	// so a field added to EmitEvent (notably Sensitive) cannot be silently
	// dropped here — the sibling of the HandleEvent receive path above.
	events := make([]pluginsdk.EmitEvent, len(r.GetEvents()))
	for i, e := range r.GetEvents() {
		events[i] = pluginsdk.EmitEventFromProto(e)
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

func coreActorKindToSDK(kind core.ActorKind) pluginsdk.ActorKind {
	switch kind {
	case core.ActorCharacter:
		return pluginsdk.ActorCharacter
	case core.ActorSystem:
		return pluginsdk.ActorSystem
	default:
		return pluginsdk.ActorPlugin
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

// PluginAuditClient returns the PluginAuditService gRPC client for a
// loaded plugin. Returns nil when the plugin is not loaded, the
// connection is not yet established, or the plugin did not register the
// service. The host uses this to route JetStream audit deliveries into
// plugin-owned audit schemas (F5).
func (h *Host) PluginAuditClient(pluginName string) pluginv1.PluginAuditServiceClient {
	h.mu.RLock()
	defer h.mu.RUnlock()
	lp, ok := h.plugins[pluginName]
	if !ok || lp.conn == nil {
		return nil
	}
	return pluginv1.NewPluginAuditServiceClient(lp.conn)
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

// sessionStreamsRequestToProto is the single host-side site mapping
// plugins.SessionStreamsRequest onto its proto wire form for the binary
// runtime. SessionStreamsRequest forks per runtime (Lua passes the same fields
// as positional on_session_subscribe args); routing the binary marshal through
// one function lets TestSessionStreamsRequestToProtoCarriesEveryField assert by
// reflection that every field crosses, so a field added here without wiring
// cannot silently miss the binary runtime (holomush-av954).
func sessionStreamsRequestToProto(req plugins.SessionStreamsRequest) *pluginv1.QuerySessionStreamsRequest {
	return &pluginv1.QuerySessionStreamsRequest{
		CharacterId: req.CharacterID,
		PlayerId:    req.PlayerID,
		SessionId:   req.SessionID,
	}
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

	resp, err := p.plugin.QuerySessionStreams(callCtx, sessionStreamsRequestToProto(req))
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

	// Token-store teardown (Task 7): cancel sweeper context first so the
	// sweeper goroutine exits on ctx.Done, then close the store. Surfaces
	// tokenStore.Close error since the existing function previously
	// returned nil unconditionally.
	h.tokenStoreCancel()
	return h.tokenStore.Close()
}

// buildHostTLSConfig creates a TLS config for the host to connect to a plugin
// as a gRPC client with mTLS. The host presents its client cert and verifies
// the plugin's server cert against the CA.
func buildHostTLSConfig(ca *tlscerts.CA, brokerCert *tlscerts.ServerCert, clientCert *tlscerts.ClientCert, pluginName string) *cryptotls.Config {
	caPool := x509.NewCertPool()
	caPool.AddCert(ca.Certificate)

	serverTLSCert := cryptotls.Certificate{
		Certificate: [][]byte{brokerCert.Certificate.Raw},
		PrivateKey:  brokerCert.PrivateKey,
	}
	clientTLSCert := cryptotls.Certificate{
		Certificate: [][]byte{clientCert.Certificate.Raw},
		PrivateKey:  clientCert.PrivateKey,
	}

	return &cryptotls.Config{
		Certificates: []cryptotls.Certificate{serverTLSCert},
		GetClientCertificate: func(*cryptotls.CertificateRequestInfo) (*cryptotls.Certificate, error) {
			return &clientTLSCert, nil
		},
		RootCAs:    caPool,
		ServerName: "holomush-plugin-" + pluginName,
		MinVersion: cryptotls.VersionTLS13,
	}
}
