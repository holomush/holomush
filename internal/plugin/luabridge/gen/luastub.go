// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// stubField is one field of a message class in the stub.
type stubField struct {
	Name     string // proto field name (snake_case, as Lua sees it)
	Type     string // LuaLS type string
	Optional bool   // emitted as "name?"
	Doc      string // leading proto comment, if any
}

// stubMessage is one generated ---@class for a proto message.
type stubMessage struct {
	ClassName string
	Fields    []stubField
}

// collectStubMessages walks every request/response message of the collected
// services, transitively following message-typed fields, and returns one
// stubMessage per distinct message. Oneof variants are flattened to optional
// fields (spec §3).
func collectStubMessages(services []serviceData) ([]stubMessage, error) {
	descs, err := reachableMessages(services)
	if err != nil {
		return nil, err
	}
	return buildStubMessages(descs), nil
}

// reachableMessages returns every message reachable as a request, response, or
// transitively-nested message field of the collected services' unary methods,
// deduplicated by proto full name (see walkMessages). It gathers each unary
// method's request/response message as a walk root.
func reachableMessages(services []serviceData) ([]protoreflect.MessageDescriptor, error) {
	var roots []protoreflect.MessageDescriptor
	for _, sd := range services {
		desc, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(sd.ServiceName))
		if err != nil {
			return nil, fmt.Errorf("service %q not registered: %w", sd.ServiceName, err)
		}
		svc, ok := desc.(protoreflect.ServiceDescriptor)
		if !ok {
			return nil, fmt.Errorf("%q is not a service descriptor", sd.ServiceName)
		}
		methods := svc.Methods()
		for i := 0; i < methods.Len(); i++ {
			m := methods.Get(i)
			if m.IsStreamingClient() || m.IsStreamingServer() {
				continue
			}
			roots = append(roots, m.Input(), m.Output())
		}
	}
	return walkMessages(roots), nil
}

// walkMessages returns roots plus every transitively message-typed field,
// deduplicated by proto full name. Keying dedup on the full name (not the short
// @class name) is the core of holomush-t4tye: two distinct messages that share a
// short name are both retained rather than the second being silently dropped.
func walkMessages(roots []protoreflect.MessageDescriptor) []protoreflect.MessageDescriptor {
	seen := map[protoreflect.FullName]bool{}
	var out []protoreflect.MessageDescriptor

	var visit func(md protoreflect.MessageDescriptor)
	visit = func(md protoreflect.MessageDescriptor) {
		if seen[md.FullName()] {
			return
		}
		seen[md.FullName()] = true
		out = append(out, md)

		fs := md.Fields()
		for i := 0; i < fs.Len(); i++ {
			fd := fs.Get(i)
			if (fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind) && !fd.IsMap() {
				visit(fd.Message())
			}
			if fd.IsMap() {
				mv := fd.MapValue()
				if mv.Kind() == protoreflect.MessageKind {
					visit(mv.Message())
				}
			}
		}
	}

	for _, r := range roots {
		visit(r)
	}
	return out
}

// buildStubMessages renders one stubMessage per reachable descriptor, naming
// classes via a collision-aware classNamer so message-field references and
// @class declarations agree even across same-short-name messages. Output is
// sorted by class name for deterministic generation.
//
// Scope: only message-field references (rendered via luaType) are namer-aware.
// Service-method @param/@return references are rendered separately by the
// template from the hardcoded "holomush.msg." ClassPrefix + the short Go type
// name, so a disambiguated collider reachable as an RPC request/response would
// produce a dangling reference there. That path is latent today (no collider is
// RPC-reachable) and guarded by TestRenderedStubIsStructurallyValid; the fuller
// fix (threading the namer into the service-method render path) is tracked in
// holomush-lfy04.
func buildStubMessages(descs []protoreflect.MessageDescriptor) []stubMessage {
	namer := newClassNamer(descs)
	out := make([]stubMessage, 0, len(descs))
	for _, md := range descs {
		var fields []stubField
		fs := md.Fields()
		for i := 0; i < fs.Len(); i++ {
			fd := fs.Get(i)
			optional := fd.HasOptionalKeyword() || fd.ContainingOneof() != nil
			fields = append(fields, stubField{
				Name:     string(fd.Name()),
				Type:     luaType(fd, namer.className),
				Optional: optional,
			})
		}
		out = append(out, stubMessage{ClassName: namer.className(md), Fields: fields})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClassName < out[j].ClassName })
	return out
}

// classNamer assigns each reachable message a LuaLS @class name keyed on its
// proto full name. A message whose short name is unique across the reachable
// set keeps the canonical holomush.msg.<ShortName> convention; only messages
// whose short name collides with another reachable message are disambiguated by
// full name (holomush-t4tye). Resolving by full name guarantees a reference to
// either collider names that specific class, never the wrong one.
type classNamer struct {
	byFull map[protoreflect.FullName]string
}

// newClassNamer computes the class-name assignment for the reachable set,
// detecting short-name collisions in a first pass before naming.
func newClassNamer(descs []protoreflect.MessageDescriptor) *classNamer {
	shortCount := map[string]int{}
	for _, md := range descs {
		shortCount[string(md.Name())]++
	}
	byFull := make(map[protoreflect.FullName]string, len(descs))
	for _, md := range descs {
		if shortCount[string(md.Name())] > 1 {
			byFull[md.FullName()] = disambiguatedClassName(md)
		} else {
			byFull[md.FullName()] = luaClassName(md)
		}
	}
	return &classNamer{byFull: byFull}
}

// className returns the assigned class name for md, falling back to the
// canonical short form for any descriptor not in the precomputed set (defensive;
// every field-referenced message is reachable and thus precomputed).
func (c *classNamer) className(md protoreflect.MessageDescriptor) string {
	if n, ok := c.byFull[md.FullName()]; ok {
		return n
	}
	return luaClassName(md)
}

// luaStubTmpl renders the full ---@meta definition file. Capability namespaces whose
// token is a valid Lua identifier are emitted as bare globals; the runtime registers
// the remaining six tokens (e.g. world.query, command-registry) via L.SetGlobal under
// their literal string key, reachable in Lua only as _G["<token>"], so those are emitted
// in _G index form to stay valid Lua (a bare `world.query = {}` would assign a field of
// an undeclared `world` global, and `command-registry = {}` is a parse error). Messages
// render as @class; ambient globals grouped by Module. The service-method block captures
// the service token in $token before ranging over methods, because methodData carries no
// Token field — $ stays rooted at the top-level data, so $.ClassPrefix resolves the
// message-class prefix.
const luaStubTmpl = `---@meta holomush
-- Code generated by internal/plugin/luabridge/gen; DO NOT EDIT.
-- Regenerate with: go generate ./internal/plugin/luabridge/...
-- Editor setup: point lua-language-server's Lua.workspace.library at pkg/plugin/luastubs/

{{range .Messages}}
---@class {{.ClassName}}
{{- range .Fields}}
---@field {{.Name}}{{if .Optional}}?{{end}} {{.Type}}{{if .Doc}} {{.Doc}}{{end}}
{{- end}}
{{end}}
{{range .Services}}
---@class holomush.host.{{.Token}}
{{if isLuaIdent .Token}}{{.Token}} = {}{{else}}_G[{{printf "%q" .Token}}] = {}{{end}}
{{- $token := .Token}}
{{- range .Methods}}
---@param req {{$.ClassPrefix}}{{.RequestGoType}}
---@return {{$.ClassPrefix}}{{.ResponseGoType}}
{{if isLuaIdent $token}}function {{$token}}.{{.GoName}}(req) end{{else}}_G[{{printf "%q" $token}}].{{.GoName}} = function(req) end{{end}}
{{- end}}
{{end}}
{{range .AmbientModules}}
---@class {{.Module}}
{{.Module}} = {}
{{- range .Fns}}
---{{.Doc}}
{{- range .Params}}
---@param {{.Name}} {{.Type}}
{{- end}}
{{- range .Returns}}
---@return {{.}}
{{- end}}
function {{.Parent}}.{{.Name}}({{paramList .Params}}) end
{{- end}}
{{end}}`

// ambientModule groups ambientFns by their Module for templating.
type ambientModule struct {
	Module string
	Fns    []ambientFnTmpl
}

// ambientFnTmpl carries the parent-table identifier alongside the decl so the
// template can name the function on its module table.
type ambientFnTmpl struct {
	ambientFn
	Parent string // the table identifier the function is set on (== Module)
}

// luaIdentRE matches a valid Lua identifier: a letter or underscore followed by
// any run of letters, digits, or underscores. Tokens that fail this (e.g.
// world.query, command-registry) are not addressable as bare globals in Lua.
var luaIdentRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// isLuaIdent reports whether s is a syntactically valid Lua identifier, and thus
// safe to emit as a bare global table name in the stub.
func isLuaIdent(s string) bool {
	return luaIdentRE.MatchString(s)
}

func renderLuaStub(services []serviceData, messages []stubMessage, ambient []ambientFn) (string, error) {
	// Group ambient fns by module, preserving the decl-table order.
	order := []string{}
	byMod := map[string][]ambientFnTmpl{}
	for _, d := range ambient {
		if _, ok := byMod[d.Module]; !ok {
			order = append(order, d.Module)
		}
		byMod[d.Module] = append(byMod[d.Module], ambientFnTmpl{ambientFn: d, Parent: d.Module})
	}
	mods := make([]ambientModule, 0, len(order))
	for _, m := range order {
		mods = append(mods, ambientModule{Module: m, Fns: byMod[m]})
	}

	data := struct {
		ClassPrefix    string
		Messages       []stubMessage
		Services       []serviceData
		AmbientModules []ambientModule
	}{
		ClassPrefix:    "holomush.msg.",
		Messages:       messages,
		Services:       services,
		AmbientModules: mods,
	}

	tmpl := template.Must(template.New("luastub").Funcs(template.FuncMap{
		"paramList": func(ps []ambientParam) string {
			names := make([]string, 0, len(ps))
			for _, p := range ps {
				names = append(names, p.Name)
			}
			return strings.Join(names, ", ")
		},
		"isLuaIdent": isLuaIdent,
	}).Parse(luaStubTmpl))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering lua stub: %w", err)
	}
	return buf.String(), nil
}
