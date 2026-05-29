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

	"github.com/stretchr/testify/require"
)

// SP4 (holomush-okm59): the generated gRPC API reference MUST cover every
// service in the public proto module. The original docs:proto task fed protoc
// a hardcoded list of 5 proto files (6 of 12 services); a new service silently
// fell out of the reference. Generation is now buf-driven over the whole
// api/proto directory, so coverage is structural — these tests guard that the
// mechanism stays in place and that the rendered doc actually contains every
// service.

var (
	protoPackageDecl = regexp.MustCompile(`(?m)^package\s+([\w.]+)\s*;`)
	// Match the service declaration by keyword + name; the opening brace may sit
	// on the same or the next line (both valid protobuf), so it is not required.
	protoServiceDecl = regexp.MustCompile(`(?m)^service\s+(\w+)\b`)
	// docAnchor matches protoc-gen-doc's per-element definition anchors, e.g.
	// `<a name="holomush-core-v1-CoreService"></a>`. The TOC references the same
	// names via `[…](#anchor)` links, so an `<a name>` proves the section was
	// actually rendered, not merely linked.
	docAnchor = regexp.MustCompile(`<a name="([^"]+)"></a>`)
)

// protoService pairs a declared service with the doc anchor protoc-gen-doc
// emits at its definition: the proto package (dots → dashes) joined to the
// service name. Proto forbids a message and a service sharing a name within a
// package, so this package-qualified anchor uniquely identifies the service —
// unlike a bare `### Name` heading, which a same-named message in another
// package could satisfy.
type protoService struct {
	name   string
	anchor string
}

// TestGRPCReferenceCoversAllServices asserts every `service` declared under
// api/proto renders its definition section in grpc-api.md, matched by the
// package-qualified anchor rather than a bare heading.
func TestGRPCReferenceCoversAllServices(t *testing.T) {
	root := findRepoRoot(t)

	services := protoServices(t, root)
	require.NotEmpty(t, services, "no services found under api/proto — proto layout changed?")

	doc, err := os.ReadFile(filepath.Join(root,
		"site", "src", "content", "docs", "reference", "grpc-api.md"))
	require.NoError(t, err, "read generated grpc-api.md")

	anchors := map[string]bool{}
	for _, m := range docAnchor.FindAllStringSubmatch(string(doc), -1) {
		anchors[m[1]] = true
	}

	var missing []string
	for _, svc := range services {
		if !anchors[svc.anchor] {
			missing = append(missing, svc.name+" ("+svc.anchor+")")
		}
	}
	sort.Strings(missing)
	require.Emptyf(t, missing,
		"grpc-api.md is missing %d service section(s): %s. Run `task docs:proto` and commit the result.",
		len(missing), strings.Join(missing, ", "))
}

// TestDocsProtoUsesBufGenerate pins the structural-coverage mechanism: the
// docs:proto task body MUST drive generation through buf (whole-module input),
// not a hand-maintained protoc file list that can drift out of coverage.
func TestDocsProtoUsesBufGenerate(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "Taskfile.yaml"))
	require.NoError(t, err, "read Taskfile.yaml")
	body := taskBlock(t, string(data), "docs:proto")
	require.Contains(t, body, "buf generate --template buf.gen.docs.yaml",
		"docs:proto must generate via buf over the whole module (SP4 structural coverage)")

	tmpl, err := os.ReadFile(filepath.Join(root, "buf.gen.docs.yaml"))
	require.NoError(t, err, "read buf.gen.docs.yaml")
	require.Contains(t, string(tmpl), "directory: api/proto",
		"buf.gen.docs.yaml must input the whole api/proto module")
}

// TestDocsProtoPostProcessingApplied guards the two perl passes in docs:proto
// that run after buf generate: the Starlight frontmatter prepend (replacing
// protoc-gen-doc's H1) and the well-known-type link rewrite. Without these the
// page fails to render in the docs site or carries dead `#google-protobuf-*`
// fragment links.
func TestDocsProtoPostProcessingApplied(t *testing.T) {
	root := findRepoRoot(t)
	doc, err := os.ReadFile(filepath.Join(root,
		"site", "src", "content", "docs", "reference", "grpc-api.md"))
	require.NoError(t, err, "read generated grpc-api.md")
	md := string(doc)

	require.True(t, strings.HasPrefix(md, "---\ntitle: \"gRPC API Reference\"\n---\n"),
		"frontmatter prepend pass did not run (docs:proto perl H1-strip)")
	require.Contains(t, md, "https://protobuf.dev/reference/protobuf/google.protobuf/",
		"well-known-type link rewrite produced no protobuf.dev links")
	require.NotContains(t, md, "#google-protobuf-",
		"raw protoc-gen-doc google.protobuf anchors remain — link rewrite pass did not run")
}

// protoServices returns the sorted, de-duplicated services declared in the
// public module. The `v*` glob covers every version directory the
// PACKAGE_DIRECTORY_MATCH layout allows (v1, a future v2, …); buf's own input
// is the broader `directory: api/proto`.
func protoServices(t *testing.T, root string) []protoService {
	t.Helper()
	matches, err := filepath.Glob(
		filepath.Join(root, "api", "proto", "holomush", "*", "v*", "*.proto"),
	)
	require.NoError(t, err, "glob proto sources")
	require.NotEmpty(t, matches, "no .proto files matched under api/proto/holomush/*/v*")

	seen := map[string]protoService{}
	for _, path := range matches {
		src, err := os.ReadFile(path)
		require.NoErrorf(t, err, "read %s", path)
		text := string(src)

		pkg := protoPackageDecl.FindStringSubmatch(text)
		require.Lenf(t, pkg, 2, "no package declaration in %s", path)
		anchorPrefix := strings.ReplaceAll(pkg[1], ".", "-") + "-"

		for _, m := range protoServiceDecl.FindAllStringSubmatch(text, -1) {
			svc := protoService{name: m[1], anchor: anchorPrefix + m[1]}
			seen[svc.anchor] = svc // key by package-qualified anchor, not bare name
		}
	}

	out := make([]protoService, 0, len(seen))
	for _, svc := range seen {
		out = append(out, svc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].anchor < out[j].anchor })
	return out
}
