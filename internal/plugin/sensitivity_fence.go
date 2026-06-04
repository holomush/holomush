// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import "github.com/samber/oops"

// EnforceSensitivity is the host-side ground-truth check that closes
// INV-PLUGIN-29 (over-claim reject) and INV-PLUGIN-30 (under-claim reject) at emit
// time. Given the manifest-declared sensitivity for an event type
// and the plugin's per-event Sensitive flag, returns the effective
// sensitivity the host MUST use, or a typed error when the
// combination is forbidden.
//
// Truth table:
//
//	manifest=never  + claim=false → effective=never
//	manifest=never  + claim=true  → REJECT (INV-PLUGIN-29, EVENT_SENSITIVITY_NOT_DECLARED)
//	manifest=may    + claim=false → effective=never (plaintext)
//	manifest=may    + claim=true  → effective=always (encrypt)
//	manifest=always + claim=false → REJECT (INV-PLUGIN-30, EVENT_SENSITIVITY_REQUIRED)
//	manifest=always + claim=true  → effective=always
func EnforceSensitivity(manifest Sensitivity, claimed bool) (Sensitivity, error) {
	switch manifest {
	case SensitivityNever:
		if claimed {
			return "", oops.Code("EVENT_SENSITIVITY_NOT_DECLARED").
				With("manifest", string(manifest)).
				Errorf("plugin claimed Sensitive=true on an event the manifest declares plaintext (INV-PLUGIN-29)")
		}
		return SensitivityNever, nil
	case SensitivityMay:
		if claimed {
			return SensitivityAlways, nil
		}
		return SensitivityNever, nil
	case SensitivityAlways:
		if !claimed {
			return "", oops.Code("EVENT_SENSITIVITY_REQUIRED").
				With("manifest", string(manifest)).
				Errorf("plugin claimed Sensitive=false on an event the manifest declares always sensitive (INV-PLUGIN-30)")
		}
		return SensitivityAlways, nil
	}
	return "", oops.Code("SENSITIVITY_INVALID").
		With("manifest", string(manifest)).
		Errorf("manifest sensitivity is not a known value")
}
