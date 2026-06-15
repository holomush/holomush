// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"

	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/plugin/dispatchwire"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// dispatchMetadataKey aliases the single shared wire key (dispatchwire.MetadataKey)
// so in-package references keep one canonical value across runtimes.
const dispatchMetadataKey = dispatchwire.MetadataKey

// newDispatchOutgoingInterceptor returns a client-side unary interceptor that
// marshals the host-vouched dispatch context from the CALL ctx into outgoing
// gRPC metadata, so it survives the Lua per-plugin bufconn boundary.
//
// Lua plugins run in-process, so the host-vouched dispatch is on the call ctx
// VALUE (pluginauthz.DispatchForHost, set only by Host.stampDispatch). The
// envelope is built solely from that value; the interceptor unconditionally
// REPLACES any existing reserved-key value (host-vouched only), so a plugin
// cannot smuggle forged scope attributes — absent or forged dispatch reaches the
// server as "no dispatch" and the scope interceptor denies (INV-PLUGIN-51).
func newDispatchOutgoingInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if dc, ok := pluginauthz.DispatchForHost(ctx); ok {
			ctx = dispatchwire.AttachOutgoing(ctx, dc)
		} else {
			// No host-vouched dispatch: strip any plugin-injected reserved key so
			// it is never forwarded (fail-closed, not fail-open).
			ctx = dispatchwire.StripOutgoing(ctx)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// newDispatchStampInterceptor is the server-side companion that reconstructs the
// host-vouched dispatch context from incoming metadata before the capability /
// scope interceptor runs. It delegates to the shared dispatchwire codec so the
// Lua and binary runtimes reconstruct dispatch identically (INV-PLUGIN-51/52).
func newDispatchStampInterceptor() grpc.UnaryServerInterceptor {
	return dispatchwire.StampInterceptor()
}
