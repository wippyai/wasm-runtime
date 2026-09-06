package engine

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"go.bytecodealliance.org/wit"

	wasmruntime "github.com/wippyai/wasm-runtime"
	"github.com/wippyai/wasm-runtime/transcoder"
)

type testMemory struct {
	data []byte
}

func newTestMemory(size int) *testMemory {
	return &testMemory{data: make([]byte, size)}
}

func (m *testMemory) Read(offset uint32, length uint32) ([]byte, error) {
	if int(offset)+int(length) > len(m.data) || int(offset) < 0 || int(length) < 0 {
		return nil, fmt.Errorf("read out of bounds: offset=%d, length=%d, size=%d", offset, length, len(m.data))
	}
	return m.data[offset : offset+length], nil
}

func (m *testMemory) Write(offset uint32, data []byte) error {
	if int(offset)+len(data) > len(m.data) || int(offset) < 0 {
		return fmt.Errorf("write out of bounds: offset=%d, length=%d, size=%d", offset, len(data), len(m.data))
	}
	copy(m.data[offset:], data)
	return nil
}

func (m *testMemory) ReadU8(offset uint32) (uint8, error) {
	if int(offset)+1 > len(m.data) {
		return 0, fmt.Errorf("read out of bounds: offset=%d", offset)
	}
	return m.data[offset], nil
}

func (m *testMemory) ReadU16(offset uint32) (uint16, error) {
	if int(offset)+2 > len(m.data) {
		return 0, fmt.Errorf("read out of bounds: offset=%d", offset)
	}
	return binary.LittleEndian.Uint16(m.data[offset:]), nil
}

func (m *testMemory) ReadU32(offset uint32) (uint32, error) {
	if int(offset)+4 > len(m.data) {
		return 0, fmt.Errorf("read out of bounds: offset=%d", offset)
	}
	return binary.LittleEndian.Uint32(m.data[offset:]), nil
}

func (m *testMemory) ReadU64(offset uint32) (uint64, error) {
	if int(offset)+8 > len(m.data) {
		return 0, fmt.Errorf("read out of bounds: offset=%d", offset)
	}
	return binary.LittleEndian.Uint64(m.data[offset:]), nil
}

func (m *testMemory) WriteU8(offset uint32, value uint8) error {
	if int(offset)+1 > len(m.data) {
		return fmt.Errorf("write out of bounds: offset=%d", offset)
	}
	m.data[offset] = value
	return nil
}

func (m *testMemory) WriteU16(offset uint32, value uint16) error {
	if int(offset)+2 > len(m.data) {
		return fmt.Errorf("write out of bounds: offset=%d", offset)
	}
	binary.LittleEndian.PutUint16(m.data[offset:], value)
	return nil
}

func (m *testMemory) WriteU32(offset uint32, value uint32) error {
	if int(offset)+4 > len(m.data) {
		return fmt.Errorf("write out of bounds: offset=%d", offset)
	}
	binary.LittleEndian.PutUint32(m.data[offset:], value)
	return nil
}

func (m *testMemory) WriteU64(offset uint32, value uint64) error {
	if int(offset)+8 > len(m.data) {
		return fmt.Errorf("write out of bounds: offset=%d", offset)
	}
	binary.LittleEndian.PutUint64(m.data[offset:], value)
	return nil
}

var _ wasmruntime.Memory = (*testMemory)(nil)

func compileStringAndListU8Types(t *testing.T) (*transcoder.Decoder, []*transcoder.CompiledType) {
	t.Helper()
	compiler := transcoder.NewCompiler()

	ctString, err := compiler.Compile(wit.String{}, reflect.TypeOf(""))
	if err != nil {
		t.Fatalf("compile string: %v", err)
	}

	ctListU8, err := compiler.Compile(&wit.TypeDef{Kind: &wit.List{Type: wit.U8{}}}, reflect.TypeOf([]byte{}))
	if err != nil {
		t.Fatalf("compile list<u8>: %v", err)
	}

	dec := transcoder.NewDecoderWithCompiler(compiler)
	return dec, []*transcoder.CompiledType{ctString, ctString, ctListU8}
}

func TestBindResult3_DecodingStringStringListU8(t *testing.T) {
	dec, paramTypes := compileStringAndListU8Types(t)

	mem := newTestMemory(1024)
	s1 := "actor-id-42"
	s2 := "handle-message"
	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02}

	offsetS1 := uint32(16)
	offsetS2 := uint32(64)
	offsetPayload := uint32(128)

	if err := mem.Write(offsetS1, []byte(s1)); err != nil {
		t.Fatalf("write s1: %v", err)
	}
	if err := mem.Write(offsetS2, []byte(s2)); err != nil {
		t.Fatalf("write s2: %v", err)
	}
	if err := mem.Write(offsetPayload, payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	stack := []uint64{
		uint64(offsetS1), uint64(len(s1)),
		uint64(offsetS2), uint64(len(s2)),
		uint64(offsetPayload), uint64(len(payload)),
	}

	hostCalled := false
	handler := func(ctx context.Context, actorID string, method string, data []byte) (string, error) {
		hostCalled = true
		if actorID != s1 {
			t.Errorf("actorID = %q, want %q", actorID, s1)
		}
		if method != s2 {
			t.Errorf("method = %q, want %q", method, s2)
		}
		if !reflect.DeepEqual(data, payload) {
			t.Errorf("data = %v, want %v", data, payload)
		}
		return fmt.Sprintf("ok:%s:%s:%d", actorID, method, len(data)), nil
	}

	thf := BindResult3(handler)
	if thf.handler == nil {
		t.Fatal("thf.handler is nil")
	}

	val, hostErr, liftErr := thf.invoke(context.Background(), dec, paramTypes, stack, mem)
	if liftErr != nil {
		t.Fatalf("unexpected liftErr: %v", liftErr)
	}
	if hostErr != nil {
		t.Fatalf("unexpected hostErr: %v", hostErr)
	}
	if !hostCalled {
		t.Fatal("host function was not called")
	}
	if !val.IsValid() {
		t.Fatal("returned reflect.Value is not valid")
	}
	if !val.CanAddr() {
		t.Fatal("returned reflect.Value must be addressable")
	}
	want := fmt.Sprintf("ok:%s:%s:%d", s1, s2, len(payload))
	if val.Interface() != want {
		t.Errorf("result = %v, want %v", val.Interface(), want)
	}
}

func TestBindResult3_InvalidMemoryRejectsBeforeHost(t *testing.T) {
	dec, paramTypes := compileStringAndListU8Types(t)

	// Memory only has 64 bytes
	mem := newTestMemory(64)

	// Point s1 to out-of-bounds offset 200
	stack := []uint64{
		200, 50,
		0, 0,
		0, 0,
	}

	hostCalled := false
	handler := func(ctx context.Context, a, b string, c []byte) (string, error) {
		hostCalled = true
		return "should_not_be_called", nil
	}

	thf := BindResult3(handler)
	val, hostErr, liftErr := thf.invoke(context.Background(), dec, paramTypes, stack, mem)

	if hostCalled {
		t.Fatal("host function was invoked despite invalid memory read")
	}
	if liftErr == nil {
		t.Fatal("expected lift error on invalid memory, got nil")
	}
	if hostErr != nil {
		t.Fatalf("expected nil hostErr, got %v", hostErr)
	}
	if val.IsValid() {
		t.Fatalf("expected invalid reflect.Value on lift failure, got %v", val)
	}
}

func TestBindResult3_HostErrorPreserved(t *testing.T) {
	dec, paramTypes := compileStringAndListU8Types(t)

	mem := newTestMemory(256)
	stack := []uint64{0, 0, 0, 0, 0, 0}

	expectedErr := errors.New("actor host execution failure")
	handler := func(ctx context.Context, a, b string, c []byte) (string, error) {
		return "partial_output", expectedErr
	}

	thf := BindResult3(handler)
	val, hostErr, liftErr := thf.invoke(context.Background(), dec, paramTypes, stack, mem)

	if liftErr != nil {
		t.Fatalf("unexpected liftErr: %v", liftErr)
	}
	if !errors.Is(hostErr, expectedErr) {
		t.Fatalf("hostErr = %v, want %v", hostErr, expectedErr)
	}
	if !val.IsValid() {
		t.Fatal("returned reflect.Value is not valid")
	}
	if !val.CanAddr() {
		t.Fatal("returned reflect.Value must be addressable")
	}
	if val.Interface() != "partial_output" {
		t.Errorf("val = %v, want partial_output", val.Interface())
	}
}

func TestBindResult0_SuccessAndHostError(t *testing.T) {
	compiler := transcoder.NewCompiler()
	dec := transcoder.NewDecoderWithCompiler(compiler)
	mem := newTestMemory(64)

	// Success case
	handlerSuccess := func(ctx context.Context) (int64, error) {
		return 42, nil
	}
	thf0 := BindResult0(handlerSuccess)
	if thf0.handler == nil {
		t.Fatal("thf0.handler is nil")
	}
	val, hostErr, liftErr := thf0.invoke(context.Background(), dec, nil, nil, mem)
	if liftErr != nil {
		t.Fatalf("unexpected liftErr: %v", liftErr)
	}
	if hostErr != nil {
		t.Fatalf("unexpected hostErr: %v", hostErr)
	}
	if !val.CanAddr() {
		t.Fatal("expected addressable reflect.Value")
	}
	if val.Interface() != int64(42) {
		t.Errorf("val = %v, want 42", val.Interface())
	}

	// Host error case
	expectedErr := errors.New("error from 0-arg host")
	handlerErr := func(ctx context.Context) (int64, error) {
		return -1, expectedErr
	}
	thfErr := BindResult0(handlerErr)
	val, hostErr, liftErr = thfErr.invoke(context.Background(), dec, nil, nil, mem)
	if liftErr != nil {
		t.Fatalf("unexpected liftErr: %v", liftErr)
	}
	if !errors.Is(hostErr, expectedErr) {
		t.Fatalf("hostErr = %v, want %v", hostErr, expectedErr)
	}
	if !val.CanAddr() {
		t.Fatal("expected addressable reflect.Value")
	}
	if val.Interface() != int64(-1) {
		t.Errorf("val = %v, want -1", val.Interface())
	}
}

func TestBindResult3_StackBoundsAndNilChecks(t *testing.T) {
	dec, paramTypes := compileStringAndListU8Types(t)
	mem := newTestMemory(64)

	handler := func(ctx context.Context, a, b string, c []byte) (int, error) {
		return 1, nil
	}
	thf := BindResult3(handler)

	// Short stack (only 2 slots instead of 6 required)
	shortStack := []uint64{0, 0}
	_, _, liftErr := thf.invoke(context.Background(), dec, paramTypes, shortStack, mem)
	if liftErr == nil {
		t.Fatal("expected liftErr for short stack, got nil")
	}

	// Nil decoder
	_, _, liftErr = thf.invoke(context.Background(), nil, paramTypes, []uint64{0, 0, 0, 0, 0, 0}, mem)
	if liftErr == nil {
		t.Fatal("expected liftErr for nil decoder, got nil")
	}

	// Nil paramTypes
	_, _, liftErr = thf.invoke(context.Background(), dec, nil, []uint64{0, 0, 0, 0, 0, 0}, mem)
	if liftErr == nil {
		t.Fatal("expected liftErr for nil paramTypes, got nil")
	}
}
