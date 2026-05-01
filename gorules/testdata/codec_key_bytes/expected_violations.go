// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// Documentation file (not compiled). See sibling expected_violations.go
// in dek_no_serialize/ for the file purpose. The rule lives in
// gorules/rules.go (CodecKeyBytesAllowlist), concatenated there per the
// Phase 2 plan's fallback guidance.

//go:build ignore_fixture
// +build ignore_fixture

package documentation

import "github.com/holomush/holomush/internal/eventbus/codec"

func leakKeyBytes(k codec.Key) []byte {
	return k.Bytes // EXPECT: INV-27 (residual defense): codec.Key.Bytes reads are restricted
}
