(module
  (type (;0;) (func (param i32) (result i32)))
  (type (;1;) (func (param i32)))
  (type (;2;) (func))
  (type (;3;) (func (result i32)))
  (import "env" "get_value" (func (;0;) (type 0)))
  (memory (;0;) 1 1)
  (global (;0;) (mut i32) i32.const 0)
  (global (;1;) (mut i32) i32.const 0)
  (export "test" (func 1))
  (export "asyncify_start_unwind" (func 2))
  (export "asyncify_stop_unwind" (func 3))
  (export "asyncify_start_rewind" (func 4))
  (export "asyncify_stop_rewind" (func 5))
  (export "asyncify_get_state" (func 6))
  (func (;1;) (type 0) (param i32) (result i32)
    (local i32 i32 i32 i32 i32 i32 i32 i32 i32 i32 i32 i32)
    global.get 0
    i32.const 2
    i32.eq
    if ;; label = @1
      global.get 1
      global.get 1
      i32.load
      i32.const -16
      i32.add
      i32.store
      global.get 1
      i32.load
      local.set 11
      local.get 11
      i32.load
      local.set 3
      local.get 11
      i32.load offset=4
      local.set 4
      local.get 11
      i32.load offset=8
      local.set 5
      local.get 11
      i32.load offset=12
      local.set 7
    end
    block (result i32) ;; label = @1
      block ;; label = @2
        block ;; label = @3
          global.get 0
          i32.const 2
          i32.eq
          if ;; label = @4
            global.get 1
            global.get 1
            i32.load
            i32.const -4
            i32.add
            i32.store
            global.get 1
            i32.load
            i32.load
            local.set 9
          end
          global.get 0
          i32.const 0
          i32.eq
          if ;; label = @4
            local.get 0
            local.set 1
            local.get 1
            i32.const 0
            i32.gt_s
            local.set 2
          end
          nop
          global.get 0
          i32.const 0
          i32.eq
          if ;; label = @4
            local.get 2
            local.set 7
          end
          local.get 7
          global.get 0
          i32.const 2
          i32.eq
          i32.or
          if ;; label = @4
            global.get 0
            i32.const 0
            i32.eq
            if ;; label = @5
              local.get 0
              local.set 3
            end
            global.get 0
            i32.const 0
            i32.eq
            local.get 9
            i32.const 0
            i32.eq
            i32.or
            if ;; label = @5
              local.get 3
              call 0
              local.set 10
              global.get 0
              i32.const 1
              i32.eq
              if ;; label = @6
                i32.const 0
                br 5 (;@1;)
              else
                local.get 10
                local.set 4
              end
            end
            global.get 0
            i32.const 0
            i32.eq
            if ;; label = @5
              local.get 4
              local.set 5
            end
          end
          local.get 7
          i32.eqz
          global.get 0
          i32.const 2
          i32.eq
          i32.or
          if ;; label = @4
            global.get 0
            i32.const 0
            i32.eq
            if ;; label = @5
              i32.const 0
              local.set 5
            end
          end
          global.get 0
          i32.const 0
          i32.eq
          if ;; label = @4
            local.get 5
            local.set 6
            local.get 6
            return
          end
          nop
          unreachable
        end
        unreachable
      end
      unreachable
    end
    local.set 8
    global.get 1
    i32.load
    local.get 8
    i32.store
    global.get 1
    global.get 1
    i32.load
    i32.const 4
    i32.add
    i32.store
    global.get 1
    i32.load
    local.set 12
    local.get 12
    local.get 3
    i32.store
    local.get 12
    local.get 4
    i32.store offset=4
    local.get 12
    local.get 5
    i32.store offset=8
    local.get 12
    local.get 7
    i32.store offset=12
    global.get 1
    global.get 1
    i32.load
    i32.const 16
    i32.add
    i32.store
    i32.const 0
  )
  (func (;2;) (type 1) (param i32)
    i32.const 1
    global.set 0
    local.get 0
    global.set 1
    global.get 1
    i32.load
    global.get 1
    i32.load offset=4
    i32.gt_u
    if ;; label = @1
      unreachable
    end
  )
  (func (;3;) (type 2)
    i32.const 0
    global.set 0
    global.get 1
    i32.load
    global.get 1
    i32.load offset=4
    i32.gt_u
    if ;; label = @1
      unreachable
    end
  )
  (func (;4;) (type 1) (param i32)
    i32.const 2
    global.set 0
    local.get 0
    global.set 1
    global.get 1
    i32.load
    global.get 1
    i32.load offset=4
    i32.gt_u
    if ;; label = @1
      unreachable
    end
  )
  (func (;5;) (type 2)
    i32.const 0
    global.set 0
    global.get 1
    i32.load
    global.get 1
    i32.load offset=4
    i32.gt_u
    if ;; label = @1
      unreachable
    end
  )
  (func (;6;) (type 3) (result i32)
    global.get 0
  )
)
