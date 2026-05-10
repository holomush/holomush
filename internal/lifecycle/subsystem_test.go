// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle

import "testing"

func TestSubsystemClusterStringIsCluster(t *testing.T) {
	if got := SubsystemCluster.String(); got != "cluster" {
		t.Fatalf("SubsystemCluster.String() = %q; want %q", got, "cluster")
	}
}

func TestSubsystemCryptoChainVerifierStringIsCryptoChainVerifier(t *testing.T) {
	if got := SubsystemCryptoChainVerifier.String(); got != "crypto_chain_verifier" {
		t.Fatalf("SubsystemCryptoChainVerifier.String() = %q; want %q", got, "crypto_chain_verifier")
	}
}

func TestSubsystemCryptoPolicyStringIsCryptoPolicy(t *testing.T) {
	if got := SubsystemCryptoPolicy.String(); got != "crypto_policy" {
		t.Fatalf("SubsystemCryptoPolicy.String() = %q; want %q", got, "crypto_policy")
	}
}
