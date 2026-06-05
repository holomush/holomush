// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package approval implements the admin approval workflow, including
// op_args_hash computation via deterministic protobuf marshalling.
//
// The google.golang.org/protobuf dependency is pinned in go.mod per INV-CRYPTO-85;
// proto.MarshalOptions{Deterministic: true} stability is documented within a
// binary version but not guaranteed across releases.
package approval
