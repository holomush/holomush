// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package authguard

import (
	plugins "github.com/holomush/holomush/internal/plugin"
)

type manifestAdapter struct{ mgr *plugins.Manager }

// NewPluginManifestLookup wraps a *plugins.Manager as a ManifestLookup.
func NewPluginManifestLookup(mgr *plugins.Manager) ManifestLookup {
	return &manifestAdapter{mgr: mgr}
}

func (a *manifestAdapter) PluginRequestsDecryption(pluginName, eventType string) bool {
	if a == nil || a.mgr == nil {
		return false
	}
	return a.mgr.PluginRequestsDecryption(pluginName, eventType)
}

func (a *manifestAdapter) PluginCanReadBack(pluginName, eventType string) bool {
	if a == nil || a.mgr == nil {
		return false
	}
	return a.mgr.PluginCanReadBack(pluginName, eventType)
}
