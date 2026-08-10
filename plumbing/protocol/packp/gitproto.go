package packp

import (
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// ErrInvalidGitProtoRequest is returned by Decode if the input is not a
// valid git protocol request.
var ErrInvalidGitProtoRequest = fmt.Errorf("invalid git protocol request")

// GitProtoRequest is a command request for the git protocol.
// It is used to send the command, endpoint, and extra parameters to the
// remote.
// See https://git-scm.com/docs/pack-protocol#_git_transport
type GitProtoRequest struct {
	RequestCommand string
	Pathname       string

	// Optional
	Host string

	// Optional
	ExtraParams []string
}

// validate validates the request.
func (g *GitProtoRequest) validate() error {
	if g.RequestCommand == "" {
		return fmt.Errorf("%w: empty request command", ErrInvalidGitProtoRequest)
	}

	// The request is a single pkt-line whose fields are separated by NUL
	// bytes ("<command> <pathname>\x00host=<host>\x00\x00<params>..."). A
	// control byte in any field breaks that framing or splices in extra
	// NUL-delimited fields (a second host=, additional parameters) that the
	// caller never set. A git:// URL with a percent-encoded NUL, for example
	// "git://host/repo%00host=evil", decodes into exactly such a Pathname, so
	// refuse control bytes in every field.
	if err := validateGitProtoField("request command", g.RequestCommand); err != nil {
		return err
	}
	if err := validateGitProtoField("pathname", g.Pathname); err != nil {
		return err
	}
	if err := validateGitProtoField("host", g.Host); err != nil {
		return err
	}
	for _, p := range g.ExtraParams {
		if err := validateGitProtoField("extra parameter", p); err != nil {
			return err
		}
	}

	return nil
}

// validateGitProtoField rejects a request field containing an ASCII control
// byte (0x00-0x1f or 0x7f). Such a byte would break the NUL-framed pkt-line or
// splice in additional fields. No valid command, path, host, or parameter
// contains one. This matches upstream git, which forbids newlines in the host
// and path of a git:// request (git.git a02ea577, CVE-2021-40330), and extends
// it to the full control range including NUL, which a Go string can carry
// through where a C string cannot.
func validateGitProtoField(name, value string) error {
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] == 0x7f {
			return fmt.Errorf("%w: %s contains control byte %#02x",
				ErrInvalidGitProtoRequest, name, value[i])
		}
	}
	return nil
}

// Encode encodes the request into the writer.
func (g *GitProtoRequest) Encode(w io.Writer) error {
	if w == nil {
		return ErrNilWriter
	}

	if err := g.validate(); err != nil {
		return err
	}

	var req strings.Builder
	fmt.Fprintf(&req, "%s %s\x00", g.RequestCommand, g.Pathname)
	if host := g.Host; host != "" {
		fmt.Fprintf(&req, "host=%s\x00", host)
	}

	if len(g.ExtraParams) > 0 {
		req.WriteString("\x00")
		for _, param := range g.ExtraParams {
			req.WriteString(param)
			req.WriteString("\x00")
		}
	}

	if _, err := pktline.Write(w, []byte(req.String())); err != nil {
		return err
	}

	return nil
}

// Decode decodes the request from the reader.
func (g *GitProtoRequest) Decode(r io.Reader) error {
	s := pktline.NewScanner(r)
	if !s.Scan() {
		if s.Err() == nil {
			return ErrInvalidGitProtoRequest
		}
		return s.Err()
	}

	if s.Len() == pktline.Flush {
		return io.EOF
	}

	line := s.Text()
	if len(line) == 0 {
		return io.EOF
	}

	if line[len(line)-1] != 0 {
		return fmt.Errorf("%w: missing null terminator", ErrInvalidGitProtoRequest)
	}

	parts := strings.SplitN(line, " ", 2)
	if len(parts) != 2 {
		return fmt.Errorf("%w: short request", ErrInvalidGitProtoRequest)
	}

	g.RequestCommand = parts[0]
	params := strings.Split(parts[1], string(null))
	if len(params) < 1 {
		return fmt.Errorf("%w: missing pathname", ErrInvalidGitProtoRequest)
	}

	g.Pathname = params[0]
	if len(params) > 1 {
		g.Host = strings.TrimPrefix(params[1], "host=")
	}

	if len(params) > 2 {
		for _, param := range params[2:] {
			if param != "" {
				g.ExtraParams = append(g.ExtraParams, param)
			}
		}
	}

	// A decoded request comes straight off the wire from an untrusted peer.
	// NUL cannot survive here (it delimits the fields split above), but other
	// control bytes such as newline or ESC can, and the server forwards these
	// fields into URL construction and log lines. Reject them symmetrically
	// with Encode.
	return g.validate()
}
