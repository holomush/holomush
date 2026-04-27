// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package main

import (
	"go/ast"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// coreOnlyFiles are cmd/holomush files that legitimately import domain
// packages because they are part of the core process entry point, not
// the gateway. Every other .go file in cmd/holomush is treated as
// gateway-side and held to INV-GW-1.
var coreOnlyFiles = map[string]struct{}{
	"core.go":                         {},
	"core_test.go":                    {},
	"deps.go":                         {},
	"deps_test.go":                    {},
	"sub_grpc.go":                     {},
	"sub_grpc_adapters_test.go":       {},
	"sub_grpc_test.go":                {},
	"automigrate_test.go":             {},
	"automigrate_integration_test.go": {},
	"migrate.go":                      {},
	"migrate_test.go":                 {},
	"cmd_plugin_events.go":            {},
	"cmd_plugin_validate.go":          {},
}

var forbidden = []string{
	"github.com/holomush/holomush/internal/world",
	"github.com/holomush/holomush/internal/access",
	"github.com/holomush/holomush/internal/store",
	"github.com/holomush/holomush/internal/plugin",
	"github.com/holomush/holomush/internal/eventbus",
	"github.com/holomush/holomush/internal/auth/service",
	"github.com/holomush/holomush/internal/command",
}

// TestGatewayImportsAreOnlyProtocolTranslation is INV-GW-1. Gateway-side
// files MUST NOT import domain packages. Core-process files are excluded
// via coreOnlyFiles.
func TestGatewayImportsAreOnlyProtocolTranslation(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles |
			packages.NeedSyntax | packages.NeedImports |
			packages.NeedTypes,
	},
		"github.com/holomush/holomush/cmd/holomush",
		"github.com/holomush/holomush/internal/web/...",
		"github.com/holomush/holomush/internal/telnet/...",
	)
	require.NoError(t, err)
	require.Empty(t, packages.PrintErrors(pkgs))

	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			goFile := pkg.Fset.Position(file.Pos()).Filename
			checkFile(t, pkg.PkgPath, goFile, file)
		}
	}
}

func checkFile(t *testing.T, pkgPath, goFile string, file *ast.File) {
	t.Helper()
	base := filepath.Base(goFile)
	if pkgPath == "github.com/holomush/holomush/cmd/holomush" {
		if _, isCore := coreOnlyFiles[base]; isCore {
			return
		}
	}
	for _, imp := range file.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		for _, bad := range forbidden {
			if importPath == bad || strings.HasPrefix(importPath, bad+"/") {
				t.Errorf("%s/%s imports forbidden domain package %s",
					pkgPath, base, importPath)
			}
		}
	}
}
