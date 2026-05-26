// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

type evalTestServer struct {
	pluginv1.UnimplementedPluginHostServiceServer
	gotAction, gotResource string
	gotToken               string
	allow                  bool
}

func (s *evalTestServer) Evaluate(ctx context.Context, req *pluginv1.PluginHostServiceEvaluateRequest) (*pluginv1.PluginHostServiceEvaluateResponse, error) {
	s.gotAction = req.GetAction()
	s.gotResource = req.GetResource()
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if tokens := md.Get(emitTokenHeader); len(tokens) > 0 {
			s.gotToken = tokens[0]
		}
	}
	return &pluginv1.PluginHostServiceEvaluateResponse{Allowed: s.allow, Reason: "ok", MatchedPolicy: "p1"}, nil
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
				conn := startPluginHostServiceTestServer(t, srv)
				client = &hostEvaluateClient{client: pluginv1.NewPluginHostServiceClient(conn)}
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
