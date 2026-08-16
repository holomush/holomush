// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package section

import (
	"context"
	"log/slog"

	"github.com/samber/oops"
)

// ValidateAtBoot is the STARTUP STEP that carries 01-SPEC §10.2's "compile time
// or at boot" requirement (D-09). It is called from
// setup.BootstrapSubsystem.Prepare, and a non-nil return ABORTS startup.
//
// # The call site is the point, not the validator
//
// A Validate() that ships without a production call site satisfies D-09 in a
// unit test and nothing else: a zero-valued descriptor reaches production and is
// discovered by the first unauthorized caller — the wrong party to learn it
// from, and by then the section has already been served ungated.
//
// # Posture: validate the whole set, name the offender, abort
//
// This mirrors policy.Bootstrap, whose failures are likewise fatal (ADR #92). It
// does NOT log-and-continue: a malformed registry entry is either an ungated
// admin surface or an unservable one, and both are programming errors that
// should stop the boot rather than degrade it.
//
// It runs BEFORE any seeding because it needs no database and no prior step — a
// programming error should abort before the boot does any work it would have to
// undo.
func ValidateAtBoot(ctx context.Context) error { return validateAtBoot(ctx, all) }

// validateAtBoot is the step body with the entry set injected, so the abort
// path can be driven with a registry shape [validateEntries] refuses to let the
// shipped set have.
func validateAtBoot(ctx context.Context, entries []Section) error {
	if err := validateEntries(entries); err != nil {
		return oops.Code("ADMIN_SECTION_REGISTRY_INVALID").
			Errorf("admin section registry is invalid: %w", err)
	}
	// The METHOD table is validated by the same startup step, for the same
	// reason: a malformed method declaration is either an ungated admin RPC or
	// one that can never be served, and both are programming errors that should
	// stop the boot rather than be discovered by the first caller they mis-gate.
	if err := validateAdminDescriptors(AdminDescriptors); err != nil {
		return oops.Code("ADMIN_METHOD_DESCRIPTORS_INVALID").
			Errorf("admin method descriptor table is invalid: %w", err)
	}
	slog.DebugContext(ctx, "admin section registry validated",
		"sections", len(entries), "admin_methods", len(AdminDescriptors))
	return nil
}
