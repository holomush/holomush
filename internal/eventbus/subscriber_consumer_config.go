// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"context"
	"errors"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/samber/oops"
)

// consumerInfo is the narrow surface buildConsumerConfig needs from an
// existing JetStream consumer — just enough to read its config. The real
// jetstream.Consumer satisfies this trivially. Tests provide a stub that
// implements ONLY CachedInfo without touching the full ~7-method
// jetstream.Consumer surface.
type consumerInfo interface {
	CachedInfo() *jetstream.ConsumerInfo
}

// consumerLookup is the narrow surface buildConsumerConfig needs from a
// JetStream context. Returns the local consumerInfo (NOT jetstream.Consumer)
// so test stubs do not need to satisfy the full jetstream.Consumer interface.
// Production callers pass a thin adapter that wraps js.Consumer(...) and
// returns its result as a consumerInfo.
type consumerLookup interface {
	LookupConsumer(ctx context.Context, stream, consumer string) (consumerInfo, error)
}

// jsConsumerLookupAdapter wraps a jetstream.JetStream so it satisfies the
// narrow consumerLookup interface buildConsumerConfig consumes. Wired into
// OpenSession and SetFilters by Task 13 (holomush-iwzt.13).
type jsConsumerLookupAdapter struct{ js jetstream.JetStream }

func (a jsConsumerLookupAdapter) LookupConsumer(ctx context.Context, stream, name string) (consumerInfo, error) {
	c, err := a.js.Consumer(ctx, stream, name)
	if err != nil {
		return nil, err //nolint:wrapcheck // caller (buildConsumerConfig) wraps with EVENTBUS_CONSUMER_LOOKUP_FAILED
	}
	return c, nil // jetstream.Consumer satisfies consumerInfo (has CachedInfo)
}

// buildConsumerConfig produces a jetstream.ConsumerConfig that is safe to pass
// to CreateOrUpdateConsumer regardless of whether the durable already exists.
// Implements the NATS-source-of-truth pattern per holomush-iwzt §6.2 / INV-PRIVACY-8.
//
// Three branches:
//
//  1. Existing consumer hit (lookupErr == nil && existing != nil): copies
//     DeliverPolicy, OptStartSeq, and OptStartTime verbatim from the existing
//     consumer's cached config. Only FilterSubjects is set fresh. OptStartTime
//     is *time.Time — nil vs. non-nil is preserved exactly.
//
//  2. ErrConsumerNotFound: builds a fresh config. If minFloor.IsZero(),
//     uses DeliverAllPolicy. Otherwise DeliverByStartTimePolicy with
//     OptStartTime = &minFloor.
//
//  3. Any other error: fails closed with EVENTBUS_CONSUMER_LOOKUP_FAILED.
//     Callers MUST NOT call CreateOrUpdateConsumer when this error is returned.
func buildConsumerConfig(
	ctx context.Context, js consumerLookup,
	streamName, consumerName string,
	filterSubjects []string, minFloor time.Time,
) (jetstream.ConsumerConfig, error) {
	existing, lookupErr := js.LookupConsumer(ctx, streamName, consumerName)
	switch {
	case lookupErr == nil && existing != nil:
		info := existing.CachedInfo()
		if info == nil {
			return jetstream.ConsumerConfig{}, oops.Code("EVENTBUS_CONSUMER_LOOKUP_FAILED").
				With("stream", streamName).With("consumer", consumerName).
				Errorf("existing consumer returned nil CachedInfo")
		}
		cur := info.Config
		cfg := jetstream.ConsumerConfig{
			Durable:        consumerName,
			Name:           consumerName,
			FilterSubjects: filterSubjects,
			DeliverPolicy:  cur.DeliverPolicy,
			OptStartSeq:    cur.OptStartSeq,
		}
		// OptStartTime is *time.Time — preserve nil vs. non-nil verbatim.
		if cur.OptStartTime != nil {
			t := *cur.OptStartTime
			cfg.OptStartTime = &t
		}
		return cfg, nil

	case errors.Is(lookupErr, jetstream.ErrConsumerNotFound):
		cfg := jetstream.ConsumerConfig{
			Durable:        consumerName,
			Name:           consumerName,
			FilterSubjects: filterSubjects,
		}
		if !minFloor.IsZero() {
			cfg.DeliverPolicy = jetstream.DeliverByStartTimePolicy
			cfg.OptStartTime = &minFloor
		} else {
			cfg.DeliverPolicy = jetstream.DeliverAllPolicy
		}
		return cfg, nil

	default:
		return jetstream.ConsumerConfig{}, oops.Code("EVENTBUS_CONSUMER_LOOKUP_FAILED").
			With("stream", streamName).With("consumer", consumerName).
			Wrap(lookupErr)
	}
}
