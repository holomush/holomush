// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// internal/eventbus/codec/allow is under the codec/ allowlist prefix —
// reads MUST NOT flag.
package allow

import "github.com/holomush/holomush/internal/eventbus/codec"

func ok(k codec.Key) []byte { return k.Bytes }
