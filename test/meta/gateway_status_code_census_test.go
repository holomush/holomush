// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Gateway status-code census
//
// grpcToConnectCode (internal/web/status_interceptor.go) is a per-code ALLOWLIST
// with a `default: connect.CodeInternal` arm. That default is a silent
// degradation: a code the facades return but the table never names reaches the
// browser as HTTP 500, indistinguishable from a server fault, and no test
// anywhere fails.
//
// That is not hypothetical. codes.Aborted — the optimistic-concurrency conflict
// the character mutation surface returns, whose entire contract is that the
// CLIENT re-reads and retries — shipped unmapped, asserted at the gRPC boundary
// by two suites and by nothing at the gateway boundary it was returned for.
//
// This census closes the class rather than the instance: every gRPC code the
// internal/grpc facades hand to status.Error/Errorf/New/Newf must either have an
// explicit case in the table, or be a member of the small set for which the
// default arm is the RIGHT answer.
// ---------------------------------------------------------------------------

// grpcCodesImportPath is the package whose selector expressions this census
// counts. internal/grpc/server.go imports go.opentelemetry.io/otel/codes under
// the same local name `codes`, so a census that matched on the identifier alone
// would collect otel's codes.Error as a gRPC status code. Every file's import
// set is resolved before its body is walked.
const grpcCodesImportPath = "google.golang.org/grpc/codes"

// defaultArmIsCorrectFor names the gRPC codes for which grpcToConnectCode's
// `default: connect.CodeInternal` is the intended answer, so their absence from
// the switch is a decision rather than an oversight. Both denote "server fault,
// unclassified" — exactly what CodeInternal says — and the interceptor's own
// doc comment states this default is deliberate.
var defaultArmIsCorrectFor = map[string]struct{}{
	"Internal": {},
	"Unknown":  {},
}

// statusConstructors are the calls whose FIRST argument is a code the facade
// RETURNS to a caller. Restricting the census to these keeps it honest: a
// `status.Code(err) == codes.NotFound` comparison consumes a code rather than
// producing one, and demanding a mapping for it would be a different claim.
var statusConstructors = map[string]struct{}{
	"Error":  {},
	"Errorf": {},
	"New":    {},
	"Newf":   {},
}

// localNameForImport returns the local identifier a file binds the given import
// path to, and whether the file imports it at all. It handles an explicit alias
// and the implicit last-path-segment form alike.
func localNameForImport(file *ast.File, path string) (string, bool) {
	for _, spec := range file.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil || value != path {
			continue
		}
		if spec.Name != nil {
			return spec.Name.Name, true
		}
		segments := strings.Split(value, "/")
		return segments[len(segments)-1], true
	}
	return "", false
}

// collectReturnedGRPCCodes walks every non-test .go file in dir and returns the
// set of gRPC code names passed as the first argument to a status constructor.
func collectReturnedGRPCCodes(t *testing.T, dir string) map[string]struct{} {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "read %s", dir)

	codesFound := map[string]struct{}{}
	fset := token.NewFileSet()

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		require.NoError(t, parseErr, "parse %s", name)

		codesLocal, importsCodes := localNameForImport(file, grpcCodesImportPath)
		if !importsCodes {
			continue
		}
		statusLocal, importsStatus := localNameForImport(file, "google.golang.org/grpc/status")
		if !importsStatus {
			continue
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			fn, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := fn.X.(*ast.Ident)
			if !ok || pkg.Name != statusLocal {
				return true
			}
			if _, ok := statusConstructors[fn.Sel.Name]; !ok {
				return true
			}
			arg, ok := call.Args[0].(*ast.SelectorExpr)
			if !ok {
				return true
			}
			argPkg, ok := arg.X.(*ast.Ident)
			if !ok || argPkg.Name != codesLocal {
				return true
			}
			codesFound[arg.Sel.Name] = struct{}{}
			return true
		})
	}

	return codesFound
}

// collectMappedGRPCCodes returns the set of gRPC code names grpcToConnectCode
// gives an explicit `case` arm.
func collectMappedGRPCCodes(t *testing.T, path string) map[string]struct{} {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	require.NoError(t, err, "parse %s", path)

	codesLocal, importsCodes := localNameForImport(file, grpcCodesImportPath)
	require.True(t, importsCodes, "%s must import %s", path, grpcCodesImportPath)

	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		candidate, ok := decl.(*ast.FuncDecl)
		if ok && candidate.Name.Name == "grpcToConnectCode" {
			fn = candidate
			break
		}
	}
	require.NotNil(t, fn, "%s must declare grpcToConnectCode", path)

	mapped := map[string]struct{}{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		clause, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, expr := range clause.List {
			sel, ok := expr.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != codesLocal {
				continue
			}
			mapped[sel.Sel.Name] = struct{}{}
		}
		return true
	})
	return mapped
}

// TestEveryGRPCCodeTheFacadesReturnHasAGatewayMapping is the class-level guard
// behind the codes.Aborted defect: a facade code with no case arm reaches the
// browser as CodeInternal, and the failure is invisible to every test that stops
// at the gRPC boundary.
func TestEveryGRPCCodeTheFacadesReturnHasAGatewayMapping(t *testing.T) {
	root := findRepoRoot(t)

	returned := collectReturnedGRPCCodes(t, filepath.Join(root, "internal", "grpc"))
	require.NotEmpty(t, returned,
		"the census derived NO codes from internal/grpc — the collector stopped matching, so a green result here would be vacuous")

	mapped := collectMappedGRPCCodes(t, filepath.Join(root, "internal", "web", "status_interceptor.go"))

	unmapped := []string{}
	for code := range returned {
		if _, ok := mapped[code]; ok {
			continue
		}
		if _, ok := defaultArmIsCorrectFor[code]; ok {
			continue
		}
		unmapped = append(unmapped, code)
	}

	assert.Empty(t, unmapped,
		"internal/grpc returns codes.%v, which grpcToConnectCode has no case for; they reach the browser as CodeInternal / HTTP 500, indistinguishable from a server fault. Add a case, or — if CodeInternal genuinely IS the answer — name the code in defaultArmIsCorrectFor with the reason",
		unmapped)

	// Non-vacuity: the two codes the census exists for must actually be in the
	// derived set. A collector that silently stopped matching status.Error would
	// otherwise report an empty unmapped list and pass.
	assert.Contains(t, returned, "Aborted",
		"the character mutation surface returns codes.Aborted; a census that cannot see it cannot guard it")
	assert.Contains(t, returned, "PermissionDenied",
		"the facades return codes.PermissionDenied; a census that cannot see it cannot guard it")
}
