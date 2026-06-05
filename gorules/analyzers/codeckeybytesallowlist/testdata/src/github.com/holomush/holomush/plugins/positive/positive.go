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
	return k.Bytes // want `INV-CRYPTO-16 \(residual defense\): codec.Key.Bytes reads are restricted`
}

func readPointerExplicit(pk *codec.Key) []byte {
	return (*pk).Bytes // want `INV-CRYPTO-16 \(residual defense\): codec.Key.Bytes reads are restricted`
}

func readPointerAuto(pk *codec.Key) []byte {
	return pk.Bytes // want `INV-CRYPTO-16 \(residual defense\): codec.Key.Bytes reads are restricted`
}

func readIndex(k codec.Key) byte {
	return k.Bytes[0] // want `INV-CRYPTO-16 \(residual defense\): codec.Key.Bytes reads are restricted`
}

func readSlice(k codec.Key, n int) []byte {
	return k.Bytes[:n] // want `INV-CRYPTO-16 \(residual defense\): codec.Key.Bytes reads are restricted`
}

// Alias-receiver bypass: `type K = codec.Key` makes the selection's
// Recv() return *types.Alias (Go 1.23+), and a direct *types.Named
// assertion fails. Without types.Unalias, the analyzer misses reads
// through the alias type. CodeRabbit finding on PR #3457.
type K = codec.Key

func readViaAlias(k K) []byte {
	return k.Bytes // want `INV-CRYPTO-16 \(residual defense\): codec.Key.Bytes reads are restricted`
}

func readViaAliasPointer(pk *K) []byte {
	return pk.Bytes // want `INV-CRYPTO-16 \(residual defense\): codec.Key.Bytes reads are restricted`
}
