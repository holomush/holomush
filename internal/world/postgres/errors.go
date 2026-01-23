// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import "errors"

// ErrNotFound is returned when an entity is not found.
var ErrNotFound = errors.New("not found")
