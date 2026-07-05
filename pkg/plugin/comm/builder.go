// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package comm

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"

	commv1 "github.com/holomush/holomush/pkg/proto/holomush/comm/v1"
)

var marshal = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false}

// build marshals to snake_case JSON. EmitIntent.Payload (pkg/plugin/event.go:120)
// and Event.Payload (event.go:80) are string, and Lua LString is a string, so the
// success value is a string to avoid a conversion at every caller.
//
// Text and ActorDisplayName carry untrusted player input (the telnet path does no
// UTF-8 validation), and protojson.Marshal returns errInvalidUTF8 on a proto3
// string field holding invalid UTF-8, so they are sanitized to valid UTF-8 first —
// a stray bad byte yields � rather than a rejected message. ActorId and OocStyle
// are host-generated (a ULID and a fixed style vocabulary), so a marshal failure
// there signals a broken invariant: build fails closed with an error rather than
// panicking or emitting partial JSON. A player-supplied line must never be able to
// crash the host.
func build(c *commv1.CommunicationContent) (string, error) {
	c.Text = strings.ToValidUTF8(c.Text, "�")
	c.ActorDisplayName = strings.ToValidUTF8(c.ActorDisplayName, "�")
	b, err := marshal.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("marshal CommunicationContent: %w", err)
	}
	return string(b), nil
}

// Say builds the CommunicationContent JSON payload for a plain spoken line.
func Say(a Author, text string) (string, error) {
	return build(&commv1.CommunicationContent{ActorId: a.ID, ActorDisplayName: a.Name, Text: strings.TrimSpace(text)})
}

// Pose builds the CommunicationContent JSON payload for a pose or semipose,
// applying the ";"/":" grammar via ParsePose.
func Pose(a Author, invokedAs, raw string) (string, error) {
	p := ParsePose(invokedAs, raw)
	return build(&commv1.CommunicationContent{ActorId: a.ID, ActorDisplayName: a.Name, Text: p.Text, NoSpace: p.NoSpace})
}

// OOC builds the CommunicationContent JSON payload for an out-of-character
// message, classifying its surface style via ParseOOC.
func OOC(a Author, raw string) (string, error) {
	p := ParseOOC(raw)
	return build(&commv1.CommunicationContent{ActorId: a.ID, ActorDisplayName: a.Name, Text: p.Text, OocStyle: p.Style})
}

// Emit builds the CommunicationContent JSON payload for an actorless
// location-wide emit message.
func Emit(text string) (string, error) {
	return build(&commv1.CommunicationContent{Text: strings.TrimSpace(text)})
}
