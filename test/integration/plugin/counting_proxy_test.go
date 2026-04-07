// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
	"context"
	"sync/atomic"

	"google.golang.org/grpc"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// countingAttributeResolverClient wraps a real AttributeResolverServiceClient
// and counts calls to each RPC. Used in plugin ABAC hardening integration
// tests to assert that ResolveResource is (or is not) invoked during a
// particular authorization code path.
//
// All counters are atomic so the proxy is safe under concurrent access
// (engine.Evaluate may be called from multiple goroutines in some test
// scenarios).
type countingAttributeResolverClient struct {
	inner                pluginv1.AttributeResolverServiceClient
	getSchemaCalls       atomic.Int64
	resolveResourceCalls atomic.Int64
}

func newCountingAttributeResolverClient(
	inner pluginv1.AttributeResolverServiceClient,
) *countingAttributeResolverClient {
	return &countingAttributeResolverClient{inner: inner}
}

func (c *countingAttributeResolverClient) GetSchema(
	ctx context.Context,
	req *pluginv1.GetSchemaRequest,
	opts ...grpc.CallOption,
) (*pluginv1.GetSchemaResponse, error) {
	c.getSchemaCalls.Add(1)
	return c.inner.GetSchema(ctx, req, opts...)
}

func (c *countingAttributeResolverClient) ResolveResource(
	ctx context.Context,
	req *pluginv1.ResolveResourceRequest,
	opts ...grpc.CallOption,
) (*pluginv1.ResolveResourceResponse, error) {
	c.resolveResourceCalls.Add(1)
	return c.inner.ResolveResource(ctx, req, opts...)
}

// ResolveResourceCallCount returns the number of ResolveResource calls
// observed so far. Use this in test assertions.
func (c *countingAttributeResolverClient) ResolveResourceCallCount() int64 {
	return c.resolveResourceCalls.Load()
}

// GetSchemaCallCount returns the number of GetSchema calls observed so far.
func (c *countingAttributeResolverClient) GetSchemaCallCount() int64 {
	return c.getSchemaCalls.Load()
}

// ResetCallCounts resets all counters to zero. Use between phases of a
// single test (e.g., after BeforeEach setup completes so the test body
// measures only the activity it triggers).
func (c *countingAttributeResolverClient) ResetCallCounts() {
	c.getSchemaCalls.Store(0)
	c.resolveResourceCalls.Store(0)
}
