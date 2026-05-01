// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package dek owns Data Encryption Key (DEK) lifecycle and material
// containment. The Material type is opaque by construction: it has no
// exported []byte accessor. The sole exported egress is AsCodecKey,
// which constructs a substrate codec.Key inline. The codec.Key.Bytes
// field is the residual leakage path; reads are gated by the ruleguard
// rule at gorules/codec_key_bytes_allowlist.go.
//
// Phase 2 ships Material plus the Manager skeleton (GetOrCreate +
// Resolve real; Add/Rotate/Rekey stubbed with tracking_bead). Phase 3
// wires Material into the codec encrypt/decrypt path.
package dek

import "github.com/holomush/holomush/internal/eventbus/codec"

// Material wraps unwrapped DEK bytes. Construction is via NewMaterial.
// The struct has no exported fields and no exported []byte accessor
// (enforced by api_test.go's static API surface check).
type Material struct {
	bytes []byte
}

// NewMaterial constructs an opaque Material wrapping a defensive copy
// of the input bytes. Callers MUST NOT retain a reference to the input
// slice and expect Material to mirror their mutations — the input is
// copied at construction.
func NewMaterial(bytes []byte) *Material {
	cp := make([]byte, len(bytes))
	copy(cp, bytes)
	return &Material{bytes: cp}
}

// AsCodecKey constructs a codec.Key with the given KeyID and the
// Material's underlying bytes. The returned codec.Key.Bytes shares
// backing memory with this Material (no further copy); reads of the
// returned key's Bytes field outside the codec/crypto package trees
// fail lint via gorules/codec_key_bytes_allowlist.go.
func (m *Material) AsCodecKey(id codec.KeyID) codec.Key {
	return codec.Key{ID: id, Bytes: m.bytes}
}
