package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
)

func TestEmptyGlobal(t *testing.T) {
	t.Parallel()
	src := NewEmpty()
	s, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	cfg, err := s.Config()
	require.NoError(t, err)
	assert.Equal(t, config.NewConfig(), cfg)
}

func TestEmptySystem(t *testing.T) {
	t.Parallel()
	src := NewEmpty()
	s, err := src.Load(config.SystemScope)
	require.NoError(t, err)
	cfg, err := s.Config()
	require.NoError(t, err)
	assert.Equal(t, config.NewConfig(), cfg)
}

func TestStaticGlobalAndSystem(t *testing.T) {
	t.Parallel()
	global := config.NewConfig()
	global.User.Name = "GlobalUser"
	system := config.NewConfig()
	system.User.Name = "SystemUser"

	src := NewStatic(*global, *system)

	gs, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	got, err := gs.Config()
	require.NoError(t, err)
	assert.Equal(t, "GlobalUser", got.User.Name)

	ss, err := src.Load(config.SystemScope)
	require.NoError(t, err)
	got, err = ss.Config()
	require.NoError(t, err)
	assert.Equal(t, "SystemUser", got.User.Name)
}

func TestStaticReturnsCopies(t *testing.T) {
	t.Parallel()
	global := config.NewConfig()
	global.User.Name = "Original"
	global.Remotes["origin"] = &config.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://example.com/repo.git"},
	}

	src := NewStatic(*global, *config.NewConfig())

	gs, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	first, err := gs.Config()
	require.NoError(t, err)
	first.User.Name = "Mutated"
	first.Remotes["upstream"] = &config.RemoteConfig{Name: "upstream"}
	delete(first.Remotes, "origin")

	gs2, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	second, err := gs2.Config()
	require.NoError(t, err)
	assert.Equal(t, "Original", second.User.Name)
	assert.Contains(t, second.Remotes, "origin")
	assert.NotContains(t, second.Remotes, "upstream")
}

func TestStaticZeroValues(t *testing.T) {
	t.Parallel()
	src := NewStatic(*config.NewConfig(), *config.NewConfig())

	gs, err := src.Load(config.GlobalScope)
	require.NoError(t, err)
	got, err := gs.Config()
	require.NoError(t, err)
	assert.Equal(t, config.NewConfig(), got)

	ss, err := src.Load(config.SystemScope)
	require.NoError(t, err)
	got, err = ss.Config()
	require.NoError(t, err)
	assert.Equal(t, config.NewConfig(), got)
}

func TestStaticUnsupportedScope(t *testing.T) {
	t.Parallel()
	src := NewStatic(*config.NewConfig(), *config.NewConfig())

	_, err := src.Load(config.LocalScope)
	require.Error(t, err)
}

func TestReadOnlyStorerRejectsWrite(t *testing.T) {
	t.Parallel()
	src := NewStatic(*config.NewConfig(), *config.NewConfig())

	s, err := src.Load(config.GlobalScope)
	require.NoError(t, err)

	err = s.SetConfig(config.NewConfig())
	require.ErrorIs(t, err, ErrReadOnly)
}
