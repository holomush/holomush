// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package blocklist

// This file is deliberately in the INTERNAL test package: the "the poll loop's
// goroutine has exited by the time Stop returns" assertion reads the
// subsystem's own completion channel. Asserting it from outside would require
// either exporting a lifecycle detail as production API or settling for
// "no further polls were observed", which is a weaker claim than the one the
// leak-on-shutdown risk actually needs.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/pkg/errutil"
)

const subKey = "core.character.name.blocklist"

// stubSource is a Source double: one settings row, plus the two version
// signals derived from it. Editing the value moves the hash and leaves
// updatedAt alone, which is exactly what a direct-SQL edit does in production.
type stubSource struct {
	mu        sync.Mutex
	value     string
	present   bool
	updatedAt int64
	hash      string
	readErr   error
}

func (s *stubSource) GetSystemInfo(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readErr != nil {
		return "", s.readErr
	}
	if !s.present {
		return "", oops.With("key", key).Wrap(store.ErrSystemInfoNotFound)
	}
	return s.value, nil
}

func (s *stubSource) SystemInfoVersion(_ context.Context, _ string) (int64, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.present {
		return 0, "", nil
	}
	return s.updatedAt, s.hash, nil
}

func (s *stubSource) set(value, hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value, s.hash, s.present = value, hash, true
}

func sourceWith(value string) *stubSource {
	return &stubSource{value: value, present: true, updatedAt: 1, hash: "h1"}
}

func newSub(t *testing.T, src *stubSource, interval time.Duration) *Subsystem {
	t.Helper()
	return NewSubsystem(SubsystemConfig{
		Source:   func() Source { return src },
		Key:      subKey,
		Interval: interval,
	})
}

func TestSubsystemIDIsTheCharacterNameBlockListSubsystem(t *testing.T) {
	s := newSub(t, sourceWith(`[]`), time.Hour)

	assert.Equal(t, lifecycle.SubsystemCharacterNameBlockList, s.ID())
}

func TestSubsystemDependsOnTheDatabaseAndNothingElse(t *testing.T) {
	s := newSub(t, sourceWith(`[]`), time.Hour)

	assert.Equal(t, []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}, s.DependsOn())
}

func TestSubsystemPrepareAbortsNamingTheOffendingPatternWhenTheListDoesNotCompile(t *testing.T) {
	// Paired positive control on the same shape: a valid list prepares.
	okSub := newSub(t, sourceWith(`["^admin$"]`), time.Hour)
	require.NoError(t, okSub.Prepare(t.Context()))

	s := newSub(t, sourceWith(`["^ok$", "(unclosed"]`), time.Hour)

	err := s.Prepare(t.Context())

	require.Error(t, err, "startup MUST abort rather than run with an unvalidated list")
	errutil.AssertErrorCode(t, err, "BLOCKLIST_PATTERN_INVALID")
	errutil.AssertErrorContext(t, err, "pattern", "(unclosed")
	assert.Contains(t, err.Error(), "(unclosed")
}

func TestSubsystemPrepareAbortsNamingTheKeyWhenTheSettingsValueIsMalformed(t *testing.T) {
	okSub := newSub(t, sourceWith(`["^admin$"]`), time.Hour)
	require.NoError(t, okSub.Prepare(t.Context()))

	s := newSub(t, sourceWith(`{`), time.Hour)

	err := s.Prepare(t.Context())

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "BLOCKLIST_VALUE_MALFORMED")
	errutil.AssertErrorContext(t, err, "key", subKey)
}

func TestSubsystemPrepareSucceedsWhenTheSettingsRowIsAbsent(t *testing.T) {
	s := newSub(t, &stubSource{}, time.Hour)

	require.NoError(t, s.Prepare(t.Context()), "an unconfigured list is not a startup failure")

	blocked, _ := s.Matcher().Match("admin")
	assert.False(t, blocked)
}

func TestSubsystemPrepareCompilesTheConfiguredListIntoTheLiveMatcher(t *testing.T) {
	s := newSub(t, sourceWith(`["^admin$"]`), time.Hour)

	require.NoError(t, s.Prepare(t.Context()))

	blocked, idx := s.Matcher().Match("admin")
	assert.True(t, blocked)
	assert.Equal(t, 0, idx)
}

func TestSubsystemMatcherReturnsTheSameLiveValueBeforeAndAfterPrepare(t *testing.T) {
	// The transport hands over a LIVE matcher, not a snapshot. A consumer that
	// grabs Matcher() at construction (which is when cmd/holomush wires the two
	// composition configs) must see every later reload.
	s := newSub(t, sourceWith(`["^admin$"]`), time.Hour)

	before := s.Matcher()
	require.NotNil(t, before)
	blocked, _ := before.Match("admin")
	require.False(t, blocked, "nothing is compiled yet")

	require.NoError(t, s.Prepare(t.Context()))

	assert.Same(t, before, s.Matcher(), "Matcher is stable across Prepare")
	blocked, _ = before.Match("admin")
	assert.True(t, blocked, "the value grabbed BEFORE Prepare now enforces the compiled list")
}

func TestSubsystemActivateStartsThePollLoopAndStopEndsItBeforeReturning(t *testing.T) {
	src := sourceWith(`[]`)
	s := newSub(t, src, 5*time.Millisecond)

	require.NoError(t, s.Prepare(t.Context()))
	blocked, _ := s.Matcher().Match("admin")
	require.False(t, blocked)

	require.NoError(t, s.Activate(t.Context()))

	// A direct-SQL-shaped edit: the value changes, updatedAt does not.
	src.set(`["^admin$"]`, "h2")

	require.Eventually(t, func() bool {
		b, _ := s.Matcher().Match("admin")
		return b
	}, 3*time.Second, 5*time.Millisecond, "Activate must actually run the loop")

	done := s.pollerDone
	require.NotNil(t, done)
	require.NoError(t, s.Stop(t.Context()))

	select {
	case <-done:
		// The loop's goroutine has returned.
	default:
		t.Fatal("Stop returned while the poll loop was still running — a shutdown leak")
	}
}

func TestSubsystemStopIsSafeBeforeActivateAndIsIdempotent(t *testing.T) {
	s := newSub(t, sourceWith(`[]`), time.Hour)
	require.NoError(t, s.Prepare(t.Context()))

	require.NoError(t, s.Stop(t.Context()))
	require.NoError(t, s.Stop(t.Context()))
}

func TestSubsystemActivateIsIdempotentAndDoesNotLaunchASecondLoop(t *testing.T) {
	s := newSub(t, sourceWith(`[]`), 5*time.Millisecond)
	require.NoError(t, s.Prepare(t.Context()))

	require.NoError(t, s.Activate(t.Context()))
	first := s.pollerDone
	require.NoError(t, s.Activate(t.Context()))

	assert.Equal(t, first, s.pollerDone, "a repeated Activate must not start a second goroutine")
	require.NoError(t, s.Stop(t.Context()))
}
