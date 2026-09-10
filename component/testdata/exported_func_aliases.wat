(component
  (core module)
  (type $a (func (result u32)))
  (type $b (func (result string)))
  (import "first" (func (type $a)))
  (import "second" (func (type $b)))
  (export "second-out" (func 1))
  (export "again" (func 2))
)
