// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// plugins/ is OUTSIDE the dek package's internal-visibility boundary,
// but lives under github.com/holomush/holomush/ so that go/types'
// internal-package visibility rule lets us import
// internal/eventbus/crypto/dek at all (positive testdata cannot live
// under example.com/ because the typechecker would reject the import
// before the analyzer ever runs — see prior cursorpackageinternal
// precedent).
package positive

import (
	"context"
	"log/slog"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func leakViaSlogInfo(m *dek.Material) {
	slog.Info("dek", "material", m) // want `INV-27: dek.Material MUST NOT be passed to log/slog`
}

func leakViaLoggerInfo(m *dek.Material, l *slog.Logger) {
	l.Info("dek", "material", m) // want `INV-27: dek.Material MUST NOT be passed to log/slog`
}

// Conversion-wrapped bypass: any(m) hides the *dek.Material type behind
// an interface, so a naive pass.TypesInfo.TypeOf(arg) check misses it.
// CodeRabbit finding on PR #3457.
func leakViaAnyConversion(m *dek.Material) {
	slog.Info("dek", "material", any(m)) // want `INV-27: dek.Material MUST NOT be passed to log/slog`
}

// *Context variants take a context.Context but otherwise mirror the
// non-Context sinks. Single canonical example: slog.InfoContext. The
// sink lookup is shared, so one call exercises the lookup for every
// *Context / LogAttrs entry added in holomush-r3vs.
func leakViaSlogInfoContext(ctx context.Context, m *dek.Material) {
	slog.InfoContext(ctx, "dek", "material", m) // want `INV-27: dek.Material MUST NOT be passed to log/slog`
}
