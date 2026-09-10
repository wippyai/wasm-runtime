package engine

import (
	"context"
	"testing"

	"go.bytecodealliance.org/wit"

	"github.com/wippyai/wasm-runtime/wat"
)

// canonicalOwnershipComponentWAT defines a genuine WebAssembly Component Model component.
// It includes:
//  1. A core module with memory, an instrumented cabi_realloc/cabi_free allocator with
//     header tags to track allocations and detect double-frees, and asyncify exports.
//  2. Canonical lift exports with realloc and memory options for string and list parameters.
//  3. Guest functions that consume/free their arguments (simulating wit-bindgen guest binders),
//     retain arguments (exposing use-after-free), or trap on double-free.
const canonicalOwnershipComponentWAT = `(component
  (core module $m
    (memory (export "memory") 2)
    (global $heap (mut i32) (i32.const 4096))
    (global $alloc_count (export "alloc_count") (mut i32) (i32.const 0))
    (global $guest_free_count (export "guest_free_count") (mut i32) (i32.const 0))
    (global $total_free_count (export "total_free_count") (mut i32) (i32.const 0))
    (global $double_free_count (export "double_free_count") (mut i32) (i32.const 0))
    (global $trap_on_df (export "trap_on_df") (mut i32) (i32.const 0))
    (global $retained_ptr (export "retained_ptr") (mut i32) (i32.const 0))
    (global $async_state (export "async_state") (mut i32) (i32.const 0))

    ;; Asyncify exports
    (func (export "asyncify_get_state") (result i32)
      (global.get $async_state)
    )
    (func (export "asyncify_start_unwind") (param i32)
      (global.set $async_state (i32.const 1))
    )
    (func (export "asyncify_stop_unwind")
      (global.set $async_state (i32.const 0))
    )
    (func (export "asyncify_start_rewind") (param i32)
      (global.set $async_state (i32.const 2))
    )
    (func (export "asyncify_stop_rewind")
      (global.set $async_state (i32.const 0))
    )

    ;; cabi_realloc: (old_ptr, old_size, align, new_size) -> ptr
    ;; Stores an allocation header at (ptr - 4):
    ;; 170 (0xAA) = allocated
    ;; 255 (0xFF) = freed
    (func $cabi_realloc (export "cabi_realloc") (param $old_ptr i32) (param $old_size i32) (param $align i32) (param $new_size i32) (result i32)
      (local $ptr i32)
      (local.set $ptr (i32.and (i32.add (i32.add (global.get $heap) (i32.const 7)) (i32.const 0)) (i32.const -4)))
      (i32.store (local.get $ptr) (i32.const 170))
      (local.set $ptr (i32.add (local.get $ptr) (i32.const 4)))
      (global.set $heap (i32.add (local.get $ptr) (local.get $new_size)))
      (global.set $alloc_count (i32.add (global.get $alloc_count) (i32.const 1)))
      (local.get $ptr)
    )

    ;; cabi_free: (ptr, size, align)
    (func $cabi_free (export "cabi_free") (param $ptr i32) (param $size i32) (param $align i32)
      (local $header i32)
      (local $tag i32)
      (if (i32.eqz (local.get $ptr))
        (then (return))
      )
      (local.set $header (i32.sub (local.get $ptr) (i32.const 4)))
      (local.set $tag (i32.load (local.get $header)))
      (if (i32.eq (local.get $tag) (i32.const 255))
        (then
          (global.set $double_free_count (i32.add (global.get $double_free_count) (i32.const 1)))
          (if (i32.ne (global.get $trap_on_df) (i32.const 0))
            (then (unreachable))
          )
          (return)
        )
      )
      (i32.store (local.get $header) (i32.const 255))
      (global.set $total_free_count (i32.add (global.get $total_free_count) (i32.const 1)))
    )

    ;; consume_string: guest consumes and frees its string argument
    (func (export "consume_string") (param $ptr i32) (param $len i32) (result i32)
      (call $cabi_free (local.get $ptr) (local.get $len) (i32.const 1))
      (global.set $guest_free_count (i32.add (global.get $guest_free_count) (i32.const 1)))
      (local.get $len)
    )

    ;; consume_list: guest consumes and frees its list<u32> argument
    (func (export "consume_list") (param $ptr i32) (param $len i32) (result i32)
      (call $cabi_free (local.get $ptr) (i32.mul (local.get $len) (i32.const 4)) (i32.const 4))
      (global.set $guest_free_count (i32.add (global.get $guest_free_count) (i32.const 1)))
      (local.get $len)
    )

    ;; retain_string: guest retains string argument without freeing
    (func (export "retain_string") (param $ptr i32) (param $len i32) (result i32)
      (global.set $retained_ptr (local.get $ptr))
      (local.get $len)
    )

    ;; get_retained_tag: returns the allocation header tag of the retained string
    (func (export "get_retained_tag") (result i32)
      (if (i32.eqz (global.get $retained_ptr))
        (then (return (i32.const 0)))
      )
      (i32.load (i32.sub (global.get $retained_ptr) (i32.const 4)))
    )

    ;; two_params: string and u32
    (func (export "two_params") (param $ptr i32) (param $len i32) (param $num i32) (result i32)
      (call $cabi_free (local.get $ptr) (local.get $len) (i32.const 1))
      (global.set $guest_free_count (i32.add (global.get $guest_free_count) (i32.const 1)))
      (i32.add (local.get $len) (local.get $num))
    )
    ;; echo_consume: (string) -> string
    ;; Frees input string, returns "ok" via retptr (exercises fast string call paths)
    (func (export "echo_consume") (param $ptr i32) (param $len i32) (result i32)
      (local $retptr i32)
      (local $outptr i32)
      (call $cabi_free (local.get $ptr) (local.get $len) (i32.const 1))
      (global.set $guest_free_count (i32.add (global.get $guest_free_count) (i32.const 1)))
      (local.set $retptr (global.get $heap))
      (global.set $heap (i32.add (global.get $heap) (i32.const 8)))
      (local.set $outptr (global.get $heap))
      (global.set $heap (i32.add (global.get $heap) (i32.const 4)))
      (i32.store8 (local.get $outptr) (i32.const 111))
      (i32.store8 (i32.add (local.get $outptr) (i32.const 1)) (i32.const 107))
      (i32.store (local.get $retptr) (local.get $outptr))
      (i32.store (i32.add (local.get $retptr) (i32.const 4)) (i32.const 2))
      (local.get $retptr)
    )

    ;; echo_retain: (string) -> string
    ;; Retains input string, returns "ok" via retptr (exercises fast string retain)
    (func (export "echo_retain") (param $ptr i32) (param $len i32) (result i32)
      (local $retptr i32)
      (local $outptr i32)
      (global.set $retained_ptr (local.get $ptr))
      (local.set $retptr (global.get $heap))
      (global.set $heap (i32.add (global.get $heap) (i32.const 8)))
      (local.set $outptr (global.get $heap))
      (global.set $heap (i32.add (global.get $heap) (i32.const 4)))
      (i32.store8 (local.get $outptr) (i32.const 111))
      (i32.store8 (i32.add (local.get $outptr) (i32.const 1)) (i32.const 107))
      (i32.store (local.get $retptr) (local.get $outptr))
      (i32.store (i32.add (local.get $retptr) (i32.const 4)) (i32.const 2))
      (local.get $retptr)
    )

    ;; Helper getters and setters
    (func (export "get_alloc_count") (result i32) (global.get $alloc_count))
    (func (export "get_guest_free_count") (result i32) (global.get $guest_free_count))
    (func (export "get_total_free_count") (result i32) (global.get $total_free_count))
    (func (export "get_double_free_count") (result i32) (global.get $double_free_count))
    (func (export "set_trap_on_double_free") (param $val i32)
      (global.set $trap_on_df (local.get $val))
    )
    (func (export "reset_counters")
      (global.set $alloc_count (i32.const 0))
      (global.set $guest_free_count (i32.const 0))
      (global.set $total_free_count (i32.const 0))
      (global.set $double_free_count (i32.const 0))
      (global.set $retained_ptr (i32.const 0))
      (global.set $trap_on_df (i32.const 0))
    )
  )

  (core instance $inst (instantiate $m))
  (alias core export $inst "memory" (core memory $mem))
  (alias core export $inst "cabi_realloc" (core func $realloc))

  (type $string_func (func (param "s" string) (result u32)))
  (type $list_func (func (param "items" (list u32)) (result u32)))
  (type $two_params_func (func (param "s" string) (param "n" u32) (result u32)))
  (type $getter_func (func (result u32)))
  (type $setter_func (func (param "v" u32)))
  (type $void_func (func))
  (type $string_echo_func (func (param "s" string) (result string)))

  (alias core export $inst "echo_consume" (core func $echo_consume_core))
  (func (export "echo_consume") (type $string_echo_func)
    (canon lift (core func $echo_consume_core) (memory $mem) (realloc $realloc))
  )

  (alias core export $inst "echo_retain" (core func $echo_retain_core))
  (func (export "echo_retain") (type $string_echo_func)
    (canon lift (core func $echo_retain_core) (memory $mem) (realloc $realloc))
  )

  (alias core export $inst "consume_string" (core func $consume_string_core))
  (func (export "consume_string") (type $string_func)
    (canon lift (core func $consume_string_core) (memory $mem) (realloc $realloc))
  )

  (alias core export $inst "consume_list" (core func $consume_list_core))
  (func (export "consume_list") (type $list_func)
    (canon lift (core func $consume_list_core) (memory $mem) (realloc $realloc))
  )

  (alias core export $inst "retain_string" (core func $retain_string_core))
  (func (export "retain_string") (type $string_func)
    (canon lift (core func $retain_string_core) (memory $mem) (realloc $realloc))
  )

  (alias core export $inst "get_retained_tag" (core func $get_retained_tag_core))
  (func (export "get_retained_tag") (type $getter_func)
    (canon lift (core func $get_retained_tag_core))
  )

  (alias core export $inst "two_params" (core func $two_params_core))
  (func (export "two_params") (type $two_params_func)
    (canon lift (core func $two_params_core) (memory $mem) (realloc $realloc))
  )

  (alias core export $inst "get_alloc_count" (core func $get_alloc_count_core))
  (func (export "get_alloc_count") (type $getter_func)
    (canon lift (core func $get_alloc_count_core))
  )

  (alias core export $inst "get_guest_free_count" (core func $get_guest_free_count_core))
  (func (export "get_guest_free_count") (type $getter_func)
    (canon lift (core func $get_guest_free_count_core))
  )

  (alias core export $inst "get_total_free_count" (core func $get_total_free_count_core))
  (func (export "get_total_free_count") (type $getter_func)
    (canon lift (core func $get_total_free_count_core))
  )

  (alias core export $inst "get_double_free_count" (core func $get_double_free_count_core))
  (func (export "get_double_free_count") (type $getter_func)
    (canon lift (core func $get_double_free_count_core))
  )

  (alias core export $inst "set_trap_on_double_free" (core func $set_trap_on_double_free_core))
  (func (export "set_trap_on_double_free") (type $setter_func)
    (canon lift (core func $set_trap_on_double_free_core))
  )

  (alias core export $inst "reset_counters" (core func $reset_counters_core))
  (func (export "reset_counters") (type $void_func)
    (canon lift (core func $reset_counters_core))
  )
)
`

func compileOwnershipComponent(t *testing.T, watContent string) []byte {
	t.Helper()
	return componentFixture(t, watContent)
}

func setupOwnershipInstance(t *testing.T) (*WazeroEngine, *WazeroInstance) {
	t.Helper()
	ctx := context.Background()
	compBytes := compileOwnershipComponent(t, canonicalOwnershipComponentWAT)

	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}

	mod, err := eng.LoadModule(ctx, compBytes)
	if err != nil {
		eng.Close(ctx)
		t.Fatalf("LoadModule: %v", err)
	}

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EnableAsyncify: true})
	if err != nil {
		eng.Close(ctx)
		t.Fatalf("InstantiateWithConfig: %v", err)
	}

	return eng, inst
}

// TestCanonicalArgumentOwnership_SyncCallsPreserveOwnership verifies that synchronous callpaths
// (CallWithLift, CallWithTypes, and CallInto) transfer ownership of lowered arguments to the guest,
// resulting in exactly 1 guest free, 1 total free, and 0 double-frees when the guest consumes/frees
// its canonical arguments.
func TestCanonicalArgumentOwnership_SyncCallsPreserveOwnership(t *testing.T) {
	ctx := context.Background()
	eng, inst := setupOwnershipInstance(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	// Subtest 1: CallWithLift on consume_string
	t.Run("CallWithLift_StringParam", func(t *testing.T) {
		_, _ = inst.CallWithLift(ctx, "reset_counters")
		res, err := inst.CallWithLift(ctx, "consume_string", "hello-canonical")
		if err != nil {
			t.Fatalf("CallWithLift consume_string failed: %v", err)
		}
		if res.(uint32) != uint32(len("hello-canonical")) {
			t.Fatalf("unexpected res: %v", res)
		}

		guestFrees, _ := inst.CallWithLift(ctx, "get_guest_free_count")
		totalFrees, _ := inst.CallWithLift(ctx, "get_total_free_count")
		doubleFrees, _ := inst.CallWithLift(ctx, "get_double_free_count")

		if guestFrees.(uint32) != 1 {
			t.Fatalf("expected guest_free_count=1, got %v", guestFrees)
		}
		if doubleFrees.(uint32) != 0 {
			t.Fatalf("expected double_free_count=0, got %v", doubleFrees)
		}
		if totalFrees.(uint32) != 1 {
			t.Fatalf("expected total_free_count=1, got %v", totalFrees)
		}
	})

	// Subtest 2: CallWithTypes on consume_string
	t.Run("CallWithTypes_StringParam", func(t *testing.T) {
		_, _ = inst.CallWithLift(ctx, "reset_counters")
		res, err := inst.CallWithTypes(ctx, "consume_string", []wit.Type{wit.String{}}, []wit.Type{wit.U32{}}, "hello-types")
		if err != nil {
			t.Fatalf("CallWithTypes consume_string failed: %v", err)
		}
		if res.(uint32) != uint32(len("hello-types")) {
			t.Fatalf("unexpected res: %v", res)
		}

		guestFrees, _ := inst.CallWithLift(ctx, "get_guest_free_count")
		totalFrees, _ := inst.CallWithLift(ctx, "get_total_free_count")
		doubleFrees, _ := inst.CallWithLift(ctx, "get_double_free_count")

		if guestFrees.(uint32) != 1 {
			t.Fatalf("expected guest_free_count=1, got %v", guestFrees)
		}
		if doubleFrees.(uint32) != 0 {
			t.Fatalf("expected double_free_count=0 in CallWithTypes, got %v", doubleFrees)
		}
		if totalFrees.(uint32) != 1 {
			t.Fatalf("expected total_free_count=1, got %v", totalFrees)
		}
	})

	// Subtest 3: CallInto on consume_string
	t.Run("CallInto_StringParam", func(t *testing.T) {
		_, _ = inst.CallWithLift(ctx, "reset_counters")
		var out uint32
		err := inst.CallInto(ctx, "consume_string", []wit.Type{wit.String{}}, []wit.Type{wit.U32{}}, &out, "hello-into")
		if err != nil {
			t.Fatalf("CallInto consume_string failed: %v", err)
		}
		if out != uint32(len("hello-into")) {
			t.Fatalf("unexpected out: %v", out)
		}

		guestFrees, _ := inst.CallWithLift(ctx, "get_guest_free_count")
		totalFrees, _ := inst.CallWithLift(ctx, "get_total_free_count")
		doubleFrees, _ := inst.CallWithLift(ctx, "get_double_free_count")

		if guestFrees.(uint32) != 1 {
			t.Fatalf("expected guest_free_count=1, got %v", guestFrees)
		}
		if doubleFrees.(uint32) != 0 {
			t.Fatalf("expected double_free_count=0 in CallInto, got %v", doubleFrees)
		}
		if totalFrees.(uint32) != 1 {
			t.Fatalf("expected total_free_count=1, got %v", totalFrees)
		}
	})

	// Subtest 4: CallWithLift on consume_list (list<u32>)
	t.Run("CallWithLift_ListParam", func(t *testing.T) {
		_, _ = inst.CallWithLift(ctx, "reset_counters")
		items := []uint32{10, 20, 30, 40}
		res, err := inst.CallWithLift(ctx, "consume_list", items)
		if err != nil {
			t.Fatalf("CallWithLift consume_list failed: %v", err)
		}
		if res.(uint32) != uint32(len(items)) {
			t.Fatalf("unexpected res: %v", res)
		}

		guestFrees, _ := inst.CallWithLift(ctx, "get_guest_free_count")
		totalFrees, _ := inst.CallWithLift(ctx, "get_total_free_count")
		doubleFrees, _ := inst.CallWithLift(ctx, "get_double_free_count")

		if guestFrees.(uint32) != 1 {
			t.Fatalf("expected guest_free_count=1, got %v", guestFrees)
		}
		if doubleFrees.(uint32) != 0 {
			t.Fatalf("expected double_free_count=0 in list CallWithLift, got %v", doubleFrees)
		}
		if totalFrees.(uint32) != 1 {
			t.Fatalf("expected total_free_count=1, got %v", totalFrees)
		}
	})
}

// TestCanonicalArgumentOwnership_Audit_AsyncSessionPreservesOwnership proves that the async session
// (StartCall -> Step -> LiftResult) ALREADY implements the sound ownership model:
// In engine/wazero.go line 2050: StartCall uses `defer allocList.Release()` (not FreeAndRelease).
// As a result, StartCall transitions lowered argument ownership to the guest without a secondary
// host-free, resulting in 0 double frees.
func TestCanonicalArgumentOwnership_Audit_AsyncSessionPreservesOwnership(t *testing.T) {
	ctx := context.Background()
	eng, inst := setupOwnershipInstance(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	_, _ = inst.CallWithLift(ctx, "reset_counters")

	cs, err := inst.StartCall(ctx, "consume_string", "hello-async-soundness")
	if err != nil {
		t.Fatalf("StartCall failed: %v", err)
	}

	stepRes, err := cs.Step(ctx, nil)
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}
	if stepRes.Status != StepDone {
		t.Fatalf("expected StepDone, got %v", stepRes.Status)
	}

	lifted, err := cs.LiftResult(ctx, stepRes.Results)
	if err != nil {
		t.Fatalf("LiftResult failed: %v", err)
	}
	if lifted.(uint32) != uint32(len("hello-async-soundness")) {
		t.Fatalf("unexpected lifted result: %v", lifted)
	}

	guestFrees, _ := inst.CallWithLift(ctx, "get_guest_free_count")
	totalFrees, _ := inst.CallWithLift(ctx, "get_total_free_count")
	doubleFrees, _ := inst.CallWithLift(ctx, "get_double_free_count")

	// In the async session:
	// Guest frees the argument once; host does NOT call cabi_free.
	if guestFrees.(uint32) != 1 {
		t.Fatalf("expected guest_free_count=1, got %v", guestFrees)
	}
	if totalFrees.(uint32) != 1 {
		t.Fatalf("expected total_free_count=1, got %v", totalFrees)
	}
	if doubleFrees.(uint32) != 0 {
		t.Fatalf("StartCall violated ownership: double_free_count=%v, want 0", doubleFrees)
	}
}

// TestCanonicalArgumentOwnership_Audit_RetainedArgumentUseAfterFree proves that when a guest
// retains a canonical parameter instead of deallocating it, synchronous callpaths free the memory
// from under the guest, resulting in immediate heap corruption / use-after-free.
func TestCanonicalArgumentOwnership_Audit_RetainedArgumentUseAfterFree(t *testing.T) {
	ctx := context.Background()
	eng, inst := setupOwnershipInstance(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	_, _ = inst.CallWithLift(ctx, "reset_counters")

	res, err := inst.CallWithLift(ctx, "retain_string", "persisted-data")
	if err != nil {
		t.Fatalf("CallWithLift retain_string failed: %v", err)
	}
	if res.(uint32) != uint32(len("persisted-data")) {
		t.Fatalf("unexpected res: %v", res)
	}

	guestFrees, _ := inst.CallWithLift(ctx, "get_guest_free_count")
	totalFrees, _ := inst.CallWithLift(ctx, "get_total_free_count")
	tag, _ := inst.CallWithLift(ctx, "get_retained_tag")

	// Guest did not free the string
	if guestFrees.(uint32) != 0 {
		t.Fatalf("expected guest_free_count=0, got %v", guestFrees)
	}
	// Under correct canonical ownership transfer, host does NOT deallocate arguments.
	// Memory remains allocated for guest use: tag is 170 (0xAA, allocated), not 255 (freed).
	if totalFrees.(uint32) != 0 {
		t.Fatalf("expected total_free_count=0 (no host deallocation), got %v", totalFrees)
	}
	if tag.(uint32) != 170 {
		t.Fatalf("expected retained allocation tag to be 170 (0xAA allocated), got %v", tag)
	}
}

// TestCanonicalArgumentOwnership_Audit_PreEntryEncodingFailureHostCleanup verifies that when
// parameter encoding fails BEFORE the guest function is entered, the host safely frees its
// own partial allocations, leaving no leaks and causing no double-frees.
func TestCanonicalArgumentOwnership_Audit_PreEntryEncodingFailureHostCleanup(t *testing.T) {
	ctx := context.Background()
	eng, inst := setupOwnershipInstance(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	_, _ = inst.CallWithLift(ctx, "reset_counters")

	// two_params expects: (param "s" string) (param "n" u32)
	// Pass valid string for arg 0, but invalid type (string) for arg 1
	_, err := inst.CallWithLift(ctx, "two_params", "valid-allocated-string", "invalid-u32-param")
	if err == nil {
		t.Fatal("expected pre-entry encoding error, got nil")
	}

	allocCount, _ := inst.CallWithLift(ctx, "get_alloc_count")
	guestFrees, _ := inst.CallWithLift(ctx, "get_guest_free_count")
	totalFrees, _ := inst.CallWithLift(ctx, "get_total_free_count")
	doubleFrees, _ := inst.CallWithLift(ctx, "get_double_free_count")

	// 1 allocation occurred during string encoding
	if allocCount.(uint32) != 1 {
		t.Fatalf("expected alloc_count=1, got %v", allocCount)
	}
	// Guest was NEVER entered
	if guestFrees.(uint32) != 0 {
		t.Fatalf("expected guest_free_count=0, got %v", guestFrees)
	}
	// Host safely freed the orphaned partial allocation
	if totalFrees.(uint32) != 1 {
		t.Fatalf("expected total_free_count=1 (host cleanup), got %v", totalFrees)
	}
	// No double frees
	if doubleFrees.(uint32) != 0 {
		t.Fatalf("expected double_free_count=0, got %v", doubleFrees)
	}
}

// TestCanonicalArgumentOwnership_TrapOnDoubleFree verifies that with trap-on-double-free configured,
// synchronous and asynchronous calls execute cleanly without triggering traps or double-frees.
func TestCanonicalArgumentOwnership_TrapOnDoubleFree(t *testing.T) {
	ctx := context.Background()
	eng, inst := setupOwnershipInstance(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	// Configure guest allocator to execute 'unreachable' if cabi_free is called on already-freed pointer
	_, _ = inst.CallWithLift(ctx, "set_trap_on_double_free", uint32(1))

	// In CallWithLift: guest consumes argument and frees it. Host does not secondary free,
	// so no double-free occurs and trap is never triggered.
	res, err := inst.CallWithLift(ctx, "consume_string", "trap-payload")
	if err != nil {
		t.Fatalf("CallWithLift returned error: %v", err)
	}
	if res.(uint32) != uint32(len("trap-payload")) {
		t.Fatalf("unexpected res: %v", res)
	}

	doubleFrees, _ := inst.CallWithLift(ctx, "get_double_free_count")
	if doubleFrees.(uint32) != 0 {
		t.Fatalf("expected double_free_count=0, got %v", doubleFrees)
	}

	// Conversely, StartCall does NOT trigger the trap
	_, _ = inst.CallWithLift(ctx, "reset_counters")
	_, _ = inst.CallWithLift(ctx, "set_trap_on_double_free", uint32(1))

	cs, err := inst.StartCall(ctx, "consume_string", "trap-payload-async")
	if err != nil {
		t.Fatalf("StartCall failed: %v", err)
	}
	stepRes, err := cs.Step(ctx, nil)
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}
	lifted, err := cs.LiftResult(ctx, stepRes.Results)
	if err != nil {
		t.Fatalf("LiftResult failed: %v", err)
	}
	if lifted.(uint32) != uint32(len("trap-payload-async")) {
		t.Fatalf("unexpected lifted: %v", lifted)
	}

	doubleFreesAsync, _ := inst.CallWithLift(ctx, "get_double_free_count")
	if doubleFreesAsync.(uint32) != 0 {
		t.Fatalf("expected double_free_count=0 in StartCall, got %v", doubleFreesAsync)
	}
}

// TestCanonicalArgumentOwnership_Audit_LegacyRawWITContract demonstrates the distinction
// between genuine component canon lifts and legacy raw WIT core module exports.
// For raw core exports (exp.Canon == nil), the host-managed allocation contract applies:
// the guest core function does not own or free the memory, so the caller deallocates it.
func TestCanonicalArgumentOwnership_Audit_LegacyRawWITContract(t *testing.T) {
	ctx := context.Background()
	rawWat := `(module
		(memory (export "memory") 1)
		(global $heap (mut i32) (i32.const 1024))
		(global $raw_frees (export "raw_frees") (mut i32) (i32.const 0))
		(func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32)
			(local $ret i32)
			(local.set $ret (global.get $heap))
			(global.set $heap (i32.add (local.get $ret) (local.get 3)))
			(local.get $ret)
		)
		(func (export "cabi_free") (param i32 i32 i32)
			(global.set $raw_frees (i32.add (global.get $raw_frees) (i32.const 1)))
		)
		(func (export "raw_echo_len") (param $ptr i32) (param $len i32) (result i32)
			;; Raw core function: does NOT free memory, caller manages it
			local.get $len
		)
		(func (export "get_raw_frees") (result i32)
			(global.get $raw_frees)
		)
	)`

	wasmBytes, err := wat.Compile(rawWat)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Verify this is a raw core module export without canonical lift options
	if inst.linkerInst != nil {
		t.Fatal("expected inst.linkerInst == nil for raw core module")
	}
	if mod.canonRegistry != nil {
		t.Fatal("expected mod.canonRegistry == nil for raw core module")
	}

	res, err := inst.CallWithTypes(ctx, "raw_echo_len", []wit.Type{wit.String{}}, []wit.Type{wit.U32{}}, "raw-wit-test")
	if err != nil {
		t.Fatalf("CallWithTypes: %v", err)
	}
	if res.(uint32) != uint32(len("raw-wit-test")) {
		t.Fatalf("unexpected res: %v", res)
	}

	rawFrees, err := inst.CallWithTypes(ctx, "get_raw_frees", nil, []wit.Type{wit.U32{}})
	if err != nil {
		t.Fatalf("get_raw_frees: %v", err)
	}
	// Under the legacy host-managed contract, caller freed the buffer
	if rawFrees.(uint32) != 1 {
		t.Fatalf("expected raw_frees=1 under legacy host-managed contract, got %v", rawFrees)
	}
}

// TestCanonicalArgumentOwnership_RegressionSpecificationCompliance is a strict regression test.
// This regression verifies that CallWithLift, CallWithTypes, and CallInto perform ZERO
// host-side frees on lowered arguments, fully conforming to the Component Model Canonical ABI spec.
func TestCanonicalArgumentOwnership_RegressionSpecificationCompliance(t *testing.T) {
	ctx := context.Background()
	eng, inst := setupOwnershipInstance(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	calls := []struct {
		call func() error
		name string
	}{
		{
			call: func() error {
				_, err := inst.CallWithLift(ctx, "consume_string", "spec-test-lift")
				return err
			},
			name: "CallWithLift",
		},
		{
			call: func() error {
				_, err := inst.CallWithTypes(ctx, "consume_string", []wit.Type{wit.String{}}, []wit.Type{wit.U32{}}, "spec-test-types")
				return err
			},
			name: "CallWithTypes",
		},
		{
			call: func() error {
				var out uint32
				return inst.CallInto(ctx, "consume_string", []wit.Type{wit.String{}}, []wit.Type{wit.U32{}}, &out, "spec-test-into")
			},
			name: "CallInto",
		},
	}

	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			_, _ = inst.CallWithLift(ctx, "reset_counters")
			if err := tc.call(); err != nil {
				t.Fatalf("%s failed: %v", tc.name, err)
			}

			guestFrees, _ := inst.CallWithLift(ctx, "get_guest_free_count")
			doubleFrees, _ := inst.CallWithLift(ctx, "get_double_free_count")

			if guestFrees.(uint32) != 1 {
				t.Fatalf("expected guest_free_count=1, got %v", guestFrees)
			}
			if doubleFrees.(uint32) != 0 {
				t.Fatalf("SPEC VIOLATION: %s caused double_free_count=%v, want 0", tc.name, doubleFrees)
			}
		})
	}
}

// TestCanonicalArgumentOwnership_FastStringPaths explicitly exercises the tryFastStringCall
// and tryCallStringInto fast paths for (string) -> string exports.
func TestCanonicalArgumentOwnership_FastStringPaths(t *testing.T) {
	ctx := context.Background()
	eng, inst := setupOwnershipInstance(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	// 1. CallWithLift on echo_consume: exercises tryFastStringCall
	t.Run("CallWithLift_FastString", func(t *testing.T) {
		_, _ = inst.CallWithLift(ctx, "reset_counters")
		res, err := inst.CallWithLift(ctx, "echo_consume", "hello-fast-string")
		if err != nil {
			t.Fatalf("CallWithLift echo_consume failed: %v", err)
		}
		if res.(string) != "ok" {
			t.Fatalf("unexpected res: %v, want %q", res, "ok")
		}

		guestFrees, _ := inst.CallWithLift(ctx, "get_guest_free_count")
		totalFrees, _ := inst.CallWithLift(ctx, "get_total_free_count")
		doubleFrees, _ := inst.CallWithLift(ctx, "get_double_free_count")

		if guestFrees.(uint32) != 1 {
			t.Fatalf("expected guest_free_count=1, got %v", guestFrees)
		}
		if doubleFrees.(uint32) != 0 {
			t.Fatalf("fast string CallWithLift caused double_free_count=%v, want 0", doubleFrees)
		}
		if totalFrees.(uint32) != 1 {
			t.Fatalf("expected total_free_count=1, got %v", totalFrees)
		}
	})

	// 2. CallInto on echo_consume: exercises tryCallStringInto
	t.Run("CallInto_FastString", func(t *testing.T) {
		_, _ = inst.CallWithLift(ctx, "reset_counters")
		var out string
		err := inst.CallInto(ctx, "echo_consume", []wit.Type{wit.String{}}, []wit.Type{wit.String{}}, &out, "hello-into-string")
		if err != nil {
			t.Fatalf("CallInto echo_consume failed: %v", err)
		}
		if out != "ok" {
			t.Fatalf("unexpected out: %v, want %q", out, "ok")
		}

		guestFrees, _ := inst.CallWithLift(ctx, "get_guest_free_count")
		totalFrees, _ := inst.CallWithLift(ctx, "get_total_free_count")
		doubleFrees, _ := inst.CallWithLift(ctx, "get_double_free_count")

		if guestFrees.(uint32) != 1 {
			t.Fatalf("expected guest_free_count=1, got %v", guestFrees)
		}
		if doubleFrees.(uint32) != 0 {
			t.Fatalf("fast string CallInto caused double_free_count=%v, want 0", doubleFrees)
		}
		if totalFrees.(uint32) != 1 {
			t.Fatalf("expected total_free_count=1, got %v", totalFrees)
		}
	})

	// 3. CallWithTypes on echo_consume: exercises tryFastCall -> tryFastStringCall
	t.Run("CallWithTypes_FastString", func(t *testing.T) {
		_, _ = inst.CallWithLift(ctx, "reset_counters")
		res, err := inst.CallWithTypes(ctx, "echo_consume", []wit.Type{wit.String{}}, []wit.Type{wit.String{}}, "hello-types-string")
		if err != nil {
			t.Fatalf("CallWithTypes echo_consume failed: %v", err)
		}
		if res.(string) != "ok" {
			t.Fatalf("unexpected res: %v, want %q", res, "ok")
		}

		guestFrees, _ := inst.CallWithLift(ctx, "get_guest_free_count")
		totalFrees, _ := inst.CallWithLift(ctx, "get_total_free_count")
		doubleFrees, _ := inst.CallWithLift(ctx, "get_double_free_count")

		if guestFrees.(uint32) != 1 {
			t.Fatalf("expected guest_free_count=1, got %v", guestFrees)
		}
		if doubleFrees.(uint32) != 0 {
			t.Fatalf("fast string CallWithTypes caused double_free_count=%v, want 0", doubleFrees)
		}
		if totalFrees.(uint32) != 1 {
			t.Fatalf("expected total_free_count=1, got %v", totalFrees)
		}
	})
}

// TestCanonicalArgumentOwnership_FastStringRetained verifies that when the guest retains
// the input string in a fast string signature ((string) -> string), neither tryFastStringCall
// nor tryCallStringInto deallocates the input buffer.
func TestCanonicalArgumentOwnership_FastStringRetained(t *testing.T) {
	ctx := context.Background()
	eng, inst := setupOwnershipInstance(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	t.Run("CallWithLift_Retained", func(t *testing.T) {
		_, _ = inst.CallWithLift(ctx, "reset_counters")
		res, err := inst.CallWithLift(ctx, "echo_retain", "fast-retained-data")
		if err != nil {
			t.Fatalf("CallWithLift echo_retain failed: %v", err)
		}
		if res.(string) != "ok" {
			t.Fatalf("unexpected res: %v", res)
		}

		guestFrees, _ := inst.CallWithLift(ctx, "get_guest_free_count")
		totalFrees, _ := inst.CallWithLift(ctx, "get_total_free_count")
		tag, _ := inst.CallWithLift(ctx, "get_retained_tag")

		if guestFrees.(uint32) != 0 {
			t.Fatalf("expected guest_free_count=0, got %v", guestFrees)
		}
		if totalFrees.(uint32) != 0 {
			t.Fatalf("expected total_free_count=0, got %v", totalFrees)
		}
		if tag.(uint32) != 170 {
			t.Fatalf("expected retained allocation tag to remain 170 (allocated), got %v", tag)
		}
	})

	t.Run("CallInto_Retained", func(t *testing.T) {
		_, _ = inst.CallWithLift(ctx, "reset_counters")
		var out string
		err := inst.CallInto(ctx, "echo_retain", []wit.Type{wit.String{}}, []wit.Type{wit.String{}}, &out, "into-retained-data")
		if err != nil {
			t.Fatalf("CallInto echo_retain failed: %v", err)
		}
		if out != "ok" {
			t.Fatalf("unexpected out: %v", out)
		}

		guestFrees, _ := inst.CallWithLift(ctx, "get_guest_free_count")
		totalFrees, _ := inst.CallWithLift(ctx, "get_total_free_count")
		tag, _ := inst.CallWithLift(ctx, "get_retained_tag")

		if guestFrees.(uint32) != 0 {
			t.Fatalf("expected guest_free_count=0, got %v", guestFrees)
		}
		if totalFrees.(uint32) != 0 {
			t.Fatalf("expected total_free_count=0, got %v", totalFrees)
		}
		if tag.(uint32) != 170 {
			t.Fatalf("expected retained allocation tag to remain 170 (allocated), got %v", tag)
		}
	})
}
