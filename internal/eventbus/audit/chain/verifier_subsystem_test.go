// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package chain_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/pkg/errutil"
)

// ---------------------------------------------------------------------------
// makeTestHandlerFor builds a Handler bundle for a named chain prefix.
// Uses the same testPayload shape as the verifier tests (scope / prev_hash /
// self_hash fields).
// ---------------------------------------------------------------------------

func makeTestHandlerFor(t *testing.T, subjectPrefix string) chain.Handler {
	t.Helper()
	c := chain.Chain{
		SubjectPrefix:     subjectPrefix,
		SelfHashField:     "self_hash",
		PrevHashField:     "prev_hash",
		ScopePayloadField: "scope",
	}
	return chain.Handler{
		Chain: c,
		SubjectFor: func(scope string) string {
			return subjectPrefix + "." + scope
		},
		ScopeFromSubject: func(subject string) (string, error) {
			idx := len(subjectPrefix) + 1 // skip the trailing "."
			if idx >= len(subject) {
				return "", nil
			}
			return subject[idx:], nil
		},
		ScopeFromPayload: func(payload []byte) (string, error) {
			var p testPayload
			if err := json.Unmarshal(payload, &p); err != nil {
				return "", err
			}
			return p.Scope, nil
		},
		Canonicalize: func(payload []byte) ([]byte, error) {
			var m map[string]any
			if err := json.Unmarshal(payload, &m); err != nil {
				return nil, err
			}
			return json.Marshal(m)
		},
		PrevHashOf: func(payload []byte) ([]byte, error) {
			var m map[string]any
			if err := json.Unmarshal(payload, &m); err != nil {
				return nil, err
			}
			v, ok := m["prev_hash"]
			if !ok || v == nil {
				return nil, nil
			}
			raw, err := json.Marshal(map[string]any{"v": v})
			if err != nil {
				return nil, err
			}
			var typed struct {
				V []byte `json:"v"`
			}
			if err := json.Unmarshal(raw, &typed); err != nil {
				return nil, err
			}
			return typed.V, nil
		},
		SelfHashOf: func(payload []byte) ([]byte, error) {
			var m map[string]any
			if err := json.Unmarshal(payload, &m); err != nil {
				return nil, err
			}
			v, ok := m["self_hash"]
			if !ok || v == nil {
				return nil, nil
			}
			raw, err := json.Marshal(map[string]any{"v": v})
			if err != nil {
				return nil, err
			}
			var typed struct {
				V []byte `json:"v"`
			}
			if err := json.Unmarshal(raw, &typed); err != nil {
				return nil, err
			}
			return typed.V, nil
		},
	}
}

// ---------------------------------------------------------------------------
// TestVerifierSubsystem_WalksAllRegisteredChains: two handlers, both with
// empty repos → first-boot OK. Both chains must be visited (Start returns nil).
// ---------------------------------------------------------------------------

func TestVerifierSubsystem_WalksAllRegisteredChains(t *testing.T) {
	h1 := makeTestHandlerFor(t, "events.g1.system.one")
	h2 := makeTestHandlerFor(t, "events.g1.system.two")

	repo := &fakeRepo{} // both chains empty → first-boot OK
	sub := chain.NewVerifierSubsystem(chain.VerifierSubsystemConfig{
		Repo:             repo,
		HandlersProvider: func() []chain.Handler { return []chain.Handler{h1, h2} },
		Logger:           slog.Default(),
	})

	require.NoError(t, sub.Prepare(context.Background()))
	require.NoError(t, sub.Activate(context.Background()))
	require.NoError(t, sub.Stop(context.Background()))
}

// ---------------------------------------------------------------------------
// TestVerifierSubsystem_RefusesBootOnBreak (INV-CRYPTO-102): a tampered self_hash
// in the repo causes Start to return AUDIT_CHAIN_HASH_MISMATCH.
// ---------------------------------------------------------------------------

func TestVerifierSubsystem_RefusesBootOnBreak(t *testing.T) {
	h := makeTestHandlerFor(t, "events.g1.system.example")

	// Build a payload with a deliberately wrong self_hash.
	p1 := buildPayload(t, "scopeA", nil)
	p1 = setPayloadSelfHash(t, p1, []byte{0xde, 0xad}) // tampered

	repo := &fakeRepo{
		entries: map[string][]chain.Entry{
			"scopeA": {{JSSeq: 1, Subject: "events.g1.system.example.scopeA", Payload: p1}},
		},
		scopes: []string{"scopeA"},
	}
	sub := chain.NewVerifierSubsystem(chain.VerifierSubsystemConfig{
		Repo:             repo,
		HandlersProvider: func() []chain.Handler { return []chain.Handler{h} },
		Logger:           slog.Default(),
	})

	err := sub.Prepare(context.Background()) // Prepare-only: the chain walk fails before Activate would ever run
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_CHAIN_HASH_MISMATCH")
}

// ---------------------------------------------------------------------------
// TestVerifierSubsystem_RejectsInvalidChainRegistration: a Handler whose Chain
// metadata fails ValidateRegistration (missing SubjectPrefix starting with
// "events.") causes Start to return AUDIT_CHAIN_INVALID_REGISTRATION.
// ---------------------------------------------------------------------------

func TestVerifierSubsystem_RejectsInvalidChainRegistration(t *testing.T) {
	bad := chain.Handler{
		Chain: chain.Chain{
			SubjectPrefix:     "audit.bad.chain", // violates INV-CRYPTO-113
			SelfHashField:     "self_hash",
			PrevHashField:     "prev_hash",
			ScopePayloadField: "scope",
		},
	}
	sub := chain.NewVerifierSubsystem(chain.VerifierSubsystemConfig{
		Repo:             &fakeRepo{},
		HandlersProvider: func() []chain.Handler { return []chain.Handler{bad} },
		Logger:           slog.Default(),
	})

	err := sub.Prepare(context.Background()) // Prepare-only: the chain walk fails before Activate would ever run
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_CHAIN_INVALID_REGISTRATION")
}
