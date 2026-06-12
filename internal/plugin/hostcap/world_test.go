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

// requireWorldNotFound asserts err is a gRPC NotFound status.
func requireWorldNotFound(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

// ============================================================================
// QueryLocation
// ============================================================================

func TestWorldServerQueryLocation(t *testing.T) {
	loc := makeLocation()
	tests := []struct {
		name       string
		querier    *fakeWorldQuerier
		locationID string
		check      func(t *testing.T, caps *worldHostCaps, resp *hostv1.QueryLocationResponse, err error)
	}{
		{
			name:       "returns location fields and stamps the plugin subject",
			querier:    &fakeWorldQuerier{locationResult: loc},
			locationID: validWorldULID,
			check: func(t *testing.T, caps *worldHostCaps, resp *hostv1.QueryLocationResponse, err error) {
				require.NoError(t, err)
				assert.Equal(t, "core-scenes", caps.lastQueriedPlugin,
					"QueryLocation must stamp the plugin name via WorldQuerier(pluginName)")
				assert.Equal(t, loc.ID.String(), resp.GetId())
				assert.Equal(t, loc.Name, resp.GetName())
				assert.Equal(t, loc.Description, resp.GetDescription())
				assert.Equal(t, string(loc.Type), resp.GetType())
			},
		},
		{
			name:       "maps world.ErrNotFound to NotFound",
			querier:    &fakeWorldQuerier{locationErr: world.ErrNotFound},
			locationID: validWorldULID,
			check: func(t *testing.T, _ *worldHostCaps, _ *hostv1.QueryLocationResponse, err error) {
				requireWorldNotFound(t, err)
			},
		},
		{
			name:       "returns opaque internal error on unexpected failure",
			querier:    &fakeWorldQuerier{locationErr: errors.New("secret db conn string")},
			locationID: validWorldULID,
			check: func(t *testing.T, _ *worldHostCaps, _ *hostv1.QueryLocationResponse, err error) {
				requireOpaqueInternal(t, err)
			},
		},
		{
			// Contract violation: a nil location with no error must fail closed
			// (Internal), never nil-dereference loc.ID.
			name:       "fails closed on a nil result without error",
			querier:    &fakeWorldQuerier{}, // GetLocation returns (nil, nil)
			locationID: validWorldULID,
			check: func(t *testing.T, _ *worldHostCaps, _ *hostv1.QueryLocationResponse, err error) {
				requireOpaqueInternal(t, err)
			},
		},
		{
			name:       "returns InvalidArgument for an unparseable location id",
			querier:    &fakeWorldQuerier{},
			locationID: "not-a-ulid",
			check: func(t *testing.T, _ *worldHostCaps, _ *hostv1.QueryLocationResponse, err error) {
				requireInvalidArgument(t, err)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			caps := newWorldCaps(tc.querier)
			srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
			resp, err := srv.QueryLocation(context.Background(), &hostv1.QueryLocationRequest{
				LocationId: tc.locationID,
			})
			tc.check(t, caps, resp, err)
		})
	}
}

// ============================================================================
// QueryCharacter
// ============================================================================

func TestWorldServerQueryCharacter(t *testing.T) {
	char := makeCharacter()
	located := makeCharacter()
	locID := ulid.Make()
	located.LocationID = &locID

	tests := []struct {
		name        string
		querier     *fakeWorldQuerier
		characterID string
		check       func(t *testing.T, caps *worldHostCaps, resp *hostv1.QueryCharacterResponse, err error)
	}{
		{
			name:        "returns character fields, empty location, and stamps the plugin subject",
			querier:     &fakeWorldQuerier{characterResult: char},
			characterID: validWorldULID,
			check: func(t *testing.T, caps *worldHostCaps, resp *hostv1.QueryCharacterResponse, err error) {
				require.NoError(t, err)
				assert.Equal(t, "core-scenes", caps.lastQueriedPlugin)
				assert.Equal(t, char.ID.String(), resp.GetId())
				assert.Equal(t, char.PlayerID.String(), resp.GetPlayerId())
				assert.Equal(t, char.Name, resp.GetName())
				assert.Equal(t, char.Description, resp.GetDescription())
				// No location set on the test character — location_id MUST be empty.
				assert.Empty(t, resp.GetLocationId())
			},
		},
		{
			name:        "populates location_id when the character has a location",
			querier:     &fakeWorldQuerier{characterResult: located},
			characterID: validWorldULID,
			check: func(t *testing.T, _ *worldHostCaps, resp *hostv1.QueryCharacterResponse, err error) {
				require.NoError(t, err)
				assert.Equal(t, locID.String(), resp.GetLocationId())
			},
		},
		{
			name:        "maps world.ErrNotFound to NotFound",
			querier:     &fakeWorldQuerier{characterErr: world.ErrNotFound},
			characterID: validWorldULID,
			check: func(t *testing.T, _ *worldHostCaps, _ *hostv1.QueryCharacterResponse, err error) {
				requireWorldNotFound(t, err)
			},
		},
		{
			name:        "returns opaque internal error on unexpected failure",
			querier:     &fakeWorldQuerier{characterErr: errors.New("secret")},
			characterID: validWorldULID,
			check: func(t *testing.T, _ *worldHostCaps, _ *hostv1.QueryCharacterResponse, err error) {
				requireOpaqueInternal(t, err)
			},
		},
		{
			// Contract violation: a nil character with no error must fail closed.
			name:        "fails closed on a nil result without error",
			querier:     &fakeWorldQuerier{}, // GetCharacter returns (nil, nil)
			characterID: validWorldULID,
			check: func(t *testing.T, _ *worldHostCaps, _ *hostv1.QueryCharacterResponse, err error) {
				requireOpaqueInternal(t, err)
			},
		},
		{
			name:        "returns InvalidArgument for an unparseable character id",
			querier:     &fakeWorldQuerier{},
			characterID: "not-a-ulid",
			check: func(t *testing.T, _ *worldHostCaps, _ *hostv1.QueryCharacterResponse, err error) {
				requireInvalidArgument(t, err)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			caps := newWorldCaps(tc.querier)
			srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
			resp, err := srv.QueryCharacter(context.Background(), &hostv1.QueryCharacterRequest{
				CharacterId: tc.characterID,
			})
			tc.check(t, caps, resp, err)
		})
	}
}

// ============================================================================
// QueryLocationCharacters
// ============================================================================

func TestWorldServerQueryLocationCharacters(t *testing.T) {
	char := makeCharacter()
	tests := []struct {
		name       string
		querier    *fakeWorldQuerier
		locationID string
		check      func(t *testing.T, caps *worldHostCaps, resp *hostv1.QueryLocationCharactersResponse, err error)
	}{
		{
			name:       "maps characters to summaries and stamps the plugin subject",
			querier:    &fakeWorldQuerier{charactersByLocationResult: []*world.Character{char}},
			locationID: validWorldULID,
			check: func(t *testing.T, caps *worldHostCaps, resp *hostv1.QueryLocationCharactersResponse, err error) {
				require.NoError(t, err)
				assert.Equal(t, "core-scenes", caps.lastQueriedPlugin)
				require.Len(t, resp.GetCharacters(), 1)
				assert.Equal(t, char.ID.String(), resp.GetCharacters()[0].GetId())
				assert.Equal(t, char.Name, resp.GetCharacters()[0].GetName())
			},
		},
		{
			name:       "returns opaque internal error on unexpected failure",
			querier:    &fakeWorldQuerier{charactersByLocationErr: errors.New("secret")},
			locationID: validWorldULID,
			check: func(t *testing.T, _ *worldHostCaps, _ *hostv1.QueryLocationCharactersResponse, err error) {
				requireOpaqueInternal(t, err)
			},
		},
		{
			name:       "returns InvalidArgument for an unparseable location id",
			querier:    &fakeWorldQuerier{},
			locationID: "not-a-ulid",
			check: func(t *testing.T, _ *worldHostCaps, _ *hostv1.QueryLocationCharactersResponse, err error) {
				requireInvalidArgument(t, err)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			caps := newWorldCaps(tc.querier)
			srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
			resp, err := srv.QueryLocationCharacters(context.Background(), &hostv1.QueryLocationCharactersRequest{
				LocationId: tc.locationID,
			})
			tc.check(t, caps, resp, err)
		})
	}
}

// ============================================================================
// QueryObject
// ============================================================================

func TestWorldServerQueryObject(t *testing.T) {
	obj := makeObject()
	tests := []struct {
		name     string
		querier  *fakeWorldQuerier
		objectID string
		check    func(t *testing.T, caps *worldHostCaps, resp *hostv1.QueryObjectResponse, err error)
	}{
		{
			name:     "returns object fields and stamps the plugin subject",
			querier:  &fakeWorldQuerier{objectResult: obj},
			objectID: validWorldULID,
			check: func(t *testing.T, caps *worldHostCaps, resp *hostv1.QueryObjectResponse, err error) {
				require.NoError(t, err)
				assert.Equal(t, "core-scenes", caps.lastQueriedPlugin)
				assert.Equal(t, obj.ID.String(), resp.GetId())
				assert.Equal(t, obj.Name, resp.GetName())
				assert.Equal(t, obj.Description, resp.GetDescription())
				assert.Equal(t, obj.IsContainer, resp.GetIsContainer())
				// makeObject places the object in a location.
				assert.NotEmpty(t, resp.GetLocationId())
			},
		},
		{
			name:     "maps world.ErrNotFound to NotFound",
			querier:  &fakeWorldQuerier{objectErr: world.ErrNotFound},
			objectID: validWorldULID,
			check: func(t *testing.T, _ *worldHostCaps, _ *hostv1.QueryObjectResponse, err error) {
				requireWorldNotFound(t, err)
			},
		},
		{
			name:     "returns opaque internal error on unexpected failure",
			querier:  &fakeWorldQuerier{objectErr: errors.New("secret")},
			objectID: validWorldULID,
			check: func(t *testing.T, _ *worldHostCaps, _ *hostv1.QueryObjectResponse, err error) {
				requireOpaqueInternal(t, err)
			},
		},
		{
			// Contract violation: a nil object with no error must fail closed.
			name:     "fails closed on a nil result without error",
			querier:  &fakeWorldQuerier{}, // GetObject returns (nil, nil)
			objectID: validWorldULID,
			check: func(t *testing.T, _ *worldHostCaps, _ *hostv1.QueryObjectResponse, err error) {
				requireOpaqueInternal(t, err)
			},
		},
		{
			name:     "returns InvalidArgument for an unparseable object id",
			querier:  &fakeWorldQuerier{},
			objectID: "not-a-ulid",
			check: func(t *testing.T, _ *worldHostCaps, _ *hostv1.QueryObjectResponse, err error) {
				requireInvalidArgument(t, err)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			caps := newWorldCaps(tc.querier)
			srv := hostcap.NewWorldQueryServer(hostcap.NewBase(caps, "core-scenes"))
			resp, err := srv.QueryObject(context.Background(), &hostv1.QueryObjectRequest{
				ObjectId: tc.objectID,
			})
			tc.check(t, caps, resp, err)
		})
	}
}
