package engine

// ownedAsyncifyTestAllocator is a conventional guest allocator used only by
// component fixtures which opt in to automatic Asyncify. It grows the module's
// own memory before returning each aligned bump allocation, so a 64 KiB
// Asyncify reservation is backed by real guest-owned storage.
const ownedAsyncifyTestAllocator = `
    (global $asyncify_test_next (mut i32) (i32.const 32768))
    (func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32)
      (local $addr i32)
      (local $end i32)
      (local $needed_pages i32)
      (local $current_pages i32)
      (local $grown_from i32)
      (local.set $addr (global.get $asyncify_test_next))
      (local.set $end (i32.add (local.get $addr) (local.get 3)))
      (local.set $needed_pages
        (i32.div_u
          (i32.add (local.get $end) (i32.const 65535))
          (i32.const 65536)))
      (local.set $current_pages (memory.size))
      (if (i32.lt_u (local.get $current_pages) (local.get $needed_pages))
        (then
          (local.set $grown_from
            (memory.grow
              (i32.sub (local.get $needed_pages) (local.get $current_pages))))
          (if (i32.eq (local.get $grown_from) (i32.const -1))
            (then (return (i32.const 0))))))
      (global.set $asyncify_test_next (local.get $end))
      (local.get $addr))
`

const ownedAsyncifyTestMemory = `(memory (export "memory") 1)` + ownedAsyncifyTestAllocator
