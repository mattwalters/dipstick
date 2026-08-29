package localstate

import (
	"fmt"
	"os"
	"path/filepath"
)

// ConfigDir returns the configuration directory for dipstick.
func ConfigDir() (string, error) {
	if env := os.Getenv("DIPSTICK_CONFIG_DIR"); env != "" {
		return filepath.Clean(env), nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "dipstick"), nil
	}
	userConfig, err := os.UserConfigDir()
	if err == nil {
		return filepath.Join(userConfig, "dipstick"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving user home directory: %w", err)
	}
	return filepath.Join(home, ".config", "dipstick"), nil
}

// DataDir returns the data and state directory for dipstick.
func DataDir() (string, error) {
	if env := os.Getenv("DIPSTICK_DATA_DIR"); env != "" {
		return filepath.Clean(env), nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "dipstick"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving user home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "dipstick"), nil
}

// CacheDir returns the cache directory for dipstick.
func CacheDir() (string, error) {
	if env := os.Getenv("DIPSTICK_CACHE_DIR"); env != "" {
		return filepath.Clean(env), nil
	}
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "dipstick"), nil
	}
	userCache, err := os.UserCacheDir()
	if err == nil {
		return filepath.Join(userCache, "dipstick"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving user home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "dipstick"), nil
}

// ProviderConfigDir returns the default configuration directory for a specific provider.
func ProviderConfigDir(provider string) (string, error) {
	switch provider {
	case "antigravity":
		if env := os.Getenv("ANTIGRAVITY_CONFIG_DIR"); env != "" {
			return filepath.Clean(env), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving user home directory: %w", err)
		}
		return filepath.Join(home, ".gemini", "antigravity-cli"), nil
	case "claude":
		if env := os.Getenv("CLAUDE_CONFIG_DIR"); env != "" {
			return filepath.Clean(env), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving user home directory: %w", err)
		}
		return filepath.Join(home, ".claude"), nil
	case "codex":
		if env := os.Getenv("CODEX_CONFIG_DIR"); env != "" {
			return filepath.Clean(env), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving user home directory: %w", err)
		}
		return filepath.Join(home, ".codex"), nil
	case "opencode":
		if env := os.Getenv("OPENCODE_CONFIG_DIR"); env != "" {
			return filepath.Clean(env), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving user home directory: %w", err)
		}
		return filepath.Join(home, ".opencode"), nil
	default:
		base, err := ConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(base, "providers", provider), nil
	}
}

// EnsureDir creates the given directory path if it does not already exist.
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("creating directory %q: %w", path, err)
	}
	return nil
}
