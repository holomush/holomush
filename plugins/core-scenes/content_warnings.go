// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"log/slog"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DefaultCWTaxonomy is the built-in OOTB set of content warning tags that
// every HoloMUSH game has available without any configuration. It is used as
// the fallback by effectiveTaxonomy when no game-scope override is stored
// (INV-5: read-time fallback is always safe and never propagates errors).
//
// Game operators can override the full list via the "content.cw_taxonomy"
// game-scope setting (see effectiveTaxonomy).
var DefaultCWTaxonomy = []string{
	"violence",
	"sexual-content",
	"death",
	"substance-use",
	"self-harm",
	"body-horror",
	"abuse",
}

// effectiveTaxonomy returns the active content-warning taxonomy for this game.
// It reads the game-scope "content.cw_taxonomy" setting from the host settings
// service. If the setting is not found, returns an error, or the client is nil,
// DefaultCWTaxonomy is returned unchanged (INV-5: the settings RPC is
// dispatch-token-gated and some RPC paths lack a token; the fallback is the
// correct graceful degradation — never propagate the error).
//
// An unexpected (infrastructure) settings error is logged at WARN before the
// fallback: validation then runs against the default taxonomy instead of the
// operator's custom one, which can let an operator-removed tag pass. Ops need
// that visibility; the read still never fails the caller.
func (s *SceneServiceImpl) effectiveTaxonomy(ctx context.Context) []string {
	if s.settings == nil {
		return DefaultCWTaxonomy
	}
	vals, ok, err := s.settings.GetSetting(ctx, pluginsdk.SettingScopeGame, "", "content.cw_taxonomy")
	if err != nil {
		if isUnexpectedSettingsError(err) {
			slog.WarnContext(
				ctx,
				"scene.service.effective_taxonomy falling back to default taxonomy on settings read error",
				"key", "content.cw_taxonomy",
				"error", err,
			)
		}
		return DefaultCWTaxonomy
	}
	if ok && len(vals) > 0 {
		return vals
	}
	return DefaultCWTaxonomy
}

// isUnexpectedSettingsError reports whether a host-settings GetSetting error is
// an infrastructure failure worth logging, rather than an expected
// auth-denied / not-found / missing-dispatch-token outcome that the CW
// resolution paths intentionally swallow. Expected codes (PermissionDenied,
// Unauthenticated, InvalidArgument, NotFound) are skipped quietly; everything
// else (Internal, Unavailable, Unknown transport failures) is unexpected and
// is surfaced before the safe fallback runs.
func isUnexpectedSettingsError(err error) bool {
	switch status.Code(err) {
	case codes.OK,
		codes.PermissionDenied,
		codes.Unauthenticated,
		codes.InvalidArgument,
		codes.NotFound:
		return false
	default:
		return true
	}
}

// validateContentWarnings checks that every tag in cws appears in the effective
// taxonomy for this game. An empty cws slice is always valid. Returns
// codes.InvalidArgument if any tag is not in the taxonomy.
func (s *SceneServiceImpl) validateContentWarnings(ctx context.Context, cws []string) error {
	if len(cws) == 0 {
		return nil
	}
	taxonomy := s.effectiveTaxonomy(ctx)
	allowed := make(map[string]struct{}, len(taxonomy))
	for _, t := range taxonomy {
		allowed[t] = struct{}{}
	}
	for _, cw := range cws {
		if _, ok := allowed[cw]; !ok {
			return status.Errorf(codes.InvalidArgument, "unknown content warning: %q", cw)
		}
	}
	return nil
}
