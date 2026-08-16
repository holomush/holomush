// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"context"
	"net"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
	"github.com/holomush/holomush/pkg/errutil"
	worldv1 "github.com/holomush/holomush/pkg/proto/holomush/world/v1"
)

func startWorldServer(t *testing.T, svc *world.Service) worldv1.WorldServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-memory bufconn for tests
	worldv1.RegisterWorldServiceServer(srv, world.NewGRPCServer(svc))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { srv.Stop(); _ = lis.Close() })

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()), // nosemgrep: go.grpc.tls.grpc-client-new-insecure-connection.grpc-client-new-insecure-connection
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return worldv1.NewWorldServiceClient(conn)
}

func TestWorldServiceServer_GetLocation(t *testing.T) {
	locID := ulid.MustNew(1, nil)
	ownerID := ulid.MustNew(2, nil)
	subjectID := access.CharacterSubject(ownerID.String())

	t.Run("returns location info when authorized", func(t *testing.T) {
		locRepo := worldtest.NewMockLocationRepository(t)
		engine := policytest.NewGrantEngine()
		engine.Grant(subjectID, "read", "location:"+locID.String())

		locRepo.EXPECT().Get(mock.Anything, locID).Return(&world.Location{
			ID:          locID,
			Name:        "Town Square",
			Description: "A bustling town square.",
			Type:        world.LocationTypePersistent,
			OwnerID:     &ownerID,
		}, nil)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: locRepo,
			Engine:       engine,
		})

		client := startWorldServer(t, svc)
		resp, err := client.GetLocation(context.Background(), &worldv1.GetLocationRequest{
			SubjectId:  ownerID.String(),
			LocationId: locID.String(),
		})
		require.NoError(t, err)
		require.NotNil(t, resp.GetLocation())
		assert.Equal(t, locID.String(), resp.GetLocation().GetId())
		assert.Equal(t, "Town Square", resp.GetLocation().GetName())
		assert.Equal(t, "A bustling town square.", resp.GetLocation().GetDescription())
		assert.Equal(t, "persistent", resp.GetLocation().GetType())
		assert.Equal(t, ownerID.String(), resp.GetLocation().GetOwnerId())
	})

	t.Run("returns InvalidArgument for malformed location ID", func(t *testing.T) {
		svc := world.NewService(world.ServiceConfig{
			LocationRepo: worldtest.NewMockLocationRepository(t),
			Engine:       policytest.AllowAllEngine(),
		})

		client := startWorldServer(t, svc)
		_, err := client.GetLocation(context.Background(), &worldv1.GetLocationRequest{
			SubjectId:  ownerID.String(),
			LocationId: "not-a-ulid",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("returns NotFound when location does not exist", func(t *testing.T) {
		locRepo := worldtest.NewMockLocationRepository(t)
		engine := policytest.NewGrantEngine()
		engine.Grant(subjectID, "read", "location:"+locID.String())

		locRepo.EXPECT().Get(mock.Anything, locID).Return(nil, world.ErrNotFound)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: locRepo,
			Engine:       engine,
		})

		client := startWorldServer(t, svc)
		_, err := client.GetLocation(context.Background(), &worldv1.GetLocationRequest{
			SubjectId:  ownerID.String(),
			LocationId: locID.String(),
		})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("returns PermissionDenied when not authorized", func(t *testing.T) {
		locRepo := worldtest.NewMockLocationRepository(t)
		engine := policytest.NewGrantEngine()
		// No grant — should deny

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: locRepo,
			Engine:       engine,
		})

		client := startWorldServer(t, svc)
		_, err := client.GetLocation(context.Background(), &worldv1.GetLocationRequest{
			SubjectId:  ownerID.String(),
			LocationId: locID.String(),
		})
		require.Error(t, err)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
	})
}

func TestWorldServiceServer_GetCharacter(t *testing.T) {
	charID := ulid.MustNew(10, nil)
	playerID := ulid.MustNew(11, nil)
	locID := ulid.MustNew(12, nil)
	subjectID := access.CharacterSubject(charID.String())

	t.Run("returns character info when authorized", func(t *testing.T) {
		charRepo := worldtest.NewMockCharacterRepository(t)
		engine := policytest.NewGrantEngine()
		engine.Grant(subjectID, "read", "character:"+charID.String())

		charRepo.EXPECT().Get(mock.Anything, charID).Return(&world.Character{
			ID:          charID,
			PlayerID:    playerID,
			Name:        "Hero",
			Description: "A brave adventurer.",
			LocationID:  &locID,
		}, nil)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: charRepo,
			Engine:        engine,
		})

		client := startWorldServer(t, svc)
		resp, err := client.GetCharacter(context.Background(), &worldv1.GetCharacterRequest{
			SubjectId:   charID.String(),
			CharacterId: charID.String(),
		})
		require.NoError(t, err)
		require.NotNil(t, resp.GetCharacter())
		assert.Equal(t, charID.String(), resp.GetCharacter().GetId())
		assert.Equal(t, playerID.String(), resp.GetCharacter().GetPlayerId())
		assert.Equal(t, "Hero", resp.GetCharacter().GetName())
		assert.Equal(t, "A brave adventurer.", resp.GetCharacter().GetDescription())
		assert.Equal(t, locID.String(), resp.GetCharacter().GetLocationId())
	})

	t.Run("returns InvalidArgument for malformed character ID", func(t *testing.T) {
		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: worldtest.NewMockCharacterRepository(t),
			Engine:        policytest.AllowAllEngine(),
		})

		client := startWorldServer(t, svc)
		_, err := client.GetCharacter(context.Background(), &worldv1.GetCharacterRequest{
			SubjectId:   charID.String(),
			CharacterId: "bad",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestWorldServiceServer_ListCharactersAtLocation(t *testing.T) {
	locID := ulid.MustNew(20, nil)
	charID := ulid.MustNew(21, nil)
	playerID := ulid.MustNew(22, nil)
	subjectID := access.CharacterSubject(charID.String())

	t.Run("returns characters at location", func(t *testing.T) {
		charRepo := worldtest.NewMockCharacterRepository(t)
		engine := policytest.NewGrantEngine()
		engine.Grant(subjectID, "list_characters", "location:"+locID.String())

		charRepo.EXPECT().GetByLocation(mock.Anything, locID, world.ListOptions{}).Return([]*world.Character{
			{
				ID:         charID,
				PlayerID:   playerID,
				Name:       "Hero",
				LocationID: &locID,
			},
		}, nil)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: charRepo,
			Engine:        engine,
		})

		client := startWorldServer(t, svc)
		resp, err := client.ListCharactersAtLocation(context.Background(), &worldv1.ListCharactersAtLocationRequest{
			SubjectId:  charID.String(),
			LocationId: locID.String(),
		})
		require.NoError(t, err)
		require.Len(t, resp.GetCharacters(), 1)
		assert.Equal(t, charID.String(), resp.GetCharacters()[0].GetId())
		assert.Equal(t, "Hero", resp.GetCharacters()[0].GetName())
	})

	t.Run("returns InvalidArgument for malformed location ID", func(t *testing.T) {
		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: worldtest.NewMockCharacterRepository(t),
			Engine:        policytest.AllowAllEngine(),
		})

		client := startWorldServer(t, svc)
		_, err := client.ListCharactersAtLocation(context.Background(), &worldv1.ListCharactersAtLocationRequest{
			SubjectId:  charID.String(),
			LocationId: "invalid",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestWorldServiceServer_ListExits(t *testing.T) {
	locID := ulid.MustNew(30, nil)
	destID := ulid.MustNew(31, nil)
	exitID := ulid.MustNew(32, nil)
	subjectID := access.CharacterSubject(ulid.MustNew(33, nil).String())

	t.Run("returns exits from location", func(t *testing.T) {
		exitRepo := worldtest.NewMockExitRepository(t)
		engine := policytest.NewGrantEngine()
		engine.Grant(subjectID, "read", "location:"+locID.String())

		exitRepo.EXPECT().ListFromLocation(mock.Anything, locID).Return([]*world.Exit{
			{
				ID:             exitID,
				Name:           "north",
				FromLocationID: locID,
				ToLocationID:   destID,
				Bidirectional:  true,
				ReturnName:     "south",
				Locked:         false,
			},
		}, nil)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: exitRepo,
			Engine:   engine,
		})

		client := startWorldServer(t, svc)
		resp, err := client.ListExits(context.Background(), &worldv1.ListExitsRequest{
			SubjectId:  ulid.MustNew(33, nil).String(),
			LocationId: locID.String(),
		})
		require.NoError(t, err)
		require.Len(t, resp.GetExits(), 1)
		e := resp.GetExits()[0]
		assert.Equal(t, exitID.String(), e.GetId())
		assert.Equal(t, "north", e.GetName())
		assert.Equal(t, locID.String(), e.GetFromLocationId())
		assert.Equal(t, destID.String(), e.GetToLocationId())
		assert.True(t, e.GetBidirectional())
		assert.Equal(t, "south", e.GetReturnName())
		assert.False(t, e.GetLocked())
	})

	t.Run("returns InvalidArgument for malformed location ID", func(t *testing.T) {
		svc := world.NewService(world.ServiceConfig{
			ExitRepo: worldtest.NewMockExitRepository(t),
			Engine:   policytest.AllowAllEngine(),
		})

		client := startWorldServer(t, svc)
		_, err := client.ListExits(context.Background(), &worldv1.ListExitsRequest{
			SubjectId:  ulid.MustNew(33, nil).String(),
			LocationId: "???",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

// TestComposedJobDenyCodeStillClassifiesAsPermissionDenied is the wire-level
// half of D-58: composing the principal kind into the deny code must not cost
// the code its gRPC classification.
//
// mapWorldError classifies on the "_ACCESS_DENIED" SUFFIX
// (internal/world/grpc_server.go:169), which is precisely why the qualifier is a
// PREFIX. The rejected alternative — CHARACTER_ACCESS_DENIED_JOB — reads fine
// and breaks this classification outright, turning every job denial into a
// codes.Internal.
//
// The error fed to the mapper is the REAL one a denied job produces, taken from
// the shipped-corpus fixture probe rather than hand-spelled, so a change to the
// composition cannot leave this test asserting a string production no longer
// emits.
func TestComposedJobDenyCodeStillClassifiesAsPermissionDenied(t *testing.T) {
	charID := ulid.MustParse(fixtureCharacter)
	svc, _ := newFixtureJobProbe(t, charID)

	// Provenance naming a DIFFERENT aggregate: denied by the instance-scoping
	// conjunct of seed:job-fixture-instance-scoped.
	denyErr := svc.UpdateCharacterDescription(context.Background(),
		world.JobCaller(fixtureJobName, world.Provenance{
			EventID:   fixtureEventID,
			EventType: fixtureEventType,
			Subject:   unrelatedCharacer,
		}), charID, "retired")
	require.Error(t, denyErr)
	errutil.AssertErrorCode(t, denyErr, "JOB_CHARACTER_ACCESS_DENIED")

	mapped := world.MapWorldErrorForTest(denyErr)

	st, ok := status.FromError(mapped)
	require.True(t, ok, "the mapper MUST return a gRPC status error")
	assert.Equal(t, codes.PermissionDenied, st.Code(),
		"a JOB_-qualified deny code MUST still classify as PermissionDenied — the qualifier is a "+
			"prefix precisely so the _ACCESS_DENIED suffix classification survives")
	assert.Equal(t, "access denied", st.Message(),
		"the mapper MUST keep returning its static message: no inner error text, and no "+
			"principal kind, crosses the wire boundary (.claude/rules/grpc-errors.md)")
}
