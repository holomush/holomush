// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

// newSnapshotForTest builds a pre-populated snapshot without needing a JS
// client. Test-only; must live in a _test.go file.
func newSnapshotForTest(firstSeq, lastSeq uint64) *StreamStateSnapshot {
	s := &StreamStateSnapshot{firstSeq: firstSeq, lastSeq: lastSeq}
	s.once.Do(func() {}) // mark as populated
	return s
}
