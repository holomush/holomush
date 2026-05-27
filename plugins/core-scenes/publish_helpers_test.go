// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/pkg/errutil"
)

func TestParseCallerCharacterIDAcceptsValidULID(t *testing.T) {
	t.Parallel()
	in := ulid.Make().String()
	got, err := parseCallerCharacterID(in)
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

func TestParseCallerCharacterIDRejectsEmpty(t *testing.T) {
	t.Parallel()
	_, err := parseCallerCharacterID("")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_PUBLISH_CALLER_REQUIRED")
}

func TestParseCallerCharacterIDRejectsMalformed(t *testing.T) {
	t.Parallel()
	_, err := parseCallerCharacterID("not-a-ulid")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_PUBLISH_CALLER_MALFORMED")
}

func TestMapStoreErrReturnsNilForNilError(t *testing.T) {
	t.Parallel()
	assert.NoError(t, mapStoreErr(context.Background(), nil))
}

func TestMapStoreErrMapsKnownCodeToFailedPrecondition(t *testing.T) {
	t.Parallel()
	err := oops.Code("SCENE_PUBLISH_ALREADY_ACTIVE").Errorf("active")
	mapped := mapStoreErr(context.Background(), err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(mapped))
	assert.Equal(t, "SCENE_PUBLISH_ALREADY_ACTIVE", status.Convert(mapped).Message(),
		"known application codes travel as the wire message for client discrimination")
}

func TestMapStoreErrMapsNotAVoterToPermissionDenied(t *testing.T) {
	t.Parallel()
	err := oops.Code("SCENE_PUBLISH_NOT_A_VOTER").Errorf("nope")
	mapped := mapStoreErr(context.Background(), err)
	assert.Equal(t, codes.PermissionDenied, status.Code(mapped))
	assert.Equal(t, "SCENE_PUBLISH_NOT_A_VOTER", status.Convert(mapped).Message())
}

func TestMapStoreErrTreatsInvalidTransitionAsOpaqueInternal(t *testing.T) {
	t.Parallel()
	// Per spec §5.2, SCENE_PUBLISH_INVALID_TRANSITION is a defensive
	// "impossible transition" signal (a bug indicator) → Internal, and the
	// code MUST NOT leak on the wire.
	err := oops.Code("SCENE_PUBLISH_INVALID_TRANSITION").Errorf("impossible")
	mapped := mapStoreErr(context.Background(), err)
	assert.Equal(t, codes.Internal, status.Code(mapped))
	assert.Equal(t, "internal error", status.Convert(mapped).Message())
}

func TestMapStoreErrMapsBareErrorToOpaqueInternal(t *testing.T) {
	t.Parallel()
	mapped := mapStoreErr(context.Background(), context.Canceled)
	assert.Equal(t, codes.Internal, status.Code(mapped))
	assert.Equal(t, "internal error", status.Convert(mapped).Message(),
		"bare errors MUST NOT leak inner detail past the trust boundary")
}
