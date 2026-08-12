// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The character-access ROUTING CENSUS (01-SPEC criterion 1, plan 04-08).
//
// # What it proves
//
// The guest gate and the server-side ownership resolution each exist in exactly
// ONE place — playerGate.resolveAndGate and playerGate.ownedCharacter, embedded
// bare on the facade so both promote onto it — and every OWNER-audience RPC, on
// both sides of its proxy pair, reaches them. An owner RPC added later that
// routes around the shared gate fails this census rather than shipping, and a
// census entry whose RPC no longer exists fails as a stale entry. Both halves of
// that guarantee need SET EQUALITY: a loop over the expected entries never visits
// a member it was not told about.
//
// # The universe is the OWNER audience, and that is a decision (D-73)
//
// Criterion 1's literal phrasing — every character-returning RPC — cannot hold.
// GetCharacterProfile and ListCharacterDirectory serve ANONYMOUS viewers by
// design, and resolveAndGate refuses every guest outright, so routing them
// through it would break the public profile and the public directory. The owner
// audience is exactly the set whose contract is "the caller must be a non-guest
// player who owns this character". The two public reads are gated by the
// reachability floor and the per-attribute floors and are asserted by their own
// tests; here they are the two members of the public-audience side of the
// partition below.
//
// # Why the ownership predicate accepts a second call shape, and why that is safe
//
// Plan 04-06 routes both mutations through ownedCharacterForMutation, which calls
// the embedded s.ownedCharacter and rewrites its NotFound onto the MUTATION row of
// the 04-02 error matrix. Their bodies therefore name the wrapper and never the
// base resolution, and a body predicate does not follow a call. A predicate
// knowing only the base name has two outcomes and both are bad: permanently RED
// against correct code, or green only by dropping this phase's two write RPCs —
// its highest-risk surface — out of criterion 1's proof.
//
// The accepted second shape is a one-member checked-in set, and it leaves the
// property intact on three independent counts:
//
//   - CLOSED, not open. A future handler routing through some new
//     ownedCharacterForX is a non-member and the census goes RED; adding that name
//     is a visible edit to this file.
//   - VERIFIED, not trusted. The integrity assertion below proves the one accepted
//     wrapper's OWN body still reaches s.ownedCharacter. Gutting the wrapper while
//     both mutations keep naming it goes RED there.
//   - ADDITIVE ON THE DERIVED SIDE ONLY. It adds an accepted call shape; it never
//     removes a member from an expected set. It therefore cannot make an ungated
//     handler pass — such a handler names neither and lands in the missing group
//     exactly as before.
//
// A general follow-any-call-one-hop relaxation would forfeit the property
// outright: every private helper would then satisfy the predicate, including one
// that reaches a repository directly.
//
// # The audience PARTITION, and why the three set comparisons are not enough
//
// Membership in each derived set is decided by what a body REFERENCES. So a new
// exported handler on the facade referencing neither gate name would sit in no
// derived set and in no expected set, the symmetric difference would be empty,
// and the census would be GREEN while a wholly ungated RPC shipped. The partition
// closes that: every exported method on the facade receiver is positively
// classified, the unary-RPC-shaped ones must equal owner-audience ∪
// public-audience, and the two literals must be disjoint.
//
// The classification FAILS CLOSED. A server-streaming signature, a value or
// unnamed receiver, any shape the classifier does not positively recognize — each
// fails by name rather than quietly leaving the universe. An unclassifiable
// method is a finding: a first streaming RPC on this facade must arrive together
// with a deliberate, reviewable edit to this file.
//
// The public-audience literal is not a way out of scrutiny, and cannot become
// one. Its membership is RECOMPUTED from the code: the derived public set is the
// unary-RPC-shaped universe members whose bodies reference resolveViewerIdentity
// on their own receiver and reference no gate name, and it is compared against
// the literal by set equality. Moving an owner name into the literal fails the
// guest-gate comparison it just left; parking a net-new ungated handler's name in
// it fails the derived-public comparison it never joined.
//
// # The partition's own limits, stated beside the claim
//
//   - IT COVERS THE FACADE HALF ONLY. internal/web's proxies hang off one
//     *Handler receiver shared by every domain in the product, so "every exported
//     method on the accepted receiver" is not the character surface there and
//     cannot be partitioned against a four-member literal. The web half stays a
//     set equality over its four named members. That costs nothing real: the
//     facade is where the gate lives, so an ungated proxy still lands on a gated
//     facade RPC. The partition is asserted exactly where bypassing it would
//     actually grant access.
//   - IT IS KEYED ON GO HANDLER NAMES. character_rpc_census_test.go is keyed on
//     fully-qualified proto method names. Neither derives from the other and
//     neither needs to; they are independent gates over the same surface and share
//     only setSymmetricDifference. A Go handler identifier is not derivable from a
//     descriptor, so the keys are not unifiable.
//   - THE GENERATED Unimplemented EMBED'S PROMOTED METHODS SIT OUTSIDE THE FuncDecl
//     WALK BY CONSTRUCTION. That residue is inert: an RPC served by the generated
//     stub returns codes.Unimplemented and grants nothing. Its proto-level presence
//     is what character_rpc_census_test.go audits. The embedded-field literal plus
//     the every-method-unexported assertion over playerGate close the other
//     promotion vector, so the only thing promotion can add to this surface is the
//     stub's grant-nothing refusal.
//
// # Mechanics
//
// The census parses internal/grpc and internal/web with go/ast and imports
// neither package, so a temporarily inconsistent handler body still PARSES and the
// census reports the membership failure it exists to report rather than a build
// error. It walks EVERY non-test .go file in each directory rather than one file,
// for the reason world_caller_census_test.go gives: relocating a handler into a
// sibling file must not carry it out of the universe.

// characterFacadeReceiverTypes is the accepted receiver-type set for the FACADE
// half — exactly one member.
//
// SceneAccessServer is deliberately absent, and consequentially so. It embeds the
// same gate, but it is a different audience surface carrying 24 s.resolveAndGate(
// and 21 s.ownedCharacter( call sites. Admitting it would derive roughly 28
// guest-gate members against the four-member literal and roughly 24 ownership
// members against the three-member literal — permanently RED. The dangerous repair
// is not the RED but the obvious fix for it: widening the expected sets with two
// dozen scene entries, which converts criterion 1's character-audience proof into a
// snapshot of the scene facade. If a derived set ever comes back carrying scene
// method names, this set is wrong — do not touch the expected literals.
func characterFacadeReceiverTypes() map[string]struct{} {
	return map[string]struct{}{"CharacterAccessServer": {}}
}

// characterWebReceiverTypes is the accepted receiver-type set for the WEB half —
// exactly one member. internal/web's proxies all hang off *Handler.
//
// facadeReceiverName takes a SET because it is a shared helper with two callers
// passing different single-member sets, not because either caller passes several.
func characterWebReceiverTypes() map[string]struct{} {
	return map[string]struct{}{"Handler": {}}
}

const (
	// guestGateSelector is the ONE guest gate (INV-SCENE-64), promoted onto the
	// facade from the embedded playerGate.
	guestGateSelector = "resolveAndGate"

	// ownershipSelector is the ONE server-side ownership resolution
	// (INV-SCENE-63), likewise promoted.
	ownershipSelector = "ownedCharacter"

	// publicPipelineSelector is the viewer-identity resolution the two
	// public-audience reads share (D-83). It YIELDS a guest rung rather than
	// refusing one, which is precisely why those two reads must not reach the
	// guest gate.
	publicPipelineSelector = "resolveViewerIdentity"

	// characterAccessClientSelector is the facade client field a web proxy holds.
	// Referencing it is what scopes the web half's universe to the character
	// surface, on a *Handler receiver shared with every other domain.
	characterAccessClientSelector = "characterAccess"

	// sessionTokenHeaderIdent is the package-level constant a proxy reads the
	// caller's session token from. It is a BARE identifier, which is why the web
	// half needs bodyReferencesIdent rather than the selector predicate.
	sessionTokenHeaderIdent = "headerInjectSessionToken"
)

// ownershipWrapperMethods is the closed set of call shapes the ownership
// predicate accepts BESIDES the shared resolution itself — one member.
//
// ownedCharacterForMutation is introduced by plan 04-06 and applies plan 04-02's
// error matrix to the MUTATION surface: it calls s.ownedCharacter and rewrites its
// NotFound into a PermissionDenied the owner READ surfaces deliberately do not
// take. Its integrity is ASSERTED below, not assumed.
func ownershipWrapperMethods() map[string]struct{} {
	return map[string]struct{}{"ownedCharacterForMutation": {}}
}

// characterGuestGateRPCs is the checked-in expected set for the guest gate: every
// OWNER-audience facade RPC must reach the one shared gate. Five members.
func characterGuestGateRPCs() map[string]struct{} {
	return map[string]struct{}{
		// 04-05, the two owner reads.
		"ListMyCharacters": {},
		"GetMyCharacter":   {},
		// 04-06, the two owner writes.
		"UpdateCharacterProfile":     {},
		"UpdateCharacterDescription": {},
		// 05-01, the default-character write. It writes a `players` column rather
		// than a `characters` row, which is why it carries no version guard — but
		// its CALLER is still an owner-audience non-guest, so it takes the same
		// guest gate as every sibling.
		"SetDefaultCharacter": {},
	}
}

// characterOwnershipRPCs is the checked-in expected set for the ownership
// resolution. Four members — this set is scoped to the owner-audience handlers
// that NAME A CHARACTER ID.
//
// ListMyCharacters is deliberately NOT a member: it names no character id and
// resolves the caller's own roster from the session, so there is nothing to check
// ownership of. Its absence is a decision, not an oversight. SetDefaultCharacter
// IS a member for the mirror-image reason: it names an existing character id, and
// a caller must not be able to point their default at someone else's row.
func characterOwnershipRPCs() map[string]struct{} {
	return map[string]struct{}{
		"GetMyCharacter":             {},
		"UpdateCharacterProfile":     {},
		"UpdateCharacterDescription": {},
		"SetDefaultCharacter":        {},
	}
}

// characterPublicAudienceRPCs is the closed two-member set of facade RPCs that
// serve ANONYMOUS viewers by design and therefore MUST NOT reach the guest gate
// (D-73).
//
// It sits on the classified side of the audience partition, and its membership is
// recomputed from the code by the derived-public comparison below: a member must
// have a unary-RPC shape, must reference publicPipelineSelector on its own
// receiver, and must reference no gate name. Adding a name here without that
// pipeline behind it turns the census RED rather than quiet.
func characterPublicAudienceRPCs() map[string]struct{} {
	return map[string]struct{}{
		// The public profile read: resolves a viewer rung, then the reachability
		// floor and the per-attribute floors.
		"GetCharacterProfile": {},
		// The public directory (04-07): one tier-floor decision on the directory
		// resource, then the per-row membership rule.
		"ListCharacterDirectory": {},
	}
}

// characterNonHandlerMethods is the checked-in set of EXPORTED facade methods that
// are deliberately not RPCs — the sibling facade's option-setter shape
// (SceneAccessServer.WithSceneDEKAdder at internal/grpc/sceneaccess_service.go:96,
// WithCharacterNameResolver at :103) is the precedent for what would live here.
//
// It ships EMPTY: the character facade declares no such method today. Every future
// member is additionally asserted NOT to carry the unary RPC shape, so this set
// classifies a method as a non-RPC and can never hide a handler from a gate.
func characterNonHandlerMethods() map[string]struct{} {
	return map[string]struct{}{}
}

// characterFacadeEmbeddedFields is the checked-in embedded-field list of the
// CharacterAccessServer struct declaration. The FuncDecl walk cannot see a method
// PROMOTED from an embedded field, so a new embed is the one way an exported
// method could join this surface invisibly. Pinning the list makes that a visible
// census edit.
//
//   - playerGate is in-repo, and every method declared on it is asserted
//     unexported below, so promotion from it places nothing exported here.
//   - UnimplementedCharacterAccessServiceServer is generated. Its promoted methods
//     ARE the proto-declared RPCs; one with no hand-written override is served by
//     the stub, returns codes.Unimplemented and grants nothing. Its descriptor-level
//     presence is audited by character_rpc_census_test.go.
func characterFacadeEmbeddedFields() map[string]struct{} {
	return map[string]struct{}{
		"playerGate": {},
		"UnimplementedCharacterAccessServiceServer": {},
	}
}

// characterWebProxyRPCs is the checked-in expected set for the WEB half: the
// owner-audience proxies, keyed by proxy name. Five members, one per owner-audience
// facade RPC (04-04, 04-06, 05-01).
func characterWebProxyRPCs() map[string]struct{} {
	return map[string]struct{}{
		"WebListMyCharacters":           {},
		"WebGetMyCharacter":             {},
		"WebUpdateCharacterProfile":     {},
		"WebUpdateCharacterDescription": {},
		"WebSetDefaultCharacter":        {},
	}
}

// sceneFacadeMethodNames are real SceneAccessServer RPC handler names. No derived
// set here may contain one: the accepted receiver-type sets are character-scoped,
// and this assertion fails loudly if that is ever widened.
func sceneFacadeMethodNames() []string {
	return []string{"ListScenesForViewer", "GetSceneForViewer", "ListMyScenes", "CreateScene", "EndScene"}
}

// censusMethod is one exported method as the routing census sees it.
type censusMethod struct {
	name string
	file string
	// recv is the receiver VARIABLE name, empty when the receiver is unnamed.
	recv string
	fn   *ast.FuncDecl
}

// parseGoDir parses every non-test .go file in a repository-relative directory.
func parseGoDir(t *testing.T, rel ...string) map[string]*ast.File {
	t.Helper()
	dir := filepath.Join(append([]string{findRepoRoot(t)}, rel...)...)

	entries, err := os.ReadDir(dir)
	require.NoErrorf(t, err, "read %s", filepath.Join(rel...))

	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		require.NoErrorf(t, parseErr, "parse %s", filepath.Join(append(rel, name)...))
		files[name] = file
	}
	return files
}

// exportedMethodsOn collects EVERY exported method whose receiver type — after
// unwrapping an optional pointer — is one of accepted, INCLUDING value receivers
// and receivers with no variable name.
//
// It deliberately does NOT use facadeReceiverName. That helper returns not-ok for
// exactly the shapes (value receiver, unnamed receiver) that must stay inside the
// universe so the classifier can fail on them by name; using it here would restore
// the silent escape the fail-closed universe exists to remove. Membership
// predicates that need a receiver name check censusMethod.recv themselves.
func exportedMethodsOn(files map[string]*ast.File, accepted map[string]struct{}) []censusMethod {
	var out []censusMethod
	for fileName, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 || !fn.Name.IsExported() {
				continue
			}
			recvField := fn.Recv.List[0]
			expr := recvField.Type
			if star, starOK := expr.(*ast.StarExpr); starOK {
				expr = star.X
			}
			ident, identOK := expr.(*ast.Ident)
			if !identOK {
				continue
			}
			if _, accept := accepted[ident.Name]; !accept {
				continue
			}
			recvName := ""
			if len(recvField.Names) > 0 {
				recvName = recvField.Names[0].Name
			}
			out = append(out, censusMethod{name: fn.Name.Name, file: fileName, recv: recvName, fn: fn})
		}
	}
	return out
}

// isUnaryRPCShape reports whether a declaration has the unary gRPC handler
// signature: exactly two parameters with context.Context first, and exactly two
// results with error second.
func isUnaryRPCShape(fn *ast.FuncDecl) bool {
	if fn == nil || fn.Type == nil {
		return false
	}
	params := flattenedParamTypes(fn.Type.Params)
	if len(params) != 2 || !isContextContext(params[0]) {
		return false
	}
	results := flattenedParamTypes(fn.Type.Results)
	return len(results) == 2 && isIdentNamed(results[1], "error")
}

// classifyFacadeMethod places one universe member into exactly one class. The
// default is CLOSED: a shape it does not positively recognize returns ok=false and
// its caller fails the census by name.
func classifyFacadeMethod(m censusMethod, nonHandlers map[string]struct{}) (class string, ok bool) {
	if isUnaryRPCShape(m.fn) {
		return "unary-rpc", true
	}
	if _, listed := nonHandlers[m.name]; listed {
		return "non-handler", true
	}
	return "", false
}

// bodyNamesMethod reports whether a body references name as the SELECTED member of
// any selector expression — `anything.name`.
//
// It is local to this census rather than a shared helper because it is the ONE
// predicate neither Task 1 helper covers and no other census needs:
// bodyReferencesSelector requires a specific receiver on the left, which is right
// for the facade half where the gate is promoted onto the handler's own receiver;
// bodyReferencesIdent skips a selector's Sel entirely. A web proxy calls its paired
// facade method on a CLIENT FIELD (h.characterAccess.GetMyCharacter), so neither
// fits.
func bodyNamesMethod(body *ast.BlockStmt, name string) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel != nil && sel.Sel.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

// referencesOwnership reports whether a method reaches the ONE shared ownership
// resolution: directly, or through a member of the closed wrapper set.
func referencesOwnership(m censusMethod) bool {
	if m.recv == "" {
		return false
	}
	if bodyReferencesSelector(m.fn.Body, m.recv, ownershipSelector) {
		return true
	}
	for wrapper := range ownershipWrapperMethods() {
		if bodyReferencesSelector(m.fn.Body, m.recv, wrapper) {
			return true
		}
	}
	return false
}

// referencesAnyGate reports whether a method references any gate name at all —
// the guest gate, the shared ownership resolution, or a checked-in wrapper.
func referencesAnyGate(m censusMethod) bool {
	if m.recv == "" {
		return false
	}
	if bodyReferencesSelector(m.fn.Body, m.recv, guestGateSelector) {
		return true
	}
	return referencesOwnership(m)
}

// characterFacadeUniverse is the fail-closed universe of the facade half, with its
// anti-vacuity guard applied.
func characterFacadeUniverse(t *testing.T) []censusMethod {
	t.Helper()
	universe := exportedMethodsOn(parseGoDir(t, "internal", "grpc"), characterFacadeReceiverTypes())
	require.NotEmpty(t, universe,
		"the character facade universe is EMPTY — no exported method on an accepted receiver type was found in internal/grpc. Either the facade moved out of that directory or its receiver type was renamed; every comparison below would pass vacuously")
	return universe
}

// characterWebProxyUniverse is the web half's universe: exported *Handler methods
// that reach the character facade client, so the census is scoped to the character
// surface on a receiver every domain in the product shares.
func characterWebProxyUniverse(t *testing.T) []censusMethod {
	t.Helper()
	all := exportedMethodsOn(parseGoDir(t, "internal", "web"), characterWebReceiverTypes())
	var scoped []censusMethod
	for _, m := range all {
		if m.recv == "" {
			continue
		}
		if bodyReferencesSelector(m.fn.Body, m.recv, characterAccessClientSelector) {
			scoped = append(scoped, m)
		}
	}
	require.NotEmpty(t, scoped,
		"the web character-proxy universe is EMPTY — no exported *Handler method references the character facade client field. Either the field was renamed or the proxies moved out of internal/web; the web comparison would pass vacuously")
	return scoped
}

// TestCharacterAccessRoutingCensusGuestGate asserts SET EQUALITY between the
// facade methods that reach the one shared guest gate and the checked-in
// owner-audience set.
func TestCharacterAccessRoutingCensusGuestGate(t *testing.T) {
	universe := characterFacadeUniverse(t)

	derived := map[string]struct{}{}
	for _, m := range universe {
		if m.recv != "" && bodyReferencesSelector(m.fn.Body, m.recv, guestGateSelector) {
			derived[m.name] = struct{}{}
		}
	}

	extra, missing, message := setSymmetricDifference(derived, characterGuestGateRPCs())
	require.Truef(t, len(extra) == 0 && len(missing) == 0,
		"the facade methods reaching the shared guest gate (s.%s) do not equal the checked-in owner-audience set. An EXTRA member is an RPC that acquired the gate without an entry here; a MISSING member is an owner-audience RPC that no longer reaches it, which is the erosion criterion 1 exists to catch. %s",
		guestGateSelector, message)

	// The two public-audience reads serve anonymous viewers by design (D-73) and
	// must not appear here. Driven by iterating the checked-in literal so those
	// names are written down exactly once in this file.
	for name := range characterPublicAudienceRPCs() {
		_, present := derived[name]
		assert.Falsef(t, present,
			"the public-audience RPC %s reaches the guest gate, which refuses every guest — that would break the public surface it exists to serve (D-73)", name)
		_, expected := characterGuestGateRPCs()[name]
		assert.Falsef(t, expected, "the public-audience RPC %s must not appear in the owner-audience expected set", name)
	}
}

// TestCharacterAccessRoutingCensusOwnership asserts the wrapper's integrity FIRST,
// then SET EQUALITY between the facade methods that reach the one shared ownership
// resolution and the checked-in set.
//
// The order is load-bearing: gutting the wrapper leaves both mutations still
// naming it, so the set comparison stays GREEN and only the integrity assertion
// moves. That asymmetry is the whole reason the integrity assertion exists.
func TestCharacterAccessRoutingCensusOwnership(t *testing.T) {
	universe := characterFacadeUniverse(t)
	facadeFiles := parseGoDir(t, "internal", "grpc")
	accepted := characterFacadeReceiverTypes()

	// --- wrapper integrity, before any comparison ---
	for wrapper := range ownershipWrapperMethods() {
		declarations := 0
		reaches := false
		for _, file := range facadeFiles {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Name.Name != wrapper {
					continue
				}
				recvName, recvOK := facadeReceiverName(fn, accepted)
				if !recvOK {
					continue
				}
				declarations++
				if bodyReferencesSelector(fn.Body, recvName, ownershipSelector) {
					reaches = true
				}
			}
		}
		require.Equalf(t, 1, declarations,
			"the accepted ownership indirection %q must be declared exactly once on an accepted receiver type across internal/grpc, found %d. Zero means the name went stale; two means the census cannot say which body it verified",
			wrapper, declarations)
		require.Truef(t, reaches,
			"the accepted ownership indirection %q no longer references s.%s in its OWN body. Both mutations still name the wrapper, so the set comparison below stays green — this assertion is the only thing standing between a gutted wrapper and a silently ungated write surface",
			wrapper, ownershipSelector)
	}

	// --- set equality ---
	derived := map[string]struct{}{}
	for _, m := range universe {
		if referencesOwnership(m) {
			derived[m.name] = struct{}{}
		}
	}

	extra, missing, message := setSymmetricDifference(derived, characterOwnershipRPCs())
	require.Truef(t, len(extra) == 0 && len(missing) == 0,
		"the facade methods reaching the shared ownership resolution (s.%s, or a checked-in wrapper) do not equal the checked-in set. %s",
		ownershipSelector, message)

	// This phase's two WRITE RPCs are inside criterion 1's proof, not outside it:
	// they reach the shared resolution through the verified wrapper.
	for _, mutation := range []string{"UpdateCharacterProfile", "UpdateCharacterDescription"} {
		_, present := derived[mutation]
		assert.Truef(t, present,
			"the mutation %s is not in the ownership set — the two write RPCs reach the shared resolution through the verified wrapper and MUST be inside criterion 1's set-equality proof", mutation)
	}

	// ListMyCharacters names no character id, so there is nothing to check
	// ownership of. Its absence is a decision.
	_, roster := derived["ListMyCharacters"]
	assert.False(t, roster,
		"ListMyCharacters resolves the caller's own roster from the session and names no character id; it must not reach the per-character ownership resolution")
}

// TestCharacterAccessRoutingCensusWebProxies asserts SET EQUALITY over the
// owner-audience web proxies.
//
// A proxy is a member when its body reads the session-token header as a bare
// identifier AND names its paired facade method. Both conjuncts matter: the first
// proves the token comes from the header rather than the request body, the second
// proves the proxy reaches the gated facade RPC rather than some other surface.
//
// The two public-audience proxies are outside the derived set for the same D-73
// reason their facade halves are outside the owner-audience literal.
func TestCharacterAccessRoutingCensusWebProxies(t *testing.T) {
	universe := characterWebProxyUniverse(t)
	public := characterPublicAudienceRPCs()

	derived := map[string]struct{}{}
	for _, m := range universe {
		paired := strings.TrimPrefix(m.name, "Web")
		if _, isPublic := public[paired]; isPublic {
			continue
		}
		if bodyReferencesIdent(m.fn.Body, sessionTokenHeaderIdent) && bodyNamesMethod(m.fn.Body, paired) {
			derived[m.name] = struct{}{}
		}
	}

	extra, missing, message := setSymmetricDifference(derived, characterWebProxyRPCs())
	require.Truef(t, len(extra) == 0 && len(missing) == 0,
		"the owner-audience web proxies that forward the session-token header AND name their paired facade method do not equal the checked-in set. A MISSING member stopped doing one of the two; an EXTRA member is a new owner-audience proxy with no entry here. %s",
		message)
}

// TestCharacterAccessRoutingCensusAudiencePartition is the assertion the three set
// comparisons are structurally blind to: every exported method on the facade
// receiver is positively classified, and the unary-RPC-shaped ones partition
// exactly into the two audience literals.
func TestCharacterAccessRoutingCensusAudiencePartition(t *testing.T) {
	universe := characterFacadeUniverse(t)
	nonHandlers := characterNonHandlerMethods()

	unary := map[string]struct{}{}
	for _, m := range universe {
		class, ok := classifyFacadeMethod(m, nonHandlers)
		require.Truef(t, ok,
			"the exported facade method %s (internal/grpc/%s) has a shape this census does not recognize — neither the unary RPC signature nor a checked-in non-handler. An unclassifiable method is a FINDING, never a quiet exclusion: a first streaming RPC on this facade must arrive with a deliberate edit to this census, and a value or unnamed receiver must be given a pointer receiver with a name",
			m.name, m.file)
		if class == "unary-rpc" {
			unary[m.name] = struct{}{}
		}
	}
	require.NotEmpty(t, unary,
		"no facade method carried the unary RPC shape — the classifier stopped matching and the partition below would compare two empty-ish sets")

	// Each checked-in non-handler must genuinely not be a handler, so that set can
	// never take a real RPC out of the partition.
	for _, m := range universe {
		if _, listed := nonHandlers[m.name]; !listed {
			continue
		}
		assert.Falsef(t, isUnaryRPCShape(m.fn),
			"%s is checked in as a non-handler but carries the unary RPC signature — that set classifies a method as a non-RPC; it may never remove a handler from the audience partition", m.name)
	}

	owner := characterGuestGateRPCs()
	public := characterPublicAudienceRPCs()

	// Disjointness: a name parked in BOTH literals would satisfy the union while
	// escaping the guest-gate comparison.
	for name := range owner {
		_, both := public[name]
		require.Falsef(t, both,
			"%s appears in BOTH audience literals; they must be disjoint or the union assertion can be satisfied by a name that escaped the guest-gate comparison", name)
	}

	expected := map[string]struct{}{}
	for name := range owner {
		expected[name] = struct{}{}
	}
	for name := range public {
		expected[name] = struct{}{}
	}

	extra, missing, message := setSymmetricDifference(unary, expected)
	require.Truef(t, len(extra) == 0 && len(missing) == 0,
		"the facade's unary-RPC-shaped exported methods do not equal owner-audience ∪ public-audience. An EXTRA member is an RPC carrying NO audience classification at all — the case the three gate comparisons cannot see, because it joins no derived set and no expected set and leaves their symmetric difference empty. Classify it: give it the shared gate and add it to the owner-audience set, or establish it serves anonymous viewers and add it to the public-audience set (whose membership is then recomputed from the code). %s",
		message)
}

// TestCharacterAccessRoutingCensusDerivedPublicAudience recomputes the
// public-audience literal's membership from the code, so the literal cannot become
// a place to park an unclassified handler.
func TestCharacterAccessRoutingCensusDerivedPublicAudience(t *testing.T) {
	universe := characterFacadeUniverse(t)

	derived := map[string]struct{}{}
	for _, m := range universe {
		if !isUnaryRPCShape(m.fn) || m.recv == "" {
			continue
		}
		if !bodyReferencesSelector(m.fn.Body, m.recv, publicPipelineSelector) {
			continue
		}
		if referencesAnyGate(m) {
			continue
		}
		derived[m.name] = struct{}{}
	}

	extra, missing, message := setSymmetricDifference(derived, characterPublicAudienceRPCs())
	require.Truef(t, len(extra) == 0 && len(missing) == 0,
		"the facade's gate-free, s.%s-resolving unary RPCs do not equal the checked-in public-audience set. A MISSING member is a name in that literal with no public read pipeline behind it — the one-line move a hurried author reaches for when the audience partition goes RED against a wholly ungated handler. An EXTRA member is a public read that was never classified. %s",
		publicPipelineSelector, message)
}

// TestCharacterAccessRoutingCensusPromotionGuard closes the two vectors the
// FuncDecl walk cannot see. A method promoted from an embedded field never enters
// the universe, so the embed list is pinned and the in-repo embed is proven to
// export nothing.
func TestCharacterAccessRoutingCensusPromotionGuard(t *testing.T) {
	files := parseGoDir(t, "internal", "grpc")

	embedded := map[string]struct{}{}
	structsSeen := 0
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok || spec.Name.Name != "CharacterAccessServer" {
				return true
			}
			st, ok := spec.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}
			structsSeen++
			for _, field := range st.Fields.List {
				if len(field.Names) != 0 {
					continue // a named field is not an embed and promotes nothing
				}
				expr := field.Type
				if star, starOK := expr.(*ast.StarExpr); starOK {
					expr = star.X
				}
				switch typed := expr.(type) {
				case *ast.Ident:
					embedded[typed.Name] = struct{}{}
				case *ast.SelectorExpr:
					embedded[typed.Sel.Name] = struct{}{}
				}
			}
			return true
		})
	}
	require.Equal(t, 1, structsSeen,
		"the CharacterAccessServer struct declaration was not found exactly once in internal/grpc — this guard would pass vacuously")

	extra, missing, message := setSymmetricDifference(embedded, characterFacadeEmbeddedFields())
	require.Truef(t, len(extra) == 0 && len(missing) == 0,
		"the facade's embedded-field list does not equal the checked-in one. A new embed can promote an exported method onto this surface that the declaration walk never sees, so adding one must be a deliberate edit to this census. %s",
		message)

	// The in-repo embed must export nothing, so promotion from it cannot place an
	// exported method on the facade surface invisibly.
	gateMethods := 0
	for fileName, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			expr := fn.Recv.List[0].Type
			if star, starOK := expr.(*ast.StarExpr); starOK {
				expr = star.X
			}
			if !isIdentNamed(expr, "playerGate") {
				continue
			}
			gateMethods++
			assert.Falsef(t, fn.Name.IsExported(),
				"playerGate.%s (internal/grpc/%s) is EXPORTED — it promotes onto every facade that embeds the gate, arriving on the character facade's exported surface without a declaration the census walk can see",
				fn.Name.Name, fileName)
		}
	}
	require.NotZero(t, gateMethods,
		"no methods were found on the embedded gate type — the receiver predicate stopped matching and this half of the guard would pass vacuously")
}

// TestCharacterAccessRoutingCensusClassifierFailsClosed drives the fail-closed
// default from FIXTURES rather than from production code: a method shape the
// classifier does not positively recognize must fail by name, not drop out of the
// universe.
func TestCharacterAccessRoutingCensusClassifierFailsClosed(t *testing.T) {
	const fixture = `package fixture

import "context"

// The recognized shape.
func (s *CharacterAccessServer) UnaryHandler(ctx context.Context, req *Request) (*Response, error) {
	return nil, nil
}

// A server-streaming handler: no context first, no error-second pair.
func (s *CharacterAccessServer) StreamHandler(req *Request, stream Stream) error {
	return nil
}

// An option-setter shape not carried by the checked-in non-handler set.
func (s *CharacterAccessServer) WithSomeDependency(d Dependency) {}
`

	file := parseMetaHelperFixture(t, fixture)
	universe := exportedMethodsOn(map[string]*ast.File{"fixture.go": file}, characterFacadeReceiverTypes())
	require.Len(t, universe, 3, "the fixture's three exported methods are all in the universe")

	byName := map[string]censusMethod{}
	for _, m := range universe {
		byName[m.name] = m
	}

	class, ok := classifyFacadeMethod(byName["UnaryHandler"], characterNonHandlerMethods())
	require.True(t, ok, "the unary RPC shape is positively classified")
	assert.Equal(t, "unary-rpc", class)

	_, ok = classifyFacadeMethod(byName["StreamHandler"], characterNonHandlerMethods())
	assert.False(t, ok,
		"a server-streaming signature MUST fail classification by name rather than slide past a filter written for unary shapes")

	_, ok = classifyFacadeMethod(byName["WithSomeDependency"], characterNonHandlerMethods())
	assert.False(t, ok,
		"an option-setter shape absent from the checked-in non-handler set MUST fail classification by name")

	// The same setter IS classifiable once it is checked in — which is the visible,
	// reviewable edit the fail-closed default forces.
	class, ok = classifyFacadeMethod(byName["WithSomeDependency"], map[string]struct{}{"WithSomeDependency": {}})
	require.True(t, ok)
	assert.Equal(t, "non-handler", class)

	// A value receiver and an unnamed receiver stay INSIDE the universe so they
	// fail downstream by name instead of vanishing from it.
	const receiverFixture = `package fixture

import "context"

func (s CharacterAccessServer) ValueReceiverHandler(ctx context.Context, req *Request) (*Response, error) {
	return nil, nil
}

func (*CharacterAccessServer) UnnamedReceiverHandler(ctx context.Context, req *Request) (*Response, error) {
	return nil, nil
}
`
	receivers := exportedMethodsOn(
		map[string]*ast.File{"fixture.go": parseMetaHelperFixture(t, receiverFixture)},
		characterFacadeReceiverTypes(),
	)
	require.Len(t, receivers, 2,
		"a value receiver and an unnamed receiver both stay in the fail-closed universe — dropping them is the silent escape this collector exists to remove")
	for _, m := range receivers {
		_, ok := classifyFacadeMethod(m, characterNonHandlerMethods())
		assert.Truef(t, ok, "%s carries the unary RPC shape and so enters the audience partition, where its absence from both literals fails by name", m.name)
	}
}

// TestCharacterAccessRoutingCensusIsCharacterScoped asserts no scene facade method
// name reaches any derived set. If the accepted receiver-type sets are ever
// widened, this fails loudly rather than being repaired by widening the expected
// literals.
func TestCharacterAccessRoutingCensusIsCharacterScoped(t *testing.T) {
	universe := characterFacadeUniverse(t)

	derived := map[string]struct{}{}
	for _, m := range universe {
		if m.recv != "" && bodyReferencesSelector(m.fn.Body, m.recv, guestGateSelector) {
			derived[m.name] = struct{}{}
		}
		if referencesOwnership(m) {
			derived[m.name] = struct{}{}
		}
	}
	for _, m := range characterWebProxyUniverse(t) {
		derived[m.name] = struct{}{}
	}
	require.NotEmpty(t, derived, "the union of the derived sets is empty — this scope assertion would pass vacuously")

	for _, scene := range sceneFacadeMethodNames() {
		_, present := derived[scene]
		assert.Falsef(t, present,
			"the scene facade method %s reached a derived set — the accepted receiver-type sets have been widened past the character surface. The derived sets then swell by roughly 45 members against the 4 / 3 / 4 literals, and the only cheap repair is to fill those literals with scene entries, which converts criterion 1's proof into a snapshot of a facade it was never about",
			scene)
	}
}
