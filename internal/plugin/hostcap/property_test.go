// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/property"
	"github.com/holomush/holomush/internal/world"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// validEntityULID is a well-formed ULID string used across property tests.
var validEntityULID = ulid.Make().String()

// fakePropertyDef is a property.Definition stub whose Get and Set behavior
// is configurable per test case.
type fakePropertyDef struct {
	getValue string
	getErr   error
	setErr   error
}

func (f *fakePropertyDef) Validate(_ string) error { return nil }

func (f *fakePropertyDef) Get(_ context.Context, _ property.WorldQuerier, _ string, _ ulid.ULID) (string, error) {
	return f.getValue, f.getErr
}

func (f *fakePropertyDef) Set(_ context.Context, _ property.WorldQuerier, _ property.WorldMutator, _ string, _ string, _ ulid.ULID, _ string) error {
	return f.setErr
}

// fakePropertyWorldQuerier satisfies hostcap.WorldQuerier with no-op stubs.
// GetLocation and GetObject are used by property.Definition.Get/Set; GetCharacter
// and GetCharactersByLocation are required by the wider WorldQuerier interface.
type fakePropertyWorldQuerier struct{}

func (fakePropertyWorldQuerier) GetLocation(_ context.Context, _ ulid.ULID) (*world.Location, error) {
	return nil, nil
}

func (fakePropertyWorldQuerier) GetCharacter(_ context.Context, _ ulid.ULID) (*world.Character, error) {
	return nil, nil
}

func (fakePropertyWorldQuerier) GetCharactersByLocation(_ context.Context, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	return nil, nil
}

func (fakePropertyWorldQuerier) GetObject(_ context.Context, _ ulid.ULID) (*world.Object, error) {
	return nil, nil
}

// fakePropertyWorldMutator satisfies world.Mutator (= hostcap.WorldMutator) with no-op stubs.
type fakePropertyWorldMutator struct{}

func (fakePropertyWorldMutator) GetLocation(_ context.Context, _ string, _ ulid.ULID) (*world.Location, error) {
	return nil, nil
}

func (fakePropertyWorldMutator) GetCharacter(_ context.Context, _ string, _ ulid.ULID) (*world.Character, error) {
	return nil, nil
}

func (fakePropertyWorldMutator) GetCharactersByLocation(_ context.Context, _ string, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	return nil, nil
}

func (fakePropertyWorldMutator) GetObject(_ context.Context, _ string, _ ulid.ULID) (*world.Object, error) {
	return nil, nil
}

func (fakePropertyWorldMutator) CreateLocation(_ context.Context, _ string, _ *world.Location) error {
	return nil
}

func (fakePropertyWorldMutator) CreateExit(_ context.Context, _ string, _ *world.Exit) error {
	return nil
}

func (fakePropertyWorldMutator) CreateObject(_ context.Context, _ string, _ *world.Object) error {
	return nil
}

func (fakePropertyWorldMutator) UpdateLocation(_ context.Context, _ string, _ *world.Location) error {
	return nil
}

func (fakePropertyWorldMutator) UpdateObject(_ context.Context, _ string, _ *world.Object) error {
	return nil
}

func (fakePropertyWorldMutator) FindLocationByName(_ context.Context, _, _ string) (*world.Location, error) {
	return nil, nil
}

// propertyHostCaps is a focused HostCapabilities stub for property tests.
// It extends stubHostCaps with configurable property/world port methods.
type propertyHostCaps struct {
	stubHostCaps
	def     *fakePropertyDef
	querier hostcap.WorldQuerier
	mutator hostcap.WorldMutator
}

func (c *propertyHostCaps) PropertyDefinition(_ string) (hostcap.PropertyDefinition, bool) {
	if c.def == nil {
		return nil, false
	}
	return c.def, true
}

func (c *propertyHostCaps) WorldQuerier(_ string) hostcap.WorldQuerier { return c.querier }
func (c *propertyHostCaps) WorldMutator() hostcap.WorldMutator         { return c.mutator }

// newPropertyCaps returns a stub with a property definition that returns getValue on Get.
func newPropertyCaps(getValue string) *propertyHostCaps {
	return &propertyHostCaps{
		def:     &fakePropertyDef{getValue: getValue},
		querier: fakePropertyWorldQuerier{},
		mutator: fakePropertyWorldMutator{},
	}
}

// requireInvalidArgument asserts err is a gRPC InvalidArgument status.
func requireInvalidArgument(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// propertyCapsWithErr builds a property stub whose definition fails Get/Set with
// the given errors (querier/mutator wired so the handler reaches the definition).
func propertyCapsWithErr(getErr, setErr error) *propertyHostCaps {
	return &propertyHostCaps{
		def:     &fakePropertyDef{getErr: getErr, setErr: setErr},
		querier: fakePropertyWorldQuerier{},
		mutator: fakePropertyWorldMutator{},
	}
}

func TestPropertyServerGetProperty(t *testing.T) {
	tests := []struct {
		name     string
		caps     *propertyHostCaps
		entityID string
		check    func(t *testing.T, resp *hostv1.GetPropertyResponse, err error)
	}{
		{
			name:     "reads through the definition and returns the value",
			caps:     newPropertyCaps("Town Square"),
			entityID: validEntityULID,
			check: func(t *testing.T, resp *hostv1.GetPropertyResponse, err error) {
				require.NoError(t, err)
				assert.Equal(t, "Town Square", resp.GetValue())
			},
		},
		{
			name:     "returns InvalidArgument for an unknown property",
			caps:     &propertyHostCaps{}, // def nil → PropertyDefinition returns false
			entityID: validEntityULID,
			check: func(t *testing.T, _ *hostv1.GetPropertyResponse, err error) {
				requireInvalidArgument(t, err)
			},
		},
		{
			name:     "returns InvalidArgument for an unparseable entity id",
			caps:     newPropertyCaps("value"),
			entityID: "not-a-ulid",
			check: func(t *testing.T, _ *hostv1.GetPropertyResponse, err error) {
				requireInvalidArgument(t, err)
			},
		},
		{
			name:     "returns opaque internal error on Get failure",
			caps:     propertyCapsWithErr(errors.New("secret DB connection string leaked"), nil),
			entityID: validEntityULID,
			check: func(t *testing.T, _ *hostv1.GetPropertyResponse, err error) {
				requireOpaqueInternal(t, err)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := hostcap.NewPropertyServer(hostcap.NewBase(tc.caps, "core-objects"))
			resp, err := srv.GetProperty(context.Background(), &hostv1.GetPropertyRequest{
				EntityType: "location",
				EntityId:   tc.entityID,
				Property:   "name",
			})
			tc.check(t, resp, err)
		})
	}
}

func TestPropertyServerSetProperty(t *testing.T) {
	tests := []struct {
		name     string
		caps     *propertyHostCaps
		entityID string
		check    func(t *testing.T, resp *hostv1.SetPropertyResponse, err error)
	}{
		{
			name:     "delegates to the definition Set and returns an empty response",
			caps:     newPropertyCaps(""),
			entityID: validEntityULID,
			check: func(t *testing.T, resp *hostv1.SetPropertyResponse, err error) {
				require.NoError(t, err)
				assert.NotNil(t, resp)
			},
		},
		{
			name:     "returns InvalidArgument for an unknown property",
			caps:     &propertyHostCaps{}, // def nil
			entityID: validEntityULID,
			check: func(t *testing.T, _ *hostv1.SetPropertyResponse, err error) {
				requireInvalidArgument(t, err)
			},
		},
		{
			name:     "returns InvalidArgument for an unparseable entity id",
			caps:     newPropertyCaps(""),
			entityID: "not-a-ulid",
			check: func(t *testing.T, _ *hostv1.SetPropertyResponse, err error) {
				requireInvalidArgument(t, err)
			},
		},
		{
			name:     "returns opaque internal error on Set failure",
			caps:     propertyCapsWithErr(nil, errors.New("secret connection string")),
			entityID: validEntityULID,
			check: func(t *testing.T, _ *hostv1.SetPropertyResponse, err error) {
				requireOpaqueInternal(t, err)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := hostcap.NewPropertyServer(hostcap.NewBase(tc.caps, "core-objects"))
			resp, err := srv.SetProperty(context.Background(), &hostv1.SetPropertyRequest{
				EntityType: "location",
				EntityId:   tc.entityID,
				Property:   "name",
				Value:      "New Name",
			})
			tc.check(t, resp, err)
		})
	}
}
