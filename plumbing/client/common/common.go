// Package common contains interfaces and non-specific protocol entities
package common

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"regexp"
	"strings"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packp"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packp/advrefs"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packp/pktline"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
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
		return Endpoint{}, plumbing.NewPermanentError(err)
	}

	if !u.IsAbs() {
		return Endpoint{}, plumbing.NewPermanentError(fmt.Errorf(
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

type GitUploadPackInfo struct {
	Capabilities *packp.Capabilities
	Refs         memory.ReferenceStorage
}

func NewGitUploadPackInfo() *GitUploadPackInfo {
	return &GitUploadPackInfo{
		Capabilities: packp.NewCapabilities(),
		Refs:         make(memory.ReferenceStorage, 0),
	}
}

func (i *GitUploadPackInfo) Decode(r io.Reader) error {
	d := advrefs.NewDecoder(r)
	ar := advrefs.New()
	if err := d.Decode(ar); err != nil {
		if err == advrefs.ErrEmpty {
			return plumbing.NewPermanentError(err)
		}
		return plumbing.NewUnexpectedError(err)
	}

	i.Capabilities = ar.Capabilities

	if err := i.addRefs(ar); err != nil {
		return plumbing.NewUnexpectedError(err)
	}

	return nil
}

func (i *GitUploadPackInfo) addRefs(ar *advrefs.AdvRefs) error {
	for name, hash := range ar.References {
		ref := plumbing.NewReferenceFromStrings(name, hash.String())
		i.Refs.SetReference(ref)
	}

	return i.addSymbolicRefs(ar)
}

func (i *GitUploadPackInfo) addSymbolicRefs(ar *advrefs.AdvRefs) error {
	if !hasSymrefs(ar) {
		return nil
	}

	for _, symref := range ar.Capabilities.Get("symref").Values {
		chunks := strings.Split(symref, ":")
		if len(chunks) != 2 {
			err := fmt.Errorf("bad number of `:` in symref value (%q)", symref)
			return plumbing.NewUnexpectedError(err)
		}
		name := plumbing.ReferenceName(chunks[0])
		target := plumbing.ReferenceName(chunks[1])
		ref := plumbing.NewSymbolicReference(name, target)
		i.Refs.SetReference(ref)
	}

	return nil
}

func hasSymrefs(ar *advrefs.AdvRefs) bool {
	return ar.Capabilities.Supports("symref")
}

func (i *GitUploadPackInfo) Head() *plumbing.Reference {
	ref, _ := storer.ResolveReference(i.Refs, plumbing.HEAD)
	return ref
}

func (i *GitUploadPackInfo) String() string {
	return string(i.Bytes())
}

func (i *GitUploadPackInfo) Bytes() []byte {
	var buf bytes.Buffer
	e := pktline.NewEncoder(&buf)

	_ = e.EncodeString("# service=git-upload-pack\n")

	// inserting a flush-pkt here violates the protocol spec, but some
	// servers do it, like Github.com
	e.Flush()

	_ = e.Encodef("%s HEAD\x00%s\n", i.Head().Hash(), i.Capabilities.String())

	for _, ref := range i.Refs {
		if ref.Type() != plumbing.HashReference {
			continue
		}

		_ = e.Encodef("%s %s\n", ref.Hash(), ref.Name())
	}

	e.Flush()

	return buf.Bytes()
}

type GitUploadPackRequest struct {
	Wants []plumbing.Hash
	Haves []plumbing.Hash
	Depth int
}

func (r *GitUploadPackRequest) Want(h ...plumbing.Hash) {
	r.Wants = append(r.Wants, h...)
}

func (r *GitUploadPackRequest) Have(h ...plumbing.Hash) {
	r.Haves = append(r.Haves, h...)
}

func (r *GitUploadPackRequest) String() string {
	b, _ := ioutil.ReadAll(r.Reader())
	return string(b)
}

func (r *GitUploadPackRequest) Reader() *strings.Reader {
	var buf bytes.Buffer
	e := pktline.NewEncoder(&buf)

	for _, want := range r.Wants {
		_ = e.Encodef("want %s\n", want)
	}

	for _, have := range r.Haves {
		_ = e.Encodef("have %s\n", have)
	}

	if r.Depth != 0 {
		_ = e.Encodef("deepen %d\n", r.Depth)
	}

	_ = e.Flush()
	_ = e.EncodeString("done\n")

	return strings.NewReader(buf.String())
}
