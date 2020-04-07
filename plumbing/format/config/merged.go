package config

// Scope defines which configuration we're reading / writing. Git uses
// a local config file in each repo (./.git/config) which corresponds
// with LocalScope here. It's values have the highest priority (i.e. they
// override those in the other two scopes). Next is the global config which
// could also be described as a user config since it lives in the
// ~/.gitconfig file. Last and lowest priority is the system config which
// resides in the /etc/gitconfig file.
type Scope int
const (
	SystemScope Scope = iota
	GlobalScope
	LocalScope
	NumScopes
)

type ScopedConfigs map[Scope]*Config

// Merged defines a read-only view of the priority-merged config params
// so that you can access the effective settings in the same way git does.
// For example, if a user has defined a user.name value in ~/.gitconfig but
// a different one in the current repo's ./.git/config file, this code:
// config.Merged.Section("user").Option("name") will give you the value
// from ./.git/config.
type Merged struct {
	scopedConfigs ScopedConfigs
}

// MergedSection is a read-only Section for merged config views.
type MergedSection struct {
	backingSection *Section
}

// MergedSubsection is a read-only Subsection for merged config views.
type MergedSubsection struct {
	backingSubsection *Subsection
}

type MergedSubsections []*MergedSubsection

func NewMerged() *Merged {
	cfg := &Merged{
		scopedConfigs: make(ScopedConfigs),
	}
	for s := SystemScope; s <= LocalScope; s++ {
		cfg.scopedConfigs[s] = New()
	}

	return cfg
}

func (m *Merged) ResetScopedConfig(scope Scope) {
	m.scopedConfigs[scope] = New()
}

// ScopedConfig allows accessing specific backing *Config instances.
func (m *Merged) ScopedConfig(scope Scope) *Config {
	return m.scopedConfigs[scope]
}

// LocalConfig allows accessing the local (i.e. ./.git/config) backing
// *Config instance.
func (m *Merged) LocalConfig() *Config {
	return m.ScopedConfig(LocalScope)
}

// GlobalConfig allows accessing the global (i.e. ~/.gitconfig) backing
// *Config instance.
func (m *Merged) GlobalConfig() *Config {
	return m.ScopedConfig(GlobalScope)
}

// SystemConfig allows accessing the system (i.e. /etc/gitconfig) backing
// *Config instance.
func (m *Merged) SystemConfig() *Config {
	return m.ScopedConfig(SystemScope)
}

// SetLocalConfig allows updating the local (i.e. ./.git/config) config. If you
// call config.SetConfig(...) on the containing top-level config instance after
// this, your new local config params will be written to ./.git/config.
func (m *Merged) SetLocalConfig(c *Config) {
	m.scopedConfigs[LocalScope] = c
}

// SetGlobalConfig allows updating the global (i.e. ~/.gitconfig) config. If you
// call config.SetConfig(...) on the containing top-level config instance after
// this, your new global config params will be written to ~/.gitconfig.
func (m *Merged) SetGlobalConfig(c *Config) {
	m.scopedConfigs[GlobalScope] = c
}

// Config.Section creates the section if it doesn't exist, which is not
// what we want in here.
func (c *Config) hasSection(name string) bool {
	sections := c.Sections
	var found bool

	for _, s := range sections {
		if s.IsName(name) {
			found = true
			break
		}
	}

	return found
}

// Section returns a read-only *MergedSection view of the config in which
// params are merged in the same order as git itself:
// Local overrides global overrides system.
func (m *Merged) Section(name string) *MergedSection {
	var mergedSection *MergedSection

	for s := SystemScope; s <= LocalScope; s++ {
		if m.scopedConfigs[s].hasSection(name) {
			sec := m.scopedConfigs[s].Section(name)
			if mergedSection == nil {
				mergedSection = NewMergedSection(sec)
			}

			if mergedSection.Options() == nil {
				mergedSection.backingSection.Options = sec.Options
			} else {
				for _, o := range sec.Options {
					mergedSection.backingSection.SetOption(o.Key, o.Value)
				}
			}

			if mergedSection.Subsections() == nil {
				mergedSection.backingSection.Subsections = sec.Subsections
			} else {
				for _, ss := range sec.Subsections {
					if mergedSection.HasSubsection(ss.Name) {
						for _, o := range ss.Options {
							mergedSection.backingSection.Subsection(ss.Name).SetOption(o.Key, o.Value)
						}
					} else {
						mergedSection.backingSection.Subsections = append(mergedSection.backingSection.Subsections, ss)
					}
				}
			}
		}
	}

	if mergedSection != nil {
		mergedSection.backingSection.Name = name
	}

	return mergedSection
}

// AddOption works just like config.AddOption except that it takes a Scope as its first argument.
// This defines which config scope (local, global, or system) this option should be added to.
func (m *Merged) AddOption(scope Scope, section string, subsection string, key string, value string) *Config {
	return m.ScopedConfig(scope).AddOption(section, subsection, key, value)
}

// SetOption works just like config.SetOption except that it takes a Scope as its first argument.
// This defines which config scope (local, global, or system) this option should be set in.
func (m *Merged) SetOption(scope Scope, section string, subsection string, key string, value string) *Config {
	return m.ScopedConfig(scope).SetOption(section, subsection, key, value)
}

// RemoveSection works just like config.RemoveSection except that it takes a Scope as its first argument.
// This defines which config scope (local, global, or system) the section should be removed from.
func (m *Merged) RemoveSection(scope Scope, name string) *Config {
	return m.ScopedConfig(scope).RemoveSection(name)
}

// RemoveSubsection works just like config.RemoveSubsection except that it takes a Scope as its first argument.
// This defines which config scope (local, global, or system) the subsection should be removed from.
func (m *Merged) RemoveSubsection(scope Scope, section string, subsection string) *Config {
	return m.ScopedConfig(scope).RemoveSubsection(section, subsection)
}

func copyOptions(os Options) Options {
	copiedOptions := make(Options, 0)

	for _, o := range os {
		copiedOptions = append(copiedOptions, o)
	}

	return copiedOptions
}

func copySubsections(ss Subsections) Subsections {
	copiedSubsections := make(Subsections, 0)

	for _, ss := range ss {
		copiedSubsections = append(copiedSubsections, &Subsection{
			Name:    ss.Name,
			Options: copyOptions(ss.Options),
		})
	}

	return copiedSubsections
}

func NewMergedSection(backing *Section) *MergedSection {
	return &MergedSection{
		backingSection: &Section{
			Name: backing.Name,
			Options: copyOptions(backing.Options),
			Subsections: copySubsections(backing.Subsections),
		},
	}
}

func (ms *MergedSection) Name() string {
	return ms.backingSection.Name
}

func (ms *MergedSection) IsName(name string) bool {
	return ms.backingSection.IsName(name)
}

func (ms *MergedSection) Options() []*Option {
	return ms.backingSection.Options
}

func (ms *MergedSection) Option(key string) string {
	return ms.backingSection.Option(key)
}

func (ms *MergedSection) Subsections() MergedSubsections {
	mss := make(MergedSubsections, 0)
	for _, ss := range ms.backingSection.Subsections {
		mss = append(mss, NewMergedSubsection(ss))
	}
	return mss
}

func (ms *MergedSection) Subsection(name string) *MergedSubsection {
	return NewMergedSubsection(ms.backingSection.Subsection(name))
}

func (ms *MergedSection) HasSubsection(name string) bool {
	return ms.backingSection.HasSubsection(name)
}

func NewMergedSubsection(backing *Subsection) *MergedSubsection {
	return &MergedSubsection{backingSubsection: backing}
}

func (mss *MergedSubsection) Name() string {
	return mss.backingSubsection.Name
}

func (mss *MergedSubsection) IsName(name string) bool {
	return mss.backingSubsection.IsName(name)
}

func (mss *MergedSubsection) Options() []*Option {
	return mss.backingSubsection.Options
}

func (mss *MergedSubsection) Option(key string) string {
	return mss.backingSubsection.Option(key)
}

