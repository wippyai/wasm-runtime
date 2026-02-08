package wasm_test

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestValTypeString(t *testing.T) {
	tests := []struct {
		want string
		v    wasm.ValType
	}{
		{"i32", wasm.ValI32},
		{"i64", wasm.ValI64},
		{"f32", wasm.ValF32},
		{"f64", wasm.ValF64},
		{"v128", wasm.ValV128},
		{"funcref", wasm.ValFuncRef},
		{"externref", wasm.ValExtern},
		{"anyref", wasm.ValAnyRef},
		{"eqref", wasm.ValEqRef},
		{"i31ref", wasm.ValI31Ref},
		{"structref", wasm.ValStructRef},
		{"arrayref", wasm.ValArrayRef},
		{"nullref", wasm.ValNullRef},
		{"nullexternref", wasm.ValNullExternRef},
		{"nullfuncref", wasm.ValNullFuncRef},
		{"ref null", wasm.ValRefNull},
		{"ref", wasm.ValRef},
		{"unknown", wasm.ValType(0xFF)},
	}

	for _, tt := range tests {
		got := tt.v.String()
		if got != tt.want {
			t.Errorf("ValType(0x%02x).String() = %q, want %q", byte(tt.v), got, tt.want)
		}
	}
}

func TestModuleNumImportedFuncs(t *testing.T) {
	m := &wasm.Module{
		Imports: []wasm.Import{
			{Module: "env", Name: "f1", Desc: wasm.ImportDesc{Kind: wasm.KindFunc}},
			{Module: "env", Name: "m1", Desc: wasm.ImportDesc{Kind: wasm.KindMemory}},
			{Module: "env", Name: "f2", Desc: wasm.ImportDesc{Kind: wasm.KindFunc}},
			{Module: "env", Name: "g1", Desc: wasm.ImportDesc{Kind: wasm.KindGlobal}},
		},
	}

	if got := m.NumImportedFuncs(); got != 2 {
		t.Errorf("NumImportedFuncs() = %d, want 2", got)
	}
}

func TestModuleNumImportedGlobals(t *testing.T) {
	m := &wasm.Module{
		Imports: []wasm.Import{
			{Module: "env", Name: "g1", Desc: wasm.ImportDesc{Kind: wasm.KindGlobal}},
			{Module: "env", Name: "g2", Desc: wasm.ImportDesc{Kind: wasm.KindGlobal}},
			{Module: "env", Name: "f1", Desc: wasm.ImportDesc{Kind: wasm.KindFunc}},
		},
	}

	if got := m.NumImportedGlobals(); got != 2 {
		t.Errorf("NumImportedGlobals() = %d, want 2", got)
	}
}

func TestModuleNumImportedTables(t *testing.T) {
	m := &wasm.Module{
		Imports: []wasm.Import{
			{Module: "env", Name: "t1", Desc: wasm.ImportDesc{Kind: wasm.KindTable}},
		},
	}

	if got := m.NumImportedTables(); got != 1 {
		t.Errorf("NumImportedTables() = %d, want 1", got)
	}
}

func TestModuleNumImportedMemories(t *testing.T) {
	m := &wasm.Module{
		Imports: []wasm.Import{
			{Module: "env", Name: "m1", Desc: wasm.ImportDesc{Kind: wasm.KindMemory}},
			{Module: "env", Name: "m2", Desc: wasm.ImportDesc{Kind: wasm.KindMemory}},
		},
	}

	if got := m.NumImportedMemories(); got != 2 {
		t.Errorf("NumImportedMemories() = %d, want 2", got)
	}
}

func TestModuleNumImportedTags(t *testing.T) {
	m := &wasm.Module{
		Imports: []wasm.Import{
			{Module: "env", Name: "tag1", Desc: wasm.ImportDesc{Kind: wasm.KindTag}},
		},
	}

	if got := m.NumImportedTags(); got != 1 {
		t.Errorf("NumImportedTags() = %d, want 1", got)
	}
}

func TestModuleNumImportsEmpty(t *testing.T) {
	m := &wasm.Module{}
	if got := m.NumImportedFuncs(); got != 0 {
		t.Errorf("NumImportedFuncs() = %d, want 0", got)
	}
	if got := m.NumImportedGlobals(); got != 0 {
		t.Errorf("NumImportedGlobals() = %d, want 0", got)
	}
	if got := m.NumImportedTables(); got != 0 {
		t.Errorf("NumImportedTables() = %d, want 0", got)
	}
	if got := m.NumImportedMemories(); got != 0 {
		t.Errorf("NumImportedMemories() = %d, want 0", got)
	}
	if got := m.NumImportedTags(); got != 0 {
		t.Errorf("NumImportedTags() = %d, want 0", got)
	}
}

func TestModuleNumTypes(t *testing.T) {
	t.Run("simple types", func(t *testing.T) {
		m := &wasm.Module{
			Types: []wasm.FuncType{
				{Params: nil, Results: nil},
				{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
			},
		}
		if got := m.NumTypes(); got != 2 {
			t.Errorf("NumTypes() = %d, want 2", got)
		}
	})

	t.Run("with TypeDefs func", func(t *testing.T) {
		ft := wasm.FuncType{Params: nil, Results: nil}
		m := &wasm.Module{
			TypeDefs: []wasm.TypeDef{
				{Kind: wasm.TypeDefKindFunc, Func: &ft},
				{Kind: wasm.TypeDefKindFunc, Func: &ft},
			},
		}
		if got := m.NumTypes(); got != 2 {
			t.Errorf("NumTypes() = %d, want 2", got)
		}
	})

	t.Run("with rec type", func(t *testing.T) {
		ft := wasm.FuncType{Params: nil, Results: nil}
		m := &wasm.Module{
			TypeDefs: []wasm.TypeDef{
				{
					Kind: wasm.TypeDefKindRec,
					Rec: &wasm.RecType{
						Types: []wasm.SubType{
							{CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: &ft}},
							{CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: &ft}},
							{CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: &ft}},
						},
					},
				},
			},
		}
		if got := m.NumTypes(); got != 3 {
			t.Errorf("NumTypes() = %d, want 3", got)
		}
	})

	t.Run("empty module", func(t *testing.T) {
		m := &wasm.Module{}
		if got := m.NumTypes(); got != 0 {
			t.Errorf("NumTypes() = %d, want 0", got)
		}
	})
}

func TestModuleGetFuncType(t *testing.T) {
	t.Run("local function", func(t *testing.T) {
		m := &wasm.Module{
			Types: []wasm.FuncType{
				{Params: nil, Results: nil},
				{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
			},
			Funcs: []uint32{0, 1},
		}

		ft := m.GetFuncType(0)
		if ft == nil {
			t.Fatal("GetFuncType(0) returned nil")
		}
		if len(ft.Params) != 0 {
			t.Errorf("expected 0 params, got %d", len(ft.Params))
		}

		ft = m.GetFuncType(1)
		if ft == nil {
			t.Fatal("GetFuncType(1) returned nil")
		}
		if len(ft.Params) != 1 || ft.Params[0] != wasm.ValI32 {
			t.Errorf("expected 1 i32 param, got %v", ft.Params)
		}
	})

	t.Run("imported function", func(t *testing.T) {
		m := &wasm.Module{
			Types: []wasm.FuncType{
				{Params: []wasm.ValType{wasm.ValF64}, Results: nil},
			},
			Imports: []wasm.Import{
				{Module: "env", Name: "log", Desc: wasm.ImportDesc{Kind: wasm.KindFunc, TypeIdx: 0}},
			},
		}

		ft := m.GetFuncType(0)
		if ft == nil {
			t.Fatal("GetFuncType(0) returned nil")
		}
		if len(ft.Params) != 1 || ft.Params[0] != wasm.ValF64 {
			t.Errorf("expected 1 f64 param, got %v", ft.Params)
		}
	})

	t.Run("invalid index", func(t *testing.T) {
		m := &wasm.Module{
			Types: []wasm.FuncType{{Params: nil, Results: nil}},
			Funcs: []uint32{0},
		}

		if ft := m.GetFuncType(100); ft != nil {
			t.Error("expected nil for invalid index")
		}
	})

	t.Run("with TypeDefs", func(t *testing.T) {
		ft := wasm.FuncType{Params: []wasm.ValType{wasm.ValI64}, Results: []wasm.ValType{wasm.ValI64}}
		m := &wasm.Module{
			TypeDefs: []wasm.TypeDef{
				{Kind: wasm.TypeDefKindFunc, Func: &ft},
			},
			Funcs: []uint32{0},
		}

		got := m.GetFuncType(0)
		if got == nil {
			t.Fatal("GetFuncType(0) returned nil")
		}
		if len(got.Params) != 1 || got.Params[0] != wasm.ValI64 {
			t.Errorf("expected i64 param, got %v", got.Params)
		}
	})

	t.Run("with SubType containing func", func(t *testing.T) {
		ft := wasm.FuncType{Params: []wasm.ValType{wasm.ValF32}, Results: nil}
		m := &wasm.Module{
			TypeDefs: []wasm.TypeDef{
				{Kind: wasm.TypeDefKindSub, Sub: &wasm.SubType{
					Final:    true,
					CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: &ft},
				}},
			},
			Funcs: []uint32{0},
		}

		got := m.GetFuncType(0)
		if got == nil {
			t.Fatal("GetFuncType(0) returned nil")
		}
		if len(got.Params) != 1 || got.Params[0] != wasm.ValF32 {
			t.Errorf("expected f32 param, got %v", got.Params)
		}
	})

	t.Run("with SubType containing struct", func(t *testing.T) {
		st := wasm.StructType{Fields: []wasm.FieldType{{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}}}}
		m := &wasm.Module{
			TypeDefs: []wasm.TypeDef{
				{Kind: wasm.TypeDefKindSub, Sub: &wasm.SubType{
					Final:    true,
					CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &st},
				}},
			},
		}

		// Looking for func type at index 0 but it's a struct
		got := m.GetFuncType(0)
		if got != nil {
			t.Error("expected nil for struct type")
		}
	})

	t.Run("with RecType containing func", func(t *testing.T) {
		ft := wasm.FuncType{Params: nil, Results: []wasm.ValType{wasm.ValI32}}
		st := wasm.StructType{Fields: []wasm.FieldType{{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}}}}
		m := &wasm.Module{
			TypeDefs: []wasm.TypeDef{
				{Kind: wasm.TypeDefKindRec, Rec: &wasm.RecType{Types: []wasm.SubType{
					{Final: true, CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &st}},
					{Final: true, CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: &ft}},
				}}},
			},
			Funcs: []uint32{1}, // func 0 has typeIdx=1 (the func in rec)
		}

		// GetFuncType takes funcIdx, not typeIdx
		// func 0 maps to typeIdx 1 which is the func type
		got := m.GetFuncType(0)
		if got == nil {
			t.Fatal("GetFuncType(0) returned nil")
		}
		if len(got.Results) != 1 || got.Results[0] != wasm.ValI32 {
			t.Errorf("expected i32 result, got %v", got.Results)
		}
	})

	t.Run("with RecType struct at funcIdx", func(t *testing.T) {
		ft := wasm.FuncType{Params: nil, Results: []wasm.ValType{wasm.ValI32}}
		st := wasm.StructType{Fields: []wasm.FieldType{{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}}}}
		m := &wasm.Module{
			TypeDefs: []wasm.TypeDef{
				{Kind: wasm.TypeDefKindRec, Rec: &wasm.RecType{Types: []wasm.SubType{
					{Final: true, CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &st}},
					{Final: true, CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: &ft}},
				}}},
			},
			Funcs: []uint32{0}, // func 0 has typeIdx=0 (the struct - invalid!)
		}

		// func 0 maps to typeIdx 0 which is a struct, should return nil
		got := m.GetFuncType(0)
		if got != nil {
			t.Error("expected nil when func points to struct type")
		}
	})

	t.Run("TypeDefs out of range", func(t *testing.T) {
		ft := wasm.FuncType{Params: nil, Results: nil}
		m := &wasm.Module{
			TypeDefs: []wasm.TypeDef{
				{Kind: wasm.TypeDefKindFunc, Func: &ft},
			},
		}

		got := m.GetFuncType(100)
		if got != nil {
			t.Error("expected nil for out of range index")
		}
	})
}

func TestModuleAddType(t *testing.T) {
	m := &wasm.Module{}

	ft1 := wasm.FuncType{Params: nil, Results: nil}
	idx1 := m.AddType(ft1)
	if idx1 != 0 {
		t.Errorf("first AddType should return 0, got %d", idx1)
	}
	if len(m.Types) != 1 {
		t.Errorf("expected 1 type, got %d", len(m.Types))
	}

	ft2 := wasm.FuncType{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}}
	idx2 := m.AddType(ft2)
	if idx2 != 1 {
		t.Errorf("second AddType should return 1, got %d", idx2)
	}

	idx3 := m.AddType(ft1)
	if idx3 != 0 {
		t.Errorf("duplicate AddType should return 0, got %d", idx3)
	}
	if len(m.Types) != 2 {
		t.Errorf("expected 2 types after duplicate add, got %d", len(m.Types))
	}
}

func TestModuleAddTypeWithExtTypes(t *testing.T) {
	m := &wasm.Module{}

	ft1 := wasm.FuncType{
		ExtParams:  []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI32}},
		ExtResults: []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI32}},
	}
	idx1 := m.AddType(ft1)
	if idx1 != 0 {
		t.Errorf("first AddType should return 0, got %d", idx1)
	}

	ft2 := wasm.FuncType{
		ExtParams:  []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI32}},
		ExtResults: []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI32}},
	}
	idx2 := m.AddType(ft2)
	if idx2 != 0 {
		t.Errorf("duplicate ExtType AddType should return 0, got %d", idx2)
	}

	ft3 := wasm.FuncType{
		ExtParams:  []wasm.ExtValType{{Kind: wasm.ExtValKindRef, ValType: wasm.ValRefNull, RefType: wasm.RefType{Nullable: true, HeapType: -16}}},
		ExtResults: []wasm.ExtValType{},
	}
	idx3 := m.AddType(ft3)
	if idx3 != 1 {
		t.Errorf("new ExtType AddType should return 1, got %d", idx3)
	}

	// Duplicate ref type
	ft4 := wasm.FuncType{
		ExtParams:  []wasm.ExtValType{{Kind: wasm.ExtValKindRef, ValType: wasm.ValRefNull, RefType: wasm.RefType{Nullable: true, HeapType: -16}}},
		ExtResults: []wasm.ExtValType{},
	}
	idx4 := m.AddType(ft4)
	if idx4 != 1 {
		t.Errorf("duplicate ref type should return 1, got %d", idx4)
	}

	// Different heap type
	ft5 := wasm.FuncType{
		ExtParams:  []wasm.ExtValType{{Kind: wasm.ExtValKindRef, ValType: wasm.ValRefNull, RefType: wasm.RefType{Nullable: true, HeapType: -17}}},
		ExtResults: []wasm.ExtValType{},
	}
	idx5 := m.AddType(ft5)
	if idx5 != 2 {
		t.Errorf("different heap type should return 2, got %d", idx5)
	}

	// Different nullability
	ft6 := wasm.FuncType{
		ExtParams:  []wasm.ExtValType{{Kind: wasm.ExtValKindRef, ValType: wasm.ValRef, RefType: wasm.RefType{Nullable: false, HeapType: -16}}},
		ExtResults: []wasm.ExtValType{},
	}
	idx6 := m.AddType(ft6)
	if idx6 != 3 {
		t.Errorf("different nullability should return 3, got %d", idx6)
	}

	// Different kind
	ft7 := wasm.FuncType{
		ExtParams:  []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI64}},
		ExtResults: []wasm.ExtValType{},
	}
	idx7 := m.AddType(ft7)
	if idx7 != 4 {
		t.Errorf("different val type should return 4, got %d", idx7)
	}

	// Different param count
	ft8 := wasm.FuncType{
		ExtParams:  []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI32}, {Kind: wasm.ExtValKindSimple, ValType: wasm.ValI32}},
		ExtResults: []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI32}},
	}
	idx8 := m.AddType(ft8)
	if idx8 != 5 {
		t.Errorf("different param count should return 5, got %d", idx8)
	}
}
