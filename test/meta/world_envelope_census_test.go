// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/outbox"
)

// The world-change census (D-01). It proves REGISTRY CONSISTENCY: a bijection
// between the EXPLICIT closed in-Service write-command descriptor set
// (world.WriteCommands — each descriptor names its taxonomy kind) and the declared
// taxonomy kinds (internal/world/outbox), with NO allow-list of pending/exempt
// commands. Membership is derived from that explicit construct, never from
// name-prefix inference or the incomplete world.Mutator subset.
//
// SCOPE / honest completeness (round-3 census MEDIUM): the bijection proves
// registry consistency, NOT discovery of a direct repo write. COMPLETENESS of
// production world writes rests on the PAIRED fences that make an unregistered
// write impossible or fence-caught:
//   - the reader-view compile fence (05-11 Task 1: world.Service holds only
//     reader views, so a direct s.xRepo.Update(...) does not compile);
//   - the AST raw-world-SQL fence (05-09: no world-table SQL outside
//     internal/world/postgres);
//   - the internal/world/postgres composition allowlist (05-07 Task 4: only
//     composition/test packages may hold a concrete writer);
//   - the removal of Create from the auth-side character-repo interfaces (05-15).
//
// The OUT-OF-Service producer assertions (the character-genesis service 05-15 AND
// the character-reaping service 05-16 each emit a DECLARED kind) live in
// TestWorldEnvelopeCensusOutOfServiceProducers below — added by 05-12 (round-9
// R6-5), which depends_on BOTH 05-15 and 05-16, so both internal/auth producer
// files are guaranteed present. They were deliberately NOT added by 05-11 because
// 05-11 and 05-16 are both wave 10, so a 05-11 census reading the reaping file
// could have run before 05-16 created it. The rest of this census is the IN-Service
// bijection over producers-of-record plus a go/ast Service-mutating-method cross-check.

// outOfServiceOnlyKinds are declared taxonomy kinds whose PRODUCER is an
// out-of-Service application service, not a world.Service command — so they have
// no in-Service producer and are excluded from this census's in-Service bijection.
// character_genesis is produced by the 05-15 character-genesis service (asserted in
// 05-12). The character delete/tombstone kind is NOT here: it HAS an in-Service
// producer (DeleteCharacter); its additional out-of-Service producer (the reaping
// service) is a sanctioned multi-producer asserted in 05-12.
func outOfServiceOnlyKinds() map[string]struct{} {
	return map[string]struct{}{
		outbox.KindCharacterGenesis: {},
	}
}

// TestWorldEnvelopeCensusBijection asserts the bijection between the explicit
// in-Service write-command descriptor set and the declared taxonomy kinds.
func TestWorldEnvelopeCensusBijection(t *testing.T) {
	commands := world.WriteCommands()
	require.NotEmpty(t, commands, "the write-command descriptor set is non-empty")

	// 1. Every registered command has exactly one DECLARED kind, and no two
	//    commands share a kind (bijection, in-Service direction).
	kindToCommand := make(map[string]string, len(commands))
	commandSeen := make(map[string]struct{}, len(commands))
	for _, c := range commands {
		require.NotEmptyf(t, c.Command, "descriptor has a command name (kind=%q)", c.Kind)
		require.NotEmptyf(t, c.Kind, "command %q declares a kind", c.Command)

		_, dup := commandSeen[c.Command]
		require.Falsef(t, dup, "command %q is registered once (no duplicate descriptor)", c.Command)
		commandSeen[c.Command] = struct{}{}

		require.Truef(t, outbox.IsDeclared(c.Kind),
			"command %q maps to kind %q, which MUST be declared in the taxonomy (no undeclared kind on the feed)",
			c.Command, c.Kind)

		if other, clash := kindToCommand[c.Kind]; clash {
			t.Fatalf("kind %q has two in-Service producers (%q and %q); the in-Service bijection is one-producer-of-record",
				c.Kind, other, c.Command)
		}
		kindToCommand[c.Kind] = c.Command
	}

	// 2. Coverage: every DECLARED world-change kind has exactly one registered
	//    in-Service producer, EXCEPT the out-of-Service-only kinds (character
	//    genesis, produced by the 05-15 service). No allow-list of pending
	//    commands — a declared kind with no in-Service producer that is not
	//    out-of-Service-only is a coverage gap.
	outOfService := outOfServiceOnlyKinds()
	for _, kind := range outbox.Kinds() {
		if _, skip := outOfService[kind]; skip {
			_, hasProducer := kindToCommand[kind]
			assert.Falsef(t, hasProducer,
				"kind %q is produced out-of-Service (05-15/05-16), it MUST NOT have an in-Service producer descriptor", kind)
			continue
		}
		_, hasProducer := kindToCommand[kind]
		assert.Truef(t, hasProducer,
			"declared kind %q has no registered in-Service write command — either wire a command or classify it out-of-Service (no silent gap, D-01)",
			kind)
	}
}

// serviceMutatingMethods parses internal/world/service.go and returns, via go/ast,
// the set of *Service methods whose body routes through the write executor
// (references the `s.mutator` selector) — the structural "mutating method" signal —
// plus the full set of *Service method names.
func serviceMutatingMethods(t *testing.T) (mutating map[string]struct{}, allMethods map[string]struct{}) {
	t.Helper()
	root := findRepoRoot(t)
	src := filepath.Join(root, "internal", "world", "service.go")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, src, nil, 0)
	require.NoError(t, err, "parse internal/world/service.go")

	mutating = map[string]struct{}{}
	allMethods = map[string]struct{}{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		recvName, ok := serviceReceiverName(fn.Recv.List[0])
		if !ok {
			continue // not a *Service (or Service) method
		}
		allMethods[fn.Name.Name] = struct{}{}
		if bodyReferencesSelector(fn.Body, recvName, "mutator") {
			mutating[fn.Name.Name] = struct{}{}
		}
	}
	return mutating, allMethods
}

// serviceReceiverName returns the receiver variable name for a method whose
// receiver type is *Service (or Service), and whether it is a Service method.
func serviceReceiverName(recv *ast.Field) (string, bool) {
	expr := recv.Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, ok := expr.(*ast.Ident)
	if !ok || ident.Name != "Service" {
		return "", false
	}
	if len(recv.Names) == 0 {
		return "", false
	}
	return recv.Names[0].Name, true
}

// bodyReferencesSelector reports whether body contains a selector expression
// `<recv>.<field>` (e.g. s.mutator).
func bodyReferencesSelector(body *ast.BlockStmt, recv, field string) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if ok && x.Name == recv && sel.Sel.Name == field {
			found = true
			return false
		}
		return true
	})
	return found
}

// TestWorldEnvelopeCensusMatchesServiceMutatingMethods is the secondary go/ast
// cross-check: the structural set of world.Service methods that route through the
// executor equals the registered in-Service descriptor command set. A mutating
// method added to Service WITHOUT a descriptor (or a descriptor for a method that
// no longer routes through the executor) is caught here.
func TestWorldEnvelopeCensusMatchesServiceMutatingMethods(t *testing.T) {
	mutating, _ := serviceMutatingMethods(t)

	registered := map[string]struct{}{}
	for _, c := range world.WriteCommands() {
		registered[c.Command] = struct{}{}
	}

	for name := range mutating {
		_, ok := registered[name]
		assert.Truef(t, ok,
			"world.Service.%s routes through the write executor but has no census descriptor — register it (D-01: no un-migrated command)",
			name)
	}
	for name := range registered {
		_, ok := mutating[name]
		assert.Truef(t, ok,
			"census descriptor %q is not a world.Service method routing through the executor (stale descriptor)",
			name)
	}
}

// TestWorldEnvelopeCensusHasNoSceneParticipantSurface asserts the vestigial
// scene-participant write surface is absent (round-5 D-07 removed it): no
// descriptor references it and world.Service exposes no such mutating method.
func TestWorldEnvelopeCensusHasNoSceneParticipantSurface(t *testing.T) {
	removed := []string{"Add" + "SceneParticipant", "Remove" + "SceneParticipant"}

	for _, c := range world.WriteCommands() {
		for _, r := range removed {
			assert.NotEqualf(t, r, c.Command, "no scene-participant write command may appear in the census (D-07)")
		}
	}

	_, allMethods := serviceMutatingMethods(t)
	for _, r := range removed {
		_, present := allMethods[r]
		assert.Falsef(t, present, "world.Service.%s was removed in 05-14 (D-07) and MUST NOT exist", r)
	}
}

// authProducerFileKinds parses an out-of-Service producer file (internal/auth/*)
// and returns the taxonomy kinds it stamps on the envelopes it emits: for every
// wmodel.IntentParams composite literal, the value of its Kind field, resolved
// through the file's local string consts. internal/auth MUST NOT import
// internal/world/outbox, so a producer names its kind via a LOCAL literal const —
// this go/ast pass resolves that literal so the census can prove it is a declared
// taxonomy kind without importing the auth package.
func authProducerFileKinds(t *testing.T, rel string) []string {
	t.Helper()
	root := findRepoRoot(t)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, 0)
	require.NoErrorf(t, err, "parse %s", rel)

	// Collect local string consts (name -> value) so a Kind: field that is a const
	// identifier can be resolved to its string.
	consts := map[string]string{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if v, uerr := strconv.Unquote(lit.Value); uerr == nil {
					consts[name.Name] = v
				}
			}
		}
	}

	var kinds []string
	ast.Inspect(file, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := cl.Type.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || x.Name != "wmodel" || sel.Sel.Name != "IntentParams" {
			return true
		}
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "Kind" {
				continue
			}
			switch v := kv.Value.(type) {
			case *ast.BasicLit:
				if v.Kind == token.STRING {
					if s, uerr := strconv.Unquote(v.Value); uerr == nil {
						kinds = append(kinds, s)
					}
				}
			case *ast.Ident:
				if s, ok := consts[v.Name]; ok {
					kinds = append(kinds, s)
				}
			}
		}
		return true
	})
	return kinds
}

// TestWorldEnvelopeCensusOutOfServiceProducers asserts the TWO sanctioned
// out-of-world application services each emit a DECLARED taxonomy kind: the
// character-genesis service (05-15, character_genesis) and the character-reaping
// service (05-16/D-06, character_deleted — the SAME kind world.Service.DeleteCharacter
// emits, so a kind MAY have >1 sanctioned producer; this asserts each out-of-world
// writer maps to a DECLARED kind, NOT a unique one — it is NOT a bijection violation
// and does NOT weaken the in-Service bijection above). No scene-participant producer
// exists (D-07).
//
// This assertion lives in 05-12 (not 05-11) to avoid the wave-10 file-creation
// race: 05-11 and 05-16 are both wave 10, so a 05-11 census reading
// internal/auth/character_reaping.go could have run before 05-16 created it. This
// plan depends_on BOTH 05-15 and 05-16, so both producer files are present here.
func TestWorldEnvelopeCensusOutOfServiceProducers(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		wantKind string
	}{
		{"character-genesis service (05-15)", "internal/auth/character_genesis.go", outbox.KindCharacterGenesis},
		{"character-reaping service (05-16, D-06)", "internal/auth/character_reaping.go", outbox.KindCharacterDeleted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kinds := authProducerFileKinds(t, tc.file)
			require.NotEmptyf(t, kinds, "%s emits at least one envelope kind", tc.file)
			for _, k := range kinds {
				assert.Truef(t, outbox.IsDeclared(k),
					"out-of-Service producer %s emits kind %q, which MUST be declared in the taxonomy (no undeclared kind on the feed)",
					tc.file, k)
			}
			assert.Containsf(t, kinds, tc.wantKind,
				"%s must emit the declared kind %q", tc.file, tc.wantKind)
		})
	}
}
