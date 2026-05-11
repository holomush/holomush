// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// chainEntry is one decoded events_audit row for the policy_set chain
// subject. Used by the emitter for idempotency comparison (it needs the
// decoded PolicySetPayload, not just the envelope bytes that
// chain.Repo.LoadEntriesByScope returns).
type chainEntry struct {
	Seq     int64
	Payload PolicySetPayload
}

// loadChainEntries reads events_audit rows for the given subject ordered by
// js_seq and two-step decodes each row: proto unmarshal (envelope) → JSON
// unmarshal (PolicySetPayload).
//
// The query is scoped to type='crypto.policy_set' so foreign rows that
// happen to share the subject (e.g., misrouted plugin emits) cannot be
// folded into the policy chain. After decoding, the envelope's Subject
// and Type are re-checked against the expected values: a mismatch
// surfaces as POLICY_CHAIN_ENVELOPE_MISMATCH (defense-in-depth against
// envelope tampering that left the SQL filter satisfied but corrupted
// the encoded subject/type fields).
//
// Post Phase 5 sub-epic E refactor: this is now only used by EmitCurrentSnapshot
// (idempotency check); chain verification goes through
// chain.NewVerifier(chain.NewPostgresRepo(pool)).VerifyScope via [VerifyChain].
func loadChainEntries(ctx context.Context, pool *pgxpool.Pool, subject string) ([]chainEntry, error) {
	rows, err := pool.Query(ctx, `
		SELECT envelope, js_seq
		  FROM events_audit
		 WHERE subject = $1
		   AND type = 'crypto.policy_set'
		 ORDER BY js_seq ASC
	`, subject)
	if err != nil {
		return nil, oops.Code("POLICY_CHAIN_QUERY_FAILED").
			With("subject", subject).Wrap(err)
	}
	defer rows.Close()

	var out []chainEntry
	for rows.Next() {
		var envelopeBytes []byte
		var seq int64
		if err := rows.Scan(&envelopeBytes, &seq); err != nil {
			return nil, oops.Code("POLICY_CHAIN_SCAN_FAILED").
				With("subject", subject).Wrap(err)
		}
		var ev eventbusv1.Event
		if err := proto.Unmarshal(envelopeBytes, &ev); err != nil {
			return nil, oops.Code("POLICY_CHAIN_ENVELOPE_DECODE_FAILED").
				With("js_seq", seq).Wrap(err)
		}
		if ev.Subject != subject || ev.Type != "crypto.policy_set" {
			return nil, oops.Code("POLICY_CHAIN_ENVELOPE_MISMATCH").
				With("subject", subject).
				With("js_seq", seq).
				With("envelope_subject", ev.Subject).
				With("envelope_type", ev.Type).
				Errorf("unexpected envelope subject/type in policy chain row")
		}
		var payload PolicySetPayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			return nil, oops.Code("POLICY_CHAIN_PAYLOAD_DECODE_FAILED").
				With("js_seq", seq).Wrap(err)
		}
		out = append(out, chainEntry{Seq: seq, Payload: payload})
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("POLICY_CHAIN_ROWS_ERR").
			With("subject", subject).Wrap(err)
	}
	return out, nil
}
