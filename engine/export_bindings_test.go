package engine

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"go.bytecodealliance.org/wit"
)

// twoCoreFixtureWasmHex is the hex dump of a two-core component assembled from test_two_core.wat.
// Core1: memory 2 pages (131072 bytes), heap starting at 70000, exports func1 (param string) (result string), get_post1, get_heap1
// Core2: memory 3 pages (196608 bytes), heap starting at 140000, exports func2 (param string) (result string), get_post2, get_heap2
const twoCoreFixtureWasmHex = "" +
	"0061736d0d00010001e9030061736d01000000011a056000017f60017f00" +
	"60000060047f7f7f7f017f60027f7f017f030b0a00010201020301000004" +
	"05030100020612037f0141f0a2040b7f0141000b7f0141000b07be010b06" +
	"6d656d6f72790200126173796e636966795f6765745f7374617465000015" +
	"6173796e636966795f73746172745f756e77696e640001146173796e6369" +
	"66795f73746f705f756e77696e640002156173796e636966795f73746172" +
	"745f726577696e640003146173796e636966795f73746f705f726577696e" +
	"6400040c636162695f7265616c6c6f6300050f636162695f706f73745f66" +
	"756e63310006096765745f706f7374310007096765745f68656170310008" +
	"0566756e633100090a6b0a040023020b0600410124020b0600410024020b" +
	"0600410224020b0600410024020b1101017f23002104200420036a240020" +
	"040b0900230141016a24010b040023010b040023000b2201017f23002102" +
	"230041086a240020022000360200200241046a200136020020020b006f04" +
	"6e616d65000605636f726531024703050500076f6c645f70747201086f6c" +
	"645f73697a650205616c69676e03086e65775f73697a6504037265740601" +
	"00037074720903000370747201036c656e02067265747074720717030005" +
	"68656170310105706f73743102066173796e633101e9030061736d010000" +
	"00011a056000017f60017f0060000060047f7f7f7f017f60027f7f017f03" +
	"0b0a0001020102030100000405030100030612037f0141e0c5080b7f0141" +
	"000b7f0141000b07be010b066d656d6f72790200126173796e636966795f" +
	"6765745f73746174650000156173796e636966795f73746172745f756e77" +
	"696e640001146173796e636966795f73746f705f756e77696e6400021561" +
	"73796e636966795f73746172745f726577696e640003146173796e636966" +
	"795f73746f705f726577696e6400040c636162695f7265616c6c6f630005" +
	"0f636162695f706f73745f66756e63320006096765745f706f7374320007" +
	"096765745f686561703200080566756e633200090a6b0a040023020b0600" +
	"410124020b0600410024020b0600410224020b0600410024020b1101017f" +
	"23002104200420036a240020040b0900230141016a24010b040023010b04" +
	"0023000b2201017f23002102230041086a24002002200036020020024104" +
	"6a200136020020020b006f046e616d65000605636f726532024703050500" +
	"076f6c645f70747201086f6c645f73697a650205616c69676e03086e6577" +
	"5f73697a650403726574060100037074720903000370747201036c656e02" +
	"06726574707472071703000568656170320105706f73743202066173796e" +
	"633202070200000000010006ad010c00020100066d656d6f727900000100" +
	"0c636162695f7265616c6c6f63000001000566756e6331000001000f6361" +
	"62695f706f73745f66756e633100000100096765745f706f737431000001" +
	"00096765745f686561703100020101066d656d6f7279000001010c636162" +
	"695f7265616c6c6f63000001010566756e6332000001010f636162695f70" +
	"6f73745f66756e633200000101096765745f706f73743200000101096765" +
	"745f6865617032070e024001036d736773007340000079080c0100000103" +
	"030004000502000b0b01000566756e6331010000080c0100000603030104" +
	"050507000b0b01000566756e633201020008060100000300010b0f010009" +
	"6765742d706f73743101040008060100000800010b0f0100096765742d70" +
	"6f73743201060008060100000400010b0f0100096765742d686561703101" +
	"080008060100000900010b0f0100096765742d6865617032010a0000ba02" +
	"0e636f6d706f6e656e742d6e616d6501870100000a00087265616c6c6f63" +
	"31010a66756e63315f636f7265020a706f7374315f636f7265030e676574" +
	"5f706f7374315f636f7265040e6765745f68656170315f636f7265050872" +
	"65616c6c6f6332060a66756e63325f636f7265070a706f7374325f636f72" +
	"65080e6765745f706f7374325f636f7265090e6765745f68656170325f63" +
	"6f7265010f00020200046d656d3101046d656d3201110011020005636f72" +
	"65310105636f72653201110012020005696e7374310105696e7374320150" +
	"010600056c6966743102056c69667432040e6c6966745f6765745f706f73" +
	"7431060e6c6966745f6765745f706f737432080e6c6966745f6765745f68" +
	"656170310a0e6c6966745f6765745f68656170320116030200087374725f" +
	"7479706501087533325f74797065"

func loadTwoCoreModule(t *testing.T) (*WazeroEngine, *WazeroModule) {
	t.Helper()
	wasmBytes, err := hex.DecodeString(twoCoreFixtureWasmHex)
	if err != nil {
		t.Fatalf("hex decode wasm: %v", err)
	}

	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		eng.Close(ctx)
		t.Fatalf("LoadModule: %v", err)
	}

	return eng, mod
}

func TestExportBindings_DefaultMemoryAmbiguity(t *testing.T) {
	ctx := context.Background()
	eng, mod := loadTwoCoreModule(t)
	defer eng.Close(ctx)

	t.Run("ambiguous default leaves instance memory nil", func(t *testing.T) {
		inst, err := mod.Instantiate(ctx)
		if err != nil {
			t.Fatalf("Instantiate: %v", err)
		}
		defer inst.Close(ctx)

		if sz := inst.MemorySize(); sz != 0 {
			t.Fatalf("expected MemorySize() = 0 when multiple memories exist without EntryExport, got %d", sz)
		}
	})

	t.Run("explicit EntryExport selects core1 memory", func(t *testing.T) {
		inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EntryExport: "func1"})
		if err != nil {
			t.Fatalf("InstantiateWithConfig func1: %v", err)
		}
		defer inst.Close(ctx)

		if sz := inst.MemorySize(); sz != 131072 {
			t.Fatalf("expected MemorySize() = 131072 for func1 (core1), got %d", sz)
		}
	})

	t.Run("explicit EntryExport selects core2 memory", func(t *testing.T) {
		inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EntryExport: "func2"})
		if err != nil {
			t.Fatalf("InstantiateWithConfig func2: %v", err)
		}
		defer inst.Close(ctx)

		if sz := inst.MemorySize(); sz != 196608 {
			t.Fatalf("expected MemorySize() = 196608 for func2 (core2), got %d", sz)
		}
	})
}

func TestExportBindings_HighOffsetSeparationAndInterleavedCalls(t *testing.T) {
	ctx := context.Background()
	eng, mod := loadTwoCoreModule(t)
	defer eng.Close(ctx)

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Verify initial state
	h1, err := inst.CallWithLift(ctx, "get-heap1")
	if err != nil {
		t.Fatalf("get-heap1: %v", err)
	}
	if h1.(uint32) != 70000 {
		t.Fatalf("initial heap1 = %d, want 70000", h1)
	}

	h2, err := inst.CallWithLift(ctx, "get-heap2")
	if err != nil {
		t.Fatalf("get-heap2: %v", err)
	}
	if h2.(uint32) != 140000 {
		t.Fatalf("initial heap2 = %d, want 140000", h2)
	}

	// Call func1: allocates at 70000 in core1, runs cabi_post_func1
	res1, err := inst.CallWithLift(ctx, "func1", "hello-core1")
	if err != nil {
		t.Fatalf("func1: %v", err)
	}
	if res1.(string) != "hello-core1" {
		t.Fatalf("func1 result = %q, want %q", res1, "hello-core1")
	}

	// Verify post1 incremented, post2 untouched
	p1, _ := inst.CallWithLift(ctx, "get-post1")
	if p1.(uint32) != 1 {
		t.Fatalf("post1 = %d, want 1", p1)
	}
	p2, _ := inst.CallWithLift(ctx, "get-post2")
	if p2.(uint32) != 0 {
		t.Fatalf("post2 = %d, want 0", p2)
	}

	// Verify heap1 advanced, heap2 untouched
	h1After, _ := inst.CallWithLift(ctx, "get-heap1")
	if h1After.(uint32) <= 70000 {
		t.Fatalf("heap1 after func1 = %d, want > 70000", h1After)
	}
	h2After, _ := inst.CallWithLift(ctx, "get-heap2")
	if h2After.(uint32) != 140000 {
		t.Fatalf("heap2 after func1 = %d, want untouched 140000", h2After)
	}

	// Call func2: allocates at 140000 in core2, runs cabi_post_func2
	res2, err := inst.CallWithLift(ctx, "func2", "greetings-from-core2")
	if err != nil {
		t.Fatalf("func2: %v", err)
	}
	if res2.(string) != "greetings-from-core2" {
		t.Fatalf("func2 result = %q, want %q", res2, "greetings-from-core2")
	}

	// Verify post2 incremented
	p2After, _ := inst.CallWithLift(ctx, "get-post2")
	if p2After.(uint32) != 1 {
		t.Fatalf("post2 = %d, want 1", p2After)
	}

	// Interleaved alternating calls
	for idx := 0; idx < 10; idx++ {
		msg1 := "ping-core1"
		r1, err := inst.CallWithLift(ctx, "func1", msg1)
		if err != nil || r1.(string) != msg1 {
			t.Fatalf("interleaved func1[%d]: got %v, err %v", idx, r1, err)
		}

		msg2 := "pong-core2"
		r2, err := inst.CallWithLift(ctx, "func2", msg2)
		if err != nil || r2.(string) != msg2 {
			t.Fatalf("interleaved func2[%d]: got %v, err %v", idx, r2, err)
		}
	}

	// Both posts should now be 11
	finalP1, _ := inst.CallWithLift(ctx, "get-post1")
	finalP2, _ := inst.CallWithLift(ctx, "get-post2")
	if finalP1.(uint32) != 11 {
		t.Fatalf("final post1 = %d, want 11", finalP1)
	}
	if finalP2.(uint32) != 11 {
		t.Fatalf("final post2 = %d, want 11", finalP2)
	}
}

func TestExportBindings_TypedAPIs(t *testing.T) {
	ctx := context.Background()
	eng, mod := loadTwoCoreModule(t)
	defer eng.Close(ctx)

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	stringType := []wit.Type{wit.String{}}

	// 1. CallWithTypes
	t.Run("CallWithTypes", func(t *testing.T) {
		r1, err := inst.CallWithTypes(ctx, "func1", stringType, stringType, "typed-arg1")
		if err != nil {
			t.Fatalf("CallWithTypes func1: %v", err)
		}
		if r1.(string) != "typed-arg1" {
			t.Fatalf("CallWithTypes func1 = %q, want %q", r1, "typed-arg1")
		}

		r2, err := inst.CallWithTypes(ctx, "func2", stringType, stringType, "typed-arg2")
		if err != nil {
			t.Fatalf("CallWithTypes func2: %v", err)
		}
		if r2.(string) != "typed-arg2" {
			t.Fatalf("CallWithTypes func2 = %q, want %q", r2, "typed-arg2")
		}
	})

	// 2. CallInto
	t.Run("CallInto", func(t *testing.T) {
		var out1 string
		if err := inst.CallInto(ctx, "func1", stringType, stringType, &out1, "into-arg1"); err != nil {
			t.Fatalf("CallInto func1: %v", err)
		}
		if out1 != "into-arg1" {
			t.Fatalf("CallInto func1 = %q, want %q", out1, "into-arg1")
		}

		var out2 string
		if err := inst.CallInto(ctx, "func2", stringType, stringType, &out2, "into-arg2"); err != nil {
			t.Fatalf("CallInto func2: %v", err)
		}
		if out2 != "into-arg2" {
			t.Fatalf("CallInto func2 = %q, want %q", out2, "into-arg2")
		}
	})

	// 3. StartCall / Step / LiftResult
	t.Run("StartCall and LiftResult", func(t *testing.T) {
		asyncInst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EnableAsyncify: true})
		if err != nil {
			t.Fatalf("InstantiateWithConfig: %v", err)
		}
		defer asyncInst.Close(ctx)

		cs, err := asyncInst.StartCall(ctx, "func1", "session-arg")
		if err != nil {
			t.Fatalf("StartCall: %v", err)
		}
		stepRes, err := cs.Step(ctx, nil)
		if err != nil {
			t.Fatalf("Step: %v", err)
		}
		if stepRes.Status != StepDone {
			t.Fatalf("Step status = %v, want StepDone", stepRes.Status)
		}
		lifted, err := cs.LiftResult(ctx, stepRes.Results)
		if err != nil {
			t.Fatalf("LiftResult: %v", err)
		}
		if lifted.(string) != "session-arg" {
			t.Fatalf("LiftResult = %q, want %q", lifted, "session-arg")
		}
	})
}

func TestExportBindings_SiblingClose(t *testing.T) {
	ctx := context.Background()
	eng, mod := loadTwoCoreModule(t)
	defer eng.Close(ctx)

	instA, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate A: %v", err)
	}

	instB, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate B: %v", err)
	}
	defer instB.Close(ctx)

	// Exercise both instances
	if _, err := instA.CallWithLift(ctx, "func1", "a-core1"); err != nil {
		t.Fatalf("instA func1: %v", err)
	}
	if _, err := instB.CallWithLift(ctx, "func1", "b-core1"); err != nil {
		t.Fatalf("instB func1: %v", err)
	}

	// Close instA
	if err := instA.Close(ctx); err != nil {
		t.Fatalf("Close instA: %v", err)
	}

	// Verify instB continues operating normally on both cores
	r1, err := instB.CallWithLift(ctx, "func1", "b-core1-after-close")
	if err != nil || r1.(string) != "b-core1-after-close" {
		t.Fatalf("instB func1 after close: got %v, err %v", r1, err)
	}

	r2, err := instB.CallWithLift(ctx, "func2", "b-core2-after-close")
	if err != nil || r2.(string) != "b-core2-after-close" {
		t.Fatalf("instB func2 after close: got %v, err %v", r2, err)
	}
}

func TestExportBindings_AsyncifySuspensionRejection(t *testing.T) {
	ctx := context.Background()
	eng, mod := loadTwoCoreModule(t)
	defer eng.Close(ctx)

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EnableAsyncify: true})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Resolve binding for func1 (core1)
	binding1, err := inst.getExportBinding("func1")
	if err != nil {
		t.Fatalf("getExportBinding func1: %v", err)
	}

	// Resolve binding for func2 (core2)
	binding2, err := inst.getExportBinding("func2")
	if err != nil {
		t.Fatalf("getExportBinding func2: %v", err)
	}

	// Verify core module separation
	if binding1.coreMod == binding2.coreMod {
		t.Fatal("expected distinct core modules for func1 and func2")
	}

	// Simulate an in-progress suspended session on core1
	cs1 := &CallSession{
		instance: inst,
		binding:  binding1,
		done:     false,
	}
	inst.setSuspendedSession(cs1)

	// 1. Attempting to call func2 on core2 must be rejected with actionable message
	_, err = inst.CallWithLift(ctx, "func2", "reject-test")
	if err == nil {
		t.Fatal("expected CallWithLift func2 to fail while core1 suspended, got nil")
	}
	if !strings.Contains(err.Error(), "switching core while suspended is not supported") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// 2. Other APIs on core2 must also reject
	stringType := []wit.Type{wit.String{}}
	if _, err := inst.CallWithTypes(ctx, "func2", stringType, stringType, "test"); err == nil {
		t.Fatal("expected CallWithTypes func2 to fail while core1 suspended")
	}
	var out string
	if err := inst.CallInto(ctx, "func2", stringType, stringType, &out, "test"); err == nil {
		t.Fatal("expected CallInto func2 to fail while core1 suspended")
	}
	if _, err := inst.StartCall(ctx, "func2", "test"); err == nil {
		t.Fatal("expected StartCall func2 to fail while core1 suspended")
	}

	// Raw asynchronous entry must also reject before invoking a synchronous
	// target, even when that target has no scheduler of its own.
	savedAsync, savedScheduler := binding2.asyncify, binding2.scheduler
	binding2.asyncify, binding2.scheduler = nil, nil
	if _, err := inst.RunAsync(ctx, "func2", 0, 0); err == nil || !strings.Contains(err.Error(), "suspended") {
		t.Fatalf("RunAsync bypassed suspended session: %v", err)
	}
	binding2.asyncify, binding2.scheduler = savedAsync, savedScheduler

	// 3. Attempting to start another call on core1 while suspended must also reject
	_, err = inst.CallWithLift(ctx, "func1", "reject-test2")
	if err == nil {
		t.Fatal("expected CallWithLift func1 to fail while call is active, got nil")
	}
	if !strings.Contains(err.Error(), "resume or complete active call before starting new call") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// 4. Clear/complete session on core1 and verify normal operation resumes
	inst.clearSuspendedSession(cs1)
	cs1.done = true

	r1, err := inst.CallWithLift(ctx, "func1", "resume-ok1")
	if err != nil || r1.(string) != "resume-ok1" {
		t.Fatalf("func1 after clear: got %v, err %v", r1, err)
	}

	r2, err := inst.CallWithLift(ctx, "func2", "resume-ok2")
	if err != nil || r2.(string) != "resume-ok2" {
		t.Fatalf("func2 after clear: got %v, err %v", r2, err)
	}
}
