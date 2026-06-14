// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	cryptotls "crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	hashiplug "github.com/hashicorp/go-plugin"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"github.com/samber/oops"
	"google.golang.org/grpc"
)

// Handler is the interface that binary plugins must implement.
type Handler interface {
	// HandleEvent processes an incoming event and returns any events to emit.
	HandleEvent(ctx context.Context, event Event) ([]EmitEvent, error)
}

// CommandHandler is implemented by binary plugins that handle commands.
// Plugins that only handle events need not implement this interface.
type CommandHandler interface {
	// HandleCommand processes a command and returns the result.
	HandleCommand(ctx context.Context, req CommandRequest) (*CommandResponse, error)
}

// HandshakeConfig is the go-plugin handshake configuration.
// Both host and plugins must use the same values.
var HandshakeConfig = hashiplug.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "HOLOMUSH_PLUGIN",
	MagicCookieValue: "holomush-v1",
}

// ServeConfig configures the plugin server.
type ServeConfig struct {
	// Handler is the event handler implementation.
	// Required; Serve will panic if nil.
	Handler Handler

	// Validator is an optional protobuf message validator installed as a
	// gRPC unary server interceptor on the plugin's gRPC server. If nil,
	// ServeWithServices constructs a default validator via
	// NewDefaultValidator() at startup. Plugins that need custom validation
	// (e.g., a validator with extra rules registered) can supply their own.
	Validator Validator
}

// Serve starts the plugin server. This should be called from main().
// It blocks and never returns under normal operation.
//
// Example usage:
//
//	package main
//
//	import (
//		"context"
//		pluginsdk "github.com/holomush/holomush/pkg/plugin"
//	)
//
//	type EchoPlugin struct{}
//
//	func (p *EchoPlugin) HandleEvent(ctx context.Context, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
//		return []pluginsdk.EmitEvent{
//			{
//				Stream:  event.Stream,
//				Type:    event.Type,
//				Payload: event.Payload,
//			},
//		}, nil
//	}
//
//	func main() {
//		pluginsdk.Serve(&pluginsdk.ServeConfig{
//			Handler: &EchoPlugin{},
//		})
//	}
func Serve(config *ServeConfig) {
	if config == nil {
		panic("plugin: config cannot be nil")
	}
	if config.Handler == nil {
		panic("plugin: config.Handler cannot be nil")
	}
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

// grpcPlugin implements go-plugin's Plugin interface for gRPC.
type grpcPlugin struct {
	hashiplug.NetRPCUnsupportedPlugin
	handler Handler
}

// GRPCServer registers the plugin server (called by plugin process).
func (p *grpcPlugin) GRPCServer(_ *hashiplug.GRPCBroker, s *grpc.Server) error {
	if p.handler == nil {
		return errors.New("plugin: handler is nil")
	}
	adapter := &pluginServerAdapter{handler: p.handler}
	if ch, ok := p.handler.(CommandHandler); ok {
		adapter.cmdHandler = ch
	}
	pluginv1.RegisterPluginServiceServer(s, adapter)
	return nil
}

// GRPCClient is required by go-plugin's GRPCPlugin interface but is never
// called on the plugin side. The host has its own GRPCClient implementation.
func (p *grpcPlugin) GRPCClient(_ context.Context, _ *hashiplug.GRPCBroker, _ *grpc.ClientConn) (interface{}, error) {
	return nil, errors.New("plugin: GRPCClient not implemented on plugin side")
}

// pluginServerAdapter adapts Handler (and optionally CommandHandler) to pluginv1.PluginServiceServer.
type pluginServerAdapter struct {
	pluginv1.UnimplementedPluginServiceServer
	handler         Handler
	cmdHandler      CommandHandler  // nil if handler does not implement CommandHandler
	serviceProvider ServiceProvider // nil if plugin does not provide services
	brokerDialer    brokerDialer
}

// Init implements pluginv1.PluginServiceServer. When a ServiceProvider is set,
// it delegates to the provider's Init; otherwise it returns an empty response.
//
// Before delegating to the provider, Init optionally injects host-facing SDK
// facades based on which optional interfaces the provider implements:
//
//   - EventSinkAware         -> provider.SetEventSink(...)
//   - FocusClientAware       -> provider.SetFocusClient(...)
//   - HostEvaluatorAware     -> provider.SetHostEvaluator(...)
//   - SettingsClientAware    -> provider.SetSettingsClient(...)
//   - SnapshotDecryptorAware -> provider.SetSnapshotDecryptor(...)
//   - CommandListerAware     -> provider.SetCommandLister(...)
//
// To avoid opening one broker connection per facade, Init dials the plugin
// host exactly once and shares that *grpc.ClientConn across every facade
// the provider consents to receive. A plugin that opts into neither
// interface pays no connection cost.
func (a *pluginServerAdapter) Init(ctx context.Context, req *pluginv1.InitRequest) (*pluginv1.InitResponse, error) {
	var config *pluginv1.ServiceConfig
	if req != nil {
		config = req.GetConfig()
	}

	// Fail closed at load: a provider that implements a host-capability *Aware
	// interface for a non-exempt capability it did not declare must not load
	// (INV-PLUGIN-54). Validation precedes injection, so a client is only ever
	// wired for a validated declaration — the spec's gate+validate in one pass.
	if a.serviceProvider != nil {
		if err := validateDeclaredCapabilities(a.serviceProvider, config.GetDeclaredCapabilities()); err != nil {
			return nil, oops.With("phase", "init").Wrap(err)
		}
	}

	_, wantsSink := a.serviceProvider.(EventSinkAware)
	_, wantsFocus := a.serviceProvider.(FocusClientAware)
	_, wantsEvaluator := a.serviceProvider.(HostEvaluatorAware)
	_, wantsSettings := a.serviceProvider.(SettingsClientAware)
	_, wantsDecryptor := a.serviceProvider.(SnapshotDecryptorAware)
	_, wantsCommandLister := a.serviceProvider.(CommandListerAware)

	// Lazily dial a single plugin-host gRPC connection shared by every
	// host-facing SDK facade the provider opts into. If the provider opts
	// into none, we never dial.
	var hostConn *grpc.ClientConn
	if wantsSink || wantsFocus || wantsEvaluator || wantsSettings || wantsDecryptor || wantsCommandLister {
		requiredServices := map[string]string(nil)
		if config != nil {
			requiredServices = config.GetRequiredServices()
		}
		conn, err := dialPluginHost(a.brokerDialer, requiredServices)
		if err != nil {
			return nil, oops.With("phase", "init").With("service", PluginHostServiceName).Wrap(err)
		}
		hostConn = conn
	}

	if sinkAware, ok := a.serviceProvider.(EventSinkAware); ok {
		sinkAware.SetEventSink(newPluginHostEventSink(hostConn))
	}
	if focusAware, ok := a.serviceProvider.(FocusClientAware); ok {
		focusAware.SetFocusClient(newPluginHostFocusClient(hostConn))
	}
	if evalAware, ok := a.serviceProvider.(HostEvaluatorAware); ok {
		evalAware.SetHostEvaluator(newHostEvaluateClient(hostConn))
	}
	if settingsAware, ok := a.serviceProvider.(SettingsClientAware); ok {
		settingsAware.SetSettingsClient(newPluginHostSettingsClient(hostConn))
	}
	if decAware, ok := a.serviceProvider.(SnapshotDecryptorAware); ok {
		decAware.SetSnapshotDecryptor(&snapshotDecryptClient{client: hostv1.NewAuditServiceClient(hostConn)})
	}
	if clAware, ok := a.serviceProvider.(CommandListerAware); ok {
		clAware.SetCommandLister(newHostCommandClient(hostConn))
	}

	if a.serviceProvider == nil {
		return &pluginv1.InitResponse{}, nil
	}
	if err := a.serviceProvider.Init(ctx, config); err != nil {
		return nil, oops.With("phase", "init").Wrap(err)
	}

	// INV-PLUGIN-32: populate RegisteredEmitTypes from EmitTypeRegistrar if the
	// provider opts in. Plugins without crypto.emits leave the set empty.
	// A registrar may legally return nil from EmitRegistry() (e.g., during
	// early construction); guard the dereference to avoid an init-time
	// panic.
	resp := &pluginv1.InitResponse{}
	if registrar, ok := a.serviceProvider.(EmitTypeRegistrar); ok {
		if reg := registrar.EmitRegistry(); reg != nil {
			resp.RegisteredEmitTypes = reg.RegisteredEmitTypes()
		}
	}
	return resp, nil
}

// HandleEvent implements pluginv1.PluginServiceServer.
func (a *pluginServerAdapter) HandleEvent(ctx context.Context, req *pluginv1.HandleEventRequest) (*pluginv1.HandleEventResponse, error) {
	ctx = contextWithIncomingActorMetadata(ctx)

	// protoEvent may be nil; proto getters return zero values for nil receivers,
	// making this safe without explicit nil checks.
	protoEvent := req.GetEvent()

	// Single proto->event mapping site (holomush-av954), guarded by
	// TestEventProtoRoundTripCarriesEveryField so a field added to Event cannot
	// be silently dropped on the binary receive side (the host→plugin analogue
	// of the holomush-dble7 connection_id omission).
	event := EventFromProto(protoEvent)

	// Call the user's handler
	emits, err := a.handler.HandleEvent(ctx, event)
	if err != nil {
		return nil, oops.With("event_id", event.ID).Wrap(err)
	}

	// Single emit->proto mapping site (holomush-av954), guarded by
	// TestEmitEventProtoRoundTripCarriesEveryField so a field added to EmitEvent
	// (notably Sensitive) cannot be silently dropped on the return-value emit path.
	protoEmits := make([]*pluginv1.EmitEvent, len(emits))
	for i, e := range emits {
		protoEmits[i] = EmitEventToProto(e)
	}

	return &pluginv1.HandleEventResponse{EmitEvents: protoEmits}, nil
}

// HandleCommand implements pluginv1.PluginServiceServer.
func (a *pluginServerAdapter) HandleCommand(ctx context.Context, req *pluginv1.HandleCommandRequest) (*pluginv1.HandleCommandResponse, error) {
	if a.cmdHandler == nil {
		return &pluginv1.HandleCommandResponse{Response: &pluginv1.CommandResponse{}}, nil
	}
	ctx = contextWithIncomingActorMetadata(ctx)

	// Single proto->cmd mapping site (holomush-peqfu). holomush-dble7 was a
	// hand-copied receive site that silently dropped connection_id; routing
	// through CommandRequestFromProto, guarded by the proto round-trip parity
	// test, makes that class of omission a test failure.
	cmd := CommandRequestFromProto(req.GetCommand())

	// Attach an audit hint slice to the handler context so plugin code
	// can call pluginsdk.Audit(ctx).Deny(...) and have hints collected
	// here for serialization into the proto response.
	handlerCtx := NewContextForHandler(ctx)

	resp, err := a.cmdHandler.HandleCommand(handlerCtx, cmd)
	if err != nil {
		return nil, oops.With("command", cmd.Command).Wrap(err)
	}

	if resp == nil {
		return &pluginv1.HandleCommandResponse{Response: &pluginv1.CommandResponse{}}, nil
	}

	// Harvest any hints the handler accumulated on its context and merge
	// them with any hints the handler attached directly to the response
	// struct (both paths are supported for flexibility).
	contextHints := HarvestAuditHints(handlerCtx)
	allHints := append([]AuditHint{}, contextHints...)
	allHints = append(allHints, resp.AuditHints...)

	// Single emit->proto mapping site (holomush-av954); see EmitEventToProto.
	protoEvents := make([]*pluginv1.EmitEvent, len(resp.Events))
	for i, e := range resp.Events {
		protoEvents[i] = EmitEventToProto(e)
	}

	protoHints := make([]*pluginv1.AuditDecisionHint, len(allHints))
	for i, h := range allHints {
		protoHints[i] = &pluginv1.AuditDecisionHint{
			Id:              h.ID,
			Name:            h.Name,
			Message:         h.Message,
			Effect:          sdkAuditEffectToProto(h.Effect),
			ActionQualifier: h.ActionQualifier,
			Resource:        h.Resource,
			Attributes:      h.Attributes,
		}
	}

	return &pluginv1.HandleCommandResponse{
		Response: &pluginv1.CommandResponse{
			Status:     sdkCommandStatusToProto(resp.Status),
			Output:     resp.Output,
			Events:     protoEvents,
			AuditHints: protoHints,
		},
	}, nil
}

// sdkAuditEffectToProto converts an SDK AuditEffect string to the closed
// proto enum. Unknown SDK effects collapse to UNSPECIFIED — they have already
// been dropped at the recorder level by the empty-ID guard, but defensive
// mapping keeps the wire format honest.
func sdkAuditEffectToProto(e AuditEffect) pluginv1.AuditEffect {
	switch e {
	case AuditEffectDeny:
		return pluginv1.AuditEffect_AUDIT_EFFECT_DENY
	case AuditEffectAllow:
		return pluginv1.AuditEffect_AUDIT_EFFECT_ALLOW
	default:
		return pluginv1.AuditEffect_AUDIT_EFFECT_UNSPECIFIED
	}
}

// sdkCommandStatusToProto converts an SDK CommandStatus to a proto CommandStatus.
func sdkCommandStatusToProto(s CommandStatus) pluginv1.CommandStatus {
	switch s {
	case CommandOK:
		return pluginv1.CommandStatus_COMMAND_STATUS_OK
	case CommandError:
		return pluginv1.CommandStatus_COMMAND_STATUS_ERROR
	case CommandFailure:
		return pluginv1.CommandStatus_COMMAND_STATUS_FAILURE
	case CommandFatal:
		return pluginv1.CommandStatus_COMMAND_STATUS_FATAL
	default:
		return pluginv1.CommandStatus_COMMAND_STATUS_OK
	}
}

// protoActorKindToActorKind converts proto ActorKind to pkg/plugin ActorKind.
func protoActorKindToActorKind(kind string) ActorKind {
	switch kind {
	case "character":
		return ActorCharacter
	case "system":
		return ActorSystem
	case "plugin":
		return ActorPlugin
	default:
		return ActorCharacter
	}
}

// loadPluginTLSProvider returns a TLS config provider for the plugin server
// if the cert env vars are set. Returns nil when running without mTLS.
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

		caCert, err := os.ReadFile(filepath.Clean(caPath))
		if err != nil {
			return nil, fmt.Errorf("read CA cert: %w", err)
		}

		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to add CA cert to pool")
		}

		return &cryptotls.Config{
			Certificates: []cryptotls.Certificate{cert},
			RootCAs:      caPool,
			ClientCAs:    caPool,
			ClientAuth:   cryptotls.RequireAndVerifyClientCert,
			MinVersion:   cryptotls.VersionTLS13,
		}, nil
	}
}
