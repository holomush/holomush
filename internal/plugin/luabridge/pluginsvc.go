// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package luabridge

import (
	"strings"

	"github.com/samber/oops"
	lua "github.com/yuin/gopher-lua"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// RegisterPluginService injects a Lua global table named after the provider
// service's lowercased short-name (e.g. service `Echo` -> global `echo`),
// exposing each unary method as a `namespace.Method{…}`-shaped function. This is
// the plugin→plugin analog of RegisterHostCaps: where host capabilities are
// codegen'd from imported host.v1 stubs, a provider's proto is third-party and
// not imported by the host, so the table is synthesized at load time from the
// provider's registered protoreflect.ServiceDescriptor and marshaled through
// dynamicpb.
//
// Each registered function marshals its single Lua table argument into a
// dynamicpb request for the method's input type, invokes the unary RPC over conn
// — a loopback whose server end is the same BrokerProxy the binary host uses to
// reach the provider (plugin-runtime symmetry: one byte-forwarder, both
// runtimes) — and marshals the dynamic response back into a Lua table. The
// invoke path is the standard gRPC `/<package>.<Service>/<Method>` form derived
// from desc.FullName() and the method name.
//
// Validation is fail-early (spec §2): the descriptor is walked and validated at
// registration (load) time, not deferred to first call. A nil descriptor or a
// service that exposes zero unary methods returns an error and sets no global.
//
// Streaming methods (client- or server-streaming) are out of scope — Lua
// consumes unary RPCs only. They are skipped (not registered); a service whose
// methods are ALL streaming has no unary surface and fails at build.
func RegisterPluginService(L *lua.LState, conn grpc.ClientConnInterface, desc protoreflect.ServiceDescriptor, pluginName string) error { //nolint:gocritic // L is the idiomatic gopher-lua name
	if desc == nil {
		return oops.Code("LUABRIDGE_PLUGIN_SVC_NIL_DESCRIPTOR").
			With("plugin", pluginName).
			Errorf("nil service descriptor")
	}

	fullName := string(desc.FullName())
	methods := desc.Methods()

	// Build the function set first (fail-early): if no unary method is found the
	// service has no Lua-consumable surface and we register nothing.
	type boundMethod struct {
		name string
		fn   *lua.LFunction
	}
	bound := make([]boundMethod, 0, methods.Len())
	for i := 0; i < methods.Len(); i++ {
		md := methods.Get(i)
		// Streaming is out of scope for Lua consumers — skip, do not register.
		if md.IsStreamingClient() || md.IsStreamingServer() {
			continue
		}
		bound = append(bound, boundMethod{
			name: string(md.Name()),
			fn:   L.NewFunction(newPluginMethodInvoker(conn, fullName, md)),
		})
	}

	if len(bound) == 0 {
		return oops.Code("LUABRIDGE_PLUGIN_SVC_NO_UNARY_METHODS").
			With("plugin", pluginName).
			With("service", fullName).
			Errorf("service %s exposes no unary methods", fullName)
	}

	namespace := strings.ToLower(string(desc.Name()))
	tbl := L.NewTable()
	for _, bm := range bound {
		L.SetField(tbl, bm.name, bm.fn)
	}
	L.SetGlobal(namespace, tbl)
	return nil
}

// newPluginMethodInvoker returns the Lua callback for a single unary method. The
// callback marshals arg 1 (a Lua table) into a dynamicpb request for md.Input(),
// invokes `/<service>/<method>` over conn with a dynamicpb response for
// md.Output(), and pushes the response table on success or (nil, "<status
// message>") on error (the established 2-return Lua convention; inner error text
// stays opaque past the host boundary, per pushBridgeError).
func newPluginMethodInvoker(conn grpc.ClientConnInterface, serviceFullName string, md protoreflect.MethodDescriptor) lua.LGFunction {
	invokePath := "/" + serviceFullName + "/" + string(md.Name())
	inputDesc := md.Input()
	outputDesc := md.Output()

	return func(L *lua.LState) int {
		req := dynamicpb.NewMessage(inputDesc)
		if err := LuaTableToProto(L.CheckTable(1), req); err != nil {
			return pushBridgeError(L, err)
		}
		resp := dynamicpb.NewMessage(outputDesc)
		if err := conn.Invoke(luaContext(L), invokePath, req, resp); err != nil {
			return pushBridgeError(L, err)
		}
		L.Push(ProtoToLuaTable(L, resp))
		L.Push(lua.LNil)
		return 2
	}
}
