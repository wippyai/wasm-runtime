package types

import (
	"reflect"
)

type CompiledType struct {
	SliceType    reflect.Type
	GoType       reflect.Type
	ElemType     *CompiledType
	ResourceType *CompiledType
	ErrType      *CompiledType
	OkType       *CompiledType
	Cases        []Case
	Fields       []Field
	FlatCount    int
	GoSize       uintptr
	WitAlign     uint32
	WitSize      uint32
	Kind         Kind
}

type Field struct {
	Type      *CompiledType
	Name      string
	WitName   string
	GoOffset  uintptr
	WitOffset uint32
	IsPointer bool
}

type Case struct {
	Type     *CompiledType
	Name     string
	GoOffset uintptr
}

func (ct *CompiledType) IsPrimitive() bool {
	return ct.Kind.IsPrimitive()
}

// IsPure returns true if type contains only primitives (no strings/lists requiring memory ops).
func (ct *CompiledType) IsPure() bool {
	switch ct.Kind {
	case KindString, KindList:
		return false
	case KindRecord, KindTuple:
		for _, f := range ct.Fields {
			if !f.Type.IsPure() {
				return false
			}
		}
		return true
	case KindOption:
		return ct.ElemType != nil && ct.ElemType.IsPure()
	case KindResult:
		okPure := ct.OkType == nil || ct.OkType.IsPure()
		errPure := ct.ErrType == nil || ct.ErrType.IsPure()
		return okPure && errPure
	case KindVariant:
		for _, c := range ct.Cases {
			if c.Type != nil && !c.Type.IsPure() {
				return false
			}
		}
		return true
	case KindEnum, KindFlags, KindOwn, KindBorrow:
		return true
	default:
		return ct.IsPrimitive()
	}
}
