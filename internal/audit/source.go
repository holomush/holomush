// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

// EventSource identifies the kind of authorization subsystem that produced
// an audit event. It is a discriminator for operator queries, not a
// behavior switch — the audit logger's mode routing uses Effect, not Source.
//
// EventSource is a defined string type (not an alias) so function signatures
// remain self-documenting and the package can attach methods later if needed.
type EventSource string

// EventSource constants. These are the only values the engine, plugin
// dispatcher, and system paths use. Additional values are additive and
// MAY be introduced by adding a new constant without breaking existing
// consumers — nothing switches on this value.
const (
	// SourceEngine is stamped on events produced by the ABAC policy engine.
	SourceEngine EventSource = "engine"

	// SourcePlugin is stamped on events produced by plugin handler code
	// via the dispatcher's hint extraction path. The specific plugin name
	// lives in the Component field.
	SourcePlugin EventSource = "plugin"

	// SourceSystem is stamped on events produced by system-bypass paths
	// (operator overrides, reaper operations, bootstrap seeding).
	SourceSystem EventSource = "system"
)
