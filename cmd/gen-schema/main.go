// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Command gen-schema generates the plugin JSON Schema file.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/holomush/holomush/internal/plugin"
)

func main() {
	schema, err := plugin.GenerateSchema()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating schema: %v\n", err)
		os.Exit(1)
	}

	outPath := filepath.Join("schemas", "plugin.schema.json")
	if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating directory: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outPath, schema, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated %s\n", outPath)
}
