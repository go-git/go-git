package dotgit

import (
	"github.com/go-git/go-billy/v6/util"
)

// TestModuleRejectsInvalidNamesThatStayBelowModules pins the names the
// storage-name check refuses although they do not leave modules/: they
// get their own error rather than one that claims an escape.
func (s *SuiteDotGit) TestModuleRejectsInvalidNamesThatStayBelowModules() {
	d := New(s.EmptyFS())
	for _, n := range []string{"x\x00y", "lib/\x00", "foo/", `foo\`, "a/b/"} {
		_, err := d.Module(n)
		s.ErrorIs(err, ErrModuleNameInvalid, "name %q", n)
		s.NotErrorIs(err, ErrModuleNameEscape, "name %q", n)
	}
}

// TestModuleWin32NamesFollowProtectNTFS checks the Windows host's rule
// on any host: a name that is not a valid Win32 path is refused under
// core.protectNTFS, which is on unless the configuration turns it off,
// and accepted with it off, as Git's is_valid_win32_path accepts any
// name then.
func (s *SuiteDotGit) TestModuleWin32NamesFollowProtectNTFS() {
	names := []string{"aux", "lib/con.go", "x..", "trail ", "a/b:c", `x/a"b`}

	for _, tc := range []struct {
		name    string
		config  string
		wt      string
		refused bool
	}{
		{name: "unset", refused: true},
		{name: "on", config: "[core]\n\tprotectNTFS = true\n", refused: true},
		{name: "off", config: "[core]\n\tprotectNTFS = false\n"},
		{name: "off, git style", config: "[core]\n\tprotectNTFS = no\n"},
		{name: "unrelated", config: "[core]\n\tbare = false\n", refused: true},
		{
			name:   "off in config.worktree",
			config: "[core]\n\trepositoryformatversion = 1\n[extensions]\n\tworktreeConfig = true\n",
			wt:     "[core]\n\tprotectNTFS = false\n",
		},
		{
			name:    "config.worktree on over off",
			config:  "[core]\n\trepositoryformatversion = 1\n\tprotectNTFS = false\n[extensions]\n\tworktreeConfig = true\n",
			wt:      "[core]\n\tprotectNTFS = true\n",
			refused: true,
		},
		{
			name:   "config.worktree ignored without the extension",
			config: "[core]\n\tprotectNTFS = false\n",
			wt:     "[core]\n\tprotectNTFS = true\n",
		},
	} {
		s.Run(tc.name, func() {
			fs := s.EmptyFS()
			if tc.config != "" {
				s.Require().NoError(util.WriteFile(fs, "config", []byte(tc.config), 0o644))
			}
			if tc.wt != "" {
				s.Require().NoError(util.WriteFile(fs, "config.worktree", []byte(tc.wt), 0o644))
			}
			d := New(fs)
			for _, n := range names {
				err := d.checkModuleName(n, true)
				if tc.refused {
					s.ErrorIs(err, ErrModuleNameInvalid, "name %q", n)
				} else {
					s.NoError(err, "name %q", n)
				}
				s.NoError(d.checkModuleName(n, false), "name %q off Windows", n)
			}
		})
	}
}
