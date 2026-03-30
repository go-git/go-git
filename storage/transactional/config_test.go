package transactional

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestConfigSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ConfigSuite))
}

type ConfigSuite struct {
	suite.Suite
}

func (s *ConfigSuite) TestSetConfigBase() {
	cfg := config.NewConfig()
	cfg.Core.Worktree = "foo"

	base := memory.NewStorage()
	err := base.SetConfig(cfg)
	s.NoError(err)

	temporal := memory.NewStorage()
	cs := NewConfigStorage(base, temporal)

	cfg, err = cs.Config()
	s.NoError(err)
	s.Equal("foo", cfg.Core.Worktree)
}

func (s *ConfigSuite) TestSetConfigTemporal() {
	cfg := config.NewConfig()
	cfg.Core.Worktree = "foo"

	base := memory.NewStorage()
	err := base.SetConfig(cfg)
	s.NoError(err)

	temporal := memory.NewStorage()

	cfg = config.NewConfig()
	cfg.Core.Worktree = "bar"

	cs := NewConfigStorage(base, temporal)
	err = cs.SetConfig(cfg)
	s.NoError(err)

	baseCfg, err := base.Config()
	s.NoError(err)
	s.Equal("foo", baseCfg.Core.Worktree)

	temporalCfg, err := temporal.Config()
	s.NoError(err)
	s.Equal("bar", temporalCfg.Core.Worktree)

	cfg, err = cs.Config()
	s.NoError(err)
	s.Equal("bar", cfg.Core.Worktree)
}

func (s *ConfigSuite) TestCommit() {
	cfg := config.NewConfig()
	cfg.Core.Worktree = "foo"

	base := memory.NewStorage()
	err := base.SetConfig(cfg)
	s.NoError(err)

	temporal := memory.NewStorage()

	cfg = config.NewConfig()
	cfg.Core.Worktree = "bar"

	cs := NewConfigStorage(base, temporal)
	err = cs.SetConfig(cfg)
	s.NoError(err)

	err = cs.Commit()
	s.NoError(err)

	baseCfg, err := base.Config()
	s.NoError(err)
	s.Equal("bar", baseCfg.Core.Worktree)
}
