// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
)

func TestBuildCharacterIdentity(t *testing.T) {
	pid := core.NewULID().String()
	cid := core.NewULID().String()

	t.Run("crypto inactive returns zero identity no lookup", func(t *testing.T) {
		s := &CoreServer{bindings: &fakeBindingRepo{}, cryptoActive: false}
		id, err := s.buildCharacterIdentity(context.Background(), pid, cid)
		require.NoError(t, err)
		require.Equal(t, eventbus.SessionIdentity{}, id)
	})

	t.Run("nil bindings returns zero identity", func(t *testing.T) {
		s := &CoreServer{bindings: nil, cryptoActive: true}
		id, err := s.buildCharacterIdentity(context.Background(), pid, cid)
		require.NoError(t, err)
		require.Equal(t, eventbus.SessionIdentity{}, id)
	})

	t.Run("active with binding returns character identity", func(t *testing.T) {
		s := &CoreServer{bindings: &fakeBindingRepo{bindingID: "bind-1"}, cryptoActive: true}
		id, err := s.buildCharacterIdentity(context.Background(), pid, cid)
		require.NoError(t, err)
		require.NotEqual(t, eventbus.SessionIdentity{}, id)
	})

	t.Run("Current error returns a non-nil error without consuming a surface code", func(t *testing.T) {
		// The helper intentionally returns an oops-without-code error so call
		// sites can wrap it with the appropriate surface code
		// (SUBSCRIBE_BINDING_LOOKUP_FAILED or HISTORY_BINDING_LOOKUP_FAILED)
		// and that code remains observable via oops.AsOops.
		s := &CoreServer{bindings: &fakeBindingRepo{err: errors.New("boom")}, cryptoActive: true}
		_, err := s.buildCharacterIdentity(context.Background(), pid, cid)
		require.Error(t, err)
	})
}
