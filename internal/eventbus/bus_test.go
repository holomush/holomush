// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"context"
	"testing"

	"github.com/holomush/holomush/internal/eventbus"
)

// fakeBus satisfies all three split interfaces; used to verify that
// EventBus is satisfiable as the composition.
type fakeBus struct{}

func (fakeBus) Publish(_ context.Context, _ eventbus.Event) error { return nil }
func (fakeBus) OpenSession(_ context.Context, _ string, _ []eventbus.Subject) (eventbus.SessionStream, error) {
	return nil, nil
}

func (fakeBus) QueryHistory(_ context.Context, _ eventbus.HistoryQuery) (eventbus.HistoryStream, error) {
	return nil, nil
}

func TestEventBusInterfaceComposesAllThree(_ *testing.T) {
	var (
		_ eventbus.Publisher     = fakeBus{}
		_ eventbus.Subscriber    = fakeBus{}
		_ eventbus.HistoryReader = fakeBus{}
		_ eventbus.EventBus      = fakeBus{}
	)
}
