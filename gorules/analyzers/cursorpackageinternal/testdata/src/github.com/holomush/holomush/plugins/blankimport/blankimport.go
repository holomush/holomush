// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Blank-import bypass: importing cursor purely for side effects (no
// symbol references) MUST still flag — the previous walker only saw
// pass.TypesInfo.Uses, so symbol-less imports could evade detection.
// CodeRabbit finding on PR #3457.
package blankimport

import _ "github.com/holomush/holomush/internal/eventbus/cursor" // want `internal/eventbus/cursor is host-internal — clients and plugins must not import it`
