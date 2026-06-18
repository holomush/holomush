// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"context"
	"strings"
	"sync"

	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// denialGRPCCode is the single, uniform mapping from a host-capability
// interceptor denial code to the gRPC status it serializes as on the wire
// (holomush-yc05l). Without it, a bare oops error has no GRPCStatus method and
// grpc-go surfaces every denial as codes.Unknown — so a plugin SDK (esp. Lua)
// branching on gRPC status cannot tell a policy denial from an infrastructure
// failure. Two classes:
//
//   - codes.PermissionDenied — the call is refused on authorization grounds:
//     the plugin did not declare the capability, declared too narrow an access
//     class, the scoped call cannot be authorized (no dispatch context / no
//     scoped resource), or policy forbade it.
//   - codes.Internal — a host-side misconfiguration the plugin cannot act on:
//     an unmapped/undescribed host.v1 method, an empty plugin name, a nil
//     declaration lookup, or a scope-eligible method with no wired extractor
//     (INV-PLUGIN-52 — a host wiring defect, not a plugin permission failure).
var denialGRPCCode = map[string]codes.Code{
	"CAPABILITY_NOT_DECLARED":               codes.PermissionDenied,
	"ACCESS_CLASS_DENIED":                   codes.PermissionDenied,
	"SCOPE_NO_DISPATCH":                     codes.PermissionDenied,
	"SCOPE_NO_RESOURCE":                     codes.PermissionDenied,
	"SCOPE_DENIED":                          codes.PermissionDenied,
	"CAPABILITY_ACCESS_DENIED":              codes.PermissionDenied,
	"UNCLASSIFIED_CAPABILITY_METHOD":        codes.Internal,
	"CAPABILITY_PLUGIN_NAME_MISSING":        codes.Internal,
	"CAPABILITY_DECLARATION_LOOKUP_MISSING": codes.Internal,
	"SCOPE_NO_EXTRACTOR":                    codes.Internal,
}

// capDeny builds a host-capability denial that carries BOTH the structured oops
// code (preserved for errutil.AssertErrorCode + structured logging) and a gRPC
// status (the wire contract plugin SDKs branch on). grpc-go's status.FromError
// walks the oops Unwrap chain to find the wrapped status, so the wire code is
// denialGRPCCode[code] while oops.AsOops still reports `code`. kv are oops
// context pairs (key, value, …). An unmapped code fails safe to codes.Internal —
// a denial must never serialize as codes.OK; TestCapabilityDenialsCarryGRPCStatus
// pins every known code to its intended status.
//
// On the wire only the status code and the static msg surface: grpc-go's
// FromError sets the wire message to err.Error(), which for this shape is the
// wrapped status's "rpc error: code = … desc = <msg>" — the oops With(kv…)
// context stays out of Error() and is never leaked to the plugin (grpc-errors.md).
func capDeny(code, msg string, kv ...any) error {
	grpcCode, ok := denialGRPCCode[code]
	if !ok {
		grpcCode = codes.Internal
	}
	return oops.Code(code).With(kv...).Wrap(status.Error(grpcCode, msg))
}

// evalFailureToStatus stamps codes.Internal onto a pluginauthz capability-
// evaluation failure while preserving its original oops code (for
// errutil.AssertErrorCode + logging), so it serializes with a classifiable wire
// status instead of codes.Unknown. EvaluateCapabilityAccess returns only host-
// side failures (nil engine/subject/action, engine errors) — never a policy
// denial — so Internal is always the correct class. The wire message is the
// generic "internal error" (grpc-errors.md: Internal errors are not detailed to
// the client). A non-oops error (defensive) keeps a stable fallback code.
func evalFailureToStatus(err error) error {
	code := "CAPABILITY_EVALUATION_FAILED"
	if oe, ok := oops.AsOops(err); ok {
		if c, isStr := oe.Code().(string); isStr && c != "" {
			code = c
		}
	}
	return oops.Code(code).Wrap(status.Error(codes.Internal, "internal error"))
}

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
// Dynamic half (M1 policy + M3 scope): EVERY declared non-exempt capability is
// authorized by the default-deny ABAC decision via
// pluginauthz.EvaluateCapabilityAccess keyed on the plugin:<name> subject
// (INV-PLUGIN-50) — declaration is necessary but not sufficient, so an operator
// policy MAY forbid a declared capability. For a scope-eligible method (a
// descriptor entry with non-empty Scopes), it requires a host-vouched dispatch
// context, fails closed when the scope-eligible method has no wired extractor
// (INV-PLUGIN-52), extracts the concrete scoped resource, and builds the scope
// context from the host-vouched dispatch attributes (the acting character's
// resolved "location" is surfaced to the policy DSL as the action attribute
// "dispatch_location"). A non-scoped method is evaluated at the capability type
// level (resource "<type>:*"): the per-capability default-permit seeds
// (seed.go) match it unconditionally so undifferentiated calls succeed, while an
// operator forbid policy overrides to deny. Every served capability resource
// type therefore MUST carry a default-permit seed — absence fails the call
// closed (guarded by the seed-completeness meta-test).
func NewCapabilityInterceptor(d InterceptorDeps) grpc.UnaryServerInterceptor {
	if d.DeclaredAccess == nil {
		// Misconfigured interceptor: without a declaration lookup every gated
		// call would dereference a nil func and panic. Fail closed — deny all
		// host.v1 capability calls (non-host.v1 methods still pass through, as
		// in the normal path). Both production install sites build this via
		// DeclaredAccessFromManifest, so this guards a misconfiguration only.
		return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
			if strings.HasPrefix(info.FullMethod, "/holomush.plugin.host.v1.") {
				return nil, capDeny("CAPABILITY_DECLARATION_LOOKUP_MISSING",
					"capability interceptor misconfigured: DeclaredAccess is nil",
					"method", info.FullMethod)
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
				return nil, capDeny("UNCLASSIFIED_CAPABILITY_METHOD",
					"host.v1 method not mapped to a capability token",
					"method", info.FullMethod)
			}
			return h(ctx, req) // not a host.v1 method — pass through untouched
		}
		md, ok := Descriptors[capToken].Methods[method]
		if !ok {
			// Fail closed: a host.v1 method with no descriptor entry is unclassifiable.
			return nil, capDeny("UNCLASSIFIED_CAPABILITY_METHOD",
				"no descriptor entry for host method",
				"method", info.FullMethod)
		}
		if declarationExemptCapabilities[capToken] {
			// Self-gated capability: skip the declaration + access-class checks;
			// its own mechanism (emit fence / dispatch subject) is the authority.
			return h(ctx, req)
		}
		declAccess, declared := d.DeclaredAccess(d.PluginName, capToken)
		if !declared {
			return nil, capDeny("CAPABILITY_NOT_DECLARED",
				"plugin did not declare capability",
				"capability", capToken)
		}
		if declAccess == "read" && md.Class == ClassWrite {
			return nil, capDeny("ACCESS_CLASS_DENIED",
				"declared access: read does not cover write method",
				"capability", capToken, "method", method)
		}
		if d.PluginName == "" {
			// Defense-in-depth, grouped with the peer static guards: access.PluginSubject
			// (called below for both the scoped and non-scoped paths) panics on an empty
			// name — an empty subject would bypass access control. Both production install
			// sites source PluginName from the manifest (schema-required, non-empty), so
			// this guards a misconfiguration only. Checked here, BEFORE the scope-eligible
			// block, so an empty name fails closed with this specific code rather than
			// masquerading as SCOPE_NO_DISPATCH on a scope-eligible call.
			return nil, capDeny("CAPABILITY_PLUGIN_NAME_MISSING",
				"capability interceptor misconfigured: empty plugin name",
				"capability", capToken, "method", method)
		}

		// Dynamic half (M1 policy + M3 scope). Every declared non-exempt
		// capability is subject to the default-deny ABAC decision (INV-PLUGIN-50):
		// declaration is necessary but not sufficient, so an operator policy MAY
		// forbid a declared capability. Scope-eligible methods additionally
		// resolve a concrete scoped resource and surface the dispatch location for
		// own-location conditions (M3); non-scoped methods evaluate at the
		// capability type level (resource "<type>:*"), which the default-permit
		// seeds match unconditionally and operator forbids may override.
		scopeEligible := len(md.Scopes) > 0
		resource := md.Resource + ":*" // type-level capability check (no instance)
		var scopeAttrs map[string]any

		if scopeEligible {
			dc, haveDispatch := pluginauthz.DispatchForHost(ctx)
			if !haveDispatch || dc.Subject == "" {
				// A scoped capability call with no host-vouched dispatch context has
				// no acting-character location to scope against: fail closed.
				return nil, capDeny("SCOPE_NO_DISPATCH",
					"scoped capability call without dispatch context",
					"capability", capToken, "method", method)
			}
			if md.Extract == nil {
				// A scope-eligible method MUST resolve its resource through a wired
				// extractor; a missing extractor fails closed (INV-PLUGIN-52).
				return nil, capDeny("SCOPE_NO_EXTRACTOR",
					"scope-eligible method missing extractor",
					"capability", capToken, "method", method)
			}
			resourceID, ok := md.Extract(req)
			if !ok || resourceID == "" {
				// No concrete scoped resource present on a scope-eligible call:
				// fail closed rather than forward unscoped (INV-PLUGIN-52).
				return nil, capDeny("SCOPE_NO_RESOURCE",
					"scope-eligible method has no scoped resource to evaluate",
					"capability", capToken, "method", method)
			}
			resource = md.Resource + ":" + resourceID
			scopeAttrs = map[string]any{"dispatch_location": dc.Attributes["location"]}
		}

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
			// Fail closed. EvaluateCapabilityAccess returns only host-side
			// evaluation FAILURES (nil engine/subject/action, engine errors) —
			// never a policy denial (a deny surfaces via dec.Allowed below). The
			// interceptor is the sole caller and the outermost gRPC boundary, so
			// per grpc-errors.md it is the one layer that stamps the wire status:
			// evalFailureToStatus maps these to codes.Internal (they are all
			// infrastructure/misconfiguration) while preserving the original oops
			// code for AssertErrorCode + logging, so they do not serialize as
			// codes.Unknown (holomush-yc05l).
			return nil, evalFailureToStatus(err)
		}
		if !dec.Allowed {
			// Scope-eligible denials carry SCOPE_DENIED (instance/own-location
			// failure); non-scoped denials carry CAPABILITY_ACCESS_DENIED (operator
			// policy forbade a declared capability at the type level).
			code := "CAPABILITY_ACCESS_DENIED"
			if scopeEligible {
				code = "SCOPE_DENIED"
			}
			return nil, capDeny(code, "denied by policy",
				"capability", capToken, "method", method, "resource", resource)
		}
		return h(ctx, req)
	}
}
