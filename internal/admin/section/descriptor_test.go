// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package section

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
)

// TestLookupMethodDescriptorMatchesTheMethodNameExactly pins that the
// method→section table performs NO case folding and NO trimming.
//
// A near-miss MUST resolve to no entry, because a near-miss that resolved to a
// neighbouring entry would authorize one method under another method's section
// and action — which is exactly the silent mis-gate the fail-closed
// ADMIN_SECTION_NOT_DECLARED arm exists to turn into a refusal.
func TestLookupMethodDescriptorMatchesTheMethodNameExactly(t *testing.T) {
	_, found := LookupMethodDescriptor("AdminListSections")
	require.True(t, found, "the shipped method MUST resolve; without it every miss below is vacuous")

	for _, near := range []string{
		"adminlistsections",
		"ADMINLISTSECTIONS",
		"AdminListSections ",
		" AdminListSections",
		"AdminListSection",
		"/holomush.adminportal.v1.AdminPortalService/AdminListSections",
	} {
		t.Run(near, func(t *testing.T) {
			_, ok := LookupMethodDescriptor(near)
			require.False(t, ok, "a near-miss MUST resolve to NO entry, never to a neighbour")
		})
	}
}

// TestTheCharactersSectionResolvesThroughAdminSectionResource pins that a
// descriptor's section id is the same token access.AdminSectionResource derives
// the `admin_section:` reference from — the registry and the method table name
// sections in ONE vocabulary, not two that could drift.
func TestTheCharactersSectionResolvesThroughAdminSectionResource(t *testing.T) {
	entry, found := Lookup("characters")
	require.True(t, found, "the characters section MUST be registered")
	require.Equal(t, access.AdminSectionResource("characters"), entry.Descriptor.Resource)

	// PortalProbeSectionID is a probe, not a scope — but it MUST still be a
	// registered id, or the admission call it feeds would build a reference to
	// a section that does not exist.
	_, probeRegistered := Lookup(string(PortalProbeSectionID))
	require.True(t, probeRegistered, "PortalProbeSectionID MUST name a registered section")
}
