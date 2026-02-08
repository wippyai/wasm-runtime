package types

type Kind uint8

const (
	KindBool Kind = iota
	KindU8
	KindS8
	KindU16
	KindS16
	KindU32
	KindS32
	KindU64
	KindS64
	KindF32
	KindF64
	KindChar
	KindString
	KindRecord
	KindList
	KindVariant
	KindOption
	KindResult
	KindTuple
	KindEnum
	KindFlags
	KindOwn
	KindBorrow
)

var kindNames = [...]string{
	KindBool:    "bool",
	KindU8:      "u8",
	KindS8:      "s8",
	KindU16:     "u16",
	KindS16:     "s16",
	KindU32:     "u32",
	KindS32:     "s32",
	KindU64:     "u64",
	KindS64:     "s64",
	KindF32:     "f32",
	KindF64:     "f64",
	KindChar:    "char",
	KindString:  "string",
	KindRecord:  "record",
	KindList:    "list",
	KindVariant: "variant",
	KindOption:  "option",
	KindResult:  "result",
	KindTuple:   "tuple",
	KindEnum:    "enum",
	KindFlags:   "flags",
	KindOwn:     "own",
	KindBorrow:  "borrow",
}

func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "unknown"
}

func (k Kind) IsPrimitive() bool {
	return k <= KindChar
}

func (k Kind) FlatCount() int {
	switch k {
	case KindString, KindList:
		return 2
	default:
		return 1
	}
}
