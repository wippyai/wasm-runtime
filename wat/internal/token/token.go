package token

import (
	"strings"
	"unicode"
)

type Type int

const (
	LParen Type = iota
	RParen
	Ident
	String
	Number
)

func (t Type) String() string {
	switch t {
	case LParen:
		return "'('"
	case RParen:
		return "')'"
	case Ident:
		return "identifier"
	case String:
		return "string"
	case Number:
		return "number"
	}
	return "unknown"
}

type Token struct {
	Value string
	Type  Type
	Line  int
}

func Tokenize(input string) []Token {
	var tokens []Token
	line := 1
	runes := []rune(input)

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if r == '\n' {
			line++
			continue
		}
		if unicode.IsSpace(r) {
			continue
		}

		// Line comment
		if r == ';' && i+1 < len(runes) && runes[i+1] == ';' {
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			line++
			continue
		}

		// Block comment or left paren
		if r == '(' {
			if i+1 < len(runes) && runes[i+1] == ';' {
				depth := 1
				i += 2
				for i < len(runes) && depth > 0 {
					if runes[i] == '(' && i+1 < len(runes) && runes[i+1] == ';' {
						depth++
						i++
					} else if runes[i] == ';' && i+1 < len(runes) && runes[i+1] == ')' {
						depth--
						i++
					} else if runes[i] == '\n' {
						line++
					}
					i++
				}
				i--
				continue
			}
			tokens = append(tokens, Token{"(", LParen, line})
			continue
		}

		if r == ')' {
			tokens = append(tokens, Token{")", RParen, line})
			continue
		}

		// String literal
		if r == '"' {
			start := i + 1
			i++
			for i < len(runes) && runes[i] != '"' {
				if runes[i] == '\\' {
					i++
				}
				i++
			}
			tokens = append(tokens, Token{string(runes[start:i]), String, line})
			continue
		}

		// Number (including negative) or signed special float values
		if r == '-' || r == '+' || unicode.IsDigit(r) {
			start := i
			// Check for -inf, +inf, -nan, +nan
			if (r == '-' || r == '+') && i+3 <= len(runes) {
				rest := string(runes[i+1 : min(i+4, len(runes))])
				if strings.HasPrefix(rest, "inf") || strings.HasPrefix(rest, "nan") {
					i++
					for i < len(runes) && (unicode.IsLetter(runes[i]) || runes[i] == ':' || unicode.IsDigit(runes[i])) {
						i++
					}
					tokens = append(tokens, Token{string(runes[start:i]), Ident, line})
					i--
					continue
				}
			}
			if r == '-' || r == '+' {
				i++
			}
			for i < len(runes) {
				c := runes[i]
				if unicode.IsDigit(c) || c == '.' || c == 'e' || c == 'E' ||
					c == 'x' || c == 'X' || c == '_' || c == 'p' || c == 'P' ||
					(c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') ||
					(c == '-' && i > start && (runes[i-1] == 'e' || runes[i-1] == 'E' || runes[i-1] == 'p' || runes[i-1] == 'P')) ||
					(c == '+' && i > start && (runes[i-1] == 'e' || runes[i-1] == 'E' || runes[i-1] == 'p' || runes[i-1] == 'P')) {
					i++
				} else {
					break
				}
			}
			tokens = append(tokens, Token{string(runes[start:i]), Number, line})
			i--
			continue
		}

		// Identifier (including $names, keywords, and offset=/align= forms)
		if r == '$' || unicode.IsLetter(r) || r == '_' || r == '.' {
			start := i
			for i < len(runes) {
				c := runes[i]
				if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' || c == '.' || c == '$' || c == '-' || c == ':' || c == '=' {
					i++
				} else {
					break
				}
			}
			tokens = append(tokens, Token{string(runes[start:i]), Ident, line})
			i--
			continue
		}
	}

	return tokens
}
