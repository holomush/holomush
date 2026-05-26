// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import "github.com/holomush/holomush/internal/plugin/pluginauthz"

// AuditorForTest exposes the Functions' internal auditor field for wiring-guard
// tests. This accessor exists so that external test packages (e.g.
// internal/plugin/setup) can verify that hostfunc.WithAuditLogger correctly
// propagates the auditor into Functions without relying on Lua round-trips.
// It MUST NOT be used in production code.
func (f *Functions) AuditorForTest() pluginauthz.Auditor {
	return f.auditor
}
