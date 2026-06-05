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
	slog.Info("dek", "material", m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log/slog`
}

func leakViaLoggerInfo(m *dek.Material, l *slog.Logger) {
	l.Info("dek", "material", m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log/slog`
}

// Conversion-wrapped bypass: any(m) hides the *dek.Material type behind
// an interface, so a naive pass.TypesInfo.TypeOf(arg) check misses it.
// CodeRabbit finding on PR #3457.
func leakViaAnyConversion(m *dek.Material) {
	slog.Info("dek", "material", any(m)) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log/slog`
}

// One positive per newly added sink symbol so a typo in the sink slice
// (e.g., "DebugContextt" instead of "DebugContext") is caught by tests
// rather than silently disabling the sink. The lookup logic is shared
// across all sinks, but the string keys are not — this is a regression
// barrier for the keys themselves. Per CodeRabbit on PR #3458.

// Free-function *Context variants.

func leakViaSlogInfoContext(ctx context.Context, m *dek.Material) {
	slog.InfoContext(ctx, "dek", "material", m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log/slog`
}

func leakViaSlogDebugContext(ctx context.Context, m *dek.Material) {
	slog.DebugContext(ctx, "dek", "material", m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log/slog`
}

func leakViaSlogWarnContext(ctx context.Context, m *dek.Material) {
	slog.WarnContext(ctx, "dek", "material", m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log/slog`
}

func leakViaSlogErrorContext(ctx context.Context, m *dek.Material) {
	slog.ErrorContext(ctx, "dek", "material", m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log/slog`
}

// Free-function LogAttrs takes (ctx, level, msg, ...Attr); a Material
// wrapped in slog.Any still routes to the same sink check.

func leakViaSlogLogAttrs(ctx context.Context, m *dek.Material) {
	slog.LogAttrs(ctx, slog.LevelInfo, "dek", slog.Any("material", m)) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log/slog`
}

// slog.With bakes Material into a returned logger's attributes; every
// subsequent log call leaks without Material appearing as a direct arg.

func leakViaSlogWith(m *dek.Material) {
	_ = slog.With("dek", "material", m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log/slog`
}

// *Logger method variants — same rationale as the free-function block.

func leakViaLoggerInfoContext(ctx context.Context, m *dek.Material, l *slog.Logger) {
	l.InfoContext(ctx, "dek", "material", m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log/slog`
}

func leakViaLoggerDebugContext(ctx context.Context, m *dek.Material, l *slog.Logger) {
	l.DebugContext(ctx, "dek", "material", m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log/slog`
}

func leakViaLoggerWarnContext(ctx context.Context, m *dek.Material, l *slog.Logger) {
	l.WarnContext(ctx, "dek", "material", m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log/slog`
}

func leakViaLoggerErrorContext(ctx context.Context, m *dek.Material, l *slog.Logger) {
	l.ErrorContext(ctx, "dek", "material", m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log/slog`
}

func leakViaLoggerLogAttrs(ctx context.Context, m *dek.Material, l *slog.Logger) {
	l.LogAttrs(ctx, slog.LevelInfo, "dek", slog.Any("material", m)) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log/slog`
}

func leakViaLoggerWith(m *dek.Material, l *slog.Logger) {
	_ = l.With("dek", "material", m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log/slog`
}
