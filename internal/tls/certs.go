// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package tls provides TLS certificate generation and loading for HoloMUSH.
package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	cryptotls "crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/samber/oops"
)

// CA holds a certificate authority certificate and private key.
type CA struct {
	Certificate *x509.Certificate
	PrivateKey  *ecdsa.PrivateKey
}

// ServerCert holds a server certificate and private key.
type ServerCert struct {
	Certificate *x509.Certificate
	PrivateKey  *ecdsa.PrivateKey
	Name        string
}

// ClientCert holds a client certificate and private key.
type ClientCert struct {
	Certificate *x509.Certificate
	PrivateKey  *ecdsa.PrivateKey
	Name        string
}

// GenerateCA creates a new root CA with the game_id embedded in CN and SAN.
// The game_id is included in:
//   - CN (Common Name): "HoloMUSH CA {game_id}"
//   - SAN (Subject Alternative Name) as URI: holomush://game/{game_id}
func GenerateCA(gameID string) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, oops.With("operation", "generate CA key").Wrap(err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, oops.With("operation", "generate serial").Wrap(err)
	}

	// Create URI SAN for game_id
	gameURI, err := url.Parse("holomush://game/" + gameID)
	if err != nil {
		return nil, oops.With("operation", "create game URI", "game_id", gameID).Wrap(err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"HoloMUSH"},
			CommonName:   "HoloMUSH CA " + gameID,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0), // 10 years
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		URIs:                  []*url.URL{gameURI},
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, oops.With("operation", "create CA certificate").Wrap(err)
	}

	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, oops.With("operation", "parse CA certificate").Wrap(err)
	}

	return &CA{Certificate: cert, PrivateKey: key}, nil
}

// GenerateServerCert creates a server certificate signed by the CA.
// The serverName is used for the certificate file naming (e.g., "core").
// The game_id is included in SAN DNS names as "holomush-{game_id}".
func GenerateServerCert(ca *CA, gameID, serverName string) (*ServerCert, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, oops.With("operation", "generate server key", "server_name", serverName).Wrap(err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, oops.With("operation", "generate serial", "server_name", serverName).Wrap(err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"HoloMUSH"},
			CommonName:   "holomush-" + serverName,
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(1, 0, 0), // 1 year
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost", "holomush-" + gameID},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, ca.Certificate, &key.PublicKey, ca.PrivateKey)
	if err != nil {
		return nil, oops.With("operation", "create server certificate", "server_name", serverName).Wrap(err)
	}

	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, oops.With("operation", "parse server certificate", "server_name", serverName).Wrap(err)
	}

	return &ServerCert{Certificate: cert, PrivateKey: key, Name: serverName}, nil
}

// SaveCertificates saves the CA and optionally a server certificate to the certs directory.
// CA is saved as root-ca.crt and root-ca.key.
// Server certificate is saved as {name}.crt and {name}.key.
func SaveCertificates(certsDir string, ca *CA, serverCert *ServerCert) error {
	if err := os.MkdirAll(certsDir, 0o700); err != nil {
		return oops.With("operation", "create certs directory", "path", certsDir).Wrap(err)
	}

	// Save CA
	if err := saveCert(filepath.Join(certsDir, "root-ca.crt"), ca.Certificate); err != nil {
		return oops.With("operation", "save CA certificate").Wrap(err)
	}
	if err := saveKey(filepath.Join(certsDir, "root-ca.key"), ca.PrivateKey); err != nil {
		return oops.With("operation", "save CA key").Wrap(err)
	}

	// Save server certificate if provided
	if serverCert != nil {
		certFile := serverCert.Name + ".crt"
		keyFile := serverCert.Name + ".key"
		if err := saveCert(filepath.Join(certsDir, certFile), serverCert.Certificate); err != nil {
			return oops.With("operation", "save server certificate", "server_name", serverCert.Name).Wrap(err)
		}
		if err := saveKey(filepath.Join(certsDir, keyFile), serverCert.PrivateKey); err != nil {
			return oops.With("operation", "save server key", "server_name", serverCert.Name).Wrap(err)
		}
	}

	return nil
}

// LoadCA loads an existing CA from the certs directory.
// Returns an error if the CA files don't exist or can't be parsed.
func LoadCA(certsDir string) (*CA, error) {
	certPath := filepath.Clean(filepath.Join(certsDir, "root-ca.crt"))
	keyPath := filepath.Clean(filepath.Join(certsDir, "root-ca.key"))

	// Read certificate
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, oops.With("operation", "read CA certificate", "path", certPath).Wrap(err)
	}

	// Read key
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, oops.With("operation", "read CA key", "path", keyPath).Wrap(err)
	}

	// Parse certificate
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, oops.With("path", certPath).Errorf("failed to decode CA certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, oops.With("operation", "parse CA certificate", "path", certPath).Wrap(err)
	}

	// Parse key
	block, _ = pem.Decode(keyPEM)
	if block == nil {
		return nil, oops.With("path", keyPath).Errorf("failed to decode CA key PEM")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, oops.With("operation", "parse CA key", "path", keyPath).Wrap(err)
	}

	return &CA{Certificate: cert, PrivateKey: key}, nil
}

// saveCert saves a certificate to a PEM file.
func saveCert(path string, cert *x509.Certificate) error {
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return oops.With("operation", "create cert file", "path", path).Wrap(err)
	}

	if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}); err != nil {
		_ = f.Close()
		return oops.With("operation", "encode certificate", "path", path).Wrap(err)
	}

	if err := f.Close(); err != nil {
		return oops.With("operation", "close cert file", "path", path).Wrap(err)
	}

	return nil
}

// saveKey saves an ECDSA private key to a PEM file.
func saveKey(path string, key *ecdsa.PrivateKey) error {
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return oops.With("operation", "marshal key", "path", path).Wrap(err)
	}

	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return oops.With("operation", "create key file", "path", path).Wrap(err)
	}

	if err := pem.Encode(f, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		_ = f.Close()
		return oops.With("operation", "encode key", "path", path).Wrap(err)
	}

	if err := f.Close(); err != nil {
		return oops.With("operation", "close key file", "path", path).Wrap(err)
	}

	return nil
}

// GenerateClientCert creates a client certificate signed by the CA.
// The clientName is used for the certificate file naming (e.g., "gateway").
func GenerateClientCert(ca *CA, clientName string) (*ClientCert, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, oops.With("operation", "generate client key", "client_name", clientName).Wrap(err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, oops.With("operation", "generate serial", "client_name", clientName).Wrap(err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"HoloMUSH"},
			CommonName:   "holomush-" + clientName,
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(1, 0, 0), // 1 year
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, ca.Certificate, &key.PublicKey, ca.PrivateKey)
	if err != nil {
		return nil, oops.With("operation", "create client certificate", "client_name", clientName).Wrap(err)
	}

	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, oops.With("operation", "parse client certificate", "client_name", clientName).Wrap(err)
	}

	return &ClientCert{Certificate: cert, PrivateKey: key, Name: clientName}, nil
}

// SaveClientCert saves a client certificate to the certs directory.
// Client certificate is saved as {name}.crt and {name}.key.
func SaveClientCert(certsDir string, clientCert *ClientCert) error {
	if err := os.MkdirAll(certsDir, 0o700); err != nil {
		return oops.With("operation", "create certs directory", "path", certsDir).Wrap(err)
	}

	certFile := clientCert.Name + ".crt"
	keyFile := clientCert.Name + ".key"
	if err := saveCert(filepath.Join(certsDir, certFile), clientCert.Certificate); err != nil {
		return oops.With("operation", "save client certificate", "client_name", clientCert.Name).Wrap(err)
	}
	if err := saveKey(filepath.Join(certsDir, keyFile), clientCert.PrivateKey); err != nil {
		return oops.With("operation", "save client key", "client_name", clientCert.Name).Wrap(err)
	}

	return nil
}

// LoadServerTLS loads TLS config for the Core gRPC server with mTLS.
// Requires server cert and CA for client verification.
func LoadServerTLS(certsDir string, serverName string) (*cryptotls.Config, error) {
	cert, err := cryptotls.LoadX509KeyPair(
		filepath.Join(certsDir, serverName+".crt"),
		filepath.Join(certsDir, serverName+".key"),
	)
	if err != nil {
		return nil, oops.With("operation", "load server certificate", "server_name", serverName).Wrap(err)
	}

	caCert, err := os.ReadFile(filepath.Clean(filepath.Join(certsDir, "root-ca.crt")))
	if err != nil {
		return nil, oops.With("operation", "read CA certificate", "path", certsDir).Wrap(err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, oops.With("path", certsDir).Errorf("failed to add CA certificate to pool")
	}

	return &cryptotls.Config{
		Certificates: []cryptotls.Certificate{cert},
		ClientCAs:    caPool,
		ClientAuth:   cryptotls.RequireAndVerifyClientCert,
		MinVersion:   cryptotls.VersionTLS13,
	}, nil
}

// LoadClientTLS loads TLS config for the Gateway gRPC client with mTLS.
// Requires client cert and CA for server verification.
// The expectedGameID is used to set ServerName for cert validation against the server's SAN.
func LoadClientTLS(certsDir string, clientName string, expectedGameID string) (*cryptotls.Config, error) {
	cert, err := cryptotls.LoadX509KeyPair(
		filepath.Join(certsDir, clientName+".crt"),
		filepath.Join(certsDir, clientName+".key"),
	)
	if err != nil {
		return nil, oops.With("operation", "load client certificate", "client_name", clientName).Wrap(err)
	}

	caCert, err := os.ReadFile(filepath.Clean(filepath.Join(certsDir, "root-ca.crt")))
	if err != nil {
		return nil, oops.With("operation", "read CA certificate", "path", certsDir).Wrap(err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, oops.With("path", certsDir).Errorf("failed to add CA certificate to pool")
	}

	return &cryptotls.Config{
		Certificates: []cryptotls.Certificate{cert},
		RootCAs:      caPool,
		ServerName:   "holomush-" + expectedGameID,
		MinVersion:   cryptotls.VersionTLS13,
	}, nil
}

// ExpirationStatus holds information about certificate expiration.
type ExpirationStatus struct {
	IsExpired           bool
	NearExpiration      bool
	DaysUntilExpiration int
	Warning             string
	Error               error
}

// CheckCertificateExpiration checks if a certificate is expired or near expiration.
// The warningThreshold specifies how far in advance to warn about expiration.
func CheckCertificateExpiration(cert *x509.Certificate, warningThreshold time.Duration) ExpirationStatus {
	now := time.Now()
	status := ExpirationStatus{}

	// Calculate days until expiration
	daysUntil := int(cert.NotAfter.Sub(now).Hours() / 24)
	status.DaysUntilExpiration = daysUntil

	// Check if expired
	if now.After(cert.NotAfter) {
		status.IsExpired = true
		status.Error = oops.With("expired_on", cert.NotAfter.Format(time.RFC3339), "hours_ago", now.Sub(cert.NotAfter).Hours()).
			Errorf("certificate expired")
		return status
	}

	// Check if not yet valid
	if now.Before(cert.NotBefore) {
		status.Error = oops.With("valid_from", cert.NotBefore.Format(time.RFC3339)).
			Errorf("certificate not yet valid")
		return status
	}

	// Check if near expiration
	if cert.NotAfter.Sub(now) <= warningThreshold {
		status.NearExpiration = true
		status.Warning = fmt.Sprintf("certificate expires in %d days on %s",
			daysUntil, cert.NotAfter.Format(time.RFC3339))
	}

	return status
}

// ValidateCertificateChain validates that a certificate was signed by the given CA.
func ValidateCertificateChain(cert *x509.Certificate, ca *x509.Certificate) error {
	if cert == nil {
		return oops.Errorf("certificate is nil")
	}
	if ca == nil {
		return oops.Errorf("CA certificate is nil")
	}

	// Check if the cert was signed by the CA
	if err := cert.CheckSignatureFrom(ca); err != nil {
		return oops.With("operation", "verify certificate signature").Wrap(err)
	}

	return nil
}

// ValidateHostname validates that the certificate is valid for the given hostname.
func ValidateHostname(cert *x509.Certificate, hostname string) error {
	if cert == nil {
		return oops.Errorf("certificate is nil")
	}
	if hostname == "" {
		return oops.Errorf("hostname is empty")
	}

	// Check DNSNames
	for _, name := range cert.DNSNames {
		if name == hostname {
			return nil
		}
	}

	// Check IP addresses if hostname looks like an IP
	if ip := net.ParseIP(hostname); ip != nil {
		for _, certIP := range cert.IPAddresses {
			if certIP.Equal(ip) {
				return nil
			}
		}
	}

	// Check Common Name as fallback (deprecated but still used)
	if cert.Subject.CommonName == hostname {
		return nil
	}

	return oops.With("hostname", hostname, "dns_names", cert.DNSNames, "common_name", cert.Subject.CommonName).
		Errorf("hostname does not match certificate")
}

// ValidateExtKeyUsage validates that the certificate has the required extended key usage.
func ValidateExtKeyUsage(cert *x509.Certificate, requiredUsage x509.ExtKeyUsage) error {
	if cert == nil {
		return oops.Errorf("certificate is nil")
	}

	for _, usage := range cert.ExtKeyUsage {
		if usage == requiredUsage {
			return nil
		}
	}

	usageName := "unknown"
	switch requiredUsage {
	case x509.ExtKeyUsageServerAuth:
		usageName = "ServerAuth"
	case x509.ExtKeyUsageClientAuth:
		usageName = "ClientAuth"
	}

	return oops.With("required_usage", usageName).Errorf("certificate missing required ExtKeyUsage")
}
