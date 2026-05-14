// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// --- parseContextFlag ---

func TestParseContextFlag_SingleID(t *testing.T) {
	ref, err := parseContextFlag("scene:01HZAVGE83MGFEXQQH5SP9NXKF")
	require.NoError(t, err)
	assert.Equal(t, "scene", ref.GetType())
	assert.Equal(t, []string{"01HZAVGE83MGFEXQQH5SP9NXKF"}, ref.GetIds())
}

func TestParseContextFlag_DualID(t *testing.T) {
	ref, err := parseContextFlag("dm:01A:01B")
	require.NoError(t, err)
	assert.Equal(t, "dm", ref.GetType())
	assert.Equal(t, []string{"01A", "01B"}, ref.GetIds())
}

func TestParseContextFlag_NIDsAllowed(t *testing.T) {
	ref, err := parseContextFlag("trade:01A:01B:01C")
	require.NoError(t, err)
	assert.Equal(t, "trade", ref.GetType())
	assert.Equal(t, []string{"01A", "01B", "01C"}, ref.GetIds())
}

func TestParseContextFlag_NoColon(t *testing.T) {
	_, err := parseContextFlag("barestring")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "type>:<id")
}

func TestParseContextFlag_Empty(t *testing.T) {
	_, err := parseContextFlag("")
	require.Error(t, err)
}

// --- exitCodeForError ---

func TestExitCodeForError_AuditEmitFailure(t *testing.T) {
	err := oops.Code("DENY_AUDIT_PRE_DATA_PUBLISH").Errorf("audit emit failed")
	assert.Equal(t, 70, exitCodeForError(err))
}

func TestExitCodeForError_OperatorCapability(t *testing.T) {
	err := oops.Code("DENY_OPERATOR_CAPABILITY").Errorf("not permitted")
	assert.Equal(t, 77, exitCodeForError(err))
}

func TestExitCodeForError_WindowTooLarge(t *testing.T) {
	err := oops.Code("DENY_OPERATOR_READ_WINDOW_TOO_LARGE").Errorf("window too large")
	assert.Equal(t, 65, exitCodeForError(err))
}

func TestExitCodeForError_TimeInverted(t *testing.T) {
	err := oops.Code("DENY_OPERATOR_READ_TIME_INVERTED").Errorf("time inverted")
	assert.Equal(t, 65, exitCodeForError(err))
}

func TestExitCodeForError_JustificationEmpty(t *testing.T) {
	err := oops.Code("DENY_OPERATOR_READ_JUSTIFICATION_EMPTY").Errorf("empty")
	assert.Equal(t, 65, exitCodeForError(err))
}

func TestExitCodeForError_JustificationTooLong(t *testing.T) {
	err := oops.Code("DENY_OPERATOR_READ_JUSTIFICATION_TOO_LONG").Errorf("too long")
	assert.Equal(t, 65, exitCodeForError(err))
}

func TestExitCodeForError_ContextPrefix(t *testing.T) {
	err := oops.Code("DENY_OPERATOR_READ_CONTEXT_INVALID").Errorf("bad context")
	assert.Equal(t, 65, exitCodeForError(err))
}

func TestExitCodeForError_DeadlineExceeded(t *testing.T) {
	err := oops.Code("READSTREAM_DEADLINE_EXCEEDED").Errorf("deadline")
	assert.Equal(t, 75, exitCodeForError(err))
}

func TestExitCodeForError_DualControlTimeout(t *testing.T) {
	err := oops.Code("READSTREAM_DUAL_CONTROL_TIMEOUT").Errorf("timeout")
	assert.Equal(t, 75, exitCodeForError(err))
}

func TestExitCodeForError_SessionInvalid(t *testing.T) {
	err := oops.Code("DENY_SESSION_INVALID").Errorf("session invalid")
	assert.Equal(t, 77, exitCodeForError(err))
}

func TestExitCodeForError_Unknown(t *testing.T) {
	err := oops.Code("SOMETHING_UNKNOWN").Errorf("unknown")
	assert.Equal(t, 70, exitCodeForError(err))
}

func TestExitCodeForError_Nil(t *testing.T) {
	assert.Equal(t, 0, exitCodeForError(nil))
}

// --- exitCodeForTerminatedBy ---

func TestExitCodeForTerminatedBy_CleanExit(t *testing.T) {
	assert.Equal(t, 0, exitCodeForTerminatedBy(adminv1.ReadFinished_TERMINATED_BY_CLIENT_EOF))
}

func TestExitCodeForTerminatedBy_ClientDisconnect(t *testing.T) {
	assert.Equal(t, 0, exitCodeForTerminatedBy(adminv1.ReadFinished_TERMINATED_BY_CLIENT_DISCONNECT))
}

func TestExitCodeForTerminatedBy_DeadlineExceeded(t *testing.T) {
	assert.Equal(t, 75, exitCodeForTerminatedBy(adminv1.ReadFinished_TERMINATED_BY_DEADLINE_EXCEEDED))
}

func TestExitCodeForTerminatedBy_ServerError(t *testing.T) {
	assert.Equal(t, 70, exitCodeForTerminatedBy(adminv1.ReadFinished_TERMINATED_BY_SERVER_ERROR))
}

func TestExitCodeForTerminatedBy_DualControlTimeout(t *testing.T) {
	assert.Equal(t, 75, exitCodeForTerminatedBy(adminv1.ReadFinished_TERMINATED_BY_DUAL_CONTROL_TIMEOUT))
}

func TestExitCodeForTerminatedBy_AuditEmitFailure(t *testing.T) {
	assert.Equal(t, 70, exitCodeForTerminatedBy(adminv1.ReadFinished_TERMINATED_BY_AUDIT_EMIT_FAILURE))
}

func TestExitCodeForTerminatedBy_Unspecified(t *testing.T) {
	assert.Equal(t, 70, exitCodeForTerminatedBy(adminv1.ReadFinished_TERMINATED_BY_UNSPECIFIED))
}

// --- renderFrame ---

func TestRenderFrame_TypedRedactionRendersReason(t *testing.T) {
	// Feed an EventFrame with metadata_only=true and no_plaintext_reason=DEK_MISSING.
	// Must render the typed reason string — NO heuristic on Payload length.
	frame := &adminv1.AdminReadStreamResponse{
		Payload: &adminv1.AdminReadStreamResponse_Event{
			Event: &corev1.EventFrame{
				Stream:            "scene.01HZAVGE83MGFEXQQH5SP9NXKF",
				Type:              "scene.emote",
				MetadataOnly:      true,
				NoPlaintextReason: corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_DEK_MISSING,
				Timestamp:         timestamppb.Now(),
			},
		},
	}

	var stdout, stderr bytes.Buffer
	renderFrame(frame, "text", &stdout, &stderr)

	assert.Contains(t, stdout.String(), "[redacted:")
	assert.Contains(t, stdout.String(), "DEK_MISSING")
	assert.Contains(t, stdout.String(), "scene.01HZAVGE83MGFEXQQH5SP9NXKF")
	assert.Contains(t, stdout.String(), "scene.emote")
	// Nothing should go to stderr for an event frame.
	assert.Empty(t, stderr.String())
}

func TestRenderFrame_PlaintextEvent(t *testing.T) {
	payload := []byte("hello world event payload")
	frame := &adminv1.AdminReadStreamResponse{
		Payload: &adminv1.AdminReadStreamResponse_Event{
			Event: &corev1.EventFrame{
				Id:           "01HZAVGE83MGFEXQQH5SP9NXKF",
				Stream:       "scene.01HZAVGE83MGFEXQQH5SP9NXKF",
				Type:         "scene.say",
				Payload:      payload,
				MetadataOnly: false,
				Timestamp:    timestamppb.Now(),
			},
		},
	}

	var stdout, stderr bytes.Buffer
	renderFrame(frame, "text", &stdout, &stderr)

	assert.Contains(t, stdout.String(), "hello world event payload")
	assert.Empty(t, stderr.String())
}

func TestRenderFrame_PendingApproval(t *testing.T) {
	frame := &adminv1.AdminReadStreamResponse{
		Payload: &adminv1.AdminReadStreamResponse_PendingApproval{
			PendingApproval: &adminv1.PendingApproval{
				RequestId: []byte{0x01, 0x02, 0x03},
				ExpiresAt: timestamppb.Now(),
			},
		},
	}

	var stdout, stderr bytes.Buffer
	renderFrame(frame, "text", &stdout, &stderr)

	assert.Contains(t, stderr.String(), "pending:")
	assert.Contains(t, stderr.String(), "request_id=")
	assert.Empty(t, stdout.String())
}

func TestRenderFrame_ReadStarted(t *testing.T) {
	frame := &adminv1.AdminReadStreamResponse{
		Payload: &adminv1.AdminReadStreamResponse_Started{
			Started: &adminv1.ReadStarted{
				RequestId:  "01HZAVGE83MGFEXQQH5SP9NXKF",
				PolicyHash: []byte{0xde, 0xad, 0xbe, 0xef},
			},
		},
	}

	var stdout, stderr bytes.Buffer
	renderFrame(frame, "text", &stdout, &stderr)

	assert.Contains(t, stderr.String(), "started:")
	assert.Contains(t, stderr.String(), "01HZAVGE83MGFEXQQH5SP9NXKF")
	assert.Empty(t, stdout.String())
}

func TestRenderFrame_ReadFinished(t *testing.T) {
	frame := &adminv1.AdminReadStreamResponse{
		Payload: &adminv1.AdminReadStreamResponse_Finished{
			Finished: &adminv1.ReadFinished{
				TerminatedBy:     adminv1.ReadFinished_TERMINATED_BY_CLIENT_EOF,
				EventsScanned:    42,
				DecryptFailCount: 1,
			},
		},
	}

	var stdout, stderr bytes.Buffer
	renderFrame(frame, "text", &stdout, &stderr)

	assert.Contains(t, stderr.String(), "finished:")
	assert.Contains(t, stderr.String(), "events_scanned=42")
	assert.Contains(t, stderr.String(), "decrypt_fail_count=1")
	assert.Empty(t, stdout.String())
}

// --- renderFrame JSON output ---

// TestRenderFrame_JSONOutputEventFrame verifies that output=="json" renders an
// event frame as a single-line JSON object to stdout (not stderr), carrying
// frame_type, subject, metadata_only, and no_plaintext_reason.
func TestRenderFrame_JSONOutputEventFrame(t *testing.T) {
	frame := &adminv1.AdminReadStreamResponse{
		Payload: &adminv1.AdminReadStreamResponse_Event{
			Event: &corev1.EventFrame{
				Stream:            "scene.01HZAVGE83MGFEXQQH5SP9NXKF",
				Type:              "scene.emote",
				MetadataOnly:      true,
				NoPlaintextReason: corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_DEK_MISSING,
				Timestamp:         timestamppb.Now(),
			},
		},
	}

	var stdout, stderr bytes.Buffer
	renderFrame(frame, "json", &stdout, &stderr)

	line := stdout.String()
	assert.NotEmpty(t, line, "JSON output must be non-empty on stdout")
	assert.Empty(t, stderr.String(), "JSON output must not write to stderr")

	// Must be valid JSON.
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(line)), &decoded),
		"JSON output must be valid JSON")

	assert.Equal(t, "event", decoded["frame_type"])
	assert.Equal(t, "scene.01HZAVGE83MGFEXQQH5SP9NXKF", decoded["stream"])
	assert.Equal(t, true, decoded["metadata_only"])
	assert.Contains(t, decoded["no_plaintext_reason"], "DEK_MISSING")
}

// TestRenderFrame_JSONOutputFinished verifies that output=="json" renders a
// ReadFinished frame with terminated_by and counters.
func TestRenderFrame_JSONOutputFinished(t *testing.T) {
	frame := &adminv1.AdminReadStreamResponse{
		Payload: &adminv1.AdminReadStreamResponse_Finished{
			Finished: &adminv1.ReadFinished{
				TerminatedBy:     adminv1.ReadFinished_TERMINATED_BY_CLIENT_EOF,
				EventsScanned:    7,
				DecryptFailCount: 2,
			},
		},
	}

	var stdout, stderr bytes.Buffer
	renderFrame(frame, "json", &stdout, &stderr)

	line := stdout.String()
	assert.NotEmpty(t, line)
	assert.Empty(t, stderr.String())

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(line)), &decoded))
	assert.Equal(t, "finished", decoded["frame_type"])
	assert.Contains(t, decoded["terminated_by"], "CLIENT_EOF")
	// JSON numbers decode as float64 when unmarshaling into map[string]any.
	assert.EqualValues(t, 7, decoded["events_scanned"])
	assert.EqualValues(t, 2, decoded["decrypt_fail_count"])
}
