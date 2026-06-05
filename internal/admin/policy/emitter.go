// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"time"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
)

// Clock abstracts time.Now for deterministic tests.
type Clock interface {
	Now() time.Time
}

// CryptoEffectiveConfig is the effective configuration that gets snapshotted
// into PolicySetPayload.PolicySnapshot. v1 only carries dual_control_required;
// future config keys land here.
type CryptoEffectiveConfig struct {
	DualControlRequired []string // sorted, deduped
}

// EmitDeps bundles the dependencies EmitCurrentSnapshot requires.
type EmitDeps struct {
	GameID          string
	ServerStartULID string
	ServerIdentity  string
	Pool            *pgxpool.Pool
	Publisher       eventbus.Publisher
	Clock           Clock
	Config          CryptoEffectiveConfig
}

// EmitCurrentSnapshot publishes one crypto.policy_set event for policyName,
// reading the latest events_audit row for the chain subject as
// prev_hash, computing the new policy_hash, and Publishing via deps.Publisher.
//
// Idempotent on no-change: if the latest event's PolicySnapshot canonicalizes
// to the same bytes as the would-be new snapshot, no Publish is issued and
// nil is returned. Otherwise, Publish error is wrapped and returned per
// INV-CRYPTO-84 fail-closed semantics.
func EmitCurrentSnapshot(ctx context.Context, deps EmitDeps, policyName string) error {
	subjectStr := "events." + deps.GameID + ".system.crypto_policy." + policyName

	// Read existing chain to get latest entry (or empty for genesis).
	entries, err := loadChainEntries(ctx, deps.Pool, subjectStr)
	if err != nil {
		return oops.Code("POLICY_EMIT_LOAD_FAILED").
			With("policy_name", policyName).Wrap(err)
	}

	// Compute prev_hash via the generalized auditchain Emitter — keeps the
	// emit-side hash computation byte-equivalent to the verifier's recompute
	// path (chain.Verifier.VerifyScope calls the same Handler.Canonicalize +
	// chain.RecomputeSelfHash). Genesis returns nil prev_hash.
	repo := chain.NewPostgresRepo(deps.Pool)
	em := chain.NewEmitter(repo)
	h := PolicySetHandlerFor(deps.GameID)
	prevHash, _, prevErr := em.ComputePrevHashFor(ctx, h, policyName)
	if prevErr != nil {
		return oops.Code("POLICY_EMIT_HASH_RECOMPUTE_FAILED").
			With("policy_name", policyName).Wrap(prevErr)
	}

	// Build the new snapshot. Unknown policyName is fail-closed: any typo
	// or config drift must stop startup rather than emit an empty snapshot
	// that silently produces a valid chain entry.
	snapshot, err := snapshotFromConfig(deps.Config, policyName)
	if err != nil {
		return err
	}
	newPayload := PolicySetPayload{
		PolicyName:      policyName,
		PolicySnapshot:  snapshot,
		PrevHash:        prevHash,
		ServerStartULID: deps.ServerStartULID,
		ServerIdentity:  deps.ServerIdentity,
		Timestamp:       deps.Clock.Now(),
	}

	// Idempotency check: if the latest entry's snapshot canonicalizes to
	// the same bytes as the new snapshot, skip the publish. Compare the
	// snapshot only (not full payload — Timestamp / ServerStartULID would
	// always differ across runs).
	if len(entries) > 0 {
		latestCanon, canonErr := canonicalizeSnapshot(entries[len(entries)-1].Payload.PolicySnapshot)
		if canonErr != nil {
			return oops.Code("POLICY_EMIT_CANONICALIZE_FAILED").
				With("policy_name", policyName).Wrap(canonErr)
		}
		newCanon, canonErr := canonicalizeSnapshot(snapshot)
		if canonErr != nil {
			return oops.Code("POLICY_EMIT_CANONICALIZE_FAILED").
				With("policy_name", policyName).Wrap(canonErr)
		}
		if bytes.Equal(latestCanon, newCanon) {
			// No change — idempotent skip.
			return nil
		}
	}

	// Compute the policy_hash for the new payload.
	policyHash, err := ComputePolicyHash(&newPayload)
	if err != nil {
		return oops.Code("POLICY_EMIT_HASH_FAILED").
			With("policy_name", policyName).Wrap(err)
	}
	newPayload.PolicyHash = policyHash

	// Marshal to JSON for the Event.Payload (the inner field; the publisher
	// chain wraps in the proto envelope on the wire).
	body, err := json.Marshal(&newPayload)
	if err != nil {
		return oops.Code("POLICY_EMIT_MARSHAL_FAILED").
			With("policy_name", policyName).Wrap(err)
	}

	subj, err := eventbus.NewSubject(subjectStr)
	if err != nil {
		return oops.Code("POLICY_EMIT_INVALID_SUBJECT").
			With("subject", subjectStr).Wrap(err)
	}
	evtType, err := eventbus.NewType("crypto.policy_set")
	if err != nil {
		return oops.Code("POLICY_EMIT_INVALID_TYPE").Wrap(err)
	}

	ev := eventbus.NewEvent(subj, evtType, eventbus.Actor{Kind: eventbus.ActorKindSystem}, body)
	ev.Timestamp = deps.Clock.Now() // honour the injected clock rather than time.Now() inside NewEvent
	if err := deps.Publisher.Publish(ctx, ev); err != nil {
		return oops.Code("POLICY_EMIT_PUBLISH_FAILED").
			With("policy_name", policyName).Wrap(err)
	}
	// Record the chain-init signal so the verifier can later distinguish
	// "first boot, no chain yet" from "chain existed and was truncated".
	// Idempotent (INSERT ... ON CONFLICT DO NOTHING); safe to call on
	// every successful emit. See chain_state.go for the design and the
	// bootstrap_metadata key shape.
	if err := markChainInitialized(ctx, deps.Pool, deps.GameID, policyName); err != nil {
		return oops.Code("POLICY_EMIT_STATE_MARK_FAILED").
			With("policy_name", policyName).Wrap(err)
	}
	return nil
}

// snapshotFromConfig builds the snapshot map for a policy_name. v1 only
// supports "dual_control_required" — future policies land additional cases.
// The slice is sorted+deduped to make canonicalization stable regardless
// of input order. An unsupported policyName is fail-closed: returning an
// error so the caller stops startup (rather than emitting an empty snapshot
// that would silently produce a chain entry).
func snapshotFromConfig(cfg CryptoEffectiveConfig, policyName string) (map[string]any, error) {
	switch policyName {
	case "dual_control_required":
		ops := append([]string(nil), cfg.DualControlRequired...)
		sort.Strings(ops)
		ops = dedupSorted(ops)
		anys := make([]any, len(ops))
		for i, op := range ops {
			anys[i] = op
		}
		return map[string]any{"required_op_kinds": anys}, nil
	default:
		return nil, oops.Code("POLICY_EMIT_UNKNOWN_POLICY").
			With("policy_name", policyName).
			Errorf("unsupported policy_name")
	}
}

func dedupSorted(s []string) []string {
	if len(s) < 2 {
		return s
	}
	out := s[:1]
	for _, v := range s[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

// canonicalizeSnapshot returns the JCS-canonicalized JSON of a snapshot
// map. Used for idempotency comparison.
func canonicalizeSnapshot(snapshot map[string]any) ([]byte, error) {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, oops.Code("POLICY_EMIT_SNAPSHOT_JSON_FAILED").Wrap(err)
	}
	canonical, err := jsoncanonicalizer.Transform(raw)
	if err != nil {
		return nil, oops.Code("POLICY_EMIT_SNAPSHOT_JCS_FAILED").Wrap(err)
	}
	return canonical, nil
}
