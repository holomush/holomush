// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
package main

import (
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ref struct {
	File  string `yaml:"file"`
	Token string `yaml:"token"`
}
type entry struct {
	ID    string `yaml:"id"`
	Scope string `yaml:"scope"`
	Refs  []ref  `yaml:"refs"`
}
type doc struct {
	Invariants []entry `yaml:"invariants"`
}

func main() {
	scope := flag.String("scope", "", "scope to migrate, e.g. INV-PRESENCE")
	regPath := flag.String("registry", "docs/architecture/invariants.yaml", "registry path")
	dry := flag.Bool("dry-run", false, "print planned rewrites without writing")
	flag.Parse()
	if *scope == "" {
		fmt.Fprintln(os.Stderr, "inv-migrate: -scope is required")
		os.Exit(2)
	}
	data, err := os.ReadFile(*regPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var d doc
	if err = yaml.Unmarshal(data, &d); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var plan []rewrite
	for _, e := range d.Invariants {
		if e.Scope != *scope {
			continue
		}
		for _, rf := range e.Refs {
			plan = append(plan, rewrite{File: rf.File, Token: rf.Token, Canonical: e.ID})
		}
	}
	if *dry {
		for _, r := range plan {
			fmt.Printf("%s: %s -> %s\n", r.File, r.Token, r.Canonical)
		}
		return
	}
	n, err := rewriteAll(plan)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("inv-migrate %s: %d files rewritten\n", *scope, n)
}
