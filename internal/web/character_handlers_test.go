// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// The character-surface proxies.
//
// A proxy has exactly two obligations and both are asserted rather than
// inspected by eye: the session token comes from the HEADER and never from the
// request body, and the paired facade RPC is the one that gets called. The
// routing census (characterWebProxyRPCs) pins the same two conjuncts
// structurally; these specs pin them behaviourally, on the values that actually
// crossed the boundary.

// recordingCharacterAccessClient is a CharacterAccessClient that records the
// request it was handed and returns a preset response or error.
//
// It records only the methods these specs drive; every other member of the
// interface returns zero values, because a proxy spec that could accidentally
// pass by reaching the WRONG facade method would not be pinning the second
// conjunct at all.
type recordingCharacterAccessClient struct {
	setDefaultReq  *characteraccessv1.SetDefaultCharacterRequest
	setDefaultResp *characteraccessv1.SetDefaultCharacterResponse
	setDefaultErr  error
}

var _ CharacterAccessClient = (*recordingCharacterAccessClient)(nil)

func (c *recordingCharacterAccessClient) GetCharacterProfile(context.Context, *characteraccessv1.GetCharacterProfileRequest) (*characteraccessv1.GetCharacterProfileResponse, error) {
	return nil, nil
}

func (c *recordingCharacterAccessClient) ListMyCharacters(context.Context, *characteraccessv1.ListMyCharactersRequest) (*characteraccessv1.ListMyCharactersResponse, error) {
	return nil, nil
}

func (c *recordingCharacterAccessClient) GetMyCharacter(context.Context, *characteraccessv1.GetMyCharacterRequest) (*characteraccessv1.GetMyCharacterResponse, error) {
	return nil, nil
}

func (c *recordingCharacterAccessClient) UpdateCharacterProfile(context.Context, *characteraccessv1.UpdateCharacterProfileRequest) (*characteraccessv1.UpdateCharacterProfileResponse, error) {
	return nil, nil
}

func (c *recordingCharacterAccessClient) UpdateCharacterDescription(context.Context, *characteraccessv1.UpdateCharacterDescriptionRequest) (*characteraccessv1.UpdateCharacterDescriptionResponse, error) {
	return nil, nil
}

func (c *recordingCharacterAccessClient) SetDefaultCharacter(_ context.Context, req *characteraccessv1.SetDefaultCharacterRequest) (*characteraccessv1.SetDefaultCharacterResponse, error) {
	c.setDefaultReq = req
	return c.setDefaultResp, c.setDefaultErr
}

func (c *recordingCharacterAccessClient) ListCharacterDirectory(context.Context, *characteraccessv1.ListCharacterDirectoryRequest) (*characteraccessv1.ListCharacterDirectoryResponse, error) {
	return nil, nil
}

// TestWebSetDefaultCharacterForwardsTheHeaderTokenAndTheCharacterIdVerbatim is
// the proxy's whole contract.
//
// The token is read from the header, so the assertion that it arrived on the
// facade request is what proves the gateway did not accept a body-supplied
// identity — WebSetDefaultCharacterRequest declares no token field, and this
// spec is what keeps that true in behaviour rather than only in the descriptor.
func TestWebSetDefaultCharacterForwardsTheHeaderTokenAndTheCharacterIdVerbatim(t *testing.T) {
	const token = "tok-set-default"
	cc := &recordingCharacterAccessClient{
		setDefaultResp: &characteraccessv1.SetDefaultCharacterResponse{
			Characters: []*characteraccessv1.OwnCharacter{
				{Id: "char-01", Name: "Ada", Status: "active", Version: 3},
				{Id: "char-02", Name: "Withdrawn", Status: "retired", Version: 9},
			},
		},
	}
	h := NewHandler(&mockCoreClient{}, WithCharacterAccessClient(cc))

	req := connect.NewRequest(&webv1.WebSetDefaultCharacterRequest{CharacterId: "char-01"})
	req.Header().Set(headerInjectSessionToken, token)

	resp, err := h.WebSetDefaultCharacter(context.Background(), req)
	require.NoError(t, err)

	require.NotNil(t, cc.setDefaultReq, "the paired facade RPC is the one that was called")
	assert.Equal(t, "char-01", cc.setDefaultReq.GetCharacterId())
	assert.Equal(t, token, cc.setDefaultReq.GetPlayerSessionToken(),
		"the token came from the X-Session-Token header, which is the only place it can come from")

	require.Len(t, resp.Msg.GetCharacters(), 2,
		"the facade's roster is re-exported verbatim, retired entries included")
	assert.Equal(t, "char-01", resp.Msg.GetCharacters()[0].GetId())
	assert.Equal(t, "retired", resp.Msg.GetCharacters()[1].GetStatus())
}

// TestWebSetDefaultCharacterPassesAFacadeErrorThroughAsIs pins that the gateway
// neither reclassifies nor re-wraps a facade refusal. Reclassifying would put a
// second, divergent authorization vocabulary in front of the one that decides.
func TestWebSetDefaultCharacterPassesAFacadeErrorThroughAsIs(t *testing.T) {
	facadeErr := oops.Code("RPC_FAILED").Errorf("facade unavailable")
	cc := &recordingCharacterAccessClient{setDefaultErr: facadeErr}
	h := NewHandler(&mockCoreClient{}, WithCharacterAccessClient(cc))

	_, err := h.WebSetDefaultCharacter(context.Background(),
		connect.NewRequest(&webv1.WebSetDefaultCharacterRequest{CharacterId: "char-01"}))
	require.Error(t, err)
	assert.ErrorIs(t, err, facadeErr)
}

// TestWebSetDefaultCharacterReturnsUnimplementedWhenClientAbsent pins the
// nil-client guard. The CodeUnimplemented it produces means an UNWIRED CLIENT in
// cmd/holomush, never an unbuilt facade — the facade handler shipped in the same
// change as this proxy.
func TestWebSetDefaultCharacterReturnsUnimplementedWhenClientAbsent(t *testing.T) {
	h := NewHandler(&mockCoreClient{})

	_, err := h.WebSetDefaultCharacter(context.Background(),
		connect.NewRequest(&webv1.WebSetDefaultCharacterRequest{CharacterId: "char-01"}))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnimplemented, connectErr.Code())
}
