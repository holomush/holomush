// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// grpcToConnectCode maps gRPC status codes to connect codes.
// Transient or unclassified codes default to CodeInternal — a clear server
// error, not CodeUnknown / HTTP 500, which conflates all failure modes.
func grpcToConnectCode(c codes.Code) connect.Code {
	switch c {
	case codes.PermissionDenied:
		return connect.CodePermissionDenied
	case codes.NotFound:
		return connect.CodeNotFound
	case codes.Unauthenticated:
		return connect.CodeUnauthenticated
	case codes.InvalidArgument:
		return connect.CodeInvalidArgument
	case codes.FailedPrecondition:
		return connect.CodeFailedPrecondition
	case codes.Unimplemented:
		return connect.CodeUnimplemented
	case codes.AlreadyExists:
		return connect.CodeAlreadyExists
	case codes.ResourceExhausted:
		return connect.CodeResourceExhausted
	case codes.Unavailable:
		return connect.CodeUnavailable
	case codes.DeadlineExceeded:
		return connect.CodeDeadlineExceeded
	case codes.Canceled:
		return connect.CodeCanceled
	default:
		return connect.CodeInternal
	}
}

// statusTranslationInterceptor returns a connect interceptor that translates
// gRPC *status.Status errors (including those wrapped in oops errors) into
// *connect.Error values with the appropriate connect code.
//
// This is the protocol-translation layer at the gateway boundary (gateway-boundary.md):
// web handlers call into gRPC clients which return oops-wrapped grpc status errors
// of the form oops.Code("RPC_FAILED").Wrap(grpcStatusErr). connect-go does not
// recognise a grpc-go *status.Status nor an oops wrap as a *connect.Error, so
// without this interceptor every distinct status code (PermissionDenied, NotFound,
// Unauthenticated, …) collapses to CodeUnknown / HTTP 500 at the browser.
//
// Opacity: the interceptor uses st.Message() as the connect error message.
// That is already the safe, client-facing outer message produced by the core
// server's gRPC handler (grpc-errors.md: internal errors are logged, not
// forwarded). The oops chain / inner error text is never surfaced.
func statusTranslationInterceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			resp, err := next(ctx, req)
			if err == nil {
				return resp, nil
			}

			// Already a *connect.Error — pass through unchanged (e.g., errors
			// produced directly by handlers with connect.NewError).
			var ce *connect.Error
			if errors.As(err, &ce) {
				return nil, err
			}

			// Walk the error chain for a grpc status. The chain is typically:
			//   oops.Error (Code="RPC_FAILED") → *status.Status
			// errors.As walks through oops's Unwrap chain to find any type
			// implementing GRPCStatus() *status.Status.
			type grpcStatuser interface {
				GRPCStatus() *status.Status
			}
			var gs grpcStatuser
			if errors.As(err, &gs) {
				st := gs.GRPCStatus()
				return nil, connect.NewError(grpcToConnectCode(st.Code()), errors.New(st.Message()))
			}

			// No grpc status in chain — return unchanged; connect-go will map
			// it to CodeUnknown, which is correct for a truly unclassified error.
			return nil, err
		}
	})
}
