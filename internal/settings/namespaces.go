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

// ValidateNamespace checks that key is dot-namespaced and begins with a
// registered top-level namespace.
func ValidateNamespace(key string) error {
	if key == "" {
		return fmt.Errorf("settings key must not be empty")
	}
	dot := strings.IndexByte(key, '.')
	if dot < 0 {
		return fmt.Errorf("settings key %q must be dot-namespaced (e.g. 'scenes.focus.replay_tail_default')", key)
	}
	ns := key[:dot]
	for _, registered := range RegisteredNamespaces {
		if ns == registered {
			return nil
		}
	}
	return fmt.Errorf("unknown namespace %q in key %q; registered: %v", ns, key, RegisteredNamespaces)
}
