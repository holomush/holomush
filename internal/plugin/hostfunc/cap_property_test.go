// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

// =============================================================================
// PropertyCapability — compile-time interface satisfaction
// =============================================================================

var _ hostfunc.Capability = (*hostfunc.PropertyCapability)(nil)

// =============================================================================
// mockPropertyAccess — test double for PropertyAccess
// =============================================================================

type mockPropertyAccess struct {
	listByParentRet []hostfunc.PropertyInfo
	listByParentErr error

	findByPrefixRet []hostfunc.PropertyInfo
	findByPrefixErr error

	updateDescErr error

	listByParentCalls []propertyListByParentCall
	findByPrefixCalls []string
	updateDescCalls   []propertyUpdateDescCall
}

type propertyListByParentCall struct {
	SubjectID  string
	ParentType string
	ParentID   string
}

type propertyUpdateDescCall struct {
	SubjectID   string
	CharacterID string
	Description string
}

func (m *mockPropertyAccess) ListPropertiesByParent(_ context.Context, subjectID, parentType, parentID string) ([]hostfunc.PropertyInfo, error) {
	m.listByParentCalls = append(m.listByParentCalls, propertyListByParentCall{
		SubjectID:  subjectID,
		ParentType: parentType,
		ParentID:   parentID,
	})
	return m.listByParentRet, m.listByParentErr
}

func (m *mockPropertyAccess) FindPropertyByPrefix(_ context.Context, prefix string) ([]hostfunc.PropertyInfo, error) {
	m.findByPrefixCalls = append(m.findByPrefixCalls, prefix)
	return m.findByPrefixRet, m.findByPrefixErr
}

func (m *mockPropertyAccess) UpdateCharacterDescription(_ context.Context, subjectID, characterID, description string) error {
	m.updateDescCalls = append(m.updateDescCalls, propertyUpdateDescCall{
		SubjectID:   subjectID,
		CharacterID: characterID,
		Description: description,
	})
	return m.updateDescErr
}

// =============================================================================
// helpers
// =============================================================================

func newCapPropertyState(t *testing.T, pa hostfunc.PropertyAccess) *lua.LState {
	t.Helper()
	propCap := hostfunc.NewPropertyCapability(pa)
	L := lua.NewState()
	t.Cleanup(func() { L.Close() })
	propCap.Register(L, "test-plugin")
	return L
}

// =============================================================================
// Namespace
// =============================================================================

func TestPropertyCapabilityNamespaceReturnsProperty(t *testing.T) {
	propCap := hostfunc.NewPropertyCapability(nil)
	assert.Equal(t, "property", propCap.Namespace())
}

// =============================================================================
// Register — table structure
// =============================================================================

func TestPropertyCapabilityRegisterCreatesFunctionTable(t *testing.T) {
	pa := &mockPropertyAccess{}
	L := newCapPropertyState(t, pa)

	tbl := L.GetGlobal("property")
	require.Equal(t, lua.LTTable, tbl.Type(), "property global should be a table")

	for _, fn := range []string{"list_by_parent", "find_by_prefix", "update_character_description"} {
		field := L.GetField(tbl.(*lua.LTable), fn)
		assert.Equal(t, lua.LTFunction, field.Type(), "property.%s should be a function", fn)
	}
}

// =============================================================================
// property.list_by_parent
// =============================================================================

func TestPropertyCapabilityListByParentCallsServiceWithArgs(t *testing.T) {
	pa := &mockPropertyAccess{}
	L := newCapPropertyState(t, pa)

	err := L.DoString(`property.list_by_parent("subj-1", "character", "char-1")`)
	require.NoError(t, err)

	require.Len(t, pa.listByParentCalls, 1)
	assert.Equal(t, "subj-1", pa.listByParentCalls[0].SubjectID)
	assert.Equal(t, "character", pa.listByParentCalls[0].ParentType)
	assert.Equal(t, "char-1", pa.listByParentCalls[0].ParentID)
}

func TestPropertyCapabilityListByParentReturnsTableWithNameValueVisibility(t *testing.T) {
	pa := &mockPropertyAccess{
		listByParentRet: []hostfunc.PropertyInfo{
			{Name: "desc", Value: "A tall figure", Visibility: "public"},
			{Name: "title", Value: "Lord", Visibility: "private"},
		},
	}
	L := newCapPropertyState(t, pa)

	err := L.DoString(`result = property.list_by_parent("subj-1", "character", "char-1")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())
	assert.Equal(t, 2, result.(*lua.LTable).Len())
}

func TestPropertyCapabilityListByParentEntryHasExpectedFields(t *testing.T) {
	pa := &mockPropertyAccess{
		listByParentRet: []hostfunc.PropertyInfo{
			{Name: "desc", Value: "A tall figure", Visibility: "public"},
		},
	}
	L := newCapPropertyState(t, pa)

	err := L.DoString(`result = property.list_by_parent("subj-1", "character", "char-1"); entry = result[1]`)
	require.NoError(t, err)

	entry := L.GetGlobal("entry")
	require.Equal(t, lua.LTTable, entry.Type())
	tbl := entry.(*lua.LTable)
	assert.Equal(t, "desc", tbl.RawGetString("name").String())
	assert.Equal(t, "A tall figure", tbl.RawGetString("value").String())
	assert.Equal(t, "public", tbl.RawGetString("visibility").String())
}

func TestPropertyCapabilityListByParentReturnsEmptyTableWhenNone(t *testing.T) {
	pa := &mockPropertyAccess{listByParentRet: nil}
	L := newCapPropertyState(t, pa)

	err := L.DoString(`result = property.list_by_parent("subj-1", "character", "char-1")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())
	assert.Equal(t, 0, result.(*lua.LTable).Len())
}

func TestPropertyCapabilityListByParentReturnsNilAndErrorOnFailure(t *testing.T) {
	pa := &mockPropertyAccess{listByParentErr: errors.New("store failure")}
	L := newCapPropertyState(t, pa)

	err := L.DoString(`result, list_err = property.list_by_parent("subj-1", "character", "char-1")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
	errVal := L.GetGlobal("list_err")
	require.Equal(t, lua.LTString, errVal.Type())
	assert.NotEmpty(t, errVal.String())
}

// =============================================================================
// property.find_by_prefix
// =============================================================================

func TestPropertyCapabilityFindByPrefixCallsServiceWithPrefix(t *testing.T) {
	pa := &mockPropertyAccess{}
	L := newCapPropertyState(t, pa)

	err := L.DoString(`property.find_by_prefix("desc")`)
	require.NoError(t, err)

	require.Len(t, pa.findByPrefixCalls, 1)
	assert.Equal(t, "desc", pa.findByPrefixCalls[0])
}

func TestPropertyCapabilityFindByPrefixReturnsTableWithAllFields(t *testing.T) {
	pa := &mockPropertyAccess{
		findByPrefixRet: []hostfunc.PropertyInfo{
			{Name: "desc", Value: "tall", Visibility: "public", ParentType: "character", ParentID: "char-1"},
			{Name: "desc", Value: "short", Visibility: "public", ParentType: "character", ParentID: "char-2"},
		},
	}
	L := newCapPropertyState(t, pa)

	err := L.DoString(`result = property.find_by_prefix("desc"); entry = result[1]`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())
	assert.Equal(t, 2, result.(*lua.LTable).Len())

	entry := L.GetGlobal("entry")
	require.Equal(t, lua.LTTable, entry.Type())
	tbl := entry.(*lua.LTable)
	assert.Equal(t, "desc", tbl.RawGetString("name").String())
	assert.Equal(t, "tall", tbl.RawGetString("value").String())
	assert.Equal(t, "public", tbl.RawGetString("visibility").String())
	assert.Equal(t, "character", tbl.RawGetString("parent_type").String())
	assert.Equal(t, "char-1", tbl.RawGetString("parent_id").String())
}

func TestPropertyCapabilityFindByPrefixReturnsEmptyTableWhenNone(t *testing.T) {
	pa := &mockPropertyAccess{findByPrefixRet: nil}
	L := newCapPropertyState(t, pa)

	err := L.DoString(`result = property.find_by_prefix("nonexistent")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())
	assert.Equal(t, 0, result.(*lua.LTable).Len())
}

func TestPropertyCapabilityFindByPrefixReturnsNilAndErrorOnFailure(t *testing.T) {
	pa := &mockPropertyAccess{findByPrefixErr: errors.New("query failed")}
	L := newCapPropertyState(t, pa)

	err := L.DoString(`result, find_err = property.find_by_prefix("desc")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
	errVal := L.GetGlobal("find_err")
	require.Equal(t, lua.LTString, errVal.Type())
	assert.NotEmpty(t, errVal.String())
}

// =============================================================================
// property.update_character_description
// =============================================================================

func TestPropertyCapabilityUpdateCharacterDescriptionCallsServiceWithArgs(t *testing.T) {
	pa := &mockPropertyAccess{}
	L := newCapPropertyState(t, pa)

	err := L.DoString(`property.update_character_description("subj-1", "char-1", "A tall warrior")`)
	require.NoError(t, err)

	require.Len(t, pa.updateDescCalls, 1)
	assert.Equal(t, "subj-1", pa.updateDescCalls[0].SubjectID)
	assert.Equal(t, "char-1", pa.updateDescCalls[0].CharacterID)
	assert.Equal(t, "A tall warrior", pa.updateDescCalls[0].Description)
}

func TestPropertyCapabilityUpdateCharacterDescriptionReturnsNothingOnSuccess(t *testing.T) {
	pa := &mockPropertyAccess{}
	L := newCapPropertyState(t, pa)

	err := L.DoString(`result = property.update_character_description("subj-1", "char-1", "desc")`)
	require.NoError(t, err)

	// on success the function returns nothing, so result should be nil
	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
}

func TestPropertyCapabilityUpdateCharacterDescriptionReturnsNilAndErrorOnFailure(t *testing.T) {
	pa := &mockPropertyAccess{updateDescErr: errors.New("update failed")}
	L := newCapPropertyState(t, pa)

	err := L.DoString(`result, upd_err = property.update_character_description("subj-1", "char-1", "desc")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
	errVal := L.GetGlobal("upd_err")
	require.Equal(t, lua.LTString, errVal.Type())
	assert.NotEmpty(t, errVal.String())
}
