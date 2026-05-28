// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

// WithPluginCrypto wires the full plugin-crypto round-trip (emit fence → publish
// encrypt → audit projection → read-back) into the harness. REQUIRES
// WithInTreePlugins (the emitter, per-plugin consumer, and read-back decryptor
// all need the loaded Manager). Assumes crypto-CORRECT plugins: WithCryptoEnabled
// is global to the shared emitter, so a loaded plugin that emits
// sensitivity:always content without claiming Sensitive=true would reject (spec
// §6.2). Drive only crypto-correct plugins (e.g. core-scenes) under this option.
func WithPluginCrypto() StartOption {
	return func(c *startConfig) { c.withPluginCrypto = true }
}
