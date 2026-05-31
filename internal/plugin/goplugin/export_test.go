// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/settings"
)

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

// AuditorForTest exposes the host's internal auditor field for wiring-guard
// tests. This accessor exists so that external test packages (e.g.
// internal/plugin/setup) can verify that goplugin.WithAuditLogger correctly
// propagates the auditor into the host without relying on gRPC round-trips.
// It MUST NOT be used in production code.
func (h *Host) AuditorForTest() pluginauthz.Auditor {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.auditor
}

// PlayerSettingsForTest exposes the host's internal playerSettings field for
// wiring-guard tests. Confirms that SetSettingsStores (and WithPlayerSettings)
// correctly propagates the store into the host so GetSetting / SetSetting RPCs
// do not nil-deref on the PLAYER path. It MUST NOT be used in production code.
func (h *Host) PlayerSettingsForTest() settings.PlayerSettingsStore {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.playerSettings
}

// CharacterSettingsForTest exposes the host's internal characterSettings field
// for wiring-guard tests. Mirrors PlayerSettingsForTest for the CHARACTER path.
// It MUST NOT be used in production code.
func (h *Host) CharacterSettingsForTest() settings.CharacterSettingsStore {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.characterSettings
}

// GameSettingsForTest exposes the host's internal gameSettings field for
// wiring-guard tests. Mirrors PlayerSettingsForTest for the GAME path.
// It MUST NOT be used in production code.
func (h *Host) GameSettingsForTest() settings.GameSettings {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.gameSettings
}
