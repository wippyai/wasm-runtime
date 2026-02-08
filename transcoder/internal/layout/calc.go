package layout

import (
	"github.com/wippyai/wasm-runtime/transcoder/internal/abi"
	"go.bytecodealliance.org/wit"
)

type Calculator struct {
	cache map[*wit.TypeDef]Info
}

func NewCalculator() *Calculator {
	return &Calculator{
		cache: make(map[*wit.TypeDef]Info),
	}
}

func (c *Calculator) Calculate(t wit.Type) Info {
	switch typ := t.(type) {
	case wit.U8, wit.S8, wit.Bool:
		return Info{Size: 1, Align: 1}
	case wit.U16, wit.S16:
		return Info{Size: 2, Align: 2}
	case wit.U32, wit.S32, wit.F32, wit.Char:
		return Info{Size: 4, Align: 4}
	case wit.U64, wit.S64, wit.F64:
		return Info{Size: 8, Align: 8}
	case wit.String:
		return Info{Size: 8, Align: 4} // [ptr: u32, len: u32]
	case *wit.TypeDef:
		return c.calculateTypeDef(typ)
	default:
		return Info{Size: 0, Align: 1}
	}
}

func (c *Calculator) calculateTypeDef(t *wit.TypeDef) Info {
	if cached, ok := c.cache[t]; ok {
		return cached
	}

	var info Info

	switch kind := t.Kind.(type) {
	case *wit.Record:
		info = c.calculateRecord(kind)
	case *wit.Variant:
		info = c.calculateVariant(kind)
	case *wit.Enum:
		info = c.calculateEnum(kind)
	case *wit.List:
		info = Info{Size: 8, Align: 4}
	case *wit.Option:
		info = c.calculateOption(kind)
	case *wit.Result:
		info = c.calculateResult(kind)
	case *wit.Tuple:
		info = c.calculateTuple(kind)
	case *wit.Flags:
		info = c.calculateFlags(kind)
	case wit.Type:
		info = c.Calculate(kind)
	default:
		info = Info{Size: 0, Align: 1}
	}

	c.cache[t] = info
	return info
}

func (c *Calculator) calculateRecord(r *wit.Record) Info {
	if len(r.Fields) == 0 {
		return Info{Size: 0, Align: 1}
	}

	fieldOffs := make(map[string]uint32)
	maxAlign := uint32(1)
	offset := uint32(0)

	for _, field := range r.Fields {
		fieldLayout := c.Calculate(field.Type)

		offset = abi.AlignTo(offset, fieldLayout.Align)
		fieldOffs[field.Name] = offset

		if fieldLayout.Align > maxAlign {
			maxAlign = fieldLayout.Align
		}

		offset += fieldLayout.Size
	}

	totalSize := abi.AlignTo(offset, maxAlign)

	return Info{
		Size:      totalSize,
		Align:     maxAlign,
		FieldOffs: fieldOffs,
	}
}

func (c *Calculator) calculateVariant(v *wit.Variant) Info {
	if len(v.Cases) == 0 {
		return Info{Size: 0, Align: 1}
	}

	discSize := abi.DiscriminantSize(len(v.Cases))

	maxAlign := discSize
	maxSize := uint32(0)

	for _, cs := range v.Cases {
		if cs.Type != nil {
			caseLayout := c.Calculate(cs.Type)
			if caseLayout.Align > maxAlign {
				maxAlign = caseLayout.Align
			}
			if caseLayout.Size > maxSize {
				maxSize = caseLayout.Size
			}
		}
	}

	payloadOffset := abi.AlignTo(discSize, maxAlign)
	totalSize := abi.AlignTo(payloadOffset+maxSize, maxAlign)

	return Info{
		Size:  totalSize,
		Align: maxAlign,
	}
}

func (c *Calculator) calculateEnum(e *wit.Enum) Info {
	size := abi.DiscriminantSize(len(e.Cases))
	return Info{Size: size, Align: size}
}

func (c *Calculator) calculateOption(o *wit.Option) Info {
	innerLayout := c.Calculate(o.Type)

	payloadOffset := abi.AlignTo(1, innerLayout.Align)
	totalSize := abi.AlignTo(payloadOffset+innerLayout.Size, innerLayout.Align)

	maxAlign := innerLayout.Align
	if maxAlign < 1 {
		maxAlign = 1
	}

	return Info{
		Size:  totalSize,
		Align: maxAlign,
	}
}

func (c *Calculator) calculateResult(r *wit.Result) Info {
	okSize := uint32(0)
	okAlign := uint32(1)
	if r.OK != nil {
		okLayout := c.Calculate(r.OK)
		okSize = okLayout.Size
		okAlign = okLayout.Align
	}

	errSize := uint32(0)
	errAlign := uint32(1)
	if r.Err != nil {
		errLayout := c.Calculate(r.Err)
		errSize = errLayout.Size
		errAlign = errLayout.Align
	}

	maxAlign := okAlign
	if errAlign > maxAlign {
		maxAlign = errAlign
	}

	maxSize := okSize
	if errSize > maxSize {
		maxSize = errSize
	}

	payloadOffset := abi.AlignTo(1, maxAlign)
	totalSize := abi.AlignTo(payloadOffset+maxSize, maxAlign)

	return Info{
		Size:  totalSize,
		Align: maxAlign,
	}
}

func (c *Calculator) calculateTuple(t *wit.Tuple) Info {
	if len(t.Types) == 0 {
		return Info{Size: 0, Align: 1}
	}

	maxAlign := uint32(1)
	offset := uint32(0)

	for _, typ := range t.Types {
		elemLayout := c.Calculate(typ)
		offset = abi.AlignTo(offset, elemLayout.Align)

		if elemLayout.Align > maxAlign {
			maxAlign = elemLayout.Align
		}

		offset += elemLayout.Size
	}

	totalSize := abi.AlignTo(offset, maxAlign)

	return Info{
		Size:  totalSize,
		Align: maxAlign,
	}
}

func (c *Calculator) calculateFlags(f *wit.Flags) Info {
	numFlags := len(f.Flags)

	if numFlags == 0 {
		return Info{Size: 0, Align: 1}
	}

	if numFlags <= 8 {
		return Info{Size: 1, Align: 1}
	} else if numFlags <= 16 {
		return Info{Size: 2, Align: 2}
	} else if numFlags <= 32 {
		return Info{Size: 4, Align: 4}
	} else if numFlags <= 64 {
		return Info{Size: 8, Align: 8}
	}

	// >64 flags: multiple u32s per Canonical ABI spec
	numU32s := (numFlags + 31) / 32
	return Info{Size: uint32(numU32s * 4), Align: 4}
}
