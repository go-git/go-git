package common

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"strings"

	"gopkg.in/src-d/go-git.v2/formats/pktline"
	"gopkg.in/src-d/go-git.v2/internal"

	"gopkg.in/sourcegraph/go-vcsurl.v1"
)

var (
	NotFoundErr           = errors.New("repository not found")
	EmptyGitUploadPackErr = errors.New("empty git-upload-pack given")
)

const GitUploadPackServiceName = "git-upload-pack"

type Endpoint string

func NewEndpoint(url string) (Endpoint, error) {
	vcs, err := vcsurl.Parse(url)
	if err != nil {
		return "", NewPermanentError(err)
	}

	link := vcs.Link()
	if !strings.HasSuffix(link, ".git") {
		link += ".git"
	}

	return Endpoint(link), nil
}

func (e Endpoint) Service(name string) string {
	return fmt.Sprintf("%s/info/refs?service=%s", e, name)
}

// Capabilities contains all the server capabilities
// https://github.com/git/git/blob/master/Documentation/technical/protocol-capabilities.txt
type Capabilities map[string][]string

func parseCapabilities(line string) Capabilities {
	values, _ := url.ParseQuery(strings.Replace(line, " ", "&", -1))

	return Capabilities(values)
}

func (c Capabilities) Decode(raw string) {
	parts := strings.SplitN(raw, "HEAD", 2)
	if len(parts) == 2 {
		raw = parts[1]
	}

	params := strings.Split(raw, " ")
	for _, p := range params {
		s := strings.SplitN(p, "=", 2)

		var value string
		if len(s) == 2 {
			value = s[1]
		}

		c[s[0]] = append(c[s[0]], value)
	}
}

func (c Capabilities) String() string {
	var o string
	for key, values := range c {
		if len(values) == 0 {
			o += key + " "
		}

		for _, value := range values {
			o += fmt.Sprintf("%s=%s ", key, value)
		}
	}

	return o[:len(o)-1]
}

// Supports returns true if capability is preent
func (r Capabilities) Supports(capability string) bool {
	_, ok := r[capability]
	return ok
}

// Get returns the values for a capability
func (r Capabilities) Get(capability string) []string {
	return r[capability]
}

// SymbolicReference returns the reference for a given symbolic reference
func (r Capabilities) SymbolicReference(sym string) string {
	if !r.Supports("symref") {
		return ""
	}

	for _, symref := range r.Get("symref") {
		parts := strings.Split(symref, ":")
		if len(parts) != 2 {
			continue
		}

		if parts[0] == sym {
			return parts[1]
		}
	}

	return ""
}

type GitUploadPackInfo struct {
	Capabilities Capabilities
	Head         string
	Refs         map[string]internal.Hash
}

func NewGitUploadPackInfo(d *pktline.Decoder) (*GitUploadPackInfo, error) {
	info := &GitUploadPackInfo{}
	if err := info.read(d); err != nil {
		if err == EmptyGitUploadPackErr {
			return nil, NewPermanentError(err)
		}

		return nil, NewUnexpectedError(err)
	}

	return info, nil
}

func (r *GitUploadPackInfo) read(d *pktline.Decoder) error {
	lines, err := d.ReadAll()
	if err != nil {
		return err
	}

	isEmpty := true
	r.Refs = map[string]internal.Hash{}
	for _, line := range lines {
		if !r.isValidLine(line) {
			continue
		}

		if r.Capabilities == nil {
			r.Capabilities = Capabilities{}
			r.Capabilities.Decode(line)
			continue
		}

		r.readLine(line)
		isEmpty = false
	}

	if isEmpty {
		return EmptyGitUploadPackErr
	}

	return nil
}

func (r *GitUploadPackInfo) isValidLine(line string) bool {
	if line[0] == '#' {
		return false
	}

	return true
}

func (r *GitUploadPackInfo) readLine(line string) {
	parts := strings.Split(strings.Trim(line, " \n"), " ")
	if len(parts) != 2 {
		return
	}

	r.Refs[parts[1]] = internal.NewHash(parts[0])
}

func (r *GitUploadPackInfo) String() string {
	return string(r.Bytes())
}

func (r *GitUploadPackInfo) Bytes() []byte {
	e := pktline.NewEncoder()
	e.AddLine("# service=git-upload-pack")
	e.AddFlush()
	e.AddLine(fmt.Sprintf("%s HEAD%s", r.Refs[r.Head], r.Capabilities.String()))

	for name, id := range r.Refs {
		e.AddLine(fmt.Sprintf("%s %s", id, name))
	}

	e.AddFlush()
	b, _ := ioutil.ReadAll(e.Reader())
	return b
}

type GitUploadPackRequest struct {
	Want []internal.Hash
	Have []internal.Hash
}

func (r *GitUploadPackRequest) String() string {
	b, _ := ioutil.ReadAll(r.Reader())
	return string(b)
}

func (r *GitUploadPackRequest) Reader() *strings.Reader {
	e := pktline.NewEncoder()
	for _, want := range r.Want {
		e.AddLine(fmt.Sprintf("want %s", want))
	}

	for _, have := range r.Have {
		e.AddLine(fmt.Sprintf("have %s", have))
	}

	e.AddFlush()
	e.AddLine("done")

	return e.Reader()
}

type PermanentError struct {
	err error
}

func NewPermanentError(err error) *PermanentError {
	if err == nil {
		return nil
	}

	return &PermanentError{err: err}
}

func (e *PermanentError) Error() string {
	return fmt.Sprintf("permanent client error: %s", e.err.Error())
}

type UnexpectedError struct {
	err error
}

func NewUnexpectedError(err error) *UnexpectedError {
	if err == nil {
		return nil
	}

	return &UnexpectedError{err: err}
}

func (e *UnexpectedError) Error() string {
	return fmt.Sprintf("unexpected client error: %s", e.err.Error())
}
