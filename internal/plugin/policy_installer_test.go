// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"

	"github.com/holomush/holomush/internal/access/policy/store"
)

type fakePolicyStore struct {
	created       []*store.StoredPolicy
	deletedSource string
	deletedPrefix string
}

func (s *fakePolicyStore) Create(_ context.Context, p *store.StoredPolicy) error {
	s.created = append(s.created, p)
	return nil
}

func (s *fakePolicyStore) CreateBatch(_ context.Context, policies []*store.StoredPolicy) error {
	s.created = append(s.created, policies...)
	return nil
}

func (s *fakePolicyStore) DeleteBySource(_ context.Context, source, prefix string) (int64, error) {
	s.deletedSource = source
	s.deletedPrefix = prefix
	return 0, nil
}

func (s *fakePolicyStore) ReplaceBySource(_ context.Context, source, prefix string, policies []*store.StoredPolicy) error {
	s.deletedSource = source
	s.deletedPrefix = prefix
	s.created = append(s.created, policies...)
	return nil
}

func TestPolicyInstaller_Install(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	policies := []plugins.ManifestPolicy{
		{
			Name: "allow-kv-read",
			DSL:  `permit(principal is plugin, action in ["kv:read"], resource is kv);`,
		},
	}

	err := installer.InstallPluginPolicies(context.Background(), "my-plugin", policies)
	require.NoError(t, err)

	require.Len(t, fs.created, 1)
	p := fs.created[0]
	assert.Equal(t, "plugin:my-plugin:allow-kv-read", p.Name)
	assert.Equal(t, "plugin", p.Source)
	assert.Equal(t, "permit", string(p.Effect))
	assert.True(t, p.Enabled)
	assert.Equal(t, "plugin:my-plugin", p.CreatedBy)
	assert.NotEmpty(t, p.DSLText)
	assert.NotEmpty(t, p.CompiledAST)
}

func TestPolicyInstaller_Install_RejectNonPluginPrincipal(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	policies := []plugins.ManifestPolicy{
		{
			Name: "bad-policy",
			DSL:  `permit(principal is character, action in ["kv:read"], resource is kv);`,
		},
	}

	err := installer.InstallPluginPolicies(context.Background(), "my-plugin", policies)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin")
	assert.Empty(t, fs.created)
}

func TestPolicyInstaller_Install_RejectNilPrincipalType(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	policies := []plugins.ManifestPolicy{
		{
			Name: "no-principal-type",
			DSL:  `permit(principal, action in ["kv:read"], resource is kv);`,
		},
	}

	err := installer.InstallPluginPolicies(context.Background(), "my-plugin", policies)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin")
	assert.Empty(t, fs.created)
}

func TestPolicyInstaller_Remove(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	err := installer.RemovePluginPolicies(context.Background(), "my-plugin")
	require.NoError(t, err)
	assert.Equal(t, "plugin", fs.deletedSource)
	assert.Equal(t, "plugin:my-plugin:", fs.deletedPrefix)
}

func TestPolicyInstaller_Install_CompilationError(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	policies := []plugins.ManifestPolicy{
		{
			Name: "bad-dsl",
			DSL:  `this is not valid dsl at all`,
		},
	}

	err := installer.InstallPluginPolicies(context.Background(), "my-plugin", policies)
	require.Error(t, err)
	assert.Empty(t, fs.created)
}

func TestPolicyInstaller_Install_RejectCrossPluginPrincipal(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	policies := []plugins.ManifestPolicy{
		{
			Name: "cross-plugin",
			DSL:  `permit(principal is plugin, action, resource) when { principal.plugin.name == "other-plugin" };`,
		},
	}

	err := installer.InstallPluginPolicies(context.Background(), "my-plugin", policies)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "foreign principal")
	assert.Contains(t, err.Error(), "other-plugin")
	assert.Empty(t, fs.created)
}
