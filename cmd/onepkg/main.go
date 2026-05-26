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

	var configPath string
	var platformOverride string
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
				platformOverride = os.Args[i+1]
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

	cfg, err := ParseConfig(configPath)
	if err != nil {
		fmt.Printf("Error parsing config: %v\n", err)
		os.Exit(1)
	}

	cfg.ApplyDefaults()

	if platformOverride != "" {
		cfg.Platforms = strings.Split(platformOverride, ",")
	}
	if outputOverride != "" {
		cfg.OutputDir = outputOverride
	}

	configDir := filepath.Dir(configPath)
	if err := cfg.Validate(configDir); err != nil {
		fmt.Printf("Error validating config: %v\n", err)
		os.Exit(1)
	}

	entries, err := CollectFiles(configDir, cfg.Items)
	if err != nil {
		fmt.Printf("Error collecting files: %v\n", err)
		os.Exit(1)
	}

	tarData, err := BuildTarGZ(entries)
	if err != nil {
		fmt.Printf("Error building archive: %v\n", err)
		os.Exit(1)
	}

	shaMap := make(map[string]string)
	for _, e := range entries {
		shaMap[e.RelPath] = e.SHA1
	}

	outputDir := filepath.Join(configDir, cfg.OutputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Printf("Error creating output dir: %v\n", err)
		os.Exit(1)
	}

	for _, platform := range cfg.Platforms {
		if err := BuildForPlatform(cfg, platform, entries, tarData, shaMap, outputDir); err != nil {
			fmt.Printf("Error building %s: %v\n", platform, err)
			os.Exit(1)
		}
		fmt.Printf("Created: %s\n", platformOutputPath(cfg, platform, outputDir))
	}

	fmt.Printf("\nBuilt %d platform(s)\n", len(cfg.Platforms))
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
			serviceContent = installer.GenerateSystemdUnit(installer.SystemdConfig{
				Name:       svc.Name,
				Exec:       filepath.Join(defaultDir, svc.Exec),
				Restart:    svc.Restart,
				User:       svc.User,
				Type:       svc.Linux.Type,
				After:      svc.Linux.After,
				RestartSec: svc.Linux.RestartSec,
			})
		}
	case "darwin":
		defaultDir = "/opt/" + cfg.Name
		if len(cfg.Services) > 0 {
			svc := cfg.Services[0]
			serviceType = "launchd"
			serviceName = "com." + svc.Name + ".plist"
			serviceContent = installer.GenerateLaunchdPlist(installer.LaunchdConfig{
				Name:      svc.Name,
				Exec:      filepath.Join(defaultDir, svc.Exec),
				User:      svc.User,
				RunAtLoad: svc.MacOS.RunAtLoad,
				KeepAlive: svc.MacOS.KeepAlive,
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
  onepkg -c config.toml [options]

Options:
  -c, --config <path>      Path to config.toml (required)
      --platform <p>       Build for specific platform (e.g., linux-arm64)
      --output <dir>       Override output directory
  -h, --help               Show this help

Examples:
  onepkg -c config.toml
  onepkg -c config.toml --platform linux-arm64
  onepkg -c config.toml --output ./dist`)
}
