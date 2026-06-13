// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"context"
	"strings"
	"sync"

	"github.com/samber/oops"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// InterceptorDeps wires the host-capability interceptor. The static half (M2)
// consumes PluginName and DeclaredAccess; the dynamic half (M1 policy + M3
// scope) additionally consumes Engine and Auditor.
type InterceptorDeps struct {
	// Engine evaluates the ABAC policy decision in the dynamic half (M1/M3)
	// via pluginauthz.EvaluateCapabilityAccess.
	Engine types.AccessPolicyEngine
	// Auditor records the single capability-access decision per scoped call.
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

// NewCapabilityInterceptor builds the host-capability unary interceptor.
//
// Static half (M2): it classifies the method, fails closed on an unclassified or
// undeclared capability, and denies when the plugin's declared access class does
// not cover the method's operation class.
//
// Dynamic half (M1 policy + M3 scope): for a scope-eligible method (a descriptor
// entry with non-empty Scopes), it requires a host-vouched dispatch context,
// fails closed when the scope-eligible method has no wired extractor
// (INV-PLUGIN-52), extracts the concrete scoped resource, builds the scope
// context from the host-vouched dispatch attributes (the acting character's
// resolved "location" is surfaced to the policy DSL as the action attribute
// "dispatch_location"), and runs the default-deny ABAC decision via
// pluginauthz.EvaluateCapabilityAccess keyed on the plugin:<name> subject
// (INV-PLUGIN-50). Non-scoped capability methods pass through after the static
// half: this task's policy evaluation is intentionally limited to scoped methods,
// because no default-permit operator seed exists for the read-only host
// capabilities and running default-deny ABAC on them would deny every
// undifferentiated capability call. The broader M1 operator-policy surface for
// non-scoped capabilities is layered on by a later task.
func NewCapabilityInterceptor(d InterceptorDeps) grpc.UnaryServerInterceptor {
	if d.DeclaredAccess == nil {
		// Misconfigured interceptor: without a declaration lookup every gated
		// call would dereference a nil func and panic. Fail closed — deny all
		// host.v1 capability calls (non-host.v1 methods still pass through, as
		// in the normal path). Both production install sites build this via
		// DeclaredAccessFromManifest, so this guards a misconfiguration only.
		return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
			if strings.HasPrefix(info.FullMethod, "/holomush.plugin.host.v1.") {
				return nil, oops.Code("CAPABILITY_DECLARATION_LOOKUP_MISSING").
					With("method", info.FullMethod).
					Errorf("capability interceptor misconfigured: DeclaredAccess is nil")
			}
			return h(ctx, req)
		}
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		capToken, method, ok := classifyHostMethod(info.FullMethod)
		if !ok {
			if strings.HasPrefix(info.FullMethod, "/holomush.plugin.host.v1.") {
				// A host.v1 method whose service is not in the capability map is
				// unclassifiable. Fail closed rather than forward ungated — an
				// unmapped host.v1 service must not bypass capability enforcement
				// (default-deny, INV-PLUGIN-50).
				return nil, oops.Code("UNCLASSIFIED_CAPABILITY_METHOD").
					With("method", info.FullMethod).
					Errorf("host.v1 method not mapped to a capability token")
			}
			return h(ctx, req) // not a host.v1 method — pass through untouched
		}
		md, ok := Descriptors[capToken].Methods[method]
		if !ok {
			// Fail closed: a host.v1 method with no descriptor entry is unclassifiable.
			return nil, oops.Code("UNCLASSIFIED_CAPABILITY_METHOD").
				With("method", info.FullMethod).
				Errorf("no descriptor entry for host method")
		}
		if declarationExemptCapabilities[capToken] {
			// Self-gated capability: skip the declaration + access-class checks;
			// its own mechanism (emit fence / dispatch subject) is the authority.
			return h(ctx, req)
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

		// Dynamic half (M1 policy + M3 scope). Only scope-eligible methods are
		// policy-evaluated in this task (see the doc comment for why).
		if len(md.Scopes) == 0 {
			return h(ctx, req)
		}

		dc, haveDispatch := pluginauthz.DispatchForHost(ctx)
		if !haveDispatch || dc.Subject == "" {
			// A scoped capability call with no host-vouched dispatch context has
			// no acting-character location to scope against: fail closed.
			return nil, oops.Code("SCOPE_NO_DISPATCH").
				With("capability", capToken).
				With("method", method).
				Errorf("scoped capability call without dispatch context")
		}
		if md.Extract == nil {
			// A scope-eligible method MUST resolve its resource through a wired
			// extractor; a missing extractor fails closed (INV-PLUGIN-52).
			return nil, oops.Code("SCOPE_NO_EXTRACTOR").
				With("capability", capToken).
				With("method", method).
				Errorf("scope-eligible method missing extractor")
		}
		resourceID, ok := md.Extract(req)
		if !ok || resourceID == "" {
			// No concrete scoped resource present on a scope-eligible call:
			// fail closed rather than forward unscoped (INV-PLUGIN-52).
			return nil, oops.Code("SCOPE_NO_RESOURCE").
				With("capability", capToken).
				With("method", method).
				Errorf("scope-eligible method has no scoped resource to evaluate")
		}
		resource := md.Resource + ":" + resourceID
		scopeAttrs := map[string]any{"dispatch_location": dc.Attributes["location"]}

		dec, err := pluginauthz.EvaluateCapabilityAccess(ctx, pluginauthz.CapabilityInput{
			Engine:     d.Engine,
			Auditor:    d.Auditor,
			PluginName: d.PluginName,
			Subject:    access.PluginSubject(d.PluginName),
			Action:     md.Action,
			Resource:   resource,
			Declared:   true, // the declaration gate above already passed
			Context:    scopeAttrs,
		})
		if err != nil {
			// Fail closed. EvaluateCapabilityAccess already returns a
			// context-wrapped oops error (engine/subject/resource); re-wrapping
			// would double-wrap and obscure its fail-closed code.
			return nil, err //nolint:wrapcheck // pluginauthz already wraps with oops context
		}
		if !dec.Allowed {
			return nil, oops.Code("SCOPE_DENIED").
				With("capability", capToken).
				With("method", method).
				With("resource", resource).
				Errorf("denied by policy/scope")
		}
		return h(ctx, req)
	}
}
