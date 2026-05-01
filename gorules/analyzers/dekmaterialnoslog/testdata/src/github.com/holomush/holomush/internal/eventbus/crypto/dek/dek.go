// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Stub of internal/eventbus/crypto/dek for analysistest. Mirrors only
// the API surface the dekmaterialno* analyzers consult (Material type
// + NewMaterial constructor; the AsCodecKey method is irrelevant to
// the rules and intentionally elided to keep the stub minimal).
package dek

type Material struct {
	bytes []byte
}

func NewMaterial(b []byte) *Material { return &Material{bytes: append([]byte(nil), b...)} }
