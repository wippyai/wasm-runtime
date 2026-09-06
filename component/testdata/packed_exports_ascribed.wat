(component
  (core module $m
    (func (export "f") (result i32) i32.const 0)
  )
  (core instance $ci (instantiate $m))
  (alias core export $ci "f" (core func $f))
  (type $ty (func (result u32)))
  (func $lifted (type $ty) (canon lift (core func $f)))
  (export "ascribed" (func $lifted) (func (type $ty)))
  (export "plain" (func $lifted))
  (export "the-type" (type $ty) (type (eq $ty)))
)
