// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/eventbus"
)

// fakeBus satisfies all three split interfaces; used to verify that
// EventBus is satisfiable as the composition.
type fakeBus struct{}

func (fakeBus) Publish(_ context.Context, _ eventbus.Event) error { return nil }
func (fakeBus) OpenSession(_ context.Context, _ string, _ eventbus.SessionIdentity, _ []eventbus.Subject, _ time.Time) (eventbus.SessionStream, error) {
	return nil, nil
}

func (fakeBus) QueryHistory(_ context.Context, _ eventbus.HistoryQuery) (eventbus.HistoryStream, error) {
	return nil, nil
}

// Compile-time interface checks: fakeBus must satisfy all three split interfaces and EventBus.
var (
	_ eventbus.Publisher     = fakeBus{}
	_ eventbus.Subscriber    = fakeBus{}
	_ eventbus.HistoryReader = fakeBus{}
	_ eventbus.EventBus      = fakeBus{}
)

func TestHistoryQueryNewCursorFields(t *testing.T) {
	t.Parallel()
	id := ulid.MustParse("01HYXYZEVT0000000000000001")
	q := eventbus.HistoryQuery{
		AfterSeq:  10,
		AfterID:   id,
		BeforeSeq: 100,
		BeforeID:  id,
	}
	assert.Equal(t, uint64(10), q.AfterSeq)
	assert.Equal(t, id, q.AfterID)
	assert.Equal(t, uint64(100), q.BeforeSeq)
	assert.Equal(t, id, q.BeforeID)
}
