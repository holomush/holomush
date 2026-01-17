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
		return nil, fmt.Errorf("failed to generate CA key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial: %w", err)
	}

	// Create URI SAN for game_id
	gameURI, err := url.Parse("holomush://game/" + gameID)
	if err != nil {
		return nil, fmt.Errorf("failed to create game URI: %w", err)
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
		return nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	return &CA{Certificate: cert, PrivateKey: key}, nil
}

// GenerateServerCert creates a server certificate signed by the CA.
// The serverName is used for the certificate file naming (e.g., "core").
// The game_id is included in SAN DNS names as "holomush-{game_id}".
func GenerateServerCert(ca *CA, gameID, serverName string) (*ServerCert, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate server key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial: %w", err)
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
		return nil, fmt.Errorf("failed to create server certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse server certificate: %w", err)
	}

	return &ServerCert{Certificate: cert, PrivateKey: key, Name: serverName}, nil
}

// SaveCertificates saves the CA and optionally a server certificate to the certs directory.
// CA is saved as root-ca.crt and root-ca.key.
// Server certificate is saved as {name}.crt and {name}.key.
func SaveCertificates(certsDir string, ca *CA, serverCert *ServerCert) error {
	if err := os.MkdirAll(certsDir, 0o700); err != nil {
		return fmt.Errorf("failed to create certs directory: %w", err)
	}

	// Save CA
	if err := saveCert(filepath.Join(certsDir, "root-ca.crt"), ca.Certificate); err != nil {
		return fmt.Errorf("failed to save CA certificate: %w", err)
	}
	if err := saveKey(filepath.Join(certsDir, "root-ca.key"), ca.PrivateKey); err != nil {
		return fmt.Errorf("failed to save CA key: %w", err)
	}

	// Save server certificate if provided
	if serverCert != nil {
		certFile := serverCert.Name + ".crt"
		keyFile := serverCert.Name + ".key"
		if err := saveCert(filepath.Join(certsDir, certFile), serverCert.Certificate); err != nil {
			return fmt.Errorf("failed to save server certificate: %w", err)
		}
		if err := saveKey(filepath.Join(certsDir, keyFile), serverCert.PrivateKey); err != nil {
			return fmt.Errorf("failed to save server key: %w", err)
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
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	// Read key
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA key: %w", err)
	}

	// Parse certificate
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode CA certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	// Parse key
	block, _ = pem.Decode(keyPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode CA key PEM")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA key: %w", err)
	}

	return &CA{Certificate: cert, PrivateKey: key}, nil
}

// saveCert saves a certificate to a PEM file.
func saveCert(path string, cert *x509.Certificate) error {
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create cert file: %w", err)
	}

	if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to encode certificate: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close cert file: %w", err)
	}

	return nil
}

// saveKey saves an ECDSA private key to a PEM file.
func saveKey(path string, key *ecdsa.PrivateKey) error {
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("failed to marshal key: %w", err)
	}

	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create key file: %w", err)
	}

	if err := pem.Encode(f, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to encode key: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close key file: %w", err)
	}

	return nil
}

// GenerateClientCert creates a client certificate signed by the CA.
// The clientName is used for the certificate file naming (e.g., "gateway").
func GenerateClientCert(ca *CA, clientName string) (*ClientCert, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate client key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial: %w", err)
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
		return nil, fmt.Errorf("failed to create client certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse client certificate: %w", err)
	}

	return &ClientCert{Certificate: cert, PrivateKey: key, Name: clientName}, nil
}

// SaveClientCert saves a client certificate to the certs directory.
// Client certificate is saved as {name}.crt and {name}.key.
func SaveClientCert(certsDir string, clientCert *ClientCert) error {
	if err := os.MkdirAll(certsDir, 0o700); err != nil {
		return fmt.Errorf("failed to create certs directory: %w", err)
	}

	certFile := clientCert.Name + ".crt"
	keyFile := clientCert.Name + ".key"
	if err := saveCert(filepath.Join(certsDir, certFile), clientCert.Certificate); err != nil {
		return fmt.Errorf("failed to save client certificate: %w", err)
	}
	if err := saveKey(filepath.Join(certsDir, keyFile), clientCert.PrivateKey); err != nil {
		return fmt.Errorf("failed to save client key: %w", err)
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
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	caCert, err := os.ReadFile(filepath.Clean(filepath.Join(certsDir, "root-ca.crt")))
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to add CA certificate to pool")
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
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	caCert, err := os.ReadFile(filepath.Clean(filepath.Join(certsDir, "root-ca.crt")))
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to add CA certificate to pool")
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
		status.Error = fmt.Errorf("certificate expired on %s (%.0f hours ago)",
			cert.NotAfter.Format(time.RFC3339),
			now.Sub(cert.NotAfter).Hours())
		return status
	}

	// Check if not yet valid
	if now.Before(cert.NotBefore) {
		status.Error = fmt.Errorf("certificate not yet valid, becomes valid on %s",
			cert.NotBefore.Format(time.RFC3339))
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
		return fmt.Errorf("certificate is nil")
	}
	if ca == nil {
		return fmt.Errorf("CA certificate is nil")
	}

	// Check if the cert was signed by the CA
	if err := cert.CheckSignatureFrom(ca); err != nil {
		return fmt.Errorf("certificate signature verification failed: %w", err)
	}

	return nil
}

// ValidateHostname validates that the certificate is valid for the given hostname.
func ValidateHostname(cert *x509.Certificate, hostname string) error {
	if cert == nil {
		return fmt.Errorf("certificate is nil")
	}
	if hostname == "" {
		return fmt.Errorf("hostname is empty")
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

	return fmt.Errorf("hostname %q does not match certificate: DNSNames=%v, CN=%s",
		hostname, cert.DNSNames, cert.Subject.CommonName)
}

// ValidateExtKeyUsage validates that the certificate has the required extended key usage.
func ValidateExtKeyUsage(cert *x509.Certificate, requiredUsage x509.ExtKeyUsage) error {
	if cert == nil {
		return fmt.Errorf("certificate is nil")
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

	return fmt.Errorf("certificate does not have required ExtKeyUsage %s", usageName)
}
