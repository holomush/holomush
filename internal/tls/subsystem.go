// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package tlscerts

import (
	"context"
	cryptotls "crypto/tls"
	"log/slog"
	"os"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/xdg"
)

// TLSSubsystemConfig configures the TLS subsystem.
type TLSSubsystemConfig struct {
	// CertsDir is the directory holding (or receiving) generated certificates.
	// It does not depend on any live resource, so it is a plain string.
	CertsDir string

	// GameID resolves the game ID at Start time. The gameID is embedded in the
	// CA certificate on first boot, and is only available once the database
	// subsystem has run InitGameID — hence a provider, not a live value.
	// Do NOT type this as an interface implemented by the database subsystem:
	// passing the subsystem directly would lose the cfg.GameID override
	// precedence that lives inside the closure the caller supplies here.
	GameID func() string

	// CertEnsurer generates or loads the TLS certificates for certsDir/gameID.
	// Default (assigned by the caller): EnsureCerts. Exposed as a field so the
	// existing deps.TLSCertEnsurer test seam continues to override cert
	// generation end to end.
	CertEnsurer func(certsDir, gameID string) (*cryptotls.Config, error)
}

// TLSSubsystem generates/loads TLS certificates for the core gRPC server. It
// wraps EnsureCerts (formerly cmd/holomush's inline ensureTLSCerts) as a
// registered lifecycle.Subsystem so certificate generation participates in
// the orchestrator's dependency graph instead of running as a hand-sequenced
// pre-start.
type TLSSubsystem struct {
	cfg       TLSSubsystemConfig
	tlsConfig *cryptotls.Config
}

// NewTLSSubsystem creates a TLSSubsystem using the provided TLSSubsystemConfig.
// It does not allocate or start any runtime resources; call Start to generate
// or load the certificates.
func NewTLSSubsystem(cfg TLSSubsystemConfig) *TLSSubsystem {
	return &TLSSubsystem{cfg: cfg}
}

// ID returns SubsystemTLS.
func (s *TLSSubsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemTLS }

// DependsOn returns [SubsystemDatabase] — the gameID embedded in the CA
// certificate on first boot is resolved from the database subsystem.
func (s *TLSSubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}
}

// Start resolves the gameID from the provider and ensures the TLS
// certificates exist, generating them on first boot.
// codecov:ignore — tested by integration and E2E tests
func (s *TLSSubsystem) Start(ctx context.Context) error {
	gameID := s.cfg.GameID()

	ensurer := s.cfg.CertEnsurer
	if ensurer == nil {
		ensurer = EnsureCerts
	}

	tlsConfig, err := ensurer(s.cfg.CertsDir, gameID)
	if err != nil {
		return oops.Code("TLS_SETUP_FAILED").With("operation", "set up TLS").With("certs_dir", s.cfg.CertsDir).Wrap(err)
	}
	s.tlsConfig = tlsConfig

	slog.InfoContext(ctx, "TLS certificates ready", "certs_dir", s.cfg.CertsDir)
	return nil
}

// Stop is a no-op — TLS certificate generation owns no runtime resource.
// codecov:ignore — tested by integration and E2E tests
func (s *TLSSubsystem) Stop(_ context.Context) error { return nil }

// TLSConfig returns the loaded server TLS configuration. Panics if called
// before Start().
func (s *TLSSubsystem) TLSConfig() *cryptotls.Config {
	if s.tlsConfig == nil {
		panic("tlscerts: TLSConfig() called before Start()")
	}
	return s.tlsConfig
}

// EnsureCerts ensures server and CA/client TLS certificates exist for the
// core component and returns a loaded server TLS configuration.
//
// If any of the expected files (`core.crt`, `core.key`, `root-ca.crt`) are
// already present in certsDir, the existing server TLS configuration is
// loaded and returned. Otherwise the function creates certsDir, generates a
// CA and server certificate for `core`, generates a gateway client
// certificate, saves all artifacts, and then loads and returns the resulting
// server TLS configuration. Returns a coded error if any step (directory
// creation, certificate generation, saving, or loading) fails.
//
// EnsureCerts is the same signature cmd/holomush's CoreDeps.TLSCertEnsurer
// field declares, so that live test seam is preserved verbatim by this move
// (relocated from cmd/holomush's ensureTLSCerts).
func EnsureCerts(certsDir, gameID string) (*cryptotls.Config, error) {
	certPath := certsDir + "/core.crt"
	keyPath := certsDir + "/core.key"
	caPath := certsDir + "/root-ca.crt"

	certExists := fileExists(certPath)
	keyExists := fileExists(keyPath)
	caExists := fileExists(caPath)

	if certExists || keyExists || caExists {
		existingConfig, err := LoadServerTLS(certsDir, "core")
		if err != nil {
			return nil, oops.Code("TLS_LOAD_FAILED").With("operation", "load existing TLS certificates").With("certs_dir", certsDir).Wrap(err)
		}
		return existingConfig, nil
	}

	slog.Info("generating TLS certificates", "certs_dir", certsDir)

	if err := xdg.EnsureDir(certsDir); err != nil {
		return nil, oops.Code("CERTS_DIR_CREATE_FAILED").With("operation", "create certs directory").With("certs_dir", certsDir).Wrap(err)
	}

	ca, err := GenerateCA(gameID)
	if err != nil {
		return nil, oops.Code("CA_GENERATE_FAILED").With("operation", "generate CA").With("game_id", gameID).Wrap(err)
	}

	serverCert, err := GenerateServerCert(ca, gameID, "core")
	if err != nil {
		return nil, oops.Code("SERVER_CERT_GENERATE_FAILED").With("operation", "generate server certificate").With("component", "core").Wrap(err)
	}

	err = SaveCertificates(certsDir, ca, serverCert)
	if err != nil {
		return nil, oops.Code("CERTS_SAVE_FAILED").With("operation", "save certificates").With("certs_dir", certsDir).Wrap(err)
	}

	gatewayCert, err := GenerateClientCert(ca, "gateway")
	if err != nil {
		return nil, oops.Code("CLIENT_CERT_GENERATE_FAILED").With("operation", "generate gateway certificate").With("component", "gateway").Wrap(err)
	}

	err = SaveClientCert(certsDir, gatewayCert)
	if err != nil {
		return nil, oops.Code("CLIENT_CERT_SAVE_FAILED").With("operation", "save gateway certificate").With("component", "gateway").Wrap(err)
	}

	slog.Info("TLS certificates generated")

	tlsConfig, err := LoadServerTLS(certsDir, "core")
	if err != nil {
		return nil, oops.Code("TLS_LOAD_FAILED").With("operation", "load generated certificates").With("certs_dir", certsDir).Wrap(err)
	}
	return tlsConfig, nil
}

// fileExists reports whether the file at path exists or should be treated as
// existing. Permission errors are treated as "exists" to avoid silently
// overwriting files we can't read.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}
