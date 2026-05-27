package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"onepkg/installer"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	// Subcommand dispatch
	if os.Args[1] == "toml-update" {
		var addMode bool
		rest := os.Args[2:]
		if len(rest) > 0 && rest[0] == "--add" {
			addMode = true
			rest = rest[1:]
		}
		var err error
		if addMode {
			err = runTomlUpdateAdd(rest)
		} else {
			err = runTomlUpdate(rest)
		}
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	var configPath string
	var platformInput string
	var outputOverride string

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-c", "--config":
			if i+1 < len(os.Args) {
				configPath = os.Args[i+1]
				i++
			}
		case "--platform":
			if i+1 < len(os.Args) {
				platformInput = os.Args[i+1]
				i++
			}
		case "--output":
			if i+1 < len(os.Args) {
				outputOverride = os.Args[i+1]
				i++
			}
		case "help", "-h", "--help":
			printHelp()
			os.Exit(0)
		}
	}

	if configPath == "" {
		fmt.Println("Error: -c <config.toml> is required")
		os.Exit(1)
	}

	if platformInput == "" {
		fmt.Println("Error: --platform <platforms> is required")
		fmt.Println("Example: --platform linux-arm64")
		fmt.Println("         --platform linux-arm64,linux-amd64")
		os.Exit(1)
	}

	platforms, err := ParsePlatforms(platformInput)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		fmt.Printf("Error parsing config: %v\n", err)
		os.Exit(1)
	}

	cfg.ApplyDefaults()

	if outputOverride != "" {
		cfg.OutputDir = outputOverride
	}

	configDir := filepath.Dir(configPath)
	if err := cfg.Validate(configDir); err != nil {
		fmt.Printf("Error validating config: %v\n", err)
		os.Exit(1)
	}

	outputDir := filepath.Join(configDir, cfg.OutputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Printf("Error creating output dir: %v\n", err)
		os.Exit(1)
	}

	for _, platform := range platforms {
		resolved, err := ResolveItems(configDir, cfg.Items, platform)
		if err != nil {
			fmt.Printf("Error resolving items for %s: %v\n", platform, err)
			os.Exit(1)
		}

		entries, err := CollectFilesResolved(configDir, resolved, cfg.Exclude)
		if err != nil {
			fmt.Printf("Error collecting files for %s: %v\n", platform, err)
			os.Exit(1)
		}

		tarData, err := BuildTarGZ(entries, *cfg.PreservePermissions)
		if err != nil {
			fmt.Printf("Error building archive for %s: %v\n", platform, err)
			os.Exit(1)
		}

		shaMap := make(map[string]string)
		for _, e := range entries {
			shaMap[e.RelPath] = e.SHA1
		}

		if err := BuildForPlatform(cfg, platform, entries, tarData, shaMap, outputDir); err != nil {
			fmt.Printf("Error building %s: %v\n", platform, err)
			os.Exit(1)
		}
		fmt.Printf("Created: %s\n", platformOutputPath(cfg, platform, outputDir))
	}

	fmt.Printf("\nBuilt %d platform(s)\n", len(platforms))
}

func BuildForPlatform(cfg *Config, platform string, entries []FileEntry, tarData []byte, shaMap map[string]string, outputDir string) error {
	parts := strings.SplitN(platform, "-", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid platform format %q, expected {os}-{arch}", platform)
	}
	goos := parts[0]
	arch := parts[1]

	versionContent, err := GenerateVersion(VersionInfo{
		Name:     cfg.Name,
		Version:  cfg.Version,
		Platform: platform,
		Created:  time.Now().UTC(),
		Files:    len(entries),
		SHA:      shaMap,
	})
	if err != nil {
		return fmt.Errorf("generate version: %w", err)
	}

	var serviceContent string
	var serviceType string
	var serviceName string
	var defaultDir string

	switch goos {
	case "linux":
		defaultDir = "/usr/local/" + cfg.Name
		if len(cfg.Services) > 0 {
			svc := cfg.Services[0]
			serviceType = "systemd"
			serviceName = svc.Name + ".service"
			workingDir := svc.Linux.WorkingDir
			if workingDir == "" {
				workingDir = defaultDir // default to install directory
			} else if !filepath.IsAbs(workingDir) {
				workingDir = filepath.Join(defaultDir, workingDir)
			}
			serviceContent = installer.GenerateSystemdUnit(installer.SystemdConfig{
				Name:        svc.Name,
				Exec:        filepath.Join(defaultDir, svc.Exec),
				Args:        svc.Args,
				Restart:     svc.Restart,
				User:        svc.User,
				Type:        svc.Linux.Type,
				After:       svc.Linux.After,
				RestartSec:  svc.Linux.RestartSec,
				WorkingDir:  workingDir,
				Environment: svc.Linux.Environment,
				LimitNOFILE: svc.Linux.LimitNOFILE,
			})
		}
	case "darwin":
		defaultDir = "/opt/" + cfg.Name
		if len(cfg.Services) > 0 {
			svc := cfg.Services[0]
			serviceType = "launchd"
			serviceName = "com." + svc.Name + ".plist"
			workingDir := svc.MacOS.WorkingDir
			if workingDir == "" {
				workingDir = defaultDir // default to install directory
			} else if !filepath.IsAbs(workingDir) {
				workingDir = filepath.Join(defaultDir, workingDir)
			}
			serviceContent = installer.GenerateLaunchdPlist(installer.LaunchdConfig{
				Name:        svc.Name,
				Exec:        filepath.Join(defaultDir, svc.Exec),
				Args:        svc.Args,
				User:        svc.User,
				RunAtLoad:   svc.MacOS.RunAtLoad,
				KeepAlive:   svc.MacOS.KeepAlive,
				WorkingDir:  workingDir,
				Environment: svc.MacOS.Environment,
				LimitNOFILE: svc.MacOS.LimitNOFILE,
			})
		}
	default:
		defaultDir = "/usr/local/" + cfg.Name
	}

	if serviceType == "" {
		serviceType = "none"
	}

	expectedArch := arch
	if goos == "linux" && arch == "arm64" {
		expectedArch = "aarch64"
	}

	// Convert DirConfig to DirSpec
	var dirs []installer.DirSpec
	for _, dir := range cfg.Dirs {
		dirs = append(dirs, installer.DirSpec{
			Path:  dir.Path,
			Owner: dir.Owner,
			Group: dir.Group,
			Mode:  dir.Mode,
		})
	}

	shellCfg := installer.ShellConfig{
		Name:           cfg.Name,
		Version:        cfg.Version,
		Platform:       platform,
		ExpectedArch:   expectedArch,
		DefaultDir:     defaultDir,
		PayloadBase64:  base64.StdEncoding.EncodeToString(tarData),
		VersionContent: base64.StdEncoding.EncodeToString([]byte(versionContent)),
		ServiceContent: base64.StdEncoding.EncodeToString([]byte(serviceContent)),
		ServiceType:    serviceType,
		ServiceName:    serviceName,
		Dirs:           dirs,
	}

	script, err := installer.GenerateShellInstaller(shellCfg)
	if err != nil {
		return fmt.Errorf("generate installer: %w", err)
	}

	outputPath := platformOutputPath(cfg, platform, outputDir)
	return os.WriteFile(outputPath, []byte(script), 0755)
}

func platformOutputPath(cfg *Config, platform string, outputDir string) string {
	platformFile := strings.ReplaceAll(platform, "-", "_")
	return filepath.Join(outputDir, fmt.Sprintf("%s_%s_%s.sh", cfg.Name, cfg.Version, platformFile))
}

func printHelp() {
	fmt.Println(`onepkg - General-purpose self-extracting installer generator

Usage:
  onepkg -c config.toml --platform <platforms> [options]
  onepkg toml-update [--add] <key> <value> <file>

Subcommands:
  toml-update           Update a key/value in a TOML file
    --add               Upsert: add if key doesn't exist, update if it does

Options:
  -c, --config <path>      Path to config.toml (required)
      --platform <p>       Target platforms, comma-separated (required)
                           e.g., --platform linux-arm64
                           e.g., --platform linux-arm64,linux-amd64
      --output <dir>       Override output directory
  -h, --help               Show this help

Supported platforms:
  linux-amd64, linux-arm64
  darwin-amd64, darwin-arm64

Item syntax:
  Items can use {platform} marker for platform-specific files:
    "bin/myapp.{platform}"  -> resolves to bin/myapp.linux-arm64, packed as bin/myapp
  Literal items are unchanged:
    "etc/config.conf"       -> packed as etc/config.conf

Examples:
  onepkg -c config.toml --platform linux-arm64
  onepkg -c config.toml --platform linux-arm64,linux-amd64
  onepkg -c config.toml --platform darwin-arm64 --output ./dist
  onepkg toml-update version "1.0.1" config.toml
  onepkg toml-update --add newkey "value" config.toml`)
}
