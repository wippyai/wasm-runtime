package engine

import (
	"fmt"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

// BenchmarkLivenessBranches measures compilation analysis separately from guest
// execution. Each branch can bypass a definition, so liveness must follow joins.
func BenchmarkLivenessBranches(b *testing.B) {
	for _, count := range []int{16, 256, 4096} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			instrs := make([]wasm.Instruction, 0, count*7+2)
			sites := make([]int, 0, count)
			for i := 0; i < count; i++ {
				sites = append(sites, len(instrs))
				instrs = append(instrs,
					wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					wasm.Instruction{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
					wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
					wasm.Instruction{Opcode: wasm.OpBrIf, Imm: wasm.BranchImm{LabelIdx: 0}},
					wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
					wasm.Instruction{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: uint32(i%63 + 1)}},
					wasm.Instruction{Opcode: wasm.OpEnd},
				)
			}
			instrs = append(instrs, wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}}, wasm.Instruction{Opcode: wasm.OpEnd})
			la := NewLivenessAnalyzer(0, 64)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if got := la.ComputeForCallSites(instrs, sites); len(got) != count {
					b.Fatal("missing call sites")
				}
			}
		})
	}
}
