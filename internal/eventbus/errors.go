// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package eventbus defines the host-facing event bus interfaces and
// supporting types. The concrete JetStream-backed implementation lives
// in subpackages (subsystem, audit, codec, telemetry).
package eventbus

import "errors"

// Sentinel errors returned by EventBus implementations. Consumers MUST
// match via errors.Is, never by string content.
var (
	ErrInvalidSubject       = errors.New("eventbus: invalid subject")
	ErrInvalidType          = errors.New("eventbus: invalid event type")
	ErrEmitNotPermitted     = errors.New("eventbus: subject not in manifest emits")
	ErrPayloadTooLarge      = errors.New("eventbus: payload exceeds MaxPayloadSize")
	ErrCodecHeaderMissing   = errors.New("eventbus: required App-Codec header missing")
	ErrUnknownCodec         = errors.New("eventbus: codec name not in registry")
	ErrPublishExpired       = errors.New("eventbus: publish retry exceeded dedup window")
	ErrInvalidFilter        = errors.New("eventbus: filter does not match stream subject")
	ErrInvalidCursor        = errors.New("eventbus: invalid history cursor")
	ErrInvalidTimeRange     = errors.New("eventbus: NotBefore must be <= NotAfter")
	ErrSessionAuth          = errors.New("eventbus: session authentication failed")
	ErrUnauthorized         = errors.New("eventbus: caller not authorized for subject")
	ErrPluginTimeout        = errors.New("eventbus: plugin RPC timeout")
	ErrSubjectOwnershipConflict = errors.New("eventbus: subject ownership conflict at startup")
	ErrManifestInvalid      = errors.New("eventbus: manifest validation failed")
	ErrStoreDirLocked       = errors.New("eventbus: NATS StoreDir is already locked by another process")
	ErrDecryptionFailed     = errors.New("eventbus: decryption failed")
	ErrKeyUnavailable       = errors.New("eventbus: codec key unavailable")
)

// MaxPayloadSize matches the prior cap in internal/core/event.go to keep
// behavior consistent across the cutover.
const MaxPayloadSize = 64 * 1024
