// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holomushlint

import (
	"go/types"
	"strings"
)

// DEKMaterialPath is the canonical fully-qualified package path of the
// dek.Material type, exported as a constant so analyzer tests can stub
// the package at the same path under testdata/src/.
const DEKMaterialPath = "github.com/holomush/holomush/internal/eventbus/crypto/dek"

// IsDEKMaterial reports whether t is dek.Material or *dek.Material.
// Resolves type aliases via types.Unalias so user-defined aliases
// like `type MyMat = dek.Material` are detected.
func IsDEKMaterial(t types.Type) bool {
	if t == nil {
		return false
	}
	t = types.Unalias(t)
	if ptr, ok := t.(*types.Pointer); ok {
		t = types.Unalias(ptr.Elem())
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	if named.Obj().Pkg() == nil {
		return false
	}
	if named.Obj().Pkg().Path() != DEKMaterialPath {
		return false
	}
	return named.Obj().Name() == "Material"
}

// PackagePathMatchesAny reports whether pkgPath equals any allow exactly,
// or starts with any allow followed by "/" (i.e., is a sub-package).
//
// Example: PackagePathMatchesAny("github.com/holomush/holomush/internal/web/handlers",
//
//	[]string{"github.com/holomush/holomush/internal/web"})
//
// returns true.
func PackagePathMatchesAny(pkgPath string, allow []string) bool {
	for _, a := range allow {
		if pkgPath == a {
			return true
		}
		if strings.HasPrefix(pkgPath, a+"/") {
			return true
		}
	}
	return false
}
