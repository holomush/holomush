// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Negative cases: writes (composite literal + field assignment) MUST NOT flag.
package negative

import "github.com/holomush/holomush/internal/eventbus/codec"

// Composite-literal construction: 'Bytes' is the field NAME (an *ast.Ident,
// not a *ast.SelectorExpr), so the SelectorExpr walker correctly skips it.
func construct(id codec.KeyID, b []byte) codec.Key {
	return codec.Key{ID: id, Bytes: b}
}

// Field assignment: SelectorExpr appears as LHS of AssignStmt; the
// analyzer's isWriteContext check filters this out.
func assign(k *codec.Key, b []byte) {
	k.Bytes = b
}

// Indexed write: k.Bytes[0] = 1 is a write context. The SelectorExpr
// k.Bytes is wrapped in an *ast.IndexExpr that itself is the LHS of
// the AssignStmt; isWriteContext must walk through the wrapper to
// recognize this as a write. (Without that walk, the analyzer would
// see k.Bytes' immediate parent as IndexExpr — not AssignStmt — and
// incorrectly flag the read of the slice header.)
func assignIndex(k *codec.Key) {
	k.Bytes[0] = 1
}
