// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import "errors"

// errStreamTerminated signals graceful Subscribe termination after a
// matching session_ended event. Propagates from sendAndCommitEvent →
// replayAndSend → the Subscribe live loop, which translates it to
// `return nil` (clean gRPC stream close).
//
// Unexported — not a public contract. The only callers that check for
// it are internal to this package.
var errStreamTerminated = errors.New("stream terminated by session_ended")
