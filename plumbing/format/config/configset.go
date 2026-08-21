package config

// ConfigSet provides read-only flat-key access across multiple sources in
// precedence order, the way git's config_set composes several configuration
// files (system, global, local, ...) into one effective view. The first source
// has the highest precedence. Because ConfigSet itself is a Getter, sets nest:
// a per-scope ConfigSet can be one source of a larger effective view.
//
// A ConfigSet never mutates its sources and is not encodable: it is a Getter
// but not a Setter, and is not accepted by Encoder. It is purely a read view,
// so the writable one-file Config keeps accurate per-file semantics while
// effective, multi-source resolution is an explicit, separate concept. A
// ConfigSet observes later mutations to its underlying sources, since it holds
// references rather than copies of their contents.
type ConfigSet struct { //nolint:revive // "config set" mirrors git's config_set terminology
	sources []Getter // highest precedence first
}

// NewConfigSet returns a ConfigSet over the given sources, highest precedence
// first. Nil sources are ignored, and the slice is copied so later mutation of
// the argument does not affect the set.
func NewConfigSet(sources ...Getter) *ConfigSet {
	srcs := make([]Getter, 0, len(sources))
	for _, s := range sources {
		if s != nil {
			srcs = append(srcs, s)
		}
	}
	return &ConfigSet{sources: srcs}
}

// Sources returns the sources that make up the set, highest precedence first.
func (s *ConfigSet) Sources() []Getter {
	return append([]Getter(nil), s.sources...)
}

// Lookup returns the highest-precedence value set for the canonical key and
// whether it was present in any source.
func (s *ConfigSet) Lookup(key string) (string, bool) {
	for _, c := range s.sources {
		if v, ok := c.Lookup(key); ok {
			return v, true
		}
	}
	return "", false
}

// Get returns the highest-precedence value for the canonical key, or the empty
// string if it is not present in any source.
func (s *ConfigSet) Get(key string) string {
	v, _ := s.Lookup(key)
	return v
}

// GetAll returns every value set for the canonical key across all sources, in
// ascending precedence order (lowest-precedence source first), matching git's
// git config --get-all ordering across files.
func (s *ConfigSet) GetAll(key string) []string {
	var all []string
	for i := len(s.sources) - 1; i >= 0; i-- {
		all = append(all, s.sources[i].GetAll(key)...)
	}
	return all
}

// Has reports whether the canonical key is present in any source.
func (s *ConfigSet) Has(key string) bool {
	_, ok := s.Lookup(key)
	return ok
}

// String returns the highest-precedence string value of the canonical key, or
// def when it is absent from every source.
func (s *ConfigSet) String(key, def string) string {
	if v, ok := s.Lookup(key); ok {
		return v
	}
	return def
}

// Bool returns the highest-precedence boolean value of the canonical key, or
// def when it is absent. A present but unparseable value returns def together
// with ErrInvalidBool.
func (s *ConfigSet) Bool(key string, def bool) (bool, error) {
	v, ok := s.Lookup(key)
	if !ok {
		return def, nil
	}
	b, err := ParseBool(v)
	if err != nil {
		return def, err
	}
	return b, nil
}

// Int returns the highest-precedence integer value of the canonical key, or def
// when it is absent. A present but unparseable value returns def together with
// an error.
func (s *ConfigSet) Int(key string, def int) (int, error) {
	v, ok := s.Lookup(key)
	if !ok {
		return def, nil
	}
	i, err := ParseInt(v)
	if err != nil {
		return def, err
	}
	return i, nil
}

// Int64 returns the highest-precedence int64 value of the canonical key, or def
// when it is absent. A present but unparseable value returns def together with
// an error.
func (s *ConfigSet) Int64(key string, def int64) (int64, error) {
	v, ok := s.Lookup(key)
	if !ok {
		return def, nil
	}
	i, err := ParseInt64(v)
	if err != nil {
		return def, err
	}
	return i, nil
}

// Uint returns the highest-precedence unsigned value of the canonical key, or
// def when it is absent. A present but unparseable value returns def together
// with an error.
func (s *ConfigSet) Uint(key string, def uint) (uint, error) {
	v, ok := s.Lookup(key)
	if !ok {
		return def, nil
	}
	u, err := ParseUint(v)
	if err != nil {
		return def, err
	}
	return u, nil
}

// Uint64 returns the highest-precedence unsigned 64-bit value of the canonical
// key, or def when it is absent. A present but unparseable value returns def
// together with an error.
func (s *ConfigSet) Uint64(key string, def uint64) (uint64, error) {
	v, ok := s.Lookup(key)
	if !ok {
		return def, nil
	}
	u, err := ParseUint64(v)
	if err != nil {
		return def, err
	}
	return u, nil
}
