// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"

	"buf.build/go/protovalidate"
	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Validator is the interface implemented by *protovalidate.Validator.
// We define a local interface so tests can substitute a fake validator
// without depending on the concrete protovalidate type.
type Validator interface {
	Validate(msg any) error
}

// validatorAdapter adapts protovalidate.Validator (which takes proto.Message)
// to our Validator interface (which takes any). The adapter type-asserts and
// returns nil for non-proto inputs, matching the interceptor's behavior of
// passing through non-proto requests.
type validatorAdapter struct {
	inner protovalidate.Validator
}

// Validate type-asserts msg to proto.Message and validates it. If msg is not
// a proto.Message, returns nil (validation is skipped for non-proto values).
func (a *validatorAdapter) Validate(msg any) error {
	pm, ok := msg.(proto.Message)
	if !ok {
		return nil
	}
	if err := a.inner.Validate(pm); err != nil {
		return oops.With("message_type", string(pm.ProtoReflect().Descriptor().FullName())).Wrap(err)
	}
	return nil
}

// NewDefaultValidator constructs a protovalidate.Validator wrapped in our
// local Validator interface. Plugins may use this directly or substitute
// their own Validator implementation via ServeConfig.Validator.
//
// The validator is stateless after construction (it caches compiled rules
// per-message-type on first encounter), so a single instance can be shared
// across all plugin handlers.
func NewDefaultValidator() (Validator, error) {
	v, err := protovalidate.New()
	if err != nil {
		return nil, oops.Wrap(err)
	}
	return &validatorAdapter{inner: v}, nil
}

// ValidateInterceptor returns a gRPC unary server interceptor that validates
// inbound proto messages using the supplied Validator. Non-proto requests
// pass through unchanged. Validation failures are mapped to gRPC
// InvalidArgument with the validator's error message attached.
//
// Per spec section 10.3, validation failures are user-facing errors and do
// not constitute service degradation. Handlers do not need to log them
// (the gRPC status response is sufficient).
func ValidateInterceptor(v Validator) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if err := v.Validate(req); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "request validation failed: %v", err)
		}
		return handler(ctx, req)
	}
}
