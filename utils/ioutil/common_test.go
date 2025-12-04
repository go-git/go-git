package ioutil

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

type CommonSuite struct {
	suite.Suite
}

func TestCommonSuite(t *testing.T) {
	suite.Run(t, new(CommonSuite))
}

type closer struct {
	called int
}

func (c *closer) Close() error {
	c.called++
	return nil
}

func (s *CommonSuite) TestNonEmptyReader_Empty() {
	var buf bytes.Buffer
	r, err := NonEmptyReader(&buf)
	s.ErrorIs(err, ErrEmptyReader)
	s.Nil(r)
}

func (s *CommonSuite) TestNonEmptyReader_NonEmpty() {
	buf := bytes.NewBuffer([]byte("1"))
	r, err := NonEmptyReader(buf)
	s.NoError(err)
	s.NotNil(r)

	read, err := io.ReadAll(r)
	s.NoError(err)
	s.Equal("1", string(read))
}

func (s *CommonSuite) TestNewReadCloser() {
	buf := bytes.NewBuffer([]byte("1"))
	closer := &closer{}
	r := NewReadCloser(buf, closer)

	read, err := io.ReadAll(r)
	s.NoError(err)
	s.Equal("1", string(read))

	s.NoError(r.Close())
	s.Equal(1, closer.called)
}

func (s *CommonSuite) TestNewContextReader() {
	buf := bytes.NewBuffer([]byte("12"))
	ctx, cancel := context.WithCancel(context.Background())

	r := NewContextReader(ctx, buf)

	b := make([]byte, 1)
	n, err := r.Read(b)
	s.Equal(1, n)
	s.NoError(err)

	cancel()
	n, err = r.Read(b)
	s.Equal(0, n)
	s.NotNil(err)
}

func (s *CommonSuite) TestNewContextReadCloser() {
	buf := NewReadCloser(bytes.NewBuffer([]byte("12")), &closer{})
	ctx, cancel := context.WithCancel(context.Background())

	r := NewContextReadCloser(ctx, buf)

	b := make([]byte, 1)
	n, err := r.Read(b)
	s.Equal(1, n)
	s.NoError(err)

	cancel()
	n, err = r.Read(b)
	s.Equal(0, n)
	s.NotNil(err)

	s.NoError(r.Close())
}

func (s *CommonSuite) TestNewContextWriter() {
	buf := bytes.NewBuffer(nil)
	ctx, cancel := context.WithCancel(context.Background())

	r := NewContextWriter(ctx, buf)

	n, err := r.Write([]byte("1"))
	s.Equal(1, n)
	s.NoError(err)

	cancel()
	n, err = r.Write([]byte("1"))
	s.Equal(0, n)
	s.NotNil(err)
}

func (s *CommonSuite) TestNewContextWriteCloser() {
	buf := NewWriteCloser(bytes.NewBuffer(nil), &closer{})
	ctx, cancel := context.WithCancel(context.Background())

	w := NewContextWriteCloser(ctx, buf)

	n, err := w.Write([]byte("1"))
	s.Equal(1, n)
	s.NoError(err)

	cancel()
	n, err = w.Write([]byte("1"))
	s.Equal(0, n)
	s.NotNil(err)

	s.NoError(w.Close())
}

func (s *CommonSuite) TestNewWriteCloserOnError() {
	buf := NewWriteCloser(bytes.NewBuffer(nil), &closer{})

	ctx, cancel := context.WithCancel(context.Background())

	var called error
	w := NewWriteCloserOnError(NewContextWriteCloser(ctx, buf), func(err error) {
		called = err
	})

	cancel()
	w.Write(nil)

	s.NotNil(called)
}

func (s *CommonSuite) TestNewReadCloserOnError() {
	buf := NewReadCloser(bytes.NewBuffer(nil), &closer{})
	ctx, cancel := context.WithCancel(context.Background())

	var called error
	w := NewReadCloserOnError(NewContextReadCloser(ctx, buf), func(err error) {
		called = err
	})

	cancel()
	w.Read(nil)

	s.NotNil(called)
}

func ExampleCheckClose() {
	// CheckClose is commonly used with named return values
	f := func() (err error) {
		// Get a io.ReadCloser
		r := io.NopCloser(strings.NewReader("foo"))

		// defer CheckClose call with an io.Closer and pointer to error
		defer CheckClose(r, &err)

		// ... work with r ...

		// if err is not nil, CheckClose will assign any close errors to it
		return err
	}

	err := f()
	if err != nil {
		panic(err)
	}
}
