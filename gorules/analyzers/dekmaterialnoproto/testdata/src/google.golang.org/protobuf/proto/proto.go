// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Stub of google.golang.org/protobuf/proto for analysistest.
//
// The real package's Marshal and (MarshalOptions).Marshal accept a
// proto.Message interface. dek.Material does NOT implement
// proto.Message today, so testdata calling proto.Marshal(*dek.Material)
// would fail to type-check against the real package — analysistest
// would never reach the analyzer's logic. This stub widens the
// parameter type to `any` so the testdata compiles and the analyzer's
// forward-defensive rule can be exercised. See bead holomush-46ya.8
// and the dekmaterialnoproto package comment for the rationale.
package proto

type MarshalOptions struct {
	Deterministic bool
}

func Marshal(m any) ([]byte, error) { return nil, nil }

func (MarshalOptions) Marshal(m any) ([]byte, error) { return nil, nil }
