// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package readstream

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/samber/oops"
)

// typeSpec describes a sensitive event context type understood by ResolveBounds.
type typeSpec struct {
	// Arity is the exact number of IDs required for this context type.
	Arity int
	// OrderInsensitiveIDs indicates that the IDs must be lex-sorted for
	// canonical deduplication (e.g. dm threads where A→B == B→A).
	OrderInsensitiveIDs bool
	// MatchID validates each ID string for this type. Returns true if valid.
	MatchID func(string) bool
}

// sensitiveTypes is the package-private registry of recognised context types
// (INV-CRYPTO-56/INV-CRYPTO-57). Hardcoded — no runtime registration.
var sensitiveTypes = map[string]typeSpec{
	"scene":     {Arity: 1, OrderInsensitiveIDs: false, MatchID: isULID},
	"location":  {Arity: 1, OrderInsensitiveIDs: false, MatchID: isULID},
	"character": {Arity: 1, OrderInsensitiveIDs: false, MatchID: isULID},
	"dm":        {Arity: 2, OrderInsensitiveIDs: true, MatchID: isULID},
}

// ulidRe matches a 26-character Crockford Base32 ULID (uppercase only, no O/I/L/U).
var ulidRe = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

func isULID(s string) bool {
	return ulidRe.MatchString(s)
}

// Request is the domestic input shape for ResolveBounds (F's wire-decoupled form).
// Zero Since/Until fields trigger defaulting.
type Request struct {
	Contexts      []ContextRef
	Since         time.Time
	Until         time.Time
	Justification string
}

// Resolved is the validated, defaulted, canonicalised output of ResolveBounds.
// All fields are guaranteed non-zero when error is nil.
type Resolved struct {
	Contexts      []ContextRef
	Since         time.Time
	Until         time.Time
	Justification string
}

// ResolvedFlags reports which bounds were defaulted by ResolveBounds.
type ResolvedFlags struct {
	SinceDefaulted bool
	UntilDefaulted bool
}

const (
	// futureBoundGrace is the slack allowed for until > now to account for
	// small clock skew between client and server.
	futureBoundGrace = 5 * time.Second
	// maxContexts is the maximum number of ContextRef entries per request.
	maxContexts = 64
	// maxJustificationBytes is the maximum UTF-8 byte length of Justification.
	maxJustificationBytes = 4096
)

// ResolveBounds validates and canonicalises a readstream Request.
//
// Validation order per spec §4.1:
//  1. Temporal: TIME_INVERTED, FUTURE_BOUND, WINDOW_TOO_LARGE
//  2. Context: TOO_MANY, TYPE_UNKNOWN, ARITY_MISMATCH, ID_MALFORMED
//  3. Justification: EMPTY, TOO_LONG
//
// Defaulting: zero Since → now-defaultWindow; zero Until → now.
// DM canonicalisation: IDs are lex-sorted for order-insensitive types.
// Deduplication: by (type, joined-ids) key after canonicalisation.
// MUST NOT mutate the input *Request.
func ResolveBounds(req *Request, now time.Time, defaultWindow, maxWindow time.Duration) (Resolved, ResolvedFlags, error) {
	if req == nil {
		return Resolved{}, ResolvedFlags{}, oops.
			Code("DENY_OPERATOR_READ_INVALID_REQUEST").
			Errorf("request must not be nil")
	}

	var flags ResolvedFlags

	// --- Defaulting (applied before temporal validation) ---
	since := req.Since
	until := req.Until

	if since.IsZero() {
		since = now.Add(-defaultWindow)
		flags.SinceDefaulted = true
	}
	if until.IsZero() {
		until = now
		flags.UntilDefaulted = true
	}

	// --- Temporal validation ---
	if !until.IsZero() && !since.IsZero() && !until.After(since) {
		return Resolved{}, ResolvedFlags{}, oops.
			Code("DENY_OPERATOR_READ_TIME_INVERTED").
			Errorf("since must be before until")
	}

	if until.After(now.Add(futureBoundGrace)) {
		return Resolved{}, ResolvedFlags{}, oops.
			Code("DENY_OPERATOR_READ_FUTURE_BOUND").
			Errorf("until %s is beyond allowed future grace (%s after now)", until, futureBoundGrace)
	}

	window := until.Sub(since)
	if window > maxWindow {
		return Resolved{}, ResolvedFlags{}, oops.
			Code("DENY_OPERATOR_READ_WINDOW_TOO_LARGE").
			With("window", window, "max_window", maxWindow).
			Errorf("requested window %s exceeds maximum %s", window, maxWindow)
	}

	// --- Context validation ---
	if len(req.Contexts) > maxContexts {
		return Resolved{}, ResolvedFlags{}, oops.
			Code("DENY_OPERATOR_READ_TOO_MANY_CONTEXTS").
			With("count", len(req.Contexts), "max", maxContexts).
			Errorf("too many contexts: %d > %d", len(req.Contexts), maxContexts)
	}

	// Validate each context entry and build canonical copies.
	type dedupeKey struct {
		typeName string
		ids      string // joined sorted IDs
	}
	seen := make(map[dedupeKey]struct{})
	resolved := make([]ContextRef, 0, len(req.Contexts))

	for i, c := range req.Contexts {
		spec, ok := sensitiveTypes[c.Type]
		if !ok {
			return Resolved{}, ResolvedFlags{}, oops.
				Code("DENY_OPERATOR_READ_TYPE_UNKNOWN").
				With("type", c.Type, "index", i).
				Errorf("unknown context type %q at index %d", c.Type, i)
		}

		if len(c.IDs) != spec.Arity {
			return Resolved{}, ResolvedFlags{}, oops.
				Code("DENY_OPERATOR_READ_ARITY_MISMATCH").
				With("type", c.Type, "want_arity", spec.Arity, "got_arity", len(c.IDs), "index", i).
				Errorf("context type %q requires %d ID(s); got %d at index %d", c.Type, spec.Arity, len(c.IDs), i)
		}

		for j, id := range c.IDs {
			if !spec.MatchID(id) {
				return Resolved{}, ResolvedFlags{}, oops.
					Code("DENY_OPERATOR_READ_ID_MALFORMED").
					With("type", c.Type, "id", id, "index", i, "id_index", j).
					Errorf("context type %q: ID[%d] %q is malformed at context index %d", c.Type, j, id, i)
			}
		}

		// Canonicalise: copy IDs, sort if orderInsensitive.
		canonIDs := make([]string, len(c.IDs))
		copy(canonIDs, c.IDs)
		if spec.OrderInsensitiveIDs {
			sort.Strings(canonIDs)
		}

		key := dedupeKey{typeName: c.Type, ids: strings.Join(canonIDs, "\x00")}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		resolved = append(resolved, ContextRef{Type: c.Type, IDs: canonIDs})
	}

	// --- Justification validation ---
	if strings.TrimSpace(req.Justification) == "" {
		return Resolved{}, ResolvedFlags{}, oops.
			Code("DENY_OPERATOR_READ_JUSTIFICATION_EMPTY").
			Errorf("justification must not be empty or whitespace-only")
	}
	if len(req.Justification) > maxJustificationBytes {
		return Resolved{}, ResolvedFlags{}, oops.
			Code("DENY_OPERATOR_READ_JUSTIFICATION_TOO_LONG").
			With("length", len(req.Justification), "max", maxJustificationBytes).
			Errorf("justification exceeds %d bytes", maxJustificationBytes)
	}

	out := Resolved{
		Contexts:      resolved,
		Since:         since,
		Until:         until,
		Justification: req.Justification,
	}
	return out, flags, nil
}
