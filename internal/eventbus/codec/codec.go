// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package codec defines the host-owned codec interface for event payload
// encoding/decoding. The Identity codec is the pass-through default;
// future encryption codecs (e.g., aes-gcm-v1) plug in via the registry.
//
// Per the spec (§9), the codec is a narrow crypto primitive — it does
// NOT know about subjects or routing. Subject→key mapping lives in a
// separate KeySelector (also defined here, no production implementation
// yet).
package codec

import "context"

// Name is a closed enumeration of host-known codecs. Plugins MUST NOT
// register codecs.
type Name string

const (
	// NameIdentity is the built-in pass-through codec that leaves payloads unchanged.
	NameIdentity Name = "identity"
	// Future:
	// NameAESGCMv1        Name = "aes-gcm-v1"
	// NameXChaCha20v1     Name = "xchacha20poly1305-v1"
)

// Codec encodes and decodes event payload bytes. Implementations MUST be
// stateless and safe for concurrent use.
type Codec interface {
	Name() Name
	Encode(ctx context.Context, plaintext []byte, key Key) ([]byte, error)
	Decode(ctx context.Context, ciphertext []byte, key Key) ([]byte, error)
}

// Key is the opaque cryptographic material a codec uses to encrypt/decrypt.
// IdentityCodec ignores it and accepts NoKey.
type Key struct {
	ID    KeyID
	Bytes []byte
	// Codec-specific metadata may be carried inside Bytes; codecs are free
	// to interpret as they need.
}

// NoKey is the sentinel passed to keyless codecs (IdentityCodec).
var NoKey = Key{}

// KeyID is a stable identifier for a key version. Stored in the codec's
// internal envelope so Decode can pick the right key on rotation.
type KeyID uint64

// KeyLabel is a logical purpose name (e.g., "scene-content", "dm-content")
// used by KeyProvider.Active to look up the current key for that purpose.
type KeyLabel string

// KeyProvider supplies keys to codecs.
type KeyProvider interface {
	Active(ctx context.Context, label KeyLabel) (Key, error)
	ByID(ctx context.Context, id KeyID) (Key, error)
}

// KeySelector maps a publish-time subject to (codec name, key label) per
// the deployment's encryption policy. Lives upstream of Codec.
type KeySelector interface {
	SelectForEncrypt(ctx context.Context, subject string) (Name, KeyLabel, error)
	SelectForDecrypt(ctx context.Context, codec Name, keyID KeyID) (Key, error)
}

// IdentityCodec is the default no-op codec. It returns plaintext unchanged.
type IdentityCodec struct{}

// Name returns NameIdentity.
func (IdentityCodec) Name() Name { return NameIdentity }

// Encode returns plaintext unchanged.
func (IdentityCodec) Encode(_ context.Context, plaintext []byte, _ Key) ([]byte, error) {
	return plaintext, nil
}

// Decode returns ciphertext unchanged.
func (IdentityCodec) Decode(_ context.Context, ciphertext []byte, _ Key) ([]byte, error) {
	return ciphertext, nil
}
