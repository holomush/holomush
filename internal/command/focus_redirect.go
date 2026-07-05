// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

// FocusRedirectTable maps a top-level verb to, per focus-kind string, the
// command that verb redirects to when the connection has that focus. It is
// built by the plugin loader from manifest focus_redirects and injected via
// WithFocusRedirects. Keyed verb-first so the dispatcher can gate its focus
// read behind a cheap verb lookup — the vast majority of commands are not
// redirect candidates and never trigger a focus read.
type FocusRedirectTable map[string]map[string]string

// Redirects reports whether any redirect exists for verb (any focus kind).
// Used to gate the focus read before it happens.
func (t FocusRedirectTable) Redirects(verb string) bool {
	_, ok := t[verb]
	return ok
}

// Target returns the redirect target command for (verb, focusKind), if any.
func (t FocusRedirectTable) Target(verb, focusKind string) (string, bool) {
	byKind, ok := t[verb]
	if !ok {
		return "", false
	}
	target, ok := byKind[focusKind]
	return target, ok
}
