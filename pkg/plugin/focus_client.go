// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
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

	// LeaveFocusByTarget removes the given focus membership from every
	// non-expired session that holds it. Used for cross-session fan-out,
	// e.g., scene-end must clear the membership on every participant's
	// session, not just the caller.
	//
	// Returns a LeaveByTargetResult describing the sweep. The error
	// return covers only enumeration failure (the host's session store
	// could not list matching sessions); in that case result is zero.
	// Per-session errors are carried on result.Failed and do NOT
	// surface as a non-nil error — this preserves the standard Go
	// `err != nil ⇒ result is zero-valued` contract so plugin authors
	// can write `if err != nil { return err }` without silently
	// dropping partial success.
	//
	// Distinguishing outcomes: inspect the returned result.
	//   - result.Succeeded > 0 && len(result.Failed) == 0 → full success
	//   - result.Succeeded > 0 && len(result.Failed) > 0  → partial
	//   - result.Succeeded == 0 && result.TotalScanned == 0 → target had no members
	//   - result.Succeeded == 0 && len(result.Failed) > 0 → total per-session failure
	LeaveFocusByTarget(ctx context.Context, target FocusKey) (LeaveByTargetResult, error)

	// PresentFocus updates the session's presenting-focus pointer. Target
	// MUST already exist in the session's FocusMemberships.
	PresentFocus(ctx context.Context, sessionID string, target FocusKey) error

	// SetConnectionFocus sets the per-connection focus pointer for the given
	// connection. focusKey nil = grid focus (D10: isSceneGrid=true skips
	// PresentingFocus write on the substrate side; INV-P5-13).
	// connectionID is the 26-char base32 ULID string from CommandRequest.ConnectionID.
	SetConnectionFocus(ctx context.Context, connectionID string, focusKey *FocusKey, isSceneGrid bool) error

	// AutoFocusOnJoin fans out to all terminal/telnet connections for the
	// character and sets their per-connection FocusKey to {scene, sceneID}.
	// Connections that are already explicitly focused on a different scene
	// are skipped (INV-P5-11). Connections whose client type is comms_hub
	// are excluded (INV-P5-4). Callers MUST have completed JoinFocus for
	// the session before invoking this RPC; the host validates membership
	// (INV-P5-1).
	//
	// characterID and sceneID are 26-char base32 ULID strings. Returns a
	// zero-value result and nil error when the character has no active
	// session (SESSION_NOT_FOUND) — callers treat this as the no-session
	// signal without surfacing an error.
	AutoFocusOnJoin(ctx context.Context, characterID, sceneID string) (AutoFocusOnJoinResult, error)

	// IsAnyConnFocused reports whether any of the character's connections has
	// FocusKey == {scene, sceneID}. Read-only: does not mutate any state.
	// characterID and sceneID are 26-char base32 ULID strings.
	//
	// Returns (false, nil) when the character has no active session (i.e.,
	// SESSION_NOT_FOUND from the host) — plugin callers use false as the
	// no-session signal without surfacing an error.
	IsAnyConnFocused(ctx context.Context, characterID, sceneID string) (bool, error)

	// QueryStreamHistory reads the tail of a stream for plugin-side display.
	// Read-only (I-13): does not mutate cursors, subscriptions, or session
	// state. The host clamps Count server-side at 500.
	//
	// The returned QueryStreamHistoryResponse.NextCursor is an opaque token
	// suitable for the next call's Cursor field to page backward through the
	// stream. Empty NextCursor means no older pages are available.
	QueryStreamHistory(ctx context.Context, req QueryStreamHistoryRequest) (QueryStreamHistoryResponse, error)
}

// FocusKey identifies a focus membership within a session.
type FocusKey struct {
	Kind     FocusKind
	TargetID string
}

// LeaveByTargetResult mirrors the host-side sweep result at the SDK
// boundary. Plugins inspect the fields directly to distinguish full,
// partial, and empty-sweep outcomes — no error-string parsing required.
// Contract holds when FocusClient.LeaveFocusByTarget returns err == nil:
// Succeeded + len(Failed) == TotalScanned.
type LeaveByTargetResult struct {
	// Succeeded counts sessions whose membership was cleared plus
	// idempotent no-ops (sessions that had already lost the membership
	// between enumeration and leave).
	Succeeded int
	// TotalScanned is the number of non-expired sessions the host's
	// sweep considered.
	TotalScanned int
	// Failed lists session IDs for which the per-session leave failed.
	// Callers that want per-session error detail can re-issue
	// LeaveFocus against these IDs; the host does not transmit
	// per-session errors on the wire.
	Failed []FailedLeave
}

// FailedLeave identifies one session that failed during a sweep.
// Err is populated only when the plugin runs in the same process as
// the coordinator (i.e., never on wire-returned values — the host
// strips error objects before serialization since errors don't cross
// process boundaries safely). Plugin authors should treat Err as
// advisory; the authoritative "which sessions failed" signal is
// SessionID membership in the Failed slice.
type FailedLeave struct {
	SessionID string
	Err       error
}

// FocusKind enumerates the kinds of focused contexts. Mirrors
// session.FocusKind on the host side; the SDK re-declares the enum so
// plugins do not take a dependency on internal/ packages.
type FocusKind string

const (
	// FocusKindScene marks a scene membership. The ScenePolicy on the
	// host derives streams "events.<gid>.scene.<target_id>.ic" and "events.<gid>.scene.<target_id>.ooc".
	FocusKindScene FocusKind = "scene"
)

// QueryStreamHistoryRequest describes a bounded tail read.
type QueryStreamHistoryRequest struct {
	Stream    string
	Count     int       // server clamps to 500
	NotBefore time.Time // zero means no time floor
	// Cursor is the opaque pagination cursor from a previous
	// QueryStreamHistoryResponse.NextCursor. Empty means "start from the
	// latest event" (no pagination state). Treat as an opaque blob — the
	// host may change its internal encoding at any time.
	Cursor []byte
}

// QueryStreamHistoryResponse is the result of a QueryStreamHistory call.
type QueryStreamHistoryResponse struct {
	// Events is the ordered slice of events on the page (oldest→newest).
	Events []Event
	// NextCursor is the opaque cursor to pass on the next call to page
	// backward. Empty when no more pages are available (the query reached
	// the beginning of the stream).
	NextCursor []byte
}

// AutoFocusOnJoinResult carries the per-connection outcome of an
// AutoFocusOnJoin call. Callers inspect the fields to choose the
// appropriate user-facing message.
//
// Distinguishing outcomes:
//   - len(FocusedConnectionIDs) > 0 && len(SkippedConnectionIDs) == 0 && len(FailedConnectionIDs) == 0 → terminal-focused
//   - len(SkippedConnectionIDs) > 0 && len(FocusedConnectionIDs) == 0 → all terminal conns explicitly focused elsewhere (INV-P5-11)
//   - TotalConnectionCount > 0 && len(FocusedConnectionIDs) == 0 && len(SkippedConnectionIDs) == 0 → comms_hub-only conns (INV-P5-4)
//   - TotalConnectionCount == 0 → no live connections (admin / scripted join)
//   - len(FailedConnectionIDs) > 0 → at least one per-conn focus failed (non-fatal; log detail only)
type AutoFocusOnJoinResult struct {
	// FocusedConnectionIDs lists connections that were successfully auto-focused.
	FocusedConnectionIDs []ulid.ULID
	// SkippedConnectionIDs lists terminal/telnet connections that were skipped
	// because they were already explicitly focused on a different scene (INV-P5-11).
	SkippedConnectionIDs []ulid.ULID
	// FailedConnectionIDs lists connections for which the focus mutation failed.
	// These are non-fatal: the join already succeeded; per-conn errors are advisory.
	FailedConnectionIDs []AutoFocusFailure
	// TotalConnectionCount is the total number of connections the host considered
	// (including comms_hub connections which are excluded from focusing per INV-P5-4).
	TotalConnectionCount uint32
}

// AutoFocusFailure identifies one connection that failed during AutoFocusOnJoin.
// Reason is the host-side failure code (e.g., "MEMBERSHIP_ABSENT").
type AutoFocusFailure struct {
	ConnectionID ulid.ULID
	Reason       string
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

func (c *pluginHostFocusClient) LeaveFocusByTarget(ctx context.Context, target FocusKey) (LeaveByTargetResult, error) {
	if c.client == nil {
		return LeaveByTargetResult{}, oops.New("plugin host focus client is not configured")
	}
	resp, err := c.client.LeaveFocusByTarget(ctx, &pluginv1.PluginHostServiceLeaveFocusByTargetRequest{
		Target: toProtoFocusKey(target),
	})
	if err != nil {
		return LeaveByTargetResult{}, wrapFanOutFocusError(err, "LeaveFocusByTarget", target)
	}
	result := LeaveByTargetResult{
		Succeeded:    int(resp.GetSucceeded()),
		TotalScanned: int(resp.GetTotalScanned()),
	}
	if ids := resp.GetFailedSessionIds(); len(ids) > 0 {
		result.Failed = make([]FailedLeave, 0, len(ids))
		for _, sid := range ids {
			// Host-side errors are not carried on the wire (see FailedLeave
			// doc); SessionID is the authoritative failure signal.
			result.Failed = append(result.Failed, FailedLeave{SessionID: sid})
		}
	}
	return result, nil
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

func (c *pluginHostFocusClient) SetConnectionFocus(ctx context.Context, connectionID string, focusKey *FocusKey, isSceneGrid bool) error {
	if c.client == nil {
		return oops.New("plugin host focus client is not configured")
	}
	connID, err := ulid.Parse(connectionID)
	if err != nil {
		return oops.Code("INVALID_ULID").With("connection_id", connectionID).Wrap(err)
	}
	req := &pluginv1.PluginHostServiceSetConnectionFocusRequest{
		ConnectionId: connID.Bytes(),
		IsSceneGrid:  isSceneGrid,
	}
	if focusKey != nil {
		req.FocusKey = toProtoFocusKey(*focusKey)
	}
	_, err = c.client.SetConnectionFocus(ctx, req)
	if err != nil {
		// Inspect the gRPC status message to recover oops codes that were lost
		// when the server's OopsError crossed the wire as status.Error(Unknown,
		// msg). The host-side FOCUS_WITHOUT_MEMBERSHIP code is the only one the
		// plugin consumer (handleSceneFocus) branches on; re-emit it faithfully
		// so the user-facing "You're not in Scene X" path is reachable.
		if st, ok := status.FromError(err); ok && strings.HasPrefix(st.Message(), "FOCUS_WITHOUT_MEMBERSHIP") {
			return oops.Code("FOCUS_WITHOUT_MEMBERSHIP").
				With("connection_id", connectionID).
				With("is_scene_grid", isSceneGrid).
				Errorf("%s", st.Message())
		}
		// Distinguish grid-pivot errors from explicit scene-focus errors for
		// telemetry accuracy (Finding #2).
		outerCode := "SCENE_FOCUS_SET_FAILED"
		if isSceneGrid {
			outerCode = "SCENE_GRID_SET_FAILED"
		}
		return oops.Code(outerCode).
			With("connection_id", connectionID).
			With("is_scene_grid", isSceneGrid).
			Wrap(err)
	}
	return nil
}

func (c *pluginHostFocusClient) IsAnyConnFocused(ctx context.Context, characterID, sceneID string) (bool, error) {
	if c.client == nil {
		return false, oops.New("plugin host focus client is not configured")
	}
	charULID, err := ulid.Parse(characterID)
	if err != nil {
		return false, oops.Code("INVALID_ULID").With("character_id", characterID).Wrap(err)
	}
	sceneULID, err := ulid.Parse(sceneID)
	if err != nil {
		return false, oops.Code("INVALID_ULID").With("scene_id", sceneID).Wrap(err)
	}
	resp, err := c.client.IsAnyConnFocused(ctx, &pluginv1.PluginHostServiceIsAnyConnFocusedRequest{
		CharacterId: charULID.Bytes(),
		SceneId:     sceneULID.Bytes(),
	})
	if err != nil {
		// Translate SESSION_NOT_FOUND → (false, nil) per interface contract.
		if st, ok := status.FromError(err); ok && (st.Code() == codes.NotFound || strings.HasPrefix(st.Message(), "SESSION_NOT_FOUND")) {
			return false, nil
		}
		return false, oops.With("character_id", characterID).With("scene_id", sceneID).Wrap(err)
	}
	return resp.GetFocused(), nil
}

func (c *pluginHostFocusClient) AutoFocusOnJoin(ctx context.Context, characterID, sceneID string) (AutoFocusOnJoinResult, error) {
	if c.client == nil {
		return AutoFocusOnJoinResult{}, oops.New("plugin host focus client is not configured")
	}
	charULID, err := ulid.Parse(characterID)
	if err != nil {
		return AutoFocusOnJoinResult{}, oops.Code("INVALID_ULID").With("character_id", characterID).Wrap(err)
	}
	sceneULID, err := ulid.Parse(sceneID)
	if err != nil {
		return AutoFocusOnJoinResult{}, oops.Code("INVALID_ULID").With("scene_id", sceneID).Wrap(err)
	}
	resp, err := c.client.AutoFocusOnJoin(ctx, &pluginv1.PluginHostServiceAutoFocusOnJoinRequest{
		CharacterId: charULID.Bytes(),
		SceneId:     sceneULID.Bytes(),
	})
	if err != nil {
		// Translate SESSION_NOT_FOUND → zero result, nil error per interface contract.
		if st, ok := status.FromError(err); ok && (st.Code() == codes.NotFound || strings.HasPrefix(st.Message(), "SESSION_NOT_FOUND")) {
			return AutoFocusOnJoinResult{}, nil
		}
		return AutoFocusOnJoinResult{}, oops.With("character_id", characterID).With("scene_id", sceneID).Wrap(err)
	}
	// Malformed connection_id bytes indicate a host/proto contract break
	// — silently dropping them would let the autofocus UI render a wrong
	// outcome (e.g., "focused 2/3 connections" when one ID was malformed)
	// while hiding the breach. Fail fast. (CodeRabbit PR #4191 round 6)
	result := AutoFocusOnJoinResult{
		TotalConnectionCount: resp.GetTotalConnectionCount(),
	}
	for _, raw := range resp.GetFocusedConnectionIds() {
		id, ok := bytesToULID(raw)
		if !ok {
			return AutoFocusOnJoinResult{}, oops.Code("INVALID_ULID").
				With("character_id", characterID).
				With("scene_id", sceneID).
				With("field", "focused_connection_ids").
				Errorf("host returned malformed connection_id bytes (len=%d, expected 16)", len(raw))
		}
		result.FocusedConnectionIDs = append(result.FocusedConnectionIDs, id)
	}
	for _, raw := range resp.GetSkippedConnectionIds() {
		id, ok := bytesToULID(raw)
		if !ok {
			return AutoFocusOnJoinResult{}, oops.Code("INVALID_ULID").
				With("character_id", characterID).
				With("scene_id", sceneID).
				With("field", "skipped_connection_ids").
				Errorf("host returned malformed connection_id bytes (len=%d, expected 16)", len(raw))
		}
		result.SkippedConnectionIDs = append(result.SkippedConnectionIDs, id)
	}
	for _, ff := range resp.GetFailedConnectionIds() {
		raw := ff.GetConnectionId()
		id, ok := bytesToULID(raw)
		if !ok {
			return AutoFocusOnJoinResult{}, oops.Code("INVALID_ULID").
				With("character_id", characterID).
				With("scene_id", sceneID).
				With("field", "failed_connection_ids").
				Errorf("host returned malformed connection_id bytes (len=%d, expected 16)", len(raw))
		}
		result.FailedConnectionIDs = append(result.FailedConnectionIDs, AutoFocusFailure{
			ConnectionID: id,
			Reason:       ff.GetReason().String(),
		})
	}
	return result, nil
}

// bytesToULID converts a raw 16-byte slice to a ulid.ULID. Returns (zero, false)
// if raw is not exactly 16 bytes.
func bytesToULID(raw []byte) (ulid.ULID, bool) {
	if len(raw) != 16 {
		return ulid.ULID{}, false
	}
	var id ulid.ULID
	copy(id[:], raw)
	return id, true
}

// queryStreamHistoryCountConversionMax is only a defensive int32 conversion
// bound — it keeps pathological int inputs (negative overflow or values
// larger than math.MaxInt32) from producing garbage on the wire. The
// host applies the semantic 500-event clamp; this client does NOT attempt
// to mirror that, intentionally, so the host remains the single source
// of truth for the cap.
const queryStreamHistoryCountConversionMax int32 = 1 << 30

func (c *pluginHostFocusClient) QueryStreamHistory(ctx context.Context, req QueryStreamHistoryRequest) (QueryStreamHistoryResponse, error) {
	if c.client == nil {
		return QueryStreamHistoryResponse{}, oops.New("plugin host focus client is not configured")
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
		Cursor:      req.Cursor,
	})
	if err != nil {
		return QueryStreamHistoryResponse{}, oops.With("stream", req.Stream).Wrap(err)
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
			Cursor:    e.GetCursor(),
		})
	}
	return QueryStreamHistoryResponse{
		Events:     events,
		NextCursor: resp.GetNextCursor(),
	}, nil
}

// wrapFanOutFocusError is the session-less variant of wrapFocusError for
// RPCs that operate on a target rather than a specific session (e.g.,
// LeaveFocusByTarget). Keeping a separate wrapper avoids log entries with
// `session_id=""`, which otherwise misleads log aggregators into treating
// fan-out failures as attribution errors.
func wrapFanOutFocusError(err error, op string, target FocusKey) error {
	if err == nil {
		return nil
	}
	base := oops.With("operation", op).
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
	"FOCUS_SWEEP_LIST_FAILED",
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
