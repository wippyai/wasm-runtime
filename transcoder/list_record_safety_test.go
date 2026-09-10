package transcoder

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"go.bytecodealliance.org/wit"
)

type listSafetyMemory struct {
	data []byte
}

func newListSafetyMemory(size int) *listSafetyMemory {
	return &listSafetyMemory{data: make([]byte, size)}
}

func (m *listSafetyMemory) bounds(offset, length uint32) error {
	end := uint64(offset) + uint64(length)
	if end > uint64(len(m.data)) {
		return fmt.Errorf("read out of bounds: offset=%d length=%d size=%d", offset, length, len(m.data))
	}
	return nil
}

func (m *listSafetyMemory) Read(offset, length uint32) ([]byte, error) {
	if err := m.bounds(offset, length); err != nil {
		return nil, err
	}
	out := make([]byte, length)
	copy(out, m.data[offset:offset+length])
	return out, nil
}

func (m *listSafetyMemory) Write(offset uint32, data []byte) error {
	if err := m.bounds(offset, uint32(len(data))); err != nil {
		return err
	}
	copy(m.data[offset:], data)
	return nil
}

func (m *listSafetyMemory) ReadU8(offset uint32) (uint8, error) {
	b, err := m.Read(offset, 1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (m *listSafetyMemory) ReadU16(offset uint32) (uint16, error) {
	b, err := m.Read(offset, 2)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(b), nil
}

func (m *listSafetyMemory) ReadU32(offset uint32) (uint32, error) {
	b, err := m.Read(offset, 4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

func (m *listSafetyMemory) ReadU64(offset uint32) (uint64, error) {
	b, err := m.Read(offset, 8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b), nil
}

func (m *listSafetyMemory) WriteU8(offset uint32, value uint8) error {
	return m.Write(offset, []byte{value})
}

func (m *listSafetyMemory) WriteU16(offset uint32, value uint16) error {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], value)
	return m.Write(offset, buf[:])
}

func (m *listSafetyMemory) WriteU32(offset uint32, value uint32) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], value)
	return m.Write(offset, buf[:])
}

func (m *listSafetyMemory) WriteU64(offset uint32, value uint64) error {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], value)
	return m.Write(offset, buf[:])
}

type ipv4SocketAddress struct {
	Port    uint16  `wit:"port"`
	Address [4]byte `wit:"address"`
}

type ipv6SocketAddress struct {
	Port     uint16    `wit:"port"`
	FlowInfo uint32    `wit:"flow-info"`
	Address  [8]uint16 `wit:"address"`
	ScopeID  uint32    `wit:"scope-id"`
}

type ipSocketAddress struct {
	IPv4 *ipv4SocketAddress `wit:"ipv4"`
	IPv6 *ipv6SocketAddress `wit:"ipv6"`
}

type outgoingDatagram struct {
	RemoteAddress *ipSocketAddress `wit:"remote-address"`
	Data          []byte           `wit:"data"`
}

type mixedScalarRecord struct {
	Key  uint64 `wit:"key"`
	ID   uint32 `wit:"id"`
	Port uint16 `wit:"port"`
	Flag uint8  `wit:"flag"`
}

type namedRecord struct {
	Name string `wit:"name"`
	ID   uint32 `wit:"id"`
}

func outgoingDatagramWIT() (listType *wit.TypeDef, recordType *wit.TypeDef) {
	ipv4Address := &wit.TypeDef{
		Kind: &wit.Tuple{Types: []wit.Type{wit.U8{}, wit.U8{}, wit.U8{}, wit.U8{}}},
	}
	ipv6Address := &wit.TypeDef{
		Kind: &wit.Tuple{Types: []wit.Type{
			wit.U16{}, wit.U16{}, wit.U16{}, wit.U16{},
			wit.U16{}, wit.U16{}, wit.U16{}, wit.U16{},
		}},
	}
	ipv4Record := &wit.TypeDef{
		Kind: &wit.Record{Fields: []wit.Field{
			{Name: "port", Type: wit.U16{}},
			{Name: "address", Type: ipv4Address},
		}},
	}
	ipv6Record := &wit.TypeDef{
		Kind: &wit.Record{Fields: []wit.Field{
			{Name: "port", Type: wit.U16{}},
			{Name: "flow-info", Type: wit.U32{}},
			{Name: "address", Type: ipv6Address},
			{Name: "scope-id", Type: wit.U32{}},
		}},
	}
	ipAddr := &wit.TypeDef{
		Kind: &wit.Variant{Cases: []wit.Case{
			{Name: "ipv4", Type: ipv4Record},
			{Name: "ipv6", Type: ipv6Record},
		}},
	}
	remote := &wit.TypeDef{Kind: &wit.Option{Type: ipAddr}}
	recordType = &wit.TypeDef{
		Kind: &wit.Record{Fields: []wit.Field{
			{Name: "data", Type: &wit.TypeDef{Kind: &wit.List{Type: wit.U8{}}}},
			{Name: "remote-address", Type: remote},
		}},
	}
	listType = &wit.TypeDef{Kind: &wit.List{Type: recordType}}
	return listType, recordType
}

func compileOutgoingDatagramList(t *testing.T) *CompiledType {
	t.Helper()
	listType, _ := outgoingDatagramWIT()
	ct, err := NewCompiler().Compile(listType, reflect.TypeOf([]outgoingDatagram{}))
	if err != nil {
		t.Fatalf("compile outgoing-datagram list: %v", err)
	}
	return ct
}

func datagramField(t *testing.T, ct *CompiledType, name string) CompiledField {
	t.Helper()
	for _, f := range ct.ElemType.Fields {
		if f.WitName == name {
			return f
		}
	}
	t.Fatalf("missing field %q", name)
	return CompiledField{}
}

func writeGuestOutgoingDatagram(mem *listSafetyMemory, recordAddr, dataAddr uint32, payload []byte, v4 *ipv4SocketAddress, v6 *ipv6SocketAddress) error {
	if err := mem.Write(dataAddr, payload); err != nil {
		return err
	}
	if err := mem.WriteU32(recordAddr, dataAddr); err != nil {
		return err
	}
	if err := mem.WriteU32(recordAddr+4, uint32(len(payload))); err != nil {
		return err
	}
	if v4 == nil && v6 == nil {
		return mem.WriteU8(recordAddr+8, 0)
	}
	if err := mem.WriteU8(recordAddr+8, 1); err != nil {
		return err
	}
	if v4 != nil {
		if err := mem.WriteU8(recordAddr+12, 0); err != nil {
			return err
		}
		if err := mem.WriteU16(recordAddr+16, v4.Port); err != nil {
			return err
		}
		return mem.Write(recordAddr+18, v4.Address[:])
	}
	if err := mem.WriteU8(recordAddr+12, 1); err != nil {
		return err
	}
	if err := mem.WriteU16(recordAddr+16, v6.Port); err != nil {
		return err
	}
	if err := mem.WriteU32(recordAddr+20, v6.FlowInfo); err != nil {
		return err
	}
	for i, part := range v6.Address {
		if err := mem.WriteU16(recordAddr+24+uint32(i)*2, part); err != nil {
			return err
		}
	}
	return mem.WriteU32(recordAddr+40, v6.ScopeID)
}

func liftDatagramList(t *testing.T, mem Memory, ct *CompiledType, recordAddr, length uint32) []outgoingDatagram {
	t.Helper()
	stack := []uint64{uint64(recordAddr), uint64(length)}
	var result []outgoingDatagram
	if _, err := NewDecoder().LiftFromStack(ct, stack, unsafe.Pointer(&result), mem); err != nil {
		t.Fatalf("LiftFromStack: %v", err)
	}
	return result
}

func TestLiftListFromStack_OutgoingDatagramGuestLayout(t *testing.T) {
	ct := compileOutgoingDatagramList(t)
	if ct.ElemType.Kind != KindRecord {
		t.Fatalf("elem kind=%v, want record", ct.ElemType.Kind)
	}
	if ct.ElemType.WitSize != 44 {
		t.Fatalf("outgoing-datagram WitSize=%d, want 44", ct.ElemType.WitSize)
	}
	remote := datagramField(t, ct, "remote-address")
	if remote.Type.Kind != KindOption {
		t.Fatalf("remote-address kind=%v, want option", remote.Type.Kind)
	}
	if remote.WitOffset != 8 {
		t.Fatalf("remote-address WitOffset=%d, want 8", remote.WitOffset)
	}

	mem := newListSafetyMemory(4096)
	payload := []byte("first")
	want := ipv4SocketAddress{Port: 38885, Address: [4]byte{127, 0, 0, 1}}
	if err := writeGuestOutgoingDatagram(mem, 64, 200, payload, &want, nil); err != nil {
		t.Fatalf("write guest record: %v", err)
	}

	raw, err := mem.Read(64, 44)
	if err != nil {
		t.Fatalf("read raw record: %v", err)
	}
	if raw[8] != 1 {
		t.Fatalf("option tag at 8=%d, want 1", raw[8])
	}
	if raw[12] != 0 {
		t.Fatalf("ipv4 variant tag at 12=%d, want 0", raw[12])
	}
	if port := binary.LittleEndian.Uint16(raw[16:18]); port != 38885 {
		t.Fatalf("port at 16=%d, want 38885", port)
	}

	result := liftDatagramList(t, mem, ct, 64, 1)
	if len(result) != 1 {
		t.Fatalf("len=%d, want 1", len(result))
	}
	if string(result[0].Data) != "first" {
		t.Fatalf("data=%q, want %q", result[0].Data, "first")
	}
	if result[0].RemoteAddress == nil {
		t.Fatal("RemoteAddress is nil for Some(ipv4)")
	}
	if result[0].RemoteAddress.IPv4 == nil {
		t.Fatal("RemoteAddress.IPv4 is nil")
	}
	if result[0].RemoteAddress.IPv6 != nil {
		t.Fatal("RemoteAddress.IPv6 is set for ipv4 datagram")
	}
	got := *result[0].RemoteAddress.IPv4
	if got != want {
		t.Fatalf("ipv4=%+v, want %+v", got, want)
	}
}

func TestLiftListFromStack_OutgoingDatagramNone(t *testing.T) {
	ct := compileOutgoingDatagramList(t)
	mem := newListSafetyMemory(4096)
	if err := writeGuestOutgoingDatagram(mem, 64, 200, []byte("none"), nil, nil); err != nil {
		t.Fatalf("write guest record: %v", err)
	}
	result := liftDatagramList(t, mem, ct, 64, 1)
	if len(result) != 1 {
		t.Fatalf("len=%d, want 1", len(result))
	}
	if string(result[0].Data) != "none" {
		t.Fatalf("data=%q, want %q", result[0].Data, "none")
	}
	if result[0].RemoteAddress != nil {
		t.Fatalf("RemoteAddress=%+v, want nil", result[0].RemoteAddress)
	}
}

func TestLiftListFromStack_OutgoingDatagramIPv6(t *testing.T) {
	ct := compileOutgoingDatagramList(t)
	mem := newListSafetyMemory(4096)
	want := ipv6SocketAddress{
		Port:     443,
		FlowInfo: 0x11223344,
		Address:  [8]uint16{0xfe80, 0, 0, 0, 0, 0, 0, 1},
		ScopeID:  7,
	}
	if err := writeGuestOutgoingDatagram(mem, 80, 300, []byte("v6"), nil, &want); err != nil {
		t.Fatalf("write guest record: %v", err)
	}
	result := liftDatagramList(t, mem, ct, 80, 1)
	if len(result) != 1 {
		t.Fatalf("len=%d, want 1", len(result))
	}
	if string(result[0].Data) != "v6" {
		t.Fatalf("data=%q, want %q", result[0].Data, "v6")
	}
	if result[0].RemoteAddress == nil || result[0].RemoteAddress.IPv6 == nil {
		t.Fatal("RemoteAddress.IPv6 is nil")
	}
	if result[0].RemoteAddress.IPv4 != nil {
		t.Fatal("RemoteAddress.IPv4 is set for ipv6 datagram")
	}
	got := *result[0].RemoteAddress.IPv6
	if got != want {
		t.Fatalf("ipv6=%+v, want %+v", got, want)
	}
}

func TestLiftListFromStack_OutgoingDatagramSomeAndNone(t *testing.T) {
	ct := compileOutgoingDatagramList(t)
	if ct.ElemType.WitSize != 44 {
		t.Fatalf("outgoing-datagram WitSize=%d, want 44", ct.ElemType.WitSize)
	}
	mem := newListSafetyMemory(8192)
	want := ipv4SocketAddress{Port: 38885, Address: [4]byte{127, 0, 0, 1}}
	if err := writeGuestOutgoingDatagram(mem, 64, 400, []byte("first"), &want, nil); err != nil {
		t.Fatalf("write Some record: %v", err)
	}
	if err := writeGuestOutgoingDatagram(mem, 64+44, 420, []byte("none"), nil, nil); err != nil {
		t.Fatalf("write None record: %v", err)
	}
	result := liftDatagramList(t, mem, ct, 64, 2)
	if len(result) != 2 {
		t.Fatalf("len=%d, want 2", len(result))
	}
	if string(result[0].Data) != "first" {
		t.Fatalf("data[0]=%q, want first", result[0].Data)
	}
	if result[0].RemoteAddress == nil || result[0].RemoteAddress.IPv4 == nil {
		t.Fatal("Some ipv4 is nil")
	}
	if *result[0].RemoteAddress.IPv4 != want {
		t.Fatalf("ipv4=%+v, want %+v", result[0].RemoteAddress.IPv4, want)
	}
	if string(result[1].Data) != "none" {
		t.Fatalf("data[1]=%q, want none", result[1].Data)
	}
	if result[1].RemoteAddress != nil {
		t.Fatalf("None is %+v", result[1].RemoteAddress)
	}
}

type listSafetyAllocator struct {
	mem    *listSafetyMemory
	offset uint32
}

func (a *listSafetyAllocator) Alloc(size, align uint32) (uint32, error) {
	if align == 0 {
		align = 1
	}
	a.offset = (a.offset + align - 1) &^ (align - 1)
	addr := a.offset
	end := uint64(addr) + uint64(size)
	if end > uint64(len(a.mem.data)) {
		return 0, fmt.Errorf("alloc out of bounds: %d+%d > %d", addr, size, len(a.mem.data))
	}
	a.offset += size
	return addr, nil
}

func (a *listSafetyAllocator) Free(uint32, uint32, uint32) {}

func TestLiftListFromStack_RecordNonU32Scalars(t *testing.T) {
	recordType := &wit.TypeDef{
		Kind: &wit.Record{Fields: []wit.Field{
			{Name: "flag", Type: wit.U8{}},
			{Name: "port", Type: wit.U16{}},
			{Name: "key", Type: wit.U64{}},
			{Name: "id", Type: wit.U32{}},
		}},
	}
	listType := &wit.TypeDef{Kind: &wit.List{Type: recordType}}
	ct, err := NewCompiler().Compile(listType, reflect.TypeOf([]mixedScalarRecord{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	offs := map[string]uint32{}
	kinds := map[string]TypeKind{}
	for _, f := range ct.ElemType.Fields {
		offs[f.WitName] = f.WitOffset
		kinds[f.WitName] = f.Type.Kind
	}
	if kinds["flag"] != KindU8 || kinds["port"] != KindU16 || kinds["key"] != KindU64 || kinds["id"] != KindU32 {
		t.Fatalf("field kinds=%v", kinds)
	}

	input := []mixedScalarRecord{
		{Flag: 7, Port: 38885, Key: 0x0102030405060708, ID: 42},
		{Flag: 9, Port: 443, Key: 99, ID: 1},
	}
	mem := newListSafetyMemory(4096)
	recordSize := ct.ElemType.WitSize
	base := uint32(64)
	for i, rec := range input {
		addr := base + uint32(i)*recordSize
		if err := mem.WriteU8(addr+offs["flag"], rec.Flag); err != nil {
			t.Fatalf("write flag: %v", err)
		}
		if err := mem.WriteU16(addr+offs["port"], rec.Port); err != nil {
			t.Fatalf("write port: %v", err)
		}
		if err := mem.WriteU64(addr+offs["key"], rec.Key); err != nil {
			t.Fatalf("write key: %v", err)
		}
		if err := mem.WriteU32(addr+offs["id"], rec.ID); err != nil {
			t.Fatalf("write id: %v", err)
		}
	}

	stack := []uint64{uint64(base), uint64(len(input))}
	var output []mixedScalarRecord
	if _, err := NewDecoder().LiftFromStack(ct, stack, unsafe.Pointer(&output), mem); err != nil {
		t.Fatalf("LiftFromStack: %v", err)
	}
	if !reflect.DeepEqual(output, input) {
		t.Fatalf("got %+v, want %+v", output, input)
	}
}

func TestLiftListFromStack_InvalidCanonicalMemoryNoSideEffects(t *testing.T) {
	ct := compileOutgoingDatagramList(t)
	mem := newListSafetyMemory(16)
	sentinel := []outgoingDatagram{{Data: []byte("keep")}}
	dest := sentinel
	stack := []uint64{0, 1}
	_, err := NewDecoder().LiftFromStack(ct, stack, unsafe.Pointer(&dest), mem)
	if err == nil {
		t.Fatal("expected bounds error for 44-byte record in 16-byte memory")
	}
	if len(dest) != 1 || string(dest[0].Data) != "keep" {
		t.Fatalf("destination mutated before bounds error: %+v", dest)
	}

	var nilDest []outgoingDatagram
	_, err = NewDecoder().LiftFromStack(ct, stack, unsafe.Pointer(&nilDest), mem)
	if err == nil {
		t.Fatal("expected bounds error for nil destination")
	}
	if nilDest != nil {
		t.Fatalf("nil destination allocated before bounds error: %+v", nilDest)
	}
}

func TestLiftListFromStack_ByteAndNumericLists(t *testing.T) {
	compiler := NewCompiler()
	dec := NewDecoder()
	enc := NewEncoder()
	mem := newListSafetyMemory(4096)
	alloc := &listSafetyAllocator{mem: mem, offset: 64}

	t.Run("bytes", func(t *testing.T) {
		ct, err := compiler.Compile(&wit.TypeDef{Kind: &wit.List{Type: wit.U8{}}}, reflect.TypeOf([]byte{}))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		input := []byte{1, 2, 3, 9}
		stack := make([]uint64, 2)
		if _, err := enc.LowerToStack(ct, unsafe.Pointer(&input), stack, mem, alloc); err != nil {
			t.Fatalf("LowerToStack: %v", err)
		}
		var output []byte
		if _, err := dec.LiftFromStack(ct, stack, unsafe.Pointer(&output), mem); err != nil {
			t.Fatalf("LiftFromStack: %v", err)
		}
		if !reflect.DeepEqual(output, input) {
			t.Fatalf("got %v, want %v", output, input)
		}
	})

	t.Run("u32", func(t *testing.T) {
		ct, err := compiler.Compile(&wit.TypeDef{Kind: &wit.List{Type: wit.U32{}}}, reflect.TypeOf([]uint32{}))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		input := []uint32{1, 2, 3}
		stack := make([]uint64, 2)
		if _, err := enc.LowerToStack(ct, unsafe.Pointer(&input), stack, mem, alloc); err != nil {
			t.Fatalf("LowerToStack: %v", err)
		}
		var output []uint32
		if _, err := dec.LiftFromStack(ct, stack, unsafe.Pointer(&output), mem); err != nil {
			t.Fatalf("LiftFromStack: %v", err)
		}
		if !reflect.DeepEqual(output, input) {
			t.Fatalf("got %v, want %v", output, input)
		}
	})
}

func TestLiftListFromStack_GCRetainsRecordPointers(t *testing.T) {
	ct := compileOutgoingDatagramList(t)
	mem := newListSafetyMemory(1 << 16)
	payload := []byte(strings.Repeat("udp-payload-", 256))
	want := ipv4SocketAddress{Port: 38885, Address: [4]byte{10, 1, 2, 3}}
	if err := writeGuestOutgoingDatagram(mem, 128, 4096, payload, &want, nil); err != nil {
		t.Fatalf("write guest record: %v", err)
	}

	result := liftDatagramList(t, mem, ct, 128, 1)
	churnGC()
	if len(result) != 1 {
		t.Fatalf("len=%d after GC, want 1", len(result))
	}
	if string(result[0].Data) != string(payload) {
		t.Fatalf("payload lost after GC: len=%d", len(result[0].Data))
	}
	if result[0].RemoteAddress == nil || result[0].RemoteAddress.IPv4 == nil {
		t.Fatal("RemoteAddress pointer lost after GC")
	}
	if *result[0].RemoteAddress.IPv4 != want {
		t.Fatalf("ipv4 after GC=%+v, want %+v", result[0].RemoteAddress.IPv4, want)
	}
	runtime.KeepAlive(result)
}

func TestLiftListFromStack_GCRetainsRecordStrings(t *testing.T) {
	recordType := &wit.TypeDef{
		Kind: &wit.Record{Fields: []wit.Field{
			{Name: "id", Type: wit.U32{}},
			{Name: "name", Type: wit.String{}},
		}},
	}
	listType := &wit.TypeDef{Kind: &wit.List{Type: recordType}}
	ct, err := NewCompiler().Compile(listType, reflect.TypeOf([]namedRecord{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mem := newListSafetyMemory(1 << 16)
	alloc := &listSafetyAllocator{mem: mem, offset: 64}
	input := []namedRecord{
		{ID: 1, Name: strings.Repeat("alpha-", 400)},
		{ID: 2, Name: strings.Repeat("beta-", 400)},
	}
	stack := make([]uint64, 2)
	if _, err := NewEncoder().LowerToStack(ct, unsafe.Pointer(&input), stack, mem, alloc); err != nil {
		t.Fatalf("LowerToStack: %v", err)
	}
	var output []namedRecord
	if _, err := NewDecoder().LiftFromStack(ct, stack, unsafe.Pointer(&output), mem); err != nil {
		t.Fatalf("LiftFromStack: %v", err)
	}
	churnGC()
	if !reflect.DeepEqual(output, input) {
		t.Fatalf("record strings lost after GC: got id=%d/%d names len=%d/%d",
			output[0].ID, output[1].ID, len(output[0].Name), len(output[1].Name))
	}
	runtime.KeepAlive(output)
}

func churnGC() {
	for i := 0; i < 8; i++ {
		_ = make([]byte, 1<<20)
		runtime.GC()
	}
}
