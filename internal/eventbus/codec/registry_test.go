// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package codec_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/codec"
)

// declaredNames lists every Name constant defined in codec.go.
// This list MUST be updated when a new constant is added — the meta-test
// below catches the case where a const is declared but not registered.
var declaredNames = []codec.Name{
	codec.NameIdentity,
	// Add new constants here when introduced.
}

func TestEveryDeclaredCodecNameIsRegistered(t *testing.T) {
	for _, n := range declaredNames {
		t.Run(string(n), func(t *testing.T) {
			c, err := codec.Resolve(n)
			require.NoError(t, err, "codec %q is declared but not in registry", n)
			require.Equal(t, n, c.Name())
		})
	}
}

func TestRegistryHasNoExtraEntriesNotDeclared(t *testing.T) {
	declared := make(map[codec.Name]bool, len(declaredNames))
	for _, n := range declaredNames {
		declared[n] = true
	}
	for _, n := range codec.All() {
		if !declared[n] {
			t.Errorf("registry contains %q which is not in declaredNames — update declaredNames or remove from registry", n)
		}
	}
}

func TestResolveUnknownCodecReturnsError(t *testing.T) {
	_, err := codec.Resolve(codec.Name("does-not-exist"))
	require.Error(t, err)
}

func TestRegisterForTestRestoresState(t *testing.T) {
	var stub stubCodec
	cleanup := codec.RegisterForTest(stub)
	c, err := codec.Resolve(stub.Name())
	require.NoError(t, err)
	require.True(t, reflect.TypeOf(c) == reflect.TypeOf(stub))

	cleanup()
	_, err = codec.Resolve(stub.Name())
	require.Error(t, err)
}

type stubCodec struct{}

func (stubCodec) Name() codec.Name { return codec.Name("test-only-stub") }
func (stubCodec) Encode(_ context.Context, p []byte, _ codec.Key, _ []byte) ([]byte, error) {
	return p, nil
}

func (stubCodec) Decode(_ context.Context, p []byte, _ codec.Key, _ []byte) ([]byte, error) {
	return p, nil
}
