// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// internal/web/ is allowlisted (it decodes/encodes cursors at the
// ConnectRPC boundary).
package allow

import "github.com/holomush/holomush/internal/eventbus/cursor"

var _ = cursor.Encode(cursor.Cursor{})
