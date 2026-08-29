package localstate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mattwalters/dipstick/internal/localstate"
)

func TestConfigDir_EnvOverride(t *testing.T) {
	custom := "/tmp/custom-dipstick-config"
	t.Setenv("DIPSTICK_CONFIG_DIR", custom)

	dir, err := localstate.ConfigDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != custom {
		t.Errorf("expected %s, got %s", custom, dir)
	}
}

func TestConfigDir_XDGOverride(t *testing.T) {
	t.Setenv("DIPSTICK_CONFIG_DIR", "")
	custom := "/tmp/custom-xdg"
	t.Setenv("XDG_CONFIG_HOME", custom)

	dir, err := localstate.ConfigDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join(custom, "dipstick")
	if dir != expected {
		t.Errorf("expected %s, got %s", expected, dir)
	}
}

func TestDataDir_EnvOverride(t *testing.T) {
	custom := "/tmp/custom-dipstick-data"
	t.Setenv("DIPSTICK_DATA_DIR", custom)

	dir, err := localstate.DataDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != custom {
		t.Errorf("expected %s, got %s", custom, dir)
	}
}

func TestDataDir_XDGOverride(t *testing.T) {
	t.Setenv("DIPSTICK_DATA_DIR", "")
	custom := "/tmp/custom-xdg-data"
	t.Setenv("XDG_DATA_HOME", custom)

	dir, err := localstate.DataDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join(custom, "dipstick")
	if dir != expected {
		t.Errorf("expected %s, got %s", expected, dir)
	}
}

func TestCacheDir_EnvOverride(t *testing.T) {
	custom := "/tmp/custom-dipstick-cache"
	t.Setenv("DIPSTICK_CACHE_DIR", custom)

	dir, err := localstate.CacheDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != custom {
		t.Errorf("expected %s, got %s", custom, dir)
	}
}

func TestCacheDir_XDGOverride(t *testing.T) {
	t.Setenv("DIPSTICK_CACHE_DIR", "")
	custom := "/tmp/custom-xdg-cache"
	t.Setenv("XDG_CACHE_HOME", custom)

	dir, err := localstate.CacheDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join(custom, "dipstick")
	if dir != expected {
		t.Errorf("expected %s, got %s", expected, dir)
	}
}

func TestProviderConfigDir(t *testing.T) {
	providers := []string{"antigravity", "claude", "codex", "opencode", "custom"}
	for _, p := range providers {
		dir, err := localstate.ProviderConfigDir(p)
		if err != nil {
			t.Errorf("unexpected error for %s: %v", p, err)
		}
		if dir == "" {
			t.Errorf("expected non-empty path for provider %s", p)
		}
	}
}

func TestProviderConfigDir_EnvOverrides(t *testing.T) {
	t.Setenv("ANTIGRAVITY_CONFIG_DIR", "/tmp/antigravity")
	dir, err := localstate.ProviderConfigDir("antigravity")
	if err != nil || dir != "/tmp/antigravity" {
		t.Errorf("expected /tmp/antigravity, got %s (err: %v)", dir, err)
	}

	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/claude")
	dir, err = localstate.ProviderConfigDir("claude")
	if err != nil || dir != "/tmp/claude" {
		t.Errorf("expected /tmp/claude, got %s (err: %v)", dir, err)
	}

	t.Setenv("CODEX_CONFIG_DIR", "/tmp/codex")
	dir, err = localstate.ProviderConfigDir("codex")
	if err != nil || dir != "/tmp/codex" {
		t.Errorf("expected /tmp/codex, got %s (err: %v)", dir, err)
	}

	t.Setenv("OPENCODE_CONFIG_DIR", "/tmp/opencode")
	dir, err = localstate.ProviderConfigDir("opencode")
	if err != nil || dir != "/tmp/opencode" {
		t.Errorf("expected /tmp/opencode, got %s (err: %v)", dir, err)
	}
}

func TestEnsureDir(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "nested", "dir")
	if err := localstate.EnsureDir(target); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("failed to stat created dir: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected %s to be a directory", target)
	}
}
