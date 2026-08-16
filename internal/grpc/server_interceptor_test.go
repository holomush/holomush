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

	srv, err := NewGRPCServer(GRPCServerConfig{AdminInterceptor: noop, AllowInsecure: true})

	require.NoError(t, err, "a TLS-less server is the bufconn test affordance and MUST build once it says so")
	require.NotNil(t, srv)
	srv.Stop()
}

// TestTheServerFactoryRefusesCleartextUnlessItIsAdmitted is the transport
// sibling of the two above, and it exists because the gate they cover was the
// only one this factory had.
//
// The constructor this factory replaced passed credentials.NewTLS(nil), which
// failed every handshake — serving in the clear was unreachable by accident.
// Guarding the authorization dependency with an error while letting a nil TLS
// fall through to no credentials would have converted "the TLS subsystem handed
// us nothing" from a boot failure into the whole core surface served in
// cleartext, with no error anywhere. Production never sets AllowInsecure, so a
// nil TLSProvider result refuses to build.
func TestTheServerFactoryRefusesCleartextUnlessItIsAdmitted(t *testing.T) {
	noop := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		return h(ctx, req)
	}

	srv, err := NewGRPCServer(GRPCServerConfig{AdminInterceptor: noop})

	require.Error(t, err,
		"a nil TLS without AllowInsecure MUST be refused, never defaulted to serving cleartext")
	require.Nil(t, srv, "no server may be returned alongside the refusal")
	errutil.AssertErrorCode(t, err, "GRPC_SERVER_TRANSPORT_INSECURE")
}
