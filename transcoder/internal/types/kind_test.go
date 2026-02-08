package types //nolint:revive // package name is used by internal consumers

import "testing"

func TestKindString(t *testing.T) {
	tests := []struct {
		want string
		kind Kind
	}{
		{"bool", KindBool},
		{"u8", KindU8},
		{"s8", KindS8},
		{"u16", KindU16},
		{"s16", KindS16},
		{"u32", KindU32},
		{"s32", KindS32},
		{"u64", KindU64},
		{"s64", KindS64},
		{"f32", KindF32},
		{"f64", KindF64},
		{"char", KindChar},
		{"string", KindString},
		{"record", KindRecord},
		{"list", KindList},
		{"variant", KindVariant},
		{"option", KindOption},
		{"result", KindResult},
		{"tuple", KindTuple},
		{"enum", KindEnum},
		{"flags", KindFlags},
		{"own", KindOwn},
		{"borrow", KindBorrow},
		{"unknown", Kind(255)},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.kind.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestKindIsPrimitive(t *testing.T) {
	primitives := []Kind{
		KindBool, KindU8, KindS8, KindU16, KindS16,
		KindU32, KindS32, KindU64, KindS64,
		KindF32, KindF64, KindChar,
	}
	for _, k := range primitives {
		if !k.IsPrimitive() {
			t.Errorf("%s should be primitive", k)
		}
	}

	nonPrimitives := []Kind{
		KindString, KindRecord, KindList, KindVariant,
		KindOption, KindResult, KindTuple, KindEnum, KindFlags,
	}
	for _, k := range nonPrimitives {
		if k.IsPrimitive() {
			t.Errorf("%s should not be primitive", k)
		}
	}
}

func TestKindFlatCount(t *testing.T) {
	tests := []struct {
		kind Kind
		want int
	}{
		{KindBool, 1},
		{KindU32, 1},
		{KindString, 2},
		{KindList, 2},
		{KindRecord, 1},
	}

	for _, tc := range tests {
		t.Run(tc.kind.String(), func(t *testing.T) {
			if got := tc.kind.FlatCount(); got != tc.want {
				t.Errorf("FlatCount() = %d, want %d", got, tc.want)
			}
		})
	}
}
