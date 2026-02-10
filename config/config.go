// Package config contains the abstraction of multiple config files
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
)

// ConfigStorer is a generic storage of Config object.
type ConfigStorer interface {
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

// Config contains the repository configuration
// https://www.kernel.org/pub/software/scm/git/docs/git-config.html#FILES
type Config struct {
	Core struct {
		// IsBare if true this repository is assumed to be bare and has no
		// working directory associated with it.
		IsBare bool
		// Worktree is the path to the root of the working tree.
		Worktree string
		// CommentChar is the character indicating the start of a
		// comment for commands like commit and tag
		CommentChar string
		// RepositoryFormatVersion identifies the repository format and layout version.
		RepositoryFormatVersion format.RepositoryFormatVersion
		// AutoCRLF if "true", all the CRLF line endings in the worktree will be
		// converted to LF when added to the repository, and vice versa on checkout.
		// If set to "input", only worktree-to-repository conversion is performed.
		AutoCRLF string
		// FileMode defines whether the executable bit of working tree files is to be honored.
		// If "false", when an index node is an Executable and is comparing hash
		// against local file, 0644 will be used as the value of its mode. The original
		// value of mode is left unchanged in the index.
		FileMode bool
	}

	User struct {
		// Name is the personal name of the author and the committer of a commit.
		Name string
		// Email is the email of the author and the committer of a commit.
		Email string
	}

	Author struct {
		// Name is the personal name of the author of a commit.
		Name string
		// Email is the email of the author of a commit.
		Email string
	}

	Committer struct {
		// Name is the personal name of the committer of a commit.
		Name string
		// Email is the email of the committer of a commit.
		Email string
	}

	GPG struct {
		// Format specifies the signature format to use when signing commits and tags.
		// Valid values are "openpgp" (default) and "ssh".
		Format string
		// SSH contains SSH-specific GPG configuration.
		SSH struct {
			// AllowedSignersFile is the path to the file containing allowed SSH signing keys.
			AllowedSignersFile string
		}
	}

	Pack struct {
		// Window controls the size of the sliding window for delta
		// compression.  The default is 10.  A value of 0 turns off
		// delta compression entirely.
		Window uint
	}

	Init struct {
		// DefaultBranch Allows overriding the default branch name
		// e.g. when initializing a new repository or when cloning
		// an empty repository.
		DefaultBranch string
	}

	Extensions struct {
		// ObjectFormat specifies the hash algorithm to use. The
		// acceptable values are sha1 and sha256. If not specified,
		// sha1 is assumed. It is an error to specify this key unless
		// core.repositoryFormatVersion is 1.
		//
		// This setting must not be changed after repository initialization
		// (e.g. clone or init).
		ObjectFormat format.ObjectFormat
	}

	Protocol struct {
		// Version sets the preferred version for the Git wire protocol.
		// When set, clients will attempt to communicate with a server
		// using the specified protocol version. If the server does not
		// support it, communication falls back to version 0. If unset,
		// the default version will be used. Supported versions:
		//
		//   0 - the original wire protocol.
		//   1 - the original wire protocol with the addition of a
		// version string in the initial response from the server.
		//   2 - Wire protocol version 2.
		Version protocol.Version
	}

	// Remotes list of repository remotes, the key of the map is the name
	// of the remote, should equal to RemoteConfig.Name.
	Remotes map[string]*RemoteConfig
	// Submodules list of repository submodules, the key of the map is the name
	// of the submodule, should equal to Submodule.Name.
	Submodules map[string]*Submodule
	// Branches list of branches, the key is the branch name and should
	// equal Branch.Name
	Branches map[string]*Branch
	// URLs list of url rewrite rules, if repo url starts with URL.InsteadOf value, it will be replaced with the
	// key instead.
	URLs map[string]*URL
	// Raw contains the raw information of a config file. The main goal is
	// preserve the parsed information from the original format, to avoid
	// dropping unsupported fields.
	Raw *format.Config
}

// Merge combines all the src Config objects into one.
// The objects are processed in the order they are passed on, and will
// override any existing values. Empty configs are ignored.
func Merge(src ...*Config) Config {
	var final Config

	for _, c := range src {
		if c == nil {
			continue
		}

		merge(&final, c)
	}

	return final
}

func merge(dst, src any) {
	tv := reflect.ValueOf(dst).Elem()
	sv := reflect.ValueOf(src).Elem()

	for i := 0; i < tv.NumField(); i++ {
		df := tv.Field(i)
		sf := sv.Field(i)

		if !df.CanSet() || sf.IsZero() {
			continue
		}

		switch df.Kind() {
		case reflect.Struct:
			// Handle nested fields which are based off structs.
			merge(df.Addr().Interface(), sf.Addr().Interface())

		case reflect.Ptr:
			if sf.IsNil() {
				continue
			}
			if df.IsNil() {
				df.Set(reflect.New(df.Type().Elem()))
			}
			// Same as per reflect.Struct, but for a struct pointer.
			if df.Elem().Kind() == reflect.Struct {
				merge(df.Interface(), sf.Interface())
			} else {
				df.Set(sf)
			}

		default:
			df.Set(sf)
		}
	}
}

// NewConfig returns a new empty Config.
func NewConfig() *Config {
	config := &Config{
		Remotes:    make(map[string]*RemoteConfig),
		Submodules: make(map[string]*Submodule),
		Branches:   make(map[string]*Branch),
		URLs:       make(map[string]*URL),
		Raw:        format.New(),
	}

	config.Core.FileMode = DefaultFileMode
	config.Pack.Window = DefaultPackWindow
	config.Protocol.Version = DefaultProtocolVersion

	return config
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

// LoadConfig loads a config file from a given scope. The returned Config,
// contains exclusively information from the given scope. If it couldn't find a
// config file to the given scope, an empty one is returned.
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

// Validate validates the fields and sets the default values.
func (c *Config) Validate() error {
	for name, r := range c.Remotes {
		if r.Name != name {
			return ErrInvalid
		}

		if err := r.Validate(); err != nil {
			return err
		}
	}

	for name, b := range c.Branches {
		if b.Name != name {
			return ErrInvalid
		}

		if err := b.Validate(); err != nil {
			return err
		}
	}

	return nil
}

const (
	remoteSection              = "remote"
	submoduleSection           = "submodule"
	branchSection              = "branch"
	coreSection                = "core"
	packSection                = "pack"
	userSection                = "user"
	authorSection              = "author"
	committerSection           = "committer"
	gpgSection                 = "gpg"
	initSection                = "init"
	urlSection                 = "url"
	extensionsSection          = "extensions"
	protocolSection            = "protocol"
	fetchKey                   = "fetch"
	urlKey                     = "url"
	pushurlKey                 = "pushurl"
	bareKey                    = "bare"
	worktreeKey                = "worktree"
	commentCharKey             = "commentChar"
	windowKey                  = "window"
	mergeKey                   = "merge"
	rebaseKey                  = "rebase"
	nameKey                    = "name"
	emailKey                   = "email"
	descriptionKey             = "description"
	defaultBranchKey           = "defaultBranch"
	repositoryFormatVersionKey = "repositoryformatversion"
	objectFormatKey            = "objectformat"
	mirrorKey                  = "mirror"
	versionKey                 = "version"
	autoCRLFKey                = "autocrlf"
	fileModeKey                = "filemode"
	formatKey                  = "format"
	allowedSignersFileKey      = "allowedSignersFile"

	// DefaultPackWindow holds the number of previous objects used to
	// generate deltas. The value 10 is the same used by git command.
	DefaultPackWindow = uint(10)
	// DefaultFileMode is the default file mode used by git command.
	DefaultFileMode = true
)

// Unmarshal parses a git-config file and stores it.
func (c *Config) Unmarshal(b []byte) error {
	r := bytes.NewBuffer(b)
	d := format.NewDecoder(r)

	c.Raw = format.New()
	if err := d.Decode(c.Raw); err != nil {
		return err
	}

	c.unmarshalCore()
	c.unmarshalExtensions()
	c.unmarshalUser()
	c.unmarshalGPG()
	c.unmarshalInit()
	if err := c.unmarshalPack(); err != nil {
		return err
	}
	unmarshalSubmodules(c.Raw, c.Submodules)

	if err := c.unmarshalBranches(); err != nil {
		return err
	}

	if err := c.unmarshalURLs(); err != nil {
		return err
	}

	if err := c.unmarshalProtocol(); err != nil {
		return err
	}

	return c.unmarshalRemotes()
}

func (c *Config) unmarshalCore() {
	s := c.Raw.Section(coreSection)
	if s.Options.Get(bareKey) == "true" {
		c.Core.IsBare = true
	}

	c.Core.Worktree = s.Options.Get(worktreeKey)
	c.Core.CommentChar = s.Options.Get(commentCharKey)
	c.Core.AutoCRLF = s.Options.Get(autoCRLFKey)

	if fileMode := s.Options.Get(fileModeKey); fileMode == "false" {
		c.Core.FileMode = false
	}

	if s.Options.Get(repositoryFormatVersionKey) == string(format.Version1) {
		c.Core.RepositoryFormatVersion = format.Version1
	}
}

func (c *Config) unmarshalExtensions() {
	s := c.Raw.Section(extensionsSection)
	c.Extensions.ObjectFormat = format.ObjectFormat(s.Options.Get(objectFormatKey))
}

func (c *Config) unmarshalUser() {
	s := c.Raw.Section(userSection)
	c.User.Name = s.Options.Get(nameKey)
	c.User.Email = s.Options.Get(emailKey)

	s = c.Raw.Section(authorSection)
	c.Author.Name = s.Options.Get(nameKey)
	c.Author.Email = s.Options.Get(emailKey)

	s = c.Raw.Section(committerSection)
	c.Committer.Name = s.Options.Get(nameKey)
	c.Committer.Email = s.Options.Get(emailKey)
}

func (c *Config) unmarshalGPG() {
	s := c.Raw.Section(gpgSection)
	c.GPG.Format = s.Options.Get(formatKey)

	// SSH subsection is parsed separately since it uses subsections
	for _, sub := range s.Subsections {
		if sub.Name == "ssh" {
			c.GPG.SSH.AllowedSignersFile = sub.Options.Get(allowedSignersFileKey)
		}
	}
}

func (c *Config) unmarshalPack() error {
	s := c.Raw.Section(packSection)
	window := s.Options.Get(windowKey)
	if window == "" {
		c.Pack.Window = DefaultPackWindow
	} else {
		winUint, err := strconv.ParseUint(window, 10, 32)
		if err != nil {
			return err
		}
		c.Pack.Window = uint(winUint)
	}
	return nil
}

func (c *Config) unmarshalRemotes() error {
	s := c.Raw.Section(remoteSection)
	for _, sub := range s.Subsections {
		r := &RemoteConfig{}
		if err := r.unmarshal(sub); err != nil {
			return err
		}

		c.Remotes[r.Name] = r
	}

	// Check if the main remote section still has options
	// after unmarshaling named subsections - this indicates an empty subsection name
	if len(s.Options) > 0 {
		emptySubsection := &format.Subsection{
			Name:    "",
			Options: s.Options,
		}

		r := &RemoteConfig{}
		if err := r.unmarshal(emptySubsection); err != nil {
			return err
		}

		c.Remotes[r.Name] = r
	}
	// Apply insteadOf url rules
	for _, r := range c.Remotes {
		r.applyURLRules(c.URLs)
	}

	return nil
}

func (c *Config) unmarshalURLs() error {
	s := c.Raw.Section(urlSection)
	for _, sub := range s.Subsections {
		r := &URL{}
		if err := r.unmarshal(sub); err != nil {
			return err
		}

		c.URLs[r.Name] = r
	}

	return nil
}

func unmarshalSubmodules(fc *format.Config, submodules map[string]*Submodule) {
	s := fc.Section(submoduleSection)
	for _, sub := range s.Subsections {
		m := &Submodule{}
		m.unmarshal(sub)

		if errors.Is(m.Validate(), ErrModuleBadPath) {
			continue
		}

		submodules[m.Name] = m
	}
}

func (c *Config) unmarshalBranches() error {
	bs := c.Raw.Section(branchSection)
	for _, sub := range bs.Subsections {
		b := &Branch{}

		if err := b.unmarshal(sub); err != nil {
			return err
		}

		c.Branches[b.Name] = b
	}
	return nil
}

func (c *Config) unmarshalProtocol() error {
	s := c.Raw.Section(protocolSection)

	c.Protocol.Version = DefaultProtocolVersion

	// If empty, don't try to parse and instead fallback
	// to default protocol version.
	if rv := s.Options.Get(versionKey); rv != "" {
		v, err := protocol.Parse(rv)
		if err != nil {
			return err
		}
		c.Protocol.Version = v
	}

	return nil
}

func (c *Config) unmarshalInit() {
	s := c.Raw.Section(initSection)
	c.Init.DefaultBranch = s.Options.Get(defaultBranchKey)
}

// Marshal returns Config encoded as a git-config file.
func (c *Config) Marshal() ([]byte, error) {
	c.marshalCore()
	c.marshalExtensions()
	c.marshalUser()
	c.marshalGPG()
	c.marshalPack()
	c.marshalRemotes()
	c.marshalSubmodules()
	c.marshalBranches()
	c.marshalURLs()
	c.marshalProtocol()
	c.marshalInit()

	buf := bytes.NewBuffer(nil)
	if err := format.NewEncoder(buf).Encode(c.Raw); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (c *Config) marshalCore() {
	s := c.Raw.Section(coreSection)
	s.SetOption(bareKey, fmt.Sprintf("%t", c.Core.IsBare))
	if string(c.Core.RepositoryFormatVersion) != "" {
		s.SetOption(repositoryFormatVersionKey, string(c.Core.RepositoryFormatVersion))
	}

	if c.Core.Worktree != "" {
		s.SetOption(worktreeKey, c.Core.Worktree)
	}

	if c.Core.AutoCRLF != "" {
		s.SetOption(autoCRLFKey, c.Core.AutoCRLF)
	}

	s.SetOption(fileModeKey, fmt.Sprintf("%t", c.Core.FileMode))
}

func (c *Config) marshalExtensions() {
	// Extensions are only supported on Version 1, therefore
	// ignore them otherwise.
	if c.Core.RepositoryFormatVersion == format.Version1 &&
		c.Extensions.ObjectFormat != format.UnsetObjectFormat {
		s := c.Raw.Section(extensionsSection)
		s.SetOption(objectFormatKey, string(c.Extensions.ObjectFormat))
	}
}

func (c *Config) marshalUser() {
	s := c.Raw.Section(userSection)
	if c.User.Name != "" {
		s.SetOption(nameKey, c.User.Name)
	}

	if c.User.Email != "" {
		s.SetOption(emailKey, c.User.Email)
	}

	s = c.Raw.Section(authorSection)
	if c.Author.Name != "" {
		s.SetOption(nameKey, c.Author.Name)
	}

	if c.Author.Email != "" {
		s.SetOption(emailKey, c.Author.Email)
	}

	s = c.Raw.Section(committerSection)
	if c.Committer.Name != "" {
		s.SetOption(nameKey, c.Committer.Name)
	}

	if c.Committer.Email != "" {
		s.SetOption(emailKey, c.Committer.Email)
	}
}

func (c *Config) marshalGPG() {
	s := c.Raw.Section(gpgSection)
	if c.GPG.Format != "" {
		s.SetOption(formatKey, c.GPG.Format)
	}

	// SSH subsection
	if c.GPG.SSH.AllowedSignersFile != "" {
		sub := s.Subsection("ssh")
		sub.SetOption(allowedSignersFileKey, c.GPG.SSH.AllowedSignersFile)
	}
}

func (c *Config) marshalPack() {
	s := c.Raw.Section(packSection)
	if c.Pack.Window != DefaultPackWindow {
		s.SetOption(windowKey, fmt.Sprintf("%d", c.Pack.Window))
	}
}

func (c *Config) marshalRemotes() {
	s := c.Raw.Section(remoteSection)
	newSubsections := make(format.Subsections, 0, len(c.Remotes))
	added := make(map[string]bool)
	for _, subsection := range s.Subsections {
		if remote, ok := c.Remotes[subsection.Name]; ok {
			newSubsections = append(newSubsections, remote.marshal())
			added[subsection.Name] = true
		}
	}

	remoteNames := make([]string, 0, len(c.Remotes))
	for name := range c.Remotes {
		remoteNames = append(remoteNames, name)
	}

	sort.Strings(remoteNames)

	for _, name := range remoteNames {
		if !added[name] {
			newSubsections = append(newSubsections, c.Remotes[name].marshal())
		}
	}

	s.Subsections = newSubsections
}

func (c *Config) marshalSubmodules() {
	s := c.Raw.Section(submoduleSection)
	s.Subsections = make(format.Subsections, len(c.Submodules))

	var i int
	for _, r := range c.Submodules {
		section := r.marshal()
		// the submodule section at config is a subset of the .gitmodule file
		// we should remove the non-valid options for the config file.
		section.RemoveOption(pathKey)
		s.Subsections[i] = section
		i++
	}
}

func (c *Config) marshalBranches() {
	s := c.Raw.Section(branchSection)
	newSubsections := make(format.Subsections, 0, len(c.Branches))
	added := make(map[string]bool)
	for _, subsection := range s.Subsections {
		if branch, ok := c.Branches[subsection.Name]; ok {
			newSubsections = append(newSubsections, branch.marshal())
			added[subsection.Name] = true
		}
	}

	branchNames := make([]string, 0, len(c.Branches))
	for name := range c.Branches {
		branchNames = append(branchNames, name)
	}

	sort.Strings(branchNames)

	for _, name := range branchNames {
		if !added[name] {
			newSubsections = append(newSubsections, c.Branches[name].marshal())
		}
	}

	s.Subsections = newSubsections
}

func (c *Config) marshalURLs() {
	s := c.Raw.Section(urlSection)
	s.Subsections = make(format.Subsections, len(c.URLs))

	var i int
	for _, r := range c.URLs {
		section := r.marshal()
		// the submodule section at config is a subset of the .gitmodule file
		// we should remove the non-valid options for the config file.
		s.Subsections[i] = section
		i++
	}
}

func (c *Config) marshalProtocol() {
	// Only marshal protocol section if a version was set.
	if c.Protocol.Version != DefaultProtocolVersion {
		s := c.Raw.Section(protocolSection)
		s.SetOption(versionKey, c.Protocol.Version.String())
	}
}

func (c *Config) marshalInit() {
	s := c.Raw.Section(initSection)
	if c.Init.DefaultBranch != "" {
		s.SetOption(defaultBranchKey, c.Init.DefaultBranch)
	}
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
	c.Mirror = c.raw.Options.Get(mirrorKey) == "true"

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
		var values []string
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

func (c *RemoteConfig) applyURLRules(urlRules map[string]*URL) {
	// save original urls
	originalURLs := make([]string, len(c.URLs))
	copy(originalURLs, c.URLs)

	for i, url := range c.URLs {
		if matchingURLRule := findLongestInsteadOfMatch(url, urlRules); matchingURLRule != nil {
			c.URLs[i] = matchingURLRule.ApplyInsteadOf(c.URLs[i])
			c.insteadOfRulesApplied = true
		}
	}

	if c.insteadOfRulesApplied {
		c.originalURLs = originalURLs
	}
}
