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
// EFFECTIVE nats-server/v2 version is below natsSecurityFloor, when
// nats-server/v2 is absent from the require block, or when the pinned version is
// not valid semver.
//
// A `replace` directive is honored before the comparison: a `require` pin can
// look compliant while `go.mod` swaps nats-server for an older tag, a fork, or a
// local path. The guard resolves the replacement's version and compares that;
// a versionless replacement (local/filesystem path or bare directory) is
// unverifiable, so the guard fails CLOSED rather than trusting the require pin.
func checkNatsFloor(gomod []byte) error {
	f, err := modfile.Parse(goModPath, gomod, nil)
	if err != nil {
		return fmt.Errorf("parse go.mod: %w", err)
	}

	requiredVersion := ""
	for _, req := range f.Require {
		if req.Mod.Path == natsModulePath {
			requiredVersion = req.Mod.Version
			break
		}
	}
	if requiredVersion == "" {
		return fmt.Errorf("%s not found in go.mod require block", natsModulePath)
	}

	// The require pin is the effective version unless a matching replace overrides it.
	effectivePath, effectiveVersion := natsModulePath, requiredVersion
	for _, rep := range f.Replace {
		if rep.Old.Path != natsModulePath {
			continue
		}
		// A replace with a specific Old.Version only applies to that exact
		// version; an empty Old.Version replaces every version of the module.
		if rep.Old.Version != "" && rep.Old.Version != requiredVersion {
			continue
		}
		if rep.New.Version == "" {
			// Local/filesystem path or versionless replacement — the code that
			// actually builds is unverifiable against the floor. Fail closed.
			return fmt.Errorf(
				"%s is replaced by %q with no resolvable version (local path or "+
					"versionless replacement) — cannot verify it meets the security "+
					"floor %s; fail closed", natsModulePath, rep.New.Path, natsSecurityFloor,
			)
		}
		effectivePath, effectiveVersion = rep.New.Path, rep.New.Version
		break
	}

	if !semver.IsValid(effectiveVersion) {
		return fmt.Errorf("%s effective version %q (via %s) is not valid semver",
			natsModulePath, effectiveVersion, effectivePath)
	}
	if semver.Compare(effectiveVersion, natsSecurityFloor) < 0 {
		return fmt.Errorf(
			"%s resolves to %s (via %s), below the required security floor %s "+
				"(GHSA-q59r-vq66-pxc2 / CVE-2026-58207 is a git-range-only OSV "+
				"record no manifest scanner can flag) — bump to >= %s",
			natsModulePath, effectiveVersion, effectivePath, natsSecurityFloor, natsSecurityFloor,
		)
	}
	return nil
}

func main() {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nats-floor-guard: cannot read %s: %v\n", goModPath, err)
		os.Exit(1)
	}
	if err := checkNatsFloor(data); err != nil {
		fmt.Fprintf(os.Stderr, "nats-floor-guard: FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("nats-floor-guard: OK — %s pinned >= %s\n", natsModulePath, natsSecurityFloor)
}
