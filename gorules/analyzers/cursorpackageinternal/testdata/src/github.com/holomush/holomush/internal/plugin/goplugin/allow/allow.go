// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// internal/plugin/goplugin/ is allowlisted (it brokers cursor
// encoding/decoding for binary plugins via the host).
package allow

import "github.com/holomush/holomush/internal/eventbus/cursor"

var _ = cursor.Encode(cursor.Cursor{})
