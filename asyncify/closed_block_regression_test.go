package asyncify_test

import "testing"

func TestClosedVoidBlockPreservesSnapshotAndResumeEffects(t *testing.T) {
	for _, position := range []string{"before", "after"} {
		before, after := "", "call $yield"
		if position == "after" {
			before, after = "call $yield", ""
		}
		body := `(func (export "run") (param $n i32) (result i32)
   (local $x i32)
   i32.const 3 local.set $x
   ` + before + `
   local.get $x
   block $done
    local.get $x i32.const 1 i32.add local.set $x
    local.get $n br_if $done
    local.get $x i32.const 2 i32.add local.set $x
   end
   ` + after + `
   local.get $x i32.add)`
		t.Run(position+"/fallthrough", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0}, 9) })
		t.Run(position+"/branch", func(t *testing.T) { runYieldMatrix(t, body, []uint64{1}, 7) })
	}
}
