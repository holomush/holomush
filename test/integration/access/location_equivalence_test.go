//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access_test

import (
	"context"
	"fmt"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/audit"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// testLocationResolver implements access.LocationResolver for tests.
type testLocationResolver struct {
	locations map[string]string // charID → locationID
}

func (r *testLocationResolver) CurrentLocation(_ context.Context, charID string) (string, error) {
	loc, ok := r.locations[charID]
	if !ok {
		return "", fmt.Errorf("character %s not found", charID)
	}
	return loc, nil
}

func (r *testLocationResolver) CharactersAt(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// testLocationCharProvider resolves character attributes including location.
type testLocationCharProvider struct {
	roles     map[string]string // "charID" → role (extracted from "character:charID")
	locations map[string]string // "charID" → locationID
}

func (p *testLocationCharProvider) Namespace() string { return "character" }

func (p *testLocationCharProvider) ResolveSubject(_ context.Context, subjectID string) (map[string]any, error) {
	// Subject format: "character:ULID"
	id := subjectID
	role := p.roles[id]
	if role == "" {
		role = "player"
	}
	attrs := map[string]any{
		"id":   id,
		"role": role,
	}
	if loc, ok := p.locations[id]; ok {
		attrs["location"] = loc
		attrs["has_location"] = true
	} else {
		attrs["location"] = ""
		attrs["has_location"] = false
	}
	return attrs, nil
}

func (p *testLocationCharProvider) ResolveResource(ctx context.Context, resourceID string) (map[string]any, error) {
	return p.ResolveSubject(ctx, resourceID)
}

func (p *testLocationCharProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"id":           types.AttrTypeString,
			"role":         types.AttrTypeString,
			"location":     types.AttrTypeString,
			"has_location": types.AttrTypeBool,
		},
	}
}

// testLocationResourceProvider resolves location resource attributes.
type testLocationResourceProvider struct {
	knownLocations map[string]bool // locationID → exists
}

func (p *testLocationResourceProvider) Namespace() string { return "location" }
func (p *testLocationResourceProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

func (p *testLocationResourceProvider) ResolveResource(_ context.Context, resourceID string) (map[string]any, error) {
	// Resource format: "location:ULID"
	if !p.knownLocations[resourceID] {
		return nil, nil
	}
	return map[string]any{
		"id": resourceID,
	}, nil
}

func (p *testLocationResourceProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"id": types.AttrTypeString,
		},
	}
}

var _ = Describe("Location-based permission equivalence", func() {
	var (
		ctx    context.Context
		charID ulid.ULID
		locID  ulid.ULID
	)

	BeforeEach(func() {
		ctx = context.Background()
		charID = ulid.Make()
		locID = ulid.Make()
	})

	Describe("Player read access to current location", func() {
		It("produces identical decisions in both engines", func() {
			charIDStr := charID.String()
			locIDStr := locID.String()
			subject := access.CharacterSubject(charIDStr)

			// --- Static engine: uses $here token via LocationResolver ---
			locResolver := &testLocationResolver{
				locations: map[string]string{
					charIDStr: locIDStr,
				},
			}
			staticEngine := access.NewStaticAccessControl(locResolver, nil)
			err := staticEngine.AssignRole(subject, "player")
			Expect(err).NotTo(HaveOccurred())

			staticAllowed := staticEngine.Check(ctx, subject, "read", "location:"+locIDStr)

			// --- ABAC engine: uses principal.character.location attribute ---
			charProvider := &testLocationCharProvider{
				roles:     map[string]string{charIDStr: "player"},
				locations: map[string]string{charIDStr: locIDStr},
			}
			locProvider := &testLocationResourceProvider{
				knownLocations: map[string]bool{locIDStr: true},
			}

			resolver := buildTestResolver(charProvider, locProvider)

			// Compile the location-read seed policy
			locationReadPolicy := `permit(principal is character, action in ["read"], resource is location) when { resource.location.id == principal.character.location };`

			schema := types.NewAttributeSchema()
			compiler := policy.NewCompiler(schema)
			compiled, _, compileErr := compiler.Compile(locationReadPolicy)
			Expect(compileErr).NotTo(HaveOccurred())

			cache := policy.NewCacheWithPoliciesForTest([]policy.CachedPolicy{
				{ID: "loc-read-1", Name: "seed:player-location-read", Compiled: compiled},
			})

			tmpDir := GinkgoT().TempDir()
			auditLogger := audit.NewLogger(audit.ModeMinimal, &discardAuditWriter{}, tmpDir+"/wal.jsonl")
			defer auditLogger.Close()

			engine := policy.NewEngine(resolver, cache, nil, auditLogger)

			req, reqErr := types.NewAccessRequest(subject, "read", "location:"+locIDStr)
			Expect(reqErr).NotTo(HaveOccurred())

			decision, evalErr := engine.Evaluate(ctx, req)
			Expect(evalErr).NotTo(HaveOccurred())

			abacAllowed := decision.IsAllowed()

			// Both engines MUST agree
			Expect(abacAllowed).To(Equal(staticAllowed),
				"Static engine: %v, ABAC engine: %v (reason: %s)", staticAllowed, abacAllowed, decision.Reason())
			Expect(staticAllowed).To(BeTrue(), "Player should be allowed to read current location")
		})

		It("denies read access to a different location", func() {
			charIDStr := charID.String()
			locIDStr := locID.String()
			otherLocID := ulid.Make().String()
			subject := access.CharacterSubject(charIDStr)

			// Static engine: character is at locID, requesting read on otherLocID
			locResolver := &testLocationResolver{
				locations: map[string]string{charIDStr: locIDStr},
			}
			staticEngine := access.NewStaticAccessControl(locResolver, nil)
			err := staticEngine.AssignRole(subject, "player")
			Expect(err).NotTo(HaveOccurred())

			staticAllowed := staticEngine.Check(ctx, subject, "read", "location:"+otherLocID)

			// ABAC engine: same setup, character at locID, resource is otherLocID
			charProvider := &testLocationCharProvider{
				roles:     map[string]string{charIDStr: "player"},
				locations: map[string]string{charIDStr: locIDStr},
			}
			locProvider := &testLocationResourceProvider{
				knownLocations: map[string]bool{otherLocID: true},
			}

			resolver := buildTestResolver(charProvider, locProvider)

			locationReadPolicy := `permit(principal is character, action in ["read"], resource is location) when { resource.location.id == principal.character.location };`
			schema := types.NewAttributeSchema()
			compiler := policy.NewCompiler(schema)
			compiled, _, compileErr := compiler.Compile(locationReadPolicy)
			Expect(compileErr).NotTo(HaveOccurred())

			cache := policy.NewCacheWithPoliciesForTest([]policy.CachedPolicy{
				{ID: "loc-read-1", Name: "seed:player-location-read", Compiled: compiled},
			})

			tmpDir := GinkgoT().TempDir()
			auditLogger := audit.NewLogger(audit.ModeMinimal, &discardAuditWriter{}, tmpDir+"/wal.jsonl")
			defer auditLogger.Close()

			engine := policy.NewEngine(resolver, cache, nil, auditLogger)

			req, reqErr := types.NewAccessRequest(subject, "read", "location:"+otherLocID)
			Expect(reqErr).NotTo(HaveOccurred())

			decision, evalErr := engine.Evaluate(ctx, req)
			Expect(evalErr).NotTo(HaveOccurred())

			abacAllowed := decision.IsAllowed()

			// Both engines MUST agree on denial
			Expect(abacAllowed).To(Equal(staticAllowed),
				"Static engine: %v, ABAC engine: %v (reason: %s)", staticAllowed, abacAllowed, decision.Reason())
			Expect(staticAllowed).To(BeFalse(), "Player should NOT be allowed to read a different location")
		})

		It("matches emit:stream:location behavior", func() {
			charIDStr := charID.String()
			locIDStr := locID.String()
			subject := access.CharacterSubject(charIDStr)

			// Static engine: has "emit:stream:location:$here" permission
			locResolver := &testLocationResolver{
				locations: map[string]string{charIDStr: locIDStr},
			}
			staticEngine := access.NewStaticAccessControl(locResolver, nil)
			err := staticEngine.AssignRole(subject, "player")
			Expect(err).NotTo(HaveOccurred())

			staticAllowed := staticEngine.Check(ctx, subject, "emit", "stream:location:"+locIDStr)

			// ABAC engine
			charProvider := &testLocationCharProvider{
				roles:     map[string]string{charIDStr: "player"},
				locations: map[string]string{charIDStr: locIDStr},
			}
			streamProvider := &testStreamProvider{
				streams: map[string]streamAttrs{
					"location:" + locIDStr: {name: "location:" + locIDStr, location: locIDStr},
				},
			}

			resolver := buildTestResolver(charProvider, streamProvider)

			streamEmitPolicy := `permit(principal is character, action in ["emit"], resource is stream) when { resource.stream.name like "location:*" && resource.stream.location == principal.character.location };`
			schema := types.NewAttributeSchema()
			compiler := policy.NewCompiler(schema)
			compiled, _, compileErr := compiler.Compile(streamEmitPolicy)
			Expect(compileErr).NotTo(HaveOccurred())

			cache := policy.NewCacheWithPoliciesForTest([]policy.CachedPolicy{
				{ID: "stream-emit-1", Name: "seed:player-stream-emit", Compiled: compiled},
			})

			tmpDir := GinkgoT().TempDir()
			auditLogger := audit.NewLogger(audit.ModeMinimal, &discardAuditWriter{}, tmpDir+"/wal.jsonl")
			defer auditLogger.Close()

			engine := policy.NewEngine(resolver, cache, nil, auditLogger)

			req, reqErr := types.NewAccessRequest(subject, "emit", "stream:location:"+locIDStr)
			Expect(reqErr).NotTo(HaveOccurred())

			decision, evalErr := engine.Evaluate(ctx, req)
			Expect(evalErr).NotTo(HaveOccurred())

			abacAllowed := decision.IsAllowed()

			Expect(abacAllowed).To(Equal(staticAllowed),
				"Static engine: %v, ABAC engine: %v (reason: %s)", staticAllowed, abacAllowed, decision.Reason())
			Expect(staticAllowed).To(BeTrue(), "Player should be allowed to emit to current location stream")
		})
	})
})

// --- Helpers ---

// buildTestResolver creates an attribute.Resolver with the given providers registered.
func buildTestResolver(providers ...attribute.AttributeProvider) *attribute.Resolver {
	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)
	for _, p := range providers {
		if err := resolver.RegisterProvider(p); err != nil {
			panic(fmt.Sprintf("failed to register provider %s: %v", p.Namespace(), err))
		}
	}
	return resolver
}

type streamAttrs struct {
	name     string
	location string
}

type testStreamProvider struct {
	streams map[string]streamAttrs // streamName → attrs
}

func (p *testStreamProvider) Namespace() string { return "stream" }
func (p *testStreamProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}
func (p *testStreamProvider) ResolveResource(_ context.Context, resourceID string) (map[string]any, error) {
	s, ok := p.streams[resourceID]
	if !ok {
		return nil, nil
	}
	return map[string]any{
		"name":     s.name,
		"location": s.location,
	}, nil
}
func (p *testStreamProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"name":     types.AttrTypeString,
			"location": types.AttrTypeString,
		},
	}
}

type discardAuditWriter struct{}

func (d *discardAuditWriter) WriteSync(_ context.Context, _ audit.Entry) error { return nil }
func (d *discardAuditWriter) WriteAsync(_ audit.Entry) error                   { return nil }
func (d *discardAuditWriter) Close() error                                     { return nil }
