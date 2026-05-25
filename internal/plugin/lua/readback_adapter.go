// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"

	"github.com/samber/oops"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// readbackDecryptorAdapter adapts plugins.ReadbackDecryptor to
// hostfunc.AuditDecryptor (batch), enabling Lua plugins to call
// holomush.decrypt_own_audit_rows via the host's read-back decrypt primitive.
//
// The adapter delegates to the COMMON DecryptOwnRows batch method — the same
// path the binary gRPC handler uses — so Lua inherits the identical
// maxDecryptBatch cap (DECRYPT_BATCH_TOO_LARGE on an over-cap batch) rather than
// silently exceeding it via a private per-row loop (plugin-runtime-symmetry
// invariant). instanceID is empty for Lua plugins: the OwnerMap g1 gate uses
// pluginName only; instanceID is informational in audit only.
type readbackDecryptorAdapter struct {
	d plugins.ReadbackDecryptor
}

// Ensure the adapter satisfies the hostfunc.AuditDecryptor interface.
var _ hostfunc.AuditDecryptor = (*readbackDecryptorAdapter)(nil)

func (a *readbackDecryptorAdapter) DecryptOwnAuditRows(ctx context.Context, pluginName string, rows []*pluginv1.AuditRow) (*pluginv1.DecryptOwnAuditRowsResponse, error) {
	const instanceID = "" // Lua plugins have no gRPC instance ID; informational only
	results, err := a.d.DecryptOwnRows(ctx, pluginName, instanceID, rows)
	if err != nil {
		return nil, oops.With("plugin", pluginName).Wrap(err)
	}
	return &pluginv1.DecryptOwnAuditRowsResponse{Results: results}, nil
}
