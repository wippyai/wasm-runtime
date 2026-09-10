(component
  (core module)
  (type $signature (func (result u32)))
  (export "first" (type $signature))
  (export "second" (type 1))
  (import "callback" (func (type 2)))
)
