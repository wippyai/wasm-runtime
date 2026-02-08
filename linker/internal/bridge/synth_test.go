package bridge

import (
	"context"
	"errors"
	"testing"

	"github.com/tetratelabs/wazero"
)

func TestNewSynthBuilder_NilRuntime(t *testing.T) {
	_, err := NewSynthBuilder(nil)
	if !errors.Is(err, ErrNilRuntime) {
		t.Errorf("expected ErrNilRuntime, got %v", err)
	}
}

func TestSynthBuilder_Build_NilSpec(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	s, err := NewSynthBuilder(rt)
	if err != nil {
		t.Fatalf("NewSynthBuilder failed: %v", err)
	}
	mod, err := s.Build(ctx, nil)
	if err != nil {
		t.Errorf("unexpected error for nil spec: %v", err)
	}
	if mod != nil {
		t.Error("expected nil module for nil spec")
	}
}

func TestSynthBuilder_BuildHostModule_EmptyExports(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	s, err := NewSynthBuilder(rt)
	if err != nil {
		t.Fatalf("NewSynthBuilder failed: %v", err)
	}
	mod, err := s.BuildHostModule(ctx, "test", nil)
	if err != nil {
		t.Errorf("unexpected error for empty exports: %v", err)
	}
	if mod != nil {
		t.Error("expected nil module for empty exports")
	}
}
