-- SPDX-License-Identifier: Apache-2.0
-- Crypto rekey checkpoint table per
-- docs/superpowers/specs/2026-05-10-event-payload-crypto-phase5-sub-epic-e-design.md §3.1.

CREATE TABLE crypto_rekey_checkpoints (
    request_id              bytea       PRIMARY KEY,
    context_type            text        NOT NULL,
    context_id              text        NOT NULL,
    op_args_hash            bytea       NOT NULL,
    policy_hash             bytea       NOT NULL,
    primary_player_id       text        NOT NULL,
    status                  text        NOT NULL,
    last_processed_event_id bytea,
    new_dek_id              bigint      REFERENCES crypto_keys(id),
    old_dek_id              bigint      NOT NULL REFERENCES crypto_keys(id),
    phase5_attempt_count    int         NOT NULL DEFAULT 0,
    phase5_missing_members  jsonb,
    force_destroy           boolean     NOT NULL DEFAULT false,
    started_at              timestamptz NOT NULL DEFAULT now(),
    last_heartbeat_at       timestamptz NOT NULL DEFAULT now(),
    completed_at            timestamptz,
    aborted_at              timestamptz,
    aborted_reason          text,
    CONSTRAINT crypto_rekey_checkpoints_terminal_consistency CHECK (
        (status NOT IN ('complete', 'aborted')) OR
        (status = 'complete' AND completed_at IS NOT NULL AND aborted_at IS NULL) OR
        (status = 'aborted' AND aborted_at IS NOT NULL AND aborted_reason IS NOT NULL AND completed_at IS NULL)
    )
);

CREATE UNIQUE INDEX crypto_rekey_checkpoints_one_active_per_context
    ON crypto_rekey_checkpoints (context_type, context_id)
    WHERE status NOT IN ('complete', 'aborted');

CREATE INDEX crypto_rekey_checkpoints_status_idx
    ON crypto_rekey_checkpoints (status, last_heartbeat_at)
    WHERE status NOT IN ('complete', 'aborted');

CREATE INDEX crypto_rekey_checkpoints_primary_player_idx
    ON crypto_rekey_checkpoints (primary_player_id, started_at DESC);
