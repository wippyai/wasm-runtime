package runtime

import (
	"context"
	"testing"

	"go.bytecodealliance.org/wit"
)

// TestWAT_E2E_Arithmetic tests all arithmetic operations through full stack
func TestWAT_E2E_Arithmetic(t *testing.T) {
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	watSource := `(module
		(func (export "add_i32") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.add
		)
		(func (export "sub_i32") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.sub
		)
		(func (export "mul_i32") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.mul
		)
		(func (export "div_i32") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.div_s
		)
		(func (export "add_i64") (param i64 i64) (result i64)
			local.get 0
			local.get 1
			i64.add
		)
		(func (export "add_f32") (param f32 f32) (result f32)
			local.get 0
			local.get 1
			f32.add
		)
		(func (export "add_f64") (param f64 f64) (result f64)
			local.get 0
			local.get 1
			f64.add
		)
	)`

	mod, err := rt.LoadWAT(ctx, watSource, `
		add-i32: func(a: s32, b: s32) -> s32
		sub-i32: func(a: s32, b: s32) -> s32
		mul-i32: func(a: s32, b: s32) -> s32
		div-i32: func(a: s32, b: s32) -> s32
		add-i64: func(a: s64, b: s64) -> s64
		add-f32: func(a: f32, b: f32) -> f32
		add-f64: func(a: f64, b: f64) -> f64
	`)
	if err != nil {
		t.Fatalf("LoadWAT: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	tests := []struct {
		expect any
		name   string
		params []wit.Type
		result []wit.Type
		args   []any
	}{
		{int32(30), "add_i32", []wit.Type{wit.S32{}, wit.S32{}}, []wit.Type{wit.S32{}}, []any{int32(10), int32(20)}},
		{int32(20), "sub_i32", []wit.Type{wit.S32{}, wit.S32{}}, []wit.Type{wit.S32{}}, []any{int32(50), int32(30)}},
		{int32(42), "mul_i32", []wit.Type{wit.S32{}, wit.S32{}}, []wit.Type{wit.S32{}}, []any{int32(7), int32(6)}},
		{int32(10), "div_i32", []wit.Type{wit.S32{}, wit.S32{}}, []wit.Type{wit.S32{}}, []any{int32(100), int32(10)}},
		{int64(3000000000000), "add_i64", []wit.Type{wit.S64{}, wit.S64{}}, []wit.Type{wit.S64{}}, []any{int64(1000000000000), int64(2000000000000)}},
		{float32(4.0), "add_f32", []wit.Type{wit.F32{}, wit.F32{}}, []wit.Type{wit.F32{}}, []any{float32(1.5), float32(2.5)}},
		{float64(4.0), "add_f64", []wit.Type{wit.F64{}, wit.F64{}}, []wit.Type{wit.F64{}}, []any{float64(1.5), float64(2.5)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := inst.CallWithTypes(ctx, tc.name, tc.params, tc.result, tc.args...)
			if err != nil {
				t.Fatalf("Call %s: %v", tc.name, err)
			}
			if result != tc.expect {
				t.Errorf("expected %v, got %v", tc.expect, result)
			}
		})
	}
}

// TestWAT_E2E_ControlFlow tests loops, if/else, blocks
func TestWAT_E2E_ControlFlow(t *testing.T) {
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	watSource := `(module
		;; factorial using loop
		(func (export "factorial") (param $n i32) (result i32)
			(local $result i32)
			(local.set $result (i32.const 1))
			(block $done
				(loop $loop
					(br_if $done (i32.le_s (local.get $n) (i32.const 1)))
					(local.set $result (i32.mul (local.get $result) (local.get $n)))
					(local.set $n (i32.sub (local.get $n) (i32.const 1)))
					(br $loop)
				)
			)
			(local.get $result)
		)

		;; fibonacci using recursion
		(func $fib_inner (param $n i32) (result i32)
			(if (result i32) (i32.le_s (local.get $n) (i32.const 1))
				(then (local.get $n))
				(else
					(i32.add
						(call $fib_inner (i32.sub (local.get $n) (i32.const 1)))
						(call $fib_inner (i32.sub (local.get $n) (i32.const 2)))
					)
				)
			)
		)
		(func (export "fibonacci") (param i32) (result i32)
			(call $fib_inner (local.get 0))
		)

		;; absolute value using if
		(func (export "abs") (param $x i32) (result i32)
			(if (result i32) (i32.lt_s (local.get $x) (i32.const 0))
				(then (i32.sub (i32.const 0) (local.get $x)))
				(else (local.get $x))
			)
		)

		;; max of two numbers
		(func (export "max") (param $a i32) (param $b i32) (result i32)
			(if (result i32) (i32.gt_s (local.get $a) (local.get $b))
				(then (local.get $a))
				(else (local.get $b))
			)
		)
	)`

	mod, err := rt.LoadWAT(ctx, watSource, `
		factorial: func(n: s32) -> s32
		fibonacci: func(n: s32) -> s32
		abs: func(x: s32) -> s32
		max: func(a: s32, b: s32) -> s32
	`)
	if err != nil {
		t.Fatalf("LoadWAT: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	s32Params := []wit.Type{wit.S32{}}
	s32Result := []wit.Type{wit.S32{}}
	s32x2Params := []wit.Type{wit.S32{}, wit.S32{}}

	t.Run("factorial", func(t *testing.T) {
		tests := []struct{ n, expect int32 }{
			{0, 1}, {1, 1}, {5, 120}, {10, 3628800},
		}
		for _, tc := range tests {
			result, err := inst.CallWithTypes(ctx, "factorial", s32Params, s32Result, tc.n)
			if err != nil {
				t.Fatalf("factorial(%d): %v", tc.n, err)
			}
			if result != tc.expect {
				t.Errorf("factorial(%d) = %v, want %d", tc.n, result, tc.expect)
			}
		}
	})

	t.Run("fibonacci", func(t *testing.T) {
		tests := []struct{ n, expect int32 }{
			{0, 0}, {1, 1}, {2, 1}, {10, 55}, {15, 610},
		}
		for _, tc := range tests {
			result, err := inst.CallWithTypes(ctx, "fibonacci", s32Params, s32Result, tc.n)
			if err != nil {
				t.Fatalf("fibonacci(%d): %v", tc.n, err)
			}
			if result != tc.expect {
				t.Errorf("fibonacci(%d) = %v, want %d", tc.n, result, tc.expect)
			}
		}
	})

	t.Run("abs", func(t *testing.T) {
		tests := []struct{ x, expect int32 }{
			{5, 5}, {-5, 5}, {0, 0}, {-100, 100},
		}
		for _, tc := range tests {
			result, err := inst.CallWithTypes(ctx, "abs", s32Params, s32Result, tc.x)
			if err != nil {
				t.Fatalf("abs(%d): %v", tc.x, err)
			}
			if result != tc.expect {
				t.Errorf("abs(%d) = %v, want %d", tc.x, result, tc.expect)
			}
		}
	})

	t.Run("max", func(t *testing.T) {
		tests := []struct{ a, b, expect int32 }{
			{5, 3, 5}, {3, 5, 5}, {0, 0, 0}, {-1, -5, -1},
		}
		for _, tc := range tests {
			result, err := inst.CallWithTypes(ctx, "max", s32x2Params, s32Result, tc.a, tc.b)
			if err != nil {
				t.Fatalf("max(%d, %d): %v", tc.a, tc.b, err)
			}
			if result != tc.expect {
				t.Errorf("max(%d, %d) = %v, want %d", tc.a, tc.b, result, tc.expect)
			}
		}
	})
}

// TestWAT_E2E_Memory tests memory read/write operations
func TestWAT_E2E_Memory(t *testing.T) {
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	watSource := `(module
		(memory (export "memory") 1)

		;; write i32 to memory
		(func (export "store_i32") (param $addr i32) (param $val i32)
			(i32.store (local.get $addr) (local.get $val))
		)

		;; read i32 from memory
		(func (export "load_i32") (param $addr i32) (result i32)
			(i32.load (local.get $addr))
		)

		;; sum array of i32
		(func (export "sum_array") (param $addr i32) (param $len i32) (result i32)
			(local $sum i32)
			(local $i i32)
			(local.set $sum (i32.const 0))
			(local.set $i (i32.const 0))
			(block $done
				(loop $loop
					(br_if $done (i32.ge_u (local.get $i) (local.get $len)))
					(local.set $sum
						(i32.add
							(local.get $sum)
							(i32.load
								(i32.add
									(local.get $addr)
									(i32.mul (local.get $i) (i32.const 4))
								)
							)
						)
					)
					(local.set $i (i32.add (local.get $i) (i32.const 1)))
					(br $loop)
				)
			)
			(local.get $sum)
		)
	)`

	mod, err := rt.LoadWAT(ctx, watSource, `
		store-i32: func(addr: u32, val: s32)
		load-i32: func(addr: u32) -> s32
		sum-array: func(addr: u32, len: u32) -> s32
	`)
	if err != nil {
		t.Fatalf("LoadWAT: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	storeParams := []wit.Type{wit.U32{}, wit.S32{}}
	loadParams := []wit.Type{wit.U32{}}
	loadResult := []wit.Type{wit.S32{}}
	sumParams := []wit.Type{wit.U32{}, wit.U32{}}

	// Store and load single value
	t.Run("store_load", func(t *testing.T) {
		_, err := inst.CallWithTypes(ctx, "store_i32", storeParams, nil, uint32(0), int32(12345))
		if err != nil {
			t.Fatalf("store_i32: %v", err)
		}

		result, err := inst.CallWithTypes(ctx, "load_i32", loadParams, loadResult, uint32(0))
		if err != nil {
			t.Fatalf("load_i32: %v", err)
		}
		if result != int32(12345) {
			t.Errorf("expected 12345, got %v", result)
		}
	})

	// Store array and sum
	t.Run("sum_array", func(t *testing.T) {
		// Store values: 1, 2, 3, 4, 5 at addresses 100, 104, 108, 112, 116
		for i := 0; i < 5; i++ {
			_, err := inst.CallWithTypes(ctx, "store_i32", storeParams, nil,
				uint32(100+i*4), int32(i+1))
			if err != nil {
				t.Fatalf("store_i32[%d]: %v", i, err)
			}
		}

		result, err := inst.CallWithTypes(ctx, "sum_array", sumParams, loadResult,
			uint32(100), uint32(5))
		if err != nil {
			t.Fatalf("sum_array: %v", err)
		}
		if result != int32(15) {
			t.Errorf("sum_array = %v, want 15", result)
		}
	})
}

// TestWAT_E2E_Globals tests mutable and immutable globals
func TestWAT_E2E_Globals(t *testing.T) {
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	watSource := `(module
		(global $counter (mut i32) (i32.const 0))
		(global $multiplier i32 (i32.const 10))

		(func (export "get_counter") (result i32)
			(global.get $counter)
		)

		(func (export "increment") (result i32)
			(global.set $counter
				(i32.add (global.get $counter) (i32.const 1))
			)
			(global.get $counter)
		)

		(func (export "multiply") (param $x i32) (result i32)
			(i32.mul (local.get $x) (global.get $multiplier))
		)
	)`

	mod, err := rt.LoadWAT(ctx, watSource, "")
	if err != nil {
		t.Fatalf("LoadWAT: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	noParams := []wit.Type{}
	s32Result := []wit.Type{wit.S32{}}
	s32Params := []wit.Type{wit.S32{}}

	// Test counter
	t.Run("counter", func(t *testing.T) {
		result, err := inst.CallWithTypes(ctx, "get_counter", noParams, s32Result)
		if err != nil {
			t.Fatalf("get_counter: %v", err)
		}
		if result != int32(0) {
			t.Errorf("initial counter = %v, want 0", result)
		}

		for i := 1; i <= 5; i++ {
			result, err := inst.CallWithTypes(ctx, "increment", noParams, s32Result)
			if err != nil {
				t.Fatalf("increment: %v", err)
			}
			if result != int32(i) {
				t.Errorf("increment #%d = %v, want %d", i, result, i)
			}
		}
	})

	// Test immutable global
	t.Run("multiplier", func(t *testing.T) {
		result, _ := inst.CallWithTypes(ctx, "multiply", s32Params, s32Result, int32(7))
		if result != int32(70) {
			t.Errorf("multiply(7) = %v, want 70", result)
		}
	})
}

// TestWAT_E2E_MultipleInstances tests pooling with WAT modules
func TestWAT_E2E_MultipleInstances(t *testing.T) {
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	watSource := `(module
		(global $state (mut i32) (i32.const 0))

		(func (export "set") (param $v i32)
			(global.set $state (local.get $v))
		)

		(func (export "get") (result i32)
			(global.get $state)
		)
	)`

	mod, err := rt.LoadWAT(ctx, watSource, "")
	if err != nil {
		t.Fatalf("LoadWAT: %v", err)
	}

	// Create multiple instances
	const numInstances = 5
	instances := make([]*Instance, numInstances)
	for i := 0; i < numInstances; i++ {
		inst, err := mod.Instantiate(ctx)
		if err != nil {
			t.Fatalf("Instantiate[%d]: %v", i, err)
		}
		instances[i] = inst
	}
	defer func() {
		for _, inst := range instances {
			inst.Close(ctx)
		}
	}()

	setParams := []wit.Type{wit.S32{}}
	getResult := []wit.Type{wit.S32{}}

	// Set different values in each instance
	for i, inst := range instances {
		_, err := inst.CallWithTypes(ctx, "set", setParams, nil, int32(i*100))
		if err != nil {
			t.Fatalf("set[%d]: %v", i, err)
		}
	}

	// Verify each instance has its own state
	for i, inst := range instances {
		result, err := inst.CallWithTypes(ctx, "get", nil, getResult)
		if err != nil {
			t.Fatalf("get[%d]: %v", i, err)
		}
		expected := int32(i * 100)
		if result != expected {
			t.Errorf("instance[%d] get() = %v, want %d", i, result, expected)
		}
	}
}

// TestWAT_E2E_DataSection tests data section initialization
func TestWAT_E2E_DataSection(t *testing.T) {
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	watSource := `(module
		(memory (export "memory") 1)
		(data (i32.const 0) "Hello, WASM!")

		;; Get byte at offset
		(func (export "get_byte") (param $offset i32) (result i32)
			(i32.load8_u (local.get $offset))
		)

		;; Get string length (null-terminated)
		(func (export "strlen") (result i32)
			(local $i i32)
			(local.set $i (i32.const 0))
			(block $done
				(loop $loop
					(br_if $done (i32.eqz (i32.load8_u (local.get $i))))
					(local.set $i (i32.add (local.get $i) (i32.const 1)))
					(br $loop)
				)
			)
			(local.get $i)
		)
	)`

	mod, err := rt.LoadWAT(ctx, watSource, `
		get-byte: func(offset: u32) -> u32
		strlen: func() -> u32
	`)
	if err != nil {
		t.Fatalf("LoadWAT: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	u32Params := []wit.Type{wit.U32{}}
	u32Result := []wit.Type{wit.U32{}}

	// Check "Hello, WASM!" is in memory
	expected := "Hello, WASM!"
	for i, ch := range expected {
		result, _ := inst.CallWithTypes(ctx, "get_byte", u32Params, u32Result, uint32(i))
		if result != uint32(ch) {
			t.Errorf("byte[%d] = %v (%c), want %d (%c)", i, result, rune(result.(uint32)), ch, ch)
		}
	}

	// Check strlen
	result, _ := inst.CallWithTypes(ctx, "strlen", nil, u32Result)
	if result != uint32(len(expected)) {
		t.Errorf("strlen = %v, want %d", result, len(expected))
	}
}
