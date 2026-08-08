// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The world-caller census (AUTHZ-01, phase 02.1) — the structural half of
// INV-WORLD-8.
//
// WHAT IT PROVES. Every world.Service COMMAND that performs an authorization
// check declares the opaque world.Caller value in its second parameter slot,
// never a bare subject string. The compiler enforces this only for the code that
// exists today: a future world.Service command written with a `subjectID string`
// parameter compiles cleanly and silently reopens the defect phase 02.1 closed.
// This census is the only thing that catches it.
//
// WHAT IT DOES NOT PROVE. It says nothing about what the caller value carries
// into the ABAC request. That half — checkAccess forwarding the caller's
// attribute channel to types.NewAccessRequest rather than a hardcoded nil, and
// SystemCaller() satisfying the S1 double-gate unaided — is proven by
// internal/world/caller_test.go, which drives a REAL policy.Engine through
// paired permit/deny assertions. INV-WORLD-8's asserted_by names both files
// because the invariant has both clauses and neither file proves the other's.
//
// THE COMMAND UNIVERSE is structural, not a name list. A world.Service *command*
// is an exported *Service method whose FIRST FLATTENED parameter's type
// expression is context.Context. That predicate is what excludes
// SetMovementHook — an exported lifecycle setter taking a single MovementHook,
// with no context and no authorization check — without growing an exemption list
// that a future contributor would silently append to. It equally excludes the
// two unexported helpers whose plain-string second parameter phase 02.1
// deliberately preserves: checkAccess (whose own second slot is now the Caller)
// and buildMoveIntent, which keeps a `subjectID string` on purpose so that
// passing a Caller method value to it is a compile error.
//
// THE AUTHORIZING PREDICATE is likewise structural: the method's body references
// the receiver's checkAccess selector. It needs no list of the 23 command names,
// it follows automatically when a command gains or loses an authorization check,
// and it is exactly what distinguishes the single documented exemption below.
//
// The census parses every non-test .go file in internal/world/ rather than
// service.go alone, so relocating a command into a sibling file cannot carry it
// out of the universe.

// callerParamIndex is the FLATTENED parameter position the caller value occupies
// on every authorizing world.Service command: index 0 is the context.Context,
// index 1 is the world.Caller. "Flattened" is load-bearing — a grouped
// declaration such as `func (s *Service) F(ctx context.Context, a, b string)`
// packs two parameters into one *ast.Field, so Params.List[1] is not
// parameter 1. Every lookup here goes through flattenedParamTypes.
const callerParamIndex = 1

// worldCallerExemptCommands are exported world.Service COMMANDS that deliberately
// perform no authorization check and therefore take no caller value. This is the
// outOfServiceOnlyKinds idiom (see world_envelope_census_test.go): one commented
// member per justifying decision, never an inline name comparison, so removing a
// member is a visible edit rather than a silent loosening.
//
// Membership is ASSERTED, not merely tolerated — see
// TestWorldServiceCallerParamCensusExemptsOnlyTheDocumentedCommand. Adding a
// checkAccess call to the member below, or landing a second unauthorized
// command, both fail loudly (D-68).
func worldCallerExemptCommands() map[string]struct{} {
	return map[string]struct{}{
		// D-05 (per internal/world/service.go:862, and that method's own doc
		// comment at :858-863): UpdateCharacterPreferences is the
		// settings-subsystem persistence primitive. The authorization decision is
		// made at the settings command layer, and the raw store path it replaced
		// had no ABAC at all, so gating it here would be an out-of-scope behavior
		// change. It takes no subject of any kind — its second parameter is the
		// character ULID — so it needs no exemption from the bare-string
		// assertion below, only from the caller-parameter one.
		"UpdateCharacterPreferences": {},
	}
}

// worldServiceCommand is one exported world.Service command as the census sees
// it: its name, the file that declares it, its flattened parameter type
// expressions, and whether its body performs an authorization check.
type worldServiceCommand struct {
	name        string
	file        string
	paramTypes  []ast.Expr
	authorizing bool
}

// worldServiceCommands parses every non-test .go file in internal/world/ and
// returns the exported *Service COMMANDS: methods whose first flattened
// parameter's type expression is context.Context. Non-command exported methods
// (lifecycle setters taking no context) and every unexported method are excluded
// by construction rather than by an exemption entry.
//
// This is a sibling collector to serviceMutatingMethods in
// world_envelope_census_test.go: it reuses that file's leaf helpers
// (serviceReceiverName, bodyReferencesSelector) and meta_helpers_test.go's
// findRepoRoot, but does not modify serviceMutatingMethods, whose allMethods
// return is consumed by TestWorldEnvelopeCensusHasNoSceneParticipantSurface.
func worldServiceCommands(t *testing.T) []worldServiceCommand {
	t.Helper()
	root := findRepoRoot(t)
	dir := filepath.Join(root, "internal", "world")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "read internal/world")

	fset := token.NewFileSet()
	var commands []worldServiceCommand
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		require.NoErrorf(t, parseErr, "parse internal/world/%s", name)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 || !fn.Name.IsExported() {
				continue
			}
			recvName, ok := serviceReceiverName(fn.Recv.List[0])
			if !ok {
				continue // not a *Service (or Service) method
			}
			params := flattenedParamTypes(fn.Type.Params)
			if len(params) == 0 || !isContextContext(params[0]) {
				continue // not a command — no context.Context in the first slot
			}
			commands = append(commands, worldServiceCommand{
				name:        fn.Name.Name,
				file:        name,
				paramTypes:  params,
				authorizing: bodyReferencesSelector(fn.Body, recvName, "checkAccess"),
			})
		}
	}
	return commands
}

// flattenedParamTypes expands a parameter list into one entry per PARAMETER,
// carrying the shared type expression on each entry of a grouped declaration and
// counting an unnamed parameter as the single slot it occupies. Indexing
// Params.List directly would conflate "list index 1" with "parameter 1" the
// moment a declaration groups two names under one type.
func flattenedParamTypes(fields *ast.FieldList) []ast.Expr {
	if fields == nil {
		return nil
	}
	var out []ast.Expr
	for _, field := range fields.List {
		slots := len(field.Names)
		if slots == 0 {
			slots = 1 // an unnamed parameter still occupies one position
		}
		for i := 0; i < slots; i++ {
			out = append(out, field.Type)
		}
	}
	return out
}

// isContextContext reports whether a parameter's type expression is the
// context.Context selector.
func isContextContext(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "Context" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "context"
}

// isIdentNamed reports whether a type expression is the bare identifier name.
// internal/world declares world.Caller in its own package, so an authorizing
// command's caller parameter reads as the unqualified identifier Caller.
func isIdentNamed(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

// Verifies: INV-WORLD-8
func TestWorldServiceCallerParamIsTypedOnEveryAuthorizingCommand(t *testing.T) {
	commands := worldServiceCommands(t)
	require.NotEmpty(t, commands,
		"the world.Service command universe is empty — the context.Context-first predicate matched nothing, so this census would pass vacuously")

	authorizing := 0
	for _, cmd := range commands {
		if !cmd.authorizing {
			continue
		}
		authorizing++

		require.Greaterf(t, len(cmd.paramTypes), callerParamIndex,
			"world.Service.%s (internal/world/%s) performs an authorization check but declares only %d parameter(s), so it cannot carry a caller value — AUTHZ-01 / INV-WORLD-8 require the typed world.Caller in the second parameter slot",
			cmd.name, cmd.file, len(cmd.paramTypes))

		got := cmd.paramTypes[callerParamIndex]
		assert.Truef(t, isIdentNamed(got, "Caller"),
			"world.Service.%s (internal/world/%s) performs an authorization check but its second parameter type is %s, not world.Caller — AUTHZ-01 / INV-WORLD-8 forbid a bare subject-string parameter on an authorizing command; wrap the subject at the call site with world.HumanCaller / world.SystemCaller instead",
			cmd.name, cmd.file, types.ExprString(got))
	}

	require.NotZerof(t, authorizing,
		"no world.Service command references the receiver's checkAccess selector — the authorizing predicate has stopped matching and this census proves nothing")
}

// TestWorldServiceCallerParamCensusExemptsOnlyTheDocumentedCommand pins the
// exemption in BOTH directions (D-68): a command that quietly loses its
// authorization check fails here, and so does an exemption entry for a command
// that has since gained one.
func TestWorldServiceCallerParamCensusExemptsOnlyTheDocumentedCommand(t *testing.T) {
	commands := worldServiceCommands(t)
	require.NotEmpty(t, commands, "the world.Service command universe is non-empty")

	exempt := worldCallerExemptCommands()

	unauthorized := map[string]struct{}{}
	for _, cmd := range commands {
		if !cmd.authorizing {
			unauthorized[cmd.name] = struct{}{}
		}
	}

	for name := range unauthorized {
		_, documented := exempt[name]
		assert.Truef(t, documented,
			"world.Service.%s is a command (its first parameter is a context.Context) but its body calls no s.checkAccess and it is not a documented exemption — either it lost its authorization check or an unauthorized command landed undocumented (AUTHZ-01 / INV-WORLD-8)",
			name)
	}
	for name := range exempt {
		_, stillUnauthorized := unauthorized[name]
		assert.Truef(t, stillUnauthorized,
			"the documented exemption world.Service.%s is no longer a non-authorizing command — if it gained an authorization check, drop it from worldCallerExemptCommands AND from the INV-WORLD-8 summary, which names it as the single exception (D-68)",
			name)
	}
}

// TestWorldServiceCallerParamSlotHoldsNoBareStringOnAnyCommand is the
// no-escape-hatch half: even a command outside the authorizing set may not
// declare a bare string where the caller value belongs. Scoped to the command
// universe on purpose — stated over every *Service method it would flag the two
// unexported helpers whose plain-string parameter phase 02.1 preserves
// deliberately.
func TestWorldServiceCallerParamSlotHoldsNoBareStringOnAnyCommand(t *testing.T) {
	commands := worldServiceCommands(t)
	require.NotEmpty(t, commands, "the world.Service command universe is non-empty")

	for _, cmd := range commands {
		if len(cmd.paramTypes) <= callerParamIndex {
			continue // too few parameters to hold a caller slot at all
		}
		assert.Falsef(t, isIdentNamed(cmd.paramTypes[callerParamIndex], "string"),
			"world.Service.%s (internal/world/%s) declares a bare string in the second parameter slot — AUTHZ-01 / INV-WORLD-8 reserve that slot for the opaque world.Caller value, so a raw subject string can never reach the ABAC request unwrapped",
			cmd.name, cmd.file)
	}
}
