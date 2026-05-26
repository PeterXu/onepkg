package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Name      string          `toml:"name"`
	Version   string          `toml:"version"`
	Platforms []string        `toml:"platforms"`
	OutputDir string          `toml:"output_dir"`
	Items     []string        `toml:"items"`
	Services  []ServiceConfig `toml:"services"`
}

type ServiceConfig struct {
	Name    string       `toml:"name"`
	Exec    string       `toml:"exec"`
	Restart string       `toml:"restart"`
	User    string       `toml:"user"`
	Linux   LinuxService `toml:"linux"`
	MacOS   MacOSService `toml:"macos"`
}

type LinuxService struct {
	Type       string   `toml:"type"`
	After      []string `toml:"after"`
	RestartSec int      `toml:"restart_sec"`
}

type MacOSService struct {
	RunAtLoad bool `toml:"run_at_load"`
	KeepAlive bool `toml:"keep_alive"`
}

func ParseConfig(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

func (c *Config) ApplyDefaults() {
	if c.OutputDir == "" {
		c.OutputDir = "packages"
	}
	// Platforms has no default - must be provided via --platform flag
	for i := range c.Services {
		if c.Services[i].Name == "" {
			c.Services[i].Name = c.Name
		}
		if c.Services[i].Restart == "" {
			c.Services[i].Restart = "on-failure"
		}
		if c.Services[i].User == "" {
			c.Services[i].User = "root"
		}
		if c.Services[i].Exec == "" {
			for _, item := range c.Items {
				if strings.HasPrefix(item, "bin/") {
					c.Services[i].Exec = item
					break
				}
			}
		}
	}
}

func (c *Config) Validate(configDir string) error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	if c.Version == "" {
		return fmt.Errorf("version is required")
	}
	if len(c.Items) == 0 {
		return fmt.Errorf("at least one item is required")
	}
	for _, item := range c.Items {
		if filepath.IsAbs(item) {
			return fmt.Errorf("absolute path not allowed in item %q", item)
		}
		cleanRel, err := filepath.Rel(configDir, filepath.Join(configDir, item))
		if err != nil || strings.HasPrefix(cleanRel, "..") {
			return fmt.Errorf("path traversal not allowed in item %q", item)
		}
		fullPath := filepath.Join(configDir, item)
		if _, err := os.Stat(fullPath); err != nil {
			return fmt.Errorf("item %q does not exist", item)
		}
	}
	itemSet := make(map[string]bool, len(c.Items))
	for _, item := range c.Items {
		itemSet[item] = true
	}
	for _, svc := range c.Services {
		if svc.Name != "" && !isValidName(svc.Name) {
			return fmt.Errorf("service name %q must contain only alphanumeric characters, hyphens, and underscores", svc.Name)
		}
		if svc.Exec != "" && !itemSet[svc.Exec] {
			matched := false
			for _, item := range c.Items {
				if strings.HasPrefix(svc.Exec, item) {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("service exec %q not found in items", svc.Exec)
			}
		}
	}
	return nil
}

func isValidName(name string) bool {
	for _, ch := range name {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_') {
			return false
		}
	}
	return len(name) > 0
}
