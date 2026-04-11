package filesystem

import (
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/go-git/go-git/v6/config"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// ConfigStorage implements config.ConfigStorer for filesystem storage.
type ConfigStorage struct {
	dir          *dotgit.DotGit
	objectFormat formatcfg.ObjectFormat
}

// Config returns the repository configuration.
//
// When the worktreeConfig extension is active and a config.worktree file
// exists, the returned config would be the worktree config overlayed over
// the commonDir config.
func (c *ConfigStorage) Config() (conf *config.Config, err error) {
	f, err := c.dir.Config()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := config.NewConfig()

			if c.objectFormat != formatcfg.SHA1 {
				cfg.Core.RepositoryFormatVersion = formatcfg.Version1
				cfg.Extensions.ObjectFormat = c.objectFormat
			}

			return cfg, nil
		}

		return nil, err
	}
	defer ioutil.CheckClose(f, &err)

	cfg, err := config.ReadConfig(f)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	if !cfg.Extensions.WorktreeConfig {
		return cfg, nil
	}

	wf, err := c.dir.ConfigWorktree()
	if err != nil {
		// If a worktree config doesn't exist we can short-circuit
		// returning the local config.
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("get worktree config: %w", err)
	}
	defer ioutil.CheckClose(wf, &err)

	wcfg, err := config.ReadConfig(wf)
	if err != nil {
		return nil, fmt.Errorf("read worktree config: %w", err)
	}

	merged := config.Merge(cfg, wcfg)

	return &merged, nil
}

// SetConfig saves the repository configuration.
//
// When the worktreeConfig extension is active and a config.worktree file
// already exists, only the delta — options whose values are absent from or
// differ from the base config — is written to config.worktree. The base
// config is left untouched in that case. This mirrors the behaviour of
// `git config --worktree`: worktree-specific overrides live in
// config.worktree while shared settings remain in the common config.
func (c *ConfigStorage) SetConfig(cfg *config.Config) (err error) {
	if err = cfg.Validate(); err != nil {
		return err
	}

	if cfg.Extensions.WorktreeConfig {
		wf, wtErr := c.dir.ConfigWorktree()
		if wtErr == nil {
			_ = wf.Close()
			return c.setWorktreeConfig(cfg)
		}
		if !errors.Is(wtErr, os.ErrNotExist) {
			return fmt.Errorf("check config.worktree: %w", wtErr)
		}
	}

	f, err := c.dir.ConfigWriter()
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(f, &err)

	b, err := cfg.Marshal()
	if err != nil {
		return err
	}

	_, err = f.Write(b)
	return err
}

// setWorktreeConfig writes only the delta between the current base config and
// cfg into config.worktree, leaving the base config file untouched.
func (c *ConfigStorage) setWorktreeConfig(cfg *config.Config) error {
	baseCfg, err := c.readBaseConfig()
	if err != nil {
		return err
	}

	// Ensure Raw reflects struct state.
	if _, err := baseCfg.Marshal(); err != nil {
		return fmt.Errorf("marshal base config: %w", err)
	}
	if _, err := cfg.Marshal(); err != nil {
		return fmt.Errorf("marshal updated config: %w", err)
	}

	f, err := c.dir.ConfigWorktreeWriter()
	if err != nil {
		return fmt.Errorf("open worktree config writer: %w", err)
	}
	defer ioutil.CheckClose(f, &err)

	delta := rawDiff(baseCfg.Raw, cfg.Raw)

	if err := formatcfg.NewEncoder(f).Encode(delta); err != nil {
		return fmt.Errorf("encode worktree config: %w", err)
	}

	return nil
}

func (c *ConfigStorage) readBaseConfig() (*config.Config, error) {
	f, err := c.dir.Config()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config.NewConfig(), nil
		}
		return nil, fmt.Errorf("read base config: %w", err)
	}
	defer ioutil.CheckClose(f, &err)

	cfg, err := config.ReadConfig(f)
	if err != nil {
		return nil, fmt.Errorf("parse base config: %w", err)
	}

	return cfg, nil
}

// rawDiff returns a config containing only sections/subsections/options
// whose effective values differ from base.
func rawDiff(base, updated *formatcfg.Config) *formatcfg.Config {
	delta := formatcfg.New()

	for _, us := range updated.Sections {
		var baseSec *formatcfg.Section
		if base.HasSection(us.Name) {
			baseSec = base.Section(us.Name)
		}

		diffOpts := diffOptions(
			baseOptions(baseSec),
			us.Options,
		)

		diffSubs := diffSubsections(baseSec, us.Subsections)

		if len(diffOpts) == 0 && len(diffSubs) == 0 {
			continue
		}

		ds := delta.Section(us.Name)
		ds.Options = diffOpts
		ds.Subsections = diffSubs
	}

	return delta
}

func diffSubsections(
	baseSec *formatcfg.Section,
	updated formatcfg.Subsections,
) formatcfg.Subsections {
	var out formatcfg.Subsections

	for _, uss := range updated {
		var baseSub *formatcfg.Subsection
		if baseSec != nil && baseSec.HasSubsection(uss.Name) {
			baseSub = baseSec.Subsection(uss.Name)
		}

		var subOpts formatcfg.Options
		if baseSub == nil {
			// This subsection is new.
			subOpts = uss.Options
		} else {
			subOpts = diffOptions(baseSub.Options, uss.Options)
		}

		if len(subOpts) == 0 {
			continue
		}

		out = append(out, &formatcfg.Subsection{
			Name:    uss.Name,
			Options: subOpts,
		})
	}

	return out
}

// diffOptions compares last-writer-wins keys between base and updated.
// If the full value set for a key differs, the option is included.
func diffOptions(
	baseOpts, updated formatcfg.Options,
) formatcfg.Options {
	if len(updated) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(updated))
	var out formatcfg.Options

	// Reverse iteration to preserve last-writer-wins semantics.
	for i := len(updated) - 1; i >= 0; i-- {
		opt := updated[i]

		if _, ok := seen[opt.Key]; ok {
			continue
		}
		seen[opt.Key] = struct{}{}

		baseVals := baseOpts.GetAll(opt.Key)
		updVals := updated.GetAll(opt.Key)

		if !slices.Equal(baseVals, updVals) {
			// Prepend to restore original order.
			out = append(formatcfg.Options{opt}, out...)
		}
	}

	return out
}

func baseOptions(sec *formatcfg.Section) formatcfg.Options {
	if sec == nil {
		return nil
	}
	return sec.Options
}
