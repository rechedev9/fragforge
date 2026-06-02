package parser

import (
	"errors"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
)

func parseToEnd(p demoinfocs.Parser) error {
	err := p.ParseToEnd()
	if errors.Is(err, demoinfocs.ErrUnexpectedEndOfDemo) {
		return nil
	}
	return err
}
