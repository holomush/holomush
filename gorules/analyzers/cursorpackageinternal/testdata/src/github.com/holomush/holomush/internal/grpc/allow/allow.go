// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// internal/grpc/ is allowlisted (it decodes/encodes cursors at the RPC
// boundary).
package allow

import "github.com/holomush/holomush/internal/eventbus/cursor"

var _, _ = cursor.Decode("")
