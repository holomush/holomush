// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/pkg/errutil"
)

// parseCallerCharacterID validates the caller_character_id string carried by
// every authenticated Phase 6 request and returns the canonical ULID string.
// Mirrors the inline caller-validation pattern at audit.go:499-524 (the
// ActorKind==CHARACTER check is enforced upstream at the host plugin gateway;
// service handlers receive the ULID string in the request body).
func parseCallerCharacterID(s string) (string, error) {
	if s == "" {
		return "", oops.Code("SCENE_PUBLISH_CALLER_REQUIRED").
			Errorf("caller_character_id is required")
	}
	parsed, err := ulid.ParseStrict(s)
	if err != nil {
		return "", oops.Code("SCENE_PUBLISH_CALLER_MALFORMED").
			With("caller_character_id", s).Wrap(err)
	}
	if parsed == (ulid.ULID{}) {
		return "", oops.Code("SCENE_PUBLISH_CALLER_MALFORMED").
			Errorf("caller_character_id is the zero ULID")
	}
	return parsed.String(), nil
}

// mapStoreErr translates a store-layer error into a gRPC status. Known
// application-error oops codes map to their semantic gRPC code, carrying the
// code as the wire message so clients can discriminate. Any unmapped error
// is funnelled through internalErr — logged with its inner detail and
// returned as a generic Internal — so an unexpected store error is never
// silently swallowed nor leaked past the trust boundary
// (.claude/rules/grpc-errors.md).
func mapStoreErr(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if oe, ok := oops.AsOops(err); ok {
		if code, isStr := oe.Code().(string); isStr {
			switch code {
			case "SCENE_PUBLISH_ALREADY_ACTIVE",
				"SCENE_PUBLISH_ALREADY_PUBLISHED",
				"SCENE_PUBLISH_ATTEMPTS_EXHAUSTED",
				"SCENE_PUBLISH_NO_ELIGIBLE_VOTERS",
				"SCENE_PUBLISH_INVALID_STATE",
				"SCENE_NOT_WATCHABLE":
				return status.Error(codes.FailedPrecondition, code) //nolint:wrapcheck // gRPC status is the wire contract; oops would shadow the code
			// SCENE_PUBLISH_INVALID_TRANSITION is intentionally NOT mapped here:
			// per spec §5.2 it is a defensive "impossible transition" signal
			// (a bug indicator), so it falls through to internalErr → Internal.
			case "SCENE_PUBLISH_NOT_A_VOTER",
				"SCENE_PUBLISH_NOT_OWNER",
				"SCENE_PUBLISH_NOT_PARTICIPANT",
				"SCENE_PRIVACY_BOUNDARY_BLOCK":
				return status.Error(codes.PermissionDenied, code) //nolint:wrapcheck // gRPC status is the wire contract; oops would shadow the code
			case "SCENE_PUBLISH_CALLER_REQUIRED",
				"SCENE_PUBLISH_CALLER_MALFORMED",
				"SCENE_PUBLISH_FORMAT_UNSUPPORTED",
				"SCENE_PUBLISH_REF_INVALID",
				"SCENE_PUBLISH_EXTEND_INVALID",
				"SCENE_PUBLISH_NO_FOCUSED_SCENE":
				return status.Error(codes.InvalidArgument, code) //nolint:wrapcheck // gRPC status is the wire contract; oops would shadow the code
			case "SCENE_PUBLISH_NOT_FOUND", "SCENE_NOT_FOUND":
				return status.Error(codes.NotFound, code) //nolint:wrapcheck // gRPC status is the wire contract; oops would shadow the code
			}
		}
	}
	return internalErr(ctx, err)
}

// internalErr logs the inner error (with trace context from ctx) and returns
// a generic Internal status. The wire-level message is deliberately opaque so
// internals never leak past the trust boundary (.claude/rules/grpc-errors.md).
func internalErr(ctx context.Context, err error) error {
	// errutil.LogErrorContext preserves the oops code + context map of any
	// structured error routed here (e.g. an unmapped SCENE_PUBLISH_* code),
	// which a plain slog call would flatten away.
	errutil.LogErrorContext(ctx, "scene publish internal error", err)
	return status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status is the wire contract; opaque Internal per grpc-errors.md
}

// publicArchiveNotFound is the single opaque NOT_FOUND returned by the PUBLIC
// archive RPCs (GetPublicSceneArchive / DownloadPublicSceneArchive) for every
// non-readable case: a nonexistent id AND any non-PUBLISHED attempt
// (COLLECTING / COOLOFF / ATTEMPT_FAILED). The uniform code+message is
// load-bearing for INV-SCENE-35: a non-participant MUST NOT be able to infer that
// an attempt exists or is in progress from the error shape.
func publicArchiveNotFound() error {
	return status.Error(codes.NotFound, "scene archive not found") //nolint:wrapcheck // gRPC status is the wire contract; uniform opaque NOT_FOUND per INV-SCENE-35
}

// SceneServiceConfig carries Phase 6 game-wide defaults, set at plugin init
// via applyConfig (main.go). Per-scene overrides are read from the scene row
// at StartScenePublish time and take precedence over these defaults. The zero
// value is intentionally invalid — config MUST be applied via applyConfig
// before any publish handler runs (INV-PLUGIN-7).
type SceneServiceConfig struct {
	DefaultVoteWindow    time.Duration
	DefaultCoolOffWindow time.Duration
}

// publishEventer is the seam SceneServiceImpl uses to emit the six Phase 6
// scene_publish_* notice events on state transitions. Phase B handlers call
// these on every transition; the real implementation (publishEventEmitter)
// lands in Phase D (Task D2) and is wired via SetPublishEventer. The
// noopPublishEventer default absorbs every call so Phase B compiles and its
// handler tests pass without touching the event substrate.
type publishEventer interface {
	emitPublishStarted(ctx context.Context, pub *PublishedScene) error
	emitVoteCast(ctx context.Context, attemptID, characterID string, result *CastVoteResult) error
	emitCoolOffStarted(ctx context.Context, attemptID string, window time.Duration) error
	emitResolved(ctx context.Context, attemptID string, finalStatus PublishedSceneStatus, reason *PublishFailureReason, tally *VoteTally) error
	emitWithdrawn(ctx context.Context, attemptID, withdrawnBy string) error
	emitAttemptsExtended(ctx context.Context, sceneID, adminID string, additional, newMax int) error
}

// noopPublishEventer is the SceneServiceImpl default until Phase D wires the
// real emitter; every method is a no-op.
type noopPublishEventer struct{}

func (noopPublishEventer) emitPublishStarted(context.Context, *PublishedScene) error { return nil }
func (noopPublishEventer) emitVoteCast(context.Context, string, string, *CastVoteResult) error {
	return nil
}

func (noopPublishEventer) emitCoolOffStarted(context.Context, string, time.Duration) error {
	return nil
}

func (noopPublishEventer) emitResolved(context.Context, string, PublishedSceneStatus, *PublishFailureReason, *VoteTally) error {
	return nil
}

func (noopPublishEventer) emitWithdrawn(context.Context, string, string) error { return nil }

func (noopPublishEventer) emitAttemptsExtended(context.Context, string, string, int, int) error {
	return nil
}
