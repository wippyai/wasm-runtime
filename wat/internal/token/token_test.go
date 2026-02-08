package token

import (
	"testing"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []Token
	}{
		{
			"empty",
			"",
			nil,
		},
		{
			"parens",
			"()",
			[]Token{{"(", LParen, 1}, {")", RParen, 1}},
		},
		{
			"module",
			"(module)",
			[]Token{{"(", LParen, 1}, {"module", Ident, 1}, {")", RParen, 1}},
		},
		{
			"whitespace",
			"  (  module  )  ",
			[]Token{{"(", LParen, 1}, {"module", Ident, 1}, {")", RParen, 1}},
		},
		{
			"newlines",
			"(\nmodule\n)",
			[]Token{{"(", LParen, 1}, {"module", Ident, 2}, {")", RParen, 3}},
		},
		{
			"identifier",
			"$foo",
			[]Token{{"$foo", Ident, 1}},
		},
		{
			"identifier_with_dots",
			"i32.const",
			[]Token{{"i32.const", Ident, 1}},
		},
		{
			"number",
			"42",
			[]Token{{"42", Number, 1}},
		},
		{
			"negative_number",
			"-42",
			[]Token{{"-42", Number, 1}},
		},
		{
			"hex_number",
			"0xFF",
			[]Token{{"0xFF", Number, 1}},
		},
		{
			"float",
			"3.14",
			[]Token{{"3.14", Number, 1}},
		},
		{
			"float_exp",
			"1e10",
			[]Token{{"1e10", Number, 1}},
		},
		{
			"float_neg_exp",
			"1e-10",
			[]Token{{"1e-10", Number, 1}},
		},
		{
			"hex_float",
			"0x1.5p10",
			[]Token{{"0x1.5p10", Number, 1}},
		},
		{
			"underscore_number",
			"1_000_000",
			[]Token{{"1_000_000", Number, 1}},
		},
		{
			"string",
			`"hello"`,
			[]Token{{"hello", String, 1}},
		},
		{
			"string_escape",
			`"hello\nworld"`,
			[]Token{{`hello\nworld`, String, 1}},
		},
		{
			"string_quote_escape",
			`"say \"hi\""`,
			[]Token{{`say \"hi\"`, String, 1}},
		},
		{
			"line_comment",
			";; comment\n(module)",
			[]Token{{"(", LParen, 2}, {"module", Ident, 2}, {")", RParen, 2}},
		},
		{
			"block_comment",
			"(; comment ;)(module)",
			[]Token{{"(", LParen, 1}, {"module", Ident, 1}, {")", RParen, 1}},
		},
		{
			"nested_block_comment",
			"(; outer (; inner ;) outer ;)(module)",
			[]Token{{"(", LParen, 1}, {"module", Ident, 1}, {")", RParen, 1}},
		},
		{
			"inf",
			"inf",
			[]Token{{"inf", Ident, 1}},
		},
		{
			"neg_inf",
			"-inf",
			[]Token{{"-inf", Ident, 1}},
		},
		{
			"nan",
			"nan",
			[]Token{{"nan", Ident, 1}},
		},
		{
			"nan_payload",
			"nan:0x1234",
			[]Token{{"nan:0x1234", Ident, 1}},
		},
		{
			"complex",
			`(module (func $add (param i32 i32) (result i32) (i32.add (local.get 0) (local.get 1))))`,
			[]Token{
				{"(", LParen, 1}, {"module", Ident, 1},
				{"(", LParen, 1}, {"func", Ident, 1}, {"$add", Ident, 1},
				{"(", LParen, 1}, {"param", Ident, 1}, {"i32", Ident, 1}, {"i32", Ident, 1}, {")", RParen, 1},
				{"(", LParen, 1}, {"result", Ident, 1}, {"i32", Ident, 1}, {")", RParen, 1},
				{"(", LParen, 1}, {"i32.add", Ident, 1},
				{"(", LParen, 1}, {"local.get", Ident, 1}, {"0", Number, 1}, {")", RParen, 1},
				{"(", LParen, 1}, {"local.get", Ident, 1}, {"1", Number, 1}, {")", RParen, 1},
				{")", RParen, 1}, {")", RParen, 1}, {")", RParen, 1},
			},
		},
		{
			"unicode_identifier",
			"$日本語",
			[]Token{{"$日本語", Ident, 1}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := Tokenize(tt.input)
			if len(tokens) != len(tt.expected) {
				t.Fatalf("token count mismatch: got %d, want %d\ngot: %v", len(tokens), len(tt.expected), tokens)
			}
			for i, tok := range tokens {
				exp := tt.expected[i]
				if tok.Type != exp.Type || tok.Value != exp.Value || tok.Line != exp.Line {
					t.Errorf("token %d mismatch:\n  got:  %+v\n  want: %+v", i, tok, exp)
				}
			}
		})
	}
}

func TestTokenTypeString(t *testing.T) {
	tests := []struct {
		want string
		typ  Type
	}{
		{"'('", LParen},
		{"')'", RParen},
		{"identifier", Ident},
		{"string", String},
		{"number", Number},
		{"unknown", Type(999)},
	}

	for _, tt := range tests {
		got := tt.typ.String()
		if got != tt.want {
			t.Errorf("Type(%d).String() = %q, want %q", tt.typ, got, tt.want)
		}
	}
}
