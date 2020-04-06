package config

type Scope int

const (
	LocalScope Scope = iota
	GlobalScope
	SystemScope
)

type ScopedConfigs map[Scope]*Config

type Merged struct {
	scopedConfigs ScopedConfigs
}

type MergedSection struct {
	backingSection *Section
}

type MergedSubsection struct {
	backingSubsection *Subsection
}

type MergedSubsections []*MergedSubsection

func NewMerged() *Merged {
	cfg := &Merged{
		scopedConfigs: make(ScopedConfigs),
	}
	for s := LocalScope; s <= SystemScope; s++ {
		cfg.scopedConfigs[s] = New()
	}

	return cfg
}

func (m *Merged) ResetScopedConfig(scope Scope) {
	m.scopedConfigs[scope] = New()
}

func (m *Merged) ScopedConfig(scope Scope) *Config {
	return m.scopedConfigs[scope]
}

func (m *Merged) LocalConfig() *Config {
	return m.ScopedConfig(LocalScope)
}

func (m *Merged) GlobalConfig() *Config {
	return m.ScopedConfig(GlobalScope)
}

func (m *Merged) SystemConfig() *Config {
	return m.ScopedConfig(SystemScope)
}

func (m *Merged) SetLocalConfig(c *Config) {
	m.scopedConfigs[LocalScope] = c
}

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

func (m *Merged) Section(name string) *MergedSection {
	var mergedSection *MergedSection

	for s := SystemScope; s >= LocalScope; s-- {
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

func (m *Merged) AddOption(scope Scope, section string, subsection string, key string, value string) *Config {
	return m.ScopedConfig(scope).AddOption(section, subsection, key, value)
}

func (m *Merged) SetOption(scope Scope, section string, subsection string, key string, value string) *Config {
	return m.ScopedConfig(scope).SetOption(section, subsection, key, value)
}

func (m *Merged) RemoveSection(scope Scope, name string) *Config {
	return m.ScopedConfig(scope).RemoveSection(name)
}

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

