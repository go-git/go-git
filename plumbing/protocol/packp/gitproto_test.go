package packp

import (
	"bytes"
	"testing"
)

func TestEncodeEmptyGitProtoRequest(t *testing.T) {
	var buf bytes.Buffer
	var p GitProtoRequest
	err := p.Encode(&buf)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEncodeGitProtoRequest(t *testing.T) {
	var buf bytes.Buffer
	p := GitProtoRequest{
		RequestCommand: "command",
		Pathname:       "pathname",
		Host:           "host",
		ExtraParams:    []string{"param1", "param2"},
	}
	err := p.Encode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	expected := "002ecommand pathname\x00host=host\x00\x00param1\x00param2\x00"
	if buf.String() != expected {
		t.Fatalf("expected %q, got %q", expected, buf.String())
	}
}

func TestEncodeInvalidGitProtoRequest(t *testing.T) {
	var buf bytes.Buffer
	p := GitProtoRequest{
		RequestCommand: "command",
	}
	err := p.Encode(&buf)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeEmptyGitProtoRequest(t *testing.T) {
	var buf bytes.Buffer
	var p GitProtoRequest
	err := p.Decode(&buf)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeGitProtoRequest(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("002ecommand pathname\x00host=host\x00\x00param1\x00param2\x00")
	var p GitProtoRequest
	err := p.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	expected := GitProtoRequest{
		RequestCommand: "command",
		Pathname:       "pathname",
		Host:           "host",
		ExtraParams:    []string{"param1", "param2"},
	}
	if p.RequestCommand != expected.RequestCommand {
		t.Fatalf("expected %q, got %q", expected.RequestCommand, p.RequestCommand)
	}
	if p.Pathname != expected.Pathname {
		t.Fatalf("expected %q, got %q", expected.Pathname, p.Pathname)
	}
	if p.Host != expected.Host {
		t.Fatalf("expected %q, got %q", expected.Host, p.Host)
	}
	if len(p.ExtraParams) != len(expected.ExtraParams) {
		t.Fatalf("expected %d, got %d", len(expected.ExtraParams), len(p.ExtraParams))
	}
}

func TestDecodeInvalidGitProtoRequest(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("0026command \x00host=host\x00\x00param1\x00param2")
	var p GitProtoRequest
	err := p.Decode(&buf)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateEmptyGitProtoRequest(t *testing.T) {
	var p GitProtoRequest
	err := p.validate()
	if err == nil {
		t.Fatal("expected error")
	}
}
