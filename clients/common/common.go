package common

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"strings"

	"gopkg.in/src-d/go-git.v2/formats/pktline"
	"gopkg.in/src-d/go-git.v2/internal"

	"gopkg.in/sourcegraph/go-vcsurl.v1"
)

const GitUploadPackServiceName = "git-upload-pack"

type Endpoint string

func NewEndpoint(url string) (Endpoint, error) {
	vcs, err := vcsurl.Parse(url)
	if err != nil {
		return "", err
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

type RemoteHead struct {
	Id   internal.Hash
	Name string
}

type GitUploadPackInfo struct {
	Capabilities Capabilities
	Refs         map[string]*RemoteHead
}

func NewGitUploadPackInfo(d *pktline.Decoder) (*GitUploadPackInfo, error) {
	info := &GitUploadPackInfo{}
	if err := info.read(d); err != nil {
		return nil, err
	}

	return info, nil
}

func (r *GitUploadPackInfo) read(d *pktline.Decoder) error {
	lines, err := d.ReadAll()
	if err != nil {
		return err
	}

	r.Refs = map[string]*RemoteHead{}
	for _, line := range lines {
		if !r.isValidLine(line) {
			continue
		}

		if r.Capabilities == nil {
			r.Capabilities = parseCapabilities(line)
			continue
		}

		r.readLine(line)
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
	rh := r.getRemoteHead(line)
	r.Refs[rh.Name] = rh
}

func (r *GitUploadPackInfo) getRemoteHead(line string) *RemoteHead {
	parts := strings.Split(strings.Trim(line, " \n"), " ")
	if len(parts) != 2 {
		return nil
	}

	return &RemoteHead{internal.NewHash(parts[0]), parts[1]}
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
