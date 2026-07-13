// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package outbox_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
)

// This file is the reader-view half of INV-WORLD-4 (WRITER-BOUNDARY) — the
// dedicated binding target (the // Verifies: annotation is flipped on in 05-12
// Task 2). It lives beside the outbox/relay code because the writer boundary is
// exactly what makes the outbox the SOLE path a world state change reaches the
// feed: world.Service cannot issue an envelope-less write because it holds no
// write-capable repo — every mutation is forced through the mutate()/WriteIntent
// envelope seam. A regression that widens a Service repo field back to a
// write-capable Repository (re-opening a directly-callable, envelope-less write)
// fails here.
//
// The OTHER halves of INV-WORLD-4 are asserted elsewhere and listed together in
// the registry's asserted_by: the AST raw-world-SQL fence
// (test/meta/world_sql_fence_test.go, incl. entity_properties + migration files),
// the internal/world/postgres composition allowlist
// (test/meta/world_import_graph_test.go), and the D-06 guest-reaping tombstone
// regression (test/integration/auth/guest_reaper_tombstone_test.go) that keeps
// the guest FK-cascade deletion hole from regrowing.

// serviceReaderFields maps each world.Service repo field to the read-only reader
// interface it MUST hold.
func serviceReaderFields() []struct {
	field string
	want  reflect.Type
} {
	return []struct {
		field string
		want  reflect.Type
	}{
		{"locationRepo", reflect.TypeOf((*world.LocationReader)(nil)).Elem()},
		{"exitRepo", reflect.TypeOf((*world.ExitReader)(nil)).Elem()},
		{"objectRepo", reflect.TypeOf((*world.ObjectReader)(nil)).Elem()},
		{"characterRepo", reflect.TypeOf((*world.CharacterReader)(nil)).Elem()},
		{"sceneRepo", reflect.TypeOf((*world.SceneReader)(nil)).Elem()},
		{"propertyRepo", reflect.TypeOf((*world.PropertyReader)(nil)).Elem()},
	}
}

// TestWorldServiceExposesNoDirectlyCallableWriteRepo proves the writer-boundary
// compile fence: every world.Service repo field is the read-only reader view (so a
// direct s.xRepo.Create()/Update()/Delete() is a type error), and the reader views
// expose NO write method. Combined, no state change can reach the feed except
// through the envelope seam — the reader-view half of INV-WORLD-4 (WRITER-BOUNDARY).
//
// Verifies: INV-WORLD-4
func TestWorldServiceExposesNoDirectlyCallableWriteRepo(t *testing.T) {
	st := reflect.TypeOf(world.Service{})

	// 1. Every repo field is the reader-view interface, never a write-capable repo.
	for _, c := range serviceReaderFields() {
		f, ok := st.FieldByName(c.field)
		require.Truef(t, ok, "world.Service has field %q", c.field)
		assert.Equalf(t, c.want, f.Type,
			"world.Service.%s MUST be the read-only reader view %s so no envelope-less write is directly callable; "+
				"widening it back to a write-capable Repository re-opens a raw write path (INV-WORLD-4 WRITER-BOUNDARY)",
			c.field, c.want)
	}

	// 2. The reader views themselves carry no write method — the fence in (1) is
	//    meaningful only if a reader cannot smuggle a mutator.
	writeMethods := []string{"Create", "Update", "Delete", "Move", "UpdateLocation", "UpdatePreferences", "DeleteByParent"}
	for _, c := range serviceReaderFields() {
		for _, wm := range writeMethods {
			_, has := c.want.MethodByName(wm)
			assert.Falsef(t, has, "reader view %s MUST NOT expose write method %s (INV-WORLD-4)", c.want, wm)
		}
	}
}
