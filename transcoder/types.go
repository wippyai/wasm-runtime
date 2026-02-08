package transcoder

import (
	"github.com/wippyai/wasm-runtime/transcoder/internal/types"
)

type TypeKind = types.Kind

const (
	KindBool    = types.KindBool
	KindU8      = types.KindU8
	KindS8      = types.KindS8
	KindU16     = types.KindU16
	KindS16     = types.KindS16
	KindU32     = types.KindU32
	KindS32     = types.KindS32
	KindU64     = types.KindU64
	KindS64     = types.KindS64
	KindF32     = types.KindF32
	KindF64     = types.KindF64
	KindChar    = types.KindChar
	KindString  = types.KindString
	KindRecord  = types.KindRecord
	KindList    = types.KindList
	KindVariant = types.KindVariant
	KindOption  = types.KindOption
	KindResult  = types.KindResult
	KindTuple   = types.KindTuple
	KindEnum    = types.KindEnum
	KindFlags   = types.KindFlags
	KindOwn     = types.KindOwn
	KindBorrow  = types.KindBorrow
)

type CompiledType = types.CompiledType
type CompiledField = types.Field
type CompiledCase = types.Case
