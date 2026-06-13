// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"context"
	"strings"
	"sync"

	"github.com/samber/oops"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/access/policy/types"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// InterceptorDeps wires the host-capability interceptor. Engine and Auditor are
// reserved for the policy + scope half (Task 10); the static half (M2) consumes
// only PluginName and DeclaredAccess.
type InterceptorDeps struct {
	// Engine evaluates ABAC policy in the dynamic half (Task 10); unused here.
	Engine types.AccessPolicyEngine
	// Auditor records capability decisions in the dynamic half (Task 10); unused here.
	Auditor pluginauthz.Auditor
	// PluginName identifies the calling plugin for DeclaredAccess lookups.
	PluginName string
	// DeclaredAccess reports the access class the plugin declared for a
	// capability token ("" => undifferentiated), and whether it declared the
	// capability at all. A false second return is fail-closed denial.
	DeclaredAccess func(plugin, capToken string) (string, bool)
}

// DeclaredAccessFromManifest builds the InterceptorDeps.DeclaredAccess lookup
// from a plugin manifest's capability requires. Presence of a capability-kind
// entry => declared; the value is the declared access narrowing ("" when
// undifferentiated). Service-kind entries are ignored. A nil manifest declares
// nothing (fail-closed). This single constructor is used by BOTH the binary and
// Lua install sites so the trust gate is built identically across runtimes
// (plugin-runtime-symmetry, INV-PLUGIN-45/49).
func DeclaredAccessFromManifest(m *plugins.Manifest) func(plugin, capToken string) (string, bool) {
	declared := make(map[string]string)
	if m != nil {
		for _, d := range m.Requires {
			if d.Kind == plugins.DependencyCapability {
				declared[d.Name] = d.Access
			}
		}
	}
	return func(_, capToken string) (string, bool) {
		a, ok := declared[capToken]
		return a, ok
	}
}

// bareServiceToToken reverses plugins.CapabilityServiceNames (token->bare
// service) into bare-service->token. Built once; the source map is fixed at
// program start.
var (
	bareServiceToTokenOnce sync.Once
	bareServiceToToken     map[string]string
)

func reverseServiceMap() map[string]string {
	bareServiceToTokenOnce.Do(func() {
		bareServiceToToken = make(map[string]string, len(plugins.CapabilityServiceNames))
		for token, bareService := range plugins.CapabilityServiceNames {
			bareServiceToToken[bareService] = token
		}
	})
	return bareServiceToToken
}

// classifyHostMethod maps a gRPC FullMethod (e.g.
// "/holomush.plugin.host.v1.KVService/Set") to its capability token and bare
// method name. ok is false when the method is not a gated host.v1 capability
// service method, in which case the interceptor passes it through untouched.
func classifyHostMethod(fullMethod string) (capToken, method string, ok bool) {
	if !strings.HasPrefix(fullMethod, "/") {
		return "", "", false
	}
	servicePath, method, found := strings.Cut(fullMethod[1:], "/")
	if !found || servicePath == "" || method == "" {
		return "", "", false
	}
	bareService := servicePath
	if i := strings.LastIndex(servicePath, "."); i >= 0 {
		bareService = servicePath[i+1:]
	}
	capToken, ok = reverseServiceMap()[bareService]
	if !ok {
		return "", "", false
	}
	return capToken, method, true
}

// NewCapabilityInterceptor builds the host-capability unary interceptor. This is
// the static half (M2): it classifies the method, fails closed on an
// unclassified or undeclared capability, and denies when the plugin's declared
// access class does not cover the method's operation class. Policy and scope
// enforcement (Task 10) plug in at the marked passthrough.
func NewCapabilityInterceptor(d InterceptorDeps) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		capToken, method, ok := classifyHostMethod(info.FullMethod)
		if !ok {
			return h(ctx, req) // not a gated host.v1 method
		}
		md, ok := Descriptors[capToken].Methods[method]
		if !ok {
			// Fail closed: a host.v1 method with no descriptor entry is unclassifiable.
			return nil, oops.Code("UNCLASSIFIED_CAPABILITY_METHOD").
				With("method", info.FullMethod).
				Errorf("no descriptor entry for host method")
		}
		declAccess, declared := d.DeclaredAccess(d.PluginName, capToken)
		if !declared {
			return nil, oops.Code("CAPABILITY_NOT_DECLARED").
				With("capability", capToken).
				Errorf("plugin did not declare capability")
		}
		if declAccess == "read" && md.Class == ClassWrite {
			return nil, oops.Code("ACCESS_CLASS_DENIED").
				With("capability", capToken).
				With("method", method).
				Errorf("declared access: read does not cover write method")
		}
		// Policy + scope enforcement (Task 10) plugs in here; static half passes through.
		return h(ctx, req)
	}
}
