package engine

import "testing"

// Canonical options belong to a component function, even when two lifts share
// the same core function and component type. Index zero is a valid memory.
func TestCanonicalDuplicateLiftOptions(t *testing.T) {
	for _, memoryFirst := range []bool{false, true} {
		name := "omitted-first"
		if memoryFirst {
			name = "memory-first"
		}
		t.Run(name, func(t *testing.T) {
			plain := `(func $plain (type $t) (canon lift (core func $f))) (export "plain" (func $plain))`
			withMemory := `(func $withmem (type $t) (canon lift (core func $f) (memory $mem))) (export "withmem" (func $withmem))`
			lifts := plain + withMemory
			if memoryFirst {
				lifts = withMemory + plain
			}
			data := compileComponentWasm(t, `(component
				(core module $m
					(memory (export "memory") 1)
					(func (export "value") (result i32) i32.const 42))
				(core instance $i (instantiate $m))
				(alias core export $i "memory" (core memory $mem))
				(alias core export $i "value" (core func $f))
				(type $t (func (result u32)))`+lifts+`)`)
			ctx := t.Context()
			eng, err := NewWazeroEngine(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer eng.Close(ctx)
			mod, err := eng.LoadModule(ctx, data)
			if err != nil {
				t.Fatal(err)
			}
			inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{})
			if err != nil {
				t.Fatal(err)
			}
			defer inst.Close(ctx)
			for _, export := range []string{"plain", "withmem"} {
				exp, ok := inst.linkerInst.GetExport(export)
				if !ok || exp.Canon == nil {
					t.Fatalf("missing canonical export %s", export)
				}
				wantMemory := export == "withmem"
				if (exp.Canon.Memory != nil) != wantMemory {
					t.Errorf("%s linker memory presence = %v, want %v", export, exp.Canon.Memory != nil, wantMemory)
				}
				binding, err := inst.getExportBinding(export)
				if err != nil {
					t.Fatal(err)
				}
				if (binding.memory != nil) != wantMemory {
					t.Errorf("%s engine memory presence = %v, want %v", export, binding.memory != nil, wantMemory)
				}
				got, err := inst.CallWithLift(ctx, export)
				if err != nil || got != uint32(42) {
					t.Fatalf("%s = %v, %v", export, got, err)
				}
			}
		})
	}
}
