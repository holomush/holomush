// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	plugins "github.com/holomush/holomush/internal/plugin"
)

// managerSource adapts *plugins.Manager to cryptowiring.ManifestSource so the
// prod call site in sub_grpc.go can pass a real Manager to
// cryptowiring.AlwaysSensitiveSet without importing the full plugin package
// into cryptowiring (which would create a circular dependency).
type managerSource struct{ mgr *plugins.Manager }

func (s managerSource) ListPlugins() []string { return s.mgr.ListPlugins() }

func (s managerSource) AlwaysSensitiveEmitTypes(name string) []string {
	dp, ok := s.mgr.GetLoadedPlugin(name)
	if !ok || dp.Manifest == nil || dp.Manifest.Crypto == nil {
		return nil
	}
	var out []string
	for _, emit := range dp.Manifest.Crypto.Emits {
		if emit.Sensitivity == plugins.SensitivityAlways {
			out = append(out, emit.EventType)
		}
	}
	return out
}
