package transcoder

import (
	"encoding/binary"
	"reflect"
	"testing"
	"unsafe"

	"go.bytecodealliance.org/wit"
)

type incomingDatagram struct {
	RemoteAddress ipSocketAddress `wit:"remote-address"`
	Data          []byte          `wit:"data"`
}

type mixedFastFallbackRecord struct {
	Maybe  *uint32  `wit:"maybe"`
	Name   string   `wit:"name"`
	Tags   []string `wit:"tags"`
	Key    uint64   `wit:"key"`
	ID     uint32   `wit:"id"`
	Octets [4]byte  `wit:"octets"`
	Port   uint16   `wit:"port"`
	Flag   uint8    `wit:"flag"`
	Ready  bool     `wit:"ready"`
}

func incomingDatagramWIT() (listType *wit.TypeDef, recordType *wit.TypeDef) {
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
	recordType = &wit.TypeDef{
		Kind: &wit.Record{Fields: []wit.Field{
			{Name: "data", Type: &wit.TypeDef{Kind: &wit.List{Type: wit.U8{}}}},
			{Name: "remote-address", Type: ipAddr},
		}},
	}
	listType = &wit.TypeDef{Kind: &wit.List{Type: recordType}}
	return listType, recordType
}

func compileIncomingDatagramList(t *testing.T) *CompiledType {
	t.Helper()
	listType, _ := incomingDatagramWIT()
	ct, err := NewCompiler().Compile(listType, reflect.TypeOf([]incomingDatagram{}))
	if err != nil {
		t.Fatalf("compile incoming-datagram list: %v", err)
	}
	return ct
}

func mixedFastFallbackWIT() *wit.TypeDef {
	octets := &wit.TypeDef{
		Kind: &wit.Tuple{Types: []wit.Type{wit.U8{}, wit.U8{}, wit.U8{}, wit.U8{}}},
	}
	maybe := &wit.TypeDef{Kind: &wit.Option{Type: wit.U32{}}}
	recordType := &wit.TypeDef{
		Kind: &wit.Record{Fields: []wit.Field{
			{Name: "id", Type: wit.U32{}},
			{Name: "ready", Type: wit.Bool{}},
			{Name: "name", Type: wit.String{}},
			{Name: "tags", Type: &wit.TypeDef{Kind: &wit.List{Type: wit.String{}}}},
			{Name: "flag", Type: wit.U8{}},
			{Name: "port", Type: wit.U16{}},
			{Name: "key", Type: wit.U64{}},
			{Name: "octets", Type: octets},
			{Name: "maybe", Type: maybe},
		}},
	}
	return &wit.TypeDef{Kind: &wit.List{Type: recordType}}
}

func compileMixedFastFallbackList(t *testing.T) *CompiledType {
	t.Helper()
	ct, err := NewCompiler().Compile(mixedFastFallbackWIT(), reflect.TypeOf([]mixedFastFallbackRecord{}))
	if err != nil {
		t.Fatalf("compile mixed record list: %v", err)
	}
	return ct
}

func lowerRecordList(t *testing.T, ct *CompiledType, ptr unsafe.Pointer, mem Memory, alloc Allocator) (dataAddr, length uint32) {
	t.Helper()
	stack := make([]uint64, 2)
	n, err := NewEncoder().LowerToStack(ct, ptr, stack, mem, alloc)
	if err != nil {
		t.Fatalf("LowerToStack: %v", err)
	}
	if n != 2 {
		t.Fatalf("consumed=%d, want 2", n)
	}
	if stack[1] == 0 {
		t.Fatal("encoded list length is 0")
	}
	return uint32(stack[0]), uint32(stack[1])
}

func readRecord(t *testing.T, mem Memory, addr, size uint32) []byte {
	t.Helper()
	raw, err := mem.Read(addr, size)
	if err != nil {
		t.Fatalf("read record at %d: %v", addr, err)
	}
	if uint32(len(raw)) != size {
		t.Fatalf("short record read: got %d, want %d", len(raw), size)
	}
	return raw
}

func assertListU8(t *testing.T, mem Memory, raw []byte, off uint32, want []byte) {
	t.Helper()
	ptr := binary.LittleEndian.Uint32(raw[off : off+4])
	n := binary.LittleEndian.Uint32(raw[off+4 : off+8])
	if n != uint32(len(want)) {
		t.Fatalf("list<u8> len=%d, want %d", n, len(want))
	}
	if n == 0 {
		if ptr != 0 {
			t.Fatalf("empty list ptr=%d, want 0", ptr)
		}
		return
	}
	got, err := mem.Read(ptr, n)
	if err != nil {
		t.Fatalf("read list<u8> payload: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("list<u8> payload=%q, want %q", got, want)
	}
}

func assertString(t *testing.T, mem Memory, raw []byte, off uint32, want string) {
	t.Helper()
	ptr := binary.LittleEndian.Uint32(raw[off : off+4])
	n := binary.LittleEndian.Uint32(raw[off+4 : off+8])
	if n != uint32(len(want)) {
		t.Fatalf("string len=%d, want %d", n, len(want))
	}
	if n == 0 {
		return
	}
	got, err := mem.Read(ptr, n)
	if err != nil {
		t.Fatalf("read string payload: %v", err)
	}
	if string(got) != want {
		t.Fatalf("string=%q, want %q", got, want)
	}
}

func assertListString(t *testing.T, mem Memory, raw []byte, off uint32, want []string) {
	t.Helper()
	ptr := binary.LittleEndian.Uint32(raw[off : off+4])
	n := binary.LittleEndian.Uint32(raw[off+4 : off+8])
	if n != uint32(len(want)) {
		t.Fatalf("list<string> len=%d, want %d", n, len(want))
	}
	if n == 0 {
		return
	}
	meta, err := mem.Read(ptr, n*8)
	if err != nil {
		t.Fatalf("read list<string> meta: %v", err)
	}
	for i, s := range want {
		assertString(t, mem, meta, uint32(i)*8, s)
	}
}

func ipv4Want() ipv4SocketAddress {
	return ipv4SocketAddress{Port: 38885, Address: [4]byte{127, 0, 0, 1}}
}

func ipv6Want() ipv6SocketAddress {
	return ipv6SocketAddress{
		Port:     443,
		FlowInfo: 0x11223344,
		Address:  [8]uint16{0xfe80, 0, 0, 0, 0, 0, 0, 1},
		ScopeID:  7,
	}
}

func assertIPv4Socket(t *testing.T, raw []byte, variantOff uint32, want ipv4SocketAddress) {
	t.Helper()
	if raw[variantOff] != 0 {
		t.Fatalf("variant tag at %d=%d, want 0 (ipv4)", variantOff, raw[variantOff])
	}
	payload := variantOff + 4
	port := binary.LittleEndian.Uint16(raw[payload : payload+2])
	if port != want.Port {
		t.Fatalf("ipv4 port=%d, want %d", port, want.Port)
	}
	addrOff := payload + 2
	got := [4]byte{raw[addrOff], raw[addrOff+1], raw[addrOff+2], raw[addrOff+3]}
	if got != want.Address {
		t.Fatalf("ipv4 address=%v, want %v", got, want.Address)
	}
}

func assertIPv6Socket(t *testing.T, raw []byte, variantOff uint32, want ipv6SocketAddress) {
	t.Helper()
	if raw[variantOff] != 1 {
		t.Fatalf("variant tag at %d=%d, want 1 (ipv6)", variantOff, raw[variantOff])
	}
	payload := variantOff + 4
	port := binary.LittleEndian.Uint16(raw[payload : payload+2])
	if port != want.Port {
		t.Fatalf("ipv6 port=%d, want %d", port, want.Port)
	}
	flow := binary.LittleEndian.Uint32(raw[payload+4 : payload+8])
	if flow != want.FlowInfo {
		t.Fatalf("ipv6 flow-info=0x%x, want 0x%x", flow, want.FlowInfo)
	}
	for i, part := range want.Address {
		off := payload + 8 + uint32(i)*2
		got := binary.LittleEndian.Uint16(raw[off : off+2])
		if got != part {
			t.Fatalf("ipv6 address[%d]=0x%x, want 0x%x", i, got, part)
		}
	}
	scope := binary.LittleEndian.Uint32(raw[payload+24 : payload+28])
	if scope != want.ScopeID {
		t.Fatalf("ipv6 scope-id=%d, want %d", scope, want.ScopeID)
	}
}

func TestLowerListToStack_OutgoingDatagramIPv4(t *testing.T) {
	ct := compileOutgoingDatagramList(t)
	if ct.ElemType.WitSize != 44 {
		t.Fatalf("outgoing-datagram WitSize=%d, want 44", ct.ElemType.WitSize)
	}
	dataField := datagramField(t, ct, "data")
	remote := datagramField(t, ct, "remote-address")
	if dataField.Type.Kind != KindList {
		t.Fatalf("data kind=%v, want list", dataField.Type.Kind)
	}
	if remote.Type.Kind != KindOption {
		t.Fatalf("remote-address kind=%v, want option", remote.Type.Kind)
	}
	if remote.WitOffset != 8 {
		t.Fatalf("remote-address WitOffset=%d, want 8", remote.WitOffset)
	}

	want := ipv4Want()
	input := []outgoingDatagram{{
		Data: []byte("hello-udp"),
		RemoteAddress: &ipSocketAddress{
			IPv4: &want,
		},
	}}

	mem := newTestMemory(8192)
	alloc := newTestAllocator(mem.data, 64)
	dataAddr, length := lowerRecordList(t, ct, unsafe.Pointer(&input), mem, alloc)
	if length != 1 {
		t.Fatalf("length=%d, want 1", length)
	}

	raw := readRecord(t, mem, dataAddr, ct.ElemType.WitSize)
	assertListU8(t, mem, raw, dataField.WitOffset, []byte("hello-udp"))
	if raw[remote.WitOffset] != 1 {
		t.Fatalf("option tag at %d=%d, want 1 (Some)", remote.WitOffset, raw[remote.WitOffset])
	}
	assertIPv4Socket(t, raw, remote.WitOffset+4, want)
}

func TestLowerListToStack_OutgoingDatagramNone(t *testing.T) {
	ct := compileOutgoingDatagramList(t)
	remote := datagramField(t, ct, "remote-address")
	input := []outgoingDatagram{{
		Data:          []byte("anon"),
		RemoteAddress: nil,
	}}

	mem := newTestMemory(8192)
	alloc := newTestAllocator(mem.data, 64)
	dataAddr, length := lowerRecordList(t, ct, unsafe.Pointer(&input), mem, alloc)
	if length != 1 {
		t.Fatalf("length=%d, want 1", length)
	}

	raw := readRecord(t, mem, dataAddr, ct.ElemType.WitSize)
	assertListU8(t, mem, raw, 0, []byte("anon"))
	if raw[remote.WitOffset] != 0 {
		t.Fatalf("option tag at %d=%d, want 0 (None)", remote.WitOffset, raw[remote.WitOffset])
	}
}

func TestLowerListToStack_OutgoingDatagramIPv6(t *testing.T) {
	ct := compileOutgoingDatagramList(t)
	remote := datagramField(t, ct, "remote-address")
	want := ipv6Want()
	input := []outgoingDatagram{{
		Data: []byte("v6-payload"),
		RemoteAddress: &ipSocketAddress{
			IPv6: &want,
		},
	}}

	mem := newTestMemory(8192)
	alloc := newTestAllocator(mem.data, 64)
	dataAddr, _ := lowerRecordList(t, ct, unsafe.Pointer(&input), mem, alloc)
	raw := readRecord(t, mem, dataAddr, ct.ElemType.WitSize)
	assertListU8(t, mem, raw, 0, []byte("v6-payload"))
	if raw[remote.WitOffset] != 1 {
		t.Fatalf("option tag at %d=%d, want 1 (Some)", remote.WitOffset, raw[remote.WitOffset])
	}
	assertIPv6Socket(t, raw, remote.WitOffset+4, want)
}

func TestLowerListToStack_OutgoingDatagramSomeAndNone(t *testing.T) {
	ct := compileOutgoingDatagramList(t)
	if ct.ElemType.WitSize != 44 {
		t.Fatalf("outgoing-datagram WitSize=%d, want 44", ct.ElemType.WitSize)
	}
	remote := datagramField(t, ct, "remote-address")
	want := ipv4Want()
	input := []outgoingDatagram{
		{
			Data:          []byte("first"),
			RemoteAddress: &ipSocketAddress{IPv4: &want},
		},
		{
			Data:          []byte("none"),
			RemoteAddress: nil,
		},
	}

	mem := newTestMemory(8192)
	alloc := newTestAllocator(mem.data, 64)
	dataAddr, length := lowerRecordList(t, ct, unsafe.Pointer(&input), mem, alloc)
	if length != 2 {
		t.Fatalf("length=%d, want 2", length)
	}

	size := ct.ElemType.WitSize
	first := readRecord(t, mem, dataAddr, size)
	assertListU8(t, mem, first, 0, []byte("first"))
	if first[remote.WitOffset] != 1 {
		t.Fatalf("first option tag=%d, want 1", first[remote.WitOffset])
	}
	assertIPv4Socket(t, first, remote.WitOffset+4, want)

	second := readRecord(t, mem, dataAddr+size, size)
	assertListU8(t, mem, second, 0, []byte("none"))
	if second[remote.WitOffset] != 0 {
		t.Fatalf("second option tag=%d, want 0", second[remote.WitOffset])
	}
}

func TestLowerListToStack_IncomingDatagramIPv4(t *testing.T) {
	ct := compileIncomingDatagramList(t)
	if ct.ElemType.Kind != KindRecord {
		t.Fatalf("elem kind=%v, want record", ct.ElemType.Kind)
	}
	if ct.ElemType.WitSize != 40 {
		t.Fatalf("incoming-datagram WitSize=%d, want 40", ct.ElemType.WitSize)
	}
	dataField := datagramField(t, ct, "data")
	remote := datagramField(t, ct, "remote-address")
	if dataField.Type.Kind != KindList {
		t.Fatalf("data kind=%v, want list", dataField.Type.Kind)
	}
	if remote.Type.Kind != KindVariant {
		t.Fatalf("remote-address kind=%v, want variant", remote.Type.Kind)
	}
	if remote.WitOffset != 8 {
		t.Fatalf("remote-address WitOffset=%d, want 8", remote.WitOffset)
	}

	want := ipv4Want()
	input := []incomingDatagram{{
		Data:          []byte{0xde, 0xad, 0xbe, 0xef},
		RemoteAddress: ipSocketAddress{IPv4: &want},
	}}

	mem := newTestMemory(8192)
	alloc := newTestAllocator(mem.data, 64)
	dataAddr, length := lowerRecordList(t, ct, unsafe.Pointer(&input), mem, alloc)
	if length != 1 {
		t.Fatalf("length=%d, want 1", length)
	}

	raw := readRecord(t, mem, dataAddr, ct.ElemType.WitSize)
	assertListU8(t, mem, raw, dataField.WitOffset, []byte{0xde, 0xad, 0xbe, 0xef})
	assertIPv4Socket(t, raw, remote.WitOffset, want)
}

func TestLowerListToStack_IncomingDatagramIPv6(t *testing.T) {
	ct := compileIncomingDatagramList(t)
	remote := datagramField(t, ct, "remote-address")
	want := ipv6Want()
	input := []incomingDatagram{{
		Data:          []byte("from-fe80"),
		RemoteAddress: ipSocketAddress{IPv6: &want},
	}}

	mem := newTestMemory(8192)
	alloc := newTestAllocator(mem.data, 64)
	dataAddr, _ := lowerRecordList(t, ct, unsafe.Pointer(&input), mem, alloc)
	raw := readRecord(t, mem, dataAddr, ct.ElemType.WitSize)
	assertListU8(t, mem, raw, 0, []byte("from-fe80"))
	assertIPv6Socket(t, raw, remote.WitOffset, want)
}

func TestLowerListToStack_IncomingDatagramIPv4AndIPv6(t *testing.T) {
	ct := compileIncomingDatagramList(t)
	v4 := ipv4Want()
	v6 := ipv6Want()
	input := []incomingDatagram{
		{Data: []byte("v4"), RemoteAddress: ipSocketAddress{IPv4: &v4}},
		{Data: []byte("v6"), RemoteAddress: ipSocketAddress{IPv6: &v6}},
	}

	mem := newTestMemory(8192)
	alloc := newTestAllocator(mem.data, 64)
	dataAddr, length := lowerRecordList(t, ct, unsafe.Pointer(&input), mem, alloc)
	if length != 2 {
		t.Fatalf("length=%d, want 2", length)
	}

	size := ct.ElemType.WitSize
	remote := datagramField(t, ct, "remote-address")
	first := readRecord(t, mem, dataAddr, size)
	assertListU8(t, mem, first, 0, []byte("v4"))
	assertIPv4Socket(t, first, remote.WitOffset, v4)

	second := readRecord(t, mem, dataAddr+size, size)
	assertListU8(t, mem, second, 0, []byte("v6"))
	assertIPv6Socket(t, second, remote.WitOffset, v6)
}

func TestLowerListToStack_MixedFastAndFallback(t *testing.T) {
	ct := compileMixedFastFallbackList(t)
	kinds := map[string]TypeKind{}
	offs := map[string]uint32{}
	for _, f := range ct.ElemType.Fields {
		kinds[f.WitName] = f.Type.Kind
		offs[f.WitName] = f.WitOffset
	}
	if kinds["id"] != KindU32 || kinds["ready"] != KindBool || kinds["name"] != KindString || kinds["tags"] != KindList {
		t.Fatalf("fast-path kinds=%v", kinds)
	}
	if kinds["flag"] != KindU8 || kinds["port"] != KindU16 || kinds["key"] != KindU64 || kinds["octets"] != KindTuple || kinds["maybe"] != KindOption {
		t.Fatalf("fallback kinds=%v", kinds)
	}

	some := uint32(42)
	input := []mixedFastFallbackRecord{
		{
			ID:     7,
			Ready:  true,
			Name:   "alpha",
			Tags:   []string{"udp", "in"},
			Flag:   9,
			Port:   38885,
			Key:    0x0102030405060708,
			Octets: [4]byte{10, 1, 2, 3},
			Maybe:  &some,
		},
		{
			ID:     8,
			Ready:  false,
			Name:   "",
			Tags:   nil,
			Flag:   0,
			Port:   443,
			Key:    99,
			Octets: [4]byte{127, 0, 0, 1},
			Maybe:  nil,
		},
	}

	mem := newTestMemory(8192)
	alloc := newTestAllocator(mem.data, 64)
	dataAddr, length := lowerRecordList(t, ct, unsafe.Pointer(&input), mem, alloc)
	if length != 2 {
		t.Fatalf("length=%d, want 2", length)
	}

	size := ct.ElemType.WitSize
	first := readRecord(t, mem, dataAddr, size)
	if binary.LittleEndian.Uint32(first[offs["id"]:offs["id"]+4]) != 7 {
		t.Fatalf("id=%d, want 7", binary.LittleEndian.Uint32(first[offs["id"]:offs["id"]+4]))
	}
	if first[offs["ready"]] != 1 {
		t.Fatalf("ready=%d, want 1", first[offs["ready"]])
	}
	assertString(t, mem, first, offs["name"], "alpha")
	assertListString(t, mem, first, offs["tags"], []string{"udp", "in"})
	if first[offs["flag"]] != 9 {
		t.Fatalf("flag=%d, want 9", first[offs["flag"]])
	}
	if binary.LittleEndian.Uint16(first[offs["port"]:offs["port"]+2]) != 38885 {
		t.Fatalf("port=%d, want 38885", binary.LittleEndian.Uint16(first[offs["port"]:offs["port"]+2]))
	}
	if binary.LittleEndian.Uint64(first[offs["key"]:offs["key"]+8]) != 0x0102030405060708 {
		t.Fatalf("key=0x%x, want 0x0102030405060708", binary.LittleEndian.Uint64(first[offs["key"]:offs["key"]+8]))
	}
	gotOctets := [4]byte{first[offs["octets"]], first[offs["octets"]+1], first[offs["octets"]+2], first[offs["octets"]+3]}
	if gotOctets != [4]byte{10, 1, 2, 3} {
		t.Fatalf("octets=%v, want [10 1 2 3]", gotOctets)
	}
	if first[offs["maybe"]] != 1 {
		t.Fatalf("maybe tag=%d, want 1", first[offs["maybe"]])
	}
	maybePayload := offs["maybe"] + 4
	if binary.LittleEndian.Uint32(first[maybePayload:maybePayload+4]) != 42 {
		t.Fatalf("maybe payload=%d, want 42", binary.LittleEndian.Uint32(first[maybePayload:maybePayload+4]))
	}

	second := readRecord(t, mem, dataAddr+size, size)
	if binary.LittleEndian.Uint32(second[offs["id"]:offs["id"]+4]) != 8 {
		t.Fatalf("second id=%d, want 8", binary.LittleEndian.Uint32(second[offs["id"]:offs["id"]+4]))
	}
	if second[offs["ready"]] != 0 {
		t.Fatalf("second ready=%d, want 0", second[offs["ready"]])
	}
	assertString(t, mem, second, offs["name"], "")
	assertListString(t, mem, second, offs["tags"], nil)
	if second[offs["flag"]] != 0 {
		t.Fatalf("second flag=%d, want 0", second[offs["flag"]])
	}
	if binary.LittleEndian.Uint16(second[offs["port"]:offs["port"]+2]) != 443 {
		t.Fatalf("second port=%d, want 443", binary.LittleEndian.Uint16(second[offs["port"]:offs["port"]+2]))
	}
	if binary.LittleEndian.Uint64(second[offs["key"]:offs["key"]+8]) != 99 {
		t.Fatalf("second key=%d, want 99", binary.LittleEndian.Uint64(second[offs["key"]:offs["key"]+8]))
	}
	gotOctets = [4]byte{second[offs["octets"]], second[offs["octets"]+1], second[offs["octets"]+2], second[offs["octets"]+3]}
	if gotOctets != [4]byte{127, 0, 0, 1} {
		t.Fatalf("second octets=%v, want [127 0 0 1]", gotOctets)
	}
	if second[offs["maybe"]] != 0 {
		t.Fatalf("second maybe tag=%d, want 0 (None)", second[offs["maybe"]])
	}
}

func TestLowerListToStack_RecordNonU32Scalars(t *testing.T) {
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
	mem := newTestMemory(4096)
	alloc := newTestAllocator(mem.data, 64)
	dataAddr, length := lowerRecordList(t, ct, unsafe.Pointer(&input), mem, alloc)
	if length != 2 {
		t.Fatalf("length=%d, want 2", length)
	}

	size := ct.ElemType.WitSize
	for i, rec := range input {
		raw := readRecord(t, mem, dataAddr+uint32(i)*size, size)
		if raw[offs["flag"]] != rec.Flag {
			t.Fatalf("[%d] flag=%d, want %d", i, raw[offs["flag"]], rec.Flag)
		}
		if binary.LittleEndian.Uint16(raw[offs["port"]:offs["port"]+2]) != rec.Port {
			t.Fatalf("[%d] port=%d, want %d", i, binary.LittleEndian.Uint16(raw[offs["port"]:offs["port"]+2]), rec.Port)
		}
		if binary.LittleEndian.Uint64(raw[offs["key"]:offs["key"]+8]) != rec.Key {
			t.Fatalf("[%d] key=0x%x, want 0x%x", i, binary.LittleEndian.Uint64(raw[offs["key"]:offs["key"]+8]), rec.Key)
		}
		if binary.LittleEndian.Uint32(raw[offs["id"]:offs["id"]+4]) != rec.ID {
			t.Fatalf("[%d] id=%d, want %d", i, binary.LittleEndian.Uint32(raw[offs["id"]:offs["id"]+4]), rec.ID)
		}
	}
}
