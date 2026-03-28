// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package naming provides themed name generators for characters.
package naming

import (
	"crypto/rand"
	"math/big"
)

// Theme generates themed names.
type Theme interface {
	Name() string
	Generate() (firstName, secondName string)
}

// cryptoIntN returns a cryptographically secure random int in [0, n).
func cryptoIntN(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		// crypto/rand failure is a system-level problem; panic is appropriate.
		panic("crypto/rand failed: " + err.Error())
	}
	return int(v.Int64())
}
