package config

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// An Encoder writes config files to an output stream.
type Encoder struct {
	w io.Writer
}

var (
	// ErrInvalidSectionName is returned when a section name is not one Git accepts.
	ErrInvalidSectionName = errors.New("invalid section name")
	// ErrInvalidSubsectionName is returned when a subsection name is not one Git accepts.
	ErrInvalidSubsectionName = errors.New("invalid subsection name")
	// ErrInvalidKey is returned when an option key is not one Git accepts.
	ErrInvalidKey = errors.New("invalid key")
)

var (
	subsectionReplacer = strings.NewReplacer(`"`, `\"`, `\`, `\\`)
	valueReplacer      = strings.NewReplacer(`"`, `\"`, `\`, `\\`, "\n", `\n`, "\t", `\t`, "\b", `\b`)
)

// NewEncoder returns a new encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w}
}

// Encode writes the config in git config format to the stream of the encoder.
func (e *Encoder) Encode(cfg *Config) error {
	if err := validateNames(cfg); err != nil {
		return err
	}

	for _, s := range cfg.Sections {
		if err := e.encodeSection(s); err != nil {
			return err
		}
	}

	return nil
}

func (e *Encoder) encodeSection(s *Section) error {
	if len(s.Options) > 0 {
		if err := e.printf("[%s]\n", s.Name); err != nil {
			return err
		}

		if err := e.encodeOptions(s.Options); err != nil {
			return err
		}
	}

	for _, ss := range s.Subsections {
		if err := e.encodeSubsection(s.Name, ss); err != nil {
			return err
		}
	}

	return nil
}

func (e *Encoder) encodeSubsection(sectionName string, s *Subsection) error {
	if err := e.printf("[%s \"%s\"]\n", sectionName, subsectionReplacer.Replace(s.Name)); err != nil {
		return err
	}

	return e.encodeOptions(s.Options)
}

func (e *Encoder) encodeOptions(opts Options) error {
	for _, o := range opts {
		if o.NoValue && o.Value == "" {
			if err := e.printf("\t%s\n", o.Key); err != nil {
				return err
			}
			continue
		}

		var value string
		if strings.ContainsAny(o.Value, "#;\"\t\n\r\\") || strings.HasPrefix(o.Value, " ") || strings.HasSuffix(o.Value, " ") {
			value = `"` + valueReplacer.Replace(o.Value) + `"`
		} else {
			value = o.Value
		}

		if err := e.printf("\t%s = %s\n", o.Key, value); err != nil {
			return err
		}
	}

	return nil
}

func (e *Encoder) printf(msg string, args ...any) error {
	_, err := fmt.Fprintf(e.w, msg, args...)
	return err
}

// validateNames refuses the section, subsection and key names that Git
// itself cannot write or read back, so that a name can neither break the
// syntax of the file nor smuggle in extra sections or options. The rules
// follow git_config_parse_key and store_create_section in Git's config.c:
//
//   - keys are non-empty, start with an ASCII letter and otherwise hold
//     only ASCII letters, digits and "-";
//   - sections are non-empty and hold only ASCII letters, digits and "-".
//     Git splits a "." into section and subsection, so a dotted section
//     cannot be written as one;
//   - subsections hold anything but a newline or a NUL. Git keeps the NUL
//     and then treats the name as a C string, which truncates it.
func validateNames(cfg *Config) error {
	for _, s := range cfg.Sections {
		if !isValidSectionName(s.Name) {
			return fmt.Errorf("%w: %q", ErrInvalidSectionName, s.Name)
		}

		if err := validateKeys(s.Options); err != nil {
			return err
		}

		for _, ss := range s.Subsections {
			if strings.ContainsAny(ss.Name, "\n\x00") {
				return fmt.Errorf("%w: %q", ErrInvalidSubsectionName, ss.Name)
			}

			if err := validateKeys(ss.Options); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateKeys(opts Options) error {
	for _, o := range opts {
		if !isValidKey(o.Key) {
			return fmt.Errorf("%w: %q", ErrInvalidKey, o.Key)
		}
	}

	return nil
}

func isValidSectionName(name string) bool {
	if name == "" {
		return false
	}

	for i := 0; i < len(name); i++ {
		if c := name[i]; !isAlnum(c) && c != '-' {
			return false
		}
	}

	return true
}

func isValidKey(key string) bool {
	if key == "" || !isAlpha(key[0]) {
		return false
	}

	for i := 1; i < len(key); i++ {
		if c := key[i]; !isAlnum(c) && c != '-' {
			return false
		}
	}

	return true
}

func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func isAlnum(c byte) bool { return isAlpha(c) || c >= '0' && c <= '9' }
