// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// cmd/internal/fsmdiagram is a build-time codegen tool that emits a Mermaid
// stateDiagram-v2 block for the CheckpointStatus FSM to stdout.
//
// Invoked via go:generate in checkpoint_fsm.go:
//
//	//go:generate go run github.com/holomush/holomush/cmd/internal/fsmdiagram
//
// The output is intended to be embedded in operator documentation. The
// diagram is derived programmatically from the validTransitions map so it
// cannot drift from the implementation.
//
// Usage:
//
//	go run ./cmd/internal/fsmdiagram
//
// Output goes to stdout; redirect to a file as needed:
//
//	go run ./cmd/internal/fsmdiagram > docs/rekey-checkpoint-fsm.md
package main

import (
	"fmt"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func main() {
	fmt.Print(dek.Diagram())
}
