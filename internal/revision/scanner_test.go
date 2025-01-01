package revision

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/suite"
)

type ScannerSuite struct {
	suite.Suite
}

func TestScannerSuite(t *testing.T) {
	suite.Run(t, new(ScannerSuite))
}

func (s *ScannerSuite) TestReadColon() {
	scanner := newScanner(bytes.NewBufferString(":"))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal(":", data)
	s.Equal(colon, tok)
}

func (s *ScannerSuite) TestReadTilde() {
	scanner := newScanner(bytes.NewBufferString("~"))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("~", data)
	s.Equal(tilde, tok)
}

func (s *ScannerSuite) TestReadCaret() {
	scanner := newScanner(bytes.NewBufferString("^"))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("^", data)
	s.Equal(caret, tok)
}

func (s *ScannerSuite) TestReadDot() {
	scanner := newScanner(bytes.NewBufferString("."))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal(".", data)
	s.Equal(dot, tok)
}

func (s *ScannerSuite) TestReadSlash() {
	scanner := newScanner(bytes.NewBufferString("/"))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("/", data)
	s.Equal(slash, tok)
}

func (s *ScannerSuite) TestReadEOF() {
	scanner := newScanner(bytes.NewBufferString(string(rune(0))))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("", data)
	s.Equal(eof, tok)
}

func (s *ScannerSuite) TestReadNumber() {
	scanner := newScanner(bytes.NewBufferString("1234"))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("1234", data)
	s.Equal(number, tok)
}

func (s *ScannerSuite) TestReadSpace() {
	scanner := newScanner(bytes.NewBufferString(" "))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal(" ", data)
	s.Equal(space, tok)
}

func (s *ScannerSuite) TestReadControl() {
	scanner := newScanner(bytes.NewBufferString(""))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("\x01", data)
	s.Equal(control, tok)
}

func (s *ScannerSuite) TestReadOpenBrace() {
	scanner := newScanner(bytes.NewBufferString("{"))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("{", data)
	s.Equal(obrace, tok)
}

func (s *ScannerSuite) TestReadCloseBrace() {
	scanner := newScanner(bytes.NewBufferString("}"))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("}", data)
	s.Equal(cbrace, tok)
}

func (s *ScannerSuite) TestReadMinus() {
	scanner := newScanner(bytes.NewBufferString("-"))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("-", data)
	s.Equal(minus, tok)
}

func (s *ScannerSuite) TestReadAt() {
	scanner := newScanner(bytes.NewBufferString("@"))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("@", data)
	s.Equal(at, tok)
}

func (s *ScannerSuite) TestReadAntislash() {
	scanner := newScanner(bytes.NewBufferString("\\"))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("\\", data)
	s.Equal(aslash, tok)
}

func (s *ScannerSuite) TestReadQuestionMark() {
	scanner := newScanner(bytes.NewBufferString("?"))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("?", data)
	s.Equal(qmark, tok)
}

func (s *ScannerSuite) TestReadAsterisk() {
	scanner := newScanner(bytes.NewBufferString("*"))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("*", data)
	s.Equal(asterisk, tok)
}

func (s *ScannerSuite) TestReadOpenBracket() {
	scanner := newScanner(bytes.NewBufferString("["))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("[", data)
	s.Equal(obracket, tok)
}

func (s *ScannerSuite) TestReadExclamationMark() {
	scanner := newScanner(bytes.NewBufferString("!"))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("!", data)
	s.Equal(emark, tok)
}

func (s *ScannerSuite) TestReadWord() {
	scanner := newScanner(bytes.NewBufferString("abcde"))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("abcde", data)
	s.Equal(word, tok)
}

func (s *ScannerSuite) TestReadTokenError() {
	scanner := newScanner(bytes.NewBufferString("`"))
	tok, data, err := scanner.scan()

	s.NoError(err)
	s.Equal("`", data)
	s.Equal(tokenError, tok)
}
