// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/dsl"
	"github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// PluginPolicyInstaller manages installation and removal of ABAC policies
// declared in plugin manifests.
type PluginPolicyInstaller interface {
	InstallPluginPolicies(ctx context.Context, pluginName string, policies []ManifestPolicy) error
	InstallPluginPoliciesWithManifest(ctx context.Context, manifest *Manifest, policies []ManifestPolicy) error
	RemovePluginPolicies(ctx context.Context, pluginName string) error
	ReplacePluginPolicies(ctx context.Context, pluginName string, policies []ManifestPolicy) error
	ReplacePluginPoliciesWithManifest(ctx context.Context, manifest *Manifest, policies []ManifestPolicy) error
}

// policyStoreWriter is a narrow interface for policy persistence, keeping
// the plugin package decoupled from the full PolicyStore.
type policyStoreWriter interface {
	Create(ctx context.Context, p *store.StoredPolicy) error
	CreateBatch(ctx context.Context, policies []*store.StoredPolicy) error
	DeleteBySource(ctx context.Context, source, namePrefix string) (int64, error)
	ReplaceBySource(ctx context.Context, source, namePrefix string, policies []*store.StoredPolicy) error
}

// PolicyInstaller implements PluginPolicyInstaller using the DSL compiler
// and a policy store writer.
type PolicyInstaller struct {
	store          policyStoreWriter
	trustAllowlist map[string]bool // server-side trust escalation allowlist
}

// NewPolicyInstaller creates a PolicyInstaller backed by the given store writer.
func NewPolicyInstaller(w policyStoreWriter) *PolicyInstaller {
	return &PolicyInstaller{store: w}
}

// SetTrustAllowlist configures the server-side allowlist of plugin names
// permitted to use trust escalation. Must be called before installing policies.
func (pi *PolicyInstaller) SetTrustAllowlist(names []string) {
	pi.trustAllowlist = make(map[string]bool, len(names))
	for _, n := range names {
		pi.trustAllowlist[n] = true
	}
}

// compilePolicies parses and validates manifest policies, returning StoredPolicy
// structs ready for persistence.
func compilePolicies(pluginName string, policies []ManifestPolicy) ([]*store.StoredPolicy, error) {
	result := make([]*store.StoredPolicy, 0, len(policies))
	for _, mp := range policies {
		parsed, err := dsl.Parse(mp.DSL)
		if err != nil {
			return nil, oops.
				With("plugin", pluginName).
				With("policy", mp.Name).
				Wrapf(err, "compiling plugin policy DSL")
		}

		if parsed.Target == nil || parsed.Target.Principal == nil || parsed.Target.Principal.Type != "plugin" {
			return nil, oops.
				With("plugin", pluginName).
				With("policy", mp.Name).
				Errorf("plugin policies must declare principal type \"plugin\"")
		}

		// Validate that the policy only references the installing plugin's name
		if ok, foreignName := dsl.ValidatePrincipalScope(parsed, pluginName); !ok {
			return nil, oops.
				With("plugin", pluginName).
				With("policy", mp.Name).
				With("foreign_principal", foreignName).
				Errorf("plugin policy references foreign principal %q; plugins can only grant permissions to themselves", foreignName)
		}

		compiled, err := dsl.CompilePolicy(parsed)
		if err != nil {
			return nil, oops.
				With("plugin", pluginName).
				With("policy", mp.Name).
				Wrapf(err, "compiling plugin policy AST")
		}

		result = append(result, &store.StoredPolicy{
			Name:        "plugin:" + pluginName + ":" + mp.Name,
			Description: "Auto-installed policy from plugin " + pluginName,
			Effect:      types.PolicyEffect(parsed.Effect),
			Source:      "plugin",
			DSLText:     mp.DSL,
			CompiledAST: compiled,
			Enabled:     true,
			CreatedBy:   "plugin:" + pluginName,
		})
	}
	return result, nil
}

// compilePoliciesWithManifest parses and validates manifest policies using the
// full plugin trust boundary (resource types, commands, trust escalation) from
// the manifest. Trust escalation requires BOTH the manifest declaring
// trust.all_principals AND the plugin name appearing in the server-side
// allowlist. Returns StoredPolicy structs ready for persistence.
func compilePoliciesWithManifest(manifest *Manifest, policies []ManifestPolicy, trustAllowlist map[string]bool) ([]*store.StoredPolicy, error) {
	pluginName := manifest.Name

	cmdNames := make([]string, len(manifest.Commands))
	for i := range manifest.Commands {
		cmdNames[i] = manifest.Commands[i].Name
	}

	// Trust escalation requires both manifest declaration AND server allowlist match.
	trustEscalated := manifest.Trust != nil && manifest.Trust.AllPrincipals && trustAllowlist[pluginName]

	valCtx := PolicyValidationContext{
		PluginName:     pluginName,
		ResourceTypes:  manifest.ResourceTypes,
		CommandNames:   cmdNames,
		TrustEscalated: trustEscalated,
	}

	result := make([]*store.StoredPolicy, 0, len(policies))
	for _, mp := range policies {
		if err := ValidatePluginPolicy(valCtx, mp); err != nil {
			return nil, oops.
				With("plugin", pluginName).
				With("policy", mp.Name).
				Wrapf(err, "validating plugin policy")
		}

		parsed, err := dsl.Parse(mp.DSL)
		if err != nil {
			return nil, oops.
				With("plugin", pluginName).
				With("policy", mp.Name).
				Wrapf(err, "compiling plugin policy DSL")
		}

		compiled, err := dsl.CompilePolicy(parsed)
		if err != nil {
			return nil, oops.
				With("plugin", pluginName).
				With("policy", mp.Name).
				Wrapf(err, "compiling plugin policy AST")
		}

		result = append(result, &store.StoredPolicy{
			Name:        "plugin:" + pluginName + ":" + mp.Name,
			Description: "Auto-installed policy from plugin " + pluginName,
			Effect:      types.PolicyEffect(parsed.Effect),
			Source:      "plugin",
			DSLText:     mp.DSL,
			CompiledAST: compiled,
			Enabled:     true,
			CreatedBy:   "plugin:" + pluginName,
		})
	}
	return result, nil
}

// InstallPluginPolicies compiles each manifest policy via the DSL compiler,
// validates that the principal type is "plugin", and persists the policies.
// This is idempotent — existing policies for the plugin are replaced.
func (pi *PolicyInstaller) InstallPluginPolicies(ctx context.Context, pluginName string, policies []ManifestPolicy) error {
	compiled, err := compilePolicies(pluginName, policies)
	if err != nil {
		return err
	}
	if err := pi.store.ReplaceBySource(ctx, "plugin", "plugin:"+pluginName+":", compiled); err != nil {
		return oops.With("plugin", pluginName).Wrapf(err, "installing plugin policies")
	}
	return nil
}

// InstallPluginPoliciesWithManifest compiles each manifest policy using the
// full trust boundary from the manifest (resource types, commands, trust
// escalation). This is idempotent — existing policies for the plugin are
// replaced.
func (pi *PolicyInstaller) InstallPluginPoliciesWithManifest(ctx context.Context, manifest *Manifest, policies []ManifestPolicy) error {
	compiled, err := compilePoliciesWithManifest(manifest, policies, pi.trustAllowlist)
	if err != nil {
		return err
	}
	if err := pi.store.ReplaceBySource(ctx, "plugin", "plugin:"+manifest.Name+":", compiled); err != nil {
		return oops.With("plugin", manifest.Name).Wrapf(err, "installing plugin policies")
	}
	return nil
}

// RemovePluginPolicies deletes all policies installed by the named plugin.
func (pi *PolicyInstaller) RemovePluginPolicies(ctx context.Context, pluginName string) error {
	_, err := pi.store.DeleteBySource(ctx, "plugin", "plugin:"+pluginName+":")
	if err != nil {
		return oops.With("plugin", pluginName).Wrapf(err, "removing plugin policies")
	}
	return nil
}

// ReplacePluginPolicies atomically removes existing policies for the plugin
// and installs new ones within a single transaction.
func (pi *PolicyInstaller) ReplacePluginPolicies(ctx context.Context, pluginName string, policies []ManifestPolicy) error {
	compiled, err := compilePolicies(pluginName, policies)
	if err != nil {
		return err
	}

	if err := pi.store.ReplaceBySource(ctx, "plugin", "plugin:"+pluginName+":", compiled); err != nil {
		slog.Error("atomic policy replace failed",
			"plugin", pluginName, "error", err)
		return oops.With("plugin", pluginName).Wrapf(err, "replacing plugin policies")
	}
	return nil
}

// ReplacePluginPoliciesWithManifest atomically removes existing policies for
// the plugin and installs new ones using the full trust boundary from the
// manifest.
func (pi *PolicyInstaller) ReplacePluginPoliciesWithManifest(ctx context.Context, manifest *Manifest, policies []ManifestPolicy) error {
	compiled, err := compilePoliciesWithManifest(manifest, policies, pi.trustAllowlist)
	if err != nil {
		return err
	}

	if err := pi.store.ReplaceBySource(ctx, "plugin", "plugin:"+manifest.Name+":", compiled); err != nil {
		slog.Error("atomic policy replace failed",
			"plugin", manifest.Name, "error", err)
		return oops.With("plugin", manifest.Name).Wrapf(err, "replacing plugin policies")
	}
	return nil
}
