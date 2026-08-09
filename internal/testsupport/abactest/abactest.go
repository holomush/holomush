// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package abactest builds a REAL ABAC engine loaded with the full
// [policy.SeedPolicies] corpus, plus schema-pinned attribute-provider doubles,
// for tests that must prove a SEED POLICY rather than a fake's canned answer.
//
// # Why this package exists
//
// The equivalent builder inside internal/access/policy (createSeedEngine) is
// unexported, declared in a _test.go file, and belongs to `package policy`.
// Symbols in a _test.go file are visible only when building that package's own
// tests, so no other package can reach it. Plans 02-08, 02-09 and 02-10 all
// need a real-engine builder over the seed corpus.
//
// # Do NOT reach for policytest here
//
// internal/access/policy/policytest's engines return CANNED decisions. An
// assertion written against one asserts the fake's behaviour, not the policy's
// — which is precisely the class of false-green this package exists to remove.
//
// # Construction goes through the EXPORTED path
//
// createSeedEngine populates the cache by assigning the UNEXPORTED
// Cache.snapshot field under the UNEXPORTED Cache.mu, and NewEngine takes a
// CONCRETE *policy.Cache that no fake can substitute for. Go forbids all of
// that from outside package policy, so this builder cannot mirror it and does
// not try. It uses the exported population path instead:
//
//	NewCompiler → NewCache → Reload → NewEngine
//
// over an in-memory [store.PolicyStore]. That is strictly better than what it
// replaces: it exercises the real compile-and-load path, so a seed whose DSL
// does not compile fails HERE rather than being hand-installed past the
// compiler. It needs no database, so consumers stay in the untagged test lane.
//
// internal/testsupport/* is depguard-forbidden in production code, so this
// package is test-only by construction despite not being a _test.go file.
package abactest

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
)

// --- The engine builder ---

// NewSeedEngine returns a real [policy.Engine] loaded with the FULL
// [policy.SeedPolicies] corpus and the given attribute providers, compiled and
// installed through the exported NewCompiler → NewCache → Reload → NewEngine
// path. It touches no database.
//
// A provider must be registered for every namespace the assertion under test
// depends on: an unregistered namespace is not an error, the attribute is
// simply absent from the bag, and every condition referencing it evaluates
// false — so the policy default-denies invisibly.
func NewSeedEngine(t *testing.T, providers ...attribute.AttributeProvider) *policy.Engine {
	t.Helper()

	registry, resolver := newSeedSchemaRegistry(t, providers...)

	compiler := policy.NewCompiler(registry.Schema())
	cache := policy.NewCache(seedStore{}, compiler)
	require.NoError(t, cache.Reload(context.Background()),
		"the seed corpus MUST compile and load through the exported cache path")

	walPath := filepath.Join(t.TempDir(), "abactest-audit-wal.jsonl")
	auditLogger := audit.NewLogger(audit.ModeAll, discardAuditWriter{}, walPath)
	t.Cleanup(func() {
		if err := auditLogger.Close(); err != nil {
			t.Logf("abactest: closing the audit logger: %v", err)
		}
	})

	return policy.NewEngine(resolver, cache, unusedSessionResolver{}, auditLogger)
}

// newSeedSchemaRegistry builds the registry NewSeedEngine's compiler is built on:
// the caller's providers, plus the `action` namespace.
//
// Why `action` is registered here, unconditionally (02.2-04, D-66 site 4):
// NewSeedEngine compiles the FULL policy.SeedPolicies() corpus, and the compiler's
// action branch HARD-ERRORS on a reference to an undeclared action.* key
// (internal/access/policy/compiler.go). Without this registration
// HasNamespace("action") is false, that branch is skipped, and a seed carrying a
// typo'd action.* key compiles clean in the UNIT tier and only fails at boot —
// which is the same silent-default-deny failure the gate exists to prevent,
// arriving through a compilation site that carries a real registry.
//
// It has to be an explicit call rather than arriving with a provider like every
// other namespace this helper carries: `action` is a caller-supplied per-request
// bag with no provider by design (D-60), so it never appears in
// Resolver.RegisteredNamespaces() and no caller can pass it in.
//
// It is deliberately NOT opt-in — no variadic option, no second constructor. A
// gate that can be turned off will be off in the test that needed it. If this
// registration turns an existing test red, that test's policy text references an
// undeclared action.* key: a real finding to report, not a reason to make the
// registration conditional.
//
// This is extracted from NewSeedEngine rather than inlined so that
// action_registration_internal_test.go can drive the REAL construction path
// instead of asserting against a hand-built mirror of it.
func newSeedSchemaRegistry(
	t *testing.T,
	providers ...attribute.AttributeProvider,
) (*attribute.SchemaRegistry, *attribute.Resolver) {
	t.Helper()

	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)
	for _, p := range providers {
		require.NoError(t, resolver.RegisterProvider(p),
			"registering the %q provider", p.Namespace())
	}
	require.NoError(t, attribute.Register(registry, "action", attribute.ActionNamespaceSchema()),
		"the action namespace MUST register, or a bad action.* seed reference passes the unit tier")

	return registry, resolver
}

// seedStore is an in-memory [store.PolicyStore] serving the seed corpus.
// Cache.Reload calls ListEnabled and nothing else; every other method returns
// a not-implemented error rather than a zero value, so a future caller that
// reaches for one fails loudly instead of silently seeing an empty store.
type seedStore struct{}

var errUnsupported = oops.Code("ABACTEST_STORE_UNSUPPORTED").
	Errorf("abactest.seedStore serves ListEnabled only")

func (seedStore) ListEnabled(_ context.Context) ([]*store.StoredPolicy, error) {
	seeds := policy.SeedPolicies()
	out := make([]*store.StoredPolicy, 0, len(seeds))
	for i, s := range seeds {
		seedVersion := s.SeedVersion
		out = append(out, &store.StoredPolicy{
			ID:          "abactest-seed-" + itoa(i+1),
			Name:        s.Name,
			Description: s.Description,
			Source:      "seed",
			DSLText:     s.DSLText,
			Enabled:     true,
			SeedVersion: &seedVersion,
		})
	}
	return out, nil
}

func (seedStore) Create(context.Context, *store.StoredPolicy) error { return errUnsupported }
func (seedStore) Get(context.Context, string) (*store.StoredPolicy, error) {
	return nil, errUnsupported
}

func (seedStore) GetByID(context.Context, string) (*store.StoredPolicy, error) {
	return nil, errUnsupported
}
func (seedStore) Update(context.Context, *store.StoredPolicy) error { return errUnsupported }
func (seedStore) Delete(context.Context, string) error              { return errUnsupported }
func (seedStore) CreateBatch(context.Context, []*store.StoredPolicy) error {
	return errUnsupported
}

func (seedStore) DeleteBySource(context.Context, string, string) (int64, error) {
	return 0, errUnsupported
}

func (seedStore) ReplaceBySource(context.Context, string, string, []*store.StoredPolicy) error {
	return errUnsupported
}

func (seedStore) List(context.Context, store.ListOptions) ([]*store.StoredPolicy, error) {
	return nil, errUnsupported
}

// itoa avoids pulling strconv in for one call site's readability.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// discardAuditWriter satisfies [audit.Writer] without retaining anything.
type discardAuditWriter struct{}

func (discardAuditWriter) WriteSync(context.Context, audit.Event) error { return nil }
func (discardAuditWriter) WriteAsync(audit.Event) error                 { return nil }
func (discardAuditWriter) Close() error                                 { return nil }

// unusedSessionResolver errors on every call. No seed policy resolves a
// session, so reaching it means the request under test was shaped wrong.
type unusedSessionResolver struct{}

func (unusedSessionResolver) ResolveSession(context.Context, string) (string, error) {
	return "", oops.Code("ABACTEST_SESSION_UNSUPPORTED").
		Errorf("abactest engines resolve no sessions")
}

// --- Provider doubles ---
//
// Each double's Schema() declares EXACTLY the keys its real counterpart
// declares. A double declaring a key the real provider does not emit turns
// every downstream behavioural assertion into a test of the double.
// TestEachDoubleDeclaresExactlyItsRealCounterpartsSchemaKeys is the
// symmetric-difference gate on that.

// staticProvider is the shared shape: a fixed subject bag, a fixed resource
// bag, and a pinned schema.
type staticProvider struct {
	namespace string
	subject   map[string]any
	resource  map[string]any
	schema    *types.NamespaceSchema
}

func (p *staticProvider) Namespace() string { return p.namespace }

func (p *staticProvider) ResolveSubject(context.Context, string) (map[string]any, error) {
	return p.subject, nil
}

func (p *staticProvider) ResolveResource(context.Context, string) (map[string]any, error) {
	return p.resource, nil
}
func (p *staticProvider) Schema() *types.NamespaceSchema { return p.schema }

// ViewerSchemaKeys mirrors attribute.ViewerTierProvider.Schema().
var ViewerSchemaKeys = map[string]types.AttrType{
	"tier":          types.AttrTypeString,
	"player_id":     types.AttrTypeString,
	"has_player_id": types.AttrTypeBool,
	"roles":         types.AttrTypeStringList,
	"has_roles":     types.AttrTypeBool,
}

// PlayerSchemaKeys mirrors attribute.PlayerAttributeProvider.Schema().
var PlayerSchemaKeys = map[string]types.AttrType{
	"id":           types.AttrTypeString,
	"grants":       types.AttrTypeStringList,
	"is_guest":     types.AttrTypeBool,
	"has_is_guest": types.AttrTypeBool,
	"roles":        types.AttrTypeStringList,
	"has_roles":    types.AttrTypeBool,
}

// PropertySchemaKeys mirrors attribute.PropertyProvider.Schema().
var PropertySchemaKeys = map[string]types.AttrType{
	"id":                        types.AttrTypeString,
	"parent_type":               types.AttrTypeString,
	"parent_id":                 types.AttrTypeString,
	"name":                      types.AttrTypeString,
	"value":                     types.AttrTypeString,
	"has_value":                 types.AttrTypeBool,
	"owner":                     types.AttrTypeString,
	"has_owner":                 types.AttrTypeBool,
	"visibility":                types.AttrTypeString,
	"parent_location":           types.AttrTypeString,
	"has_parent_location":       types.AttrTypeBool,
	"visible_to":                types.AttrTypeStringList,
	"excluded_from":             types.AttrTypeStringList,
	"owner_player_id":           types.AttrTypeString,
	"has_owner_player_id":       types.AttrTypeBool,
	"visible_to_players":        types.AttrTypeStringList,
	"has_visible_to_players":    types.AttrTypeBool,
	"excluded_from_players":     types.AttrTypeStringList,
	"has_excluded_from_players": types.AttrTypeBool,
}

// Viewer describes a viewer subject as 01-SPEC §8.4.1 models it.
//
// PlayerID is OMITTED from the bag on the anonymous rung — never emitted as an
// empty-string sentinel — so every identity-bearing viewer twin is
// UNSATISFIABLE there rather than matching an empty peer. That is the
// omit-don't-sentinel guarantee (.claude/rules/abac-providers.md), and it is
// what the doubles must reproduce for a privacy assertion to mean anything.
type Viewer struct {
	Tier     string
	PlayerID string
	Roles    []string
}

// ViewerProvider returns a `viewer`-namespace double for the given subject.
func ViewerProvider(v Viewer) attribute.AttributeProvider {
	attrs := map[string]any{"tier": v.Tier}

	if v.PlayerID != "" {
		attrs["player_id"] = v.PlayerID
		attrs["has_player_id"] = true
	} else {
		attrs["has_player_id"] = false
	}

	if len(v.Roles) > 0 {
		roles := make([]any, len(v.Roles))
		for i, r := range v.Roles {
			roles[i] = r
		}
		attrs["roles"] = roles
		attrs["has_roles"] = true
	} else {
		attrs["has_roles"] = false
	}

	return &staticProvider{
		namespace: "viewer",
		subject:   attrs,
		schema:    &types.NamespaceSchema{Attributes: ViewerSchemaKeys},
	}
}

// Player describes a player subject.
type Player struct {
	ID      string
	Roles   []string
	Grants  []string
	IsGuest *bool
}

// PlayerProvider returns a `player`-namespace double for the given subject.
func PlayerProvider(p Player) attribute.AttributeProvider {
	attrs := map[string]any{}

	if p.ID != "" {
		attrs["id"] = p.ID
	}
	if len(p.Roles) > 0 {
		roles := make([]any, len(p.Roles))
		for i, r := range p.Roles {
			roles[i] = r
		}
		attrs["roles"] = roles
		attrs["has_roles"] = true
	} else {
		attrs["has_roles"] = false
	}
	if len(p.Grants) > 0 {
		grants := make([]any, len(p.Grants))
		for i, g := range p.Grants {
			grants[i] = g
		}
		attrs["grants"] = grants
	}
	if p.IsGuest != nil {
		attrs["is_guest"] = *p.IsGuest
		attrs["has_is_guest"] = true
	} else {
		attrs["has_is_guest"] = false
	}

	return &staticProvider{
		namespace: "player",
		subject:   attrs,
		schema:    &types.NamespaceSchema{Attributes: PlayerSchemaKeys},
	}
}

// PropertyFixture describes an entity_properties row in CHARACTER-KEYED terms
// only, exactly as the row itself carries it.
//
// There is DELIBERATELY no field for owner_player_id, visible_to_players or
// excluded_from_players. Those are DERIVED here, from OwnerCharacterID /
// VisibleTo / ExcludedFrom through CharacterOwners, in the same two directions
// the real provider uses (02-CONTEXT.md D-27). Exposing setters for them would
// let a privacy suite assert D-27 while never exercising it — the fixture would
// simply state whatever answer the test wanted.
//
// CharacterOwners maps character id → owning player id and MUST be the fixture's
// COMPLETE character universe: the permit-side ALL direction asks whether EVERY
// character of a player appears in a field, so an incomplete map silently turns
// an ALL test into an ANY test.
type PropertyFixture struct {
	ID               string
	Name             string
	ParentType       string
	ParentID         string
	Visibility       string
	ParentLocation   string
	Value            *string
	OwnerCharacterID string
	VisibleTo        []string
	ExcludedFrom     []string
	CharacterOwners  map[string]string
}

// PropertyProvider returns a `property`-namespace double for the given row,
// deriving the three player-keyed peers rather than accepting them.
func PropertyProvider(f PropertyFixture) attribute.AttributeProvider {
	attrs := map[string]any{
		"id":          f.ID,
		"parent_type": f.ParentType,
		"parent_id":   f.ParentID,
		"name":        f.Name,
		"visibility":  f.Visibility,
	}

	if f.Value != nil {
		attrs["value"] = *f.Value
		attrs["has_value"] = true
	} else {
		attrs["has_value"] = false
	}

	if f.OwnerCharacterID != "" {
		attrs["owner"] = f.OwnerCharacterID
		attrs["has_owner"] = true
	} else {
		attrs["has_owner"] = false
	}

	if f.ParentLocation != "" {
		attrs["parent_location"] = f.ParentLocation
		attrs["has_parent_location"] = true
	} else {
		attrs["has_parent_location"] = false
	}

	if len(f.VisibleTo) > 0 {
		attrs["visible_to"] = toAnySlice(f.VisibleTo)
	}
	if len(f.ExcludedFrom) > 0 {
		attrs["excluded_from"] = toAnySlice(f.ExcludedFrom)
	}

	DerivePlayerPeers(f, attrs)

	return &staticProvider{
		namespace: "property",
		resource:  attrs,
		schema:    &types.NamespaceSchema{Attributes: PropertySchemaKeys},
	}
}

// DerivePlayerPeers writes the three player-keyed peers into attrs, in
// D-27's two directions:
//
//   - owner_player_id and visible_to_players feed PERMITS → the ALL direction. A
//     player enters only when EVERY character of theirs appears in the field.
//   - excluded_from_players feeds a FORBID → the ANY direction. A player enters
//     when ANY character of theirs does, so an exclusion is never lost.
//
// Each direction is the one that CANNOT WIDEN ITS OWN POLICY'S EFFECT. Do not
// "simplify" the ALL branch into an ANY branch for symmetry: that one-line diff
// reads as cleanup and reintroduces the widening D-27 declined.
//
// This is an INDEPENDENT implementation of the real provider's derivation, not
// a call into it — TestTheDerivedPeersAgreeWithTheRealPropertyProvider is a
// genuine differential test only because the two bodies are separate.
//
// Exported so a consumer can assert against the same derivation without
// building a provider.
func DerivePlayerPeers(f PropertyFixture, attrs map[string]any) {
	scopes := ownerScopes(f)

	if len(scopes) == 0 {
		attrs["has_owner_player_id"] = false
		attrs["has_visible_to_players"] = false
		attrs["has_excluded_from_players"] = false
		return
	}

	if f.OwnerCharacterID != "" {
		owners := playersWhoseEveryCharacterIsIn(scopes, []string{f.OwnerCharacterID})
		if len(owners) == 1 {
			attrs["owner_player_id"] = owners[0]
			attrs["has_owner_player_id"] = true
		} else {
			attrs["has_owner_player_id"] = false
		}
	} else {
		attrs["has_owner_player_id"] = false
	}

	emitPlayerList(attrs, "visible_to_players",
		playersWhoseEveryCharacterIsIn(scopes, f.VisibleTo))
	emitPlayerList(attrs, "excluded_from_players",
		playersWithAnyCharacterIn(scopes, f.ExcludedFrom))
}

// ownerScopes builds player id → that player's COMPLETE character set, for
// every player owning at least one character the ROW references. It mirrors
// postgres.CharacterOwnerResolver.ResolveOwnerScopes.
func ownerScopes(f PropertyFixture) map[string][]string {
	referenced := make(map[string]struct{})
	if f.OwnerCharacterID != "" {
		referenced[f.OwnerCharacterID] = struct{}{}
	}
	for _, c := range f.VisibleTo {
		referenced[c] = struct{}{}
	}
	for _, c := range f.ExcludedFrom {
		referenced[c] = struct{}{}
	}

	owningPlayers := make(map[string]struct{})
	for charID := range referenced {
		if playerID, ok := f.CharacterOwners[charID]; ok && playerID != "" {
			owningPlayers[playerID] = struct{}{}
		}
	}

	scopes := make(map[string][]string, len(owningPlayers))
	for charID, playerID := range f.CharacterOwners {
		if _, wanted := owningPlayers[playerID]; !wanted {
			continue
		}
		scopes[playerID] = append(scopes[playerID], charID)
	}
	for playerID := range scopes {
		sort.Strings(scopes[playerID])
	}
	return scopes
}

func playersWhoseEveryCharacterIsIn(scopes map[string][]string, field []string) []string {
	if len(field) == 0 {
		return nil
	}
	member := make(map[string]struct{}, len(field))
	for _, id := range field {
		member[id] = struct{}{}
	}

	out := []string{}
	for playerID, chars := range scopes {
		if len(chars) == 0 {
			// "Every character of this player is a member" is vacuously true
			// for an empty set, and a vacuous PERMIT is the wrong way for this
			// branch to fail.
			continue
		}
		all := true
		for _, c := range chars {
			if _, ok := member[c]; !ok {
				all = false
				break
			}
		}
		if all {
			out = append(out, playerID)
		}
	}
	return out
}

func playersWithAnyCharacterIn(scopes map[string][]string, field []string) []string {
	if len(field) == 0 {
		return nil
	}
	member := make(map[string]struct{}, len(field))
	for _, id := range field {
		member[id] = struct{}{}
	}

	out := []string{}
	for playerID, chars := range scopes {
		for _, c := range chars {
			if _, ok := member[c]; ok {
				out = append(out, playerID)
				break
			}
		}
	}
	return out
}

// emitPlayerList emits key + has_key, OMITTING key entirely when no player
// qualifies. An empty list is the list-flavored empty-string sentinel: a
// RESOLVED value a containsAny-shaped condition would evaluate against.
func emitPlayerList(attrs map[string]any, key string, players []string) {
	if len(players) == 0 {
		attrs["has_"+key] = false
		return
	}
	sort.Strings(players)
	attrs[key] = toAnySlice(players)
	attrs["has_"+key] = true
}

func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}
