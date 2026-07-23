package packp

import (
	"bytes"
	"fmt"
	"testing"
)

func TestEncodeEmptyGitProtoRequest(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	var p GitProtoRequest
	err := p.Encode(&buf)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEncodeGitProtoRequest(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	var buf bytes.Buffer
	p := GitProtoRequest{}
	err := p.Encode(&buf)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeEmptyGitProtoRequest(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	var p GitProtoRequest
	err := p.Decode(&buf)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeGitProtoRequest(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	var buf bytes.Buffer
	buf.WriteString("0026command \x00host=host\x00\x00param1\x00param2")
	var p GitProtoRequest
	err := p.Decode(&buf)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateEmptyGitProtoRequest(t *testing.T) {
	t.Parallel()
	var p GitProtoRequest
	err := p.validate()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEncodeGitProtoRequestRejectsControlBytes(t *testing.T) {
	t.Parallel()
	// A git:// URL such as "git://host/repo%00host=evil%00%00version=2"
	// decodes to this Pathname. Encoding it verbatim would append a second
	// host= selector and extra parameters to the single daemon request.
	cases := []GitProtoRequest{
		{RequestCommand: "git-upload-pack", Pathname: "/repo\x00host=evil"},
		{RequestCommand: "git-upload-pack", Pathname: "/repo", Host: "host\x00evil"},
		{RequestCommand: "git-upload-pack", Pathname: "/repo\nhost=evil"},
		{RequestCommand: "git-upload-pack", Pathname: "/repo", ExtraParams: []string{"version=2\x00inject"}},
		{RequestCommand: "git-upload-pack\x7f", Pathname: "/repo"},
	}
	for _, p := range cases {
		var buf bytes.Buffer
		if err := p.Encode(&buf); err == nil {
			t.Fatalf("expected error for %+v, got encoded %q", p, buf.String())
		}
		if buf.Len() != 0 {
			t.Fatalf("expected no bytes written on rejection, got %q", buf.String())
		}
	}
}

func TestDecodeGitProtoRequestRejectsControlBytes(t *testing.T) {
	t.Parallel()
	// NUL is the field delimiter and cannot appear inside a field, but a peer
	// can put a newline or other control byte in the pathname or host. The
	// server forwards these into URL construction and log lines, so a decoded
	// request carrying one must be rejected.
	payloads := []string{
		"git-upload-pack /repo\nhost=evil\x00",
		"git-upload-pack /repo\x00host=ev\x1bil\x00",
	}
	for _, payload := range payloads {
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "%04x%s", len(payload)+4, payload)
		var p GitProtoRequest
		if err := p.Decode(&buf); err == nil {
			t.Fatalf("expected error decoding %q", payload)
		}
	}
}
