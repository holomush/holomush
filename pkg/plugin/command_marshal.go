// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"

// CommandRequestToProto converts the canonical CommandRequest into its proto
// wire form. It is the single host-side site mapping CommandRequest fields onto
// pluginv1.CommandRequest (used by the binary-plugin host on send); pair every
// edit here with CommandRequestFromProto so a field added to CommandRequest is
// carried in both directions. TestCommandRequestProtoRoundTripCarriesEveryField
// fails if the two drift — this is the structural guard behind holomush-dble7,
// where the receive side silently dropped connection_id (holomush-peqfu).
func CommandRequestToProto(cmd CommandRequest) *pluginv1.CommandRequest {
	return &pluginv1.CommandRequest{
		Command:       cmd.Command,
		Args:          cmd.Args,
		RawInput:      cmd.InvokedAs,
		CharacterId:   cmd.CharacterID,
		CharacterName: cmd.CharacterName,
		LocationId:    cmd.LocationID,
		SessionId:     cmd.SessionID,
		PlayerId:      cmd.PlayerID,
		ConnectionId:  cmd.ConnectionID,
	}
}

// CommandRequestFromProto converts a proto CommandRequest back into the
// canonical CommandRequest (used by the binary-plugin SDK adapter on receive).
// See CommandRequestToProto for the parity contract.
func CommandRequestFromProto(p *pluginv1.CommandRequest) CommandRequest {
	return CommandRequest{
		Command:       p.GetCommand(),
		Args:          p.GetArgs(),
		CharacterID:   p.GetCharacterId(),
		CharacterName: p.GetCharacterName(),
		LocationID:    p.GetLocationId(),
		SessionID:     p.GetSessionId(),
		PlayerID:      p.GetPlayerId(),
		InvokedAs:     p.GetRawInput(),
		ConnectionID:  p.GetConnectionId(),
	}
}
