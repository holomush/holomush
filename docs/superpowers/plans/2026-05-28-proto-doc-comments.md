<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# SP0 — Proto Doc Comments + buf `COMMENTS` Ratchet Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Author substantive, Go-grounded doc comments on every message, field, RPC, service, enum, and enum value across all 14 protos, guarded by a buf `COMMENTS` lint ratchet plus a name-echo quality meta-check, so coverage can only grow.

**Architecture:** A per-path `lint.ignore_only.COMMENTS` ratchet in `buf.yaml` exempts not-yet-documented protos; each authoring PR documents one proto and removes its line. A Go meta-test rejects name-echo comments (built from a standard `FileDescriptorSet`). A registry (`api/proto/doc-ratchet.yaml`) is kept in bijection with the ratchet, each entry citing an open bead.

**Tech Stack:** Protocol Buffers, `buf` (v2 lint), Go `testing` + testify, `google.golang.org/protobuf/types/descriptorpb`, `gopkg.in/yaml.v3`, go-task.

**Spec:** `docs/superpowers/specs/2026-05-28-proto-doc-comments-design.md`
**Design bead:** holomush-300ad

---

## File Structure

| File | Responsibility | Phase |
| ---- | -------------- | ----- |
| `test/meta/proto_doc_comments_test.go` (create) | Name-echo gate (INV-3) + `isNameEcho` helper + INV-1 buf.yaml config assertion + INV-5 Taskfile-wiring assertion | 1 |
| `test/meta/proto_doc_ratchet_test.go` (create) | Registry↔buf.yaml bijection + ID-format guard (INV-4, CI half) | 1 |
| `scripts/proto-ratchet-audit.sh` + `Taskfile.yaml` `proto-ratchet:audit` (create) | Open-bead audit (INV-4, local/pre-close half) | 1 |
| `Taskfile.yaml` (modify `lint:proto`, ~line 501) | Run the name-echo gate in the lint gate | 1 |
| `buf.yaml` (modify lint block, ~line 28) | Enable `COMMENTS`; ratchet `ignore_only.COMMENTS` | 1 |
| `api/proto/doc-ratchet.yaml` (create) | Registry of pending protos, each citing an open bead | 1 |
| `api/proto/holomush/eventbus/v1/eventbus.proto` (modify) | First documented proto (loop proof) | 1 |
| `api/proto/holomush/content/v1/content.proto` (modify) | Second documented proto (loop proof) | 1 |
| `.claude/rules/proto-doc-comments.md` (create) | Auto-loading authoring guidance | 1 |
| `CLAUDE.md` (modify Code Conventions) | One-line pointer to the rule | 1 |
| `site/src/content/docs/contributing/proto-doc-comments.md` (create) | Contributor doc | 1 |
| The 12 protos in the ratchet (modify, one per task) | Authoring + ratchet removal | 2 |

---

## Phase 1 — Infrastructure & Loop Proof

### Task 1: Name-echo quality gate + lint wiring

**Files:**

- Create: `test/meta/proto_doc_comments_test.go`
- Modify: `Taskfile.yaml` (`lint:proto` target, ~line 501-505)
- Test: the file IS the test (`test/meta/`)

Repo facts: `test/meta/` is package `meta`; `findRepoRoot(t)` already exists in `test/meta/inv_binding_test.go`. `buf` is on PATH (used by `task lint:proto`). `descriptorpb` and `testify` are already in `go.mod`.

- [ ] **Step 1: Write the failing helper unit test**

Add to a new file `test/meta/proto_doc_comments_test.go`:

```go
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
		{"bare name echo", " CreateSceneRequest", "CreateSceneRequest", true},
		{"name with request suffix stripped", " CreateScene", "CreateSceneRequest", true},
		{"name with response suffix stripped", " CreateScene", "CreateSceneResponse", true},
		{"snake field echo", " next_cursor", "next_cursor", true},
		{"case and punctuation insensitive", " Key.", "key", true},
		{"substantive comment passes", " Opaque pagination cursor from a prior page.", "cursor", false},
		{"empty comment is not echo (buf handles emptiness)", "", "key", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isNameEcho(tc.comment, tc.elem))
		})
	}
}
```

- [ ] **Step 2: Run it to verify it fails (undefined `isNameEcho`)**

Run: `task test -- -run TestIsNameEcho ./test/meta/`
Expected: FAIL — `undefined: isNameEcho`.

- [ ] **Step 3: Implement `isNameEcho`**

Append to `test/meta/proto_doc_comments_test.go`:

```go
// normalizeComment lowercases, trims whitespace and a trailing period, and
// collapses internal whitespace so "  Key. " and "key" compare equal.
func normalizeComment(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, ".")
	return strings.Join(strings.Fields(s), " ")
}

// echoSuffixes are conventional message suffixes a comment may drop and still
// be an echo: "// CreateScene" over message CreateSceneRequest.
var echoSuffixes = []string{"request", "response", "_request", "_response"}

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
```

- [ ] **Step 4: Run helper test to verify it passes**

Run: `task test -- -run TestIsNameEcho ./test/meta/`
Expected: PASS.

- [ ] **Step 5: Write the full-scan gate test**

Append the gate that builds a standard `FileDescriptorSet` and walks every element's leading comment. The path encoding uses well-known field numbers: file→`message_type=4`, `enum_type=5`, `service=6`; message→`field=2`, `nested_type=3`, `enum_type=4`, `oneof_decl=8`; service→`method=2`; enum→`value=2`.

```go
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
		msgField    = 2
		msgNested   = 3
		msgEnum     = 4
		msgOneof    = 8
		svcMethod   = 2
		enumValue   = 2
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
		if path[2] == svcMethod {
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
	if rest[0] == enumValue {
		return e.Value[rest[1]].GetName(), true
	}
	return "", false
}
```

- [ ] **Step 6: Run the gate; fix any pre-existing echoes**

Run: `task test -- -run TestProtoCommentsNoNameEcho ./test/meta/`
Expected: PASS, OR a list of pre-existing echo comments. If it fails, fix each flagged comment in its `.proto` file (these are by definition low-value name restatements) until the test passes. Do **not** weaken `isNameEcho` to make them pass.

- [ ] **Step 7: Wire the gate into `task lint:proto`**

Modify `Taskfile.yaml` `lint:proto` (currently `buf lint` + `buf format --diff --exit-code`):

```yaml
  lint:proto:
    desc: Lint protobuf definitions (matches CI's Buf Lint check)
    cmds:
      - buf lint
      - buf format --diff --exit-code
      - go test ./test/meta/ -run TestProtoCommentsNoNameEcho
```

Only the name-echo gate runs here (INV-5). The INV-1 config assertion is added
in Task 4 (where `- COMMENTS` first appears in `buf.yaml`), so every commit in
Task 1 leaves `task lint:proto` green.

- [ ] **Step 8: Add the INV-5 wiring assertion**

Append to `test/meta/proto_doc_comments_test.go`. Use the existing `taskBlock`
helper (`test/meta/pr_prep_fast_lane_test.go:16`) so the check is scoped to the
`lint:proto` task body, not a stray match anywhere in `Taskfile.yaml`:

```go
// INV-5: the lint:proto task body MUST run the name-echo gate.
func TestLintProtoRunsNameEcho(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "Taskfile.yaml"))
	require.NoError(t, err, "read Taskfile.yaml")
	body := taskBlock(t, string(data), "lint:proto")
	require.Contains(t, body, "TestProtoCommentsNoNameEcho",
		"lint:proto must run the name-echo gate (INV-5)")
}
```

- [ ] **Step 9: Run the meta-tests**

Run: `task test -- ./test/meta/`
Expected: PASS — `TestIsNameEcho`, `TestProtoCommentsNoNameEcho`, and
`TestLintProtoRunsNameEcho` all green. (No INV-1 assertion exists yet; it
arrives in Task 4.)

- [ ] **Step 10: Commit**

```bash
jj describe -m "test(proto): name-echo doc-comment gate + lint:proto wiring (INV-3, INV-5)"
jj new
```

---

### Task 2: Document `eventbus.proto` (loop proof 1)

**Files:**

- Modify: `api/proto/holomush/eventbus/v1/eventbus.proto`
- Grounding: `internal/eventbus/` (host envelope: `core.Event`, `Actor`, codec)

Current gaps: enum values `ACTOR_KIND_*` (5), `Actor.kind`, `Event.subject`, `Event.type`, `Event.timestamp`, `Event.actor` lack comments. `Event.id`, `Event.payload`, `Event.rendering`, `Actor.id` already have inline comments.

- [ ] **Step 1: Probe the implementation for grounding**

Run: `mcp__probe__search_code "ActorKind Event envelope JetStream"` (path `internal/eventbus`). Confirm what each enum value and field means in the publish path before writing.

- [ ] **Step 2: Add leading comments to every undocumented element**

Add a substantive leading comment above each enum value and field. Example (write the rest analogously, grounded in the codec/publish code — do NOT echo names):

```proto
enum ActorKind {
  // ACTOR_KIND_UNSPECIFIED is the zero value; a well-formed envelope never
  // carries it — emitters MUST set a concrete kind.
  ACTOR_KIND_UNSPECIFIED = 0;
  // ACTOR_KIND_CHARACTER marks an event caused by an in-game character action.
  ACTOR_KIND_CHARACTER = 1;
  // ACTOR_KIND_PLAYER marks an event caused by the human account, not a character.
  ACTOR_KIND_PLAYER = 2;
  // ACTOR_KIND_SYSTEM marks an event the host itself originated.
  ACTOR_KIND_SYSTEM = 3;
  // ACTOR_KIND_PLUGIN marks an event a plugin emitted; gated by the
  // manifest's actor_kinds_claimable list at event_emitter.go::Emit.
  ACTOR_KIND_PLUGIN = 4;
}
```

For `Event.subject`: the NATS dot-delimited subject (`events.<game_id>.<domain>...`). For `Event.type`: the event-type discriminator. For `Event.timestamp`: when the event occurred (host clock). For `Event.actor`: who caused it. For `Actor.kind`: which `ActorKind` this actor is.

- [ ] **Step 3: Verify name-echo passes**

Run: `task test -- -run TestProtoCommentsNoNameEcho ./test/meta/`
Expected: PASS.

- [ ] **Step 4: Verify buf lint (COMMENTS not yet enabled, so this just confirms no regression)**

Run: `task lint:proto`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj describe -m "docs(proto): document eventbus.proto fields and enum values"
jj new
```

---

### Task 3: Document `content.proto` (loop proof 2)

**Files:**

- Modify: `api/proto/holomush/content/v1/content.proto`
- Grounding: `internal/grpc/content_service.go`

Current gaps: all fields on `GetContentRequest` (`key`), `GetContentResponse` (`item`), `ListContentRequest` (`prefix`, `limit`, `cursor`), `ListContentResponse` (`items`, `next_cursor`), `ContentItem` (`key`, `content_type`, `body`, `metadata`) lack comments, and all 5 messages lack leading comments. Service + RPCs are already commented.

- [ ] **Step 1: Probe the implementation for grounding**

Run: `mcp__probe__extract_code` on the `ContentService` impl in `internal/grpc/content_service.go` to confirm key semantics (is `limit` capped? is `cursor` opaque? what is `metadata` keyed by?).

- [ ] **Step 2: Add leading comments to every message and field**

Example (write all, grounded — no echoes):

```proto
// GetContentRequest selects a single content item by its exact storage key.
message GetContentRequest {
  // key is the exact content-store key; no prefix matching.
  string key = 1;
}

message ListContentRequest {
  // prefix restricts results to keys beginning with this string.
  string prefix = 1;
  // limit caps the page size; the server clamps values above its maximum.
  int32 limit = 2;
  // cursor is the opaque next_cursor from a prior page; empty starts at the top.
  string cursor = 3;
}
```

- [ ] **Step 3: Verify name-echo passes**

Run: `task test -- -run TestProtoCommentsNoNameEcho ./test/meta/`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj describe -m "docs(proto): document content.proto messages and fields"
jj new
```

---

### Task 4: Enable `COMMENTS` + populate the ratchet

**Files:**

- Modify: `buf.yaml` (lint block, ~line 28-45)
- Test: `test/meta/proto_doc_comments_test.go` (`TestBufYAMLEnablesComments`)

The 12 protos NOT documented in Tasks 2-3 go into the ratchet. `eventbus.proto` and `content.proto` are omitted (now enforced).

- [ ] **Step 1: Enable COMMENTS and add the ratchet list**

Edit `buf.yaml`:

```yaml
lint:
  use:
    - STANDARD
    - COMMENTS
  ignore_only:
    PACKAGE_DIRECTORY_MATCH:
      - internal/eventbus/cursor/cursor.proto
    RPC_REQUEST_RESPONSE_UNIQUE:
      - api/proto/holomush/admin/v1/admin.proto
    RPC_RESPONSE_STANDARD_NAME:
      - api/proto/holomush/admin/v1/admin.proto
    # SP0 ratchet (holomush-300ad): each proto below is exempt from COMMENT_*
    # until fully documented. Document → remove this line AND its
    # api/proto/doc-ratchet.yaml entry in the same PR. Delete this whole block
    # when empty. Bijection enforced by test/meta/proto_doc_ratchet_test.go.
    COMMENTS:
      - api/proto/holomush/core/v1/core.proto
      - api/proto/holomush/plugin/v1/plugin.proto
      - api/proto/holomush/plugin/v1/hostfunc.proto
      - api/proto/holomush/plugin/v1/audit.proto
      - api/proto/holomush/plugin/v1/attribute.proto
      - api/proto/holomush/web/v1/web.proto
      - api/proto/holomush/scene/v1/scene.proto
      - api/proto/holomush/world/v1/world.proto
      - api/proto/holomush/control/v1/control.proto
      - api/proto/holomush/admin/v1/admin.proto
      - api/proto/holomush/admin/v1/rekey.proto
      - api/proto/holomush/admin/v1/read_stream.proto
```

- [ ] **Step 2: Verify lint passes (eventbus/content enforced, 12 exempt)**

Run: `task lint:proto`
Expected: PASS. If a `COMMENT_*` violation fires on `eventbus.proto` or `content.proto`, finish documenting that element (Task 2/3 gap).

- [ ] **Step 3: Add the INV-1 config assertion**

Append to `test/meta/proto_doc_comments_test.go` (it can only pass now that
Step 1 added `- COMMENTS`):

```go
// INV-1: buf.yaml lint.use MUST include COMMENTS.
func TestBufYAMLEnablesComments(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "buf.yaml"))
	require.NoError(t, err, "read buf.yaml")
	require.Contains(t, string(data), "- COMMENTS",
		"buf.yaml lint.use must enable the COMMENTS category (INV-1)")
}
```

Run: `task test -- -run TestBufYAMLEnablesComments ./test/meta/`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj describe -m "build(buf): enable COMMENTS lint category + SP0 per-proto ratchet"
jj new
```

---

### Task 5: Registry + bijection meta-test (INV-4)

**Files:**

- Create: `api/proto/doc-ratchet.yaml`
- Create: `test/meta/proto_doc_ratchet_test.go`

**Ordering note (blocking):** the registry cites the Phase 2 authoring beads
(Tasks 7-18). Those bead IDs exist only **after** `plan-to-beads` materializes
this plan's epic + children. This task MUST be implemented after materialization.
Recover the child IDs with `bd show holomush-300ad` (or `bd list --parent
holomush-300ad`) and map each Phase 2 proto to its child bead. The bijection
test's `require.Regexp(^holomush-[a-z0-9.]+$)` deliberately **rejects** the
`holomush-XXXX` placeholder below — that guard exists precisely so a registry
left un-filled fails CI rather than passing silently.

- [ ] **Step 1: Create the registry with REAL bead IDs**

Create `api/proto/doc-ratchet.yaml`. The `holomush-XXXX` tokens below are a
template — replace **every one** with the real lowercase Phase 2 child bead ID
for that proto before running any test. A remaining `XXXX` is a guaranteed test
failure by design.

```yaml
# SPDX-License-Identifier: Apache-2.0
# SP0 proto-doc ratchet registry (holomush-300ad).
# INV-4: every entry MUST match a buf.yaml lint.ignore_only.COMMENTS line
# exactly AND cite an OPEN bead. Document a proto → delete BOTH its buf.yaml
# line and this entry, then close the bead. Delete this file when empty.
pending:
  - path: api/proto/holomush/core/v1/core.proto
    bead: holomush-XXXX
  - path: api/proto/holomush/plugin/v1/plugin.proto
    bead: holomush-XXXX
  - path: api/proto/holomush/plugin/v1/hostfunc.proto
    bead: holomush-XXXX
  - path: api/proto/holomush/plugin/v1/audit.proto
    bead: holomush-XXXX
  - path: api/proto/holomush/plugin/v1/attribute.proto
    bead: holomush-XXXX
  - path: api/proto/holomush/web/v1/web.proto
    bead: holomush-XXXX
  - path: api/proto/holomush/scene/v1/scene.proto
    bead: holomush-XXXX
  - path: api/proto/holomush/world/v1/world.proto
    bead: holomush-XXXX
  - path: api/proto/holomush/control/v1/control.proto
    bead: holomush-XXXX
  - path: api/proto/holomush/admin/v1/admin.proto
    bead: holomush-XXXX
  - path: api/proto/holomush/admin/v1/rekey.proto
    bead: holomush-XXXX
  - path: api/proto/holomush/admin/v1/read_stream.proto
    bead: holomush-XXXX
```

- [ ] **Step 2: Write the bijection test**

Create `test/meta/proto_doc_ratchet_test.go`:

```go
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
var commentsRatchetRE = regexp.MustCompile(`(?s)COMMENTS:\n(.*?)(?:\n\S|\nbreaking:|\z)`)
var protoPathRE = regexp.MustCompile(`-\s+(api/proto/\S+\.proto)`)

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
```

- [ ] **Step 3: Run the bijection test**

Run: `task test -- -run TestProtoDocRatchetBijection ./test/meta/`
Expected: PASS — 12 paths on each side, every `bead:` a real lowercase ID. If it
fails on a regex assertion, a `holomush-XXXX` placeholder is still present (fill
it). The bijection test checks path↔entry correspondence and ID **format** only;
the open-bead half of INV-4 is the audit script in Step 4.

- [ ] **Step 4: Add the open-bead audit (INV-4, open half)**

The bijection test runs in CI where `bd` may be absent, so the bead-status check
lives in a separate script — exactly how `task quarantine:audit`
(`scripts/quarantine-audit.sh`) is scoped. Create `scripts/proto-ratchet-audit.sh`:

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# INV-4 (open half): fail if any doc-ratchet.yaml cited bead is closed.
# Run locally / pre-bd-close; NOT in CI (bd not guaranteed there).
set -euo pipefail
reg="api/proto/doc-ratchet.yaml"
beads=$(rg -o 'holomush-[a-z0-9.]+' "$reg" | sort -u)
rc=0
for b in $beads; do
  # bd show --json returns an ARRAY; index [0] (matches scripts/quarantine-audit.sh:24).
  status=$(bd show "$b" --json 2>/dev/null | jq -r '.[0].status // "MISSING"')
  if [ "$status" = "closed" ] || [ "$status" = "MISSING" ]; then
    echo "ERROR: doc-ratchet bead $b is $status (must be open)" >&2
    rc=1
  fi
done
[ "$rc" -eq 0 ] && echo "✓ all doc-ratchet beads open"
exit "$rc"
```

Add the task to `Taskfile.yaml` (near `quarantine:audit`, ~line 230):

```yaml
  proto-ratchet:audit:
    desc: Fail if any doc-ratchet.yaml bead is closed (needs bd; run locally / pre-bd-close). INV-4.
    cmds:
      - bash scripts/proto-ratchet-audit.sh
```

Run: `task proto-ratchet:audit`
Expected: `✓ all doc-ratchet beads open`.

- [ ] **Step 5: Add INV-binding assertion**

Append to `test/meta/proto_doc_ratchet_test.go` a test asserting each numbered invariant has an enforcing test by name:

```go
func TestProtoDocInvariantsHaveTests(t *testing.T) {
	// INV-1..INV-5 each map to a named test. This guards the spec's
	// "every invariant has an enforcing test" discipline.
	required := []string{
		"TestBufYAMLEnablesComments",       // INV-1
		"TestProtoCommentsNoNameEcho",      // INV-3 (INV-2 = buf lint in CI)
		"TestProtoDocRatchetBijection",     // INV-4
		"TestLintProtoRunsNameEcho",        // INV-5
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
```

- [ ] **Step 6: Run and commit**

Run: `task test -- ./test/meta/` and `task proto-ratchet:audit`
Expected: both PASS.

```bash
jj describe -m "test(proto): doc-ratchet registry bijection + open-bead audit (INV-4)"
jj new
```

---

### Task 6: Authoring guidance rule + contributor doc

**Files:**

- Create: `.claude/rules/proto-doc-comments.md`
- Modify: `CLAUDE.md` (Code Conventions section)
- Create: `site/src/content/docs/contributing/proto-doc-comments.md`

- [ ] **Step 1: Create the auto-loading rule**

Create `.claude/rules/proto-doc-comments.md`:

```markdown
---
paths:
  - "api/proto/**/*.proto"
---

# Proto Doc-Comment Conventions

Every message, field, RPC, service, enum, and enum value MUST carry a leading
doc comment. Enforced by buf's `COMMENTS` lint category (ratcheted per-proto in
`buf.yaml` `lint.ignore_only.COMMENTS`) plus a name-echo quality gate
(`test/meta/proto_doc_comments_test.go`, run by `task lint:proto`).

## What a good comment says

- The element's **purpose**, contract, units, invariants, and failure modes.
- NEVER a restatement of the name. `// CreateSceneRequest` over
  `message CreateSceneRequest` is rejected by the name-echo gate.

## Ground every comment in the Go handler

Before writing a comment, find the implementing handler (probe the RPC/message
name) and describe what it actually does. Do NOT invent behavior from the field
name. Handler locations: core→`internal/grpc`, world→`internal/world`,
scene→`plugins/core-scenes`, web→`internal/web`, control→`internal/control`,
admin→`internal/admin`, content→`internal/grpc/content_service.go`,
plugin/attribute→`plugins/core-scenes/resolver.go`,
plugin/audit→`plugins/core-scenes/audit.go`, hostfunc→`internal/plugin`.

## Proto ↔ handler mismatch protocol

If the proto and its handler disagree (ignored field, unimplemented RPC,
overridden default), file `bd create -t bug` capturing the mismatch and document
the CURRENT behavior. Do NOT change the schema as part of SP0.

## Ratchet workflow

1. Document the proto fully.
2. Remove its line from `buf.yaml` `lint.ignore_only.COMMENTS` AND its
   `api/proto/doc-ratchet.yaml` entry (the bijection test enforces both).
3. Close the proto's authoring bead.
4. Confirm `task lint:proto` is green.
```

- [ ] **Step 2: Add the CLAUDE.md pointer**

In `CLAUDE.md` under "Code Conventions", add a subsection pointer:

```markdown
### Proto Doc Comments

Every proto element needs a Go-grounded leading comment; no name-echo. Enforced
by buf `COMMENTS` (ratcheted in `buf.yaml`) + name-echo gate. Full guide:
`.claude/rules/proto-doc-comments.md` (auto-loads on `api/proto/**`).
```

- [ ] **Step 3: Create the contributor doc**

Create `site/src/content/docs/contributing/proto-doc-comments.md` with frontmatter (`title: "Proto Doc Comments"`) restating the convention, the Go-grounding requirement, the ratchet workflow, and the mismatch protocol. Link it from the contributing index.

- [ ] **Step 4: Lint docs**

Run: `task lint:markdown` (and `rumdl check site/src/content/docs/contributing/proto-doc-comments.md`)
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj describe -m "docs(proto): authoring guidance rule + contributor doc"
jj new
```

---

## Phase 2 — Authoring (one proto per task)

### Authoring procedure (applies to every Phase 2 task)

For each proto:

1. **Ground:** probe the handler package named in the task; read the impl to learn real semantics, units, invariants, defaults, and failure modes.
2. **Author:** add a substantive leading comment to every message, field, RPC, service, enum, enum value, and oneof. No name-echoes.
3. **Mismatch beads:** for any proto↔handler disagreement, `bd create -t bug` and document current behavior.
4. **Un-ratchet:** delete the proto's line from `buf.yaml` `lint.ignore_only.COMMENTS` AND its `api/proto/doc-ratchet.yaml` entry.
5. **Verify:** `task lint:proto` green (now enforces `COMMENT_*` + name-echo on this proto); `task test -- ./test/meta/` green (bijection still holds).
6. **Commit & close bead.**

Large protos (`core`, `plugin`, `web`, `scene`) MAY be split into per-service commits within the one task/bead.

**Model labels:** Phase 2 tasks are mechanical (read handler → write comments → un-ratchet → verify). `plan-to-beads` SHOULD tag Tasks 7-18 `model:sonnet` (the large protos — `core`, `plugin`, `web`, `scene` — MAY use `model:opus` for grounding-judgment density). Phase 1 infra tasks (1, 5) involve real Go/test authoring and SHOULD be `model:opus`.

### Task 7: Document `world.proto`

**Files:** Modify `api/proto/holomush/world/v1/world.proto`; grounding `internal/world/`. Inventory: 11 msgs, 4 rpcs.

- [ ] Follow the authoring procedure. Acceptance: `world.proto` removed from both ratchets; `task lint:proto` + `task test -- ./test/meta/` green; `task pr-prep` green.

### Task 8: Document `control.proto`

**Files:** Modify `api/proto/holomush/control/v1/control.proto`; grounding `internal/control/grpc_server.go`. Inventory: 4 msgs, 2 rpcs.

- [ ] Follow the authoring procedure. Acceptance as Task 7 for `control.proto`.

### Task 9: Document `plugin/attribute.proto`

**Files:** Modify `api/proto/holomush/plugin/v1/attribute.proto`; grounding `plugins/core-scenes/resolver.go`, `internal/plugin/pluginauthz/`. Inventory: 7 msgs, 2 rpcs, 1 enum.

- [ ] Follow the authoring procedure. Acceptance as Task 7 for `attribute.proto`.

### Task 10: Document `plugin/audit.proto`

**Files:** Modify `api/proto/holomush/plugin/v1/audit.proto`; grounding `plugins/core-scenes/audit.go`, `internal/eventbus/audit/`. Inventory: 8 msgs, 2 rpcs.

- [ ] Follow the authoring procedure. Acceptance as Task 7 for `audit.proto`.

### Task 11: Document `admin/read_stream.proto`

**Files:** Modify `api/proto/holomush/admin/v1/read_stream.proto`; grounding `internal/admin/socket/read_stream_handler.go`. Inventory: 6 msgs, 1 enum.

- [ ] Follow the authoring procedure. Acceptance as Task 7 for `read_stream.proto`.

### Task 12: Document `admin/rekey.proto`

**Files:** Modify `api/proto/holomush/admin/v1/rekey.proto`; grounding `internal/admin/`, `internal/eventbus/crypto/dek/`. Inventory: 14 msgs.

- [ ] Follow the authoring procedure. Acceptance as Task 7 for `rekey.proto`.

### Task 13: Document `admin/admin.proto`

**Files:** Modify `api/proto/holomush/admin/v1/admin.proto`; grounding `internal/admin/socket/` (`status_handler.go`, `rekey_handler.go`, `handlers.go`). Inventory: 8 msgs, 10 rpcs.

- [ ] Follow the authoring procedure. Acceptance as Task 7 for `admin.proto`.

### Task 14: Document `plugin/hostfunc.proto`

**Files:** Modify `api/proto/holomush/plugin/v1/hostfunc.proto`; grounding `internal/plugin/goplugin/host.go`, `internal/plugin/pluginauthz/`. Inventory: 28 msgs, 12 rpcs, 1 enum.

- [ ] Follow the authoring procedure. Acceptance as Task 7 for `hostfunc.proto`.

### Task 15: Document `core.proto`

**Files:** Modify `api/proto/holomush/core/v1/core.proto`; grounding `internal/grpc/`. Inventory: 48 msgs, 20 rpcs, 5 enums. MAY split by service/section.

- [ ] Follow the authoring procedure. Acceptance as Task 7 for `core.proto`.

### Task 16: Document `web.proto`

**Files:** Modify `api/proto/holomush/web/v1/web.proto`; grounding `internal/web/`. Inventory: 50 msgs, 22 rpcs, 4 enums. MAY split by handler group.

- [ ] Follow the authoring procedure. Acceptance as Task 7 for `web.proto`.

### Task 17: Document `plugin/plugin.proto`

**Files:** Modify `api/proto/holomush/plugin/v1/plugin.proto`; grounding `internal/plugin/`. Inventory: 50 msgs, 22 rpcs, 5 enums. MAY split by service.

- [ ] Follow the authoring procedure. Acceptance as Task 7 for `plugin.proto`.

### Task 18: Document `scene/v1/scene.proto`

**Files:** Modify `api/proto/holomush/scene/v1/scene.proto`; grounding `plugins/core-scenes/`. Inventory: 58 msgs, 23 rpcs (largest). MAY split by service.

- [ ] Follow the authoring procedure. Acceptance as Task 7 for `scene.proto`.

---

## Phase 3 — Close-out

### Task 19: Delete the ratchet & close the stub

**Files:** Modify `buf.yaml`; delete `api/proto/doc-ratchet.yaml`; delete `test/meta/proto_doc_ratchet_test.go`; delete `scripts/proto-ratchet-audit.sh` + the `proto-ratchet:audit` task in `Taskfile.yaml`.

- [ ] **Step 1: Confirm the ratchet is empty**

The `COMMENTS:` block in `buf.yaml` should have no entries and `doc-ratchet.yaml` no `pending` entries (all removed during Phase 2).

- [ ] **Step 2: Delete the now-empty ratchet block and registry**

Remove the `COMMENTS:` key from `buf.yaml` `lint.ignore_only` (keep `- COMMENTS` under `lint.use`). Delete `api/proto/doc-ratchet.yaml`, `test/meta/proto_doc_ratchet_test.go`, `scripts/proto-ratchet-audit.sh`, and the `proto-ratchet:audit` task in `Taskfile.yaml`. Remove the `TestProtoDocRatchetBijection` entry from `TestProtoDocInvariantsHaveTests`'s required list (INV-1/3/5 stay; INV-4's ratchet no longer exists).

- [ ] **Step 3: Verify unconditional enforcement**

Run: `task lint:proto && task test -- ./test/meta/`
Expected: PASS — every proto now enforced by `COMMENT_*` + name-echo with no exemptions.

- [ ] **Step 4: Run the full gate**

Run: `task pr-prep`
Expected: `✓ All PR checks passed.`

- [ ] **Step 5: Close the stub**

```bash
bd close holomush-ay6xm --reason="Superseded by SP0 epic holomush-300ad (proto doc comments + COMMENTS ratchet landed)"
```

- [ ] **Step 6: Commit**

```bash
jj describe -m "build(buf): remove SP0 doc-comment ratchet — full COMMENTS enforcement"
jj new
```

---

## Self-Review Notes

- **Spec coverage:** Component 1 (ratchet) → Tasks 4, 19. Component 2 (name-echo) → Task 1. Component 3 (guidance) → Task 6. Component 4 (registry bijection + open-bead audit) → Task 5. INV-1→Task 4; INV-2→buf lint in CI; INV-3→Task 1; INV-4→Task 5 (bijection test + audit script); INV-5→Task 1. Docs deliverable → Task 6. Loop-proof protos → Tasks 2-3. Authoring all 14 → Tasks 2-3 + 7-18. Non-goal (no docs:proto change) respected.
- **Design-review findings folded in:** (1) `--as-file-descriptor-set` used in Task 1 Step 5; (2) lint-wiring shape specified in Task 1 Step 7; (3) bead-ID materialization ordering called out in Task 5's ordering note.
- **Plan-review round-1 findings folded in:** (crit) Task 5 now requires real bead IDs with honest expected-FAIL-on-placeholder wording; (1) `TestBufYAMLEnablesComments` moved out of Task 1's `lint:proto` wiring into Task 4 so every Task 1 commit stays green; (2) INV-4 open-bead check split into `scripts/proto-ratchet-audit.sh` / `task proto-ratchet:audit` per the `quarantine:audit` precedent; (minor) `TestLintProtoRunsNameEcho` scoped via `taskBlock`; Task 13 path → `internal/admin/socket/`; Phase 2 model labels noted.
- **Ordering invariant:** every commit keeps `task lint:proto` and `task test -- ./test/meta/` green (eventbus/content documented before COMMENTS is enabled; ratchet and registry stay in bijection).

<!-- adr-capture: sha256=88b77f515106593e; ts=2026-05-28T20:01:13Z; adrs= -->
