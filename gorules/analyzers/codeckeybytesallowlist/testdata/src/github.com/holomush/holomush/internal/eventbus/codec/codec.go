// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package codec

type KeyID uint64

type Key struct {
	ID    KeyID
	Bytes []byte
}
