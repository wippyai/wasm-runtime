package component

import (
	"testing"
)

func TestParseCanonSimple(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name: "lower with no options",
			// vec(1), lower(01 00), func(0), opts(0)
			data:    []byte{0x01, 0x01, 0x00, 0x00, 0x00},
			wantErr: false,
		},
		{
			name: "resource.drop",
			// vec(1), resource.drop(03), resource(19)
			data:    []byte{0x01, 0x03, 0x13},
			wantErr: false,
		},
		{
			name: "resource.new",
			// vec(1), resource.new(02), resource(5)
			data:    []byte{0x01, 0x02, 0x05},
			wantErr: false,
		},
		{
			name: "resource.rep",
			// vec(1), resource.rep(04), resource(7)
			data:    []byte{0x01, 0x04, 0x07},
			wantErr: false,
		},
		{
			name: "lower with options",
			// vec(1), lower(01 00), func(5), opts(2), Memory(03 00), UTF8(00)
			data:    []byte{0x01, 0x01, 0x00, 0x05, 0x02, 0x03, 0x00, 0x00},
			wantErr: false,
		},
		{
			name: "previously failing section - lower func 2",
			// vec(1), lower(01 00), func(2), opts(0)
			data:    []byte{0x01, 0x01, 0x00, 0x02, 0x00},
			wantErr: false,
		},
		{
			name: "lift with no options",
			// vec(1), lift(00 00), core_func(43), opts(0), type(23)
			data:    []byte{0x01, 0x00, 0x00, 0x2b, 0x00, 0x17},
			wantErr: false,
		},
		{
			name: "lift with memory and realloc",
			// vec(1), lift(00 00), core_func(44), opts(3), Memory(03 00), Realloc(04 14), UTF8(00), type(24)
			data:    []byte{0x01, 0x00, 0x00, 0x2c, 0x03, 0x03, 0x00, 0x04, 0x14, 0x00, 0x18},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canons, err := ParseCanonSectionEntries(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCanonSectionEntries() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(canons) != 1 || canons[0] == nil {
					t.Fatalf("ParseCanonSectionEntries() returned %d canons, want 1", len(canons))
				}
				canon := canons[0]
				t.Logf("Parsed canon: kind=%d, funcIdx=%d, opts=%d",
					canon.Kind, canon.FuncIndex, len(canon.Options))
			}
		})
	}
}

func TestCanonDef_GetMemoryIndex(t *testing.T) {
	// No memory option - default 0
	canon := &CanonDef{Options: []CanonOption{}}
	if canon.GetMemoryIndex() != 0 {
		t.Errorf("GetMemoryIndex() = %d, want 0 (default)", canon.GetMemoryIndex())
	}

	// With memory option
	canon = &CanonDef{Options: []CanonOption{
		{Kind: CanonOptUTF8},
		{Kind: CanonOptMemory, Index: 5},
	}}
	if canon.GetMemoryIndex() != 5 {
		t.Errorf("GetMemoryIndex() = %d, want 5", canon.GetMemoryIndex())
	}
}

func TestCanonDef_GetReallocIndex(t *testing.T) {
	// No realloc option - default -1
	canon := &CanonDef{Options: []CanonOption{}}
	if canon.GetReallocIndex() != -1 {
		t.Errorf("GetReallocIndex() = %d, want -1 (default)", canon.GetReallocIndex())
	}

	// With realloc option
	canon = &CanonDef{Options: []CanonOption{
		{Kind: CanonOptRealloc, Index: 3},
	}}
	if canon.GetReallocIndex() != 3 {
		t.Errorf("GetReallocIndex() = %d, want 3", canon.GetReallocIndex())
	}
}

func TestCanonDef_GetStringEncoding(t *testing.T) {
	tests := []struct {
		name     string
		options  []CanonOption
		expected byte
	}{
		{"default (no option)", []CanonOption{}, 0}, // UTF-8 default
		{"UTF-8", []CanonOption{{Kind: CanonOptUTF8}}, 0},
		{"UTF-16", []CanonOption{{Kind: CanonOptUTF16}}, 1},
		{"compact UTF-16", []CanonOption{{Kind: CanonOptCompactUTF16}}, 2},
		{"with other options", []CanonOption{
			{Kind: CanonOptMemory, Index: 1},
			{Kind: CanonOptUTF16},
			{Kind: CanonOptRealloc, Index: 2},
		}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canon := &CanonDef{Options: tt.options}
			if canon.GetStringEncoding() != tt.expected {
				t.Errorf("GetStringEncoding() = %d, want %d", canon.GetStringEncoding(), tt.expected)
			}
		})
	}
}

func TestParseCanonSectionEntries_VectorOfTwoLowers(t *testing.T) {
	data := []byte{
		0x02,                   // vec count = 2
		0x01, 0x00, 0x00, 0x00, // lower func 0, no opts
		0x01, 0x00, 0x02, 0x00, // lower func 2, no opts
	}

	canons, err := ParseCanonSectionEntries(data)
	if err != nil {
		t.Fatalf("ParseCanonSectionEntries() error = %v", err)
	}
	if len(canons) != 2 {
		t.Fatalf("len = %d, want 2", len(canons))
	}
	if canons[0].Kind != CanonLower || canons[0].FuncIndex != 0 {
		t.Errorf("canons[0] = kind %d func %d, want lower func 0", canons[0].Kind, canons[0].FuncIndex)
	}
	if canons[1].Kind != CanonLower || canons[1].FuncIndex != 2 {
		t.Errorf("canons[1] = kind %d func %d, want lower func 2", canons[1].Kind, canons[1].FuncIndex)
	}
}

func TestParseCanonSectionEntries_EmptyVector(t *testing.T) {
	canons, err := ParseCanonSectionEntries([]byte{0x00})
	if err != nil {
		t.Fatalf("empty vec: %v", err)
	}
	if len(canons) != 0 {
		t.Fatalf("len = %d, want 0", len(canons))
	}
}

func TestParseCanonSectionEntries_TrailingBytes(t *testing.T) {
	data := []byte{
		0x01,
		0x01, 0x00, 0x00, 0x00,
		0x00,
	}
	if _, err := ParseCanonSectionEntries(data); err == nil {
		t.Fatal("expected error for trailing bytes")
	}
}

func TestParseCanonSectionEntries_TruncatedVector(t *testing.T) {
	data := []byte{
		0x02,
		0x01, 0x00, 0x00, 0x00,
	}
	if _, err := ParseCanonSectionEntries(data); err == nil {
		t.Fatal("expected error for truncated vector")
	}
}

func TestParseCanonSectionEntries_UnknownKind(t *testing.T) {
	data := []byte{
		0x01, // vec count = 1
		0xFF, // unknown kind
	}

	_, err := ParseCanonSectionEntries(data)
	if err == nil {
		t.Error("expected error for unknown kind")
	}
}

func TestParseCanonSectionEntries_LiftInvalidSubKind(t *testing.T) {
	data := []byte{
		0x01, // vec count = 1
		0x00, // kind = lift
		0x01, // subkind = 1 (invalid, must be 0)
	}

	_, err := ParseCanonSectionEntries(data)
	if err == nil {
		t.Error("expected error for invalid lift sub-kind")
	}
}

func TestParseCanonSectionEntries_LowerInvalidSubKind(t *testing.T) {
	data := []byte{
		0x01, // vec count = 1
		0x01, // kind = lower
		0x01, // subkind = 1 (invalid, must be 0)
	}

	_, err := ParseCanonSectionEntries(data)
	if err == nil {
		t.Error("expected error for invalid lower sub-kind")
	}
}

func TestReadCanonOption_AllKinds(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected CanonOption
	}{
		{"UTF8", []byte{0x00}, CanonOption{Kind: CanonOptUTF8, Encoding: 0}},
		{"UTF16", []byte{0x01}, CanonOption{Kind: CanonOptUTF16, Encoding: 1}},
		{"CompactUTF16", []byte{0x02}, CanonOption{Kind: CanonOptCompactUTF16, Encoding: 2}},
		{"Memory", []byte{0x03, 0x05}, CanonOption{Kind: CanonOptMemory, Index: 5}},
		{"Realloc", []byte{0x04, 0x03}, CanonOption{Kind: CanonOptRealloc, Index: 3}},
		{"PostReturn", []byte{0x05, 0x07}, CanonOption{Kind: CanonOptPostReturn, Index: 7}},
		{"Async", []byte{0x06}, CanonOption{Kind: CanonOptAsync}},
		{"Callback", []byte{0x07, 0x02}, CanonOption{Kind: CanonOptCallback, Index: 2}},
		{"CoreType", []byte{0x08, 0x01}, CanonOption{Kind: CanonOptCoreType, Index: 1}},
		{"Gc", []byte{0x09}, CanonOption{Kind: CanonOptGc}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := getReader(tt.data)
			defer putReader(r)

			opt, err := readCanonOption(r)
			if err != nil {
				t.Fatalf("readCanonOption failed: %v", err)
			}
			if opt.Kind != tt.expected.Kind {
				t.Errorf("Kind = %d, want %d", opt.Kind, tt.expected.Kind)
			}
			if opt.Index != tt.expected.Index {
				t.Errorf("Index = %d, want %d", opt.Index, tt.expected.Index)
			}
		})
	}
}

func TestReadCanonOption_UnknownKind(t *testing.T) {
	data := []byte{0xFF}
	r := getReader(data)
	defer putReader(r)

	_, err := readCanonOption(r)
	if err == nil {
		t.Error("expected error for unknown option kind")
	}
}
