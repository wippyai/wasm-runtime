package component

import (
	"os"
	"testing"
)

func TestDecodeAndValidateComplex(t *testing.T) {
	data, err := os.ReadFile("../testbed/complex.wasm")
	if err != nil {
		t.Fatalf("failed to read complex.wasm: %v", err)
	}

	validated, err := DecodeAndValidate(data)
	if err != nil {
		t.Fatalf("DecodeAndValidate failed: %v", err)
	}

	if validated == nil {
		t.Fatal("validated component is nil")
	}

	t.Logf("Successfully validated complex.wasm")
	t.Logf("Types: %d", validated.TypeCount())
	t.Logf("Funcs: %d", validated.FuncCount())
	t.Logf("Instances: %d", validated.InstanceCount())
}

func TestDecodeAndValidateMinimal(t *testing.T) {
	data, err := os.ReadFile("../testbed/minimal.wasm")
	if err != nil {
		t.Fatalf("failed to read minimal.wasm: %v", err)
	}

	validated, err := DecodeAndValidate(data)
	if err != nil {
		t.Fatalf("DecodeAndValidate failed: %v", err)
	}

	if validated == nil {
		t.Fatal("validated component is nil")
	}

	t.Logf("Successfully validated minimal.wasm")
}

func TestDecodeAndValidateCalculator(t *testing.T) {
	data, err := os.ReadFile("../testbed/calculator.wasm")
	if err != nil {
		t.Fatalf("failed to read calculator.wasm: %v", err)
	}

	validated, err := DecodeAndValidate(data)
	if err != nil {
		t.Fatalf("DecodeAndValidate failed: %v", err)
	}

	if validated == nil {
		t.Fatal("validated component is nil")
	}

	t.Logf("Successfully validated calculator.wasm")
}
