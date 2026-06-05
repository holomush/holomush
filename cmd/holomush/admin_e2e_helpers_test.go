// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

// Helpers used by T26 dual-control scenarios in admin_authenticate_e2e_test.go.
// Kept in a separate file from T25's setup so the dual-control surface area is
// readable independently. All methods hang off *adminAuthEnv (the shared
// fixture) because both T25 and T26 scenarios run within the same Ginkgo It
// (single-boot constraint — see the long-form comment in
// admin_authenticate_e2e_test.go::Describe).

import (
	"crypto/rand"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"

	. "github.com/onsi/gomega" //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/admin/approval"
	"github.com/holomush/holomush/internal/pgnanos"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// openApproval opens a fresh pending admin_approvals row with the given
// primary player. The approval is for op_kind "rekey" with a random 32-byte
// op_args_hash; sub-epic D's Approve handler does not validate op_args_hash
// (that's a sub-epic E Rekey-proceed concern), so the hash content is opaque
// to every T26 scenario.
func (e *adminAuthEnv) openApproval(primaryPlayerID ulid.ULID) approval.RequestID {
	hash := make([]byte, 32)
	_, err := rand.Read(hash)
	Expect(err).NotTo(HaveOccurred(), "rand.Read for op_args_hash")
	id, err := e.approvalRepo.Open(e.ctx, approval.OpenRequest{
		PrimaryPlayerID: primaryPlayerID.String(),
		OpKind:          "rekey",
		OpArgsHash:      hash,
	})
	Expect(err).NotTo(HaveOccurred(), "approvalRepo.Open")
	return id
}

// approve calls the AdminService.Approve RPC via UDS.
func (e *adminAuthEnv) approve(sessionToken string, id approval.RequestID) error {
	_, err := e.client.Approve(e.ctx, connect.NewRequest(&adminv1.ApproveRequest{
		SessionToken: sessionToken,
		RequestId:    id[:],
	}))
	return err
}

// approvalRow returns approved_at + approved_by_player_id from the DB for a
// given request_id. Used to assert MarkApproved actually fired (or didn't).
func (e *adminAuthEnv) approvalRow(id approval.RequestID) (approvedAt *time.Time, approvedByPlayerID string) {
	// admin_approvals.approved_at is BIGINT epoch-ns (post-gfo6 Phase 4).
	var approvedAtNs *pgnanos.Time
	err := e.queryPool.QueryRow(
		e.ctx,
		`SELECT approved_at, COALESCE(approved_by_player_id, '')
		   FROM admin_approvals WHERE request_id = $1`,
		id[:],
	).Scan(&approvedAtNs, &approvedByPlayerID)
	Expect(err).NotTo(HaveOccurred(), "SELECT admin_approvals row")
	if approvedAtNs != nil {
		t := approvedAtNs.Time()
		approvedAt = &t
	}
	return approvedAt, approvedByPlayerID
}

// forceExpireApproval directly mutates the row's expires_at into the past so
// the subsequent Approve call sees an expired row. Mirrors the same SQL the
// approval/repo_integration_test uses for INV-CRYPTO-72 coverage.
func (e *adminAuthEnv) forceExpireApproval(id approval.RequestID) {
	tag, err := e.queryPool.Exec(e.ctx,
		`UPDATE admin_approvals SET expires_at = (EXTRACT(EPOCH FROM now() - interval '1 minute') * 1e9)::BIGINT WHERE request_id = $1`,
		id[:])
	Expect(err).NotTo(HaveOccurred(), "force-expire admin_approvals row")
	Expect(tag.RowsAffected()).To(Equal(int64(1)), "force-expire must touch exactly one row")
}

// Note: client-side error inspection is limited to connect.CodeOf(err)
// because ConnectRPC does not transmit the server-side oops error chain
// over the wire — only the connect.Code and message string. The DENY_*
// oops codes (INV-CRYPTO-72, INV-CRYPTO-73, INV-CRYPTO-74, INV-CRYPTO-83) are server-internal
// taxonomy covered by handler-level unit tests (e.g.,
// internal/admin/approval/handler_test.go and internal/admin/auth/handler_test.go).
