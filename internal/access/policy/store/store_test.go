// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestValidateSourceNaming(t *testing.T) {
	tests := []struct {
		name      string
		pName     string
		source    string
		wantErr   bool
		errorCode string
	}{
		{
			name:   "admin policy with admin source passes",
			pName:  "allow-say",
			source: "admin",
		},
		{
			name:   "seed-prefixed name with seed source passes",
			pName:  "seed:default-allow",
			source: "seed",
		},
		{
			name:   "lock-prefixed name with lock source passes",
			pName:  "lock:no-delete",
			source: "lock",
		},
		{
			name:   "plugin policy with plugin source passes",
			pName:  "echo-bot-allow",
			source: "plugin",
		},
		{
			name:      "seed-prefixed name with admin source fails",
			pName:     "seed:default-allow",
			source:    "admin",
			wantErr:   true,
			errorCode: "POLICY_SOURCE_MISMATCH",
		},
		{
			name:      "non-seed name with seed source fails",
			pName:     "allow-say",
			source:    "seed",
			wantErr:   true,
			errorCode: "POLICY_SOURCE_MISMATCH",
		},
		{
			name:      "lock-prefixed name with admin source fails",
			pName:     "lock:no-delete",
			source:    "admin",
			wantErr:   true,
			errorCode: "POLICY_SOURCE_MISMATCH",
		},
		{
			name:      "non-lock name with lock source fails",
			pName:     "no-delete",
			source:    "lock",
			wantErr:   true,
			errorCode: "POLICY_SOURCE_MISMATCH",
		},
		{
			name:   "short name does not match seed prefix",
			pName:  "seed",
			source: "admin",
		},
		{
			name:   "short name does not match lock prefix",
			pName:  "lock",
			source: "admin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.ValidateSourceNaming(tt.pName, tt.source)
			if tt.wantErr {
				assert.Error(t, err)
				errutil.AssertErrorCode(t, err, tt.errorCode)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateGrammarVersion(t *testing.T) {
	tests := []struct {
		name      string
		ast       json.RawMessage
		wantErr   bool
		errorCode string
	}{
		{
			name: "valid AST with grammar_version",
			ast:  json.RawMessage(`{"type":"policy","grammar_version":1}`),
		},
		{
			name:      "empty AST is rejected",
			ast:       nil,
			wantErr:   true,
			errorCode: "POLICY_INVALID_AST",
		},
		{
			name:      "AST missing grammar_version",
			ast:       json.RawMessage(`{"type":"policy","effect":"permit"}`),
			wantErr:   true,
			errorCode: "POLICY_INVALID_AST",
		},
		{
			name:      "invalid JSON",
			ast:       json.RawMessage(`not json`),
			wantErr:   true,
			errorCode: "POLICY_INVALID_AST",
		},
		{
			name:      "JSON array instead of object",
			ast:       json.RawMessage(`[1,2,3]`),
			wantErr:   true,
			errorCode: "POLICY_INVALID_AST",
		},
		{
			name:      "grammar_version as string is rejected",
			ast:       json.RawMessage(`{"type":"policy","grammar_version":"1"}`),
			wantErr:   true,
			errorCode: "POLICY_INVALID_AST",
		},
		{
			name:      "grammar_version as bool is rejected",
			ast:       json.RawMessage(`{"type":"policy","grammar_version":true}`),
			wantErr:   true,
			errorCode: "POLICY_INVALID_AST",
		},
		{
			name:      "grammar_version as object is rejected",
			ast:       json.RawMessage(`{"type":"policy","grammar_version":{"v":1}}`),
			wantErr:   true,
			errorCode: "POLICY_INVALID_AST",
		},
		{
			name:      "grammar_version zero is rejected",
			ast:       json.RawMessage(`{"type":"policy","grammar_version":0}`),
			wantErr:   true,
			errorCode: "POLICY_INVALID_AST",
		},
		{
			name:      "grammar_version negative is rejected",
			ast:       json.RawMessage(`{"type":"policy","grammar_version":-1}`),
			wantErr:   true,
			errorCode: "POLICY_INVALID_AST",
		},
		{
			name: "grammar_version 2 is accepted",
			ast:  json.RawMessage(`{"type":"policy","grammar_version":2}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.ValidateGrammarVersion(tt.ast)
			if tt.wantErr {
				assert.Error(t, err)
				errutil.AssertErrorCode(t, err, tt.errorCode)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestStoredPolicy_EffectUsesTypesPackage(t *testing.T) {
	// Verify that StoredPolicy.Effect is types.PolicyEffect, not a raw string.
	// This is a compile-time test — if it compiles, the type constraint holds.
	p := store.StoredPolicy{
		Effect: "permit",
	}
	assert.Equal(t, "permit", p.Effect.String())
}
