// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

// TestDialExternalFailsClosedWhenUnreachable proves the fail-closed boot
// contract (D-02): dialing an unreachable external NATS URL returns a nil conn
// and the coded EVENTBUS_EXTERNAL_CONNECT_FAILED error rather than degrading to
// an embedded fallback. nats.Connect has no RetryOnFailedConnect here, so an
// initial dial to a closed port fails immediately.
func TestDialExternalFailsClosedWhenUnreachable(t *testing.T) {
	t.Parallel()
	// 127.0.0.1:1 is a reserved port that refuses connections immediately.
	conn, err := dialExternal(Config{Mode: ModeExternal, URL: "nats://127.0.0.1:1"})
	require.Error(t, err, "unreachable external NATS must fail closed")
	assert.Nil(t, conn, "no connection is returned on a failed dial")
	errutil.AssertErrorCode(t, err, "EVENTBUS_EXTERNAL_CONNECT_FAILED")
}

// TestExporterEnabledIsEmbeddedOnly locks OQ-7: the embedded-only Prometheus
// exporter never runs in external mode, where s.server is nil and scraping
// server.MonitorAddr() would nil-dereference. External mode with
// PrometheusExporter=true must report the exporter disabled.
func TestExporterEnabledIsEmbeddedOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"embedded with flag starts exporter", Config{Mode: ModeEmbedded, PrometheusExporter: true}, true},
		{"embedded without flag does not", Config{Mode: ModeEmbedded, PrometheusExporter: false}, false},
		{"external with flag is embedded-only guarded off", Config{Mode: ModeExternal, PrometheusExporter: true}, false},
		{"external without flag does not", Config{Mode: ModeExternal, PrometheusExporter: false}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &Subsystem{cfg: tt.cfg}
			assert.Equal(t, tt.want, s.exporterEnabled())
		})
	}
}
