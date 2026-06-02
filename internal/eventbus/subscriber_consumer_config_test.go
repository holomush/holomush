// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

// stubJS satisfies the narrow consumerLookup interface.
// LookupConsumer returns a consumerInfo (NOT jetstream.Consumer) so the
// test stub does NOT need to implement the full jetstream.Consumer surface.
type stubJS struct {
	info        consumerInfo // nil → not found
	lookupErr   error
	lookupCalls int
}

func (s *stubJS) LookupConsumer(_ context.Context, _, _ string) (consumerInfo, error) {
	s.lookupCalls++
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	return s.info, nil
}

// stubConsumer satisfies the narrow consumerInfo interface — just CachedInfo().
type stubConsumer struct {
	cfg jetstream.ConsumerConfig
}

func (s *stubConsumer) CachedInfo() *jetstream.ConsumerInfo {
	return &jetstream.ConsumerInfo{Config: s.cfg}
}

// TestBuildConsumerConfig_FreshOpenSession_ComputesMinFloor verifies that
// when no durable consumer exists (ErrConsumerNotFound), the builder
// produces DeliverByStartTimePolicy with OptStartTime = minFloor.
func TestBuildConsumerConfig_FreshOpenSession_ComputesMinFloor(t *testing.T) {
	minFloor := time.Now().Add(-time.Hour)
	js := &stubJS{info: nil, lookupErr: jetstream.ErrConsumerNotFound}

	cfg, err := buildConsumerConfig(context.Background(), js, "STREAM", "consumer-name",
		[]string{"events.gid.location.X"}, minFloor)

	require.NoError(t, err)
	assert.Equal(t, jetstream.DeliverByStartTimePolicy, cfg.DeliverPolicy)
	require.NotNil(t, cfg.OptStartTime)
	assert.True(t, cfg.OptStartTime.Equal(minFloor))
}

// TestBuildConsumerConfig_FreshOpenSession_ZeroMinFloor_UsesDeliverAll verifies
// the other half of the ErrConsumerNotFound branch: when minFloor is the zero
// time.Time, the builder produces DeliverAllPolicy (no OptStartTime). Without
// this case the zero-minFloor path is reachable but unverified.
func TestBuildConsumerConfig_FreshOpenSession_ZeroMinFloor_UsesDeliverAll(t *testing.T) {
	js := &stubJS{info: nil, lookupErr: jetstream.ErrConsumerNotFound}

	cfg, err := buildConsumerConfig(context.Background(), js, "STREAM", "consumer-name",
		[]string{"events.gid.location.X"}, time.Time{})

	require.NoError(t, err)
	assert.Equal(t, jetstream.DeliverAllPolicy, cfg.DeliverPolicy)
	assert.Nil(t, cfg.OptStartTime, "DeliverAllPolicy MUST NOT set OptStartTime")
}

// Verifies: INV-PRIVACY-8
//
// TestBuildConsumerConfig_ExistingConsumer_PreservesStartPolicy verifies that
// when a durable consumer already exists, the builder copies DeliverPolicy and
// OptStartTime verbatim from the existing config and does NOT use the fresh
// minFloor passed by the caller. This is the core INV-PRIVACY-8 invariant.
func TestBuildConsumerConfig_ExistingConsumer_PreservesStartPolicy(t *testing.T) {
	origStart := time.Now().Add(-2 * time.Hour)
	js := &stubJS{info: &stubConsumer{cfg: jetstream.ConsumerConfig{
		DeliverPolicy: jetstream.DeliverByStartTimePolicy,
		OptStartTime:  &origStart,
	}}}

	cfg, err := buildConsumerConfig(context.Background(), js, "STREAM", "consumer-name",
		[]string{"events.gid.location.X", "events.gid.scene.Y.ic"}, time.Now())

	require.NoError(t, err)
	assert.Equal(t, jetstream.DeliverByStartTimePolicy, cfg.DeliverPolicy)
	require.NotNil(t, cfg.OptStartTime)
	assert.True(t, cfg.OptStartTime.Equal(origStart), "MUST copy existing OptStartTime verbatim — INV-PRIVACY-8")
	assert.ElementsMatch(t, []string{"events.gid.location.X", "events.gid.scene.Y.ic"}, cfg.FilterSubjects)
}

// TestBuildConsumerConfig_SetFilters_PreservesStartPolicy verifies that
// rotating FilterSubjects (simulating a SetFilters call on an established
// durable) does NOT regress the start-policy. The builder copies existing
// policy verbatim and only replaces FilterSubjects.
func TestBuildConsumerConfig_SetFilters_PreservesStartPolicy(t *testing.T) {
	origStart := time.Now().Add(-3 * time.Hour)
	js := &stubJS{info: &stubConsumer{cfg: jetstream.ConsumerConfig{
		DeliverPolicy: jetstream.DeliverByStartTimePolicy,
		OptStartTime:  &origStart,
	}}}
	newFilters := []string{"events.gid.scene.Z.ic", "events.gid.location.W"}

	cfg, err := buildConsumerConfig(context.Background(), js, "STREAM", "consumer-name",
		newFilters, time.Now())

	require.NoError(t, err)
	assert.Equal(t, jetstream.DeliverByStartTimePolicy, cfg.DeliverPolicy)
	require.NotNil(t, cfg.OptStartTime)
	assert.True(t, cfg.OptStartTime.Equal(origStart), "filter rotation MUST NOT regress start-policy — INV-PRIVACY-8")
	assert.ElementsMatch(t, newFilters, cfg.FilterSubjects)
}

// TestBuildConsumerConfig_TransientLookupError_FailsClosed verifies that
// any non-NotFound lookup error causes the builder to fail closed with
// EVENTBUS_CONSUMER_LOOKUP_FAILED. CreateOrUpdateConsumer MUST NOT be called
// (the caller receives an error and must abort the operation).
func TestBuildConsumerConfig_TransientLookupError_FailsClosed(t *testing.T) {
	js := &stubJS{lookupErr: errors.New("nats: connection closed")}

	_, err := buildConsumerConfig(context.Background(), js, "STREAM", "consumer-name",
		[]string{"events.gid.location.X"}, time.Now())

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_CONSUMER_LOOKUP_FAILED")
	assert.Equal(t, 1, js.lookupCalls, "LookupConsumer MUST be called exactly once")
}
