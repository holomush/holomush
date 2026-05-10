// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integration

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega" //nolint:revive // gomega convention
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/admin/policy"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// loadChainEntriesViaSQL queries events_audit for the chain subject in
// js_seq order and two-step decodes (proto envelope → JSON payload).
// Mirrors policy.loadChainEntries but is exported here so the E2E spec
// can read raw payloads for direct field assertions (PrevHash IS NULL,
// PolicyHash equality, etc.) without going through VerifyChain.
func loadChainEntriesViaSQL(ctx context.Context, pool *pgxpool.Pool, subject string) []policy.PolicySetPayload {
	rows, err := pool.Query(ctx, `
		SELECT envelope
		  FROM events_audit
		 WHERE subject = $1
		 ORDER BY js_seq ASC
	`, subject)
	Expect(err).NotTo(HaveOccurred(), "events_audit query")
	defer rows.Close()

	var out []policy.PolicySetPayload
	for rows.Next() {
		var envelopeBytes []byte
		Expect(rows.Scan(&envelopeBytes)).To(Succeed(), "scan envelope")
		var ev eventbusv1.Event
		Expect(proto.Unmarshal(envelopeBytes, &ev)).To(Succeed(), "proto.Unmarshal envelope")
		var p policy.PolicySetPayload
		Expect(json.Unmarshal(ev.Payload, &p)).To(Succeed(), "json.Unmarshal payload")
		out = append(out, p)
	}
	Expect(rows.Err()).NotTo(HaveOccurred(), "rows.Err")
	return out
}

// tamperSecondRowEnvelope corrupts the second chain row's stored envelope
// by mutating its JSON payload's policy_snapshot while leaving the stored
// policy_hash unchanged. This produces a row whose recomputed hash no
// longer matches the stored hash → POLICY_CHAIN_HASH_MISMATCH on the
// next verifier pass. Mirrors the tamper pattern in
// internal/admin/policy/verifier_integration_test.go::TestVerifyChainAgainstRealEventsAudit.
func tamperSecondRowEnvelope(ctx context.Context, pool *pgxpool.Pool, subject string) {
	// Read the second row's current envelope and js_seq.
	var envelopeBytes []byte
	var jsSeq int64
	err := pool.QueryRow(ctx, `
		SELECT envelope, js_seq
		  FROM events_audit
		 WHERE subject = $1
		 ORDER BY js_seq ASC
		 OFFSET 1 LIMIT 1
	`, subject).Scan(&envelopeBytes, &jsSeq)
	Expect(err).NotTo(HaveOccurred(), "fetch second-row envelope")

	// Decode envelope, mutate payload, re-encode envelope (preserves
	// envelope-level fields and only corrupts the inner JSON payload).
	var ev eventbusv1.Event
	Expect(proto.Unmarshal(envelopeBytes, &ev)).To(Succeed(), "decode envelope")

	var payload policy.PolicySetPayload
	Expect(json.Unmarshal(ev.Payload, &payload)).To(Succeed(), "decode payload")

	// Corrupt the snapshot — keep stored policy_hash + prev_hash unchanged
	// so the recomputed hash will diverge from the stored one.
	payload.PolicySnapshot = map[string]any{
		"required_op_kinds": []any{"tampered_kind_xyz"},
	}
	body, err := json.Marshal(&payload)
	Expect(err).NotTo(HaveOccurred(), "remarshal tampered payload")

	ev.Payload = body
	corruptEnvelope, err := proto.Marshal(&ev)
	Expect(err).NotTo(HaveOccurred(), "remarshal tampered envelope")

	tag, err := pool.Exec(ctx,
		`UPDATE events_audit SET envelope = $1 WHERE subject = $2 AND js_seq = $3`,
		corruptEnvelope, subject, jsSeq)
	Expect(err).NotTo(HaveOccurred(), "UPDATE events_audit envelope")
	Expect(tag.RowsAffected()).To(Equal(int64(1)),
		"tamper UPDATE must touch exactly one row")
}
