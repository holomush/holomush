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

// fakePropertyWorldQuerier satisfies property.WorldQuerier with no-op stubs.
type fakePropertyWorldQuerier struct{}

func (fakePropertyWorldQuerier) GetLocation(_ context.Context, _ ulid.ULID) (*world.Location, error) {
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

// TestPropertyServerGetPropertyReadsViaDefinition verifies that GetProperty
// resolves through PropertyDefinition.Get and returns the configured value.
func TestPropertyServerGetPropertyReadsViaDefinition(t *testing.T) {
	caps := newPropertyCaps("Town Square")
	srv := hostcap.NewPropertyServer(hostcap.NewBase(caps, "core-objects"))
	resp, err := srv.GetProperty(context.Background(), &hostv1.GetPropertyRequest{
		EntityType: "location",
		EntityId:   validEntityULID,
		Property:   "name",
	})
	require.NoError(t, err)
	assert.Equal(t, "Town Square", resp.GetValue())
}

// TestPropertyServerGetPropertyReturnsInvalidArgumentForUnknownProperty verifies
// that GetProperty returns codes.InvalidArgument when the property name is not registered.
func TestPropertyServerGetPropertyReturnsInvalidArgumentForUnknownProperty(t *testing.T) {
	caps := &propertyHostCaps{} // def nil → PropertyDefinition returns false
	srv := hostcap.NewPropertyServer(hostcap.NewBase(caps, "core-objects"))
	_, err := srv.GetProperty(context.Background(), &hostv1.GetPropertyRequest{
		EntityType: "location",
		EntityId:   validEntityULID,
		Property:   "nonexistent",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// TestPropertyServerGetPropertyReturnsInvalidArgumentForBadEntityID verifies
// that an unparseable entity_id returns codes.InvalidArgument.
func TestPropertyServerGetPropertyReturnsInvalidArgumentForBadEntityID(t *testing.T) {
	caps := newPropertyCaps("value")
	srv := hostcap.NewPropertyServer(hostcap.NewBase(caps, "core-objects"))
	_, err := srv.GetProperty(context.Background(), &hostv1.GetPropertyRequest{
		EntityType: "location",
		EntityId:   "not-a-ulid",
		Property:   "name",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// TestPropertyServerGetPropertyReturnsOpaqueInternalErrorOnGetFailure verifies
// that an internal error from property.Definition.Get does not leak inner details
// to the caller — the status message must be the static string "internal error".
func TestPropertyServerGetPropertyReturnsOpaqueInternalErrorOnGetFailure(t *testing.T) {
	caps := &propertyHostCaps{
		def:     &fakePropertyDef{getErr: errors.New("secret DB connection string leaked")},
		querier: fakePropertyWorldQuerier{},
		mutator: fakePropertyWorldMutator{},
	}
	srv := hostcap.NewPropertyServer(hostcap.NewBase(caps, "core-objects"))
	_, err := srv.GetProperty(context.Background(), &hostv1.GetPropertyRequest{
		EntityType: "location",
		EntityId:   validEntityULID,
		Property:   "name",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	// The message MUST NOT leak the inner error text (grpc-errors.md).
	assert.Equal(t, "internal error", st.Message(),
		"inner error detail must not leak to the caller")
	assert.NotContains(t, st.Message(), "secret")
}

// TestPropertyServerSetPropertyDelegatesToDefinitionSet verifies that SetProperty
// calls property.Definition.Set and returns an empty response on success.
func TestPropertyServerSetPropertyDelegatesToDefinitionSet(t *testing.T) {
	caps := newPropertyCaps("")
	srv := hostcap.NewPropertyServer(hostcap.NewBase(caps, "core-objects"))
	resp, err := srv.SetProperty(context.Background(), &hostv1.SetPropertyRequest{
		EntityType: "location",
		EntityId:   validEntityULID,
		Property:   "name",
		Value:      "New Name",
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

// TestPropertyServerSetPropertyReturnsInvalidArgumentForUnknownProperty verifies
// that SetProperty returns codes.InvalidArgument when the property is not registered.
func TestPropertyServerSetPropertyReturnsInvalidArgumentForUnknownProperty(t *testing.T) {
	caps := &propertyHostCaps{} // def nil
	srv := hostcap.NewPropertyServer(hostcap.NewBase(caps, "core-objects"))
	_, err := srv.SetProperty(context.Background(), &hostv1.SetPropertyRequest{
		EntityType: "location",
		EntityId:   validEntityULID,
		Property:   "nonexistent",
		Value:      "v",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// TestPropertyServerSetPropertyReturnsInvalidArgumentForBadEntityID verifies
// that an unparseable entity_id on SetProperty returns codes.InvalidArgument.
func TestPropertyServerSetPropertyReturnsInvalidArgumentForBadEntityID(t *testing.T) {
	caps := newPropertyCaps("")
	srv := hostcap.NewPropertyServer(hostcap.NewBase(caps, "core-objects"))
	_, err := srv.SetProperty(context.Background(), &hostv1.SetPropertyRequest{
		EntityType: "location",
		EntityId:   "not-a-ulid",
		Property:   "name",
		Value:      "v",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// TestPropertyServerSetPropertyReturnsOpaqueInternalErrorOnSetFailure verifies
// that an internal error from property.Definition.Set does not leak inner details.
func TestPropertyServerSetPropertyReturnsOpaqueInternalErrorOnSetFailure(t *testing.T) {
	caps := &propertyHostCaps{
		def:     &fakePropertyDef{setErr: errors.New("secret connection string")},
		querier: fakePropertyWorldQuerier{},
		mutator: fakePropertyWorldMutator{},
	}
	srv := hostcap.NewPropertyServer(hostcap.NewBase(caps, "core-objects"))
	_, err := srv.SetProperty(context.Background(), &hostv1.SetPropertyRequest{
		EntityType: "location",
		EntityId:   validEntityULID,
		Property:   "name",
		Value:      "v",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal error", st.Message(),
		"inner error detail must not leak to the caller")
	assert.NotContains(t, st.Message(), "secret")
}
