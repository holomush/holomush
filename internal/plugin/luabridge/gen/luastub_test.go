// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	// Imported for its side effect: registers the holomush.plugin.v1
	// FileDescriptors so this package's tests can resolve the cross-package
	// DecryptOwnAuditRows* collision pair (holomush-t4tye).
	_ "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// mustMessageDesc resolves a registered message descriptor by full name.
func mustMessageDesc(t *testing.T, fullName string) protoreflect.MessageDescriptor {
	t.Helper()
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(fullName))
	require.NoErrorf(t, err, "descriptor %s", fullName)
	md, ok := d.(protoreflect.MessageDescriptor)
	require.Truef(t, ok, "%s is not a message", fullName)
	return md
}

// TestBuildStubMessagesDisambiguatesCrossPackageShortNameCollision proves the
// holomush-t4tye fix: when two distinct full-name messages that share a short
// name are both reachable, neither is silently dropped (FullName-keyed dedup)
// and each gets a distinct, package-disambiguated @class name. Non-colliding
// messages keep the canonical holomush.msg.<ShortName> convention.
func TestBuildStubMessagesDisambiguatesCrossPackageShortNameCollision(t *testing.T) {
	// DecryptOwnAuditRowsRequest exists in BOTH packages; short names collide.
	hostReq := mustMessageDesc(t, "holomush.plugin.host.v1.DecryptOwnAuditRowsRequest")
	pluginReq := mustMessageDesc(t, "holomush.plugin.v1.DecryptOwnAuditRowsRequest")
	// EmitEventRequest has a unique short name across the reachable set.
	emit := mustMessageDesc(t, "holomush.plugin.host.v1.EmitEventRequest")

	// AuditRow is the message-typed field of both colliders (rows = repeated
	// holomush.plugin.v1.AuditRow); include it so field-type resolution is
	// exercised, not just @class naming.
	auditRow := mustMessageDesc(t, "holomush.plugin.v1.AuditRow")

	msgs := buildStubMessages([]protoreflect.MessageDescriptor{hostReq, pluginReq, emit, auditRow})

	byClass := map[string]*stubMessage{}
	for i := range msgs {
		byClass[msgs[i].ClassName] = &msgs[i]
	}

	// No silent drop: all four messages survive as distinct classes.
	require.Len(t, msgs, 4, "every distinct full-name message MUST be emitted")
	assert.Contains(t, byClass, "holomush.msg.plugin.host.v1.DecryptOwnAuditRowsRequest",
		"host.v1 collider class present")
	assert.Contains(t, byClass, "holomush.msg.plugin.v1.DecryptOwnAuditRowsRequest",
		"plugin.v1 collider class present")
	// Non-colliding messages keep the canonical short form.
	assert.Contains(t, byClass, "holomush.msg.EmitEventRequest",
		"non-colliding message keeps holomush.msg.<ShortName>")

	// Field-type resolution within a collider flows through the namer: the
	// host.v1 collider's `rows` field references AuditRow (unique short name),
	// so it resolves to the canonical class — proving the field path is
	// namer-routed, not hardcoded.
	host := byClass["holomush.msg.plugin.host.v1.DecryptOwnAuditRowsRequest"]
	require.NotNil(t, host)
	require.Len(t, host.Fields, 1)
	assert.Equal(t, "holomush.msg.AuditRow[]", host.Fields[0].Type,
		"message-typed field within a collider resolves via the classNamer")
}

// TestWalkMessagesDedupsByFullNameNotShortName pins the core holomush-t4tye
// fix at the walk layer: when two distinct full-name messages that share a
// short name are both walk roots, both survive. Keying dedup on the short
// @class name (the original bug) would silently drop the second root.
func TestWalkMessagesDedupsByFullNameNotShortName(t *testing.T) {
	hostReq := mustMessageDesc(t, "holomush.plugin.host.v1.DecryptOwnAuditRowsRequest")
	pluginReq := mustMessageDesc(t, "holomush.plugin.v1.DecryptOwnAuditRowsRequest")

	got := walkMessages([]protoreflect.MessageDescriptor{hostReq, pluginReq})

	fulls := map[protoreflect.FullName]bool{}
	for _, md := range got {
		fulls[md.FullName()] = true
	}
	assert.True(t, fulls["holomush.plugin.host.v1.DecryptOwnAuditRowsRequest"],
		"host.v1 root retained")
	assert.True(t, fulls["holomush.plugin.v1.DecryptOwnAuditRowsRequest"],
		"plugin.v1 root retained (short-name dedup would have dropped it)")
}

// TestClassNamerFallsBackToCanonicalForUnknownDescriptor covers className's
// defensive fallback: a descriptor not in the precomputed set resolves to the
// canonical holomush.msg.<ShortName> form rather than panicking.
func TestClassNamerFallsBackToCanonicalForUnknownDescriptor(t *testing.T) {
	emit := mustMessageDesc(t, "holomush.plugin.host.v1.EmitEventRequest")
	unknown := mustMessageDesc(t, "holomush.plugin.host.v1.DecryptOwnAuditRowsRequest")

	namer := newClassNamer([]protoreflect.MessageDescriptor{emit})

	assert.Equal(t, "holomush.msg.DecryptOwnAuditRowsRequest", namer.className(unknown),
		"unknown descriptor falls back to canonical short form")
}

func TestCollectStubMessagesIncludesRequestAndResponse(t *testing.T) {
	services, err := collectServices()
	require.NoError(t, err)

	msgs, err := collectStubMessages(services)
	require.NoError(t, err)

	names := map[string]bool{}
	for _, m := range msgs {
		names[m.ClassName] = true
	}
	// EmitService.EmitEvent has request EmitEventRequest + response EmitEventResponse.
	assert.True(t, names["holomush.msg.EmitEventRequest"], "request message class present")
	assert.True(t, names["holomush.msg.EmitEventResponse"], "response message class present")
}

func TestRenderLuaStubProducesMetaAndNamespaces(t *testing.T) {
	services, err := collectServices()
	require.NoError(t, err)
	msgs, err := collectStubMessages(services)
	require.NoError(t, err)

	out, err := renderLuaStub(services, msgs, ambientDecls)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(out, "---@meta"), "stub MUST begin with ---@meta")
	assert.Contains(t, out, "---@class holomush.host.emit")
	assert.Contains(t, out, "function emit.EmitEvent(req)")
	assert.Contains(t, out, "---@class holomush.msg.EmitEventRequest")
	assert.Contains(t, out, "---@class holomush.config")
	assert.Contains(t, out, "function holo.fmt.bold(text)")

	// Non-identifier capability tokens (world.query, command-registry, ...) are
	// registered by the runtime via L.SetGlobal under their literal string key, so
	// the stub MUST emit them in _G index form to be valid Lua.
	assert.Contains(t, out, `_G["world.query"] = {}`)
	assert.Contains(t, out, `_G["world.query"].QueryObject = function(req) end`)
	assert.Contains(t, out, `_G["command-registry"] = {}`)

	// The invalid bare forms (parse errors / wrong surface) MUST be gone.
	assert.NotContains(t, out, "\nworld.query = {}")
	assert.NotContains(t, out, "command-registry = {}")
}

func TestRenderedStubIsStructurallyValid(t *testing.T) {
	services, err := collectServices()
	require.NoError(t, err)
	msgs, err := collectStubMessages(services)
	require.NoError(t, err)
	out, err := renderLuaStub(services, msgs, ambientDecls)
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(out, "---@meta"))

	// Collect declared classes.
	declRe := regexp.MustCompile(`(?m)^---@class (\S+)`)
	declared := map[string]bool{}
	for _, m := range declRe.FindAllStringSubmatch(out, -1) {
		declared[m[1]] = true
	}

	// Every referenced holomush.msg.* class MUST be declared. The class token
	// may be dotted (package-disambiguated colliders, holomush-t4tye), so match
	// a run of identifier chars and dots — stops at the trailing [] of a list.
	refRe := regexp.MustCompile(`holomush\.msg\.[\w.]+`)
	for _, ref := range refRe.FindAllString(out, -1) {
		assert.Truef(t, declared[ref], "referenced class %q is not declared", ref)
	}

	// No @field / @param with an empty type (would be "---@field name" with no type token).
	assert.NotRegexp(t, regexp.MustCompile(`(?m)^---@(field|param)\s+\S+\s*$`), out,
		"every @field/@param MUST carry a type")
}
