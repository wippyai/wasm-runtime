package wat

import (
	"github.com/wippyai/wasm-runtime/wat/internal/encoder"
	"github.com/wippyai/wasm-runtime/wat/internal/parser"
	"github.com/wippyai/wasm-runtime/wat/internal/token"
)

func Compile(source string) ([]byte, error) {
	tokens := token.Tokenize(source)
	p := parser.New(tokens)
	mod, err := p.Parse()
	if err != nil {
		return nil, err
	}
	return encoder.Encode(mod), nil
}
