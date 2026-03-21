// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	cryptotls "crypto/tls"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/samber/oops"
)

// certPollInterval is the interval between cert availability checks.
const certPollInterval = 500 * time.Millisecond

// defaultCertPollTimeout is the maximum time to wait for TLS certs to become available.
const defaultCertPollTimeout = 30 * time.Second

// gatewayTLSConfig holds the results of loading all gateway TLS certificates.
type gatewayTLSConfig struct {
	gameID     string
	clientTLS  *cryptotls.Config
	controlTLS *cryptotls.Config
}

// isTransientCertError returns true if the error is a transient file-not-found
// error that may resolve when cert files are created by the core process.
func isTransientCertError(err error) bool {
	return err != nil && errors.Is(err, os.ErrNotExist)
}

// tryLoadCerts attempts to load all gateway TLS certificates in one pass.
func tryLoadCerts(deps *GatewayDeps, certsDir, component string) (*gatewayTLSConfig, error) {
	gameID, err := deps.GameIDExtractor(certsDir)
	if err != nil {
		return nil, oops.Code("GAME_ID_EXTRACT_FAILED").
			With("operation", "extract game_id from CA").
			With("certs_dir", certsDir).
			Wrap(err)
	}

	clientTLS, err := deps.ClientTLSLoader(certsDir, component, gameID)
	if err != nil {
		return nil, oops.Code("TLS_LOAD_FAILED").
			With("operation", "load client TLS certificates").
			With("component", component).
			With("certs_dir", certsDir).
			Wrap(err)
	}

	controlTLS, err := deps.ControlTLSLoader(certsDir, component)
	if err != nil {
		return nil, oops.Code("CONTROL_TLS_FAILED").
			With("operation", "load control TLS config").
			With("component", component).
			With("certs_dir", certsDir).
			Wrap(err)
	}

	return &gatewayTLSConfig{
		gameID:     gameID,
		clientTLS:  clientTLS,
		controlTLS: controlTLS,
	}, nil
}

// waitForTLSCerts polls for TLS certificate availability with the given timeout.
// It retries on file-not-found errors (transient) but fails immediately on
// permanent errors like invalid certificate format. This allows the gateway
// to start before core has generated TLS certificates.
//
//nolint:unparam // component is always "gateway" today but the parameter keeps the function general
func waitForTLSCerts(ctx context.Context, deps *GatewayDeps, certsDir, component string, timeout time.Duration) (*gatewayTLSConfig, error) {
	// Try immediately — no waiting if certs are already available.
	result, err := tryLoadCerts(deps, certsDir, component)
	if err == nil {
		return result, nil
	}
	if !isTransientCertError(err) {
		return nil, err
	}

	// Certs not found — poll until available or timeout.
	slog.Info("waiting for TLS certificates", "certs_dir", certsDir, "timeout", timeout)

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	ticker := time.NewTicker(certPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, oops.
				Code("CERT_POLL_TIMEOUT").
				With("timeout", timeout).
				With("certs_dir", certsDir).
				Wrapf(err, "timed out waiting for TLS certificates")
		case <-ticker.C:
			result, err = tryLoadCerts(deps, certsDir, component)
			if err == nil {
				slog.Info("TLS certificates loaded", "certs_dir", certsDir)
				return result, nil
			}
			if !isTransientCertError(err) {
				return nil, err
			}
			slog.Debug("still waiting for TLS certificates", "certs_dir", certsDir, "error", err)
		}
	}
}
