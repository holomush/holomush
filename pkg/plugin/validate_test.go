// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

func TestNewDefaultValidatorReturnsValidatorWithoutError(t *testing.T) {
	v, err := NewDefaultValidator()
	require.NoError(t, err)
	require.NotNil(t, v)
}

func TestValidateInterceptorPassesValidProtoMessage(t *testing.T) {
	v, err := NewDefaultValidator()
	require.NoError(t, err)

	interceptor := ValidateInterceptor(v)

	// Use any proto message — without annotations protovalidate accepts everything.
	req := &pluginv1.GetSchemaRequest{}

	var handlerCalled bool
	handler := func(_ context.Context, _ any) (any, error) {
		handlerCalled = true
		return "ok", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/holomush.test/Foo"}
	resp, err := interceptor(context.Background(), req, info, handler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.True(t, handlerCalled, "handler should be invoked when validation passes")
}

func TestValidateInterceptorPassesNonProtoRequest(t *testing.T) {
	v, err := NewDefaultValidator()
	require.NoError(t, err)

	interceptor := ValidateInterceptor(v)

	// Non-proto value (e.g., int) — interceptor should pass it through to handler.
	var handlerCalled bool
	handler := func(_ context.Context, _ any) (any, error) {
		handlerCalled = true
		return "ok", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/holomush.test/Foo"}
	_, err = interceptor(context.Background(), 42, info, handler)
	require.NoError(t, err)
	assert.True(t, handlerCalled, "handler should be invoked even for non-proto requests")
}

func TestValidateInterceptorPropagatesHandlerError(t *testing.T) {
	v, err := NewDefaultValidator()
	require.NoError(t, err)

	interceptor := ValidateInterceptor(v)

	sentinel := errors.New("handler boom")
	handler := func(_ context.Context, _ any) (any, error) {
		return nil, sentinel
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/holomush.test/Foo"}
	_, err = interceptor(context.Background(), &pluginv1.GetSchemaRequest{}, info, handler)
	require.Error(t, err)
	assert.True(t, errors.Is(err, sentinel), "handler error should propagate unchanged")
}

func TestValidateInterceptorReturnsInvalidArgumentForFailedValidation(t *testing.T) {
	// Isolated check on the interceptor's error-mapping path: inject a
	// validator that always fails and confirm the interceptor converts
	// its error into gRPC InvalidArgument. This exercises the mapping
	// logic independently of any specific annotated proto, so a future
	// refactor of the default validator cannot silently regress the
	// InvalidArgument contract.
	interceptor := ValidateInterceptor(&alwaysFailValidator{})

	handler := func(_ context.Context, _ any) (any, error) {
		t.Fatal("handler should not be called when validation fails")
		return nil, nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/holomush.test/Foo"}
	_, err := interceptor(context.Background(), &pluginv1.GetSchemaRequest{}, info, handler)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestValidateInterceptorRejectsAnnotatedProtoViolatingConstraints(t *testing.T) {
	// End-to-end integration check: the default validator (constructed via
	// NewDefaultValidator → buf.build/go/protovalidate) MUST reject a real
	// annotated proto message whose fields violate their buf.validate
	// constraints. This pins the full path — validator construction,
	// constraint parsing, and the interceptor's InvalidArgument mapping —
	// so a regression in any link is caught by one test.
	//
	// CommandRequest.character_id carries (buf.validate.field).string.min_len = 1,
	// so an empty value is the minimal invalid fixture.
	v, err := NewDefaultValidator()
	require.NoError(t, err)

	interceptor := ValidateInterceptor(v)

	handler := func(_ context.Context, _ any) (any, error) {
		t.Fatal("handler should not be called when validation rejects the request")
		return nil, nil
	}

	invalid := &pluginv1.CommandRequest{
		Command: "scene",
		// CharacterID intentionally left empty to trigger min_len rejection.
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/holomush.plugin.v1.PluginService/HandleCommand"}
	_, err = interceptor(context.Background(), invalid, info, handler)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "character_id",
		"error message should identify the violating field")
}

// alwaysFailValidator is a test double that always returns a validation error.
type alwaysFailValidator struct{}

func (a *alwaysFailValidator) Validate(any) error {
	return errors.New("forced validation failure for test")
}
