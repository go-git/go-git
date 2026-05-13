package object_test

import (
	"fmt"
	"os"
	"testing"

	_ "unsafe"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/x/plugin"
	xconfig "github.com/go-git/go-git/v6/x/plugin/config"
)

//go:linkname resetPluginEntry github.com/go-git/go-git/v6/x/plugin.resetEntry
func resetPluginEntry(name plugin.Name)

func TestMain(m *testing.M) {
	resetPluginEntry("config-loader")

	cfg := config.NewConfig()
	cfg.User.Name = "Test User"
	cfg.User.Email = "test@example.com"

	err := plugin.Register(plugin.ConfigLoader(), func() plugin.ConfigSource {
		return xconfig.NewStatic(*cfg, *cfg)
	})
	if err != nil {
		panic(fmt.Errorf("failed to register test config loader: %v", err))
	}

	os.Exit(m.Run())
}
