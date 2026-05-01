// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek

import (
	"context"
	"encoding/hex"
	"os"

	"github.com/samber/oops"
)

const (
	// KEKByteLength is the required length of a KEK in bytes (256 bits)
	// — chacha20poly1305 key size.
	KEKByteLength = 32

	// EnvSourceName is the canonical KEKSource.Name() value.
	EnvSourceName = "local-aead/env"
)

// EnvSource reads the master KEK from an environment variable.
// Refused in production mode; intended for unit and integration tests.
// The env value MUST be 64 hex characters (32 bytes after decode).
type EnvSource struct {
	envVar   string
	prodMode bool
}

// NewEnvSource constructs an EnvSource. prodMode=true causes Load to
// return CRYPTO_KEK_ENV_SOURCE_PROD_FORBIDDEN; tests pass false.
func NewEnvSource(envVar string, prodMode bool) *EnvSource {
	return &EnvSource{envVar: envVar, prodMode: prodMode}
}

// Name returns "local-aead/env".
func (s *EnvSource) Name() string { return EnvSourceName }

// Load decodes the hex-encoded KEK from the configured env var. Strict
// hex (64 chars → 32 bytes) — no raw-bytes fallback to avoid ambiguity
// when an ASCII KEK happens to also be valid hex.
func (s *EnvSource) Load(_ context.Context) ([]byte, error) {
	if s.prodMode {
		return nil, oops.Code("KEK_ENV_SOURCE_PROD_FORBIDDEN").
			With("env_var", s.envVar).
			Errorf("env KEKSource is dev/test only — refused in production mode")
	}
	raw, ok := os.LookupEnv(s.envVar)
	if !ok || raw == "" {
		return nil, oops.Code("KEK_ENV_VAR_MISSING").
			With("env_var", s.envVar).
			Errorf("env var %q not set or empty", s.envVar)
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		return nil, oops.Code("KEK_ENV_VAR_NOT_HEX").
			With("env_var", s.envVar).
			Wrap(err)
	}
	if len(decoded) != KEKByteLength {
		return nil, oops.Code("KEK_ENV_VAR_WRONG_LENGTH").
			With("env_var", s.envVar).
			With("expected_bytes", KEKByteLength).
			With("got_bytes", len(decoded)).
			Errorf("env var %q must decode to %d bytes (64 hex chars); got %d bytes",
				s.envVar, KEKByteLength, len(decoded))
	}
	return decoded, nil
}

// Persist refuses (env is read-only).
func (s *EnvSource) Persist(_ context.Context, _ []byte) error {
	return oops.Code("KEK_ENV_SOURCE_READ_ONLY").
		With("env_var", s.envVar).
		Errorf("env KEKSource cannot persist; rotate via a writable source")
}
