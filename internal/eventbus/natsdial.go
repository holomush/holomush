// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"net/url"
	"strings"

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
			With("url", redactURL(cfg.URL)).
			Wrap(err)
	}
	return conn, nil
}

// redactURL strips any userinfo (user:pass) from a NATS URL so a boot-time
// dial failure does not leak the URL-embedded password into structured
// errors/logs. Dev clusters may carry user:pass in cfg.URL (config.go);
// this keeps that credential out of EVENTBUS_EXTERNAL_CONNECT_FAILED.
//
// nats.Connect accepts a comma-separated seed list in a single string
// (nats://a:pw@h1,nats://b:pw2@h2), which url.Parse treats as ONE URL whose
// User is only the first credential — leaving the 2nd+ embedded passwords in
// the parsed host/path. Split on "," and redact each seed independently so no
// credential in any seed survives. If a seed cannot be parsed it is dropped
// entirely rather than risking a leak.
func redactURL(raw string) string {
	seeds := strings.Split(raw, ",")
	redacted := make([]string, len(seeds))
	for i, seed := range seeds {
		redacted[i] = redactOneURL(seed)
	}
	return strings.Join(redacted, ",")
}

// redactOneURL redacts a single NATS URL (no comma-separated seed list).
//
// url.Parse only recognizes userinfo (and lets Redacted() strip the password)
// when the seed carries a scheme. A scheme-less seed like "user:pass@host:4222"
// parses as Scheme="user"/Opaque="pass@host:4222" with User=nil, so Redacted()
// would leave the password intact. Config.Validate only rejects an empty URL,
// so such a malformed seed can reach here — prepend the default NATS scheme
// before parsing so the userinfo is recognized and redacted.
func redactOneURL(raw string) string {
	toParse := strings.TrimSpace(raw)
	if toParse != "" && !strings.Contains(toParse, "://") {
		toParse = "nats://" + toParse
	}
	u, err := url.Parse(toParse)
	if err != nil {
		return "<unparseable url redacted>"
	}
	if u.User != nil {
		u.User = nil
	}
	return u.Redacted()
}
