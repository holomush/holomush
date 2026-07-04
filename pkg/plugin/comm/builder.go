// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package comm

import (
	"strings"

	"google.golang.org/protobuf/encoding/protojson"

	commv1 "github.com/holomush/holomush/pkg/proto/holomush/comm/v1"
)

var marshal = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false}

// build marshals to snake_case JSON and returns a string — EmitIntent.Payload
// (pkg/plugin/event.go:120) and Event.Payload (event.go:80) are string, and Lua
// LString is a string, so returning string avoids a conversion at every caller.
//
// Text and ActorDisplayName carry untrusted player input (the telnet path does
// no UTF-8 validation), and protojson.Marshal returns errInvalidUTF8 on a
// proto3 string field holding invalid UTF-8, so they are sanitized first.
func build(c *commv1.CommunicationContent) string {
	c.Text = strings.ToValidUTF8(c.Text, "�")
	c.ActorDisplayName = strings.ToValidUTF8(c.ActorDisplayName, "�")
	b, err := marshal.Marshal(c) // string fields sanitized to valid UTF-8 above; enum/bool cannot fail — so Marshal cannot error
	if err != nil {
		panic("comm.build: " + err.Error())
	}
	return string(b)
}

// Say builds the CommunicationContent JSON payload for a plain spoken line.
func Say(a Author, text string) string {
	return build(&commv1.CommunicationContent{ActorId: a.ID, ActorDisplayName: a.Name, Text: strings.TrimSpace(text)})
}

// Pose builds the CommunicationContent JSON payload for a pose or semipose,
// applying the ";"/":" grammar via ParsePose.
func Pose(a Author, invokedAs, raw string) string {
	p := ParsePose(invokedAs, raw)
	return build(&commv1.CommunicationContent{ActorId: a.ID, ActorDisplayName: a.Name, Text: p.Text, NoSpace: p.NoSpace})
}

// OOC builds the CommunicationContent JSON payload for an out-of-character
// message, classifying its surface style via ParseOOC.
func OOC(a Author, raw string) string {
	p := ParseOOC(raw)
	return build(&commv1.CommunicationContent{ActorId: a.ID, ActorDisplayName: a.Name, Text: p.Text, OocStyle: p.Style})
}

// Emit builds the CommunicationContent JSON payload for an actorless
// location-wide emit message.
func Emit(text string) string {
	return build(&commv1.CommunicationContent{Text: strings.TrimSpace(text)})
}
