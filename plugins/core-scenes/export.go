// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"log/slog"
	"regexp"
	"strings"

	"github.com/samber/oops"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/pkg/errutil"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// exportRenderMime maps the two supported export formats to their MIME types.
// The key set is the authoritative supported-format list; any absent format
// yields SCENE_EXPORT_BAD_FORMAT. Mirrors DownloadPublishedScene's MIME
// vocabulary (publishRenderMime) for the two formats we expose here.
var exportRenderMime = map[string]string{
	"markdown": "text/markdown",
	"jsonl":    "application/jsonl",
}

// exportFileExt maps the two supported export formats to file extensions.
var exportFileExt = map[string]string{
	"markdown": ".md",
	"jsonl":    ".jsonl",
}

// slugRe matches runs of characters that are not lowercase letters or digits.
var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a scene title to a URL/filename-safe slug.
// "Tea at the Manor" → "tea-at-the-manor".
func slugify(title string) string {
	s := strings.ToLower(title)
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// ExportSceneLog renders a scene's IC log to a downloadable document for any
// participant (any role including observer). The participant gate is plugin-code
// enforced — non-participants fail before ABAC, which is never consulted here.
// Decryption flows through the host-mediated snapshot decrypt seam
// (DecryptOwnAuditRows; fail-closed if decryptor is unconfigured). Supported
// formats are "markdown" and "jsonl"; any other format yields
// SCENE_EXPORT_BAD_FORMAT. An empty log is not an error — it yields a sentinel
// document (renderMarkdown's empty-entry sentinel line for markdown; zero
// records for jsonl).
func (s *SceneServiceImpl) ExportSceneLog(ctx context.Context, req *scenev1.ExportSceneLogRequest) (*scenev1.ExportSceneLogResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.export_scene_log",
		attribute.String("scene_id", req.GetSceneId()),
		attribute.String("character_id", req.GetCharacterId()),
		attribute.String("format", req.GetFormat()))
	defer span.End()

	// 1. Format validation — resource-independent client error, runs first so
	// it leaks nothing about scene existence or participation.
	mime, ok := exportRenderMime[req.GetFormat()]
	if !ok {
		fmtErr := oops.Code("SCENE_EXPORT_BAD_FORMAT").
			With("format", req.GetFormat()).Errorf("unsupported export format")
		recordError(span, fmtErr)
		return nil, mapStoreErr(ctx, fmtErr)
	}

	// 2. Participant gate (CODE ONLY — no ABAC consulted): any participant
	// row (owner, member, or observer) passes. A missing row yields
	// SCENE_EXPORT_NOT_PARTICIPANT, which mapStoreErr maps to PermissionDenied.
	_, err := s.store.GetParticipant(ctx, req.GetSceneId(), req.GetCharacterId())
	if err != nil {
		if oe, ok2 := oops.AsOops(err); ok2 {
			if code, isStr := oe.Code().(string); isStr && code == "SCENE_PARTICIPANT_NOT_FOUND" {
				notPartErr := oops.Code("SCENE_EXPORT_NOT_PARTICIPANT").
					With("scene_id", req.GetSceneId()).
					With("character_id", req.GetCharacterId()).
					Errorf("not a participant of this scene")
				recordError(span, notPartErr)
				return nil, mapStoreErr(ctx, notPartErr)
			}
		}
		recordError(span, err)
		errutil.LogErrorContext(ctx, "scene.service.export_scene_log get participant failed", err)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status is the wire contract; opaque Internal per grpc-errors.md
	}

	// 3. Read the scene row for the title (used for filename slug).
	scene, err := s.store.Get(ctx, req.GetSceneId())
	if err != nil {
		recordError(span, err)
		errutil.LogErrorContext(ctx, "scene.service.export_scene_log get scene failed", err)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status is the wire contract; opaque Internal per grpc-errors.md
	}

	// 4. Read IC scene_log rows (live read, ORDER BY id ASC).
	// Build the full IC subject the same way publish_scheduler.go does — it is
	// the AAD-canonical subject that the AEAD tag was computed against at encrypt
	// time and that OwnerMap.Resolve requires to locate the DEK.
	fullICSubject := dotStyleSceneSubjectIC(s.gameID, req.GetSceneId())
	logRows, err := s.store.ReadSceneLogForExport(ctx, fullICSubject)
	if err != nil {
		recordError(span, err)
		errutil.LogErrorContext(ctx, "scene.service.export_scene_log read log failed", err)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status is the wire contract; opaque Internal per grpc-errors.md
	}

	// 5. Decrypt in snapshotDecryptBatch chunks via the same snapshotDecryptor
	// seam the publish snapshot uses. Fail closed if decryptor unconfigured.
	entries, decryptErr := s.exportDecryptRows(ctx, req.GetSceneId(), fullICSubject, logRows)
	if decryptErr != nil {
		recordError(span, decryptErr)
		return nil, decryptErr
	}

	// 6. Render via the same renderer functions DownloadPublishedScene uses.
	var content []byte
	switch req.GetFormat() {
	case "markdown":
		content = []byte(renderMarkdown(entries))
	case "jsonl":
		var renderErr error
		content, renderErr = renderJSONL(entries)
		if renderErr != nil {
			recordError(span, renderErr)
			errutil.LogErrorContext(ctx, "scene.service.export_scene_log render failed", renderErr)
			return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status is the wire contract; opaque Internal per grpc-errors.md
		}
	}

	filename := slugify(scene.Title) + exportFileExt[req.GetFormat()]

	slog.InfoContext(ctx, "scene.service.export_scene_log ok",
		"scene_id", req.GetSceneId(),
		"character_id", req.GetCharacterId(),
		"format", req.GetFormat(),
		"entry_count", len(entries),
		"filename", filename)

	return &scenev1.ExportSceneLogResponse{
		Content:  content,
		MimeType: mime,
		Filename: filename,
	}, nil
}

// exportDecryptRows chunks logRows through the snapshotDecryptor seam (same
// seam as the publish snapshot, ≤snapshotDecryptBatch per call) and decodes
// each plaintext payload into a PublishedSceneEntry. Fail-closed when the
// decryptor is unconfigured or returns any error — mirrors decryptAndRender's
// contract but maps failures to gRPC Internal (not PublishFailureReason) since
// export failures are synchronous user-visible errors, not background pipeline
// failures.
//
// fullICSubject is the NATS dot-style IC subject (events.<game_id>.scene.<scene_id>.ic).
// It is the AAD-canonical field: OwnerMap.Resolve uses it to locate the DEK and
// the AEAD tag-check binds to it. Passing "" causes both to fail (Blocker 1).
func (s *SceneServiceImpl) exportDecryptRows(ctx context.Context, sceneID, fullICSubject string, logRows []LogRow) ([]PublishedSceneEntry, error) {
	if len(logRows) == 0 {
		return nil, nil
	}

	if s.decryptor == nil {
		slog.ErrorContext(ctx, "scene.service.export_scene_log decryptor not configured",
			"scene_id", sceneID,
			"code", "SCENE_EXPORT_NO_DECRYPTOR")
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status is the wire contract; opaque Internal per grpc-errors.md
	}

	entries := make([]PublishedSceneEntry, 0, len(logRows))
	for start := 0; start < len(logRows); start += snapshotDecryptBatch {
		end := start + snapshotDecryptBatch
		if end > len(logRows) {
			end = len(logRows)
		}
		chunk := logRows[start:end]

		auditRows := make([]*pluginv1.AuditRow, len(chunk))
		for i := range chunk {
			auditRows[i] = logRowToAuditRow(fullICSubject, chunk[i])
		}

		results, err := s.decryptor.DecryptOwnAuditRows(ctx, auditRows)
		if err != nil {
			errutil.LogErrorContext(ctx, "scene.service.export_scene_log decrypt batch failed", err,
				"scene_id", sceneID, "chunk_start", start, "chunk_len", len(chunk))
			return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status is the wire contract; opaque Internal per grpc-errors.md
		}
		if len(results) != len(chunk) {
			slog.ErrorContext(ctx, "scene.service.export_scene_log decrypt result count mismatch",
				"scene_id", sceneID, "want", len(chunk), "got", len(results))
			return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status is the wire contract; opaque Internal per grpc-errors.md
		}

		for i := range results {
			plaintext, refused := decryptedPlaintext(results[i])
			if refused != "" {
				slog.ErrorContext(ctx, "scene.service.export_scene_log row decrypt refused",
					"scene_id", sceneID, "reason", refused)
				return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status is the wire contract; opaque Internal per grpc-errors.md
			}
			entry, ok, decodeErr := decodeSnapshotEntry(chunk[i].Type, plaintext)
			if decodeErr != nil {
				errutil.LogErrorContext(ctx, "scene.service.export_scene_log entry decode failed", decodeErr,
					"scene_id", sceneID, "event_type", chunk[i].Type)
				return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status is the wire contract; opaque Internal per grpc-errors.md
			}
			if !ok {
				// Unknown type slipped past the SQL filter — should be unreachable.
				slog.ErrorContext(ctx, "scene.service.export_scene_log unknown event type after sql filter",
					"scene_id", sceneID, "event_type", chunk[i].Type)
				return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status is the wire contract; opaque Internal per grpc-errors.md
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}
