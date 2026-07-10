// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"github.com/nats-io/nats.go"
	"github.com/samber/oops"
)

// externalClientName identifies the host connection on an external NATS
// cluster. It appears in the cluster's connz/monitoring output so operators
// can attribute the connection to the HoloMUSH server account.
const externalClientName = "holomush-server"

// Dial opens a NATS connection to the external cluster described by cfg
// (Mode == ModeExternal), applying the same creds-file / TLS options the
// server uses at boot (D-04). It is exported for operator-host CLIs (e.g.
// the audit DLQ replay tool) that need a broker handle without standing up
// the full EventBus subsystem. Callers own the returned *nats.Conn and MUST
// Close it.
func Dial(cfg Config) (*nats.Conn, error) {
	return dialExternal(cfg)
}

// dialExternal opens a connection to an external NATS cluster addressed by
// cfg.URL (Mode == ModeExternal). Authentication follows D-04: a NATS .creds
// file (JWT/NKey decentralized auth) when cfg.Credentials is set, plus an
// optional TLS block (private CA via RootCAs and/or a client certificate for
// mTLS) when cfg.TLS is populated. User:pass-in-URL works implicitly for dev
// clusters because the URL passes straight to nats.go.
//
// Fail-closed (D-02): a dial failure is wrapped as
// EVENTBUS_EXTERNAL_CONNECT_FAILED and returned so Start refuses to boot — the
// orchestrator (compose restart policy / k8s) owns retry. There is no embedded
// fallback. nats.go's built-in reconnect handles transient drops of an
// already-established connection, so no RetryOnFailedConnect is set here: at
// boot we want an immediate, coded failure rather than a silent retry loop.
func dialExternal(cfg Config) (*nats.Conn, error) {
	opts := []nats.Option{nats.Name(externalClientName)}
	if cfg.Credentials != "" {
		opts = append(opts, nats.UserCredentials(cfg.Credentials))
	}
	if cfg.TLS.CA != "" {
		opts = append(opts, nats.RootCAs(cfg.TLS.CA))
	}
	// A client certificate requires both halves; nats.ClientCert loads the
	// pair. Populate it when either path is set so a half-configured TLS block
	// surfaces a clear load error rather than being silently ignored.
	if cfg.TLS.Cert != "" || cfg.TLS.Key != "" {
		opts = append(opts, nats.ClientCert(cfg.TLS.Cert, cfg.TLS.Key))
	}

	conn, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, oops.Code("EVENTBUS_EXTERNAL_CONNECT_FAILED").
			With("url", cfg.URL).
			Wrap(err)
	}
	return conn, nil
}
