// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	// jsoncanonicalizer provides RFC 8785 JSON Canonicalization Scheme (JCS).
	// Pinned in go.mod at pseudo-version 20241213102144-19d51d7fe467 per
	// INV-D13: switching implementations is a chain-breaking master-spec amendment.
	_ "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)
