package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml"

	"onepkg/installer"
)

type Config struct {
	Name                string          `toml:"name"`
	Version             string          `toml:"version"`
	Platforms           []string        `toml:"platforms"`
	OutputDir           string          `toml:"output_dir"`
	PreservePermissions *bool           `toml:"preserve_permissions"` // nil = default true
	Exclude             []string        `toml:"exclude"`              // patterns to exclude from package
	Items               []string        `toml:"items"`
	Dirs                []DirConfig     `toml:"dirs"`
	SystemOverlay       []OverlayConfig `toml:"system_overlay"`
	Services            []ServiceConfig `toml:"services"`
	Mode                string          `toml:"mode"` // "install" (default) | "run"
	Commands            []CommandConfig `toml:"commands"`
	RequireAdmin        bool            `toml:"require_admin"` // run mode only; default false (zero value)
}

// CommandConfig is one entry in [[commands]] for mode = "run" packages.
// Exactly one of Exec or Shell must be set.
type CommandConfig struct {
	Exec  string   `toml:"exec"`  // bundled file path, resolved against the temp extraction dir; empty if Shell set
	Args  []string `toml:"args"`  // args for Exec
	Shell string   `toml:"shell"` // arbitrary PowerShell expression; empty if Exec set
}

type ServiceConfig struct {
	Name    string         `toml:"name"`
	Exec    string         `toml:"exec"`
	Args    []string       `toml:"args"`
	Restart string         `toml:"restart"`
	User    string         `toml:"user"`
	Linux   LinuxService   `toml:"linux"`
	MacOS   MacOSService   `toml:"macos"`
	Windows WindowsService `toml:"windows"`
}

type WindowsService struct {
	StartType       string `toml:"start_type"`        // auto | manual | disabled
	RecoveryDelayMs *int   `toml:"recovery_delay_ms"` // delay before restart on failure (ms); nil = default 5000, 0 = no recovery
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

type OverlayConfig struct {
	System string `toml:"system"`
	Item   string `toml:"item"`
}

func ParseConfig(path string) (*Config, error) {
	tree, err := toml.LoadFile(path)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	var cfg Config
	if err := tree.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return &cfg, nil
}

func (c *Config) ApplyDefaults() {
	if c.Mode == "" {
		c.Mode = "install"
	}
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
		// Windows service defaults
		if c.Services[i].Windows.StartType == "" {
			c.Services[i].Windows.StartType = "auto"
		}
		if c.Services[i].Windows.RecoveryDelayMs == nil {
			defaultDelay := 5000
			c.Services[i].Windows.RecoveryDelayMs = &defaultDelay
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

	mode := c.Mode
	if mode == "" {
		mode = "install"
	}
	switch mode {
	case "install", "run":
		// valid
	default:
		return fmt.Errorf("mode %q must be \"install\" or \"run\"", c.Mode)
	}

	// Item existence / traversal checks apply to items present in either mode.
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

	if mode == "run" {
		return c.validateRun()
	}
	return c.validateInstall(configDir)
}

// validateInstall holds the install-mode rules: items required, [[commands]]
// rejected, plus service/dirs/overlay checks (unchanged from pre-run-mode behavior).
func (c *Config) validateInstall(configDir string) error {
	if len(c.Items) == 0 {
		return fmt.Errorf("at least one item is required")
	}
	if len(c.Commands) > 0 {
		return fmt.Errorf("[[commands]] is only allowed in mode = \"run\"")
	}

	itemSet := make(map[string]bool, len(c.Items))
	for _, item := range c.Items {
		itemSet[item] = true
	}
	for _, svc := range c.Services {
		if svc.Name != "" && !isValidName(svc.Name) {
			return fmt.Errorf("service name %q must contain only alphanumeric characters, hyphens, and underscores", svc.Name)
		}
		if svc.Exec != "" && HasPlatformMarker(svc.Exec) {
			return fmt.Errorf("service exec %q should not contain {platform} marker (use stripped path, e.g., \"bin/myapp\")", svc.Exec)
		}
		if svc.Exec != "" && !itemSet[svc.Exec] {
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
		if svc.Windows.StartType != "" {
			switch svc.Windows.StartType {
			case "auto", "manual", "disabled":
			default:
				return fmt.Errorf("service windows start_type %q must be one of: auto, manual, disabled", svc.Windows.StartType)
			}
		}
		if svc.Windows.RecoveryDelayMs != nil && *svc.Windows.RecoveryDelayMs < 0 {
			return fmt.Errorf("service windows recovery_delay_ms must be >= 0")
		}
	}
	for _, d := range c.Dirs {
		if d.Path == "" {
			return fmt.Errorf("dirs path is required")
		}
		if strings.Contains(d.Path, "..") {
			return fmt.Errorf("path traversal not allowed in dirs path %q", d.Path)
		}
		if err := installer.ValidateKnownFolderMarkers(d.Path); err != nil {
			return err
		}
		cleanPath := filepath.Clean(d.Path)
		if filepath.IsAbs(d.Path) && isCriticalPath(cleanPath) {
			return fmt.Errorf("critical system path not allowed: %q", d.Path)
		}
		if d.Mode != "" && !isValidMode(d.Mode) {
			return fmt.Errorf("invalid mode %q for dirs path %q (must be octal like 0755)", d.Mode, d.Path)
		}
		if d.Owner != "" && !isValidName(d.Owner) {
			return fmt.Errorf("invalid owner %q for dirs path %q", d.Owner, d.Path)
		}
		if d.Group != "" && !isValidName(d.Group) {
			return fmt.Errorf("invalid group %q for dirs path %q", d.Group, d.Path)
		}
	}
	for _, overlay := range c.SystemOverlay {
		if overlay.Item == "" {
			return fmt.Errorf("overlay item is required")
		}
		if overlay.System == "" {
			return fmt.Errorf("overlay system path is required")
		}
		if err := installer.ValidateKnownFolderMarkers(overlay.System); err != nil {
			return err
		}
		if strings.Contains(overlay.System, "..") {
			return fmt.Errorf("path traversal not allowed in overlay system path %q", overlay.System)
		}
		// A marked system path (e.g. {AppDataFolder}\...) is absolute on the target
		// but cannot be Go-evaluated for absoluteness or critical-path safety, so
		// those checks only apply to literal paths.
		if !installer.HasKnownFolderMarker(overlay.System) {
			if !filepath.IsAbs(overlay.System) {
				return fmt.Errorf("overlay system path %q must be an absolute path", overlay.System)
			}
			cleanSys := filepath.Clean(overlay.System)
			if isCriticalPath(cleanSys) {
				return fmt.Errorf("critical system path not allowed in overlay: %q", overlay.System)
			}
		}
		matched := false
		for _, item := range c.Items {
			if overlay.Item == item {
				matched = true
				break
			}
			if HasPlatformMarker(item) && StripPlatformMarker(item) == overlay.Item {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("overlay item %q not found in items", overlay.Item)
		}
	}
	return nil
}

// validateRun holds the run-mode rules: install artifacts rejected, commands
// required and well-formed, command exec must reference a bundled item.
func (c *Config) validateRun() error {
	if len(c.Services) > 0 {
		return fmt.Errorf("[[services]] is not allowed in mode = \"run\"")
	}
	if len(c.Dirs) > 0 {
		return fmt.Errorf("[[dirs]] is not allowed in mode = \"run\"")
	}
	if len(c.SystemOverlay) > 0 {
		return fmt.Errorf("[[system_overlay]] is not allowed in mode = \"run\"")
	}
	if len(c.Commands) == 0 {
		return fmt.Errorf("mode = \"run\" requires at least one [[commands]] entry")
	}
	for i, cmd := range c.Commands {
		hasExec := cmd.Exec != ""
		hasShell := cmd.Shell != ""
		if hasExec == hasShell {
			return fmt.Errorf("command %d must set exactly one of exec or shell", i)
		}
		if hasExec {
			if HasPlatformMarker(cmd.Exec) {
				return fmt.Errorf("command %d exec %q should not contain {platform} marker (use stripped path)", i, cmd.Exec)
			}
			if !itemMatches(c.Items, cmd.Exec) {
				return fmt.Errorf("command %d exec %q not found in items", i, cmd.Exec)
			}
		}
		for _, a := range cmd.Args {
			if err := installer.ValidateKnownFolderMarkers(a); err != nil {
				return err
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

// itemMatches reports whether exec names a bundled item, by exact match or by the
// stripped {platform} form (e.g. "bin/cleanup" matches item "bin/cleanup.{platform}").
// It deliberately does NOT use service-exec's looser prefix match (config.go Validate).
func itemMatches(items []string, exec string) bool {
	for _, item := range items {
		if item == exec {
			return true
		}
		if HasPlatformMarker(item) && StripPlatformMarker(item) == exec {
			return true
		}
	}
	return false
}
