package config

import "testing"

func TestSplitKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key        string
		section    string
		subsection string
		variable   string
	}{
		{"core.bare", "core", "", "bare"},
		{"remote.origin.url", "remote", "origin", "url"},
		{"url.git@github.com:.insteadOf", "url", "git@github.com:", "insteadOf"},
		// A subsection may contain dots (e.g. a URL); git keeps everything
		// between the first and last dot as the subsection.
		{"url.https://github.com/.insteadOf", "url", "https://github.com/", "insteadOf"},
		// Quotes are config-file header syntax, not key syntax: in a flat key
		// they are literal subsection characters, exactly as git treats them
		// (git config 'remote."origin".url' writes [remote "\"origin\""]).
		{`remote."origin".url`, "remote", `"origin"`, "url"},
		{"core", "core", "", ""},
		{"a.b.c.d", "a", "b.c", "d"},
	}
	for _, tt := range tests {
		section, subsection, variable := SplitKey(tt.key)
		if section != tt.section || subsection != tt.subsection || variable != tt.variable {
			t.Errorf("SplitKey(%q) = (%q,%q,%q), want (%q,%q,%q)",
				tt.key, section, subsection, variable, tt.section, tt.subsection, tt.variable)
		}
	}
}

func TestIsValidKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key  string
		want bool
	}{
		{"core.bare", true},
		{"remote.origin.url", true},
		{"core.auto-crlf", true},
		{"url.https://github.com/.insteadOf", true},
		{"core", false},               // no section/variable separator
		{"core.", false},              // empty variable
		{".bare", false},              // empty section
		{"core.1abc", false},          // variable must start with a letter
		{"co re.bare", false},         // space in section
		{"core.ba re", false},         // space in variable
		{"core.bar=baz", false},       // '=' in variable
		{"url.a\nb.insteadOf", false}, // newline in subsection
	}
	for _, tt := range tests {
		if got := IsValidKey(tt.key); got != tt.want {
			t.Errorf("IsValidKey(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}

func FuzzKey(f *testing.F) {
	for _, s := range []string{"core.bare", "remote.origin.url", "a.b.c.d", "", ".", "x", "a.\n.b"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		// Neither function may panic on arbitrary input.
		_ = IsValidKey(s)
		section, subsection, variable := SplitKey(s)

		// A valid key must round-trip through SplitKey into non-empty
		// section and variable.
		if IsValidKey(s) {
			if section == "" || variable == "" {
				t.Fatalf("valid key %q split to empty section/variable (%q,%q,%q)",
					s, section, subsection, variable)
			}
		}
	})
}
