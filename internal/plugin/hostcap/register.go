// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"google.golang.org/grpc"

	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// CapabilitySet selects which host.v1 capability services a per-plugin server
// registers. Both runtimes share the single-source server bodies; the set is the
// only registration-level difference between them (INV-PLUGIN-49, spec §1).
type CapabilitySet int

const (
	// BinaryDefaultSet is the capability set the binary (goplugin) runtime
	// registers today: the 9 host.v1 services with a binary consumer. It
	// deliberately omits Session/Property/World — those have no binary consumer
	// (spec §1) and are registered only in the Lua set once their server bodies
	// land (Tasks 3–5).
	BinaryDefaultSet CapabilitySet = iota
	// LuaDefaultSet is the capability set the Lua runtime registers. It extends
	// BinaryDefaultSet with five Lua-only services: PropertyService,
	// SessionService, SessionAdminService, WorldQueryService, and
	// WorldMutationService. All five are fully enabled as of Task 1
	// (holomush-eykuh.4.1).
	LuaDefaultSet
)

// RegisterCapabilities registers the host.v1 capability servers for the given
// set onto srv. It is the single registration source for both runtimes
// (INV-PLUGIN-49); the only per-runtime difference is the set + the adapter baked
// into base. goplugin's newPluginHostServiceServer calls this with
// BinaryDefaultSet; the Lua per-plugin server (§1, Task 7) calls it with
// LuaDefaultSet.
//
// The LuaDefaultSet branch registers the five Lua-only services:
// PropertyService (Task 3), SessionService + SessionAdminService (Task 4),
// WorldQueryService (Task 5), and WorldMutationService (Task 1,
// holomush-eykuh.4.1). All five servers now exist; LuaDefaultSet is fully
// enabled.
func RegisterCapabilities(srv *grpc.Server, base hostCapabilityBase, set CapabilitySet) {
	hostv1.RegisterFocusServiceServer(srv, &focusServer{hostCapabilityBase: base})
	hostv1.RegisterEmitServiceServer(srv, &emitServer{hostCapabilityBase: base})
	hostv1.RegisterEvalServiceServer(srv, &evalServer{hostCapabilityBase: base})
	hostv1.RegisterSettingsServiceServer(srv, &settingsServer{hostCapabilityBase: base})
	hostv1.RegisterStreamHistoryServiceServer(srv, &streamHistoryServer{hostCapabilityBase: base})
	hostv1.RegisterStreamSubscriptionServiceServer(srv, &streamSubscriptionServer{hostCapabilityBase: base})
	hostv1.RegisterAuditServiceServer(srv, &auditServer{hostCapabilityBase: base})
	hostv1.RegisterCommandRegistryServiceServer(srv, &commandRegistryServer{hostCapabilityBase: base})
	hostv1.RegisterKVServiceServer(srv, &kvServer{hostCapabilityBase: base})

	if set == LuaDefaultSet {
		hostv1.RegisterPropertyServiceServer(srv, &propertyServer{hostCapabilityBase: base})
		hostv1.RegisterSessionServiceServer(srv, &sessionServer{hostCapabilityBase: base})
		hostv1.RegisterSessionAdminServiceServer(srv, &sessionAdminServer{hostCapabilityBase: base})
		hostv1.RegisterWorldQueryServiceServer(srv, &worldServer{hostCapabilityBase: base})
		hostv1.RegisterWorldMutationServiceServer(srv, &worldMutationServer{hostCapabilityBase: base})
	}
}

// The constructors below expose the per-capability servers as their host.v1
// service-interface types. They keep the server struct types unexported (the
// public construction surface is NewBase + RegisterCapabilities) while letting
// in-package-adjacent test harnesses — notably goplugin's behavior-test shim,
// which drives each RPC through the real handler against a concrete *Host — build
// and invoke a single capability server without standing up a full gRPC server.
// Each returns the narrow service interface so callers cannot reach into the
// struct.

// NewFocusServer builds the FocusService capability server bound to base.
func NewFocusServer(base hostCapabilityBase) hostv1.FocusServiceServer {
	return &focusServer{hostCapabilityBase: base}
}

// NewEmitServer builds the EmitService capability server bound to base.
func NewEmitServer(base hostCapabilityBase) hostv1.EmitServiceServer {
	return &emitServer{hostCapabilityBase: base}
}

// NewEvalServer builds the EvalService capability server bound to base.
func NewEvalServer(base hostCapabilityBase) hostv1.EvalServiceServer {
	return &evalServer{hostCapabilityBase: base}
}

// NewSettingsServer builds the SettingsService capability server bound to base.
func NewSettingsServer(base hostCapabilityBase) hostv1.SettingsServiceServer {
	return &settingsServer{hostCapabilityBase: base}
}

// NewStreamHistoryServer builds the StreamHistoryService capability server bound to base.
func NewStreamHistoryServer(base hostCapabilityBase) hostv1.StreamHistoryServiceServer {
	return &streamHistoryServer{hostCapabilityBase: base}
}

// NewStreamSubscriptionServer builds the StreamSubscriptionService capability server bound to base.
func NewStreamSubscriptionServer(base hostCapabilityBase) hostv1.StreamSubscriptionServiceServer {
	return &streamSubscriptionServer{hostCapabilityBase: base}
}

// NewAuditServer builds the AuditService capability server bound to base.
func NewAuditServer(base hostCapabilityBase) hostv1.AuditServiceServer {
	return &auditServer{hostCapabilityBase: base}
}

// NewCommandRegistryServer builds the CommandRegistryService capability server bound to base.
func NewCommandRegistryServer(base hostCapabilityBase) hostv1.CommandRegistryServiceServer {
	return &commandRegistryServer{hostCapabilityBase: base}
}
