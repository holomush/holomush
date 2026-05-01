// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package negative

import (
	"github.com/oklog/ulid/v2"
)

// A function literally named Make() defined locally MUST NOT match — the
// rule targets the upstream ulid package only.
type Local struct{}

func (Local) Make() ulid.ULID { return ulid.ULID{} }

var l Local
var _ = l.Make() // OK — method on local type
