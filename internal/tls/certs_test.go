// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package tls provides TLS certificate generation and loading for HoloMUSH.
package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCA(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	require.NotNil(t, ca.Certificate, "CA certificate is nil")
	require.NotNil(t, ca.PrivateKey, "CA private key is nil")
	assert.True(t, ca.Certificate.IsCA, "Certificate is not a CA")

	// Verify game_id is in CN
	expectedCN := "HoloMUSH CA " + gameID
	assert.Equal(t, expectedCN, ca.Certificate.Subject.CommonName)

	// Verify game_id is in SAN as URI
	expectedURI := "holomush://game/" + gameID
	found := false
	for _, uri := range ca.Certificate.URIs {
		if uri.String() == expectedURI {
			found = true
			break
		}
	}
	assert.True(t, found, "CA SAN URIs missing %q", expectedURI)

	// Save and verify we can load it
	err = SaveCertificates(tmpDir, ca, nil)
	require.NoError(t, err)

	certPath := filepath.Join(tmpDir, "root-ca.crt")
	keyPath := filepath.Join(tmpDir, "root-ca.key")

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	require.NoError(t, err, "Failed to load CA")

	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err, "Failed to parse cert")

	assert.True(t, x509Cert.IsCA, "Loaded certificate is not a CA")
}

func TestGenerateServerCert(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err)

	require.NotNil(t, serverCert.Certificate, "Server certificate is nil")
	require.NotNil(t, serverCert.PrivateKey, "Server private key is nil")

	// Verify it's signed by CA
	err = serverCert.Certificate.CheckSignatureFrom(ca.Certificate)
	assert.NoError(t, err, "Server cert not signed by CA")

	// Verify CN
	expectedCN := "holomush-core"
	assert.Equal(t, expectedCN, serverCert.Certificate.Subject.CommonName)

	// Verify game_id is in SAN DNS names
	expectedSAN := "holomush-" + gameID
	assert.Contains(t, serverCert.Certificate.DNSNames, expectedSAN)

	// Save and verify
	err = SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err)

	certPath := filepath.Join(tmpDir, "core.crt")
	keyPath := filepath.Join(tmpDir, "core.key")

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	require.NoError(t, err, "Failed to load server cert")

	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err, "Failed to parse cert")

	assert.False(t, x509Cert.IsCA, "Server certificate should not be a CA")
}

func TestSaveAndLoadCertificates(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	// Generate CA
	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	// Generate server cert
	serverCert, err := GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err)

	// Save both
	err = SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err)

	// Verify files exist
	files := []string{"root-ca.crt", "root-ca.key", "core.crt", "core.key"}
	for _, f := range files {
		path := filepath.Join(tmpDir, f)
		assert.FileExists(t, path)
	}

	// Load CA
	loadedCA, err := LoadCA(tmpDir)
	require.NoError(t, err)

	assert.NotNil(t, loadedCA.Certificate, "Loaded CA certificate is nil")
	assert.NotNil(t, loadedCA.PrivateKey, "Loaded CA private key is nil")
	assert.True(t, loadedCA.Certificate.IsCA, "Loaded certificate is not a CA")

	// Verify game_id preserved in loaded CA
	expectedCN := "HoloMUSH CA " + gameID
	assert.Equal(t, expectedCN, loadedCA.Certificate.Subject.CommonName)
}

func TestLoadCA_MissingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Try to load from empty directory
	_, err := LoadCA(tmpDir)
	assert.Error(t, err, "LoadCA() should return error for missing files")

	// Create only cert file, missing key
	certPath := filepath.Join(tmpDir, "root-ca.crt")
	err = os.WriteFile(certPath, []byte("dummy"), 0o600)
	require.NoError(t, err, "Failed to create dummy cert")

	_, err = LoadCA(tmpDir)
	assert.Error(t, err, "LoadCA() should return error when key file is missing")
}

func TestSaveCertificates_OnlyCA(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	// Save only CA (nil server cert)
	err = SaveCertificates(tmpDir, ca, nil)
	require.NoError(t, err)

	// Verify CA files exist
	caFiles := []string{"root-ca.crt", "root-ca.key"}
	for _, f := range caFiles {
		path := filepath.Join(tmpDir, f)
		assert.FileExists(t, path)
	}

	// Verify server cert files don't exist
	serverFiles := []string{"core.crt", "core.key"}
	for _, f := range serverFiles {
		path := filepath.Join(tmpDir, f)
		assert.NoFileExists(t, path)
	}
}

func TestGameIDExtraction(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	// Extract game_id from URI SAN
	var extractedID string
	for _, uri := range ca.Certificate.URIs {
		if uri.Scheme == "holomush" && uri.Host == "game" {
			extractedID = uri.Path[1:] // Remove leading slash
			break
		}
	}

	assert.Equal(t, gameID, extractedID)
}

func TestGenerateCA_URIFormat(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	// Parse and verify URI format
	expectedURI, _ := url.Parse("holomush://game/" + gameID)

	found := false
	for _, uri := range ca.Certificate.URIs {
		if uri.Scheme == expectedURI.Scheme &&
			uri.Host == expectedURI.Host &&
			uri.Path == expectedURI.Path {
			found = true
			break
		}
	}
	assert.True(t, found, "CA certificate missing URI SAN holomush://game/%s", gameID)
}

func TestGenerateClientCert(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	clientCert, err := GenerateClientCert(ca, "gateway")
	require.NoError(t, err)

	require.NotNil(t, clientCert.Certificate, "Client certificate is nil")
	require.NotNil(t, clientCert.PrivateKey, "Client private key is nil")
	assert.Equal(t, "gateway", clientCert.Name)

	// Verify it's signed by CA
	err = clientCert.Certificate.CheckSignatureFrom(ca.Certificate)
	assert.NoError(t, err, "Client cert not signed by CA")

	// Verify CN
	expectedCN := "holomush-gateway"
	assert.Equal(t, expectedCN, clientCert.Certificate.Subject.CommonName)

	// Verify ExtKeyUsage is ClientAuth
	assert.Contains(t, clientCert.Certificate.ExtKeyUsage, x509.ExtKeyUsageClientAuth, "Client certificate missing ClientAuth ExtKeyUsage")
}

func TestSaveClientCert(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	clientCert, err := GenerateClientCert(ca, "gateway")
	require.NoError(t, err)

	err = SaveClientCert(tmpDir, clientCert)
	require.NoError(t, err)

	// Verify files exist
	files := []string{"gateway.crt", "gateway.key"}
	for _, f := range files {
		path := filepath.Join(tmpDir, f)
		assert.FileExists(t, path)
	}

	// Verify we can load the certificate
	cert, err := tls.LoadX509KeyPair(
		filepath.Join(tmpDir, "gateway.crt"),
		filepath.Join(tmpDir, "gateway.key"),
	)
	require.NoError(t, err, "Failed to load client cert")

	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err, "Failed to parse cert")

	assert.Equal(t, "holomush-gateway", x509Cert.Subject.CommonName)
}

func TestLoadServerTLS(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	// Generate and save CA
	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	// Generate and save server cert
	serverCert, err := GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err)

	err = SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err)

	// Load server TLS config
	config, err := LoadServerTLS(tmpDir, "core")
	require.NoError(t, err)

	// Verify config
	assert.Equal(t, tls.RequireAndVerifyClientCert, config.ClientAuth, "Expected mTLS with client cert verification")
	assert.Len(t, config.Certificates, 1)
	assert.NotNil(t, config.ClientCAs, "Expected ClientCAs pool")
	assert.Equal(t, uint16(tls.VersionTLS13), config.MinVersion)
}

func TestLoadClientTLS(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	// Generate and save CA
	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	err = SaveCertificates(tmpDir, ca, nil)
	require.NoError(t, err)

	// Generate and save client cert
	clientCert, err := GenerateClientCert(ca, "gateway")
	require.NoError(t, err)

	err = SaveClientCert(tmpDir, clientCert)
	require.NoError(t, err)

	// Load client TLS config
	config, err := LoadClientTLS(tmpDir, "gateway", gameID)
	require.NoError(t, err)

	// Verify config
	assert.NotNil(t, config.RootCAs, "Expected RootCAs pool")
	assert.Len(t, config.Certificates, 1)
	expectedServerName := "holomush-" + gameID
	assert.Equal(t, expectedServerName, config.ServerName)
	assert.Equal(t, uint16(tls.VersionTLS13), config.MinVersion)
}

func TestLoadServerTLS_MissingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Try to load from empty directory
	_, err := LoadServerTLS(tmpDir, "core")
	assert.Error(t, err, "LoadServerTLS() should return error for missing files")
}

func TestLoadClientTLS_MissingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Try to load from empty directory
	_, err := LoadClientTLS(tmpDir, "gateway", "test-game")
	assert.Error(t, err, "LoadClientTLS() should return error for missing files")
}

// =============================================================================
// Certificate Expiration Tests (e55.34)
// =============================================================================

func TestCertificateNearExpiration(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	// Generate a certificate that expires in 7 days (near expiration)
	cert, err := generateCertWithExpiry(ca, gameID, "near-expiry", 7*24*time.Hour)
	require.NoError(t, err)

	// Check expiration status
	status := CheckCertificateExpiration(cert.Certificate, 30*24*time.Hour) // 30-day warning threshold
	assert.False(t, status.IsExpired, "Certificate should not be expired yet")
	assert.True(t, status.NearExpiration, "Certificate should be marked as near expiration")
	assert.LessOrEqual(t, status.DaysUntilExpiration, 8)
	assert.NotEmpty(t, status.Warning, "Expected warning message for near-expiration certificate")
}

func TestCertificateExpired(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	// Generate a certificate that is already expired (negative duration)
	cert, err := generateExpiredCert(ca, gameID, "expired")
	require.NoError(t, err)

	// Check expiration status
	status := CheckCertificateExpiration(cert.Certificate, 30*24*time.Hour)
	assert.True(t, status.IsExpired, "Certificate should be marked as expired")
	assert.Error(t, status.Error, "Expected error for expired certificate")
	assert.Less(t, status.DaysUntilExpiration, 0, "DaysUntilExpiration should be < 0 for expired cert")
}

func TestCertificateValid(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	// Generate a certificate with plenty of time (1 year)
	serverCert, err := GenerateServerCert(ca, gameID, "valid-server")
	require.NoError(t, err)

	// Check expiration status
	status := CheckCertificateExpiration(serverCert.Certificate, 30*24*time.Hour)
	assert.False(t, status.IsExpired, "Certificate should not be expired")
	assert.False(t, status.NearExpiration, "Certificate should not be near expiration")
	assert.Empty(t, status.Warning, "Expected no warning")
	assert.GreaterOrEqual(t, status.DaysUntilExpiration, 360, "DaysUntilExpiration should be >= 360 for 1-year cert")
}

func TestCertificateRotation(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	// Generate original CA and server cert
	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	oldServerCert, err := GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err)

	// Save original certs
	err = SaveCertificates(tmpDir, ca, oldServerCert)
	require.NoError(t, err)

	// Record original serial number
	oldSerial := oldServerCert.Certificate.SerialNumber

	// Generate new server cert (rotation)
	newServerCert, err := GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err)

	// Verify new cert has different serial
	assert.NotEqual(t, 0, oldSerial.Cmp(newServerCert.Certificate.SerialNumber), "Rotated certificate should have different serial number")

	// Save rotated cert (overwrites old one)
	err = SaveCertificates(tmpDir, ca, newServerCert)
	require.NoError(t, err)

	// Verify we can load the rotated cert
	config, err := LoadServerTLS(tmpDir, "core")
	require.NoError(t, err)

	// Verify loaded cert is the new one
	loadedCert, err := x509.ParseCertificate(config.Certificates[0].Certificate[0])
	require.NoError(t, err, "Failed to parse loaded cert")

	assert.Equal(t, 0, loadedCert.SerialNumber.Cmp(newServerCert.Certificate.SerialNumber), "Loaded certificate should be the rotated one")
}

// =============================================================================
// Invalid Certificate Tests (e55.35)
// =============================================================================

func TestSelfSignedCertWithoutCA(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	// Generate CA and proper server cert
	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err)

	// Save certs
	err = SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err)

	// Generate a separate CA (simulating self-signed cert from wrong CA)
	differentCA, err := GenerateCA("different-game-id")
	require.NoError(t, err)

	// Try to validate server cert against the wrong CA
	err = ValidateCertificateChain(serverCert.Certificate, differentCA.Certificate)
	assert.Error(t, err, "ValidateCertificateChain() should fail for cert signed by different CA")

	// Error message should be clear
	if err != nil {
		assert.True(t, containsAny(err.Error(), []string{"signature", "verify", "CA", "certificate"}), "Error message should mention signature/verification issue, got: %v", err)
	}
}

func TestWrongHostnameInCertificate(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err)

	// Test hostname validation
	tests := []struct {
		name          string
		hostname      string
		expectValid   bool
		expectErrPart string
	}{
		{
			name:        "valid localhost",
			hostname:    "localhost",
			expectValid: true,
		},
		{
			name:        "valid game hostname",
			hostname:    "holomush-" + gameID,
			expectValid: true,
		},
		{
			name:          "wrong hostname",
			hostname:      "wrong.example.com",
			expectValid:   false,
			expectErrPart: "hostname",
		},
		{
			name:          "different game id",
			hostname:      "holomush-different-game",
			expectValid:   false,
			expectErrPart: "hostname",
		},
		{
			name:          "empty hostname",
			hostname:      "",
			expectValid:   false,
			expectErrPart: "hostname",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHostname(serverCert.Certificate, tt.hostname)
			if tt.expectValid {
				assert.NoError(t, err, "ValidateHostname(%q) unexpected error", tt.hostname)
			} else {
				assert.Error(t, err, "ValidateHostname(%q) expected error", tt.hostname)
				if err != nil && tt.expectErrPart != "" {
					assert.True(t, containsAny(err.Error(), []string{tt.expectErrPart}), "Error message should mention %q, got: %v", tt.expectErrPart, err)
				}
			}
		})
	}
}

func TestMismatchedKeyAndCertPair(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	// Generate CA and two server certs
	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	serverCert1, err := GenerateServerCert(ca, gameID, "server1")
	require.NoError(t, err)

	serverCert2, err := GenerateServerCert(ca, gameID, "server2")
	require.NoError(t, err)

	// Save server1's cert
	err = saveCert(filepath.Join(tmpDir, "mismatched.crt"), serverCert1.Certificate)
	require.NoError(t, err)

	// Save server2's key (mismatched!)
	err = saveKey(filepath.Join(tmpDir, "mismatched.key"), serverCert2.PrivateKey)
	require.NoError(t, err)

	// Try to load the mismatched pair
	_, err = tls.LoadX509KeyPair(
		filepath.Join(tmpDir, "mismatched.crt"),
		filepath.Join(tmpDir, "mismatched.key"),
	)
	assert.Error(t, err, "Loading mismatched cert/key pair should fail")

	// Error message should indicate the mismatch
	if err != nil {
		assert.True(t, containsAny(err.Error(), []string{"private key", "match", "correspond", "not valid"}), "Error message should indicate key mismatch, got: %v", err)
	}
}

func TestValidateCertificateChain_ValidChain(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err)

	// Validate chain - should succeed
	err = ValidateCertificateChain(serverCert.Certificate, ca.Certificate)
	assert.NoError(t, err)
}

func TestLoadCertificate_InvalidPEM(t *testing.T) {
	tmpDir := t.TempDir()

	// Write invalid PEM data
	invalidCertPath := filepath.Join(tmpDir, "invalid.crt")
	err := os.WriteFile(invalidCertPath, []byte("not valid PEM data"), 0o600)
	require.NoError(t, err, "Failed to write invalid cert")

	// Try to read and parse
	certPEM, err := os.ReadFile(filepath.Clean(invalidCertPath))
	require.NoError(t, err, "Failed to read cert file")

	block, _ := pem.Decode(certPEM)
	assert.Nil(t, block, "pem.Decode() should return nil for invalid PEM")
}

func TestClientCertForServerAuth(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	// Generate a client cert
	clientCert, err := GenerateClientCert(ca, "gateway")
	require.NoError(t, err)

	// Verify it does NOT have ServerAuth ExtKeyUsage
	assert.NotContains(t, clientCert.Certificate.ExtKeyUsage, x509.ExtKeyUsageServerAuth, "Client certificate should NOT have ServerAuth ExtKeyUsage")

	// Validate that using client cert for server auth would be inappropriate
	err = ValidateExtKeyUsage(clientCert.Certificate, x509.ExtKeyUsageServerAuth)
	assert.Error(t, err, "Client cert should fail ServerAuth validation")
	if err != nil {
		assert.True(t, containsAny(err.Error(), []string{"ExtKeyUsage", "server", "usage"}), "Error message should mention ExtKeyUsage issue, got: %v", err)
	}
}

func TestServerCertForClientAuth(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	// Generate a server cert
	serverCert, err := GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err)

	// Verify it does NOT have ClientAuth ExtKeyUsage
	assert.NotContains(t, serverCert.Certificate.ExtKeyUsage, x509.ExtKeyUsageClientAuth, "Server certificate should NOT have ClientAuth ExtKeyUsage")

	// Validate that using server cert for client auth would be inappropriate
	err = ValidateExtKeyUsage(serverCert.Certificate, x509.ExtKeyUsageClientAuth)
	assert.Error(t, err, "Server cert should fail ClientAuth validation")
}

// =============================================================================
// Helper Functions for Tests
// =============================================================================

// generateCertWithExpiry creates a server certificate with a specific expiry duration.
func generateCertWithExpiry(ca *CA, gameID, name string, validFor time.Duration) (*ServerCert, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"HoloMUSH"},
			CommonName:   "holomush-" + name,
		},
		NotBefore:   time.Now().Add(-time.Hour), // Started 1 hour ago
		NotAfter:    time.Now().Add(validFor),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost", "holomush-" + gameID},
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, ca.Certificate, &key.PublicKey, ca.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return &ServerCert{Certificate: cert, PrivateKey: key, Name: name}, nil
}

// generateExpiredCert creates a server certificate that is already expired.
func generateExpiredCert(ca *CA, gameID, name string) (*ServerCert, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"HoloMUSH"},
			CommonName:   "holomush-" + name,
		},
		NotBefore:   time.Now().Add(-48 * time.Hour), // Started 48 hours ago
		NotAfter:    time.Now().Add(-24 * time.Hour), // Expired 24 hours ago
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost", "holomush-" + gameID},
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, ca.Certificate, &key.PublicKey, ca.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return &ServerCert{Certificate: cert, PrivateKey: key, Name: name}, nil
}

// containsAny checks if s contains any of the substrings (case-insensitive).
func containsAny(s string, substrings []string) bool {
	sLower := strings.ToLower(s)
	for _, sub := range substrings {
		if strings.Contains(sLower, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

// =============================================================================
// Additional Coverage Tests (e55.69)
// =============================================================================

func TestLoadCA_InvalidCertPEM(t *testing.T) {
	tmpDir := t.TempDir()

	// Create valid key but invalid cert PEM
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	keyBytes, _ := x509.MarshalECPrivateKey(key)

	certPath := filepath.Join(tmpDir, "root-ca.crt")
	keyPath := filepath.Join(tmpDir, "root-ca.key")

	// Write invalid cert (not valid PEM)
	err := os.WriteFile(certPath, []byte("not valid pem data"), 0o600)
	require.NoError(t, err, "Failed to write invalid cert")

	// Write valid key
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	err = os.WriteFile(keyPath, keyPEM, 0o600)
	require.NoError(t, err, "Failed to write key")

	_, err = LoadCA(tmpDir)
	assert.Error(t, err, "LoadCA() should return error for invalid cert PEM")
	assert.Contains(t, err.Error(), "decode CA certificate PEM")
}

func TestLoadCA_InvalidKeyPEM(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game"

	// Generate valid CA first to get valid cert
	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	certPath := filepath.Join(tmpDir, "root-ca.crt")
	keyPath := filepath.Join(tmpDir, "root-ca.key")

	// Write valid cert
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Certificate.Raw})
	err = os.WriteFile(certPath, certPEM, 0o600)
	require.NoError(t, err, "Failed to write cert")

	// Write invalid key (not valid PEM)
	err = os.WriteFile(keyPath, []byte("not valid pem data"), 0o600)
	require.NoError(t, err, "Failed to write key")

	_, err = LoadCA(tmpDir)
	assert.Error(t, err, "LoadCA() should return error for invalid key PEM")
	assert.Contains(t, err.Error(), "decode CA key PEM")
}

func TestLoadCA_InvalidCertificateData(t *testing.T) {
	tmpDir := t.TempDir()

	certPath := filepath.Join(tmpDir, "root-ca.crt")
	keyPath := filepath.Join(tmpDir, "root-ca.key")

	// Write valid PEM but invalid certificate data
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("invalid cert data")})
	err := os.WriteFile(certPath, certPEM, 0o600)
	require.NoError(t, err, "Failed to write cert")

	// Write valid key PEM but invalid key data
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte("invalid key data")})
	err = os.WriteFile(keyPath, keyPEM, 0o600)
	require.NoError(t, err, "Failed to write key")

	_, err = LoadCA(tmpDir)
	assert.Error(t, err, "LoadCA() should return error for invalid certificate data")
	assert.Contains(t, err.Error(), "parse CA certificate")
}

func TestLoadCA_InvalidKeyData(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game"

	// Generate valid CA first to get valid cert
	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	certPath := filepath.Join(tmpDir, "root-ca.crt")
	keyPath := filepath.Join(tmpDir, "root-ca.key")

	// Write valid cert PEM
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Certificate.Raw})
	err = os.WriteFile(certPath, certPEM, 0o600)
	require.NoError(t, err, "Failed to write cert")

	// Write valid PEM but invalid key data
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte("invalid key data")})
	err = os.WriteFile(keyPath, keyPEM, 0o600)
	require.NoError(t, err, "Failed to write key")

	_, err = LoadCA(tmpDir)
	assert.Error(t, err, "LoadCA() should return error for invalid key data")
	assert.Contains(t, err.Error(), "parse CA key")
}

func TestLoadServerTLS_InvalidCAPEM(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game"

	// Generate and save valid server cert
	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err)

	// Save server cert
	err = saveCert(filepath.Join(tmpDir, "core.crt"), serverCert.Certificate)
	require.NoError(t, err)
	err = saveKey(filepath.Join(tmpDir, "core.key"), serverCert.PrivateKey)
	require.NoError(t, err)

	// Write invalid CA (valid PEM but not a certificate)
	invalidCAPEM := pem.EncodeToMemory(&pem.Block{Type: "JUNK", Bytes: []byte("not a certificate")})
	err = os.WriteFile(filepath.Join(tmpDir, "root-ca.crt"), invalidCAPEM, 0o600)
	require.NoError(t, err, "Failed to write invalid CA")

	_, err = LoadServerTLS(tmpDir, "core")
	assert.Error(t, err, "LoadServerTLS() should return error for invalid CA PEM")
	assert.Contains(t, err.Error(), "failed to add CA certificate")
}

func TestLoadClientTLS_InvalidCAPEM(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game"

	// Generate and save valid client cert
	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	clientCert, err := GenerateClientCert(ca, "gateway")
	require.NoError(t, err)

	// Save client cert
	err = saveCert(filepath.Join(tmpDir, "gateway.crt"), clientCert.Certificate)
	require.NoError(t, err)
	err = saveKey(filepath.Join(tmpDir, "gateway.key"), clientCert.PrivateKey)
	require.NoError(t, err)

	// Write invalid CA (valid PEM but not a certificate)
	invalidCAPEM := pem.EncodeToMemory(&pem.Block{Type: "JUNK", Bytes: []byte("not a certificate")})
	err = os.WriteFile(filepath.Join(tmpDir, "root-ca.crt"), invalidCAPEM, 0o600)
	require.NoError(t, err, "Failed to write invalid CA")

	_, err = LoadClientTLS(tmpDir, "gateway", gameID)
	assert.Error(t, err, "LoadClientTLS() should return error for invalid CA PEM")
	assert.Contains(t, err.Error(), "failed to add CA certificate")
}

func TestLoadServerTLS_MissingCAFile(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game"

	// Generate and save valid server cert
	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err)

	// Save server cert but NOT CA
	err = saveCert(filepath.Join(tmpDir, "core.crt"), serverCert.Certificate)
	require.NoError(t, err)
	err = saveKey(filepath.Join(tmpDir, "core.key"), serverCert.PrivateKey)
	require.NoError(t, err)

	_, err = LoadServerTLS(tmpDir, "core")
	assert.Error(t, err, "LoadServerTLS() should return error for missing CA file")
	assert.Contains(t, err.Error(), "read CA certificate")
}

func TestLoadClientTLS_MissingCAFile(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game"

	// Generate and save valid client cert
	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	clientCert, err := GenerateClientCert(ca, "gateway")
	require.NoError(t, err)

	// Save client cert but NOT CA
	err = saveCert(filepath.Join(tmpDir, "gateway.crt"), clientCert.Certificate)
	require.NoError(t, err)
	err = saveKey(filepath.Join(tmpDir, "gateway.key"), clientCert.PrivateKey)
	require.NoError(t, err)

	_, err = LoadClientTLS(tmpDir, "gateway", gameID)
	assert.Error(t, err, "LoadClientTLS() should return error for missing CA file")
	assert.Contains(t, err.Error(), "read CA certificate")
}

func TestValidateCertificateChain_NilCert(t *testing.T) {
	gameID := "test-game"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	err = ValidateCertificateChain(nil, ca.Certificate)
	assert.Error(t, err, "ValidateCertificateChain() should return error for nil cert")
	assert.Contains(t, err.Error(), "certificate is nil")
}

func TestValidateCertificateChain_NilCA(t *testing.T) {
	gameID := "test-game"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err)

	err = ValidateCertificateChain(serverCert.Certificate, nil)
	assert.Error(t, err, "ValidateCertificateChain() should return error for nil CA")
	assert.Contains(t, err.Error(), "CA certificate is nil")
}

func TestValidateHostname_NilCert(t *testing.T) {
	err := ValidateHostname(nil, "localhost")
	assert.Error(t, err, "ValidateHostname() should return error for nil cert")
	assert.Contains(t, err.Error(), "certificate is nil")
}

func TestValidateHostname_IPAddress(t *testing.T) {
	gameID := "test-game"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err)

	// Test with valid IP (127.0.0.1 is in the cert's IPAddresses)
	err = ValidateHostname(serverCert.Certificate, "127.0.0.1")
	assert.NoError(t, err, "ValidateHostname() should accept 127.0.0.1")

	// Test with invalid IP
	err = ValidateHostname(serverCert.Certificate, "192.168.1.1")
	assert.Error(t, err, "ValidateHostname() should reject 192.168.1.1")
}

func TestValidateExtKeyUsage_NilCert(t *testing.T) {
	err := ValidateExtKeyUsage(nil, x509.ExtKeyUsageServerAuth)
	assert.Error(t, err, "ValidateExtKeyUsage() should return error for nil cert")
	assert.Contains(t, err.Error(), "certificate is nil")
}

func TestCheckCertificateExpiration_NotYetValid(t *testing.T) {
	gameID := "test-game"

	ca, err := GenerateCA(gameID)
	require.NoError(t, err)

	// Generate a certificate that is not yet valid
	cert, err := generateNotYetValidCert(ca, gameID, "future")
	require.NoError(t, err)

	status := CheckCertificateExpiration(cert.Certificate, 30*24*time.Hour)
	assert.Error(t, status.Error, "Expected error for not-yet-valid certificate")
	assert.Contains(t, status.Error.Error(), "not yet valid")
}

// generateNotYetValidCert creates a certificate that is not yet valid.
func generateNotYetValidCert(ca *CA, gameID, name string) (*ServerCert, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"HoloMUSH"},
			CommonName:   "holomush-" + name,
		},
		NotBefore:   time.Now().Add(24 * time.Hour), // Starts tomorrow
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost", "holomush-" + gameID},
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, ca.Certificate, &key.PublicKey, ca.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return &ServerCert{Certificate: cert, PrivateKey: key, Name: name}, nil
}
