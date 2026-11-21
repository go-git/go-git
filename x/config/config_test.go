package xconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// unsetGitConfigEnv clears the GIT_CONFIG_* overrides so a test exercises pure
// path resolution, restoring any prior values when it finishes.
func unsetGitConfigEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{envGitConfigGlobal, envGitConfigSystem, envGitConfigNoSystem} {
		if v, ok := os.LookupEnv(k); ok {
			t.Cleanup(func() { _ = os.Setenv(k, v) })
			_ = os.Unsetenv(k)
		}
	}
}

func TestLoadConfigSetSkipsMissingAndErrorsOnParse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ok := filepath.Join(dir, "ok")
	writeFile(t, ok, "[user]\n\tname = Jane\n")
	missing := filepath.Join(dir, "missing")

	got, err := loadConfigSet([]string{missing, ok})
	if err != nil {
		t.Fatalf("loadConfigSet: %v", err)
	}
	if n := len(got.Sources()); n != 1 {
		t.Fatalf("loaded %d sources, want 1 (missing skipped)", n)
	}
	if got.Get("user.name") != "Jane" {
		t.Errorf("user.name = %q", got.Get("user.name"))
	}

	bad := filepath.Join(dir, "bad")
	writeFile(t, bad, "this is not = valid [ git config")
	if _, err := loadConfigSet([]string{bad}); err == nil {
		t.Error("loadConfigSet should error on a malformed file")
	}
}

func TestGlobalConfigLayersHomeAndXDG(t *testing.T) {
	unsetGitConfigEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(envXDGConfigHome, filepath.Join(home, "xdg"))

	writeFile(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = FromHome\n")
	writeFile(t, filepath.Join(home, "xdg", "git", "config"), "[user]\n\tname = FromXDG\n\temail = xdg@example.com\n")

	set, err := GlobalConfig()
	if err != nil {
		t.Fatalf("GlobalConfig: %v", err)
	}
	if n := len(set.Sources()); n != 2 {
		t.Fatalf("GlobalConfig sources = %d, want 2 (home and XDG layered)", n)
	}
	if got := set.String("user.name", ""); got != "FromHome" {
		t.Errorf("user.name = %q, want FromHome (~/.gitconfig wins)", got)
	}
	if got := set.String("user.email", ""); got != "xdg@example.com" {
		t.Errorf("user.email = %q, want xdg value (layered, not mutually exclusive)", got)
	}
}

func TestGlobalConfigEnvOverride(t *testing.T) {
	unsetGitConfigEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(envXDGConfigHome, filepath.Join(home, "xdg"))
	writeFile(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = FromHome\n")
	writeFile(t, filepath.Join(home, "xdg", "git", "config"), "[user]\n\temail = xdg@example.com\n")

	alt := filepath.Join(home, "alt", "gitconfig")
	writeFile(t, alt, "[user]\n\tname = FromAlt\n")

	t.Run("non-empty path replaces home and XDG", func(t *testing.T) {
		t.Setenv(envGitConfigGlobal, alt)
		set, err := GlobalConfig()
		if err != nil {
			t.Fatalf("GlobalConfig: %v", err)
		}
		if got := set.String("user.name", ""); got != "FromAlt" {
			t.Errorf("user.name = %q, want FromAlt", got)
		}
		if set.Has("user.email") {
			t.Error("user.email should be absent: GIT_CONFIG_GLOBAL replaces XDG too")
		}
	})

	t.Run("empty value disables global", func(t *testing.T) {
		t.Setenv(envGitConfigGlobal, "")
		set, err := GlobalConfig()
		if err != nil {
			t.Fatalf("GlobalConfig: %v", err)
		}
		if n := len(set.Sources()); n != 0 {
			t.Errorf("sources = %d, want 0 (global disabled)", n)
		}
		if set.Has("user.name") {
			t.Error("global config should be disabled")
		}
	})
}

func TestSystemConfigEnv(t *testing.T) {
	unsetGitConfigEnv(t)
	home := t.TempDir()
	sys := filepath.Join(home, "sys", "gitconfig")
	writeFile(t, sys, "[core]\n\tsomething = sys\n")

	t.Run("GIT_CONFIG_SYSTEM points at a file", func(t *testing.T) {
		t.Setenv(envGitConfigSystem, sys)
		set, err := SystemConfig()
		if err != nil {
			t.Fatalf("SystemConfig: %v", err)
		}
		if got := set.String("core.something", ""); got != "sys" {
			t.Errorf("core.something = %q, want sys", got)
		}
	})

	t.Run("GIT_CONFIG_SYSTEM empty disables system", func(t *testing.T) {
		t.Setenv(envGitConfigSystem, "")
		set, err := SystemConfig()
		if err != nil {
			t.Fatalf("SystemConfig: %v", err)
		}
		if n := len(set.Sources()); n != 0 {
			t.Errorf("sources = %d, want 0 (system disabled)", n)
		}
	})

	t.Run("GIT_CONFIG_NOSYSTEM skips system even with GIT_CONFIG_SYSTEM set", func(t *testing.T) {
		t.Setenv(envGitConfigSystem, sys)
		t.Setenv(envGitConfigNoSystem, "1")
		set, err := SystemConfig()
		if err != nil {
			t.Fatalf("SystemConfig: %v", err)
		}
		if set.Has("core.something") {
			t.Error("GIT_CONFIG_NOSYSTEM should skip system config")
		}
	})
}

func TestIsNoSystem(t *testing.T) { //nolint:paralleltest // mutates process environment
	cases := []struct {
		val  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"FALSE", false},
		{"no", false},
		{"off", false},
		{"1", true},
		{"true", true},
		{"yes", true},
		{"anything", true},
	}
	for _, tc := range cases {
		t.Setenv(envGitConfigNoSystem, tc.val)
		if got := isNoSystem(); got != tc.want {
			t.Errorf("isNoSystem(%q) = %v, want %v", tc.val, got, tc.want)
		}
	}
}

func TestSystemConfigSkipsMissing(t *testing.T) { //nolint:paralleltest // mutates process environment
	unsetGitConfigEnv(t)
	// The platform's default system file may or may not exist; either way this
	// must not error.
	if _, err := SystemConfig(); err != nil {
		t.Fatalf("SystemConfig: %v", err)
	}
}
