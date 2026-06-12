// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"github.com/samber/oops"
	"google.golang.org/grpc"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostcap"
)

// pluginEndpoint holds an in-process gRPC server that serves the Lua capability
// set (hostcap.LuaDefaultSet) for a single named plugin. The endpoint is created
// once per plugin at Load time and torn down when the plugin is unloaded or the
// host closes. *plugins.InProcessConn directly satisfies grpc.ClientConnInterface
// (Invoke/NewStream/Close — verified at internal/plugin/inprocess_conn.go:62,67,73),
// so the generated hostv1 client stubs accept ep.Conn() without an adapter.
type pluginEndpoint struct {
	conn *plugins.InProcessConn
}

// newPluginEndpoint creates a per-plugin bufconn endpoint serving the Lua
// capability set. It builds a *grpc.Server, registers the LuaDefaultSet
// capability servers (INV-PLUGIN-49), and wraps the server with an in-process
// bufconn listener. The caller must call Close when the plugin is unloaded.
func newPluginEndpoint(adapter hostcap.HostCapabilities, pluginName string) (*pluginEndpoint, error) {
	// The server is served exclusively over an in-memory bufconn listener
	// (plugins.NewInProcessConn below), never a network socket — there is no
	// wire to encrypt or tamper with. TLS credentials are therefore N/A; this
	// mirrors the client half of the same transport, which carries the matching
	// nosemgrep on insecure.NewCredentials (internal/plugin/inprocess_conn.go).
	srv := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection
	hostcap.RegisterCapabilities(srv, hostcap.NewBase(adapter, pluginName), hostcap.LuaDefaultSet)
	conn, err := plugins.NewInProcessConn(srv)
	if err != nil {
		return nil, oops.In("lua").With("plugin", pluginName).Wrap(err)
	}
	return &pluginEndpoint{conn: conn}, nil
}

// Conn returns the in-process client connection. *InProcessConn already satisfies
// grpc.ClientConnInterface directly, so the generated hostv1 clients accept it
// without a wrapper (no .Conn() accessor needed on InProcessConn itself).
func (e *pluginEndpoint) Conn() grpc.ClientConnInterface { return e.conn }

// Close shuts down the in-process gRPC server and listener. Idempotent with
// respect to the backing InProcessConn.Close semantics.
func (e *pluginEndpoint) Close() error {
	return e.conn.Close() //nolint:wrapcheck // InProcessConn.Close is a peer helper in the same module; already oops-wrapped at its boundary
}
