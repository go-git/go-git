package pktline

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestEncodeEmptyErrorLine(t *testing.T) {
	e := &ErrorLine{}
	err := e.Encode(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEncodeErrorLine(t *testing.T) {
	e := &ErrorLine{
		Text: "something",
	}
	var buf bytes.Buffer
	err := e.Encode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "0012ERR something\n" {
		t.Fatalf("unexpected encoded error line: %q", buf.String())
	}
}

func TestDecodeEmptyErrorLine(t *testing.T) {
	var buf bytes.Buffer
	e := &ErrorLine{}
	err := e.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if e.Text != "" {
		t.Fatalf("unexpected error line: %q", e.Text)
	}
}

func TestDecodeErrorLine(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("000eERR foobar")
	var e *ErrorLine
	err := e.Decode(&buf)
	if !errors.As(err, &e) {
		t.Fatalf("expected error line, got: %T: %v", err, err)
	}
	if e.Text != "foobar" {
		t.Fatalf("unexpected error line: %q", e.Text)
	}
}

func TestDecodeErrorLineLn(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("000fERR foobar\n")
	var e *ErrorLine
	err := e.Decode(&buf)
	if !errors.As(err, &e) {
		t.Fatalf("expected error line, got: %T: %v", err, err)
	}
	if e.Text != "foobar" {
		t.Fatalf("unexpected error line: %q", e.Text)
	}
}
