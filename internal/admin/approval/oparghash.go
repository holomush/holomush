// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package approval

import (
	"crypto/sha256"

	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"
)

// ComputeOpArgsHash returns SHA-256 over the proto-deterministic-marshal
// of args. Both the primary's CLI (computing the hash for Open) and the
// server-side proceeding handler (recomputing for verification) use this
// helper so the hashes byte-equal across processes. INV-CRYPTO-75 + INV-CRYPTO-76.
//
// Cross-binary stability is load-bearing on the google.golang.org/protobuf
// version pin (INV-CRYPTO-85); the meta-test in proto_meta_test.go locks that pin.
func ComputeOpArgsHash(msg proto.Message) ([]byte, error) {
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg)
	if err != nil {
		return nil, oops.Code("OP_ARGS_HASH_MARSHAL_FAILED").Wrap(err)
	}
	sum := sha256.Sum256(raw)
	return sum[:], nil
}
