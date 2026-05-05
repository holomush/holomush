// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/pkg/errutil"
)

type stubIdentityRegistry struct{ idsByName map[string]ulid.ULID }

func (s *stubIdentityRegistry) NameByID(ulid.ULID) (string, bool) { return "", false }
func (s *stubIdentityRegistry) IDByName(name string) (ulid.ULID, bool) {
	id, ok := s.idsByName[name]
	return id, ok
}

func TestStampPluginActorSucceedsForRegisteredPlugin(t *testing.T) {
	expected := core.NewULID()
	reg := &stubIdentityRegistry{idsByName: map[string]ulid.ULID{"core-scenes": expected}}

	got, err := stampPluginActor(reg, "core-scenes")
	require.NoError(t, err)
	assert.Equal(t, core.ActorPlugin, got.Kind)
	assert.Equal(t, expected.String(), got.ID)
}

func TestStampPluginActorFailsForUnregistered(t *testing.T) {
	reg := &stubIdentityRegistry{idsByName: nil}
	_, err := stampPluginActor(reg, "unknown")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_UNREGISTERED_INVOKE")
}

func TestStampPluginActorFailsForNilRegistry(t *testing.T) {
	_, err := stampPluginActor(nil, "any-plugin")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_UNREGISTERED_INVOKE")
}
