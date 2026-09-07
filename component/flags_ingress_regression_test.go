package component

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wippyai/wasm-runtime/component/internal/arena"
)

func flagNames(count int) []string {
	names := make([]string, count)
	for i := range names {
		names[i] = fmt.Sprintf("f%d", i)
	}
	return names
}

func encodedFlags(count int) []byte {
	b := []byte{byte(count)} // all boundary counts fit in a one-byte unsigned LEB.
	for _, name := range flagNames(count) {
		b = append(b, byte(len(name)))
		b = append(b, name...)
	}
	return b
}

// The parser, resolver, and streaming validator are distinct ingress paths:
// binary component input, direct component values, and parsed type-section
// registration. They must make the same admission decision.
func TestFlagsIngressCanonicalRange(t *testing.T) {
	for _, count := range []int{0, 1, 32, 33} {
		t.Run(fmt.Sprintf("count_%d", count), func(t *testing.T) {
			wantErr := count == 0 || count > 32

			parsed, err := parseFlagsType(bytes.NewReader(encodedFlags(count)))
			if (err != nil) != wantErr {
				t.Fatalf("parser error = %v, wantErr=%v", err, wantErr)
			}
			if err == nil && len(parsed.Names) != count {
				t.Fatalf("parser kept %d names, want %d", len(parsed.Names), count)
			}

			_, err = NewTypeResolverWithInstances(nil, nil).Resolve(FlagsType{Names: flagNames(count)})
			if (err != nil) != wantErr {
				t.Fatalf("resolver error = %v, wantErr=%v", err, wantErr)
			}

			validator := NewStreamingValidator()
			state := arena.NewState(arena.KindComponent)
			err = validator.addDefinedType(state, FlagsType{Names: flagNames(count)})
			if (err != nil) != wantErr {
				t.Fatalf("streaming validator error = %v, wantErr=%v", err, wantErr)
			}
		})
	}
}
