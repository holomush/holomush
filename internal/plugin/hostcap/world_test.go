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
	"github.com/holomush/holomush/internal/world"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// --- fake WorldQuerier -------------------------------------------------------

// fakeWorldQuerier is a configurable stub satisfying hostcap.WorldQuerier.
// It records the most-recent subject used by callers that stamp a subject (the
// worldServer calls s.host.WorldQuerier(s.pluginName) on every RPC, so the
// returned querier's subject reflects what the server stamped). Subject stamping
// is validated by the worldHostCaps wrapper below.
type fakeWorldQuerier struct {
	// per-method configured results
	locationResult *world.Location
	locationErr    error

	characterResult *world.Character
	characterErr    error

	charactersByLocationResult []*world.Character
	charactersByLocationErr    error

	objectResult *world.Object
	objectErr    error
}

func (f *fakeWorldQuerier) GetLocation(_ context.Context, _ ulid.ULID) (*world.Location, error) {
	return f.locationResult, f.locationErr
}

func (f *fakeWorldQuerier) GetCharacter(_ context.Context, _ ulid.ULID) (*world.Character, error) {
	return f.characterResult, f.characterErr
}

func (f *fakeWorldQuerier) GetCharactersByLocation(_ context.Context, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	return f.charactersByLocationResult, f.charactersByLocationErr
}

func (f *fakeWorldQuerier) GetObject(_ context.Context, _ ulid.ULID) (*world.Object, error) {
	return f.objectResult, f.objectErr
}

// --- worldHostCaps -----------------------------------------------------------

// worldHostCaps is a focused HostCapabilities stub for worldServer tests.
// It extends stubHostCaps with a configurable WorldQuerier that records the
// pluginName passed to WorldQuerier(pluginName) so tests can assert subject
// stamping.
type worldHostCaps struct {
	stubHostCaps
	querier           *fakeWorldQuerier
	lastQueriedPlugin string
}

func (c *worldHostCaps) WorldQuerier(pluginName string) hostcap.WorldQuerier {
	c.lastQueriedPlugin = pluginName
	return c.querier
}

// newWorldCaps returns a worldHostCaps wired to the given fake querier.
func newWorldCaps(q *fakeWorldQuerier) *worldHostCaps {
	return &worldHostCaps{querier: q}
}

// validWorldULID is a well-formed ULID string reused across world tests.
var validWorldULID = ulid.Make().String()

// makeLocation returns a minimal world.Location for use in happy-path tests.
func makeLocation() *world.Location {
	loc, err := world.NewLocation("Town Square", "A busy plaza.", world.LocationTypePersistent)
	if err != nil {
		panic("makeLocation: " + err.Error())
	}
	return loc
}

// makeCharacter returns a minimal world.Character for use in happy-path tests.
func makeCharacter() *world.Character {
	char, err := world.NewCharacter(ulid.Make(), "Alice")
	if err != nil {
		panic("makeCharacter: " + err.Error())
	}
	return char
}

// makeObject returns a minimal world.Object for use in happy-path tests.
func makeObject() *world.Object {
	locID := ulid.Make()
	obj, err := world.NewObject("Sword", world.Containment{LocationID: &locID})
	if err != nil {
		panic("makeObject: " + err.Error())
	}
	return obj
}

// ============================================================================
// QueryLocation
// ============================================================================

// TestWorldServerQueryLocationStampsPluginSubject verifies that QueryLocation
// calls WorldQuerier with the plugin name, confirming the server stamps the
// plugin subject via the port.
func TestWorldServerQueryLocationStampsPluginSubject(t *testing.T) {
	loc := makeLocation()
	caps := newWorldCaps(&fakeWorldQuerier{locationResult: loc})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	_, err := srv.QueryLocation(context.Background(), &hostv1.QueryLocationRequest{
		LocationId: validWorldULID,
	})
	require.NoError(t, err)
	assert.Equal(t, "core-scenes", caps.lastQueriedPlugin,
		"QueryLocation must stamp the plugin name via WorldQuerier(pluginName)")
}

// TestWorldServerQueryLocationReturnsLocationFields verifies that QueryLocation
// maps the domain Location fields to the wire response correctly.
func TestWorldServerQueryLocationReturnsLocationFields(t *testing.T) {
	loc := makeLocation()
	caps := newWorldCaps(&fakeWorldQuerier{locationResult: loc})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	resp, err := srv.QueryLocation(context.Background(), &hostv1.QueryLocationRequest{
		LocationId: validWorldULID,
	})
	require.NoError(t, err)
	assert.Equal(t, loc.ID.String(), resp.GetId())
	assert.Equal(t, loc.Name, resp.GetName())
	assert.Equal(t, loc.Description, resp.GetDescription())
	assert.Equal(t, string(loc.Type), resp.GetType())
}

// TestWorldServerQueryLocationReturnsNotFoundForMissingLocation verifies that a
// world.ErrNotFound from the querier maps to codes.NotFound on the wire.
func TestWorldServerQueryLocationReturnsNotFoundForMissingLocation(t *testing.T) {
	caps := newWorldCaps(&fakeWorldQuerier{locationErr: world.ErrNotFound})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	_, err := srv.QueryLocation(context.Background(), &hostv1.QueryLocationRequest{
		LocationId: validWorldULID,
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

// TestWorldServerQueryLocationReturnsOpaqueInternalErrorOnFailure verifies that
// an unexpected error from the querier produces a codes.Internal with a generic
// message and does not leak inner details (grpc-errors.md).
func TestWorldServerQueryLocationReturnsOpaqueInternalErrorOnFailure(t *testing.T) {
	caps := newWorldCaps(&fakeWorldQuerier{locationErr: errors.New("secret db conn string")})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	_, err := srv.QueryLocation(context.Background(), &hostv1.QueryLocationRequest{
		LocationId: validWorldULID,
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal error", st.Message(),
		"inner error detail must not leak to the caller")
	assert.NotContains(t, st.Message(), "secret")
}

// TestWorldServerQueryLocationReturnsInvalidArgumentForBadULID verifies that an
// unparseable location_id returns codes.InvalidArgument with a generic message.
func TestWorldServerQueryLocationReturnsInvalidArgumentForBadULID(t *testing.T) {
	caps := newWorldCaps(&fakeWorldQuerier{})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	_, err := srv.QueryLocation(context.Background(), &hostv1.QueryLocationRequest{
		LocationId: "not-a-ulid",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// ============================================================================
// QueryCharacter
// ============================================================================

// TestWorldServerQueryCharacterStampsPluginSubject verifies subject stamping.
func TestWorldServerQueryCharacterStampsPluginSubject(t *testing.T) {
	char := makeCharacter()
	caps := newWorldCaps(&fakeWorldQuerier{characterResult: char})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	_, err := srv.QueryCharacter(context.Background(), &hostv1.QueryCharacterRequest{
		CharacterId: validWorldULID,
	})
	require.NoError(t, err)
	assert.Equal(t, "core-scenes", caps.lastQueriedPlugin)
}

// TestWorldServerQueryCharacterReturnsCharacterFields verifies field mapping.
func TestWorldServerQueryCharacterReturnsCharacterFields(t *testing.T) {
	char := makeCharacter()
	caps := newWorldCaps(&fakeWorldQuerier{characterResult: char})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	resp, err := srv.QueryCharacter(context.Background(), &hostv1.QueryCharacterRequest{
		CharacterId: validWorldULID,
	})
	require.NoError(t, err)
	assert.Equal(t, char.ID.String(), resp.GetId())
	assert.Equal(t, char.PlayerID.String(), resp.GetPlayerId())
	assert.Equal(t, char.Name, resp.GetName())
	assert.Equal(t, char.Description, resp.GetDescription())
	// No location set on the test character — location_id MUST be empty string.
	assert.Empty(t, resp.GetLocationId())
}

// TestWorldServerQueryCharacterWithLocationIDPopulatesField verifies that a
// character with a LocationID has it set in the response.
func TestWorldServerQueryCharacterWithLocationIDPopulatesField(t *testing.T) {
	char := makeCharacter()
	locID := ulid.Make()
	char.LocationID = &locID
	caps := newWorldCaps(&fakeWorldQuerier{characterResult: char})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	resp, err := srv.QueryCharacter(context.Background(), &hostv1.QueryCharacterRequest{
		CharacterId: validWorldULID,
	})
	require.NoError(t, err)
	assert.Equal(t, locID.String(), resp.GetLocationId())
}

// TestWorldServerQueryCharacterReturnsNotFoundForMissingCharacter maps
// world.ErrNotFound → codes.NotFound.
func TestWorldServerQueryCharacterReturnsNotFoundForMissingCharacter(t *testing.T) {
	caps := newWorldCaps(&fakeWorldQuerier{characterErr: world.ErrNotFound})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	_, err := srv.QueryCharacter(context.Background(), &hostv1.QueryCharacterRequest{
		CharacterId: validWorldULID,
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

// TestWorldServerQueryCharacterReturnsOpaqueInternalErrorOnFailure verifies
// opacity of unexpected querier errors.
func TestWorldServerQueryCharacterReturnsOpaqueInternalErrorOnFailure(t *testing.T) {
	caps := newWorldCaps(&fakeWorldQuerier{characterErr: errors.New("secret")})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	_, err := srv.QueryCharacter(context.Background(), &hostv1.QueryCharacterRequest{
		CharacterId: validWorldULID,
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal error", st.Message())
	assert.NotContains(t, st.Message(), "secret")
}

// TestWorldServerQueryCharacterReturnsInvalidArgumentForBadULID verifies
// InvalidArgument on unparseable character_id.
func TestWorldServerQueryCharacterReturnsInvalidArgumentForBadULID(t *testing.T) {
	caps := newWorldCaps(&fakeWorldQuerier{})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	_, err := srv.QueryCharacter(context.Background(), &hostv1.QueryCharacterRequest{
		CharacterId: "not-a-ulid",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// ============================================================================
// QueryLocationCharacters
// ============================================================================

// TestWorldServerQueryLocationCharactersStampsPluginSubject verifies subject
// stamping for the characters-at-location query.
func TestWorldServerQueryLocationCharactersStampsPluginSubject(t *testing.T) {
	char := makeCharacter()
	caps := newWorldCaps(&fakeWorldQuerier{charactersByLocationResult: []*world.Character{char}})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	_, err := srv.QueryLocationCharacters(context.Background(), &hostv1.QueryLocationCharactersRequest{
		LocationId: validWorldULID,
	})
	require.NoError(t, err)
	assert.Equal(t, "core-scenes", caps.lastQueriedPlugin)
}

// TestWorldServerQueryLocationCharactersReturnsCharacterSummaries verifies that
// QueryLocationCharacters maps domain characters to lightweight CharacterSummary
// protos (id and name only).
func TestWorldServerQueryLocationCharactersReturnsCharacterSummaries(t *testing.T) {
	char := makeCharacter()
	caps := newWorldCaps(&fakeWorldQuerier{charactersByLocationResult: []*world.Character{char}})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	resp, err := srv.QueryLocationCharacters(context.Background(), &hostv1.QueryLocationCharactersRequest{
		LocationId: validWorldULID,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetCharacters(), 1)
	assert.Equal(t, char.ID.String(), resp.GetCharacters()[0].GetId())
	assert.Equal(t, char.Name, resp.GetCharacters()[0].GetName())
}

// TestWorldServerQueryLocationCharactersReturnsOpaqueInternalErrorOnFailure
// verifies opacity of unexpected querier errors.
func TestWorldServerQueryLocationCharactersReturnsOpaqueInternalErrorOnFailure(t *testing.T) {
	caps := newWorldCaps(&fakeWorldQuerier{charactersByLocationErr: errors.New("secret")})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	_, err := srv.QueryLocationCharacters(context.Background(), &hostv1.QueryLocationCharactersRequest{
		LocationId: validWorldULID,
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal error", st.Message())
	assert.NotContains(t, st.Message(), "secret")
}

// TestWorldServerQueryLocationCharactersReturnsInvalidArgumentForBadULID
// verifies InvalidArgument on unparseable location_id.
func TestWorldServerQueryLocationCharactersReturnsInvalidArgumentForBadULID(t *testing.T) {
	caps := newWorldCaps(&fakeWorldQuerier{})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	_, err := srv.QueryLocationCharacters(context.Background(), &hostv1.QueryLocationCharactersRequest{
		LocationId: "not-a-ulid",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// ============================================================================
// QueryObject
// ============================================================================

// TestWorldServerQueryObjectStampsPluginSubject verifies subject stamping.
func TestWorldServerQueryObjectStampsPluginSubject(t *testing.T) {
	obj := makeObject()
	caps := newWorldCaps(&fakeWorldQuerier{objectResult: obj})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	_, err := srv.QueryObject(context.Background(), &hostv1.QueryObjectRequest{
		ObjectId: validWorldULID,
	})
	require.NoError(t, err)
	assert.Equal(t, "core-scenes", caps.lastQueriedPlugin)
}

// TestWorldServerQueryObjectReturnsObjectFields verifies field mapping for the
// domain Object → wire QueryObjectResponse conversion.
func TestWorldServerQueryObjectReturnsObjectFields(t *testing.T) {
	obj := makeObject()
	caps := newWorldCaps(&fakeWorldQuerier{objectResult: obj})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	resp, err := srv.QueryObject(context.Background(), &hostv1.QueryObjectRequest{
		ObjectId: validWorldULID,
	})
	require.NoError(t, err)
	assert.Equal(t, obj.ID.String(), resp.GetId())
	assert.Equal(t, obj.Name, resp.GetName())
	assert.Equal(t, obj.Description, resp.GetDescription())
	assert.Equal(t, obj.IsContainer, resp.GetIsContainer())
	// makeObject places the object in a location.
	assert.NotEmpty(t, resp.GetLocationId())
}

// TestWorldServerQueryObjectReturnsNotFoundForMissingObject maps
// world.ErrNotFound → codes.NotFound.
func TestWorldServerQueryObjectReturnsNotFoundForMissingObject(t *testing.T) {
	caps := newWorldCaps(&fakeWorldQuerier{objectErr: world.ErrNotFound})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	_, err := srv.QueryObject(context.Background(), &hostv1.QueryObjectRequest{
		ObjectId: validWorldULID,
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

// TestWorldServerQueryObjectReturnsOpaqueInternalErrorOnFailure verifies
// opacity of unexpected querier errors.
func TestWorldServerQueryObjectReturnsOpaqueInternalErrorOnFailure(t *testing.T) {
	caps := newWorldCaps(&fakeWorldQuerier{objectErr: errors.New("secret")})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	_, err := srv.QueryObject(context.Background(), &hostv1.QueryObjectRequest{
		ObjectId: validWorldULID,
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal error", st.Message())
	assert.NotContains(t, st.Message(), "secret")
}

// TestWorldServerQueryObjectReturnsInvalidArgumentForBadULID verifies
// InvalidArgument on unparseable object_id.
func TestWorldServerQueryObjectReturnsInvalidArgumentForBadULID(t *testing.T) {
	caps := newWorldCaps(&fakeWorldQuerier{})
	srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
	_, err := srv.QueryObject(context.Background(), &hostv1.QueryObjectRequest{
		ObjectId: "not-a-ulid",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}
