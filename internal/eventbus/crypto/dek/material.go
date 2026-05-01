// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package dek owns Data Encryption Key (DEK) lifecycle and material
// containment. The Material type is opaque by construction: it has no
// exported []byte accessor. The sole exported egress is AsCodecKey,
// which constructs a substrate codec.Key inline. The codec.Key.Bytes
// field is the residual leakage path; reads are gated by the
// codeckeybytesallowlist analyzer in gorules/analyzers/.
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

// AsCodecKey constructs a codec.Key with the given KeyID and a fresh
// copy of the Material's underlying bytes. Each call returns an
// independent slice so that downstream mutations of codec.Key.Bytes
// (e.g., a misbehaving codec or a cached caller) cannot corrupt the
// Material or sibling keys minted from the same Material. Reads of
// the returned key's Bytes field outside the codec/crypto package
// trees fail lint via the codeckeybytesallowlist analyzer in
// gorules/analyzers/.
func (m *Material) AsCodecKey(id codec.KeyID) codec.Key {
	out := make([]byte, len(m.bytes))
	copy(out, m.bytes)
	return codec.Key{ID: id, Bytes: out}
}
