// Package common contains interfaces and non-specific protocol entities
package common

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"regexp"
	"strings"

	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/formats/packp/pktline"
	"gopkg.in/src-d/go-git.v4/storage/memory"
)

var (
	ErrRepositoryNotFound    = errors.New("repository not found")
	ErrAuthorizationRequired = errors.New("authorization required")
	ErrEmptyGitUploadPack    = errors.New("empty git-upload-pack given")
	ErrInvalidAuthMethod     = errors.New("invalid auth method")
)

const GitUploadPackServiceName = "git-upload-pack"

type GitUploadPackService interface {
	Connect() error
	SetAuth(AuthMethod) error
	Info() (*GitUploadPackInfo, error)
	Fetch(*GitUploadPackRequest) (io.ReadCloser, error)
	Disconnect() error
}

type AuthMethod interface {
	Name() string
	String() string
}

type Endpoint url.URL

var (
	isSchemeRegExp   = regexp.MustCompile("^[^:]+://")
	scpLikeUrlRegExp = regexp.MustCompile("^(?P<user>[^@]+@)?(?P<host>[^:]+):/?(?P<path>.+)$")
)

func NewEndpoint(endpoint string) (Endpoint, error) {
	endpoint = transformSCPLikeIfNeeded(endpoint)

	u, err := url.Parse(endpoint)
	if err != nil {
		return Endpoint{}, core.NewPermanentError(err)
	}

	if !u.IsAbs() {
		return Endpoint{}, core.NewPermanentError(fmt.Errorf(
			"invalid endpoint: %s", endpoint,
		))
	}

	return Endpoint(*u), nil
}

func transformSCPLikeIfNeeded(endpoint string) string {
	if !isSchemeRegExp.MatchString(endpoint) && scpLikeUrlRegExp.MatchString(endpoint) {
		m := scpLikeUrlRegExp.FindStringSubmatch(endpoint)
		return fmt.Sprintf("ssh://%s%s/%s", m[1], m[2], m[3])
	}

	return endpoint
}

func (e *Endpoint) String() string {
	u := url.URL(*e)
	return u.String()
}

// Capabilities contains all the server capabilities
// https://github.com/git/git/blob/master/Documentation/technical/protocol-capabilities.txt
type Capabilities struct {
	m map[string]*Capability
	o []string
}

// Capability represents a server capability
type Capability struct {
	Name   string
	Values []string
}

// NewCapabilities returns a new Capabilities struct
func NewCapabilities() *Capabilities {
	return &Capabilities{
		m: make(map[string]*Capability, 0),
	}
}

// Decode decodes a string
func (c *Capabilities) Decode(raw string) {
	params := strings.Split(raw, " ")
	for _, p := range params {
		s := strings.SplitN(p, "=", 2)

		var value string
		if len(s) == 2 {
			value = s[1]
		}

		c.Add(s[0], value)
	}
}

// Get returns the values for a capability
func (c *Capabilities) Get(capability string) *Capability {
	return c.m[capability]
}

// Set sets a capability removing the values
func (c *Capabilities) Set(capability string, values ...string) {
	if _, ok := c.m[capability]; ok {
		delete(c.m, capability)
	}

	c.Add(capability, values...)
}

// Add adds a capability, values are optional
func (c *Capabilities) Add(capability string, values ...string) {
	if !c.Supports(capability) {
		c.m[capability] = &Capability{Name: capability}
		c.o = append(c.o, capability)
	}

	if len(values) == 0 {
		return
	}

	c.m[capability].Values = append(c.m[capability].Values, values...)
}

// Supports returns true if capability is present
func (c *Capabilities) Supports(capability string) bool {
	_, ok := c.m[capability]
	return ok
}

// SymbolicReference returns the reference for a given symbolic reference
func (c *Capabilities) SymbolicReference(sym string) string {
	if !c.Supports("symref") {
		return ""
	}

	for _, symref := range c.Get("symref").Values {
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

func (c *Capabilities) String() string {
	if len(c.o) == 0 {
		return ""
	}

	var o string
	for _, key := range c.o {
		cap := c.m[key]

		added := false
		for _, value := range cap.Values {
			if value == "" {
				continue
			}

			added = true
			o += fmt.Sprintf("%s=%s ", key, value)
		}

		if len(cap.Values) == 0 || !added {
			o += key + " "
		}
	}

	if len(o) == 0 {
		return o
	}

	return o[:len(o)-1]
}

type GitUploadPackInfo struct {
	Capabilities *Capabilities
	Refs         memory.ReferenceStorage
}

func NewGitUploadPackInfo() *GitUploadPackInfo {
	return &GitUploadPackInfo{Capabilities: NewCapabilities()}
}

func (r *GitUploadPackInfo) Decode(s *pktline.Scanner) error {
	if err := r.read(s); err != nil {
		if err == ErrEmptyGitUploadPack {
			return core.NewPermanentError(err)
		}

		return core.NewUnexpectedError(err)
	}

	return nil
}

func (r *GitUploadPackInfo) read(s *pktline.Scanner) error {
	isEmpty := true
	r.Refs = make(memory.ReferenceStorage, 0)
	smartCommentIgnore := false
	for s.Scan() {
		line := string(s.Bytes())

		if smartCommentIgnore {
			// some servers like Github add a flush-pkt after the smart http comment
			// that we must ignore to prevent a premature termination of the read.
			if len(line) == 0 {
				continue
			}
			smartCommentIgnore = false
		}

		// exit on first flush-pkt
		if len(line) == 0 {
			break
		}

		if isSmartHttpComment(line) {
			smartCommentIgnore = true
			continue
		}

		if err := r.readLine(line); err != nil {
			return err
		}

		isEmpty = false
	}

	if isEmpty {
		return ErrEmptyGitUploadPack
	}

	return s.Err()
}

func isSmartHttpComment(line string) bool {
	return line[0] == '#'
}

func (r *GitUploadPackInfo) readLine(line string) error {
	hashEnd := strings.Index(line, " ")
	hash := line[:hashEnd]

	zeroID := strings.Index(line, string([]byte{0}))
	if zeroID == -1 {
		name := line[hashEnd+1 : len(line)-1]
		ref := core.NewReferenceFromStrings(name, hash)
		return r.Refs.Set(ref)
	}

	name := line[hashEnd+1 : zeroID]
	r.Capabilities.Decode(line[zeroID+1 : len(line)-1])
	if !r.Capabilities.Supports("symref") {
		ref := core.NewReferenceFromStrings(name, hash)
		return r.Refs.Set(ref)
	}

	target := r.Capabilities.SymbolicReference(name)
	ref := core.NewSymbolicReference(core.ReferenceName(name), core.ReferenceName(target))
	return r.Refs.Set(ref)
}

func (r *GitUploadPackInfo) Head() *core.Reference {
	ref, _ := core.ResolveReference(r.Refs, core.HEAD)
	return ref
}

func (r *GitUploadPackInfo) String() string {
	return string(r.Bytes())
}

func (r *GitUploadPackInfo) Bytes() []byte {
	p := pktline.New()
	_ = p.AddString("# service=git-upload-pack\n")
	// inserting a flush-pkt here violates the protocol spec, but some
	// servers do it, like Github.com
	p.AddFlush()

	firstLine := fmt.Sprintf("%s HEAD\x00%s\n", r.Head().Hash(), r.Capabilities.String())
	_ = p.AddString(firstLine)

	for _, ref := range r.Refs {
		if ref.Type() != core.HashReference {
			continue
		}

		ref := fmt.Sprintf("%s %s\n", ref.Hash(), ref.Name())
		_ = p.AddString(ref)
	}

	p.AddFlush()
	b, _ := ioutil.ReadAll(p)

	return b
}

type GitUploadPackRequest struct {
	Wants []core.Hash
	Haves []core.Hash
	Depth int
}

func (r *GitUploadPackRequest) Want(h ...core.Hash) {
	r.Wants = append(r.Wants, h...)
}

func (r *GitUploadPackRequest) Have(h ...core.Hash) {
	r.Haves = append(r.Haves, h...)
}

func (r *GitUploadPackRequest) String() string {
	b, _ := ioutil.ReadAll(r.Reader())
	return string(b)
}

func (r *GitUploadPackRequest) Reader() *strings.Reader {
	p := pktline.New()

	for _, want := range r.Wants {
		_ = p.AddString(fmt.Sprintf("want %s\n", want))
	}

	for _, have := range r.Haves {
		_ = p.AddString(fmt.Sprintf("have %s\n", have))
	}

	if r.Depth != 0 {
		_ = p.AddString(fmt.Sprintf("deepen %d\n", r.Depth))
	}

	p.AddFlush()
	_ = p.AddString("done\n")

	b, _ := ioutil.ReadAll(p)

	return strings.NewReader(string(b))
}
