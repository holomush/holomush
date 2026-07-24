// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"

	"github.com/oklog/ulid/v2"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// This file exposes the unexported members of the extracted units to the
// external `package grpc_test` suites. It carries no production code and is
// compiled only under `go test`.
//
// Why this shape: ARCH-01 success criterion 1 (D-02) requires each extracted
// unit to be constructible and exercisable from OUTSIDE the package, with only
// its own collaborators. Keeping the cluster's helpers unexported preserves the
// production surface (no new exported API on `package grpc`) while still letting
// the proof tests reach them. Same pattern 08-02 used for
// internal/plugin/export_test.go.

// ExportToSubject exposes (*SubscribeHandler).toSubject.
func ExportToSubject(h *SubscribeHandler, gameID, streamName string) (eventbus.Subject, error) {
	return h.toSubject(gameID, streamName)
}

// ExportComputeInitialFilters exposes (*SubscribeHandler).computeInitialFilters.
func ExportComputeInitialFilters(ctx context.Context, h *SubscribeHandler, plan focus.RestorePlan) []eventbus.Subject {
	return h.computeInitialFilters(ctx, plan)
}

// ExportToProtoSubscribeResponse exposes (*SubscribeHandler).toProtoSubscribeResponse.
func ExportToProtoSubscribeResponse(h *SubscribeHandler, ev eventbus.Event, metadataOnly bool) *corev1.SubscribeResponse {
	return h.toProtoSubscribeResponse(ev, metadataOnly)
}

// ExportEmitCommandResponse exposes (*CommandHandler).emitCommandResponse.
func ExportEmitCommandResponse(ctx context.Context, h *CommandHandler, char core.CharacterRef, text string, isError bool) error {
	return h.emitCommandResponse(ctx, char, text, isError)
}

// ExportExecuteCommand exposes (*CommandHandler).executeCommand.
func ExportExecuteCommand(ctx context.Context, h *CommandHandler, info *session.Info, input, connectionIDStr string) error {
	return h.executeCommand(ctx, info, input, connectionIDStr)
}

// ExportRunDisconnectHooks exposes (*LifecycleHandler).runDisconnectHooks.
func ExportRunDisconnectHooks(ctx context.Context, h *LifecycleHandler, info session.Info) {
	h.runDisconnectHooks(ctx, info)
}

// ExportDispatchDelivery exposes (*SubscribeHandler).dispatchDelivery. The
// locationFollower argument is fixed at nil: it is an unexported type, and the
// nil path is the one the badge-downgrade and identity-fallback proofs need.
func ExportDispatchDelivery(
	ctx context.Context,
	h *SubscribeHandler,
	info *session.Info,
	delivery eventbus.Delivery,
	stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
	connID *ulid.ULID,
) error {
	return h.dispatchDelivery(ctx, info, delivery, stream, nil, connID)
}
