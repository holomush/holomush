// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import "github.com/holomush/holomush/internal/grpc/focus"

// FocusStreamCoordinatorOptions is the SINGLE assembly point for the two
// registry-derived focus.Coordinator senders (INV-FS-4): the session-wide
// StreamSender and the per-Connection ConnectionSender, both backed by the
// same SessionStreamRegistry. Production (cmd/holomush) and the integration
// harness MUST build their coordinator stream wiring through this helper rather
// than hand-rolling NewStreamSenderAdapter + NewConnectionSenderAdapter, so the
// harness is a faithful production mirror by construction.
func FocusStreamCoordinatorOptions(reg *SessionStreamRegistry) []focus.CoordinatorOption {
	return []focus.CoordinatorOption{
		focus.WithStreamSender(NewStreamSenderAdapter(reg)),
		focus.WithConnectionSender(NewConnectionSenderAdapter(reg)),
	}
}
