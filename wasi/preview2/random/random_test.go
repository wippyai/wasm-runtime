package random

import (
	"context"
	"testing"
)

func TestSecureRandomHost_GetRandomBytes(t *testing.T) {
	host := NewSecureRandomHost()
	ctx := context.Background()

	data := host.GetRandomBytes(ctx, 16)
	if len(data) != 16 {
		t.Errorf("expected 16 bytes, got %d", len(data))
	}

	allZero := true
	for _, b := range data {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("random bytes should not all be zero")
	}
}

func TestSecureRandomHost_GetRandomU64(t *testing.T) {
	host := NewSecureRandomHost()
	ctx := context.Background()

	v1 := host.GetRandomU64(ctx)
	v2 := host.GetRandomU64(ctx)

	if v1 == 0 && v2 == 0 {
		t.Error("both random u64 values are zero, unlikely")
	}
}

func TestSecureRandomHost_Namespace(t *testing.T) {
	host := NewSecureRandomHost()
	ns := host.Namespace()
	expected := "wasi:random/random@0.2.0"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestInsecureRandomHost_GetInsecureRandomBytes(t *testing.T) {
	host := NewInsecureRandomHost()
	ctx := context.Background()

	data := host.GetInsecureRandomBytes(ctx, 32)
	if len(data) != 32 {
		t.Errorf("expected 32 bytes, got %d", len(data))
	}

	allZero := true
	for _, b := range data {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("insecure random bytes should not all be zero")
	}
}

func TestInsecureRandomHost_GetInsecureRandomU64(t *testing.T) {
	host := NewInsecureRandomHost()
	ctx := context.Background()

	v1 := host.GetInsecureRandomU64(ctx)
	v2 := host.GetInsecureRandomU64(ctx)

	if v1 == v2 {
		t.Error("two consecutive insecure random u64 values should differ")
	}
}

func TestInsecureRandomHost_Namespace(t *testing.T) {
	host := NewInsecureRandomHost()
	ns := host.Namespace()
	expected := "wasi:random/insecure@0.2.0"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestInsecureSeedHost_InsecureSeed(t *testing.T) {
	host := NewInsecureSeedHost()
	ctx := context.Background()

	s1, s2 := host.InsecureSeed(ctx)

	if s1 == 0 && s2 == 0 {
		t.Error("insecure seed should not be (0, 0)")
	}
}

func TestInsecureSeedHost_Namespace(t *testing.T) {
	host := NewInsecureSeedHost()
	ns := host.Namespace()
	expected := "wasi:random/insecure-seed@0.2.0"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestSecureRandom_Uniqueness(t *testing.T) {
	host := NewSecureRandomHost()
	ctx := context.Background()

	seen := make(map[uint64]bool)
	for i := 0; i < 100; i++ {
		v := host.GetRandomU64(ctx)
		if seen[v] {
			t.Errorf("duplicate random value: %d", v)
		}
		seen[v] = true
	}
}

func TestInsecureRandom_Distribution(t *testing.T) {
	host := NewInsecureRandomHost()
	ctx := context.Background()

	counts := make(map[byte]int)
	data := host.GetInsecureRandomBytes(ctx, 1000)

	for _, b := range data {
		counts[b]++
	}

	if len(counts) < 200 {
		t.Errorf("poor distribution: only %d unique values out of 256 possible", len(counts))
	}
}

func BenchmarkSecureRandomBytes(b *testing.B) {
	host := NewSecureRandomHost()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		host.GetRandomBytes(ctx, 32)
	}
}

func BenchmarkInsecureRandomBytes(b *testing.B) {
	host := NewInsecureRandomHost()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		host.GetInsecureRandomBytes(ctx, 32)
	}
}
