// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Command nats-floor-guard is a deterministic supply-chain compensating control
// wired into `task lint:vuln`. It fails the build when go.mod pins
// github.com/nats-io/nats-server/v2 below the security floor v2.14.3.
//
// Why a bespoke guard instead of relying on the scanners: the nats-server
// advisory GHSA-q59r-vq66-pxc2 (alias CVE-2026-58207) is published in OSV as a
// git-commit-range-only record with NO Go-ecosystem package binding, so neither
// osv-scanner (manifest scan) nor govulncheck (Go vuln DB, reachability) can
// flag a vulnerable nats-server v2.14.2 from go.mod. This guard closes that
// blind spot: a regression to a nats-server below v2.14.3 deterministically
// fails lint:vuln, independent of any vulnerability database.
package main

import (
	"fmt"
	"os"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

const (
	natsModulePath    = "github.com/nats-io/nats-server/v2"
	natsSecurityFloor = "v2.14.3"
	goModPath         = "go.mod"
)

// checkNatsFloor parses go.mod content and returns a non-nil error when the
// required nats-server/v2 version is below floor, when nats-server/v2 is absent
// from the require block, or when the pinned version is not valid semver.
func checkNatsFloor(gomod []byte, floor string) error {
	f, err := modfile.Parse(goModPath, gomod, nil)
	if err != nil {
		return fmt.Errorf("parse go.mod: %w", err)
	}
	for _, req := range f.Require {
		if req.Mod.Path != natsModulePath {
			continue
		}
		v := req.Mod.Version
		if !semver.IsValid(v) {
			return fmt.Errorf("%s has invalid semver version %q", natsModulePath, v)
		}
		if semver.Compare(v, floor) < 0 {
			return fmt.Errorf(
				"%s is pinned at %s, below the required security floor %s "+
					"(GHSA-q59r-vq66-pxc2 / CVE-2026-58207 is a git-range-only OSV "+
					"record no manifest scanner can flag) — bump to >= %s",
				natsModulePath, v, floor, floor,
			)
		}
		return nil
	}
	return fmt.Errorf("%s not found in go.mod require block", natsModulePath)
}

func main() {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nats-floor-guard: cannot read %s: %v\n", goModPath, err)
		os.Exit(1)
	}
	if err := checkNatsFloor(data, natsSecurityFloor); err != nil {
		fmt.Fprintf(os.Stderr, "nats-floor-guard: FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("nats-floor-guard: OK — %s pinned >= %s\n", natsModulePath, natsSecurityFloor)
}
