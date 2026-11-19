package config

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/go-git/go-git/v6/config"
)

func TestAutoAttributesFileOfTheSystem(t *testing.T) {
	t.Setenv("PROGRAMFILES", "pf")

	want := "/etc/gitattributes"
	if runtime.GOOS == "windows" {
		want = filepath.Join("pf", "Git", "etc", "gitattributes")
	}
	fs, name, err := NewAuto().AttributesFile(config.SystemScope, "ignored")
	assert.NoError(t, err)
	assert.NotNil(t, fs)
	assert.Equal(t, want, name)
}

func TestAutoAttributesFileOfTheUser(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "u")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	for _, tc := range []struct {
		name, xdg, configured, want string
	}{
		{name: "default", want: filepath.Join(home, ".config", "git", "attributes")},
		{name: "XDG_CONFIG_HOME", xdg: "xdg", want: filepath.Join("xdg", "git", "attributes")},
		{name: "configured", xdg: "xdg", configured: "/abs/attributes", want: "/abs/attributes"},
		{name: "home", configured: "~/attributes", want: filepath.Join(home, "attributes")},
		{name: "relative", configured: "rel", want: "rel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", tc.xdg)

			_, name, err := NewAuto().AttributesFile(config.GlobalScope, tc.configured)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, name)
		})
	}
}

func TestAutoAttributesFileOfAnotherScope(t *testing.T) {
	t.Parallel()

	fs, name, err := NewAuto().AttributesFile(config.LocalScope, "")
	assert.NoError(t, err)
	assert.Nil(t, fs)
	assert.Empty(t, name)
}

// Git dies on a name it cannot expand, so a source reports it rather than
// leave no file.
func TestAutoAttributesFileThatCannotBeExpanded(t *testing.T) {
	for _, tc := range []struct {
		name, configured string
		noHome           bool
	}{
		{name: "user that does not exist", configured: "~no-such-user-here/attributes"},
		{name: "home that is unset", configured: "~/attributes", noHome: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.noHome {
				t.Setenv("HOME", "")
				t.Setenv("USERPROFILE", "")
			}

			fs, name, err := NewAuto().AttributesFile(config.GlobalScope, tc.configured)
			assert.Error(t, err)
			assert.Nil(t, fs)
			assert.Empty(t, name)
		})
	}
}
