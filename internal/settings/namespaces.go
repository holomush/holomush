// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings

import (
	"fmt"
	"strings"
)

// RegisteredNamespaces lists the known top-level namespaces. Keys not
// beginning with one of these (followed by '.') are rejected by
// SetString. This list grows additively; removing a namespace is a
// breaking change and requires migration of any stored keys.
var RegisteredNamespaces = []string{
	"core",
	"scenes",
	"channels",
	"auth",
}

// ReservedNamespace is the top-level segment owned by plugin partitions.
// Plugin-partitioned scopes prefix every key with "plugin/<name>/" (slash-
// delimited), so a host key whose first dot-delimited segment is "plugin" is
// rejected by ValidateNamespace to guarantee host writes can never collide with
// the plugin-partition keyspace. It is intentionally NOT a registered namespace.
const ReservedNamespace = "plugin"

// ValidateNamespace checks that key is dot-namespaced and begins with a
// registered top-level namespace. The reserved "plugin" segment is rejected
// even though it is not registered, so the error message names it explicitly.
func ValidateNamespace(key string) error {
	if key == "" {
		return fmt.Errorf("settings key must not be empty")
	}
	dot := strings.IndexByte(key, '.')
	if dot < 0 {
		return fmt.Errorf("settings key %q must be dot-namespaced (e.g. 'scenes.focus.replay_tail_default')", key)
	}
	ns := key[:dot]
	if ns == ReservedNamespace {
		return fmt.Errorf("namespace %q is reserved for plugin partitions and cannot be a host key (key %q)", ReservedNamespace, key)
	}
	for _, registered := range RegisteredNamespaces {
		if ns == registered {
			return nil
		}
	}
	return fmt.Errorf("unknown namespace %q in key %q; registered: %v", ns, key, RegisteredNamespaces)
}
