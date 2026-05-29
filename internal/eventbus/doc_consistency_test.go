// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goCodeBlock matches the first ```go fenced block in a Markdown document and
// captures its body (the interface declarations the rule file documents).
var goCodeBlock = regexp.MustCompile("(?s)```go\\n(.*?)\\n```")

// documentedInterfaces are the EventBus role interfaces that
// .claude/rules/event-interfaces.md mirrors from bus.go. Drift in any of these
// signatures is what this test guards against (holomush-dj95.4).
var documentedInterfaces = []string{"Publisher", "Subscriber", "HistoryReader", "EventBus"}

// TestEventInterfacesDocMatchesLiveInterfaces asserts that the interface
// signatures documented in .claude/rules/event-interfaces.md exactly match the
// live Publisher/Subscriber/HistoryReader/EventBus interfaces in bus.go.
//
// It compares structure, not text: both sources are parsed and each interface
// is rendered to a comment- and whitespace-normalized form, so reformatting or
// reworded doc comments don't trip it — but a changed method signature (e.g.
// adding a parameter to OpenSession) does. This is the anti-drift guard from
// holomush-dj95.4: changing a live signature without updating the rule file
// fails here.
func TestEventInterfacesDocMatchesLiveInterfaces(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must resolve current test file")
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))

	liveSrc := mustReadFile(t, filepath.Join(moduleRoot, "internal/eventbus/bus.go"))
	live := renderInterfaces(t, "bus.go", liveSrc)

	docPath := filepath.Join(moduleRoot, ".claude/rules/event-interfaces.md")
	docMarkdown := mustReadFile(t, docPath)
	match := goCodeBlock.FindSubmatch(docMarkdown)
	require.NotNil(t, match, "event-interfaces.md must contain a ```go interface block")
	// Wrap the block in a package clause so it parses as a Go file. The block
	// is written in the eventbus package's own source form (unqualified types,
	// named params), so its rendered interfaces match bus.go exactly.
	docSrc := append([]byte("package eventbus\n"), match[1]...)
	doc := renderInterfaces(t, "event-interfaces.md", docSrc)

	for _, name := range documentedInterfaces {
		liveSig, inLive := live[name]
		require.Truef(t, inLive, "interface %s not found in bus.go", name)
		docSig, inDoc := doc[name]
		require.Truef(t, inDoc, "interface %s not documented in .claude/rules/event-interfaces.md", name)
		assert.Equalf(t, liveSig, docSig,
			"interface %s in .claude/rules/event-interfaces.md is out of sync with bus.go; "+
				"regenerate the rule file's ```go block to match the live signature", name)
	}
}

// renderInterfaces parses Go source and returns, per interface type name, its
// normalized rendered form (comments stripped by the printer, whitespace
// collapsed). Embedded interfaces render as their type name.
func renderInterfaces(t *testing.T, name string, src []byte) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	require.NoErrorf(t, err, "parse %s", name)

	out := make(map[string]string)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if _, ok := ts.Type.(*ast.InterfaceType); !ok {
				continue
			}
			out[ts.Name.Name] = renderNode(t, fset, ts.Type)
		}
	}
	return out
}

// renderNode prints an AST node and collapses all runs of whitespace to single
// spaces, yielding a canonical form independent of indentation or line breaks.
// The printer does not emit comments for a bare sub-node, so doc-comment
// wording is excluded from the comparison.
func renderNode(t *testing.T, fset *token.FileSet, node ast.Node) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, printer.Fprint(&buf, fset, node))
	return strings.Join(strings.Fields(buf.String()), " ")
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoErrorf(t, err, "read %s", path)
	return data
}
