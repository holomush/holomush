// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"strings"
	"time"

	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// FocusClient is the SDK-facing facade binary-plugin code uses to drive the
// server-owned focus substrate on behalf of a session. All calls cross the
// plugin broker (mTLS) to the host's PluginHostService.
//
// See docs/superpowers/specs/2026-04-11-focus-substrate-design.md §3.4 for
// the host-side interface this wraps, and §4.3/4.4/4.5 for transition
// semantics. Plugins MUST NOT mutate session.FocusMemberships directly
// (invariant I-6); all mutations flow through this interface.
type FocusClient interface {
	// JoinFocus adds a focus membership and applies the kind-specific
	// server-owned replay policy. Callers provide the target (kind + id);
	// the server determines streams, replay mode, cursor baselines, and
	// subscription updates. Callers MUST NOT declare replay mode or stream
	// names (invariant I-7).
	JoinFocus(ctx context.Context, sessionID string, target FocusKey) error

	// LeaveFocus removes a focus membership. Idempotent on non-member.
	LeaveFocus(ctx context.Context, sessionID string, target FocusKey) error

	// PresentFocus updates the session's presenting-focus pointer. Target
	// MUST already exist in the session's FocusMemberships.
	PresentFocus(ctx context.Context, sessionID string, target FocusKey) error

	// QueryStreamHistory reads the tail of a stream for plugin-side display.
	// Read-only (I-13): does not mutate cursors, subscriptions, or session
	// state. The host clamps Count server-side at 500.
	QueryStreamHistory(ctx context.Context, req QueryStreamHistoryRequest) ([]Event, error)
}

// FocusKey identifies a focus membership within a session.
type FocusKey struct {
	Kind     FocusKind
	TargetID string
}

// FocusKind enumerates the kinds of focused contexts. Mirrors
// session.FocusKind on the host side; the SDK re-declares the enum so
// plugins do not take a dependency on internal/ packages.
type FocusKind string

const (
	// FocusKindScene marks a scene membership. The ScenePolicy on the
	// host derives streams "scene:<target_id>:ic" and "scene:<target_id>:ooc".
	FocusKindScene FocusKind = "scene"
)

// QueryStreamHistoryRequest describes a bounded tail read.
type QueryStreamHistoryRequest struct {
	Stream    string
	Count     int       // server clamps to 500
	NotBefore time.Time // zero means no time floor
}

// FocusClientAware is the optional interface service providers implement to
// receive a FocusClient during Init, parallel to EventSinkAware.
type FocusClientAware interface {
	SetFocusClient(FocusClient)
}

// pluginHostFocusClient is the broker-backed FocusClient implementation.
type pluginHostFocusClient struct {
	client pluginv1.PluginHostServiceClient
}

// newPluginHostFocusClient constructs a FocusClient wrapping the given
// PluginHostServiceClient. Exposed to the adapter for wiring; test code
// constructs a pluginHostFocusClient directly.
func newPluginHostFocusClient(client pluginv1.PluginHostServiceClient) FocusClient {
	return &pluginHostFocusClient{client: client}
}

func toProtoFocusKind(k FocusKind) pluginv1.FocusKind {
	switch k {
	case FocusKindScene:
		return pluginv1.FocusKind_FOCUS_KIND_SCENE
	default:
		return pluginv1.FocusKind_FOCUS_KIND_UNSPECIFIED
	}
}

func toProtoFocusKey(key FocusKey) *pluginv1.FocusKey {
	return &pluginv1.FocusKey{
		Kind:     toProtoFocusKind(key.Kind),
		TargetId: key.TargetID,
	}
}

func (c *pluginHostFocusClient) JoinFocus(ctx context.Context, sessionID string, target FocusKey) error {
	if c.client == nil {
		return oops.New("plugin host focus client is not configured")
	}
	_, err := c.client.JoinFocus(ctx, &pluginv1.PluginHostServiceJoinFocusRequest{
		SessionId: sessionID,
		Target:    toProtoFocusKey(target),
	})
	return wrapFocusError(err, "JoinFocus", sessionID, target)
}

func (c *pluginHostFocusClient) LeaveFocus(ctx context.Context, sessionID string, target FocusKey) error {
	if c.client == nil {
		return oops.New("plugin host focus client is not configured")
	}
	_, err := c.client.LeaveFocus(ctx, &pluginv1.PluginHostServiceLeaveFocusRequest{
		SessionId: sessionID,
		Target:    toProtoFocusKey(target),
	})
	return wrapFocusError(err, "LeaveFocus", sessionID, target)
}

func (c *pluginHostFocusClient) PresentFocus(ctx context.Context, sessionID string, target FocusKey) error {
	if c.client == nil {
		return oops.New("plugin host focus client is not configured")
	}
	_, err := c.client.PresentFocus(ctx, &pluginv1.PluginHostServicePresentFocusRequest{
		SessionId: sessionID,
		Target:    toProtoFocusKey(target),
	})
	return wrapFocusError(err, "PresentFocus", sessionID, target)
}

// queryStreamHistoryCountConversionMax is only a defensive int32 conversion
// bound — it keeps pathological int inputs (negative overflow or values
// larger than math.MaxInt32) from producing garbage on the wire. The
// host applies the semantic 500-event clamp; this client does NOT attempt
// to mirror that, intentionally, so the host remains the single source
// of truth for the cap.
const queryStreamHistoryCountConversionMax int32 = 1 << 30

func (c *pluginHostFocusClient) QueryStreamHistory(ctx context.Context, req QueryStreamHistoryRequest) ([]Event, error) {
	if c.client == nil {
		return nil, oops.New("plugin host focus client is not configured")
	}
	var notBeforeMs int64
	if !req.NotBefore.IsZero() {
		notBeforeMs = req.NotBefore.UnixMilli()
	}
	var count int32
	switch {
	case req.Count < 0:
		count = 0
	case int64(req.Count) > int64(queryStreamHistoryCountConversionMax):
		count = queryStreamHistoryCountConversionMax
	default:
		count = int32(req.Count) // bounds-checked above
	}
	resp, err := c.client.QueryStreamHistory(ctx, &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream:      req.Stream,
		Count:       count,
		NotBeforeMs: notBeforeMs,
	})
	if err != nil {
		return nil, oops.With("stream", req.Stream).Wrap(err)
	}
	events := make([]Event, 0, len(resp.GetEvents()))
	for _, e := range resp.GetEvents() {
		events = append(events, Event{
			ID:        e.GetId(),
			Stream:    e.GetStream(),
			Type:      EventType(e.GetType()),
			Timestamp: e.GetTimestamp(),
			ActorKind: protoActorKindToActorKind(e.GetActorKind()),
			ActorID:   e.GetActorId(),
			Payload:   e.GetPayload(),
		})
	}
	return events, nil
}

// wrapFocusError maps gRPC codes returned by the host's focus RPCs into
// oops-coded errors that callers can switch on via errors.As + oe.Code().
// Unknown codes pass through with a generic wrap.
func wrapFocusError(err error, op, sessionID string, target FocusKey) error {
	if err == nil {
		return nil
	}
	base := oops.With("operation", op).
		With("session_id", sessionID).
		With("focus_kind", string(target.Kind)).
		With("target_id", target.TargetID)
	st, ok := status.FromError(err)
	if !ok {
		return base.Wrap(err)
	}
	code := codeFromStatus(st)
	if code == "" {
		return base.Wrap(err)
	}
	return base.Code(code).Wrap(err)
}

// focusErrorCodePrefixes enumerates the recognized oops codes the host
// prepends to its gRPC status messages for focus RPCs.
var focusErrorCodePrefixes = []string{
	"SESSION_NOT_FOUND",
	"SESSION_EXPIRED",
	"FOCUS_ALREADY_MEMBER",
	"FOCUS_KIND_UNREGISTERED",
	"FOCUS_POLICY_FAILED",
	"FOCUS_NOT_MEMBER",
}

// codeFromStatus returns the expected oops code for a gRPC status, or ""
// if the code-message pair does not match a known focus-RPC error. The
// host stamps the oops code as a "CODE: ..." prefix on the status message;
// this function detects it and falls back to gRPC code mapping only when
// the prefix is absent.
func codeFromStatus(st *status.Status) string {
	msg := st.Message()
	for _, c := range focusErrorCodePrefixes {
		// Require the ':' delimiter the host stamps, or an exact match.
		// A plain prefix match would bind a future code like
		// FOCUS_NOT_MEMBER_EXTRA to FOCUS_NOT_MEMBER silently.
		if msg == c || strings.HasPrefix(msg, c+":") {
			return c
		}
	}
	switch st.Code() {
	case codes.NotFound:
		return "SESSION_NOT_FOUND"
	case codes.AlreadyExists:
		return "FOCUS_ALREADY_MEMBER"
	case codes.FailedPrecondition:
		return "SESSION_EXPIRED"
	case codes.InvalidArgument:
		return "FOCUS_KIND_UNREGISTERED"
	case codes.Internal:
		return "FOCUS_POLICY_FAILED"
	}
	return ""
}

// newFocusClientFromBroker dials the plugin host service via the broker
// and returns a FocusClient. See broker.go for the shared dial helper.
func newFocusClientFromBroker(broker brokerDialer, services map[string]string) (FocusClient, error) {
	if broker == nil {
		return nil, oops.New("plugin host broker is not configured")
	}
	conn, err := dialPluginHost(broker, services)
	if err != nil {
		return nil, err
	}
	return newPluginHostFocusClient(pluginv1.NewPluginHostServiceClient(conn)), nil
}
