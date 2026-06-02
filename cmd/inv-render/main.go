// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Command inv-render renders docs/architecture/invariants.md from the
// machine-readable registry docs/architecture/invariants.yaml.
//
// invariants.md is a generated artifact within its BEGIN/END GENERATED
// regions; the surrounding prose is hand-authored. Run after editing the
// YAML:
//
//	go run ./cmd/inv-render            # rewrite invariants.md in place
//	go run ./cmd/inv-render -check     # CI: fail if invariants.md is stale
//
// The -check mode renders in memory and compares against the on-disk file
// without mutating it; CI uses it as a generate-and-diff guard, replacing the
// former dual-parse consistency lint (no markdown is ever parsed back).
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/holomush/holomush/internal/invregistry"
)

func main() {
	regPath := flag.String("registry", "docs/architecture/invariants.yaml", "registry YAML path")
	mdPath := flag.String("md", "docs/architecture/invariants.md", "rendered markdown path")
	check := flag.Bool("check", false, "verify the markdown is up to date without writing (exit 1 on drift)")
	flag.Parse()

	if err := run(*regPath, *mdPath, *check); err != nil {
		fmt.Fprintln(os.Stderr, "inv-render:", err)
		os.Exit(1)
	}
}

func run(regPath, mdPath string, check bool) error {
	doc, err := invregistry.Load(regPath)
	if err != nil {
		return err
	}
	current, err := os.ReadFile(mdPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", mdPath, err)
	}
	rendered, err := Render(doc, string(current))
	if err != nil {
		return err
	}
	if string(current) == rendered {
		return nil
	}
	if check {
		return fmt.Errorf("%s is out of date — run `task invariants:render` and commit the result", mdPath)
	}
	if err := os.WriteFile(mdPath, []byte(rendered), 0o644); err != nil { //nolint:gosec // G306: invariants.md is a committed doc, 0644 by repo convention
		return fmt.Errorf("write %s: %w", mdPath, err)
	}
	fmt.Printf("inv-render: wrote %s\n", mdPath)
	return nil
}
