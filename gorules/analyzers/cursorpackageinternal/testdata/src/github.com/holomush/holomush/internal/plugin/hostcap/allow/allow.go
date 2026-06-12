// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// internal/plugin/hostcap/ is allowlisted (it holds the runtime-neutral
// host.v1 capability servers relocated from goplugin; QueryStreamHistory
// brokers cursor encoding/decoding at the plugin host-callback boundary).
package allow

import "github.com/holomush/holomush/internal/eventbus/cursor"

var _ = cursor.Encode(cursor.Cursor{})
