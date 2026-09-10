package binary

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestReadBytesRejectsNegativeLength(t *testing.T) {
	defer func() {
		if value := recover(); value != nil {
			t.Errorf("negative length panicked: %v", value)
		}
	}()
	if _, err := NewReader(bytes.NewReader(nil)).ReadBytes(-1); err == nil {
		t.Fatal("negative length accepted")
	}
}

func TestReadBytesTruncatedPosition(t *testing.T) {
	r := NewReader(bytes.NewReader([]byte{1, 2, 3}))
	if result, err := r.ReadBytes(1 << 24); !errors.Is(err, io.EOF) || result != nil {
		t.Fatalf("truncated read: %v, %v", result, err)
	}
	if r.Position() != 3 {
		t.Fatalf("position=%d, want3 consumed bytes", r.Position())
	}
}

func BenchmarkReadBytesTruncatedClaim(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := NewReader(bytes.NewReader(nil)).ReadBytes(1 << 24); !errors.Is(err, io.EOF) {
			b.Fatal(err)
		}
	}
}

func TestReadVectorCountBounds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		data  []byte
		want  uint32
		valid bool
	}{
		{"empty", []byte{0}, 0, true},
		{"exact lower bound", []byte{2, 0, 0}, 2, true},
		{"missing element", []byte{2, 0}, 0, false},
		{"maximum count", []byte{255, 255, 255, 255, 15}, 0, false},
		{"truncated leb", []byte{128}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewReader(bytes.NewReader(tc.data))
			got, err := r.ReadVectorCount()
			if (err == nil) != tc.valid || got != tc.want {
				t.Fatalf("got %d, %v", got, err)
			}
			if tc.valid && r.Position() != 1 {
				t.Fatal("consumed vector elements")
			}
		})
	}
}
