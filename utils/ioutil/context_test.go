package ioutil

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type ContextReadCloserSuite struct {
	suite.Suite
}

func TestContextReadCloserSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, &ContextReadCloserSuite{})
}

func (s *ContextReadCloserSuite) TestRead() {
	buf := []byte("abcdef")
	buf2 := make([]byte, 3)
	r := NewContextReadCloser(context.Background(), bytes.NewReader(buf))
	s.T().Cleanup(func() { r.Close() })

	// read first half
	n, err := r.Read(buf2)
	s.Require().Equal(3, n)
	s.Require().NoError(err)
	s.Require().Equal(buf[:3], buf2)

	// read second half
	n, err = r.Read(buf2)
	s.Require().Equal(3, n)
	s.Require().NoError(err)
	s.Require().Equal(buf[3:6], buf2)

	// read more.
	n, err = r.Read(buf2)
	s.Equal(0, n)
	s.ErrorIs(err, io.EOF)
}

type testReader func([]byte) (int, error)

func (r testReader) Read(b []byte) (int, error) {
	return r(b)
}

func (s *ContextReadCloserSuite) TestReadEmpty() {
	called := 0
	r := testReader(func(b []byte) (int, error) {
		called++
		return len(b), nil
	})

	ctxr := NewContextReadCloser(context.Background(), r)
	s.T().Cleanup(func() { ctxr.Close() })

	n, err := ctxr.Read(make([]byte, 0))
	s.Equal(0, n)
	s.NotErrorIs(err, io.EOF)
	s.Equal(err, nil)
	s.Equal(0, called)
}

func (s *ContextReadCloserSuite) TestReadCancel() {
	called := 0
	r := testReader(func(b []byte) (int, error) {
		called++
		return len(b), nil
	})

	ctx, cancel := context.WithCancel(context.Background())

	ctxr := NewContextReadCloser(ctx, r)
	s.T().Cleanup(func() { ctxr.Close() })
	cancel()

	n, err := ctxr.Read(make([]byte, 1))
	s.Equal(0, n)
	s.ErrorIs(err, context.Canceled)
	s.Equal(0, called)
}

func (s *ContextReadCloserSuite) TestClose() {
	called := 0
	r := testReader(func(b []byte) (int, error) {
		called++
		return len(b), nil
	})

	ctxr := NewContextReadCloser(context.Background(), r)
	s.T().Cleanup(func() { ctxr.Close() })

	s.Require().NoError(ctxr.Close())

	n, err := ctxr.Read(make([]byte, 1))
	s.Require().Equal(0, n)
	s.Require().Error(err)
	s.Require().Equal(0, called)

	s.NoError(ctxr.Close())
}

func (s *ContextReadCloserSuite) TestPanic() {
	r := NewContextReadCloser(context.Background(), nil)
	s.T().Cleanup(func() { r.Close() })

	done := make(chan struct{}, 1)

	go func() {
		s.Require().NotPanics(func() {
			n, err := r.Read(make([]byte, 1))
			s.Require().Error(err)
			s.Require().Equal(0, n)
		})

		done <- struct{}{}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		s.T().Error("test timed out")
	}
}

type ContextWriteCloserSuite struct {
	suite.Suite
}

func TestContextWriteCloserSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, &ContextWriteCloserSuite{})
}

func (s *ContextWriteCloserSuite) TestWrite() {
	var buf bytes.Buffer
	w := NewContextWriteCloser(context.Background(), &buf)

	// write three
	n, err := w.Write([]byte("abc"))
	s.Require().Equal(3, n)
	s.Require().NoError(err)
	s.Require().Equal("abc", buf.String())

	// write three more
	n, err = w.Write([]byte("def"))
	s.Require().Equal(3, n)
	s.Require().NoError(err)
	s.Require().Equal("abcdef", buf.String())
}

type testWriter func([]byte) (int, error)

func (w testWriter) Write(b []byte) (int, error) {
	return w(b)
}

func (s *ContextWriteCloserSuite) TestWriteEmpty() {
	called := 0
	w := testWriter(func(b []byte) (int, error) {
		called++
		return len(b), nil
	})

	ctxw := NewContextWriteCloser(context.Background(), w)
	s.T().Cleanup(func() { ctxw.Close() })

	n, err := ctxw.Write(make([]byte, 0))
	s.Equal(0, n)
	s.Equal(err, nil)
	s.Equal(0, called)
}

func (s *ContextWriteCloserSuite) TestWriteCancel() {
	called := 0
	w := testWriter(func(b []byte) (int, error) {
		called++
		return len(b), nil
	})

	ctx, cancel := context.WithCancel(context.Background())

	ctxw := NewContextWriteCloser(ctx, w)
	s.T().Cleanup(func() { ctxw.Close() })
	cancel()

	n, err := ctxw.Write(make([]byte, 1))
	s.Equal(0, n)
	s.ErrorIs(err, context.Canceled)
	s.Equal(0, called)
}

func (s *ContextWriteCloserSuite) TestClose() {
	called := 0
	w := testWriter(func(b []byte) (int, error) {
		called++
		return len(b), nil
	})

	ctxw := NewContextWriteCloser(context.Background(), w)
	s.T().Cleanup(func() { ctxw.Close() })

	s.Require().Error(ctxw.Close())

	n, err := ctxw.Write(make([]byte, 1))
	s.Require().Equal(0, n)
	s.Require().Error(err)
	s.Require().Equal(0, called)

	s.NoError(ctxw.Close())
}

func (s *ContextWriteCloserSuite) TestPanic() {
	w := NewContextWriteCloser(context.Background(), nil)

	done := make(chan struct{}, 1)

	go func() {
		s.Require().NotPanics(func() {
			n, err := w.Write([]byte{'a'})
			s.Require().Error(err)
			s.Require().Equal(0, n)
		})

		done <- struct{}{}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		s.T().Error("test timed out")
	}
}

func BenchmarkContextReader(b *testing.B) {
	data := bytes.Repeat([]byte{'0'}, 10000)

	ctx := context.Background()
	r := bytes.NewReader(data)
	ctxr := NewContextReadCloser(ctx, r)
	b.Cleanup(func() { ctxr.Close() })

	for b.Loop() {
		r.Reset(data)

		n, _ := ctxr.Read(data)
		b.SetBytes(int64(n))
	}
}

func BenchmarkContextWriter(b *testing.B) {
	data := bytes.Repeat([]byte{'0'}, 10000)

	ctx := context.Background()
	ctxw := NewContextWriteCloser(ctx, io.Discard)
	b.Cleanup(func() { ctxw.Close() })

	for b.Loop() {
		n, _ := ctxw.Write(data)
		b.SetBytes(int64(n))
	}
}
