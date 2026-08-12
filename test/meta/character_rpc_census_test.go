// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	// Blank imports register the generated descriptors this census walks. Every
	// package declaring a service under api/proto is present, so a service moving
	// between files cannot carry its methods out of the universe.
	_ "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	_ "github.com/holomush/holomush/pkg/proto/holomush/channel/v1"
	_ "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
	_ "github.com/holomush/holomush/pkg/proto/holomush/content/v1"
	_ "github.com/holomush/holomush/pkg/proto/holomush/control/v1"
	_ "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	_ "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
	_ "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	_ "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
	_ "github.com/holomush/holomush/pkg/proto/holomush/sceneaccess/v1"
	_ "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
	_ "github.com/holomush/holomush/pkg/proto/holomush/world/v1"
)

// The CHARACTER-RETURNING RPC CENSUS (01-SPEC §2.6, §3).
//
// # What it proves
//
// The set of RPCs whose response carries character data equals the checked-in
// §3 read-surface inventory, compared by SET EQUALITY in both directions. A new
// character-returning RPC that ships without an inventory row makes this RED —
// §3.1 rule 3 says that is the intended behavior, not a friction. An inventory
// row whose RPC no longer exists is equally RED, so the covered surface cannot
// quietly shrink.
//
// §2.6 names this census the SOLE mandated enforcement gate for the
// audience-and-message-shape section. There is deliberately no companion lint
// banning character-shaped struct literals outside the projection package: a
// second gate over the same property costs a rule and a maintenance surface for
// coverage this already has, and later becomes the argument for relaxing the gate
// that works.
//
// # The comparison key
//
// The fully-qualified proto method name in package.Service.Method form, compared
// as an EXACT string.
//
//   - Never a substring or prefix match, and never a bare METHOD name. A substring
//     key silently collapses distinct members and the census stops counting one of
//     them. §2.6's worked example does not hold literally — "ListCharacters" is not
//     a substring of "ListAllCharacters" — but the trap it describes is live one
//     level down: no fully-qualified name in the inventory is a substring of
//     another, because the package-and-service prefixes differ, while METHOD names
//     collapse in seventeen places, GetCharacter into GetCharacterProfile among
//     them. TestCharacterReturningRPCCensusKeyIsAnExactString derives those
//     collapses from the inventory and pins that the exact fully-qualified key
//     keeps each member its own.
//   - Never a Go handler identifier. Handler names are not stable against
//     refactor, several are shared across services, and a Go name is not derivable
//     from a descriptor at all. characteraccess_routing_census_test.go is the
//     census keyed on Go handler names; the two are independent gates over the
//     same surface and share only setSymmetricDifference.
//
// # Derivation, and the one class it cannot reach
//
// The type-reachable half walks the registered service descriptors and asks
// whether a method's output message TRANSITIVELY contains one of a checked-in set
// of character-shaped message names. Both halves of §2.6's guarantee hold over
// that class.
//
// §3.2 records that some surfaces carry character identity as a bare display-name
// scalar or as rendered bytes, where no typed message exists for a predicate to
// find. A descriptor-graph predicate cannot reach them, so they are enumerated by
// name and UNIONED into the derived side, exactly as §3.2 specifies. The cost is
// stated there and restated here rather than discovered under a red test: for
// that class both sides share one source, so it is self-certifying and the "no
// more" half does not hold over it. §3.2 carries the standing review obligation
// that covers it — a new scalar-identity field arrives with its own inventory row
// in the same change.
//
// The enumeration ADDS members to the derived side. It never removes one from
// scrutiny, and its own membership is asserted exact, so dropping a member is a
// visible edit rather than a silent loosening.
//
// # The expected set
//
// §3.3's rows plus the Phase-4 rows §9 adds, minus the row §2.4 deletes,
// transcribed from those tables rather than re-derived. §3.4 fixes the rule; D-72
// fixes the obligation: a later phase adding a character-returning RPC adds its
// inventory row in the SAME change, which is why this census needs nothing that
// removes an RPC from its scope.
//
// §9's read and mutation tables also name administrative and lifecycle RPCs that
// later phases deliver — the Admin* surfaces, CreateCharacter's reshape, retire
// and un-retire. None is declared in the tree today, so none is a descriptor and
// none belongs in the expected set yet. Each arrives with its own row under D-72.

// characterShapedMessages is the checked-in set of §3.2 type-reachable
// character-shaped message full names. A response transitively containing one is a
// census member.
func characterShapedMessages() map[string]struct{} {
	return map[string]struct{}{
		// §3.2's pre-v0.13 rows.
		"holomush.world.v1.CharacterInfo":          {},
		"holomush.core.v1.CharacterSummary":        {},
		"holomush.core.v1.CharacterDirectoryEntry": {},
		"holomush.core.v1.PresenceEntry":           {},
		"holomush.web.v1.CharacterSummary":         {},
		"holomush.web.v1.WebPresenceEntry":         {},
		"holomush.plugin.host.v1.CharacterSummary": {},
		// The v0.13 replacements (§2.2, §2.4). AdminCharacter is not declared yet;
		// it arrives with the admin surface and its own inventory rows (D-72).
		"holomush.characteraccess.v1.PublicCharacter":        {},
		"holomush.characteraccess.v1.PublicCharacterSummary": {},
		"holomush.characteraccess.v1.OwnCharacter":           {},
	}
}

// characterNameReachableRPCs is the §3.2 name-reachable class: surfaces carrying
// character identity as a bare scalar or as rendered bytes, where no proto field
// names a character and no descriptor-graph predicate can reach them. One comment
// per member naming the §3.3 reason.
//
// Its membership is asserted EXACT below, so removing a member is a visible edit.
func characterNameReachableRPCs() map[string]struct{} {
	return map[string]struct{}{
		// §3.3: a bare `character_name` display scalar on the response.
		"holomush.core.v1.CoreService.SelectCharacter":  {},
		"holomush.core.v1.CoreService.CreateCharacter":  {},
		"holomush.web.v1.WebService.WebSelectCharacter": {},
		"holomush.web.v1.WebService.WebCreateCharacter": {},
		// §3.3: the plugin-facing twin of GetCharacter. Its "character-shaped
		// message returned" column is the inline field list
		// `id/player_id/name/description/location_id`, not a message name — the
		// response declares those as bare scalars
		// (api/proto/holomush/plugin/host/v1/world.proto:96-108), so no typed
		// message exists for the descriptor predicate to reach and only this
		// enumeration puts it in the census.
		"holomush.plugin.host.v1.WorldQueryService.QueryCharacter": {},
		// §3.3: historical event frames whose payload carries a denormalized
		// display name captured at emit time (§5).
		"holomush.core.v1.CoreService.QueryStreamHistory":  {},
		"holomush.web.v1.WebService.WebQueryStreamHistory": {},
		// §3.3's three public export surfaces: rendered bytes or a frozen
		// participant column, published to readers who may be unauthenticated.
		"holomush.web.v1.WebService.WebExportScene":                {},
		"holomush.web.v1.WebService.WebGetPublicSceneArchive":      {},
		"holomush.web.v1.WebService.WebDownloadPublicSceneArchive": {},
		// §3.3's fourth public export surface: the same frozen participant column
		// in bulk, one entry per published scene.
		"holomush.web.v1.WebService.WebListPublishedScenes": {},
	}
}

// characterReadSurfaceInventory is the §3 expected set: the fully-qualified proto
// method name of every RPC whose response carries character data.
//
// WebListAllCharacters is absent by construction — §2.4 deletes it and plan 04-07
// did, so a row for it here would be a stale entry the census reports as missing.
func characterReadSurfaceInventory() map[string]struct{} {
	inventory := map[string]struct{}{
		// --- §3.3, host-owned character projections ---
		"holomush.world.v1.WorldService.GetCharacter":                       {},
		"holomush.world.v1.WorldService.ListCharactersAtLocation":           {},
		"holomush.plugin.host.v1.WorldQueryService.QueryLocationCharacters": {},
		"holomush.core.v1.CoreService.AuthenticatePlayer":                   {},
		"holomush.core.v1.CoreService.CreatePlayer":                         {},
		"holomush.core.v1.CoreService.CreateGuest":                          {},
		"holomush.core.v1.CoreService.ListCharacters":                       {},
		"holomush.core.v1.CoreService.ListAllCharacters":                    {},
		"holomush.core.v1.CoreService.CheckPlayerSession":                   {},
		"holomush.core.v1.CoreService.ListFocusPresence":                    {},
		"holomush.web.v1.WebService.WebAuthenticatePlayer":                  {},
		"holomush.web.v1.WebService.WebCreatePlayer":                        {},
		"holomush.web.v1.WebService.WebCreateGuest":                         {},
		"holomush.web.v1.WebService.WebListCharacters":                      {},
		"holomush.web.v1.WebService.WebCheckSession":                        {},
		"holomush.web.v1.WebService.WebListFocusPresence":                   {},

		// --- §3.4 / §9.2, the v0.13 character-access read surface ---
		"holomush.characteraccess.v1.CharacterAccessService.GetCharacterProfile":    {},
		"holomush.characteraccess.v1.CharacterAccessService.ListCharacterDirectory": {},
		"holomush.characteraccess.v1.CharacterAccessService.ListMyCharacters":       {},
		"holomush.characteraccess.v1.CharacterAccessService.GetMyCharacter":         {},

		// --- §3.4 / §9.3, the v0.13 owner mutation surface (responses carry
		// OwnCharacter, so both are members) ---
		"holomush.characteraccess.v1.CharacterAccessService.UpdateCharacterProfile":     {},
		"holomush.characteraccess.v1.CharacterAccessService.UpdateCharacterDescription": {},
		// §3.4 / §9.3: the default-character write. Its response is
		// ListMyCharactersResponse-shaped (D-90) so the browser re-renders from
		// server truth, which makes it a character-returning RPC and therefore a
		// census member — the write targets a `players` column, but what it
		// ANSWERS with is the owner roster.
		"holomush.characteraccess.v1.CharacterAccessService.SetDefaultCharacter": {},

		// --- §2.4 / §9.2, the web halves of the pairs above. §9.2 records that a
		// proxy pair is a CENSUS PAIR: §3.3 already lists core-side and web-side
		// twins as separate members, and these follow that precedent ---
		"holomush.web.v1.WebService.WebGetCharacterProfile":        {},
		"holomush.web.v1.WebService.WebListCharacterDirectory":     {},
		"holomush.web.v1.WebService.WebListMyCharacters":           {},
		"holomush.web.v1.WebService.WebGetMyCharacter":             {},
		"holomush.web.v1.WebService.WebUpdateCharacterProfile":     {},
		"holomush.web.v1.WebService.WebUpdateCharacterDescription": {},
		"holomush.web.v1.WebService.WebSetDefaultCharacter":        {},
	}
	// The §3.2 name-reachable rows are inventory members too, and are written down
	// exactly once — in characterNameReachableRPCs, beside their justifying
	// reasons. Restating them here would create two lists that can disagree.
	for name := range characterNameReachableRPCs() {
		inventory[name] = struct{}{}
	}
	return inventory
}

// removedWebDirectoryRPC is the RPC §2.4 splits away and plan 04-07 deleted. Its
// absence is asserted at the DESCRIPTOR level, not merely in source: a retirement
// that left the method on a registered service would still be callable.
const removedWebDirectoryRPC = "holomush.web.v1.WebService.WebListAllCharacters"

// registeredMethods walks the global proto registry and returns every registered
// method keyed by its fully-qualified name, with the service count and method
// count the anti-vacuity guards check.
func registeredMethods(t *testing.T) (methods map[string]protoreflect.MethodDescriptor, services int) {
	t.Helper()
	methods = map[string]protoreflect.MethodDescriptor{}
	protoregistry.GlobalFiles.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		svcs := file.Services()
		for i := 0; i < svcs.Len(); i++ {
			svc := svcs.Get(i)
			services++
			ms := svc.Methods()
			for j := 0; j < ms.Len(); j++ {
				m := ms.Get(j)
				methods[string(m.FullName())] = m
			}
		}
		return true
	})
	return methods, services
}

// responseCarriesCharacterShape reports whether msg transitively contains a
// message from the checked-in character-shaped set. The visited set makes a cyclic
// schema terminate.
func responseCarriesCharacterShape(msg protoreflect.MessageDescriptor, shapes map[string]struct{}, visited map[string]struct{}) bool {
	if msg == nil {
		return false
	}
	full := string(msg.FullName())
	if _, ok := shapes[full]; ok {
		return true
	}
	if _, seen := visited[full]; seen {
		return false
	}
	visited[full] = struct{}{}

	fields := msg.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		if field.Kind() != protoreflect.MessageKind && field.Kind() != protoreflect.GroupKind {
			continue
		}
		if responseCarriesCharacterShape(field.Message(), shapes, visited) {
			return true
		}
	}
	return false
}

// TestCharacterReturningRPCCensusMatchesTheReadSurfaceInventory is the gate: the
// derived set equals the checked-in §3 inventory, in both directions.
func TestCharacterReturningRPCCensusMatchesTheReadSurfaceInventory(t *testing.T) {
	methods, services := registeredMethods(t)

	// --- anti-vacuity, before the comparison ---
	require.NotZero(t, services,
		"the proto registry reported ZERO services — the blank imports at the top of this file no longer register anything (a generated package moved or was renamed), and every comparison below would pass vacuously")
	require.NotEmpty(t, methods,
		"the registry walk found no methods — services are registered but carry none, so the derived set is empty by construction rather than by the predicate")
	shapes := characterShapedMessages()
	require.NotEmpty(t, shapes,
		"the character-shaped message set is empty, so the type-reachable predicate can never match and the census would reduce to its self-certifying name-reachable half")

	// Every checked-in character-shaped message must actually be registered.
	// A renamed or deleted message would otherwise silently stop matching.
	for name := range shapes {
		_, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(name))
		require.NoErrorf(t, err,
			"the checked-in character-shaped message %q is not registered — if it was renamed or removed, update this set in the same change, or the predicate silently stops reaching every response that carried it", name)
	}

	derived := map[string]struct{}{}
	for name, method := range methods {
		if responseCarriesCharacterShape(method.Output(), shapes, map[string]struct{}{}) {
			derived[name] = struct{}{}
		}
	}
	// §3.2's specified union: the name-reachable class has no descriptor-side
	// signal, so it is seeded from the same explicit list the expected side uses.
	for name := range characterNameReachableRPCs() {
		derived[name] = struct{}{}
	}

	extra, missing, message := setSymmetricDifference(derived, characterReadSurfaceInventory())
	require.Truef(t, len(extra) == 0 && len(missing) == 0,
		"the RPCs whose response carries character data do not equal the §3 read-surface inventory. An EXTRA member is a character-returning endpoint that shipped without an inventory row and therefore without an audience verdict or a projection — add the row (§3.1 rule 3); never relax the comparison and never widen the predicate. A MISSING member is an inventory row whose RPC no longer exists, which quietly shrinks what this census covers. %s",
		message)
}

// TestCharacterReturningRPCCensusNameReachableMembershipIsExact pins the
// name-reachable list in both directions against the inventory, so a member cannot
// be dropped from the class without the edit being visible.
func TestCharacterReturningRPCCensusNameReachableMembershipIsExact(t *testing.T) {
	nameReachable := characterNameReachableRPCs()
	require.NotEmpty(t, nameReachable,
		"the name-reachable enumeration is empty — §3.2 records that a whole class of surface is unreachable by a type predicate, so an empty list means that class stopped being covered at all")

	methods, _ := registeredMethods(t)
	inventory := characterReadSurfaceInventory()

	names := make([]string, 0, len(nameReachable))
	for name := range nameReachable {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		_, registered := methods[name]
		assert.Truef(t, registered,
			"the name-reachable member %q is not a registered proto method — the surface was renamed or removed, and this enumeration is the ONLY thing that puts it in the census, so a stale entry is invisible coverage", name)
		_, inInventory := inventory[name]
		assert.Truef(t, inInventory,
			"the name-reachable member %q is not in the §3 inventory — for this class both sides share one source, so a member present on one side only is a bookkeeping error rather than a finding", name)
	}
}

// TestCharacterReturningRPCCensusRemovedWebDirectoryRPCIsGone asserts plan 04-07's
// retirement is complete at the DESCRIPTOR level, not only in source.
func TestCharacterReturningRPCCensusRemovedWebDirectoryRPCIsGone(t *testing.T) {
	methods, services := registeredMethods(t)
	require.NotZero(t, services, "the registry walk found no services — this absence assertion would pass vacuously")

	_, registered := methods[removedWebDirectoryRPC]
	assert.Falsef(t, registered,
		"%s is still a registered proto method — §2.4 splits it into the public directory and the admin list, and plan 04-07 deleted it. A retirement that leaves the method on a registered service leaves it callable", removedWebDirectoryRPC)

	_, inInventory := characterReadSurfaceInventory()[removedWebDirectoryRPC]
	assert.Falsef(t, inInventory,
		"%s still has an inventory row — §3.3 marks that row as deleted by the same change that deletes the RPC", removedWebDirectoryRPC)
}

// TestCharacterReturningRPCCensusKeyIsAnExactString pins the comparison key
// against a pair of REAL inventory members whose method names stand in a strict
// substring relation. Both must be counted separately; a substring or prefix key
// collapses them and the census silently stops counting one.
//
// §2.6's own worked example — "…ListCharacters is a substring of
// …ListAllCharacters" — does not hold literally: "ListCharacters" is not a
// substring of "ListAllCharacters". The trap it describes is nonetheless real and
// LIVE in this tree, one level down. No fully-qualified name in the inventory is a
// substring of another, because the package-and-service prefixes differ; but 17
// pairs of METHOD names collapse, GetCharacter into GetCharacterProfile among
// them. That is exactly why §2.6 fixes the key at the FULLY-QUALIFIED name
// compared exactly, and why a Go handler identifier — which is method-name shaped
// — is forbidden as a key. The scan below proves the collapse is live rather than
// hypothetical, then asserts the exact key counts both members.
func TestCharacterReturningRPCCensusKeyIsAnExactString(t *testing.T) {
	inventory := characterReadSurfaceInventory()
	require.NotEmpty(t, inventory, "the inventory is empty — this fixture would pin nothing")

	// A concrete, named pair from the shipped tree: two distinct RPCs on two
	// distinct services whose METHOD names stand in a strict substring relation.
	const (
		shorter = "holomush.world.v1.WorldService.GetCharacter"
		longer  = "holomush.characteraccess.v1.CharacterAccessService.GetCharacterProfile"
	)
	methods, _ := registeredMethods(t)
	for _, name := range []string{shorter, longer} {
		_, inInventory := inventory[name]
		assert.Truef(t, inInventory, "%s is its own inventory member", name)
		_, registered := methods[name]
		assert.Truef(t, registered, "%s is its own registered method", name)
	}
	assert.NotEqual(t, shorter, longer, "the two members are distinct under the exact-string key")

	// The collapse a substring-shaped key would produce, demonstrated over the
	// inventory rather than asserted from memory. strings.Contains appears HERE
	// and nowhere on the comparison path: this scan exists to prove the forbidden
	// key would be wrong, which is the opposite of using it.
	collapsed := map[string]struct{}{}
	methodNames := map[string]struct{}{}
	for name := range inventory {
		idx := strings.LastIndex(name, ".")
		methodNames[name[idx+1:]] = struct{}{}
	}
	for a := range methodNames {
		for b := range methodNames {
			if a != b && strings.Contains(b, a) {
				collapsed[a+" folds into "+b] = struct{}{}
			}
		}
	}
	require.NotEmptyf(t, collapsed,
		"no method name in the inventory is a strict substring of another, so this fixture no longer demonstrates the collapse §2.6 forbids — re-derive the pair rather than deleting the assertion")

	assert.Containsf(t, collapsed, "GetCharacter folds into GetCharacterProfile",
		"the named pair must be one of the live collapses, or the two constants above have drifted from the inventory")

	// Quantified: a substring-shaped key would fold at least this many DISTINCT
	// member names into some other member, and every one of those is a row the
	// census would stop counting.
	folded := map[string]struct{}{}
	for pair := range collapsed {
		folded[strings.SplitN(pair, " folds into ", 2)[0]] = struct{}{}
	}
	assert.GreaterOrEqualf(t, len(folded), 2,
		"a substring-shaped key folds %d inventory method name(s) into another member. Each is a row that would silently stop being counted, which is why §2.6 fixes the key at the exact fully-qualified name",
		len(folded))

	// The exact fully-qualified key keeps every one of them distinct: the derived
	// side is a map keyed on it, so two members can never merge into one.
	for name := range inventory {
		method := name[strings.LastIndex(name, ".")+1:]
		if _, isFolded := folded[method]; !isFolded {
			continue
		}
		_, stillItsOwnMember := inventory[name]
		assert.Truef(t, stillItsOwnMember,
			"%s carries a method name a substring key would fold, yet it is its own member under the exact key — that separation IS the coverage", name)
	}
}
