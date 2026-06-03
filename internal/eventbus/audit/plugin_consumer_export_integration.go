// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package audit

import "github.com/holomush/holomush/internal/eventbus/codec"

// KeySelectorForTest exposes the manager's KeySelector to integration
// tests that assert INV-CRYPTO-45 pointer-identity with the hot-tier reader.
//
// Integration-only: this file compiles only under the `integration`
// build tag, so it never reaches a production binary. The production
// dispatcher does NOT consume the selector in Phase 7 (see
// PluginConsumerManager.keySelector docs); the selector is substrate
// for INV-CRYPTO-45 cross-tier instance equality.
func (m *PluginConsumerManager) KeySelectorForTest() codec.KeySelector { return m.keySelector }
