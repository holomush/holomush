// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package positive

import "github.com/oklog/ulid/v2"

var _ = ulid.Make() // want `use idgen.New\(\) for entity IDs or core.NewULID\(\) for event IDs; ulid.Make\(\) uses math/rand`
