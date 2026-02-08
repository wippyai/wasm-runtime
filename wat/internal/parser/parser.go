package parser

import (
	"fmt"
	"strings"

	"github.com/wippyai/wasm-runtime/wat/internal/ast"
	"github.com/wippyai/wasm-runtime/wat/internal/token"
)

type Parser struct {
	mod       *ast.Module
	typeMap   map[string]uint32
	funcMap   map[string]uint32
	globalMap map[string]uint32
	memMap    map[string]uint32
	tableMap  map[string]uint32
	elemMap   map[string]uint32
	dataMap   map[string]uint32
	tokens    []token.Token
	labels    []string
	pos       int
}

func New(tokens []token.Token) *Parser {
	return &Parser{
		tokens:    tokens,
		typeMap:   make(map[string]uint32),
		funcMap:   make(map[string]uint32),
		globalMap: make(map[string]uint32),
		memMap:    make(map[string]uint32),
		tableMap:  make(map[string]uint32),
		elemMap:   make(map[string]uint32),
		dataMap:   make(map[string]uint32),
	}
}

func (p *Parser) Parse() (*ast.Module, error) {
	return p.parseModule()
}

func (p *Parser) peek() *token.Token {
	if p.pos >= len(p.tokens) {
		return nil
	}
	return &p.tokens[p.pos]
}

func (p *Parser) next() *token.Token {
	if p.pos >= len(p.tokens) {
		return nil
	}
	t := &p.tokens[p.pos]
	p.pos++
	return t
}

func (p *Parser) expect(typ token.Type) (*token.Token, error) {
	t := p.next()
	if t == nil {
		return nil, fmt.Errorf("unexpected end of input")
	}
	if t.Type != typ {
		return nil, fmt.Errorf("line %d: expected %v, got %q", t.Line, typ, t.Value)
	}
	return t, nil
}

func (p *Parser) pushLabel(name string) {
	p.labels = append(p.labels, name)
}

func (p *Parser) popLabel() {
	if len(p.labels) > 0 {
		p.labels = p.labels[:len(p.labels)-1]
	}
}

func (p *Parser) resolveLabel(name string) (uint32, bool) {
	for i := len(p.labels) - 1; i >= 0; i-- {
		if p.labels[i] == name {
			return uint32(len(p.labels) - 1 - i), true
		}
	}
	return 0, false
}

func (p *Parser) parseValType() (ast.ValType, error) {
	t, err := p.expect(token.Ident)
	if err != nil {
		return 0, err
	}
	switch t.Value {
	case "i32":
		return ast.ValTypeI32, nil
	case "i64":
		return ast.ValTypeI64, nil
	case "f32":
		return ast.ValTypeF32, nil
	case "f64":
		return ast.ValTypeF64, nil
	case "funcref":
		return ast.ValTypeFuncref, nil
	case "externref":
		return ast.ValTypeExternref, nil
	default:
		return 0, fmt.Errorf("unknown value type: %s", t.Value)
	}
}

func (p *Parser) parseIdx(nameMap map[string]uint32) (uint32, error) {
	t := p.peek()
	if t == nil {
		return 0, fmt.Errorf("expected index")
	}

	if t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
		p.next()
		if nameMap != nil {
			if idx, ok := nameMap[t.Value]; ok {
				return idx, nil
			}
			return 0, fmt.Errorf("unknown identifier: %s", t.Value)
		}
		return 0, fmt.Errorf("unexpected identifier: %s", t.Value)
	}

	return p.parseU32()
}

func (p *Parser) findOrAddType(ft ast.FuncType) uint32 {
	for i, t := range p.mod.Types {
		if t.Equal(ft) {
			return uint32(i)
		}
	}
	idx := uint32(len(p.mod.Types))
	p.mod.Types = append(p.mod.Types, ft)
	return idx
}
