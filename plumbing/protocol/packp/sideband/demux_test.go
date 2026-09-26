package sideband

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

type SidebandSuite struct {
	suite.Suite
}

func TestSidebandSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(SidebandSuite))
}

func (s *SidebandSuite) TestDecode() {
	expected := []byte("abcdefghijklmnopqrstuvwxyz")

	buf := bytes.NewBuffer(nil)
	pktline.Write(buf, PackData.WithPayload(expected[0:8]))
	pktline.Write(buf, ProgressMessage.WithPayload([]byte{'F', 'O', 'O', '\n'}))
	pktline.Write(buf, PackData.WithPayload(expected[8:16]))
	pktline.Write(buf, PackData.WithPayload(expected[16:26]))

	content := make([]byte, 26)
	d := NewDemuxer(Sideband64k, buf)
	n, err := io.ReadFull(d, content)
	s.NoError(err)
	s.Equal(26, n)
	s.Equal(expected, content)
}

func (s *SidebandSuite) TestDecodeMoreThanContain() {
	expected := []byte("abcdefghijklmnopqrstuvwxyz")

	buf := bytes.NewBuffer(nil)
	pktline.Write(buf, PackData.WithPayload(expected))

	content := make([]byte, 42)
	d := NewDemuxer(Sideband64k, buf)
	n, err := io.ReadFull(d, content)
	s.ErrorIs(err, io.ErrUnexpectedEOF)
	s.Equal(26, n)
	s.Equal(expected, content[0:26])
}

func (s *SidebandSuite) TestDecodeWithError() {
	expected := []byte("abcdefghijklmnopqrstuvwxyz")

	buf := bytes.NewBuffer(nil)
	pktline.Write(buf, PackData.WithPayload(expected[0:8]))
	pktline.Write(buf, ErrorMessage.WithPayload([]byte{'F', 'O', 'O', '\n'}))
	pktline.Write(buf, PackData.WithPayload(expected[8:16]))
	pktline.Write(buf, PackData.WithPayload(expected[16:26]))

	content := make([]byte, 26)
	d := NewDemuxer(Sideband64k, buf)
	n, err := io.ReadFull(d, content)
	s.ErrorContains(err, "unexpected error: FOO\n")
	s.Equal(8, n)
	s.Equal(expected[0:8], content[0:8])
}

type mockReader struct{}

func (r *mockReader) Read([]byte) (int, error) { return 0, errors.New("foo") }

func (s *SidebandSuite) TestDecodeFromFailingReader() {
	content := make([]byte, 26)
	d := NewDemuxer(Sideband64k, &mockReader{})
	n, err := io.ReadFull(d, content)
	s.ErrorContains(err, "foo")
	s.Equal(0, n)
}

func (s *SidebandSuite) TestDecodeWithProgress() {
	expected := []byte("abcdefghijklmnopqrstuvwxyz")

	input := bytes.NewBuffer(nil)
	pktline.Write(input, PackData.WithPayload(expected[0:8]))
	pktline.Write(input, ProgressMessage.WithPayload([]byte{'F', 'O', 'O', '\n'}))
	pktline.Write(input, PackData.WithPayload(expected[8:16]))
	pktline.Write(input, PackData.WithPayload(expected[16:26]))

	output := bytes.NewBuffer(nil)
	content := make([]byte, 26)
	d := NewDemuxer(Sideband64k, input)
	d.Progress = output

	n, err := io.ReadFull(d, content)
	s.NoError(err)
	s.Equal(26, n)
	s.Equal(expected, content)

	progress, err := io.ReadAll(output)
	s.NoError(err)
	s.Equal([]byte{'F', 'O', 'O', '\n'}, progress)
}

func (s *SidebandSuite) TestDecodeFlushEOF() {
	expected := []byte("abcdefghijklmnopqrstuvwxyz")

	input := bytes.NewBuffer(nil)
	pktline.Write(input, PackData.WithPayload(expected[0:8]))
	pktline.Write(input, ProgressMessage.WithPayload([]byte{'F', 'O', 'O', '\n'}))
	pktline.Write(input, PackData.WithPayload(expected[8:16]))
	pktline.Write(input, PackData.WithPayload(expected[16:26]))
	pktline.WriteFlush(input)
	pktline.Write(input, PackData.WithPayload([]byte("bar\n")))

	output := bytes.NewBuffer(nil)
	content := bytes.NewBuffer(nil)
	d := NewDemuxer(Sideband64k, input)
	d.Progress = output

	n, err := content.ReadFrom(d)
	s.NoError(err)
	s.Equal(int64(26), n)
	s.Equal(expected, content.Bytes())

	progress, err := io.ReadAll(output)
	s.NoError(err)
	s.Equal([]byte{'F', 'O', 'O', '\n'}, progress)
}

func (s *SidebandSuite) TestDecodeWithUnknownChannel() {
	buf := bytes.NewBuffer(nil)
	pktline.Write(buf, []byte{'4', 'F', 'O', 'O', '\n'})

	content := make([]byte, 26)
	d := NewDemuxer(Sideband64k, buf)
	n, err := io.ReadFull(d, content)
	s.ErrorContains(err, "unknown channel 4FOO\n")
	s.Equal(0, n)
}

func (s *SidebandSuite) TestDecodeWithPending() {
	expected := []byte("abcdefghijklmnopqrstuvwxyz")

	buf := bytes.NewBuffer(nil)
	pktline.Write(buf, PackData.WithPayload(expected[0:8]))
	pktline.Write(buf, PackData.WithPayload(expected[8:16]))
	pktline.Write(buf, PackData.WithPayload(expected[16:26]))

	content := make([]byte, 13)
	d := NewDemuxer(Sideband64k, buf)
	n, err := io.ReadFull(d, content)
	s.NoError(err)
	s.Equal(13, n)
	s.Equal(expected[0:13], content)

	n, err = d.Read(content)
	s.NoError(err)
	s.Equal(13, n)
	s.Equal(expected[13:26], content)
}

func (s *SidebandSuite) TestDecodeErrMaxPacked() {
	buf := bytes.NewBuffer(nil)
	pktline.Write(buf, PackData.WithPayload(bytes.Repeat([]byte{'0'}, MaxPackedSize+1)))

	content := make([]byte, 13)
	d := NewDemuxer(Sideband, buf)
	n, err := io.ReadFull(d, content)
	s.ErrorIs(err, ErrMaxPackedExceeded)
	s.Equal(0, n)
}

func (s *SidebandSuite) TestDecodeMasksControlCharactersInProgress() {
	for _, tc := range []struct {
		name     string
		message  string
		expected string
	}{
		{"clear screen", "\x1b[2J\x1b[Hdone\n", "^[[2J^[[Hdone\n"},
		{"cursor movement", "\x1b[3A\x1b[Kgone\n", "^[[3A^[[Kgone\n"},
		{"operating system command", "\x1b]0;title\x07\n", "^[]0;title^G\n"},
		{"c0 controls and delete", "\x00\x01\x08\x7f\n", "^@^A^H^?\n"},
		{"color sequences", "\x1b[1;31mfatal\x1b[0m: nope\x1b[m\n", "\x1b[1;31mfatal\x1b[0m: nope\x1b[m\n"},
		{"color with colon subparameters", "\x1b[38:2::255:0:0mred\x1b[39m\n", "\x1b[38:2::255:0:0mred\x1b[39m\n"},
		{"unterminated color sequence", "\x1b[31", "^[[31"},
		{"color sequence with foreign parameter", "\x1b[31;xm", "^[[31;xm"},
		{"escape at end", "done\x1b", "done^["},
		{"progress line breaks and tabs", "Counting: 50%\rCounting: 100%\n\tdone\n", "Counting: 50%\rCounting: 100%\n\tdone\n"},
		{"multibyte text", "Récupération 完了\n", "Récupération 完了\n"},
	} {
		s.Run(tc.name, func() {
			input := bytes.NewBuffer(nil)
			pktline.Write(input, ProgressMessage.WithPayload([]byte(tc.message)))
			pktline.Write(input, PackData.WithPayload([]byte("pack")))

			output := bytes.NewBuffer(nil)
			d := NewDemuxer(Sideband64k, input)
			d.Progress = output

			content := make([]byte, 4)
			_, err := io.ReadFull(d, content)
			s.NoError(err)
			s.Equal([]byte("pack"), content)
			s.Equal(tc.expected, output.String())
		})
	}
}

func (s *SidebandSuite) TestDecodeMasksControlCharactersInErrors() {
	s.Run("error channel", func() {
		buf := bytes.NewBuffer(nil)
		pktline.Write(buf, ErrorMessage.WithPayload([]byte("\x1b[2J\x1b[Hfatal: \x1b[31mnope\x1b[m\n")))

		d := NewDemuxer(Sideband64k, buf)
		_, err := io.ReadFull(d, make([]byte, 1))
		s.ErrorContains(err, "unexpected error: ^[[2J^[[Hfatal: \x1b[31mnope\x1b[m\n")
	})

	s.Run("unknown channel", func() {
		buf := bytes.NewBuffer(nil)
		pktline.Write(buf, []byte("4\x1b[2JFOO\n"))

		d := NewDemuxer(Sideband64k, buf)
		_, err := io.ReadFull(d, make([]byte, 1))
		s.ErrorContains(err, "unknown channel 4^[[2JFOO\n")
	})
}
