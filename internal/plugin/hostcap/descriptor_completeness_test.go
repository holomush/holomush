// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// TestEveryScopeEligibleMethodHasExtractor is a build-time meta-test: a scope-
// eligible descriptor method (one carrying scope tokens) MUST wire a resource
// Extract, or the interceptor's scope half has no resource to authorize against
// — a fail-open hazard. Adding scopes to a method without an extractor fails CI.
//
// Verifies: INV-PLUGIN-52
func TestEveryScopeEligibleMethodHasExtractor(t *testing.T) {
	for token, d := range Descriptors {
		for name, m := range d.Methods {
			if len(m.Scopes) > 0 {
				require.NotNilf(t, m.Extract,
					"capability %q method %q is scope-eligible but has no extractor (fail-open hazard)",
					token, name)
			}
		}
	}
}

// TestInterceptorScopedMethodWithoutExtractorFailsClosed proves the interceptor's
// runtime guard fires when a scope-eligible method has a nil Extract. All real
// scoped methods carry extractors (enforced by the meta-test above), so we
// temporarily strip the extractor off a real scoped descriptor (CreateExit),
// restoring it via t.Cleanup so the global table is unaffected afterward. The
// guard MUST deny with SCOPE_NO_EXTRACTOR before any policy evaluation.
//
// Verifies: INV-PLUGIN-52
func TestInterceptorScopedMethodWithoutExtractorFailsClosed(t *testing.T) {
	// Save + restore the real CreateExit descriptor so other tests are unaffected.
	orig := Descriptors["world.mutation"].Methods["CreateExit"]
	t.Cleanup(func() { Descriptors["world.mutation"].Methods["CreateExit"] = orig })
	noExtract := orig
	noExtract.Extract = nil
	Descriptors["world.mutation"].Methods["CreateExit"] = noExtract

	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         ownLocationEngine{}, // guard fires before eval; engine choice is irrelevant
		PluginName:     "builder-bot",
		DeclaredAccess: func(_, _ string) (string, bool) { return "write", true },
	})
	_, err := ic(scopedDispatchCtx(testLocID), &hostv1.CreateExitRequest{FromId: testLocID},
		createExitInfo(), okHandler)
	errutil.AssertErrorCode(t, err, "SCOPE_NO_EXTRACTOR")
}
