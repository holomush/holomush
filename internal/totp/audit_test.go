// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubjectBuilders(t *testing.T) {
	assert.Equal(t, "events.default.system.crypto_totp.bootstrap.completed",
		SubjectBootstrapCompleted("default"))
	assert.Equal(t, "events.default.system.crypto_totp.01HZ.enrolled",
		SubjectEnrolled("default", "01HZ"))
	assert.Equal(t, "events.default.system.crypto_totp.01HZ.cleared",
		SubjectCleared("default", "01HZ"))
	assert.Equal(t, "events.default.system.crypto_totp.01HZ.recovery_consumed",
		SubjectRecoveryConsumed("default", "01HZ"))
	assert.Equal(t, "events.default.system.crypto_totp.01HZ.locked",
		SubjectLocked("default", "01HZ"))
}

func TestEventTypeConstants(t *testing.T) {
	assert.Equal(t, "crypto.totp_bootstrap_completed", EventTypeBootstrapCompleted)
	assert.Equal(t, "crypto.totp_enrolled", EventTypeEnrolled)
	assert.Equal(t, "crypto.totp_cleared", EventTypeCleared)
	assert.Equal(t, "crypto.totp_recovery_code_consumed", EventTypeRecoveryConsumed)
	assert.Equal(t, "crypto.totp_locked", EventTypeLocked)
}
