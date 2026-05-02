// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Stub for analysistest. Mirrors the import path of github.com/oklog/ulid/v2
// and exports just the surface the rule consults.
package ulid

type ULID [16]byte

func Make() ULID { return ULID{} }
