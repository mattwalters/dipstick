package localstate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Resolver provides injectable path and credential resolution for vendor state.
type Resolver struct {
	lookupEnv     func(string) (string, bool)
	userHomeDir   func() (string, error)
	userConfigDir func() (string, error)
	userCacheDir  func() (string, error)
	goos          string
	keychain      KeychainReader
	now           func() time.Time
}

// Option configures a Resolver.
type Option func(*Resolver)

// WithLookupEnv sets the environment variable lookup function.
func WithLookupEnv(fn func(string) (string, bool)) Option {
	return func(r *Resolver) {
		r.lookupEnv = fn
	}
}

// WithEnvMap sets environment variables from a map.
func WithEnvMap(env map[string]string) Option {
	return func(r *Resolver) {
		r.lookupEnv = func(key string) (string, bool) {
			val, ok := env[key]
			return val, ok
		}
	}
}

// WithHomeDir sets a static user home directory.
func WithHomeDir(dir string) Option {
	return func(r *Resolver) {
		r.userHomeDir = func() (string, error) {
			return dir, nil
		}
	}
}

// WithUserHomeDirFunc sets the user home directory resolver func.
func WithUserHomeDirFunc(fn func() (string, error)) Option {
	return func(r *Resolver) {
		r.userHomeDir = fn
	}
}

// WithUserConfigDirFunc sets the user config directory resolver func.
func WithUserConfigDirFunc(fn func() (string, error)) Option {
	return func(r *Resolver) {
		r.userConfigDir = fn
	}
}

// WithUserCacheDirFunc sets the user cache directory resolver func.
func WithUserCacheDirFunc(fn func() (string, error)) Option {
	return func(r *Resolver) {
		r.userCacheDir = fn
	}
}

// WithGOOS sets the target operating system (e.g. "darwin", "linux").
func WithGOOS(goos string) Option {
	return func(r *Resolver) {
		r.goos = goos
	}
}

// WithKeychain sets the KeychainReader.
func WithKeychain(kr KeychainReader) Option {
	return func(r *Resolver) {
		r.keychain = kr
	}
}

// WithNow sets the time provider function.
func WithNow(fn func() time.Time) Option {
	return func(r *Resolver) {
		r.now = fn
	}
}

// New creates a new Resolver with default settings.
func New(opts ...Option) *Resolver {
	r := &Resolver{
		lookupEnv:     os.LookupEnv,
		userHomeDir:   os.UserHomeDir,
		userConfigDir: os.UserConfigDir,
		userCacheDir:  os.UserCacheDir,
		goos:          runtime.GOOS,
		keychain:      NewPlatformKeychainReader(),
		now:           time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	return r
}

func (r *Resolver) getenv(key string) string {
	if r.lookupEnv != nil {
		if val, ok := r.lookupEnv(key); ok {
			return val
		}
		return ""
	}
	return os.Getenv(key)
}

// ConfigDir returns the configuration directory for dipstick.
func (r *Resolver) ConfigDir() (string, error) {
	if env := r.getenv("DIPSTICK_CONFIG_DIR"); env != "" {
		return filepath.Clean(env), nil
	}
	if xdg := r.getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "dipstick"), nil
	}
	if r.userConfigDir != nil {
		userConfig, err := r.userConfigDir()
		if err == nil && userConfig != "" {
			return filepath.Join(userConfig, "dipstick"), nil
		}
	}
	home, err := r.userHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving user home directory: %w", err)
	}
	return filepath.Join(home, ".config", "dipstick"), nil
}

// DataDir returns the data and state directory for dipstick.
func (r *Resolver) DataDir() (string, error) {
	if env := r.getenv("DIPSTICK_DATA_DIR"); env != "" {
		return filepath.Clean(env), nil
	}
	if xdg := r.getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "dipstick"), nil
	}
	home, err := r.userHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving user home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "dipstick"), nil
}

// CacheDir returns the cache directory for dipstick.
func (r *Resolver) CacheDir() (string, error) {
	if env := r.getenv("DIPSTICK_CACHE_DIR"); env != "" {
		return filepath.Clean(env), nil
	}
	if xdg := r.getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "dipstick"), nil
	}
	if r.userCacheDir != nil {
		userCache, err := r.userCacheDir()
		if err == nil && userCache != "" {
			return filepath.Join(userCache, "dipstick"), nil
		}
	}
	home, err := r.userHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving user home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "dipstick"), nil
}

// ClaudePaths contains resolved paths for Claude Code configuration and data.
type ClaudePaths struct {
	ConfigDir       string
	SettingsFile    string
	ProjectsDir     string
	SessionsDir     string
	HistoryFile     string
	CredentialsFile string
}

// ClaudePaths resolves all filesystem paths for Claude Code.
func (r *Resolver) ClaudePaths() (*ClaudePaths, error) {
	var configDir string
	if env := r.getenv("CLAUDE_CONFIG_DIR"); env != "" {
		configDir = filepath.Clean(env)
	} else {
		home, err := r.userHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolving claude home directory: %w", err)
		}
		configDir = filepath.Join(home, ".claude")
	}

	return &ClaudePaths{
		ConfigDir:       configDir,
		SettingsFile:    filepath.Join(configDir, "settings.json"),
		ProjectsDir:     filepath.Join(configDir, "projects"),
		SessionsDir:     filepath.Join(configDir, "sessions"),
		HistoryFile:     filepath.Join(configDir, "history.jsonl"),
		CredentialsFile: filepath.Join(configDir, ".credentials.json"),
	}, nil
}

// CodexPaths contains resolved paths for Codex CLI configuration and data.
type CodexPaths struct {
	HomeDir           string
	AuthFile          string
	ConfigFile        string
	SessionsDir       string
	HistoryFile       string
	SQLiteLogsPattern string
}

// CodexPaths resolves all filesystem paths for Codex CLI.
func (r *Resolver) CodexPaths() (*CodexPaths, error) {
	var homeDir string
	if env := r.getenv("CODEX_HOME"); env != "" {
		homeDir = filepath.Clean(env)
	} else if env := r.getenv("CODEX_CONFIG_DIR"); env != "" {
		homeDir = filepath.Clean(env)
	} else {
		home, err := r.userHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolving codex home directory: %w", err)
		}
		homeDir = filepath.Join(home, ".codex")
	}

	return &CodexPaths{
		HomeDir:           homeDir,
		AuthFile:          filepath.Join(homeDir, "auth.json"),
		ConfigFile:        filepath.Join(homeDir, "config.toml"),
		SessionsDir:       filepath.Join(homeDir, "sessions"),
		HistoryFile:       filepath.Join(homeDir, "history.jsonl"),
		SQLiteLogsPattern: filepath.Join(homeDir, "logs_*.sqlite"),
	}, nil
}

// AntigravityPaths contains resolved paths for Antigravity CLI and desktop app.
type AntigravityPaths struct {
	CLIConfigDir      string
	CLIOAuthTokenFile string
	CLISettingsFile   string
	DesktopConfigDir  string
	DesktopDataDir    string
	DesktopLegacyDir  string
}

// AntigravityPaths resolves all filesystem paths for Antigravity.
func (r *Resolver) AntigravityPaths() (*AntigravityPaths, error) {
	var cliDir string
	if env := r.getenv("ANTIGRAVITY_CONFIG_DIR"); env != "" {
		cliDir = filepath.Clean(env)
	} else {
		home, err := r.userHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolving antigravity cli home directory: %w", err)
		}
		cliDir = filepath.Join(home, ".gemini", "antigravity-cli")
	}

	home, err := r.userHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolving antigravity home directory: %w", err)
	}

	legacyDir := filepath.Join(home, ".antigravity")
	var desktopConfig, desktopData string

	if r.goos == "darwin" {
		desktopConfig = filepath.Join(home, "Library", "Application Support", "Antigravity")
		desktopData = desktopConfig
	} else {
		if xdgConfig := r.getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
			desktopConfig = filepath.Join(xdgConfig, "Antigravity")
		} else {
			desktopConfig = filepath.Join(home, ".config", "Antigravity")
		}

		if xdgData := r.getenv("XDG_DATA_HOME"); xdgData != "" {
			desktopData = filepath.Join(xdgData, "Antigravity")
		} else {
			desktopData = filepath.Join(home, ".local", "share", "Antigravity")
		}
	}

	return &AntigravityPaths{
		CLIConfigDir:      cliDir,
		CLIOAuthTokenFile: filepath.Join(cliDir, "antigravity-oauth-token"),
		CLISettingsFile:   filepath.Join(cliDir, "settings.json"),
		DesktopConfigDir:  desktopConfig,
		DesktopDataDir:    desktopData,
		DesktopLegacyDir:  legacyDir,
	}, nil
}

// OpenCodePaths contains resolved paths for OpenCode configuration and data.
type OpenCodePaths struct {
	ConfigDir  string
	ConfigFile string
	AuthFile   string
}

// OpenCodePaths resolves all filesystem paths for OpenCode.
func (r *Resolver) OpenCodePaths() (*OpenCodePaths, error) {
	var configDir string
	if env := r.getenv("OPENCODE_CONFIG_DIR"); env != "" {
		configDir = filepath.Clean(env)
	} else {
		home, err := r.userHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolving opencode home directory: %w", err)
		}
		configDir = filepath.Join(home, ".opencode")
	}

	return &OpenCodePaths{
		ConfigDir:  configDir,
		ConfigFile: filepath.Join(configDir, "config.json"),
		AuthFile:   filepath.Join(configDir, "auth.json"),
	}, nil
}

// ProviderConfigDir returns the default configuration directory for a specific provider.
func (r *Resolver) ProviderConfigDir(provider string) (string, error) {
	switch provider {
	case "antigravity":
		paths, err := r.AntigravityPaths()
		if err != nil {
			return "", err
		}
		return paths.CLIConfigDir, nil
	case "claude":
		paths, err := r.ClaudePaths()
		if err != nil {
			return "", err
		}
		return paths.ConfigDir, nil
	case "codex":
		paths, err := r.CodexPaths()
		if err != nil {
			return "", err
		}
		return paths.HomeDir, nil
	case "opencode":
		paths, err := r.OpenCodePaths()
		if err != nil {
			return "", err
		}
		return paths.ConfigDir, nil
	default:
		base, err := r.ConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(base, "providers", provider), nil
	}
}

// ReadClaudeCredentials attempts to read Claude credentials from macOS Keychain first,
// falling back to reading the disk-based .credentials.json file.
func (r *Resolver) ReadClaudeCredentials(ctx context.Context) (*ClaudeCredentials, error) {
	var keychainErr error
	var keychainCreds *ClaudeCredentials
	if r.keychain != nil {
		data, err := r.keychain.GetGenericPassword(ctx, ClaudeCredentialService, "")
		if err == nil && len(data) > 0 {
			creds, parseErr := ParseClaudeCredentials(data, r.now())
			if parseErr == nil {
				return creds, nil
			}
			keychainErr = parseErr
			keychainCreds = creds
		}
	}

	paths, err := r.ClaudePaths()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(paths.CredentialsFile)
	if err != nil {
		if os.IsNotExist(err) {
			if keychainErr != nil {
				return keychainCreds, keychainErr
			}
			return nil, ErrCredentialNotFound
		}
		return nil, fmt.Errorf("reading claude credentials file: %w", err)
	}

	return ParseClaudeCredentials(data, r.now())
}

// ReadCodexAuth reads and parses Codex auth.json from the resolved Codex home directory.
func (r *Resolver) ReadCodexAuth(ctx context.Context) (*CodexAuth, error) {
	paths, err := r.CodexPaths()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(paths.AuthFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrCredentialNotFound
		}
		return nil, fmt.Errorf("reading codex auth file: %w", err)
	}

	return ParseCodexAuth(data, r.now())
}

// Package-level functions using default OS resolver.

// ConfigDir returns the configuration directory for dipstick using OS environment.
func ConfigDir() (string, error) {
	return New().ConfigDir()
}

// DataDir returns the data and state directory for dipstick using OS environment.
func DataDir() (string, error) {
	return New().DataDir()
}

// CacheDir returns the cache directory for dipstick using OS environment.
func CacheDir() (string, error) {
	return New().CacheDir()
}

// ClaudePaths returns resolved paths for Claude Code using OS environment.
func ClaudePathsSummary() (*ClaudePaths, error) {
	return New().ClaudePaths()
}

// CodexPaths returns resolved paths for Codex CLI using OS environment.
func CodexPathsSummary() (*CodexPaths, error) {
	return New().CodexPaths()
}

// AntigravityPaths returns resolved paths for Antigravity using OS environment.
func AntigravityPathsSummary() (*AntigravityPaths, error) {
	return New().AntigravityPaths()
}

// OpenCodePaths returns resolved paths for OpenCode using OS environment.
func OpenCodePathsSummary() (*OpenCodePaths, error) {
	return New().OpenCodePaths()
}

// ProviderConfigDir returns the default configuration directory for a specific provider using OS environment.
func ProviderConfigDir(provider string) (string, error) {
	return New().ProviderConfigDir(provider)
}

// ReadClaudeCredentials reads Claude credentials using default OS resolution.
func ReadClaudeCredentials(ctx context.Context) (*ClaudeCredentials, error) {
	return New().ReadClaudeCredentials(ctx)
}

// ReadCodexAuth reads Codex authentication using default OS resolution.
func ReadCodexAuth(ctx context.Context) (*CodexAuth, error) {
	return New().ReadCodexAuth(ctx)
}

// EnsureDir creates the given directory path if it does not already exist.
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("creating directory %q: %w", path, err)
	}
	return nil
}
