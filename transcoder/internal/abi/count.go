package abi

import "go.bytecodealliance.org/wit"

func GetFlatCount(t wit.Type) int {
	switch t := t.(type) {
	case wit.Bool, wit.U8, wit.S8, wit.U16, wit.S16, wit.U32, wit.S32, wit.U64, wit.S64, wit.F32, wit.F64, wit.Char:
		return 1
	case wit.String:
		return 2
	case *wit.TypeDef:
		switch kind := t.Kind.(type) {
		case *wit.Record:
			count := 0
			for _, f := range kind.Fields {
				count += GetFlatCount(f.Type)
			}
			return count
		case *wit.List:
			return 2
		case *wit.Option:
			return 1 + GetFlatCount(kind.Type)
		case *wit.Tuple:
			count := 0
			for _, elem := range kind.Types {
				count += GetFlatCount(elem)
			}
			return count
		case *wit.Enum, *wit.Flags:
			return 1
		case *wit.Result:
			// 1 (discriminant) + max(ok flat count, err flat count)
			okCount := 0
			if kind.OK != nil {
				okCount = GetFlatCount(kind.OK)
			}
			errCount := 0
			if kind.Err != nil {
				errCount = GetFlatCount(kind.Err)
			}
			maxPayload := okCount
			if errCount > maxPayload {
				maxPayload = errCount
			}
			return 1 + maxPayload
		case *wit.Variant:
			// 1 (discriminant) + max payload flat count
			maxPayload := 0
			for _, c := range kind.Cases {
				if c.Type != nil {
					caseCount := GetFlatCount(c.Type)
					if caseCount > maxPayload {
						maxPayload = caseCount
					}
				}
			}
			return 1 + maxPayload
		case wit.Type:
			return GetFlatCount(kind)
		}
	}
	return 1
}

// DiscriminantSize: 1 byte for <=256 cases, 2 for <=65536, else 4 per spec.
func DiscriminantSize(numCases int) uint32 {
	if numCases <= 256 {
		return 1
	} else if numCases <= 65536 {
		return 2
	}
	return 4
}

func DiscriminantAlign(numCases int) uint32 {
	return DiscriminantSize(numCases)
}
