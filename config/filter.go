package config

import (
	"fmt"
	"strconv"

	format "github.com/go-git/go-git/v5/plumbing/format/config"
)

type Filter struct {
	// Name of the filter.
	Name string
	// The command to run for a file being staged.
	Clean string
	// The command to run for a file being checked out.
	Smudge string
	// The command to run for a persistent filter process.
	Process string
	// If the filter process is required for all operations.
	Required bool

	raw *format.Subsection
}

func (f *Filter) marshal() *format.Subsection {
	if f.raw == nil {
		f.raw = &format.Subsection{}
	}

	f.raw.Name = f.Name

	if f.Clean == "" {
		f.raw.RemoveOption(remoteSection)
	} else {
		f.raw.SetOption(cleanKey, f.Clean)
	}

	if f.Smudge == "" {
		f.raw.RemoveOption(remoteSection)
	} else {
		f.raw.SetOption(smudgeKey, f.Smudge)
	}

	if f.Process == "" {
		f.raw.RemoveOption(remoteSection)
	} else {
		f.raw.SetOption(processKey, f.Process)
	}

	if !f.Required {
		f.raw.RemoveOption(remoteSection)
	} else {
		f.raw.SetOption(requiredKey, fmt.Sprintf("%t", f.Required))
	}

	return f.raw
}

func (f *Filter) unmarshal(s *format.Subsection) error {
	f.raw = s

	required, err := strconv.ParseBool(f.raw.Options.Get(requiredKey))
	if err != nil {
		return err
	}

	f.Name = f.raw.Name
	f.Clean = f.raw.Options.Get(cleanKey)
	f.Smudge = f.raw.Options.Get(smudgeKey)
	f.Process = f.raw.Options.Get(processKey)
	f.Required = required

	return nil
}
