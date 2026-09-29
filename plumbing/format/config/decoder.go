package config

import (
	"errors"
	"io"

	"github.com/go-git/gcfg/v2"
)

// A Decoder reads and decodes config files from an input stream.
type Decoder struct {
	io.Reader
}

// NewDecoder returns a new decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r}
}

// Decode reads the whole config from its input and stores it in the
// value pointed to by config.
func (d *Decoder) Decode(config *Config) error {
	if config == nil {
		return errors.New("config is nil")
	}

	idx := newDecodeIndex(config)
	cb := func(s, ss, k, v string, _ bool) error {
		// A header line carries no option and is reported only so that
		// the section it names comes into existence.
		section, subsection := idx.lookup(s, ss)
		if k == "" {
			return nil
		}

		if subsection != nil {
			subsection.AddOption(k, v)
		} else {
			section.AddOption(k, v)
		}
		return nil
	}
	return gcfg.ReadWithCallback(d, cb)
}

// decodeIndex resolves the sections and subsections a decoder writes into.
//
// Config.Section and Section.Subsection scan the collection they are called
// on, comparing the wanted name against each entry in turn until one matches.
// Because the decoder resolves the section of every header and of every option
// line, decoding costs O(lines × sections × len(name)), so a moderately sized
// config from an untrusted source, a .gitmodules file for instance, is
// expensive to decode. The index replaces both scans with map lookups.
//
// It lives no longer than one Decode call, which is the config's only writer
// for that time, so the index cannot go stale. It is deliberately not kept on
// Config, whose Sections, Subsections and Options fields are exported and
// assigned to directly by other packages, with no hook an index could observe.
//
// Only the lookup is reimplemented. Sections and subsections are created
// through the same appendSection and appendSubsection that Config.Section and
// Section.Subsection use, so what has to stay in step with them is which entry
// a name resolves to, nothing more. TestDecodeMatchesPublicLookups checks that
// the two agree.
type decodeIndex struct {
	config *Config

	// sections maps the case-folded section name to the section
	// Config.Section resolves that name to, which is the last one added.
	sections map[string]*Section
	// subsections maps a section to its subsections by name. Subsection
	// names are matched exactly, so they are keyed verbatim.
	subsections map[*Section]map[string]*Subsection

	// name, subName, section and subsection hold the last resolved header.
	// gcfg assigns the section and subsection strings once per header and
	// reports every line of that section with those same strings, so the
	// comparison below takes the pointer-identity fast path in string
	// equality and never walks the name. That is what keeps the per-option
	// path independent of how long the name is.
	//
	// The maps alone are not enough. Case folding and hashing the name once
	// per option line costs O(lines × len(name)), which for an input that
	// is mostly one long section name is quadratic in its size, just as the
	// scan it replaced was. See TestDecodeScalesLinearly.
	name, subName string
	section       *Section
	subsection    *Subsection
}

// newDecodeIndex returns an index over the sections config already holds.
func newDecodeIndex(config *Config) *decodeIndex {
	idx := &decodeIndex{
		config:   config,
		sections: make(map[string]*Section, len(config.Sections)),
	}

	// The config is not guaranteed to be empty. Seeding forwards leaves the
	// last section of every name in the map, matching the backwards scan of
	// Config.Section.
	for _, s := range config.Sections {
		idx.sections[foldKey(s.Name)] = s
	}
	return idx
}

// lookup returns the section with the given name and, when subName is not
// empty, its subsection with that name, creating either if it does not exist
// yet, resolving names by the same rules as Config.Section and
// Section.Subsection.
func (idx *decodeIndex) lookup(name, subName string) (*Section, *Subsection) {
	if idx.section != nil && name == idx.name && subName == idx.subName {
		return idx.section, idx.subsection
	}

	key := foldKey(name)
	section, ok := idx.sections[key]
	if !ok {
		section = idx.config.appendSection(name)
		idx.sections[key] = section
	}

	var subsection *Subsection
	if subName != "" {
		subsection = idx.lookupSubsection(section, subName)
	}

	idx.name, idx.subName = name, subName
	idx.section, idx.subsection = section, subsection
	return section, subsection
}

// lookupSubsection returns the subsection of section with the given name,
// adding it if it does not exist yet. The per-section index is built on first
// use, so a config without subsections never pays for one, and like the one in
// newDecodeIndex it is seeded in order, leaving the last subsection of every
// name in the map to match the backwards scan of Section.Subsection.
func (idx *decodeIndex) lookupSubsection(section *Section, name string) *Subsection {
	subsections, ok := idx.subsections[section]
	if !ok {
		if idx.subsections == nil {
			idx.subsections = make(map[*Section]map[string]*Subsection)
		}

		subsections = make(map[string]*Subsection, len(section.Subsections))
		for _, ss := range section.Subsections {
			subsections[ss.Name] = ss
		}
		idx.subsections[section] = subsections
	}

	subsection, ok := subsections[name]
	if !ok {
		subsection = section.appendSubsection(name)
		subsections[name] = subsection
	}
	return subsection
}
