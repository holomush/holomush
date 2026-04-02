// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

// mockHasherLogging is a mock hasher for testing.
// It validates passwords based on a simple rule: password must be "correctpassword".
type mockHasherLogging struct{}

func (m *mockHasherLogging) Hash(_ string) (string, error) {
	return "$argon2id$v=19$m=65536,t=1,p=4$salt$hash", nil
}

func (m *mockHasherLogging) Verify(password, hash string) (bool, error) {
	// Only accept "correctpassword" as valid
	// For dummy hash (timing attack prevention), always return false
	if hash == "$argon2id$v=19$m=65536,t=1,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" {
		return false, nil
	}
	return password == "correctpassword", nil
}

func (m *mockHasherLogging) NeedsUpgrade(_ string) bool {
	return false
}

// logEntry represents a parsed JSON log entry.
type logEntry struct {
	Level     string `json:"level"`
	Msg       string `json:"msg"`
	Event     string `json:"event"`
	Operation string `json:"operation"`
	Error     string `json:"error"`
	PlayerID  string `json:"player_id"`
	SessionID string `json:"session_id"`
}
