// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

// fakeDLQJetStream records the last stream config declared and the last
// message published, and can be told to fail either call. It fakes the
// narrow dlqJetStream surface so EnsureStream/Capture are unit-testable
// without a broker.
type fakeDLQJetStream struct {
	createCalls  int
	lastStream   jetstream.StreamConfig
	createErr    error
	publishCalls int
	lastMsg      *nats.Msg
	publishErr   error
}

func (f *fakeDLQJetStream) CreateOrUpdateStream(_ context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error) {
	f.createCalls++
	f.lastStream = cfg
	return nil, f.createErr
}

func (f *fakeDLQJetStream) PublishMsg(_ context.Context, msg *nats.Msg, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	f.publishCalls++
	f.lastMsg = msg
	if f.publishErr != nil {
		return nil, f.publishErr
	}
	return &jetstream.PubAck{}, nil
}

func testDLQConfig() DLQConfig {
	return DLQConfig{
		StreamName: "EVENTS_AUDIT_DLQ",
		Subject:    "internal.main.audit.dlq",
		MaxAge:     48 * time.Hour,
		MaxBytes:   4096,
		Storage:    jetstream.MemoryStorage,
	}
}

func TestDLQEnsureStreamCreatesBoundedStream(t *testing.T) {
	t.Parallel()
	fake := &fakeDLQJetStream{}
	d := newDLQPublisher(fake, testDLQConfig())

	require.NoError(t, d.EnsureStream(context.Background()))

	require.Equal(t, 1, fake.createCalls)
	assert.Equal(t, "EVENTS_AUDIT_DLQ", fake.lastStream.Name)
	assert.Equal(t, []string{"internal.main.audit.dlq.>"}, fake.lastStream.Subjects)
	assert.Equal(t, 48*time.Hour, fake.lastStream.MaxAge)
	assert.Equal(t, int64(4096), fake.lastStream.MaxBytes)
	assert.Equal(t, jetstream.MemoryStorage, fake.lastStream.Storage)
	assert.Equal(t, jetstream.LimitsPolicy, fake.lastStream.Retention)
}

func TestDLQEnsureStreamMapsZeroMaxBytesToUnlimited(t *testing.T) {
	t.Parallel()
	cfg := testDLQConfig()
	cfg.MaxBytes = 0
	fake := &fakeDLQJetStream{}
	d := newDLQPublisher(fake, cfg)

	require.NoError(t, d.EnsureStream(context.Background()))
	assert.Equal(t, int64(-1), fake.lastStream.MaxBytes,
		"zero MaxBytes must map to the JetStream -1 unbounded sentinel, not literal 0")
}

func TestDLQEnsureStreamIsIdempotent(t *testing.T) {
	t.Parallel()
	fake := &fakeDLQJetStream{}
	d := newDLQPublisher(fake, testDLQConfig())

	require.NoError(t, d.EnsureStream(context.Background()))
	require.NoError(t, d.EnsureStream(context.Background()))
	assert.Equal(t, 2, fake.createCalls,
		"CreateOrUpdateStream is idempotent; a second call is a no-op success")
}

func TestDLQEnsureStreamSurfacesDeclareError(t *testing.T) {
	t.Parallel()
	fake := &fakeDLQJetStream{createErr: errors.New("boom")}
	d := newDLQPublisher(fake, testDLQConfig())

	err := d.EnsureStream(context.Background())
	errutil.AssertErrorCode(t, err, "AUDIT_DLQ_STREAM_DECLARE_FAILED")
}

func TestDLQCapturePreservesSubjectHeadersDataAndIncrementsCounter(t *testing.T) {
	// Not parallel: asserts a delta on the package-global DLQMessagesTotal.
	before := testutil.ToFloat64(DLQMessagesTotal)

	fake := &fakeDLQJetStream{}
	d := newDLQPublisher(fake, testDLQConfig())

	origHeaders := nats.Header{
		"Nats-Msg-Id":    []string{"01ABCDEF"},
		"App-Event-Type": []string{"test.unit"},
	}
	msg := &stubMsg{
		headers: origHeaders,
		subject: "events.main.test",
		data:    []byte(`{"hello":"world"}`),
	}

	require.NoError(t, d.Capture(context.Background(), msg))

	require.Equal(t, 1, fake.publishCalls)
	require.NotNil(t, fake.lastMsg)
	assert.Equal(t, "internal.main.audit.dlq.events.main.test", fake.lastMsg.Subject,
		"the original subject is encoded in the DLQ subject suffix for replay")
	assert.Equal(t, nats.Header(origHeaders), fake.lastMsg.Header,
		"headers (incl. Nats-Msg-Id) must be preserved unchanged")
	assert.Equal(t, []byte(`{"hello":"world"}`), fake.lastMsg.Data)

	after := testutil.ToFloat64(DLQMessagesTotal)
	assert.InDelta(t, 1.0, after-before, 0.0001,
		"holomush_audit_dlq_messages_total increments exactly once per captured message")
}

func TestDLQCaptureSurfacesPublishErrorWithoutIncrementingCounter(t *testing.T) {
	// Not parallel: asserts the package-global DLQMessagesTotal is untouched.
	before := testutil.ToFloat64(DLQMessagesTotal)

	fake := &fakeDLQJetStream{publishErr: errors.New("no stream matches subject")}
	d := newDLQPublisher(fake, testDLQConfig())
	msg := &stubMsg{
		headers: nats.Header{"Nats-Msg-Id": []string{"01ABCDEF"}},
		subject: "events.main.test",
		data:    []byte(`{}`),
	}

	err := d.Capture(context.Background(), msg)
	errutil.AssertErrorCode(t, err, "AUDIT_DLQ_PUBLISH_FAILED")

	after := testutil.ToFloat64(DLQMessagesTotal)
	assert.InDelta(t, 0.0, after-before, 0.0001,
		"a failed publish must NOT increment the counter (caller will Nak, never drop)")
}
