package config

import "testing"

func mustSet(t *testing.T, c *Config, key, value string) {
	t.Helper()
	if err := c.Set(key, value); err != nil {
		t.Fatalf("Set(%q): %v", key, err)
	}
}

func TestConfigSet(t *testing.T) {
	t.Parallel()
	system := New()
	mustSet(t, system, "core.editor", "vi")
	mustSet(t, system, "pack.window", "20")

	global := New()
	mustSet(t, global, "user.name", "Ayman")
	mustSet(t, global, "pack.window", "50") // overrides system

	local := New()
	mustSet(t, local, "core.bare", "true")

	// Highest precedence first: local, global, system.
	set := NewConfigSet(local, global, system)

	if got := set.String("core.editor", ""); got != "vi" { // from system
		t.Errorf("core.editor = %q, want vi", got)
	}
	if got := set.String("user.name", ""); got != "Ayman" { // from global
		t.Errorf("user.name = %q, want Ayman", got)
	}
	if b, err := set.Bool("core.bare", false); err != nil || !b { // from local
		t.Errorf("core.bare = %v, %v, want true", b, err)
	}
	if n, err := set.Int("pack.window", 0); err != nil || n != 50 { // global wins over system
		t.Errorf("pack.window = %d, %v, want 50 (global over system)", n, err)
	}
	if _, ok := set.Lookup("core.missing"); ok {
		t.Error("Lookup core.missing should be absent")
	}
	if set.Has("core.missing") {
		t.Error("Has core.missing should be false")
	}
}

func TestConfigSetGetAllAccumulates(t *testing.T) {
	t.Parallel()
	system := New()
	_ = system.Add("safe.directory", "/system")
	global := New()
	_ = global.Add("safe.directory", "/global")
	local := New()
	_ = local.Add("safe.directory", "/local")

	set := NewConfigSet(local, global, system)

	// Ascending precedence: lowest (system) first.
	got := set.GetAll("safe.directory")
	want := []string{"/system", "/global", "/local"}
	if len(got) != len(want) {
		t.Fatalf("GetAll = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("GetAll = %v, want %v", got, want)
		}
	}
}

func TestConfigSetIgnoresNilAndIsImmutable(t *testing.T) {
	t.Parallel()
	a := New()
	mustSet(t, a, "user.name", "A")

	srcs := []Getter{a, nil}
	set := NewConfigSet(srcs...)

	if set.String("user.name", "") != "A" {
		t.Error("nil source should be ignored")
	}

	// Mutating the original argument slice must not affect the set.
	b := New()
	mustSet(t, b, "user.name", "B")
	srcs[0] = b
	if set.String("user.name", "") != "A" {
		t.Error("ConfigSet must copy its source slice")
	}
	if got := set.Sources(); len(got) != 1 {
		t.Errorf("Sources() len = %d, want 1 (nil ignored)", len(got))
	}
}
