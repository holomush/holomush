// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Stub testdata for the codeckeybytesallowlist analyzer. The package
// path 'github.com/holomush/holomush/plugins/positive' is OUTSIDE the
// codec/ and crypto/ allowlist prefixes, so reads here must flag.
// (Path is under github.com/holomush/holomush/ — a sibling of internal/
// — to satisfy Go's internal-package visibility rule, since this file
// imports internal/eventbus/codec.)
package positive

import "github.com/holomush/holomush/internal/eventbus/codec"

func readValue(k codec.Key) []byte {
	return k.Bytes // want `INV-27 \(residual defense\): codec.Key.Bytes reads are restricted`
}

func readPointerExplicit(pk *codec.Key) []byte {
	return (*pk).Bytes // want `INV-27 \(residual defense\): codec.Key.Bytes reads are restricted`
}

func readPointerAuto(pk *codec.Key) []byte {
	return pk.Bytes // want `INV-27 \(residual defense\): codec.Key.Bytes reads are restricted`
}

func readIndex(k codec.Key) byte {
	return k.Bytes[0] // want `INV-27 \(residual defense\): codec.Key.Bytes reads are restricted`
}

func readSlice(k codec.Key, n int) []byte {
	return k.Bytes[:n] // want `INV-27 \(residual defense\): codec.Key.Bytes reads are restricted`
}
