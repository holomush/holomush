// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
	commv1 "github.com/holomush/holomush/pkg/proto/holomush/comm/v1"
)

// =============================================================================
// holo.comm.pose() / say() / ooc() / emit()
// =============================================================================

func TestHoloCommPoseReturnsConformingPayload(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()

	hostfunc.RegisterStdlib(ls) // sets global `holo`, now incl. holo.comm
	require.NoError(t, ls.DoString(`payload = holo.comm.pose("01H", "Alaric", ";", "waves")`))

	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(ls.GetGlobal("payload").String()), &got))
	require.Equal(t, "01H", got.GetActorId())
	require.Equal(t, "Alaric", got.GetActorDisplayName())
	require.Equal(t, "waves", got.GetText())
	require.True(t, got.GetNoSpace())
}

func TestHoloCommSayReturnsConformingPayload(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()

	hostfunc.RegisterStdlib(ls)
	require.NoError(t, ls.DoString(`payload = holo.comm.say("01H", "Alaric", "  hello  ")`))

	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(ls.GetGlobal("payload").String()), &got))
	require.Equal(t, "01H", got.GetActorId())
	require.Equal(t, "Alaric", got.GetActorDisplayName())
	require.Equal(t, "hello", got.GetText())
}

func TestHoloCommOOCReturnsConformingPayload(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()

	hostfunc.RegisterStdlib(ls)
	require.NoError(t, ls.DoString(`payload = holo.comm.ooc("01H", "Alaric", ":laughs")`))

	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(ls.GetGlobal("payload").String()), &got))
	require.Equal(t, "01H", got.GetActorId())
	require.Equal(t, "pose", got.GetOocStyle())
	require.Equal(t, "laughs", got.GetText())
}

func TestHoloCommEmitReturnsConformingPayload(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()

	hostfunc.RegisterStdlib(ls)
	require.NoError(t, ls.DoString(`payload = holo.comm.emit("the ground trembles")`))

	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(ls.GetGlobal("payload").String()), &got))
	require.Equal(t, "", got.GetActorId())
	require.Equal(t, "the ground trembles", got.GetText())
}
