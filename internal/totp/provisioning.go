// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"net/url"

	"github.com/samber/oops"
)

const (
	secretBytes                = 20 // 160 bits, RFC 4226 §4
	recoveryCodeBytes          = 8  // 64 bits, formatted xxxx-xxxx-xxxx-xxxx
	recoveryCodesPerEnrollment = 10
)

func generateSecret() (string, error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", oops.Code("TOTP_SECRET_GEN_FAILED").Wrap(err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

func buildProvisioningURI(username, gameID, secret string) (string, error) {
	if username == "" || gameID == "" || secret == "" {
		return "", oops.Code("TOTP_URI_INVALID_INPUT").
			Errorf("username, gameID, and secret all required")
	}
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", "holomush")
	account := fmt.Sprintf("holomush-%s:%s", gameID, username)
	return fmt.Sprintf("otpauth://totp/%s?%s", url.PathEscape(account), q.Encode()), nil
}

func generateRecoveryCodes(n int) ([]string, error) {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		buf := make([]byte, recoveryCodeBytes)
		if _, err := rand.Read(buf); err != nil {
			return nil, oops.Code("TOTP_RECOVERY_GEN_FAILED").Wrap(err)
		}
		raw := hex.EncodeToString(buf)
		out[i] = fmt.Sprintf("%s-%s-%s-%s", raw[0:4], raw[4:8], raw[8:12], raw[12:16])
	}
	return out, nil
}
