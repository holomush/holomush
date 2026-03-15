// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// gen-ebnf generates the EBNF grammar and railroad diagram from the DSL parser.
//
// Usage:
//
//	go generate ./internal/access/policy/dsl/
//	# or directly:
//	go run cmd/gen-ebnf/main.go
//
// Outputs (relative to module root):
//   - site/docs/developers/policy-dsl.ebnf
//   - site/docs/developers/policy-dsl-railroad.html
package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/holomush/holomush/internal/access/policy/dsl"
)

func main() {
	root := findModuleRoot()
	ebnf := dsl.EBNF()

	ebnfPath := filepath.Join(root, "site", "docs", "developers", "policy-dsl.ebnf")
	if err := os.WriteFile(ebnfPath, []byte(ebnf), 0o644); err != nil { //nolint:gosec // documentation file, world-readable is correct
		log.Fatalf("writing EBNF: %v", err)
	}
	fmt.Printf("wrote %s\n", ebnfPath)

	railroadPath := filepath.Join(root, "site", "docs", "developers", "policy-dsl-railroad.html")
	cmd := exec.Command("go", "run", "github.com/alecthomas/participle/v2/cmd/railroad@latest")
	cmd.Stdin = bytes.NewReader([]byte(ebnf))

	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("running railroad tool: %v", err)
	}

	if err := os.WriteFile(railroadPath, out, 0o644); err != nil { //nolint:gosec // documentation file, world-readable is correct
		log.Fatalf("writing railroad diagram: %v", err)
	}
	fmt.Printf("wrote %s\n", railroadPath)
}

// findModuleRoot walks up from the current directory to find go.mod.
func findModuleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		log.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		// Safety: don't walk above the filesystem root
		if !strings.Contains(parent, "holomush") && parent == "/" {
			break
		}
		dir = parent
	}
	log.Fatal("could not find module root (go.mod)")
	return ""
}
