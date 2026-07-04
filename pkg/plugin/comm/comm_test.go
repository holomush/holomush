// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package comm_test

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/holomush/holomush/pkg/plugin/comm"
	commv1 "github.com/holomush/holomush/pkg/proto/holomush/comm/v1"
)

func TestParsePoseSetsNoSpaceForSemipose(t *testing.T) {
	got := comm.ParsePose(";", "waves") // invokedAs ";", raw "waves"
	require.Equal(t, "waves", got.Text)
	require.True(t, got.NoSpace)
}

func TestParsePoseStripsLeadingSigilFromRaw(t *testing.T) {
	got := comm.ParsePose("", ":waves") // no alias; sigil embedded in raw
	require.Equal(t, "waves", got.Text)
	require.False(t, got.NoSpace)
}

func TestParseOOCStyleFromPrefix(t *testing.T) {
	require.Equal(t, "pose", comm.ParseOOC(":laughs").Style)
	require.Equal(t, "semipose", comm.ParseOOC(";'s data is gone").Style)
	require.Equal(t, "say", comm.ParseOOC("brb").Style)
}

func TestBuildPoseProducesValidCommunicationContent(t *testing.T) {
	payload := comm.Pose(comm.Author{ID: "01H...", Name: "Alaric"}, ";", "waves") // returns string
	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(payload), &got))
	require.Equal(t, "01H...", got.GetActorId())
	require.Equal(t, "waves", got.GetText())
	require.True(t, got.GetNoSpace())
}

func TestBuildEmitOmitsActor(t *testing.T) {
	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(comm.Emit("the ground trembles")), &got))
	require.Equal(t, "", got.GetActorId())
	require.Equal(t, "the ground trembles", got.GetText())
}

func TestBuildOOCSetsPoseStyle(t *testing.T) {
	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(comm.OOC(comm.Author{ID: "01H", Name: "Alaric"}, ":laughs")), &got))
	require.Equal(t, "pose", got.GetOocStyle())
	require.Equal(t, "laughs", got.GetText())
}

func TestBuildSaySetsActorAndTrimsText(t *testing.T) {
	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(comm.Say(comm.Author{ID: "01H", Name: "Alaric"}, "  hello  ")), &got))
	require.Equal(t, "01H", got.GetActorId())
	require.Equal(t, "Alaric", got.GetActorDisplayName())
	require.Equal(t, "hello", got.GetText())
}

func TestBuildSanitizesInvalidUTF8(t *testing.T) {
	var payload string
	require.NotPanics(t, func() {
		payload = comm.Say(comm.Author{ID: "01H", Name: "Al\xffaric"}, "wa\xffves")
	})
	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(payload), &got))
	require.True(t, utf8.ValidString(got.GetText()))
	require.True(t, utf8.ValidString(got.GetActorDisplayName()))
}
