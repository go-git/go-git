package config

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/go-git/go-git/v6/internal/pathutil"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

var (
	// ErrModuleEmptyURL is returned when a submodule has an empty URL.
	ErrModuleEmptyURL = errors.New("module config: empty URL")
	// ErrModuleEmptyPath is returned when a submodule has an empty path.
	ErrModuleEmptyPath = errors.New("module config: empty path")
	// ErrModuleBadPath is returned when a submodule has an invalid path.
	ErrModuleBadPath = errors.New("submodule has an invalid path")
	// ErrModuleBadName is returned when a submodule's name is not safe
	// for use as a path component.
	ErrModuleBadName = errors.New("ignoring suspicious submodule name")
)

// Modules defines the submodules properties, represents a .gitmodules file
// https://www.kernel.org/pub/software/scm/git/docs/gitmodules.html
type Modules struct {
	// Submodules is a map of submodules being the key the name of the submodule.
	Submodules map[string]*Submodule

	raw *format.Config
}

// NewModules returns a new empty Modules
func NewModules() *Modules {
	return &Modules{
		Submodules: make(map[string]*Submodule),
		raw:        format.New(),
	}
}

const (
	pathKey   = "path"
	branchKey = "branch"
)

// Unmarshal parses a git-config file and stores it.
func (m *Modules) Unmarshal(b []byte) error {
	r := bytes.NewBuffer(b)
	d := format.NewDecoder(r)

	m.raw = format.New()
	if err := d.Decode(m.raw); err != nil {
		return err
	}

	unmarshalSubmodules(m.raw, m.Submodules)
	return nil
}

// Marshal returns Modules encoded as a git-config file.
func (m *Modules) Marshal() ([]byte, error) {
	s := m.raw.Section(submoduleSection)
	s.Subsections = make(format.Subsections, len(m.Submodules))

	var i int
	for _, r := range m.Submodules {
		s.Subsections[i] = r.marshal()
		i++
	}

	buf := bytes.NewBuffer(nil)
	if err := format.NewEncoder(buf).Encode(m.raw); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Submodule defines a submodule.
type Submodule struct {
	// Name module name
	Name string
	// Path defines the path, relative to the top-level directory of the Git
	// working tree.
	Path string
	// URL defines a URL from which the submodule repository can be cloned.
	URL string
	// Branch is a remote branch name for tracking updates in the upstream
	// submodule. Optional value.
	Branch string

	// raw representation of the subsection, filled by marshal or unmarshal are
	// called.
	raw *format.Subsection
}

// Validate validates the fields and sets the default values.
func (m *Submodule) Validate() error {
	if err := validSubmoduleName(m.Name); err != nil {
		return fmt.Errorf("%w: %q", ErrModuleBadName, m.Name)
	}

	if m.Path == "" {
		return ErrModuleEmptyPath
	}

	// The path is validated before the URL is checked for emptiness.
	// unmarshalSubmodules drops a stanza only on ErrModuleBadPath or
	// ErrModuleBadName, so a stanza carrying `path = ..` and no `url =`
	// would otherwise be reported as ErrModuleEmptyURL and kept with its
	// unsafe Path intact.
	//
	// pathutil.ValidSubmodulePath is the validator Submodule.Repository
	// runs on this Path before it chroots to it, so the parser applies the
	// same gate: a stanza it keeps is one whose Path can reach that chroot.
	// The error is flattened to ErrModuleBadPath, the sentinel the parser
	// drops a stanza on.
	//
	// Path is worktree-relative and is attacker-controlled via .gitmodules,
	// so its component rules run on every host. Only the volume prefix rule
	// is host-dependent, because filepath.VolumeName recognises a drive
	// letter on Windows alone.
	if err := pathutil.ValidSubmodulePath(m.Path); err != nil {
		return ErrModuleBadPath
	}

	if m.URL == "" {
		return ErrModuleEmptyURL
	}

	return nil
}

// validSubmoduleName validates storage names below .git/modules.
// Upstream Git's check_submodule_name at submodule-config.c#L214-L237
// in tag v2.54.0[1] rejects empty names and literal parent
// components. go-git additionally applies
// pathutil.IsUnsafeStorageName, which adds periods-only components,
// parent disguises and the components a filesystem folds to ".", and
// rejects NULs, leading/trailing separators and drive prefixes. Both
// separators are recognized on every host. C Git accepts some of
// these extra names; the periods-only restriction is policy, not an
// assertion of NTFS parent folding.
//
// [1]: https://github.com/git/git/blob/v2.54.0/submodule-config.c#L214-L237
func validSubmoduleName(name string) error {
	if name == "" {
		return ErrModuleBadName
	}
	if slices.ContainsFunc(strings.FieldsFunc(name, isPathSep), pathutil.IsUnsafeStorageName) {
		return ErrModuleBadName
	}
	// go-git-specific defensive checks beyond canonical Git.
	if strings.ContainsRune(name, 0) {
		return ErrModuleBadName
	}
	if isPathSep(rune(name[0])) || isPathSep(rune(name[len(name)-1])) {
		return ErrModuleBadName
	}
	if len(name) >= 2 && name[1] == ':' {
		return ErrModuleBadName
	}
	return nil
}

func isPathSep(r rune) bool { return r == '/' || r == '\\' }

func (m *Submodule) unmarshal(s *format.Subsection) {
	m.raw = s

	m.Name = m.raw.Name
	m.Path = m.raw.Option(pathKey)
	m.URL = m.raw.Option(urlKey)
	m.Branch = m.raw.Option(branchKey)
}

func (m *Submodule) marshal() *format.Subsection {
	if m.raw == nil {
		m.raw = &format.Subsection{}
	}

	m.raw.Name = m.Name
	if m.raw.Name == "" {
		m.raw.Name = m.Path
	}

	m.raw.SetOption(pathKey, m.Path)
	m.raw.SetOption(urlKey, m.URL)

	if m.Branch != "" {
		m.raw.SetOption(branchKey, m.Branch)
	}

	return m.raw
}
