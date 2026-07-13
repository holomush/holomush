// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
)

// TestServiceHoldsOnlyReaderViews is the compile-time write-fence assertion
// (05-11, INV-WORLD-4 reader-view half). world.Service's repo fields MUST be the
// read-only reader interfaces so a direct `s.xRepo.Update(...)` / `.Delete(...)` /
// `.Create(...)` is a type error: the write-capable repos live ONLY on the write
// executor behind mutate(). A regression that widens a field back to the full
// Repository interface (re-opening a directly-callable write path) fails here.
func TestServiceHoldsOnlyReaderViews(t *testing.T) {
	st := reflect.TypeOf(world.Service{})

	cases := []struct {
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
	for _, c := range cases {
		f, ok := st.FieldByName(c.field)
		require.Truef(t, ok, "world.Service has field %q", c.field)
		assert.Equalf(t, c.want, f.Type,
			"world.Service.%s MUST be the read-only reader view %s (the writer repos live only on the write executor); "+
				"widening it back to a write-capable Repository re-opens a directly-callable envelope-less write path",
			c.field, c.want)
	}
}

// TestReaderViewsCarryNoWriteMethods documents that the reader interfaces expose
// no write method — a belt-and-braces check that Get/List reads are all a reader
// offers, so the fence in TestServiceHoldsOnlyReaderViews is meaningful.
func TestReaderViewsCarryNoWriteMethods(t *testing.T) {
	writeMethods := []string{"Create", "Update", "Delete", "Move", "UpdateLocation", "UpdatePreferences", "DeleteByParent"}
	readers := []reflect.Type{
		reflect.TypeOf((*world.LocationReader)(nil)).Elem(),
		reflect.TypeOf((*world.ExitReader)(nil)).Elem(),
		reflect.TypeOf((*world.ObjectReader)(nil)).Elem(),
		reflect.TypeOf((*world.CharacterReader)(nil)).Elem(),
		reflect.TypeOf((*world.SceneReader)(nil)).Elem(),
		reflect.TypeOf((*world.PropertyReader)(nil)).Elem(),
	}
	for _, r := range readers {
		for _, wm := range writeMethods {
			_, has := r.MethodByName(wm)
			assert.Falsef(t, has, "reader %s MUST NOT expose write method %s", r, wm)
		}
	}
}
