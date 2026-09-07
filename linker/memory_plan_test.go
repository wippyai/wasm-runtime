package linker

import (
	"context"
	"encoding/hex"
	"math"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

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

func TestOwnedMemoryPlanActualComponent(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	data, err := hex.DecodeString(twoCoreFixtureWasmHex)
	if err != nil {
		t.Fatal(err)
	}
	comp, err := component.DecodeAndValidate(data)
	if err != nil {
		t.Fatal(err)
	}
	pre, err := NewWithDefaults(rt).Instantiate(ctx, comp)
	if err != nil {
		t.Fatal(err)
	}
	defer pre.Close(ctx)
	plan, err := pre.OwnedMemoryPlan()
	if err != nil {
		t.Fatal(err)
	}
	total, err := InitialMemoryBytes(plan)
	if err != nil || len(plan) != 2 || total != 5*65536 {
		t.Fatalf("plan=%+v bytes=%d err=%v", plan, total, err)
	}
	plan[0].MinimumPages = 999
	again, err := pre.OwnedMemoryPlan()
	if err != nil {
		t.Fatal(err)
	}
	if again[0].MinimumPages == 999 {
		t.Fatal("caller mutated internal plan")
	}
	// Test multiplicity against an already compiled graph: the same definition
	// instantiated twice is a second owner, while an unused definition is not.
	original := pre.graph.Instances[pre.topoOrder[0]]
	pre.graph.Instances = append(pre.graph.Instances, original)
	pre.topoOrder = append(pre.topoOrder, len(pre.graph.Instances)-1)
	pre.ownedMemoryTypes = append(pre.ownedMemoryTypes, []wasm.MemoryType{{Limits: wasm.Limits{Min: 1000}}})
	repeated, err := pre.OwnedMemoryPlan()
	if err != nil {
		t.Fatal(err)
	}
	if len(repeated) != 3 || repeated[2].ModuleIndex != original.ModuleIndex {
		t.Fatalf("repeated plan=%+v", repeated)
	}
}
func TestMemoryMetadataIncludesUnexportedExcludesImports(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   int
	}{
		{`(module (memory 2))`, 1},
		{`(module (import "owner" "memory" (memory 2)))`, 0},
	} {
		data, err := wat.Compile(tc.source)
		if err != nil {
			t.Fatal(err)
		}
		m, err := wasm.ParseModuleMetadata(data)
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Memories) != tc.want {
			t.Fatalf("owned memories=%d want %d", len(m.Memories), tc.want)
		}
	}
}
func TestInitialMemoryBytesOverflow(t *testing.T) {
	for _, plan := range [][]OwnedMemory{{{MinimumPages: math.MaxUint64}}, {{MinimumPages: math.MaxUint64 / 65536}, {MinimumPages: 1}}} {
		if _, err := InitialMemoryBytes(plan); err == nil {
			t.Fatal("overflow accepted")
		}
	}
}

type planObservedAllocator struct{ initial []uint64 }
type planObservedMemory struct {
	owner       *planObservedAllocator
	data        []byte
	initialized bool
}

func (a *planObservedAllocator) Allocate(_, _ uint64) experimental.LinearMemory {
	return &planObservedMemory{owner: a}
}
func (m *planObservedMemory) Reallocate(size uint64) []byte {
	if !m.initialized {
		m.owner.initial = append(m.owner.initial, size)
		m.initialized = true
	}
	if uint64(len(m.data)) != size {
		m.data = make([]byte, size)
	}
	return m.data
}
func (*planObservedMemory) Free() {}

func TestOwnedMemoryPlanMatchesRepeatedRuntimeAllocations(t *testing.T) {
	for _, transform := range []bool{false, true} {
		t.Run(map[bool]string{false: "declared-unexported", true: "asyncify-added"}[transform], func(t *testing.T) {
			ctx := context.Background()
			rt := wazero.NewRuntime(ctx)
			defer rt.Close(ctx)
			source := `(module (memory 2))`
			if transform {
				source = `(module (import "env" "sleep" (func $sleep (param i32))) (func (export "run") (call $sleep (i32.const 1))))`
			}
			data, err := wat.Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			unused, err := wat.Compile(`(module (memory 100))`)
			if err != nil {
				t.Fatal(err)
			}
			comp := &component.ValidatedComponent{Raw: &component.Component{
				CoreModules: [][]byte{data, unused},
				CoreInstances: []component.CoreInstance{
					{Parsed: &component.ParsedCoreInstance{Kind: component.CoreInstanceInstantiate, ModuleIndex: 0}},
					{Parsed: &component.ParsedCoreInstance{Kind: component.CoreInstanceInstantiate, ModuleIndex: 0}},
				},
			}}
			l := New(rt, Options{AsyncifyTransform: transform, AsyncifyImports: []string{"env.sleep"}})
			if err := l.DefineFunc("env#sleep", api.GoModuleFunc(func(context.Context, api.Module, []uint64) {}), []api.ValueType{api.ValueTypeI32}, nil); err != nil {
				t.Fatal(err)
			}
			pre, err := l.Instantiate(ctx, comp)
			if err != nil {
				t.Fatal(err)
			}
			defer pre.Close(ctx)
			plan, err := pre.OwnedMemoryPlan()
			if err != nil {
				t.Fatal(err)
			}
			if len(plan) != 2 {
				t.Fatalf("plan=%+v", plan)
			}
			observed := &planObservedAllocator{}
			inst, err := pre.NewInstance(experimental.WithMemoryAllocator(ctx, observed))
			if err != nil {
				t.Fatal(err)
			}
			defer inst.Close(ctx)
			if len(observed.initial) != len(plan) {
				t.Fatalf("allocations=%v plan=%+v", observed.initial, plan)
			}
			for i, bytes := range observed.initial {
				if bytes != plan[i].MinimumPages*65536 {
					t.Errorf("allocation %d=%d plan=%+v", i, bytes, plan[i])
				}
			}
			if transform {
				original, err := wasm.ParseModuleMetadata(data)
				if err != nil {
					t.Fatal(err)
				}
				if len(original.Memories) != 0 || plan[0].MinimumPages == 0 {
					t.Fatal("test did not cover added memory")
				}
			}
		})
	}
}
