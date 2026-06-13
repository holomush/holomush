// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/pkg/errutil"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

func okHandler(_ context.Context, _ any) (any, error) { return struct{}{}, nil }

// ctxWithDispatch returns a ctx carrying a host-vouched dispatch context. The
// static half does not read it; it is here so Task 10's scope tests can reuse it.
func ctxWithDispatch(t *testing.T) context.Context {
	t.Helper()
	return pluginauthz.WithDispatch(context.Background(), pluginauthz.DispatchContext{Subject: "character:01TEST"})
}

func TestInterceptorAccessReadDeniesWriteMethod(t *testing.T) {
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         policytest.AllowAllEngine(),
		DeclaredAccess: func(_, _ string) (string, bool) { return "read", true },
	})
	_, err := ic(ctxWithDispatch(t), &hostv1.SetRequest{}, &grpc.UnaryServerInfo{
		FullMethod: "/holomush.plugin.host.v1.KVService/Set",
	}, okHandler)
	errutil.AssertErrorCode(t, err, "ACCESS_CLASS_DENIED")
}

func TestInterceptorAbsentDeclaredAccessPermitsWrite(t *testing.T) {
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         policytest.AllowAllEngine(),
		DeclaredAccess: func(_, _ string) (string, bool) { return "", true }, // declared, no access narrowing
	})
	_, err := ic(ctxWithDispatch(t), &hostv1.SetRequest{}, &grpc.UnaryServerInfo{
		FullMethod: "/holomush.plugin.host.v1.KVService/Set",
	}, okHandler)
	require.NoError(t, err)
}

func TestInterceptorUndeclaredCapabilityDenied(t *testing.T) {
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         policytest.AllowAllEngine(),
		DeclaredAccess: func(_, _ string) (string, bool) { return "", false }, // not declared
	})
	_, err := ic(ctxWithDispatch(t), &hostv1.GetRequest{}, &grpc.UnaryServerInfo{
		FullMethod: "/holomush.plugin.host.v1.KVService/Get",
	}, okHandler)
	errutil.AssertErrorCode(t, err, "CAPABILITY_NOT_DECLARED")
}

func TestInterceptorNonHostMethodPassesThrough(t *testing.T) {
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         policytest.AllowAllEngine(),
		DeclaredAccess: func(_, _ string) (string, bool) { return "", false },
	})
	resp, err := ic(context.Background(), struct{}{}, &grpc.UnaryServerInfo{
		FullMethod: "/some.other.Service/Method",
	}, okHandler)
	require.NoError(t, err)
	require.NotNil(t, resp) // handler ran
}

func TestClassifyHostMethodResolvesKnownService(t *testing.T) {
	capToken, method, ok := classifyHostMethod("/holomush.plugin.host.v1.KVService/Set")
	require.True(t, ok)
	require.Equal(t, "kv", capToken)
	require.Equal(t, "Set", method)
}

func TestClassifyHostMethodRejectsNonHostMethod(t *testing.T) {
	_, _, ok := classifyHostMethod("/some.other.Service/Method")
	require.False(t, ok)
}
