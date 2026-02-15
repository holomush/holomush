// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package policytest provides test helpers for the ABAC policy engine.
package policytest

import (
	"context"

	"github.com/stretchr/testify/mock"

	"github.com/holomush/holomush/internal/access/policy/types"
)

// AllowAllEngine returns a mock engine that allows all requests.
func AllowAllEngine() *MockAccessPolicyEngine {
	m := &MockAccessPolicyEngine{}
	m.On("Evaluate", mock.Anything, mock.Anything).
		Maybe().
		Return(types.NewDecision(types.EffectAllow, "test-allow-all", ""), nil)
	return m
}

// DenyAllEngine returns a mock engine that denies all requests.
func DenyAllEngine() *MockAccessPolicyEngine {
	m := &MockAccessPolicyEngine{}
	m.On("Evaluate", mock.Anything, mock.Anything).
		Maybe().
		Return(types.NewDecision(types.EffectDeny, "test-deny-all", ""), nil)
	return m
}

// GrantEngine is a test AccessPolicyEngine that allows specific subject+action+resource
// combinations and denies everything else.
type GrantEngine struct {
	grants map[string]bool
}

// NewGrantEngine creates a GrantEngine with no initial grants (denies everything).
func NewGrantEngine() *GrantEngine {
	return &GrantEngine{grants: make(map[string]bool)}
}

// Grant allows a specific subject+action+resource combination.
func (g *GrantEngine) Grant(subject, action, resource string) {
	g.grants[subject+"\x00"+action+"\x00"+resource] = true
}

// Evaluate implements policy.AccessPolicyEngine.
func (g *GrantEngine) Evaluate(_ context.Context, req types.AccessRequest) (types.Decision, error) {
	key := req.Subject + "\x00" + req.Action + "\x00" + req.Resource
	if g.grants[key] {
		return types.NewDecision(types.EffectAllow, "test-grant", ""), nil
	}
	return types.NewDecision(types.EffectDeny, "test-deny", ""), nil
}

// ErrorEngine is a test AccessPolicyEngine that always returns the configured error.
// Used to test fail-closed error paths.
type ErrorEngine struct {
	err error
}

// NewErrorEngine creates an engine that always returns the given error.
func NewErrorEngine(err error) *ErrorEngine {
	return &ErrorEngine{err: err}
}

// Evaluate always returns a deny decision and the configured error.
func (e *ErrorEngine) Evaluate(_ context.Context, _ types.AccessRequest) (types.Decision, error) {
	return types.NewDecision(types.EffectDefaultDeny, "error-engine", ""), e.err
}
