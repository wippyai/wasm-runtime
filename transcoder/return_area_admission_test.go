package transcoder

import (
	"fmt"
	"testing"

	"go.bytecodealliance.org/wit"
)

// Model the end and beginning of a 4 GiB memory without allocating it.
type returnAreaWrapMemory struct{ *mockMemory }

func (m *returnAreaWrapMemory) ReadU8(addr uint32) (uint8, error) {
	if addr == 0xFFFFFFFC {
		return 7, nil
	}
	return 0, fmt.Errorf("unexpected byte address %x", addr)
}

func (m *returnAreaWrapMemory) ReadU64(addr uint32) (uint64, error) {
	if addr == 4 {
		return 9, nil
	}
	return 0, fmt.Errorf("unexpected word address %x", addr)
}

func TestReturnAreaRejectsAddressWrap(t *testing.T) {
	types := []wit.Type{wit.U8{}, wit.U64{}}
	mem := &returnAreaWrapMemory{mockMemory: &mockMemory{}}
	d := NewDecoder()
	if _, err := d.LoadResults(types, 0xFFFFFFFC, mem); err == nil {
		t.Fatal("return area wrapped into the beginning of memory")
	}
	var result struct {
		A uint8
		B uint64
	}
	if err := d.LoadResultsInto(types, 0xFFFFFFFC, mem, &result); err == nil {
		t.Fatal("typed return area wrapped into the beginning of memory")
	}
}

func TestReturnAreaRejectsMalformedSchemaBeforeLayout(t *testing.T) {
	for _, kind := range typedNilKinds() {
		for _, into := range []bool{false, true} {
			name := kind.name + "/values"
			if into {
				name = kind.name + "/into"
			}
			t.Run(name, func(t *testing.T) {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("malformed schema caused panic: %v", r)
					}
				}()
				types := []wit.Type{wit.U8{}, &wit.TypeDef{Kind: kind.kind}}
				d := NewDecoder()
				mem := &mockMemory{data: make([]byte, 32)}
				var err error
				if into {
					var result struct {
						A uint8
						B uint64
					}
					err = d.LoadResultsInto(types, 0, mem, &result)
				} else {
					_, err = d.LoadResults(types, 0, mem)
				}
				if err == nil {
					t.Fatal("malformed schema was accepted")
				}
			})
		}
	}
}
