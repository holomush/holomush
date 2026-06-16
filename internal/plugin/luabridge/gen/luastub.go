// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"fmt"
	"sort"

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
	seen := map[string]bool{}
	var out []stubMessage

	var visit func(md protoreflect.MessageDescriptor)
	visit = func(md protoreflect.MessageDescriptor) {
		cn := luaClassName(md)
		if seen[cn] {
			return
		}
		seen[cn] = true

		var fields []stubField
		fs := md.Fields()
		for i := 0; i < fs.Len(); i++ {
			fd := fs.Get(i)
			optional := fd.HasOptionalKeyword() || fd.ContainingOneof() != nil
			fields = append(fields, stubField{
				Name:     string(fd.Name()),
				Type:     luaType(fd),
				Optional: optional,
			})
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
		out = append(out, stubMessage{ClassName: cn, Fields: fields})
	}

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
			visit(m.Input())
			visit(m.Output())
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ClassName < out[j].ClassName })
	return out, nil
}
