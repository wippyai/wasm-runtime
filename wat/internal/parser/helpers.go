package parser

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/wippyai/wasm-runtime/wat/internal/token"
)

func (p *Parser) parseU32() (uint32, error) {
	t, err := p.expect(token.Number)
	if err != nil {
		return 0, err
	}
	s := strings.ReplaceAll(t.Value, "_", "")
	val, err := strconv.ParseUint(s, 0, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %s", t.Value)
	}
	return uint32(val), nil
}

func (p *Parser) parseF32() (float32, error) {
	t := p.next()
	if t == nil {
		return 0, fmt.Errorf("unexpected end of input")
	}
	if t.Type == token.Ident {
		switch t.Value {
		case "nan", "+nan":
			return float32(math.NaN()), nil
		case "inf", "+inf":
			return float32(math.Inf(1)), nil
		case "-inf":
			return float32(math.Inf(-1)), nil
		case "-nan":
			return float32(math.Float32frombits(0xFFC00000)), nil
		}
		if strings.HasPrefix(t.Value, "nan:") || strings.HasPrefix(t.Value, "+nan:") {
			return float32(math.NaN()), nil
		}
		if strings.HasPrefix(t.Value, "-nan:") {
			return float32(math.Float32frombits(0xFFC00000)), nil
		}
	}
	if t.Type != token.Number {
		return 0, fmt.Errorf("expected float, got %q", t.Value)
	}
	val, err := strconv.ParseFloat(t.Value, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid f32: %s", t.Value)
	}
	return float32(val), nil
}

func (p *Parser) parseF64() (float64, error) {
	t := p.next()
	if t == nil {
		return 0, fmt.Errorf("unexpected end of input")
	}
	if t.Type == token.Ident {
		switch t.Value {
		case "nan", "+nan":
			return math.NaN(), nil
		case "inf", "+inf":
			return math.Inf(1), nil
		case "-inf":
			return math.Inf(-1), nil
		case "-nan":
			return math.Float64frombits(0xFFF8000000000000), nil
		}
		if strings.HasPrefix(t.Value, "nan:") || strings.HasPrefix(t.Value, "+nan:") {
			return math.NaN(), nil
		}
		if strings.HasPrefix(t.Value, "-nan:") {
			return math.Float64frombits(0xFFF8000000000000), nil
		}
	}
	if t.Type != token.Number {
		return 0, fmt.Errorf("expected float, got %q", t.Value)
	}
	val, err := strconv.ParseFloat(t.Value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid f64: %s", t.Value)
	}
	return val, nil
}

func DecodeStringLiteral(s string) []byte {
	var result []byte
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			// Unicode escape: \u{XXXX}
			if runes[i+1] == 'u' && i+2 < len(runes) && runes[i+2] == '{' {
				i += 3
				start := i
				for i < len(runes) && runes[i] != '}' {
					i++
				}
				if i < len(runes) {
					hexStr := string(runes[start:i])
					if codepoint, err := parseHex(hexStr); err == nil {
						result = append(result, encodeUTF8(codepoint)...)
					}
				}
				continue
			}
			// Hex escape: \XX
			if i+2 < len(runes) && isHexDigit(runes[i+1]) && isHexDigit(runes[i+2]) {
				val := hexValue(runes[i+1])*16 + hexValue(runes[i+2])
				result = append(result, val)
				i += 2
				continue
			}
			switch runes[i+1] {
			case 'n':
				result = append(result, '\n')
				i++
			case 't':
				result = append(result, '\t')
				i++
			case 'r':
				result = append(result, '\r')
				i++
			case '\\':
				result = append(result, '\\')
				i++
			case '"':
				result = append(result, '"')
				i++
			case '\'':
				result = append(result, '\'')
				i++
			case '0':
				result = append(result, 0)
				i++
			default:
				result = append(result, byte(runes[i]))
			}
		} else {
			// Direct UTF-8 encoding for non-ASCII
			result = append(result, encodeUTF8(runes[i])...)
		}
	}
	return result
}

func parseHex(s string) (rune, error) {
	var val rune
	for _, r := range s {
		if r == '_' {
			continue
		}
		if !isHexDigit(r) {
			return 0, fmt.Errorf("invalid hex digit: %c", r)
		}
		val = val*16 + rune(hexValue(r))
	}
	return val, nil
}

func encodeUTF8(r rune) []byte {
	if r < 0x80 {
		return []byte{byte(r)}
	}
	if r < 0x800 {
		return []byte{
			byte(0xC0 | (r >> 6)),
			byte(0x80 | (r & 0x3F)),
		}
	}
	if r < 0x10000 {
		return []byte{
			byte(0xE0 | (r >> 12)),
			byte(0x80 | ((r >> 6) & 0x3F)),
			byte(0x80 | (r & 0x3F)),
		}
	}
	return []byte{
		byte(0xF0 | (r >> 18)),
		byte(0x80 | ((r >> 12) & 0x3F)),
		byte(0x80 | ((r >> 6) & 0x3F)),
		byte(0x80 | (r & 0x3F)),
	}
}

func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func hexValue(r rune) byte {
	switch {
	case r >= '0' && r <= '9':
		return byte(r - '0')
	case r >= 'a' && r <= 'f':
		return byte(r - 'a' + 10)
	case r >= 'A' && r <= 'F':
		return byte(r - 'A' + 10)
	}
	return 0
}
