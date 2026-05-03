// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestTestPillRecordsTriggerOnChannelWithoutExiting(t *testing.T) {
	p, events := NewTestPill()
	p.Trigger(context.Background(), PillReasonMissedInvalidationAck, MemberID("01HEXAMPLE_SOURCE"))

	select {
	case ev := <-events:
		if ev.Reason != PillReasonMissedInvalidationAck {
			t.Errorf("Reason = %q; want %q", ev.Reason, PillReasonMissedInvalidationAck)
		}
		if ev.SourceID != MemberID("01HEXAMPLE_SOURCE") {
			t.Errorf("SourceID = %q; want %q", ev.SourceID, "01HEXAMPLE_SOURCE")
		}
		if ev.At.IsZero() {
			t.Error("At is zero; want non-zero timestamp")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("expected pill event on channel within 1s; got none")
	}
}

func TestProductionPillCallsExitFn125WithReasonInLogs(t *testing.T) {
	var exitCode int
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	// Construct production Pill but inject a test exit function so
	// we can assert the code instead of terminating.
	p := &productionPill{
		self:    MemberID("01HSELF"),
		logger:  logger,
		metrics: nil, // skip metrics for this test
		exitFn:  func(code int) { exitCode = code },
	}

	p.Trigger(context.Background(), PillReasonMissedProbeResponse, MemberID("01HCOORDINATOR"))

	if exitCode != 125 {
		t.Errorf("exit code = %d; want 125", exitCode)
	}
	logged := buf.String()
	if !strings.Contains(logged, "missed_probe_response") {
		t.Errorf("log output = %q; want to contain reason 'missed_probe_response'", logged)
	}
	if !strings.Contains(logged, "01HCOORDINATOR") {
		t.Errorf("log output = %q; want to contain source_id '01HCOORDINATOR'", logged)
	}
}

func TestNewProductionPillWiresExitFnToOSExit(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	p := NewProductionPill(MemberID("01HSELF"), logger, nil)
	pp, ok := p.(*productionPill)
	if !ok {
		t.Fatalf("NewProductionPill returned %T; want *productionPill", p)
	}
	if pp.self != MemberID("01HSELF") {
		t.Errorf("self = %q; want '01HSELF'", pp.self)
	}
	if pp.logger != logger {
		t.Errorf("logger not threaded through to productionPill")
	}
	if pp.exitFn == nil {
		t.Error("exitFn is nil; production constructor MUST wire os.Exit")
	}
	// Sanity: the test substitute pattern works (we don't actually
	// invoke exitFn here because it would terminate the test binary).
}

func TestDevPillPanicsWithReasonAndSource(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	p := NewDevPill(MemberID("01HSELF"), logger)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected DevPill to panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value type = %T; want string", r)
		}
		if !strings.Contains(msg, "missed_invalidation_ack") {
			t.Errorf("panic message = %q; want to contain reason 'missed_invalidation_ack'", msg)
		}
		if !strings.Contains(msg, "01HCOORDINATOR") {
			t.Errorf("panic message = %q; want to contain source '01HCOORDINATOR'", msg)
		}
	}()

	p.Trigger(context.Background(), PillReasonMissedInvalidationAck, MemberID("01HCOORDINATOR"))
}
