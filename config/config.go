// Package config contains the abstraction of multiple config files
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-git/go-billy/v6/osfs"

	"github.com/go-git/go-git/v6/internal/url"
	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/protocol"
)

const (
	// DefaultFetchRefSpec is the default refspec used for fetch.
	DefaultFetchRefSpec = "+refs/heads/*:refs/remotes/%s/*"
	// DefaultPushRefSpec is the default refspec used for push.
	DefaultPushRefSpec = "refs/heads/*:refs/heads/*"
	// DefaultProtocolVersion is the value assumed if none is defined
	// at the config file. This value is used to define when this section
	// should be marshalled or not.
	// Note that this does not need to align with the default protocol
	// version from plumbing/protocol.
	DefaultProtocolVersion = protocol.V0 // go-git only supports V0 at the moment

	// DefaultPackWindow holds the number of previous objects used to
	// generate deltas. The value 10 is the same used by git command.
	DefaultPackWindow = uint(10)
	// DefaultFileMode is the default file mode used by git command.
	DefaultFileMode = true
)

// ConfigStorer is a generic storage of Config object.
type ConfigStorer interface { //nolint:revive // stutters but is a well-established name
	Config() (*Config, error)
	SetConfig(*Config) error
}

var (
	// ErrInvalid is returned when a config key is invalid.
	ErrInvalid = errors.New("config invalid key in remote or branch")
	// ErrRemoteConfigNotFound is returned when a remote config is not found.
	ErrRemoteConfigNotFound = errors.New("remote config not found")
	// ErrRemoteConfigEmptyURL is returned when a remote config has an empty URL.
	ErrRemoteConfigEmptyURL = errors.New("remote config: empty URL")
	// ErrRemoteConfigEmptyName is returned when a remote config has an empty name.
	ErrRemoteConfigEmptyName = errors.New("remote config: empty name")
)

// Scope defines the scope of a config file, such as local, global or system.
type Scope int

// Available ConfigScope's
const (
	LocalScope Scope = iota
	GlobalScope
	SystemScope
)

const (
	remoteSection    = "remote"
	submoduleSection = "submodule"
	branchSection    = "branch"
	urlSection       = "url"
	fetchKey         = "fetch"
	urlKey           = "url"
	pushurlKey       = "pushurl"
	mirrorKey        = "mirror"
	mergeKey         = "merge"
	rebaseKey        = "rebase"
	descriptionKey   = "description"
)

// Config is a Git configuration, backed by a flat set of string keys in the
// canonical "section.subsection.name = value" model used by git. The Raw field
// is the single source of truth; the typed accessors and porcelain helpers read
// from and write to it.
//
// https://www.kernel.org/pub/software/scm/git/docs/git-config.html#FILES
type Config struct {
	// Raw holds the parsed key/value model. It is the canonical state of the
	// configuration; every accessor reads from and writes to it.
	Raw *format.Config
}

// Core groups the values of the [core] section.
type Core struct {
	// IsBare if true this repository is assumed to be bare and has no
	// working directory associated with it.
	IsBare bool
	// Worktree is the path to the root of the working tree.
	Worktree string
	// CommentChar is the character indicating the start of a comment for
	// commands like commit and tag.
	CommentChar string
	// RepositoryFormatVersion identifies the repository format and layout version.
	RepositoryFormatVersion format.RepositoryFormatVersion
	// AutoCRLF controls automatic line-ending conversion.
	AutoCRLF string
	// FileMode defines whether the executable bit of working tree files is honored.
	FileMode bool
	// HooksPath is the path to look for hooks instead of $GIT_DIR/hooks.
	HooksPath string
	// ProtectNTFS controls NTFS-specific path protections.
	ProtectNTFS OptBool
	// ProtectHFS controls HFS+-specific path protections.
	ProtectHFS OptBool
}

// User groups the values of the [user] section.
type User struct {
	// Name is the personal name of the author and committer of a commit.
	Name string
	// Email is the email of the author and committer of a commit.
	Email string
	// SigningKey is the key used when signing tags or commits.
	SigningKey string
}

// Identity is a name/email pair, used by the [author] and [committer] sections.
type Identity struct {
	Name  string
	Email string
}

// GPGSSH holds the [gpg "ssh"] subsection values.
type GPGSSH struct {
	// AllowedSignersFile is the path to the file containing allowed SSH signing keys.
	AllowedSignersFile string
}

// GPG groups the values of the [gpg] section.
type GPG struct {
	// Format specifies the signature format ("openpgp", "x509" or "ssh").
	Format string
	// SSH contains SSH-specific GPG configuration.
	SSH GPGSSH
}

// Pack groups the values of the [pack] section.
type Pack struct {
	// Window controls the size of the sliding window for delta compression.
	Window uint
	// ReadReverseIndex controls whether Git reads .rev files from disk.
	ReadReverseIndex bool
	// WriteReverseIndex controls whether Git writes .rev files.
	WriteReverseIndex bool
}

// Extensions groups the values of the [extensions] section.
type Extensions struct {
	// ObjectFormat specifies the hash algorithm to use ("sha1" or "sha256").
	ObjectFormat format.ObjectFormat
	// WorktreeConfig indicates that per-worktree config files are enabled.
	WorktreeConfig bool
}

// Index groups the values of the [index] section.
type Index struct {
	// SkipHash if true, the index checksum is not written or verified.
	SkipHash OptBool
}

// Init groups the values of the [init] section.
type Init struct {
	// DefaultBranch overrides the default branch name.
	DefaultBranch string
}

// UploadArchive groups the values of the [uploadArchive] section.
type UploadArchive struct {
	// AllowUnreachable allows clients to request archives using arbitrary
	// SHA-1 expressions.
	AllowUnreachable OptBool
}

// Signing holds a gpgSign flag, used by the [tag] and [commit] sections.
type Signing struct {
	GpgSign OptBool
}

// Protocol groups the values of the [protocol] section.
type Protocol struct {
	// Version sets the preferred version for the Git wire protocol.
	Version protocol.Version
}

// NewConfig returns a new Config populated with go-git's defaults.
func NewConfig() *Config {
	c := &Config{Raw: format.New()}
	c.Raw.Set("core.bare", "false")
	c.Raw.Set("core.filemode", strconv.FormatBool(DefaultFileMode))
	return c
}

// Core returns the values of the [core] section.
func (c *Config) Core() Core {
	var rfv format.RepositoryFormatVersion
	if c.Raw.Get("core.repositoryformatversion") == format.Version1 {
		rfv = format.Version1
	}
	return Core{
		IsBare:                  c.Raw.Bool("core.bare", false),
		Worktree:                c.Raw.Get("core.worktree"),
		CommentChar:             c.Raw.Get("core.commentChar"),
		RepositoryFormatVersion: rfv,
		AutoCRLF:                c.Raw.Get("core.autocrlf"),
		FileMode:                c.Raw.Bool("core.filemode", DefaultFileMode),
		HooksPath:               c.Raw.Get("core.hooksPath"),
		ProtectNTFS:             parseConfigBool(c.Raw.Get("core.protectNTFS")),
		ProtectHFS:              parseConfigBool(c.Raw.Get("core.protectHFS")),
	}
}

// SetBare sets core.bare.
func (c *Config) SetBare(v bool) { c.Raw.Set("core.bare", strconv.FormatBool(v)) }

// SetFileMode sets core.filemode.
func (c *Config) SetFileMode(v bool) { c.Raw.Set("core.filemode", strconv.FormatBool(v)) }

// SetWorktree sets or clears core.worktree.
func (c *Config) SetWorktree(v string) { setOrUnset(c.Raw, "core.worktree", v) }

// SetCommentChar sets or clears core.commentChar.
func (c *Config) SetCommentChar(v string) { setOrUnset(c.Raw, "core.commentChar", v) }

// SetAutoCRLF sets or clears core.autocrlf.
func (c *Config) SetAutoCRLF(v string) { setOrUnset(c.Raw, "core.autocrlf", v) }

// SetHooksPath sets or clears core.hooksPath.
func (c *Config) SetHooksPath(v string) { setOrUnset(c.Raw, "core.hooksPath", v) }

// SetProtectNTFS sets or clears core.protectNTFS.
func (c *Config) SetProtectNTFS(v OptBool) { setOptBool(c.Raw, "core.protectNTFS", v) }

// SetProtectHFS sets or clears core.protectHFS.
func (c *Config) SetProtectHFS(v OptBool) { setOptBool(c.Raw, "core.protectHFS", v) }

// SetRepositoryFormatVersion sets or clears core.repositoryformatversion.
func (c *Config) SetRepositoryFormatVersion(v format.RepositoryFormatVersion) {
	setOrUnset(c.Raw, "core.repositoryformatversion", string(v))
}

// User returns the values of the [user] section.
func (c *Config) User() User {
	return User{
		Name:       c.Raw.Get("user.name"),
		Email:      c.Raw.Get("user.email"),
		SigningKey: c.Raw.Get("user.signingKey"),
	}
}

// SetUser sets the [user] section values, clearing empty ones.
func (c *Config) SetUser(v User) {
	setOrUnset(c.Raw, "user.name", v.Name)
	setOrUnset(c.Raw, "user.email", v.Email)
	setOrUnset(c.Raw, "user.signingKey", v.SigningKey)
}

// Author returns the values of the [author] section.
func (c *Config) Author() Identity {
	return Identity{Name: c.Raw.Get("author.name"), Email: c.Raw.Get("author.email")}
}

// SetAuthor sets the [author] section values, clearing empty ones.
func (c *Config) SetAuthor(v Identity) {
	setOrUnset(c.Raw, "author.name", v.Name)
	setOrUnset(c.Raw, "author.email", v.Email)
}

// Committer returns the values of the [committer] section.
func (c *Config) Committer() Identity {
	return Identity{Name: c.Raw.Get("committer.name"), Email: c.Raw.Get("committer.email")}
}

// SetCommitter sets the [committer] section values, clearing empty ones.
func (c *Config) SetCommitter(v Identity) {
	setOrUnset(c.Raw, "committer.name", v.Name)
	setOrUnset(c.Raw, "committer.email", v.Email)
}

// Tag returns the values of the [tag] section.
func (c *Config) Tag() Signing {
	return Signing{GpgSign: parseConfigBool(c.Raw.Get("tag.gpgSign"))}
}

// SetTagGpgSign sets or clears tag.gpgSign.
func (c *Config) SetTagGpgSign(v OptBool) { setOptBool(c.Raw, "tag.gpgSign", v) }

// Commit returns the values of the [commit] section.
func (c *Config) Commit() Signing {
	return Signing{GpgSign: parseConfigBool(c.Raw.Get("commit.gpgSign"))}
}

// SetCommitGpgSign sets or clears commit.gpgSign.
func (c *Config) SetCommitGpgSign(v OptBool) { setOptBool(c.Raw, "commit.gpgSign", v) }

// GPG returns the values of the [gpg] section.
func (c *Config) GPG() GPG {
	return GPG{
		Format: c.Raw.Get("gpg.format"),
		SSH:    GPGSSH{AllowedSignersFile: c.Raw.Get("gpg.ssh.allowedSignersFile")},
	}
}

// SetGPGFormat sets or clears gpg.format.
func (c *Config) SetGPGFormat(v string) { setOrUnset(c.Raw, "gpg.format", v) }

// SetGPGSSHAllowedSignersFile sets or clears gpg.ssh.allowedSignersFile.
func (c *Config) SetGPGSSHAllowedSignersFile(v string) {
	setOrUnset(c.Raw, "gpg.ssh.allowedSignersFile", v)
}

// Pack returns the values of the [pack] section.
func (c *Config) Pack() Pack {
	w, _ := c.Raw.Uint("pack.window", uint64(DefaultPackWindow))
	return Pack{
		Window:            uint(w),
		ReadReverseIndex:  c.Raw.Bool("pack.readReverseIndex", true),
		WriteReverseIndex: c.Raw.Bool("pack.writeReverseIndex", true),
	}
}

// SetPackWindow sets pack.window.
func (c *Config) SetPackWindow(v uint) { c.Raw.Set("pack.window", strconv.FormatUint(uint64(v), 10)) }

// SetPackReadReverseIndex sets pack.readReverseIndex.
func (c *Config) SetPackReadReverseIndex(v bool) {
	c.Raw.Set("pack.readReverseIndex", strconv.FormatBool(v))
}

// SetPackWriteReverseIndex sets pack.writeReverseIndex.
func (c *Config) SetPackWriteReverseIndex(v bool) {
	c.Raw.Set("pack.writeReverseIndex", strconv.FormatBool(v))
}

// Index returns the values of the [index] section.
func (c *Config) Index() Index {
	return Index{SkipHash: parseConfigBool(c.Raw.Get("index.skipHash"))}
}

// SetIndexSkipHash sets or clears index.skipHash.
func (c *Config) SetIndexSkipHash(v OptBool) { setOptBool(c.Raw, "index.skipHash", v) }

// Init returns the values of the [init] section.
func (c *Config) Init() Init {
	return Init{DefaultBranch: c.Raw.Get("init.defaultBranch")}
}

// SetInitDefaultBranch sets or clears init.defaultBranch.
func (c *Config) SetInitDefaultBranch(v string) { setOrUnset(c.Raw, "init.defaultBranch", v) }

// UploadArchive returns the values of the [uploadArchive] section.
func (c *Config) UploadArchive() UploadArchive {
	return UploadArchive{AllowUnreachable: parseConfigBool(c.Raw.Get("uploadArchive.allowUnreachable"))}
}

// SetUploadArchiveAllowUnreachable sets or clears uploadArchive.allowUnreachable.
func (c *Config) SetUploadArchiveAllowUnreachable(v OptBool) {
	setOptBool(c.Raw, "uploadArchive.allowUnreachable", v)
}

// Extensions returns the values of the [extensions] section.
func (c *Config) Extensions() Extensions {
	return Extensions{
		ObjectFormat:   format.ObjectFormat(c.Raw.Get("extensions.objectformat")),
		WorktreeConfig: c.Raw.Bool("extensions.worktreeConfig", false),
	}
}

// SetObjectFormat sets or clears extensions.objectformat.
func (c *Config) SetObjectFormat(v format.ObjectFormat) {
	setOrUnset(c.Raw, "extensions.objectformat", string(v))
}

// SetWorktreeConfig sets or clears extensions.worktreeConfig.
func (c *Config) SetWorktreeConfig(v bool) {
	if v {
		c.Raw.Set("extensions.worktreeConfig", "true")
		return
	}
	c.Raw.Unset("extensions.worktreeConfig")
}

// Protocol returns the values of the [protocol] section.
func (c *Config) Protocol() Protocol {
	v := DefaultProtocolVersion
	if rv := c.Raw.Get("protocol.version"); rv != "" {
		if p, err := protocol.Parse(rv); err == nil {
			v = p
		}
	}
	return Protocol{Version: v}
}

// SetProtocolVersion sets protocol.version.
func (c *Config) SetProtocolVersion(v protocol.Version) {
	c.Raw.Set("protocol.version", v.String())
}

// Remotes returns the [remote] subsections keyed by name, with url.insteadOf
// rewrite rules applied to their URLs.
func (c *Config) Remotes() map[string]*RemoteConfig {
	m, _ := c.remotes()
	return m
}

// Remote returns the named remote or nil if it does not exist.
func (c *Config) Remote(name string) *RemoteConfig {
	return c.Remotes()[name]
}

func (c *Config) remotes() (map[string]*RemoteConfig, error) {
	out := make(map[string]*RemoteConfig)
	urls := c.urls()
	s := c.Raw.Section(remoteSection)
	for _, sub := range s.Subsections {
		r := &RemoteConfig{}
		if err := r.unmarshal(sub); err != nil {
			return nil, err
		}
		r.applyURLRules(urls)
		out[r.Name] = r
	}
	if len(s.Options) > 0 {
		r := &RemoteConfig{}
		if err := r.unmarshal(&format.Subsection{Name: "", Options: s.Options}); err != nil {
			return nil, err
		}
		r.applyURLRules(urls)
		out[r.Name] = r
	}
	return out, nil
}

// SetRemote writes the remote into Raw, replacing any existing remote of the
// same name.
func (c *Config) SetRemote(r *RemoteConfig) {
	c.setSubsection(remoteSection, r.Name, r.marshal())
}

// RemoveRemote removes the named remote.
func (c *Config) RemoveRemote(name string) { c.Raw.RemoveSubsection(remoteSection, name) }

// Branches returns the [branch] subsections keyed by name.
func (c *Config) Branches() map[string]*Branch {
	m, _ := c.branches()
	return m
}

// Branch returns the named branch or nil if it does not exist.
func (c *Config) Branch(name string) *Branch {
	return c.Branches()[name]
}

func (c *Config) branches() (map[string]*Branch, error) {
	out := make(map[string]*Branch)
	s := c.Raw.Section(branchSection)
	for _, sub := range s.Subsections {
		b := &Branch{}
		if err := b.unmarshal(sub); err != nil {
			return nil, err
		}
		out[b.Name] = b
	}
	return out, nil
}

// SetBranch writes the branch into Raw, replacing any existing branch of the
// same name.
func (c *Config) SetBranch(b *Branch) {
	c.setSubsection(branchSection, b.Name, b.marshal())
}

// RemoveBranch removes the named branch.
func (c *Config) RemoveBranch(name string) { c.Raw.RemoveSubsection(branchSection, name) }

// Submodules returns the [submodule] subsections keyed by name, skipping any
// with an invalid name or path.
func (c *Config) Submodules() map[string]*Submodule {
	out := make(map[string]*Submodule)
	s := c.Raw.Section(submoduleSection)
	for _, sub := range s.Subsections {
		m := &Submodule{}
		m.unmarshal(sub)
		if err := m.Validate(); errors.Is(err, ErrModuleBadPath) ||
			errors.Is(err, ErrModuleBadName) {
			continue
		}
		out[m.Name] = m
	}
	return out
}

// Submodule returns the named submodule or nil if it does not exist.
func (c *Config) Submodule(name string) *Submodule {
	return c.Submodules()[name]
}

// SetSubmodule writes the submodule into Raw, replacing any existing submodule
// of the same name. The path option is not stored in the repository config.
func (c *Config) SetSubmodule(m *Submodule) {
	sub := m.marshal()
	sub.RemoveOption(pathKey)
	c.setSubsection(submoduleSection, m.Name, sub)
}

// URLs returns the [url] subsections in file order.
func (c *Config) URLs() []*URL {
	return c.urls()
}

func (c *Config) urls() []*URL {
	s := c.Raw.Section(urlSection)
	out := make([]*URL, 0, len(s.Subsections))
	for _, sub := range s.Subsections {
		u := &URL{}
		_ = u.unmarshal(sub)
		out = append(out, u)
	}
	return out
}

// AddURL appends a url rewrite rule.
func (c *Config) AddURL(u *URL) {
	c.setSubsection(urlSection, u.Name, u.marshal())
}

// setSubsection replaces a subsection of the same name within section, or
// appends it if absent.
func (c *Config) setSubsection(section, name string, sub *format.Subsection) {
	s := c.Raw.Section(section)
	for i, ss := range s.Subsections {
		if ss.IsName(name) {
			s.Subsections[i] = sub
			return
		}
	}
	s.Subsections = append(s.Subsections, sub)
}

// Merge combines all the src Config objects into one. The objects are processed
// in the order they are passed on; later sources override values from earlier
// ones for the same key or subsection. Nil configs are ignored.
func (c *Config) merge(src *Config) {
	if src == nil || src.Raw == nil {
		return
	}
	for _, s := range src.Raw.Sections {
		if len(s.Options) == 0 && len(s.Subsections) == 0 {
			continue
		}
		ds := c.Raw.Section(s.Name)
		for _, o := range s.Options {
			ds.SetOption(o.Key, o.Value)
		}
		for _, sub := range s.Subsections {
			c.setSubsection(s.Name, sub.Name, &format.Subsection{
				Name:    sub.Name,
				Options: append(format.Options(nil), sub.Options...),
			})
		}
	}
}

// Merge combines all the src Config objects into one. The objects are processed
// in the order they are passed on, and later sources override earlier values
// for the same key or subsection. Empty configs are ignored.
func Merge(src ...*Config) Config {
	final := Config{Raw: format.New()}
	for _, c := range src {
		final.merge(c)
	}
	return final
}

// ReadConfig reads a config file from a io.Reader.
func ReadConfig(r io.Reader) (*Config, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	cfg := NewConfig()
	if err = cfg.Unmarshal(b); err != nil {
		return nil, err
	}

	return cfg, nil
}

// LoadConfig loads a config file from a given scope.
//
// Deprecated: Use the ConfigLoader plugin instead. This will be removed in v7.
func LoadConfig(scope Scope) (*Config, error) {
	if scope == LocalScope {
		return nil, fmt.Errorf("LocalScope should be read from the a ConfigStorer")
	}

	files, err := Paths(scope)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		f, err := osfs.Default.Open(file)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, err
		}

		defer func() { _ = f.Close() }()
		return ReadConfig(f)
	}

	return NewConfig(), nil
}

// Paths returns the config file location for a given scope.
//
// Deprecated: Use the ConfigLoader plugin instead.
// This will be removed in v7.
func Paths(scope Scope) ([]string, error) {
	var files []string
	switch scope {
	case GlobalScope:
		xdg := os.Getenv("XDG_CONFIG_HOME")
		if xdg != "" {
			files = append(files, filepath.Join(xdg, "git/config"))
		}

		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}

		files = append(files,
			filepath.Join(home, ".gitconfig"),
			filepath.Join(home, ".config/git/config"),
		)
	case SystemScope:
		files = append(files, "/etc/gitconfig")
	}

	return files, nil
}

// Validate validates the remotes and branches and sets default values.
func (c *Config) Validate() error {
	remotes, err := c.remotes()
	if err != nil {
		return err
	}
	for name, r := range remotes {
		if r.Name != name {
			return ErrInvalid
		}
		if err := r.Validate(); err != nil {
			return err
		}
	}

	branches, err := c.branches()
	if err != nil {
		return err
	}
	for name, b := range branches {
		if b.Name != name {
			return ErrInvalid
		}
		if err := b.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// Unmarshal parses a git-config file and stores it.
func (c *Config) Unmarshal(b []byte) error {
	c.Raw = format.New()
	if err := format.NewDecoder(bytes.NewBuffer(b)).Decode(c.Raw); err != nil {
		return err
	}

	if _, err := c.Raw.Uint("pack.window", uint64(DefaultPackWindow)); err != nil {
		return err
	}
	if rv := c.Raw.Get("protocol.version"); rv != "" {
		if _, err := protocol.Parse(rv); err != nil {
			return err
		}
	}
	if _, err := c.remotes(); err != nil {
		return err
	}
	if _, err := c.branches(); err != nil {
		return err
	}

	return nil
}

// Marshal returns Config encoded as a git-config file. Because Raw is the
// single source of truth, this simply encodes it.
func (c *Config) Marshal() ([]byte, error) {
	if c.Raw == nil {
		c.Raw = format.New()
	}

	buf := bytes.NewBuffer(nil)
	if err := format.NewEncoder(buf).Encode(c.Raw); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// setOrUnset sets key to val, or removes it when val is empty.
func setOrUnset(raw *format.Config, key, val string) {
	if val == "" {
		raw.Unset(key)
		return
	}
	raw.Set(key, val)
}

// setOptBool writes a tri-state boolean: the formatted value when set, or the
// key removed when unset.
func setOptBool(raw *format.Config, key string, v OptBool) {
	if v.IsSet() {
		raw.Set(key, v.FormatBool())
		return
	}
	raw.Unset(key)
}

// RemoteConfig contains the configuration for a given remote repository.
type RemoteConfig struct {
	// Name of the remote
	Name string
	// URLs the URLs of a remote repository. It must be non-empty. Fetch will
	// always use the first URL, while push will use all of them.
	URLs []string
	// Mirror indicates that the repository is a mirror of remote.
	Mirror bool

	// insteadOfRulesApplied have urls been modified
	insteadOfRulesApplied bool
	// originalURLs are the urls before applying insteadOf rules
	originalURLs []string

	// Fetch the default set of "refspec" for fetch operation
	Fetch []RefSpec

	// raw representation of the subsection, filled by marshal or unmarshal are
	// called
	raw *format.Subsection
}

// Validate validates the fields and sets the default values.
func (c *RemoteConfig) Validate() error {
	if c.Name == "" {
		return ErrRemoteConfigEmptyName
	}

	if len(c.URLs) == 0 {
		return ErrRemoteConfigEmptyURL
	}

	for _, r := range c.Fetch {
		if err := r.Validate(); err != nil {
			return err
		}
	}

	if len(c.Fetch) == 0 {
		c.Fetch = []RefSpec{RefSpec(fmt.Sprintf(DefaultFetchRefSpec, c.Name))}
	}

	return plumbing.NewRemoteHEADReferenceName(c.Name).Validate()
}

func (c *RemoteConfig) unmarshal(s *format.Subsection) error {
	c.raw = s

	fetch := []RefSpec{}
	for _, f := range c.raw.Options.GetAll(fetchKey) {
		rs := RefSpec(f)
		if err := rs.Validate(); err != nil {
			return err
		}

		fetch = append(fetch, rs)
	}

	c.Name = c.raw.Name
	c.URLs = append([]string(nil), c.raw.Options.GetAll(urlKey)...)
	c.URLs = append(c.URLs, c.raw.Options.GetAll(pushurlKey)...)
	c.Fetch = fetch
	if c.raw.Options.Has(mirrorKey) {
		if b, err := format.ParseBool(c.raw.Options.Get(mirrorKey)); err == nil {
			c.Mirror = b
		}
	}

	return nil
}

func (c *RemoteConfig) marshal() *format.Subsection {
	if c.raw == nil {
		c.raw = &format.Subsection{}
	}

	c.raw.Name = c.Name
	if len(c.URLs) == 0 {
		c.raw.RemoveOption(urlKey)
	} else {
		urls := c.URLs
		if c.insteadOfRulesApplied {
			urls = c.originalURLs
		}

		c.raw.SetOption(urlKey, urls...)
	}

	if len(c.Fetch) == 0 {
		c.raw.RemoveOption(fetchKey)
	} else {
		values := make([]string, 0, len(c.Fetch))
		for _, rs := range c.Fetch {
			values = append(values, rs.String())
		}

		c.raw.SetOption(fetchKey, values...)
	}

	if c.Mirror {
		c.raw.SetOption(mirrorKey, strconv.FormatBool(c.Mirror))
	}

	return c.raw
}

// IsFirstURLLocal returns true if the first URL is a local path.
func (c *RemoteConfig) IsFirstURLLocal() bool {
	return url.IsLocalEndpoint(c.URLs[0])
}

func (c *RemoteConfig) applyURLRules(urlRules []*URL) {
	// save original urls
	originalURLs := make([]string, len(c.URLs))
	copy(originalURLs, c.URLs)

	for i, u := range c.URLs {
		if rewrittenURL, matched := applyLongestInsteadOfMatch(u, urlRules); matched {
			c.URLs[i] = rewrittenURL
			c.insteadOfRulesApplied = true
		}
	}

	if c.insteadOfRulesApplied {
		c.originalURLs = originalURLs
	}
}
