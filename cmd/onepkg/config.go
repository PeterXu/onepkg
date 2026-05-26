package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Name               string          `toml:"name"`
	Version            string          `toml:"version"`
	Platforms          []string        `toml:"platforms"`
	OutputDir          string          `toml:"output_dir"`
	PreservePermissions *bool          `toml:"preserve_permissions"` // nil = default true
	Exclude            []string        `toml:"exclude"`              // patterns to exclude from package
	Items              []string        `toml:"items"`
	Dirs               []DirConfig     `toml:"dirs"`
	Services           []ServiceConfig `toml:"services"`
}

type ServiceConfig struct {
	Name    string       `toml:"name"`
	Exec    string       `toml:"exec"`
	Args    []string     `toml:"args"`
	Restart string       `toml:"restart"`
	User    string       `toml:"user"`
	Linux   LinuxService `toml:"linux"`
	MacOS   MacOSService `toml:"macos"`
}

type LinuxService struct {
	Type        string            `toml:"type"`
	After       []string          `toml:"after"`
	RestartSec  int               `toml:"restart_sec"`
	WorkingDir  string            `toml:"working_dir"`
	Environment map[string]string `toml:"environment"`
	LimitNOFILE int               `toml:"limit_nofile"`
}

type MacOSService struct {
	RunAtLoad   bool              `toml:"run_at_load"`
	KeepAlive   bool              `toml:"keep_alive"`
	WorkingDir  string            `toml:"working_dir"`
	Environment map[string]string `toml:"environment"`
	LimitNOFILE int               `toml:"limit_nofile"`
}

type DirConfig struct {
	Path  string `toml:"path"`  // Required
	Owner string `toml:"owner"` // Optional, default: service user or root
	Group string `toml:"group"` // Optional, default: same as owner
	Mode  string `toml:"mode"`  // Optional, default: "0755"
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
	if c.PreservePermissions == nil {
		defaultTrue := true
		c.PreservePermissions = &defaultTrue
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
	// Dirs defaults
	defaultUser := "root"
	if len(c.Services) > 0 && c.Services[0].User != "" {
		defaultUser = c.Services[0].User
	}
	for i := range c.Dirs {
		if c.Dirs[i].Owner == "" {
			c.Dirs[i].Owner = defaultUser
		}
		if c.Dirs[i].Group == "" {
			c.Dirs[i].Group = c.Dirs[i].Owner
		}
		if c.Dirs[i].Mode == "" {
			c.Dirs[i].Mode = "0755"
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
		// Skip file existence check for {platform} markers - validated at build time
		if !HasPlatformMarker(item) {
			fullPath := filepath.Join(configDir, item)
			if _, err := os.Stat(fullPath); err != nil {
				return fmt.Errorf("item %q does not exist", item)
			}
		}
	}
	// Service validation
	itemSet := make(map[string]bool, len(c.Items))
	for _, item := range c.Items {
		itemSet[item] = true
	}
	for _, svc := range c.Services {
		if svc.Name != "" && !isValidName(svc.Name) {
			return fmt.Errorf("service name %q must contain only alphanumeric characters, hyphens, and underscores", svc.Name)
		}
		// Service exec must not contain {platform} marker - use stripped path
		if svc.Exec != "" && HasPlatformMarker(svc.Exec) {
			return fmt.Errorf("service exec %q should not contain {platform} marker (use stripped path, e.g., \"bin/myapp\")", svc.Exec)
		}
		// Service exec validation needs to handle {platform} markers in items
		if svc.Exec != "" && !itemSet[svc.Exec] {
			// Check if exec matches a platform-marked item (stripped form)
			matched := false
			for _, item := range c.Items {
				if HasPlatformMarker(item) {
					if StripPlatformMarker(item) == svc.Exec {
						matched = true
						break
					}
				} else if strings.HasPrefix(svc.Exec, item) {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("service exec %q not found in items", svc.Exec)
			}
		}
	}
	// Dirs validation
	for _, d := range c.Dirs {
		if d.Path == "" {
			return fmt.Errorf("dirs path is required")
		}
		// Check path traversal - look for ".." in original path before cleaning
		if strings.Contains(d.Path, "..") {
			return fmt.Errorf("path traversal not allowed in dirs path %q", d.Path)
		}
		cleanPath := filepath.Clean(d.Path)
		// Check critical system paths (only for absolute paths)
		if filepath.IsAbs(d.Path) && isCriticalPath(cleanPath) {
			return fmt.Errorf("critical system path not allowed: %q", d.Path)
		}
		// Validate mode if specified
		if d.Mode != "" && !isValidMode(d.Mode) {
			return fmt.Errorf("invalid mode %q for dirs path %q (must be octal like 0755)", d.Mode, d.Path)
		}
		// Validate owner/group if specified
		if d.Owner != "" && !isValidName(d.Owner) {
			return fmt.Errorf("invalid owner %q for dirs path %q", d.Owner, d.Path)
		}
		if d.Group != "" && !isValidName(d.Group) {
			return fmt.Errorf("invalid group %q for dirs path %q", d.Group, d.Path)
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

// criticalPaths is a blacklist of system paths that must never be created by installers
var criticalPaths = map[string]bool{
	"/etc/passwd":  true,
	"/etc/shadow":  true,
	"/etc/sudoers": true,
	"/root":        true,
	"/boot":        true,
	"/sys":         true,
	"/proc":        true,
	"/dev":         true,
}

// isCriticalPath checks if a path is a critical system path or under one
func isCriticalPath(path string) bool {
	// Check exact match
	if criticalPaths[path] {
		return true
	}
	// Check if path is under a critical directory
	for critical := range criticalPaths {
		if strings.HasPrefix(path, critical+"/") {
			return true
		}
	}
	return false
}

// isValidMode checks if mode is a valid octal permission string (0[0-7]{3,4})
func isValidMode(mode string) bool {
	if len(mode) < 4 || len(mode) > 5 {
		return false
	}
	if mode[0] != '0' {
		return false
	}
	for _, c := range mode[1:] {
		if c < '0' || c > '7' {
			return false
		}
	}
	return true
}

const PlatformMarker = "{platform}"

// HasPlatformMarker returns true if the item contains {platform} placeholder
func HasPlatformMarker(item string) bool {
	return strings.Contains(item, PlatformMarker)
}

// ResolvePlatformItem replaces {platform} with the actual platform string
func ResolvePlatformItem(item, platform string) string {
	return strings.ReplaceAll(item, PlatformMarker, platform)
}

// StripPlatformMarker removes .{platform} from the item path
func StripPlatformMarker(item string) string {
	return strings.ReplaceAll(item, "."+PlatformMarker, "")
}
