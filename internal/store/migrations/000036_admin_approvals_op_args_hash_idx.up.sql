-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Migration 000036: composite index on admin_approvals for GetByOpArgsHash
-- lookup (op_kind, op_args_hash, expires_at). Index-only — no schema change.

CREATE INDEX IF NOT EXISTS admin_approvals_op_kind_args_hash_idx
    ON admin_approvals (op_kind, op_args_hash, expires_at);
