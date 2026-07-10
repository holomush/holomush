// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginauthz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestAuthorizePluginStreamContributionPermitsOwnDomainRelativeRef proves the
// shared fence lets a plugin contribute a relative ref in a namespace it owns.
func TestAuthorizePluginStreamContributionPermitsOwnDomainRelativeRef(t *testing.T) {
	err := pluginauthz.AuthorizePluginStreamContribution("core-channels", []string{"channel"}, "channel.01CHAN0000000000000000000")
	require.NoError(t, err)
}

// TestAuthorizePluginStreamContributionRejects covers every fence rejection with
// its specific oops code. Asserts against the SAME function both the
// establishment merge and the mid-session guard use (review R3-A).
func TestAuthorizePluginStreamContributionRejects(t *testing.T) {
	tests := []struct {
		name     string
		owned    []string
		ref      string
		wantCode string
	}{
		{"pre-qualified events subject", []string{"channel"}, "events.main.channel.c1", "STREAM_NOT_RELATIVE"},
		{"colon-style ref", []string{"channel"}, "channel:c1", "STREAM_NOT_RELATIVE"},
		{"empty leading domain", []string{"channel"}, ".c1", "STREAM_NOT_RELATIVE"},
		{"wildcard trailing", []string{"channel"}, "channel.>", "STREAM_WILDCARD_FORBIDDEN"},
		{"wildcard star", []string{"channel"}, "channel.*", "STREAM_WILDCARD_FORBIDDEN"},
		{"forbidden system", []string{"channel"}, "system.rekey.ct.cid", "STREAM_FORBIDDEN_NAMESPACE"},
		{"forbidden audit", []string{"channel"}, "audit.log.x", "STREAM_FORBIDDEN_NAMESPACE"},
		{"forbidden crypto", []string{"channel"}, "crypto.policy.x", "STREAM_FORBIDDEN_NAMESPACE"},
		{"foreign domain not owned", []string{"channel"}, "scene.01SCENE00000000000000000000", "STREAM_NAMESPACE_NOT_OWNED"},
		{"no owned domains", nil, "channel.c1", "STREAM_NAMESPACE_NOT_OWNED"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := pluginauthz.AuthorizePluginStreamContribution("core-channels", tc.owned, tc.ref)
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, tc.wantCode)
		})
	}
}

// TestAuthorizeStreamSubscribePermitsOwnRelativeRef proves the mid-session guard
// accepts the calling plugin's own relative ref and QUALIFIES it before the ABAC
// decision (review R2-A).
func TestAuthorizeStreamSubscribePermitsOwnRelativeRef(t *testing.T) {
	eng := &recordingEngine{allow: true}
	dec, err := pluginauthz.AuthorizeStreamSubscribe(context.Background(), pluginauthz.StreamSubscribeInput{
		Engine:           eng,
		PluginName:       "core-channels",
		Subject:          access.PluginSubject("core-channels"),
		GameID:           "main",
		Stream:           "channel.01CHAN0000000000000000000",
		OwnedEmitDomains: []string{"channel"},
	})
	require.NoError(t, err)
	assert.True(t, dec.Allowed)
	assert.Equal(t, "stream:events.main.channel.01CHAN0000000000000000000", eng.gotResource,
		"the ABAC resource must be the QUALIFIED stream (R2-A) and the action write")
}

// TestAuthorizeStreamSubscribeRejectsPreQualifiedSubject proves the guard rejects
// a full events. subject with STREAM_NOT_RELATIVE BEFORE the engine (review R2-A).
func TestAuthorizeStreamSubscribeRejectsPreQualifiedSubject(t *testing.T) {
	eng := &recordingEngine{allow: true}
	_, err := pluginauthz.AuthorizeStreamSubscribe(context.Background(), pluginauthz.StreamSubscribeInput{
		Engine:           eng,
		PluginName:       "core-channels",
		Subject:          access.PluginSubject("core-channels"),
		GameID:           "main",
		Stream:           "events.main.channel.c1",
		OwnedEmitDomains: []string{"channel"},
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "STREAM_NOT_RELATIVE")
	assert.Empty(t, eng.gotResource, "must be rejected before the engine is consulted")
}

// TestAuthorizeStreamSubscribeRejectsForbiddenAndForeignInHandler proves that the
// forbidden-namespace and foreign-domain rejections happen IN-HANDLER even when
// the ABAC engine would ALLOW (the broad seed:plugin-stream-subscribe write
// permit) — the in-handler fence, not the read-only forbids, is the control
// (review R2-B / R3-A).
func TestAuthorizeStreamSubscribeRejectsForbiddenAndForeignInHandler(t *testing.T) {
	tests := []struct {
		name, stream, wantCode string
	}{
		{"system namespace", "system.rekey.ct.cid", "STREAM_FORBIDDEN_NAMESPACE"},
		{"audit namespace", "audit.log.x", "STREAM_FORBIDDEN_NAMESPACE"},
		{"crypto namespace", "crypto.policy.x", "STREAM_FORBIDDEN_NAMESPACE"},
		{"another plugin's domain", "scene.01SCENE00000000000000000000", "STREAM_NAMESPACE_NOT_OWNED"},
		{"wildcard", "channel.>", "STREAM_WILDCARD_FORBIDDEN"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eng := &recordingEngine{allow: true} // engine would permit — proves in-handler fence denies
			_, err := pluginauthz.AuthorizeStreamSubscribe(context.Background(), pluginauthz.StreamSubscribeInput{
				Engine:           eng,
				PluginName:       "core-channels",
				Subject:          access.PluginSubject("core-channels"),
				GameID:           "main",
				Stream:           tc.stream,
				OwnedEmitDomains: []string{"channel"},
			})
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, tc.wantCode)
			assert.Empty(t, eng.gotResource, "in-handler fence must reject before the engine is consulted")
		})
	}
}
