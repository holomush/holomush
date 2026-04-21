// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

// ackRecordingMsg augments stubMsg with an ack counter so handle-level tests
// can confirm Ack was (or wasn't) called on the delivery path.
type ackRecordingMsg struct {
	stubMsg
	ackCount int
	ackErr   error
}

func (a *ackRecordingMsg) Ack() error {
	a.ackCount++
	return a.ackErr
}

// TestPluginConsumerHandleAcksOnDispatchSuccess exercises the trivial handle
// wrapper: on dispatch success it MUST Ack the delivery so JS advances the
// cursor.
func TestPluginConsumerHandleAcksOnDispatchSuccess(t *testing.T) {
	t.Parallel()
	cli := &stubPluginAuditClient{}
	pc := &pluginConsumer{
		cfg: PluginConsumerConfig{PluginName: "core-scenes", Client: cli},
	}
	msg := &ackRecordingMsg{
		stubMsg: stubMsg{
			headers: validPluginHeaders(t),
			subject: "events.main.plugin.test",
			data:    newValidEnvelopeBytes(t),
		},
	}
	pc.handle(msg)
	assert.True(t, cli.called, "dispatch must reach the client")
	assert.Equal(t, 1, msg.ackCount, "successful dispatch acks")
}

// TestPluginConsumerHandleSkipsAckOnDispatchFailure exercises the error log
// branch. MUST NOT Ack so AckWait governs redelivery (§6 contract).
func TestPluginConsumerHandleSkipsAckOnDispatchFailure(t *testing.T) {
	t.Parallel()
	cli := &stubPluginAuditClient{err: errors.New("plugin RPC down")}
	pc := &pluginConsumer{
		cfg: PluginConsumerConfig{PluginName: "core-scenes", Client: cli},
	}
	msg := &ackRecordingMsg{
		stubMsg: stubMsg{
			headers: validPluginHeaders(t),
			subject: "events.main.plugin.test",
			data:    newValidEnvelopeBytes(t),
		},
	}
	pc.handle(msg)
	assert.True(t, cli.called)
	assert.Equal(t, 0, msg.ackCount, "failed dispatch must NOT ack — AckWait governs")
}

// TestPluginConsumerHandleSkipsAckOnDecodeFailure covers the decode-failed
// branch that dispatch returns before reaching the client.
func TestPluginConsumerHandleSkipsAckOnDecodeFailure(t *testing.T) {
	t.Parallel()
	cli := &stubPluginAuditClient{}
	pc := &pluginConsumer{
		cfg: PluginConsumerConfig{PluginName: "p", Client: cli},
	}
	h := validPluginHeaders(t)
	h.Del(headerCodec)
	msg := &ackRecordingMsg{
		stubMsg: stubMsg{
			headers: h,
			subject: "events.main.p.bad",
			data:    newValidEnvelopeBytes(t),
		},
	}
	pc.handle(msg)
	assert.False(t, cli.called, "decode failure never reaches client")
	assert.Equal(t, 0, msg.ackCount)
}

// TestPluginConsumerStopUnstartedIsNoop covers the !started branch of Stop.
func TestPluginConsumerStopUnstartedIsNoop(t *testing.T) {
	t.Parallel()
	m := NewPluginConsumerManager(nil)
	require.NoError(t, m.Stop(context.Background()))
}

// TestPluginConsumerStartTwiceIsIdempotent covers the "already started" Start
// guard (second call returns nil without re-entering Consume).
func TestPluginConsumerStartTwiceIsIdempotent(t *testing.T) {
	t.Parallel()
	m := NewPluginConsumerManager(nil)
	// Zero consumers → Start succeeds. Second Start hits the already-started
	// guard and returns nil without touching anything.
	require.NoError(t, m.Start(context.Background()))
	require.NoError(t, m.Start(context.Background()))
}

// TestDispatchUsesBackgroundWhenWorkerCtxNil covers the nil-workerCtx branch
// in dispatch where the code falls back to context.Background.
func TestDispatchUsesBackgroundWhenWorkerCtxNil(t *testing.T) {
	t.Parallel()
	cli := &stubPluginAuditClient{}
	pc := &pluginConsumer{
		cfg:       PluginConsumerConfig{PluginName: "p", Client: cli},
		workerCtx: nil, // forces background fallback
	}
	msg := &stubMsg{
		headers: validPluginHeaders(t),
		subject: "events.main.p.bg",
		data:    newValidEnvelopeBytes(t),
	}
	require.NoError(t, pc.dispatch(msg))
	assert.True(t, cli.called)
}

// TestDecodeEnvelopeRejectsUnknownCodec covers the codec.Resolve failure
// branch of decodeEnvelope. The codec passes identity-check but a bogus
// name still reaches Resolve when we flip the identity check by forging
// a header that only contains metadata-valid characters — but the simpler
// path is to use the direct AUDIT_PLUGIN_CODEC_UNSUPPORTED gate for any
// non-identity codec so Resolve is unreachable from real traffic. Left
// documented rather than reached to avoid synthetic coverage.
func TestDecodeEnvelopeCodecUnsupportedWins(t *testing.T) {
	t.Parallel()
	// Sanity: confirm the non-identity codec gate fires before Resolve.
	h := validPluginHeaders(t)
	h.Set(headerCodec, "aes-gcm-v1")
	_, err := decodeEnvelope(h, []byte("anything"))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_CODEC_UNSUPPORTED")
}

// TestDecodeEnvelopeIdentityCodecDecodesSuccess is the happy path proving
// the end-to-end non-error branch executes.
func TestDecodeEnvelopeIdentityCodecDecodesSuccess(t *testing.T) {
	t.Parallel()
	h := validPluginHeaders(t)
	env, err := decodeEnvelope(h, newValidEnvelopeBytes(t))
	require.NoError(t, err)
	require.NotNil(t, env)
	assert.Equal(t, "events.main.plugin.test", env.GetSubject())
}

// Silence unused import lints when any of the above are trimmed.
var _ = jetstream.MsgMetadata{}
