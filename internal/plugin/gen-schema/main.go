// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// gen-schema generates the plugin JSON Schema file from Go types.
//
// Usage:
//
//	go generate ./internal/plugin/
//	# or directly:
//	go run ./internal/plugin/gen-schema
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	plugins "github.com/holomush/holomush/internal/plugin"
)

func main() {
	root := findModuleRoot()

	schema, err := plugins.GenerateSchema()
	if err != nil {
		log.Fatalf("generating schema: %v", err)
	}

	outPath := filepath.Join(root, "schemas", "plugin.schema.json")
	if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
		log.Fatalf("creating directory: %v", err)
	}

	if err := os.WriteFile(outPath, schema, 0o600); err != nil {
		log.Fatalf("writing schema: %v", err)
	}

	fmt.Printf("wrote %s\n", outPath)
}

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
		dir = parent
	}
	log.Fatal("could not find module root (go.mod)")
	return ""
}
