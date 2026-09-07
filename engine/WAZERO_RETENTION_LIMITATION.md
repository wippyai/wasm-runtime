# Wazero 1.12 Compiler Engine Retention Limitation & Deterministic Lifecycle Verification

**Location:** `engine/WAZERO_RETENTION_LIMITATION.md`
**Status:** Canonical Reference
**Scope:** Wazero v1.12.0 Compiler Engine interaction with Wippy component and bridge lifecycle

---

## 1. Executive Summary

During investigation of intermittent test failures in [`TestBridgeModuleCleanup`](file:///home/wolfy-j/wippy/wasm/.worktrees/actor-runtime-perf/engine/linker_leak_test.go#L110-L280), an upstream memory retention behavior was identified in the stock Wazero v1.12.0 compiler engine (`internal/engine/wazevo`).

Specifically:
- Stock Wazero retains strong references to closed compiled modules in the backing array of `e.sortedCompiledModules` between `len` and `cap`.
- This creates an active GC root for the lifetime of the Wazero engine, retaining ~1,376 KB of compiled module AST, bytecode, and JIT executable metadata after instance and module closure.
- Standalone reproductions using **zero Wippy code** ([`/tmp/w1-stock-wazero-retention/`](file:///tmp/w1-stock-wazero-retention/)) confirmed that this retention occurs purely inside stock Wazero during compilation/closure, plateaus across consecutive cycles (bounded retention, not an unbounded leak), and is cleanly reclaimed upon `engine.Close()`.
- The legacy test assertion relied on a fragile whole-process heap delta threshold (`retained > 64 KB && retained > instanceMemory/2`), which conflated guest instance/bridge lifecycle with engine-level compiler metadata lifecycle, failing intermittently (4/30 runs or 13.33%).
- **Policy Decision:** We document this limitation honestly. Unsafe pointer arithmetic or private reflection into Wazero internals is strictly forbidden. Stock Wazero is kept untouched without forks or monkey patches. Relaxing heap thresholds is rejected.
- **Solution:** Wippy asserts guest instance, core module, and bridge module lifecycle **deterministically** via runtime module registrations, bridge reference counting, instance reference clearing, and linear memory detachment, while asserting compiler memory reclamation at the true architectural boundary: `engine.Close()`.

---

## 2. Upstream Root Cause in Stock Wazero v1.12.0

### A. Code Location
In stock Wazero (`github.com/tetratelabs/wazero/internal/engine/wazevo/engine.go`, lines 691–702):

```go
// deleteCompiledModuleFromSortedList removes a compiled module from the sorted list.
func (e *engine) deleteCompiledModuleFromSortedList(cm *compiledModule) {
    ptr := uintptr(unsafe.Pointer(&cm.executable[0]))

    index := sort.Search(len(e.sortedCompiledModules), func(i int) bool {
        return uintptr(unsafe.Pointer(&e.sortedCompiledModules[i].executable[0])) >= ptr
    })
    if index >= len(e.sortedCompiledModules) {
        return
    }
    copy(e.sortedCompiledModules[index:], e.sortedCompiledModules[index+1:])
    e.sortedCompiledModules = e.sortedCompiledModules[:len(e.sortedCompiledModules)-1]
}
```

### B. The Slice Truncation Pointer Retention Defect
1. Wazero shifts elements left using `copy` to overwrite the target entry at `index`.
2. It truncates the slice header length via `e.sortedCompiledModules = e.sortedCompiledModules[:len(e.sortedCompiledModules)-1]`.
3. **The defect:** The slot at `len-1` prior to truncation is **never set to nil** (`e.sortedCompiledModules[len-1] = nil` is omitted).
4. Under Go runtime garbage collection semantics, slice backing arrays are scanned up to their allocated capacity (`cap`), not merely their active length (`len`).
5. Because `e.sortedCompiledModules` is a field on the long-lived `wazevo.engine` struct, any pointer in slots `[len : cap]` remains an active GC root for the engine's lifetime.
6. The retained `*compiledModule` holds:
   - `module *wasm.Module` (parsed AST, section data, passive data segments, function signatures).
   - `executables` (JIT machine code allocations).
   - Trampoline tables, source maps, and function offsets.

### C. Engine-Level Reclamation
When `engine.Close()` is called (line 646 of `wazevo/engine.go`):
```go
func (e *engine) Close() (err error) {
    e.mux.Lock()
    defer e.mux.Unlock()
    e.sortedCompiledModules = nil
    e.compiledModules = nil
    e.sharedFunctions = nil
    return nil
}
```
`e.sortedCompiledModules` is set to `nil`, severing the reference to the backing array and allowing the Go GC to reclaim all compiled module allocations back to baseline.

---

## 3. Empirical Evidence & Diagnostic Audit Summary

### A. Baseline Failure Rate
In 30 baseline executions of [`TestBridgeModuleCleanup`](file:///home/wolfy-j/wippy/wasm/.worktrees/actor-runtime-perf/engine/linker_leak_test.go#L110-L280):
- **26 passed, 4 failed** (failure frequency: **13.33%**).
- In all 4 failures, the retained heap allocation was identically **1,376 KB**.
- Intermittent failures arose because slot addresses in slice capacity shifted across warmup runs and separate test process invocations.

### B. Stepwise Diagnostics (Worker 77928)
1. **Pprof Heap Analysis:** Profiling showed 75.91% of retained memory in `wazero/internal/wasm.NewMemoryInstance` / `buildMemory` and 14.65% in `wazevo.(*callEngine).init`.
2. **Field-by-field Nil Isolation (`diag18`):**
   - Nil-ing `compiledModules` produced no change (1,375 KB still retained).
   - Nil-ing `sortedCompiledModules` dropped retained memory immediately from 1,375 KB to -11 KB.
3. **Slice Backing Array Inspection (`diag19`):**
   Inspecting the backing array of `sortedCompiledModules` revealed `len=3, cap=32`. Slots 3 through 16 (beyond `len`) retained valid pointers to compiled modules.
4. **Zero-Tail Diagnostic Proof (`diag20`):**
   Zeroing vacated capacity (`fullSlice[len:] = nil`) across 10 consecutive runs resulted in **10/10 PASSES (0% failure)**, confirming the exact root cause.

### C. Independent Root Proof (Pure Stock Wazero, Zero Wippy Code)
In [`/tmp/w1-stock-wazero-retention/`](file:///tmp/w1-stock-wazero-retention/):
- A standalone Go program compiled and closed a 16 MiB module with stock Wazero v1.12.0 without instantiating any guest instances.
- Result:
  - Baseline `HeapAlloc`: 717 KB
  - After 1 closed module: 17.5 MB (~16 MB retained)
  - After 21 closed modules: 17.5 MB (bounded plateau, not an unbounded leak)
  - After `runtime.Close()`: 736 KB (clean release back to baseline)

---

## 4. Engineering Policy & Architectural Principles

1. **Strict Prohibition on Unsafe Reflection / Pointer Hacks:**
   Diagnostic zeroing of unexported Wazero struct fields using `unsafe.Pointer` breaks Go ABI stability, violates memory safety guarantees, and risks undefined behavior or crashes across Go or Wazero patch releases. It is strictly forbidden in production and tests.
2. **Stock Wazero Kept Untouched:**
   Wippy maintains standard dependencies against upstream `github.com/tetratelabs/wazero`. We do not maintain an unsafe private fork for upstream compiler slice truncation. The issue is documented and reported upstream.
3. **Rejection of Memory Threshold Relaxation:**
   Arbitrarily increasing memory tolerance (e.g. from 64 KB to 2 MB) is rejected. Relaxing thresholds conceals real memory leaks and does not provide verification of actual cleanup.
4. **Clear Lifecycle Separation:**
   - **Guest Instance & Bridge Lifecycle (Owned by Wippy):** Wippy manages core module instances, synthetic bridge modules, reference counts, linear memory wrappers, resource stores, and dispatch bindings. These must be closed, deregistered, and zeroed deterministically on instance closure.
   - **Compiler Engine Metadata Lifecycle (Owned by Wazero):** Wazero manages AST parsing, code generation, and module metadata. These are bound to the `wazero.Runtime` / `engine` lifecycle and are reclaimed at `engine.Close()`.

---

## 5. Deterministic Lifecycle Verification Implementation

In [`engine/linker_leak_test.go`](file:///home/wolfy-j/wippy/wasm/.worktrees/actor-runtime-perf/engine/linker_leak_test.go):

### A. [`instanceCleanupViolations`](file:///home/wolfy-j/wippy/wasm/.worktrees/actor-runtime-perf/engine/linker_leak_test.go#L20-L95)
A deterministic inspector that verifies all resource invariants:
1. **Core Module Deregistration:** Every instance-scoped module (e.g. `$0#1`, `$3#1`) must be deregistered from Wazero runtime (`engine.runtime.Module(name) == nil`).
2. **Bridge Module Lifecycle:**
   - Surviving instances sharing the bridge: `engine.runtime.Module(bridgeName) != nil`.
   - Final instance closed: `engine.runtime.Module(bridgeName) == nil`.
3. **Instance Pointer Zeroing:** `inst.linkerInst`, `inst.instance`, `inst.memory`, `inst.allocFn`, `inst.freeFn`, `inst.alloc`, `inst.resources`, `inst.exportBindings`, and cache maps must all be `nil`.
4. **Linear Memory Detachment:** `inst.HasMemory() == false` and `inst.MemorySize() == 0`.

### B. [`TestBridgeModuleCleanup`](file:///home/wolfy-j/wippy/wasm/.worktrees/actor-runtime-perf/engine/linker_leak_test.go#L110-L280)
- **`SingleInstanceLifecycleAndTeardown`:** Tests single-instance instantiation, runtime presence, execution, closure, reference clearing, and post-close call rejection.
- **`MultiInstanceSurvivorBridgeRetentionAndTeardown`:** Tests that when two instances share a bridge, closing instance 1 preserves the bridge in runtime and leaves survivor instance 2 fully operational. Closing instance 2 triggers final release and deregistration from runtime.
- **`ReinstantiationAfterAllClosed`:** Verifies that instantiating a new instance after all previous instances have closed cleanly recreates the bridge module and executes properly.

### C. [`TestBridgeCleanup_FailsOnMissingCleanup`](file:///home/wolfy-j/wippy/wasm/.worktrees/actor-runtime-perf/engine/linker_leak_test.go#L285-L355)
- Directly asserts that `instanceCleanupViolations` detects unclosed core modules, unclosed bridge modules, retained linker instances, and retained memory flags when cleanup is omitted, ensuring that real Wippy cleanup regressions will cause test failure.

### D. [`TestEngineCloseMemoryReclamation`](file:///home/wolfy-j/wippy/wasm/.worktrees/actor-runtime-perf/engine/linker_leak_test.go#L360-L420)
- Asserts compiler memory reclamation at the correct architectural boundary (`engine.Close()`), verifying that baseline heap memory is fully restored once the Wazero runtime and its compiler engine are closed.
