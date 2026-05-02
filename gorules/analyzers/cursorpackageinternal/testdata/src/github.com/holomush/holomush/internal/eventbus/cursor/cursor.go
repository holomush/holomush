// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cursor

type OwnerKind int

const (
	OwnerUnspecified OwnerKind = iota
	OwnerHost
	OwnerPlugin
)

const CurrentVersion = 1

type Owner struct {
	Kind OwnerKind
}

type Cursor struct {
	Owner Owner
}

type HostCursor struct {
	Cursor Cursor
}

func CurrentEpoch() int { return 0 }

func Encode(c Cursor) string { return "" }

func Decode(s string) (Cursor, error) { return Cursor{}, nil }
