// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import "errors"

// ErrNotFound is returned when a requested entity does not exist.
var ErrNotFound = errors.New("not found")
