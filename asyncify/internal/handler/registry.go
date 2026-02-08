package handler

import (
	"github.com/wippyai/wasm-runtime/asyncify/internal/codegen"
	"github.com/wippyai/wasm-runtime/wasm"
)

// Handler is the interface for instruction transformation.
//
// Handlers are stateless and can be shared across multiple transformations.
// All mutable state is passed via Context.
//
// Handlers should:
//   - Update the simulated stack appropriately
//   - Emit transformed bytecode via ctx.Emit
//   - Allocate locals via ctx.AllocTemp if needed
//   - Return nil on success, error on failure
type Handler interface {
	// Handle transforms a single instruction within the given context.
	//
	// The handler should emit the transformed bytecode and update
	// stack/local state as appropriate.
	Handle(ctx *Context, instr wasm.Instruction) error
}

// StackEffect describes the stack signature of an instruction.
type StackEffect struct {
	Pushes []wasm.ValType
	Pops   int
}

// StackEffecter is optionally implemented by handlers with static stack effects.
// This allows the simulation pass to query handlers for their stack behavior
// instead of maintaining a separate stack effects table.
type StackEffecter interface {
	StackEffect() StackEffect
}

// StackEffectWith is for handlers with instruction-dependent stack effects.
// Used by prefix handlers (SIMD, Misc, GC) where the effect depends on sub-opcode.
type StackEffectWith interface {
	StackEffectWith(instr wasm.Instruction) *StackEffect
}

// Func is an adapter to use ordinary functions as Handlers.
//
// Example:
//
//	r.Register(wasm.OpNop, handler.Func(func(ctx *Context, instr wasm.Instruction) error {
//	    ctx.Emit.Nop()
//	    return nil
//	}))
type Func func(ctx *Context, instr wasm.Instruction) error

// Handle implements Handler.
func (f Func) Handle(ctx *Context, instr wasm.Instruction) error {
	return f(ctx, instr)
}

// Registry maps opcodes to their handlers.
//
// The registry provides O(1) handler lookup by opcode. Handlers can be
// registered individually or in bulk. Missing handlers can be detected
// before transformation begins.
type Registry struct {
	handlers [256]Handler
	names    [256]string
}

// NewRegistry creates an empty Registry.
//
// All opcodes are initially unhandled. Use Register or RegisterBulk
// to add handlers.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a handler for a single opcode.
//
// If a handler was already registered for this opcode, it is replaced.
// The name is optional and used for debugging/error messages.
func (r *Registry) Register(opcode byte, h Handler, name string) {
	r.handlers[opcode] = h
	r.names[opcode] = name
}

// RegisterFunc registers a function as a handler for an opcode.
//
// Convenience wrapper around Register with Func.
func (r *Registry) RegisterFunc(opcode byte, fn func(*Context, wasm.Instruction) error, name string) {
	r.Register(opcode, Func(fn), name)
}

// RegisterBulk registers the same handler for multiple opcodes.
//
// Useful for passthrough handlers that handle many similar opcodes.
func (r *Registry) RegisterBulk(opcodes []byte, h Handler, name string) {
	for _, op := range opcodes {
		r.handlers[op] = h
		r.names[op] = name
	}
}

// Get returns the handler for an opcode, or nil if not registered.
func (r *Registry) Get(opcode byte) Handler {
	return r.handlers[opcode]
}

// Has returns true if a handler is registered for the opcode.
func (r *Registry) Has(opcode byte) bool {
	return r.handlers[opcode] != nil
}

// Name returns the name of the handler for an opcode.
func (r *Registry) Name(opcode byte) string {
	return r.names[opcode]
}

// MissingHandlers returns opcodes that have no registered handler.
//
// Use this to verify all expected opcodes are handled before
// transformation.
func (r *Registry) MissingHandlers(opcodes []byte) []byte {
	var missing []byte
	for _, op := range opcodes {
		if r.handlers[op] == nil {
			missing = append(missing, op)
		}
	}
	return missing
}

// StackEntry represents a value on the simulated stack.
//
// During asyncify transformation, we flatten the WASM stack to locals.
// Each stack entry tracks which local holds the value and its type.
type StackEntry struct {
	LocalIdx uint32
	Type     wasm.ValType
}

// Stack is a simulated value stack for flattening.
//
// The asyncify transformation flattens stack operations to explicit
// local variables. Stack tracks what local holds each stack position.
type Stack struct {
	entries  []StackEntry
	fallback uint32
}

// NewStack creates a new Stack with the given fallback local.
//
// The fallback is returned when popping from an empty stack.
// This handles edge cases in unreachable code paths.
func NewStack(fallback uint32) *Stack {
	return &Stack{fallback: fallback}
}

// Push adds a value to the top of the stack.
func (s *Stack) Push(localIdx uint32, valType wasm.ValType) {
	s.entries = append(s.entries, StackEntry{LocalIdx: localIdx, Type: valType})
}

// PushI32 is a convenience for pushing an i32 value.
func (s *Stack) PushI32(localIdx uint32) {
	s.Push(localIdx, wasm.ValI32)
}

// Pop removes and returns the top local index.
//
// If the stack is empty, returns the fallback local.
func (s *Stack) Pop() uint32 {
	if len(s.entries) == 0 {
		return s.fallback
	}
	e := s.entries[len(s.entries)-1]
	s.entries = s.entries[:len(s.entries)-1]
	return e.LocalIdx
}

// PopTyped removes and returns the top entry with type.
//
// If the stack is empty, returns (fallback, i32).
func (s *Stack) PopTyped() StackEntry {
	if len(s.entries) == 0 {
		return StackEntry{LocalIdx: s.fallback, Type: wasm.ValI32}
	}
	e := s.entries[len(s.entries)-1]
	s.entries = s.entries[:len(s.entries)-1]
	return e
}

// Peek returns the top entry without removing it.
//
// If the stack is empty, returns (fallback, i32).
func (s *Stack) Peek() StackEntry {
	if len(s.entries) == 0 {
		return StackEntry{LocalIdx: s.fallback, Type: wasm.ValI32}
	}
	return s.entries[len(s.entries)-1]
}

// Len returns the current stack depth.
func (s *Stack) Len() int {
	return len(s.entries)
}

// IsEmpty returns true if the stack has no entries.
func (s *Stack) IsEmpty() bool {
	return len(s.entries) == 0
}

// Clear removes all entries from the stack.
func (s *Stack) Clear() {
	s.entries = s.entries[:0]
}

// Locals manages local variable allocation during transformation.
//
// Original locals (parameters + declared locals) have known types.
// Temp locals are allocated on demand for flattening.
type Locals struct {
	body           *wasm.FuncBody
	types          []wasm.ValType
	nextIdx        uint32
	preDeclaredMax uint32
}

// NewLocals creates a Locals manager.
//
// startIdx should be the first available local index (after params + declared).
// initialTypes are the types of params + declared locals.
func NewLocals(startIdx uint32, body *wasm.FuncBody, initialTypes []wasm.ValType) *Locals {
	types := make([]wasm.ValType, len(initialTypes))
	copy(types, initialTypes)
	return &Locals{
		nextIdx:        startIdx,
		preDeclaredMax: uint32(len(initialTypes)),
		body:           body,
		types:          types,
	}
}

// Alloc allocates a new local of the specified type.
//
// Returns the local index. For pre-declared locals, only returns the index
// without adding to body.Locals. For new locals, also adds to body.
func (l *Locals) Alloc(valType wasm.ValType) uint32 {
	idx := l.nextIdx
	l.nextIdx++

	// Fast path: use pre-declared locals only when the predicted type matches.
	if idx < l.preDeclaredMax {
		if int(idx) < len(l.types) && l.types[idx] == valType {
			return idx
		}
		// Dry-run stack simulation can diverge from real emission around complex
		// control flow. Never reuse a pre-declared slot with a mismatched type.
		// Fall through to allocate a fresh local at the end.
	}

	// Allocate at the end to keep type soundness even if nextIdx drifted due
	// pre-declared mismatch fallback above.
	idx = uint32(len(l.types))
	l.nextIdx = idx + 1
	l.body.Locals = append(l.body.Locals, wasm.LocalEntry{Count: 1, ValType: valType})
	l.types = append(l.types, valType)

	if idx >= l.preDeclaredMax {
		return idx
	}
	return idx
}

// AllocI32 allocates a new i32 local.
func (l *Locals) AllocI32() uint32 {
	return l.Alloc(wasm.ValI32)
}

// TypeOf returns the type of a local, or i32 if out of range.
func (l *Locals) TypeOf(localIdx uint32) wasm.ValType {
	if int(localIdx) < len(l.types) {
		return l.types[localIdx]
	}
	return wasm.ValI32
}

// NextIdx returns the next local index that would be allocated.
func (l *Locals) NextIdx() uint32 {
	return l.nextIdx
}

// Context provides shared state for handlers during transformation.
//
// Handlers access the emitter, stack, locals, and global indices
// through the Context. This centralizes state management and
// enables testing handlers in isolation.
type Context struct {
	Emit         *codegen.Emitter
	Stack        *Stack
	Locals       *Locals
	Module       *wasm.Module
	ControlDepth int
	StateGlobal  uint32
	DataGlobal   uint32
}

// NewContext creates a Context for transformation.
//
// The emitter, stack, and locals should be pre-configured for the
// function being transformed.
func NewContext(emit *codegen.Emitter, stack *Stack, locals *Locals, stateGlobal, dataGlobal uint32) *Context {
	return &Context{
		Emit:        emit,
		Stack:       stack,
		Locals:      locals,
		StateGlobal: stateGlobal,
		DataGlobal:  dataGlobal,
	}
}

// AllocTemp allocates a temporary local of the given type.
//
// Convenience wrapper for Locals.Alloc.
func (c *Context) AllocTemp(valType wasm.ValType) uint32 {
	return c.Locals.Alloc(valType)
}

// TypeOf returns the type of a local.
//
// Convenience wrapper for Locals.TypeOf.
func (c *Context) TypeOf(localIdx uint32) wasm.ValType {
	return c.Locals.TypeOf(localIdx)
}

// PushResult pushes a value onto the stack after allocation.
//
// This is the common pattern:
//  1. Allocate a temp local
//  2. Emit code that leaves value in that local
//  3. Push the local onto the stack
func (c *Context) PushResult(valType wasm.ValType, localIdx uint32) {
	c.Stack.Push(localIdx, valType)
}

// PopArg pops a value from the stack for use as an argument.
//
// Returns the local index holding the value.
func (c *Context) PopArg() uint32 {
	return c.Stack.Pop()
}
