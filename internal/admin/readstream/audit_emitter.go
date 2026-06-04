// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package readstream

import (
	"context"
	"encoding/json"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
)

// CompletedAuditFailuresTotal counts completion-audit publish failures
// (INV-CRYPTO-60). Exported so tests can read via testutil.ToFloat64.
//
// Registered via promauto (process-global DefaultRegisterer) so it survives
// test suite restarts in the same process without duplicate-registration panics.
var CompletedAuditFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "holomush_admin_readstream_completed_audit_failures_total",
	Help: "Total failures emitting the crypto.system.operator_read_completed audit event (INV-CRYPTO-60)",
})

const (
	operatorReadEventType          = "crypto.system.operator_read"
	operatorReadCompletedEventType = "crypto.system.operator_read_completed"
)

// OperatorReadAuditEmitter emits hash-chained audit events for operator read
// sessions. Mirrors internal/eventbus/crypto/dek/audit.go::RekeyAuditEmitter.
type OperatorReadAuditEmitter interface {
	// EmitStart canonicalizes the start payload, computes prev_hash for the
	// scope (genesis: nil), stamps prev_hash + self_hash into the payload,
	// and publishes to events.<game>.system.operator_read.<request_id>.
	// Returns an error; callers MUST NOT stream data if EmitStart fails (INV-CRYPTO-53).
	EmitStart(ctx context.Context, payload OperatorReadStartPayload, requestID ulid.ULID) error

	// EmitCompleted does the same for the completed event. The chain emitter
	// loads existing entries by scope (returns the start event), and its
	// recomputed self_hash becomes prev_hash. Returns nil on publish success.
	// On failure: increments INV-CRYPTO-60 metric and returns wrapped error.
	EmitCompleted(ctx context.Context, payload OperatorReadCompletedPayload, requestID ulid.ULID) error
}

// operatorReadAuditEmitter is the concrete implementation.
type operatorReadAuditEmitter struct {
	ce  chain.Emitter
	pub eventbus.Publisher
	h   chain.Handler
}

// NewOperatorReadAuditEmitter constructs an OperatorReadAuditEmitter.
// ce provides prev_hash computation from events_audit.
// pub is the eventbus publish surface.
// h is OperatorReadHandlerFor(gameID) — must be pre-constructed.
func NewOperatorReadAuditEmitter(ce chain.Emitter, pub eventbus.Publisher, h chain.Handler) OperatorReadAuditEmitter {
	return &operatorReadAuditEmitter{ce: ce, pub: pub, h: h}
}

// EmitStart implements OperatorReadAuditEmitter.
func (e *operatorReadAuditEmitter) EmitStart(ctx context.Context, payload OperatorReadStartPayload, requestID ulid.ULID) error {
	scope := requestID.String()

	// Step 1: compute prev_hash from the current chain head (genesis → nil).
	prevHashBytes, _, err := e.ce.ComputePrevHashFor(ctx, e.h, scope)
	if err != nil {
		return oops.Code("OPERATOR_READ_AUDIT_PREV_HASH_FAILED").Wrap(err)
	}

	// Step 2: stamp prev_hash; zero self_hash before computing it.
	payload.PrevHash = encodeHashPtr(prevHashBytes)
	payload.SelfHash = "" // zeroed before self-hash computation

	// Step 3: marshal → map[string]any → RecomputeSelfHash → set SelfHash.
	raw, err := json.Marshal(&payload)
	if err != nil {
		return oops.Code("OPERATOR_READ_AUDIT_MARSHAL_FAILED").Wrap(err)
	}
	var m map[string]any
	if uerr := json.Unmarshal(raw, &m); uerr != nil {
		return oops.Code("OPERATOR_READ_AUDIT_UNMARSHAL_FAILED").Wrap(uerr)
	}
	selfHashBytes, herr := chain.RecomputeSelfHash(m, e.h.Chain.SelfHashField)
	if herr != nil {
		return oops.Code("OPERATOR_READ_AUDIT_SELF_HASH_FAILED").Wrap(herr)
	}
	payload.SelfHash = encodeHash(selfHashBytes)

	// Step 4: re-marshal with SelfHash populated.
	raw, err = json.Marshal(&payload)
	if err != nil {
		return oops.Code("OPERATOR_READ_AUDIT_REMARSHAL_FAILED").Wrap(err)
	}

	// Step 5: build event and publish.
	subject := eventbus.Subject(e.h.SubjectFor(scope))
	ev := eventbus.NewEvent(subject, operatorReadEventType, eventbus.Actor{Kind: eventbus.ActorKindSystem}, raw)
	if perr := e.pub.Publish(ctx, ev); perr != nil {
		return oops.Code("OPERATOR_READ_AUDIT_PUBLISH_FAILED").
			With("subject", subject).Wrap(perr)
	}
	return nil
}

// EmitCompleted implements OperatorReadAuditEmitter.
//
// INV-CRYPTO-59: prev_hash MUST equal the recomputed self_hash of the start event.
// Returns a wrapped error on publish/chain failure; the caller MUST log and
// discard the error per INV-CRYPTO-60 (see handler.go::handleInternal step 10).
// CompletedAuditFailuresTotal is incremented inside this method on every
// failure path before returning.
func (e *operatorReadAuditEmitter) EmitCompleted(ctx context.Context, payload OperatorReadCompletedPayload, requestID ulid.ULID) error {
	scope := requestID.String()

	// Step 1: compute prev_hash — LoadEntriesByScope returns the start event as
	// the only existing entry; its self_hash becomes completed's prev_hash.
	prevHashBytes, _, err := e.ce.ComputePrevHashFor(ctx, e.h, scope)
	if err != nil {
		CompletedAuditFailuresTotal.Inc()
		return oops.Code("OPERATOR_READ_AUDIT_CHAIN_FAILED").Wrap(err)
	}
	if prevHashBytes == nil {
		// No preceding start event — completed without a start is a bug.
		CompletedAuditFailuresTotal.Inc()
		return oops.Code("OPERATOR_READ_AUDIT_COMPLETED_NO_START").
			With("scope", scope).
			Errorf("no preceding start event found for completed audit")
	}

	// Step 2: stamp prev_hash; zero self_hash.
	payload.PrevHash = encodeHash(prevHashBytes)
	payload.SelfHash = ""

	// Step 3: marshal → map[string]any → RecomputeSelfHash.
	raw, err := json.Marshal(&payload)
	if err != nil {
		CompletedAuditFailuresTotal.Inc()
		return oops.Code("OPERATOR_READ_AUDIT_MARSHAL_FAILED").Wrap(err)
	}
	var m map[string]any
	if uerr := json.Unmarshal(raw, &m); uerr != nil {
		CompletedAuditFailuresTotal.Inc()
		return oops.Code("OPERATOR_READ_AUDIT_UNMARSHAL_FAILED").Wrap(uerr)
	}
	selfHashBytes, herr := chain.RecomputeSelfHash(m, e.h.Chain.SelfHashField)
	if herr != nil {
		CompletedAuditFailuresTotal.Inc()
		return oops.Code("OPERATOR_READ_AUDIT_SELF_HASH_FAILED").Wrap(herr)
	}
	payload.SelfHash = encodeHash(selfHashBytes)

	// Step 4: re-marshal.
	raw, err = json.Marshal(&payload)
	if err != nil {
		CompletedAuditFailuresTotal.Inc()
		return oops.Code("OPERATOR_READ_AUDIT_REMARSHAL_FAILED").Wrap(err)
	}

	// Step 5: publish.
	subject := eventbus.Subject(e.h.SubjectFor(scope))
	ev := eventbus.NewEvent(subject, operatorReadCompletedEventType, eventbus.Actor{Kind: eventbus.ActorKindSystem}, raw)
	if perr := e.pub.Publish(ctx, ev); perr != nil {
		CompletedAuditFailuresTotal.Inc()
		return oops.Code("OPERATOR_READ_AUDIT_PUBLISH_FAILED").
			With("subject", subject).Wrap(perr)
	}
	return nil
}
