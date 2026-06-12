// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

type evalTestServer struct {
	hostv1.UnimplementedEvalServiceServer
	gotAction, gotResource string
	gotToken               string
	allow                  bool
}

func (s *evalTestServer) Evaluate(ctx context.Context, req *hostv1.EvaluateRequest) (*hostv1.EvaluateResponse, error) {
	s.gotAction = req.GetAction()
	s.gotResource = req.GetResource()
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if tokens := md.Get(emitTokenHeader); len(tokens) > 0 {
			s.gotToken = tokens[0]
		}
	}
	return &hostv1.EvaluateResponse{Allowed: s.allow, Reason: "ok", MatchedPolicy: "p1"}, nil
}

// startEvalServiceTestServer starts an in-process gRPC server that registers
// EvalService from the given test double. The returned *grpc.ClientConn is
// ready to use and cleaned up via t.Cleanup.
func startEvalServiceTestServer(t *testing.T, srv *evalTestServer) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- bufconn test server
	hostv1.RegisterEvalServiceServer(server, srv)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	conn, err := grpc.NewClient(
		"passthrough:///eval-service-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()), // nosemgrep: go.grpc.tls.grpc-client-new-insecure-connection.grpc-client-new-insecure-connection -- bufconn test client
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestHostEvaluateClient(t *testing.T) {
	type tc struct {
		name string
		// buildCtx, if non-nil, wraps context.Background() before the call.
		buildCtx  func() context.Context
		nilClient bool
		allow     bool
		action    string
		resource  string
		// wantErr asserts that Evaluate returns an error.
		wantErr bool
		// wantAllowed is only consulted when wantErr is false.
		wantAllowed  bool
		wantReason   string
		wantPolicy   string
		wantAction   string
		wantResource string
		wantToken    string
	}

	tests := []tc{
		{
			name:         "forwards action and resource, returns decision fields",
			allow:        true,
			action:       "extend_publish_attempts",
			resource:     "scene:01SCENE",
			wantAllowed:  true,
			wantReason:   "ok",
			wantPolicy:   "p1",
			wantAction:   "extend_publish_attempts",
			wantResource: "scene:01SCENE",
		},
		{
			name:      "nil client fails closed",
			nilClient: true,
			action:    "read",
			resource:  "scene:01SCENE",
			wantErr:   true,
		},
		{
			name:  "ferries dispatch token from incoming command context",
			allow: true,
			buildCtx: func() context.Context {
				return metadata.NewIncomingContext(
					context.Background(),
					metadata.Pairs(emitTokenHeader, "dispatch-token-abc"),
				)
			},
			action:       "pause",
			resource:     "scene:01SCENE",
			wantAllowed:  true,
			wantToken:    "dispatch-token-abc",
			wantAction:   "pause",
			wantResource: "scene:01SCENE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &evalTestServer{allow: tt.allow}

			var client *hostEvaluateClient
			if tt.nilClient {
				client = &hostEvaluateClient{}
			} else {
				conn := startEvalServiceTestServer(t, srv)
				client = &hostEvaluateClient{client: hostv1.NewEvalServiceClient(conn)}
			}

			ctx := context.Background()
			if tt.buildCtx != nil {
				ctx = tt.buildCtx()
			}

			dec, err := client.Evaluate(ctx, tt.action, tt.resource)

			if tt.wantErr {
				require.Error(t, err)
				assert.False(t, dec.Allowed)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantAllowed, dec.Allowed)
			if tt.wantReason != "" {
				assert.Equal(t, tt.wantReason, dec.Reason)
			}
			if tt.wantPolicy != "" {
				assert.Equal(t, tt.wantPolicy, dec.MatchedPolicy)
			}
			if tt.wantAction != "" {
				assert.Equal(t, tt.wantAction, srv.gotAction)
			}
			if tt.wantResource != "" {
				assert.Equal(t, tt.wantResource, srv.gotResource)
			}
			if tt.wantToken != "" {
				assert.Equal(t, tt.wantToken, srv.gotToken,
					"Evaluate MUST ferry the dispatch token from the incoming command context to the host")
			}
		})
	}
}
