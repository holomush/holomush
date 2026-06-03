// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package history

import "github.com/holomush/holomush/internal/eventbus/codec"

// KeySelectorForTest exposes the reader's KeySelector to integration
// tests that assert INV-CRYPTO-45 pointer-identity with PluginConsumerManager.
//
// Integration-only: gated by the `integration` build tag so the
// accessor never reaches a production binary.
func (r *Reader) KeySelectorForTest() codec.KeySelector { return r.selector }
