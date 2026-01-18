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
)

func TestGenerateCA(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	if ca.Certificate == nil {
		t.Fatal("CA certificate is nil")
	}
	if ca.PrivateKey == nil {
		t.Fatal("CA private key is nil")
	}
	if !ca.Certificate.IsCA {
		t.Error("Certificate is not a CA")
	}

	// Verify game_id is in CN
	expectedCN := "HoloMUSH CA " + gameID
	if ca.Certificate.Subject.CommonName != expectedCN {
		t.Errorf("CA CN = %q, want %q", ca.Certificate.Subject.CommonName, expectedCN)
	}

	// Verify game_id is in SAN as URI
	expectedURI := "holomush://game/" + gameID
	found := false
	for _, uri := range ca.Certificate.URIs {
		if uri.String() == expectedURI {
			found = true
			break
		}
	}
	if !found {
		uris := make([]string, 0, len(ca.Certificate.URIs))
		for _, u := range ca.Certificate.URIs {
			uris = append(uris, u.String())
		}
		t.Errorf("CA SAN URIs missing %q, got %v", expectedURI, uris)
	}

	// Save and verify we can load it
	if err := SaveCertificates(tmpDir, ca, nil); err != nil {
		t.Fatalf("SaveCertificates() error = %v", err)
	}

	certPath := filepath.Join(tmpDir, "root-ca.crt")
	keyPath := filepath.Join(tmpDir, "root-ca.key")

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("Failed to load CA: %v", err)
	}

	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("Failed to parse cert: %v", err)
	}

	if !x509Cert.IsCA {
		t.Error("Loaded certificate is not a CA")
	}
}

func TestGenerateServerCert(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	if serverCert.Certificate == nil {
		t.Fatal("Server certificate is nil")
	}
	if serverCert.PrivateKey == nil {
		t.Fatal("Server private key is nil")
	}

	// Verify it's signed by CA
	if err := serverCert.Certificate.CheckSignatureFrom(ca.Certificate); err != nil {
		t.Errorf("Server cert not signed by CA: %v", err)
	}

	// Verify CN
	expectedCN := "holomush-core"
	if serverCert.Certificate.Subject.CommonName != expectedCN {
		t.Errorf("Server CN = %q, want %q", serverCert.Certificate.Subject.CommonName, expectedCN)
	}

	// Verify game_id is in SAN DNS names
	expectedSAN := "holomush-" + gameID
	found := false
	for _, name := range serverCert.Certificate.DNSNames {
		if name == expectedSAN {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Server SAN missing %q, got %v", expectedSAN, serverCert.Certificate.DNSNames)
	}

	// Save and verify
	if err := SaveCertificates(tmpDir, ca, serverCert); err != nil {
		t.Fatalf("SaveCertificates() error = %v", err)
	}

	certPath := filepath.Join(tmpDir, "core.crt")
	keyPath := filepath.Join(tmpDir, "core.key")

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("Failed to load server cert: %v", err)
	}

	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("Failed to parse cert: %v", err)
	}

	if x509Cert.IsCA {
		t.Error("Server certificate should not be a CA")
	}
}

func TestSaveAndLoadCertificates(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	// Generate CA
	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	// Generate server cert
	serverCert, err := GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	// Save both
	if err := SaveCertificates(tmpDir, ca, serverCert); err != nil {
		t.Fatalf("SaveCertificates() error = %v", err)
	}

	// Verify files exist
	files := []string{"root-ca.crt", "root-ca.key", "core.crt", "core.key"}
	for _, f := range files {
		path := filepath.Join(tmpDir, f)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("Expected file %s to exist: %v", f, err)
		}
	}

	// Load CA
	loadedCA, err := LoadCA(tmpDir)
	if err != nil {
		t.Fatalf("LoadCA() error = %v", err)
	}

	if loadedCA.Certificate == nil {
		t.Error("Loaded CA certificate is nil")
	}
	if loadedCA.PrivateKey == nil {
		t.Error("Loaded CA private key is nil")
	}
	if !loadedCA.Certificate.IsCA {
		t.Error("Loaded certificate is not a CA")
	}

	// Verify game_id preserved in loaded CA
	expectedCN := "HoloMUSH CA " + gameID
	if loadedCA.Certificate.Subject.CommonName != expectedCN {
		t.Errorf("Loaded CA CN = %q, want %q", loadedCA.Certificate.Subject.CommonName, expectedCN)
	}
}

func TestLoadCA_MissingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Try to load from empty directory
	_, err := LoadCA(tmpDir)
	if err == nil {
		t.Error("LoadCA() should return error for missing files")
	}

	// Create only cert file, missing key
	certPath := filepath.Join(tmpDir, "root-ca.crt")
	if err := os.WriteFile(certPath, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("Failed to create dummy cert: %v", err)
	}

	_, err = LoadCA(tmpDir)
	if err == nil {
		t.Error("LoadCA() should return error when key file is missing")
	}
}

func TestSaveCertificates_OnlyCA(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	// Save only CA (nil server cert)
	if err := SaveCertificates(tmpDir, ca, nil); err != nil {
		t.Fatalf("SaveCertificates() error = %v", err)
	}

	// Verify CA files exist
	caFiles := []string{"root-ca.crt", "root-ca.key"}
	for _, f := range caFiles {
		path := filepath.Join(tmpDir, f)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("Expected file %s to exist: %v", f, err)
		}
	}

	// Verify server cert files don't exist
	serverFiles := []string{"core.crt", "core.key"}
	for _, f := range serverFiles {
		path := filepath.Join(tmpDir, f)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("File %s should not exist", f)
		}
	}
}

func TestGameIDExtraction(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	// Extract game_id from URI SAN
	var extractedID string
	for _, uri := range ca.Certificate.URIs {
		if uri.Scheme == "holomush" && uri.Host == "game" {
			extractedID = uri.Path[1:] // Remove leading slash
			break
		}
	}

	if extractedID != gameID {
		t.Errorf("Extracted game_id = %q, want %q", extractedID, gameID)
	}
}

func TestGenerateCA_URIFormat(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

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
	if !found {
		t.Errorf("CA certificate missing URI SAN holomush://game/%s", gameID)
	}
}

func TestGenerateClientCert(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	clientCert, err := GenerateClientCert(ca, "gateway")
	if err != nil {
		t.Fatalf("GenerateClientCert() error = %v", err)
	}

	if clientCert.Certificate == nil {
		t.Fatal("Client certificate is nil")
	}
	if clientCert.PrivateKey == nil {
		t.Fatal("Client private key is nil")
	}
	if clientCert.Name != "gateway" {
		t.Errorf("Client name = %q, want 'gateway'", clientCert.Name)
	}

	// Verify it's signed by CA
	if err := clientCert.Certificate.CheckSignatureFrom(ca.Certificate); err != nil {
		t.Errorf("Client cert not signed by CA: %v", err)
	}

	// Verify CN
	expectedCN := "holomush-gateway"
	if clientCert.Certificate.Subject.CommonName != expectedCN {
		t.Errorf("Client CN = %q, want %q", clientCert.Certificate.Subject.CommonName, expectedCN)
	}

	// Verify ExtKeyUsage is ClientAuth
	hasClientAuth := false
	for _, usage := range clientCert.Certificate.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			hasClientAuth = true
			break
		}
	}
	if !hasClientAuth {
		t.Error("Client certificate missing ClientAuth ExtKeyUsage")
	}
}

func TestSaveClientCert(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	clientCert, err := GenerateClientCert(ca, "gateway")
	if err != nil {
		t.Fatalf("GenerateClientCert() error = %v", err)
	}

	if err := SaveClientCert(tmpDir, clientCert); err != nil {
		t.Fatalf("SaveClientCert() error = %v", err)
	}

	// Verify files exist
	files := []string{"gateway.crt", "gateway.key"}
	for _, f := range files {
		path := filepath.Join(tmpDir, f)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("Expected file %s to exist: %v", f, err)
		}
	}

	// Verify we can load the certificate
	cert, err := tls.LoadX509KeyPair(
		filepath.Join(tmpDir, "gateway.crt"),
		filepath.Join(tmpDir, "gateway.key"),
	)
	if err != nil {
		t.Fatalf("Failed to load client cert: %v", err)
	}

	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("Failed to parse cert: %v", err)
	}

	if x509Cert.Subject.CommonName != "holomush-gateway" {
		t.Errorf("Loaded cert CN = %q, want 'holomush-gateway'", x509Cert.Subject.CommonName)
	}
}

func TestLoadServerTLS(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	// Generate and save CA
	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	// Generate and save server cert
	serverCert, err := GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	if err := SaveCertificates(tmpDir, ca, serverCert); err != nil {
		t.Fatalf("SaveCertificates() error = %v", err)
	}

	// Load server TLS config
	config, err := LoadServerTLS(tmpDir, "core")
	if err != nil {
		t.Fatalf("LoadServerTLS() error = %v", err)
	}

	// Verify config
	if config.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Error("Expected mTLS with client cert verification")
	}
	if len(config.Certificates) != 1 {
		t.Errorf("Expected 1 certificate, got %d", len(config.Certificates))
	}
	if config.ClientCAs == nil {
		t.Error("Expected ClientCAs pool")
	}
	if config.MinVersion != tls.VersionTLS13 {
		t.Errorf("Expected TLS 1.3 min version, got %d", config.MinVersion)
	}
}

func TestLoadClientTLS(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	// Generate and save CA
	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	if err := SaveCertificates(tmpDir, ca, nil); err != nil {
		t.Fatalf("SaveCertificates() error = %v", err)
	}

	// Generate and save client cert
	clientCert, err := GenerateClientCert(ca, "gateway")
	if err != nil {
		t.Fatalf("GenerateClientCert() error = %v", err)
	}

	if err := SaveClientCert(tmpDir, clientCert); err != nil {
		t.Fatalf("SaveClientCert() error = %v", err)
	}

	// Load client TLS config
	config, err := LoadClientTLS(tmpDir, "gateway", gameID)
	if err != nil {
		t.Fatalf("LoadClientTLS() error = %v", err)
	}

	// Verify config
	if config.RootCAs == nil {
		t.Error("Expected RootCAs pool")
	}
	if len(config.Certificates) != 1 {
		t.Errorf("Expected 1 certificate, got %d", len(config.Certificates))
	}
	expectedServerName := "holomush-" + gameID
	if config.ServerName != expectedServerName {
		t.Errorf("ServerName = %q, want %q", config.ServerName, expectedServerName)
	}
	if config.MinVersion != tls.VersionTLS13 {
		t.Errorf("Expected TLS 1.3 min version, got %d", config.MinVersion)
	}
}

func TestLoadServerTLS_MissingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Try to load from empty directory
	_, err := LoadServerTLS(tmpDir, "core")
	if err == nil {
		t.Error("LoadServerTLS() should return error for missing files")
	}
}

func TestLoadClientTLS_MissingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Try to load from empty directory
	_, err := LoadClientTLS(tmpDir, "gateway", "test-game")
	if err == nil {
		t.Error("LoadClientTLS() should return error for missing files")
	}
}

// =============================================================================
// Certificate Expiration Tests (e55.34)
// =============================================================================

func TestCertificateNearExpiration(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	// Generate a certificate that expires in 7 days (near expiration)
	cert, err := generateCertWithExpiry(ca, gameID, "near-expiry", 7*24*time.Hour)
	if err != nil {
		t.Fatalf("generateCertWithExpiry() error = %v", err)
	}

	// Check expiration status
	status := CheckCertificateExpiration(cert.Certificate, 30*24*time.Hour) // 30-day warning threshold
	if status.IsExpired {
		t.Error("Certificate should not be expired yet")
	}
	if !status.NearExpiration {
		t.Error("Certificate should be marked as near expiration")
	}
	if status.DaysUntilExpiration > 8 {
		t.Errorf("DaysUntilExpiration = %d, want <= 7", status.DaysUntilExpiration)
	}
	if status.Warning == "" {
		t.Error("Expected warning message for near-expiration certificate")
	}
}

func TestCertificateExpired(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	// Generate a certificate that is already expired (negative duration)
	cert, err := generateExpiredCert(ca, gameID, "expired")
	if err != nil {
		t.Fatalf("generateExpiredCert() error = %v", err)
	}

	// Check expiration status
	status := CheckCertificateExpiration(cert.Certificate, 30*24*time.Hour)
	if !status.IsExpired {
		t.Error("Certificate should be marked as expired")
	}
	if status.Error == nil {
		t.Error("Expected error for expired certificate")
	}
	if status.DaysUntilExpiration >= 0 {
		t.Errorf("DaysUntilExpiration = %d, want < 0 for expired cert", status.DaysUntilExpiration)
	}
}

func TestCertificateValid(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	// Generate a certificate with plenty of time (1 year)
	serverCert, err := GenerateServerCert(ca, gameID, "valid-server")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	// Check expiration status
	status := CheckCertificateExpiration(serverCert.Certificate, 30*24*time.Hour)
	if status.IsExpired {
		t.Error("Certificate should not be expired")
	}
	if status.NearExpiration {
		t.Error("Certificate should not be near expiration")
	}
	if status.Warning != "" {
		t.Errorf("Expected no warning, got: %s", status.Warning)
	}
	if status.DaysUntilExpiration < 360 {
		t.Errorf("DaysUntilExpiration = %d, want >= 360 for 1-year cert", status.DaysUntilExpiration)
	}
}

func TestCertificateRotation(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	// Generate original CA and server cert
	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	oldServerCert, err := GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	// Save original certs
	if err := SaveCertificates(tmpDir, ca, oldServerCert); err != nil {
		t.Fatalf("SaveCertificates() error = %v", err)
	}

	// Record original serial number
	oldSerial := oldServerCert.Certificate.SerialNumber

	// Generate new server cert (rotation)
	newServerCert, err := GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() rotation error = %v", err)
	}

	// Verify new cert has different serial
	if oldSerial.Cmp(newServerCert.Certificate.SerialNumber) == 0 {
		t.Error("Rotated certificate should have different serial number")
	}

	// Save rotated cert (overwrites old one)
	if err := SaveCertificates(tmpDir, ca, newServerCert); err != nil {
		t.Fatalf("SaveCertificates() rotation error = %v", err)
	}

	// Verify we can load the rotated cert
	config, err := LoadServerTLS(tmpDir, "core")
	if err != nil {
		t.Fatalf("LoadServerTLS() after rotation error = %v", err)
	}

	// Verify loaded cert is the new one
	loadedCert, err := x509.ParseCertificate(config.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatalf("Failed to parse loaded cert: %v", err)
	}

	if loadedCert.SerialNumber.Cmp(newServerCert.Certificate.SerialNumber) != 0 {
		t.Error("Loaded certificate should be the rotated one")
	}
}

// =============================================================================
// Invalid Certificate Tests (e55.35)
// =============================================================================

func TestSelfSignedCertWithoutCA(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "01HX7MZABC123DEF456GHJ"

	// Generate CA and proper server cert
	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	// Save certs
	if err := SaveCertificates(tmpDir, ca, serverCert); err != nil {
		t.Fatalf("SaveCertificates() error = %v", err)
	}

	// Generate a separate CA (simulating self-signed cert from wrong CA)
	differentCA, err := GenerateCA("different-game-id")
	if err != nil {
		t.Fatalf("GenerateCA() for different CA error = %v", err)
	}

	// Try to validate server cert against the wrong CA
	err = ValidateCertificateChain(serverCert.Certificate, differentCA.Certificate)
	if err == nil {
		t.Error("ValidateCertificateChain() should fail for cert signed by different CA")
	}

	// Error message should be clear
	if err != nil && !containsAny(err.Error(), []string{"signature", "verify", "CA", "certificate"}) {
		t.Errorf("Error message should mention signature/verification issue, got: %v", err)
	}
}

func TestWrongHostnameInCertificate(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

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
				if err != nil {
					t.Errorf("ValidateHostname(%q) unexpected error: %v", tt.hostname, err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateHostname(%q) expected error, got nil", tt.hostname)
				}
				if err != nil && tt.expectErrPart != "" && !containsAny(err.Error(), []string{tt.expectErrPart}) {
					t.Errorf("Error message should mention %q, got: %v", tt.expectErrPart, err)
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
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	serverCert1, err := GenerateServerCert(ca, gameID, "server1")
	if err != nil {
		t.Fatalf("GenerateServerCert() for server1 error = %v", err)
	}

	serverCert2, err := GenerateServerCert(ca, gameID, "server2")
	if err != nil {
		t.Fatalf("GenerateServerCert() for server2 error = %v", err)
	}

	// Save server1's cert
	if err := saveCert(filepath.Join(tmpDir, "mismatched.crt"), serverCert1.Certificate); err != nil {
		t.Fatalf("saveCert() error = %v", err)
	}

	// Save server2's key (mismatched!)
	if err := saveKey(filepath.Join(tmpDir, "mismatched.key"), serverCert2.PrivateKey); err != nil {
		t.Fatalf("saveKey() error = %v", err)
	}

	// Try to load the mismatched pair
	_, err = tls.LoadX509KeyPair(
		filepath.Join(tmpDir, "mismatched.crt"),
		filepath.Join(tmpDir, "mismatched.key"),
	)
	if err == nil {
		t.Error("Loading mismatched cert/key pair should fail")
	}

	// Error message should indicate the mismatch
	if err != nil && !containsAny(err.Error(), []string{"private key", "match", "correspond", "not valid"}) {
		t.Errorf("Error message should indicate key mismatch, got: %v", err)
	}
}

func TestValidateCertificateChain_ValidChain(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	// Validate chain - should succeed
	if err := ValidateCertificateChain(serverCert.Certificate, ca.Certificate); err != nil {
		t.Errorf("ValidateCertificateChain() unexpected error: %v", err)
	}
}

func TestLoadCertificate_InvalidPEM(t *testing.T) {
	tmpDir := t.TempDir()

	// Write invalid PEM data
	invalidCertPath := filepath.Join(tmpDir, "invalid.crt")
	if err := os.WriteFile(invalidCertPath, []byte("not valid PEM data"), 0o600); err != nil {
		t.Fatalf("Failed to write invalid cert: %v", err)
	}

	// Try to read and parse
	certPEM, err := os.ReadFile(filepath.Clean(invalidCertPath))
	if err != nil {
		t.Fatalf("Failed to read cert file: %v", err)
	}

	block, _ := pem.Decode(certPEM)
	if block != nil {
		t.Error("pem.Decode() should return nil for invalid PEM")
	}
}

func TestClientCertForServerAuth(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	// Generate a client cert
	clientCert, err := GenerateClientCert(ca, "gateway")
	if err != nil {
		t.Fatalf("GenerateClientCert() error = %v", err)
	}

	// Verify it does NOT have ServerAuth ExtKeyUsage
	hasServerAuth := false
	for _, usage := range clientCert.Certificate.ExtKeyUsage {
		if usage == x509.ExtKeyUsageServerAuth {
			hasServerAuth = true
			break
		}
	}
	if hasServerAuth {
		t.Error("Client certificate should NOT have ServerAuth ExtKeyUsage")
	}

	// Validate that using client cert for server auth would be inappropriate
	err = ValidateExtKeyUsage(clientCert.Certificate, x509.ExtKeyUsageServerAuth)
	if err == nil {
		t.Error("Client cert should fail ServerAuth validation")
	}
	if err != nil && !containsAny(err.Error(), []string{"ExtKeyUsage", "server", "usage"}) {
		t.Errorf("Error message should mention ExtKeyUsage issue, got: %v", err)
	}
}

func TestServerCertForClientAuth(t *testing.T) {
	gameID := "01HX7MZABC123DEF456GHJ"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	// Generate a server cert
	serverCert, err := GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	// Verify it does NOT have ClientAuth ExtKeyUsage
	hasClientAuth := false
	for _, usage := range serverCert.Certificate.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			hasClientAuth = true
			break
		}
	}
	if hasClientAuth {
		t.Error("Server certificate should NOT have ClientAuth ExtKeyUsage")
	}

	// Validate that using server cert for client auth would be inappropriate
	err = ValidateExtKeyUsage(serverCert.Certificate, x509.ExtKeyUsageClientAuth)
	if err == nil {
		t.Error("Server cert should fail ClientAuth validation")
	}
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
	if err := os.WriteFile(certPath, []byte("not valid pem data"), 0o600); err != nil {
		t.Fatalf("Failed to write invalid cert: %v", err)
	}

	// Write valid key
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("Failed to write key: %v", err)
	}

	_, err := LoadCA(tmpDir)
	if err == nil {
		t.Error("LoadCA() should return error for invalid cert PEM")
	}
	if !strings.Contains(err.Error(), "decode CA certificate PEM") {
		t.Errorf("Error should mention PEM decode failure, got: %v", err)
	}
}

func TestLoadCA_InvalidKeyPEM(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game"

	// Generate valid CA first to get valid cert
	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	certPath := filepath.Join(tmpDir, "root-ca.crt")
	keyPath := filepath.Join(tmpDir, "root-ca.key")

	// Write valid cert
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Certificate.Raw})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("Failed to write cert: %v", err)
	}

	// Write invalid key (not valid PEM)
	if err := os.WriteFile(keyPath, []byte("not valid pem data"), 0o600); err != nil {
		t.Fatalf("Failed to write key: %v", err)
	}

	_, err = LoadCA(tmpDir)
	if err == nil {
		t.Error("LoadCA() should return error for invalid key PEM")
	}
	if !strings.Contains(err.Error(), "decode CA key PEM") {
		t.Errorf("Error should mention key PEM decode failure, got: %v", err)
	}
}

func TestLoadCA_InvalidCertificateData(t *testing.T) {
	tmpDir := t.TempDir()

	certPath := filepath.Join(tmpDir, "root-ca.crt")
	keyPath := filepath.Join(tmpDir, "root-ca.key")

	// Write valid PEM but invalid certificate data
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("invalid cert data")})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("Failed to write cert: %v", err)
	}

	// Write valid key PEM but invalid key data
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte("invalid key data")})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("Failed to write key: %v", err)
	}

	_, err := LoadCA(tmpDir)
	if err == nil {
		t.Error("LoadCA() should return error for invalid certificate data")
	}
	if !strings.Contains(err.Error(), "parse CA certificate") {
		t.Errorf("Error should mention certificate parse failure, got: %v", err)
	}
}

func TestLoadCA_InvalidKeyData(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game"

	// Generate valid CA first to get valid cert
	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	certPath := filepath.Join(tmpDir, "root-ca.crt")
	keyPath := filepath.Join(tmpDir, "root-ca.key")

	// Write valid cert PEM
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Certificate.Raw})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("Failed to write cert: %v", err)
	}

	// Write valid PEM but invalid key data
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte("invalid key data")})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("Failed to write key: %v", err)
	}

	_, err = LoadCA(tmpDir)
	if err == nil {
		t.Error("LoadCA() should return error for invalid key data")
	}
	if !strings.Contains(err.Error(), "parse CA key") {
		t.Errorf("Error should mention key parse failure, got: %v", err)
	}
}

func TestLoadServerTLS_InvalidCAPEM(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game"

	// Generate and save valid server cert
	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	// Save server cert
	if err := saveCert(filepath.Join(tmpDir, "core.crt"), serverCert.Certificate); err != nil {
		t.Fatalf("saveCert() error = %v", err)
	}
	if err := saveKey(filepath.Join(tmpDir, "core.key"), serverCert.PrivateKey); err != nil {
		t.Fatalf("saveKey() error = %v", err)
	}

	// Write invalid CA (valid PEM but not a certificate)
	invalidCAPEM := pem.EncodeToMemory(&pem.Block{Type: "JUNK", Bytes: []byte("not a certificate")})
	if err := os.WriteFile(filepath.Join(tmpDir, "root-ca.crt"), invalidCAPEM, 0o600); err != nil {
		t.Fatalf("Failed to write invalid CA: %v", err)
	}

	_, err = LoadServerTLS(tmpDir, "core")
	if err == nil {
		t.Error("LoadServerTLS() should return error for invalid CA PEM")
	}
	if !strings.Contains(err.Error(), "failed to add CA certificate") {
		t.Errorf("Error should mention CA certificate pool failure, got: %v", err)
	}
}

func TestLoadClientTLS_InvalidCAPEM(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game"

	// Generate and save valid client cert
	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	clientCert, err := GenerateClientCert(ca, "gateway")
	if err != nil {
		t.Fatalf("GenerateClientCert() error = %v", err)
	}

	// Save client cert
	if err := saveCert(filepath.Join(tmpDir, "gateway.crt"), clientCert.Certificate); err != nil {
		t.Fatalf("saveCert() error = %v", err)
	}
	if err := saveKey(filepath.Join(tmpDir, "gateway.key"), clientCert.PrivateKey); err != nil {
		t.Fatalf("saveKey() error = %v", err)
	}

	// Write invalid CA (valid PEM but not a certificate)
	invalidCAPEM := pem.EncodeToMemory(&pem.Block{Type: "JUNK", Bytes: []byte("not a certificate")})
	if err := os.WriteFile(filepath.Join(tmpDir, "root-ca.crt"), invalidCAPEM, 0o600); err != nil {
		t.Fatalf("Failed to write invalid CA: %v", err)
	}

	_, err = LoadClientTLS(tmpDir, "gateway", gameID)
	if err == nil {
		t.Error("LoadClientTLS() should return error for invalid CA PEM")
	}
	if !strings.Contains(err.Error(), "failed to add CA certificate") {
		t.Errorf("Error should mention CA certificate pool failure, got: %v", err)
	}
}

func TestLoadServerTLS_MissingCAFile(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game"

	// Generate and save valid server cert
	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	// Save server cert but NOT CA
	if err := saveCert(filepath.Join(tmpDir, "core.crt"), serverCert.Certificate); err != nil {
		t.Fatalf("saveCert() error = %v", err)
	}
	if err := saveKey(filepath.Join(tmpDir, "core.key"), serverCert.PrivateKey); err != nil {
		t.Fatalf("saveKey() error = %v", err)
	}

	_, err = LoadServerTLS(tmpDir, "core")
	if err == nil {
		t.Error("LoadServerTLS() should return error for missing CA file")
	}
	if !strings.Contains(err.Error(), "read CA certificate") {
		t.Errorf("Error should mention reading CA certificate, got: %v", err)
	}
}

func TestLoadClientTLS_MissingCAFile(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game"

	// Generate and save valid client cert
	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	clientCert, err := GenerateClientCert(ca, "gateway")
	if err != nil {
		t.Fatalf("GenerateClientCert() error = %v", err)
	}

	// Save client cert but NOT CA
	if err := saveCert(filepath.Join(tmpDir, "gateway.crt"), clientCert.Certificate); err != nil {
		t.Fatalf("saveCert() error = %v", err)
	}
	if err := saveKey(filepath.Join(tmpDir, "gateway.key"), clientCert.PrivateKey); err != nil {
		t.Fatalf("saveKey() error = %v", err)
	}

	_, err = LoadClientTLS(tmpDir, "gateway", gameID)
	if err == nil {
		t.Error("LoadClientTLS() should return error for missing CA file")
	}
	if !strings.Contains(err.Error(), "read CA certificate") {
		t.Errorf("Error should mention reading CA certificate, got: %v", err)
	}
}

func TestValidateCertificateChain_NilCert(t *testing.T) {
	gameID := "test-game"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	err = ValidateCertificateChain(nil, ca.Certificate)
	if err == nil {
		t.Error("ValidateCertificateChain() should return error for nil cert")
	}
	if !strings.Contains(err.Error(), "certificate is nil") {
		t.Errorf("Error should mention nil certificate, got: %v", err)
	}
}

func TestValidateCertificateChain_NilCA(t *testing.T) {
	gameID := "test-game"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	err = ValidateCertificateChain(serverCert.Certificate, nil)
	if err == nil {
		t.Error("ValidateCertificateChain() should return error for nil CA")
	}
	if !strings.Contains(err.Error(), "CA certificate is nil") {
		t.Errorf("Error should mention nil CA certificate, got: %v", err)
	}
}

func TestValidateHostname_NilCert(t *testing.T) {
	err := ValidateHostname(nil, "localhost")
	if err == nil {
		t.Error("ValidateHostname() should return error for nil cert")
	}
	if !strings.Contains(err.Error(), "certificate is nil") {
		t.Errorf("Error should mention nil certificate, got: %v", err)
	}
}

func TestValidateHostname_IPAddress(t *testing.T) {
	gameID := "test-game"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	// Test with valid IP (127.0.0.1 is in the cert's IPAddresses)
	err = ValidateHostname(serverCert.Certificate, "127.0.0.1")
	if err != nil {
		t.Errorf("ValidateHostname() should accept 127.0.0.1, got: %v", err)
	}

	// Test with invalid IP
	err = ValidateHostname(serverCert.Certificate, "192.168.1.1")
	if err == nil {
		t.Error("ValidateHostname() should reject 192.168.1.1")
	}
}

func TestValidateExtKeyUsage_NilCert(t *testing.T) {
	err := ValidateExtKeyUsage(nil, x509.ExtKeyUsageServerAuth)
	if err == nil {
		t.Error("ValidateExtKeyUsage() should return error for nil cert")
	}
	if !strings.Contains(err.Error(), "certificate is nil") {
		t.Errorf("Error should mention nil certificate, got: %v", err)
	}
}

func TestCheckCertificateExpiration_NotYetValid(t *testing.T) {
	gameID := "test-game"

	ca, err := GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	// Generate a certificate that is not yet valid
	cert, err := generateNotYetValidCert(ca, gameID, "future")
	if err != nil {
		t.Fatalf("generateNotYetValidCert() error = %v", err)
	}

	status := CheckCertificateExpiration(cert.Certificate, 30*24*time.Hour)
	if status.Error == nil {
		t.Error("Expected error for not-yet-valid certificate")
	}
	if !strings.Contains(status.Error.Error(), "not yet valid") {
		t.Errorf("Error should mention 'not yet valid', got: %v", status.Error)
	}
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
