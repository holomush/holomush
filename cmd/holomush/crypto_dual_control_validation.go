// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import "log/slog"

// validateDualControlRequired filters crypto.dual_control_required against
// the set of known op_kinds, dropping unknown entries with a slog.Warn so
// the server starts (lax+warn mode) instead of refusing to boot on a
// configuration typo. Per spec §9.
func validateDualControlRequired(ops []string, logger *slog.Logger) []string {
	known := map[string]struct{}{"rekey": {}, "admin_read_stream": {}}
	valid := make([]string, 0, len(ops))
	for _, op := range ops {
		if _, ok := known[op]; !ok {
			logger.Warn("crypto.dual_control_required references unknown op_kind; ignoring",
				"op_kind", op,
				"known_ops", []string{"rekey", "admin_read_stream"})
			continue
		}
		valid = append(valid, op)
	}
	return valid
}
