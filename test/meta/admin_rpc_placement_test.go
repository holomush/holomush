// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// protoRPCDecl matches an rpc declaration by keyword + name. It is the
// method-level peer of protoPackageDecl / protoServiceDecl
// (grpc_api_coverage_test.go), and this file is a SECOND CONSUMER of those two
// rather than a re-spelled walker: protoServices returns only sorted,
// package-qualified SERVICE names and exposes neither file contents nor rpc
// names, so it cannot supply what this fence needs.
var protoRPCDecl = regexp.MustCompile(`(?m)^\s*rpc\s+(\w+)\b`)

// adminRPCAllowedPackages is the EXPLICIT, two-element set of proto packages an
// `^Admin`-named rpc may live in. Each carries the reason it is allowed, and the
// reason is printed in the failure message so a future author reads the boundary
// instead of inferring it.
//
// It is spelled as exact package names. No prefix match, no wildcard: a fence
// whose membership test is "starts with holomush.admin" would admit a
// hypothetical holomush.adminsomething.v1 nobody reviewed.
var adminRPCAllowedPackages = map[string]string{
	"holomush.adminportal.v1": "the player-session admin portal — every method under it is gated by the " +
		"/holomush.adminportal.v1. interceptor prefix on the core gRPC server, from the fail-closed " +
		"section.AdminDescriptors table",
	"holomush.admin.v1": "the PRE-EXISTING break-glass operator control plane (rpc AdminReadStream, " +
		"api/proto/holomush/admin/v1/admin.proto), served over a UNIX domain socket by internal/admin/socket/ " +
		"under AssertOperatorAdmin and never over the core gRPC server. A single-package fence would be RED " +
		"at HEAD on that one rpc, and a guard that is red before this phase adds anything proves nothing " +
		"about what this phase adds",
}

// TestEveryAdminPrefixedRPCLivesInAnAdminPackage closes the class the character
// facade census cannot: an admin RPC landing on a THIRD service.
//
// The character census fences the character facade only. An `^Admin` rpc added
// to a new facade — or to SceneAccessService — sits outside
// /holomush.adminportal.v1. so the interceptor never gates it, and outside both
// censuses so neither sees it. This walks every .proto under api/proto and
// requires each such rpc to live in one of two explicitly reasoned packages.
//
// # Why the PROTO layer rather than registered ServiceDescs
//
// Placement is a wire-contract property, and the proto is the artifact a future
// author edits FIRST. A guard over registered descriptors fires only after the
// service is wired, which is later than the point at which the mistake is cheap.
//
// # What this fence does NOT cover, stated so nobody infers coverage it lacks
//
// It is a guard over a NAMING CONVENTION, not over PRIVILEGED SEMANTICS. A
// future privileged RPC named without the Admin prefix — `PurgeCharacter` on
// CharacterAccessService — escapes both this fence and the
// /holomush.adminportal.v1. interceptor prefix, and nothing in this phase
// catches it. Closing that class needs a DECLARED AUTHORIZATION DOMAIN: a proto
// option, or an explicit privileged-RPC registry that both the interceptor and
// this fence read. That is deliberately not built here; the limitation is
// recorded as threat T-06-02d with disposition `accept`.
func TestEveryAdminPrefixedRPCLivesInAnAdminPackage(t *testing.T) {
	root := findRepoRoot(t)

	matches, err := filepath.Glob(
		filepath.Join(root, "api", "proto", "holomush", "*", "v*", "*.proto"),
	)
	require.NoError(t, err, "glob proto sources")
	require.NotEmpty(t, matches, "no .proto files matched under api/proto/holomush/*/v* — the fence would pass vacuously")

	type rpcSite struct {
		pkg  string
		rpc  string
		file string
	}

	var pairs []rpcSite
	for _, path := range matches {
		src, readErr := os.ReadFile(path)
		require.NoErrorf(t, readErr, "read %s", path)

		pkgMatch := protoPackageDecl.FindSubmatch(src)
		require.NotNilf(t, pkgMatch, "%s declares no proto package", path)
		pkg := string(pkgMatch[1])

		rel, relErr := filepath.Rel(root, path)
		require.NoError(t, relErr)

		for _, m := range protoRPCDecl.FindAllSubmatch(src, -1) {
			pairs = append(pairs, rpcSite{pkg: pkg, rpc: string(m[1]), file: rel})
		}
	}
	require.NotEmpty(t, pairs,
		"no (package, rpc) pairs were collected — the glob matched files but the rpc regex found nothing, "+
			"so every assertion below would pass vacuously")

	// Sorted, so a failure enumerates offenders reproducibly.
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].pkg != pairs[j].pkg {
			return pairs[i].pkg < pairs[j].pkg
		}
		return pairs[i].rpc < pairs[j].rpc
	})

	allowed := make([]string, 0, len(adminRPCAllowedPackages))
	for pkg := range adminRPCAllowedPackages {
		allowed = append(allowed, pkg)
	}
	sort.Strings(allowed)

	adminRPCs := 0
	for _, p := range pairs {
		if !strings.HasPrefix(p.rpc, "Admin") {
			continue
		}
		adminRPCs++
		_, ok := adminRPCAllowedPackages[p.pkg]
		assert.Truef(t, ok,
			"rpc %s is declared in package %s (%s), which is not an admin package.\n"+
				"An Admin-prefixed rpc outside %v sits OUTSIDE the /holomush.adminportal.v1. interceptor "+
				"prefix, so the section gate never runs for it, and outside both routing censuses, so "+
				"neither sees it. Move it onto holomush.adminportal.v1.AdminPortalService.\n"+
				"The allowed packages and why: %s",
			p.rpc, p.pkg, p.file, allowed, formatAllowedAdminPackages())
	}

	require.Positivef(t, adminRPCs,
		"no Admin-prefixed rpc was found anywhere under api/proto. The fence has nothing to judge, "+
			"which means it can no longer go red — check the regex before trusting this pass")
}

// formatAllowedAdminPackages renders the allowed set with its reasons, in a
// stable order, for the failure message.
func formatAllowedAdminPackages() string {
	pkgs := make([]string, 0, len(adminRPCAllowedPackages))
	for pkg := range adminRPCAllowedPackages {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)

	var b strings.Builder
	for _, pkg := range pkgs {
		b.WriteString("\n  - ")
		b.WriteString(pkg)
		b.WriteString(": ")
		b.WriteString(adminRPCAllowedPackages[pkg])
	}
	return b.String()
}
