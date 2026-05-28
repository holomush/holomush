// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type docRatchet struct {
	Pending []struct {
		Path string `yaml:"path"`
		Bead string `yaml:"bead"`
	} `yaml:"pending"`
}

// commentsRatchetRE pulls proto paths out of the buf.yaml COMMENTS: block.
// It matches `- api/proto/.../*.proto` lines following the COMMENTS: key.
var (
	commentsRatchetRE = regexp.MustCompile(`(?s)COMMENTS:\n(.*?)(?:\n\S|\nbreaking:|\z)`)
	protoPathRE       = regexp.MustCompile(`-\s+(api/proto/\S+\.proto)`)
)

func TestProtoDocRatchetBijection(t *testing.T) {
	root := findRepoRoot(t)

	regPath := filepath.Join(root, "api", "proto", "doc-ratchet.yaml")
	regData, err := os.ReadFile(regPath)
	require.NoError(t, err, "read doc-ratchet.yaml")
	var reg docRatchet
	require.NoError(t, yaml.Unmarshal(regData, &reg), "parse doc-ratchet.yaml")

	var regPaths []string
	for _, e := range reg.Pending {
		require.NotEmpty(t, e.Bead, "registry entry %s missing bead", e.Path)
		require.Regexp(t, `^holomush-[a-z0-9.]+$`, e.Bead,
			"registry entry %s has placeholder/invalid bead %q", e.Path, e.Bead)
		regPaths = append(regPaths, e.Path)
	}

	bufData, err := os.ReadFile(filepath.Join(root, "buf.yaml"))
	require.NoError(t, err, "read buf.yaml")
	block := commentsRatchetRE.FindStringSubmatch(string(bufData))
	var bufPaths []string
	if len(block) == 2 {
		for _, m := range protoPathRE.FindAllStringSubmatch(block[1], -1) {
			bufPaths = append(bufPaths, m[1])
		}
	}

	sort.Strings(regPaths)
	sort.Strings(bufPaths)
	require.Equal(t, bufPaths, regPaths,
		"buf.yaml COMMENTS ratchet and doc-ratchet.yaml must list the same protos "+
			"(buf=%v registry=%v)", bufPaths, regPaths)
}

func TestProtoDocInvariantsHaveTests(t *testing.T) {
	// INV-1..INV-5 each map to a named test. This guards the spec's
	// "every invariant has an enforcing test" discipline.
	required := []string{
		"TestBufYAMLEnablesComments",   // INV-1
		"TestProtoCommentsNoNameEcho",  // INV-3 (INV-2 = buf lint in CI)
		"TestProtoDocRatchetBijection", // INV-4
		"TestLintProtoRunsNameEcho",    // INV-5
	}
	root := findRepoRoot(t)
	var src []byte
	for _, f := range []string{"proto_doc_comments_test.go", "proto_doc_ratchet_test.go"} {
		b, err := os.ReadFile(filepath.Join(root, "test", "meta", f))
		require.NoError(t, err)
		src = append(src, b...)
	}
	for _, name := range required {
		require.Containsf(t, string(src), "func "+name, "missing enforcing test %s", name)
	}
}
