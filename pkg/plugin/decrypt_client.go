// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"

	"github.com/samber/oops"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// SnapshotDecryptor is the interface exposed to binary plugins for host-mediated
// read-back decryption of their OWN sensitive audit rows. The plugin never holds
// a DEK; it passes ciphertext rows (read from its own audit table) to the host,
// which decrypts host-side and returns per-row plaintext or a typed refusal
// (INV-CRYPTO-26, INV-CRYPTO-37).
//
// This is the snapshot direct entry of the read-back design
// (docs/superpowers/specs/2026-05-25-plugin-readback-decrypt-design.md §3.2):
// the plugin already holds the rows from its in-tx SQL read, so it does NOT
// route through PluginAuditService.QueryHistory (no self-loop, INV-CRYPTO-31).
//
// The host enforces a per-call batch cap (maxDecryptBatch=500) and REJECTS an
// over-cap batch with DECRYPT_BATCH_TOO_LARGE; callers MUST chunk larger row
// sets themselves.
type SnapshotDecryptor interface {
	// DecryptOwnAuditRows submits a batch of the plugin's own audit rows for
	// host-mediated decryption. The result slice is per-row, order-preserving,
	// echoing each row's Id for positional correlation (INV-CRYPTO-37). A nil
	// client fails closed (returns an error).
	DecryptOwnAuditRows(ctx context.Context, rows []*pluginv1.AuditRow) ([]*pluginv1.RowResult, error)
}

// SnapshotDecryptorAware is the optional interface service providers implement
// to receive a SnapshotDecryptor during Init, parallel to HostEvaluatorAware,
// FocusClientAware, and EventSinkAware. Implement this on the plugin struct to
// get the host-mediated read-back decryptor injected before Init is called.
type SnapshotDecryptorAware interface {
	SetSnapshotDecryptor(SnapshotDecryptor)
}

// snapshotDecryptClient is the concrete SnapshotDecryptor used by binary
// plugins. It wraps the generated PluginHostServiceClient and forwards
// DecryptOwnAuditRows calls. The host resolves the calling plugin's identity
// from the connection, so no per-dispatch token is ferried here (unlike
// hostEvaluateClient): read-back decrypt is system-initiated from the plugin's
// own lifecycle, not command-gated.
type snapshotDecryptClient struct {
	client pluginv1.PluginHostServiceClient
}

// DecryptOwnAuditRows implements SnapshotDecryptor. A nil client fails closed.
func (c *snapshotDecryptClient) DecryptOwnAuditRows(ctx context.Context, rows []*pluginv1.AuditRow) ([]*pluginv1.RowResult, error) {
	if c.client == nil {
		return nil, oops.New("snapshot decrypt client is not configured")
	}
	// Delegate to the shared transport helper so the no-log / discard-plaintext
	// contract and the gRPC status pass-through live in exactly one place.
	return DecryptOwnAuditRows(ctx, c.client, rows)
}
