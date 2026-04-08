// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// OpsEventKind enumerates the recognised ops event kinds. The dotted naming
// convention is also enforced by the database CHECK constraint on
// scene_ops_events.kind. The Go-side narrow API in recordOpsEventTx prevents
// typos and ad-hoc kinds in handlers.
type OpsEventKind string

const (
	OpsKindMembershipInvite        OpsEventKind = "membership.invite"
	OpsKindMembershipJoin          OpsEventKind = "membership.join"
	OpsKindMembershipLeave         OpsEventKind = "membership.leave"
	OpsKindMembershipKick          OpsEventKind = "membership.kick"
	OpsKindMembershipOwnershipXfer OpsEventKind = "membership.ownership_transferred"
	OpsKindLifecycleCreated        OpsEventKind = "lifecycle.created"
	OpsKindLifecycleEnded          OpsEventKind = "lifecycle.ended"
	OpsKindLifecyclePaused         OpsEventKind = "lifecycle.paused"
	OpsKindLifecycleResumed        OpsEventKind = "lifecycle.resumed"
	OpsKindSettingsUpdated         OpsEventKind = "settings.updated"
)

// IsValid reports whether k is one of the declared OpsEventKind constants.
func (k OpsEventKind) IsValid() bool {
	switch k {
	case OpsKindMembershipInvite, OpsKindMembershipJoin, OpsKindMembershipLeave,
		OpsKindMembershipKick, OpsKindMembershipOwnershipXfer,
		OpsKindLifecycleCreated, OpsKindLifecycleEnded,
		OpsKindLifecyclePaused, OpsKindLifecycleResumed,
		OpsKindSettingsUpdated:
		return true
	}
	return false
}

// recordOpsEventTx inserts a scene_ops_events row inside an existing
// transaction. The kind MUST be one of the OpsEventKind constants — the
// helper rejects unknown kinds with SCENE_OPS_EVENT_INVALID_KIND so typos
// surface as errors instead of silently writing junk.
//
// targetID may be empty for kinds that affect the whole scene (lifecycle.*,
// settings.*); pass "" for those.
//
// payload is marshalled to JSONB. Pass nil or an empty map for kinds that
// don't need extra context.
func recordOpsEventTx(ctx context.Context, tx pgx.Tx, sceneID string, kind OpsEventKind, actorID, targetID string, payload map[string]any) error {
	if !kind.IsValid() {
		return oops.Code("SCENE_OPS_EVENT_INVALID_KIND").
			With("kind", string(kind)).
			Errorf("unknown ops event kind")
	}

	id, err := newOpsEventID()
	if err != nil {
		return oops.Code("SCENE_OPS_EVENT_ID_GEN_FAILED").Wrap(err)
	}

	if payload == nil {
		payload = map[string]any{}
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return oops.Code("SCENE_OPS_EVENT_PAYLOAD_MARSHAL_FAILED").
			With("kind", string(kind)).
			Wrap(err)
	}

	var targetParam any
	if targetID == "" {
		targetParam = nil
	} else {
		targetParam = targetID
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO scene_ops_events (id, scene_id, kind, actor_id, target_id, payload)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		id, sceneID, string(kind), actorID, targetParam, payloadJSON,
	)
	if err != nil {
		return oops.Code("SCENE_OPS_EVENT_INSERT_FAILED").
			With("scene_id", sceneID).
			With("kind", string(kind)).
			Wrap(err)
	}
	return nil
}

// newOpsEventID generates a ULID using crypto/rand. Mirrors newSceneID in
// service.go — math/rand is forbidden everywhere per CLAUDE.md.
func newOpsEventID() (string, error) {
	ms := ulid.Timestamp(time.Now())
	id, err := ulid.New(ms, rand.Reader)
	if err != nil {
		return "", err
	}
	return "ope-" + id.String(), nil
}
