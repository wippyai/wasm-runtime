package handler

import (
	"github.com/wippyai/wasm-runtime/wasm"
)

// BinaryOpHandler processes operations that consume two values and produce one.
//
// WebAssembly has many binary operations: arithmetic (add, sub, mul, div),
// comparisons (eq, lt, gt), bitwise (and, or, xor, shl, shr), and more.
// They all follow the same stack pattern - pop two operands, push one result.
//
// In the flattened representation, we pop two local indices from our simulated
// stack, emit code to load both values, apply the operation, and store the
// result in a fresh temporary. The temporary's index goes onto the simulated
// stack as the "result" of this operation.
//
// The ResultType field specifies what type the result has. For most arithmetic
// ops this matches the input types, but comparison operations always produce
// i32 (0 or 1) regardless of whether they compare i32, i64, f32, or f64 values.
//
// Emitted code pattern:
//
//	local.get $lhs
//	local.get $rhs
//	<opcode>
//	local.set $result
type BinaryOpHandler struct {
	Opcode     byte
	ResultType wasm.ValType
}

func (h BinaryOpHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	rhs := ctx.Stack.Pop()
	lhs := ctx.Stack.Pop()
	tmp := ctx.AllocTemp(h.ResultType)

	ctx.Emit.LocalGet(lhs).LocalGet(rhs).EmitRawOpcode(h.Opcode).LocalSet(tmp)
	ctx.Stack.Push(tmp, h.ResultType)

	return nil
}

// StackEffect implements StackEffecter.
func (h BinaryOpHandler) StackEffect() StackEffect {
	return StackEffect{Pops: 2, Pushes: []wasm.ValType{h.ResultType}}
}

// UnaryOpHandler processes operations that consume one value and produce one.
//
// Unary operations include negation, absolute value, bit counting (clz, ctz,
// popcnt), type tests (eqz), and various float operations (ceil, floor, sqrt).
// They pop one value, apply the operation, and push one result.
//
// Like BinaryOpHandler, we work with local indices rather than stack values.
// Pop the operand's local index, emit the load-operate-store sequence, and
// push the result's local index.
//
// The ResultType may differ from the input type. For example, i64.eqz takes
// an i64 but produces an i32 result (0 or 1).
type UnaryOpHandler struct {
	Opcode     byte
	ResultType wasm.ValType
}

func (h UnaryOpHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	operand := ctx.Stack.Pop()
	tmp := ctx.AllocTemp(h.ResultType)

	ctx.Emit.LocalGet(operand).EmitRawOpcode(h.Opcode).LocalSet(tmp)
	ctx.Stack.Push(tmp, h.ResultType)

	return nil
}

// StackEffect implements StackEffecter.
func (h UnaryOpHandler) StackEffect() StackEffect {
	return StackEffect{Pops: 1, Pushes: []wasm.ValType{h.ResultType}}
}

// SelectHandler implements WebAssembly's conditional select operation.
//
// Select is a ternary operator: it takes three values (true-value, false-value,
// condition) and returns one based on whether the condition is non-zero.
// It's similar to C's ternary operator: condition ? true-val : false-val
//
// The stack order is: [bottom] true-val, false-val, condition [top]
// So we pop condition first, then false-val (with its type), then true-val.
//
// We use PopTyped for false-val to get its type, which becomes the result type.
// Both input values must have the same type, so we use that type for our
// result temporary.
type SelectHandler struct{}

func (h SelectHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	cond := ctx.Stack.Pop()
	falseVal := ctx.Stack.PopTyped()
	trueVal := ctx.Stack.Pop()

	tmp := ctx.AllocTemp(falseVal.Type)

	ctx.Emit.LocalGet(trueVal).LocalGet(falseVal.LocalIdx).LocalGet(cond).Select().LocalSet(tmp)
	ctx.Stack.Push(tmp, falseVal.Type)

	return nil
}

// SelectTypeHandler implements the typed select operation (select t*).
//
// This is identical to SelectHandler but uses the type from the immediate
// rather than inferring it from the stack. The immediate contains a vector
// of value types (typically just one).
type SelectTypeHandler struct{}

func (h SelectTypeHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	cond := ctx.Stack.Pop()
	falseVal := ctx.Stack.PopTyped()
	trueVal := ctx.Stack.Pop()

	// Use the type from the immediate if available, otherwise use stack type
	resultType := falseVal.Type
	if imm, ok := instr.Imm.(wasm.SelectTypeImm); ok && len(imm.Types) > 0 {
		resultType = imm.Types[0]
	}

	tmp := ctx.AllocTemp(resultType)

	ctx.Emit.LocalGet(trueVal).LocalGet(falseVal.LocalIdx).LocalGet(cond).EmitInstr(instr).LocalSet(tmp)
	ctx.Stack.Push(tmp, resultType)

	return nil
}

// RegisterArithmeticHandlers adds handlers for all arithmetic, comparison,
// bitwise, and related operations. This is the bulk of WebAssembly's
// computational instructions, covering all four value types (i32, i64, f32, f64).
func RegisterArithmeticHandlers(r *Registry) {
	// i32 arithmetic: standard integer math operations
	r.Register(wasm.OpI32Add, BinaryOpHandler{wasm.OpI32Add, wasm.ValI32}, "i32.add")
	r.Register(wasm.OpI32Sub, BinaryOpHandler{wasm.OpI32Sub, wasm.ValI32}, "i32.sub")
	r.Register(wasm.OpI32Mul, BinaryOpHandler{wasm.OpI32Mul, wasm.ValI32}, "i32.mul")
	r.Register(wasm.OpI32DivS, BinaryOpHandler{wasm.OpI32DivS, wasm.ValI32}, "i32.div_s")
	r.Register(wasm.OpI32DivU, BinaryOpHandler{wasm.OpI32DivU, wasm.ValI32}, "i32.div_u")
	r.Register(wasm.OpI32RemS, BinaryOpHandler{wasm.OpI32RemS, wasm.ValI32}, "i32.rem_s")
	r.Register(wasm.OpI32RemU, BinaryOpHandler{wasm.OpI32RemU, wasm.ValI32}, "i32.rem_u")

	// i32 bitwise: logical and shift operations
	r.Register(wasm.OpI32And, BinaryOpHandler{wasm.OpI32And, wasm.ValI32}, "i32.and")
	r.Register(wasm.OpI32Or, BinaryOpHandler{wasm.OpI32Or, wasm.ValI32}, "i32.or")
	r.Register(wasm.OpI32Xor, BinaryOpHandler{wasm.OpI32Xor, wasm.ValI32}, "i32.xor")
	r.Register(wasm.OpI32Shl, BinaryOpHandler{wasm.OpI32Shl, wasm.ValI32}, "i32.shl")
	r.Register(wasm.OpI32ShrS, BinaryOpHandler{wasm.OpI32ShrS, wasm.ValI32}, "i32.shr_s")
	r.Register(wasm.OpI32ShrU, BinaryOpHandler{wasm.OpI32ShrU, wasm.ValI32}, "i32.shr_u")
	r.Register(wasm.OpI32Rotl, BinaryOpHandler{wasm.OpI32Rotl, wasm.ValI32}, "i32.rotl")
	r.Register(wasm.OpI32Rotr, BinaryOpHandler{wasm.OpI32Rotr, wasm.ValI32}, "i32.rotr")

	// i32 bit counting: unary operations that analyze bit patterns
	r.Register(wasm.OpI32Clz, UnaryOpHandler{wasm.OpI32Clz, wasm.ValI32}, "i32.clz")
	r.Register(wasm.OpI32Ctz, UnaryOpHandler{wasm.OpI32Ctz, wasm.ValI32}, "i32.ctz")
	r.Register(wasm.OpI32Popcnt, UnaryOpHandler{wasm.OpI32Popcnt, wasm.ValI32}, "i32.popcnt")

	// i32 comparison: all produce i32 result (0 or 1)
	r.Register(wasm.OpI32Eqz, UnaryOpHandler{wasm.OpI32Eqz, wasm.ValI32}, "i32.eqz")
	r.Register(wasm.OpI32Eq, BinaryOpHandler{wasm.OpI32Eq, wasm.ValI32}, "i32.eq")
	r.Register(wasm.OpI32Ne, BinaryOpHandler{wasm.OpI32Ne, wasm.ValI32}, "i32.ne")
	r.Register(wasm.OpI32LtS, BinaryOpHandler{wasm.OpI32LtS, wasm.ValI32}, "i32.lt_s")
	r.Register(wasm.OpI32LtU, BinaryOpHandler{wasm.OpI32LtU, wasm.ValI32}, "i32.lt_u")
	r.Register(wasm.OpI32GtS, BinaryOpHandler{wasm.OpI32GtS, wasm.ValI32}, "i32.gt_s")
	r.Register(wasm.OpI32GtU, BinaryOpHandler{wasm.OpI32GtU, wasm.ValI32}, "i32.gt_u")
	r.Register(wasm.OpI32LeS, BinaryOpHandler{wasm.OpI32LeS, wasm.ValI32}, "i32.le_s")
	r.Register(wasm.OpI32LeU, BinaryOpHandler{wasm.OpI32LeU, wasm.ValI32}, "i32.le_u")
	r.Register(wasm.OpI32GeS, BinaryOpHandler{wasm.OpI32GeS, wasm.ValI32}, "i32.ge_s")
	r.Register(wasm.OpI32GeU, BinaryOpHandler{wasm.OpI32GeU, wasm.ValI32}, "i32.ge_u")

	// i64 arithmetic
	r.Register(wasm.OpI64Add, BinaryOpHandler{wasm.OpI64Add, wasm.ValI64}, "i64.add")
	r.Register(wasm.OpI64Sub, BinaryOpHandler{wasm.OpI64Sub, wasm.ValI64}, "i64.sub")
	r.Register(wasm.OpI64Mul, BinaryOpHandler{wasm.OpI64Mul, wasm.ValI64}, "i64.mul")
	r.Register(wasm.OpI64DivS, BinaryOpHandler{wasm.OpI64DivS, wasm.ValI64}, "i64.div_s")
	r.Register(wasm.OpI64DivU, BinaryOpHandler{wasm.OpI64DivU, wasm.ValI64}, "i64.div_u")
	r.Register(wasm.OpI64RemS, BinaryOpHandler{wasm.OpI64RemS, wasm.ValI64}, "i64.rem_s")
	r.Register(wasm.OpI64RemU, BinaryOpHandler{wasm.OpI64RemU, wasm.ValI64}, "i64.rem_u")

	// i64 bitwise
	r.Register(wasm.OpI64And, BinaryOpHandler{wasm.OpI64And, wasm.ValI64}, "i64.and")
	r.Register(wasm.OpI64Or, BinaryOpHandler{wasm.OpI64Or, wasm.ValI64}, "i64.or")
	r.Register(wasm.OpI64Xor, BinaryOpHandler{wasm.OpI64Xor, wasm.ValI64}, "i64.xor")
	r.Register(wasm.OpI64Shl, BinaryOpHandler{wasm.OpI64Shl, wasm.ValI64}, "i64.shl")
	r.Register(wasm.OpI64ShrS, BinaryOpHandler{wasm.OpI64ShrS, wasm.ValI64}, "i64.shr_s")
	r.Register(wasm.OpI64ShrU, BinaryOpHandler{wasm.OpI64ShrU, wasm.ValI64}, "i64.shr_u")
	r.Register(wasm.OpI64Rotl, BinaryOpHandler{wasm.OpI64Rotl, wasm.ValI64}, "i64.rotl")
	r.Register(wasm.OpI64Rotr, BinaryOpHandler{wasm.OpI64Rotr, wasm.ValI64}, "i64.rotr")

	// i64 bit counting
	r.Register(wasm.OpI64Clz, UnaryOpHandler{wasm.OpI64Clz, wasm.ValI64}, "i64.clz")
	r.Register(wasm.OpI64Ctz, UnaryOpHandler{wasm.OpI64Ctz, wasm.ValI64}, "i64.ctz")
	r.Register(wasm.OpI64Popcnt, UnaryOpHandler{wasm.OpI64Popcnt, wasm.ValI64}, "i64.popcnt")

	// i64 comparison: note these produce i32 results, not i64
	r.Register(wasm.OpI64Eqz, UnaryOpHandler{wasm.OpI64Eqz, wasm.ValI32}, "i64.eqz")
	r.Register(wasm.OpI64Eq, BinaryOpHandler{wasm.OpI64Eq, wasm.ValI32}, "i64.eq")
	r.Register(wasm.OpI64Ne, BinaryOpHandler{wasm.OpI64Ne, wasm.ValI32}, "i64.ne")
	r.Register(wasm.OpI64LtS, BinaryOpHandler{wasm.OpI64LtS, wasm.ValI32}, "i64.lt_s")
	r.Register(wasm.OpI64LtU, BinaryOpHandler{wasm.OpI64LtU, wasm.ValI32}, "i64.lt_u")
	r.Register(wasm.OpI64GtS, BinaryOpHandler{wasm.OpI64GtS, wasm.ValI32}, "i64.gt_s")
	r.Register(wasm.OpI64GtU, BinaryOpHandler{wasm.OpI64GtU, wasm.ValI32}, "i64.gt_u")
	r.Register(wasm.OpI64LeS, BinaryOpHandler{wasm.OpI64LeS, wasm.ValI32}, "i64.le_s")
	r.Register(wasm.OpI64LeU, BinaryOpHandler{wasm.OpI64LeU, wasm.ValI32}, "i64.le_u")
	r.Register(wasm.OpI64GeS, BinaryOpHandler{wasm.OpI64GeS, wasm.ValI32}, "i64.ge_s")
	r.Register(wasm.OpI64GeU, BinaryOpHandler{wasm.OpI64GeU, wasm.ValI32}, "i64.ge_u")

	// f32 arithmetic
	r.Register(wasm.OpF32Add, BinaryOpHandler{wasm.OpF32Add, wasm.ValF32}, "f32.add")
	r.Register(wasm.OpF32Sub, BinaryOpHandler{wasm.OpF32Sub, wasm.ValF32}, "f32.sub")
	r.Register(wasm.OpF32Mul, BinaryOpHandler{wasm.OpF32Mul, wasm.ValF32}, "f32.mul")
	r.Register(wasm.OpF32Div, BinaryOpHandler{wasm.OpF32Div, wasm.ValF32}, "f32.div")
	r.Register(wasm.OpF32Min, BinaryOpHandler{wasm.OpF32Min, wasm.ValF32}, "f32.min")
	r.Register(wasm.OpF32Max, BinaryOpHandler{wasm.OpF32Max, wasm.ValF32}, "f32.max")
	r.Register(wasm.OpF32Copysign, BinaryOpHandler{wasm.OpF32Copysign, wasm.ValF32}, "f32.copysign")

	// f32 unary: float-specific operations
	r.Register(wasm.OpF32Abs, UnaryOpHandler{wasm.OpF32Abs, wasm.ValF32}, "f32.abs")
	r.Register(wasm.OpF32Neg, UnaryOpHandler{wasm.OpF32Neg, wasm.ValF32}, "f32.neg")
	r.Register(wasm.OpF32Ceil, UnaryOpHandler{wasm.OpF32Ceil, wasm.ValF32}, "f32.ceil")
	r.Register(wasm.OpF32Floor, UnaryOpHandler{wasm.OpF32Floor, wasm.ValF32}, "f32.floor")
	r.Register(wasm.OpF32Trunc, UnaryOpHandler{wasm.OpF32Trunc, wasm.ValF32}, "f32.trunc")
	r.Register(wasm.OpF32Nearest, UnaryOpHandler{wasm.OpF32Nearest, wasm.ValF32}, "f32.nearest")
	r.Register(wasm.OpF32Sqrt, UnaryOpHandler{wasm.OpF32Sqrt, wasm.ValF32}, "f32.sqrt")

	// f32 comparison: produce i32 results
	r.Register(wasm.OpF32Eq, BinaryOpHandler{wasm.OpF32Eq, wasm.ValI32}, "f32.eq")
	r.Register(wasm.OpF32Ne, BinaryOpHandler{wasm.OpF32Ne, wasm.ValI32}, "f32.ne")
	r.Register(wasm.OpF32Lt, BinaryOpHandler{wasm.OpF32Lt, wasm.ValI32}, "f32.lt")
	r.Register(wasm.OpF32Gt, BinaryOpHandler{wasm.OpF32Gt, wasm.ValI32}, "f32.gt")
	r.Register(wasm.OpF32Le, BinaryOpHandler{wasm.OpF32Le, wasm.ValI32}, "f32.le")
	r.Register(wasm.OpF32Ge, BinaryOpHandler{wasm.OpF32Ge, wasm.ValI32}, "f32.ge")

	// f64 arithmetic
	r.Register(wasm.OpF64Add, BinaryOpHandler{wasm.OpF64Add, wasm.ValF64}, "f64.add")
	r.Register(wasm.OpF64Sub, BinaryOpHandler{wasm.OpF64Sub, wasm.ValF64}, "f64.sub")
	r.Register(wasm.OpF64Mul, BinaryOpHandler{wasm.OpF64Mul, wasm.ValF64}, "f64.mul")
	r.Register(wasm.OpF64Div, BinaryOpHandler{wasm.OpF64Div, wasm.ValF64}, "f64.div")
	r.Register(wasm.OpF64Min, BinaryOpHandler{wasm.OpF64Min, wasm.ValF64}, "f64.min")
	r.Register(wasm.OpF64Max, BinaryOpHandler{wasm.OpF64Max, wasm.ValF64}, "f64.max")
	r.Register(wasm.OpF64Copysign, BinaryOpHandler{wasm.OpF64Copysign, wasm.ValF64}, "f64.copysign")

	// f64 unary
	r.Register(wasm.OpF64Abs, UnaryOpHandler{wasm.OpF64Abs, wasm.ValF64}, "f64.abs")
	r.Register(wasm.OpF64Neg, UnaryOpHandler{wasm.OpF64Neg, wasm.ValF64}, "f64.neg")
	r.Register(wasm.OpF64Ceil, UnaryOpHandler{wasm.OpF64Ceil, wasm.ValF64}, "f64.ceil")
	r.Register(wasm.OpF64Floor, UnaryOpHandler{wasm.OpF64Floor, wasm.ValF64}, "f64.floor")
	r.Register(wasm.OpF64Trunc, UnaryOpHandler{wasm.OpF64Trunc, wasm.ValF64}, "f64.trunc")
	r.Register(wasm.OpF64Nearest, UnaryOpHandler{wasm.OpF64Nearest, wasm.ValF64}, "f64.nearest")
	r.Register(wasm.OpF64Sqrt, UnaryOpHandler{wasm.OpF64Sqrt, wasm.ValF64}, "f64.sqrt")

	// f64 comparison
	r.Register(wasm.OpF64Eq, BinaryOpHandler{wasm.OpF64Eq, wasm.ValI32}, "f64.eq")
	r.Register(wasm.OpF64Ne, BinaryOpHandler{wasm.OpF64Ne, wasm.ValI32}, "f64.ne")
	r.Register(wasm.OpF64Lt, BinaryOpHandler{wasm.OpF64Lt, wasm.ValI32}, "f64.lt")
	r.Register(wasm.OpF64Gt, BinaryOpHandler{wasm.OpF64Gt, wasm.ValI32}, "f64.gt")
	r.Register(wasm.OpF64Le, BinaryOpHandler{wasm.OpF64Le, wasm.ValI32}, "f64.le")
	r.Register(wasm.OpF64Ge, BinaryOpHandler{wasm.OpF64Ge, wasm.ValI32}, "f64.ge")

	// select: conditional value selection
	r.Register(wasm.OpSelect, SelectHandler{}, "select")
	r.Register(wasm.OpSelectType, SelectTypeHandler{}, "select_t")
}
