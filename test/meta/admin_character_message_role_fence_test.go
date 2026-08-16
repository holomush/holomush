// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	adminportalv1 "github.com/holomush/holomush/pkg/proto/holomush/adminportal/v1"
)

// 01-SPEC §10.6 names this test explicitly as the DURABLE verification that role
// mutation stays out of the admin character surface: "the durable verification
// is schema-level: a meta-test that fails if the admin character message ever
// gains a field whose name matches role|grant|permission|capability, paired with
// an allowlist test asserting set equality against the checked-in list".
//
// # Why it walks DESCRIPTORS and not source
//
// The risk §10.6 names is a FUTURE field on a GENERATED message. A grep over the
// hand-written handler in internal/grpc/admin_characters_write.go cannot see one:
// adding `repeated string roles = 90;` to AdminCharacter in the .proto and
// running `task proto` produces a fully-wired accessor with no edit to any Go
// file this repository hand-writes. Only the descriptor sees it.
//
// The paired allowlist test is TestAdminProfileMaskAllowlistMatchesSpec, in
// internal/grpc — it lives there because it reads an unexported map.
//
// adminRoleBearingFieldName is the §10.6 vocabulary, case-insensitive so
// `Roles`, `roleIds` and `RoleGrant` are all caught.
var adminRoleBearingFieldName = regexp.MustCompile(`(?i)(role|grant|permission|capability)`)

// adminCharacterMessages is every message on the admin CHARACTER surface whose
// shape a role field could be smuggled onto: the write request, the projection
// both the list and the detail embed, and each write response.
//
// It is spelled as a literal list rather than derived from the service
// descriptor deliberately: a derived list would silently shrink if a message
// were renamed, and a shrinking fence passes.
func adminCharacterMessages() []proto.Message {
	return []proto.Message{
		&adminportalv1.AdminUpdateCharacterRequest{},
		&adminportalv1.AdminCharacter{},
		&adminportalv1.AdminCharacterDetail{},
		&adminportalv1.AdminUpdateCharacterResponse{},
		&adminportalv1.AdminRetireCharacterResponse{},
		&adminportalv1.AdminUnretireCharacterResponse{},
		&adminportalv1.AdminGetCharacterResponse{},
	}
}

// collectDescriptorFieldNames walks a message descriptor and every nested
// message reachable from it, returning every field name it finds.
//
// THE VISITED SET IS NOT OPTIONAL. Proto descriptor graphs may CYCLE — a
// self-referential message, or google.protobuf.Struct / Value / ListValue, which
// reference each other — and an unbounded walk over one HANGS rather than fails.
// A hang is a far worse failure mode than a red: CI reports a timeout with no
// message pointing at anything. Today's enumerated set is scalars plus
// google.protobuf.FieldMask and is acyclic, so this is future-proofing; the
// guard is three lines and the failure it prevents is silent.
func collectDescriptorFieldNames(md protoreflect.MessageDescriptor, visited map[protoreflect.FullName]bool, out *[]string) {
	if visited[md.FullName()] {
		return
	}
	visited[md.FullName()] = true

	fields := md.Fields()
	for i := range fields.Len() {
		f := fields.Get(i)
		*out = append(*out, string(f.Name()))
		if f.Kind() == protoreflect.MessageKind || f.Kind() == protoreflect.GroupKind {
			collectDescriptorFieldNames(f.Message(), visited, out)
		}
	}
}

// TestAdminCharacterMessagesCarryNoRoleBearingField is §10.6's designated
// schema-level fence.
//
// It carries NO `// Verifies:` annotation, deliberately. The property it proves —
// no role-bearing FIELD on the admin character messages — is an elevation-of-
// privilege fence, not INV-PRIVACY-13's prose-and-alt-linkage retention property,
// and annotating it with an invariant it does not assert would be a false-green.
// It is invariant-shaped and probably wants an id of its own; that is recorded in
// 06-05-SUMMARY.md rather than minted here unplanned.
func TestAdminCharacterMessagesCarryNoRoleBearingField(t *testing.T) {
	visited := map[protoreflect.FullName]bool{}
	var names []string
	for _, msg := range adminCharacterMessages() {
		collectDescriptorFieldNames(msg.ProtoReflect().Descriptor(), visited, &names)
	}

	require.NotEmpty(t, names,
		"a reflection walk that collected nothing would pass this fence vacuously")

	for _, name := range names {
		assert.NotRegexp(t, adminRoleBearingFieldName, name,
			"01-SPEC §10.6: no admin character message may carry a role-bearing field; %q matches "+
				"role|grant|permission|capability. Role mutation is out of scope for v0.13 (PORTAL-08, #4899), "+
				"and the field mask is the only thing bounding what an admin may write to a character "+
				"they do not own.", name)
	}
}
