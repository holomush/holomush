// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/pkg/errutil"
)

// TestTheServerFactoryRefusesToBuildAnUngatedServer is what makes the gate's
// mount STRUCTURAL rather than remembered.
//
// There is exactly one production Core/Portal gRPC server factory, and it
// cannot produce a server without the admin interceptor. A future composition
// that forgets the gate therefore fails to construct rather than serving the
// admin surface ungated — the failure mode is a boot error, not a silent
// authorization hole.
func TestTheServerFactoryRefusesToBuildAnUngatedServer(t *testing.T) {
	srv, err := NewGRPCServer(GRPCServerConfig{})

	require.Error(t, err, "a nil AdminInterceptor MUST be refused, never defaulted to pass-through")
	require.Nil(t, srv, "no server may be returned alongside the refusal")
	errutil.AssertErrorCode(t, err, "GRPC_SERVER_ADMIN_GATE_MISSING")
}

// TestTheServerFactoryBuildsAServerWhenTheGateIsSupplied is the paired positive
// control: without it, the refusal above cannot be distinguished from a factory
// that never returns a server at all.
func TestTheServerFactoryBuildsAServerWhenTheGateIsSupplied(t *testing.T) {
	noop := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		return h(ctx, req)
	}

	srv, err := NewGRPCServer(GRPCServerConfig{AdminInterceptor: noop})

	require.NoError(t, err, "a TLS-less server is the bufconn test affordance and MUST build")
	require.NotNil(t, srv)
	srv.Stop()
}
