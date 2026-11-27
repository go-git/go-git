// Package config provides struct-tag-based marshaling and unmarshaling of
// Git INI-style configuration files.
//
// It works similarly to encoding/json: define a Go struct with `gitconfig`
// tags and call [Unmarshal] or [Marshal] to convert between the struct and
// a parsed Git config ([github.com/go-git/go-git/v6/plumbing/format/config.Config]).
//
// # Struct Tags
//
// The `gitconfig` struct tag controls how fields are mapped:
//
//	type Core struct {
//	    IsBare   bool   `gitconfig:"bare"`
//	    Worktree string `gitconfig:"worktree"`
//	}
//
// Tag options are comma-separated after the key name:
//
//   - omitempty: skip zero-value fields on marshal
//   - multivalue: collect all occurrences into a slice
//   - subsection: this field maps to Git subsections
//   - -: skip this field entirely
//
// Additional tag keys provide parameterized values:
//
//   - gitconfigDefault: literal default value when key is absent
//   - gitconfigSub: fixed subsection name for single named subsections
//
// # Sections and Subsections
//
// All fields are declared at the root struct level. The `gitconfig` tag key
// is always the section name. Both section-key fields and subsection fields
// can share the same section name:
//
//	type Config struct {
//	    Core    CoreConfig               `gitconfig:"core"`
//	    Remotes map[string]*RemoteConfig `gitconfig:"remote,subsection"`
//	    GPG     GPGConfig                `gitconfig:"gpg"`
//	    GPGSSH  *SSHConfig               `gitconfig:"gpg,subsection" gitconfigSub:"ssh"`
//	}
//
// # Custom Types
//
// Types implementing [Marshaler] or [Unmarshaler] control their own
// serialization. The package also checks for [encoding.TextMarshaler]
// and [encoding.TextUnmarshaler] as fallbacks.
package config
