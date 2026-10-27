// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import "github.com/nats-io/nats.go/jetstream"

// newDetachedSessionStream builds a jetStreamSessionStream with no live
// ConsumeContext (cc == nil) so Close exercises only the local inbox
// teardown. bufSize 0 yields an unbuffered inbox so a single handle() call
// parks on the send until teardown.
func newDetachedSessionStream(bufSize int) *jetStreamSessionStream {
	return &jetStreamSessionStream{
		inbox: make(chan jetstream.Msg, bufSize),
		done:  make(chan struct{}),
	}
}
