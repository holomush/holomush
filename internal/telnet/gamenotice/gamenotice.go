// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package gamenotice builds the shared `[>GAME: <msg>]` leader that telnet
// surfaces render for game-originated notices (scene activity, idle, invite).
// The `>` echoes the `>holomush_` software wordmark (branding.md, D-03).
//
// Every builder is a pure string transform: it takes the bare scene id and
// returns the fully-formed line. No builder performs I/O, DB access, logging,
// or any scene-title/content lookup — the leader carries only the scene id, so
// it cannot leak scene content (INV-SCENE-70 privacy parity with INV-SCENE-62).
// This is the reusable primitive mandated by D-03; it is deliberately NOT a
// core-scenes-local string.
package gamenotice

import "fmt"

// Activity returns the leader for a scene that has new activity, rendered to a
// non-focused member on the telnet surface.
func Activity(sceneID string) string {
	return fmt.Sprintf("[>GAME: Scene #%s has new activity]", sceneID)
}

// Idle returns the leader announcing a scene has gone idle. Reusable primitive;
// wired only in the Plan 06 emit path.
func Idle(sceneID string) string {
	return fmt.Sprintf("[>GAME: Scene #%s is now idle]", sceneID)
}

// Invite returns the leader announcing the recipient was invited to a scene.
// Reusable primitive; not wired at the non-focused seam this phase.
func Invite(sceneID string) string {
	return fmt.Sprintf("[>GAME: You were invited to Scene #%s]", sceneID)
}
