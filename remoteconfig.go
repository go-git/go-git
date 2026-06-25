package git

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/internal/url"
	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

var (
	// ErrRemoteConfigNotFound is returned when a remote config is not found.
	ErrRemoteConfigNotFound = errors.New("remote config not found")
	// ErrRemoteConfigEmptyURL is returned when a remote config has an empty URL.
	ErrRemoteConfigEmptyURL = errors.New("remote config: empty URL")
	// ErrRemoteConfigEmptyName is returned when a remote config has an empty name.
	ErrRemoteConfigEmptyName = errors.New("remote config: empty name")
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
	Fetch []plumbing.RefSpec

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
		c.Fetch = []plumbing.RefSpec{plumbing.RefSpec(fmt.Sprintf(config.DefaultFetchRefSpec, c.Name))}
	}

	return plumbing.NewRemoteHEADReferenceName(c.Name).Validate()
}

func (c *RemoteConfig) unmarshal(s *format.Subsection) error {
	c.raw = s

	fetch := []plumbing.RefSpec{}
	for _, f := range c.raw.Options.GetAll(fetchKey) {
		rs := plumbing.RefSpec(f)
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

// setSubsection replaces a subsection of the same name within section of cfg,
// or appends it if absent.
func setSubsection(cfg *config.Config, section, name string, sub *format.Subsection) {
	s := cfg.Raw.Section(section)
	for i, ss := range s.Subsections {
		if ss.IsName(name) {
			s.Subsections[i] = sub
			return
		}
	}
	s.Subsections = append(s.Subsections, sub)
}

// remoteConfigs returns the [remote] subsections of cfg keyed by name, with
// url.insteadOf rewrite rules applied to their URLs.
func remoteConfigs(cfg *config.Config) map[string]*RemoteConfig {
	m, _ := readRemoteConfigs(cfg)
	return m
}

func readRemoteConfigs(cfg *config.Config) (map[string]*RemoteConfig, error) {
	out := make(map[string]*RemoteConfig)
	urls := urlConfigs(cfg)
	s := cfg.Raw.Section(remoteSection)
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

// remoteConfig returns the named remote of cfg, or nil if it does not exist.
func remoteConfig(cfg *config.Config, name string) *RemoteConfig {
	return remoteConfigs(cfg)[name]
}

// setRemoteConfig writes r into cfg, replacing any existing remote of the same name.
func setRemoteConfig(cfg *config.Config, r *RemoteConfig) {
	setSubsection(cfg, remoteSection, r.Name, r.marshal())
}

// branchConfigs returns the [branch] subsections of cfg keyed by name.
func branchConfigs(cfg *config.Config) map[string]*Branch {
	m, _ := readBranchConfigs(cfg)
	return m
}

func readBranchConfigs(cfg *config.Config) (map[string]*Branch, error) {
	out := make(map[string]*Branch)
	s := cfg.Raw.Section(branchSection)
	for _, sub := range s.Subsections {
		b := &Branch{}
		if err := b.unmarshal(sub); err != nil {
			return nil, err
		}
		out[b.Name] = b
	}
	return out, nil
}

// branchConfig returns the named branch of cfg, or nil if it does not exist.
func branchConfig(cfg *config.Config, name string) *Branch {
	return branchConfigs(cfg)[name]
}

// setBranchConfig writes b into cfg, replacing any existing branch of the same name.
func setBranchConfig(cfg *config.Config, b *Branch) {
	setSubsection(cfg, branchSection, b.Name, b.marshal())
}

// submoduleConfigs returns the [submodule] subsections of cfg keyed by name,
// skipping any with an invalid name or path.
func submoduleConfigs(cfg *config.Config) map[string]*SubmoduleConfig {
	out := make(map[string]*SubmoduleConfig)
	s := cfg.Raw.Section(submoduleSection)
	for _, sub := range s.Subsections {
		m := &SubmoduleConfig{}
		m.unmarshal(sub)
		if err := m.Validate(); errors.Is(err, ErrModuleBadPath) ||
			errors.Is(err, ErrModuleBadName) {
			continue
		}
		out[m.Name] = m
	}
	return out
}

// submoduleConfig returns the named submodule of cfg, or nil if it does not exist.
func submoduleConfig(cfg *config.Config, name string) *SubmoduleConfig {
	return submoduleConfigs(cfg)[name]
}

// setSubmoduleConfig writes m into cfg, replacing any existing submodule of the
// same name. The path option is not stored in the repository config.
func setSubmoduleConfig(cfg *config.Config, m *SubmoduleConfig) {
	sub := m.marshal()
	sub.RemoveOption(pathKey)
	setSubsection(cfg, submoduleSection, m.Name, sub)
}

// urlConfigs returns the [url] subsections of cfg in file order.
func urlConfigs(cfg *config.Config) []*URL {
	s := cfg.Raw.Section(urlSection)
	out := make([]*URL, 0, len(s.Subsections))
	for _, sub := range s.Subsections {
		u := &URL{}
		_ = u.unmarshal(sub)
		out = append(out, u)
	}
	return out
}
