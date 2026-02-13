// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package policy provides benchmarks for the ABAC policy engine.
//
// Run benchmarks from the repository root:
//
//	go test -bench=. -benchmem -count=3 ./internal/access/policy/ -run=^$
//
// Run specific benchmark:
//
//	go test -bench=BenchmarkSinglePolicyEvaluation -benchmem ./internal/access/policy/ -run=^$
//
// Performance targets:
//   - BenchmarkSinglePolicyEvaluation: <10μs per operation
//   - BenchmarkConditionEvaluation: <1ms per operation
//   - BenchmarkFiftyPolicyEvaluation: <100μs per operation
//   - BenchmarkAttributeResolution: <50μs per operation
//   - BenchmarkWorstCase_NestedIf: <25ms per operation
//   - BenchmarkWorstCase_AllPoliciesMatch: <10ms per operation
package policy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/audit"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// noopAuditWriter discards all audit entries for benchmarking.
type noopAuditWriter struct{}

func (n *noopAuditWriter) WriteSync(_ context.Context, _ audit.Entry) error { return nil }
func (n *noopAuditWriter) WriteAsync(_ audit.Entry) error                   { return nil }
func (n *noopAuditWriter) Close() error                                     { return nil }

// noopSessionResolver always returns empty for benchmarking.
type noopSessionResolver struct{}

func (n *noopSessionResolver) ResolveSession(_ context.Context, _ string) (string, error) {
	return "", nil
}

// benchAttributeProvider provides in-memory attributes for benchmarking.
type benchAttributeProvider struct {
	namespace string
	attrs     map[string]any
}

func (p *benchAttributeProvider) Namespace() string {
	return p.namespace
}

func (p *benchAttributeProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return p.attrs, nil
}

func (p *benchAttributeProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

func (p *benchAttributeProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"role":    types.AttrTypeString,
			"level":   types.AttrTypeFloat,
			"banned":  types.AttrTypeBool,
			"faction": types.AttrTypeString,
			"muted":   types.AttrTypeBool,
			"score":   types.AttrTypeFloat,
			"rank":    types.AttrTypeString,
			"credits": types.AttrTypeFloat,
			"karma":   types.AttrTypeFloat,
			"xp":      types.AttrTypeFloat,
		},
	}
}

// createBenchEngine creates an engine with in-memory dependencies for benchmarking.
func createBenchEngine(b *testing.B, dslTexts []string, attrs map[string]any) *Engine {
	b.Helper()

	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)

	provider := &benchAttributeProvider{
		namespace: "character",
		attrs:     attrs,
	}
	if err := resolver.RegisterProvider(provider); err != nil {
		b.Fatal(err)
	}

	tmpDir := b.TempDir()
	walPath := filepath.Join(tmpDir, "bench-wal.jsonl")
	auditLogger := audit.NewLogger(audit.ModeAll, &noopAuditWriter{}, walPath)
	b.Cleanup(func() {
		_ = auditLogger.Close()
		_ = os.Remove(walPath)
	})

	schema := types.NewAttributeSchema()
	compiler := NewCompiler(schema)

	policies := make([]CachedPolicy, 0, len(dslTexts))
	for i, text := range dslTexts {
		compiled, _, err := compiler.Compile(text)
		if err != nil {
			b.Fatalf("compile policy %d: %v", i, err)
		}
		policies = append(policies, CachedPolicy{
			ID:       fmt.Sprintf("policy-%d", i+1),
			Name:     fmt.Sprintf("bench-policy-%d", i+1),
			Compiled: compiled,
		})
	}

	cache := NewCache(nil, nil)
	cache.mu.Lock()
	cache.snapshot = &Snapshot{
		Policies:  policies,
		CreatedAt: time.Now(),
	}
	cache.mu.Unlock()
	cache.lastUpdate.Store(time.Now().UnixNano())

	engine := NewEngine(resolver, cache, &noopSessionResolver{}, auditLogger)
	return engine
}

// generateBenchPolicies creates N policies alternating between permit and forbid.
func generateBenchPolicies(count int) []string {
	var policies []string
	for i := 0; i < count; i++ {
		effect := "permit"
		if i%2 == 0 {
			effect = "forbid"
		}
		policies = append(policies, fmt.Sprintf(
			`%s(principal is character, action in ["say"], resource is location) when { principal.character.level > %d };`,
			effect, i,
		))
	}
	return policies
}

// generateNestedIfPolicy creates a policy with deeply nested if-then-else.
func generateNestedIfPolicy(depth int) string {
	dsl := `permit(principal is character, action in ["say"], resource is location) when { `

	for i := 0; i < depth; i++ {
		dsl += fmt.Sprintf("if principal.character.level > %d then ", i)
	}

	dsl += "true"

	for i := 0; i < depth; i++ {
		dsl += " else false"
	}

	dsl += " };"
	return dsl
}

// BenchmarkSinglePolicyEvaluation benchmarks a single policy evaluation.
// Target: <10μs
func BenchmarkSinglePolicyEvaluation(b *testing.B) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "admin" };`
	attrs := map[string]any{"role": "admin", "level": float64(10)}

	engine := createBenchEngine(b, []string{dslText}, attrs)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := engine.Evaluate(ctx, req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkConditionEvaluation benchmarks a single policy with 3 conditions.
// Target: <1ms per policy
func BenchmarkConditionEvaluation(b *testing.B) {
	dslText := `permit(principal is character, action in ["say"], resource is location) when {
		principal.character.role == "admin" &&
		principal.character.level > 5 &&
		principal.character.banned == false
	};`
	attrs := map[string]any{
		"role":   "admin",
		"level":  float64(10),
		"banned": false,
	}

	engine := createBenchEngine(b, []string{dslText}, attrs)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := engine.Evaluate(ctx, req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFiftyPolicyEvaluation benchmarks 50 policies.
// Target: <100μs
func BenchmarkFiftyPolicyEvaluation(b *testing.B) {
	policies := generateBenchPolicies(50)
	attrs := map[string]any{"role": "admin", "level": float64(25)}

	engine := createBenchEngine(b, policies, attrs)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := engine.Evaluate(ctx, req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAttributeResolution benchmarks in-memory attribute resolution.
// Target: <50μs
func BenchmarkAttributeResolution(b *testing.B) {
	attrs := map[string]any{
		"role":    "admin",
		"level":   float64(10),
		"banned":  false,
		"faction": "rebels",
		"muted":   false,
		"score":   float64(1000),
		"rank":    "general",
		"credits": float64(5000),
		"karma":   float64(50),
		"xp":      float64(10000),
	}

	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)

	provider := &benchAttributeProvider{
		namespace: "character",
		attrs:     attrs,
	}
	if err := resolver.RegisterProvider(provider); err != nil {
		b.Fatal(err)
	}

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := resolver.Resolve(ctx, req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWorstCase_NestedIf benchmarks deeply nested if-then-else (10 levels).
// Target: <25ms
func BenchmarkWorstCase_NestedIf(b *testing.B) {
	dslText := generateNestedIfPolicy(10)
	attrs := map[string]any{"level": float64(15)}

	engine := createBenchEngine(b, []string{dslText}, attrs)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := engine.Evaluate(ctx, req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWorstCase_AllPoliciesMatch benchmarks 50 policies all matching.
// Target: <10ms
func BenchmarkWorstCase_AllPoliciesMatch(b *testing.B) {
	// All policies use level > 0, so with level=50 they all match
	policies := generateBenchPolicies(50)
	attrs := map[string]any{"level": float64(50)}

	engine := createBenchEngine(b, policies, attrs)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := engine.Evaluate(ctx, req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEvaluateEndToEnd benchmarks full Evaluate() with in-memory deps.
func BenchmarkEvaluateEndToEnd(b *testing.B) {
	policies := []string{
		`permit(principal is character, action in ["say"], resource is location) when { principal.character.role == "admin" };`,
		`permit(principal is character, action in ["say"], resource is location) when { principal.character.level > 5 };`,
		`forbid(principal is character, action in ["say"], resource is location) when { principal.character.banned == true };`,
	}
	attrs := map[string]any{
		"role":   "admin",
		"level":  float64(10),
		"banned": false,
	}

	engine := createBenchEngine(b, policies, attrs)

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "say",
		Resource: "location:01XYZ",
	}

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		decision, err := engine.Evaluate(ctx, req)
		if err != nil {
			b.Fatal(err)
		}
		if !decision.IsAllowed() {
			b.Fatal("expected allowed decision")
		}
	}
}
