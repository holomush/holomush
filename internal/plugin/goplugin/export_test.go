// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import "github.com/holomush/holomush/internal/access/policy/types"

// EngineForTest exposes the host's internal engine field for wiring-guard
// tests. This accessor exists so that external test packages (e.g.
// internal/plugin/setup) can verify that goplugin.WithEngine correctly
// propagates the engine into the host without relying on gRPC round-trips.
// It MUST NOT be used in production code.
func (h *Host) EngineForTest() types.AccessPolicyEngine {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.engine
}
