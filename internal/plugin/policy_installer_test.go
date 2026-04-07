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

func TestPolicyInstallerInstall(t *testing.T) {
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

func TestPolicyInstallerInstallRejectNonPluginPrincipal(t *testing.T) {
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

func TestPolicyInstallerInstallRejectNilPrincipalType(t *testing.T) {
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

func TestPolicyInstallerRemove(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	err := installer.RemovePluginPolicies(context.Background(), "my-plugin")
	require.NoError(t, err)
	assert.Equal(t, "plugin", fs.deletedSource)
	assert.Equal(t, "plugin:my-plugin:", fs.deletedPrefix)
}

func TestPolicyInstallerInstallCompilationError(t *testing.T) {
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

func TestPolicyInstallerInstallRejectCrossPluginPrincipal(t *testing.T) {
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

func TestPolicyInstallerAcceptsCharacterPolicyOnDeclaredResourceType(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	policies := []plugins.ManifestPolicy{
		{
			Name: "widget-read",
			DSL:  `permit(principal is character, action in ["read"], resource is widget);`,
		},
	}

	manifest := &plugins.Manifest{
		Name:          "my-plugin",
		Version:       "1.0.0",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"widget"},
		Commands:      []plugins.CommandSpec{{Name: "widget"}},
	}

	err := installer.InstallPluginPoliciesWithManifest(context.Background(), manifest, policies)
	require.NoError(t, err)
	assert.Len(t, fs.created, 1)
}

func TestPolicyInstallerRejectsCharacterPolicyOnProtectedType(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	policies := []plugins.ManifestPolicy{
		{
			Name: "bad-policy",
			DSL:  `permit(principal is character, action in ["read"], resource is location);`,
		},
	}

	manifest := &plugins.Manifest{
		Name:          "my-plugin",
		Version:       "1.0.0",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"widget"},
	}

	err := installer.InstallPluginPoliciesWithManifest(context.Background(), manifest, policies)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "protected")
}

func TestPolicyInstallerWithManifestReplacePolicies(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	policies := []plugins.ManifestPolicy{
		{
			Name: "widget-write",
			DSL:  `permit(principal is character, action in ["write"], resource is widget);`,
		},
	}

	manifest := &plugins.Manifest{
		Name:          "my-plugin",
		Version:       "1.0.0",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"widget"},
		Commands:      []plugins.CommandSpec{{Name: "widget"}},
	}

	err := installer.ReplacePluginPoliciesWithManifest(context.Background(), manifest, policies)
	require.NoError(t, err)
	assert.Len(t, fs.created, 1)
	assert.Equal(t, "plugin:my-plugin:widget-write", fs.created[0].Name)
	// Prove the replace path actually ran (not the install path) by checking
	// the fakePolicyStore recorded a delete-by-source call. Without this
	// assertion, the test would also pass if ReplacePluginPoliciesWithManifest
	// silently fell through to InstallPluginPolicies.
	assert.Equal(t, "plugin", fs.deletedSource,
		"replace path must call ReplaceBySource which records the source")
	assert.Equal(t, "plugin:my-plugin:", fs.deletedPrefix,
		"replace path must scope the delete to this plugin's policy prefix")
}

func TestPolicyInstallerWithManifestStillAcceptsPluginPrincipal(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	policies := []plugins.ManifestPolicy{
		{
			Name: "allow-kv-read",
			DSL:  `permit(principal is plugin, action in ["kv:read"], resource is kv);`,
		},
	}

	manifest := &plugins.Manifest{
		Name:    "my-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
	}

	err := installer.InstallPluginPoliciesWithManifest(context.Background(), manifest, policies)
	require.NoError(t, err)
	assert.Len(t, fs.created, 1)
}

func TestPolicyInstallerWithManifestRejectsSystemPrincipal(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	policies := []plugins.ManifestPolicy{
		{
			Name: "system-policy",
			DSL:  `permit(principal is system, action in ["admin"], resource is server);`,
		},
	}

	manifest := &plugins.Manifest{
		Name:    "my-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
	}

	err := installer.InstallPluginPoliciesWithManifest(context.Background(), manifest, policies)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "system")
}

func TestPolicyInstallerTrustEscalationRequiresServerAllowlist(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)
	// No allowlist configured — trust escalation should NOT work even if
	// manifest declares trust.all_principals: true.

	policies := []plugins.ManifestPolicy{
		{
			Name: "grab-locations",
			DSL:  `permit(principal is character, action in ["read"], resource is location);`,
		},
	}

	manifest := &plugins.Manifest{
		Name:          "sneaky-plugin",
		Version:       "1.0.0",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"gadget"},
		Trust:         &plugins.TrustConfig{AllPrincipals: true},
	}

	err := installer.InstallPluginPoliciesWithManifest(context.Background(), manifest, policies)
	require.Error(t, err, "trust escalation should be rejected without server allowlist")
	assert.Contains(t, err.Error(), "protected")
}

func TestPolicyInstallerTrustEscalationSucceedsWithServerAllowlist(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)
	installer.SetTrustAllowlist([]string{"trusted-plugin"})

	policies := []plugins.ManifestPolicy{
		{
			Name: "read-locations",
			DSL:  `permit(principal is character, action in ["read"], resource is location);`,
		},
	}

	manifest := &plugins.Manifest{
		Name:          "trusted-plugin",
		Version:       "1.0.0",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"gadget"},
		Trust:         &plugins.TrustConfig{AllPrincipals: true},
	}

	err := installer.InstallPluginPoliciesWithManifest(context.Background(), manifest, policies)
	require.NoError(t, err, "allowlisted plugin with trust declaration should succeed")
	assert.Len(t, fs.created, 1)
}

func TestPolicyInstallerTrustAllowlistIgnoresNonMatchingPlugin(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)
	installer.SetTrustAllowlist([]string{"other-plugin"})

	policies := []plugins.ManifestPolicy{
		{
			Name: "grab-locations",
			DSL:  `permit(principal is character, action in ["read"], resource is location);`,
		},
	}

	manifest := &plugins.Manifest{
		Name:          "sneaky-plugin",
		Version:       "1.0.0",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"gadget"},
		Trust:         &plugins.TrustConfig{AllPrincipals: true},
	}

	err := installer.InstallPluginPoliciesWithManifest(context.Background(), manifest, policies)
	require.Error(t, err, "non-allowlisted plugin should be rejected despite trust declaration")
	assert.Contains(t, err.Error(), "protected")
}

func TestPolicyInstallerWithManifestRejectsInvalidDSL(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	manifest := &plugins.Manifest{
		Name:          "broken-plugin",
		Version:       "1.0.0",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"widget"},
	}
	policies := []plugins.ManifestPolicy{
		{Name: "bad", DSL: "not valid dsl at all"},
	}

	err := installer.InstallPluginPoliciesWithManifest(context.Background(), manifest, policies)
	require.Error(t, err, "manifest installer must surface DSL parse errors")
	assert.Empty(t, fs.created, "no policies should be persisted on parse failure")
}

func TestPolicyInstallerReplaceWithManifestRejectsInvalidDSL(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	manifest := &plugins.Manifest{
		Name:    "broken-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
	}
	policies := []plugins.ManifestPolicy{
		{Name: "bad", DSL: "permit(garbage)"},
	}

	err := installer.ReplacePluginPoliciesWithManifest(context.Background(), manifest, policies)
	require.Error(t, err, "Replace path should also reject malformed DSL")
	assert.Empty(t, fs.created)
}

func TestPolicyInstallerReplaceRejectsInvalidDSL(t *testing.T) {
	// Covers ReplacePluginPolicies error path (compilePolicies failure
	// before the store is touched).
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	policies := []plugins.ManifestPolicy{
		{Name: "bad", DSL: "this is not dsl"},
	}

	err := installer.ReplacePluginPolicies(context.Background(), "my-plugin", policies)
	require.Error(t, err)
	assert.Empty(t, fs.created)
}

func TestPolicyInstallerReplaceSucceeds(t *testing.T) {
	// Covers the happy path through ReplacePluginPolicies — store.ReplaceBySource
	// is called and a compiled policy lands in the fake store.
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	policies := []plugins.ManifestPolicy{
		{
			Name: "allow-kv-read",
			DSL:  `permit(principal is plugin, action in ["kv:read"], resource is kv);`,
		},
	}

	err := installer.ReplacePluginPolicies(context.Background(), "my-plugin", policies)
	require.NoError(t, err)
	require.Len(t, fs.created, 1)
	assert.Equal(t, "plugin:my-plugin:allow-kv-read", fs.created[0].Name)
}
