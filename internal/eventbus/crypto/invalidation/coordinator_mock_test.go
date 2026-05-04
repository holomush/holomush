// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invalidation_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/holomush/holomush/internal/cluster"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/invalidation"
	"github.com/holomush/holomush/internal/eventbus/natsconn/natsmock"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestRequestInvalidationByMockedConnError demonstrates the
// natsconn.Conn interface seam (holomush-ojw1.3.23) by driving the
// Coordinator's INVALIDATION_INBOX_SUB_FAILED and
// INVALIDATION_PUBLISH_FAILED error paths with a natsmock.Conn — no
// embedded NATS server required. These error paths are infeasible to
// trigger on a real embedded server because both calls fan out to a
// healthy in-process loop.
//
// Each subtest installs the matching hook on the mock and asserts the
// outer oops code surfaces at the public RequestInvalidation
// boundary. wantInner asserts the wrapped error survives via the
// standard errors.Is chain (which works here because the inner
// errors are non-oops sentinels — the deepest-Code() traversal hazard
// from holomush-ojw1.3.22 doesn't apply to non-oops children).
func TestRequestInvalidationByMockedConnError(t *testing.T) {
	t.Parallel()

	const self = cluster.MemberID("01HSELFAAAAAAAAAAAAAAAAAA")
	const other = cluster.MemberID("01HOTHERAAAAAAAAAAAAAAAAA")
	twoMember := []cluster.Member{
		{ID: self, Status: cluster.StatusAlive},
		{ID: other, Status: cluster.StatusAlive},
	}

	// boom is the canned non-oops sentinel returned by the mock's
	// hooks. We wrap it via the inner error chain and assert via
	// errors.Is to confirm the wrap path preserves the original.
	boom := errors.New("simulated nats failure") //nolint:err113 // test-only sentinel

	cases := []struct {
		name      string
		install   func(*natsmock.Conn)
		wantCode  string
		wantInner error
	}{
		{
			// Flow: publishAndCollect → SubscribeSync → returns boom →
			// coordinator wraps with INVALIDATION_INBOX_SUB_FAILED.
			// We assert via errors.Is to prove the inner error stayed
			// in the chain (Wrap on a non-oops error is fine; deepest
			// traversal only walks oops children).
			name: "wraps SubscribeSync failure as INVALIDATION_INBOX_SUB_FAILED",
			install: func(m *natsmock.Conn) {
				m.SubscribeSyncHook = func(_ string) (*nats.Subscription, error) {
					return nil, boom
				}
			},
			wantCode:  "INVALIDATION_INBOX_SUB_FAILED",
			wantInner: boom,
		},
		{
			// Flow: publishAndCollect → SubscribeSync OK → PublishRequest
			// returns boom → coordinator wraps with INVALIDATION_PUBLISH_FAILED.
			name: "wraps PublishRequest failure as INVALIDATION_PUBLISH_FAILED",
			install: func(m *natsmock.Conn) {
				m.PublishRequestHook = func(_, _ string, _ []byte) error {
					return boom
				}
			},
			wantCode:  "INVALIDATION_PUBLISH_FAILED",
			wantInner: boom,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mock := &natsmock.Conn{}
			tc.install(mock)

			stub := &stubRegistry{
				self:        self,
				liveMembers: twoMember,
			}

			coord, err := invalidation.New(invalidation.Config{
				ClusterID:         "test-game",
				InvalidateTimeout: 100 * time.Millisecond,
			}, invalidation.Deps{
				Conn:      mock,
				Registry:  stub,
				DEKCache:  dek.NewCache(dek.CacheConfig{}),
				PartCache: dek.NewParticipantsCache(dek.CacheConfig{}),
				Logger:    slog.Default(),
			})
			if err != nil {
				t.Fatalf("invalidation.New: %v", err)
			}

			gotErr := coord.RequestInvalidation(
				context.Background(),
				dek.ContextID{Type: "scene", ID: "01HSCENE"},
				invalidation.ActionRekey, 1, 2,
			)

			errutil.AssertErrorCode(t, gotErr, tc.wantCode)
			if tc.wantInner != nil && !errors.Is(gotErr, tc.wantInner) {
				t.Errorf("expected error chain to contain %v, got %v", tc.wantInner, gotErr)
			}
		})
	}
}
