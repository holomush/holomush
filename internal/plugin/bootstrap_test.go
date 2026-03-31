// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockBootstrapPlugin struct {
	priority  int
	err       error
	called    bool
	callOrder *[]string
	name      string
}

func (m *mockBootstrapPlugin) Priority() int { return m.priority }
func (m *mockBootstrapPlugin) Bootstrap(ctx context.Context, _ *Manifest, _ string) error {
	m.called = true
	if m.callOrder != nil {
		*m.callOrder = append(*m.callOrder, m.name)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return m.err
}

func newTestRunner() *BootstrapRunner {
	return NewBootstrapRunner(slog.Default())
}

func TestBootstrapRunner_EmptySucceeds(t *testing.T) {
	r := newTestRunner()
	err := r.RunAll(context.Background())
	require.NoError(t, err)
}

func TestBootstrapRunner_SinglePluginRuns(t *testing.T) {
	r := newTestRunner()
	p := &mockBootstrapPlugin{priority: 100, name: "a"}
	r.Register(p)

	err := r.RunAll(context.Background())
	require.NoError(t, err)
	assert.True(t, p.called)
}

func TestBootstrapRunner_PriorityOrdering(t *testing.T) {
	r := newTestRunner()
	order := []string{}
	r.Register(&mockBootstrapPlugin{priority: 300, name: "300", callOrder: &order})
	r.Register(&mockBootstrapPlugin{priority: 100, name: "100", callOrder: &order})
	r.Register(&mockBootstrapPlugin{priority: 200, name: "200", callOrder: &order})

	err := r.RunAll(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"100", "200", "300"}, order)
}

func TestBootstrapRunner_SamePriorityPreservesRegistrationOrder(t *testing.T) {
	r := newTestRunner()
	order := []string{}
	r.Register(&mockBootstrapPlugin{priority: 100, name: "A", callOrder: &order})
	r.Register(&mockBootstrapPlugin{priority: 100, name: "B", callOrder: &order})
	r.Register(&mockBootstrapPlugin{priority: 100, name: "C", callOrder: &order})

	err := r.RunAll(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "B", "C"}, order)
}

func TestBootstrapRunner_ErrorStopsExecution(t *testing.T) {
	r := newTestRunner()
	order := []string{}
	errB := errors.New("bootstrap failed")

	a := &mockBootstrapPlugin{priority: 100, name: "A", callOrder: &order}
	b := &mockBootstrapPlugin{priority: 200, name: "B", callOrder: &order, err: errB}
	c := &mockBootstrapPlugin{priority: 300, name: "C", callOrder: &order}

	r.Register(a)
	r.Register(b)
	r.Register(c)

	err := r.RunAll(context.Background())
	require.ErrorIs(t, err, errB)
	assert.True(t, a.called)
	assert.True(t, b.called)
	assert.False(t, c.called)
	assert.Equal(t, []string{"A", "B"}, order)
}

func TestBootstrapRunner_ContextCancellation(t *testing.T) {
	r := newTestRunner()
	p := &mockBootstrapPlugin{priority: 100, name: "a"}
	r.Register(p)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.RunAll(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}
