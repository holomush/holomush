// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// --- Tests for parseRequestID ---

func TestParseRequestIDValidULID(t *testing.T) {
	// 01ARZ3NDEKTSV4RRFFQ69G5FAV is a well-formed ULID.
	b, err := parseRequestID("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	require.NoError(t, err)
	assert.Len(t, b, 16, "ULID must yield 16 bytes")
}

func TestParseRequestIDValidHex(t *testing.T) {
	// 32-char hex of 16 bytes.
	b, err := parseRequestID("0102030405060708090a0b0c0d0e0f10")
	require.NoError(t, err)
	assert.Len(t, b, 16)
	assert.Equal(t, byte(0x01), b[0])
	assert.Equal(t, byte(0x10), b[15])
}

func TestParseRequestIDInvalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"short_hex", "deadbeef"},
		{"not_ulid_not_hex", "this-is-not-a-valid-request-id"},
		{"odd_hex", "0102030405060708090a0b0c0d0e0f"},         // 30 chars
		{"bad_hex_chars", "ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ"}, // 32 chars but not hex
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseRequestID(tc.input)
			assert.Error(t, err, "expected error for input %q", tc.input)
		})
	}
}

// --- Tests for runAdminApprove ---

func TestRunAdminApproveSuccess(t *testing.T) {
	rid := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

	h := &fakeAdminHandler{
		onAuthenticate: func(_ context.Context, _ *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
			return connect.NewResponse(&adminv1.AuthenticateResponse{SessionToken: "tok-approve"}), nil
		},
		onApprove: func(_ context.Context, req *connect.Request[adminv1.ApproveRequest]) (*connect.Response[adminv1.ApproveResponse], error) {
			assert.Equal(t, "tok-approve", req.Msg.GetSessionToken())
			assert.Equal(t, rid, req.Msg.GetRequestId())
			return connect.NewResponse(&adminv1.ApproveResponse{}), nil
		},
	}
	client, cleanup := newFakeAdminServer(t, h)
	defer cleanup()

	cmd, out := newTestCmdWithIO("operator2\nsecret\n654321\n")
	cmd.SetContext(t.Context())

	err := runAdminApprove(cmd, client, "0102030405060708090a0b0c0d0e0f10", rid)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Approved request")
}

func TestRunAdminApproveAuthFails(t *testing.T) {
	h := &fakeAdminHandler{
		onAuthenticate: func(_ context.Context, _ *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
			return nil, connect.NewError(connect.CodeUnauthenticated, nil)
		},
	}
	client, cleanup := newFakeAdminServer(t, h)
	defer cleanup()

	rid := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	cmd, _ := newTestCmdWithIO("operator2\nbadpass\n000000\n")
	cmd.SetContext(t.Context())

	err := runAdminApprove(cmd, client, "raw-input", rid)
	require.Error(t, err)
}

func TestRunAdminApproveServerError(t *testing.T) {
	h := &fakeAdminHandler{
		onAuthenticate: func(_ context.Context, _ *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error) {
			return connect.NewResponse(&adminv1.AuthenticateResponse{SessionToken: "tok-ok"}), nil
		},
		onApprove: func(_ context.Context, _ *connect.Request[adminv1.ApproveRequest]) (*connect.Response[adminv1.ApproveResponse], error) {
			return nil, connect.NewError(connect.CodeNotFound, nil)
		},
	}
	client, cleanup := newFakeAdminServer(t, h)
	defer cleanup()

	rid := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	cmd, _ := newTestCmdWithIO("operator2\npass\n123456\n")
	cmd.SetContext(t.Context())

	err := runAdminApprove(cmd, client, "raw-input", rid)
	require.Error(t, err)
}
