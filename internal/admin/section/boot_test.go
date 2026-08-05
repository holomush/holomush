// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package section

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestValidateAtBootAcceptsTheShippedRegistry(t *testing.T) {
	require.NoError(t, ValidateAtBoot(t.Context()),
		"the shipped registry MUST boot; a startup step that always fails is not a gate")
}

// TestTheBootStepAbortsAndNamesTheOffendingEntryOnAZeroValuedDescriptor drives
// the STEP BODY with a substitute slice, because [validateEntries] refuses to
// let a malformed registry exist in the shipped set.
//
// It asserts the operator-facing half D-09 asks for: the boot fails, it carries
// the registry-invalid code, and it NAMES the offending entry — a startup
// abort that does not say which entry is wrong sends the operator reading the
// whole table.
func TestTheBootStepAbortsAndNamesTheOffendingEntryOnAZeroValuedDescriptor(t *testing.T) {
	// Paired positive control on the same call path.
	require.NoError(t, validateAtBoot(t.Context(), All()))

	err := validateAtBoot(t.Context(), []Section{
		{ID: "characters", Status: StatusAvailable, Descriptor: Descriptor{
			Action:   ActionRead,
			Resource: access.AdminSectionResource("characters"),
		}},
		{ID: "moderation", Status: StatusPlanned, Descriptor: Descriptor{}},
	})

	require.Error(t, err, "§10.2: a zero-valued descriptor MUST abort startup, not reach a request path")
	errutil.AssertErrorCode(t, err, "ADMIN_SECTION_DESCRIPTOR_INVALID")
	assert.Contains(t, err.Error(), "moderation",
		"the abort MUST name the offending entry")
	assert.Contains(t, err.Error(), "registry is invalid",
		"the boot step labels the failure as a registry problem so the operator knows what to fix")
}
