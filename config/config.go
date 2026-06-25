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

// ErrInvalid is returned when a config key is invalid.
var ErrInvalid = errors.New("config invalid key in remote or branch")

// Scope defines the scope of a config file, such as local, global or system.
type Scope int

// Available ConfigScope's
const (
	LocalScope Scope = iota
	GlobalScope
	SystemScope
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
			merged := &format.Subsection{
				Name:    sub.Name,
				Options: append(format.Options(nil), sub.Options...),
			}
			replaced := false
			for i, existing := range ds.Subsections {
				if existing.IsName(sub.Name) {
					ds.Subsections[i] = merged
					replaced = true
					break
				}
			}
			if !replaced {
				ds.Subsections = append(ds.Subsections, merged)
			}
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
// Validate validates the remote fetch refspecs stored in the config. Higher
// level validation of remotes, branches and submodules is performed by the
// porcelain layer that owns those types.
func (c *Config) Validate() error {
	for _, sub := range c.Raw.Section("remote").Subsections {
		for _, f := range sub.Options.GetAll("fetch") {
			if err := plumbing.RefSpec(f).Validate(); err != nil {
				return err
			}
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

	return c.Validate()
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
