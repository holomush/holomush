// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// This package's import path is under internal/eventbus/, which is on
// the cursorpackageinternal allowlist. References to cursor MUST NOT
// flag.
package allow

import "github.com/holomush/holomush/internal/eventbus/cursor"

var _ = cursor.Encode(cursor.Cursor{})
