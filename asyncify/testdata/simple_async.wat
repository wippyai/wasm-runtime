(module
  (type (;0;) (func (param i32) (result i32)))
  (import "env" "get_value" (func $get_value (type 0)))
  (memory (;0;) 1)
  (export "test" (func 1))
  (func (;1;) (type 0) (param $n i32) (result i32)
    local.get $n
    i32.const 0
    i32.gt_s
    if (result i32)
      local.get $n
      call $get_value
    else
      i32.const 0
    end
  )
)
