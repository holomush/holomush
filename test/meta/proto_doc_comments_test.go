// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestIsNameEcho(t *testing.T) {
	cases := []struct {
		name    string
		comment string
		elem    string
		want    bool
	}{
		{"bare name restatement returns true", " CreateSceneRequest", "CreateSceneRequest", true},
		{"name with request suffix stripped returns true", " CreateScene", "CreateSceneRequest", true},
		{"name with response suffix stripped returns true", " CreateScene", "CreateSceneResponse", true},
		{"snake-case field name restatement returns true", " next_cursor", "next_cursor", true},
		{"differing case and trailing period still returns true", " Key.", "key", true},
		{"substantive comment returns false", " Opaque pagination cursor from a prior page.", "cursor", false},
		{"empty comment returns false (buf handles emptiness)", "", "key", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isNameEcho(tc.comment, tc.elem))
		})
	}
}

// normalizeComment lowercases, trims whitespace and a trailing period, and
// collapses internal whitespace so "  Key. " and "key" compare equal.
func normalizeComment(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, ".")
	return strings.Join(strings.Fields(s), " ")
}

// echoSuffixes are conventional message suffixes a comment may drop and still
// be an echo: "// CreateScene" over message CreateSceneRequest. Proto message
// names are CamelCase, so the lowercased, underscore-free name only ever sheds
// the bare "request"/"response" tails.
var echoSuffixes = []string{"request", "response"}

// isNameEcho reports whether a leading comment merely restates the element
// name (optionally minus a conventional Request/Response suffix).
func isNameEcho(comment, elemName string) bool {
	c := normalizeComment(comment)
	if c == "" {
		return false // emptiness is buf's COMMENT_* job, not ours.
	}
	n := strings.ToLower(elemName)
	if c == n {
		return true
	}
	for _, suf := range echoSuffixes {
		if c == strings.TrimSuffix(n, suf) {
			return true
		}
	}
	return false
}

func TestProtoCommentsNoNameEcho(t *testing.T) {
	root := findRepoRoot(t)
	fds := buildFileDescriptorSet(t, root)

	for _, fd := range fds.GetFile() {
		if fd.GetSourceCodeInfo() == nil {
			continue
		}
		for _, loc := range fd.GetSourceCodeInfo().GetLocation() {
			lead := loc.GetLeadingComments()
			if lead == "" {
				continue
			}
			name, ok := elementName(fd, loc.GetPath())
			if !ok {
				continue
			}
			require.Falsef(t, isNameEcho(lead, name),
				"%s: leading comment for %q merely restates its name (comment=%q). "+
					"Write a substantive comment grounded in the Go handler.",
				fd.GetName(), name, strings.TrimSpace(lead))
		}
	}
}

// buildFileDescriptorSet shells `buf build --as-file-descriptor-set` for the
// public schema module. --as-file-descriptor-set yields a STANDARD
// descriptorpb.FileDescriptorSet (the default -o emits a buf Image). Source
// info (leading comments) is included unless --exclude-source-info is passed.
func buildFileDescriptorSet(t *testing.T, root string) *descriptorpb.FileDescriptorSet {
	t.Helper()
	out := filepath.Join(t.TempDir(), "schema.binpb")
	cmd := exec.Command("buf", "build", "api/proto", "--as-file-descriptor-set", "-o", out)
	cmd.Dir = root
	combined, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "buf build failed: %s", combined)
	data, err := os.ReadFile(out)
	require.NoError(t, err, "read FileDescriptorSet")
	fds := &descriptorpb.FileDescriptorSet{}
	require.NoError(t, proto.Unmarshal(data, fds), "unmarshal FileDescriptorSet")
	return fds
}

// elementName resolves a SourceCodeInfo path to the named element's simple
// name. Returns ok=false for paths that don't terminate on a named element
// (e.g. file-level options), which the caller skips.
func elementName(fd *descriptorpb.FileDescriptorProto, path []int32) (string, bool) {
	const (
		fileMessage = 4
		fileEnum    = 5
		fileService = 6
		svcMethod   = 2
	)
	if len(path) < 2 {
		return "", false
	}
	switch path[0] {
	case fileMessage:
		return messageName(fd.MessageType[path[1]], path[2:])
	case fileEnum:
		return enumName(fd.EnumType[path[1]], path[2:])
	case fileService:
		svc := fd.Service[path[1]]
		if len(path) == 2 {
			return svc.GetName(), true
		}
		if len(path) >= 4 && path[2] == svcMethod {
			return svc.Method[path[3]].GetName(), true
		}
	}
	return "", false
}

func messageName(m *descriptorpb.DescriptorProto, rest []int32) (string, bool) {
	const (
		msgField  = 2
		msgNested = 3
		msgEnum   = 4
		msgOneof  = 8
	)
	if len(rest) == 0 {
		return m.GetName(), true
	}
	if len(rest) < 2 {
		return "", false
	}
	switch rest[0] {
	case msgField:
		return m.Field[rest[1]].GetName(), true
	case msgOneof:
		return m.OneofDecl[rest[1]].GetName(), true
	case msgNested:
		return messageName(m.NestedType[rest[1]], rest[2:])
	case msgEnum:
		return enumName(m.EnumType[rest[1]], rest[2:])
	}
	return "", false
}

func enumName(e *descriptorpb.EnumDescriptorProto, rest []int32) (string, bool) {
	const enumValue = 2
	if len(rest) == 0 {
		return e.GetName(), true
	}
	if len(rest) >= 2 && rest[0] == enumValue {
		return e.Value[rest[1]].GetName(), true
	}
	return "", false
}

// INV-5: the lint:proto task body MUST run the name-echo gate.
func TestLintProtoRunsNameEcho(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "Taskfile.yaml"))
	require.NoError(t, err, "read Taskfile.yaml")
	body := taskBlock(t, string(data), "lint:proto")
	require.Contains(t, body, "TestProtoCommentsNoNameEcho",
		"lint:proto must run the name-echo gate (INV-5)")
}

// INV-1: buf.yaml lint.use MUST include COMMENTS.
func TestBufYAMLEnablesComments(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "buf.yaml"))
	require.NoError(t, err, "read buf.yaml")
	require.Contains(t, string(data), "- COMMENTS",
		"buf.yaml lint.use must enable the COMMENTS category (INV-1)")
}

// TestProtoDocInvariantsHaveTests guards the spec's "every invariant has an
// enforcing test" discipline: INV-1/3/5 each map to a named test. (INV-2 is
// buf lint in CI; INV-4's doc-ratchet bijection was retired once every proto
// was documented and full COMMENTS enforcement landed — SP0 close-out.)
func TestProtoDocInvariantsHaveTests(t *testing.T) {
	required := []string{
		"TestBufYAMLEnablesComments",  // INV-1
		"TestProtoCommentsNoNameEcho", // INV-3
		"TestLintProtoRunsNameEcho",   // INV-5
	}
	root := findRepoRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "test", "meta", "proto_doc_comments_test.go"))
	require.NoError(t, err)
	src := string(b)
	for _, name := range required {
		require.Containsf(t, src, "func "+name, "missing enforcing test %s", name)
	}
}
