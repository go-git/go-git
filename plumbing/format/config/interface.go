package config

var (
	_ Getter = (*Config)(nil)
	_ Getter = (*ConfigSet)(nil)
)

// Getter is a read-only, flat-key view of git configuration. A single parsed
// file (*Config), an effective multi-source view (*ConfigSet), and any
// caller-supplied source all satisfy it, so they can be layered together with
// NewConfigSet. It is the minimal primitive the typed accessors build on:
// Lookup resolves the winning value, GetAll the full multi-valued history.
type Getter interface {
	// Lookup returns the value set for the canonical key and whether it was
	// present at all.
	Lookup(key string) (string, bool)
	// GetAll returns every value set for the canonical key, in ascending
	// precedence order (oldest/lowest first).
	GetAll(key string) []string
}
