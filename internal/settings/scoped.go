// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings

import (
	"context"
	"encoding/json"
	"time"

	"github.com/samber/oops"
)

// scopedView is the plugin-partitioned implementation of Scoped. The host
// partition holds the principal's flat dot-keyed preferences (e.g.
// "scenes.focus.replay_tail_default") and is namespace-validated on reads,
// preserving the legacy bare-read behavior. Each plugin partition is an
// isolated map keyed by plugin name and is NOT namespace-validated, so
// plugin-private keys need no RegisteredNamespaces entry.
//
// commit persists Plugin/Host writes when non-nil (repo-backed views built by
// the player/character stores). It is nil for read-only views (the legacy
// reader path and the null character store), in which case writes update the
// in-memory maps only.
//
// dirty records which partitions THIS view mutated so a repo-backed commit
// serializes only changed partitions, never re-writing a clean-loaded plugin
// partition with its stale value (cross-plugin lost-update safety). It is nil
// for views without dirty tracking (read-only / test views), where it is
// simply unused.
type scopedView struct {
	host    map[string]json.RawMessage            // host-partition flat data
	plugins map[string]map[string]json.RawMessage // plugin name -> partition data
	dirty   *dirtyTracker                         // nil unless repo-backed + tracking
	commit  func(ctx context.Context) error       // nil for read-only views; non-nil persists
}

// dirtyTracker records the partitions a scopedView mutated through its
// Plugin(name)/Host() writables. A repo-backed commit reads it to serialize
// only the changed partitions, so a concurrent For() handle that loaded the
// same plugins but mutated different ones does not lose the other's update.
type dirtyTracker struct {
	plugins map[string]bool // plugin-partition names written through this view
	host    bool            // host partition written through this view
}

func newDirtyTracker() *dirtyTracker {
	return &dirtyTracker{plugins: map[string]bool{}}
}

// newScopedView constructs a read-only scopedView over the given host partition
// with an empty plugin partition set and a nil commit func: writes update the
// in-memory maps only. Repo-backed, persisting views are built via
// newScopedViewWithPlugins with a non-nil commit func.
func newScopedView(host map[string]json.RawMessage) *scopedView {
	return newScopedViewWithPlugins(host, nil, nil)
}

// newScopedViewWithPlugins constructs a scopedView seeded with both the host
// partition and pre-loaded plugin partitions (plugin name -> key -> raw value),
// plus the supplied commit func (may be nil). The repo-backed player store uses
// this to materialize Preferences.Plugins so previously-persisted plugin
// partitions are readable.
func newScopedViewWithPlugins(
	host map[string]json.RawMessage,
	plugins map[string]map[string]json.RawMessage,
	commit func(ctx context.Context) error,
) *scopedView {
	if host == nil {
		host = map[string]json.RawMessage{}
	}
	if plugins == nil {
		plugins = map[string]map[string]json.RawMessage{}
	}
	return &scopedView{
		host:    host,
		plugins: plugins,
		commit:  commit,
	}
}

// NewScopedForTest returns an in-memory Scoped over the given host partition
// with no commit func. Writes update the in-memory maps so same-instance
// round-trips work without persistence. Intended for tests only.
func NewScopedForTest(host map[string]json.RawMessage) Scoped {
	return newScopedView(host)
}

// newTrackedScopedView builds a repo-backed, dirty-tracking scopedView. The
// makeCommit factory receives the view's dirtyTracker so the commit serializes
// only the partitions this view mutated — the cross-plugin lost-update fix: a
// concurrent For() handle that loaded the same plugins but changed different
// ones will not have its writes clobbered by this view's stale loaded copies.
func newTrackedScopedView(
	host map[string]json.RawMessage,
	plugins map[string]map[string]json.RawMessage,
	makeCommit func(dirty *dirtyTracker) func(ctx context.Context) error,
) *scopedView {
	if host == nil {
		host = map[string]json.RawMessage{}
	}
	if plugins == nil {
		plugins = map[string]map[string]json.RawMessage{}
	}
	dirty := newDirtyTracker()
	v := &scopedView{host: host, plugins: plugins, dirty: dirty}
	v.commit = makeCommit(dirty)
	return v
}

// newFailClosedView returns a scopedView whose reads resolve to empty (honoring
// the Settings reads-never-error contract) but whose writes fail closed: every
// Plugin/Host write returns loadErr instead of silently mutating an in-memory
// map that will never be persisted. Repo-backed stores return this when the
// initial load fails, so a caller's write surfaces the failure rather than
// vanishing.
func newFailClosedView(loadErr error) *scopedView {
	v := newScopedViewWithPlugins(nil, nil, nil)
	v.commit = func(context.Context) error { return loadErr }
	return v
}

// hostReader wraps the host partition as a namespace-validated Settings.
func (v *scopedView) hostReader() *jsonMapSettings {
	return &jsonMapSettings{data: v.host, validateNamespace: true}
}

// Bare Settings reads delegate to the host partition, identical to the
// legacy namespace-validated behavior the resolution Chain depends on.

func (v *scopedView) StringN(ctx context.Context, key string) (string, bool) {
	return v.hostReader().StringN(ctx, key)
}

func (v *scopedView) IntN(ctx context.Context, key string) (int, bool) {
	return v.hostReader().IntN(ctx, key)
}

func (v *scopedView) BoolN(ctx context.Context, key string) (value, ok bool) {
	return v.hostReader().BoolN(ctx, key)
}

func (v *scopedView) DurationN(ctx context.Context, key string) (time.Duration, bool) {
	return v.hostReader().DurationN(ctx, key)
}

func (v *scopedView) StringSliceN(ctx context.Context, key string) ([]string, bool) {
	return v.hostReader().StringSliceN(ctx, key)
}

// Plugin returns a Writable over the named plugin's isolated partition. The
// partition is created lazily on first access and is NOT namespace-validated.
func (v *scopedView) Plugin(name string) Writable {
	data, ok := v.plugins[name]
	if !ok {
		data = map[string]json.RawMessage{}
		v.plugins[name] = data
	}
	return &writableView{data: data, validateNamespace: false, commit: v.commit, onWrite: v.markPluginDirty(name)}
}

// Host returns a Writable over the host partition (namespace-validated).
func (v *scopedView) Host() Writable {
	return &writableView{data: v.host, validateNamespace: true, commit: v.commit, onWrite: v.markHostDirty()}
}

// markPluginDirty returns a callback that records a write to the named plugin
// partition, or nil when this view has no dirty tracker (read-only views).
func (v *scopedView) markPluginDirty(name string) func() {
	if v.dirty == nil {
		return nil
	}
	return func() { v.dirty.plugins[name] = true }
}

// markHostDirty returns a callback that records a write to the host partition,
// or nil when this view has no dirty tracker.
func (v *scopedView) markHostDirty() func() {
	if v.dirty == nil {
		return nil
	}
	return func() { v.dirty.host = true }
}

// writableView is a read+write view over a single partition map. Reads reuse
// jsonMapSettings (gated by validateNamespace); writes mutate the map in place
// and, when a commit func is present, persist via it.
type writableView struct {
	data              map[string]json.RawMessage
	validateNamespace bool
	onWrite           func() // marks this partition dirty on the parent view; may be nil
	commit            func(ctx context.Context) error
}

func (w *writableView) reader() *jsonMapSettings {
	return &jsonMapSettings{data: w.data, validateNamespace: w.validateNamespace}
}

func (w *writableView) StringN(ctx context.Context, key string) (string, bool) {
	return w.reader().StringN(ctx, key)
}

func (w *writableView) IntN(ctx context.Context, key string) (int, bool) {
	return w.reader().IntN(ctx, key)
}

func (w *writableView) BoolN(ctx context.Context, key string) (value, ok bool) {
	return w.reader().BoolN(ctx, key)
}

func (w *writableView) DurationN(ctx context.Context, key string) (time.Duration, bool) {
	return w.reader().DurationN(ctx, key)
}

func (w *writableView) StringSliceN(ctx context.Context, key string) ([]string, bool) {
	return w.reader().StringSliceN(ctx, key)
}

// SetString stores value as a JSON string under key. When validateNamespace
// is set (host partition), the key MUST begin with a registered namespace.
func (w *writableView) SetString(ctx context.Context, key, value string) error {
	if w.validateNamespace {
		if err := ValidateNamespace(key); err != nil {
			return oops.With("key", key).Wrap(err)
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return oops.With("key", key).Wrap(err)
	}
	// json.Marshal returns []byte, which is assignable to json.RawMessage
	// (the map's value type) without an explicit conversion.
	w.data[key] = encoded
	if w.onWrite != nil {
		w.onWrite()
	}
	if w.commit != nil {
		return w.commit(ctx)
	}
	return nil
}

// SetStringSlice stores values as a native JSON array under key so
// StringSliceN round-trips. Host partitions are namespace-validated.
func (w *writableView) SetStringSlice(ctx context.Context, key string, values []string) error {
	if w.validateNamespace {
		if err := ValidateNamespace(key); err != nil {
			return oops.With("key", key).Wrap(err)
		}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return oops.With("key", key).Wrap(err)
	}
	w.data[key] = encoded
	if w.onWrite != nil {
		w.onWrite()
	}
	if w.commit != nil {
		return w.commit(ctx)
	}
	return nil
}

// Compile-time interface checks.
var (
	_ Scoped   = (*scopedView)(nil)
	_ Writable = (*writableView)(nil)
)
