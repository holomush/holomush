// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package main implements connid_echo_plugin: a test-only binary plugin whose
// HandleCommand echoes the ConnectionID it received back in the response
// output. It is the fixture for connection_id_roundtrip_test.go, which proves
// that CommandRequest.connection_id survives the full
// host -> proto -> SDK-adapter -> handler round-trip over real go-plugin gRPC.
//
// Regression guard for holomush-dble7: the binary-plugin SDK receive adapter
// (pkg/plugin/sdk.go pluginServerAdapter.HandleCommand) once dropped
// ConnectionID when rebuilding pluginsdk.CommandRequest from the proto, which
// no struct-boundary unit test caught (the dispatcher test used a fake
// deliverer). This plugin drives the real adapter.
package main

import (
	"context"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// echoPlugin implements both Handler (required by Serve) and CommandHandler
// (so the SDK server adapter wires cmdHandler and routes HandleCommand here).
type echoPlugin struct{}

// HandleEvent is an unused no-op; this plugin only exercises the command path.
func (p *echoPlugin) HandleEvent(_ context.Context, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	return nil, nil
}

// HandleCommand echoes the received ConnectionID so the test can assert it
// survived the gRPC proto round-trip. The "connid=" prefix keeps the assertion
// unambiguous even when ConnectionID is empty (the legacy/scripted path).
func (p *echoPlugin) HandleCommand(_ context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return pluginsdk.OK("connid=" + req.ConnectionID), nil
}

func main() {
	pluginsdk.Serve(&pluginsdk.ServeConfig{Handler: &echoPlugin{}})
}
