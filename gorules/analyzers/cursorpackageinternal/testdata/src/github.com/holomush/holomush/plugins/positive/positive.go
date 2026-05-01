// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// plugins/ is OUTSIDE the cursorpackageinternal allowlist. References
// to cursor MUST flag. The package lives under github.com/holomush/holomush/
// so that go/types' internal-package visibility rule lets us import
// internal/eventbus/cursor at all (positive testdata cannot live under
// example.com/ because the typechecker would reject the import before
// the analyzer ever runs).
package positive

import "github.com/holomush/holomush/internal/eventbus/cursor"

var _ = cursor.Encode(cursor.Cursor{}) // want `internal/eventbus/cursor is host-internal — clients and plugins must not import it` `internal/eventbus/cursor is host-internal — clients and plugins must not import it`

var _ = cursor.CurrentVersion // want `internal/eventbus/cursor is host-internal — clients and plugins must not import it`

var _ cursor.OwnerKind = cursor.OwnerHost // want `internal/eventbus/cursor is host-internal — clients and plugins must not import it` `internal/eventbus/cursor is host-internal — clients and plugins must not import it`

var _ = cursor.Owner{Kind: cursor.OwnerPlugin} // want `internal/eventbus/cursor is host-internal — clients and plugins must not import it` `internal/eventbus/cursor is host-internal — clients and plugins must not import it` `internal/eventbus/cursor is host-internal — clients and plugins must not import it`
