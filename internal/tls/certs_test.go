// Package tls provides TLS certificate generation and loading for HoloMUSH.
package tls

import (
	"crypto/tls"
	"crypto/x509"
	"net/url"
	"os"
	"path/filepath"
	"testing"
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
