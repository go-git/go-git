package config

import "errors"

var (
	ErrModuleEmptyURL  = errors.New("module config: empty URL")
	ErrModuleEmptyPath = errors.New("module config: empty path")
)

const DefaultModuleBranch = "master"

// Modules defines the submodules properties
type Modules map[string]*Module

// Module defines a submodule
// https://www.kernel.org/pub/software/scm/git/docs/gitmodules.html
type Module struct {
	// Path defines the path, relative to the top-level directory of the Git
	// working tree,
	Path string
	// URL defines a URL from which the submodule repository can be cloned.
	URL string
	// Branch is a remote branch name for tracking updates in the upstream
	// submodule.
	Branch string
}

// Validate validate the fields and set the default values
func (m *Module) Validate() error {
	if m.Path == "" {
		return ErrModuleEmptyPath
	}

	if m.URL == "" {
		return ErrModuleEmptyURL
	}

	if m.Branch == "" {
		m.Branch = DefaultModuleBranch
	}

	return nil
}
