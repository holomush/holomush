// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Binary plugin fixture for the Phase 7 INV-EVENTBUS-27 e2e test
// (holomush-1r0v.4). The fixture exercises the read-side
// PluginDowngradeFence end-to-end through the real loader / gRPC /
// dispatcher / fence chain.
//
// Two QueryHistory branches selectable via the QueryHistoryRequest
// subject substring (NOT via env var — go-plugin strips the child
// process environment to PATH + cert vars only; see
// internal/plugin/goplugin/host.go:79 and :421):
//
//	subject contains ".test_downgrade_honest." → return ciphertext
//	  byte-equal: replays the most-recent AuditEvent row verbatim, so
//	  the host fence passes the row through and the host crypto stack
//	  decrypts to plaintext.
//
//	subject contains ".test_downgrade_malicious." → fabricate a row
//	  with codec=identity + cleartext payload for the
//	  `test-downgrade-attacker:secret` event type. The host's
//	  PluginDowngradeFence (INV-P7-7) MUST refuse this row per-row
//	  (metadata_only=true, NoPlaintextReason=DowngradeRefused) and
//	  emit a `plugin_integrity_violation` audit.
//
// The honest-mode cache is a single atomic.Pointer[AuditRow] —
// deliberately last-writer-wins. The e2e test issues one Emit then
// one QueryHistory per assertion, which keeps the cache adequate
// without per-subject indexing.
package main

import (
	"context"
	"strings"
	"sync/atomic"

	"google.golang.org/grpc"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

const (
	// modeMarkerHonest and modeMarkerMalicious are subject substrings
	// the e2e test embeds in the QueryHistory subject to select the
	// fixture branch. The plugin name segment in audit subjects per the
	// manifest's audit block (events.*.test_downgrade_*.>) doubles as the
	// mode selector — there is no separate channel.
	modeMarkerHonest    = ".test_downgrade_honest."
	modeMarkerMalicious = ".test_downgrade_malicious."

	// fabricatedEventType matches the manifest's crypto.emits declaration
	// (sensitivity:always). The host fence's manifest-set heuristic
	// (INV-P7-7) refuses identity-codec rows for this type — that is the
	// downgrade attack the malicious branch simulates.
	fabricatedEventType = "test-downgrade-attacker:secret"

	// fabricatedSubject is the subject the malicious branch stamps onto
	// fabricated rows. Matches the manifest's audit pattern so the host
	// router accepts the row as belonging to this plugin.
	fabricatedSubject = "events.test.test_downgrade_malicious.01ABC.ic"

	// fabricatedPayload is the cleartext the malicious branch leaks. The
	// host fence MUST replace this with an empty payload + metadata_only
	// frame; the e2e test asserts the leak does not reach the caller.
	fabricatedPayload = "LEAKED PLAINTEXT"
)

// fixtureCache holds the most-recently-ingested AuditEventRequest row.
// Honest mode replays it byte-equal; malicious mode ignores it. The
// pointer is package-global because the gRPC server adapter constructs
// a fresh fixtureAuditServer on each registration but tests use a
// single plugin process.
var fixtureCache atomic.Pointer[pluginv1.AuditRow]

// fixtureAuditServer implements pluginv1.PluginAuditServiceServer. Mode
// selection is per-call via the QueryHistory request's subject — see
// the package doc.
type fixtureAuditServer struct {
	pluginv1.UnimplementedPluginAuditServiceServer
}

// AuditEvent caches the incoming row so a subsequent honest-mode
// QueryHistory can replay it byte-equal. Malicious mode ignores the
// cache (it fabricates its own row).
func (s *fixtureAuditServer) AuditEvent(_ context.Context, req *pluginv1.AuditEventRequest) (*pluginv1.AuditEventResponse, error) {
	if req != nil && req.GetRow() != nil {
		fixtureCache.Store(req.GetRow())
	}
	return &pluginv1.AuditEventResponse{}, nil
}

// QueryHistory dispatches to honest or malicious branch based on the
// subject substring. Unknown subjects return an empty stream — the host
// reader treats this as a clean EOF and yields zero rows.
func (s *fixtureAuditServer) QueryHistory(req *pluginv1.QueryHistoryRequest, stream pluginv1.PluginAuditService_QueryHistoryServer) error {
	subject := req.GetSubject()
	switch {
	case strings.Contains(subject, modeMarkerMalicious):
		// Fabricate a downgrade row: codec=identity + cleartext payload
		// for an always-sensitive event type. The host fence MUST refuse
		// this per-row (INV-P7-7).
		return stream.Send(&pluginv1.QueryHistoryResponse{
			Row: &pluginv1.AuditRow{
				// 16-byte raw ULID; plugin_router.go:191 falls back to
				// raw-bytes interpretation when ulid.Parse fails.
				Id:        []byte("0123456789ABCDEF"),
				Subject:   fabricatedSubject,
				Type:      fabricatedEventType,
				Codec:     "identity",
				Payload:   []byte(fabricatedPayload),
				SchemaVer: 1,
			},
		})
	case strings.Contains(subject, modeMarkerHonest):
		row := fixtureCache.Load()
		if row == nil {
			// Cache miss — return EOF cleanly so the test gets a
			// deterministic "no rows" rather than a hung stream.
			return nil
		}
		return stream.Send(&pluginv1.QueryHistoryResponse{Row: row})
	default:
		// Unknown subject — return EOF cleanly.
		return nil
	}
}

// fixturePlugin is the SDK-side wrapper. The SDK's ServeWithServices
// requires a Handler (HandleEvent) and a ServiceProvider
// (RegisterServices + Init); both are no-op for this fixture, which
// only registers the PluginAuditService.
type fixturePlugin struct {
	auditSrv *fixtureAuditServer
}

// HandleEvent is a no-op — the fixture only services QueryHistory.
func (p *fixturePlugin) HandleEvent(_ context.Context, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	return nil, nil
}

// RegisterServices registers the PluginAuditService on the go-plugin
// gRPC transport so the host can call AuditEvent and QueryHistory.
func (p *fixturePlugin) RegisterServices(registrar grpc.ServiceRegistrar) {
	pluginv1.RegisterPluginAuditServiceServer(registrar, p.auditSrv)
}

// Init is a no-op — the fixture needs no DB or external service wiring.
func (p *fixturePlugin) Init(_ context.Context, _ *pluginv1.ServiceConfig) error {
	return nil
}

func main() {
	plugin := &fixturePlugin{
		auditSrv: &fixtureAuditServer{},
	}
	pluginsdk.ServeWithServices(
		&pluginsdk.ServeConfig{Handler: plugin},
		plugin,
	)
}
