package config

import (
	"errors"
	"testing"
)

func TestConfigAccessors(t *testing.T) {
	t.Parallel()
	c := New()
	if err := c.Set("core.bare", "true"); err != nil {
		t.Fatalf("Set core.bare: %v", err)
	}
	if err := c.Set("remote.origin.url", "git@github.com:go-git/go-git.git"); err != nil {
		t.Fatalf("Set url: %v", err)
	}
	if err := c.Add("remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		t.Fatalf("Add fetch: %v", err)
	}
	if err := c.Add("remote.origin.fetch", "+refs/tags/*:refs/tags/*"); err != nil {
		t.Fatalf("Add fetch: %v", err)
	}
	_ = c.Set("pack.window", "16")

	if got := c.Get("remote.origin.url"); got != "git@github.com:go-git/go-git.git" {
		t.Errorf("Get url = %q", got)
	}
	if b, err := c.Bool("core.bare", false); err != nil || !b {
		t.Errorf("Bool core.bare = %v, %v", b, err)
	}
	if b, _ := c.Bool("core.missing", true); !b {
		t.Error("missing bool should return default")
	}
	if all := c.GetAll("remote.origin.fetch"); len(all) != 2 {
		t.Errorf("GetAll fetch len = %d, want 2", len(all))
	}
	if n, err := c.Int("pack.window", 10); err != nil || n != 16 {
		t.Errorf("Int pack.window = %d, %v", n, err)
	}
	if n, err := c.Int("pack.depth", 50); err != nil || n != 50 {
		t.Errorf("Int default = %d, %v", n, err)
	}
	if s := c.String("user.name", "nobody"); s != "nobody" {
		t.Errorf("String default = %q", s)
	}

	if _, ok := c.Lookup("core.bare"); !ok {
		t.Error("Lookup core.bare should be present")
	}
	if _, ok := c.Lookup("core.nope"); ok {
		t.Error("Lookup core.nope should be absent")
	}

	_ = c.Set("remote.origin.fetch", "single")
	if all := c.GetAll("remote.origin.fetch"); len(all) != 1 {
		t.Errorf("Set should collapse multivalue, got %d", len(all))
	}

	if !c.Unset("core.bare") {
		t.Error("Unset core.bare should report removal")
	}
	if c.Has("core.bare") {
		t.Error("core.bare should be unset")
	}
	if c.Unset("core.bare") {
		t.Error("Unset of absent key should report false")
	}
}

func TestConfigAccessorMergesRepeatedSections(t *testing.T) {
	t.Parallel()
	c := New()
	c.Section("core").AddOption("bare", "false")
	c.Sections = append(c.Sections, &Section{
		Name:    "core",
		Options: Options{&Option{Key: "bare", Value: "true"}},
	})

	if got := c.Get("core.bare"); got != "true" {
		t.Errorf("Get core.bare across blocks = %q, want last value true", got)
	}
	if all := c.GetAll("core.bare"); len(all) != 2 {
		t.Errorf("GetAll core.bare = %v, want 2 values", all)
	}
	if b, err := c.Bool("core.bare", false); err != nil || !b {
		t.Errorf("Bool merged = %v, %v", b, err)
	}
}

func TestConfigAccessorInvalidKey(t *testing.T) {
	t.Parallel()
	c := New()
	if err := c.Set("core", "x"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Set invalid key err = %v, want ErrInvalidKey", err)
	}
	if err := c.Add("core.1bad", "x"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Add invalid key err = %v, want ErrInvalidKey", err)
	}
}

func TestConfigBoolInvalidValue(t *testing.T) {
	t.Parallel()
	c := New()
	_ = c.Set("core.bare", "notabool")
	b, err := c.Bool("core.bare", true)
	if !errors.Is(err, ErrInvalidBool) {
		t.Errorf("Bool err = %v, want ErrInvalidBool", err)
	}
	if !b {
		t.Error("Bool should return the default on parse error")
	}
}
