package brcrud

import (
	"reflect"

	"github.com/zaid223r/Bridgr/core/brschema"
)

// access applies a model's readonly and writeonly fields. It only touches the
// tagged fields, whose index paths were resolved at registration; models
// without tags skip it entirely.
type access[T any] struct {
	readOnly, writeOnly []*brschema.Field
}

func newAccess[T any](m *brschema.Model) access[T] {
	return access[T]{readOnly: m.ReadOnly, writeOnly: m.WriteOnly}
}

// clearReadOnly zeroes v's readonly fields before a body is decoded into it, so
// decoding allocates fresh values instead of writing through pointers v shares
// with the stored record.
func (a access[T]) clearReadOnly(v *T) {
	if len(a.readOnly) == 0 {
		return
	}
	rv := reflect.ValueOf(v).Elem()
	for _, f := range a.readOnly {
		rv.FieldByIndex(f.Index).SetZero()
	}
}

// keepReadOnly copies v's readonly fields from base, undoing whatever the body
// set.
func (a access[T]) keepReadOnly(v *T, base T) {
	if len(a.readOnly) == 0 {
		return
	}
	rv, bv := reflect.ValueOf(v).Elem(), reflect.ValueOf(base)
	for _, f := range a.readOnly {
		rv.FieldByIndex(f.Index).Set(bv.FieldByIndex(f.Index))
	}
}

// hide returns v without its writeonly fields. Their json tags have omitzero
// or omitempty (brschema enforces it), so zero values are left out.
func (a access[T]) hide(v T) T {
	if len(a.writeOnly) == 0 {
		return v
	}
	rv := reflect.ValueOf(&v).Elem()
	for _, f := range a.writeOnly {
		rv.FieldByIndex(f.Index).SetZero()
	}
	return v
}

// hideAll hides the writeonly fields of every item, in place.
func (a access[T]) hideAll(items []T) {
	if len(a.writeOnly) == 0 {
		return
	}
	for i := range items {
		items[i] = a.hide(items[i])
	}
}
