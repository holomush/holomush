// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/web/v1/webv1connect"
)

// newInterceptorTestServer starts an httptest server with the web handler
// mounted with the statusTranslationInterceptor (matching server.go), and
// returns a connect client pointing at it plus a cleanup function.
// This mirrors the pattern used by newStreamEventsServer in handler_test.go.
func newInterceptorTestServer(t *testing.T, mc *mockCoreClient, sc *mockSceneAccessClient) (webv1connect.WebServiceClient, func()) {
	t.Helper()
	h := NewHandler(mc, WithSceneAccessClient(sc))
	return newInterceptorTestServerFromHandler(t, h)
}

// newInterceptorTestServerFromHandler is the low-level helper used when the
// caller needs to control handler construction (e.g. omit WithSceneAccessClient).
func newInterceptorTestServerFromHandler(t *testing.T, h *Handler) (webv1connect.WebServiceClient, func()) {
	t.Helper()
	_, httpHandler := webv1connect.NewWebServiceHandler(
		h,
		connect.WithInterceptors(statusTranslationInterceptor()),
	)
	srv := httptest.NewServer(httpHandler)
	client := webv1connect.NewWebServiceClient(http.DefaultClient, srv.URL)
	return client, srv.Close
}

// --- grpcToConnectCode unit tests ---

func TestGrpcToConnectCodeMapsKnownCodes(t *testing.T) {
	tests := []struct {
		name     string
		grpc     codes.Code
		expected connect.Code
	}{
		{"maps PermissionDenied", codes.PermissionDenied, connect.CodePermissionDenied},
		{"maps NotFound", codes.NotFound, connect.CodeNotFound},
		{"maps Unauthenticated", codes.Unauthenticated, connect.CodeUnauthenticated},
		{"maps InvalidArgument", codes.InvalidArgument, connect.CodeInvalidArgument},
		{"maps FailedPrecondition", codes.FailedPrecondition, connect.CodeFailedPrecondition},
		{"maps Unimplemented", codes.Unimplemented, connect.CodeUnimplemented},
		{"maps AlreadyExists", codes.AlreadyExists, connect.CodeAlreadyExists},
		{"maps ResourceExhausted", codes.ResourceExhausted, connect.CodeResourceExhausted},
		{"maps Unavailable", codes.Unavailable, connect.CodeUnavailable},
		{"maps DeadlineExceeded", codes.DeadlineExceeded, connect.CodeDeadlineExceeded},
		{"maps Canceled", codes.Canceled, connect.CodeCanceled},
		{"maps Unknown to CodeInternal", codes.Unknown, connect.CodeInternal},
		{"maps Internal to CodeInternal", codes.Internal, connect.CodeInternal},
		{"maps OK to CodeInternal as safe default", codes.OK, connect.CodeInternal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, grpcToConnectCode(tc.grpc))
		})
	}
}

// --- interceptor integration tests via httptest roundtrip ---

// TestStatusInterceptorTranslatesOopsWrappedPermissionDeniedToConnectCode asserts
// that a grpc PermissionDenied wrapped in oops (the real client.go error shape)
// reaches the browser as connect.CodePermissionDenied, not CodeUnknown/HTTP 500.
func TestStatusInterceptorTranslatesOopsWrappedPermissionDeniedToConnectCode(t *testing.T) {
	// Simulate the error shape produced by internal/grpc/client.go:
	//   oops.Code("RPC_FAILED").Wrap(status.Error(codes.PermissionDenied, ...))
	grpcErr := status.Error(codes.PermissionDenied, "guests cannot access scenes")
	wrappedErr := oops.Code("RPC_FAILED").With("method", "ListScenesForViewer").Wrap(grpcErr)

	sc := &mockSceneAccessClient{listScenesErr: wrappedErr}
	client, cleanup := newInterceptorTestServer(t, &mockCoreClient{}, sc)
	defer cleanup()

	_, err := client.WebListScenes(context.Background(),
		connect.NewRequest(&webv1.WebListScenesRequest{SessionId: "sess-guest"}))

	require.Error(t, err)
	var ce *connect.Error
	require.ErrorAs(t, err, &ce, "error must be a *connect.Error after interceptor translation")
	assert.Equal(t, connect.CodePermissionDenied, ce.Code(),
		"oops-wrapped PermissionDenied must reach client as CodePermissionDenied, not CodeUnknown")
	assert.Equal(t, "guests cannot access scenes", ce.Message(),
		"client-safe grpc status message must be preserved; oops chain must not be leaked")
}

// TestStatusInterceptorTranslatesOopsWrappedNotFoundToConnectCode asserts that
// a grpc NotFound wrapped in oops reaches the client as connect.CodeNotFound.
func TestStatusInterceptorTranslatesOopsWrappedNotFoundToConnectCode(t *testing.T) {
	grpcErr := status.Error(codes.NotFound, "scene not found")
	wrappedErr := oops.Code("RPC_FAILED").With("method", "GetSceneForViewer").Wrap(grpcErr)

	sc := &mockSceneAccessClient{getSceneErr: wrappedErr}
	client, cleanup := newInterceptorTestServer(t, &mockCoreClient{}, sc)
	defer cleanup()

	_, err := client.WebGetScene(context.Background(),
		connect.NewRequest(&webv1.WebGetSceneRequest{SessionId: "s", SceneId: "no-such"}))

	require.Error(t, err)
	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, connect.CodeNotFound, ce.Code())
	assert.Equal(t, "scene not found", ce.Message())
}

// TestStatusInterceptorMapsUnclassifiedGrpcErrorToCodeInternal asserts that
// grpc codes without a direct connect mapping (e.g. codes.Internal) become
// connect.CodeInternal, not CodeUnknown, so callers see a clear "server error".
func TestStatusInterceptorMapsUnclassifiedGrpcErrorToCodeInternal(t *testing.T) {
	grpcErr := status.Error(codes.Internal, "internal server error")
	wrappedErr := oops.Code("RPC_FAILED").With("method", "WebListScenes").Wrap(grpcErr)

	sc := &mockSceneAccessClient{listScenesErr: wrappedErr}
	client, cleanup := newInterceptorTestServer(t, &mockCoreClient{}, sc)
	defer cleanup()

	_, err := client.WebListScenes(context.Background(),
		connect.NewRequest(&webv1.WebListScenesRequest{SessionId: "s"}))

	require.Error(t, err)
	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, connect.CodeInternal, ce.Code(),
		"unmapped grpc codes must become CodeInternal, never CodeUnknown")
}

// TestStatusInterceptorPassesThroughExistingConnectErrors asserts that errors
// already constructed as *connect.Error (e.g. from connect.NewError in handlers)
// pass through the interceptor unchanged.
func TestStatusInterceptorPassesThroughExistingConnectErrors(t *testing.T) {
	// An error produced directly by the handler (e.g. "sceneAccess is nil"):
	// NewHandler WITHOUT WithSceneAccessClient leaves h.sceneAccess == nil,
	// so WebListScenes returns connect.NewError(connect.CodeUnimplemented, …)
	// without ever reaching the gRPC client layer.
	h := NewHandler(&mockCoreClient{}) // no WithSceneAccessClient
	client, cleanup := newInterceptorTestServerFromHandler(t, h)
	defer cleanup()

	_, err := client.WebListScenes(context.Background(),
		connect.NewRequest(&webv1.WebListScenesRequest{}))

	require.Error(t, err)
	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, connect.CodeUnimplemented, ce.Code(),
		"existing *connect.Error must pass through interceptor unchanged")
}

// TestStatusInterceptorDoesNotLeakOopsChainInMessage asserts that the oops
// error context (method name, RPC_FAILED code, inner error details) is not
// included in the message visible to connect clients.
func TestStatusInterceptorDoesNotLeakOopsChainInMessage(t *testing.T) {
	grpcErr := status.Error(codes.PermissionDenied, "guests cannot access scenes")
	wrappedErr := oops.Code("RPC_FAILED").With("method", "ListScenesForViewer").With("secret", "internal-detail").Wrap(grpcErr)

	sc := &mockSceneAccessClient{listScenesErr: wrappedErr}
	client, cleanup := newInterceptorTestServer(t, &mockCoreClient{}, sc)
	defer cleanup()

	_, err := client.WebListScenes(context.Background(),
		connect.NewRequest(&webv1.WebListScenesRequest{SessionId: "s"}))

	require.Error(t, err)
	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	// Message must be exactly the grpc status message — no oops metadata leaked.
	assert.Equal(t, "guests cannot access scenes", ce.Message())
	assert.NotContains(t, ce.Message(), "RPC_FAILED",
		"oops error code must not leak to client")
	assert.NotContains(t, ce.Message(), "internal-detail",
		"oops context fields must not leak to client")
	// Confirm the oops chain is still intact on the server side (for our own
	// verification that errors.As genuinely walked through the oops wrap).
	assert.True(t, errors.Is(wrappedErr, grpcErr),
		"oops wrapping must preserve the grpc error in the chain")
}
