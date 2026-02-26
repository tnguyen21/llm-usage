package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds user preferences for which providers to display.
type Config struct {
	Providers ProviderConfig `json:"providers"`
}

// ProviderConfig holds visibility settings for each provider.
type ProviderConfig struct {
	Claude bool `json:"claude"`
	Codex  bool `json:"codex"`
	Kimi   bool `json:"kimi"`
}

// DefaultConfig returns the default configuration (all providers enabled).
func DefaultConfig() Config {
	return Config{
		Providers: ProviderConfig{
			Claude: true,
			Codex:  true,
			Kimi:   true,
		},
	}
}

// configDir returns the configuration directory.
func configDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "llm-usage")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "llm-usage")
}

// configPath returns the full path to the config file.
func configPath() string {
	return filepath.Join(configDir(), "config.json")
}

// LoadConfig loads the configuration from disk, or returns defaults if not found.
func LoadConfig() (Config, error) {
	cfg := DefaultConfig()
	path := configPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Config doesn't exist yet, return defaults
			return cfg, nil
		}
		return cfg, fmt.Errorf("failed to read config: %w", err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("failed to parse config: %w", err)
	}

	return cfg, nil
}

// Save saves the configuration to disk.
func (c Config) Save() error {
	dir := configDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	path := configPath()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// AnyEnabled returns true if at least one provider is enabled.
func (c Config) AnyEnabled() bool {
	return c.Providers.Claude || c.Providers.Codex || c.Providers.Kimi
}

// ToggleProvider toggles the visibility of a provider.
func (c *Config) ToggleProvider(name string) bool {
	switch name {
	case "claude":
		c.Providers.Claude = !c.Providers.Claude
		return c.Providers.Claude
	case "codex":
		c.Providers.Codex = !c.Providers.Codex
		return c.Providers.Codex
	case "kimi":
		c.Providers.Kimi = !c.Providers.Kimi
		return c.Providers.Kimi
	}
	return false
}
