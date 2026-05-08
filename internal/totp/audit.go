// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Audit subject builders + event-type constants. RESERVED for use by
// callers with eventbus access (sub-epic D's OperatorAuthProvider; future
// server-side flows). Sub-epic A does NOT emit — see spec §"Audit events
// emitted" / "Emission ownership and the host-shell-CLI gap".

package totp

import (
	"fmt"
	"time"
)

// Event type constants for TOTP audit events emitted by sub-epic D consumers.
const (
	EventTypeBootstrapCompleted = "crypto.totp_bootstrap_completed"
	EventTypeEnrolled           = "crypto.totp_enrolled"
	EventTypeCleared            = "crypto.totp_cleared"
	EventTypeRecoveryConsumed   = "crypto.totp_recovery_code_consumed"
	EventTypeLocked             = "crypto.totp_locked"
)

// SubjectBootstrapCompleted returns the NATS subject for the bootstrap-consumed audit event.
func SubjectBootstrapCompleted(gameID string) string {
	return fmt.Sprintf("events.%s.system.crypto_totp.bootstrap.completed", gameID)
}

// SubjectEnrolled returns the NATS subject for a player TOTP enrollment audit event.
func SubjectEnrolled(gameID, playerID string) string {
	return fmt.Sprintf("events.%s.system.crypto_totp.%s.enrolled", gameID, playerID)
}

// SubjectCleared returns the NATS subject for a player TOTP enrollment cleared audit event.
func SubjectCleared(gameID, playerID string) string {
	return fmt.Sprintf("events.%s.system.crypto_totp.%s.cleared", gameID, playerID)
}

// SubjectRecoveryConsumed returns the NATS subject for a recovery-code consumed audit event.
func SubjectRecoveryConsumed(gameID, playerID string) string {
	return fmt.Sprintf("events.%s.system.crypto_totp.%s.recovery_consumed", gameID, playerID)
}

// SubjectLocked returns the NATS subject for a player TOTP brute-force lock audit event.
func SubjectLocked(gameID, playerID string) string {
	return fmt.Sprintf("events.%s.system.crypto_totp.%s.locked", gameID, playerID)
}

// ClearReason is the audit-payload reason a TOTP enrollment was cleared.
// Defined here (rather than types.go) so audit payload structs can reference
// it without inducing a chain dependency on T3.
type ClearReason string

// ClearReason values for TOTP enrollment cleared audit events.
const (
	ClearReasonRecoveryCode ClearReason = "recovery_code"
	ClearReasonAdminReset   ClearReason = "admin_reset"
)

// BootstrapCompletedPayload is the JSON payload for the bootstrap-consumed audit event.
// Field names match spec §"Audit events emitted" payload column.
type BootstrapCompletedPayload struct {
	ConsumedAt         time.Time `json:"consumed_at"`
	ConsumedByPlayerID string    `json:"consumed_by_player_id"`
	BootstrapKey       string    `json:"bootstrap_key"`
}

// EnrolledPayload is the JSON payload for the TOTP enrolled audit event.
type EnrolledPayload struct {
	PlayerID            string    `json:"player_id"`
	EnrolledAt          time.Time `json:"enrolled_at"`
	RecoveryCodesIssued int       `json:"recovery_codes_issued"`
}

// ClearedPayload is the JSON payload for the TOTP enrollment cleared audit event.
type ClearedPayload struct {
	PlayerID  string      `json:"player_id"`
	ClearedAt time.Time   `json:"cleared_at"`
	ClearedBy ClearReason `json:"cleared_by"`
}

// RecoveryConsumedPayload is the JSON payload for the recovery-code consumed audit event.
type RecoveryConsumedPayload struct {
	PlayerID       string    `json:"player_id"`
	ConsumedAt     time.Time `json:"consumed_at"`
	RecoveryCodeID string    `json:"recovery_code_id"`
}

// LockedPayload is the JSON payload for the TOTP brute-force lock audit event.
type LockedPayload struct {
	PlayerID    string    `json:"player_id"`
	LockedAt    time.Time `json:"locked_at"`
	LockedUntil time.Time `json:"locked_until"`
	Reason      string    `json:"reason"` // "brute_force_protection"
}
