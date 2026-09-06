(component
  (type $host_iface
    (instance
      (export "peek" (func (param "data" (list u8)) (result u32)))
      (export "mirror" (func (param "data" (list u8)) (result (list u8))))
    )
  )
  (import "test:canon/host@0.1.0" (instance $host (type $host_iface)))

  (core module $m
    (import "host" "peek" (func $host_peek (param i32 i32) (result i32)))
    (import "host" "mirror" (func $host_mirror (param i32 i32 i32)))
    (memory (export "memory") 1)
    (global $heap (mut i32) (i32.const 1024))
    (func (export "cabi_realloc") (param $old_ptr i32) (param $old_size i32) (param $align i32) (param $new_size i32) (result i32)
      (local $ret i32)
      (local.set $ret (global.get $heap))
      (global.set $heap (i32.add (local.get $ret) (local.get $new_size)))
      (local.get $ret)
    )
    (func (export "call-peek") (param $ptr i32) (param $len i32) (result i32)
      (call $host_peek (local.get $ptr) (local.get $len))
    )
    (func (export "call-mirror") (param $ptr i32) (param $len i32) (result i32)
      (local $retptr i32)
      (local.set $retptr (i32.const 16))
      (call $host_mirror (local.get $ptr) (local.get $len) (local.get $retptr))
      (local.get $retptr)
    )
  )

  (core module $shim
    (type $t (func (param i32 i32) (result i32)))
    (table (export "$imports") 2 2 funcref)
    (func $i0 (type $t) (param i32 i32) (result i32)
      local.get 0
      local.get 1
      i32.const 0
      call_indirect (type $t)
    )
    (type $mirror_t (func (param i32 i32 i32)))
    (func $i1 (type $mirror_t) (param i32 i32 i32)
      local.get 0
      local.get 1
      local.get 2
      i32.const 1
      call_indirect (type $mirror_t)
    )
    (export "0" (func $i0))
    (export "1" (func $i1))
  )

  (core module $fixup
    (type $t (func (param i32 i32) (result i32)))
    (import "" "0" (func $real0 (type $t)))
    (type $mirror_t (func (param i32 i32 i32)))
    (import "" "1" (func $real1 (type $mirror_t)))
    (import "" "$imports" (table 2 2 funcref))
    (elem (i32.const 0) func $real0 $real1)
  )

  (core instance $shim_inst (instantiate $shim))
  (alias core export $shim_inst "0" (core func $peek_stub))
  (alias core export $shim_inst "1" (core func $mirror_stub))
  (core instance $host_stubs
    (export "peek" (func $peek_stub))
    (export "mirror" (func $mirror_stub))
  )
  (core instance $m_inst (instantiate $m
    (with "host" (instance $host_stubs))
  ))

  (alias core export $m_inst "memory" (core memory $mem))
  (alias core export $m_inst "cabi_realloc" (core func $realloc))
  (alias core export $shim_inst "$imports" (core table $imports))
  (alias export $host "peek" (func $host_peek))
  (alias export $host "mirror" (func $host_mirror))

  (core func $peek_lowered (canon lower (func $host_peek) (memory $mem) (realloc $realloc)))
  (core func $mirror_lowered (canon lower (func $host_mirror) (memory $mem) (realloc $realloc)))

  (core instance $fixup_args
    (export "$imports" (table $imports))
    (export "0" (func $peek_lowered))
    (export "1" (func $mirror_lowered))
  )
  (core instance $fixup_inst (instantiate $fixup
    (with "" (instance $fixup_args))
  ))

  (alias core export $m_inst "call-peek" (core func $call_peek_core))
  (alias core export $m_inst "call-mirror" (core func $call_mirror_core))

  (type $peek_ty (func (param "data" (list u8)) (result u32)))
  (type $mirror_ty (func (param "data" (list u8)) (result (list u8))))
  (func $peek_func (type $peek_ty)
    (canon lift (core func $call_peek_core) (memory $mem) (realloc $realloc))
  )
  (func $mirror_func (type $mirror_ty)
    (canon lift (core func $call_mirror_core) (memory $mem) (realloc $realloc))
  )
  (export "call-peek" (func $peek_func))
  (export "call-mirror" (func $mirror_func))
)
