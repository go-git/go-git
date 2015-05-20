package pktline

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrOverflow = errors.New("unexpected string length (overflow)")
)

type Encoder struct {
	lines []string
}

func NewEncoder() *Encoder {
	return &Encoder{make([]string, 0)}
}

func (e *Encoder) AddLine(line string) error {
	le, err := EncodeFromString(line + "\n")
	if err != nil {
		return err
	}

	e.lines = append(e.lines, le)
	return nil
}

func (e *Encoder) AddFlush() {
	e.lines = append(e.lines, "0000")
}

func (e *Encoder) GetReader() *strings.Reader {
	data := strings.Join(e.lines, "")

	return strings.NewReader(data)
}

func EncodeFromString(line string) (string, error) {
	return Encode([]byte(line))
}

func Encode(line []byte) (string, error) {
	if line == nil {
		return "0000", nil
	}

	l := len(line) + HEADER_LENGTH
	if l > MAX_LENGTH {
		return "", ErrOverflow
	}

	return fmt.Sprintf("%04x%s", l, line), nil
}
